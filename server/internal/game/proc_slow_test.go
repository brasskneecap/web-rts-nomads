package game

import (
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

	procParams := ProcEffectParams{
		Damage: 25, DamageType: DamageCold, ProjectileID: "frost_bolt",
		SlowMultiplier: 0.75, SlowDurationSeconds: 2,
	}

	// Fire the proc effect directly, then land the bolt (its own projectile).
	s.executeProcEffectLocked(procSourceFromUnit(attacker), target, procParams)
	if len(s.Projectiles) != 1 {
		t.Fatalf("expected 1 proc bolt, got %d", len(s.Projectiles))
	}
	// No slow before the bolt lands.
	if target.SlowedRemaining > 0 {
		t.Fatalf("target should not be slowed before the bolt lands, got %v", target.SlowedRemaining)
	}

	dead := []int{}
	s.landProjectileLocked(s.Projectiles[0], target, &dead)

	// The proc's slow lands on the one generic slow track (the separate cold
	// track was retired). Move + attack speed both scale by SlowMultiplier.
	if target.SlowedMultiplier != procParams.SlowMultiplier {
		t.Errorf("SlowedMultiplier = %v, want %v (25%% slower on attack + move speed)", target.SlowedMultiplier, procParams.SlowMultiplier)
	}
	if target.SlowedRemaining != procParams.SlowDurationSeconds {
		t.Errorf("SlowedRemaining = %v, want %v", target.SlowedRemaining, procParams.SlowDurationSeconds)
	}
	// slowFactorLocked is the single seam both movement and attack cadence read,
	// so confirming it reflects the slow proves both speeds are reduced.
	if got := slowFactorLocked(target); got != procParams.SlowMultiplier {
		t.Errorf("effective slow factor = %v, want %v", got, procParams.SlowMultiplier)
	}
}

// TestSlow_RefreshStrongerAndLonger asserts the single slow track's refresh
// policy: a second slow keeps the stronger (lower) multiplier and the longer
// remaining duration, independently.
func TestSlow_RefreshStrongerAndLonger(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xF207)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 100, MaxHP: 100}
	s.nextUnitID++
	s.addUnitLocked(u)

	s.ApplySlowLocked(u.ID, 0.7, 3.0)  // weaker but longer
	s.ApplySlowLocked(u.ID, 0.5, 1.0)  // stronger but shorter
	if u.SlowedMultiplier != 0.5 {     // refresh-stronger keeps the lower mult
		t.Errorf("SlowedMultiplier = %v, want 0.5 (refresh-stronger)", u.SlowedMultiplier)
	}
	if u.SlowedRemaining != 3.0 { // refresh-longer keeps the greater duration
		t.Errorf("SlowedRemaining = %v, want 3.0 (refresh-longer)", u.SlowedRemaining)
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

	s.executeProcEffectLocked(procSourceFromUnit(attacker), target, ProcEffectParams{Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"})
	dead := []int{}
	s.landProjectileLocked(s.Projectiles[0], target, &dead)

	if target.SlowedRemaining > 0 {
		t.Errorf("a proc without a slow must not chill the target, got SlowedRemaining=%v", target.SlowedRemaining)
	}
}

// TestFrostSword_ProcCastsFrostBolt guards the shipped catalog: the frost_sword
// proc now CASTS the Frost Bolt ability (the full-circle wiring) rather than
// firing a bespoke proc effect. Asserts the wiring is an ability reference to a
// registered cold ability whose program carries the chill composition.
func TestFrostSword_ProcCastsFrostBolt(t *testing.T) {
	def, ok := getItemDef("frost_sword")
	if !ok {
		t.Fatal("frost_sword not in catalog")
	}
	p := firstProcFor(t, def, ProcOnHit)
	if p.Ability == "" {
		t.Fatalf("frost_sword proc should cast an ability, got ability=%q", p.Ability)
	}
	adef, ok := getAbilityDef(p.Ability)
	if !ok {
		t.Fatalf("frost_sword proc ability %q is not a registered ability", p.Ability)
	}
	if adef.DamageType != DamageCold {
		t.Errorf("frost_sword proc ability %q damageType = %q, want cold", p.Ability, adef.DamageType)
	}
	// The cold ability must chill: its program carries a change_stat that
	// multiplies moveSpeed (the composed slow) — recovered by the same tooltip
	// shadow that shatterDef uses.
	if m := abilityMechanicsShadow(adef); m.SlowMultiplier <= 0 || m.SlowMultiplier >= 1 {
		t.Errorf("frost_sword's ability %q should chill (slow multiplier in (0,1)), got %v", p.Ability, m.SlowMultiplier)
	}
}
