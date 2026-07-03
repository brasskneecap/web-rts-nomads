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
	// StartWaveBonus is a testing toggle: when true, every player is presented
	// the wave-upgrade pick at match start (before wave 1) on wave-enabled maps.
	// Optional (defaults false / absent). A player who owns a start-bonus
	// advancement always gets the pick regardless of this flag — see
	// startWaveBonusEnabled / playerHasStartWaveBonusAdvancement.
	StartWaveBonus bool `json:"startWaveBonus"`
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

// SetStartWaveBonusForTest overrides the player.json StartWaveBonus toggle at
// runtime and returns a restore func (call it, typically via t.Cleanup, to put
// the committed value back).
//
// It exists so protocol/transport regression tests — notably the SP outbound
// baseline in internal/ws — stay deterministic regardless of the committed
// testing toggle. StartWaveBonus is a debug switch the user flips on and off;
// when on, a match opens with an RNG-seeded start-of-match upgrade offer whose
// card set differs every run, which would otherwise leak per-run randomness
// into a golden snapshot and make the baseline unpinnable. Those tests force it
// off so the guard reflects normal gameplay either way. NOT for production use.
func SetStartWaveBonusForTest(enabled bool) (restore func()) {
	prev := playerConfigSingleton
	cfg := prev
	cfg.StartWaveBonus = enabled
	playerConfigSingleton = cfg
	return func() { playerConfigSingleton = prev }
}
