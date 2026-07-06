package game

import (
	"math"
	"sort"
	"webrts/server/pkg/protocol"
)

type EnemySpawnTimer struct {
	RemainingDelay    float64
	TotalDelay        float64
	RemainingInterval float64
	TotalInterval     float64
	SpawnOnce         bool
	HasFired          bool
}

// WaveManager drives the prep → active → prep cycle for wave-based maps.
// It is only enabled when at least one enemy-spawnpoint has "waveNumber" > 0
// in its metadata. Maps without wave numbers use the legacy always-on behaviour.
//
// Tuning:
//
//	wavePrepDuration  — seconds of prep between waves (default 60)
//	waveActiveDuration — max seconds a wave stays active (default 120; 0 = never time out)
type WaveManager struct {
	Enabled     bool
	CurrentWave int
	TotalWaves  int    // derived from max waveNumber across all spawnpoints (0 = infinite)
	State       string // "prep" | "active" | "complete"
	// Timer meaning differs by state:
	//   "prep"   → seconds remaining until wave starts
	//   "active" → seconds elapsed since wave started
	Timer float64
	// InitialPrepDuration is the prep countdown before wave 1. Falls back to
	// PrepDuration when the map does not configure it separately.
	InitialPrepDuration float64
	// PrepDuration is the prep countdown between subsequent waves.
	PrepDuration float64
	WaveDuration float64 // 0 means no automatic timeout; wave must be ended externally
	// Continuous, when true, runs the map in continuous-wave mode: the active
	// phase advances to the next wave on the WaveDuration timer instead of
	// waiting for the field to clear (see tickWaveLocked). Enemies persist and
	// accumulate across waves; neutral camps reset each wave instead of
	// despawning. Derived from MapConfig.WaveConfig.ContinuousWaves.
	Continuous bool
	// NeutralResetWave is the CurrentWave value the neutral camps were last reset
	// for in continuous mode. tickNeutralCampsLocked resets all camps when this
	// lags CurrentWave, giving "camps reset at the start of each new wave."
	NeutralResetWave int
	// SpawnedThisWave counts wave-gated enemy units (enemy faction, not
	// ignoreWaveClear) spawned since the current wave activated. Reset on
	// every wave activation (prep→active and the continuous upgrade→active
	// rollover). Guards the clear-the-field transition: an empty field does
	// NOT complete a wave whose spawns (commonly delayed) haven't fired yet.
	SpawnedThisWave int
}

const enemyPlayerID = "__enemy__"

const enemyPlayerColor = "#e74c3c"

// neutralPlayerID is the virtual player slot for neutral camp units.
// Neutrals are hostile to player units (distinct OwnerID → existing AI
// scoring treats them as valid targets in both directions) and have no
// base/resources/defeat condition. Neutrals only exist outside "active"
// wave state; the lifecycle is owned by state_neutral_camps.go.
const neutralPlayerID = "__neutral__"

const neutralPlayerColor = "#9b59b6"

// -------------------------------------------------------------------------

// initWaveManagerLocked scans all enemy-spawnpoint buildings for "waveNumber",
// "startingWave", or "waveInterval" metadata. If any are present the wave
// system is enabled and the manager is initialised in the "prep" phase for
// wave 1. Only "waveNumber" contributes to TotalWaves — "startingWave" and
// "waveInterval" are open-ended (no implied cap).
func (s *GameState) initWaveManagerLocked() {
	hasWavePoints := false
	maxWave := 0
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.BuildingType != "enemy-spawnpoint" {
			continue
		}
		if wn, ok := getMetadataFloat(b.Metadata, "waveNumber"); ok && int(wn) > 0 {
			hasWavePoints = true
			if int(wn) > maxWave {
				maxWave = int(wn)
			}
		}
		if _, ok := getMetadataFloat(b.Metadata, "startingWave"); ok {
			hasWavePoints = true
		}
		if iv, ok := getMetadataFloat(b.Metadata, "waveInterval"); ok && int(iv) > 0 {
			hasWavePoints = true
		}
	}

	if !hasWavePoints {
		// No wave-controlled spawn points — use legacy always-on mode.
		s.WaveManager = WaveManager{}
		return
	}

	prepDuration := wavePrepDuration
	initialPrep := 0.0 // 0 ⇒ fall back to prepDuration below
	waveDuration := waveActiveDuration
	totalWaves := maxWave
	continuous := false

	if cfg := s.MapConfig.WaveConfig; cfg != nil {
		if cfg.PrepDuration > 0 {
			prepDuration = cfg.PrepDuration
		}
		if cfg.InitialPrepDuration > 0 {
			initialPrep = cfg.InitialPrepDuration
		}
		if cfg.WaveDuration > 0 {
			waveDuration = cfg.WaveDuration
		}
		if cfg.TotalWaves > 0 {
			totalWaves = cfg.TotalWaves
		}
		continuous = cfg.ContinuousWaves
	}

	// Unconfigured initial prep uses the between-wave value, preserving the
	// legacy single-timer behaviour.
	if initialPrep <= 0 {
		initialPrep = prepDuration
	}

	s.WaveManager = WaveManager{
		Enabled:             true,
		CurrentWave:         0, // 0 means "prep before wave 1"
		TotalWaves:          totalWaves,
		State:               "prep",
		Timer:               initialPrep,
		InitialPrepDuration: initialPrep,
		PrepDuration:        prepDuration,
		WaveDuration:        waveDuration,
		Continuous:          continuous,
	}
}

