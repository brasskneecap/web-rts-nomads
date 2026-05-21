package game

// Focus Target — server-side lifecycle helpers.
//
// Focus Target is a player-issued sticky support assignment (Cleric / spellcaster
// primary use case). When active, the unit follows the focus ally and prioritises
// healing them. Focus is modelled as an OrderFocusFollow order paired with a
// FocusTargetID field on the unit.
//
// All mutations go through the three helpers below so every transition is
// observable from one place. The rest of the simulation reads FocusTargetID and
// validates it per-tick via validateFocusTargetLocked.

// RequestSetFocusTargetLocked sets or clears the focus target for casterUnitID.
// playerID must own the caster. When targetUnitID == 0 the focus is cleared.
// When targetUnitID != 0 the target must be alive, visible, and on the same team
// as the caster — otherwise a descriptive error string is returned and no state
// changes.
//
// Caller holds s.mu.
func (s *GameState) RequestSetFocusTargetLocked(playerID string, casterUnitID int, targetUnitID int) (bool, string) {
	caster := s.getUnitByIDLocked(casterUnitID)
	if caster == nil || caster.HP <= 0 {
		return false, "Invalid caster."
	}
	if caster.OwnerID != playerID {
		return false, "You do not own that unit."
	}

	// TargetUnitID == 0 means "clear focus".
	if targetUnitID == 0 {
		s.clearFocusTargetLocked(caster)
		return true, ""
	}

	target := s.getUnitByIDLocked(targetUnitID)
	if target == nil || target.HP <= 0 || !target.Visible {
		return false, "Invalid focus target."
	}
	// Focus target must be a same-team ally (not an enemy).
	if !s.unitsFriendlyLocked(caster, target) {
		return false, "Focus target must be an ally."
	}

	// Set the order and focus ID together so they never diverge.
	caster.Order = OrderState{Type: OrderFocusFollow}
	caster.FocusTargetID = targetUnitID

	// Setting a Focus Target implies "actively support that ally" — which is
	// only meaningful if the caster's heal-class abilities are armed. Without
	// this, a fresh Cleric with autocast disabled would do nothing despite an
	// active focus, which contradicts the player's explicit intent. Enable
	// autocast on every heal-category ability the caster owns; non-heal
	// abilities are left untouched so this doesn't toggle (e.g.) Arch Mage
	// arcane_bolt on as a side effect.
	enableHealAutoCastForFocusLocked(caster)
	return true, ""
}

// enableHealAutoCastForFocusLocked flips on autocast for every heal-category
// ability on the unit's ability bar. Idempotent: an already-on autocast stays
// on; this never DISABLES anything. Called from RequestSetFocusTargetLocked
// when focus is being SET (not cleared) so the act of assigning a Focus Target
// arms the Cleric for the support role implied by that assignment.
//
// Caller holds s.mu.
func enableHealAutoCastForFocusLocked(caster *Unit) {
	if caster == nil || len(caster.Abilities) == 0 {
		return
	}
	for _, abilityID := range caster.Abilities {
		def, ok := getAbilityDef(abilityID)
		if !ok {
			continue
		}
		if def.Category != AbilityCategoryHeal {
			continue
		}
		if !def.SupportsAutoCast {
			continue // ability doesn't expose an autocast toggle at all
		}
		if caster.AutoCastEnabled == nil {
			caster.AutoCastEnabled = make(map[string]bool, 1)
		}
		caster.AutoCastEnabled[abilityID] = true
	}
}

// clearFocusTargetLocked clears the focus target. If the unit is currently in
// OrderFocusFollow, the order transitions to OrderIdle and the in-flight path
// is cancelled so the unit doesn't keep following a stale destination. Always
// zeroes FocusTargetID. Safe to call when no focus is active.
//
// Caller holds s.mu.
func (s *GameState) clearFocusTargetLocked(unit *Unit) {
	if unit == nil {
		return
	}
	unit.FocusTargetID = 0
	if unit.Order.Type == OrderFocusFollow {
		unit.Order = OrderState{Type: OrderIdle}
		// Cancel the follow path so the unit doesn't drift to a stale
		// destination after focus clears. Moving stays false until the next
		// order or the auto-heal AI picks up a target.
		unit.Path = nil
		unit.Moving = false
	}
}

// RequestSetFocusTarget is the lock-acquiring public entry point for the WS
// handler. It acquires s.mu, then delegates to RequestSetFocusTargetLocked.
func (s *GameState) RequestSetFocusTarget(playerID string, casterUnitID int, targetUnitID int) (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.RequestSetFocusTargetLocked(playerID, casterUnitID, targetUnitID)
}

// validateFocusTargetLocked re-resolves the unit's focus target and clears it
// if the target is no longer valid (nil, dead, invisible, or switched teams).
// Returns true when the focus is still valid after the check. The caller is
// responsible for calling this before any focus-dependent per-tick logic so
// subsequent code can safely use FocusTargetID without re-validating.
//
// Caller holds s.mu.
func (s *GameState) validateFocusTargetLocked(unit *Unit) bool {
	if unit == nil || unit.FocusTargetID == 0 {
		return false
	}
	focus := s.getUnitByIDLocked(unit.FocusTargetID)
	if focus == nil || focus.HP <= 0 || !focus.Visible || !s.unitsFriendlyLocked(unit, focus) {
		s.clearFocusTargetLocked(unit)
		return false
	}
	return true
}
