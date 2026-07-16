# Standalone Perks SP1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move perks from pools-nested-under-units to a standalone `catalog/perks/<id>/<id>.json` catalog with its own editor, runtime **behavior-identical**, guarded by a migration-equivalence test.

**Architecture:** A migration generator converts the 21 pool files → 72 standalone `PerkDef` files (eligibility baked onto each def). A Go test proves the perk registry built from the new catalog is deep-equal to the one from the old pools. Then the server flips atomically: a standalone loader + an id-addressed overlay + `POST /perks {perk}` / `DELETE /perks/{id}`, removing all pool machinery and the old pool files. `rebuildPerkRegistry` keeps its atomic-swap + sorted-determinism, only its source changes (id-keyed defs, not pool arrays), so the reader contract and determinism are preserved. The client gets a standalone Perks editor triad and retires `PerkPoolEditor` + the unit-editor perk section.

**Tech Stack:** Go (`internal/game`, `internal/http`, `cmd/`), TypeScript / Vue 3 (`game-portal`), Vitest, `go test`.

## Global Constraints

- Branch: `perks-standalone` (off `reference-def-editors`, base `a53ff60`). Nothing to `main` until the whole perks effort is done. No push.
- **Runtime behavior-identical:** perk selection (`eligiblePerksForUnitAtRank`), determinism (sorted-by-id registry, `rngPerks`), and every perk hook must be unchanged. No `game`→`profile` write.
- The registry `perkDefsByID map[string]*PerkDef` stays global-by-id, its reads stay `perkDefsMu`-synchronized, and rebuild stays deterministic (sorted keys, atomic swap under `perkDefsMu.Lock()`).
- `perkIDPattern = ^[a-z0-9_]+$` — path-traversal gate on every id entry point.
- `PerkDef` struct is UNCHANGED (see it at `perk_defs.go:92-160`): `ID`,`DisplayName`,`Description`,`TooltipTemplate`,`TooltipTemplateByTrap`,`TooltipTemplateByOwnedPerk`,`Icon`,`UnitType`,`Path`,`Rank`,`RequiresPerk`,`Config map[string]float64`,`ConfigByRank map[string]map[string]float64`,`Effect *PerkEffect`,`GrantsAbilities []string`,`Wired bool` (derived, set only by `ListPerkDefs`). `PerkEffect{Name,Target,SizeScale,DurationSeconds,Variant}`.
- Standalone file = a marshaled `PerkDef` with `config` already SPLIT into `config` (scalars) + `configByRank` (per-rank), and `unitType`/`path`/`rank` present in the JSON (were injected from the dir path before).
- WorldEditorPanel wiring uses the CURRENT screen-switch pattern.
- No literal `cursor:` in new component CSS except `cursor: not-allowed` on forbidden states.
- Build gates: server `go build ./...` + `go vet ./...` + `go test ./...` (NOT gofmt). Client `npm run build` (`vue-tsc -b`) + `npm run test`. 3 pre-existing `ListEditorPanel.test.ts` failures are expected; confirm no NEW failures.
- Per-task commits, explicit `git add <files>` (NEVER `-A`/`.` — untracked docs + pre-existing `test-steam.ps1` / `.claude/skills/build-nomads/` must not be swept in).

## File Structure

**Server:** `cmd/migrate-perks/main.go` (CREATE, one-shot), `catalog/perks/**` (GENERATED, committed), `perk_defs.go` (MODIFY: standalone loader + `validatePerkDef`, remove pool loader), `perk_persistence.go` (MODIFY: id-addressed overlay + rebuild-source flip, remove pool machinery), `perk_editor.go` (MODIFY: id-addressed wrappers), `editor_handlers.go` (MODIFY: `/perks` flip), `cmd/api/main.go` (unchanged call site), and DELETE `catalog/units/**/paths/**/perks/*.json`.

**Client:** `game/perks/perkEditorForm.ts` + `perkEditorApi.ts` (CREATE), `components/PerkEditorPanel.vue` + `views/PerkEditor.vue` (CREATE), `router/index.ts` + `WorldEditorToolbar.vue`(+test) + `WorldEditorPanel.vue` (MODIFY), DELETE `components/PerkPoolEditor.vue`(+test), MODIFY `components/UnitTypeEditorPanel.vue` (remove perk section) + `game/units/pathEditorApi.ts` (remove savePerks/deletePerks).

