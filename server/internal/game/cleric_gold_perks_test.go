package game

// Cleric Gold perk unit tests.
//
// Covers all three Gold Cleric perks: divine_intervention, beacon_of_life,
// divine_judgement, plus the HealMeta plumbing they all share. Setup mirrors
// cleric_silver_perks_test.go.
//
// Tunable values are read from the catalog via perkDefByID so a JSON tuning
// pass never silently invalidates the suite.

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────────────────────
// divine_intervention
// ─────────────────────────────────────────────────────────────────────────────

// TestDivineIntervention_RevivesDyingAlly grants the perk to one cleric, puts
// an ally in lethal damage range, and verifies the ally is revived with HP
// equal to healAmount and tagged with InvulnerabilityRemaining.
func TestDivineIntervention_RevivesDyingAlly(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "divine_intervention")
	def := perkDefByID("divine_intervention")
	if def == nil {
		t.Fatal("divine_intervention perk def not found")
	}
	cfg := def.ConfigForRank(cleric.Rank)
	wantHP := int(math.Round(cfg["healAmount"]))
	wantInvuln := cfg["protectionDurationSeconds"]

	ally := spawnClericTestAlly(t, s, cleric.X+10, cleric.Y)
	ally.Armor = 0
	ally.HP = 5 // about to die

	s.applyUnitDamageWithSourceLocked(ally, 100, DamageSource{Kind: "melee"})

	if ally.HP <= 0 {
		t.Errorf("ally died despite divine_intervention; HP = %d", ally.HP)
	}
	if ally.HP != wantHP {
		t.Errorf("ally HP after intervention = %d, want %d (healAmount)", ally.HP, wantHP)
	}
	if math.Abs(ally.PerkState.InvulnerabilityRemaining-wantInvuln) > 1e-6 {
		t.Errorf("InvulnerabilityRemaining = %.3f, want %.3f", ally.PerkState.InvulnerabilityRemaining, wantInvuln)
	}
	if cleric.PerkState.DivineInterventionCooldownRemaining != cfg["cooldownSeconds"] {
		t.Errorf("cleric cooldown after save = %.3f, want %.3f",
			cleric.PerkState.DivineInterventionCooldownRemaining, cfg["cooldownSeconds"])
	}
}

// TestDivineIntervention_DoesNotFireWhileOnCooldown verifies that a second
// lethal hit while the cooldown is non-zero kills the ally normally.
func TestDivineIntervention_DoesNotFireWhileOnCooldown(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "divine_intervention")
	def := perkDefByID("divine_intervention")
	if def == nil {
		t.Fatal("divine_intervention perk def not found")
	}
	cfg := def.ConfigForRank(cleric.Rank)

	// Force cooldown active.
	cleric.PerkState.DivineInterventionCooldownRemaining = cfg["cooldownSeconds"]

	ally := spawnClericTestAlly(t, s, cleric.X+10, cleric.Y)
	ally.Armor = 0
	ally.HP = 5

	s.applyUnitDamageWithSourceLocked(ally, 100, DamageSource{Kind: "melee"})

	if ally.HP > 0 {
		t.Errorf("intervention fired despite cooldown; ally HP = %d", ally.HP)
	}
}

// TestDivineIntervention_OutsideRadiusNoSave puts the ally outside triggerRadius
// and asserts the ally dies (the saver cannot reach).
func TestDivineIntervention_OutsideRadiusNoSave(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "divine_intervention")
	def := perkDefByID("divine_intervention")
	if def == nil {
		t.Fatal("divine_intervention perk def not found")
	}
	cfg := def.ConfigForRank(cleric.Rank)

	ally := spawnClericTestAlly(t, s, cleric.X+cfg["triggerRadius"]+50, cleric.Y)
	ally.Armor = 0
	ally.HP = 5

	s.applyUnitDamageWithSourceLocked(ally, 100, DamageSource{Kind: "melee"})

	if ally.HP > 0 {
		t.Errorf("intervention fired outside radius; ally HP = %d", ally.HP)
	}
}

