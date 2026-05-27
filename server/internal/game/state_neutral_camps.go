package game

import (
	"log/slog"
	"sort"

	"webrts/server/pkg/protocol"
)

// NeutralCampState distinguishes "spawned and active" from "wave hidden."
// Edge transitions are driven by WaveManager.State transitions in
// tickNeutralCampsLocked (Batch E).
type NeutralCampState int

const (
	NeutralCampActive     NeutralCampState = iota // group is alive at the camp; passive guard mode
	NeutralCampWaveHidden                         // wave is active; no neutrals exist; respawn on next wave clear
)

// NeutralCamp is the runtime state for one map-authored NeutralSpawn.
// All target references are by ID per AI_RULES — AliveUnitIDs is the
// authoritative list of unit IDs spawned by this camp this respawn cycle,
// rebuilt every time the wave clears.
type NeutralCamp struct {
	PlacementID  string
	X, Y         int
	StartingTier int
	TierUpEveryN int
	GroupID      string // specific id or protocol.NeutralSpawnRandomGroupID
	CurrentTier  int    // recomputed each respawn
	AliveUnitIDs []int
	State        NeutralCampState

	AggroRange              float64
	LeashRange              float64
	HealthMultiplier        float64
	HealthMultiplierPerWave float64
	DamageMultiplier        float64
	DamageMultiplierPerWave float64
}

// initNeutralCampsLocked builds NeutralCamp runtime state from
// MapConfig.NeutralSpawns. Called once during game-state initialization
// (alongside initWaveManagerLocked). Idempotent — safe to call twice.
//
// Camps are stored in a sorted slice (by PlacementID) so iteration order
// is deterministic across runs.
//
// Must be called under s.mu write lock.
func (s *GameState) initNeutralCampsLocked() {
	if len(s.MapConfig.NeutralSpawns) == 0 {
		s.NeutralCamps = nil
		return
	}
	camps := make([]NeutralCamp, 0, len(s.MapConfig.NeutralSpawns))
	for _, ns := range s.MapConfig.NeutralSpawns {
		startingTier := ns.StartingTier
		if startingTier < 1 {
			startingTier = 1
		}
		groupID := ns.GroupID
		if groupID == "" {
			groupID = protocol.NeutralSpawnRandomGroupID
		}
		camps = append(camps, NeutralCamp{
			PlacementID:             ns.ID,
			X:                       ns.X,
			Y:                       ns.Y,
			StartingTier:            startingTier,
			TierUpEveryN:            ns.TierUpEveryNWaves,
			GroupID:                 groupID,
			CurrentTier:             startingTier,
			State:                   NeutralCampWaveHidden, // promoted to Active on first tick by Batch E
			AggroRange:              ns.AggroRange,
			LeashRange:              ns.LeashRange,
			HealthMultiplier:        ns.HealthMultiplier,
			HealthMultiplierPerWave: ns.HealthMultiplierPerWave,
			DamageMultiplier:        ns.DamageMultiplier,
			DamageMultiplierPerWave: ns.DamageMultiplierPerWave,
		})
	}
	sort.Slice(camps, func(i, j int) bool { return camps[i].PlacementID < camps[j].PlacementID })
	s.NeutralCamps = camps
}

// tickNeutralCampsLocked is edge-triggered off WaveManager.State. Does no
// per-tick work in the steady state. Lifecycle transitions:
//
//   - Game start / wave clear (camp WaveHidden, wave NOT active) →
//     recompute CurrentTier, then spawnGroupForCampLocked.
//   - Wave starts (camp Active, wave active) → despawnNeutralCampLocked.
//   - Wave active + camp WaveHidden (steady mid-wave) → nothing.
//   - Wave inactive + camp Active (steady between waves) → nothing.
//
// Must be called under s.mu write lock.
func (s *GameState) tickNeutralCampsLocked() {
	if len(s.NeutralCamps) == 0 {
		return
	}
	waveActive := s.WaveManager.Enabled && s.WaveManager.State == "active"

	for i := range s.NeutralCamps {
		camp := &s.NeutralCamps[i]
		switch camp.State {
		case NeutralCampWaveHidden:
			if !waveActive {
				camp.CurrentTier = computeNeutralCurrentTier(s.WaveManager.CurrentWave, camp.StartingTier, camp.TierUpEveryN)
				s.spawnGroupForCampLocked(camp)
			}
		case NeutralCampActive:
			if waveActive {
				s.despawnNeutralCampLocked(camp)
			}
		}
	}
}

// computeNeutralCurrentTier returns startingTier + (completedWaves / tierUpEveryN)
// when auto-scaling is enabled, else startingTier.
//
// completedWaves is WaveManager.CurrentWave (clamped >= 0). CurrentWave
// increments when prep→active fires and stays at N until the next wave
// begins. When State="upgrade" and CurrentWave=N, wave N just cleared and
// N waves are complete. When State="prep" and CurrentWave=0, no waves are
// complete yet. With tierUpEveryN=2: tier promotes at waves 2, 4, 6, …
// cleared.
//
// Pure function — takes primitives so it's trivially testable in isolation.
func computeNeutralCurrentTier(currentWave, startingTier, tierUpEveryN int) int {
	if startingTier < 1 {
		startingTier = 1
	}
	if tierUpEveryN <= 0 {
		return startingTier
	}
	completed := currentWave
	if completed < 0 {
		completed = 0
	}
	return startingTier + completed/tierUpEveryN
}

