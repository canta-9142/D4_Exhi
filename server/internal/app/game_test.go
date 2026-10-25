package app

import (
	"math"
	"reflect"
	"testing"
	"time"
)

var epoch = time.Unix(1000, 0)

func startedGame(t *testing.T) *game {
	t.Helper()
	g := newGame(func(change) {})
	g.random = func(n int) int { return n / 2 }
	if err := g.start(epoch); err != nil {
		t.Fatal(err)
	}
	return &g
}

func TestDelayedTicksMatchOnTimeTicks(t *testing.T) {
	delayed, regular := startedGame(t), startedGame(t)
	delayed.advance(epoch.Add(130 * time.Second))
	for at := 100 * time.Millisecond; at <= 130*time.Second; at += 100 * time.Millisecond {
		regular.advance(epoch.Add(at))
	}
	if delayed.phase != Finished || delayed.elapsed != GameDuration || delayed.nextPID != 8*119 {
		t.Fatalf("wrong game boundary: phase=%s elapsed=%s generated=%d", delayed.phase, delayed.elapsed, delayed.nextPID)
	}
	if !reflect.DeepEqual(delayed.nodes, regular.nodes) {
		t.Fatal("delayed Tick changed the simulation result")
	}
	score := delayed.nodes[1].score
	delayed.advance(epoch.Add(200 * time.Second))
	if delayed.nodes[1].score != score {
		t.Fatal("score changed after Finish")
	}
}

func TestSchedulingAndSimultaneousDeadlines(t *testing.T) {
	g := startedGame(t)
	var changes []change
	g.notify = func(c change) { changes = append(changes, c) }
	g.advance(epoch.Add(time.Second))
	if len(changes) != PlayerCount {
		t.Fatalf("generation notifications: %v", changes)
	}
	for i, c := range changes {
		if c.kind != "push" || c.node != NodeID(i+1) {
			t.Fatalf("generation order: %v", changes)
		}
	}
	if g.nodes[1].nextTransfer != 1500*time.Millisecond {
		t.Fatal("packet transferred before its arrival + service time")
	}

	// Arrange a bot and a player deadline at the next generation time.
	g.nodes[1].nextTransfer = 2 * time.Second
	g.nodes[9].fifo = []*packet{{PacketView: PacketView{1000, 3}, origin: 1, visited: 1<<1 | 1<<9}}
	g.nodes[9].nextTransfer = 2 * time.Second
	changes = nil
	g.advance(epoch.Add(2 * time.Second))
	var atTwo []change
	// Other deadlines at 1.5s may precede the 2s group.
	start := -1
	for i, c := range changes {
		if c.kind == "push" && c.node == 1 {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatal("missing second generation")
	}
	atTwo = changes[start:]
	for i := 0; i < PlayerCount; i++ {
		if atTwo[i].kind != "push" || atTwo[i].node != NodeID(i+1) {
			t.Fatalf("generation did not precede transfer: %v", atTwo)
		}
	}
	botIndex, playerIndex := -1, -1
	for i, c := range atTwo {
		if c.kind == "fifo" && c.node == 9 {
			botIndex = i
		}
		if c.kind == "fifo" && c.node == 1 {
			playerIndex = i
		}
	}
	if botIndex < 0 || playerIndex <= botIndex {
		t.Fatalf("bot/player tie order: %v", atTwo)
	}
}

func TestDeliveryAndLoss(t *testing.T) {
	for _, tc := range []struct {
		name        string
		path        []NodeID
		dst         NodeID
		fullBot     bool
		originScore int
		relayScore  int
	}{
		{"two hops", []NodeID{1, 2, 3}, 3, false, 15, 8},
		{"three hops", []NodeID{1, 2, 3, 4}, 4, false, 10, 8},
		{"delivery on fifth hop", []NodeID{1, 2, 3, 4, 5, 6}, 6, false, 10, 8},
		{"TTL loss", []NodeID{1, 2, 3, 4, 5, 6}, 7, false, 0, 0},
		{"loop to source", []NodeID{1, 2, 1}, 3, false, 0, 0},
		{"bot congestion", []NodeID{1, 9}, 3, true, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := startedGame(t)
			p := &packet{PacketView: PacketView{0, tc.dst}, origin: 1, visited: 1 << 1}
			g.append(1, p)
			if tc.fullBot {
				g.nodes[9].fifo = make([]*packet, botCapacity)
			}
			for i := 1; i < len(tc.path); i++ {
				g.transfer(tc.path[i-1], tc.path[i], true)
			}
			if g.nodes[1].score != tc.originScore || g.nodes[2].score != tc.relayScore {
				t.Fatalf("scores: origin=%d relay=%d", g.nodes[1].score, g.nodes[2].score)
			}
			for id := NodeID(1); id <= NodeCount; id++ {
				for _, queued := range g.nodes[id].fifo {
					if queued == p {
						t.Fatal("completed/lost packet remains queued")
					}
				}
			}
		})
	}
}

