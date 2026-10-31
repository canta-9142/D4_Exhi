package app

import (
	"fmt"
	"strings"
)

// Code classifies a rejected command for callers such as web (web.md 5).
type Code string

const (
	CodeInvalid            Code = "invalid"
	CodeNotFound           Code = "not_found"
	CodePhaseConflict      Code = "phase_conflict"
	CodePIDExhausted       Code = "pid_exhausted"
	CodePlayersNotReady    Code = "players_not_ready"
	CodeAssignmentConflict Code = "assignment_conflict"
	CodeDeviceDisconnected Code = "device_disconnected"
	CodeDeviceKindMismatch Code = "device_kind_mismatch"
	CodeDeviceNotAssigned  Code = "device_not_assigned"
	CodeManualModeConflict Code = "manual_mode_conflict"
	// CodeNotPreparing and CodeNotStopped appear only in StartBlocker; a
	// rejected Start uses CodePhaseConflict instead.
	CodeNotPreparing Code = "not_preparing"
	CodeNotStopped   Code = "not_stopped"
)

// Rejection reports a command that app did not apply. The message is for logs;
// callers must branch on Code. Nodes lists the affected nodes in ID order.
type Rejection struct {
	Code  Code
	Nodes []NodeID
	msg   string
}

func (r *Rejection) Error() string { return r.msg }

func reject(code Code, msg string) *Rejection {
	return &Rejection{Code: code, msg: msg}
}

// StartBlocker is one unmet Start condition, in web.md 3.3 order.
type StartBlocker struct {
	Code  Code     `json:"code"`
	Nodes []NodeID `json:"nodes,omitempty"`
}

func (b StartBlocker) reject() *Rejection {
	switch b.Code {
	case CodeNotPreparing:
		return reject(CodePhaseConflict, "Start requires Preparing phase")
	case CodeNotStopped:
		return reject(CodePhaseConflict, "Start requires stopped operation")
	case CodePIDExhausted:
		return reject(b.Code, "not enough packet IDs for another game; restart the server")
	}
	names := make([]string, len(b.Nodes))
	for i, id := range b.Nodes {
		names[i] = id.wireName()
	}
	r := reject(b.Code, fmt.Sprintf("players are not ready: %s", strings.Join(names, ", ")))
	r.Nodes = append([]NodeID(nil), b.Nodes...)
	return r
}
