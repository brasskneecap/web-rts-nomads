package game

import "encoding/json"

// ─────────────────────────────────────────────────────────────────────────────
// channel_beam executor (siphon_life's composable migration).
//
// DESIGN (BAKED PAYLOAD, DELEGATE TO THE EXISTING SEAM — same shape as
// launch_projectile/launch_vortex/charge_fire_volley): Execute does not
// reimplement the channel lifecycle. It resolves the caster + the query-
// resolved initial target, builds a channelSpec from the compiled config,
// and calls startChannelLocked — the EXACT SAME mechanical helper the legacy
// direct path in beginAbilityChannelLocked calls (ability_channel.go).
// tickUnitChannelLocked then drives the multi-tick drain/heal loop every
// tick exactly as it already does for a legacy-started channel, reading its
// config via channelSpecFor — this action only decides whether and with
// what parameters to START one.
//
// WHY THIS NEVER RUNS FROM CAST RESOLUTION: unlike every other migrated
// action (which fires from resolveAbilityProgramCastLocked, well after
// beginAbilityCastLocked has already committed mana/GCD/cooldown), this
// action's trigger only ever fires from INSIDE beginAbilityChannelLocked
// itself — see that function and ability_channel.go's file doc comment
// ("THE ORDERING DECISION") for why. beginAbilityChannelLocked runs ALL
// gating (ownership, busy, GCD, target legality, range, mana-for-first-tick)
// before firing the trigger, so this Execute performs no validation of its
// own — it is the unconditional "just start it" step, mirroring
// charge_fire_volley's Execute (spell_charge.go), whose hostile-in-range
// gate likewise already ran in its caller (tickArcaneMissilesLocked) before
// the trigger fired.
// ─────────────────────────────────────────────────────────────────────────────

func (channelBeamConfig) actionConfig() {}

func init() {
	registerAction(ActionDescriptor{
		Type: ActionChannelBeam,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c channelBeamConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(channelBeamConfig)
			var out []ValidationIssue
			if c.ChannelType == "" {
				out = append(out, ValidationIssue{Code: "empty_required_property", Message: "channel_beam requires channelType", Severity: "error"})
			}
			// A zero/negative tick interval would make tickUnitChannelLocked's
			// ChannelNextTickIn advance by 0 each iteration, looping forever
			// (capped only by channelMaxTicksPerUpdate, which would then fire
			// every Update() call) — same invariant compileMeteorZoneConfig's
			// burn tick interval requires.
			if c.TickIntervalSeconds <= 0 {
				out = append(out, ValidationIssue{Code: "empty_required_property", Message: "channel_beam requires tickIntervalSeconds > 0", Severity: "error"})
			}
			return out
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "channelType", Label: "Channel Type", Control: "text", Section: "Properties"},
			{Key: "tickIntervalSeconds", Label: "Tick Interval (s)", Control: "number", Section: "Timing"},
			{Key: "manaCostPerTick", Label: "Mana Cost Per Tick", Control: "number", Section: "Properties"},
			{Key: "damagePerTick", Label: "Damage Per Tick", Control: "number", Section: "Properties"},
			{Key: "healingMultiplier", Label: "Healing Multiplier", Control: "number", Section: "Properties"},
			{Key: "allyHealRadius", Label: "Ally Heal Radius", Control: "number", Section: "Targeting"},
		}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(channelBeamConfig)
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
		},
	})
}
