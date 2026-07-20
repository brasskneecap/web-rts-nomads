package game

import (
	"encoding/json"
	"fmt"
)

// ═════════════════════════════════════════════════════════════════════════════
// apply_color_overlay — the full-body-tint sibling of apply_mark.
//
// A duration-AGNOSTIC status effect, authored ONLY inside an
// apply_status_duration's On Apply (on_action_complete) trigger, exactly like
// change_stat / apply_mark: it writes a chosen tint COLOR onto
// ctx.CurrentStatus (AbilityStatus.OverlayColor, ability_status.go), which the
// snapshot serializes (unitStatusOverlayColorLocked) and the client paints over
// the afflicted unit's sprite for the status's lifetime — generalizing the
// hardcoded chill/blue overlay so any authored status can tint its target
// (poison green, burn red, …). Cleared automatically on the container's expiry
// because it lives on the SAME AbilityStatus object the container ticks down.
// ═════════════════════════════════════════════════════════════════════════════

// Overlay colors are validated against hexColorPattern (perk_defs.go) — a #RGB
// / #RRGGBB / #RRGGBBAA hex value, the same narrow grammar the perk color
// picker already uses, so an authored value is unambiguous server-side and safe
// to hand straight to the client's canvas fillStyle.

// applyColorOverlayConfig is the decoded config for apply_color_overlay: the
// tint color, nothing else. No duration of its own (the enclosing
// apply_status_duration owns the lifetime).
type applyColorOverlayConfig struct {
	Color string `json:"color"`
}

func (applyColorOverlayConfig) actionConfig() {}

func init() {
	registerAction(ActionDescriptor{
		Type: ActionApplyColorOverlay,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c applyColorOverlayConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		// Held to the same "must live in an apply_status_duration's On Apply
		// trigger" placement rule as change_stat/apply_mark (walkAction,
		// ability_program_validate.go) — that is the only moment ctx.CurrentStatus
		// is bound, so authored anywhere else it would be inert. Color must be a
		// #RGB / #RRGGBB hex value.
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(applyColorOverlayConfig)
			var out []ValidationIssue
			if c.Color == "" {
				out = append(out, ValidationIssue{Code: "empty_required_property", Message: "apply_color_overlay requires a color", Severity: "error"})
			} else if !hexColorPattern.MatchString(c.Color) {
				out = append(out, ValidationIssue{Code: "invalid_property", Message: fmt.Sprintf("color %q must be a hex value like #96d6ff", c.Color), Severity: "error"})
			}
			return out
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "color", Label: "Overlay Color", Control: "color", Section: "Presentation"},
		}},
		// Execute never reads `targets` — it operates entirely on
		// ctx.CurrentStatus, exactly like apply_mark. A nil CurrentStatus is only
		// reachable by bypassing validation; treat it as a defensive no-op trace.
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, _ []int) []int {
			c := cfg.(applyColorOverlayConfig)
			if ctx.CurrentStatus == nil {
				ctx.trace("action_skipped", ctx.currentActionPath, map[string]any{"reason": "no_current_status"})
				return nil
			}
			ctx.CurrentStatus.OverlayColor = c.Color
			ctx.trace("color_overlay_applied", ctx.currentActionPath, map[string]any{"color": c.Color})
			return nil
		},
	})
}