// tickWaveLocked advances the wave state machine each server tick.
func (s *GameState) tickWaveLocked(dt float64) {
	wm := &s.WaveManager
	if !wm.Enabled {
		return
	}

	switch wm.State {
	case "prep":
		wm.Timer -= dt
		if wm.Timer <= 0 {
			// Advance to the next wave's active phase.
			wm.CurrentWave++
			wm.State = "active"
			wm.Timer = 0
			wm.SpawnedThisWave = 0
			// Per-wave reset of pathing diagnostics + building strike count so the
			// debug snapshot reflects only the current wave's behaviour and the
			// escalation system doesn't carry forward stale memos.
			for _, u := range s.Units {
				if u == nil {
					continue
				}
				u.PathDiagnostics = PathDiagnostics{}
				u.UnreachableBuildingStrikeCount = 0
			}
			// Reset spawn timers so this wave's points re-arm from the wave start.
			s.resetWaveSpawnTimersLocked(wm.CurrentWave)
		}

	case "active":
		wm.Timer += dt
		// Cap the timer at WaveDuration so tickEnemySpawnpointsLocked sees the
		// spawn phase as closed once it expires. The wave itself ends as soon as
		// all enemies are dead — we don't require the timer to have expired first.
		if wm.WaveDuration > 0 && wm.Timer >= wm.WaveDuration {
			wm.Timer = wm.WaveDuration
		}

		// isFinalWave: the last wave of a bounded run. Both discrete maps and a
		// bounded continuous map's final wave end by clearing the field.
		isFinalWave := wm.TotalWaves > 0 && wm.CurrentWave >= wm.TotalWaves

		// Continuous mode, non-final wave: advance to the NEXT wave purely on the
		// WaveDuration countdown — the field is NOT cleared first, so enemies
		// persist and accumulate. Crediting the wave-clear metric here is what
		// lets a survive_waves objective progress (and end the run after its
		// target wave). The upgrade pick is presented at each new wave.
		if wm.Continuous && !isFinalWave {
			if wm.WaveDuration > 0 && wm.Timer >= wm.WaveDuration {
				s.recordWaveClearedMetricLocked()
				wm.State = "upgrade"
				s.enterWaveUpgradePhaseLocked()
			}
			return
		}

		// Discrete maps (and a continuous map's final wave): clear the field to
		// progress. Allow clear once the wave has been active for at least 5
		// seconds so spawners with 0-delay have had a chance to place enemies on
		// the field. After that, transition as soon as all enemies are dead —
		// but ONLY once this wave's spawns have actually fired. Spawn delays
		// commonly exceed 5s, and without the guard an empty field completed
		// the wave before a single one of its enemies existed (in the field:
		// forest-1's final wave "cleared" six seconds after activating). A
		// wave whose spawnpoints never fire (misconfigured map) still
		// terminates once the spawn window closes.
		const minActiveSeconds = 5.0
		spawnWindowClosed := wm.WaveDuration > 0 && wm.Timer >= wm.WaveDuration
		canClear := wm.SpawnedThisWave > 0 || spawnWindowClosed || wm.WaveDuration <= 0
		if wm.Timer >= minActiveSeconds && canClear && s.countEnemyUnitsLocked() == 0 {
			// Metrics: credit every human player with a wave clear before the
			// state actually transitions away from "active." Mirrors the camp
			// clear hook: in single-team campaign play this is equivalent to
			// "the team that survived the wave."
			s.recordWaveClearedMetricLocked()
			if isFinalWave {
				wm.State = "complete"
				// Legacy markWaveObjectivesCompleteLocked() call removed in §9
				// of campaign-objectives-and-metrics. The wave-complete state
				// alone is now consumed by the new victory rule in
				// checkVictoryLocked, gated AND with allRequiredObjectivesCompleted.
			} else {
				wm.State = "upgrade"
				s.enterWaveUpgradePhaseLocked()
			}
		}

	case "upgrade":
		s.tickUpgradePhaseLocked()

		// "complete" is terminal — nothing more to tick.
	}
}

