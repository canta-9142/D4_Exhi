package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/canta-9142/d4-exhi/internal/app"
	"github.com/canta-9142/d4-exhi/internal/records"
)

var testNow = time.Date(2026, 10, 30, 13, 25, 7, 0, app.JST)

func now() time.Time { return testNow }

// eligibleGame is a game of the current full duration in which P1-P8 score
// scores and the first eligible of them are eligible.
func eligibleGame(at time.Time, eligible int, scores ...int) app.GameRecord {
	r := app.GameRecord{FinishedAt: at, Duration: app.GameDuration, Elapsed: app.GameDuration, Reason: app.FinishTimeout}
	for i := range app.PlayerCount {
		p := app.PlayerResult{Node: app.NodeID(i + 1)}
		if i < len(scores) {
			p.Score = scores[i]
		}
		if i < eligible {
			p.Eligible, p.Rank = true, 1
			for _, other := range scores[:eligible] {
				if other > p.Score {
					p.Rank++
				}
			}
		}
		r.Results = append(r.Results, p)
	}
	return r
}

func writeRecords(t *testing.T, path string, games ...app.GameRecord) {
	t.Helper()
	s, err := records.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(s.SaveGames(games), s.Close()); err != nil {
		t.Fatal(err)
	}
}

// readRecords loads path with finished_at in JST, as the tests write them.
func readRecords(t *testing.T, path string) []app.GameRecord {
	t.Helper()
	s, err := records.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	games, err := s.Load()
	if err := errors.Join(err, s.Close()); err != nil {
		t.Fatal(err)
	}
	for i := range games {
		games[i].FinishedAt = games[i].FinishedAt.In(app.JST)
	}
	return games
}

func runRecords(input string, args ...string) (string, error) {
	var out bytes.Buffer
	err := run(args, strings.NewReader(input), &out, now)
	return out.String(), err
}

// bandCounts reads the band rows of a histogram as label to count.
func bandCounts(t *testing.T, output string) map[string]string {
	t.Helper()
	counts := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && (strings.Contains(fields[0], "-") || strings.Contains(fields[0], "~") || fields[0] == "total") {
			counts[fields[0]] = fields[1]
		}
	}
	if len(counts) != bandCount+1 {
		t.Fatalf("histogram has %d rows, want %d:\n%s", len(counts), bandCount+1, output)
	}
	return counts
}

func TestBand(t *testing.T) {
	if bandCount != 16 {
		t.Fatalf("bandCount = %d, want 16", bandCount)
	}
	for score, want := range map[int]int{0: 0, 99: 0, 100: 1, 199: 1, 200: 2, 1000: 10, 1399: 13, 1400: 14, 1499: 14, 1500: 15, 1999: 15, 100000: 15} {
		if got := band(score); got != want {
			t.Errorf("band(%d) = %d, want %d", score, got, want)
		}
	}
	labels := make([]string, bandCount)
	for i := range labels {
		labels[i] = bandLabel(i)
	}
	want := []string{"0-99", "100-199", "200-299", "300-399", "400-499", "500-599", "600-699", "700-799", "800-899", "900-999", "1000-1099", "1100-1199", "1200-1299", "1300-1399", "1400-1499", "1500~"}
	if !reflect.DeepEqual(labels, want) {
		t.Fatalf("labels %v, want %v", labels, want)
	}
}

func TestHistogramCountsThePopulationOfTheTopPercent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.sqlite3")
	at := testNow.Add(-time.Hour)
	manual := eligibleGame(at, 1, 1500)
	manual.Reason, manual.Elapsed = app.FinishManual, time.Second
	older := eligibleGame(at, 1, 1500)
	older.Duration, older.Elapsed = 2*app.GameDuration, 2*app.GameDuration
	writeRecords(t, path,
		eligibleGame(at, 4, 999, 1000, 1999, 2000, 1500), // P5 is not eligible.
		eligibleGame(at, 2, 0, 1050),
		manual, older,
	)
	output, err := runRecords("", "histogram", "-records-file", path)
	if err != nil {
		t.Fatal(err)
	}
	got := bandCounts(t, output)
	want := map[string]string{"0-99": "1", "900-999": "1", "1000-1099": "2", "1500~": "2", "total": "6"}
	for label, count := range got {
		if w := want[label]; count != w && !(w == "" && count == "0") {
			t.Errorf("%s: %s, want %s", label, count, w)
		}
	}
	if !strings.Contains(output, "ゲーム 4件のうち対象 2件、スコア 6件") {
		t.Errorf("summary missing:\n%s", output)
	}
	// histogram reads while a server has the file open.
	server, err := records.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if _, err := runRecords("", "histogram", "-records-file", path); err != nil {
		t.Fatalf("histogram while the server runs: %v", err)
	}
}

func TestHistogramNeverCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "records.sqlite3")
	if _, err := runRecords("", "histogram", "-records-file", path); err == nil {
		t.Fatal("histogram of a missing file succeeded")
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("parent directory: %v, want not created", err)
	}
}

func TestClearNeedsTheConfirmation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "records.sqlite3")
	games := []app.GameRecord{eligibleGame(testNow.Add(-time.Hour), 2, 1200, 800), eligibleGame(testNow.Add(-time.Minute), 1, 1500)}
	writeRecords(t, path, games...)
	for _, input := range []string{"", "yes\n", "DELETE 1 GAMES\n", "delete 2 games\n"} {
		output, err := runRecords(input, "clear", "-records-file", path)
		if !errors.Is(err, errCanceled) {
			t.Fatalf("input %q: %v", input, err)
		}
		if !strings.Contains(output, "DELETE 2 GAMES") {
			t.Fatalf("prompt missing:\n%s", output)
		}
	}
	if got := readRecords(t, path); !reflect.DeepEqual(got, games) {
		t.Fatalf("canceled clear changed the records: %+v", got)
	}
	if matches, _ := filepath.Glob(path + ".bak-*"); len(matches) != 0 {
		t.Fatalf("canceled clear made backups %v", matches)
	}

	if _, err := runRecords("DELETE 2 GAMES\r\n", "clear", "-records-file", path); err != nil {
		t.Fatal(err)
	}
	if got := readRecords(t, path); len(got) != 0 {
		t.Fatalf("after clear: %+v", got)
	}
	backup := path + ".bak-20261030-132507"
	if got := readRecords(t, backup); !reflect.DeepEqual(got, games) {
		t.Fatalf("backup: %+v", got)
	}

	// An empty file needs no confirmation and is left as it is.
	output, err := runRecords("", "clear", "-records-file", path)
	if err != nil || !strings.Contains(output, "記録はありません") {
		t.Fatalf("clear of empty records: %v\n%s", err, output)
	}
}

func TestClearKeepsRecordsIfTheBackupExists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.sqlite3")
	games := []app.GameRecord{eligibleGame(testNow.Add(-time.Hour), 1, 1200)}
	writeRecords(t, path, games...)
	if err := os.WriteFile(path+".bak-20261030-132507", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runRecords("DELETE 1 GAMES\n", "clear", "-records-file", path); err == nil {
		t.Fatal("clear succeeded without a backup")
	}
	if got := readRecords(t, path); !reflect.DeepEqual(got, games) {
		t.Fatalf("records changed: %+v", got)
	}
}

func TestClearAndSeedRefuseWhileTheServerRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.sqlite3")
	games := []app.GameRecord{eligibleGame(testNow.Add(-time.Hour), 1, 1200)}
	writeRecords(t, path, games...)
	server, err := records.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(filepath.Dir(path), "old.sqlite3")
	writeRecords(t, backup, eligibleGame(testNow.Add(-2*time.Hour), 1, 900))
	for _, args := range [][]string{{"clear"}, {"seed", "-scores", "1000"}, {"restore", "-from", backup}} {
		args = append(args, "-records-file", path)
		if _, err := runRecords("DELETE 1 GAMES\nSEED 1 GAMES\nRESTORE 1 GAMES\n", args...); !errors.Is(err, records.ErrInUse) {
			t.Fatalf("%v while the server runs: %v, want ErrInUse", args, err)
		}
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if got := readRecords(t, path); !reflect.DeepEqual(got, games) {
		t.Fatalf("records changed: %+v", got)
	}
}

