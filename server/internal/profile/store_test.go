package profile

import (
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
		PlayerID:            playerID,
		Version:             CurrentVersion,
		CreatedAtUnix:       time.Now().Unix(),
		UpdatedAtUnix:       time.Now().Unix(),
		LegendPoints:        42,
		LifetimeLegendPoints: 100,
		UnlockedBuffIDs:     []string{"iron_discipline"},
		EquippedBuffIDs:     []string{},
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
	if len(loaded.UnlockedBuffIDs) != 1 || loaded.UnlockedBuffIDs[0] != "iron_discipline" {
		t.Errorf("UnlockedBuffIDs: want [iron_discipline], got %v", loaded.UnlockedBuffIDs)
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
		PlayerID:        playerID,
		Version:         CurrentVersion,
		LegendPoints:    7,
		CreatedAtUnix:   time.Now().Unix(),
		EquippedBuffIDs: []string{},
		UnlockedBuffIDs: []string{},
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
		PlayerID:        playerID,
		Version:         CurrentVersion,
		CreatedAtUnix:   time.Now().Unix(),
		EquippedBuffIDs: []string{},
		UnlockedBuffIDs: []string{},
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
	p, err := m.GetOrCreate(playerID, "default_commander", []string{"iron_discipline"})
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if p == nil {
		t.Fatal("GetOrCreate returned nil")
	}
	if p.SelectedCommanderID != "default_commander" {
		t.Errorf("SelectedCommanderID: want default_commander, got %q", p.SelectedCommanderID)
	}
	if len(p.UnlockedBuffIDs) != 1 || p.UnlockedBuffIDs[0] != "iron_discipline" {
		t.Errorf("UnlockedBuffIDs: want [iron_discipline], got %v", p.UnlockedBuffIDs)
	}
	if len(p.EquippedBuffIDs) != 0 {
		t.Errorf("EquippedBuffIDs should be empty, got %v", p.EquippedBuffIDs)
	}
	if p.Version != CurrentVersion {
		t.Errorf("Version: want %d, got %d", CurrentVersion, p.Version)
	}
}

func TestManager_GetOrCreate_IdempotentOnSecondCall(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	playerID := "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"
	p1, err := m.GetOrCreate(playerID, "cmd1", nil)
	if err != nil {
		t.Fatalf("first GetOrCreate: %v", err)
	}
	p2, err := m.GetOrCreate(playerID, "cmd2", []string{"buff_x"})
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
	_, err := m.GetOrCreate(playerID, "cmd", nil)
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
