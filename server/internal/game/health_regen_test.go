package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestHealthRegen_DefaultRateHealsOneHPEveryFiveSeconds verifies that a
// freshly-spawned unit with the default regen rate (0.2 HP/s) heals exactly
// 1 HP after 5 seconds of simulation ticks, and not before.
func TestHealthRegen_DefaultRateHealsOneHPEveryFiveSeconds(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	s.mu.Lock()
	unit := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	// Chip the unit below max so regen has room to tick.
	unit.HP = unit.MaxHP - 5
	startHP := unit.HP
	s.mu.Unlock()

	// Tick for just under 5 seconds — unit must not have regened yet
	// (accumulator < 1).
	const dt = 0.1
	const preHealTicks = 49 // 4.9 s
	for i := 0; i < preHealTicks; i++ {
		s.Update(dt)
	}
	s.mu.RLock()
	if unit.HP != startHP {
		t.Errorf("pre-5s regen: HP should still be %d, got %d", startHP, unit.HP)
	}
	s.mu.RUnlock()

	// One more tick crosses the 5 s threshold — exactly +1 HP.
	s.Update(dt)
	s.mu.RLock()
	if unit.HP != startHP+1 {
		t.Errorf("at 5s: HP should be %d, got %d", startHP+1, unit.HP)
	}
	s.mu.RUnlock()

	// Five more seconds → another +1 HP.
	for i := 0; i < 50; i++ {
		s.Update(dt)
	}
	s.mu.RLock()
	if unit.HP != startHP+2 {
		t.Errorf("at 10s: HP should be %d, got %d", startHP+2, unit.HP)
	}
	s.mu.RUnlock()
}

// TestHealthRegen_DoesNotExceedMaxHP verifies regen clamps to MaxHP and that
// the accumulator resets at full HP so a fresh hit doesn't instantly trigger
// banked regen.
func TestHealthRegen_DoesNotExceedMaxHP(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	s.mu.Lock()
	unit := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	unit.HP = unit.MaxHP
	s.mu.Unlock()

	// Tick 30 seconds at full HP — HP should stay at MaxHP, accumulator at 0.
	const dt = 0.1
	for i := 0; i < 300; i++ {
		s.Update(dt)
	}
	s.mu.RLock()
	if unit.HP != unit.MaxHP {
		t.Errorf("HP should stay at MaxHP (%d), got %d", unit.MaxHP, unit.HP)
	}
	if unit.HealthRegenAccumulator != 0 {
		t.Errorf("accumulator should reset at MaxHP, got %.4f", unit.HealthRegenAccumulator)
	}
	s.mu.RUnlock()

	// Chip HP and tick one more step — regen must start from zero, not jump.
	s.mu.Lock()
	unit.HP = unit.MaxHP - 5
	s.mu.Unlock()
	s.Update(dt)
	s.mu.RLock()
	if unit.HP != unit.MaxHP-5 {
		t.Errorf("HP after one tick post-damage: expected no instant regen (%d), got %d", unit.MaxHP-5, unit.HP)
	}
	s.mu.RUnlock()
}

// TestHealthRegen_DeadUnitsDoNotRegen verifies dead units never heal back via
// the passive regen path.
func TestHealthRegen_DeadUnitsDoNotRegen(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	s.mu.Lock()
	unit := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	unit.HP = 0
	s.mu.Unlock()

	const dt = 0.1
	for i := 0; i < 100; i++ {
		s.Update(dt)
	}
	s.mu.RLock()
	if unit.HP != 0 {
		t.Errorf("dead unit should not regen, HP=%d", unit.HP)
	}
	s.mu.RUnlock()
}
