package game

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// ═════════════════════════════════════════════════════════════════════════════
// Task 1c — CHARACTERIZATION tests for migrating hold_the_line and hawk_spirit
// off their bespoke Go handlers (perkFlatMaxHPBonusLocked in
// perks_defense.go; the hawk_spirit arms of perkAttackSpeedBonusLocked and
// perkBonusDamageMultiplierLocked in perks_attack.go) onto data-driven
// PerkDef.StatModifiers (perk_stat_modifiers.go / stat_modifiers.go).
//
// Every "want" below is derived from the perk's catalog StatModifiers value
// (via the statModifierValue test helper, perk_describe_test.go) plus a
// companion no-perk unit's own observed stat (never a hardcoded balance
// literal) and is computed WITHOUT calling the hook function(s) slated for
// deletion — so these tests still mean something (and can actually fail)
// after the Go handlers are gone. "got" is read from the live, real fold
// path the engine actually uses for that stat, so the test is checking real
// engine output, not re-deriving its own answer from the same code under
// test.
//
// Where a helper below (rawAttackDamageLocked) mirrors production code
// verbatim (state_combat.go's applyDelayedAttackLocked, the pre-crit raw
// damage calc around line 621-635), that mirroring is deliberate: it is the
// formula that determines observable combat damage, just extracted so it can
// be exercised without a full projectile/melee tick.
// ═════════════════════════════════════════════════════════════════════════════

func newStatMigrationState(t *testing.T, seed int64) *GameState {
	t.Helper()
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), seed)
	return s
}

// rawAttackDamageLocked mirrors the pre-crit raw-damage formula in
// state_combat.go's applyDelayedAttackLocked (lines ~621-635): the outgoing-
// damage-multiplier hook folded in first, then the zone-aura / perk
// stat-modifier (add, mul) pair folded on top via applyStatStages. Kept as a
// literal mirror (not a call into an extracted prod helper — none exists)
// so damage-arm characterization can run without a live projectile/melee
// tick.
func rawAttackDamageLocked(s *GameState, attacker, target *Unit) float64 {
	raw := float64(attacker.Damage) * (1.0 + s.perkBonusDamageMultiplierLocked(attacker, target))
	dmgAdd, dmgMul := s.playerStatModifierLocked(attacker.OwnerID, statDamage)
	dmgPerkStages := s.unitPerkStatModifiersLocked(attacker, statDamage)
	if dmgAdd != 0 || dmgMul != 1 || len(dmgPerkStages) > 0 {
		raw = applyStatStages(raw, mergeZoneIntoBaseStage(dmgPerkStages, dmgAdd, dmgMul))
	}
	return raw
}

// effectiveAttackSpeedLocked mirrors the documented effective-speed formula
// (progression.go / state.go call sites): unit.AttackSpeed + the perk hook's
// returned bonus. No Max(0.1, ...) clamp here — this is the raw effective
// value before any floor is applied, which is what exposes ordering/rounding
// changes most directly.
func effectiveAttackSpeedLocked(s *GameState, unit *Unit) float64 {
	return unit.AttackSpeed + s.perkAttackSpeedBonusLocked(unit)
}

// ═════════════════════════════════════════════════════════════════════════════
// A. hold_the_line — flat max-HP bonus
// ═════════════════════════════════════════════════════════════════════════════

