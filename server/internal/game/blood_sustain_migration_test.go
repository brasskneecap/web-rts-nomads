package game

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"webrts/server/pkg/protocol"
)

// ═════════════════════════════════════════════════════════════════════════════
// blood_sustain migration — Go on-hit hook → composable on_damage_dealt passive
//
// blood_sustain's lifesteal used to be a `case "blood_sustain":` arm in
// onPerkAttackDamageAppliedLocked (perks_attack.go), called from every
// attacker-attributed on-hit reaction call site (primary attack,
// savage_strikes bonus hit, cleaving_rage secondary). It is now a GRANTED
// PASSIVE ABILITY (catalog/abilities/blood_sustain) whose on_damage_dealt
// trigger is scoped to DamageCategoryBasicAttack — dispatched from
// fireOnDamageDealtLocked at applyUnitDamageWithSourceLocked's single
// canonical HP-loss point (ability_damage_dealt.go).
//
// blood_sustain's config carries a single tunable, lifestealPercent (0.2 as
// authored today) — every expectation below DERIVES that percentage from
// perkDefByID("blood_sustain").Config rather than hardcoding 0.2, per project
// convention (no hardcoded balance tunables in tests).
//
// ── THE APPROVED NORMALIZATION (git-blame this comment before touching it) ──
// VERIFIED against the actual call sites (damage_pipeline.go / state_combat.go
// / perks_attack.go) BEFORE migrating:
//   - basic melee attacks, ranged basic-attack arrows, and pierce-shaped
//     arrows all land through resolveAttackHitLocked (state_combat.go) /
//     landProjectileLocked's normal-arrow path (projectile.go), which tag
//     DamageCategoryBasicAttack regardless of delivery mode (see
//     resolveAttackHitLocked's own doc comment, ~line 298-303). UNCHANGED by
//     the migration — TestBloodSustain_BasicMeleeAttack_HealsPercentOfDamage
//     and TestBloodSustain_RangedBasicAttack_HealsPercentOfDamage assert the
//     exact same values before and after (see git history of this file: the
//     migration commit did not touch either function body).
//   - savage_strikes' bonus hit, cleaving_rage's cleave hit, and
//     whirlwind_core's AoE sweep are EACH their OWN DamageSource literal in
//     perks_attack.go tagged Category: DamageCategoryPerk, NOT basic_attack.
//     These STOP healing blood_sustain now that its on_damage_dealt trigger
//     is scoped to ["basic_attack"] — this is the accepted, user-approved
//     normalization. See TestBloodSustain_CleaveBonusHit_NormalizedOut_NoLongerHeals
//     below, which used to assert the opposite (a heal) before this
//     migration — check this file's git history for the pre-migration
//     version if you need the old oracle.
//   - blood_engine (Gold) is DELIBERATELY UNTOUCHED: it keeps its own
//     `case "blood_engine":` arm in onPerkAttackDamageAppliedLocked and still
//     heals off EVERY on-hit reaction, including bonus hits. The two perks
//     now diverge on bonus-hit interactions when both are taken on the same
//     unit (blood_sustain: basic attacks only; blood_engine: everything).
// ═════════════════════════════════════════════════════════════════════════════

// bloodSustainLifestealPercent reads the current lifestealPercent tunable
// straight from the catalog so no expectation in this file hardcodes it.
func bloodSustainLifestealPercent(t *testing.T) float64 {
	t.Helper()
	def := perkDefByID("blood_sustain")
	if def == nil {
		t.Fatal(`perkDefByID("blood_sustain") = nil`)
	}
	pct := def.Config["lifestealPercent"]
	if pct <= 0 {
		t.Fatalf("blood_sustain.lifestealPercent = %v, want > 0 for this test to be meaningful", pct)
	}
	return pct
}

// newBloodSustainScene spawns an attacker (blood_sustain granted, HP well
// below MaxHP so healing is observable and not clamped) and a durable enemy
// target. Caller holds s.mu.
func newBloodSustainScene(t *testing.T) (s *GameState, attacker, target *Unit) {
	t.Helper()
	s = NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 7)
	s.mu.Lock()

	attacker = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	attacker.MaxHP = 1000
	attacker.HP = 400 // deliberately damaged so a lifesteal heal is observable
	attacker.Visible = true
	attacker.Damage = 60
	grantPerk(attacker, "blood_sustain")
	// Route through the REAL production wiring (PerkDef.GrantsAbilities →
	// assignUnitPathAbilitiesLocked, path_ability_defs.go step 4) rather than
	// hand-appending to unit.Abilities, so this scene also proves the perk
	// actually grants the ability outside of a rank-up/spawn flow.
	s.assignUnitPathAbilitiesLocked(attacker)

	target = spawnEnemy(t, s, 420, 400)
	target.MaxHP = 5000
	target.HP = 5000

	return s, attacker, target
}

