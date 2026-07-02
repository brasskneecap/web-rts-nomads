package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

func addRecipeShop(s *GameState, bID string) {
	neutral := neutralPlayerID
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
		ID: bID, BuildingType: "recipe-shop", Width: 3, Height: 3,
		Visible: true, Occupied: true, OwnerID: &neutral,
		Capabilities: []string{"recipe-purchase"},
		Metadata:     map[string]interface{}{},
	})
}

func TestRecipeShop_PopulatesDeterministicSubset(t *testing.T) {
	roll := func(seed int64) []string {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), seed)
		s.EnsurePlayer("p1")
		s.mu.Lock()
		defer s.mu.Unlock()
		addRecipeShop(s, "rs-1")
		if s.buildingsByID == nil {
			s.buildingsByID = map[string]*protocol.BuildingTile{}
		}
		for i := range s.MapConfig.Buildings {
			b := &s.MapConfig.Buildings[i]
			s.buildingsByID[b.ID] = b
		}
		s.initShopBuildingsLocked()
		s.populateRecipeShopInventoriesLocked()
		b := s.buildingsByID["rs-1"]
		out := make([]string, 0, len(b.RecipeInventory))
		for _, e := range b.RecipeInventory {
			if e.Quantity != 1 {
				t.Errorf("recipe stock quantity = %d, want 1", e.Quantity)
			}
			out = append(out, e.RecipeID)
		}
		return out
	}
	a := roll(0xABC)
	b := roll(0xABC)
	if len(a) == 0 {
		t.Fatal("recipe shop stocked nothing")
	}
	if len(a) > 2 {
		t.Fatalf("recipe shop stocked %d > cap 2", len(a))
	}
	// Determinism: same seed → identical subset (order included).
	if len(a) != len(b) {
		t.Fatalf("non-deterministic count: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("non-deterministic subset: %v vs %v", a, b)
		}
	}
	// No duplicates.
	seen := map[string]bool{}
	for _, id := range a {
		if seen[id] {
			t.Fatalf("duplicate recipe in stock: %v", a)
		}
		seen[id] = true
	}
}

// TestRecipeShop_SamplesFromAssignedList verifies that a recipe-shop with a
// "recipeList" metadata only ever stocks recipes drawn from that list (never
// from the global pool). A subset list is registered for the test duration.
func TestRecipeShop_SamplesFromAssignedList(t *testing.T) {
	const listID = "test_two_swords"
	recipeListCatalogSingleton[listID] = &RecipeListDef{
		ID: listID, Name: "Two Swords", Recipes: []string{"fire_sword", "frost_sword"},
	}
	t.Cleanup(func() { delete(recipeListCatalogSingleton, listID) })

	allowed := map[string]bool{"fire_sword": true, "frost_sword": true}

	// Across many seeds, an assigned shop must only ever stock list recipes —
	// under global sampling, lightning_sword would appear and fail this.
	for seed := int64(0); seed < 25; seed++ {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), seed)
		s.EnsurePlayer("p1")
		s.mu.Lock()
		neutral := neutralPlayerID
		s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
			ID: "rs-list", BuildingType: "recipe-shop", Width: 3, Height: 3,
			Visible: true, Occupied: true, OwnerID: &neutral,
			Capabilities: []string{"recipe-purchase"},
			Metadata:     map[string]interface{}{"recipeList": listID},
		})
		if s.buildingsByID == nil {
			s.buildingsByID = map[string]*protocol.BuildingTile{}
		}
		for i := range s.MapConfig.Buildings {
			b := &s.MapConfig.Buildings[i]
			s.buildingsByID[b.ID] = b
		}
		s.initShopBuildingsLocked()
		s.populateRecipeShopInventoriesLocked()
		b := s.buildingsByID["rs-list"]
		if len(b.RecipeInventory) == 0 {
			s.mu.Unlock()
			t.Fatalf("seed %d: assigned shop stocked nothing", seed)
		}
		for _, e := range b.RecipeInventory {
			if !allowed[e.RecipeID] {
				s.mu.Unlock()
				t.Fatalf("seed %d: shop stocked %q, not in the assigned list", seed, e.RecipeID)
			}
		}
		s.mu.Unlock()
	}
}

