package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/canta-9142/d4-exhi/internal/app"
)

// Tests listen on an ephemeral loopback port instead of the default :8080.
const testAddr = "127.0.0.1:0"

func TestRun(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, tc := range []struct {
		name      string
		args      []string
		wantError bool
	}{
		{"default", []string{"-http-addr", testAddr}, false},
		{"debug", []string{"-log-level", "debug", "-http-addr", testAddr}, false},
		{"help", []string{"-h"}, false}, {"invalid level", []string{"-log-level", "invalid"}, true},
		{"unknown option", []string{"-unknown"}, true}, {"positional argument", []string{"extra"}, true},
		{"invalid address", []string{"-http-addr", "127.0.0.1:invalid"}, true},
		{"empty device path", []string{"-devices-file", ""}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			var output bytes.Buffer
			err := run(ctx, tc.args, &output)
			if (err != nil) != tc.wantError {
				t.Fatalf("run returned %v", err)
			}
			if tc.name == "default" && (!strings.Contains(output.String(), "server starting") || !strings.Contains(output.String(), "server stopped")) {
				t.Fatal("lifecycle logs missing")
			}
		})
	}
}

func TestRunPortInUse(t *testing.T) {
	t.Chdir(t.TempDir())
	l, err := net.Listen("tcp", testAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	var output bytes.Buffer
	if err := run(context.Background(), []string{"-http-addr", l.Addr().String()}, &output); err == nil {
		t.Fatal("run succeeded on a port in use")
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestRunServesHTTP(t *testing.T) {
	t.Chdir(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var output syncBuffer
	done := make(chan error, 1)
	go func() { done <- run(ctx, []string{"-http-addr", testAddr}, &output) }()

	addrPattern := regexp.MustCompile(`http_addr=(\S+)`)
	var addr string
	for deadline := time.Now().Add(5 * time.Second); addr == ""; {
		if m := addrPattern.FindStringSubmatch(output.String()); m != nil {
			addr = m[1]
		} else if time.Now().After(deadline) {
			t.Fatalf("listen address not logged: %s", output.String())
		} else {
			time.Sleep(10 * time.Millisecond)
		}
	}
	for path, want := range map[string]int{"/": http.StatusOK, "/api/unknown": http.StatusNotFound} {
		resp, err := http.Get("http://" + addr + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("GET %s: status = %d, want %d", path, resp.StatusCode, want)
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run did not stop")
	}
	if !strings.Contains(output.String(), "server stopped") {
		t.Fatal("stop log missing")
	}
	if _, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		t.Error("listener still open after run returned")
	}
}

func TestRunLoadsDeviceFileBeforeListening(t *testing.T) {
	listener, err := net.Listen("tcp", testAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	path := filepath.Join(t.TempDir(), "devices.json")
	invalid := []byte(`{"version":2,"devices":[]}`)
	if err := os.WriteFile(path, invalid, 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	// The occupied address must not mask the earlier file validation error.
	err = run(context.Background(), []string{"-http-addr", listener.Addr().String(), "-devices-file", path}, &output)
	if err == nil || !strings.Contains(err.Error(), "load device assignments") || !strings.Contains(output.String(), "cannot load device assignments") || strings.Contains(output.String(), "server starting") {
		t.Fatalf("bad file reached network startup: %v, logs %s", err, output.String())
	}
	if body, err := os.ReadFile(path); err != nil || !bytes.Equal(body, invalid) {
		t.Fatalf("startup overwrote invalid file: %s, %v", body, err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"devices":[{"deviceId":"001122AABBCC","kind":"player","nodeId":"P1","autoAssignExcluded":true}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := run(ctx, []string{"-http-addr", testAddr, "-devices-file", path}, io.Discard); err != nil {
		t.Fatalf("valid saved assignments prevented startup: %v", err)
	}
}

func TestFatalPersistenceFailureClosesHTTPAndReturnsError(t *testing.T) {
	listener, err := net.Listen("tcp", testAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	application := app.New(app.Options{
		Logger:      slog.New(slog.DiscardHandler),
		Send:        func(app.SessionID, app.Message) error { return nil },
		SaveDevices: func([]app.DeviceAssignment) error { return app.ErrDeviceSaveUncertain },
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, application, listener, slog.New(slog.DiscardHandler)) }()
	// Leave a partially received HTTP request open to exercise active connections.
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: localhost\r\n"); err != nil {
		t.Fatal(err)
	}
	if err := application.Connected(ctx, "player"); err != nil {
		t.Fatal(err)
	}
	if err := application.MessageReceived(ctx, "player", app.IncomingMessage{Type: "hello", UID: "001122AABBCC", Kind: app.PlayerDevice}); err == nil {
		t.Fatal("uncertain automatic assignment returned success")
	}
	select {
	case err := <-done:
		if !errors.Is(err, app.ErrDeviceSaveUncertain) {
			t.Fatalf("fatal persistence error lost: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("HTTP did not stop promptly after fatal persistence failure")
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var b [1]byte
	if _, err := conn.Read(b[:]); err == nil {
		t.Fatal("HTTP connection remained open")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("HTTP connection was not closed")
	}
	if conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second); err == nil {
		conn.Close()
		t.Fatal("HTTP listener remained open")
	}
}
