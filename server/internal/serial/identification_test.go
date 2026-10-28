package serial

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/canta-9142/d4-exhi/internal/app"
)

func TestQueuedHelloCannotCompleteNewIdentification(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	receiver := Receiver{Logger: logger}
	application := app.New(app.Options{
		Logger: logger,
		Send: func(_ app.SessionID, message app.Message) error {
			if message.BeginIdentification {
				receiver.WaitHello(message.Identification)
			}
			return nil
		},
	})
	done := make(chan error, 1)
	go func() { done <- application.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	const session = app.SessionID("queued-hello")
	receive := func(message app.IncomingMessage) error {
		return application.MessageReceived(ctx, session, message)
	}
	must(application.Connected(ctx, session))
	must(receiver.Feed([]byte(playerHello+"\n"), time.Now(), receive))
	// Leave this board unassigned, but identified, and start its preview.
	must(application.Execute(ctx, app.Command{Operation: app.SetDeviceAssignment, DeviceID: playerIdentity.UID}))
	must(receiver.Feed([]byte(playerHello+"\n"), time.Now(), receive))
	preview := app.Command{Operation: app.SetDevicePreview, DeviceID: playerIdentity.UID}
	must(application.Execute(ctx, preview))

	// The receive FIFO may parse both HELLOs before delivering the first one.
	var queued []app.IncomingMessage
	must(receiver.Feed([]byte(playerHello+"\n"+playerHello+"\n"), time.Now(), func(message app.IncomingMessage) error {
		queued = append(queued, message)
		return nil
	}))
	if len(queued) != 2 {
		t.Fatalf("queued %d HELLOs", len(queued))
	}
	// The first HELLO ends the preview and makes app start a new identify.
	must(receive(queued[0]))
	// This HELLO was already received before that new identify request.
	must(receive(queued[1]))
	must(receiver.CheckTimeout(time.Now()))
	if err := application.Execute(ctx, preview); err == nil {
		t.Fatal("an old queued HELLO completed the new identification")
	}
	// A fresh HELLO must complete identification and restore normal operation.
	must(receiver.Feed([]byte(playerHello+"\n"), time.Now(), receive))
	must(application.Execute(ctx, preview))
}

func TestReceiverRetainsReadIdentification(t *testing.T) {
	var receiver Receiver
	now := time.Now()
	var received []uint64
	handle := func(message app.IncomingMessage) error {
		received = append(received, message.Identification)
		return nil
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	receiver.WaitHello(1)
	must(receiver.feed([]byte(playerHello+"\n"), now, 1, handle))
	receiver.WaitHello(2)
	// These bytes were already read before generation two started, even though
	// parsing was delayed. They must not become a generation-two HELLO.
	must(receiver.feed([]byte(playerHello+"\n"), now, 1, handle))
	if len(received) != 1 {
		t.Fatal("old buffered HELLO was assigned a new generation")
	}
	must(receiver.feed([]byte(playerHello+"\n"), now, 2, handle))
	if len(received) != 2 || received[1] != 2 {
		t.Fatalf("fresh HELLO was not accepted: %v", received)
	}
	receiver.WaitHello(3)
	must(receiver.feed([]byte("old partial"), now, 2, handle))
	must(receiver.feed([]byte(playerHello+"\n"), now, 3, handle))
	if len(received) != 2 {
		t.Fatal("old partial line was not discarded through LF")
	}
	// Retry and delayed old requests must preserve a partially received HELLO.
	must(receiver.Feed([]byte(playerHello[:10]), now, handle))
	receiver.WaitHello(3)
	receiver.WaitHello(2)
	must(receiver.Feed([]byte(playerHello[10:]+"\n"), now, handle))
	if len(received) != 3 || received[2] != 3 {
		t.Fatalf("retry or old identify changed the generation: %v", received)
	}
}

// heldReadPort fixes the order around a blocking Read without sleeps: tests can
// change identification either before data arrives or after bytes are copied.
type heldReadPort struct {
	*bufferedReceivePort
	entered chan struct{}
	copied  chan struct{}
	release chan struct{}
	first   bool // Only the reader goroutine uses first.
}

func (p *heldReadPort) Read(buf []byte) (int, error) {
	if p.first {
		return p.bufferedReceivePort.Read(buf)
	}
	p.first = true
	close(p.entered)
	n, err := p.bufferedReceivePort.Read(buf)
	close(p.copied)
	select {
	case <-p.release:
		return n, err
	case <-p.closed:
		return 0, io.EOF
	}
}

func TestReadAcrossIdentificationDoesNotRelabelHello(t *testing.T) {
	for _, transition := range []string{"after-copy", "while-waiting"} {
		t.Run(transition, func(t *testing.T) {
			port := &heldReadPort{
				bufferedReceivePort: newBufferedReceivePort(),
				entered:             make(chan struct{}), copied: make(chan struct{}), release: make(chan struct{}),
			}
			s := &connection{port: port, done: make(chan struct{})}
			s.receiver.WaitHello(1)
			transport := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
			reads := make(chan readResult, 1)
			workerDone := make(chan struct{})
			go func() { transport.read(s, reads); close(workerDone) }()
			t.Cleanup(func() {
				close(s.done)
				_ = port.Close()
				select {
				case <-workerDone:
				case <-time.After(time.Second):
					t.Error("reader did not stop")
				}
			})
			wait := func(signal <-chan struct{}) {
				t.Helper()
				select {
				case <-signal:
				case <-time.After(time.Second):
					t.Fatal("Read did not reach the expected boundary")
				}
			}
			next := func() readResult {
				t.Helper()
				select {
				case result := <-reads:
					return result
				case <-time.After(time.Second):
					t.Fatal("Read did not return")
					return readResult{}
				}
			}
			wait(port.entered)
			if transition == "after-copy" {
				port.input <- []byte(playerHello + "\n")
				wait(port.copied)
			}
			s.receiver.WaitHello(2)
			if transition == "while-waiting" {
				port.input <- []byte(playerHello + "\n")
				wait(port.copied)
			}
			close(port.release)
			first := next()
			if first.identification != 1 {
				t.Fatalf("Read relabeled its result as generation %d; want 1", first.identification)
			}
			var received []app.IncomingMessage
			handle := func(message app.IncomingMessage) error { received = append(received, message); return nil }
			if err := s.receiver.feed(first.data, first.at, first.identification, handle); err != nil || len(received) != 0 {
				t.Fatalf("HELLO from crossing Read was accepted: %+v, %v", received, err)
			}
			// The periodic identify retry keeps generation two. A subsequent Read
			// must accept its HELLO so conservative discarding cannot stall recovery.
			s.receiver.WaitHello(2)
			port.input <- []byte(playerHello + "\n")
			second := next()
			if err := s.receiver.feed(second.data, second.at, second.identification, handle); err != nil || len(received) != 1 || received[0].Identification != 2 {
				t.Fatalf("fresh HELLO did not recover identification: %+v, %v", received, err)
			}
		})
	}
}
