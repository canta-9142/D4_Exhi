package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/canta-9142/d4-exhi/internal/app"
)

// validCommands are requests of web.md 4.1 and the commands they become.
var validCommands = []struct {
	name string
	body string
	want app.Command
}{
	{"start", `{"type":"start","data":{}}`, app.Command{Operation: app.Start}},
	{"finish", `{"type":"finish","data":{}}`, app.Command{Operation: app.Finish}},
	{"reset", `{"type":"reset","data":{}}`, app.Command{Operation: app.Reset}},
	{"whitespace and key order", " \r\n\t{ \"data\" : { } , \"type\" : \"start\" }\n", app.Command{Operation: app.Start}},
	{"escaped key and value", `{"type":"start","data":{}}`, app.Command{Operation: app.Start}},
	{"manual", `{"type":"set_node_mode","data":{"nodeId":"P1","mode":"manual"}}`,
		app.Command{Operation: app.SetNodeEnabled, Node: 1, Enabled: true}},
	{"auto", `{"type":"set_node_mode","data":{"mode":"auto","nodeId":"P8"}}`,
		app.Command{Operation: app.SetNodeEnabled, Node: 8}},
	{"fractional rate", `{"type":"set_bot_rate","data":{"nodeId":"B1","ratePps":1.5}}`,
		app.Command{Operation: app.SetBotRate, Node: 9, Rate: 1.5}},
	{"integer rate", `{"type":"set_bot_rate","data":{"nodeId":"B3","ratePps":2}}`,
		app.Command{Operation: app.SetBotRate, Node: 11, Rate: 2}},
	{"exponent rate", `{"type":"set_bot_rate","data":{"nodeId":"B2","ratePps":1e-3}}`,
		app.Command{Operation: app.SetBotRate, Node: 10, Rate: 0.001}},
	{"assign player", `{"type":"set_device_assignment","data":{"deviceId":"AABBCCDDEEFF","target":{"kind":"player","nodeId":"P8"}}}`,
		app.Command{Operation: app.SetDeviceAssignment, DeviceID: "AABBCCDDEEFF", Node: 8, Kind: app.PlayerDevice}},
	{"assign LED to player node", `{"type":"set_device_assignment","data":{"deviceId":"001122334455","target":{"kind":"led","nodeId":"P7"}}}`,
		app.Command{Operation: app.SetDeviceAssignment, DeviceID: "001122334455", Node: 7, Kind: app.LEDDevice}},
	{"assign LED to bot node", `{"type":"set_device_assignment","data":{"target":{"nodeId":"B2","kind":"led"},"deviceId":"001122334455"}}`,
		app.Command{Operation: app.SetDeviceAssignment, DeviceID: "001122334455", Node: 10, Kind: app.LEDDevice}},
	{"unassign", `{"type":"set_device_assignment","data":{"deviceId":"AABBCCDDEEFF","target":null}}`,
		app.Command{Operation: app.SetDeviceAssignment, DeviceID: "AABBCCDDEEFF"}},
	{"swap", `{"type":"swap_device_assignments","data":{"firstDeviceId":"AABBCCDDEEFF","secondDeviceId":"112233445566"}}`,
		app.Command{Operation: app.SwapDeviceAssignments, DeviceID: "AABBCCDDEEFF", OtherDeviceID: "112233445566"}},
	{"replace", `{"type":"replace_device_assignment","data":{"oldDeviceId":"AABBCCDDEEFF","newDeviceId":"112233445566"}}`,
		app.Command{Operation: app.ReplaceDeviceAssignment, DeviceID: "AABBCCDDEEFF", OtherDeviceID: "112233445566"}},
	{"preview", `{"type":"set_device_preview","data":{"deviceId":"001122334455"}}`,
		app.Command{Operation: app.SetDevicePreview, DeviceID: "001122334455"}},
	{"stop preview", `{"type":"set_device_preview","data":{"deviceId":null}}`,
		app.Command{Operation: app.SetDevicePreview}},
	// Device IDs are compared as given (web.md 2).
	{"Device ID as given", `{"type":"set_device_preview","data":{"deviceId":" aabbccddeeff "}}`,
		app.Command{Operation: app.SetDevicePreview, DeviceID: " aabbccddeeff "}},
	{"Device IDs differing in case", `{"type":"swap_device_assignments","data":{"firstDeviceId":"aabbccddeeff","secondDeviceId":"AABBCCDDEEFF"}}`,
		app.Command{Operation: app.SwapDeviceAssignments, DeviceID: "aabbccddeeff", OtherDeviceID: "AABBCCDDEEFF"}},
}

