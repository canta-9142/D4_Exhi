package app

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"time"
)

const (
	PlayerCount  = 8
	NodeCount    = 11
	GameDuration = 90 * time.Second
	botCapacity  = 4
	packetTTL    = 5
	// maxRoundPackets is the FIFO length that cuts a normal round short, so
	// that the MCU's 1024-packet cache never overflows (server.md 3.2).
	maxRoundPackets = 1000
	maxGains        = 16
	maxCombo        = 999
	// CountdownDuration runs within Playing, from the start of a round (or
	// of the simultaneous game) until its game time begins.
	CountdownDuration = 3 * time.Second
	// SelectDuration, ResultDuration and PracticeIdleDuration are how long a
	// station stays in the mode selection, the result and an idle practice
	// before it goes back to idle (rules.md).
	SelectDuration       = 20 * time.Second
	ResultDuration       = 30 * time.Second
	PracticeIdleDuration = 60 * time.Second
	practiceLength       = 3
	// endless is the clock limit of endless operation: no limit.
	endless = time.Duration(math.MaxInt64)
)

type NodeID uint8
type Phase string

// Phase is both the simultaneous game's phase and, on the wire, the phase of
// one station (player.md).
const (
	Preparing Phase = "Preparing"
	Idle      Phase = "Idle"
	Select    Phase = "Select"
	Playing   Phase = "Playing"
	Finished  Phase = "Finished"
)

// GameMode is how the server runs games, fixed at startup (server.md 3.7).
type GameMode string

const (
	Endless      GameMode = "endless"
	Simultaneous GameMode = "simultaneous"
)

// OperationState is whether endless operation is running (server.md 3.1).
type OperationState string

const (
	OperationStopped OperationState = "stopped"
	OperationRunning OperationState = "running"
)

// Station is the stage of one player node (server.md 3.1).
type Station string

const (
	StationStopped  Station = "stopped"
	StationIdle     Station = "idle"
	StationSelect   Station = "select"
	StationPlaying  Station = "playing"
	StationFinished Station = "finished"
)

// RoundKind is the kind of a round, or "" for none.
type RoundKind string

