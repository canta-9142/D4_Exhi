package app

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"
)

// gameSaver is a SaveGame callback that keeps every call.
type gameSaver struct {
	mu      sync.Mutex
	records []GameRecord
	err     error
}

func (s *gameSaver) save(r GameRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, r)
	return s.err
}

func (s *gameSaver) saved() []GameRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.records)
}

func (s *gameSaver) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

// do runs f in Run's goroutine without advancing the game.
func (h *harness) do(f func()) {
	h.t.Helper()
	h.must(h.a.submit(h.ctx, false, func(time.Time) error { f(); return nil }))
}

// manualPlayer connects P1 and switches it to manual, so that it is eligible
// in the next game.
func (h *harness) manualPlayer() {
	h.t.Helper()
	h.connect("p1", testUID, 1)
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
}

// testRecord is a game of the current duration. eligible players are ranked
// by score; ineligible players have no rank.
func testRecord(at time.Time, reason FinishReason, eligible, ineligible map[NodeID]int) GameRecord {
	r := GameRecord{FinishedAt: at, Duration: GameDuration, Elapsed: GameDuration, Reason: reason}
	if reason == FinishManual {
		r.Elapsed = GameDuration / 2
	}
	for id := NodeID(1); id <= PlayerCount; id++ {
		p := PlayerResult{Node: id, Score: ineligible[id]}
		if score, ok := eligible[id]; ok {
			p.Score, p.Eligible, p.Rank = score, true, 1
			for _, other := range eligible {
				if other > score {
					p.Rank++
				}
			}
		}
		r.Results = append(r.Results, p)
	}
	return r
}

func jst(hour, minute, second, nanosecond int) time.Time {
	return time.Date(2026, 10, 28, hour, minute, second, nanosecond, JST)
}

// equalBest compares the instants of the times, whatever their locations.
func equalBest(got *BestScore, want BestScore) bool {
	return got != nil && got.Score == want.Score && slices.Equal(got.Nodes, want.Nodes) && got.FinishedAt.Equal(want.FinishedAt)
}

func TestGameRecordIsSavedOnceAtFinished(t *testing.T) {
	for _, tc := range []struct {
		name    string
		finish  func(h *harness)
		reason  FinishReason
		elapsed time.Duration
	}{
		// A late Tick still finishes the record at the end of the game time.
		{"timeout", func(h *harness) { h.tick(GameDuration + 5*time.Second) }, FinishTimeout, GameDuration},
		{"manual", func(h *harness) { h.tick(30 * time.Second); h.execute(Command{Operation: Finish}) }, FinishManual, 30 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			saver := &gameSaver{}
			h := newHarnessWithOptions(t, Options{Mode: Simultaneous, SaveGame: saver.save})
			h.manualPlayer()
			h.start() // The game clock starts at epoch.
			if len(saver.saved()) != 0 {
				t.Fatal("saved before Finished")
			}
			tc.finish(h)
			s := h.state()
			saved := saver.saved()
			if s.Phase != Finished || len(saved) != 1 {
				t.Fatalf("phase=%s saves=%d", s.Phase, len(saved))
			}
			r := saved[0]
			if r.Reason != tc.reason || r.Elapsed != tc.elapsed || r.Duration != GameDuration || !r.FinishedAt.Equal(epoch.Add(tc.elapsed)) || len(r.Results) != PlayerCount {
				t.Fatalf("record %+v", r)
			}
			for i, p := range r.Results {
				n := s.Nodes[i]
				rank := 0
				if n.Rank != nil {
					rank = *n.Rank
				}
				if p.Node != n.ID || p.Score != n.Score || p.Eligible != n.RankingEligible || p.Rank != rank {
					t.Fatalf("result %+v does not match the settled node %+v", p, n)
				}
			}
			if !r.Results[0].Eligible || r.Results[0].Rank != 1 || r.Results[1].Eligible || r.Results[1].Rank != 0 {
				t.Fatalf("eligibility and ranks: %+v", r.Results[:2])
			}
			h.tick(GameDuration + 10*time.Second)
			if err := h.a.Execute(h.ctx, Command{Operation: Finish}); err == nil {
				t.Fatal("Finish accepted while Finished")
			}
			if got := len(saver.saved()); got != 1 {
				t.Fatalf("saves=%d after Finished", got)
			}
		})
	}
}

