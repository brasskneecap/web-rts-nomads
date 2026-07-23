package game

import (
	"fmt"
	"math"
	"strings"
)

// perk_describe.go turns a PerkDef's TYPED authored data (StatModifiers,
// AbilityModifiers, AbilityRiders, GrantsAbilities) into player-facing
// tooltip prose — the perk-catalog sibling of ability_describe.go's
// describeAbility, and built to close the SAME dual-source-of-truth gap
// abilities already closed there.
//
// Before this file existed, a perk's tooltip was hand-authored prose
// (PerkDef.TooltipTemplate) interpolated client-side against a SEPARATE
// freeform Config map. A designer who changed a typed StatModifier's Value
// via the perk editor's dropdown without ALSO updating the matching Config
// key got a tooltip that silently lied — the whole point of making perk
// stats typed and validated (see PerkStatModifier's doc comment) was to
// eliminate exactly that class of bug, and TooltipTemplate/Config
// re-introduced it through the back door.
//
// describePerk is the fix: it reads the SAME typed fields the engine
// applies (perk_stat_modifiers.go, ability_modifiers.go, ability_riders.go,
// path_ability_defs.go) and generates prose from them directly, so
// tooltip and behavior can never drift. It is exposed exactly like
// describeAbility: PerkDef.GeneratedDescription (populated only on
// ListPerkDefs' returned copies — see that field's doc comment) is what a
// future client / editor consumes; TooltipTemplate/Config remain the
// authoritative source until that client cutover lands (a separate task),
// at which point they can be deleted per perk that has fully equivalent
// typed data (see this file's package-level callers for which perks
// qualify today).
//
// Mirrors describeAbility's architecture on purpose: pure function (no
// GameState, no lock — the only external read is the read-only ability
// registry via getAbilityDef, mirroring how describeAbility never touches
// live state either), deterministic (grouping never leaks Go's randomized
// map iteration order into the generated string — see describePerkStatModifiers),
// and additive-only (an untouched PerkDef field yields no clause, exactly
// like an unset AbilityDef field yields no clause in describeAbility).

// describePerk builds generated tooltip prose from def's typed authored
// fields, in a fixed field order (StatModifiers, then Auras, then
// AbilityModifiers, then AbilityRiders, then GrantsAbilities). Returns "" when
// none of those fields are populated, so callers can fall back to a
// hand-authored Description/TooltipTemplate instead of showing nothing.
func describePerk(def PerkDef) string {
	var sentences []string

	if s := describePerkStatModifiers(def.StatModifiers); s != "" {
		sentences = append(sentences, s)
	}
	if s := describePerkAuras(def.Auras); s != "" {
		sentences = append(sentences, s)
	}
	if s := describePerkAbilityModifiers(def.AbilityModifiers); s != "" {
		sentences = append(sentences, s)
	}
	if s := describePerkAbilityStats(def.AbilityStats); s != "" {
		sentences = append(sentences, s)
	}
	if s := describePerkAbilityFields(def.AbilityFields); s != "" {
		sentences = append(sentences, s)
	}
	if s := describePerkAbilityRiders(def.AbilityRiders); s != "" {
		sentences = append(sentences, s)
	}
	if s := describePerkGrantsAbilities(def.GrantsAbilities); s != "" {
		sentences = append(sentences, s)
	}

	return strings.Join(sentences, " ")
}

// ─────────────────────────────────────────────────────────────────────────────
// StatModifiers
// ─────────────────────────────────────────────────────────────────────────────

