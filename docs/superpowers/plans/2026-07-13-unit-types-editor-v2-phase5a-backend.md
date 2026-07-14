# Unit-Types Editor v2 — Phase 5a (Path/Perk Persistence Backend) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give promotion **paths** and **perks** a writable editor overlay + HTTP write routes (they are embed-only today), with promotion-integrity validation that makes a boot-panic-inducing save impossible.

**Architecture:** Mirror the existing unit/faction/ability editor persistence template exactly (validate → write disk → register overlay; `editorValidationError` → 400). The path loader's 16-site panic `init()` is refactored into an error-returning `registerPathFile` reused by both the fail-loud embed loader and the error-surfacing editor. The 10 derived path maps move behind an `RWMutex` accessor layer so an editor write can rebuild them safely while the sim reads them off the tick hot path (rank-up/spawn/item/upgrade only). Perks gain the same overlay + a duplicate-id rejection and a hand-maintained wired/inert flag.

**Tech Stack:** Go 1.22 (server), `net/http` ServeMux, `//go:embed`, `sync.RWMutex`.

**Scope boundary:** This plan is server-only and independently verifiable via `go test ./...`. The client path-editor UI is Phase 5b and depends on the routes this plan ships. No simulation behavior changes: every rank-up/spawn read resolves through the same maps, only now guarded and overlay-aware.

**Reference spec:** `docs/superpowers/specs/2026-07-13-unit-types-editor-v2-design.md` §7 (Pillar E), §8 (HTTP surface), §9 (validation/safety, esp. §9.1 promotion integrity), §10 (testing).

**Global gates (run from `server/`):**
- Build gate: `go build ./...`
- Vet gate: `go vet ./...`
- Test gate: `go test ./internal/game/... ./internal/http/...`
- Do NOT use `gofmt -l` as a gate (CRLF flags the whole checkout). Pre-existing failure `cmd/api TestServerReadyLineAndStdinShutdown` is unrelated — introduce no NEW failures.
- Determinism (AI_RULES): no wall-clock, no unseeded rand, no map-iteration order driving outcomes. Path rolls sort keys; perk pools sort by id — preserve both.

**Reference facts established by recon (do not re-derive):**
- Path loader: `path_defs.go` `init()` at :252-456, 16 panic sites, populates 10 package globals (`pathModifiersByKey`, `pathBoundsByPath`, `pathVisionRangeByPath`, `pathProjectileByPath`, `pathDamageTypeByPath`, `pathAttackTypeByPath`, `pathProjectileScaleByPath`, `pathAbilitiesByPath`, `pathChannelLoopByPath`, `pathsByUnitType`). On-disk shape = `pathCatalogFile` (:25-82).
- `pathChances` cross-validation panic at `path_defs.go:451`.
- Reads of derived maps (off tick hot path): `pathModifierFor` (progression.go:193), `applyRankModifiersLocked` (progression.go:404-585 — reads vision/projectile/damageType/attackType/projectileScale maps), `assignUnitPathAbilitiesLocked` (path_ability_defs.go:163), `channelLoopRangeForUnitLocked`, `rollProgressionPathLocked` (progression.go:376), `ListPathBounds`/`ListPathsByUnitType` (path_defs.go:198-219).
- Perk loader: `perk_defs.go` init at :245-313, `perkDefsByID map[string]*PerkDef` (:178). **Duplicate-id silent overwrite at `perk_defs.go:306`.** On-disk = array of `perkEntryJSON` (:187-199) at `.../paths/<path>/perks/<rank>.json`; UnitType/Path/Rank injected from file path.
- **Empty-pool `Intn(0)` guard ALREADY EXISTS** at `perks.go:942` (`assignUnitPerkLocked`) and `perks.go:975-977` (`maybeAssignExtraPerkLocked`). This plan adds a *regression test*, not a guard.
- Perk determinism sort: `perk_defs.go:352-358` (`eligiblePerksForUnitAtRank` sorts by id).
- Persistence template: `unit_persistence.go` (`unitIDPattern = ^[a-z0-9_]+$` :14, `resolveUnitsDir` :27-40, `SaveUnitDef` :44-76, `DeleteUnitOverride` :85-99, `LoadPersistedUnitsIntoOverlay` :136-177). `SkipDir("paths")` at :111-112 and :154-155 STAYS (units editor never owns paths).
- Faction template (fresh-map-merge at read): `faction_persistence.go`, `faction_defs.go:122-152` (`ListFactions` merges embed+overlay into a fresh map each call). `errFactionHasUnits` sentinel pattern at :28-37.
- `editorValidationError` at `item_editor.go:23-33`; `IsEditorValidationError`.
- Editor handlers: `editor_handlers.go` `registerEditorRoutes(mux)` (called router.go:343). POST template :190-211 (`/abilities`), DELETE template :213-237 (`/abilities/`).
- Catalog GET routes: `/catalog/units` (router.go:88-95, already serves `paths`=`ListPathBounds()` + `pathsByUnit`=`ListPathsByUnitType()`), `/catalog/perks` (router.go:99-104, `ListPerkDefs()`). `registerAbilityCatalogRoutes` (router.go:24-54). **Go 1.22 mux panics on duplicate route registration** (documented router.go:113-118) — extend existing GETs, never re-register.
- Startup overlay wiring: `cmd/api/main.go:51-59` (add path/perk loads after `LoadPersistedUnitsIntoOverlay`).
- Vite proxy: `client/src/game-portal/vite.config.ts:55-68`. `/paths` and `/perks` NOT proxied → will 404 in dev unless added.

