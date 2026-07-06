package game

import "webrts/server/pkg/protocol"

// ═════════════════════════════════════════════════════════════════════════════
// MELEE ATTACK EVENTS — per-tick transient list of melee swings
//
// Ranged attacks are audible to the client for free: a fired shot spawns a
// Projectile that appears in the snapshot, and the client plays the arrow-shot
// sound when a new projectile shows up. Melee attacks have no projectile, so
// there is nothing in the per-tick state for the client to diff against.
//
// This channel restores that signal: every time a melee unit's swing resolves
// (applyDelayedAttackLocked, at windup completion) we push its AttackType here
// — the sound key authored on the unit def (or its promotion path), e.g.
// "swing" / "stab". The snapshot drains the list each tick and the client
// plays the matching effect, exactly like crit_events.go / hit_damage_events.go.
//
// AttackType is resolved server-side (unit.AttackType is seeded from the def at
// spawn and overridden per promotion path in applyRankModifiersLocked) so the
// client never has to redo base+path resolution for audio — it just plays the
// sound named by the event. Units with no AttackType (ranged units, workers)
// never push here; their audio, if any, comes from the projectile.
//
// Like crit_events.go this list is purely presentational — no gameplay logic
// reads it.
// ═════════════════════════════════════════════════════════════════════════════

// meleeAttackEvent is one server-side record of a melee swing resolving,
// tagged with the attacker's world position so the client can drop the sound
// when it's off-screen. Cleared at the end of each tick after the snapshot
// drains it.
type meleeAttackEvent struct {
	AttackType string
	X          float64
	Y          float64
}

// recordMeleeAttackLocked queues a melee-swing event for the client's attack
// sound. Safe to call with an empty attackType (no-op) so the call sites don't
// need to gate on it themselves. x/y is the attacker's position at swing start.
func (s *GameState) recordMeleeAttackLocked(attackType string, x, y float64) {
	if attackType == "" {
		return
	}
	s.meleeAttackEventsThisTick = append(s.meleeAttackEventsThisTick, meleeAttackEvent{
		AttackType: attackType,
		X:          x,
		Y:          y,
	})
}

// snapshotMeleeAttackEventsLocked converts the per-tick queue into the wire
// format. Returns nil when empty so the field omits cleanly from JSON.
func (s *GameState) snapshotMeleeAttackEventsLocked() []protocol.MeleeAttackSnapshot {
	if len(s.meleeAttackEventsThisTick) == 0 {
		return nil
	}
	out := make([]protocol.MeleeAttackSnapshot, 0, len(s.meleeAttackEventsThisTick))
	for _, e := range s.meleeAttackEventsThisTick {
		out = append(out, protocol.MeleeAttackSnapshot{
			AttackType: e.AttackType,
			X:          e.X,
			Y:          e.Y,
		})
	}
	return out
}

// resetMeleeAttackEventsThisTickLocked truncates the queue. Called once per
// tick alongside the other transient event channels so the client only ever
// sees swings that landed in the current tick.
func (s *GameState) resetMeleeAttackEventsThisTickLocked() {
	s.meleeAttackEventsThisTick = s.meleeAttackEventsThisTick[:0]
}
