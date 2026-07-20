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
//
// TargetQueryFields is ONLY meaningful on a field whose Control is
// "target_query" (e.g. select_targets' "target", ability_program_registry.go
// below): it names, in display order, the TargetQueryDef (ability_program.go)
// sub-fields THIS action's targeting shape actually uses, out of the 13 that
// exist — select_targets IS the free-form scene-query/narrowing action and
// declares targetQueryFieldsFull, while launch_projectile/channel_beam each
// resolve to exactly ONE unit to fly at/channel and declare the much
// narrower targetQueryFieldsSourceOnly (see that var's doc comment for the
// full argument). Neither declares MinCount/Filters/RequireLineOfSight
// (decoded and validated, but not yet enforced by
// resolveTargetQueryLocked/applyTargetFiltersLocked — ability_exec_targeting.go's
// own TODO(phase-3b) notes — so declaring them here would just re-create the
// exact "advertises a field the action can't use" bug this mechanism exists
// to fix). An action with NO target_query field declares no targeting shape
// at all: the editor shows no Targeting section for it.
//
// ShowWhen is a minimal, data-driven conditional-visibility gate: when
// non-nil, the field is only shown in the inspector while ShowWhen evaluates
// true against the action's OWN config (see FieldCondition's doc comment).
// nil means "always shown" (every field declared before this mechanism
// existed keeps that behavior for free — the zero value is the common case).
type SchemaField struct {
	Key               string          `json:"key"`
	Label             string          `json:"label"`
	Control           string          `json:"control"` // number|text|boolean|enum|multiselect|asset|sentinel_number|duration|percentage|target_query|context_ref|animation_marker
	Options           []string        `json:"options,omitempty"`
	Section           string          `json:"section,omitempty"` // Basic|Targeting|Timing|Properties|Presentation|Conditions|Advanced|Notes
	TargetQueryFields []string        `json:"targetQueryFields,omitempty"`
	ShowWhen          *FieldCondition `json:"showWhen,omitempty"`
}

// FieldCondition is a minimal, data-driven visibility gate for a SchemaField:
// "show this field only when the action's own config[Key] <Op> Value" —
// evaluated against the SAME config object the wire already carries (no
// second lookup, no cross-action reference). This is deliberately NOT a
// rules engine: no boolean AND/OR/NOT composition of multiple conditions, no
// comparing one config field against another. If a future field ever needs
// more than "one config field vs. one constant," extend Op's vocabulary
// before reaching for a nested structure.
//
// Op reuses (a subset of) AbilityConditionDef's existing comparison
// vocabulary (evaluateOneConditionLocked, ability_exec_flow.go — also
// surfaced to the editor as ProgramEnums()["conditionOps"]) rather than
// inventing a second one: eq|ne|lt|lte|gt|gte. eq/ne compare config[Key]
// against Value as plain JSON scalars (numbers, strings, or booleans alike —
// needed for e.g. launch_projectile's travelMode == "direction" gate, a
// string field, alongside chainCount > 0, a numeric one). lt/lte/gt/gte are
// numeric-only (ordering a string is never meaningful here), matching every
// current use (chainCount > 0). Value is json.RawMessage so both shapes
// round-trip through the SAME field without a Go-side union type.
type FieldCondition struct {
	Key   string          `json:"key"`
	Op    string          `json:"op"`
	Value json.RawMessage `json:"value,omitempty"`
}

