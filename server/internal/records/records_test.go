package records

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/canta-9142/d4-exhi/internal/app"
)

// game is a record of the current duration with P1-P8 scoring the given
// points. eligible players are ranked by them.
func game(at time.Time, reason app.FinishReason, scores [app.PlayerCount]int, eligible ...app.NodeID) app.GameRecord {
	r := app.GameRecord{Kind: app.RecordGame, FinishedAt: at, Duration: app.GameDuration, Elapsed: app.GameDuration, Reason: reason}
	if reason == app.FinishManual {
		r.Elapsed = 41*time.Second + 250*time.Millisecond
	}
	for i, score := range scores {
		p := app.PlayerResult{Node: app.NodeID(i + 1), Score: score}
		for _, id := range eligible {
			if id == p.Node {
				p.Eligible, p.Rank = true, 1
				for _, other := range eligible {
					if scores[other-1] > score {
						p.Rank++
					}
				}
			}
		}
		r.Results = append(r.Results, p)
	}
	return r
}

func mustOpen(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustLoad(t *testing.T, s *Store) []app.GameRecord {
	t.Helper()
	records, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	return records
}

// raw opens path without Store, for setting up and inspecting files.
func raw(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func pragma(t *testing.T, db *sql.DB, name string) string {
	t.Helper()
	var value string
	if err := db.QueryRow("PRAGMA " + name).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestSaveCloseReopenLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "records.sqlite3")
	s := mustOpen(t, path)
	if records := mustLoad(t, s); records == nil || len(records) != 0 {
		t.Fatalf("new file: %v", records)
	}
	saved := []app.GameRecord{
		game(time.Date(2026, 10, 28, 13, 59, 59, 999_999_999, app.JST), app.FinishTimeout, [8]int{300, 300, 900, 0, 15, 8, 0, 0}, 1, 2, 5),
		game(time.Date(2026, 10, 28, 5, 0, 0, 0, time.UTC), app.FinishManual, [8]int{}, 8),
		game(time.Date(2026, 10, 28, 14, 0, 0, 0, app.JST), app.FinishTimeout, [8]int{1, 2, 3, 4, 5, 6, 7, 8}),
	}
	for _, r := range saved {
		if err := s.SaveGame(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = mustOpen(t, path)
	defer s.Close()
	loaded := mustLoad(t, s)
	if len(loaded) != len(saved) {
		t.Fatalf("loaded %d games, want %d", len(loaded), len(saved))
	}
	for i, r := range saved {
		got := loaded[i]
		// finished_at keeps milliseconds, truncated, in UTC.
		if !got.FinishedAt.Equal(r.FinishedAt.Truncate(time.Millisecond)) || got.FinishedAt.Location() != time.UTC {
			t.Errorf("game %d finished at %s, want %s", i, got.FinishedAt, r.FinishedAt)
		}
		got.FinishedAt = r.FinishedAt
		if !reflect.DeepEqual(got, r) {
			t.Errorf("game %d\n got %+v\nwant %+v", i, got, r)
		}
	}
	db := raw(t, path)
	if mode, version := pragma(t, db, "journal_mode"), pragma(t, db, "user_version"); mode != "wal" || version != "2" {
		t.Fatalf("journal_mode=%s user_version=%s", mode, version)
	}
	var finished string
	if err := db.QueryRow("SELECT finished_at FROM games WHERE id = 1").Scan(&finished); err != nil || finished != "2026-10-28T04:59:59.999Z" {
		t.Fatalf("finished_at = %q, %v", finished, err)
	}
}

func TestOpenUsesEmptyFileAsNewDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.sqlite3")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s := mustOpen(t, path)
	defer s.Close()
	if err := s.SaveGame(game(time.Now(), app.FinishTimeout, [8]int{10}, 1)); err != nil {
		t.Fatal(err)
	}
	if records := mustLoad(t, s); len(records) != 1 {
		t.Fatalf("loaded %d games", len(records))
	}
}

// v1Records is a version 1 file holding one game, written as the version 1
// server wrote it, and that game as it loads once migrated.
func v1Records(t *testing.T) (string, app.GameRecord) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "records.sqlite3")
	db := raw(t, path)
	statements := append(slices.Clone(migrations[0]), "PRAGMA user_version = 1",
		"INSERT INTO games (finished_at, duration_ms, elapsed_ms, reason) VALUES ('2026-10-28T04:05:12.000Z', 90000, 90000, 'timeout')")
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	r := game(time.Date(2026, 10, 28, 13, 5, 12, 0, app.JST), app.FinishTimeout, [8]int{812}, 1)
	for _, p := range r.Results {
		rank := any(nil)
		if p.Eligible {
			rank = p.Rank
		}
		if _, err := db.Exec("INSERT INTO results (game_id, node, score, eligible, rank) VALUES (1, ?, ?, ?, ?)", nodeName(p.Node), p.Score, p.Eligible, rank); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return path, r
}

// round is a normal round of node that scored score.
func round(at time.Time, node app.NodeID, score int) app.GameRecord {
	return app.GameRecord{
		Kind: app.RecordRound, FinishedAt: at, Duration: app.GameDuration, Elapsed: app.GameDuration, Reason: app.FinishTimeout,
		Results: []app.PlayerResult{{Node: node, Score: score, Eligible: true, Rank: 1}},
	}
}

func TestOpenMigratesVersion1GamesToSimultaneousGames(t *testing.T) {
	path, r := v1Records(t)
	s := mustOpen(t, path)
	loaded := mustLoad(t, s)
	if len(loaded) != 1 || loaded[0].Kind != app.RecordGame || !reflect.DeepEqual(loaded[0].Results, r.Results) {
		t.Fatalf("migrated games: %+v", loaded)
	}
	// Rounds join the migrated games in save order.
	added := round(time.Date(2026, 10, 28, 13, 7, 0, 0, app.JST), 3, 640)
	if err := s.SaveGame(added); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = mustOpen(t, path)
	defer s.Close()
	loaded = mustLoad(t, s)
	if len(loaded) != 2 || loaded[0].Kind != app.RecordGame || !reflect.DeepEqual(loaded[1].Results, added.Results) || loaded[1].Kind != app.RecordRound {
		t.Fatalf("games after adding a round: %+v", loaded)
	}
	if version := pragma(t, raw(t, path), "user_version"); version != "2" {
		t.Fatalf("user_version = %s, want 2", version)
	}
}

func TestOpenMigratesOlderVersion(t *testing.T) {
	path, r := v1Records(t)
	next := append(append([][]string(nil), migrations...), []string{"ALTER TABLE games ADD COLUMN note TEXT"})
	s, err := open(path, next)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if loaded := mustLoad(t, s); len(loaded) != 1 || loaded[0].Results[0] != r.Results[0] {
		t.Fatalf("migration lost the saved game: %+v", loaded)
	}
	var version int
	var note sql.NullString
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 3 {
		t.Fatalf("user_version = %d, %v", version, err)
	}
	if err := s.db.QueryRow("SELECT note FROM games").Scan(&note); err != nil || note.Valid {
		t.Fatalf("migrated column: %v, %v", note, err)
	}
}

func TestFailedMigrationKeepsVersionAndRecords(t *testing.T) {
	path, _ := v1Records(t)
	broken := append(append([][]string(nil), migrations...), []string{"CREATE TABLE extra (x INTEGER) STRICT", "NOT SQL"})
	if _, err := open(path, broken); err == nil || !strings.Contains(err.Error(), "migrate records from version 2") {
		t.Fatalf("broken migration: %v", err)
	}
	db := raw(t, path)
	var tables int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name = 'extra'").Scan(&tables); err != nil || tables != 0 || pragma(t, db, "user_version") != "1" {
		t.Fatalf("the failed migration was partly applied: tables=%d %v", tables, err)
	}
	db.Close()
	s := mustOpen(t, path)
	defer s.Close()
	if loaded := mustLoad(t, s); len(loaded) != 1 {
		t.Fatalf("loaded %d games after the failed migration", len(loaded))
	}
}

