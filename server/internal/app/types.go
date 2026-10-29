package app

import (
	"encoding/json"
	"fmt"
	"time"
)

// SessionID must be unique for each connection, including reconnects.
type SessionID string

type Operation string

type DeviceKind string

const (
	PlayerDevice DeviceKind = "player"
	LEDDevice    DeviceKind = "led"
)

const (
	Start                   Operation = "Start"
	Finish                  Operation = "Finish"
	Reset                   Operation = "Reset"
	SetNodeEnabled          Operation = "SetNodeEnabled"
	SetBotRate              Operation = "SetBotRate"
	SetDeviceAssignment     Operation = "SetDeviceAssignment"
	SwapDeviceAssignments   Operation = "SwapDeviceAssignments"
	ReplaceDeviceAssignment Operation = "ReplaceDeviceAssignment"
	SetDevicePreview        Operation = "SetDevicePreview"
)

type Command struct {
	Operation Operation
	// Node zero removes a device assignment.
	Node          NodeID
	Enabled       bool
	Rate          float64
	DeviceID      string
	OtherDeviceID string
	Kind          DeviceKind
}

// IncomingMessage has already passed serial's JSON/required-field validation.
// app checks its session, synchronization generation and game semantics.
type IncomingMessage struct {
	// Identification is transport metadata, not an MCU-supplied field.
	Identification uint64
	Type           string
	UID            string
	Kind           DeviceKind // Set by serial when classifying a HELLO response.
	Sync           string
	RID            uint32
	PID            uint16
	To             NodeID
}

// Message is an outgoing command. serial uses MarshalLine and adds LF framing.
type Message struct {
	Type string `json:"type"`
	Data any    `json:"data"`
	// Identification metadata never changes the wire format. Retries retain
	// their generation and leave BeginIdentification false.
	BeginIdentification bool   `json:"-"`
	Identification      uint64 `json:"-"`
}

type ConfigData struct {
	Node    NodeID `json:"node"`
	Mode    string `json:"mode"`
	Preview bool   `json:"preview"`
}
type StatusData struct {
	Score     int  `json:"score"`
	Remaining int  `json:"remaining"`
	Ready     bool `json:"ready"`
	Countdown int  `json:"countdown"`
}
type LEDConfigData struct {
	Node    NodeID
	Preview bool
}
type SummaryData struct {
	Sync     string `json:"sync"`
	Score    int    `json:"score"`
	Rank     *int   `json:"rank"`
	Eligible bool   `json:"eligible"`
	// TopPercent is 1-50, or nil when the player is not within the top half
	// or the server cannot tell (player.md).
	TopPercent *int `json:"topPercent"`
}
type LightData struct {
	Neighbor    NodeID
	Destination NodeID
	Direction   string
}
type BufferData struct{ Count int }

// MarshalLine preserves the LED text protocol; identify remains shared JSON.
func (m Message) MarshalLine() ([]byte, error) {
	switch m.Type {
	case "CONFIG":
		if d, ok := m.Data.(LEDConfigData); ok && validAssignment(LEDDevice, d.Node) {
			return []byte(fmt.Sprintf("CONFIG %s %t", d.Node.wireName(), d.Preview)), nil
		}
	case "LIGHT":
		if d, ok := m.Data.(LightData); ok && d.Neighbor >= 1 && d.Neighbor <= NodeCount && d.Destination >= 1 && d.Destination <= NodeCount && (d.Direction == "incoming" || d.Direction == "outgoing") {
			return []byte(fmt.Sprintf("LIGHT %s %s %s", d.Neighbor.wireName(), d.Destination.wireName(), d.Direction)), nil
		}
	case "BUFFER":
		if d, ok := m.Data.(BufferData); ok && d.Count >= 0 && d.Count <= botCapacity {
			return []byte(fmt.Sprintf("BUFFER %d", d.Count)), nil
		}
	case "CLEAR":
		return []byte("CLEAR"), nil
	default:
		return json.Marshal(m)
	}
	return nil, fmt.Errorf("invalid %s data", m.Type)
}

type PhaseData struct {
	Phase Phase `json:"phase"`
}
type ResetData struct {
	Sync    string       `json:"sync"`
	Packets []PacketView `json:"packets"`
}
type ResultData struct {
	Sync string `json:"sync"`
	RID  uint32 `json:"rid"`
	OK   bool   `json:"ok"`
}

type NodeSnapshot struct {
	ID              NodeID  `json:"id"`
	Kind            string  `json:"kind"`
	Enabled         bool    `json:"enabled"`
	FIFOUsage       int     `json:"fifo_usage"`
	Score           int     `json:"score"`
	RankingEligible bool    `json:"ranking_eligible"`
	Rate            float64 `json:"rate"`
	Ready           bool    `json:"ready"`
	Rank            *int    `json:"rank"`
	// TopPercent is 1-100 for a player who has one, otherwise nil. Unlike the
	// summary it includes values above 50.
	TopPercent *int `json:"top_percent"`
}

type DeviceSnapshot struct {
	ID             string     `json:"id"`
	Node           NodeID     `json:"node"`
	Session        SessionID  `json:"session"`
	Connected      bool       `json:"connected"`
	Kind           DeviceKind `json:"kind"`
	AssignmentKind DeviceKind `json:"assignment_kind,omitempty"`
}

type PreviewSnapshot struct {
	DeviceID string     `json:"device_id"`
	Kind     DeviceKind `json:"kind"`
	Node     NodeID     `json:"node"`
}

// BestScore is a best score of rules.md: the score, every node that has it in
// node order, and when its game finished.
type BestScore struct {
	Score      int       `json:"score"`
	Nodes      []NodeID  `json:"nodes"`
	FinishedAt time.Time `json:"finished_at"`
}

// HourlyBest is the best score of the JST hour that begins at HourStart.
type HourlyBest struct {
	HourStart time.Time `json:"hour_start"`
	BestScore
}

// RecordsSnapshot holds the best scores of server.md 3.5.5 and the state of
// the records file.
type RecordsSnapshot struct {
	// GameBest is the current game's best while Finished, otherwise nil.
	GameBest *BestScore `json:"game_best"`
	// HourlyBests is newest hour first.
	HourlyBests []HourlyBest `json:"hourly_bests"`
	AllTimeBest *BestScore   `json:"all_time_best"`
	// Available is false if the records file could not be used at startup;
	// the bests then count only the games of this process.
	Available         bool   `json:"available"`
	UnavailableReason string `json:"unavailable_reason"`
	// SaveFailed reports whether the latest save failed.
	SaveFailed bool `json:"save_failed"`
}

type Snapshot struct {
	Phase     Phase         `json:"phase"`
	Elapsed   time.Duration `json:"elapsed"`
	Remaining time.Duration `json:"remaining"`
	// RemainingSeconds is the value MCUs receive as status.remaining.
	RemainingSeconds int `json:"remaining_seconds"`
	// CountdownSeconds is the value MCUs receive as status.countdown.
	CountdownSeconds int              `json:"countdown_seconds"`
	Nodes            []NodeSnapshot   `json:"nodes"`
	Devices          []DeviceSnapshot `json:"devices"`
	CanStart         bool             `json:"can_start"`
	StartBlockers    []StartBlocker   `json:"start_blockers"`
	Preview          *PreviewSnapshot `json:"preview,omitempty"`
	Records          RecordsSnapshot  `json:"records"`
}