// FieldConditionMatches evaluates cond against config — a decoded JSON
// object in exactly the shape the client's evaluator reads action.config as
// (JSON scalars decode to float64/string/bool in both Go's map[string]any
// and JS/TS alike), so this doubles as a Go-side reference implementation of
// what the client must do, not just a server-only convenience.
//
// A Key absent from config resolves to the ZERO VALUE OF Value's OWN TYPE
// (0 for a number, "" for a string, false for a bool) rather than
// "unevaluable" — an omitempty field the author hasn't touched yet (e.g.
// launch_projectile's chainCount before it's ever set, or travelMode before
// it's ever authored away from its "to_target" default) must gate
// identically to one explicitly authored as that zero value. An unparseable
// Value, a type mismatch between config[Key] and Value, or an unrecognized
// Op all conservatively resolve to false (hide the field), mirroring
// evaluateOneConditionLocked's conservative-fail convention.
func FieldConditionMatches(cond FieldCondition, config map[string]any) bool {
	var want any
	if err := json.Unmarshal(cond.Value, &want); err != nil {
		return false
	}
	got, present := config[cond.Key]
	if !present {
		switch want.(type) {
		case string:
			got = ""
		case bool:
			got = false
		default:
			got = 0.0
		}
	}

	switch cond.Op {
	case "eq":
		eq, comparable := scalarEqual(got, want)
		return comparable && eq
	case "ne":
		eq, comparable := scalarEqual(got, want)
		return comparable && !eq
	case "lt", "lte", "gt", "gte":
		gotNum, ok := got.(float64)
		if !ok {
			return false
		}
		wantNum, ok := want.(float64)
		if !ok {
			return false
		}
		switch cond.Op {
		case "lt":
			return gotNum < wantNum
		case "lte":
			return gotNum <= wantNum
		case "gt":
			return gotNum > wantNum
		default: // "gte"
			return gotNum >= wantNum
		}
	default:
		return false
	}
}

// scalarEqual compares a and b for equality, but ONLY when both are one of
// the JSON scalar kinds (nil, bool, float64, string) Go's `==` can safely
// compare on interface values. Guards against a run-time panic: comparing
// two interface values with identical dynamic types is only safe when that
// type is itself comparable — a slice or map decoded from a malformed/
// unexpected showWhen.value or config[Key] would panic on a bare `==`
// otherwise. The second return value reports whether the comparison was
// even attempted; false means "can't tell," which every caller here treats
// as "condition doesn't match" (eq) / "condition doesn't match" (ne) — i.e.
// hide the field, the same conservative-fail default as everywhere else in
// this evaluator.
func scalarEqual(a, b any) (equal bool, comparable bool) {
	switch a.(type) {
	case nil, bool, float64, string:
	default:
		return false, false
	}
	switch b.(type) {
	case nil, bool, float64, string:
	default:
		return false, false
	}
	return a == b, true
}

// FieldVisible reports whether f should be shown in the inspector for the
// given action config. nil ShowWhen -> always visible (every pre-existing
// field, unchanged).
func FieldVisible(f SchemaField, config map[string]any) bool {
	if f.ShowWhen == nil {
		return true
	}
	return FieldConditionMatches(*f.ShowWhen, config)
}

// targetQueryFieldsFull names every currently-ENFORCED TargetQueryDef
// (ability_program.go) sub-field, in display order. select_targets — the
// action whose entire job is resolving a free-form, possibly-many-unit scene
// query — declares this full shape. launch_projectile/channel_beam do NOT
// reuse it verbatim (see targetQueryFieldsSourceOnly's doc comment for why a
// bolt/beam only ever needs to say WHO, via "source," never narrow a pool):
// the two shapes are deliberately different, not a shared verbatim list.
//
// minCount/filters/requireLineOfSight are deliberately excluded from every
// action's declared shape (this one included): they decode and validate, but
// resolveTargetQueryLocked/applyTargetFiltersLocked do not enforce them yet
// (that file's own TODO(phase-3b) notes) — declaring them here would just
// re-create the exact "advertises a field the action can't use" bug this
// mechanism exists to fix. Add them back once they're wired.
//
// excludeRef IS enforced (applyTargetFiltersLocked drops candidates in the
// named ctxUnitSet — the chain "already hit" set), so it's declared here now
// that the editor can author a saved set to point it at (store_targets).
var targetQueryFieldsFull = []string{
	"source", "origin", "originRef", "relations", "radius",
	"ordering", "maxCount", "includeInitialTarget", "excludeSource", "excludeCurrentEvent", "excludeRef", "aliveState",
}

