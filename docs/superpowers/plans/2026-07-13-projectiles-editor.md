# Projectiles Editor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a create/edit/delete editor for `ProjectileDef`s (projectiles/beams) to the world editor, via a writable `PROJECTILE_CATALOG_DIR` overlay mirroring the shipped Effects/Abilities editors.

**Architecture:** Server grows `projectile_persistence.go` (overlay + overlay-aware readers) + a `validateProjectileDef` gate (validate + normalize) + `projectile_editor.go` + `POST /projectiles` / `DELETE /projectiles/{id}`. Client grows the triad — `game/projectiles/projectileEditorForm.ts`, `projectileEditorApi.ts`, `components/ProjectileEditorPanel.vue` — surfaced as a `/projectile-editor` route and a world-editor screen (the CURRENT full-screen-switch pattern). `GET /catalog/projectiles` already exists (becomes overlay-merged). The two effect-reference dropdowns reuse the existing `fetchEffectIds` helper.

**Tech Stack:** Go (`internal/game`, `internal/http`), TypeScript / Vue 3 (`game-portal`), Vitest, `go test`.

## Global Constraints

- Branch: `reference-def-editors` (sub-project 2 of 3; Effects already merged on this branch).
- `ProjectileDef` (6 fields): `ID string json:"id"`, `Kind EmitterKind json:"kind,omitempty"` (`"projectile" | "beam"`, empty ⇒ projectile), `DurationMs int json:"durationMs,omitempty"` (beam flash ms), `Speed float64 json:"speed"` (px/s), `FollowEffect string json:"followEffect,omitempty"` (→ effect id), `ImpactEffect string json:"impactEffect,omitempty"` (→ effect id).
- Loader constants (in `projectile_defs.go`): `EmitterKindProjectile`, `EmitterKindBeam`, `defaultBeamDurationMs` (260), `defaultProjectileSpeed`.
- `validateProjectileDef` VALIDATES + NORMALIZES in place: `Kind==""→EmitterKindProjectile`; error if `Kind ∉ {projectile, beam}`; `Kind==beam && DurationMs<=0 → defaultBeamDurationMs`; `Speed<=0 → defaultProjectileSpeed`.
- `ProjectileDef.Speed` affects the SIMULATION; the overlay is authoring-time-only (accepted invariant, same as units/abilities). No `game`→`profile` write.
- `projectileIDPattern = ^[a-z0-9_]+$` — path-traversal gate on every id entry point.
- `kind` is a hardcoded closed-enum dropdown (`['projectile', 'beam']`); `followEffect`/`impactEffect` are dropdowns fed by `/catalog/effects` (via the existing `fetchEffectIds` from `@/game/abilities/abilityEditorApi`), each with a blank "(none)" option.
- Catalog layout: per-id subfolder `catalog/projectiles/<id>/<id>.json`.
- **WorldEditorPanel wiring uses the CURRENT screen-switch pattern** (`activeScreen: EditorScreen` union + shared `case` arm + `v-else-if` chain in `<section class="we-screen">`), NOT any modal pattern.
- No literal `cursor:` in new/changed component CSS except `cursor: not-allowed` on forbidden states.
- Build gates: server `go build ./...` + `go vet ./...` + `go test ./...` (NOT gofmt). Client `npm run build` (`vue-tsc -b`) + `npm run test`, from `client/src/game-portal`. NOTE: 3 pre-existing `ListEditorPanel.test.ts` failures (stale selectors, unrelated) exist on this branch — expected; confirm no NEW failures.
- Per-task commits with explicit `git add <files>` (NEVER `-A`/`.` — untracked docs + pre-existing untracked `test-steam.ps1` / `.claude/skills/build-nomads/` must not be swept in). Do not push.

## File Structure

**Server:** `projectile_defs.go` (MODIFY: extract `validateProjectileDef`; overlay-aware readers), `projectile_persistence.go` (CREATE), `projectile_editor.go` (CREATE), `editor_handlers.go` (MODIFY), `cmd/api/main.go` (MODIFY).
**Client:** `game/projectiles/projectileEditorForm.ts` + `projectileEditorApi.ts` (CREATE), `components/ProjectileEditorPanel.vue` (CREATE), `views/ProjectileEditor.vue` (CREATE), `router/index.ts` (MODIFY), `components/world-editor/WorldEditorToolbar.vue` + test + `WorldEditorPanel.vue` (MODIFY).

---

## Task 1: Extract `validateProjectileDef` (validate + normalize)

**Files:**
- Modify: `server/internal/game/projectile_defs.go` (the `loadProjectileDefs` loop, ~lines 143-154)
- Test: `server/internal/game/projectile_defs_test.go` (create or append)

**Interfaces:**
- Produces: `func validateProjectileDef(def *ProjectileDef) error` — normalizes Kind/DurationMs/Speed in place; returns an error for an invalid Kind. Does NOT check id.

- [ ] **Step 1: Write the failing test**

Append to `server/internal/game/projectile_defs_test.go`:

