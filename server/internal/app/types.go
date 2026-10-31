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
	Start Operation = "Start"
	// Stop and ReturnToBot are for endless operation, Finish and Reset for
	// the simultaneous game (server.md 2.2).
	Stop                    Operation = "Stop"
	ReturnToBot             Operation = "ReturnToBot"
	Finish                  Operation = "Finish"
	Reset                   Operation = "Reset"
	SetNodeEnabled          Operation = "SetNodeEnabled"
	SetAllNodesEnabled      Operation = "SetAllNodesEnabled"
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

// Result is what ExecuteWithResult returns for an accepted command.
type Result struct {
	// Skipped lists, in ascending order, the player nodes that
	// SetAllNodesEnabled could not make manual because their MCU is not
	// connected and identified. They stay automatic.
	Skipped []NodeID
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
	// Round is a CHOOSE's selection, "normal" or "tutorial".
	Round string
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
	// Round is the kind of the station's round, or nil (player.md).
	Round *RoundKind `json:"round"`
	// Window is how many seconds the station has before it times out.
	Window int `json:"window"`
	// Rank is the simultaneous game's provisional rank while the game time
	// runs, its settled rank once Finished, and nil otherwise, for an
	// ineligible player and in endless operation.
	Rank *int `json:"rank"`
	// Gains are the points earned since the last status to this session, in
	// order. They are [] rather than null, and flat in data for the player
	// firmware's JSON nesting limit (player.md).
	Gains []int `json:"gains"`
	// Combo is shown only and never affects the score (server.md 4.4).
	Combo int `json:"combo"`
	// Bots is the FIFO usage of B1-B3, only while practicing (server.md
	// 3.6.1); the field is absent otherwise.
	Bots []int `json:"bots,omitempty"`
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
	// BottomHalf is true when the player is in the bottom half: the summary
	// then says only that, with TopPercent nil.
	BottomHalf bool `json:"bottomHalf"`
	// The best scores of server.md 3.5.7 stay flat in data, because the
	// player firmware limits JSON nesting to 4 (player.md).
	GameBest *int `json:"gameBest"`
	// GameBestNodes is ints, not []NodeID, which would encode as base64.
	GameBestNodes   []int `json:"gameBestNodes"`
	HourBest        *int  `json:"hourBest"`
	HourBestStart   *int  `json:"hourBestStart"`
	HourBestMine    bool  `json:"hourBestMine"`
	AllTimeBest     *int  `json:"allTimeBest"`
	AllTimeBestMine bool  `json:"allTimeBestMine"`
}
type LightData struct {
	Neighbor    NodeID
	Destination NodeID
	Direction   string
}

// CheerData sends light out of Node, a player with the game's best score,
// along the link to Neighbor in the colors of Style (led.md).
type CheerData struct {
	Neighbor NodeID
	Node     NodeID
	Style    string
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
	case "CHEER":
		if d, ok := m.Data.(CheerData); ok && d.Neighbor.player() && d.Node.player() && (d.Style == "game" || d.Style == "hour" || d.Style == "record") {
			return []byte(fmt.Sprintf("CHEER %s %s %s", d.Neighbor.wireName(), d.Node.wireName(), d.Style)), nil
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
	// Station and the rest are a player's stage and its times, the values
	// that its MCU receives in phase and status; empty or 0 for a bot.
	Station          Station   `json:"station,omitempty"`
	Round            RoundKind `json:"round,omitempty"`
	CountdownSeconds int       `json:"countdown_seconds"`
	RemainingSeconds int       `json:"remaining_seconds"`
	WindowSeconds    int       `json:"window_seconds"`
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
	// GameBest is the simultaneous game's best while Finished, otherwise nil.
	GameBest *BestScore `json:"game_best"`
	// RecentRounds is the normal rounds settled since startup, newest first.
	RecentRounds []RecentRound `json:"recent_rounds"`
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
	Mode GameMode `json:"mode"`
	// Operation is whether endless operation runs; the simultaneous game
	// always runs.
	Operation OperationState `json:"operation"`
	// Phase is the simultaneous game's phase, and empty in endless operation.
	Phase Phase `json:"phase"`
	// Elapsed is the game clock: since Start in endless operation, and the
	// simultaneous game's settled elapsed time.
	Elapsed time.Duration `json:"elapsed"`
	// Remaining, RemainingSeconds and CountdownSeconds are the simultaneous
	// game's, and 0 in endless operation, where each player has its own.
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