// targetQueryFieldsSourceOnly is launch_projectile's and channel_beam's
// targeting shape: a bolt/beam resolves to exactly ONE live unit and flies
// at (or channels at) it — it never needs to narrow a pool down from many
// candidates. Every field targetQueryFieldsFull carries beyond "source"
// (radius/ordering/maxCount/origin/originRef/includeInitialTarget/
// excludeSource) is a POOL-NARROWING concept: it only means something when
// the Source can yield more than one candidate. That narrowing is
// select_targets' job — the intended composition is select_targets (pick
// who, with the full narrowing toolkit) -> launch_projectile/channel_beam
// (deliver to them), chained via a preceding select_targets action's
// Outputs + this action's Input["targets"], exactly like deal_damage/
// restore_health already consume a preceding selection. Declaring the full
// set here would let an author configure e.g. Radius/MaxCount on the bolt
// itself and have it silently do nothing useful beyond "source" — the exact
// "advertises a field the action can't meaningfully use" bug this whole
// per-action-shape mechanism exists to fix (see SchemaField's doc comment).
//
// relations and aliveState are deliberately excluded too, not just the
// narrowing five:
//   - relations is redundant by construction. When Source is initial_target,
//     the cast's own canTarget*/Relations gating (AbilityEntryDef, evaluated
//     at cast-target-acquisition time — see compileEntryLegacy/the cast
//     handlers) already decided who's a legal target before InitialTarget was
//     ever set. When Source is prev_action_targets, the PRECEDING
//     select_targets action already applied its own Relations filter to
//     produce that selection. A second relations filter here can only ever
//     narrow a set that's already exactly right, or (if authored
//     inconsistently with the upstream gate) silently drop the one target
//     this action needs — never a genuine use.
//   - aliveState is excluded for the same "nothing upstream ever needs it"
//     reason PLUS a concrete harm: applyTargetFiltersLocked's default
//     AliveState ("") already excludes HP<=0 candidates, which is exactly
//     "don't fire a bolt at a corpse" — the correct default for every
//     shipped and planned projectile/beam ability. Exposing aliveState here
//     would let an author flip it to "dead"/"any" and bolt a corpse, a
//     capability nothing in the catalog wants and the mechanism (source
//     resolves ONE thing to hit) doesn't need to offer.
var targetQueryFieldsSourceOnly = []string{"source"}

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
	// Radius decodes for backward JSON-compat only: NO code path ever reads
	// it — Execute below applies Amount to every id in its resolved `targets`
	// unconditionally, and AoE/splash damage is composed a completely
	// different way (a preceding select_targets action with its OWN
	// TargetQueryDef.Radius, feeding this action via Input["targets"] — see
	// compileProjectileImpactTrigger/compileMeteorActions, ability_compile.go).
	// Not surfaced in Schema (below) for exactly that reason: an inspector
	// field for it would advertise a capability this action has never had.
	Radius float64 `json:"radius,omitempty"`
	// FlatOffset is a flat, UNFOLDED adjustment added to Amount's fold result
	// as the LAST step, after both the SpellModifier fold and the
	// ctx.effectiveDamageMultiplier scaling — i.e. it never itself scales
	// with the caster's modifiers. Built for chain_lightning's authored
	// bounce chain (compileChainLightningActions, ability_compile.go):
	// legacy folds a spell's modifiers exactly ONCE, at cast time, off the
	// RAW primary damage, then subtracts an UNFOLDED BounceDamageFalloff*hop
	// from that single folded value for every bounce
	// (fireProcBeamLocked, projectile.go — `dmg := p.Damage -
	// p.BounceDamageFalloff*hop`). A bounce hop's deal_damage therefore sets
	// Amount to the SAME value as the primary hit's (so it folds through the
	// identical modifiers to the identical base) and FlatOffset to
	// -(BounceDamageFalloff*hop) — reproducing "subtract a flat amount from
	// the already-scaled primary hit" instead of "scale a pre-reduced raw
	// amount" (which diverges from legacy the moment a multiplicative
	// modifier is active: fold(x-c) != fold(x)-c when mul != 1). 0/absent
	// (every deal_damage authored before this field existed, and the primary
	// hit's own dmg action) is a no-op.
	FlatOffset int `json:"flatOffset,omitempty"`
	// AmountRef, when non-empty, names a ctxScalar in ctx.Named whose value is the
	// damage amount (e.g. "trigger_damage", bound by the rider runner to the
	// triggering tick's damage). AmountMult scales it (default 1 when 0/unset).
	// Used for riders like shared_suffering that deal a fraction of the event that
	// fired them. When AmountRef is set the value is applied RAW — NOT re-folded
	// through effectiveAbilityDamageLocked / effectiveDamageMultiplier — because
	// the referenced scalar is already a final, folded number (the tick's actual
	// damage). FlatOffset still applies last, as usual. When AmountRef is empty,
	// Amount is used exactly as before (100% unchanged path).
	AmountRef  string  `json:"amountRef,omitempty"`
	AmountMult float64 `json:"amountMult,omitempty"`
}

