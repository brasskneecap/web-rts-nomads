# Item Editor — Plan A: Server Persistence + API — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Writable overlays for items/recipes/lists/loot-tables (mirroring `maps.go`) plus editor HTTP endpoints (`POST /items`, `DELETE /items/{id}`, icon upload/serve, `/catalog/procs`) — fully curl-testable with no UI.

**Architecture:** Each catalog gains a `runtime*` overlay map + RWMutex + `resolve*Dir()` (env override, dev default = embedded source dir) + `LoadPersisted*IntoOverlay()` at startup + overlay-wins readers — the exact `maps.go:319-743` pattern. A `game.SaveEditorItem` orchestrator applies one editor save transactionally-ish (item def → recipe → list memberships → loot-table membership). HTTP routes follow `/maps` handler conventions with `registerEditorRoutes(mux)`.

**Tech Stack:** Go 1.22 stdlib only (`net/http`, `encoding/json`, `image/png`, `os`, `sync`), module `webrts/server` at root `server/`.

**Spec:** `docs/superpowers/specs/2026-07-09-item-editor-design.md` (Plan A sections)

## Global Constraints

- Branch: `ui-item-editor` (already created; verify, never switch).
- Env overrides + dev defaults: `ITEM_CATALOG_DIR` → `<cwd>/internal/game/catalog/items`; `RECIPE_CATALOG_DIR` → `.../catalog/recipes`; `NEUTRAL_GROUPS_DIR` → `.../catalog/neutral_groups` (same resolution shape as `resolveMapsDir`, maps.go:609).
- Overlay precedence: runtime/persisted wins over embed, in the READERS (maps.go pattern). Overlay maps guarded by their own RWMutex (they mutate at runtime).
- Persisted-file loading recovers per-file panics into logged skips (`slog.Warn`) — one bad file never crashes startup (maps.go:664-743 pattern).
- Validation reuses the embed-load validators verbatim: `validateItemDef`, `validateRecipeDef`. Item id: `^[a-z0-9_]+$`. Id matching an embedded item = intentional override.
- **Disk files keep the AUTHORED form.** `ItemOnHitProc.MarshalJSON` (items.go:126-142) enriches wire output with resolved proc payload — writing that to disk would freeze resolved values as overrides. Disk marshaling MUST bypass it (Task 1 defines `itemProcDisk`).
- Icon uploads: PNG only (decode-sniffed), ≤ 256 KB, stored `<items dir>/_icons/<id>.png`; `_icons/` and `lists/` skipped by all item-def walks. Upload ALWAYS sets the item's iconKey to the item id.
- Loot "weight" = d100 range width; the item joins the merchant subtable matching its category (Weapon→merchant_weapons, Armor/Shield→merchant_armor, Accessory→merchant_accessories, Consumable→merchant_potions, other→merchant_accessories); the subtable's ranges renormalize proportionally to 1..100 on every membership change.
- HTTP: routes in a new `registerEditorRoutes(mux *http.ServeMux)` (convention: profile_handlers.go:50); reuse `writeJSON`/`writeJSONError` (profile_handlers.go:17-27); ID-in-path via `strings.TrimPrefix`+`strings.Cut` (router.go:257 pattern); CORS gains `DELETE` (router.go:524).
- Validation failures → 400 `{"error":"validation_failed","message":"<err>"}` (single message; the spec's "field errors" is satisfied by Plan B displaying the message — spec amended).
- All Go commands from `server/`. Gates: `go vet ./...`, `go build ./...`, `go test ./...` — no NEW failures (known flaky on main: cmd/api TestServerReadyLineAndStdinShutdown).
- gofmt CRLF quirk on this checkout: use vet/build as format gates.
- Tests follow map_persistence_test.go patterns: call `loadPersisted*FromDir(t.TempDir())` directly (no env mutation); `t.Cleanup` deletes overlay entries under lock so nothing leaks across tests.
- Commit messages: short imperative.

---

### Task 1: Item-def overlay — dirs, disk form, load, readers, save, delete

**Files:**
- Create: `server/internal/game/item_persistence.go`
- Modify: `server/internal/game/items.go` (walk skips `_icons/`; `getItemDef`/`ListItemDefs` overlay-aware; `ItemDef` gains `Overridden`)
- Test: `server/internal/game/item_persistence_test.go`

**Interfaces:**
- Consumes: `ItemDef`, `validateItemDef`, `itemCatalogSingleton`, `itemListsSubdir` (items.go).
- Produces: `resolveItemsDir() (string, error)`; `runtimeItemsMu sync.RWMutex` + `runtimeItems map[string]*ItemDef`; `LoadPersistedItemsIntoOverlay()`; `loadPersistedItemsFromDir(dir string) int`; `SaveItemDef(def *ItemDef) error`; `DeleteItemOverride(id string) (existed bool, err error)`; `itemIsEmbedded(id string) bool`; `renderItemDefJSON(def *ItemDef) ([]byte, error)` (authored disk form); `ItemDef.Overridden bool` json `overridden,omitempty`. Tasks 2/5/6 use these.

- [ ] **Step 1: Write the failing tests**

`server/internal/game/item_persistence_test.go`:
```go
package game

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// itemOverlayCleanup removes an overlay entry after the test (process-global).
func itemOverlayCleanup(t *testing.T, id string) {
	t.Helper()
	t.Cleanup(func() {
		runtimeItemsMu.Lock()
		delete(runtimeItems, id)
		runtimeItemsMu.Unlock()
	})
}

// TestItemOverlay_LoadFromDirOverlaysDisk: an item JSON in the writable dir
// becomes visible through getItemDef/ListItemDefs, flagged Overridden.
func TestItemOverlay_LoadFromDirOverlaysDisk(t *testing.T) {
	const id = "test_overlay_item"
	itemOverlayCleanup(t, id)
	def := &ItemDef{ID: id, DisplayName: "Overlay Item", IconKey: id, Kind: ItemKindEquipment, Tier: ItemTierCommon, SlotKind: "any", Modifiers: &ItemModifiers{Armor: 3}}
	raw, err := renderItemDefJSON(def)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, id+".json"), raw, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if _, present := getItemDef(id); present {
		t.Fatalf("item %q unexpectedly present before load", id)
	}
	if n := loadPersistedItemsFromDir(dir); n < 1 {
		t.Fatalf("expected >=1 item loaded, got %d", n)
	}
	got, ok := getItemDef(id)
	if !ok {
		t.Fatal("overlay item not visible after load")
	}
	if !got.Overridden {
		t.Error("overlay item must be flagged Overridden")
	}
	if got.Modifiers == nil || got.Modifiers.Armor != 3 {
		t.Errorf("payload lost in round-trip: %+v", got.Modifiers)
	}
	// ListItemDefs contains it exactly once.
	count := 0
	for _, d := range ListItemDefs() {
		if d.ID == id {
			count++
		}
	}
	if count != 1 {
		t.Errorf("ListItemDefs contains overlay item %d times, want 1", count)
	}
}

// TestItemOverlay_OverlayWinsOverEmbed: overriding a shipped item id replaces
// it in reads; DeleteItemOverride restores the embedded version.
func TestItemOverlay_OverlayWinsOverEmbed(t *testing.T) {
	const id = "leather_armor" // shipped item
	itemOverlayCleanup(t, id)
	embedded, ok := getItemDef(id)
	if !ok {
		t.Skip("leather_armor not in catalog")
	}
	override := *embedded
	override.DisplayName = "EDITED Leather"
	override.Overridden = true
	runtimeItemsMu.Lock()
	runtimeItems[id] = &override
	runtimeItemsMu.Unlock()

	got, _ := getItemDef(id)
	if got.DisplayName != "EDITED Leather" {
		t.Fatalf("overlay must win: got %q", got.DisplayName)
	}
	runtimeItemsMu.Lock()
	delete(runtimeItems, id)
	runtimeItemsMu.Unlock()
	got, _ = getItemDef(id)
	if got.DisplayName == "EDITED Leather" {
		t.Fatal("embed must be restored after overlay removal")
	}
}

// TestRenderItemDefJSON_AuthoredFormNotWireForm guards the disk format: a def
// with a proc REFERENCE must serialize the reference (effect id), never the
// resolved wire payload (damageType/projectileID), which would freeze resolved
// values as overrides on reload.
func TestRenderItemDefJSON_AuthoredFormNotWireForm(t *testing.T) {
	def := &ItemDef{ID: "x", DisplayName: "X", IconKey: "x", Kind: ItemKindEquipment, Tier: ItemTierRare, SlotKind: "any",
		OnHitProc: &ItemOnHitProc{Chance: 0.1, Effect: "fire_bolt_ignite"}}
	raw, err := renderItemDefJSON(def)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	s := string(raw)
	if !strings.Contains(s, `"effect": "fire_bolt_ignite"`) {
		t.Errorf("disk form must keep the effect reference:\n%s", s)
	}
	if strings.Contains(s, "damageType") || strings.Contains(s, "projectileID") {
		t.Errorf("disk form must NOT contain resolved wire fields:\n%s", s)
	}
	// And the disk form round-trips through the normal parser.
	var back ItemDef
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("round-trip unmarshal: %v", err)
	}
	if back.OnHitProc == nil || back.OnHitProc.Effect != "fire_bolt_ignite" || back.OnHitProc.Damage != 0 {
		t.Errorf("round-trip drifted: %+v", back.OnHitProc)
	}
}

// TestItemOverlay_SkipsMalformedAndIconsDir: bad files are skipped without
// panic; _icons/ and lists/ are not parsed as defs.
func TestItemOverlay_SkipsMalformedAndIconsDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"_icons", "lists"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, sub, "junk.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if n := loadPersistedItemsFromDir(dir); n != 0 {
		t.Fatalf("expected 0 loaded from malformed/skipped dirs, got %d", n)
	}
}

// TestSaveItemDef_WritesTierPathAndRegistersLive: SaveItemDef writes to
// <dir>/<category>/<tier>/<id>.json and the def is immediately visible.
func TestSaveItemDef_WritesTierPathAndRegistersLive(t *testing.T) {
	const id = "test_saved_item"
	itemOverlayCleanup(t, id)
	dir := t.TempDir()
	t.Setenv("ITEM_CATALOG_DIR", dir)
	def := &ItemDef{ID: id, DisplayName: "Saved", IconKey: id, Kind: ItemKindEquipment, Tier: ItemTierUncommon, Category: "Shield", SlotKind: "any", Modifiers: &ItemModifiers{Armor: 7}}
	if err := SaveItemDef(def); err != nil {
		t.Fatalf("save: %v", err)
	}
	want := filepath.Join(dir, "shields", "uncommon", id+".json")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected file at %s: %v", want, err)
	}
	if got, ok := getItemDef(id); !ok || !got.Overridden || got.Modifiers.Armor != 7 {
		t.Fatalf("live registration failed: ok=%v def=%+v", ok, got)
	}
	// Invalid defs are rejected before any write.
	bad := &ItemDef{ID: "Bad ID!", DisplayName: "x", IconKey: "x"}
	if err := SaveItemDef(bad); err == nil {
		t.Error("expected id-format validation error")
	}
}

// TestDeleteItemOverride_RemovesFileAndOverlay.
func TestDeleteItemOverride_RemovesFileAndOverlay(t *testing.T) {
	const id = "test_delete_item"
	itemOverlayCleanup(t, id)
	dir := t.TempDir()
	t.Setenv("ITEM_CATALOG_DIR", dir)
	def := &ItemDef{ID: id, DisplayName: "Doomed", IconKey: id, Kind: ItemKindEquipment, Tier: ItemTierCommon, SlotKind: "any"}
	if err := SaveItemDef(def); err != nil {
		t.Fatalf("save: %v", err)
	}
	existed, err := DeleteItemOverride(id)
	if err != nil || !existed {
		t.Fatalf("delete: existed=%v err=%v", existed, err)
	}
	if _, ok := getItemDef(id); ok {
		t.Fatal("editor-created item must vanish after delete")
	}
	if existed, _ := DeleteItemOverride(id); existed {
		t.Fatal("second delete must report not-existed")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd server && go test ./internal/game/ -run "TestItemOverlay_|TestRenderItemDefJSON_|TestSaveItemDef_|TestDeleteItemOverride_" -v`
Expected: FAIL to compile — `undefined: renderItemDefJSON`, `runtimeItems`, etc.

- [ ] **Step 3: Implement `item_persistence.go`**

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

// ─── Writable item catalog overlay ───────────────────────────────────────────
//
// Mirrors the map persistence system (maps.go): editor saves write JSON files
// into a writable dir and register into an in-memory overlay that WINS over
// the embedded catalog in every reader. Loaded once at startup by
// LoadPersistedItemsIntoOverlay; per-file failures are logged skips.

var (
	runtimeItemsMu sync.RWMutex
	runtimeItems   = map[string]*ItemDef{}
)

// itemIDPattern is the editor's id discipline (embed files predate it and are
// exempt — they were validated by their own loaders).
var itemIDPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

// itemIconsSubdirName holds uploaded icon PNGs inside the items dir; it is
// skipped by every def walk (like lists/).
const itemIconsSubdirName = "_icons"

// resolveItemsDir mirrors resolveMapsDir: env override, else the dev source
// catalog dir so editor saves land as ordinary git-visible changes.
func resolveItemsDir() (string, error) {
	if dir := os.Getenv("ITEM_CATALOG_DIR"); dir != "" {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cwd, "internal", "game", "catalog", "items")
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	return "", fmt.Errorf("items directory not found at %s; set ITEM_CATALOG_DIR env var to override", dir)
}

// itemProcDisk strips ItemOnHitProc's wire-enrichment MarshalJSON (a defined
// type has no methods), so DISK files keep the authored reference+overrides
// form. Writing the enriched wire form would re-read resolved values as
// frozen overrides — see the items.go MarshalJSON doc comment.
type itemProcDisk ItemOnHitProc

// itemDefDisk shadows the two proc fields with the method-less type; every
// other field marshals identically to ItemDef.
type itemDefDisk struct {
	ItemDef
	Overridden   bool          `json:"overridden,omitempty"` // never persisted (always zero on write path)
	OnHitProc    *itemProcDisk `json:"onHitProc,omitempty"`
	OnStruckProc *itemProcDisk `json:"onStruckProc,omitempty"`
}

// renderItemDefJSON serializes a def in the AUTHORED form for disk.
func renderItemDefJSON(def *ItemDef) ([]byte, error) {
	d := itemDefDisk{ItemDef: *def}
	d.ItemDef.Overridden = false
	if def.OnHitProc != nil {
		p := itemProcDisk(*def.OnHitProc)
		d.OnHitProc = &p
	}
	if def.OnStruckProc != nil {
		p := itemProcDisk(*def.OnStruckProc)
		d.OnStruckProc = &p
	}
	// Zero the embedded copies so the shadow fields are the only emitters.
	d.ItemDef.OnHitProc = nil
	d.ItemDef.OnStruckProc = nil
	return json.MarshalIndent(d, "", "  ")
}

// itemCategorySubdir maps an item's category to its catalog subdirectory,
// matching the embedded layout. Unknown categories go under misc/.
func itemCategorySubdir(def *ItemDef) string {
	switch def.Category {
	case "Weapon":
		return "weapons"
	case "Armor":
		return "armor"
	case "Shield":
		return "shields"
	case "Accessory":
		return "accessories"
	case "Consumable":
		return "consumables"
	default:
		return "misc"
	}
}

// SaveItemDef validates, writes <dir>/<category>/<tier>/<id>.json in authored
// form, and registers the def into the overlay (live without restart).
func SaveItemDef(def *ItemDef) error {
	if !itemIDPattern.MatchString(def.ID) {
		return fmt.Errorf("item id %q must match %s", def.ID, itemIDPattern)
	}
	if err := validateItemDef(def); err != nil {
		return err
	}
	dir, err := resolveItemsDir()
	if err != nil {
		return err
	}
	tier := string(def.Tier)
	if tier == "" {
		tier = string(ItemTierCommon)
	}
	outDir := filepath.Join(dir, itemCategorySubdir(def), tier)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	raw, err := renderItemDefJSON(def)
	if err != nil {
		return err
	}
	// Remove any previous override saved under a different category/tier so an
	// edited item never exists at two paths.
	removeItemOverrideFiles(dir, def.ID)
	if err := os.WriteFile(filepath.Join(outDir, def.ID+".json"), raw, 0o644); err != nil {
		return err
	}
	reg := *def
	reg.Overridden = true
	runtimeItemsMu.Lock()
	runtimeItems[def.ID] = &reg
	runtimeItemsMu.Unlock()
	return nil
}

// itemIsEmbedded reports whether id ships in the embedded catalog.
func itemIsEmbedded(id string) bool {
	_, ok := itemCatalogSingleton[id]
	return ok
}

// DeleteItemOverride removes the writable override file(s) + overlay entry.
// For embedded items this is reset-to-default; for editor-created items it is
// a true delete. existed reports whether any override was found.
func DeleteItemOverride(id string) (existed bool, err error) {
	dir, derr := resolveItemsDir()
	if derr != nil {
		return false, derr
	}
	removed := removeItemOverrideFiles(dir, id)
	runtimeItemsMu.Lock()
	_, inOverlay := runtimeItems[id]
	delete(runtimeItems, id)
	runtimeItemsMu.Unlock()
	return removed || inOverlay, nil
}

// removeItemOverrideFiles deletes every <id>.json def file under dir (any
// category/tier), skipping lists/ and _icons/. Returns whether any was removed.
func removeItemOverrideFiles(dir, id string) bool {
	removed := false
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "lists" || d.Name() == itemIconsSubdirName {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == id+".json" {
			if rerr := os.Remove(path); rerr == nil {
				removed = true
			}
		}
		return nil
	})
	return removed
}

