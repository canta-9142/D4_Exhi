package app

// maxDeviceTopPercent is the largest top percent sent to a player MCU; anyone
// below it is told only that they are in the bottom half (player.md).
const maxDeviceTopPercent = 50

// topPercent is the top percent of score within population, which includes
// the score itself: ceil(100 * (scores above it + 1) / population), kept in
// 1-100. A tie shares a value, since only higher scores count. Any population
// size gives one (rules.md), but an empty population reports false.
func topPercent(population []int, score int) (int, bool) {
	n := len(population)
	if n == 0 {
		return 0, false
	}
	higher := 0
	for _, s := range population {
		if s > score {
			higher++
		}
	}
	percent := (100*(higher+1) + n - 1) / n
	return min(max(percent, 1), 100), true
}

// settleTopPercent sets the top percent of each eligible player of r, which
// has already been added to the population. It sets none for a game that is
// not part of the population, so that a manual Finish gives nobody a percent.
func (a *App) settleTopPercent(r GameRecord) {
	if !r.counted() {
		return
	}
	for _, p := range r.Results {
		if !p.Eligible {
			continue
		}
		if percent, ok := topPercent(a.bests.scores, p.Score); ok {
			a.game.nodes[p.Node].topPercent = percent
		}
	}
}

// deviceTopPercent is the top percent in n's summary: nil unless the records
// file was usable at startup (otherwise the population holds only this
// process's games) and n is within maxDeviceTopPercent.
func (a *App) deviceTopPercent(n *node) *int {
	if a.options.RecordsUnavailable != "" || n.topPercent > maxDeviceTopPercent {
		return nil
	}
	return optionalPercent(n.topPercent)
}

// deviceBottomHalf reports whether n's summary says only that n is in the
// bottom half, hiding the value. Like deviceTopPercent, it needs the records
// file, so that a player is not told so from this process's games alone.
func (a *App) deviceBottomHalf(n *node) bool {
	return a.options.RecordsUnavailable == "" && n.topPercent > maxDeviceTopPercent
}

// optionalPercent is a copy of percent for a Snapshot or message, so that it
// does not alias game state, or nil for 0 (none).
func optionalPercent(percent int) *int {
	if percent == 0 {
		return nil
	}
	return &percent
}
