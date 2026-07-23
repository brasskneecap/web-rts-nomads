package game

import (
	"fmt"
	"math"
	"sort"

	"webrts/server/pkg/protocol"
)

// ═════════════════════════════════════════════════════════════════════════════
// SHARED STAT-MODIFIER VOCABULARY
//
// This file defines the single, system-agnostic stat-modifier language that
// zone auras (and, in future, campaign modifiers, equipment, and global events)
// all speak. It is deliberately LAYERED OVER the existing per-stat read sites
// (effectiveArmorLocked, perkAttackSpeedBonusLocked, applyRankModifiersLocked,
// …) rather than replacing them: a contributor emits protocol.StatModifiers,
// those are aggregated per player into a PlayerStatModifierSet, and each
// existing read site folds in ONE extra (add, mul) term resolved from that set.
//
// Stacking rule, applied per stat at the read site:
//
//	effective = (base + Σ add) × Π multiply
//
// All functions here are pure or read player state; the resolver is called from
// hot-path read sites and must stay O(1).
// ═════════════════════════════════════════════════════════════════════════════

// Stat operation sentinels (mirror of the strings authored in map JSON).
const (
	statOpAdd      = "add"
	statOpMultiply = "multiply"
	// statOpAmplify scales a value's DISTANCE FROM 1.0 rather than the value
	// itself: result = 1 - (1 - value) x factor. It exists for INVERSE-SENSE
	// quantities, where a lower number is a stronger effect — a slow multiplier
	// of 0.35 ("slowed to 35% speed") is stronger than 0.7, so "make this slow
	// 35% stronger" is NOT value x 1.35 (that would WEAKEN it to 0.4725); it is
	// amplifying the 0.65 reduction to 0.8775, giving 0.1225.
	//
	// This is not a new idea — amplifySlow (perks_trapper.go) has always done
	// exactly this for the Trapper's amplified_effects perk. The op makes that
	// math expressible as DATA so an inverse-sense ability parameter can be
	// amplified by a perk/item/advancement like any other.
	//
	// Composition is a product of the factors (1 - (1-b)*f1*f2), so it is
	// order-independent — which the deterministic fold requires.
	statOpAmplify = "amplify"
)

// Stat-modifier STAGES — the evaluation order a stat's accumulated
// modifiers fold through (applyStatStages below).
//
//   - statStageIntrinsic — applied FIRST, strictly BEFORE any base-stage
//     additive bonus (zone auras, base-stage perk modifiers) is folded in.
//     Intended for perks that scale the unit's OWN base stat rather than
//     competing with external additive bonuses — e.g. hawk_spirit's damage
//     multiplier must scale attacker.Damage but must NOT scale a zone aura's
//     flat damage add. Authoring a modifier at this stage with only a
//     multiply (no add) collapses the stage to exactly `value = base × Πmul`
//     because nothing has been added to the pool yet — see the worked
//     example on applyStatStages.
//   - statStageBase  — the default (an empty authored Stage means base). Folds
//     into the SAME (base + Σadd) × Πmul pool zone auras already use
//     (playerStatModifierLocked) — base-stage perk modifiers and zone auras
//     are one pool, not two competing ones.
//   - statStageFinal — applied strictly AFTER every base-stage contribution
//     has already been folded in, for modifiers meant to scale the
//     fully-computed stat rather than compete with base bonuses (e.g. "then
//     double it").
const (
	statStageIntrinsic = "intrinsic" // scales the unit's OWN base stat, before any external additive bonus
	statStageBase      = "base"
	statStageFinal     = "final"
)

// statStages is the ordered stage list applyStatStages folds through.
// DETERMINISM: this fixed authored order — never a map's iteration order —
// is what makes "final" reliably mean "after everything else," and
// "intrinsic" reliably mean "before everything else."
var statStages = []string{statStageIntrinsic, statStageBase, statStageFinal}

