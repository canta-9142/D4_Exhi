package app

import (
	"reflect"
	"testing"
	"time"
)

// startCountdown starts a game with a manual P1 at test time 0. Unlike
// harness.start, the countdown runs from 0 to CountdownDuration.
func startCountdown(t *testing.T) *harness {
	t.Helper()
	h := newSimultaneousHarness(t)
	h.connect("p1", testUID, 1)
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
	h.ready("p1")
	h.clear()
	h.execute(Command{Operation: Start})
	return h
}

func queuedPackets(s Snapshot) int {
	total := 0
	for _, n := range s.Nodes {
		total += n.FIFOUsage
	}
	return total
}

func TestCountdownPrecedesGameTime(t *testing.T) {
	h := startCountdown(t)
	s := h.state()
	if s.Phase != Playing || s.CountdownSeconds != 3 || s.Elapsed != 0 || s.RemainingSeconds != gameSeconds || queuedPackets(s) != 0 {
		t.Fatalf("Start: phase=%s countdown=%d elapsed=%s remaining=%d packets=%d", s.Phase, s.CountdownSeconds, s.Elapsed, s.RemainingSeconds, queuedPackets(s))
	}
	if got := h.statusMessages("p1"); !reflect.DeepEqual(got, []StatusData{{Remaining: gameSeconds, Ready: true, Countdown: 3, Round: roundOf(RoundNormal), Gains: []int{}}}) {
		t.Fatalf("Start status: %v", got)
	}
	for _, tc := range []struct {
		at        time.Duration
		countdown int
	}{
		{999 * time.Millisecond, 3},
		{time.Second, 2},
		{2900 * time.Millisecond, 1},
		{CountdownDuration, 0},
	} {
		h.tick(tc.at)
		if s := h.state(); s.CountdownSeconds != tc.countdown || s.Elapsed != 0 || s.RemainingSeconds != gameSeconds || queuedPackets(s) != 0 {
			t.Fatalf("at %s: countdown=%d elapsed=%s remaining=%d packets=%d, want countdown=%d", tc.at, s.CountdownSeconds, s.Elapsed, s.RemainingSeconds, queuedPackets(s), tc.countdown)
		}
	}
	// The first generation keeps its time relative to the game clock origin.
	h.tick(CountdownDuration + 999*time.Millisecond)
	if s := h.state(); queuedPackets(s) != 0 || s.RemainingSeconds != gameSeconds {
		t.Fatalf("generated before Start + 4s: packets=%d remaining=%d", queuedPackets(s), s.RemainingSeconds)
	}
	h.tick(CountdownDuration + time.Second)
	if s := h.state(); queuedPackets(s) != PlayerCount || s.Elapsed != time.Second || s.RemainingSeconds != gameSeconds-1 || s.CountdownSeconds != 0 {
		t.Fatalf("Start + 4s: packets=%d elapsed=%s remaining=%d countdown=%d", queuedPackets(s), s.Elapsed, s.RemainingSeconds, s.CountdownSeconds)
	}
}

func TestCountdownStatusIsSentOncePerValue(t *testing.T) {
	h := startCountdown(t)
	for at := 100 * time.Millisecond; at <= CountdownDuration+500*time.Millisecond; at += 100 * time.Millisecond {
		h.tick(at)
	}
	var countdowns []int
	for _, s := range h.statusMessages("p1") {
		if s.Remaining != gameSeconds || s.Score != 0 || !s.Ready {
			t.Fatalf("countdown changed other status fields: %+v", s)
		}
		countdowns = append(countdowns, s.Countdown)
	}
	if !reflect.DeepEqual(countdowns, []int{3, 2, 1, 0}) {
		t.Fatalf("countdown statuses = %v, want [3 2 1 0]", countdowns)
	}
}

// The admin page and the MCUs show the same digit after every tick.
func TestSnapshotCountdownMatchesStatus(t *testing.T) {
	h := startCountdown(t)
	for at := 100 * time.Millisecond; at <= CountdownDuration+200*time.Millisecond; at += 100 * time.Millisecond {
		h.tick(at)
		statuses := h.statusMessages("p1")
		if got, want := h.state().CountdownSeconds, statuses[len(statuses)-1].Countdown; got != want {
			t.Fatalf("at %s: Snapshot countdown %d, status countdown %d", at, got, want)
		}
	}
}