// TestDivineIntervention_InvulnerabilityBlocksFollowupHits verifies the brief
// post-save window absorbs every incoming damage instance until it decays.
func TestDivineIntervention_InvulnerabilityBlocksFollowupHits(t *testing.T) {
	s, _ := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	ally := spawnClericTestAlly(t, s, 500, 400)
	ally.Armor = 0
	ally.HP = ally.MaxHP
	ally.PerkState.InvulnerabilityRemaining = 1.0
	startHP := ally.HP

	// Multiple follow-up hits — all should be negated while invulnerable.
	for i := 0; i < 3; i++ {
		s.applyUnitDamageWithSourceLocked(ally, 50, DamageSource{Kind: "melee"})
	}

	if ally.HP != startHP {
		t.Errorf("invulnerable ally took damage: HP %d → %d", startHP, ally.HP)
	}
}

// TestDivineIntervention_RevivedDoesNotTriggerJudgement asserts that the
// revive HP restore does NOT route through applyClericHealLocked — divine_judgement
// must not detonate AoE around a freshly-saved unit (per gold cleric design).
func TestDivineIntervention_RevivedDoesNotTriggerJudgement(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "divine_intervention")
	grantPerk(cleric, "divine_judgement")

	ally := spawnClericTestAlly(t, s, cleric.X+10, cleric.Y)
	ally.Armor = 0
	ally.HP = 5

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: ally.X + 10, Y: ally.Y})
	enemy.Visible = true
	enemy.Armor = 0
	enemyStartHP := enemy.HP

	s.applyUnitDamageWithSourceLocked(ally, 100, DamageSource{Kind: "melee"})

	if enemy.HP < enemyStartHP {
		t.Errorf("intervention save triggered divine_judgement AoE: enemy HP %d → %d", enemyStartHP, enemy.HP)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// beacon_of_life
// ─────────────────────────────────────────────────────────────────────────────

// TestBeaconOfLife_SplashHealsNearbyAllies casts a primary heal and asserts
// nearby allies receive splashHealPercent of the primary amount.
func TestBeaconOfLife_SplashHealsNearbyAllies(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "beacon_of_life")
	def := perkDefByID("beacon_of_life")
	if def == nil {
		t.Fatal("beacon_of_life perk def not found")
	}
	cfg := def.ConfigForRank(cleric.Rank)
	splashPct := cfg["splashHealPercent"]

	primaryAmount := 30
	wantSplash := int(math.Round(float64(primaryAmount) * splashPct))

	primary := spawnClericTestAlly(t, s, 500, 400)
	primary.HP = primary.MaxHP / 2

	// Splash victim within splashRadius of primary.
	splash := spawnClericTestAlly(t, s, primary.X+10, primary.Y)
	splash.HP = splash.MaxHP - wantSplash - 5 // missing more than the splash
	splashStartHP := splash.HP

	s.applyClericHealLocked(cleric, primary, primaryAmount, healMetaPrimaryAbility())

	if splash.HP != splashStartHP+wantSplash {
		t.Errorf("splash ally HP: %d → %d, want %d", splashStartHP, splash.HP, splashStartHP+wantSplash)
	}
}

// TestBeaconOfLife_PrimaryNotInSplash verifies the primary target is excluded
// from the splash loop (no double heal).
func TestBeaconOfLife_PrimaryNotInSplash(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "beacon_of_life")

	primaryAmount := 30
	primary := spawnClericTestAlly(t, s, 500, 400)
	primary.HP = primary.MaxHP - primaryAmount - 50 // missing well over the primary
	primaryStartHP := primary.HP

	s.applyClericHealLocked(cleric, primary, primaryAmount, healMetaPrimaryAbility())

	if primary.HP != primaryStartHP+primaryAmount {
		t.Errorf("primary HP %d → %d, want %d (single primary heal, no splash double-up)",
			primaryStartHP, primary.HP, primaryStartHP+primaryAmount)
	}
}

