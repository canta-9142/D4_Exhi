package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDeviceFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "nested", "devices.json")
	store := &DeviceStore{Path: path}
	if devices, err := LoadDeviceAssignments(path); err != nil || len(devices) != 0 {
		t.Fatalf("missing file: %v, %v", devices, err)
	}
	want := []DeviceAssignment{
		{"001122334455", LEDDevice, 9, false},
		{"112233445566", PlayerDevice, 0, true},
		{"AABBCCDDEEFF", PlayerDevice, 1, true},
	}
	input := []DeviceAssignment{want[2], want[0], want[1], {"FFFFFFFFFFFF", PlayerDevice, 0, false}}
	if err := store.Save(input); err != nil {
		t.Fatal(err)
	}
	got, err := LoadDeviceAssignments(path)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip: %v, %v", got, err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON := `{"version":1,"devices":[{"deviceId":"001122334455","kind":"led","nodeId":"B1","autoAssignExcluded":false},{"deviceId":"112233445566","kind":"player","nodeId":null,"autoAssignExcluded":true},{"deviceId":"AABBCCDDEEFF","kind":"player","nodeId":"P1","autoAssignExcluded":true}]}`
	// Compare decoded JSON so formatting is not part of the storage contract.
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	var expected any
	if err := json.Unmarshal([]byte(wantJSON), &expected); err != nil || !reflect.DeepEqual(decoded, expected) {
		t.Fatalf("unexpected persisted fields: %s", body)
	}
	// A leftover temporary file must never be considered for restoration.
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), ".devices.json-crash"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDeviceAssignments(path); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(nil); err != nil {
		t.Fatal(err)
	}
	if devices, err := LoadDeviceAssignments(path); err != nil || devices == nil || len(devices) != 0 {
		t.Fatalf("empty mapping: %v, %v", devices, err)
	}
}

