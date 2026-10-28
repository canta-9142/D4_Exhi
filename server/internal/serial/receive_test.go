package serial

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/canta-9142/d4-exhi/internal/app"
)

// Blocking app's logger models a slow app event without blocking any transport
// callback. The transport uses a separate logger, so its receive loop is free.
type pausedAppLog struct {
	slog.Handler
	blocked chan struct{}
	release chan struct{}
	once    sync.Once
	entered sync.Once
}

func (l *pausedAppLog) Handle(ctx context.Context, record slog.Record) error {
	if record.Message == "duplicate device UID" {
		l.entered.Do(func() { close(l.blocked) })
		<-l.release
	}
	return l.Handler.Handle(ctx, record)
}

func (l *pausedAppLog) resume() { l.once.Do(func() { close(l.release) }) }

// Input buffered here is already available to Read, just like bytes in a TTY's
// kernel buffer. Each chunk is smaller than the transport's read buffer.
type bufferedReceivePort struct {
	input  chan []byte
	reads  chan int
	closed chan struct{}
	once   sync.Once
	count  int // Only the read goroutine uses count.
}

func newBufferedReceivePort(chunks ...string) *bufferedReceivePort {
	p := &bufferedReceivePort{input: make(chan []byte, 8), reads: make(chan int, 8), closed: make(chan struct{})}
	for _, chunk := range chunks {
		p.input <- []byte(chunk)
	}
	return p
}

func (p *bufferedReceivePort) Read(buf []byte) (int, error) {
	select {
	case data := <-p.input:
		if len(data) > len(buf) {
			return 0, errors.New("test input exceeds the Read buffer")
		}
		p.count++
		select {
		case p.reads <- p.count:
		case <-p.closed:
			return 0, io.EOF
		}
		return copy(buf, data), nil
	case <-p.closed:
		return 0, io.EOF
	}
}

func (p *bufferedReceivePort) Write(data []byte) (int, error) { return len(data), nil }
func (p *bufferedReceivePort) Close() error {
	p.once.Do(func() { close(p.closed) })
	return nil
}

func serveWithPausedApp(t *testing.T, port io.ReadWriteCloser, pauseRegistration bool) (*servedConnection, *pausedAppLog) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	log := &pausedAppLog{
		Handler: slog.NewTextHandler(io.Discard, nil),
		blocked: make(chan struct{}), release: make(chan struct{}),
	}
	transport := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	application := app.New(app.Options{
		Logger: slog.New(log),
		Send: func(id app.SessionID, message app.Message) error {
			if id == "reference" || id == "pause" {
				return nil
			}
			return transport.Send(id, message)
		},
		Close: transport.Close,
	})
	appDone := make(chan error, 1)
	go func() { appDone <- application.Run(ctx) }()
	t.Cleanup(func() {
		log.resume()
		cancel()
		select {
		case <-appDone:
		case <-time.After(time.Second):
			t.Error("app did not stop")
		}
	})
	if err := application.Connected(ctx, "reference"); err != nil {
		t.Fatal(err)
	}
	identity := playerIdentity
	identity.Identification = 1
	if err := application.MessageReceived(ctx, "reference", identity); err != nil {
		t.Fatal(err)
	}
	if pauseRegistration {
		if err := application.Connected(ctx, "pause"); err != nil {
			t.Fatal(err)
		}
		go func() { _ = application.MessageReceived(ctx, "pause", identity) }()
		select {
		case <-log.blocked:
		case <-ctx.Done():
			t.Fatal("app did not pause before session registration")
		}
	}
	s := &servedConnection{done: make(chan struct{})}
	go func() { s.err = transport.Serve(ctx, application, port); close(s.done) }()
	t.Cleanup(func() {
		log.resume()
		cancel()
		select {
		case <-s.done:
		case <-time.After(time.Second):
			t.Error("session did not stop")
		}
	})
	select {
	case <-log.blocked:
	case <-ctx.Done():
		t.Fatal("app did not reach the paused event")
	}
	return s, log
}

func TestTransportAppDelayDoesNotCauseReceiveTimeout(t *testing.T) {
	// Every byte is available before serving starts. A valid resync is split
	// into more chunks than the raw read queue can hold while app is paused.
	port := newBufferedReceivePort(playerHello+"\n"+`{"type":"resync","data":{"padding":"a`, "b", "c", "d\"}}\n")
	s, log := serveWithPausedApp(t, port, false)
	for count := 0; count < 3; {
		select {
		case count = <-port.reads:
		case <-time.After(time.Second):
			t.Fatal("reader did not reach the buffered input")
		}
	}
	select {
	case <-time.After(LineTimeout + 100*time.Millisecond):
	case <-s.done:
		t.Fatalf("session ended while input was available: %v", s.err)
	}
	log.resume()
	select {
	case <-s.done:
		t.Fatalf("app delay caused a receive failure: %v", s.err)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestTransportReceiveQueueOverflowWhileAppPaused(t *testing.T) {
	port := newBufferedReceivePort(playerHello + "\n")
	s, log := serveWithPausedApp(t, port, false)
	// The paused handler already consumed HELLO. Exactly ReceiveQueueSize more
	// messages may wait; the next must fail without blocking the receive loop.
	port.input <- []byte(strings.Repeat(resyncLine+"\n", ReceiveQueueSize+1))
	select {
	case <-port.closed:
	case <-time.After(time.Second):
		t.Fatal("full message queue blocked receiving instead of closing the session")
	}
	log.resume()
	select {
	case <-s.done:
		if !errors.Is(s.err, ErrReceiveQueueFull) {
			t.Fatalf("receive queue overflow = %v", s.err)
		}
	case <-time.After(time.Second):
		t.Fatal("session did not finish after app resumed")
	}
}

func TestTransportDetectsReceiveTimeoutWhileAppPaused(t *testing.T) {
	port := newBufferedReceivePort(playerHello + "\n{")
	s, log := serveWithPausedApp(t, port, false)
	select {
	case <-port.closed:
	case <-time.After(LineTimeout + 500*time.Millisecond):
		t.Fatal("paused app prevented detection of actual receive inactivity")
	}
	log.resume()
	select {
	case <-s.done:
		if !errors.Is(s.err, ErrLineTimeout) {
			t.Fatalf("real receive timeout = %v", s.err)
		}
	case <-time.After(time.Second):
		t.Fatal("session did not finish after app resumed")
	}
}

func TestTransportRegistrationDelayDoesNotCauseReceiveTimeout(t *testing.T) {
	port := newBufferedReceivePort(playerHello+"\n"+`{"type":"resync","data":{"padding":"a`, "b", "c", "d\"}}\n")
	s, log := serveWithPausedApp(t, port, true)
	select {
	case <-time.After(LineTimeout + 100*time.Millisecond):
	case <-s.done:
		t.Fatalf("session ended during registration: %v", s.err)
	}
	log.resume()
	// Ensure all input was read after app resumed, then allow timeout handling
	// to run. No byte gap was introduced by the delayed registration.
	for count := 0; count < 4; {
		select {
		case count = <-port.reads:
		case <-s.done:
			t.Fatalf("registration delay caused a receive failure: %v", s.err)
		case <-time.After(time.Second):
			t.Fatal("reader did not consume buffered input")
		}
	}
	select {
	case <-s.done:
		t.Fatalf("registration delay caused a receive failure: %v", s.err)
	case <-time.After(100 * time.Millisecond):
	}
}
