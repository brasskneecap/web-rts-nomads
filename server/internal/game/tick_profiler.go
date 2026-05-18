package game

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Tick profiler — opt-in performance instrumentation for the simulation loop.
//
// Enable with WEBRTS_TICK_PROFILE=1. When disabled, every entry point is a
// fast no-op (the env var is read once and cached as a bool), so leaving the
// instrumentation in place has no production cost. When enabled, each
// labelled section in Update accumulates wall-time, and a summary is logged
// to stdout every profileLogIntervalTicks (5s at 20 Hz).

const profileLogIntervalTicks = 100

// defaultSlowTickThreshold: a single tick whose largest instrumented section
// meets/exceeds this gets logged immediately with that tick's full breakdown.
// 8ms is ~16% of the 50ms (20 Hz) tick budget — far above the sub-ms steady
// state, low enough to catch any human-visible freeze. Override with
// WEBRTS_TICK_SLOW_MS; set it to 0 to disable the tripwire (window summary
// still prints).
const defaultSlowTickThreshold = 8 * time.Millisecond

var (
	profilerEnableOnce sync.Once
	profilerEnabled    bool
	profilerSlowTick   time.Duration // 0 = tripwire disabled

	profilerMu      sync.Mutex
	profilerTotals  = map[string]time.Duration{}
	profilerSamples = map[string]int{}
	profilerMax     = map[string]time.Duration{}
	profilerTicks   int
	// profilerTickTotals accumulates the CURRENT tick only; reset every tick
	// in profileTickComplete. The window maps above can't catch a one-tick
	// freeze (a 100-tick average dilutes it ~100x); this per-tick view is the
	// tripwire's input.
	profilerTickTotals = map[string]time.Duration{}
)

func tickProfileEnabled() bool {
	profilerEnableOnce.Do(func() {
		v := strings.ToLower(strings.TrimSpace(os.Getenv("WEBRTS_TICK_PROFILE")))
		profilerEnabled = v == "1" || v == "true" || v == "yes" || v == "on"
		if !profilerEnabled {
			return
		}
		profilerSlowTick = defaultSlowTickThreshold
		if raw := strings.TrimSpace(os.Getenv("WEBRTS_TICK_SLOW_MS")); raw != "" {
			if ms, err := strconv.Atoi(raw); err == nil && ms >= 0 {
				profilerSlowTick = time.Duration(ms) * time.Millisecond
			}
		}
		slowDesc := "disabled"
		if profilerSlowTick > 0 {
			slowDesc = profilerSlowTick.String()
		}
		fmt.Fprintf(os.Stderr,
			"[tick-profile] enabled — summary every %d ticks, slow-tick tripwire %s\n",
			profileLogIntervalTicks, slowDesc)
	})
	return profilerEnabled
}

// profileSection times fn under the given label when profiling is enabled,
// and runs fn directly otherwise. Convenient for short call sites where
// wrapping in a closure reads naturally.
func profileSection(label string, fn func()) {
	if !tickProfileEnabled() {
		fn()
		return
	}
	start := time.Now()
	fn()
	profileRecord(label, time.Since(start))
}

// profileStart returns a stop function that records elapsed time under label
// when called. Use this for large/multi-statement blocks where wrapping in a
// closure would obscure the code:
//
//	stop := profileStart("perUnitTick")
//	for _, unit := range s.Units { ... }
//	stop()
//
// When profiling is disabled, both profileStart and the returned stop are
// near-no-ops (one bool check + one closure return).
func profileStart(label string) func() {
	if !tickProfileEnabled() {
		return func() {}
	}
	start := time.Now()
	return func() { profileRecord(label, time.Since(start)) }
}

// profileStartDeferred times a block whose label is only known when the block
// ends — e.g. a pathfind that should be attributed to "ok" vs "coarseFail" vs
// "fineFail" depending on which branch it exits from. Returns a stop(label)
// recorder; the caller sets the label as it learns the outcome (typically in a
// deferred closure that reads a local `outcome` string). No-op when profiling
// is disabled (one bool check + closure return), same as profileStart.
func profileStartDeferred() func(label string) {
	if !tickProfileEnabled() {
		return func(string) {}
	}
	start := time.Now()
	return func(label string) { profileRecord(label, time.Since(start)) }
}

