package app

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// scoresAround is n scores for TopPercent, with the score 500 among them: higher
// of the others are above it, and the rest tie with it or are below it.
func scoresAround(n, higher int) []int {
	scores := []int{500}
	for i := 0; i < higher; i++ {
		scores = append(scores, 600)
	}
	for i := 0; len(scores) < n; i++ {
		scores = append(scores, []int{500, 400}[i%2])
	}
	return scores
}

func TestTopPercent(t *testing.T) {
	for _, tc := range []struct {
		name         string
		n, higher    int
		want         int
		wantComputed bool
	}{
		{"29 scores are too few", 29, 0, 0, false},
		{"30 scores are enough", 30, 29, 100, true},
		{"the best of 30", 30, 0, 4, true},
		{"the best of 100", 100, 0, 1, true},
		{"the best of 99 is not yet 1", 99, 0, 2, true},
		{"exactly 50", 100, 49, 50, true},
		{"just over 50", 100, 50, 51, true},
		{"the worst of 100", 100, 99, 100, true},
		{"rounds up from 50", 30, 14, 50, true},
		{"rounds up past 50", 30, 15, 54, true},
		{"rounds up", 200, 19, 10, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := topPercent(scoresAround(tc.n, tc.higher), 500)
			if got != tc.want || ok != tc.wantComputed {
				t.Fatalf("topPercent = %d, %t; want %d, %t", got, ok, tc.want, tc.wantComputed)
			}
		})
	}

	t.Run("a tie does not push the others down", func(t *testing.T) {
		same := slices.Repeat([]int{500}, 40)
		if got, ok := topPercent(same, 500); got != 3 || !ok { // ceil(100 / 40)
			t.Fatalf("topPercent = %d, %t; want 3, true", got, ok)
		}
	})
	t.Run("a score outside the population stays within 100", func(t *testing.T) {
		if got, ok := topPercent(slices.Repeat([]int{100}, 30), 50); got != 100 || !ok {
			t.Fatalf("topPercent = %d, %t; want 100, true", got, ok)
		}
	})
	t.Run("no population", func(t *testing.T) {
		if got, ok := topPercent(nil, 0); got != 0 || ok {
			t.Fatalf("topPercent = %d, %t; want 0, false", got, ok)
		}
	})
}

func TestDeviceTopPercent(t *testing.T) {
	for _, tc := range []struct {
		name        string
		percent     int
		unavailable string
		want        int // 0 for nil.
	}{
		{"none", 0, "", 0},
		{"the best", 1, "", 1},
		{"exactly 50", 50, "", 50},
		{"just over 50", 51, "", 0},
		{"the worst", 100, "", 0},
		{"records unavailable", 10, "file is not a database", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &App{options: Options{RecordsUnavailable: tc.unavailable}}
			got := a.deviceTopPercent(&node{topPercent: tc.percent})
			switch {
			case tc.want == 0 && got != nil:
				t.Fatalf("deviceTopPercent = %d, want nil", *got)
			case tc.want != 0 && (got == nil || *got != tc.want):
				t.Fatalf("deviceTopPercent = %v, want %d", got, tc.want)
			}
		})
	}
}

