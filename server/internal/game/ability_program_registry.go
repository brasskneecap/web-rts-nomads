package game

import (
	"encoding/json"
	"math"
	"sort"
)

// ValidationIssue is one structured problem found while validating an ability
// program, mapped back to a UI card/field via Path. Defined here (the registry)
// so descriptors can return issues; the program validator (later task) reuses it.
type ValidationIssue struct {
	Path     string `json:"path"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Severity string `json:"severity"` // "error" | "warning"
}

// ActionConfig is the decoded, typed config for one action. Concrete per type.
type ActionConfig interface{ actionConfig() }

// ValidationScope carries the context available to an action at its position in
// the program, for validation (e.g. which named context keys / prior outputs
// exist). Populated by the program validator in a later task; empty is valid.
type ValidationScope struct {
	AvailableContext map[string]bool
	PriorOutputs     map[string]bool
}

// SchemaField describes ONE editable control for the schema-driven editor.
type SchemaField struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Control string   `json:"control"` // number|text|boolean|enum|multiselect|asset|sentinel_number|duration|percentage|target_query|context_ref|animation_marker|nested_triggers
	Options []string `json:"options,omitempty"`
	Section string   `json:"section,omitempty"` // Basic|Targeting|Timing|Properties|Presentation|Conditions|Advanced|Notes
}

// ActionFieldSchema is the ordered set of controls the inspector renders for an
// action type. Emitted to the editor in a later phase.
type ActionFieldSchema struct {
	Fields []SchemaField `json:"fields"`
}

// ActionDescriptor is the single registry entry per action type. Decode + Validate
// + Schema land in Phase 2; Execute + Describe are added in later phases.
type ActionDescriptor struct {
	Type     ActionType
	Decode   func(json.RawMessage) (ActionConfig, error)
	Validate func(cfg ActionConfig, scope ValidationScope) []ValidationIssue
	Schema   ActionFieldSchema

	// Execute applies the action. `targets` is the resolved target-set (unit IDs)
	// the executor prepared from the action's Target query / Input refs. It returns
	// the action's output unit IDs (e.g. select_targets returns its selection,
	// deal_damage returns the units actually hit) which the executor binds per
	// action.Outputs. A nil Execute ⇒ the action is a runtime no-op (e.g.
	// play_sound / change_render_layer / camera_shake, still deferred to a
	// later phase — play_presentation itself is registered as of Phase 6b
	// Task 1, ability_exec_presentation.go). Added in Phase 3.
	Execute func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int
}

var actionRegistry = map[ActionType]ActionDescriptor{}

func registerAction(d ActionDescriptor) { actionRegistry[d.Type] = d }

func lookupActionDescriptor(t ActionType) (ActionDescriptor, bool) {
	d, ok := actionRegistry[t]
	return d, ok
}

// ActionSchema is one action type's editor metadata: its inspector controls and
// whether the composable runtime can execute it today (Execute != nil). The editor
// lists non-runnable actions as "display-only" (deferred mechanics).
type ActionSchema struct {
	Type     ActionType    `json:"type"`
	Fields   []SchemaField `json:"fields"`
	Runnable bool          `json:"runnable"`
}

// ActionSchemas returns editor metadata for every action type in allActionTypes,
// sorted by Type for determinism. A type with a registered descriptor contributes
// its Schema.Fields + Runnable=(Execute!=nil); a type with no descriptor (deferred,
// e.g. custom) contributes empty Fields + Runnable=false so the editor can
// still list it.
func ActionSchemas() []ActionSchema {
	out := make([]ActionSchema, 0, len(allActionTypes))
	for _, t := range allActionTypes {
		// Fields defaults to an empty (non-nil) slice so a deferred action type
		// (no registered descriptor) serializes as "fields":[] rather than
		// "fields":null — the editor does .map/.length on it unconditionally.
		as := ActionSchema{Type: t, Fields: []SchemaField{}}
		if d, ok := lookupActionDescriptor(t); ok {
			if len(d.Schema.Fields) > 0 {
				as.Fields = d.Schema.Fields
			}
			as.Runnable = d.Execute != nil
		}
		out = append(out, as)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

// ── deal_damage ──
type dealDamageConfig struct {
	Amount int        `json:"amount"`
	Type   DamageType `json:"type"`
	Radius float64    `json:"radius,omitempty"`
}

func (dealDamageConfig) actionConfig() {}

// ── restore_health ──
type restoreHealthConfig struct {
	Amount int        `json:"amount"`
	School DamageType `json:"school,omitempty"`
}

func (restoreHealthConfig) actionConfig() {}

// ── select_targets: config is empty; the TargetQueryDef on the action carries it ──
type selectTargetsConfig struct{}

func (selectTargetsConfig) actionConfig() {}

func init() {
	registerAction(ActionDescriptor{
		Type: ActionDealDamage,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c dealDamageConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(dealDamageConfig)
			var out []ValidationIssue
			if c.Amount <= 0 {
				out = append(out, ValidationIssue{Code: "empty_required_property", Message: "deal_damage requires amount > 0", Severity: "error"})
			}
			if c.Type != "" && !IsValidDamageType(c.Type) {
				out = append(out, ValidationIssue{Code: "invalid_damage_type", Message: "unknown damage type " + string(c.Type), Severity: "error"})
			}
			return out
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "amount", Label: "Amount", Control: "number", Section: "Properties"},
			{Key: "type", Label: "Damage Type", Control: "enum", Section: "Properties"},
			{Key: "radius", Label: "Radius", Control: "number", Section: "Targeting"},
		}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(dealDamageConfig)
			dt := c.Type.OrPhysical()
			// Scale by the caster's spell-modifiers for this ability's school/
			// tags, at parity with the legacy effectiveSpellLocked path. Legacy
			// burn/DoT is applied raw; zone-tick ctx has abilityDef==nil so burn
			// stays raw — parity.
			caster := s.getUnitByIDLocked(ctx.CasterID)
			amount := c.Amount
			if caster != nil && ctx.abilityDef != nil {
				amount = s.effectiveAbilityDamageLocked(caster, *ctx.abilityDef, c.Amount)
			}
			// Honour a caller-supplied reduced/boosted-effectiveness cast (e.g.
			// unstable_magic's free proc — see EffectiveSpell.DamageEffectivenessMultiplier
			// / resolveAbilityProgramCastLocked). A multiplier of 1.0 (the default
			// for every ordinary cast) is a no-op.
			if m := ctx.effectiveDamageMultiplier(); m != 1.0 {
				amount = int(math.Round(float64(amount) * m))
			}
			hit := make([]int, 0, len(targets))
			for _, id := range targets {
				u := s.getUnitByIDLocked(id)
				if u == nil || u.HP <= 0 {
					continue
				}
				s.applyUnitDamageWithSourceLocked(u, amount, DamageSource{AttackerUnitID: ctx.CasterID, Kind: "ability", DamageType: dt})
				hit = append(hit, id)
				ctx.trace("damage_applied", ctx.currentActionPath, map[string]any{"unit": id, "amount": amount, "type": string(dt)})
			}
			return hit
		},
	})
	registerAction(ActionDescriptor{
		Type: ActionRestoreHealth,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c restoreHealthConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(restoreHealthConfig)
			if c.Amount <= 0 {
				return []ValidationIssue{{Code: "empty_required_property", Message: "restore_health requires amount > 0", Severity: "error"}}
			}
			if c.School != "" && !IsValidDamageType(c.School) {
				return []ValidationIssue{{Code: "invalid_damage_type", Message: "unknown school " + string(c.School), Severity: "error"}}
			}
			return nil
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "amount", Label: "Amount", Control: "number", Section: "Properties"},
			{Key: "school", Label: "School", Control: "enum", Section: "Properties"},
		}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(restoreHealthConfig)
			caster := s.getUnitByIDLocked(ctx.CasterID)
			if caster == nil {
				return nil
			}
			// Divine Healer (silver cleric) scales every heal amount produced by
			// the caster, at parity with the legacy resolveAbilityCastOnTargetLocked
			// path (which scales def.HealAmount by the same multiplier). base is
			// THIS action's configured amount, not necessarily def.HealAmount — see
			// effectiveAbilityHealLocked's doc comment, mirroring
			// effectiveAbilityDamageLocked's "action's amount, not def's" contract.
			amount := c.Amount
			if ctx.abilityDef != nil {
				amount = s.effectiveAbilityHealLocked(caster, *ctx.abilityDef, c.Amount)
			}
			healed := make([]int, 0, len(targets))
			for _, id := range targets {
				u := s.getUnitByIDLocked(id)
				if u == nil || u.HP <= 0 {
					continue
				}
				s.applyClericHealLocked(caster, u, amount, healMetaPrimaryAbility())
				// Perk hook: fire once per resolved (caster,target) pair, matching
				// legacy resolveAbilityCastOnTargetLocked's onPerkAbilityResolvedLocked
				// call exactly — same trigger condition (the hook itself gates on
				// def.Category==heal), same once-per-target cardinality, and
				// immediately after the heal is applied, same as legacy's ordering.
				// Guarded on ctx.abilityDef!=nil (unset for zone-tick / non-cast
				// contexts) since legacy only ever calls this from cast resolution.
				if ctx.abilityDef != nil {
					s.onPerkAbilityResolvedLocked(caster, *ctx.abilityDef, u)
				}
				healed = append(healed, id)
				ctx.trace("healing_applied", ctx.currentActionPath, map[string]any{"unit": id, "amount": amount})
			}
			return healed
		},
	})
	registerAction(ActionDescriptor{
		Type:     ActionSelectTargets,
		Decode:   func(b json.RawMessage) (ActionConfig, error) { return selectTargetsConfig{}, nil },
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue { return nil },
		Schema:   ActionFieldSchema{Fields: []SchemaField{{Key: "target", Label: "Target Query", Control: "target_query", Section: "Targeting"}}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, _ ActionConfig, targets []int) []int {
			ctx.trace("targets_selected", ctx.currentActionPath, map[string]any{"count": len(targets)})
			return targets
		},
	})
}
