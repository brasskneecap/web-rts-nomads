package game

import (
	"log"
	mrand "math/rand"
	"time"
)

type upgradeWeight struct {
	def    UpgradeDef
	weight float64
}

// generateUpgradeOffersLocked returns 3 upgrade cards for playerID.
// Filters groups the player has already maxed, weights by rarity (wave-scaled),
// and on milestone waves guarantees at least one card of milestoneMinRarity+.
// Caller must hold s.mu.
func (s *GameState) generateUpgradeOffersLocked(playerID string) []UpgradeDef {
	player := s.Players[playerID]
	if player == nil {
		return nil
	}
	tuning := gameplayTuning().WaveUpgrade
	wave := s.WaveManager.CurrentWave

	// Build weighted pool — exclude groups at/above the effective stack cap.
	var pool []upgradeWeight
	for _, def := range listUpgradeDefs() {
		if !def.Unlimited {
			effectiveCap := def.MaxStacks
			if player.UpgradeState.MaxUpgradeStacks > effectiveCap {
				effectiveCap = player.UpgradeState.MaxUpgradeStacks
			}
			if player.UpgradeState.UpgradeStacks[def.Group] >= effectiveCap {
				continue
			}
		}
		base := tuning.BaseWeights[def.Rarity]
		scale := tuning.RarityScalePerWave[def.Rarity]
		w := base + scale*float64(wave-1)
		if w <= 0 {
			continue
		}
		pool = append(pool, upgradeWeight{def: def, weight: w})
	}
	if len(pool) == 0 {
		return nil
	}

	// Detect milestone wave.
	isMilestone := false
	for _, mw := range tuning.MilestoneWaves {
		if wave == mw {
			isMilestone = true
			break
		}
	}
	minRarityRank := -1
	if isMilestone && tuning.MilestoneMinRarity != "" {
		minRarityRank = upgradeRarityOrder[tuning.MilestoneMinRarity]
	}

	selected := make([]UpgradeDef, 0, 3)
	usedIDs := make(map[string]bool)

	// On a milestone wave, force the first pick from the epic+ sub-pool.
	if minRarityRank >= 0 {
		var epicPool []upgradeWeight
		for _, w := range pool {
			if upgradeRarityOrder[w.def.Rarity] >= minRarityRank {
				epicPool = append(epicPool, w)
			}
		}
		if len(epicPool) > 0 {
			pick := weightedSampleUpgrade(s.rngSpawn, epicPool)
			selected = append(selected, pick)
			usedIDs[pick.ID] = true
		}
	}

	// Fill remaining slots from the full pool (excluding already selected).
	for len(selected) < 3 {
		var available []upgradeWeight
		for _, w := range pool {
			if !usedIDs[w.def.ID] {
				available = append(available, w)
			}
		}
		if len(available) == 0 {
			break
		}
		pick := weightedSampleUpgrade(s.rngSpawn, available)
		usedIDs[pick.ID] = true
		selected = append(selected, pick)
	}
	return selected
}

func weightedSampleUpgrade(rng *mrand.Rand, pool []upgradeWeight) UpgradeDef {
	total := 0.0
	for _, w := range pool {
		total += w.weight
	}
	r := rng.Float64() * total
	for _, w := range pool {
		r -= w.weight
		if r <= 0 {
			return w.def
		}
	}
	return pool[len(pool)-1].def
}

// enterWaveUpgradePhaseLocked initialises per-player offer state for the
// current wave. Must be called once when the wave state transitions to "upgrade".
// Caller must hold s.mu.
func (s *GameState) enterWaveUpgradePhaseLocked() {
	tuning := gameplayTuning().WaveUpgrade
	deadlineMs := time.Now().UnixMilli() + int64(tuning.TimerSeconds*1000)
	humanCount := 0
	for playerID, player := range s.Players {
		if playerID == enemyPlayerID {
			continue
		}
		humanCount++
		player.UpgradeState.RerollsRemaining = player.UpgradeState.MaxRerolls
		player.UpgradeState.CurrentOffers = s.generateUpgradeOffersLocked(playerID)
		player.UpgradeState.OfferDeadlineMs = deadlineMs
		player.UpgradeState.Resolved = false
		log.Printf("[UPGRADE] player=%s offers=%d timerSeconds=%.0f", playerID, len(player.UpgradeState.CurrentOffers), tuning.TimerSeconds)
	}
	if humanCount == 0 {
		log.Printf("[UPGRADE] WARNING: no human players found")
	}
}

// tickUpgradePhaseLocked checks per-player deadlines and advances to "prep"
// once all players have resolved. Caller must hold s.mu.
func (s *GameState) tickUpgradePhaseLocked() {
	now := time.Now().UnixMilli()
	for playerID, player := range s.Players {
		if playerID == enemyPlayerID || player.UpgradeState.Resolved {
			continue
		}
		if now >= player.UpgradeState.OfferDeadlineMs {
			log.Printf("[UPGRADE] auto-pick fired: player=%s now=%d deadline=%d", playerID, now, player.UpgradeState.OfferDeadlineMs)
			if len(player.UpgradeState.CurrentOffers) > 0 {
				s.applyUpgradeLocked(playerID, player.UpgradeState.CurrentOffers[0].ID, 0)
			}
			player.UpgradeState.Resolved = true
		}
	}
	if s.waveUpgradeAllResolvedLocked() {
		log.Printf("[UPGRADE] all resolved → prep")
		s.WaveManager.State = "prep"
		s.WaveManager.Timer = s.WaveManager.PrepDuration
	}
}

func (s *GameState) waveUpgradeAllResolvedLocked() bool {
	for playerID, player := range s.Players {
		if playerID == enemyPlayerID {
			continue
		}
		if !player.UpgradeState.Resolved {
			return false
		}
	}
	return true
}

// HandleWaveUpgradeReroll processes a player's reroll request.
func (s *GameState) HandleWaveUpgradeReroll(playerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	player := s.Players[playerID]
	if player == nil || player.UpgradeState.Resolved {
		return
	}
	if s.WaveManager.State != "upgrade" {
		return
	}
	if player.UpgradeState.RerollsRemaining <= 0 {
		return
	}
	player.UpgradeState.RerollsRemaining--
	player.UpgradeState.CurrentOffers = s.generateUpgradeOffersLocked(playerID)
}

