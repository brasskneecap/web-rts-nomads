package game

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

// ═════════════════════════════════════════════════════════════════════════════
// ABILITY FIELD MODIFIERS — the PRECISE half of ability modification.
//
// A source that KNOWS the ability it is buffing addresses one field on one
// action by name:
//
//	{ target: "fire_pit", action: "pit", field: "radius", op: "multiply", value: 1.5 }
//
// This replaces the ability-PARAMETER indirection ($radius / params / paramsByRank).
// The parameter layer existed only to give a perk a name to point at, and it cost
// more than it earned: the ability's config held the STRING "$radius" instead of a
// number, so the editor rendered a blank control, and every new ability had to
// remember to declare a parameter spelled exactly right before any existing perk
// could reach it. Addressing the schema field directly means the config holds a
// real number that renders, and nothing has to be declared twice.
//
// PRECISE vs BROAD (ability_stats.go):
//
//	precise  {ability, action, field}   perks — surgical, knows the program
//	broad    {kind} or {action.kind}    units/items — cannot name an ability
//
// Precision is the whole point of this half. extended_setup extends a trap's ZONE
// duration but NOT the burn status inside it; fire_pit owns both, at different
// depths, and only an action-level address can tell them apart.
//
// FOLD ORDER. Precise modifiers fold first, through the same staged
// (base + Σadd) × Πmul math the parameter system used — so a migrated perk is
// arithmetically identical to what it was. The broad ability-stat fold then
// applies to that result (ability_stats.go), which means a unit's "+15% radius"
// amplifies a perk's contribution rather than being amplified by it. Stated
// explicitly because the other order is equally plausible and gives different
// numbers.
// ═════════════════════════════════════════════════════════════════════════════

// abilityFieldTagPrefix marks a Target as a tag match rather than an ability-id
// match ("tag:trap" — every ability carrying that tag).
const abilityFieldTagPrefix = "tag:"

// strippedTagTarget returns the tag name when target uses the "tag:" form.
func strippedTagTarget(target string) (string, bool) {
	if len(target) > len(abilityFieldTagPrefix) && target[:len(abilityFieldTagPrefix)] == abilityFieldTagPrefix {
		return target[len(abilityFieldTagPrefix):], true
	}
	return "", false
}

// amplifyTowardZero scales a value's DISTANCE FROM 1.0 by factor:
// result = 1 - (1 - value) x factor, clamped to [0, 1].
//
// This is the statOpAmplify implementation, and it is deliberately the same math
// amplifySlow (perks_trapper.go) has always applied to trap slows — a 0.35 slow
// amplified by 1.35 becomes 0.1225, not 0.4725. It exists because some fields
// are INVERSE-SENSE multipliers where "stronger" means "closer to 0", so a plain
// multiply would weaken them. Values at or above 1.0 are returned unchanged
// (there is no reduction to amplify), matching amplifySlow's own early-out.
func amplifyTowardZero(value, factor float64) float64 {
	if value >= 1 {
		return value
	}
	reduction := (1 - value) * factor
	if reduction < 0 {
		reduction = 0
	}
	if reduction > 1 {
		reduction = 1
	}
	return 1 - reduction
}

// AbilityFieldModifier is one source's contribution to one action's field.
//
//   - Target — the ability id, or "tag:<name>" for every ability carrying that
//     tag (AbilityDef.Tags).
//   - Action — the AUTHORED action id within that ability's program ("pit").
//     Action ids are stable across edits in a way flow paths are not, which is
//     why the preview's force-branch overrides key on them too.
//   - Field  — the action's schema field key ("radius"), validated at load
//     against the action type's registered SchemaField set.
//   - Op     — statOpAdd / statOpMultiply / statOpAmplify. Amplify exists for
//     INVERSE-SENSE fields where "stronger" means "closer to 0" (a slow
//     multiplier), which a plain multiply would weaken — see amplifyTowardZero.
//   - Stage  — statStageIntrinsic / statStageBase (default) / statStageFinal.
type AbilityFieldModifier struct {
	Target string  `json:"target"`
	Action string  `json:"action"`
	Field  string  `json:"field"`
	Op     string  `json:"op"`
	Value  float64 `json:"value"`
	Stage  string  `json:"stage,omitempty"`
}