// countEnemyUnitsLocked returns the number of living enemy units on the field
// that gate wave progression. Units spawned from spawnpoints flagged with
// metadata["ignoreWaveClear"] are skipped so ambient/background enemies do
// not stall the active → prep transition.
//
// Note: server-side Visible is NOT checked here. Enemy units are always
// Visible=true on the server; the client's fog-of-war only controls rendering
// on the client side. Filtering by Visible would leave units outside the
// player's vision range alive but undetectable, stalling the wave clear.
// MiningInside is excluded because a unit inside a building is not on the
// active field.
func (s *GameState) countEnemyUnitsLocked() int {
	count := 0
	for _, u := range s.Units {
		if u.OwnerID == enemyPlayerID && u.HP > 0 && !u.MiningInside && !u.IgnoreWaveClear {
			count++
		}
	}
	return count
}

// resetWaveSpawnTimersLocked removes the cached EnemySpawnTimer entries for
// all spawnpoints that belong to the given wave. They will be re-created with
// fresh timers the next time tickEnemySpawnpointsLocked processes them.
func (s *GameState) resetWaveSpawnTimersLocked(waveNumber int) {
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.BuildingType != "enemy-spawnpoint" {
			continue
		}
		// Reset specific-wave spawners assigned to this wave.
		if wn, ok := getMetadataFloat(b.Metadata, "waveNumber"); ok && int(wn) == waveNumber {
			delete(s.EnemySpawnTimers, b.ID)
			continue
		}
		// Reset repeating spawners that are active at this wave number.
		if sw, ok := getMetadataFloat(b.Metadata, "startingWave"); ok && waveNumber >= int(sw) {
			delete(s.EnemySpawnTimers, b.ID)
			continue
		}
		// Reset interval spawners on waves that are multiples of waveInterval
		// (active waves only — waveNumber == 0 is the pre-wave-1 prep state).
		if iv, ok := getMetadataFloat(b.Metadata, "waveInterval"); ok && int(iv) > 0 && waveNumber > 0 && waveNumber%int(iv) == 0 {
			delete(s.EnemySpawnTimers, b.ID)
		}
	}
}

func (s *GameState) ensureEnemyPlayerLocked() {
	if _, exists := s.Players[enemyPlayerID]; exists {
		return
	}
	s.Players[enemyPlayerID] = &Player{
		ID:                            enemyPlayerID,
		Color:                         enemyPlayerColor,
		Resources:                     map[string]int{},
		GlobalUnitSpawnTimeMultiplier: 1,
		UnitSpawnTimeMultipliers:      map[string]float64{},
		Metrics:                       NewMatchMetrics(),
	}
}

func (s *GameState) ensureNeutralPlayerLocked() {
	if _, exists := s.Players[neutralPlayerID]; exists {
		return
	}
	s.Players[neutralPlayerID] = &Player{
		ID:                            neutralPlayerID,
		Color:                         neutralPlayerColor,
		Resources:                     map[string]int{},
		GlobalUnitSpawnTimeMultiplier: 1,
		UnitSpawnTimeMultipliers:      map[string]float64{},
		Metrics:                       NewMatchMetrics(),
	}
}

// seedEnemyObjectiveAtSpawnLocked sets a routed enemy's sticky
// ObjectiveBuildingID at spawn so it does not have to lazily re-acquire on its
// first no-target eval. Mirrors the spawn routing rules: __none__ and static-
// objective (ObjectiveID already set) units get no objective; targetPlayerLabel
// units prefer that player's townhall; the default routes to the nearest
// player townhall. spawnPos is the unit's spawn position (origin for the
// nearest-townhall search).
func (s *GameState) seedEnemyObjectiveAtSpawnLocked(unit *Unit, targetPlayerLabel string, spawnPos protocol.Vec2) {
	if unit == nil || unit.ObjectiveID != "" || targetPlayerLabel == "__none__" {
		return
	}
	var b *protocol.BuildingTile
	if targetPlayerLabel != "" && unit.TargetPlayerID != "" {
		b = s.findNearestAttackableBuildingForPlayerLocked(unit, unit.TargetPlayerID)
	}
	if b == nil {
		b = s.getNearestPlayerTownhallBuildingLocked(spawnPos.X, spawnPos.Y)
	}
	if b != nil {
		// The seeded ObjectiveBuildingID may differ from the initial spawn-path
		// destination (seed picks the nearest attackable building for TargetPlayerID
		// while the spawn path goes to a townhall center). This is intentional:
		// enemyAdvanceToObjectiveLocked re-validates ObjectiveBuildingID every
		// no-target eval, so the sticky objective self-corrects within one tick.
		unit.ObjectiveBuildingID = b.ID
	}
}

