package game

import (
	"os"
	"strconv"
	"testing"
	"time"

	"webrts/server/pkg/protocol"
)

// newBenchmarkState constructs a GameState with ~100 units spread across 2
// players. It uses the default catalog map so it exercises the same code paths
// as a real match.
//
// Accepts testing.TB so it works for both *testing.T and *testing.B.
func newBenchmarkState(tb testing.TB) *GameState {
	tb.Helper()

	state := NewGameState(GetMapConfigByID(DefaultMapID()))

	// EnsurePlayer triggers the full spawn path: townhall claim + starting
	// loadout (typically 3 workers from the default map). Done outside the lock
	// because EnsurePlayer acquires it internally.
	state.EnsurePlayer("bench-player-1")
	state.EnsurePlayer("bench-player-2")

	// Spawn additional soldiers to reach ~100 units total (~47 per player on
	// top of the 3 starting workers each).
	state.mu.Lock()
	color1 := "#3498db"
	color2 := "#2ecc71"
	if p, ok := state.Players["bench-player-1"]; ok {
		color1 = p.Color
	}
	if p, ok := state.Players["bench-player-2"]; ok {
		color2 = p.Color
	}

	for i := 0; i < 47; i++ {
		x := 200.0 + float64(i%10)*30.0
		y := 200.0 + float64(i/10)*30.0
		state.spawnPlayerUnitLocked("soldier", "bench-player-1", color1, protocol.Vec2{X: x, Y: y})
	}
	for i := 0; i < 47; i++ {
		x := 600.0 + float64(i%10)*30.0
		y := 600.0 + float64(i/10)*30.0
		state.spawnPlayerUnitLocked("soldier", "bench-player-2", color2, protocol.Vec2{X: x, Y: y})
	}
	state.mu.Unlock()

	return state
}

// BenchmarkGameState_Update_100Units measures the cost of a single Update tick
// with ~100 units on the default map.
func BenchmarkGameState_Update_100Units(b *testing.B) {
	state := newBenchmarkState(b)
	const dt = 0.05 // 50 ms simulated tick

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state.Update(dt)
	}
}

// TestGameState_Update_TickBudget_100Units asserts that 200 consecutive Update
// ticks with ~100 units stay within a per-tick wall-clock budget.
//
// Default budget: 20 ms/tick (leaves 30 ms headroom against the 50 ms period).
// Override via TICK_BUDGET_MS env var to relax the limit on slower CI boxes.
//
// Skipped under -short.
func TestGameState_Update_TickBudget_100Units(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping tick-budget test under -short")
	}

	budgetMs := 20.0
	if v := os.Getenv("TICK_BUDGET_MS"); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil && parsed > 0 {
			budgetMs = parsed
		}
	}

	const ticks = 200
	const dt = 0.05 // 50 ms simulated tick

	state := newBenchmarkState(t)

	start := time.Now()
	for i := 0; i < ticks; i++ {
		state.Update(dt)
	}
	elapsed := time.Since(start)

	avgMs := float64(elapsed.Milliseconds()) / float64(ticks)
	t.Logf("200-tick run: total=%v, avg=%.2f ms/tick (budget=%.0f ms/tick, unit count=%d)",
		elapsed, avgMs, budgetMs, len(state.Units))

	if avgMs > budgetMs {
		t.Errorf("average tick time %.2f ms exceeds budget %.0f ms — perf regression", avgMs, budgetMs)
	}
}
