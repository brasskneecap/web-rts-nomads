package protocol

type Vec2 struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type MapConfig struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Description string       `json:"description"`
	Size      string         `json:"size"`
	Width     float64        `json:"width"`
	Height    float64        `json:"height"`
	GridCols  int            `json:"gridCols"`
	GridRows  int            `json:"gridRows"`
	CellSize  float64        `json:"cellSize"`
	Terrain   []TerrainTile  `json:"terrain"`
	Obstacles []ObstacleTile `json:"obstacles"`
	Buildings []BuildingTile `json:"buildings"`
}

type GridCoord struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type TerrainTile struct {
	GridCoord
	Terrain string `json:"terrain"`
}

type ObstacleTile struct {
	GridCoord
	Obstacle string `json:"obstacle"`
}

type BuildingTile struct {
	GridCoord
	ID            string                 `json:"id"`
	BuildingType  string                 `json:"buildingType"`
	Width         int                    `json:"width"`
	Height        int                    `json:"height"`
	Occupied      bool                   `json:"occupied"`
	Visible       bool                   `json:"visible"`
	OwnerID       *string                `json:"ownerId,omitempty"`
	Capabilities  []string               `json:"capabilities"`
	ResourceType  string                 `json:"resourceType,omitempty"`
	ResourceAmount int                   `json:"resourceAmount,omitempty"`
	SpawnUnitTypes []string              `json:"spawnUnitTypes,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

type JoinMatchMessage struct {
	Type     string `json:"type"`
	PlayerID string `json:"playerId"`
	MapID    string `json:"mapId"`
	MatchID  string `json:"matchId,omitempty"`
}

type LeaveMatchMessage struct {
	Type     string `json:"type"`
	PlayerID string `json:"playerId"`
	MatchID  string `json:"matchId"`
}

type MoveCommandMessage struct {
	Type        string `json:"type"`
	UnitIDs     []int  `json:"unitIds"`
	Destination Vec2   `json:"destination"`
}

type GatherCommandMessage struct {
	Type       string `json:"type"`
	UnitIDs    []int  `json:"unitIds"`
	BuildingID string `json:"buildingId"`
}

type ClientMessage struct {
	Type string `json:"type"`
}

type ResourceStock struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Amount int    `json:"amount"`
	Accent string `json:"accent"`
}

type PlayerSnapshot struct {
	PlayerID   string          `json:"playerId"`
	Color      string          `json:"color"`
	Resources  []ResourceStock `json:"resources"`
}

type UnitSnapshot struct {
	ID                  int      `json:"id"`
	OwnerID             string   `json:"ownerId"`
	Color               string   `json:"color"`
	UnitType            string   `json:"unitType"`
	Name                string   `json:"name"`
	Capabilities        []string `json:"capabilities,omitempty"`
	Visible             bool     `json:"visible"`
	Status              string   `json:"status,omitempty"`
	X                   float64  `json:"x"`
	Y                   float64  `json:"y"`
	HP                  int      `json:"hp"`
	MaxHP               int      `json:"maxHp"`
	CarriedResourceType string   `json:"carriedResourceType,omitempty"`
	CarriedAmount       int      `json:"carriedAmount,omitempty"`
	TargetX             float64  `json:"targetX,omitempty"`
	TargetY             float64  `json:"targetY,omitempty"`
	Moving              bool     `json:"moving"`
}

type WelcomeMessage struct {
	Type     string    `json:"type"`
	PlayerID string    `json:"playerId"`
	MatchID  string    `json:"matchId"`
	Map      MapConfig `json:"map"`
}

type MatchSnapshotMessage struct {
	Type      string         `json:"type"`
	Tick      int            `json:"tick"`
	ServerNow int64          `json:"serverNow"`
	MatchID   string         `json:"matchId"`
	Map       MapConfig      `json:"map"`
	Players   []PlayerSnapshot `json:"players"`
	Units     []UnitSnapshot `json:"units"`
}

type PingMessage struct {
	Type string `json:"type"`
}

type PongMessage struct {
	Type string `json:"type"`
}

type ErrorMessage struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}