// LoadPersistedItemsIntoOverlay loads editor-saved items at startup. Mirrors
// LoadPersistedMapsIntoOverlay: best-effort, never fatal.
func LoadPersistedItemsIntoOverlay() {
	dir, err := resolveItemsDir()
	if err != nil {
		slog.Info("persisted items: no writable items dir; using embedded catalog only", "err", err)
		return
	}
	if n := loadPersistedItemsFromDir(dir); n > 0 {
		slog.Info("persisted items: overlaid on embedded catalog", "count", n, "dir", dir)
	}
}

// loadPersistedItemsFromDir walks dir for item defs (skipping lists/ and
// _icons/), overlaying each valid one. Files identical to their embedded
// counterpart are still overlaid (harmless; dev dir IS the embed source).
func loadPersistedItemsFromDir(dir string) int {
	loaded := 0
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "lists" || d.Name() == itemIconsSubdirName {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			return nil
		}
		def, perr := parsePersistedItemFile(path)
		if perr != nil {
			slog.Warn("persisted items: skipped file", "file", d.Name(), "err", perr)
			return nil
		}
		runtimeItemsMu.Lock()
		runtimeItems[def.ID] = def
		runtimeItemsMu.Unlock()
		loaded++
		return nil
	})
	return loaded
}

func parsePersistedItemFile(path string) (def *ItemDef, err error) {
	defer func() {
		if r := recover(); r != nil {
			def = nil
			err = fmt.Errorf("invalid item: %v", r)
		}
	}()
	raw, rerr := os.ReadFile(path)
	if rerr != nil {
		return nil, rerr
	}
	var d ItemDef
	if uerr := json.Unmarshal(raw, &d); uerr != nil {
		return nil, uerr
	}
	if d.ID == "" {
		return nil, fmt.Errorf("item has empty id")
	}
	if verr := validateItemDef(&d); verr != nil {
		return nil, verr
	}
	d.Overridden = true
	return &d, nil
}
```

- [ ] **Step 4: Make the readers overlay-aware + add `Overridden` (items.go)**

`ItemDef` gains (after `OnStruckProc`):
```go
	// Overridden marks a def sourced from the writable editor overlay rather
	// than the embedded catalog. Runtime-only provenance for the editor UI —
	// never authored in embed files (the disk writer always strips it).
	Overridden bool `json:"overridden,omitempty"`
