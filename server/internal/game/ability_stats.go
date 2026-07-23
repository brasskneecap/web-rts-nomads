package game

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

// ═════════════════════════════════════════════════════════════════════════════
// ABILITY STATS — "+2s duration", "+15% radius", authored on a UNIT or an ITEM.
//
// This is the BROAD half of ability modification. A perk usually knows which
// ability it is buffing and addresses a field precisely; a unit type or an item
// cannot — an item does not know who equipped it or what they cast. So those
// sources target a KIND (ability_stat_kinds.go) and every schema field carrying
// that Kind scales.
//
// Two levels of precision, pooled into the SAME two accumulators:
//
//	broad   "duration"                       every kinded duration field
//	scoped  "create_zone.duration"           only that action's duration
//
//	value = (base + Σflat_broad + Σflat_scoped) × (1 + Σpct_broad + Σpct_scoped)
//
// Both pool ADDITIVELY. Two +15% sources make +30%, not +32.25% — that is how a
// player reads stacked percentages, and it matches how the existing fraction
// stats (lifesteal, critChance) already pool. Multiplicative composition remains
// available to the precise per-field modifier path, which folds through the
// staged statOpMultiply math instead.
//
// WHERE IT APPLIES: executeActionLocked's single decode seam (ability_exec.go),
// after loop-var/parameter substitution and before the typed decode. Doing it
// there means every action and every context kind — cast, zone tick, status
// tick, projectile impact, rider — is covered without per-action authoring, the
// same argument ensureAbilityParamsLocked makes for parameters.
// ═════════════════════════════════════════════════════════════════════════════

// AbilityStatMod is one source's contribution to one ability stat. Flat is in
// the field's own units (seconds for a duration, world units for a radius); Pct
// is a fraction, so 0.15 is +15%.
type AbilityStatMod struct {
	Flat float64 `json:"flat,omitempty"`
	Pct  float64 `json:"pct,omitempty"`
}

// IsZero reports whether this contributes nothing, so an authored-but-empty
// entry (a designer added the row and left it at 0) costs nothing at runtime.
func (m AbilityStatMod) IsZero() bool { return m.Flat == 0 && m.Pct == 0 }

// abilityStatSource pairs a source id with its contributions. As with
// abilityParamSource, the id exists to make the fold DETERMINISTIC: float
// addition is order-sensitive and the simulation must reproduce under a seed, so
// sources fold in sorted-id order rather than map-iteration order.
type abilityStatSource struct {
	id    string
	stats map[string]AbilityStatMod
}

