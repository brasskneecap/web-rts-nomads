package game

import (
	"fmt"
	"testing"

	"webrts/server/pkg/protocol"
)

// TestCatalogMaps_TargetedWaveSpawnRoutesReachable asserts, for every shipped
// map, that each enemy-spawnpoint with a targetPlayerLabel has a terrain-only
// fine-A* route (within the node budget) from the spawnpoint to that player's
// townhall. If this fails the wave cannot march on its target base: the spawn
// pathfind arms the army-wide objectiveUnreachableUntil cache and every unit
// falls back to chasing the nearest hostile instead (in the field: forest-1
// player-2 waves pooling around the NE zone). A failure means either the map
// walls off the route or unitPathExpansionFactor is too low for the detour.
func TestCatalogMaps_TargetedWaveSpawnRoutesReachable(t *testing.T) {
	for _, entry := range mapCatalog {
		entry := entry
		t.Run(entry.ID, func(t *testing.T) {
			s := NewGameStateWithSeed(cloneMapConfig(entry.Map), 42)
			s.mu.Lock()
			defer s.mu.Unlock()
			tr := injectPathTracker(s)

			// Claim every labeled townhall so the map is in its "all players
			// joined" runtime shape (occupied townhalls are visible and count
			// as terrain obstacles for routes past other bases).
			townhallByLabel := map[string]*protocol.BuildingTile{}
			for i := range s.MapConfig.Buildings {
				b := &s.MapConfig.Buildings[i]
				if b.BuildingType != "spawn-point" {
					continue
				}
				label, ok := getMetadataString(b.Metadata, "playerLabel")
				if !ok || label == "" {
					continue
				}
				th := s.resolveSpawnPointTownhallLocked(*b, true)
				if th == nil {
					continue
				}
				claimed := s.claimSpecificTownhallForPlayerLocked("test-"+label, th.ID)
				if claimed != nil {
					townhallByLabel[label] = claimed
				}
			}

			blocked := s.getBlockedCellsLocked()
			probe := &Unit{ID: -1}

			for i := range s.MapConfig.Buildings {
				b := &s.MapConfig.Buildings[i]
				if b.BuildingType != "enemy-spawnpoint" {
					continue
				}
				label, ok := getMetadataString(b.Metadata, "targetPlayerLabel")
				if !ok || label == "" || label == "__none__" {
					continue
				}
				th := townhallByLabel[label]
				if th == nil {
					t.Errorf("%s: enemy-spawnpoint %s targets label %q with no resolvable townhall",
						entry.ID, b.ID, label)
					continue
				}

				spawnCenter := protocol.Vec2{
					X: (float64(b.X) + float64(b.Width)/2) * s.MapConfig.CellSize,
					Y: (float64(b.Y) + float64(b.Height)/2) * s.MapConfig.CellSize,
				}
				goalCenter := s.buildingCenterLocked(th)

				probe.X, probe.Y = spawnCenter.X, spawnCenter.Y
				sub := s.buildUnitPathBlockedLocked(probe, blocked)

				start := s.worldToUnitPathSubGrid(spawnCenter.X, spawnCenter.Y)
				if rs, ok := s.findNearestUnitPathSubWalkable(start, sub); ok {
					start = rs
				}
				goal := s.worldToUnitPathSubGrid(goalCenter.X, goalCenter.Y)
				resolvedGoal, ok := s.findNearestUnitPathSubWalkable(goal, sub)
				if !ok {
					t.Errorf("%s: no walkable sub-cell near townhall %s", entry.ID, th.ID)
					continue
				}

				hitsBefore := tr.unitPathBudgetHits
				path := s.findUnitPath(start, resolvedGoal, sub)
				if len(path) == 0 {
					reason := "no terrain route exists (map walls off the target base)"
					if tr.unitPathBudgetHits > hitsBefore {
						reason = fmt.Sprintf("node budget exhausted (%d expansions) before the route was found — "+
							"raise unitPathExpansionFactor or shorten the detour",
							unitPathExpansionFactor*(func() int { c, r := s.unitPathSubGridDims(); return c + r }()))
					}
					t.Errorf("%s: enemy-spawnpoint %s cannot path to %s's townhall %s: %s",
						entry.ID, b.ID, label, th.ID, reason)
				}
			}
		})
	}
}
