package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/canta-9142/d4-exhi/internal/app"
	"github.com/canta-9142/d4-exhi/internal/serial"
	"github.com/canta-9142/d4-exhi/internal/web"
)

const shutdownTimeout = 5 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("server", flag.ContinueOnError)
	flags.SetOutput(output)
	levelName := flags.String("log-level", "info", "log level: debug, info, warn, error")
	// All interfaces by default: the admin connects from a laptop over the LAN.
	httpAddr := flags.String("http-addr", ":8080", "listen address of the admin HTTP server")
	devicesFile := flags.String("devices-file", "data/devices.json", "device assignment JSON file")
	listPorts := flags.Bool("list-serial-ports", false, "list USB serial port candidates and exit")
	var ports []string
	flags.Func("serial-port", "USB CDC port path (repeat for multiple ports)", func(value string) error {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("serial port path must not be empty")
		}
		name, err := filepath.Abs(value)
		if err != nil {
			return err
		}
		if slices.Contains(ports, name) {
			return fmt.Errorf("serial port specified more than once: %s", name)
		}
		ports = append(ports, name)
		return nil
	})
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(*levelName)); err != nil {
		return err
	}
	if *listPorts {
		names, err := serial.ListPorts()
		if err != nil {
			return err
		}
		for _, name := range names {
			if _, err := fmt.Fprintln(output, name); err != nil {
				return err
			}
		}
		return nil
	}
	logger := slog.New(slog.NewTextHandler(output, &slog.HandlerOptions{Level: level}))
	if strings.TrimSpace(*devicesFile) == "" {
		return errors.New("device assignment file path must not be empty")
	}
	devices, err := app.LoadDevices(*devicesFile)
	if err != nil {
		logger.Error("cannot load device assignments", "path", *devicesFile, "error", err)
		return fmt.Errorf("load device assignments: %w", err)
	}
	// Listen before starting app so that a port in use is a startup error.
	listener, err := net.Listen("tcp", *httpAddr)
	if err != nil {
		return fmt.Errorf("http listen: %w", err)
	}
	transport := serial.New(logger)
	application := app.New(app.Options{
		Logger: logger, Send: transport.Send, Close: transport.Close, Devices: devices,
		SaveDevices: func(candidate []app.SavedDevice) error { return app.SaveDevices(*devicesFile, candidate) },
	})
	server := &http.Server{
		Handler:           web.New(application, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}
	logger.Info("server starting", "phase", app.Preparing, "http_addr", listener.Addr().String(), "serial_ports", ports)

	// app outlives ctx until HTTP has shut down, so in-flight requests can
	// still reach it.
	appCtx, stopApp := context.WithCancel(context.WithoutCancel(ctx))
	defer stopApp()
	appDone := make(chan error, 1)
	go func() { appDone <- application.Run(appCtx) }()
	httpDone := make(chan error, 1)
	go func() { httpDone <- server.Serve(listener) }()
	portsCtx, stopPorts := context.WithCancel(ctx)
	defer stopPorts()
	var workers sync.WaitGroup
	for _, name := range ports {
		workers.Add(1)
		go func() { defer workers.Done(); transport.RunPort(portsCtx, application, name) }()
	}

	var appErr, httpErr error
	appRunning, httpRunning := true, true
	select {
	case <-ctx.Done():
	case appErr = <-appDone:
		appRunning = false
		if appErr == nil {
			appErr = errors.New("stopped unexpectedly")
		}
	case httpErr = <-httpDone:
		httpRunning = false
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Warn("http shutdown timed out; closing connections", "error", err)
		server.Close()
	}
	if httpRunning {
		if err := <-httpDone; !errors.Is(err, http.ErrServerClosed) {
			httpErr = err
		}
	}
	// Keep app running until every serial session has delivered Disconnected.
	stopPorts()
	workers.Wait()
	stopApp()
	if appRunning {
		appErr = <-appDone
	}
	logger.Info("server stopped")
	var errs []error
	if appErr != nil {
		errs = append(errs, fmt.Errorf("app: %w", appErr))
	}
	if httpErr != nil {
		errs = append(errs, fmt.Errorf("http server: %w", httpErr))
	}
	return errors.Join(errs...)
}
