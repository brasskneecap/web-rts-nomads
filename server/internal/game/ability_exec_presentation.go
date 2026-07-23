package game

import "encoding/json"

// ─────────────────────────────────────────────────────────────────────────────
// play_presentation executor (Phase 6b, Task 1).
//
// play_presentation's compiled config has two shapes (both already defined in
// ability_compile.go, where the compiler builds them):
//
//   - on-target (playPresentationOnTargetConfig): {asset, oncePerTarget} with
//     Input["attach"] pointing at a resolved unit-set context key (e.g. heal's
//     trailing healing_glow, attached to "healTargets").
//   - at-point (playPresentationAtPointConfig): {asset, position, scale,
//     renderLayer, presentationId} anchored to a world position (e.g.
//     shatter's ground burst, meteor's falling sprite).
//
// playPresentationAtPointConfig carries every field this executor needs, so
// Decode always unmarshals into it rather than introducing a third config
// type: on-target JSON's "asset" decodes normally and its "oncePerTarget" key
// is simply dropped (unrecognized field) — Execute doesn't need that value,
// since looping the resolved target set once per unit already IS "once per
// target".
//
// The distinguishing signal between the two shapes is NOT config shape (a
// hand-authored at-point config could legitimately omit Position, per the
// fallback rule below) — it is whether the ACTION declared Input["attach"],
// exactly like deal_damage/restore_health are handed a pre-resolved
// `targets []int` via Input["targets"]. ActionDescriptor.Execute's signature
// doesn't carry the AbilityActionDef, so this executor consults
// ctx.currentActionHasAttachInput, a small save/restore field
// executeActionLocked sets alongside ctx.currentActionPath (ability_exec.go)
// specifically so this one action type can make that distinction without
// widening the shared Execute signature for every registered action. See
// resolveActionTargetsLocked / executeActionLocked in ability_exec.go for the
// small, additive changes that back this (Input["attach"] resolves the same
// way Input["targets"] does; the boolean records which one fired).
//
// The at-point config's Position ContextRef is resolved via the shared
// resolveContextPositionLocked (ability_zone.go) — the same helper create_zone
// uses — rather than a bespoke helper here, so both camelCase ("castPoint",
// "impactPosition", "zoneCenter") and the program model's canonical
// snake_case origin strings (OriginCastPoint/OriginImpactPosition/
// OriginZoneCenter, ability_program.go) resolve identically everywhere a
// position ContextRef appears. An unrecognized/empty key falls back to
// ctx.CastPoint (the common case: shatter/meteor always author "castPoint"
// explicitly, but a hand-authored config that omits position should still do
// something sane rather than render at the origin).
// ─────────────────────────────────────────────────────────────────────────────

func (playPresentationAtPointConfig) actionConfig() {}

