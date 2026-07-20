package game

import "sort"

// ─────────────────────────────────────────────────────────────────────────────
// Perk stat modifiers — RUNTIME aggregation.
//
// PerkDef.StatModifiers (perk_defs.go) is the typed, validated,
// registry-backed stat-modifier vocabulary that replaces the old "designer
// writes a config key and hopes some Go switch arm reads that exact string"
// convention — a typo in Stat now fails catalog load (validatePerkDef)
// instead of silently doing nothing. This file composes a unit's OWNED
// perks' StatModifiers entries into the per-stage (add, mul) pools
// stat_modifiers.go's applyStatStages expects, mirroring
// abilityScalarModifiersForCasterLocked's "walk PerkIDs, compose over owned
// perks" idiom (ability_modifiers.go).
// ─────────────────────────────────────────────────────────────────────────────

// unitPerkStatModifiersLocked composes every owned perk's StatModifiers
// entry for `stat` into per-stage (add, mul) pools, keyed by
// PerkStatModifier.Stage (an empty authored Stage folds into
// statStageBase). DETERMINISTIC: perks are walked in perk-id-sorted order —
// NEVER unit.PerkIDs slice order or map iteration order — because float
// add/mul is order-sensitive (AI_RULES.md sim determinism rule).
//
// Returns nil (identity — see applyStatStages, which treats a nil/absent
// stage as a no-op) when unit is nil, stat is not a registered stat, the
// unit owns no perks, or no owned perk modifies stat. Safe to call on any
// unit for any stat; callers do not need to special-case "no modifiers."
//
// Caller holds s.mu (read or write).
func (s *GameState) unitPerkStatModifiersLocked(unit *Unit, stat string) map[string]statStageAccum {
	if unit == nil || !isKnownStat(stat) || len(unit.PerkIDs) == 0 {
		return nil
	}

	ids := append([]string(nil), unit.PerkIDs...)
	sort.Strings(ids)

	var stages map[string]statStageAccum
	for _, perkID := range ids {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		for _, m := range def.StatModifiers {
			if m.Stat != stat {
				continue
			}
			stage := m.Stage
			if stage == "" {
				stage = statStageBase
			}
			if stages == nil {
				stages = make(map[string]statStageAccum, 1)
			}
			acc, ok := stages[stage]
			if !ok {
				acc = statStageAccum{Add: 0, Mul: 1}
			}
			switch m.Op {
			case statOpAdd:
				acc.Add += m.Value
			case statOpMultiply:
				acc.Mul *= m.Value
			}
			stages[stage] = acc
		}
	}
	return stages
}
