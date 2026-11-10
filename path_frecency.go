package ochazuke

import (
	"slices"
	"time"
)

// Frecency for the path picker's recent places: how often and how recently a
// folder was chosen. The age brackets and the cap are those of tuios's
// applist (pkg/applist/frecency.go), copied as logic: a boost, capped,
// so frecency orders places that are otherwise alike and never makes a
// poor one outrank a good one.

// PathUse is a place the application knows was chosen: how many times, and
// when last (mtm's dir_recent Uses and LastUsed).
type PathUse struct {
	Path     string
	Uses     int
	LastUsed time.Time
}

// PathMaxBoost caps what use and age add to a place's rank.
const PathMaxBoost = 32

// PathBoost is what a place's history adds to its rank, between 0 and
// PathMaxBoost: its use count times a weight that falls with age (8 within
// the hour, 4 within the day, 2 within the week, then 1).
func PathBoost(uses int, last, now time.Time) int {
	if uses <= 0 {
		return 0
	}
	age := now.Sub(last)
	var s int
	switch {
	case age < time.Hour:
		s = uses * 8
	case age < 24*time.Hour:
		s = uses * 4
	case age < 7*24*time.Hour:
		s = uses * 2
	default:
		s = uses
	}
	return min(s, PathMaxBoost)
}

// SetRecentUses gives the recent source the use count and last use of the
// places it knows (SetRecentPaths lists them): the recent source then lists
// them by PathBoost, newest first among equals, and the hybrid style's
// Recent section too.
func (r *Runtime) SetRecentUses(fn func(PathPick) []PathUse) {
	r.chooserMemory().recentUses = fn
}

func (r *Runtime) now() time.Time { return time.Now() }

// frecentOrder is paths ordered by boost, the given order kept among equal
// boosts; a place only uses names follows, strongest first.
func frecentOrder(paths []string, uses []PathUse, now time.Time) []string {
	boost := map[string]int{}
	for _, u := range uses {
		boost[u.Path] = PathBoost(u.Uses, u.LastUsed, now)
	}
	out := slices.Clone(paths)
	slices.SortStableFunc(out, func(a, b string) int { return boost[b] - boost[a] })
	have := map[string]bool{}
	for _, p := range out {
		have[p] = true
	}
	extra := slices.Clone(uses)
	slices.SortStableFunc(extra, func(a, b PathUse) int { return boost[b.Path] - boost[a.Path] })
	for _, u := range extra {
		if !have[u.Path] && u.Path != "" {
			have[u.Path] = true
			out = append(out, u.Path)
		}
	}
	return out
}