// despawnNeutralCampLocked removes every alive unit owned by this camp
// from s.Units and clears camp.AliveUnitIDs. Uses the project's canonical
// unit-removal helper so any cross-system cleanup (threat tables,
// projectile aim, etc.) runs.
//
// IDs are snapshotted before iteration because onUnitRemovedFromCampLocked
// (called from removeUnitLocked) mutates camp.AliveUnitIDs in-place.
//
// Must be called under s.mu write lock.
func (s *GameState) despawnNeutralCampLocked(camp *NeutralCamp) {
	toRemove := append([]int(nil), camp.AliveUnitIDs...)
	for _, id := range toRemove {
		u := s.getUnitByIDLocked(id)
		if u == nil {
			continue
		}
		s.removeUnitLocked(id)
	}
	// Clear any IDs that the hook may have missed (e.g. units already gone).
	camp.AliveUnitIDs = camp.AliveUnitIDs[:0]
	camp.State = NeutralCampWaveHidden
}

// onUnitRemovedFromCampLocked strips a unit ID from its owning camp's
// AliveUnitIDs slice. Called from removeUnitLocked when the unit has a
// non-empty NeutralCampID. O(N) over the camp's roster; rosters are
// small (typically <= 8) so this is fine.
//
// Must be called under s.mu write lock.
func (s *GameState) onUnitRemovedFromCampLocked(unitID int, campID string) {
	for i := range s.NeutralCamps {
		camp := &s.NeutralCamps[i]
		if camp.PlacementID != campID {
			continue
		}
		for j, id := range camp.AliveUnitIDs {
			if id == unitID {
				camp.AliveUnitIDs = append(camp.AliveUnitIDs[:j], camp.AliveUnitIDs[j+1:]...)
				return
			}
		}
		return
	}
}

// spawnGroupForCampLocked materializes the camp's current group at the
// camp center. Resolves tier (falling back to the largest available
// tier <= camp.CurrentTier), picks the group (specific id or random
// using s.rngSpawn), then spawns each composition entry as a guard-mode
// unit anchored at the camp center under neutralPlayerID. Appends each
// spawned unit ID to camp.AliveUnitIDs and sets camp.State = NeutralCampActive.
//
// No-op when:
//   - resolveNeutralTier returns 0 (no tier loaded / requested <= 0).
//   - the requested specific group is not found at the resolved tier.
//
// All randomness uses s.rngSpawn for determinism. Composition entries are
// processed in JSON order; per-entry spawns are placed in a small
// deterministic ring around the camp center cell.
//
// Must be called under s.mu write lock.
func (s *GameState) spawnGroupForCampLocked(camp *NeutralCamp) {
	tier := resolveNeutralTier(camp.CurrentTier)
	if tier == 0 {
		return
	}

	var group NeutralGroup
	var ok bool
	if camp.GroupID == protocol.NeutralSpawnRandomGroupID {
		ids := listNeutralGroupIDs(tier)
		if len(ids) == 0 {
			return
		}
		pick := s.rngSpawn.Intn(len(ids))
		group, ok = getNeutralGroup(tier, ids[pick])
	} else {
		group, ok = getNeutralGroup(tier, camp.GroupID)
	}
	if !ok {
		slog.Warn("spawnGroupForCampLocked: group not found at tier; skipping camp",
			"campID", camp.PlacementID, "groupID", camp.GroupID, "tier", tier)
		return
	}

	s.ensureNeutralPlayerLocked()

	cellSize := s.MapConfig.CellSize
	centerWX := float64(camp.X)*cellSize + cellSize/2
	centerWY := float64(camp.Y)*cellSize + cellSize/2
	centerPos := protocol.Vec2{X: centerWX, Y: centerWY}

	wavesElapsed := 0
	if s.WaveManager.Enabled && s.WaveManager.CurrentWave > 1 {
		wavesElapsed = s.WaveManager.CurrentWave - 1
	}
	hpBase := camp.HealthMultiplier
	if hpBase <= 0 {
		hpBase = 1
	}
	dmgBase := camp.DamageMultiplier
	if dmgBase <= 0 {
		dmgBase = 1
	}
	hpMult := hpBase + camp.HealthMultiplierPerWave*float64(wavesElapsed)
	dmgMult := dmgBase + camp.DamageMultiplierPerWave*float64(wavesElapsed)

	aggro := camp.AggroRange
	if aggro < guardMinAggroRange {
		aggro = guardMinAggroRange
	}
	leash := camp.LeashRange
	if leash < aggro {
		leash = aggro
	}

	placedOrderID := s.nextMovementOrderIDLocked()
	blocked := s.getBlockedCellsLocked()
	centerCell := s.worldToGrid(centerWX, centerWY)

	spawnIdx := 0
	for _, entry := range group.Composition {
		for i := 0; i < entry.Count; i++ {
			offsetCell := neutralCampRingOffset(centerCell, spawnIdx)
			spawnCell, found := s.findNearestWalkable(offsetCell, blocked)
			if !found {
				// Fallback to center cell.
				spawnCell, found = s.findNearestWalkable(centerCell, blocked)
				if !found {
					slog.Warn("spawnGroupForCampLocked: no walkable cell found; skipping unit",
						"campID", camp.PlacementID, "unitType", entry.UnitType, "spawnIdx", spawnIdx)
					spawnIdx++
					continue
				}
			}
			spawnPos := s.gridToWorldCenter(spawnCell)
			unit := s.spawnNeutralUnitLocked(entry.UnitType, spawnPos)
			if unit == nil {
				slog.Warn("spawnGroupForCampLocked: spawnNeutralUnitLocked returned nil; skipping",
					"campID", camp.PlacementID, "unitType", entry.UnitType)
				spawnIdx++
				continue
			}
			unit.OrderID = placedOrderID
			unit.GuardMode = true
			unit.GuardAnchorX = centerPos.X
			unit.GuardAnchorY = centerPos.Y
			unit.GuardAggroRange = aggro
			unit.GuardLeashRange = leash
			unit.IgnoreWaveClear = true
			unit.NeutralCampID = camp.PlacementID
			unit.Order = OrderState{Type: OrderHold, HoldX: centerPos.X, HoldY: centerPos.Y}
			unit.CombatAnchorX = centerPos.X
			unit.CombatAnchorY = centerPos.Y
			unit.Status = "Guarding"

			s.applyWaveStatScalingLocked(unit, hpMult, dmgMult)

			camp.AliveUnitIDs = append(camp.AliveUnitIDs, unit.ID)
			spawnIdx++
		}
	}
	camp.State = NeutralCampActive
}

