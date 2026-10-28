package serial

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func openTestPTY(t *testing.T) (*os.File, string) {
	t.Helper()
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	master := os.NewFile(uintptr(fd), "pty-master")
	t.Cleanup(func() { _ = master.Close() })
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	if err := master.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	return master, "/dev/pts/" + strconv.Itoa(number)
}

func TestOpenPortRawTTY(t *testing.T) {
	master, name := openTestPTY(t)
	port, err := OpenPort(name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = port.Close() })
	alias := filepath.Join(t.TempDir(), "same-device")
	if err := os.Symlink(name, alias); err != nil {
		t.Fatal(err)
	}
	if duplicate, err := OpenPort(alias); err == nil {
		_ = duplicate.Close()
		t.Fatal("opened the same TTY twice through a symlink")
	}
	raw, err := port.(*ttyPort).SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Control(func(fd uintptr) {
		settings, err := unix.IoctlGetTermios(int(fd), unix.TCGETS)
		if err != nil {
			t.Error(err)
			return
		}
		if settings.Iflag != 0 || settings.Oflag != 0 || settings.Lflag != 0 || settings.Cflag&unix.CSIZE != unix.CS8 || settings.Cflag&(unix.PARENB|unix.CSTOPB|unix.CRTSCTS) != 0 || settings.Cflag&unix.CBAUD != unix.B115200 {
			t.Errorf("unexpected TTY settings: %+v", settings)
		}
		flags, err := unix.FcntlInt(fd, unix.F_GETFL, 0)
		if err != nil || flags&unix.O_NONBLOCK == 0 {
			t.Errorf("TTY lost nonblocking mode: flags=%x, %v", flags, err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	input := []byte("hello\r\n")
	if _, err := master.Write(input); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(input))
	if _, err := io.ReadFull(port, got); err != nil || !bytes.Equal(got, input) {
		t.Fatalf("TTY changed incoming bytes: %q, %v", got, err)
	}
	if _, err := port.Write(input); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(master, got); err != nil || !bytes.Equal(got, input) {
		t.Fatalf("TTY changed outgoing bytes: %q, %v", got, err)
	}
	if err := port.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenPort(name)
	if err != nil {
		t.Fatalf("cannot reopen closed TTY: %v", err)
	}
	_ = reopened.Close()
}

func TestOpenPortCloseUnblocksIO(t *testing.T) {
	for _, operation := range []string{"read", "write"} {
		t.Run(operation, func(t *testing.T) {
			_, name := openTestPTY(t)
			port, err := OpenPort(name)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = port.Close() })
			started := make(chan struct{})
			result := make(chan error, 1)
			go func() {
				close(started)
				var err error
				if operation == "read" {
					_, err = port.Read(make([]byte, 1))
				} else {
					// A large write fills the PTY buffer while its master is unread.
					_, err = port.Write(make([]byte, 1024*1024))
				}
				result <- err
			}()
			<-started
			select {
			case err := <-result:
				t.Fatalf("%s did not block: %v", operation, err)
			case <-time.After(20 * time.Millisecond):
			}
			closed := make(chan error, 1)
			go func() { closed <- port.Close() }()
			select {
			case err := <-closed:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("TTY Close blocked")
			}
			select {
			case err := <-result:
				if err == nil {
					t.Fatalf("%s succeeded after Close", operation)
				}
			case <-time.After(time.Second):
				t.Fatalf("Close did not unblock %s", operation)
			}
		})
	}
}

func TestOpenPortRejectsNonTTY(t *testing.T) {
	name := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(name, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{name, name + "-missing"} {
		if port, err := OpenPort(path); err == nil {
			_ = port.Close()
			t.Fatalf("opened a non-TTY: %s", path)
		}
	}
	data, err := os.ReadFile(name)
	if err != nil || strings.TrimSpace(string(data)) != "preserve" {
		t.Fatalf("non-TTY contents changed: %q, %v", data, err)
	}
}
