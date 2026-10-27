package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path"
	"regexp"
	"slices"
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
		{http.MethodGet, "/api"},
		{http.MethodPost, "/api"},
		{http.MethodGet, "/static"},
		{http.MethodGet, "/api/"},
		{http.MethodGet, "/api/unknown"},
		{http.MethodPost, "/api/state/extra"},
		{http.MethodGet, "/unknown"},
		{http.MethodGet, "/index.html"},
		{http.MethodGet, "/static/"},
		{http.MethodGet, "/static/missing.js"},
		{http.MethodPost, "/static/missing.js"},
		{http.MethodPost, "/static/"},
		// Non-canonical paths, which ServeMux would redirect.
		{http.MethodGet, "/api//state"},
		{http.MethodPost, "/api/./commands"},
		{http.MethodGet, "/static/../api/state"},
		{http.MethodGet, "/static//index.html"},
		{http.MethodGet, "/./"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			checkError(t, serve(t, h, tc.method, tc.path), http.StatusNotFound, codeNotFound)
		})
	}
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

var (
	// htmlRef matches the src and href attributes of the admin page.
	htmlRef = regexp.MustCompile(`(?:src|href)="([^"]*)"`)
	// moduleRef matches static imports such as `from "./api.js"`.
	moduleRef = regexp.MustCompile(`(?:import|from)\s*"([^"]*)"`)
)

// TestStaticFiles serves every embedded file and checks that the page and its
// modules refer only to embedded files, so that a renamed file cannot break
// the admin page silently.
func TestStaticFiles(t *testing.T) {
	h := New(fakeApp{}, nil)
	static, err := fs.Sub(embedded, "static")
	if err != nil {
		t.Fatal(err)
	}
	// Browsers refuse module scripts and stylesheets of other types.
	types := map[string][]string{
		".html": {"text/html"},
		".css":  {"text/css"},
		".js":   {"text/javascript", "application/javascript"},
	}
	refs := map[string]string{} // embedded name -> file referring to it
	err = fs.WalkDir(static, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		want, err := fs.ReadFile(static, name)
		if err != nil {
			return err
		}
		w := serve(t, h, http.MethodGet, "/static/"+name)
		if w.Code != http.StatusOK {
			t.Errorf("%s: status = %d", name, w.Code)
		} else if !bytes.Equal(w.Body.Bytes(), want) {
			t.Errorf("%s: body differs from the embedded file", name)
		}
		ext := path.Ext(name)
		got := w.Header().Get("Content-Type")
		if !slices.ContainsFunc(types[ext], func(prefix string) bool { return strings.HasPrefix(got, prefix) }) {
			t.Errorf("%s: Content-Type = %q, want one of %q", name, got, types[ext])
		}
		if got := w.Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("%s: Cache-Control = %q", name, got)
		}
		switch ext {
		case ".html":
			for _, m := range htmlRef.FindAllStringSubmatch(string(want), -1) {
				ref, ok := strings.CutPrefix(m[1], "/static/")
				if !ok {
					t.Errorf("%s: %q is not an embedded file", name, m[1])
				}
				refs[ref] = name
			}
		case ".js":
			for _, m := range moduleRef.FindAllStringSubmatch(string(want), -1) {
				if !strings.HasPrefix(m[1], "./") && !strings.HasPrefix(m[1], "../") {
					t.Errorf("%s: import %q is not relative", name, m[1])
				}
				refs[path.Join(path.Dir(name), m[1])] = name
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) == 0 {
		t.Error("no references found in the admin page")
	}
	for ref, from := range refs {
		if info, err := fs.Stat(static, ref); err != nil || info.IsDir() {
			t.Errorf("%s refers to %s, which is not an embedded file", from, ref)
		}
	}
}

func TestPanicRecovery(t *testing.T) {
	h := newHandler(fakeApp{}, nil)
	h.mux.HandleFunc("/panic", func(http.ResponseWriter, *http.Request) { panic("boom") })
	// An informational header does not commit the response.
	h.mux.HandleFunc("/after-early-hints", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusEarlyHints)
		panic("boom")
	})
	checkError(t, serve(t, h, http.MethodGet, "/panic"), http.StatusInternalServerError, codeInternalError)

	// ResponseRecorder treats 1xx as final, so check through a real server.
	server := httptest.NewServer(h)
	defer server.Close()
	resp, err := http.Get(server.URL + "/after-early-hints")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("after 103: status = %d, want 500", resp.StatusCode)
	}
}

// A panic after the header is sent must abort the response, not end it as if
// the partial body were complete.
func TestPanicAfterWriteAborts(t *testing.T) {
	h := newHandler(fakeApp{}, nil)
	for _, tc := range []struct {
		path   string
		handle http.HandlerFunc
	}{
		{"/after-header", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			panic("boom")
		}},
		{"/after-body", func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte(`{"ok":`))
			if err := http.NewResponseController(w).Flush(); err != nil {
				t.Errorf("flush: %v", err)
			}
			panic("boom")
		}},
		// Flushing through ResponseController also sends the header.
		{"/after-flush", func(w http.ResponseWriter, _ *http.Request) {
			if err := http.NewResponseController(w).Flush(); err != nil {
				t.Errorf("flush: %v", err)
			}
			panic("boom")
		}},
		{"/abort", func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }},
	} {
		h.mux.HandleFunc(tc.path, tc.handle)
		t.Run(tc.path, func(t *testing.T) {
			defer func() {
				if v := recover(); v != http.ErrAbortHandler {
					t.Errorf("panic = %v, want http.ErrAbortHandler", v)
				}
			}()
			serve(t, h, http.MethodGet, tc.path)
		})
	}

	server := httptest.NewServer(h)
	defer server.Close()
	resp, err := http.Get(server.URL + "/after-body")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if body, err := io.ReadAll(resp.Body); err == nil {
		t.Errorf("client read a complete body %q from an aborted response", body)
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
