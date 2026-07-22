package game

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// ability_describe.go turns an AbilityDef's configured fields into player-facing
// tooltip prose. It is the SINGLE source of truth for "what does this ability
// do?" text: the in-match action-bar tooltip and the world-editor preview both
// render EffectiveDescription() rather than carrying their own hardcoded copy,
// so the two can never drift. An author who dislikes the generated wording sets
// AbilityDef.Description to override it verbatim (see the field doc).
//
// The generator is intentionally compositional: it selects a primary-effect
// clause from the fields that are set (channel / summon / charge-fire / damage /
// heal) and appends modifier clauses (area, chain, slow, burn, pull) for
// whichever secondary fields are non-zero. A new ability that reuses existing
// fields gets a reasonable sentence for free; a genuinely novel mechanic gets
// an empty string here and is expected to author Description directly.

// EffectiveDescription resolves the ability's tooltip prose: the author's
// Description override when set, otherwise the generated text. This is what the
// snapshot and editor preview should call — never describeAbility directly,
// which ignores the override.
func (a AbilityDef) EffectiveDescription() string {
	if s := strings.TrimSpace(a.Description); s != "" {
		return s
	}
	return describeAbility(a)
}

// GeneratedDescription returns the prose generated purely from a's configured
// fields, IGNORING any Description override. The world-editor uses it to show
// the author what the auto-generated text would be (as the default and the
// "reset to generated" target) even while an override is in effect. Runtime
// callers want EffectiveDescription instead.
func (a AbilityDef) GeneratedDescription() string { return describeAbility(a) }

// describeAbility builds the generated tooltip prose from a's configured fields,
// ignoring any Description override (callers wanting the override should use
// EffectiveDescription). Returns "" when no modeled effect is present.
//
// Composable (schemaVersion>=2) abilities route through describeAbilityProgram
// instead of reading the (deliberately cleared, per ConvertLegacyAbility)
// legacy mechanic fields directly below. This MUST use the same branch
// condition as the cast-time authority split (resolveAbilityCastLocked,
// ability_cast.go) so description and behavior never read from different
// sources for the same ability.
func describeAbility(a AbilityDef) string {
	if a.SchemaVersion >= 2 && a.Program != nil {
		// Prose is generated WITHOUT a caster, so it describes what the ability
		// does on its own — the authored numbers, before any perk/item/rank fold.
		// That works directly now: an action's config holds literal numbers. It
		// used to require a pre-pass resolving "$name" references, without which
		// every parameterized field reached the prose builders as an unparsed
		// string and the tooltip degraded to blanks.
		return describeAbilityProgram(a)
	}
	return describeLegacyAbility(a)
}

// NOTE: describeAbility used to pre-resolve "$name" parameter references to
// their declared bases before generating prose, because an action's config held
// the STRING "$dps" rather than a number. Actions now hold literal numbers
// (ability_field_mods.go), so the substitution pass is gone and prose reads the
// config directly.

// describeLegacyAbility is the flat-field generator: every legacy
// (schemaVersion 0/1) ability's tooltip prose is built here, unchanged from
// before composable abilities existed.
func describeLegacyAbility(a AbilityDef) string {
	switch {
	case a.ChannelType != "":
		return describeChannelAbility(a)
	case a.IsChargeFirePassive():
		return describeChargeFireAbility(a)
	case a.SummonUnitType != "":
		return describeSummonAbility(a)
	default:
		return describeEffectAbility(a)
	}
}

// ═════════════════════════════════════════════════════════════════════════
// COMPOSABLE (schemaVersion>=2) PROSE — walk the Program, not flat fields
// ═════════════════════════════════════════════════════════════════════════
//
// describeAbilityProgram recovers the same magnitudes describeLegacyAbility's
// helpers read (DamageAmount, HealAmount, Radius, SlowMultiplier, ...) by
// walking a's compiled Program, then hands a "shadow" AbilityDef (a's
// cast-setup fields, which survive conversion untouched, PLUS the recovered
// mechanic fields) to describeLegacyAbility so the exact same formatting code
// runs either way. This is what makes tooltip prose provably unchanged across
// the legacy->composable migration: describeLegacyAbility(legacyDef) ==
// describeAbility(convertedDef) is a byte-for-byte comparison of the same
// function's output, not two independently-maintained string builders.
//
// Every mechanic family produced by compileLegacyAbility's compilers can be
// recovered this way: heal / multi-heal, instant and point-AoE damage (with
// optional slow rider), delayed-impact + burn zone (meteor), summon,
// charge-fire passive (arcane_missiles, whose compileChargeFireAction bakes
// its full spec into a charge_fire_volley action's config — see the
// ActionChargeFireVolley case below), and channel (siphon_life, whose
// compileChannelBeamAction bakes its full spec into a channel_beam action's
// config — see the channeled ActionBeam case below, and channelSpecFor's
// identical recovery for the RUNTIME side).
func describeAbilityProgram(a AbilityDef) string {
	// on_damage_dealt + restore_health(amountRef) — the "self-heal a fraction
	// of damage just dealt" reactive-passive shape (blood_sustain, the first
	// migrated triggered perk; more follow). This has no legacy-field
	// equivalent to recover (restore_health's AmountRef path deliberately
	// carries no static HealAmount — see restoreHealthConfig's doc comment),
	// so it is described directly from the Program rather than routed through
	// abilityMechanicsShadow's legacy-field recovery.
	if desc := describeOnDamageDealtLifestealAbility(a); desc != "" {
		return desc
	}
	if desc := describeStatModifierOnlyAbility(a); desc != "" {
		return desc
	}
	if desc := describeTrapAbility(a); desc != "" {
		return desc
	}
	if desc := describeVisibleZoneAbility(a); desc != "" {
		return desc
	}
	return describeLegacyAbility(abilityMechanicsShadow(a))
}

