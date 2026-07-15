package profile

import "testing"

// TestMigrate_V8KnownRecipeIDsCarriesIntoV9 guards the one part of the
// recipes-into-items merge that touches SAVED PLAYER DATA. A v8 profile stores
// its crafting unlocks under "knownRecipeIds"; v9 stores the same values under
// "knownCraftableIds" (a recipe id was always the id of the item it produced).
// The migration must carry every learned recipe across verbatim — a bug here
// silently wipes what a player has unlocked, permanently.
func TestMigrate_V8KnownRecipeIDsCarriesIntoV9(t *testing.T) {
	p := &PlayerProfile{
		Version:              8,
		LegacyKnownRecipeIDs: []string{"fire_sword", "scimitar"},
	}
	migrateProfile(p)

	if got := p.KnownCraftableIDs; len(got) != 2 || got[0] != "fire_sword" || got[1] != "scimitar" {
		t.Fatalf("KnownCraftableIDs = %v, want the v8 values carried across verbatim", got)
	}
	// The legacy key is cleared so the next Save writes only the v9 schema.
	if p.LegacyKnownRecipeIDs != nil {
		t.Errorf("LegacyKnownRecipeIDs = %v, want nil after migration", p.LegacyKnownRecipeIDs)
	}
	if p.Version != CurrentVersion {
		t.Fatalf("version = %d, want %d", p.Version, CurrentVersion)
	}
}

// TestMigrate_MigrationIsIdempotent: running the migration twice must not lose
// or duplicate a player's unlocks.
func TestMigrate_MigrationIsIdempotent(t *testing.T) {
	p := &PlayerProfile{Version: 8, LegacyKnownRecipeIDs: []string{"fire_sword"}}
	migrateProfile(p)
	migrateProfile(p)
	if got := p.KnownCraftableIDs; len(got) != 1 || got[0] != "fire_sword" {
		t.Fatalf("KnownCraftableIDs = %v after a second migration, want [fire_sword]", got)
	}
}

// TestMigrate_InitsKnownCraftableIDs: a profile predating the ledger entirely
// gets an empty (non-nil) one.
func TestMigrate_InitsKnownCraftableIDs(t *testing.T) {
	p := &PlayerProfile{Version: 7} // v7: the field did not exist yet
	migrateProfile(p)
	if p.KnownCraftableIDs == nil {
		t.Fatal("migration should initialize KnownCraftableIDs to non-nil")
	}
	if p.Version != CurrentVersion {
		t.Fatalf("version = %d, want %d", p.Version, CurrentVersion)
	}
}

func TestRecordKnownRecipe_Idempotent(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	pid := "00000000-0000-0000-0000-000000000001"
	if err := m.RecordKnownRecipe(pid, "fire_sword"); err != nil {
		t.Fatal(err)
	}
	if err := m.RecordKnownRecipe(pid, "fire_sword"); err != nil {
		t.Fatal(err)
	}
	if err := m.RecordKnownRecipe(pid, "frost_sword"); err != nil {
		t.Fatal(err)
	}
	p, err := m.Get(pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.KnownCraftableIDs) != 2 {
		t.Fatalf("KnownCraftableIDs = %v, want 2 unique", p.KnownCraftableIDs)
	}
	// Sorted.
	if p.KnownCraftableIDs[0] != "fire_sword" || p.KnownCraftableIDs[1] != "frost_sword" {
		t.Fatalf("KnownCraftableIDs not sorted: %v", p.KnownCraftableIDs)
	}
}
