package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// Heal's "ally" is now team-based, not owner-based: a same-team unit owned
// by a DIFFERENT player is a valid heal target; a cross-team unit is not;
// the __enemy__ AI never is. Also covers the auto-cast ally selector.
func TestTeam_AbilityTargetingIsTeamBased(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 100, 100)
	caster.AttackRange = 300 // heal castRange = match_attack_range
	teammate := spawnProjTestUnit(t, s, "p2", 140, 100) // different owner
	teammate.HP = teammate.MaxHP - 30
	foe := spawnProjTestUnit(t, s, "p3", 160, 100) // different owner
	foe.HP = foe.MaxHP - 30
	pveEnemy := spawnProjTestUnit(t, s, enemyPlayerID, 180, 100)
	pveEnemy.HP = pveEnemy.MaxHP - 30

	// p1 & p2 same team; p3 a different team.
	setTeam(s, "p1", 0)
	setTeam(s, "p2", 0)
	setTeam(s, "p3", 1)

	heal, ok := getAbilityDef("heal")
	if !ok {
		t.Fatal("heal ability def missing")
	}

	// Same-team, different-owner ally → heal-able (the new team behavior).
	if !s.canAbilityTargetUnitLocked(heal, caster, teammate) {
		t.Error("heal should target a same-team ally owned by a different player")
	}
	// Cross-team unit → NOT heal-able (classified enemy; heal can't target enemies).
	if s.canAbilityTargetUnitLocked(heal, caster, foe) {
		t.Error("heal must not target a cross-team unit")
	}
	// __enemy__ AI → never an ally.
	if s.canAbilityTargetUnitLocked(heal, caster, pveEnemy) {
		t.Error("heal must not target the __enemy__ AI")
	}

	// Auto-cast ally selector picks the damaged same-team different-owner ally.
	sel := lowestHPSelector(t)
	def := healLikeDef(300)
	if got := sel(s, caster, def); got != teammate {
		t.Errorf("ally selector should pick the same-team different-owner ally (id %d); got %v", teammate.ID, idOf(got))
	}

	// Flip p2 to a hostile team → the same unit is no longer a valid ally
	// (pure data change; the bake-in working at the ability layer).
	setTeam(s, "p2", 9)
	if s.canAbilityTargetUnitLocked(heal, caster, teammate) {
		t.Error("after flipping p2 to another team, heal must no longer target it")
	}
	if got := sel(s, caster, def); got == teammate {
		t.Error("ally selector must not pick a now-cross-team unit")
	}
}

// End-to-end: a cast initiated on a same-team different-owner ally resolves
// (HP restored), through the real beginAbilityCastLocked → tick lifecycle.
func TestTeam_HealCastResolvesOnCrossOwnerTeammate(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	app := s.spawnPlayerUnitLocked("apprentice", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	app.Visible = true
	mate := spawnProjTestUnit(t, s, "p2", 450, 400) // different owner, same team
	mate.HP = mate.MaxHP - 5
	want := mate.HP + 5
	setTeam(s, "p1", 0)
	setTeam(s, "p2", 0)
	mateID := mate.ID
	ok, reason := s.beginAbilityCastLocked(app, "heal", mate)
	s.mu.Unlock()
	if !ok {
		t.Fatalf("heal on same-team cross-owner ally should start: %q", reason)
	}

	advance(s, 25) // > 1s cast time

	s.mu.RLock()
	defer s.mu.RUnlock()
	if got := s.unitsByID[mateID].HP; got != want {
		t.Errorf("cross-owner teammate HP = %d; want %d (healed +5)", got, want)
	}
}
