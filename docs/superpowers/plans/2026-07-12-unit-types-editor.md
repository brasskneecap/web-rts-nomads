# Unit-Types Editor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A world-editor data editor to create/edit/delete unit type definitions (`UnitDef`) with full field coverage, persisted through a new writable overlay mirroring the item editor, reachable from the world-editor toolbar and a standalone route.

**Architecture:** Server gains a `UNIT_CATALOG_DIR` writable overlay (`unit_persistence.go`) that shadows the embedded `catalog/units` tree; validation is factored out of the panic-in-loader into a reusable `validateUnitDef`; a `unit_editor.go` orchestrator + `POST/DELETE /units` HTTP routes save/delete authored defs to disk. Client gets `unitEditorApi.ts` + `unitEditorForm.ts` (full field coverage + an opaque `remainder` bag for lossless round-trip of the art blobs), a `UnitTypeEditorPanel.vue`, a standalone `/unit-type-editor` route, and world-editor toolbar-popup wiring.

**Tech Stack:** Go 1.22 (server, module `webrts/server`, root `server/`), Vue 3 + TypeScript SPA (`client/src/game-portal`, vitest), Tauri desktop shell (`desktop/`, Rust).

**Spec:** `docs/superpowers/specs/2026-07-12-unit-types-editor-design.md`

## Global Constraints

