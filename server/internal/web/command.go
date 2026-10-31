package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/canta-9142/d4-exhi/internal/app"
)

// maxCommandBytes limits the body of POST /api/commands. Larger bodies are
// 400 invalid_request.
const maxCommandBytes = 64 << 10

// noDataOperations are the types whose data must be {}.
var noDataOperations = map[string]app.Operation{"start": app.Start, "finish": app.Finish, "reset": app.Reset}

// ledNodes are the LED control nodes of web.md 2: P1, P3, P5, P7 and B1-B3.
var ledNodes = []app.NodeID{1, 3, 5, 7, 9, 10, 11}

// conflictCodes are the app codes answered with 409 and the same-named code
// (web.md 5).
var conflictCodes = map[app.Code]errorCode{
	app.CodePhaseConflict:      codePhaseConflict,
	app.CodePIDExhausted:       codePIDExhausted,
	app.CodePlayersNotReady:    codePlayersNotReady,
	app.CodeAssignmentConflict: codeAssignmentConflict,
	app.CodeDeviceDisconnected: codeDeviceDisconnected,
	app.CodeDeviceKindMismatch: codeDeviceKindMismatch,
	app.CodeDeviceNotAssigned:  codeDeviceNotAssigned,
	app.CodeManualModeConflict: codeManualModeConflict,
}

type okBody struct {
	OK bool `json:"ok"`
}

// skippedBody is the success response of set_all_node_modes (web.md 4.2).
// Skipped is never null.
type skippedBody struct {
	OK      bool     `json:"ok"`
	Skipped []string `json:"skipped"`
}

// handleCommand serves POST /api/commands (web.md 4). A request that is not
// a valid command never reaches app.
func (h *handler) handleCommand(w http.ResponseWriter, r *http.Request) {
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		writeError(w, http.StatusUnsupportedMediaType, codeUnsupportedMediaType, errorMessages[codeUnsupportedMediaType], nil)
		return
	}
	command, err := decodeCommand(http.MaxBytesReader(w, r.Body, maxCommandBytes))
	if err != nil {
		h.logger.Info("invalid command request", "error", err)
		writeError(w, http.StatusBadRequest, codeInvalidRequest, errorMessages[codeInvalidRequest], nil)
		return
	}
	result, err := h.app.ExecuteWithResult(r.Context(), command)
	var rejection *app.Rejection
	switch {
	case err == nil:
		h.writeSuccess(w, command, result)
	case errors.As(err, &rejection):
		h.writeRejection(w, command, rejection)
	case errors.Is(err, app.ErrStopped):
		writeError(w, http.StatusServiceUnavailable, codeUnavailable, errorMessages[codeUnavailable], nil)
	default:
		h.logger.Error("app execute failed", "operation", command.Operation, "error", err)
		writeError(w, http.StatusInternalServerError, codeInternalError, errorMessages[codeInternalError], nil)
	}
}

// writeSuccess answers a command app applied. Only set_all_node_modes adds
// anything to {"ok":true}.
func (h *handler) writeSuccess(w http.ResponseWriter, command app.Command, result app.Result) {
	if command.Operation != app.SetAllNodesEnabled {
		writeJSON(w, http.StatusOK, okBody{OK: true})
		return
	}
	skipped := make([]string, 0, len(result.Skipped))
	for _, id := range sortedIDs(result.Skipped) {
		name, err := playerName(id)
		if err != nil {
			// The command was applied, but the client cannot be told which
			// players stayed automatic, so this is not reported as a success.
			h.logger.Error("invalid skipped node", "node", id, "error", err)
			writeError(w, http.StatusInternalServerError, codeInternalError, errorMessages[codeInternalError], nil)
			return
		}
		skipped = append(skipped, name)
	}
	writeJSON(w, http.StatusOK, skippedBody{OK: true, Skipped: skipped})
}

// writeRejection answers a command app did not apply. The app message is
// English and goes only to the log; the response uses errorMessages.
func (h *handler) writeRejection(w http.ResponseWriter, command app.Command, r *app.Rejection) {
	h.logger.Info("command rejected", "operation", command.Operation, "code", r.Code, "error", r.Error())
	switch r.Code {
	case app.CodeInvalid:
		writeError(w, http.StatusBadRequest, codeInvalidRequest, errorMessages[codeInvalidRequest], nil)
		return
	case app.CodeNotFound:
		writeError(w, http.StatusNotFound, codeNotFound, errorMessages[codeNotFound], nil)
		return
	}
	code, ok := conflictCodes[r.Code]
	if !ok {
		h.logger.Error("unknown rejection code", "code", r.Code)
		writeError(w, http.StatusInternalServerError, codeInternalError, errorMessages[codeInternalError], nil)
		return
	}
	var details map[string]any
	if code == codePlayersNotReady {
		names := make([]string, 0, len(r.Nodes))
		for _, id := range sortedIDs(r.Nodes) {
			name, err := playerName(id)
			if err != nil {
				h.logger.Error("invalid rejection", "code", r.Code, "error", err)
				writeError(w, http.StatusInternalServerError, codeInternalError, errorMessages[codeInternalError], nil)
				return
			}
			names = append(names, name)
		}
		details = map[string]any{"nodeIds": names}
	}
	writeError(w, http.StatusConflict, code, errorMessages[code], details)
}