// matchesAbilityID reports whether m applies to def, by id or by "tag:".
func (m AbilityFieldModifier) matchesAbilityID(def AbilityDef) bool {
	if m.Target == "" {
		return false
	}
	if tag, ok := strippedTagTarget(m.Target); ok {
		for _, t := range def.Tags {
			if t == tag {
				return true
			}
		}
		return false
	}
	return m.Target == def.ID
}

// abilityFieldSource pairs a source id with its contributions, so the fold is
// DETERMINISTIC — float add/multiply is order-sensitive and the simulation must
// reproduce under a seed, so sources fold in sorted-id order.
type abilityFieldSource struct {
	id   string
	mods []AbilityFieldModifier
}

// collectAbilityFieldModsLocked gathers every source contributing field
// modifiers to this ability for this caster.
//
// ── EXTENSION POINT ─────────────────────────────────────────────────────────
// A new source family (advancement, status, zone aura, RANK) appends its own
// abilityFieldSource here and every ability picks it up for free. Keep ids
// prefixed by family so they never collide and the sort stays stable.
//
// Caller holds s.mu.
func (s *GameState) collectAbilityFieldModsLocked(caster *Unit, def AbilityDef) []abilityFieldSource {
	if caster == nil {
		return nil
	}
	var out []abilityFieldSource

	if len(caster.AbilityFields) > 0 {
		if mods := matchingFieldMods(caster.AbilityFields, def); len(mods) > 0 {
			out = append(out, abilityFieldSource{id: "unit:" + caster.UnitType, mods: mods})
		}
	}
	for _, perkID := range caster.PerkIDs {
		pd := perkDefByID(perkID)
		if pd == nil || len(pd.AbilityFields) == 0 {
			continue
		}
		if mods := matchingFieldMods(pd.AbilityFields, def); len(mods) > 0 {
			out = append(out, abilityFieldSource{id: "perk:" + perkID, mods: mods})
		}
	}
	for _, eq := range caster.Equipped {
		if eq == nil || eq.ItemID == "" {
			continue
		}
		itemDef, ok := getItemDef(eq.ItemID)
		if !ok || itemDef == nil || len(itemDef.AbilityFields) == 0 {
			continue
		}
		if mods := matchingFieldMods(itemDef.AbilityFields, def); len(mods) > 0 {
			out = append(out, abilityFieldSource{id: "item:" + eq.ItemID, mods: mods})
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

func matchingFieldMods(mods []AbilityFieldModifier, def AbilityDef) []AbilityFieldModifier {
	var out []AbilityFieldModifier
	for _, m := range mods {
		if m.matchesAbilityID(def) {
			out = append(out, m)
		}
	}
	return out
}

// applyAbilityFieldModsToConfigLocked rewrites the fields of ONE action's raw
// config that sources have targeted by {ability, action, field}.
//
// Returns config unchanged when nothing targets this action, which is the
// overwhelmingly common case — the cost for an unmodified action is one slice
// length check after the caster's source scan.
//
// Caller holds s.mu.
func (s *GameState) applyAbilityFieldModsToConfigLocked(caster *Unit, abilityID, actionID string, config json.RawMessage) json.RawMessage {
	if len(config) == 0 || caster == nil || abilityID == "" || actionID == "" {
		return config
	}
	def, ok := getAbilityDef(abilityID)
	if !ok {
		return config
	}
	sources := s.collectAbilityFieldModsLocked(caster, def)
	if len(sources) == 0 {
		return config
	}

	// Accumulate per-field, per-stage across sources in deterministic order.
	type fieldAccum struct {
		stages  map[string]statStageAccum
		amplify float64
	}
	byField := map[string]*fieldAccum{}
	for _, src := range sources {
		for _, m := range src.mods {
			if m.Action != actionID {
				continue
			}
			acc := byField[m.Field]
			if acc == nil {
				acc = &fieldAccum{amplify: 1}
				byField[m.Field] = acc
			}
			if m.Op == statOpAmplify {
				acc.amplify *= m.Value
				continue
			}
			stage := m.Stage
			if stage == "" {
				stage = statStageBase
			}
			if acc.stages == nil {
				acc.stages = make(map[string]statStageAccum, len(statStages))
			}
			st := acc.stages[stage]
			if st.Mul == 0 {
				st.Mul = 1
			}
			switch m.Op {
			case statOpMultiply:
				st.Mul *= m.Value
			default: // statOpAdd
				st.Add += m.Value
			}
			acc.stages[stage] = st
		}
	}
	if len(byField) == 0 {
		return config
	}

	var decoded map[string]any
	if err := json.Unmarshal(config, &decoded); err != nil {
		return config
	}
	changed := false
	for field, acc := range byField {
		raw, present := decoded[field]
		if !present {
			// Same rule as the broad fold: a modifier never MATERIALISES a value
			// the author never set. Load-time validation already guarantees the
			// field is a real schema field of this action, so absence here means
			// "author left it at the action's own default" — which the action
			// must keep owning.
			continue
		}
		base, isNum := raw.(float64)
		if !isNum {
			continue // an unresolved loop var / "$param" string; skip, don't guess
		}
		v := applyStatStages(base, acc.stages)
		if acc.amplify != 1 {
			v = amplifyTowardZero(v, acc.amplify)
		}
		if v == base {
			continue
		}
		decoded[field] = v
		changed = true
	}
	if !changed {
		return config
	}
	out, err := json.Marshal(decoded)
	if err != nil {
		return config
	}
	return out
}

// AbilityRankOverride sets one action field to a specific value at a given rank.
//
// Rank SELECTS THE BASE; it is not a modifier source. That is exactly the
// contract paramsByRank had ("rank selects the base, it is not itself a
// modifier") and keeping it means a perk's ×1.5 composes with a rank the same
// way at every rank, instead of a gold unit's perk multiplying a different
// number than a bronze one's by accident.
//
// A `set` shape (rather than add/multiply) is deliberate: a designer authoring
// "fire pit does 45 damage at gold" should type 45, not the 2.8125 multiplier
// that happens to turn 16 into 45.
type AbilityRankOverride struct {
	Action string  `json:"action"`
	Field  string  `json:"field"`
	Value  float64 `json:"value"`
}

// applyAbilityRankOverridesToConfig rewrites an action's config with the
// rank-specific base values before any modifier folds. Returns config unchanged
// for an unranked caster or an ability with no overrides — the common case.
func applyAbilityRankOverridesToConfig(def AbilityDef, rank, actionID string, config json.RawMessage) json.RawMessage {
	if rank == "" || len(def.ByRank) == 0 || len(config) == 0 {
		return config
	}
	overrides := def.ByRank[rank]
	if len(overrides) == 0 {
		return config
	}
	var decoded map[string]any
	if err := json.Unmarshal(config, &decoded); err != nil {
		return config
	}
	changed := false
	for _, o := range overrides {
		if o.Action != actionID {
			continue
		}
		if _, present := decoded[o.Field]; !present {
			continue
		}
		decoded[o.Field] = o.Value
		changed = true
	}
	if !changed {
		return config
	}
	out, err := json.Marshal(decoded)
	if err != nil {
		return config
	}
	return out
}

// abilityRankBaseValue returns the rank-effective authored value of one action
// field: the ByRank override when one exists, else the program's own literal.
func abilityRankBaseValue(def AbilityDef, rank, actionID, field string, authored float64) float64 {
	if rank == "" {
		return authored
	}
	for _, o := range def.ByRank[rank] {
		if o.Action == actionID && o.Field == field {
			return o.Value
		}
	}
	return authored
}

// validateAbilityRankOverrides checks an ability's byRank block: ranks must be
// real, and every {action, field} must exist in this ability's own program, so a
// rename can never leave a rank silently un-scaled.
func validateAbilityRankOverrides(def AbilityDef) error {
	if len(def.ByRank) == 0 {
		return nil
	}
	actions := programActionTypes(def)
	ranks := make([]string, 0, len(def.ByRank))
	for r := range def.ByRank {
		ranks = append(ranks, r)
	}
	sort.Strings(ranks)
	for _, rank := range ranks {
		switch rank {
		case unitRankBronze, unitRankSilver, unitRankGold:
		default:
			return fmt.Errorf("ability %q: byRank has unknown rank %q (want %q, %q or %q)", def.ID, rank, unitRankBronze, unitRankSilver, unitRankGold)
		}
		for _, o := range def.ByRank[rank] {
			actionType, exists := actions[o.Action]
			if !exists {
				return fmt.Errorf("ability %q: byRank[%q] targets action %q, which its program does not contain (present: %v)",
					def.ID, rank, o.Action, sortedActionIDs(actions))
			}
			desc, ok := lookupActionDescriptor(actionType)
			if !ok {
				continue
			}
			f, found := schemaFieldByKey(desc, o.Field)
			if !found || !isNumericControl(f.Control) {
				return fmt.Errorf("ability %q: byRank[%q] targets action %q field %q, which is not a numeric field of %s (available: %v)",
					def.ID, rank, o.Action, o.Field, actionType, sortedFieldKeys(desc))
			}
			if math.IsNaN(o.Value) || math.IsInf(o.Value, 0) {
				return fmt.Errorf("ability %q: byRank[%q][%s.%s].value must be finite, got %v", def.ID, rank, o.Action, o.Field, o.Value)
			}
		}
	}
	return nil
}

// targetQueryRadiusField is the pseudo-field key a modifier uses to address a
// target query's radius: "target.radius". The prefix distinguishes it from a
// config key, because TargetQueryDef lives on the action as a SIBLING of Config,
// not inside it.
const targetQueryRadiusField = "target.radius"

// foldTargetQueryRadiusLocked applies both modifier paths to an action's target
// query radius, returning the value to use for this resolution.
//
// TWO GUARDS, both load-bearing:
//
//  1. A RadiusRef is left ALONE. The only refs in use name the enclosing zone's
//     live "zone_radius" (fire_pit, caltrops), and that number was ALREADY folded
//     when the zone's own create_zone radius was modified. Folding again here
//     would apply the same "+15% radius" twice to one number.
//
//  2. A radius <= 0 is left alone. Negative values are SENTINELS, not
//     quantities — greater_heal authors -1 for "match the caster's cast range"
//     (resolved by CastRange(radius).Resolve). Scaling a sentinel produces a
//     meaningless number, and 0 means "no radius query" at all.
//
// Caller holds s.mu.
func (s *GameState) foldTargetQueryRadiusLocked(ctx *RuntimeAbilityContext, a *AbilityActionDef, q TargetQueryDef) float64 {
	if q.RadiusRef != "" || q.Radius <= 0 {
		return q.Radius
	}
	caster := s.getUnitByIDLocked(ctx.CasterID)
	if caster == nil {
		return q.Radius
	}
	radius := q.Radius

	// Precise ({ability, action, "target.radius"}) folds first, then broad, to
	// match the ordering executeActionLocked uses for config fields.
	if def, ok := getAbilityDef(ctx.AbilityID); ok {
		radius = s.foldOneFieldLocked(caster, def, a.ID, targetQueryRadiusField, radius)
	}
	flat, pct := s.abilityStatFoldLocked(caster, a.Type, abilityStatKindRadius)
	if flat != 0 || pct != 0 {
		radius = foldAbilityStat(radius, flat, pct)
	}
	return radius
}

// foldOneFieldLocked applies every precise modifier targeting one named field of
// one action to a single base value. Shared by the target-query path with
// applyAbilityFieldModsToConfigLocked's per-config walk.
//
// Caller holds s.mu.
func (s *GameState) foldOneFieldLocked(caster *Unit, def AbilityDef, actionID, field string, base float64) float64 {
	sources := s.collectAbilityFieldModsLocked(caster, def)
	if len(sources) == 0 {
		return base
	}
	var stages map[string]statStageAccum
	amplify := 1.0
	for _, src := range sources {
		for _, m := range src.mods {
			if m.Action != actionID || m.Field != field {
				continue
			}
			if m.Op == statOpAmplify {
				amplify *= m.Value
				continue
			}
			stage := m.Stage
			if stage == "" {
				stage = statStageBase
			}
			if stages == nil {
				stages = make(map[string]statStageAccum, len(statStages))
			}
			st := stages[stage]
			if st.Mul == 0 {
				st.Mul = 1
			}
			switch m.Op {
			case statOpMultiply:
				st.Mul *= m.Value
			default:
				st.Add += m.Value
			}
			stages[stage] = st
		}
	}
	v := applyStatStages(base, stages)
	if amplify != 1 {
		v = amplifyTowardZero(v, amplify)
	}
	return v
}

// ── LOAD-TIME VALIDATION ────────────────────────────────────────────────────

// programActionTypes enumerates every action in def's program as
// actionID -> ActionType, including actions nested inside zone/status/projectile/
// beam triggers, conditional branches, and loop bodies.
//
// Implemented as a generic walk of the program's raw JSON rather than a typed
// recursion, deliberately: the typed walker (validationWalker.walkAction) has to
// decode each container to reach its children and carries a lot of
// placement-rule state, and duplicating that here would mean this enumerator
// silently missed any container shape added later. Every action serializes with
// an "id" and a "type", so matching on those — filtered through isKnownActionType
// so trigger objects (which also carry id/type) are excluded — finds all of them
// at any depth, for free, forever. Load-time only, so the reflection cost is
// irrelevant.
func programActionTypes(def AbilityDef) map[string]ActionType {
	if def.Program == nil {
		return nil
	}
	raw, err := json.Marshal(def.Program)
	if err != nil {
		return nil
	}
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil
	}
	out := map[string]ActionType{}
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			id, hasID := t["id"].(string)
			typ, hasType := t["type"].(string)
			if hasID && hasType && isKnownActionType(ActionType(typ)) {
				out[id] = ActionType(typ)
			}
			for _, child := range t {
				walk(child)
			}
		case []any:
			for _, child := range t {
				walk(child)
			}
		}
	}
	walk(tree)
	return out
}

