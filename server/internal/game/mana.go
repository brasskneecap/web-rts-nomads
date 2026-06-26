package game

// Mana resource system.
//
// Mana is an optional per-unit resource used by spellcasters to pay ability
// costs. It is entirely opt-in: a unit with MaxMana == 0 has no mana and is
// skipped by every mana code path here, so non-caster units carry zero
// overhead and unchanged behavior.
//
// Invariants enforced by this file:
//   - CurrentMana is always clamped to [0, MaxMana].
//   - Regen never pushes CurrentMana above MaxMana.
//   - Spending more mana than available fails and changes nothing.
//
// TODO(tuning): mana costs / regen rates are authored per ability and per
// unit (see Part 6 ability defs and Part 7 Acolyte). This file owns only
// the resource mechanics, not the values.

// tickUnitManaRegenLocked advances a unit's passive mana regeneration for one
// tick of length dt seconds. It mirrors the passive HP-regen block in
// Update(): a float accumulator carries fractional mana between ticks so a
// sub-1 mana/s rate still restores integer mana on the correct cadence, and
// the accumulator resets at full mana so a freshly-spent caster doesn't
// instantly get banked regen back.
//
// Skipped (no-op) for: nil units, dead units (HP <= 0), units with no mana
// pool (MaxMana <= 0), and units with a non-positive regen rate. The
// MaxMana <= 0 gate is the "units with 0 max_mana skip regeneration" rule.
//
// Caller holds s.mu.
func (s *GameState) tickUnitManaRegenLocked(unit *Unit, dt float64) {
	if unit == nil || unit.HP <= 0 || unit.MaxMana <= 0 {
		return
	}
	rate := s.effectiveManaRegenLocked(unit)
	if rate <= 0 {
		return
	}
	if unit.CurrentMana >= unit.MaxMana {
		// Clamp defensively (in case MaxMana was lowered) and park the
		// accumulator so the next spend starts regen from zero.
		unit.CurrentMana = unit.MaxMana
		unit.ManaRegenAccumulator = 0
		return
	}
	unit.ManaRegenAccumulator += rate * dt
	if unit.ManaRegenAccumulator < 1 {
		return
	}
	gain := int(unit.ManaRegenAccumulator)
	unit.ManaRegenAccumulator -= float64(gain)
	unit.CurrentMana += gain
	if unit.CurrentMana >= unit.MaxMana {
		unit.CurrentMana = unit.MaxMana
		unit.ManaRegenAccumulator = 0
	}
}

// effectiveManaRegenLocked returns the unit's current passive mana regen
// rate in mana/second, including any covering Mana Conduit aura bonus from
// an allied Cleric (max-wins across multiple covering sources). Returns 0
// when the unit has no mana pool. Shared between the regen tick and the
// snapshot builder so the HUD's stat row always shows the same effective
// rate the simulation will apply this tick.
//
// Caller holds s.mu.
func (s *GameState) effectiveManaRegenLocked(unit *Unit) float64 {
	if unit == nil || unit.MaxMana <= 0 {
		return 0
	}
	rate := unit.ManaRegenPerSecond + s.manaConduitAuraBonusLocked(unit)
	// Zone-aura mana regen, folded read-on-demand as (base + add) × mul. Applied
	// at this shared chokepoint so the regen tick and the HUD stat row agree.
	if add, mul := s.playerStatModifierLocked(unit.OwnerID, statManaRegen); add != 0 || mul != 1 {
		rate = (rate + add) * mul
	}
	return rate
}

// spendUnitManaLocked attempts to deduct cost mana from unit and reports
// whether it succeeded. It is the single entry point for paying a mana cost
// (ability casts, etc.).
//
//   - cost <= 0: nothing to pay — succeeds without touching state (lets
//     free abilities / basic attacks go through the same call site uniformly).
//   - unit has no mana pool (MaxMana <= 0) but a positive cost is required:
//     fails (a costed ability cannot be paid by a unit with no mana).
//   - insufficient CurrentMana: fails gracefully, mana is left unchanged.
//   - otherwise: deducts cost, clamps at 0, succeeds.
//
// Mirrors healUnitLocked's nil/guard style. Caller holds s.mu.
func (s *GameState) spendUnitManaLocked(unit *Unit, cost int) bool {
	if cost <= 0 {
		return true
	}
	if unit == nil || unit.MaxMana <= 0 {
		return false
	}
	if unit.CurrentMana < cost {
		return false
	}
	unit.CurrentMana -= cost
	if unit.CurrentMana < 0 {
		unit.CurrentMana = 0
	}
	return true
}

// addUnitManaLocked grants `amount` mana to unit, clamped to [0, MaxMana].
// Returns the amount actually granted (0 when the unit has no mana pool, is
// already at max, or amount <= 0). Symmetric to spendUnitManaLocked — this
// is the single entry point for restoring mana so perks / abilities don't
// each re-implement the clamping + nil-guard logic.
//
// Auto-emits a manaRestoreEvent for the client when gain > 0 so every
// intentional grant produces a blue "+N" floating popup. Passive regen
// deliberately bypasses this helper (it mutates Unit.CurrentMana directly
// in tickUnitManaRegenLocked) so the +1/5s drip never spams popups —
// that's the load-bearing reason intentional vs passive grants are split
// across two code paths.
//
// Caller holds s.mu.
func (s *GameState) addUnitManaLocked(unit *Unit, amount int) int {
	if amount <= 0 || unit == nil || unit.MaxMana <= 0 || unit.HP <= 0 {
		return 0
	}
	room := unit.MaxMana - unit.CurrentMana
	if room <= 0 {
		return 0
	}
	gain := amount
	if gain > room {
		gain = room
	}
	unit.CurrentMana += gain
	s.recordManaRestoreEventLocked(unit, gain)
	return gain
}
