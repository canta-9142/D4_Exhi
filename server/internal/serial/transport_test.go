package serial

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/canta-9142/d4-exhi/internal/app"
)

type transportHarness struct {
	t         *testing.T
	transport *Transport
	app       *app.App
	ctx       context.Context
	started   chan app.SessionID
}

type servedConnection struct {
	id   app.SessionID
	done chan struct{}
	err  error
}

// net.Pipe provides real blocking I/O. The wrapper observes writes before they
// block, and can inject a failure without relying on sleeps or queue timing.
type observedPort struct {
	net.Conn
	writes    chan []byte
	closes    atomic.Int32
	failAfter int
	failure   error
	short     bool
	calls     int // Used only by the single writer goroutine.
}

func newObservedPort() (*observedPort, net.Conn) {
	server, peer := net.Pipe()
	return &observedPort{Conn: server, writes: make(chan []byte, SendQueueSize+16)}, peer
}

func (p *observedPort) Write(data []byte) (int, error) {
	p.calls++
	select {
	case p.writes <- append([]byte(nil), data...):
	default:
	}
	if p.calls > p.failAfter {
		if p.failure != nil {
			return 0, p.failure
		}
		if p.short {
			return len(data) - 1, nil
		}
	}
	return p.Conn.Write(data)
}

func (p *observedPort) Close() error {
	p.closes.Add(1)
	return p.Conn.Close()
}

func newTransportHarness(t *testing.T) *transportHarness {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := &transportHarness{t: t, ctx: ctx, transport: New(logger), started: make(chan app.SessionID, 16)}
	// The simultaneous game accepts READY and CHOOSE without a running
	// operation, so that a test reaches a ready player at once.
	h.app = app.New(app.Options{
		Mode:   app.Simultaneous,
		Logger: logger,
		Send: func(id app.SessionID, message app.Message) error {
			err := h.transport.Send(id, message)
			if message.BeginIdentification {
				h.started <- id
			}
			return err
		},
		Close: h.transport.Close,
	})
	done := make(chan error, 1)
	go func() { done <- h.app.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(time.Second):
			t.Error("app did not stop")
		}
	})
	return h
}

func (h *transportHarness) serve(ctx context.Context, port io.ReadWriteCloser) *servedConnection {
	h.t.Helper()
	s := &servedConnection{done: make(chan struct{})}
	go func() { s.err = h.transport.Serve(ctx, h.app, port); close(s.done) }()
	select {
	case s.id = <-h.started:
	case <-s.done:
		// An immediate write failure can finish Serve before this select runs.
		select {
		case s.id = <-h.started:
		default:
			h.t.Fatalf("session ended before Connected: %v", s.err)
		}
	case <-h.ctx.Done():
		h.t.Fatal(h.ctx.Err())
	}
	h.t.Cleanup(func() {
		h.transport.Close(s.id)
		select {
		case <-s.done:
		case <-time.After(time.Second):
			h.t.Error("serial session did not stop")
		}
	})
	return s
}

func (h *transportHarness) wait(s *servedConnection) error {
	h.t.Helper()
	select {
	case <-s.done:
		return s.err
	case <-h.ctx.Done():
		h.t.Fatal(h.ctx.Err())
		return nil
	}
}

func (h *transportHarness) line(peer net.Conn, reader *bufio.Reader) string {
	h.t.Helper()
	if err := peer.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		h.t.Fatal(err)
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		h.t.Fatal(err)
	}
	return strings.TrimSuffix(line, "\n")
}

