package game

import (
	"sync"
	"time"
	"webrts/server/pkg/protocol"
)

// PlayerRemovalGrace is the delay between a WebSocket disconnect and the
// actual removal of the player's in-match state. A reconnect within this
// window cancels the removal and the player's army is preserved.
const PlayerRemovalGrace = 30 * time.Second

type MatchClient interface {
	WriteJSON(v any) error
	// WriteBytes sends a pre-marshaled JSON payload. Used by BroadcastSnapshot
	// so the per-tick snapshot can be marshaled while still holding the game
	// state lock (avoiding a concurrent-mutation race in json.Marshal over
	// shared map fields), then sent without the lock.
	WriteBytes(payload []byte) error
	// PlayerID returns the player ID associated with this connection. Used by
	// BroadcastSnapshot to build a per-player FOW-filtered snapshot.
	PlayerID() string
}

type Match struct {
	ID    string
	MapID string
	State *GameState

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

// SetCampaignLevel installs the objectives authored on the named campaign
// level onto the match's GameState. Idempotent and no-op when levelID is
// empty (Custom Game lobbies). When the levelID is non-empty but does not
// resolve in the campaign catalog, logs and leaves the objectives slice
// empty — a stale id from the client should not fail the whole match start.
//
// Called by LobbyManager.Start immediately after `manager.NewMatch`. The
// loop has already started ticking by then (see NewMatch above), so the
// objectives may briefly be missing for tick 0; tick evaluation is monotone
// and self-corrects on tick 1.
func (m *Match) SetCampaignLevel(levelID string) {
	m.State.SetCampaignLevelLocked(levelID)
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
	// Snapshot the client set under the lock, then release before writing.
	// Holding the lock across WriteJSON serialises writes and blocks
	// AddClient/RemoveClient behind a slow or stuck client's write deadline.
	m.mu.RLock()
	clients := make([]MatchClient, 0, len(m.Clients))
	for c := range m.Clients {
		clients = append(clients, c)
	}
	m.mu.RUnlock()

	// Drain the per-tick loot notification queue once before the client loop.
	// Keyed by playerID → slice so the same player can collect multiple chests
	// in one tick (e.g. two units on two different chests both arrive this tick).
	// DrainPendingLootNotifications acquires s.mu internally.
	var lootNotifsByPlayer map[string][]protocol.LootCollectedNotification
	if notifs := m.State.DrainPendingLootNotifications(); len(notifs) > 0 {
		lootNotifsByPlayer = make(map[string][]protocol.LootCollectedNotification, len(notifs))
		for _, n := range notifs {
			lootNotifsByPlayer[n.PlayerID] = append(lootNotifsByPlayer[n.PlayerID], n)
		}
	}

	serverNow := time.Now().UnixMilli()
	profileSection("snapshotBroadcast", func() {
		for _, client := range clients {
			// Marshal the per-player snapshot to bytes WHILE the state
			// RLock is held (inside MarshalSnapshotForPlayer). This is
			// the load-bearing piece of the snapshot-race fix: several
			// snapshot fields (notably BuildingTile.Metadata) alias live
			// state maps that the tick loop mutates every tick, so the
			// JSON encoder MUST run under the lock or it can panic with
			// "index out of range" inside encoding/json's mapEncoder
			// when the live map grows mid-marshal.
			var (
				payload []byte
				err     error
			)
			profileSection("snapshotBuild", func() {
				payload, err = m.State.MarshalSnapshotForPlayer(client.PlayerID(), m.ID, serverNow)
			})
			if err != nil {
				// Snapshot marshal failures are bugs (the schema is
				// fully JSON-encodable); skip this client for this
				// tick rather than spamming the log.
				continue
			}
			profileClientSend(client.PlayerID(), func() {
				_ = client.WriteBytes(payload)
			})

			// Push any loot-collected notifications for this player. Sent
			// after the snapshot so the client can correlate: resources
			// already appear in the snapshot by the time the toasts fire.
			// LootCollectedNotification.Resources is allocated fresh by
			// grantLootDropToPlayerLocked (deep-copied from the chest), so
			// these messages don't alias live state and WriteJSON is safe.
			for _, n := range lootNotifsByPlayer[client.PlayerID()] {
				_ = client.WriteJSON(n)
			}
		}
	})

	// Advance the obstacle-delta baseline ONCE after every client has been
	// served. snapshotObstacleDeltasLocked is a pure read, so each viewer's
	// SnapshotForPlayer above sees the same removals + metadata patches;
	// this commit clears pendingObstacleRemovals and updates the last-sent
	// worker counts so the NEXT broadcast only ships what changed since
	// this one.
	m.State.CommitObstacleDeltaStateAfterBroadcast()

	sendProfileBroadcastComplete()
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
