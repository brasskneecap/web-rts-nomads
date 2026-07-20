package game

// ─────────────────────────────────────────────────────────────────────────────
// Hook 7 — move-speed multiplier
//
// perkMoveSpeedMultiplierLocked returns the effective move-speed multiplier
// contributed by the unit's perks. Always returns ≥ 1.0 (no perk = 1.0).
//
// Used in state.go Update() movement step:
//
//	step := unitMoveSpeed * s.perkMoveSpeedMultiplierLocked(unit) * dt
//
// ADD NEW MOVE-SPEED-MODIFYING PERKS HERE.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) perkMoveSpeedMultiplierLocked(unit *Unit) float64 {
	if unit == nil {
		return 1.0
	}

	bonus := 0.0
	for _, perkID := range unit.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}

		switch perkID {

		case "momentum":
			// State-driven: the post-attack buff is refreshed/decayed in
			// onPerkAttackFiredLocked / tickUnitPerkStateLocked.
			bonus += unit.PerkState.MomentumBonus

		// ── add cases for new move-speed perks below this line ──────────────
		}
	}

	// zealous_march (silver cleric): data-driven aura (PerkDef.Auras,
	// perk_defs.go), resolved once per tick by the generic aura cache
	// (perk_aura_stat_cache.go) and read here in O(1). Landing the read at
	// this EXACT position — additively into `bonus`, before
	// perkMult = 1.0 + bonus below — is load-bearing: see the "ordering
	// trap" doc comment on perk_aura_stat_cache.go for why folding this
	// through the generic (base+add)×mul stat pipeline instead would change
	// how it composes with momentum's bonus (same additive pool). Returns
	// (0, 0) when no covering aura is in range, so it's free when off.
	auraMoveSpeedBonus, _ := s.unitAuraStatContributionLocked(unit, statMoveSpeed)
	bonus += auraMoveSpeedBonus

	perkMult := 1.0 + bonus

	// Zone-aura move speed. The movement step is unit.MoveSpeed × this multiplier,
	// so we return a multiplier m where unit.MoveSpeed × m = (unit.MoveSpeed +
	// add) × mul × perkMult — i.e. the canonical (base + add) × mul composed with
	// the perk multiplier. workerMoveSpeed stacks on top for worker units. No
	// active aura ⇒ (0, 1) and the worker term is skipped, returning perkMult.
	//
	// Data-driven perk stat modifiers (PerkStatModifier{Stat: "moveSpeed"})
	// fold into the same (add, mul) bundle via mergeZoneIntoBaseStage /
	// applyStatStages, ahead of the multiply-by-perkMult step. No perk authors
	// statModifiers today, so this is byte-identical to the prior zone-only
	// fold.
	add, mul := s.playerStatModifierLocked(unit.OwnerID, statMoveSpeed)
	if unit.UnitType == "worker" {
		wAdd, wMul := s.playerStatModifierLocked(unit.OwnerID, statWorkerMoveSpeed)
		add += wAdd
		mul *= wMul
	}
	moveSpeedPerkStages := s.unitPerkStatModifiersLocked(unit, statMoveSpeed)
	if (add != 0 || mul != 1 || len(moveSpeedPerkStages) > 0) && unit.MoveSpeed > 0 {
		effective := applyStatStages(unit.MoveSpeed, mergeZoneIntoBaseStage(moveSpeedPerkStages, add, mul)) * perkMult
		return effective / unit.MoveSpeed
	}

	return perkMult
}
