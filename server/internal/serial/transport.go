package serial

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/canta-9142/d4-exhi/internal/app"
)

const (
	SendQueueSize    = 64
	ReceiveQueueSize = 64
)

var (
	ErrSessionClosed    = errors.New("serial session is closed")
	ErrQueueFull        = errors.New("serial send queue is full")
	ErrReceiveQueueFull = errors.New("serial receive message queue is full")
	nextSession         atomic.Uint64
)

// Transport owns active sessions. Construct it before app, then pass Send and
// Close as app's callbacks. Start app.Run before serving any connections.
type Transport struct {
	mu       sync.Mutex
	sessions map[app.SessionID]*connection
	logger   *slog.Logger
}

type connection struct {
	id       app.SessionID
	ctx      context.Context
	cancel   context.CancelFunc
	port     io.ReadWriteCloser
	receiver Receiver
	outbound chan []byte
	done     chan struct{}
	err      error // Written before done is closed; first failure wins.
}

type readResult struct {
	identification uint64
	data           []byte
	at             time.Time
	err            error
}

func New(logger *slog.Logger) *Transport {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	}
	return &Transport{sessions: make(map[app.SessionID]*connection), logger: logger}
}

// Serve takes ownership of an already-open port and blocks until its session
// ends. The port must support concurrent Read/Write, and Close must unblock both.
// Every call allocates a process-wide unique SessionID, including reconnects.
// It closes the port, joins its I/O workers and notifies app before returning.
func (t *Transport) Serve(ctx context.Context, application *app.App, port io.ReadWriteCloser) error {
	return t.serve(ctx, application, port, t.logger)
}

func (t *Transport) serve(ctx context.Context, application *app.App, port io.ReadWriteCloser, logger *slog.Logger) error {
	id, err := allocateSessionID()
	if err != nil {
		_ = port.Close()
		return err
	}
	logger = logger.With("session", id)
	sessionCtx, cancel := context.WithCancel(ctx)
	s := &connection{
		id: id, ctx: sessionCtx, cancel: cancel, port: port,
		receiver: Receiver{Logger: logger},
		outbound: make(chan []byte, SendQueueSize), done: make(chan struct{}),
	}
	t.mu.Lock()
	t.sessions[id] = s
	t.mu.Unlock()
	logger.Info("serial session started")

	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		select {
		case <-ctx.Done():
			t.stop(s, ctx.Err())
		case <-s.done:
		}
		_ = port.Close()
	}()
	go func() { defer workers.Done(); t.write(s) }()

	err = application.Connected(s.ctx, id)
	if err == nil {
		// Do not timestamp input before registration completes: app's initial
		// processing delay must not become a gap between captured read times.
		reads := make(chan readResult, 1)
		messages := make(chan app.IncomingMessage, ReceiveQueueSize)
		workers.Add(2)
		go func() { defer workers.Done(); t.read(s, reads) }()
		go func() { defer workers.Done(); t.deliver(s, application, messages) }()
		err = s.receive(reads, messages)
	}
	t.stop(s, err)
	workers.Wait()
	// A connection's cancellation must not suppress its Disconnected event.
	// app must remain running until Serve returns, or have completed Run already.
	if err := application.Disconnected(context.WithoutCancel(ctx), id); err != nil && !errors.Is(err, app.ErrStopped) {
		logger.Warn("serial disconnect notification failed", "error", err)
	}
	if errors.Is(s.err, io.EOF) || errors.Is(s.err, ErrSessionClosed) || errors.Is(s.err, context.Canceled) || errors.Is(s.err, context.DeadlineExceeded) {
		logger.Info("serial session ended", "error", s.err)
	} else {
		logger.Warn("serial session failed", "error", s.err)
	}
	return s.err
}

