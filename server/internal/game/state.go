package game

import (
	"crypto/rand"
	"encoding/binary"
	"math"
	mrand "math/rand"
	"sync"
	"time"
	"webrts/server/pkg/protocol"
)

// OrderType is the player's standing order for a unit. It is the single source
// of truth for "what is this unit doing from the player's intent perspective".
// The zero value is OrderIdle so freshly-allocated Units are always valid.
//
// queue-ready: this becomes []OrderState for shift-queue in a future design.
type OrderType int

const (
	OrderIdle        OrderType = iota // no standing order; default acquisition
	OrderMove                         // force-move: ignore enemies en route
	OrderAttackMove                   // a-move: break off to engage acquired enemies
	OrderAttackTarget                 // sticky attack on AttackTargetID/AttackBuildingTargetID
	OrderHold                         // do not move; engage in-range enemies only
	OrderPatrol                       // cycle waypoints; engage acquired enemies; resume
)

// orderTypeString returns the wire-format string for OrderType, matching the
// constants defined in protocol/messages.go. Used by Snapshot.
func orderTypeString(t OrderType) string {
	switch t {
	case OrderMove:
		return protocol.OrderStringMove
	case OrderAttackMove:
		return protocol.OrderStringAttackMove
	case OrderAttackTarget:
		return protocol.OrderStringAttackTarget
	case OrderHold:
		return protocol.OrderStringHold
	case OrderPatrol:
		return protocol.OrderStringPatrol
	default:
		return protocol.OrderStringIdle
	}
}

// OrderState holds the player's standing order for a unit.
// queue-ready: this becomes []OrderState for shift-queue.
type OrderState struct {
	Type OrderType
	// Destination for Move / AttackMove. Re-used as patrol "current waypoint".
	DestX, DestY float64
	// Patrol-only: the OTHER endpoint of the patrol leg.
	// PatrolWaypoints can grow to a []Vec2 later if N-point patrol is wanted.
	PatrolReturnX, PatrolReturnY float64
	// For Hold: the position where the unit was ordered to hold.
	// For AttackMove/Patrol: the anchor returned to when combat ends.
	HoldX, HoldY float64
}

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

	// ObjectiveID links this unit to a VictoryCondition. Non-empty when spawned
	// from an enemy-spawnpoint whose metadata["objectiveId"] matches a condition.
	ObjectiveID string

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

	// NonCombat marks the unit as passive: combat AI never auto-acquires
	// targets for it. The unit only engages when the player issues an
	// explicit OrderAttackTarget (sticky attack). Workers are the canonical
	// non-combat unit; see catalog/units/worker.json.
	NonCombat bool

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
	// Order is the player's current standing order for this unit. It is the
	// single source of truth for intent — replacing the old ManualMove /
	// ManualAttackTarget bool pair. All combat-AI gates, retreat suppression,
	// and resume-after-combat logic key off Order.Type.
	// queue-ready: becomes []OrderState for shift-queue in a future design.
	Order OrderState

	CombatAnchorX      float64
	CombatAnchorY      float64
	LastTargetEvalTick int
	CurrentTargetScore float64
	TauntedByUnitID    int
	TauntRemaining     float64
	// StunnedRemaining is seconds left on the stun CC applied to this unit by
	// ApplyStunLocked. Decays in state.go Update() alongside WeakenedRemaining.
	// While > 0 the unit cannot attack or move along its path, but
	// AttackTargetID and Path are preserved so it resumes cleanly when it expires.
	StunnedRemaining float64
	// SlowedRemaining is seconds left on the slow CC applied to this unit by
	// ApplySlowLocked. Decays in state.go Update(); when it reaches 0,
	// SlowedMultiplier is also cleared.
	SlowedRemaining float64
	// SlowedMultiplier is the movement speed fraction while slowed (e.g. 0.7 =
	// 70% speed). Set by ApplySlowLocked; 0 when no slow is active.
	SlowedMultiplier float64
	ThreatTable        map[int]*ThreatEntry
	TankedDamageByUnit map[int]float64
	// DamageDealtByUnit accumulates damage this unit has taken from each
	// attacker, keyed by attacker ID. On death the map is paid out so
	// contributors earn damage XP only when the target actually dies.
	DamageDealtByUnit map[int]int
}

