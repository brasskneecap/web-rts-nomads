package protocol

type Vec2 struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type MapConfig struct {
	Size   string  `json:"size"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type JoinMatchMessage struct {
	Type     string `json:"type"`
	PlayerID string `json:"playerId"`
	MapSize  string `json:"mapSize"`
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

type ClientMessage struct {
	Type string `json:"type"`
}

type UnitSnapshot struct {
	ID      int     `json:"id"`
	OwnerID string  `json:"ownerId"`
	Color   string  `json:"color"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	HP      int     `json:"hp"`
	MaxHP   int     `json:"maxHp"`
	TargetX float64 `json:"targetX,omitempty"`
	TargetY float64 `json:"targetY,omitempty"`
	Moving  bool    `json:"moving"`
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