// describeVisibleZoneAbility describes an ability whose effect is a persistent
// VISIBLE zone — a create_zone that names a sprite (the shape the Trapper's
// traps are authored in). The generic shadow-recovery path cannot describe
// these: a zone's damage lives inside its nested on_zone_tick trigger, and
// under this design that damage is typically behind a capability branch, so
// there is no flat mechanic field to recover.
//
// Prose describes the DEFAULT behavior — the branch a caster with no
// contributing source gets — then lists the ability's declared variants using
// each capability's own `describe` text, per
// docs/design/ability_perk_interaction.md §10 ("an ability should describe its
// default branch and note its variants").
//
// Returns "" for any program with no visible create_zone, so every other
// ability keeps its existing description path untouched.
func describeVisibleZoneAbility(a AbilityDef) string {
	cfg, ok := findVisibleZoneConfig(a.Program)
	if !ok {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Places %s", withIndefiniteArticle(zoneDisplayNoun(a, cfg)))
	if cfg.Radius > 0 {
		fmt.Fprintf(&b, " (%s radius)", trimFloat(cfg.Radius))
	}
	if amount, dmgType, interval, found := firstZoneTickDamage(cfg); found {
		word := damageTypeWord(dmgType)
		if interval == 1 {
			fmt.Fprintf(&b, " that deals %s%s damage per second", trimFloat(amount), word)
		} else if interval > 0 {
			fmt.Fprintf(&b, " that deals %s%s damage every %ss", trimFloat(amount), word, trimFloat(interval))
		} else {
			fmt.Fprintf(&b, " that deals %s%s damage", trimFloat(amount), word)
		}
	}
	if cfg.Duration > 0 {
		fmt.Fprintf(&b, ". Lasts %ss", trimFloat(cfg.Duration))
	}
	b.WriteString(".")

	// Variant branches (a conditional on has_perk) are deliberately NOT listed
	// here: the branch names its perk inline in the program, and the PERK's own
	// description is where "what Lasting Flames does to your fire pit" belongs.
	// Describing it from both sides would drift.
	return b.String()
}

// zoneDisplayNoun is the noun the prose calls the zone: its authored zone name
// when present, else the ability's display name, lowercased so it reads inside
// a sentence.
func zoneDisplayNoun(a AbilityDef, cfg createZoneConfig) string {
	if n := strings.TrimSpace(cfg.Name); n != "" {
		return strings.ToLower(n)
	}
	if a.DisplayName != "" {
		return strings.ToLower(a.DisplayName)
	}
	return humanizeID(a.ID)
}

// findVisibleZoneConfig walks a program for the first create_zone action that
// opted into visibility, recursing through nested action children.
func findVisibleZoneConfig(prog *AbilityProgram) (createZoneConfig, bool) {
	if prog == nil {
		return createZoneConfig{}, false
	}
	var walk func(triggers []AbilityTriggerDef) (createZoneConfig, bool)
	walk = func(triggers []AbilityTriggerDef) (createZoneConfig, bool) {
		for _, trg := range triggers {
			for _, action := range trg.Actions {
				if action.Type == ActionCreateZone {
					var c createZoneConfig
					decodeActionConfig(action.Config, &c)
					if c.Sprite != "" {
						return c, true
					}
				}
				if c, ok := walk(action.Children); ok {
					return c, true
				}
			}
		}
		return createZoneConfig{}, false
	}
	return walk(prog.Triggers)
}

// firstZoneTickDamage finds the damage a zone deals on tick, looking through
// conditional branches so a capability-gated default branch still describes.
// The FIRST deal_damage encountered wins, which is why an ability should author
// its default branch first — the same convention the rest of the describe code
// uses for "primary" effects.
func firstZoneTickDamage(cfg createZoneConfig) (amount float64, dmgType string, interval float64, found bool) {
	var scan func(actions []AbilityActionDef) (float64, string, bool)
	scan = func(actions []AbilityActionDef) (float64, string, bool) {
		for _, act := range actions {
			switch act.Type {
			case ActionDealDamage:
				var dc dealDamageConfig
				decodeActionConfig(act.Config, &dc)
				if dc.Amount > 0 {
					return float64(dc.Amount), string(dc.Type), true
				}
			case ActionConditional:
				var cc conditionalConfig
				decodeActionConfig(act.Config, &cc)
				if amt, t, ok := scan(cc.Then); ok {
					return amt, t, ok
				}
			}
		}
		return 0, "", false
	}
	for _, trg := range cfg.Triggers {
		if trg.Type != TriggerOnTick {
			continue
		}
		if amt, t, ok := scan(trg.Actions); ok {
			return amt, t, cfg.TickInterval, true
		}
	}
	return 0, "", 0, false
}

// damageTypeWord renders a damage type as a leading-space-prefixed adjective
// ("fire" -> " fire"), or "" when unset, so callers can splice it into prose
// without worrying about double spaces.
func damageTypeWord(t string) string {
	if t == "" || t == string(DamagePhysical) {
		return ""
	}
	return " " + t
}

// describeTrapAbility recognizes a program whose gameplay effect is a
// place_trap action — the Trapper's four traps (caltrops, fire_pit,
// explosive_trap, marker_trap), which became pool abilities when the bronze
// trap perks were retired. place_trap has no legacy AbilityDef field family to
// recover into, so (exactly like describeOnDamageDealtLifestealAbility and
// describeStatModifierOnlyAbility above) it is described straight from the
// Program instead of routing through abilityMechanicsShadow.
//
// Clauses are emitted only for the stats a given trap actually authors, so one
// generic builder covers all four trap shapes with no per-trap-type switch —
// a new trap authored with the same config vocabulary describes itself for
// free. Returns "" for any program with no place_trap action.
func describeTrapAbility(a AbilityDef) string {
	cfg, ok := findPlaceTrapConfig(a.Program)
	if !ok {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Places %s", withIndefiniteArticle(humanizeID(cfg.TrapType)))
	// Zone size: explosive_trap authors explosionRadius, the others radius.
	radius := cfg.Radius
	if radius == 0 {
		radius = cfg.ExplosionRadius
	}
	if radius > 0 {
		fmt.Fprintf(&b, " (%s radius)", trimFloat(radius))
	}
	var effects []string
	if cfg.DamagePerSecond > 0 {
		effects = append(effects, fmt.Sprintf("deals %s damage per second", trimFloat(cfg.DamagePerSecond)))
	}
	if cfg.SlowMultiplier > 0 && cfg.SlowMultiplier < 1 {
		effects = append(effects, fmt.Sprintf("slows enemies to %s%% of their speed", trimFloat(cfg.SlowMultiplier*100)))
	}
	if cfg.BurstDamage > 0 {
		clause := fmt.Sprintf("detonates for %s damage", trimFloat(cfg.BurstDamage))
		if cfg.TriggerRadius > 0 {
			clause += fmt.Sprintf(" when an enemy comes within %s", trimFloat(cfg.TriggerRadius))
		}
		effects = append(effects, clause)
	}
	if cfg.MarkMultiplier > 0 {
		clause := fmt.Sprintf("marks enemies to take %s damage from all sources", signedPercent(cfg.MarkMultiplier))
		if cfg.MarkDuration > 0 {
			clause += fmt.Sprintf(" for %ss", trimFloat(cfg.MarkDuration))
		}
		effects = append(effects, clause)
	}
	if len(effects) > 0 {
		b.WriteString(" that " + joinTrapEffectClauses(effects))
	}
	if cfg.DurationSeconds > 0 {
		fmt.Fprintf(&b, ". Lasts %ss", trimFloat(cfg.DurationSeconds))
	}
	b.WriteString(".")
	return b.String()
}

// withIndefiniteArticle prefixes a noun phrase with "a"/"an", or leaves it bare
// when it reads as a plural (trailing "s"). Keeps generated trap prose
// grammatical across trap names without a per-trap-type table: "a fire pit",
// "an explosive trap", but "caltrops" (already plural).
func withIndefiniteArticle(noun string) string {
	if noun == "" {
		return noun
	}
	if strings.HasSuffix(noun, "s") {
		return noun
	}
	switch noun[0] {
	case 'a', 'e', 'i', 'o', 'u', 'A', 'E', 'I', 'O', 'U':
		return "an " + noun
	}
	return "a " + noun
}

// joinTrapEffectClauses renders 1..n trap effect clauses as readable prose:
// "a", "a and b", "a, b and c".
func joinTrapEffectClauses(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
	}
}

// describeStatModifierOnlyAbility recognizes a program whose ENTIRE gameplay
// effect is "select some targets, then apply_status_duration whose
// config.triggers nest one or more change_stat actions" — no damage, no
// heal, no summon, no projectile/beam/zone (mark_of_weakness, the pilot
// ability for this shape — the status-carried-stat-modifiers mechanic has no
// legacy-field equivalent to recover, exactly like
// describeOnDamageDealtLifestealAbility's blood_sustain case above). Returns
// "" for every other program, so every mechanic family that includes a
// damage/heal/summon/projectile/beam/zone action anywhere in its top-level
// triggers keeps using abilityMechanicsShadow's flat-field recovery
// unchanged — the bail-out is deliberately broad (any of those action types
// anywhere disqualifies the whole ability) rather than narrowly scoped to
// "the same trigger as the apply_status_duration", so this can never misfire
// on a damage/heal ability that also happens to apply a stat-modifying
// status (e.g. a future "deals damage AND weakens armor" spell) — that case
// falls through to the normal shadow-recovery/legacy-clause path exactly
// like a slow-on-hit ability, keeping this function's blast radius = zero
// for every ability that isn't this exact pure-debuff shape. apply_mark
// (the icon-channel sibling nested alongside change_stat) contributes no
// clause of its own — it's cosmetic, not a describable magnitude.
func describeStatModifierOnlyAbility(a AbilityDef) string {
	if a.Program == nil {
		return ""
	}
	var (
		mods     []PerkStatModifier
		duration float64
		radius   float64
		found    bool
	)
	for _, trig := range a.Program.Triggers {
		var pendingRadius float64
		for _, act := range trig.Actions {
			if !act.IsEnabled() {
				continue
			}
			switch act.Type {
			case ActionSelectTargets:
				if act.Target != nil {
					pendingRadius = act.Target.Radius
				}
			case ActionDealDamage, ActionRestoreHealth, ActionSummonUnit,
				ActionLaunchProjectile, ActionBeam, ActionChargeFireVolley,
				ActionCreateZone, ActionLoop, ActionPlaceTrap:
				// Any of these means this is NOT a pure stat-modifier ability
				// — bail immediately so the normal recovery path handles it.
				return ""
			case ActionApplyStatusDuration:
				var cfg applyStatusDurationConfig
				decodeActionConfig(act.Config, &cfg)
				nested, bail := collectNestedChangeStatMods(cfg.Triggers)
				if bail {
					return ""
				}
				if len(nested) == 0 {
					continue
				}
				mods = nested
				duration = cfg.Duration
				radius = pendingRadius
				found = true
			}
		}
	}
	if !found || len(mods) == 0 {
		return ""
	}

	// Reuse the SAME describeStatModifierClause the perk-aura/StatModifiers
	// tooltip prose uses (perk_describe.go) — one formatting function for
	// "here is a PerkStatModifier as English" regardless of which of the
	// three emitters (perk / aura / status) authored it, matching this
	// task's "one shared vocabulary, one validation bar" precedent for
	// Validate. Stage defaults to statStageBase exactly like that call site.
	clauses := make([]string, 0, len(mods))
	for _, m := range mods {
		stage := m.Stage
		if stage == "" {
			stage = statStageBase
		}
		clauses = append(clauses, describeStatModifierClause(m, stage))
	}
	scope := "the target"
	if radius > 0 {
		scope = fmt.Sprintf("all enemies within %s units of the target", trimFloat(radius))
	}
	sentence := fmt.Sprintf("Afflicts %s with %s", scope, strings.Join(clauses, ", "))
	if duration > 0 {
		sentence += fmt.Sprintf(" for %ss", trimFloat(duration))
	}
	return sentence + "."
}

// collectNestedChangeStatMods walks an apply_status_duration's config.triggers
// (compiled on_action_complete triggers, ability_status_duration.go) and
// collects every nested change_stat action into a PerkStatModifier, in
// authored order. apply_mark is recognized and skipped (cosmetic, no
// magnitude to recover). Any OTHER nested action type sets bail=true — the
// caller (describeStatModifierOnlyAbility) treats that identically to one of
// its own top-level disqualifying action types, so a future
// apply_status_duration that also, say, deals damage inside its
// on_action_complete never gets miscategorized as a pure stat-modifier
// ability.
func collectNestedChangeStatMods(triggers []AbilityTriggerDef) (mods []PerkStatModifier, bail bool) {
	for _, trig := range triggers {
		for _, act := range trig.Actions {
			if !act.IsEnabled() {
				continue
			}
			switch act.Type {
			case ActionChangeStat:
				var cfg changeStatConfig
				decodeActionConfig(act.Config, &cfg)
				mods = append(mods, PerkStatModifier{Stat: cfg.Stat, Op: cfg.Op, Value: cfg.Value, Stage: cfg.Stage})
			case ActionApplyMark:
				// Cosmetic only — contributes no describable magnitude.
			default:
				return nil, true
			}
		}
	}
	return mods, false
}

// describeOnDamageDealtLifestealAbility recognizes a program whose top-level
// triggers include an on_damage_dealt trigger with an enabled restore_health
// action that names an AmountRef (a bound runtime scalar, not a static
// amount) targeting the caster. Returns "" for every ability that does not
// match this exact shape, so every other mechanic family keeps using the
// shadow-recovery path unchanged.
func describeOnDamageDealtLifestealAbility(a AbilityDef) string {
	if a.Program == nil {
		return ""
	}
	for _, trig := range a.Program.Triggers {
		if trig.Type != TriggerOnDamageDealt {
			continue
		}
		for _, act := range trig.Actions {
			if !act.IsEnabled() || act.Type != ActionRestoreHealth {
				continue
			}
			var cfg restoreHealthConfig
			decodeActionConfig(act.Config, &cfg)
			if cfg.AmountRef == "" {
				continue
			}
			mult := cfg.AmountMult
			if mult == 0 {
				mult = 1
			}
			pct := int(math.Round(mult * 100))
			return fmt.Sprintf("Heals the caster for %d%% of the damage dealt%s.", pct, describeDamageScopeFragment(trig.DamageScope))
		}
	}
	return ""
}

// describeDamageScopeFragment renders an on_damage_dealt trigger's
// DamageScope as an inline qualifier (" by basic attacks", ...). A nil/empty
// scope (any damage) renders as "" so the sentence reads "... damage dealt."
func describeDamageScopeFragment(scope *DamageTriggerScope) string {
	if scope == nil || len(scope.Categories) == 0 {
		return ""
	}
	words := make([]string, 0, len(scope.Categories))
	for _, c := range scope.Categories {
		words = append(words, damageCategoryPluralWord(c))
	}
	return " by " + strings.Join(words, " or ")
}

// damageCategoryPluralWord renders a DamageCategory as an inline plural noun
// phrase for tooltip prose ("basic attacks", "abilities", ...).
func damageCategoryPluralWord(c DamageCategory) string {
	switch c {
	case DamageCategoryBasicAttack:
		return "basic attacks"
	case DamageCategoryAbility:
		return "abilities"
	case DamageCategoryTrap:
		return "traps"
	case DamageCategoryBuilding:
		return "buildings"
	case DamageCategoryPerk:
		return "perk hits"
	case DamageCategoryItem:
		return "item procs"
	default:
		return string(c)
	}
}

// abilityMechanicsShadow recovers a's mechanic magnitudes (HealAmount,
// DamageAmount, Radius, SlowMultiplier, SummonCount, Burn*, ...) by walking
// its compiled Program and returns a's cast-setup fields (targeting, cost/
// timing, presentation hooks — everything that survives conversion
// untouched) PLUS those recovered magnitudes on a fresh AbilityDef with
// SchemaVersion reset to 0 and Program cleared. For a legacy (SchemaVersion<2
// or Program nil) def it is the identity function.
//
// This is the single recovery seam: describeAbilityProgram uses it to build
// tooltip prose with describeLegacyAbility's formatting code, and test code
// that needs "the effective HealAmount/DamageAmount/etc. of this catalog
// ability" — regardless of whether it is still legacy-shaped or has been
// migrated to a schemaVersion:2 Program — calls it too, rather than reading a
// migrated ability's now-cleared flat fields directly (which would silently
// read 0 and produce a vacuous expectation). See
// TestDescribeAbility_ProgramEquivalence for the byte-for-byte prose proof
// this recovery is faithful, and ability_legacy_fixtures_test.go's doc
// comment for why frozen fixtures — not this shadow — are what golden
// equivalence tests compare the executor against.
func abilityMechanicsShadow(a AbilityDef) AbilityDef {
	if a.SchemaVersion < 2 || a.Program == nil {
		return a
	}
	shadow := a
	shadow.SchemaVersion = 0
	shadow.Program = nil

	m := &programMechanics{program: a.Program}
	m.walkTriggers(a.Program.Triggers, false)
	for _, pres := range a.Program.Presentations {
		m.walkTriggers(pres.Triggers, false)
	}
	if len(a.Program.NamedTriggers) > 0 {
		// NamedTriggers is a map: sort keys so walk order (and therefore which
		// "first primary deal_damage wins" tie-break fires) is deterministic
		// regardless of Go's randomized map iteration.
		keys := make([]string, 0, len(a.Program.NamedTriggers))
		for k := range a.Program.NamedTriggers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			m.walkTrigger(a.Program.NamedTriggers[k], false)
		}
	}
	m.applyTo(&shadow)

	return shadow
}

