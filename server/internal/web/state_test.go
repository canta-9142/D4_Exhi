package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/canta-9142/d4-exhi/internal/app"
)

// gameSeconds is the game length in whole seconds.
const gameSeconds = int(app.GameDuration / time.Second)

// stateApp returns a fixed Snapshot and records how State was called.
type stateApp struct {
	snapshot app.Snapshot
	err      error
	calls    int
	ctx      context.Context
}

func (a *stateApp) State(ctx context.Context) (app.Snapshot, error) {
	a.calls++
	a.ctx = ctx
	return a.snapshot, a.err
}

func (a *stateApp) Execute(context.Context, app.Command) error { return nil }

// baseSnapshot is a Preparing game with every node in auto mode and no
// devices.
func baseSnapshot() app.Snapshot {
	s := app.Snapshot{
		Phase: app.Preparing, Remaining: app.GameDuration, RemainingSeconds: gameSeconds,
		CanStart: true, StartBlockers: []app.StartBlocker{}, Devices: []app.DeviceSnapshot{},
		Records: app.RecordsSnapshot{HourlyBests: []app.HourlyBest{}, Available: true},
	}
	for id := app.NodeID(1); id <= app.NodeCount; id++ {
		n := app.NodeSnapshot{ID: id, Kind: "player"}
		if id > app.PlayerCount {
			n.Kind, n.Rate = "bot", 1
		}
		s.Nodes = append(s.Nodes, n)
	}
	return s
}

// stateBody is the decoded body of a successful GET /api/state, keeping each
// value as written.
type stateBody struct {
	Game    map[string]json.RawMessage
	Players []map[string]json.RawMessage
	Bots    []map[string]json.RawMessage
	Devices json.RawMessage
	Preview json.RawMessage
}

func getState(t *testing.T, s app.Snapshot) (*httptest.ResponseRecorder, stateBody) {
	t.Helper()
	w := serve(t, New(&stateApp{snapshot: s}, nil), http.MethodGet, "/api/state")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body %s", w.Code, w.Body)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	var body stateBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON %s: %v", w.Body, err)
	}
	return w, body
}