```
`getItemDef` becomes:
```go
func getItemDef(id string) (*ItemDef, bool) {
	runtimeItemsMu.RLock()
	if def, ok := runtimeItems[id]; ok {
		runtimeItemsMu.RUnlock()
		return def, true
	}
	runtimeItemsMu.RUnlock()
	def, ok := itemCatalogSingleton[id]
	return def, ok
}
```
`ListItemDefs` becomes (merge pattern of ListMapCatalogSummaries):
```go
func ListItemDefs() []*ItemDef {
	merged := make(map[string]*ItemDef, len(itemCatalogSingleton))
	for id, def := range itemCatalogSingleton {
		merged[id] = def
	}
	runtimeItemsMu.RLock()
	for id, def := range runtimeItems {
		merged[id] = def
	}
	runtimeItemsMu.RUnlock()
	defs := make([]*ItemDef, 0, len(merged))
	for _, def := range merged {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}
```
`loadItemCatalog`'s walk gains the `_icons` skip next to the lists skip:
```go
		if d.IsDir() {
			if path == itemListsSubdir || d.Name() == itemIconsSubdirName {
				return fs.SkipDir // lists load separately; _icons holds uploaded PNGs
			}
			return nil
		}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd server && go test ./internal/game/ -run "TestItemOverlay_|TestRenderItemDefJSON_|TestSaveItemDef_|TestDeleteItemOverride_" -v`
Expected: PASS.
Run: `cd server && go build ./... && go test ./internal/game/ 2>&1 | grep -E "^(--- FAIL|FAIL|ok)"`
Expected: `ok` only.

- [ ] **Step 6: Commit**

```bash
git add server/internal/game/item_persistence.go server/internal/game/item_persistence_test.go server/internal/game/items.go
git commit -m "Add writable item-def overlay: save/load/delete mirroring map persistence"
```

---

### Task 2: Recipe + item-list + recipe-list overlays

**Files:**
- Create: `server/internal/game/recipe_persistence.go`
- Modify: `server/internal/game/recipes.go` (`getRecipeDef`/`ListRecipeDefs`/`getRecipeListDef`/`ListRecipeListDefs` overlay-aware)
- Modify: `server/internal/game/items.go` (`getItemListDef`/`ListItemListDefs` overlay-aware)
- Test: `server/internal/game/recipe_persistence_test.go`

**Interfaces:**
- Consumes: Task 1 (`resolveItemsDir`, overlay conventions), `RecipeDef`, `validateRecipeDef`, `RecipeListDef`, `ItemListDef`.
- Produces: `resolveRecipesDir()`; `runtimeRecipes`/`runtimeRecipeLists`/`runtimeItemLists` overlays + mutexes; `LoadPersistedRecipesIntoOverlay()`; `loadPersistedRecipesFromDir(dir) int`; `SaveRecipeDef(def *RecipeDef) error` (writes `<dir>/<rarity>/<id>.json`); `DeleteRecipeOverride(id) (bool, error)`; `SaveItemListDef(def *ItemListDef) error` (writes `<items dir>/lists/<id>.json`); `SaveRecipeListDef(def *RecipeListDef) error` (writes `<recipes dir>/lists/<id>.json`); `ensureItemListMembership(listID, itemID string, member bool) error`; `ensureRecipeListMembership(listID, recipeID string, member bool) error`. Task 5 calls the ensure* helpers.

- [ ] **Step 1: Write the failing tests**

`server/internal/game/recipe_persistence_test.go`:
```go
package game

import (
	"os"
	"path/filepath"
	"testing"
)

func recipeOverlayCleanup(t *testing.T, id string) {
	t.Helper()
	t.Cleanup(func() {
		runtimeRecipesMu.Lock()
		delete(runtimeRecipes, id)
		runtimeRecipesMu.Unlock()
	})
}

// TestSaveRecipeDef_RarityFromOutputTierAndLiveRegistration.
func TestSaveRecipeDef_RarityFromOutputTierAndLiveRegistration(t *testing.T) {
	const id = "test_recipe_save"
	recipeOverlayCleanup(t, id)
	dir := t.TempDir()
	t.Setenv("RECIPE_CATALOG_DIR", dir)
	// Inputs/output must be known items — use shipped ones.
	def := &RecipeDef{ID: id, Name: "Test Recipe", Inputs: []string{"steel_shield", "fire_ring"}, CostGold: 100, Output: "fire_shield"}
	if err := SaveRecipeDef(def); err != nil {
		t.Fatalf("save: %v", err)
	}
	// fire_shield is rare → file lands in rare/.
	if _, err := os.Stat(filepath.Join(dir, "rare", id+".json")); err != nil {
		t.Fatalf("expected rare/%s.json: %v", id, err)
	}
	got, ok := getRecipeDef(id)
	if !ok || got.Rarity != ItemTierRare || got.CostGold != 100 {
		t.Fatalf("live registration wrong: ok=%v %+v", ok, got)
	}
	// Validation still gates: unknown input rejected before write.
	bad := &RecipeDef{ID: "bad_r", Name: "x", Inputs: []string{"no_such_item", "fire_ring"}, CostGold: 1, Output: "fire_shield"}
	if err := SaveRecipeDef(bad); err == nil {
		t.Error("expected unknown-input validation error")
	}
}

// TestRecipeOverlay_LoadFromDirDerivesRarityFromPath.
func TestRecipeOverlay_LoadFromDirDerivesRarityFromPath(t *testing.T) {
	const id = "test_recipe_load"
	recipeOverlayCleanup(t, id)
	dir := t.TempDir()
	sub := filepath.Join(dir, "uncommon")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{ "id": "` + id + `", "name": "Loaded", "inputs": ["steel_shield", "fire_ring"], "costGold": 50, "output": "fire_shield" }`)
	if err := os.WriteFile(filepath.Join(sub, id+".json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if n := loadPersistedRecipesFromDir(dir); n < 1 {
		t.Fatalf("expected >=1 recipe loaded, got %d", n)
	}
	got, ok := getRecipeDef(id)
	if !ok || got.Rarity != ItemTierUncommon {
		t.Fatalf("rarity from path failed: ok=%v %+v", ok, got)
	}
}

// TestEnsureItemListMembership_AddRemoveIdempotent operates on the shipped
// marketplace list via the overlay (never mutating the embed).
func TestEnsureItemListMembership_AddRemoveIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ITEM_CATALOG_DIR", dir)
	t.Cleanup(func() {
		runtimeItemListsMu.Lock()
		delete(runtimeItemLists, "marketplace")
		runtimeItemListsMu.Unlock()
	})
	before, _ := getItemListDef("marketplace")
	baseLen := len(before.Items)

	if err := ensureItemListMembership("marketplace", "fire_ring", true); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := ensureItemListMembership("marketplace", "fire_ring", true); err != nil {
		t.Fatalf("re-add: %v", err)
	}
	after, _ := getItemListDef("marketplace")
	if len(after.Items) != baseLen+1 {
		t.Fatalf("add not idempotent: %d → %d, want +1", baseLen, len(after.Items))
	}
	// File written whole to <items dir>/lists/.
	if _, err := os.Stat(filepath.Join(dir, "lists", "marketplace.json")); err != nil {
		t.Fatalf("list file not written: %v", err)
	}
	if err := ensureItemListMembership("marketplace", "fire_ring", false); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := ensureItemListMembership("marketplace", "fire_ring", false); err != nil {
		t.Fatalf("re-remove: %v", err)
	}
	final, _ := getItemListDef("marketplace")
	if len(final.Items) != baseLen {
		t.Fatalf("remove not idempotent: want %d, got %d", baseLen, len(final.Items))
	}
}

// TestEnsureRecipeListMembership_Idempotent — same shape for recipe lists.
func TestEnsureRecipeListMembership_Idempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RECIPE_CATALOG_DIR", dir)
	t.Cleanup(func() {
		runtimeRecipeListsMu.Lock()
		delete(runtimeRecipeLists, "druid_recipes_1")
		runtimeRecipeListsMu.Unlock()
	})
	before, _ := getRecipeListDef("druid_recipes_1")
	baseLen := len(before.Recipes)
	if err := ensureRecipeListMembership("druid_recipes_1", "scimitar", true); err != nil {
		t.Fatalf("add: %v", err)
	}
	after, _ := getRecipeListDef("druid_recipes_1")
	if len(after.Recipes) != baseLen+1 {
		t.Fatalf("want +1 member, got %d → %d", baseLen, len(after.Recipes))
	}
	if err := ensureRecipeListMembership("druid_recipes_1", "scimitar", false); err != nil {
		t.Fatalf("remove: %v", err)
	}
	final, _ := getRecipeListDef("druid_recipes_1")
	if len(final.Recipes) != baseLen {
		t.Fatalf("remove failed: %d", len(final.Recipes))
	}
}
```

- [ ] **Step 2: Run to verify compile failure**

Run: `cd server && go test ./internal/game/ -run "TestSaveRecipeDef_|TestRecipeOverlay_|TestEnsureItemListMembership_|TestEnsureRecipeListMembership_" -v`
Expected: FAIL to compile — undefined symbols.

- [ ] **Step 3: Implement `recipe_persistence.go`**

```go
package game

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ─── Writable recipe / list overlays (see item_persistence.go) ──────────────

var (
	runtimeRecipesMu sync.RWMutex
	runtimeRecipes   = map[string]*RecipeDef{}

	runtimeRecipeListsMu sync.RWMutex
	runtimeRecipeLists   = map[string]*RecipeListDef{}

	runtimeItemListsMu sync.RWMutex
	runtimeItemLists   = map[string]*ItemListDef{}
)

func resolveRecipesDir() (string, error) {
	if dir := os.Getenv("RECIPE_CATALOG_DIR"); dir != "" {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cwd, "internal", "game", "catalog", "recipes")
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	return "", fmt.Errorf("recipes directory not found at %s; set RECIPE_CATALOG_DIR env var to override", dir)
}

// SaveRecipeDef validates, writes <dir>/<rarity>/<id>.json (rarity = the
// OUTPUT item's tier, matching how the embed loader derives Rarity from the
// subdirectory), and registers live.
func SaveRecipeDef(def *RecipeDef) error {
	if err := validateRecipeDef(def); err != nil {
		return err
	}
	out, ok := getItemDef(def.Output)
	if !ok {
		return fmt.Errorf("recipe %q: output %q is not a known item", def.ID, def.Output)
	}
	rarity := out.Tier
	if rarity == "" {
		rarity = ItemTierUncommon
	}
	dir, err := resolveRecipesDir()
	if err != nil {
		return err
	}
	outDir := filepath.Join(dir, string(rarity))
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	// Rarity is path-derived on load; never author it into the file.
	fileDef := *def
	fileDef.Rarity = ""
	raw, err := json.MarshalIndent(&fileDef, "", "  ")
	if err != nil {
		return err
	}
	removeRecipeOverrideFiles(dir, def.ID)
	if err := os.WriteFile(filepath.Join(outDir, def.ID+".json"), raw, 0o644); err != nil {
		return err
	}
	reg := *def
	reg.Rarity = rarity
	runtimeRecipesMu.Lock()
	runtimeRecipes[def.ID] = &reg
	runtimeRecipesMu.Unlock()
	return nil
}

// DeleteRecipeOverride removes the writable recipe file + overlay entry.
func DeleteRecipeOverride(id string) (existed bool, err error) {
	dir, derr := resolveRecipesDir()
	if derr != nil {
		return false, derr
	}
	removed := removeRecipeOverrideFiles(dir, id)
	runtimeRecipesMu.Lock()
	_, inOverlay := runtimeRecipes[id]
	delete(runtimeRecipes, id)
	runtimeRecipesMu.Unlock()
	return removed || inOverlay, nil
}

func removeRecipeOverrideFiles(dir, id string) bool {
	removed := false
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "lists" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == id+".json" {
			if rerr := os.Remove(path); rerr == nil {
				removed = true
			}
		}
		return nil
	})
	return removed
}

// LoadPersistedRecipesIntoOverlay — startup hook, best-effort.
func LoadPersistedRecipesIntoOverlay() {
	dir, err := resolveRecipesDir()
	if err != nil {
		slog.Info("persisted recipes: no writable recipes dir; using embedded catalog only", "err", err)
		return
	}
	if n := loadPersistedRecipesFromDir(dir); n > 0 {
		slog.Info("persisted recipes: overlaid on embedded catalog", "count", n, "dir", dir)
	}
}

func loadPersistedRecipesFromDir(dir string) int {
	loaded := 0
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "lists" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			return nil
		}
		def, perr := parsePersistedRecipeFile(dir, path)
		if perr != nil {
			slog.Warn("persisted recipes: skipped file", "file", d.Name(), "err", perr)
			return nil
		}
		runtimeRecipesMu.Lock()
		runtimeRecipes[def.ID] = def
		runtimeRecipesMu.Unlock()
		loaded++
		return nil
	})
	return loaded
}

func parsePersistedRecipeFile(root, path string) (def *RecipeDef, err error) {
	defer func() {
		if r := recover(); r != nil {
			def = nil
			err = fmt.Errorf("invalid recipe: %v", r)
		}
	}()
	raw, rerr := os.ReadFile(path)
	if rerr != nil {
		return nil, rerr
	}
	var d RecipeDef
	if uerr := json.Unmarshal(raw, &d); uerr != nil {
		return nil, uerr
	}
	if d.ID == "" {
		return nil, fmt.Errorf("recipe has empty id")
	}
	// Rarity from the subdirectory relative to root, like the embed loader.
	rel, rerr2 := filepath.Rel(root, path)
	if rerr2 == nil {
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) >= 2 {
			d.Rarity = ItemTier(parts[0])
		}
	}
	if d.Rarity == "" {
		d.Rarity = ItemTierUncommon
	}
	if verr := validateRecipeDef(&d); verr != nil {
		return nil, verr
	}
	return &d, nil
}

// ─── List saves + membership helpers ─────────────────────────────────────────

// SaveItemListDef writes the whole list to <items dir>/lists/<id>.json and
// registers it in the overlay.
func SaveItemListDef(def *ItemListDef) error {
	dir, err := resolveItemsDir()
	if err != nil {
		return err
	}
	outDir := filepath.Join(dir, "lists")
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
	reg := *def
	runtimeItemListsMu.Lock()
	runtimeItemLists[def.ID] = &reg
	runtimeItemListsMu.Unlock()
	return nil
}

// SaveRecipeListDef — same for recipe lists under <recipes dir>/lists/.
func SaveRecipeListDef(def *RecipeListDef) error {
	dir, err := resolveRecipesDir()
	if err != nil {
		return err
	}
	outDir := filepath.Join(dir, "lists")
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
	reg := *def
	runtimeRecipeListsMu.Lock()
	runtimeRecipeLists[def.ID] = &reg
	runtimeRecipeListsMu.Unlock()
	return nil
}

// ensureItemListMembership adds/removes itemID in the named list and saves the
// whole list. Idempotent: no write when already in the desired state. An
// unknown list is created on add (empty name = title-cased id).
func ensureItemListMembership(listID, itemID string, member bool) error {
	cur, ok := getItemListDef(listID)
	var list ItemListDef
	if ok {
		list = *cur
		list.Items = append([]string(nil), cur.Items...)
	} else {
		if !member {
			return nil
		}
		list = ItemListDef{ID: listID, Name: listID}
	}
	idx := -1
	for i, id := range list.Items {
		if id == itemID {
			idx = i
			break
		}
	}
	switch {
	case member && idx >= 0, !member && idx < 0:
		return nil // already in desired state
	case member:
		list.Items = append(list.Items, itemID)
		sort.Strings(list.Items)
	default:
		list.Items = append(list.Items[:idx], list.Items[idx+1:]...)
	}
	return SaveItemListDef(&list)
}

// ensureRecipeListMembership — mirror for recipe lists.
func ensureRecipeListMembership(listID, recipeID string, member bool) error {
	cur, ok := getRecipeListDef(listID)
	var list RecipeListDef
	if ok {
		list = *cur
		list.Recipes = append([]string(nil), cur.Recipes...)
	} else {
		if !member {
			return nil
		}
		list = RecipeListDef{ID: listID, Name: listID}
	}
	idx := -1
	for i, id := range list.Recipes {
		if id == recipeID {
			idx = i
			break
		}
	}
	switch {
	case member && idx >= 0, !member && idx < 0:
		return nil
	case member:
		list.Recipes = append(list.Recipes, recipeID)
		sort.Strings(list.Recipes)
	default:
		list.Recipes = append(list.Recipes[:idx], list.Recipes[idx+1:]...)
	}
	return SaveRecipeListDef(&list)
}
```

- [ ] **Step 4: Overlay-aware readers**

`recipes.go` — `getRecipeDef` / `ListRecipeDefs` / `getRecipeListDef` / `ListRecipeListDefs` gain the identical overlay-first / merge patterns from Task 1 Step 4, using `runtimeRecipes(Mu)` and `runtimeRecipeLists(Mu)`:
```go
func getRecipeDef(id string) (*RecipeDef, bool) {
	runtimeRecipesMu.RLock()
	if def, ok := runtimeRecipes[id]; ok {
		runtimeRecipesMu.RUnlock()
		return def, true
	}
	runtimeRecipesMu.RUnlock()
	def, ok := recipeCatalogSingleton[id]
	return def, ok
}

func ListRecipeDefs() []*RecipeDef {
	merged := make(map[string]*RecipeDef, len(recipeCatalogSingleton))
	for id, def := range recipeCatalogSingleton {
		merged[id] = def
	}
	runtimeRecipesMu.RLock()
	for id, def := range runtimeRecipes {
		merged[id] = def
	}
	runtimeRecipesMu.RUnlock()
	defs := make([]*RecipeDef, 0, len(merged))
	for _, def := range merged {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}
```
(`getRecipeListDef`/`ListRecipeListDefs` identical with `runtimeRecipeLists`; `items.go`'s `getItemListDef`/`ListItemListDefs` identical with `runtimeItemLists`.)

- [ ] **Step 5: Run tests, then whole package**

Run: `cd server && go test ./internal/game/ -run "TestSaveRecipeDef_|TestRecipeOverlay_|TestEnsureItemListMembership_|TestEnsureRecipeListMembership_" -v`
Expected: PASS.
Run: `cd server && go test ./internal/game/ 2>&1 | grep -E "^(--- FAIL|FAIL|ok)"`
Expected: `ok` only.

- [ ] **Step 6: Commit**

```bash
git add server/internal/game/recipe_persistence.go server/internal/game/recipe_persistence_test.go server/internal/game/recipes.go server/internal/game/items.go
git commit -m "Add writable recipe and list overlays with membership helpers"
```

---

### Task 3: Loot-table overlay + weighted merchant membership

**Files:**
- Create: `server/internal/game/loot_table_persistence.go`
- Modify: `server/internal/game/loot_table_defs.go` (`getLootTable` + packaged-item getter overlay-aware; expose the raw catalog for editing)
- Test: `server/internal/game/loot_table_persistence_test.go`

**Interfaces:**
- Consumes: `rawLootCatalog`, `LootSubtableEntry`, `getLootTable`, `packagedItemsByID`, `loadPackagedItems`/`loadLootTables` internals (read for context before editing).
- Produces: `resolveNeutralGroupsDir()`; `LoadPersistedLootTablesIntoOverlay()`; `loadPersistedLootTablesFromFile(path string) bool`; `SetMerchantItemAvailability(itemID, category string, enabled bool, weight int) error`; `merchantSubtableForCategory(category string) string`; overlay-aware `getLootTable` + packaged-item lookups.

**Approach notes (read before coding):**
- The loot catalog is ONE file (`catalog/neutral_groups/loot_tables.json`) with `packagedItems` (subtables holding `{item,min,max}` rows) and `tables`. Merchant stock items live in the `merchant_weapons` / `merchant_potions` / `merchant_accessories` / `merchant_armor` packaged subtables referenced by the `merchant_basic` table.
- "Weight" = a subtable row's d100 width (`max-min+1`). Membership editing works on WIDTHS: extract each row's width, add/remove/update the item's row, then renormalize all widths proportionally to sum exactly 100 (largest-remainder rounding; iterate rows in slice order for determinism) and reassign contiguous `min`/`max` from 1.
- Keep a runtime copy of the whole raw catalog: `runtimeLootCatalogMu sync.RWMutex` + `runtimeLootCatalog *rawLootCatalog` (nil = embed only). Save = mutate a deep copy, write whole file to `<neutral groups dir>/loot_tables.json`, rebuild the derived `packagedItems`/`lootTables` overlay maps from it, swap under lock. Readers check overlay maps first.
- The derived overlay maps: `runtimePackagedItems map[string]PackagedItem`, `runtimeLootTables map[string]LootTableDef` — rebuilt whole on every load/save (small data; simplicity wins).

- [ ] **Step 1: Write the failing tests**

`server/internal/game/loot_table_persistence_test.go`:
```go
package game

import (
	"os"
	"path/filepath"
	"testing"
)

func lootOverlayCleanup(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		runtimeLootCatalogMu.Lock()
		runtimeLootCatalog = nil
		runtimePackagedItems = nil
		runtimeLootTables = nil
		runtimeLootCatalogMu.Unlock()
	})
}

// subtableWidths returns item→width for a packaged subtable (test helper).
func subtableWidths(t *testing.T, subtable string) map[string]int {
	t.Helper()
	pi, ok := getPackagedItem(subtable)
	if !ok {
		t.Fatalf("subtable %q missing", subtable)
	}
	out := map[string]int{}
	total := 0
	for _, e := range pi.Entries {
		w := e.Max - e.Min + 1
		out[e.Item] = w
		total += w
	}
	if total != 100 {
		t.Fatalf("subtable %q widths sum to %d, want 100", subtable, total)
	}
	return out
}

// TestSetMerchantItemAvailability_AddRenormalizesTo100.
func TestSetMerchantItemAvailability_AddRenormalizesTo100(t *testing.T) {
	lootOverlayCleanup(t)
	dir := t.TempDir()
	t.Setenv("NEUTRAL_GROUPS_DIR", dir)

	before := subtableWidths(t, "merchant_armor")
	if _, present := before["elven_cloak"]; present {
		t.Skip("elven_cloak already in merchant_armor")
	}
	if err := SetMerchantItemAvailability("elven_cloak", "Armor", true, 20); err != nil {
		t.Fatalf("add: %v", err)
	}
	after := subtableWidths(t, "merchant_armor") // re-validates sum==100
	if w := after["elven_cloak"]; w < 15 || w > 25 {
		t.Errorf("added weight ~20 expected (rounding tolerance), got %d", w)
	}
	// Relative order of pre-existing entries preserved (widths scaled, not zeroed).
	for item, w := range before {
		if after[item] == 0 {
			t.Errorf("pre-existing %q lost its slot", item)
		}
		_ = w
	}
	// Whole file written.
	if _, err := os.Stat(filepath.Join(dir, "loot_tables.json")); err != nil {
		t.Fatalf("loot_tables.json not written: %v", err)
	}
}

// TestSetMerchantItemAvailability_RemoveAndIdempotence.
func TestSetMerchantItemAvailability_RemoveAndIdempotence(t *testing.T) {
	lootOverlayCleanup(t)
	dir := t.TempDir()
	t.Setenv("NEUTRAL_GROUPS_DIR", dir)

	if err := SetMerchantItemAvailability("elven_cloak", "Armor", true, 10); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := SetMerchantItemAvailability("elven_cloak", "Armor", false, 0); err != nil {
		t.Fatalf("remove: %v", err)
	}
	after := subtableWidths(t, "merchant_armor")
	if _, present := after["elven_cloak"]; present {
		t.Error("item must be gone after remove")
	}
	// Removing again is a no-op, not an error.
	if err := SetMerchantItemAvailability("elven_cloak", "Armor", false, 0); err != nil {
		t.Fatalf("re-remove: %v", err)
	}
}

// TestLootOverlay_LoadFromFileOverridesEmbed: a persisted loot_tables.json is
// picked up at startup and wins over the embed.
func TestLootOverlay_LoadFromFileOverridesEmbed(t *testing.T) {
	lootOverlayCleanup(t)
	dir := t.TempDir()
	t.Setenv("NEUTRAL_GROUPS_DIR", dir)
	// Produce a persisted file via the editing API, then clear the in-memory
	// overlay and reload it from disk — simulating a restart.
	if err := SetMerchantItemAvailability("elven_cloak", "Armor", true, 10); err != nil {
		t.Fatalf("seed: %v", err)
	}
	runtimeLootCatalogMu.Lock()
	runtimeLootCatalog, runtimePackagedItems, runtimeLootTables = nil, nil, nil
	runtimeLootCatalogMu.Unlock()
	if ok := loadPersistedLootTablesFromFile(filepath.Join(dir, "loot_tables.json")); !ok {
		t.Fatal("reload from disk failed")
	}
	widths := subtableWidths(t, "merchant_armor")
	if _, present := widths["elven_cloak"]; !present {
		t.Error("persisted membership lost across simulated restart")
	}
}

// TestMerchantSubtableForCategory mapping.
func TestMerchantSubtableForCategory(t *testing.T) {
	cases := map[string]string{
		"Weapon": "merchant_weapons", "Armor": "merchant_armor", "Shield": "merchant_armor",
		"Accessory": "merchant_accessories", "Consumable": "merchant_potions", "Anything": "merchant_accessories",
	}
	for cat, want := range cases {
		if got := merchantSubtableForCategory(cat); got != want {
			t.Errorf("%s → %s, want %s", cat, got, want)
		}
	}
}
```
NOTE: the tests use `getPackagedItem(id string) (PackagedItem, bool)` — if loot_table_defs.go exposes packaged items differently (direct map access), add this small getter as part of Step 3 and use it in the shop code path check.

- [ ] **Step 2: Run to verify compile failure**

Run: `cd server && go test ./internal/game/ -run "TestSetMerchantItemAvailability_|TestLootOverlay_|TestMerchantSubtableForCategory" -v`
Expected: FAIL to compile.

- [ ] **Step 3: Implement `loot_table_persistence.go`**

```go
package game

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// ─── Writable loot-table overlay (single-file catalog) ──────────────────────
//
// The neutral-groups loot catalog is one JSON file (packagedItems + tables).
// The editor edits merchant subtable membership by WIDTH (d100 range size),
// renormalizing each edited subtable to sum exactly 100. The whole file is
// rewritten on every change and overlaid at startup.

var (
	runtimeLootCatalogMu sync.RWMutex
	runtimeLootCatalog   *rawLootCatalog
	runtimePackagedItems map[string]PackagedItem
	runtimeLootTables    map[string]LootTableDef
)

func resolveNeutralGroupsDir() (string, error) {
	if dir := os.Getenv("NEUTRAL_GROUPS_DIR"); dir != "" {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cwd, "internal", "game", "catalog", "neutral_groups")
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	return "", fmt.Errorf("neutral_groups directory not found at %s; set NEUTRAL_GROUPS_DIR env var to override", dir)
}

// merchantSubtableForCategory picks the merchant subtable an item's category
// stocks into. Shields sell beside armor; unknown categories default to
// accessories (the most generic bucket).
func merchantSubtableForCategory(category string) string {
	switch category {
	case "Weapon":
		return "merchant_weapons"
	case "Armor", "Shield":
		return "merchant_armor"
	case "Consumable":
		return "merchant_potions"
	case "Accessory":
		return "merchant_accessories"
	default:
		return "merchant_accessories"
	}
}

// currentRawLootCatalogLocked returns a deep copy of the effective raw
// catalog (overlay if present, else re-parsed embed). Caller need NOT hold the
// mutex; the copy is private to the caller.
func currentRawLootCatalogCopy() (*rawLootCatalog, error) {
	runtimeLootCatalogMu.RLock()
	src := runtimeLootCatalog
	runtimeLootCatalogMu.RUnlock()
	if src == nil {
		raw, err := lootTablesFS.ReadFile("catalog/neutral_groups/loot_tables.json")
		if err != nil {
			return nil, err
		}
		var parsed rawLootCatalog
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return nil, err
		}
		return &parsed, nil
	}
	// Deep copy via JSON round-trip (small data, editor-frequency calls).
	blob, err := json.Marshal(src)
	if err != nil {
		return nil, err
	}
	var cp rawLootCatalog
	if err := json.Unmarshal(blob, &cp); err != nil {
		return nil, err
	}
	return &cp, nil
}

// renormalizeSubtable reassigns contiguous 1..100 ranges from the given
// widths, scaling proportionally with largest-remainder rounding. Entries
// keep their slice order (deterministic).
func renormalizeSubtable(entries []LootSubtableEntry, widths []int) []LootSubtableEntry {
	total := 0
	for _, w := range widths {
		total += w
	}
	if total <= 0 || len(entries) == 0 || len(entries) >= 100 {
		// >=100 members cannot each hold a >=1-wide d100 slot; refuse to
		// renormalize rather than emit zero-width rows (unreachable for the
		// small merchant subtables, guarded for safety).
		return entries
	}
	scaled := make([]int, len(widths))
	remainders := make([]float64, len(widths))
	sum := 0
	for i, w := range widths {
		exact := float64(w) * 100.0 / float64(total)
		scaled[i] = int(exact)
		if scaled[i] < 1 {
			scaled[i] = 1 // every member keeps at least 1% — never silently vanishes
		}
		remainders[i] = exact - float64(int(exact))
		sum += scaled[i]
	}
	// Distribute the remainder (may be negative if the min-1 clamps overshot).
	for sum != 100 {
		bestIdx, best := 0, -1.0
		for i, r := range remainders {
			if sum < 100 && r > best {
				best, bestIdx = r, i
			}
			if sum > 100 && scaled[i] > 1 && (best < 0 || r < best) {
				best, bestIdx = r, i
			}
		}
		if sum < 100 {
			scaled[bestIdx]++
			remainders[bestIdx] = 0
			sum++
		} else {
			scaled[bestIdx]--
			remainders[bestIdx] = 1
			sum--
		}
	}
	cursor := 1
	out := make([]LootSubtableEntry, len(entries))
	for i, e := range entries {
		out[i] = LootSubtableEntry{Item: e.Item, Min: cursor, Max: cursor + scaled[i] - 1}
		cursor += scaled[i]
	}
	return out
}

// SetMerchantItemAvailability adds/removes itemID (with the given d100 width
// as its weight; ≤0 defaults to 10) in the merchant subtable matching
// category, renormalizes that subtable, persists the whole loot catalog, and
// swaps the runtime overlay. Idempotent in both directions.
func SetMerchantItemAvailability(itemID, category string, enabled bool, weight int) error {
	if weight <= 0 {
		weight = 10
	}
	subtableID := merchantSubtableForCategory(category)
	cat, err := currentRawLootCatalogCopy()
	if err != nil {
		return err
	}
	sub, ok := cat.PackagedItems[subtableID]
	if !ok || sub.Kind != PackagedItemSubtable {
		return fmt.Errorf("merchant subtable %q not found", subtableID)
	}
	idx := -1
	for i, e := range sub.Entries {
		if e.Item == itemID {
			idx = i
			break
		}
	}
	if enabled == (idx >= 0) {
		// Already in desired membership state. On enable, still allow weight
		// updates: fall through only when the width differs.
		if !enabled {
			return nil
		}
		if cur := sub.Entries[idx].Max - sub.Entries[idx].Min + 1; cur == weight {
			return nil
		}
	}
	widths := make([]int, 0, len(sub.Entries)+1)
	entries := make([]LootSubtableEntry, 0, len(sub.Entries)+1)
	for i, e := range sub.Entries {
		if i == idx && !enabled {
			continue // dropping this row
		}
		w := e.Max - e.Min + 1
		if i == idx && enabled {
			w = weight // weight update in place
		}
		entries = append(entries, e)
		widths = append(widths, w)
	}
	if enabled && idx < 0 {
		entries = append(entries, LootSubtableEntry{Item: itemID})
		widths = append(widths, weight)
	}
	sub.Entries = renormalizeSubtable(entries, widths)
	cat.PackagedItems[subtableID] = sub
	return persistAndSwapLootCatalog(cat)
}

// persistAndSwapLootCatalog writes the whole catalog file and rebuilds the
// derived overlay maps.
func persistAndSwapLootCatalog(cat *rawLootCatalog) error {
	dir, err := resolveNeutralGroupsDir()
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cat, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "loot_tables.json"), raw, 0o644); err != nil {
		return err
	}
	swapLootOverlayFromRaw(cat)
	return nil
}

// swapLootOverlayFromRaw derives the lookup maps (same derivation as the
// embed loaders) and installs everything under one lock.
func swapLootOverlayFromRaw(cat *rawLootCatalog) {
	packaged := make(map[string]PackagedItem, len(cat.PackagedItems))
	for id, rawPI := range cat.PackagedItems {
		packaged[id] = PackagedItem{Kind: rawPI.Kind, Resources: rawPI.Resources, Entries: rawPI.Entries}
	}
	tables := make(map[string]LootTableDef, len(cat.Tables))
	for id, entries := range cat.Tables {
		tables[id] = entries
	}
	runtimeLootCatalogMu.Lock()
	runtimeLootCatalog = cat
	runtimePackagedItems = packaged
	runtimeLootTables = tables
	runtimeLootCatalogMu.Unlock()
}

// LoadPersistedLootTablesIntoOverlay — startup hook, best-effort.
func LoadPersistedLootTablesIntoOverlay() {
	dir, err := resolveNeutralGroupsDir()
	if err != nil {
		slog.Info("persisted loot tables: no writable dir; using embedded catalog only", "err", err)
		return
	}
	path := filepath.Join(dir, "loot_tables.json")
	if loadPersistedLootTablesFromFile(path) {
		slog.Info("persisted loot tables: overlaid on embedded catalog", "file", path)
	}
}

func loadPersistedLootTablesFromFile(path string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var cat rawLootCatalog
	if err := json.Unmarshal(raw, &cat); err != nil {
		slog.Warn("persisted loot tables: skipped invalid file", "file", path, "err", err)
		return false
	}
	swapLootOverlayFromRaw(&cat)
	return true
}
```
IMPORTANT adaptation note: in the DEV default, the writable dir IS the embed source dir, so at startup `loadPersistedLootTablesFromFile` loads the same file the embed already loaded — harmless (identical data). Also: check the actual field names of `PackagedItem`/`rawPackagedItem` in loot_table_defs.go before coding `swapLootOverlayFromRaw` — mirror whatever derivation `loadPackagedItems` performs (e.g. validation of ranges); if the embed loader validates contiguity/coverage, run the same validation and reject the file on failure.

- [ ] **Step 4: Overlay-aware loot readers (loot_table_defs.go)**

```go
func getLootTable(id string) (LootTableDef, bool) {
	runtimeLootCatalogMu.RLock()
	if runtimeLootTables != nil {
		if t, ok := runtimeLootTables[id]; ok {
			runtimeLootCatalogMu.RUnlock()
			return t, true
		}
	}
	runtimeLootCatalogMu.RUnlock()
	t, ok := lootTablesByID[id]
	return t, ok
}

// getPackagedItem resolves a packaged item, overlay first (add this getter if
// call sites currently read packagedItemsByID directly, and route them
// through it — grep: packagedItemsByID).
func getPackagedItem(id string) (PackagedItem, bool) {
	runtimeLootCatalogMu.RLock()
	if runtimePackagedItems != nil {
		if p, ok := runtimePackagedItems[id]; ok {
			runtimeLootCatalogMu.RUnlock()
			return p, true
		}
	}
	runtimeLootCatalogMu.RUnlock()
	p, ok := packagedItemsByID[id]
	return p, ok
}
```
Route every direct `packagedItemsByID[...]` / `lootTablesByID[...]` read in non-test code through these getters (grep and list the call sites in your report).

- [ ] **Step 5: Run tests, then whole package**

Run: `cd server && go test ./internal/game/ -run "TestSetMerchantItemAvailability_|TestLootOverlay_|TestMerchantSubtableForCategory" -v`
Expected: PASS.
Run: `cd server && go test ./internal/game/ 2>&1 | grep -E "^(--- FAIL|FAIL|ok)"`
Expected: `ok` only (loot-drop tests must still pass — the overlay is nil in their runs unless a test set it).

- [ ] **Step 6: Commit**

```bash
git add server/internal/game/loot_table_persistence.go server/internal/game/loot_table_persistence_test.go server/internal/game/loot_table_defs.go server/internal/game/state_shop.go
git commit -m "Add loot-table overlay with weighted merchant membership editing"
```
(Include state_shop.go only if call-site rerouting touched it.)

---

### Task 4: `ListProcEffectDefs` + `GET /catalog/procs`

**Files:**
- Modify: `server/internal/game/proc_effect_defs.go` (add lister)
- Modify: `server/internal/http/router.go` (route, next to `/catalog/items` at :67)
- Test: `server/internal/game/proc_effect_defs_test.go` (append), `server/internal/http/editor_routes_test.go` (create — httptest harness reused by Task 5)

**Interfaces:**
- Consumes: `procEffectDefsByID`, `ProcEffectDef`.
- Produces: `game.ListProcEffectDefs() []ProcEffectDef` (sorted by ID); wire route `GET /catalog/procs` → `{"procs":[...]}`.

- [ ] **Step 1: Failing tests**

Append to `proc_effect_defs_test.go`:
```go
// TestListProcEffectDefs_SortedAndComplete.
func TestListProcEffectDefs_SortedAndComplete(t *testing.T) {
	defs := ListProcEffectDefs()
	if len(defs) < 3 {
		t.Fatalf("expected >=3 shipped proc effects, got %d", len(defs))
	}
	for i := 1; i < len(defs); i++ {
		if defs[i-1].ID >= defs[i].ID {
			t.Fatalf("not sorted: %q before %q", defs[i-1].ID, defs[i].ID)
		}
	}
	if _, ok := getProcEffectDef(defs[0].ID); !ok {
		t.Error("listed def not resolvable")
	}
}
```
Create `server/internal/http/editor_routes_test.go`:
```go
package http

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"webrts/server/internal/ws"
)

// newTestRouter builds the real router with minimal deps for editor-route
// tests. profileManager may be nil-safe — check NewRouter's usage; if profile
// routes require a non-nil manager, construct one against t.TempDir()
// (pattern: grep NewManager usage in profile package tests).
func newTestRouter(t *testing.T) *httptest.Server {
	t.Helper()
	hub := ws.NewHub()
	srv := httptest.NewServer(NewRouter(hub, "*", newTestProfileManager(t), nil))
	t.Cleanup(srv.Close)
	return srv
}

func TestCatalogProcsRoute(t *testing.T) {
	srv := newTestRouter(t)
	resp, err := srv.Client().Get(srv.URL + "/catalog/procs")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var body struct {
		Procs []map[string]any `json:"procs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Procs) < 3 {
		t.Fatalf("expected >=3 procs, got %d", len(body.Procs))
	}
	if body.Procs[0]["id"] == "" {
		t.Error("proc entries need ids")
	}
}
```
(`newTestProfileManager`: implement per the existing profile/ws test helpers — find with `grep -rn "NewHub()\|profile.NewManager" server/internal --include="*_test.go" | head`; mirror whatever the existing router/transport tests do to construct these. If an existing helper builds a full router in tests, reuse its approach verbatim.)

- [ ] **Step 2: Verify failure**

Run: `cd server && go test ./internal/game/ -run TestListProcEffectDefs -v && go test ./internal/http/ -run TestCatalogProcsRoute -v`
Expected: compile failures (`ListProcEffectDefs` undefined; route missing → 404 once compiling).

- [ ] **Step 3: Implement**

`proc_effect_defs.go` (below `getProcEffectDef`; add `"sort"` import):
```go
// ListProcEffectDefs returns every registered proc effect sorted by id —
// consumed by the /catalog/procs route for the item editor's effect picker.
func ListProcEffectDefs() []ProcEffectDef {
	defs := make([]ProcEffectDef, 0, len(procEffectDefsByID))
	for _, def := range procEffectDefsByID {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}
```
`router.go` (after the `/catalog/recipes` route):
```go
	mux.HandleFunc("/catalog/procs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"procs": game.ListProcEffectDefs(),
		})
	})
