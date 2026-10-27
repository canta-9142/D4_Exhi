package serial

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/canta-9142/d4-exhi/internal/app"
)

const (
	playerHello = `{"type":"hello","data":{"uid":"001122AABBCC"}}`
	generation  = "0123456789ABCDEF0123456789ABCDEF"
	readyLine   = `{"type":"ready","data":{"sync":"` + generation + `"}}`
	sendLine    = `{"type":"send","data":{"sync":"` + generation + `","rid":0,"pid":0,"to":1}}`
	resyncLine  = `{"type":"resync","data":{}}`
)

var playerIdentity = app.IncomingMessage{Type: "hello", UID: "001122AABBCC", Kind: app.PlayerDevice}

func TestParseMessages(t *testing.T) {
	for _, tt := range []struct {
		line string
		want app.IncomingMessage
	}{
		{playerHello, playerIdentity},
		{"HELLO AABBCCDDEEFF", app.IncomingMessage{Type: "hello", UID: "AABBCCDDEEFF", Kind: app.LEDDevice}},
		{" \tHELLO\t  AABBCCDDEEFF\t ", app.IncomingMessage{Type: "hello", UID: "AABBCCDDEEFF", Kind: app.LEDDevice}},
		{resyncLine, app.IncomingMessage{Type: "resync"}},
		{readyLine, app.IncomingMessage{Type: "ready", Sync: generation}},
		{sendLine, app.IncomingMessage{Type: "send", Sync: generation, To: 1}},
		{`{"type":"send","data":{"sync":"` + generation + `","rid":4294967295,"pid":65535,"to":11}}`, app.IncomingMessage{Type: "send", Sync: generation, RID: 4294967295, PID: 65535, To: 11}},
		{`{"type":"resync","data":{"uid":null,"rid":"ignored","extra":[1]},"extra":true}`, app.IncomingMessage{Type: "resync"}},
		{`{"type":"hello","data":{"uid":"001122AABBCC","sync":false}}`, playerIdentity},
	} {
		t.Run(tt.line, func(t *testing.T) {
			got, err := parseLine([]byte(tt.line))
			if err != nil || got != tt.want {
				t.Fatalf("parseLine = %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}
}

func TestInvalidMessages(t *testing.T) {
	invalid := []string{
		"HELLO led-01", "HELLO aabbccddeeff", "HELLO AABBCCDDEEF", "HELLO AABBCCDDEEFF extra",
		"HELLO\vAABBCCDDEEFF", "HELLO AABBCCDDEEFF\r", "HELLO AABBCCDDEEFF\x7f",
		strings.Repeat(" ", MaxLEDLineBytes) + "HELLO AABBCCDDEEFF",
		`null`, `[]`, `1`, `"hello"`, `{`, playerHello + resyncLine,
		`{}`, `{"Type":"hello","data":{"uid":"001122AABBCC"}}`,
		`{"type":null,"data":{}}`, `{"type":1,"data":{}}`,
		`{"type":"hello"}`, `{"type":"resync","data":null}`,
		`{"type":"resync","data":[]}`, `{"type":"resync","data":"{}"}`,
		`{"type":"hello","data":{"UID":"001122AABBCC"}}`,
		`{"type":"hello","data":{"uid":"001122aabbcc"}}`,
		`{"type":"hello","data":{"uid":"001122AABBC"}}`,
		`{"type":"hello","data":{"uid":"001122AABBCG"}}`,
		`{"type":"ready","data":{"sync":"0123456789abcdef0123456789abcdef"}}`,
		`{"type":"ready","data":{"sync":"0123456789ABCDEF"}}`,
		`{"type":"ready","data":{"sync":"0123456789ABCDEF0123456789ABCDEG"}}`,
		`{"type":"unknown","data":{}}`, `{"type":"SEND","data":{}}`,
		`{"type":"identify","data":{}}`, `{"type":"config","data":{}}`,
		`{"type":"reset","data":{}}`, `{"type":"push","data":{}}`,
		`{"type":"result","data":{}}`, `{"type":"phase","data":{}}`,
		`{"type":"status","data":{}}`,
		"HELLO", "HELLO ", "HELLO led extra", "HELLO led\t01", "HELLO led\x00", "hello led",
		playerHello[:len(playerHello)-1] + string([]byte{0xff}) + "}", "HELLO led\xff",
	}
	// Zero is valid for RID and PID, so absence/null must not become zero.
	for _, original := range []string{playerHello, readyLine, sendLine} {
		var envelope map[string]any
		if err := json.Unmarshal([]byte(original), &envelope); err != nil {
			t.Fatal(err)
		}
		data := envelope["data"].(map[string]any)
		for _, field := range []string{"uid", "sync", "rid", "pid", "to"} {
			value, exists := data[field]
			if !exists {
				continue
			}
			delete(data, field)
			encoded, _ := json.Marshal(envelope)
			invalid = append(invalid, string(encoded))
			for _, bad := range []any{nil, true, map[string]any{}, []any{}, 1.5} {
				data[field] = bad
				encoded, _ = json.Marshal(envelope)
				invalid = append(invalid, string(encoded))
			}
			data[field] = value
		}
	}
	for _, tt := range []struct {
		field string
		value string
	}{
		{"rid", "-1"}, {"rid", "4294967296"}, {"rid", `"0"`}, {"rid", "0.0"}, {"rid", "1e0"},
		{"pid", "-1"}, {"pid", "65536"}, {"pid", `"0"`}, {"pid", "0.0"},
		{"to", "0"}, {"to", "12"}, {"to", "256"}, {"to", "-1"}, {"to", `"1"`},
	} {
		var envelope map[string]json.RawMessage
		var data map[string]json.RawMessage
		_ = json.Unmarshal([]byte(sendLine), &envelope)
		_ = json.Unmarshal(envelope["data"], &data)
		data[tt.field] = json.RawMessage(tt.value)
		envelope["data"], _ = json.Marshal(data)
		encoded, _ := json.Marshal(envelope)
		invalid = append(invalid, string(encoded))
	}
	for _, line := range invalid {
		t.Run(line, func(t *testing.T) {
			if message, err := parseLine([]byte(line)); err == nil {
				t.Fatalf("accepted invalid input: %+v", message)
			}
		})
	}
}

func TestReceiverFramingAndIdentification(t *testing.T) {
	var receiver Receiver
	var got []app.IncomingMessage
	handle := func(message app.IncomingMessage) error { got = append(got, message); return nil }
	now := time.Unix(0, 0)
	for _, chunk := range []string{
		"\n\r\ninvalid\n" + sendLine + "\n" + readyLine + "\n" + resyncLine + "\n",
		playerHello[:15], playerHello[15:] + "\r", "\n" + readyLine + "\n" + sendLine[:20],
		sendLine[20:] + "\n" + playerHello + "\n" + resyncLine + "\n",
	} {
		if err := receiver.Feed([]byte(chunk), now, handle); err != nil {
			t.Fatal(err)
		}
		now = now.Add(500 * time.Millisecond)
	}
	want := []app.IncomingMessage{
		playerIdentity,
		{Type: "ready", Sync: generation},
		{Type: "send", Sync: generation, To: 1},
		playerIdentity,
		{Type: "resync"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("received %+v; want %+v", got, want)
	}
}

func TestReceiverErrorsAreTerminal(t *testing.T) {
	for _, tt := range []struct{ hello, invalid string }{
		{playerHello, "not JSON"},
		{playerHello, `{"type":"ready","data":{}}`},
		{playerHello, "HELLO AABBCCDDEEFF"},
		{"HELLO AABBCCDDEEFF", playerHello},
		{"HELLO AABBCCDDEEFF", sendLine},
	} {
		t.Run(tt.hello+tt.invalid, func(t *testing.T) {
			var receiver Receiver
			calls := 0
			handle := func(app.IncomingMessage) error { calls++; return nil }
			err := receiver.Feed([]byte(tt.hello+"\n"+tt.invalid+"\n"+tt.hello+"\n"), time.Time{}, handle)
			if err == nil || calls != 1 {
				t.Fatalf("error = %v, handler calls = %d; want an error after HELLO", err, calls)
			}
			receiver.WaitHello(1)
			if got := receiver.Feed([]byte(tt.hello+"\n"), time.Time{}, handle); got != err || calls != 1 {
				t.Fatalf("failed receiver resumed: %v, calls = %d", got, calls)
			}
		})
	}
	var receiver Receiver
	want := errors.New("handler stopped")
	calls := 0
	err := receiver.Feed([]byte(playerHello+"\n"+resyncLine+"\n"), time.Time{}, func(app.IncomingMessage) error {
		calls++
		return want
	})
	if err != want || calls != 1 || receiver.CheckTimeout(time.Time{}) != want {
		t.Fatalf("handler error = %v, calls = %d", err, calls)
	}
}

func TestReceiverLineLimit(t *testing.T) {
	for _, identified := range []bool{false, true} {
		for _, ending := range []string{"\n", "\r\n"} {
			for _, length := range []int{MaxLineBytes - 1, MaxLineBytes, MaxLineBytes + 1} {
				t.Run(fmt.Sprintf("identified=%t/ending=%q/bytes=%d", identified, ending, length), func(t *testing.T) {
					var receiver Receiver
					calls := 0
					handle := func(app.IncomingMessage) error { calls++; return nil }
					if identified {
						if err := receiver.Feed([]byte(playerHello+"\n"), time.Time{}, handle); err != nil {
							t.Fatal(err)
						}
						calls = 0
					}
					line := playerHello + strings.Repeat(" ", length-len(playerHello))
					// Exercise CR and LF arriving in separate reads at the exact limit.
					err := receiver.Feed([]byte(line+strings.TrimSuffix(ending, "\n")), time.Time{}, handle)
					if err == nil {
						err = receiver.Feed([]byte("\n"+playerHello+"\n"), time.Time{}, handle)
					}
					if length > MaxLineBytes && identified {
						if !errors.Is(err, ErrLineTooLong) || calls != 0 {
							t.Fatalf("error = %v, calls = %d", err, calls)
						}
						return
					}
					want := 2
					if length > MaxLineBytes {
						want = 1
					}
					if err != nil || calls != want {
						t.Fatalf("error = %v, calls = %d; want %d", err, calls, want)
					}
				})
			}
		}
	}
	// A CR inside the payload counts; only the CR immediately before LF is free.
	var receiver Receiver
	calls := 0
	input := strings.Repeat("x", MaxLineBytes) + "\r\r\n" + "HELLO AABBCCDDEEFF\n"
	if err := receiver.Feed([]byte(input), time.Time{}, func(app.IncomingMessage) error { calls++; return nil }); err != nil || calls != 1 {
		t.Fatalf("oversized CR payload was not discarded: %v, calls = %d", err, calls)
	}
}

func TestReceiverTimeout(t *testing.T) {
	for _, identified := range []bool{false, true} {
		for _, explicitCheck := range []bool{false, true} {
			t.Run(fmt.Sprintf("identified=%t/check=%t", identified, explicitCheck), func(t *testing.T) {
				var receiver Receiver
				now := time.Unix(0, 0)
				calls := 0
				handle := func(app.IncomingMessage) error { calls++; return nil }
				if identified {
					if err := receiver.Feed([]byte(playerHello+"\n"), now, handle); err != nil {
						t.Fatal(err)
					}
				}
				if err := receiver.CheckTimeout(now.Add(time.Hour)); err != nil {
					t.Fatalf("idle connection timed out: %v", err)
				}
				calls = 0
				now = now.Add(time.Hour)
				if err := receiver.Feed([]byte("old"), now, handle); err != nil {
					t.Fatal(err)
				}
				if err := receiver.CheckTimeout(now.Add(500*time.Millisecond - time.Nanosecond)); err != nil {
					t.Fatalf("early timeout: %v", err)
				}
				// Refresh the inactivity deadline; total line duration may exceed 1s.
				now = now.Add(500 * time.Millisecond)
				if err := receiver.Feed([]byte(" tail"), now, handle); err != nil {
					t.Fatal(err)
				}
				if err := receiver.CheckTimeout(now.Add(LineTimeout - time.Nanosecond)); err != nil {
					t.Fatalf("deadline was not refreshed: %v", err)
				}
				now = now.Add(LineTimeout)
				var err error
				if explicitCheck {
					err = receiver.CheckTimeout(now)
				} else {
					err = receiver.Feed([]byte(playerHello+"\n"), now, handle)
				}
				if identified {
					if !errors.Is(err, ErrLineTimeout) || calls != 0 {
						t.Fatalf("timeout = %v, calls = %d", err, calls)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if explicitCheck {
					// Even a valid HELLO in the discarded line's tail must be ignored.
					if err := receiver.Feed([]byte(playerHello+"\n"), now, handle); err != nil {
						t.Fatal(err)
					}
				}
				if calls != 0 {
					t.Fatal("accepted the timed-out line's tail")
				}
				if err := receiver.Feed([]byte(playerHello+"\n"), now, handle); err != nil || calls != 1 {
					t.Fatalf("did not recover after LF: %v, calls = %d", err, calls)
				}
			})
		}
	}
}

func TestReceiverReidentification(t *testing.T) {
	var receiver Receiver
	now := time.Unix(0, 0)
	var got []string
	handle := func(message app.IncomingMessage) error {
		got = append(got, message.Type)
		if message.Type == "resync" {
			// app may request identify while handling an incoming message.
			receiver.WaitHello(2)
		}
		return nil
	}
	if err := receiver.Feed([]byte(playerHello+"\nold partial"), now, handle); err != nil {
		t.Fatal(err)
	}
	receiver.WaitHello(1)
	input := playerHello + "\ninvalid\n" + sendLine + "\n" + playerHello + "\n" + resyncLine + "\n" + readyLine + "\n" + playerHello + "\n"
	if err := receiver.Feed([]byte(input), now, handle); err != nil {
		t.Fatal(err)
	}
	if err := receiver.Feed([]byte(playerHello+"\n"), now, handle); err != nil {
		t.Fatal(err)
	}
	if want := []string{"hello", "hello", "resync", "hello"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("received %v; want %v", got, want)
	}
}

func TestReceiverIdentifyRetriesPreserveHello(t *testing.T) {
	for _, line := range []string{playerHello, "HELLO AABBCCDDEEFF"} {
		t.Run(line, func(t *testing.T) {
			var receiver Receiver
			var got []app.IncomingMessage
			handle := func(message app.IncomingMessage) error { got = append(got, message); return nil }
			now := time.Unix(0, 0)
			// The response spans more than one retry period, without a 1s gap.
			for i, chunk := range []string{line[:5], line[5:10], line[10:] + "\n"} {
				request := app.Message{Type: "identify", Data: struct{}{}, BeginIdentification: i == 0, Identification: 1}
				if request.BeginIdentification {
					receiver.WaitHello(request.Identification)
				}
				if err := receiver.CheckTimeout(now); err != nil {
					t.Fatal(err)
				}
				if err := receiver.Feed([]byte(chunk), now, handle); err != nil {
					t.Fatal(err)
				}
				now = now.Add(600 * time.Millisecond)
			}
			want, err := parseLine([]byte(line))
			if err != nil {
				t.Fatal(err)
			}
			want.Identification = 1
			if !reflect.DeepEqual(got, []app.IncomingMessage{want}) {
				t.Fatalf("retry discarded HELLO: received %+v; want %+v", got, want)
			}
		})
	}
}

func TestReceiverAppReidentification(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	receiver := Receiver{Logger: logger}
	a := app.New(app.Options{
		Logger: logger,
		Send: func(_ app.SessionID, message app.Message) error {
			if message.BeginIdentification {
				// This runs on app's goroutine while Feed waits for the handler.
				receiver.WaitHello(message.Identification)
			}
			return nil
		},
	})
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(time.Second):
			t.Error("app did not stop")
		}
	})
	const session = app.SessionID("receiver-test")
	if err := a.Connected(ctx, session); err != nil {
		t.Fatal(err)
	}
	var got []string
	input := playerHello + "\n" + resyncLine + "\n" + readyLine + "\n" + playerHello + "\n"
	handle := func(message app.IncomingMessage) error {
		got = append(got, message.Type)
		if err := a.MessageReceived(ctx, session, message); err != nil {
			return err
		}
		if message.Type == "resync" {
			// Removing the assignment makes app send identify before returning.
			return a.Execute(ctx, app.Command{Operation: app.SetDeviceAssignment, DeviceID: playerIdentity.UID})
		}
		return nil
	}
	err := receiver.Feed([]byte(input), time.Now(), handle)
	if err != nil {
		t.Fatal(err)
	}
	if err := receiver.Feed([]byte(playerHello+"\n"), time.Now(), handle); err != nil {
		t.Fatal(err)
	}
	if want := []string{"hello", "resync", "hello"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("received %v; want %v (old READY must be discarded)", got, want)
	}
}

func TestReceiverIdentifyRetryBeforeHelloAccepted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	receiver := Receiver{Logger: logger}
	identifies := make(chan app.Message, 8)
	a := app.New(app.Options{
		Logger: logger,
		Send: func(_ app.SessionID, message app.Message) error {
			if message.Type == "identify" {
				if message.BeginIdentification {
					receiver.WaitHello(message.Identification)
				}
				select {
				case identifies <- message:
				default:
					return errors.New("test identify queue is full")
				}
			}
			return nil
		},
	})
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(time.Second):
			t.Error("app did not stop")
		}
	})
	const session = app.SessionID("hello-retry-test")
	if err := a.Connected(ctx, session); err != nil {
		t.Fatal(err)
	}
	if initial := <-identifies; !initial.BeginIdentification {
		t.Fatal("initial identify did not start identification")
	}
	var got []string
	err := receiver.Feed([]byte(playerHello+"\n"+resyncLine+"\n"), time.Now(), func(message app.IncomingMessage) error {
		got = append(got, message.Type)
		if message.Type == "hello" {
			// Receiver has parsed HELLO, but app still awaits it. Hold delivery
			// until app's real ticker sends a retry, fixing the event order.
			select {
			case retry := <-identifies:
				if retry.BeginIdentification {
					return errors.New("identify retry was marked as a new identification")
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return a.MessageReceived(ctx, session, message)
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"hello", "resync"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("retry reset the accepted HELLO: received %v; want %v", got, want)
	}
}

func TestReceiverConcurrentIdentify(t *testing.T) {
	receiver := Receiver{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 10000 {
			receiver.WaitHello(uint64(i + 1))
		}
	}()
	defer func() { <-done }()
	handle := func(app.IncomingMessage) error { return nil }
	for range 1000 {
		if err := receiver.Feed([]byte(playerHello+"\n"+resyncLine+"\n"), time.Time{}, handle); err != nil {
			t.Fatal(err)
		}
		if err := receiver.CheckTimeout(time.Time{}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReceiverLogsDiscardReasons(t *testing.T) {
	for _, reason := range []string{"invalid serial JSON", ErrLineTooLong.Error(), ErrLineTimeout.Error()} {
		t.Run(reason, func(t *testing.T) {
			var output bytes.Buffer
			receiver := Receiver{Logger: slog.New(slog.NewTextHandler(&output, nil)).With("session", "test-session")}
			handle := func(app.IncomingMessage) error { return nil }
			now := time.Unix(0, 0)
			if err := receiver.Feed([]byte(sendLine+"\n"), now, handle); err != nil || output.Len() != 0 {
				t.Fatalf("old SEND was treated as malformed input: %v, logs = %q", err, output.String())
			}
			input := "invalid\n"
			if reason == ErrLineTooLong.Error() {
				input = strings.Repeat("x", MaxLineBytes+1)
			} else if reason == ErrLineTimeout.Error() {
				input = "{"
			}
			if err := receiver.Feed([]byte(input), now, handle); err != nil {
				t.Fatal(err)
			}
			for _, elapsed := range []time.Duration{LineTimeout, 2 * LineTimeout} {
				if err := receiver.CheckTimeout(now.Add(elapsed)); err != nil {
					t.Fatal(err)
				}
			}
			if reason != "invalid serial JSON" {
				if err := receiver.Feed([]byte("discarded tail\n"), now.Add(2*LineTimeout), handle); err != nil {
					t.Fatal(err)
				}
			}
			log := output.String()
			if !strings.Contains(log, reason) || !strings.Contains(log, "session=test-session") || strings.Count(log, "level=WARN") != 1 {
				t.Fatalf("want one session-tagged discard log containing %q; got %q", reason, log)
			}
			calls := 0
			if err := receiver.Feed([]byte(playerHello+"\n"), now.Add(2*LineTimeout), func(app.IncomingMessage) error { calls++; return nil }); err != nil || calls != 1 {
				t.Fatalf("did not recover after logging: %v, calls = %d", err, calls)
			}
		})
	}
}

func TestLEDLineLimitAndReidentification(t *testing.T) {
	const hello = "HELLO AABBCCDDEEFF"
	for _, reidentify := range []bool{false, true} {
		t.Run(fmt.Sprint(reidentify), func(t *testing.T) {
			receiver := Receiver{Logger: slog.New(slog.DiscardHandler)}
			receiver.WaitHello(1)
			calls := 0
			handle := func(app.IncomingMessage) error { calls++; return nil }
			boundary := strings.Repeat(" ", MaxLEDLineBytes-len(hello)) + hello
			if err := receiver.Feed([]byte(boundary+"\r\n \t\n"), time.Now(), handle); err != nil || calls != 1 {
				t.Fatalf("valid LED boundary/blank line: calls=%d, %v", calls, err)
			}
			if reidentify {
				receiver.WaitHello(2)
			}
			err := receiver.Feed([]byte(strings.Repeat("x", MaxLEDLineBytes+1)), time.Now(), handle)
			if reidentify {
				if err != nil || len(receiver.line) != 0 || !receiver.dropping {
					t.Fatalf("LED limit was lost while awaiting HELLO: %v", err)
				}
				if err := receiver.Feed([]byte("\n"+hello+"\n"), time.Now(), handle); err != nil || calls != 2 {
					t.Fatalf("LED did not recover after oversized input: calls=%d, %v", calls, err)
				}
			} else if !errors.Is(err, ErrLineTooLong) {
				t.Fatalf("identified LED exceeded 96-byte limit: %v", err)
			}
		})
	}
}
