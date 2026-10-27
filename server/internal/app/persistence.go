package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

// ErrSaveUncertain means rename succeeded but directory synchronization failed.
// The process must stop without applying the candidate or restoring the old file.
var ErrSaveUncertain = errors.New("device assignment save outcome is uncertain")

// SavedDevice excludes connection, game and display state. Node zero is unassigned.
type SavedDevice struct {
	DeviceID           string
	Kind               DeviceKind
	Node               NodeID
	AutoAssignExcluded bool
}

type deviceFile struct {
	Version *int               `json:"version"`
	Devices *[]json.RawMessage `json:"devices"`
}

type deviceEntry struct {
	DeviceID           *string         `json:"deviceId"`
	Kind               *DeviceKind     `json:"kind"`
	NodeID             json.RawMessage `json:"nodeId"`
	AutoAssignExcluded *bool           `json:"autoAssignExcluded"`
}

func decodeDeviceJSON(data []byte, target any, required ...string) error {
	if !utf8.Valid(data) {
		return errors.New("device file is not UTF-8")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if len(fields) != len(required) {
		return errors.New("missing or unknown device file fields")
	}
	for _, name := range required {
		if _, ok := fields[name]; !ok {
			return fmt.Errorf("missing device file field %q", name)
		}
	}
	return json.Unmarshal(data, target)
}

// LoadDevices validates the entire file before returning any records. Only a
// missing file is an empty initial state; an invalid/unreadable file is an error.
func LoadDevices(path string) ([]SavedDevice, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var file deviceFile
	if err := decodeDeviceJSON(data, &file, "version", "devices"); err != nil {
		return nil, err
	}
	if file.Version == nil || *file.Version != 1 || file.Devices == nil {
		return nil, errors.New("invalid device file version or devices array")
	}
	devices := make([]SavedDevice, 0, len(*file.Devices))
	for _, raw := range *file.Devices {
		var entry deviceEntry
		if err := decodeDeviceJSON(raw, &entry, "deviceId", "kind", "nodeId", "autoAssignExcluded"); err != nil {
			return nil, err
		}
		if entry.DeviceID == nil || entry.Kind == nil || entry.AutoAssignExcluded == nil || len(entry.NodeID) == 0 {
			return nil, errors.New("missing or null device field")
		}
		var node NodeID
		if !bytes.Equal(bytes.TrimSpace(entry.NodeID), []byte("null")) {
			var name string
			if err := json.Unmarshal(entry.NodeID, &name); err != nil {
				return nil, err
			}
			for id := NodeID(1); id <= NodeCount; id++ {
				if id.wireName() == name {
					node = id
					break
				}
			}
			if node == 0 {
				return nil, fmt.Errorf("invalid saved node %q", name)
			}
		}
		devices = append(devices, SavedDevice{*entry.DeviceID, *entry.Kind, node, *entry.AutoAssignExcluded})
	}
	if err := validateSavedDevices(devices); err != nil {
		return nil, err
	}
	return devices, nil
}

func validateSavedDevices(devices []SavedDevice) error {
	type assignment struct {
		kind DeviceKind
		node NodeID
	}
	uids := make(map[string]bool, len(devices))
	assigned := make(map[assignment]bool)
	for _, d := range devices {
		key := assignment{d.Kind, d.Node}
		if !validIdentity(d.Kind, d.DeviceID) || (d.Node != 0 && !validAssignment(d.Kind, d.Node)) {
			return fmt.Errorf("invalid saved device %q", d.DeviceID)
		}
		if uids[d.DeviceID] || (d.Node != 0 && assigned[key]) {
			return fmt.Errorf("duplicate saved device or assignment %q", d.DeviceID)
		}
		uids[d.DeviceID], assigned[key] = true, true
	}
	return nil
}

// SaveDevices durably replaces one complete table. All failures before rename
// leave the old file intact. Records are emitted in Device ID order.
func SaveDevices(path string, devices []SavedDevice) error {
	return saveDevices(path, devices, syncDirectory)
}

func saveDevices(path string, devices []SavedDevice, syncDir func(string) error) error {
	if err := validateSavedDevices(devices); err != nil {
		return err
	}
	devices = slices.Clone(devices)
	slices.SortFunc(devices, func(a, b SavedDevice) int { return strings.Compare(a.DeviceID, b.DeviceID) })
	entries := make([]deviceEntry, 0, len(devices))
	for _, d := range devices {
		if d.Node == 0 && !d.AutoAssignExcluded {
			continue
		}
		node := json.RawMessage("null")
		if d.Node != 0 {
			node, _ = json.Marshal(d.Node.wireName())
		}
		entries = append(entries, deviceEntry{&d.DeviceID, &d.Kind, node, &d.AutoAssignExcluded})
	}
	data, err := json.MarshalIndent(struct {
		Version int           `json:"version"`
		Devices []deviceEntry `json:"devices"`
	}{1, entries}, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := makeDeviceDirectory(dir, syncDir); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".devices-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("%w: %w", ErrSaveUncertain, err)
	}
	return nil
}

// Sync ancestor entries even on retries: a previous failed save may have
// created directories without completing their synchronization.
func makeDeviceDirectory(dir string, syncDir func(string) error) error {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	for parent := filepath.Dir(dir); parent != dir; parent = filepath.Dir(dir) {
		if err := syncDir(parent); err != nil {
			return err
		}
		dir = parent
	}
	return nil
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// assignmentCandidate is built before any mode, preview or in-memory change.
func (a *App) assignmentCandidate(updates map[string]NodeID, explicit bool) []SavedDevice {
	devices := make([]SavedDevice, 0, len(a.devices))
	for uid, d := range a.devices {
		node, changed := updates[uid]
		if !changed {
			node = d.node
		}
		excluded := d.noAuto || (changed && explicit && node == 0)
		if node != 0 || excluded {
			devices = append(devices, SavedDevice{uid, d.kind, node, excluded})
		}
	}
	slices.SortFunc(devices, func(a, b SavedDevice) int { return strings.Compare(a.DeviceID, b.DeviceID) })
	return devices
}