// A Finish processed after the game time first catches up, which finishes the
// game by timeout; the Finish itself is then rejected and saves nothing.
func TestLateFinishSavesTheTimeoutOnce(t *testing.T) {
	saver := &gameSaver{}
	h := newHarnessWithOptions(t, Options{Mode: Simultaneous, SaveGame: saver.save})
	h.manualPlayer()
	h.start()
	h.clock.Store(int64(GameDuration + 50*time.Millisecond))
	expectCode(t, h.a.Execute(h.ctx, Command{Operation: Finish}), CodePhaseConflict)
	saved := saver.saved()
	if len(saved) != 1 || saved[0].Reason != FinishTimeout || saved[0].Elapsed != GameDuration || !saved[0].FinishedAt.Equal(epoch.Add(GameDuration)) {
		t.Fatalf("saved %+v", saved)
	}
}

func TestGameRecordIsNotSavedOnResetReconnectOrState(t *testing.T) {
	saver := &gameSaver{}
	h := newHarnessWithOptions(t, Options{Mode: Simultaneous, SaveGame: saver.save})
	h.manualPlayer()
	h.start()
	h.tick(GameDuration)
	h.state()
	h.must(h.a.Disconnected(h.ctx, "p1"))
	h.must(h.a.Connected(h.ctx, "p1b"))
	h.message("p1b", IncomingMessage{Type: "hello", UID: testUID})
	h.message("p1b", IncomingMessage{Type: "resync"})
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
	h.state()
	if got := len(saver.saved()); got != 1 {
		t.Fatalf("saves=%d after reconnect, resync, mode change and State", got)
	}
	h.execute(Command{Operation: Reset})
	h.tick(GameDuration + time.Minute)
	if got := len(saver.saved()); got != 1 {
		t.Fatalf("saves=%d after Reset", got)
	}
}

func TestSaveFailureKeepsGameGoingAndIsReported(t *testing.T) {
	saver := &gameSaver{err: errors.New("disk full")}
	h := newHarnessWithOptions(t, Options{Mode: Simultaneous, SaveGame: saver.save})
	h.manualPlayer()
	h.start()
	h.tick(GameDuration)
	s := h.state()
	if s.Phase != Finished || s.Nodes[0].Rank == nil || *s.Nodes[0].Rank != 1 {
		t.Fatalf("the failed save changed the result: %s %+v", s.Phase, s.Nodes[0])
	}
	if got := h.summary("p1"); !got.Eligible || got.Rank == nil || *got.Rank != 1 {
		t.Fatalf("summary after the failed save: %+v", got)
	}
	rs := s.Records
	if !rs.SaveFailed || !rs.Available || rs.GameBest == nil || rs.AllTimeBest == nil || len(rs.HourlyBests) != 1 {
		t.Fatalf("the failed game is not reported or not counted: %+v", rs)
	}
	h.execute(Command{Operation: Reset})
	if !h.state().Records.SaveFailed {
		t.Fatal("Reset cleared the failure of the latest save")
	}
	saver.fail(nil)
	h.start()
	h.tick(2 * GameDuration)
	if rs := h.state().Records; rs.SaveFailed || len(saver.saved()) != 2 {
		t.Fatalf("the next successful save: failed=%t saves=%d", rs.SaveFailed, len(saver.saved()))
	}
}

func TestBestScoresCountOnlyEligibleFullGamesOfCurrentDuration(t *testing.T) {
	counted := testRecord(jst(13, 10, 0, 0), FinishTimeout, map[NodeID]int{1: 100}, map[NodeID]int{2: 900})
	manual := testRecord(jst(13, 20, 0, 0), FinishManual, map[NodeID]int{1: 500}, nil)
	longer := testRecord(jst(13, 30, 0, 0), FinishTimeout, map[NodeID]int{1: 700}, nil)
	longer.Duration, longer.Elapsed = GameDuration+30*time.Second, GameDuration+30*time.Second
	automatic := testRecord(jst(14, 10, 0, 0), FinishTimeout, nil, map[NodeID]int{3: 800})
	h := newHarnessWithOptions(t, Options{Mode: Simultaneous, Records: []GameRecord{counted, manual, longer, automatic}})
	rs := h.state().Records
	want := &BestScore{Score: 100, Nodes: []NodeID{1}, FinishedAt: counted.FinishedAt}
	if !reflect.DeepEqual(rs.AllTimeBest, want) || !reflect.DeepEqual(rs.HourlyBests, []HourlyBest{{HourStart: jst(13, 0, 0, 0), BestScore: *want}}) {
		t.Fatalf("bests %+v %+v", rs.AllTimeBest, rs.HourlyBests)
	}
	if rs.GameBest != nil || !rs.Available || rs.UnavailableReason != "" || rs.SaveFailed {
		t.Fatalf("records state %+v", rs)
	}
}

