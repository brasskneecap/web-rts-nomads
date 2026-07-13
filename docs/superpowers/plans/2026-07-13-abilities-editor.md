# Abilities Editor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a create/edit/delete editor for `AbilityDef`s to the world editor, via a writable catalog overlay mirroring the Item and Unit-Types editors, with validated id-reference dropdowns and a family-gated full-coverage form.

**Architecture:** Server grows an `ABILITY_CATALOG_DIR` writable overlay (`ability_persistence.go`), a thin editor wrapper (`ability_editor.go`), `POST /abilities` + `DELETE /abilities/{id}` write routes, and six `GET /catalog/*` read routes feeding the dropdowns. Client grows the proven triad — `game/abilities/abilityEditorForm.ts` (pure), `abilityEditorApi.ts` (fetch), `components/AbilityEditorPanel.vue` (family-gated) — surfaced both as a world-editor toolbar popup and a standalone `/ability-editor` route.

**Tech Stack:** Go (server, `internal/game` + `internal/http`), TypeScript / Vue 3 (client, `game-portal`), Vitest, `go test`.

## Global Constraints

- Branch: `abilities-editor` (already created off `main`).
- Server-authoritative sim is untouched behavior-wise; the editor only writes catalog data an author opts into. No client-side gameplay logic.
- Commander abilities (`commander_abilities.go`, hardcoded Go registry) are OUT of scope.
- Reference fields (projectile / effects / summon-unit / auto-cast selector / category / damage-type) are set via validated dropdowns fed by `GET /catalog/*` — never free text.
- `Icon` is a string reference to already-bundled art (default = ability id). No runtime art upload (that is sub-project 4).
- No literal `cursor:` declarations in new/changed component CSS except `cursor: not-allowed` on forbidden-action states (global cursor rules handle the rest — CLAUDE.md).
- Ability id validation regex is the path-traversal gate: `^[a-z0-9_]+$`. Server validation is the correctness gate; handlers have no auth (matches the item/unit editor handlers).
- Build gates: server `go vet ./...` + `go build ./...` + `go test ./...` (NOT gofmt — CRLF noise). Client `npm run build` (`vue-tsc -b`, enforces `noUnusedLocals`) + `npm run test`. Run client commands from `client/src/game-portal`.
- Do NOT auto-commit beyond the per-task commits this plan specifies; do not push. (Committing per-task is the plan's TDD rhythm and is authorized; pushing/PRs are not.)

## File Structure

**Server (Go, package `game` unless noted):**
- `server/internal/game/ability_defs.go` — MODIFY: add `CastRange.MarshalJSON`; extract `validateAbilityDef`; make `getAbilityDef`/`ListAbilityDefs` overlay-aware.
- `server/internal/game/ability_persistence.go` — CREATE: the writable overlay.
- `server/internal/game/ability_editor.go` — CREATE: editor save/delete wrappers.
- `server/internal/game/autocast_selectors.go` — MODIFY: add `ListAutoCastSelectorNames`.
- `server/internal/http/editor_handlers.go` — MODIFY: `POST /abilities`, `DELETE /abilities/{id}`.
- `server/internal/http/router.go` — MODIFY: six `GET /catalog/*` read routes.
- Startup bootstrap (wherever `LoadPersistedUnitsIntoOverlay()` is called) — MODIFY: call `LoadPersistedAbilitiesIntoOverlay()`.

**Client (TS/Vue, `client/src/game-portal/src`):**
- `game/abilities/abilityEditorForm.ts` — CREATE: `AuthoredAbilityDef`, form transforms, family inference.
- `game/abilities/abilityEditorApi.ts` — CREATE: fetch/save/delete.
- `components/AbilityEditorPanel.vue` — CREATE: the family-gated editor panel.
- `views/AbilityEditor.vue` — CREATE: standalone route shell.
- `router/index.ts` — MODIFY: `/ability-editor` route.
- `components/world-editor/WorldEditorToolbar.vue` — MODIFY: flip `abilities` enabled.
- `components/world-editor/WorldEditorPanel.vue` — MODIFY: Abilities toolbar popup.

---

## Task 1: `CastRange.MarshalJSON` (round-trip the sentinel)

**Files:**
- Modify: `server/internal/game/ability_defs.go` (near the `CastRange` type, after `UnmarshalJSON` ~line 70)
- Test: `server/internal/game/ability_defs_test.go` (create or append)

**Interfaces:**
- Produces: `func (c CastRange) MarshalJSON() ([]byte, error)` — emits `"match_attack_range"` for the sentinel (any negative), the plain number otherwise.

- [ ] **Step 1: Write the failing test**

Append to `server/internal/game/ability_defs_test.go`:

```go
func TestCastRangeJSONRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   CastRange
		want string
	}{
		{"literal", CastRange(220), "220"},
		{"sentinel", CastRange(CastRangeMatchAttackRange), `"match_attack_range"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(raw) != tc.want {
				t.Fatalf("marshal = %s, want %s", raw, tc.want)
			}
			var back CastRange
			if err := json.Unmarshal(raw, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if back.MatchesAttackRange() != tc.in.MatchesAttackRange() || (!tc.in.MatchesAttackRange() && back != tc.in) {
				t.Fatalf("round-trip = %v, want %v", back, tc.in)
			}
		})
	}
}
```

Ensure the test file's imports include `"encoding/json"` and `"testing"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/game/ -run TestCastRangeJSONRoundTrip`
Expected: FAIL — sentinel marshals as `-1`, not `"match_attack_range"`.

- [ ] **Step 3: Add the MarshalJSON method**

In `server/internal/game/ability_defs.go`, immediately after the `UnmarshalJSON` method (before `MatchesAttackRange`):

```go
// MarshalJSON writes the match-attack-range sentinel back as the string
// "match_attack_range" (symmetric with UnmarshalJSON) so an authored ability
// round-trips through SaveAbilityDef without collapsing the sentinel to -1.
// Any concrete range marshals as its plain number.
func (c CastRange) MarshalJSON() ([]byte, error) {
	if c.MatchesAttackRange() {
		return []byte(`"match_attack_range"`), nil
	}
	return json.Marshal(float64(c))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/game/ -run TestCastRangeJSONRoundTrip`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/internal/game/ability_defs.go server/internal/game/ability_defs_test.go
git commit -m "feat(abilities): round-trip CastRange match_attack_range sentinel via MarshalJSON"
```

---

## Task 2: Extract `validateAbilityDef` (shared load + save gate)

**Files:**
- Modify: `server/internal/game/ability_defs.go` (the `loadAbilityDefs` loop, ~lines 530-572)
- Test: `server/internal/game/ability_defs_test.go`

**Interfaces:**
- Produces: `func validateAbilityDef(def *AbilityDef) error` — validates content (damageType, category, burn config) and normalizes defaultable numerics IN PLACE (`TargetCount<1→1`, `SummonCount<1→1`, channel `HealingMultiplier 0→1.0`, charge-fire `ManaToChargeRatio 0→1.0`). Does NOT check the id (callers gate that). Returned by later tasks' save path.

- [ ] **Step 1: Write the failing test**

Append to `server/internal/game/ability_defs_test.go`:

```go
func TestValidateAbilityDef(t *testing.T) {
	t.Run("rejects unknown category", func(t *testing.T) {
		def := AbilityDef{ID: "x", Category: "not_a_category"}
		if err := validateAbilityDef(&def); err == nil {
			t.Fatal("expected error for unknown category")
		}
	})
	t.Run("rejects burn without impact delay", func(t *testing.T) {
		def := AbilityDef{ID: "x", BurnDurationSeconds: 3, BurnTickIntervalSeconds: 1}
		if err := validateAbilityDef(&def); err == nil {
			t.Fatal("expected error: burn requires impactDelaySeconds > 0")
		}
	})
	t.Run("normalizes target and summon counts", func(t *testing.T) {
		def := AbilityDef{ID: "x"}
		if err := validateAbilityDef(&def); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if def.TargetCount != 1 || def.SummonCount != 1 {
			t.Fatalf("TargetCount=%d SummonCount=%d, want 1/1", def.TargetCount, def.SummonCount)
		}
	})
	t.Run("normalizes channel healing multiplier", func(t *testing.T) {
		def := AbilityDef{ID: "x", ChannelType: "beam"}
		_ = validateAbilityDef(&def)
		if def.HealingMultiplier != 1.0 {
			t.Fatalf("HealingMultiplier=%v, want 1.0", def.HealingMultiplier)
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/game/ -run TestValidateAbilityDef`
Expected: FAIL — `validateAbilityDef` undefined.

- [ ] **Step 3: Add `validateAbilityDef` and call it from the loader**

In `server/internal/game/ability_defs.go`, add the function (place it just above `loadAbilityDefs`):

```go
// validateAbilityDef checks an ability def's content and normalizes its
// defaultable numeric fields IN PLACE. It is the single validation gate shared
// by the catalog loader (loadAbilityDefs) and the editor save path
// (SaveAbilityDef), so a def that loads cleanly is exactly a def that saves
// cleanly. It deliberately does NOT check the id — the loader gates that
// against the directory name and the editor against abilityIDPattern.
func validateAbilityDef(def *AbilityDef) error {
	if def.DamageType != "" && !IsValidDamageType(def.DamageType) {
		return fmt.Errorf("damageType %q is not a registered damage type", def.DamageType)
	}
	if def.Category != "" && !IsValidAbilityCategory(def.Category) {
		return fmt.Errorf("category %q is not a registered ability category", def.Category)
	}
	if def.BurnDurationSeconds > 0 && def.ImpactDelaySeconds <= 0 {
		return fmt.Errorf("burnDurationSeconds requires impactDelaySeconds > 0")
	}
	if def.BurnDurationSeconds > 0 && def.BurnTickIntervalSeconds <= 0 {
		return fmt.Errorf("burnDurationSeconds requires burnTickIntervalSeconds > 0")
	}
	if def.TargetCount < 1 {
		def.TargetCount = 1
	}
	if def.SummonCount < 1 {
		def.SummonCount = 1
	}
	if def.ChannelType != "" && def.HealingMultiplier == 0 {
		def.HealingMultiplier = 1.0
	}
	if def.IsChargeFirePassive() && def.ManaToChargeRatio == 0 {
		def.ManaToChargeRatio = 1.0
	}
	return nil
}
```

Then REPLACE the inline validation+normalization block in `loadAbilityDefs` (the lines from the `def.DamageType != ""` check through the `ManaToChargeRatio` normalization, i.e. the current ~lines 530-572, but KEEP the `def.ID == ""`, `def.ID != idKey`, and duplicate-id `panic`s) with a single call. The loop body becomes:

```go
		if def.ID == "" {
			panic(rel + `: missing "id" field`)
		}
		if def.ID != idKey {
			panic(rel + ": def.ID " + def.ID + " does not match directory name " + idKey)
		}
		if err := validateAbilityDef(&def); err != nil {
			panic(rel + ": " + err.Error())
		}
		if _, dup := result[def.ID]; dup {
			panic(rel + `: duplicate ability id "` + def.ID + `"`)
		}
		result[def.ID] = def
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd server && go test ./internal/game/ -run 'TestValidateAbilityDef|TestCastRange'`
Expected: PASS. Then `cd server && go build ./...` — Expected: builds (confirms the loader still compiles and all catalog abilities still load without panic).

- [ ] **Step 5: Commit**

```bash
git add server/internal/game/ability_defs.go server/internal/game/ability_defs_test.go
git commit -m "refactor(abilities): extract validateAbilityDef shared by loader and save path"
```

---

## Task 3: Ability writable overlay (`ability_persistence.go`)

**Files:**
- Create: `server/internal/game/ability_persistence.go`
- Modify: `server/internal/game/ability_defs.go` (`getAbilityDef`, `ListAbilityDefs` — overlay-aware)
- Modify: startup bootstrap that calls `LoadPersistedUnitsIntoOverlay()` (add `LoadPersistedAbilitiesIntoOverlay()`)
- Test: `server/internal/game/ability_persistence_test.go`

**Interfaces:**
- Consumes: `validateAbilityDef` (Task 2), `CastRange.MarshalJSON` (Task 1).
- Produces:
  - `func SaveAbilityDef(def *AbilityDef) error`
  - `func DeleteAbilityOverride(id string) (existed bool, err error)`
  - `func AbilityIsEmbedded(id string) bool`
  - `func LoadPersistedAbilitiesIntoOverlay()`
  - `var abilityIDPattern = regexp.MustCompile(`^[a-z0-9_]+$`)`
  - overlay-aware `getAbilityDef` / `ListAbilityDefs`.

- [ ] **Step 1: Write the failing test**

Create `server/internal/game/ability_persistence_test.go`:

```go
package game

import (
	"testing"
)

func TestSaveAndOverlayAbilityDef(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ABILITY_CATALOG_DIR", dir)

	def := &AbilityDef{
		ID:          "test_bolt",
		DisplayName: "Test Bolt",
		Type:        AbilitySpell,
		CastRange:   CastRange(CastRangeMatchAttackRange),
		DamageAmount: 40,
	}
	if err := SaveAbilityDef(def); err != nil {
		t.Fatalf("SaveAbilityDef: %v", err)
	}

	got, ok := getAbilityDef("test_bolt")
	if !ok {
		t.Fatal("getAbilityDef: overlay def not found")
	}
	if !got.CastRange.MatchesAttackRange() {
		t.Fatalf("CastRange sentinel lost on round-trip: %v", got.CastRange)
	}
	if got.TargetCount != 1 {
		t.Fatalf("TargetCount not normalized: %d", got.TargetCount)
	}

	// Not embedded → delete removes it entirely.
	if AbilityIsEmbedded("test_bolt") {
		t.Fatal("test_bolt should not be embedded")
	}
	existed, err := DeleteAbilityOverride("test_bolt")
	if err != nil || !existed {
		t.Fatalf("DeleteAbilityOverride existed=%v err=%v", existed, err)
	}
	if _, ok := getAbilityDef("test_bolt"); ok {
		t.Fatal("def still resolvable after delete")
	}
}

func TestSaveAbilityDefRejectsBadID(t *testing.T) {
	t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())
	if err := SaveAbilityDef(&AbilityDef{ID: "Bad ID/../x"}); err == nil {
		t.Fatal("expected id-pattern rejection")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/game/ -run 'TestSaveAndOverlayAbilityDef|TestSaveAbilityDefRejectsBadID'`
Expected: FAIL — `SaveAbilityDef` / `AbilityIsEmbedded` / `DeleteAbilityOverride` undefined.

- [ ] **Step 3: Create `ability_persistence.go`**

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

var abilityIDPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

var (
	runtimeAbilitiesMu sync.RWMutex
	runtimeAbilities   = map[string]AbilityDef{}
)

// resolveAbilitiesDir returns the writable abilities catalog dir:
// ABILITY_CATALOG_DIR if set, else the dev source tree.
func resolveAbilitiesDir() (string, error) {
	if dir := os.Getenv("ABILITY_CATALOG_DIR"); dir != "" {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cwd, "internal", "game", "catalog", "abilities")
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	return "", fmt.Errorf("abilities directory not found at %s; set ABILITY_CATALOG_DIR env var to override", dir)
}

// SaveAbilityDef validates and writes an authored ability def to
// <dir>/<id>/<id>.json, then registers it in the overlay.
func SaveAbilityDef(def *AbilityDef) error {
	if !abilityIDPattern.MatchString(def.ID) {
		return fmt.Errorf("ability id %q must match %s", def.ID, abilityIDPattern)
	}
	if err := validateAbilityDef(def); err != nil {
		return err
	}
	dir, err := resolveAbilitiesDir()
	if err != nil {
		return err
	}
	outDir := filepath.Join(dir, def.ID)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, def.ID+".json"), raw, 0o644); err != nil {
		return err
	}
	runtimeAbilitiesMu.Lock()
	runtimeAbilities[def.ID] = *def
	runtimeAbilitiesMu.Unlock()
	return nil
}