const (
	// Unit move speed is now authored per-type in catalog/units/<type>.json
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

const (
	wavePrepDuration   = 60.0
	waveActiveDuration = 120.0
)

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

	// matchSeed is the root seed for all per-match RNG streams. Log it on match
	// creation so a bug report with the seed can be reproduced offline.
	matchSeed   int64
	rngPerks    *mrand.Rand // perk selection, path assignment, taunt procs
	rngCosmetic *mrand.Rand // unit colour assignment and other visual randomness
	rngSpawn    *mrand.Rand // reserved for future wave/spawn randomness

	// buildingDamageDealt mirrors Unit.DamageDealtByUnit for buildings.
	// buildingID → attackerID → accumulated damage. Paid out on destruction.
	buildingDamageDealt map[string]map[int]int

	// unitsByID is an O(1) index into s.Units, maintained in lockstep.
	// Use addUnitLocked / removeUnitByIDLocked to mutate — do NOT write to
	// s.Units or unitsByID directly outside those helpers.
	unitsByID map[int]*Unit

	// buildingsByID is an O(1) index into s.MapConfig.Buildings, maintained in
	// lockstep. Use addBuildingLocked / removeBuildingLocked to mutate.
	buildingsByID map[string]*protocol.BuildingTile

	// obstaclesByID is an O(1) index into s.MapConfig.Obstacles. Populated by
	// setMapConfigLocked and maintained by addObstacleLocked /
	// removeObstacleLocked. Obstacles with no id (walls) are not indexed.
	obstaclesByID map[string]*protocol.ObstacleTile

	// blockedCellsCache holds the last computed blocked-cell set.
	// blockedCellsValid is false when any building has been added or removed
	// since the last build. Guarded by s.mu.
	blockedCellsCache map[gridPoint]bool
	blockedCellsValid bool

	// Banners is the set of active rallying banners. Persisted as match state.
	// Ticked in tickBannersLocked after combat resolution.
	Banners      []*Banner
	nextBannerID int

	// Traps is the set of active Trapper traps. Ticked each Update:
	//   tickTrapEffectsLocked(dt)  — zone effects, before tickBannersLocked
	//   tickTrapsLocked(dt)        — lifetime decay + triggered cull, after tickBannersLocked
	Traps      []*Trap
	nextTrapID int

	// Projectiles is the set of in-flight ranged attacks. Ticked once per
	// Update() after tickUnitCombatLocked so freshly-fired shots decay on the
	// next tick, not their birth tick. Damage and all on-hit perk triggers
	// fire when a projectile lands; see projectile.go.
	Projectiles      []*Projectile
	nextProjectileID int

	// guardianAuraCache maps recipient unit ID to the combined armor bonus they
	// receive from the strongest guardian_aura covering them this tick.
	// FlatArmor and PercentArmor are taken as max independently across all
	// covering auras. Rebuilt once per tick in rebuildGuardianAuraCacheLocked.
	// Zero value (absent key) = no aura.
	guardianAuraCache map[int]guardianAuraValue

	// battleTracker is the debug/telemetry damage-and-kill accumulator. Armed
	// only when MapConfig.Debug.BattleTracker is true; otherwise the tracker
	// is allocated but disabled and every track* call is a no-op. Serialized
	// into MatchSnapshotMessage.BattleTracker (omitted when disabled).
	battleTracker *BattleTracker

	// playersWithTownhall tracks which player IDs have ever owned a townhall,
	// so we can distinguish "never had one yet" from "just lost the last one".
	playersWithTownhall map[string]bool
	// lostPlayerIDs is the set of players whose last townhall has been destroyed.
	// Once set, it is never cleared for the duration of the match.
	lostPlayerIDs map[string]bool

	// objectiveCompleted tracks which VictoryCondition IDs have been fulfilled.
	objectiveCompleted map[string]bool
	// objectiveKillCounts tracks progress toward "killUnit" objectives.
	objectiveKillCounts map[string]int
	// victoryAchieved is true once every VictoryCondition is complete.
	victoryAchieved bool
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
	// Mirror of catalog/units/raider.json moveSpeed — kept here because
	// spawnRaiderUnitLocked doesn't do a def lookup like the soldier-type path.
	raiderMoveSpeed = 100.0
)

