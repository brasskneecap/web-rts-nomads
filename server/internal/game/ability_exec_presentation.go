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
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(playPresentationAtPointConfig)
			if c.Asset == "" && c.PresentationID == "" {
				return []ValidationIssue{{
					Code:     "missing_presentation_content",
					Message:  "play_presentation has neither asset nor presentation",
					Severity: "warning",
				}}
			}
			return nil
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "asset", Label: "Asset", Control: "asset", Section: "Presentation"},
			{Key: "position", Label: "Position", Control: "context_ref", Section: "Presentation"},
			{Key: "scale", Label: "Scale", Control: "number", Section: "Presentation"},
			{Key: "renderLayer", Label: "Render Layer", Control: "enum", Section: "Presentation"},
			{Key: "presentationId", Label: "Presentation ID", Control: "text", Section: "Presentation"},
		}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(playPresentationAtPointConfig)

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
					s.playEffectOnUnitLocked(u, c.Asset)
					hit = append(hit, id)
				}
				ctx.trace("presentation_played", ctx.currentActionPath, map[string]any{
					"asset":   c.Asset,
					"targets": len(hit),
				})
				return hit
			}

			// At-point: one effect at a resolved world position. Targets pass
			// through unchanged (this action doesn't filter/produce a target
			// set, matching wait/store_targets' pass-through Execute pattern).
			pos := s.resolveContextPositionLocked(ctx, &c.Position, ctx.CastPoint)
			s.playEffectAtPointLocked(c.Asset, pos.X, pos.Y, c.Scale)

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
