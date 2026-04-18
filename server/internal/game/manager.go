package game

import (
	"fmt"
	"log"
	"sync"
)

type MatchManager struct {
	mu      sync.RWMutex
	matches map[string]*Match
	nextID  int
}

func NewMatchManager() *MatchManager {
	return &MatchManager{
		matches: make(map[string]*Match),
		nextID:  1,
	}
}

func (m *MatchManager) FindOrCreateMatch(mapID string) *Match {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, match := range m.matches {
		if match.MapID == mapID && match.PlayerCount() < 4 {
			return match
		}
	}

	matchID := fmt.Sprintf("match-%d", m.nextID)
	m.nextID++

	match := NewMatch(matchID, mapID)
	m.matches[matchID] = match
	log.Printf("match created: id=%s mapID=%s seed=%d\n", matchID, mapID, match.State.MatchSeed())

	return match
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