```

- [ ] **Step 4: Verify pass + commit**

Run: `cd server && go test ./internal/game/ -run TestListProcEffectDefs -v && go test ./internal/http/ -run TestCatalogProcsRoute -v`
Expected: PASS.
```bash
git add server/internal/game/proc_effect_defs.go server/internal/game/proc_effect_defs_test.go server/internal/http/router.go server/internal/http/editor_routes_test.go
git commit -m "Add ListProcEffectDefs + GET /catalog/procs"
```

---

### Task 5: Editor orchestration + `POST /items` + `DELETE /items/{id}`

**Files:**
- Create: `server/internal/game/item_editor.go` (orchestrator + request types)
- Create: `server/internal/http/editor_handlers.go` (`registerEditorRoutes`)
- Modify: `server/internal/http/router.go` (call `registerEditorRoutes(mux)`; CORS gains DELETE)
- Modify: `server/cmd/api/main.go` (startup: the three `LoadPersisted*IntoOverlay` calls next to the maps one)
- Modify: `client/src/game-portal/vite.config.ts` (proxy gains `/items`)
- Test: `server/internal/game/item_editor_test.go`, append to `server/internal/http/editor_routes_test.go`

**Interfaces:**
- Consumes: Tasks 1-3 (`SaveItemDef`, `DeleteItemOverride`, `itemIsEmbedded`, `SaveRecipeDef`, `DeleteRecipeOverride`, `ensureItemListMembership`, `ensureRecipeListMembership`, `SetMerchantItemAvailability`).
- Produces:
```go
type EditorRecipeSpec struct {
	Inputs   []string `json:"inputs"`
	CostGold int      `json:"costGold"`
}
type EditorLootAvailability struct {
	Enabled bool `json:"enabled"`
	Weight  int  `json:"weight"`
}
type EditorAvailability struct {
	Marketplace       bool                   `json:"marketplace"`
	WanderingMerchant bool                   `json:"wanderingMerchant"`
	LootTable         EditorLootAvailability `json:"lootTable"`
	RecipeList        bool                   `json:"recipeList"`
}
type EditorItemSaveRequest struct {
	Item         ItemDef            `json:"item"`
	Recipe       *EditorRecipeSpec  `json:"recipe"`
	Availability EditorAvailability `json:"availability"`
}
func SaveEditorItem(req EditorItemSaveRequest) error
func DeleteEditorItem(id string) (existed bool, err error)
func IsEditorValidationError(err error) bool
```
Wire: `POST /items` (201 `{id,status:"saved"}` / 400 `{"error":"validation_failed","message":...}`), `DELETE /items/{id}` (200 `{id,status:"deleted"|"reset"}` / 404).

- [ ] **Step 1: Failing tests**

`server/internal/game/item_editor_test.go`:
```go
package game

