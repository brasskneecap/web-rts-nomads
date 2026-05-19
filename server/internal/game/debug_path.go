package game

import (
	"fmt"
	"log"
	"math"
	"os"
	"runtime"
	"sort"
)

// debugPathingEnabled is evaluated once at startup. Set WEBRTS_DEBUG_PATHING=1
// before launching the server to enable the path-debug analyzer.
var debugPathingEnabled = os.Getenv("WEBRTS_DEBUG_PATHING") == "1"

// unitPathDebugStats tracks per-unit re-path activity within the current
// reporting window (lastReportTick … now).
type unitPathDebugStats struct {
	repathCount    int
	windowStartX   float64
	windowStartY   float64
	windowStartTick int
}

// debugPathTracker accumulates counters across each reporting window. All
// methods no-op when the receiver is nil, so callers never need a guard at
// the call site when the feature is disabled.
type debugPathTracker struct {
	unitStats          map[int]*unitPathDebugStats
	totalCoarsePathCalls int
	totalFinePathCalls   int
	// Failed A* is the expensive case: it exhausts the whole reachable
	// component before returning nil, where a successful search terminates
	// early at the goal. These split the call totals above into the subset
	// that returned no route.
	totalCoarsePathFails int
	totalFinePathFails   int
	// unreachableRetargets counts "a unit target became unreachable" events
	// (assignAttackApproachPath gave up → drift or drop+memo). Enemy split is
	// tracked separately to confirm/deny the "enemies chasing player units
	// through a blob" hypothesis.
	unreachableRetargets      int
	unreachableRetargetsEnemy int
	// unitPathBudgetHits counts fine A* searches aborted by the node-expansion
	// budget (treated as "no route"). This is the bound that caps the ~70ms
	// exhaustive-search freezes; tracking it lets the tripwire re-measure show
	// how often the cap fires (and flags a too-tight budget cutting reachable
	// routes if it climbs on open maps).
	unitPathBudgetHits int
	callerCounts       map[string]int
	lastReportTick     int
}

// newDebugPathTracker returns nil when the feature is disabled, keeping every
// hot path at a single nil-check branch with no allocation cost.
func newDebugPathTracker() *debugPathTracker {
	if !debugPathingEnabled {
		return nil
	}
	log.Printf("[debug-path] enabled — set WEBRTS_DEBUG_PATHING=0 to disable")
	return &debugPathTracker{
		unitStats:    make(map[int]*unitPathDebugStats),
		callerCounts: make(map[string]int),
	}
}

// recordCoarsePath increments the coarse (terrain A*) call counter.
func (t *debugPathTracker) recordCoarsePath() {
	if t == nil {
		return
	}
	t.totalCoarsePathCalls++
}

// recordFinePath increments the fine (sub-cell A*) call counter.
func (t *debugPathTracker) recordFinePath() {
	if t == nil {
		return
	}
	t.totalFinePathCalls++
}

// recordCoarseFail increments the coarse (terrain A*) no-route counter. Call
// when findPath returns nil — the search exhausted the reachable component.
func (t *debugPathTracker) recordCoarseFail() {
	if t == nil {
		return
	}
	t.totalCoarsePathFails++
}

// recordFineFail increments the fine (sub-cell A*) no-route counter. Call when
// findUnitPath returns nil — the most expensive pathing outcome in the sim.
func (t *debugPathTracker) recordFineFail() {
	if t == nil {
		return
	}
	t.totalFinePathFails++
}

// recordUnitPathBudgetHit records one fine A* aborted by the expansion budget.
func (t *debugPathTracker) recordUnitPathBudgetHit() {
	if t == nil {
		return
	}
	t.unitPathBudgetHits++
}

// recordUnreachableRetarget records one "target became unreachable" event
// (enterAttackDrift / dropUnreachableAITarget). isEnemy attributes it to the
// wave AI so the report can isolate enemy-vs-player retarget churn.
func (t *debugPathTracker) recordUnreachableRetarget(isEnemy bool) {
	if t == nil {
		return
	}
	t.unreachableRetargets++
	if isEnemy {
		t.unreachableRetargetsEnemy++
	}
}

// recordRepath records one re-path event for unitID at the given world
// position and tick. If this is the first event for the unit in this window,
// the window origin is initialised to (x, y, tick).
func (t *debugPathTracker) recordRepath(unitID int, x, y float64, tick int) {
	if t == nil {
		return
	}
	stats, ok := t.unitStats[unitID]
	if !ok {
		stats = &unitPathDebugStats{
			windowStartX:    x,
			windowStartY:    y,
			windowStartTick: tick,
		}
		t.unitStats[unitID] = stats
	}
	stats.repathCount++

	// Walk up the stack past assignUnitPath itself to find the real caller.
	// Frame 0 = this function, 1 = assignUnitPath, 2 = the actual caller.
	if _, file, line, ok := runtime.Caller(2); ok {
		key := fmt.Sprintf("%s:%d", trimRepoPrefix(file), line)
		t.callerCounts[key]++
	}
}

