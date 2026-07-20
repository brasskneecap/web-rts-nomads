package game

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSavePerkDefWritesAssociationFolder(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PERK_CATALOG_DIR", tmp)

	// Generic perk (empty association) lands under generic/.
	if err := SavePerkDef(&PerkDef{ID: "test_generic_perk", DisplayName: "Test", Path: ""}); err != nil {
		t.Fatalf("save generic: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "generic", "test_generic_perk", "test_generic_perk.json")); err != nil {
		t.Fatalf("expected file under generic/: %v", err)
	}

	// Associated perk lands under its path folder, and the body must NOT carry a path key.
	if err := SavePerkDef(&PerkDef{ID: "test_trapper_perk", DisplayName: "Test2", Path: "trapper"}); err != nil {
		t.Fatalf("save assoc: %v", err)
	}
	p := filepath.Join(tmp, "trapper", "test_trapper_perk", "test_trapper_perk.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("expected file under trapper/: %v", err)
	}
	if strings.Contains(string(raw), "\"path\"") {
		t.Errorf("path key must not be persisted in the file body: %s", raw)
	}

	// Cleanup overlay so we don't leak these synthetic perks into other tests.
	t.Cleanup(func() {
		_, _ = DeletePerkOverride("test_generic_perk")
		_, _ = DeletePerkOverride("test_trapper_perk")
	})
}