import (
	"os"
	"testing"
)

// editorEnv points every writable dir at temp dirs and cleans all overlays.
func editorEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ITEM_CATALOG_DIR", t.TempDir())
	t.Setenv("RECIPE_CATALOG_DIR", t.TempDir())
	t.Setenv("NEUTRAL_GROUPS_DIR", t.TempDir())
	t.Cleanup(func() {
		runtimeItemsMu.Lock()
		runtimeItems = map[string]*ItemDef{}
		runtimeItemsMu.Unlock()
		runtimeRecipesMu.Lock()
		runtimeRecipes = map[string]*RecipeDef{}
		runtimeRecipesMu.Unlock()
		runtimeItemListsMu.Lock()
		runtimeItemLists = map[string]*ItemListDef{}
		runtimeItemListsMu.Unlock()
		runtimeRecipeListsMu.Lock()
		runtimeRecipeLists = map[string]*RecipeListDef{}
		runtimeRecipeListsMu.Unlock()
		runtimeLootCatalogMu.Lock()
		runtimeLootCatalog, runtimePackagedItems, runtimeLootTables = nil, nil, nil
		runtimeLootCatalogMu.Unlock()
	})
	_ = os.Getenv // silence unused import if not otherwise used
}

// TestSaveEditorItem_FullSurface: item + recipe + all four availability
// surfaces round-trip through every reader.
func TestSaveEditorItem_FullSurface(t *testing.T) {
	editorEnv(t)
	req := EditorItemSaveRequest{
		Item: ItemDef{ID: "editor_test_blade", DisplayName: "Editor Blade", IconKey: "editor_test_blade",
			Kind: ItemKindEquipment, Tier: ItemTierRare, Category: "Weapon", SlotKind: "any",
			CostGold: 120, Modifiers: &ItemModifiers{Damage: 9},
			OnHitProc: &ItemOnHitProc{Chance: 0.1, Effect: "fire_bolt_ignite"}},
		Recipe: &EditorRecipeSpec{Inputs: []string{"broad_sword", "fire_ring"}, CostGold: 150},
		Availability: EditorAvailability{
			Marketplace: true, WanderingMerchant: true,
			LootTable:  EditorLootAvailability{Enabled: true, Weight: 15},
			RecipeList: true,
		},
	}
	if err := SaveEditorItem(req); err != nil {
		t.Fatalf("save: %v", err)
	}
	if def, ok := getItemDef("editor_test_blade"); !ok || !def.Overridden || def.CostGold != 120 {
		t.Fatalf("item not registered: ok=%v %+v", ok, def)
	}
	if rec, ok := getRecipeDef("editor_test_blade"); !ok || rec.Output != "editor_test_blade" || rec.CostGold != 150 {
		t.Fatalf("recipe not registered: ok=%v %+v", ok, rec)
	}
	mkt, _ := getItemListDef("marketplace")
	if !containsString(mkt.Items, "editor_test_blade") {
		t.Error("missing from marketplace list")
	}
	wm, _ := getItemListDef("wandering_merchant")
	if !containsString(wm.Items, "editor_test_blade") {
		t.Error("missing from wandering_merchant list")
	}
	if pi, ok := getPackagedItem("merchant_weapons"); ok {
		found := false
		for _, e := range pi.Entries {
			if e.Item == "editor_test_blade" {
				found = true
			}
		}
		if !found {
			t.Error("missing from merchant_weapons subtable")
		}
	} else {
		t.Error("merchant_weapons subtable missing")
	}
	dr, _ := getRecipeListDef("druid_recipes_1")
	if !containsString(dr.Recipes, "editor_test_blade") {
		t.Error("recipe missing from druid_recipes_1")
	}
}

