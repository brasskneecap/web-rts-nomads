package game

import (
	"fmt"
	"log"
	"log/slog"
	"sync"
	"time"
)

// DominionPointCommitter persists end-of-match dominion-point totals to a player's
// profile. Implemented by *profile.Manager; declared here so the game package
// does not import the profile package. CommitDominionPoints is called once per
// human player when a match transitions to game-over; implementations must be
// safe to call concurrently from the match's tick goroutine and must no-op
// when earned <= 0.
type DominionPointCommitter interface {
	CommitDominionPoints(playerID string, earned int) error
}

// RecipeRecorder persists a crafted recipe into a player's profile. Declared
// in package game so the game package does not import profile (the
// *profile.Manager satisfies it). Mirrors DominionPointCommitter.
type RecipeRecorder interface {
	RecordKnownRecipe(playerID, recipeID string) error
}

type MatchManager struct {
	mu             sync.RWMutex
	matches        map[string]*Match
	nextID         int
	committer      DominionPointCommitter
	recipeRecorder RecipeRecorder
}

func NewMatchManager() *MatchManager {
	return &MatchManager{
		matches: make(map[string]*Match),
		nextID:  1,
	}
}

// SetDominionPointCommitter wires a committer that will receive each human
// player's earned dominion-point total on match-over. Passing nil disables the
// commit step (the default — tests do not need persistence).
func (m *MatchManager) SetDominionPointCommitter(c DominionPointCommitter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.committer = c
}

func (m *MatchManager) getCommitter() DominionPointCommitter {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.committer
}

// SetRecipeRecorder wires the recorder that persists a crafted recipe to a
// player's profile. Passing nil disables recording (the default — tests do not
// need persistence).
func (m *MatchManager) SetRecipeRecorder(r RecipeRecorder) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recipeRecorder = r
}

func (m *MatchManager) getRecipeRecorder() RecipeRecorder {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.recipeRecorder
}

func (m *MatchManager) NewMatch(mapID string) *Match {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.newMatchLocked(mapID)
}

func (m *MatchManager) newMatchLocked(mapID string) *Match {
	return m.newMatchLockedEphemeral(mapID, false)
}

// NewEphemeralMatch creates a fresh throwaway match for editor playtesting.
// It is registered so snapshots/commands route, but FindOrCreateMatch never
// reuses it (see the Ephemeral skip there), and its reward hooks no-op.
func (m *MatchManager) NewEphemeralMatch(mapID string) *Match {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.newMatchLockedEphemeral(mapID, true)
}

func (m *MatchManager) newMatchLockedEphemeral(mapID string, ephemeral bool) *Match {
	matchID := fmt.Sprintf("match-%d", m.nextID)
	m.nextID++

	match := newMatchWithEphemeral(matchID, mapID, ephemeral)
	m.matches[matchID] = match
	log.Printf("match created: id=%s mapID=%s seed=%d ephemeral=%t\n", matchID, mapID, match.State.MatchSeed(), ephemeral)

	// Immediate-commit hook for dominionPoints.commitMode == "immediate".
	// Fires per kill-drop; runs the actual profile write in a goroutine so
	// rollDominionPointDropLocked (called from the tick loop) returns instantly.
	match.State.SetImmediateDominionPointDropHandler(func(playerID string, amount int) {
		if match.State.Ephemeral {
			return
		}
		committer := m.getCommitter()
		if committer == nil {
			return
		}
		go func() {
			if err := committer.CommitDominionPoints(playerID, amount); err != nil {
				log.Printf("commit dominion points (immediate): matchID=%s playerID=%s amount=%d err=%v",
					matchID, playerID, amount, err)
			}
		}()
	})

	// Post-craft hook: records the crafted recipe to the player's persistent
	// profile. The handler is already invoked via `go` inside
	// handleCraftItemLocked, so this closure runs off-thread — no extra
	// goroutine needed here. Captures only the two string args + recorder.
	match.State.SetRecipeCraftedHandler(func(playerID, recipeID string) {
		if match.State.Ephemeral {
			return
		}
		recorder := m.getRecipeRecorder()
		if recorder == nil {
			return
		}
		if err := recorder.RecordKnownRecipe(playerID, recipeID); err != nil {
			slog.Warn("RecordKnownRecipe failed", "playerID", playerID, "recipeID", recipeID, "err", err)
		}
	})

	match.loop.OnGameOver = func() {
		if committer := m.getCommitter(); committer != nil && !match.State.Ephemeral {
			for _, summary := range match.State.HumanPlayerMatchSummaries() {
				if summary.DominionPointsEarned <= 0 {
					continue
				}
				if err := committer.CommitDominionPoints(summary.PlayerID, summary.DominionPointsEarned); err != nil {
					log.Printf("commit dominion points: matchID=%s playerID=%s earned=%d err=%v",
						matchID, summary.PlayerID, summary.DominionPointsEarned, err)
				}
			}
		}
		// A continue-play match that ended in VICTORY keeps simulating so the
		// player can pick "Continue Playing" — it must NOT auto-tear-down here.
		// It is cleaned up when the player explicitly exits (leave_match →
		// DeleteMatch) or fully disconnects. A defeat (or a non-continue match)
		// leaves the sim halted, so it still gets the 15s end-screen wind-down.
		if !match.State.IsSimulationHalted() {
			log.Printf("game over (continue-play): match id=%s kept alive until player exits\n", matchID)
			return
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
		if match.MapID == mapID && match.PlayerCount() < 4 && !match.State.IsGameOver() && !match.State.Ephemeral {
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
