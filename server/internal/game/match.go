package game

import (
	"sync"
	"time"
)

type MatchClient interface {
	WriteJSON(v any) error
}

type Match struct {
	ID      string
	MapID   string
	State   *GameState

	mu      sync.RWMutex
	Clients map[MatchClient]struct{}

	loop *Loop
}

func NewMatch(id string, mapID string) *Match {
	state := NewGameState(GetMapConfigByID(mapID))

	match := &Match{
		ID:      id,
		MapID:   state.GetMapConfig().ID,
		State:   state,
		Clients: make(map[MatchClient]struct{}),
	}

	match.loop = NewLoop(state, match)
	match.loop.Start()

	return match
}

func (m *Match) AddClient(client MatchClient) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Clients[client] = struct{}{}
}

func (m *Match) RemoveClient(client MatchClient) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Clients, client)
}

func (m *Match) ListClients() []MatchClient {
	m.mu.RLock()
	defer m.mu.RUnlock()

	clients := make([]MatchClient, 0, len(m.Clients))
	for client := range m.Clients {
		clients = append(clients, client)
	}
	return clients
}

func (m *Match) PlayerCount() int {
	m.State.mu.RLock()
	defer m.State.mu.RUnlock()
	return len(m.State.Players)
}

func (m *Match) BroadcastSnapshot() {
	snapshot := m.State.Snapshot()
	snapshot.MatchID = m.ID
	snapshot.ServerNow = time.Now().UnixMilli()

	m.mu.RLock()
	defer m.mu.RUnlock()

	for client := range m.Clients {
		_ = client.WriteJSON(snapshot)
	}
}

func (m *Match) RemovePlayer(playerID string) {
	m.State.RemovePlayer(playerID)
}
