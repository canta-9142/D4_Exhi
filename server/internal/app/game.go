package app

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"time"
)

const (
	PlayerCount    = 8
	NodeCount      = 11
	GameDuration   = 120 * time.Second
	botCapacity    = 4
	packetTTL      = 5
	maxGamePackets = 1000
)

type NodeID uint8
type Phase string

const (
	Preparing Phase = "Preparing"
	Playing   Phase = "Playing"
	Finished  Phase = "Finished"
)

func (id NodeID) player() bool { return id >= 1 && id <= PlayerCount }
func (id NodeID) bot() bool    { return id > PlayerCount && id <= NodeCount }

func (id NodeID) wireName() string {
	if id.player() {
		return fmt.Sprintf("P%d", id)
	}
	return fmt.Sprintf("B%d", id-PlayerCount)
}

// The player entries match docs/spec/player.md; bot links are their reverse.
var neighbors = [NodeCount + 1][]NodeID{
	1:  {2, 9, 11, 8},
	2:  {3, 10, 9, 1},
	3:  {4, 10, 11, 2},
	4:  {5, 10, 9, 3},
	5:  {6, 11, 9, 4},
	6:  {7, 9, 10, 5},
	7:  {8, 9, 11, 6},
	8:  {1, 10, 11, 7},
	9:  {1, 2, 4, 5, 6, 7},
	10: {2, 3, 4, 6, 8},
	11: {1, 3, 5, 7, 8},
}

// Fixed priorities: shortest remaining distance, then ascending node ID.
// FIFO usage and previously visited nodes never alter this order.
var routes = func() (table [NodeCount + 1][PlayerCount + 1][]NodeID) {
	for dst := NodeID(1); dst <= PlayerCount; dst++ {
		var distances [NodeCount + 1]int
		for id := NodeID(1); id <= NodeCount; id++ {
			distances[id] = NodeCount
		}
		distances[dst] = 0
		queue := []NodeID{dst}
		for len(queue) > 0 {
			id := queue[0]
			queue = queue[1:]
			for _, next := range neighbors[id] {
				if distances[next] == NodeCount {
					distances[next] = distances[id] + 1
					queue = append(queue, next)
				}
			}
		}
		for id := NodeID(1); id <= NodeCount; id++ {
			table[id][dst] = slices.Clone(neighbors[id])
			slices.SortFunc(table[id][dst], func(a, b NodeID) int {
				if distances[a] != distances[b] {
					return distances[a] - distances[b]
				}
				return int(a) - int(b)
			})
		}
	}
	return
}()

// PacketView is the FIFO representation sent to a player MCU.
type PacketView struct {
	PID         uint16 `json:"pid"`
	Destination NodeID `json:"dst"`
}

type packet struct {
	PacketView
	origin  NodeID
	hops    uint8
	visited uint16
}

type node struct {
	manual         bool
	rate           float64
	fifo           []*packet
	score          int
	eligible       bool
	ready          bool
	nextGeneration time.Duration
	nextTransfer   time.Duration
}

type change struct {
	kind   string
	node   NodeID
	packet PacketView
	to     NodeID
}

type game struct {
	phase   Phase
	started time.Time
	elapsed time.Duration
	nodes   [NodeCount + 1]node
	nextPID uint32
	random  func(int) int
	notify  func(change)
}

func newGame(notify func(change)) game {
	g := game{phase: Preparing, random: rand.IntN, notify: notify}
	for id := NodeID(1); id <= NodeCount; id++ {
		g.nodes[id].rate = 1
		if id.player() {
			g.nodes[id].rate = 2
		}
	}
	return g
}

func (g *game) startBlockReasons() []string {
	reasons := make([]string, 0)
	if g.phase != Preparing {
		reasons = append(reasons, "Start requires Preparing phase")
	}
	if 65536-g.nextPID < maxGamePackets {
		reasons = append(reasons, "not enough packet IDs for another game; restart the server")
	}
	for id := NodeID(1); id <= PlayerCount; id++ {
		if n := &g.nodes[id]; n.manual && !n.ready {
			reasons = append(reasons, fmt.Sprintf("%s is not ready", id.wireName()))
		}
	}
	return reasons
}

func (g *game) start(now time.Time) error {
	if reasons := g.startBlockReasons(); len(reasons) > 0 {
		return fmt.Errorf("%s", strings.Join(reasons, "; "))
	}
	g.phase, g.started, g.elapsed = Playing, now, 0
	for id := NodeID(1); id <= NodeCount; id++ {
		n := &g.nodes[id]
		if id.player() {
			n.eligible = n.manual
			n.nextGeneration = time.Second
		}
	}
	g.notify(change{kind: "phase"})
	return nil
}

func (g *game) finish() error {
	if g.phase != Playing {
		return fmt.Errorf("Finish requires Playing phase")
	}
	g.phase = Finished
	g.notify(change{kind: "phase"})
	return nil
}

