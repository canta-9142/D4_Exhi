// Package records keeps game records in an SQLite file (server.md 3.5). It
// only reads and writes them; app counts the best scores.
package records

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/canta-9142/d4-exhi/internal/app"
	_ "modernc.org/sqlite" // cgo-free, for Windows tests and arm64 cross builds.
)

// migrations[v] brings a database from schema version v to v+1, as kept in
// PRAGMA user_version. Append to it; never edit a released step.
var migrations = [][]string{
	{
		`CREATE TABLE games (
			id INTEGER PRIMARY KEY,
			finished_at TEXT NOT NULL,
			duration_ms INTEGER NOT NULL CHECK (duration_ms > 0),
			elapsed_ms INTEGER NOT NULL,
			reason TEXT NOT NULL CHECK (reason IN ('timeout', 'manual')),
			CHECK (elapsed_ms BETWEEN 0 AND duration_ms),
			CHECK (reason = 'manual' OR elapsed_ms = duration_ms)
		) STRICT`,
		`CREATE TABLE results (
			game_id INTEGER NOT NULL REFERENCES games (id),
			node TEXT NOT NULL CHECK (node IN ('P1', 'P2', 'P3', 'P4', 'P5', 'P6', 'P7', 'P8')),
			score INTEGER NOT NULL CHECK (score >= 0),
			eligible INTEGER NOT NULL CHECK (eligible IN (0, 1)),
			rank INTEGER,
			PRIMARY KEY (game_id, node),
			CHECK (CASE WHEN eligible = 1 THEN rank IS NOT NULL AND rank BETWEEN 1 AND 8 ELSE rank IS NULL END)
		) STRICT`,
	},
}

// timeLayout is RFC 3339 in UTC with milliseconds; its fixed width keeps
// finished_at in time order as text.
const timeLayout = "2006-01-02T15:04:05.000Z07:00"

// connectionSettings apply to every connection. journal_mode is persistent in
// the file, so migrate sets it only once the file is known to be ours.
const connectionSettings = "?_pragma=busy_timeout(1000)&_pragma=foreign_keys(1)&_pragma=synchronous(FULL)"

// Store is an open records file. Its methods may be called from one
// goroutine at a time.
type Store struct {
	db *sql.DB
}

// Open opens the records file at path, brings its schema to the current
// version and validates every record. Only a missing file is created, along
// with its parent directory. A file it cannot use is left unchanged.
func Open(path string) (*Store, error) {
	return open(path, migrations)
}

func open(path string, migrations [][]string) (*Store, error) {
	// The driver reads everything after '?' as its own parameters, and SQLite
	// opens a name starting with "file:" as a URI rather than as this path.
	if strings.TrimSpace(path) == "" || strings.ContainsRune(path, '?') || strings.HasPrefix(path, "file:") {
		return nil, fmt.Errorf("records path %q must be nonempty, without '?' and not starting with \"file:\"", path)
	}
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	case !info.Mode().IsRegular():
		return nil, fmt.Errorf("records path %s is not a regular file", path)
	}
	db, err := sql.Open("sqlite", path+connectionSettings)
	if err != nil {
		return nil, err
	}
	// One connection keeps the per-connection settings and serializes writes.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(migrations); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// migrate checks the version, the schema and every record before committing
// a migration or switching to WAL, so that a file it cannot use is left as it
// was.
func (s *Store) migrate(migrations [][]string) error {
	ctx := context.Background()
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > len(migrations) {
		return fmt.Errorf("records version %d is newer than the supported version %d", version, len(migrations))
	}
	if version == 0 {
		var objects int
		if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema").Scan(&objects); err != nil {
			return err
		}
		if objects != 0 {
			return errors.New("records file has a schema without a version")
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for v := version; v < len(migrations); v++ {
		for _, statement := range migrations[v] {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("migrate records from version %d: %w", v, err)
			}
		}
	}
	if version < len(migrations) {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", len(migrations))); err != nil {
			return err
		}
	}
	if _, err := load(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// journal_mode cannot change within a transaction.
	var mode string
	if err := s.db.QueryRowContext(ctx, "PRAGMA journal_mode = WAL").Scan(&mode); err != nil {
		return err
	}
	if mode != "wal" {
		return fmt.Errorf("records file cannot use WAL: journal mode is %s", mode)
	}
	return nil
}

// Load returns every game in save order. It validates all of them and
// returns none if any is invalid.
func (s *Store) Load() ([]app.GameRecord, error) {
	ctx := context.Background()
	// One transaction reads both tables from the same state.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return load(ctx, tx)
}

