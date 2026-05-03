package game

import (
	"crypto/rand"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

type LobbyStatus string

const (
	LobbyStatusOpen    LobbyStatus = "open"
	LobbyStatusStarted LobbyStatus = "started"
	LobbyStatusClosed  LobbyStatus = "closed"
)

type Lobby struct {
	ID           string      `json:"id"`
	MapID        string      `json:"mapId"`
	MapName      string      `json:"mapName"`
	HostPlayerID string      `json:"hostPlayerId"`
	Players      []string    `json:"players"`
	MaxPlayers   int         `json:"maxPlayers"`
	CreatedAt    int64       `json:"createdAt"`
	LastActivity int64       `json:"-"`
	Status       LobbyStatus `json:"status"`
	MatchID      string      `json:"matchId,omitempty"`
}

var (
	ErrLobbyNotFound     = errors.New("lobby not found")
	ErrLobbyAlreadyStarted = errors.New("lobby already started")
	ErrLobbyClosed       = errors.New("lobby is closed")
	ErrLobbyFull         = errors.New("lobby is full")
	ErrNotHost           = errors.New("caller is not the host")
)

type LobbyManager struct {
	mu      sync.Mutex
	lobbies map[string]*Lobby
	stop    chan struct{}
}

func NewLobbyManager() *LobbyManager {
	lm := &LobbyManager{
		lobbies: make(map[string]*Lobby),
		stop:    make(chan struct{}),
	}
	go lm.cleanupLoop()
	return lm
}

func (lm *LobbyManager) Close() {
	close(lm.stop)
}

func (lm *LobbyManager) Create(mapID, hostPlayerID string) (*Lobby, error) {
	entry, ok := GetMapCatalogEntryByID(mapID)
	if !ok {
		return nil, fmt.Errorf("map %q not found", mapID)
	}

	maxPlayers := 0
	for _, b := range entry.Map.Buildings {
		if b.BuildingType == "spawn-point" {
			maxPlayers++
		}
	}
	if maxPlayers < 1 {
		return nil, fmt.Errorf("map %q has no spawn points", mapID)
	}

	id, err := lm.generateID()
	if err != nil {
		return nil, fmt.Errorf("generate lobby id: %w", err)
	}

	now := time.Now().UnixMilli()
	lobby := &Lobby{
		ID:           id,
		MapID:        mapID,
		MapName:      entry.Name,
		HostPlayerID: hostPlayerID,
		Players:      []string{hostPlayerID},
		MaxPlayers:   maxPlayers,
		CreatedAt:    now,
		LastActivity: now,
		Status:       LobbyStatusOpen,
	}

	lm.mu.Lock()
	lm.lobbies[id] = lobby
	lm.mu.Unlock()

	return lm.shallowCopy(lobby), nil
}

func (lm *LobbyManager) List() []*Lobby {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	result := make([]*Lobby, 0)
	for _, l := range lm.lobbies {
		if l.Status == LobbyStatusOpen {
			result = append(result, lm.shallowCopy(l))
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt > result[j].CreatedAt
	})

	return result
}

func (lm *LobbyManager) Get(id string) (*Lobby, bool) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	l, ok := lm.lobbies[id]
	if !ok {
		return nil, false
	}
	return lm.shallowCopy(l), true
}

func (lm *LobbyManager) Join(id, playerID string) (*Lobby, error) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	l, ok := lm.lobbies[id]
	if !ok {
		return nil, ErrLobbyNotFound
	}

	if l.Status == LobbyStatusStarted {
		return nil, ErrLobbyAlreadyStarted
	}
	if l.Status == LobbyStatusClosed {
		return nil, ErrLobbyClosed
	}

	for _, p := range l.Players {
		if p == playerID {
			l.LastActivity = time.Now().UnixMilli()
			return lm.shallowCopy(l), nil
		}
	}

	if len(l.Players) >= l.MaxPlayers {
		return nil, ErrLobbyFull
	}

	l.Players = append(l.Players, playerID)
	l.LastActivity = time.Now().UnixMilli()

	return lm.shallowCopy(l), nil
}

func (lm *LobbyManager) Leave(id, playerID string) (*Lobby, error) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	l, ok := lm.lobbies[id]
	if !ok {
		return nil, ErrLobbyNotFound
	}

	idx := -1
	for i, p := range l.Players {
		if p == playerID {
			idx = i
			break
		}
	}
	if idx == -1 {
		return nil, fmt.Errorf("player %q not in lobby", playerID)
	}

	l.Players = append(l.Players[:idx], l.Players[idx+1:]...)
	l.LastActivity = time.Now().UnixMilli()

	if playerID == l.HostPlayerID {
		l.Status = LobbyStatusClosed
	}

	return lm.shallowCopy(l), nil
}

func (lm *LobbyManager) Start(id, callerPlayerID string, manager *MatchManager) (*Lobby, error) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	l, ok := lm.lobbies[id]
	if !ok {
		return nil, ErrLobbyNotFound
	}
	if callerPlayerID != l.HostPlayerID {
		return nil, ErrNotHost
	}
	if l.Status == LobbyStatusStarted {
		return nil, ErrLobbyAlreadyStarted
	}
	if l.Status == LobbyStatusClosed {
		return nil, ErrLobbyClosed
	}

	match := manager.NewMatch(l.MapID)
	for _, p := range l.Players {
		match.State.EnsurePlayer(p)
	}

	l.MatchID = match.ID
	l.Status = LobbyStatusStarted
	l.LastActivity = time.Now().UnixMilli()

	return lm.shallowCopy(l), nil
}

func (lm *LobbyManager) cleanupStale() {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	now := time.Now().UnixMilli()
	for id, l := range lm.lobbies {
		age := now - l.LastActivity
		switch l.Status {
		case LobbyStatusClosed:
			if age > 30_000 {
				delete(lm.lobbies, id)
			}
		case LobbyStatusStarted:
			if age > 60_000 {
				delete(lm.lobbies, id)
			}
		case LobbyStatusOpen:
			if age > 300_000 {
				delete(lm.lobbies, id)
			}
		}
	}
}

func (lm *LobbyManager) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-lm.stop:
			return
		case <-ticker.C:
			lm.cleanupStale()
		}
	}
}

func (lm *LobbyManager) shallowCopy(l *Lobby) *Lobby {
	cp := *l
	cp.Players = append([]string(nil), l.Players...)
	return &cp
}

const lobbyIDAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func (lm *LobbyManager) generateID() (string, error) {
	for range 10 {
		buf := make([]byte, 6)
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		chars := make([]byte, 6)
		for i, b := range buf {
			chars[i] = lobbyIDAlphabet[int(b)%len(lobbyIDAlphabet)]
		}
		id := "lob-" + string(chars)
		if _, exists := lm.lobbies[id]; !exists {
			return id, nil
		}
	}
	return "", fmt.Errorf("failed to generate unique lobby id after 10 attempts")
}
