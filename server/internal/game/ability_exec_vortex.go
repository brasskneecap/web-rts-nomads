package game

import "encoding/json"

// ─────────────────────────────────────────────────────────────────────────────
// launch_vortex executor (arcane_orb's composable migration).
//
// DESIGN (BAKED PAYLOAD, DELEGATE TO THE EXISTING SEAM — same shape as
// launch_projectile, ability_exec_projectile.go): Execute does not
// reimplement the orb's travel, pull, or damage-over-time math. It resolves
// the caster, folds the action's configured Radius/PullStrength/
// DamagePerSecond/ProjectileSpeed through the SAME modifier-fold seam legacy
// uses (applySpellModField — see the FOLD PARITY note below), builds a
// lightweight AbilityDef + EffectiveSpell shim from the compiled config, and
// calls spawnArcaneOrbLocked — the EXACT SAME function the legacy point-cast
// resolver calls for arcane_orb (resolveAbilityCastAtPointLocked,
// ability_cast.go:~255). tickArcaneOrbProjectileLocked (projectile.go) then
// drives the moving vortex every tick exactly as it already does for a
// legacy-cast orb; this file only decides WHETHER and WITH WHAT PARAMETERS
// to spawn one.
//
// WHY A NEW ACTION TYPE, NOT AN EXTENDED launch_projectile: a
// launch_projectile action's identity (and its Execute's whole shape) is
// "resolve a target, then either fire a homing bolt that deals impact
// damage or resolve a chain-bounce" — see that file's doc comment. The orb
// does none of that: it has no target lock (targets nothing — it drags
// whatever wanders into its radius), deals no impact damage at all (only a
// ticking DoT along its path), and its "amount" field (DamagePerSecond) is
// not even the same shape as a one-shot Amount. Overloading
// launchProjectileConfig with Radius/PullStrength/DamagePerSecond fields
// would force its Validate to accept two mutually-exclusive required-field
// shapes (Amount>0 OR PullStrength>0) and its Execute to branch on which
// shape fired — strictly worse than a second, focused action type with its
// own crisp schema/validation, matching the precedent of create_zone living
// beside launch_projectile as its own action rather than a launch_projectile
// variant.
//
// FOLD PARITY — EXACTLY ONCE, PER FIELD:
// legacy resolves ALL FOUR of spawnArcaneOrbLocked's modifier-eligible
// inputs through ONE effectiveSpellLocked call before ever reaching
// spawnArcaneOrbLocked (ability_cast.go's `eff`, built at
// beginAbilityCastAtPointLocked): eff.Radius, eff.PullStrength,
// eff.DamagePerSecond, and eff.ProjectileSpeed are ALL folded fields (see
// resolveEffectiveSpell, spell_modifier.go) — unlike launch_projectile's
// Radius, which that file's own doc comment notes is deliberately NOT
// modifier-scaled. Do not assume the two actions fold the same fields the
// same way; arcane_orb's legacy behavior folds all four, so this Execute
// must too, via the same applySpellModField helper and the same mods
// collection (collectSpellModifiersLocked(caster, *ctx.abilityDef)) — never
// by re-deriving from ctx.abilityDef's own (cleared, on a v2 ability)
// Radius/PullStrength/DamagePerSecond/ProjectileSpeed fields, which would
// either double-fold or silently read zero.
// ─────────────────────────────────────────────────────────────────────────────

func (launchVortexConfig) actionConfig() {}

