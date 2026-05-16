package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// spawnManaTestUnit spawns a lone, idle player soldier and gives it a mana
// pool. Caller does NOT hold s.mu (this takes it). Returns the unit.
func spawnManaTestUnit(t *testing.T, s *GameState, maxMana, curMana int, regen float64) *Unit {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	u.MaxMana = maxMana
	u.CurrentMana = curMana
	u.ManaRegenPerSecond = regen
	return u
}

// ── Regenerates correctly over time ──────────────────────────────────────────

func TestMana_RegeneratesOverTime(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	u := spawnManaTestUnit(t, s, 50, 10, 1.0) // 1 mana/s

	// Float accumulation means 0.1 summed N times is slightly under N*0.1, so
	// the integer mana crosses ~one tick late. Like health_regen_test.go we
	// assert "just under → no gain" and a tolerant later window for the rate,
	// not an exact crossing tick.
	const dt = 0.1

	// 0.9 s: accumulator < 1, no integer mana yet.
	for i := 0; i < 9; i++ {
		s.Update(dt)
	}
	s.mu.RLock()
	if u.CurrentMana != 10 {
		t.Errorf("pre-1s: CurrentMana should still be 10, got %d", u.CurrentMana)
	}
	s.mu.RUnlock()

	// By 1.2 s exactly +1 mana has regened (1 mana/s → not yet +2).
	for i := 0; i < 3; i++ {
		s.Update(dt)
	}
	s.mu.RLock()
	if u.CurrentMana != 11 {
		t.Errorf("by 1.2s: CurrentMana should be 11 (exactly +1 at 1 mana/s), got %d", u.CurrentMana)
	}
	s.mu.RUnlock()

	// By ~6.2 s total: exactly +6 over the elapsed seconds (rate holds, the
	// 0.2 s margin absorbs the one-tick float lag).
	for i := 0; i < 50; i++ {
		s.Update(dt)
	}
	s.mu.RLock()
	if u.CurrentMana != 16 {
		t.Errorf("by ~6.2s: CurrentMana should be 16 (+6 at 1 mana/s), got %d", u.CurrentMana)
	}
	s.mu.RUnlock()
}

// ── Caps at MaxMana ──────────────────────────────────────────────────────────

func TestMana_CapsAtMaxMana(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	u := spawnManaTestUnit(t, s, 50, 49, 100.0) // huge regen, 1 below cap

	const dt = 0.1
	for i := 0; i < 20; i++ { // way more than enough to overfill
		s.Update(dt)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if u.CurrentMana != u.MaxMana {
		t.Errorf("CurrentMana should clamp to MaxMana (%d), got %d", u.MaxMana, u.CurrentMana)
	}
	if u.ManaRegenAccumulator != 0 {
		t.Errorf("accumulator should reset to 0 at full mana, got %.4f", u.ManaRegenAccumulator)
	}
}

// A unit at full mana that spends some must regen from zero, not instantly
// receive banked accumulator.
func TestMana_NoInstantRebankAfterSpend(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	u := spawnManaTestUnit(t, s, 50, 50, 1.0)

	const dt = 0.1
	// Sit at full for a while (accumulator parked at 0).
	for i := 0; i < 30; i++ {
		s.Update(dt)
	}
	s.mu.Lock()
	if !s.spendUnitManaLocked(u, 20) {
		t.Fatal("spend 20 of 50 should succeed")
	}
	if u.CurrentMana != 30 {
		t.Fatalf("after spend: CurrentMana = %d, want 30", u.CurrentMana)
	}
	s.mu.Unlock()

	// Under 1 s of regen → still 30 (no banked jump).
	for i := 0; i < 9; i++ {
		s.Update(dt)
	}
	s.mu.RLock()
	if u.CurrentMana != 30 {
		t.Errorf("post-spend sub-1s regen should not bank: CurrentMana = %d, want 30", u.CurrentMana)
	}
	s.mu.RUnlock()
}

// ── Units with 0 MaxMana skip regen logic ────────────────────────────────────

func TestMana_ZeroMaxManaSkipsRegen(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	// MaxMana 0 but a positive regen rate set anyway — must still be skipped.
	u := spawnManaTestUnit(t, s, 0, 0, 5.0)

	const dt = 0.1
	for i := 0; i < 100; i++ {
		s.Update(dt)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if u.CurrentMana != 0 {
		t.Errorf("MaxMana==0 unit must not gain mana, got CurrentMana=%d", u.CurrentMana)
	}
	if u.ManaRegenAccumulator != 0 {
		t.Errorf("MaxMana==0 unit must not accumulate regen, got %.4f", u.ManaRegenAccumulator)
	}
}

// ── Spending mana: success / failure / clamps ────────────────────────────────

func TestMana_Spend(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	u := spawnManaTestUnit(t, s, 50, 30, 0)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Sufficient mana → success, deducted.
	if !s.spendUnitManaLocked(u, 5) {
		t.Fatal("spend 5 of 30 should succeed")
	}
	if u.CurrentMana != 25 {
		t.Errorf("after spending 5: CurrentMana = %d, want 25", u.CurrentMana)
	}

	// Insufficient mana → fails gracefully, unchanged.
	if s.spendUnitManaLocked(u, 1000) {
		t.Error("spending more mana than available should fail")
	}
	if u.CurrentMana != 25 {
		t.Errorf("failed spend must not change mana: CurrentMana = %d, want 25", u.CurrentMana)
	}

	// Exact balance → success, lands on 0.
	if !s.spendUnitManaLocked(u, 25) {
		t.Error("spending the exact remaining balance should succeed")
	}
	if u.CurrentMana != 0 {
		t.Errorf("after spending exact balance: CurrentMana = %d, want 0", u.CurrentMana)
	}

	// Now empty: any positive cost fails.
	if s.spendUnitManaLocked(u, 1) {
		t.Error("spending from empty pool should fail")
	}

	// Zero / negative cost → free success, no change.
	if !s.spendUnitManaLocked(u, 0) {
		t.Error("cost 0 should succeed (free)")
	}
	if !s.spendUnitManaLocked(u, -10) {
		t.Error("negative cost should succeed (treated as free)")
	}
	if u.CurrentMana != 0 {
		t.Errorf("free spend must not change mana: CurrentMana = %d, want 0", u.CurrentMana)
	}
}

func TestMana_SpendGuards_NoPoolAndNil(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	u := spawnManaTestUnit(t, s, 0, 0, 0) // no mana pool

	s.mu.Lock()
	defer s.mu.Unlock()

	// A costed spend on a unit with no mana pool fails.
	if s.spendUnitManaLocked(u, 5) {
		t.Error("costed spend on a MaxMana==0 unit should fail")
	}
	// ...but a free spend still succeeds (uniform call site for free abilities).
	if !s.spendUnitManaLocked(u, 0) {
		t.Error("free spend should succeed even with no mana pool")
	}
	// nil unit with a positive cost fails and does not panic.
	if s.spendUnitManaLocked(nil, 5) {
		t.Error("spend on nil unit should fail")
	}
}