// programMechanics accumulates the mechanic magnitudes recovered by walking a
// composable AbilityProgram. Each field mirrors a legacy AbilityDef field the
// describe* helpers read; a zero value means "not found while walking",
// exactly like an unset legacy field.
type programMechanics struct {
	damageAmount int
	radius       float64 // AoE radius for the PRIMARY (non-zone) deal_damage / launch_projectile splash
	healAmount   int
	targetCount  int

	// chainCount / bounceDamageFalloff are recovered from a launch_projectile
	// action (chain_lightning's compiled shape) — see describeDamageClause,
	// which reads a.ChainCount / a.BounceDamageFalloff to build the "arcs to
	// N more enemies" clause.
	chainCount          int
	bounceDamageFalloff int
	// projectile / projectileScale / minorDamage are recovered from a
	// launch_projectile action's config (arcane_bolt/fireball/chain_lightning's
	// compiled shape). Not read by any describe* prose helper today (the
	// generated tooltip never names the projectile asset), but recovered here
	// anyway so non-prose callers of abilityMechanicsShadow — e.g. a test
	// wanting "the effective Projectile id of this catalog ability" — get the
	// right answer post-migration too, matching the other mechanic fields'
	// recovery discipline.
	projectile      string
	projectileScale float64
	minorDamage     bool

	summonUnitType string
	summonCount    int

	slowMultiplier      float64
	slowDurationSeconds float64

	// damagePerSecond / pullStrength are recovered from a launch_projectile
	// action's config with TickInterval>0 (arcane_orb's ticking-vortex shape
	// — see compileTickingProjectileActions). radius/projectile/
	// projectileScale above are shared with launch_projectile's other shapes'
	// recovery (mutually exclusive per program, same "first primary wins"
	// guard).
	damagePerSecond float64
	pullStrength    float64

	burnDurationSeconds     float64
	burnDamagePerTick       int
	burnTickIntervalSeconds float64
	burnEffectAtPoint       string // create_zone's lingering Presentation (meteor's burning_crater)

	effectOnTarget string  // play_presentation attached to a resolved target set (heal's healing_glow)
	effectAtPoint  string  // play_presentation anchored to a world position (shatter's/meteor's burst)
	effectScale    float64 // scale carried by whichever of the above was found

	// chargeRequired / manaToChargeRatio / missileCount / damagePerMissile /
	// missileDelayMs / chargeTargeting / chargeAllowDuplicateTargets are
	// recovered from a charge_fire_volley action's config (arcane_missiles'
	// compiled shape — see compileChargeFireAction). projectile /
	// projectileScale / minorDamage above are shared with launch_projectile's
	// recovery (both shapes): a charge-fire program has no primary-damage
	// action, so sawPrimaryDamage never gates this branch.
	chargeRequired              float64
	manaToChargeRatio           float64
	missileCount                int
	damagePerMissile            int
	missileDelayMs              float64
	chargeTargeting             string
	chargeAllowDuplicateTargets bool

	// channelType / channelTickIntervalSeconds / channelManaCostPerTick /
	// channelDamagePerTick / channelHealingMultiplier / channelAllyHealRadius
	// are recovered from a channel_beam action's config (siphon_life's
	// compiled shape — see compileChannelBeamAction). channelType doubles as
	// the "was a channel recovered at all" flag applyTo checks, since
	// describeLegacyAbility's dispatch switch keys on a.ChannelType != ""
	// (a discriminator string, unlike every other mechanic family's own
	// nonzero magnitude).
	channelType                string
	channelTickIntervalSeconds float64
	channelManaCostPerTick     int
	channelDamagePerTick       int
	channelHealingMultiplier   float64
	channelAllyHealRadius      float64

	sawPrimaryDamage bool // first non-zone deal_damage wins (matches one-hit-per-cast shape)

	// program is the AbilityProgram being walked, set by abilityMechanicsShadow
	// so recoverChainLightningLoop can follow a beam's on_beam_impact
	// trigger_event handoff into the named `bounce` trigger. Nil for callers
	// that build a programMechanics without a program in hand (the loop
	// recovery then no-ops and the legacy nested-beam recovery still runs).
	program *AbilityProgram
}

