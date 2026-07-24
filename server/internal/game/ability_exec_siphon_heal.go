package game

import (
	"encoding/json"
	"math"
)

// ── siphon_heal ──
//
// The per-tick heal of a channeled Siphon Life, expressed as a composable
// action so the perk multiplier that used to live on AbilityModifier.HealMult
// (soul_leech / beam_mastery) is now an abilityFields modifier on this action's
// `healMult` field — the heal sibling of the on_beam_tick deal_damage's
// `amount` field. See compileChannelBeamTickTrigger (ability_compile.go) for
// where it is compiled into the trigger, and ActionSiphonHeal's doc comment
// (ability_program.go) for the runtime contract.
//
// It does NOT heal the action's target set. The heal distribution is the
// bespoke Siphon Life rule (self-first → dark_renewal shield cascade → lowest-
// HP ally), which stays in distributeSiphonHealLocked (ability_channel.go);
// this action just computes the tick's heal amount and hands it to that
// distributor, then records it in ctx.lastAppliedHeal for chain_siphon.
type siphonHealConfig struct {
	// HealingMultiplier is the ability's own per-tick heal fraction of the
	// tick's applied damage (def.HealingMultiplier). Baked at compile time.
	HealingMultiplier float64 `json:"healingMultiplier"`
	// HealMult is the perk multiplier layer, base 1.0. It is the field-mod
	// target that replaced AbilityModifier.HealMult — a perk multiplies it via
	// abilityFields (op "multiply"), composing multiplicatively across perks
	// exactly as the retired scalar aggregator did. Kept as a SEPARATE factor
	// (rather than folded into HealingMultiplier) so the multiplication order
	// stays `damage × healingMultiplier × healMult`, byte-identical to the
	// legacy inline computation this replaces.
	HealMult float64 `json:"healMult"`
	// AllyHealRadius is the radius within which the distributor may heal an ally
	// when the caster is at full HP (def.AllyHealRadius). Baked at compile time.
	AllyHealRadius float64 `json:"allyHealRadius,omitempty"`
}

func (siphonHealConfig) actionConfig() {}

func init() {
	registerAction(ActionDescriptor{
		Type: ActionSiphonHeal,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c siphonHealConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(siphonHealConfig)
			var out []ValidationIssue
			if c.HealingMultiplier < 0 {
				out = append(out, ValidationIssue{Code: "invalid_value", Message: "siphon_heal healingMultiplier must be >= 0", Severity: "error"})
			}
			return out
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "healingMultiplier", Label: "Healing Multiplier", Control: "number", Section: "Properties"},
			{Key: "healMult", Label: "Heal ×", Control: "number", Section: "Properties"},
			{Key: "allyHealRadius", Label: "Ally Heal Radius", Control: "number", Section: "Properties"},
		}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, _ []int) []int {
			c := cfg.(siphonHealConfig)
			caster := s.getUnitByIDLocked(ctx.CasterID)
			if caster == nil {
				return nil
			}
			// A missing/zero healMult means "no perk multiplier" — identity 1.0,
			// matching the retired AbilityModifier.HealMult convention (<=0 ==
			// unset). The compiled config always carries 1.0, so this only bites
			// a hand-built config.
			healMult := c.HealMult
			if healMult <= 0 {
				healMult = 1
			}
			heal := int(math.Round(float64(ctx.lastAppliedDamage) * c.HealingMultiplier * healMult))
			ctx.lastAppliedHeal = heal
			if heal > 0 {
				s.distributeSiphonHealLocked(caster, heal, c.AllyHealRadius)
			}
			return nil
		},
	})
}