---

## File Structure

**New files (`server/internal/game/`):**
- `path_persistence.go` — `runtimePaths` overlay, dir resolution, Save/Delete/Load, derived-map rebuild.
- `path_editor.go` — `SaveEditorPath`/`DeleteEditorPath` wrappers + `validatePathChancesLocked` promotion integrity.
- `perk_persistence.go` — `runtimePerks` overlay, Save/Delete/Load, duplicate-id rejection.
- `perk_editor.go` — `SaveEditorPerkPool`/`DeleteEditorPerkPool` wrappers.
- `path_persistence_test.go`, `path_editor_test.go`, `perk_persistence_test.go`, `path_promotion_integrity_test.go`, `perk_wired_test.go` — tests.

**Modified files:**
- `path_defs.go` — extract `registerPathFile`/`validatePathFile`; add `pathCatalogMu` + accessors; rebuild helper.
- `progression.go`, `path_ability_defs.go` — direct derived-map reads → guarded accessors (Task 1).
- `perk_defs.go` — `perkDefsMu` + overlay-aware `perkDefsByID` access; duplicate-id guard; `wiredPerkIDs` + `Wired` on the `/catalog/perks` payload struct.
- `perks.go` — reads of `perkDefsByID` through accessor (only if racy).
- `server/internal/http/editor_handlers.go` — `/paths`, `/paths/`, `/perks`, `/perks/` handlers.
- `server/internal/http/router.go` — `/catalog/paths` GET (full merged path defs).
- `server/cmd/api/main.go` — `LoadPersistedPathsIntoOverlay()` + `LoadPersistedPerksIntoOverlay()`.
- `client/src/game-portal/vite.config.ts` — add `/paths`, `/perks` proxy prefixes.

---

## Task 1: Guard the derived path maps behind an RWMutex accessor layer (no behavior change)

**Why first:** An editor write rebuilds these maps while the sim reads them during rank-up (under `s.mu`, on the tick loop). Direct global reads + a concurrent overlay rebuild = data race. Establish the accessor layer with zero behavior change before anything writes.

**Files:**
- Modify: `server/internal/game/path_defs.go`
- Modify: `server/internal/game/progression.go` (read sites in `pathModifierFor`, `applyRankModifiersLocked`, `rollProgressionPathLocked`)
- Modify: `server/internal/game/path_ability_defs.go:163` (`pathAbilitiesByPath` read)
- Find and modify: `channelLoopRangeForUnitLocked` (`pathChannelLoopByPath` read)
- Test: `server/internal/game/path_defs_accessor_test.go` (new)