---

## Task 1: Migration generator + standalone catalog + equivalence gate

**Files:**
- Create: `server/cmd/migrate-perks/main.go`
- Generate + commit: `server/internal/game/catalog/perks/<id>/<id>.json` (72 files)
- Test: `server/internal/game/perk_migration_test.go`

**Interfaces:**
- Consumes: the existing `embeddedPerkPools`, `buildPerkDefsFromPool`, `perkDefsByID` (old registry).
- Produces: the standalone catalog on disk + a `perkDefFromEmbeddedFile(path)`-style test helper.

- [ ] **Step 1: Write the migration generator**

The generator reuses the exact in-package conversion (`buildPerkDefsFromPool`) so the output is provably the same defs. It must live where it can read the game package internals — put it in `server/cmd/migrate-perks/main.go` and have it call an exported helper, OR (simpler) implement it as a Go **test** in package `game` that writes the files. Use the test approach (it has direct access to `embeddedPerkPools`/`buildPerkDefsFromPool` and runs in-package):

Create `server/internal/game/perk_migration_test.go`:

```go
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
```

- [ ] **Step 2: Run the generator + commit the catalog**

Run: `cd server && GENERATE_PERK_CATALOG=1 go test ./internal/game/ -run TestGeneratePerkCatalog -count=1`
Then confirm the tree: `ls server/internal/game/catalog/perks | head` (expect ~72 dirs). Inspect one file (e.g. `catalog/perks/bloodlust/bloodlust.json`) — it must contain `"unitType"`, `"path"`, `"rank"`, and split `"config"`/`"configByRank"`.

- [ ] **Step 3: Write the equivalence test (the gate)**

Append to `server/internal/game/perk_migration_test.go` — build a registry from the NEW catalog files and deep-compare it to the OLD (`perkDefsByID` still built from pools at this point):

```go
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
```

- [ ] **Step 4: Run the equivalence test**

Run: `cd server && go test ./internal/game/ -run TestPerkCatalogEquivalentToPools -count=1`
Expected: PASS — proves the standalone catalog reproduces the exact registry. If it fails, the migration is lossy; fix the generator before proceeding.

- [ ] **Step 5: Commit the catalog + tests**

```bash
git add server/internal/game/catalog/perks server/internal/game/perk_migration_test.go
git commit -m "feat(perks): generate standalone perk catalog from pools + equivalence gate"
```

---

## Task 2: Server flip — standalone loader + id-addressed overlay + editor + HTTP (atomic)

This task switches the registry source to the standalone catalog and removes ALL pool machinery. It must land as one compiling unit. The Task 1 equivalence test + a new selection-equivalence assertion guard behavior.

**Files:**
- Modify: `server/internal/game/perk_defs.go`, `server/internal/game/perk_persistence.go`, `server/internal/game/perk_editor.go`, `server/internal/http/editor_handlers.go`
- Delete: `server/internal/game/catalog/units/**/paths/**/perks/*.json` (all 21 pool files)
- Test: `server/internal/game/perk_persistence_test.go` (rewrite for id-addressed), `server/internal/http/editor_handlers_perks_test.go` (rewrite)

**Interfaces:**
- Produces: `loadPerkDefs()`, `embeddedPerkDefs map[string]PerkDef`, `validatePerkDef`, `perkIDPattern`, `runtimePerks`, `SavePerkDef`, `PerkIsEmbedded`, `DeletePerkOverride`, `LoadPersistedPerksIntoOverlay` (id-addressed), `EditorPerkSaveRequest`, `SaveEditorPerk`, `DeleteEditorPerk`.

- [ ] **Step 1: Standalone loader + validator (`perk_defs.go`)**

Add the embed + loader (mirror `loadEffectDefs`), and `validatePerkDef`. Add near the top:

