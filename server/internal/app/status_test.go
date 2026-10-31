package app

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// playingGame starts a game whose countdown has ended, with the given manual
// players eligible.
func playingGame(t *testing.T, manual ...NodeID) *game {
	t.Helper()
	g := newGame(Simultaneous, func(change) {})
	for _, id := range manual {
		g.setManual(id, true)
		g.nodes[id].ready = true
	}
	if err := g.start(epoch); err != nil {
		t.Fatal(err)
	}
	g.advance(epoch.Add(CountdownDuration))
	return &g
}

// testPacket has visited path, which ends at the node it is queued at, and
// has taken one hop fewer than the path's length.
func testPacket(pid uint16, dst NodeID, path ...NodeID) *packet {
	p := &packet{PacketView: PacketView{pid, dst}, origin: path[0], hops: uint8(len(path) - 1)}
	for _, id := range path {
		p.visited |= 1 << id
	}
	return p
}

func (g *game) place(id NodeID, p *packet) {
	g.nodes[id].fifo = append(g.nodes[id].fifo, p)
}

func (h *harness) place(id NodeID, packets ...*packet) {
	h.t.Helper()
	h.must(h.a.submit(h.ctx, false, func(time.Time) error {
		for _, p := range packets {
			h.a.game.place(id, p)
		}
		h.a.game.schedule(id)
		return nil
	}))
}

func lastStatus(t *testing.T, h *harness, id SessionID) StatusData {
	t.Helper()
	statuses := h.statusMessages(id)
	if len(statuses) == 0 {
		t.Fatalf("no status for %s", id)
	}
	return statuses[len(statuses)-1]
}

func rankValue(rank *int) int {
	if rank == nil {
		return 0
	}
	return *rank
}

// gainsHarness has a manual P1 with an automatic P2 that holds a packet P1
// sent (15 for P1 on arrival) and then one P1 relayed (8 for P1), both due
// within the first game second.
func gainsHarness(t *testing.T) (*harness, string) {
	t.Helper()
	h := newSimultaneousHarness(t)
	h.connect("p1", testUID, 1)
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
	h.start()
	syncID := h.reset("p1").Sync
	h.place(2, testPacket(100, 3, 1, 2), testPacket(101, 3, 4, 1, 2))
	return h, syncID
}

func TestStatusGainsArriveInOrderInOneStatus(t *testing.T) {
	h, syncID := gainsHarness(t)
	h.clear()
	h.tick(time.Second)
	statuses := h.statusMessages("p1")
	if len(statuses) != 1 || !reflect.DeepEqual(statuses[0].Gains, []int{15, 8}) || statuses[0].Score != 23 {
		t.Fatalf("statuses: %+v", statuses)
	}
	line, err := Message{Type: "status", Data: statuses[0]}.MarshalLine()
	h.must(err)
	if depth := jsonDepth(t, line); depth != 3 || !strings.Contains(string(line), `"gains":[15,8]`) {
		t.Fatalf("status JSON depth %d: %s", depth, line)
	}

	// The next status, here for the combo, carries no gains already sent.
	h.clear()
	h.message("p1", IncomingMessage{Type: "send", Sync: syncID, RID: 0, PID: 0, To: 9})
	if got := lastStatus(t, h, "p1"); got.Gains == nil || len(got.Gains) != 0 || got.Combo != 1 || got.Score != 23 {
		t.Fatalf("status after the gains were sent: %+v", got)
	}
}

func TestResyncStatusDropsPendingGains(t *testing.T) {
	h, syncID := gainsHarness(t)
	h.clear()
	// The resync's catch-up delivers both packets before the resync applies.
	h.clock.Store(int64(time.Second))
	h.message("p1", IncomingMessage{Type: "resync"})
	if got := messageTypes(h.messages()); !reflect.DeepEqual(got, []string{"push", "reset", "phase", "status"}) {
		t.Fatalf("resync messages: %v", got)
	}
	if got := lastStatus(t, h, "p1"); len(got.Gains) != 0 || got.Score != 23 {
		t.Fatalf("resync status: %+v", got)
	}
	if h.reset("p1").Sync == syncID {
		t.Fatal("resync kept the synchronization ID")
	}
	syncID = h.reset("p1").Sync
	h.clear()
	h.message("p1", IncomingMessage{Type: "send", Sync: syncID, RID: 0, PID: 0, To: 9})
	if got := lastStatus(t, h, "p1"); len(got.Gains) != 0 || got.Combo != 1 {
		t.Fatalf("dropped gains came back: %+v", got)
	}
}

