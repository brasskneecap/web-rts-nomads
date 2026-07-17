package game

import "encoding/json"

// ability_exec_actions.go registers the remaining Phase 3 action executors:
// summon_unit, apply_force, apply_status, remove_status, modify_resource.
// Each adapts an EXISTING gameplay seam (spawnSummonedUnitLocked,
// applyPullLocked, applyProcSlowLocked/ApplyStunLocked/applyProcBurnLocked,
// spendUnitManaLocked/addUnitManaLocked) rather than reimplementing it. Follows
// the deal_damage/restore_health/select_targets pattern in
// ability_program_registry.go exactly. NOT wired into the live cast path —
// only tests call these executors in Phase 3.

// ── summon_unit ──────────────────────────────────────────────────────────

type summonUnitConfig struct {
	UnitType string `json:"unitType"`
	Count    int    `json:"count"`
}

func (summonUnitConfig) actionConfig() {}

// ── apply_force ──────────────────────────────────────────────────────────

type applyForceConfig struct {
	Strength float64 `json:"strength"`
	Duration float64 `json:"duration"`
}

func (applyForceConfig) actionConfig() {}

// ── apply_status ─────────────────────────────────────────────────────────

type applyStatusConfig struct {
	Status     string     `json:"status"`
	Multiplier float64    `json:"multiplier"`
	Duration   float64    `json:"duration"`
	DPS        float64    `json:"dps"`
	School     DamageType `json:"school"`
}

func (applyStatusConfig) actionConfig() {}

// ── remove_status ────────────────────────────────────────────────────────

type removeStatusConfig struct {
	Status string `json:"status"`
}

func (removeStatusConfig) actionConfig() {}

// ── modify_resource ──────────────────────────────────────────────────────

type modifyResourceConfig struct {
	Resource string `json:"resource"`
	Amount   int    `json:"amount"`
}

func (modifyResourceConfig) actionConfig() {}

