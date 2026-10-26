// Package web serves the admin HTTP API (docs/spec/web.md) and the embedded
// admin page. It converts HTTP requests into app calls and never changes game
// state directly.
package web

import (
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"github.com/canta-9142/d4-exhi/internal/app"
)

// App is the part of app.App that web depends on.
type App interface {
	State(ctx context.Context) (app.Snapshot, error)
	Execute(ctx context.Context, c app.Command) error
}

//go:embed static
var embedded embed.FS

type handler struct {
	app    App
	logger *slog.Logger
	mux    *http.ServeMux
	static fs.FS
}

// New returns the handler for the API and the admin page. A nil logger
// discards logs.
func New(a App, logger *slog.Logger) http.Handler {
	return newHandler(a, logger)
}

func newHandler(a App, logger *slog.Logger) *handler {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	static, err := fs.Sub(embedded, "static")
	if err != nil {
		panic(err)
	}
	h := &handler{app: a, logger: logger, mux: http.NewServeMux(), static: static}
	// Patterns carry no method so that unsupported methods reach allowMethods
	// and get the JSON 405 of web.md 5 instead of ServeMux's plain text.
	h.mux.Handle("/api/state", allowMethods(h.handleState, http.MethodGet))
	h.mux.Handle("/api/commands", allowMethods(h.handleCommand, http.MethodPost))
	// Without the exact /api and /static patterns, ServeMux would redirect
	// them to the trailing-slash subtree instead of answering JSON 404.
	h.mux.HandleFunc("/api/", notFound)
	h.mux.HandleFunc("/api", notFound)
	h.mux.HandleFunc("/static/", h.handleStatic)
	h.mux.HandleFunc("/static", notFound)
	h.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			notFound(w, r)
			return
		}
		h.serveFile(w, r, "index.html")
	})
	return h
}

// ServeHTTP recovers panics as 500 internal_error and writes the access log.
// A panic after the header is sent aborts the response instead, so that the
// client does not take a partial body as complete.
func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rec := &recorder{ResponseWriter: w}
	defer func() {
		v := recover()
		abort := v == http.ErrAbortHandler || (v != nil && rec.wrote)
		if v != nil && v != http.ErrAbortHandler {
			h.logger.Error("http handler panic", "method", r.Method, "path", r.URL.Path,
				"panic", fmt.Sprint(v), "stack", string(debug.Stack()))
			if !abort {
				writeError(rec, http.StatusInternalServerError, codeInternalError, errorMessages[codeInternalError], nil)
			}
		}
		h.logger.Debug("http request", "method", r.Method, "path", r.URL.Path, "status", rec.status,
			"aborted", abort, "duration", time.Since(start), "remote", r.RemoteAddr)
		if abort {
			// net/http closes the connection without logging this value.
			panic(http.ErrAbortHandler)
		}
	}()
	// ServeMux redirects non-canonical paths such as /api//state with an
	// HTML response; no such path is served, so answer JSON 404 instead.
	if r.Method != http.MethodConnect && r.URL.Path != cleanPath(r.URL.Path) {
		notFound(rec, r)
		return
	}
	h.mux.ServeHTTP(rec, r)
}

// cleanPath is the canonical form ServeMux redirects to: path.Clean with a
// trailing slash kept.
func cleanPath(p string) string {
	if p == "" || p[0] != '/' {
		p = "/" + p
	}
	clean := path.Clean(p)
	if strings.HasSuffix(p, "/") && clean != "/" {
		clean += "/"
	}
	return clean
}

// allowMethods rejects other methods with 405 method_not_allowed and an Allow
// header listing methods.
func allowMethods(next http.HandlerFunc, methods ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if checkMethod(w, r, methods...) {
			next(w, r)
		}
	})
}

// checkMethod writes 405 and reports false unless r uses one of methods.
func checkMethod(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	if slices.Contains(methods, r.Method) {
		return true
	}
	w.Header().Set("Allow", strings.Join(methods, ", "))
	writeError(w, http.StatusMethodNotAllowed, codeMethodNotAllowed, errorMessages[codeMethodNotAllowed], nil)
	return false
}

func notFound(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, codeNotFound, errorMessages[codeNotFound], nil)
}

func (h *handler) handleStatic(w http.ResponseWriter, r *http.Request) {
	h.serveFile(w, r, strings.TrimPrefix(r.URL.Path, "/static/"))
}

// serveFile serves an embedded regular file. Directories and missing files
// are 404 not_found for every method; nothing is listed or redirected.
func (h *handler) serveFile(w http.ResponseWriter, r *http.Request, name string) {
	if name == "" || !fs.ValidPath(name) {
		notFound(w, r)
		return
	}
	f, err := h.static.Open(name)
	if err != nil {
		notFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		notFound(w, r)
		return
	}
	// The file exists, so other methods are 405 rather than 404 (web.md 5).
	if !checkMethod(w, r, http.MethodGet, http.MethodHead) {
		return
	}
	content, ok := f.(io.ReadSeeker)
	if !ok {
		panic(fmt.Sprintf("embedded file %s is not seekable", name))
	}
	// Embedded files have no modification time; revalidate so that a new
	// build is picked up.
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, info.Name(), info.ModTime(), content)
}

// recorder remembers the status for the access log and whether the header
// has been sent.
type recorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (r *recorder) WriteHeader(status int) {
	// Informational responses other than 101 do not commit the header.
	if !r.wrote && (status < 100 || status > 199 || status == http.StatusSwitchingProtocols) {
		r.status, r.wrote = status, true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.status, r.wrote = http.StatusOK, true
	}
	return r.ResponseWriter.Write(b)
}

// FlushError sends the header like Write does. http.ResponseController
// prefers it over Unwrap, so flushes cannot bypass the recorder.
func (r *recorder) FlushError() error {
	if !r.wrote {
		r.status, r.wrote = http.StatusOK, true
	}
	return http.NewResponseController(r.ResponseWriter).Flush()
}

func (r *recorder) Flush() { r.FlushError() }

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