func TestDeviceFileRejectsInvalidDocumentsWithoutPartialRestoration(t *testing.T) {
	valid := `{"deviceId":"001122AABBCC","kind":"player","nodeId":"P1","autoAssignExcluded":false}`
	document := func(row string) string { return `{"version":1,"devices":[` + row + `]}` }
	invalid := []string{
		"", "null", "[]", "{", "{}", `{"version":1}`, `{"devices":[]}`,
		`{"version":null,"devices":[]}`, `{"version":"1","devices":[]}`,
		`{"version":1.5,"devices":[]}`, `{"version":2,"devices":[]}`,
		`{"version":1,"devices":null}`, `{"version":1,"devices":{}}`,
		`{"version":1,"devices":[]} {}`, `{"Version":1,"devices":[]}`,
		document("null"), document("[]"), document("{}"),
		document(valid + "," + valid),
		document(valid + "," + strings.ReplaceAll(valid, "001122AABBCC", "112233AABBCC")),
		document(valid + "," + strings.ReplaceAll(valid, `"player"`, `"led"`)),
		document(strings.ReplaceAll(valid, `"deviceId":"001122AABBCC",`, "")),
		document(strings.ReplaceAll(valid, `"kind":"player",`, "")),
		document(strings.ReplaceAll(valid, `"nodeId":"P1",`, "")),
		document(strings.ReplaceAll(valid, `,"autoAssignExcluded":false`, "")),
		document(strings.ReplaceAll(valid, `"deviceId"`, `"DeviceId"`)),
		document(strings.ReplaceAll(valid, `"001122AABBCC"`, `null`)),
		document(strings.ReplaceAll(valid, `"deviceId":"001122AABBCC"`, `"deviceId":null,"DeviceID":"001122AABBCC"`)),
		document(strings.ReplaceAll(valid, `"player"`, `null`)),
		document(strings.ReplaceAll(valid, `false`, `null`)),
		document(strings.ReplaceAll(valid, `false`, `"false"`)),
		document(strings.ReplaceAll(valid, `"001122AABBCC"`, `1122`)),
		document(strings.ReplaceAll(valid, `"001122AABBCC"`, `"001122aabbcc"`)),
		document(strings.ReplaceAll(valid, `"001122AABBCC"`, `"1122"`)),
		document(strings.ReplaceAll(valid, `"player"`, `"unknown"`)),
		document(strings.ReplaceAll(valid, `"P1"`, `1`)),
		document(strings.ReplaceAll(valid, `"P1"`, `"P9"`)),
		document(strings.ReplaceAll(valid, `"P1"`, `"B1"`)),
		document(strings.ReplaceAll(strings.ReplaceAll(valid, `"player"`, `"led"`), `"P1"`, `"P2"`)),
		document(strings.ReplaceAll(valid, `"001122AABBCC"`, "\"\xff\"")),
	}
	path := filepath.Join(t.TempDir(), "devices.json")
	for _, body := range invalid {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if got, err := LoadDeviceAssignments(path); err == nil || got != nil {
			t.Errorf("accepted %q: %v, %v", body, got, err)
		}
		after, err := os.ReadFile(path)
		if err != nil || string(after) != body {
			t.Fatalf("load modified invalid document: %s, %v", after, err)
		}
	}
	if _, err := LoadDeviceAssignments(filepath.Dir(path)); err == nil {
		t.Fatal("directory treated as missing file")
	}
	// The same role is allowed once for each kind, but never twice per kind.
	otherKind := strings.ReplaceAll(strings.ReplaceAll(valid, `"player"`, `"led"`), "001122AABBCC", "112233AABBCC")
	if err := os.WriteFile(path, []byte(document(valid+","+otherKind)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDeviceAssignments(path); err != nil {
		t.Fatal(err)
	}
}

func TestDeviceFileFailureBeforeAndAfterReplacement(t *testing.T) {
	failure := errors.New("directory sync failed")
	for _, afterRename := range []bool{false, true} {
		t.Run(map[bool]string{false: "before rename", true: "after rename"}[afterRename], func(t *testing.T) {
			dir := t.TempDir()
			if !afterRename {
				dir = filepath.Join(dir, "new", "nested")
			}
			path := filepath.Join(dir, "devices.json")
			store := &DeviceStore{Path: path}
			if afterRename {
				if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := store.writeDeviceFile([]byte("new"), func(syncPath string) error {
				if !afterRename || syncPath == dir {
					return failure
				}
				return syncDeviceDirectory(syncPath)
			})
			if !errors.Is(err, failure) || errors.Is(err, ErrDeviceSaveUncertain) != afterRename {
				t.Fatalf("wrong failure classification: %v", err)
			}
			body, err := os.ReadFile(path)
			if !afterRename && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("file created before directory sync completed: %s, %v", body, err)
			}
			if afterRename && (err != nil || string(body) != "new") {
				t.Fatalf("file rolled back or damaged: %q, %v", body, err)
			}
			leftovers, err := filepath.Glob(filepath.Join(dir, ".devices.json-*"))
			if err != nil || len(leftovers) != 0 {
				t.Fatalf("temporary files not cleaned: %v, %v", leftovers, err)
			}
		})
	}
}

func TestDeviceFileDirectoryDurabilityAndRenameFailure(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "new", "nested")
	path := filepath.Join(dir, "devices.json")
	store := &DeviceStore{Path: path}
	var synced []string
	if err := store.writeDeviceFile([]byte("new"), func(path string) error {
		synced = append(synced, path)
		return syncDeviceDirectory(path)
	}); err != nil {
		t.Fatal(err)
	}
	for _, parent := range []string{root, filepath.Join(root, "new"), dir} {
		found := false
		for _, path := range synced {
			found = found || path == parent
		}
		if !found {
			t.Fatalf("directory creation not synced: %s, calls %v", parent, synced)
		}
	}
	// A directory in place of the target makes rename fail after the temp file
	// was fully written. It must be preserved and must not be a fatal sync error.
	target := filepath.Join(dir, "occupied")
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatal(err)
	}
	targetStore := &DeviceStore{Path: target}
	err := targetStore.writeDeviceFile([]byte("new"), syncDeviceDirectory)
	if err == nil || errors.Is(err, ErrDeviceSaveUncertain) {
		t.Fatalf("rename failure: %v", err)
	}
	if stat, err := os.Stat(target); err != nil || !stat.IsDir() {
		t.Fatalf("rename replaced target: %v, %v", stat, err)
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, ".occupied-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("rename failure left temp file: %v, %v", leftovers, err)
	}
}

func TestExistingDeviceDirectoryDoesNotSyncAncestors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "devices.json")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	store := &DeviceStore{Path: path}
	syncs := 0
	if err := store.writeDeviceFile([]byte("new"), func(path string) error {
		syncs++
		if path != dir {
			return errors.New("unrelated ancestor does not support directory fsync")
		}
		return syncDeviceDirectory(path)
	}); err != nil {
		t.Fatalf("save depended on an unrelated ancestor: %v", err)
	}
	if syncs != 1 {
		t.Fatalf("existing directory synced %d times, want once after replacement", syncs)
	}
	if body, err := os.ReadFile(path); err != nil || string(body) != "new" {
		t.Fatalf("file not replaced: %s, %v", body, err)
	}
}

