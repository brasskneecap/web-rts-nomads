package game

import "encoding/json"

// ability_exec_place_trap.go registers the place_trap action: an authored
// ability plants a trap by reusing the existing trap runtime primitive
// (plantTrapLocked, trap.go) — the SAME code path today's Trapper bronze
// perks (caltrops, fire_pit, explosive_trap, marker_trap) drive via
// tickTrapPlacementLocked. This is Phase 1 of the "Trapper traps →
// abilities" migration: it only ADDS the action so a future trap ability can
// use it. Nothing in production authors place_trap yet (no trap ability
// exists in the catalog), so this change is additive — existing perk-driven
// trap placement is untouched.
//
// Follows the summon_unit action's shape exactly (ability_exec_actions.go):
// a typed config struct decoded from the action's raw JSON config, an
// Execute that resolves the caster from ctx.CasterID, calls the existing Go
// seam, and returns targets unchanged (planting a trap doesn't produce a
// target-set output for the rest of the program).

// placeTrapConfig is the decoded config for the place_trap action. It
// carries everything trapConfigFromPerkLocked reads out of a bronze trap
// PerkDef's Config map, plus ConfigByRank overrides so an authored trap
// ability can scale its stats per unit rank the same way PerkDef.ConfigForRank
// does today (e.g. fire_pit's damagePerSecond/radius growing at Silver/Gold).
type placeTrapConfig struct {
	TrapType             string  `json:"trapType"`
	DurationSeconds      float64 `json:"durationSeconds"`
	PlaceIntervalSeconds float64 `json:"placeIntervalSeconds"`
	Radius               float64 `json:"radius,omitempty"`
	ExplosionRadius      float64 `json:"explosionRadius,omitempty"`
	TriggerRadius        float64 `json:"triggerRadius,omitempty"`
	DamagePerSecond      float64 `json:"damagePerSecond,omitempty"`
	SlowMultiplier       float64 `json:"slowMultiplier,omitempty"`
	BurstDamage          float64 `json:"burstDamage,omitempty"`
	MarkMultiplier       float64 `json:"markMultiplier,omitempty"`
	MarkDuration         float64 `json:"markDuration,omitempty"`
	// ConfigByRank carries per-rank overrides keyed by unit rank
	// ("silver"/"gold"); each inner map is a field name (matching this
	// struct's json tags) -> value. Mirrors PerkDef.ConfigByRank — e.g.
	// fire_pit's Silver rank scales up damagePerSecond and radius.
	// Missing rank (or an unrecognized field name inside it) is a no-op;
	// toTrapConfig only reads the handful of keys it knows about.
	ConfigByRank map[string]map[string]float64 `json:"configByRank,omitempty"`
}

func (placeTrapConfig) actionConfig() {}

// toTrapConfig builds a TrapConfig from c, applying any ConfigByRank[rank]
// overrides on top of the base fields — mirrors PerkDef.ConfigForRank's
// semantics (base values, overridden per-field by the rank block when
// present). rank == "" (or a rank with no override block) returns the base
// fields unchanged.
func (c placeTrapConfig) toTrapConfig(rank string) TrapConfig {
	tc := TrapConfig{
		TrapType:             c.TrapType,
		DurationSeconds:      c.DurationSeconds,
		PlaceIntervalSeconds: c.PlaceIntervalSeconds,
		Radius:               c.Radius,
		ExplosionRadius:      c.ExplosionRadius,
		TriggerRadius:        c.TriggerRadius,
		DamagePerSecond:      c.DamagePerSecond,
		SlowMultiplier:       c.SlowMultiplier,
		BurstDamage:          c.BurstDamage,
		MarkMultiplier:       c.MarkMultiplier,
		MarkDuration:         c.MarkDuration,
	}
	ov, ok := c.ConfigByRank[rank]
	if !ok {
		return tc
	}
	if v, ok := ov["durationSeconds"]; ok {
		tc.DurationSeconds = v
	}
	if v, ok := ov["placeIntervalSeconds"]; ok {
		tc.PlaceIntervalSeconds = v
	}
	if v, ok := ov["radius"]; ok {
		tc.Radius = v
	}
	if v, ok := ov["explosionRadius"]; ok {
		tc.ExplosionRadius = v
	}
	if v, ok := ov["triggerRadius"]; ok {
		tc.TriggerRadius = v
	}
	if v, ok := ov["damagePerSecond"]; ok {
		tc.DamagePerSecond = v
	}
	if v, ok := ov["slowMultiplier"]; ok {
		tc.SlowMultiplier = v
	}
	if v, ok := ov["burstDamage"]; ok {
		tc.BurstDamage = v
	}
	if v, ok := ov["markMultiplier"]; ok {
		tc.MarkMultiplier = v
	}
	if v, ok := ov["markDuration"]; ok {
		tc.MarkDuration = v
	}
	return tc
}

func init() {
	registerAction(ActionDescriptor{
		Type: ActionPlaceTrap,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c placeTrapConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(placeTrapConfig)
			if c.TrapType == "" {
				return []ValidationIssue{{Code: "empty_required_property", Message: "place_trap requires trapType", Severity: "error"}}
			}
			return nil
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "trapType", Label: "Trap Type", Control: "text", Section: "Properties"},
			{Key: "durationSeconds", Label: "Duration", Control: "duration", Section: "Timing"},
			{Key: "placeIntervalSeconds", Label: "Place Interval", Control: "duration", Section: "Timing"},
			{Key: "radius", Label: "Radius", Control: "number", Section: "Properties"},
			{Key: "explosionRadius", Label: "Explosion Radius", Control: "number", Section: "Properties"},
			{Key: "triggerRadius", Label: "Trigger Radius", Control: "number", Section: "Properties"},
			{Key: "damagePerSecond", Label: "Damage Per Second", Control: "number", Section: "Properties"},
			{Key: "slowMultiplier", Label: "Slow Multiplier", Control: "percentage", Section: "Properties"},
			{Key: "burstDamage", Label: "Burst Damage", Control: "number", Section: "Properties"},
			{Key: "markMultiplier", Label: "Mark Multiplier", Control: "percentage", Section: "Properties"},
			{Key: "markDuration", Label: "Mark Duration", Control: "duration", Section: "Timing"},
		}},
		// Execute plants the trap via the existing plantTrapLocked seam — it
		// already offsets placement toward the caster's nearest enemy
		// (trapPlacementOffsetLocked) and appends to s.Traps, so this action
		// does no placement geometry of its own. Traps aren't a target-set
		// output for the rest of the program, so targets pass through
		// unchanged regardless of whether the plant happened.
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(placeTrapConfig)
			caster := s.getUnitByIDLocked(ctx.CasterID)
			if caster == nil {
				return targets
			}
			tc := c.toTrapConfig(caster.Rank)
			s.plantTrapLocked(caster, tc)
			ctx.trace("trap_placed", ctx.currentActionPath, map[string]any{"trapType": c.TrapType})
			return targets
		},
	})
}
