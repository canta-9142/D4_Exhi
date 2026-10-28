package serial

import (
	"bufio"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestListPorts(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"ttyACM1", "ttyACM0", "ttyUSB0", "unrelated"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	previous := portPatterns
	t.Cleanup(func() { portPatterns = previous })
	portPatterns = []string{filepath.Join(dir, "tty*"), filepath.Join(dir, "ttyACM*")}
	got, err := ListPorts()
	want := []string{filepath.Join(dir, "ttyACM0"), filepath.Join(dir, "ttyACM1"), filepath.Join(dir, "ttyUSB0")}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("port candidates = %v, %v; want %v", got, err, want)
	}
}

func TestRunPortRetriesAndReconnects(t *testing.T) {
	h := newTransportHarness(t)
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	first, peer1 := newObservedPort()
	defer peer1.Close()
	second, peer2 := newObservedPort()
	defer peer2.Close()
	var attempts atomic.Int32
	open := func(name string) (io.ReadWriteCloser, error) {
		if name != "test-port" {
			t.Errorf("opened unexpected path: %s", name)
		}
		switch attempts.Add(1) {
		case 1:
			return nil, os.ErrNotExist
		case 2:
			return first, nil
		case 3:
			return second, nil
		default:
			return nil, os.ErrNotExist
		}
	}
	done := make(chan struct{})
	go func() { h.transport.runPort(ctx, h.app, "test-port", open, time.Millisecond); close(done) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("port loop did not stop")
		}
	})
	var oldID string
	select {
	case id := <-h.started:
		oldID = string(id)
	case <-h.ctx.Done():
		t.Fatal(h.ctx.Err())
	}
	h.hello(peer1, bufio.NewReader(peer1))
	_ = peer1.Close()
	select {
	case id := <-h.started:
		if string(id) == oldID {
			t.Fatal("port reconnect reused a session")
		}
	case <-h.ctx.Done():
		t.Fatal(h.ctx.Err())
	}
	h.hello(peer2, bufio.NewReader(peer2))
	cancel()
	select {
	case <-done:
	case <-h.ctx.Done():
		t.Fatal(h.ctx.Err())
	}
	state, err := h.app.State(h.ctx)
	if err != nil || state.Devices[0].Connected || attempts.Load() != 3 || first.closes.Load() != 1 || second.closes.Load() != 1 {
		t.Fatalf("bad retry/shutdown state: %+v, %v; attempts=%d, closes=%d/%d", state, err, attempts.Load(), first.closes.Load(), second.closes.Load())
	}
}

func TestRunPortCancellation(t *testing.T) {
	for _, stage := range []string{"before-open", "retry-wait", "after-open"} {
		t.Run(stage, func(t *testing.T) {
			h := newTransportHarness(t)
			ctx, cancel := context.WithCancel(h.ctx)
			defer cancel()
			port, peer := newObservedPort()
			defer peer.Close()
			var attempts atomic.Int32
			attempted := make(chan struct{})
			if stage == "before-open" {
				cancel()
			}
			open := func(string) (io.ReadWriteCloser, error) {
				attempts.Add(1)
				close(attempted)
				if stage == "after-open" {
					cancel()
					return port, nil
				}
				return nil, os.ErrNotExist
			}
			done := make(chan struct{})
			go func() { h.transport.runPort(ctx, h.app, "cancel-port", open, time.Hour); close(done) }()
			if stage == "retry-wait" {
				select {
				case <-attempted:
				case <-h.ctx.Done():
					t.Fatal(h.ctx.Err())
				}
				cancel()
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("port loop ignored cancellation")
			}
			if stage == "before-open" && attempts.Load() != 0 {
				t.Fatal("canceled loop opened a port")
			}
			if stage == "after-open" && port.closes.Load() != 1 {
				t.Fatal("port opened during cancellation was not closed")
			}
		})
	}
}
