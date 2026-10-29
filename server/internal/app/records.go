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

// bests holds the hourly and all-time best scores of rules.md. Records are
// added in save order and a tie never replaces a best, so the earlier game
// keeps it.
type bests struct {
	hourly  []HourlyBest // Newest hour first.
	allTime *BestScore
}

// add counts r if it ran its full duration under the current rule.
func (b *bests) add(r GameRecord) {
	if r.Reason != FinishTimeout || r.Duration != GameDuration {
		return
	}
	best := r.best()
	if best == nil {
		return
	}
	if b.allTime == nil || best.Score > b.allTime.Score {
		b.allTime = best
	}
	t := best.FinishedAt.In(JST)
	hour := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, JST)
	i, found := slices.BinarySearchFunc(b.hourly, hour, func(h HourlyBest, hour time.Time) int { return hour.Compare(h.HourStart) })
	switch {
	case !found:
		b.hourly = slices.Insert(b.hourly, i, HourlyBest{HourStart: hour, BestScore: *best})
	case best.Score > b.hourly[i].Score:
		b.hourly[i].BestScore = *best
	}
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
	a.bests.add(r)
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
