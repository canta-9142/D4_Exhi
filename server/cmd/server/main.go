package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"

	"github.com/canta-9142/d4-exhi/internal/app"
	"github.com/canta-9142/d4-exhi/internal/serial"
)

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
	transport := serial.New(logger)
	application := app.New(app.Options{Logger: logger, Send: transport.Send, Close: transport.Close})
	logger.Info("server starting", "phase", app.Preparing, "serial_ports", ports)
	portsCtx, stopPorts := context.WithCancel(ctx)
	defer stopPorts()
	// Keep app alive to receive Disconnected while serial workers shut down.
	appCtx, stopApp := context.WithCancel(context.WithoutCancel(ctx))
	defer stopApp()
	appDone := make(chan error, 1)
	go func() { appDone <- application.Run(appCtx) }()
	var workers sync.WaitGroup
	for _, name := range ports {
		workers.Add(1)
		go func() { defer workers.Done(); transport.RunPort(portsCtx, application, name) }()
	}
	var err error
	appStopped := false
	select {
	case <-ctx.Done():
	case err = <-appDone:
		appStopped = true
	}
	stopPorts()
	workers.Wait()
	stopApp()
	if !appStopped {
		err = <-appDone
	}
	logger.Info("server stopped")
	return err
}