func TestResetRetainsSettingsAndPID(t *testing.T) {
	g := startedGame(t)
	g.setManual(1, true)
	g.nodes[9].rate = 3
	g.advance(epoch.Add(time.Second))
	nextPID := g.nextPID
	g.reset()
	if g.nextPID != nextPID || !g.nodes[1].manual || g.nodes[9].rate != 3 {
		t.Fatal("Reset discarded retained state")
	}
	for id := NodeID(1); id <= NodeCount; id++ {
		n := &g.nodes[id]
		if len(n.fifo) != 0 || n.score != 0 || n.nextTransfer != 0 || n.nextGeneration != 0 {
			t.Fatal("Reset left game state")
		}
	}
	g.nodes[1].ready = true
	if err := g.start(epoch); err != nil {
		t.Fatal(err)
	}
	g.advance(epoch.Add(time.Second))
	if g.nodes[1].fifo[0].PID != uint16(nextPID) {
		t.Fatal("PID was reused")
	}
	_ = g.finish()
	g.reset()
	g.nextPID = 65536 - maxGamePackets + 1
	if err := g.start(epoch); err == nil {
		t.Fatal("Start accepted insufficient PID capacity")
	}
	g.nextPID = 65536 - maxGamePackets
	g.nodes[1].ready = true
	if err := g.start(epoch); err != nil {
		t.Fatalf("exactly 1000 free PIDs rejected: %v", err)
	}
}

func TestRankingCannotBeRestoredDuringGame(t *testing.T) {
	g := startedGame(t)
	g.reset()
	g.setManual(1, true)
	g.nodes[1].ready = true
	if err := g.start(epoch); err != nil {
		t.Fatal(err)
	}
	if !g.nodes[1].eligible || g.nodes[2].eligible {
		t.Fatal("wrong initial eligibility")
	}
	g.setManual(1, false)
	g.setManual(1, true)
	if g.nodes[1].eligible {
		t.Fatal("manual control restored ranking eligibility")
	}
	_ = g.finish()
	g.setManual(2, true)
	if g.nodes[2].eligible {
		t.Fatal("Finished ranking changed")
	}
}

func TestFixedRouting(t *testing.T) {
	for from := NodeID(1); from <= NodeCount; from++ {
		for dst := NodeID(1); dst <= PlayerCount; dst++ {
			if from == dst {
				continue
			}
			var visited uint16
			id := from
			for hops := 0; id != dst; hops++ {
				if visited&(1<<id) != 0 || hops >= packetTTL {
					t.Fatalf("invalid route %d -> %d", from, dst)
				}
				visited |= 1 << id
				id = routes[id][dst][0]
			}
		}
	}
	if routes[1][4][0] != 9 {
		t.Fatal("route should take the shortest path")
	}
	if routes[1][3][0] != 2 {
		t.Fatal("equal distances should use ascending node ID")
	}
}

func TestRateExtremesDoNotOverflow(t *testing.T) {
	g := startedGame(t)
	g.elapsed = time.Second
	g.nodes[9].rate = math.SmallestNonzeroFloat64
	g.append(9, &packet{})
	if g.nodes[9].nextTransfer != GameDuration {
		t.Fatal("slow rate overflowed its deadline")
	}
	if interval(math.MaxFloat64) != time.Nanosecond {
		t.Fatal("fast rate lost forward progress")
	}
}
