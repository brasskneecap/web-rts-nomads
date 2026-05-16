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
	BuffSlots     BuffSlotsTuning                    `json:"buffSlots"`
	WaveUpgrade   WaveUpgradeTuning                  `json:"waveUpgrade"`
	UnitOverrides map[string]UnitLegendPointOverride `json:"unitOverrides"`
}

// LegendPointsTuning holds all legend-point earning rates.
type LegendPointsTuning struct {
	WinBonus              int     `json:"winBonus"`
	LossConsolation       int     `json:"lossConsolation"`
	PerObjective          int     `json:"perObjective"`
	PerKillBaseDropChance float64 `json:"perKillBaseDropChance"`
	PerKillBaseAmount     int     `json:"perKillBaseAmount"`
}

// BuffSlotsTuning controls how many player buffs can be active simultaneously.
type BuffSlotsTuning struct {
	MaxActive int `json:"maxActive"`
}

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
	if t.BuffSlots.MaxActive <= 0 {
		panic(fmt.Sprintf("catalog/tuning/gameplay_tuning.json: buffSlots.maxActive must be > 0, got %d", t.BuffSlots.MaxActive))
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