func init() {
	registerAction(ActionDescriptor{
		Type: ActionPlayPresentation,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c playPresentationAtPointConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, scope ValidationScope) []ValidationIssue {
			c := cfg.(playPresentationAtPointConfig)
			var out []ValidationIssue
			if c.Asset == "" && c.PresentationID == "" {
				out = append(out, ValidationIssue{
					Code:     "missing_presentation_content",
					Message:  "play_presentation has neither asset nor presentation",
					Severity: "warning",
				})
			}
			// bindToStatusDuration binds the visual to ctx.CurrentStatus, which is
			// only bound inside an apply_status_duration's On Apply trigger (the
			// same gate change_stat/apply_mark are held to). Anywhere else it is
			// inert — reject it rather than ship an inert authorable field.
			if c.BindToStatusDuration && !scope.InsideStatusDuration {
				out = append(out, ValidationIssue{
					Code:     "invalid_placement",
					Message:  "bindToStatusDuration is only valid in an apply_status_duration's On Apply (on_action_complete) trigger — it binds the visual to the enclosing status's lifetime",
					Severity: "error",
				})
			}
			if c.BindToStatusDuration && c.Asset == "" {
				out = append(out, ValidationIssue{
					Code:     "empty_required_property",
					Message:  "bindToStatusDuration requires an asset (the unit-anchored visual to attach)",
					Severity: "error",
				})
			}
			return out
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			// `asset` uses the `animation` control — the same AnimationPicker as
			// create_zone's visual — so a presentation can be any source (effect /
			// projectile / beam / object@state / uploaded image), not just an
			// effect id. The at-point Execute renders a server-def'd effect via the
			// EffectSnapshot path and everything else via a transient decal.
			{Key: "asset", Label: "Asset", Control: "animation", Section: "Presentation"},
			{Key: "position", Label: "Position", Control: "context_ref", Section: "Presentation"},
			{Key: "scale", Label: "Scale", Control: "number", Section: "Presentation"},
			{Key: "duration", Label: "Decal Duration", Control: "duration", Section: "Presentation"},
			{Key: "renderLayer", Label: "Render Layer", Control: "enum", Options: []string{"in_front_of_units", "behind_units"}, Section: "Presentation"},
			{Key: "presentationId", Label: "Presentation ID", Control: "text", Section: "Presentation"},
			// bindToStatusDuration: attach the asset to the afflicted unit for the
			// enclosing status's duration (valid only in an apply_status_duration
			// On Apply trigger — see the config field's doc comment). The
			// persistent-visual half of a data-authored status (burn's fire, …).
			{Key: "bindToStatusDuration", Label: "Last for Status Duration", Control: "boolean", Section: "Presentation"},
		}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(playPresentationAtPointConfig)

			if c.BindToStatusDuration {
				// Status-bound visual: attach the effect to the afflicted unit
				// (ctx.CurrentStatus.TargetUnitID) for the status's Remaining
				// duration — operates on ctx.CurrentStatus directly, exactly like
				// change_stat/apply_mark, so it ignores `targets`. A nil
				// CurrentStatus is only reachable by bypassing validation; treat
				// it as a defensive no-op trace, not a panic.
				if ctx.CurrentStatus == nil {
					ctx.trace("action_skipped", ctx.currentActionPath, map[string]any{"reason": "no_current_status"})
					return targets
				}
				u := s.getUnitByIDLocked(ctx.CurrentStatus.TargetUnitID)
				// ALIVE, not just resolvable: a unit that took lethal damage
				// earlier in this same Update pass is still in unitsByID until
				// drainPendingDeathsLocked runs, so a re-applying status
				// (a zone's per-tick refresh) would otherwise pin a fresh
				// flame onto a corpse right after tickEffectsLocked dropped it.
				if s.unitIsAliveLocked(u) {
					// Refresh-not-restack: a "refresh"-stacking status re-runs its On
					// Apply every time the zone re-applies it, so queueing here
					// unconditionally stacked one flame per tick. See
					// refreshEffectOnUnitForDurationLocked.
					s.refreshEffectOnUnitForDurationLocked(u, animationEffectRef(c.Asset), ctx.CurrentStatus.Remaining, c.Scale)
					ctx.trace("presentation_played", ctx.currentActionPath, map[string]any{
						"asset":    c.Asset,
						"unit":     u.ID,
						"duration": ctx.CurrentStatus.Remaining,
						"bound":    true,
					})
				}
				return targets
			}

			if ctx.currentActionHasAttachInput {
				// On-target: attach a per-unit effect to every resolved target
				// (targets was already resolved from Input["attach"] by
				// resolveActionTargetsLocked, the same seam deal_damage/
				// restore_health use for Input["targets"]).
				hit := make([]int, 0, len(targets))
				for _, id := range targets {
					u := s.getUnitByIDLocked(id)
					if u == nil {
						continue
					}
					s.playEffectOnUnitLocked(u, animationEffectRef(c.Asset))
					hit = append(hit, id)
				}
				ctx.trace("presentation_played", ctx.currentActionPath, map[string]any{
					"asset":   c.Asset,
					"targets": len(hit),
				})
				return hit
			}

			// At-point: one animation at a resolved world position. Targets pass
			// through unchanged (this action doesn't filter/produce a target
			// set, matching wait/store_targets' pass-through Execute pattern).
			pos := s.resolveContextPositionLocked(ctx, &c.Position, ctx.CastPoint)
			// Layer: presentations read as a burst over the target, so default to
			// IN FRONT of units (matching the effect renderer's above-units pass);
			// only "behind_units" pushes it to the ground layer.
			aboveUnits := c.RenderLayer != "behind_units"
			s.playAnimationAtPointLocked(ctx, c.Asset, pos, c.Scale, c.Duration, aboveUnits)

			if c.PresentationID != "" && ctx.program != nil {
				for i := range ctx.program.Presentations {
					pres := &ctx.program.Presentations[i]
					if pres.ID != c.PresentationID {
						continue
					}
					for _, trig := range pres.Triggers {
						if trig.Type == TriggerOnAnimationMarker {
							s.scheduleMarkerTriggersLocked(ctx, *pres)
							break
						}
					}
					break
				}
			}

			ctx.trace("presentation_played", ctx.currentActionPath, map[string]any{
				"asset":          c.Asset,
				"presentationId": c.PresentationID,
			})
			return targets
		},
	})
}