func TestFailedDirectoryCreationSyncIsRetried(t *testing.T) {
	for _, failedParent := range []string{"root", "new"} {
		t.Run(failedParent, func(t *testing.T) {
			root := t.TempDir()
			parent := filepath.Join(root, "new")
			dir := filepath.Join(parent, "nested")
			path := filepath.Join(dir, "devices.json")
			store := &DeviceStore{Path: path}
			failurePath := root
			if failedParent == "new" {
				failurePath = parent
			}
			failure := errors.New("creation directory sync failed")
			err := store.writeDeviceFile([]byte("first"), func(path string) error {
				if path == failurePath {
					return failure
				}
				return syncDeviceDirectory(path)
			})
			if !errors.Is(err, failure) || errors.Is(err, ErrDeviceSaveUncertain) {
				t.Fatalf("creation sync failure misclassified: %v", err)
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("created assignment file before parent sync: %v", err)
			}
			var synced []string
			syncDir := func(path string) error {
				synced = append(synced, path)
				return syncDeviceDirectory(path)
			}
			if err := store.writeDeviceFile([]byte("retry"), syncDir); err != nil {
				t.Fatal(err)
			}
			want := []string{parent, dir}
			if failedParent == "root" {
				want = append([]string{root}, want...)
			}
			if !reflect.DeepEqual(synced, want) {
				t.Fatalf("retry did not sync exactly the remaining parents: %v, want %v", synced, want)
			}
			synced = nil
			if err := store.writeDeviceFile([]byte("next"), syncDir); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(synced, []string{dir}) {
				t.Fatalf("completed parent sync was repeated: %v", synced)
			}
		})
	}
}

func TestDeviceStoreLoadAndSaveUseSameResolvedPath(t *testing.T) {
	for _, tc := range []struct {
		name     string
		existing bool
		suffix   string
	}{
		{"existing file", true, "devices.json"},
		{"missing file", false, "devices.json"},
		{"missing parents", false, "new/nested/devices.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			actual := filepath.Join(root, "actual")
			if err := os.MkdirAll(filepath.Join(actual, "inner"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(actual, "inner"), filepath.Join(root, "link")); err != nil {
				t.Fatal(err)
			}
			path := root + "/link/../" + tc.suffix
			target := filepath.Join(actual, tc.suffix)
			initial := []DeviceAssignment{}
			if tc.existing {
				initial = append(initial, DeviceAssignment{DeviceID: testUID, Kind: PlayerDevice, Node: 1})
				seed := &DeviceStore{Path: target}
				if err := seed.Save(initial); err != nil {
					t.Fatal(err)
				}
			}
			store := &DeviceStore{Path: path}
			got, err := store.Load()
			if err != nil || !reflect.DeepEqual(got, initial) {
				t.Fatalf("loaded a different file: %v, %v", got, err)
			}
			if tc.name == "missing parents" {
				if _, err := os.Stat(filepath.Dir(target)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("Load created missing parents: %v", err)
				}
			}
			want := []DeviceAssignment{{DeviceID: testUID, Kind: PlayerDevice, Node: 2}}
			if err := store.Save(want); err != nil {
				t.Fatal(err)
			}
			restarted := &DeviceStore{Path: path}
			got, err = restarted.Load()
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("successful save lost after restart: %v, %v", got, err)
			}
			got, err = LoadDeviceAssignments(target)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("save missed original filesystem target: %v, %v", got, err)
			}
			if _, err := os.Stat(filepath.Join(root, tc.suffix)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("save also created the lexically cleaned target: %v", err)
			}
		})
	}
}

func TestDeviceStoreResolvesRelativePathFromSymlinkedWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	actual := filepath.Join(root, "actual")
	if err := os.MkdirAll(filepath.Join(actual, "inner"), 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(filepath.Join(actual, "inner"), link); err != nil {
		t.Fatal(err)
	}
	seed := &DeviceStore{Path: filepath.Join(actual, "devices.json")}
	want := []DeviceAssignment{{DeviceID: testUID, Kind: PlayerDevice, Node: 1}}
	if err := seed.Save(want); err != nil {
		t.Fatal(err)
	}
	t.Chdir(link)
	t.Setenv("PWD", link)
	store := &DeviceStore{Path: "../devices.json"}
	got, err := store.Load()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("logical cwd changed parent path: %v, %v", got, err)
	}
	want[0].Node = 2
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	if got, err := seed.Load(); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("relative save missed original target: %v, %v", got, err)
	}
}

func TestDeviceStoreKeepsTargetWhenWorkingDirectoryChanges(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	t.Chdir(root)
	store := &DeviceStore{Path: "new/devices.json"}
	if _, err := store.Load(); err != nil {
		t.Fatal(err)
	}
	t.Chdir(other)
	want := []DeviceAssignment{{DeviceID: testUID, Kind: PlayerDevice, Node: 1}}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadDeviceAssignments(filepath.Join(root, "new", "devices.json"))
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("save followed changed working directory: %v, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(other, "new")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("save created directory in the new working directory: %v", err)
	}
}

func TestDeviceStoreFollowsFileSymlinkWithMissingTarget(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "devices.json")
	if err := os.Symlink("new/nested/devices.json", path); err != nil {
		t.Fatal(err)
	}
	store := &DeviceStore{Path: path}
	if _, err := store.Load(); err != nil {
		t.Fatal(err)
	}
	want := []DeviceAssignment{{DeviceID: testUID, Kind: PlayerDevice, Node: 1}}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("save replaced the file symlink: %v, %v", info, err)
	}
	got, err := LoadDeviceAssignments(path)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("save missed symlink target: %v, %v", got, err)
	}
}