// newMatchSeed generates a cryptographically-random int64 seed so concurrent
// match creations never collide on the same nanosecond.
func newMatchSeed() int64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fallback: time-based seed. Collision risk is low in practice but
		// possible under rapid match creation; crypto/rand should never fail.
		return time.Now().UnixNano()
	}
	return int64(binary.LittleEndian.Uint64(b[:]))
}

// NewGameState creates a GameState with a freshly generated per-match seed.
// Call-sites that need a reproducible seed (tests, offline replay) should use
// NewGameStateWithSeed instead.
func NewGameState(mapConfig protocol.MapConfig) *GameState {
	return NewGameStateWithSeed(mapConfig, newMatchSeed())
}

// NewGameStateWithSeed creates a GameState whose RNG streams are derived from
// seed. Use seed == 0 only in tests where you intentionally want the zero seed.
// Each stream gets a distinct salt so they advance independently.
func NewGameStateWithSeed(mapConfig protocol.MapConfig, seed int64) *GameState {
	const (
		saltPerks    int64 = 0x1
		saltCosmetic int64 = 0x2
		saltSpawn    int64 = 0x3
	)
	state := &GameState{
		Units:               []*Unit{},
		Players:             map[string]*Player{},
		Productions:         map[string][]*UnitProduction{},
		EnemySpawnTimers:    map[string]*EnemySpawnTimer{},
		nextUnitID:          1,
		nextBannerID:        1,
		nextTrapID:          1,
		nextProjectileID:    1,
		matchSeed:           seed,
		rngPerks:            mrand.New(mrand.NewSource(seed ^ saltPerks)),
		rngCosmetic:         mrand.New(mrand.NewSource(seed ^ saltCosmetic)),
		rngSpawn:            mrand.New(mrand.NewSource(seed ^ saltSpawn)),
		buildingDamageDealt: map[string]map[int]int{},
		unitsByID:           map[int]*Unit{},
		buildingsByID:       map[string]*protocol.BuildingTile{},
		obstaclesByID:       map[string]*protocol.ObstacleTile{},
		guardianAuraCache:   map[int]guardianAuraValue{},
	}

	// Arm the battle tracker iff the map opts in via debug.battleTracker. The
	// tracker is still allocated when disabled so call sites can invoke its
	// methods unconditionally (a nil check + flag check short-circuits cheaply).
	state.battleTracker = newBattleTracker(mapConfig.Debug != nil && mapConfig.Debug.BattleTracker)

	state.SetMapConfig(mapConfig)
	return state
}

// MatchSeed returns the root seed used to initialise this match's RNG streams.
// Log this value when creating a match so bug reports can reference it.
func (s *GameState) MatchSeed() int64 {
	return s.matchSeed
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

	// Rebuild buildingsByID index from the freshly-cloned Buildings slice.
	s.buildingsByID = make(map[string]*protocol.BuildingTile, len(s.MapConfig.Buildings))
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		s.buildingsByID[b.ID] = b
	}
	s.obstaclesByID = make(map[string]*protocol.ObstacleTile, len(s.MapConfig.Obstacles))
	for i := range s.MapConfig.Obstacles {
		o := &s.MapConfig.Obstacles[i]
		if o.ID == "" {
			continue
		}
		s.obstaclesByID[o.ID] = o
	}
	// Blocked cells derived from this new map config are not yet computed.
	s.invalidateBlockedCellsLocked()
}