// TestHoldTheLine_MaxHP_Unpathed: (a) baseline unpathed soldier vs (b) the
// same unit with hold_the_line. The perk must add EXACTLY the catalog's
// maxHp "add" StatModifier value on top of the no-perk baseline.
func TestHoldTheLine_MaxHP_Unpathed(t *testing.T) {
	s := newStatMigrationState(t, 1)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := requirePerkDef(t, "hold_the_line")
	bonus := int(statModifierValue(t, def, statMaxHp, statOpAdd))
	if bonus == 0 {
		t.Fatal("hold_the_line maxHp add StatModifier is 0 — perk json changed shape?")
	}

	baseline := s.spawnPlayerUnitLocked("soldier", "p1", "#ff0000", protocol.Vec2{X: 300, Y: 300})
	s.applyRankModifiersLocked(baseline, false)

	perked := s.spawnPlayerUnitLocked("soldier", "p1", "#ff0000", protocol.Vec2{X: 340, Y: 300})
	grantPerk(perked, "hold_the_line")
	s.applyRankModifiersLocked(perked, false)

	want := baseline.MaxHP + bonus
	if perked.MaxHP != want {
		t.Fatalf("unpathed MaxHP with hold_the_line = %d, want %d (baseline %d + StatModifier bonus %d)",
			perked.MaxHP, want, baseline.MaxHP, bonus)
	}
}

// TestHoldTheLine_MaxHP_PromotedRanked: (c) a promoted, ranked unit (path ×
// rank multiplier already scaling MaxHP) must still get exactly the flat
// config bonus added on top. This is what would expose the perk's
// contribution landing on the wrong side of the rank multiplier.
func TestHoldTheLine_MaxHP_PromotedRanked(t *testing.T) {
	s := newStatMigrationState(t, 2)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := requirePerkDef(t, "hold_the_line")
	bonus := int(statModifierValue(t, def, statMaxHp, statOpAdd))

	baseline := s.spawnPlayerUnitLocked("soldier", "p1", "#ff0000", protocol.Vec2{X: 300, Y: 300})
	baseline.ProgressionPath = unitPathVanguard
	baseline.Rank = unitRankGold
	s.applyRankModifiersLocked(baseline, false)

	perked := s.spawnPlayerUnitLocked("soldier", "p1", "#ff0000", protocol.Vec2{X: 340, Y: 300})
	perked.ProgressionPath = unitPathVanguard
	perked.Rank = unitRankGold
	grantPerk(perked, "hold_the_line")
	s.applyRankModifiersLocked(perked, false)

	want := baseline.MaxHP + bonus
	if perked.MaxHP != want {
		t.Fatalf("vanguard/gold MaxHP with hold_the_line = %d, want %d (path-scaled baseline %d + StatModifier bonus %d)",
			perked.MaxHP, want, baseline.MaxHP, bonus)
	}
}

// TestHoldTheLine_MaxHP_WithZoneAura: (d) an active zone-aura maxHp modifier
// (add AND multiply) stacking with the perk. "want" is built from a
// zone-aura-FREE baseline (captured on a separate, aura-less player) plus the
// config bonus, folded through applyStatStages/mergeZoneIntoBaseStage
// directly — the shared engine this migration does NOT touch — reproducing
// exactly the fold progression.go performs. If hold_the_line's contribution
// ends up on the wrong side of the zone multiplier post-migration, this is
// the test that catches it.
func TestHoldTheLine_MaxHP_WithZoneAura(t *testing.T) {
	s := newStatMigrationState(t, 3)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := requirePerkDef(t, "hold_the_line")
	bonus := int(statModifierValue(t, def, statMaxHp, statOpAdd))

	// p0: no zone aura, used only to capture the aura-free baseline.
	baseline := s.spawnPlayerUnitLocked("soldier", "p0", "#00ff00", protocol.Vec2{X: 200, Y: 300})
	s.applyRankModifiersLocked(baseline, false)

	// p1: zone-aura-affected player, owns the perked unit.
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	const zoneAdd = 20.0
	const zoneMul = 1.1
	s.Players["p1"].ZoneStatModifiers = PlayerStatModifierSet{
		statMaxHp: statAccum{Add: zoneAdd, Mul: zoneMul},
	}
	perked := s.spawnPlayerUnitLocked("soldier", "p1", "#ff0000", protocol.Vec2{X: 340, Y: 300})
	grantPerk(perked, "hold_the_line")
	s.applyRankModifiersLocked(perked, false)

	wantF := applyStatStages(float64(baseline.MaxHP+bonus), map[string]statStageAccum{
		statStageBase: {Add: zoneAdd, Mul: zoneMul},
	})
	want := maxInt(1, int(math.Round(wantF)))
	if perked.MaxHP != want {
		t.Fatalf("MaxHP with hold_the_line + zone aura = %d, want %d (aura-free baseline %d + bonus %d, then ×(add %v, mul %v))",
			perked.MaxHP, want, baseline.MaxHP, bonus, zoneAdd, zoneMul)
	}
}

