package game

import "encoding/json"

// ─────────────────────────────────────────────────────────────────────────────
// beam executor — one action, two shapes selected by beamConfig.Channeled.
//
// MOMENTARY (Channeled=false) — the beam analogue of launch_projectile
// (ability_exec_projectile.go): spawn an instantaneous Beam visual at a
// resolved target and, a beat later (ImpactDelaySeconds), run a nested
// on_beam_impact trigger's actions. Unlike a projectile there is no travel
// time; the impact fires after a fixed authored delay
// (beamProcDamageDelaySeconds when unset), driven by tickBeamsLocked's
// countdown on Beam.DamageDelayRemaining. Used by chain_lightning (each hop is
// a momentary beam; the impact selects the next victim and launches the next).
//
// CHANNELED (Channeled=true) — hands off to the multi-tick channel lifecycle
// (ability_channel.go). Execute does not reimplement anything: it resolves the
// caster + query-resolved target, builds a channelSpec from the compiled
// config, and calls startChannelLocked — the EXACT SAME mechanical helper the
// legacy direct path in beginAbilityChannelLocked calls. tickUnitChannelLocked
// then drives the drain/heal loop each tick (reading channelSpecFor and firing
// the nested on_beam_tick trigger for the authored per-tick damage). Used by
// siphon_life.
//
// WHY THE CHANNELED BRANCH NEVER RUNS FROM CAST RESOLUTION: a channel can only
// START from beginAbilityChannelLocked (it owns the GCD/cooldown/interrupt
// gating — see ability_channel.go's "THE ORDERING DECISION"). So a channeled
// beam is ONLY valid as the channel-start action of a root on_cast_complete
// trigger; the validator (ability_program_validate.go) rejects it anywhere
// else, rather than letting it silently fail from the wrong call site.
//
// The CROSS-TICK OP BUDGET / DOUBLE-FOLD notes on ability_exec_projectile.go's
// file doc comment apply to the momentary branch verbatim (same
// ctx.sharedOpsRemaining / ctx.abilityDef seams) — see fireBeamImpactLocked
// (beam.go).
// ─────────────────────────────────────────────────────────────────────────────

// defaultBeamVariant is the client-side renderer variant used when a momentary
// beam leaves Variant unset.
const defaultBeamVariant = "lightning_bolt"

// beamConfig is the unified beam action config. The Channeled toggle selects
// which field group and which nested trigger apply:
//   - momentary: Variant/SpawnOrigin/ImpactDelaySeconds/DurationMs + on_beam_impact
//   - channeled: ChannelType/TickIntervalSeconds/ManaCostPerTick/DamagePerTick/
//     HealingMultiplier/AllyHealRadius + on_beam_tick
//
// The channeled magnitudes are baked here because a converted (schemaVersion 2)
// ability has its legacy mechanic fields cleared (ConvertLegacyAbility), so
// Config is channelSpecFor's only source of truth for them. The per-tick
// DAMAGE itself is additionally authored in the on_beam_tick trigger (Triggers)
// — tickUnitChannelLocked runs that through the real deal_damage action so the
// spell-modifier + Siphoner-perk fold goes through the one canonical path;
// DamagePerTick here is only the shadow-recovery / legacy value.
type beamConfig struct {
	Variant     string       `json:"variant,omitempty"`
	SpawnOrigin TargetOrigin `json:"spawnOrigin,omitempty"`
	// SpawnOriginRef supplies the named-context key when SpawnOrigin is
	// "named_context_value" — e.g. a loop chain arcing FROM its "cursor" unit
	// (the current target) TO the next. nil for every other origin.
	SpawnOriginRef *ContextRef `json:"spawnOriginRef,omitempty"`
	// Channeled selects the channeled lifecycle over the momentary one-shot.
	Channeled bool `json:"channeled,omitempty"`

	// ── momentary (Channeled=false) ──
	ImpactDelaySeconds float64 `json:"impactDelaySeconds,omitempty"`
	DurationMs         int     `json:"durationMs,omitempty"`

	// ── channeled (Channeled=true) ──
	ChannelType         string  `json:"channelType,omitempty"`
	TickIntervalSeconds float64 `json:"tickIntervalSeconds,omitempty"`
	ManaCostPerTick     int     `json:"manaCostPerTick,omitempty"`
	DamagePerTick       int     `json:"damagePerTick,omitempty"`
	HealingMultiplier   float64 `json:"healingMultiplier,omitempty"`
	AllyHealRadius      float64 `json:"allyHealRadius,omitempty"`

	// Triggers carries the nested effect trigger: on_beam_impact (momentary) or
	// on_beam_tick (channeled). config.triggers slot, mirroring
	// launchProjectileConfig.Triggers — never Children (no on_action_complete
	// auto-fire); fires only on the beam's deferred impact / each channel tick.
	Triggers []AbilityTriggerDef `json:"triggers,omitempty"`
}

