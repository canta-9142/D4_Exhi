// Command records shows and maintains the game records file of the server
// (server.md 3.5.8). histogram only reads the file. clear, seed and restore
// change it only while no server has it open, and only after the operator
// types the confirmation they show.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/canta-9142/d4-exhi/internal/app"
	"github.com/canta-9142/d4-exhi/internal/records"
)

const usage = `使い方: records <サブコマンド> [オプション]

  histogram  スコア帯ごとの人数を表示する（読み取りだけ。サーバーの動作中も使える）
  clear      すべてのゲーム記録を削除する（サーバーの停止中だけ）
  seed       空の記録へプレイヤーA（P1）の通常ラウンドを追加する（サーバーの停止中だけ）
  restore    記録をバックアップの内容に置き換える（サーバーの停止中だけ）

オプションは records <サブコマンド> -h で表示する。`

// errCanceled means the operator did not type the confirmation, so nothing
// was changed.
var errCanceled = errors.New("入力が確認の文字列と一致しないため中止しました。記録は変更していません")

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, time.Now); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, input io.Reader, output io.Writer, now func() time.Time) error {
	if len(args) == 0 {
		fmt.Fprintln(output, usage)
		return errors.New("サブコマンドを指定してください")
	}
	var err error
	switch args[0] {
	case "histogram":
		err = histogram(args[1:], output)
	case "clear":
		err = clearRecords(args[1:], input, output, now)
	case "seed":
		err = seed(args[1:], input, output, now)
	case "restore":
		err = restore(args[1:], input, output, now)
	case "-h", "-help", "--help", "help":
		fmt.Fprintln(output, usage)
		return nil
	default:
		fmt.Fprintln(output, usage)
		return fmt.Errorf("不明なサブコマンド %q", args[0])
	}
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return err
}

// newFlags is the flag set of a subcommand with the -records-file option of
// cmd/server.
func newFlags(name string, output io.Writer) (*flag.FlagSet, *string) {
	flags := flag.NewFlagSet("records "+name, flag.ContinueOnError)
	flags.SetOutput(output)
	path := flags.String("records-file", "data/records.sqlite3", "game record SQLite file")
	return flags, path
}

func parse(flags *flag.FlagSet, args []string) error {
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	return nil
}

func histogram(args []string, output io.Writer) error {
	flags, path := newFlags("histogram", output)
	if err := parse(flags, args); err != nil {
		return err
	}
	// records.Open would create a missing file.
	if _, err := os.Stat(*path); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("記録ファイル %s がありません", *path)
	}
	store, err := records.Open(*path)
	if err != nil {
		return fmt.Errorf("記録ファイル %s を開けません: %w", *path, err)
	}
	games, err := store.Load()
	if err := errors.Join(err, store.Close()); err != nil {
		return fmt.Errorf("記録ファイル %s を読めません: %w", *path, err)
	}
	fmt.Fprintf(output, "記録ファイル: %s\n", *path)
	writeHistogram(output, games)
	return nil
}

// openExclusive opens path for clear and seed, explaining a running server.
func openExclusive(path string) (*records.Store, error) {
	store, err := records.OpenExclusive(path)
	if errors.Is(err, records.ErrInUse) {
		return nil, fmt.Errorf("サーバーが記録ファイル %s を開いています。サーバーを停止してから実行してください: %w", path, err)
	}
	if err != nil {
		return nil, fmt.Errorf("記録ファイル %s を開けません: %w", path, err)
	}
	return store, nil
}

func clearRecords(args []string, input io.Reader, output io.Writer, now func() time.Time) (err error) {
	flags, path := newFlags("clear", output)
	if err := parse(flags, args); err != nil {
		return err
	}
	store, err := openExclusive(*path)
	if err != nil {
		return err
	}
	// Close checkpoints the WAL into the file, so its failure is reported.
	defer func() { err = errors.Join(err, store.Close()) }()
	games, err := store.Load()
	if err != nil {
		return fmt.Errorf("記録ファイル %s を読めません: %w", *path, err)
	}
	if len(games) == 0 {
		fmt.Fprintf(output, "記録ファイル %s にゲームの記録はありません。何も変更していません。\n", *path)
		return nil
	}
	backup := backupPath(*path, now)
	fmt.Fprintf(output, "記録ファイル: %s\n", *path)
	writeHistogram(output, games)
	fmt.Fprintf(output, "\nゲーム %d件の記録をすべて削除します。削除の前に %s へバックアップを作ります。\n", len(games), backup)
	if err := confirm(input, output, fmt.Sprintf("DELETE %d GAMES", len(games))); err != nil {
		return err
	}
	if err := store.Backup(backup); err != nil {
		return fmt.Errorf("バックアップを作れないため中止しました。記録は変更していません: %w", err)
	}
	deleted, err := store.DeleteAll()
	if err != nil {
		return fmt.Errorf("削除に失敗しました。記録は変更していません（バックアップ: %s）: %w", backup, err)
	}
	fmt.Fprintf(output, "ゲーム %d件の記録を削除しました。バックアップ: %s\n", deleted, backup)
	return nil
}