- [ ] **Step 1: Write a failing test** asserting accessors return the same values as the embedded catalog for a known path (e.g. `cleric`).

```go
package game

import "testing"

func TestPathAccessorsMatchEmbeddedCatalog(t *testing.T) {
	// cleric is an embedded acolyte path; these must resolve through the guarded accessors.
	if got := pathVisionRangeFor("cleric"); got != pathVisionRangeByPathUnsafe("cleric") {
		t.Fatalf("pathVisionRangeFor(cleric)=%v, want raw map value", got)
	}
	mod, ok := pathModifierLookup(pathModifierKey("cleric", unitRankBronze))
	if !ok {
		t.Fatalf("pathModifierLookup(cleric/bronze) missing")
	}
	if mod.Path != "cleric" {
		t.Fatalf("modifier path = %q, want cleric", mod.Path)
	}
	if got := pathsForUnitType("acolyte"); len(got) == 0 {
		t.Fatalf("pathsForUnitType(acolyte) empty, want cleric/siphoner")
	}
}
```
(`pathVisionRangeByPathUnsafe` is a temporary test-only direct read you delete once accessors exist; or assert against a literal from the catalog — do NOT hardcode a balance number, read it from the map before the mutex lands.)

- [ ] **Step 2: Run to confirm it fails** (accessors undefined): `go test ./internal/game/ -run TestPathAccessors` → FAIL (undefined).

- [ ] **Step 3: Add the mutex + accessors** in `path_defs.go`. Add `var pathCatalogMu sync.RWMutex` guarding ALL 10 derived maps. Add read accessors (each takes `RLock`):

```go
func pathModifierLookup(key string) (pathModifierDef, bool) {
	pathCatalogMu.RLock(); defer pathCatalogMu.RUnlock()
	m, ok := pathModifiersByKey[key]; return m, ok
}
func pathVisionRangeFor(path string) (float64, bool) {
	pathCatalogMu.RLock(); defer pathCatalogMu.RUnlock()
	v, ok := pathVisionRangeByPath[path]; return v, ok
}
// …one per map: pathProjectileFor, pathDamageTypeFor, pathAttackTypeFor,
//   pathProjectileScaleFor, pathAbilitiesFor (return a COPY), pathChannelLoopFor,
//   pathBoundsFor, pathsForUnitType (return a COPY).
```
`pathAbilitiesFor` and `pathsForUnitType` must return **copies** (callers must not mutate shared slices). `ListPathBounds`/`ListPathsByUnitType` take `RLock` too.

- [ ] **Step 4: Replace direct reads** at the sites listed above with the accessors. In `applyRankModifiersLocked`, the block reading `pathVisionRangeByPath[...]`, `pathProjectileByPath[...]`, etc. becomes `pathVisionRangeFor(...)`, etc. `pathModifierFor` uses `pathModifierLookup`. `rollProgressionPathLocked` uses `pathsForUnitType`. Leave `init()`'s writes as direct map assignment (init is single-threaded, pre-serving).

- [ ] **Step 5: Run the full game test suite** — nothing should change: `go test ./internal/game/` → PASS (all existing progression/rank-up tests green). Then `go vet ./...` and `go build ./...`.

- [ ] **Step 6: Commit.**

---

## Task 2: Extract error-returning path validation + registration from the panic init

**Files:**
- Modify: `server/internal/game/path_defs.go`
- Test: `server/internal/game/path_validate_test.go` (new)

- [ ] **Step 1: Write failing tests** for a pure `validatePathFile(file *pathCatalogFile, pathKey string) error`: valid `cleric`-shaped file passes; each rejection has a case — `path` empty, `path != pathKey`, unregistered projectile, invalid damageType, `projectileScale < 0`, `channelLoop.end < start`, empty/unregistered ability id, unknown rank name. Assert the error **message** names the field (these become inline editor errors).

