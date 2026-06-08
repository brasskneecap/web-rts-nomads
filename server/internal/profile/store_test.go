package profile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileStore_SaveAndLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)

	playerID := "12345678-1234-1234-1234-123456789abc"
	p := &PlayerProfile{
		PlayerID:             playerID,
		Version:              CurrentVersion,
		CreatedAtUnix:        time.Now().Unix(),
		UpdatedAtUnix:        time.Now().Unix(),
		LegendPoints:         42,
		LifetimeLegendPoints: 100,
		OwnedUpgradeRanks:    map[string]int{},
		ActiveUpgradeIDs:     []string{},
	}

	if err := store.Save(playerID, p); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.Load(playerID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil for existing profile")
	}
	if loaded.LegendPoints != 42 {
		t.Errorf("LegendPoints: want 42, got %d", loaded.LegendPoints)
	}
}

func TestFileStore_Load_NotExist_ReturnsNil(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)

	loaded, err := store.Load("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("unexpected error for missing profile: %v", err)
	}
	if loaded != nil {
		t.Errorf("expected nil for missing profile, got %+v", loaded)
	}
}

func TestFileStore_InvalidPlayerID_Rejected(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)

	// Path traversal attempt.
	if _, err := store.Load("../../etc/passwd-00000000-0000"); err == nil {
		t.Error("expected error for path-traversal player ID, got nil")
	}
	// Too short.
	if _, err := store.Load("short"); err == nil {
		t.Error("expected error for short player ID, got nil")
	}
}

func TestFileStore_BackupFallback(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)

	playerID := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	p := &PlayerProfile{
		PlayerID:         playerID,
		Version:          CurrentVersion,
		LegendPoints:     7,
		CreatedAtUnix:    time.Now().Unix(),
		OwnedUpgradeRanks: map[string]int{},
		ActiveUpgradeIDs: []string{},
	}

	// Save twice so the second save creates a .bak of the first.
	if err := store.Save(playerID, p); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	if err := store.Save(playerID, p); err != nil {
		t.Fatalf("second Save: %v", err)
	}

	// Corrupt the primary file.
	primary := filepath.Join(dir, playerID+".json")
	if err := os.WriteFile(primary, []byte("NOT JSON"), 0o644); err != nil {
		t.Fatalf("corrupt primary: %v", err)
	}

	// Load should fall back to backup.
	loaded, err := store.Load(playerID)
	if err != nil {
		t.Fatalf("Load with corrupt primary: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected fallback to backup, got nil")
	}
	if loaded.LegendPoints != 7 {
		t.Errorf("backup LegendPoints: want 7, got %d", loaded.LegendPoints)
	}
}

func TestFileStore_BothCorrupt_ReturnsProfileCorruptError(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)

	playerID := "cccccccc-cccc-cccc-cccc-cccccccccccc"
	p := &PlayerProfile{
		PlayerID:         playerID,
		Version:          CurrentVersion,
		CreatedAtUnix:    time.Now().Unix(),
		OwnedUpgradeRanks: map[string]int{},
		ActiveUpgradeIDs: []string{},
	}

	// Save to get a backup created on next save.
	if err := store.Save(playerID, p); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	// Save again so there is a .bak file.
	if err := store.Save(playerID, p); err != nil {
		t.Fatalf("second Save: %v", err)
	}

	// Corrupt both.
	for _, suffix := range []string{".json", ".json.bak"} {
		path := filepath.Join(dir, playerID+suffix)
		if err := os.WriteFile(path, []byte("BROKEN"), 0o644); err != nil {
			t.Fatalf("corrupt %s: %v", suffix, err)
		}
	}

	_, err := store.Load(playerID)
	if err == nil {
		t.Fatal("expected ProfileCorruptError, got nil")
	}
	var pce *ProfileCorruptError
	if !isProfileCorrupt(err, &pce) {
		t.Errorf("expected *ProfileCorruptError, got %T: %v", err, err)
	}
}

