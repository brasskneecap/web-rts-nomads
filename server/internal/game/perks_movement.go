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

	// zealous_march (silver cleric): max-wins aura contribution from nearby
	// allied Clerics. Lives in perks_cleric_silver.go so all four cleric
	// silver perks stay colocated. Returns 0 when no covering aura is in
	// range, so it's free when off.
	bonus += s.perkMoveSpeedBonusFromClericAurasLocked(unit)

	return 1.0 + bonus
}
