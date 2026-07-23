package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────────────────────
// ONE-SHOT ZONES (consume_zone) and explosive_trap, its first consumer.
//
// consume_zone is the generic "end the zone I am running inside" primitive. It
// is what lets a pressure-plate trap be an ordinary zone that authors
// "on enter: blast, then consume myself" rather than needing a distinct
// one-shot zone kind — and any ability wanting a spend-once ward gets the same
// shape for free.
// ─────────────────────────────────────────────────────────────────────────────

func castExplosiveTrap(t *testing.T, s *GameState) (caster, enemy *Unit) {
	t.Helper()
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}

	caster = s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 300, Y: 300})
	if caster == nil {
		caster = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 300, Y: 300})
	}
	grantTrapAbility(caster, "explosive_trap")

	enemy = s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 380, Y: 300})
	if enemy == nil {
		t.Fatal("enemy spawn failed")
	}
	enemy.Visible = true
	enemy.HP, enemy.MaxHP = 500, 500

	ok, reason := s.beginAbilityCastLocked(caster, "explosive_trap", enemy)
	if !ok {
		t.Fatalf("beginAbilityCastLocked(explosive_trap) failed: %q", reason)
	}
	return caster, enemy
}

// TestExplosiveTrapZone_DetonatesOnceAndVanishes is the behaviour the trap
// exists for: stepping on it CONSUMES the armed trap immediately (it vanishes),
// and starts a detonation loop that — a fuse later — plays the explosion and
// blasts the enemy. The armed trap does not keep ticking for its authored
// duration.
func TestExplosiveTrapZone_DetonatesOnceAndVanishes(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	_, enemy := castExplosiveTrap(t, s)
	if len(s.AbilityZones) != 1 {
		t.Fatalf("AbilityZones = %d, want 1 (armed trap)", len(s.AbilityZones))
	}
	// Authored duration is long, so anything that removes it must be the
	// consume, not expiry.
	if rem := s.AbilityZones[0].Remaining; rem < 5 {
		t.Fatalf("armed trap Remaining = %v; test needs a long duration to prove consume != expiry", rem)
	}

	before := enemy.HP
	// on_zone_enter fires (enemy is inside): select targets, consume the trap
	// (it vanishes NOW), and schedule the detonation loop ~1s out.
	s.tickAbilityZonesLocked(0.1)
	for _, z := range s.AbilityZones {
		if len(z.Triggers) > 0 {
			t.Fatalf("armed trap should be consumed on enter, but a triggered zone remains (sprite=%q)", z.Sprite)
		}
	}

	// Advance time so the scheduled detonation fires (explosion decal + blast),
	// then the decal expires.
	for i := 0; i < 30; i++ { // 3s > the 1s fuse + 0.8s decal
		s.simTime += 0.1
		s.tickPendingLoopsLocked()
		s.tickAbilityZonesLocked(0.1)
	}

	if enemy.HP >= before {
		t.Errorf("enemy stepping on the trap took no blast damage (HP %d -> %d)", before, enemy.HP)
	}
	// After the detonation and the explosion decal's short life, nothing remains.
	if len(s.AbilityZones) != 0 {
		t.Fatalf("everything should be gone after detonating, %d zone(s) remain", len(s.AbilityZones))
	}
}

// TestExplosiveTrapZone_ArmedTrapWaitsForAVictim: no victim ⇒ no blast, and the
// trap stays armed. Guards against consume_zone firing eagerly.
func TestExplosiveTrapZone_ArmedTrapWaitsForAVictim(t *testing.T) {
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

	// A far-away enemy: in cast range, nowhere near the armed trigger radius.
	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 500, Y: 300})
	enemy.Visible = true
	enemy.HP, enemy.MaxHP = 500, 500
	if ok, reason := s.beginAbilityCastLocked(caster, "explosive_trap", enemy); !ok {
		t.Fatalf("cast failed: %q", reason)
	}
	// Move the victim far away so it is not an occupant.
	enemy.X, enemy.Y = 2000, 2000

	before := enemy.HP
	s.tickAbilityZonesLocked(1)

	if enemy.HP != before {
		t.Errorf("distant enemy was damaged by an untriggered trap (HP %d -> %d)", before, enemy.HP)
	}
	if len(s.AbilityZones) != 1 {
		t.Fatalf("armed trap should persist with no victim, got %d zone(s)", len(s.AbilityZones))
	}
}

// TestConsumeZone_NoOpOutsideAZone: the action is safe to author anywhere; it
// simply does nothing when the execution is not zone-driven.
func TestConsumeZone_NoOpOutsideAZone(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()
	caster := teamCombatUnit(t, s, "p1", 0, 0)

	tr := runOneActionProgram(t, s, caster.ID, 0, ActionConsumeZone, `{}`, nil)
	if !traceHas(tr, "zone_consume_skipped") {
		t.Errorf("consume_zone outside a zone should trace a skip: %+v", tr.Events)
	}
}
