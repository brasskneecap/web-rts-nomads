package game

import (
	"math"
	"math/rand"
	"sync"
	"time"

	"webrts/server/pkg/protocol"
)

type Unit struct {
	ID      int
	OwnerID string
	Color   string
	X       float64
	Y       float64
	HP      int
	MaxHP   int

	TargetX float64
	TargetY float64
	Moving  bool
}

type Player struct {
	ID    string
	Color string
}

type GameState struct {
	mu sync.RWMutex

	Tick int

	MapSize   string
	MapWidth  float64
	MapHeight float64

	Units   []*Unit
	Players map[string]*Player

	nextUnitID int
	rng        *rand.Rand
}

func NewGameState() *GameState {
	state := &GameState{
		Units:      []*Unit{},
		Players:    map[string]*Player{},
		nextUnitID: 1,
		rng:        rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	state.SetMapSize("large")
	return state
}

func (s *GameState) SetMapSize(size string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.setMapSizeLocked(size)
}

func (s *GameState) setMapSizeLocked(size string) {
	switch size {
	case "small":
		s.MapSize = "small"
		s.MapWidth = 3072
		s.MapHeight = 2048
	case "medium":
		s.MapSize = "medium"
		s.MapWidth = 4096
		s.MapHeight = 3072
	default:
		s.MapSize = "large"
		s.MapWidth = 6144
		s.MapHeight = 4096
	}
}

func (s *GameState) GetMapConfig() protocol.MapConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return protocol.MapConfig{
		Size:   s.MapSize,
		Width:  s.MapWidth,
		Height: s.MapHeight,
	}
}

func (s *GameState) Snapshot() protocol.MatchSnapshotMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	units := make([]protocol.UnitSnapshot, 0, len(s.Units))
	for _, unit := range s.Units {
		snapshot := protocol.UnitSnapshot{
			ID:      unit.ID,
			OwnerID: unit.OwnerID,
			Color:   unit.Color,
			X:       unit.X,
			Y:       unit.Y,
			HP:      unit.HP,
			MaxHP:   unit.MaxHP,
			Moving:  unit.Moving,
		}

		if unit.Moving {
			snapshot.TargetX = unit.TargetX
			snapshot.TargetY = unit.TargetY
		}

		units = append(units, snapshot)
	}

	return protocol.MatchSnapshotMessage{
		Type:      "match_snapshot",
		Tick:      s.Tick,
		ServerNow: time.Now().UnixMilli(),
		Map: protocol.MapConfig{
			Size:   s.MapSize,
			Width:  s.MapWidth,
			Height: s.MapHeight,
		},
		Units: units,
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

	const speed = 100.0

	for _, unit := range s.Units {
		if !unit.Moving {
			continue
		}

		dx := unit.TargetX - unit.X
		dy := unit.TargetY - unit.Y
		dist := math.Sqrt(dx*dx + dy*dy)

		if dist == 0 {
			unit.Moving = false
			continue
		}

		step := speed * dt
		if step >= dist {
			unit.X = unit.TargetX
			unit.Y = unit.TargetY
			unit.Moving = false
			continue
		}

		unit.X += (dx / dist) * step
		unit.Y += (dy / dist) * step
	}
}

func (s *GameState) MoveUnits(playerID string, unitIDs []int, dest protocol.Vec2) {
	s.mu.Lock()
	defer s.mu.Unlock()

	validUnits := make([]*Unit, 0, len(unitIDs))
	unitMap := make(map[int]*Unit, len(s.Units))

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
		unit.TargetX = clampFloat(dest.X, 0, s.MapWidth)
		unit.TargetY = clampFloat(dest.Y, 0, s.MapHeight)
		unit.Moving = true
		return
	}

	spacing := 24.0
	cols := int(math.Ceil(math.Sqrt(float64(len(validUnits)))))
	rows := int(math.Ceil(float64(len(validUnits)) / float64(cols)))

	totalWidth := float64(cols-1) * spacing
	totalHeight := float64(rows-1) * spacing

	startX := dest.X - totalWidth/2
	startY := dest.Y - totalHeight/2

	for i, unit := range validUnits {
		col := i % cols
		row := i / cols

		targetX := startX + float64(col)*spacing
		targetY := startY + float64(row)*spacing

		unit.TargetX = clampFloat(targetX, 0, s.MapWidth)
		unit.TargetY = clampFloat(targetY, 0, s.MapHeight)
		unit.Moving = true
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
	}

	s.spawnUnitsForPlayerLocked(playerID, color, 5)
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
}

func (s *GameState) spawnUnitsForPlayerLocked(playerID, color string, count int) {
	playerIndex := len(s.Players) - 1

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

	spacing := 28.0
	cols := int(math.Ceil(math.Sqrt(float64(count))))

	for i := 0; i < count; i++ {
		col := i % cols
		row := i / cols

		unit := &Unit{
			ID:      s.nextUnitID,
			OwnerID: playerID,
			Color:   color,
			X:       baseX + float64(col)*spacing,
			Y:       baseY + float64(row)*spacing,
			HP:      100,
			MaxHP:   100,
		}

		s.nextUnitID++
		s.Units = append(s.Units, unit)
	}
}

func (s *GameState) randomColor() string {
	palette := []string{
		"#e74c3c",
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
