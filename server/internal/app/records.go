package app

import (
	"slices"
	"time"
)

// JST is the fixed UTC+9 zone that divides the hourly best scores
// (server.md 3.5.5). Unlike a tzdata location it never observes daylight
// saving time.
var JST = time.FixedZone("JST", 9*60*60)

// FinishReason tells how a game reached Finished.
type FinishReason string

const (
	FinishTimeout FinishReason = "timeout" // The game duration ran out.
	FinishManual  FinishReason = "manual"  // An admin Finish command.
)

// RecordKind tells a normal round from a simultaneous game (server.md 3.5.1).
type RecordKind string

const (
	RecordRound RecordKind = "round"
	RecordGame  RecordKind = "game"
)

// maxRecentRounds is how many recent rounds the Snapshot lists.
const maxRecentRounds = 10

// GameRecord is the durable result of one normal round or simultaneous game
// (server.md 3.5.1).
type GameRecord struct {
	Kind RecordKind
	// FinishedAt is the wall-clock time at which the game clock stopped: its
	// origin plus the settled elapsed time.
	FinishedAt time.Time
	// Duration is the rule's game length when the game was played.
	Duration time.Duration
	Elapsed  time.Duration
	Reason   FinishReason
	// Results holds the players in node order: the one player of a round, or
	// P1-P8 of a simultaneous game, including ineligible players.
	Results []PlayerResult
}

// PlayerResult is a player's settled result. Rank is 1-8 for an eligible
// player and 0 otherwise; a round's player has rank 1, which is no rank of
// the rules.
type PlayerResult struct {
	Node     NodeID
	Score    int
	Eligible bool
	Rank     int
}

// RecentRound is a normal round settled since startup (server.md 3.5.5).
type RecentRound struct {
	Node       NodeID    `json:"node"`
	Score      int       `json:"score"`
	TopPercent int       `json:"top_percent"`
	FinishedAt time.Time `json:"finished_at"`
}

// record builds the GameRecord of the simultaneous game that has just
// finished, from the same settled state as its ranks.
func (g *game) record() GameRecord {
	r := GameRecord{
		Kind: RecordGame,
		// Round(0) drops the monotonic reading: the record is a wall-clock time.
		FinishedAt: g.origin.Add(g.elapsed).Round(0),
		Duration:   GameDuration, Elapsed: g.elapsed, Reason: g.reason,
		Results: make([]PlayerResult, 0, PlayerCount),
	}
	for id := NodeID(1); id <= PlayerCount; id++ {
		n := &g.nodes[id]
		r.Results = append(r.Results, PlayerResult{Node: id, Score: n.score, Eligible: n.eligible, Rank: n.rank})
	}
	return r
}

// roundRecord builds the GameRecord of the normal round of id that has just
// ended. It finishes at its game time's end, however late the tick that ends
// it.
func (g *game) roundRecord(id NodeID) GameRecord {
	n := &g.nodes[id]
	return GameRecord{
		Kind: RecordRound, FinishedAt: g.origin.Add(n.roundEnd).Round(0),
		Duration: GameDuration, Elapsed: GameDuration, Reason: FinishTimeout,
		Results: []PlayerResult{{Node: id, Score: n.score, Eligible: true, Rank: 1}},
	}
}

// best is the highest eligible score of r and every node that has it, or nil
// if no player was eligible.
func (r GameRecord) best() *BestScore {
	var best *BestScore
	for _, p := range r.Results {
		switch {
		case !p.Eligible:
		case best == nil || p.Score > best.Score:
			best = &BestScore{Score: p.Score, Nodes: []NodeID{p.Node}, FinishedAt: r.FinishedAt}
		case p.Score == best.Score:
			best.Nodes = append(best.Nodes, p.Node)
		}
	}
	return best
}

// counted reports whether r ran its full duration under the current rule,
// which the hourly and all-time bests and the top percent require. Rounds and
// simultaneous games count alike.
func (r GameRecord) counted() bool {
	return r.Reason == FinishTimeout && r.Duration == GameDuration
}

// bests holds the hourly and all-time best scores of rules.md. Records are
// added in save order and a tie never replaces a best, so the earlier game
// keeps it.
type bests struct {
	hourly  []HourlyBest // Newest hour first.
	allTime *BestScore
	// scores is every eligible score of the counted games, the population of
	// the top percent. It is in no particular order.
	scores []int
}

// add counts r if it ran its full duration under the current rule. It
// reports whether r set the best of its hour and the all-time best.
func (b *bests) add(r GameRecord) (hour, allTime bool) {
	if !r.counted() {
		return false, false
	}
	for _, p := range r.Results {
		if p.Eligible {
			b.scores = append(b.scores, p.Score)
		}
	}
	best := r.best()
	if best == nil {
		return false, false
	}
	if b.allTime == nil || best.Score > b.allTime.Score {
		b.allTime, allTime = best, true
	}
	start := hourStart(best.FinishedAt)
	i, found := b.findHour(start)
	switch {
	case !found:
		b.hourly = slices.Insert(b.hourly, i, HourlyBest{HourStart: start, BestScore: *best})
		hour = true
	case best.Score > b.hourly[i].Score:
		b.hourly[i].BestScore = *best
		hour = true
	}
	return hour, allTime
}

// hourStart is the start of the JST hour that contains t.
func hourStart(t time.Time) time.Time {
	t = t.In(JST)
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, JST)
}

// findHour is the index of the hour that begins at start in b.hourly, or
// where to insert it, as slices.BinarySearchFunc reports.
func (b *bests) findHour(start time.Time) (int, bool) {
	return slices.BinarySearchFunc(b.hourly, start, func(h HourlyBest, start time.Time) int { return start.Compare(h.HourStart) })
}