```go
//go:embed all:catalog/perks
var perkDefsFS embed.FS

var perkIDPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

// embeddedPerkDefs is the standalone catalog, id-keyed, loaded once at init.
var embeddedPerkDefs = loadPerkDefs()

func loadPerkDefs() map[string]PerkDef {
	entries, err := fs.ReadDir(perkDefsFS, "catalog/perks")
	if err != nil {
		panic("catalog/perks: " + err.Error())
	}
	result := make(map[string]PerkDef, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		rel := "catalog/perks/" + id + "/" + id + ".json"
		data, err := perkDefsFS.ReadFile(rel)
		if err != nil {
			panic(rel + ": " + err.Error())
		}
		var def PerkDef
		if err := json.Unmarshal(data, &def); err != nil {
			panic(rel + ": " + err.Error())
		}
		if def.ID == "" {
			panic(rel + `: missing "id"`)
		}
		if def.ID != id {
			panic(rel + ": id " + def.ID + " != dir " + id)
		}
		if err := validatePerkDef(&def); err != nil {
			panic(rel + ": " + err.Error())
		}
		if _, dup := result[def.ID]; dup {
			panic(rel + ": duplicate perk id " + def.ID)
		}
		result[def.ID] = def
	}
	return result
}

// validatePerkDef is the shared load + save content gate. Does NOT check id
// (loader gates against dir name; editor against perkIDPattern).
func validatePerkDef(def *PerkDef) error {
	switch def.Rank {
	case "", unitRankBronze, unitRankSilver, unitRankGold:
	default:
		return fmt.Errorf("rank %q must be \"\" | bronze | silver | gold", def.Rank)
	}
	if def.Effect != nil {
		switch def.Effect.Target {
		case "", "self", "enemies":
		default:
			return fmt.Errorf("effect.target %q must be \"self\" | \"enemies\"", def.Effect.Target)
		}
	}
	return nil
}
```

Ensure `perk_defs.go` imports `embed`, `io/fs`, `regexp`, `fmt`, `encoding/json`.

REMOVE the pool-walk `init()` (`:415-477`), `embeddedPerkPools` var, `perkEntryJSON`, `splitRankConfig`, `buildPerkDefsFromPool`, `clonePerkEntries`, `perkPoolKey`, `splitPerkPoolKey`, `perkPoolDirName`, `perkRankOverrideKeys` — but ONLY after confirming Task 1's generator (which used them) is committed; the generator test file will fail to compile once these are gone, so DELETE `perk_migration_test.go` in this task too (its job — generation + equivalence proof — is done and committed). `perkDefLookup`, `snapshotPerkDefs`, `ListPerkDefs`, `eligiblePerksForUnitAtRank`, `ConfigForRank`, `PerkEffect`, `PerkDef` stay.

- [ ] **Step 2: Id-addressed overlay + rebuild-source flip (`perk_persistence.go`)**

Rewrite the file to the effect/projectile overlay shape, but keep the `rebuildPerkRegistry` atomic-swap contract — only its SOURCE changes to id-keyed maps:

```go
var (
	runtimePerksMu sync.RWMutex
	runtimePerks   = map[string]PerkDef{}
)

func resolvePerksDir() (string, error) {
	if dir := os.Getenv("PERK_CATALOG_DIR"); dir != "" {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cwd, "internal", "game", "catalog", "perks")
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	return "", fmt.Errorf("perks directory not found at %s; set PERK_CATALOG_DIR to override", dir)
}

// rebuildPerkRegistry merges embeddedPerkDefs + runtimePerks (overlay wins) into
// a fresh id->*PerkDef map, swapped under perkDefsMu.Lock(). Sorted-by-id build
// preserves determinism. Readers (perkDefLookup/snapshotPerkDefs/ListPerkDefs)
// are unchanged — they still read perkDefsByID under perkDefsMu.
func rebuildPerkRegistry() {
	runtimePerksMu.RLock()
	overlay := make(map[string]PerkDef, len(runtimePerks))
	for k, v := range runtimePerks {
		overlay[k] = v
	}
	runtimePerksMu.RUnlock()

	merged := make(map[string]PerkDef, len(embeddedPerkDefs)+len(overlay))
	for k, v := range embeddedPerkDefs {
		merged[k] = v
	}
	for k, v := range overlay {
		merged[k] = v
	}
	ids := make([]string, 0, len(merged))
	for id := range merged {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	fresh := make(map[string]*PerkDef, len(merged))
	for _, id := range ids {
		def := merged[id]
		fresh[id] = &def
	}
	perkDefsMu.Lock()
	perkDefsByID = fresh
	perkDefsMu.Unlock()
}

func SavePerkDef(def *PerkDef) error {
	if !perkIDPattern.MatchString(def.ID) {
		return fmt.Errorf("perk id %q must match %s", def.ID, perkIDPattern)
	}
	if err := validatePerkDef(def); err != nil {
		return err
	}
	dir, err := resolvePerksDir()
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
	runtimePerksMu.Lock()
	runtimePerks[def.ID] = *def
	runtimePerksMu.Unlock()
	rebuildPerkRegistry()
	return nil
}

func PerkIsEmbedded(id string) bool {
	_, ok := embeddedPerkDefs[id]
	return ok
}

func DeletePerkOverride(id string) (existed bool, err error) {
	if !perkIDPattern.MatchString(id) {
		return false, nil
	}
	dir, derr := resolvePerksDir()
	if derr != nil {
		return false, derr
	}
	removed := false
	if rerr := os.Remove(filepath.Join(dir, id, id+".json")); rerr == nil {
		removed = true
		_ = os.Remove(filepath.Join(dir, id))
	}
	runtimePerksMu.Lock()
	_, inOverlay := runtimePerks[id]
	delete(runtimePerks, id)
	runtimePerksMu.Unlock()
	rebuildPerkRegistry()
	return removed || inOverlay, nil
}

func LoadPersistedPerksIntoOverlay() {
	dir, err := resolvePerksDir()
	if err != nil {
		slog.Info("persisted perks: no writable perks dir; using embedded catalog only", "err", err)
		return
	}
	loaded := 0
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		var def PerkDef
		if json.Unmarshal(raw, &def) != nil || def.ID == "" || validatePerkDef(&def) != nil {
			slog.Warn("persisted perks: skipped file", "file", d.Name())
			return nil
		}
		runtimePerksMu.Lock()
		runtimePerks[def.ID] = def
		runtimePerksMu.Unlock()
		loaded++
		return nil
	})
	if loaded > 0 {
		rebuildPerkRegistry()
		slog.Info("persisted perks: overlaid on embedded catalog", "count", loaded, "dir", dir)
	}
}

// init builds the initial registry from the embedded standalone catalog.
func init() { rebuildPerkRegistry() }
```

REMOVE `runtimePerkPools`, `runtimePerkPoolsMu`, the old `rebuildPerkRegistry` body, `validatePerkPoolEntries`, `SavePerkPool`, `PerkPoolIsEmbedded`, `DeletePerkPool`, `removePerkPoolFile`, `loadPersistedPerkPoolsFromDir`, `parsePersistedPerkPoolFile`, and the old `resolvePerksDir` alias. Ensure imports are correct (`encoding/json`, `log/slog`, `os`, `path/filepath`, `sort`, `strings`, `sync`, `fmt`).

- [ ] **Step 3: Editor wrapper (`perk_editor.go`)**

Replace the pool wrappers with:

```go
package game

import "fmt"

type EditorPerkSaveRequest struct {
	Perk PerkDef `json:"perk"`
}

func SaveEditorPerk(req EditorPerkSaveRequest) error {
	perk := req.Perk
	if !perkIDPattern.MatchString(perk.ID) {
		return editorValidationError{fmt.Errorf("perk id %q must match %s", perk.ID, perkIDPattern)}
	}
	if err := validatePerkDef(&perk); err != nil {
		return editorValidationError{err}
	}
	return SavePerkDef(&perk)
}

func DeleteEditorPerk(id string) (existed bool, err error) {
	return DeletePerkOverride(id)
}
```

- [ ] **Step 4: HTTP flip (`editor_handlers.go`)**

REPLACE the `POST /perks` (`:525-553`) and `DELETE /perks/` (`:556-582`) handlers with the effect/projectile shape:

```go
	mux.HandleFunc("/perks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
			return
		}
		var req game.EditorPerkSaveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		if err := game.SaveEditorPerk(req); err != nil {
			if game.IsEditorValidationError(err) {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": req.Perk.ID, "status": "saved"})
	})

	mux.HandleFunc("/perks/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/perks/")
		if r.Method != http.MethodDelete {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "DELETE only")
			return
		}
		if id == "" || strings.Contains(id, "/") {
			writeJSONError(w, http.StatusBadRequest, "invalid_id", "expected /perks/{id}")
			return
		}
		existed, err := game.DeleteEditorPerk(id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error())
			return
		}
		if !existed {
			writeJSONError(w, http.StatusNotFound, "not_found", "no editor override for "+id)
			return
		}
		status := "deleted"
		if game.PerkIsEmbedded(id) {
			status = "reset"
		}
		writeJSON(w, map[string]string{"id": id, "status": status})
	})
```

- [ ] **Step 5: Delete the old pool files**

Run: `rm -r server/internal/game/catalog/units/*/*/paths/*/perks` (removes the 21 `perks/` dirs across all unit/path trees). Confirm: `find server/internal/game/catalog/units -name 'perks' -type d` returns nothing.

- [ ] **Step 6: Rewrite the server tests**

Rewrite `server/internal/game/perk_persistence_test.go` for the id-addressed model (mirror `effect_persistence_test.go`): `SavePerkDef` → `perkDefLookup` round-trip; overlay-wins; disk round-trip + embed-revert (using a real embedded perk id via `PerkIsEmbedded`); bad-id reject; `DeletePerkOverride`. Rewrite `server/internal/http/editor_handlers_perks_test.go` for `POST /perks {perk}` 201/400 + `DELETE /perks/{id}` deleted/reset (mirror `editor_handlers_effects_test.go`). Add a **selection-equivalence** test: for representative (unitType, path, rank) triples that shipped perks, `eligiblePerksForUnitAtRank`-style filtering over `snapshotPerkDefs()` returns the expected perk ids (pin the known bronze/silver/gold sets for e.g. soldier/berserker).

- [ ] **Step 7: Build, vet, full game+http suites**

Run: `cd server && go build ./... && go vet ./... && go test ./internal/game/ ./internal/http/`
Expected: PASS. `go build` passing proves every pool reference was removed consistently.

- [ ] **Step 8: Commit**

```bash
git add server/internal/game/perk_defs.go server/internal/game/perk_persistence.go server/internal/game/perk_editor.go server/internal/http/editor_handlers.go server/internal/game/perk_persistence_test.go server/internal/http/editor_handlers_perks_test.go
git rm -r server/internal/game/catalog/units/*/*/paths/*/perks
git rm server/internal/game/perk_migration_test.go
git commit -m "feat(perks): flip server to standalone id-addressed perk catalog + editor, retire pools"
```

---

## Task 3: Client perk form + API modules

**Files:**
- Create: `client/src/game-portal/src/game/perks/perkEditorForm.ts` + `perkEditorApi.ts`
- Test: `perkEditorForm.test.ts` + `perkEditorApi.test.ts`

**Interfaces:**
- Produces: `AuthoredPerkDef` (models all `PerkDef` fields), `PerkEditorForm` + `remainder`, `createBlankForm`/`formFromDef`/`saveRequestFromForm`, `EditorValidationError`, `fetchAuthoredPerkDefs`, `saveEditorPerk`, `deleteEditorPerk`.

- [ ] **Step 1: Write the failing tests**

Create `client/src/game-portal/src/game/perks/perkEditorForm.test.ts` (mirror `effectEditorForm.test.ts`) asserting: `createBlankForm().id===''`; round-trip of modeled fields (`id`, `displayName`, `unitType`, `path`, `rank`, `config`, `effect`, `grantsAbilities`) + an unmodeled key preserved in `remainder`; drop-undefined. Create `perkEditorApi.test.ts` (mirror `effectEditorApi.test.ts`) asserting: 400 `validation_failed` → `EditorValidationError`; `fetchAuthoredPerkDefs` reads `{perks}`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd client/src/game-portal && npx vitest run src/game/perks/`
Expected: FAIL — modules not found.

- [ ] **Step 3: Create `perkEditorForm.ts`**

```ts
export interface PerkEffectShape {
  name?: string
  target?: string
  sizeScale?: number
  durationSeconds?: number
  variant?: string
}

