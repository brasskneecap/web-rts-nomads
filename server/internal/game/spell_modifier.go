package game

import (
	"fmt"
	"math"
	"sort"
)

// Spell modifier pipeline (arch-mage-spell-system, §10).
//
// A spell's base definition (AbilityDef) is IMMUTABLE. Perks, buffs, items,
// and future systems tune spells by contributing SpellModifiers, which are
// FOLDED at cast time into an EffectiveSpell. The base def is never mutated —
// two casts of the same spell under different modifiers each start from the
// same base.
//
// Determinism: for each field, all `add` operations are applied first (a sum),
// then all `multiply` operations (a product). Sum and product are each
// order-independent, so the resolved value never depends on the order in which
// modifiers were collected (or on map-iteration order). This is why fold order
// is pinned this way — it removes any need to sort modifiers for correctness.

// SpellModField is the typed, load-validated enum of spell values a modifier
// may target. Extensible-enum idiom, mirroring DamageType / AbilityCategory.
// The empty value is not valid (a modifier must name a field).
type SpellModField string

const (
	SpellModFieldManaCost        SpellModField = "manaCost"
	SpellModFieldCooldown        SpellModField = "cooldown"
	SpellModFieldCastTime        SpellModField = "castTime"
	SpellModFieldDamage          SpellModField = "damage"
	SpellModFieldRadius          SpellModField = "radius"
	SpellModFieldProjectileSpeed SpellModField = "projectileSpeed"
	SpellModFieldDuration        SpellModField = "duration"
	SpellModFieldChainCount      SpellModField = "chainCount"
	SpellModFieldPullStrength    SpellModField = "pullStrength"
)

// spellModFieldRegistry is the recognised modifier-field set. Never mutated
// from the tick loop, so it adds no determinism/concurrency concern.
var spellModFieldRegistry = map[SpellModField]struct{}{
	SpellModFieldManaCost:        {},
	SpellModFieldCooldown:        {},
	SpellModFieldCastTime:        {},
	SpellModFieldDamage:          {},
	SpellModFieldRadius:          {},
	SpellModFieldProjectileSpeed: {},
	SpellModFieldDuration:        {},
	SpellModFieldChainCount:      {},
	SpellModFieldPullStrength:    {},
}

// IsValidSpellModField reports whether f is a recognised modifier field.
func IsValidSpellModField(f SpellModField) bool {
	_, ok := spellModFieldRegistry[f]
	return ok
}