// backupPath is where clear and restore back up the records file at path.
func backupPath(path string, now func() time.Time) string {
	return path + ".bak-" + now().In(app.JST).Format("20060102-150405")
}

// restore replaces the games of the records file with those of a backup. It
// copies them into the file under its exclusive lock rather than replacing
// the file, so that SQLite applies a WAL file left by a crashed server to the
// file it belongs to, never to the backup.
func restore(args []string, input io.Reader, output io.Writer, now func() time.Time) (err error) {
	flags, path := newFlags("restore", output)
	from := flags.String("from", "", "backup file to restore, such as one that clear made")
	if err := parse(flags, args); err != nil {
		return err
	}
	if *from == "" {
		return errors.New("-from で戻すバックアップを指定してください")
	}
	target, err := os.Stat(*path)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("記録ファイル %s がありません。パスを確認してください（記録ファイルがない場所へ置くだけなら、サーバーを止めてバックアップをその名前で複製すれば足ります）", *path)
	}
	if err != nil {
		return err
	}
	source, err := os.Stat(*from)
	if err != nil {
		return fmt.Errorf("バックアップ %s を使えません: %w", *from, err)
	}
	if os.SameFile(source, target) {
		return fmt.Errorf("バックアップ %s は記録ファイルそのものです", *from)
	}
	games, err := loadBackup(*from)
	if err != nil {
		return fmt.Errorf("バックアップ %s を使えません: %w", *from, err)
	}

	store, err := openExclusive(*path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, store.Close()) }()
	current, err := store.Load()
	if err != nil {
		return fmt.Errorf("記録ファイル %s を読めません: %w", *path, err)
	}
	backup := backupPath(*path, now)
	fmt.Fprintf(output, "記録ファイル: %s（現在のゲーム %d件）\n", *path, len(current))
	fmt.Fprintf(output, "戻すバックアップ: %s\n", *from)
	writeHistogram(output, games)
	fmt.Fprintf(output, "\n記録ファイルのゲーム %d件を、バックアップのゲーム %d件に置き換えます。", len(current), len(games))
	if len(current) != 0 {
		fmt.Fprintf(output, "置き換えの前に、現在の記録を %s へバックアップします。", backup)
	}
	fmt.Fprintln(output)
	if err := confirm(input, output, fmt.Sprintf("RESTORE %d GAMES", len(games))); err != nil {
		return err
	}
	if len(current) != 0 {
		if err := store.Backup(backup); err != nil {
			return fmt.Errorf("現在の記録のバックアップを作れないため中止しました。記録は変更していません: %w", err)
		}
	}
	if err := store.ReplaceAll(games); err != nil {
		return fmt.Errorf("置き換えに失敗しました。記録は変更していません: %w", err)
	}
	fmt.Fprintf(output, "バックアップのゲーム %d件に置き換えました。\n", len(games))
	if len(current) != 0 {
		fmt.Fprintf(output, "置き換える前の記録: %s\n", backup)
	}
	return nil
}

// loadBackup validates and loads the backup at path from a temporary copy,
// since opening the backup itself could migrate it and switch it to WAL.
func loadBackup(path string) (games []app.GameRecord, err error) {
	// The copy would miss the part of the backup in its WAL file. A backup
	// that clear made has none; one appears only while something has the file
	// open, or after it crashed.
	if _, err := os.Lstat(path + "-wal"); err == nil {
		return nil, fmt.Errorf("%s-wal があります。バックアップを開いているプロセスを止めるか、正しく閉じてから実行してください", path)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "records-restore-")
	if err != nil {
		return nil, err
	}
	// The directory also holds the WAL files of the copy.
	defer func() { err = errors.Join(err, os.RemoveAll(dir)) }()
	copied := filepath.Join(dir, "records.sqlite3")
	if err := copyFile(path, copied); err != nil {
		return nil, err
	}
	store, err := records.Open(copied)
	if err != nil {
		return nil, err
	}
	games, err = store.Load()
	if err := errors.Join(err, store.Close()); err != nil {
		return nil, err
	}
	return games, nil
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	return errors.Join(err, out.Close())
}

// Defaults of the generated scores of seed.
const (
	defaultMean = 1400
	defaultSD   = 250
	defaultMin  = 600
	defaultMax  = 2200
	// maxSeedGames keeps a mistyped count from filling the file.
	maxSeedGames = 10000
	// maxDraws bounds the redraws of a score outside the range.
	maxDraws = 10000
)