// ---- Index helpers -------------------------------------------------------

// invalidateBlockedCellsLocked marks the blocked-cells cache as stale.
// Must be called under s.mu write lock whenever a building is added or
// removed, or when obstacles change.
func (s *GameState) invalidateBlockedCellsLocked() {
	s.blockedCellsValid = false
}

// getBlockedCellsLocked returns the cached blocked-cells map, rebuilding it
// if the cache is stale. The returned map is read-only; callers must NOT
// mutate it. If a call site needs a mutable copy (e.g. to add reserved
// cells for a single pathing pass), copy the map locally.
// Must be called under s.mu lock (read or write).
func (s *GameState) getBlockedCellsLocked() map[gridPoint]bool {
	if !s.blockedCellsValid {
		s.blockedCellsCache = s.buildBlockedCells()
		s.blockedCellsValid = true
	}
	return s.blockedCellsCache
}

// addUnitLocked appends unit to s.Units and registers it in s.unitsByID.
// Must be called under s.mu write lock.
func (s *GameState) addUnitLocked(u *Unit) {
	s.Units = append(s.Units, u)
	if s.unitsByID == nil {
		s.unitsByID = make(map[int]*Unit)
	}
	s.unitsByID[u.ID] = u
}

// removeUnitByIDLocked removes the unit with the given ID from both s.Units
// and s.unitsByID. Returns true if the unit was found.
// Must be called under s.mu write lock.
func (s *GameState) removeUnitByIDLocked(id int) bool {
	delete(s.unitsByID, id)
	filtered := make([]*Unit, 0, len(s.Units))
	found := false
	for _, u := range s.Units {
		if u.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, u)
	}
	s.Units = filtered
	return found
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
			ObjectiveID:         unit.ObjectiveID,
			ID:                  unit.ID,
			OwnerID:             unit.OwnerID,
			Color:               unit.Color,
			UnitType:            unit.UnitType,
			Archetype:           unit.Archetype,
			Name:                unit.Name,
			Capabilities:        append([]string(nil), unit.Capabilities...),
			Visible:             unit.Visible,
			Status:              unit.Status,
			Order:               orderTypeString(unit.Order.Type),
			X:                   unit.X,
			Y:                   unit.Y,
			HP:                  unit.HP,
			MaxHP:               unit.MaxHP,
			Damage:              effectiveDamage,
			AttackSpeed:         effectiveAttackSpeed,
			MoveSpeed:           effectiveMoveSpeed,
			Armor:               s.effectiveArmorLocked(unit),
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
			ActiveDebuffs:       s.activeDebuffIconsLocked(unit),
			PerkCooldowns:       s.perkCooldownsLocked(unit),
			StunnedRemaining:    unit.StunnedRemaining,
			SlowedRemaining:     unit.SlowedRemaining,
			SlowedMultiplier:    unit.SlowedMultiplier,
			CarriedResourceType: unit.CarriedResourceType,
			CarriedAmount:       unit.CarriedAmount,
			Moving:              unit.Moving,
		}

		if unit.Moving {
			snapshot.TargetX = unit.TargetX
			snapshot.TargetY = unit.TargetY
		}

		if unit.UnitType == "archer" && unit.ProgressionPath == "trapper" {
			snapshot.EffectiveTrap = s.EffectiveTrapSnapshotLocked(unit)
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
	buildings := make([]protocol.BuildingTile, len(s.MapConfig.Buildings))
	copy(buildings, s.MapConfig.Buildings)
	obstacles := make([]protocol.ObstacleTile, len(s.MapConfig.Obstacles))
	copy(obstacles, s.MapConfig.Obstacles)

	var banners []protocol.BannerSnapshot
	for _, b := range s.Banners {
		banners = append(banners, protocol.BannerSnapshot{
			ID:               b.ID,
			OwnerID:          b.OwnerPlayerID,
			X:                b.X,
			Y:                b.Y,
			Radius:           b.Radius,
			RemainingSeconds: b.RemainingSeconds,
		})
	}

	var projectiles []protocol.ProjectileSnapshot
	for _, proj := range s.Projectiles {
		progress := 0.0
		if proj.TotalSeconds > 0 {
			progress = 1.0 - (proj.RemainingSeconds / proj.TotalSeconds)
			if progress < 0 {
				progress = 0
			} else if progress > 1 {
				progress = 1
			}
		}
		projectiles = append(projectiles, protocol.ProjectileSnapshot{
			ID:           proj.ID,
			OwnerUnitID:  proj.OwnerUnitID,
			OwnerID:      proj.OwnerPlayerID,
			TargetUnitID: proj.TargetUnitID,
			OriginX:      proj.OriginX,
			OriginY:      proj.OriginY,
			TargetX:      proj.TargetX,
			TargetY:      proj.TargetY,
			Progress:     progress,
			Variant:      proj.Variant,
		})
	}

	var traps []protocol.TrapSnapshot
	for _, trap := range s.Traps {
		traps = append(traps, protocol.TrapSnapshot{
			ID:               trap.ID,
			OwnerID:          trap.OwnerPlayerID,
			X:                trap.X,
			Y:                trap.Y,
			Radius:           trap.Radius,
			TriggerRadius:    trap.TriggerRadius, // explosive_trap only; 0 for others (omitted over the wire)
			Variant:          trapVisualVariant(trap),
			ScaleMultiplier:  trapVisualScaleMultiplier(trap),
			Type:             trap.TrapType,
			RemainingSeconds: trap.RemainingSeconds,
			Triggered:        trap.Triggered, // one-tick VFX flash flag (fires on every detonation)
		})
	}

	var gameOver *protocol.GameOverSnapshot
	if len(s.lostPlayerIDs) > 0 {
		ids := make([]string, 0, len(s.lostPlayerIDs))
		for id := range s.lostPlayerIDs {
			ids = append(ids, id)
		}
		gameOver = &protocol.GameOverSnapshot{LostPlayerIDs: ids}
	}

	var victory *protocol.VictorySnapshot
	if len(s.MapConfig.VictoryConditions) > 0 {
		objectives := make([]protocol.ObjectiveSnapshot, 0, len(s.MapConfig.VictoryConditions))
		for _, vc := range s.MapConfig.VictoryConditions {
			progress := 0
			if vc.Type == "killUnit" && s.objectiveKillCounts != nil {
				progress = s.objectiveKillCounts[vc.ID]
			}
			completed := s.objectiveCompleted != nil && s.objectiveCompleted[vc.ID]
			objectives = append(objectives, protocol.ObjectiveSnapshot{
				ID:        vc.ID,
				Type:      vc.Type,
				Label:     vc.Label,
				Completed: completed,
				Progress:  progress,
				Count:     vc.Count,
			})
		}
		victory = &protocol.VictorySnapshot{
			Achieved:   s.victoryAchieved,
			Objectives: objectives,
		}
	}

	return protocol.MatchSnapshotMessage{
		Type:      "match_snapshot",
		Tick:      s.Tick,
		ServerNow: time.Now().UnixMilli(),
		Buildings: buildings,
		Obstacles: obstacles,
		Players:   players,
		Units:     units,
		Banners:     banners,
		Traps:       traps,
		Projectiles: projectiles,
		Wave: protocol.WaveSnapshot{
			Enabled:      wm.Enabled,
			CurrentWave:  wm.CurrentWave,
			TotalWaves:   wm.TotalWaves,
			State:        wm.State,
			Timer:        wm.Timer,
			WaveDuration: wm.WaveDuration,
		},
		// Nil when debug tracker is disabled — `omitempty` drops it from JSON.
		BattleTracker: s.battleTrackerSnapshotLocked(),
		GameOver:      gameOver,
		Victory:       victory,
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

	s.battleTracker.tickLocked(dt)
	s.updateUnitProductionsLocked(dt)
	s.tickBuildingRepairsLocked(dt)
	blocked := s.getBlockedCellsLocked()
	s.tickBuildingCombatLocked(dt)
	s.tickWaveLocked(dt)
	s.rebuildGuardianAuraCacheLocked()
	s.tickCombatAILocked(dt, blocked)
	s.tickUnitCombatLocked(dt, blocked)
	// Projectiles tick after combat resolution so shots fired this tick wait
	// a full dt before decaying on the next Update pass.
	s.tickProjectilesLocked(dt)
	s.tickEnemySpawnpointsLocked(dt, blocked)
	s.tickTrapEffectsLocked(dt)            // zone effects + trigger detection
	s.tickTrapperSilverDebuffsLocked(dt)   // barbed ramp, lasting_flames exit, burn DoT
	s.tickBannersLocked(dt)
	s.tickTrapsLocked(dt) // lifetime decay + triggered cull

	for _, unit := range s.Units {
		if unit.RankUpFxRemaining > 0 {
			unit.RankUpFxRemaining = math.Max(0, unit.RankUpFxRemaining-dt)
		}
		// Cross-unit debuff decay — these states are stamped onto ANY unit by
		// perks on OTHER units (Punishing Guard, Challenger's Mark), so they must
		// tick for every unit regardless of that unit's own perk ownership.
		// Mirrors the TauntRemaining pattern in decayThreatLocked (combat_ai.go).
		if unit.PerkState.WeakenedRemaining > 0 {
			unit.PerkState.WeakenedRemaining = math.Max(0, unit.PerkState.WeakenedRemaining-dt)
			if unit.PerkState.WeakenedRemaining == 0 {
				unit.PerkState.WeakenedMultiplier = 0
			}
		}
		// Mark stacks decay independently (each source ticks down its own
		// Remaining). lastExpired is true only when the final active stack
		// hits 0 this tick — that's when mark-gone effects (Final Exposure,
		// Shared Pain disarm) fire.
		if unit.PerkState.decayMarkStacks(dt) {
			// overload_protocol → Final Exposure: when the last mark stack
			// expires, fire burst damage to this victim and an optional
			// small AoE to nearby enemies. The armed fields are consumed
			// immediately after so re-arming via a fresh mark works again.
			if unit.PerkState.FinalExposureDamage > 0 && unit.HP > 0 {
				s.fireFinalExposureLocked(unit)
			}
			unit.PerkState.FinalExposureDamage = 0
			unit.PerkState.FinalExposureAoeRadius = 0
			unit.PerkState.FinalExposureOwnerUnitID = 0
			// ascendant_infusion → Shared Pain disarms with the mark.
			unit.PerkState.SharedPainFraction = 0
		}
		// ascendant_infusion → Electrified Caltrops per-victim stun cooldown.
		// Cross-unit state (lives on any enemy hit by Electrified), same decay
		// pattern as SlowedRemaining / MarkedRemaining.
		if unit.PerkState.ElectrifiedStunCooldownRemaining > 0 {
			unit.PerkState.ElectrifiedStunCooldownRemaining = math.Max(0, unit.PerkState.ElectrifiedStunCooldownRemaining-dt)
		}
		// Generic CC decay — Stun and Slow are general primitives that any perk or
		// ability can stamp onto any unit, so they decay here alongside the other
		// cross-unit debuffs rather than in tickUnitPerkStateLocked.
		if unit.StunnedRemaining > 0 {
			unit.StunnedRemaining = math.Max(0, unit.StunnedRemaining-dt)
		}
		if unit.SlowedRemaining > 0 {
			unit.SlowedRemaining = math.Max(0, unit.SlowedRemaining-dt)
			if unit.SlowedRemaining == 0 {
				unit.SlowedMultiplier = 0
			}
		}
		// Trapper combat tail-window: decay toward 0 each tick regardless of
		// unit type. Only archers set this to 1.5s (in tickUnitCombatLocked),
		// so it is always 0 for non-archers and the check is cheap.
		if unit.PerkState.LastCombatSeconds > 0 {
			unit.PerkState.LastCombatSeconds = math.Max(0, unit.PerkState.LastCombatSeconds-dt)
		}

		// Advance time-based perk state (idle timers, buff durations).
		s.tickUnitPerkStateLocked(unit, dt)
		s.updateWorkerTaskLocked(unit, dt, blocked)

		if unit.MiningInside {
			continue
		}

		// Stun gates all pathing. Leave Moving and Path intact so the unit
		// resumes exactly where it was once the stun expires.
		if unit.StunnedRemaining > 0 {
			continue
		}

		if !unit.Moving {
			// Force-move order is complete when the unit stops moving.
			if unit.Order.Type == OrderMove {
				unit.Order = OrderState{Type: OrderIdle}
			}
			continue
		}

		if len(unit.Path) == 0 {
			unit.Moving = false
			// Force-move order is complete when the path is exhausted.
			if unit.Order.Type == OrderMove {
				unit.Order = OrderState{Type: OrderIdle}
			}
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
		// (momentum, future speed perks) × slow multiplier (CC primitive).
		step := unit.MoveSpeed * s.perkMoveSpeedMultiplierLocked(unit) * slowFactorLocked(unit) * dt
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
	s.refreshObstacleRuntimeMetadataLocked()
	s.checkPlayerLossLocked()
	s.checkVictoryLocked()
}

// markObjectiveKillLocked records a kill toward a "killUnit" victory condition
// and marks it complete once the required kill count is reached.
func (s *GameState) markObjectiveKillLocked(objectiveID string) {
	if objectiveID == "" {
		return
	}
	if s.objectiveCompleted == nil {
		s.objectiveCompleted = map[string]bool{}
	}
	if s.objectiveKillCounts == nil {
		s.objectiveKillCounts = map[string]int{}
	}
	for _, vc := range s.MapConfig.VictoryConditions {
		if vc.ID != objectiveID || vc.Type != "killUnit" {
			continue
		}
		s.objectiveKillCounts[objectiveID]++
		needed := vc.Count
		if needed <= 0 {
			needed = 1
		}
		if s.objectiveKillCounts[objectiveID] >= needed {
			s.objectiveCompleted[objectiveID] = true
		}
		return
	}
}

// markBuildingObjectiveCompleteLocked checks if the destroyed building is
// linked to a "destroyBuilding" victory condition and marks it complete.
func (s *GameState) markBuildingObjectiveCompleteLocked(buildingID string) {
	building := s.getBuildingByIDLocked(buildingID)
	if building == nil {
		return
	}
	objectiveID, ok := getMetadataString(building.Metadata, "objectiveId")
	if !ok || objectiveID == "" {
		return
	}
	if s.objectiveCompleted == nil {
		s.objectiveCompleted = map[string]bool{}
	}
	for _, vc := range s.MapConfig.VictoryConditions {
		if vc.ID == objectiveID && vc.Type == "destroyBuilding" {
			s.objectiveCompleted[objectiveID] = true
			return
		}
	}
}

// markWaveObjectivesCompleteLocked marks all "surviveWaves" victory conditions
// as complete. Called when the wave manager reaches state "complete".
func (s *GameState) markWaveObjectivesCompleteLocked() {
	if s.objectiveCompleted == nil {
		s.objectiveCompleted = map[string]bool{}
	}
	for _, vc := range s.MapConfig.VictoryConditions {
		if vc.Type == "surviveWaves" {
			s.objectiveCompleted[vc.ID] = true
		}
	}
}

// checkVictoryLocked checks whether all VictoryConditions are complete and
// sets victoryAchieved. No-op when the map has no victory conditions.
func (s *GameState) checkVictoryLocked() {
	if len(s.MapConfig.VictoryConditions) == 0 || s.victoryAchieved {
		return
	}
	for _, vc := range s.MapConfig.VictoryConditions {
		if s.objectiveCompleted == nil || !s.objectiveCompleted[vc.ID] {
			return
		}
	}
	s.victoryAchieved = true
}

// checkPlayerLossLocked scans all townhalls each tick to detect players who
// have lost all of theirs. A player can only lose once they have owned at
// least one townhall — this prevents marking players as "lost" before they
// have even claimed a starting position.
func (s *GameState) checkPlayerLossLocked() {
	if s.playersWithTownhall == nil {
		s.playersWithTownhall = map[string]bool{}
	}
	if s.lostPlayerIDs == nil {
		s.lostPlayerIDs = map[string]bool{}
	}

	townhallCounts := map[string]int{}
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.BuildingType != "townhall" || !b.Visible || b.OwnerID == nil || *b.OwnerID == enemyPlayerID {
			continue
		}
		s.playersWithTownhall[*b.OwnerID] = true
		townhallCounts[*b.OwnerID]++
	}

	for playerID := range s.playersWithTownhall {
		if s.lostPlayerIDs[playerID] {
			continue
		}
		if townhallCounts[playerID] == 0 {
			s.lostPlayerIDs[playerID] = true
		}
	}
}

// IsGameOver returns true once the match has ended — either a player has lost
// all their townhalls, or all victory objectives have been completed.
func (s *GameState) IsGameOver() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.lostPlayerIDs) > 0 || s.victoryAchieved
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

	// Collect IDs first, then remove via the helper to keep the index in sync.
	var toRemove []int
	for _, unit := range s.Units {
		if unit.OwnerID == playerID {
			toRemove = append(toRemove, unit.ID)
		}
	}
	for _, id := range toRemove {
		delete(s.unitsByID, id)
	}
	filtered := make([]*Unit, 0, len(s.Units)-len(toRemove))
	for _, unit := range s.Units {
		if unit.OwnerID != playerID {
			filtered = append(filtered, unit)
		}
	}
	s.Units = filtered

	s.releaseTownhallForPlayerLocked(playerID)

	// Drop any traps planted by the leaving player — mirrors banner cleanup.
	if len(s.Traps) > 0 {
		kept := s.Traps[:0]
		for _, trap := range s.Traps {
			if trap.OwnerPlayerID != playerID {
				kept = append(kept, trap)
			}
		}
		s.Traps = kept
	}

	// Drop any in-flight projectiles fired by the leaving player.
	s.cullProjectilesLocked(func(p *Projectile) bool {
		return p.OwnerPlayerID == playerID
	})
}

func (s *GameState) removeUnitLocked(unitID int) {
	s.removeUnitByIDLocked(unitID)

	// Drop in-flight projectiles involving this unit so stale IDs don't linger.
	s.cullProjectilesLocked(func(p *Projectile) bool {
		return p.OwnerUnitID == unitID || p.TargetUnitID == unitID
	})

	// Clear attack targets pointing to removed unit
	for _, u := range s.Units {
		if u.AttackTargetID == unitID {
			u.AttackTargetID = 0
			u.Attacking = false
			u.Status = "Idle"
			// If the player's explicit attack order was on this unit, demote
			// it back to Idle so non-combat attackers (workers) return to
			// passive state and combat attackers resume default AI targeting.
			// Mirrors the demotion in clearCombatTargetLocked.
			if u.Order.Type == OrderAttackTarget {
				u.Order = OrderState{Type: OrderIdle}
			}
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