// ═════════════════════════════════════════════════════════════════════════════
// B. hawk_spirit — attack-speed bonus + damage multiplier
// ═════════════════════════════════════════════════════════════════════════════

func newHawkSpiritCombatPair(t *testing.T, seed int64) (s *GameState, attacker, target *Unit) {
	t.Helper()
	s = NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), seed)
	s.mu.Lock()
	defer s.mu.Unlock()
	attacker = s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	target = s.spawnPlayerUnitLocked("archer", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 600, Y: 400})
	return s, attacker, target
}

// TestHawkSpirit_Unpathed: (a)/(b) baseline vs with-perk, unpathed. Attack
// speed bonus must add exactly config's attackSpeedBonus; damage must scale
// by exactly (1 + config's damageMultiplier).
func TestHawkSpirit_Unpathed(t *testing.T) {
	s, attacker, target := newHawkSpiritCombatPair(t, 10)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := requirePerkDef(t, "hawk_spirit")
	asBonus := statModifierValue(t, def, statAttackSpeed, statOpAdd)
	// Representation note: the StatModifier's multiply entry IS the factor
	// itself (1.15), unlike the old (deleted) damageMultiplier config key
	// which was a bonus fraction (0.15) meant to be used as (1 + value).
	dmgMult := statModifierValue(t, def, statDamage, statOpMultiply)
	if asBonus == 0 || dmgMult == 0 {
		t.Fatal("hawk_spirit StatModifier values are 0 — perk json changed shape?")
	}

	baselineAS := effectiveAttackSpeedLocked(s, attacker)
	baselineDamage := rawAttackDamageLocked(s, attacker, target)

	grantPerk(attacker, "hawk_spirit")

	wantAS := baselineAS + asBonus
	if got := effectiveAttackSpeedLocked(s, attacker); got != wantAS {
		t.Errorf("effective attack speed = %v, want %v (baseline %v + StatModifier add %v)", got, wantAS, baselineAS, asBonus)
	}

	wantDamage := baselineDamage * dmgMult
	if got := rawAttackDamageLocked(s, attacker, target); got != wantDamage {
		t.Errorf("raw attack damage = %v, want %v (baseline %v × StatModifier mult %v)", got, wantDamage, baselineDamage, dmgMult)
	}
}

// TestHawkSpirit_PromotedRanked: (c) a promoted/ranked archer (marksman/gold)
// so rank multipliers are already scaling AttackSpeed and Damage. The flat
// AS bonus and the damage multiplier must still land as documented, exposing
// any change relative to where rank multipliers apply.
func TestHawkSpirit_PromotedRanked(t *testing.T) {
	s, attacker, target := newHawkSpiritCombatPair(t, 11)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := requirePerkDef(t, "hawk_spirit")
	asBonus := statModifierValue(t, def, statAttackSpeed, statOpAdd)
	dmgMult := statModifierValue(t, def, statDamage, statOpMultiply)

	attacker.ProgressionPath = unitPathMarksman
	attacker.Rank = unitRankGold
	s.applyRankModifiersLocked(attacker, false)
	target.ProgressionPath = unitPathMarksman
	target.Rank = unitRankGold
	s.applyRankModifiersLocked(target, false)

	baselineAS := effectiveAttackSpeedLocked(s, attacker)
	baselineDamage := rawAttackDamageLocked(s, attacker, target)

	grantPerk(attacker, "hawk_spirit")

	wantAS := baselineAS + asBonus
	if got := effectiveAttackSpeedLocked(s, attacker); got != wantAS {
		t.Errorf("marksman/gold effective attack speed = %v, want %v (baseline %v + StatModifier add %v)", got, wantAS, baselineAS, asBonus)
	}

	wantDamage := baselineDamage * dmgMult
	if got := rawAttackDamageLocked(s, attacker, target); got != wantDamage {
		t.Errorf("marksman/gold raw attack damage = %v, want %v (baseline %v × StatModifier mult %v)", got, wantDamage, baselineDamage, dmgMult)
	}
}

