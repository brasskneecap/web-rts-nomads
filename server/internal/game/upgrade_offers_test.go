package game

import (
	"testing"
	"time"
)

func newTestStateForUpgrades(t *testing.T) *GameState {
	t.Helper()
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.WaveManager.Enabled = true
	s.WaveManager.CurrentWave = 1
	s.Players["p1"] = &Player{
		ID:           "p1",
		Resources:    map[string]int{},
		UpgradeState: newPlayerUpgradeState(1, 3),
	}
	return s
}

func TestGenerateUpgradeOffers_ReturnsThree(t *testing.T) {
	s := newTestStateForUpgrades(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	offers := s.generateUpgradeOffersLocked("p1")
	if len(offers) != 3 {
		t.Fatalf("expected 3 offers, got %d", len(offers))
	}
}

func TestGenerateUpgradeOffers_NoDuplicateIDs(t *testing.T) {
	s := newTestStateForUpgrades(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	offers := s.generateUpgradeOffersLocked("p1")
	seen := map[string]bool{}
	for _, o := range offers {
		if seen[o.ID] {
			t.Errorf("duplicate offer id: %s", o.ID)
		}
		seen[o.ID] = true
	}
}

func TestGenerateUpgradeOffers_FiltersMaxedGroup(t *testing.T) {
	s := newTestStateForUpgrades(t)
	// Max out every group except fortify so that fortify must appear.
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, def := range listUpgradeDefs() {
		if def.Group != "fortify" {
			s.Players["p1"].UpgradeState.UpgradeStacks[def.Group] = 99
		}
	}
	offers := s.generateUpgradeOffersLocked("p1")
	for _, o := range offers {
		if o.Group != "fortify" {
			t.Errorf("maxed group %q appeared in offers", o.Group)
		}
	}
}

func TestMilestoneWave_GuaranteesEpicOrBetter(t *testing.T) {
	s := newTestStateForUpgrades(t)
	s.WaveManager.CurrentWave = 5 // first milestone
	s.mu.Lock()
	defer s.mu.Unlock()
	// Need an epic+ upgrade in catalog for this test to be meaningful.
	// If none exist in the seed catalog, skip.
	hasEpicInCatalog := false
	for _, def := range listUpgradeDefs() {
		if upgradeRarityOrder[def.Rarity] >= upgradeRarityOrder[upgradeRarityEpic] {
			hasEpicInCatalog = true
			break
		}
	}
	if !hasEpicInCatalog {
		t.Skip("no epic+ upgrades in seed catalog — add one to test milestone guarantee")
	}
	// Run 20 times to rule out luck.
	for i := 0; i < 20; i++ {
		offers := s.generateUpgradeOffersLocked("p1")
		hasEpicOrBetter := false
		for _, o := range offers {
			if upgradeRarityOrder[o.Rarity] >= upgradeRarityOrder[upgradeRarityEpic] {
				hasEpicOrBetter = true
			}
		}
		if !hasEpicOrBetter {
			t.Errorf("milestone wave 5 offer set %d had no epic+ card: %v", i, offers)
		}
	}
}

func TestEnterWaveUpgradePhase_SetsDeadlineAndOffers(t *testing.T) {
	s := newTestStateForUpgrades(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	before := time.Now().UnixMilli()
	s.enterWaveUpgradePhaseLocked()
	after := time.Now().UnixMilli()
	p := s.Players["p1"]
	if p.UpgradeState.Resolved {
		t.Error("player should not be resolved after phase entry")
	}
	if p.UpgradeState.OfferDeadlineMs < before {
		t.Error("deadline should be in the future")
	}
	if p.UpgradeState.OfferDeadlineMs > after+30_000 {
		t.Error("deadline should be within 30 seconds")
	}
	if len(p.UpgradeState.CurrentOffers) != 3 {
		t.Errorf("expected 3 current offers, got %d", len(p.UpgradeState.CurrentOffers))
	}
	if p.UpgradeState.RerollsRemaining != 1 {
		t.Errorf("rerolls remaining: got %d, want 1", p.UpgradeState.RerollsRemaining)
	}
}