- Branch: all work on `unit-types-editor` (already created off `world-editor`; verify, never switch).
- **Never modify** the item editor (`components/ItemEditorPanel.vue`, `game/items/*`, `item_editor.go`, `item_persistence.go`) or the old map editor (`components/MapEditorPanel.vue`, `views/Editor.vue`). Mirror their patterns in NEW files; the world editor panel (`components/world-editor/WorldEditorPanel.vue`) IS edited (it's the copy).
- Follow the item-editor overlay + disk template; do not invent a new persistence pattern.
- `unitIDPattern = ^[a-z0-9_]+$` guards both `type` and `faction` path segments; enforce at handler AND persistence layers.
- Overlay registered only AFTER a successful disk write.
- `UnitDef` has NO custom `MarshalJSON` → persist with plain `json.MarshalIndent` (no disk-shadow struct). The three `json:"-"` advancement fields (`BonusArrows`, `TrapEffectBonus`, `TrapRadiusBonus`) are never serialized — correct and intended.
- Reuse the existing in-package `editorValidationError{}` / `IsEditorValidationError` (defined in `item_editor.go`) — do NOT redefine.
- Reuse the existing `GET /catalog/units` route (`router.go:51-58`) and client `fetchUnitDefs` — do NOT add a new list route.
- `game/` package must NOT import `profile`; `Locked` = caller holds `s.mu`; deterministic sim (no wall-clock / unseeded rand). Store refs by ID/string key, never pointer-across-ticks (AI_RULES).
- No literal `cursor:` declarations in new component CSS except `cursor: not-allowed` on forbidden-action states.
- All Go commands from `server/`; client from `client/src/game-portal` (`npm run test`, `npm run build`). `gofmt -l` flags the whole checkout (CRLF) — use `go vet`/`go build` as gates. Known pre-existing failures unrelated to this work: cmd/api `TestServerReadyLineAndStdinShutdown`; possibly ws `TestSPBaseline_StructuralShape`. Introduce NO new failures.
- Commit messages: short imperative.

---

### Task 1: Server — extract `validateUnitDef` from the panic-in-loader

**Files:**
- Modify: `server/internal/game/unit_defs.go` (loader `loadUnitDefsByType` lines 238-348)
- Test: `server/internal/game/unit_defs_validate_test.go` (create)

**Interfaces:**
- Produces: `func validateUnitDef(def *UnitDef) error` — validates a def's INTERNAL consistency (not directory placement). Returns `nil` for a valid def, a descriptive `error` otherwise. Reused by Task 2 (`parsePersistedUnitFile`, `SaveUnitDef`) and Task 3 (`SaveEditorUnit`).
- Consumes: existing package funcs `IsValidDamageType`, `getProjectileDef`, `getBuildingDef`, package var `combatProfiles`, consts `TargetClassGround`/`TargetClassFlyer`.

Note: the loader's directory-placement checks (`def.Type != unitKey`, `def.Faction != factionKey`, root-must-be-dir) stay INLINE in the loader — they are not def-internal and have no meaning at editor-save time. `validateUnitDef` covers everything else the loader currently panics on.

- [ ] **Step 1: Write the failing test**

Create `server/internal/game/unit_defs_validate_test.go`:
```go
package game

import "testing"

func validUnitDefForTest() UnitDef {
	return UnitDef{
		Type:        "test_unit",
		Faction:     "human",
		Name:        "Test Unit",
		HP:          100,
		Damage:      10,
		AttackRange: 1,
		AttackSpeed: 1,
		MoveSpeed:   2,
	}
}

func TestValidateUnitDef_ValidPasses(t *testing.T) {
	def := validUnitDefForTest()
	if err := validateUnitDef(&def); err != nil {
		t.Fatalf("expected valid def to pass, got %v", err)
	}
}

func TestValidateUnitDef_Rejections(t *testing.T) {
	cases := map[string]func(*UnitDef){
		"unknown damage type":   func(d *UnitDef) { d.DamageType = "not_a_real_type" },
		"unknown projectile":    func(d *UnitDef) { d.Projectile = "not_a_real_projectile" },
		"unknown building":      func(d *UnitDef) { d.RequiresBuildings = []string{"not_a_real_building"} },
		"bad targetable type":   func(d *UnitDef) { d.TargetableTypes = []string{"submarine"} },
		"dp chance > 1":         func(d *UnitDef) { d.DominionPointDropChance = 1.5 },
		"negative dp amount":    func(d *UnitDef) { d.DominionPointAmount = -1 },
		"negative projScale":    func(d *UnitDef) { d.ProjectileScale = -1 },
		"channel end < start":   func(d *UnitDef) { d.ChannelLoop = &ChannelLoopRange{Start: 5, End: 2} },
		"negative mana":         func(d *UnitDef) { d.MaxMana = -1 },
		"pathChances sum zero":  func(d *UnitDef) { d.PathChances = map[string]float64{"a": 0} },
		"unknown combatProfile": func(d *UnitDef) { d.CombatProfile = "not_a_profile" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			def := validUnitDefForTest()
			mutate(&def)
			if err := validateUnitDef(&def); err == nil {
				t.Fatalf("expected %s to be rejected, got nil", name)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/game/ -run TestValidateUnitDef -count=1`
Expected: FAIL — `undefined: validateUnitDef`.

- [ ] **Step 3: Add `validateUnitDef` and call it from the loader**

In `server/internal/game/unit_defs.go`, add this function (near `getUnitDef`, ~line 350):
```go
// validateUnitDef checks a unit def's internal consistency (field ranges and
// cross-references). It does NOT check directory placement (type==dir,
// faction==parent) — those are the loader's concern. Shared by the embed
// loader (which panics on error) and the editor persistence path (which
// surfaces the error).
func validateUnitDef(def *UnitDef) error {
	if def.CombatProfile != "" {
		if _, ok := combatProfiles[def.CombatProfile]; !ok {
			return fmt.Errorf("unit %q: combatProfile %q is not a known profile", def.Type, def.CombatProfile)
		}
	}
	if def.DominionPointDropChance < 0 || def.DominionPointDropChance > 1 {
		return fmt.Errorf("unit %q: dominionPointDropChance must be in [0,1]", def.Type)
	}
	if def.DominionPointAmount < 0 {
		return fmt.Errorf("unit %q: dominionPointAmount must be >= 0", def.Type)
	}
	for _, t := range def.TargetableTypes {
		if t != TargetClassGround && t != TargetClassFlyer {
			return fmt.Errorf("unit %q: targetableTypes entry %q must be one of %q | %q", def.Type, t, TargetClassGround, TargetClassFlyer)
		}
	}
	if def.DamageType != "" && !IsValidDamageType(def.DamageType) {
		return fmt.Errorf("unit %q: damageType %q is not a registered damage type", def.Type, string(def.DamageType))
	}
	if def.Projectile != "" {
		if _, ok := getProjectileDef(def.Projectile); !ok {
			return fmt.Errorf("unit %q: projectile %q is not a registered projectile def", def.Type, def.Projectile)
		}
	}
	if def.ProjectileScale < 0 {
		return fmt.Errorf("unit %q: projectileScale must be >= 0", def.Type)
	}
	if def.ChannelLoop != nil {
		if def.ChannelLoop.Start < 0 {
			return fmt.Errorf("unit %q: channelLoop.start must be >= 0", def.Type)
		}
		if def.ChannelLoop.End < def.ChannelLoop.Start {
			return fmt.Errorf("unit %q: channelLoop.end must be >= channelLoop.start", def.Type)
		}
	}
	if def.MaxMana < 0 || def.ManaRegenRate < 0 {
		return fmt.Errorf("unit %q: maxMana and manaRegenRate must be >= 0", def.Type)
	}
	if len(def.PathChances) > 0 {
		var sum float64
		for path, weight := range def.PathChances {
			if weight < 0 {
				return fmt.Errorf("unit %q: pathChances[%q] must be >= 0", def.Type, path)
			}
			sum += weight
		}
		if sum <= 0 {
			return fmt.Errorf("unit %q: pathChances weights must sum to > 0", def.Type)
		}
	}
	for _, b := range def.RequiresBuildings {
		if _, ok := getBuildingDef(b); !ok {
			return fmt.Errorf("unit %q: requiresBuildings entry %q is not a registered building type", def.Type, b)
		}
	}
	return nil
}
```

Then in `loadUnitDefsByType`, REPLACE the inline block of checks (from the `if def.CombatProfile != ""` check through the `RequiresBuildings` loop — the checks now living in `validateUnitDef`) with a single call, keeping the directory-placement checks that precede it:
```go
			if def.Type != unitKey {
				panic(rel + ": def.Type " + def.Type + " does not match directory name " + unitKey)
			}
			if def.Faction != factionKey {
				panic(rel + `: def.Faction "` + def.Faction + `" does not match parent directory "` + factionKey + `"`)
			}
			if err := validateUnitDef(&def); err != nil {
				panic(rel + ": " + err.Error())
			}
			if _, dup := result[def.Type]; dup {
				panic(rel + `: duplicate unit type "` + def.Type + `" — type ids must be globally unique across factions`)
			}
			result[def.Type] = def
```
(Leave the `def.Type == ""` empty-type panic in place before these — `validateUnitDef` does not check for empty type since the loader already does and the editor path checks the id pattern separately.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd server && go test ./internal/game/ -run TestValidateUnitDef -count=1`
Expected: PASS.
Run: `cd server && go build ./... && go vet ./internal/game/`
Expected: clean (loader still compiles and the embed catalog still loads — a broken extraction would panic at package init and fail the build/test).

- [ ] **Step 5: Commit**

```bash
git add server/internal/game/unit_defs.go server/internal/game/unit_defs_validate_test.go
git commit -m "Extract validateUnitDef from unit loader"
```

---

### Task 2: Server — `unit_persistence.go` writable overlay

**Files:**
- Create: `server/internal/game/unit_persistence.go`
- Modify: `server/internal/game/unit_defs.go` (`getUnitDef`, `ListUnitDefs` — make overlay-aware)
- Modify: `server/cmd/api/main.go:54` area (call `LoadPersistedUnitsIntoOverlay`)
- Test: `server/internal/game/unit_persistence_test.go` (create)

**Interfaces:**
- Consumes: `validateUnitDef` (Task 1); existing `unitDefsByType` embed map.
- Produces:
  - `var unitIDPattern = regexp.MustCompile(\`^[a-z0-9_]+$\`)`
  - `func resolveUnitsDir() (string, error)`
  - overlay state `runtimeUnitsMu sync.RWMutex`, `runtimeUnits map[string]UnitDef`
  - `func SaveUnitDef(def *UnitDef) error`
  - `func DeleteUnitOverride(unitType string) (existed bool, err error)`
  - `func UnitIsEmbedded(unitType string) bool`
  - `func LoadPersistedUnitsIntoOverlay()`
  - `getUnitDef`/`ListUnitDefs` now return overlay entries first (overlay-wins).

- [ ] **Step 1: Write the failing test**

Create `server/internal/game/unit_persistence_test.go`:
```go
package game

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveUnitDef_OverlayWinsAndReverts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_CATALOG_DIR", dir)

	// Override an existing embedded unit (archer) with a changed damage value.
	base, ok := getUnitDef("archer")
	if !ok {
		t.Fatal("archer must exist in embed")
	}
	edited := base
	edited.Damage = base.Damage + 777
	if err := SaveUnitDef(&edited); err != nil {
		t.Fatalf("SaveUnitDef: %v", err)
	}
	got, _ := getUnitDef("archer")
	if got.Damage != base.Damage+777 {
		t.Fatalf("overlay did not win: got damage %d", got.Damage)
	}
	// File written under <faction>/<type>/<type>.json
	if _, err := os.Stat(filepath.Join(dir, "human", "archer", "archer.json")); err != nil {
		t.Fatalf("expected override file: %v", err)
	}
	// Delete reverts to embed.
	existed, err := DeleteUnitOverride("archer")
	if err != nil || !existed {
		t.Fatalf("DeleteUnitOverride existed=%v err=%v", existed, err)
	}
	reverted, _ := getUnitDef("archer")
	if reverted.Damage != base.Damage {
		t.Fatalf("did not revert: got damage %d want %d", reverted.Damage, base.Damage)
	}
}

func TestSaveUnitDef_LosslessArtBlobs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_CATALOG_DIR", dir)
	def := UnitDef{
		Type: "art_unit", Faction: "human", Name: "Art", HP: 1, Damage: 1,
		AttackRange: 1, AttackSpeed: 1, MoveSpeed: 1,
		Bounds: json.RawMessage(`{"w":42,"h":7}`),
		Shadow: json.RawMessage(`{"scale":0.5}`),
	}
	if err := SaveUnitDef(&def); err != nil {
		t.Fatalf("SaveUnitDef: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "human", "art_unit", "art_unit.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var round UnitDef
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(round.Bounds) == "" || string(round.Shadow) == "" {
		t.Fatalf("art blobs lost on round-trip: bounds=%q shadow=%q", round.Bounds, round.Shadow)
	}
}

func TestSaveUnitDef_RejectsBadID(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_CATALOG_DIR", dir)
	def := UnitDef{Type: "../evil", Faction: "human", HP: 1, Damage: 1, AttackRange: 1, AttackSpeed: 1, MoveSpeed: 1}
	if err := SaveUnitDef(&def); err == nil {
		t.Fatal("expected bad-id rejection")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/game/ -run TestSaveUnitDef -count=1`
Expected: FAIL — `undefined: SaveUnitDef` / `DeleteUnitOverride`.

- [ ] **Step 3: Create `unit_persistence.go`**

```go
package game

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

var unitIDPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

// unitPathsSubdirName is skipped on any catalog walk — promotion paths are a
// separate catalog dimension owned by path_defs.go, not by this editor.
const unitPathsSubdirName = "paths"

var (
	runtimeUnitsMu sync.RWMutex
	runtimeUnits   = map[string]UnitDef{}
)

// resolveUnitsDir returns the writable units catalog dir: UNIT_CATALOG_DIR if
// set, else the dev source tree.
func resolveUnitsDir() (string, error) {
	if dir := os.Getenv("UNIT_CATALOG_DIR"); dir != "" {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cwd, "internal", "game", "catalog", "units")
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	return "", fmt.Errorf("units directory not found at %s; set UNIT_CATALOG_DIR env var to override", dir)
}

// SaveUnitDef validates and writes an authored unit def to
// <dir>/<faction>/<type>/<type>.json, then registers it in the overlay.
func SaveUnitDef(def *UnitDef) error {
	if !unitIDPattern.MatchString(def.Type) {
		return fmt.Errorf("unit type %q must match %s", def.Type, unitIDPattern)
	}
	if !unitIDPattern.MatchString(def.Faction) {
		return fmt.Errorf("unit faction %q must match %s", def.Faction, unitIDPattern)
	}
	if err := validateUnitDef(def); err != nil {
		return err
	}
	dir, err := resolveUnitsDir()
	if err != nil {
		return err
	}
	outDir := filepath.Join(dir, def.Faction, def.Type)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	// Remove any previous override under a different faction so an edited unit
	// never exists at two paths.
	removeUnitOverrideFiles(dir, def.Type)
	if err := os.WriteFile(filepath.Join(outDir, def.Type+".json"), raw, 0o644); err != nil {
		return err
	}
	runtimeUnitsMu.Lock()
	runtimeUnits[def.Type] = *def
	runtimeUnitsMu.Unlock()
	return nil
}

// UnitIsEmbedded reports whether a unit type exists in the embedded catalog.
func UnitIsEmbedded(unitType string) bool {
	_, ok := unitDefsByType[unitType]
	return ok
}

// DeleteUnitOverride removes the override file(s) + overlay entry for a type.
func DeleteUnitOverride(unitType string) (existed bool, err error) {
	if !unitIDPattern.MatchString(unitType) {
		return false, nil // never a valid override id; also blocks path traversal
	}
	dir, derr := resolveUnitsDir()
	if derr != nil {
		return false, derr
	}
	removed := removeUnitOverrideFiles(dir, unitType)
	runtimeUnitsMu.Lock()
	_, inOverlay := runtimeUnits[unitType]
	delete(runtimeUnits, unitType)
	runtimeUnitsMu.Unlock()
	return removed || inOverlay, nil
}

// removeUnitOverrideFiles deletes every <type>.json for the given type under
// dir, skipping the paths/ subdir. Returns whether anything was removed.
func removeUnitOverrideFiles(dir, unitType string) bool {
	removed := false
	target := unitType + ".json"
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == unitPathsSubdirName {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == target {
			if rerr := os.Remove(path); rerr == nil {
				removed = true
			}
		}
		return nil
	})
	return removed
}

// LoadPersistedUnitsIntoOverlay overlays writable unit defs onto the embed at startup.
func LoadPersistedUnitsIntoOverlay() {
	dir, err := resolveUnitsDir()
	if err != nil {
		slog.Info("persisted units: no writable units dir; using embedded catalog only", "err", err)
		return
	}
	if n := loadPersistedUnitsFromDir(dir); n > 0 {
		slog.Info("persisted units: overlaid on embedded catalog", "count", n, "dir", dir)
	}
}

func loadPersistedUnitsFromDir(dir string) int {
	loaded := 0
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == unitPathsSubdirName {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			return nil
		}
		def, perr := parsePersistedUnitFile(path)
		if perr != nil {
			slog.Warn("persisted units: skipped file", "file", d.Name(), "err", perr)
			return nil
		}
		runtimeUnitsMu.Lock()
		runtimeUnits[def.Type] = *def
		runtimeUnitsMu.Unlock()
		loaded++
		return nil
	})
	return loaded
}

func parsePersistedUnitFile(path string) (def *UnitDef, err error) {
	raw, rerr := os.ReadFile(path)
	if rerr != nil {
		return nil, rerr
	}
	var d UnitDef
	if uerr := json.Unmarshal(raw, &d); uerr != nil {
		return nil, uerr
	}
	if d.Type == "" {
		return nil, fmt.Errorf("unit has empty type")
	}
	if verr := validateUnitDef(&d); verr != nil {
		return nil, verr
	}
	return &d, nil
}
```

- [ ] **Step 4: Make `getUnitDef` / `ListUnitDefs` overlay-aware**

In `server/internal/game/unit_defs.go`, replace `getUnitDef` and `ListUnitDefs` (lines 350-362) with:
```go
func getUnitDef(unitType string) (UnitDef, bool) {
	runtimeUnitsMu.RLock()
	if def, ok := runtimeUnits[unitType]; ok {
		runtimeUnitsMu.RUnlock()
		return def, true
	}
	runtimeUnitsMu.RUnlock()
	def, ok := unitDefsByType[unitType]
	return def, ok
}

func ListUnitDefs() []UnitDef {
	merged := make(map[string]UnitDef, len(unitDefsByType))
	for t, def := range unitDefsByType {
		merged[t] = def
	}
	runtimeUnitsMu.RLock()
	for t, def := range runtimeUnits {
		merged[t] = def
	}
	runtimeUnitsMu.RUnlock()
	defs := make([]UnitDef, 0, len(merged))
	for _, def := range merged {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Type < defs[j].Type })
	return defs
}
```

- [ ] **Step 5: Wire startup loader**

In `server/cmd/api/main.go`, after the `game.LoadPersistedItemsIntoOverlay()` line (~54), add:
```go
	game.LoadPersistedUnitsIntoOverlay()
```

- [ ] **Step 6: Run tests + gates**

Run: `cd server && go test ./internal/game/ -run 'TestSaveUnitDef|TestValidateUnitDef' -count=1`
Expected: PASS.
Run: `cd server && go build ./... && go vet ./internal/game/ ./cmd/api/`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add server/internal/game/unit_persistence.go server/internal/game/unit_defs.go server/internal/game/unit_persistence_test.go server/cmd/api/main.go
git commit -m "Add writable unit overlay + persistence"
```

---

### Task 3: Server — `unit_editor.go` orchestrator + `POST/DELETE /units` routes

**Files:**
- Create: `server/internal/game/unit_editor.go`
- Modify: `server/internal/http/editor_handlers.go` (`registerEditorRoutes`)
- Test: `server/internal/game/unit_editor_test.go` (create)

**Interfaces:**
- Consumes: `SaveUnitDef`, `DeleteUnitOverride`, `UnitIsEmbedded`, `validateUnitDef`, `unitIDPattern`, existing `editorValidationError{}`/`IsEditorValidationError`.
- Produces:
  - `type EditorUnitSaveRequest struct { Unit UnitDef \`json:"unit"\` }`
  - `func SaveEditorUnit(req EditorUnitSaveRequest) error`
  - `func DeleteEditorUnit(unitType string) (existed bool, err error)`
  - HTTP: `POST /units` (save), `DELETE /units/{id}` (delete/reset).

- [ ] **Step 1: Write the failing test**

Create `server/internal/game/unit_editor_test.go`:
```go
package game

import "testing"

func TestSaveEditorUnit_ValidationError(t *testing.T) {
	t.Setenv("UNIT_CATALOG_DIR", t.TempDir())
	req := EditorUnitSaveRequest{Unit: UnitDef{Type: "bad", Faction: "human", HP: 1, Damage: 1, AttackRange: 1, AttackSpeed: 1, MoveSpeed: 1, Projectile: "nope"}}
	err := SaveEditorUnit(req)
	if err == nil || !IsEditorValidationError(err) {
		t.Fatalf("want editor validation error, got %v", err)
	}
}

func TestDeleteEditorUnit_EmbedResets(t *testing.T) {
	t.Setenv("UNIT_CATALOG_DIR", t.TempDir())
	base, _ := getUnitDef("archer")
	edited := base
	edited.Damage += 5
	if err := SaveEditorUnit(EditorUnitSaveRequest{Unit: edited}); err != nil {
		t.Fatalf("save: %v", err)
	}
	existed, err := DeleteEditorUnit("archer")
	if err != nil || !existed {
		t.Fatalf("delete existed=%v err=%v", existed, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/game/ -run 'TestSaveEditorUnit|TestDeleteEditorUnit' -count=1`
Expected: FAIL — `undefined: EditorUnitSaveRequest`.

- [ ] **Step 3: Create `unit_editor.go`**

```go
package game

import "fmt"

// EditorUnitSaveRequest is the body of POST /units.
type EditorUnitSaveRequest struct {
	Unit UnitDef `json:"unit"`
}

// SaveEditorUnit validates then persists an authored unit def. Validation
// failures are wrapped as editorValidationError so the handler returns 400.
func SaveEditorUnit(req EditorUnitSaveRequest) error {
	unit := req.Unit
	if !unitIDPattern.MatchString(unit.Type) {
		return editorValidationError{fmt.Errorf("unit type %q must match %s", unit.Type, unitIDPattern)}
	}
	if !unitIDPattern.MatchString(unit.Faction) {
		return editorValidationError{fmt.Errorf("unit faction %q must match %s", unit.Faction, unitIDPattern)}
	}
	if err := validateUnitDef(&unit); err != nil {
		return editorValidationError{err}
	}
	return SaveUnitDef(&unit)
}

// DeleteEditorUnit removes an override; embed-backed types reset to default.
func DeleteEditorUnit(unitType string) (existed bool, err error) {
	return DeleteUnitOverride(unitType)
}
```

- [ ] **Step 4: Run the Go test to verify it passes**

Run: `cd server && go test ./internal/game/ -run 'TestSaveEditorUnit|TestDeleteEditorUnit' -count=1`
Expected: PASS.

- [ ] **Step 5: Add the HTTP routes**

In `server/internal/http/editor_handlers.go`, inside `registerEditorRoutes(mux)`, add alongside the existing `POST /items` registration a units block (mirror the item handler's decode + error mapping + status codes exactly):
```go
	mux.HandleFunc("/units", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req game.EditorUnitSaveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		if err := game.SaveEditorUnit(req); err != nil {
			if game.IsEditorValidationError(err) {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": req.Unit.Type, "status": "saved"})
	})

	mux.HandleFunc("/units/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/units/")
		if id == "" || strings.Contains(id, "/") {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		existed, err := game.DeleteEditorUnit(id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error())
			return
		}
		if !existed {
			http.NotFound(w, r)
			return
		}
		status := "deleted"
		if game.UnitIsEmbedded(id) {
			status = "reset"
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": status})
	})
```
(If `editor_handlers.go` does not already import `strings` / `encoding/json`, add them. Confirm the exact `writeJSON`/`writeJSONError` signatures against the existing item handler in the same file and match them.)

- [ ] **Step 6: Gates**

Run: `cd server && go build ./... && go vet ./internal/game/ ./internal/http/`
Expected: clean.
Run: `cd server && go test ./internal/game/ -run 'TestSaveEditorUnit|TestDeleteEditorUnit' -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add server/internal/game/unit_editor.go server/internal/game/unit_editor_test.go server/internal/http/editor_handlers.go
git commit -m "Add unit editor orchestrator + POST/DELETE /units routes"
```

---

### Task 4: Server — verify no stale per-match unit-catalog snapshot

**Files:**
- Investigate: `server/internal/game/state.go`, `state_spawn.go`, and any `*Catalog` field on `GameState`
- Test: `server/internal/game/unit_overlay_visibility_test.go` (create)
- Modify: only if a snapshot is found

**Interfaces:**
- Consumes: `SaveUnitDef`, `getUnitDef` (Task 2).

Context: the item editor shipped a bug where `GameState.itemCatalog` snapshotted the embed-only singleton, making editor items invisible to gameplay. This task confirms units don't have the same trap.

- [ ] **Step 1: Investigate**

Run: `cd server && grep -rn "unitCatalog\|unitDefsByType\|EffectiveUnitDefs\|getUnitDef" internal/game/state.go internal/game/state_spawn.go`
Determine: does unit spawn read `getUnitDef` live (good), or copy a snapshot taken at match creation (bad)? Record the finding in the commit message.

- [ ] **Step 2: Write the guard test**

Create `server/internal/game/unit_overlay_visibility_test.go`. This test proves an overlay edit is visible through the same accessor spawn uses (`getUnitDef`). If Step 1 finds spawn uses a different accessor, target THAT accessor instead:
```go
package game

import "testing"

func TestUnitOverlayVisibleToGetUnitDef(t *testing.T) {
	t.Setenv("UNIT_CATALOG_DIR", t.TempDir())
	base, ok := getUnitDef("archer")
	if !ok {
		t.Fatal("archer must exist")
	}
	edited := base
	edited.HP = base.HP + 12345
	if err := SaveUnitDef(&edited); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, _ := getUnitDef("archer")
	if got.HP != base.HP+12345 {
		t.Fatalf("overlay edit not visible via getUnitDef: HP=%d", got.HP)
	}
	_, _ = DeleteUnitOverride("archer")
}
```

- [ ] **Step 3: Run + verify**

Run: `cd server && go test ./internal/game/ -run TestUnitOverlayVisible -count=1`
Expected: PASS (Task 2 made `getUnitDef` overlay-aware).

- [ ] **Step 4: If a snapshot was found in Step 1, fix it**

If `GameState` holds a per-match unit snapshot that spawn reads instead of `getUnitDef`, change the snapshot to source from the merged `ListUnitDefs()` (not the embed singleton), and add a match-level test that a spawned unit reflects an overlay edit. If NO snapshot exists (spawn reads `getUnitDef` live), no code change — the guard test above is the regression net. State which case held in the commit message.

- [ ] **Step 5: Commit**

```bash
git add server/internal/game/unit_overlay_visibility_test.go
git commit -m "Guard: unit overlay edits visible to spawn path"
```

---

### Task 5: Client — harden renderer placeholder for blank/no-bounds units

**Files:**
- Modify: `client/src/game-portal/src/game/rendering/unitSprites.ts` (placeholder/bounds path, ~line 361)
- Test: `client/src/game-portal/src/game/rendering/unitSprites.placeholder.test.ts` (create)

**Interfaces:**
- Produces: unit sprite resolution for an unknown/artless unit type returns a safe placeholder descriptor instead of throwing or returning a broken value.

Context: blank-created units have no spritesheet and no `bounds`. The renderer already has a placeholder path keyed on the def's bounds; a blank unit has no bounds, so that path must default to a fixed placeholder size rather than dereferencing missing bounds.

- [ ] **Step 1: Read the current placeholder path**

Read `unitSprites.ts` around the "No sprite (placeholder path)" comment (~line 361) and the exported function that resolves a unit's render descriptor / bounds. Identify the exact exported function a caller uses to get bounds/sprite for a `unitType` (e.g. `unitPlaceholderBounds(def)` or similar). Use the REAL exported name in the test below.

- [ ] **Step 2: Write the failing test**

Create `client/src/game-portal/src/game/rendering/unitSprites.placeholder.test.ts`. Replace `resolvePlaceholderBounds` and its import with the real exported symbol found in Step 1; assert it returns a positive-sized box for a def with no `bounds` and no spritesheet:
```ts
import { describe, expect, it } from 'vitest'
import { resolvePlaceholderBounds } from './unitSprites'

describe('unit placeholder render for artless units', () => {
  it('returns a positive-sized box when the def has no bounds', () => {
    const box = resolvePlaceholderBounds({ type: 'brand_new_unit' } as any)
    expect(box.w).toBeGreaterThan(0)
    expect(box.h).toBeGreaterThan(0)
  })
})
```

- [ ] **Step 3: Run to verify it fails**

Run: `cd client/src/game-portal && npm run test -- unitSprites.placeholder`
Expected: FAIL (symbol missing, or throws on missing bounds).

- [ ] **Step 4: Implement the fallback**

In `unitSprites.ts`, ensure the placeholder-bounds resolver defaults to a constant fixed box (e.g. `const PLACEHOLDER_BOUNDS = { w: 32, h: 32 }`) when `def.bounds` is absent, and export the resolver used by the test. Do not alter behavior for units that DO have bounds/sprites — only add the missing-bounds default. Keep the change minimal and localized.

- [ ] **Step 5: Run to verify pass + build**

Run: `cd client/src/game-portal && npm run test -- unitSprites.placeholder && npm run build`
Expected: PASS + clean build.

- [ ] **Step 6: Commit**

```bash
git add client/src/game-portal/src/game/rendering/unitSprites.ts client/src/game-portal/src/game/rendering/unitSprites.placeholder.test.ts
git commit -m "Harden unit renderer placeholder for artless units"
```

---

### Task 6: Client — `unitEditorApi.ts` + `unitEditorForm.ts` (full coverage + lossless round-trip)

**Files:**
- Create: `client/src/game-portal/src/game/units/unitEditorApi.ts`
- Create: `client/src/game-portal/src/game/units/unitEditorForm.ts`
- Test: `client/src/game-portal/src/game/units/unitEditorForm.test.ts` (create)

**Interfaces:**
- Produces:
  - `unitEditorApi.ts`: `saveEditorUnit(unit: AuthoredUnitDef): Promise<void>`, `deleteEditorUnit(type: string): Promise<'deleted'|'reset'>`, `fetchAuthoredUnitDefs(): Promise<AuthoredUnitDef[]>`, `class EditorValidationError extends Error`.
  - `unitEditorForm.ts`: `interface AuthoredUnitDef` (every authored field, index-signature remainder), `interface UnitEditorForm`, `createBlankForm(): UnitEditorForm`, `formFromDef(def: AuthoredUnitDef): UnitEditorForm`, `saveRequestFromForm(form: UnitEditorForm): AuthoredUnitDef`.
- Consumes: `POST /units`, `DELETE /units/{id}`, `GET /catalog/units` (existing).

- [ ] **Step 1: Write the failing round-trip test**

Create `client/src/game-portal/src/game/units/unitEditorForm.test.ts`:
```ts
import { describe, expect, it } from 'vitest'
import { createBlankForm, formFromDef, saveRequestFromForm, type AuthoredUnitDef } from './unitEditorForm'

const fullDef: AuthoredUnitDef = {
  type: 'archer', faction: 'human', name: 'Archer', archetype: 'ranged',
  hp: 120, armor: 2, damage: 18, attackRange: 5, attackSpeed: 1.2, splashRadius: 0,
  moveSpeed: 2.4, resourceCost: { gold: 40 }, meatCost: 1, spawnSeconds: 8,
  capabilities: ['attack'], combatProfile: 'ranged_basic', attackType: 'bow',
  damageType: 'physical', targetableTypes: ['ground', 'flyer'], projectile: 'arrow',
  projectileScale: 1, goldGatherAmount: 0, woodGatherAmount: 0, maxMana: 0,
  manaRegenRate: 0, visionRange: 6, flyer: false, abilities: [],
  requiresBuildings: [], pathChances: {}, dominionPointDropChance: 0.1,
  dominionPointAmount: 1, spawnExp: 0, nonCombat: false,
  // art blobs the form does NOT model — must survive untouched:
  attackVisual: { anchor: 'hand' }, bounds: { w: 20, h: 40 }, shadow: { scale: 0.6 },
}

describe('unitEditorForm round-trip', () => {
  it('formFromDef -> saveRequestFromForm is lossless, incl. art blobs', () => {
    const out = saveRequestFromForm(formFromDef(fullDef))
    expect(out).toEqual(fullDef)
  })

  it('createBlankForm produces a settable shell with no art', () => {
    const form = createBlankForm()
    form.type = 'my_unit'
    form.faction = 'human'
    const def = saveRequestFromForm(form)
    expect(def.type).toBe('my_unit')
    expect(def.faction).toBe('human')
    expect(def.attackVisual).toBeUndefined()
  })
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd client/src/game-portal && npm run test -- unitEditorForm`
Expected: FAIL — module missing.

- [ ] **Step 3: Implement `unitEditorForm.ts`**

Model every authored field as typed form state, and capture everything else in a `remainder` object. `saveRequestFromForm` recombines `...remainder` first, then the modeled fields, dropping `undefined`/empty so the output equals the input for a full def. The modeled key set below is the authoritative list; `REMAINDER_IGNORES` is the set of keys the form models (so they are NOT double-counted into `remainder`).
```ts
// AuthoredUnitDef is the full authored shape (superset of the render-time
// UnitDef). Modeled fields are typed; unmodeled keys (attackVisual/bounds/
// shadow + any future keys) ride along via the index signature.
export interface AuthoredUnitDef {
  type: string
  faction: string
  name?: string
  archetype?: string
  hp?: number
  armor?: number
  damage?: number
  attackRange?: number
  attackSpeed?: number
  splashRadius?: number
  moveSpeed?: number
  resourceCost?: Record<string, number>
  meatCost?: number
  spawnSeconds?: number
  capabilities?: string[]
  combatProfile?: string
  attackType?: string
  damageType?: string
  targetableTypes?: string[]
  projectile?: string
  projectileScale?: number
  goldGatherAmount?: number
  woodGatherAmount?: number
  maxMana?: number
  manaRegenRate?: number
  visionRange?: number
  flyer?: boolean
  abilities?: string[]
  requiresBuildings?: string[]
  pathChances?: Record<string, number>
  dominionPointDropChance?: number
  dominionPointAmount?: number
  spawnExp?: number
  experience?: number
  nonCombat?: boolean
  trainLabel?: string
  channelLoop?: { start: number; end: number }
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  [key: string]: any
}

// The keys the form models — everything NOT in this set is preserved verbatim
// in the form's `remainder`.
const MODELED_KEYS = [
  'type','faction','name','archetype','hp','armor','damage','attackRange',
  'attackSpeed','splashRadius','moveSpeed','resourceCost','meatCost','spawnSeconds',
  'capabilities','combatProfile','attackType','damageType','targetableTypes',
  'projectile','projectileScale','goldGatherAmount','woodGatherAmount','maxMana',
  'manaRegenRate','visionRange','flyer','abilities','requiresBuildings','pathChances',
  'dominionPointDropChance','dominionPointAmount','spawnExp','experience','nonCombat',
  'trainLabel','channelLoop',
] as const

export interface UnitEditorForm extends AuthoredUnitDef {
  remainder: Record<string, unknown>
}

export function createBlankForm(): UnitEditorForm {
  return { type: '', faction: '', remainder: {} }
}

export function formFromDef(def: AuthoredUnitDef): UnitEditorForm {
  const modeled: Record<string, unknown> = {}
  const remainder: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(def)) {
    if ((MODELED_KEYS as readonly string[]).includes(k)) modeled[k] = v
    else remainder[k] = v
  }
  return { ...(modeled as AuthoredUnitDef), remainder }
}

export function saveRequestFromForm(form: UnitEditorForm): AuthoredUnitDef {
  const { remainder, ...modeled } = form
  const out: Record<string, unknown> = { ...remainder }
  for (const [k, v] of Object.entries(modeled)) {
    if (v === undefined) continue
    out[k] = v
  }
  return out as AuthoredUnitDef
}
```

- [ ] **Step 4: Implement `unitEditorApi.ts`**

Mirror `itemEditorApi.ts` idioms (`API_BASE`, `EditorValidationError`, status parsing):
```ts
import type { AuthoredUnitDef } from './unitEditorForm'

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

export class EditorValidationError extends Error {
  serverMessage: string
  constructor(message: string) {
    super(message)
    this.name = 'EditorValidationError'
    this.serverMessage = message
  }
}

// fetchAuthoredUnitDefs loads the merged (overlay-over-embed) unit defs as raw
// authored objects, preserving every JSON key (incl. art blobs) at runtime.
export async function fetchAuthoredUnitDefs(): Promise<AuthoredUnitDef[]> {
  const res = await fetch(`${API_BASE}/catalog/units`)
  if (!res.ok) throw new Error(`Failed to load unit defs: ${res.status}`)
  const data = (await res.json()) as { units: AuthoredUnitDef[] }
  return data.units
}

export async function saveEditorUnit(unit: AuthoredUnitDef): Promise<void> {
  const res = await fetch(`${API_BASE}/units`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ unit }),
  })
  if (res.status === 400) {
    const body = (await res.json()) as { error?: string; message?: string }
    if (body.error === 'validation_failed') throw new EditorValidationError(body.message ?? 'validation failed')
  }
  if (!res.ok) throw new Error(`Failed to save unit: ${res.status}`)
}

export async function deleteEditorUnit(type: string): Promise<'deleted' | 'reset'> {
  const res = await fetch(`${API_BASE}/units/${encodeURIComponent(type)}`, { method: 'DELETE' })
  if (!res.ok) throw new Error(`Failed to delete unit: ${res.status}`)
  const body = (await res.json()) as { status: 'deleted' | 'reset' }
  return body.status
}
```
(Confirm the server 400 body shape from `writeJSONError` — it emits `{error, message}` per the item handler. If the field names differ, match them.)

- [ ] **Step 5: Run tests + typecheck**

Run: `cd client/src/game-portal && npm run test -- unitEditorForm`
Expected: PASS (both round-trip cases).
Run: `cd client/src/game-portal && npx vue-tsc --noEmit`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add client/src/game-portal/src/game/units/
git commit -m "Add unit editor API + form model with lossless round-trip"
```

---

### Task 7: Client — `UnitTypeEditorPanel.vue` + standalone view + route

**Files:**
- Create: `client/src/game-portal/src/components/UnitTypeEditorPanel.vue`
- Create: `client/src/game-portal/src/views/UnitTypeEditor.vue`
- Modify: `client/src/game-portal/src/router/index.ts` (import + route)
- Test: manual (build + open)

**Interfaces:**
- Consumes: `fetchAuthoredUnitDefs`, `saveEditorUnit`, `deleteEditorUnit`, `EditorValidationError` (Task 6); `createBlankForm`, `formFromDef`, `saveRequestFromForm`, `AuthoredUnitDef`, `UnitEditorForm` (Task 6).
- Produces: a self-contained panel (no required props) mountable both at `/unit-type-editor` and inside the world-editor popup (Task 8). No prop/emit contract.

Structure mirrors `ItemEditorPanel.vue`: a left unit list (with New / Delete), a right sectioned collapsible form. The section list is §3 of the spec (Identity, Stats, Cost, Combat, Resources, Mana, Vision, Abilities, Gating, Rewards, Flags).

- [ ] **Step 1: Implement the panel script logic**

Create `client/src/game-portal/src/components/UnitTypeEditorPanel.vue`. The `<script setup lang="ts">` block carries the load/select/new/save/delete state machine — this is the logic a reviewer must see:
```vue
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import {
  createBlankForm, formFromDef, saveRequestFromForm,
  type AuthoredUnitDef, type UnitEditorForm,
} from '@/game/units/unitEditorForm'
import {
  fetchAuthoredUnitDefs, saveEditorUnit, deleteEditorUnit, EditorValidationError,
} from '@/game/units/unitEditorApi'

const units = ref<AuthoredUnitDef[]>([])
const form = ref<UnitEditorForm>(createBlankForm())
const selectedType = ref<string | null>(null)
const saveError = ref('')
const loadError = ref('')
const busy = ref(false)

const FACTIONS = ['human', 'raider', 'wildborne', 'witherborne'] as const

async function reload() {
  try {
    units.value = await fetchAuthoredUnitDefs()
    loadError.value = ''
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : String(e)
  }
}

function selectUnit(def: AuthoredUnitDef) {
  form.value = formFromDef(def)
  selectedType.value = def.type
  saveError.value = ''
}

function newUnit() {
  form.value = createBlankForm()
  selectedType.value = null
  saveError.value = ''
}

async function save() {
  saveError.value = ''
  busy.value = true
  try {
    await saveEditorUnit(saveRequestFromForm(form.value))
    await reload()
    selectedType.value = form.value.type
  } catch (e) {
    saveError.value = e instanceof EditorValidationError ? e.serverMessage
      : e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function removeUnit() {
  if (!selectedType.value) return
  busy.value = true
  try {
    await deleteEditorUnit(selectedType.value)
    await reload()
    newUnit()
  } finally {
    busy.value = false
  }
}

onMounted(reload)
</script>
```

- [ ] **Step 2: Implement the template — one representative control per field kind**

Add the `<template>`. The §3 sections each contain fields of one of five binding shapes; implement each shape once as shown, then replicate per the §3 field table. Do NOT invent controls beyond the §3 fields.
```vue
<template>
  <div class="unit-editor">
    <aside class="unit-editor__list">
      <button class="unit-editor__new" :disabled="busy" @click="newUnit">+ New Unit</button>
      <p v-if="loadError" class="unit-editor__error">{{ loadError }}</p>
      <ul>
        <li v-for="u in units" :key="u.type">
          <button :class="{ 'is-selected': u.type === selectedType }" @click="selectUnit(u)">{{ u.type }}</button>
        </li>
      </ul>
    </aside>

    <section class="unit-editor__form">
      <!-- scalar string -->
      <label>Type <input v-model="form.type" :disabled="selectedType !== null" /></label>
      <!-- enum select -->
      <label>Faction
        <select v-model="form.faction">
          <option v-for="f in FACTIONS" :key="f" :value="f">{{ f }}</option>
        </select>
      </label>
      <!-- scalar number (repeat for hp/armor/damage/attackRange/attackSpeed/
           splashRadius/moveSpeed/meatCost/spawnSeconds/projectileScale/
           goldGatherAmount/woodGatherAmount/maxMana/manaRegenRate/visionRange/
           dominionPointDropChance/dominionPointAmount/spawnExp/experience) -->
      <label>HP <input type="number" v-model.number="form.hp" /></label>
      <!-- boolean (repeat for flyer/nonCombat) -->
      <label>Non-combat <input type="checkbox" v-model="form.nonCombat" /></label>
      <!-- string list (repeat for abilities/capabilities/requiresBuildings/
           targetableTypes); comma-separated editing is acceptable for v1 -->
      <label>Abilities (comma-separated)
        <input :value="(form.abilities ?? []).join(',')"
               @input="form.abilities = ($event.target as HTMLInputElement).value.split(',').map(s=>s.trim()).filter(Boolean)" />
      </label>
      <!-- string->number map (repeat for resourceCost/pathChances): render rows
           add/remove; store back into form.resourceCost -->
      <!-- ...replicate the above shapes for every field in spec §3... -->

      <p v-if="saveError" class="unit-editor__error">{{ saveError }}</p>
      <div class="unit-editor__actions">
        <button :disabled="busy || !form.type || !form.faction" @click="save">Save</button>
        <button :disabled="busy || selectedType === null" @click="removeUnit">Delete</button>
      </div>
    </section>
  </div>
</template>
```

- [ ] **Step 3: Styles (no literal cursor declarations)**

Add scoped styles for `.unit-editor` (a two-column flex/grid: list + form), `.is-selected`, `.unit-editor__error` (a warning color). Do NOT write any literal `cursor:` declaration (global rules cover it; buttons get the hover cursor automatically). The `:disabled` state is fine — no `cursor: not-allowed` needed unless a forbidden-action affordance is wanted.

- [ ] **Step 4: Standalone view + route**

Create `client/src/game-portal/src/views/UnitTypeEditor.vue` (mirror `ItemEditor.vue`):
```vue
<template>
  <div class="unit-type-editor-view">
    <div class="unit-type-editor-view__topbar">
      <ExitButton @click="router.push('/')" />
    </div>
    <UnitTypeEditorPanel />
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import UnitTypeEditorPanel from '@/components/UnitTypeEditorPanel.vue'
import ExitButton from '@/components/ui/ExitButton.vue'
const router = useRouter()
</script>

<style scoped>
.unit-type-editor-view { position: relative; width: 100%; height: 100%; min-height: 0; display: flex; overflow: hidden; }
.unit-type-editor-view__topbar { position: absolute; top: 16px; right: 16px; z-index: 20; }
</style>
```
In `client/src/game-portal/src/router/index.ts`, add the import (alongside the `ItemEditor` import at line 8):
```ts
import UnitTypeEditor from '@/views/UnitTypeEditor.vue'
```
and the route (alongside `/item-editor` at line 43):
```ts
    { path: '/unit-type-editor', component: UnitTypeEditor, meta: { hideDominionPanel: true } },
```

- [ ] **Step 5: Build + manual + commit**

Run: `cd client/src/game-portal && npm run build`
Expected: clean.
Manual: navigate to `/unit-type-editor` → the unit list loads → select `archer` → its fields populate → change HP → Save (no error) → Delete resets it → New Unit clears the form.
```bash
git add client/src/game-portal/src/components/UnitTypeEditorPanel.vue client/src/game-portal/src/views/UnitTypeEditor.vue client/src/game-portal/src/router/index.ts
git commit -m "Add unit type editor panel + standalone route"
```

---

### Task 8: Client — world-editor toolbar popup wiring

**Files:**
- Modify: `client/src/game-portal/src/components/world-editor/WorldEditorToolbar.vue:33` (enable `unit-types`)
- Modify: `client/src/game-portal/src/components/world-editor/WorldEditorPanel.vue` (ref, case, modal, active-id, import)
- Test: manual (build + open)

**Interfaces:**
- Consumes: `UnitTypeEditorPanel.vue` (Task 7), the toolbar `unit-types` category.
- Produces: a modal overlay hosting the unit editor, following the exact `itemsPopupOpen` pattern already in the panel.

- [ ] **Step 1: Enable the toolbar category**

In `client/src/game-portal/src/components/world-editor/WorldEditorToolbar.vue`, change line 33 from:
```ts
  { id: 'unit-types', label: 'Unit Types', enabled: false },
```
to:
```ts
  { id: 'unit-types', label: 'Unit Types', enabled: true },
```

- [ ] **Step 2: Add the popup state + wiring in WorldEditorPanel.vue**

Mirror the four `itemsPopupOpen` touch points (import ~1721, ref ~1934, case ~1958, modal ~1701, active-id ~1996):

Import (alongside the `ItemEditorPanel` import ~line 1721):
```ts
import UnitTypeEditorPanel from '@/components/UnitTypeEditorPanel.vue'
```
Ref (alongside `itemsPopupOpen` ~line 1934):
```ts
const unitTypesPopupOpen = ref(false)
```
Toolbar-select case (alongside `case 'items':` ~line 1958):
```ts
    case 'unit-types':
      unitTypesPopupOpen.value = true
      break
```
Active-id computed line (alongside the `items` line ~1996):
```ts
  if (unitTypesPopupOpen.value) return 'unit-types'
```
Modal overlay block (alongside the item modal ~line 1701):
```html
    <div v-if="unitTypesPopupOpen" class="we-modal-overlay">
      <div class="we-modal we-modal--wide">
        <div class="we-modal__header">
          <span>Unit Type Editor</span>
          <UiButton size="sm" @click="unitTypesPopupOpen = false">Close</UiButton>
        </div>
        <div class="we-modal__body">
          <UnitTypeEditorPanel />
        </div>
      </div>
    </div>
```

- [ ] **Step 3: Build + manual + commit**

Run: `cd client/src/game-portal && npm run build`
Expected: clean.
Manual: world editor → toolbar → Unit Types (now enabled) → the unit editor opens in a modal → edit a unit → Close → back to the map. Then the milestone E2E: edit `archer` damage → Save → Close → place an archer → Play → it fights with the new damage → Stop; create a blank unit (faction+id) → place it → renders as a placeholder.
```bash
git add client/src/game-portal/src/components/world-editor/WorldEditorToolbar.vue client/src/game-portal/src/components/world-editor/WorldEditorPanel.vue
git commit -m "Wire unit type editor into world editor toolbar"
```

---

### Task 9: Desktop env dir + verification sweep

**Files:**
- Modify: `desktop/src-tauri/src/supervisor.rs` (add `UNIT_CATALOG_DIR`)
- Fixes only otherwise.

- [ ] **Step 1: Add the Tauri env dir**

In `desktop/src-tauri/src/supervisor.rs`, where the other four catalog env vars are set from `locate_repo_catalog_dir()` (lines ~212-217), add a fifth mapping `UNIT_CATALOG_DIR` → the repo `catalog/units` dir, following the exact pattern of `ITEM_CATALOG_DIR`. Without this the packaged Tauri build would hit "units directory not found" on save.

- [ ] **Step 2: Full gates**

Run: `cd server && go vet ./... && go build ./... && go test ./... -count=1 2>&1 | grep -E "^(--- FAIL|FAIL)"`
Expected: only the known pre-existing failures (Global Constraints).
Run: `cd client/src/game-portal && npm run test && npm run build`
Expected: green + clean.
Run (desktop compiles — only if a Rust toolchain is present; otherwise note skipped):
`cd desktop/src-tauri && cargo check`
Expected: clean, or note "cargo unavailable, env-var edit is a 3-line mirror of ITEM_CATALOG_DIR".

- [ ] **Step 3: Untouched-editor check**

Run: `cd "$(git rev-parse --show-toplevel)" && git diff --stat world-editor..unit-types-editor -- client/src/game-portal/src/components/MapEditorPanel.vue client/src/game-portal/src/views/Editor.vue client/src/game-portal/src/components/ItemEditorPanel.vue server/internal/game/item_editor.go server/internal/game/item_persistence.go`
Expected: NO changes to any of these (map editor + item editor untouched).

- [ ] **Step 4: Catalog hygiene**

Manual saves during testing write real files under `server/internal/game/catalog/units/`. `git status` must show none committed. Revert any: `git checkout -- server/internal/game/catalog/units/`.

- [ ] **Step 5: Commit any fixes**

```bash
git add -A client/ server/ desktop/
git commit -m "Unit type editor: desktop env dir + verification fixes"
```
(Skip the commit if only the supervisor.rs change exists — fold it in: `git commit -m "Add UNIT_CATALOG_DIR to desktop supervisor"`.)
