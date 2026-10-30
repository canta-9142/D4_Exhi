package app

import (
	"crypto/rand"
	"encoding/hex"
	"slices"
	"strings"
)

func (a *App) remainingSeconds() int {
	return seconds(GameDuration - a.game.elapsed)
}

// statusKey is the part of a status compared with the last one sent. Gains are
// not part of it: any pending gains are sent.
type statusKey struct {
	score, remaining, countdown, rank, combo int
	ready                                    bool
}

// status leaves Gains empty; sendStatus takes the pending gains.
func (a *App) status(node NodeID) (StatusData, statusKey) {
	key := statusKey{remaining: a.remainingSeconds(), countdown: a.game.countdown}
	if node != 0 {
		n := &a.game.nodes[node]
		key.score, key.ready, key.rank, key.combo = n.score, n.manual && n.ready, a.game.statusRank(node), n.combo
	}
	data := StatusData{Score: key.score, Remaining: key.remaining, Ready: key.ready, Countdown: key.countdown,
		Gains: []int{}, Combo: key.combo}
	if key.rank != 0 {
		rank := key.rank
		data.Rank = &rank
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
	if a.game.phase != Finished || node == 0 || s.sync == "" || a.previewing(id) {
		s.summaryPending = false
		return
	}
	n := &a.game.nodes[node]
	var rank, topPercent *int
	bottomHalf := false
	if n.eligible {
		value := n.rank
		rank = &value
		topPercent = a.deviceTopPercent(n)
		bottomHalf = a.deviceBottomHalf(n)
	}
	data := SummaryData{Sync: s.sync, Score: n.score, Rank: rank, Eligible: n.eligible, TopPercent: topPercent,
		BottomHalf: bottomHalf}
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
	s.phasePending = a.game.phase == Finished && a.sessionNode(id) != 0 && !a.previewing(id)
	return true
}

func (a *App) resetFIFO(id SessionID, node NodeID) bool {
	packets := make([]PacketView, 0)
	if node != 0 {
		for _, p := range a.game.nodes[node].fifo {
			packets = append(packets, p.PacketView)
		}
	}
	return a.sendReset(id, packets)
}

func (a *App) phase(id SessionID) bool {
	if !a.send(id, Message{Type: "phase", Data: PhaseData{a.game.phase}}) {
		return false
	}
	a.sessions[id].forceStatus = true
	a.sessions[id].phasePending = false
	a.sessions[id].summaryPending = a.game.phase == Finished && a.sessionNode(id) != 0 && !a.previewing(id)
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
	case "phase":
		for node := NodeID(1); node <= PlayerCount; node++ {
			if id := a.nodeSession(PlayerDevice, node); id != "" {
				a.phase(id)
			}
		}
		a.assignPending = true
		a.autoAssignPending()
	case "finished":
		a.recordGame()
		a.sendCheers()
	case "reset":
		a.gameBest = nil
		a.deviceBests = deviceBests{}
		a.notifyReset()
	case "push", "fifo":
		if c.node.bot() {
			if c.kind == "push" {
				a.buffer(c.node)
			}
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

func (a *App) notifyReset() {
	p := a.clearPreview()
	for node := NodeID(1); node <= PlayerCount; node++ {
		id := a.nodeSession(PlayerDevice, node)
		if id == "" {
			continue
		}
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

// cheers lists the CHEER commands of a finished game (server.md 5.3): one for
// every link of each node in its best, from that node in the style of the
// highest best it set. A link between two tied nodes gets one, from the lower.
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

// sendCheers sends the CHEER commands once, at Finished. A controller set up
// again while Finished never gets them: the 8-second cheer would no longer
// follow the result (server.md 5.3).
func (a *App) sendCheers() {
	for _, c := range cheers(a.gameBest, a.deviceBests) {
		id := a.nodeSession(LEDDevice, c.owner)
		if id != "" && !a.previewing(id) {
			a.send(id, Message{Type: "CHEER", Data: c.data})
		}
	}
}
