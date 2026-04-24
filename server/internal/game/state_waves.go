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
	Timer        float64
	PrepDuration float64
	WaveDuration float64 // 0 means no automatic timeout; wave must be ended externally
}

const enemyPlayerID = "__enemy__"

const enemyPlayerColor = "#e74c3c"

// -------------------------------------------------------------------------

// initWaveManagerLocked scans all enemy-spawnpoint buildings for "waveNumber"
// or "startingWave" metadata. If any have a value > 0 the wave system is
// enabled and the manager is initialised in the "prep" phase for wave 1.
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
	}

	if !hasWavePoints {
		// No wave-controlled spawn points — use legacy always-on mode.
		s.WaveManager = WaveManager{}
		return
	}

	prepDuration := wavePrepDuration
	waveDuration := waveActiveDuration
	totalWaves := maxWave

	if cfg := s.MapConfig.WaveConfig; cfg != nil {
		if cfg.PrepDuration > 0 {
			prepDuration = cfg.PrepDuration
		}
		if cfg.WaveDuration > 0 {
			waveDuration = cfg.WaveDuration
		}
		if cfg.TotalWaves > 0 {
			totalWaves = cfg.TotalWaves
		}
	}

	s.WaveManager = WaveManager{
		Enabled:      true,
		CurrentWave:  0, // 0 means "prep before wave 1"
		TotalWaves:   totalWaves,
		State:        "prep",
		Timer:        prepDuration,
		PrepDuration: prepDuration,
		WaveDuration: waveDuration,
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
			// Reset spawn timers so this wave's points re-arm from the wave start.
			s.resetWaveSpawnTimersLocked(wm.CurrentWave)
		}

	case "active":
		wm.Timer += dt
		// Spawn phase ends when the active timer expires, but the wave itself
		// does not end until every enemy spawned during it is dead. Hold the
		// timer at WaveDuration so tickEnemySpawnpointsLocked keeps seeing the
		// spawn phase as closed while we wait on the clear.
		timerExpired := wm.WaveDuration > 0 && wm.Timer >= wm.WaveDuration
		if timerExpired {
			wm.Timer = wm.WaveDuration
			if s.countEnemyUnitsLocked() == 0 {
				if wm.TotalWaves > 0 && wm.CurrentWave >= wm.TotalWaves {
					wm.State = "complete"
					s.markWaveObjectivesCompleteLocked()
				} else {
					wm.State = "prep"
					wm.Timer = wm.PrepDuration
				}
			}
		}

		// "complete" is terminal — nothing more to tick.
	}
}

// countEnemyUnitsLocked returns the number of living enemy units on the field
// that gate wave progression. Units spawned from spawnpoints flagged with
// metadata["ignoreWaveClear"] are skipped so ambient/background enemies do
// not stall the active → prep transition.
func (s *GameState) countEnemyUnitsLocked() int {
	count := 0
	for _, u := range s.Units {
		if u.OwnerID == enemyPlayerID && u.HP > 0 && u.Visible && !u.IgnoreWaveClear {
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
	}
}

func (s *GameState) tickEnemySpawnpointsLocked(dt float64, blocked map[gridPoint]bool) {
	for i := range s.MapConfig.Buildings {
		building := &s.MapConfig.Buildings[i]
		if building.BuildingType != "enemy-spawnpoint" {
			continue
		}

		// gameStart spawnpoints fire exactly once at match start, bypassing wave gating.
		isGameStart := getMetadataBool(building.Metadata, "gameStart")
		if !isGameStart {
			// Wave-gating: when wave mode is enabled, check waveNumber (specific wave)
			// and startingWave (every wave from N onwards). Points with neither field
			// (or waveNumber == 0) are legacy points that always fire regardless.
			if s.WaveManager.Enabled {
				wm := &s.WaveManager
				waveTimerExpired := wm.WaveDuration > 0 && wm.Timer >= wm.WaveDuration

				if sw, hasSW := getMetadataFloat(building.Metadata, "startingWave"); hasSW && int(sw) > 0 {
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
		// "__none__") but no matching player has joined yet, skip this spawnpoint.
		if tpl, ok := getMetadataString(building.Metadata, "targetPlayerLabel"); ok && tpl != "" && tpl != "__none__" {
			if s.findPlayerIDByLabelLocked(tpl) == "" {
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

		// Scale spawn count exponentially: wave N spawns 2^(N-1) × base count.
		if s.WaveManager.Enabled && s.WaveManager.CurrentWave > 1 {
			multiplier := 1 << uint(s.WaveManager.CurrentWave-1)
			if multiplier > 512 {
				multiplier = 512
			}
			spawnCount *= multiplier
		}

		hpMult, dmgMult := s.computeWaveStatScalingLocked(building)

		spawnPositions := s.getTownhallSpawnPositionsLocked(*building, spawnCount, blocked)

		center := protocol.Vec2{
			X: (float64(building.X) + float64(building.Width)/2) * s.MapConfig.CellSize,
			Y: (float64(building.Y) + float64(building.Height)/2) * s.MapConfig.CellSize,
		}

		for i := 0; i < spawnCount; i++ {
			var spawnPos protocol.Vec2
			if i < len(spawnPositions) {
				spawnPos = spawnPositions[i]
			} else {
				cell, ok := s.findNearestWalkable(s.worldToGrid(center.X, center.Y), blocked)
				if !ok {
					break
				}
				spawnPos = s.gridToWorldCenter(cell)
			}

			unit := s.spawnEnemyUnitLocked(unitType, spawnPos)
			if unit == nil {
				continue
			}
			s.applyWaveStatScalingLocked(unit, hpMult, dmgMult)
			unit.OrderID = orderID
			unit.IgnoreWaveClear = ignoreWaveClear
			if objectiveId != "" {
				// Existing objective mechanic: keep unit stationary at an objective.
				unit.ObjectiveID = objectiveId
				unit.Status = "Idle"
			} else if targetPlayerLabel == "__none__" {
				// Explicit stay-at-spawn: no path assigned.
				unit.Status = "Idle"
			} else if targetPlayerLabel != "" {
				// Route to a specific player's townhall, falling back to nearest.
				unit.Status = "Advancing"
				playerID := s.findPlayerIDByLabelLocked(targetPlayerLabel)
				var target *protocol.Vec2
				if playerID != "" {
					target = s.getPlayerTownhallCenterLocked(playerID)
				}
				if target == nil {
					target = s.getNearestPlayerTownhallCenterLocked(spawnPos.X, spawnPos.Y)
				}
				if target != nil {
					s.assignUnitPath(unit, *target, blocked, nil)
				}
			} else {
				// Default: route to nearest player townhall.
				unit.Status = "Advancing"
				target := s.getNearestPlayerTownhallCenterLocked(spawnPos.X, spawnPos.Y)
				if target != nil {
					s.assignUnitPath(unit, *target, blocked, nil)
				}
			}
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
		hp, _, ok := getBuildingHP(b)
		if !ok || hp <= 0 {
			continue
		}
		dist := s.distanceToBuilding(enemy.X, enemy.Y, b)
		if dist < bestDistSq {
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