// AbilityIsEmbedded reports whether an ability id ships in the embedded catalog.
func AbilityIsEmbedded(id string) bool {
	_, ok := abilityDefsByID[id]
	return ok
}

// DeleteAbilityOverride removes the override file + overlay entry for an id.
// Embed-backed ids reset to their shipped default; overlay-only ids are gone.
func DeleteAbilityOverride(id string) (existed bool, err error) {
	if !abilityIDPattern.MatchString(id) {
		return false, nil // never a valid override id; also blocks path traversal
	}
	dir, derr := resolveAbilitiesDir()
	if derr != nil {
		return false, derr
	}
	removed := false
	if rerr := os.Remove(filepath.Join(dir, id, id+".json")); rerr == nil {
		removed = true
		_ = os.Remove(filepath.Join(dir, id)) // best-effort: drop the now-empty dir
	}
	runtimeAbilitiesMu.Lock()
	_, inOverlay := runtimeAbilities[id]
	delete(runtimeAbilities, id)
	runtimeAbilitiesMu.Unlock()
	return removed || inOverlay, nil
}

// LoadPersistedAbilitiesIntoOverlay overlays writable ability defs onto the
// embed at startup. Best-effort; a bad file is skipped, never fatal.
func LoadPersistedAbilitiesIntoOverlay() {
	dir, err := resolveAbilitiesDir()
	if err != nil {
		slog.Info("persisted abilities: no writable abilities dir; using embedded catalog only", "err", err)
		return
	}
	if n := loadPersistedAbilitiesFromDir(dir); n > 0 {
		slog.Info("persisted abilities: overlaid on embedded catalog", "count", n, "dir", dir)
	}
}

