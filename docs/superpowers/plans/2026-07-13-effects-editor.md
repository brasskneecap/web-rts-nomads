# Effects Editor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a create/edit/delete editor for `EffectDef`s (visual effects) to the world editor, via a writable `EFFECT_CATALOG_DIR` overlay mirroring the shipped Abilities editor.

**Architecture:** Server grows an `effect_persistence.go` overlay + overlay-aware readers + a `validateEffectDef` gate + an `effect_editor.go` wrapper + `POST /effects` / `DELETE /effects/{id}` routes. Client grows the proven triad — `game/effects/effectEditorForm.ts` (pure), `effectEditorApi.ts` (fetch), `components/EffectEditorPanel.vue` — surfaced as a `/effect-editor` route and a world-editor toolbar popup. `GET /catalog/effects` already exists (becomes overlay-merged).

**Tech Stack:** Go (`internal/game`, `internal/http`), TypeScript / Vue 3 (`game-portal`), Vitest, `go test`.

## Global Constraints

- Branch: `reference-def-editors` (off `main`, base `d4fbff8`) — shared with the later Projectiles editor + Perks wiring.
- `EffectDef` (4 fields): `ID string json:"id"`, `SpritePath string json:"spritePath,omitempty"`, `Duration float64 json:"duration"`, `Anchor EffectAnchor json:"anchor,omitempty"`. `EffectAnchor` is a closed enum `"center" | "feet" | "head"` (empty ⇒ center), validated by `isValidEffectAnchor` (`effect_defs.go:50-57`).
- Effect defs are **presentation-only** (drive client animation, not sim) — no determinism concern; no `game`→`profile` write.
- Effect **art** (sprite sheets) is OUT of scope — the editor edits the def only; new effects reference existing bundled art `assets/effects/<id>/` by id.
- `effectIDPattern = ^[a-z0-9_]+$` is the path-traversal gate on every id entry point (save, delete).
- Catalog layout is per-id subfolder `catalog/effects/<id>/<id>.json`.
- The `anchor` field is a hardcoded closed-enum dropdown in the panel (`['', 'center', 'feet', 'head']`) — NOT a `/catalog/*` endpoint.
- No literal `cursor:` in new/changed component CSS except `cursor: not-allowed` on forbidden states.
- Build gates: server `go build ./...` + `go vet ./...` + `go test ./...` (NOT gofmt — CRLF repo). Client `npm run build` (`vue-tsc -b`) + `npm run test`, run from `client/src/game-portal`.
- Per-task commits with explicit `git add <files>` (NEVER `-A`/`.` — untracked docs and pre-existing untracked items like `test-steam.ps1` / `.claude/skills/build-nomads/` must not be swept in). Do not push.

## File Structure

**Server:**
- `server/internal/game/effect_defs.go` — MODIFY: extract `validateEffectDef`; make `getEffectDef`/`ListEffectDefs` overlay-aware.
- `server/internal/game/effect_persistence.go` — CREATE: the overlay.
- `server/internal/game/effect_editor.go` — CREATE: editor save/delete wrappers.
- `server/internal/http/editor_handlers.go` — MODIFY: `POST /effects` + `DELETE /effects/{id}`.
- `server/cmd/api/main.go` — MODIFY: `LoadPersistedEffectsIntoOverlay()` boot call.

**Client (`client/src/game-portal/src`):**
- `game/effects/effectEditorForm.ts` — CREATE.
- `game/effects/effectEditorApi.ts` — CREATE.
- `components/EffectEditorPanel.vue` — CREATE.
- `views/EffectEditor.vue` — CREATE.
- `router/index.ts` — MODIFY: `/effect-editor` route.
- `components/world-editor/WorldEditorToolbar.vue` (+ its test) — MODIFY: flip `effects` enabled.
- `components/world-editor/WorldEditorPanel.vue` — MODIFY: Effects popup.

---

## Task 1: Extract `validateEffectDef`

**Files:**
- Modify: `server/internal/game/effect_defs.go` (the `loadEffectDefs` loop, ~lines 130-136)
- Test: `server/internal/game/effect_defs_test.go` (create or append)

**Interfaces:**
- Produces: `func validateEffectDef(def *EffectDef) error` — `Duration < 0` → error; `Anchor != "" && !isValidEffectAnchor(Anchor)` → error. Does NOT check id (the loader gates id against the dir name; the editor against `effectIDPattern`).

- [ ] **Step 1: Write the failing test**

Append to `server/internal/game/effect_defs_test.go`:

```go
func TestValidateEffectDef(t *testing.T) {
	t.Run("rejects negative duration", func(t *testing.T) {
		if err := validateEffectDef(&EffectDef{ID: "x", Duration: -1}); err == nil {
			t.Fatal("expected error for negative duration")
		}
	})
	t.Run("rejects unknown anchor", func(t *testing.T) {
		if err := validateEffectDef(&EffectDef{ID: "x", Anchor: "sideways"}); err == nil {
			t.Fatal("expected error for unknown anchor")
		}
	})
	t.Run("accepts empty anchor and zero duration", func(t *testing.T) {
		if err := validateEffectDef(&EffectDef{ID: "x"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("accepts a known anchor", func(t *testing.T) {
		if err := validateEffectDef(&EffectDef{ID: "x", Anchor: "feet", Duration: 0.5}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
```