export interface AuthoredPerkDef {
  id: string
  displayName?: string
  description?: string
  tooltipTemplate?: string
  tooltipTemplateByTrap?: Record<string, string>
  tooltipTemplateByOwnedPerk?: Record<string, string>
  icon?: string
  unitType?: string
  path?: string
  rank?: string
  requiresPerk?: string
  config?: Record<string, number>
  configByRank?: Record<string, Record<string, number>>
  effect?: PerkEffectShape | null
  grantsAbilities?: string[]
  wired?: boolean
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  [key: string]: any
}

const MODELED_KEYS = [
  'id','displayName','description','tooltipTemplate','tooltipTemplateByTrap',
  'tooltipTemplateByOwnedPerk','icon','unitType','path','rank','requiresPerk',
  'config','configByRank','effect','grantsAbilities','wired',
] as const

export interface PerkEditorForm extends AuthoredPerkDef {
  remainder: Record<string, unknown>
}

export function createBlankForm(): PerkEditorForm {
  return { id: '', remainder: {} }
}

export function formFromDef(def: AuthoredPerkDef): PerkEditorForm {
  const modeled: Record<string, unknown> = {}
  const remainder: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(def)) {
    if ((MODELED_KEYS as readonly string[]).includes(k)) modeled[k] = v
    else remainder[k] = v
  }
  return { ...(modeled as AuthoredPerkDef), remainder }
}

