package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/canta-9142/d4-exhi/internal/app"
)

type fakeApp struct{}

func (fakeApp) State(context.Context) (app.Snapshot, error) { return app.Snapshot{}, nil }
func (fakeApp) Execute(context.Context, app.Command) error  { return nil }

func serve(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

// checkError verifies the web.md 5 error form, including details {}.
func checkError(t *testing.T, w *httptest.ResponseRecorder, status int, code errorCode) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status = %d, want %d; body %s", w.Code, status, w.Body)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	var body struct {
		OK    *bool `json:"ok"`
		Error struct {
			Code    errorCode       `json:"code"`
			Message string          `json:"message"`
			Details json.RawMessage `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON %s: %v", w.Body, err)
	}
	if body.OK == nil || *body.OK {
		t.Errorf("ok = %v, want false", body.OK)
	}
	if body.Error.Code != code || body.Error.Message != errorMessages[code] {
		t.Errorf("error = %s %q, want %s %q", body.Error.Code, body.Error.Message, code, errorMessages[code])
	}
	if string(body.Error.Details) != "{}" {
		t.Errorf("details = %s, want {}", body.Error.Details)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h := New(fakeApp{}, nil)
	for _, tc := range []struct{ method, path, allow string }{
		{http.MethodPost, "/api/state", "GET"},
		{http.MethodPut, "/api/state", "GET"},
		{http.MethodHead, "/api/state", "GET"},
		{http.MethodGet, "/api/commands", "POST"},
		{http.MethodDelete, "/api/commands", "POST"},
		{http.MethodPost, "/", "GET, HEAD"},
		{http.MethodPost, "/static/index.html", "GET, HEAD"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			w := serve(t, h, tc.method, tc.path)
			checkError(t, w, http.StatusMethodNotAllowed, codeMethodNotAllowed)
			if got := w.Header().Get("Allow"); got != tc.allow {
				t.Errorf("Allow = %q, want %q", got, tc.allow)
			}
		})
	}
}

func TestNotFound(t *testing.T) {
	h := New(fakeApp{}, nil)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/"},
		{http.MethodGet, "/api/unknown"},
		{http.MethodPost, "/api/state/extra"},
		{http.MethodGet, "/unknown"},
		{http.MethodGet, "/index.html"},
		{http.MethodGet, "/static/"},
		{http.MethodGet, "/static/missing.js"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			checkError(t, serve(t, h, tc.method, tc.path), http.StatusNotFound, codeNotFound)
		})
	}
}

func TestPlaceholderAPI(t *testing.T) {
	h := New(fakeApp{}, nil)
	checkError(t, serve(t, h, http.MethodGet, "/api/state"), http.StatusServiceUnavailable, codeUnavailable)
	checkError(t, serve(t, h, http.MethodPost, "/api/commands"), http.StatusServiceUnavailable, codeUnavailable)
}

func TestIndex(t *testing.T) {
	h := New(fakeApp{}, nil)
	for _, path := range []string{"/", "/static/index.html"} {
		w := serve(t, h, http.MethodGet, path)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", path, w.Code)
		}
		if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
			t.Errorf("%s: Content-Type = %q", path, got)
		}
		if !strings.Contains(w.Body.String(), "<title>") {
			t.Errorf("%s: body is not the admin page: %s", path, w.Body)
		}
	}
	if w := serve(t, h, http.MethodHead, "/"); w.Code != http.StatusOK {
		t.Errorf("HEAD /: status = %d", w.Code)
	}
}

func TestPanicRecovery(t *testing.T) {
	h := newHandler(fakeApp{}, nil)
	h.mux.HandleFunc("/panic", func(http.ResponseWriter, *http.Request) { panic("boom") })
	h.mux.HandleFunc("/panic-after-write", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		panic("boom")
	})
	checkError(t, serve(t, h, http.MethodGet, "/panic"), http.StatusInternalServerError, codeInternalError)
	// Once the header is sent, recovery must not write a second response.
	if w := serve(t, h, http.MethodGet, "/panic-after-write"); w.Code != http.StatusAccepted || w.Body.Len() != 0 {
		t.Errorf("status = %d, body %q", w.Code, w.Body)
	}
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	writeError(w, http.StatusConflict, codePlayersNotReady, errorMessages[codePlayersNotReady],
		map[string]any{"nodeIds": []string{"P1"}})
	want := `{"ok":false,"error":{"code":"players_not_ready","message":"準備が完了していないプレイヤーがいます。","details":{"nodeIds":["P1"]}}}` + "\n"
	if w.Code != http.StatusConflict || w.Body.String() != want {
		t.Errorf("got %d %s", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	writeJSON(w, http.StatusOK, func() {})
	checkError(t, w, http.StatusInternalServerError, codeInternalError)
}

func TestErrorMessages(t *testing.T) {
	codes := []errorCode{
		codeInvalidRequest, codeNotFound, codeMethodNotAllowed, codePhaseConflict, codePIDExhausted,
		codePlayersNotReady, codeAssignmentConflict, codeDeviceDisconnected, codeDeviceKindMismatch,
		codeDeviceNotAssigned, codeManualModeConflict, codeUnsupportedMediaType, codeInternalError, codeUnavailable,
	}
	if len(errorMessages) != len(codes) {
		t.Errorf("errorMessages has %d entries, want %d", len(errorMessages), len(codes))
	}
	for _, code := range codes {
		if errorMessages[code] == "" {
			t.Errorf("no message for %s", code)
		}
	}
	// The fallback body must match the table.
	var body errorBody
	if err := json.Unmarshal(internalErrorBody, &body); err != nil || body.Error.Message != errorMessages[codeInternalError] {
		t.Errorf("internalErrorBody = %s, %v", internalErrorBody, err)
	}
}
