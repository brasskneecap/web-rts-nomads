package game

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestGeneratePerkCatalog is a one-shot generator (run explicitly with
// -run TestGeneratePerkCatalog) that flattens the pool-nested perks into the
// standalone catalog/perks/<id>/<id>.json layout. It is NOT part of the normal
// suite gate — it writes source files. Guarded by an env var so a plain
// `go test ./...` never regenerates.
func TestGeneratePerkCatalog(t *testing.T) {
	if os.Getenv("GENERATE_PERK_CATALOG") == "" {
		t.Skip("set GENERATE_PERK_CATALOG=1 to (re)generate the standalone perk catalog")
	}
	outRoot := filepath.Join("catalog", "perks")
	for key, entries := range embeddedPerkPools {
		unitType, pathName, rank, ok := splitPerkPoolKey(key)
		if !ok {
			t.Fatalf("bad pool key %q", key)
		}
		defs, err := buildPerkDefsFromPool(unitType, pathName, rank, entries)
		if err != nil {
			t.Fatalf("buildPerkDefsFromPool(%q): %v", key, err)
		}
		for _, def := range defs {
			dir := filepath.Join(outRoot, def.ID)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			// Marshal the *value* (drop the derived Wired flag — it's false on
			// the registry def anyway and recomputed by ListPerkDefs).
			raw, err := json.MarshalIndent(def, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, def.ID+".json"), raw, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// TestPerkCatalogEquivalentToPools is the equivalence gate: it builds a
// registry directly from the generated standalone catalog files and
// deep-compares it (via per-def JSON marshal) to perkDefsByID, which is still
// built from the pool-nested catalog at this point in the migration. A
// failure here means the generator is lossy and the migration must not
// proceed until it's fixed.
func TestPerkCatalogEquivalentToPools(t *testing.T) {
	// Build the "new" registry directly from the generated standalone files.
	fromCatalog := map[string]*PerkDef{}
	root := filepath.Join("catalog", "perks")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read %s: %v (run TestGeneratePerkCatalog first)", root, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		raw, err := os.ReadFile(filepath.Join(root, id, id+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var def PerkDef
		if err := json.Unmarshal(raw, &def); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		fromCatalog[def.ID] = &def
	}

	// The "old" registry is perkDefsByID (built from pools by init()).
	if len(fromCatalog) != len(perkDefsByID) {
		t.Fatalf("count mismatch: catalog=%d pools=%d", len(fromCatalog), len(perkDefsByID))
	}
	for id, oldDef := range perkDefsByID {
		newDef, ok := fromCatalog[id]
		if !ok {
			t.Fatalf("perk %q missing from standalone catalog", id)
		}
		// Wired is derived/false on both; compare the rest via JSON to avoid
		// map-ordering noise.
		oldJSON, _ := json.Marshal(oldDef)
		newJSON, _ := json.Marshal(newDef)
		if string(oldJSON) != string(newJSON) {
			t.Fatalf("perk %q differs:\n old=%s\n new=%s", id, oldJSON, newJSON)
		}
	}
}