// SpellModFields returns every registered field, sorted, for stable output in
// APIs and tests.
func SpellModFields() []SpellModField {
	out := make([]SpellModField, 0, len(spellModFieldRegistry))
	for f := range spellModFieldRegistry {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// SpellModOperation is how a modifier combines with the running value. The
// empty value defaults to add (SpellModAdd) so authoring "operation" is
// optional and the common case (additive) needs no field.
type SpellModOperation string

const (
	SpellModAdd      SpellModOperation = "add"
	SpellModMultiply SpellModOperation = "multiply"
)

// SpellModTarget selects which spells a modifier applies to. A modifier
// applies when EVERY specified field matches (unspecified fields are
// wildcards). An entirely-empty target matches nothing and is an authoring
// error (see Validate) — "modify every spell" is never expressible by accident.
type SpellModTarget struct {
	SpellID string `json:"spellId,omitempty"` // matches AbilityDef.ID
	School  string `json:"school,omitempty"`  // matches AbilityDef.DamageType
	Tag     string `json:"tag,omitempty"`     // matches AbilityDef.Tags membership
}

// SpellModifier is one contribution to a spell's effective values. It is pure
// value data (determinism- and snapshot-safe) so buffs/items/perks can
// construct or store them freely.
type SpellModifier struct {
	Target    SpellModTarget    `json:"target"`
	Field     SpellModField     `json:"field"`
	Operation SpellModOperation `json:"operation,omitempty"` // "" ⇒ add
	Value     float64           `json:"value"`
}

// Validate reports an authoring error in a modifier: an empty target (matches
// nothing), an unrecognised field, or an unknown operation.
func (m SpellModifier) Validate() error {
	if m.Target.SpellID == "" && m.Target.School == "" && m.Target.Tag == "" {
		return fmt.Errorf("spell modifier target must specify at least one of spellId/school/tag")
	}
	if !IsValidSpellModField(m.Field) {
		return fmt.Errorf("spell modifier field %q is not a recognised SpellModField", m.Field)
	}
	if m.Operation != "" && m.Operation != SpellModAdd && m.Operation != SpellModMultiply {
		return fmt.Errorf("spell modifier operation %q must be %q or %q", m.Operation, SpellModAdd, SpellModMultiply)
	}
	return nil
}

// appliesTo reports whether this modifier targets the given ability. All
// specified target fields must match; unspecified fields are wildcards. An
// empty target never applies (guarded here as well as in Validate, so a
// malformed modifier that slipped through is inert rather than universal).
func (m SpellModifier) appliesTo(def AbilityDef) bool {
	t := m.Target
	if t.SpellID == "" && t.School == "" && t.Tag == "" {
		return false
	}
	if t.SpellID != "" && t.SpellID != def.ID {
		return false
	}
	if t.School != "" && string(def.DamageType) != t.School {
		return false
	}
	if t.Tag != "" && !def.HasTag(t.Tag) {
		return false
	}
	return true
}

// EffectiveSpell is the resolved, tick-local view of a spell's modifier-eligible
// values after folding active modifiers over the base def. Cast code reads
// THIS, never the raw AbilityDef fields, for anything a modifier can touch.
type EffectiveSpell struct {
	ManaCost        int
	Cooldown        float64
	CastTime        float64
	Damage          int
	DamagePerSecond float64
	Radius          float64
	ProjectileSpeed float64
	Duration        float64
	ChainCount      int
	PullStrength    float64
	// DamageEffectivenessMultiplier scales a COMPOSABLE (program-driven)
	// deal_damage action's resolved amount on TOP of the standard
	// SpellModifier fold, for a caller that resolves a spell at reduced (or
	// boosted) effectiveness via a caller-built EffectiveSpell rather than a
	// normal cast — e.g. unstable_magic's free proc (perks_arch_mage.go),
	// which calls scaleEffectiveSpellDamage to set this field. The Go zero
	// value (every ordinary cast) means "no extra scaling"; read it via
	// effectivenessMultiplier(), never the raw field, so the zero-value case
	// is never accidentally treated as "scale to zero". Legacy resolution
	// never reads this field — it reads .Damage/.DamagePerSecond directly,
	// which scaleEffectiveSpellDamage already scales in place — so this is
	// inert for every path except resolveAbilityProgramCastLocked honouring a
	// caller-supplied eff (see RuntimeAbilityContext.damageEffectivenessMultiplier).
	DamageEffectivenessMultiplier float64
}

// effectivenessMultiplier returns e.DamageEffectivenessMultiplier, treating
// the Go zero value (an ordinary, non-scaled EffectiveSpell) as 1.0 (no extra
// scaling). See the field's doc comment for why this indirection exists.
func (e EffectiveSpell) effectivenessMultiplier() float64 {
	if e.DamageEffectivenessMultiplier == 0 {
		return 1.0
	}
	return e.DamageEffectivenessMultiplier
}

// EffectiveCooldown mirrors AbilityDef.EffectiveCooldown on the resolved
// values: the armed cooldown is at least the cast time so the action-bar wipe
// is always visible while the cast is in flight.
func (e EffectiveSpell) EffectiveCooldown() float64 {
	if e.CastTime > e.Cooldown {
		return e.CastTime
	}
	return e.Cooldown
}

// applySpellModField folds the adds/muls for one field over base, considering
// only modifiers in mods that apply to def (per SpellModifier.appliesTo): all
// `add` operations are summed first, then all `multiply` operations are
// applied as a product, and the result is floored at 0. Pure and
// deterministic; order-independent (sum and product are each
// order-independent). Shared by resolveEffectiveSpell (legacy per-field
// resolution) and effectiveAbilityDamageLocked (the executor's deal_damage
// scaling seam) so a composable action's amount scales identically to a
// legacy spell's field — this is the single fold implementation, not two
// copies that could drift.
func applySpellModField(mods []SpellModifier, def AbilityDef, field SpellModField, base float64) float64 {
	add := 0.0
	mul := 1.0
	hasMul := false
	for _, m := range mods {
		if m.Field != field || !m.appliesTo(def) {
			continue
		}
		switch m.Operation {
		case SpellModMultiply:
			mul *= m.Value
			hasMul = true
		default: // SpellModAdd or "" (default additive)
			add += m.Value
		}
	}
	v := base + add
	if hasMul {
		v *= mul
	}
	if v < 0 {
		return 0
	}
	return v
}

// resolveEffectiveSpell folds mods over def and returns the effective values.
// Pure and deterministic: no lock, no RNG, no clock; result is independent of
// the order of mods. The base def is not mutated. Modifiers that do not apply
// to def are skipped; a modifier naming a field the spell does not use is
// harmless (it adjusts an EffectiveSpell field the spell's mechanic ignores).
// Resolved values are floored at 0 (a negative mana cost / damage / radius is
// meaningless) and int-valued fields are rounded.
func resolveEffectiveSpell(def AbilityDef, mods []SpellModifier) EffectiveSpell {
	apply := func(field SpellModField, base float64) float64 {
		return applySpellModField(mods, def, field, base)
	}
	return EffectiveSpell{
		ManaCost: int(math.Round(apply(SpellModFieldManaCost, float64(def.ManaCost)))),
		Cooldown: apply(SpellModFieldCooldown, def.Cooldown),
		CastTime: apply(SpellModFieldCastTime, def.CastTime),
		Damage:   int(math.Round(apply(SpellModFieldDamage, float64(def.DamageAmount)))),
		// DamagePerSecond reuses the `damage` field so a "+X% damage" perk scales
		// a DoT too. A spell declares DamageAmount OR DamagePerSecond, never both,
		// so the shared field never double-applies within one spell.
		DamagePerSecond: apply(SpellModFieldDamage, def.DamagePerSecond),
		Radius:          apply(SpellModFieldRadius, def.Radius),
		ProjectileSpeed: apply(SpellModFieldProjectileSpeed, def.ProjectileSpeed),
		Duration:        apply(SpellModFieldDuration, def.Duration),
		ChainCount:      int(math.Round(apply(SpellModFieldChainCount, float64(def.ChainCount)))),
		PullStrength:    apply(SpellModFieldPullStrength, def.PullStrength),
	}
}

// collectSpellModifiersLocked gathers every active modifier for (caster, def)
// from all sources. This is the single documented plug-in point where future
// spell-modifying content is wired in: add a source below and it flows into
// every cast automatically. Collection is deterministic — it reads only stable
// unit/perk/item state, no clock or unseeded RNG. Matching against def is done
// later by resolveEffectiveSpell, so a source may return a superset.
//
// Caller holds s.mu.
func (s *GameState) collectSpellModifiersLocked(caster *Unit, def AbilityDef) []SpellModifier {
	if caster == nil {
		return nil
	}
	var mods []SpellModifier
	// Source 1: per-unit modifiers (the concrete buff/item attachment point).
	mods = append(mods, caster.SpellModifiers...)
	// Source 2+: perk/other seams. Empty today — the place future passive
	// spell-tuning perks plug in without touching the cast path.
	mods = append(mods, s.perkSpellModifiersLocked(caster, def)...)
	return mods
}

// perkSpellModifiersLocked is the seam for perks that tune spells. No perk
// contributes spell modifiers yet; this returns nil and exists so the wiring
// is present and obvious for future Arch Mage perks (which will read
// caster.PerkState / PerkIDs and emit SpellModifiers here).
//
// Caller holds s.mu.
func (s *GameState) perkSpellModifiersLocked(caster *Unit, def AbilityDef) []SpellModifier {
	return nil
}

// effectiveSpellLocked resolves the cast-time effective values for caster's
// use of def: collect all active modifiers, then fold. The base def is never
// mutated.
//
// Caller holds s.mu.
func (s *GameState) effectiveSpellLocked(caster *Unit, def AbilityDef) EffectiveSpell {
	eff := resolveEffectiveSpell(def, s.collectSpellModifiersLocked(caster, def))
	// Fold the caster's perk-driven cooldown modifier (AbilityModifier.CooldownMult,
	// composed by abilityScalarModifiersForCasterLocked) on top of the
	// SpellModifier-resolved cooldown. This is the cast-path read-point for the
	// Tier-A ability cooldown modifier — identity (1.0) for every ability whose
	// caster owns no CooldownMult perk, so existing casts are unchanged; the
	// Trapper's rapid_deployment is the first consumer, shortening its trap
	// abilities' cooldowns. Applied to eff.Cooldown before EffectiveCooldown()
	// clamps it against CastTime at the arm site (ability_cast.go).
	if mods := s.abilityScalarModifiersForCasterLocked(caster, def.ID); mods.CooldownMult > 0 {
		eff.Cooldown *= mods.CooldownMult
	}
	return eff
}

// abilityDamageStatOnlyLocked applies ONLY the caster's abilityDamage stat, with
// none of the spell-modifier fold effectiveAbilityDamageLocked also does.
//
// The split exists to keep a documented legacy parity intact. A zone tick / DoT
// tick historically applied its damage RAW — that is what the legacy trap and
// ground-hazard runtimes did, and the golden migration tests assert the
// executor matches them exactly (a caster's SpellModifier deliberately does not
// reach a burn tick). Meanwhile abilityDamage is a UNIT stat that must reach
// every ability the unit casts, or a perk saying "my abilities hit harder" does
// nothing on precisely the abilities that are zones.
//
// So: the stat applies everywhere, spell modifiers only where they always did.
// If DoT ticks should start honouring spell modifiers too, that is a gameplay
// decision to make deliberately — it changes meteor, and the goldens will say so.
//
// Caller holds s.mu.
func (s *GameState) abilityDamageStatOnlyLocked(caster *Unit, base int) int {
	mult := s.effectiveStatLocked(caster, unitBaseStat(caster, statAbilityDamage), statAbilityDamage)
	if mult == 1.0 {
		return base
	}
	return int(math.Round(float64(base) * mult))
}

// effectiveAbilityDamageLocked scales a composable ability action's base
// damage amount by caster's active spell-modifiers for def's school/tags,
// at PARITY with effectiveSpellLocked's Damage field: it folds the exact
// same modifier set through the exact same applySpellModField helper, so a
// deal_damage action scales identically to a legacy spell's DamageAmount.
// base is the action's configured amount (e.g. dealDamageConfig.Amount), not
// necessarily def.DamageAmount, so the executor's per-action authoring stays
// independent of the legacy def's own DamageAmount field. Caller holds s.mu.
func (s *GameState) effectiveAbilityDamageLocked(caster *Unit, def AbilityDef, base int) int {
	mods := s.collectSpellModifiersLocked(caster, def)
	scaled := applySpellModField(mods, def, SpellModFieldDamage, float64(base))
	// Fold the caster's unit-level ability-damage stat on top. This is the ONE
	// read point for "this unit's abilities hit harder", so a rank, item,
	// advancement, perk, status or zone aura all reach every ability's damage
	// through the stat chokepoint with no per-ability authoring
	// (docs/design/ability_perk_interaction.md D3). Identity (1.0) for any unit
	// that authors no base and owns no contributing source, so this is a no-op
	// for every existing ability.
	scaled *= s.effectiveStatLocked(caster, unitBaseStat(caster, statAbilityDamage), statAbilityDamage)
	return int(math.Round(scaled))
}
