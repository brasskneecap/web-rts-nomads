package profile

import (
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

// Manager provides per-player serialized access to profiles. Concurrent calls
// for the same playerID are serialized; calls for different players run in
// parallel.
type Manager struct {
	store Store
	muMap sync.Map // playerID string -> *sync.Mutex
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

// CommitDominionPoints atomically adds earned to both DominionPoints and
// LifetimeDominionPoints for the named player. Called once per match-end by the
// match manager; a no-op when earned <= 0. Satisfies game.DominionPointCommitter.
func (m *Manager) CommitDominionPoints(playerID string, earned int) error {
	if earned <= 0 {
		return nil
	}
	return m.WithLocked(playerID, func(p *PlayerProfile) error {
		p.DominionPoints += earned
		p.LifetimeDominionPoints += earned
		return nil
	})
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
		p = newDefaultProfile(playerID, DefaultCommanderID)
	}
	if err := fn(p); err != nil {
		return err
	}
	p.UpdatedAtUnix = time.Now().Unix()
	return m.store.Save(playerID, p)
}

// GetOrCreate returns the existing profile for playerID, or creates and
// persists a fresh default profile if none exists. defaultCommanderID is set
// as the owned and selected commander.
func (m *Manager) GetOrCreate(playerID string, defaultCommanderID string) (*PlayerProfile, error) {
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

	p = newDefaultProfile(playerID, defaultCommanderID)
	p.UpdatedAtUnix = time.Now().Unix()
	if err := m.store.Save(playerID, p); err != nil {
		return nil, fmt.Errorf("profile.GetOrCreate save %q: %w", playerID, err)
	}
	return p, nil
}

// newDefaultProfile returns a freshly initialised profile with no match history.
func newDefaultProfile(playerID, commanderID string) *PlayerProfile {
	now := time.Now().Unix()
	return &PlayerProfile{
		PlayerID:                    playerID,
		Version:                     CurrentVersion,
		CreatedAtUnix:               now,
		UpdatedAtUnix:               now,
		OwnedCommanderIDs:           []string{commanderID},
		SelectedCommanderID:         commanderID,
		OwnedUpgradeRanks:           map[string]int{},
		ActiveUpgradeIDs:            []string{},
		AcquiredAdvancements:        []AcquiredAdvancement{},
		CompletedCampaignLevels:     []string{},
		CompletedCampaignObjectives: map[string][]string{},
		KnownRecipeIDs:              []string{},
	}
}

// RecordKnownRecipe records recipeID into the player's permanent KnownRecipeIDs
// set (idempotent, sorted). Called fire-and-forget after a successful craft so
// the recipe is craftable in all future matches. No-op on empty recipeID.
func (m *Manager) RecordKnownRecipe(playerID, recipeID string) error {
	if recipeID == "" {
		return nil
	}
	return m.WithLocked(playerID, func(p *PlayerProfile) error {
		for _, id := range p.KnownRecipeIDs {
			if id == recipeID {
				return nil // already known
			}
		}
		p.KnownRecipeIDs = append(p.KnownRecipeIDs, recipeID)
		sort.Strings(p.KnownRecipeIDs)
		return nil
	})
}