func isProfileCorrupt(err error, out **ProfileCorruptError) bool {
	if pce, ok := err.(*ProfileCorruptError); ok {
		*out = pce
		return true
	}
	return false
}

func TestManager_GetOrCreate_DefaultsCorrect(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	playerID := "dddddddd-dddd-dddd-dddd-dddddddddddd"
	p, err := m.GetOrCreate(playerID, "default_commander")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if p == nil {
		t.Fatal("GetOrCreate returned nil")
	}
	if p.SelectedCommanderID != "default_commander" {
		t.Errorf("SelectedCommanderID: want default_commander, got %q", p.SelectedCommanderID)
	}
	if p.Version != CurrentVersion {
		t.Errorf("Version: want %d, got %d", CurrentVersion, p.Version)
	}
	if p.ActiveUpgradeIDs == nil {
		t.Error("ActiveUpgradeIDs should be non-nil on a new profile")
	}
}

func TestManager_GetOrCreate_IdempotentOnSecondCall(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	playerID := "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"
	p1, err := m.GetOrCreate(playerID, "cmd1")
	if err != nil {
		t.Fatalf("first GetOrCreate: %v", err)
	}
	p2, err := m.GetOrCreate(playerID, "cmd2")
	if err != nil {
		t.Fatalf("second GetOrCreate: %v", err)
	}
	if p1.SelectedCommanderID != p2.SelectedCommanderID {
		t.Errorf("second call should return existing profile, but commanders differ: %q vs %q",
			p1.SelectedCommanderID, p2.SelectedCommanderID)
	}
}

func TestManager_WithLocked_MutatesAndPersists(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	playerID := "ffffffff-ffff-ffff-ffff-ffffffffffff"
	_, err := m.GetOrCreate(playerID, "cmd")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}

	err = m.WithLocked(playerID, func(p *PlayerProfile) error {
		p.LegendPoints = 99
		return nil
	})
	if err != nil {
		t.Fatalf("WithLocked: %v", err)
	}

	loaded, err := m.Get(playerID)
	if err != nil {
		t.Fatalf("Get after WithLocked: %v", err)
	}
	if loaded.LegendPoints != 99 {
		t.Errorf("LegendPoints after WithLocked: want 99, got %d", loaded.LegendPoints)
	}
}

// TestMigrateProfile_V1ToV2_OwnedUpgradeRanksInitialized verifies that loading
// a v1 profile (which lacks ownedUpgradeRanks) produces a non-nil empty map
// and that the next save persists the v2 schema version.
func TestMigrateProfile_V1ToV2_OwnedUpgradeRanksInitialized(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)
	m := NewManager(dir)

	playerID := "11111111-1111-1111-1111-111111111111"

	// Write a hand-rolled v1 JSON fixture that lacks ownedUpgradeRanks.
	v1JSON := `{
		"playerId": "11111111-1111-1111-1111-111111111111",
		"version": 1,
		"createdAtUnix": 0,
		"updatedAtUnix": 0,
		"legendPoints": 42,
		"lifetimeLegendPoints": 50,
		"ownedCommanderIds": [],
		"selectedCommanderId": "",
		"equippedBuffIds": [],
		"unlockedBuffIds": [],
		"stats": {}
	}`
	primaryPath := filepath.Join(dir, playerID+".json")
	if err := os.WriteFile(primaryPath, []byte(v1JSON), 0o644); err != nil {
		t.Fatalf("write v1 fixture: %v", err)
	}

	// Load should apply migration.
	loaded, err := store.Load(playerID)
	if err != nil {
		t.Fatalf("Load v1 profile: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil for existing v1 profile")
	}
	if loaded.OwnedUpgradeRanks == nil {
		t.Error("OwnedUpgradeRanks should be non-nil after v1→v2 migration")
	}
	if len(loaded.OwnedUpgradeRanks) != 0 {
		t.Errorf("OwnedUpgradeRanks should be empty after migration, got %v", loaded.OwnedUpgradeRanks)
	}
	if loaded.Version != CurrentVersion {
		t.Errorf("Version after migration: want %d, got %d", CurrentVersion, loaded.Version)
	}
	if loaded.LegendPoints != 42 {
		t.Errorf("LegendPoints should be preserved: want 42, got %d", loaded.LegendPoints)
	}

	// Trigger a WithLocked mutation to force a save.
	err = m.WithLocked(playerID, func(p *PlayerProfile) error {
		p.LegendPoints = 55
		return nil
	})
	if err != nil {
		t.Fatalf("WithLocked after migration: %v", err)
	}

	// Re-read the raw file and verify the saved version is CurrentVersion.
	data, err := os.ReadFile(primaryPath)
	if err != nil {
		t.Fatalf("re-read saved file: %v", err)
	}
	var saved PlayerProfile
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("unmarshal saved file: %v", err)
	}
	if saved.Version != CurrentVersion {
		t.Errorf("saved version: want %d, got %d", CurrentVersion, saved.Version)
	}
	if saved.OwnedUpgradeRanks == nil {
		t.Error("saved OwnedUpgradeRanks should be non-nil")
	}
}

