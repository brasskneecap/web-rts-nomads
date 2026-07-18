package game

import (
	"encoding/json"
	"fmt"
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
		return describeAbilityProgram(a)
	}
	return describeLegacyAbility(a)
}

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
	return describeLegacyAbility(abilityMechanicsShadow(a))
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

	m := &programMechanics{}
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
			// chain_lightning's authored bounce chain (momentary)
			// (compileChainLightningActions, ability_compile.go): a ladder of
			// nested beam actions, one per hop (0 = primary, 1..ChainCount =
			// bounces). See recoverChainLightningBeam's doc comment.
			if m.sawPrimaryDamage {
				break
			}
			m.projectile = cfg.Variant
			m.sawPrimaryDamage = true
			m.recoverChainLightningBeam(cfg, 0)
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
					if trig.Type != TriggerOnProjectileTick {
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
			var cfg applyStatusConfig
			decodeActionConfig(act.Config, &cfg)
			if cfg.Status == "slow" {
				m.slowMultiplier = cfg.Multiplier
				m.slowDurationSeconds = cfg.Duration
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
