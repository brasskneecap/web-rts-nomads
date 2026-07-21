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
// an allied Cleric (max-wins across multiple covering sources — mana_conduit
// carries no PerAdditionalSource, so this is pure max-wins with no stacking
// term at all, unlike zealous_march). Returns 0 when the unit has no mana
// pool. Shared between the regen tick and the snapshot builder so the HUD's
// stat row always shows the same effective rate the simulation will apply
// this tick.
//
// mana_conduit is fully data-driven: PerkDef.Auras (perk_defs.go, catalog
// JSON) declares the radius/targets/self-inclusion/value, and the generic
// per-tick cache (perk_aura_stat_cache.go) resolves it for every recipient
// with zero perk-specific Go — see perks_cleric.go's mana_conduit section.
// The read below lands at the EXACT arithmetic position the deleted bespoke
// helper (manaConduitAuraBonusLocked) occupied: added into `rate` BEFORE the
// zone/perk-stat-modifier (base + add) × mul fold runs. This is load-bearing
// — see perk_aura_stat_cache.go's "ordering trap" doc and
// perk_aura_migration_test.go's TestManaConduitMigration_ZoneAuraOrderingGuard
// for why folding it through the generic zone pipeline instead would change
// how it composes with an active zone manaRegen aura.
//
// Caller holds s.mu.
func (s *GameState) effectiveManaRegenLocked(unit *Unit) float64 {
	if unit == nil || unit.MaxMana <= 0 {
		return 0
	}
	auraBonus, _ := s.unitAuraStatContributionLocked(unit, statManaRegen)
	rate := unit.ManaRegenPerSecond + auraBonus
	// Fold the perk + status + zone-aura "manaRegen" pool onto rate through the
	// shared chokepoint so the regen tick and the HUD stat row agree. The
	// bespoke PerkAura bonus is already in `rate` above (different arithmetic
	// position — see effectiveStatLocked's aura note). Empty pool + no zone
	// aura ⇒ identity, byte-identical to the pre-chokepoint zone-only fold.
	return s.effectiveStatLocked(unit, rate, statManaRegen)
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
	// Feed the Arcane Charge loop: every mana point spent by a unit with the
	// arcane_missiles charge-fire passive builds toward its next auto-volley.
	// No-op for every unit without the passive (arch-mage-spell-system).
	s.accrueArcaneChargeLocked(unit, cost)
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
