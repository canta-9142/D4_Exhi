package main

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/canta-9142/d4-exhi/internal/serial"
)

// Tests listen on an ephemeral loopback port instead of the default :8080.
const testAddr = "127.0.0.1:0"

func TestRun(t *testing.T) {
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
		{"serial port", []string{"-http-addr", testAddr, "-serial-port", "/dev/ttyACM0"}, false},
		{"multiple serial ports", []string{"-http-addr", testAddr, "-serial-port", "/dev/ttyACM0", "-serial-port", "/dev/ttyACM1"}, false},
		{"empty serial port", []string{"-serial-port", " "}, true},
		{"duplicate serial port", []string{"-serial-port", "/dev/ttyACM0", "-serial-port", "/dev/./ttyACM0"}, true},
		{"list ports", []string{"-list-serial-ports"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			var output bytes.Buffer
			args := append(append([]string(nil), tc.args...), "-devices-file", filepath.Join(t.TempDir(), "devices.json"))
			err := run(ctx, args, &output)
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
	l, err := net.Listen("tcp", testAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	var output bytes.Buffer
	if err := run(context.Background(), []string{"-http-addr", l.Addr().String(), "-devices-file", filepath.Join(t.TempDir(), "devices.json")}, &output); err == nil {
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var output syncBuffer
	devicesFile := filepath.Join(t.TempDir(), "devices.json")
	store := &app.DeviceStore{Path: devicesFile}
	if err := store.Save([]app.SavedDevice{{DeviceID: "AABBCCDDEEFF", Kind: app.PlayerDevice, Node: 1}}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- run(ctx, []string{"-http-addr", testAddr, "-devices-file", devicesFile}, &output) }()

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
	resp, err := http.Get("http://" + addr + "/api/state")
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		Devices []struct {
			DeviceID   string
			Connected  bool
			Assignment struct{ NodeID string }
		}
	}
	err = json.NewDecoder(resp.Body).Decode(&state)
	resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK || len(state.Devices) != 1 || state.Devices[0].DeviceID != "AABBCCDDEEFF" || state.Devices[0].Connected || state.Devices[0].Assignment.NodeID != "P1" {
		t.Fatalf("restored devices not exposed as disconnected: %+v, %v", state, err)
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

func TestRunRejectsInvalidDeviceFileBeforeListening(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	if err := os.WriteFile(path, []byte(`{"version":99,"devices":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	// The invalid address would fail independently; the device file must be
	// validated first, before HTTP or serial can accept an event.
	err := run(context.Background(), []string{"-http-addr", "invalid", "-devices-file", path}, &output)
	if err == nil || !strings.Contains(err.Error(), "load device assignments") || !strings.Contains(output.String(), "cannot load device assignments") || strings.Contains(output.String(), "server starting") {
		t.Fatalf("invalid device file did not prevent startup: %v, %s", err, output.String())
	}
	data, _ := os.ReadFile(path)
	if string(data) != `{"version":99,"devices":[]}` {
		t.Fatal("invalid file was replaced during startup")
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
		SaveDevices: func([]app.SavedDevice) error { return app.ErrSaveUncertain },
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, application, listener, slog.New(slog.DiscardHandler), serial.New(slog.New(slog.DiscardHandler)), nil)
	}()
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
	if err := application.MessageReceived(ctx, "player", app.IncomingMessage{Type: "hello", UID: "001122AABBCC", Kind: app.PlayerDevice, Identification: 1}); err == nil {
		t.Fatal("uncertain automatic assignment returned success")
	}
	select {
	case err := <-done:
		if !errors.Is(err, app.ErrSaveUncertain) {
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
