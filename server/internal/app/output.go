package app

import (
	"crypto/rand"
	"encoding/hex"
	"slices"
	"strings"
)

// remainingSeconds is the simultaneous game's remaining time as MCUs receive
// it, and 0 in endless operation.
func (a *App) remainingSeconds() int {
	if a.game.mode != Simultaneous {
		return 0
	}
	return seconds(GameDuration - a.game.elapsed)
}

// nodeView is what a player MCU and the Snapshot show of one player, from the
// settled state (player.md, web.md 3.2).
type nodeView struct {
	station                                    Station
	round                                      RoundKind
	score, remaining, countdown, window, combo int
	ready                                      bool
	// bots is the bot FIFO usage, shown only while practicing.
	bots    [3]int
	hasBots bool
}

func (g *game) view(id NodeID) nodeView {
	n := &g.nodes[id]
	v := nodeView{station: n.station, round: n.round, score: n.score, combo: n.combo}
	if g.mode == Simultaneous {
		v.ready = n.manual && n.ready
		if g.phase != Preparing {
			v.remaining = seconds(GameDuration - g.elapsed)
		}
		if g.phase == Playing {
			v.countdown = g.countdown
		}
	} else {
		switch n.station {
		case StationSelect, StationFinished:
			v.window = seconds(n.deadline - g.elapsed)
		case StationPlaying:
			v.countdown = seconds(n.roundStart - g.elapsed)
			if n.round == RoundNormal {
				v.remaining = seconds(n.roundEnd - max(g.elapsed, n.roundStart))
			} else {
				// The idle timer starts when the countdown ends.
				v.window = min(seconds(n.deadline-max(g.elapsed, n.roundStart)), seconds(PracticeIdleDuration))
			}
		}
	}
	if n.practicing() {
		v.score, v.combo = 0, 0
		v.bots, v.hasBots = g.botUsage(), true
	}
	return v
}

// wirePhase is the phase sent to the player MCU of node, which is 0 for an
// unassigned board under preview.
func (g *game) wirePhase(node NodeID) Phase {
	station := g.restingStation()
	if node != 0 {
		station = g.nodes[node].station
	} else if g.mode == Simultaneous && g.phase != Preparing {
		return g.phase
	}
	switch station {
	case StationStopped:
		return Preparing
	case StationSelect:
		return Select
	case StationPlaying:
		return Playing
	case StationFinished:
		return Finished
	}
	return Idle
}

// hasSummary reports whether the player MCU of node gets a summary: in a
// result, but not after the simultaneous game's practice (server.md 3.7.4).
func (g *game) hasSummary(node NodeID) bool {
	if node == 0 {
		return false
	}
	n := &g.nodes[node]
	return n.station == StationFinished && n.round != RoundTutorial
}

// statusKey is the part of a status compared with the last one sent. Gains are
// not part of it: any pending gains are sent.
type statusKey struct {
	score, remaining, countdown, rank, combo, window int
	ready                                            bool
	round                                            RoundKind
	bots                                             [3]int
	hasBots                                          bool
}

// status leaves Gains empty; sendStatus takes the pending gains. An
// unassigned board under preview shows zeros.
func (a *App) status(node NodeID) (StatusData, statusKey) {
	var key statusKey
	if node != 0 {
		v := a.game.view(node)
		key = statusKey{score: v.score, remaining: v.remaining, countdown: v.countdown, rank: a.game.statusRank(node),
			combo: v.combo, window: v.window, ready: v.ready, round: v.round, bots: v.bots, hasBots: v.hasBots}
	}
	data := StatusData{Score: key.score, Remaining: key.remaining, Ready: key.ready, Countdown: key.countdown,
		Window: key.window, Gains: []int{}, Combo: key.combo}
	if key.round != "" {
		round := key.round
		data.Round = &round
	}
	if key.rank != 0 {
		rank := key.rank
		data.Rank = &rank
	}
	if key.hasBots {
		data.Bots = slices.Clone(key.bots[:])
	}
	return data, key
}

func (a *App) flushPlayerUpdates() {
	for id := range a.sessions {
		a.flushPlayerSession(id)
	}
}

func (a *App) flushPlayerSession(id SessionID) {
	s := a.sessions[id]
	if s == nil {
		return
	}
	if s.phasePending && !a.phase(id) {
		return
	}
	a.sendStatus(id)
	a.sendSummary(id)
}

