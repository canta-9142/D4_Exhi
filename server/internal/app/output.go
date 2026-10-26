package app

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
)

func (a *App) remainingSeconds() int {
	remaining := GameDuration - a.game.elapsed
	return int((remaining + time.Second - 1) / time.Second)
}

func (a *App) status(node NodeID) StatusData {
	data := StatusData{Remaining: a.remainingSeconds()}
	if node != 0 {
		n := &a.game.nodes[node]
		data.Score, data.Ready = n.score, n.manual && n.ready
	}
	return data
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
	var rank *int
	if n.eligible {
		value := n.rank
		rank = &value
	}
	if a.send(id, Message{Type: "summary", Data: SummaryData{Sync: s.sync, Score: n.score, Rank: rank, Eligible: n.eligible}}) {
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
	data := a.status(d.node)
	if !s.forceStatus && s.hasStatus && data == s.lastStatus {
		return
	}
	if a.send(id, Message{Type: "status", Data: data}) {
		s.lastStatus, s.hasStatus, s.forceStatus = data, true, false
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
	switch c.kind {
	case "phase":
		for node := NodeID(1); node <= PlayerCount; node++ {
			if id := a.nodeSession(PlayerDevice, node); id != "" {
				a.phase(id)
			}
		}
		a.autoAssignPending()
	case "reset":
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
	a.autoAssignPending()
}

func (a *App) buffer(node NodeID) {
	id := a.nodeSession(LEDDevice, node)
	if id != "" && (a.preview == nil || a.preview.uid != a.sessions[id].uid) {
		a.send(id, Message{Type: "BUFFER", Data: BufferData{len(a.game.nodes[node].fifo)}})
	}
}

// Every undirected link is driven by one controller from the LED layout table.
func (a *App) light(from, to, destination NodeID) {
	owner, neighbor := from, to
	if to.bot() {
		owner, neighbor = to, from
	} else if !from.bot() {
		if from%2 == 0 {
			owner, neighbor = to, from
		}
	}
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
