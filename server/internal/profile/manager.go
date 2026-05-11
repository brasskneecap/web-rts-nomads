package profile

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// Manager provides per-player serialized access to profiles. Concurrent calls
// for the same playerID are serialized; calls for different players run in
// parallel.
type Manager struct {
	store  Store
	muMap  sync.Map // playerID string → *sync.Mutex
}

// NewManager returns a Manager backed by files in profilesDir. If profilesDir
// is empty the value of WEBRTS_PROFILES_DIR is used; if that is also empty,
// "./profiles" is the default.
func NewManager(profilesDir string) *Manager {
	if profilesDir == "" {
		profilesDir = os.Getenv("WEBRTS_PROFILES_DIR")
	}
	if profilesDir == "" {
		profilesDir = "./profiles"
	}
	return &Manager{store: NewFileStore(profilesDir)}
}

// playerMu returns the per-player mutex, creating it if this is the first call
// for that player. Uses sync.Map to avoid a global lock on the map itself.
func (m *Manager) playerMu(playerID string) *sync.Mutex {
	actual, _ := m.muMap.LoadOrStore(playerID, &sync.Mutex{})
	return actual.(*sync.Mutex)
}

// Get loads the profile for playerID. Returns nil (not an error) when no
// profile file exists yet.
func (m *Manager) Get(playerID string) (*PlayerProfile, error) {
	mu := m.playerMu(playerID)
	mu.Lock()
	defer mu.Unlock()
	return m.store.Load(playerID)
}

// Save writes p to persistent storage, overwriting any previous file.
func (m *Manager) Save(playerID string, p *PlayerProfile) error {
	mu := m.playerMu(playerID)
	mu.Lock()
	defer mu.Unlock()
	p.UpdatedAtUnix = time.Now().Unix()
	return m.store.Save(playerID, p)
}

// WithLocked loads the profile for playerID (creating a default if absent),
// calls fn with a pointer to it, and saves the result. The player mutex is
// held for the entire duration, so fn must not call Get/Save for the same
// playerID.
func (m *Manager) WithLocked(playerID string, fn func(*PlayerProfile) error) error {
	mu := m.playerMu(playerID)
	mu.Lock()
	defer mu.Unlock()

	p, err := m.store.Load(playerID)
	if err != nil {
		return fmt.Errorf("profile.WithLocked load %q: %w", playerID, err)
	}
	if p == nil {
		p = newDefaultProfile(playerID, DefaultCommanderID, nil)
	}
	if err := fn(p); err != nil {
		return err
	}
	p.UpdatedAtUnix = time.Now().Unix()
	return m.store.Save(playerID, p)
}

// GetOrCreate returns the existing profile for playerID, or creates and
// persists a fresh default profile if none exists. defaultBuffIDs are added to
// UnlockedBuffIDs on the new profile; defaultCommanderID is set as the owned
// and selected commander.
func (m *Manager) GetOrCreate(playerID string, defaultCommanderID string, defaultBuffIDs []string) (*PlayerProfile, error) {
	mu := m.playerMu(playerID)
	mu.Lock()
	defer mu.Unlock()

	p, err := m.store.Load(playerID)
	if err != nil {
		return nil, fmt.Errorf("profile.GetOrCreate load %q: %w", playerID, err)
	}
	if p != nil {
		return p, nil
	}

	p = newDefaultProfile(playerID, defaultCommanderID, defaultBuffIDs)
	p.UpdatedAtUnix = time.Now().Unix()
	if err := m.store.Save(playerID, p); err != nil {
		return nil, fmt.Errorf("profile.GetOrCreate save %q: %w", playerID, err)
	}
	return p, nil
}

// newDefaultProfile returns a freshly initialised profile with no match history.
func newDefaultProfile(playerID, commanderID string, buffIDs []string) *PlayerProfile {
	now := time.Now().Unix()
	if buffIDs == nil {
		buffIDs = []string{}
	}
	return &PlayerProfile{
		PlayerID:            playerID,
		Version:             CurrentVersion,
		CreatedAtUnix:       now,
		UpdatedAtUnix:       now,
		OwnedCommanderIDs:   []string{commanderID},
		SelectedCommanderID: commanderID,
		EquippedBuffIDs:     append([]string(nil), buffIDs...),
		UnlockedBuffIDs:     append([]string(nil), buffIDs...),
	}
}