// TestGetOrCreate_NewProfile_OwnedUpgradeRanksNonNil verifies that a brand-new
// profile has a non-nil empty OwnedUpgradeRanks and the current schema version.
func TestGetOrCreate_NewProfile_OwnedUpgradeRanksNonNil(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	playerID := "22222222-2222-2222-2222-222222222222"
	p, err := m.GetOrCreate(playerID, "default_commander")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if p.OwnedUpgradeRanks == nil {
		t.Error("new profile OwnedUpgradeRanks should be non-nil")
	}
	if len(p.OwnedUpgradeRanks) != 0 {
		t.Errorf("new profile OwnedUpgradeRanks should be empty, got %v", p.OwnedUpgradeRanks)
	}
	if p.Version != CurrentVersion {
		t.Errorf("new profile version: want %d, got %d", CurrentVersion, p.Version)
	}
	if p.ActiveUpgradeIDs == nil {
		t.Error("new profile ActiveUpgradeIDs should be non-nil")
	}
}

// TestMigrateProfile_V5ToV6_CompletedCampaignObjectivesInitialized verifies
// that loading a v5 profile (which lacks completedCampaignObjectives) produces
// a non-nil empty map and that the next save persists the v6 schema version.
// Other v5 fields (notably CompletedCampaignLevels) are preserved unchanged.
func TestMigrateProfile_V5ToV6_CompletedCampaignObjectivesInitialized(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)
	m := NewManager(dir)

	playerID := "55555555-5555-5555-5555-555555555555"

	// Hand-rolled v5 JSON fixture: has every v5 field (including
	// completedCampaignLevels) but is missing completedCampaignObjectives.
	v5JSON := `{
		"playerId": "55555555-5555-5555-5555-555555555555",
		"version": 5,
		"createdAtUnix": 0,
		"updatedAtUnix": 0,
		"legendPoints": 7,
		"lifetimeLegendPoints": 12,
		"ownedCommanderIds": ["nomad_commander_default"],
		"selectedCommanderId": "nomad_commander_default",
		"stats": {},
		"ownedUpgradeRanks": {},
		"activeUpgradeIds": [],
		"acquiredAdvancements": [],
		"completedCampaignLevels": ["forest_01"]
	}`
	primaryPath := filepath.Join(dir, playerID+".json")
	if err := os.WriteFile(primaryPath, []byte(v5JSON), 0o644); err != nil {
		t.Fatalf("write v5 fixture: %v", err)
	}

	loaded, err := store.Load(playerID)
	if err != nil {
		t.Fatalf("Load v5 profile: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil for existing v5 profile")
	}
	if loaded.CompletedCampaignObjectives == nil {
		t.Error("CompletedCampaignObjectives should be non-nil after v5→v6 migration")
	}
	if len(loaded.CompletedCampaignObjectives) != 0 {
		t.Errorf("CompletedCampaignObjectives should be empty after migration, got %v", loaded.CompletedCampaignObjectives)
	}
	if loaded.Version != CurrentVersion {
		t.Errorf("Version after migration: want %d, got %d", CurrentVersion, loaded.Version)
	}
	// v5 data must survive the migration unchanged.
	if loaded.LegendPoints != 7 {
		t.Errorf("LegendPoints: want 7, got %d", loaded.LegendPoints)
	}
	if len(loaded.CompletedCampaignLevels) != 1 || loaded.CompletedCampaignLevels[0] != "forest_01" {
		t.Errorf("CompletedCampaignLevels lost across migration, got %v", loaded.CompletedCampaignLevels)
	}

	// Trigger a WithLocked mutation to force a save and verify the on-disk
	// JSON now carries version=6 AND a non-nil empty objectives map.
	err = m.WithLocked(playerID, func(p *PlayerProfile) error {
		p.LegendPoints = 9
		return nil
	})
	if err != nil {
		t.Fatalf("WithLocked after migration: %v", err)
	}

	data, err := os.ReadFile(primaryPath)
	if err != nil {
		t.Fatalf("re-read saved file: %v", err)
	}
	var saved PlayerProfile
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("unmarshal saved file: %v", err)
	}
	if saved.Version != CurrentVersion {
		t.Errorf("saved version: want %d, got %d", CurrentVersion, saved.Version)
	}
	if saved.CompletedCampaignObjectives == nil {
		t.Error("saved CompletedCampaignObjectives should be non-nil (so JSON is {} not null)")
	}
}

