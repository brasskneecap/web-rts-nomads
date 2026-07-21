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
//
// unitStatusStatModifiersLocked below is the THIRD emitter sharing this same
// PerkStatModifier vocabulary (perks apply to the OWNER; auras apply in a
// radius via perk_aura_stat_cache.go; a status applies to whichever unit it
// is currently afflicting) — it mirrors unitPerkStatModifiersLocked's shape
// field-for-field, just sourced from active AbilityStatus objects
// (ability_status.go) instead of owned PerkDefs.
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

// unitStatusStatModifiersLocked composes every ACTIVE AbilityStatus on
// `unit` whose StatModifiers carries an entry for `stat` into per-stage
// (add, mul) pools — the status-sourced sibling of
// unitPerkStatModifiersLocked above. Same fold shape (per-stage add-sums,
// mul-products), just walking s.AbilityStatuses filtered to this unit's
// TargetUnitID instead of unit.PerkIDs.
//
// DETERMINISTIC: walks s.AbilityStatuses in its existing slice (append)
// order — the SAME stable order tickAbilityStatusesLocked's own doc comment
// relies on (ability_status.go: "program-execution order — never
// map-iteration order") — never a map, so composing float add/mul across
// multiple simultaneously-active statuses never depends on iteration order
// (AI_RULES sim determinism rule). No sort-by-id step is needed (unlike
// unitPerkStatModifiersLocked, which sorts unit.PerkIDs because THAT slice's
// order is authoring/save order, not append order) — s.AbilityStatuses'
// append order already is the stable order.
//
// STACKING: an authored status refreshed in place (the default "refresh"
// stacking — see spawnAbilityStatusLocked) is always exactly ONE entry in
// s.AbilityStatuses for its (AbilityID,Name,target) key, so it contributes
// its StatModifiers exactly once no matter how many times it was
// reapplied/refreshed. An explicitly "stack"-configured status becomes N
// independent AbilityStatus entries (up to MaxStacks) sharing that key, so
// this function naturally sums/multiplies N contributions — one term per
// live instance — with no special-case stacking logic needed here: N
// instances in the slice already means N loop iterations.
//
// Returns nil (identity — see applyStatStages) when unit is nil, stat is
// not a registered stat, no statuses are active at all, or no active status
// targeting unit carries a StatModifiers entry for stat. Safe to call on any
// unit for any stat.
//
// Caller holds s.mu (read or write).
func (s *GameState) unitStatusStatModifiersLocked(unit *Unit, stat string) map[string]statStageAccum {
	if unit == nil || !isKnownStat(stat) || len(s.AbilityStatuses) == 0 {
		return nil
	}

	var stages map[string]statStageAccum
	for _, st := range s.AbilityStatuses {
		if st == nil || st.TargetUnitID != unit.ID || len(st.StatModifiers) == 0 {
			continue
		}
		for _, m := range st.StatModifiers {
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

// ─────────────────────────────────────────────────────────────────────────────
// The stat chokepoint.
//
// unitStatStagesLocked and effectiveStatLocked are the two shared entry points
// the per-stat read sites call instead of re-deriving the
// perk→status→(zone) merge inline. Every read site used to hand-roll the same
// three lines (unitPerkStatModifiersLocked + playerStatModifierLocked +
// applyStatStages/mergeZoneIntoBaseStage) and — crucially — most of them
// FORGOT the status emitter, so a status-authored change_stat only reached
// armor and healingReceived. These two helpers make the perk+status+zone fold
// uniform across every stat, which is what lets a status author a plain
// change_stat(moveSpeed, multiply, 0.5) and have it actually slow the unit
// (the chill-as-composition goal).
// ─────────────────────────────────────────────────────────────────────────────

// unitStatStagesLocked composes the full PER-UNIT (add, mul) stage pool for a
// stat from every emitter that shares the PerkStatModifier vocabulary and
// attaches to the unit itself — owned perks (unitPerkStatModifiersLocked) AND
// active statuses (unitStatusStatModifiersLocked) — merged into one pool via
// mergeStatStagePools. This is the "everything on the unit" half of the
// chokepoint; effectiveStatLocked layers the OWNER's zone auras on top. Read
// sites that must exclude zone auras (e.g. the damage HUD number, which has
// never shown zone auras) call THIS directly instead of effectiveStatLocked.
//
// Returns nil (identity — applyStatStages treats a nil/absent stage as a
// no-op) when nothing on the unit modifies stat. Caller holds s.mu.
func (s *GameState) unitStatStagesLocked(unit *Unit, stat string) map[string]statStageAccum {
	return mergeStatStagePools(
		s.unitPerkStatModifiersLocked(unit, stat),
		s.unitStatusStatModifiersLocked(unit, stat),
	)
}

// effectiveStatLocked folds a unit's base value for a registered stat through
// every emitter that shares the stat vocabulary — owned perks, active
// statuses, and the owner's zone auras — via the shared three-stage engine
// (applyStatStages), per the canonical (base + Σadd) × Πmul rule with
// intrinsic/base/final staging. THE single chokepoint the uniform per-stat
// read sites call.
//
// Covering PerkAuras (unitAuraStatContributionLocked) are deliberately NOT
// folded here: several stats (moveSpeed, critChance, attackSpeed, armor) fold
// their aura contribution at a DIFFERENT arithmetic position (see the
// "ordering trap" doc on perk_aura_stat_cache.go), so aura folding stays
// bespoke at those read sites. effectiveStatLocked covers the perk+status+zone
// pool that IS uniform across every stat; a read site with a bespoke aura fold
// pre-composes its aura contribution into `base` before calling this (see
// effectiveArmorLocked) or resolves the aura separately (moveSpeed).
//
// Returns base unchanged for a nil unit or an unregistered stat — so a read
// site that calls this when nothing modifies the stat behaves exactly as
// before the chokepoint existed. Caller holds s.mu.
func (s *GameState) effectiveStatLocked(unit *Unit, base float64, stat string) float64 {
	if unit == nil || !isKnownStat(stat) {
		return base
	}
	stages := s.unitStatStagesLocked(unit, stat)
	add, mul := s.playerStatModifierLocked(unit.OwnerID, stat)
	return applyStatStages(base, mergeZoneIntoBaseStage(stages, add, mul))
}
