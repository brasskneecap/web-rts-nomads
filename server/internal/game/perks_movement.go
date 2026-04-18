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
	if unit == nil || len(unit.PerkIDs) == 0 {
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
	return 1.0 + bonus
}