func (beamConfig) actionConfig() {}

func init() {
	registerAction(ActionDescriptor{
		Type: ActionBeam,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c beamConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(beamConfig)
			var out []ValidationIssue
			if c.Channeled {
				if c.ChannelType == "" {
					out = append(out, ValidationIssue{Code: "empty_required_property", Message: "a channeled beam requires channelType", Severity: "error"})
				}
				// A zero/negative tick interval would make tickUnitChannelLocked's
				// ChannelNextTickIn advance by 0 each iteration, looping forever.
				if c.TickIntervalSeconds <= 0 {
					out = append(out, ValidationIssue{Code: "empty_required_property", Message: "a channeled beam requires tickIntervalSeconds > 0", Severity: "error"})
				}
			} else {
				if c.ImpactDelaySeconds < 0 {
					out = append(out, ValidationIssue{Code: "invalid_property", Message: "beam impactDelaySeconds must not be negative", Severity: "error"})
				}
				if !isValidSpawnOrigin(c.SpawnOrigin) {
					out = append(out, ValidationIssue{Code: "invalid_property", Message: "unknown spawnOrigin " + string(c.SpawnOrigin), Severity: "error"})
				}
			}
			return out
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			// target: the ONE unit the beam hits/channels at — its own direct
			// TargetQueryDef (SrcInitialTarget), narrow source-only shape, same
			// as launch_projectile.
			{Key: "target", Label: "Fire Beam At", Control: "target_query", Section: "Targeting", TargetQueryFields: targetQueryFieldsSourceOnly},
			{Key: "variant", Label: "Variant", Control: "text", Section: "Presentation"},
			{Key: "channeled", Label: "Channeled", Control: "boolean", Section: "Properties"},
			// momentary-only fields (hidden while channeled)
			{Key: "spawnOrigin", Label: "Spawn Origin", Control: "enum", Options: spawnOriginOptions, Section: "Advanced", ShowWhen: beamMomentaryShowWhen()},
			// Shared with launch_projectile: the Saved Value the beam arcs from,
			// shown only when Spawn Origin is "Saved Position".
			{Key: "spawnOriginRef", Label: "Saved Value", Control: "context_ref", Section: "Advanced", ShowWhen: spawnOriginNamedShowWhen()},
			{Key: "impactDelaySeconds", Label: "Impact Delay (s)", Control: "number", Section: "Advanced", ShowWhen: beamMomentaryShowWhen()},
			{Key: "durationMs", Label: "Duration (ms)", Control: "number", Section: "Presentation", ShowWhen: beamMomentaryShowWhen()},
			// channeled-only fields (shown only while channeled) — this is the
			// "timing becomes available when channeled" surface.
			{Key: "channelType", Label: "Channel Type", Control: "text", Section: "Properties", ShowWhen: beamChanneledShowWhen()},
			{Key: "tickIntervalSeconds", Label: "Tick Interval (s)", Control: "number", Section: "Timing", ShowWhen: beamChanneledShowWhen()},
			{Key: "manaCostPerTick", Label: "Mana Cost Per Tick", Control: "number", Section: "Properties", ShowWhen: beamChanneledShowWhen()},
			{Key: "damagePerTick", Label: "Damage Per Tick", Control: "number", Kind: abilityStatKindDamage, Section: "Properties", ShowWhen: beamChanneledShowWhen()},
			{Key: "healingMultiplier", Label: "Healing Multiplier", Control: "number", Section: "Properties", ShowWhen: beamChanneledShowWhen()},
			{Key: "allyHealRadius", Label: "Ally Heal Radius", Control: "number", Kind: abilityStatKindRadius, Section: "Targeting", ShowWhen: beamChanneledShowWhen()},
		}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(beamConfig)
			if c.Channeled {
				return s.executeChanneledBeamLocked(ctx, c, targets)
			}
			return s.executeMomentaryBeamLocked(ctx, c, targets)
		},
	})
}

