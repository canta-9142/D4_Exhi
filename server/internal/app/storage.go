package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

// ErrDeviceSaveUncertain means replacement succeeded but directory sync failed.
// The process must stop without committing the candidate or rolling back the file.
var ErrDeviceSaveUncertain = errors.New("device assignment persistence is uncertain")

// DeviceAssignment is the durable part of an identified device. Node zero is
// unassigned; AutoAssignExcluded survives explicit reassignment as well as Reset.
type DeviceAssignment struct {
	DeviceID           string
	Kind               DeviceKind
	Node               NodeID
	AutoAssignExcluded bool
}

// LoadDeviceAssignments validates the entire file before returning any records.
// Only a nonexistent file is treated as an empty initial mapping.
func LoadDeviceAssignments(path string) ([]DeviceAssignment, error) {
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []DeviceAssignment{}, nil
	}
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(body) {
		return nil, errors.New("device file is not UTF-8")
	}
	var file map[string]json.RawMessage
	if err := json.Unmarshal(body, &file); err != nil {
		return nil, err
	}
	var version int
	if err := json.Unmarshal(file["version"], &version); err != nil || version != 1 {
		return nil, errors.New("device file requires version 1")
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(file["devices"], &rows); err != nil || rows == nil {
		return nil, errors.New("device file requires a devices array")
	}
	result := make([]DeviceAssignment, 0, len(rows))
	uids := make(map[string]bool)
	type assignmentKey struct {
		kind DeviceKind
		node NodeID
	}
	assignments := make(map[assignmentKey]bool)
	for i, row := range rows {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(row, &fields); err != nil {
			return nil, fmt.Errorf("device %d: %w", i, err)
		}
		for _, name := range []string{"deviceId", "kind", "nodeId", "autoAssignExcluded"} {
			if len(fields[name]) == 0 {
				return nil, fmt.Errorf("device %d: missing %s", i, name)
			}
		}
		var uid, nodeName *string
		var kind *DeviceKind
		var excluded *bool
		if err := errors.Join(
			json.Unmarshal(fields["deviceId"], &uid),
			json.Unmarshal(fields["kind"], &kind),
			json.Unmarshal(fields["nodeId"], &nodeName),
			json.Unmarshal(fields["autoAssignExcluded"], &excluded),
		); err != nil {
			return nil, fmt.Errorf("device %d: %w", i, err)
		}
		if uid == nil || kind == nil || excluded == nil || !validIdentity(*kind, *uid) {
			return nil, fmt.Errorf("device %d: invalid identity or excluded flag", i)
		}
		d := DeviceAssignment{DeviceID: *uid, Kind: *kind, AutoAssignExcluded: *excluded}
		if nodeName != nil {
			for node := NodeID(1); node <= NodeCount; node++ {
				if node.wireName() == *nodeName && validAssignment(d.Kind, node) {
					d.Node = node
					break
				}
			}
			if d.Node == 0 {
				return nil, fmt.Errorf("device %d: invalid node %q for %s", i, *nodeName, d.Kind)
			}
		}
		if uids[d.DeviceID] {
			return nil, fmt.Errorf("duplicate device ID %s", d.DeviceID)
		}
		uids[d.DeviceID] = true
		if d.Node != 0 {
			key := assignmentKey{d.Kind, d.Node}
			if assignments[key] {
				return nil, fmt.Errorf("duplicate %s assignment %s", d.Kind, d.Node.wireName())
			}
			assignments[key] = true
		}
		result = append(result, d)
	}
	slices.SortFunc(result, func(a, b DeviceAssignment) int { return strings.Compare(a.DeviceID, b.DeviceID) })
	return result, nil
}

// DeviceStore resolves one save path on first use and remembers directory
// creation whose parent sync has not succeeded yet. Set Path before use;
// Load and Save must be called serially on the same instance.
type DeviceStore struct {
	Path           string
	resolvedPath   string
	pendingParents []string
}

