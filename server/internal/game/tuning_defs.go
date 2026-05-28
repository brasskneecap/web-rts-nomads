package game

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed catalog/tuning/gameplay_tuning.json
var gameplayTuningJSON []byte

// GameplayTuning is the root struct for all externalized gameplay numeric rates.
// Loaded once at init; never mutated after startup.
type GameplayTuning struct {
	Version       int                                `json:"version"`
	LegendPoints  LegendPointsTuning                 `json:"legendPoints"`
	WaveUpgrade   WaveUpgradeTuning                  `json:"waveUpgrade"`
	UnitOverrides map[string]UnitLegendPointOverride `json:"unitOverrides"`
	Experience    ExperienceTuning                   `json:"experience"`
}

// LegendPointsTuning holds all legend-point earning rates.
type LegendPointsTuning struct {
	WinBonus              int     `json:"winBonus"`
	LossConsolation       int     `json:"lossConsolation"`
	PerObjective          int     `json:"perObjective"`
	PerKillBaseDropChance float64 `json:"perKillBaseDropChance"`
	PerKillBaseAmount     int     `json:"perKillBaseAmount"`
	// CommitMode selects when kill-drop earnings reach the player's profile:
	//   "matchEnd"  — accumulate into Player.RunLegendPointDrops, commit
	//                 once at game-over via the MatchManager's committer.
	//                 Default; intended for shipped builds.
	//   "immediate" — fire-and-forget commit per drop (skips the
	//                 RunLegendPointDrops accumulator). Intended for
	//                 testing / verification builds only.
	// Empty string is treated as "matchEnd" for backward compatibility.
	CommitMode string `json:"commitMode"`
}

const (
	legendPointCommitModeMatchEnd  = "matchEnd"
	legendPointCommitModeImmediate = "immediate"
)

// WaveUpgradeTuning controls offer generation for the wave upgrade phase.
type WaveUpgradeTuning struct {
	// TimerSeconds is how long players have to pick before auto-select fires.
	TimerSeconds float64 `json:"timerSeconds"`
	// BaseWeights is the rarity probability weight at wave 1.
	BaseWeights map[string]float64 `json:"baseWeights"`
	// RarityScalePerWave is added to each rarity's weight each wave (can be negative).
	RarityScalePerWave map[string]float64 `json:"rarityScalePerWave"`
	// MilestoneWaves are wave numbers that guarantee at least one card of MilestoneMinRarity or better.
	MilestoneWaves []int `json:"milestoneWaves"`
	// MilestoneMinRarity is the minimum rarity guaranteed on a milestone wave.
	MilestoneMinRarity string `json:"milestoneMinRarity"`
}

// UnitLegendPointOverride lets specific unit types earn different legend-point
// rewards when killed, overriding the base tuning values.
type UnitLegendPointOverride struct {
	LegendPointDropChance float64 `json:"legendPointDropChance"`
	LegendPointAmount     int     `json:"legendPointAmount"`
}

// ExperienceTuning selects the experience-gaining system and tunes the
// "split" mode. Mode "classic" leaves all legacy payouts unchanged; "split"
// distributes each enemy's experience value evenly among eligible recipients.
type ExperienceTuning struct {
	// Mode is "classic" (legacy payouts) or "split" (even per-enemy split).
	Mode string `json:"mode"`
	// SplitDefaultXP is the experience used when an enemy's UnitDef omits the
	// "experience" field. Must be >= 0.
	SplitDefaultXP int `json:"splitDefaultXP"`
	// SplitEligibilityRadius is the proximity radius in world pixels, measured
	// from the dying unit at the moment of death. Must be > 0.
	SplitEligibilityRadius float64 `json:"splitEligibilityRadius"`
}

var gameplayTuningSingleton GameplayTuning

