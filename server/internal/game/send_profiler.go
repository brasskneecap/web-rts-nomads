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

// Send profiler — opt-in instrumentation for per-client snapshot writes.
//
// Enable with WEBRTS_SEND_PROFILE=1. When disabled, profileClientSend is a
// fast no-op (one cached-bool check), so leaving the wrapper in place in
// BroadcastSnapshot has no production cost.
//
// What it measures: the wall-time of a single client.WriteJSON call. That
// time includes serialisation + the transport's WriteMessage path. For a
// healthy direct websocket on localhost this is sub-millisecond. For Steam
// Sockets it includes gzip + the synchronous IPC round-trip to the Rust
// shell, which is the most likely place for a stalled-joiner symptom to
// show up.
//
// What it does NOT measure: end-to-end delivery time to the friend's
// machine. The Steam relay's send queue depth lives on the Rust side; we'd
// need to surface SteamNetConnectionRealTimeStatus from the shell to see
// that (Phase 2B). This profiler answers a narrower question first: "is
// the server-side write itself blocking, or is it cheap and the lag is
// downstream of WriteJSON returning?"

const (
	sendProfileLogIntervalTicks = 100
	// slowSendThreshold: any single write exceeding this gets logged
	// immediately with the offending client's playerID. 10ms is ~20% of
	// the 50ms tick budget and far above the sub-ms steady state on a
	// healthy direct-WS path. Override with WEBRTS_SEND_SLOW_MS; set to 0
	// to disable the tripwire (the window summary still prints).
	defaultSlowSendThreshold = 10 * time.Millisecond
)

var (
	sendProfilerEnableOnce sync.Once
	sendProfilerEnabled    bool
	sendProfilerSlow       time.Duration

	sendProfilerMu sync.Mutex
	// Per-playerID rolling window stats.
	sendProfilerStats = map[string]*sendStats{}
	sendProfilerTicks int
)

type sendStats struct {
	count     int
	total     time.Duration
	max       time.Duration
	slowCount int // writes >= sendProfilerSlow this window
}

func sendProfileEnabled() bool {
	sendProfilerEnableOnce.Do(func() {
		v := strings.ToLower(strings.TrimSpace(os.Getenv("WEBRTS_SEND_PROFILE")))
		sendProfilerEnabled = v == "1" || v == "true" || v == "yes" || v == "on"
		if !sendProfilerEnabled {
			return
		}
		sendProfilerSlow = defaultSlowSendThreshold
		if raw := strings.TrimSpace(os.Getenv("WEBRTS_SEND_SLOW_MS")); raw != "" {
			if ms, err := strconv.Atoi(raw); err == nil && ms >= 0 {
				sendProfilerSlow = time.Duration(ms) * time.Millisecond
			}
		}
		slowDesc := "disabled"
		if sendProfilerSlow > 0 {
			slowDesc = sendProfilerSlow.String()
		}
		fmt.Fprintf(os.Stderr,
			"[send-profile] enabled — summary every %d ticks, slow-write tripwire %s\n",
			sendProfileLogIntervalTicks, slowDesc)
	})
	return sendProfilerEnabled
}

// profileClientSend wraps a single snapshot write with timing. playerID is
// the per-client bucket key. When profiling is disabled this is a one-bool-
// check no-op around fn().
func profileClientSend(playerID string, fn func()) {
	if !sendProfileEnabled() {
		fn()
		return
	}
	start := time.Now()
	fn()
	d := time.Since(start)

	sendProfilerMu.Lock()
	st, ok := sendProfilerStats[playerID]
	if !ok {
		st = &sendStats{}
		sendProfilerStats[playerID] = st
	}
	st.count++
	st.total += d
	if d > st.max {
		st.max = d
	}
	if sendProfilerSlow > 0 && d >= sendProfilerSlow {
		st.slowCount++
	}
	sendProfilerMu.Unlock()

	// Slow-write tripwire: log the instant it happens, so a single
	// stalled write names the offending client immediately instead of
	// being averaged across the window. Done outside the lock.
	if sendProfilerSlow > 0 && d >= sendProfilerSlow {
		fmt.Fprintf(os.Stderr,
			"[send-profile][SLOW] player=%s write took %s (threshold %s)\n",
			playerID, d.Round(time.Microsecond), sendProfilerSlow)
	}
}

// sendProfileBroadcastComplete is called once per BroadcastSnapshot after
// all per-client sends have run. It advances the window counter and emits
// the per-client summary every sendProfileLogIntervalTicks (5s at 20 Hz).
func sendProfileBroadcastComplete() {
	if !sendProfileEnabled() {
		return
	}

	sendProfilerMu.Lock()
	sendProfilerTicks++
	if sendProfilerTicks < sendProfileLogIntervalTicks {
		sendProfilerMu.Unlock()
		return
	}

	type row struct {
		player    string
		avg       time.Duration
		max       time.Duration
		total     time.Duration
		count     int
		slowCount int
	}
	rows := make([]row, 0, len(sendProfilerStats))
	for player, st := range sendProfilerStats {
		avg := time.Duration(0)
		if st.count > 0 {
			avg = st.total / time.Duration(st.count)
		}
		rows = append(rows, row{player, avg, st.max, st.total, st.count, st.slowCount})
	}
	// Reset window.
	sendProfilerStats = map[string]*sendStats{}
	ticks := sendProfilerTicks
	sendProfilerTicks = 0
	sendProfilerMu.Unlock()

	if len(rows) == 0 {
		return
	}

	// Worst average first — slow joiners surface at the top.
	sort.Slice(rows, func(i, j int) bool { return rows[i].avg > rows[j].avg })

	var b strings.Builder
	fmt.Fprintf(&b, "[send-profile] %d ticks, %d clients\n", ticks, len(rows))
	for _, r := range rows {
		fmt.Fprintf(&b, "  player=%-32s  writes=%4d  avg %8s   max %8s   slow=%d\n",
			r.player, r.count,
			r.avg.Round(time.Microsecond),
			r.max.Round(time.Microsecond),
			r.slowCount)
	}
	fmt.Fprint(os.Stderr, b.String())
}