func TestSeedAddsGamesOfA(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.sqlite3")
	writeRecords(t, path)
	if _, err := runRecords("no\n", "seed", "-records-file", path, "-scores", "1200, 950,2100"); !errors.Is(err, errCanceled) {
		t.Fatalf("canceled seed: %v", err)
	}
	if got := readRecords(t, path); len(got) != 0 {
		t.Fatalf("canceled seed added %+v", got)
	}

	output, err := runRecords("SEED 3 GAMES\n", "seed", "-records-file", path, "-scores", "1200, 950,2100")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "2026-10-29T13:00:00+09:00") || !strings.Contains(output, "最高 2100") {
		t.Errorf("preview missing the time or the best:\n%s", output)
	}
	start := time.Date(2026, 10, 29, 13, 0, 0, 0, app.JST)
	var want []app.GameRecord
	for i, score := range []int{1200, 950, 2100} {
		want = append(want, eligibleGame(start.Add(time.Duration(i)*time.Second), 1, score))
	}
	if got := readRecords(t, path); !reflect.DeepEqual(got, want) {
		t.Fatalf("seeded %+v, want %+v", got, want)
	}

	// seed adds only to empty records.
	if _, err := runRecords("SEED 1 GAMES\n", "seed", "-records-file", path, "-scores", "1000"); err == nil {
		t.Fatal("seed into existing records succeeded")
	}
	if got := readRecords(t, path); len(got) != 3 {
		t.Fatalf("records have %d games, want 3", len(got))
	}
}

func TestSeedDrawsReproducibleScoresInRange(t *testing.T) {
	draw := func() []app.GameRecord {
		path := filepath.Join(t.TempDir(), "records.sqlite3")
		writeRecords(t, path)
		args := []string{"seed", "-records-file", path, "-count", "200", "-mean", "1400", "-sd", "300", "-min", "1000", "-max", "1800", "-rand-seed", "42", "-at", "2026-10-29T09:30:00+09:00"}
		if _, err := runRecords("SEED 200 GAMES\n", args...); err != nil {
			t.Fatal(err)
		}
		return readRecords(t, path)
	}
	first, second := draw(), draw()
	if !reflect.DeepEqual(first, second) {
		t.Fatal("the same -rand-seed drew different scores")
	}
	if len(first) != 200 {
		t.Fatalf("%d games, want 200", len(first))
	}
	distinct := make(map[int]bool)
	for _, r := range first {
		score := r.Results[0].Score
		if score < 1000 || score > 1800 {
			t.Fatalf("score %d outside 1000-1800", score)
		}
		distinct[score] = true
	}
	if len(distinct) < 50 {
		t.Fatalf("only %d distinct scores", len(distinct))
	}
}

func TestSeedRejectsInvalidOptions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.sqlite3")
	writeRecords(t, path)
	for _, args := range [][]string{
		{},
		{"-scores", "1000", "-count", "3"},
		{"-scores", "1000,,1200"},
		{"-scores", "-5"},
		{"-count", "-1"},
		{"-count", "10001"},
		{"-count", "3", "-mean", "500"},
		{"-count", "3", "-min", "1500", "-max", "1000", "-mean", "1200"},
		{"-count", "3", "-sd", "-1"},
		{"-scores", "1000", "-at", "2026-10-30T13:30:00+09:00"},
		{"-scores", "1000", "-at", "yesterday"},
	} {
		args = append([]string{"seed", "-records-file", path}, args...)
		if _, err := runRecords("SEED 3 GAMES\nSEED 1 GAMES\n", args...); err == nil || errors.Is(err, errCanceled) {
			t.Errorf("%v: %v, want an option error", args[3:], err)
		}
	}
	if got := readRecords(t, path); len(got) != 0 {
		t.Fatalf("invalid options added %+v", got)
	}
}

func TestRestoreReplacesTheRecordsAndBacksThemUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "records.sqlite3")
	current := []app.GameRecord{eligibleGame(testNow.Add(-time.Hour), 2, 1200, 800), eligibleGame(testNow.Add(-time.Minute), 1, 1500)}
	writeRecords(t, path, current...)
	from := filepath.Join(dir, "old.sqlite3")
	old := []app.GameRecord{
		eligibleGame(testNow.Add(-3*time.Hour), 1, 1900),
		eligibleGame(testNow.Add(-2*time.Hour), 3, 1000, 1100, 990),
		eligibleGame(testNow.Add(-time.Hour), 1, 2050),
	}
	writeRecords(t, from, old...)
	source, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}

	output, err := runRecords("RESTORE 2 GAMES\n", "restore", "-records-file", path, "-from", from)
	if !errors.Is(err, errCanceled) || !strings.Contains(output, "RESTORE 3 GAMES") {
		t.Fatalf("restore with the wrong count: %v\n%s", err, output)
	}
	if got := readRecords(t, path); !reflect.DeepEqual(got, current) {
		t.Fatalf("canceled restore changed the records: %+v", got)
	}
	if matches, _ := filepath.Glob(path + ".bak-*"); len(matches) != 0 {
		t.Fatalf("canceled restore made backups %v", matches)
	}

	if _, err := runRecords("RESTORE 3 GAMES\n", "restore", "-records-file", path, "-from", from); err != nil {
		t.Fatal(err)
	}
	if got := readRecords(t, path); !reflect.DeepEqual(got, old) {
		t.Fatalf("after restore: %+v", got)
	}
	if got := readRecords(t, path+".bak-20261030-132507"); !reflect.DeepEqual(got, current) {
		t.Fatalf("backup of the replaced records: %+v", got)
	}
	// The backup itself is only read.
	if after, err := os.ReadFile(from); err != nil || !bytes.Equal(after, source) {
		t.Fatalf("restore changed the backup: %v", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(from + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("backup%s: %v, want none", suffix, err)
		}
	}
}