// enemySpawnPathDestinationLocked decides where a freshly spawned advancing
// enemy should path. Precedence:
//  1. captureDest — where a capture-defense spawn must go to stop the capture
//     (the claim tower for claim zones, the capturing units for presence zones);
//  2. the target player's NEAREST building (e.g. a forward tower) when a target
//     player is set, instead of beelining their townhall;
//  3. the nearest player townhall.
//
// Returns nil only when there is nothing to advance on. Pure decision (assigns
// no path) so spawn routing stays unit-testable; mirrors the sticky objective
// seeded by seedEnemyObjectiveAtSpawnLocked so the movement target and the
// objective agree.
func (s *GameState) enemySpawnPathDestinationLocked(unit *Unit, targetPlayerID string, spawnPos protocol.Vec2, captureDest *protocol.Vec2) *protocol.Vec2 {
	if captureDest != nil {
		return captureDest
	}
	if targetPlayerID != "" {
		if b := s.findNearestAttackableBuildingForPlayerLocked(unit, targetPlayerID); b != nil {
			c := s.buildingCenterLocked(b)
			return &c
		}
		if thc := s.getPlayerTownhallCenterLocked(targetPlayerID); thc != nil {
			return thc
		}
	}
	return s.getNearestPlayerTownhallCenterLocked(spawnPos.X, spawnPos.Y)
}

