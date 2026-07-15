package game

import (
	"fmt"
	"sort"
)

// ─── Roll coverage ──────────────────────────────────────────────────────────
//
// Weighted lists and loot tables share one rule: their ranges must TILE the die
// — cover 1..maxRoll with no gaps and no overlaps, so every possible roll lands
// on exactly one outcome.
//
// Gaps are an ERROR, not an implicit "nothing happens". A no-drop chance is a
// thing you say out loud (a table's `nothing` row), because a hole in the ranges
// is indistinguishable from a typo — and the one gap that ever shipped
// (raider_loot's 51-60, a deliberate 10% no-drop) read exactly like one.

// rollRange is one claimed slice of a die, labelled for error messages.
type rollRange struct {
	Min, Max int
	Label    string // what this range yields, e.g. an item id or "nothing"
}

// validateRollCoverage checks that ranges tile 1..maxRoll exactly. `what` and
// `id` only shape the error message (e.g. "list", "basic_weapons").
func validateRollCoverage(what, id string, maxRoll int, ranges []rollRange) error {
	if maxRoll < 1 {
		return fmt.Errorf("%s %q: maxRoll must be at least 1, got %d", what, id, maxRoll)
	}
	if len(ranges) == 0 {
		return fmt.Errorf("%s %q: needs at least 1 entry", what, id)
	}

	for _, r := range ranges {
		if r.Min < 1 || r.Max > maxRoll {
			return fmt.Errorf("%s %q: %s claims rolls %d-%d, outside the die 1-%d",
				what, id, r.Label, r.Min, r.Max, maxRoll)
		}
		if r.Min > r.Max {
			return fmt.Errorf("%s %q: %s claims rolls %d-%d, which is backwards",
				what, id, r.Label, r.Min, r.Max)
		}
	}

	// Sort a copy so the author's authored order is not a correctness constraint.
	sorted := append([]rollRange(nil), ranges...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Min < sorted[j].Min })

	next := 1 // the lowest roll not yet claimed
	for _, r := range sorted {
		if r.Min > next {
			return fmt.Errorf("%s %q: rolls %d-%d land on nothing — every roll must be covered "+
				"(to mean \"nothing happens\", say so with an explicit entry)",
				what, id, next, r.Min-1)
		}
		if r.Min < next {
			overlapEnd := r.Max
			if next-1 < overlapEnd {
				overlapEnd = next - 1
			}
			return fmt.Errorf("%s %q: rolls %d-%d are claimed twice (%s overlaps the range before it)",
				what, id, r.Min, overlapEnd, r.Label)
		}
		next = r.Max + 1
	}
	if next <= maxRoll {
		return fmt.Errorf("%s %q: rolls %d-%d land on nothing — every roll must be covered "+
			"(to mean \"nothing happens\", say so with an explicit entry)",
			what, id, next, maxRoll)
	}
	return nil
}

// pickRange returns the index of the range containing roll, or -1. Coverage is
// validated at load, so -1 is unreachable for a loaded def — callers still guard.
func pickRange(ranges []rollRange, roll int) int {
	for i, r := range ranges {
		if roll >= r.Min && roll <= r.Max {
			return i
		}
	}
	return -1
}
