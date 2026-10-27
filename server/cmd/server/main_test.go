package main

import (
	"bytes"
	"context"
	"encoding/json"
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
	if err := app.SaveDevices(devicesFile, []app.SavedDevice{{DeviceID: "AABBCCDDEEFF", Kind: app.PlayerDevice, Node: 1}}); err != nil {
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
