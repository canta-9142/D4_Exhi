package app

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
		return reject(CodePhaseConflict, "preview cannot start while Playing")
	}
	d := a.devices[uid]
	if d == nil {
		return reject(CodeNotFound, "device has not been identified")
	}
	s := a.sessions[d.session]
	if s == nil || s.failed || s.waitingHello {
		return reject(CodeDeviceDisconnected, "preview requires a connected, identified device")
	}
	if d.kind == PlayerDevice && d.node != 0 && a.game.nodes[d.node].manual {
		return reject(CodeManualModeConflict, "player preview requires automatic operation")
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
	} else if a.send(d.session, Message{Type: "config", Data: ConfigData{Node: node, Mode: "auto", Preview: true}}) && a.resetFIFO(d.session, d.node) {
		a.phase(d.session)
	}
	return nil
}

func (a *App) previewing(id SessionID) bool {
	s := a.sessions[id]
	return s != nil && a.preview != nil && a.preview.uid == s.uid && a.devices[s.uid].session == id
}

// Drop ownership when a subsequent Reset/config/resync will restore the device,
// or when it disconnected. A late HELLO cannot restart the preview.
func (a *App) clearPreview() *preview {
	p := a.preview
	a.preview = nil
	return p
}

// Send preview=false and complete normal synchronization before the next action.
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
	} else {
		a.syncDevice(d.session, true)
		if d.kind == PlayerDevice {
			a.flushPlayerSession(d.session)
		}
	}
}
