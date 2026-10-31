package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// epochHour is a time in the JST hour of the harness games, which finish
// shortly after epoch (09:16:40 JST).
func epochHour(minute int) time.Time {
	return time.Date(1970, 1, 1, 9, minute, 0, 0, JST)
}

// summaryBests is the best score fields of a summary, with nil as -1 so that
// a failure prints the values.
type summaryBests struct {
	Game            int
	GameNodes       []int
	Hour, HourStart int
	HourMine        bool
	AllTime         int
	AllTimeMine     bool
}

func bestsOf(d SummaryData) summaryBests {
	value := func(p *int) int {
		if p == nil {
			return -1
		}
		return *p
	}
	return summaryBests{
		Game: value(d.GameBest), GameNodes: d.GameBestNodes,
		Hour: value(d.HourBest), HourStart: value(d.HourBestStart), HourMine: d.HourBestMine,
		AllTime: value(d.AllTimeBest), AllTimeMine: d.AllTimeBestMine,
	}
}

func wantBests(t *testing.T, h *harness, id NodeID, want summaryBests) {
	t.Helper()
	if got := bestsOf(h.summary(SessionID(id.wireName()))); !reflect.DeepEqual(got, want) {
		t.Errorf("%s summary bests\n got %+v\nwant %+v", id.wireName(), got, want)
	}
}

// finishManually plays a game until an admin Finish with scores.
func (h *harness) finishManually(scores map[NodeID]int) {
	h.t.Helper()
	h.start()
	h.tick(30 * time.Second)
	h.do(func() {
		for id, score := range scores {
			h.a.game.nodes[id].score = score
		}
	})
	h.execute(Command{Operation: Finish})
}

