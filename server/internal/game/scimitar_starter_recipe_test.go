package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestScimitarStarterRecipe verifies the scimitar recipe is defined as
// 3× broad_sword → scimitar and is a starter recipe unlocked by default.
func TestScimitarStarterRecipe(t *testing.T) {
	def, ok := getRecipeDef("scimitar")
	if !ok {
		t.Fatal("scimitar recipe not found")
	}
	if def.Output != "scimitar" {
		t.Errorf("output = %q, want scimitar", def.Output)
	}
	if len(def.Inputs) != 3 {
		t.Fatalf("inputs = %v, want 3 broad_sword", def.Inputs)
	}
	for i, in := range def.Inputs {
		if in != "broad_sword" {
			t.Errorf("input[%d] = %q, want broad_sword", i, in)
		}
	}
	if !def.Starter {
		t.Error("scimitar should be a starter recipe")
	}
}

// TestScimitar_UnlockedByDefault verifies a freshly joined player has the
// starter scimitar recipe unlocked without any Recipe Shop purchase.
func TestScimitar_UnlockedByDefault(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	s.EnsurePlayer("p1") // no known recipes, no purchases
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.playerKnowsRecipeLocked("p1", "scimitar") {
		t.Fatal("scimitar should be unlocked by default (starter recipe)")
	}
}

// TestCraftScimitar_ConsumesThreeBroadSwords verifies crafting the scimitar at
// an Artificer consumes exactly three broad swords and yields one scimitar.
func TestCraftScimitar_ConsumesThreeBroadSwords(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 3)
	s.EnsurePlayer("p1")
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
	p.Resources["gold"] = 1000
	broad, _ := getItemDef("broad_sword")
	s.addItemToVaultLocked(p, broad)
	s.addItemToVaultLocked(p, broad)
	s.addItemToVaultLocked(p, broad)
	s.mu.Unlock()

	if !s.CraftItem("p1", "scimitar") {
		t.Fatal("crafting scimitar should succeed")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if got := vaultItemCountLocked(p, "broad_sword"); got != 0 {
		t.Errorf("broad_sword left = %d, want 0 (all 3 consumed)", got)
	}
	if got := vaultItemCountLocked(p, "scimitar"); got != 1 {
		t.Errorf("scimitar count = %d, want 1", got)
	}
	if got := p.Resources["gold"]; got != 1000-25 {
		t.Errorf("gold = %d, want %d (25 charged)", got, 1000-25)
	}
}
