package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// advanceTicksUntil steps the sim in 0.05s increments until pred returns true or
// maxSteps is reached, returning whether pred was satisfied. pred is evaluated
// before each step (and once more after the loop) and must do its own locking.
// Used by ability-projectile tests to fast-forward a bolt to impact.
func advanceTicksUntil(s *GameState, maxSteps int, pred func() bool) bool {
	for i := 0; i < maxSteps; i++ {
		if pred() {
			return true
		}
		s.Update(0.05)
	}
	return pred()
}

// TestAdept_AutocastsArcaneBolt_RealSpawn reproduces the user's report ("not
// seeing arcane_bolt get cast / not sure it deals damage"). It spawns a REAL
// adept via the catalog spawn path (so Abilities + default-autocast seeding
// come from adept.json/arcane_bolt.json, nothing injected), puts a hostile in
// cast range, and ticks. If the sim path is correct the adept should autocast
// arcane_bolt and the enemy should lose HP equal to the ability's DamageAmount.
func TestAdept_AutocastsArcaneBolt_RealSpawn(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()

	adept := s.spawnPlayerUnitLocked("adept", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	adept.Visible = true

	// Sanity: the catalog gave it arcane_bolt and seeded autocast ON.
	if !containsString(adept.Abilities, "arcane_bolt") {
		s.mu.Unlock()
		t.Fatalf("adept.Abilities = %v; expected it to include arcane_bolt", adept.Abilities)
	}
	if !adept.AutoCastEnabled["arcane_bolt"] {
		s.mu.Unlock()
		t.Fatalf("arcane_bolt autocast not seeded ON at spawn; AutoCastEnabled=%v", adept.AutoCastEnabled)
	}

	arcaneDef, ok := getAbilityDef("arcane_bolt")
	if !ok {
		s.mu.Unlock()
		t.Fatal("arcane_bolt def not found")
	}
	// arcane_bolt is schemaVersion:2 as of the composable-abilities migration:
	// DamageAmount is cleared on the raw def (the compiled launch_projectile
	// action's Config.Amount is the sole authority now). Recovered here purely
	// for the diagnostic message below — the actual pass/fail assertion is the
	// enemy.HP comparison, which needs no recovery.
	arcaneDef = abilityMechanicsShadow(arcaneDef)

	// Enemy well within the adept's cast range (match_attack_range = 220).
	enemy := spawnProjTestUnit(t, s, enemyPlayerID, 450, 400)
	enemy.Visible = true
	enemy.HP = enemy.MaxHP
	startHP := enemy.HP
	s.mu.Unlock()

	// Run enough ticks to cover a 0.5s cast time.
	sawCast := false
	for i := 0; i < 40; i++ {
		s.Update(0.05)
		s.mu.RLock()
		if adept.CastAbilityID == "arcane_bolt" {
			sawCast = true
		}
		hp := enemy.HP
		s.mu.RUnlock()
		if hp < startHP {
			break
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if !sawCast {
		t.Errorf("adept never entered an arcane_bolt cast (CastAbilityID stayed empty across ticks)")
	}
	if enemy.HP >= startHP {
		t.Errorf("enemy HP unchanged (%d); arcane_bolt should have dealt DamageAmount=%d", enemy.HP, arcaneDef.DamageAmount)
	}
}