func compact(t *testing.T, s string) string {
	t.Helper()
	var b bytes.Buffer
	if err := json.Compact(&b, []byte(s)); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// The web.md 3.1 example, with every player and bot included.
func TestStateExample(t *testing.T) {
	s := baseSnapshot()
	s.CanStart = false
	s.StartBlockers = []app.StartBlocker{{Code: app.CodePlayersNotReady, Nodes: []app.NodeID{1}}}
	s.Nodes[0].Enabled = true
	s.Devices = []app.DeviceSnapshot{
		{ID: "001122334455", Kind: app.LEDDevice, Connected: true, Session: "s1"},
		{ID: "AABBCCDDEEFF", Kind: app.PlayerDevice, Connected: true, Session: "s2", Node: 1, AssignmentKind: app.PlayerDevice},
	}
	s.Preview = &app.PreviewSnapshot{DeviceID: "001122334455", Kind: app.LEDDevice, Node: 1}
	best := app.BestScore{Score: 812, Nodes: []app.NodeID{3}, FinishedAt: time.Date(2026, 10, 28, 13, 5, 12, 0, app.JST)}
	s.Records.HourlyBests = []app.HourlyBest{{HourStart: time.Date(2026, 10, 28, 13, 0, 0, 0, app.JST), BestScore: best}}
	s.Records.AllTimeBest = &best

	w, _ := getState(t, s)
	want := compact(t, `{
  "game": {
    "phase": "Preparing",
    "durationSeconds": 90,
    "elapsedMs": 0,
    "remainingSeconds": 90,
    "countdownSeconds": 0,
    "canStart": false,
    "startBlockers": [
      {"code": "players_not_ready", "nodeIds": ["P1"]}
    ]
  },
  "players": [
    {"nodeId": "P1", "mode": "manual", "ready": false, "fifoLength": 0, "score": 0, "rankingEligible": null, "rank": null, "topPercent": null},
    {"nodeId": "P2", "mode": "auto", "ready": false, "fifoLength": 0, "score": 0, "rankingEligible": null, "rank": null, "topPercent": null},
    {"nodeId": "P3", "mode": "auto", "ready": false, "fifoLength": 0, "score": 0, "rankingEligible": null, "rank": null, "topPercent": null},
    {"nodeId": "P4", "mode": "auto", "ready": false, "fifoLength": 0, "score": 0, "rankingEligible": null, "rank": null, "topPercent": null},
    {"nodeId": "P5", "mode": "auto", "ready": false, "fifoLength": 0, "score": 0, "rankingEligible": null, "rank": null, "topPercent": null},
    {"nodeId": "P6", "mode": "auto", "ready": false, "fifoLength": 0, "score": 0, "rankingEligible": null, "rank": null, "topPercent": null},
    {"nodeId": "P7", "mode": "auto", "ready": false, "fifoLength": 0, "score": 0, "rankingEligible": null, "rank": null, "topPercent": null},
    {"nodeId": "P8", "mode": "auto", "ready": false, "fifoLength": 0, "score": 0, "rankingEligible": null, "rank": null, "topPercent": null}
  ],
  "bots": [
    {"nodeId": "B1", "fifoLength": 0, "ratePps": 1},
    {"nodeId": "B2", "fifoLength": 0, "ratePps": 1},
    {"nodeId": "B3", "fifoLength": 0, "ratePps": 1}
  ],
  "devices": [
    {"deviceId": "001122334455", "kind": "led", "connected": true, "assignment": null},
    {"deviceId": "AABBCCDDEEFF", "kind": "player", "connected": true, "assignment": {"kind": "player", "nodeId": "P1"}}
  ],
  "preview": {"deviceId": "001122334455", "nodeId": "P1"},
  "records": {
    "gameBest": null,
    "hourlyBests": [
      {"hourStart": "2026-10-28T13:00:00+09:00", "score": 812, "nodeIds": ["P3"], "finishedAt": "2026-10-28T13:05:12+09:00"}
    ],
    "allTimeBest": {"score": 812, "nodeIds": ["P3"], "finishedAt": "2026-10-28T13:05:12+09:00"},
    "available": true,
    "unavailableReason": null,
    "saveFailed": false
  }
}`) + "\n"
	if got := w.Body.String(); got != want {
		t.Errorf("body\n got %s\nwant %s", got, want)
	}
}

// A Playing game with values in every player and bot field.
func TestStatePlaying(t *testing.T) {
	s := baseSnapshot()
	s.Phase, s.Elapsed, s.RemainingSeconds = app.Playing, 30*time.Second, gameSeconds-30
	s.CanStart, s.StartBlockers = false, []app.StartBlocker{{Code: app.CodeNotPreparing}}
	s.Nodes[2] = app.NodeSnapshot{ID: 3, Kind: "player", Enabled: true, Ready: true, FIFOUsage: 5, Score: 2147483647, RankingEligible: true}
	s.Nodes[9] = app.NodeSnapshot{ID: 10, Kind: "bot", FIFOUsage: 4, Rate: 2.5}

	w, body := getState(t, s)
	wantGame := `{"phase":"Playing","durationSeconds":90,"elapsedMs":30000,"remainingSeconds":60,"countdownSeconds":0,"canStart":false,"startBlockers":[{"code":"not_preparing"}]}`
	if got := compact(t, string(mustField(t, w, "game"))); got != wantGame {
		t.Errorf("game = %s, want %s", got, wantGame)
	}
	wantP3 := `{"nodeId":"P3","mode":"manual","ready":true,"fifoLength":5,"score":2147483647,"rankingEligible":true,"rank":null,"topPercent":null}`
	if got := compact(t, string(mustField(t, w, "players", 2))); got != wantP3 {
		t.Errorf("players[2] = %s, want %s", got, wantP3)
	}
	wantB2 := `{"nodeId":"B2","fifoLength":4,"ratePps":2.5}`
	if got := compact(t, string(mustField(t, w, "bots", 1))); got != wantB2 {
		t.Errorf("bots[1] = %s, want %s", got, wantB2)
	}
	if got := string(body.Players[0]["rankingEligible"]); got != "false" {
		t.Errorf("players[0].rankingEligible = %s, want false", got)
	}
}

// mustField returns the raw JSON at key, optionally indexed into an array.
func mustField(t *testing.T, w *httptest.ResponseRecorder, key string, index ...int) json.RawMessage {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &top); err != nil {
		t.Fatal(err)
	}
	raw := top[key]
	if len(index) == 0 {
		return raw
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil || index[0] >= len(items) {
		t.Fatalf("%s = %s: no element %d", key, raw, index[0])
	}
	return items[index[0]]
}

func TestStateEmpty(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    app.Snapshot
	}{
		{"nil slices", app.Snapshot{Phase: app.Preparing, CanStart: true}},
		{"empty slices", app.Snapshot{Phase: app.Preparing, CanStart: true,
			Nodes: []app.NodeSnapshot{}, Devices: []app.DeviceSnapshot{}, StartBlockers: []app.StartBlocker{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, body := getState(t, tc.s)
			for key, want := range map[string]string{"players": "[]", "bots": "[]", "devices": "[]", "preview": "null"} {
				if got := string(mustField(t, w, key)); got != want {
					t.Errorf("%s = %s, want %s", key, got, want)
				}
			}
			if got := string(body.Game["startBlockers"]); got != "[]" {
				t.Errorf("startBlockers = %s, want []", got)
			}
			var records map[string]json.RawMessage
			if err := json.Unmarshal(mustField(t, w, "records"), &records); err != nil {
				t.Fatal(err)
			}
			for key, want := range map[string]string{"gameBest": "null", "hourlyBests": "[]", "allTimeBest": "null"} {
				if got := string(records[key]); got != want {
					t.Errorf("records.%s = %s, want %s", key, got, want)
				}
			}
		})
	}
}

func TestStateRecords(t *testing.T) {
	// A tie listed out of order, finishing just before 14:00 JST in UTC.
	tie := app.BestScore{Score: 300, Nodes: []app.NodeID{3, 1}, FinishedAt: time.Date(2026, 10, 28, 4, 59, 59, 999_000_000, time.UTC)}
	later := app.BestScore{Score: 250, Nodes: []app.NodeID{8}, FinishedAt: time.Date(2026, 10, 28, 14, 0, 0, 0, app.JST)}
	s := baseSnapshot()
	s.Phase = app.Finished
	s.Records = app.RecordsSnapshot{
		GameBest: &later, AllTimeBest: &tie, SaveFailed: true, Available: true,
		HourlyBests: []app.HourlyBest{
			{HourStart: time.Date(2026, 10, 28, 5, 0, 0, 0, time.UTC), BestScore: later},
			{HourStart: time.Date(2026, 10, 28, 13, 0, 0, 0, app.JST), BestScore: tie},
		},
	}
	w, _ := getState(t, s)
	want := compact(t, `{
  "gameBest": {"score": 250, "nodeIds": ["P8"], "finishedAt": "2026-10-28T14:00:00+09:00"},
  "hourlyBests": [
    {"hourStart": "2026-10-28T14:00:00+09:00", "score": 250, "nodeIds": ["P8"], "finishedAt": "2026-10-28T14:00:00+09:00"},
    {"hourStart": "2026-10-28T13:00:00+09:00", "score": 300, "nodeIds": ["P1", "P3"], "finishedAt": "2026-10-28T13:59:59+09:00"}
  ],
  "allTimeBest": {"score": 300, "nodeIds": ["P1", "P3"], "finishedAt": "2026-10-28T13:59:59+09:00"},
  "available": true,
  "unavailableReason": null,
  "saveFailed": true
}`)
	if got := compact(t, string(mustField(t, w, "records"))); got != want {
		t.Errorf("records\n got %s\nwant %s", got, want)
	}
	if tie.Nodes[0] != 3 {
		t.Error("the conversion reordered app's node slice")
	}

	s = baseSnapshot()
	s.Records = app.RecordsSnapshot{Available: false, UnavailableReason: "open: file is not a database"}
	w, _ = getState(t, s)
	want = `{"gameBest":null,"hourlyBests":[],"allTimeBest":null,"available":false,"unavailableReason":"open: file is not a database","saveFailed":false}`
	if got := compact(t, string(mustField(t, w, "records"))); got != want {
		t.Errorf("unavailable records\n got %s\nwant %s", got, want)
	}

	s = baseSnapshot()
	s.Records.AllTimeBest = &app.BestScore{Score: 1, Nodes: []app.NodeID{9}}
	if w := serve(t, New(&stateApp{snapshot: s}, nil), http.MethodGet, "/api/state"); w.Code != http.StatusInternalServerError {
		t.Errorf("a bot best: status %d, want 500", w.Code)
	}
}

func TestStateRank(t *testing.T) {
	rank := func(n int) *int { return &n }
	// Scores 300, 300, 100 and an ineligible 900: app gives the three ranked
	// players 1, 1 and 3 and leaves the fourth without a rank.
	finished := func() app.Snapshot {
		s := baseSnapshot()
		s.Phase = app.Finished
		scores := []int{300, 300, 100, 900}
		ranks := []*int{rank(1), rank(1), rank(3), nil}
		for i, score := range scores {
			s.Nodes[i].Score, s.Nodes[i].RankingEligible, s.Nodes[i].Rank = score, ranks[i] != nil, ranks[i]
		}
		return s
	}
	for _, tc := range []struct {
		name string
		s    app.Snapshot
		want []string
	}{
		{"Preparing", func() app.Snapshot { s := baseSnapshot(); s.Phase = app.Preparing; return s }(),
			[]string{"null", "null", "null", "null"}},
		{"Playing", func() app.Snapshot {
			s := baseSnapshot()
			s.Phase = app.Playing
			for i := range s.Nodes {
				s.Nodes[i].RankingEligible = true
			}
			return s
		}(), []string{"null", "null", "null", "null"}},
		{"Finished", finished(), []string{"1", "1", "3", "null"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, body := getState(t, tc.s)
			if len(body.Players) != app.PlayerCount {
				t.Fatalf("got %d players, want %d", len(body.Players), app.PlayerCount)
			}
			for i, p := range body.Players {
				raw, ok := p["rank"]
				if !ok {
					t.Fatalf("players[%d] has no rank: %s", i, mustField(t, w, "players", i))
				}
				if i < len(tc.want) && string(raw) != tc.want[i] {
					t.Errorf("players[%d].rank = %s, want %s", i, raw, tc.want[i])
				}
				if i >= len(tc.want) && string(raw) != "null" {
					t.Errorf("players[%d].rank = %s, want null", i, raw)
				}
			}
		})
	}
}

// topPercent is passed through as app gives it, including the values above 50
// that the player summary leaves out (web.md 3.2).
func TestStateTopPercent(t *testing.T) {
	percent := func(n int) *int { return &n }
	for _, tc := range []struct {
		name    string
		phase   app.Phase
		percent []*int
		want    []string
	}{
		{"Preparing", app.Preparing, nil, []string{"null", "null", "null", "null"}},
		{"Playing", app.Playing, nil, []string{"null", "null", "null", "null"}},
		{"Finished without percents", app.Finished, nil, []string{"null", "null", "null", "null"}},
		{"Finished", app.Finished, []*int{percent(1), percent(50), percent(51), nil}, []string{"1", "50", "51", "null"}},
		{"Finished at the largest", app.Finished, []*int{percent(100)}, []string{"100", "null", "null", "null"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := baseSnapshot()
			s.Phase = tc.phase
			for i, p := range tc.percent {
				s.Nodes[i].TopPercent = p
			}
			w, body := getState(t, s)
			for i, p := range body.Players {
				raw, ok := p["topPercent"]
				if !ok {
					t.Fatalf("players[%d] has no topPercent: %s", i, mustField(t, w, "players", i))
				}
				want := "null"
				if i < len(tc.want) {
					want = tc.want[i]
				}
				if string(raw) != want {
					t.Errorf("players[%d].topPercent = %s, want %s", i, raw, want)
				}
			}
		})
	}
}

func TestStateRankingEligible(t *testing.T) {
	for _, tc := range []struct {
		phase    app.Phase
		eligible bool
		want     string
	}{
		{app.Preparing, false, "null"},
		{app.Preparing, true, "null"},
		{app.Playing, false, "false"},
		{app.Playing, true, "true"},
		{app.Finished, false, "false"},
		{app.Finished, true, "true"},
	} {
		t.Run(fmt.Sprintf("%s %t", tc.phase, tc.eligible), func(t *testing.T) {
			s := baseSnapshot()
			s.Phase = tc.phase
			for i := range s.Nodes {
				s.Nodes[i].RankingEligible = tc.eligible
			}
			_, body := getState(t, s)
			for i, p := range body.Players {
				if got := string(p["rankingEligible"]); got != tc.want {
					t.Errorf("players[%d].rankingEligible = %s, want %s", i, got, tc.want)
				}
			}
		})
	}
}

func TestStateTime(t *testing.T) {
	for _, tc := range []struct {
		elapsed   time.Duration
		remaining int
		wantMs    string
	}{
		{0, gameSeconds, "0"},
		{999 * time.Microsecond, gameSeconds, "0"},
		{time.Millisecond, gameSeconds, "1"},
		{1999999 * time.Nanosecond, gameSeconds, "1"},
		{61*time.Second + 234*time.Millisecond + 999999, 29, "61234"},
		{app.GameDuration, 0, "90000"},
	} {
		t.Run(tc.elapsed.String(), func(t *testing.T) {
			s := baseSnapshot()
			s.Phase, s.Elapsed, s.RemainingSeconds = app.Playing, tc.elapsed, tc.remaining
			_, body := getState(t, s)
			if got := string(body.Game["elapsedMs"]); got != tc.wantMs {
				t.Errorf("elapsedMs = %s, want %s", got, tc.wantMs)
			}
			if got, want := string(body.Game["remainingSeconds"]), fmt.Sprint(tc.remaining); got != want {
				t.Errorf("remainingSeconds = %s, want %s", got, want)
			}
			if got := string(body.Game["durationSeconds"]); got != "90" {
				t.Errorf("durationSeconds = %s, want 90", got)
			}
		})
	}
}

// countdownSeconds is written in every phase, including its zero value.
func TestStateCountdown(t *testing.T) {
	for _, tc := range []struct {
		phase     app.Phase
		countdown int
	}{
		{app.Preparing, 0},
		{app.Playing, 3},
		{app.Playing, 2},
		{app.Playing, 1},
		{app.Playing, 0},
		{app.Finished, 0},
	} {
		t.Run(fmt.Sprintf("%s/%d", tc.phase, tc.countdown), func(t *testing.T) {
			s := baseSnapshot()
			s.Phase, s.CountdownSeconds = tc.phase, tc.countdown
			_, body := getState(t, s)
			got, ok := body.Game["countdownSeconds"]
			if !ok {
				t.Fatal("countdownSeconds is missing")
			}
			if want := fmt.Sprint(tc.countdown); string(got) != want {
				t.Errorf("countdownSeconds = %s, want %s", got, want)
			}
			if got := string(body.Game["remainingSeconds"]); got != fmt.Sprint(gameSeconds) {
				t.Errorf("remainingSeconds = %s, want %d", got, gameSeconds)
			}
		})
	}
}

func TestStateStartBlockers(t *testing.T) {
	for _, tc := range []struct {
		name     string
		blockers []app.StartBlocker
		want     string
	}{
		{"none", nil, `[]`},
		{"not preparing", []app.StartBlocker{{Code: app.CodeNotPreparing}}, `[{"code":"not_preparing"}]`},
		{"all", []app.StartBlocker{
			{Code: app.CodeNotPreparing},
			{Code: app.CodePIDExhausted},
			{Code: app.CodePlayersNotReady, Nodes: []app.NodeID{1, 8}},
		}, `[{"code":"not_preparing"},{"code":"pid_exhausted"},{"code":"players_not_ready","nodeIds":["P1","P8"]}]`},
		{"nodes in number order", []app.StartBlocker{{Code: app.CodePlayersNotReady, Nodes: []app.NodeID{5, 2, 7}}},
			`[{"code":"players_not_ready","nodeIds":["P2","P5","P7"]}]`},
		// nodeIds belongs to players_not_ready alone and is always present there.
		{"players without nodes", []app.StartBlocker{{Code: app.CodePlayersNotReady}},
			`[{"code":"players_not_ready","nodeIds":[]}]`},
		{"nodes on other code", []app.StartBlocker{{Code: app.CodePIDExhausted, Nodes: []app.NodeID{1}}},
			`[{"code":"pid_exhausted"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := baseSnapshot()
			s.StartBlockers, s.CanStart = tc.blockers, len(tc.blockers) == 0
			_, body := getState(t, s)
			if got := string(body.Game["startBlockers"]); got != tc.want {
				t.Errorf("startBlockers = %s, want %s", got, tc.want)
			}
			if got, want := string(body.Game["canStart"]), fmt.Sprint(len(tc.blockers) == 0); got != want {
				t.Errorf("canStart = %s, want %s", got, want)
			}
		})
	}
}

// Players and bots are split by kind and listed in number order whatever the
// order in the Snapshot.
func TestStateNodeOrder(t *testing.T) {
	s := baseSnapshot()
	for i, j := 0, len(s.Nodes)-1; i < j; i, j = i+1, j-1 {
		s.Nodes[i], s.Nodes[j] = s.Nodes[j], s.Nodes[i]
	}
	_, body := getState(t, s)
	var players, bots []string
	for _, p := range body.Players {
		players = append(players, string(p["nodeId"]))
	}
	for _, b := range body.Bots {
		bots = append(bots, string(b["nodeId"]))
	}
	if got, want := fmt.Sprint(players), `["P1" "P2" "P3" "P4" "P5" "P6" "P7" "P8"]`; got != want {
		t.Errorf("players = %s, want %s", got, want)
	}
	if got, want := fmt.Sprint(bots), `["B1" "B2" "B3"]`; got != want {
		t.Errorf("bots = %s, want %s", got, want)
	}
}

func TestStateDevices(t *testing.T) {
	s := baseSnapshot()
	// web keeps the order app returns.
	s.Devices = []app.DeviceSnapshot{
		{ID: "ff", Kind: app.LEDDevice, Node: 10, AssignmentKind: app.LEDDevice},
		{ID: "aa", Kind: app.PlayerDevice, Connected: true, Session: "s", Node: 8, AssignmentKind: app.PlayerDevice},
		{ID: "cc", Kind: app.LEDDevice, Node: 7, AssignmentKind: app.LEDDevice, Connected: true},
		{ID: "bb", Kind: app.PlayerDevice},
	}
	s.Preview = &app.PreviewSnapshot{DeviceID: "bb", Kind: app.PlayerDevice, Node: 1}
	_, body := getState(t, s)
	want := `[{"deviceId":"ff","kind":"led","connected":false,"assignment":{"kind":"led","nodeId":"B2"}},` +
		`{"deviceId":"aa","kind":"player","connected":true,"assignment":{"kind":"player","nodeId":"P8"}},` +
		`{"deviceId":"cc","kind":"led","connected":true,"assignment":{"kind":"led","nodeId":"P7"}},` +
		`{"deviceId":"bb","kind":"player","connected":false,"assignment":null}]`
	if got := string(body.Devices); got != want {
		t.Errorf("devices\n got %s\nwant %s", got, want)
	}
	if got, want := string(body.Preview), `{"deviceId":"bb","nodeId":"P1"}`; got != want {
		t.Errorf("preview = %s, want %s", got, want)
	}
}

// The normal configuration of web.md 3.2: 11 logical nodes, and 15 devices
// made of 8 player boards and 7 LED boards. A player board and an LED board
// on the same node are separate devices.
func TestStateFullConfiguration(t *testing.T) {
	s := baseSnapshot()
	var devices []string
	for _, d := range []struct {
		kind app.DeviceKind
		node app.NodeID
		name string
	}{
		{app.PlayerDevice, 1, "P1"}, {app.PlayerDevice, 2, "P2"}, {app.PlayerDevice, 3, "P3"},
		{app.PlayerDevice, 4, "P4"}, {app.PlayerDevice, 5, "P5"}, {app.PlayerDevice, 6, "P6"},
		{app.PlayerDevice, 7, "P7"}, {app.PlayerDevice, 8, "P8"},
		{app.LEDDevice, 9, "B1"}, {app.LEDDevice, 10, "B2"}, {app.LEDDevice, 11, "B3"},
		{app.LEDDevice, 1, "P1"}, {app.LEDDevice, 3, "P3"}, {app.LEDDevice, 5, "P5"}, {app.LEDDevice, 7, "P7"},
	} {
		id := fmt.Sprintf("%02x", len(s.Devices)+1)
		s.Devices = append(s.Devices, app.DeviceSnapshot{
			ID: id, Kind: d.kind, Connected: true, Session: "s", Node: d.node, AssignmentKind: d.kind,
		})
		devices = append(devices, fmt.Sprintf(`{"deviceId":%q,"kind":%q,"connected":true,"assignment":{"kind":%q,"nodeId":%q}}`,
			id, d.kind, d.kind, d.name))
	}
	_, body := getState(t, s)
	if len(body.Players) != 8 || len(body.Bots) != 3 {
		t.Errorf("got %d players and %d bots, want 8 and 3", len(body.Players), len(body.Bots))
	}
	if got, want := string(body.Devices), "["+strings.Join(devices, ",")+"]"; got != want {
		t.Errorf("devices\n got %s\nwant %s", got, want)
	}
}

type ctxKey struct{}

// The response is built from one State call made with the request context.
func TestStateCallsAppOnce(t *testing.T) {
	a := &stateApp{snapshot: baseSnapshot()}
	h := New(a, nil)
	ctx := context.WithValue(context.Background(), ctxKey{}, "request")
	r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/state", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body %s", w.Code, w.Body)
	}
	if a.calls != 1 {
		t.Errorf("State called %d times, want 1", a.calls)
	}
	if a.ctx == nil || a.ctx.Value(ctxKey{}) != "request" {
		t.Errorf("State was not called with the request context")
	}
}

func TestStateErrors(t *testing.T) {
	invalid := func(change func(*app.Snapshot)) app.Snapshot {
		s := baseSnapshot()
		change(&s)
		return s
	}
	for _, tc := range []struct {
		name     string
		snapshot app.Snapshot
		err      error
		status   int
		code     errorCode
	}{
		{"stopped", app.Snapshot{}, app.ErrStopped, http.StatusServiceUnavailable, codeUnavailable},
		{"wrapped stopped", app.Snapshot{}, fmt.Errorf("state: %w", app.ErrStopped), http.StatusServiceUnavailable, codeUnavailable},
		{"canceled", app.Snapshot{}, context.Canceled, http.StatusInternalServerError, codeInternalError},
		{"other", app.Snapshot{}, errors.New("boom"), http.StatusInternalServerError, codeInternalError},
		// Snapshots app never produces must not become malformed JSON.
		{"player out of range", invalid(func(s *app.Snapshot) { s.Nodes[0].ID = 9 }), nil, http.StatusInternalServerError, codeInternalError},
		{"bot out of range", invalid(func(s *app.Snapshot) { s.Nodes[10].ID = 12 }), nil, http.StatusInternalServerError, codeInternalError},
		{"unknown node kind", invalid(func(s *app.Snapshot) { s.Nodes[0].Kind = "" }), nil, http.StatusInternalServerError, codeInternalError},
		{"blocked bot", invalid(func(s *app.Snapshot) {
			s.StartBlockers = []app.StartBlocker{{Code: app.CodePlayersNotReady, Nodes: []app.NodeID{9}}}
		}), nil, http.StatusInternalServerError, codeInternalError},
		{"assignment to node 0", invalid(func(s *app.Snapshot) {
			s.Devices = []app.DeviceSnapshot{{ID: "aa", Kind: app.PlayerDevice, AssignmentKind: app.PlayerDevice}}
		}), nil, http.StatusInternalServerError, codeInternalError},
		{"preview of node 0", invalid(func(s *app.Snapshot) {
			s.Preview = &app.PreviewSnapshot{DeviceID: "aa", Kind: app.PlayerDevice}
		}), nil, http.StatusInternalServerError, codeInternalError},
		{"infinite rate", invalid(func(s *app.Snapshot) { s.Nodes[8].Rate = math.Inf(1) }), nil, http.StatusInternalServerError, codeInternalError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &stateApp{snapshot: tc.snapshot, err: tc.err}
			checkError(t, serve(t, New(a, nil), http.MethodGet, "/api/state"), tc.status, tc.code)
			if a.calls != 1 {
				t.Errorf("State called %d times, want 1", a.calls)
			}
		})
	}
}