func TestHourlyBestsSplitAtJSTHours(t *testing.T) {
	var b bests
	b.add(testRecord(jst(13, 59, 59, 999_000_000), FinishTimeout, map[NodeID]int{1: 300}, nil))
	b.add(testRecord(jst(14, 0, 0, 0), FinishTimeout, map[NodeID]int{2: 200}, nil))
	// 05:30 UTC is 14:30 JST. The 12:30 game is added after a later hour, as
	// after a clock change.
	b.add(testRecord(time.Date(2026, 10, 28, 5, 30, 0, 0, time.UTC), FinishTimeout, map[NodeID]int{3: 250}, nil))
	b.add(testRecord(jst(12, 30, 0, 0), FinishTimeout, map[NodeID]int{4: 50}, nil))
	var got []string
	for _, h := range b.hourly {
		got = append(got, fmt.Sprintf("%s %d %v", h.HourStart.Format(time.RFC3339), h.Score, h.Nodes))
	}
	want := []string{"2026-10-28T14:00:00+09:00 250 [3]", "2026-10-28T13:00:00+09:00 300 [1]", "2026-10-28T12:00:00+09:00 50 [4]"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hourly bests %v, want %v", got, want)
	}
}

func TestBestScoreTiesKeepEarlierGameWithAllTiedNodes(t *testing.T) {
	first := testRecord(jst(13, 10, 0, 0), FinishTimeout, map[NodeID]int{1: 300, 3: 300, 5: 100}, nil)
	tie := testRecord(jst(13, 20, 0, 0), FinishTimeout, map[NodeID]int{2: 300}, nil)
	var b bests
	b.add(first)
	b.add(tie)
	want := BestScore{Score: 300, Nodes: []NodeID{1, 3}, FinishedAt: first.FinishedAt}
	if !reflect.DeepEqual(*b.allTime, want) || !reflect.DeepEqual(b.hourly[0].BestScore, want) {
		t.Fatalf("a tie replaced the earlier game: %+v %+v", b.allTime, b.hourly)
	}
	// Save order decides, even if the later game's clock reads earlier.
	earlierClock := testRecord(jst(13, 5, 0, 0), FinishTimeout, map[NodeID]int{4: 300}, nil)
	b.add(earlierClock)
	if !reflect.DeepEqual(*b.allTime, want) {
		t.Fatalf("finished_at decided a tie: %+v", b.allTime)
	}
	higher := testRecord(jst(13, 30, 0, 0), FinishTimeout, map[NodeID]int{4: 301}, nil)
	b.add(higher)
	want = BestScore{Score: 301, Nodes: []NodeID{4}, FinishedAt: higher.FinishedAt}
	if !reflect.DeepEqual(*b.allTime, want) || !reflect.DeepEqual(b.hourly[0].BestScore, want) || len(b.hourly) != 1 {
		t.Fatalf("a higher score did not replace the best: %+v %+v", b.allTime, b.hourly)
	}
}

