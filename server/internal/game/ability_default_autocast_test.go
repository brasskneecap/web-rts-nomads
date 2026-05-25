package game

// Default-auto-cast seeding tests.
//
// Confirms that abilities marked `defaultAutoCast: true` in their JSON are
// seeded ON at spawn for player-owned units, that enemy units never get the
// default (per design — enemy casters must not auto-cast), and that the
// seeding survives the heal → greater_heal promotion swap so a player who
// never touched the toggle still has greater_heal on auto-cast after their
// acolyte is promoted to cleric.

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestDefaultAutoCast_HealOnAtPlayerSpawn confirms heal autocast is ON at
// spawn for player-owned acolytes. Reads the catalog field directly so a
// future flip of defaultAutoCast → false in heal.json would surface here.
func TestDefaultAutoCast_HealOnAtPlayerSpawn(t *testing.T) {
	def, ok := getAbilityDef("heal")
	if !ok {
		t.Fatal("heal ability def not found")
	}
	if !def.DefaultAutoCast {
		t.Skip("heal.json no longer declares defaultAutoCast: true — test is intentionally inert")
	}

	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	app := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if app == nil {
		t.Fatal("acolyte spawn failed")
	}
	if !app.AutoCastEnabled["heal"] {
		t.Errorf("AutoCastEnabled[\"heal\"] = false at spawn; want true (heal.json defaultAutoCast: true)")
	}
}

// TestDefaultAutoCast_EnemyAcolyteSeeded confirms enemy-owned acolytes
// DO get default auto-cast seeded. Enemies are AI-controlled and must use
// their abilities — player toggles never reach them, so there is no choice
// to preserve and no reason to suppress the seed.
func TestDefaultAutoCast_EnemyAcolyteSeeded(t *testing.T) {
	def, ok := getAbilityDef("heal")
	if !ok {
		t.Fatal("heal ability def not found")
	}
	if !def.DefaultAutoCast {
		t.Skip("heal.json no longer declares defaultAutoCast: true — test is intentionally inert")
	}

	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	enemy := s.spawnPlayerUnitLocked("acolyte", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 400, Y: 400})
	if enemy == nil {
		t.Fatal("enemy acolyte spawn failed")
	}
	if !enemy.AutoCastEnabled["heal"] {
		t.Errorf("enemy acolyte AutoCastEnabled[\"heal\"] = false; want true (enemy units are AI-controlled and must auto-cast their abilities)")
	}
}

// TestDefaultAutoCast_PreservesExplicitPlayerOff confirms that an explicit
// player choice (autocast OFF) is preserved by the seeding helper. The
// seeding must only fill ABSENT entries, never overwrite a player-set value.
func TestDefaultAutoCast_PreservesExplicitPlayerOff(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	app := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if app == nil {
		t.Fatal("acolyte spawn failed")
	}
	// Player explicitly disabled heal autocast.
	app.AutoCastEnabled["heal"] = false

	// Re-seed (idempotent — happens on every path-ability assignment).
	s.seedDefaultAutoCastLocked(app)

	if app.AutoCastEnabled["heal"] {
		t.Errorf("re-seed overwrote explicit player choice: AutoCastEnabled[\"heal\"] = true (player set false)")
	}
}

// TestDefaultAutoCast_HealSeedingSurvivesGreaterHealPromotion verifies that a
// player who never toggled heal autocast — receiving it on by default at spawn
// — has greater_heal also on by default after the heal → greater_heal swap
// (because the migration carries the value, and greater_heal also has
// defaultAutoCast: true so it would seed even if migration dropped it).
func TestDefaultAutoCast_HealSeedingSurvivesGreaterHealPromotion(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	app := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if app == nil {
		t.Fatal("acolyte spawn failed")
	}
	// Sanity: spawned with heal autocast on.
	if !app.AutoCastEnabled["heal"] {
		t.Fatal("precondition: acolyte should spawn with heal autocast on")
	}

	// Promote to (cleric, bronze). The path override swaps heal → greater_heal.
	app.ProgressionPath = unitPathCleric
	app.Rank = unitRankBronze
	s.assignUnitPathAbilitiesLocked(app)

	if !app.AutoCastEnabled["greater_heal"] {
		t.Errorf("greater_heal autocast = false after promotion; should inherit heal's on state (or seed from its own defaultAutoCast)")
	}
	if _, stillHasHeal := app.AutoCastEnabled["heal"]; stillHasHeal {
		t.Errorf("heal autocast entry should be migrated away (deleted) after promotion swap")
	}
}
