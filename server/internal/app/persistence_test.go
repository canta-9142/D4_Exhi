package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"
)

func (h *harness) setSaveDevices(save func([]DeviceAssignment) error) {
	h.t.Helper()
	h.must(h.a.submit(h.ctx, false, func(time.Time) error {
		h.a.options.SaveDevices = save
		return nil
	}))
}

func reservedPlayers() []DeviceAssignment {
	devices := make([]DeviceAssignment, 0, PlayerCount)
	for node := NodeID(1); node <= PlayerCount; node++ {
		devices = append(devices, DeviceAssignment{DeviceID: fmt.Sprintf("F%011X", node), Kind: PlayerDevice, Node: node})
	}
	return devices
}

func TestAssignmentsPersistAndRestoreAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	store := &DeviceStore{Path: path}
	saves := 0
	save := func(devices []DeviceAssignment) error {
		saves++
		return store.Save(devices)
	}
	h := newHarnessWithOptions(t, Options{SaveDevices: save})
	const replacement = "112233AABBCC"
	const led = "A00000000001"
	h.connect("source", testUID, 1)
	h.connect("replacement", replacement, 2)
	h.connectLED("led", led, 9)
	h.execute(Command{Operation: SetDeviceAssignment, DeviceID: testUID})
	h.message("source", IncomingMessage{Type: "hello", UID: testUID})
	h.execute(Command{Operation: SetDeviceAssignment, DeviceID: testUID, Node: 1})
	// Reassigning a previously excluded UID must retain the exclusion flag.
	h.execute(Command{Operation: SwapDeviceAssignments, DeviceID: testUID, OtherDeviceID: replacement})
	before := saves
	h.execute(Command{Operation: ReplaceDeviceAssignment, DeviceID: testUID, OtherDeviceID: replacement})
	if saves != before+1 {
		t.Fatalf("replacement was not one save: %d -> %d", before, saves)
	}
	want := []DeviceAssignment{
		{testUID, PlayerDevice, 0, true},
		{replacement, PlayerDevice, 2, false},
		{led, LEDDevice, 9, false},
	}
	devices, err := LoadDeviceAssignments(path)
	h.must(err)
	if !reflect.DeepEqual(devices, want) {
		t.Fatalf("persisted mapping: %v, want %v", devices, want)
	}
	before = saves
	h.execute(Command{Operation: SetDevicePreview, DeviceID: led})
	h.execute(Command{Operation: SetDevicePreview})
	h.execute(Command{Operation: Reset})
	h.must(h.a.Disconnected(h.ctx, "replacement"))
	if saves != before {
		t.Fatal("preview, Reset or disconnect persisted temporary state")
	}
	restored := newHarnessWithOptions(t, Options{InitialDevices: devices, SaveDevices: save})
	snapshot := restored.state()
	if snapshot.Phase != Preparing || snapshot.Preview != nil || len(snapshot.Devices) != len(want) {
		t.Fatalf("bad restored state: %+v", snapshot)
	}
	for _, d := range snapshot.Devices {
		if d.Connected || d.Session != "" {
			t.Fatal("restored device has a live session")
		}
	}
	for _, node := range snapshot.Nodes {
		if node.Enabled || node.Ready || node.Score != 0 || node.Rank != nil || node.FIFOUsage != 0 {
			t.Fatalf("restored transient game state: %+v", node)
		}
	}
	// P2 is reserved even before its owner connects; a new UID gets P1.
	restored.must(restored.a.Connected(restored.ctx, "new"))
	restored.message("new", IncomingMessage{Type: "hello", UID: "223344AABBCC"})
	if d := restored.device("223344AABBCC"); d.Node != 1 {
		t.Fatalf("restored reservation stolen: %+v", d)
	}
	restored.must(restored.a.Connected(restored.ctx, "old"))
	restored.message("old", IncomingMessage{Type: "hello", UID: testUID})
	if restored.device(testUID).Node != 0 {
		t.Fatal("restored excluded UID automatically assigned")
	}
	restored.must(restored.a.Connected(restored.ctx, "replacement"))
	restored.clear()
	restored.message("replacement", IncomingMessage{Type: "hello", UID: replacement})
	if d := restored.device(replacement); d.Node != 2 || !d.Connected {
		t.Fatalf("restored owner not synchronized: %+v", d)
	}
	if messages := restored.messagesFor("replacement"); len(messages) < 3 || messages[0].Type != "config" || messages[0].Data.(ConfigData).Mode != "auto" {
		t.Fatalf("bad restored synchronization: %v", messages)
	}
	restored.must(restored.a.Connected(restored.ctx, "wrong-kind"))
	if err := restored.a.MessageReceived(restored.ctx, "wrong-kind", IncomingMessage{Type: "hello", Kind: LEDDevice, UID: replacement}); err == nil {
		t.Fatal("restored kind mismatch accepted")
	}
	if d := restored.device(replacement); d.Kind != PlayerDevice || d.Node != 2 || d.Session != "replacement" {
		t.Fatal("kind mismatch changed the restored assignment")
	}
}