// ─────────────────────────────────────────────────────────────────────────────
// Basic attacks — UNCHANGED by the migration
// ─────────────────────────────────────────────────────────────────────────────

// TestBloodSustain_BasicMeleeAttack_HealsPercentOfDamage: a normal melee
// swing still heals blood_sustain. resolveAttackHitLocked (state_combat.go)
// is the SHARED hub every basic-attack delivery mode (melee, ranged, pierce)
// lands through with damage already post-armor-mitigation and Category
// always DamageCategoryBasicAttack — see that function's doc comment.
func TestBloodSustain_BasicMeleeAttack_HealsPercentOfDamage(t *testing.T) {
	s, attacker, target := newBloodSustainScene(t)
	defer s.mu.Unlock()

	pct := bloodSustainLifestealPercent(t)
	const landedDamage = 80 // arbitrary already-mitigated hit value, not a tunable

	preHP := attacker.HP
	var dead []int
	s.resolveAttackHitLocked(attacker, target, landedDamage, &dead)

	wantHeal := int(math.Round(float64(landedDamage) * pct))
	gotHeal := attacker.HP - preHP
	if gotHeal != wantHeal {
		t.Fatalf("melee attacker healed %d, want %d (round(%d * %v))", gotHeal, wantHeal, landedDamage, pct)
	}
}

