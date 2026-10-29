package app

import (
	"errors"
	"math"
	"reflect"
	"testing"
	"time"
)

const (
	testUID2 = "112233AABBCC"
	testLED  = "A00000000001"
)

func rejectionOf(t *testing.T, err error) *Rejection {
	t.Helper()
	var r *Rejection
	if !errors.As(err, &r) {
		t.Fatalf("not a Rejection: %v", err)
	}
	return r
}

func TestRejectionCodes(t *testing.T) {
	twoPlayers := func(h *harness) {
		h.connect("p1", testUID, 1)
		h.connect("p2", testUID2, 2)
	}
	playing := func(h *harness) {
		h.connect("p1", testUID, 1)
		h.start()
	}
	for _, tc := range []struct {
		name    string
		setup   func(*harness)
		command Command
		code    Code
		nodes   []NodeID
	}{
		{"mode change while Playing", playing, Command{Operation: SetNodeEnabled, Node: 1, Enabled: true}, CodePhaseConflict, nil},
		{"mode change for bot", nil, Command{Operation: SetNodeEnabled, Node: 9}, CodeInvalid, nil},
		{"manual without MCU", nil, Command{Operation: SetNodeEnabled, Node: 2, Enabled: true}, CodeDeviceDisconnected, nil},
		{"bot rate for player", nil, Command{Operation: SetBotRate, Node: 1, Rate: 1}, CodeInvalid, nil},
		{"bot rate NaN", nil, Command{Operation: SetBotRate, Node: 9, Rate: math.NaN()}, CodeInvalid, nil},
		{"unknown operation", nil, Command{Operation: "unknown"}, CodeInvalid, nil},
		{"Finish while Preparing", nil, Command{Operation: Finish}, CodePhaseConflict, nil},
		{"Start while Playing", playing, Command{Operation: Start}, CodePhaseConflict, nil},
		{"Start with unready players", func(h *harness) {
			twoPlayers(h)
			h.execute(Command{Operation: SetNodeEnabled, Node: 2, Enabled: true})
			h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
		}, Command{Operation: Start}, CodePlayersNotReady, []NodeID{1, 2}},
		{"Start after Finish reports phase first", func(h *harness) {
			playing(h)
			h.execute(Command{Operation: Finish})
			h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
		}, Command{Operation: Start}, CodePhaseConflict, nil},
		{"assignment while Playing", playing, Command{Operation: SetDeviceAssignment, DeviceID: testUID, Kind: PlayerDevice, Node: 2}, CodePhaseConflict, nil},
		{"assignment of unknown device", nil, Command{Operation: SetDeviceAssignment, DeviceID: testUID2, Kind: PlayerDevice, Node: 1}, CodeNotFound, nil},
		{"assignment out of range", nil, Command{Operation: SetDeviceAssignment, DeviceID: testUID, Kind: PlayerDevice, Node: 9}, CodeInvalid, nil},
		{"assignment kind mismatch", nil, Command{Operation: SetDeviceAssignment, DeviceID: testUID, Kind: LEDDevice, Node: 3}, CodeDeviceKindMismatch, nil},
		{"unassignment kind mismatch", nil, Command{Operation: SetDeviceAssignment, DeviceID: testUID, Kind: LEDDevice}, CodeDeviceKindMismatch, nil},
		{"unassignment with invalid kind", nil, Command{Operation: SetDeviceAssignment, DeviceID: testUID, Kind: "invalid"}, CodeInvalid, nil},
		{"assignment to used node", twoPlayers, Command{Operation: SetDeviceAssignment, DeviceID: testUID, Kind: PlayerDevice, Node: 2}, CodeAssignmentConflict, nil},
		{"swap with itself", nil, Command{Operation: SwapDeviceAssignments, DeviceID: testUID, OtherDeviceID: testUID}, CodeInvalid, nil},
		{"swap with unknown device", nil, Command{Operation: SwapDeviceAssignments, DeviceID: testUID, OtherDeviceID: testUID2}, CodeNotFound, nil},
		{"swap with other kind", func(h *harness) {
			h.connect("p1", testUID, 1)
			h.connectLED("l1", testLED, 3)
		}, Command{Operation: SwapDeviceAssignments, DeviceID: testUID, OtherDeviceID: testLED}, CodeDeviceKindMismatch, nil},
		{"swap from unassigned source", func(h *harness) {
			twoPlayers(h)
			h.execute(Command{Operation: SetDeviceAssignment, DeviceID: testUID})
		}, Command{Operation: SwapDeviceAssignments, DeviceID: testUID, OtherDeviceID: testUID2}, CodeDeviceNotAssigned, nil},
		{"swap with unassigned device", func(h *harness) {
			twoPlayers(h)
			h.execute(Command{Operation: SetDeviceAssignment, DeviceID: testUID2})
		}, Command{Operation: SwapDeviceAssignments, DeviceID: testUID, OtherDeviceID: testUID2}, CodeDeviceNotAssigned, nil},
		{"replace with disconnected device", func(h *harness) {
			twoPlayers(h)
			h.must(h.a.Disconnected(h.ctx, "p2"))
		}, Command{Operation: ReplaceDeviceAssignment, DeviceID: testUID, OtherDeviceID: testUID2}, CodeDeviceDisconnected, nil},
		{"preview while Playing", playing, Command{Operation: SetDevicePreview, DeviceID: testUID}, CodePhaseConflict, nil},
		{"preview of unknown device", nil, Command{Operation: SetDevicePreview, DeviceID: testUID2}, CodeNotFound, nil},
		{"preview of disconnected device", func(h *harness) {
			twoPlayers(h)
			h.must(h.a.Disconnected(h.ctx, "p2"))
		}, Command{Operation: SetDevicePreview, DeviceID: testUID2}, CodeDeviceDisconnected, nil},
		{"preview while waiting for HELLO", func(h *harness) {
			twoPlayers(h)
			h.execute(Command{Operation: SetDeviceAssignment, DeviceID: testUID2}) // Unassigned players are re-identified.
		}, Command{Operation: SetDevicePreview, DeviceID: testUID2}, CodeDeviceDisconnected, nil},
		{"preview of manual player", func(h *harness) {
			h.connect("p1", testUID, 1)
			h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
		}, Command{Operation: SetDevicePreview, DeviceID: testUID}, CodeManualModeConflict, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			if tc.setup != nil {
				tc.setup(h)
			} else {
				h.connect("p1", testUID, 1)
			}
			r := rejectionOf(t, h.a.Execute(h.ctx, tc.command))
			if r.Code != tc.code || !reflect.DeepEqual(r.Nodes, tc.nodes) || r.Error() == "" {
				t.Fatalf("got %q %v (%q), want %q %v", r.Code, r.Nodes, r.Error(), tc.code, tc.nodes)
			}
		})
	}
}

