package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// explosive_trap detonates on its on_tick: each detonation tick plays the
// explosion and damages enemies in the zone, capped by the zone's maxTicks (1 =
// a single blast, then the zone expires). Aftershock (explosive_chain) is a pure
// data modifier that ADDS to that maxTicks — one more detonation tick per +1
// (each with its own explosion), spaced by the tickInterval. These assert the
// blast count, derived from behavior (HP lost), not any pinned tunable.
func castExplosiveTrapAndMeasure(t *testing.T, perks []string) int {
	t.Helper()
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}

	caster := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 300, Y: 300})
	if caster == nil {
		caster = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 300, Y: 300})
	}
	grantTrapAbility(caster, "explosive_trap")
	caster.PerkIDs = perks
	caster.Damage = 0

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 380, Y: 300})
	if enemy == nil {
		t.Fatal("enemy spawn failed")
	}
	enemy.Visible = true
	enemy.HP, enemy.MaxHP = 1_000_000, 1_000_000
	enemy.MoveSpeed = 0

	if ok, reason := s.beginAbilityCastLocked(caster, "explosive_trap", enemy); !ok {
		t.Fatalf("explosive_trap cast failed: %q", reason)
	}

	startHP := enemy.HP
	// on_zone_enter selects + consumes + starts the detonation loop; the loop's
	// iterations fire from s.pendingLoops, spaced by the body's `wait`. Drive
	// BOTH the zone ticks (to fire the enter) and the pending loops (to fire the
	// scheduled detonations), advancing simTime. 12s covers every aftershock beat.
	for i := 0; i < 120; i++ {
		s.tickAbilityZonesLocked(0.1)
		s.simTime += 0.1
		s.tickPendingLoopsLocked()
	}
	return startHP - enemy.HP
}

func TestExplosiveTrap_AftershockScalesBlastCount(t *testing.T) {
	base := castExplosiveTrapAndMeasure(t, nil)
	withPerk := castExplosiveTrapAndMeasure(t, []string{"explosive_chain"})

	if base <= 0 {
		t.Fatalf("explosive trap dealt no damage (%d) — the detonation never blasted", base)
	}
	if withPerk <= base {
		t.Errorf("explosive_chain added no extra blast: base %d, with perk %d — the maxTicks abilityFields fold did not reach the detonation zone",
			base, withPerk)
	}
	// Exactly one extra blast (the perk adds +1 to maxTicks): the perk total is
	// twice the base (base = 1 blast, perk = 2 blasts of the same magnitude).
	if withPerk != base*2 {
		t.Errorf("explosive_chain should add exactly one more blast: base %d, with perk %d (want %d)", base, withPerk, base*2)
	}
}