- [ ] **Step 2: Run → FAIL** (function undefined).

- [ ] **Step 3: Implement `validatePathFile`** — lift every validation currently expressed as a `panic(...)` in `init()` (:307-390) into `error` returns with the same messages (minus the `rel:` filename prefix — the caller adds context). Then implement:

```go
// registerPathFileLocked validates the parsed file and writes it into every
// derived map. Caller holds pathCatalogMu.Lock(). Used by the embed loader
// (wraps err in panic) and the overlay rebuild (surfaces err).
func registerPathFileLocked(unitKey string, file *pathCatalogFile) error { … }
```
Move the map-population logic (:316-430) into `registerPathFileLocked`. `init()` becomes: walk → unmarshal → `pathCatalogMu.Lock()` → for each file `if err := registerPathFileLocked(unitKey, &file); err != nil { panic(rel + ": " + err.Error()) }` → keep the `pathChances` cross-validation panic block as-is (it stays fail-loud at boot). **Behavior identical; fail-loud preserved.**

- [ ] **Step 4: Run** the new tests + the full game suite → PASS. `go vet`, `go build`.

- [ ] **Step 5: Commit.**

---

## Task 3: `path_persistence.go` — writable overlay + Save/Delete/Load with derived-map rebuild

**Files:**
- Create: `server/internal/game/path_persistence.go`
- Test: `server/internal/game/path_persistence_test.go`

**Design:** One canonical overlay `runtimePaths map[string]*pathCatalogFile` (keyed by path id) + `runtimePathUnitByPath map[string]string` (path id → owning unit type, needed for rebuild + dir). On any write, **rebuild all 10 derived maps from scratch** (embed-base snapshot + overlay on top) under `pathCatalogMu.Lock()` — the fresh-map-then-swap pattern, mirroring how `ListFactions` rebuilds. To get the embed base, capture it once at init into an unexported `embeddedPathFiles map[string]*pathCatalogFile` (+ `embeddedPathUnit map[string]string`) so rebuild doesn't re-walk the FS.

- [ ] **Step 1: Write failing round-trip test** on an isolated `UNIT_CATALOG_DIR` (`t.TempDir()`, `t.Setenv`): `SavePathDef` a new path `zealot` under `acolyte` with a bronze rank → `pathModifierLookup("zealot/bronze")` reflects it and `pathsForUnitType("acolyte")` includes `zealot` → `DeletePathOverride("zealot")` → both revert to embed (no `zealot`). Also: overlay a path id that shadows an embed path and assert overlay wins.

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Implement.** Capture `embeddedPathFiles` in `init()` before/as it registers. Then:

```go
var (
	runtimePathsMu sync.RWMutex // guards runtimePaths; distinct from pathCatalogMu (derived maps)
	runtimePaths   = map[string]*pathCatalogFile{}
	runtimePathUnit = map[string]string{}
)

// resolvePathsDir reuses the units tree (paths live under it).
func resolvePathsDir() (string, error) { return resolveUnitsDir() }

func SavePathDef(unitType string, file *pathCatalogFile) error {
	// id-pattern guard on unitType + file.Path (traversal guard)
	// validatePathFile(file, file.Path)
	// resolve <dir>/<faction>/<unit>/paths/<path>/<path>.json  (faction from owning unit def)
	// MkdirAll, MarshalIndent, WriteFile
	// runtimePaths[file.Path] = file; runtimePathUnit[file.Path] = unitType
	// rebuildDerivedPathMaps()
}

func DeletePathOverride(pathID string) (existed bool, err error) {
	// id-pattern guard; remove <path>/ dir (incl. its perks/ subdir); drop overlay entry; rebuild
}

// rebuildDerivedPathMaps rebuilds all 10 derived maps from embeddedPathFiles +
// runtimePaths under pathCatalogMu.Lock() (fresh maps, then assigned). Overlay wins.
func rebuildDerivedPathMaps() { … }

func LoadPersistedPathsIntoOverlay() { /* walk writable tree's */ }
```
The owning faction comes from the unit def (`getUnitDef(unitType).Faction`). Delete removes the whole `<path>/` directory (its perks live inside). Rebuild MUST re-run `validatePathFile` on overlay entries and skip (log) invalid ones so a hand-edited bad file can't wedge the rebuild.