// Canonical stat identifiers. Adding a new stat is: (1) a const here, (2) an
// entry in statRegistry, (3) one read-site wire-up where the stat is consumed.
// Nothing in the aura code or the aggregation needs to change.
const (
	// Combat — these all have existing read sites.
	statHealthRegen = "healthRegen"
	statManaRegen   = "manaRegen"
	statMoveSpeed   = "moveSpeed"
	statAttackSpeed = "attackSpeed"
	statDamage      = "damage"
	statArmor       = "armor"
	statMaxHp       = "maxHp"
	statMaxMana     = "maxMana"
	statAttackRange = "attackRange"
	statCritChance  = "critChance"
	statCritMult    = "critMultiplier"
	// statLifesteal is the fraction of damage-dealt an attacker heals for on
	// every hit (0.1 = "heal 10% of the damage you deal"). A base-authorable
	// stat with no typed field — a unit type carries a base lifesteal via
	// UnitDef.BaseStats and perks/statuses/auras add to it; consumed at the
	// canonical HP-loss point (applyLifestealLocked, lifesteal.go). The first
	// genuinely NEW base-authorable stat, proving the vocabulary extends to
	// derived combat properties.
	statLifesteal = "lifesteal"
	// statThorns is the fraction of an ATTACK's damage the DEFENDER reflects
	// back at the attacker (0.25 = "reflect 25% of melee/attack damage taken").
	// The defender-side twin of lifesteal — a base-authorable stat consumed at
	// the attack-hit reaction hook (applyThornsLocked, thorns.go), the stat form
	// of the retaliation perk.
	statThorns = "thorns"

	// Economy / workers — these get NEW read sites (gather/production/construction).
	statGoldGatherRate            = "goldGatherRate"
	statWoodGatherRate            = "woodGatherRate"
	statGatherSpeed               = "gatherSpeed"
	statWorkerMoveSpeed           = "workerMoveSpeed"
	statUnitProductionSpeed       = "unitProductionSpeed"
	statBuildingConstructionSpeed = "buildingConstructionSpeed"

	// Aura-only reduction stats — read directly from unitAuraStatContributionLocked
	// at a bespoke fold site rather than through applyStatStages (see the
	// stat's own doc note below for why).
	statProjectileDamageReduction = "projectileDamageReduction"

	// Aura-only bonus stats — same category as statProjectileDamageReduction
	// above (read directly from unitAuraStatContributionLocked, no
	// unitPerkStatModifiersLocked/applyStatStages fold site exists for
	// either). statArmorPercent is guardian_aura's percent-armor dimension;
	// see its doc note below for why it needs its own id rather than reusing
	// statArmor (which already has a DIFFERENT, unrelated generic fold site
	// at effectiveArmorLocked's flat-armor position).
	statArmorPercent = "armorPercent"

	// statHealingReceived is a multiplier on every heal amount a unit
	// receives (base 1.0 = "100% of the authored heal lands"; e.g. a
	// multiply of 0.7 authored on an active AbilityStatus = "take 70% of
	// incoming healing"). Unlike statArmor/statDamage/etc. this stat has no
	// per-unit base field at all — the fold site (healUnitLocked) computes
	// `applyStatStages(1.0, ...)` against the SAME fixed identity baseline
	// gatherSpeed/unitProductionSpeed use, never a value read off the unit.
	// Introduced as the pilot consumer of unitStatusStatModifiersLocked
	// (perk_stat_modifiers.go) — an active AbilityStatus's
	// StatModifiers{Stat:"healingReceived"} entry is the data-driven
	// replacement for the bespoke UnitPerkState.MarkOfWeaknessHealingReceived-
	// Mult field (perks_siphoner.go). Has a real top-level fold site (unlike
	// statProjectileDamageReduction/statArmorPercent above), so it is NOT
	// AuraOnly — a status is a different emitter from an aura, and
	// "healingReceived" makes just as much sense authored directly as a
	// status's own StatModifiers entry as it would inside a PerkAura.
	statHealingReceived = "healingReceived"

	// statAbilityDamage is a PERCENTAGE amplifier — the multiplicative half of
	// the pair it forms with statAbilityPower below. It scales an ability's
	// damage by whatever that damage already is (including any ability-power
	// contribution), so "+20%" is worth more on a big nuke than a small tick.
	// Contrast abilityPower, which adds a FIXED amount normalized by a per-action
	// ratio. Labelled "Ability Damage %" so the two are never confused in a
	// picker that lists them adjacently.
	//
	// statAbilityDamage is a multiplier on the damage every ABILITY this unit
	// casts deals (base 1.0 = "abilities deal their authored damage"; +0.15 =
	// "+15% ability damage"). It is the unit-level "my spells hit harder" axis,
	// deliberately a STAT rather than a per-ability parameter so it composes
	// through the one stat chokepoint every other source already uses: a rank,
	// an ITEM, an ADVANCEMENT, a perk, a status or a zone aura all raise it the
	// same way with no per-ability authoring
	// (docs/design/ability_perk_interaction.md D3).
	//
	// Fixed-1.0 baseline like healingReceived/gatherSpeed, so an `add` of 0.15
	// is unambiguously +15 percentage points ⇒ IsFraction. Base-authorable, so
	// a unit type can carry its own baseline via UnitDef.baseStats the way
	// critChance/lifesteal/thorns do.
	//
	// Folded at effectiveAbilityDamageLocked (spell_modifier.go), the single
	// seam a composable deal_damage action's amount already passes through.
	// NOTE: deal_damage's amountRef path deliberately bypasses that seam (it
	// applies a referenced scalar RAW — see dealDamageConfig), so ability
	// damage derived from a context scalar is not scaled by this stat.
	statAbilityDamage = "abilityDamage"
	// statAbilityPower is a FLAT pool a unit contributes to its abilities'
	// magnitudes. It is not a multiplier: an ability opts in per damage/heal
	// action with a RATIO (dealDamageConfig.APRatio), and contributes
	// abilityPower x ratio to that action's amount.
	//
	// The ratio is what makes a DoT and a burst nuke comparable. A flat "+10
	// ability damage" is wildly stronger on a 8-tick burn than on a one-shot
	// hit; a burn authoring apRatio 0.125 per tick and a nuke authoring 1.0 both
	// gain the SAME total from one point of ability power. Ratios above 1 are
	// legitimate (a long-cooldown ultimate); the engine imposes no ceiling —
	// 0..1 is a balance convention, not a constraint.
	statAbilityPower = "abilityPower"

	// statDamageTaken multiplies every point of damage a unit RECEIVES (base
	// 1.0 = "take normal damage"; +0.2 = "take 20% more"). It is the
	// data-driven form of the Trapper marker trap's "marked enemies take bonus
	// damage from all sources", and generalizes to any buff/debuff that makes a
	// unit more or less fragile — a status, an aura or a perk raises it the same
	// way, with no new fold site.
	//
	// Fixed-1.0 baseline like healingReceived/abilityDamage, so an `add` of 0.2
	// is unambiguously +20 percentage points ⇒ IsFraction. Folded at the
	// incoming-damage amplification step (perks_defense.go), the same position
	// the legacy hand-rolled mark multiplier occupies.
	statDamageTaken = "damageTaken"

	// statDamageDealt is the ATTACKER-side mirror of statDamageTaken: it
	// multiplies every point of damage a unit DEALS (base 1.0 = "deal normal
	// damage"; an `add` of -0.2 = "deal 20% less"). It is the data-driven form
	// of an outgoing-damage debuff (Trapper's exposed_weakness marks enemies to
	// deal less) and generalizes to any status/aura/perk that makes a unit hit
	// harder or softer, with no new fold site.
	//
	// Folded at the ONE point every outgoing damage instance funnels through —
	// applyUnitDamageWithSourceLocked, resolving the attacker via
	// src.AttackerUnitID — so it covers basic attacks, abilities, procs, and
	// traps alike (the true mirror of damageTaken, which folds on the target at
	// the same chokepoint). This is what makes it stronger than a change_stat on
	// the `damage` stat, which is base attack damage only and would not touch
	// the victim's ability damage.
	//
	// Fixed-1.0 baseline ⇒ IsFraction. AllowMultiply for symmetry, though the
	// shipped exposed_weakness authoring expresses the debuff as `add -0.2`.
	statDamageDealt = "damageDealt"
)

