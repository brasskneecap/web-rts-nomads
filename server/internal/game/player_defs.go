package game

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed catalog/player/player.json
var playerConfigJSON []byte

// PlayerConfig is the externalized configuration for a player's match-start
// state. It centralizes values that were previously hardcoded at player
// creation so they are easy to find and tune. Loaded once at init; never
// mutated after startup.
//
// Match-start values here are the baseline; permanent (profile) upgrades layer
// on top of them at match start — see applyProfileUpgradesToPlayerLocked and
// the "startingResource" effect in profile_upgrade_defs.go.
type PlayerConfig struct {
	Version int `json:"version"`
	// StartingResources is the resource map every player begins a match with,
	// keyed by resource type (e.g. "gold", "wood"). Profile upgrades add to
	// these amounts after the player is created.
	StartingResources map[string]int `json:"startingResources"`
}

// playerConfigSingleton is initialized via a package-level var initializer
// (not init()) so that other package-level initializers which reference
// playerConfig() — e.g. the profile-upgrade effect registry validating
// "startingResource" resource keys — observe the loaded config. Go orders
// var initialization by dependency; init() funcs run only afterward.
var playerConfigSingleton = loadPlayerConfig()

func loadPlayerConfig() PlayerConfig {
	var c PlayerConfig
	if err := json.Unmarshal(playerConfigJSON, &c); err != nil {
		panic("catalog/player/player.json: " + err.Error())
	}
	if c.Version != 1 {
		panic(fmt.Sprintf("catalog/player/player.json: unsupported version %d (want 1)", c.Version))
	}
	if len(c.StartingResources) == 0 {
		panic("catalog/player/player.json: startingResources must not be empty")
	}
	for resource, amount := range c.StartingResources {
		if resource == "" {
			panic("catalog/player/player.json: startingResources has an empty resource key")
		}
		if amount < 0 {
			panic(fmt.Sprintf("catalog/player/player.json: startingResources[%q] must be >= 0, got %d", resource, amount))
		}
	}
	return c
}

// playerConfig returns the package-level player configuration loaded at startup.
func playerConfig() PlayerConfig {
	return playerConfigSingleton
}

// newStartingResources returns a fresh, mutable copy of the configured starting
// resources. Each player must get its own map so per-player spending and
// upgrade bonuses never alias the shared singleton.
func (c PlayerConfig) newStartingResources() map[string]int {
	out := make(map[string]int, len(c.StartingResources))
	for resource, amount := range c.StartingResources {
		out[resource] = amount
	}
	return out
}

// ExportedPlayerConfig exposes the player configuration singleton to packages
// outside the game package (e.g. HTTP handlers). Internal simulation code
// should use the unexported playerConfig() accessor.
func ExportedPlayerConfig() PlayerConfig {
	return playerConfigSingleton
}