func init() {
	registerAction(ActionDescriptor{
		Type: ActionLaunchVortex,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c launchVortexConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(launchVortexConfig)
			var out []ValidationIssue
			if c.Projectile == "" {
				out = append(out, ValidationIssue{Code: "empty_required_property", Message: "launch_vortex requires projectile", Severity: "error"})
			}
			if c.PullStrength <= 0 {
				out = append(out, ValidationIssue{Code: "empty_required_property", Message: "launch_vortex requires pullStrength > 0", Severity: "error"})
			}
			if c.Type != "" && !IsValidDamageType(c.Type) {
				out = append(out, ValidationIssue{Code: "invalid_damage_type", Message: "unknown damage type " + string(c.Type), Severity: "error"})
			}
			return out
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "projectile", Label: "Projectile", Control: "asset", Section: "Presentation"},
			{Key: "projectileScale", Label: "Projectile Scale", Control: "number", Section: "Presentation"},
			{Key: "projectileSpeed", Label: "Travel Speed", Control: "number", Section: "Properties"},
			{Key: "radius", Label: "Radius", Control: "number", Section: "Targeting"},
			{Key: "pullStrength", Label: "Pull Strength", Control: "number", Section: "Properties"},
			{Key: "damagePerSecond", Label: "Damage Per Second", Control: "number", Section: "Properties"},
			{Key: "type", Label: "Damage Type", Control: "enum", Section: "Properties"},
			{Key: "castRange", Label: "Cast Range", Control: "sentinel_number", Section: "Targeting"},
		}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(launchVortexConfig)
			caster := s.getUnitByIDLocked(ctx.CasterID)
			if caster == nil {
				return nil
			}

			// Fold EXACTLY ONCE, per field — see the file doc comment. Falls back
			// to the raw baked values when ctx.abilityDef is unset (e.g. a
			// hand-built test context), matching every sibling action's
			// ctx.abilityDef-nil degrade (deal_damage / restore_health /
			// launch_projectile all skip their own scaling seam the same way).
			radius, pull, dps, speed := c.Radius, c.PullStrength, c.DamagePerSecond, c.ProjectileSpeed
			if ctx.abilityDef != nil {
				mods := s.collectSpellModifiersLocked(caster, *ctx.abilityDef)
				radius = applySpellModField(mods, *ctx.abilityDef, SpellModFieldRadius, c.Radius)
				pull = applySpellModField(mods, *ctx.abilityDef, SpellModFieldPullStrength, c.PullStrength)
				dps = applySpellModField(mods, *ctx.abilityDef, SpellModFieldDamage, c.DamagePerSecond)
				speed = applySpellModField(mods, *ctx.abilityDef, SpellModFieldProjectileSpeed, c.ProjectileSpeed)
			}

			// Mirror legacy's exact spawn gate (resolveAbilityCastAtPointLocked:
			// `eff.PullStrength > 0 && def.Projectile != ""`) — a modifier that
			// zeroes (or reduces below zero) the folded pull strength cancels the
			// orb entirely, same as legacy. Mana was already spent one level up
			// (resolveAbilityProgramCastLocked), matching legacy's own
			// spend-then-maybe-no-op ordering.
			if pull <= 0 || c.Projectile == "" {
				ctx.trace("action_skipped", ctx.currentActionPath, map[string]any{"reason": "no_pull_or_projectile", "pullStrength": pull})
				return nil
			}

			// Shim def/eff built purely from the baked config (+ folded values)
			// — never from ctx.abilityDef's own mechanic fields, which are
			// cleared on a converted (schemaVersion 2) ability. See the file doc
			// comment.
			def := AbilityDef{
				ID:              ctx.AbilityID,
				Projectile:      c.Projectile,
				ProjectileScale: c.ProjectileScale,
				DamageType:      c.Type,
			}
			eff := EffectiveSpell{
				Radius:          radius,
				PullStrength:    pull,
				DamagePerSecond: dps,
				ProjectileSpeed: speed,
			}
			distance := c.CastRange.Resolve(caster)

			s.spawnArcaneOrbLocked(caster, ctx.CastPoint.X, ctx.CastPoint.Y, def, eff, distance)
			ctx.trace("vortex_launched", ctx.currentActionPath, map[string]any{
				"radius": radius, "pullStrength": pull, "damagePerSecond": dps, "speed": speed,
			})
			return nil
		},
	})
}
