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
}

type gameState struct {
	Phase            app.Phase      `json:"phase"`
	DurationSeconds  int            `json:"durationSeconds"`
	ElapsedMs        int64          `json:"elapsedMs"`
	RemainingSeconds int            `json:"remainingSeconds"`
	CanStart         bool           `json:"canStart"`
	StartBlockers    []startBlocker `json:"startBlockers"`
}

// startBlocker carries nodeIds only for players_not_ready (web.md 3.3), where
// the slice is non-nil so that it is written even when empty.
type startBlocker struct {
	Code    app.Code `json:"code"`
	NodeIDs []string `json:"nodeIds,omitzero"`
}

type playerState struct {
	NodeID     string `json:"nodeId"`
	Mode       string `json:"mode"`
	Ready      bool   `json:"ready"`
	FIFOLength int    `json:"fifoLength"`
	Score      int    `json:"score"`
	// RankingEligible is null while Preparing.
	RankingEligible *bool `json:"rankingEligible"`
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
			Phase:            s.Phase,
			DurationSeconds:  int(app.GameDuration / time.Second),
			ElapsedMs:        s.Elapsed.Milliseconds(),
			RemainingSeconds: s.RemainingSeconds,
			CanStart:         s.CanStart,
			StartBlockers:    make([]startBlocker, 0, len(s.StartBlockers)),
		},
		Players: make([]playerState, 0, app.PlayerCount),
		Bots:    make([]botState, 0, app.NodeCount-app.PlayerCount),
		Devices: make([]deviceState, 0, len(s.Devices)),
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
			var eligible *bool
			if s.Phase != app.Preparing {
				eligible = &n.RankingEligible
			}
			res.Players = append(res.Players, playerState{
				NodeID: name, Mode: mode, Ready: n.Ready, FIFOLength: n.FIFOUsage, Score: n.Score,
				RankingEligible: eligible,
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
	return res, nil
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
