package game

import "webrts/server/pkg/protocol"

// ═════════════════════════════════════════════════════════════════════════════
// HIT DAMAGE EVENTS — per-tick transient list of individual landed hits
//
// The client derives its floating damage numbers by diffing each unit's HP
// between snapshots, so N hits that land on one unit within a single snapshot
// interval collapse into ONE number equal to their sum (two 12-damage soldier
// strikes → "24"; two 25-damage frostbolts → "50"). The HP delta alone can't
// tell the client it was really 12+12.
//
// This channel restores that granularity: every hit that actually removes HP
// pushes a (UnitID, Damage) entry here at the HP-loss point of
// applyUnitDamageWithSourceLocked. The snapshot drains the list each tick and
// resets it, exactly like crit_events.go.
//
// The client sums the entries for a unit and — when they reconcile with the
// major (post-minor-peel) HP delta AND there are 2+ of them — splits the
// popup into one staggered number per hit. When they DON'T reconcile (passive
// regen landed the same tick, an ancillary/minor instance is mixed in, etc.)
// the client silently ignores this channel and keeps the single combined
// number. That makes the split purely additive: worst case is the current
// behaviour.
//
// Like crit_events.go this list is purely visual — no gameplay logic reads it.
// ═════════════════════════════════════════════════════════════════════════════

// hitDamageEvent is one server-side record of an individual hit's HP loss.
// Cleared at the end of each tick after the snapshot drains it.
type hitDamageEvent struct {
	UnitID int
	Damage int
}

// recordHitDamageLocked queues an individual-hit event for the client's
// per-hit popup splitter. Safe to call with damage <= 0 (no-op) so callers
// don't need to gate on the post-mitigation amount themselves.
//
// Called automatically from applyUnitDamageWithSourceLocked at the HP-loss
// point — do NOT call it manually from perks/abilities; any damage that routes
// through the pipeline is captured here.
func (s *GameState) recordHitDamageLocked(target *Unit, damage int) {
	if target == nil || damage <= 0 {
		return
	}
	s.hitDamageEventsThisTick = append(s.hitDamageEventsThisTick, hitDamageEvent{
		UnitID: target.ID,
		Damage: damage,
	})
}

// snapshotHitDamageEventsLocked converts the per-tick queue into the wire
// format. Returns nil when empty so the field omits cleanly from JSON.
func (s *GameState) snapshotHitDamageEventsLocked() []protocol.DamageHitSnapshot {
	if len(s.hitDamageEventsThisTick) == 0 {
		return nil
	}
	out := make([]protocol.DamageHitSnapshot, 0, len(s.hitDamageEventsThisTick))
	for _, e := range s.hitDamageEventsThisTick {
		out = append(out, protocol.DamageHitSnapshot{
			UnitID: e.UnitID,
			Damage: e.Damage,
		})
	}
	return out
}

// resetHitDamageEventsThisTickLocked truncates the queue. Called once per tick
// alongside the other transient damage channels so the client only ever sees
// hits that landed in the current tick — matches the lifetime of the HP-diff
// damage events on the client.
func (s *GameState) resetHitDamageEventsThisTickLocked() {
	s.hitDamageEventsThisTick = s.hitDamageEventsThisTick[:0]
}