// deviceBests is what a settled result sends of the hourly and all-time
// bests in its summary (server.md 3.5.7), settled once with the result.
type deviceBests struct {
	hour    *HourlyBest // The best of the result's hour; nil for none.
	allTime *BestScore
	// hourMine and allTimeMine report whether the result set those bests, so
	// that its nodes in them are sent *Mine.
	hourMine, allTimeMine bool
}

// settleDeviceBests is the bests of the record r for summaries, after r has
// been added to a.bests. It is none if the records file was unusable at
// startup, since the bests then count only this process's games.
func (a *App) settleDeviceBests(r GameRecord, hourSet, allTimeSet bool) deviceBests {
	var d deviceBests
	if a.options.RecordsUnavailable != "" {
		return d
	}
	if i, found := a.bests.findHour(hourStart(r.FinishedAt)); found {
		h := a.bests.hourly[i]
		h.Nodes = slices.Clone(h.Nodes)
		d.hour, d.hourMine = &h, hourSet
	}
	d.allTime, d.allTimeMine = a.bests.allTime.clone(), allTimeSet
	return d
}

// setSummaryBests sets the best scores of node's summary. A round's best of
// its own is the player's score (server.md 3.5.7).
func (a *App) setSummaryBests(data *SummaryData, node NodeID) {
	n := &a.game.nodes[node]
	data.GameBestNodes = []int{}
	best := a.gameBest
	if a.game.mode == Endless {
		best = &BestScore{Score: n.score, Nodes: []NodeID{node}}
	}
	if best != nil {
		data.GameBest = optionalScore(best.Score)
		for _, id := range best.Nodes {
			data.GameBestNodes = append(data.GameBestNodes, int(id))
		}
	}
	if h := n.bests.hour; h != nil {
		start := h.HourStart.In(JST).Hour()
		data.HourBest, data.HourBestStart = optionalScore(h.Score), &start
	}
	if b := n.bests.allTime; b != nil {
		data.AllTimeBest = optionalScore(b.Score)
	}
	data.HourBestMine, data.AllTimeBestMine = n.bests.mine(node)
}

// mine reports whether node is in the hourly and all-time bests that the
// result set, as its summary's hourBestMine and allTimeBestMine.
func (b deviceBests) mine(node NodeID) (hour, allTime bool) {
	hour = b.hourMine && b.hour != nil && slices.Contains(b.hour.Nodes, node)
	allTime = b.allTimeMine && b.allTime != nil && slices.Contains(b.allTime.Nodes, node)
	return hour, allTime
}

// cheerStyle is the CHEER style of node, a node of the result's best: that
// of the highest best it set (server.md 5.3).
func (b deviceBests) cheerStyle(node NodeID) string {
	hour, allTime := b.mine(node)
	switch {
	case allTime:
		return "record"
	case hour:
		return "hour"
	}
	return "game"
}

// optionalScore is a copy of score for a message, so that it does not alias
// the bests.
func optionalScore(score int) *int {
	return &score
}

// save saves r. A failed save leaves the game going and still counts in
// memory; the Snapshot reports it until a later save succeeds. Nothing is
// retried.
func (a *App) save(r GameRecord) {
	if a.options.RecordsUnavailable != "" || a.options.SaveGame == nil {
		return
	}
	err := a.options.SaveGame(r)
	a.saveFailed = err != nil
	if err != nil {
		a.options.Logger.Error("cannot save game record", "kind", r.Kind, "finished_at", r.FinishedAt, "reason", r.Reason, "error", err)
	}
}

// recordRound saves and counts the normal round of node that has just
// ended, then settles its top percent and bests and cheers it, all before
// any later round of the same input (server.md 3.5.1).
func (a *App) recordRound(node NodeID) {
	r := a.game.roundRecord(node)
	a.save(r)
	hourSet, allTimeSet := a.bests.add(r)
	a.settleTopPercent(r)
	n := &a.game.nodes[node]
	n.bests = a.settleDeviceBests(r, hourSet, allTimeSet)
	a.recentRounds = slices.Insert(a.recentRounds, 0, RecentRound{Node: node, Score: n.score, TopPercent: n.topPercent, FinishedAt: r.FinishedAt})
	if len(a.recentRounds) > maxRecentRounds {
		a.recentRounds = a.recentRounds[:maxRecentRounds]
	}
	a.sendCheers(r.best(), n.bests)
}

// recordGame saves and counts the simultaneous game that has just finished.
func (a *App) recordGame() {
	r := a.game.record()
	a.save(r)
	a.gameBest = r.best()
	hourSet, allTimeSet := a.bests.add(r)
	a.settleTopPercent(r)
	bests := a.settleDeviceBests(r, hourSet, allTimeSet)
	for id := NodeID(1); id <= PlayerCount; id++ {
		a.game.nodes[id].bests = bests
	}
	a.sendCheers(a.gameBest, bests)
}

func (a *App) recordsSnapshot() RecordsSnapshot {
	s := RecordsSnapshot{
		GameBest: a.gameBest.clone(), RecentRounds: slices.Clone(a.recentRounds), HourlyBests: make([]HourlyBest, 0, len(a.bests.hourly)),
		AllTimeBest: a.bests.allTime.clone(), Available: a.options.RecordsUnavailable == "",
		UnavailableReason: a.options.RecordsUnavailable, SaveFailed: a.saveFailed,
	}
	if s.RecentRounds == nil {
		s.RecentRounds = []RecentRound{}
	}
	for _, h := range a.bests.hourly {
		h.Nodes = slices.Clone(h.Nodes)
		s.HourlyBests = append(s.HourlyBests, h)
	}
	return s
}

func (b *BestScore) clone() *BestScore {
	if b == nil {
		return nil
	}
	c := *b
	c.Nodes = slices.Clone(b.Nodes)
	return &c
}