```go
func TestValidateProjectileDef(t *testing.T) {
	t.Run("normalizes empty kind to projectile", func(t *testing.T) {
		def := ProjectileDef{ID: "x"}
		if err := validateProjectileDef(&def); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if def.Kind != EmitterKindProjectile {
			t.Fatalf("kind not normalized: %q", def.Kind)
		}
	})
	t.Run("rejects an invalid kind", func(t *testing.T) {
		if err := validateProjectileDef(&ProjectileDef{ID: "x", Kind: "laser"}); err == nil {
			t.Fatal("expected error for invalid kind")
		}
	})
	t.Run("defaults beam duration and projectile speed", func(t *testing.T) {
		beam := ProjectileDef{ID: "b", Kind: EmitterKindBeam}
		_ = validateProjectileDef(&beam)
		if beam.DurationMs != defaultBeamDurationMs {
			t.Fatalf("beam DurationMs=%d, want %d", beam.DurationMs, defaultBeamDurationMs)
		}
		proj := ProjectileDef{ID: "p"}
		_ = validateProjectileDef(&proj)
		if proj.Speed != defaultProjectileSpeed {
			t.Fatalf("Speed=%v, want %v", proj.Speed, defaultProjectileSpeed)
		}
	})
}
```

Ensure the test file has `package game` + `import "testing"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/game/ -run TestValidateProjectileDef`
Expected: FAIL — `validateProjectileDef` undefined.

- [ ] **Step 3: Add `validateProjectileDef` and call it from the loader**