func TestStartBlockersOrderAndRejection(t *testing.T) {
	g := newGame(func(change) {})
	if b := g.startBlockers(); b == nil || len(b) != 0 {
		t.Fatalf("startable game reported %v", b)
	}
	g.phase, g.nextPID = Finished, 65536
	g.nodes[3].manual = true
	g.nodes[1].manual = true
	g.nodes[2].manual, g.nodes[2].ready = true, true
	want := []StartBlocker{{Code: CodeNotPreparing}, {Code: CodePIDExhausted}, {Code: CodePlayersNotReady, Nodes: []NodeID{1, 3}}}
	if got := g.startBlockers(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for _, step := range []struct {
		prepare func()
		code    Code
		nodes   []NodeID
	}{
		{func() {}, CodePhaseConflict, nil},
		{func() { g.phase = Preparing }, CodePIDExhausted, nil},
		{func() { g.nextPID = 0 }, CodePlayersNotReady, []NodeID{1, 3}},
	} {
		step.prepare()
		r := rejectionOf(t, g.start(epoch))
		if r.Code != step.code || !reflect.DeepEqual(r.Nodes, step.nodes) {
			t.Fatalf("got %q %v, want %q %v", r.Code, r.Nodes, step.code, step.nodes)
		}
	}
	if g.phase != Preparing {
		t.Fatal("rejected Start changed phase")
	}
}

func TestSnapshotRemainingSecondsMatchesStatus(t *testing.T) {
	h := newHarness(t)
	h.connect("p1", testUID, 1)
	if s := h.state(); s.RemainingSeconds != gameSeconds {
		t.Fatalf("Preparing remaining %d", s.RemainingSeconds)
	}
	h.start()
	h.clear()
	h.tick(1500 * time.Millisecond)
	statuses := h.statusMessages("p1")
	if len(statuses) == 0 {
		t.Fatal("missing STATUS")
	}
	if s := h.state(); s.RemainingSeconds != gameSeconds-1 || s.RemainingSeconds != statuses[len(statuses)-1].Remaining {
		t.Fatalf("snapshot %d, status %d", s.RemainingSeconds, statuses[len(statuses)-1].Remaining)
	}
	h.execute(Command{Operation: Finish})
	h.tick(10 * time.Second)
	if s := h.state(); s.RemainingSeconds != gameSeconds-1 {
		t.Fatalf("Finished remaining changed: %d", s.RemainingSeconds)
	}
	h.execute(Command{Operation: Reset})
	if s := h.state(); s.RemainingSeconds != gameSeconds {
		t.Fatalf("Reset remaining: %d", s.RemainingSeconds)
	}
}