// walkTriggers walks each trigger's action list. inZone is true while walking
// a create_zone action's own nested Triggers (a zone-tick's deal_damage is the
// BURN tick, not the primary hit — see the ActionDealDamage case below).
func (m *programMechanics) walkTriggers(trigs []AbilityTriggerDef, inZone bool) {
	for _, t := range trigs {
		m.walkTrigger(t, inZone)
	}
}

func (m *programMechanics) walkTrigger(t AbilityTriggerDef, inZone bool) {
	var pendingRadius float64
	var pendingTargetCount int
	havePending := false

	for _, act := range t.Actions {
		if !act.IsEnabled() {
			continue
		}
		switch act.Type {
		case ActionSelectTargets:
			if act.Target != nil {
				pendingRadius = act.Target.Radius
				pendingTargetCount = act.Target.MaxCount
				havePending = true
			}
		case ActionDealDamage:
			var cfg dealDamageConfig
			decodeActionConfig(act.Config, &cfg)
			if inZone {
				m.burnDamagePerTick = cfg.Amount
			} else if !m.sawPrimaryDamage {
				m.damageAmount = cfg.Amount
				if havePending {
					m.radius = pendingRadius
				}
				m.sawPrimaryDamage = true
			}
		case ActionBeam:
			var cfg beamConfig
			decodeActionConfig(act.Config, &cfg)
			if cfg.Channeled {
				// siphon_life's channel-start beam.
				m.channelType = cfg.ChannelType
				m.channelTickIntervalSeconds = cfg.TickIntervalSeconds
				m.channelManaCostPerTick = cfg.ManaCostPerTick
				m.channelDamagePerTick = cfg.DamagePerTick
				m.channelHealingMultiplier = cfg.HealingMultiplier
				m.channelAllyHealRadius = cfg.AllyHealRadius
				break
			}
			// A direct momentary beam is the LEGACY unrolled chain ladder
			// (compileChainLightningActions) — recovered by recoverChainLightningBeam.
			// The current chain_lightning form has its beam INSIDE a loop action's
			// body instead, recovered by the ActionLoop case below.
			if m.sawPrimaryDamage {
				break
			}
			m.projectile = cfg.Variant
			m.sawPrimaryDamage = true
			m.recoverChainLightningBeam(cfg, 0)
		case ActionLoop:
			var cfg loopConfig
			decodeActionConfig(act.Config, &cfg)
			m.recoverLoop(cfg)
		case ActionLaunchProjectile:
			// arcane_bolt / fireball / arcane_orb's compiled shape (a single
			// launch_projectile action with its own Target query, no preceding
			// select_targets — see compileProjectileActions /
			// compileTickingProjectileActions). chain_lightning used to share
			// this shape too (the pre-redesign ChainCount>0 shim); it is now
			// recovered via the ActionBeam case above instead.
			var cfg launchProjectileConfig
			decodeActionConfig(act.Config, &cfg)
			if m.sawPrimaryDamage {
				break
			}
			m.projectile = cfg.Projectile
			m.projectileScale = cfg.ProjectileScale
			if cfg.TickInterval > 0 {
				// arcane_orb's ticking-vortex shim: its magnitudes live SOLELY
				// in the nested on_projectile_tick trigger (see
				// launchProjectileConfig's TickInterval doc comment — the
				// genuine-composition fix removed the top-level sibling
				// scalars this used to read, since Execute never consulted
				// them for anything but a copy of the SAME nested trigger's
				// numbers). Recover via the SAME vortexMagnitudesFromTrigger
				// reader Execute/Validate use (ability_exec_projectile.go), so
				// this description and the live executor can never drift on
				// what these numbers mean. damagePerSecond is recovered as the
				// authored per-tick chunk divided back by TickInterval — the
				// raw per-second rate, matching the legacy def's own
				// DamagePerSecond field (this recovery reads the AUTHORED
				// baseline, never a live-cast-folded value, same as every
				// other magnitude here).
				for _, trig := range cfg.Triggers {
					if trig.Type != TriggerOnTick {
						continue
					}
					vm := vortexMagnitudesFromTrigger(trig)
					m.radius = vm.radius
					m.pullStrength = vm.pullStrength
					if cfg.TickInterval > 0 {
						m.damagePerSecond = float64(vm.damageAmount) / cfg.TickInterval
					}
					break
				}
				m.sawPrimaryDamage = true
				break
			}
			// arcane_bolt / fireball's migrated shape: damage (+ splash) now
			// lives in the nested on_projectile_impact trigger's
			// select_targets/deal_damage pair (compileProjectileImpactTrigger) —
			// recurse into it exactly like create_zone's on_zone_tick recursion
			// below, reusing the SAME select_targets/deal_damage recovery
			// (pendingRadius -> m.radius) so fireball's splash radius comes back
			// out correctly.
			for _, trig := range cfg.Triggers {
				if trig.Type == TriggerOnProjectileImpact {
					m.walkTrigger(trig, false)
				}
			}
		case ActionRestoreHealth:
			var cfg restoreHealthConfig
			decodeActionConfig(act.Config, &cfg)
			m.healAmount = cfg.Amount
			if havePending {
				m.targetCount = pendingTargetCount
			}
		case ActionApplyStatus:
			// Standalone apply_status (legacy compiler output). Both "slow" and
			// "chill" are the same recoverable magnitude to the tooltip — a
			// move/attack slow of some multiplier for some duration; "chill" is
			// simply the cold-track (icy-overlay) variant.
			var cfg applyStatusConfig
			decodeActionConfig(act.Config, &cfg)
			if cfg.Status == "slow" || cfg.Status == "chill" {
				m.slowMultiplier = cfg.Multiplier
				m.slowDurationSeconds = cfg.Duration
			}
		case ActionApplyStatusDuration:
			// Decomposed shape (Shatter's authored form): the slow lives inside
			// the container's config.triggers, and the container — not the nested
			// action — owns the duration. Two authoring idioms recover to the same
			// tooltip magnitude:
			//   (1) a nested apply_status(chill|slow) — the legacy CC primitive,
			//       still used by other abilities;
			//   (2) a nested change_stat that MULTIPLIES moveSpeed — the
			//       chill-as-composition form Shatter migrated to (paired with a
			//       change_stat on attackSpeed + apply_color_overlay). The moveSpeed
			//       multiply IS the slow; recovering it reproduces the exact
			//       "slows by X%" prose the old apply_status(chill) produced.
			// The attackSpeed multiply and the color overlay are deliberately not
			// re-described as a second clause — one slow magnitude, as before.
			var cfg applyStatusDurationConfig
			decodeActionConfig(act.Config, &cfg)
			for _, trig := range cfg.Triggers {
				for _, nested := range trig.Actions {
					if !nested.IsEnabled() {
						continue
					}
					switch nested.Type {
					case ActionApplyStatus:
						var sc applyStatusConfig
						decodeActionConfig(nested.Config, &sc)
						if sc.Status == "slow" || sc.Status == "chill" {
							m.slowMultiplier = sc.Multiplier
							m.slowDurationSeconds = cfg.Duration
						}
					case ActionChangeStat:
						var csc changeStatConfig
						decodeActionConfig(nested.Config, &csc)
						if csc.Stat == statMoveSpeed && csc.Op == statOpMultiply {
							m.slowMultiplier = csc.Value
							m.slowDurationSeconds = cfg.Duration
						}
					}
				}
			}
		case ActionSummonUnit:
			var cfg summonUnitConfig
			decodeActionConfig(act.Config, &cfg)
			m.summonUnitType = cfg.UnitType
			m.summonCount = cfg.Count
		case ActionChargeFireVolley:
			var cfg chargeFireVolleyConfig
			decodeActionConfig(act.Config, &cfg)
			m.chargeRequired = cfg.ChargeRequired
			m.manaToChargeRatio = cfg.ManaToChargeRatio
			m.missileCount = cfg.MissileCount
			m.damagePerMissile = cfg.DamagePerMissile
			m.missileDelayMs = cfg.MissileDelayMs
			m.chargeTargeting = cfg.Targeting
			m.chargeAllowDuplicateTargets = cfg.AllowDuplicateTargets
			if cfg.Projectile != "" {
				m.projectile = cfg.Projectile
				m.projectileScale = cfg.ProjectileScale
				m.minorDamage = cfg.MinorDamage
			}
		case ActionCreateZone:
			var cfg createZoneConfig
			decodeActionConfig(act.Config, &cfg)
			m.burnDurationSeconds = cfg.Duration
			m.burnTickIntervalSeconds = cfg.TickInterval
			m.burnEffectAtPoint = cfg.Presentation
			m.walkTriggers(cfg.Triggers, true)
		case ActionPlayPresentation:
			// Both compiled shapes (playPresentationOnTargetConfig for heal's
			// on-target VFX, playPresentationAtPointConfig for shatter's/
			// meteor's world-anchored burst) decode into this fuller struct —
			// see ability_compile.go's playPresentationOnTargetConfig doc
			// comment. An "attach" input key is how the compiler marks the
			// on-target shape (compileHealActions); its absence means a
			// point-anchored effect (compileShatterActions/compileMeteorActions).
			var cfg playPresentationAtPointConfig
			decodeActionConfig(act.Config, &cfg)
			if _, onTarget := act.Input["attach"]; onTarget {
				m.effectOnTarget = cfg.Asset
			} else {
				m.effectAtPoint = cfg.Asset
			}
			m.effectScale = cfg.Scale
		}
		for _, child := range act.Children {
			m.walkTrigger(child, inZone)
		}
	}
}