func (dealDamageConfig) actionConfig() {}

// ── restore_health ──
type restoreHealthConfig struct {
	Amount int        `json:"amount"`
	School DamageType `json:"school,omitempty"`
	// AmountRef, when non-empty, names a ctxScalar in ctx.Named whose value is the
	// heal amount (e.g. "trigger_damage", bound by fireOnDamageDealtLocked to the
	// triggering hit's landed damage — see ability_damage_dealt.go). AmountMult
	// scales it (default 1 when 0/unset). Built for lifesteal-style passives that
	// heal a fraction of the damage their unit just dealt, which varies per hit and
	// so cannot be expressed as a static Amount. When AmountRef is set the value is
	// applied RAW — deliberately SKIPPING effectiveAbilityHealLocked's
	// divine_healer fold — because the referenced scalar is already a final,
	// folded number (the tick's actual damage/heal), mirroring deal_damage's
	// identical AmountRef contract exactly. A ref that isn't bound (missing, or
	// bound to a non-scalar) resolves to amount 0 (a no-op heal) rather than
	// falling back to Amount — an author who set AmountRef meant "the runtime
	// value," not "the static Amount as a fallback." When AmountRef is empty,
	// Amount is used exactly as before (100% unchanged path).
	AmountRef  string  `json:"amountRef,omitempty"`
	AmountMult float64 `json:"amountMult,omitempty"`
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
			// A non-empty AmountRef supplies the amount at runtime (from a bound
			// ctxScalar), so Amount == 0 is valid alongside it — only require
			// Amount > 0 when there is no ref to fall back on.
			if c.Amount <= 0 && c.AmountRef == "" {
				out = append(out, ValidationIssue{Code: "empty_required_property", Message: "deal_damage requires amount > 0 or amountRef", Severity: "error"})
			}
			if c.Type != "" && !IsValidDamageType(c.Type) {
				out = append(out, ValidationIssue{Code: "invalid_damage_type", Message: "unknown damage type " + string(c.Type), Severity: "error"})
			}
			return out
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "amount", Label: "Amount", Control: "number", Section: "Properties"},
			{Key: "type", Label: "Damage Type", Control: "enum", Section: "Properties"},
			{Key: "amountRef", Label: "Amount From (context)", Control: "text", Section: "Properties"},
			{Key: "amountMult", Label: "Amount ×", Control: "number", Section: "Properties"},
		}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(dealDamageConfig)
			dt := c.Type.OrPhysical()
			// Scale by the caster's spell-modifiers for this ability's school/
			// tags, at parity with the legacy effectiveSpellLocked path. Legacy
			// burn/DoT is applied raw; zone-tick ctx has abilityDef==nil so burn
			// stays raw — parity.
			caster := s.getUnitByIDLocked(ctx.CasterID)
			var amount int
			if c.AmountRef != "" {
				// AmountRef path (see the field's doc comment): the bound scalar
				// is already a final, folded damage number, so it is applied RAW —
				// deliberately SKIPPING both effectiveAbilityDamageLocked's
				// spell-modifier fold and effectiveDamageMultiplier below. A ref
				// that isn't bound (missing, or bound to a non-scalar) resolves to
				// amount 0 (a no-op hit) rather than falling back to Amount — an
				// author who set AmountRef meant "the runtime value," not "the
				// static Amount as a fallback."
				mult := c.AmountMult
				if mult == 0 {
					mult = 1
				}
				if v, ok := ctx.Named[c.AmountRef]; ok && v.Kind == ctxScalar {
					amount = int(math.Round(v.Scalar * mult))
				}
			} else {
				amount = c.Amount
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
			}
			// FlatOffset (see the field's doc comment) applies LAST, after
			// both scaling steps (or the AmountRef path above), and is itself
			// never scaled. Per-hop decay is now expressed with a loop variable
			// (deal_damage amount: "a") rather than a bespoke ref field — see
			// ability_exec_loop.go.
			amount += c.FlatOffset
			// lastAppliedDamage is reset here (start of this Execute) and
			// accumulated below so a 0-hit run reports 0 rather than leaking a
			// stale value from a previous action's run on the same ctx -- see
			// that field's doc comment (ability_exec.go).
			ctx.lastAppliedDamage = 0
			hit := make([]int, 0, len(targets))
			for _, id := range targets {
				u := s.getUnitByIDLocked(id)
				if u == nil || u.HP <= 0 {
					continue
				}
				s.applyUnitDamageWithSourceLocked(u, amount, DamageSource{AttackerUnitID: ctx.CasterID, Kind: "ability", Category: DamageCategoryAbility, DamageType: dt, SourceAbilityID: ctx.AbilityID})
				hit = append(hit, id)
				ctx.lastAppliedDamage += amount
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
			// A non-empty AmountRef supplies the amount at runtime (from a bound
			// ctxScalar), so Amount == 0 is valid alongside it — only require
			// Amount > 0 when there is no ref to fall back on.
			if c.Amount <= 0 && c.AmountRef == "" {
				return []ValidationIssue{{Code: "empty_required_property", Message: "restore_health requires amount > 0 or amountRef", Severity: "error"}}
			}
			if c.School != "" && !IsValidDamageType(c.School) {
				return []ValidationIssue{{Code: "invalid_damage_type", Message: "unknown school " + string(c.School), Severity: "error"}}
			}
			return nil
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "amount", Label: "Amount", Control: "number", Section: "Properties"},
			{Key: "school", Label: "School", Control: "enum", Section: "Properties"},
			{Key: "amountRef", Label: "Amount From (context)", Control: "text", Section: "Properties"},
			{Key: "amountMult", Label: "Amount ×", Control: "number", Section: "Properties"},
		}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(restoreHealthConfig)
			caster := s.getUnitByIDLocked(ctx.CasterID)
			if caster == nil {
				return nil
			}
			var amount int
			if c.AmountRef != "" {
				// AmountRef path (see the field's doc comment): the bound scalar
				// is already a final, folded number, so it is applied RAW —
				// deliberately SKIPPING effectiveAbilityHealLocked's divine_healer
				// fold. A ref that isn't bound (missing, or bound to a non-scalar)
				// resolves to amount 0 (a no-op heal) rather than falling back to
				// Amount — an author who set AmountRef meant "the runtime value,"
				// not "the static Amount as a fallback."
				mult := c.AmountMult
				if mult == 0 {
					mult = 1
				}
				if v, ok := ctx.Named[c.AmountRef]; ok && v.Kind == ctxScalar {
					amount = int(math.Round(v.Scalar * mult))
				}
			} else {
				// Divine Healer (silver cleric) scales every heal amount produced by
				// the caster, at parity with the legacy resolveAbilityCastOnTargetLocked
				// path (which scales def.HealAmount by the same multiplier). base is
				// THIS action's configured amount, not necessarily def.HealAmount — see
				// effectiveAbilityHealLocked's doc comment, mirroring
				// effectiveAbilityDamageLocked's "action's amount, not def's" contract.
				amount = c.Amount
				if ctx.abilityDef != nil {
					amount = s.effectiveAbilityHealLocked(caster, *ctx.abilityDef, c.Amount)
				}
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
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "target", Label: "Target Selection", Control: "target_query", Section: "Targeting", TargetQueryFields: targetQueryFieldsFull},
		}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, _ ActionConfig, targets []int) []int {
			ctx.trace("targets_selected", ctx.currentActionPath, map[string]any{"count": len(targets)})
			return targets
		},
	})
}
