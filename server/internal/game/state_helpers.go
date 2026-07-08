package game

import (
	"math"
	"strconv"
	"strings"
	"webrts/server/pkg/protocol"
)

func (s *GameState) getUnitByIDLocked(unitID int) *Unit {
	return s.unitsByID[unitID]
}

func (s *GameState) getBuildingByIDLocked(buildingID string) *protocol.BuildingTile {
	return s.buildingsByID[buildingID]
}

func unitHasCapability(unitType, capability string) bool {
	def, ok := getUnitDef(unitType)
	return ok && containsString(def.Capabilities, capability)
}

func (s *GameState) isUnitNearBuildingLocked(unit *Unit, building protocol.BuildingTile, padding float64) bool {
	left := float64(building.X) * s.MapConfig.CellSize
	top := float64(building.Y) * s.MapConfig.CellSize
	right := left + float64(building.Width)*s.MapConfig.CellSize
	bottom := top + float64(building.Height)*s.MapConfig.CellSize
	return unit.X >= left-padding && unit.X <= right+padding && unit.Y >= top-padding && unit.Y <= bottom+padding
}

// playerSlotColors assigns a fixed team color per player slot (0-based). These
// are deliberate, not randomized, so a given slot always renders the same color
// across matches and no player's color collides with the enemy-red or neutral
// (#9b59b6) health-bar colors used to tell friend from foe — the color of a
// unit's health bar is how a player identifies which units are theirs. Slots
// beyond this list wrap around to keep a stable, deterministic color.
var playerSlotColors = []string{
	"#3498db", // player 1 — blue
	"#e67e22", // player 2 — orange
	"#8e44ad", // player 3 — purple (distinct from the neutral purple #9b59b6)
	"#f1c40f", // player 4 — yellow
	"#2ecc71", // player 5 — green
	"#1abc9c", // player 6 — teal
	"#ec4899", // player 7 — pink
}

// slotColorForPlayerLocked returns the fixed team color for playerID's slot. The
// slot comes from the map's authored spawn-point playerLabel ("player1"..); on
// maps without labeled slots it falls back to join order. Deterministic and
// stable for the life of the slot. Must be called after the player has claimed
// their starting townhall (so findPlayerLabelLocked can resolve the label).
func (s *GameState) slotColorForPlayerLocked(playerID string) string {
	idx := s.playerSlotIndexLocked(playerID)
	n := len(playerSlotColors)
	return playerSlotColors[((idx%n)+n)%n]
}

// playerSlotIndexLocked resolves a 0-based slot index for playerID. Prefers the
// authored spawn-point label (player1 -> 0); falls back to join order (count of
// other human players already present) when the map has no labeled slot.
func (s *GameState) playerSlotIndexLocked(playerID string) int {
	if label := s.findPlayerLabelLocked(playerID); label != "" {
		if n, ok := playerLabelIndex(label); ok {
			return n
		}
	}
	// Fallback: join order. s.Players already contains this player, so count the
	// other humans that joined before it.
	idx := 0
	for id := range s.Players {
		if id == playerID || id == enemyPlayerID || id == neutralPlayerID {
			continue
		}
		idx++
	}
	return idx
}

// playerLabelIndex parses a "playerN" slot label into a 0-based index.
func playerLabelIndex(label string) (int, bool) {
	const prefix = "player"
	if !strings.HasPrefix(label, prefix) {
		return 0, false
	}
	n, err := strconv.Atoi(label[len(prefix):])
	if err != nil || n < 1 {
		return 0, false
	}
	return n - 1, true
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

func getMetadataBool(metadata map[string]interface{}, key string) bool {
	if metadata == nil {
		return false
	}
	v, ok := metadata[key]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
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