// recoverChainLightningBeam recurses through the nested launch_beam/
// on_beam_impact ladder compileChainLightningActions (ability_compile.go)
// builds — hop 0 is the primary hit, hop 1..ChainCount are bounces, each
// level's on_beam_impact carrying a deal_damage(+store_targets+select_targets
// +launch_beam) shape (the +store/+select/+launch trio only present when a
// deeper hop exists). Recovers:
//   - damageAmount: hop 0's deal_damage Amount. Every hop's Amount is the
//     SAME raw def.DamageAmount (see dealDamageConfig.FlatOffset's doc
//     comment for why the compiler bakes the per-hop reduction as an offset
//     rather than a smaller Amount), so only hop 0's needs reading.
//   - bounceDamageFalloff: hop 1's FlatOffset negated. FlatOffset at hop k is
//     -(BounceDamageFalloff*k), so -FlatOffset at hop 1 IS
//     BounceDamageFalloff directly.
//   - chainCount: the deepest hop level the program actually compiled to
//     (compileChainLightningHop stops early once a hop's damage would be
//     non-positive, so this can be < the original def.ChainCount for an
//     extreme-falloff ability, matching the compiled behavior rather than a
//     possibly-stale intent).
func (m *programMechanics) recoverChainLightningBeam(cfg beamConfig, hop int) {
	for _, trig := range cfg.Triggers {
		if trig.Type != TriggerOnBeamImpact {
			continue
		}
		for _, act := range trig.Actions {
			switch act.Type {
			case ActionDealDamage:
				var dc dealDamageConfig
				decodeActionConfig(act.Config, &dc)
				switch hop {
				case 0:
					m.damageAmount = dc.Amount
				case 1:
					m.bounceDamageFalloff = -dc.FlatOffset
				}
			case ActionBeam:
				m.chainCount = hop + 1
				var nested beamConfig
				decodeActionConfig(act.Config, &nested)
				m.recoverChainLightningBeam(nested, hop+1)
			}
		}
	}
}