In `server/internal/game/projectile_defs.go`, add the function just above `loadProjectileDefs`. Ensure `"fmt"` is imported (add it if the file doesn't already import it):

```go
// validateProjectileDef normalizes an emitter def's defaultable fields IN PLACE
// (Kind, beam DurationMs, Speed) and returns an error for an invalid Kind. It is
// the single gate shared by the catalog loader and the editor save path, so a
// def that loads cleanly is exactly a def that saves cleanly. It does NOT check
// the id (the loader gates that against the dir name, the editor against
// projectileIDPattern).
func validateProjectileDef(def *ProjectileDef) error {
	if def.Kind == "" {
		def.Kind = EmitterKindProjectile
	}
	if def.Kind != EmitterKindProjectile && def.Kind != EmitterKindBeam {
		return fmt.Errorf("invalid kind %q — must be \"projectile\" or \"beam\"", def.Kind)
	}
	if def.Kind == EmitterKindBeam && def.DurationMs <= 0 {
		def.DurationMs = defaultBeamDurationMs
	}
	if def.Speed <= 0 {
		def.Speed = defaultProjectileSpeed
	}
	return nil
}
```

Then REPLACE the inline normalize/validate block in `loadProjectileDefs` (the `Kind==""` normalize, the invalid-kind panic, the beam-duration default, and the speed default — the current ~lines 143-154) with a single call, keeping the id-empty / id≠dir / duplicate-id panics:

```go
		if def.ID == "" {
			panic(rel + `: missing "id" field`)
		}
		if def.ID != idKey {
			panic(rel + ": def.ID " + def.ID + " does not match directory name " + idKey)
		}
		if err := validateProjectileDef(&def); err != nil {
			panic(rel + ": " + err.Error())
		}
		if _, dup := result[def.ID]; dup {
			panic(rel + `: duplicate projectile id "` + def.ID + `"`)
		}
		result[def.ID] = def
```

- [ ] **Step 4: Run test + build**

Run: `cd server && go test ./internal/game/ -run TestValidateProjectileDef && go build ./...`
Expected: PASS + builds (every embedded projectile still loads).

- [ ] **Step 5: Commit**

```bash
git add server/internal/game/projectile_defs.go server/internal/game/projectile_defs_test.go
git commit -m "refactor(projectiles): extract validateProjectileDef shared by loader and save path"
```

---

## Task 2: Projectile overlay + overlay-aware readers + boot

**Files:**
- Create: `server/internal/game/projectile_persistence.go`
- Modify: `server/internal/game/projectile_defs.go` (`getProjectileDef`, `ListProjectileDefs`)
- Modify: `server/cmd/api/main.go`
- Test: `server/internal/game/projectile_persistence_test.go`

**Interfaces:**
- Consumes: `validateProjectileDef` (Task 1), `projectileDefsByID` (embed map).
- Produces: `SaveProjectileDef(*ProjectileDef) error`, `DeleteProjectileOverride(id) (bool, error)`, `ProjectileIsEmbedded(id) bool`, `LoadPersistedProjectilesIntoOverlay()`, `var projectileIDPattern`, overlay-aware readers.

- [ ] **Step 1: Write the failing test**

Create `server/internal/game/projectile_persistence_test.go`:

```go
package game

import "testing"

func TestSaveAndOverlayProjectileDef(t *testing.T) {
	t.Setenv("PROJECTILE_CATALOG_DIR", t.TempDir())
	// empty Kind + zero Speed should normalize on save
	if err := SaveProjectileDef(&ProjectileDef{ID: "test_bolt"}); err != nil {
		t.Fatalf("SaveProjectileDef: %v", err)
	}
	got, ok := getProjectileDef("test_bolt")
	if !ok || got.Kind != EmitterKindProjectile || got.Speed != defaultProjectileSpeed {
		t.Fatalf("normalize-on-save failed: ok=%v got=%+v", ok, got)
	}
	if ProjectileIsEmbedded("test_bolt") {
		t.Fatal("test_bolt should not be embedded")
	}
	existed, err := DeleteProjectileOverride("test_bolt")
	if err != nil || !existed {
		t.Fatalf("delete existed=%v err=%v", existed, err)
	}
	if _, ok := getProjectileDef("test_bolt"); ok {
		t.Fatal("def still resolvable after delete")
	}
}

func TestProjectileDiskRoundTripAndRevert(t *testing.T) {
	t.Setenv("PROJECTILE_CATALOG_DIR", t.TempDir())
	if err := SaveProjectileDef(&ProjectileDef{ID: "disk_bolt", Speed: 300, FollowEffect: "fizzle"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	runtimeProjectilesMu.Lock()
	delete(runtimeProjectiles, "disk_bolt")
	runtimeProjectilesMu.Unlock()
	if _, ok := getProjectileDef("disk_bolt"); ok {
		t.Fatal("expected miss after clearing overlay")
	}
	LoadPersistedProjectilesIntoOverlay()
	if got, ok := getProjectileDef("disk_bolt"); !ok || got.Speed != 300 || got.FollowEffect != "fizzle" {
		t.Fatalf("disk reload failed: ok=%v got=%+v", ok, got)
	}
	var embeddedID string
	for _, d := range ListProjectileDefs() {
		if ProjectileIsEmbedded(d.ID) {
			embeddedID = d.ID
			break
		}
	}
	if embeddedID == "" {
		t.Skip("no embedded projectiles to test revert")
	}
	original := projectileDefsByID[embeddedID]
	override := original
	override.Speed = original.Speed + 111
	if err := SaveProjectileDef(&override); err != nil {
		t.Fatalf("override save: %v", err)
	}
	if got, _ := getProjectileDef(embeddedID); got.Speed != original.Speed+111 {
		t.Fatal("overlay did not win over embed")
	}
	if _, err := DeleteProjectileOverride(embeddedID); err != nil {
		t.Fatalf("revert delete: %v", err)
	}
	if !ProjectileIsEmbedded(embeddedID) {
		t.Fatal("embedded id lost embedded status")
	}
	if got, _ := getProjectileDef(embeddedID); got.Speed != original.Speed {
		t.Fatalf("did not revert to embedded default: %+v", got)
	}
}

func TestSaveProjectileDefRejectsBadID(t *testing.T) {
	t.Setenv("PROJECTILE_CATALOG_DIR", t.TempDir())
	if err := SaveProjectileDef(&ProjectileDef{ID: "Bad/../x"}); err == nil {
		t.Fatal("expected id-pattern rejection")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/game/ -run Projectile`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Create `projectile_persistence.go`**

Mirror `server/internal/game/effect_persistence.go` exactly, substituting `Projectile`/`projectile`/`PROJECTILE_CATALOG_DIR`/`projectileDefsByID`. The full file:

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

var projectileIDPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

var (
	runtimeProjectilesMu sync.RWMutex
	runtimeProjectiles   = map[string]ProjectileDef{}
)

func resolveProjectilesDir() (string, error) {
	if dir := os.Getenv("PROJECTILE_CATALOG_DIR"); dir != "" {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cwd, "internal", "game", "catalog", "projectiles")
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	return "", fmt.Errorf("projectiles directory not found at %s; set PROJECTILE_CATALOG_DIR env var to override", dir)
}

// SaveProjectileDef validates+normalizes and writes an authored projectile def
// to <dir>/<id>/<id>.json, then registers it in the overlay.
func SaveProjectileDef(def *ProjectileDef) error {
	if !projectileIDPattern.MatchString(def.ID) {
		return fmt.Errorf("projectile id %q must match %s", def.ID, projectileIDPattern)
	}
	if err := validateProjectileDef(def); err != nil {
		return err
	}
	dir, err := resolveProjectilesDir()
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
	runtimeProjectilesMu.Lock()
	runtimeProjectiles[def.ID] = *def
	runtimeProjectilesMu.Unlock()
	return nil
}

func ProjectileIsEmbedded(id string) bool {
	_, ok := projectileDefsByID[id]
	return ok
}

func DeleteProjectileOverride(id string) (existed bool, err error) {
	if !projectileIDPattern.MatchString(id) {
		return false, nil
	}
	dir, derr := resolveProjectilesDir()
	if derr != nil {
		return false, derr
	}
	removed := false
	if rerr := os.Remove(filepath.Join(dir, id, id+".json")); rerr == nil {
		removed = true
		_ = os.Remove(filepath.Join(dir, id))
	}
	runtimeProjectilesMu.Lock()
	_, inOverlay := runtimeProjectiles[id]
	delete(runtimeProjectiles, id)
	runtimeProjectilesMu.Unlock()
	return removed || inOverlay, nil
}

func LoadPersistedProjectilesIntoOverlay() {
	dir, err := resolveProjectilesDir()
	if err != nil {
		slog.Info("persisted projectiles: no writable projectiles dir; using embedded catalog only", "err", err)
		return
	}
	if n := loadPersistedProjectilesFromDir(dir); n > 0 {
		slog.Info("persisted projectiles: overlaid on embedded catalog", "count", n, "dir", dir)
	}
}

func loadPersistedProjectilesFromDir(dir string) int {
	loaded := 0
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			return nil
		}
		def, perr := parsePersistedProjectileFile(path)
		if perr != nil {
			slog.Warn("persisted projectiles: skipped file", "file", d.Name(), "err", perr)
			return nil
		}
		runtimeProjectilesMu.Lock()
		runtimeProjectiles[def.ID] = *def
		runtimeProjectilesMu.Unlock()
		loaded++
		return nil
	})
	return loaded
}