func (g *game) reset() {
	g.phase, g.started, g.elapsed = Preparing, time.Time{}, 0
	for id := NodeID(1); id <= NodeCount; id++ {
		n := &g.nodes[id]
		n.fifo = nil
		n.score, n.nextGeneration, n.nextTransfer = 0, 0, 0
		n.eligible = n.manual
		n.ready = false
	}
	g.notify(change{kind: "reset"})
}

func interval(rate float64) time.Duration {
	// Positive rates outside the duration range saturate to its nearest bound.
	ns := float64(time.Second) / rate
	if ns >= float64(math.MaxInt64) {
		return time.Duration(math.MaxInt64)
	}
	if ns < 1 {
		return time.Nanosecond
	}
	return time.Duration(ns)
}

func (g *game) schedule(id NodeID) {
	n := &g.nodes[id]
	n.nextTransfer = 0
	if g.phase == Playing && !n.manual && len(n.fifo) > 0 {
		wait := interval(n.rate)
		// Deadlines beyond the game are irrelevant; avoid duration overflow.
		if wait >= GameDuration-g.elapsed {
			n.nextTransfer = GameDuration
		} else {
			n.nextTransfer = g.elapsed + wait
		}
	}
}

func (g *game) setManual(id NodeID, manual bool) {
	n := &g.nodes[id]
	if n.manual == manual {
		return
	}
	n.manual = manual
	n.ready = false
	if g.phase == Playing && !manual {
		n.eligible = false
	}
	if g.phase == Preparing {
		n.eligible = manual
	}
	g.schedule(id)
}

// advance is shared by Tick, commands and MCU events. State never calls it.
func (g *game) advance(now time.Time) {
	if g.phase != Playing {
		return
	}
	until := min(max(now.Sub(g.started), g.elapsed), GameDuration)
	for {
		deadline := GameDuration
		var selected NodeID
		generate := false
		// Strictly earlier selection preserves the tie order: generation,
		// bots, then automatic players, each in ascending node ID order.
		for id := NodeID(1); id <= PlayerCount; id++ {
			if at := g.nodes[id].nextGeneration; at > 0 && at < deadline {
				deadline, selected, generate = at, id, true
			}
		}
		for _, ids := range [][2]NodeID{{9, 11}, {1, 8}} {
			for id := ids[0]; id <= ids[1]; id++ {
				if at := g.nodes[id].nextTransfer; at > 0 && at < deadline {
					deadline, selected, generate = at, id, false
				}
			}
		}
		if selected == 0 || deadline > until {
			break
		}
		g.elapsed = deadline
		if generate {
			dst := NodeID(g.random(PlayerCount-1) + 1)
			if dst >= selected {
				dst++
			}
			p := &packet{PacketView: PacketView{uint16(g.nextPID), dst}, origin: selected, visited: 1 << selected}
			g.nextPID++
			g.nodes[selected].nextGeneration += time.Second
			g.append(selected, p)
		} else {
			p := g.nodes[selected].fifo[0]
			for _, c := range g.transfer(selected, routes[selected][p.Destination][0], false) {
				g.notify(c)
			}
		}
	}
	g.elapsed = until
	if until == GameDuration {
		_ = g.finish()
	}
}

func (g *game) append(id NodeID, p *packet) {
	g.notify(g.enqueue(id, p))
}

func (g *game) enqueue(id NodeID, p *packet) change {
	n := &g.nodes[id]
	n.fifo = append(n.fifo, p)
	if len(n.fifo) == 1 {
		g.schedule(id)
	}
	return change{kind: "push", node: id, packet: p.PacketView}
}

func (g *game) canTransfer(from NodeID, pid uint16, to NodeID) bool {
	if g.phase != Playing || !from.player() || !g.nodes[from].manual {
		return false
	}
	q := g.nodes[from].fifo
	return len(q) > 0 && q[0].PID == pid && slices.Contains(neighbors[from], to)
}

// A valid transfer consumes the head even if it is subsequently lost. Return
// notifications separately so a manual SEND can publish RESULT before them.
func (g *game) transfer(from, to NodeID, manual bool) []change {
	var changes []change
	n := &g.nodes[from]
	p := n.fifo[0]
	n.fifo[0] = nil
	n.fifo = n.fifo[1:]
	g.schedule(from)
	p.hops++
	lost := p.visited&(1<<to) != 0 || (to.bot() && len(g.nodes[to].fifo) >= botCapacity) || (p.hops >= packetTTL && to != p.Destination)
	if !lost {
		if to == p.Destination {
			for id := NodeID(1); id <= PlayerCount; id++ {
				if id == p.origin {
					if p.hops <= 2 {
						g.nodes[id].score += 15
					} else {
						g.nodes[id].score += 10
					}
				} else if p.visited&(1<<id) != 0 {
					g.nodes[id].score += 8
				}
			}
		} else {
			p.visited |= 1 << to
			changes = append(changes, g.enqueue(to, p))
		}
	}
	if !manual {
		changes = append(changes, change{kind: "fifo", node: from})
	}
	changes = append(changes, change{kind: "link", node: from, to: to, packet: p.PacketView})
	if from.bot() {
		changes = append(changes, change{kind: "buffer", node: from})
	}
	return changes
}