// broadcastNeutralCampAggroLocked propagates an acquired target to all
// camp-mates of acquirer. Each broadcast resolves the target by ID and
// validates against the canonical guard (AI_RULES rule 3) before
// assigning. No *Unit is stored anywhere.
//
// Idempotent: camp-mates already on the same target are skipped. No-op when:
//   - acquirer is nil or has empty NeutralCampID (not a neutral unit)
//   - targetID is the zero value
//   - the target fails the canonical validity guard
//   - the acquirer's camp can't be found (shouldn't happen in normal play)
//
// Must be called under s.mu write lock.
func (s *GameState) broadcastNeutralCampAggroLocked(acquirer *Unit, targetID int) {
	if acquirer == nil || acquirer.NeutralCampID == "" || targetID == 0 {
		return
	}
	// Resolve and validate per AI_RULES rule 3. Target must be alive, visible,
	// and on a different team from the acquirer (neutrals own themselves, so
	// any player-owned unit satisfies the ownership check).
	target := s.getUnitByIDLocked(targetID)
	if target == nil || !target.Visible || target.HP <= 0 || target.OwnerID == acquirer.OwnerID {
		return
	}
	// Find the camp by ID. Linear scan over s.NeutralCamps; N is always small
	// (one entry per map-authored spawn point, typically < 20).
	var camp *NeutralCamp
	for i := range s.NeutralCamps {
		if s.NeutralCamps[i].PlacementID == acquirer.NeutralCampID {
			camp = &s.NeutralCamps[i]
			break
		}
	}
	if camp == nil {
		return
	}
	// Iterate by ID; resolve each mate at point-of-use per AI_RULES rule 2.
	for _, mateID := range camp.AliveUnitIDs {
		if mateID == acquirer.ID {
			continue // skip self
		}
		mate := s.getUnitByIDLocked(mateID)
		if mate == nil || mate.HP <= 0 {
			continue
		}
		if mate.AttackTargetID == targetID {
			continue // already on the same target; idempotent
		}
		mate.AttackTargetID = targetID
	}
}

// neutralCampRingOffset places successive units in a deterministic ring
// around the camp center cell so units in the same camp don't stack.
// Index 0 -> center; 1..8 -> the 8-neighbour ring; 9+ -> wider spiral.
func neutralCampRingOffset(center gridPoint, idx int) gridPoint {
	if idx == 0 {
		return center
	}
	ring1 := [8]gridPoint{
		{X: center.X + 1, Y: center.Y},
		{X: center.X - 1, Y: center.Y},
		{X: center.X, Y: center.Y + 1},
		{X: center.X, Y: center.Y - 1},
		{X: center.X + 1, Y: center.Y + 1},
		{X: center.X - 1, Y: center.Y - 1},
		{X: center.X + 1, Y: center.Y - 1},
		{X: center.X - 1, Y: center.Y + 1},
	}
	if idx-1 < len(ring1) {
		return ring1[idx-1]
	}
	step := (idx - 1) - len(ring1)
	return gridPoint{X: center.X + 2 + step%3, Y: center.Y + step/3}
}
