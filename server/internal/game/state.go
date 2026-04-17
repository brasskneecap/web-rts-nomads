package game

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"webrts/server/pkg/protocol"
)

type Unit struct {
	ID                  int
	OwnerID             string
	Color               string
	UnitType            string
	Archetype           string
	Name                string
	Capabilities        []string
	Visible             bool
	Status              string
	X                   float64
	Y                   float64
	HP                  int
	MaxHP               int
	BaseMaxHP           int
	BaseDamage          int
	BaseAttackSpeed     float64
	BaseMoveSpeed       float64
	XP                  int
	XPProgressRemainder float64
	Rank                string
	RankUpFxRemaining   float64
	ProgressionPath     string
	Armor               int
	PerkIDs             []string  // assigned perk ids, in rank-up order (index 0 = Bronze, 1 = Silver, 2 = Gold)
	PerkState           UnitPerkState // runtime state shared across the unit's perks

	// Shield is a temporary HP pool consumed before HP by applyUnitDamageLocked.
	// First-pass implementation: only granted by blood_engine (gold berserker)
	// via overheal conversion; no decay — persists until consumed. Extend here
	// if future perks need shield decay or alternate gain mechanics.
	Shield int

	CarriedResourceType string
	CarriedAmount       int
	GatherTargetID      string
	GatherBuildingType  string
	ReturnTargetID      string
	MiningInside        bool
	MiningRemaining     float64
	Gathering           bool
	Returning           bool
	BuildTargetID       string
	Building            bool
	TargetX             float64
	TargetY             float64
	Moving              bool
	Path                []protocol.Vec2
	OrderID             int64

	Damage                 int
	AttackRange            float64
	AttackSpeed            float64
	// MoveSpeed is the effective pixels-per-second for pathing movement, after
	// rank/path/perk modifiers are applied. Populated by applyRankModifiersLocked.
	MoveSpeed              float64
	AttackCooldown         float64
	AttackTargetID         int
	AttackBuildingTargetID string
	Attacking              bool
	ManualMove             bool

	CombatAnchorX      float64
	CombatAnchorY      float64
	LastTargetEvalTick int
	CurrentTargetScore float64
	TauntedByUnitID    int
	TauntRemaining     float64
	ThreatTable        map[int]*ThreatEntry
	TankedDamageByUnit map[int]float64
	// DamageDealtByUnit accumulates damage this unit has taken from each
	// attacker, keyed by attacker ID. On death the map is paid out so
	// contributors earn damage XP only when the target actually dies.
	DamageDealtByUnit map[int]int
}

const (
	// Unit move speed is now authored per-type in catalog/unit-defs.json
	// (UnitDef.MoveSpeed). Path multipliers (pathModifierTable) and perk
	// multipliers (momentum) stack on top of the per-unit BaseMoveSpeed.
	unitRadius             = 10.0
	unitFormationSpacing   = 28.0
	unitSeparationDistance = 22.0
)

type Player struct {
	ID        string
	Color     string
	Resources map[string]int

	GlobalUnitSpawnTimeMultiplier float64
	UnitSpawnTimeMultipliers      map[string]float64
}

type UnitProduction struct {
	PlayerID         string
	UnitType         string
	RemainingSeconds float64
	TotalSeconds     float64
}

type EnemySpawnTimer struct {
	RemainingDelay    float64
	TotalDelay        float64
	RemainingInterval float64
	TotalInterval     float64
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

const (
	wavePrepDuration   = 60.0
	waveActiveDuration = 120.0
)

type PlayerStartUnit struct {
	UnitType string
	Count    int
}

const enemyPlayerID = "__enemy__"
const enemyPlayerColor = "#e74c3c"

type GameState struct {
	mu sync.RWMutex

	Tick int

	MapConfig protocol.MapConfig
	MapID     string
	MapWidth  float64
	MapHeight float64

	Units   []*Unit
	Players map[string]*Player

	Productions      map[string][]*UnitProduction
	EnemySpawnTimers map[string]*EnemySpawnTimer
	WaveManager      WaveManager

	nextUnitID     int
	nextBuildingID int
	nextOrderID    int64
	rng            *rand.Rand

	// buildingDamageDealt mirrors Unit.DamageDealtByUnit for buildings.
	// buildingID → attackerID → accumulated damage. Paid out on destruction.
	buildingDamageDealt map[string]map[int]int
}

const (
	defaultGoldGatherAmount = 20
	defaultWoodGatherAmount = 15
	goldmineWorkerCap       = 3
	goldmineMiningSeconds   = 5.0
	treeWorkerCap           = 1
	treeChoppingSeconds     = 3.0
	minUnitSpawnSeconds     = 0.25
)

const (
	raiderDamage      = 5
	raiderAttackRange = 60.0
	raiderAttackSpeed = 1.0
	raiderHP          = 75
	raiderMaxHP       = 75
	// Mirror of catalog/unit-defs.json "raider".moveSpeed — kept here because
	// spawnRaiderUnitLocked doesn't do a def lookup like the soldier-type path.
	raiderMoveSpeed = 100.0
)

func NewGameState(mapConfig protocol.MapConfig) *GameState {
	state := &GameState{
		Units:               []*Unit{},
		Players:             map[string]*Player{},
		Productions:         map[string][]*UnitProduction{},
		EnemySpawnTimers:    map[string]*EnemySpawnTimer{},
		nextUnitID:          1,
		rng:                 rand.New(rand.NewSource(time.Now().UnixNano())),
		buildingDamageDealt: map[string]map[int]int{},
	}

	state.SetMapConfig(mapConfig)
	return state
}

func (s *GameState) SetMapConfig(mapConfig protocol.MapConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.setMapConfigLocked(mapConfig)
}

func (s *GameState) setMapConfigLocked(mapConfig protocol.MapConfig) {
	s.MapConfig = cloneMapConfig(mapConfig)
	s.MapID = s.MapConfig.ID
	s.MapWidth = s.MapConfig.Width
	s.MapHeight = s.MapConfig.Height
	s.Productions = map[string][]*UnitProduction{}
	s.EnemySpawnTimers = map[string]*EnemySpawnTimer{}
	s.initWaveManagerLocked()
}

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
		// The wave only ends once the active timer has expired AND all spawned
		// enemies have been killed. Spawners stop firing when the timer expires
		// (wave-gating skips them), so this just waits for cleanup.
		timerExpired := wm.WaveDuration > 0 && wm.Timer >= wm.WaveDuration
		if timerExpired && s.countEnemyUnitsLocked() == 0 {
			if wm.TotalWaves > 0 && wm.CurrentWave >= wm.TotalWaves {
				wm.State = "complete"
			} else {
				wm.State = "prep"
				wm.Timer = wm.PrepDuration
			}
		}

		// "complete" is terminal — nothing more to tick.
	}
}

// countEnemyUnitsLocked returns the number of living enemy units on the field.
func (s *GameState) countEnemyUnitsLocked() int {
	count := 0
	for _, u := range s.Units {
		if u.OwnerID == enemyPlayerID && u.HP > 0 && u.Visible {
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

func (s *GameState) GetMapConfig() protocol.MapConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.MapConfig
}

func (s *GameState) Snapshot() protocol.MatchSnapshotMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	units := make([]protocol.UnitSnapshot, 0, len(s.Units))
	for _, unit := range s.Units {
		// Effective stats for the HUD: base × rank × path (already in
		// unit.Damage/AttackSpeed/MoveSpeed) × live perk multipliers. Kept
		// target-agnostic (target=nil) so only self-based perk bonuses apply
		// here — per-hit situational bonuses like executioner still live in
		// the combat-resolution path.
		effectiveDamage := int(math.Round(float64(unit.Damage) * (1.0 + s.perkBonusDamageMultiplierLocked(unit, nil))))
		effectiveAttackSpeed := math.Max(0.1, unit.AttackSpeed+s.perkAttackSpeedBonusLocked(unit))
		effectiveMoveSpeed := unit.MoveSpeed * s.perkMoveSpeedMultiplierLocked(unit)

		snapshot := protocol.UnitSnapshot{
			ID:                  unit.ID,
			OwnerID:             unit.OwnerID,
			Color:               unit.Color,
			UnitType:            unit.UnitType,
			Archetype:           unit.Archetype,
			Name:                unit.Name,
			Capabilities:        append([]string(nil), unit.Capabilities...),
			Visible:             unit.Visible,
			Status:              unit.Status,
			X:                   unit.X,
			Y:                   unit.Y,
			HP:                  unit.HP,
			MaxHP:               unit.MaxHP,
			Damage:              effectiveDamage,
			AttackSpeed:         effectiveAttackSpeed,
			MoveSpeed:           effectiveMoveSpeed,
			Armor:               unit.Armor,
			XP:                  unit.XP,
			Rank:                unit.Rank,
			XPToNextRank:        s.unitXPToNextRankLocked(unit),
			XPIntoCurrentRank:   s.unitXPIntoCurrentRankLocked(unit),
			RecentRankUpSeconds: unit.RankUpFxRemaining,
			ProgressionPath:     unit.ProgressionPath,
			PerkIDs:             unit.PerkIDs,
			Shield:              unit.Shield,
			MaxShield:           s.unitMaxShieldLocked(unit),
			ActiveBuffs:         s.activeBuffIconsLocked(unit),
			CarriedResourceType: unit.CarriedResourceType,
			CarriedAmount:       unit.CarriedAmount,
			Moving:              unit.Moving,
		}

		if unit.Moving {
			snapshot.TargetX = unit.TargetX
			snapshot.TargetY = unit.TargetY
		}

		units = append(units, snapshot)
	}

	players := make([]protocol.PlayerSnapshot, 0, len(s.Players))
	for _, player := range s.Players {
		if player.ID == enemyPlayerID {
			continue
		}
		players = append(players, protocol.PlayerSnapshot{
			PlayerID:  player.ID,
			Color:     player.Color,
			Resources: s.getPlayerResourceStocksLocked(player),
		})
	}

	wm := s.WaveManager
	return protocol.MatchSnapshotMessage{
		Type:      "match_snapshot",
		Tick:      s.Tick,
		ServerNow: time.Now().UnixMilli(),
		Map:       s.MapConfig,
		Players:   players,
		Units:     units,
		Wave: protocol.WaveSnapshot{
			Enabled:      wm.Enabled,
			CurrentWave:  wm.CurrentWave,
			TotalWaves:   wm.TotalWaves,
			State:        wm.State,
			Timer:        wm.Timer,
			WaveDuration: wm.WaveDuration,
		},
	}
}

func (s *GameState) IncrementTick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Tick++
}

func (s *GameState) Update(dt float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.updateUnitProductionsLocked(dt)
	s.tickBuildingRepairsLocked(dt)
	blocked := s.buildBlockedCells()
	s.tickBuildingCombatLocked(dt)
	s.tickWaveLocked(dt)
	s.tickCombatAILocked(dt, blocked)
	s.tickUnitCombatLocked(dt, blocked)
	s.tickEnemySpawnpointsLocked(dt, blocked)

	for _, unit := range s.Units {
		if unit.RankUpFxRemaining > 0 {
			unit.RankUpFxRemaining = math.Max(0, unit.RankUpFxRemaining-dt)
		}
		// Advance time-based perk state (idle timers, buff durations).
		s.tickUnitPerkStateLocked(unit, dt)
		s.updateWorkerTaskLocked(unit, dt, blocked)

		if unit.MiningInside {
			continue
		}

		if !unit.Moving {
			unit.ManualMove = false
			continue
		}

		if len(unit.Path) == 0 {
			unit.Moving = false
			unit.ManualMove = false
			continue
		}

		nextWaypoint := unit.Path[0]
		dx := nextWaypoint.X - unit.X
		dy := nextWaypoint.Y - unit.Y
		dist := math.Sqrt(dx*dx + dy*dy)

		if dist == 0 {
			unit.X = nextWaypoint.X
			unit.Y = nextWaypoint.Y
			unit.Path = unit.Path[1:]
			unit.Moving = len(unit.Path) > 0
			continue
		}

		// Effective move speed: per-unit stat (base × rank × path, already baked
		// into unit.MoveSpeed by applyRankModifiersLocked) × perk multiplier
		// (momentum, future speed perks).
		step := unit.MoveSpeed * s.perkMoveSpeedMultiplierLocked(unit) * dt
		if step >= dist {
			unit.X = nextWaypoint.X
			unit.Y = nextWaypoint.Y
			unit.Path = unit.Path[1:]
			unit.Moving = len(unit.Path) > 0
			continue
		}

		nextX := unit.X + (dx/dist)*step
		nextY := unit.Y + (dy/dist)*step
		nextCell := s.worldToGrid(nextX, nextY)
		if !s.isWalkable(nextCell, blocked) {
			if !s.repathUnitLocked(unit, blocked) {
				unit.Path = nil
				unit.Moving = false
			}
			continue
		}

		unit.X = nextX
		unit.Y = nextY
	}

	s.applyUnitSeparationLocked(blocked)
	s.refreshBuildingRuntimeMetadataLocked()
}