func (s *GameState) tickEnemySpawnpointsLocked(dt float64, blocked map[gridPoint]bool) {
	for i := range s.MapConfig.Buildings {
		building := &s.MapConfig.Buildings[i]
		if building.BuildingType != "enemy-spawnpoint" {
			continue
		}

		// gameStart spawnpoints fire exactly once at match start, bypassing wave gating.
		isGameStart := getMetadataBool(building.Metadata, "gameStart")
		triggerZoneID, hasTrigger := getMetadataString(building.Metadata, "triggerCaptureZoneId")
		hasTrigger = hasTrigger && triggerZoneID != ""
		// Capture-defense routing. For a capture-trigger spawnpoint that is
		// actively firing, the units should try to STOP the capture:
		//   - claim zone  → destroy the tower on the slot (captureTower);
		//   - presence zone → attack the units holding the zone (captureDest).
		// captureTower drives the sticky building objective; captureDest is the
		// move target either way.
		var captureTower *protocol.BuildingTile
		var captureDest *protocol.Vec2
		if hasTrigger {
			// Capture-zone-triggered spawnpoint ("While Zone Being Captured"
			// spawn-timing mode): bypass wave gating and spawn only while the
			// linked zone's capture progress is actively advancing. Stops the
			// instant the zone is captured, contested, or abandoned. Re-arm the
			// interval while dormant so re-activation doesn't fire instantly.
			if !s.zoneCapturingLocked(triggerZoneID) {
				if t := s.EnemySpawnTimers[building.ID]; t != nil {
					t.RemainingInterval = t.TotalInterval
				}
				continue
			}
			if captureTower = s.claimZoneTowerLocked(triggerZoneID); captureTower != nil {
				c := s.buildingCenterLocked(captureTower)
				captureDest = &c
			} else if rt := s.zoneRuntimeByIDLocked(triggerZoneID); rt != nil {
				// No structure (presence): aim at the units capturing the zone,
				// measured from the spawnpoint.
				bx := (float64(building.X) + float64(building.Width)/2) * s.MapConfig.CellSize
				by := (float64(building.Y) + float64(building.Height)/2) * s.MapConfig.CellSize
				captureDest = s.nearestCapturingUnitPosLocked(rt, bx, by)
			}
		} else if !isGameStart {
			// Wave-gating: when wave mode is enabled, check waveNumber (specific wave)
			// and startingWave (every wave from N onwards). Points with neither field
			// (or waveNumber == 0) are legacy points that always fire regardless.
			if s.WaveManager.Enabled {
				wm := &s.WaveManager
				waveTimerExpired := wm.WaveDuration > 0 && wm.Timer >= wm.WaveDuration

				if iv, hasIV := getMetadataFloat(building.Metadata, "waveInterval"); hasIV && int(iv) > 0 {
					// Interval spawn: active on every wave that is a multiple of N
					// (waves N, 2N, 3N…) while the active timer is running.
					if wm.State != "active" || wm.CurrentWave <= 0 || wm.CurrentWave%int(iv) != 0 || waveTimerExpired {
						continue
					}
				} else if sw, hasSW := getMetadataFloat(building.Metadata, "startingWave"); hasSW && int(sw) > 0 {
					// Repeating spawn: active every wave >= startingWave while timer is running.
					if wm.State != "active" || wm.CurrentWave < int(sw) || waveTimerExpired {
						continue
					}
				} else if wn, hasWN := getMetadataFloat(building.Metadata, "waveNumber"); hasWN && int(wn) > 0 {
					// Single-wave spawn: active only during its assigned wave.
					if wm.State != "active" || int(wn) != wm.CurrentWave || waveTimerExpired {
						continue
					}
				} else {
					// No wave tag — when wave mode is on, hold until any wave is active.
					if wm.State != "active" || waveTimerExpired {
						continue
					}
				}
			}
		}

		s.ensureEnemyPlayerLocked()

		// Inactive spawnpoint check: if targetPlayerLabel names a real label (not
		// "__none__") but no matching player has ever joined, skip this spawnpoint.
		//
		// Only "never joined" suppresses the spawnpoint. Once the label's player
		// has joined (recorded in joinedTargetLabels at EnsurePlayer time), losing
		// their townhall must NOT re-dormant the spawnpoint: the townhall is
		// removed from the map on destruction, so findPlayerIDByLabelLocked can no
		// longer resolve the label — but with any player base still standing the
		// wave should keep coming. It fires and re-routes to the nearest surviving
		// base (enemySpawnPathDestinationLocked / seedEnemyObjectiveAtSpawnLocked
		// fall back to nearest when TargetPlayerID resolves empty).
		if tpl, ok := getMetadataString(building.Metadata, "targetPlayerLabel"); ok && tpl != "" && tpl != "__none__" {
			if s.findPlayerIDByLabelLocked(tpl) == "" && !s.joinedTargetLabels[tpl] {
				continue
			}
		}

		timer, exists := s.EnemySpawnTimers[building.ID]
		if !exists {
			delay := 60.0
			interval := 10.0
			if isGameStart {
				delay = 0
			} else if building.Metadata != nil {
				if v, ok := getMetadataFloat(building.Metadata, "spawnDelaySeconds"); ok && v >= 0 {
					delay = v
				}
				if v, ok := getMetadataFloat(building.Metadata, "spawnIntervalSeconds"); ok && v > 0 {
					interval = v
				}
			}
			spawnOnce := isGameStart || getMetadataBool(building.Metadata, "spawnOnce")
			timer = &EnemySpawnTimer{
				RemainingDelay:    delay,
				TotalDelay:        delay,
				RemainingInterval: 0,
				TotalInterval:     interval,
				SpawnOnce:         spawnOnce,
			}
			s.EnemySpawnTimers[building.ID] = timer
		}

		if timer.SpawnOnce && timer.HasFired {
			continue
		}

		if timer.RemainingDelay > 0 {
			timer.RemainingDelay = math.Max(0, timer.RemainingDelay-dt)
			continue
		}

		timer.RemainingInterval -= dt
		if timer.RemainingInterval > 0 {
			continue
		}
		timer.RemainingInterval += timer.TotalInterval
		orderID := s.nextMovementOrderIDLocked()

		spawnCount := 1
		unitType := "raider"
		objectiveId := ""
		targetPlayerLabel := ""
		// spawnAlliance selects the faction of the spawned units: "neutral"
		// makes them share the neutral camps' owner (so they don't fight the
		// camps), anything else (default/absent) keeps the legacy enemy faction.
		spawnNeutral := false
		if v, ok := getMetadataString(building.Metadata, "spawnAlliance"); ok && v == "neutral" {
			spawnNeutral = true
		}
		ignoreWaveClear := getMetadataBool(building.Metadata, "ignoreWaveClear")
		if building.Metadata != nil {
			if v, ok := getMetadataFloat(building.Metadata, "spawnCount"); ok && v >= 1 {
				spawnCount = int(v)
			}
			if v, ok := building.Metadata["unitType"].(string); ok && v != "" {
				unitType = v
			}
			if v, ok := building.Metadata["objectiveId"].(string); ok && v != "" {
				objectiveId = v
			}
			if v, ok := getMetadataString(building.Metadata, "targetPlayerLabel"); ok {
				targetPlayerLabel = v
			}
		}

		hpMult, dmgMult := s.computeWaveStatScalingLocked(building)

		stopSpawnPos := profileStart("enemySpawn.positions")
		spawnPositions := s.getTownhallSpawnPositionsLocked(*building, spawnCount, blocked)
		stopSpawnPos()

		center := protocol.Vec2{
			X: (float64(building.X) + float64(building.Width)/2) * s.MapConfig.CellSize,
			Y: (float64(building.Y) + float64(building.Height)/2) * s.MapConfig.CellSize,
		}

		// Two-phase spawn: configure every unit first, collecting the ones
		// that need a route, THEN path them as a batch sharing one
		// unit-obstacle blocked map. Every spawned unit carries the same
		// orderID, so buildUnitPathBlockedLocked excludes the identical set
		// for all of them — the per-plane sub-cell map is the same map.
		// Building it once instead of once-per-unit (a fresh large map alloc +
		// full unit/terrain scan each time) is what removes the per-spawn
		// pathing spike. Mirrors the player MoveUnits batch path.
		type spawnPathReq struct {
			unit   *Unit
			target protocol.Vec2
		}
		spawnedUnits := make([]*Unit, 0, spawnCount)
		pathReqs := make([]spawnPathReq, 0, spawnCount)

		for i := 0; i < spawnCount; i++ {
			var spawnPos protocol.Vec2
			if i < len(spawnPositions) {
				spawnPos = spawnPositions[i]
			} else {
				// Overflow beyond the perimeter slots: constrain to the
				// spawnpoint cell's own region so the extra units can't be
				// dropped into a sealed pocket across a tree line.
				centerCell := s.worldToGrid(center.X, center.Y)
				cell, ok := s.findNearestWalkableInRegionLocked(centerCell, s.walkableRegionAtLocked(centerCell), blocked, nil)
				if !ok {
					break
				}
				spawnPos = s.gridToWorldCenter(cell)
			}

			var unit *Unit
			if spawnNeutral {
				unit = s.spawnNeutralUnitLocked(unitType, spawnPos)
			} else {
				unit = s.spawnEnemyUnitLocked(unitType, spawnPos)
			}
			if unit == nil {
				continue
			}
			s.applyWaveStatScalingLocked(unit, hpMult, dmgMult)
			unit.OrderID = orderID
			unit.IgnoreWaveClear = ignoreWaveClear
			spawnedUnits = append(spawnedUnits, unit)
			if objectiveId != "" {
				// Existing objective mechanic: keep unit stationary at an objective.
				unit.ObjectiveID = objectiveId
				unit.Status = "Idle"
			} else if targetPlayerLabel == "__none__" {
				// Explicit stay-at-spawn: no path assigned.
				unit.Status = "Idle"
			} else {
				// Advancing. Resolve the target player (if any) and persist it so
				// the AI's "no target → nearest building" fallback in
				// enemyAdvanceToObjectiveLocked keeps preferring this player even
				// after the unit re-evaluates mid-flight.
				unit.Status = "Advancing"
				playerID := ""
				if targetPlayerLabel != "" {
					if playerID = s.findPlayerIDByLabelLocked(targetPlayerLabel); playerID != "" {
						unit.TargetPlayerID = playerID
					}
				}
				// Capture defenders go stop the capture (claim tower or the
				// capturing units); otherwise head for the target player's nearest
				// building, else the nearest townhall.
				if captureTower != nil {
					unit.ObjectiveBuildingID = captureTower.ID
				}
				if dest := s.enemySpawnPathDestinationLocked(unit, playerID, spawnPos, captureDest); dest != nil {
					pathReqs = append(pathReqs, spawnPathReq{unit: unit, target: *dest})
				}
			}
			// Seed the sticky objective for non-capture units (capture defenders
			// already point at the tower / capturers above).
			if captureDest == nil {
				s.seedEnemyObjectiveAtSpawnLocked(unit, targetPlayerLabel, spawnPos)
			}
		}

		// Wave-clear guard bookkeeping: count enemy-faction spawns that gate
		// the clear-the-field transition (neutral-alliance and ignoreWaveClear
		// units are excluded from countEnemyUnitsLocked, so they don't count
		// here either). See WaveManager.SpawnedThisWave.
		if !spawnNeutral && !ignoreWaveClear {
			s.WaveManager.SpawnedThisWave += len(spawnedUnits)
		}

		// Drop path requests whose objective is cached unreachable army-wide.
		// seedEnemyObjectiveAtSpawnLocked set ObjectiveBuildingID above, so we
		// can consult the same s.objectiveUnreachableUntil cache that
		// enemyAdvanceToObjectiveLocked uses. A skipped unit isn't stranded:
		// its first no-target eval runs enemyAdvanceToObjectiveLocked which,
		// seeing the same cache, goes straight to engaging the blockers. This
		// removes the per-wave N×budgeted-A* spawn spike when the base is
		// walled off, without changing behavior when it is reachable (no cache
		// entry → every request still paths).
		pending := pathReqs[:0]
		for _, r := range pathReqs {
			if r.unit.ObjectiveBuildingID != "" &&
				s.Tick < s.objectiveUnreachableUntil[r.unit.ObjectiveBuildingID] {
				continue
			}
			pending = append(pending, r)
		}

		if len(pending) > 0 {
			groundSub, flyerSub := s.buildGroupSubBlockedLocked(spawnedUnits, blocked)
			subFor := func(u *Unit) map[gridPoint]bool {
				if u != nil && u.Flyer {
					return flyerSub
				}
				return groundSub
			}
			profileSection("enemySpawn.unitPath", func() {
				for _, r := range pending {
					s.assignUnitPathWithSubBlocked(r.unit, r.target, blocked, subFor(r.unit), nil)
				}
			})
		}

		if timer.SpawnOnce {
			timer.HasFired = true
		}
	}
}