// TestBeaconOfLife_SplashDoesNotChain verifies that a beacon splash heal does
// NOT itself trigger another splash. We measure: a splash victim should NOT
// cause an additional heal to a different ally nearby.
func TestBeaconOfLife_SplashDoesNotChain(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "beacon_of_life")
	def := perkDefByID("beacon_of_life")
	if def == nil {
		t.Fatal("beacon_of_life perk def not found")
	}
	cfg := def.ConfigForRank(cleric.Rank)
	splashPct := cfg["splashHealPercent"]
	splashRadius := cfg["splashRadius"]

	primary := spawnClericTestAlly(t, s, 500, 400)
	primary.HP = primary.MaxHP / 2

	// First splash victim adjacent to primary.
	splashA := spawnClericTestAlly(t, s, primary.X+10, primary.Y)
	splashA.HP = splashA.MaxHP / 2

	// Second ally near splashA but FAR from primary (just outside splashRadius
	// of primary). If chaining happened, splashA's splash would heal this one.
	farFromPrimary := primary.X + splashRadius + 10
	splashB := spawnClericTestAlly(t, s, farFromPrimary, primary.Y)
	splashB.HP = splashB.MaxHP - 100 // missing a lot so any heal would show
	splashBStartHP := splashB.HP

	primaryAmount := 30
	s.applyClericHealLocked(cleric, primary, primaryAmount, healMetaPrimaryAbility())

	if splashB.HP != splashBStartHP {
		// If splashA's splash chained, splashB would gain splashPct * splashAmount.
		expectedChainGain := int(math.Round(float64(primaryAmount) * splashPct * splashPct))
		t.Errorf("splashB HP %d → %d (gain %d); chain heal detected — expected chain would be %d. Beacon must not re-trigger from a splash heal.",
			splashBStartHP, splashB.HP, splashB.HP-splashBStartHP, expectedChainGain)
	}
}

// TestBeaconOfLife_RestorationAuraDoesNotSplash verifies the metadata gate:
// restoration aura's HealMeta has CanTriggerBeacon = false so its pulses do
// not splash.
func TestBeaconOfLife_RestorationAuraDoesNotSplash(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "restoration_aura")
	grantPerk(cleric, "beacon_of_life")

	primary := spawnClericTestAlly(t, s, cleric.X+10, cleric.Y)
	primary.HP = primary.MaxHP / 2

	splashVictim := spawnClericTestAlly(t, s, primary.X+10, primary.Y)
	splashVictim.HP = splashVictim.MaxHP - 100
	splashVictimStartHP := splashVictim.HP

	// Drive the aura tick — should heal primary AND splashVictim directly (both
	// are in the aura radius), but the metadata says no Beacon. So splashVictim
	// should ONLY receive the direct aura heal, not aura+splash.
	raDef := perkDefByID("restoration_aura")
	if raDef == nil {
		t.Fatal("restoration_aura perk def not found")
	}
	raCfg := raDef.ConfigForRank(cleric.Rank)
	wantAuraHeal := int(math.Round(raCfg["healAmount"]))

	s.tickRestorationAuraPulseLocked(cleric, raDef, 0.05)

	if splashVictim.HP != splashVictimStartHP+wantAuraHeal {
		t.Errorf("splashVictim HP %d → %d, want exactly %d (aura heal only — no beacon splash from aura pulse)",
			splashVictimStartHP, splashVictim.HP, splashVictimStartHP+wantAuraHeal)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// divine_judgement
// ─────────────────────────────────────────────────────────────────────────────

// TestDivineJudgement_DealsHolyDamageAroundHealedUnit casts a heal that
// triggers judgement and verifies enemies in radius take intendedAmount damage.
func TestDivineJudgement_DealsHolyDamageAroundHealedUnit(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "divine_judgement")
	def := perkDefByID("divine_judgement")
	if def == nil {
		t.Fatal("divine_judgement perk def not found")
	}
	cfg := def.ConfigForRank(cleric.Rank)
	radius := cfg["radius"]

	healAmount := 30

	primary := spawnClericTestAlly(t, s, 500, 400)
	primary.HP = primary.MaxHP / 2

	// Enemy inside judgement radius of the primary.
	enemyIn := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: primary.X + radius*0.5, Y: primary.Y})
	enemyIn.Visible = true
	enemyIn.Armor = 0
	enemyInStartHP := enemyIn.HP

	// Enemy outside judgement radius.
	enemyOut := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: primary.X + radius*2, Y: primary.Y})
	enemyOut.Visible = true
	enemyOut.Armor = 0
	enemyOutStartHP := enemyOut.HP

	s.applyClericHealLocked(cleric, primary, healAmount, healMetaPrimaryAbility())

	if enemyIn.HP != enemyInStartHP-healAmount {
		t.Errorf("enemy-in HP %d → %d, want %d (full heal amount as holy damage)",
			enemyInStartHP, enemyIn.HP, enemyInStartHP-healAmount)
	}
	if enemyOut.HP != enemyOutStartHP {
		t.Errorf("enemy-out HP %d → %d, want %d (outside radius)",
			enemyOutStartHP, enemyOut.HP, enemyOutStartHP)
	}
}

