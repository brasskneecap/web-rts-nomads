package game

import (
	"math"
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
		return available[s.rngCosmetic.Intn(len(available))]
	}

	return palette[s.rngCosmetic.Intn(len(palette))]
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