// TestSaveEditorItem_ValidationRejectsBeforeAnyWrite: an invalid proc effect
// reference fails without touching any availability file.
func TestSaveEditorItem_ValidationRejectsBeforeAnyWrite(t *testing.T) {
	editorEnv(t)
	req := EditorItemSaveRequest{
		Item: ItemDef{ID: "bad_item", DisplayName: "Bad", IconKey: "x", Kind: ItemKindEquipment,
			Tier: ItemTierCommon, SlotKind: "any",
			OnHitProc: &ItemOnHitProc{Chance: 0.1, Effect: "no_such_effect"}},
		Availability: EditorAvailability{Marketplace: true},
	}
	err := SaveEditorItem(req)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !IsEditorValidationError(err) {
		t.Errorf("expected validation-class error, got %v", err)
	}
	if _, ok := getItemDef("bad_item"); ok {
		t.Error("invalid item must not register")
	}
	mkt, _ := getItemListDef("marketplace")
	if containsString(mkt.Items, "bad_item") {
		t.Error("availability must not change on failed save")
	}
}

// TestSaveEditorItem_SelfRecipeRejected.
func TestSaveEditorItem_SelfRecipeRejected(t *testing.T) {
	editorEnv(t)
	req := EditorItemSaveRequest{
		Item:   ItemDef{ID: "selfy", DisplayName: "Selfy", IconKey: "x", Kind: ItemKindEquipment, Tier: ItemTierCommon, SlotKind: "any"},
		Recipe: &EditorRecipeSpec{Inputs: []string{"selfy", "fire_ring"}, CostGold: 10},
	}
	if err := SaveEditorItem(req); err == nil {
		t.Fatal("self-referencing recipe must be rejected")
	}
}

