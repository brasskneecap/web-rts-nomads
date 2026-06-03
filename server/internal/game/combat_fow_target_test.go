package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// A player-owned unit must only engage enemies its owner can actually see.
// Fog of war was previously enforced only in the snapshot layer, so unit combat
// AI happily acquired and held enemies sitting in cells the player could not
// see. These tests pin the full-consistency rule: acquisition, persistence, and
// retaliation all gate on the attacker owner's FOW. The __enemy__ AI has no FOW
// grid and stays omniscient (exempt) — that asymmetry is intentional.
func TestCombat_FogHidesEnemyFromPlayerUnits(t *testing.T) {
	// setup builds a p1 soldier and an __enemy__ soldier 150px apart — inside
	// the soldier's 240 detection range and 230 leash — placed far from the p1
	// townhall (~(160,704), vision 320) so the attacker's own VisionRange is the
	// only p1 vision source. attackerVision tunes whether the enemy ends up in
	// fog (Dark) or revealed (Clear) after the FOW recompute.
	const (
		ax, ay = 1600.0, 400.0
		ex, ey = 1750.0, 400.0 // 150px east of the attacker
	)
	setup := func(t *testing.T, attackerVision float64) (*GameState, *Unit, *Unit) {
		t.Helper()
		s := newObjectiveTestState(t)
		s.FOW["p1"] = newPlayerFOW(s.MapConfig.GridCols, s.MapConfig.GridRows)

		attacker := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: ax, Y: ay})
		attacker.Visible = true
		attacker.MaxHP, attacker.HP = 100, 100
		s.initializeCombatUnitLocked(attacker)
		attacker.VisionRange = attackerVision
		attacker.CombatAnchorX, attacker.CombatAnchorY = ax, ay

		enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: ex, Y: ey})
		enemy.Visible = true
		enemy.MaxHP, enemy.HP = 100, 100
		s.initializeCombatUnitLocked(enemy)

		s.recomputeFOWLocked()
		return s, attacker, enemy
	}

	selectBest := func(s *GameState, attacker *Unit) combatTarget {
		profile := resolveCombatProfile(attacker)
		idx := newCombatSpatialIndex(combatSpatialBucketSize)
		for _, u := range s.Units {
			if u != nil && u.Visible && u.HP > 0 {
				idx.add(u)
			}
		}
		return s.selectBestTargetLocked(attacker, profile, combatEvalContext{index: idx, blocked: s.getBlockedCellsLocked()})
	}

	t.Run("enemy in fog is not acquired", func(t *testing.T) {
		s, attacker, enemy := setup(t, 50) // 50px vision: enemy at 150px stays Dark
		defer s.mu.Unlock()

		if s.FOW["p1"].isClearAtWorld(enemy.X, enemy.Y, s.MapConfig.CellSize) {
			t.Fatal("fixture broken: enemy must be in fog (Dark) for p1")
		}
		if best := selectBest(s, attacker); best.Kind == combatTargetUnit {
			t.Fatalf("player unit acquired a fogged enemy (id=%d); units must not target enemies they cannot see", best.Unit.ID)
		}
	})

	t.Run("enemy in vision is acquired", func(t *testing.T) {
		s, attacker, enemy := setup(t, 300) // 300px vision: enemy at 150px is Clear
		defer s.mu.Unlock()

		if !s.FOW["p1"].isClearAtWorld(enemy.X, enemy.Y, s.MapConfig.CellSize) {
			t.Fatal("fixture broken: enemy must be revealed (Clear) for p1")
		}
		best := selectBest(s, attacker)
		if best.Kind != combatTargetUnit || best.Unit == nil || best.Unit.ID != enemy.ID {
			t.Fatalf("player unit must acquire a visible enemy; got kind=%d", best.Kind)
		}
	})

	t.Run("held target is dropped when it enters fog", func(t *testing.T) {
		// Full-consistency rule: a target the owner can no longer see is no
		// longer a valid held target, so the unit lets go instead of attacking
		// blind through the fog.
		sFog, atkFog, enemyFog := setup(t, 50)
		defer sFog.mu.Unlock()
		if sFog.combatTargetIsValidLocked(atkFog, enemyFog) {
			t.Error("a fogged enemy must not be a valid held target for a player unit")
		}

		sClear, atkClear, enemyClear := setup(t, 300)
		defer sClear.mu.Unlock()
		if !sClear.combatTargetIsValidLocked(atkClear, enemyClear) {
			t.Error("a revealed enemy must remain a valid held target")
		}
	})

	t.Run("enemy AI is exempt and still sees the player unit", func(t *testing.T) {
		// The attacker here is the __enemy__ unit (no FOW grid). It must acquire
		// the p1 unit regardless of any fog — enemy AI omniscience is preserved.
		s, attacker, enemy := setup(t, 50)
		defer s.mu.Unlock()
		if !s.combatTargetIsValidLocked(enemy, attacker) {
			t.Error("enemy AI (no FOW grid) must still be able to target the player unit, fog or not")
		}
	})
}
