package game

import "webrts/server/pkg/protocol"

// ═════════════════════════════════════════════════════════════════════════════
// MANA RESTORE EVENTS — per-tick transient list of intentional mana grants
//
// Mirror of heal_events.go but for mana restored to a unit. The client
// spawns a blue "+N" floating popup over the unit when an entry lands, so
// the player can see when a perk grants them mana (Repurposed Life, future
// cleric mana abilities, etc.).
//
// PASSIVE REGEN INTENTIONALLY DOES NOT EMIT. The natural 0.2 mana/s drip
// would produce one popup every 5 seconds per spellcaster — visual noise
// with no information. Passive regen mutates Unit.CurrentMana directly in
// tickUnitManaRegenLocked and never calls addUnitManaLocked, so it bypasses
// this channel cleanly. Only intentional ability / perk grants that route
// through addUnitManaLocked produce a popup.
//
// Drained at end-of-tick by resetManaRestoreEventsThisTickLocked, same
// lifecycle as the other transient event queues (crit, minor damage, heal,
// damage type hints, lethal).
// ═════════════════════════════════════════════════════════════════════════════

// manaRestoreEvent is one server-side record of a mana grant landing on a
// unit. Amount is the actual amount granted (post-clamp at MaxMana), so
// the popup never shows "+5" when only 2 mana actually fit in the pool.
type manaRestoreEvent struct {
	UnitID int
	Amount int
}

// recordManaRestoreEventLocked queues a mana grant for the floating-number
// renderer. No-op for amount <= 0 so callers don't need to gate themselves
// (addUnitManaLocked clamps the amount before calling and may pass 0 when
// the pool is full — that's the "no popup" path).
//
// Caller holds s.mu write lock.
func (s *GameState) recordManaRestoreEventLocked(target *Unit, amount int) {
	if target == nil || amount <= 0 {
		return
	}
	s.manaRestoreEventsThisTick = append(s.manaRestoreEventsThisTick, manaRestoreEvent{
		UnitID: target.ID,
		Amount: amount,
	})
}

// snapshotManaRestoreEventsLocked converts the per-tick queue into wire
// format. Returns nil when empty so the JSON field omits cleanly.
//
// Caller holds s.mu (read or write).
func (s *GameState) snapshotManaRestoreEventsLocked() []protocol.ManaRestoreEventSnapshot {
	if len(s.manaRestoreEventsThisTick) == 0 {
		return nil
	}
	out := make([]protocol.ManaRestoreEventSnapshot, 0, len(s.manaRestoreEventsThisTick))
	for _, e := range s.manaRestoreEventsThisTick {
		out = append(out, protocol.ManaRestoreEventSnapshot{
			UnitID: e.UnitID,
			Amount: e.Amount,
		})
	}
	return out
}

// resetManaRestoreEventsThisTickLocked drops the per-tick queue. Called
// from the same place the other transient event queues reset so they all
// lifecycle in lockstep.
func (s *GameState) resetManaRestoreEventsThisTickLocked() {
	s.manaRestoreEventsThisTick = s.manaRestoreEventsThisTick[:0]
}
