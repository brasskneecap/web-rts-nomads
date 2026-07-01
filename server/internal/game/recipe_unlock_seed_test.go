package game

import "testing"

func TestEnsurePlayer_SeedsAndUnlocksRecipes(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	s.EnsurePlayerWithUpgrades("p1", nil, nil, nil, []string{"fire_sword"})
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.playerKnowsRecipeLocked("p1", "fire_sword") {
		t.Fatal("fire_sword should be seeded from knownRecipeIDs")
	}
	if s.playerKnowsRecipeLocked("p1", "frost_sword") {
		t.Fatal("frost_sword was not seeded; should be unknown")
	}
	// Unlock is idempotent and additive.
	p := s.Players["p1"]
	s.unlockRecipeForPlayerLocked(p, "frost_sword")
	s.unlockRecipeForPlayerLocked(p, "frost_sword")
	count := 0
	for _, id := range p.UnlockedRecipeIDs {
		if id == "frost_sword" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("frost_sword appears %d times, want 1 (idempotent)", count)
	}
}

func TestEnsurePlayer_BackwardCompatShim(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	s.EnsurePlayer("p1") // must still compile/work with no recipe arg
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Players["p1"] == nil {
		t.Fatal("EnsurePlayer did not create player")
	}
}