func (a *App) sendSummary(id SessionID) {
	s := a.sessions[id]
	if s == nil || s.failed || s.waitingHello || !s.summaryPending {
		return
	}
	node := a.sessionNode(id)
	if !a.game.hasSummary(node) || s.sync == "" || a.previewing(id) {
		s.summaryPending = false
		return
	}
	n := &a.game.nodes[node]
	// A round is the result of one player and has no rank (server.md 3.1).
	data := SummaryData{Sync: s.sync, Score: n.score, Eligible: true}
	if a.game.mode == Simultaneous {
		data.Eligible = n.eligible
		if n.eligible {
			rank := n.rank
			data.Rank = &rank
		}
	}
	if data.Eligible {
		data.TopPercent = a.deviceTopPercent(n)
		data.BottomHalf = a.deviceBottomHalf(n)
	}
	a.setSummaryBests(&data, node)
	if a.send(id, Message{Type: "summary", Data: data}) {
		s.summaryPending = false
	}
}

func (a *App) sendStatus(id SessionID) {
	s := a.sessions[id]
	if s == nil || s.failed || s.waitingHello {
		return
	}
	d := a.devices[s.uid]
	if d == nil || d.kind != PlayerDevice || d.session != id {
		return
	}
	if d.node == 0 && (a.preview == nil || a.preview.uid != s.uid) {
		return
	}
	data, key := a.status(d.node)
	var gains []int
	if d.node != 0 {
		gains = a.game.nodes[d.node].gains
	}
	if !s.forceStatus && s.hasStatus && key == s.lastStatus && len(gains) == 0 {
		return
	}
	if len(gains) > 0 {
		// Taken even if the send fails: the session then closes and its next
		// synchronization would drop them anyway.
		data.Gains, a.game.nodes[d.node].gains = gains, nil
	}
	if a.send(id, Message{Type: "status", Data: data}) {
		s.lastStatus, s.hasStatus, s.forceStatus = key, true, false
	}
}

func (a *App) sendReset(id SessionID, packets []PacketView) bool {
	s := a.sessions[id]
	if s == nil {
		return false
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		a.options.Logger.Error("cannot generate synchronization ID", "error", err)
		a.closeSession(id)
		return false
	}
	s.sync = strings.ToUpper(hex.EncodeToString(token[:]))
	if !a.send(id, Message{Type: "reset", Data: ResetData{s.sync, packets}}) {
		return false
	}
	s.phasePending = a.game.hasSummary(a.sessionNode(id)) && !a.previewing(id)
	return true
}

// queue is what the player MCU of node caches: the practice queue while
// practicing, and the real FIFO otherwise (server.md 3.6).
func (a *App) queue(node NodeID) []PacketView {
	packets := make([]PacketView, 0)
	if node == 0 {
		return packets
	}
	n := &a.game.nodes[node]
	if n.practicing() {
		return append(packets, n.practice...)
	}
	for _, p := range n.fifo {
		packets = append(packets, p.PacketView)
	}
	return packets
}

func (a *App) resetFIFO(id SessionID, node NodeID) bool {
	return a.sendReset(id, a.queue(node))
}

func (a *App) phase(id SessionID) bool {
	node := a.sessionNode(id)
	if !a.send(id, Message{Type: "phase", Data: PhaseData{a.game.wirePhase(node)}}) {
		return false
	}
	a.sessions[id].forceStatus = true
	a.sessions[id].phasePending = false
	a.sessions[id].summaryPending = a.game.hasSummary(node) && !a.previewing(id)
	return true
}

func (a *App) syncDevice(id SessionID, config bool) {
	s := a.sessions[id]
	if s == nil || s.failed || s.waitingHello {
		return
	}
	d := a.devices[s.uid]
	if d == nil || d.session != id || d.node == 0 {
		return
	}
	if d.kind == LEDDevice {
		if a.send(id, Message{Type: "CONFIG", Data: LEDConfigData{Node: d.node, Preview: false}}) && d.node.bot() {
			a.send(id, Message{Type: "BUFFER", Data: BufferData{len(a.game.nodes[d.node].fifo)}})
		}
		return
	}
	mode := "auto"
	if a.game.nodes[d.node].manual {
		mode = "manual"
	}
	if config && !a.send(id, Message{Type: "config", Data: ConfigData{Node: d.node, Mode: mode}}) {
		return
	}
	// A synchronizing status never replays earlier gains (player.md).
	a.game.nodes[d.node].gains = nil
	if a.resetFIFO(id, d.node) {
		a.phase(id)
	}
}

func (a *App) restoreUnassigned(uid string) {
	d := a.devices[uid]
	if d == nil || d.session == "" {
		return
	}
	a.identify(d.session, a.eventNow)
}

func (a *App) notify(c change) {
	if a.fatalErr != nil {
		return
	}
	switch c.kind {
	case "station":
		a.notifyStation(c)
	case "phase":
		a.assignPending = true
		a.autoAssignPending()
	case "finished":
		if c.node != 0 {
			a.recordRound(c.node)
		} else {
			a.recordGame()
		}
	case "reset":
		a.gameBest = nil
		a.notifyReset()
	case "push", "fifo":
		if c.node.bot() {
			if c.kind == "push" {
				a.buffer(c.node)
			}
			return
		}
		// A practicing MCU caches its practice queue, not the real FIFO.
		if a.game.nodes[c.node].practicing() {
			return
		}
		if id := a.nodeSession(PlayerDevice, c.node); id != "" {
			if a.preview != nil && a.preview.uid == a.sessions[id].uid {
				return
			}
			if c.kind == "push" {
				a.send(id, Message{Type: "push", Data: c.packet})
			} else {
				a.resetFIFO(id, c.node)
			}
		}
	case "buffer":
		a.buffer(c.node)
	case "link":
		a.light(c.node, c.to, c.packet.Destination)
	}
}