// collectAbilityStatSourcesLocked gathers every source contributing ability
// stats for this caster.
//
// ── EXTENSION POINT ─────────────────────────────────────────────────────────
// A new source family (perk, advancement, status, zone aura, rank) appends its
// own abilityStatSource here and every ability picks it up for free. Keep ids
// prefixed by family so they can never collide and the sort stays stable.
//
// Caller holds s.mu.
func (s *GameState) collectAbilityStatSourcesLocked(caster *Unit, abilityID string) []abilityStatSource {
	// Preview runs execute under a scratch ability id; rows naming the authored
	// id must still match. No-op in a real match.
	abilityID = s.authoredAbilityIDLocked(abilityID)
	if caster == nil {
		return nil
	}
	var out []abilityStatSource

	// Unit type + advancements. As with AbilityParams, these arrive as ONE
	// source because by spawn time they are indistinguishable — the fold does
	// not care where a contribution came from.
	if len(caster.AbilityStats) > 0 {
		out = append(out, abilityStatSource{id: "unit:" + caster.UnitType, stats: caster.AbilityStats})
	}

	// Promotion path at the unit's CURRENT rank. Absolute per rank, so this is
	// the whole path contribution — gold's block already includes silver's (the
	// editor floors each rank at the previous one, and
	// validatePathAbilityStatsByRank enforces it for hand-edited files).
	// Resolved fresh here rather than baked at rank-up so a promotion takes
	// effect the same tick, like every other rank-driven value.
	if stats := pathAbilityStatsFor(caster.ProgressionPath, caster.Rank); len(stats) > 0 {
		out = append(out, abilityStatSource{id: "path:" + caster.ProgressionPath + ":" + caster.Rank, stats: stats})
	}

	for _, eq := range caster.Equipped {
		if eq == nil || eq.ItemID == "" {
			continue
		}
		itemDef, ok := getItemDef(eq.ItemID)
		if !ok || itemDef == nil || len(itemDef.AbilityStats) == 0 {
			continue
		}
		out = append(out, abilityStatSource{id: "item:" + eq.ItemID, stats: itemDef.AbilityStats})
	}

	// Perks. A perk row may NAME an ability, in which case it only contributes
	// while that ability is the one being cast; a row that names none applies to
	// every ability the unit has, exactly like the unit's own block. This is the
	// difference between "your FIRE PIT is 50% bigger" and "your abilities are
	// 50% bigger", and it is the only reason abilityID is threaded down here.
	for _, perkID := range caster.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil || len(def.AbilityStats) == 0 {
			continue
		}
		stats := make(map[string]AbilityStatMod, len(def.AbilityStats))
		for _, row := range def.AbilityStats {
			if row.Ability != "" && row.Ability != abilityID {
				continue
			}
			// Two rows of the same perk may address the same stat (one global,
			// one ability-specific); they ADD, matching how two separate perks
			// would combine.
			cur := stats[row.Stat]
			cur.Flat += row.Flat
			cur.Pct += row.Pct
			stats[row.Stat] = cur
		}
		if len(stats) == 0 {
			continue
		}
		out = append(out, abilityStatSource{id: "perk:" + perkID, stats: stats})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

// casterHasPerkAbilityStats is the cheap pre-check for the fold's early bail:
// does this caster carry ANY perk that contributes ability stats?
func casterHasPerkAbilityStats(caster *Unit) bool {
	for _, perkID := range caster.PerkIDs {
		if def := perkDefByID(perkID); def != nil && len(def.AbilityStats) > 0 {
			return true
		}
	}
	return false
}

// abilityStatFoldLocked sums the flat and percentage contributions that apply to
// a field of `kind` on an action of type `action`, across every source. Returns
// (0, 0) when nothing applies, which is the overwhelmingly common case.
//
// Caller holds s.mu.
func (s *GameState) abilityStatFoldLocked(caster *Unit, abilityID string, action ActionType, kind string) (flat, pct float64) {
	sources := s.collectAbilityStatSourcesLocked(caster, abilityID)
	if len(sources) == 0 {
		return 0, 0
	}
	scopedID := scopedAbilityStatID(action, kind)
	for _, src := range sources {
		if m, ok := src.stats[kind]; ok {
			flat += m.Flat
			pct += m.Pct
		}
		if m, ok := src.stats[scopedID]; ok {
			flat += m.Flat
			pct += m.Pct
		}
	}
	return flat, pct
}

// applyAbilityStatsToConfig rewrites the KINDED numeric fields of one action's
// raw config, folding the caster's ability stats in.
//
// Only TOP-LEVEL keys are walked, deliberately. A container action's config
// carries nested triggers whose actions have configs of their own (create_zone's
// on_tick, apply_status_duration's on_tick, launch_projectile's impact); those
// nested actions each run through executeActionLocked themselves and get their
// own fold there. Recursing here would apply the same stat twice to a nested
// field — e.g. a "+15% duration" would hit fire_pit's zone AND its burn status
// through the zone's own config, then hit the burn status again when the status
// action executes.
//
// Returns config unchanged when nothing applies, so an ability cast by a unit
// with no ability stats pays one map length check.
//
// Caller holds s.mu.
func (s *GameState) applyAbilityStatsToConfigLocked(caster *Unit, abilityID string, action ActionType, config json.RawMessage) json.RawMessage {
	if len(config) == 0 || caster == nil {
		return config
	}
	// Cheap bail before any JSON work: does this caster carry ability stats at
	// all? Every unit without an item or an authored block exits here.
	if len(caster.AbilityStats) == 0 &&
		len(pathAbilityStatsFor(caster.ProgressionPath, caster.Rank)) == 0 &&
		!casterHasItemAbilityStats(caster) &&
		!casterHasPerkAbilityStats(caster) {
		return config
	}
	// INFLICTED-STAT fold: a change_stat action carries the id of the unit stat
	// it inflicts in its own config, so a row addressed by that stat id folds
	// onto this action's `value` wherever the action sits in the program. Runs
	// before the kinded walk below and returns early — change_stat has no kinded
	// fields, so the two never both apply to one action.
	if action == ActionChangeStat {
		return s.applyInflictedStatToChangeStatLocked(caster, abilityID, config)
	}

	desc, ok := lookupActionDescriptor(action)
	if !ok {
		return config
	}
	// Which top-level keys of THIS action carry a Kind.
	kindByKey := make(map[string]string, len(desc.Schema.Fields))
	for _, f := range desc.Schema.Fields {
		if f.Kind != "" && isAbilityStatGridKind(f.Kind) {
			kindByKey[f.Key] = f.Kind
		}
	}
	if len(kindByKey) == 0 {
		return config
	}

	var decoded map[string]any
	if err := json.Unmarshal(config, &decoded); err != nil {
		return config
	}
	changed := false
	for key, kind := range kindByKey {
		raw, present := decoded[key]
		if !present {
			// An ABSENT field is left absent. Folding a stat onto a field the
			// author never set would materialise a value out of nothing — e.g. a
			// "+2s duration" would give a zone an authored-nowhere 2s lifetime
			// instead of leaving it to the action's own default.
			continue
		}
		base, isNum := raw.(float64)
		if !isNum {
			// Still a string here means an UNRESOLVED loop var or "$param"
			// reference (resolveConfigVars leaves those in place rather than
			// inventing a number). Skip rather than guess.
			continue
		}
		flat, pct := s.abilityStatFoldLocked(caster, abilityID, action, kind)
		if !abilityStatKindAllowsPct(kind) {
			// Belt and braces: validateAbilityStats rejects an authored pct on a
			// whole-quantity stat at load, so reaching here means a def built
			// outside the catalog (a test fixture). Drop it rather than let it
			// round to a surprise.
			pct = 0
		}
		if flat == 0 && pct == 0 {
			continue
		}
		folded := foldAbilityStat(base, flat, pct)
		if abilityStatKindIsIntegral(kind) {
			// A count field decodes into a Go int (loop.iterations, summon_unit's
			// count). Writing 3.45 back would make encoding/json REJECT the whole
			// config — "cannot unmarshal number into field of type int" — and the
			// action would be skipped entirely with only a validation_error trace.
			// So an integral kind rounds here, at the one place that knows a fold
			// happened.
			folded = math.Round(folded)
		}
		decoded[key] = folded
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

// foldAbilityStat is the arithmetic, in one place: (base + flat) × (1 + pct),
// clamped at 0 so a hostile/mistaken -200% can never produce a negative radius
// or a negative duration (both of which read as "infinite" or "absent" to the
// consumers downstream rather than as a small value).
func foldAbilityStat(base, flat, pct float64) float64 {
	v := (base + flat) * (1 + pct)
	if v < 0 || math.IsNaN(v) {
		return 0
	}
	if math.IsInf(v, 1) {
		return base
	}
	return v
}

// casterHasItemAbilityStats reports whether any equipped item contributes
// ability stats — the second half of applyAbilityStatsToConfigLocked's cheap
// bail, kept separate so the common "no items with stats" case does no
// allocation.
func casterHasItemAbilityStats(caster *Unit) bool {
	for _, eq := range caster.Equipped {
		if eq == nil || eq.ItemID == "" {
			continue
		}
		itemDef, ok := getItemDef(eq.ItemID)
		if ok && itemDef != nil && len(itemDef.AbilityStats) > 0 {
			return true
		}
	}
	return false
}

// applyInflictedStatToChangeStatLocked adds every matching source contribution
// to a change_stat action's value.
//
// "Matching" means the source authored a row whose stat id equals the stat this
// action inflicts — so a perk row `{stat: "damageTaken", flat: 0.15}` finds
// marker_trap's mark without knowing the action is called "vulnerable", and
// `{stat: "moveSpeed", flat: -0.15}` finds caltrops' slow the same way.
//
// ADD, never multiply. See the FLAT ONLY note on the inflicted-stat rows in
// AbilityStatDefs: these values are often inverse-sense, and a flat add is the
// only op that reads the same way at every site.
//
// Caller holds s.mu.
func (s *GameState) applyInflictedStatToChangeStatLocked(caster *Unit, abilityID string, config json.RawMessage) json.RawMessage {
	var decoded map[string]any
	if err := json.Unmarshal(config, &decoded); err != nil {
		return config
	}
	statID, _ := decoded["stat"].(string)
	if statID == "" {
		return config
	}
	base, isNum := decoded["value"].(float64)
	if !isNum {
		// An unresolved loop var / "$param" reference. Skip rather than guess.
		return config
	}
	var flat float64
	for _, src := range s.collectAbilityStatSourcesLocked(caster, abilityID) {
		if m, ok := src.stats[statID]; ok {
			flat += m.Flat
		}
	}
	if flat == 0 {
		return config
	}
	decoded["value"] = base + flat
	out, err := json.Marshal(decoded)
	if err != nil {
		return config
	}
	return out
}

// validateAbilityStats checks an authored ability-stat block: every id must be a
// stat the registry actually offers, and every value finite. An unknown id is a
// LOAD ERROR rather than a silent no-op — the whole point of deriving the ids
// from the action registry is that "+15% raduis" fails loudly at authoring time
// instead of doing nothing forever.
func validateAbilityStats(sourceLabel string, stats map[string]AbilityStatMod) error {
	if len(stats) == 0 {
		return nil
	}
	valid := make(map[string]bool)
	flatOnly := make(map[string]bool)
	for _, d := range AbilityStatDefs() {
		valid[d.ID] = true
		flatOnly[d.ID] = d.FlatOnly
	}
	ids := make([]string, 0, len(stats))
	for id := range stats {
		ids = append(ids, id)
	}
	sort.Strings(ids) // deterministic error messages
	for _, id := range ids {
		if !valid[id] {
			offered := make([]string, 0, len(valid))
			for v := range valid {
				offered = append(offered, v)
			}
			sort.Strings(offered)
			return fmt.Errorf("%s: abilityStats[%q] is not a known ability stat (offered: %v)", sourceLabel, id, offered)
		}
		m := stats[id]
		if math.IsNaN(m.Flat) || math.IsInf(m.Flat, 0) {
			return fmt.Errorf("%s: abilityStats[%q].flat must be finite, got %v", sourceLabel, id, m.Flat)
		}
		if math.IsNaN(m.Pct) || math.IsInf(m.Pct, 0) {
			return fmt.Errorf("%s: abilityStats[%q].pct must be finite, got %v", sourceLabel, id, m.Pct)
		}
		if m.Pct != 0 && isInflictedStatID(id) {
			return fmt.Errorf("%s: abilityStats[%q] addresses a stat an ability INFLICTS, which takes a flat amount only — these values are often inverse-sense (a moveSpeed multiplier of 0.35 is a stronger slow than 0.7), so a percentage has no single reading. Use \"flat\": negative strengthens a slow, positive strengthens a debuff", sourceLabel, id)
		}
		if m.Pct != 0 && flatOnly[id] {
			return fmt.Errorf("%s: abilityStats[%q] is a whole quantity and takes a flat bonus only — a percentage of a small count rounds to nothing (+15%% of 3 is 3). Use \"flat\" instead", sourceLabel, id)
		}
	}
	return nil
}

// validatePerkAbilityStats checks what can be checked AT LOAD: a stat id is
// present and the numbers are finite.
//
// It deliberately does NOT check that the stat id is real or that a named
// ability exists. Perk defs are built by a package-level VAR initializer, which
// Go runs before any init() — so the action registry AbilityStatDefs() derives
// from, and the ability catalog, are both still empty here. Those two checks
// live in TestCatalog_PerkAbilityStatsResolve instead, where everything is
// populated. A typo'd stat or ability id fails CI rather than at boot, which is
// the same trade every other cross-registry catalog rule in this package makes.
func validatePerkAbilityStats(sourceLabel string, rows []PerkAbilityStat) error {
	for i, row := range rows {
		if row.Stat == "" {
			return fmt.Errorf("%s: abilityStats[%d] names no stat", sourceLabel, i)
		}
		if math.IsNaN(row.Flat) || math.IsInf(row.Flat, 0) {
			return fmt.Errorf("%s: abilityStats[%d].flat must be finite, got %v", sourceLabel, i, row.Flat)
		}
		if math.IsNaN(row.Pct) || math.IsInf(row.Pct, 0) {
			return fmt.Errorf("%s: abilityStats[%d].pct must be finite, got %v", sourceLabel, i, row.Pct)
		}
	}
	return nil
}

// perkAbilityStatsResolve is validatePerkAbilityStats' other half, split out so
// it can run once the registries exist (see that function's doc comment for
// why it cannot run at load). Checks the stat id against the live grid and any
// named ability against the live catalog.
//
// The ability check earns its place: a row naming an ability id that does not
// exist contributes NOTHING and looks entirely correct in the editor.
func perkAbilityStatsResolve(sourceLabel string, rows []PerkAbilityStat) error {
	if len(rows) == 0 {
		return nil
	}
	byStat := make(map[string]AbilityStatMod, len(rows))
	for i, row := range rows {
		if row.Ability != "" {
			if _, ok := getAbilityDef(row.Ability); !ok {
				return fmt.Errorf("%s: abilityStats[%d] targets ability %q, which does not exist — the row would contribute nothing. Leave `ability` empty to affect every ability the unit has",
					sourceLabel, i, row.Ability)
			}
		}
		cur := byStat[row.Stat]
		cur.Flat += row.Flat
		cur.Pct += row.Pct
		byStat[row.Stat] = cur
	}
	return validateAbilityStats(sourceLabel, byStat)
}

func copyAbilityStats(src map[string]AbilityStatMod) map[string]AbilityStatMod {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]AbilityStatMod, len(src))
	for k, v := range src {
		if v.IsZero() {
			continue // an authored-but-untouched editor row carries no cost
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// abilityScalingTermsLocked returns the caster-derived additive contribution to
// an ability magnitude: attackDamage x adRatio + abilityPower x apRatio.
//
// Returns 0 for a nil caster or when both ratios are 0 — the latter being every
// action authored before ratios existed, which is why this is additive rather
// than a mode toggle: the identity case needs no migration.
//
// Attack damage is read through effectiveStatLocked so a buffed/debuffed
// attacker's abilities scale with the damage it actually has, matching the
// canonical combat read (state_combat.go).
//
// Caller holds s.mu.
func (s *GameState) abilityScalingTermsLocked(caster *Unit, adRatio, apRatio float64) float64 {
	if caster == nil || (adRatio == 0 && apRatio == 0) {
		return 0
	}
	var out float64
	if adRatio != 0 {
		out += s.effectiveStatLocked(caster, float64(caster.Damage), statDamage) * adRatio
	}
	if apRatio != 0 {
		out += s.effectiveStatLocked(caster, unitBaseStat(caster, statAbilityPower), statAbilityPower) * apRatio
	}
	return out
}