// statDef describes a registered stat: its id, the human label the editor and
// HUD show, whether a multiply operation is meaningful for it, whether an
// "add" delta should render as a percentage, and whether the stat only has a
// meaning as an aura contribution.
//
// AllowMultiply is advisory metadata for the editor/UI today (both operations
// are accepted by validation); it documents intent and drives sensible editor
// defaults.
//
// IsFraction is true when the stat's VALUE is itself a dimensionless 0-1-ish
// fraction (a probability, or a ratio measured against a fixed,
// context-independent baseline of 1.0) — so an authored "add" of 0.1 always
// means "+10 percentage points" and the generator (describeStatModifierClause)
// renders it as a percentage. It is false when the stat is a raw rate/value
// with a PER-UNIT base that varies (attackSpeed, moveSpeed, damage, armor,
// maxHp, healthRegen, manaRegen, attackRange, critMultiplier, and the
// per-unit-type gather amounts) — there an "add" delta must render as a bare
// number, because the percentage effect depends on which unit's base it lands
// on and rendering it as a % would be a guess dressed up as a fact (this is
// the exact class of bug this field exists to prevent — see hawk_spirit).
// Conservative default: false. See stat_modifiers.go / perk_describe.go task
// notes for the per-stat read-site evidence behind each determination below.
//
// AuraOnly is true when the stat has NO top-level fold site at all — nothing
// in unitPerkStatModifiersLocked's caller list (mana.go, perks_defense.go,
// perks_attack.go, perks_movement.go, progression.go, state.go,
// state_combat.go) ever resolves this stat for the OWNING unit's own
// PerkDef.StatModifiers. The stat is consumed EXCLUSIVELY via the aura cache
// (unitAuraStatContributionLocked, perk_aura_stat_cache.go) at a bespoke,
// aura-specific read site — see each AuraOnly stat's own doc note above its
// registry entry for the exact fold site. A top-level (self)
// PerkDef.StatModifiers entry naming an AuraOnly stat would be silently
// inert (the value is computed and stored in the per-stage pool but nothing
// ever reads that pool for this stat), so validatePerkDef REJECTS it there —
// same "no inert authorable fields" rule that rejects PerCompanion on a
// top-level entry. The IDENTICAL stat inside a PerkAura.StatModifiers entry
// is its valid, intended home and remains accepted. False for every stat
// that already has a real top-level fold site.
type statDef struct {
	ID            string
	Label         string
	AllowMultiply bool
	IsFraction    bool
	AuraOnly      bool
}