// TestDeleteEditorItem_CleansEverythingForEditorCreated.
func TestDeleteEditorItem_CleansEverythingForEditorCreated(t *testing.T) {
	editorEnv(t)
	req := EditorItemSaveRequest{
		Item:   ItemDef{ID: "doomed_item", DisplayName: "Doomed", IconKey: "doomed_item", Kind: ItemKindEquipment, Tier: ItemTierCommon, Category: "Weapon", SlotKind: "any"},
		Recipe: &EditorRecipeSpec{Inputs: []string{"broad_sword", "fire_ring"}, CostGold: 10},
		Availability: EditorAvailability{Marketplace: true, LootTable: EditorLootAvailability{Enabled: true, Weight: 10}, RecipeList: true},
	}
	if err := SaveEditorItem(req); err != nil {
		t.Fatalf("save: %v", err)
	}
	existed, err := DeleteEditorItem("doomed_item")
	if err != nil || !existed {
		t.Fatalf("delete: existed=%v err=%v", existed, err)
	}
	if _, ok := getItemDef("doomed_item"); ok {
		t.Error("item still visible")
	}
	if _, ok := getRecipeDef("doomed_item"); ok {
		t.Error("recipe still visible")
	}
	mkt, _ := getItemListDef("marketplace")
	if containsString(mkt.Items, "doomed_item") {
		t.Error("still in marketplace list")
	}
	if pi, _ := getPackagedItem("merchant_weapons"); pi.Entries != nil {
		for _, e := range pi.Entries {
			if e.Item == "doomed_item" {
				t.Error("still in merchant subtable")
			}
		}
	}
}
```
(`containsString` exists in the game package — verify with grep; if unexported elsewhere, reuse it.)

Append to `server/internal/http/editor_routes_test.go`:
```go
func TestItemsRoute_SaveAndDelete(t *testing.T) {
	t.Setenv("ITEM_CATALOG_DIR", t.TempDir())
	t.Setenv("RECIPE_CATALOG_DIR", t.TempDir())
	t.Setenv("NEUTRAL_GROUPS_DIR", t.TempDir())
	srv := newTestRouter(t)

	body := `{"item":{"id":"route_test_item","displayName":"Route Test","iconKey":"route_test_item","kind":"equipment","tier":"common","slotKind":"any","costGold":10},"recipe":null,"availability":{"marketplace":true,"wanderingMerchant":false,"lootTable":{"enabled":false,"weight":0},"recipeList":false}}`
	resp, err := srv.Client().Post(srv.URL+"/items", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, raw)
	}

	// Invalid body → 400 with the validation envelope.
	bad := `{"item":{"id":"NOT VALID","displayName":"x","iconKey":"x"},"availability":{}}`
	resp2, _ := srv.Client().Post(srv.URL+"/items", "application/json", strings.NewReader(bad))
	defer resp2.Body.Close()
	if resp2.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp2.StatusCode)
	}

	// DELETE removes it.
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/items/route_test_item", nil)
	resp3, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != 200 {
		t.Fatalf("delete status %d", resp3.StatusCode)
	}
	// Second delete → 404.
	resp4, _ := srv.Client().Do(req)
	defer resp4.Body.Close()
	if resp4.StatusCode != 404 {
		t.Fatalf("re-delete expected 404, got %d", resp4.StatusCode)
	}
}
```
(add imports `io`, `net/http`, `strings` to the test file; note the game-package overlays are process-global — the route test relies on env isolation via t.Setenv.)

- [ ] **Step 2: Verify failure**

Run: `cd server && go test ./internal/game/ -run "TestSaveEditorItem_|TestDeleteEditorItem_" -v && go test ./internal/http/ -run TestItemsRoute_ -v`
Expected: compile failures.

- [ ] **Step 3: Implement `item_editor.go`**

```go
package game

import (
	"errors"
	"fmt"
)

// ─── Editor orchestration: one save request → item + recipe + availability ──

type EditorRecipeSpec struct {
	Inputs   []string `json:"inputs"`
	CostGold int      `json:"costGold"`
}

type EditorLootAvailability struct {
	Enabled bool `json:"enabled"`
	Weight  int  `json:"weight"`
}

type EditorAvailability struct {
	Marketplace       bool                   `json:"marketplace"`
	WanderingMerchant bool                   `json:"wanderingMerchant"`
	LootTable         EditorLootAvailability `json:"lootTable"`
	RecipeList        bool                   `json:"recipeList"`
}

type EditorItemSaveRequest struct {
	Item         ItemDef            `json:"item"`
	Recipe       *EditorRecipeSpec  `json:"recipe"`
	Availability EditorAvailability `json:"availability"`
}

// editorValidationError wraps content errors so the HTTP layer maps them to
// 400 (everything else is a 500). Mirrors IsMapSaveValidationError.
type editorValidationError struct{ err error }

func (e editorValidationError) Error() string { return e.err.Error() }
func (e editorValidationError) Unwrap() error { return e.err }

func IsEditorValidationError(err error) bool {
	var v editorValidationError
	return errors.As(err, &v)
}

// SaveEditorItem validates EVERYTHING first (so a failure never leaves a
// partial save), then applies: item def → recipe → list memberships → loot
// membership. Directory-resolution or IO failures can still land mid-way
// (documented last-write-wins editor semantics, same as maps).
func SaveEditorItem(req EditorItemSaveRequest) error {
	item := req.Item
	// ── validate-first phase (no writes) ──
	if !itemIDPattern.MatchString(item.ID) {
		return editorValidationError{fmt.Errorf("item id %q must match %s", item.ID, itemIDPattern)}
	}
	if err := validateItemDef(&item); err != nil {
		return editorValidationError{err}
	}
	var recipe *RecipeDef
	if req.Recipe != nil {
		for _, in := range req.Recipe.Inputs {
			if in == item.ID {
				return editorValidationError{fmt.Errorf("recipe for %q cannot use itself as an input", item.ID)}
			}
		}
		recipe = &RecipeDef{
			ID:       item.ID,
			Name:     item.DisplayName,
			Inputs:   req.Recipe.Inputs,
			CostGold: req.Recipe.CostGold,
			Output:   item.ID,
		}
		// validateRecipeDef resolves inputs/output via getItemDef; the output
		// isn't registered yet on a brand-new item, so validate inputs here
		// and the full recipe after the item registers.
		for i, in := range recipe.Inputs {
			if _, ok := getItemDef(in); !ok {
				return editorValidationError{fmt.Errorf("recipe input[%d] %q is not a known item", i, in)}
			}
		}
		if len(recipe.Inputs) < 2 {
			return editorValidationError{fmt.Errorf("recipe needs at least 2 inputs, has %d", len(recipe.Inputs))}
		}
		if recipe.CostGold < 0 {
			return editorValidationError{fmt.Errorf("recipe costGold must not be negative")}
		}
	}

	// ── apply phase ──
	if err := SaveItemDef(&item); err != nil {
		return err
	}
	if recipe != nil {
		if err := SaveRecipeDef(recipe); err != nil {
			return err
		}
	} else {
		// Crafting toggled off: drop any overlay recipe named after the item.
		// Embedded recipes can't be deleted — reverting an embedded recipe is
		// out of scope (spec).
		if _, err := DeleteRecipeOverride(item.ID); err != nil {
			return err
		}
	}
	if err := ensureItemListMembership("marketplace", item.ID, req.Availability.Marketplace); err != nil {
		return err
	}
	if err := ensureItemListMembership("wandering_merchant", item.ID, req.Availability.WanderingMerchant); err != nil {
		return err
	}
	if err := SetMerchantItemAvailability(item.ID, item.Category, req.Availability.LootTable.Enabled, req.Availability.LootTable.Weight); err != nil {
		return err
	}
	inRecipeList := req.Availability.RecipeList && recipe != nil
	if err := ensureRecipeListMembership("druid_recipes_1", item.ID, inRecipeList); err != nil {
		return err
	}
	return nil
}

// DeleteEditorItem removes the item override. For editor-created items (not
// in the embed) it also strips the recipe, list memberships, and loot rows so
// no dangling references survive (list/recipe validators would reject them at
// next startup otherwise).
func DeleteEditorItem(id string) (existed bool, err error) {
	existed, err = DeleteItemOverride(id)
	if err != nil || !existed {
		return existed, err
	}
	if itemIsEmbedded(id) {
		return true, nil // reset-to-default: embed provides the def; memberships stay
	}
	if _, derr := DeleteRecipeOverride(id); derr != nil {
		return true, derr
	}
	if lerr := ensureItemListMembership("marketplace", id, false); lerr != nil {
		return true, lerr
	}
	if lerr := ensureItemListMembership("wandering_merchant", id, false); lerr != nil {
		return true, lerr
	}
	if lerr := ensureRecipeListMembership("druid_recipes_1", id, false); lerr != nil {
		return true, lerr
	}
	// Sweep every merchant subtable (category may have changed since save).
	for _, sub := range []string{"Weapon", "Armor", "Accessory", "Consumable"} {
		if lerr := SetMerchantItemAvailability(id, sub, false, 0); lerr != nil {
			return true, lerr
		}
	}
	return true, nil
}
```

- [ ] **Step 4: Implement `editor_handlers.go` + wiring**

`server/internal/http/editor_handlers.go`:
```go
package http

import (
	"encoding/json"
	"net/http"
	"strings"

	"webrts/server/internal/game"
)

// registerEditorRoutes wires the item-editor endpoints. No auth, matching the
// map editor (dev/desktop tool); server-side validation is the gate.
func registerEditorRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/items", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
			return
		}
		var req game.EditorItemSaveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		if err := game.SaveEditorItem(req); err != nil {
			if game.IsEditorValidationError(err) {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": req.Item.ID, "status": "saved"})
	})

	mux.HandleFunc("/items/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/items/")
		// Task 6 adds POST /items/{id}/image here via a suffix check.
		if r.Method != http.MethodDelete {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "DELETE only")
			return
		}
		if id == "" || strings.Contains(id, "/") {
			writeJSONError(w, http.StatusBadRequest, "invalid_id", "expected /items/{id}")
			return
		}
		existed, err := game.DeleteEditorItem(id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error())
			return
		}
		if !existed {
			writeJSONError(w, http.StatusNotFound, "not_found", "no editor override for "+id)
			return
		}
		status := "deleted"
		if game.ItemIsEmbedded(id) {
			status = "reset"
		}
		writeJSON(w, map[string]string{"id": id, "status": status})
	})
}
```
NOTE: `itemIsEmbedded` is unexported (game package); export it as `ItemIsEmbedded` in Task 1's file (rename + keep a thin unexported alias if game-internal callers exist) — reflect that in this task's diff.

`router.go`: add `registerEditorRoutes(mux)` after `registerAdvancementRoutes(...)`; CORS methods line becomes:
```go
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
```

`server/cmd/api/main.go`, directly after `game.LoadPersistedMapsIntoOverlay()`:
```go
	// Same restart-survival contract for the item editor's catalogs.
	game.LoadPersistedItemsIntoOverlay()
	game.LoadPersistedRecipesIntoOverlay()
	game.LoadPersistedLootTablesIntoOverlay()