export function saveRequestFromForm(form: PerkEditorForm): AuthoredPerkDef {
  const { remainder, ...modeled } = form
  const out: Record<string, unknown> = { ...remainder }
  for (const [k, v] of Object.entries(modeled)) {
    if (v === undefined) continue
    if (k === 'wired') continue // derived server-side; never sent
    out[k] = v
  }
  return out as AuthoredPerkDef
}
```

- [ ] **Step 4: Create `perkEditorApi.ts`**

Mirror `effectEditorApi.ts` exactly, substituting perk/perks: `fetchAuthoredPerkDefs()` GET `/catalog/perks` → `data.perks ?? []`; `saveEditorPerk(perk)` POST `/perks` `{perk}` with the 400 `validation_failed` → local `EditorValidationError`; `deleteEditorPerk(id)` DELETE `/perks/{id}` → `'deleted'|'reset'`.

- [ ] **Step 5: Run tests + build**

Run: `cd client/src/game-portal && npx vitest run src/game/perks/ && npm run build`
Expected: PASS + clean.

- [ ] **Step 6: Commit**

```bash
git add client/src/game-portal/src/game/perks/
git commit -m "feat(perks): client form + api modules"
```

---

## Task 4: Perk editor panel + view + route

**Files:**
- Create: `client/src/game-portal/src/components/PerkEditorPanel.vue` + `views/PerkEditor.vue`
- Modify: `client/src/game-portal/src/router/index.ts`
- Test: `PerkEditorPanel.test.ts`
- Reference (read first): `components/EffectEditorPanel.vue` (shell) + `views/EffectEditor.vue`; and the OLD `PerkPoolEditor.vue` for the perk-specific field controls (config rows, effect fields) before it is deleted in Task 5.

**Panel structure (build to this — full `PerkDef` coverage):**
- On mount: `fetchAuthoredPerkDefs()` (left list) + `fetchUnitDefs()`/path options for the eligibility dropdowns (unitType from `/catalog/units`; path/rank can be free-text or dropdowns — `rank` is `['', 'bronze', 'silver', 'gold']`).
- Left list of perks (id + displayName, plus an "inert" badge when `!wired`) + "New".
- Form fields: `id` (text, disabled when editing); `displayName`; `description`; `icon`; **eligibility** `unitType`/`path` (text or dropdown, blank = "(any)") + `rank` (`<select>` `['','bronze','silver','gold']`); `requiresPerk` (text/dropdown of perk ids); `tooltipTemplate`; `config` as add/remove key→number rows; `configByRank` as rank→(key→number) (can be a simpler textarea/JSON control if add/remove nesting is heavy — full coverage but pragmatic); `effect` (name/target/sizeScale/durationSeconds/variant, target `<select>` `['','self','enemies']`); `grantsAbilities` as a string-list (comma or rows). `wired` shown read-only.
- Save → `saveRequestFromForm` → `saveEditorPerk`; `EditorValidationError` inline. Delete/Reset → `deleteEditorPerk(selectedId)` worded from status. `data-test="perk-row"`. No literal `cursor:`.

- [ ] **Step 1: Write the failing test**

Create `PerkEditorPanel.test.ts` (mirror `EffectEditorPanel.test.ts`): stub `/catalog/perks` returning one perk (`{perks:[{id:'bloodlust',displayName:'Bloodlust',rank:'bronze',wired:true}]}`) and `/catalog/units` returning `{units:[{type:'soldier'}], paths:[], pathsByUnit:{}}`; mount; `flushPromises`; assert it lists 'Bloodlust'.

- [ ] **Step 2: Run test to verify it fails** — `npx vitest run src/components/PerkEditorPanel.test.ts` → FAIL.

- [ ] **Step 3: Read the references, then build the panel** per the structure above.

- [ ] **Step 4: Create the view + route** — `views/PerkEditor.vue` (mirror `EffectEditor.vue`, `perk-editor-view` + `PerkEditorPanel`); `router/index.ts` add `import PerkEditor from '@/views/PerkEditor.vue'` + `{ path: '/perk-editor', component: PerkEditor, meta: { hideDominionPanel: true } }` next to `/effect-editor`.

- [ ] **Step 5: Test + build** — `npx vitest run src/components/PerkEditorPanel.test.ts && npm run build` → PASS + clean.

- [ ] **Step 6: Commit**

```bash
git add client/src/game-portal/src/components/PerkEditorPanel.vue client/src/game-portal/src/components/PerkEditorPanel.test.ts client/src/game-portal/src/views/PerkEditor.vue client/src/game-portal/src/router/index.ts
git commit -m "feat(perks): standalone perk editor panel + /perk-editor route"
```

---

## Task 5: World-editor Perks screen + retire the pool UI (atomic client retirement)

**Files:**
- Modify: `WorldEditorToolbar.vue` (+ `worldEditorToolbar.test.ts`), `WorldEditorPanel.vue`
- Modify: `components/UnitTypeEditorPanel.vue` (remove perk section), `game/units/pathEditorApi.ts` (remove savePerks/deletePerks)
- Delete: `components/PerkPoolEditor.vue` + `PerkPoolEditor.test.ts`

- [ ] **Step 1: Toolbar test + flip** — in `worldEditorToolbar.test.ts` set `expect(byId.perks.enabled).toBe(true)` (leave `unit-paths`/`campaigns` false); in `WorldEditorToolbar.vue` set `{ id: 'perks', label: 'Perks', enabled: true }`. Run the toolbar test RED→GREEN.

- [ ] **Step 2: Wire the Perks screen (`WorldEditorPanel.vue`, current screen-switch pattern)** — import `PerkEditorPanel`; add `'perks'` to `EditorScreen`; add `case 'perks':` to the shared `activeScreen.value = id` arm; add `<PerkEditorPanel v-else-if="activeScreen === 'perks'" />` to the `<section class="we-screen">` chain; drop `perks` from the default comment.

- [ ] **Step 3: Retire the unit-editor perk section (`UnitTypeEditorPanel.vue`)** — remove the `<PerkPoolEditor .../>` host + its enclosing "Perk Pools" `SectionCard` (`:625-634`), `poolsForPath` (`:785-794`), the per-rank `savePerksApi` loop in `savePath` (`:1698-1700`), the `perkCatalog` fetch/state if now unused, and the `import PerkPoolEditor`/`savePerks`/`deletePerks`/`fetchPerkCatalog` imports that become unused. `npm run build` (noUnusedLocals) is the gate that all dangling refs are gone. Keep path/rank/ability editing intact.

- [ ] **Step 4: Remove pool endpoints from `pathEditorApi.ts`** — delete `savePerks` + `deletePerks` (the `POST /perks {unit,path,rank}` / `DELETE /perks/{u}/{p}/{r}` callers). Keep `fetchPerkCatalog` only if still referenced elsewhere; otherwise remove it. Keep `PerkEntry` if still imported by remaining code, else remove.

- [ ] **Step 5: Delete `PerkPoolEditor.vue` + its test.**

```bash
git rm client/src/game-portal/src/components/PerkPoolEditor.vue client/src/game-portal/src/components/PerkPoolEditor.test.ts
```

- [ ] **Step 6: Full client gates** — `cd client/src/game-portal && npm run build && npm run test`. Build clean (proves no dangling pool refs); suite green except the 3 known pre-existing `ListEditorPanel` failures. Confirm `UnitTypeEditorPanel.test.ts` still passes (path save flow without perks).

- [ ] **Step 7: Commit**

```bash
git add client/src/game-portal/src/components/world-editor/WorldEditorToolbar.vue client/src/game-portal/src/components/world-editor/worldEditorToolbar.test.ts client/src/game-portal/src/components/world-editor/WorldEditorPanel.vue client/src/game-portal/src/components/UnitTypeEditorPanel.vue client/src/game-portal/src/game/units/pathEditorApi.ts
git rm client/src/game-portal/src/components/PerkPoolEditor.vue client/src/game-portal/src/components/PerkPoolEditor.test.ts
git commit -m "feat(perks): enable Perks world-editor screen + retire pool-authoring UI"
```

---

## Task 6: Final verification

- [ ] **Step 1: Full server suite** — `cd server && go build ./... && go vet ./... && go test ./...` (known pre-existing `internal/ws`/`cmd/api` failures excepted; `internal/game`+`internal/http` MUST pass).
- [ ] **Step 2: Determinism replay** — run the game package's determinism/seed-replay test(s) (grep for `determinism`/`Seed` in `internal/game/*_test.go`) to confirm perk rolls are byte-identical before/after (the sorted-by-id registry + `rngPerks` are preserved).
- [ ] **Step 3: Full client suite** — `cd client/src/game-portal && npm run build && npm run test` (only the 3 known `ListEditorPanel` failures).
- [ ] **Step 4: Manual E2E (hard gate — running server):** author/edit a perk in the standalone Perks editor (set unitType/path/rank eligibility) → start a match → confirm the eligible unit rolls it at that rank exactly as before; reset an embedded perk; confirm the Unit Types editor no longer shows a perk section and still saves paths; confirm a match's perk selection matches pre-migration behavior.
- [ ] **Step 5: Confirm clean tree** — `git status` (only intended changes; the old `catalog/units/**/perks` gone, new `catalog/perks/**` present) and `git log --oneline` for the task commits.

---

## Self-Review Notes (for the executor)

- **Spec coverage:** §1 catalog format → Tasks 1-2; §2 migration + equivalence → Task 1; §3 loader + validatePerkDef → Task 2; §4 id-addressed persistence + rebuild-source flip → Task 2; §5 editor+HTTP → Task 2; §6 client triad + retirement → Tasks 3-5; testing/determinism → per-task + Task 6.
- **Behavior-identical guarantee** rests on: (a) Task 1's registry deep-equal test, (b) `rebuildPerkRegistry` keeping its sorted-by-id atomic swap (only source changed), (c) readers + `eligiblePerksForUnitAtRank` untouched, (d) Task 6 determinism replay.
- **Type consistency:** `AuthoredPerkDef`/`MODELED_KEYS` (Task 3) ↔ server `EditorPerkSaveRequest.Perk` `{perk}` (Task 2). `/catalog/perks` key `perks` (unchanged) ↔ fetcher (Task 3). `PerkDef` struct unchanged throughout.
- **Watch items:** (1) Tasks 2/5 are atomic compile-coupled removals — `go build`/`npm run build` passing is the completeness gate. (2) Delete `perk_migration_test.go` in Task 2 (it references the pool helpers being removed). (3) The standalone JSON stores `config` PRE-SPLIT (scalars) + `configByRank` — the generator (Task 1) does the split via `splitRankConfig`; the loader (Task 2) unmarshals directly (no split). (4) Task 5 relies on `noUnusedLocals` to catch every dangling pool reference in `UnitTypeEditorPanel`/`pathEditorApi`.