// TestRecipeShop_UnknownListFallsBackToAll verifies an invalid/unknown recipeList
// does not break population — the shop falls back to the global recipe pool.
func TestRecipeShop_UnknownListFallsBackToAll(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 7)
	s.EnsurePlayer("p1")
	s.mu.Lock()
	defer s.mu.Unlock()
	neutral := neutralPlayerID
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
		ID: "rs-bad", BuildingType: "recipe-shop", Width: 3, Height: 3,
		Visible: true, Occupied: true, OwnerID: &neutral,
		Capabilities: []string{"recipe-purchase"},
		Metadata:     map[string]interface{}{"recipeList": "no_such_list"},
	})
	if s.buildingsByID == nil {
		s.buildingsByID = map[string]*protocol.BuildingTile{}
	}
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		s.buildingsByID[b.ID] = b
	}
	s.initShopBuildingsLocked()
	s.populateRecipeShopInventoriesLocked()
	if len(s.buildingsByID["rs-bad"].RecipeInventory) == 0 {
		t.Fatal("unknown recipeList should fall back to all recipes; stocked nothing")
	}
}

// firstNeutralGroupID returns any neutral group id present in the catalog, or ""
// (skip signal) when none exist.
func firstNeutralGroupID() string {
	for tier := 1; tier <= 10; tier++ {
		if ids := listNeutralGroupIDs(tier); len(ids) > 0 {
			return ids[0]
		}
	}
	return ""
}

// TestShopGuards_SpawnAtChosenCell verifies that a shop with guardSpawnX/Y
// metadata rings its guards around (and anchors them to) the chosen cell rather
// than the building footprint center.
func TestShopGuards_SpawnAtChosenCell(t *testing.T) {
	groupID := firstNeutralGroupID()
	if groupID == "" {
		t.Skip("no neutral group available")
	}

	s, _ := newBuildingAttackState(t) // obstacle-free 40x24, s locked
	defer s.mu.Unlock()

	const spawnCX, spawnCY = 12, 18
	neutral := neutralPlayerID
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
		ID: "rs-spawn", BuildingType: "recipe-shop", GridCoord: protocol.GridCoord{X: 20, Y: 5}, Width: 3, Height: 3,
		Visible: true, Occupied: true, OwnerID: &neutral,
		Capabilities: []string{"recipe-purchase"},
		Metadata: map[string]interface{}{
			"guardGroupId": groupID,
			"guardSpawnX":  float64(spawnCX),
			"guardSpawnY":  float64(spawnCY),
		},
	})
	if s.buildingsByID == nil {
		s.buildingsByID = map[string]*protocol.BuildingTile{}
	}
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		s.buildingsByID[b.ID] = b
	}
	s.initShopBuildingsLocked()
	s.spawnShopGuardsLocked()

	b := s.buildingsByID["rs-spawn"]
	if len(b.ShopGuardUnitIDs) == 0 {
		t.Fatal("expected guards to spawn")
	}
	want := s.gridToWorldCenter(gridPoint{X: spawnCX, Y: spawnCY})
	// Shop footprint center — the anchor must NOT be here.
	shopCX := (20 + 1.5) * s.MapConfig.CellSize
	for _, id := range b.ShopGuardUnitIDs {
		u := s.getUnitByIDLocked(id)
		if u == nil {
			t.Fatalf("guard %d missing", id)
		}
		if u.GuardAnchorX != want.X || u.GuardAnchorY != want.Y {
			t.Errorf("guard anchor = (%.1f,%.1f), want chosen cell (%.1f,%.1f)", u.GuardAnchorX, u.GuardAnchorY, want.X, want.Y)
		}
		if u.GuardAnchorX == shopCX {
			t.Errorf("guard anchored at the shop center; guardSpawn metadata was ignored")
		}
	}
}