func trimRepoPrefix(path string) string {
	const marker = "internal/game/"
	if i := indexOf(path, marker); i >= 0 {
		return path[i+len(marker):]
	}
	return path
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// suspicious bundles the fields we log for a stuck unit.
type suspicious struct {
	unitID                 int
	ownerID                string
	orderType              OrderType
	formationOrderID       int64
	attackTargetID         int
	attackBuildingTargetID string
	repathCount            int
	moved                  float64
	targDist               float64
	pathLen                int
}

// reportPathDebugLocked prints a one-line summary every ~1 second (20 ticks)
// and per-unit detail for units that appear stuck. Must be called under s.mu.
func (s *GameState) reportPathDebugLocked() {
	t := s.debugPathTracker
	if t == nil {
		return
	}
	if s.Tick-t.lastReportTick < 20 {
		return
	}

	var suspects []suspicious
	aliveUnitCount := 0

	for _, unit := range s.Units {
		if unit == nil || unit.HP <= 0 || !unit.Visible {
			continue
		}
		aliveUnitCount++

		stats, ok := t.unitStats[unit.ID]
		if !ok {
			continue
		}

		windowTicks := s.Tick - stats.windowStartTick
		if windowTicks <= 0 {
			continue
		}

		dx := unit.X - stats.windowStartX
		dy := unit.Y - stats.windowStartY
		moved := math.Hypot(dx, dy)

		if stats.repathCount >= 3 && moved < 40 {
			var targDist float64
			if unit.AttackTargetID != 0 {
				target := s.getUnitByIDLocked(unit.AttackTargetID)
				if target != nil {
					targDist = math.Hypot(unit.X-target.X, unit.Y-target.Y)
				}
			}
			suspects = append(suspects, suspicious{
				unitID:                 unit.ID,
				ownerID:                unit.OwnerID,
				orderType:              unit.Order.Type,
				formationOrderID:       unit.OrderID,
				attackTargetID:         unit.AttackTargetID,
				attackBuildingTargetID: unit.AttackBuildingTargetID,
				repathCount:            stats.repathCount,
				moved:                  moved,
				targDist:               targDist,
				pathLen:                len(unit.Path),
			})
		}
	}

	log.Printf("[debug-path tick=%d coarse=%d(fail=%d) fine=%d(fail=%d) budgetHit=%d unreachRetarget=%d(enemy=%d) suspicious=%d/%d-units]",
		s.Tick, t.totalCoarsePathCalls, t.totalCoarsePathFails,
		t.totalFinePathCalls, t.totalFinePathFails, t.unitPathBudgetHits,
		t.unreachableRetargets, t.unreachableRetargetsEnemy,
		len(suspects), aliveUnitCount)

	// Top assignUnitPath callers, hottest first.
	type callerHit struct {
		site  string
		count int
	}
	hits := make([]callerHit, 0, len(t.callerCounts))
	for site, count := range t.callerCounts {
		hits = append(hits, callerHit{site, count})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].count > hits[j].count })
	hitLimit := len(hits)
	if hitLimit > 5 {
		hitLimit = 5
	}
	for _, h := range hits[:hitLimit] {
		log.Printf("  caller %-50s %d", h.site, h.count)
	}

	limit := len(suspects)
	if limit > 5 {
		limit = 5
	}
	for _, su := range suspects[:limit] {
		log.Printf("  unit=%d owner=%s order=%d formID=%d targetUnit=%d targetBldg=%q repaths=%d moved=%.0fpx targDist=%.0f pathLen=%d",
			su.unitID, su.ownerID, su.orderType, su.formationOrderID, su.attackTargetID, su.attackBuildingTargetID,
			su.repathCount, su.moved, su.targDist, su.pathLen)
	}

	// Reset counters and advance the window for all tracked units. Drop entries
	// for units that are no longer alive so the map doesn't grow unboundedly.
	t.totalCoarsePathCalls = 0
	t.totalFinePathCalls = 0
	t.totalCoarsePathFails = 0
	t.totalFinePathFails = 0
	t.unreachableRetargets = 0
	t.unreachableRetargetsEnemy = 0
	t.unitPathBudgetHits = 0
	for k := range t.callerCounts {
		delete(t.callerCounts, k)
	}
	t.lastReportTick = s.Tick

	for id, stats := range t.unitStats {
		unit := s.getUnitByIDLocked(id)
		if unit == nil || unit.HP <= 0 {
			delete(t.unitStats, id)
			continue
		}
		stats.repathCount = 0
		stats.windowStartX = unit.X
		stats.windowStartY = unit.Y
		stats.windowStartTick = s.Tick
	}
}
