package game

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// spawnArchMageCasterLocked spawns an adept (the Arch Mage's base unit) owned by
// "p1" with the given Gold perk(s) assigned. Caller holds s.mu.
func spawnArchMageCasterLocked(s *GameState, perkIDs ...string) *Unit {
	if s.Players["p1"] == nil {
		s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	}
	caster := s.spawnPlayerUnitLocked("adept", "p1", "#3498db", protocol.Vec2{X: 300, Y: 300})
	caster.Visible = true
	caster.PerkIDs = append([]string(nil), perkIDs...)
	return caster
}

// TestArchMageGoldPerks_Defined verifies the three Gold perks load from the
// catalog with the correct path-derived eligibility, an icon, and sane config.
// Values are asserted as invariants (ranges), never pinned to exact balance
// numbers, so tuning gold.json can't silently break this.
func TestArchMageGoldPerks_Defined(t *testing.T) {
	for _, id := range []string{"arcane_feedback", "arcane_conduit", "unstable_magic"} {
		def := perkDefByID(id)
		if def == nil {
			t.Fatalf("%s: perk def missing from catalog", id)
		}
		if def.UnitType != "adept" || def.Path != "arch_mage" || def.Rank != "gold" {
			t.Errorf("%s: eligibility = %s/%s/%s; want adept/arch_mage/gold",
				id, def.UnitType, def.Path, def.Rank)
		}
		if def.Icon == "" {
			t.Errorf("%s: missing icon", id)
		}
	}
	if m := perkDefByID("arcane_feedback").Config["manaPerHit"]; m <= 0 {
		t.Errorf("arcane_feedback manaPerHit = %v; want > 0", m)
	}
	if pc := perkDefByID("unstable_magic").Config["procChance"]; pc <= 0 || pc > 1 {
		t.Errorf("unstable_magic procChance = %v; want (0,1]", pc)
	}
	if e := perkDefByID("unstable_magic").Config["effectiveness"]; e <= 0 || e > 1 {
		t.Errorf("unstable_magic effectiveness = %v; want (0,1]", e)
	}
}

// TestArchMageGoldPerks_EligibleAtGoldRank verifies an Arch Mage (adept /
// arch_mage) at gold rank has exactly the three new Gold perks in its eligible
// pool — so a gold rank-up (assignUnitPerkLocked) grants one of them. This is
// the server half of the "gold perk shows up" flow; the client then renders the
// granted perk in its gold cell. The expected set is derived from the catalog
// (all adept/arch_mage/gold PerkDefs), not hardcoded, so adding a fourth gold
// perk won't break it.
func TestArchMageGoldPerks_EligibleAtGoldRank(t *testing.T) {
	unit := &Unit{UnitType: "adept", ProgressionPath: "arch_mage"}
	pool := eligiblePerksForUnitAtRank(unit, "gold")

	got := make(map[string]bool, len(pool))
	for _, def := range pool {
		got[def.ID] = true
	}
	// Cross-check against the catalog: every adept/arch_mage/gold perk must be
	// eligible, and nothing else.
	want := make(map[string]bool)
	for _, def := range ListPerkDefs() {
		if def.UnitType == "adept" && def.Path == "arch_mage" && def.Rank == "gold" {
			want[def.ID] = true
		}
	}
	if len(want) == 0 {
		t.Fatal("no adept/arch_mage/gold perks in the catalog")
	}
	if len(got) != len(want) {
		t.Errorf("eligible gold pool size = %d; want %d (%v vs %v)", len(got), len(want), got, want)
	}
	for id := range want {
		if !got[id] {
			t.Errorf("gold perk %q should be eligible for a gold arch_mage but was not in the pool", id)
		}
	}
}

// TestArcaneFeedback_RestoresManaOnHit verifies an Arcane Missile hit restores
// the configured mana to the caster, and that a caster WITHOUT the perk gains
// nothing from the same hit. The amount is derived from the perk config.
func TestArcaneFeedback_RestoresManaOnHit(t *testing.T) {
	def := perkDefByID("arcane_feedback")
	if def == nil {
		t.Fatal("arcane_feedback perk def missing")
	}
	want := int(def.Config["manaPerHit"])

	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	target := spawnEnemy(t, s, 360, 300)

	// With the perk: mana climbs by manaPerHit.
	caster := spawnArchMageCasterLocked(s, "arcane_feedback")
	if caster.MaxMana <= 0 {
		t.Fatalf("adept should have a mana pool; MaxMana=%d", caster.MaxMana)
	}
	caster.CurrentMana = 0
	s.onArcaneMissileHitLocked(caster, target)
	if caster.CurrentMana != want {
		t.Errorf("mana after Arcane Missile hit = %d; want %d", caster.CurrentMana, want)
	}

	// Without the perk: the same hit restores nothing.
	noPerk := spawnArchMageCasterLocked(s)
	noPerk.CurrentMana = 0
	s.onArcaneMissileHitLocked(noPerk, target)
	if noPerk.CurrentMana != 0 {
		t.Errorf("caster without arcane_feedback gained mana: %d; want 0", noPerk.CurrentMana)
	}
}

