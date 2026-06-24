package game

import "testing"

// TestPlayerConfig_StartingResourcesCopyIsolated verifies newStartingResources
// returns a fresh map each call so per-player mutation never aliases the
// shared singleton.
func TestPlayerConfig_StartingResourcesCopyIsolated(t *testing.T) {
	a := playerConfig().newStartingResources()
	b := playerConfig().newStartingResources()

	if len(a) == 0 {
		t.Fatal("startingResources copy is empty; expected configured resources")
	}
	a["gold"] += 1000
	if b["gold"] == a["gold"] {
		t.Errorf("mutating one copy affected another: a=%v b=%v", a, b)
	}
	// The singleton itself must be untouched.
	if playerConfig().StartingResources["gold"] == a["gold"] {
		t.Errorf("mutating a copy leaked into the singleton: %v", playerConfig().StartingResources)
	}
}

// TestPlayer_DefaultStartingResources verifies a freshly enrolled player begins
// with exactly the configured starting resources (no upgrades).
func TestPlayer_DefaultStartingResources(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	s.EnsurePlayerWithUpgrades("p1", nil, nil, nil)

	s.mu.RLock()
	p, ok := s.Players["p1"]
	s.mu.RUnlock()
	if !ok {
		t.Fatal("player p1 not found")
	}

	for resource, want := range playerConfig().StartingResources {
		if got := p.Resources[resource]; got != want {
			t.Errorf("starting %s: want %d, got %d", resource, want, got)
		}
	}
}

// TestProfileUpgrade_StartingGold_AddsResource verifies the startingResource
// effect (starting_gold) layers on top of the configured baseline gold.
func TestProfileUpgrade_StartingGold_AddsResource(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	const rank = 3
	s.EnsurePlayerWithUpgrades("p1", map[string]int{"starting_gold": rank}, nil, nil)

	s.mu.RLock()
	p, ok := s.Players["p1"]
	s.mu.RUnlock()
	if !ok {
		t.Fatal("player p1 not found")
	}

	def, found := getProfileUpgradeDef("starting_gold")
	if !found {
		t.Fatal("starting_gold profile upgrade not found in catalog")
	}
	wantGold := playerConfig().StartingResources["gold"] + rank*def.Effect.AmountPerRank
	if got := p.Resources["gold"]; got != wantGold {
		t.Errorf("gold with starting_gold rank %d: want %d, got %d", rank, wantGold, got)
	}
	// Wood must be untouched by a gold-only upgrade.
	if got, want := p.Resources["wood"], playerConfig().StartingResources["wood"]; got != want {
		t.Errorf("wood should be unchanged: want %d, got %d", want, got)
	}
}

// TestProfileUpgrade_StartingGold_InactiveNotApplied verifies an owned-but-
// inactive starting_gold upgrade does not grant bonus gold.
func TestProfileUpgrade_StartingGold_InactiveNotApplied(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	s.EnsurePlayerWithUpgrades("p1", map[string]int{"starting_gold": 3}, []string{}, nil)

	s.mu.RLock()
	p, ok := s.Players["p1"]
	s.mu.RUnlock()
	if !ok {
		t.Fatal("player p1 not found")
	}

	if got, want := p.Resources["gold"], playerConfig().StartingResources["gold"]; got != want {
		t.Errorf("inactive starting_gold must not apply: want %d, got %d", want, got)
	}
}