func TestFailedAssignmentSaveLeavesStateAndNotificationsUnchanged(t *testing.T) {
	for _, command := range []Command{
		{Operation: SetDeviceAssignment, DeviceID: testUID, Kind: PlayerDevice, Node: 3},
		{Operation: SetDeviceAssignment, DeviceID: testUID},
		{Operation: SwapDeviceAssignments, DeviceID: testUID, OtherDeviceID: testUID2},
		{Operation: ReplaceDeviceAssignment, DeviceID: testUID, OtherDeviceID: testUID2},
	} {
		t.Run(fmt.Sprintf("%s/%d", command.Operation, command.Node), func(t *testing.T) {
			h := newHarness(t)
			h.connect("source", testUID, 1)
			h.connect("other", testUID2, 2)
			h.execute(Command{Operation: SetNodeEnabled, Node: 2, Enabled: true})
			h.ready("other")
			h.execute(Command{Operation: SetDevicePreview, DeviceID: testUID})
			before := h.state()
			beforeAssignments := h.a.deviceAssignments(nil, false)
			failure := errors.New("disk full")
			calls := 0
			h.setSaveDevices(func(candidate []DeviceAssignment) error {
				calls++
				if !slices.Equal(h.a.deviceAssignments(nil, false), beforeAssignments) || h.a.preview == nil || !h.a.game.nodes[2].ready {
					t.Error("state changed before saving")
				}
				return failure
			})
			h.clear()
			if err := h.a.Execute(h.ctx, command); !errors.Is(err, failure) {
				t.Fatalf("save error lost: %v", err)
			}
			if calls != 1 || !reflect.DeepEqual(h.state(), before) || !slices.Equal(h.a.deviceAssignments(nil, false), beforeAssignments) || len(h.messages()) != 0 {
				t.Fatal("failed save changed assignment, exclusion, mode, ready, preview or notifications")
			}
		})
	}
}

func TestNotificationFailureKeepsDurableAssignment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	store := &DeviceStore{Path: path}
	h := newHarnessWithOptions(t, Options{SaveDevices: store.Save})
	h.connect("player", testUID, 1)
	h.mu.Lock()
	h.failType = "config"
	h.mu.Unlock()
	h.execute(Command{Operation: SetDeviceAssignment, DeviceID: testUID, Node: 2})
	devices, err := LoadDeviceAssignments(path)
	h.must(err)
	if !slices.Equal(devices, []DeviceAssignment{{testUID, PlayerDevice, 2, false}}) {
		t.Fatalf("send failure rolled back file: %v", devices)
	}
	if d := h.device(testUID); d.Connected || d.Node != 2 {
		t.Fatalf("send failure rolled back assignment: %+v", d)
	}
}

