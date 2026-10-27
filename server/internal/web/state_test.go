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
		Phase: app.Preparing, Remaining: app.GameDuration, RemainingSeconds: 120,
		CanStart: true, StartBlockers: []app.StartBlocker{}, Devices: []app.DeviceSnapshot{},
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

	w, _ := getState(t, s)
	want := compact(t, `{
  "game": {
    "phase": "Preparing",
    "durationSeconds": 120,
    "elapsedMs": 0,
    "remainingSeconds": 120,
    "canStart": false,
    "startBlockers": [
      {"code": "players_not_ready", "nodeIds": ["P1"]}
    ]
  },
  "players": [
    {"nodeId": "P1", "mode": "manual", "ready": false, "fifoLength": 0, "score": 0, "rankingEligible": null},
    {"nodeId": "P2", "mode": "auto", "ready": false, "fifoLength": 0, "score": 0, "rankingEligible": null},
    {"nodeId": "P3", "mode": "auto", "ready": false, "fifoLength": 0, "score": 0, "rankingEligible": null},
    {"nodeId": "P4", "mode": "auto", "ready": false, "fifoLength": 0, "score": 0, "rankingEligible": null},
    {"nodeId": "P5", "mode": "auto", "ready": false, "fifoLength": 0, "score": 0, "rankingEligible": null},
    {"nodeId": "P6", "mode": "auto", "ready": false, "fifoLength": 0, "score": 0, "rankingEligible": null},
    {"nodeId": "P7", "mode": "auto", "ready": false, "fifoLength": 0, "score": 0, "rankingEligible": null},
    {"nodeId": "P8", "mode": "auto", "ready": false, "fifoLength": 0, "score": 0, "rankingEligible": null}
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
  "preview": {"deviceId": "001122334455", "nodeId": "P1"}
}`) + "\n"
	if got := w.Body.String(); got != want {
		t.Errorf("body\n got %s\nwant %s", got, want)
	}
}

// A Playing game with values in every player and bot field.
func TestStatePlaying(t *testing.T) {
	s := baseSnapshot()
	s.Phase, s.Elapsed, s.RemainingSeconds = app.Playing, 30*time.Second, 90
	s.CanStart, s.StartBlockers = false, []app.StartBlocker{{Code: app.CodeNotPreparing}}
	s.Nodes[2] = app.NodeSnapshot{ID: 3, Kind: "player", Enabled: true, Ready: true, FIFOUsage: 5, Score: 2147483647, RankingEligible: true}
	s.Nodes[9] = app.NodeSnapshot{ID: 10, Kind: "bot", FIFOUsage: 4, Rate: 2.5}

	w, body := getState(t, s)
	wantGame := `{"phase":"Playing","durationSeconds":120,"elapsedMs":30000,"remainingSeconds":90,"canStart":false,"startBlockers":[{"code":"not_preparing"}]}`
	if got := compact(t, string(mustField(t, w, "game"))); got != wantGame {
		t.Errorf("game = %s, want %s", got, wantGame)
	}
	wantP3 := `{"nodeId":"P3","mode":"manual","ready":true,"fifoLength":5,"score":2147483647,"rankingEligible":true}`
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
		{0, 120, "0"},
		{999 * time.Microsecond, 120, "0"},
		{time.Millisecond, 120, "1"},
		{1999999 * time.Nanosecond, 120, "1"},
		{61*time.Second + 234*time.Millisecond + 999999, 59, "61234"},
		{app.GameDuration, 0, "120000"},
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
			if got := string(body.Game["durationSeconds"]); got != "120" {
				t.Errorf("durationSeconds = %s, want 120", got)
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
