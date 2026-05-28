package profile

import (
	"os"
	"path/filepath"
	"testing"
)

// TestManager_WEBRTSProfilesDirEnv_HonoredWhenArgEmpty verifies that
// NewManager("") falls back to WEBRTS_PROFILES_DIR when the explicit arg is
// empty. This is load-bearing for §3 of the standalone-desktop-app change:
// the Tauri shell injects the OS user-data directory via this env var, and
// passing an empty arg from main.go must defer to it (and not silently fall
// through to "./profiles").
func TestManager_WEBRTSProfilesDirEnv_HonoredWhenArgEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WEBRTS_PROFILES_DIR", dir)

	m := NewManager("")

	playerID := "abcdef01-2345-6789-abcd-ef0123456789"
	if _, err := m.GetOrCreate(playerID, "default_commander"); err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}

	wantPath := filepath.Join(dir, playerID+".json")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("profile file not found at expected env-derived path %s: %v", wantPath, err)
	}

	// Also confirm the legacy default (./profiles) was NOT touched.
	if _, err := os.Stat(filepath.Join("./profiles", playerID+".json")); err == nil {
		t.Errorf("profile file unexpectedly created under ./profiles; env var was ignored")
	}
}

// TestManager_ExplicitArgOverridesEnv ensures the explicit profilesDir argument
// to NewManager takes precedence over WEBRTS_PROFILES_DIR, so callers (e.g.,
// tests) can pin a directory without environment interference.
func TestManager_ExplicitArgOverridesEnv(t *testing.T) {
	envDir := t.TempDir()
	argDir := t.TempDir()
	t.Setenv("WEBRTS_PROFILES_DIR", envDir)

	m := NewManager(argDir)

	playerID := "11111111-2222-3333-4444-555555555555"
	if _, err := m.GetOrCreate(playerID, "default_commander"); err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}

	if _, err := os.Stat(filepath.Join(argDir, playerID+".json")); err != nil {
		t.Errorf("profile not at explicit-arg path %s: %v", argDir, err)
	}
	if _, err := os.Stat(filepath.Join(envDir, playerID+".json")); err == nil {
		t.Errorf("profile unexpectedly written to env path %s when explicit arg was given", envDir)
	}
}
