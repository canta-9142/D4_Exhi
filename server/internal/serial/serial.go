package serial

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/canta-9142/d4-exhi/internal/app"
)

const (
	MaxLineBytes = 32768
	LineTimeout  = time.Second
)

var (
	ErrLineTooLong = errors.New("serial line exceeds 32768 bytes")
	ErrLineTimeout = errors.New("serial line reception stalled for one second")
)

// Receiver decodes one session's byte stream. Its zero value awaits HELLO.
// One read goroutine owns Feed and CheckTimeout. WaitHello may be called from
// another goroutine, including app's Send callback; it never waits for a handler.
// It performs no transport I/O: the session owner closes the connection and notifies app
// when Feed or CheckTimeout returns an error. Errors are terminal; reconnects
// must use a new Receiver.
type Receiver struct {
	// Set Logger before receiving. A nil Logger logs discarded input to stdout.
	Logger *slog.Logger

	identify       atomic.Uint64
	identification uint64
	line           []byte
	lastByte       time.Time
	dropping       bool
	kind           app.DeviceKind
	err            error
}

// WaitHello schedules the requested identification generation before the next
// input or timeout check. Repeated or older generations have no effect.
// It is safe to call concurrently and does not revive a failed receiver.
func (r *Receiver) WaitHello(identification uint64) {
	for current := r.identify.Load(); current < identification; current = r.identify.Load() {
		if r.identify.CompareAndSwap(current, identification) {
			return
		}
	}
}

func (r *Receiver) applyIdentify() {
	identification := r.identify.Load()
	if identification == r.identification {
		return
	}
	r.identification = identification
	r.kind = ""
	if len(r.line) != 0 {
		r.line = r.line[:0]
		r.dropping = true
	}
}

// Feed accepts a read chunk, processing complete messages in wire order.
// handle may call WaitHello, but must not call Feed recursively. A handler error
// stops processing immediately and is terminal, just like a protocol error.
func (r *Receiver) Feed(data []byte, now time.Time, handle func(app.IncomingMessage) error) error {
	return r.feed(data, now, r.identify.Load(), handle)
}

// feed retains the generation captured by the reader, including raw chunks
// buffered before a new identify. A generation is never added at delivery time.
func (r *Receiver) feed(data []byte, now time.Time, identification uint64, handle func(app.IncomingMessage) error) error {
	if err := r.CheckTimeout(now); err != nil {
		return err
	}
	for _, b := range data {
		r.applyIdentify()
		if identification != r.identification {
			// Ignore old bytes but retain LF framing, so a trailing old fragment
			// cannot be joined to the first response in the new generation.
			r.line = r.line[:0]
			r.dropping = data[len(data)-1] != '\n'
			return nil
		}
		if b == '\n' {
			line := bytes.TrimSuffix(r.line, []byte{'\r'})
			r.line = r.line[:0]
			if r.dropping {
				r.dropping = false
				continue
			}
			if len(line) == 0 {
				continue
			}
			message, err := parseLine(line)
			if err == nil && r.kind != "" && message.Kind != "" && message.Kind != r.kind {
				err = errors.New("message uses a different device protocol")
			}
			if err != nil {
				if r.kind != "" {
					r.err = err
					return err
				}
				r.logDiscard(err)
				continue
			}
			if r.kind == "" && message.Type != "hello" {
				continue
			}
			if r.kind == app.LEDDevice && message.Type != "hello" {
				r.err = errors.New("unexpected message from LED device")
				return r.err
			}
			if message.Type == "hello" {
				r.kind = message.Kind
			}
			message.Identification = r.identification
			if err := handle(message); err != nil {
				r.err = err
				return err
			}
			continue
		}
		if r.dropping {
			continue
		}
		// Reserve one extra byte for CRLF, which does not count toward the limit.
		if len(r.line) == MaxLineBytes+1 || (len(r.line) == MaxLineBytes && b != '\r') {
			if err := r.dropLine(ErrLineTooLong); err != nil {
				return err
			}
			continue
		}
		r.line = append(r.line, b)
		r.lastByte = now
	}
	return nil
}

// CheckTimeout must also be called while reads are idle. Only an unfinished
// line has a deadline; a healthy connection may otherwise remain silent.
func (r *Receiver) CheckTimeout(now time.Time) error {
	if r.err != nil {
		return r.err
	}
	r.applyIdentify()
	if len(r.line) != 0 && now.Sub(r.lastByte) >= LineTimeout {
		return r.dropLine(ErrLineTimeout)
	}
	return nil
}

func (r *Receiver) dropLine(err error) error {
	r.line = r.line[:0]
	r.dropping = true
	if r.kind != "" {
		r.err = err
	} else {
		r.logDiscard(err)
	}
	return r.err
}

func (r *Receiver) logDiscard(err error) {
	logger := r.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	}
	logger.Warn("discarded serial input while awaiting HELLO", "error", err)
}

func parseLine(line []byte) (app.IncomingMessage, error) {
	var message app.IncomingMessage
	if !utf8.Valid(line) {
		return message, errors.New("serial line is not UTF-8")
	}
	if uid, ok := strings.CutPrefix(string(line), "HELLO "); ok {
		if uid == "" || strings.ContainsRune(uid, 0) || strings.ContainsFunc(uid, unicode.IsSpace) {
			return message, errors.New("invalid LED UID")
		}
		return app.IncomingMessage{Type: "hello", UID: uid, Kind: app.LEDDevice}, nil
	}
	var envelope, data map[string]json.RawMessage
	if err := json.Unmarshal(line, &envelope); err != nil {
		return message, fmt.Errorf("invalid serial JSON: %w", err)
	}
	if err := requiredField(envelope, "type", &message.Type); err != nil {
		return message, err
	}
	if err := requiredField(envelope, "data", &data); err != nil {
		return message, err
	}
	switch message.Type {
	case "hello":
		if err := requiredField(data, "uid", &message.UID); err != nil {
			return message, err
		}
		if !upperHex(message.UID, 12) {
			return message, errors.New("invalid player UID")
		}
		message.Kind = app.PlayerDevice
	case "send", "ready":
		if err := requiredField(data, "sync", &message.Sync); err != nil {
			return message, err
		}
		if !upperHex(message.Sync, 32) {
			return message, errors.New("invalid synchronization ID")
		}
		if message.Type == "send" {
			if err := requiredField(data, "rid", &message.RID); err != nil {
				return message, err
			}
			if err := requiredField(data, "pid", &message.PID); err != nil {
				return message, err
			}
			if err := requiredField(data, "to", &message.To); err != nil {
				return message, err
			}
			if message.To < 1 || message.To > app.NodeCount {
				return message, errors.New("invalid destination node")
			}
		}
	case "resync":
	default:
		return message, errors.New("unknown or server-only message type")
	}
	return message, nil
}

func requiredField(object map[string]json.RawMessage, name string, target any) error {
	raw := object[name]
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("missing or null %s field", name)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("invalid %s field: %w", name, err)
	}
	return nil
}

func upperHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}