func profileRecord(label string, d time.Duration) {
	profilerMu.Lock()
	profilerTotals[label] += d
	profilerSamples[label]++
	if d > profilerMax[label] {
		profilerMax[label] = d
	}
	profilerTickTotals[label] += d
	profilerMu.Unlock()
}

// profileTickComplete runs the slow-tick tripwire for the tick that just
// finished, then bumps the window counter and emits the rolling summary every
// profileLogIntervalTicks. tick is the absolute sim tick (correlates with the
// [debug-path tick=N] lines); unitCount makes the report self-explanatory.
func profileTickComplete(tick, unitCount int) {
	if !tickProfileEnabled() {
		return
	}

	type slowRow struct {
		label string
		d     time.Duration
	}

	profilerMu.Lock()

	// Snapshot + clear THIS tick's per-section times (tripwire input). The
	// window maps below average over 100 ticks, which dilutes a one-tick
	// freeze ~100x; this per-tick view is the only thing that can catch it.
	var slowRows []slowRow
	var worstLabel string
	var worstDur time.Duration
	if profilerSlowTick > 0 {
		slowRows = make([]slowRow, 0, len(profilerTickTotals))
		for label, d := range profilerTickTotals {
			slowRows = append(slowRows, slowRow{label, d})
			if d > worstDur {
				worstDur = d
				worstLabel = label
			}
		}
	}
	for k := range profilerTickTotals {
		delete(profilerTickTotals, k)
	}

	profilerTicks++
	windowReady := profilerTicks >= profileLogIntervalTicks
	var totals, maxes map[string]time.Duration
	var samples map[string]int
	var ticks int
	if windowReady {
		totals = profilerTotals
		samples = profilerSamples
		maxes = profilerMax
		ticks = profilerTicks
		profilerTotals = map[string]time.Duration{}
		profilerSamples = map[string]int{}
		profilerMax = map[string]time.Duration{}
		profilerTicks = 0
	}
	profilerMu.Unlock()

	// Slow-tick tripwire: trigger on the largest SINGLE section (nesting-proof
	// — combatAI and its combatAI.* children all accumulate, so a summed total
	// would double-count; the worst single block is the honest signal). Logged
	// the instant it happens, with that tick's full breakdown, so a transient
	// freeze names its own culprit instead of being averaged away.
	if profilerSlowTick > 0 && worstDur >= profilerSlowTick {
		sort.Slice(slowRows, func(i, j int) bool { return slowRows[i].d > slowRows[j].d })
		var sb strings.Builder
		fmt.Fprintf(&sb, "[tick-profile][SLOW] tick=%d units=%d — %s took %s (threshold %s); tick breakdown:\n",
			tick, unitCount, worstLabel, worstDur.Round(time.Microsecond), profilerSlowTick)
		limit := len(slowRows)
		if limit > 12 {
			limit = 12
		}
		for _, r := range slowRows[:limit] {
			fmt.Fprintf(&sb, "  %-32s %s\n", r.label, r.d.Round(time.Microsecond))
		}
		fmt.Fprint(os.Stderr, sb.String())
	}

	if !windowReady {
		return
	}

	type row struct {
		label string
		avg   time.Duration
		max   time.Duration
		total time.Duration
		count int
	}
	rows := make([]row, 0, len(totals))
	var grandTotal time.Duration
	for label, total := range totals {
		count := samples[label]
		avg := time.Duration(0)
		if count > 0 {
			avg = total / time.Duration(count)
		}
		rows = append(rows, row{label, avg, maxes[label], total, count})
		grandTotal += total
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].total > rows[j].total })

	var b strings.Builder
	fmt.Fprintf(&b, "[tick-profile] %d ticks, %d units, total %s (avg %s/tick)\n",
		ticks, unitCount, grandTotal.Round(time.Microsecond), (grandTotal / time.Duration(ticks)).Round(time.Microsecond))
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-32s avg %8s   max %8s   sum %s\n",
			r.label, r.avg.Round(time.Microsecond), r.max.Round(time.Microsecond), r.total.Round(time.Microsecond))
	}
	fmt.Fprint(os.Stderr, b.String())
}