**Note on `pathChances` cross-validation:** the boot-time panic block in `init()` stays for embed integrity. The *editor* enforces integrity via Task 4's `validatePathChancesLocked` (rejects at save, never writes). Do NOT panic inside `rebuildDerivedPathMaps` — a rebuild happens at runtime; a panic there crashes the live server.

- [ ] **Step 4: Run** round-trip + overlay-wins tests + full suite → PASS. `go vet`, `go build`.

- [ ] **Step 5: Commit.**

---

## Task 4: `path_editor.go` — editor wrappers + promotion integrity (§9.1)

**Files:**
- Create: `server/internal/game/path_editor.go`
- Test: `server/internal/game/path_promotion_integrity_test.go`

- [ ] **Step 1: Write failing tests** for each §9.1 rule (messages must name the thing + say what to do):
  1. Saving a unit whose `pathChances` references a non-existent path → rejected, message names the path + unit.
  2. A path whose `path` field ≠ directory name → rejected on path save.
  3. A path with an empty `ranks` table → rejected when referenced by a unit's `pathChances` (a promotion into it gains nothing).
  4. Weights `< 0` or summing to `0` → rejected on unit save.
  5. Deleting a path still referenced by any unit's `pathChances` → rejected, lists referencing units.
  6. **Boot-panic proof:** drive create-path → save-unit-with-pathChances → attempt-delete-path against a temp `UNIT_CATALOG_DIR`, then re-run the catalog loader (fresh registration over that dir) and assert no panic.

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Implement.**

```go
func SaveEditorPath(unitType string, file *pathCatalogFile) error {
	if !unitIDPattern.MatchString(unitType) { return editorValidationError{…} }
	if !unitIDPattern.MatchString(file.Path) { return editorValidationError{…} }
	if err := validatePathFile(file, file.Path); err != nil { return editorValidationError{err} }
	return SavePathDef(unitType, file)
}

func DeleteEditorPath(pathID string) (existed bool, err error) {
	if refs := unitsReferencingPath(pathID); len(refs) > 0 {
		return false, editorValidationError{fmt.Errorf(
			"path %q is still referenced by pathChances on: %s. Remove those rows first.",
			pathID, strings.Join(refs, ", "))}
	}
	return DeletePathOverride(pathID)
}

// validatePathChancesLocked enforces §9.1 on EVERY unit save. Called from
// SaveEditorUnit (unit_editor.go). Returns editorValidationError on any dangling/
// misconfigured reference. Reads the merged path registry via accessors.
func validatePathChancesLocked(def *UnitDef) error { … } // the 4-row table from §9.1
```
Wire `validatePathChancesLocked(&unit)` into `SaveEditorUnit` (`unit_editor.go`) BEFORE it calls `SaveUnitDef`. `unitsReferencingPath` scans merged unit defs (`ListUnitDefs`) for the id in `PathChances`.

**Ordering guarantee (§9.1):** the "Add Path" UX writes the path file first, then the pathChances row — the intermediate state (path exists, unreferenced) is valid. The reverse (reference a not-yet-created path) is exactly what `validatePathChancesLocked` rejects. Do not add any endpoint that writes a pathChances row before the path.

- [ ] **Step 4: Run** integrity tests (incl. boot-panic proof) + full suite → PASS. `go vet`, `go build`.

- [ ] **Step 5: Commit.**

---

## Task 5: `perk_persistence.go` — writable perk overlay + duplicate-id rejection

**Files:**
- Create: `server/internal/game/perk_persistence.go`, `server/internal/game/perk_editor.go`
- Test: `server/internal/game/perk_persistence_test.go`

