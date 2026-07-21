package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// evasionTestPair spawns a MELEE attacker ("p1" soldier — the melee branch of
// applyDelayedAttackLocked must run) and a hostile target with deep HP at
// melee range. Target evasion is then forced via EquipmentBonus.
func evasionTestPair(t *testing.T, s *GameState) (attacker, target *Unit) {
	t.Helper()
	attacker = s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	target = &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 1_000_000, MaxHP: 1_000_000, X: 10, Y: 0}
	s.nextUnitID++
	s.addUnitLocked(target)
	return attacker, target
}

// forceAvoid pins the target's evasion at the cap so every basic attack on it
// is avoided at most 75% — for whiff-semantics tests we instead use a helper
// that retries until a whiff occurs, asserting semantics of THAT whiff.
// Simpler and deterministic: set block=0.75 dodge=0 → every avoided roll is a
// block; then scan a bounded number of swings for at least one whiff.
func forceMaxBlock(u *Unit) {
	u.EquipmentBonus.DodgeChance = 0
	u.EquipmentBonus.BlockChance = 0.75
	u.PathDodgeChance = -baseUnitDodgeChance // cancel base dodge: all avoids attribute to block
}

// disableEvasion pins a unit's evasion to zero so legacy always-hit tests
// keep their contract under the new base dodge.
func disableEvasion(u *Unit) { u.PathDodgeChance = -baseUnitDodgeChance }

// TestEvasion_MeleeWhiffIsFullWhiff: an avoided melee swing deals no damage,
// fires no on-hit procs, and records an evade event of the attributed kind.
func TestEvasion_MeleeWhiffIsFullWhiff(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xE7A1)
	s.mu.Lock()
	defer s.mu.Unlock()
	attacker, target := evasionTestPair(t, s)
	forceMaxBlock(target)
	attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{{Chance: 1.0, Ability: "fire_bolt"}}

	sawWhiff := false
	for i := 0; i < 100 && !sawWhiff; i++ {
		hpBefore := target.HP
		projsBefore := len(s.Projectiles)
		evadesBefore := len(s.evadeEventsThisTick)
		attacker.AttackWindupTargetID = target.ID
		deadUnitIDs := []int{}
		s.applyDelayedAttackLocked(attacker, &deadUnitIDs, &[]string{})
		if target.HP == hpBefore {
			sawWhiff = true
			if len(s.Projectiles) != projsBefore {
				t.Error("whiffed swing must not fire on-hit proc bolts")
			}
			if len(s.evadeEventsThisTick) != evadesBefore+1 {
				t.Fatalf("whiff must record exactly one evade event, got %d new", len(s.evadeEventsThisTick)-evadesBefore)
			}
			ev := s.evadeEventsThisTick[len(s.evadeEventsThisTick)-1]
			if ev.UnitID != target.ID || ev.Kind != "block" {
				t.Errorf("evade event = %+v, want {UnitID:%d Kind:block}", ev, target.ID)
			}
		}
	}
	if !sawWhiff {
		t.Fatal("75% block never produced a whiff in 100 swings — wiring missing?")
	}
}

// TestEvasion_ProjectileLandingRollsEvasion: a basic-attack arrow can be
// avoided at landing; an avoided landing deals no damage and records the
// evade event. Proc bolts (SkipOnHitEffects) are immune — separate test.
func TestEvasion_ProjectileLandingRollsEvasion(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xE7A2)
	s.mu.Lock()
	defer s.mu.Unlock()
	attacker, target := evasionTestPair(t, s)
	forceMaxBlock(target)

	sawWhiff := false
	for i := 0; i < 100 && !sawWhiff; i++ {
		hpBefore := target.HP
		dead := []int{}
		s.landProjectileLocked(&Projectile{ID: "arrow", OwnerUnitID: attacker.ID, OwnerPlayerID: attacker.OwnerID, TargetUnitID: target.ID, Damage: 10}, target, &dead)
		if target.HP == hpBefore {
			sawWhiff = true
		}
	}
	if !sawWhiff {
		t.Fatal("75% block never produced an arrow whiff in 100 landings — wiring missing?")
	}
}

// TestEvasion_ProcBoltIgnoresEvasion: a SkipOnHitEffects bolt always lands —
// no roll, no rngCombat consumption, damage always applies.
func TestEvasion_ProcBoltIgnoresEvasion(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xE7A3)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, target := evasionTestPair(t, s)
	forceMaxBlock(target)

	for i := 0; i < 20; i++ {
		hpBefore := target.HP
		dead := []int{}
		s.landProjectileLocked(&Projectile{ID: "bolt", TargetUnitID: target.ID, Damage: 10, DamageType: DamageFire, SkipOnHitEffects: true}, target, &dead)
		if target.HP != hpBefore-10 {
			t.Fatalf("proc bolt %d: damage %d→%d, proc bolts must never be evaded", i, hpBefore, target.HP)
		}
	}
}

// TestEvasion_SnapshotCarriesEvadeEvents: recorded evade events appear on the
// wire snapshot and clear next tick (mirrors minorDamageEvents lifecycle).
func TestEvasion_SnapshotCarriesEvadeEvents(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xE7A4)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, target := evasionTestPair(t, s)
	s.recordEvadeEventLocked(target, "dodge")
	events := s.snapshotEvadeEventsLocked()
	if len(events) != 1 || events[0].UnitID != target.ID || events[0].Kind != "dodge" {
		t.Fatalf("snapshot events = %+v, want one {UnitID:%d Kind:dodge}", events, target.ID)
	}
}