// isJSONContentType accepts application/json with at most charset=utf-8.
func isJSONContentType(value string) bool {
	mediaType, params, err := mime.ParseMediaType(value)
	if err != nil || mediaType != "application/json" {
		return false
	}
	for name, v := range params {
		if name != "charset" || !strings.EqualFold(v, "utf-8") {
			return false
		}
	}
	return true
}

// decodeCommand reads one command of web.md 4.1 from body.
//
// encoding/json matches struct fields case-insensitively, so objects are
// decoded into maps and their keys are compared exactly. Values are never
// converted between JSON types, and null is accepted only where web.md 4.1
// allows it.
func decodeCommand(body io.Reader) (app.Command, error) {
	dec := json.NewDecoder(body)
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return app.Command{}, fmt.Errorf("read body: %w", err)
	}
	if err := dec.Decode(&json.RawMessage{}); err != io.EOF {
		if err == nil {
			err = errors.New("another JSON value")
		}
		return app.Command{}, fmt.Errorf("after the command: %w", err)
	}
	// encoding/json replaces invalid UTF-8 instead of rejecting it, which
	// would alter Device IDs.
	if !utf8.Valid(raw) {
		return app.Command{}, errors.New("body is not valid UTF-8")
	}
	fields, err := decodeObject(raw, "type", "data")
	if err != nil {
		return app.Command{}, err
	}
	typ, err := decodeString(fields["type"])
	if err != nil {
		return app.Command{}, fmt.Errorf("type: %w", err)
	}
	command, err := decodeData(typ, fields["data"])
	if err != nil {
		return app.Command{}, fmt.Errorf("%q command: %w", typ, err)
	}
	return command, nil
}

func decodeData(typ string, data json.RawMessage) (app.Command, error) {
	switch typ {
	case "start", "finish", "reset":
		if _, err := decodeObject(data); err != nil {
			return app.Command{}, err
		}
		return app.Command{Operation: noDataOperations[typ]}, nil

	case "set_node_mode":
		fields, err := decodeObject(data, "nodeId", "mode")
		if err != nil {
			return app.Command{}, err
		}
		node, err := decodeNode(fields["nodeId"], playerName)
		if err != nil {
			return app.Command{}, err
		}
		manual, err := decodeMode(fields["mode"])
		if err != nil {
			return app.Command{}, err
		}
		return app.Command{Operation: app.SetNodeEnabled, Node: node, Enabled: manual}, nil

	case "set_all_node_modes":
		fields, err := decodeObject(data, "mode")
		if err != nil {
			return app.Command{}, err
		}
		manual, err := decodeMode(fields["mode"])
		if err != nil {
			return app.Command{}, err
		}
		return app.Command{Operation: app.SetAllNodesEnabled, Enabled: manual}, nil

	case "set_bot_rate":
		fields, err := decodeObject(data, "nodeId", "ratePps")
		if err != nil {
			return app.Command{}, err
		}
		node, err := decodeNode(fields["nodeId"], botName)
		if err != nil {
			return app.Command{}, err
		}
		rate, err := decodeRate(fields["ratePps"])
		if err != nil {
			return app.Command{}, fmt.Errorf("ratePps: %w", err)
		}
		return app.Command{Operation: app.SetBotRate, Node: node, Rate: rate}, nil

	case "set_device_assignment":
		fields, err := decodeObject(data, "deviceId", "target")
		if err != nil {
			return app.Command{}, err
		}
		id, err := decodeDeviceID(fields["deviceId"])
		if err != nil {
			return app.Command{}, fmt.Errorf("deviceId: %w", err)
		}
		command := app.Command{Operation: app.SetDeviceAssignment, DeviceID: id}
		if isNull(fields["target"]) {
			return command, nil
		}
		command.Kind, command.Node, err = decodeTarget(fields["target"])
		if err != nil {
			return app.Command{}, fmt.Errorf("target: %w", err)
		}
		return command, nil

	case "swap_device_assignments":
		return decodeDevicePair(data, app.SwapDeviceAssignments, "firstDeviceId", "secondDeviceId")

	case "replace_device_assignment":
		return decodeDevicePair(data, app.ReplaceDeviceAssignment, "oldDeviceId", "newDeviceId")

	case "set_device_preview":
		fields, err := decodeObject(data, "deviceId")
		if err != nil {
			return app.Command{}, err
		}
		command := app.Command{Operation: app.SetDevicePreview}
		if isNull(fields["deviceId"]) {
			return command, nil
		}
		command.DeviceID, err = decodeDeviceID(fields["deviceId"])
		if err != nil {
			return app.Command{}, fmt.Errorf("deviceId: %w", err)
		}
		return command, nil
	}
	return app.Command{}, errors.New("unknown type")
}

