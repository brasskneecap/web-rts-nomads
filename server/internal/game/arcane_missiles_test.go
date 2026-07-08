package game

import "testing"

func arcaneMissilesDef(t *testing.T) AbilityDef {
	t.Helper()
	def, ok := getAbilityDef("arcane_missiles")
	if !ok {
		t.Fatal(`getAbilityDef("arcane_missiles") missing`)
	}
	if !def.IsChargeFirePassive() {
		t.Fatalf("arcane_missiles should be a charge-fire passive; type=%q chargeRequired=%v", def.Type, def.ChargeRequired)
	}
	return def
}

// Charge accrues on mana spend ONLY for a unit that owns the passive.
func TestArcaneCharge_AccruesOnManaSpendForOwnersOnly(t *testing.T) {
	def := arcaneMissilesDef(t)
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	mage := spawnProjTestUnit(t, s, "p1", 100, 100)
	mage.Abilities = []string{"arcane_missiles"}
	mage.MaxMana = 100
	mage.CurrentMana = 100
	if !s.spendUnitManaLocked(mage, 10) {
		t.Fatal("mana spend failed")
	}
	if mage.ArcaneCharge != 10*def.ManaToChargeRatio {
		t.Errorf("ArcaneCharge = %v; want %v", mage.ArcaneCharge, 10*def.ManaToChargeRatio)
	}

	// A unit without the passive never accrues charge.
	plain := spawnProjTestUnit(t, s, "p1", 200, 100)
	plain.MaxMana = 100
	plain.CurrentMana = 100
	s.spendUnitManaLocked(plain, 10)
	if plain.ArcaneCharge != 0 {
		t.Errorf("non-owner ArcaneCharge = %v; want 0", plain.ArcaneCharge)
	}
}

// At threshold the passive fires exactly MissileCount missiles at in-range
// enemies and resets charge (carrying overflow).
func TestArcaneMissiles_FiresVolleyAtThreshold(t *testing.T) {
	def := arcaneMissilesDef(t)
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	mage := spawnProjTestUnit(t, s, "p1", 100, 100)
	mage.Abilities = []string{"arcane_missiles"}
	mage.AttackRange = 300
	e1 := spawnProjTestUnit(t, s, enemyPlayerID, 200, 100)
	e2 := spawnProjTestUnit(t, s, enemyPlayerID, 150, 150)
	_ = e1
	_ = e2

	mage.ArcaneCharge = def.ChargeRequired + 5 // over threshold
	before := len(s.Projectiles)
	// The volley is staggered (one bolt every 60ms); advance enough ticks to
	// launch all of them. Projectiles are not ticked here, so they accumulate.
	for i := 0; i < 6; i++ {
		s.tickArcaneMissilesLocked(0.05)
	}
	if got := len(s.Projectiles) - before; got != def.MissileCount {
		t.Errorf("fired %d missiles; want %d", got, def.MissileCount)
	}
	if mage.ArcaneCharge != 5 {
		t.Errorf("charge after fire = %v; want 5 (overflow carried)", mage.ArcaneCharge)
	}

	// Below threshold: no fire.
	mage.ArcaneCharge = def.ChargeRequired - 1
	before = len(s.Projectiles)
	for i := 0; i < 6; i++ {
		s.tickArcaneMissilesLocked(0.05)
	}
	if len(s.Projectiles) != before {
		t.Error("fired below threshold")
	}
}

// With no enemy in range, a charged mage banks its charge (no wasted volley).
func TestArcaneMissiles_BanksChargeWhenNoTarget(t *testing.T) {
	def := arcaneMissilesDef(t)
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	mage := spawnProjTestUnit(t, s, "p1", 100, 100)
	mage.Abilities = []string{"arcane_missiles"}
	mage.AttackRange = 100
	// enemy far outside range
	spawnProjTestUnit(t, s, enemyPlayerID, 5000, 5000)
	mage.ArcaneCharge = def.ChargeRequired + 3
	before := len(s.Projectiles)
	for i := 0; i < 6; i++ {
		s.tickArcaneMissilesLocked(0.05)
	}
	if len(s.Projectiles) != before {
		t.Error("fired with no in-range target")
	}
	if mage.ArcaneCharge != def.ChargeRequired+3 {
		t.Errorf("charge = %v; want banked %v", mage.ArcaneCharge, def.ChargeRequired+3)
	}
}