// TestArcaneFeedback_RestoresManaOnBasicAttack verifies a landed basic attack
// restores the configured mana to an Arch Mage with the perk (via the on-hit
// reaction hub), and that a caster without the perk gains nothing. The amount
// is derived from the perk config.
func TestArcaneFeedback_RestoresManaOnBasicAttack(t *testing.T) {
	def := perkDefByID("arcane_feedback")
	if def == nil {
		t.Fatal("arcane_feedback perk def missing")
	}
	want := int(def.Config["manaPerBasicAttack"])
	if want <= 0 {
		t.Fatalf("arcane_feedback manaPerBasicAttack = %d; want > 0", want)
	}

	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	target := spawnEnemy(t, s, 360, 300)

	// With the perk: a landed basic attack restores manaPerBasicAttack.
	caster := spawnArchMageCasterLocked(s, "arcane_feedback")
	caster.CurrentMana = 0
	s.onPerkAttackDamageAppliedLocked(caster, target, 10)
	if caster.CurrentMana != want {
		t.Errorf("mana after basic attack = %d; want %d", caster.CurrentMana, want)
	}

	// Without the perk: nothing.
	noPerk := spawnArchMageCasterLocked(s)
	noPerk.CurrentMana = 0
	s.onPerkAttackDamageAppliedLocked(noPerk, target, 10)
	if noPerk.CurrentMana != 0 {
		t.Errorf("caster without arcane_feedback gained mana on basic attack: %d; want 0", noPerk.CurrentMana)
	}
}

// TestArcaneConduit_TriggersItemOnHitProcs verifies that with the perk an Arcane
// Missile hit runs the caster's equipped on-hit procs (a proc bolt is spawned),
// and that without the perk the same equipped proc never fires from a missile
// hit. The proc chance is forced to 1.0 so the assertion is deterministic.
func TestArcaneConduit_TriggersItemOnHitProcs(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	target := spawnEnemy(t, s, 360, 300)

	guaranteedProc := EquipmentProc{
		Chance: 1.0,
		Params: ProcEffectParams{Damage: 10, DamageType: "arcane", ProjectileID: "fire_bolt"},
	}

	// Without the perk: equipped proc does NOT fire from a missile hit.
	noPerk := spawnArchMageCasterLocked(s)
	noPerk.EquipmentBonus.OnHitProcs = []EquipmentProc{guaranteedProc}
	before := len(s.Projectiles)
	s.onArcaneMissileHitLocked(noPerk, target)
	if len(s.Projectiles) != before {
		t.Errorf("without arcane_conduit a missile hit fired an item proc: projectiles %d -> %d",
			before, len(s.Projectiles))
	}

	// With the perk: the proc fires (spawns exactly one proc bolt).
	caster := spawnArchMageCasterLocked(s, "arcane_conduit")
	caster.EquipmentBonus.OnHitProcs = []EquipmentProc{guaranteedProc}
	before = len(s.Projectiles)
	s.onArcaneMissileHitLocked(caster, target)
	if len(s.Projectiles) != before+1 {
		t.Errorf("arcane_conduit should fire the item on-hit proc: projectiles %d -> %d (want +1)",
			before, len(s.Projectiles))
	}
}

