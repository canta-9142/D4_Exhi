package app

import (
	"errors"
	"time"
)

type preview struct {
	uid     string
	node    NodeID // Includes temporary P1 for an unassigned board.
	started time.Time
	slot    int64
}

func (a *App) setPreview(uid string, now time.Time) error {
	if uid == "" {
		a.restorePreview()
		return nil
	}
	if a.game.phase == Playing {
		return errors.New("preview cannot start while Playing")
	}
	d := a.devices[uid]
	if d == nil || d.session == "" {
		return errors.New("preview requires a connected, identified device")
	}
	s := a.sessions[d.session]
	if s == nil || s.failed || s.waitingHello {
		return errors.New("device is waiting for identification")
	}
	if d.kind == PlayerDevice && d.node != 0 && a.game.nodes[d.node].manual {
		return errors.New("player preview requires automatic operation")
	}
	if a.preview != nil && a.preview.uid == uid {
		return nil
	}
	a.restorePreview()
	node := d.node
	if node == 0 {
		node = 1
	}
	a.preview = &preview{uid: uid, node: node, started: now, slot: -1}
	if d.kind == LEDDevice {
		if a.send(d.session, Message{Type: "CONFIG", Data: LEDConfigData{node}}) {
			a.updatePreview(now)
		}
	} else if a.send(d.session, Message{Type: "config", Data: ConfigData{Node: node, Mode: "auto"}}) && a.resetFIFO(d.session, d.node) {
		a.phase(d.session)
	}
	return nil
}

// Drop preview ownership when the caller's Reset/config/resync will restore the
// device, or when it disconnected. This also prevents late HELLOs restarting it.
func (a *App) clearPreview() *preview {
	p := a.preview
	a.preview = nil
	return p
}

// Finish the complete restoration before starting another preview or Playing.
func (a *App) restorePreview() {
	p := a.clearPreview()
	if p == nil {
		return
	}
	d := a.devices[p.uid]
	if d.session == "" {
		return
	}
	if d.node == 0 {
		a.restoreUnassigned(p.uid)
	} else if d.kind == PlayerDevice {
		if a.resetFIFO(d.session, d.node) && a.phase(d.session) {
			a.sendStatus(d.session)
		}
	} else if a.send(d.session, Message{Type: "CLEAR", Data: struct{}{}}) && d.node.bot() {
		a.send(d.session, Message{Type: "BUFFER", Data: BufferData{len(a.game.nodes[d.node].fifo)}})
	}
}

func (a *App) updatePreview(now time.Time) {
	p := a.preview
	if p == nil {
		return
	}
	d := a.devices[p.uid]
	if d.kind != LEDDevice {
		return
	}
	// Send only the current half-second state, even after multiple missed cycles.
	slot := max(0, int64(now.Sub(p.started)/(500*time.Millisecond)))
	if slot == p.slot {
		return
	}
	p.slot = slot
	if !a.send(d.session, Message{Type: "CLEAR", Data: struct{}{}}) || slot%2 != 0 {
		return
	}
	if p.node.bot() {
		if !a.send(d.session, Message{Type: "BUFFER", Data: BufferData{4}}) {
			return
		}
		// GPIO1 is the first player link for every Bot controller.
		a.send(d.session, Message{Type: "LIGHT", Data: LightData{neighbors[p.node][0], p.node, "outgoing"}})
	} else {
		// P3/P5/P7 use the previous neighbor on GPIO0, next on GPIO1.
		// P1 instead uses P2 on GPIO0 and the wraparound P8 on GPIO1.
		first, second := p.node-1, p.node+1
		if p.node == 1 {
			first, second = 2, 8
		}
		if a.send(d.session, Message{Type: "LIGHT", Data: LightData{first, p.node, "outgoing"}}) {
			a.send(d.session, Message{Type: "LIGHT", Data: LightData{second, p.node, "outgoing"}})
		}
	}
}
