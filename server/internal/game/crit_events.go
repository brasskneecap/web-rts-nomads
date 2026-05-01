package game

import "webrts/server/pkg/protocol"

// ═════════════════════════════════════════════════════════════════════════════
// CRIT EVENTS — per-tick transient list of critical hits
//
// The damage pipeline applies crit damage in several places (state_combat
// primary, split shot extras, double shot deferred, pierce primary). Each
// time a crit lands, we push a small (UnitID, Damage) entry here. The
// snapshot drains the list to the client every tick and the queue resets.
//
// The client uses these entries to match its HP-diff damage events: when a
// damage event's (target, amount) matches a crit entry, the floating-number
// renderer draws a red circle behind the number. Match on amount lets the
// edge case "one crit + one normal hit on the same target same tick" pick
// the right one.
//
// This list is purely visual — no gameplay logic reads it. Splash damage,
// burn ticks, etc. don't push entries because they're not "shots that
// crit'd".
// ═════════════════════════════════════════════════════════════════════════════

// critEvent is one server-side record of a crit landing. Cleared at the end
// of each tick after the snapshot drains it.
type critEvent struct {
	UnitID int
	Damage int
}

// recordCritHitLocked queues a critical-hit event for the floating-number
// renderer. Safe to call with damage <= 0 (no-op) so callers don't need to
// gate on the post-armor amount themselves.
func (s *GameState) recordCritHitLocked(target *Unit, damage int) {
	if target == nil || damage <= 0 {
		return
	}
	s.critEventsThisTick = append(s.critEventsThisTick, critEvent{
		UnitID: target.ID,
		Damage: damage,
	})
}

// snapshotCritEventsLocked converts the per-tick queue into the wire format.
// Returns nil when empty so the field omits cleanly from JSON.
func (s *GameState) snapshotCritEventsLocked() []protocol.CritEventSnapshot {
	if len(s.critEventsThisTick) == 0 {
		return nil
	}
	out := make([]protocol.CritEventSnapshot, 0, len(s.critEventsThisTick))
	for _, e := range s.critEventsThisTick {
		out = append(out, protocol.CritEventSnapshot{
			UnitID: e.UnitID,
			Damage: e.Damage,
		})
	}
	return out
}

// resetCritEventsThisTickLocked truncates the queue. Called once per tick at
// the end of the snapshot serialization so the client only ever sees crits
// that landed in the current tick — matches the lifetime of HP-diff damage
// events on the client.
func (s *GameState) resetCritEventsThisTickLocked() {
	s.critEventsThisTick = s.critEventsThisTick[:0]
}
