package game

import (
	"fmt"
	"os"
	"sort"
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

var (
	profilerEnableOnce sync.Once
	profilerEnabled    bool

	profilerMu      sync.Mutex
	profilerTotals  = map[string]time.Duration{}
	profilerSamples = map[string]int{}
	profilerMax     = map[string]time.Duration{}
	profilerTicks   int
)

func tickProfileEnabled() bool {
	profilerEnableOnce.Do(func() {
		v := strings.ToLower(strings.TrimSpace(os.Getenv("WEBRTS_TICK_PROFILE")))
		profilerEnabled = v == "1" || v == "true" || v == "yes" || v == "on"
		if profilerEnabled {
			fmt.Fprintln(os.Stderr, "[tick-profile] enabled — summary every", profileLogIntervalTicks, "ticks")
		}
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

func profileRecord(label string, d time.Duration) {
	profilerMu.Lock()
	profilerTotals[label] += d
	profilerSamples[label]++
	if d > profilerMax[label] {
		profilerMax[label] = d
	}
	profilerMu.Unlock()
}

// profileTickComplete bumps the tick counter and emits a summary every
// profileLogIntervalTicks. Caller passes the unit count so the report is
// self-explanatory.
func profileTickComplete(unitCount int) {
	if !tickProfileEnabled() {
		return
	}
	profilerMu.Lock()
	profilerTicks++
	if profilerTicks < profileLogIntervalTicks {
		profilerMu.Unlock()
		return
	}
	totals := profilerTotals
	samples := profilerSamples
	maxes := profilerMax
	ticks := profilerTicks
	profilerTotals = map[string]time.Duration{}
	profilerSamples = map[string]int{}
	profilerMax = map[string]time.Duration{}
	profilerTicks = 0
	profilerMu.Unlock()

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
