package game

import (
	"testing"
)

// killToCorpse runs the death pipeline the way real damage does: enqueue, then
// drain. Going through the drain (rather than calling killUnitToCorpseLocked
// directly) is the point — the corpse must be what the ordinary death path
// produces, not something only a test can build.
func killToCorpse(t *testing.T, s *GameState, u *Unit) {
	t.Helper()
	u.HP = 0
	s.enqueueDeathLocked(u, DamageSource{})
	s.drainPendingDeathsLocked()
}

// A dead unit becomes a body: out of the live registry, into s.Corpses, still
// the same *Unit so a later revive has something to restore.
func TestDeath_LeavesACorpse(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	victim := teamCombatUnit(t, s, "p2", 50, 0)
	id, owner := victim.ID, victim.OwnerID

	killToCorpse(t, s, victim)

	if got := s.getUnitByIDLocked(id); got != nil {
		t.Error("a dead unit still resolves as a UNIT; every `range s.Units` loop would see it")
	}
	for _, u := range s.Units {
		if u.ID == id {
			t.Fatal("the body is still in s.Units")
		}
	}

	corpse := s.getCorpseByIDLocked(id)
	if corpse == nil {
		t.Fatal("no corpse for the dead unit")
	}
	if corpse != victim {
		t.Error("the corpse is not the same *Unit — a revive would have nothing to restore")
	}
	if !corpse.Dead {
		t.Error("corpse is not flagged Dead")
	}
	if corpse.OwnerID != owner {
		t.Errorf("corpse owner = %q, want %q — a body belongs to whoever owned it in life", corpse.OwnerID, owner)
	}
	if s.unitIsAliveLocked(corpse) {
		t.Error("a corpse reports as alive")
	}
	if corpse.CorpseRemaining != corpseLifetimeSeconds {
		t.Errorf("corpse lifetime = %v, want %v", corpse.CorpseRemaining, corpseLifetimeSeconds)
	}
}

// The body decays on its own after corpseLifetimeSeconds and is gone for good.
func TestCorpse_DecaysAfterItsLifetime(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	victim := teamCombatUnit(t, s, "p2", 50, 0)
	id := victim.ID
	killToCorpse(t, s, victim)

	s.tickCorpsesLocked(corpseLifetimeSeconds - 1)
	if s.getCorpseByIDLocked(id) == nil {
		t.Fatal("the body decayed a second early")
	}

	s.tickCorpsesLocked(1)
	if s.getCorpseByIDLocked(id) != nil {
		t.Error("the body outlived its lifetime")
	}
	if len(s.Corpses) != 0 {
		t.Errorf("s.Corpses = %d, want 0", len(s.Corpses))
	}
}

// Death tears the unit out of everyone else's state, and does it at DEATH, not
// at decay — a corpse is inert, nothing is still swinging at it.
// (docs/design/death_and_corpses.md §6)
func TestDeath_TearsDownAttackersImmediately(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	attacker := teamCombatUnit(t, s, "p1", 0, 0)
	victim := teamCombatUnit(t, s, "p2", 50, 0)

	attacker.AttackTargetID = victim.ID
	attacker.AttackWindupRemaining = 0.4
	attacker.Attacking = true
	attacker.ThreatTable = map[int]*ThreatEntry{victim.ID: {}}

	killToCorpse(t, s, victim)

	if attacker.AttackTargetID != 0 {
		t.Error("attacker still targets the corpse")
	}
	if attacker.AttackWindupRemaining != 0 {
		t.Error("attacker is still winding up a swing at the corpse")
	}
	if _, ok := attacker.ThreatTable[victim.ID]; ok {
		t.Error("the corpse is still on an attacker's threat table")
	}
}

// A query must not select a body unless it asked for one. This is the rule that
// keeps every ability authored before corpses existed behaving identically.
func TestTargetQuery_AliveStateGatesCorpses(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	living := teamCombatUnit(t, s, "p2", 40, 0)
	doomed := teamCombatUnit(t, s, "p2", 60, 0)
	killToCorpse(t, s, doomed)

	ctx := &RuntimeAbilityContext{CasterID: caster.ID}
	query := func(aliveState string) []int {
		return s.resolveTargetQueryLocked(ctx, TargetQueryDef{
			Source:     SrcAllInScene,
			Relations:  []TargetRelation{RelEnemy},
			AliveState: aliveState,
		})
	}
	has := func(ids []int, id int) bool {
		for _, got := range ids {
			if got == id {
				return true
			}
		}
		return false
	}

	// The default. Every pre-corpse query is this one.
	if ids := query(""); has(ids, doomed.ID) || !has(ids, living.ID) {
		t.Errorf("default query = %v; want the living unit %d only, never the corpse %d", ids, living.ID, doomed.ID)
	}
	if ids := query("dead"); !has(ids, doomed.ID) || has(ids, living.ID) {
		t.Errorf("dead query = %v; want the corpse %d only", ids, doomed.ID)
	}
	if ids := query("any"); !has(ids, doomed.ID) || !has(ids, living.ID) {
		t.Errorf("any query = %v; want both %d and %d", ids, living.ID, doomed.ID)
	}
	// A typo must not silently widen the query to include bodies.
	if ids := query("undead"); has(ids, doomed.ID) {
		t.Errorf("unrecognized aliveState selected a corpse: %v", ids)
	}
}

