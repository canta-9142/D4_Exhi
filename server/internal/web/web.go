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
	h.mux.HandleFunc("/api/", notFound)
	h.mux.Handle("/static/", allowMethods(h.handleStatic, http.MethodGet, http.MethodHead))
	index := allowMethods(func(w http.ResponseWriter, r *http.Request) { h.serveFile(w, r, "index.html") },
		http.MethodGet, http.MethodHead)
	h.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			notFound(w, r)
			return
		}
		index.ServeHTTP(w, r)
	})
	return h
}

// ServeHTTP recovers panics as 500 internal_error and writes the access log.
func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rec := &recorder{ResponseWriter: w}
	defer func() {
		if v := recover(); v != nil {
			if v == http.ErrAbortHandler {
				panic(v)
			}
			h.logger.Error("http handler panic", "method", r.Method, "path", r.URL.Path,
				"panic", fmt.Sprint(v), "stack", string(debug.Stack()))
			// After the header is sent the status can no longer change.
			if !rec.wrote {
				writeError(rec, http.StatusInternalServerError, codeInternalError, errorMessages[codeInternalError], nil)
			}
		}
		h.logger.Debug("http request", "method", r.Method, "path", r.URL.Path, "status", rec.status,
			"duration", time.Since(start), "remote", r.RemoteAddr)
	}()
	h.mux.ServeHTTP(rec, r)
}

// allowMethods rejects other methods with 405 method_not_allowed and an Allow
// header listing methods.
func allowMethods(next http.HandlerFunc, methods ...string) http.Handler {
	allow := strings.Join(methods, ", ")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !slices.Contains(methods, r.Method) {
			w.Header().Set("Allow", allow)
			writeError(w, http.StatusMethodNotAllowed, codeMethodNotAllowed, errorMessages[codeMethodNotAllowed], nil)
			return
		}
		next(w, r)
	})
}

func notFound(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, codeNotFound, errorMessages[codeNotFound], nil)
}

func (h *handler) handleStatic(w http.ResponseWriter, r *http.Request) {
	h.serveFile(w, r, strings.TrimPrefix(r.URL.Path, "/static/"))
}

// serveFile serves an embedded regular file. Directories and missing files
// are 404 not_found; nothing is listed or redirected.
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
	if !r.wrote {
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

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