// TestHawkSpirit_WithZoneAura_AttackSpeed: (d) an active zone-aura attackSpeed
// modifier (add AND multiply) stacking with hawk_spirit. "want" reproduces
// the documented pre-migration formula from perks_attack.go
// (perkAttackSpeedBonusLocked's doc comment): today hawk_spirit's flat bonus
// is summed into perkAttackSpeedBonusLocked's `total`, which sits INSIDE the
// (value + zoneAdd) × zoneMul fold alongside unit.AttackSpeed itself — so the
// zone multiplier scales hawk's bonus too. Computed without calling
// perkAttackSpeedBonusLocked so the assertion stays meaningful after
// hawk_spirit's case is deleted from it.
func TestHawkSpirit_WithZoneAura_AttackSpeed(t *testing.T) {
	s, attacker, _ := newHawkSpiritCombatPair(t, 12)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := requirePerkDef(t, "hawk_spirit")
	asBonus := statModifierValue(t, def, statAttackSpeed, statOpAdd)

	baselineAS := attacker.AttackSpeed // no perk yet, no other AS contributor

	const zoneAdd = 0.05
	const zoneMul = 1.2
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players["p1"].ZoneStatModifiers = PlayerStatModifierSet{
		statAttackSpeed: statAccum{Add: zoneAdd, Mul: zoneMul},
	}

	grantPerk(attacker, "hawk_spirit")

	wantEffective := (baselineAS + asBonus + zoneAdd) * zoneMul
	if got := effectiveAttackSpeedLocked(s, attacker); got != wantEffective {
		t.Errorf("effective attack speed with zone aura = %v, want %v (base %v + perk %v + zoneAdd %v, ×zoneMul %v)",
			got, wantEffective, baselineAS, asBonus, zoneAdd, zoneMul)
	}
}

// TestHawkSpirit_WithZoneAura_Damage: (d) an active zone-aura damage modifier
// (add AND multiply) stacking with hawk_spirit's damage multiplier. "want"
// reproduces the documented pre-migration formula from state_combat.go: the
// outgoing-damage-multiplier hook (which includes hawk_spirit's
// damageMultiplier) is applied to raw damage FIRST, THEN the zone-aura
// (add, mul) pair folds on top — so the zone's additive term is NOT itself
// scaled by hawk's multiplier. This is the case flagged as the highest-risk
// ordering change in the task brief: moving hawk_spirit's multiplier onto
// the SAME merged (add, mul) stage as the zone aura would make the zone
// add ALSO get scaled by hawk's multiplier, which the pre-migration formula
// never does.
func TestHawkSpirit_WithZoneAura_Damage(t *testing.T) {
	s, attacker, target := newHawkSpiritCombatPair(t, 13)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := requirePerkDef(t, "hawk_spirit")
	dmgMult := statModifierValue(t, def, statDamage, statOpMultiply)

	baselineDamage := float64(attacker.Damage) // no perk yet, no other damage-mult contributor

	const zoneAdd = 8.0
	const zoneMul = 1.2
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players["p1"].ZoneStatModifiers = PlayerStatModifierSet{
		statDamage: statAccum{Add: zoneAdd, Mul: zoneMul},
	}

	grantPerk(attacker, "hawk_spirit")

	wantDamage := (baselineDamage*dmgMult + zoneAdd) * zoneMul
	if got := rawAttackDamageLocked(s, attacker, target); got != wantDamage {
		t.Errorf("raw attack damage with zone aura = %v, want %v (base %v × mult %v folded with zoneAdd %v, ×zoneMul %v)",
			got, wantDamage, baselineDamage, dmgMult, zoneAdd, zoneMul)
	}
}

