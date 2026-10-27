package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	for _, tc := range []struct {
		name      string
		args      []string
		wantError bool
	}{
		{"default", nil, false}, {"debug", []string{"-log-level", "debug"}, false},
		{"help", []string{"-h"}, false}, {"invalid level", []string{"-log-level", "invalid"}, true},
		{"unknown option", []string{"-unknown"}, true}, {"positional argument", []string{"extra"}, true},
		{"serial port", []string{"-serial-port", "/dev/ttyACM0"}, false},
		{"multiple serial ports", []string{"-serial-port", "/dev/ttyACM0", "-serial-port", "/dev/ttyACM1"}, false},
		{"empty serial port", []string{"-serial-port", " "}, true},
		{"duplicate serial port", []string{"-serial-port", "/dev/ttyACM0", "-serial-port", "/dev/./ttyACM0"}, true},
		{"list ports", []string{"-list-serial-ports"}, false},
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