func TestRestoreRejectsUnusableBackups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "records.sqlite3")
	current := []app.GameRecord{eligibleGame(testNow.Add(-time.Hour), 1, 1200)}
	writeRecords(t, path, current...)
	garbage := filepath.Join(dir, "garbage.sqlite3")
	if err := os.WriteFile(garbage, []byte("not a database, just some text to fill a page"), 0o644); err != nil {
		t.Fatal(err)
	}
	withWAL := filepath.Join(dir, "open.sqlite3")
	writeRecords(t, withWAL, eligibleGame(testNow.Add(-time.Hour), 1, 900))
	if err := os.WriteFile(withWAL+"-wal", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]string{
		"no -from":        {"-records-file", path},
		"missing backup":  {"-records-file", path, "-from", filepath.Join(dir, "missing.sqlite3")},
		"the file itself": {"-records-file", path, "-from", path},
		"not a database":  {"-records-file", path, "-from", garbage},
		"backup with WAL": {"-records-file", path, "-from", withWAL},
		"missing records": {"-records-file", filepath.Join(dir, "missing", "records.sqlite3"), "-from", withWAL},
	} {
		if _, err := runRecords("RESTORE 1 GAMES\nRESTORE 0 GAMES\n", append([]string{"restore"}, args...)...); err == nil || errors.Is(err, errCanceled) {
			t.Errorf("%s: %v, want an error before the confirmation", name, err)
		}
	}
	if got := readRecords(t, path); !reflect.DeepEqual(got, current) {
		t.Fatalf("records changed: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restore created the missing directory: %v", err)
	}
}

// TestRestoreAppliesTheWALToTheRecordsItBelongsTo restores over records whose
// last game is only in a WAL file, as a crashed server leaves it: the game is
// in the backup of the replaced records, and nothing of it is in the result.
func TestRestoreAppliesTheWALToTheRecordsItBelongsTo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "records.sqlite3")
	walGame := eligibleGame(testNow.Add(-time.Minute), 1, 1777)
	crashed := filepath.Join(dir, "crashed")
	if err := os.Mkdir(crashed, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRecords(t, path, eligibleGame(testNow.Add(-time.Hour), 1, 1200))
	// Copy the file and its WAL while a connection still has the last game
	// only in the WAL.
	server, err := records.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.SaveGame(walGame); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal"} {
		if err := copyFile(path+suffix, filepath.Join(crashed, "records.sqlite3"+suffix)); err != nil {
			t.Fatal(err)
		}
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(crashed, "records.sqlite3")
	if _, err := os.Stat(path + "-wal"); err != nil {
		t.Fatalf("the crashed copy has no WAL: %v", err)
	}
	from := filepath.Join(dir, "old.sqlite3")
	old := []app.GameRecord{eligibleGame(testNow.Add(-3*time.Hour), 1, 900)}
	writeRecords(t, from, old...)

	if _, err := runRecords("RESTORE 1 GAMES\n", "restore", "-records-file", path, "-from", from); err != nil {
		t.Fatal(err)
	}
	if got := readRecords(t, path); !reflect.DeepEqual(got, old) {
		t.Fatalf("after restore: %+v", got)
	}
	replaced := readRecords(t, path+".bak-20261030-132507")
	if len(replaced) != 2 || !reflect.DeepEqual(replaced[1], walGame) {
		t.Fatalf("backup of the replaced records: %+v", replaced)
	}
}
