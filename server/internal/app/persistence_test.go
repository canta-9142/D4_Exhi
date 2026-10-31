package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func (h *harness) setSaveDevices(save func([]SavedDevice) error) {
	h.t.Helper()
	h.must(h.a.submit(h.ctx, false, func(time.Time) error {
		h.a.options.SaveDevices = save
		return nil
	}))
}

func reservedPlayers() []SavedDevice {
	devices := make([]SavedDevice, 0, PlayerCount)
	for node := NodeID(1); node <= PlayerCount; node++ {
		devices = append(devices, SavedDevice{DeviceID: fmt.Sprintf("F%011X", node), Kind: PlayerDevice, Node: node})
	}
	return devices
}

func TestAssignmentsPersistAndRestoreAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	store := &DeviceStore{Path: path}
	saves := 0
	save := func(devices []SavedDevice) error {
		saves++
		return store.Save(devices)
	}
	h := newHarnessWithOptions(t, Options{Mode: Simultaneous, SaveDevices: save})
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
	want := []SavedDevice{
		{testUID, PlayerDevice, 0, true},
		{replacement, PlayerDevice, 2, false},
		{led, LEDDevice, 9, false},
	}
	devices, err := LoadDevices(path)
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
	restored := newHarnessWithOptions(t, Options{Mode: Simultaneous, Devices: devices, SaveDevices: save})
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
	if err := restored.a.MessageReceived(restored.ctx, "wrong-kind", IncomingMessage{Type: "hello", Kind: LEDDevice, UID: replacement, Identification: restored.identification("wrong-kind")}); err == nil {
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
			h := newSimultaneousHarness(t)
			h.connect("source", testUID, 1)
			h.connect("other", testUID2, 2)
			h.execute(Command{Operation: SetNodeEnabled, Node: 2, Enabled: true})
			h.ready("other")
			h.execute(Command{Operation: SetDevicePreview, DeviceID: testUID})
			before := h.state()
			beforeAssignments := h.a.assignmentCandidate(nil, false)
			failure := errors.New("disk full")
			calls := 0
			h.setSaveDevices(func(candidate []SavedDevice) error {
				calls++
				if !slices.Equal(h.a.assignmentCandidate(nil, false), beforeAssignments) || h.a.preview == nil || !h.a.game.nodes[2].ready {
					t.Error("state changed before saving")
				}
				return failure
			})
			h.clear()
			if err := h.a.Execute(h.ctx, command); !errors.Is(err, failure) {
				t.Fatalf("save error lost: %v", err)
			}
			if calls != 1 || !reflect.DeepEqual(h.state(), before) || !slices.Equal(h.a.assignmentCandidate(nil, false), beforeAssignments) || len(h.messages()) != 0 {
				t.Fatal("failed save changed assignment, exclusion, mode, ready, preview or notifications")
			}
		})
	}
}

func TestNotificationFailureKeepsDurableAssignment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	store := &DeviceStore{Path: path}
	h := newHarnessWithOptions(t, Options{Mode: Simultaneous, SaveDevices: store.Save})
	h.connect("player", testUID, 1)
	h.mu.Lock()
	h.failType = "config"
	h.mu.Unlock()
	h.execute(Command{Operation: SetDeviceAssignment, DeviceID: testUID, Node: 2})
	devices, err := LoadDevices(path)
	h.must(err)
	if !slices.Equal(devices, []SavedDevice{{testUID, PlayerDevice, 2, false}}) {
		t.Fatalf("send failure rolled back file: %v", devices)
	}
	if d := h.device(testUID); d.Connected || d.Node != 2 {
		t.Fatalf("send failure rolled back assignment: %+v", d)
	}
}