// TestDivineJudgement_FiresOnFullHPHeal asserts the AoE still detonates at
// full strength when the target is at full HP (heal does nothing to the
// target's HP but the judgement still fires using the INTENDED amount).
func TestDivineJudgement_FiresOnFullHPHeal(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "divine_judgement")
	def := perkDefByID("divine_judgement")
	if def == nil {
		t.Fatal("divine_judgement perk def not found")
	}
	cfg := def.ConfigForRank(cleric.Rank)
	radius := cfg["radius"]

	healAmount := 30

	primary := spawnClericTestAlly(t, s, 500, 400)
	primary.HP = primary.MaxHP // FULL HP — heal would do nothing visible

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: primary.X + radius*0.5, Y: primary.Y})
	enemy.Visible = true
	enemy.Armor = 0
	enemyStartHP := enemy.HP

	s.applyClericHealLocked(cleric, primary, healAmount, healMetaPrimaryAbility())

	if enemy.HP != enemyStartHP-healAmount {
		t.Errorf("full-HP heal: enemy HP %d → %d, want %d (judgement must use intended amount, not post-clamp delta)",
			enemyStartHP, enemy.HP, enemyStartHP-healAmount)
	}
}

// TestDivineJudgement_RestorationAuraTriggers verifies that restoration aura
// pulses trigger judgement (per the spec: "Restoration Aura can trigger
// Divine Judgement"). Geometry note: restoration aura heals the cleric AND
// allies, so the judgement detonates around EACH healed unit. We place the
// enemy in the ally's judgement radius but outside the cleric's so we measure
// exactly one detonation (the ally's) for the assertion to be deterministic.
func TestDivineJudgement_RestorationAuraTriggers(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "restoration_aura")
	grantPerk(cleric, "divine_judgement")

	raDef := perkDefByID("restoration_aura")
	jDef := perkDefByID("divine_judgement")
	if raDef == nil || jDef == nil {
		t.Fatal("perk def missing")
	}
	raCfg := raDef.ConfigForRank(cleric.Rank)
	jCfg := jDef.ConfigForRank(cleric.Rank)
	healAmount := int(math.Round(raCfg["healAmount"]))
	jRadius := jCfg["radius"]

	// Place ally inside aura range (192) but far enough that the enemy can be
	// in ally's judgement radius without also being in the cleric's.
	ally := spawnClericTestAlly(t, s, cleric.X+100, cleric.Y)
	ally.HP = ally.MaxHP / 2

	// Enemy at ally.X + jRadius/2 = ally + 20, so 100+20 = 120 from cleric
	// (outside cleric's judgement) and 20 from ally (inside ally's judgement).
	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: ally.X + jRadius*0.5, Y: ally.Y})
	enemy.Visible = true
	enemy.Armor = 0
	enemyStartHP := enemy.HP

	s.tickRestorationAuraPulseLocked(cleric, raDef, 0.05)

	if enemy.HP != enemyStartHP-healAmount {
		t.Errorf("aura-triggered judgement: enemy HP %d → %d, want %d (one detonation at heal amount)",
			enemyStartHP, enemy.HP, enemyStartHP-healAmount)
	}
}