const (
	RoundNormal   RoundKind = "normal"
	RoundTutorial RoundKind = "tutorial"
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

// Times of a node are on the game clock, like game.elapsed; 0 means none.
type node struct {
	manual         bool
	rate           float64
	fifo           []*packet
	nextGeneration time.Duration
	nextTransfer   time.Duration

	station Station
	round   RoundKind
	// roundStart ends the countdown: the game time of a normal round, or the
	// first practice SEND. roundEnd is when a normal round's game time ends.
	roundStart, roundEnd time.Duration
	// deadline is when the station times out: the end of the mode selection,
	// of a normal round, of the result or of an idle practice.
	deadline time.Duration
	// lastPractice is when the latest practice SEND was accepted.
	lastPractice time.Duration
	// practice is the practice queue (server.md 3.6), and practicePID the
	// next PID to try for it.
	practice    []PacketView
	practicePID uint16

	// score is the current round's score, or the simultaneous game's.
	score int
	// gains are the points not yet sent in a status, at most maxGains.
	gains []int
	// combo counts manual transfers that were not lost. It is shown on the
	// player MCU only and never affects the score.
	combo      int
	topPercent int // 1-100 once a result is settled; 0 for none.
	// bests are the hourly and all-time bests settled with the result.
	bests deviceBests

	// The simultaneous game's selection and ranking (server.md 3.7).
	ready    bool
	eligible bool
	rank     int
}

// practicing reports whether the node shows its practice queue.
func (n *node) practicing() bool {
	return n.station == StationPlaying && n.round == RoundTutorial
}

type change struct {
	kind   string
	node   NodeID
	packet PacketView
	to     NodeID
	// practice is set on a station change that swaps the MCU's queue between
	// the practice queue and the real FIFO.
	practice bool
}

type game struct {
	mode GameMode
	// running is whether endless operation is running.
	running bool
	// phase is the simultaneous game's phase.
	phase Phase
	// origin is when the game clock reads 0: the Start of endless operation,
	// or CountdownDuration after the Start of the simultaneous game.
	origin  time.Time
	elapsed time.Duration
	nodes   [NodeCount + 1]node
	// nextPID is the next PID to try; firstPID is where Start and Stop put
	// it, which tests may change to reach the wraparound.
	nextPID, firstPID uint16
	// used marks the PIDs of the packets in the network.
	used [65536 / 64]uint64
	// random draws generated destinations and practiceRandom practice ones,
	// so that practice never changes the real network.
	random, practiceRandom func(int) int
	notify                 func(change)
	// overflowed lists the nodes whose normal round reached maxRoundPackets.
	overflowed []NodeID
	// countdown is the simultaneous game's ceil(origin - now) in seconds as of
	// the last advance, so that State, which never advances, reports a
	// settled value.
	countdown int
	reason    FinishReason // How the simultaneous game finished, while Finished.
}

func newGame(mode GameMode, notify func(change)) game {
	if mode != Simultaneous {
		mode = Endless
	}
	g := game{mode: mode, phase: Preparing, random: rand.IntN, practiceRandom: rand.IntN, notify: notify}
	for id := NodeID(1); id <= NodeCount; id++ {
		g.nodes[id].rate = 1
		if id.player() {
			g.nodes[id].rate = 2
			g.nodes[id].station = g.restingStation()
		}
	}
	return g
}

// restingStation is the station of a player outside any round: stopped while
// endless operation is, and idle otherwise.
func (g *game) restingStation() Station {
	if g.mode == Endless && !g.running {
		return StationStopped
	}
	return StationIdle
}

// clockRunning reports whether the network runs: while endless operation
// runs, or while the simultaneous game is Playing.
func (g *game) clockRunning() bool {
	if g.mode == Endless {
		return g.running
	}
	return g.phase == Playing
}

// locked reports whether operation modes, assignments and previews are
// frozen: while the simultaneous game is Playing (server.md 3.7.1).
func (g *game) locked() bool {
	return g.mode == Simultaneous && g.phase == Playing
}

// limit is where the game clock stops.
func (g *game) limit() time.Duration {
	if g.mode == Endless {
		return endless
	}
	return GameDuration
}

func (g *game) startBlockers() []StartBlocker {
	reasons := make([]StartBlocker, 0)
	if g.mode == Endless {
		if g.running {
			reasons = append(reasons, StartBlocker{Code: CodeNotStopped})
		}
		return reasons
	}
	if g.phase != Preparing {
		reasons = append(reasons, StartBlocker{Code: CodeNotPreparing})
	}
	var nodes []NodeID
	for id := NodeID(1); id <= PlayerCount; id++ {
		if n := &g.nodes[id]; n.manual && !n.ready {
			nodes = append(nodes, id)
		}
	}
	if len(nodes) > 0 {
		reasons = append(reasons, StartBlocker{Code: CodePlayersNotReady, Nodes: nodes})
	}
	return reasons
}

func (g *game) startError() error {
	if blockers := g.startBlockers(); len(blockers) > 0 {
		return blockers[0].reject()
	}
	return nil
}

// clear empties the network and every round, keeping the operation modes,
// the rates and the simultaneous game's selections.
func (g *game) clear() {
	g.elapsed, g.countdown, g.reason = 0, 0, ""
	for id := NodeID(1); id <= NodeCount; id++ {
		n := &g.nodes[id]
		n.fifo = nil
		n.nextGeneration, n.nextTransfer = 0, 0
		n.roundStart, n.roundEnd, n.deadline, n.lastPractice = 0, 0, 0, 0
		n.practice, n.practicePID = nil, 0
		n.score, n.gains, n.combo, n.topPercent, n.bests = 0, nil, 0, 0, deviceBests{}
		n.eligible, n.rank = false, 0
	}
	// Stale requests are rejected by sync generation and session, not by PID.
	g.used = [len(g.used)]uint64{}
	g.nextPID = g.firstPID
	g.overflowed = nil
}

// start begins endless operation, or the simultaneous game's Playing.
func (g *game) start(now time.Time) error {
	if err := g.startError(); err != nil {
		return err
	}
	g.clear()
	if g.mode == Endless {
		g.running, g.origin = true, now
	} else {
		g.phase, g.origin = Playing, now.Add(CountdownDuration)
		g.countdown = seconds(CountdownDuration)
	}
	for id := NodeID(1); id <= PlayerCount; id++ {
		n := &g.nodes[id]
		n.nextGeneration = time.Second
		if g.mode == Endless {
			n.station, n.round = StationIdle, ""
			continue
		}
		// Automatic players play the game as normal rounds (server.md 3.7.3).
		if !n.manual || n.round == "" {
			n.round = RoundNormal
		}
		n.station, n.roundEnd = StationPlaying, GameDuration
		n.eligible = n.manual && n.round == RoundNormal
		if n.round == RoundTutorial {
			g.refillPractice(id)
		}
	}
	g.notify(change{kind: "reset"})
	return nil
}

// stop ends endless operation, cutting every round short.
func (g *game) stop() error {
	if g.mode != Endless || !g.running {
		return reject(CodePhaseConflict, "Stop requires running endless operation")
	}
	g.running = false
	g.clear()
	for id := NodeID(1); id <= PlayerCount; id++ {
		g.nodes[id].station, g.nodes[id].round = StationStopped, ""
	}
	g.notify(change{kind: "reset"})
	return nil
}

// rankOf follows rules.md: one more than the eligible players scoring higher.
func (g *game) rankOf(id NodeID) int {
	rank := 1
	for other := NodeID(1); other <= PlayerCount; other++ {
		if g.nodes[other].eligible && g.nodes[other].score > g.nodes[id].score {
			rank++
		}
	}
	return rank
}

// statusRank is the simultaneous game's provisional rank while the game time
// runs, the settled rank once Finished, and 0 for none.
func (g *game) statusRank(id NodeID) int {
	switch {
	case g.mode != Simultaneous || !g.nodes[id].eligible:
		return 0
	case g.phase == Finished:
		return g.nodes[id].rank
	case g.phase == Playing && g.countdown == 0:
		return g.rankOf(id)
	}
	return 0
}

// finish settles the simultaneous game. The record is published before the
// stations so that it is saved even if their notifications stop app.
func (g *game) finish(reason FinishReason) error {
	if g.mode != Simultaneous || g.phase != Playing {
		return reject(CodePhaseConflict, "Finish requires the simultaneous game's Playing phase")
	}
	for id := NodeID(1); id <= PlayerCount; id++ {
		n := &g.nodes[id]
		if n.eligible {
			n.rank = g.rankOf(id)
		}
	}
	g.phase, g.countdown, g.reason = Finished, 0, reason
	g.notify(change{kind: "finished"})
	for id := NodeID(1); id <= PlayerCount; id++ {
		n := &g.nodes[id]
		practice := n.practicing()
		n.station, n.practice, n.gains = StationFinished, nil, nil
		g.notify(change{kind: "station", node: id, practice: practice})
	}
	g.notify(change{kind: "phase"})
	return nil
}

// reset brings the simultaneous game back to Preparing.
func (g *game) reset() error {
	if g.mode != Simultaneous {
		return reject(CodePhaseConflict, "Reset requires the simultaneous game")
	}
	g.phase = Preparing
	g.clear()
	for id := NodeID(1); id <= PlayerCount; id++ {
		n := &g.nodes[id]
		n.station, n.round, n.ready = StationIdle, "", false
	}
	g.notify(change{kind: "reset"})
	g.notify(change{kind: "phase"})
	return nil
}

// seconds rounds a duration up to whole seconds, and is 0 if it is not positive.
func seconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int((d + time.Second - 1) / time.Second)
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

// automatic reports whether app transfers the node's FIFO: every node but a
// manual player in a normal round (server.md 3.2).
func (g *game) automatic(id NodeID) bool {
	n := &g.nodes[id]
	return !(id.player() && n.manual && n.station == StationPlaying && n.round == RoundNormal)
}

func (g *game) schedule(id NodeID) {
	n := &g.nodes[id]
	n.nextTransfer = 0
	if g.clockRunning() && g.automatic(id) && len(n.fifo) > 0 {
		wait := interval(n.rate)
		// Deadlines beyond the clock's limit are irrelevant; avoid overflow.
		if limit := g.limit(); wait >= limit-g.elapsed {
			n.nextTransfer = limit
		} else {
			n.nextTransfer = g.elapsed + wait
		}
	}
}

// rescheduleIf schedules the node's transfer again if whether app transfers
// it automatically has changed from wasAutomatic: transfer stops for a normal
// round and resumes from now when it ends.
func (g *game) rescheduleIf(id NodeID, wasAutomatic bool) {
	if g.automatic(id) != wasAutomatic {
		g.schedule(id)
	}
}

func (g *game) setManual(id NodeID, manual bool) {
	n := &g.nodes[id]
	if n.manual == manual {
		return
	}
	wasAutomatic := g.automatic(id)
	n.manual = manual
	if !manual {
		n.combo = 0
	}
	switch {
	case g.mode == Endless:
		// Switching to automatic cuts the round short; app synchronizes the
		// MCU from its config, so the change itself sends nothing.
		if !manual {
			g.abort(id, false)
		}
	case g.phase == Preparing:
		n.ready = false
		g.enter(id, StationIdle, false)
	case g.phase == Playing:
		n.eligible = false
	default:
		n.ready = false
	}
	// Compared with the mode before the change: enter only sees the new mode,
	// so it cannot tell that transfer must resume.
	g.rescheduleIf(id, wasAutomatic)
}

// enter moves a player to idle or the mode selection, dropping its round
// and result. Unless quiet, it notifies the station change.
func (g *game) enter(id NodeID, station Station, notify bool) {
	n := &g.nodes[id]
	wasAutomatic, practice := g.automatic(id), n.practicing()
	n.station, n.round = station, ""
	n.roundStart, n.roundEnd, n.deadline, n.lastPractice = 0, 0, 0, 0
	n.practice = nil
	n.score, n.gains, n.combo, n.topPercent, n.bests = 0, nil, 0, 0, deviceBests{}
	if station == StationSelect && g.mode == Endless {
		n.deadline = g.elapsed + SelectDuration
	}
	g.rescheduleIf(id, wasAutomatic)
	if notify {
		g.notify(change{kind: "station", node: id, practice: practice})
	}
}

// abort cuts a round, mode selection or result short and returns the node to
// idle without a record (server.md 3.1). It does nothing to an idle node.
func (g *game) abort(id NodeID, notify bool) {
	if g.mode != Endless || !g.running || g.nodes[id].station == StationIdle {
		return
	}
	g.enter(id, StationIdle, notify)
}

// ready accepts a long press of a manual player (server.md 4.7, 3.7.2).
func (g *game) ready(id NodeID) {
	n := &g.nodes[id]
	if !n.manual {
		return
	}
	if g.mode == Simultaneous {
		if g.phase == Preparing && n.station == StationIdle && !n.ready {
			n.station = StationSelect
			g.notify(change{kind: "station", node: id})
		}
		return
	}
	if g.running && (n.station == StationIdle || n.station == StationFinished || n.practicing()) {
		g.enter(id, StationSelect, true)
	}
}

// choose accepts the mode selection of a manual player (server.md 4.8, 3.7.2).
func (g *game) choose(id NodeID, round RoundKind) {
	n := &g.nodes[id]
	if !n.manual || n.station != StationSelect {
		return
	}
	if g.mode == Simultaneous {
		if g.phase == Preparing {
			n.station, n.round, n.ready = StationIdle, round, true
			g.notify(change{kind: "station", node: id})
		}
		return
	}
	if !g.running {
		return
	}
	wasAutomatic := g.automatic(id)
	n.station, n.round = StationPlaying, round
	n.roundStart = g.elapsed + CountdownDuration
	n.score, n.gains, n.combo = 0, nil, 0
	if round == RoundNormal {
		n.roundEnd = n.roundStart + GameDuration
		n.deadline = n.roundEnd
	} else {
		n.roundEnd = 0
		n.deadline = n.roundStart + PracticeIdleDuration
		n.practicePID = 0
		g.refillPractice(id)
	}
	g.rescheduleIf(id, wasAutomatic)
	g.notify(change{kind: "station", node: id, practice: round == RoundTutorial})
}

// returnToBot cuts the node's round, mode selection or result short and
// keeps its operation mode (server.md 4.2).
func (g *game) returnToBot(id NodeID) error {
	if g.mode != Endless || !g.running {
		return reject(CodePhaseConflict, "ReturnToBot requires running endless operation")
	}
	g.abort(id, true)
	return nil
}

// expire handles the node's station deadline (server.md 4.3).
func (g *game) expire(id NodeID) {
	n := &g.nodes[id]
	if n.station != StationPlaying || n.round != RoundNormal {
		g.enter(id, StationIdle, true)
		return
	}
	wasAutomatic := g.automatic(id)
	n.station, n.deadline, n.gains = StationFinished, g.elapsed+ResultDuration, nil
	g.rescheduleIf(id, wasAutomatic)
	g.notify(change{kind: "finished", node: id})
	g.notify(change{kind: "station", node: id})
}

// countsPoints reports whether points of the node count: during the game time
// of a normal round, which the simultaneous game's normal and automatic
// players play from the clock's origin.
func (g *game) countsPoints(id NodeID) bool {
	n := &g.nodes[id]
	return n.station == StationPlaying && n.round == RoundNormal && g.elapsed >= n.roundStart
}

// advance is shared by Tick, commands and MCU events. State never calls it.
func (g *game) advance(now time.Time) {
	if !g.clockRunning() {
		return
	}
	if g.mode == Simultaneous {
		// Until the origin, the countdown runs and nothing is due: until stays 0.
		g.countdown = seconds(g.origin.Sub(now))
	}
	limit := g.limit()
	until := min(max(now.Sub(g.origin), g.elapsed), limit)
	for {
		deadline := limit
		var selected NodeID
		kind := ""
		// Strictly earlier selection preserves the tie order: stations,
		// generation, bots, then automatic players, each in ascending node ID
		// order.
		for id := NodeID(1); id <= PlayerCount; id++ {
			if at := g.nodes[id].deadline; at > 0 && at < deadline {
				deadline, selected, kind = at, id, "station"
			}
		}
		for id := NodeID(1); id <= PlayerCount; id++ {
			if at := g.nodes[id].nextGeneration; at > 0 && at < deadline {
				deadline, selected, kind = at, id, "generate"
			}
		}
		for _, ids := range [][2]NodeID{{9, 11}, {1, 8}} {
			for id := ids[0]; id <= ids[1]; id++ {
				if at := g.nodes[id].nextTransfer; at > 0 && at < deadline {
					deadline, selected, kind = at, id, "transfer"
				}
			}
		}
		if selected == 0 || deadline > until {
			break
		}
		g.elapsed = deadline
		switch kind {
		case "station":
			g.expire(selected)
		case "generate":
			dst := NodeID(g.random(PlayerCount-1) + 1)
			if dst >= selected {
				dst++
			}
			p := &packet{PacketView: PacketView{g.allocatePID(), dst}, origin: selected, visited: 1 << selected}
			g.nodes[selected].nextGeneration += time.Second
			g.append(selected, p)
		case "transfer":
			p := g.nodes[selected].fifo[0]
			for _, c := range g.transfer(selected, routes[selected][p.Destination][0], false) {
				g.notify(c)
			}
		}
		g.abortOverflowed()
	}
	g.elapsed = until
	if g.mode == Simultaneous && until == GameDuration {
		_ = g.finish(FinishTimeout)
	}
}

// allocatePID issues the next PID that no packet in the network uses
// (server.md 3.2). Far fewer packets exist than PIDs, so one is always free.
func (g *game) allocatePID() uint16 {
	for {
		pid := g.nextPID
		g.nextPID++
		if g.used[pid/64]&(1<<(pid%64)) == 0 {
			g.used[pid/64] |= 1 << (pid % 64)
			return pid
		}
	}
}

func (g *game) release(pid uint16) {
	g.used[pid/64] &^= 1 << (pid % 64)
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
	if g.mode == Endless && len(n.fifo) >= maxRoundPackets && !g.automatic(id) {
		g.overflowed = append(g.overflowed, id)
	}
	return change{kind: "push", node: id, packet: p.PacketView}
}

// abortOverflowed cuts short the normal rounds whose FIFO reached
// maxRoundPackets, after the packets' own notifications.
func (g *game) abortOverflowed() {
	for len(g.overflowed) > 0 {
		id := g.overflowed[0]
		g.overflowed = g.overflowed[1:]
		g.abort(id, true)
	}
}

// accepting reports whether a manual player's SEND may be accepted now: in a
// round of either kind, after its countdown.
func (g *game) accepting(id NodeID, round RoundKind) bool {
	n := &g.nodes[id]
	if !id.player() || !n.manual || n.station != StationPlaying || n.round != round || !g.clockRunning() {
		return false
	}
	if g.mode == Simultaneous {
		return g.countdown == 0
	}
	return g.elapsed >= n.roundStart
}

func (g *game) canTransfer(from NodeID, pid uint16, to NodeID) bool {
	if !g.accepting(from, RoundNormal) {
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
	// Only the hop just taken decides the combo, so that later hops by other
	// nodes cannot break it.
	if manual {
		if lost {
			n.combo = 0
		} else if n.combo < maxCombo {
			n.combo++
		}
	}
	switch {
	case lost:
		g.release(p.PID)
	case to == p.Destination:
		g.release(p.PID)
		for id := NodeID(1); id <= PlayerCount; id++ {
			if id == p.origin {
				if p.hops <= 2 {
					g.gain(id, 15)
				} else {
					g.gain(id, 10)
				}
			} else if p.visited&(1<<id) != 0 {
				g.gain(id, 8)
			}
		}
	default:
		p.visited |= 1 << to
		changes = append(changes, g.enqueue(to, p))
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

// gain adds points to a player whose points count, and keeps them for the
// next status; status gains are for display, so those beyond maxGains are
// dropped. Points of any other node are discarded (rules.md).
func (g *game) gain(id NodeID, points int) {
	if !g.countsPoints(id) {
		return
	}
	n := &g.nodes[id]
	n.score += points
	if len(n.gains) < maxGains {
		n.gains = append(n.gains, points)
	}
}

// refillPractice fills the practice queue up to practiceLength, with
// destinations drawn uniformly from the other players, and returns what it
// added. PIDs count up per round, skipping those in the queue (server.md 3.6).
func (g *game) refillPractice(id NodeID) []PacketView {
	n := &g.nodes[id]
	start := len(n.practice)
	for len(n.practice) < practiceLength {
		pid := n.practicePID
		n.practicePID++
		if slices.ContainsFunc(n.practice, func(p PacketView) bool { return p.PID == pid }) {
			continue
		}
		dst := NodeID(g.practiceRandom(PlayerCount-1) + 1)
		if dst >= id {
			dst++
		}
		n.practice = append(n.practice, PacketView{pid, dst})
	}
	return slices.Clone(n.practice[start:])
}

func (g *game) canPractice(from NodeID, pid uint16, to NodeID) bool {
	if !g.accepting(from, RoundTutorial) {
		return false
	}
	q := g.nodes[from].practice
	return len(q) > 0 && q[0].PID == pid && slices.Contains(neighbors[from], to)
}

// practiceSend consumes the head of the practice queue and returns the packet
// that replaces it. It touches nothing else of the network (server.md 3.6).
func (g *game) practiceSend(id NodeID) PacketView {
	n := &g.nodes[id]
	n.practice = n.practice[1:]
	n.lastPractice = g.elapsed
	if g.mode == Endless {
		n.deadline = g.elapsed + PracticeIdleDuration
	}
	return g.refillPractice(id)[0]
}

// botUsage is the FIFO usage of B1-B3, as sent in BUFFER.
func (g *game) botUsage() [3]int {
	return [3]int{len(g.nodes[9].fifo), len(g.nodes[10].fifo), len(g.nodes[11].fifo)}
}