func load(ctx context.Context, tx *sql.Tx) ([]app.GameRecord, error) {
	records := make([]app.GameRecord, 0)
	index := make(map[int64]int)
	rows, err := tx.QueryContext(ctx, "SELECT id, finished_at, duration_ms, elapsed_ms, reason FROM games ORDER BY id")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, duration, elapsed int64
		var finished, reason string
		if err := rows.Scan(&id, &finished, &duration, &elapsed, &reason); err != nil {
			rows.Close()
			return nil, err
		}
		at, err := time.Parse(time.RFC3339Nano, finished)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("game %d: finished_at: %w", id, err)
		}
		if duration <= 0 || duration > maxMilliseconds || elapsed < 0 || elapsed > duration {
			rows.Close()
			return nil, fmt.Errorf("game %d: invalid duration %d ms or elapsed %d ms", id, duration, elapsed)
		}
		index[id] = len(records)
		records = append(records, app.GameRecord{
			FinishedAt: at, Duration: time.Duration(duration) * time.Millisecond, Elapsed: time.Duration(elapsed) * time.Millisecond,
			Reason: app.FinishReason(reason), Results: make([]app.PlayerResult, 0, app.PlayerCount),
		})
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	// Shorter names first, so that the order stays numeric beyond P9.
	rows, err = tx.QueryContext(ctx, "SELECT game_id, node, score, eligible, rank FROM results ORDER BY game_id, length(node), node")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var gameID, score, eligible int64
		var node string
		var rank sql.NullInt64
		if err := rows.Scan(&gameID, &node, &score, &eligible, &rank); err != nil {
			return nil, err
		}
		i, ok := index[gameID]
		if !ok {
			return nil, fmt.Errorf("result %s for unknown game %d", node, gameID)
		}
		r := &records[i]
		// Results arrive in node order, so each must be the next player.
		next := app.NodeID(len(r.Results) + 1)
		if node != nodeName(next) || eligible < 0 || eligible > 1 || score > math.MaxInt32 {
			return nil, fmt.Errorf("game %d: invalid result %s", gameID, node)
		}
		p := app.PlayerResult{Node: next, Score: int(score), Eligible: eligible == 1}
		if rank.Valid {
			if rank.Int64 < 1 || rank.Int64 > app.PlayerCount {
				return nil, fmt.Errorf("game %d: invalid rank of %s", gameID, node)
			}
			p.Rank = int(rank.Int64)
		}
		r.Results = append(r.Results, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, r := range records {
		if err := check(r); err != nil {
			return nil, fmt.Errorf("game %d of %d: %w", i+1, len(records), err)
		}
	}
	return records, nil
}

// maxMilliseconds is the longest duration in milliseconds that time.Duration
// holds.
const maxMilliseconds = int64(math.MaxInt64 / time.Millisecond)

// SaveGame writes r in one transaction.
func (s *Store) SaveGame(r app.GameRecord) error {
	if err := check(r); err != nil {
		return err
	}
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, "INSERT INTO games (finished_at, duration_ms, elapsed_ms, reason) VALUES (?, ?, ?, ?)",
		r.FinishedAt.UTC().Format(timeLayout), r.Duration.Milliseconds(), r.Elapsed.Milliseconds(), string(r.Reason))
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	for _, p := range r.Results {
		eligible, rank := 0, any(nil)
		if p.Eligible {
			eligible, rank = 1, p.Rank
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO results (game_id, node, score, eligible, rank) VALUES (?, ?, ?, ?, ?)",
			id, nodeName(p.Node), p.Score, eligible, rank); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Close closes the file. A Store cannot be used after Close.
func (s *Store) Close() error {
	return s.db.Close()
}

// check validates the record invariants of server.md 3.5.1.
func check(r app.GameRecord) error {
	if r.Reason != app.FinishTimeout && r.Reason != app.FinishManual {
		return fmt.Errorf("invalid finish reason %q", r.Reason)
	}
	if r.Duration < time.Millisecond || r.Elapsed < 0 || r.Elapsed > r.Duration || (r.Reason == app.FinishTimeout && r.Elapsed != r.Duration) {
		return fmt.Errorf("invalid duration %s or elapsed %s for %s", r.Duration, r.Elapsed, r.Reason)
	}
	if len(r.Results) != app.PlayerCount {
		return fmt.Errorf("%d results, want %d", len(r.Results), app.PlayerCount)
	}
	for i, p := range r.Results {
		if p.Node != app.NodeID(i+1) || p.Score < 0 || p.Score > math.MaxInt32 {
			return fmt.Errorf("invalid result %d: %+v", i, p)
		}
		if p.Eligible != (p.Rank != 0) || p.Rank < 0 || p.Rank > app.PlayerCount {
			return fmt.Errorf("invalid rank of %s: eligible=%t rank=%d", nodeName(p.Node), p.Eligible, p.Rank)
		}
	}
	return nil
}

func nodeName(id app.NodeID) string {
	return fmt.Sprintf("P%d", id)
}
