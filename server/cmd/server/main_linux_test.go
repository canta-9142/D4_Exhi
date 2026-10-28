package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/canta-9142/d4-exhi/internal/app"
	"github.com/canta-9142/d4-exhi/internal/serial"
)

func serverTestPTY(t *testing.T) (*os.File, string) {
	t.Helper()
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	master := os.NewFile(uintptr(fd), "server-test-pty")
	t.Cleanup(func() { _ = master.Close() })
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	if err := master.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	return master, fmt.Sprintf("/dev/pts/%d", number)
}

func TestFatalPersistenceFailureClosesSerialPort(t *testing.T) {
	master, portPath := serverTestPTY(t)
	listener, err := net.Listen("tcp", testAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	logger := slog.New(slog.DiscardHandler)
	transport := serial.New(logger)
	application := app.New(app.Options{
		Logger: logger, Send: transport.Send, Close: transport.Close,
		SaveDevices: func([]app.SavedDevice) error { return app.ErrSaveUncertain },
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, application, listener, logger, transport, []string{portPath}) }()
	line, err := bufio.NewReader(master).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != `{"type":"identify","data":{}}` {
		t.Fatalf("initial serial identification: %q, %v", line, err)
	}
	if _, err := io.WriteString(master, "{\"type\":\"hello\",\"data\":{\"uid\":\"001122AABBCC\"}}\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, app.ErrSaveUncertain) {
			t.Fatalf("serial persistence failure lost: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("server did not finish serial workers after fatal persistence failure")
	}
	// Returning from serve must release the TTY's exclusive open as well as
	// stopping app and HTTP; otherwise the port cannot be used after restart.
	port, err := serial.OpenPort(portPath)
	if err != nil {
		t.Fatalf("serial port still held after fatal shutdown: %v", err)
	}
	port.Close()
}

func TestRunWithPlayerAndLEDPorts(t *testing.T) {
	player, playerPath := serverTestPTY(t)
	led, ledPath := serverTestPTY(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var output syncBuffer
	done := make(chan error, 1)
	devicesFile := filepath.Join(t.TempDir(), "devices.json")
	go func() {
		done <- run(ctx, []string{"-http-addr", testAddr, "-devices-file", devicesFile, "-serial-port", playerPath, "-serial-port", ledPath}, &output)
	}()
	finished := false
	t.Cleanup(func() {
		cancel()
		if !finished {
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Error("server did not stop")
			}
		}
	})
	playerReader, ledReader := bufio.NewReader(player), bufio.NewReader(led)
	readLine := func(reader *bufio.Reader) string {
		t.Helper()
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSuffix(line, "\n")
	}
	for _, reader := range []*bufio.Reader{playerReader, ledReader} {
		if line := readLine(reader); line != `{"type":"identify","data":{}}` {
			t.Fatalf("unexpected identify: %s", line)
		}
	}
	if _, err := io.WriteString(player, "{\"type\":\"hello\",\"data\":{\"uid\":\"001122AABBCC\"}}\n"); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"config", "reset", "phase", "status"} {
		var message struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(readLine(playerReader)), &message); err != nil || message.Type != kind {
			t.Fatalf("player synchronization = %+v, %v; want %s", message, err, kind)
		}
	}
	if _, err := io.WriteString(led, "HELLO AABBCCDDEEFF\n"); err != nil {
		t.Fatal(err)
	}
	if line := readLine(ledReader); line != "CONFIG P1 false" {
		t.Fatalf("LED configuration = %s", line)
	}
	cancel()
	select {
	case err := <-done:
		finished = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not finish active serial sessions")
	}
	if !strings.Contains(output.String(), "server stopped") || strings.Count(output.String(), "serial session ended") != 2 {
		t.Fatalf("missing shutdown logs: %s", output.String())
	}
}
