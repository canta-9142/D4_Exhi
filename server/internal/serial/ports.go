package serial

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"time"

	"github.com/canta-9142/d4-exhi/internal/app"
)

// ListPorts returns USB serial TTY candidates by their OS device-name patterns.
// It never opens a port or probes a connected MCU.
func ListPorts() ([]string, error) {
	var ports []string
	for _, pattern := range portPatterns {
		names, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		ports = append(ports, names...)
	}
	slices.Sort(ports)
	return slices.Compact(ports), nil
}

// RunPort opens a single configured path and serves each connection until ctx
// is canceled. Missing ports and ended sessions are retried after one second.
func (t *Transport) RunPort(ctx context.Context, application *app.App, name string) {
	t.runPort(ctx, application, name, OpenPort, time.Second)
}

func (t *Transport) runPort(ctx context.Context, application *app.App, name string, open func(string) (io.ReadWriteCloser, error), retry time.Duration) {
	logger := t.logger.With("port", name)
	for ctx.Err() == nil {
		port, err := open(name)
		if err != nil {
			logger.Warn("cannot open serial port; will retry", "error", err)
		} else {
			if ctx.Err() != nil {
				_ = port.Close()
				return
			}
			if err := t.serve(ctx, application, port, logger); errors.Is(err, app.ErrStopped) {
				return
			}
		}
		timer := time.NewTimer(retry)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