// invalidCommands are requests web.md 4.1 rejects with 400 invalid_request.
var invalidCommands = []struct{ name, body string }{
	// Body
	{"empty body", ``},
	{"whitespace only", " \n"},
	{"not JSON", `start`},
	{"truncated", `{"type":"start","data":{}`},
	{"array", `[{"type":"start","data":{}}]`},
	{"null", `null`},
	{"string", `"start"`},
	{"number", `1`},
	{"value after command", `{"type":"start","data":{}}{"type":"start","data":{}}`},
	{"null after command", `{"type":"start","data":{}} null`},
	{"garbage after command", `{"type":"start","data":{}}x`},
	{"bracket after command", `{"type":"start","data":{}}]`},
	{"invalid UTF-8", "{\"type\":\"set_device_preview\",\"data\":{\"deviceId\":\"\xff\"}}"},

	// Top level
	{"empty object", `{}`},
	{"data missing", `{"type":"start"}`},
	{"type missing", `{"data":{}}`},
	{"capitalized type key", `{"Type":"start","data":{}}`},
	{"upper-case data key", `{"type":"start","DATA":{}}`},
	{"type in two cases", `{"type":"start","Type":"start","data":{}}`},
	{"unknown top-level field", `{"type":"start","data":{},"id":1}`},
	{"null type", `{"type":null,"data":{}}`},
	{"number type", `{"type":1,"data":{}}`},
	{"empty type", `{"type":"","data":{}}`},
	{"unknown type", `{"type":"stop","data":{}}`},
	{"capitalized type", `{"type":"Start","data":{}}`},

	// start, finish, reset
	{"start with argument", `{"type":"start","data":{"force":true}}`},
	{"finish with null data", `{"type":"finish","data":null}`},
	{"reset with array data", `{"type":"reset","data":[]}`},
	{"start with string data", `{"type":"start","data":""}`},

	// set_node_mode
	{"mode missing", `{"type":"set_node_mode","data":{"nodeId":"P1"}}`},
	{"mode nodeId missing", `{"type":"set_node_mode","data":{"mode":"manual"}}`},
	{"mode with unknown field", `{"type":"set_node_mode","data":{"nodeId":"P1","mode":"manual","ready":true}}`},
	{"capitalized nodeId key", `{"type":"set_node_mode","data":{"NodeId":"P1","mode":"manual"}}`},
	{"mode for bot", `{"type":"set_node_mode","data":{"nodeId":"B1","mode":"auto"}}`},
	{"mode for P0", `{"type":"set_node_mode","data":{"nodeId":"P0","mode":"auto"}}`},
	{"mode for P9", `{"type":"set_node_mode","data":{"nodeId":"P9","mode":"auto"}}`},
	{"lower-case nodeId", `{"type":"set_node_mode","data":{"nodeId":"p1","mode":"auto"}}`},
	{"zero-padded nodeId", `{"type":"set_node_mode","data":{"nodeId":"P01","mode":"auto"}}`},
	{"space in nodeId", `{"type":"set_node_mode","data":{"nodeId":" P1","mode":"auto"}}`},
	{"empty nodeId", `{"type":"set_node_mode","data":{"nodeId":"","mode":"auto"}}`},
	{"null nodeId", `{"type":"set_node_mode","data":{"nodeId":null,"mode":"auto"}}`},
	{"internal nodeId", `{"type":"set_node_mode","data":{"nodeId":1,"mode":"auto"}}`},
	{"capitalized mode", `{"type":"set_node_mode","data":{"nodeId":"P1","mode":"Manual"}}`},
	{"unknown mode", `{"type":"set_node_mode","data":{"nodeId":"P1","mode":"enabled"}}`},
	{"empty mode", `{"type":"set_node_mode","data":{"nodeId":"P1","mode":""}}`},
	{"null mode", `{"type":"set_node_mode","data":{"nodeId":"P1","mode":null}}`},
	{"boolean mode", `{"type":"set_node_mode","data":{"nodeId":"P1","mode":true}}`},

	// set_bot_rate
	{"rate for player", `{"type":"set_bot_rate","data":{"nodeId":"P1","ratePps":1}}`},
	{"rate for B0", `{"type":"set_bot_rate","data":{"nodeId":"B0","ratePps":1}}`},
	{"rate for B4", `{"type":"set_bot_rate","data":{"nodeId":"B4","ratePps":1}}`},
	{"rate for lower-case bot", `{"type":"set_bot_rate","data":{"nodeId":"b1","ratePps":1}}`},
	{"string rate", `{"type":"set_bot_rate","data":{"nodeId":"B1","ratePps":"1.5"}}`},
	{"zero rate", `{"type":"set_bot_rate","data":{"nodeId":"B1","ratePps":0}}`},
	{"negative zero rate", `{"type":"set_bot_rate","data":{"nodeId":"B1","ratePps":-0}}`},
	{"negative rate", `{"type":"set_bot_rate","data":{"nodeId":"B1","ratePps":-1}}`},
	{"null rate", `{"type":"set_bot_rate","data":{"nodeId":"B1","ratePps":null}}`},
	{"boolean rate", `{"type":"set_bot_rate","data":{"nodeId":"B1","ratePps":true}}`},
	{"overflowing rate", `{"type":"set_bot_rate","data":{"nodeId":"B1","ratePps":1e400}}`},
	{"rate missing", `{"type":"set_bot_rate","data":{"nodeId":"B1"}}`},
	{"capitalized rate key", `{"type":"set_bot_rate","data":{"nodeId":"B1","ratePPS":1}}`},

	// set_device_assignment
	{"assign empty deviceId", `{"type":"set_device_assignment","data":{"deviceId":"","target":null}}`},
	{"assign null deviceId", `{"type":"set_device_assignment","data":{"deviceId":null,"target":null}}`},
	{"assign numeric deviceId", `{"type":"set_device_assignment","data":{"deviceId":1,"target":null}}`},
	{"assign deviceId missing", `{"type":"set_device_assignment","data":{"target":null}}`},
	{"target missing", `{"type":"set_device_assignment","data":{"deviceId":"AABBCCDDEEFF"}}`},
	{"empty target", `{"type":"set_device_assignment","data":{"deviceId":"AABBCCDDEEFF","target":{}}}`},
	{"target kind missing", `{"type":"set_device_assignment","data":{"deviceId":"AABBCCDDEEFF","target":{"nodeId":"P1"}}}`},
	{"target nodeId missing", `{"type":"set_device_assignment","data":{"deviceId":"AABBCCDDEEFF","target":{"kind":"player"}}}`},
	{"target with unknown field", `{"type":"set_device_assignment","data":{"deviceId":"AABBCCDDEEFF","target":{"kind":"player","nodeId":"P1","preview":false}}}`},
	{"capitalized target kind", `{"type":"set_device_assignment","data":{"deviceId":"AABBCCDDEEFF","target":{"kind":"Player","nodeId":"P1"}}}`},
	{"bot target kind", `{"type":"set_device_assignment","data":{"deviceId":"AABBCCDDEEFF","target":{"kind":"bot","nodeId":"B1"}}}`},
	{"null target kind", `{"type":"set_device_assignment","data":{"deviceId":"AABBCCDDEEFF","target":{"kind":null,"nodeId":"P1"}}}`},
	{"null target nodeId", `{"type":"set_device_assignment","data":{"deviceId":"AABBCCDDEEFF","target":{"kind":"player","nodeId":null}}}`},
	{"player target B1", `{"type":"set_device_assignment","data":{"deviceId":"AABBCCDDEEFF","target":{"kind":"player","nodeId":"B1"}}}`},
	{"player target P9", `{"type":"set_device_assignment","data":{"deviceId":"AABBCCDDEEFF","target":{"kind":"player","nodeId":"P9"}}}`},
	{"LED target P2", `{"type":"set_device_assignment","data":{"deviceId":"001122334455","target":{"kind":"led","nodeId":"P2"}}}`},
	{"LED target P8", `{"type":"set_device_assignment","data":{"deviceId":"001122334455","target":{"kind":"led","nodeId":"P8"}}}`},
	{"LED target B4", `{"type":"set_device_assignment","data":{"deviceId":"001122334455","target":{"kind":"led","nodeId":"B4"}}}`},
	{"array target", `{"type":"set_device_assignment","data":{"deviceId":"AABBCCDDEEFF","target":[]}}`},
	{"string target", `{"type":"set_device_assignment","data":{"deviceId":"AABBCCDDEEFF","target":"P1"}}`},

	// swap_device_assignments
	{"swap same device", `{"type":"swap_device_assignments","data":{"firstDeviceId":"AABBCCDDEEFF","secondDeviceId":"AABBCCDDEEFF"}}`},
	{"swap empty first", `{"type":"swap_device_assignments","data":{"firstDeviceId":"","secondDeviceId":"112233445566"}}`},
	{"swap empty second", `{"type":"swap_device_assignments","data":{"firstDeviceId":"AABBCCDDEEFF","secondDeviceId":""}}`},
	{"swap null first", `{"type":"swap_device_assignments","data":{"firstDeviceId":null,"secondDeviceId":"112233445566"}}`},
	{"swap second missing", `{"type":"swap_device_assignments","data":{"firstDeviceId":"AABBCCDDEEFF"}}`},
	{"swap with replace keys", `{"type":"swap_device_assignments","data":{"oldDeviceId":"AABBCCDDEEFF","newDeviceId":"112233445566"}}`},
	{"swap with unknown field", `{"type":"swap_device_assignments","data":{"firstDeviceId":"AABBCCDDEEFF","secondDeviceId":"112233445566","force":true}}`},

	// replace_device_assignment
	{"replace same device", `{"type":"replace_device_assignment","data":{"oldDeviceId":"AABBCCDDEEFF","newDeviceId":"AABBCCDDEEFF"}}`},
	{"replace empty new", `{"type":"replace_device_assignment","data":{"oldDeviceId":"AABBCCDDEEFF","newDeviceId":""}}`},
	{"replace null old", `{"type":"replace_device_assignment","data":{"oldDeviceId":null,"newDeviceId":"112233445566"}}`},
	{"replace numeric new", `{"type":"replace_device_assignment","data":{"oldDeviceId":"AABBCCDDEEFF","newDeviceId":112233445566}}`},
	{"replace with swap keys", `{"type":"replace_device_assignment","data":{"firstDeviceId":"AABBCCDDEEFF","secondDeviceId":"112233445566"}}`},

	// set_device_preview
	{"preview empty deviceId", `{"type":"set_device_preview","data":{"deviceId":""}}`},
	{"preview numeric deviceId", `{"type":"set_device_preview","data":{"deviceId":1}}`},
	{"preview deviceId missing", `{"type":"set_device_preview","data":{}}`},
	{"preview with unknown field", `{"type":"set_device_preview","data":{"deviceId":null,"nodeId":"P1"}}`},
	{"preview null data", `{"type":"set_device_preview","data":null}`},
}