- [ ] **Step 1: Write failing tests:** save a `perks/<rank>.json` array for `(acolyte, cleric, bronze)` → `perkDefsByID` reflects the ids (with UnitType/Path/Rank injected) → delete reverts to embed. **Duplicate-id rejection:** saving a pool containing an id already owned by a *different* (unit,path,rank) → `editorValidationError` naming the other owner. Overlay-wins for a same-id edit within the same location.

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Implement.** Mirror Task 3. Add `perkDefsMu sync.RWMutex` guarding `perkDefsByID`; convert the (few) read sites in `perks.go`/`perk_defs.go` that run concurrently with a possible editor write to take `RLock` (eligibility scans run under `s.mu` on the tick loop — a concurrent editor rebuild races them, so this guard is required, same rationale as Task 1).

```go
func SavePerkPool(unitType, path, rank string, entries []perkEntryJSON) error {
	// id-pattern guard on unitType/path/rank + each entry.ID
	// validate rank name; validate no duplicate id vs OTHER locations (merged registry)
	// write <dir>/<faction>/<unit>/paths/<path>/perks/<rank>.json
	// rebuild perkDefsByID from embeddedPerks + runtimePerks under perkDefsMu.Lock()
}
func DeletePerkPool(unitType, path, rank string) (existed bool, err error) { … }
func LoadPersistedPerksIntoOverlay() { … }
```
`rebuildPerkRegistry` reruns the same UnitType/Path/Rank injection the init walk does, and the **duplicate-id detection** (spec §7.2, fixing the `perk_defs.go:306` silent overwrite): during rebuild, if two entries across the merged set share an id, that's a rejected save — surface it as the reason the save failed rather than silently clobbering. Embedded duplicates (none exist today) would still panic at boot; editor saves reject.

- [ ] **Step 4: Run** perk round-trip + duplicate-id tests + full suite → PASS. `go vet`, `go build`.

- [ ] **Step 5: Commit.**

---

## Task 6: Wired-vs-inert perk flag (§7.3)

**Files:**
- Create: `server/internal/game/perk_wired.go`, `server/internal/game/perk_wired_test.go`
- Modify: `perk_defs.go` (`ListPerkDefs` payload gains `Wired bool`)

- [ ] **Step 1: Write the coverage test.** Assert `wiredPerkIDs` exactly matches the set of ids that have a Go handler. Since Go `switch` labels aren't reflectable, the test enforces the set against the **embedded catalog**: every embedded perk id whose behavior is implemented must be in `wiredPerkIDs`, and every id in `wiredPerkIDs` must exist as an embedded perk (no typos). Also assert `ListPerkDefs()` sets `Wired=true` for a known wired id (e.g. `bloodlust`) and `Wired=false` for a config-only id if one exists (else document that all shipped perks are wired).

```go
func TestWiredPerkIDsCoverEmbeddedCatalog(t *testing.T) {
	for id := range wiredPerkIDs {
		if _, ok := perkDefsByID[id]; !ok {
			t.Errorf("wiredPerkIDs lists %q which is not a catalog perk", id)
		}
	}
}
```

- [ ] **Step 2: Run → FAIL** (`wiredPerkIDs` undefined).

- [ ] **Step 3: Implement.** Create `wiredPerkIDs = map[string]struct{}{…}` — the hand-maintained set of every perk id that appears as a `case` label in the dispatch switches (files listed in recon: `perks.go`, `perks_attack.go`, `perks_defense.go`, `perks_marksman.go`, `perks_trapper.go`, `perks_crit.go`, `perks_movement.go`, `perks_siphoner.go`, `perks_arch_mage.go`, `perks_cleric.go`, `perks_auras.go`, `perks_vision.go`). Enumerate by grepping `case "` in those files. Add `Wired bool` to the `ListPerkDefs` payload struct; set `Wired = wiredPerkIDs[def.ID]`. Add a doc comment: this set is hand-maintained until the perk redesign (§7.4) makes behavior data-driven; a new perk id with no Go handler ships `Wired=false` so the editor can label it "inert".