func parsePersistedProjectileFile(path string) (*ProjectileDef, error) {
	raw, rerr := os.ReadFile(path)
	if rerr != nil {
		return nil, rerr
	}
	var d ProjectileDef
	if uerr := json.Unmarshal(raw, &d); uerr != nil {
		return nil, uerr
	}
	if d.ID == "" {
		return nil, fmt.Errorf("projectile has empty id")
	}
	if verr := validateProjectileDef(&d); verr != nil {
		return nil, verr
	}
	return &d, nil
}
```

- [ ] **Step 4: Make `getProjectileDef` and `ListProjectileDefs` overlay-aware**

In `server/internal/game/projectile_defs.go`, REPLACE the existing `getProjectileDef` and `ListProjectileDefs` with:

```go
// getProjectileDef looks up a projectile definition by id, overlay-first, then
// the embedded catalog.
func getProjectileDef(id string) (ProjectileDef, bool) {
	runtimeProjectilesMu.RLock()
	if def, ok := runtimeProjectiles[id]; ok {
		runtimeProjectilesMu.RUnlock()
		return def, true
	}
	runtimeProjectilesMu.RUnlock()
	def, ok := projectileDefsByID[id]
	return def, ok
}

// ListProjectileDefs returns every registered projectile definition (overlay
// merged over embed) sorted by id.
func ListProjectileDefs() []ProjectileDef {
	merged := make(map[string]ProjectileDef, len(projectileDefsByID))
	for id, def := range projectileDefsByID {
		merged[id] = def
	}
	runtimeProjectilesMu.RLock()
	for id, def := range runtimeProjectiles {
		merged[id] = def
	}
	runtimeProjectilesMu.RUnlock()
	defs := make([]ProjectileDef, 0, len(merged))
	for _, def := range merged {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}
```

(`sort` is already imported in `projectile_defs.go`.)

- [ ] **Step 5: Wire the startup loader**

In `server/cmd/api/main.go`, immediately after `game.LoadPersistedEffectsIntoOverlay()` (added by the Effects editor), add:

```go
	game.LoadPersistedProjectilesIntoOverlay()
```

- [ ] **Step 6: Run tests + build + vet**

Run: `cd server && go test ./internal/game/ -run Projectile && go build ./... && go vet ./...`
Expected: PASS + clean.

- [ ] **Step 7: Commit**

```bash
git add server/internal/game/projectile_persistence.go server/internal/game/projectile_defs.go server/internal/game/projectile_persistence_test.go server/cmd/api/main.go
git commit -m "feat(projectiles): writable PROJECTILE_CATALOG_DIR overlay + overlay-aware readers"
```

---

## Task 3: Editor wrapper + HTTP routes

**Files:**
- Create: `server/internal/game/projectile_editor.go`
- Modify: `server/internal/http/editor_handlers.go` (add `POST /projectiles` + `DELETE /projectiles/{id}` after the `/effects/` block)
- Test: `server/internal/game/projectile_editor_test.go` + `server/internal/http/editor_handlers_projectiles_test.go`

**Interfaces:**
- Consumes: `SaveProjectileDef`, `DeleteProjectileOverride`, `ProjectileIsEmbedded`, `projectileIDPattern`, `validateProjectileDef`, `editorValidationError`/`IsEditorValidationError`.
- Produces: `EditorProjectileSaveRequest{ Projectile ProjectileDef }`, `SaveEditorProjectile`, `DeleteEditorProjectile`.

- [ ] **Step 1: Write the failing tests**

Create `server/internal/game/projectile_editor_test.go`:

```go
package game

import "testing"

func TestSaveEditorProjectileValidation(t *testing.T) {
	t.Setenv("PROJECTILE_CATALOG_DIR", t.TempDir())
	err := SaveEditorProjectile(EditorProjectileSaveRequest{Projectile: ProjectileDef{ID: "bad", Kind: "laser"}})
	if err == nil || !IsEditorValidationError(err) {
		t.Fatalf("expected editor validation error, got %v", err)
	}
}

func TestSaveEditorProjectileOK(t *testing.T) {
	t.Setenv("PROJECTILE_CATALOG_DIR", t.TempDir())
	if err := SaveEditorProjectile(EditorProjectileSaveRequest{Projectile: ProjectileDef{ID: "ok_bolt", Speed: 200}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := getProjectileDef("ok_bolt"); !ok {
		t.Fatal("saved projectile not resolvable")
	}
}
```

Create `server/internal/http/editor_handlers_projectiles_test.go`:

```go
package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPostProjectilesValidationFails(t *testing.T) {
	t.Setenv("PROJECTILE_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerEditorRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/projectiles", strings.NewReader(`{"projectile":{"id":"x","kind":"laser"}}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "validation_failed") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPostProjectilesSavesThenDeletes(t *testing.T) {
	t.Setenv("PROJECTILE_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerEditorRoutes(mux)
	post := httptest.NewRequest(http.MethodPost, "/projectiles", strings.NewReader(`{"projectile":{"id":"post_bolt","speed":250}}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, post)
	if rec.Code != http.StatusCreated {
		t.Fatalf("save status=%d body=%s", rec.Code, rec.Body.String())
	}
	del := httptest.NewRequest(http.MethodDelete, "/projectiles/post_bolt", nil)
	drec := httptest.NewRecorder()
	mux.ServeHTTP(drec, del)
	if drec.Code != http.StatusOK || !strings.Contains(drec.Body.String(), "deleted") {
		t.Fatalf("delete status=%d body=%s", drec.Code, drec.Body.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd server && go test ./internal/game/ -run TestSaveEditorProjectile && go test ./internal/http/ -run TestPostProjectiles`
Expected: FAIL — undefined / no route.

- [ ] **Step 3: Create `projectile_editor.go`**

```go
package game

import "fmt"

// EditorProjectileSaveRequest is the body of POST /projectiles.
type EditorProjectileSaveRequest struct {
	Projectile ProjectileDef `json:"projectile"`
}

// SaveEditorProjectile validates then persists an authored projectile def.
// Validation failures are wrapped as editorValidationError so the handler
// returns 400.
func SaveEditorProjectile(req EditorProjectileSaveRequest) error {
	projectile := req.Projectile
	if !projectileIDPattern.MatchString(projectile.ID) {
		return editorValidationError{fmt.Errorf("projectile id %q must match %s", projectile.ID, projectileIDPattern)}
	}
	if err := validateProjectileDef(&projectile); err != nil {
		return editorValidationError{err}
	}
	return SaveProjectileDef(&projectile)
}

// DeleteEditorProjectile removes an override; embed-backed ids reset to default.
func DeleteEditorProjectile(id string) (existed bool, err error) {
	return DeleteProjectileOverride(id)
}
```

- [ ] **Step 4: Add the HTTP handlers**

In `server/internal/http/editor_handlers.go`, inside `registerEditorRoutes`, AFTER the `mux.HandleFunc("/effects/", ...)` block (added by the Effects editor), add:

```go
	mux.HandleFunc("/projectiles", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
			return
		}
		var req game.EditorProjectileSaveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		if err := game.SaveEditorProjectile(req); err != nil {
			if game.IsEditorValidationError(err) {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": req.Projectile.ID, "status": "saved"})
	})

	mux.HandleFunc("/projectiles/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/projectiles/")
		if r.Method != http.MethodDelete {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "DELETE only")
			return
		}
		if id == "" || strings.Contains(id, "/") {
			writeJSONError(w, http.StatusBadRequest, "invalid_id", "expected /projectiles/{id}")
			return
		}
		existed, err := game.DeleteEditorProjectile(id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error())
			return
		}
		if !existed {
			writeJSONError(w, http.StatusNotFound, "not_found", "no editor override for "+id)
			return
		}
		status := "deleted"
		if game.ProjectileIsEmbedded(id) {
			status = "reset"
		}
		writeJSON(w, map[string]string{"id": id, "status": status})
	})
```

- [ ] **Step 5: Run tests + build + vet**

Run: `cd server && go test ./internal/game/ -run TestSaveEditorProjectile && go test ./internal/http/ -run TestPostProjectiles && go build ./... && go vet ./...`
Expected: PASS + clean.

- [ ] **Step 6: Commit**

```bash
git add server/internal/game/projectile_editor.go server/internal/game/projectile_editor_test.go server/internal/http/editor_handlers.go server/internal/http/editor_handlers_projectiles_test.go
git commit -m "feat(projectiles): editor wrapper + POST /projectiles + DELETE /projectiles/{id}"
```

---

## Task 4: Client form + API modules

**Files:**
- Create: `client/src/game-portal/src/game/projectiles/projectileEditorForm.ts`
- Create: `client/src/game-portal/src/game/projectiles/projectileEditorApi.ts`
- Test: `client/src/game-portal/src/game/projectiles/projectileEditorForm.test.ts` + `projectileEditorApi.test.ts`

**Interfaces:**
- Produces:
  - `interface AuthoredProjectileDef { id: string; kind?: string; durationMs?: number; speed?: number; followEffect?: string; impactEffect?: string; [k: string]: any }`
  - `interface ProjectileEditorForm extends AuthoredProjectileDef { remainder: Record<string, unknown> }`
  - `createBlankForm()`, `formFromDef(def)`, `saveRequestFromForm(form)`
  - `class EditorValidationError extends Error { serverMessage: string }`
  - `fetchAuthoredProjectileDefs()`, `saveEditorProjectile(projectile)`, `deleteEditorProjectile(id)`

- [ ] **Step 1: Write the failing tests**

Create `client/src/game-portal/src/game/projectiles/projectileEditorForm.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { createBlankForm, formFromDef, saveRequestFromForm, type AuthoredProjectileDef } from './projectileEditorForm'

describe('projectileEditorForm', () => {
  it('createBlankForm has empty id + remainder', () => {
    const f = createBlankForm()
    expect(f.id).toBe('')
    expect(f.remainder).toEqual({})
  })
  it('round-trips modeled fields and preserves unmodeled keys', () => {
    const def = { id: 'bolt', kind: 'beam', durationMs: 300, speed: 0, followEffect: 'fizzle', futureKnob: 9 } as AuthoredProjectileDef
    const form = formFromDef(def)
    expect(form.kind).toBe('beam')
    expect(form.remainder).toEqual({ futureKnob: 9 })
    const out = saveRequestFromForm(form)
    expect(out.followEffect).toBe('fizzle')
    expect((out as Record<string, unknown>).futureKnob).toBe(9)
  })
  it('saveRequestFromForm drops undefined modeled fields', () => {
    const form = createBlankForm()
    form.id = 'x'
    const out = saveRequestFromForm(form)
    expect('speed' in out).toBe(false)
  })
})
```

Create `client/src/game-portal/src/game/projectiles/projectileEditorApi.test.ts`:

```ts
import { afterEach, describe, expect, it, vi } from 'vitest'
import { EditorValidationError, saveEditorProjectile, fetchAuthoredProjectileDefs } from './projectileEditorApi'

afterEach(() => vi.restoreAllMocks())

function mockFetch(status: number, body: unknown) {
  vi.stubGlobal('fetch', vi.fn(async () => ({
    ok: status >= 200 && status < 300, status, json: async () => body,
  })) as unknown as typeof fetch)
}

describe('projectileEditorApi', () => {
  it('throws EditorValidationError on 400 validation_failed', async () => {
    mockFetch(400, { error: 'validation_failed', message: 'bad kind' })
    await expect(saveEditorProjectile({ id: 'x' })).rejects.toBeInstanceOf(EditorValidationError)
  })
  it('fetchAuthoredProjectileDefs reads the projectiles array', async () => {
    mockFetch(200, { projectiles: [{ id: 'fire_bolt' }, { id: 'frost_bolt' }] })
    await expect(fetchAuthoredProjectileDefs()).resolves.toEqual([{ id: 'fire_bolt' }, { id: 'frost_bolt' }])
  })
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd client/src/game-portal && npx vitest run src/game/projectiles/`
Expected: FAIL — modules not found.

- [ ] **Step 3: Create `projectileEditorForm.ts`**

```ts
export interface AuthoredProjectileDef {
  id: string
  kind?: string
  durationMs?: number
  speed?: number
  followEffect?: string
  impactEffect?: string
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  [key: string]: any
}

const MODELED_KEYS = ['id', 'kind', 'durationMs', 'speed', 'followEffect', 'impactEffect'] as const

export interface ProjectileEditorForm extends AuthoredProjectileDef {
  remainder: Record<string, unknown>
}

export function createBlankForm(): ProjectileEditorForm {
  return { id: '', remainder: {} }
}

export function formFromDef(def: AuthoredProjectileDef): ProjectileEditorForm {
  const modeled: Record<string, unknown> = {}
  const remainder: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(def)) {
    if ((MODELED_KEYS as readonly string[]).includes(k)) modeled[k] = v
    else remainder[k] = v
  }
  return { ...(modeled as AuthoredProjectileDef), remainder }
}

export function saveRequestFromForm(form: ProjectileEditorForm): AuthoredProjectileDef {
  const { remainder, ...modeled } = form
  const out: Record<string, unknown> = { ...remainder }
  for (const [k, v] of Object.entries(modeled)) {
    if (v === undefined) continue
    out[k] = v
  }
  return out as AuthoredProjectileDef
}
```

- [ ] **Step 4: Create `projectileEditorApi.ts`**

```ts
import type { AuthoredProjectileDef } from './projectileEditorForm'

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

export class EditorValidationError extends Error {
  serverMessage: string
  constructor(message: string) {
    super(message)
    this.name = 'EditorValidationError'
    this.serverMessage = message
  }
}

export async function fetchAuthoredProjectileDefs(): Promise<AuthoredProjectileDef[]> {
  const res = await fetch(`${API_BASE}/catalog/projectiles`)
  if (!res.ok) throw new Error(`Failed to load projectile defs: ${res.status}`)
  const data = (await res.json()) as { projectiles: AuthoredProjectileDef[] }
  return data.projectiles ?? []
}

export async function saveEditorProjectile(projectile: AuthoredProjectileDef): Promise<void> {
  const res = await fetch(`${API_BASE}/projectiles`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ projectile }),
  })
  if (res.status === 400) {
    const body = (await res.json()) as { error?: string; message?: string }
    if (body.error === 'validation_failed') throw new EditorValidationError(body.message ?? 'validation failed')
  }
  if (!res.ok) throw new Error(`Failed to save projectile: ${res.status}`)
}

export async function deleteEditorProjectile(id: string): Promise<'deleted' | 'reset'> {
  const res = await fetch(`${API_BASE}/projectiles/${encodeURIComponent(id)}`, { method: 'DELETE' })
  if (!res.ok) throw new Error(`Failed to delete projectile: ${res.status}`)
  const body = (await res.json()) as { status: 'deleted' | 'reset' }
  return body.status
}
```

- [ ] **Step 5: Run tests + build**

Run: `cd client/src/game-portal && npx vitest run src/game/projectiles/ && npm run build`
Expected: PASS + build clean.

- [ ] **Step 6: Commit**

```bash
git add client/src/game-portal/src/game/projectiles/projectileEditorForm.ts client/src/game-portal/src/game/projectiles/projectileEditorApi.ts client/src/game-portal/src/game/projectiles/projectileEditorForm.test.ts client/src/game-portal/src/game/projectiles/projectileEditorApi.test.ts
git commit -m "feat(projectiles): client form + api modules"
```

---

## Task 5: Editor panel + view + route

**Files:**
- Create: `client/src/game-portal/src/components/ProjectileEditorPanel.vue`
- Create: `client/src/game-portal/src/views/ProjectileEditor.vue`
- Modify: `client/src/game-portal/src/router/index.ts`
- Test: `client/src/game-portal/src/components/ProjectileEditorPanel.test.ts`
- Reference (read first): `client/src/game-portal/src/components/EffectEditorPanel.vue` (the just-shipped sibling — same shell, closest reference) and `views/EffectEditor.vue`.

**Interfaces:**
- Consumes: everything from `projectileEditorForm.ts` + `projectileEditorApi.ts`; plus `fetchEffectIds` from `@/game/abilities/abilityEditorApi` for the effect dropdowns.

**Panel structure (build to this):**
- On mount: `fetchAuthoredProjectileDefs()` into a `ref` (left list), and `fetchEffectIds()` into a `ref` (effect dropdown options).
- Left column: list of projectile defs (id) + "New" (`createBlankForm()`); clicking a def → `formFromDef(def)` + record selected id. `data-test="projectile-row"` on the rows.
- Right column form (6 fields):
  - `id` — text, `:disabled="selectedId !== null"`.
  - `kind` — `<select v-model="form.kind">` over `['projectile', 'beam']`.
  - `durationMs` — number (`v-model.number`).
  - `speed` — number (`v-model.number`).
  - `followEffect` — `<select v-model="form.followEffect">` with a blank "(none)" option + one `<option>` per fetched effect id.
  - `impactEffect` — same as followEffect.
- Save → `saveRequestFromForm(form)` → `saveEditorProjectile(...)`; on `EditorValidationError` show `err.serverMessage` inline; success → refresh list + saved indicator.
- Delete/Reset → `deleteEditorProjectile(selectedId)`; word from returned status.
- No literal `cursor:`.

- [ ] **Step 1: Write the failing test**

Create `client/src/game-portal/src/components/ProjectileEditorPanel.test.ts`:

```ts
import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ProjectileEditorPanel from './ProjectileEditorPanel.vue'

function stubFetch() {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (String(url).endsWith('/catalog/projectiles')) {
      return { ok: true, status: 200, json: async () => ({ projectiles: [{ id: 'fire_bolt', kind: 'projectile', speed: 300 }] }) }
    }
    if (String(url).endsWith('/catalog/effects')) {
      return { ok: true, status: 200, json: async () => ({ effects: [{ id: 'fizzle' }] }) }
    }
    return { ok: true, status: 200, json: async () => ({}) }
  }) as unknown as typeof fetch)
}

