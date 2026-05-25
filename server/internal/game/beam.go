package game

import (
	"fmt"

	"webrts/server/pkg/protocol"
)

// Beam is a channeled-beam visual entity that exists for the duration of a
// unit's channel. It carries no simulation state — all damage, mana, and
// stop logic is driven by the Unit's Channel* fields. Beam is the visual
// entity the client renders between caster and target.
//
// ID-not-pointer rule: CasterUnitID and TargetUnitID are integer IDs.
// The Beam is removed when the channel stops (stopUnitChannelLocked /
// clearUnitChannelLocked) or when either participant is removed from the
// game (removeBeamForUnitLocked / removeBeamForTargetLocked).
type Beam struct {
	// ID is the stable wire identifier for this beam (e.g. "beam-0").
	ID string
	// CasterUnitID is the ID of the unit channeling the ability.
	CasterUnitID int
	// TargetUnitID is the ID of the enemy unit being drained.
	TargetUnitID int
	// OwnerPlayerID is the player who owns the caster (for FOW filtering).
	OwnerPlayerID string
	// AbilityID is the ability driving this beam (e.g. "siphon_life").
	AbilityID string
	// Variant is the client-side renderer variant (e.g. "siphon_life").
	Variant string
}

// spawnBeamLocked creates a new Beam entity, appends it to s.Beams, and
// returns it. Called by beginAbilityChannelLocked when a channel starts.
//
// Caller holds s.mu write lock.
func (s *GameState) spawnBeamLocked(caster *Unit, target *Unit, abilityID, variant string) *Beam {
	b := &Beam{
		ID:            fmt.Sprintf("beam-%d", s.nextBeamID),
		CasterUnitID:  caster.ID,
		TargetUnitID:  target.ID,
		OwnerPlayerID: caster.OwnerID,
		AbilityID:     abilityID,
		Variant:       variant,
	}
	s.nextBeamID++
	s.Beams = append(s.Beams, b)
	return b
}

// removeBeamForUnitLocked drops any beam whose CasterUnitID == unitID.
// Called from stopUnitChannelLocked and clearUnitChannelLocked, and also
// from removeUnitLocked so a dying caster's beam doesn't linger.
//
// Caller holds s.mu write lock.
func (s *GameState) removeBeamForUnitLocked(unitID int) {
	if len(s.Beams) == 0 {
		return
	}
	kept := s.Beams[:0]
	for _, b := range s.Beams {
		if b.CasterUnitID == unitID {
			continue // drop
		}
		kept = append(kept, b)
	}
	s.Beams = kept
}

// removeBeamByIDLocked drops the beam with the given stable wire ID. No-op
// when no beam matches (the caller may be cleaning up state that was already
// removed via a different path — e.g. removeBeamForTargetLocked firing on a
// dead chain victim before chain_siphon's per-tick sync runs).
//
// Caller holds s.mu write lock.
func (s *GameState) removeBeamByIDLocked(id string) {
	if len(s.Beams) == 0 || id == "" {
		return
	}
	kept := s.Beams[:0]
	for _, b := range s.Beams {
		if b.ID == id {
			continue // drop
		}
		kept = append(kept, b)
	}
	s.Beams = kept
}

// removeBeamForTargetLocked drops any beam whose TargetUnitID == targetID.
// Called from removeUnitLocked so a beam whose target died is dropped
// immediately. The channel tick also catches this on the next tick, but
// removing the beam here keeps the visual state clean during the same tick
// the target dies.
//
// Caller holds s.mu write lock.
func (s *GameState) removeBeamForTargetLocked(targetID int) {
	if len(s.Beams) == 0 {
		return
	}
	kept := s.Beams[:0]
	for _, b := range s.Beams {
		if b.TargetUnitID == targetID {
			continue // drop
		}
		kept = append(kept, b)
	}
	s.Beams = kept
}

// beamSnapshotsLocked builds the wire-format beam slice for a snapshot.
// When fow is nil (spectator / unfiltered Snapshot()), all beams are
// included. When fow is non-nil (SnapshotForPlayer), a beam is included only
// when the caster OR the target is visible to the viewer — matching the
// pattern projectiles use.
//
// Caller holds s.mu (read lock is sufficient).
func (s *GameState) beamSnapshotsLocked(fow *PlayerFOW) []protocol.BeamSnapshot {
	if len(s.Beams) == 0 {
		return nil
	}
	var out []protocol.BeamSnapshot
	for _, b := range s.Beams {
		if fow != nil {
			// FOW filter: include the beam when either endpoint is visible.
			casterVisible := false
			targetVisible := false
			if caster := s.getUnitByIDLocked(b.CasterUnitID); caster != nil {
				casterVisible = fow.isClearAtWorld(caster.X, caster.Y, s.MapConfig.CellSize)
			}
			if target := s.getUnitByIDLocked(b.TargetUnitID); target != nil {
				targetVisible = fow.isClearAtWorld(target.X, target.Y, s.MapConfig.CellSize)
			}
			if !casterVisible && !targetVisible {
				continue
			}
		}
		out = append(out, protocol.BeamSnapshot{
			ID:           b.ID,
			CasterUnitId: b.CasterUnitID,
			TargetUnitId: b.TargetUnitID,
			OwnerId:      b.OwnerPlayerID,
			AbilityId:    b.AbilityID,
			Variant:      b.Variant,
		})
	}
	return out
}