// TestGetOrCreate_NewProfile_CompletedCampaignObjectivesNonNil verifies that
// a brand-new profile has a non-nil empty CompletedCampaignObjectives map.
func TestGetOrCreate_NewProfile_CompletedCampaignObjectivesNonNil(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	playerID := "66666666-6666-6666-6666-666666666666"
	p, err := m.GetOrCreate(playerID, "default_commander")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if p.CompletedCampaignObjectives == nil {
		t.Error("new profile CompletedCampaignObjectives should be non-nil")
	}
	if len(p.CompletedCampaignObjectives) != 0 {
		t.Errorf("new profile CompletedCampaignObjectives should be empty, got %v", p.CompletedCampaignObjectives)
	}
}

// TestMigrateProfile_V2ToV3_ActiveUpgradeIDsPopulated verifies that a v2
// profile with OwnedUpgradeRanks populated gets ActiveUpgradeIDs defaulted to
// the sorted list of all owned upgrades with rank > 0.
func TestMigrateProfile_V2ToV3_ActiveUpgradeIDsPopulated(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)

	playerID := "33333333-3333-3333-3333-333333333333"

	// Write a hand-rolled v2 JSON fixture that lacks activeUpgradeIds.
	v2JSON := `{
		"playerId": "33333333-3333-3333-3333-333333333333",
		"version": 2,
		"createdAtUnix": 0,
		"updatedAtUnix": 0,
		"legendPoints": 0,
		"lifetimeLegendPoints": 0,
		"ownedCommanderIds": [],
		"selectedCommanderId": "",
		"stats": {},
		"ownedUpgradeRanks": {"additional_worker": 2}
	}`
	primaryPath := filepath.Join(dir, playerID+".json")
	if err := os.WriteFile(primaryPath, []byte(v2JSON), 0o644); err != nil {
		t.Fatalf("write v2 fixture: %v", err)
	}

	loaded, err := store.Load(playerID)
	if err != nil {
		t.Fatalf("Load v2 profile: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil for existing v2 profile")
	}
	if loaded.ActiveUpgradeIDs == nil {
		t.Fatal("ActiveUpgradeIDs should be non-nil after v2->v3 migration")
	}
	if len(loaded.ActiveUpgradeIDs) != 1 || loaded.ActiveUpgradeIDs[0] != "additional_worker" {
		t.Errorf("ActiveUpgradeIDs: want [additional_worker], got %v", loaded.ActiveUpgradeIDs)
	}
	if loaded.Version != CurrentVersion {
		t.Errorf("Version after migration: want %d, got %d", CurrentVersion, loaded.Version)
	}
}