// Load fixes the target path before validating the complete assignment file.
func (s *DeviceStore) Load() ([]DeviceAssignment, error) {
	path, err := s.filePath()
	if err != nil {
		return nil, err
	}
	return LoadDeviceAssignments(path)
}

func (s *DeviceStore) filePath() (string, error) {
	if s.resolvedPath != "" {
		return s.resolvedPath, nil
	}
	_, name := filepath.Split(s.Path)
	if name == "" || name == "." || name == ".." {
		return "", fmt.Errorf("device path %q requires a file name", s.Path)
	}
	path, err := resolveDevicePath(s.Path)
	if err != nil {
		return "", err
	}
	s.resolvedPath = path
	return path, nil
}

// Resolve existing symlinks before cleaning '..'. For a first-run path, resolve
// the existing prefix and append the missing components without creating them.
func resolveDevicePath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		// Join would clean '..' before symlinks in cwd or path were resolved.
		path = cwd + string(filepath.Separator) + path
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	trimmed := strings.TrimRight(path, string(filepath.Separator))
	dir, name := filepath.Split(trimmed)
	if target, linkErr := os.Readlink(trimmed); linkErr == nil {
		if !filepath.IsAbs(target) {
			target = dir + target
		}
		return resolveDevicePath(target)
	}
	if name == "" || name == "." {
		return "", err
	}
	if dir == "" {
		dir = "."
	}
	parent, err := resolveDevicePath(dir)
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, name), nil
}

// Save writes assignments and automatic-assignment exclusions in Device ID
// order, using a synced temporary file and atomic rename.
func (s *DeviceStore) Save(devices []DeviceAssignment) error {
	type entry struct {
		DeviceID           string     `json:"deviceId"`
		Kind               DeviceKind `json:"kind"`
		NodeID             *string    `json:"nodeId"`
		AutoAssignExcluded bool       `json:"autoAssignExcluded"`
	}
	file := struct {
		Version int     `json:"version"`
		Devices []entry `json:"devices"`
	}{Version: 1, Devices: make([]entry, 0, len(devices))}
	for _, d := range devices {
		if d.Node == 0 && !d.AutoAssignExcluded {
			continue
		}
		var node *string
		if d.Node != 0 {
			name := d.Node.wireName()
			node = &name
		}
		file.Devices = append(file.Devices, entry{d.DeviceID, d.Kind, node, d.AutoAssignExcluded})
	}
	slices.SortFunc(file.Devices, func(a, b entry) int { return strings.Compare(a.DeviceID, b.DeviceID) })
	body, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return s.writeDeviceFile(append(body, '\n'), syncDeviceDirectory)
}

func syncDeviceDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

// syncDir is passed explicitly so tests can exercise a failed directory fsync
// before and after replacement without changing process-wide filesystem hooks.
func (s *DeviceStore) writeDeviceFile(body []byte, syncDir func(string) error) error {
	path, err := s.filePath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := s.prepareDirectory(dir, syncDir); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	defer func() {
		temp.Close()
		os.Remove(temp.Name())
	}()
	if _, err := temp.Write(body); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return err
	}
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("%w: sync device directory: %w", ErrDeviceSaveUncertain, err)
	}
	return nil
}

func (s *DeviceStore) prepareDirectory(dir string, syncDir func(string) error) error {
	var missing []string
	for current := dir; ; current = filepath.Dir(current) {
		info, err := os.Stat(current)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("device directory %s is not a directory", current)
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) || filepath.Dir(current) == current {
			return err
		}
		missing = append(missing, current)
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], 0755); err != nil {
			return err
		}
		s.pendingParents = append(s.pendingParents, filepath.Dir(missing[i]))
	}
	// A failed creation sync is retried even though the directory now exists.
	// Successfully synced parents and unrelated ancestors need no further sync.
	for len(s.pendingParents) > 0 {
		if err := syncDir(s.pendingParents[0]); err != nil {
			return fmt.Errorf("sync device directory parent: %w", err)
		}
		s.pendingParents = s.pendingParents[1:]
	}
	return nil
}