func (s *GameState) findNearestAttackablePlayerBuildingLocked(enemy *Unit) *protocol.BuildingTile {
	var best *protocol.BuildingTile
	bestDistSq := math.MaxFloat64

	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.OwnerID == nil || *b.OwnerID == enemyPlayerID {
			continue
		}
		// Pending-start ghosts are not yet attackable — don't route waves onto them.
		if buildingPendingStart(b) {
			continue
		}
		hp, _, ok := getBuildingHP(b)
		if !ok || hp <= 0 {
			continue
		}
		if b.ID == enemy.UnreachableBuildingTargetID && s.Tick < enemy.UnreachableUntilTick {
			continue
		}
		dist := s.distanceToBuilding(enemy.X, enemy.Y, b)
		if dist < bestDistSq || (dist == bestDistSq && (best == nil || b.ID < best.ID)) {
			bestDistSq = dist
			best = b
		}
	}

	return best
}

// findNearestAttackableBuildingForPlayerLocked is the player-filtered variant
// of findNearestAttackablePlayerBuildingLocked. Returns the nearest live,
// owner-matched building for the given player, or nil if that player has none.
// Used by the AI's no-target fallback to honor an enemy's TargetPlayerID
// instead of unconditionally picking the geographically nearest player base.
func (s *GameState) findNearestAttackableBuildingForPlayerLocked(enemy *Unit, playerID string) *protocol.BuildingTile {
	if playerID == "" {
		return nil
	}
	var best *protocol.BuildingTile
	bestDistSq := math.MaxFloat64
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.OwnerID == nil || *b.OwnerID != playerID {
			continue
		}
		// Pending-start ghosts are not yet attackable — don't route waves onto them.
		if buildingPendingStart(b) {
			continue
		}
		hp, _, ok := getBuildingHP(b)
		if !ok || hp <= 0 {
			continue
		}
		if b.ID == enemy.UnreachableBuildingTargetID && s.Tick < enemy.UnreachableUntilTick {
			continue
		}
		dist := s.distanceToBuilding(enemy.X, enemy.Y, b)
		if dist < bestDistSq || (dist == bestDistSq && (best == nil || b.ID < best.ID)) {
			bestDistSq = dist
			best = b
		}
	}
	return best
}