func TestCountdownKeepsPlayingRules(t *testing.T) {
	h := startCountdown(t)
	h.tick(1500 * time.Millisecond)
	sync := h.currentSync("p1")

	h.clear()
	h.message("p1", IncomingMessage{Type: "send", Sync: sync, RID: 1, PID: 0, To: 2})
	var results []ResultData
	for _, m := range h.messagesFor("p1") {
		if m.Type == "result" {
			results = append(results, m.Data.(ResultData))
		}
	}
	if !reflect.DeepEqual(results, []ResultData{{Sync: sync, RID: 1, OK: false}}) {
		t.Fatalf("SEND during the countdown: %v", results)
	}

	h.clear()
	h.press("p1")
	if got := h.statusMessages("p1"); !reflect.DeepEqual(got, []StatusData{{Remaining: gameSeconds, Ready: true, Countdown: 2, Round: roundOf(RoundNormal), Gains: []int{}}}) {
		t.Fatalf("READY during the countdown: %v", got)
	}
	if s := h.state(); !s.Nodes[0].Ready || !s.Nodes[0].Enabled {
		t.Fatal("READY during the countdown changed the player")
	}

	expectCode(t, h.a.Execute(h.ctx, Command{Operation: SetNodeEnabled, Node: 1, Enabled: false}), CodePhaseConflict)
	expectCode(t, h.a.Execute(h.ctx, Command{Operation: SetDevicePreview, DeviceID: testUID}), CodePhaseConflict)
	if s := h.state(); !s.Nodes[0].Enabled || !s.Nodes[0].RankingEligible || s.Preview != nil || s.CountdownSeconds != 2 {
		t.Fatal("a refused command changed the game")
	}
}

func TestDisconnectDuringCountdownLosesEligibility(t *testing.T) {
	h := startCountdown(t)
	h.tick(time.Second)
	h.must(h.a.Disconnected(h.ctx, "p1"))
	if s := h.state(); s.Nodes[0].Enabled || s.Nodes[0].RankingEligible {
		t.Fatalf("disconnect during the countdown kept the ranking: %+v", s.Nodes[0])
	}
	h.tick(GameDuration + CountdownDuration)
	if s := h.state(); s.Phase != Finished || s.Nodes[0].RankingEligible || s.Nodes[0].Rank != nil {
		t.Fatalf("final result: phase=%s %+v", s.Phase, s.Nodes[0])
	}
}

func TestResetDuringCountdown(t *testing.T) {
	h := startCountdown(t)
	h.tick(time.Second)
	h.clear()
	h.execute(Command{Operation: Reset})
	if s := h.state(); s.Phase != Preparing || s.CountdownSeconds != 0 || s.Elapsed != 0 || s.RemainingSeconds != gameSeconds {
		t.Fatalf("Reset: phase=%s countdown=%d elapsed=%s remaining=%d", s.Phase, s.CountdownSeconds, s.Elapsed, s.RemainingSeconds)
	}
	if got := h.statusMessages("p1"); !reflect.DeepEqual(got, []StatusData{{Gains: []int{}}}) {
		t.Fatalf("Reset status: %v", got)
	}
	h.tick(CountdownDuration + 5*time.Second)
	if s := h.state(); s.Phase != Preparing || queuedPackets(s) != 0 || s.CountdownSeconds != 0 {
		t.Fatal("the reset countdown still started the game")
	}
}

func TestFinishDuringCountdown(t *testing.T) {
	h := startCountdown(t)
	h.tick(2 * time.Second)
	h.clear()
	h.execute(Command{Operation: Finish})
	s := h.state()
	if s.Phase != Finished || s.CountdownSeconds != 0 || s.Elapsed != 0 || s.RemainingSeconds != gameSeconds || queuedPackets(s) != 0 {
		t.Fatalf("Finish: phase=%s countdown=%d elapsed=%s remaining=%d packets=%d", s.Phase, s.CountdownSeconds, s.Elapsed, s.RemainingSeconds, queuedPackets(s))
	}
	if p := s.Nodes[0]; p.Score != 0 || !p.RankingEligible || p.Rank == nil || *p.Rank != 1 {
		t.Fatalf("final result: %+v", p)
	}
	if got := h.statusMessages("p1"); !reflect.DeepEqual(got, []StatusData{{Remaining: gameSeconds, Ready: true, Round: roundOf(RoundNormal), Rank: optionalScore(1), Gains: []int{}}}) {
		t.Fatalf("Finish status: %v", got)
	}
	if got := h.summary("p1"); got.Score != 0 || !got.Eligible || got.Rank == nil || *got.Rank != 1 {
		t.Fatalf("Finish summary: %+v", got)
	}
	h.clear()
	h.tick(CountdownDuration + 5*time.Second)
	if s := h.state(); s.Phase != Finished || queuedPackets(s) != 0 || s.Elapsed != 0 || len(h.statusMessages("p1")) != 0 {
		t.Fatal("the game advanced after Finish during the countdown")
	}
}

func roundOf(round RoundKind) *RoundKind { return &round }
