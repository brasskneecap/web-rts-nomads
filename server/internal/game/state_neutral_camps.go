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
	// SpawnedGroupID is the id of the group rolled at the most recent
	// spawnGroupForCampLocked call. Used at wipe time (Batch 3+) to find
	// the right loot_table even when GroupID is the "__random__" sentinel.
	// Empty before the first spawn.
	SpawnedGroupID string
	CurrentTier    int // recomputed each respawn
	AliveUnitIDs   []int
	State          NeutralCampState
	// LastKillerWasEnemy records whether the most recent camp-unit kill was
	// landed by the __enemy__ wave faction (only possible when EnemiesFightNeutrals
	// is on). When the camp's final unit dies, this gates loot: an enemy-wiped
	// camp drops nothing (see maybeDropChestForCampLocked). Reset on (re)spawn.
	LastKillerWasEnemy bool
	// LastKillerWasPlayer records whether the most recent camp-unit kill was
	// landed by a real player (not the enemy faction, not neutrals, not
	// anonymous damage). When the camp's final unit dies, this gates the
	// kill_camps objective metric: only a player-team wipe counts as "the
	// team killed this camp." Reset on (re)spawn alongside LastKillerWasEnemy.
	LastKillerWasPlayer bool

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

	// Continuous-wave maps keep camps on the field through every wave and reset
	// them (full roster respawn) at each new wave instead of despawning while a
	// wave is active. Handled separately so the discrete lifecycle below is
	// untouched.
	if s.WaveManager.Continuous {
		s.tickNeutralCampsContinuousLocked()
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

// tickNeutralCampsContinuousLocked is the continuous-mode neutral-camp
// lifecycle. Camps spawn as soon as they exist (during the initial prep, before
// wave 1) and persist through active waves — they are NOT despawned when a wave
// is active. At the start of each new wave (CurrentWave advances past
// NeutralResetWave) every still-living camp is reset: wiped via
// despawnNeutralCampLocked (which flips state to WaveHidden first, so the reset
// drops NO loot) and respawned with a fresh full roster at the new tier. A camp
// the players fully cleared mid-wave stays cleared until the next wave reset.
//
// Must be called under s.mu write lock.
func (s *GameState) tickNeutralCampsContinuousLocked() {
	wm := &s.WaveManager
	newWave := wm.CurrentWave > wm.NeutralResetWave
	if newWave {
		wm.NeutralResetWave = wm.CurrentWave
	}
	for i := range s.NeutralCamps {
		camp := &s.NeutralCamps[i]
		// New wave → wipe living camps so the spawn below brings a fresh roster.
		if newWave && camp.State == NeutralCampActive {
			s.despawnNeutralCampLocked(camp)
		}
		if camp.State == NeutralCampWaveHidden {
			camp.CurrentTier = computeNeutralCurrentTier(wm.CurrentWave, camp.StartingTier, camp.TierUpEveryN)
			s.spawnGroupForCampLocked(camp)
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
	// Flip state BEFORE per-unit removals fire the hook. The wipe
	// trigger in onUnitRemovedFromCampLocked is gated on
	// State == NeutralCampActive specifically so wave-start despawn
	// (which also drives the slice to 0) does NOT spawn chests — those
	// units weren't killed by the player.
	camp.State = NeutralCampWaveHidden
	for _, id := range toRemove {
		u := s.getUnitByIDLocked(id)
		if u == nil {
			continue
		}
		s.removeUnitLocked(id)
	}
	// Belt-and-suspenders clear: any IDs the hook may have missed.
	camp.AliveUnitIDs = camp.AliveUnitIDs[:0]
}

// markCampKillerLocked records on the named camp whether the killing blow on
// one of its units was landed by the __enemy__ wave faction. Read by
// maybeDropChestForCampLocked when the camp's final unit dies, so an enemy-wiped
// camp drops no loot. killerOwnerID is empty for anonymous/unresolved kills,
// which counts as "not the enemy" (loot still drops, as before).
//
// Must be called under s.mu write lock.
func (s *GameState) markCampKillerLocked(campID, killerOwnerID string) {
	for i := range s.NeutralCamps {
		if s.NeutralCamps[i].PlacementID == campID {
			s.NeutralCamps[i].LastKillerWasEnemy = killerOwnerID == enemyPlayerID
			s.NeutralCamps[i].LastKillerWasPlayer = killerOwnerID != "" &&
				killerOwnerID != enemyPlayerID && killerOwnerID != neutralPlayerID
			return
		}
	}
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
				// Wipe-trigger: when the player's combat drove this camp from >0
				// units to 0, roll the loot table. The State guard ensures
				// wave-start despawn (which also drives the slice to 0) does NOT
				// fire this — despawnNeutralCampLocked flips State to WaveHidden
				// before invoking removeUnitLocked.
				if len(camp.AliveUnitIDs) == 0 && camp.State == NeutralCampActive {
					s.maybeDropChestForCampLocked(camp)
					// Metrics: credit the camp clear ONLY when the killing blow
					// came from a real player (LastKillerWasPlayer — set by the
					// damage pipeline before removal). A camp wiped by the
					// __enemy__ wave faction or by anonymous damage is nobody's
					// objective progress; crediting it regardless silently
					// completed kill_camps objectives the player never earned.
					// Phase 1 has a single shared team for campaign play, so
					// crediting all non-AI players is equivalent to "the team
					// that landed the killing blow." When PvP campaign ships,
					// this needs to thread the killer's TeamID through.
					if camp.LastKillerWasPlayer {
						s.recordCampClearedMetricLocked(camp.CurrentTier)
					}
				}
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

	camp.SpawnedGroupID = group.ID
	// Fresh roster — clear any stale killer markers from the prior cycle.
	camp.LastKillerWasEnemy = false
	camp.LastKillerWasPlayer = false

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

	// An authored AggroRange overrides the guard floor entirely — neutral camps
	// honor whatever value the map sets. Only fall back to the default when unset.
	aggro := camp.AggroRange
	if aggro <= 0 {
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
			continue // skip self; the acquirer already has its own threat/target
		}
		mate := s.getUnitByIDLocked(mateID)
		if mate == nil || mate.HP <= 0 {
			continue
		}
		// Confer threat on the attacker so the mate gets the same retaliation
		// leash-bypass the directly-hit guards get (shouldDropCurrentTargetLocked).
		// Without this, a mate whose anchor->attacker distance exceeds its
		// GuardLeashRange immediately drops the broadcast target and stays at its
		// post — the "furthest-back guard ignores the fight" case. Applied (and
		// refreshed) before the idempotent target-set so a sustained attack keeps
		// the whole camp committed even after every mate already holds the target.
		s.addThreatLocked(mate, target, neutralCampLinkThreat, true)
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

// neutralCampSnapshotsLocked returns the per-tick wire view of every camp.
// Sent unfiltered (no FOW gating) so the minimap can always render the POI
// dot. Iteration order is the camp slice's deterministic order
// (sorted by PlacementID via initNeutralCampsLocked).
//
// Must be called under s.mu read lock.
func (s *GameState) neutralCampSnapshotsLocked() []protocol.NeutralCampSnapshot {
	if len(s.NeutralCamps) == 0 {
		return nil
	}
	out := make([]protocol.NeutralCampSnapshot, len(s.NeutralCamps))
	for i := range s.NeutralCamps {
		camp := &s.NeutralCamps[i]
		out[i] = protocol.NeutralCampSnapshot{
			ID:             camp.PlacementID,
			X:              camp.X,
			Y:              camp.Y,
			CurrentTier:    camp.CurrentTier,
			AliveUnitCount: len(camp.AliveUnitIDs),
		}
	}
	return out
}