func TestExclusionOnlyChangeIsPersistedAndIdenticalRequestsDoNotWrite(t *testing.T) {
	h := newHarnessWithOptions(t, Options{InitialDevices: reservedPlayers()})
	h.must(h.a.Connected(h.ctx, "spare"))
	h.message("spare", IncomingMessage{Type: "hello", UID: testUID})
	if h.device(testUID).Node != 0 {
		t.Fatal("expected unassigned spare")
	}
	saves := 0
	failed := true
	failure := errors.New("disk full")
	h.setSaveDevices(func(devices []DeviceAssignment) error {
		saves++
		if devices[0].DeviceID != testUID || !devices[0].AutoAssignExcluded || devices[0].Node != 0 {
			t.Errorf("missing exclusion-only candidate: %v", devices)
		}
		if failed {
			return failure
		}
		return nil
	})
	h.clear()
	command := Command{Operation: SetDeviceAssignment, DeviceID: testUID}
	if err := h.a.Execute(h.ctx, command); !errors.Is(err, failure) || h.a.devices[testUID].noAuto {
		t.Fatalf("failed exclusion committed: %v", err)
	}
	failed = false
	h.execute(command)
	h.execute(command)
	if saves != 2 || !h.a.devices[testUID].noAuto || len(h.messages()) != 0 {
		t.Fatal("exclusion was lost, repeated or caused unnecessary synchronization")
	}
}

func TestAutomaticSaveFailurePreservesConnectionAndRetriesOnTrigger(t *testing.T) {
	saves := 0
	failed := true
	h := newHarnessWithOptions(t, Options{SaveDevices: func([]DeviceAssignment) error {
		saves++
		if failed {
			return errors.New("disk full")
		}
		return nil
	}})
	h.must(h.a.Connected(h.ctx, "player"))
	h.clear()
	h.message("player", IncomingMessage{Type: "hello", UID: testUID})
	if d := h.device(testUID); !d.Connected || d.Node != 0 || h.a.devices[testUID].noAuto || len(h.messages()) != 0 || saves != 1 {
		t.Fatalf("automatic failure lost identity or connection: %+v, saves %d", d, saves)
	}
	h.tick(time.Second)
	if saves != 1 {
		t.Fatal("retry occurred without an assignment trigger")
	}
	failed = false
	h.execute(Command{Operation: Reset})
	if h.device(testUID).Node != 1 || saves != 2 {
		t.Fatal("automatic assignment did not retry at Reset")
	}
}

func TestFollowUpAutomaticSaveFailureDoesNotUndoSuccessfulOperation(t *testing.T) {
	devices := reservedPlayers()
	h := newHarnessWithOptions(t, Options{InitialDevices: devices})
	h.must(h.a.Connected(h.ctx, "spare"))
	h.message("spare", IncomingMessage{Type: "hello", UID: testUID})
	var committed []DeviceAssignment
	calls := 0
	h.setSaveDevices(func(candidate []DeviceAssignment) error {
		calls++
		if calls == 2 {
			return errors.New("disk full")
		}
		committed = slices.Clone(candidate)
		return nil
	})
	h.execute(Command{Operation: SetDeviceAssignment, DeviceID: devices[0].DeviceID})
	if calls != 2 || h.device(devices[0].DeviceID).Node != 0 || !h.a.devices[devices[0].DeviceID].noAuto || h.device(testUID).Node != 0 || !slices.Equal(committed, h.a.deviceAssignments(nil, false)) {
		t.Fatal("follow-up save failure undid successful unassignment")
	}
	h.execute(Command{Operation: Reset})
	if calls != 3 || h.device(testUID).Node != 1 {
		t.Fatal("follow-up assignment did not retry")
	}
}

