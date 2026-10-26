package web

import "net/http"

// handleCommand serves POST /api/commands (web.md 4). Until request decoding
// is implemented it answers 503 unavailable without calling app.
func (h *handler) handleCommand(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusServiceUnavailable, codeUnavailable, errorMessages[codeUnavailable], nil)
}