func TestPastRecordsAndCurrentGameAreCounted(t *testing.T) {
	past := testRecord(jst(13, 10, 0, 0), FinishTimeout, map[NodeID]int{2: 500}, nil)
	h := newHarnessWithOptions(t, Options{Mode: Simultaneous, Records: []GameRecord{past}})
	if rs := h.state().Records; rs.AllTimeBest == nil || rs.AllTimeBest.Score != 500 || len(rs.HourlyBests) != 1 {
		t.Fatalf("past records at startup: %+v", rs)
	}
	h.manualPlayer()
	h.start()
	h.tick(GameDuration - time.Second)
	h.do(func() { h.a.game.nodes[1].score = 10000 })
	h.tick(GameDuration)
	s := h.state()
	rs := s.Records
	want := BestScore{Score: s.Nodes[0].Score, Nodes: []NodeID{1}, FinishedAt: epoch.Add(GameDuration)}
	if !equalBest(rs.GameBest, want) || !equalBest(rs.AllTimeBest, want) {
		t.Fatalf("current game: %+v %+v", rs.GameBest, rs.AllTimeBest)
	}
	// The 2026 hour is newer than this game's hour at epoch.
	if len(rs.HourlyBests) != 2 || rs.HourlyBests[0].Score != 500 || !equalBest(&rs.HourlyBests[1].BestScore, want) {
		t.Fatalf("hourly bests %+v", rs.HourlyBests)
	}
	rs.HourlyBests[1].Nodes[0] = 8
	rs.GameBest.Nodes[0] = 8
	if again := h.state().Records; again.HourlyBests[1].Nodes[0] != 1 || again.GameBest.Nodes[0] != 1 {
		t.Fatal("the Snapshot shares its node slices with app")
	}
}

func TestUnavailableRecordsCountOnlyThisProcess(t *testing.T) {
	saver := &gameSaver{}
	past := testRecord(jst(13, 10, 0, 0), FinishTimeout, map[NodeID]int{2: 100000}, nil)
	h := newHarnessWithOptions(t, Options{Mode: Simultaneous, Records: []GameRecord{past}, SaveGame: saver.save, RecordsUnavailable: "file is not a database"})
	rs := h.state().Records
	if rs.Available || rs.UnavailableReason != "file is not a database" || rs.AllTimeBest != nil || len(rs.HourlyBests) != 0 || rs.SaveFailed {
		t.Fatalf("unavailable records at startup: %+v", rs)
	}
	h.manualPlayer()
	h.start()
	h.tick(GameDuration)
	s := h.state()
	rs = s.Records
	if len(saver.saved()) != 0 {
		t.Fatal("SaveGame called although the records file is unavailable")
	}
	want := BestScore{Score: s.Nodes[0].Score, Nodes: []NodeID{1}, FinishedAt: epoch.Add(GameDuration)}
	if rs.Available || rs.SaveFailed || !equalBest(rs.AllTimeBest, want) || len(rs.HourlyBests) != 1 || !equalBest(&rs.HourlyBests[0].BestScore, want) {
		t.Fatalf("this process's game: %+v", rs)
	}
}

func TestGameBestLastsWhileFinished(t *testing.T) {
	saver := &gameSaver{}
	h := newHarnessWithOptions(t, Options{Mode: Simultaneous, SaveGame: saver.save})
	h.manualPlayer()
	h.ready("p1")
	h.execute(Command{Operation: Start}) // The game clock starts at epoch+3s.
	h.tick(time.Second)
	if h.state().Records.GameBest != nil {
		t.Fatal("game best while Playing")
	}
	h.execute(Command{Operation: Finish})
	rs := h.state().Records
	// A Finish during the countdown records the game clock origin.
	want := BestScore{Score: 0, Nodes: []NodeID{1}, FinishedAt: epoch.Add(CountdownDuration)}
	if !equalBest(rs.GameBest, want) || rs.AllTimeBest != nil || len(rs.HourlyBests) != 0 {
		t.Fatalf("manual Finish: %+v", rs)
	}
	if saved := saver.saved(); len(saved) != 1 || saved[0].Reason != FinishManual || saved[0].Elapsed != 0 || !saved[0].FinishedAt.Equal(want.FinishedAt) {
		t.Fatalf("saved %+v", saved)
	}
	h.execute(Command{Operation: Reset})
	if h.state().Records.GameBest != nil {
		t.Fatal("game best after Reset")
	}
	// A game without eligible players is saved but has no best.
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: false})
	h.start()
	h.tick(GameDuration + time.Minute)
	if rs := h.state().Records; rs.GameBest != nil || rs.AllTimeBest != nil || len(saver.saved()) != 2 {
		t.Fatalf("all-automatic game: %+v saves=%d", rs, len(saver.saved()))
	}
}