// beamChanneledShowWhen / beamMomentaryShowWhen gate the two mutually-exclusive
// field groups on the `channeled` toggle. Absent `channeled` resolves to false
// (FieldConditionMatches), so a fresh momentary beam shows the momentary group.
func beamChanneledShowWhen() *FieldCondition {
	return &FieldCondition{Key: "channeled", Op: "eq", Value: json.RawMessage("true")}
}
func beamMomentaryShowWhen() *FieldCondition {
	return &FieldCondition{Key: "channeled", Op: "eq", Value: json.RawMessage("false")}
}

// executeMomentaryBeamLocked spawns a one-shot beam per target and defers its
// authored on_beam_impact actions. Caller holds s.mu.
func (s *GameState) executeMomentaryBeamLocked(ctx *RuntimeAbilityContext, c beamConfig, targets []int) []int {
	caster := s.getUnitByIDLocked(ctx.CasterID)
	if caster == nil {
		return nil
	}

	// Shared cross-tick op budget for this lineage — see
	// ability_exec_projectile.go's CROSS-TICK OP BUDGET section.
	var budget *int
	if ctx.sharedOpsRemaining != nil {
		budget = ctx.sharedOpsRemaining
	} else {
		remaining := maxExecutionOps - ctx.opsUsed
		budget = &remaining
	}

	var impactActions []AbilityActionDef
	for _, trig := range c.Triggers {
		if trig.Type == TriggerOnBeamImpact {
			impactActions = trig.Actions
			break
		}
	}

	variant := c.Variant
	if variant == "" {
		variant = defaultBeamVariant
	}
	delay := c.ImpactDelaySeconds
	if delay == 0 {
		delay = beamProcDamageDelaySeconds
	}
	originPos := s.resolveOriginLocked(ctx, c.SpawnOrigin, c.SpawnOriginRef)
	if c.SpawnOrigin == OriginTargetsCenter {
		// Centroid of this beam's target list — resolveOriginLocked can't see
		// the targets, so compute it here (matches launch_projectile).
		originPos = s.targetsCenterLocked(ctx, targets)
	}
	// The VISUAL-origin unit (Beam.CasterUnitID) is the unit at the spawn
	// origin, NOT necessarily the caster: a chain bounce spawns from
	// current_event_position, so it must lift from the previous victim's chest
	// to read as a continuous chain. Falls back to the caster for
	// caster-relative origins, 0 for pure-position origins. Attribution / beam
	// ownership still come from caster.ID (first arg), unchanged.
	originUnit := s.originUnitForSpawnLocked(ctx, c.SpawnOrigin, c.SpawnOriginRef)

	hit := make([]int, 0, len(targets))
	for _, id := range targets {
		target := s.getUnitByIDLocked(id)
		if target == nil || target.HP <= 0 {
			continue
		}
		s.spawnBeamWithImpactActionsLocked(caster.ID, originUnit, originPos.X, originPos.Y, target, variant, ctx.AbilityID, impactActions, budget, ctx.effectiveDamageMultiplier(), c.DurationMs, delay, ctx.Named)
		hit = append(hit, id)
		ctx.trace("beam_launched", ctx.currentActionPath, map[string]any{"target": id})
	}
	return hit
}

// executeChanneledBeamLocked starts a channel on the resolved target. It
// performs NO gating of its own — beginAbilityChannelLocked already ran all of
// it before firing this action's trigger (see the file doc comment). Caller
// holds s.mu.
func (s *GameState) executeChanneledBeamLocked(ctx *RuntimeAbilityContext, c beamConfig, targets []int) []int {
	caster := s.getUnitByIDLocked(ctx.CasterID)
	if caster == nil || len(targets) == 0 {
		return nil
	}
	target := s.getUnitByIDLocked(targets[0])
	if target == nil {
		ctx.trace("action_skipped", ctx.currentActionPath, map[string]any{"reason": "no_target"})
		return nil
	}
	spec := channelSpec{
		ChannelType:         c.ChannelType,
		TickIntervalSeconds: c.TickIntervalSeconds,
		ManaCostPerTick:     c.ManaCostPerTick,
		DamagePerTick:       c.DamagePerTick,
		HealingMultiplier:   c.HealingMultiplier,
		AllyHealRadius:      c.AllyHealRadius,
	}
	s.startChannelLocked(caster, target, ctx.AbilityID, spec)
	ctx.trace("channel_started", ctx.currentActionPath, map[string]any{
		"target":              target.ID,
		"tickIntervalSeconds": spec.TickIntervalSeconds,
	})
	return nil
}