// describePerkStatModifiers renders mods as a single sentence, e.g.
// "+90 Max Health." or "+15% Damage (before other bonuses), +0.3 Attack Speed."
//
// Modifiers are grouped by stage and walked in statStages order (intrinsic,
// then base, then final) — the SAME fixed, authored order applyStatStages
// folds through (stat_modifiers.go) — rather than the slice's authored
// order, so a perk that mixes stages always reads "what applies first"
// first. Within a stage, authored (slice) order is preserved. This never
// ranges over a map to build output — the intermediate stage bucketing map
// is only ever read back via the statStages slice, never ranged directly —
// so output order is identical across repeated calls (AI_RULES.md
// determinism).
func describePerkStatModifiers(mods []PerkStatModifier) string {
	if len(mods) == 0 {
		return ""
	}

	byStage := make(map[string][]PerkStatModifier, len(statStages))
	for _, m := range mods {
		stage := m.Stage
		if stage == "" {
			stage = statStageBase
		}
		byStage[stage] = append(byStage[stage], m)
	}

	var clauses []string
	for _, stage := range statStages {
		for _, m := range byStage[stage] {
			clauses = append(clauses, describeStatModifierClause(m, stage))
		}
	}
	if len(clauses) == 0 {
		return ""
	}
	return capitalize(strings.Join(clauses, ", ")) + "."
}

// describeStatModifierClause renders one modifier: "multiply" always as a
// signed percentage delta ("+15% Damage" for 1.15, "-50% Mana Regen" for 0.5)
// since a raw factor like "1.15x" is far less readable than "+15%". "add"
// renders two ways depending on isFractionStat(m.Stat):
//   - true (the stat's value IS itself a 0-1-ish fraction, e.g. critChance) ->
//     a signed percentage ("+10% Crit Chance" for +0.1), because the delta
//     already IS a percentage-point amount independent of any per-unit base.
//   - false (a raw rate/value with a per-unit base, e.g. attackSpeed) -> a
//     signed bare number ("+0.3 Attack Speed"), because the real percentage
//     effect depends on which unit's base the add lands on and rendering a
//     guessed percentage here is exactly the hawk_spirit bug this exists to
//     prevent.
//
// The stage suffix is appended ONLY for a non-default stage — statStageBase
// (the authoring default, an empty Stage) gets no suffix at all.
func describeStatModifierClause(m PerkStatModifier, stage string) string {
	var clause string
	switch {
	case m.Op == statOpMultiply:
		clause = fmt.Sprintf("%s %s", signedPercent(m.Value-1), plainStatLabel(statLabel(m.Stat)))
	case isFractionStat(m.Stat):
		clause = fmt.Sprintf("%s %s", signedPercent(m.Value), plainStatLabel(statLabel(m.Stat)))
	default:
		clause = fmt.Sprintf("%s %s", signedNumber(m.Value), statLabel(m.Stat))
	}
	switch stage {
	case statStageIntrinsic:
		clause += " (before other bonuses)"
	case statStageFinal:
		clause += " (applied last)"
	}
	return clause
}

// ─────────────────────────────────────────────────────────────────────────────
// Auras
// ─────────────────────────────────────────────────────────────────────────────

// describePerkAuras renders each PerkAura as one sentence: "<Allies|Enemies>
// within <radius> gain <deltas>." plus a trailing stacking clause that is
// ALWAYS present — either "Each additional covering source adds <deltas>."
// when PerAdditionalSource is nonzero, or "Multiple sources do not stack;
// the strongest aura wins." when it is zero and Stacking resolves to
// auraStackingMax (today's only implemented mode). Conveying the stacking
// rule unconditionally, rather than only when PerAdditionalSource is set, is
// what let mana_conduit's tooltipTemplate be deleted (perk_describe_test.go)
// — the old omission silently dropped the "does not stack" rule for any
// aura with no per-source term. Multiple auras on one perk (none shipped
// today) become multiple space-joined sentences in authored (slice) order —
// auras have no stage concept to reorder by (validatePerkDef rejects
// anything but the base stage; see PerkAura's doc comment).
func describePerkAuras(auras []PerkAura) string {
	if len(auras) == 0 {
		return ""
	}
	var out []string
	for _, a := range auras {
		if clause := describePerkAuraClause(a); clause != "" {
			out = append(out, clause)
		}
	}
	return strings.Join(out, " ")
}

// describePerkAuraClause renders one PerkAura. Returns "" when it has no
// StatModifiers (nothing to describe).
func describePerkAuraClause(a PerkAura) string {
	if len(a.StatModifiers) == 0 {
		return ""
	}
	parts := make([]string, 0, len(a.StatModifiers))
	for _, sm := range a.StatModifiers {
		parts = append(parts, describeAuraStatModifierClause(sm))
	}
	sentence := fmt.Sprintf("%s within %s gain %s.", auraTargetLabel(a.Targets), trimFloat(a.Radius), strings.Join(parts, ", "))
	if stacking := describeAuraStackingClause(a); stacking != "" {
		sentence += " " + stacking
	}
	return sentence
}

