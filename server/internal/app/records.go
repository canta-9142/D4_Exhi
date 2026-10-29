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

// GameRecord is the durable result of one finished game (server.md 3.5.1).
type GameRecord struct {
	// FinishedAt is the wall-clock time at which the game clock stopped: its
	// origin plus the settled elapsed time.
	FinishedAt time.Time
	// Duration is the rule's game length when the game was played.
	Duration time.Duration
	Elapsed  time.Duration
	Reason   FinishReason
	// Results holds P1-P8 in node order, including ineligible players.
	Results []PlayerResult
}

// PlayerResult is a player's settled result. Rank is 1-8 for an eligible
// player and 0 otherwise.
type PlayerResult struct {
	Node     NodeID
	Score    int
	Eligible bool
	Rank     int
}

// record builds the GameRecord of a game that has just finished, from the
// same settled state as its ranks.
func (g *game) record() GameRecord {
	r := GameRecord{
		// Round(0) drops the monotonic reading: the record is a wall-clock time.
		FinishedAt: g.started.Add(g.elapsed).Round(0),
		Duration:   GameDuration, Elapsed: g.elapsed, Reason: g.reason,
		Results: make([]PlayerResult, 0, PlayerCount),
	}
	for id := NodeID(1); id <= PlayerCount; id++ {
		n := &g.nodes[id]
		r.Results = append(r.Results, PlayerResult{Node: id, Score: n.score, Eligible: n.eligible, Rank: n.rank})
	}
	return r
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
// which the hourly and all-time bests and the top percent require.
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

// deviceBests is what a finished game sends of the hourly and all-time
// bests in each summary (server.md 3.5.7), settled once at Finished.
type deviceBests struct {
	hour    *HourlyBest // The best of the game's hour; nil for none.
	allTime *BestScore
	// hourMine and allTimeMine report whether the game set those bests, so
	// that its nodes in them are sent *Mine.
	hourMine, allTimeMine bool
}

// settleDeviceBests keeps the bests of the game r for summaries, after r has
// been added to b. It keeps none if the records file was unusable at
// startup, since the bests then count only this process's games.
func (a *App) settleDeviceBests(r GameRecord, hourSet, allTimeSet bool) {
	a.deviceBests = deviceBests{}
	if a.options.RecordsUnavailable != "" {
		return
	}
	if i, found := a.bests.findHour(hourStart(r.FinishedAt)); found {
		h := a.bests.hourly[i]
		h.Nodes = slices.Clone(h.Nodes)
		a.deviceBests.hour, a.deviceBests.hourMine = &h, hourSet
	}
	a.deviceBests.allTime, a.deviceBests.allTimeMine = a.bests.allTime.clone(), allTimeSet
}

// setSummaryBests sets the best scores of node's summary.
func (a *App) setSummaryBests(data *SummaryData, node NodeID) {
	data.GameBestNodes = []int{}
	if b := a.gameBest; b != nil {
		data.GameBest = optionalScore(b.Score)
		for _, id := range b.Nodes {
			data.GameBestNodes = append(data.GameBestNodes, int(id))
		}
	}
	if h := a.deviceBests.hour; h != nil {
		start := h.HourStart.In(JST).Hour()
		data.HourBest, data.HourBestStart = optionalScore(h.Score), &start
		data.HourBestMine = a.deviceBests.hourMine && slices.Contains(h.Nodes, node)
	}
	if b := a.deviceBests.allTime; b != nil {
		data.AllTimeBest = optionalScore(b.Score)
		data.AllTimeBestMine = a.deviceBests.allTimeMine && slices.Contains(b.Nodes, node)
	}
}

// optionalScore is a copy of score for a message, so that it does not alias
// the bests.
func optionalScore(score int) *int {
	return &score
}

// recordGame saves and counts the game that has just finished. A failed save
// leaves the game going and still counts in memory; the Snapshot reports it
// until a later save succeeds. Nothing is retried.
func (a *App) recordGame() {
	r := a.game.record()
	if a.options.RecordsUnavailable == "" && a.options.SaveGame != nil {
		err := a.options.SaveGame(r)
		a.saveFailed = err != nil
		if err != nil {
			a.options.Logger.Error("cannot save game record", "finished_at", r.FinishedAt, "reason", r.Reason, "error", err)
		}
	}
	a.gameBest = r.best()
	hourSet, allTimeSet := a.bests.add(r)
	a.settleTopPercent(r)
	a.settleDeviceBests(r, hourSet, allTimeSet)
}

func (a *App) recordsSnapshot() RecordsSnapshot {
	s := RecordsSnapshot{
		GameBest: a.gameBest.clone(), HourlyBests: make([]HourlyBest, 0, len(a.bests.hourly)), AllTimeBest: a.bests.allTime.clone(),
		Available: a.options.RecordsUnavailable == "", UnavailableReason: a.options.RecordsUnavailable, SaveFailed: a.saveFailed,
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