// TestDivineJudgement_AlliesNotDamaged confirms friendly units inside the AoE
// are NOT hit (judgement only targets hostiles).
func TestDivineJudgement_AlliesNotDamaged(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "divine_judgement")
	def := perkDefByID("divine_judgement")
	if def == nil {
		t.Fatal("divine_judgement perk def not found")
	}
	cfg := def.ConfigForRank(cleric.Rank)
	radius := cfg["radius"]

	primary := spawnClericTestAlly(t, s, 500, 400)
	primary.HP = primary.MaxHP / 2

	bystander := spawnClericTestAlly(t, s, primary.X+radius*0.5, primary.Y)
	bystander.Armor = 0
	bystanderStartHP := bystander.HP

	s.applyClericHealLocked(cleric, primary, 30, healMetaPrimaryAbility())

	if bystander.HP != bystanderStartHP {
		t.Errorf("ally hit by judgement: HP %d → %d (should be unchanged)", bystanderStartHP, bystander.HP)
	}
}

// TestDivineJudgement_DivineHealerScalesDamage confirms that when divine_healer
// scales the heal amount, the resulting judgement damage scales too (because
// judgement uses the post-multiplier intended amount).
func TestDivineJudgement_DivineHealerScalesDamage(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "divine_healer")
	grantPerk(cleric, "divine_judgement")

	jDef := perkDefByID("divine_judgement")
	dhDef := perkDefByID("divine_healer")
	if jDef == nil || dhDef == nil {
		t.Fatal("perk def missing")
	}
	jCfg := jDef.ConfigForRank(cleric.Rank)
	dhCfg := dhDef.ConfigForRank(cleric.Rank)
	radius := jCfg["radius"]

	healAmount := 30
	// applyClericHealLocked itself does NOT scale by divine_healer (the scaling
	// happens in resolveAbilityCastOnTargetLocked / tickRestorationAuraPulseLocked
	// before calling the helper). So this test passes the already-scaled amount
	// and asserts that's what judgement uses.
	scaledAmount := int(math.Round(float64(healAmount) * dhCfg["healMultiplier"]))

	primary := spawnClericTestAlly(t, s, 500, 400)
	primary.HP = primary.MaxHP - scaledAmount - 5

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: primary.X + radius*0.5, Y: primary.Y})
	enemy.Visible = true
	enemy.Armor = 0
	enemyStartHP := enemy.HP

	s.applyClericHealLocked(cleric, primary, scaledAmount, healMetaPrimaryAbility())

	if enemy.HP != enemyStartHP-scaledAmount {
		t.Errorf("divine_healer-scaled judgement: enemy HP %d → %d, want %d (damage = scaled heal)",
			enemyStartHP, enemy.HP, enemyStartHP-scaledAmount)
	}
}