// programActionConfigValue reads one numeric config field of one authored action
// out of an ability's program, at any nesting depth. Returns the authored value
// and the owning action's type.
//
// This is the "what does the ability actually say" read that ability PARAMETERS
// used to provide: before, a tooltip asked the params block; now it asks the
// program directly, which is strictly better because the program is the thing
// that runs. Load/debug-path only (a raw-JSON walk), never on the tick path.
func programActionConfigValue(def AbilityDef, actionID, field string) (float64, ActionType, bool) {
	if def.Program == nil {
		return 0, "", false
	}
	raw, err := json.Marshal(def.Program)
	if err != nil {
		return 0, "", false
	}
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return 0, "", false
	}
	var found bool
	var value float64
	var actionType ActionType
	var walk func(any)
	walk = func(v any) {
		if found {
			return
		}
		switch t := v.(type) {
		case map[string]any:
			id, hasID := t["id"].(string)
			typ, hasType := t["type"].(string)
			if hasID && hasType && id == actionID && isKnownActionType(ActionType(typ)) {
				if cfg, ok := t["config"].(map[string]any); ok {
					if n, ok := cfg[field].(float64); ok {
						value, actionType, found = n, ActionType(typ), true
						return
					}
				}
				// "target.radius" lives on the action's target query, a SIBLING
				// of config — see targetQueryRadiusField.
				if field == targetQueryRadiusField {
					if tq, ok := t["target"].(map[string]any); ok {
						if n, ok := tq["radius"].(float64); ok {
							value, actionType, found = n, ActionType(typ), true
							return
						}
					}
				}
			}
			for _, child := range t {
				walk(child)
			}
		case []any:
			for _, child := range t {
				walk(child)
			}
		}
	}
	walk(tree)
	return value, actionType, found
}

