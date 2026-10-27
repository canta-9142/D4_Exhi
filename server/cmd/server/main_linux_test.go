package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
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

func TestRunWithPlayerAndLEDPorts(t *testing.T) {
	player, playerPath := serverTestPTY(t)
	led, ledPath := serverTestPTY(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var output bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- run(ctx, []string{"-serial-port", playerPath, "-serial-port", ledPath}, &output) }()
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
	if _, err := io.WriteString(led, "HELLO led-main-test\n"); err != nil {
		t.Fatal(err)
	}
	if line := readLine(ledReader); line != "CONFIG P1" {
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
