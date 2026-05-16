package profile

// CurrentVersion is the schema version written into every new profile.
// Increment this when the struct layout changes and add migration logic.
const CurrentVersion = 1

// DefaultCommanderID is the commander assigned to new profiles when no other
// commander is specified.
const DefaultCommanderID = "nomad_commander_default"

// PlayerProfile is the persistent per-player profile stored as a JSON file.
// All match-transient data lives elsewhere; this is the cross-match record.
type PlayerProfile struct {
	PlayerID             string       `json:"playerId"`
	Version              int          `json:"version"`
	CreatedAtUnix        int64        `json:"createdAtUnix"`
	UpdatedAtUnix        int64        `json:"updatedAtUnix"`
	LegendPoints         int          `json:"legendPoints"`
	LifetimeLegendPoints int          `json:"lifetimeLegendPoints"`
	OwnedCommanderIDs    []string     `json:"ownedCommanderIds"`
	SelectedCommanderID  string       `json:"selectedCommanderId"`
	EquippedBuffIDs      []string     `json:"equippedBuffIds"`
	UnlockedBuffIDs      []string     `json:"unlockedBuffIds"`
	Stats                ProfileStats `json:"stats"`

	// Wave upgrade legend-incrementable caps. Zero values fall back to defaults
	// (MaxRerolls=1, MaxUpgradeStacks=3) applied at match start.
	MaxRerolls       int `json:"maxRerolls"`
	MaxUpgradeStacks int `json:"maxUpgradeStacks"`
}

// ProfileStats tracks lifetime match and combat statistics for a player.
type ProfileStats struct {
	MatchesPlayed  int `json:"matchesPlayed"`
	MatchesWon     int `json:"matchesWon"`
	MatchesLost    int `json:"matchesLost"`
	EnemiesKilled  int `json:"enemiesKilled"`
	ObjectivesDone int `json:"objectivesDone"`
}