func loadPersistedAbilitiesFromDir(dir string) int {
	loaded := 0
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			return nil
		}
		def, perr := parsePersistedAbilityFile(path)
		if perr != nil {
			slog.Warn("persisted abilities: skipped file", "file", d.Name(), "err", perr)
			return nil
		}
		runtimeAbilitiesMu.Lock()
		runtimeAbilities[def.ID] = *def
		runtimeAbilitiesMu.Unlock()
		loaded++
		return nil
	})
	return loaded
}

func parsePersistedAbilityFile(path string) (*AbilityDef, error) {
	raw, rerr := os.ReadFile(path)
	if rerr != nil {
		return nil, rerr
	}
	var d AbilityDef
	if uerr := json.Unmarshal(raw, &d); uerr != nil {
		return nil, uerr
	}
	if d.ID == "" {
		return nil, fmt.Errorf("ability has empty id")
	}
	if verr := validateAbilityDef(&d); verr != nil {
		return nil, verr
	}
	return &d, nil
}
```

- [ ] **Step 4: Make `getAbilityDef` and `ListAbilityDefs` overlay-aware**

In `server/internal/game/ability_defs.go`, REPLACE the existing `getAbilityDef` and `ListAbilityDefs` with:

```go
// getAbilityDef looks up an ability definition by id, overlay-first (an
// authored override wins over the embedded default), then the embedded
// catalog. The bool is false when no ability with that id is registered.
func getAbilityDef(id string) (AbilityDef, bool) {
	runtimeAbilitiesMu.RLock()
	if def, ok := runtimeAbilities[id]; ok {
		runtimeAbilitiesMu.RUnlock()
		return def, true
	}
	runtimeAbilitiesMu.RUnlock()
	def, ok := abilityDefsByID[id]
	return def, ok
}

// ListAbilityDefs returns every registered ability definition (overlay merged
// over embed) sorted by id.
func ListAbilityDefs() []AbilityDef {
	merged := make(map[string]AbilityDef, len(abilityDefsByID))
	for id, def := range abilityDefsByID {
		merged[id] = def
	}
	runtimeAbilitiesMu.RLock()
	for id, def := range runtimeAbilities {
		merged[id] = def
	}
	runtimeAbilitiesMu.RUnlock()
	defs := make([]AbilityDef, 0, len(merged))
	for _, def := range merged {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}
```

- [ ] **Step 5: Wire the startup loader**

Run: `cd server && grep -rn "LoadPersistedUnitsIntoOverlay()" --include=*.go` to find the bootstrap call site (NOT the definition in `unit_persistence.go`). In that same file, immediately after the `LoadPersistedUnitsIntoOverlay()` call, add:

```go
	game.LoadPersistedAbilitiesIntoOverlay()
```

(Use the `game.` qualifier only if the call site is outside package `game`; match the qualification of the adjacent `LoadPersistedUnitsIntoOverlay` call exactly.)

- [ ] **Step 6: Run tests + build**

Run: `cd server && go test ./internal/game/ -run 'Ability' && go build ./... && go vet ./...`
Expected: PASS + clean build/vet.

- [ ] **Step 7: Commit**

```bash
git add server/internal/game/ability_persistence.go server/internal/game/ability_defs.go server/internal/game/ability_persistence_test.go
git add -A  # picks up the one-line bootstrap edit
git commit -m "feat(abilities): writable ABILITY_CATALOG_DIR overlay + overlay-aware readers"
```

---

## Task 4: Editor wrapper (`ability_editor.go`)

**Files:**
- Create: `server/internal/game/ability_editor.go`
- Test: `server/internal/game/ability_editor_test.go`

**Interfaces:**
- Consumes: `SaveAbilityDef`, `DeleteAbilityOverride`, `abilityIDPattern`, `validateAbilityDef`, in-package `editorValidationError` + `IsEditorValidationError`.
- Produces:
  - `type EditorAbilitySaveRequest struct { Ability AbilityDef `json:"ability"` }`
  - `func SaveEditorAbility(req EditorAbilitySaveRequest) error`
  - `func DeleteEditorAbility(id string) (existed bool, err error)`

- [ ] **Step 1: Write the failing test**

Create `server/internal/game/ability_editor_test.go`:

```go
package game

import "testing"

func TestSaveEditorAbilityValidation(t *testing.T) {
	t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())
	err := SaveEditorAbility(EditorAbilitySaveRequest{Ability: AbilityDef{ID: "bad", Category: "nope"}})
	if err == nil || !IsEditorValidationError(err) {
		t.Fatalf("expected editor validation error, got %v", err)
	}
}

