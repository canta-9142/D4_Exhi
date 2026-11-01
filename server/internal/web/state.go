package web

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/canta-9142/d4-exhi/internal/app"
)

// stateResponse is the Snapshot of web.md 3. Fields are never omitted: empty
// arrays are [] and a missing assignment or preview is null.
type stateResponse struct {
	Game    gameState     `json:"game"`
	Players []playerState `json:"players"`
	Bots    []botState    `json:"bots"`
	Devices []deviceState `json:"devices"`
	Preview *previewState `json:"preview"`
	Records recordsState  `json:"records"`
}

// gameState is the game of web.md 3.2 and 3.4. Mode and Operation are app's
// values passed through; Phase, RemainingSeconds and CountdownSeconds are the
// simultaneous game's, and null or 0 in endless operation.
type gameState struct {
	Mode             app.GameMode       `json:"mode"`
	Operation        app.OperationState `json:"operation"`
	Phase            *app.Phase         `json:"phase"`
	DurationSeconds  int                `json:"durationSeconds"`
	ElapsedMs        int64              `json:"elapsedMs"`
	RemainingSeconds int                `json:"remainingSeconds"`
	CountdownSeconds int                `json:"countdownSeconds"`
	CanStart         bool               `json:"canStart"`
	StartBlockers    []startBlocker     `json:"startBlockers"`
}

// startBlocker carries nodeIds only for players_not_ready (web.md 3.3), where
// the slice is non-nil so that it is written even when empty.
type startBlocker struct {
	Code    app.Code `json:"code"`
	NodeIDs []string `json:"nodeIds,omitzero"`
}

type playerState struct {
	NodeID string `json:"nodeId"`
	Mode   string `json:"mode"`
	// Station, Round and the three times are the player's stage, passed
	// through: the values its MCU receives in phase and status (web.md 3.2).
	// Round is null outside playing and finished.
	Station          app.Station    `json:"station"`
	Round            *app.RoundKind `json:"round"`
	CountdownSeconds int            `json:"countdownSeconds"`
	RemainingSeconds int            `json:"remainingSeconds"`
	WindowSeconds    int            `json:"windowSeconds"`
	FIFOLength       int            `json:"fifoLength"`
	Score            int            `json:"score"`
	// Ready, RankingEligible and Rank are the simultaneous game's. In endless
	// operation Ready is false and the other two are null.
	Ready bool `json:"ready"`
	// RankingEligible is null in endless operation and while Preparing.
	RankingEligible *bool `json:"rankingEligible"`
	// Rank is app's final rank, passed through. It is null except for the
	// ranked players of a finished simultaneous game.
	Rank *int `json:"rank"`
	// TopPercent is app's top percent, passed through. It is null except for
	// a player in the finished stage who has one, and includes values above 50.
	TopPercent *int `json:"topPercent"`
}

type botState struct {
	NodeID     string  `json:"nodeId"`
	FIFOLength int     `json:"fifoLength"`
	RatePps    float64 `json:"ratePps"`
}

type deviceState struct {
	DeviceID   string           `json:"deviceId"`
	Kind       app.DeviceKind   `json:"kind"`
	Connected  bool             `json:"connected"`
	Assignment *assignmentState `json:"assignment"`
}

type assignmentState struct {
	Kind   app.DeviceKind `json:"kind"`
	NodeID string         `json:"nodeId"`
}

type previewState struct {
	DeviceID string `json:"deviceId"`
	NodeID   string `json:"nodeId"`
}

// recordsState is the records of web.md 3.2: app's best scores, passed
// through, and whether the records file can be used.
type recordsState struct {
	// RecentRounds is never null: [] when no round has settled.
	RecentRounds []recentRoundState `json:"recentRounds"`
	GameBest     *bestState         `json:"gameBest"`
	HourlyBests  []hourlyBestState  `json:"hourlyBests"`
	AllTimeBest  *bestState         `json:"allTimeBest"`
	Available    bool               `json:"available"`
	// UnavailableReason is null while the records file is available.
	UnavailableReason *string `json:"unavailableReason"`
	SaveFailed        bool    `json:"saveFailed"`
}

