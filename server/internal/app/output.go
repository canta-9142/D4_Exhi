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

func (a *App) flushStatus() {
	for id := range a.sessions {
		a.sendStatus(id)
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
	return a.send(id, Message{Type: "reset", Data: ResetData{s.sync, packets}})
}

func (a *App) resetFIFO(id SessionID, node NodeID) bool {
	packets := make([]PacketView, 0)
	// The preview copy never enters the game's FIFO or PID allocator.
	if a.preview != nil && a.sessions[id] != nil && a.preview.uid == a.sessions[id].uid {
		packets = append(packets, PacketView{PID: 9999, Destination: a.preview.node})
	} else if node != 0 {
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
		if a.send(id, Message{Type: "CONFIG", Data: LEDConfigData{Node: d.node}}) && d.node.bot() {
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
	case "phase", "reset":
		var stopped *preview
		if c.kind == "reset" {
			stopped = a.clearPreview()
		}
		for node := NodeID(1); node <= PlayerCount; node++ {
			if id := a.nodeSession(PlayerDevice, node); id != "" {
				if a.phase(id) && c.kind == "reset" {
					a.resetFIFO(id, node)
				}
			}
		}
		if c.kind == "reset" {
			for _, node := range ledNodes {
				if id := a.nodeSession(LEDDevice, node); id != "" {
					message := Message{Type: "CLEAR", Data: struct{}{}}
					if stopped != nil && stopped.uid == a.sessions[id].uid {
						message = Message{Type: "CONFIG", Data: LEDConfigData{Node: node}}
					}
					if a.send(id, message) && node.bot() {
						a.send(id, Message{Type: "BUFFER", Data: BufferData{0}})
					}
				}
			}
			if stopped != nil && a.devices[stopped.uid].node == 0 {
				a.restoreUnassigned(stopped.uid)
			}
		}
		a.autoAssignPending()
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
