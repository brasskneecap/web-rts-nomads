package game

import "webrts/server/pkg/protocol"

// ─────────────────────────────────────────────────────────────────────────────
// Corpses.
//
// A unit that dies is torn down (nothing points at it, swings at it or flies
// toward it) and moved OUT of s.Units into s.Corpses, where it lingers for
// corpseLifetimeSeconds before leaving the field for good.
//
// The body is the same *Unit value it always was — same ID, same rank, path,
// perks, items and XP — so a revive is a move back into the live registry
// rather than a reconstruction. What it is NOT is a unit: getUnitByIDLocked
// does not resolve it, it is in no spatial index, it grants no vision, it
// blocks no movement, and no `range s.Units` loop can see it.
//
// See docs/design/death_and_corpses.md.
// ─────────────────────────────────────────────────────────────────────────────

// killUnitLocked is the death exit every "cull what died this tick" loop uses:
// resolve the id and leave a body.
//
// The kill paths that predate the pendingDeaths pipeline (combat, projectiles,
// beams, traps, marksman procs) accumulate their own deadUnitIDs slice and
// removed the unit themselves. That call was removeUnitLocked, which now means
// "take it off the field entirely" — so every ordinary combat kill produced no
// corpse at all, only the handful of deaths that route through
// drainPendingDeathsLocked did. This is the one-word fix for those sites.
//
// A unit that is already a corpse (both paths reached it in the same tick) is a
// no-op, as is an id that no longer resolves.
//
// Caller holds s.mu.
func (s *GameState) killUnitLocked(id int) {
	if u := s.getUnitByIDLocked(id); u != nil {
		s.killUnitToCorpseLocked(u)
	}
}

// getCorpseByIDLocked resolves a body by the unit id it had in life, or nil.
//
// The deliberate counterpart to getUnitByIDLocked: code that wants a corpse has
// to say so. Everything else keeps asking for units and keeps correctly getting
// nothing back for a dead one.
//
// Caller holds s.mu.
func (s *GameState) getCorpseByIDLocked(id int) *Unit {
	if s.corpsesByID == nil {
		return nil
	}
	return s.corpsesByID[id]
}

// tickCorpsesLocked counts every body down and removes the ones whose time is
// up. Removal is the ordinary removeUnitLocked path — the tear-down inside it
// is idempotent and already ran at death, so this is just the registry delete
// plus a second harmless sweep for references that cannot exist.
//
// Caller holds s.mu.
func (s *GameState) tickCorpsesLocked(dt float64) {
	if len(s.Corpses) == 0 {
		return
	}
	kept := s.Corpses[:0]
	for _, c := range s.Corpses {
		if c == nil {
			continue
		}
		c.CorpseRemaining -= dt
		if c.CorpseRemaining > 0 {
			kept = append(kept, c)
			continue
		}
		delete(s.corpsesByID, c.ID)
		// The body never re-entered s.Units, so this is a no-op on the live
		// registry; it is here so a corpse leaves through exactly one door.
		s.removeUnitLocked(c.ID)
	}
	s.Corpses = kept
}

// consumeCorpseLocked takes a body off the field permanently — what an ability
// that spends a corpse (raise skeleton) calls once it has used it. Returns
// false when the id is not a corpse, so a caller racing another consumer this
// tick can tell it lost rather than double-spending the body.
//
// Caller holds s.mu.
func (s *GameState) consumeCorpseLocked(id int) bool {
	if s.getCorpseByIDLocked(id) == nil {
		return false
	}
	delete(s.corpsesByID, id)
	kept := s.Corpses[:0]
	for _, c := range s.Corpses {
		if c != nil && c.ID != id {
			kept = append(kept, c)
		}
	}
	s.Corpses = kept
	s.removeUnitLocked(id)
	return true
}

// corpseSnapshotsLocked builds the wire list of bodies. `fow` nil means an
// unfiltered snapshot; otherwise a body is sent only where the viewer has
// vision, exactly like the unit it used to be. Returns nil when there are no
// corpses so the field is omitted from JSON entirely.
//
// Caller holds s.mu (read is sufficient).
func (s *GameState) corpseSnapshotsLocked(fow *PlayerFOW, viewerID string) []protocol.CorpseSnapshot {
	if len(s.Corpses) == 0 {
		return nil
	}
	cellSize := s.MapConfig.CellSize
	out := make([]protocol.CorpseSnapshot, 0, len(s.Corpses))
	for _, c := range s.Corpses {
		if c == nil {
			continue
		}
		// Your own dead are always visible to you, matching how a live unit is
		// exempt from your own fog.
		if fow != nil && c.OwnerID != viewerID && !fow.isClearAtWorld(c.X, c.Y, cellSize) {
			continue
		}
		out = append(out, protocol.CorpseSnapshot{
			ID:              c.ID,
			OwnerID:         c.OwnerID,
			UnitType:        c.UnitType,
			Name:            c.Name,
			Rank:            c.Rank,
			ProgressionPath: c.ProgressionPath,
			Color:           c.Color,
			X:               c.X,
			Y:               c.Y,
			Remaining:       c.CorpseRemaining,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
