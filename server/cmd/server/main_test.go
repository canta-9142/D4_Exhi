package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
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