// notifyStation sends a player's new station: phase, a reset when its MCU
// switches between the practice queue and the real FIFO, then (with the
// flush) status and summary (player.md).
func (a *App) notifyStation(c change) {
	id := a.nodeSession(PlayerDevice, c.node)
	if id == "" || !a.phase(id) || !c.practice || a.previewing(id) {
		return
	}
	if a.game.nodes[c.node].station == StationFinished {
		// The simultaneous game's practice ends with its status before the
		// real FIFO (server.md 3.7.4).
		a.sendStatus(id)
	}
	a.resetFIFO(id, c.node)
}

// notifyReset sends what Start and Stop of endless operation, and Start and
// Reset of the simultaneous game, send (server.md 4.2).
func (a *App) notifyReset() {
	p := a.clearPreview()
	for node := NodeID(1); node <= PlayerCount; node++ {
		id := a.nodeSession(PlayerDevice, node)
		if id == "" {
			continue
		}
		// Initial values never replay earlier gains.
		a.game.nodes[node].gains = nil
		if p != nil && p.uid == a.sessions[id].uid {
			if !a.send(id, Message{Type: "config", Data: ConfigData{Node: node, Mode: "auto", Preview: false}}) {
				continue
			}
		}
		if a.phase(id) {
			a.resetFIFO(id, node)
		}
	}
	for _, node := range ledNodes {
		id := a.nodeSession(LEDDevice, node)
		if id == "" {
			continue
		}
		if p != nil && p.uid == a.sessions[id].uid {
			a.syncDevice(id, true)
			continue
		}
		if a.send(id, Message{Type: "CLEAR", Data: struct{}{}}) && node.bot() {
			a.send(id, Message{Type: "BUFFER", Data: BufferData{0}})
		}
	}
	if p != nil && a.devices[p.uid].node == 0 {
		a.restoreUnassigned(p.uid)
	}
	a.assignPending = true
	a.autoAssignPending()
}

func (a *App) buffer(node NodeID) {
	id := a.nodeSession(LEDDevice, node)
	if id != "" && (a.preview == nil || a.preview.uid != a.sessions[id].uid) {
		a.send(id, Message{Type: "BUFFER", Data: BufferData{len(a.game.nodes[node].fifo)}})
	}
}

// linkOwner is the controller that drives the undirected link between a and b
// in the LED layout table, and the neighbor it names the link by: a bot drives
// its links, and of two players the odd one does.
func linkOwner(a, b NodeID) (owner, neighbor NodeID) {
	if b.bot() || !a.bot() && a%2 == 0 {
		return b, a
	}
	return a, b
}

func (a *App) light(from, to, destination NodeID) {
	owner, neighbor := linkOwner(from, to)
	id := a.nodeSession(LEDDevice, owner)
	if id == "" || (a.preview != nil && a.preview.uid == a.sessions[id].uid) {
		return
	}
	direction := "outgoing"
	if owner == to {
		direction = "incoming"
	}
	a.send(id, Message{Type: "LIGHT", Data: LightData{neighbor, destination, direction}})
}

// cheer is a CHEER command and the controller to send it to.
type cheer struct {
	owner NodeID
	data  CheerData
}

// cheers lists the CHEER commands of a settled result (server.md 5.3, 3.7.4):
// one for every link of each node in its best, from that node in the style of
// the highest best it set. A link between two tied nodes gets one, from the
// lower.
func cheers(best *BestScore, bests deviceBests) []cheer {
	if best == nil {
		return nil
	}
	var list []cheer
	for _, node := range best.Nodes {
		style := bests.cheerStyle(node)
		for _, other := range neighbors[node] {
			if other < node && slices.Contains(best.Nodes, other) {
				continue
			}
			owner, neighbor := linkOwner(node, other)
			list = append(list, cheer{owner, CheerData{neighbor, node, style}})
		}
	}
	return list
}

// sendCheers sends the CHEER commands once, when the result is settled. A
// controller set up again later never gets them: the 8-second cheer would no
// longer follow the result (server.md 5.3).
func (a *App) sendCheers(best *BestScore, bests deviceBests) {
	for _, c := range cheers(best, bests) {
		id := a.nodeSession(LEDDevice, c.owner)
		if id != "" && !a.previewing(id) {
			a.send(id, Message{Type: "CHEER", Data: c.data})
		}
	}
}
