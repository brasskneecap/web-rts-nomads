package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

func setupCraft(t *testing.T, gold int, ingredients []string) (*GameState, *Player) {
	t.Helper()
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 3)
	s.EnsurePlayerWithUpgrades("p1", nil, nil, nil, []string{"fire_sword"})
	s.mu.Lock()
	owner := "p1"
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
		ID: "art-1", BuildingType: "artificer", Visible: true, Occupied: true,
		OwnerID: &owner, Capabilities: []string{"crafting"}, Metadata: map[string]interface{}{},
	})
	if s.buildingsByID == nil {
		s.buildingsByID = map[string]*protocol.BuildingTile{}
	}
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		s.buildingsByID[b.ID] = b
	}
	p := s.Players["p1"]
	p.Resources["gold"] = gold
	for _, id := range ingredients {
		def, _ := getItemDef(id)
		s.addItemToVaultLocked(p, def)
	}
	s.mu.Unlock()
	return s, p
}

func TestCraftItem_Success(t *testing.T) {
	s, p := setupCraft(t, 1000, []string{"broad_sword", "fire_ring"})
	if !s.CraftItem("p1", "fire_sword") {
		t.Fatal("craft should succeed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if vaultItemCountLocked(p, "broad_sword") != 0 || vaultItemCountLocked(p, "fire_ring") != 0 {
		t.Fatal("inputs should be consumed")
	}
	if vaultItemCountLocked(p, "fire_sword") != 1 {
		t.Fatal("output should be added to vault")
	}
	fireSwordDef, ok := getRecipeDef("fire_sword")
	if !ok {
		t.Fatal("fire_sword recipe not found in catalog")
	}
	wantGold := 1000 - fireSwordDef.CostGold
	if p.Resources["gold"] != wantGold {
		t.Fatalf("gold = %d, want %d (%d charged)", p.Resources["gold"], wantGold, fireSwordDef.CostGold)
	}
}

func TestCraftItem_RejectsMissingIngredient(t *testing.T) {
	s, p := setupCraft(t, 1000, []string{"broad_sword"}) // no fire_ring
	if s.CraftItem("p1", "fire_sword") {
		t.Fatal("craft must fail without all ingredients")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if vaultItemCountLocked(p, "broad_sword") != 1 {
		t.Fatal("inputs must NOT be consumed on a failed craft")
	}
	if p.Resources["gold"] != 1000 {
		t.Fatal("gold must be unchanged on a failed craft")
	}
}

func TestCraftItem_RejectsUnknownRecipe(t *testing.T) {
	s, _ := setupCraft(t, 1000, []string{"broad_sword", "ice_ring"})
	// frost_sword is craftable by ingredients but NOT in the player's unlocked set.
	if s.CraftItem("p1", "frost_sword") {
		t.Fatal("craft must fail for a recipe the player hasn't unlocked")
	}
}

func TestCraftItem_RejectsNoArtificer(t *testing.T) {
	s, _ := setupCraft(t, 1000, []string{"broad_sword", "fire_ring"})
	s.mu.Lock()
	// Remove the artificer.
	s.MapConfig.Buildings = s.MapConfig.Buildings[:0]
	for k := range s.buildingsByID {
		delete(s.buildingsByID, k)
	}
	s.mu.Unlock()
	if s.CraftItem("p1", "fire_sword") {
		t.Fatal("craft must fail without a built Artificer")
	}
}