func TestSaveEditorAbilityOK(t *testing.T) {
	t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())
	if err := SaveEditorAbility(EditorAbilitySaveRequest{Ability: AbilityDef{ID: "ok_bolt", DamageAmount: 10}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := getAbilityDef("ok_bolt"); !ok {
		t.Fatal("saved ability not resolvable")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/game/ -run TestSaveEditorAbility`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Create `ability_editor.go`**

```go
package game

import "fmt"

// EditorAbilitySaveRequest is the body of POST /abilities.
type EditorAbilitySaveRequest struct {
	Ability AbilityDef `json:"ability"`
}

// SaveEditorAbility validates then persists an authored ability def. Validation
// failures are wrapped as editorValidationError so the handler returns 400.
func SaveEditorAbility(req EditorAbilitySaveRequest) error {
	ability := req.Ability
	if !abilityIDPattern.MatchString(ability.ID) {
		return editorValidationError{fmt.Errorf("ability id %q must match %s", ability.ID, abilityIDPattern)}
	}
	if err := validateAbilityDef(&ability); err != nil {
		return editorValidationError{err}
	}
	return SaveAbilityDef(&ability)
}

// DeleteEditorAbility removes an override; embed-backed ids reset to default.
func DeleteEditorAbility(id string) (existed bool, err error) {
	return DeleteAbilityOverride(id)
}
```

Note: `editorValidationError` is a single-field struct (`editorValidationError{err}`) already defined in `item_editor.go`; do NOT redefine it.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/game/ -run TestSaveEditorAbility`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/internal/game/ability_editor.go server/internal/game/ability_editor_test.go
git commit -m "feat(abilities): editor save/delete wrappers with validation-error wrapping"
```

---

## Task 5: HTTP write routes (`POST /abilities`, `DELETE /abilities/{id}`)

**Files:**
- Modify: `server/internal/http/editor_handlers.go` (inside `registerEditorRoutes`, after the `/units/` handler)
- Test: `server/internal/http/editor_handlers_abilities_test.go`

**Interfaces:**
- Consumes: `game.EditorAbilitySaveRequest`, `game.SaveEditorAbility`, `game.IsEditorValidationError`, `game.DeleteEditorAbility`, `game.AbilityIsEmbedded`.

- [ ] **Step 1: Write the failing test**

Create `server/internal/http/editor_handlers_abilities_test.go`:

```go
package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPostAbilitiesValidationFails(t *testing.T) {
	t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerEditorRoutes(mux)

	body := `{"ability":{"id":"x","category":"not_real"}}`
	req := httptest.NewRequest(http.MethodPost, "/abilities", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "validation_failed") {
		t.Fatalf("body missing validation_failed: %s", rec.Body.String())
	}
}

func TestPostAbilitiesSavesThenDeletes(t *testing.T) {
	t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerEditorRoutes(mux)

	post := httptest.NewRequest(http.MethodPost, "/abilities", strings.NewReader(`{"ability":{"id":"post_bolt","damageAmount":5}}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, post)
	if rec.Code != http.StatusCreated {
		t.Fatalf("save status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}

	del := httptest.NewRequest(http.MethodDelete, "/abilities/post_bolt", nil)
	drec := httptest.NewRecorder()
	mux.ServeHTTP(drec, del)
	if drec.Code != http.StatusOK || !strings.Contains(drec.Body.String(), "deleted") {
		t.Fatalf("delete status=%d body=%s", drec.Code, drec.Body.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/http/ -run TestPostAbilities`
Expected: FAIL — no `/abilities` route (404 / method-not-allowed).

- [ ] **Step 3: Add the handlers**

In `server/internal/http/editor_handlers.go`, inside `registerEditorRoutes`, after the `mux.HandleFunc("/units/", ...)` block, add (mirrors `/units` and `/units/` exactly):

```go
	mux.HandleFunc("/abilities", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
			return
		}
		var req game.EditorAbilitySaveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		if err := game.SaveEditorAbility(req); err != nil {
			if game.IsEditorValidationError(err) {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": req.Ability.ID, "status": "saved"})
	})

	mux.HandleFunc("/abilities/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/abilities/")
		if r.Method != http.MethodDelete {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "DELETE only")
			return
		}
		if id == "" || strings.Contains(id, "/") {
			writeJSONError(w, http.StatusBadRequest, "invalid_id", "expected /abilities/{id}")
			return
		}
		existed, err := game.DeleteEditorAbility(id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error())
			return
		}
		if !existed {
			writeJSONError(w, http.StatusNotFound, "not_found", "no editor override for "+id)
			return
		}
		status := "deleted"
		if game.AbilityIsEmbedded(id) {
			status = "reset"
		}
		writeJSON(w, map[string]string{"id": id, "status": status})
	})
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/http/ -run TestPostAbilities`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/internal/http/editor_handlers.go server/internal/http/editor_handlers_abilities_test.go
git commit -m "feat(abilities): POST /abilities + DELETE /abilities/{id} editor routes"
```

---

## Task 6: HTTP read routes for the dropdowns

**Files:**
- Modify: `server/internal/game/autocast_selectors.go` (add `ListAutoCastSelectorNames`)
- Modify: `server/internal/http/router.go` (six `GET /catalog/*` routes)
- Test: `server/internal/http/router_catalog_abilities_test.go`

**Interfaces:**
- Consumes: `game.ListAbilityDefs`, `game.ListProjectileDefs`, `game.ListEffectDefs`, `game.DamageTypes`, `game.AbilityCategories`.
- Produces: `func ListAutoCastSelectorNames() []string` (sorted); routes returning JSON `{ "abilities": [...] }`, `{ "projectiles": [...] }`, `{ "effects": [...] }`, `{ "autoCastSelectors": [...] }`, `{ "abilityCategories": [...] }`, `{ "damageTypes": [...] }`.

- [ ] **Step 1: Write the failing test**

Create `server/internal/http/router_catalog_abilities_test.go`:

```go
package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCatalogAbilitiesRoutes(t *testing.T) {
	mux := http.NewServeMux()
	registerCatalogRoutes(mux) // NOTE: use the actual function name router.go uses to register /catalog/* (see step 3)

	for _, path := range []string{
		"/catalog/abilities", "/catalog/projectiles", "/catalog/effects",
		"/catalog/autocast-selectors", "/catalog/ability-categories", "/catalog/damage-types",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, rec.Code)
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: bad json: %v", path, err)
		}
	}
}
```

Before running, open `server/internal/http/router.go` and confirm the exact function that registers the existing `/catalog/units` route (it may be `registerCatalogRoutes`, or the routes may be inline in a larger setup function). Use that real function name in the test's registration call; if the `/catalog/*` routes are registered inside a broader function, call that one instead. Adjust the test to whatever the real registration entrypoint is.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/http/ -run TestCatalogAbilitiesRoutes`
Expected: FAIL — the new `/catalog/*` paths 404.

- [ ] **Step 3: Add `ListAutoCastSelectorNames`**

In `server/internal/game/autocast_selectors.go`, add (ensure `"sort"` is imported):

```go
// ListAutoCastSelectorNames returns the registered auto-cast selector names,
// sorted. Exposed so the abilities editor can offer a validated dropdown.
func ListAutoCastSelectorNames() []string {
	names := make([]string, 0, len(autoCastSelectors))
	for name := range autoCastSelectors {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
```

- [ ] **Step 4: Add the six read routes**

In `server/internal/http/router.go`, next to the existing `mux.HandleFunc("/catalog/units", ...)`, add:

```go
	mux.HandleFunc("/catalog/abilities", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"abilities": game.ListAbilityDefs()})
	})

	mux.HandleFunc("/catalog/projectiles", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"projectiles": game.ListProjectileDefs()})
	})

	mux.HandleFunc("/catalog/effects", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"effects": game.ListEffectDefs()})
	})

	mux.HandleFunc("/catalog/autocast-selectors", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"autoCastSelectors": game.ListAutoCastSelectorNames()})
	})

	mux.HandleFunc("/catalog/ability-categories", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"abilityCategories": game.AbilityCategories()})
	})

	mux.HandleFunc("/catalog/damage-types", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"damageTypes": game.DamageTypes()})
	})
```

- [ ] **Step 5: Run test + build**

Run: `cd server && go test ./internal/http/ -run TestCatalogAbilitiesRoutes && go build ./... && go vet ./...`
Expected: PASS + clean.

- [ ] **Step 6: Commit**

```bash
git add server/internal/game/autocast_selectors.go server/internal/http/router.go server/internal/http/router_catalog_abilities_test.go
git commit -m "feat(abilities): GET /catalog/{abilities,projectiles,effects,autocast-selectors,ability-categories,damage-types}"
```

---

## Task 7: Client form module (`abilityEditorForm.ts`)

**Files:**
- Create: `client/src/game-portal/src/game/abilities/abilityEditorForm.ts`
- Test: `client/src/game-portal/src/game/abilities/abilityEditorForm.test.ts`

**Interfaces:**
- Produces:
  - `interface AuthoredAbilityDef` — modeled superset of `AbilityDef`; `castRange: number | 'match_attack_range'`.
  - `type AbilityFamily = 'basic' | 'channel' | 'charge' | 'meteor' | 'archmage'`.
  - `interface AbilityEditorForm extends AuthoredAbilityDef { remainder: Record<string, unknown> }`.
  - `function createBlankForm(): AbilityEditorForm`
  - `function formFromDef(def: AuthoredAbilityDef): AbilityEditorForm`
  - `function saveRequestFromForm(form: AbilityEditorForm): AuthoredAbilityDef`
  - `function inferFamily(def: AuthoredAbilityDef): AbilityFamily`

- [ ] **Step 1: Write the failing test**

Create `client/src/game-portal/src/game/abilities/abilityEditorForm.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import {
  createBlankForm,
  formFromDef,
  saveRequestFromForm,
  inferFamily,
  type AuthoredAbilityDef,
} from './abilityEditorForm'

describe('abilityEditorForm', () => {
  it('createBlankForm returns an empty-id form with a remainder bag', () => {
    const f = createBlankForm()
    expect(f.id).toBe('')
    expect(f.remainder).toEqual({})
  })

  it('formFromDef splits modeled keys from the remainder and round-trips', () => {
    const def: AuthoredAbilityDef = {
      id: 'heal',
      displayName: 'Heal',
      healAmount: 40,
      castRange: 'match_attack_range',
      // an unmodeled/future key must survive verbatim:
      futureKnob: { nested: true },
    } as AuthoredAbilityDef
    const form = formFromDef(def)
    expect(form.id).toBe('heal')
    expect(form.healAmount).toBe(40)
    expect(form.remainder).toEqual({ futureKnob: { nested: true } })

    const out = saveRequestFromForm(form)
    expect(out.castRange).toBe('match_attack_range')
    expect((out as Record<string, unknown>).futureKnob).toEqual({ nested: true })
    expect(out.healAmount).toBe(40)
  })

  it('saveRequestFromForm drops undefined modeled fields', () => {
    const form = createBlankForm()
    form.id = 'x'
    const out = saveRequestFromForm(form)
    expect('displayName' in out).toBe(false)
  })

  it('inferFamily picks the family from non-zero fields', () => {
    expect(inferFamily({ id: 'a', channelType: 'beam' } as AuthoredAbilityDef)).toBe('channel')
    expect(inferFamily({ id: 'a', chargeRequired: 5 } as AuthoredAbilityDef)).toBe('charge')
    expect(inferFamily({ id: 'a', impactDelaySeconds: 1.2 } as AuthoredAbilityDef)).toBe('meteor')
    expect(inferFamily({ id: 'a', chainCount: 3 } as AuthoredAbilityDef)).toBe('archmage')
    expect(inferFamily({ id: 'a', healAmount: 10 } as AuthoredAbilityDef)).toBe('basic')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client/src/game-portal && npx vitest run src/game/abilities/abilityEditorForm.test.ts`
Expected: FAIL — module not found.

- [ ] **Step 3: Create `abilityEditorForm.ts`**

```ts
// AuthoredAbilityDef is the full authored shape (superset of the runtime
// AbilityDef). Modeled fields are typed; unmodeled / future keys ride along via
// the index signature and are preserved verbatim through the form's remainder.
export interface AuthoredAbilityDef {
  id: string
  displayName?: string
  type?: 'spell' | 'passive' | ''
  // targeting
  canTargetSelf?: boolean
  canTargetAllies?: boolean
  canTargetEnemies?: boolean
  targetsPoint?: boolean
  // castRange: a world-pixel number OR the sentinel string.
  castRange?: number | 'match_attack_range'
  // cost / timing
  castTime?: number
  manaCost?: number
  cooldown?: number
  // classification
  damageType?: string
  tags?: string[]
  category?: string
  targetCount?: number
  // auto-cast trio
  supportsAutoCast?: boolean
  autoCastTargetSelector?: string
  defaultAutoCast?: boolean
  // presentation / refs (always shown)
  icon?: string
  casterAnimation?: string
  effectOnTarget?: string
  effectAtPoint?: string
  effectScale?: number
  burnEffectAtPoint?: string
  projectile?: string
  // family: basic
  healAmount?: number
  damageAmount?: number
  damagePerSecond?: number
  minorDamage?: boolean
  summonUnitType?: string
  summonCount?: number
  // family: channel-beam
  channelType?: string
  tickIntervalSeconds?: number
  manaCostPerTick?: number
  damagePerTick?: number
  healingMultiplier?: number
  allyHealRadius?: number
  // family: charge-fire
  chargeRequired?: number
  manaToChargeRatio?: number
  missileCount?: number
  damagePerMissile?: number
  targeting?: string
  allowDuplicateTargets?: boolean
  missileDelayMs?: number
  // family: meteor ground-hazard
  impactDelaySeconds?: number
  burnDurationSeconds?: number
  burnDamagePerTick?: number
  burnTickIntervalSeconds?: number
  burnRadius?: number
  // family: arch-mage spell
  radius?: number
  projectileSpeed?: number
  projectileScale?: number
  duration?: number
  chainCount?: number
  bounceRange?: number
  bounceDamageFalloff?: number
  pullStrength?: number
  slowMultiplier?: number
  slowDurationSeconds?: number
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  [key: string]: any
}

export type AbilityFamily = 'basic' | 'channel' | 'charge' | 'meteor' | 'archmage'

// The keys the form models — everything NOT in this set is preserved verbatim
// in the form's `remainder`.
const MODELED_KEYS = [
  'id','displayName','type','canTargetSelf','canTargetAllies','canTargetEnemies',
  'targetsPoint','castRange','castTime','manaCost','cooldown','damageType','tags',
  'category','targetCount','supportsAutoCast','autoCastTargetSelector','defaultAutoCast',
  'icon','casterAnimation','effectOnTarget','effectAtPoint','effectScale','burnEffectAtPoint',
  'projectile','healAmount','damageAmount','damagePerSecond','minorDamage','summonUnitType',
  'summonCount','channelType','tickIntervalSeconds','manaCostPerTick','damagePerTick',
  'healingMultiplier','allyHealRadius','chargeRequired','manaToChargeRatio','missileCount',
  'damagePerMissile','targeting','allowDuplicateTargets','missileDelayMs','impactDelaySeconds',
  'burnDurationSeconds','burnDamagePerTick','burnTickIntervalSeconds','burnRadius','radius',
  'projectileSpeed','projectileScale','duration','chainCount','bounceRange','bounceDamageFalloff',
  'pullStrength','slowMultiplier','slowDurationSeconds',
] as const

export interface AbilityEditorForm extends AuthoredAbilityDef {
  remainder: Record<string, unknown>
}

export function createBlankForm(): AbilityEditorForm {
  return { id: '', remainder: {} }
}

export function formFromDef(def: AuthoredAbilityDef): AbilityEditorForm {
  const modeled: Record<string, unknown> = {}
  const remainder: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(def)) {
    if ((MODELED_KEYS as readonly string[]).includes(k)) modeled[k] = v
    else remainder[k] = v
  }
  return { ...(modeled as AuthoredAbilityDef), remainder }
}

export function saveRequestFromForm(form: AbilityEditorForm): AuthoredAbilityDef {
  const { remainder, ...modeled } = form
  const out: Record<string, unknown> = { ...remainder }
  for (const [k, v] of Object.entries(modeled)) {
    if (v === undefined) continue
    out[k] = v
  }
  return out as AuthoredAbilityDef
}

// inferFamily picks the most specific mechanic family a def uses, so the panel
// opens on the right section when editing an existing ability. Checked
// most-specific first; defaults to 'basic'. Purely a UI convenience — the form
// always carries and saves every field regardless of the selected family.
export function inferFamily(def: AuthoredAbilityDef): AbilityFamily {
  if (def.channelType) return 'channel'
  if ((def.chargeRequired ?? 0) > 0) return 'charge'
  if ((def.impactDelaySeconds ?? 0) > 0 || (def.burnDurationSeconds ?? 0) > 0) return 'meteor'
  if ((def.chainCount ?? 0) > 0 || (def.radius ?? 0) > 0 || (def.pullStrength ?? 0) > 0 ||
      (def.slowMultiplier ?? 0) > 0 || (def.duration ?? 0) > 0) return 'archmage'
  return 'basic'
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd client/src/game-portal && npx vitest run src/game/abilities/abilityEditorForm.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add client/src/game-portal/src/game/abilities/abilityEditorForm.ts client/src/game-portal/src/game/abilities/abilityEditorForm.test.ts
git commit -m "feat(abilities): AuthoredAbilityDef form module with remainder round-trip + family inference"
```

---

## Task 8: Client API module (`abilityEditorApi.ts`)

**Files:**
- Create: `client/src/game-portal/src/game/abilities/abilityEditorApi.ts`
- Test: `client/src/game-portal/src/game/abilities/abilityEditorApi.test.ts`

**Interfaces:**
- Consumes: `AuthoredAbilityDef` (Task 7).
- Produces:
  - `class EditorValidationError extends Error { serverMessage: string }`
  - `fetchAuthoredAbilityDefs(): Promise<AuthoredAbilityDef[]>` (GET `/catalog/abilities`)
  - `fetchProjectileIds(): Promise<string[]>`, `fetchEffectIds(): Promise<string[]>`
  - `fetchAutoCastSelectors(): Promise<string[]>`, `fetchAbilityCategories(): Promise<string[]>`, `fetchDamageTypes(): Promise<string[]>`
  - `saveEditorAbility(ability: AuthoredAbilityDef): Promise<void>` (POST `/abilities`)
  - `deleteEditorAbility(id: string): Promise<'deleted' | 'reset'>`

- [ ] **Step 1: Write the failing test**

Create `client/src/game-portal/src/game/abilities/abilityEditorApi.test.ts`:

```ts
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  EditorValidationError,
  saveEditorAbility,
  fetchProjectileIds,
} from './abilityEditorApi'

afterEach(() => vi.restoreAllMocks())

function mockFetch(status: number, body: unknown) {
  vi.stubGlobal('fetch', vi.fn(async () => ({
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  })) as unknown as typeof fetch)
}

describe('abilityEditorApi', () => {
  it('saveEditorAbility throws EditorValidationError on 400 validation_failed', async () => {
    mockFetch(400, { error: 'validation_failed', message: 'bad category' })
    await expect(saveEditorAbility({ id: 'x' })).rejects.toBeInstanceOf(EditorValidationError)
  })

  it('fetchProjectileIds maps defs to ids', async () => {
    mockFetch(200, { projectiles: [{ id: 'fire_bolt' }, { id: 'holy_bolt' }] })
    await expect(fetchProjectileIds()).resolves.toEqual(['fire_bolt', 'holy_bolt'])
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client/src/game-portal && npx vitest run src/game/abilities/abilityEditorApi.test.ts`
Expected: FAIL — module not found.

- [ ] **Step 3: Create `abilityEditorApi.ts`**

```ts
import type { AuthoredAbilityDef } from './abilityEditorForm'

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

// EditorValidationError carries the server's validation message for inline
// display beside Save (the server is the validator). Body shape:
//   {"error":"validation_failed","message":"..."}
export class EditorValidationError extends Error {
  serverMessage: string
  constructor(message: string) {
    super(message)
    this.name = 'EditorValidationError'
    this.serverMessage = message
  }
}

async function getJson<T>(path: string): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`)
  if (!res.ok) throw new Error(`Failed to load ${path}: ${res.status}`)
  return (await res.json()) as T
}

export async function fetchAuthoredAbilityDefs(): Promise<AuthoredAbilityDef[]> {
  const data = await getJson<{ abilities: AuthoredAbilityDef[] }>('/catalog/abilities')
  return data.abilities ?? []
}

export async function fetchProjectileIds(): Promise<string[]> {
  const data = await getJson<{ projectiles: { id: string }[] }>('/catalog/projectiles')
  return (data.projectiles ?? []).map((p) => p.id)
}

export async function fetchEffectIds(): Promise<string[]> {
  const data = await getJson<{ effects: { id: string }[] }>('/catalog/effects')
  return (data.effects ?? []).map((e) => e.id)
}

export async function fetchAutoCastSelectors(): Promise<string[]> {
  const data = await getJson<{ autoCastSelectors: string[] }>('/catalog/autocast-selectors')
  return data.autoCastSelectors ?? []
}

export async function fetchAbilityCategories(): Promise<string[]> {
  const data = await getJson<{ abilityCategories: string[] }>('/catalog/ability-categories')
  return data.abilityCategories ?? []
}

export async function fetchDamageTypes(): Promise<string[]> {
  const data = await getJson<{ damageTypes: string[] }>('/catalog/damage-types')
  return data.damageTypes ?? []
}

export async function saveEditorAbility(ability: AuthoredAbilityDef): Promise<void> {
  const res = await fetch(`${API_BASE}/abilities`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ability }),
  })
  if (res.status === 400) {
    const body = (await res.json()) as { error?: string; message?: string }
    if (body.error === 'validation_failed') throw new EditorValidationError(body.message ?? 'validation failed')
  }
  if (!res.ok) throw new Error(`Failed to save ability: ${res.status}`)
}

export async function deleteEditorAbility(id: string): Promise<'deleted' | 'reset'> {
  const res = await fetch(`${API_BASE}/abilities/${encodeURIComponent(id)}`, { method: 'DELETE' })
  if (!res.ok) throw new Error(`Failed to delete ability: ${res.status}`)
  const body = (await res.json()) as { status: 'deleted' | 'reset' }
  return body.status
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd client/src/game-portal && npx vitest run src/game/abilities/abilityEditorApi.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add client/src/game-portal/src/game/abilities/abilityEditorApi.ts client/src/game-portal/src/game/abilities/abilityEditorApi.test.ts
git commit -m "feat(abilities): client API for ability catalog + save/delete"
```

---

## Task 9: The editor panel (`AbilityEditorPanel.vue`)

**Files:**
- Create: `client/src/game-portal/src/components/AbilityEditorPanel.vue`
- Test: `client/src/game-portal/src/components/AbilityEditorPanel.test.ts`
- Reference (structural template, read before writing): `client/src/game-portal/src/components/UnitTypeEditorPanel.vue`

**Interfaces:**
- Consumes: everything from `abilityEditorForm.ts` and `abilityEditorApi.ts`.
- Produces: a default-export SFC. No props, no emits (self-contained editor, same as `UnitTypeEditorPanel`).

**Panel structure (build to this shape):**
- On mount: `fetchAuthoredAbilityDefs()` for the left list, and in parallel `fetchProjectileIds`, `fetchEffectIds`, `fetchAutoCastSelectors`, `fetchAbilityCategories`, `fetchDamageTypes`, plus `fetchAuthoredUnitDefs()` (from `@/game/units/unitEditorApi`, reused) mapped to `.type` for the summon-unit dropdown. Store each list in a `ref`.
- Left column: list of abilities (id + displayName) with a "New" button (`createBlankForm()`). Clicking one runs `formFromDef(def)` into `form` and `selectedFamily.value = inferFamily(def)`.
- Right column form, two regions:
  1. **Always-shown block:** `id` (text; disabled when editing an existing id), `displayName`, `type` (select: `""`/`spell`/`passive`), `category` (select from `abilityCategories`, plus a blank option), `damageType` (select from `damageTypes`, plus blank), `tags` (comma-separated text ↔ `string[]`), targeting checkboxes (`canTargetSelf/Allies/Enemies`, `targetsPoint`), `castRange` (see below), `castTime`/`manaCost`/`cooldown`/`targetCount` (number inputs), the auto-cast trio (`supportsAutoCast` checkbox; when true, show `autoCastTargetSelector` select from `autoCastSelectors` and `defaultAutoCast` checkbox), `icon`/`casterAnimation` (text), and the reference selects `effectOnTarget`/`effectAtPoint`/`burnEffectAtPoint` (from `effectIds`, each with a blank option), `effectScale` (number), `projectile` (from `projectileIds`, blank option).
  2. **Family selector + revealed block:** a `<select v-model="selectedFamily">` with the five families; below it, `v-if="selectedFamily === '...'"` sections binding that family's fields (Basic: `healAmount`, `damageAmount`, `damagePerSecond`, `minorDamage`, `summonUnitType` [select from `unitTypeIds`, blank option], `summonCount`; Channel: `channelType`, `tickIntervalSeconds`, `manaCostPerTick`, `damagePerTick`, `healingMultiplier`, `allyHealRadius`; Charge: `chargeRequired`, `manaToChargeRatio`, `missileCount`, `damagePerMissile`, `targeting`, `allowDuplicateTargets`, `missileDelayMs`; Meteor: `impactDelaySeconds`, `burnDurationSeconds`, `burnDamagePerTick`, `burnTickIntervalSeconds`, `burnRadius`; Archmage: `radius`, `projectileSpeed`, `projectileScale`, `duration`, `chainCount`, `bounceRange`, `bounceDamageFalloff`, `pullStrength`, `slowMultiplier`, `slowDurationSeconds`).
- **`castRange` control:** a "Match attack range" checkbox bound to a computed `castRangeMatchesAttack` (`form.castRange === 'match_attack_range'`). When checked, set `form.castRange = 'match_attack_range'` and hide the number input; when unchecked, show a number input bound to `form.castRange` (coerce to Number; default 0).
- **Save:** `saveRequestFromForm(form)` → `saveEditorAbility(...)`. On `EditorValidationError`, show `err.serverMessage` inline next to Save; on success, refresh the list and show a saved indicator.
- **Delete/Reset:** call `deleteEditorAbility(form.id)`; label the button "Reset to default" when the id is in the embedded set (track an `embeddedIds` set derived from the initial fetch — an ability that came back from `/catalog/abilities` and cannot be individually distinguished as embedded vs overlay from the client, so: after a successful delete, use the returned status `'deleted' | 'reset'` to word the toast, and always label the button "Delete / Reset").
- **CSS:** no literal `cursor:` declarations; `cursor: not-allowed` only if you add a forbidden-action state. Reuse `UnitTypeEditorPanel.vue`'s class idioms/layout.

- [ ] **Step 1: Write the failing test**

Create `client/src/game-portal/src/components/AbilityEditorPanel.test.ts`:

```ts
import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AbilityEditorPanel from './AbilityEditorPanel.vue'

// Stub every fetch the panel makes on mount with an empty-but-valid payload,
// keyed by URL suffix.
function stubCatalogFetch() {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    const map: Record<string, unknown> = {
      '/catalog/abilities': { abilities: [{ id: 'heal', displayName: 'Heal', healAmount: 40 }] },
      '/catalog/projectiles': { projectiles: [{ id: 'fire_bolt' }] },
      '/catalog/effects': { effects: [{ id: 'healing_glow' }] },
      '/catalog/autocast-selectors': { autoCastSelectors: ['self'] },
      '/catalog/ability-categories': { abilityCategories: ['heal'] },
      '/catalog/damage-types': { damageTypes: ['holy'] },
      '/catalog/units': { units: [{ type: 'skeleton' }], paths: [], pathsByUnit: {} },
    }
    const key = Object.keys(map).find((k) => String(url).endsWith(k))
    return { ok: true, status: 200, json: async () => map[key ?? ''] ?? {} }
  }) as unknown as typeof fetch)
}

afterEach(() => vi.restoreAllMocks())

describe('AbilityEditorPanel', () => {
  it('mounts and lists abilities from the catalog', async () => {
    stubCatalogFetch()
    const wrapper = mount(AbilityEditorPanel)
    await flushPromises()
    expect(wrapper.text()).toContain('Heal')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client/src/game-portal && npx vitest run src/components/AbilityEditorPanel.test.ts`
Expected: FAIL — component not found.

- [ ] **Step 3: Read the reference then build the panel**

Read `client/src/game-portal/src/components/UnitTypeEditorPanel.vue` in full and mirror its list/form/save/delete shell, its `<script setup>` state pattern, and its scoped-CSS class names. Implement `AbilityEditorPanel.vue` per the "Panel structure" above. Bind every modeled field; the family selector uses `selectedFamily` (a `ref<AbilityFamily>('basic')`). Ensure no literal `cursor:` declarations.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd client/src/game-portal && npx vitest run src/components/AbilityEditorPanel.test.ts`
Expected: PASS

- [ ] **Step 5: Full client build gate**

Run: `cd client/src/game-portal && npm run build`
Expected: `vue-tsc -b` clean (this catches unused locals/imports the `--noEmit` path misses).

- [ ] **Step 6: Commit**

```bash
git add client/src/game-portal/src/components/AbilityEditorPanel.vue client/src/game-portal/src/components/AbilityEditorPanel.test.ts
git commit -m "feat(abilities): family-gated AbilityEditorPanel with validated ref dropdowns"
```

---

## Task 10: Standalone view + route (`/ability-editor`)

**Files:**
- Create: `client/src/game-portal/src/views/AbilityEditor.vue`
- Modify: `client/src/game-portal/src/router/index.ts`
- Reference: `client/src/game-portal/src/views/UnitTypeEditor.vue`

**Interfaces:**
- Consumes: `AbilityEditorPanel` (Task 9).

- [ ] **Step 1: Create the view**

`client/src/game-portal/src/views/AbilityEditor.vue` (mirror `UnitTypeEditor.vue`):

```vue
<template>
  <div class="ability-editor-view">
    <div class="ability-editor-view__topbar">
      <ExitButton @click="router.push('/')" />
    </div>
    <AbilityEditorPanel />
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import AbilityEditorPanel from '@/components/AbilityEditorPanel.vue'
import ExitButton from '@/components/ui/ExitButton.vue'
const router = useRouter()
</script>

<style scoped>
.ability-editor-view { position: relative; width: 100%; height: 100%; min-height: 0; display: flex; overflow: hidden; }
.ability-editor-view__topbar { position: absolute; top: 16px; right: 16px; z-index: 20; }
</style>
```

- [ ] **Step 2: Add the route**

In `client/src/game-portal/src/router/index.ts`, add the import next to the other editor views:

```ts
import AbilityEditor from '@/views/AbilityEditor.vue'
```

and the route next to `/unit-type-editor`:

```ts
    { path: '/ability-editor', component: AbilityEditor, meta: { hideDominionPanel: true } },
```

- [ ] **Step 3: Build gate**

Run: `cd client/src/game-portal && npm run build`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add client/src/game-portal/src/views/AbilityEditor.vue client/src/game-portal/src/router/index.ts
git commit -m "feat(abilities): standalone /ability-editor route + view"
```

---

## Task 11: World-editor toolbar + popup wiring

**Files:**
- Modify: `client/src/game-portal/src/components/world-editor/WorldEditorToolbar.vue` (flip `abilities` enabled)
- Modify: `client/src/game-portal/src/components/world-editor/WorldEditorToolbar.test.ts` (expectation)
- Modify: `client/src/game-portal/src/components/world-editor/WorldEditorPanel.vue` (popup)

**Interfaces:**
- Consumes: `AbilityEditorPanel` (Task 9).

- [ ] **Step 1: Update the toolbar test expectation**

Open `client/src/game-portal/src/components/world-editor/WorldEditorToolbar.test.ts` and find the assertion(s) about which categories are enabled. Update the expectation so `abilities` is enabled (in the enabled set / not in the disabled set), leaving `unit-paths`, `perks`, `effects`, `projectiles`, `campaigns` disabled.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client/src/game-portal && npx vitest run src/components/world-editor/WorldEditorToolbar.test.ts`
Expected: FAIL — `abilities` still disabled in the component.

- [ ] **Step 3: Flip the toolbar entry**

In `WorldEditorToolbar.vue`, change the abilities entry:

```ts
  { id: 'abilities', label: 'Abilities', enabled: true },
```

- [ ] **Step 4: Run toolbar test to verify it passes**

Run: `cd client/src/game-portal && npx vitest run src/components/world-editor/WorldEditorToolbar.test.ts`
Expected: PASS

- [ ] **Step 5: Wire the popup in `WorldEditorPanel.vue`**

Three edits mirroring the existing `unit-types` popup:

(a) Import next to `UnitTypeEditorPanel` (~line 1751):

```ts
import AbilityEditorPanel from '@/components/AbilityEditorPanel.vue'
```

(b) State + toolbar-select + active-id (~lines 1966, 1993, 2037). Add the ref:

```ts
const abilitiesPopupOpen = ref(false)
```

In `onToolbarSelect`, add a case (and remove `abilities` from the default-case comment):

```ts
    case 'abilities':
      abilitiesPopupOpen.value = true
      break
```

In the `toolbarActiveId` computed, add:

```ts
  if (abilitiesPopupOpen.value) return 'abilities'
```

(c) The modal, next to the `unitTypesPopupOpen` modal (~line 1740):

```vue
    <div v-if="abilitiesPopupOpen" class="we-modal-overlay">
      <div class="we-modal we-modal--wide">
        <div class="we-modal__header">
          <span>Ability Editor</span>
          <UiButton size="sm" @click="abilitiesPopupOpen = false">Close</UiButton>
        </div>
        <div class="we-modal__body">
          <AbilityEditorPanel />
        </div>
      </div>
    </div>
```

- [ ] **Step 6: Full client gate**

Run: `cd client/src/game-portal && npm run build && npm run test`
Expected: build clean, all tests green.

- [ ] **Step 7: Commit**

```bash
git add client/src/game-portal/src/components/world-editor/WorldEditorToolbar.vue client/src/game-portal/src/components/world-editor/WorldEditorToolbar.test.ts client/src/game-portal/src/components/world-editor/WorldEditorPanel.vue
git commit -m "feat(abilities): enable Abilities toolbar category + world-editor popup"
```

---

## Task 12: Final integration verification

**Files:** none (verification only).

- [ ] **Step 1: Full server suite**

Run: `cd server && go build ./... && go vet ./... && go test ./...`
Expected: builds, vet clean, all tests pass.

- [ ] **Step 2: Full client suite**

Run: `cd client/src/game-portal && npm run build && npm run test`
Expected: build clean, all tests pass.

- [ ] **Step 3: Manual E2E (author → assign → play)**

With the server running (writable `ABILITY_CATALOG_DIR` set, or dev tree):
1. Open the world editor → click **Abilities** → **New**. Author a heal-family ability (id `test_heal`, type `spell`, `canTargetAllies` true, `castRange` = match attack range, `healAmount` 50, `supportsAutoCast` on, selector `lowest_hp_percentage_ally_in_range`, `defaultAutoCast` on). Save → confirm a saved indicator and that it appears in the list.
2. Open **Unit Types** → edit a unit → add `test_heal` to its `abilities` → Save.
3. Place two of that unit + an enemy → **Play** → confirm the unit casts/auto-casts the heal. **Pause/Reset** returns to the editor.
4. Edit `test_heal` (change `healAmount`), Save, Play again → confirm the change takes.
5. Delete `test_heal` → confirm it disappears (it was overlay-only → "deleted").
6. Edit an EMBEDDED ability (e.g. `heal`), Save, then **Delete/Reset** → confirm the toast says "reset" and the shipped values return.

- [ ] **Step 4: Confirm no stray changes**

Run: `git status` — expect only the files this plan touched. `git log --oneline main..HEAD` — expect the per-task commits in order.

---

## Self-Review Notes (for the executor)

- **Spec coverage:** §1 overlay → Task 3; §1 `validateAbilityDef` → Task 2; §2 editor+HTTP write → Tasks 4-5; §2 read endpoints → Task 6; §3 client triad → Tasks 7-9; §4 wiring → Tasks 10-11; §5 `CastRange` → Task 1 (+ round-trip asserted in Task 3); §7 testing → per-task + Task 12; out-of-scope (commander abilities, new projectile/effect editors, art upload) → untouched by every task.
- **Type consistency:** `AuthoredAbilityDef` / `AbilityEditorForm` / `AbilityFamily` and the `MODELED_KEYS` set are defined once (Task 7) and consumed by Tasks 8-9. Server `EditorAbilitySaveRequest.Ability` (Task 4) matches the client POST body `{ ability }` (Task 8). Read-route JSON keys (`abilities`/`projectiles`/`effects`/`autoCastSelectors`/`abilityCategories`/`damageTypes`, Task 6) match the client fetchers (Task 8).
- **Watch item:** in Task 6, confirm the real `/catalog/*` registration function name in `router.go` before writing the test's registration call; the `/catalog/units` route shows the exact `mux` and package qualifier to mirror.