// describeAuraStackingClause renders the stacking rule for a — this is
// ALWAYS non-empty for the one implemented Stacking mode (auraStackingMax),
// so a perk's generated tooltip never leaves a reader guessing whether
// multiple covering sources add up.
//
//   - PerAdditionalSource != 0 — every covering source beyond the first adds
//     a smaller, separately-authored bonus. PerAdditionalSource is a single
//     scalar shared by every StatModifiers entry on this aura (see
//     auraEmission / rebuildAuraStatCacheLocked, perk_aura_stat_cache.go —
//     one PerAdditionalSource per aura, not per stat), so the clause reuses
//     the same stat labels with the stacking magnitude substituted for
//     Value.
//   - PerAdditionalSource == 0 (the common case: no per-source term
//     authored) and Stacking resolves to auraStackingMax (the default when
//     omitted — see validatePerkDef's switch) — only the single strongest
//     covering source counts; extra sources contribute nothing.
//
// A future non-max Stacking mode would need its own branch here; today
// validatePerkDef rejects anything but "" / auraStackingMax at load time, so
// this switch can never fall through silently.
func describeAuraStackingClause(a PerkAura) string {
	if a.PerAdditionalSource > 0 {
		stackParts := make([]string, 0, len(a.StatModifiers))
		for _, sm := range a.StatModifiers {
			stackParts = append(stackParts, describeAuraStatModifierClause(PerkStatModifier{Stat: sm.Stat, Op: sm.Op, Value: a.PerAdditionalSource}))
		}
		return fmt.Sprintf("Each additional covering source adds %s.", strings.Join(stackParts, ", "))
	}
	if a.Stacking == "" || a.Stacking == auraStackingMax {
		return "Multiple sources do not stack; the strongest aura wins."
	}
	return ""
}

// auraTargetLabel turns PerkAura.Targets ("allies"/"enemies"/"sameOwner")
// into the capitalized subject of the generated sentence. "sameOwner" reads
// as "Your units" rather than "Allies" — the whole point of the distinct
// Targets value is that it is NARROWER than "allies" (same OWNER, not
// merely same team; see PerkAura.Targets' doc comment), so reusing the
// "Allies" label here would misdescribe guardian_aura's actual behavior in
// team games. Defaults to "Allies" for any other unrecognized value —
// validatePerkDef already rejects anything but "allies"/"enemies"/
// "sameOwner" at load time, so this only matters for a synthetic
// (non-catalog) PerkDef built directly in a test.
func auraTargetLabel(targets string) string {
	switch targets {
	case "enemies":
		return "Enemies"
	case "sameOwner":
		return "Your units"
	default:
		return "Allies"
	}
}

// describeAuraStatModifierClause renders one aura StatModifiers entry as
// "<delta> <Label>". validatePerkDef guarantees Op == statOpAdd and
// Stage == base for every aura entry (see that function's aura loop), so
// unlike describeStatModifierClause (which also handles multiply and
// non-base stages for PerkDef.StatModifiers), this only ever needs the "add"
// rendering rule.
//
// Percentage-vs-bare-number rendering deliberately does NOT reuse
// isFractionStat here — see auraStatRendersAsPercent's doc comment for why an
// aura's Value has different semantics from a plain PerkStatModifier add.
func describeAuraStatModifierClause(sm PerkStatModifier) string {
	if auraStatRendersAsPercent(sm.Stat) {
		return fmt.Sprintf("%s %s", signedPercent(sm.Value), statLabel(sm.Stat))
	}
	return fmt.Sprintf("%s %s", signedNumber(sm.Value), statLabel(sm.Stat))
}