// findBestBuildingAttackPositionLocked returns the walkable perimeter cell
// closest to the enemy that is not already claimed by another enemy targeting
// the same building. Falls back to the closest cell if all are claimed.
func (s *GameState) findBestBuildingAttackPositionLocked(enemy *Unit, building *protocol.BuildingTile, blocked map[gridPoint]bool) *protocol.Vec2 {
	candidates := make([]gridPoint, 0, (building.Width+2)*(building.Height+2))
	seen := make(map[gridPoint]bool)

	for y := building.Y - 1; y <= building.Y+building.Height; y++ {
		for x := building.X - 1; x <= building.X+building.Width; x++ {
			isPerimeter := x == building.X-1 || x == building.X+building.Width || y == building.Y-1 || y == building.Y+building.Height
			if !isPerimeter {
				continue
			}
			cell := gridPoint{X: x, Y: y}
			if seen[cell] || !s.isWalkable(cell, blocked) {
				continue
			}
			seen[cell] = true
			candidates = append(candidates, cell)
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	// Mark perimeter cells already claimed by other enemies targeting this building.
	claimed := make(map[gridPoint]bool)
	for _, u := range s.Units {
		if u == enemy || u.AttackBuildingTargetID != building.ID {
			continue
		}
		tx, ty := u.TargetX, u.TargetY
		if u.Attacking {
			tx, ty = u.X, u.Y
		}
		claimed[s.worldToGrid(tx, ty)] = true
	}

	sort.Slice(candidates, func(i, j int) bool {
		a := s.gridToWorldCenter(candidates[i])
		b := s.gridToWorldCenter(candidates[j])
		return distanceSquared(a.X, a.Y, enemy.X, enemy.Y) < distanceSquared(b.X, b.Y, enemy.X, enemy.Y)
	})

	for _, cell := range candidates {
		if !claimed[cell] {
			pos := s.gridToWorldCenter(cell)
			return &pos
		}
	}

	// All cells claimed – still pick the closest so the unit keeps moving.
	pos := s.gridToWorldCenter(candidates[0])
	return &pos
}

// findPlayerIDByLabelLocked returns the player ID that occupies the townhall
// linked to the first spawn-point whose "playerLabel" metadata matches label.
// Returns "" if no such spawn-point exists or the linked townhall has no owner
// (i.e. the player never joined).
func (s *GameState) findPlayerIDByLabelLocked(label string) string {
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.BuildingType != "spawn-point" {
			continue
		}
		pl, ok := getMetadataString(b.Metadata, "playerLabel")
		if !ok || pl != label {
			continue
		}
		// Ownership is tracked on the townhall, not the spawn-point.
		// Resolve the linked townhall and read its OwnerID.
		townhall := s.resolveSpawnPointTownhallLocked(*b, true)
		if townhall == nil || townhall.OwnerID == nil {
			return ""
		}
		return *townhall.OwnerID
	}
	return ""
}

// getPlayerTownhallCenterLocked returns the center of the given player's live
// townhall building (Occupied == true, hp > 0), or nil if they have none.
func (s *GameState) getPlayerTownhallCenterLocked(playerID string) *protocol.Vec2 {
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.BuildingType != "townhall" {
			continue
		}
		if !b.Occupied || b.OwnerID == nil || *b.OwnerID != playerID {
			continue
		}
		hp, _, ok := getBuildingHP(b)
		if !ok || hp <= 0 {
			continue
		}
		cx := (float64(b.X) + float64(b.Width)/2) * s.MapConfig.CellSize
		cy := (float64(b.Y) + float64(b.Height)/2) * s.MapConfig.CellSize
		pos := protocol.Vec2{X: cx, Y: cy}
		return &pos
	}
	return nil
}

