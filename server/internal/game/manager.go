package game

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// LegendPointCommitter persists end-of-match legend-point totals to a player's
// profile. Implemented by *profile.Manager; declared here so the game package
// does not import the profile package. CommitLegendPoints is called once per
// human player when a match transitions to game-over; implementations must be
// safe to call concurrently from the match's tick goroutine and must no-op
// when earned <= 0.
type LegendPointCommitter interface {
	CommitLegendPoints(playerID string, earned int) error
}

type MatchManager struct {
	mu        sync.RWMutex
	matches   map[string]*Match
	nextID    int
	committer LegendPointCommitter
}

func NewMatchManager() *MatchManager {
	return &MatchManager{
		matches: make(map[string]*Match),
		nextID:  1,
	}
}

// SetLegendPointCommitter wires a committer that will receive each human
// player's earned legend-point total on match-over. Passing nil disables the
// commit step (the default — tests do not need persistence).
func (m *MatchManager) SetLegendPointCommitter(c LegendPointCommitter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.committer = c
}

func (m *MatchManager) getCommitter() LegendPointCommitter {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.committer
}

func (m *MatchManager) NewMatch(mapID string) *Match {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.newMatchLocked(mapID)
}

func (m *MatchManager) newMatchLocked(mapID string) *Match {
	matchID := fmt.Sprintf("match-%d", m.nextID)
	m.nextID++

	match := NewMatch(matchID, mapID)
	m.matches[matchID] = match
	log.Printf("match created: id=%s mapID=%s seed=%d\n", matchID, mapID, match.State.MatchSeed())

	// Immediate-commit hook for legendPoints.commitMode == "immediate".
	// Fires per kill-drop; runs the actual profile write in a goroutine so
	// rollLegendPointDropLocked (called from the tick loop) returns instantly.
	match.State.SetImmediateLegendPointDropHandler(func(playerID string, amount int) {
		committer := m.getCommitter()
		if committer == nil {
			return
		}
		go func() {
			if err := committer.CommitLegendPoints(playerID, amount); err != nil {
				log.Printf("commit legend points (immediate): matchID=%s playerID=%s amount=%d err=%v",
					matchID, playerID, amount, err)
			}
		}()
	})

	match.loop.OnGameOver = func() {
		if committer := m.getCommitter(); committer != nil {
			for _, summary := range match.State.HumanPlayerMatchSummaries() {
				if summary.LegendPointsEarned <= 0 {
					continue
				}
				if err := committer.CommitLegendPoints(summary.PlayerID, summary.LegendPointsEarned); err != nil {
					log.Printf("commit legend points: matchID=%s playerID=%s earned=%d err=%v",
						matchID, summary.PlayerID, summary.LegendPointsEarned, err)
				}
			}
		}
		log.Printf("game over: scheduling match deletion id=%s\n", matchID)
		time.AfterFunc(15*time.Second, func() {
			m.DeleteMatch(matchID)
		})
	}

	return match
}

func (m *MatchManager) FindOrCreateMatch(mapID string) *Match {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, match := range m.matches {
		if match.MapID == mapID && match.PlayerCount() < 4 && !match.State.IsGameOver() {
			return match
		}
	}

	return m.newMatchLocked(mapID)
}

func (m *MatchManager) GetMatch(matchID string) (*Match, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	match, ok := m.matches[matchID]
	return match, ok
}

func (m *MatchManager) ListMatches() []*Match {
	m.mu.RLock()
	defer m.mu.RUnlock()

	matches := make([]*Match, 0, len(m.matches))
	for _, match := range m.matches {
		matches = append(matches, match)
	}
	return matches
}

func (m *MatchManager) DeleteMatch(matchID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if match, ok := m.matches[matchID]; ok {
		match.Stop()
		delete(m.matches, matchID)
	}
}

// IsPlayerInActiveMatch returns true when playerID has an active (non-game-over)
// presence in any match currently tracked by this manager. Used by purchase
// handlers to reject profile changes while a player is mid-match.
func (m *MatchManager) IsPlayerInActiveMatch(playerID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, match := range m.matches {
		if match.State.IsGameOver() {
			continue
		}
		if match.HasPlayer(playerID) {
			return true
		}
	}
	return false
}

// EvictPlayerFromOtherMatches enforces the "one active match per player"
// rule: any match the player still occupies (or has a pending disconnect-
// grace timer in) other than exceptMatchID has the player removed
// immediately. Empty exceptMatchID evicts from EVERY match. Matches that
// become empty after eviction are deleted. Called from the join_match
// handler so starting a new game cleanly closes any prior match the
// player was in.
func (m *MatchManager) EvictPlayerFromOtherMatches(playerID, exceptMatchID string) {
	// Snapshot matches under read lock; mutate under per-match locks below
	// to avoid the m.mu / match.mu / DeleteMatch lock ordering trap.
	m.mu.RLock()
	candidates := make([]*Match, 0, len(m.matches))
	for _, match := range m.matches {
		if match.ID == exceptMatchID {
			continue
		}
		candidates = append(candidates, match)
	}
	m.mu.RUnlock()

	for _, match := range candidates {
		hadGrace := match.CancelPlayerRemoval(playerID)
		hadPlayer := match.HasPlayer(playerID)
		if !hadGrace && !hadPlayer {
			continue
		}
		match.RemovePlayer(playerID)
		log.Printf("evict: player=%s from prior match=%s (joining new game)", playerID, match.ID)
		if match.ClientCount() == 0 && match.PendingCleanupCount() == 0 {
			m.DeleteMatch(match.ID)
		}
	}
}