// Relations work on a body exactly as they do on a unit — that is what lets a
// raise take ENEMY corpses while a revive takes allied ones, so the two can
// never fight over the same body (§4).
func TestTargetQuery_CorpseKeepsItsOwnerRelation(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	ownDead := teamCombatUnit(t, s, "p1", 30, 0)
	enemyDead := teamCombatUnit(t, s, "p2", 60, 0)
	killToCorpse(t, s, ownDead)
	killToCorpse(t, s, enemyDead)

	ctx := &RuntimeAbilityContext{CasterID: caster.ID}
	got := s.resolveTargetQueryLocked(ctx, TargetQueryDef{
		Source:     SrcAllInScene,
		Relations:  []TargetRelation{RelEnemy},
		AliveState: "dead",
	})
	if len(got) != 1 || got[0] != enemyDead.ID {
		t.Errorf("enemy-corpse query = %v, want just %d (the allied body must not be raisable)", got, enemyDead.ID)
	}
}

// Spending a body removes it immediately, and a second attempt fails rather
// than double-spending — two abilities racing for the same corpse in one tick
// must not both get it.
func TestConsumeCorpse_IsExactlyOnce(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	victim := teamCombatUnit(t, s, "p2", 50, 0)
	id := victim.ID
	killToCorpse(t, s, victim)

	if !s.consumeCorpseLocked(id) {
		t.Fatal("first consume failed")
	}
	if s.consumeCorpseLocked(id) {
		t.Error("second consume succeeded — the body was spent twice")
	}
	if s.getCorpseByIDLocked(id) != nil {
		t.Error("the consumed body is still on the field")
	}
}

// Bodies reach the client on their own list, and only where the viewer can see.
func TestCorpseSnapshot_IsSeparateAndFogged(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	victim := teamCombatUnit(t, s, "p2", 50, 0)
	id := victim.ID
	killToCorpse(t, s, victim)

	snap := s.snapshotUnfilteredLocked()
	for _, u := range snap.Units {
		if u.ID == id {
			t.Fatal("a corpse was sent in the UNITS list; client selection/HP-bar code would pick it up")
		}
	}
	if len(snap.Corpses) != 1 || snap.Corpses[0].ID != id {
		t.Fatalf("snapshot corpses = %+v, want the one body %d", snap.Corpses, id)
	}
	if snap.Corpses[0].OwnerID != "p2" {
		t.Errorf("corpse ownerId = %q, want p2", snap.Corpses[0].OwnerID)
	}
	if snap.Corpses[0].Remaining <= 0 {
		t.Error("corpse Remaining is not set; the client cannot fade it out")
	}
}

// THE regression. Corpses shipped working only for deaths that route through
// drainPendingDeathsLocked — which almost nothing does. Ordinary combat,
// projectiles, beams, traps and marksman procs all accumulate their own
// deadUnitIDs slice and removed the unit themselves, so a normal kill in a
// normal match produced no body at all. This is that path, end to end through
// tickUnitCombatLocked rather than through any corpse-aware helper.
func TestMeleeKill_LeavesACorpse(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	attacker := teamCombatUnit(t, s, "p1", 0, 0)
	victim := teamCombatUnit(t, s, "p2", 20, 0)
	victim.HP, victim.MaxHP = 1, 1
	attacker.Damage = 500
	attacker.AttackTargetID = victim.ID
	id := victim.ID

	// Swing until the victim dies or we give up — the windup/cooldown timing is
	// not what this test is about.
	for i := 0; i < 200 && s.getUnitByIDLocked(id) != nil; i++ {
		s.tickUnitCombatLocked(0.05, map[gridPoint]bool{})
	}

	if s.getUnitByIDLocked(id) != nil {
		t.Fatal("victim never died; the test setup, not the corpse path, is wrong")
	}
	if s.getCorpseByIDLocked(id) == nil {
		t.Error("a melee kill left no body — the combat death path skipped the corpse")
	}
}

// A despawn is not a death. A neutral camp hiding its wave must not litter the
// map with bodies.
func TestRemoveUnit_DespawnLeavesNoCorpse(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	u := teamCombatUnit(t, s, "p2", 50, 0)
	id := u.ID
	s.removeUnitLocked(id)

	if s.getCorpseByIDLocked(id) != nil {
		t.Error("removeUnitLocked left a corpse; it is the despawn path, not the death path")
	}
}
