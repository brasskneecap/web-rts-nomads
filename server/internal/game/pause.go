package game

import "time"

// HandleSetPause toggles the simulation pause flag. Any human player in the
// match can pause or resume; the player ID is recorded in PausedBy so the
// snapshot can render "Paused by <player>" on every client.
//
// While paused, Update() returns immediately and the wave-upgrade auto-pick
// deadline is frozen. On resume, OfferDeadlineMs for every player in the
// upgrade phase is shifted forward by the elapsed pause duration so a player
// who was halfway through their selection still has the same remaining time
// after the resume.
func (s *GameState) HandleSetPause(playerID string, paused bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if playerID == enemyPlayerID || playerID == "" {
		return
	}
	if _, ok := s.Players[playerID]; !ok {
		return
	}

	if paused {
		if s.Paused {
			return
		}
		s.Paused = true
		s.PausedBy = playerID
		s.pausedAtMs = time.Now().UnixMilli()
		return
	}

	if !s.Paused {
		return
	}
	now := time.Now().UnixMilli()
	if s.pausedAtMs > 0 && now > s.pausedAtMs {
		elapsed := now - s.pausedAtMs
		// Shift any in-flight wave-upgrade deadlines so the visible
		// selection timer resumes from where it left off.
		for _, player := range s.Players {
			if player == nil {
				continue
			}
			if player.UpgradeState.OfferDeadlineMs > 0 && !player.UpgradeState.Resolved {
				player.UpgradeState.OfferDeadlineMs += elapsed
			}
		}
	}
	s.Paused = false
	s.PausedBy = ""
	s.pausedAtMs = 0
}

// IsPaused reports whether the simulation is currently paused. Safe to call
// without holding s.mu — callers that need a consistent paired read of
// (Paused, PausedBy) should snapshot via SnapshotForPlayer instead.
func (s *GameState) IsPaused() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Paused
}