Ensure the test file has `package game` and imports `"testing"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/game/ -run TestValidateEffectDef`
Expected: FAIL — `validateEffectDef` undefined.

- [ ] **Step 3: Add `validateEffectDef` and call it from the loader**

In `server/internal/game/effect_defs.go`, add the function just above `loadEffectDefs`. Ensure `"fmt"` is imported (add it to the import block if the file doesn't already import it):

```go
// validateEffectDef checks an effect def's content. It is the single validation
// gate shared by the catalog loader and the editor save path, so a def that
// loads cleanly is exactly a def that saves cleanly. It does NOT check the id —
// the loader gates that against the directory name, the editor against
// effectIDPattern.
func validateEffectDef(def *EffectDef) error {
	if def.Duration < 0 {
		return fmt.Errorf("duration must be >= 0")
	}
	if def.Anchor != "" && !isValidEffectAnchor(def.Anchor) {
		return fmt.Errorf("anchor %q must be one of \"center\" | \"feet\" | \"head\"", def.Anchor)
	}
	return nil
}
```

Then REPLACE the two inline checks in `loadEffectDefs` (the `if def.Duration < 0 { panic(...) }` and `if def.Anchor != "" && !isValidEffectAnchor(...) { panic(...) }` blocks) with a single call, keeping the id-empty / id≠dir / duplicate-id panics:

```go
		if def.ID == "" {
			panic(rel + `: missing "id" field`)
		}
		if def.ID != idKey {
			panic(rel + ": def.ID " + def.ID + " does not match directory name " + idKey)
		}
		if err := validateEffectDef(&def); err != nil {
			panic(rel + ": " + err.Error())
		}
		if _, dup := result[def.ID]; dup {
			panic(rel + `: duplicate effect id "` + def.ID + `"`)
		}
		result[def.ID] = def
```

- [ ] **Step 4: Run test + build**

Run: `cd server && go test ./internal/game/ -run TestValidateEffectDef && go build ./...`
Expected: PASS + builds (confirms every embedded effect still loads).

- [ ] **Step 5: Commit**

```bash
git add server/internal/game/effect_defs.go server/internal/game/effect_defs_test.go
git commit -m "refactor(effects): extract validateEffectDef shared by loader and save path"
```

---

## Task 2: Effect writable overlay (`effect_persistence.go`) + overlay-aware readers + boot

**Files:**
- Create: `server/internal/game/effect_persistence.go`
- Modify: `server/internal/game/effect_defs.go` (`getEffectDef`, `ListEffectDefs`)
- Modify: `server/cmd/api/main.go` (add the boot call)
- Test: `server/internal/game/effect_persistence_test.go`

**Interfaces:**
- Consumes: `validateEffectDef` (Task 1), `CastRange`-style nothing; `effectDefsByID` (embed map).
- Produces: `SaveEffectDef(*EffectDef) error`, `DeleteEffectOverride(id string) (bool, error)`, `EffectIsEmbedded(id string) bool`, `LoadPersistedEffectsIntoOverlay()`, `var effectIDPattern`, overlay-aware `getEffectDef`/`ListEffectDefs`.

- [ ] **Step 1: Write the failing test**

Create `server/internal/game/effect_persistence_test.go`:

```go
package game

import "testing"

func TestSaveAndOverlayEffectDef(t *testing.T) {
	t.Setenv("EFFECT_CATALOG_DIR", t.TempDir())
	def := &EffectDef{ID: "test_glow", Duration: 0.75, Anchor: "head"}
	if err := SaveEffectDef(def); err != nil {
		t.Fatalf("SaveEffectDef: %v", err)
	}
	got, ok := getEffectDef("test_glow")
	if !ok || got.Duration != 0.75 || got.Anchor != "head" {
		t.Fatalf("overlay def not resolved: ok=%v got=%+v", ok, got)
	}
	if EffectIsEmbedded("test_glow") {
		t.Fatal("test_glow should not be embedded")
	}
	existed, err := DeleteEffectOverride("test_glow")
	if err != nil || !existed {
		t.Fatalf("delete existed=%v err=%v", existed, err)
	}
	if _, ok := getEffectDef("test_glow"); ok {
		t.Fatal("def still resolvable after delete")
	}
}

func TestEffectDiskRoundTripAndRevert(t *testing.T) {
	t.Setenv("EFFECT_CATALOG_DIR", t.TempDir())
	// disk round-trip: save, clear overlay, reload from disk
	if err := SaveEffectDef(&EffectDef{ID: "disk_fx", Duration: 1.5, Anchor: "feet"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	runtimeEffectsMu.Lock()
	delete(runtimeEffects, "disk_fx")
	runtimeEffectsMu.Unlock()
	if _, ok := getEffectDef("disk_fx"); ok {
		t.Fatal("expected miss after clearing overlay")
	}
	LoadPersistedEffectsIntoOverlay()
	if got, ok := getEffectDef("disk_fx"); !ok || got.Duration != 1.5 || got.Anchor != "feet" {
		t.Fatalf("disk reload failed: ok=%v got=%+v", ok, got)
	}
	// embed-revert: override a real embedded effect, then delete reverts
	var embeddedID string
	for _, d := range ListEffectDefs() {
		if EffectIsEmbedded(d.ID) {
			embeddedID = d.ID
			break
		}
	}
	if embeddedID == "" {
		t.Skip("no embedded effects to test revert")
	}
	original := effectDefsByID[embeddedID]
	override := original
	override.Duration = original.Duration + 5
	if err := SaveEffectDef(&override); err != nil {
		t.Fatalf("override save: %v", err)
	}
	if got, _ := getEffectDef(embeddedID); got.Duration != original.Duration+5 {
		t.Fatal("overlay did not win over embed")
	}
	if _, err := DeleteEffectOverride(embeddedID); err != nil {
		t.Fatalf("revert delete: %v", err)
	}
	if !EffectIsEmbedded(embeddedID) {
		t.Fatal("embedded id lost embedded status")
	}
	if got, _ := getEffectDef(embeddedID); got.Duration != original.Duration {
		t.Fatalf("did not revert to embedded default: %+v", got)
	}
}

func TestSaveEffectDefRejectsBadID(t *testing.T) {
	t.Setenv("EFFECT_CATALOG_DIR", t.TempDir())
	if err := SaveEffectDef(&EffectDef{ID: "Bad/../x"}); err == nil {
		t.Fatal("expected id-pattern rejection")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/game/ -run 'Effect' `
Expected: FAIL — `SaveEffectDef`/`EffectIsEmbedded`/`DeleteEffectOverride`/`runtimeEffects` undefined.

- [ ] **Step 3: Create `effect_persistence.go`**

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

var effectIDPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

var (
	runtimeEffectsMu sync.RWMutex
	runtimeEffects   = map[string]EffectDef{}
)

// resolveEffectsDir returns the writable effects catalog dir: EFFECT_CATALOG_DIR
// if set, else the dev source tree.
func resolveEffectsDir() (string, error) {
	if dir := os.Getenv("EFFECT_CATALOG_DIR"); dir != "" {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cwd, "internal", "game", "catalog", "effects")
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	return "", fmt.Errorf("effects directory not found at %s; set EFFECT_CATALOG_DIR env var to override", dir)
}

// SaveEffectDef validates and writes an authored effect def to
// <dir>/<id>/<id>.json, then registers it in the overlay.
func SaveEffectDef(def *EffectDef) error {
	if !effectIDPattern.MatchString(def.ID) {
		return fmt.Errorf("effect id %q must match %s", def.ID, effectIDPattern)
	}
	if err := validateEffectDef(def); err != nil {
		return err
	}
	dir, err := resolveEffectsDir()
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
	runtimeEffectsMu.Lock()
	runtimeEffects[def.ID] = *def
	runtimeEffectsMu.Unlock()
	return nil
}

// EffectIsEmbedded reports whether an effect id ships in the embedded catalog.
func EffectIsEmbedded(id string) bool {
	_, ok := effectDefsByID[id]
	return ok
}

// DeleteEffectOverride removes the override file + overlay entry for an id.
func DeleteEffectOverride(id string) (existed bool, err error) {
	if !effectIDPattern.MatchString(id) {
		return false, nil // never a valid override id; also blocks path traversal
	}
	dir, derr := resolveEffectsDir()
	if derr != nil {
		return false, derr
	}
	removed := false
	if rerr := os.Remove(filepath.Join(dir, id, id+".json")); rerr == nil {
		removed = true
		_ = os.Remove(filepath.Join(dir, id)) // best-effort: drop the now-empty dir
	}
	runtimeEffectsMu.Lock()
	_, inOverlay := runtimeEffects[id]
	delete(runtimeEffects, id)
	runtimeEffectsMu.Unlock()
	return removed || inOverlay, nil
}

// LoadPersistedEffectsIntoOverlay overlays writable effect defs onto the embed
// at startup. Best-effort; a bad file is skipped, never fatal.
func LoadPersistedEffectsIntoOverlay() {
	dir, err := resolveEffectsDir()
	if err != nil {
		slog.Info("persisted effects: no writable effects dir; using embedded catalog only", "err", err)
		return
	}
	if n := loadPersistedEffectsFromDir(dir); n > 0 {
		slog.Info("persisted effects: overlaid on embedded catalog", "count", n, "dir", dir)
	}
}

func loadPersistedEffectsFromDir(dir string) int {
	loaded := 0
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			return nil
		}
		def, perr := parsePersistedEffectFile(path)
		if perr != nil {
			slog.Warn("persisted effects: skipped file", "file", d.Name(), "err", perr)
			return nil
		}
		runtimeEffectsMu.Lock()
		runtimeEffects[def.ID] = *def
		runtimeEffectsMu.Unlock()
		loaded++
		return nil
	})
	return loaded
}