func (s *GameState) MoveUnits(playerID string, unitIDs []int, dest protocol.Vec2) {
	s.mu.Lock()
	defer s.mu.Unlock()

	validUnits := make([]*Unit, 0, len(unitIDs))
	unitMap := make(map[int]*Unit, len(s.Units))
	blocked := s.buildBlockedCells()

	for _, unit := range s.Units {
		unitMap[unit.ID] = unit
	}

	for _, unitID := range unitIDs {
		unit, ok := unitMap[unitID]
		if !ok {
			continue
		}
		if unit.OwnerID != playerID {
			continue
		}
		validUnits = append(validUnits, unit)
	}

	if len(validUnits) == 0 {
		return
	}

	if len(validUnits) == 1 {
		unit := validUnits[0]
		orderID := s.nextMovementOrderIDLocked()
		s.resetUnitMovementLocked(unit, orderID)
		unit.ManualMove = true
		unit.CombatAnchorX = dest.X
		unit.CombatAnchorY = dest.Y
		s.assignUnitPath(unit, dest, blocked, nil)
		return
	}

	clampedDest := protocol.Vec2{
		X: clampFloat(dest.X, unitRadius, s.MapWidth-unitRadius),
		Y: clampFloat(dest.Y, unitRadius, s.MapHeight-unitRadius),
	}
	anchorGoal := s.worldToGrid(clampedDest.X, clampedDest.Y)
	anchorCell, ok := s.findNearestWalkable(anchorGoal, blocked)
	if !ok {
		return
	}

	anchor := s.clampPointToCell(clampedDest, anchorCell)
	targets := buildFormationTargets(validUnits, anchor, unitFormationSpacing)
	orderID := s.nextMovementOrderIDLocked()

	for i, unit := range validUnits {
		target := targets[i]
		s.resetUnitMovementLocked(unit, orderID)
		unit.ManualMove = true
		unit.CombatAnchorX = target.X
		unit.CombatAnchorY = target.Y

		s.assignUnitPath(unit, protocol.Vec2{
			X: clampFloat(target.X, 0, s.MapWidth),
			Y: clampFloat(target.Y, 0, s.MapHeight),
		}, blocked, nil)
	}
}

func (s *GameState) AttackMoveUnits(playerID string, unitIDs []int, dest protocol.Vec2) {
	s.mu.Lock()
	defer s.mu.Unlock()

	validUnits := make([]*Unit, 0, len(unitIDs))
	unitMap := make(map[int]*Unit, len(s.Units))
	blocked := s.buildBlockedCells()

	for _, unit := range s.Units {
		unitMap[unit.ID] = unit
	}

	for _, unitID := range unitIDs {
		unit, ok := unitMap[unitID]
		if !ok {
			continue
		}
		if unit.OwnerID != playerID {
			continue
		}
		validUnits = append(validUnits, unit)
	}

	if len(validUnits) == 0 {
		return
	}

	if len(validUnits) == 1 {
		unit := validUnits[0]
		orderID := s.nextMovementOrderIDLocked()
		s.resetUnitMovementLocked(unit, orderID)
		unit.CombatAnchorX = dest.X
		unit.CombatAnchorY = dest.Y
		s.assignUnitPath(unit, dest, blocked, nil)
		return
	}

	clampedDest := protocol.Vec2{
		X: clampFloat(dest.X, unitRadius, s.MapWidth-unitRadius),
		Y: clampFloat(dest.Y, unitRadius, s.MapHeight-unitRadius),
	}
	anchorGoal := s.worldToGrid(clampedDest.X, clampedDest.Y)
	anchorCell, ok := s.findNearestWalkable(anchorGoal, blocked)
	if !ok {
		return
	}

	anchor := s.clampPointToCell(clampedDest, anchorCell)
	targets := buildFormationTargets(validUnits, anchor, unitFormationSpacing)
	orderID := s.nextMovementOrderIDLocked()

	for i, unit := range validUnits {
		target := targets[i]
		s.resetUnitMovementLocked(unit, orderID)
		unit.CombatAnchorX = target.X
		unit.CombatAnchorY = target.Y

		s.assignUnitPath(unit, protocol.Vec2{
			X: clampFloat(target.X, 0, s.MapWidth),
			Y: clampFloat(target.Y, 0, s.MapHeight),
		}, blocked, nil)
	}
}

func (s *GameState) EnsurePlayer(playerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.Players[playerID]; exists {
		return
	}

	color := s.randomColor()
	s.Players[playerID] = &Player{
		ID:    playerID,
		Color: color,
		Resources: map[string]int{
			"gold": 500,
			"wood": 180,
		},
		GlobalUnitSpawnTimeMultiplier: 1,
		UnitSpawnTimeMultipliers:      map[string]float64{},
	}

	home, spawnPoint := s.claimPlayerStartLocked(playerID)
	s.spawnUnitsForPlayerLocked(playerID, color, home, s.getPlayerStartLoadoutLocked(spawnPoint))
}

func (s *GameState) RemovePlayer(playerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.Players, playerID)

	filtered := make([]*Unit, 0, len(s.Units))
	for _, unit := range s.Units {
		if unit.OwnerID != playerID {
			filtered = append(filtered, unit)
		}
	}

	s.Units = filtered
	s.releaseTownhallForPlayerLocked(playerID)
}

func (s *GameState) spawnUnitsForPlayerLocked(playerID, color string, home *protocol.BuildingTile, loadout []PlayerStartUnit) {
	totalCount := 0
	for _, entry := range loadout {
		if entry.Count > 0 {
			totalCount += entry.Count
		}
	}
	if totalCount <= 0 {
		return
	}

	playerIndex := len(s.Players) - 1
	blocked := s.buildBlockedCells()
	spawnPositions := make([]protocol.Vec2, 0, totalCount)

	if home != nil {
		spawnPositions = s.getTownhallSpawnPositionsLocked(*home, totalCount, blocked)
	}

	if len(spawnPositions) < totalCount {
		spawnPositions = append(spawnPositions, s.getFallbackSpawnPositionsLocked(playerIndex, totalCount-len(spawnPositions), blocked)...)
	}
	if len(spawnPositions) == 0 {
		return
	}

	spawnIndex := 0
	for _, entry := range loadout {
		if entry.Count <= 0 {
			continue
		}
		for i := 0; i < entry.Count; i++ {
			spawn := spawnPositions[minInt(spawnIndex, len(spawnPositions)-1)]
			s.spawnPlayerUnitLocked(entry.UnitType, playerID, color, spawn)
			spawnIndex++
		}
	}
}

func (s *GameState) spawnPlayerUnitLocked(unitType, playerID, color string, spawn protocol.Vec2) *Unit {
	def, ok := getUnitDef(unitType)
	if !ok {
		return nil
	}
	return s.spawnUnitFromDefLocked(def, unitType, playerID, color, spawn)
}

func (s *GameState) spawnUnitFromDefLocked(def UnitDef, unitType, playerID, color string, spawn protocol.Vec2) *Unit {
	unit := &Unit{
		ID:                 s.nextUnitID,
		OwnerID:            playerID,
		Color:              color,
		UnitType:           unitType,
		Archetype:          resolveUnitArchetype(def, unitType),
		Name:               def.Name,
		Capabilities:       append([]string{}, def.Capabilities...),
		Visible:            true,
		Status:             "Idle",
		X:                  spawn.X,
		Y:                  spawn.Y,
		HP:                 def.HP,
		MaxHP:              def.HP,
		BaseMaxHP:          def.HP,
		BaseDamage:         def.Damage,
		BaseAttackSpeed:    def.AttackSpeed,
		BaseMoveSpeed:      def.MoveSpeed,
		Damage:             def.Damage,
		AttackRange:        def.AttackRange,
		AttackSpeed:        def.AttackSpeed,
		MoveSpeed:          def.MoveSpeed,
		Rank:               unitRankBase,
		ProgressionPath:    unitPathNone,
		CombatAnchorX:      spawn.X,
		CombatAnchorY:      spawn.Y,
		ThreatTable:        map[int]*ThreatEntry{},
		TankedDamageByUnit: map[int]float64{},
		DamageDealtByUnit:  map[int]int{},
	}

	s.nextUnitID++
	s.Units = append(s.Units, unit)
	s.initializeCombatUnitLocked(unit)
	s.applyRankModifiersLocked(unit, false)
	return unit
}

func (s *GameState) spawnRaiderUnitLocked(playerID, color string, spawn protocol.Vec2) *Unit {
	unit := &Unit{
		ID:                 s.nextUnitID,
		OwnerID:            playerID,
		Color:              color,
		UnitType:           "raider",
		Archetype:          "raider",
		Name:               "Raider",
		Capabilities:       []string{"move", "attack"},
		Visible:            true,
		Status:             "Idle",
		X:                  spawn.X,
		Y:                  spawn.Y,
		HP:                 raiderHP,
		MaxHP:              raiderMaxHP,
		BaseMaxHP:          raiderMaxHP,
		BaseDamage:         raiderDamage,
		BaseAttackSpeed:    raiderAttackSpeed,
		BaseMoveSpeed:      raiderMoveSpeed,
		MoveSpeed:          raiderMoveSpeed,
		Damage:             raiderDamage,
		AttackRange:        raiderAttackRange,
		AttackSpeed:        raiderAttackSpeed,
		Rank:               unitRankBase,
		ProgressionPath:    unitPathNone,
		CombatAnchorX:      spawn.X,
		CombatAnchorY:      spawn.Y,
		ThreatTable:        map[int]*ThreatEntry{},
		TankedDamageByUnit: map[int]float64{},
		DamageDealtByUnit:  map[int]int{},
	}

	s.nextUnitID++
	s.Units = append(s.Units, unit)
	s.initializeCombatUnitLocked(unit)
	s.applyRankModifiersLocked(unit, false)
	return unit
}

func (s *GameState) spawnEnemyUnitLocked(unitType string, spawn protocol.Vec2) *Unit {
	if def, ok := getUnitDef(unitType); ok {
		return s.spawnUnitFromDefLocked(def, unitType, enemyPlayerID, enemyPlayerColor, spawn)
	}
	switch unitType {
	case "raider":
		return s.spawnRaiderUnitLocked(enemyPlayerID, enemyPlayerColor, spawn)
	default:
		return s.spawnRaiderUnitLocked(enemyPlayerID, enemyPlayerColor, spawn)
	}
}

func resolveUnitArchetype(def UnitDef, unitType string) string {
	if def.Archetype != "" {
		return def.Archetype
	}
	return unitType
}

func (s *GameState) removeUnitLocked(unitID int) {
	filtered := make([]*Unit, 0, len(s.Units))
	for _, u := range s.Units {
		if u.ID != unitID {
			filtered = append(filtered, u)
		}
	}
	s.Units = filtered

	// Clear attack targets pointing to removed unit
	for _, u := range s.Units {
		if u.AttackTargetID == unitID {
			u.AttackTargetID = 0
			u.Attacking = false
			u.Status = "Idle"
		}
		delete(u.ThreatTable, unitID)
		delete(u.TankedDamageByUnit, unitID)
		delete(u.DamageDealtByUnit, unitID)
		if u.TauntedByUnitID == unitID {
			u.TauntedByUnitID = 0
			u.TauntRemaining = 0
		}
	}
	// Forfeit banked damage-dealt XP on any building: if this unit is dead it
	// can no longer earn XP, so strip its entries from every building's map.
	for buildingID, m := range s.buildingDamageDealt {
		delete(m, unitID)
		if len(m) == 0 {
			delete(s.buildingDamageDealt, buildingID)
		}
	}
}

