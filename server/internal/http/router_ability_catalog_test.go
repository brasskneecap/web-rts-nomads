// server/internal/http/router_ability_catalog_test.go
package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// catalogAbilityEntry is a local decode shape mirroring the wire fields of
// abilityCatalogEntry that this test cares about. Using a targeted struct
// (rather than the full game.AbilityDef) keeps the test decoupled from
// unrelated AbilityDef fields.
type catalogAbilityEntry struct {
	ID                   string          `json:"id"`
	SchemaVersion        int             `json:"schemaVersion"`
	Program              json.RawMessage `json:"program"`
	CompiledProgram      json.RawMessage `json:"compiledProgram"`
	Runnable             bool            `json:"runnable"`
	GeneratedDescription string          `json:"generatedDescription"`
	Custom               bool            `json:"custom"`
}

// TestCatalogAbilitiesCompiledProgram covers Phase 5a Task 4: GET
// /catalog/abilities must additionally expose a display-only compiled
// composable view (compiledProgram) and a runnable flag for LEGACY abilities
// (schemaVersion 0/absent, no authored program), without regressing the
// existing generatedDescription field.
func TestCatalogAbilitiesCompiledProgram(t *testing.T) {
	// siphon_life was the LAST still-legacy catalog ability (see the
	// composable-abilities migration): every real catalog ability is now
	// schemaVersion:2, so this test's "legacy ability, compiledProgram
	// computed on the fly" half needs a scratch-seeded legacy ability
	// instead of a real catalog id. Seed one via the editor save endpoint —
	// same ABILITY_CATALOG_DIR + POST /abilities pattern
	// editor_convert_test.go's "convert_test_heal" uses — so it lands in the
	// same runtimeAbilities overlay GET /catalog/abilities reads.
	t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerAbilityCatalogRoutes(mux)
	registerEditorRoutes(mux)

	seedBody := `{"ability":{"id":"catalog_test_legacy_heal","type":"spell","canTargetAllies":true,"castRange":220,"manaCost":10,"healAmount":15}}`
	seed := httptest.NewRequest(http.MethodPost, "/abilities", strings.NewReader(seedBody))
	seedRec := httptest.NewRecorder()
	mux.ServeHTTP(seedRec, seed)
	if seedRec.Code != http.StatusCreated {
		t.Fatalf("seed status: %d %s", seedRec.Code, seedRec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/catalog/abilities", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /catalog/abilities status = %d, want 200", rec.Code)
	}

	var body struct {
		Abilities []catalogAbilityEntry `json:"abilities"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad json: %v", err)
	}

	byID := make(map[string]catalogAbilityEntry, len(body.Abilities))
	for _, e := range body.Abilities {
		byID[e.ID] = e
	}

	// The scratch-seeded legacy heal ability is the fixture for the
	// "compiledProgram gets computed on the fly, legacy has no authored
	// program" half of this test; fireball (below) is on the authored-
	// program/runnable=true half alongside raise_skeleton.
	legacyHeal, ok := byID["catalog_test_legacy_heal"]
	if !ok {
		t.Fatalf("catalog missing seeded scratch ability %q", "catalog_test_legacy_heal")
	}
	if legacyHeal.SchemaVersion != 0 {
		t.Fatalf("catalog_test_legacy_heal schemaVersion = %d, want 0 (legacy)", legacyHeal.SchemaVersion)
	}
	if string(legacyHeal.Program) != "null" && len(legacyHeal.Program) != 0 {
		t.Fatalf("catalog_test_legacy_heal program = %s, want null/absent (legacy has no authored program)", legacyHeal.Program)
	}
	if len(legacyHeal.CompiledProgram) == 0 || string(legacyHeal.CompiledProgram) == "null" {
		t.Fatalf("catalog_test_legacy_heal compiledProgram = %s, want a non-null compiled program", legacyHeal.CompiledProgram)
	}
	// select_targets/restore_health are both registered ActionDescriptors, so
	// a plain heal-shaped legacy ability's compiled program is runnable.
	if !legacyHeal.Runnable {
		t.Fatalf("catalog_test_legacy_heal runnable = false, want true (select_targets/restore_health are registered)")
	}
	if legacyHeal.GeneratedDescription == "" {
		t.Fatalf("catalog_test_legacy_heal generatedDescription is empty; regression in existing field")
	}

	// fireball is schemaVersion:2 in the live catalog as of the
	// composable-abilities migration: it now carries an AUTHORED Program
	// (arcane_bolt/chain_lightning too — see ability_legacy_fixtures_test.go),
	// same shape as raise_skeleton below. launch_projectile has a registered
	// ActionDescriptor, so its compiled program (a single launch_projectile
	// action) is fully executor-runnable — see
	// TestCompileExecutorRunnableClassification (server/internal/game).
	fireball, ok := byID["fireball"]
	if !ok {
		t.Fatalf("catalog missing %q", "fireball")
	}
	if fireball.SchemaVersion != 2 {
		t.Fatalf("fireball schemaVersion = %d, want 2 (composable-abilities migration)", fireball.SchemaVersion)
	}
	if len(fireball.Program) == 0 || string(fireball.Program) == "null" {
		t.Fatalf("fireball program = %s, want a non-null authored program", fireball.Program)
	}
	if len(fireball.CompiledProgram) != 0 && string(fireball.CompiledProgram) != "null" {
		t.Fatalf("fireball compiledProgram = %s, want null/absent (has an authored program, nothing to compile on the fly)", fireball.CompiledProgram)
	}
	if !fireball.Runnable {
		t.Fatalf("fireball runnable = false, want true (projectile action is registered with the executor)")
	}
	if fireball.GeneratedDescription == "" {
		t.Fatalf("fireball generatedDescription is empty; regression in existing field")
	}

	// raise_skeleton is schemaVersion:2 in the live catalog as of the
	// composable-abilities migration: it now carries an AUTHORED Program
	// (the embedded AbilityDef's own "program" field, not the display-only
	// CompiledProgram router.go computes on the fly for legacy abilities —
	// see registerAbilityCatalogRoutes's doc comment: "authored (v2)
	// abilities already carry their flow in AbilityDef.Program, so
	// CompiledProgram stays nil for them").
	raiseSkeleton, ok := byID["raise_skeleton"]
	if !ok {
		t.Fatalf("catalog missing %q", "raise_skeleton")
	}
	if raiseSkeleton.SchemaVersion != 2 {
		t.Fatalf("raise_skeleton schemaVersion = %d, want 2 (composable-abilities migration)", raiseSkeleton.SchemaVersion)
	}
	if len(raiseSkeleton.Program) == 0 || string(raiseSkeleton.Program) == "null" {
		t.Fatalf("raise_skeleton program = %s, want a non-null authored program", raiseSkeleton.Program)
	}
	if len(raiseSkeleton.CompiledProgram) != 0 && string(raiseSkeleton.CompiledProgram) != "null" {
		t.Fatalf("raise_skeleton compiledProgram = %s, want null/absent (has an authored program, nothing to compile on the fly)", raiseSkeleton.CompiledProgram)
	}
	if !raiseSkeleton.Runnable {
		t.Fatalf("raise_skeleton runnable = false, want true (summon program is fully executable)")
	}
	if raiseSkeleton.GeneratedDescription == "" {
		t.Fatalf("raise_skeleton generatedDescription is empty; regression in existing field")
	}
}

// TestCatalogAbilitiesCustomFlag covers the provenance flag GET
// /catalog/abilities exposes so the editor can label its destructive button
// "Delete" vs "Reset" before the click (see abilityCatalogEntry.Custom):
// true for an author-created id, false for a shipped catalog id (whether or
// not it currently has an override saved over it). Seeded the same way as
// TestCatalogAbilitiesCompiledProgram's scratch legacy ability.
func TestCatalogAbilitiesCustomFlag(t *testing.T) {
	t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerAbilityCatalogRoutes(mux)
	registerEditorRoutes(mux)

	seedBody := `{"ability":{"id":"catalog_test_custom_bolt","damageAmount":5}}`
	seed := httptest.NewRequest(http.MethodPost, "/abilities", strings.NewReader(seedBody))
	seedRec := httptest.NewRecorder()
	mux.ServeHTTP(seedRec, seed)
	if seedRec.Code != http.StatusCreated {
		t.Fatalf("seed status: %d %s", seedRec.Code, seedRec.Body.String())
	}

	// Also override a shipped ability, to prove Custom stays false even for
	// an embedded id that currently has an editor override on top of it.
	overrideBody := `{"ability":{"id":"fireball","displayName":"Overridden Fireball"}}`
	override := httptest.NewRequest(http.MethodPost, "/abilities", strings.NewReader(overrideBody))
	overrideRec := httptest.NewRecorder()
	mux.ServeHTTP(overrideRec, override)
	if overrideRec.Code != http.StatusCreated {
		t.Fatalf("override status: %d %s", overrideRec.Code, overrideRec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/catalog/abilities", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /catalog/abilities status = %d, want 200", rec.Code)
	}

	var body struct {
		Abilities []catalogAbilityEntry `json:"abilities"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	byID := make(map[string]catalogAbilityEntry, len(body.Abilities))
	for _, e := range body.Abilities {
		byID[e.ID] = e
	}

	authored, ok := byID["catalog_test_custom_bolt"]
	if !ok {
		t.Fatalf("catalog missing seeded scratch ability %q", "catalog_test_custom_bolt")
	}
	if !authored.Custom {
		t.Errorf("catalog_test_custom_bolt custom = false, want true (author-created)")
	}

	shipped, ok := byID["fireball"]
	if !ok {
		t.Fatalf("catalog missing %q", "fireball")
	}
	if shipped.Custom {
		t.Errorf("fireball custom = true, want false (shipped, even with an override on top)")
	}

	arcaneOrb, ok := byID["arcane_orb"]
	if !ok {
		t.Fatalf("catalog missing %q", "arcane_orb")
	}
	if arcaneOrb.Custom {
		t.Errorf("arcane_orb custom = true, want false (shipped)")
	}
}
