package app

import "errors"

type preview struct {
	uid  string
	node NodeID // Includes temporary P1 for an unassigned board.
}

func (a *App) setPreview(uid string) error {
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
	a.preview = &preview{uid: uid, node: node}
	if d.kind == LEDDevice {
		a.send(d.session, Message{Type: "CONFIG", Data: LEDConfigData{Node: node, Preview: true}})
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
	} else if a.send(d.session, Message{Type: "CONFIG", Data: LEDConfigData{Node: d.node}}) && d.node.bot() {
		a.send(d.session, Message{Type: "BUFFER", Data: BufferData{len(a.game.nodes[d.node].fifo)}})
	}
}
