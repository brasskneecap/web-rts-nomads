package profile

// CurrentVersion is the schema version written into every new profile.
// Increment this when the struct layout changes and add migration logic.
const CurrentVersion = 8

// DefaultCommanderID is the commander assigned to new profiles when no other
// commander is specified.
const DefaultCommanderID = "nomad_commander_default"

// PlayerProfile is the persistent per-player profile stored as a JSON file.
// All match-transient data lives elsewhere; this is the cross-match record.
type PlayerProfile struct {
	PlayerID               string       `json:"playerId"`
	Version                int          `json:"version"`
	CreatedAtUnix          int64        `json:"createdAtUnix"`
	UpdatedAtUnix          int64        `json:"updatedAtUnix"`
	DominionPoints         int          `json:"dominionPoints"`
	LifetimeDominionPoints int          `json:"lifetimeDominionPoints"`
	OwnedCommanderIDs      []string     `json:"ownedCommanderIds"`
	SelectedCommanderID    string       `json:"selectedCommanderId"`
	Stats                  ProfileStats `json:"stats"`

	// Legacy pre-v7 currency keys ("Legend Points"). Read on load so the
	// v6->v7 migration can carry existing balances into DominionPoints /
	// LifetimeDominionPoints, then cleared to nil so they are never
	// re-serialized (omitempty drops nil pointers). See migrateProfile.
	LegacyLegendPoints         *int `json:"legendPoints,omitempty"`
	LegacyLifetimeLegendPoints *int `json:"lifetimeLegendPoints,omitempty"`

	// Wave upgrade dominion-incrementable caps. Zero values fall back to defaults
	// (MaxRerolls=1, MaxUpgradeStacks=3) applied at match start.
	MaxRerolls       int `json:"maxRerolls"`
	MaxUpgradeStacks int `json:"maxUpgradeStacks"`

	// OwnedUpgradeRanks maps profile upgrade ID to the player's purchased rank.
	// Added in schema version 2. A nil map is equivalent to an empty map; both
	// mean no upgrades have been purchased. Initialized to a non-nil empty map
	// on creation and on v1->v2 migration.
	OwnedUpgradeRanks map[string]int `json:"ownedUpgradeRanks"`

	// ActiveUpgradeIDs is the sorted set of upgrade IDs the player has chosen
	// to activate. Presence in the slice means active; absence means inactive.
	// Added in schema version 3. On v2->v3 migration, any upgrade with rank > 0
	// is automatically added (active by default).
	ActiveUpgradeIDs []string `json:"activeUpgradeIds"`

	// AcquiredAdvancements is the list of unit advancement nodes the player has
	// purchased. Each entry records the advancement ID and the Dominion Point cost
	// paid at purchase time (used for refund-on-cost-change on load).
	// Added in schema version 4. A nil slice is equivalent to an empty slice.
	AcquiredAdvancements []AcquiredAdvancement `json:"acquiredAdvancements"`

	// CompletedCampaignLevels is the set of campaign level IDs the player has
	// completed. Stored sorted + deduped. The campaign catalog itself lives on
	// the client (see client/src/game-portal/src/data/campaigns.ts) — the server
	// only records which level IDs the player has finished so unlock state can
	// be computed from this list at any time. Added in schema version 5.
	CompletedCampaignLevels []string `json:"completedCampaignLevels"`

	// CompletedCampaignObjectives records the union of objective IDs the
	// player has ever completed in any past attempt of a campaign level. The
	// map key is the literal string "<campaignId>/<levelId>"; the value is a
	// sorted, deduped set of objective IDs. Replay starts with fresh in-match
	// progress; this map is the all-time record. A level can have objectives
	// completed without the level itself being beaten — the two are tracked
	// independently. Added in schema version 6.
	CompletedCampaignObjectives map[string][]string `json:"completedCampaignObjectives"`

	// CreditedMatchIDs records match IDs whose end-of-match dominion-point
	// award has already been applied to this profile, so a client retry /
	// recap re-mount cannot double-credit. Bounded to the most recent entries
	// (see award handler). nil/empty for fresh profiles. Added in schema
	// version 7. A nil slice is equivalent to an empty slice; no migration is
	// needed because omitempty serializes nil as absent and the award handler
	// treats a missing ledger the same as an empty one.
	CreditedMatchIDs []string `json:"creditedMatchIds,omitempty"`

	// KnownRecipeIDs is the set of crafting recipe IDs this player has crafted
	// at least once, unlocking them for crafting in all future matches. Added in
	// schema version 8. Sorted, deduped. nil == empty.
	KnownRecipeIDs []string `json:"knownRecipeIds"`
}

// AcquiredAdvancement records a single purchased advancement node.
type AcquiredAdvancement struct {
	// ID is the UnitAdvancementNode.ID that was purchased.
	ID string `json:"id"`
	// CostPaid is the Dominion Point cost deducted at purchase time. Stored so
	// that if the catalog cost changes after purchase, a refund-on-cost-change
	// migration can issue the correct delta on next load.
	CostPaid int `json:"costPaid"`
}

// ProfileStats tracks lifetime match and combat statistics for a player.
type ProfileStats struct {
	MatchesPlayed  int `json:"matchesPlayed"`
	MatchesWon     int `json:"matchesWon"`
	MatchesLost    int `json:"matchesLost"`
	EnemiesKilled  int `json:"enemiesKilled"`
	ObjectivesDone int `json:"objectivesDone"`
}