// statRegistry is the single source of truth for which stats exist. Ordered for
// deterministic iteration and stable editor/UI lists. Keep combat then economy.
//
// IsFraction determinations (each verified against its read site, not
// guessed):
//   - healthRegen/manaRegen: raw HP or mana per second; base is
//     unit.HealthRegenPerSecond / unit.ManaRegenPerSecond, which varies per
//     unit. False.
//   - moveSpeed/attackSpeed: raw rate; base is unit.MoveSpeed /
//     unit.AttackSpeed, which varies per unit (this is the hawk_spirit bug:
//     a +0.3 add on a 1.5 base archer is +20%, not +30%). False.
//   - damage/armor/maxHp/maxMana/attackRange: raw amounts; base is a
//     per-unit(-type) field. False.
//   - critChance: a true 0-1 probability (defaultCritChance = 0.05); an add
//     of 0.1 unambiguously means "+10 percentage points of chance to crit"
//     regardless of the unit. True — this is vulture_spirit's case.
//   - critMultiplier: a raw multiplier around a fixed-but->1 baseline
//     (defaultCritMultiplier = 2.0, i.e. "2x", not a 0-1 fraction); an add
//     delta's percentage effect depends on which baseline it lands on
//     (bullseye's override is 2.5). False.
//   - goldGatherRate/woodGatherRate: raw per-haul resource amount; base is
//     def.GoldGatherAmount/WoodGatherAmount, which varies per unit type.
//     False.
//   - gatherSpeed/unitProductionSpeed/buildingConstructionSpeed: NOT a
//     per-unit field at all — the read sites compute
//     `speed := (1 + add) × mul` against a hardcoded identity baseline of
//     1.0 ("100% speed"), so an add of 0.1 always means "+10% speed" with no
//     unit-dependent ambiguity. True.
//   - workerMoveSpeed: folds into the SAME add/mul pool as moveSpeed
//     (perks_movement.go) and is applied against unit.MoveSpeed, a raw
//     per-unit base — same reasoning as moveSpeed. False.
//   - projectileDamageReduction: the VALUE itself is a 0-1 fraction of
//     incoming projectile damage to negate (sanctuary's 0.25 = "25% less
//     projectile damage") — not a delta against any per-unit base at all.
//     The fold site (perks_defense.go) reads it as
//     multiplier = 1.0 - value directly; there is no base stat it's added
//     to, so "is an add delta a percentage" is unambiguously yes. True.
//   - armorPercent: the VALUE itself is a 0-1 fraction of the recipient's
//     base armor (guardian_aura's 0.20 = "+20% of base armor") — same
//     "no per-unit base to be ambiguous against" reasoning as
//     projectileDamageReduction. The fold site (effectiveArmorLocked) reads
//     it via unitAuraStatContributionLocked and adds it straight into its
//     own percentBonus accumulator (core = armor × (1+percentBonus) + flat),
//     never through applyStatStages — same "aura-only, no generic top-level
//     fold site" category as projectileDamageReduction; a top-level
//     PerkDef.StatModifiers{Stat:"armorPercent"} entry would be silently
//     inert today for the identical, already-accepted reason
//     projectileDamageReduction's would be. True.
//   - healingReceived: NOT a per-unit field — healUnitLocked's fold site
//     computes `applyStatStages(1.0, ...)` against a fixed identity baseline
//     of 1.0 ("100% of the heal lands"), the SAME shape gatherSpeed/
//     unitProductionSpeed/buildingConstructionSpeed use (not critMultiplier,
//     whose baseline VARIES per ability — bullseye overrides it to 2.5).
//     healingReceived's baseline never varies: it is always exactly 1.0 by
//     definition, so an add delta's percentage meaning is unambiguous
//     regardless of which unit/status authored it. True. AllowMultiply is
//     true because the pilot authoring (mark_of_weakness's migration) always
//     expresses the debuff as a multiply (0.7 = "70% healing received");
//     add is still permitted (e.g. "+10 percentage points") for symmetry
//     with every other AllowMultiply stat, but multiply is the expected
//     idiom here.
var statRegistry = []statDef{
	{statHealthRegen, "Health Regen", true, false, false},
	{statManaRegen, "Mana Regen", true, false, false},
	{statMoveSpeed, "Move Speed", true, false, false},
	{statAttackSpeed, "Attack Speed", true, false, false},
	{statDamage, "Damage", true, false, false},
	{statArmor, "Armor", true, false, false},
	{statMaxHp, "Max Health", true, false, false},
	{statMaxMana, "Max Mana", true, false, false},
	{statAttackRange, "Attack Range", true, false, false},
	{statCritChance, "Crit Chance", true, true, false},
	{statCritMult, "Crit Multiplier", true, false, false},
	// Lifesteal: a 0-1 fraction of damage dealt healed back. IsFraction (an
	// "add 0.1" is unambiguously +10 percentage points), not AuraOnly (it has a
	// real read site, applyLifestealLocked).
	{statLifesteal, "Lifesteal", true, true, false},
	// Thorns: a 0-1 fraction of attack damage reflected to the attacker. Same
	// IsFraction/AuraOnly shape as lifesteal (read site: applyThornsLocked).
	{statThorns, "Thorns", true, true, false},
	{statGoldGatherRate, "Gold Gather Rate", true, false, false},
	{statWoodGatherRate, "Wood Gather Rate", true, false, false},
	{statGatherSpeed, "Gather Speed", true, true, false},
	{statWorkerMoveSpeed, "Worker Move Speed", true, false, false},
	{statUnitProductionSpeed, "Unit Production Speed", true, true, false},
	{statBuildingConstructionSpeed, "Building Construction Speed", true, true, false},
	{statProjectileDamageReduction, "Projectile Damage Reduction", false, true, true},
	{statArmorPercent, "Percent Armor", false, true, true},
	{statHealingReceived, "Healing Received", true, true, false},
	// Ability Damage: fixed-1.0-baseline multiplier on ability damage, so an
	// `add` is a percentage-point amount (IsFraction). Real top-level fold site
	// (effectiveAbilityDamageLocked) ⇒ not AuraOnly.
	{statAbilityDamage, "Ability Damage %", true, true, false},
	// Ability Power: a FLAT pool (not a fixed-1.0 multiplier), so an `add` is a
	// whole amount, not a percentage point ⇒ NOT IsFraction — the one place it
	// differs from Ability Damage directly above. AllowMultiply so a perk can
	// scale the pool; real fold site (deal_damage's ratio term) ⇒ not AuraOnly.
	{statAbilityPower, "Ability Power", true, false, false},
	// Vulnerable: fixed-1.0-baseline multiplier on incoming damage, so an `add`
	// is a percentage-point amount (IsFraction). Real top-level fold site
	// (perks_defense.go's amplification step) ⇒ not AuraOnly.
	//
	// Labelled "Vulnerable", not "Damage Taken", because the raw name is
	// AMBIGUOUS IN SIGN: a designer reading "Damage Taken +0.2" cannot tell
	// whether the unit now takes more damage or takes 0.2 less. "Vulnerable
	// +20%" only has one reading. The ID stays damageTaken — it is what
	// authored catalog data references (marker_trap's mark) and what the fold
	// site reads; only the human-facing name changed.
	{statDamageTaken, "Vulnerable", true, true, false},
	// Weaken: the attacker-side mirror of Vulnerable. Fixed-1.0-baseline
	// multiplier on OUTGOING damage, so an `add` is a percentage-point amount
	// (IsFraction). Real top-level fold site (applyUnitDamageWithSourceLocked's
	// attacker resolution) ⇒ not AuraOnly.
	//
	// Labelled "Weaken", not "Damage Dealt", for the same sign-ambiguity reason
	// Vulnerable is not "Damage Taken": a designer reading "Damage Dealt -0.2"
	// cannot tell whether the unit now deals more or less. The pilot use is the
	// debuff direction (exposed_weakness's `add -0.2` = deal 20% less), so the
	// label names that: "Weaken" reads as unambiguously "deals less". The ID
	// stays damageDealt — what authored catalog data references and what the
	// fold site reads; only the human-facing name is opinionated about sign.
	{statDamageDealt, "Weaken", true, true, false},
}

