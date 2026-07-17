package game

import (
	"encoding/json"
	"math"
)

// ─────────────────────────────────────────────────────────────────────────────
// launch_projectile executor (Phase 6c: the deferred "executor pass").
//
// DESIGN (user-chosen, do not "improve" on it): BAKED PAYLOAD, DELEGATE TO
// EXISTING SEAMS. Execute does not reimplement projectile travel, homing,
// splash, or chain-bounce logic — it resolves the caster + target(s), folds
// the action's configured Amount through the SAME modifier-fold seam
// deal_damage uses (effectiveAbilityDamageLocked, exactly once — see the
// DOUBLE-FOLD note below), builds a lightweight EffectiveSpell + AbilityDef
// shim from the compiled config, and calls the EXACT SAME functions the
// legacy cast resolver calls for a projectile/chain ability
// (resolveAbilityCastOnTargetLocked, ability_cast.go:608-625):
//
//   - cfg.ChainCount > 0 -> fireAbilityChainLocked (chain_lightning's
//     beam-bounce mechanic — this NEVER spawns a Projectile, it resolves the
//     whole bounce chain inline as Beams via executeProcEffectLocked /
//     fireProcBeamLocked; "launch_projectile" is a slight misnomer for this
//     case, accepted per the design doc).
//   - otherwise          -> fireAbilityProjectileLocked (arcane_bolt/fireball's
//     homing bolt; fireball's splash is applied BY THE PROJECTILE at impact
//     via Projectile.AbilitySplashRadius, snapshotted from cfg.Radius here).
//
// Neither ProjectileSpawnDef.Triggers nor TriggerOnProjectileImpact is
// touched by this file — both remain dead/unwired, matching the design
// decision to not build impact re-entry.
//
// DOUBLE-FOLD HAZARD — read before touching the amount computation:
// legacy folds a spell's modifiers ONCE, at effectiveSpellLocked
// (ability_cast.go's `eff`, built before resolveAbilityCastOnTargetLocked
// ever runs) — eff.Damage is ALREADY the scaled number by the time it
// reaches fireAbilityProjectileLocked/fireAbilityChainLocked. The composable
// model has no equivalent up-front `eff`: this action's cfg.Amount is the
// RAW authored base (== the legacy def's DamageAmount at compile time), so
// Execute folds it itself, EXACTLY ONCE, via effectiveAbilityDamageLocked —
// the same helper (and the same underlying applySpellModField) that
// resolveEffectiveSpell's Damage field uses. Given identical inputs (the
// caster's active modifiers, the def's school/tags, the same base amount)
// the two produce IDENTICAL results — see
// TestAbilityCompileGolden_ArcaneBolt/Fireball/ChainLightning's
// modified_caster sub-tests for the byte-for-byte proof. Do NOT also read
// ctx.abilityDef.DamageAmount or re-derive eff.Damage from anywhere else —
// that would fold the modifier a second time.
//
// ctx.effectiveDamageMultiplier() is applied on top (same as deal_damage) so
// a caller-customized EffectiveSpell (unstable_magic's reduced-effectiveness
// free proc, if it is ever routed through this action for a migrated
// ability) is honoured identically; it is a 1.0 no-op for every ordinary
// cast today.
// ─────────────────────────────────────────────────────────────────────────────

func (launchProjectileConfig) actionConfig() {}

func init() {
	registerAction(ActionDescriptor{
		Type: ActionLaunchProjectile,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c launchProjectileConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(launchProjectileConfig)
			var out []ValidationIssue
			if c.Projectile == "" {
				out = append(out, ValidationIssue{Code: "empty_required_property", Message: "launch_projectile requires projectile", Severity: "error"})
			}
			if c.Amount <= 0 {
				out = append(out, ValidationIssue{Code: "empty_required_property", Message: "launch_projectile requires amount > 0", Severity: "error"})
			}
			if c.Type != "" && !IsValidDamageType(c.Type) {
				out = append(out, ValidationIssue{Code: "invalid_damage_type", Message: "unknown damage type " + string(c.Type), Severity: "error"})
			}
			return out
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "projectile", Label: "Projectile", Control: "asset", Section: "Presentation"},
			{Key: "amount", Label: "Amount", Control: "number", Section: "Properties"},
			{Key: "type", Label: "Damage Type", Control: "enum", Section: "Properties"},
			{Key: "radius", Label: "Splash Radius", Control: "number", Section: "Targeting"},
			{Key: "projectileScale", Label: "Projectile Scale", Control: "number", Section: "Presentation"},
			{Key: "chainCount", Label: "Chain Count", Control: "number", Section: "Properties"},
			{Key: "bounceRange", Label: "Bounce Range", Control: "number", Section: "Properties"},
			{Key: "bounceDamageFalloff", Label: "Bounce Damage Falloff", Control: "number", Section: "Properties"},
		}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(launchProjectileConfig)
			caster := s.getUnitByIDLocked(ctx.CasterID)
			if caster == nil {
				return nil
			}

			// Fold EXACTLY ONCE — see the file doc comment.
			amount := c.Amount
			if ctx.abilityDef != nil {
				amount = s.effectiveAbilityDamageLocked(caster, *ctx.abilityDef, c.Amount)
			}
			if m := ctx.effectiveDamageMultiplier(); m != 1.0 {
				amount = int(math.Round(float64(amount) * m))
			}
			// Mirror legacy's guard exactly: resolveAbilityCastOnTargetLocked only
			// fires the projectile/chain branch when `eff.Damage > 0`.
			if amount <= 0 {
				ctx.trace("action_skipped", ctx.currentActionPath, map[string]any{"reason": "non_positive_damage", "amount": amount})
				return nil
			}

			// Shim def/eff built purely from the baked config — never from
			// ctx.abilityDef's mechanic fields, which are cleared on a converted
			// (schemaVersion 2) ability. See the file doc comment.
			def := AbilityDef{
				ID:                  ctx.AbilityID,
				DamageType:          c.Type,
				Projectile:          c.Projectile,
				ProjectileScale:     c.ProjectileScale,
				MinorDamage:         c.MinorDamage,
				BounceRange:         c.BounceRange,
				BounceDamageFalloff: c.BounceDamageFalloff,
			}
			eff := EffectiveSpell{
				Damage:     amount,
				Radius:     c.Radius,
				ChainCount: c.ChainCount,
			}

			hit := make([]int, 0, len(targets))
			for _, id := range targets {
				target := s.getUnitByIDLocked(id)
				// Mirror legacy's other half of the guard: `target.HP > 0`. The
				// compiled Target query (SrcInitialTarget, default AliveState)
				// already filters this at resolveActionTargetsLocked time, but a
				// hand-authored program could feed this action a stale/dead id via
				// Input["targets"], so Execute re-checks defensively — same
				// discipline as every other registered action's per-target loop
				// (deal_damage, restore_health, apply_force, ...).
				if target == nil || target.HP <= 0 {
					continue
				}
				switch {
				case eff.ChainCount > 0:
					// chain_lightning: never spawns a Projectile — resolves the
					// whole bounce chain inline as Beams. See file doc comment.
					s.fireAbilityChainLocked(caster, target, def, eff)
				default:
					s.fireAbilityProjectileLocked(caster, target, def, eff)
				}
				hit = append(hit, id)
				ctx.trace("projectile_launched", ctx.currentActionPath, map[string]any{
					"target": id, "amount": amount, "chainCount": eff.ChainCount,
				})
			}
			return hit
		},
	})
}