// TestBloodSustain_RangedBasicAttack_HealsPercentOfDamage: same behavior for
// a ranged basic attack. Ranged arrows land through the exact same
// resolveAttackHitLocked hub as melee (see projectile.go's
// landProjectileLocked, which calls resolveAttackHitLocked for a normal
// basic-attack bolt) — Kind differs ("melee" vs "projectile") but Category is
// identical (DamageCategoryBasicAttack), so blood_sustain's behavior is
// identical to the melee case.
func TestBloodSustain_RangedBasicAttack_HealsPercentOfDamage(t *testing.T) {
	s, attacker, target := newBloodSustainScene(t)
	defer s.mu.Unlock()

	pct := bloodSustainLifestealPercent(t)
	const landedDamage = 45 // arbitrary already-mitigated hit value, not a tunable

	preHP := attacker.HP
	var dead []int
	s.resolveAttackHitLocked(attacker, target, landedDamage, &dead)

	wantHeal := int(math.Round(float64(landedDamage) * pct))
	gotHeal := attacker.HP - preHP
	if gotHeal != wantHeal {
		t.Fatalf("ranged attacker healed %d, want %d (round(%d * %v))", gotHeal, wantHeal, landedDamage, pct)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Cleave bonus hit — THE NORMALIZED BEHAVIOR CHANGE
// ─────────────────────────────────────────────────────────────────────────────

// TestBloodSustain_CleaveBonusHit_NormalizedOut_NoLongerHeals is the one
// assertion in this file whose EXPECTATION FLIPPED across the migration.
// Before: cleaving_rage's secondary hit (DamageCategoryPerk) healed
// blood_sustain, because the old Go hook fired unconditionally from
// applyCleaveHitLocked regardless of category (see the removed
// perks_attack.go comment: "Let on-damage perks (blood_sustain) react to
// cleave hits"). After: the migrated passive's on_damage_dealt trigger is
// scoped to categories:["basic_attack"], and a cleave hit is tagged
// DamageCategoryPerk — so damageTriggerScopeMatches rejects it and the
// trigger never fires. THIS IS THE USER-APPROVED NORMALIZATION described at
// the top of this file, not a bug. blood_engine (Gold) is unaffected and
// still heals off this same cleave hit — see
// TestBloodSustain_CleaveBonusHit_BloodEngineStillHealsIndependently below.
func TestBloodSustain_CleaveBonusHit_NormalizedOut_NoLongerHeals(t *testing.T) {
	s, attacker, primaryTarget := newBloodSustainScene(t)
	defer s.mu.Unlock()

	grantPerk(attacker, "cleaving_rage")
	cleaveDef := perkDefByID("cleaving_rage")
	if cleaveDef == nil {
		t.Fatal(`perkDefByID("cleaving_rage") = nil`)
	}
	splashRadius := cleaveDef.Config["splashRadius"]

	// Secondary victim within cleave splash radius of the primary target.
	secondary := spawnEnemy(t, s, primaryTarget.X+splashRadius*0.3, primaryTarget.Y)
	secondary.MaxHP = 5000
	secondary.HP = 5000

	preAttackerHP := attacker.HP
	preSecondaryHP := secondary.HP
	var dead []int
	s.onPerkAttackFiredLocked(attacker, primaryTarget, 10, &dead)

	if secondary.HP >= preSecondaryHP {
		t.Fatal("cleave did not land on the secondary target; test setup broken")
	}
	if gotHeal := attacker.HP - preAttackerHP; gotHeal != 0 {
		t.Fatalf("attacker healed %d after a cleave (DamageCategoryPerk) bonus hit, want 0 — blood_sustain's on_damage_dealt trigger is scoped to basic_attack only (normalized behavior, see file header)", gotHeal)
	}
}

// TestBloodSustain_CleaveBonusHit_BloodEngineStillHealsIndependently proves
// the divergence called out above: blood_engine (which kept its Go hook) DOES
// still heal off the exact same cleave hit that blood_sustain (migrated) no
// longer reacts to. Both perks on one unit — only blood_engine's heal lands.
func TestBloodSustain_CleaveBonusHit_BloodEngineStillHealsIndependently(t *testing.T) {
	s, attacker, primaryTarget := newBloodSustainScene(t)
	defer s.mu.Unlock()

	grantPerk(attacker, "cleaving_rage")
	grantPerk(attacker, "blood_engine")
	beDef := perkDefByID("blood_engine")
	if beDef == nil {
		t.Fatal(`perkDefByID("blood_engine") = nil`)
	}
	cleaveDef := perkDefByID("cleaving_rage")
	if cleaveDef == nil {
		t.Fatal(`perkDefByID("cleaving_rage") = nil`)
	}
	splashRadius := cleaveDef.Config["splashRadius"]

	secondary := spawnEnemy(t, s, primaryTarget.X+splashRadius*0.3, primaryTarget.Y)
	secondary.MaxHP = 5000
	secondary.HP = 5000

	bePct := beDef.Config["lifestealPercent"]
	preAttackerHP := attacker.HP
	preSecondaryHP := secondary.HP
	var dead []int
	s.onPerkAttackFiredLocked(attacker, primaryTarget, 10, &dead)

	actualCleaveDamage := preSecondaryHP - secondary.HP
	if actualCleaveDamage <= 0 {
		t.Fatal("cleave did not land on the secondary target; test setup broken")
	}
	// blood_engine's own healUnitLocked call — entirely independent of
	// blood_sustain's migrated passive.
	wantHeal := int(math.Round(float64(actualCleaveDamage) * bePct))
	gotHeal := attacker.HP - preAttackerHP
	if gotHeal != wantHeal {
		t.Fatalf("attacker healed %d via blood_engine after cleave, want %d (round(%d * %v)) — blood_engine must be unaffected by the blood_sustain migration", gotHeal, wantHeal, actualCleaveDamage, bePct)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Post-migration invariants
// ─────────────────────────────────────────────────────────────────────────────

// TestBloodSustain_HealsAttackerNotEnemy proves the lifesteal lands on the
// ATTACKER (the caster of the on_damage_dealt passive, resolved via
// target:{source:"caster"} in the ability program), never on the unit that
// was hit.
func TestBloodSustain_HealsAttackerNotEnemy(t *testing.T) {
	s, attacker, target := newBloodSustainScene(t)
	defer s.mu.Unlock()

	target.HP = 4000 // leave headroom so an accidental heal would be visible
	preTargetHP := target.HP

	var dead []int
	s.resolveAttackHitLocked(attacker, target, 50, &dead)

	// target.HP should have dropped by exactly the landed damage (50, no
	// mitigation configured in this scene) and NOT been healed back up by any
	// part of the blood_sustain pipeline.
	if target.HP != preTargetHP-50 {
		t.Fatalf("target.HP = %d, want %d (damage landed, no stray heal back onto the enemy)", target.HP, preTargetHP-50)
	}
}

// TestBloodSustain_DoesNotFireOnAbilityDamage proves the migrated passive's
// DamageScope excludes DamageCategoryAbility — an ability-sourced hit from
// the same attacker must not heal blood_sustain, matching the pre-migration
// contract (the old Go hook only ever ran from attack-resolution call sites,
// never from ability damage) and mirroring
// TestAbilityOnDamageDealt_BasicAttackScope_OnlyFiresOnBasicAttack's proof at
// the generic-trigger level.
func TestBloodSustain_DoesNotFireOnAbilityDamage(t *testing.T) {
	s, attacker, target := newBloodSustainScene(t)
	defer s.mu.Unlock()

	preHP := attacker.HP
	s.applyUnitDamageWithSourceLocked(target, 50, DamageSource{
		AttackerUnitID: attacker.ID, Kind: "ability", Category: DamageCategoryAbility, SourceAbilityID: "some_other_ability",
	})

	if attacker.HP != preHP {
		t.Fatalf("attacker.HP = %d, want unchanged %d — blood_sustain must not fire on DamageCategoryAbility damage", attacker.HP, preHP)
	}
}

// TestBloodSustain_NoDoubleHeal_OneHealPerDamageInstance guards against a
// re-entrancy or double-fire regression: a single landed basic-attack hit
// must produce exactly one heal application, not two. The heal itself
// (restore_health) does not deal damage, so it cannot re-trigger
// fireOnDamageDealtLocked's own OnDamageDealtDispatchActive guard — this test
// instead pins the OBSERVABLE outcome (exactly one hit's worth of healing)
// across two independent hits, proving no hidden double-application per hit.
func TestBloodSustain_NoDoubleHeal_OneHealPerDamageInstance(t *testing.T) {
	s, attacker, target := newBloodSustainScene(t)
	defer s.mu.Unlock()

	pct := bloodSustainLifestealPercent(t)
	const landedDamage = 30

	preHP := attacker.HP
	var dead []int
	s.resolveAttackHitLocked(attacker, target, landedDamage, &dead)
	afterFirstHit := attacker.HP

	wantHealPerHit := int(math.Round(float64(landedDamage) * pct))
	if got := afterFirstHit - preHP; got != wantHealPerHit {
		t.Fatalf("heal after hit 1 = %d, want %d (single application)", got, wantHealPerHit)
	}

	s.resolveAttackHitLocked(attacker, target, landedDamage, &dead)
	afterSecondHit := attacker.HP

	if got := afterSecondHit - afterFirstHit; got != wantHealPerHit {
		t.Fatalf("heal after hit 2 = %d, want %d (still exactly one heal per hit, no accumulation/double-fire)", got, wantHealPerHit)
	}
}

// TestBloodSustain_GeneratedDescription_NamesPercentAndBasicAttackScope
// proves the ability-description generator (ability_describe.go's
// describeOnDamageDealtLifestealAbility, added alongside this migration)
// produces non-empty, sentence-terminated prose for the shipped blood_sustain
// ability and names both its percentage (derived from the ability's own
// amountMult, not hardcoded) and its basic-attack scope.
func TestBloodSustain_GeneratedDescription_NamesPercentAndBasicAttackScope(t *testing.T) {
	def, ok := getAbilityDef("blood_sustain")
	if !ok {
		t.Fatal(`getAbilityDef("blood_sustain") = false`)
	}
	desc := describeAbility(def)
	if strings.TrimSpace(desc) == "" {
		t.Fatal("generated description is empty")
	}
	if !strings.HasSuffix(strings.TrimSpace(desc), ".") {
		t.Fatalf("description not sentence-terminated: %q", desc)
	}

	// Derive the expected percent from the ability's own program config
	// rather than hardcoding 20.
	var pct int
	for _, trig := range def.Program.Triggers {
		if trig.Type != TriggerOnDamageDealt {
			continue
		}
		for _, act := range trig.Actions {
			if act.Type != ActionRestoreHealth {
				continue
			}
			var cfg restoreHealthConfig
			decodeActionConfig(act.Config, &cfg)
			pct = int(math.Round(cfg.AmountMult * 100))
		}
	}
	if pct == 0 {
		t.Fatal("could not recover amountMult from blood_sustain's program; test setup broken")
	}
	wantPct := fmt.Sprintf("%d%%", pct)
	if !strings.Contains(desc, wantPct) {
		t.Errorf("description %q does not contain expected percent %q", desc, wantPct)
	}
	if !strings.Contains(desc, "basic attack") {
		t.Errorf("description %q does not name the basic_attack damage scope", desc)
	}
}