func TestSummaryBestsWithoutEligiblePlayers(t *testing.T) {
	h := topPercentHarness(t, Options{}, nil, []NodeID{1, 2})
	h.playToTimeout(map[NodeID]int{1: 300, 2: 200})
	for _, id := range []NodeID{1, 2} {
		wantBests(t, h, id, summaryBests{Game: -1, GameNodes: []int{}, Hour: -1, HourStart: -1, AllTime: -1})
	}
	encoded, err := json.Marshal(h.summary("P1"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"gameBest":null`, `"gameBestNodes":[]`, `"hourBest":null`, `"hourBestStart":null`, `"hourBestMine":false`, `"allTimeBest":null`, `"allTimeBestMine":false`} {
		if !strings.Contains(string(encoded), want) {
			t.Errorf("summary %s lacks %s", encoded, want)
		}
	}
}

func TestSummaryBestsOfTiedPlayers(t *testing.T) {
	h := topPercentHarness(t, Options{}, []NodeID{1, 2, 4}, []NodeID{3})
	// P3 is above them but not eligible.
	h.playToTimeout(map[NodeID]int{1: 500, 2: 300, 3: 900, 4: 500})
	tied := summaryBests{Game: 500, GameNodes: []int{1, 4}, Hour: 500, HourStart: 9, HourMine: true, AllTime: 500, AllTimeMine: true}
	other := summaryBests{Game: 500, GameNodes: []int{1, 4}, Hour: 500, HourStart: 9, AllTime: 500}
	wantBests(t, h, 1, tied)
	wantBests(t, h, 2, other)
	wantBests(t, h, 3, other)
	wantBests(t, h, 4, tied)
	if encoded, _ := json.Marshal(h.summary("P1")); !strings.Contains(string(encoded), `"gameBestNodes":[1,4]`) {
		t.Errorf("summary %s lacks the nodes as numbers", encoded)
	}
}

func TestSummaryBestsMine(t *testing.T) {
	// The all-time best of 800 is from another hour; this hour's is 400.
	history := []GameRecord{
		testRecord(jst(12, 0, 0, 0), FinishTimeout, map[NodeID]int{5: 800}, nil),
		testRecord(epochHour(0), FinishTimeout, map[NodeID]int{5: 400}, nil),
	}
	for _, tc := range []struct {
		name   string
		scores map[NodeID]int
		p1, p2 summaryBests
	}{
		{"a new all-time best", map[NodeID]int{1: 900, 2: 600},
			summaryBests{Game: 900, GameNodes: []int{1}, Hour: 900, HourStart: 9, HourMine: true, AllTime: 900, AllTimeMine: true},
			summaryBests{Game: 900, GameNodes: []int{1}, Hour: 900, HourStart: 9, AllTime: 900}},
		{"a new best of the hour only", map[NodeID]int{1: 600, 2: 500},
			summaryBests{Game: 600, GameNodes: []int{1}, Hour: 600, HourStart: 9, HourMine: true, AllTime: 800},
			summaryBests{Game: 600, GameNodes: []int{1}, Hour: 600, HourStart: 9, AllTime: 800}},
		// A tie leaves the bests with the earlier games.
		{"ties with the earlier bests", map[NodeID]int{1: 800, 2: 400},
			summaryBests{Game: 800, GameNodes: []int{1}, Hour: 800, HourStart: 9, HourMine: true, AllTime: 800},
			summaryBests{Game: 800, GameNodes: []int{1}, Hour: 800, HourStart: 9, AllTime: 800}},
		{"no new best", map[NodeID]int{1: 400, 2: 100},
			summaryBests{Game: 400, GameNodes: []int{1}, Hour: 400, HourStart: 9, AllTime: 800},
			summaryBests{Game: 400, GameNodes: []int{1}, Hour: 400, HourStart: 9, AllTime: 800}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := topPercentHarness(t, Options{Records: history}, []NodeID{1, 2}, nil)
			h.playToTimeout(tc.scores)
			wantBests(t, h, 1, tc.p1)
			wantBests(t, h, 2, tc.p2)
		})
	}
}

func TestSummaryBestsOfManualFinish(t *testing.T) {
	t.Run("earlier bests", func(t *testing.T) {
		history := []GameRecord{testRecord(epochHour(0), FinishTimeout, map[NodeID]int{5: 400}, nil)}
		h := topPercentHarness(t, Options{Records: history}, []NodeID{1}, nil)
		h.finishManually(map[NodeID]int{1: 900})
		wantBests(t, h, 1, summaryBests{Game: 900, GameNodes: []int{1}, Hour: 400, HourStart: 9, AllTime: 400})
	})
	t.Run("no counted game in the hour", func(t *testing.T) {
		history := []GameRecord{
			testRecord(jst(12, 0, 0, 0), FinishTimeout, map[NodeID]int{5: 400}, nil),
			testRecord(epochHour(0), FinishManual, map[NodeID]int{5: 1000}, nil),
		}
		h := topPercentHarness(t, Options{Records: history}, []NodeID{1}, nil)
		h.finishManually(map[NodeID]int{1: 900})
		wantBests(t, h, 1, summaryBests{Game: 900, GameNodes: []int{1}, Hour: -1, HourStart: -1, AllTime: 400})
	})
}

func TestSummaryBestsWithUnavailableRecords(t *testing.T) {
	options := Options{
		Records:            []GameRecord{testRecord(epochHour(0), FinishTimeout, map[NodeID]int{5: 400}, nil)},
		RecordsUnavailable: "file is not a database",
	}
	h := topPercentHarness(t, options, []NodeID{1}, nil)
	h.playToTimeout(map[NodeID]int{1: 900})
	wantBests(t, h, 1, summaryBests{Game: 900, GameNodes: []int{1}, Hour: -1, HourStart: -1, AllTime: -1})
	// The Snapshot still counts this process's game.
	if rs := h.state().Records; rs.AllTimeBest == nil || rs.AllTimeBest.Score != 900 {
		t.Fatalf("Snapshot all-time best %+v", rs.AllTimeBest)
	}
}

func TestSummaryBestsAreFixedUntilReset(t *testing.T) {
	h := topPercentHarness(t, Options{}, []NodeID{1, 2}, nil)
	h.playToTimeout(map[NodeID]int{1: 700, 2: 500})
	want := summaryBests{Game: 700, GameNodes: []int{1}, Hour: 700, HourStart: 9, HourMine: true, AllTime: 700, AllTimeMine: true}
	first := h.summary("P1")
	wantBests(t, h, 1, want)

	// Reconnection, resynchronization and a mode change leave them.
	h.must(h.a.Disconnected(h.ctx, "P1"))
	h.must(h.a.Connected(h.ctx, "P1b"))
	h.message("P1b", IncomingMessage{Type: "hello", UID: fmt.Sprintf("%012X", 1)})
	h.message("P1b", IncomingMessage{Type: "resync"})
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: false})
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
	again := h.summary("P1b")
	if again.Sync == first.Sync {
		t.Fatal("the summary after reconnecting has the old sync")
	}
	again.Sync = first.Sync
	if !reflect.DeepEqual(again, first) {
		t.Fatalf("summary after reconnecting\n got %+v\nwant %+v", again, first)
	}

	h.execute(Command{Operation: Reset})
	h.do(func() {
		if h.a.gameBest != nil || h.a.game.nodes[1].bests != (deviceBests{}) {
			t.Errorf("Reset left the bests %+v %+v", h.a.gameBest, h.a.game.nodes[1].bests)
		}
	})
	// The next game is settled anew, against the first one.
	h.playToTimeout(map[NodeID]int{1: 600, 2: 800})
	want = summaryBests{Game: 800, GameNodes: []int{2}, Hour: 800, HourStart: 9, AllTime: 800}
	if got := bestsOf(h.summary("P1b")); !reflect.DeepEqual(got, want) {
		t.Errorf("summary bests of the next game\n got %+v\nwant %+v", got, want)
	}
}

// jsonDepth is the deepest nesting of objects and arrays in data.
func jsonDepth(t *testing.T, data []byte) int {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(data))
	depth, deepest := 0, 0
	for {
		token, err := d.Token()
		if err != nil {
			break
		}
		switch token {
		case json.Delim('{'), json.Delim('['):
			depth++
			deepest = max(deepest, depth)
		case json.Delim('}'), json.Delim(']'):
			depth--
		}
	}
	return deepest
}

func TestSummaryJSONDepth(t *testing.T) {
	rank, percent, best, start := 1, 12, 900, 9
	data := SummaryData{
		Sync: "S", Score: 900, Rank: &rank, Eligible: true, TopPercent: &percent,
		GameBest: &best, GameBestNodes: []int{1, 2, 3, 4, 5, 6, 7, 8},
		HourBest: &best, HourBestStart: &start, HourBestMine: true, AllTimeBest: &best, AllTimeBestMine: true,
	}
	line, err := Message{Type: "summary", Data: data}.MarshalLine()
	if err != nil {
		t.Fatal(err)
	}
	// The player firmware rejects nesting deeper than 4 (player.md).
	if depth := jsonDepth(t, line); depth > 3 {
		t.Fatalf("summary %s nests %d deep, want at most 3", line, depth)
	}
	want := `{"type":"summary","data":{"sync":"S","score":900,"rank":1,"eligible":true,"topPercent":12,"bottomHalf":false,"gameBest":900,"gameBestNodes":[1,2,3,4,5,6,7,8],"hourBest":900,"hourBestStart":9,"hourBestMine":true,"allTimeBest":900,"allTimeBestMine":true}}`
	if string(line) != want {
		t.Fatalf("summary\n got %s\nwant %s", line, want)
	}
}

// A swap during Finished sends each board the *Mine of its new node.
func TestSummaryBestsFollowSwappedBoards(t *testing.T) {
	h := topPercentHarness(t, Options{}, []NodeID{1, 2}, nil)
	h.playToTimeout(map[NodeID]int{1: 700, 2: 500})
	h.execute(Command{Operation: SwapDeviceAssignments, DeviceID: fmt.Sprintf("%012X", 1), OtherDeviceID: fmt.Sprintf("%012X", 2)})
	// The P1 session now shows P2 and the other way round.
	mine := summaryBests{Game: 700, GameNodes: []int{1}, Hour: 700, HourStart: 9, HourMine: true, AllTime: 700, AllTimeMine: true}
	other := summaryBests{Game: 700, GameNodes: []int{1}, Hour: 700, HourStart: 9, AllTime: 700}
	for _, tc := range []struct {
		session SessionID
		want    summaryBests
	}{{"P1", other}, {"P2", mine}} {
		if got := bestsOf(h.summary(tc.session)); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("session %s summary bests after the swap\n got %+v\nwant %+v", tc.session, got, tc.want)
		}
	}
}