// The passive is never manually castable.
func TestArcaneMissiles_NotManuallyCastable(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	mage := spawnProjTestUnit(t, s, "p1", 100, 100)
	mage.Abilities = []string{"arcane_missiles"}
	mage.AttackRange = 300
	mage.MaxMana = 100
	mage.CurrentMana = 100
	enemy := spawnProjTestUnit(t, s, enemyPlayerID, 200, 100)
	if ok, _ := s.beginAbilityCastLocked(mage, "arcane_missiles", enemy); ok {
		t.Error("passive arcane_missiles must not be manually castable")
	}
}

// Missile targeting is seed-deterministic.
func TestArcaneMissiles_DeterministicTargeting(t *testing.T) {
	def := arcaneMissilesDef(t)
	run := func() []int {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 99)
		s.mu.Lock()
		defer s.mu.Unlock()
		mage := spawnProjTestUnit(t, s, "p1", 100, 100)
		mage.Abilities = []string{"arcane_missiles"}
		mage.AttackRange = 400
		for i := 0; i < 4; i++ {
			spawnProjTestUnit(t, s, enemyPlayerID, float64(150+i*40), 120)
		}
		mage.ArcaneCharge = def.ChargeRequired
		before := len(s.Projectiles)
		for i := 0; i < 6; i++ {
			s.tickArcaneMissilesLocked(0.05)
		}
		var targets []int
		for _, p := range s.Projectiles[before:] {
			targets = append(targets, p.TargetUnitID)
		}
		return targets
	}
	a, b := run(), run()
	if len(a) != len(b) {
		t.Fatalf("length mismatch %v vs %v", a, b)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("non-deterministic targeting: %v vs %v", a, b)
		}
	}
}

// A promoted Arch Mage knows arcane_missiles (passive) + a bronze slot spell,
// and NOT arcane_bolt.
func TestArchMage_IdentityAfterPromotion(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	mage := spawnProjTestUnit(t, s, "p1", 100, 100)
	mage.UnitType = "adept"
	mage.ProgressionPath = "arch_mage"
	mage.Rank = "bronze"
	mage.Abilities = nil
	s.rollUnitPoolSpellsLocked(mage)
	s.assignUnitPathAbilitiesLocked(mage)

	if !containsStr(mage.Abilities, "arcane_missiles") {
		t.Errorf("Arch Mage missing arcane_missiles passive; Abilities=%v", mage.Abilities)
	}
	if containsStr(mage.Abilities, "arcane_bolt") {
		t.Errorf("Arch Mage still has arcane_bolt (should be replaced); Abilities=%v", mage.Abilities)
	}
	slot := mage.PoolSpellsByRank["bronze"]
	if slot == "" || !containsStr(mage.Abilities, slot) {
		t.Errorf("Arch Mage missing its bronze slot spell %q; Abilities=%v", slot, mage.Abilities)
	}
}

// Arcane Missiles' per-missile damage registers as MINOR damage events (side-
// falling popups) with the arcane color variant, not the main damage number.
func TestArcaneMissiles_DamageIsMinor(t *testing.T) {
	def := arcaneMissilesDef(t)
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	mage := spawnProjTestUnit(t, s, "p1", 100, 100)
	mage.Abilities = []string{"arcane_missiles"}
	mage.AttackRange = 300
	enemy := spawnProjTestUnit(t, s, enemyPlayerID, 150, 100)
	enemy.MaxHP = 100000
	enemy.HP = 100000
	enemy.MoveSpeed = 0
	startHP := enemy.HP

	mage.ArcaneCharge = def.ChargeRequired
	// Fire the staggered volley AND land each missile; minor events accumulate
	// across ticks (no Update, so no end-of-tick reset).
	for i := 0; i < 14; i++ {
		s.tickArcaneMissilesLocked(0.05)
		s.tickProjectilesLocked(0.05)
	}

	if enemy.HP >= startHP {
		t.Fatal("missiles dealt no damage")
	}
	if len(s.minorDamageEventsThisTick) != def.MissileCount {
		t.Errorf("minor damage events = %d; want %d (one per missile)", len(s.minorDamageEventsThisTick), def.MissileCount)
	}
	for _, e := range s.minorDamageEventsThisTick {
		if e.Variant != "arcane" {
			t.Errorf("minor event variant = %q; want %q", e.Variant, "arcane")
		}
		if e.Damage <= 0 {
			t.Errorf("minor event damage = %d; want > 0", e.Damage)
		}
	}
}
