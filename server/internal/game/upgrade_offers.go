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
	if wave < 1 {
		// Start-of-match bonus is generated before wave 1 (CurrentWave == 0). The
		// per-wave weight/milestone math is defined from wave 1 up; clamp so the
		// pre-wave-1 offer behaves like a wave-1 offer instead of underflowing the
		// per-wave rarity scale. During a real upgrade phase CurrentWave is always
		// >= 1, so this is a no-op there.
		wave = 1
	}

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
		if playerID == enemyPlayerID || playerID == neutralPlayerID {
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

// startWaveBonusEnabled reports whether player should be shown the wave-upgrade
// pick at match start (before wave 1). The player.json "startWaveBonus" flag is
// a testing toggle; a player who owns a start-bonus advancement always gets the
// pick, overriding the toggle.
func startWaveBonusEnabled(player *Player) bool {
	if playerHasStartWaveBonusAdvancement(player) {
		return true
	}
	return playerConfig().StartWaveBonus
}

// playerHasStartWaveBonusAdvancement reports whether the player owns an
// advancement that grants the match-start wave bonus. No such advancement exists
// yet, so this is the single extension point the future node hooks into: when it
// is added (register its effect kind in advancementEffectRegistry and match on it
// here — e.g. by scanning player.AcquiredAdvancements for that node's ID),
// owning it will force the start bonus on even when the player.json toggle is
// false. Until then it returns false and the toggle is the only source.
func playerHasStartWaveBonusAdvancement(player *Player) bool {
	return false
}

// maybeGrantStartWaveBonusLocked presents the wave-upgrade pick to a freshly
// joined player at match start, when enabled. It only fires on wave-enabled maps
// (the "upgrade" phase is only ticked/resolved when WaveManager.Enabled) and only
// before wave 1 (CurrentWave == 0); a player joining after the first wave gets
// nothing. It puts the wave manager into the "upgrade" phase so the normal
// snapshot/resolution flow (buildWaveUpgradeSnapshotLocked drives the modal,
// tickUpgradePhaseLocked resolves it) runs unchanged, and on resolution hands off
// to the prep countdown for wave 1. Per-player: it only (re)generates offers for
// the passed player, so a player who already picked is not reset when a later one
// joins. Caller must hold s.mu.
func (s *GameState) maybeGrantStartWaveBonusLocked(player *Player) {
	if player == nil {
		return
	}
	wm := &s.WaveManager
	if !wm.Enabled || wm.CurrentWave != 0 {
		return
	}
	// Only the pre-wave-1 window: "prep" is the fresh-match state; "upgrade"
	// means an earlier-joining player already opened the start bonus.
	if wm.State != "prep" && wm.State != "upgrade" {
		return
	}
	if !startWaveBonusEnabled(player) {
		return
	}
	tuning := gameplayTuning().WaveUpgrade
	wm.State = "upgrade"
	player.UpgradeState.RerollsRemaining = player.UpgradeState.MaxRerolls
	player.UpgradeState.CurrentOffers = s.generateUpgradeOffersLocked(player.ID)
	player.UpgradeState.OfferDeadlineMs = time.Now().UnixMilli() + int64(tuning.TimerSeconds*1000)
	player.UpgradeState.Resolved = false
}

// tickUpgradePhaseLocked checks per-player deadlines and advances to "prep"
// once all players have resolved. Caller must hold s.mu.
func (s *GameState) tickUpgradePhaseLocked() {
	now := time.Now().UnixMilli()
	for playerID, player := range s.Players {
		if playerID == enemyPlayerID || playerID == neutralPlayerID || player.UpgradeState.Resolved {
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
		wm := &s.WaveManager
		// CurrentWave == 0 is the start-of-match bonus (see
		// maybeGrantStartWaveBonusLocked), NOT a between-wave upgrade. It must
		// hand back to the normal prep countdown for wave 1 — advancing here
		// would skip prep and drop straight into wave 1. Only a real
		// between-wave resolution (CurrentWave >= 1) uses the continuous
		// advance-immediately path below.
		if wm.Continuous && wm.CurrentWave >= 1 {
			// Continuous mode: the upgrade pick WAS the between-wave pause, so
			// release the next wave immediately rather than re-running a prep
			// countdown. Enemies from prior waves persist and accumulate; the
			// CurrentWave bump drives the neutral-camp reset in
			// tickNeutralCampsLocked. Mirrors the prep→active re-arm in
			// tickWaveLocked.
			wm.CurrentWave++
			wm.State = "active"
			wm.Timer = 0
			wm.SpawnedThisWave = 0
			for _, u := range s.Units {
				if u == nil {
					continue
				}
				u.PathDiagnostics = PathDiagnostics{}
				u.UnreachableBuildingStrikeCount = 0
			}
			s.resetWaveSpawnTimersLocked(wm.CurrentWave)
			log.Printf("[UPGRADE] all resolved → active wave %d (continuous)", wm.CurrentWave)
		} else {
			log.Printf("[UPGRADE] all resolved → prep")
			wm.State = "prep"
			wm.Timer = wm.PrepDuration
		}
	}
}

func (s *GameState) waveUpgradeAllResolvedLocked() bool {
	for playerID, player := range s.Players {
		if playerID == enemyPlayerID || playerID == neutralPlayerID {
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
