package records

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/canta-9142/d4-exhi/internal/app"
)

var maintenanceStart = time.Date(2026, 10, 30, 4, 0, 0, 0, time.UTC)

func threeGames() []app.GameRecord {
	return []app.GameRecord{
		game(maintenanceStart, app.FinishTimeout, [8]int{1200, 900}, 1, 2),
		game(maintenanceStart.Add(time.Minute), app.FinishManual, [8]int{300}, 1),
		game(maintenanceStart.Add(2*time.Minute), app.FinishTimeout, [8]int{2000}, 1),
	}
}

func TestOpenExclusiveFailsWhileServerHasFileOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.sqlite3")
	server := mustOpen(t, path)
	if err := server.SaveGames(threeGames()); err != nil {
		t.Fatal(err)
	}
	if s, err := OpenExclusive(path); !errors.Is(err, ErrInUse) {
		if s != nil {
			s.Close()
		}
		t.Fatalf("OpenExclusive while open: %v, want ErrInUse", err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := OpenExclusive(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(mustLoad(t, s)); got != 3 {
		t.Fatalf("loaded %d games, want 3", got)
	}
	// A server starting during maintenance cannot use the file.
	if other, err := Open(path); err == nil {
		other.Close()
		t.Fatal("Open succeeded while OpenExclusive holds the file")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	server = mustOpen(t, path)
	defer server.Close()
	if got := mustLoad(t, server); !reflect.DeepEqual(got, threeGames()) {
		t.Fatalf("after maintenance: %+v", got)
	}
}

func TestOpenExclusiveNeverCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "records.sqlite3")
	if s, err := OpenExclusive(path); err == nil {
		s.Close()
		t.Fatal("OpenExclusive of a missing file succeeded")
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("parent directory: %v, want not created", err)
	}
}

func TestDeleteAllKeepsSchemaAndRestartsIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.sqlite3")
	s := mustOpen(t, path)
	if err := s.SaveGames(threeGames()); err != nil {
		t.Fatal(err)
	}
	n, err := s.DeleteAll()
	if err != nil || n != 3 {
		t.Fatalf("DeleteAll = %d, %v, want 3", n, err)
	}
	if got := mustLoad(t, s); len(got) != 0 {
		t.Fatalf("after DeleteAll: %+v", got)
	}
	if err := s.SaveGame(threeGames()[0]); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	db := raw(t, path)
	if got := pragma(t, db, "user_version"); got != "1" {
		t.Fatalf("user_version = %s, want 1", got)
	}
	var id, results int
	if err := db.QueryRow("SELECT id, (SELECT count(*) FROM results) FROM games").Scan(&id, &results); err != nil {
		t.Fatal(err)
	}
	if id != 1 || results != app.PlayerCount {
		t.Fatalf("id %d with %d results, want 1 with %d", id, results, app.PlayerCount)
	}
}

func TestBackupCopiesRecordsAndRefusesExistingFile(t *testing.T) {
	dir := t.TempDir()
	s := mustOpen(t, filepath.Join(dir, "records.sqlite3"))
	defer s.Close()
	if err := s.SaveGames(threeGames()); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "backup.sqlite3")
	if err := s.Backup(backup); err != nil {
		t.Fatal(err)
	}
	copied := mustOpen(t, backup)
	defer copied.Close()
	if got := mustLoad(t, copied); !reflect.DeepEqual(got, threeGames()) {
		t.Fatalf("backup: %+v", got)
	}

	empty := filepath.Join(dir, "empty.sqlite3")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{backup, empty} {
		if err := s.Backup(path); err == nil {
			t.Errorf("Backup to existing %s succeeded", filepath.Base(path))
		}
	}
	if info, err := os.Stat(empty); err != nil || info.Size() != 0 {
		t.Fatalf("existing empty file: %v, %v", info, err)
	}
}

func TestReplaceAllReplacesEveryGameOrNone(t *testing.T) {
	s := mustOpen(t, filepath.Join(t.TempDir(), "records.sqlite3"))
	defer s.Close()
	if err := s.SaveGames(threeGames()); err != nil {
		t.Fatal(err)
	}
	invalid := threeGames()[1:]
	invalid[1].Reason = "crashed"
	if err := s.ReplaceAll(invalid); err == nil {
		t.Fatal("ReplaceAll with an invalid record succeeded")
	}
	if got := mustLoad(t, s); !reflect.DeepEqual(got, threeGames()) {
		t.Fatalf("failed ReplaceAll changed the games: %+v", got)
	}
	replacement := threeGames()[1:]
	if err := s.ReplaceAll(replacement); err != nil {
		t.Fatal(err)
	}
	if got := mustLoad(t, s); !reflect.DeepEqual(got, replacement) {
		t.Fatalf("after ReplaceAll: %+v", got)
	}
}

func TestSaveGamesSavesNoneIfAnyIsInvalid(t *testing.T) {
	s := mustOpen(t, filepath.Join(t.TempDir(), "records.sqlite3"))
	defer s.Close()
	games := threeGames()
	games[2].Results = games[2].Results[:app.PlayerCount-1]
	if err := s.SaveGames(games); err == nil {
		t.Fatal("SaveGames with an invalid record succeeded")
	}
	if got := mustLoad(t, s); len(got) != 0 {
		t.Fatalf("saved %d games, want none", len(got))
	}
}
