package game

import "webrts/server/pkg/protocol"

// ═════════════════════════════════════════════════════════════════════════════
// LETHAL DAMAGE EVENTS — per-tick transient list of overkill killing-blow amounts
//
// HP is clamped to 0 on the server, so a unit that takes 100 damage with 5 HP
// remaining loses 5 on the wire — not the 100 actually dealt. The client
// derives its floating damage numbers from HP-diffs and therefore underreports
// killing blows ("overkill"). This list carries the pre-clamp damage value for
// every overkill hit so the client can show the real amount on the killing-
// blow popup.
//
// Only recorded when damage strictly exceeds the target's remaining HP. Exact
// kills (damage == HP) are skipped because the client's HP-delta already
// equals the correct number, so there's nothing for the override to fix.
//
// Drained per tick like crit_events / minor_damage_events.
// ═════════════════════════════════════════════════════════════════════════════

// lethalDamageEvent is one server-side record of an overkill hit. Damage is
// the post-mitigation, post-shield value that was applied to HP before HP
// was clamped to 0 — i.e., what the client should show.
type lethalDamageEvent struct {
	UnitID int
	Damage int
}

// recordLethalDamageLocked queues an overkill kill event for the floating-
// number renderer. Caller must verify the hit actually overkilled (damage >
// prevHP). damage is the raw value that hit HP.
func (s *GameState) recordLethalDamageLocked(target *Unit, damage int) {
	if target == nil || damage <= 0 {
		return
	}
	s.lethalDamageEventsThisTick = append(s.lethalDamageEventsThisTick, lethalDamageEvent{
		UnitID: target.ID,
		Damage: damage,
	})
}

// snapshotLethalDamageEventsLocked converts the per-tick queue into the wire
// format. Returns nil when empty so the field omits cleanly from JSON.
func (s *GameState) snapshotLethalDamageEventsLocked() []protocol.LethalDamageEventSnapshot {
	if len(s.lethalDamageEventsThisTick) == 0 {
		return nil
	}
	out := make([]protocol.LethalDamageEventSnapshot, 0, len(s.lethalDamageEventsThisTick))
	for _, e := range s.lethalDamageEventsThisTick {
		out = append(out, protocol.LethalDamageEventSnapshot{
			UnitID: e.UnitID,
			Damage: e.Damage,
		})
	}
	return out
}

func (s *GameState) resetLethalDamageEventsThisTickLocked() {
	s.lethalDamageEventsThisTick = s.lethalDamageEventsThisTick[:0]
}