// TestDivineJudgement_NoRecursion asserts that judgement damage cannot chain
// into another judgement. We use a single cleric with judgement and verify
// each enemy is hit exactly once (one damage instance), not multiple times
// from cascading triggers.
func TestDivineJudgement_NoRecursion(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "divine_judgement")
	def := perkDefByID("divine_judgement")
	if def == nil {
		t.Fatal("divine_judgement perk def not found")
	}
	cfg := def.ConfigForRank(cleric.Rank)
	radius := cfg["radius"]

	healAmount := 30

	primary := spawnClericTestAlly(t, s, 500, 400)
	primary.HP = primary.MaxHP / 2

	// Two enemies inside the radius.
	enemyA := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: primary.X + radius*0.3, Y: primary.Y})
	enemyA.Visible = true
	enemyA.Armor = 0
	enemyAStartHP := enemyA.HP

	enemyB := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: primary.X - radius*0.3, Y: primary.Y})
	enemyB.Visible = true
	enemyB.Armor = 0
	enemyBStartHP := enemyB.HP

	s.applyClericHealLocked(cleric, primary, healAmount, healMetaPrimaryAbility())

	// Each enemy should take exactly one judgement damage instance.
	if enemyA.HP != enemyAStartHP-healAmount {
		t.Errorf("enemyA HP %d → %d, want %d (one judgement hit, no recursion)",
			enemyAStartHP, enemyA.HP, enemyAStartHP-healAmount)
	}
	if enemyB.HP != enemyBStartHP-healAmount {
		t.Errorf("enemyB HP %d → %d, want %d (one judgement hit, no recursion)",
			enemyBStartHP, enemyB.HP, enemyBStartHP-healAmount)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HealMeta — combined / interaction tests
// ─────────────────────────────────────────────────────────────────────────────

// TestHealMeta_BeaconSplashStillTriggersJudgement is the canonical "all three
// gold perks working together" integration check. A cleric with beacon AND
// judgement heals a primary; the splash heals neighbours; each splash victim
// detonates judgement around themselves.
func TestHealMeta_BeaconSplashStillTriggersJudgement(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "beacon_of_life")
	grantPerk(cleric, "divine_judgement")

	beaconDef := perkDefByID("beacon_of_life")
	jDef := perkDefByID("divine_judgement")
	if beaconDef == nil || jDef == nil {
		t.Fatal("perk def missing")
	}
	beaconCfg := beaconDef.ConfigForRank(cleric.Rank)
	jCfg := jDef.ConfigForRank(cleric.Rank)
	splashPct := beaconCfg["splashHealPercent"]
	jRadius := jCfg["radius"]

	healAmount := 30
	splashAmount := int(math.Round(float64(healAmount) * splashPct))

	primary := spawnClericTestAlly(t, s, 500, 400)
	primary.HP = primary.MaxHP / 2

	// Splash victim placed close to primary (inside beacon splashRadius) but
	// far enough that the enemy near splash can be outside primary's judgement
	// radius. With jRadius=40 and beacon splashRadius=90 we put the splash
	// victim 60 away from primary — well within splash, well outside the
	// primary's judgement footprint via the enemy ahead of the splash victim.
	splashVictim := spawnClericTestAlly(t, s, primary.X+60, primary.Y)
	splashVictim.HP = splashVictim.MaxHP / 2

	// Enemy placed on the FAR SIDE of the splash victim, jRadius*0.5 from it,
	// so it sits in splash victim's judgement footprint but well outside
	// primary's judgement footprint.
	enemyNearSplash := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: splashVictim.X + jRadius*0.5, Y: splashVictim.Y})
	enemyNearSplash.Visible = true
	enemyNearSplash.Armor = 0
	// Sanity-check geometry — fail loudly rather than silently skip so a
	// future tuning change that breaks the test gets noticed.
	dx := enemyNearSplash.X - primary.X
	dy := enemyNearSplash.Y - primary.Y
	if dx*dx+dy*dy <= jRadius*jRadius {
		t.Fatalf("test geometry: enemy is inside primary's judgement radius (dist=%.0f, jRadius=%.0f) — tune perk values shifted the test layout",
			math.Sqrt(dx*dx+dy*dy), jRadius)
	}
	enemyStartHP := enemyNearSplash.HP

	s.applyClericHealLocked(cleric, primary, healAmount, healMetaPrimaryAbility())

	// The splash victim's judgement detonates with splashAmount damage.
	if enemyNearSplash.HP != enemyStartHP-splashAmount {
		t.Errorf("splash-triggered judgement: enemy HP %d → %d, want %d (splash heal triggers judgement at splash amount)",
			enemyStartHP, enemyNearSplash.HP, enemyStartHP-splashAmount)
	}
}
