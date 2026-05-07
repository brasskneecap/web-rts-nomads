package game

import "webrts/server/pkg/protocol"

// ═════════════════════════════════════════════════════════════════════════════
// MINOR DAMAGE EVENTS — per-tick transient list of ancillary damage hits
//
// Mirror of crit_events.go but for "minor" damage that should render as a
// distinct (smaller, orange) floating number to communicate that it's an
// ancillary effect rather than the main damage source. Currently used by
// ascendant_infusion's Reactive Flames AoE — fires alongside the host trap's
// DoT but should read as splash damage, not "the fire pit suddenly hit
// harder."
//
// The client groups entries per unit for the tick, then in its HP-diff loop
// peels off matching minor amounts before emitting the regular popup —
// portion = orange smaller popup, remainder = normal popup.
//
// Like CritEventSnapshot, this list is purely visual; gameplay logic does
// not depend on it.
// ═════════════════════════════════════════════════════════════════════════════

// minorDamageEvent is one server-side record of an ancillary damage hit.
// Variant maps to a renderer color on the client side (e.g. "fire" = orange
// for Reactive Flames, "electric" = purple for Electrified Caltrops). Empty
// variant defaults to the renderer's "fire" / orange.
// Drained at end-of-tick by resetMinorDamageEventsThisTickLocked.
type minorDamageEvent struct {
	UnitID  int
	Damage  int
	Variant string
}

// recordMinorDamageHitLocked queues an ancillary damage event for the
// floating-number renderer. No-op for damage <= 0 so callers don't need to
// gate on the post-armor amount themselves. variant selects the renderer
// color ("fire" / "electric"); pass "" for the default fire/orange.
func (s *GameState) recordMinorDamageHitLocked(target *Unit, damage int, variant string) {
	if target == nil || damage <= 0 {
		return
	}
	s.minorDamageEventsThisTick = append(s.minorDamageEventsThisTick, minorDamageEvent{
		UnitID:  target.ID,
		Damage:  damage,
		Variant: variant,
	})
}

// snapshotMinorDamageEventsLocked converts the per-tick queue into the wire
// format. Returns nil when empty so the field omits cleanly from JSON.
func (s *GameState) snapshotMinorDamageEventsLocked() []protocol.MinorDamageEventSnapshot {
	if len(s.minorDamageEventsThisTick) == 0 {
		return nil
	}
	out := make([]protocol.MinorDamageEventSnapshot, 0, len(s.minorDamageEventsThisTick))
	for _, e := range s.minorDamageEventsThisTick {
		out = append(out, protocol.MinorDamageEventSnapshot{
			UnitID:  e.UnitID,
			Damage:  e.Damage,
			Variant: e.Variant,
		})
	}
	return out
}

func (s *GameState) resetMinorDamageEventsThisTickLocked() {
	s.minorDamageEventsThisTick = s.minorDamageEventsThisTick[:0]
}