func parsePersistedEffectFile(path string) (*EffectDef, error) {
	raw, rerr := os.ReadFile(path)
	if rerr != nil {
		return nil, rerr
	}
	var d EffectDef
	if uerr := json.Unmarshal(raw, &d); uerr != nil {
		return nil, uerr
	}
	if d.ID == "" {
		return nil, fmt.Errorf("effect has empty id")
	}
	if verr := validateEffectDef(&d); verr != nil {
		return nil, verr
	}
	return &d, nil
}
```

- [ ] **Step 4: Make `getEffectDef` and `ListEffectDefs` overlay-aware**

In `server/internal/game/effect_defs.go`, REPLACE the existing `getEffectDef` and `ListEffectDefs` with:

```go
// getEffectDef looks up an effect definition by id, overlay-first (an authored
// override wins over the embedded default), then the embedded catalog.
func getEffectDef(id string) (EffectDef, bool) {
	runtimeEffectsMu.RLock()
	if def, ok := runtimeEffects[id]; ok {
		runtimeEffectsMu.RUnlock()
		return def, true
	}
	runtimeEffectsMu.RUnlock()
	def, ok := effectDefsByID[id]
	return def, ok
}

// ListEffectDefs returns every registered effect definition (overlay merged
// over embed) sorted by id.
func ListEffectDefs() []EffectDef {
	merged := make(map[string]EffectDef, len(effectDefsByID))
	for id, def := range effectDefsByID {
		merged[id] = def
	}
	runtimeEffectsMu.RLock()
	for id, def := range runtimeEffects {
		merged[id] = def
	}
	runtimeEffectsMu.RUnlock()
	defs := make([]EffectDef, 0, len(merged))
	for _, def := range merged {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}
```

(`sort` is already imported in `effect_defs.go` — the old `ListEffectDefs` used it.)

- [ ] **Step 5: Wire the startup loader**

In `server/cmd/api/main.go`, immediately after `game.LoadPersistedAbilitiesIntoOverlay()` (line 60), add:

```go
	game.LoadPersistedEffectsIntoOverlay()
```

- [ ] **Step 6: Run tests + build + vet**

Run: `cd server && go test ./internal/game/ -run Effect && go build ./... && go vet ./...`
Expected: PASS + clean.

- [ ] **Step 7: Commit**

```bash
git add server/internal/game/effect_persistence.go server/internal/game/effect_defs.go server/internal/game/effect_persistence_test.go server/cmd/api/main.go
git commit -m "feat(effects): writable EFFECT_CATALOG_DIR overlay + overlay-aware readers"
```

---

## Task 3: Editor wrapper + HTTP routes

**Files:**
- Create: `server/internal/game/effect_editor.go`
- Modify: `server/internal/http/editor_handlers.go` (add `POST /effects` + `DELETE /effects/{id}` after the `/abilities/` block)
- Test: `server/internal/game/effect_editor_test.go` + `server/internal/http/editor_handlers_effects_test.go`

**Interfaces:**
- Consumes: `SaveEffectDef`, `DeleteEffectOverride`, `EffectIsEmbedded`, `effectIDPattern`, `validateEffectDef`, `editorValidationError`/`IsEditorValidationError`.
- Produces: `EditorEffectSaveRequest{ Effect EffectDef }`, `SaveEditorEffect`, `DeleteEditorEffect`.

- [ ] **Step 1: Write the failing tests**

Create `server/internal/game/effect_editor_test.go`:

```go
package game

import "testing"

func TestSaveEditorEffectValidation(t *testing.T) {
	t.Setenv("EFFECT_CATALOG_DIR", t.TempDir())
	err := SaveEditorEffect(EditorEffectSaveRequest{Effect: EffectDef{ID: "bad", Duration: -1}})
	if err == nil || !IsEditorValidationError(err) {
		t.Fatalf("expected editor validation error, got %v", err)
	}
}

func TestSaveEditorEffectOK(t *testing.T) {
	t.Setenv("EFFECT_CATALOG_DIR", t.TempDir())
	if err := SaveEditorEffect(EditorEffectSaveRequest{Effect: EffectDef{ID: "ok_fx", Duration: 1}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := getEffectDef("ok_fx"); !ok {
		t.Fatal("saved effect not resolvable")
	}
}
```

Create `server/internal/http/editor_handlers_effects_test.go`:

```go
package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPostEffectsValidationFails(t *testing.T) {
	t.Setenv("EFFECT_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerEditorRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/effects", strings.NewReader(`{"effect":{"id":"x","duration":-1}}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "validation_failed") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPostEffectsSavesThenDeletes(t *testing.T) {
	t.Setenv("EFFECT_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerEditorRoutes(mux)
	post := httptest.NewRequest(http.MethodPost, "/effects", strings.NewReader(`{"effect":{"id":"post_fx","duration":1}}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, post)
	if rec.Code != http.StatusCreated {
		t.Fatalf("save status=%d body=%s", rec.Code, rec.Body.String())
	}
	del := httptest.NewRequest(http.MethodDelete, "/effects/post_fx", nil)
	drec := httptest.NewRecorder()
	mux.ServeHTTP(drec, del)
	if drec.Code != http.StatusOK || !strings.Contains(drec.Body.String(), "deleted") {
		t.Fatalf("delete status=%d body=%s", drec.Code, drec.Body.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd server && go test ./internal/game/ -run TestSaveEditorEffect && go test ./internal/http/ -run TestPostEffects`
Expected: FAIL — undefined symbols / no `/effects` route.

- [ ] **Step 3: Create `effect_editor.go`**

```go
package game

import "fmt"

// EditorEffectSaveRequest is the body of POST /effects.
type EditorEffectSaveRequest struct {
	Effect EffectDef `json:"effect"`
}

// SaveEditorEffect validates then persists an authored effect def. Validation
// failures are wrapped as editorValidationError so the handler returns 400.
func SaveEditorEffect(req EditorEffectSaveRequest) error {
	effect := req.Effect
	if !effectIDPattern.MatchString(effect.ID) {
		return editorValidationError{fmt.Errorf("effect id %q must match %s", effect.ID, effectIDPattern)}
	}
	if err := validateEffectDef(&effect); err != nil {
		return editorValidationError{err}
	}
	return SaveEffectDef(&effect)
}

// DeleteEditorEffect removes an override; embed-backed ids reset to default.
func DeleteEditorEffect(id string) (existed bool, err error) {
	return DeleteEffectOverride(id)
}
```

- [ ] **Step 4: Add the HTTP handlers**

In `server/internal/http/editor_handlers.go`, inside `registerEditorRoutes`, AFTER the `mux.HandleFunc("/abilities/", ...)` block, add (mirror of the `/abilities` + `/abilities/` pair):

```go
	mux.HandleFunc("/effects", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
			return
		}
		var req game.EditorEffectSaveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		if err := game.SaveEditorEffect(req); err != nil {
			if game.IsEditorValidationError(err) {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": req.Effect.ID, "status": "saved"})
	})

	mux.HandleFunc("/effects/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/effects/")
		if r.Method != http.MethodDelete {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "DELETE only")
			return
		}
		if id == "" || strings.Contains(id, "/") {
			writeJSONError(w, http.StatusBadRequest, "invalid_id", "expected /effects/{id}")
			return
		}
		existed, err := game.DeleteEditorEffect(id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error())
			return
		}
		if !existed {
			writeJSONError(w, http.StatusNotFound, "not_found", "no editor override for "+id)
			return
		}
		status := "deleted"
		if game.EffectIsEmbedded(id) {
			status = "reset"
		}
		writeJSON(w, map[string]string{"id": id, "status": status})
	})
```

- [ ] **Step 5: Run tests + build + vet**

Run: `cd server && go test ./internal/game/ -run TestSaveEditorEffect && go test ./internal/http/ -run TestPostEffects && go build ./... && go vet ./...`
Expected: PASS + clean.

- [ ] **Step 6: Commit**

```bash
git add server/internal/game/effect_editor.go server/internal/game/effect_editor_test.go server/internal/http/editor_handlers.go server/internal/http/editor_handlers_effects_test.go
git commit -m "feat(effects): editor wrapper + POST /effects + DELETE /effects/{id}"
```

---

## Task 4: Client form + API modules

**Files:**
- Create: `client/src/game-portal/src/game/effects/effectEditorForm.ts`
- Create: `client/src/game-portal/src/game/effects/effectEditorApi.ts`
- Test: `client/src/game-portal/src/game/effects/effectEditorForm.test.ts` + `effectEditorApi.test.ts`

**Interfaces:**
- Produces:
  - `interface AuthoredEffectDef { id: string; spritePath?: string; duration?: number; anchor?: string; [k: string]: any }`
  - `interface EffectEditorForm extends AuthoredEffectDef { remainder: Record<string, unknown> }`
  - `createBlankForm()`, `formFromDef(def)`, `saveRequestFromForm(form)`
  - `class EditorValidationError extends Error { serverMessage: string }`
  - `fetchAuthoredEffectDefs()`, `saveEditorEffect(effect)`, `deleteEditorEffect(id)`

- [ ] **Step 1: Write the failing tests**

Create `client/src/game-portal/src/game/effects/effectEditorForm.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { createBlankForm, formFromDef, saveRequestFromForm, type AuthoredEffectDef } from './effectEditorForm'

describe('effectEditorForm', () => {
  it('createBlankForm has empty id + remainder', () => {
    const f = createBlankForm()
    expect(f.id).toBe('')
    expect(f.remainder).toEqual({})
  })
  it('round-trips modeled fields and preserves unmodeled keys', () => {
    const def = { id: 'glow', duration: 0.5, anchor: 'head', futureKnob: 7 } as AuthoredEffectDef
    const form = formFromDef(def)
    expect(form.duration).toBe(0.5)
    expect(form.remainder).toEqual({ futureKnob: 7 })
    const out = saveRequestFromForm(form)
    expect(out.anchor).toBe('head')
    expect((out as Record<string, unknown>).futureKnob).toBe(7)
  })
  it('saveRequestFromForm drops undefined modeled fields', () => {
    const form = createBlankForm()
    form.id = 'x'
    const out = saveRequestFromForm(form)
    expect('duration' in out).toBe(false)
  })
})
```

Create `client/src/game-portal/src/game/effects/effectEditorApi.test.ts`:

```ts
import { afterEach, describe, expect, it, vi } from 'vitest'
import { EditorValidationError, saveEditorEffect, fetchAuthoredEffectDefs } from './effectEditorApi'

afterEach(() => vi.restoreAllMocks())

function mockFetch(status: number, body: unknown) {
  vi.stubGlobal('fetch', vi.fn(async () => ({
    ok: status >= 200 && status < 300, status, json: async () => body,
  })) as unknown as typeof fetch)
}

describe('effectEditorApi', () => {
  it('throws EditorValidationError on 400 validation_failed', async () => {
    mockFetch(400, { error: 'validation_failed', message: 'bad duration' })
    await expect(saveEditorEffect({ id: 'x' })).rejects.toBeInstanceOf(EditorValidationError)
  })
  it('fetchAuthoredEffectDefs reads the effects array', async () => {
    mockFetch(200, { effects: [{ id: 'glow' }, { id: 'fizzle' }] })
    await expect(fetchAuthoredEffectDefs()).resolves.toEqual([{ id: 'glow' }, { id: 'fizzle' }])
  })
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd client/src/game-portal && npx vitest run src/game/effects/`
Expected: FAIL — modules not found.

- [ ] **Step 3: Create `effectEditorForm.ts`**

```ts
// AuthoredEffectDef is the full authored shape of an EffectDef. Modeled fields
// are typed; unmodeled/future keys ride along via the index signature and are
// preserved verbatim through the form's remainder.
export interface AuthoredEffectDef {
  id: string
  spritePath?: string
  duration?: number
  anchor?: string
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  [key: string]: any
}

const MODELED_KEYS = ['id', 'spritePath', 'duration', 'anchor'] as const

export interface EffectEditorForm extends AuthoredEffectDef {
  remainder: Record<string, unknown>
}

export function createBlankForm(): EffectEditorForm {
  return { id: '', remainder: {} }
}

export function formFromDef(def: AuthoredEffectDef): EffectEditorForm {
  const modeled: Record<string, unknown> = {}
  const remainder: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(def)) {
    if ((MODELED_KEYS as readonly string[]).includes(k)) modeled[k] = v
    else remainder[k] = v
  }
  return { ...(modeled as AuthoredEffectDef), remainder }
}

export function saveRequestFromForm(form: EffectEditorForm): AuthoredEffectDef {
  const { remainder, ...modeled } = form
  const out: Record<string, unknown> = { ...remainder }
  for (const [k, v] of Object.entries(modeled)) {
    if (v === undefined) continue
    out[k] = v
  }
  return out as AuthoredEffectDef
}
```

- [ ] **Step 4: Create `effectEditorApi.ts`**

```ts
import type { AuthoredEffectDef } from './effectEditorForm'

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

// EditorValidationError carries the server's validation message for inline
// display beside Save. Body shape: {"error":"validation_failed","message":"..."}
export class EditorValidationError extends Error {
  serverMessage: string
  constructor(message: string) {
    super(message)
    this.name = 'EditorValidationError'
    this.serverMessage = message
  }
}

export async function fetchAuthoredEffectDefs(): Promise<AuthoredEffectDef[]> {
  const res = await fetch(`${API_BASE}/catalog/effects`)
  if (!res.ok) throw new Error(`Failed to load effect defs: ${res.status}`)
  const data = (await res.json()) as { effects: AuthoredEffectDef[] }
  return data.effects ?? []
}

export async function saveEditorEffect(effect: AuthoredEffectDef): Promise<void> {
  const res = await fetch(`${API_BASE}/effects`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ effect }),
  })
  if (res.status === 400) {
    const body = (await res.json()) as { error?: string; message?: string }
    if (body.error === 'validation_failed') throw new EditorValidationError(body.message ?? 'validation failed')
  }
  if (!res.ok) throw new Error(`Failed to save effect: ${res.status}`)
}

export async function deleteEditorEffect(id: string): Promise<'deleted' | 'reset'> {
  const res = await fetch(`${API_BASE}/effects/${encodeURIComponent(id)}`, { method: 'DELETE' })
  if (!res.ok) throw new Error(`Failed to delete effect: ${res.status}`)
  const body = (await res.json()) as { status: 'deleted' | 'reset' }
  return body.status
}
```

- [ ] **Step 5: Run tests + build**

Run: `cd client/src/game-portal && npx vitest run src/game/effects/ && npm run build`
Expected: PASS + build clean.

- [ ] **Step 6: Commit**

```bash
git add client/src/game-portal/src/game/effects/effectEditorForm.ts client/src/game-portal/src/game/effects/effectEditorApi.ts client/src/game-portal/src/game/effects/effectEditorForm.test.ts client/src/game-portal/src/game/effects/effectEditorApi.test.ts
git commit -m "feat(effects): client form + api modules"
```

---

## Task 5: Editor panel + view + route

**Files:**
- Create: `client/src/game-portal/src/components/EffectEditorPanel.vue`
- Create: `client/src/game-portal/src/views/EffectEditor.vue`
- Modify: `client/src/game-portal/src/router/index.ts`
- Test: `client/src/game-portal/src/components/EffectEditorPanel.test.ts`
- Reference (read first): `client/src/game-portal/src/components/UnitTypeEditorPanel.vue` (list/form/save/delete shell) and `client/src/game-portal/src/views/UnitTypeEditor.vue` (view shell).

**Interfaces:**
- Consumes: everything from `effectEditorForm.ts` + `effectEditorApi.ts`.
- Produces: `EffectEditorPanel.vue` (default export, no props/emits), `EffectEditor.vue`, `/effect-editor` route.

**Panel structure (build to this):**
- On mount: `fetchAuthoredEffectDefs()` into a `ref` for the left list.
- Left column: list of effect defs (id) + a "New" button (`createBlankForm()`); clicking a def runs `formFromDef(def)` into `form` and records the selected id.
- Right column form:
  - `id` — text input, **disabled when editing an existing def** (`:disabled="selectedId !== null"`).
  - `spritePath` — text input.
  - `duration` — number input (`v-model.number`).
  - `anchor` — `<select v-model="form.anchor">` over `['', 'center', 'feet', 'head']` (the `''` option labeled e.g. "(center — default)").
- Save → `saveRequestFromForm(form)` → `saveEditorEffect(...)`; on `EditorValidationError`, show `err.serverMessage` inline; on success refresh the list + a saved indicator.
- Delete/Reset → `deleteEditorEffect(form.id)`; word the result from the returned `'deleted' | 'reset'`.
- `data-test="effect-row"` on the list rows.
- No literal `cursor:` declarations.

- [ ] **Step 1: Write the failing test**

Create `client/src/game-portal/src/components/EffectEditorPanel.test.ts`:

```ts
import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import EffectEditorPanel from './EffectEditorPanel.vue'

function stubFetch() {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (String(url).endsWith('/catalog/effects')) {
      return { ok: true, status: 200, json: async () => ({ effects: [{ id: 'healing_glow', duration: 0.6, anchor: 'center' }] }) }
    }
    return { ok: true, status: 200, json: async () => ({}) }
  }) as unknown as typeof fetch)
}