func TestOpenRejectsUnusableFilesWithoutChangingThem(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, path string)
		want  string
	}{
		{"not a database", func(t *testing.T, path string) {
			if err := os.WriteFile(path, bytes.Repeat([]byte("not a database\n"), 512), 0o600); err != nil {
				t.Fatal(err)
			}
		}, "not a database"},
		{"truncated header", func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("SQLite format 3\x00\x10\x00"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, "not a database"},
		{"newer version", func(t *testing.T, path string) {
			db := raw(t, path)
			if _, err := db.Exec("CREATE TABLE games (id INTEGER PRIMARY KEY); PRAGMA user_version = 3"); err != nil {
				t.Fatal(err)
			}
			db.Close()
		}, "newer than the supported version 2"},
		{"foreign schema", func(t *testing.T, path string) {
			db := raw(t, path)
			if _, err := db.Exec("CREATE TABLE other (x)"); err != nil {
				t.Fatal(err)
			}
			db.Close()
		}, "schema without a version"},
		{"foreign schema with a version", func(t *testing.T, path string) {
			db := raw(t, path)
			if _, err := db.Exec("CREATE TABLE other (x); PRAGMA user_version = 1"); err != nil {
				t.Fatal(err)
			}
			db.Close()
		}, "no such table"},
		{"directory", func(t *testing.T, path string) {
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
		}, "not a regular file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "records.sqlite3")
			tc.setup(t, path)
			before, _ := os.ReadFile(path)
			s, err := Open(path)
			if err == nil {
				s.Close()
				t.Fatal("Open accepted an unusable file")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q, want %q", err, tc.want)
			}
			if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
				t.Fatal("Open changed the file")
			}
			if _, err := os.Stat(path + "-wal"); err == nil {
				t.Fatal("Open switched the file to WAL")
			}
		})
	}
}

