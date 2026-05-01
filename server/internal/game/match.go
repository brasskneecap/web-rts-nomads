package game

import (
	"sync"
	"time"
)

// PlayerRemovalGrace is the delay between a WebSocket disconnect and the
// actual removal of the player's in-match state. A reconnect within this
// window cancels the removal and the player's army is preserved.
const PlayerRemovalGrace = 30 * time.Second

type MatchClient interface {
	WriteJSON(v any) error
}

type Match struct {
	ID      string
	MapID   string
	State   *GameState

	mu                    sync.RWMutex
	Clients               map[MatchClient]struct{}
	pendingPlayerCleanups map[string]*time.Timer

	loop *Loop
}

func NewMatch(id string, mapID string) *Match {
	state := NewGameState(GetMapConfigByID(mapID))

	match := &Match{
		ID:                    id,
		MapID:                 state.GetMapConfig().ID,
		State:                 state,
		Clients:               make(map[MatchClient]struct{}),
		pendingPlayerCleanups: make(map[string]*time.Timer),
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
	count := 0
	for id := range m.State.Players {
		if id != enemyPlayerID {
			count++
		}
	}
	return count
}

func (m *Match) HasPlayer(playerID string) bool {
	m.State.mu.RLock()
	defer m.State.mu.RUnlock()
	_, ok := m.State.Players[playerID]
	return ok
}

func (m *Match) BroadcastSnapshot() {
	snapshot := m.State.Snapshot()
	snapshot.MatchID = m.ID
	snapshot.ServerNow = time.Now().UnixMilli()

	// Snapshot the client set under the lock, then release before writing.
	// Holding the lock across WriteJSON serialises writes and blocks
	// AddClient/RemoveClient behind a slow or stuck client's write deadline.
	m.mu.RLock()
	clients := make([]MatchClient, 0, len(m.Clients))
	for c := range m.Clients {
		clients = append(clients, c)
	}
	m.mu.RUnlock()

	for _, client := range clients {
		_ = client.WriteJSON(snapshot)
	}
}

func (m *Match) RemovePlayer(playerID string) {
	m.State.RemovePlayer(playerID)
}

// SchedulePlayerRemoval arranges for playerID's in-match state to be
// removed after grace has elapsed. If a pending removal already exists
// for that player it is cancelled first (safety; shouldn't happen in
// normal operation). The manager is needed so the timer callback can
// delete the match if it becomes empty after the removal.
func (m *Match) SchedulePlayerRemoval(playerID string, grace time.Duration, manager *MatchManager) {
	m.mu.Lock()
	// Cancel any pre-existing timer for this player (idempotency).
	if existing, ok := m.pendingPlayerCleanups[playerID]; ok {
		existing.Stop()
	}

	t := time.AfterFunc(grace, func() {
		m.RemovePlayer(playerID)

		// Remove the timer entry and check whether the match is now empty.
		m.mu.Lock()
		delete(m.pendingPlayerCleanups, playerID)
		shouldDelete := len(m.Clients) == 0 && len(m.pendingPlayerCleanups) == 0
		m.mu.Unlock()

		if shouldDelete {
			manager.DeleteMatch(m.ID)
		}
	})
	m.pendingPlayerCleanups[playerID] = t
	m.mu.Unlock()
}

// CancelPlayerRemoval cancels a pending scheduled removal for playerID.
// Returns true if a pending cleanup was cancelled (i.e. this is a reconnect),
// false if there was nothing to cancel.
func (m *Match) CancelPlayerRemoval(playerID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.pendingPlayerCleanups[playerID]
	if !ok {
		return false
	}
	t.Stop()
	delete(m.pendingPlayerCleanups, playerID)
	return true
}

func (m *Match) ClientCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.Clients)
}

// PendingCleanupCount returns the number of players with a pending removal
// timer. Used to gate match deletion.
func (m *Match) PendingCleanupCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.pendingPlayerCleanups)
}

func (m *Match) Stop() {
	// Stop all pending player-removal timers so their callbacks don't fire
	// into a deleted match.
	m.mu.Lock()
	for playerID, t := range m.pendingPlayerCleanups {
		t.Stop()
		delete(m.pendingPlayerCleanups, playerID)
	}
	m.mu.Unlock()

	m.loop.Stop()
}