// TestShopGuards_DefaultsToShopCenter verifies that with no guardSpawn metadata,
// guards anchor to the building footprint center (unchanged behavior).
func TestShopGuards_DefaultsToShopCenter(t *testing.T) {
	groupID := firstNeutralGroupID()
	if groupID == "" {
		t.Skip("no neutral group available")
	}

	s, _ := newBuildingAttackState(t)
	defer s.mu.Unlock()

	neutral := neutralPlayerID
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
		ID: "rs-default", BuildingType: "recipe-shop", GridCoord: protocol.GridCoord{X: 20, Y: 5}, Width: 3, Height: 3,
		Visible: true, Occupied: true, OwnerID: &neutral,
		Capabilities: []string{"recipe-purchase"},
		Metadata:     map[string]interface{}{"guardGroupId": groupID},
	})
	if s.buildingsByID == nil {
		s.buildingsByID = map[string]*protocol.BuildingTile{}
	}
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		s.buildingsByID[b.ID] = b
	}
	s.initShopBuildingsLocked()
	s.spawnShopGuardsLocked()

	b := s.buildingsByID["rs-default"]
	if len(b.ShopGuardUnitIDs) == 0 {
		t.Fatal("expected guards to spawn")
	}
	wantX := (20 + 1.5) * s.MapConfig.CellSize
	wantY := (5 + 1.5) * s.MapConfig.CellSize
	for _, id := range b.ShopGuardUnitIDs {
		u := s.getUnitByIDLocked(id)
		if u.GuardAnchorX != wantX || u.GuardAnchorY != wantY {
			t.Errorf("guard anchor = (%.1f,%.1f), want shop center (%.1f,%.1f)", u.GuardAnchorX, u.GuardAnchorY, wantX, wantY)
		}
	}
}

// TestRecipeShopGuards_OptInSpawnAndLock verifies that a recipe-shop with a
// guardGroupId spawns a guard squad and reports locked while they live, and
// that a recipe-shop WITHOUT guard metadata spawns no guards and is never
// locked (guards are opt-in per placed building).
func TestRecipeShopGuards_OptInSpawnAndLock(t *testing.T) {
	// Find any neutral group id present in the catalog.
	var groupID string
	for tier := 1; tier <= 10 && groupID == ""; tier++ {
		if ids := listNeutralGroupIDs(tier); len(ids) > 0 {
			groupID = ids[0]
			break
		}
	}
	if groupID == "" {
		t.Skip("no neutral group available")
	}

	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 5)
	s.EnsurePlayer("p1")
	s.mu.Lock()
	defer s.mu.Unlock()

	neutral := neutralPlayerID
	// Guarded recipe shop.
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
		ID: "rs-guarded", BuildingType: "recipe-shop", Width: 3, Height: 3,
		Visible: true, Occupied: true, OwnerID: &neutral,
		Capabilities: []string{"recipe-purchase"},
		Metadata:     map[string]interface{}{"guardGroupId": groupID},
	})
	// Unguarded recipe shop (no guardGroupId).
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
		ID: "rs-open", BuildingType: "recipe-shop", Width: 3, Height: 3,
		Visible: true, Occupied: true, OwnerID: &neutral,
		Capabilities: []string{"recipe-purchase"},
		Metadata:     map[string]interface{}{},
	})
	if s.buildingsByID == nil {
		s.buildingsByID = map[string]*protocol.BuildingTile{}
	}
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		s.buildingsByID[b.ID] = b
	}
	s.initShopBuildingsLocked()
	s.spawnShopGuardsLocked()

	guarded := s.buildingsByID["rs-guarded"]
	if len(guarded.ShopGuardUnitIDs) == 0 {
		t.Fatal("guarded recipe shop should spawn at least one guard")
	}
	if !s.shopLockedLocked(guarded) {
		t.Fatal("guarded recipe shop should be locked while guards are alive")
	}

	open := s.buildingsByID["rs-open"]
	if len(open.ShopGuardUnitIDs) != 0 {
		t.Fatalf("recipe shop without guardGroupId should spawn no guards, got %d", len(open.ShopGuardUnitIDs))
	}
	if s.shopLockedLocked(open) {
		t.Fatal("recipe shop without guards should not be locked")
	}

	// Kill the guards → the guarded shop unlocks.
	for _, id := range guarded.ShopGuardUnitIDs {
		if u := s.getUnitByIDLocked(id); u != nil {
			u.HP = 0
		}
	}
	if s.shopLockedLocked(guarded) {
		t.Error("recipe shop should unlock after all guards reach HP 0")
	}
}