// Open validates the records before it migrates the file or switches it to
// WAL, so a file whose rows are invalid stays as it was, in its journal mode.
func TestOpenRejectsInvalidRecordsWithoutChangingThem(t *testing.T) {
	for _, tc := range []struct {
		name string
		sql  string
	}{
		{"no results", "DELETE FROM results"},
		{"bad time", "UPDATE games SET finished_at = 'yesterday'"},
		{"unknown game", "PRAGMA foreign_keys = OFF; INSERT INTO results (game_id, node, score, eligible) VALUES (2, 'P1', 0, 0)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, _ := v1Records(t)
			db := raw(t, path)
			if _, err := db.Exec("PRAGMA journal_mode = DELETE; " + tc.sql); err != nil {
				t.Fatal(err)
			}
			db.Close()
			before, _ := os.ReadFile(path)
			if s, err := Open(path); err == nil {
				s.Close()
				t.Fatal("Open accepted invalid records")
			}
			if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
				t.Fatal("Open changed the file")
			}
			if mode := pragma(t, raw(t, path), "journal_mode"); mode != "delete" {
				t.Fatalf("journal_mode = %s after the rejected Open", mode)
			}
		})
	}
}

func TestOpenRejectsPathsTheDriverReadsDifferently(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	for _, path := range []string{"", " ", "records.sqlite3?mode=ro", "file:records.sqlite3"} {
		if s, err := Open(path); err == nil {
			s.Close()
			t.Errorf("Open(%q) succeeded", path)
		}
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("rejected paths created %v, %v", entries, err)
	}
}

func TestSchemaRejectsInconsistentRows(t *testing.T) {
	path, _ := v1Records(t)
	if err := mustOpen(t, path).Close(); err != nil {
		t.Fatal(err)
	}
	db := raw(t, path)
	for _, statement := range []string{
		"UPDATE games SET kind = 'other'",
		"UPDATE results SET rank = NULL WHERE node = 'P1'",
		"UPDATE results SET rank = 2 WHERE node = 'P2'",
		"UPDATE results SET score = -1 WHERE node = 'P3'",
		"UPDATE games SET elapsed_ms = 1000",
		"UPDATE games SET reason = 'other'",
		"INSERT INTO results (game_id, node, score, eligible) VALUES (2, 'P1', 0, 0)",
	} {
		if _, err := db.Exec("PRAGMA foreign_keys = ON; " + statement); err == nil || !strings.Contains(err.Error(), "constraint failed") {
			t.Errorf("%q: %v, want a constraint failure", statement, err)
		}
	}
}

func TestSaveGameRejectsInvalidRecordWithoutWriting(t *testing.T) {
	path, _ := v1Records(t)
	s := mustOpen(t, path)
	defer s.Close()
	valid := game(time.Now(), app.FinishTimeout, [8]int{1}, 1)
	empty := valid
	empty.Results = nil
	duplicate := round(time.Now(), 2, 100)
	duplicate.Results = append(duplicate.Results, duplicate.Results[0])
	unordered := game(time.Now(), app.FinishTimeout, [8]int{1}, 1)
	unordered.Results[0], unordered.Results[1] = unordered.Results[1], unordered.Results[0]
	unknown := round(time.Now(), 2, 100)
	unknown.Kind = ""
	early := valid
	early.Elapsed = time.Second
	unranked := game(time.Now(), app.FinishTimeout, [8]int{1}, 1)
	unranked.Results[0].Rank = 0
	for name, r := range map[string]app.GameRecord{
		"no results": empty, "duplicate node": duplicate, "nodes out of order": unordered, "no kind": unknown,
		"early timeout": early, "unranked": unranked,
	} {
		if err := s.SaveGame(r); err == nil {
			t.Errorf("%s: SaveGame accepted an invalid record", name)
		}
	}
	if records := mustLoad(t, s); len(records) != 1 {
		t.Fatalf("%d games after rejected saves", len(records))
	}
}