type recentRoundState struct {
	NodeID     string `json:"nodeId"`
	Score      int    `json:"score"`
	TopPercent int    `json:"topPercent"`
	FinishedAt string `json:"finishedAt"`
}

type bestState struct {
	Score      int      `json:"score"`
	NodeIDs    []string `json:"nodeIds"`
	FinishedAt string   `json:"finishedAt"`
}

type hourlyBestState struct {
	HourStart string `json:"hourStart"`
	bestState
}

// handleState serves GET /api/state (web.md 3). The response is built only
// from one app.State result, so that every field comes from the same moment.
func (h *handler) handleState(w http.ResponseWriter, r *http.Request) {
	snapshot, err := h.app.State(r.Context())
	if errors.Is(err, app.ErrStopped) {
		writeError(w, http.StatusServiceUnavailable, codeUnavailable, errorMessages[codeUnavailable], nil)
		return
	}
	if err != nil {
		h.logger.Error("app state failed", "error", err)
		writeError(w, http.StatusInternalServerError, codeInternalError, errorMessages[codeInternalError], nil)
		return
	}
	state, err := newStateResponse(snapshot)
	if err != nil {
		h.logger.Error("invalid app snapshot", "error", err)
		writeError(w, http.StatusInternalServerError, codeInternalError, errorMessages[codeInternalError], nil)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// newStateResponse converts s without judging game rules. It fails only for
// node IDs that have no public form, which app never produces.
func newStateResponse(s app.Snapshot) (stateResponse, error) {
	res := stateResponse{
		Game: gameState{
			Mode:             s.Mode,
			Operation:        s.Operation,
			DurationSeconds:  int(app.GameDuration / time.Second),
			ElapsedMs:        s.Elapsed.Milliseconds(),
			RemainingSeconds: s.RemainingSeconds,
			CountdownSeconds: s.CountdownSeconds,
			CanStart:         s.CanStart,
			StartBlockers:    make([]startBlocker, 0, len(s.StartBlockers)),
		},
		Players: make([]playerState, 0, app.PlayerCount),
		Bots:    make([]botState, 0, app.NodeCount-app.PlayerCount),
		Devices: make([]deviceState, 0, len(s.Devices)),
	}

	// Phase is the simultaneous game's; endless operation has none.
	if s.Phase != "" {
		res.Game.Phase = &s.Phase
	}

	for _, b := range s.StartBlockers {
		blocker := startBlocker{Code: b.Code}
		if b.Code == app.CodePlayersNotReady {
			blocker.NodeIDs = make([]string, 0, len(b.Nodes))
			for _, id := range sortedIDs(b.Nodes) {
				name, err := playerName(id)
				if err != nil {
					return stateResponse{}, err
				}
				blocker.NodeIDs = append(blocker.NodeIDs, name)
			}
		}
		res.Game.StartBlockers = append(res.Game.StartBlockers, blocker)
	}

	nodes := slices.Clone(s.Nodes)
	slices.SortStableFunc(nodes, func(a, b app.NodeSnapshot) int { return int(a.ID) - int(b.ID) })
	for _, n := range nodes {
		switch n.Kind {
		case "player":
			name, err := playerName(n.ID)
			if err != nil {
				return stateResponse{}, err
			}
			mode := "auto"
			if n.Enabled {
				mode = "manual"
			}
			// Eligibility belongs to the simultaneous game and is not yet
			// known while it prepares.
			var eligible *bool
			if s.Phase != "" && s.Phase != app.Preparing {
				eligible = &n.RankingEligible
			}
			var round *app.RoundKind
			if n.Round != "" {
				round = &n.Round
			}
			res.Players = append(res.Players, playerState{
				NodeID: name, Mode: mode, Station: n.Station, Round: round,
				CountdownSeconds: n.CountdownSeconds, RemainingSeconds: n.RemainingSeconds, WindowSeconds: n.WindowSeconds,
				FIFOLength: n.FIFOUsage, Score: n.Score,
				Ready: n.Ready, RankingEligible: eligible, Rank: n.Rank, TopPercent: n.TopPercent,
			})
		case "bot":
			name, err := botName(n.ID)
			if err != nil {
				return stateResponse{}, err
			}
			res.Bots = append(res.Bots, botState{NodeID: name, FIFOLength: n.FIFOUsage, RatePps: n.Rate})
		default:
			return stateResponse{}, fmt.Errorf("node %d has unknown kind %q", n.ID, n.Kind)
		}
	}

	for _, d := range s.Devices {
		device := deviceState{DeviceID: d.ID, Kind: d.Kind, Connected: d.Connected}
		if d.AssignmentKind != "" {
			name, err := nodeName(d.Node)
			if err != nil {
				return stateResponse{}, err
			}
			device.Assignment = &assignmentState{Kind: d.AssignmentKind, NodeID: name}
		}
		res.Devices = append(res.Devices, device)
	}

	if p := s.Preview; p != nil {
		name, err := nodeName(p.Node)
		if err != nil {
			return stateResponse{}, err
		}
		res.Preview = &previewState{DeviceID: p.DeviceID, NodeID: name}
	}

	records, err := newRecordsState(s.Records)
	if err != nil {
		return stateResponse{}, err
	}
	res.Records = records
	return res, nil
}

func newRecordsState(r app.RecordsSnapshot) (recordsState, error) {
	res := recordsState{
		RecentRounds: make([]recentRoundState, 0, len(r.RecentRounds)),
		HourlyBests:  make([]hourlyBestState, 0, len(r.HourlyBests)),
		Available:    r.Available, SaveFailed: r.SaveFailed,
	}
	if !r.Available {
		reason := r.UnavailableReason
		res.UnavailableReason = &reason
	}
	var err error
	if res.GameBest, err = newBestState(r.GameBest); err != nil {
		return recordsState{}, err
	}
	if res.AllTimeBest, err = newBestState(r.AllTimeBest); err != nil {
		return recordsState{}, err
	}
	for _, round := range r.RecentRounds {
		name, err := playerName(round.Node)
		if err != nil {
			return recordsState{}, err
		}
		res.RecentRounds = append(res.RecentRounds, recentRoundState{
			NodeID: name, Score: round.Score, TopPercent: round.TopPercent, FinishedAt: jstTime(round.FinishedAt),
		})
	}
	for _, h := range r.HourlyBests {
		best, err := newBestState(&h.BestScore)
		if err != nil {
			return recordsState{}, err
		}
		res.HourlyBests = append(res.HourlyBests, hourlyBestState{HourStart: jstTime(h.HourStart), bestState: *best})
	}
	return res, nil
}

func newBestState(b *app.BestScore) (*bestState, error) {
	if b == nil {
		return nil, nil
	}
	res := &bestState{Score: b.Score, NodeIDs: make([]string, 0, len(b.Nodes)), FinishedAt: jstTime(b.FinishedAt)}
	for _, id := range sortedIDs(b.Nodes) {
		name, err := playerName(id)
		if err != nil {
			return nil, err
		}
		res.NodeIDs = append(res.NodeIDs, name)
	}
	return res, nil
}

// jstTime is t in RFC 3339 at +09:00, truncated to the second (web.md 3.2).
func jstTime(t time.Time) string {
	return t.In(app.JST).Format(time.RFC3339)
}

func sortedIDs(ids []app.NodeID) []app.NodeID {
	ids = slices.Clone(ids)
	slices.Sort(ids)
	return ids
}

// nodeName returns the public node ID of web.md 2: 1-8 are P1-P8 and 9-11
// are B1-B3.
func nodeName(id app.NodeID) (string, error) {
	switch {
	case id >= 1 && id <= app.PlayerCount:
		return fmt.Sprintf("P%d", id), nil
	case id > app.PlayerCount && id <= app.NodeCount:
		return fmt.Sprintf("B%d", id-app.PlayerCount), nil
	}
	return "", fmt.Errorf("node %d has no public ID", id)
}

func playerName(id app.NodeID) (string, error) {
	if id < 1 || id > app.PlayerCount {
		return "", fmt.Errorf("node %d is not a player", id)
	}
	return nodeName(id)
}

func botName(id app.NodeID) (string, error) {
	if id <= app.PlayerCount || id > app.NodeCount {
		return "", fmt.Errorf("node %d is not a bot", id)
	}
	return nodeName(id)
}