// ═════════════════════════════════════════════════════════════════════════════
// C. vulture_spirit — crit-chance bonus + damage multiplier (task 1e)
//
// vulture_spirit's damage arm shares the SAME hawk_spirit blocker (see the
// package doc above rawAttackDamageLocked / the case comment this task
// deletes from perkBonusDamageMultiplierLocked): it must migrate onto the
// new "intrinsic" stage (statStageIntrinsic, stat_modifiers.go), not "base"
// or "final" — both of those apply strictly after a base-stage add has
// already been folded in, which would let the multiplier scale a zone aura's
// additive damage bonus (it must not).
//
// Every "want" below is derived from the perk's catalog StatModifiers value,
// never a hardcoded balance literal, and is computed WITHOUT calling the hook
// function(s) slated for deletion (perkBonusDamageMultiplierLocked's
// "vulture_spirit" arm, perkCritChanceBonusLocked's "vulture_spirit" arm) —
// so these tests still mean something (and can actually fail) after those
// arms are gone.
// ═════════════════════════════════════════════════════════════════════════════

// TestVultureSpirit_Unpathed: (a)/(b) baseline vs with-perk, unpathed. Crit
// chance must add exactly config's critChanceBonus on top of defaultCritChance;
// damage must scale by exactly (1 + config's damageMultiplier).
func TestVultureSpirit_Unpathed(t *testing.T) {
	s, attacker, target := newHawkSpiritCombatPair(t, 20)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := requirePerkDef(t, "vulture_spirit")
	critBonus := statModifierValue(t, def, statCritChance, statOpAdd)
	// Representation note: the StatModifier's multiply entry IS the factor
	// itself (1.1), unlike the old (deleted) damageMultiplier config key
	// which was a bonus fraction (0.1) meant to be used as (1 + value).
	dmgMult := statModifierValue(t, def, statDamage, statOpMultiply)
	if critBonus == 0 || dmgMult == 0 {
		t.Fatal("vulture_spirit StatModifier values are 0 — perk json changed shape?")
	}

	baselineCrit := s.unitCritChanceLocked(attacker, nil)
	baselineDamage := rawAttackDamageLocked(s, attacker, target)

	grantPerk(attacker, "vulture_spirit")

	wantCrit := baselineCrit + critBonus
	if got := s.unitCritChanceLocked(attacker, nil); got != wantCrit {
		t.Errorf("crit chance = %v, want %v (baseline %v + StatModifier add %v)", got, wantCrit, baselineCrit, critBonus)
	}

	wantDamage := baselineDamage * dmgMult
	if got := rawAttackDamageLocked(s, attacker, target); got != wantDamage {
		t.Errorf("raw attack damage = %v, want %v (baseline %v × StatModifier mult %v)", got, wantDamage, baselineDamage, dmgMult)
	}
}