// EffectiveAbilityFieldLocked returns what one authored action field will
// ACTUALLY be worth when this caster runs it — the authored value with both
// modifier paths folded in, in the same order executeActionLocked applies them.
//
// This is the read a tooltip or an editor preview wants: it cannot drift from
// what the ability does, because it folds through the same helpers the executor
// does rather than re-deriving the arithmetic.
//
// Caller holds s.mu.
func (s *GameState) EffectiveAbilityFieldLocked(caster *Unit, abilityID, actionID, field string) (float64, bool) {
	def, ok := getAbilityDef(abilityID)
	if !ok {
		return 0, false
	}
	base, actionType, found := programActionConfigValue(def, actionID, field)
	if !found {
		return 0, false
	}
	if caster != nil {
		base = abilityRankBaseValue(def, caster.Rank, actionID, field, base)
	}
	v := s.foldOneFieldLocked(caster, def, actionID, field, base)
	kind := ""
	if field == targetQueryRadiusField {
		kind = abilityStatKindRadius
	} else if desc, ok := lookupActionDescriptor(actionType); ok {
		if f, ok := schemaFieldByKey(desc, field); ok && isAbilityStatGridKind(f.Kind) {
			kind = f.Kind
		}
	}
	if kind != "" {
		flat, pct := s.abilityStatFoldLocked(caster, actionType, kind)
		if !abilityStatKindAllowsPct(kind) {
			pct = 0
		}
		if flat != 0 || pct != 0 {
			v = foldAbilityStat(v, flat, pct)
			if abilityStatKindIsIntegral(kind) {
				v = math.Round(v)
			}
		}
	}
	return v, true
}