- [ ] **Step 4: Run** coverage test + full suite → PASS. `go vet`, `go build`.

- [ ] **Step 5: Commit.**

---

## Task 7: Empty-pool safety + integrity regression tests

**Files:**
- Test: `server/internal/game/perk_empty_pool_test.go` (new)

- [ ] **Step 1: Write the test.** The guard already exists (`perks.go:942`) — this test *pins* it so a future refactor can't remove it. Construct a `GameState`, spawn a unit onto an editor-shaped path with **no perk files**, rank it to Bronze via the normal rank-up flow, assert: no panic, `unit.PerkIDs` unchanged (nothing granted), sim continues. Use the seeded RNG path (`assignUnitPerkLocked`), not a direct pool index.

- [ ] **Step 2: Run → PASS immediately** (guard present). Confirm it genuinely exercises the empty-pool branch (temporarily stub the guard locally to see it panic, then restore — do not commit the stub).

- [ ] **Step 3: Commit.**

---

## Task 8: HTTP routes + startup wiring + Vite proxy

**Files:**
- Modify: `server/internal/http/editor_handlers.go` (`/paths`, `/paths/`, `/perks`, `/perks/`)
- Modify: `server/internal/http/router.go` (`/catalog/paths` full merged GET)
- Modify: `server/cmd/api/main.go` (load calls)
- Modify: `client/src/game-portal/vite.config.ts` (proxy)
- Test: `server/internal/http/path_perk_routes_test.go` (new)

- [ ] **Step 1: Write failing handler tests** against a bare `*http.ServeMux` (mirror ability-route tests): `POST /paths` with a valid body → 201; with a dangling-pathChances-inducing body → 400 `{error:"validation_failed"}`; `DELETE /paths/{id}` of a referenced path → 400; `GET /catalog/paths` returns full merged path defs (ranks + overlay). `POST /perks` round-trip → 201; duplicate-id → 400.

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Implement.**
  - In `registerEditorRoutes`: add `/paths` (POST), `/paths/` (DELETE), `/perks` (POST), `/perks/` (DELETE) following the exact `/abilities` POST/DELETE template (validate → `IsEditorValidationError` → 400 `validation_failed` → 500 `save_failed`). POST `/paths` body: `{ unit: string, path: pathCatalogFile }`. DELETE `/paths/{id}`. POST `/perks` body: `{ unit, path, rank, perks: []perkEntryJSON }`. DELETE `/perks/{unit}/{path}/{rank}`.
  - In `router.go`: add `/catalog/paths` GET returning `game.ListPathDefsFull()` (new accessor returning the merged embed+overlay `pathCatalogFile`s — ranks, overlay fields, everything; the existing `/catalog/units` `paths` field stays bounds-only for back-compat). **Do NOT re-register `/catalog/units` or `/catalog/perks`** (Go 1.22 dup-panic). Confirm `/catalog/paths` isn't already registered anywhere.
  - In `main.go:51-59`: add `game.LoadPersistedPathsIntoOverlay()` and `game.LoadPersistedPerksIntoOverlay()` after `LoadPersistedUnitsIntoOverlay()`.
  - In `vite.config.ts` proxy block: add `'/paths'` and `'/perks'` (same shape as `/units`). Note: `/catalog/paths` is already covered by the `/catalog` prefix.

- [ ] **Step 4: Run** handler tests + full server suite + `go vet ./...` + `go build ./...` → PASS. Then from `client/src/game-portal`: `npx vue-tsc -b` (proxy change is config-only; typecheck should stay green).

- [ ] **Step 5: Commit.**

---

## Final review

After all 8 tasks: dispatch a final code reviewer over the whole Phase 5a diff. Verify against §9.1 (no editor-driven sequence can produce a boot panic — the Task 4 boot-panic proof is the anchor), §13 global constraints (determinism preserved, no sim behavior change, `Locked` discipline), and that the accessor layer left every existing progression test green. Then hand off to Phase 5b (client UI).
