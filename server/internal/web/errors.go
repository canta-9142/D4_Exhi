package web

import (
	"encoding/json"
	"net/http"
)

// errorCode is the machine-readable code of an error response (web.md 5).
type errorCode string

const (
	codeInvalidRequest       errorCode = "invalid_request"
	codeNotFound             errorCode = "not_found"
	codeMethodNotAllowed     errorCode = "method_not_allowed"
	codePhaseConflict        errorCode = "phase_conflict"
	codePIDExhausted         errorCode = "pid_exhausted"
	codePlayersNotReady      errorCode = "players_not_ready"
	codeAssignmentConflict   errorCode = "assignment_conflict"
	codeDeviceDisconnected   errorCode = "device_disconnected"
	codeDeviceKindMismatch   errorCode = "device_kind_mismatch"
	codeDeviceNotAssigned    errorCode = "device_not_assigned"
	codeManualModeConflict   errorCode = "manual_mode_conflict"
	codeUnsupportedMediaType errorCode = "unsupported_media_type"
	codeInternalError        errorCode = "internal_error"
	codeUnavailable          errorCode = "unavailable"
)

// errorMessages holds the display message for each code. Clients must branch
// on the code, not on these texts.
var errorMessages = map[errorCode]string{
	codeInvalidRequest:       "要求の形式が正しくありません。",
	codeNotFound:             "対象が見つかりません。",
	codeMethodNotAllowed:     "このメソッドには対応していません。",
	codePhaseConflict:        "現在のフェーズではこの操作を行えません。",
	codePIDExhausted:         "次のゲームに必要なパケットIDが不足しています。サーバーを再起動してください。",
	codePlayersNotReady:      "準備が完了していないプレイヤーがいます。",
	codeAssignmentConflict:   "割当先が他の機器と競合しています。",
	codeDeviceDisconnected:   "機器が接続されていないか、識別が完了していません。",
	codeDeviceKindMismatch:   "機器の種別が一致しません。",
	codeDeviceNotAssigned:    "機器が割り当てられていません。",
	codeManualModeConflict:   "手動操作中のプレイヤーには確認表示を開始できません。",
	codeUnsupportedMediaType: "Content-Typeにはapplication/jsonを指定してください。",
	codeInternalError:        "サーバー内部でエラーが発生しました。",
	codeUnavailable:          "サーバーが要求を受け付けられない状態です。",
}

type errorBody struct {
	OK    bool        `json:"ok"`
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    errorCode      `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

// internalErrorBody is written when a response cannot be encoded.
var internalErrorBody = []byte(`{"ok":false,"error":{"code":"internal_error","message":"サーバー内部でエラーが発生しました。","details":{}}}` + "\n")

// writeJSON writes v as an API response, which is never cached.
func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		status, body = http.StatusInternalServerError, internalErrorBody
	} else {
		body = append(body, '\n')
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	w.Write(body)
}

// writeError writes the common error form of web.md 5. Nil details become {}.
func writeError(w http.ResponseWriter, status int, code errorCode, message string, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: message, Details: details}})
}