func TestExclusionOnlyChangeIsPersistedAndIdenticalRequestsDoNotWrite(t *testing.T) {
	h := newHarnessWithOptions(t, Options{Mode: Simultaneous, Devices: reservedPlayers()})
	h.must(h.a.Connected(h.ctx, "spare"))
	h.message("spare", IncomingMessage{Type: "hello", UID: testUID})
	if h.device(testUID).Node != 0 {
		t.Fatal("expected unassigned spare")
	}
	saves := 0
	failed := true
	failure := errors.New("disk full")
	h.setSaveDevices(func(devices []SavedDevice) error {
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
	h := newHarnessWithOptions(t, Options{Mode: Simultaneous, SaveDevices: func([]SavedDevice) error {
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
	h := newHarnessWithOptions(t, Options{Mode: Simultaneous, Devices: devices})
	h.must(h.a.Connected(h.ctx, "spare"))
	h.message("spare", IncomingMessage{Type: "hello", UID: testUID})
	var committed []SavedDevice
	calls := 0
	h.setSaveDevices(func(candidate []SavedDevice) error {
		calls++
		if calls == 2 {
			return errors.New("disk full")
		}
		committed = slices.Clone(candidate)
		return nil
	})
	h.execute(Command{Operation: SetDeviceAssignment, DeviceID: devices[0].DeviceID})
	if calls != 2 || h.device(devices[0].DeviceID).Node != 0 || !h.a.devices[devices[0].DeviceID].noAuto || h.device(testUID).Node != 0 || !slices.Equal(committed, h.a.assignmentCandidate(nil, false)) {
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
			h := newSimultaneousHarness(t)
			h.connect("first", testUID, 1)
			h.connect("second", testUID2, 2)
			writes := 0
			h.setSaveDevices(func(candidate []SavedDevice) error {
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
			h := newSimultaneousHarness(t)
			h.connect("source", testUID, 1)
			h.connect("other", testUID2, 2)
			h.execute(Command{Operation: SetNodeEnabled, Node: 2, Enabled: true})
			h.ready("other")
			h.execute(Command{Operation: SetDevicePreview, DeviceID: testUID})
			if automatic {
				h.must(h.a.Connected(h.ctx, "new"))
			}
			beforeAssignments := h.a.assignmentCandidate(nil, false)
			beforeNodes, beforePreview := h.a.game.nodes, *h.a.preview
			h.wantRunError = ErrSaveUncertain
			h.setSaveDevices(func([]SavedDevice) error { return fmt.Errorf("directory sync: %w", ErrSaveUncertain) })
			h.clear()
			var err error
			if automatic {
				err = h.a.MessageReceived(h.ctx, "new", IncomingMessage{Type: "hello", UID: "223344AABBCC", Kind: PlayerDevice, Identification: h.identification("new")})
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
			if !slices.Equal(h.a.assignmentCandidate(nil, false), beforeAssignments) || !reflect.DeepEqual(h.a.game.nodes, beforeNodes) || h.a.preview == nil || *h.a.preview != beforePreview {
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
	h := newSimultaneousHarness(t)
	h.start()
	h.must(h.a.Connected(h.ctx, "pending"))
	h.message("pending", IncomingMessage{Type: "hello", UID: testUID})
	h.wantRunError = ErrSaveUncertain
	var sentAtFailure int
	h.setSaveDevices(func([]SavedDevice) error {
		sentAtFailure = len(h.messages())
		return ErrSaveUncertain
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

func TestDeviceFileRoundTripAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "nested", "devices.json")
	if devices, err := LoadDevices(path); err != nil || len(devices) != 0 {
		t.Fatalf("missing file: %v, %v", devices, err)
	}
	input := []SavedDevice{
		{"AABBCCDDEEFF", PlayerDevice, 1, false},
		{"001122334455", LEDDevice, 1, false}, // Same node, different kind.
		{"112233445566", PlayerDevice, 0, true},
		{"FFFFFFFFFFFF", LEDDevice, 0, false}, // Not saved.
	}
	if err := saveDeviceFixture(path, input); err != nil {
		t.Fatal(err)
	}
	want := []SavedDevice{input[1], input[2], input[0]}
	got, err := LoadDevices(path)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip: %+v, %v; want %+v", got, err, want)
	}
	if err := saveDeviceFixture(path, nil); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"devices": []`) {
		t.Fatalf("empty table is not an array: %s", data)
	}

	const entry = `{"deviceId":"AABBCCDDEEFF","kind":"player","nodeId":"P1","autoAssignExcluded":false}`
	file := func(entries string) string { return `{"version":1,"devices":[` + entries + `]}` }
	invalid := []string{
		"", "null", "{}", `{"version":2,"devices":[]}`, `{"version":"1","devices":[]}`,
		`{"version":1,"devices":null}`, `{"devices":[]}`, `{"version":1}`,
		`{"Version":1,"devices":[]}`, file(strings.Replace(entry, "deviceId", "DeviceId", 1)),
		file("null"), file(strings.Replace(entry, `"deviceId":"AABBCCDDEEFF",`, "", 1)),
		file(strings.Replace(entry, `"kind":"player",`, "", 1)),
		file(strings.Replace(entry, `"nodeId":"P1",`, "", 1)),
		file(strings.Replace(entry, `false`, `null`, 1)),
		file(strings.Replace(entry, `false`, `0`, 1)),
		file(strings.Replace(entry, `AABBCCDDEEFF`, `aabbccddeeff`, 1)),
		file(strings.Replace(entry, `"player"`, `"unknown"`, 1)),
		file(strings.Replace(entry, `"P1"`, `"B1"`, 1)),
		file(strings.Replace(entry, `"P1"`, `1`, 1)),
		file(entry + "," + entry),
		file(entry + "," + strings.Replace(entry, "AABBCCDDEEFF", "112233445566", 1)),
		file(entry) + "{}", string([]byte{0xff}),
	}
	for _, text := range invalid {
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		if devices, err := LoadDevices(path); err == nil || devices != nil {
			t.Errorf("invalid file accepted/partially restored: %q => %+v, %v", text, devices, err)
		}
	}
	if _, err := LoadDevices(filepath.Dir(path)); err == nil {
		t.Fatal("directory accepted as a device file")
	}
}

func TestDeviceFileFailuresBeforeAndAfterRename(t *testing.T) {
	devices := []SavedDevice{{testUID, PlayerDevice, 1, false}}
	for _, afterRename := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-rename", true: "after-rename"}[afterRename], func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "new", "devices.json")
			if afterRename {
				if err := saveDeviceFixture(path, nil); err != nil {
					t.Fatal(err)
				}
			}
			failure := errors.New("directory sync failed")
			store := &DeviceStore{Path: path}
			err := store.save(devices, func(dir string) error {
				if !afterRename || dir == filepath.Dir(path) {
					return failure
				}
				return nil
			})
			if !errors.Is(err, failure) || errors.Is(err, ErrSaveUncertain) != afterRename {
				t.Fatalf("save failure classification: %v", err)
			}
			got, err := LoadDevices(path)
			if err != nil || (afterRename && !reflect.DeepEqual(got, devices)) || (!afterRename && len(got) != 0) {
				t.Fatalf("unexpected file after failure: %+v, %v", got, err)
			}
			leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".devices-*"))
			if err != nil || len(leftovers) != 0 {
				t.Fatalf("temporary files left behind: %v, %v", leftovers, err)
			}
		})
	}
	// A failed rename must leave the existing target intact, including when it
	// is not a file. No temporary file can become the restore source.
	path := filepath.Join(t.TempDir(), "devices.json")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := saveDeviceFixture(path, devices); err == nil || errors.Is(err, ErrSaveUncertain) {
		t.Fatalf("rename onto directory: %v", err)
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Fatalf("failed rename changed target: %v", err)
	}
}

func setSave(t *testing.T, h *harness, save func([]SavedDevice) error) {
	t.Helper()
	h.must(h.a.submit(h.ctx, false, func(time.Time) error { h.a.options.SaveDevices = save; return nil }))
}

func TestAssignmentsAreSavedBeforeAnyObservableChange(t *testing.T) {
	h := newSimultaneousHarness(t)
	h.connect("p1", testUID, 1)
	h.connect("p2", "112233445566", 2)
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
	h.ready("p1")
	h.execute(Command{Operation: SetDevicePreview, DeviceID: "112233445566"})
	before := h.state()
	h.clear()
	failure := errors.New("disk write failed")
	setSave(t, h, func([]SavedDevice) error { return failure })
	swap := Command{Operation: SwapDeviceAssignments, DeviceID: testUID, OtherDeviceID: "112233445566"}
	if err := h.a.Execute(h.ctx, swap); !errors.Is(err, failure) {
		t.Fatalf("failed swap: %v", err)
	}
	if got := h.state(); !reflect.DeepEqual(got, before) || len(h.messages()) != 0 {
		t.Fatal("save failure changed assignments, manual/ready, preview or MCU output")
	}
	path := filepath.Join(t.TempDir(), "devices.json")
	writes := 0
	setSave(t, h, func(candidate []SavedDevice) error {
		writes++
		if h.a.devices[testUID].node != 1 || h.a.preview == nil || !h.a.game.nodes[1].manual || !h.a.game.nodes[1].ready {
			t.Error("assignment side effects occurred before save")
		}
		return saveDeviceFixture(path, candidate)
	})
	h.execute(swap)
	if writes != 1 || h.state().Preview != nil {
		t.Fatal("swap did not save exactly once and clear affected preview")
	}
	setSave(t, h, func(candidate []SavedDevice) error { writes++; return saveDeviceFixture(path, candidate) })
	h.execute(Command{Operation: SetDeviceAssignment, DeviceID: testUID, Kind: PlayerDevice, Node: 2})
	if writes != 1 {
		t.Fatal("same assignment unnecessarily saved")
	}
	h.execute(Command{Operation: SetDeviceAssignment, DeviceID: testUID})
	h.execute(Command{Operation: SetDeviceAssignment, DeviceID: testUID, Node: 2})
	devices, err := LoadDevices(path)
	if err != nil || len(devices) != 2 || !devices[0].AutoAssignExcluded {
		t.Fatalf("exclusion was lost after explicit reassignment: %+v, %v", devices, err)
	}
	restored := New(Options{Devices: devices})
	if restored.devices[testUID].session != "" || restored.devices[testUID].node != 2 || restored.game.nodes[2].manual || restored.game.nodes[2].ready || restored.preview != nil {
		t.Fatal("restoration included transient connection/game/display state")
	}
}

func TestAutomaticAssignmentRetriesAfterSaveFailureOnReset(t *testing.T) {
	h := newSimultaneousHarness(t)
	setSave(t, h, func([]SavedDevice) error { return errors.New("disk full") })
	h.must(h.a.Connected(h.ctx, "p1"))
	h.clear()
	h.message("p1", IncomingMessage{Type: "hello", UID: testUID})
	device := h.state().Devices[0]
	if !device.Connected || device.Node != 0 || len(h.messages()) != 0 {
		t.Fatal("failed automatic assignment was applied or device disconnected")
	}
	writes := 0
	setSave(t, h, func([]SavedDevice) error { writes++; return nil })
	h.execute(Command{Operation: Reset})
	if writes != 1 || h.state().Devices[0].Node != 1 {
		t.Fatal("automatic assignment did not retry after storage recovered")
	}
}

func TestUncertainSaveStopsAppWithoutApplyingOrSending(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(map[bool]string{false: "manual", true: "automatic"}[automatic], func(t *testing.T) {
			var sent []Message
			closed := 0
			devices := []SavedDevice{{testUID, PlayerDevice, 1, false}}
			if automatic {
				devices = nil
			}
			a := New(Options{
				Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Devices: devices,
				Send:        func(_ SessionID, m Message) error { sent = append(sent, m); return nil },
				Close:       func(SessionID) { closed++ },
				SaveDevices: func([]SavedDevice) error { return ErrSaveUncertain },
			})
			a.ticks = make(chan time.Time)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- a.Run(ctx) }()
			if err := a.Connected(ctx, "p1"); err != nil {
				t.Fatal(err)
			}
			err := a.MessageReceived(ctx, "p1", IncomingMessage{Type: "hello", Kind: PlayerDevice, UID: testUID, Identification: 1})
			before := 1 // Only the initial identify is sent on automatic failure.
			if !automatic {
				if err != nil {
					t.Fatal(err)
				}
				before = len(sent)
				err = a.Execute(ctx, Command{Operation: SetDeviceAssignment, DeviceID: testUID})
			}
			if !errors.Is(err, ErrStopped) {
				t.Fatalf("uncertain operation returned %v; want app stopped", err)
			}
			select {
			case err := <-done:
				if !errors.Is(err, ErrSaveUncertain) {
					t.Fatalf("Run did not report storage failure: %v", err)
				}
			case <-ctx.Done():
				t.Fatal("app did not stop on uncertain save")
			}
			wantNode := NodeID(1)
			if automatic {
				wantNode = 0
			}
			if a.devices[testUID].node != wantNode || a.devices[testUID].noAuto || len(sent) != before || closed != 1 {
				t.Fatal("uncertain save applied candidate, sent more messages or failed to close")
			}
		})
	}
}

func saveDeviceFixture(path string, devices []SavedDevice) error {
	store := &DeviceStore{Path: path}
	return store.Save(devices)
}
