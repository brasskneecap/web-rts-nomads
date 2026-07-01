package profile

import "testing"

func TestMigrate_InitsKnownRecipeIDs(t *testing.T) {
	p := &PlayerProfile{Version: 7} // a v7 profile with the field absent
	migrateProfile(p)
	if p.KnownRecipeIDs == nil {
		t.Fatal("migration should initialize KnownRecipeIDs to non-nil")
	}
	if p.Version != CurrentVersion {
		t.Fatalf("version = %d, want %d", p.Version, CurrentVersion)
	}
	if CurrentVersion != 8 {
		t.Fatalf("CurrentVersion = %d, want 8", CurrentVersion)
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
	if len(p.KnownRecipeIDs) != 2 {
		t.Fatalf("KnownRecipeIDs = %v, want 2 unique", p.KnownRecipeIDs)
	}
	// Sorted.
	if p.KnownRecipeIDs[0] != "fire_sword" || p.KnownRecipeIDs[1] != "frost_sword" {
		t.Fatalf("KnownRecipeIDs not sorted: %v", p.KnownRecipeIDs)
	}
}
