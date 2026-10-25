package app

import (
	"errors"
	"slices"
)

var ledNodes = []NodeID{1, 3, 5, 7, 9, 10, 11}

func validAssignment(kind DeviceKind, node NodeID) bool {
	if kind == PlayerDevice {
		return node.player()
	}
	return kind == LEDDevice && slices.Contains(ledNodes, node)
}

func (a *App) hello(id SessionID, message IncomingMessage) error {
	s := a.sessions[id]
	if !validIdentity(message.Kind, message.UID) || (s.uid != "" && s.uid != message.UID) {
		a.closeSession(id)
		return errors.New("invalid or changed device identity")
	}
	d := a.devices[message.UID]
	if d != nil && d.kind != message.Kind {
		a.closeSession(id)
		return errors.New("device kind changed")
	}
	s.uid, s.waitingHello = message.UID, false
	if d == nil {
		d = &device{kind: message.Kind}
		a.devices[s.uid] = d
	}
	if d.session != "" && d.session != id {
		a.options.Logger.Warn("duplicate device UID", "uid", s.uid, "session", id, "existing_session", d.session)
		return nil
	}
	d.session = id
	wasPreview := a.preview != nil && a.preview.uid == s.uid
	if wasPreview {
		a.clearPreview()
	}
	if d.node != 0 {
		a.syncDevice(id, true)
	} else if !a.autoAssign(s.uid) && wasPreview {
		a.restoreUnassigned(s.uid)
	}
	return nil
}

func (a *App) sessionNode(id SessionID) NodeID {
	s := a.sessions[id]
	if s == nil || s.failed || s.waitingHello {
		return 0
	}
	d := a.devices[s.uid]
	if d == nil || d.session != id || d.kind != PlayerDevice {
		return 0
	}
	return d.node
}

func (a *App) nodeSession(kind DeviceKind, node NodeID) SessionID {
	for _, d := range a.devices {
		if d.kind == kind && d.node == node && d.session != "" {
			if s := a.sessions[d.session]; s != nil && !s.failed && !s.waitingHello {
				return d.session
			}
		}
	}
	return ""
}

func (a *App) assigned(kind DeviceKind, node NodeID, except string) bool {
	for uid, d := range a.devices {
		if uid != except && d.kind == kind && d.node == node {
			return true
		}
	}
	return false
}

func (a *App) autoAssign(uid string) bool {
	d := a.devices[uid]
	if a.game.phase == Playing || d.node != 0 || d.noAuto || d.session == "" {
		return false
	}
	if s := a.sessions[d.session]; s == nil || s.failed || s.waitingHello {
		return false
	}
	nodes := []NodeID{1, 2, 3, 4, 5, 6, 7, 8}
	if d.kind == LEDDevice {
		nodes = ledNodes
	}
	for _, node := range nodes {
		if !a.assigned(d.kind, node, uid) {
			a.applyAssignments(map[string]NodeID{uid: node}, false)
			return true
		}
	}
	return false
}

func (a *App) autoAssignPending() {
	if a.game.phase == Playing {
		return
	}
	uids := make([]string, 0, len(a.devices))
	for uid := range a.devices {
		uids = append(uids, uid)
	}
	slices.Sort(uids)
	for _, uid := range uids {
		a.autoAssign(uid)
	}
}

func (a *App) changeAssignments(c Command) error {
	if a.game.phase == Playing {
		return errors.New("device assignment cannot change while Playing")
	}
	d := a.devices[c.DeviceID]
	if d == nil {
		return errors.New("device has not been identified")
	}
	switch c.Operation {
	case SetDeviceAssignment:
		if c.Node != 0 && (!validAssignment(c.Kind, c.Node) || c.Kind != d.kind) {
			return errors.New("invalid assignment kind or node")
		}
		if c.Node == 0 && c.Kind != "" && c.Kind != d.kind {
			return errors.New("assignment kind does not match device")
		}
		if c.Node != 0 && a.assigned(d.kind, c.Node, c.DeviceID) {
			return errors.New("node is already assigned")
		}
		a.applyAssignments(map[string]NodeID{c.DeviceID: c.Node}, true)
	case SwapDeviceAssignments, ReplaceDeviceAssignment:
		other := a.devices[c.OtherDeviceID]
		if c.DeviceID == c.OtherDeviceID || other == nil || d.kind != other.kind || d.node == 0 {
			return errors.New("exchange requires distinct identified devices of the same kind and an assigned source")
		}
		if c.Operation == SwapDeviceAssignments {
			if other.node == 0 {
				return errors.New("swap requires two assigned devices")
			}
			a.applyAssignments(map[string]NodeID{c.DeviceID: other.node, c.OtherDeviceID: d.node}, true)
		} else {
			s := a.sessions[other.session]
			if s == nil || s.failed || s.waitingHello {
				return errors.New("replacement device is not connected and identified")
			}
			a.applyAssignments(map[string]NodeID{c.DeviceID: 0, c.OtherDeviceID: d.node}, true)
		}
	}
	return nil
}

// Validate first, then commit the complete mapping before notifying any MCU.
func (a *App) applyAssignments(updates map[string]NodeID, explicit bool) {
	changed := make([]string, 0, len(updates))
	for uid, node := range updates {
		d := a.devices[uid]
		if explicit && node == 0 {
			d.noAuto = true
		}
		if d.node != node {
			changed = append(changed, uid)
		}
	}
	slices.Sort(changed)
	for _, uid := range changed {
		d := a.devices[uid]
		if a.preview != nil && a.preview.uid == uid {
			a.clearPreview()
		}
		if d.kind == PlayerDevice {
			for _, node := range []NodeID{d.node, updates[uid]} {
				if node != 0 {
					a.game.setManual(node, false)
					a.game.nodes[node].ready = false
				}
			}
		}
	}
	for _, uid := range changed {
		a.devices[uid].node = updates[uid]
	}
	for _, uid := range changed {
		d := a.devices[uid]
		if d.session == "" {
			continue
		}
		if d.node == 0 {
			a.restoreUnassigned(uid)
		} else {
			a.syncDevice(d.session, true)
		}
	}
}
