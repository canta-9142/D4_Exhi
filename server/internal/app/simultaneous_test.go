package app

import (
	"reflect"
	"testing"
	"time"
)

// A player of the simultaneous game who chose the tutorial practices with
// the practice queue until the game ends, unranked and without a result
// (server.md 3.7.3, 3.7.4).
func TestSimultaneousPracticeEndsWithTheGame(t *testing.T) {
	saver := &gameSaver{}
	h := newHarnessWithOptions(t, Options{Mode: Simultaneous, SaveGame: saver.save})
	for _, id := range []NodeID{1, 2} {
		h.connect(playerSession(id), playerUID(id), id)
		h.execute(Command{Operation: SetNodeEnabled, Node: id, Enabled: true})
	}
	h.press("P1")
	h.choose("P1", RoundTutorial)
	if n := h.node(1); !n.Ready || n.Round != RoundTutorial || n.Station != StationIdle || n.WindowSeconds != 0 {
		t.Fatalf("tutorial choice: %+v", n)
	}
	h.clear()
	h.start() // P2 chooses a normal game.
	if got := h.typesFor("P1"); !reflect.DeepEqual(got, []string{"phase", "reset", "status"}) || !reflect.DeepEqual(h.phasesFor("P1"), []Phase{Playing}) {
		t.Fatalf("P1 at Start: %v", got)
	}
	if got := h.reset("P1").Packets; len(got) != practiceLength {
		t.Fatalf("practice queue at Start: %v", got)
	}
	if s := h.state(); s.Nodes[0].RankingEligible || !s.Nodes[1].RankingEligible {
		t.Fatalf("eligibility: %+v %+v", s.Nodes[0], s.Nodes[1])
	}
	h.tick(10 * time.Second)
	status := lastStatus(t, h, "P1")
	if status.Round == nil || *status.Round != RoundTutorial || status.Remaining != gameSeconds-10 || status.Window != 0 || status.Score != 0 || status.Bots == nil || status.Rank != nil {
		t.Fatalf("practice status: %+v", status)
	}
	if r := h.send("P1", 0, 2); !r.OK {
		t.Fatal("practice SEND refused")
	}
	// There is no idle timeout; the practice ends with the game.
	h.tick(80 * time.Second)
	if n := h.node(1); n.Station != StationPlaying || n.Score != 0 {
		t.Fatalf("practice before the end: %+v", n)
	}
	h.clear()
	h.tick(GameDuration + 1)
	if got := h.typesFor("P1"); !reflect.DeepEqual(got, []string{"phase", "status", "reset"}) || !reflect.DeepEqual(h.phasesFor("P1"), []Phase{Finished}) {
		t.Fatalf("P1 at the end: %v", got)
	}
	if got := h.statusMessages("P1")[0]; got.Round == nil || *got.Round != RoundTutorial || got.Bots != nil {
		t.Fatalf("P1 final status: %+v", got)
	}
	if got := h.typesFor("P2"); !reflect.DeepEqual(got[len(got)-3:], []string{"phase", "status", "summary"}) {
		t.Fatalf("P2 at the end: %v", got)
	}
	saved := saver.saved()
	if len(saved) != 1 || saved[0].Kind != RecordGame || len(saved[0].Results) != PlayerCount || saved[0].Results[0] != (PlayerResult{Node: 1}) {
		t.Fatalf("saved %+v", saved)
	}
	h.clear()
	h.message("P1", IncomingMessage{Type: "resync"})
	if got := h.typesFor("P1"); !reflect.DeepEqual(got, []string{"reset", "phase", "status"}) {
		t.Fatalf("resync after the practice: %v", got)
	}
}

func TestSimultaneousGameRefusesEndlessCommands(t *testing.T) {
	h := newSimultaneousHarness(t)
	h.connect("P1", playerUID(1), 1)
	for _, op := range []Operation{Stop, ReturnToBot} {
		expectCode(t, h.a.Execute(h.ctx, Command{Operation: op, Node: 1}), CodePhaseConflict)
	}
	if s := h.state(); s.Mode != Simultaneous || s.Operation != OperationRunning || s.Phase != Preparing || s.Nodes[0].Station != StationIdle {
		t.Fatalf("simultaneous snapshot: %+v", s)
	}
	if got := h.phasesFor("P1"); len(got) == 0 || got[len(got)-1] != Idle {
		t.Fatalf("Preparing phase on the wire: %v", got)
	}
}