func TestAssignmentRetryRequiresNewlyFreedRole(t *testing.T) {
	for _, tc := range []struct {
		name    string
		command Command
		writes  int
	}{
		{"swap", Command{Operation: SwapDeviceAssignments, DeviceID: testUID, OtherDeviceID: testUID2}, 1},
		{"same assignment", Command{Operation: SetDeviceAssignment, DeviceID: testUID, Kind: PlayerDevice, Node: 1}, 0},
		{"move", Command{Operation: SetDeviceAssignment, DeviceID: testUID, Kind: PlayerDevice, Node: 3}, 2},
		{"replace", Command{Operation: ReplaceDeviceAssignment, DeviceID: testUID, OtherDeviceID: testUID2}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.connect("first", testUID, 1)
			h.connect("second", testUID2, 2)
			writes := 0
			h.setSaveDevices(func(candidate []DeviceAssignment) error {
				writes++
				if len(candidate) == 3 {
					return errors.New("automatic save failed")
				}
				return nil
			})
			h.must(h.a.Connected(h.ctx, "waiting"))
			h.message("waiting", IncomingMessage{Type: "hello", UID: "223344AABBCC"})
			if writes != 1 || h.device("223344AABBCC").Node != 0 {
				t.Fatal("expected a connected device waiting after a save failure")
			}
			writes = 0
			h.execute(tc.command)
			h.tick(time.Second)
			if writes != tc.writes {
				t.Fatalf("writes = %d, want %d; retried without a newly freed role or missed a free role", writes, tc.writes)
			}
		})
	}
}

func TestUncertainSaveStopsAppWithoutCommitOrFurtherNotifications(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(fmt.Sprintf("automatic=%t", automatic), func(t *testing.T) {
			h := newHarness(t)
			h.connect("source", testUID, 1)
			h.connect("other", testUID2, 2)
			h.execute(Command{Operation: SetNodeEnabled, Node: 2, Enabled: true})
			h.ready("other")
			h.execute(Command{Operation: SetDevicePreview, DeviceID: testUID})
			if automatic {
				h.must(h.a.Connected(h.ctx, "new"))
			}
			beforeAssignments := h.a.deviceAssignments(nil, false)
			beforeNodes, beforePreview := h.a.game.nodes, *h.a.preview
			h.wantRunError = ErrDeviceSaveUncertain
			h.setSaveDevices(func([]DeviceAssignment) error { return fmt.Errorf("directory sync: %w", ErrDeviceSaveUncertain) })
			h.clear()
			var err error
			if automatic {
				err = h.a.MessageReceived(h.ctx, "new", IncomingMessage{Type: "hello", UID: "223344AABBCC", Kind: PlayerDevice})
			} else {
				err = h.a.Execute(h.ctx, Command{Operation: SwapDeviceAssignments, DeviceID: testUID, OtherDeviceID: testUID2})
			}
			if err == nil {
				t.Fatal("uncertain save returned success")
			}
			select {
			case <-h.a.done:
			case <-h.ctx.Done():
				t.Fatal("uncertain save did not stop app")
			}
			if !slices.Equal(h.a.deviceAssignments(nil, false), beforeAssignments) || !reflect.DeepEqual(h.a.game.nodes, beforeNodes) || h.a.preview == nil || *h.a.preview != beforePreview {
				t.Fatal("uncertain save committed candidate or changed game/preview state")
			}
			if len(h.messages()) != 0 || len(h.closed) != len(h.a.sessions) {
				t.Fatal("sent after uncertain save or failed to close sessions")
			}
			if _, err := h.a.State(context.Background()); !errors.Is(err, ErrStopped) {
				t.Fatalf("stopped app accepted state request: %v", err)
			}
		})
	}
}

func TestUncertainAutomaticSaveDuringCatchUpStopsBeforeRequestedCommand(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.must(h.a.Connected(h.ctx, "pending"))
	h.message("pending", IncomingMessage{Type: "hello", UID: testUID})
	h.wantRunError = ErrDeviceSaveUncertain
	var sentAtFailure int
	h.setSaveDevices(func([]DeviceAssignment) error {
		sentAtFailure = len(h.messages())
		return ErrDeviceSaveUncertain
	})
	h.clock.Store(int64(GameDuration))
	if err := h.a.Execute(h.ctx, Command{Operation: Reset}); err == nil {
		t.Fatal("Reset succeeded after fatal error during time catch-up")
	}
	<-h.a.done
	if h.a.game.phase != Finished || h.a.devices[testUID].node != 0 || len(h.messages()) != sentAtFailure {
		t.Fatal("processed command or sent notifications after fatal catch-up failure")
	}
}