func (s *GameState) TrainUnit(playerID, buildingID, unitType string) {
	if _, ok := getUnitDef(unitType); !ok {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	building := s.getBuildingByIDLocked(buildingID)
	if building == nil || !building.Visible {
		return
	}
	if building.OwnerID == nil || *building.OwnerID != playerID {
		return
	}
	if building.Metadata != nil && building.Metadata["underConstruction"] == true {
		return
	}
	if !containsString(building.SpawnUnitTypes, unitType) {
		return
	}
	player, ok := s.Players[playerID]
	if !ok {
		return
	}
	if !s.canAffordUnitCostLocked(player, unitType) {
		return
	}
	if !s.canAffordMeatCostLocked(playerID, unitType) {
		return
	}

	s.payUnitCostLocked(player, unitType)
	s.beginUnitProductionLocked(player, *building, unitType)
}

func (s *GameState) AttackWithUnits(playerID string, unitIDs []int, targetUnitID int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.getUnitByIDLocked(targetUnitID)
	if target == nil || !target.Visible || target.OwnerID == playerID {
		return
	}

	blocked := s.buildBlockedCells()
	orderID := s.nextMovementOrderIDLocked()

	for _, unitID := range unitIDs {
		unit := s.getUnitByIDLocked(unitID)
		if unit == nil || unit.OwnerID != playerID || !unitHasCapability(unit.UnitType, "attack") {
			continue
		}

		s.resetUnitMovementLocked(unit, orderID)
		unit.AttackTargetID = targetUnitID
		unit.Attacking = false
		unit.Status = "Moving To Attack"
		unit.CombatAnchorX = unit.X
		unit.CombatAnchorY = unit.Y

		dx := target.X - unit.X
		dy := target.Y - unit.Y
		dist := math.Sqrt(dx*dx + dy*dy)
		if dist > unit.AttackRange {
			s.assignUnitPath(unit, protocol.Vec2{X: target.X, Y: target.Y}, blocked, nil)
		}
	}
}

func (s *GameState) tickUnitCombatLocked(dt float64, blocked map[gridPoint]bool) {
	var deadUnitIDs []int
	var destroyedBuildingIDs []string

	for _, unit := range s.Units {
		// Handle unit-vs-unit combat
		if unit.AttackTargetID != 0 {
			target := s.getUnitByIDLocked(unit.AttackTargetID)
			if target == nil || !target.Visible {
				unit.AttackTargetID = 0
				unit.Attacking = false
				unit.Status = "Idle"
			} else {
				dx := target.X - unit.X
				dy := target.Y - unit.Y
				dist := math.Sqrt(dx*dx + dy*dy)

				if dist <= unit.AttackRange {
					unit.Moving = false
					unit.Path = nil
					unit.Attacking = true
					unit.Status = "Attacking"

					if unit.AttackCooldown <= 0 {
						// Outgoing damage: base × (1 + perk bonus), then armor.
						// perk bonus comes from executioner (silver berserker) and any
						// future outgoing-damage-multiplier perks.
						rawDamage := float64(unit.Damage) * (1.0 + s.perkBonusDamageMultiplierLocked(unit, target))
						damage := maxInt(0, int(math.Round(rawDamage))-target.Armor)
						// Route through the shared helper so shield (blood_engine) absorbs first.
						s.applyUnitDamageLocked(target, damage)
						s.onUnitDamagedLocked(unit, target, damage)
						s.recordSoldierTankContributionLocked(unit, target, damage)
						s.recordDamageDealtLocked(unit, target, damage)
						// Perk on-attack effects (bloodlust accumulation,
						// savage_strikes bonus hit, cleaving_rage extra target,
						// momentum move-speed refresh).
						s.onPerkAttackFiredLocked(unit, target, damage, &deadUnitIDs)
						// Perk on-hit reactions (blood_sustain lifesteal, future on-hit procs).
						s.onPerkAttackDamageAppliedLocked(unit, target, damage)
						// Use effective attack speed (base + perk bonus) for the cooldown.
						effectiveSpeed := math.Max(0.1, unit.AttackSpeed+s.perkAttackSpeedBonusLocked(unit))
						unit.AttackCooldown = 1.0 / effectiveSpeed
						if target.HP <= 0 {
							target.HP = 0
							s.awardKillXPLocked(unit)
							s.payoutDamageDealtXPLocked(target)
							s.awardSoldierTankKillXPLocked(target.ID)
							s.onPerkKillLocked(unit) // perk on-kill effects (relentless boost)
							deadUnitIDs = append(deadUnitIDs, target.ID)
						}
					} else {
						unit.AttackCooldown = math.Max(0, unit.AttackCooldown-dt)
					}
				} else {
					unit.Attacking = false
					unit.Status = "Moving To Attack"
					profile := resolveCombatProfile(unit)
					if !unit.Moving {
						s.refreshUnitAttackApproachLocked(unit, target, profile, blocked, true)
					} else {
						s.refreshUnitAttackApproachLocked(unit, target, profile, blocked, false)
					}
				}
			}
			continue
		}

		// Handle unit-vs-building combat
		if unit.AttackBuildingTargetID != "" {
			building := s.getBuildingByIDLocked(unit.AttackBuildingTargetID)
			if building == nil {
				unit.AttackBuildingTargetID = ""
				unit.Attacking = false
				unit.Status = "Idle"
				continue
			}
			hp, _, hpOk := getBuildingHP(building)
			if !hpOk || hp <= 0 {
				unit.AttackBuildingTargetID = ""
				unit.Attacking = false
				unit.Status = "Idle"
			} else {
				dist := s.distanceToBuilding(unit.X, unit.Y, building)

				if dist <= unit.AttackRange {
					unit.Moving = false
					unit.Path = nil
					unit.Attacking = true
					unit.Status = "Attacking"

					if unit.AttackCooldown <= 0 {
						damage := unit.Damage
						newHP := hp - float64(damage)
						building.Metadata["hp"] = newHP
						s.onBuildingDamagedLocked(unit, building, damage)
						s.recordDamageDealtBuildingLocked(unit, building.ID, damage)
						unit.AttackCooldown = 1.0 / unit.AttackSpeed
						if newHP <= 0 {
							building.Metadata["hp"] = 0.0
							s.payoutBuildingDamageDealtXPLocked(building.ID)
							destroyedBuildingIDs = append(destroyedBuildingIDs, building.ID)
						}
					} else {
						unit.AttackCooldown = math.Max(0, unit.AttackCooldown-dt)
					}
				} else {
					unit.Attacking = false
					unit.Status = "Moving To Attack"
					if !unit.Moving {
						// Re-path to the same claimed position rather than recalculating,
						// so enemies don't all converge on the same closest cell.
						s.assignUnitPath(unit, protocol.Vec2{X: unit.TargetX, Y: unit.TargetY}, blocked, nil)
					}
				}
			}
			continue
		}

		if unit.AttackCooldown > 0 {
			unit.AttackCooldown = math.Max(0, unit.AttackCooldown-dt)
		}
	}

	for _, id := range deadUnitIDs {
		s.removeUnitLocked(id)
	}
	for _, id := range destroyedBuildingIDs {
		s.destroyBuildingLocked(id)
	}
}

func (s *GameState) assignUnitPath(unit *Unit, dest protocol.Vec2, blocked map[gridPoint]bool, reservedGoals map[gridPoint]bool) {
	clampedDest := protocol.Vec2{
		X: clampFloat(dest.X, unitRadius, s.MapWidth-unitRadius),
		Y: clampFloat(dest.Y, unitRadius, s.MapHeight-unitRadius),
	}

	start := s.worldToGrid(unit.X, unit.Y)
	resolvedStart, ok := s.findNearestWalkable(start, blocked)
	if ok {
		start = resolvedStart
	}
	goal := s.worldToGrid(clampedDest.X, clampedDest.Y)

	resolvedGoal, ok := s.findNearestWalkableAvailable(goal, blocked, reservedGoals)
	if !ok {
		unit.Path = nil
		unit.Moving = false
		return
	}

	path := s.findPath(start, resolvedGoal, blocked)
	if len(path) == 0 {
		unit.Path = nil
		unit.Moving = false
		return
	}

	if len(path) > 0 && distanceSquared(unit.X, unit.Y, path[0].X, path[0].Y) < 4 {
		path = path[1:]
	}

	if firstStep := s.buildPathEntryPoint(unit, start); firstStep != nil {
		path = append([]protocol.Vec2{*firstStep}, path...)
	}

	finalTarget := s.clampPointToCell(clampedDest, resolvedGoal)
	if len(path) == 0 {
		path = []protocol.Vec2{finalTarget}
	} else {
		path[len(path)-1] = finalTarget
	}
	path = simplifyLeadingWaypoints(unit, path, finalTarget)

	if reservedGoals != nil {
		reservedGoals[resolvedGoal] = true
	}

	unit.TargetX = finalTarget.X
	unit.TargetY = finalTarget.Y
	unit.Path = path
	unit.Moving = len(path) > 0
}

func (s *GameState) GatherWithUnits(playerID string, unitIDs []int, buildingID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	building := s.getBuildingByIDLocked(buildingID)
	if building == nil || !building.Visible || building.ResourceAmount <= 0 {
		return
	}
	if building.BuildingType != "goldmine" && building.BuildingType != "tree" {
		return
	}

	blocked := s.buildBlockedCells()
	if len(s.getBuildingApproachPositionsLocked(*building, 1, blocked, nil)) == 0 {
		return
	}

	orderID := s.nextMovementOrderIDLocked()

	for _, unitID := range unitIDs {
		unit := s.getUnitByIDLocked(unitID)
		if unit == nil || unit.OwnerID != playerID || !unitHasCapability(unit.UnitType, "gather") {
			continue
		}

		unitPos := &protocol.Vec2{X: unit.X, Y: unit.Y}
		approachPoints := s.getBuildingApproachPositionsLocked(*building, 1, blocked, unitPos)
		if len(approachPoints) == 0 {
			continue
		}

		s.resetUnitMovementLocked(unit, orderID)
		unit.GatherTargetID = buildingID
		unit.GatherBuildingType = building.BuildingType
		unit.ReturnTargetID = ""
		unit.Gathering = false
		unit.Returning = false
		s.assignUnitPath(unit, approachPoints[0], blocked, nil)
	}
}

func (s *GameState) CancelCurrentTraining(playerID, buildingID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	building := s.getBuildingByIDLocked(buildingID)
	if building == nil || !building.Visible {
		return
	}
	if building.BuildingType != "townhall" && building.BuildingType != "barracks" {
		return
	}
	if building.OwnerID == nil || *building.OwnerID != playerID {
		return
	}

	queue := s.Productions[building.ID]
	if len(queue) == 0 {
		return
	}

	player, ok := s.Players[playerID]
	if !ok {
		return
	}

	s.refundUnitCostLocked(player, queue[0].UnitType)
	s.consumeProductionQueueItemLocked(building.ID)
}

func (s *GameState) SetBuildingSpawnPoint(playerID, buildingID string, point protocol.Vec2) {
	s.mu.Lock()
	defer s.mu.Unlock()

	building := s.getBuildingByIDLocked(buildingID)
	if building == nil || !building.Visible {
		return
	}
	if building.BuildingType != "townhall" && building.BuildingType != "barracks" {
		return
	}
	if building.OwnerID == nil || *building.OwnerID != playerID {
		return
	}

	clampedPoint := protocol.Vec2{
		X: clampFloat(point.X, unitRadius, s.MapWidth-unitRadius),
		Y: clampFloat(point.Y, unitRadius, s.MapHeight-unitRadius),
	}
	if building.Metadata == nil {
		building.Metadata = map[string]interface{}{}
	}
	building.Metadata["spawnPointX"] = clampedPoint.X
	building.Metadata["spawnPointY"] = clampedPoint.Y
}

func (s *GameState) BuildBuilding(playerID, buildingType string, unitIDs []int, gridX, gridY int) {
	def, ok := getBuildingDef(buildingType)
	if !ok {
		return
	}
	if !def.IsBuildable() {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	player, ok := s.Players[playerID]
	if !ok {
		return
	}

	for resource, cost := range def.ResourceCost {
		if player.Resources[resource] < cost {
			return
		}
	}

	gridW, gridH := def.Width, def.Height

	if gridX < 0 || gridY < 0 || gridX+gridW > s.MapConfig.GridCols || gridY+gridH > s.MapConfig.GridRows {
		return
	}

	blocked := s.buildBlockedCells()
	for dy := 0; dy < gridH; dy++ {
		for dx := 0; dx < gridW; dx++ {
			if blocked[gridPoint{X: gridX + dx, Y: gridY + dy}] {
				return
			}
		}
	}

	for _, unit := range s.Units {
		if !unit.Visible {
			continue
		}
		cell := s.worldToGrid(unit.X, unit.Y)
		if cell.X >= gridX && cell.X < gridX+gridW && cell.Y >= gridY && cell.Y < gridY+gridH {
			return
		}
	}

	for resource, cost := range def.ResourceCost {
		player.Resources[resource] -= cost
	}

	metadata := map[string]interface{}{
		"underConstruction": true,
		"hp":                1.0,
		"maxHp":             def.MaxHp,
		"hpPerSecond":       def.HpPerSecond(),
	}
	for k, v := range def.Metadata {
		metadata[k] = v
	}

	s.nextBuildingID++
	ownerID := playerID
	building := protocol.BuildingTile{
		GridCoord:      protocol.GridCoord{X: gridX, Y: gridY},
		ID:             fmt.Sprintf("%s-%d", buildingType, s.nextBuildingID),
		BuildingType:   buildingType,
		Width:          gridW,
		Height:         gridH,
		Occupied:       true,
		Visible:        true,
		OwnerID:        &ownerID,
		Capabilities:   append([]string{}, def.Capabilities...),
		SpawnUnitTypes: append([]string{}, def.SpawnUnitTypes...),
		Metadata:       metadata,
	}

	s.MapConfig.Buildings = append(s.MapConfig.Buildings, building)
	buildingID := building.ID

	blocked = s.buildBlockedCells()
	orderID := s.nextMovementOrderIDLocked()

	assigned := 0
	for _, unitID := range unitIDs {
		if assigned >= maxBuildersPerBuilding {
			break
		}
		unit := s.getUnitByIDLocked(unitID)
		if unit == nil || unit.OwnerID != playerID || unit.UnitType != "worker" {
			continue
		}
		approachPoints := s.getBuildingApproachPositionsLocked(building, 1, blocked, &protocol.Vec2{X: unit.X, Y: unit.Y})
		if len(approachPoints) == 0 {
			continue
		}
		s.resetUnitMovementLocked(unit, orderID)
		unit.BuildTargetID = buildingID
		s.assignUnitPath(unit, approachPoints[0], blocked, nil)
		assigned++
	}
}

func (s *GameState) repathUnitLocked(unit *Unit, blocked map[gridPoint]bool) bool {
	if !unit.Moving {
		return false
	}

	dest := protocol.Vec2{X: unit.TargetX, Y: unit.TargetY}
	s.assignUnitPath(unit, dest, blocked, nil)
	return unit.Moving
}

func (s *GameState) claimTownhallForPlayerLocked(playerID string) *protocol.BuildingTile {
	for i := range s.MapConfig.Buildings {
		building := &s.MapConfig.Buildings[i]
		if building.BuildingType != "townhall" {
			continue
		}
		if building.OwnerID != nil && *building.OwnerID == playerID {
			building.Occupied = true
			building.Visible = true
			return building
		}
	}

	for i := range s.MapConfig.Buildings {
		building := &s.MapConfig.Buildings[i]
		if building.BuildingType != "townhall" || building.Occupied {
			continue
		}
		return s.claimSpecificTownhallForPlayerLocked(playerID, building.ID)
	}

	return nil
}

func (s *GameState) claimSpecificTownhallForPlayerLocked(playerID, buildingID string) *protocol.BuildingTile {
	for i := range s.MapConfig.Buildings {
		building := &s.MapConfig.Buildings[i]
		if building.BuildingType != "townhall" || building.ID != buildingID {
			continue
		}
		if building.Occupied && (building.OwnerID == nil || *building.OwnerID != playerID) {
			return nil
		}

		ownerID := playerID
		building.OwnerID = &ownerID
		building.Occupied = true
		building.Visible = true
		if building.Metadata == nil {
			building.Metadata = map[string]interface{}{}
		}
		def, _ := getBuildingDef("townhall")
		building.Metadata["hp"] = def.MaxHp
		building.Metadata["maxHp"] = def.MaxHp
		building.SpawnUnitTypes = append([]string{}, def.SpawnUnitTypes...)
		return building
	}

	return nil
}

func (s *GameState) releaseTownhallForPlayerLocked(playerID string) {
	for i := range s.MapConfig.Buildings {
		building := &s.MapConfig.Buildings[i]
		if building.BuildingType != "townhall" || building.OwnerID == nil || *building.OwnerID != playerID {
			continue
		}

		building.OwnerID = nil
		building.Occupied = false
		building.Visible = false
		delete(s.Productions, building.ID)
	}
}

func (s *GameState) claimPlayerStartLocked(playerID string) (*protocol.BuildingTile, *protocol.BuildingTile) {
	for i := range s.MapConfig.Buildings {
		building := &s.MapConfig.Buildings[i]
		if building.BuildingType == "townhall" && building.OwnerID != nil && *building.OwnerID == playerID {
			return building, s.getLinkedSpawnPointForTownhallLocked(*building)
		}
	}

	spawnPoints := make([]*protocol.BuildingTile, 0)
	for i := range s.MapConfig.Buildings {
		if s.MapConfig.Buildings[i].BuildingType == "spawn-point" {
			spawnPoints = append(spawnPoints, &s.MapConfig.Buildings[i])
		}
	}
	sort.Slice(spawnPoints, func(i, j int) bool {
		return getSpawnFillOrder(spawnPoints[i]) < getSpawnFillOrder(spawnPoints[j])
	})
	for _, spawnPoint := range spawnPoints {
		townhall := s.resolveSpawnPointTownhallLocked(*spawnPoint, false)
		if townhall == nil {
			continue
		}

		claimed := s.claimSpecificTownhallForPlayerLocked(playerID, townhall.ID)
		if claimed != nil {
			return claimed, spawnPoint
		}
	}

	home := s.claimTownhallForPlayerLocked(playerID)
	if home == nil {
		return nil, nil
	}
	return home, s.getLinkedSpawnPointForTownhallLocked(*home)
}

func getSpawnFillOrder(spawnPoint *protocol.BuildingTile) float64 {
	if spawnPoint.Metadata == nil {
		return 0
	}
	if v, ok := getMetadataFloat(spawnPoint.Metadata, "fillOrder"); ok {
		return v
	}
	return 0
}

func (s *GameState) getPlayerStartLoadoutLocked(spawnPoint *protocol.BuildingTile) []PlayerStartUnit {
	defaultLoadout := []PlayerStartUnit{{UnitType: "worker", Count: 3}}
	if spawnPoint == nil || spawnPoint.Metadata == nil {
		return defaultLoadout
	}

	loadout := make([]PlayerStartUnit, 0)
	if rawEntries, ok := spawnPoint.Metadata["spawnUnits"].([]interface{}); ok {
		for _, rawEntry := range rawEntries {
			entryMap, ok := rawEntry.(map[string]interface{})
			if !ok {
				continue
			}

			unitType, ok := getMetadataString(entryMap, "unitType")
			if !ok {
				continue
			}
			if _, exists := getUnitDef(unitType); !exists {
				continue
			}

			count := 1
			if configuredCount, ok := getMetadataFloat(entryMap, "count"); ok && configuredCount >= 1 {
				count = int(configuredCount)
			}

			loadout = append(loadout, PlayerStartUnit{
				UnitType: unitType,
				Count:    count,
			})
		}
	}

	if len(loadout) > 0 {
		return loadout
	}

	// Backwards compatibility for older maps using unitType/spawnCount.
	unitType := "worker"
	count := 3
	if configuredType, ok := getMetadataString(spawnPoint.Metadata, "unitType"); ok {
		if _, exists := getUnitDef(configuredType); exists {
			unitType = configuredType
		}
	}
	if configuredCount, ok := getMetadataFloat(spawnPoint.Metadata, "spawnCount"); ok && configuredCount >= 1 {
		count = int(configuredCount)
	}

	return []PlayerStartUnit{{UnitType: unitType, Count: count}}
}

func (s *GameState) getLinkedSpawnPointForTownhallLocked(home protocol.BuildingTile) *protocol.BuildingTile {
	homeCenter := protocol.Vec2{
		X: (float64(home.X) + float64(home.Width)/2) * s.MapConfig.CellSize,
		Y: (float64(home.Y) + float64(home.Height)/2) * s.MapConfig.CellSize,
	}

	var nearestUnassigned *protocol.BuildingTile
	bestDistance := math.Inf(1)

	for i := range s.MapConfig.Buildings {
		building := &s.MapConfig.Buildings[i]
		if building.BuildingType != "spawn-point" {
			continue
		}

		if linkedTownhallID, ok := getMetadataString(building.Metadata, "townhallId"); ok && linkedTownhallID != "" {
			if linkedTownhallID == home.ID {
				return building
			}
			continue
		}

		center := protocol.Vec2{
			X: (float64(building.X) + float64(building.Width)/2) * s.MapConfig.CellSize,
			Y: (float64(building.Y) + float64(building.Height)/2) * s.MapConfig.CellSize,
		}
		dist := distanceSquared(center.X, center.Y, homeCenter.X, homeCenter.Y)
		if dist < bestDistance {
			bestDistance = dist
			nearestUnassigned = building
		}
	}

	return nearestUnassigned
}

func (s *GameState) resolveSpawnPointTownhallLocked(spawnPoint protocol.BuildingTile, allowOccupied bool) *protocol.BuildingTile {
	if linkedTownhallID, ok := getMetadataString(spawnPoint.Metadata, "townhallId"); ok && linkedTownhallID != "" {
		building := s.getBuildingByIDLocked(linkedTownhallID)
		if building == nil || building.BuildingType != "townhall" {
			return nil
		}
		if !allowOccupied && building.Occupied {
			return nil
		}
		return building
	}

	spawnCenter := protocol.Vec2{
		X: (float64(spawnPoint.X) + float64(spawnPoint.Width)/2) * s.MapConfig.CellSize,
		Y: (float64(spawnPoint.Y) + float64(spawnPoint.Height)/2) * s.MapConfig.CellSize,
	}

	var nearest *protocol.BuildingTile
	bestDistance := math.Inf(1)
	for i := range s.MapConfig.Buildings {
		building := &s.MapConfig.Buildings[i]
		if building.BuildingType != "townhall" {
			continue
		}
		if !allowOccupied && building.Occupied {
			continue
		}

		center := protocol.Vec2{
			X: (float64(building.X) + float64(building.Width)/2) * s.MapConfig.CellSize,
			Y: (float64(building.Y) + float64(building.Height)/2) * s.MapConfig.CellSize,
		}
		dist := distanceSquared(center.X, center.Y, spawnCenter.X, spawnCenter.Y)
		if dist < bestDistance {
			bestDistance = dist
			nearest = building
		}
	}

	return nearest
}

func (s *GameState) getTownhallSpawnPositionsLocked(home protocol.BuildingTile, count int, blocked map[gridPoint]bool) []protocol.Vec2 {
	if count <= 0 {
		return nil
	}

	homeCenter := protocol.Vec2{
		X: (float64(home.X) + float64(home.Width)/2) * s.MapConfig.CellSize,
		Y: (float64(home.Y) + float64(home.Height)/2) * s.MapConfig.CellSize,
	}
	spawnOrigin := s.getTownhallSpawnOriginLocked(home)
	candidates := make([]gridPoint, 0, (home.Width+2)*(home.Height+2))
	seen := make(map[gridPoint]bool)

	for y := home.Y - 1; y <= home.Y+home.Height; y++ {
		for x := home.X - 1; x <= home.X+home.Width; x++ {
			isPerimeter := x == home.X-1 || x == home.X+home.Width || y == home.Y-1 || y == home.Y+home.Height
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

	sort.Slice(candidates, func(i, j int) bool {
		a := s.gridToWorldCenter(candidates[i])
		b := s.gridToWorldCenter(candidates[j])
		return distanceSquared(a.X, a.Y, spawnOrigin.X, spawnOrigin.Y) < distanceSquared(b.X, b.Y, spawnOrigin.X, spawnOrigin.Y)
	})

	positions := make([]protocol.Vec2, 0, minInt(count, len(candidates)))
	for _, cell := range candidates {
		if len(positions) >= count {
			break
		}

		cellCenter := s.gridToWorldCenter(cell)
		offsetX := cellCenter.X - homeCenter.X
		offsetY := cellCenter.Y - homeCenter.Y
		dist := math.Hypot(offsetX, offsetY)
		if dist > 0 {
			scale := math.Min(s.MapConfig.CellSize*0.18, dist)
			cellCenter.X += (offsetX / dist) * scale
			cellCenter.Y += (offsetY / dist) * scale
		}

		positions = append(positions, protocol.Vec2{
			X: clampFloat(cellCenter.X, unitRadius, s.MapWidth-unitRadius),
			Y: clampFloat(cellCenter.Y, unitRadius, s.MapHeight-unitRadius),
		})
	}

	return positions
}

func (s *GameState) getTownhallSpawnOriginLocked(home protocol.BuildingTile) protocol.Vec2 {
	if home.Metadata != nil {
		x, xOk := getMetadataFloat(home.Metadata, "spawnPointX")
		y, yOk := getMetadataFloat(home.Metadata, "spawnPointY")
		if xOk && yOk {
			return protocol.Vec2{
				X: clampFloat(x, unitRadius, s.MapWidth-unitRadius),
				Y: clampFloat(y, unitRadius, s.MapHeight-unitRadius),
			}
		}
	}

	return protocol.Vec2{
		X: (float64(home.X) + float64(home.Width)/2) * s.MapConfig.CellSize,
		Y: (float64(home.Y) + float64(home.Height)/2) * s.MapConfig.CellSize,
	}
}

func (s *GameState) getFallbackSpawnPositionsLocked(playerIndex, count int, blocked map[gridPoint]bool) []protocol.Vec2 {
	paddingX := 220.0
	paddingY := 220.0
	spawnBlockWidth := 260.0
	spawnBlockHeight := 220.0

	spawnsPerRow := int(math.Max(1, math.Floor((s.MapWidth-paddingX*2)/spawnBlockWidth)))
	colIndex := playerIndex % spawnsPerRow
	rowIndex := playerIndex / spawnsPerRow

	baseX := paddingX + float64(colIndex)*spawnBlockWidth
	baseY := paddingY + float64(rowIndex)*spawnBlockHeight

	baseX = math.Min(baseX, s.MapWidth-180)
	baseY = math.Min(baseY, s.MapHeight-180)

	cols := int(math.Ceil(math.Sqrt(float64(count))))
	reserved := make(map[gridPoint]bool, count)
	positions := make([]protocol.Vec2, 0, count)

	for i := 0; i < count; i++ {
		col := i % cols
		row := i / cols

		target := protocol.Vec2{
			X: baseX + float64(col)*unitFormationSpacing,
			Y: baseY + float64(row)*unitFormationSpacing,
		}

		spawnCell, ok := s.findNearestWalkableAvailable(s.worldToGrid(target.X, target.Y), blocked, reserved)
		if !ok {
			continue
		}

		reserved[spawnCell] = true
		positions = append(positions, s.clampPointToCell(target, spawnCell))
	}

	return positions
}

func (s *GameState) clampPointToCell(point protocol.Vec2, cell gridPoint) protocol.Vec2 {
	cellMinX := float64(cell.X) * s.MapConfig.CellSize
	cellMinY := float64(cell.Y) * s.MapConfig.CellSize
	cellMaxX := cellMinX + s.MapConfig.CellSize
	cellMaxY := cellMinY + s.MapConfig.CellSize

	minX := cellMinX + unitRadius
	maxX := cellMaxX - unitRadius
	minY := cellMinY + unitRadius
	maxY := cellMaxY - unitRadius

	if minX > maxX {
		minX = (cellMinX + cellMaxX) / 2
		maxX = minX
	}
	if minY > maxY {
		minY = (cellMinY + cellMaxY) / 2
		maxY = minY
	}

	return protocol.Vec2{
		X: clampFloat(point.X, minX, maxX),
		Y: clampFloat(point.Y, minY, maxY),
	}
}

func (s *GameState) buildPathEntryPoint(unit *Unit, start gridPoint) *protocol.Vec2 {
	entryPoint := s.clampPointToCell(protocol.Vec2{X: unit.X, Y: unit.Y}, start)
	if distanceSquared(unit.X, unit.Y, entryPoint.X, entryPoint.Y) < 64 {
		return nil
	}

	return &entryPoint
}

func (s *GameState) nextMovementOrderIDLocked() int64 {
	s.nextOrderID++
	return s.nextOrderID
}

func (s *GameState) resetUnitMovementLocked(unit *Unit, orderID int64) {
	unit.OrderID = orderID
	unit.Path = nil
	unit.Moving = false
	unit.TargetX = unit.X
	unit.TargetY = unit.Y
	unit.GatherTargetID = ""
	unit.GatherBuildingType = ""
	unit.ReturnTargetID = ""
	unit.MiningInside = false
	unit.MiningRemaining = 0
	unit.Gathering = false
	unit.Returning = false
	unit.BuildTargetID = ""
	unit.Building = false
	unit.AttackTargetID = 0
	unit.AttackBuildingTargetID = ""
	unit.Attacking = false
	unit.ManualMove = false
	unit.Visible = true
	unit.Status = "Idle"
	unit.CurrentTargetScore = 0
	unit.TauntedByUnitID = 0
	unit.TauntRemaining = 0
}

func (s *GameState) clearUnitGatherStateLocked(unit *Unit) {
	unit.GatherTargetID = ""
	unit.GatherBuildingType = ""
	unit.ReturnTargetID = ""
	unit.MiningInside = false
	unit.MiningRemaining = 0
	unit.Gathering = false
	unit.Returning = false
	unit.Visible = true
	unit.Status = "Idle"
}

func (s *GameState) removeBuildingByIDLocked(buildingID string) {
	delete(s.Productions, buildingID)
	filtered := make([]protocol.BuildingTile, 0, len(s.MapConfig.Buildings)-1)
	for _, b := range s.MapConfig.Buildings {
		if b.ID != buildingID {
			filtered = append(filtered, b)
		}
	}
	s.MapConfig.Buildings = filtered
}

// completeReturnDepositLocked handles a worker who is returning to deposit but the
// resource node is already depleted or gone. The worker deposits their carried load
// and then idles instead of looping back to the resource.
func (s *GameState) completeReturnDepositLocked(unit *Unit, blocked map[gridPoint]bool) {
	townhall := s.getBuildingByIDLocked(unit.ReturnTargetID)
	if townhall == nil {
		townhall = s.findOwnedTownhallLocked(unit.OwnerID)
		if townhall != nil {
			unit.ReturnTargetID = townhall.ID
		}
	}
	if townhall == nil {
		s.clearUnitGatherStateLocked(unit)
		return
	}

	if s.isUnitNearBuildingLocked(unit, *townhall, s.MapConfig.CellSize*1.5) && !unit.Moving {
		if player, ok := s.Players[unit.OwnerID]; ok && unit.CarriedAmount > 0 {
			player.Resources[unit.CarriedResourceType] += unit.CarriedAmount
		}
		unit.CarriedAmount = 0
		unit.CarriedResourceType = ""
		unit.Returning = false
		unit.Gathering = false
		if unit.GatherBuildingType == "tree" {
			s.redirectUnitToTreeLocked(unit, blocked)
		} else {
			s.clearUnitGatherStateLocked(unit)
		}
		return
	}

	if !unit.Moving {
		unitPos := &protocol.Vec2{X: unit.X, Y: unit.Y}
		approachPoints := s.getBuildingApproachPositionsLocked(*townhall, 1, blocked, unitPos)
		if len(approachPoints) > 0 {
			s.assignUnitPath(unit, approachPoints[0], blocked, nil)
		}
	}
}

func (s *GameState) getUnitByIDLocked(unitID int) *Unit {
	for _, unit := range s.Units {
		if unit.ID == unitID {
			return unit
		}
	}
	return nil
}

func (s *GameState) getBuildingByIDLocked(buildingID string) *protocol.BuildingTile {
	for i := range s.MapConfig.Buildings {
		if s.MapConfig.Buildings[i].ID == buildingID {
			return &s.MapConfig.Buildings[i]
		}
	}
	return nil
}

func (s *GameState) getUsedMeatForPlayerLocked(playerID string) int {
	used := 0
	for _, unit := range s.Units {
		if unit.OwnerID == playerID {
			if def, ok := getUnitDef(unit.UnitType); ok {
				used += def.MeatCost
			}
		}
	}
	for _, queue := range s.Productions {
		for _, prod := range queue {
			if prod.PlayerID == playerID {
				if def, ok := getUnitDef(prod.UnitType); ok {
					used += def.MeatCost
				}
			}
		}
	}
	return used
}

func (s *GameState) getMaxMeatForPlayerLocked(playerID string) int {
	total := 0
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.OwnerID == nil || *b.OwnerID != playerID {
			continue
		}
		underConstruction, _ := b.Metadata["underConstruction"].(bool)
		if underConstruction {
			continue
		}
		if def, ok := getBuildingDef(b.BuildingType); ok {
			if supply, ok := def.Metadata["foodSupply"]; ok {
				switch v := supply.(type) {
				case float64:
					total += int(v)
				case int:
					total += v
				}
			}
		}
	}
	return total
}

func (s *GameState) canAffordMeatCostLocked(playerID, unitType string) bool {
	def, ok := getUnitDef(unitType)
	if !ok {
		return true
	}
	return s.getUsedMeatForPlayerLocked(playerID)+def.MeatCost <= s.getMaxMeatForPlayerLocked(playerID)
}

func (s *GameState) getPlayerResourceStocksLocked(player *Player) []protocol.ResourceStock {
	usedMeat := s.getUsedMeatForPlayerLocked(player.ID)
	maxMeat := s.getMaxMeatForPlayerLocked(player.ID)
	return []protocol.ResourceStock{
		{ID: "gold", Label: "Gold", Amount: player.Resources["gold"], Accent: "#d4a84f"},
		{ID: "wood", Label: "Wood", Amount: player.Resources["wood"], Accent: "#7a9a52"},
		{ID: "food", Label: "Food", Amount: usedMeat, Max: &maxMeat, Accent: "#c96e43"},
	}
}

func (s *GameState) findNearestAvailableTreeLocked(excludeID string, unitX, unitY float64, blocked map[gridPoint]bool) *protocol.BuildingTile {
	var best *protocol.BuildingTile
	bestDist := math.MaxFloat64

	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.BuildingType != "tree" || b.ID == excludeID {
			continue
		}
		if b.ResourceAmount <= 0 {
			continue
		}
		if s.countWorkersInsideBuildingLocked(b.ID) >= treeWorkerCap {
			continue
		}
		if len(s.getBuildingApproachPositionsLocked(*b, 1, blocked, nil)) == 0 {
			continue
		}
		centerX := (float64(b.X) + float64(b.Width)/2) * s.MapConfig.CellSize
		centerY := (float64(b.Y) + float64(b.Height)/2) * s.MapConfig.CellSize
		dist := distanceSquared(unitX, unitY, centerX, centerY)
		if dist < bestDist {
			bestDist = dist
			best = b
		}
	}

	return best
}

func (s *GameState) redirectUnitToTreeLocked(unit *Unit, blocked map[gridPoint]bool) {
	next := s.findNearestAvailableTreeLocked(unit.GatherTargetID, unit.X, unit.Y, blocked)
	if next == nil {
		s.clearUnitGatherStateLocked(unit)
		return
	}

	unit.GatherTargetID = next.ID
	unit.GatherBuildingType = "tree"
	unit.ReturnTargetID = ""
	unit.Returning = false
	unit.Gathering = false
	unit.MiningInside = false
	unit.MiningRemaining = 0
	unit.Status = "Heading To Tree"

	unitPos := &protocol.Vec2{X: unit.X, Y: unit.Y}
	approachPoints := s.getBuildingApproachPositionsLocked(*next, 1, blocked, unitPos)
	if len(approachPoints) > 0 {
		s.assignUnitPath(unit, approachPoints[0], blocked, nil)
	}
}

func (s *GameState) updateWorkerBuildStateLocked(unit *Unit) {
	if unit.Moving || unit.Building {
		return
	}
	building := s.getBuildingByIDLocked(unit.BuildTargetID)
	if building == nil {
		unit.BuildTargetID = ""
		unit.Status = "Idle"
		return
	}
	unit.Building = true
	unit.Status = "Building"
}

const maxBuildersPerBuilding = 3

func getBuildingHP(building *protocol.BuildingTile) (hp, maxHp float64, ok bool) {
	if building.Metadata == nil {
		return 0, 0, false
	}
	h, hOk := building.Metadata["hp"].(float64)
	m, mOk := building.Metadata["maxHp"].(float64)
	if !hOk || !mOk || m <= 0 {
		return 0, 0, false
	}
	return h, m, true
}

func (s *GameState) tickBuildingRepairsLocked(dt float64) {
	for i := range s.MapConfig.Buildings {
		building := &s.MapConfig.Buildings[i]

		hp, maxHp, ok := getBuildingHP(building)
		if !ok || hp >= maxHp {
			continue
		}

		builderCount := 0
		for _, unit := range s.Units {
			if unit.BuildTargetID == building.ID && unit.Building {
				builderCount++
			}
		}
		if builderCount == 0 {
			building.Metadata["builderCount"] = 0
			continue
		}

		building.Metadata["builderCount"] = builderCount

		hpPerSecond := maxHp / 15.0 // fallback: match original barracks rate
		if v, ok := building.Metadata["hpPerSecond"]; ok {
			if f, ok := v.(float64); ok && f > 0 {
				hpPerSecond = f
			}
		}
		newHp := math.Min(maxHp, hp+hpPerSecond*float64(builderCount)*dt)
		building.Metadata["hp"] = newHp

		if newHp >= maxHp {
			// Building complete / fully repaired
			delete(building.Metadata, "underConstruction")
			delete(building.Metadata, "builderCount")
			for _, unit := range s.Units {
				if unit.BuildTargetID == building.ID {
					unit.BuildTargetID = ""
					unit.Building = false
					unit.Status = "Idle"
				}
			}
		}
	}
}

func (s *GameState) tickBuildingCombatLocked(dt float64) {
	var deadUnitIDs []int

	for i := range s.MapConfig.Buildings {
		building := &s.MapConfig.Buildings[i]
		if !building.Visible || building.OwnerID == nil {
			continue
		}
		if building.Metadata != nil && building.Metadata["underConstruction"] == true {
			continue
		}

		def, ok := getBuildingDef(building.BuildingType)
		if !ok || def.Damage <= 0 || def.AttackRange <= 0 || def.AttackSpeed <= 0 {
			continue
		}

		if building.Metadata == nil {
			building.Metadata = map[string]interface{}{}
		}

		cooldown, _ := getMetadataFloat(building.Metadata, "attackCooldown")
		if cooldown > 0 {
			cooldown = math.Max(0, cooldown-dt)
			building.Metadata["attackCooldown"] = cooldown
		}

		target := s.findNearestHostileUnitForBuildingLocked(building, *building.OwnerID, def.AttackRange)
		if target == nil || cooldown > 0 {
			continue
		}

		// Route through the shared helper so shield (blood_engine) absorbs first.
		s.applyUnitDamageLocked(target, def.Damage)
		building.Metadata["attackCooldown"] = 1.0 / def.AttackSpeed
		if target.HP <= 0 {
			target.HP = 0
			deadUnitIDs = append(deadUnitIDs, target.ID)
		}
	}

	for _, id := range deadUnitIDs {
		s.removeUnitLocked(id)
	}
}

func (s *GameState) RepairBuilding(playerID string, unitIDs []int, buildingID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	building := s.getBuildingByIDLocked(buildingID)
	if building == nil || building.OwnerID == nil || *building.OwnerID != playerID {
		return
	}

	hp, maxHp, ok := getBuildingHP(building)
	if !ok || hp >= maxHp {
		return // no HP pool or already at full health
	}

	// Count existing builders not in the incoming unit list
	unitIDSet := make(map[int]bool, len(unitIDs))
	for _, id := range unitIDs {
		unitIDSet[id] = true
	}
	existingBuilders := 0
	for _, unit := range s.Units {
		if unit.BuildTargetID == buildingID && !unitIDSet[unit.ID] {
			existingBuilders++
		}
	}

	blocked := s.buildBlockedCells()
	orderID := s.nextMovementOrderIDLocked()

	added := 0
	for _, unitID := range unitIDs {
		if existingBuilders+added >= maxBuildersPerBuilding {
			break
		}
		unit := s.getUnitByIDLocked(unitID)
		if unit == nil || unit.OwnerID != playerID || unit.UnitType != "worker" {
			continue
		}
		unit.GatherTargetID = ""
		unit.MiningInside = false
		unit.Building = false

		approachPoints := s.getBuildingApproachPositionsLocked(*building, 1, blocked, &protocol.Vec2{X: unit.X, Y: unit.Y})
		if len(approachPoints) == 0 {
			continue
		}
		s.resetUnitMovementLocked(unit, orderID)
		unit.BuildTargetID = buildingID
		s.assignUnitPath(unit, approachPoints[0], blocked, nil)
		added++
	}
}

func (s *GameState) updateWorkerTaskLocked(unit *Unit, dt float64, blocked map[gridPoint]bool) {
	if !unitHasCapability(unit.UnitType, "gather") {
		return
	}

	if unit.AttackTargetID != 0 {
		return
	}

	if unit.BuildTargetID != "" {
		s.updateWorkerBuildStateLocked(unit)
		return
	}

	if unit.GatherTargetID == "" {
		return
	}

	resourceNode := s.getBuildingByIDLocked(unit.GatherTargetID)
	nodeAlive := resourceNode != nil && resourceNode.ResourceAmount > 0

	if !nodeAlive {
		if unit.Returning && unit.CarriedAmount > 0 {
			// Node is gone but the worker has resources to deposit.
			// completeReturnDepositLocked will redirect to a new tree afterwards
			// (if GatherBuildingType is "tree") rather than idling.
			s.completeReturnDepositLocked(unit, blocked)
		} else if unit.GatherBuildingType == "tree" {
			s.redirectUnitToTreeLocked(unit, blocked)
		} else {
			s.clearUnitGatherStateLocked(unit)
		}
		return
	}

	isTree := resourceNode.BuildingType == "tree"

	if unit.MiningInside {
		if isTree {
			unit.Status = "Chopping Wood"
		} else {
			unit.Status = "Mining Gold"
		}
		unit.MiningRemaining -= dt
		if unit.MiningRemaining > 0 {
			return
		}

		unit.MiningInside = false
		unit.Gathering = false
		unit.Visible = true
		gathered := minInt(gatherAmountForUnitResource(unit.UnitType, resourceNode.ResourceType), resourceNode.ResourceAmount)
		if gathered > 0 {
			unit.CarriedResourceType = resourceNode.ResourceType
			unit.CarriedAmount = gathered
			resourceNode.ResourceAmount -= gathered
		}

		if !isTree {
			if exitPoint := s.getUnitExitPositionForBuildingLocked(*resourceNode, unit); exitPoint != nil {
				unit.X = exitPoint.X
				unit.Y = exitPoint.Y
				unit.TargetX = exitPoint.X
				unit.TargetY = exitPoint.Y
			}
		}

		// Remove the building once its resource pool is empty.
		if resourceNode.ResourceAmount <= 0 {
			s.removeBuildingByIDLocked(resourceNode.ID)
		}

		s.sendWorkerToDepositLocked(unit, blocked)
		return
	}

	if unit.Returning {
		if isTree {
			unit.Status = "Returning Wood"
		} else {
			unit.Status = "Returning Gold"
		}
		townhall := s.getBuildingByIDLocked(unit.ReturnTargetID)
		if townhall == nil {
			townhall = s.findOwnedTownhallLocked(unit.OwnerID)
			if townhall != nil {
				unit.ReturnTargetID = townhall.ID
			}
		}
		if townhall == nil {
			unit.Returning = false
			return
		}

		if s.isUnitNearBuildingLocked(unit, *townhall, s.MapConfig.CellSize*1.5) && !unit.Moving {
			if player, ok := s.Players[unit.OwnerID]; ok && unit.CarriedResourceType != "" && unit.CarriedAmount > 0 {
				player.Resources[unit.CarriedResourceType] += unit.CarriedAmount
			}
			unit.CarriedAmount = 0
			unit.CarriedResourceType = ""
			unit.Returning = false
			unit.Gathering = false

			// Re-check the node; another worker may have depleted it this tick.
			liveNode := s.getBuildingByIDLocked(unit.GatherTargetID)
			if liveNode == nil || liveNode.ResourceAmount <= 0 {
				if isTree {
					s.redirectUnitToTreeLocked(unit, blocked)
				} else {
					s.clearUnitGatherStateLocked(unit)
				}
				return
			}

			if isTree {
				unit.Status = "Returning To Tree"
			} else {
				unit.Status = "Returning To Mine"
			}

			unitPos := &protocol.Vec2{X: unit.X, Y: unit.Y}
			approachPoints := s.getBuildingApproachPositionsLocked(*liveNode, 1, blocked, unitPos)
			if len(approachPoints) > 0 {
				s.assignUnitPath(unit, approachPoints[0], blocked, nil)
			}
			return
		}

		if !unit.Moving {
			unitPos := &protocol.Vec2{X: unit.X, Y: unit.Y}
			approachPoints := s.getBuildingApproachPositionsLocked(*townhall, 1, blocked, unitPos)
			if len(approachPoints) > 0 {
				s.assignUnitPath(unit, approachPoints[0], blocked, nil)
			}
		}
		return
	}

	if !s.isUnitNearBuildingLocked(unit, *resourceNode, s.MapConfig.CellSize*1.5) {
		if isTree {
			unit.Status = "Heading To Tree"
		} else {
			unit.Status = "Heading To Mine"
		}
		if !unit.Moving {
			unitPos := &protocol.Vec2{X: unit.X, Y: unit.Y}
			approachPoints := s.getBuildingApproachPositionsLocked(*resourceNode, 1, blocked, unitPos)
			if len(approachPoints) > 0 {
				s.assignUnitPath(unit, approachPoints[0], blocked, nil)
			}
		}
		return
	}

	if unit.Moving {
		if isTree {
			unit.Status = "Heading To Tree"
		} else {
			unit.Status = "Heading To Mine"
		}
		return
	}

	workerCap := goldmineWorkerCap
	if isTree {
		workerCap = treeWorkerCap
	}
	if s.countWorkersInsideBuildingLocked(resourceNode.ID) >= workerCap {
		if isTree {
			s.redirectUnitToTreeLocked(unit, blocked)
		} else {
			unit.Status = "Waiting For Mine Slot"
		}
		return
	}

	choppingDuration := goldmineMiningSeconds
	if isTree {
		choppingDuration = treeChoppingSeconds
	}
	unit.Gathering = true
	unit.MiningInside = true
	unit.MiningRemaining = choppingDuration
	if !isTree {
		unit.Visible = false
	}
	unit.Moving = false
	unit.Path = nil
	if isTree {
		unit.Status = "Chopping Wood"
	} else {
		unit.Status = "Mining Gold"
	}
}

func (s *GameState) sendWorkerToDepositLocked(unit *Unit, blocked map[gridPoint]bool) {
	townhall := s.findOwnedTownhallLocked(unit.OwnerID)
	if townhall == nil {
		unit.Status = "Idle"
		return
	}

	unit.ReturnTargetID = townhall.ID
	unit.Returning = true
	unit.Gathering = false
	if unit.CarriedResourceType == "wood" {
		unit.Status = "Returning Wood"
	} else {
		unit.Status = "Returning Gold"
	}

	unitPos := &protocol.Vec2{X: unit.X, Y: unit.Y}
	approachPoints := s.getBuildingApproachPositionsLocked(*townhall, 1, blocked, unitPos)
	if len(approachPoints) > 0 {
		s.assignUnitPath(unit, approachPoints[0], blocked, nil)
	}
}

func gatherAmountForUnitResource(unitType, resourceType string) int {
	def, ok := getUnitDef(unitType)
	if !ok {
		return defaultGatherAmountForResource(resourceType)
	}

	switch resourceType {
	case "gold":
		if def.GoldGatherAmount > 0 {
			return def.GoldGatherAmount
		}
	case "wood":
		if def.WoodGatherAmount > 0 {
			return def.WoodGatherAmount
		}
	}

	return defaultGatherAmountForResource(resourceType)
}

func defaultGatherAmountForResource(resourceType string) int {
	switch resourceType {
	case "gold":
		return defaultGoldGatherAmount
	case "wood":
		return defaultWoodGatherAmount
	default:
		return defaultWoodGatherAmount
	}
}

func unitHasCapability(unitType, capability string) bool {
	def, ok := getUnitDef(unitType)
	return ok && containsString(def.Capabilities, capability)
}

func (s *GameState) findNearestHostileUnitForBuildingLocked(building *protocol.BuildingTile, ownerID string, attackRange float64) *Unit {
	var best *Unit
	bestDistSq := attackRange * attackRange

	for _, unit := range s.Units {
		if !unit.Visible || unit.HP <= 0 || unit.OwnerID == ownerID {
			continue
		}

		dist := s.distanceToBuilding(unit.X, unit.Y, building)
		distSq := dist * dist
		if distSq > bestDistSq {
			continue
		}

		best = unit
		bestDistSq = distSq
	}

	return best
}

func (s *GameState) findOwnedTownhallLocked(ownerID string) *protocol.BuildingTile {
	for i := range s.MapConfig.Buildings {
		building := &s.MapConfig.Buildings[i]
		if building.BuildingType == "townhall" && building.Visible && building.OwnerID != nil && *building.OwnerID == ownerID {
			return building
		}
	}
	return nil
}

func (s *GameState) isUnitNearBuildingLocked(unit *Unit, building protocol.BuildingTile, padding float64) bool {
	left := float64(building.X) * s.MapConfig.CellSize
	top := float64(building.Y) * s.MapConfig.CellSize
	right := left + float64(building.Width)*s.MapConfig.CellSize
	bottom := top + float64(building.Height)*s.MapConfig.CellSize
	return unit.X >= left-padding && unit.X <= right+padding && unit.Y >= top-padding && unit.Y <= bottom+padding
}

func (s *GameState) getBuildingApproachPositionsLocked(building protocol.BuildingTile, count int, blocked map[gridPoint]bool, origin *protocol.Vec2) []protocol.Vec2 {
	if count <= 0 {
		return nil
	}

	candidates := make([]gridPoint, 0, (building.Width+2)*(building.Height+2))
	seen := make(map[gridPoint]bool)

	sortOrigin := protocol.Vec2{
		X: (float64(building.X) + float64(building.Width)/2) * s.MapConfig.CellSize,
		Y: (float64(building.Y) + float64(building.Height)/2) * s.MapConfig.CellSize,
	}
	if origin != nil {
		sortOrigin = *origin
	}

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

	sort.Slice(candidates, func(i, j int) bool {
		a := s.gridToWorldCenter(candidates[i])
		b := s.gridToWorldCenter(candidates[j])
		return distanceSquared(a.X, a.Y, sortOrigin.X, sortOrigin.Y) < distanceSquared(b.X, b.Y, sortOrigin.X, sortOrigin.Y)
	})

	positions := make([]protocol.Vec2, 0, minInt(count, len(candidates)))
	for _, cell := range candidates {
		positions = append(positions, s.gridToWorldCenter(cell))
		if len(positions) >= count {
			break
		}
	}

	return positions
}

func (s *GameState) getUnitExitPositionForBuildingLocked(building protocol.BuildingTile, unit *Unit) *protocol.Vec2 {
	unitPos := &protocol.Vec2{X: unit.X, Y: unit.Y}
	positions := s.getBuildingApproachPositionsLocked(building, 1, s.buildBlockedCells(), unitPos)
	if len(positions) == 0 {
		return nil
	}
	position := positions[0]
	return &position
}

func (s *GameState) countWorkersInsideBuildingLocked(buildingID string) int {
	count := 0
	for _, unit := range s.Units {
		if unit.MiningInside && unit.GatherTargetID == buildingID {
			count++
		}
	}
	return count
}

func (s *GameState) refreshBuildingRuntimeMetadataLocked() {
	for i := range s.MapConfig.Buildings {
		building := &s.MapConfig.Buildings[i]
		if building.Metadata == nil {
			building.Metadata = map[string]interface{}{}
		}

		if building.BuildingType == "goldmine" {
			building.Metadata["currentWorkers"] = s.countWorkersInsideBuildingLocked(building.ID)
			building.Metadata["maxWorkers"] = goldmineWorkerCap
		}
		if building.BuildingType == "tree" {
			building.Metadata["currentWorkers"] = s.countWorkersInsideBuildingLocked(building.ID)
			building.Metadata["maxWorkers"] = treeWorkerCap
		}

		if queue := s.Productions[building.ID]; len(queue) > 0 {
			activeProduction := queue[0]
			building.Metadata["producingUnitType"] = activeProduction.UnitType
			building.Metadata["productionRemainingSeconds"] = activeProduction.RemainingSeconds
			building.Metadata["productionTotalSeconds"] = activeProduction.TotalSeconds
			building.Metadata["productionQueueLength"] = len(queue)
			building.Metadata["queuedUnitTypes"] = joinProductionUnitTypes(queue)
		} else {
			delete(building.Metadata, "producingUnitType")
			delete(building.Metadata, "productionRemainingSeconds")
			delete(building.Metadata, "productionTotalSeconds")
			delete(building.Metadata, "productionQueueLength")
			delete(building.Metadata, "queuedUnitTypes")
		}

		if building.BuildingType == "enemy-spawnpoint" {
			if timer, exists := s.EnemySpawnTimers[building.ID]; exists {
				if timer.RemainingDelay > 0 {
					building.Metadata["spawnTimerRemaining"] = timer.RemainingDelay
					building.Metadata["spawnTimerTotal"] = timer.TotalDelay
					building.Metadata["spawnTimerPhase"] = "delay"
				} else {
					building.Metadata["spawnTimerRemaining"] = timer.RemainingInterval
					building.Metadata["spawnTimerTotal"] = timer.TotalInterval
					building.Metadata["spawnTimerPhase"] = "interval"
				}
			}
		}
	}
}

func (s *GameState) beginUnitProductionLocked(player *Player, building protocol.BuildingTile, unitType string) {
	spawnSeconds := s.getEffectiveUnitSpawnSecondsLocked(player, building, unitType)
	s.Productions[building.ID] = append(s.Productions[building.ID], &UnitProduction{
		PlayerID:         player.ID,
		UnitType:         unitType,
		RemainingSeconds: spawnSeconds,
		TotalSeconds:     spawnSeconds,
	})
}

func (s *GameState) updateUnitProductionsLocked(dt float64) {
	if len(s.Productions) == 0 {
		return
	}

	completed := make([]string, 0, len(s.Productions))

	for buildingID, queue := range s.Productions {
		if len(queue) == 0 {
			completed = append(completed, buildingID)
			continue
		}

		production := queue[0]
		production.RemainingSeconds = math.Max(0, production.RemainingSeconds-dt)
		if production.RemainingSeconds <= 0 {
			completed = append(completed, buildingID)
		}
	}

	for _, buildingID := range completed {
		s.completeUnitProductionLocked(buildingID)
	}
}

func (s *GameState) completeUnitProductionLocked(buildingID string) {
	queue, ok := s.Productions[buildingID]
	if !ok || len(queue) == 0 {
		delete(s.Productions, buildingID)
		return
	}
	production := queue[0]

	building := s.getBuildingByIDLocked(buildingID)
	if building == nil || !building.Visible || building.OwnerID == nil || *building.OwnerID != production.PlayerID {
		s.consumeProductionQueueItemLocked(buildingID)
		return
	}

	player, ok := s.Players[production.PlayerID]
	if !ok {
		s.consumeProductionQueueItemLocked(buildingID)
		return
	}

	spawnPosition, ok := s.getProductionSpawnPositionLocked(*building)
	if !ok {
		return
	}

	unit := s.spawnPlayerUnitLocked(production.UnitType, production.PlayerID, player.Color, spawnPosition)
	if unit != nil {
		rallyPoint := s.getTownhallSpawnOriginLocked(*building)
		if distanceSquared(unit.X, unit.Y, rallyPoint.X, rallyPoint.Y) > unitRadius*unitRadius {
			unit.Status = "Moving To Spawn Point"
			s.assignUnitPath(unit, rallyPoint, s.buildBlockedCells(), nil)
		}
	}

	s.consumeProductionQueueItemLocked(buildingID)
}

func (s *GameState) getProductionSpawnPositionLocked(building protocol.BuildingTile) (protocol.Vec2, bool) {
	blocked := s.buildBlockedCells()
	spawnPositions := s.getTownhallSpawnPositionsLocked(building, 1, blocked)
	if len(spawnPositions) > 0 {
		return spawnPositions[0], true
	}

	rallyPoint := s.getTownhallSpawnOriginLocked(building)
	spawnCell, ok := s.findNearestWalkable(s.worldToGrid(rallyPoint.X, rallyPoint.Y), blocked)
	if !ok {
		return protocol.Vec2{}, false
	}

	return s.clampPointToCell(rallyPoint, spawnCell), true
}

func (s *GameState) getEffectiveUnitSpawnSecondsLocked(player *Player, building protocol.BuildingTile, unitType string) float64 {
	spawnSeconds := 1.0
	if def, ok := getUnitDef(unitType); ok && def.SpawnSeconds > 0 {
		spawnSeconds = def.SpawnSeconds
	}

	if building.Metadata != nil {
		if multiplier, ok := getMetadataFloat(building.Metadata, "spawnTimeMultiplier"); ok && multiplier > 0 {
			spawnSeconds *= multiplier
		}
		if multiplier, ok := getMetadataFloat(building.Metadata, "spawnTime"+formatMetadataUnitTypeSuffix(unitType)+"Multiplier"); ok && multiplier > 0 {
			spawnSeconds *= multiplier
		}
	}

	if player.GlobalUnitSpawnTimeMultiplier > 0 {
		spawnSeconds *= player.GlobalUnitSpawnTimeMultiplier
	}
	if multiplier, ok := player.UnitSpawnTimeMultipliers[unitType]; ok && multiplier > 0 {
		spawnSeconds *= multiplier
	}

	return math.Max(minUnitSpawnSeconds, spawnSeconds)
}

func (s *GameState) CanAffordUnit(playerID, unitType string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	player, ok := s.Players[playerID]
	if !ok {
		return false
	}
	return s.canAffordUnitCostLocked(player, unitType) && s.canAffordMeatCostLocked(playerID, unitType)
}

func (s *GameState) CanAffordBuilding(playerID, buildingType string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	player, ok := s.Players[playerID]
	if !ok {
		return false
	}
	def, ok := getBuildingDef(buildingType)
	if !ok {
		return true
	}
	for resource, cost := range def.ResourceCost {
		if player.Resources[resource] < cost {
			return false
		}
	}
	return true
}

func (s *GameState) canAffordUnitCostLocked(player *Player, unitType string) bool {
	def, ok := getUnitDef(unitType)
	if !ok {
		return false
	}
	for resourceID, amount := range def.ResourceCost {
		if player.Resources[resourceID] < amount {
			return false
		}
	}
	return true
}

func (s *GameState) payUnitCostLocked(player *Player, unitType string) {
	def, ok := getUnitDef(unitType)
	if !ok {
		return
	}
	for resourceID, amount := range def.ResourceCost {
		player.Resources[resourceID] -= amount
	}
}

func (s *GameState) refundUnitCostLocked(player *Player, unitType string) {
	def, ok := getUnitDef(unitType)
	if !ok {
		return
	}
	for resourceID, amount := range def.ResourceCost {
		player.Resources[resourceID] += amount
	}
}

func (s *GameState) consumeProductionQueueItemLocked(buildingID string) {
	queue := s.Productions[buildingID]
	if len(queue) <= 1 {
		delete(s.Productions, buildingID)
		return
	}

	s.Productions[buildingID] = queue[1:]
}

func joinProductionUnitTypes(queue []*UnitProduction) string {
	unitTypes := make([]string, 0, len(queue))
	for _, production := range queue {
		unitTypes = append(unitTypes, production.UnitType)
	}

	return strings.Join(unitTypes, ",")
}

func (s *GameState) applyUnitSeparationLocked(blocked map[gridPoint]bool) {
	minDistance := unitSeparationDistance
	minDistanceSq := minDistance * minDistance

	for i := 0; i < len(s.Units); i++ {
		for j := i + 1; j < len(s.Units); j++ {
			a := s.Units[i]
			b := s.Units[j]
			dx := b.X - a.X
			dy := b.Y - a.Y
			distSq := dx*dx + dy*dy

			if a.Moving && b.Moving && a.OrderID != 0 && a.OrderID == b.OrderID {
				continue
			}

			if distSq >= minDistanceSq {
				continue
			}

			engagedMelee := s.unitsAreInMutualMeleeLocked(a, b)

			dist := math.Sqrt(distSq)
			if dist < 0.001 {
				angle := float64((a.ID+b.ID)%16) * (math.Pi / 8)
				dx = math.Cos(angle)
				dy = math.Sin(angle)
				dist = 1
			}

			overlapScale := 0.5
			if a.Moving || b.Moving {
				overlapScale = 0.18
			}
			if engagedMelee {
				// Let melee units stay in contact once they've committed to each other.
				// Strong separation here creates the visible "staggering" loop where
				// combatants are pushed out of range and then immediately step back in.
				overlapScale = 0.05
			}

			overlap := (minDistance - dist) * overlapScale
			pushX := (dx / dist) * overlap
			pushY := (dy / dist) * overlap

			s.tryMoveUnitByOffsetLocked(a, -pushX, -pushY, blocked)
			s.tryMoveUnitByOffsetLocked(b, pushX, pushY, blocked)
		}
	}
}

func (s *GameState) tryMoveUnitByOffsetLocked(unit *Unit, offsetX, offsetY float64, blocked map[gridPoint]bool) {
	nextX := clampFloat(unit.X+offsetX, unitRadius, s.MapWidth-unitRadius)
	nextY := clampFloat(unit.Y+offsetY, unitRadius, s.MapHeight-unitRadius)
	if !s.isWalkable(s.worldToGrid(nextX, nextY), blocked) {
		return
	}

	unit.X = nextX
	unit.Y = nextY
}

func (s *GameState) unitsAreInMutualMeleeLocked(a, b *Unit) bool {
	if a == nil || b == nil {
		return false
	}
	if a.OwnerID == b.OwnerID {
		return false
	}
	aProfile := resolveCombatProfile(a)
	bProfile := resolveCombatProfile(b)
	if !aProfile.Melee || !bProfile.Melee {
		return false
	}
	if a.AttackTargetID != b.ID && b.AttackTargetID != a.ID {
		return false
	}
	const meleeContactPadding = 8.0
	aRange := math.Max(a.AttackRange, unitRadius+meleeContactPadding)
	bRange := math.Max(b.AttackRange, unitRadius+meleeContactPadding)
	return distanceSquared(a.X, a.Y, b.X, b.Y) <= aRange*aRange || distanceSquared(a.X, a.Y, b.X, b.Y) <= bRange*bRange
}

func (s *GameState) randomColor() string {
	palette := []string{
		"#3498db",
		"#2ecc71",
		"#f1c40f",
		"#9b59b6",
		"#e67e22",
		"#1abc9c",
		"#ec4899",
	}

	used := make(map[string]bool)
	for _, player := range s.Players {
		used[player.Color] = true
	}

	available := make([]string, 0, len(palette))
	for _, color := range palette {
		if !used[color] {
			available = append(available, color)
		}
	}

	if len(available) > 0 {
		return available[s.rng.Intn(len(available))]
	}

	return palette[s.rng.Intn(len(palette))]
}

func clampFloat(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func distanceSquared(ax, ay, bx, by float64) float64 {
	dx := ax - bx
	dy := ay - by
	return dx*dx + dy*dy
}

func simplifyLeadingWaypoints(unit *Unit, path []protocol.Vec2, finalTarget protocol.Vec2) []protocol.Vec2 {
	for len(path) > 1 {
		first := path[0]
		second := path[1]
		toFinalX := finalTarget.X - unit.X
		toFinalY := finalTarget.Y - unit.Y
		toFirstX := first.X - unit.X
		toFirstY := first.Y - unit.Y
		toSecondX := second.X - unit.X
		toSecondY := second.Y - unit.Y

		if dotProduct(toFirstX, toFirstY, toFinalX, toFinalY) < 0 && dotProduct(toSecondX, toSecondY, toFinalX, toFinalY) >= 0 {
			path = path[1:]
			continue
		}

		if distanceSquared(unit.X, unit.Y, first.X, first.Y) < 100 {
			path = path[1:]
			continue
		}

		break
	}

	return path
}

func dotProduct(ax, ay, bx, by float64) float64 {
	return ax*bx + ay*by
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func formatMetadataUnitTypeSuffix(unitType string) string {
	if unitType == "" {
		return ""
	}

	first := unitType[:1]
	if first >= "a" && first <= "z" {
		first = strings.ToUpper(first)
	}

	return first + unitType[1:]
}

func getMetadataFloat(metadata map[string]interface{}, key string) (float64, bool) {
	if metadata == nil {
		return 0, false
	}

	value, ok := metadata[key]
	if !ok {
		return 0, false
	}

	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	default:
		return 0, false
	}
}

func getMetadataString(metadata map[string]interface{}, key string) (string, bool) {
	if metadata == nil {
		return "", false
	}

	value, ok := metadata[key]
	if !ok {
		return "", false
	}

	typed, ok := value.(string)
	if !ok {
		return "", false
	}

	return typed, true
}

func buildFormationTargets(units []*Unit, anchor protocol.Vec2, spacing float64) []protocol.Vec2 {
	count := len(units)
	if count == 0 {
		return nil
	}
	if count == 1 {
		return []protocol.Vec2{anchor}
	}

	center := averageUnitPosition(units)
	forwardX := anchor.X - center.X
	forwardY := anchor.Y - center.Y
	forwardLength := math.Hypot(forwardX, forwardY)

	if forwardLength < 0.001 {
		forwardX, forwardY = 0, 1
		forwardLength = 1
	}

	forwardX /= forwardLength
	forwardY /= forwardLength
	rightX := forwardY
	rightY := -forwardX

	cols := int(math.Ceil(math.Sqrt(float64(count))))
	rows := int(math.Ceil(float64(count) / float64(cols)))
	totalWidth := float64(cols-1) * spacing
	totalHeight := float64(rows-1) * spacing
	slots := make([]protocol.Vec2, 0, count)

	for i := 0; i < count; i++ {
		col := i % cols
		row := i / cols
		rightOffset := float64(col)*spacing - totalWidth/2
		forwardOffset := float64(row)*spacing - totalHeight/2

		slots = append(slots, protocol.Vec2{
			X: anchor.X + rightX*rightOffset + forwardX*forwardOffset,
			Y: anchor.Y + rightY*rightOffset + forwardY*forwardOffset,
		})
	}

	type formationIndex struct {
		index   int
		right   float64
		forward float64
	}

	unitOrder := make([]formationIndex, 0, count)
	for index, unit := range units {
		relativeX := unit.X - center.X
		relativeY := unit.Y - center.Y
		unitOrder = append(unitOrder, formationIndex{
			index:   index,
			right:   relativeX*rightX + relativeY*rightY,
			forward: relativeX*forwardX + relativeY*forwardY,
		})
	}

	slotOrder := make([]formationIndex, 0, count)
	for index, slot := range slots {
		relativeX := slot.X - anchor.X
		relativeY := slot.Y - anchor.Y
		slotOrder = append(slotOrder, formationIndex{
			index:   index,
			right:   relativeX*rightX + relativeY*rightY,
			forward: relativeX*forwardX + relativeY*forwardY,
		})
	}

	sort.Slice(unitOrder, func(i, j int) bool {
		if math.Abs(unitOrder[i].forward-unitOrder[j].forward) > 8 {
			return unitOrder[i].forward < unitOrder[j].forward
		}
		return unitOrder[i].right < unitOrder[j].right
	})

	sort.Slice(slotOrder, func(i, j int) bool {
		if math.Abs(slotOrder[i].forward-slotOrder[j].forward) > 8 {
			return slotOrder[i].forward < slotOrder[j].forward
		}
		return slotOrder[i].right < slotOrder[j].right
	})

	targets := make([]protocol.Vec2, count)
	for i := 0; i < count; i++ {
		targets[unitOrder[i].index] = slots[slotOrder[i].index]
	}

	return targets
}

func averageUnitPosition(units []*Unit) protocol.Vec2 {
	if len(units) == 0 {
		return protocol.Vec2{}
	}

	var totalX float64
	var totalY float64

	for _, unit := range units {
		totalX += unit.X
		totalY += unit.Y
	}

	return protocol.Vec2{
		X: totalX / float64(len(units)),
		Y: totalY / float64(len(units)),
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
			}
		}

		s.ensureEnemyPlayerLocked()

		timer, exists := s.EnemySpawnTimers[building.ID]
		if !exists {
			delay := 60.0
			interval := 10.0
			if building.Metadata != nil {
				if v, ok := getMetadataFloat(building.Metadata, "spawnDelaySeconds"); ok && v >= 0 {
					delay = v
				}
				if v, ok := getMetadataFloat(building.Metadata, "spawnIntervalSeconds"); ok && v > 0 {
					interval = v
				}
			}
			timer = &EnemySpawnTimer{
				RemainingDelay:    delay,
				TotalDelay:        delay,
				RemainingInterval: 0,
				TotalInterval:     interval,
			}
			s.EnemySpawnTimers[building.ID] = timer
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
		if building.Metadata != nil {
			if v, ok := getMetadataFloat(building.Metadata, "spawnCount"); ok && v >= 1 {
				spawnCount = int(v)
			}
			if v, ok := building.Metadata["unitType"].(string); ok && v != "" {
				unitType = v
			}
		}

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
			unit.OrderID = orderID
			unit.Status = "Advancing"

			target := s.getNearestPlayerTownhallCenterLocked(spawnPos.X, spawnPos.Y)
			if target != nil {
				s.assignUnitPath(unit, *target, blocked, nil)
			}
		}
	}
}

func (s *GameState) buildingCenterLocked(building *protocol.BuildingTile) protocol.Vec2 {
	return protocol.Vec2{
		X: (float64(building.X) + float64(building.Width)/2) * s.MapConfig.CellSize,
		Y: (float64(building.Y) + float64(building.Height)/2) * s.MapConfig.CellSize,
	}
}

// distanceToBuilding returns the distance from a point to the nearest edge of
// a building's world-space bounding box (0 if the point is inside the box).
func (s *GameState) distanceToBuilding(x, y float64, building *protocol.BuildingTile) float64 {
	cs := s.MapConfig.CellSize
	left := float64(building.X) * cs
	top := float64(building.Y) * cs
	right := float64(building.X+building.Width) * cs
	bottom := float64(building.Y+building.Height) * cs

	cx := clampFloat(x, left, right)
	cy := clampFloat(y, top, bottom)
	dx := x - cx
	dy := y - cy
	return math.Sqrt(dx*dx + dy*dy)
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

func (s *GameState) destroyBuildingLocked(buildingID string) {
	// Clear any enemy attack references to this building
	for _, unit := range s.Units {
		if unit.AttackBuildingTargetID == buildingID {
			unit.AttackBuildingTargetID = ""
			unit.Attacking = false
			unit.Status = "Idle"
		}
	}
	// Drop any lingering banked damage-XP entry. The combat path pays out
	// before queuing destruction, so this is only defensive.
	delete(s.buildingDamageDealt, buildingID)

	// Remove the building from the map
	filtered := make([]protocol.BuildingTile, 0, len(s.MapConfig.Buildings))
	for _, b := range s.MapConfig.Buildings {
		if b.ID != buildingID {
			filtered = append(filtered, b)
		}
	}
	s.MapConfig.Buildings = filtered
}

func (s *GameState) getNearestPlayerTownhallCenterLocked(x, y float64) *protocol.Vec2 {
	var best *protocol.Vec2
	bestDistSq := math.MaxFloat64

	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.BuildingType != "townhall" || !b.Occupied || !b.Visible {
			continue
		}
		cx := (float64(b.X) + float64(b.Width)/2) * s.MapConfig.CellSize
		cy := (float64(b.Y) + float64(b.Height)/2) * s.MapConfig.CellSize
		distSq := distanceSquared(x, y, cx, cy)
		if distSq < bestDistSq {
			bestDistSq = distSq
			pos := protocol.Vec2{X: cx, Y: cy}
			best = &pos
		}
	}

	return best
}