// computeWaveStatScalingLocked returns (hpMultiplier, damageMultiplier) for a
// spawnpoint at the current global wave. Each multiplier is
// `base + perWave * max(0, CurrentWave - 1)` so wave 1 yields the configured
// base. When wave mode is disabled, only the base multipliers apply.
// Defaults: base=1.0, perWave=0.
func (s *GameState) computeWaveStatScalingLocked(building *protocol.BuildingTile) (float64, float64) {
	hpBase, hpPerWave := 1.0, 0.0
	dmgBase, dmgPerWave := 1.0, 0.0
	if building.Metadata != nil {
		if v, ok := getMetadataFloat(building.Metadata, "healthMultiplier"); ok && v > 0 {
			hpBase = v
		}
		if v, ok := getMetadataFloat(building.Metadata, "healthMultiplierPerWave"); ok {
			hpPerWave = v
		}
		if v, ok := getMetadataFloat(building.Metadata, "damageMultiplier"); ok && v > 0 {
			dmgBase = v
		}
		if v, ok := getMetadataFloat(building.Metadata, "damageMultiplierPerWave"); ok {
			dmgPerWave = v
		}
	}
	wavesElapsed := 0
	if s.WaveManager.Enabled && s.WaveManager.CurrentWave > 1 {
		wavesElapsed = s.WaveManager.CurrentWave - 1
	}
	return hpBase + hpPerWave*float64(wavesElapsed), dmgBase + dmgPerWave*float64(wavesElapsed)
}

// applyWaveStatScalingLocked scales a freshly-spawned enemy's base HP and
// damage by the per-spawnpoint multipliers, then re-runs rank/path modifiers
// so derived stats stay consistent. HP is set to the new MaxHP.
func (s *GameState) applyWaveStatScalingLocked(unit *Unit, hpMult, dmgMult float64) {
	if unit == nil {
		return
	}
	if hpMult <= 0 {
		hpMult = 1
	}
	if dmgMult <= 0 {
		dmgMult = 1
	}
	if hpMult == 1 && dmgMult == 1 {
		return
	}
	if hpMult != 1 {
		unit.BaseMaxHP = maxInt(1, int(math.Round(float64(unit.BaseMaxHP)*hpMult)))
	}
	if dmgMult != 1 {
		unit.BaseDamage = maxInt(0, int(math.Round(float64(unit.BaseDamage)*dmgMult)))
	}
	s.applyRankModifiersLocked(unit, false)
	unit.HP = unit.MaxHP
}
