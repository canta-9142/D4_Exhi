package app

import (
	"fmt"
	"math/bits"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func playerSession(id NodeID) SessionID { return SessionID(id.wireName()) }
func playerUID(id NodeID) string        { return fmt.Sprintf("%012X", id) }

// endlessHarness runs endless operation, stopped, with the given players
// connected and manual.
func endlessHarness(t *testing.T, options Options, manual ...NodeID) *harness {
	t.Helper()
	h := newHarnessWithOptions(t, options)
	for _, id := range manual {
		h.connect(playerSession(id), playerUID(id), id)
		h.execute(Command{Operation: SetNodeEnabled, Node: id, Enabled: true})
	}
	return h
}

// quiet stops the players' generation, so that only the packets a test sets
// move and its messages are only those it causes.
func (h *harness) quiet() {
	h.t.Helper()
	h.do(func() {
		for id := NodeID(1); id <= PlayerCount; id++ {
			h.a.game.nodes[id].nextGeneration = 0
		}
	})
}

// play is a long press followed by the selection of round.
func (h *harness) play(id SessionID, round RoundKind) {
	h.t.Helper()
	h.press(id)
	h.choose(id, round)
}

func (h *harness) node(id NodeID) NodeSnapshot {
	h.t.Helper()
	return h.state().Nodes[id-1]
}

// setFIFO replaces the real FIFO of id, without telling its MCU.
func (h *harness) setFIFO(id NodeID, packets ...*packet) {
	h.t.Helper()
	h.do(func() {
		h.a.game.nodes[id].fifo = packets
		h.a.game.schedule(id)
	})
}

// send sends a SEND of the session's current generation and returns its
// result.
func (h *harness) send(id SessionID, pid uint16, to NodeID) ResultData {
	h.t.Helper()
	h.nextRID++
	before := len(h.messages())
	h.message(id, IncomingMessage{Type: "send", Sync: h.sync(id), RID: h.nextRID, PID: pid, To: to})
	for _, m := range h.messages()[before:] {
		if m.session == id && m.message.Type == "result" {
			return m.message.Data.(ResultData)
		}
	}
	h.t.Fatal("no result")
	return ResultData{}
}

func (h *harness) phasesFor(id SessionID) []Phase {
	var phases []Phase
	for _, m := range h.messagesFor(id) {
		if m.Type == "phase" {
			phases = append(phases, m.Data.(PhaseData).Phase)
		}
	}
	return phases
}

func TestEndlessStartAndStopResetPlayersAndLEDs(t *testing.T) {
	h := endlessHarness(t, Options{}, 1)
	h.connect("P2", playerUID(2), 2)
	h.connectLED(ledSession(9), ledUID(9), 9)
	h.connectLED(ledSession(1), ledUID(1), 1)
	if s := h.state(); s.Mode != Endless || s.Operation != OperationStopped || !s.CanStart || len(s.StartBlockers) != 0 || s.Phase != "" {
		t.Fatalf("initial state: %+v", s)
	}
	if n := h.node(1); n.Station != StationStopped || n.Round != "" {
		t.Fatalf("initial station: %+v", n)
	}
	h.tick(5 * time.Second)
	if queuedPackets(h.state()) != 0 {
		t.Fatal("stopped operation generated packets")
	}
	for _, op := range []Operation{Stop, Finish, Reset, ReturnToBot} {
		expectCode(t, h.a.Execute(h.ctx, Command{Operation: op, Node: 1}), CodePhaseConflict)
	}

	h.clear()
	h.execute(Command{Operation: Start})
	for _, id := range []SessionID{"P1", "P2"} {
		if got := h.typesFor(id); !reflect.DeepEqual(got, []string{"phase", "reset", "status"}) || !reflect.DeepEqual(h.phasesFor(id), []Phase{Idle}) {
			t.Fatalf("%s at Start: %v %v", id, got, h.phasesFor(id))
		}
		if len(h.reset(id).Packets) != 0 {
			t.Fatalf("%s reset is not empty", id)
		}
	}
	if got := lastStatus(t, h, "P1"); !reflect.DeepEqual(got, StatusData{Gains: []int{}}) {
		t.Fatalf("status at Start: %+v", got)
	}
	if got := h.lines(ledSession(9)); !reflect.DeepEqual(got, []string{"CLEAR", "BUFFER 0"}) {
		t.Fatalf("bot LED at Start: %v", got)
	}
	if got := h.lines(ledSession(1)); !reflect.DeepEqual(got, []string{"CLEAR"}) {
		t.Fatalf("player LED at Start: %v", got)
	}
	s := h.state()
	if s.Operation != OperationRunning || s.CanStart || !reflect.DeepEqual(s.StartBlockers, []StartBlocker{{Code: CodeNotStopped}}) || s.Nodes[0].Station != StationIdle {
		t.Fatalf("running state: %+v", s)
	}
	expectCode(t, h.a.Execute(h.ctx, Command{Operation: Start}), CodePhaseConflict)

	// The network runs, and the clock is that of the operation.
	h.tick(6 * time.Second)
	if s := h.state(); queuedPackets(s) == 0 || s.Elapsed != time.Second {
		t.Fatalf("running operation: packets=%d elapsed=%s", queuedPackets(s), s.Elapsed)
	}

	h.clear()
	h.execute(Command{Operation: Stop})
	if got := h.typesFor("P1"); !reflect.DeepEqual(got, []string{"phase", "reset", "status"}) || !reflect.DeepEqual(h.phasesFor("P1"), []Phase{Preparing}) {
		t.Fatalf("P1 at Stop: %v %v", got, h.phasesFor("P1"))
	}
	if got := h.lines(ledSession(9)); !reflect.DeepEqual(got, []string{"CLEAR", "BUFFER 0"}) {
		t.Fatalf("bot LED at Stop: %v", got)
	}
	if s := h.state(); s.Operation != OperationStopped || queuedPackets(s) != 0 || s.Elapsed != 0 || s.Nodes[0].Station != StationStopped || !s.Nodes[0].Enabled {
		t.Fatalf("stopped state: %+v", s)
	}
	expectCode(t, h.a.Execute(h.ctx, Command{Operation: Stop}), CodePhaseConflict)
}

func TestEndlessReadyAndChooseFollowTheStation(t *testing.T) {
	h := endlessHarness(t, Options{}, 1)
	h.connect("P2", playerUID(2), 2)
	h.clear()
	h.press("P1")
	if got := h.typesFor("P1"); !reflect.DeepEqual(got, []string{"status"}) || h.node(1).Station != StationStopped {
		t.Fatalf("READY while stopped: %v", got)
	}
	h.execute(Command{Operation: Start})
	h.quiet()

	// A stale generation, an automatic player and a CHOOSE before the
	// selection get only a status.
	h.clear()
	h.message("P1", IncomingMessage{Type: "ready", Sync: strings.Repeat("F", 32)})
	h.choose("P1", RoundNormal)
	h.press("P2")
	if got, other := h.typesFor("P1"), h.typesFor("P2"); !reflect.DeepEqual(got, []string{"status", "status"}) || !reflect.DeepEqual(other, []string{"status"}) {
		t.Fatalf("refused inputs: %v %v", got, other)
	}
	if h.node(1).Station != StationIdle || h.node(2).Station != StationIdle {
		t.Fatal("a refused input changed the station")
	}

	h.tick(2 * time.Second)
	h.clear()
	h.press("P1")
	if got := h.typesFor("P1"); !reflect.DeepEqual(got, []string{"phase", "status"}) || !reflect.DeepEqual(h.phasesFor("P1"), []Phase{Select}) {
		t.Fatalf("READY: %v", got)
	}
	if got := lastStatus(t, h, "P1"); !reflect.DeepEqual(got, StatusData{Window: 20, Gains: []int{}}) {
		t.Fatalf("selection status: %+v", got)
	}
	if n := h.node(1); n.Station != StationSelect || n.WindowSeconds != 20 {
		t.Fatalf("selection snapshot: %+v", n)
	}
	h.clear()
	h.press("P1")
	if got := h.typesFor("P1"); !reflect.DeepEqual(got, []string{"status"}) {
		t.Fatalf("READY during the selection: %v", got)
	}

	h.clear()
	h.choose("P1", RoundNormal)
	if got := h.typesFor("P1"); !reflect.DeepEqual(got, []string{"phase", "status"}) || !reflect.DeepEqual(h.phasesFor("P1"), []Phase{Playing}) {
		t.Fatalf("CHOOSE: %v", got)
	}
	if got := lastStatus(t, h, "P1"); !reflect.DeepEqual(got, StatusData{Remaining: gameSeconds, Countdown: 3, Round: roundOf(RoundNormal), Gains: []int{}}) {
		t.Fatalf("round status: %+v", got)
	}
	if n := h.node(1); n.Station != StationPlaying || n.Round != RoundNormal || n.CountdownSeconds != 3 || n.RemainingSeconds != gameSeconds {
		t.Fatalf("round snapshot: %+v", n)
	}
	h.clear()
	h.press("P1")
	h.choose("P1", RoundTutorial)
	if got := h.typesFor("P1"); !reflect.DeepEqual(got, []string{"status", "status"}) || h.node(1).Round != RoundNormal {
		t.Fatalf("READY and CHOOSE during a normal round: %v", got)
	}

	// An invalid selection is a protocol error.
	if err := h.a.MessageReceived(h.ctx, "P1", IncomingMessage{Identification: h.identification("P1"), Type: "choose", Sync: h.sync("P1"), Round: "hard"}); err == nil {
		t.Fatal("CHOOSE of an unknown round accepted")
	}
}

func TestSelectionTimesOutAfter20Seconds(t *testing.T) {
	h := endlessHarness(t, Options{}, 1)
	h.execute(Command{Operation: Start})
	h.quiet()
	h.tick(5 * time.Second)
	h.press("P1")
	h.tick(24900 * time.Millisecond)
	if n := h.node(1); n.Station != StationSelect || n.WindowSeconds != 1 {
		t.Fatalf("before the timeout: %+v", n)
	}
	h.clear()
	h.tick(25 * time.Second)
	if n := h.node(1); n.Station != StationIdle || n.WindowSeconds != 0 {
		t.Fatalf("after the timeout: %+v", n)
	}
	if got := h.typesFor("P1"); !reflect.DeepEqual(got, []string{"phase", "status"}) || !reflect.DeepEqual(h.phasesFor("P1"), []Phase{Idle}) {
		t.Fatalf("timeout messages: %v", got)
	}
}

func TestNormalRoundCountsOnlyItsGameTimeAndSettlesItsResult(t *testing.T) {
	saver := &gameSaver{}
	h := endlessHarness(t, Options{SaveGame: saver.save}, 1)
	for _, node := range ledNodes {
		h.connectLED(ledSession(node), ledUID(node), node)
	}
	h.execute(Command{Operation: Start})
	h.quiet()
	h.tick(time.Second)
	h.play("P1", RoundNormal)

	// The countdown refuses SEND and stops automatic transfer.
	h.tick(2 * time.Second)
	h.setFIFO(1, testPacket(60000, 2, 1))
	h.clear()
	if r := h.send("P1", 60000, 2); r.OK {
		t.Fatal("SEND during the countdown accepted")
	}
	if got := h.typesFor("P1"); !reflect.DeepEqual(got, []string{"result", "reset"}) {
		t.Fatalf("refused SEND: %v", got)
	}
	h.tick(3900 * time.Millisecond)
	if n := h.node(1); n.FIFOUsage != 1 || n.CountdownSeconds != 1 {
		t.Fatalf("automatic transfer during the countdown: %+v", n)
	}
	h.tick(4 * time.Second)
	if r := h.send("P1", 60000, 2); !r.OK {
		t.Fatal("SEND after the countdown refused")
	}
	if n := h.node(1); n.Score != 15 || n.RemainingSeconds != gameSeconds || n.CountdownSeconds != 0 {
		t.Fatalf("after the first delivery: %+v", n)
	}
	h.tick(50 * time.Second)
	if n := h.node(1); n.RemainingSeconds != gameSeconds-46 {
		t.Fatalf("remaining at 50s: %+v", n)
	}

	h.clear()
	h.tick(94 * time.Second)
	if got := h.typesFor("P1"); !reflect.DeepEqual(got, []string{"phase", "status", "summary"}) || !reflect.DeepEqual(h.phasesFor("P1"), []Phase{Finished}) {
		t.Fatalf("round end: %v", got)
	}
	saved := saver.saved()
	want := GameRecord{Kind: RecordRound, FinishedAt: epoch.Add(94 * time.Second), Duration: GameDuration, Elapsed: GameDuration, Reason: FinishTimeout,
		Results: []PlayerResult{{Node: 1, Score: 15, Eligible: true, Rank: 1}}}
	if len(saved) != 1 || !saved[0].FinishedAt.Equal(want.FinishedAt) {
		t.Fatalf("saved %+v", saved)
	}
	saved[0].FinishedAt = want.FinishedAt
	if !reflect.DeepEqual(saved[0], want) {
		t.Fatalf("saved %+v, want %+v", saved[0], want)
	}
	summary := h.summary("P1")
	// Alone in the population, the round is at 100%, which the player only
	// hears as the bottom half.
	if summary.Score != 15 || summary.Rank != nil || !summary.Eligible || summary.TopPercent != nil || !summary.BottomHalf || *summary.GameBest != 15 ||
		!reflect.DeepEqual(summary.GameBestNodes, []int{1}) || !summary.AllTimeBestMine || !summary.HourBestMine {
		t.Fatalf("summary: %+v", summary)
	}
	if got := lastStatus(t, h, "P1"); got.Score != 15 || got.Window != 30 || got.Round == nil || *got.Round != RoundNormal || got.Rank != nil || got.Remaining != 0 {
		t.Fatalf("result status: %+v", got)
	}
	if got := h.cheersSent(); !reflect.DeepEqual(got, p1Cheers("record")) {
		t.Fatalf("cheers: %v", got)
	}
	s := h.state()
	if n := s.Nodes[0]; n.Station != StationFinished || n.Score != 15 || *n.TopPercent != 100 || n.WindowSeconds != 30 || n.Rank != nil {
		t.Fatalf("result snapshot: %+v", n)
	}
	if got := s.Records.RecentRounds; len(got) != 1 || got[0].Node != 1 || got[0].Score != 15 || got[0].TopPercent != 100 || !got[0].FinishedAt.Equal(want.FinishedAt) {
		t.Fatalf("recent rounds: %+v", got)
	}
	// Automatic transfer resumes at the result.
	h.tick(96 * time.Second)
	if h.node(1).FIFOUsage > 1 {
		t.Fatalf("the result did not transfer automatically: %+v", h.node(1))
	}

	h.clear()
	h.tick(124 * time.Second)
	if n := h.node(1); n.Station != StationIdle || n.Score != 0 || n.TopPercent != nil {
		t.Fatalf("after the result: %+v", n)
	}
	if got := h.phasesFor("P1"); !reflect.DeepEqual(got, []Phase{Idle}) {
		t.Fatalf("result timeout: %v", got)
	}
	if len(saver.saved()) != 1 || len(h.state().Records.RecentRounds) != 1 {
		t.Fatal("the result timeout changed the records")
	}
}

func TestLongPressDuringTheResultSelectsAgain(t *testing.T) {
	saver := &gameSaver{}
	h := endlessHarness(t, Options{SaveGame: saver.save}, 1)
	h.execute(Command{Operation: Start})
	h.quiet()
	h.play("P1", RoundNormal)
	h.tick(100 * time.Second)
	// The result runs from 93s.
	h.clear()
	h.press("P1")
	if got := h.typesFor("P1"); !reflect.DeepEqual(got, []string{"phase", "status"}) || !reflect.DeepEqual(h.phasesFor("P1"), []Phase{Select}) {
		t.Fatalf("READY during the result: %v", got)
	}
	if n := h.node(1); n.Station != StationSelect || n.TopPercent != nil || n.Score != 0 || n.WindowSeconds != 20 {
		t.Fatalf("selection after the result: %+v", n)
	}
	if len(saver.saved()) != 1 {
		t.Fatal("leaving the result changed the record")
	}
	h.choose("P1", RoundNormal)
	if n := h.node(1); n.Station != StationPlaying || n.RemainingSeconds != gameSeconds {
		t.Fatalf("second round: %+v", n)
	}
}

// Two neighbors play rounds that overlap without being aligned.
func TestNeighborsPlayIndependentRounds(t *testing.T) {
	saver := &gameSaver{}
	h := endlessHarness(t, Options{SaveGame: saver.save}, 1, 2)
	h.execute(Command{Operation: Start})
	h.tick(time.Second)
	h.play("P1", RoundNormal) // Game time 4s-94s.
	h.tick(32 * time.Second)
	h.play("P2", RoundNormal) // Game time 35s-125s.

	h.tick(10 * time.Second)
	h.setFIFO(1, testPacket(60000, 2, 1), testPacket(60001, 2, 1))
	h.send("P1", 60000, 2)
	h.tick(40 * time.Second)
	h.setFIFO(2, testPacket(60002, 1, 2))
	h.send("P2", 60002, 1)
	h.send("P1", 60001, 2)
	if s := h.state(); s.Nodes[0].Score != 30 || s.Nodes[1].Score != 15 {
		t.Fatalf("scores: P1=%d P2=%d", s.Nodes[0].Score, s.Nodes[1].Score)
	}

	h.tick(94 * time.Second)
	s := h.state()
	if s.Nodes[0].Station != StationFinished || s.Nodes[1].Station != StationPlaying || s.Nodes[1].RemainingSeconds != 31 {
		t.Fatalf("at P1's end: %+v %+v", s.Nodes[0], s.Nodes[1])
	}
	h.tick(125 * time.Second)
	saved := saver.saved()
	if len(saved) != 2 || saved[0].Results[0] != (PlayerResult{1, 30, true, 1}) || saved[1].Results[0] != (PlayerResult{2, 15, true, 1}) ||
		!saved[1].FinishedAt.Equal(epoch.Add(125*time.Second)) {
		t.Fatalf("saved %+v", saved)
	}
	s = h.state()
	// P2's population holds P1's round too.
	if s.Nodes[1].Station != StationFinished || *s.Nodes[1].TopPercent != 100 || s.Nodes[0].Station != StationIdle {
		t.Fatalf("at P2's end: %+v %+v", s.Nodes[0], s.Nodes[1])
	}
	if got := s.Records.RecentRounds; len(got) != 2 || got[0].Node != 2 || got[1].Node != 1 || got[1].TopPercent != 100 {
		t.Fatalf("recent rounds: %+v", got)
	}
	if got := h.summary("P2"); got.AllTimeBestMine || *got.AllTimeBest != 30 || *got.GameBest != 15 || !reflect.DeepEqual(got.GameBestNodes, []int{2}) {
		t.Fatalf("P2 summary: %+v", got)
	}
}

// Rounds that end in one input are recorded in the order of their ends, and
// of their nodes at the same end, so that the earlier keeps a tied best.
func TestRoundsEndingInOneTickAreRecordedInOrder(t *testing.T) {
	saver := &gameSaver{}
	h := endlessHarness(t, Options{SaveGame: saver.save}, 1, 2, 3)
	h.execute(Command{Operation: Start})
	h.quiet()
	h.tick(500 * time.Millisecond)
	h.play("P3", RoundNormal) // Ends at 93.5s.
	h.tick(time.Second)
	h.play("P2", RoundNormal) // P2 and P1 end at 94s.
	h.play("P1", RoundNormal)
	h.tick(200 * time.Second)
	var order []NodeID
	for _, r := range saver.saved() {
		order = append(order, r.Results[0].Node)
	}
	if !reflect.DeepEqual(order, []NodeID{3, 1, 2}) {
		t.Fatalf("records in order %v, want [3 1 2]", order)
	}
	s := h.state()
	if s.Records.AllTimeBest == nil || !reflect.DeepEqual(s.Records.AllTimeBest.Nodes, []NodeID{3}) {
		t.Fatalf("tied all-time best: %+v", s.Records.AllTimeBest)
	}
	var recent []NodeID
	for _, r := range s.Records.RecentRounds {
		recent = append(recent, r.Node)
	}
	if !reflect.DeepEqual(recent, []NodeID{2, 1, 3}) {
		t.Fatalf("recent rounds %v, want [2 1 3]", recent)
	}
}

func TestAbortedRoundsAreNotRecorded(t *testing.T) {
	for _, tc := range []struct {
		name   string
		abort  func(h *harness)
		types  []string
		manual bool
	}{
		{"return to bot", func(h *harness) { h.execute(Command{Operation: ReturnToBot, Node: 1}) }, []string{"phase", "status"}, true},
		{"automatic", func(h *harness) { h.execute(Command{Operation: SetNodeEnabled, Node: 1}) }, []string{"config", "reset", "phase", "status"}, false},
		{"disconnect", func(h *harness) { h.must(h.a.Disconnected(h.ctx, "P1")) }, nil, false},
		{"stop", func(h *harness) { h.execute(Command{Operation: Stop}) }, []string{"phase", "reset", "status"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			saver := &gameSaver{}
			h := endlessHarness(t, Options{SaveGame: saver.save}, 1)
			for _, node := range ledNodes {
				h.connectLED(ledSession(node), ledUID(node), node)
			}
			h.execute(Command{Operation: Start})
			h.quiet()
			h.play("P1", RoundNormal)
			h.tick(10 * time.Second)
			h.setFIFO(1, testPacket(60000, 2, 1))
			h.send("P1", 60000, 2)
			h.clear()
			tc.abort(h)
			if got := h.typesFor("P1"); !reflect.DeepEqual(got, tc.types) {
				t.Fatalf("messages %v, want %v", got, tc.types)
			}
			n := h.node(1)
			if n.Score != 0 || n.Enabled != tc.manual || n.TopPercent != nil || (n.Station != StationIdle && n.Station != StationStopped) {
				t.Fatalf("after the abort: %+v", n)
			}
			h.tick(200 * time.Second)
			if len(saver.saved()) != 0 || len(h.state().Records.RecentRounds) != 0 || len(h.cheersSent()) != 0 {
				t.Fatalf("the aborted round was recorded: %+v", saver.saved())
			}
		})
	}
}

func TestReturnToBot(t *testing.T) {
	saver := &gameSaver{}
	h := endlessHarness(t, Options{SaveGame: saver.save}, 1)
	h.execute(Command{Operation: Start})
	h.quiet()
	expectCode(t, h.a.Execute(h.ctx, Command{Operation: ReturnToBot, Node: 9}), CodeInvalid)
	h.clear()
	h.execute(Command{Operation: ReturnToBot, Node: 1})
	if len(h.messages()) != 0 {
		t.Fatal("ReturnToBot of an idle player sent something")
	}
	// A result cut short keeps its record.
	h.play("P1", RoundNormal)
	h.tick(100 * time.Second)
	h.execute(Command{Operation: ReturnToBot, Node: 1})
	if n := h.node(1); n.Station != StationIdle || !n.Enabled || n.TopPercent != nil {
		t.Fatalf("after ReturnToBot: %+v", n)
	}
	if len(saver.saved()) != 1 || len(h.state().Records.RecentRounds) != 1 {
		t.Fatal("ReturnToBot changed a saved result")
	}
	// The player can start again at once.
	h.play("P1", RoundNormal)
	if h.node(1).Station != StationPlaying {
		t.Fatal("ReturnToBot stopped accepting the player")
	}
}

func TestFIFOLimitCutsOnlyNormalRounds(t *testing.T) {
	h := endlessHarness(t, Options{}, 1)
	h.execute(Command{Operation: Start})
	full := make([]*packet, maxRoundPackets-1)
	for i := range full {
		full[i] = testPacket(uint16(30000+i), 2, 1)
	}
	// Idle FIFOs have no limit.
	h.setFIFO(1, append(slices.Clone(full), testPacket(29999, 2, 1), testPacket(29998, 2, 1))...)
	h.tick(500 * time.Millisecond)
	if h.node(1).Station != StationIdle {
		t.Fatal("an idle FIFO was cut")
	}

	h.play("P1", RoundNormal)
	h.setFIFO(1, slices.Clone(full)...)
	h.clear()
	h.tick(1200 * time.Millisecond) // P1 generates its 1000th packet at 1s.
	if n := h.node(1); n.Station != StationIdle || n.FIFOUsage != maxRoundPackets {
		t.Fatalf("after reaching the limit: %+v", n)
	}
	if got := h.typesFor("P1"); !reflect.DeepEqual(got, []string{"push", "phase", "status"}) {
		t.Fatalf("limit messages: %v", got)
	}
	// Automatic transfer resumes from the abort.
	h.tick(2 * time.Second)
	if h.node(1).FIFOUsage >= maxRoundPackets {
		t.Fatal("the aborted node did not transfer automatically")
	}
}

func TestPIDsWrapAroundAndSkipThoseInUse(t *testing.T) {
	g := newGame(Endless, func(change) {})
	g.nextPID = 65534
	for _, pid := range []uint16{65535, 0, 2} {
		g.used[pid/64] |= 1 << (pid % 64)
	}
	var got []uint16
	for range 3 {
		got = append(got, g.allocatePID())
	}
	if !reflect.DeepEqual(got, []uint16{65534, 1, 3}) {
		t.Fatalf("allocated %v, want [65534 1 3]", got)
	}

	h := endlessHarness(t, Options{})
	h.do(func() { h.a.game.firstPID = 65530 })
	for range 2 {
		h.tick(0)
		h.execute(Command{Operation: Start})
		h.tick(time.Second)
		var pids []int
		h.do(func() {
			for id := NodeID(1); id <= PlayerCount; id++ {
				pids = append(pids, int(h.a.game.nodes[id].fifo[0].PID))
			}
		})
		if want := []int{65530, 65531, 65532, 65533, 65534, 65535, 0, 1}; !reflect.DeepEqual(pids, want) {
			t.Fatalf("first PIDs %v, want %v", pids, want)
		}
		h.execute(Command{Operation: Stop})
	}
}

// However long the operation runs, the PIDs in use are those of the packets
// in the network, and no two of them are alike.
func TestPIDsInUseMatchThePackets(t *testing.T) {
	g := newGame(Endless, func(change) {})
	r := rand.New(rand.NewPCG(1, 2))
	g.random = r.IntN
	g.nextPID = 65000
	if err := g.start(epoch); err != nil {
		t.Fatal(err)
	}
	for at := time.Duration(0); at < 2*time.Hour; at += 37 * time.Second {
		g.advance(epoch.Add(at))
		seen := make(map[uint16]bool)
		for id := NodeID(1); id <= NodeCount; id++ {
			for _, p := range g.nodes[id].fifo {
				if seen[p.PID] {
					t.Fatalf("PID %d twice at %s", p.PID, at)
				}
				seen[p.PID] = true
			}
		}
		used := 0
		for _, word := range g.used {
			used += bits.OnesCount64(word)
		}
		if used != len(seen) {
			t.Fatalf("%d PIDs in use for %d packets at %s", used, len(seen), at)
		}
	}
	if g.nextPID >= 65000 {
		t.Fatalf("PIDs did not wrap: next %d", g.nextPID)
	}
}

func TestPracticeQueueReplacesTheMCUQueue(t *testing.T) {
	h := endlessHarness(t, Options{}, 1)
	h.execute(Command{Operation: Start})
	h.quiet()
	h.tick(1500 * time.Millisecond)
	real := h.reset("P1").Packets
	h.do(func() { h.a.game.practiceRandom = func(n int) int { return n - 1 } }) // P8.
	h.clear()
	h.play("P1", RoundTutorial)
	if got := h.typesFor("P1"); !reflect.DeepEqual(got, []string{"phase", "status", "phase", "reset", "status"}) || !reflect.DeepEqual(h.phasesFor("P1"), []Phase{Select, Playing}) {
		t.Fatalf("entering the practice: %v", got)
	}
	practice := []PacketView{{0, 8}, {1, 8}, {2, 8}}
	if got := h.reset("P1").Packets; !reflect.DeepEqual(got, practice) {
		t.Fatalf("practice reset %v, want %v", got, practice)
	}
	status := lastStatus(t, h, "P1")
	if !reflect.DeepEqual(status, StatusData{Countdown: 3, Round: roundOf(RoundTutorial), Window: 60, Gains: []int{}, Bots: []int{0, 0, 0}}) {
		t.Fatalf("practice status: %+v", status)
	}
	if n := h.node(1); n.Station != StationPlaying || n.Round != RoundTutorial || n.WindowSeconds != 60 || n.RemainingSeconds != 0 {
		t.Fatalf("practice snapshot: %+v", n)
	}

	// A resync sends the practice queue.
	h.clear()
	h.message("P1", IncomingMessage{Type: "resync"})
	if got := h.typesFor("P1"); !reflect.DeepEqual(got, []string{"reset", "phase", "status"}) || !reflect.DeepEqual(h.reset("P1").Packets, practice) {
		t.Fatalf("resync during the practice: %v", got)
	}

	// The real FIFO's changes reach the MCU no more.
	h.clear()
	h.tick(20 * time.Second)
	for _, m := range h.messagesFor("P1") {
		if m.Type == "push" || m.Type == "reset" {
			t.Fatalf("real FIFO change sent during the practice: %+v", m)
		}
	}

	// Leaving swaps the real FIFO back.
	h.clear()
	h.press("P1")
	if got := h.typesFor("P1"); !reflect.DeepEqual(got, []string{"phase", "reset", "status"}) || !reflect.DeepEqual(h.phasesFor("P1"), []Phase{Select}) {
		t.Fatalf("leaving the practice: %v", got)
	}
	var fifo []PacketView
	h.do(func() {
		for _, p := range h.a.game.nodes[1].fifo {
			fifo = append(fifo, p.PacketView)
		}
	})
	if got := h.reset("P1").Packets; !reflect.DeepEqual(got, append(make([]PacketView, 0), fifo...)) {
		t.Fatalf("reset after the practice %v, want the real FIFO %v (was %v)", got, fifo, real)
	}
	if got := lastStatus(t, h, "P1"); got.Bots != nil || got.Round != nil {
		t.Fatalf("selection status after the practice: %+v", got)
	}
}

func TestPracticeSendTouchesOnlyThePracticeQueue(t *testing.T) {
	saver := &gameSaver{}
	h := endlessHarness(t, Options{SaveGame: saver.save}, 1)
	for _, node := range ledNodes {
		h.connectLED(ledSession(node), ledUID(node), node)
	}
	h.execute(Command{Operation: Start})
	h.quiet()
	h.tick(time.Second)
	h.play("P1", RoundTutorial)
	h.tick(2 * time.Second)
	if r := h.send("P1", 0, 2); r.OK {
		t.Fatal("practice SEND during the countdown accepted")
	}
	if got := h.reset("P1").Packets; len(got) != practiceLength || got[0].PID != 0 {
		t.Fatalf("refused practice SEND reset %v", got)
	}

	h.tick(4 * time.Second)
	h.setFIFO(9, testPacket(60000, 3, 4), testPacket(60001, 3, 4), testPacket(60002, 3, 4), testPacket(60003, 3, 4))
	h.setFIFO(1, testPacket(60004, 2, 8, 1))
	before := h.state()
	h.clear()
	// To P2, then to the full B1: neither is lost or lit.
	for i, to := range []NodeID{2, 9, 8, 11} {
		// The queue starts with PIDs 0-2 and each refill takes the next one.
		if r := h.send("P1", uint16(i), to); !r.OK {
			t.Fatalf("practice SEND %d to %s refused", i, to.wireName())
		}
	}
	got := h.messagesFor("P1")
	if types := h.typesFor("P1"); !reflect.DeepEqual(types[:2], []string{"result", "push"}) {
		t.Fatalf("practice SEND messages: %v", types)
	}
	if p := got[1].Data.(PacketView); p.PID != 3 || p.Destination == 1 {
		t.Fatalf("refill %+v", p)
	}
	for _, node := range ledNodes {
		if lines := h.lines(ledSession(node)); len(lines) != 0 {
			t.Fatalf("practice SEND lit %s: %v", node.wireName(), lines)
		}
	}
	after := h.state()
	if after.Nodes[0].FIFOUsage != before.Nodes[0].FIFOUsage || after.Nodes[8].FIFOUsage != botCapacity {
		t.Fatalf("practice SEND changed real FIFOs: P1 %d->%d B1 %d", before.Nodes[0].FIFOUsage, after.Nodes[0].FIFOUsage, after.Nodes[8].FIFOUsage)
	}
	for i, n := range after.Nodes {
		if n.Score != 0 {
			t.Fatalf("node %d scored %d", i+1, n.Score)
		}
	}
	h.do(func() {
		if q := h.a.game.nodes[1].practice; len(q) != practiceLength || q[0].PID != 4 || q[2].PID != 6 {
			t.Errorf("practice queue %v", q)
		}
		if h.a.game.nodes[1].combo != 0 {
			t.Error("practice SEND counted a combo")
		}
	})

	// A neighbor that is not adjacent, or another PID, is refused with a
	// reset of the practice queue.
	h.clear()
	if r := h.send("P1", 4, 5); r.OK {
		t.Fatal("practice SEND to a node not adjacent accepted")
	}
	if r := h.send("P1", 5, 2); r.OK {
		t.Fatal("practice SEND of the second packet accepted")
	}
	if types := h.typesFor("P1"); !reflect.DeepEqual(types, []string{"result", "reset", "result", "reset"}) {
		t.Fatalf("refused practice SENDs: %v", types)
	}
	if q := h.reset("P1").Packets; len(q) != practiceLength || q[0].PID != 4 {
		t.Fatalf("practice reset %v", q)
	}
	h.tick(300 * time.Second)
	if len(saver.saved()) != 0 || len(h.cheersSent()) != 0 {
		t.Fatal("the practice was recorded or cheered")
	}
}

func TestPracticeKeepsTheRealFIFOAutomatic(t *testing.T) {
	h := endlessHarness(t, Options{}, 1)
	h.execute(Command{Operation: Start})
	h.play("P1", RoundTutorial)
	h.tick(10 * time.Second)
	h.setFIFO(1, testPacket(60000, 2, 1), testPacket(60001, 2, 1), testPacket(60002, 2, 1), testPacket(60003, 2, 1))
	h.tick(11 * time.Second)
	// 2 pkt/s drain the queue while P1 generates 1 pkt/s and sends nothing.
	if n := h.node(1); n.FIFOUsage >= 4 || n.Score != 0 {
		t.Fatalf("real FIFO during the practice: %+v", n)
	}
	h.tick(13 * time.Second)
	if n := h.node(1); n.FIFOUsage > 1 {
		t.Fatalf("real FIFO did not keep draining: %+v", n)
	}
}

func TestPracticeStatusShowsTheBotsOnly(t *testing.T) {
	h := endlessHarness(t, Options{}, 1, 2)
	h.execute(Command{Operation: Start})
	h.quiet()
	h.play("P1", RoundTutorial)
	h.play("P2", RoundNormal)
	h.tick(4 * time.Second)
	h.clear()
	h.setFIFO(10, testPacket(60000, 3, 4), testPacket(60001, 3, 4))
	h.setFIFO(11, testPacket(60002, 3, 4))
	h.tick(4100 * time.Millisecond)
	status := lastStatus(t, h, "P1")
	if !reflect.DeepEqual(status.Bots, []int{0, 2, 1}) || status.Score != 0 || len(status.Gains) != 0 || status.Combo != 0 || status.Remaining != 0 {
		t.Fatalf("practice status: %+v", status)
	}
	var usage []int
	h.do(func() {
		bots := h.a.game.botUsage()
		usage = bots[:]
	})
	if !reflect.DeepEqual(status.Bots, usage) {
		t.Fatalf("bots %v, real usage %v", status.Bots, usage)
	}
	// Other stations never carry bots.
	for _, line := range h.lines("P2") {
		if strings.Contains(line, `"bots"`) {
			t.Fatalf("normal round status with bots: %s", line)
		}
	}
	h.clear()
	h.tick(4200 * time.Millisecond)
	if len(h.statusMessages("P1")) != 0 {
		t.Fatal("unchanged bots resent the status")
	}
	// B2's transfer at 5s changes the bots: one status for that input.
	h.tick(5100 * time.Millisecond)
	if got := h.statusMessages("P1"); len(got) != 1 || reflect.DeepEqual(got[0].Bots, []int{0, 2, 1}) {
		t.Fatalf("statuses after a bot transfer: %+v", got)
	}
}

func TestPracticeEndsAfter60SecondsWithoutASend(t *testing.T) {
	h := endlessHarness(t, Options{}, 1)
	h.execute(Command{Operation: Start})
	h.quiet()
	h.tick(time.Second)
	h.play("P1", RoundTutorial) // The countdown ends at 4s.
	h.tick(30 * time.Second)
	if n := h.node(1); n.WindowSeconds != 34 {
		t.Fatalf("window at 30s: %+v", n)
	}
	h.send("P1", 0, 2)
	h.tick(89900 * time.Millisecond)
	if n := h.node(1); n.Station != StationPlaying || n.WindowSeconds != 1 {
		t.Fatalf("a SEND did not restart the idle timer: %+v", n)
	}
	h.clear()
	h.tick(90 * time.Second)
	if n := h.node(1); n.Station != StationIdle || !n.Enabled {
		t.Fatalf("after 60 idle seconds: %+v", n)
	}
	if got := h.typesFor("P1"); !reflect.DeepEqual(got, []string{"phase", "reset", "status"}) || !reflect.DeepEqual(h.phasesFor("P1"), []Phase{Idle}) {
		t.Fatalf("practice timeout: %v", got)
	}
}

// Practicing is invisible to the rest of the network: a neighbor's offset
// normal round scores the same whether the practice runs or not.
func TestPracticeDoesNotChangeANeighborsRound(t *testing.T) {
	play := func(practice bool) (int, []int) {
		h := endlessHarness(t, Options{}, 1, 2)
		h.do(func() {
			r := rand.New(rand.NewPCG(3, 4))
			h.a.game.random = r.IntN
			h.a.game.practiceRandom = rand.New(rand.NewPCG(5, 6)).IntN
		})
		h.execute(Command{Operation: Start})
		h.tick(2 * time.Second)
		if practice {
			h.play("P1", RoundTutorial)
		}
		h.tick(7 * time.Second)
		h.play("P2", RoundNormal) // Game time 10s-100s.
		for at := 10 * time.Second; at < 100*time.Second; at += 700 * time.Millisecond {
			h.tick(at)
			var head *packet
			h.do(func() {
				if q := h.a.game.nodes[2].fifo; len(q) > 0 {
					head = q[0]
				}
			})
			if head != nil {
				h.send("P2", head.PID, routes[2][head.Destination][0])
			}
			if practice && h.node(1).Station == StationPlaying {
				var pid uint16
				h.do(func() { pid = h.a.game.nodes[1].practice[0].PID })
				h.send("P1", pid, 2)
			}
		}
		h.tick(101 * time.Second)
		var fifos []int
		for _, n := range h.state().Nodes {
			fifos = append(fifos, n.FIFOUsage)
		}
		return h.node(2).Score, fifos
	}
	score, fifos := play(false)
	practiced, practicedFIFOs := play(true)
	if score == 0 || practiced != score || !reflect.DeepEqual(fifos, practicedFIFOs) {
		t.Fatalf("without practice %d %v, with practice %d %v", score, fifos, practiced, practicedFIFOs)
	}
}

func TestPracticeQueuePIDsSkipThoseQueued(t *testing.T) {
	g := newGame(Endless, func(change) {})
	g.practiceRandom = func(int) int { return 0 }
	n := &g.nodes[3]
	n.practice, n.practicePID = []PacketView{{65535, 1}, {1, 1}}, 65535
	if got := g.refillPractice(3); !reflect.DeepEqual(got, []PacketView{{0, 1}}) {
		t.Fatalf("refill %v", got)
	}
	n.practice = n.practice[1:]
	if got := g.refillPractice(3); !reflect.DeepEqual(got, []PacketView{{2, 1}}) {
		t.Fatalf("refill %v, want PID 2 past the queued 1", got)
	}
	// Destinations cover the other seven players uniformly.
	g.practiceRandom = rand.New(rand.NewPCG(7, 8)).IntN
	counts := make(map[NodeID]int)
	for range 7000 {
		n.practice = n.practice[1:]
		counts[g.refillPractice(3)[0].Destination]++
	}
	if counts[3] != 0 || len(counts) != PlayerCount-1 {
		t.Fatalf("destinations %v", counts)
	}
	for id, c := range counts {
		if c < 850 || c > 1150 {
			t.Fatalf("destination %d drawn %d times of 7000", id, c)
		}
	}
}

// Past simultaneous games of eight players and rounds form one population.
func TestRoundsJoinThePopulationOfSimultaneousGames(t *testing.T) {
	game := testRecord(epoch.Add(-time.Hour), FinishTimeout, map[NodeID]int{1: 500, 2: 300}, map[NodeID]int{3: 900})
	game.Kind = RecordGame
	round := GameRecord{Kind: RecordRound, FinishedAt: epoch.Add(-time.Minute), Duration: GameDuration, Elapsed: GameDuration, Reason: FinishTimeout,
		Results: []PlayerResult{{Node: 4, Score: 400, Eligible: true, Rank: 1}}}
	h := endlessHarness(t, Options{Records: []GameRecord{game, round}}, 1)
	h.execute(Command{Operation: Start})
	h.play("P1", RoundNormal)
	h.tick(50 * time.Second)
	h.do(func() { h.a.game.nodes[1].score = 450 })
	h.tick(GameDuration + CountdownDuration)
	// 500 alone is higher among 500, 300, 400 and 450: ceil(100 * 2 / 4).
	if got := h.node(1).TopPercent; got == nil || *got != 50 {
		t.Fatalf("top percent %v, want 50", got)
	}
	if s := h.summary("P1"); *s.AllTimeBest != 500 || s.AllTimeBestMine {
		t.Fatalf("summary: %+v", s)
	}
}

func TestAssignmentsWhileRunningRequireAutomaticPlayers(t *testing.T) {
	h := endlessHarness(t, Options{}, 1)
	h.connect("P2", playerUID(2), 2)
	h.execute(Command{Operation: Start})
	expectCode(t, h.a.Execute(h.ctx, Command{Operation: SwapDeviceAssignments, DeviceID: playerUID(1), OtherDeviceID: playerUID(2)}), CodeManualModeConflict)
	expectCode(t, h.a.Execute(h.ctx, Command{Operation: SetDevicePreview, DeviceID: playerUID(1)}), CodeManualModeConflict)
	h.execute(Command{Operation: SetDevicePreview, DeviceID: playerUID(2)})
	h.execute(Command{Operation: SetNodeEnabled, Node: 1})
	h.execute(Command{Operation: SwapDeviceAssignments, DeviceID: playerUID(1), OtherDeviceID: playerUID(2)})
	if d := h.device(playerUID(1)); d.Node != 2 {
		t.Fatalf("swap while running: %+v", d)
	}
	// A new board is assigned while running.
	h.must(h.a.Connected(h.ctx, "P3"))
	h.message("P3", IncomingMessage{Type: "hello", UID: playerUID(3)})
	if d := h.device(playerUID(3)); d.Node != 3 {
		t.Fatalf("automatic assignment while running: %+v", d)
	}
	if got := h.phasesFor("P3"); !reflect.DeepEqual(got, []Phase{Idle}) {
		t.Fatalf("new board phases %v", got)
	}
}

// Switching every player to automatic cuts each round short, synchronizes
// each MCU from its config and resumes automatic transfer.
func TestBulkAutomaticSwitchCutsEveryRound(t *testing.T) {
	saver := &gameSaver{}
	h := endlessHarness(t, Options{SaveGame: saver.save}, 1, 2)
	h.execute(Command{Operation: Start})
	h.quiet()
	h.play("P1", RoundNormal)
	h.play("P2", RoundTutorial)
	h.tick(10 * time.Second)
	h.setFIFO(1, testPacket(60000, 2, 1), testPacket(60001, 2, 1))
	h.clear()
	if _, err := h.a.ExecuteWithResult(h.ctx, Command{Operation: SetAllNodesEnabled}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []SessionID{"P1", "P2"} {
		if got := h.typesFor(id); !reflect.DeepEqual(got, []string{"config", "reset", "phase", "status"}) || !reflect.DeepEqual(h.phasesFor(id), []Phase{Idle}) {
			t.Fatalf("%s: %v", id, got)
		}
	}
	// P2's MCU gets its real FIFO back, not the practice queue.
	if got := h.reset("P2").Packets; len(got) != 0 {
		t.Fatalf("P2 reset %v", got)
	}
	s := h.state()
	for _, n := range s.Nodes[:2] {
		if n.Enabled || n.Station != StationIdle || n.Round != "" {
			t.Fatalf("after the switch: %+v", n)
		}
	}
	// P1's FIFO transfers automatically again, from the switch.
	h.tick(10500 * time.Millisecond)
	if n := h.node(1); n.FIFOUsage != 1 {
		t.Fatalf("P1 FIFO after 0.5s: %+v", n)
	}
	h.tick(200 * time.Second)
	if len(saver.saved()) != 0 {
		t.Fatalf("a switched round was recorded: %+v", saver.saved())
	}
}
