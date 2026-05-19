package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

func intPtr(v int) *int { return &v }

func TestResolveUnitXPValue(t *testing.T) {
	// Absent → splitDefaultXP (10 by shipped tuning).
	if got := resolveUnitXPValue(UnitDef{}); got != gameplayTuning().Experience.SplitDefaultXP {
		t.Errorf("absent experience: got %d, want %d", got, gameplayTuning().Experience.SplitDefaultXP)
	}
	// Explicit value honored.
	if got := resolveUnitXPValue(UnitDef{Experience: intPtr(7)}); got != 7 {
		t.Errorf("explicit 7: got %d, want 7", got)
	}
	// Explicit 0 honored (unit grants no XP) — NOT treated as absent.
	if got := resolveUnitXPValue(UnitDef{Experience: intPtr(0)}); got != 0 {
		t.Errorf("explicit 0: got %d, want 0", got)
	}
}

func TestSpawnSeedsXPValue(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()
	enemy := s.spawnEnemyUnitLocked("raider", protocol.Vec2{X: 100, Y: 100})
	if enemy == nil {
		t.Fatal("spawnEnemyUnitLocked returned nil")
	}
	if enemy.XPValue != gameplayTuning().Experience.SplitDefaultXP {
		t.Errorf("raider XPValue = %d, want %d (splitDefaultXP)", enemy.XPValue, gameplayTuning().Experience.SplitDefaultXP)
	}
}

func TestSpawnRaiderUnit_SeedsXPValue(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.spawnRaiderUnitLocked(enemyPlayerID, "#e74c3c", protocol.Vec2{X: 200, Y: 200})
	if r == nil {
		t.Fatal("spawnRaiderUnitLocked returned nil")
	}
	if r.XPValue != gameplayTuning().Experience.SplitDefaultXP {
		t.Errorf("raider fallback XPValue = %d, want %d (splitDefaultXP)", r.XPValue, gameplayTuning().Experience.SplitDefaultXP)
	}
}

func TestExperienceTuning_DefaultsLoaded(t *testing.T) {
	et := gameplayTuning().Experience
	if et.Mode != experienceModeClassic {
		t.Errorf("default experience.mode = %q, want %q", et.Mode, experienceModeClassic)
	}
	if et.SplitDefaultXP != 10 {
		t.Errorf("default experience.splitDefaultXP = %d, want 10", et.SplitDefaultXP)
	}
	if et.SplitEligibilityRadius != 500 {
		t.Errorf("default experience.splitEligibilityRadius = %v, want 500", et.SplitEligibilityRadius)
	}
}