// auraStatRendersAsPercent reports whether an AURA's StatModifiers Value for
// `stat` should render as a percentage in generated prose, rather than a bare
// number.
//
// This is DELIBERATELY separate from isFractionStat, which governs
// PerkStatModifier's "add" rendering (stat_modifiers.go) and is false for
// moveSpeed — because the two mechanisms consume the SAME field name
// differently:
//
//   - PerkStatModifier{Stat: "moveSpeed", Op: "add"} folds through
//     unitPerkStatModifiersLocked / applyStatStages, landing as a raw addend
//     on unit.MoveSpeed BEFORE the (base+add)×mul pool is evaluated — its
//     real percentage effect depends on which unit's MoveSpeed base it lands
//     on, so rendering a guessed percentage there would be exactly the
//     hawk_spirit bug isFractionStat exists to prevent (a +0.3 add is +20%
//     on a 1.5 base and +30% on a 1.0 base — the generator cannot know
//     which recipient will read it).
//   - PerkAura{Stat: "moveSpeed"}'s Value is NOT folded through that
//     pipeline at all (see PerkAura's doc comment). Its one shipped fold
//     site, perkMoveSpeedMultiplierLocked (perks_movement.go), adds the
//     resolved contribution into `bonus` and then computes
//     `perkMult := 1.0 + bonus`, which directly multiplies the recipient's
//     final move speed. That composition makes Value a BONUS FRACTION of
//     whatever the recipient's speed already is — the same role a "multiply"
//     operand plays elsewhere — not an absolute delta on a per-unit-varying
//     base. A Value of 0.3 unambiguously means "+30% of this unit's current
//     speed" for every recipient regardless of their base, so there is no
//     unit-dependent ambiguity to guess at: rendering it as a percentage is a
//     fact about the fold site's arithmetic, not a guess dressed up as one.
//
// Conservative default false, matching isFractionStat: an aura fold site
// that instead added its Value directly onto a raw per-unit base (the same
// way PerkStatModifier does) would need bare-number rendering instead, and
// must be added here explicitly — never swept in by a blanket default.
// moveSpeed is the only stat with a shipped aura fold site today
// (perkMoveSpeedMultiplierLocked); a future aura fold site for a different
// stat must inspect its OWN composition (per the "ordering trap" doc on
// perk_aura_stat_cache.go) before deciding whether to add itself here.
func auraStatRendersAsPercent(stat string) bool {
	switch stat {
	case statMoveSpeed:
		return true
	default:
		return false
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AbilityModifiers
// ─────────────────────────────────────────────────────────────────────────────

// describePerkAbilityModifiers renders each modifier as its own
// "<Ability>: <deltas>." sentence, e.g. "Siphon Life: +100% damage, +100%
// healing." Multiple AbilityModifier entries (targeting different abilities)
// become multiple sentences, space-joined.
func describePerkAbilityModifiers(mods []AbilityModifier) string {
	if len(mods) == 0 {
		return ""
	}
	var out []string
	for _, m := range mods {
		if clause := describeAbilityModifierClause(m); clause != "" {
			out = append(out, clause)
		}
	}
	return strings.Join(out, " ")
}

// ─────────────────────────────────────────────────────────────────────────────
// AbilityStats
// ─────────────────────────────────────────────────────────────────────────────

// describePerkAbilityStats renders a perk's broad ability-stat rows, e.g.
// "+50% Zone Radius." or "Fire Pit: +2s Duration." — one clause per row, in
// AUTHORED ORDER (a slice, never a map), so the sentence is identical across
// repeated calls.
//
// Two row families read differently and are worded to match:
//
//   - a KINDED row scales a shape (radius, duration, count), so it reads as a
//     percentage or an amount OF that thing.
//   - an INFLICTED row changes what the ability does TO a unit, and its sign is
//     the whole meaning: "-15% Move Speed" is a STRONGER slow, not a weaker one.
//     Worded as "stronger"/"weaker" rather than a bare signed number, because a
//     designer reading "-0.15 Move Speed" cannot tell which way it cuts — the
//     exact ambiguity that got "Damage Taken" renamed to "Vulnerable".
func describePerkAbilityStats(rows []PerkAbilityStat) string {
	if len(rows) == 0 {
		return ""
	}
	var out []string
	for _, row := range rows {
		clause := describeAbilityStatRow(row)
		if clause == "" {
			continue
		}
		if row.Ability != "" {
			clause = fmt.Sprintf("%s: %s", abilityDisplayNameOrID(row.Ability), clause)
		}
		out = append(out, clause+".")
	}
	return strings.Join(out, " ")
}

func describeAbilityStatRow(row PerkAbilityStat) string {
	if row.Flat == 0 && row.Pct == 0 {
		return ""
	}
	label := abilityStatRowLabel(row.Stat)

	if isInflictedStatID(row.Stat) {
		// Sign carries the meaning, and which direction is "stronger" depends on
		// the stat: more Vulnerable is a bigger number, a harder slow is a
		// SMALLER move-speed multiplier. Rather than encode that per stat, say
		// what changed and by how much, and let the sign speak.
		//
		// A fixed-1.0-baseline stat (Vulnerable, Healing Received) is measured in
		// percentage POINTS, so 0.15 reads as "+15%"; anything else is a raw
		// amount. Same rule describeStatModifierClause uses, for the same reason.
		amount := signedNumber(row.Flat)
		if isFractionStat(row.Stat) {
			amount = signedPercent(row.Flat)
		}
		return fmt.Sprintf("%s %s applied", amount, plainStatLabel(label))
	}

	var parts []string
	if row.Pct != 0 {
		parts = append(parts, fmt.Sprintf("%s %s", signedPercent(row.Pct), plainStatLabel(label)))
	}
	if row.Flat != 0 {
		parts = append(parts, fmt.Sprintf("%s %s", signedNumber(row.Flat), label))
	}
	return strings.Join(parts, ", ")
}

// plainStatLabel drops a trailing "%" from a stat's display name when the
// clause around it already carries one. "Ability Damage %" earns that suffix in
// a PICKER, where it is what separates it from Ability Power at a glance; in a
// sentence it produces "+35% Ability Damage %", which reads as a typo.
func plainStatLabel(label string) string {
	return strings.TrimSpace(strings.TrimSuffix(label, "%"))
}

// abilityStatRowLabel is the designer-facing name for an ability-stat id:
// the registry label for an inflicted unit stat, the scoped/broad grid label
// otherwise. Falls back to the raw id so a stat that has lost its definition
// still reads as something rather than vanishing from the sentence.
func abilityStatRowLabel(id string) string {
	for _, d := range AbilityStatDefs() {
		if d.ID == id {
			return d.Label
		}
	}
	return id
}

// ─────────────────────────────────────────────────────────────────────────────
// AbilityFields
// ─────────────────────────────────────────────────────────────────────────────

// describePerkAbilityFields renders a perk's PRECISE rows — one field of one
// action of one ability. There is no way to name the field in prose that a
// designer would recognise (an action id like "mark" is authoring detail), so
// the clause names the ABILITY and the operation's effect, e.g.
// "Marker Trap: +35% mark duration."
//
// Authored order, never a map, for the same determinism reason as every other
// describer here.
func describePerkAbilityFields(mods []AbilityFieldModifier) string {
	if len(mods) == 0 {
		return ""
	}
	var out []string
	for _, m := range mods {
		delta := describeFieldOpDelta(m)
		if delta == "" {
			continue
		}
		out = append(out, fmt.Sprintf("%s: %s %s.",
			abilityFieldTargetLabel(m.Target), delta, humanizeFieldName(m.Field)))
	}
	return strings.Join(out, " ")
}

// abilityFieldTargetLabel names what a precise modifier points at: a single
// ability by display name, or a TAG ("tag:trap") as the group it stands for,
// since a tag addresses however many abilities carry it.
func abilityFieldTargetLabel(target string) string {
	if tag, ok := strippedTagTarget(target); ok {
		return fmt.Sprintf("Abilities tagged %q", tag)
	}
	return abilityDisplayNameOrID(target)
}

// describeFieldOpDelta renders the SIZE of a precise modifier, honouring its op.
// An identity value (x1, +0) contributes nothing and returns "".
func describeFieldOpDelta(m AbilityFieldModifier) string {
	switch m.Op {
	case statOpAdd:
		if m.Value == 0 {
			return ""
		}
		return signedNumber(m.Value)
	case statOpAmplify:
		if m.Value == 1 {
			return ""
		}
		return signedPercent(m.Value - 1)
	default: // statOpMultiply and the empty default
		if m.Value == 1 {
			return ""
		}
		return signedPercent(m.Value - 1)
	}
}

// humanizeFieldName turns a config key into prose ("duration", "radius",
// "amount" -> "damage"). Deliberately small: the set of fields a perk addresses
// precisely is small, and a key with no entry reads fine as itself.
func humanizeFieldName(field string) string {
	switch field {
	case "amount":
		return "damage"
	case "value":
		return "strength"
	case targetQueryRadiusField:
		return "target radius"
	}
	return field
}

// abilityModifierField pairs one AbilityModifier scalar with its prose
// label, in the fixed rendering order describeAbilityModifierClause walks:
// damage, healing, mana cost, range, then cooldown. This matches
// AbilityModifier's own field order (perk_defs.go) so the generated clause
// order never depends on anything but that struct's declaration.
type abilityModifierField struct {
	mult  float64
	label string
}

// describeAbilityModifierClause renders one AbilityModifier as
// "<Ability>: <deltas>." A zero-valued mult field means "unset" (identity
// 1.0, per AbilityModifier's doc comment) and is skipped, so a modifier that
// only sets DamageMult never claims a phantom "+0% healing". Returns "" when
// every field is unset (e.g. a Target with no scalars authored).
func describeAbilityModifierClause(m AbilityModifier) string {
	fields := []abilityModifierField{
		{m.DamageMult, "damage"},
		{m.HealMult, "healing"},
		{m.ManaCostMult, "mana cost"},
		{m.RangeMult, "range"},
		{m.CooldownMult, "cooldown"},
	}
	var parts []string
	for _, f := range fields {
		if f.mult == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s", signedPercent(f.mult-1), f.label))
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("%s: %s.", abilityDisplayNameOrID(m.Target), strings.Join(parts, ", "))
}

// ─────────────────────────────────────────────────────────────────────────────
// AbilityRiders
// ─────────────────────────────────────────────────────────────────────────────

// describePerkAbilityRiders renders each rider as a brief clause naming the
// target ability, the trigger it grafts onto, and (when cheaply
// enumerable) what kind of effect it adds — e.g. "Adds an extra damage
// effect to Siphon Life's beam tick." A rider's Actions can in principle run
// an arbitrarily deep action program (the SAME structural validator an
// authored ability's own actions go through — see AbilityRider's doc
// comment); this deliberately does not attempt to fully describe that
// program, only to name its top-level effect kind(s), matching the "brief
// clause" bar the perk-authoring task set rather than duplicating
// describeAbilityProgram's much heavier full-program walk.
func describePerkAbilityRiders(riders []AbilityRider) string {
	if len(riders) == 0 {
		return ""
	}
	out := make([]string, 0, len(riders))
	for _, r := range riders {
		out = append(out, describeAbilityRiderClause(r))
	}
	return strings.Join(out, " ")
}

// describeAbilityRiderClause renders one rider. Falls back to the generic
// "an extra effect" when none of its actions are a recognized effect kind
// (riderEffectSummary returns "") — e.g. a rider whose Actions are purely
// targeting/context plumbing feeding a later, unrecognized action type.
func describeAbilityRiderClause(r AbilityRider) string {
	name := abilityDisplayNameOrID(r.Target)
	trigger := riderTriggerLabel(r.Trigger)
	if effect := riderEffectSummary(r.Actions); effect != "" {
		return fmt.Sprintf("Adds an extra %s effect to %s's %s.", effect, name, trigger)
	}
	return fmt.Sprintf("Adds an extra effect to %s's %s.", name, trigger)
}

// riderEffectLabels maps the ActionTypes that represent a player-visible
// "effect kind" (as opposed to targeting/context/presentation plumbing) to
// the word used in the generated clause. Deliberately small: only the
// action types that meaningfully change what a rider clause promises are
// listed here — an unlisted type (e.g. select_targets, store_targets,
// play_presentation) contributes no word, matching an authored ability's
// own tooltip never narrating its VFX-only actions either.
var riderEffectLabels = map[ActionType]string{
	ActionDealDamage:    "damage",
	ActionRestoreHealth: "healing",
	ActionApplyStatus:   "status",
	ActionRemoveStatus:  "status-removal",
	ActionSummonUnit:    "summon",
	ActionCreateZone:    "zone",
	ActionApplyForce:    "force",
}

// riderEffectSummary enumerates the DISTINCT recognized effect kinds among
// actions, in the actions' own authored order (first-seen wins the
// position), joined with "and". Disabled actions (Disabled: true) are
// skipped, matching the runtime (AbilityActionDef.IsEnabled). Returns "" when
// none of actions is a recognized effect kind.
func riderEffectSummary(actions []AbilityActionDef) string {
	seen := make(map[string]bool, len(actions))
	var words []string
	for _, act := range actions {
		if !act.IsEnabled() {
			continue
		}
		label, ok := riderEffectLabels[act.Type]
		if !ok || seen[label] {
			continue
		}
		seen[label] = true
		words = append(words, label)
	}
	return strings.Join(words, " and ")
}

// riderTriggerLabel turns a TriggerType id into inline prose ("on_tick"
// -> "beam tick").
func riderTriggerLabel(t TriggerType) string {
	return strings.ReplaceAll(strings.TrimPrefix(string(t), "on_"), "_", " ")
}

// ─────────────────────────────────────────────────────────────────────────────
// GrantsAbilities
// ─────────────────────────────────────────────────────────────────────────────

// describePerkGrantsAbilities renders "Grants: <name1>, <name2>." Returns ""
// for an empty list.
func describePerkGrantsAbilities(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	names := make([]string, len(ids))
	for i, id := range ids {
		names[i] = abilityDisplayNameOrID(id)
	}
	return fmt.Sprintf("Grants: %s.", strings.Join(names, ", "))
}

// ─────────────────────────────────────────────────────────────────────────────
// Shared helpers
// ─────────────────────────────────────────────────────────────────────────────

// abilityDisplayNameOrID resolves id's DisplayName via the read-only ability
// registry (getAbilityDef — the same lookup ability_modifiers.go/
// ability_riders.go use at cast time), falling back to a humanized,
// title-cased form of the id itself when the ability is unknown (an authored
// perk referencing a not-yet-created ability id, or a stale reference) so
// the generator degrades to readable prose instead of a raw snake_case id.
func abilityDisplayNameOrID(id string) string {
	if def, ok := getAbilityDef(id); ok && strings.TrimSpace(def.DisplayName) != "" {
		return def.DisplayName
	}
	return titleCaseWords(humanizeID(id))
}

// titleCaseWords upper-cases the first rune of every space-separated word in
// s ("skeleton soldier" -> "Skeleton Soldier"). ASCII-simple, matching
// capitalize's own simplicity (ids and generated prose are ASCII).
func titleCaseWords(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		words[i] = capitalize(w)
	}
	return strings.Join(words, " ")
}

// signedNumber formats v as a sign-prefixed, trimmed number: 90 -> "+90",
// -5 -> "-5", 0.3 -> "+0.3".
func signedNumber(v float64) string {
	if v < 0 {
		return "-" + trimFloat(-v)
	}
	return "+" + trimFloat(v)
}

// signedPercent formats delta (a fractional change, e.g. 0.15 for +15%) as a
// sign-prefixed whole-or-fractional percent: 0.15 -> "+15%", -0.5 -> "-50%".
// Rounds to 2 decimal places of PERCENT (i.e. 4 decimal places of delta)
// before trimming, which absorbs float64 representation noise from authored
// literals like 1.15 (whose exact delta from 1.0 is not exactly 0.15) without
// losing genuinely fractional percentages (e.g. 12.5%).
func signedPercent(delta float64) string {
	pct := math.Round(delta*100*100) / 100
	if pct < 0 {
		return "-" + trimFloat(-pct) + "%"
	}
	return "+" + trimFloat(pct) + "%"
}