func init() {
	var t GameplayTuning
	if err := json.Unmarshal(gameplayTuningJSON, &t); err != nil {
		panic("catalog/tuning/gameplay_tuning.json: " + err.Error())
	}
	if t.Version != 1 {
		panic(fmt.Sprintf("catalog/tuning/gameplay_tuning.json: unsupported version %d (want 1)", t.Version))
	}
	if t.LegendPoints.WinBonus < 0 {
		panic("catalog/tuning/gameplay_tuning.json: legendPoints.winBonus must be >= 0")
	}
	if t.LegendPoints.LossConsolation < 0 {
		panic("catalog/tuning/gameplay_tuning.json: legendPoints.lossConsolation must be >= 0")
	}
	if t.LegendPoints.PerObjective < 0 {
		panic("catalog/tuning/gameplay_tuning.json: legendPoints.perObjective must be >= 0")
	}
	if t.LegendPoints.PerKillBaseDropChance < 0 || t.LegendPoints.PerKillBaseDropChance > 1 {
		panic(fmt.Sprintf("catalog/tuning/gameplay_tuning.json: legendPoints.perKillBaseDropChance must be in [0,1], got %v", t.LegendPoints.PerKillBaseDropChance))
	}
	if t.LegendPoints.PerKillBaseAmount < 0 {
		panic("catalog/tuning/gameplay_tuning.json: legendPoints.perKillBaseAmount must be >= 0")
	}
	switch t.LegendPoints.CommitMode {
	case "", legendPointCommitModeMatchEnd, legendPointCommitModeImmediate:
		// valid
	default:
		panic(fmt.Sprintf("catalog/tuning/gameplay_tuning.json: legendPoints.commitMode must be %q or %q, got %q",
			legendPointCommitModeMatchEnd, legendPointCommitModeImmediate, t.LegendPoints.CommitMode))
	}
	for unitType, override := range t.UnitOverrides {
		if override.LegendPointDropChance < 0 || override.LegendPointDropChance > 1 {
			panic(fmt.Sprintf("catalog/tuning/gameplay_tuning.json: unitOverrides[%q].legendPointDropChance must be in [0,1], got %v", unitType, override.LegendPointDropChance))
		}
		if override.LegendPointAmount < 0 {
			panic(fmt.Sprintf("catalog/tuning/gameplay_tuning.json: unitOverrides[%q].legendPointAmount must be >= 0, got %d", unitType, override.LegendPointAmount))
		}
	}
	if t.WaveUpgrade.TimerSeconds <= 0 {
		panic("catalog/tuning/gameplay_tuning.json: waveUpgrade.timerSeconds must be > 0")
	}
	if len(t.WaveUpgrade.BaseWeights) == 0 {
		panic("catalog/tuning/gameplay_tuning.json: waveUpgrade.baseWeights must not be empty")
	}
	if _, ok := upgradeRarityOrder[t.WaveUpgrade.MilestoneMinRarity]; !ok {
		panic("catalog/tuning/gameplay_tuning.json: waveUpgrade.milestoneMinRarity: unknown rarity " + t.WaveUpgrade.MilestoneMinRarity)
	}
	switch t.Experience.Mode {
	case experienceModeClassic, experienceModeSplit:
	default:
		panic(fmt.Sprintf("catalog/tuning/gameplay_tuning.json: experience.mode must be %q or %q, got %q", experienceModeClassic, experienceModeSplit, t.Experience.Mode))
	}
	if t.Experience.SplitDefaultXP < 0 {
		panic(fmt.Sprintf("catalog/tuning/gameplay_tuning.json: experience.splitDefaultXP must be >= 0, got %d", t.Experience.SplitDefaultXP))
	}
	if t.Experience.SplitEligibilityRadius <= 0 {
		panic(fmt.Sprintf("catalog/tuning/gameplay_tuning.json: experience.splitEligibilityRadius must be > 0, got %v", t.Experience.SplitEligibilityRadius))
	}
	gameplayTuningSingleton = t
}

// gameplayTuning returns the package-level gameplay tuning loaded at startup.
func gameplayTuning() GameplayTuning {
	return gameplayTuningSingleton
}

// ExportedGameplayTuning exposes the tuning singleton to packages outside the
// game package (e.g. HTTP handlers). Internal simulation code should use the
// unexported gameplayTuning() accessor.
func ExportedGameplayTuning() GameplayTuning {
	return gameplayTuningSingleton
}