// recoverLoop recovers chain mechanics from a `loop` action (chain_lightning's
// shape: the loop is the BOUNCES — the primary hit is a direct deal_damage
// before the loop, recovered by walkTrigger's own ActionDealDamage case).
// Recovers:
//   - chainCount: Iterations bounces beyond the primary — but only when the
//     body actually chains (has both a select_targets and a beam), so a
//     non-chain loop gets no spurious "arcs to N enemies" clause.
//   - bounceDamageFalloff: -Step of the body deal_damage's amount variable.
//   - damageAmount: only as a FALLBACK — the primary is normally recovered
//     before this; a loop with no preceding primary damage falls back to the
//     body deal_damage's amount (literal, or a variable's Start).
//   - projectile: the body beam's variant (for the icon fallback).
func (m *programMechanics) recoverLoop(cfg loopConfig) {
	vars := map[string]LoopVar{}
	for _, v := range cfg.Vars {
		vars[v.Name] = v
	}
	hasBeam, hasSelect := false, false
	for _, act := range cfg.Body {
		switch act.Type {
		case ActionBeam:
			hasBeam = true
			var bc beamConfig
			decodeActionConfig(act.Config, &bc)
			if m.projectile == "" {
				m.projectile = bc.Variant
			}
		case ActionSelectTargets:
			hasSelect = true
		case ActionDealDamage:
			amtLit, amtRef := fieldLiteralOrRef(act.Config, "amount")
			if amtRef != "" {
				if v, ok := vars[amtRef]; ok {
					if m.damageAmount == 0 {
						m.damageAmount = int(v.Start)
					}
					// The "-N damage per bounce" prose only fits an ADDITIVE step;
					// a percent step scales multiplicatively, so leave the flat
					// falloff unset (no misleading clause) for it.
					if v.Step != 0 && v.StepMode != loopStepPercent && m.bounceDamageFalloff == 0 {
						m.bounceDamageFalloff = int(math.Round(-v.Step))
					}
				}
			} else if m.damageAmount == 0 {
				m.damageAmount = amtLit
			}
		}
	}
	if hasBeam && hasSelect && m.chainCount == 0 && cfg.Iterations > 0 {
		m.chainCount = cfg.Iterations
	}
}