// TestVultureSpirit_PromotedRanked: (c) a promoted, ranked archer
// (marksman/gold) so rank multipliers are already scaling Damage. The crit
// bonus and damage multiplier must still land as documented.
func TestVultureSpirit_PromotedRanked(t *testing.T) {
	s, attacker, target := newHawkSpiritCombatPair(t, 21)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := requirePerkDef(t, "vulture_spirit")
	critBonus := statModifierValue(t, def, statCritChance, statOpAdd)
	dmgMult := statModifierValue(t, def, statDamage, statOpMultiply)

	attacker.ProgressionPath = unitPathMarksman
	attacker.Rank = unitRankGold
	s.applyRankModifiersLocked(attacker, false)
	target.ProgressionPath = unitPathMarksman
	target.Rank = unitRankGold
	s.applyRankModifiersLocked(target, false)

	baselineCrit := s.unitCritChanceLocked(attacker, nil)
	baselineDamage := rawAttackDamageLocked(s, attacker, target)

	grantPerk(attacker, "vulture_spirit")

	wantCrit := baselineCrit + critBonus
	if got := s.unitCritChanceLocked(attacker, nil); got != wantCrit {
		t.Errorf("marksman/gold crit chance = %v, want %v (baseline %v + StatModifier add %v)", got, wantCrit, baselineCrit, critBonus)
	}

	wantDamage := baselineDamage * dmgMult
	if got := rawAttackDamageLocked(s, attacker, target); got != wantDamage {
		t.Errorf("marksman/gold raw attack damage = %v, want %v (baseline %v × StatModifier mult %v)", got, wantDamage, baselineDamage, dmgMult)
	}
}

// TestVultureSpirit_WithZoneAura_CritChance: (d) an active zone-aura
// critChance modifier (add AND multiply) stacking with vulture_spirit. "want"
// reproduces the documented pre-migration formula from perkCritChanceBonusLocked's
// doc comment: the perk bonus is composed against the defaultCritChance
// baseline the same way the zone aura is, i.e. total crit chance is
// (defaultCritChance + perkBonus + zoneAdd) × zoneMul. Computed without
// calling perkCritChanceBonusLocked so the assertion stays meaningful after
// vulture_spirit's case is deleted from it.
func TestVultureSpirit_WithZoneAura_CritChance(t *testing.T) {
	s, attacker, _ := newHawkSpiritCombatPair(t, 22)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := requirePerkDef(t, "vulture_spirit")
	critBonus := statModifierValue(t, def, statCritChance, statOpAdd)

	const zoneAdd = 0.05
	const zoneMul = 1.2
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players["p1"].ZoneStatModifiers = PlayerStatModifierSet{
		statCritChance: statAccum{Add: zoneAdd, Mul: zoneMul},
	}

	grantPerk(attacker, "vulture_spirit")

	wantChance := (defaultCritChance + critBonus + zoneAdd) * zoneMul
	if got := s.unitCritChanceLocked(attacker, nil); got != wantChance {
		t.Errorf("crit chance with zone aura = %v, want %v (default %v + perk %v + zoneAdd %v, ×zoneMul %v)",
			got, wantChance, defaultCritChance, critBonus, zoneAdd, zoneMul)
	}
}

// TestVultureSpirit_WithZoneAura_Damage: (d) an active zone-aura damage
// modifier (add AND multiply) stacking with vulture_spirit's damage
// multiplier. Same shape as TestHawkSpirit_WithZoneAura_Damage — the
// multiplier must scale ONLY the unit's own base damage, never the zone
// aura's additive term.
func TestVultureSpirit_WithZoneAura_Damage(t *testing.T) {
	s, attacker, target := newHawkSpiritCombatPair(t, 23)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := requirePerkDef(t, "vulture_spirit")
	dmgMult := statModifierValue(t, def, statDamage, statOpMultiply)

	baselineDamage := float64(attacker.Damage) // no perk yet, no other damage-mult contributor

	const zoneAdd = 8.0
	const zoneMul = 1.2
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players["p1"].ZoneStatModifiers = PlayerStatModifierSet{
		statDamage: statAccum{Add: zoneAdd, Mul: zoneMul},
	}

	grantPerk(attacker, "vulture_spirit")

	wantDamage := (baselineDamage*dmgMult + zoneAdd) * zoneMul
	if got := rawAttackDamageLocked(s, attacker, target); got != wantDamage {
		t.Errorf("raw attack damage with zone aura = %v, want %v (base %v × mult %v folded with zoneAdd %v, ×zoneMul %v)",
			got, wantDamage, baselineDamage, dmgMult, zoneAdd, zoneMul)
	}
}