afterEach(() => vi.restoreAllMocks())

describe('ProjectileEditorPanel', () => {
  it('mounts and lists projectiles from the catalog', async () => {
    stubFetch()
    const wrapper = mount(ProjectileEditorPanel)
    await flushPromises()
    expect(wrapper.text()).toContain('fire_bolt')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client/src/game-portal && npx vitest run src/components/ProjectileEditorPanel.test.ts`
Expected: FAIL — component not found.

- [ ] **Step 3: Read the reference, then build the panel**

Read `EffectEditorPanel.vue` in full; mirror its list/form/save/delete shell. Build `ProjectileEditorPanel.vue` per "Panel structure" above (6 fields; `kind` select `['projectile','beam']`; `followEffect`/`impactEffect` selects fed by `fetchEffectIds()` with a blank "(none)" option; `data-test="projectile-row"`; no literal `cursor:`).

- [ ] **Step 4: Create the view + route**

`client/src/game-portal/src/views/ProjectileEditor.vue` (mirror `EffectEditor.vue`, swapping identifiers to `projectile-editor-view` + `ProjectileEditorPanel`).

In `client/src/game-portal/src/router/index.ts`, add the import next to the other editor views:

```ts
import ProjectileEditor from '@/views/ProjectileEditor.vue'
```

and the route next to `/effect-editor`:

```ts
    { path: '/projectile-editor', component: ProjectileEditor, meta: { hideDominionPanel: true } },
```

- [ ] **Step 5: Run test + full client build**

Run: `cd client/src/game-portal && npx vitest run src/components/ProjectileEditorPanel.test.ts && npm run build`
Expected: PASS + build clean.

- [ ] **Step 6: Commit**

```bash
git add client/src/game-portal/src/components/ProjectileEditorPanel.vue client/src/game-portal/src/components/ProjectileEditorPanel.test.ts client/src/game-portal/src/views/ProjectileEditor.vue client/src/game-portal/src/router/index.ts
git commit -m "feat(projectiles): editor panel + /projectile-editor route"
```

---

## Task 6: World-editor toolbar + screen wiring (current pattern)

**Files:**
- Modify: `client/src/game-portal/src/components/world-editor/WorldEditorToolbar.vue`
- Modify: `client/src/game-portal/src/components/world-editor/worldEditorToolbar.test.ts`
- Modify: `client/src/game-portal/src/components/world-editor/WorldEditorPanel.vue`

**Interfaces:**
- Consumes: `ProjectileEditorPanel` (Task 5).

- [ ] **Step 1: Update the toolbar test expectation**

In `worldEditorToolbar.test.ts`, change the `projectiles` assertion to `expect(byId.projectiles.enabled).toBe(true)` (leave `unit-paths`/`perks`/`campaigns` disabled).

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client/src/game-portal && npx vitest run src/components/world-editor/worldEditorToolbar.test.ts`
Expected: FAIL — `projectiles` still disabled.

- [ ] **Step 3: Flip the toolbar entry**

In `WorldEditorToolbar.vue`:

```ts
  { id: 'projectiles', label: 'Projectiles', enabled: true },
```

- [ ] **Step 4: Run toolbar test to verify it passes**

Run: `cd client/src/game-portal && npx vitest run src/components/world-editor/worldEditorToolbar.test.ts`
Expected: PASS

- [ ] **Step 5: Wire the screen in `WorldEditorPanel.vue` (CURRENT screen-switch pattern)**

Four edits, mirroring the `abilities`/`effects` screen wiring (Grep for `EffectEditorPanel` / `activeScreen` / `EditorScreen` to find each):

(a) Import next to `EffectEditorPanel`:

```ts
import ProjectileEditorPanel from '@/components/ProjectileEditorPanel.vue'
```

(b) Add `'projectiles'` to the `EditorScreen` union type:

```ts
type EditorScreen = 'map' | 'items' | 'unit-types' | 'abilities' | 'effects' | 'projectiles'
```

(c) In `onToolbarSelect`, add `case 'projectiles':` to the shared fall-through arm (the one ending `activeScreen.value = id; break`), and drop `projectiles` from the `default:` "coming soon" comment:

```ts
    case 'items':
    case 'unit-types':
    case 'abilities':
    case 'effects':
    case 'projectiles':
      activeScreen.value = id
      break
```

(d) In the `<section v-if="activeScreen !== 'map'" class="we-screen">` chain, add after the effects line:

```vue
      <ProjectileEditorPanel v-else-if="activeScreen === 'projectiles'" />
```

`toolbarActiveId` already generalizes — do NOT change it.

- [ ] **Step 6: Full client gates**

Run: `cd client/src/game-portal && npm run build && npm run test`
Expected: build clean; suite green EXCEPT the 3 known pre-existing `ListEditorPanel.test.ts` failures (confirm no NEW failures).

- [ ] **Step 7: Commit**

```bash
git add client/src/game-portal/src/components/world-editor/WorldEditorToolbar.vue client/src/game-portal/src/components/world-editor/worldEditorToolbar.test.ts client/src/game-portal/src/components/world-editor/WorldEditorPanel.vue
git commit -m "feat(projectiles): enable Projectiles toolbar category + world-editor screen"
```

---

## Task 7: Final verification

**Files:** none (verification only).

- [ ] **Step 1: Full server suite**

Run: `cd server && go build ./... && go vet ./... && go test ./...`
Expected: builds, vet clean, all tests pass (known repo-wide pre-existing `internal/ws` / `cmd/api` failures excepted; `internal/game` + `internal/http` MUST pass).

- [ ] **Step 2: Full client suite**

Run: `cd client/src/game-portal && npm run build && npm run test`
Expected: build clean; only the 3 known pre-existing `ListEditorPanel.test.ts` failures (no new ones).

- [ ] **Step 3: Manual E2E (hard gate — requires a running server)**

1. World editor → **Projectiles** → **New** → author a projectile (id `test_bolt`, kind `projectile`, speed `300`, followEffect `fizzle`) → Save → confirm it lists.
2. Open **Abilities** editor → the projectile dropdown → confirm `test_bolt` appears.
3. In the projectile editor's followEffect dropdown, confirm a newly-authored effect (from the Effects editor) appears as a choice.
4. Edit an embedded projectile's speed → Save → **Delete/Reset** → confirm it reverts to the shipped default.

- [ ] **Step 4: Confirm clean tree**

Run: `git status` and `git log --oneline` for the task commits.

---

## Self-Review Notes (for the executor)

- **Spec coverage:** §1 overlay + readers → Task 2; §2 `validateProjectileDef` (validate+normalize) → Task 1; §3 editor+HTTP → Task 3; §4 client triad → Tasks 4-5; §5 wiring (current screen-switch pattern) → Tasks 5-6; testing → per-task + Task 7.
- **Type consistency:** `AuthoredProjectileDef`/`ProjectileEditorForm`/`MODELED_KEYS` defined once (Task 4), consumed by Task 5. Server `EditorProjectileSaveRequest.Projectile` (Task 3) matches client POST body `{ projectile }` (Task 4). `/catalog/projectiles` key `projectiles` (server) matches the fetcher (Task 4). The effect dropdowns reuse `fetchEffectIds` (Task 5) which reads `/catalog/effects` `{effects}` (existing).
- **Watch items:** (1) Task 1 — add `"fmt"` to `projectile_defs.go` imports if missing. (2) Task 6 uses the CURRENT screen-switch pattern — do NOT look for the retired `abilitiesPopupOpen`/modal pattern. (3) `validateProjectileDef` NORMALIZES (mutates) — the round-trip test asserts normalization survives save.
