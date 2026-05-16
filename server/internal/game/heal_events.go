package game

import "webrts/server/pkg/protocol"

// ═════════════════════════════════════════════════════════════════════════════
// HEAL EVENTS — per-tick transient list of healing applied to units
//
// Mirrors crit_events.go. Unlike damage, the client has no HP-diff signal it
// can turn into a floating "+N" (HP going up isn't tracked as a damage event),
// so the server explicitly records each meaningful heal. The snapshot drains
// the list every tick and the renderer spawns a light-green "+N" over the
// target unit, resolving the unit's live position from the snapshot by ID.
//
// Scope: intentional heals only (the heal ability / any AbilityDef.HealAmount).
// Passive HP regen is deliberately NOT recorded — it ticks ~constantly in tiny
// amounts and would spam a +1 over every unit. This list is purely visual; no
// gameplay logic reads it.
// ═════════════════════════════════════════════════════════════════════════════

// healEvent is one server-side record of healing landing on a unit. Cleared at
// the end of each tick after the snapshot drains it.
type healEvent struct {
	UnitID int
	Amount int
}

// recordHealEventLocked queues a heal event for the floating-number renderer.
// Safe to call with amount <= 0 (no-op) so callers don't need to gate on the
// post-clamp amount themselves.
func (s *GameState) recordHealEventLocked(target *Unit, amount int) {
	if target == nil || amount <= 0 {
		return
	}
	s.healEventsThisTick = append(s.healEventsThisTick, healEvent{
		UnitID: target.ID,
		Amount: amount,
	})
}

// snapshotHealEventsLocked converts the per-tick queue into the wire format.
// Returns nil when empty so the field omits cleanly from JSON.
func (s *GameState) snapshotHealEventsLocked() []protocol.HealEventSnapshot {
	if len(s.healEventsThisTick) == 0 {
		return nil
	}
	out := make([]protocol.HealEventSnapshot, 0, len(s.healEventsThisTick))
	for _, e := range s.healEventsThisTick {
		out = append(out, protocol.HealEventSnapshot{
			UnitID: e.UnitID,
			Amount: e.Amount,
		})
	}
	return out
}

// resetHealEventsThisTickLocked truncates the queue. Called once per tick
// alongside the other event-queue resets so the client only ever sees heals
// that landed in the current tick.
func (s *GameState) resetHealEventsThisTickLocked() {
	s.healEventsThisTick = s.healEventsThisTick[:0]
}