// Send copies the encoded line into a bounded FIFO. It never waits for serial
// I/O or an app event. Queue failure immediately invalidates the session.
func (t *Transport) Send(id app.SessionID, message app.Message) error {
	line, err := message.MarshalLine()
	if err == nil && len(line) > MaxLineBytes {
		err = ErrLineTooLong
	}
	if err == nil {
		line = append(line, '\n')
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.sessions[id]
	if s == nil {
		return ErrSessionClosed
	}
	if err != nil {
		t.stopLocked(s, err)
		return err
	}
	if message.BeginIdentification {
		s.receiver.WaitHello(message.Identification)
	}
	select {
	case s.outbound <- line:
		return nil
	default:
		t.stopLocked(s, ErrQueueFull)
		return ErrQueueFull
	}
}

// Close is safe in app's callback: it invalidates the session and wakes the
// port-closing worker, without waiting for I/O or calling back into app.
func (t *Transport) Close(id app.SessionID) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s := t.sessions[id]; s != nil {
		t.stopLocked(s, ErrSessionClosed)
	}
}

func (t *Transport) stop(s *connection, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopLocked(s, err)
}

func (t *Transport) stopLocked(s *connection, err error) {
	if t.sessions[s.id] != s {
		return
	}
	delete(t.sessions, s.id)
	s.err = err
	close(s.done)
	s.cancel()
}

func (t *Transport) write(s *connection) {
	for {
		select {
		case <-s.done:
			return
		case line := <-s.outbound:
			// done and outbound may both be ready: do not drain a closed session.
			select {
			case <-s.done:
				return
			default:
			}
			n, err := s.port.Write(line)
			if err == nil && n != len(line) {
				err = io.ErrShortWrite
			}
			if err != nil {
				t.stop(s, err)
				return
			}
		}
	}
}

func (t *Transport) read(s *connection, reads chan<- readResult) {
	buf := make([]byte, 4096)
	for {
		// Pin the generation before blocking. Receiver discards the result if
		// identify changes during Read; old bytes must never acquire a new tag.
		identification := s.receiver.identify.Load()
		n, err := s.port.Read(buf)
		if n != 0 || err != nil {
			result := readResult{identification: identification, data: append([]byte(nil), buf[:n]...), at: time.Now(), err: err}
			select {
			case reads <- result:
			case <-s.done:
				return
			}
		}
		if err != nil {
			return
		}
		select {
		case <-s.done:
			return
		default:
		}
	}
}

// deliver is the only worker that waits for app. Parsing and the receive
// deadline continue independently, even while an app event is still running.
func (t *Transport) deliver(s *connection, application *app.App, messages <-chan app.IncomingMessage) {
	for {
		select {
		case <-s.done:
			return
		case message := <-messages:
			if err := application.MessageReceived(s.ctx, s.id, message); err != nil {
				t.stop(s, err)
				return
			}
		}
	}
}

func (s *connection) receive(reads <-chan readResult, messages chan<- app.IncomingMessage) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	handle := func(message app.IncomingMessage) error {
		if err := s.ctx.Err(); err != nil {
			return err
		}
		select {
		case messages <- message:
			return nil
		default:
			return ErrReceiveQueueFull
		}
	}
	feed := func(result readResult) error {
		if err := s.ctx.Err(); err != nil {
			return err
		}
		if len(result.data) != 0 {
			if err := s.receiver.feed(result.data, result.at, result.identification, handle); err != nil {
				return err
			}
		}
		return result.err
	}
	for {
		// Drain already-received data before checking inactivity. Buffered
		// fragments retain their read times even if this loop runs later.
		select {
		case result := <-reads:
			if err := feed(result); err != nil {
				return err
			}
			continue
		default:
		}
		select {
		case <-s.done:
			return s.err
		case result := <-reads:
			if err := feed(result); err != nil {
				return err
			}
		case <-ticker.C:
			select {
			case result := <-reads:
				if err := feed(result); err != nil {
					return err
				}
			default:
				if err := s.receiver.CheckTimeout(time.Now()); err != nil {
					return err
				}
			}
		}
	}
}

func allocateSessionID() (app.SessionID, error) {
	for {
		current := nextSession.Load()
		if current == ^uint64(0) {
			return "", errors.New("serial session IDs exhausted")
		}
		if nextSession.CompareAndSwap(current, current+1) {
			return app.SessionID("serial-" + strconv.FormatUint(current+1, 10)), nil
		}
	}
}