// statRegistryByID is the O(1) lookup index built once at init.
var statRegistryByID = func() map[string]statDef {
	m := make(map[string]statDef, len(statRegistry))
	for _, d := range statRegistry {
		m[d.ID] = d
	}
	return m
}()

// isKnownStat reports whether id names a registered stat.
func isKnownStat(id string) bool {
	_, ok := statRegistryByID[id]
	return ok
}

// statLabel returns the display label for a stat id, falling back to the raw id.
func statLabel(id string) string {
	if d, ok := statRegistryByID[id]; ok {
		return d.Label
	}
	return id
}

// isFractionStat reports whether id's value is itself a fraction (statDef.
// IsFraction) — see that field's doc comment. An unknown id conservatively
// returns false (bare-number rendering), matching statLabel's fallback
// behavior for unregistered ids.
func isFractionStat(id string) bool {
	if d, ok := statRegistryByID[id]; ok {
		return d.IsFraction
	}
	return false
}

// isAuraOnlyStat reports whether id names a stat that has no top-level fold
// site (statDef.AuraOnly) — see that field's doc comment. An unknown id
// conservatively returns false, matching isKnownStat's job of gating unknown
// ids separately (validatePerkStatModifier rejects unknown stats before this
// would ever matter).
func isAuraOnlyStat(id string) bool {
	if d, ok := statRegistryByID[id]; ok {
		return d.AuraOnly
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// Per-unit BASE stats.
//
// Most stats' base is a typed Unit field (Damage, MoveSpeed, Armor, …) — that
// field is the single source of truth and a designer edits it directly. A few
// registered stats have NO typed field: their base was a hardcoded global
// default (critChance = 5%, critMultiplier = 2×). statBaseAuthorable names those
// stats so a designer can author a per-unit-type base value for them on
// UnitDef.BaseStats (seeded onto Unit.BaseStats at spawn) — e.g. a naturally
// crit-prone unit type authoring baseStats.critChance = 0.15. This is the first
// step toward "a unit carries a base value for ANY registered stat"
// (lifesteal, thorns, …): a new base-authorable stat is (1) an entry here,
// (2) a statBaseDefault case, (3) a read site that folds
// unitBaseStat(unit, stat) through effectiveStatLocked.
// ─────────────────────────────────────────────────────────────────────────────

// statBaseAuthorable names the stats whose per-unit BASE value may be authored
// on UnitDef.BaseStats. Restricted to stats with no typed Unit field, so the
// map is never a second source of truth for a stat the unit already stores as
// a field (validateUnitDef rejects an authored baseStats key outside this set —
// same "no inert / no double-source authoring" rule the perk/status vocabulary
// uses).
var statBaseAuthorable = map[string]bool{
	statCritChance:    true,
	statCritMult:      true,
	statLifesteal:     true,
	statThorns:        true,
	statAbilityDamage: true,
	statAbilityPower:  true,
}

// isBaseAuthorableStat reports whether a per-unit base value may be authored for
// stat on UnitDef.BaseStats.
func isBaseAuthorableStat(stat string) bool { return statBaseAuthorable[stat] }

// baseAuthorableStatIDs returns the base-authorable stat ids, sorted — used to
// build designer-facing validation messages so they can never go stale as the
// set grows.
func baseAuthorableStatIDs() []string {
	out := make([]string, 0, len(statBaseAuthorable))
	for id := range statBaseAuthorable {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// statUnitInterval lists the base-authorable stats whose value is a genuine
// 0-1 probability/ratio, so an authored base above 1 is certainly a mistake
// (a designer typing 50 meaning "50%").
//
// This is deliberately SEPARATE from statDef.IsFraction. IsFraction is a
// RENDERING concern — "an `add` on this stat is a percentage-point amount, so
// show +0.1 as +10%" — and it is true for two different families:
//
//   - genuine 0-1 probabilities (critChance, lifesteal, thorns), whose value
//     must stay within [0,1]; and
//   - fixed-1.0-baseline MULTIPLIERS (abilityDamage, healingReceived,
//     gatherSpeed), whose value legitimately exceeds 1 — "1.5x ability damage"
//     is a perfectly good base.
//
// Only the first family may be range-clamped. Conflating the two was harmless
// while every base-authorable fraction stat happened to be a probability;
// abilityDamage is the first that is not.
var statUnitInterval = map[string]bool{
	statCritChance: true,
	statLifesteal:  true,
	statThorns:     true,
}

// isUnitIntervalStat reports whether stat's value must lie within [0,1].
func isUnitIntervalStat(stat string) bool { return statUnitInterval[stat] }

// statBaseDefault returns the base value a unit has for a base-authorable stat
// when it authors none — the former hardcoded global default. 0 for any stat
// with no registered default (which, combined with unitBaseStat, means an
// unauthored unit behaves exactly as before this system existed).
func statBaseDefault(stat string) float64 {
	switch stat {
	case statCritChance:
		return defaultCritChance
	case statCritMult:
		return defaultCritMultiplier
	case statAbilityDamage:
		// Identity multiplier: a unit authoring no base deals exactly the
		// ability's authored damage, so introducing this stat is a no-op.
		return 1
	case statAbilityPower:
		// A flat pool, so 0 is the identity: a unit authoring no base
		// contributes nothing through any ability's ratio.
		return 0
	}
	return 0
}

// unitBaseStat returns a unit's BASE value for a base-authorable stat: its
// authored per-unit-type value (Unit.BaseStats, seeded from UnitDef.BaseStats —
// which respects advancement-effective defs, see spawnUnitFromDefLocked) if
// present, else the stat's registered default. Pure read of the unit; safe on a
// nil unit or a unit with no BaseStats map (returns the default), so every read
// site behaves exactly as before when nothing authors a base.
func unitBaseStat(unit *Unit, stat string) float64 {
	if unit != nil {
		if v, ok := unit.BaseStats[stat]; ok {
			return v
		}
	}
	return statBaseDefault(stat)
}

// copyBaseStats returns a shallow copy of an authored BaseStats map, or nil when
// empty — so a unit that authors none carries a nil map (byte-identical to
// before this system) and a per-unit mutation never scribbles on the shared
// catalog def's map. Caller: spawnUnitFromDefLocked.
func copyBaseStats(src map[string]float64) map[string]float64 {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]float64, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// ListStatIDs returns the registered stat ids in a stable sorted order. Used by
// the editor schema endpoint / TS mirror and for deterministic enumeration.
func ListStatIDs() []string {
	ids := make([]string, 0, len(statRegistry))
	for _, d := range statRegistry {
		ids = append(ids, d.ID)
	}
	sort.Strings(ids)
	return ids
}

// validateStatModifier checks a single modifier at catalog load. ctx is a
// human-readable location (e.g. "zone north_outpost aura 0") used in the panic
// message. Returns an error; callers at load time panic on it (catalogs are
// static, so a bad entry is a build error, mirroring the zone validators).
func validateStatModifier(ctx string, m protocol.StatModifier) error {
	if !isKnownStat(m.Stat) {
		return fmt.Errorf("%s: unknown stat %q", ctx, m.Stat)
	}
	if m.Operation != statOpAdd && m.Operation != statOpMultiply {
		return fmt.Errorf("%s: invalid operation %q (want %q or %q)", ctx, m.Operation, statOpAdd, statOpMultiply)
	}
	if math.IsNaN(m.Value) || math.IsInf(m.Value, 0) {
		return fmt.Errorf("%s: non-finite value", ctx)
	}
	if m.Operation == statOpMultiply && m.Value == 0 {
		return fmt.Errorf("%s: multiply value must be non-zero", ctx)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Aggregation
// ─────────────────────────────────────────────────────────────────────────────

// statAccum is the reduced contribution for a single stat: the summed additive
// total and the product of multiplicative factors. Identity is {Add: 0, Mul: 1}.
type statAccum struct {
	Add float64
	Mul float64
}

// PlayerStatModifierSet is a player's aggregated stat modifiers, keyed by stat
// id. Absent keys resolve to the identity (0, 1). Reduced from all of the
// player's active StatModifier sources (zone auras in v1) and rebuilt on change
// — see zone_auras.go. Stored on Player; server-only (never on the wire).
type PlayerStatModifierSet map[string]statAccum

// newPlayerStatModifierSet returns an empty, non-nil set.
func newPlayerStatModifierSet() PlayerStatModifierSet {
	return PlayerStatModifierSet{}
}

// fold applies one modifier into the set per the stacking rule: add → sum,
// multiply → product. The first multiply seeds from the identity 1.0.
func (set PlayerStatModifierSet) fold(m protocol.StatModifier) {
	acc, ok := set[m.Stat]
	if !ok {
		acc = statAccum{Add: 0, Mul: 1}
	}
	switch m.Operation {
	case statOpAdd:
		acc.Add += m.Value
	case statOpMultiply:
		acc.Mul *= m.Value
	}
	set[m.Stat] = acc
}

// resolve returns (add, mul) for a stat, or the identity (0, 1) when absent.
func (set PlayerStatModifierSet) resolve(stat string) (add, mul float64) {
	if set == nil {
		return 0, 1
	}
	acc, ok := set[stat]
	if !ok {
		return 0, 1
	}
	return acc.Add, acc.Mul
}

// applyStatModifier applies a resolved (add, mul) to a base value per the
// canonical rule: effective = (base + add) × mul. Convenience for read sites.
func applyStatModifier(base, add, mul float64) float64 {
	return (base + add) * mul
}

// ─────────────────────────────────────────────────────────────────────────────
// Stage evaluation — shared by zone auras AND perk stat modifiers
// (perk_stat_modifiers.go). This is the ENGINE for PerkDef.StatModifiers: a
// typed, validated (validatePerkDef), registry-backed (isKnownStat) stat
// vocabulary that replaces the old freeform Config-map convention where a
// perk's Go handler had to know an exact key string — a typo there silently
// read 0 forever. StatModifiers is rejected at catalog load instead.
// ─────────────────────────────────────────────────────────────────────────────

// statStageAccum holds the (add, mul) pool for ONE stage. Identity is
// {Add: 0, Mul: 1} — a caller building one of these MUST seed Mul at 1; the
// Go zero value {0, 0} is NOT identity (an unseeded zero Mul would zero the
// stat when applied). unitPerkStatModifiersLocked and mergeZoneIntoBaseStage
// both seed correctly; do not construct a statStageAccum literal elsewhere
// without the same care.
type statStageAccum struct {
	Add float64
	Mul float64
}

// applyStatStages folds base through each stage in statStages order
// (intrinsic, then base, then final):
//
//	value := base
//	for each stage in statStages: value = (value + stage.Add) * stage.Mul
//
// This subsumes the pre-existing single-pool zone-aura rule as the "base"
// stage, and gives "final" strict after-everything semantics — e.g. base=10,
// a base-stage +10 add, a base-stage ×2 multiply, and a final-stage ×2
// multiply yields ((10+10)×2)×2 = 80, not 10+10×2×2.
//
// Adding "intrinsic" ahead of "base": base=10, an intrinsic-stage ×2
// multiply (no add authored at that stage), a base-stage +10 add, a
// base-stage ×2 multiply, and a final-stage ×2 multiply yields
// ((10×2 + 10) × 2) × 2 = 120 — the intrinsic multiply scales ONLY the
// unit's own base value, never the base stage's additive term.
//
// A stage absent from stages is a no-op for that stage — there is no
// implicit identity lookup, the map is trusted to only contain stages that
// actually contribute. Safe on a nil stages map (returns base unchanged),
// which is what every stat-read site relies on before any perk or aura
// modifies that stat.
func applyStatStages(base float64, stages map[string]statStageAccum) float64 {
	value := base
	for _, stage := range statStages {
		acc, ok := stages[stage]
		if !ok {
			continue
		}
		value = (value + acc.Add) * acc.Mul
	}
	return value
}

// mergeZoneIntoBaseStage merges a zone-aura (add, mul) pair — already
// resolved by the caller via playerStatModifierLocked — into the "base"
// stage of a perk stat-modifier pool (unitPerkStatModifiersLocked). Zone
// auras and base-stage perk StatModifiers are the same pool by design (see
// the package doc above), so this is the ONE merge point every stat-read
// fold site uses to combine the two sources before calling
// applyStatStages — do not re-derive this merge inline at a call site.
//
// Mutates and returns stages in place: stages is always a fresh, single-use
// map freshly built by unitPerkStatModifiersLocked for this one call, never
// a value retained or shared elsewhere, so in-place mutation is safe.
// Allocates a new map only when stages is nil and there is something to
// merge. No-op (returns stages, possibly nil, completely unchanged) when the
// zone pair is already identity — keeps the common "no aura active" path
// allocation-free.
func mergeZoneIntoBaseStage(stages map[string]statStageAccum, zoneAdd, zoneMul float64) map[string]statStageAccum {
	if zoneAdd == 0 && zoneMul == 1 {
		return stages
	}
	if stages == nil {
		stages = make(map[string]statStageAccum, 1)
	}
	base, ok := stages[statStageBase]
	if !ok {
		base = statStageAccum{Add: 0, Mul: 1}
	}
	base.Add += zoneAdd
	base.Mul *= zoneMul
	stages[statStageBase] = base
	return stages
}

// mergeStatStagePools merges pool b's per-stage (add, mul) contributions into
// pool a, stage by stage: adds sum, muls multiply — the same composition
// rule every other merge in this file uses. Used to combine two independent
// stat-modifier EMITTERS (e.g. unitPerkStatModifiersLocked's owned-perk pool
// and unitStatusStatModifiersLocked's active-status pool, perk_stat_modifiers.go)
// into one pool before the zone-aura merge / applyStatStages call, so a
// read site folds perk + status + zone-aura contributions through the exact
// same three-stage engine rather than three separate ad-hoc passes.
//
// Safe with either argument nil/empty (returns the other unchanged — no
// allocation on the common "nothing to merge" path). Mutates and returns a
// in place when a is non-nil, matching mergeZoneIntoBaseStage's "fresh,
// single-use map" contract: callers must pass an `a` that is not retained or
// shared elsewhere (unitPerkStatModifiersLocked's return value always is —
// it allocates a new map per call). When a is nil, allocates a fresh map
// sized for b rather than returning b itself, so the caller never receives
// back b's own backing map.
func mergeStatStagePools(a, b map[string]statStageAccum) map[string]statStageAccum {
	if len(b) == 0 {
		return a
	}
	if a == nil {
		a = make(map[string]statStageAccum, len(b))
	}
	for stage, bAcc := range b {
		acc, ok := a[stage]
		if !ok {
			acc = statStageAccum{Add: 0, Mul: 1}
		}
		acc.Add += bAcc.Add
		acc.Mul *= bAcc.Mul
		a[stage] = acc
	}
	return a
}

// playerStatModifierLocked resolves the (add, mul) a player's aggregated zone
// auras contribute to a stat. O(1). Returns the identity (0, 1) for an unknown
// player or a stat with no active modifier — so a read site that calls this
// when no auras are active behaves exactly as before this system existed.
//
// Must be called under s.mu (read or write) lock.
func (s *GameState) playerStatModifierLocked(playerID, stat string) (add, mul float64) {
	player, ok := s.Players[playerID]
	if !ok || player == nil {
		return 0, 1
	}
	return player.ZoneStatModifiers.resolve(stat)
}
