package serial

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

var portPatterns = []string{"/dev/ttyACM*", "/dev/ttyUSB*"}

type ttyPort struct{ *os.File }

// OpenPort configures a USB CDC TTY at the firmware's 115200 baud, 8N1, without
// flow control. Nonblocking descriptors let os.File.Close cancel both reads
// and writes through Go's poller. This is required by Transport.Serve.
func OpenPort(name string) (io.ReadWriteCloser, error) {
	fd, err := unix.Open(name, unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open serial port %q: %w", name, err)
	}
	configured := false
	defer func() {
		if !configured {
			_ = unix.Close(fd)
		}
	}()
	// Also excludes aliases of the same device opened by another port worker.
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, fmt.Errorf("lock serial port %q: %w", name, err)
	}
	settings, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return nil, fmt.Errorf("read serial settings %q: %w", name, err)
	}
	settings.Iflag, settings.Oflag, settings.Lflag = 0, 0, 0
	settings.Cflag = unix.CLOCAL | unix.CREAD | unix.CS8 | unix.HUPCL | unix.B115200
	settings.Ispeed, settings.Ospeed = unix.B115200, unix.B115200
	settings.Cc[unix.VMIN], settings.Cc[unix.VTIME] = 1, 0
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, settings); err != nil {
		return nil, fmt.Errorf("configure serial port %q: %w", name, err)
	}
	// DTR enables the ESP32-S3's CDC connection; RTS is kept low for normal use.
	// Some TTYs (including pseudo terminals) have no modem-control lines.
	bits, err := unix.IoctlGetInt(fd, unix.TIOCMGET)
	if err == nil {
		err = unix.IoctlSetPointerInt(fd, unix.TIOCMSET, (bits|unix.TIOCM_DTR)&^unix.TIOCM_RTS)
	}
	if err != nil && !errors.Is(err, unix.ENOTTY) {
		return nil, fmt.Errorf("configure serial control lines %q: %w", name, err)
	}
	if err := unix.IoctlSetInt(fd, unix.TIOCEXCL, 0); err != nil {
		return nil, fmt.Errorf("lock serial port %q: %w", name, err)
	}
	port := &ttyPort{File: os.NewFile(uintptr(fd), name)}
	configured = true // os.File now owns the descriptor, including on failure below.
	if err := port.SetReadDeadline(time.Time{}); err != nil {
		_ = port.Close()
		return nil, fmt.Errorf("serial port %q cannot use cancellable I/O: %w", name, err)
	}
	return port, nil
}

func (p *ttyPort) Close() error {
	// Releasing TIOCEXCL allows a PTY or a still-present CDC port to be reopened.
	// Control keeps the descriptor alive without changing its nonblocking mode.
	if raw, err := p.SyscallConn(); err == nil {
		_ = raw.Control(func(fd uintptr) { _ = unix.IoctlSetInt(int(fd), unix.TIOCNXCL, 0) })
	}
	return p.File.Close()
}