// fieldLiteralOrRef reads a numeric config field by key, which may be a literal
// number or a loop-variable reference (a bare letter string). Returns
// (number, "") for a literal, (0, name) for a reference, (0, "") when absent.
func fieldLiteralOrRef(raw json.RawMessage, key string) (int, string) {
	if len(raw) == 0 {
		return 0, ""
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return 0, ""
	}
	f, ok := fields[key]
	if !ok {
		return 0, ""
	}
	var n float64
	if json.Unmarshal(f, &n) == nil {
		return int(n), ""
	}
	var s string
	if json.Unmarshal(f, &s) == nil {
		return 0, s
	}
	return 0, ""
}

// decodeActionConfig best-effort unmarshals raw into out. An empty config or a
// decode error leaves out at its zero value — the same "field not present"
// signal an unset legacy field carries, so a malformed/partial program config
// degrades to an incomplete-but-non-panicking description rather than an error.
func decodeActionConfig(raw json.RawMessage, out any) {
	if len(raw) == 0 {
		return
	}
	_ = json.Unmarshal(raw, out)
}

// applyTo writes the recovered magnitudes onto shadow's legacy mechanic
// fields, gated the same way the legacy catalog gates them (a zero magnitude
// is inert and must not overwrite the zero value already on shadow from
// conversion).
func (m *programMechanics) applyTo(shadow *AbilityDef) {
	if m.damageAmount > 0 {
		shadow.DamageAmount = m.damageAmount
		shadow.Radius = m.radius
	}
	if m.damagePerSecond > 0 {
		shadow.DamagePerSecond = m.damagePerSecond
		shadow.Radius = m.radius
	}
	if m.pullStrength > 0 {
		shadow.PullStrength = m.pullStrength
	}
	if m.chainCount > 0 {
		shadow.ChainCount = m.chainCount
		shadow.BounceDamageFalloff = m.bounceDamageFalloff
	}
	if m.projectile != "" {
		shadow.Projectile = m.projectile
		shadow.ProjectileScale = m.projectileScale
		shadow.MinorDamage = m.minorDamage
	}
	if m.healAmount > 0 {
		shadow.HealAmount = m.healAmount
		// A single-target heal's compiled select_targets query has no MaxCount
		// at all (Source: SrcInitialTarget — see compileHealActions), so
		// m.targetCount is 0 for it, same as an unset legacy TargetCount.
		// Only overwrite shadow's TargetCount (already normalised to >= 1 by
		// validateAbilityDef) when a real multi-target MaxCount was recovered
		// — otherwise a single-target heal's shadow would regress from 1 to 0.
		if m.targetCount > 0 {
			shadow.TargetCount = m.targetCount
		}
	}
	if m.summonUnitType != "" {
		shadow.SummonUnitType = m.summonUnitType
		shadow.SummonCount = m.summonCount
	}
	if m.slowMultiplier > 0 {
		shadow.SlowMultiplier = m.slowMultiplier
		shadow.SlowDurationSeconds = m.slowDurationSeconds
	}
	if m.burnDurationSeconds > 0 {
		shadow.BurnDurationSeconds = m.burnDurationSeconds
		shadow.BurnDamagePerTick = m.burnDamagePerTick
		shadow.BurnTickIntervalSeconds = m.burnTickIntervalSeconds
	}
	if m.burnEffectAtPoint != "" {
		shadow.BurnEffectAtPoint = m.burnEffectAtPoint
	}
	if m.effectOnTarget != "" {
		shadow.EffectOnTarget = m.effectOnTarget
	}
	if m.effectAtPoint != "" {
		shadow.EffectAtPoint = m.effectAtPoint
		shadow.EffectScale = m.effectScale
	}
	if m.chargeRequired > 0 {
		shadow.ChargeRequired = m.chargeRequired
		shadow.ManaToChargeRatio = m.manaToChargeRatio
		shadow.MissileCount = m.missileCount
		shadow.DamagePerMissile = m.damagePerMissile
		shadow.MissileDelayMs = m.missileDelayMs
		shadow.Targeting = m.chargeTargeting
		shadow.AllowDuplicateTargets = m.chargeAllowDuplicateTargets
	}
	if m.channelType != "" {
		shadow.ChannelType = m.channelType
		shadow.TickIntervalSeconds = m.channelTickIntervalSeconds
		shadow.ManaCostPerTick = m.channelManaCostPerTick
		shadow.DamagePerTick = m.channelDamagePerTick
		shadow.HealingMultiplier = m.channelHealingMultiplier
		shadow.AllyHealRadius = m.channelAllyHealRadius
	}
}