func (h *transportHarness) syncPlayer(peer net.Conn, reader *bufio.Reader) string {
	h.t.Helper()
	var generation string
	for _, kind := range []string{"config", "reset", "phase", "status"} {
		var message struct {
			Type string `json:"type"`
			Data struct {
				Sync string `json:"sync"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(h.line(peer, reader)), &message); err != nil {
			h.t.Fatal(err)
		}
		if message.Type != kind {
			h.t.Fatalf("initial synchronization: received %q, want %q", message.Type, kind)
		}
		if kind == "reset" {
			generation = message.Data.Sync
		}
	}
	return generation
}

func (h *transportHarness) hello(peer net.Conn, reader *bufio.Reader) string {
	h.t.Helper()
	if got := h.line(peer, reader); got != `{"type":"identify","data":{}}` {
		h.t.Fatalf("first command = %s", got)
	}
	if _, err := io.WriteString(peer, playerHello+"\r\n"); err != nil {
		h.t.Fatal(err)
	}
	return h.syncPlayer(peer, reader)
}

func TestTransportSendFIFOAndCopy(t *testing.T) {
	h := newTransportHarness(t)
	port, peer := newObservedPort()
	defer peer.Close()
	s := h.serve(h.ctx, port)
	// The writer is blocked in the initial identify. Send must still return.
	select {
	case <-port.writes:
	case <-h.ctx.Done():
		t.Fatal(h.ctx.Err())
	}
	status := &app.StatusData{Score: 42, Remaining: 120}
	messages := []app.Message{
		{Type: "CONFIG", Data: app.LEDConfigData{Node: 9}},
		{Type: "status", Data: status},
		{Type: "CLEAR", Data: struct{}{}},
	}
	var want []string
	for _, message := range messages {
		line, err := message.MarshalLine()
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, string(line))
		if err := h.transport.Send(s.id, message); err != nil {
			t.Fatal(err)
		}
	}
	status.Score = 999 // Queued bytes must not refer back to mutable message data.
	reader := bufio.NewReader(peer)
	if got := h.line(peer, reader); got != `{"type":"identify","data":{}}` {
		t.Fatal(got)
	}
	var got []string
	for range messages {
		got = append(got, h.line(peer, reader))
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("wire order/copy = %v; want %v", got, want)
	}
}

func TestTransportQueueOverflow(t *testing.T) {
	h := newTransportHarness(t)
	port, peer := newObservedPort()
	defer peer.Close()
	s := h.serve(h.ctx, port)
	select {
	case <-port.writes: // The initial identify now occupies the blocked writer.
	case <-h.ctx.Done():
		t.Fatal(h.ctx.Err())
	}
	message := app.Message{Type: "CLEAR", Data: struct{}{}}
	for range SendQueueSize {
		if err := h.transport.Send(s.id, message); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.transport.Send(s.id, message); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("overflow = %v; want ErrQueueFull", err)
	}
	if err := h.transport.Send(s.id, message); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("closed session accepted another send: %v", err)
	}
	if err := h.wait(s); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("session result = %v; want original queue failure", err)
	}
	h.transport.Close(s.id)
	if port.closes.Load() != 1 || len(port.writes) != 0 {
		t.Fatalf("close count = %d, extra writes = %d; want one close and discarded queue", port.closes.Load(), len(port.writes))
	}
}

func TestTransportWriteFailure(t *testing.T) {
	writeFailure := errors.New("injected serial write failure")
	for _, short := range []bool{false, true} {
		name := "error"
		want := writeFailure
		if short {
			name, want = "short-write", io.ErrShortWrite
		}
		t.Run(name, func(t *testing.T) {
			h := newTransportHarness(t)
			port, peer := newObservedPort()
			defer peer.Close()
			port.short = short
			if !short {
				port.failure = writeFailure
			}
			s := h.serve(h.ctx, port)
			if err := h.wait(s); !errors.Is(err, want) {
				t.Fatalf("write failure = %v; want %v", err, want)
			}
			if err := h.transport.Send(s.id, app.Message{Type: "CLEAR"}); !errors.Is(err, ErrSessionClosed) {
				t.Fatalf("failed session accepted another send: %v", err)
			}
			if port.closes.Load() != 1 {
				t.Fatalf("port closed %d times", port.closes.Load())
			}
		})
	}
}

func TestTransportReconnectAndReady(t *testing.T) {
	h := newTransportHarness(t)
	port, peer := newObservedPort()
	defer peer.Close()
	s := h.serve(h.ctx, port)
	reader := bufio.NewReader(peer)
	h.hello(peer, reader)
	if err := h.app.Execute(h.ctx, app.Command{Operation: app.SetNodeEnabled, Node: 1, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	generation := h.syncPlayer(peer, reader)
	// A long press opens the mode selection, and the selection makes the
	// player ready.
	for _, tc := range []struct {
		line  string
		phase app.Phase
		ready bool
	}{
		{`{"type":"ready","data":{"sync":"` + generation + `"}}`, app.Select, false},
		{`{"type":"choose","data":{"sync":"` + generation + `","round":"normal"}}`, app.Idle, true},
	} {
		if _, err := io.WriteString(peer, tc.line+"\n"); err != nil {
			t.Fatal(err)
		}
		var phase struct {
			Type string        `json:"type"`
			Data app.PhaseData `json:"data"`
		}
		var status struct {
			Type string         `json:"type"`
			Data app.StatusData `json:"data"`
		}
		if err := json.Unmarshal([]byte(h.line(peer, reader)), &phase); err != nil || phase.Type != "phase" || phase.Data.Phase != tc.phase {
			t.Fatalf("%s: phase = %+v, %v", tc.line, phase, err)
		}
		if err := json.Unmarshal([]byte(h.line(peer, reader)), &status); err != nil || status.Type != "status" || status.Data.Ready != tc.ready {
			t.Fatalf("%s: status = %+v, %v", tc.line, status, err)
		}
	}
	if err := peer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := h.wait(s); !errors.Is(err, io.EOF) {
		t.Fatalf("disconnect result = %v", err)
	}
	state, err := h.app.State(h.ctx)
	if err != nil || state.Devices[0].Connected || state.Nodes[0].Enabled || state.Nodes[0].Ready {
		t.Fatalf("disconnect did not clear manual/ready: %+v, %v", state, err)
	}
	port2, peer2 := newObservedPort()
	defer peer2.Close()
	s2 := h.serve(h.ctx, port2)
	h.hello(peer2, bufio.NewReader(peer2))
	if s2.id == s.id {
		t.Fatal("reconnect reused a SessionID")
	}
	h.transport.Close(s.id) // A stale close must not affect the new connection.
	state, err = h.app.State(h.ctx)
	if err != nil || !state.Devices[0].Connected || state.Devices[0].Session != s2.id || state.Nodes[0].Enabled {
		t.Fatalf("bad reconnect state: %+v, %v", state, err)
	}
}

func TestTransportCancellationNotifiesRunningApp(t *testing.T) {
	h := newTransportHarness(t)
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	port, peer := newObservedPort()
	defer peer.Close()
	s := h.serve(ctx, port)
	h.hello(peer, bufio.NewReader(peer))
	cancel() // Cancel just this port; app's own context remains active.
	if err := h.wait(s); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel result = %v", err)
	}
	state, err := h.app.State(h.ctx)
	if err != nil || state.Devices[0].Connected {
		t.Fatalf("cancellation suppressed Disconnected: %+v, %v", state, err)
	}
	if port.closes.Load() != 1 {
		t.Fatalf("port closed %d times", port.closes.Load())
	}
}

func TestTransportReceiveFailure(t *testing.T) {
	for _, input := range []string{"invalid\n", strings.Repeat("x", MaxLineBytes+1) + "\n", "{"} {
		name := "malformed"
		if len(input) > MaxLineBytes {
			name = "oversized"
		} else if input == "{" {
			name = "timeout"
		}
		t.Run(name, func(t *testing.T) {
			h := newTransportHarness(t)
			port, peer := newObservedPort()
			defer peer.Close()
			s := h.serve(h.ctx, port)
			h.hello(peer, bufio.NewReader(peer))
			_, _ = io.WriteString(peer, input) // The server may close before the entire invalid line is written.
			err := h.wait(s)
			if err == nil || (name == "oversized" && !errors.Is(err, ErrLineTooLong)) || (name == "timeout" && !errors.Is(err, ErrLineTimeout)) {
				t.Fatalf("receive failure = %v", err)
			}
			state, err := h.app.State(h.ctx)
			if err != nil || state.Devices[0].Connected {
				t.Fatalf("receive failure did not disconnect: %+v, %v", state, err)
			}
		})
	}
}

func TestTransportSessionIDsAreProcessWide(t *testing.T) {
	h1, h2 := newTransportHarness(t), newTransportHarness(t)
	port1, peer1 := newObservedPort()
	defer peer1.Close()
	port2, peer2 := newObservedPort()
	defer peer2.Close()
	s1, s2 := h1.serve(h1.ctx, port1), h2.serve(h2.ctx, port2)
	if s1.id == s2.id {
		t.Fatal("different transports allocated the same SessionID")
	}
}
