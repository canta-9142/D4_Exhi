package web

import "net/http"

// handleState serves GET /api/state (web.md 3). Until the Snapshot conversion
// is implemented it answers 503 unavailable.
func (h *handler) handleState(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusServiceUnavailable, codeUnavailable, errorMessages[codeUnavailable], nil)
}
