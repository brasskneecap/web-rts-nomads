package game

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// TestOnHitProc_ChillSlowsOnLand drives the full frost-style path: a proc bolt
// carrying a slow lands on its target and applies the chill through the shared
// slow system (attack + move speed × SlowMultiplier for SlowDurationSeconds).
// Values are read from the proc so a balance tweak can't break the test.
func TestOnHitProc_ChillSlowsOnLand(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xF205)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 1_000_000, MaxHP: 1_000_000, X: 50, Y: 0}
	s.nextUnitID++
	s.addUnitLocked(target)

	proc := EquipmentProc{Chance: 1.0, Params: ProcEffectParams{
		Damage: 25, DamageType: DamageCold, ProjectileID: "frost_bolt",
		SlowMultiplier: 0.75, SlowDurationSeconds: 2,
	}}
	attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{proc}

	// Fire the proc, then land the bolt (its own projectile).
	s.rollEquipmentProcsLocked(attacker, target)
	if len(s.Projectiles) != 1 {
		t.Fatalf("expected 1 proc bolt, got %d", len(s.Projectiles))
	}
	// No slow before the bolt lands.
	if target.SlowedRemaining > 0 {
		t.Fatalf("target should not be slowed before the bolt lands, got %v", target.SlowedRemaining)
	}

	dead := []int{}
	s.landProjectileLocked(s.Projectiles[0], target, &dead)

	// A cold-typed proc lands on the COLD track, not the physical one.
	if target.ColdSlowedMultiplier != proc.Params.SlowMultiplier {
		t.Errorf("ColdSlowedMultiplier = %v, want %v (25%% slower on attack + move speed)", target.ColdSlowedMultiplier, proc.Params.SlowMultiplier)
	}
	if target.ColdSlowedRemaining != proc.Params.SlowDurationSeconds {
		t.Errorf("ColdSlowedRemaining = %v, want %v", target.ColdSlowedRemaining, proc.Params.SlowDurationSeconds)
	}
	// The physical track must stay untouched — the chill is separate.
	if target.SlowedRemaining != 0 {
		t.Errorf("cold chill must not touch the physical slow track, got SlowedRemaining=%v", target.SlowedRemaining)
	}
	// slowFactorLocked is the single seam both movement and attack cadence read,
	// so confirming it reflects the chill proves both speeds are reduced.
	if got := slowFactorLocked(target); got != proc.Params.SlowMultiplier {
		t.Errorf("effective slow factor = %v, want %v", got, proc.Params.SlowMultiplier)
	}
}

// TestSlow_ColdAndPhysicalStackSeparately asserts the two slow categories are
// tracked independently and compose multiplicatively — a unit carrying both a
// trap (physical) slow and a chill is slowed by their product, and each track
// keeps its own timer.
func TestSlow_ColdAndPhysicalStackSeparately(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xF207)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 100, MaxHP: 100}
	s.nextUnitID++
	s.addUnitLocked(u)

	// Physical slow (e.g. a trap) and a cold chill, different strengths/timers.
	const physMult, physDur = 0.7, 3.0
	const coldMult, coldDur = 0.75, 2.0
	s.ApplySlowLocked(u.ID, physMult, physDur)
	s.ApplyColdSlowLocked(u.ID, coldMult, coldDur)

	if u.SlowedMultiplier != physMult || u.SlowedRemaining != physDur {
		t.Errorf("physical track: mult=%v rem=%v, want %v / %v", u.SlowedMultiplier, u.SlowedRemaining, physMult, physDur)
	}
	if u.ColdSlowedMultiplier != coldMult || u.ColdSlowedRemaining != coldDur {
		t.Errorf("cold track: mult=%v rem=%v, want %v / %v", u.ColdSlowedMultiplier, u.ColdSlowedRemaining, coldMult, coldDur)
	}
	// Effective factor stacks multiplicatively.
	if got, want := slowFactorLocked(u), physMult*coldMult; math.Abs(got-want) > 1e-9 {
		t.Errorf("stacked slow factor = %v, want %v (%v × %v)", got, want, physMult, coldMult)
	}
}

// TestOnHitProc_NoChillWhenUnset guards the default: a proc without slow fields
// lands damage but applies no slow.
func TestOnHitProc_NoChillWhenUnset(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xF206)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 1_000_000, MaxHP: 1_000_000, X: 50, Y: 0}
	s.nextUnitID++
	s.addUnitLocked(target)

	attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{{Chance: 1.0, Params: ProcEffectParams{Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"}}}
	s.rollEquipmentProcsLocked(attacker, target)
	dead := []int{}
	s.landProjectileLocked(s.Projectiles[0], target, &dead)

	if target.SlowedRemaining > 0 {
		t.Errorf("a proc without a slow must not chill the target, got SlowedRemaining=%v", target.SlowedRemaining)
	}
}

// TestFrostSword_ProcIsWiredToChill guards the shipped catalog: the frost_sword
// proc carries a valid chill (a real slow multiplier in (0,1) for a positive
// duration). Asserted as invariants, not pinned numbers.
func TestFrostSword_ProcIsWiredToChill(t *testing.T) {
	def, ok := getItemDef("frost_sword")
	if !ok {
		t.Fatal("frost_sword not in catalog")
	}
	p := def.OnHitProc
	if p == nil {
		t.Fatal("frost_sword has no onHitProc")
	}
	params, ok := p.ResolveParams()
	if !ok {
		t.Fatalf("frost_sword onHitProc.effect %q is not a registered proc effect", p.Effect)
	}
	if !(params.SlowMultiplier > 0 && params.SlowMultiplier < 1) {
		t.Errorf("frost_sword chill needs a slow multiplier in (0,1), got %v", params.SlowMultiplier)
	}
	if params.SlowDurationSeconds <= 0 {
		t.Errorf("frost_sword chill needs a positive duration, got %v", params.SlowDurationSeconds)
	}
}
