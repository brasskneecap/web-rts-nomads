package game

import "webrts/server/pkg/protocol"

// effectiveVisionRangeLocked returns the unit's vision range after applying
// all perk multipliers. Caller must hold s.mu.
func (s *GameState) effectiveVisionRangeLocked(u *Unit) float64 {
	return u.VisionRange * s.perkVisionRangeMultiplierLocked(u)
}

// recomputeFOWLocked rebuilds every player's FOW grid from scratch each tick:
//  1. Clear the Clear bits (preserve EverSeen).
//  2. Stamp vision circles from each living, visible player unit.
//  3. Stamp vision from player-owned buildings (Q2).
//  4. Snapshot any building now in a Clear cell into KnownBuildings.
//
// Caller must hold s.mu write lock.
func (s *GameState) recomputeFOWLocked() {
	blocking := s.getVisionBlockingCellsLocked()

	for playerID, fow := range s.FOW {
		fow.clearClearBits()

		for _, u := range s.Units {
			if u.OwnerID != playerID {
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
			if b.OwnerID == nil || *b.OwnerID != playerID {
				continue
			}
			cx := (float64(b.GridCoord.X) + float64(b.Width)/2.0) * s.MapConfig.CellSize
			cy := (float64(b.GridCoord.Y) + float64(b.Height)/2.0) * s.MapConfig.CellSize
			fow.stampCircle(cx, cy, buildingVisionRange(b.BuildingType), s.MapConfig.CellSize, blocking)
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