func TestDecodeCommand(t *testing.T) {
	for _, tc := range validCommands {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeCommand(strings.NewReader(tc.body))
			if err != nil {
				t.Fatalf("decodeCommand(%s): %v", tc.body, err)
			}
			if got != tc.want {
				t.Errorf("decodeCommand(%s) = %+v, want %+v", tc.body, got, tc.want)
			}
		})
	}
}

func TestDecodeCommandInvalid(t *testing.T) {
	for _, tc := range invalidCommands {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := decodeCommand(strings.NewReader(tc.body)); err == nil {
				t.Errorf("decodeCommand(%s) = %+v, want an error", tc.body, got)
			}
		})
	}
}

// commandApp records the commands passed to Execute and returns err.
type commandApp struct {
	err      error
	commands []app.Command
	ctx      context.Context
}

func (a *commandApp) State(context.Context) (app.Snapshot, error) { return app.Snapshot{}, nil }

func (a *commandApp) Execute(ctx context.Context, c app.Command) error {
	a.commands = append(a.commands, c)
	a.ctx = ctx
	return a.err
}

// postCommand sends body to POST /api/commands. An empty contentType leaves
// the header unset.
func postCommand(t *testing.T, h http.Handler, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	ctx := context.WithValue(t.Context(), ctxKey{}, "request")
	r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/commands", strings.NewReader(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// checkOK verifies the success response of web.md 4.2.
func checkOK(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusOK || w.Body.String() != "{\"ok\":true}\n" {
		t.Fatalf("got %d %s, want 200 {\"ok\":true}", w.Code, w.Body)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
}

// Each valid request reaches Execute once, with the request context.
func TestCommandExecutes(t *testing.T) {
	for _, tc := range validCommands {
		t.Run(tc.name, func(t *testing.T) {
			a := &commandApp{}
			checkOK(t, postCommand(t, New(a, nil), "application/json", tc.body))
			if len(a.commands) != 1 || a.commands[0] != tc.want {
				t.Errorf("Execute got %+v, want [%+v]", a.commands, tc.want)
			}
			if a.ctx == nil || a.ctx.Value(ctxKey{}) != "request" {
				t.Errorf("Execute was not called with the request context")
			}
		})
	}
}

func TestCommandInvalidRequest(t *testing.T) {
	for _, tc := range invalidCommands {
		t.Run(tc.name, func(t *testing.T) {
			a := &commandApp{}
			checkError(t, postCommand(t, New(a, nil), "application/json", tc.body), http.StatusBadRequest, codeInvalidRequest)
			if len(a.commands) != 0 {
				t.Errorf("Execute called with %+v", a.commands)
			}
		})
	}
}

func TestCommandBodyLimit(t *testing.T) {
	start := `{"type":"start","data":{}}`
	for _, tc := range []struct {
		name string
		body string
		ok   bool
	}{
		{"at limit", start + strings.Repeat(" ", maxCommandBytes-len(start)), true},
		{"trailing space over limit", start + strings.Repeat(" ", maxCommandBytes-len(start)+1), false},
		{"leading space over limit", strings.Repeat(" ", maxCommandBytes-len(start)+1) + start, false},
		{"long Device ID", `{"type":"set_device_preview","data":{"deviceId":"` + strings.Repeat("A", maxCommandBytes) + `"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &commandApp{}
			w := postCommand(t, New(a, nil), "application/json", tc.body)
			if tc.ok {
				checkOK(t, w)
				return
			}
			checkError(t, w, http.StatusBadRequest, codeInvalidRequest)
			if len(a.commands) != 0 {
				t.Errorf("Execute called with %+v", a.commands)
			}
		})
	}
}

func TestCommandContentType(t *testing.T) {
	body := `{"type":"start","data":{}}`
	for _, contentType := range []string{
		"application/json",
		"application/json; charset=utf-8",
		"application/json;charset=UTF-8",
		`Application/JSON; Charset="utf-8"`,
	} {
		t.Run(contentType, func(t *testing.T) {
			a := &commandApp{}
			checkOK(t, postCommand(t, New(a, nil), contentType, body))
			if len(a.commands) != 1 {
				t.Errorf("Execute called %d times, want 1", len(a.commands))
			}
		})
	}
	for _, contentType := range []string{
		"",
		"text/plain",
		"text/json",
		"application/x-www-form-urlencoded",
		"application/problem+json",
		"application/jsonx",
		"application",
		"*/*",
		"application/json; charset=iso-8859-1",
		"application/json; charset=utf-16",
		"application/json; charset=utf8",
		"application/json; version=1",
		"application/json; charset=utf-8; version=1",
		"application/json; charset",
	} {
		t.Run(contentType, func(t *testing.T) {
			a := &commandApp{}
			checkError(t, postCommand(t, New(a, nil), contentType, body), http.StatusUnsupportedMediaType, codeUnsupportedMediaType)
			if len(a.commands) != 0 {
				t.Errorf("Execute called with %+v", a.commands)
			}
		})
	}
	// The media type is checked before the body.
	checkError(t, postCommand(t, New(&commandApp{}, nil), "text/plain", "not JSON"),
		http.StatusUnsupportedMediaType, codeUnsupportedMediaType)
}

func TestCommandErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   errorCode
	}{
		{"invalid", &app.Rejection{Code: app.CodeInvalid}, http.StatusBadRequest, codeInvalidRequest},
		{"not found", &app.Rejection{Code: app.CodeNotFound}, http.StatusNotFound, codeNotFound},
		{"phase conflict", &app.Rejection{Code: app.CodePhaseConflict}, http.StatusConflict, codePhaseConflict},
		{"PID exhausted", &app.Rejection{Code: app.CodePIDExhausted}, http.StatusConflict, codePIDExhausted},
		{"assignment conflict", &app.Rejection{Code: app.CodeAssignmentConflict}, http.StatusConflict, codeAssignmentConflict},
		{"device disconnected", &app.Rejection{Code: app.CodeDeviceDisconnected}, http.StatusConflict, codeDeviceDisconnected},
		{"device kind mismatch", &app.Rejection{Code: app.CodeDeviceKindMismatch}, http.StatusConflict, codeDeviceKindMismatch},
		{"device not assigned", &app.Rejection{Code: app.CodeDeviceNotAssigned}, http.StatusConflict, codeDeviceNotAssigned},
		{"manual mode conflict", &app.Rejection{Code: app.CodeManualModeConflict}, http.StatusConflict, codeManualModeConflict},
		{"wrapped rejection", fmt.Errorf("execute: %w", &app.Rejection{Code: app.CodeDeviceDisconnected}),
			http.StatusConflict, codeDeviceDisconnected},
		// Only players_not_ready has details.
		{"nodes of another code", &app.Rejection{Code: app.CodePhaseConflict, Nodes: []app.NodeID{1}},
			http.StatusConflict, codePhaseConflict},
		// Codes web.md 5 does not define never reach the client.
		{"not preparing", &app.Rejection{Code: app.CodeNotPreparing}, http.StatusInternalServerError, codeInternalError},
		{"unknown code", &app.Rejection{Code: "busy"}, http.StatusInternalServerError, codeInternalError},
		{"bot not ready", &app.Rejection{Code: app.CodePlayersNotReady, Nodes: []app.NodeID{1, 9}},
			http.StatusInternalServerError, codeInternalError},
		{"stopped", app.ErrStopped, http.StatusServiceUnavailable, codeUnavailable},
		{"wrapped stopped", fmt.Errorf("execute: %w", app.ErrStopped), http.StatusServiceUnavailable, codeUnavailable},
		{"canceled", context.Canceled, http.StatusInternalServerError, codeInternalError},
		{"other", errors.New("boom"), http.StatusInternalServerError, codeInternalError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &commandApp{err: tc.err}
			checkError(t, postCommand(t, New(a, nil), "application/json", `{"type":"start","data":{}}`), tc.status, tc.code)
			if len(a.commands) != 1 {
				t.Errorf("Execute called %d times, want 1", len(a.commands))
			}
		})
	}
}

func TestCommandPlayersNotReady(t *testing.T) {
	for _, tc := range []struct {
		name  string
		nodes []app.NodeID
		want  string
	}{
		{"sorted", []app.NodeID{8, 1, 3}, `["P1","P3","P8"]`},
		{"one", []app.NodeID{2}, `["P2"]`},
		{"none", nil, `[]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &commandApp{err: &app.Rejection{Code: app.CodePlayersNotReady, Nodes: tc.nodes}}
			w := postCommand(t, New(a, nil), "application/json", `{"type":"start","data":{}}`)
			want := `{"ok":false,"error":{"code":"players_not_ready","message":"` + errorMessages[codePlayersNotReady] +
				`","details":{"nodeIds":` + tc.want + `}}}` + "\n"
			if w.Code != http.StatusConflict || w.Body.String() != want {
				t.Errorf("got %d %s, want 409 %s", w.Code, w.Body, want)
			}
		})
	}
}

// With the real app, rejections carry app's English messages, which must not
// reach the response.
func TestCommandWithApp(t *testing.T) {
	a := app.New(app.Options{Logger: slog.New(slog.DiscardHandler)})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	h := New(a, nil)
	start := `{"type":"start","data":{}}`

	// Every player starts in auto mode, so Start is accepted once.
	checkOK(t, postCommand(t, h, "application/json", start))
	checkError(t, postCommand(t, h, "application/json", start), http.StatusConflict, codePhaseConflict)
	checkOK(t, postCommand(t, h, "application/json", `{"type":"reset","data":{}}`))
	checkError(t, postCommand(t, h, "application/json", `{"type":"set_device_preview","data":{"deviceId":"AABBCCDDEEFF"}}`),
		http.StatusNotFound, codeNotFound)
	checkError(t, postCommand(t, h, "application/json", `{"type":"set_node_mode","data":{"nodeId":"P1","mode":"manual"}}`),
		http.StatusConflict, codeDeviceDisconnected)

	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run: %v", err)
	}
	checkError(t, postCommand(t, h, "application/json", start), http.StatusServiceUnavailable, codeUnavailable)
}