afterEach(() => vi.restoreAllMocks())

describe('EffectEditorPanel', () => {
  it('mounts and lists effects from the catalog', async () => {
    stubFetch()
    const wrapper = mount(EffectEditorPanel)
    await flushPromises()
    expect(wrapper.text()).toContain('healing_glow')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client/src/game-portal && npx vitest run src/components/EffectEditorPanel.test.ts`
Expected: FAIL — component not found.

- [ ] **Step 3: Read the reference, then build the panel**

Read `UnitTypeEditorPanel.vue` in full; mirror its list/form/save/delete shell and scoped-CSS idioms. Build `EffectEditorPanel.vue` per "Panel structure" above (4 fields, anchor hardcoded select, `data-test="effect-row"`, no literal `cursor:`).

- [ ] **Step 4: Create the view + route**

`client/src/game-portal/src/views/EffectEditor.vue` (mirror `UnitTypeEditor.vue`):

```vue
<template>
  <div class="effect-editor-view">
    <div class="effect-editor-view__topbar">
      <ExitButton @click="router.push('/')" />
    </div>
    <EffectEditorPanel />
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import EffectEditorPanel from '@/components/EffectEditorPanel.vue'
import ExitButton from '@/components/ui/ExitButton.vue'
const router = useRouter()
</script>

<style scoped>
.effect-editor-view { position: relative; width: 100%; height: 100%; min-height: 0; display: flex; overflow: hidden; }
.effect-editor-view__topbar { position: absolute; top: 16px; right: 16px; z-index: 20; }
</style>
```

In `client/src/game-portal/src/router/index.ts`, add the import next to the other editor views:

```ts
import EffectEditor from '@/views/EffectEditor.vue'
```

and the route next to `/ability-editor` (or `/unit-type-editor`):

```ts
    { path: '/effect-editor', component: EffectEditor, meta: { hideDominionPanel: true } },
```

- [ ] **Step 5: Run test + full client build**

Run: `cd client/src/game-portal && npx vitest run src/components/EffectEditorPanel.test.ts && npm run build`
Expected: PASS + build clean.

- [ ] **Step 6: Commit**

```bash
git add client/src/game-portal/src/components/EffectEditorPanel.vue client/src/game-portal/src/components/EffectEditorPanel.test.ts client/src/game-portal/src/views/EffectEditor.vue client/src/game-portal/src/router/index.ts
git commit -m "feat(effects): editor panel + /effect-editor route"
```

---

## Task 6: World-editor toolbar + popup wiring

**Files:**
- Modify: `client/src/game-portal/src/components/world-editor/WorldEditorToolbar.vue`
- Modify: `client/src/game-portal/src/components/world-editor/worldEditorToolbar.test.ts`
- Modify: `client/src/game-portal/src/components/world-editor/WorldEditorPanel.vue`

**Interfaces:**
- Consumes: `EffectEditorPanel` (Task 5).

- [ ] **Step 1: Update the toolbar test expectation**

In `worldEditorToolbar.test.ts`, find the enabled/disabled assertions and update so `effects` is enabled: change the `expect(byId.effects.enabled).toBe(false)` assertion to `.toBe(true)`, leaving `unit-paths`/`perks`/`projectiles`/`campaigns` disabled.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client/src/game-portal && npx vitest run src/components/world-editor/worldEditorToolbar.test.ts`
Expected: FAIL — `effects` still disabled in the component.

- [ ] **Step 3: Flip the toolbar entry**

In `WorldEditorToolbar.vue`, change the effects entry:

```ts
  { id: 'effects', label: 'Effects', enabled: true },
```

- [ ] **Step 4: Run toolbar test to verify it passes**

Run: `cd client/src/game-portal && npx vitest run src/components/world-editor/worldEditorToolbar.test.ts`
Expected: PASS

- [ ] **Step 5: Wire the popup in `WorldEditorPanel.vue`**

Mirror ALL the existing `abilitiesPopupOpen` touch points for a new `effectsPopupOpen` (use Grep for `abilitiesPopupOpen` / `AbilityEditorPanel` to find each):

(a) Import next to `AbilityEditorPanel`:

```ts
import EffectEditorPanel from '@/components/EffectEditorPanel.vue'
```

(b) State ref next to `const abilitiesPopupOpen = ref(false)`:

```ts
const effectsPopupOpen = ref(false)
```

(c) In `onToolbarSelect`, add a case next to `case 'abilities':`, and remove `effects` from the default-case "coming soon" comment:

```ts
    case 'effects':
      effectsPopupOpen.value = true
      break
```

(d) In `toolbarActiveId`, next to the abilities line:

```ts
  if (effectsPopupOpen.value) return 'effects'
```

(e) The modal, next to the `abilitiesPopupOpen` modal:

```vue
    <div v-if="effectsPopupOpen" class="we-modal-overlay">
      <div class="we-modal we-modal--wide">
        <div class="we-modal__header">
          <span>Effect Editor</span>
          <UiButton size="sm" @click="effectsPopupOpen = false">Close</UiButton>
        </div>
        <div class="we-modal__body">
          <EffectEditorPanel />
        </div>
      </div>
    </div>
```

- [ ] **Step 6: Full client gates**

Run: `cd client/src/game-portal && npm run build && npm run test`
Expected: build clean, all tests green.

- [ ] **Step 7: Commit**

```bash
git add client/src/game-portal/src/components/world-editor/WorldEditorToolbar.vue client/src/game-portal/src/components/world-editor/worldEditorToolbar.test.ts client/src/game-portal/src/components/world-editor/WorldEditorPanel.vue
git commit -m "feat(effects): enable Effects toolbar category + world-editor popup"
```

---

## Task 7: Final verification

**Files:** none (verification only).

- [ ] **Step 1: Full server suite**

Run: `cd server && go build ./... && go vet ./... && go test ./...`
Expected: builds, vet clean, all tests pass. (Known repo-wide pre-existing failures may exist — `internal/ws TestSPBaseline_StructuralShape`, `cmd/api TestServerReadyLineAndStdinShutdown` — treat as expected if they fail identically on `main`; `internal/game` + `internal/http` MUST pass.)

- [ ] **Step 2: Full client suite**

Run: `cd client/src/game-portal && npm run build && npm run test`
Expected: build clean, all tests pass.

- [ ] **Step 3: Manual E2E (hard gate — requires a running server)**

1. World editor → **Effects** → **New** → author an effect (id `test_glow`, duration `0.75`, anchor `head`) → Save → confirm it appears in the list.
2. Open the **Abilities** editor → an effect dropdown (e.g. Effect On Target) → confirm `test_glow` appears as a choice.
3. Edit an embedded effect's duration → Save → **Delete/Reset** → confirm it reverts to the shipped default.

- [ ] **Step 4: Confirm clean tree**

Run: `git status` (only intended files committed; spec/plan docs + pre-existing untracked items may remain) and `git log --oneline` for the task commits.

---

## Self-Review Notes (for the executor)

- **Spec coverage:** §1 overlay + overlay-aware readers → Task 2; §2 `validateEffectDef` → Task 1; §3 editor+HTTP → Task 3; §4 client triad → Tasks 4-5; §5 wiring → Tasks 5-6; testing → per-task + Task 7.
- **Type consistency:** `AuthoredEffectDef`/`EffectEditorForm` and `MODELED_KEYS` are defined once (Task 4) and consumed by Task 5. Server `EditorEffectSaveRequest.Effect` (Task 3) matches the client POST body `{ effect }` (Task 4). The `/catalog/effects` key `effects` (already server-side) matches the client fetcher (Task 4).
- **Watch item:** Task 1 — add `"fmt"` to `effect_defs.go`'s import block if it isn't already imported (the existing loader panics use string concat, not `fmt`).