// abilitiesForFieldTarget resolves a modifier target to the ability defs it
// applies to. Like abilitiesForParamTarget, an unresolvable target is NOT an
// error here (catalog load order); the catalog-integrity test covers it.
func abilitiesForFieldTarget(target string) []AbilityDef {
	if tag, isTag := strippedTagTarget(target); isTag {
		if tag == "" {
			return nil
		}
		var out []AbilityDef
		for _, def := range ListAbilityDefs() {
			for _, t := range def.Tags {
				if t == tag {
					out = append(out, def)
					break
				}
			}
		}
		return out
	}
	if def, ok := getAbilityDef(target); ok {
		return []AbilityDef{def}
	}
	return nil
}

// validateAbilityFieldModifiers checks a SOURCE's field contributions against the
// abilities they target. Three things must hold, and each replaces a guarantee
// the parameter system used to give:
//
//   - the named ACTION must exist in the target ability's program (a rename or a
//     deleted node must not leave a perk silently doing nothing);
//   - the named FIELD must be a registered schema field of that action's TYPE
//     (so "+30% raduis" fails at authoring time);
//   - the field must be numeric-ish, i.e. not a text/enum/boolean control, since
//     the fold multiplies it.
func validateAbilityFieldModifiers(sourceLabel string, mods []AbilityFieldModifier) error {
	for _, m := range mods {
		if m.Target == "" {
			return fmt.Errorf("%s: abilityFields entry has an empty target", sourceLabel)
		}
		if m.Action == "" {
			return fmt.Errorf("%s: abilityFields entry targeting %q has an empty action id", sourceLabel, m.Target)
		}
		if m.Field == "" {
			return fmt.Errorf("%s: abilityFields entry targeting %q action %q has an empty field", sourceLabel, m.Target, m.Action)
		}
		switch m.Op {
		case "", statOpAdd, statOpMultiply, statOpAmplify:
		default:
			return fmt.Errorf("%s: abilityFields[%s.%s].op %q must be %q, %q or %q",
				sourceLabel, m.Action, m.Field, m.Op, statOpAdd, statOpMultiply, statOpAmplify)
		}
		switch m.Stage {
		case "", statStageIntrinsic, statStageBase, statStageFinal:
		default:
			return fmt.Errorf("%s: abilityFields[%s.%s].stage %q is unknown (want %q, %q, %q, or omit for base)",
				sourceLabel, m.Action, m.Field, m.Stage, statStageIntrinsic, statStageBase, statStageFinal)
		}
		if math.IsNaN(m.Value) || math.IsInf(m.Value, 0) {
			return fmt.Errorf("%s: abilityFields[%s.%s].value must be finite, got %v", sourceLabel, m.Action, m.Field, m.Value)
		}

		for _, def := range abilitiesForFieldTarget(m.Target) {
			actions := programActionTypes(def)
			actionType, exists := actions[m.Action]
			if !exists {
				return fmt.Errorf("%s: ability %q has no action %q — an action id must exist before a source can modify its fields (present: %v)",
					sourceLabel, def.ID, m.Action, sortedActionIDs(actions))
			}
			desc, ok := lookupActionDescriptor(actionType)
			if !ok {
				continue // deferred action type with no descriptor yet
			}
			if m.Field == targetQueryRadiusField {
				// The target-query pseudo-field. Valid only on an action that
				// actually declares a target_query control — otherwise the
				// modifier would address a query the action never resolves.
				if !hasTargetQueryField(desc) {
					return fmt.Errorf("%s: ability %q action %q (%s) has no target query, so %q cannot apply",
						sourceLabel, def.ID, m.Action, actionType, targetQueryRadiusField)
				}
				continue
			}
			field, found := schemaFieldByKey(desc, m.Field)
			if !found {
				return fmt.Errorf("%s: ability %q action %q (%s) has no field %q (available: %v)",
					sourceLabel, def.ID, m.Action, actionType, m.Field, sortedFieldKeys(desc))
			}
			if !isNumericControl(field.Control) {
				return fmt.Errorf("%s: ability %q action %q field %q is a %q control, which is not a number — only numeric fields can be modified",
					sourceLabel, def.ID, m.Action, m.Field, field.Control)
			}
		}
	}
	return nil
}