func init() {
	registerAction(ActionDescriptor{
		Type: ActionSummonUnit,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c summonUnitConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(summonUnitConfig)
			if c.UnitType == "" {
				return []ValidationIssue{{Code: "empty_required_property", Message: "summon_unit requires unitType", Severity: "error"}}
			}
			return nil
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "unitType", Label: "Unit Type", Control: "text", Section: "Properties"},
			{Key: "count", Label: "Count", Control: "number", Section: "Properties"},
		}},
		// Execute fans the summon out via the existing spawnSummonedUnitLocked
		// seam (raise_skeleton's row-fanout behavior) — no reimplementation.
		// Summons aren't a target output for the rest of the program, so this
		// always returns nil regardless of Count/success.
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(summonUnitConfig)
			caster := s.getUnitByIDLocked(ctx.CasterID)
			if caster == nil {
				return nil
			}
			count := c.Count
			if count < 1 {
				count = 1
			}
			shim := AbilityDef{SummonUnitType: c.UnitType, SummonCount: count}
			s.spawnSummonedUnitLocked(caster, shim)
			ctx.trace("unit_summoned", ctx.currentActionPath, map[string]any{"unitType": c.UnitType, "count": count})
			return nil
		},
	})

	registerAction(ActionDescriptor{
		Type: ActionApplyForce,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c applyForceConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(applyForceConfig)
			if c.Strength <= 0 || c.Duration <= 0 {
				return []ValidationIssue{{Code: "empty_required_property", Message: "apply_force requires strength > 0 and duration > 0", Severity: "error"}}
			}
			return nil
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "strength", Label: "Strength", Control: "number", Section: "Properties"},
			{Key: "duration", Label: "Duration", Control: "duration", Section: "Timing"},
		}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(applyForceConfig)
			caster := s.getUnitByIDLocked(ctx.CasterID)
			if caster == nil {
				return nil
			}
			affected := make([]int, 0, len(targets))
			for _, id := range targets {
				u := s.getUnitByIDLocked(id)
				if u == nil || u.HP <= 0 {
					continue
				}
				// TODO(phase-3b): configurable pull origin (impact/zone center).
				s.applyPullLocked(u, caster.X, caster.Y, c.Strength, c.Duration)
				affected = append(affected, id)
				ctx.trace("force_applied", ctx.currentActionPath, map[string]any{"unit": id, "strength": c.Strength, "duration": c.Duration})
			}
			return affected
		},
	})

	registerAction(ActionDescriptor{
		Type: ActionApplyStatus,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c applyStatusConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(applyStatusConfig)
			if c.Status == "" || c.Duration <= 0 {
				return []ValidationIssue{{Code: "empty_required_property", Message: "apply_status requires status and duration > 0", Severity: "error"}}
			}
			return nil
		},
		// Phase 3 supports only these three CC primitives (the ones with an
		// existing generic gameplay seam). Author-defined/custom statuses are a
		// later phase.
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "status", Label: "Status", Control: "enum", Options: []string{"slow", "stun", "burn"}, Section: "Properties"},
			{Key: "multiplier", Label: "Multiplier", Control: "percentage", Section: "Properties"},
			{Key: "duration", Label: "Duration", Control: "duration", Section: "Timing"},
			{Key: "dps", Label: "DPS", Control: "number", Section: "Properties"},
			{Key: "school", Label: "School", Control: "enum", Section: "Properties"},
		}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(applyStatusConfig)
			applied := make([]int, 0, len(targets))
			for _, id := range targets {
				switch c.Status {
				case "slow":
					// applyProcSlowLocked routes cold -> chill track; an empty/other
					// school routes physical (see combat_ai_cc.go).
					s.applyProcSlowLocked(id, c.Multiplier, c.Duration, c.School)
				case "stun":
					s.ApplyStunLocked(id, c.Duration)
				case "burn":
					s.applyProcBurnLocked(id, c.DPS, c.Duration, ctx.CasterID)
				default:
					ctx.trace("action_skipped", ctx.currentActionPath, map[string]any{"reason": "unknown_status", "status": c.Status})
					continue
				}
				applied = append(applied, id)
				ctx.trace("status_applied", ctx.currentActionPath, map[string]any{"unit": id, "status": c.Status})
			}
			return applied
		},
	})

	registerAction(ActionDescriptor{
		Type: ActionRemoveStatus,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c removeStatusConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(removeStatusConfig)
			if c.Status == "" {
				return []ValidationIssue{{Code: "empty_required_property", Message: "remove_status requires status", Severity: "error"}}
			}
			return nil
		},
		// Phase 3 can only clear the generic CC tracks (slow/stun) that
		// apply_status can set. Author-defined/custom statuses are a later phase.
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "status", Label: "Status", Control: "enum", Options: []string{"slow", "stun"}, Section: "Properties"},
		}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(removeStatusConfig)
			removed := make([]int, 0, len(targets))
			for _, id := range targets {
				u := s.getUnitByIDLocked(id)
				if u == nil {
					continue
				}
				switch c.Status {
				case "slow":
					u.SlowedRemaining = 0
					u.SlowedMultiplier = 0
					u.ColdSlowedRemaining = 0
					u.ColdSlowedMultiplier = 0
				case "stun":
					u.StunnedRemaining = 0
				default:
					ctx.trace("action_skipped", ctx.currentActionPath, map[string]any{"reason": "unknown_status", "status": c.Status})
					continue
				}
				removed = append(removed, id)
				ctx.trace("status_removed", ctx.currentActionPath, map[string]any{"unit": id, "status": c.Status})
			}
			return removed
		},
	})

	registerAction(ActionDescriptor{
		Type: ActionModifyResource,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c modifyResourceConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(modifyResourceConfig)
			if c.Resource == "" {
				return []ValidationIssue{{Code: "empty_required_property", Message: "modify_resource requires resource", Severity: "error"}}
			}
			return nil
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "resource", Label: "Resource", Control: "enum", Options: []string{"mana"}, Section: "Properties"},
			{Key: "amount", Label: "Amount", Control: "number", Section: "Properties"},
		}},
		// Execute acts on the CASTER, not the resolved target set — a resource
		// is a property of the unit paying/gaining it, not of an external target.
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(modifyResourceConfig)
			caster := s.getUnitByIDLocked(ctx.CasterID)
			if caster == nil {
				return nil
			}
			if c.Resource != "mana" {
				ctx.trace("action_skipped", ctx.currentActionPath, map[string]any{"reason": "unknown_resource", "resource": c.Resource})
				return nil
			}
			switch {
			case c.Amount < 0:
				if !s.spendUnitManaLocked(caster, -c.Amount) {
					ctx.trace("action_skipped", ctx.currentActionPath, map[string]any{"reason": "insufficient_mana", "amount": c.Amount})
					return nil
				}
			case c.Amount > 0:
				s.addUnitManaLocked(caster, c.Amount)
			}
			ctx.trace("resource_modified", ctx.currentActionPath, map[string]any{"resource": c.Resource, "amount": c.Amount})
			return nil
		},
	})
}