func TestSummaryTopPercentJSON(t *testing.T) {
	rank, percent := 2, 12
	for _, tc := range []struct {
		name string
		data SummaryData
		want string
	}{
		{"within the top half", SummaryData{Sync: "S", Score: 345, Rank: &rank, Eligible: true, TopPercent: &percent},
			`{"type":"summary","data":{"sync":"S","score":345,"rank":2,"eligible":true,"topPercent":12}}`},
		{"outside the top half", SummaryData{Sync: "S", Score: 345, Rank: &rank, Eligible: true},
			`{"type":"summary","data":{"sync":"S","score":345,"rank":2,"eligible":true,"topPercent":null}}`},
		{"not eligible", SummaryData{Sync: "S", Score: 400},
			`{"type":"summary","data":{"sync":"S","score":400,"rank":null,"eligible":false,"topPercent":null}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			line, err := Message{Type: "summary", Data: tc.data}.MarshalLine()
			if err != nil {
				t.Fatal(err)
			}
			if string(line) != tc.want {
				t.Fatalf("summary\n got %s\nwant %s", line, tc.want)
			}
		})
	}
}

// topPercentHistory is saved games that hold scores as their eligible results,
// eight to a game.
func topPercentHistory(scores []int) []GameRecord {
	var records []GameRecord
	for i := 0; len(scores) > 0; i++ {
		n := min(len(scores), PlayerCount)
		eligible := make(map[NodeID]int)
		for j, score := range scores[:n] {
			eligible[NodeID(j+1)] = score
		}
		records = append(records, testRecord(jst(12, i%60, 0, 0), FinishTimeout, eligible, nil))
		scores = scores[n:]
	}
	return records
}

// pastScores is the scores of earlier games: above of them above the score 5000
// that the test players get, and below of them not above it (half tie with it).
func pastScores(above, below int) []int {
	scores := slices.Repeat([]int{6000}, above)
	for i := 0; i < below; i++ {
		scores = append(scores, []int{5000, 100}[i%2])
	}
	return scores
}

// topPercentHarness has the given players connected, with manual ones eligible
// in the coming game.
func topPercentHarness(t *testing.T, options Options, manual, automatic []NodeID) *harness {
	t.Helper()
	h := newHarnessWithOptions(t, options)
	for _, id := range slices.Concat(manual, automatic) {
		h.connect(SessionID(id.wireName()), fmt.Sprintf("%012X", id), id)
	}
	for _, id := range manual {
		h.execute(Command{Operation: SetNodeEnabled, Node: id, Enabled: true})
	}
	return h
}

// playToTimeout starts a game and lets the game time run out with scores, which
// it sets just before the end so that no later transfer changes them.
func (h *harness) playToTimeout(scores map[NodeID]int) {
	h.t.Helper()
	h.start()
	base := time.Duration(h.clock.Load())
	h.tick(base + GameDuration - time.Millisecond)
	h.do(func() {
		for id, score := range scores {
			h.a.game.nodes[id].score = score
		}
	})
	h.tick(base + GameDuration)
}

func topPercentOf(s Snapshot, id NodeID) *int { return s.Nodes[id-1].TopPercent }

func wantPercent(t *testing.T, what string, got *int, want int) {
	t.Helper()
	switch {
	case want == 0 && got != nil:
		t.Errorf("%s = %d, want null", what, *got)
	case want != 0 && got == nil:
		t.Errorf("%s = null, want %d", what, want)
	case want != 0 && *got != want:
		t.Errorf("%s = %d, want %d", what, *got, want)
	}
}

func TestTopPercentAtTimeout(t *testing.T) {
	for _, tc := range []struct {
		name         string
		above, below int // Scores of earlier games.
		manual       []NodeID
		automatic    []NodeID
		snapshot     int // Of each manual player; 0 for null.
		summary      int
	}{
		{"29 scores", 0, 28, []NodeID{1}, nil, 0, 0},
		{"30 scores", 0, 29, []NodeID{1}, nil, 4, 4},
		{"the best of 100", 0, 99, []NodeID{1}, nil, 1, 1},
		{"exactly 50", 49, 50, []NodeID{1}, nil, 50, 50},
		{"just over 50", 50, 49, []NodeID{1}, nil, 51, 0},
		{"the worst of 100", 99, 0, []NodeID{1}, nil, 100, 0},
		{"tied players share a value", 49, 49, []NodeID{1, 2}, nil, 50, 50},
		// P3 is not in the population: counting it would make 101 scores with
		// 50 above and give 51.
		{"an ineligible player is not counted", 49, 50, []NodeID{1}, []NodeID{3}, 50, 50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			history := topPercentHistory(pastScores(tc.above, tc.below))
			h := topPercentHarness(t, Options{Records: history}, tc.manual, tc.automatic)
			scores := map[NodeID]int{}
			for _, id := range tc.manual {
				scores[id] = 5000
			}
			for _, id := range tc.automatic {
				scores[id] = 9999
			}
			h.playToTimeout(scores)

			s := h.state()
			if s.Phase != Finished {
				t.Fatalf("phase = %s", s.Phase)
			}
			for _, id := range tc.manual {
				name := id.wireName()
				wantPercent(t, name+" Snapshot topPercent", topPercentOf(s, id), tc.snapshot)
				got := h.summary(SessionID(name))
				wantPercent(t, name+" summary topPercent", got.TopPercent, tc.summary)
				encoded, err := json.Marshal(got)
				if err != nil {
					t.Fatal(err)
				}
				want := `"topPercent":null`
				if tc.summary != 0 {
					want = fmt.Sprintf(`"topPercent":%d`, tc.summary)
				}
				if !strings.Contains(string(encoded), want) {
					t.Errorf("%s summary %s lacks %s", name, encoded, want)
				}
			}
			for _, id := range tc.automatic {
				name := id.wireName()
				wantPercent(t, name+" Snapshot topPercent", topPercentOf(s, id), 0)
				got := h.summary(SessionID(name))
				if got.Eligible || got.TopPercent != nil || got.Rank != nil {
					t.Errorf("%s summary of an ineligible player: %+v", name, got)
				}
				if encoded, _ := json.Marshal(got); !strings.Contains(string(encoded), `"topPercent":null`) {
					t.Errorf("%s summary %s lacks a null topPercent", name, encoded)
				}
			}
		})
	}
}

func TestManualFinishGivesNoTopPercent(t *testing.T) {
	// 99 earlier scores and the game's own would make 100, but a manual Finish
	// gives nobody a value.
	h := topPercentHarness(t, Options{Records: topPercentHistory(pastScores(0, 99))}, []NodeID{1}, nil)
	h.start()
	h.tick(30 * time.Second)
	h.do(func() { h.a.game.nodes[1].score = 5000 })
	h.execute(Command{Operation: Finish})
	s := h.state()
	if s.Phase != Finished {
		t.Fatalf("phase = %s", s.Phase)
	}
	wantPercent(t, "Snapshot topPercent", topPercentOf(s, 1), 0)
	if got := h.summary("P1"); !got.Eligible || got.Rank == nil || got.TopPercent != nil {
		t.Fatalf("summary after a manual Finish: %+v", got)
	}
}

func TestManualFinishJoinsNoPopulation(t *testing.T) {
	h := topPercentHarness(t, Options{Records: topPercentHistory(pastScores(0, 28))}, []NodeID{1}, nil)
	h.start()
	h.tick(30 * time.Second)
	h.execute(Command{Operation: Finish})

	// 28 earlier scores and this game's make 29. With the manual game's score
	// counted, they would make 30.
	h.execute(Command{Operation: Reset})
	h.playToTimeout(map[NodeID]int{1: 7000})
	wantPercent(t, "Snapshot topPercent of the second game", topPercentOf(h.state(), 1), 0)
	h.execute(Command{Operation: Reset})
	h.playToTimeout(map[NodeID]int{1: 7000})
	wantPercent(t, "Snapshot topPercent of the third game", topPercentOf(h.state(), 1), 4)
}

func TestTopPercentWithUnavailableRecords(t *testing.T) {
	// The past scores are ignored. Only this process's games count, so the
	// 30th game is the first with enough scores.
	options := Options{
		Records:            topPercentHistory(slices.Repeat([]int{1_000_000}, 100)),
		RecordsUnavailable: "file is not a database",
	}
	h := topPercentHarness(t, options, []NodeID{1}, nil)
	for i := 1; i <= 30; i++ {
		if i > 1 {
			h.execute(Command{Operation: Reset})
		}
		h.playToTimeout(map[NodeID]int{1: 100 * i})
		switch i {
		case 29:
			wantPercent(t, "Snapshot topPercent of game 29", topPercentOf(h.state(), 1), 0)
		case 30:
			s := h.state()
			wantPercent(t, "Snapshot topPercent of game 30", topPercentOf(s, 1), 4)
			if s.Records.Available {
				t.Fatal("records available")
			}
			if got := h.summary("P1"); !got.Eligible || got.TopPercent != nil {
				t.Fatalf("summary with unavailable records: %+v", got)
			}
		}
	}
}

func TestTopPercentIsFixedUntilReset(t *testing.T) {
	h := topPercentHarness(t, Options{Records: topPercentHistory(pastScores(49, 50))}, []NodeID{1}, nil)
	s := h.state()
	wantPercent(t, "Snapshot topPercent in Preparing", topPercentOf(s, 1), 0)
	h.start()
	h.tick(time.Second)
	wantPercent(t, "Snapshot topPercent in Playing", topPercentOf(h.state(), 1), 0)
	h.tick(GameDuration - time.Millisecond)
	h.do(func() { h.a.game.nodes[1].score = 5000 })
	h.tick(GameDuration)

	// Snapshot and summary come from the same settled value.
	snapshot := topPercentOf(h.state(), 1)
	summary := h.summary("P1")
	wantPercent(t, "Snapshot topPercent", snapshot, 50)
	wantPercent(t, "summary topPercent", summary.TopPercent, 50)
	*snapshot = 99
	wantPercent(t, "Snapshot topPercent after changing a copy", topPercentOf(h.state(), 1), 50)

	// Reconnection, resynchronization and a mode change leave it.
	h.must(h.a.Disconnected(h.ctx, "P1"))
	wantPercent(t, "Snapshot topPercent after a disconnect", topPercentOf(h.state(), 1), 50)
	h.must(h.a.Connected(h.ctx, "P1b"))
	h.message("P1b", IncomingMessage{Type: "hello", UID: fmt.Sprintf("%012X", 1)})
	h.message("P1b", IncomingMessage{Type: "resync"})
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: false})
	wantPercent(t, "summary topPercent after reconnecting", h.summary("P1b").TopPercent, 50)
	wantPercent(t, "Snapshot topPercent after the reconnection", topPercentOf(h.state(), 1), 50)

	h.execute(Command{Operation: Reset})
	if got := topPercentOf(h.state(), 1); got != nil {
		t.Fatalf("Reset left topPercent = %d", *got)
	}
}
