package game

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadPersistedMapsFromDir_OverlaysDiskMaps proves the fix for the core
// map-editor-persistence bug: a map written to the writable map dir (where the
// editor saves) is loaded into the runtime overlay at startup, so an edit made
// in a previous session survives a server restart instead of only living in the
// non-persistent in-memory overlay of the process that made the edit.
func TestLoadPersistedMapsFromDir_OverlaysDiskMaps(t *testing.T) {
	base, ok := GetMapCatalogEntryByID("forest-1")
	if !ok {
		t.Skip("forest-1 not in catalog")
	}

	const id = "test-startup-persist"
	base.ID = id
	base.Name = "Persisted Startup Map"
	base.Map.ID = id
	base.Map.Name = "Persisted Startup Map"
	// Drop the inherited campaign block: reusing forest-1's would register a
	// second owner for campaign level "forest_01" and panic campaign discovery.
	base.Map.Campaign = nil

	// This test registers into the process-global runtime overlay; remove the
	// entry afterward so it does not leak into other tests in the package.
	t.Cleanup(func() {
		runtimeMapsMu.Lock()
		delete(runtimeMaps, id)
		runtimeMapsMu.Unlock()
	})

	raw, err := RenderCatalogEntryJSON(base)
	if err != nil {
		t.Fatalf("render fixture: %v", err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, id+".json"), raw, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if _, present := GetMapCatalogEntryByID(id); present {
		t.Fatalf("map %q unexpectedly present before startup load", id)
	}

	n := loadPersistedMapsFromDir(dir)
	if n < 1 {
		t.Fatalf("expected at least 1 map loaded from disk, got %d", n)
	}

	got, ok := GetMapCatalogEntryByID(id)
	if !ok {
		t.Fatalf("map %q not in catalog after startup load — edits would still be lost on restart", id)
	}
	if got.Name != "Persisted Startup Map" {
		t.Fatalf("loaded map name = %q, want %q", got.Name, "Persisted Startup Map")
	}
}

// TestLoadPersistedMapsFromDir_SkipsMalformedFiles proves a corrupt on-disk map
// (a user could hand-edit one, or a save could be interrupted) is skipped with a
// log rather than panicking the server at startup — unlike the embedded-catalog
// load, whose validity is a build-time invariant.
func TestLoadPersistedMapsFromDir_SkipsMalformedFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{ not valid json"), 0o644); err != nil {
		t.Fatalf("write malformed fixture: %v", err)
	}

	// Must not panic; the malformed file contributes nothing.
	n := loadPersistedMapsFromDir(dir)
	if n != 0 {
		t.Fatalf("expected 0 maps loaded from a dir with only a malformed file, got %d", n)
	}
}

// TestLoadPersistedMapsFromDir_MissingDir proves a nonexistent dir is a safe
// no-op (a distributed build with no writable map dir must still start).
func TestLoadPersistedMapsFromDir_MissingDir(t *testing.T) {
	n := loadPersistedMapsFromDir(filepath.Join(t.TempDir(), "does-not-exist"))
	if n != 0 {
		t.Fatalf("expected 0 maps loaded from a missing dir, got %d", n)
	}
}