func TestStatusGainsKeepTheFirstSixteen(t *testing.T) {
	g := playingGame(t, 1)
	for i := range maxGains + 4 {
		g.gain(1, 8+i)
	}
	n := &g.nodes[1]
	if len(n.gains) != maxGains || n.gains[0] != 8 || n.gains[maxGains-1] != 8+maxGains-1 {
		t.Fatalf("gains: %v", n.gains)
	}
	if want := 20*8 + 19*20/2; n.score != want {
		t.Fatalf("score %d, want %d: dropped gains must still score", n.score, want)
	}
}

func TestStatusSentWhenAnotherPlayerChangesTheRank(t *testing.T) {
	h := newSimultaneousHarness(t)
	h.connect("p1", testUID, 1)
	h.connect("p2", "112233AABBCC", 2)
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
	h.execute(Command{Operation: SetNodeEnabled, Node: 2, Enabled: true})
	h.start()
	syncID := h.reset("p2").Sync
	if got := lastStatus(t, h, "p1"); got.Rank != nil || got.Countdown == 0 {
		t.Fatalf("countdown status: %+v", got)
	}
	h.tick(0) // The countdown ends.
	if got := lastStatus(t, h, "p1"); rankValue(got.Rank) != 1 || got.Countdown != 0 {
		t.Fatalf("status after the countdown: %+v", got)
	}
	h.place(2, testPacket(100, 3, 2))
	h.clear()
	h.message("p2", IncomingMessage{Type: "send", Sync: syncID, RID: 0, PID: 100, To: 3})
	statuses := h.statusMessages("p1")
	if len(statuses) != 1 || rankValue(statuses[0].Rank) != 2 || statuses[0].Score != 0 || len(statuses[0].Gains) != 0 {
		t.Fatalf("P1 statuses: %+v", statuses)
	}
	if got := lastStatus(t, h, "p2"); rankValue(got.Rank) != 1 || got.Score != 15 || !reflect.DeepEqual(got.Gains, []int{15}) {
		t.Fatalf("P2 status: %+v", got)
	}
}

func TestStatusRankIsNullForIneligiblePlayers(t *testing.T) {
	h := newSimultaneousHarness(t)
	h.connect("p1", testUID, 1)
	h.connect("p2", "112233AABBCC", 2)
	h.execute(Command{Operation: SetNodeEnabled, Node: 2, Enabled: true})
	h.clear()
	h.message("p1", IncomingMessage{Type: "resync"})
	line, err := Message{Type: "status", Data: lastStatus(t, h, "p1")}.MarshalLine()
	h.must(err)
	if !strings.Contains(string(line), `"rank":null,"gains":[],"combo":0`) {
		t.Fatalf("Preparing status: %s", line)
	}
	h.start()
	h.tick(time.Second)
	if p1, p2 := lastStatus(t, h, "p1"), lastStatus(t, h, "p2"); p1.Rank != nil || rankValue(p2.Rank) != 1 {
		t.Fatalf("Playing ranks: P1 %+v, P2 %+v", p1, p2)
	}
	h.execute(Command{Operation: Finish})
	if p1, p2 := lastStatus(t, h, "p1"), lastStatus(t, h, "p2"); p1.Rank != nil || rankValue(p2.Rank) != rankValue(h.summary("p2").Rank) {
		t.Fatalf("Finished ranks: P1 %+v, P2 %+v", p1, p2)
	}
}

func TestStatusRankTiesAndPhases(t *testing.T) {
	g := newGame(Simultaneous, func(change) {})
	for _, id := range []NodeID{1, 2, 3} {
		g.setManual(id, true)
		g.nodes[id].ready = true
	}
	if r := g.statusRank(1); r != 0 {
		t.Fatalf("Preparing rank %d", r)
	}
	if err := g.start(epoch); err != nil {
		t.Fatal(err)
	}
	if r := g.statusRank(1); r != 0 {
		t.Fatalf("countdown rank %d", r)
	}
	g.advance(epoch.Add(CountdownDuration))
	for id, score := range map[NodeID]int{1: 100, 2: 100, 3: 80, 4: 200} {
		g.nodes[id].score = score
	}
	want := []int{1, 1, 3, 0, 0, 0, 0, 0}
	for id := NodeID(1); id <= PlayerCount; id++ {
		if got := g.statusRank(id); got != want[id-1] {
			t.Fatalf("Playing rank of P%d = %d, want %d", id, got, want[id-1])
		}
	}
	// A provisional rank leaves out a player who lost eligibility.
	g.setManual(1, false)
	if g.statusRank(1) != 0 || g.statusRank(2) != 1 || g.statusRank(3) != 2 {
		t.Fatal("ineligible player kept in the provisional ranks")
	}
	if err := g.finish(FinishManual); err != nil {
		t.Fatal(err)
	}
	if g.statusRank(2) != g.nodes[2].rank || g.statusRank(3) != g.nodes[3].rank || g.statusRank(1) != 0 {
		t.Fatal("Finished rank differs from the settled rank")
	}
}