```

`client/src/game-portal/vite.config.ts`: add `'/items'` to the proxy list beside `'/maps'` (same target).

- [ ] **Step 5: Run tests, whole module**

Run: `cd server && go test ./internal/game/ -run "TestSaveEditorItem_|TestDeleteEditorItem_" -v && go test ./internal/http/ -run "TestItemsRoute_|TestCatalogProcsRoute" -v`
Expected: PASS.
Run: `cd server && go build ./... && go test ./... -count=1 2>&1 | grep -E "^(--- FAIL|FAIL)"`
Expected: nothing new (known flaky cmd/api test may appear).

- [ ] **Step 6: Commit**

```bash
git add server/internal/game/item_editor.go server/internal/game/item_editor_test.go server/internal/game/item_persistence.go server/internal/http/editor_handlers.go server/internal/http/editor_routes_test.go server/internal/http/router.go server/cmd/api/main.go client/src/game-portal/vite.config.ts
git commit -m "Add editor orchestration + POST /items + DELETE /items/{id}"
```

---

### Task 6: Icon upload + serve

**Files:**
- Modify: `server/internal/game/item_persistence.go` (`SaveItemIcon`, `ReadItemIcon`, icon delete in `DeleteItemOverride`)
- Modify: `server/internal/http/editor_handlers.go` (POST `/items/{id}/image`), `server/internal/http/router.go` (GET `/catalog/items/{id}/image` — register `/catalog/items/` prefix handler)
- Test: append to `server/internal/game/item_persistence_test.go` and `server/internal/http/editor_routes_test.go`

**Interfaces:**
- Consumes: Task 1 (`resolveItemsDir`, `itemIconsSubdirName`, `SaveItemDef`, `getItemDef`), Task 5 handler skeleton.
- Produces: `game.SaveItemIcon(id string, png []byte) error` (validates PNG + ≤256KB, writes `_icons/<id>.png`, forces the override's IconKey to id); `game.ReadItemIcon(id string) ([]byte, bool)`; wire `POST /items/{id}/image` (201) + `GET /catalog/items/{id}/image` (200 image/png | 404).

- [ ] **Step 1: Failing tests**

Append to `item_persistence_test.go` (import `bytes`, `image`, `image/png`):
```go
// tinyPNG renders a 4x4 PNG in memory.
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestSaveItemIcon_RoundTripAndIconKeyForce.
func TestSaveItemIcon_RoundTripAndIconKeyForce(t *testing.T) {
	const id = "test_icon_item"
	itemOverlayCleanup(t, id)
	dir := t.TempDir()
	t.Setenv("ITEM_CATALOG_DIR", dir)
	def := &ItemDef{ID: id, DisplayName: "Icon", IconKey: "something_else", Kind: ItemKindEquipment, Tier: ItemTierCommon, SlotKind: "any"}
	if err := SaveItemDef(def); err != nil {
		t.Fatalf("save def: %v", err)
	}
	data := tinyPNG(t)
	if err := SaveItemIcon(id, data); err != nil {
		t.Fatalf("save icon: %v", err)
	}
	back, ok := ReadItemIcon(id)
	if !ok || !bytes.Equal(back, data) {
		t.Fatalf("icon round-trip failed: ok=%v len=%d", ok, len(back))
	}
	// IconKey forced to the item id so the URL mapping is unambiguous.
	if got, _ := getItemDef(id); got.IconKey != id {
		t.Errorf("iconKey = %q, want %q", got.IconKey, id)
	}
	// Non-PNG rejected.
	if err := SaveItemIcon(id, []byte("not a png")); err == nil {
		t.Error("expected PNG validation error")
	}
	// Oversize rejected.
	if err := SaveItemIcon(id, make([]byte, 300*1024)); err == nil {
		t.Error("expected size-cap error")
	}
	// Unknown item rejected.
	if err := SaveItemIcon("no_such_item_xyz", data); err == nil {
		t.Error("expected unknown-item error")
	}
}
```
Append to `editor_routes_test.go`:
```go
func TestItemImageRoutes(t *testing.T) {
	t.Setenv("ITEM_CATALOG_DIR", t.TempDir())
	t.Setenv("RECIPE_CATALOG_DIR", t.TempDir())
	t.Setenv("NEUTRAL_GROUPS_DIR", t.TempDir())
	srv := newTestRouter(t)

	// Create the item first.
	body := `{"item":{"id":"img_item","displayName":"Img","iconKey":"img_item","kind":"equipment","tier":"common","slotKind":"any","costGold":1},"recipe":null,"availability":{}}`
	resp, _ := srv.Client().Post(srv.URL+"/items", "application/json", strings.NewReader(body))
	resp.Body.Close()

	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	up, err := srv.Client().Post(srv.URL+"/items/img_item/image", "image/png", bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer up.Body.Close()
	if up.StatusCode != 201 {
		raw, _ := io.ReadAll(up.Body)
		t.Fatalf("upload status %d: %s", up.StatusCode, raw)
	}
	got, err := srv.Client().Get(srv.URL + "/catalog/items/img_item/image")
	if err != nil {
		t.Fatalf("GET image: %v", err)
	}
	defer got.Body.Close()
	if got.StatusCode != 200 || got.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("serve: status %d type %s", got.StatusCode, got.Header.Get("Content-Type"))
	}
	// Missing icon → 404.
	miss, _ := srv.Client().Get(srv.URL + "/catalog/items/never_uploaded/image")
	defer miss.Body.Close()
	if miss.StatusCode != 404 {
		t.Fatalf("missing icon expected 404, got %d", miss.StatusCode)
	}
}
```
(add imports `bytes`, `image`, `image/png` to the http test file.)

- [ ] **Step 2: Verify failure**

Run: `cd server && go test ./internal/game/ -run TestSaveItemIcon_ -v && go test ./internal/http/ -run TestItemImageRoutes -v`
Expected: compile failures.

- [ ] **Step 3: Implement**

`item_persistence.go` additions (imports `bytes`, `image/png`):
```go
// maxItemIconBytes caps uploaded icon size (item icons are ~32-64px sprites).
const maxItemIconBytes = 256 * 1024

// SaveItemIcon validates and stores an uploaded PNG for the item, and forces
// the item's iconKey to its id so the client's server-URL fallback resolves
// unambiguously (spec: upload ALWAYS sets iconKey to the item id).
func SaveItemIcon(id string, data []byte) error {
	def, ok := getItemDef(id)
	if !ok {
		return fmt.Errorf("item %q not found", id)
	}
	if len(data) > maxItemIconBytes {
		return fmt.Errorf("icon exceeds %d bytes", maxItemIconBytes)
	}
	if _, err := png.DecodeConfig(bytes.NewReader(data)); err != nil {
		return fmt.Errorf("icon is not a valid PNG: %w", err)
	}
	dir, err := resolveItemsDir()
	if err != nil {
		return err
	}
	iconDir := filepath.Join(dir, itemIconsSubdirName)
	if err := os.MkdirAll(iconDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(iconDir, id+".png"), data, 0o644); err != nil {
		return err
	}
	if def.IconKey != id {
		updated := *def
		updated.IconKey = id
		return SaveItemDef(&updated)
	}
	return nil
}

// ReadItemIcon returns the uploaded PNG for id, if any.
func ReadItemIcon(id string) ([]byte, bool) {
	if !itemIDPattern.MatchString(id) {
		return nil, false // also blocks path traversal
	}
	dir, err := resolveItemsDir()
	if err != nil {
		return nil, false
	}
	data, err := os.ReadFile(filepath.Join(dir, itemIconsSubdirName, id+".png"))
	if err != nil {
		return nil, false
	}
	return data, true
}
```
`DeleteItemOverride` additionally removes `_icons/<id>.png` (ignore not-exist errors).

`editor_handlers.go` — extend the `/items/` handler's dispatch (before the DELETE branch):
```go
		if rest, isImage := strings.CutSuffix(id, "/image"); isImage && r.Method == http.MethodPost {
			data, rerr := io.ReadAll(http.MaxBytesReader(w, r.Body, 256*1024+1))
			if rerr != nil {
				writeJSONError(w, http.StatusBadRequest, "read_failed", rerr.Error())
				return
			}
			if err := game.SaveItemIcon(rest, data); err != nil {
				writeJSONError(w, http.StatusBadRequest, "icon_rejected", err.Error())
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": rest, "status": "icon_saved"})
			return
		}
```
`router.go` — register the serve route next to `/catalog/items`:
```go
	mux.HandleFunc("/catalog/items/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/catalog/items/")
		id, suffix, ok := strings.Cut(rest, "/")
		if !ok || suffix != "image" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		data, found := game.ReadItemIcon(id)
		if !found {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(data)
	})
```
(add `strings` import to router.go if absent.)

- [ ] **Step 4: Run tests + whole module + commit**

Run: `cd server && go test ./internal/game/ -run TestSaveItemIcon_ -v && go test ./internal/http/ -run TestItemImageRoutes -v && go build ./...`
Expected: PASS + clean build.
```bash
git add server/internal/game/item_persistence.go server/internal/game/item_persistence_test.go server/internal/http/editor_handlers.go server/internal/http/editor_routes_test.go server/internal/http/router.go
git commit -m "Add item icon upload and serving"
```

---

### Task 7: Verification sweep + curl smoke script

**Files:**
- Create: `server/scripts/item-editor-smoke.sh` (manual curl walkthrough, documentation value)
- Fixes only if verification finds drift.

- [ ] **Step 1: Full gates**

Run: `cd server && go vet ./... && go build ./... && go test ./... -count=1 2>&1 | grep -E "^(--- FAIL|FAIL)"`
Expected: vet/build clean; nothing beyond the known flaky cmd/api test.

- [ ] **Step 2: Write the smoke script** (documentation artifact; also proves the curl-testability claim)

`server/scripts/item-editor-smoke.sh`:
```bash
#!/usr/bin/env bash
# Manual smoke test for the item-editor API against a locally running server
# (go run ./cmd/api). Exercises: proc list, save, list-visibility, icon
# upload/serve, delete. Run from server/: bash scripts/item-editor-smoke.sh
set -euo pipefail
BASE="${BASE:-http://localhost:8080}"

echo "-- procs available:" && curl -sf "$BASE/catalog/procs" | head -c 300 && echo
echo "-- saving smoke_blade..."
curl -sf -X POST "$BASE/items" -H 'Content-Type: application/json' -d '{
  "item": {"id":"smoke_blade","displayName":"Smoke Blade","iconKey":"smoke_blade",
           "kind":"equipment","tier":"rare","category":"Weapon","slotKind":"any","costGold":120,
           "modifiers":{"damage":9},
           "onHitProc":{"chance":0.1,"effect":"fire_bolt_ignite"}},
  "recipe": {"inputs":["broad_sword","fire_ring"],"costGold":150},
  "availability": {"marketplace":true,"wanderingMerchant":false,
                    "lootTable":{"enabled":true,"weight":15},"recipeList":true}
}' && echo
echo "-- visible in catalog:" && curl -sf "$BASE/catalog/items" | grep -o '"id":"smoke_blade"' && echo ok
echo "-- uploading icon..."
# any small png; reuse a shipped one
curl -sf -X POST "$BASE/items/smoke_blade/image" -H 'Content-Type: image/png' \
  --data-binary @../client/src/game-portal/src/assets/items/weapons/common/broad_sword.png || echo "(adjust png path if layout differs)"
echo "-- serving icon:" && curl -sfI "$BASE/catalog/items/smoke_blade/image" | head -2
echo "-- deleting..." && curl -sf -X DELETE "$BASE/items/smoke_blade" && echo
echo "SMOKE OK"
```

- [ ] **Step 3: Manual smoke run**

Run: `cd server && (go run ./cmd/api &) && sleep 3 && bash scripts/item-editor-smoke.sh; kill %1 2>/dev/null`
Expected: `SMOKE OK` (if the server port differs, `BASE=... bash scripts/...`). If the icon-path line needs adjusting, fix the script, not the API.
IMPORTANT: the smoke run writes `smoke_blade` artifacts into the DEV catalog dirs and list files if it fails mid-way — after any failed run, `git status` and revert stray catalog changes (`git checkout -- server/internal/game/catalog/`). The script's own DELETE cleans up on success, but list-file edits (marketplace membership) persist as modified files — REVERT these before committing unless intentionally keeping them: `git checkout -- server/internal/game/catalog/`.

- [ ] **Step 4: Commit**

```bash
git checkout -- server/internal/game/catalog/ 2>/dev/null || true
git add server/scripts/item-editor-smoke.sh
git commit -m "Item editor server: verification sweep + curl smoke script"
```