// decodeMode reads the mode of set_node_mode and set_all_node_modes and
// reports whether it is manual.
func decodeMode(raw json.RawMessage) (bool, error) {
	mode, err := decodeString(raw)
	if err != nil {
		return false, fmt.Errorf("mode: %w", err)
	}
	if mode != "manual" && mode != "auto" {
		return false, fmt.Errorf("unknown mode %q", mode)
	}
	return mode == "manual", nil
}

// decodeTarget reads {"kind","nodeId"} of set_device_assignment.
func decodeTarget(raw json.RawMessage) (app.DeviceKind, app.NodeID, error) {
	fields, err := decodeObject(raw, "kind", "nodeId")
	if err != nil {
		return "", 0, err
	}
	kind, err := decodeString(fields["kind"])
	if err != nil {
		return "", 0, fmt.Errorf("kind: %w", err)
	}
	name := playerName
	switch app.DeviceKind(kind) {
	case app.PlayerDevice:
	case app.LEDDevice:
		name = ledName
	default:
		return "", 0, fmt.Errorf("unknown kind %q", kind)
	}
	node, err := decodeNode(fields["nodeId"], name)
	if err != nil {
		return "", 0, err
	}
	return app.DeviceKind(kind), node, nil
}

// decodeDevicePair reads the two distinct Device IDs of swap and replace.
func decodeDevicePair(data json.RawMessage, operation app.Operation, firstKey, secondKey string) (app.Command, error) {
	fields, err := decodeObject(data, firstKey, secondKey)
	if err != nil {
		return app.Command{}, err
	}
	first, err := decodeDeviceID(fields[firstKey])
	if err != nil {
		return app.Command{}, fmt.Errorf("%s: %w", firstKey, err)
	}
	second, err := decodeDeviceID(fields[secondKey])
	if err != nil {
		return app.Command{}, fmt.Errorf("%s: %w", secondKey, err)
	}
	if first == second {
		return app.Command{}, fmt.Errorf("%s and %s are the same device", firstKey, secondKey)
	}
	return app.Command{Operation: operation, DeviceID: first, OtherDeviceID: second}, nil
}

// decodeObject reads a JSON object that has exactly keys, compared
// case-sensitively.
func decodeObject(raw json.RawMessage, keys ...string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	// null decodes into a nil map without an error.
	if fields == nil {
		return nil, errors.New("not an object")
	}
	for _, key := range keys {
		if _, ok := fields[key]; !ok {
			return nil, fmt.Errorf("missing field %q", key)
		}
	}
	for key := range fields {
		if !slices.Contains(keys, key) {
			return nil, fmt.Errorf("unknown field %q", key)
		}
	}
	return fields, nil
}

func isNull(raw json.RawMessage) bool { return string(raw) == "null" }

// decodeString reads a JSON string. Unmarshal would leave the string empty
// for null, so null is rejected first.
func decodeString(raw json.RawMessage) (string, error) {
	if isNull(raw) {
		return "", errors.New("null is not a string")
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", err
	}
	return s, nil
}

func decodeDeviceID(raw json.RawMessage) (string, error) {
	id, err := decodeString(raw)
	if err == nil && id == "" {
		err = errors.New("empty Device ID")
	}
	return id, err
}

// decodeRate reads a finite positive JSON number. Strings such as "1.5" are
// not numbers.
func decodeRate(raw json.RawMessage) (float64, error) {
	if isNull(raw) {
		return 0, errors.New("null is not a number")
	}
	var rate float64
	if err := json.Unmarshal(raw, &rate); err != nil {
		return 0, err
	}
	// Unmarshal rejects numbers beyond float64 and JSON has no NaN, so the
	// rate is finite here.
	if rate <= 0 {
		return 0, fmt.Errorf("%v is not positive", rate)
	}
	return rate, nil
}

// decodeNode reads a public node ID that name accepts.
func decodeNode(raw json.RawMessage, name func(app.NodeID) (string, error)) (app.NodeID, error) {
	s, err := decodeString(raw)
	if err != nil {
		return 0, fmt.Errorf("nodeId: %w", err)
	}
	for id := app.NodeID(1); id <= app.NodeCount; id++ {
		if n, err := name(id); err == nil && n == s {
			return id, nil
		}
	}
	return 0, fmt.Errorf("invalid nodeId %q", s)
}

func ledName(id app.NodeID) (string, error) {
	if !slices.Contains(ledNodes, id) {
		return "", fmt.Errorf("node %d has no LED board", id)
	}
	return nodeName(id)
}