// TestUnstableMagic_CastsLearnedSpellAtReducedEffectiveness drives the Unstable
// Magic cast helper directly (bypassing the proc-chance roll) with a caster that
// has learned only fireball. It verifies a fireball projectile is launched, that
// it is free (no mana required / spent), and that its damage is scaled to the
// configured effectiveness — the scaled value derived from the catalog.
func TestUnstableMagic_CastsLearnedSpellAtReducedEffectiveness(t *testing.T) {
	fb, ok := getAbilityDef("fireball")
	if !ok {
		t.Fatal("fireball ability def missing")
	}
	if fb.TargetsPoint {
		t.Fatal("test assumes fireball is unit-targeted (no mana spent by resolveAbilityCastOnTargetLocked)")
	}
	// fireball is schemaVersion:2 as of the composable-abilities migration:
	// its DamageAmount is cleared (the compiled launch_projectile action's
	// Config.Amount is the sole authority now — see ConvertLegacyAbility).
	// abilityMechanicsShadow recovers the same magnitude from the Program so
	// this test's expectation still tracks the catalog instead of reading
	// the now-zeroed flat field directly.
	fbShadow := abilityMechanicsShadow(fb)
	if fbShadow.DamageAmount <= 0 {
		t.Fatalf("test assumes fireball deals direct damage; DamageAmount=%d", fbShadow.DamageAmount)
	}
	effect := perkDefByID("unstable_magic").Config["effectiveness"]

	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	caster := spawnArchMageCasterLocked(s, "unstable_magic")
	caster.CurrentMana = 0 // prove the proc is free — a normal cast would fail here
	caster.PoolSpellsByRank = map[string]string{"bronze": "fireball"}
	target := spawnEnemy(t, s, 360, 300)

	before := len(s.Projectiles)
	s.fireUnstableMagicLocked(caster, target, effect)
	if len(s.Projectiles) != before+1 {
		t.Fatalf("Unstable Magic should launch a fireball projectile: projectiles %d -> %d",
			before, len(s.Projectiles))
	}
	proj := s.Projectiles[len(s.Projectiles)-1]
	want := int(math.Round(float64(fbShadow.DamageAmount) * effect))
	if proj.Damage != want {
		t.Errorf("Unstable Magic fireball damage = %d; want %d (%.0f%% of %d)",
			proj.Damage, want, effect*100, fbShadow.DamageAmount)
	}
	if caster.CurrentMana != 0 {
		t.Errorf("Unstable Magic proc should be free; caster mana changed to %d", caster.CurrentMana)
	}
}

// TestArcaneFeedback_EndToEndMissileLand fires a real Arcane Missile bolt via
// the ability-projectile path and lets it fly and land through the normal tick
// loop, proving the land-site wiring in landProjectileLocked (the charge-fire
// passive gate → onArcaneMissileHitLocked) actually fires the perk. Starting the
// caster at 0 mana, any positive mana after the bolt lands can only come from
// Arcane Feedback (passive regen can't reach manaPerHit over the short flight).
func TestArcaneFeedback_EndToEndMissileLand(t *testing.T) {
	amDef, ok := getAbilityDef("arcane_missiles")
	if !ok {
		t.Fatal("arcane_missiles ability def missing")
	}
	if !amDef.IsChargeFirePassive() {
		t.Fatal("arcane_missiles must be a charge-fire passive for the land gate to fire the perk")
	}
	feedback := int(perkDefByID("arcane_feedback").Config["manaPerHit"])
	if feedback <= 0 {
		t.Fatalf("arcane_feedback manaPerHit = %d; want > 0", feedback)
	}

	s := newProjectileTestState(t)
	s.mu.Lock()
	caster := spawnArchMageCasterLocked(s, "arcane_feedback")
	caster.CurrentMana = 0
	target := spawnEnemy(t, s, 360, 300)
	s.fireAbilityProjectileLocked(caster, target, amDef, EffectiveSpell{Damage: amDef.DamagePerMissile})
	if n := len(s.Projectiles); n == 0 || s.Projectiles[n-1].SourceAbilityID != "arcane_missiles" {
		t.Fatal("fired Arcane Missile bolt must carry SourceAbilityID=arcane_missiles")
	}
	s.mu.Unlock()

	advance(s, 40) // let the bolt fly and land

	s.mu.RLock()
	defer s.mu.RUnlock()
	if caster.CurrentMana < feedback {
		t.Errorf("caster mana after missile landed = %d; want >= %d (Arcane Feedback fired on land)",
			caster.CurrentMana, feedback)
	}
}

// TestUnstableMagic_NoLearnedSpellsIsNoop verifies the cast helper is inert when
// the caster has learned no pool spells (nothing to unleash, no panic).
func TestUnstableMagic_NoLearnedSpellsIsNoop(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	caster := spawnArchMageCasterLocked(s, "unstable_magic")
	caster.PoolSpellsByRank = nil
	target := spawnEnemy(t, s, 360, 300)

	before := len(s.Projectiles)
	s.fireUnstableMagicLocked(caster, target, 0.4)
	if len(s.Projectiles) != before {
		t.Errorf("Unstable Magic with no learned spells should do nothing: projectiles %d -> %d",
			before, len(s.Projectiles))
	}
}