// seedPlayer is the player of a seeded round: A.
const seedPlayer app.NodeID = 1

func seed(args []string, input io.Reader, output io.Writer, now func() time.Time) (err error) {
	flags, path := newFlags("seed", output)
	list := flags.String("scores", "", "comma-separated scores to add, in order (instead of -count)")
	count := flags.Int("count", 0, "number of scores to draw from a normal distribution (instead of -scores)")
	mean := flags.Float64("mean", defaultMean, "mean of the drawn scores")
	sd := flags.Float64("sd", defaultSD, "standard deviation of the drawn scores")
	low := flags.Int("min", defaultMin, "lowest drawn score; a draw outside [min, max] is drawn again")
	high := flags.Int("max", defaultMax, "highest drawn score")
	randSeed := flags.Uint64("rand-seed", 0, "seed of the draws (0: from the current time)")
	atText := flags.String("at", "", "finished_at of the first game in RFC 3339 (default: the start of the JST hour 24 hours ago); each later game is 1 second later")
	if err := parse(flags, args); err != nil {
		return err
	}
	current := now()
	var scores []int
	switch {
	case (*list == "") == (*count == 0):
		return errors.New("-scores と -count のどちらか一方を指定してください")
	case *list != "":
		scores, err = parseScores(*list)
	default:
		if *randSeed == 0 {
			*randSeed = uint64(current.UnixNano())
		}
		scores, err = drawScores(*count, *mean, *sd, *low, *high, *randSeed)
	}
	if err != nil {
		return err
	}
	at := current.Add(-24 * time.Hour).In(app.JST).Truncate(time.Hour)
	if *atText != "" {
		if at, err = time.Parse(time.RFC3339, *atText); err != nil {
			return fmt.Errorf("-at: %w", err)
		}
	}
	last := at.Add(time.Duration(len(scores)-1) * time.Second)
	if last.After(current) {
		return fmt.Errorf("最後のゲームの終了時刻 %s が現在より後です。-at に過去の時刻を指定してください", last.In(app.JST).Format(time.RFC3339))
	}
	games := make([]app.GameRecord, 0, len(scores))
	for i, score := range scores {
		games = append(games, seedGame(at.Add(time.Duration(i)*time.Second), score))
	}

	store, err := openExclusive(*path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, store.Close()) }()
	existing, err := store.Load()
	if err != nil {
		return fmt.Errorf("記録ファイル %s を読めません: %w", *path, err)
	}
	if len(existing) != 0 {
		return fmt.Errorf("記録ファイル %s にゲーム %d件の記録があります。seed は空の記録にだけ追加できます。先に clear を実行してください", *path, len(existing))
	}

	fmt.Fprintf(output, "記録ファイル: %s\n", *path)
	fmt.Fprintf(output, "追加するゲーム: %d件。1件ごとにプレイヤーA（P1）1人の通常ラウンド\n", len(games))
	fmt.Fprintf(output, "終了時刻: %s から1秒ずつ（%s まで）\n", at.In(app.JST).Format(time.RFC3339), last.In(app.JST).Format(time.RFC3339))
	if *count != 0 {
		fmt.Fprintf(output, "分布: 平均 %g、標準偏差 %g、範囲 %d〜%d、乱数のシード %d（同じ点数を作り直すには -rand-seed %d）\n", *mean, *sd, *low, *high, *randSeed, *randSeed)
	}
	lowest, highest, sum := scores[0], scores[0], 0
	for _, s := range scores {
		lowest, highest, sum = min(lowest, s), max(highest, s), sum+s
	}
	fmt.Fprintf(output, "点数: 最低 %d、最高 %d、平均 %.1f。通算のベストスコアは %d になります\n",
		lowest, highest, float64(sum)/float64(len(scores)), highest)
	writeHistogram(output, games)
	fmt.Fprintln(output)
	if err := confirm(input, output, fmt.Sprintf("SEED %d GAMES", len(games))); err != nil {
		return err
	}
	if err := store.SaveGames(games); err != nil {
		return fmt.Errorf("追加に失敗しました。記録は変更していません: %w", err)
	}
	fmt.Fprintf(output, "ゲーム %d件の記録を追加しました。\n", len(games))
	return nil
}

func parseScores(list string) ([]int, error) {
	fields := strings.Split(list, ",")
	if len(fields) > maxSeedGames {
		return nil, fmt.Errorf("-scores は %d件までです", maxSeedGames)
	}
	scores := make([]int, 0, len(fields))
	for _, field := range fields {
		score, err := strconv.Atoi(strings.TrimSpace(field))
		if err != nil || score < 0 || score > math.MaxInt32 {
			return nil, fmt.Errorf("-scores の %q は0以上の整数ではありません", field)
		}
		scores = append(scores, score)
	}
	return scores, nil
}

