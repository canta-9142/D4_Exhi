package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

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
	if err := SaveDevices(path, input); err != nil {
		t.Fatal(err)
	}
	want := []SavedDevice{input[1], input[2], input[0]}
	got, err := LoadDevices(path)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip: %+v, %v; want %+v", got, err, want)
	}
	if err := SaveDevices(path, nil); err != nil {
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
				if err := SaveDevices(path, nil); err != nil {
					t.Fatal(err)
				}
			}
			failure := errors.New("directory sync failed")
			err := saveDevices(path, devices, func(dir string) error {
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
	if err := SaveDevices(path, devices); err == nil || errors.Is(err, ErrSaveUncertain) {
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
	h := newHarness(t)
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
		return SaveDevices(path, candidate)
	})
	h.execute(swap)
	if writes != 1 || h.state().Preview != nil {
		t.Fatal("swap did not save exactly once and clear affected preview")
	}
	setSave(t, h, func(candidate []SavedDevice) error { writes++; return SaveDevices(path, candidate) })
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

func TestAutomaticAssignmentRetriesAfterSaveFailure(t *testing.T) {
	h := newHarness(t)
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
	h.tick(0)
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
