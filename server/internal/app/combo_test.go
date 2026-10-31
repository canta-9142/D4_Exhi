package app

import (
	"slices"
	"testing"
	"time"
)

func TestComboCountsManualTransfersThatAreNotLost(t *testing.T) {
	full := make([]*packet, botCapacity)
	for _, tc := range []struct {
		name   string
		packet *packet
		to     NodeID
		bot    []*packet
		before int
		want   int
	}{
		{"queued at the next node", testPacket(100, 3, 1), 2, nil, 4, 5},
		{"arrived", testPacket(100, 2, 1), 2, nil, 4, 5},
		{"reaches the maximum", testPacket(100, 2, 1), 2, nil, maxCombo - 1, maxCombo},
		{"stops at the maximum", testPacket(100, 2, 1), 2, nil, maxCombo, maxCombo},
		{"congestion", testPacket(100, 3, 1), 9, full, 4, 0},
		{"timeout", testPacket(100, 3, 4, 5, 6, 7, 1), 2, nil, 4, 0},
		{"loop", testPacket(100, 3, 2, 1), 2, nil, 4, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := playingGame(t, 1)
			g.place(1, tc.packet)
			g.nodes[9].fifo = slices.Clone(tc.bot)
			g.nodes[1].combo = tc.before
			g.transfer(1, tc.to, true)
			if g.nodes[1].combo != tc.want {
				t.Fatalf("combo %d, want %d", g.nodes[1].combo, tc.want)
			}
		})
	}
}

func TestComboIgnoresLaterHopsAndAutomaticTransfers(t *testing.T) {
	g := playingGame(t, 1)
	g.place(1, testPacket(100, 3, 1))
	g.transfer(1, 2, true)
	// P2 is automatic; its transfer into a full bot loses P1's packet.
	g.nodes[9].fifo = make([]*packet, botCapacity)
	g.transfer(2, 9, false)
	if g.nodes[1].combo != 1 || g.nodes[2].combo != 0 {
		t.Fatalf("combos after a later loss: P1 %d, P2 %d", g.nodes[1].combo, g.nodes[2].combo)
	}
	g.place(2, testPacket(101, 1, 2))
	g.transfer(2, 1, false)
	if g.nodes[2].combo != 0 || g.nodes[2].score != 15 {
		t.Fatalf("automatic transfer counted: combo %d, score %d", g.nodes[2].combo, g.nodes[2].score)
	}
}

func TestComboResets(t *testing.T) {
	g := playingGame(t, 1, 2)
	g.nodes[1].combo, g.nodes[2].combo = 7, 7
	g.setManual(1, false)
	g.setManual(2, true) // No change: P2 is already manual.
	if g.nodes[1].combo != 0 || g.nodes[2].combo != 7 {
		t.Fatalf("switch to automatic: P1 %d, P2 %d", g.nodes[1].combo, g.nodes[2].combo)
	}
	if err := g.finish(FinishManual); err != nil {
		t.Fatal(err)
	}
	if g.nodes[2].combo != 7 {
		t.Fatal("Finish reset the combo")
	}
	g.reset()
	if g.nodes[2].combo != 0 {
		t.Fatal("Reset kept the combo")
	}
	g.nodes[2].combo = 7 // Start must clear a combo even if one were left.
	g.nodes[2].ready = true
	if err := g.start(epoch); err != nil {
		t.Fatal(err)
	}
	if g.nodes[2].combo != 0 {
		t.Fatal("Start kept the combo")
	}
}

func TestComboChangeSendsStatusWithoutScoring(t *testing.T) {
	h := newSimultaneousHarness(t)
	h.connect("p1", testUID, 1)
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
	h.start()
	syncID := h.reset("p1").Sync
	h.tick(0)
	h.place(1, testPacket(100, 3, 1), testPacket(101, 3, 1), testPacket(102, 3, 1))
	h.place(9, make([]*packet, botCapacity)...)
	for i, tc := range []struct {
		pid   uint16
		to    NodeID
		combo int
	}{{100, 2, 1}, {101, 9, 0}, {102, 2, 1}} {
		h.clear()
		h.message("p1", IncomingMessage{Type: "send", Sync: syncID, RID: uint32(i), PID: tc.pid, To: tc.to})
		statuses := h.statusMessages("p1")
		if len(statuses) != 1 || statuses[0].Combo != tc.combo || statuses[0].Score != 0 || len(statuses[0].Gains) != 0 {
			t.Fatalf("SEND %d statuses: %+v", tc.pid, statuses)
		}
	}
	// A rejected SEND keeps the combo, so no status follows its RESET.
	h.clear()
	h.message("p1", IncomingMessage{Type: "send", Sync: syncID, RID: 3, PID: 100, To: 2})
	if statuses := h.statusMessages("p1"); len(statuses) != 0 {
		t.Fatalf("rejected SEND sent statuses: %+v", statuses)
	}
	// Switching to automatic, here by disconnecting, clears it.
	h.must(h.a.Disconnected(h.ctx, "p1"))
	h.must(h.a.submit(h.ctx, false, func(time.Time) error {
		if h.a.game.nodes[1].combo != 0 {
			t.Error("disconnect kept the combo")
		}
		return nil
	}))
}