// drawScores draws count rounded scores from a normal distribution truncated
// to [low, high], so that the range does not pile scores up at its ends.
func drawScores(count int, mean, sd float64, low, high int, seed uint64) ([]int, error) {
	switch {
	case count < 1 || count > maxSeedGames:
		return nil, fmt.Errorf("-count は1〜%dにしてください", maxSeedGames)
	case low < 0 || low > high || high > math.MaxInt32:
		return nil, fmt.Errorf("-min と -max は 0 ≤ min ≤ max ≤ %d にしてください", math.MaxInt32)
	case math.IsNaN(mean) || mean < float64(low) || mean > float64(high):
		return nil, errors.New("-mean は -min 以上 -max 以下にしてください")
	case math.IsNaN(sd) || math.IsInf(sd, 0) || sd < 0:
		return nil, errors.New("-sd は0以上の有限の値にしてください")
	}
	r := rand.New(rand.NewPCG(seed, 0))
	scores := make([]int, 0, count)
	for range count {
		draw := 0
		for ; draw < maxDraws; draw++ {
			score := math.Round(mean + sd*r.NormFloat64())
			if score >= float64(low) && score <= float64(high) {
				scores = append(scores, int(score))
				break
			}
		}
		if draw == maxDraws {
			return nil, errors.New("範囲内の点数を作れません。-sd を小さくするか、-min・-max を広げてください")
		}
	}
	return scores, nil
}

// seedGame is a normal round of A that ran its full duration and scored
// score (server.md 3.5.8).
func seedGame(at time.Time, score int) app.GameRecord {
	return app.GameRecord{
		Kind: app.RecordRound, FinishedAt: at, Duration: app.GameDuration, Elapsed: app.GameDuration, Reason: app.FinishTimeout,
		Results: []app.PlayerResult{{Node: seedPlayer, Score: score, Eligible: true, Rank: 1}},
	}
}

// confirm asks the operator to type phrase and reports errCanceled for
// anything else, including no input.
func confirm(input io.Reader, output io.Writer, phrase string) error {
	fmt.Fprintf(output, "実行するには %s と入力してください（それ以外で中止）: ", phrase)
	line, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if strings.TrimSpace(line) != phrase {
		fmt.Fprintln(output)
		return errCanceled
	}
	return nil
}

// The score bands of histogram: every bandWidth points from 0, and bandHigh
// or more. Scores are never negative.
const (
	bandWidth = 100
	bandHigh  = 1500
	bandCount = bandHigh/bandWidth + 1
	barWidth  = 40
)

// band is the index of the band that contains score.
func band(score int) int {
	return min(score/bandWidth, bandCount-1)
}

func bandLabel(i int) string {
	if i == bandCount-1 {
		return fmt.Sprintf("%d~", bandHigh)
	}
	return fmt.Sprintf("%d-%d", i*bandWidth, (i+1)*bandWidth-1)
}

// population is the scores of the top percent (server.md 3.5.6): those of
// the eligible players of the games that ran the current full duration.
func population(games []app.GameRecord) (scores []int, counted int) {
	for _, r := range games {
		if r.Reason != app.FinishTimeout || r.Duration != app.GameDuration {
			continue
		}
		counted++
		for _, p := range r.Results {
			if p.Eligible {
				scores = append(scores, p.Score)
			}
		}
	}
	return scores, counted
}

func writeHistogram(w io.Writer, games []app.GameRecord) {
	scores, counted := population(games)
	var counts [bandCount]int
	for _, s := range scores {
		counts[band(s)]++
	}
	most := 0
	for _, n := range counts {
		most = max(most, n)
	}
	fmt.Fprintf(w, "対象: %d秒を最後まで行ったゲームの、ランキング対象者のスコア（上位%%の母集団と同じ）\n", int(app.GameDuration/time.Second))
	fmt.Fprintf(w, "ゲーム %d件のうち対象 %d件、スコア %d件\n\n", len(games), counted, len(scores))
	for i, n := range counts {
		percent, bar := 0.0, 0
		if len(scores) != 0 {
			percent = 100 * float64(n) / float64(len(scores))
		}
		if n != 0 {
			bar = max(1, n*barWidth/most)
		}
		fmt.Fprintf(w, "%9s %6d %5.1f%% %s\n", bandLabel(i), n, percent, strings.Repeat("#", bar))
	}
	fmt.Fprintf(w, "%9s %6d\n", "total", len(scores))
}