// describeChannelAbility covers channeled beams (siphon_life): a per-tick drain
// that optionally heals the caster and nearby allies for the damage dealt.
func describeChannelAbility(a AbilityDef) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Channels a beam that drains %d %s health from the target",
		a.DamagePerTick, abilitySchoolWord(a))
	if a.TickIntervalSeconds > 0 {
		fmt.Fprintf(&b, " every %ss", trimFloat(a.TickIntervalSeconds))
	}
	if a.HealingMultiplier > 0 {
		b.WriteString(", healing the caster")
		if a.AllyHealRadius > 0 {
			fmt.Fprintf(&b, " and allies within %s", trimFloat(a.AllyHealRadius))
		}
		b.WriteString(" for the health drained")
	}
	b.WriteString(".")
	return b.String()
}

// describeChargeFireAbility covers Arcane-Charge auto-firing passives
// (arcane_missiles): spending mana builds charge, and at the threshold a volley
// fires automatically.
func describeChargeFireAbility(a AbilityDef) string {
	return fmt.Sprintf(
		"Passive. Spending mana builds Arcane Charge; at %s charge it fires %d missiles dealing %d %s damage each at nearby enemies, then resets.",
		trimFloat(a.ChargeRequired), a.MissileCount, a.DamagePerMissile, abilitySchoolWord(a),
	)
}

// describeSummonAbility covers summons (raise_skeleton).
func describeSummonAbility(a AbilityDef) string {
	count := a.SummonCount
	if count < 1 {
		count = 1
	}
	unit := humanizeID(a.SummonUnitType)
	if count != 1 {
		unit += "s"
	}
	return fmt.Sprintf("Summons %d %s that fight for the caster.", count, unit)
}

// describeEffectAbility composes a damage and/or heal sentence with its
// secondary modifier clauses (area, chain, slow, burn, pull). This is the
// default path for the majority of abilities.
func describeEffectAbility(a AbilityDef) string {
	clauses := make([]string, 0, 2)

	if dmg := describeDamageClause(a); dmg != "" {
		clauses = append(clauses, dmg)
	}
	if a.HealAmount > 0 {
		clauses = append(clauses, describeHealClause(a))
	}
	if len(clauses) == 0 {
		return ""
	}

	sentence := capitalize(strings.Join(clauses, ", and ")) + "."

	// Burn is a trailing, self-contained sentence (meteor's lingering crater),
	// not an inline clause, so it reads cleanly regardless of what preceded it.
	if a.BurnDurationSeconds > 0 && a.BurnDamagePerTick > 0 {
		sentence += " " + describeBurnSentence(a)
	}
	return sentence
}

// describeDamageClause builds the "deals X damage ..." fragment (lowercased,
// no trailing period) for either a one-shot hit (DamageAmount) or a
// damage-over-time field (DamagePerSecond), plus area / chain / slow / pull
// modifiers. Returns "" when the ability deals no damage.
func describeDamageClause(a AbilityDef) string {
	var b strings.Builder
	switch {
	case a.DamageAmount > 0:
		fmt.Fprintf(&b, "deals %d %s damage", a.DamageAmount, abilitySchoolWord(a))
		b.WriteString(describeAreaFragment(a))
	case a.DamagePerSecond > 0:
		fmt.Fprintf(&b, "deals %s %s damage per second", trimFloat(a.DamagePerSecond), abilitySchoolWord(a))
		b.WriteString(describeAreaFragment(a))
	default:
		return ""
	}
	if a.ChainCount > 0 {
		fmt.Fprintf(&b, ", then arcs to %d more nearby enem%s", a.ChainCount, plural(a.ChainCount, "y", "ies"))
		if a.BounceDamageFalloff > 0 {
			fmt.Fprintf(&b, " (-%d damage per bounce)", a.BounceDamageFalloff)
		}
	}
	if a.SlowMultiplier > 0 && a.SlowMultiplier < 1 {
		pct := int((1 - a.SlowMultiplier) * 100)
		fmt.Fprintf(&b, ", slowing them by %d%%", pct)
		if a.SlowDurationSeconds > 0 {
			fmt.Fprintf(&b, " for %ss", trimFloat(a.SlowDurationSeconds))
		}
	}
	if a.PullStrength > 0 {
		b.WriteString(", pulling them toward its center")
	}
	return b.String()
}

// describeAreaFragment describes the target set of a damaging ability: an area
// clause when the ability has an area of effect (radius on a point / aoe-tagged
// cast), otherwise a plain "to the target".
func describeAreaFragment(a AbilityDef) string {
	if a.Radius > 0 && (a.TargetsPoint || a.HasTag("aoe")) {
		return fmt.Sprintf(" to all enemies within %s units", trimFloat(a.Radius))
	}
	return " to the target"
}

// describeHealClause builds the "restores X health ..." fragment (lowercased,
// no trailing period), scaled by TargetCount for multi-target heals.
func describeHealClause(a AbilityDef) string {
	if a.TargetCount > 1 {
		return fmt.Sprintf("restores %d health to up to %d allies", a.HealAmount, a.TargetCount)
	}
	return fmt.Sprintf("restores %d health to an ally", a.HealAmount)
}

// describeBurnSentence describes a lingering ground hazard (meteor's crater).
func describeBurnSentence(a AbilityDef) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Leaves a burning area dealing %d damage", a.BurnDamagePerTick)
	if a.BurnTickIntervalSeconds > 0 {
		fmt.Fprintf(&b, " every %ss", trimFloat(a.BurnTickIntervalSeconds))
	}
	fmt.Fprintf(&b, " for %ss.", trimFloat(a.BurnDurationSeconds))
	return b.String()
}

// abilitySchoolWord is the damage-type word used inline in prose ("fire",
// "holy", ...), defaulting an unspecified type to "physical" to match the
// runtime's DamageType.OrPhysical() resolution.
func abilitySchoolWord(a AbilityDef) string {
	return string(a.DamageType.OrPhysical())
}

// humanizeID turns a snake_case catalog id into spaced words
// ("skeleton_soldier" -> "skeleton soldier").
func humanizeID(id string) string {
	return strings.ReplaceAll(id, "_", " ")
}

// capitalize upper-cases the first rune of s (ASCII-simple; ids and generated
// prose are ASCII).
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// plural picks the singular or plural suffix based on n.
func plural(n int, singular, pluralForm string) string {
	if n == 1 {
		return singular
	}
	return pluralForm
}

// trimFloat formats a float without a trailing ".0" (0.25 -> "0.25", 4.0 ->
// "4", 3 -> "3") for clean inline numbers.
func trimFloat(f float64) string {
	return fmt.Sprintf("%g", f)
}
