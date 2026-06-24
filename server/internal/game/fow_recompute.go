package game

import "webrts/server/pkg/protocol"

// effectiveVisionRangeLocked returns the unit's vision range after applying
// all perk multipliers. Caller must hold s.mu.
func (s *GameState) effectiveVisionRangeLocked(u *Unit) float64 {
	return u.VisionRange * s.perkVisionRangeMultiplierLocked(u)
}

// fowOwnerSharesVisionLocked reports whether vision from ownerID's units /
// buildings should be stamped into viewerID's FOW. ownerID must be a real,
// vision-having player (it has an s.FOW entry — this excludes the __enemy__
// AI and neutral/unowned "" owners, which never grant players sight) AND be
// allied with the viewer (same team; self included).
//
// At the default single team this reduces, for a lone player, to
// "ownerID == viewerID" — byte-identical to the pre-team behavior. For
// allied co-op players on the same team it yields shared vision (the
// approved P2 feature). Cross-team players never share sight.
//
// Caller must hold s.mu.
func (s *GameState) fowOwnerSharesVisionLocked(ownerID, viewerID string) bool {
	if ownerID == "" {
		return false
	}
	if _, real := s.FOW[ownerID]; !real {
		return false
	}
	return s.playersAreFriendlyLocked(ownerID, viewerID)
}

// recomputeFOWLocked rebuilds every player's FOW grid from scratch each tick:
//  1. Clear the Clear bits (preserve EverSeen).
//  2. Stamp vision circles from each living, visible unit owned by the
//     player OR an allied (same-team) player — shared team vision.
//  3. Stamp vision from player-/ally-owned buildings.
//  4. Snapshot any building now in a Clear cell into KnownBuildings.
//
// Determinism: each player's grid is computed independently and vision union
// is commutative, so the s.FOW / s.Units iteration order never drives the
// result. Caller must hold s.mu write lock.
func (s *GameState) recomputeFOWLocked() {
	blocking := s.getVisionBlockingCellsLocked()

	for playerID, fow := range s.FOW {
		fow.clearClearBits()

		for _, u := range s.Units {
			if !s.fowOwnerSharesVisionLocked(u.OwnerID, playerID) {
				continue
			}
			if u.HP <= 0 || !u.Visible {
				continue
			}
			vr := s.effectiveVisionRangeLocked(u)
			unitBlocking := blocking
			if u.Flyer {
				unitBlocking = nil // flyers see over terrain and obstacles
			}
			fow.stampCircle(u.X, u.Y, vr, s.MapConfig.CellSize, unitBlocking)
		}

		for i := range s.MapConfig.Buildings {
			b := &s.MapConfig.Buildings[i]
			if b.OwnerID == nil || !s.fowOwnerSharesVisionLocked(*b.OwnerID, playerID) {
				continue
			}
			// A pending-start building (placed but no worker has begun
			// construction) grants no vision yet — it is a reserved ghost, not a
			// working structure.
			if buildingPendingStart(b) {
				continue
			}
			cx := (float64(b.GridCoord.X) + float64(b.Width)/2.0) * s.MapConfig.CellSize
			cy := (float64(b.GridCoord.Y) + float64(b.Height)/2.0) * s.MapConfig.CellSize
			buildingBlocking := blocking
			if def, ok := getBuildingDef(b.BuildingType); ok && def.UnobstructedVision {
				buildingBlocking = nil // sees over trees/obstacles/terrain (like flyers)
			}
			fow.stampCircle(cx, cy, buildingVisionRange(b.BuildingType), s.MapConfig.CellSize, buildingBlocking)
		}

		// Reveal owned-zone interiors: a zone controlled by the viewer's team is
		// fully lit regardless of unit/building line-of-sight, so holding
		// territory grants map awareness over it. zonesAlliedLocked handles the
		// team sentinel and allied players; neutral/enemy/unowned zones are not
		// revealed. Runs before the KnownBuildings snapshot so structures inside
		// a held zone become known too.
		if len(s.Zones) > 0 {
			for zi := range s.Zones {
				rt := &s.Zones[zi]
				if !s.zonesAlliedLocked(rt.Owner, playerID) {
					continue
				}
				for _, c := range rt.Def.Cells {
					fow.markClearCell(c[0], c[1])
				}
			}
		}

		for i := range s.MapConfig.Buildings {
			b := &s.MapConfig.Buildings[i]
			if fow.anyFootprintClear(b) {
				clone := *b
				fow.KnownBuildings[b.ID] = &clone
			}
		}
	}
}

// buildingVisionRange returns the vision radius in world pixels for a
// player-owned building. Reads visionRange from the building catalog;
// buildings without an explicit value default to 320 px (~5 cells).
func buildingVisionRange(buildingType string) float64 {
	if def, ok := getBuildingDef(buildingType); ok && def.VisionRange > 0 {
		return def.VisionRange
	}
	return 320
}

// packFOW converts a PlayerFOW into the wire-format FogOfWarSnapshot.
func packFOW(fow *PlayerFOW, revTick int) *protocol.FogOfWarSnapshot {
	if fow == nil {
		return nil
	}
	return &protocol.FogOfWarSnapshot{
		Cols:    fow.Cols,
		Rows:    fow.Rows,
		Runs:    fow.encodeRLE(),
		RevTick: revTick,
	}
}
