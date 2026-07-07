package game

import "webrts/server/pkg/protocol"

// evadeEvent is one avoided basic attack this tick — the defender's ID and
// which stat avoided it ("dodge" | "block"). Drained into the snapshot each
// tick like minorDamageEvent.
type evadeEvent struct {
	UnitID int
	Kind   string
}

// recordEvadeEventLocked queues a dodge/block popup over the defender.
// Caller holds s.mu.
func (s *GameState) recordEvadeEventLocked(target *Unit, kind string) {
	if target == nil || kind == "" {
		return
	}
	s.evadeEventsThisTick = append(s.evadeEventsThisTick, evadeEvent{UnitID: target.ID, Kind: kind})
}

// snapshotEvadeEventsLocked converts this tick's evade events to their wire
// form. Caller holds s.mu.
func (s *GameState) snapshotEvadeEventsLocked() []protocol.EvadeEventSnapshot {
	if len(s.evadeEventsThisTick) == 0 {
		return nil
	}
	out := make([]protocol.EvadeEventSnapshot, len(s.evadeEventsThisTick))
	for i, e := range s.evadeEventsThisTick {
		out[i] = protocol.EvadeEventSnapshot{UnitID: e.UnitID, Kind: e.Kind}
	}
	return out
}