// isNumericControl reports whether a SchemaField's control holds a number the
// fold can operate on. Guards against a modifier targeting a text/enum/boolean
// field, where a multiply is meaningless.
func isNumericControl(control string) bool {
	switch control {
	case "number", "duration", "percentage", "sentinel_number":
		return true
	default:
		return false
	}
}

// hasTargetQueryField reports whether an action declares a target_query control,
// i.e. whether it resolves a TargetQueryDef of its own that a "target.radius"
// modifier could reach.
func hasTargetQueryField(desc ActionDescriptor) bool {
	for _, f := range desc.Schema.Fields {
		if f.Control == "target_query" {
			return true
		}
	}
	return false
}

func schemaFieldByKey(desc ActionDescriptor, key string) (SchemaField, bool) {
	for _, f := range desc.Schema.Fields {
		if f.Key == key {
			return f, true
		}
	}
	return SchemaField{}, false
}

func sortedActionIDs(actions map[string]ActionType) []string {
	out := make([]string, 0, len(actions))
	for id := range actions {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func sortedFieldKeys(desc ActionDescriptor) []string {
	out := make([]string, 0, len(desc.Schema.Fields))
	for _, f := range desc.Schema.Fields {
		if isNumericControl(f.Control) {
			out = append(out, f.Key)
		}
	}
	sort.Strings(out)
	return out
}
