# Recipe Crafting System — Server Engine Implementation Plan (Plan 2 of 3)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the server-authoritative recipe crafting loop — discover recipes at neutral **Recipe Shops**, craft stronger items at a player-built **Artificer** by consuming ingredient items + gold, and permanently unlock each recipe on the player's account after the first successful craft.

**Architecture:** A new `RecipeDef` catalog (mirroring the item catalog) defines inputs → output + gold cost, served at `GET /catalog/recipes`. A neutral **Recipe Shop** building (capability `recipe-purchase`) stocks a deterministic random subset of recipes per match; buying one adds it to the player's in-match `UnlockedRecipeIDs`. A player-built **Artificer** (capability `crafting`) lets the player craft any unlocked recipe whose ingredients sit in their Vault, consuming the inputs + gold and producing the output. The **first** successful craft of a recipe is recorded to the persistent `PlayerProfile.KnownRecipeIDs` through a fire-and-forget `RecipeRecorder` seam (mirroring the existing `DominionPointCommitter`), so future matches pre-unlock it. All commands are hand-written `case` arms in the existing `handlers.go` switch calling new `*GameState` methods.

**Tech Stack:** Go (server, `server/internal/game`, `server/internal/profile`, `server/internal/ws`, `server/internal/http`, `server/pkg/protocol`). Deterministic tick simulation under a seeded RNG.

**Scope note (3-plan feature):**
- **Plan 1 (done):** elemental item mechanics + the ring/sword items this plan crafts.
- **Plan 2 (this plan):** server engine — recipe catalog, Recipe Shop, Artificer, `purchase_recipe`/`craft_item` commands, `KnownRecipeIDs` persistence + migration + profile-API exposure + match-join seeding. Fully Go-testable; no client UI.
- **Plan 3 (later):** client UI wiring — `/catalog/recipes` fetch + `recipeDefs.ts`, `buy-recipe-*`/`craft-*` action-bar actions in `getBuildingActions`, command senders, and threading `KnownRecipeIDs` from the profile API into `JoinMatchMessage` so the account-wide loop is end-to-end.

Design spec: `docs/superpowers/specs/2026-06-30-equipment-recipe-crafting-design.md`.

## Global Constraints

(from `CLAUDE.md` / `.claude/rules/AI_RULES.md` — every task implicitly includes these)

- **Determinism.** Any randomness (e.g. the Recipe Shop's per-match recipe sample) MUST use a seeded RNG already on `GameState` (`s.rngLoot` for loot/shop rolls). No `math/rand`, no wall-clock, no map-iteration order driving outcomes — iterate sorted keys/slices.
- **Lock discipline.** `*Locked` methods assume `s.mu` is held; public wrappers (`PurchaseRecipe`, `CraftItem`) acquire `s.mu` then delegate, exactly like `PurchaseItem`/`RerollShop`.
- **Not on the tick path.** The craft→profile write MUST be fire-and-forget off the tick loop, mirroring `SetImmediateDominionPointDropHandler` (the handler is invoked from a non-tick command path here, but keep it non-blocking — spawn a goroutine for the profile I/O — so the design invariant holds and a slow disk never stalls a command).
- **No Steamworks symbols in Go; `desktop/` untouched.** This plan does not touch Steam or the desktop crate.
- **IDs:** buildings are identified by `string` ID, units by `int`. Targets stored by ID. (No new cross-tick pointers are introduced here.)
- **Catalog IDs are globally unique.** A new `id` that collides with an existing catalog file clobbers the map — check before adding (Plan 1 hit this with `ice_sword`).
- **Recipe naming carried from Plan 1:** the crafted frost variant is **`frost_sword`** (the `ice_sword` id was already taken by an epic weapon). Recipes output `fire_sword`, `frost_sword`, `lightning_sword`.

---

### Task 1: RecipeDef catalog, loader, validation, and `GET /catalog/recipes`

**Files:**
- Create: `server/internal/game/recipes.go`
- Create: `server/internal/game/catalog/recipes/fire_sword.json`, `frost_sword.json`, `lightning_sword.json`
- Modify: `server/internal/http/router.go` (add a route beside `/catalog/items` at lines 66-71)
- Test: `server/internal/game/recipes_test.go`

**Interfaces:**
- Produces:
  - `type RecipeDef struct { ID string; Name string; Inputs []string; CostGold int; Output string }` (JSON: `id`, `name`, `inputs`, `costGold`, `output`)
  - `func getRecipeDef(id string) (*RecipeDef, bool)`
  - `func ListRecipeDefs() []*RecipeDef` (sorted by ID)
  - `func validateRecipeDef(def *RecipeDef) error` (≥2 inputs; every input id and the output id resolve via `getItemDef`)

- [ ] **Step 1: Write the failing test**

```go
package game

import "testing"

func TestRecipeCatalog_LoadsAndValidates(t *testing.T) {
	cases := []struct {
		id     string
		inputs []string
		output string
	}{
		{"fire_sword", []string{"broad_sword", "fire_ring"}, "fire_sword"},
		{"frost_sword", []string{"broad_sword", "ice_ring"}, "frost_sword"},
		{"lightning_sword", []string{"broad_sword", "lightning_ring"}, "lightning_sword"},
	}
	for _, tc := range cases {
		def, ok := getRecipeDef(tc.id)
		if !ok {
			t.Fatalf("recipe %q not found", tc.id)
		}
		if def.Output != tc.output {
			t.Errorf("%s: output = %q, want %q", tc.id, def.Output, tc.output)
		}
		if len(def.Inputs) != len(tc.inputs) {
			t.Fatalf("%s: %d inputs, want %d", tc.id, len(def.Inputs), len(tc.inputs))
		}
		for i, in := range tc.inputs {
			if def.Inputs[i] != in {
				t.Errorf("%s: input[%d] = %q, want %q", tc.id, i, def.Inputs[i], in)
			}
		}
		if def.CostGold <= 0 {
			t.Errorf("%s: costGold must be positive, got %d", tc.id, def.CostGold)
		}
	}
	if len(ListRecipeDefs()) < 3 {
		t.Fatalf("expected >=3 recipes, got %d", len(ListRecipeDefs()))
	}
}

func TestValidateRecipeDef_Rules(t *testing.T) {
	if err := validateRecipeDef(&RecipeDef{ID: "r", Inputs: []string{"broad_sword"}, Output: "fire_sword"}); err == nil {
		t.Error("expected error for <2 inputs")
	}
	if err := validateRecipeDef(&RecipeDef{ID: "r", Inputs: []string{"broad_sword", "no_such_item"}, Output: "fire_sword"}); err == nil {
		t.Error("expected error for unknown input item")
	}
	if err := validateRecipeDef(&RecipeDef{ID: "r", Inputs: []string{"broad_sword", "fire_ring"}, Output: "no_such_item"}); err == nil {
		t.Error("expected error for unknown output item")
	}
	if err := validateRecipeDef(&RecipeDef{ID: "r", Inputs: []string{"broad_sword", "fire_ring"}, Output: "fire_sword"}); err != nil {
		t.Errorf("valid recipe rejected: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/internal/game/ -run 'TestRecipeCatalog_LoadsAndValidates|TestValidateRecipeDef_Rules' -v`
Expected: FAIL — `getRecipeDef`/`RecipeDef` undefined (compile error).

- [ ] **Step 3: Create `recipes.go` (mirror `items.go`'s loader exactly)**

```go
package game

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed catalog/recipes
var recipeDefsFS embed.FS

// RecipeDef is the catalog definition for one craftable recipe: a set of input
// item IDs (consumed) plus a gold cost that produce one output item ID.
type RecipeDef struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Inputs   []string `json:"inputs"`
	CostGold int      `json:"costGold"`
	Output   string   `json:"output"`
}

var recipeCatalogSingleton = loadRecipeCatalog()

func loadRecipeCatalog() map[string]*RecipeDef {
	catalog := make(map[string]*RecipeDef)
	err := fs.WalkDir(recipeDefsFS, "catalog/recipes", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		data, err := recipeDefsFS.ReadFile(path)
		if err != nil {
			panic(path + ": " + err.Error())
		}
		var def RecipeDef
		if err := json.Unmarshal(data, &def); err != nil {
			panic(path + ": " + err.Error())
		}
		if def.ID == "" {
			panic(path + `: missing "id" field`)
		}
		if err := validateRecipeDef(&def); err != nil {
			panic(path + ": " + err.Error())
		}
		catalog[def.ID] = &def
		return nil
	})
	if err != nil {
		panic("catalog/recipes: " + err.Error())
	}
	return catalog
}

// validateRecipeDef enforces: at least two inputs, and every input + the output
// resolves to a real item def. Called at catalog load (fail-fast).
func validateRecipeDef(def *RecipeDef) error {
	if len(def.Inputs) < 2 {
		return fmt.Errorf("recipe %q: needs at least 2 inputs, has %d", def.ID, len(def.Inputs))
	}
	for i, in := range def.Inputs {
		if _, ok := getItemDef(in); !ok {
			return fmt.Errorf("recipe %q: input[%d] %q is not a known item", def.ID, i, in)
		}
	}
	if _, ok := getItemDef(def.Output); !ok {
		return fmt.Errorf("recipe %q: output %q is not a known item", def.ID, def.Output)
	}
	return nil
}

func getRecipeDef(id string) (*RecipeDef, bool) {
	def, ok := recipeCatalogSingleton[id]
	return def, ok
}

// ListRecipeDefs returns all recipe defs sorted by ID (for the HTTP route and
// deterministic iteration).
func ListRecipeDefs() []*RecipeDef {
	defs := make([]*RecipeDef, 0, len(recipeCatalogSingleton))
	for _, def := range recipeCatalogSingleton {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}
```

- [ ] **Step 4: Create the three recipe JSON files**

`catalog/recipes/fire_sword.json`:
```json
{ "id": "fire_sword", "name": "Fire Sword", "inputs": ["broad_sword", "fire_ring"], "costGold": 150, "output": "fire_sword" }
```
`catalog/recipes/frost_sword.json`:
```json
{ "id": "frost_sword", "name": "Frost Sword", "inputs": ["broad_sword", "ice_ring"], "costGold": 150, "output": "frost_sword" }
```
`catalog/recipes/lightning_sword.json`:
```json
{ "id": "lightning_sword", "name": "Lightning Sword", "inputs": ["broad_sword", "lightning_ring"], "costGold": 150, "output": "lightning_sword" }
```

- [ ] **Step 5: Add the HTTP route**

In `server/internal/http/router.go`, after the `/catalog/items` block (lines 66-71), add:

```go
	mux.HandleFunc("/catalog/recipes", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"recipes": game.ListRecipeDefs(),
		})
	})
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./server/internal/game/ -run 'TestRecipeCatalog_LoadsAndValidates|TestValidateRecipeDef_Rules' -v`
Expected: PASS. Then `go build ./...` from `server/` — clean (router compiles).

- [ ] **Step 7: Commit**

```bash
git add server/internal/game/recipes.go server/internal/game/catalog/recipes server/internal/game/recipes_test.go server/internal/http/router.go
git commit -m "feat(recipes): add RecipeDef catalog, loader, validation, and /catalog/recipes route"
```

---

### Task 2: Recipe Shop building + per-match recipe inventory

**Files:**
- Create: `server/internal/game/catalog/buildings/recipe-shop.json`
- Modify: `server/pkg/protocol/messages.go` (add `RecipeStockEntry`; add `RecipeInventory` to the building snapshot struct that has `ShopInventory`, ~line 581-602)
- Create: `server/internal/game/state_recipe_shop.go`
- Modify: `server/internal/game/state_shop.go` (`initShopBuildingsLocked` also marks `recipe-shop`; wire recipe-inventory population)
- Test: `server/internal/game/state_recipe_shop_test.go`

**Interfaces:**
- Consumes: `ListRecipeDefs` (Task 1), `s.rngLoot`, `s.MapConfig.Buildings`, `hasCapability` pattern.
- Produces:
  - `protocol.RecipeStockEntry struct { RecipeID string; Quantity int }` (JSON `recipeId`, `quantity`)
  - `protocol.BuildingTile.RecipeInventory []RecipeStockEntry` (JSON `recipeInventory,omitempty`) — `BuildingTile` is both the sim type (`s.MapConfig.Buildings`) and the snapshot type, so adding the field auto-serializes it to clients.
  - `func hasRecipePurchaseCapability(b *protocol.BuildingTile) bool`
  - `func (s *GameState) populateRecipeShopInventoriesLocked()` — for every `recipe-shop` building, deterministically sample up to `defaultRecipeShopCount` (2) distinct recipe IDs via `s.rngLoot` and set `b.RecipeInventory` (Quantity 1 each).

- [ ] **Step 1: Write the failing test**

```go
package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

func addRecipeShop(s *GameState, bID string) {
	neutral := neutralPlayerID
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
		ID: bID, BuildingType: "recipe-shop", Width: 3, Height: 3,
		Visible: true, Occupied: true, OwnerID: &neutral,
		Capabilities: []string{"recipe-purchase"},
		Metadata:     map[string]interface{}{},
	})
}

func TestRecipeShop_PopulatesDeterministicSubset(t *testing.T) {
	roll := func(seed int64) []string {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), seed)
		s.EnsurePlayer("p1")
		s.mu.Lock()
		defer s.mu.Unlock()
		addRecipeShop(s, "rs-1")
		if s.buildingsByID == nil {
			s.buildingsByID = map[string]*protocol.BuildingTile{}
		}
		for i := range s.MapConfig.Buildings {
			b := &s.MapConfig.Buildings[i]
			s.buildingsByID[b.ID] = b
		}
		s.initShopBuildingsLocked()
		s.populateRecipeShopInventoriesLocked()
		b := s.buildingsByID["rs-1"]
		out := make([]string, 0, len(b.RecipeInventory))
		for _, e := range b.RecipeInventory {
			if e.Quantity != 1 {
				t.Errorf("recipe stock quantity = %d, want 1", e.Quantity)
			}
			out = append(out, e.RecipeID)
		}
		return out
	}
	a := roll(0xABC)
	b := roll(0xABC)
	if len(a) == 0 {
		t.Fatal("recipe shop stocked nothing")
	}
	if len(a) > 2 {
		t.Fatalf("recipe shop stocked %d > cap 2", len(a))
	}
	// Determinism: same seed → identical subset (order included).
	if len(a) != len(b) {
		t.Fatalf("non-deterministic count: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("non-deterministic subset: %v vs %v", a, b)
		}
	}
	// No duplicates.
	seen := map[string]bool{}
	for _, id := range a {
		if seen[id] {
			t.Fatalf("duplicate recipe in stock: %v", a)
		}
		seen[id] = true
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/internal/game/ -run TestRecipeShop_PopulatesDeterministicSubset -v`
Expected: FAIL — `populateRecipeShopInventoriesLocked` / `RecipeInventory` undefined.

- [ ] **Step 3: Add protocol types**

In `server/pkg/protocol/messages.go`, next to `ShopStockEntry` (lines 608-611) add:

```go
// RecipeStockEntry is one purchasable recipe slot in a Recipe Shop's inventory.
// Quantity decrements on purchase; 0 means sold out (kept in the list so the
// client can render it disabled), mirroring ShopStockEntry.
type RecipeStockEntry struct {
	RecipeID string `json:"recipeId"`
	Quantity int    `json:"quantity"`
}
```

In the building snapshot struct (the one with `ShopInventory []ShopStockEntry` at line 581), add after the shop fields:

```go
	RecipeInventory []RecipeStockEntry `json:"recipeInventory,omitempty"`
```

- [ ] **Step 4: Create the recipe-shop building def**

`server/internal/game/catalog/buildings/recipe-shop.json`:
```json
{
  "type": "recipe-shop",
  "class": "neutral",
  "buildable": false,
  "width": 3,
  "height": 3,
  "maxHp": 0,
  "buildSeconds": 0,
  "resourceCost": {},
  "capabilities": ["recipe-purchase"],
  "spawnUnitTypes": [],
  "metadata": {},
  "color": "#b45309",
  "label": "Recipe Trader",
  "hotkey": ""
}
```

- [ ] **Step 5: Create `state_recipe_shop.go`**

```go
package game

import (
	"sort"

	"webrts/server/pkg/protocol"
)

// defaultRecipeShopCount is how many distinct recipes a Recipe Shop stocks per
// match. Kept small so recipe discovery is a scarce, run-varied resource.
const defaultRecipeShopCount = 2

// hasRecipePurchaseCapability reports whether b is a Recipe Shop.
func hasRecipePurchaseCapability(b *protocol.BuildingTile) bool {
	for _, c := range b.Capabilities {
		if c == "recipe-purchase" {
			return true
		}
	}
	return false
}

// populateRecipeShopInventoriesLocked fills every recipe-shop building's
// RecipeInventory with a deterministic random subset of all recipes, sampled
// via s.rngLoot. Iteration order over buildings is sorted by ID so the sample
// is reproducible across runs. Must be called under s.mu write lock, once at
// match start (after buildingsByID is built).
func (s *GameState) populateRecipeShopInventoriesLocked() {
	all := ListRecipeDefs() // already sorted by ID
	if len(all) == 0 {
		return
	}
	indices := make([]int, 0, len(s.MapConfig.Buildings))
	for i := range s.MapConfig.Buildings {
		if hasRecipePurchaseCapability(&s.MapConfig.Buildings[i]) {
			indices = append(indices, i)
		}
	}
	sort.Slice(indices, func(i, j int) bool {
		return s.MapConfig.Buildings[indices[i]].ID < s.MapConfig.Buildings[indices[j]].ID
	})
	for _, idx := range indices {
		b := &s.MapConfig.Buildings[idx]
		// Already populated (idempotent guard).
		if len(b.RecipeInventory) > 0 {
			continue
		}
		count := defaultRecipeShopCount
		if count > len(all) {
			count = len(all)
		}
		// Partial Fisher-Yates over a copy of the sorted recipe list using the
		// seeded loot RNG → deterministic per (seed, building order).
		pool := make([]*RecipeDef, len(all))
		copy(pool, all)
		for k := 0; k < count; k++ {
			j := k + s.rngLoot.Intn(len(pool)-k)
			pool[k], pool[j] = pool[j], pool[k]
		}
		entries := make([]protocol.RecipeStockEntry, 0, count)
		for k := 0; k < count; k++ {
			entries = append(entries, protocol.RecipeStockEntry{RecipeID: pool[k].ID, Quantity: 1})
		}
		b.RecipeInventory = entries
	}
}
```

- [ ] **Step 6: Mark recipe-shop buildings visible at init + wire population**

In `server/internal/game/state_shop.go`'s `initShopBuildingsLocked` (lines 125-139), broaden the type check so recipe-shops are also assigned to neutral + made visible. Change the `if b.BuildingType != "neutral-shop" { continue }` guard to:

```go
		if b.BuildingType != "neutral-shop" && b.BuildingType != "recipe-shop" {
			continue
		}
```

Find where `populateShopInventoriesLocked()` is called at match start (grep: `populateShopInventoriesLocked()` — it is called from the map-config setup, e.g. `setMapConfigLocked`). Immediately after that call site, add:

```go
	s.populateRecipeShopInventoriesLocked()
```

- [ ] **Step 7: Run test to verify it passes**

Run: `go test ./server/internal/game/ -run TestRecipeShop_PopulatesDeterministicSubset -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add server/internal/game/state_recipe_shop.go server/internal/game/catalog/buildings/recipe-shop.json server/pkg/protocol/messages.go server/internal/game/state_shop.go server/internal/game/state_recipe_shop_test.go
git commit -m "feat(recipes): add Recipe Shop building with deterministic per-match recipe stock"
```

---

### Task 3: `Player.UnlockedRecipeIDs` + match-join seeding + player snapshot

**Files:**
- Modify: `server/internal/game/state.go` (`Player` struct ~line 586-686; `EnsurePlayerWithUpgrades` ~line 3203; `EnsurePlayer` ~line 3190; the `Player{...}` literal ~line 3241-3260)
- Modify: `server/pkg/protocol/messages.go` (`JoinMatchMessage` ~line 613-631; `PlayerSnapshot` ~line 988-1019)
- Modify: `server/internal/ws/handlers.go` (the `join_match` call to `EnsurePlayerWithUpgrades`, ~line 412)
- Modify: the player-snapshot builder (grep `playerVaultSnapshotsLocked` caller / where `PlayerSnapshot{...}` is constructed)
- Test: `server/internal/game/recipe_unlock_seed_test.go`

**Interfaces:**
- Produces:
  - `Player.UnlockedRecipeIDs []string` — in-match set of recipes the player may craft (seeded from profile `KnownRecipeIDs` at join, grown by `purchase_recipe`).
  - `EnsurePlayerWithUpgrades(playerID string, ownedUpgradeRanks map[string]int, activeUpgradeIDs []string, acquiredAdvancementIDs []string, knownRecipeIDs []string)` — NEW trailing param.
  - `func (s *GameState) playerKnowsRecipeLocked(playerID, recipeID string) bool`
  - `func (s *GameState) unlockRecipeForPlayerLocked(player *Player, recipeID string)` (idempotent append, keeps slice sorted)
  - `JoinMatchMessage.KnownRecipeIDs []string`, `PlayerSnapshot.UnlockedRecipeIDs []string`.

- [ ] **Step 1: Write the failing test**

```go
package game

import "testing"

func TestEnsurePlayer_SeedsAndUnlocksRecipes(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	s.EnsurePlayerWithUpgrades("p1", nil, nil, nil, []string{"fire_sword"})
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.playerKnowsRecipeLocked("p1", "fire_sword") {
		t.Fatal("fire_sword should be seeded from knownRecipeIDs")
	}
	if s.playerKnowsRecipeLocked("p1", "frost_sword") {
		t.Fatal("frost_sword was not seeded; should be unknown")
	}
	// Unlock is idempotent and additive.
	p := s.Players["p1"]
	s.unlockRecipeForPlayerLocked(p, "frost_sword")
	s.unlockRecipeForPlayerLocked(p, "frost_sword")
	count := 0
	for _, id := range p.UnlockedRecipeIDs {
		if id == "frost_sword" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("frost_sword appears %d times, want 1 (idempotent)", count)
	}
}

func TestEnsurePlayer_BackwardCompatShim(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	s.EnsurePlayer("p1") // must still compile/work with no recipe arg
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Players["p1"] == nil {
		t.Fatal("EnsurePlayer did not create player")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/internal/game/ -run 'TestEnsurePlayer_SeedsAndUnlocksRecipes|TestEnsurePlayer_BackwardCompatShim' -v`
Expected: FAIL — `playerKnowsRecipeLocked` undefined and `EnsurePlayerWithUpgrades` arity mismatch.

- [ ] **Step 3: Add the `Player` field**

In `server/internal/game/state.go`, in the `Player` struct (near `AcquiredAdvancements []string` ~line 681) add:

```go
	// UnlockedRecipeIDs is the in-match set of recipe IDs this player may craft
	// at an Artificer. Seeded at join from the profile's KnownRecipeIDs and
	// grown by purchase_recipe. Sorted, deduped.
	UnlockedRecipeIDs []string
```

- [ ] **Step 4: Thread the new param + seed it**

Change `EnsurePlayerWithUpgrades`'s signature (state.go:3203) to add `knownRecipeIDs []string` as the final parameter. In the `Player{...}` literal (~3241-3260) add a field set from a defensive copy:

```go
	recipeIDs := make([]string, 0, len(knownRecipeIDs))
	recipeIDs = append(recipeIDs, knownRecipeIDs...)
	sort.Strings(recipeIDs)
```
…and in the struct literal:
```go
		UnlockedRecipeIDs: recipeIDs,
```

Update `EnsurePlayer` (state.go:3190-3192) to pass `nil`:
```go
func (s *GameState) EnsurePlayer(playerID string) {
	s.EnsurePlayerWithUpgrades(playerID, nil, nil, nil, nil)
}
```

Add the two helpers (anywhere in state.go or a small `state_recipes.go` — prefer `state_recipe_shop.go` to keep recipe code together):

```go
// playerKnowsRecipeLocked reports whether the player may craft recipeID this
// match (seeded from profile + purchased). Must be called under s.mu.
func (s *GameState) playerKnowsRecipeLocked(playerID, recipeID string) bool {
	p, ok := s.Players[playerID]
	if !ok {
		return false
	}
	for _, id := range p.UnlockedRecipeIDs {
		if id == recipeID {
			return true
		}
	}
	return false
}

// unlockRecipeForPlayerLocked adds recipeID to the player's in-match unlocked
// set if absent, keeping the slice sorted. Idempotent. Must be called under s.mu.
func (s *GameState) unlockRecipeForPlayerLocked(player *Player, recipeID string) {
	if player == nil || recipeID == "" {
		return
	}
	for _, id := range player.UnlockedRecipeIDs {
		if id == recipeID {
			return
		}
	}
	player.UnlockedRecipeIDs = append(player.UnlockedRecipeIDs, recipeID)
	sort.Strings(player.UnlockedRecipeIDs)
}
```

(Ensure `sort` is imported in whichever file holds these.)

- [ ] **Step 5: Wire the wire-protocol + handler call**

In `server/pkg/protocol/messages.go`:
- `JoinMatchMessage` (~613-631): add `KnownRecipeIDs []string \`json:"knownRecipeIds,omitempty"\``.
- `PlayerSnapshot` (~988-1019): add `UnlockedRecipeIDs []string \`json:"unlockedRecipeIds,omitempty"\``.

In `server/internal/ws/handlers.go` join_match (~line 412), update the call:
```go
	match.State.EnsurePlayerWithUpgrades(msg.PlayerID, msg.OwnedUpgradeRanks, msg.ActiveUpgradeIDs, msg.AcquiredAdvancementIDs, msg.KnownRecipeIDs)
```

In the player-snapshot builder (grep for where `PlayerSnapshot{` is constructed with `Vault:` — likely a `playerSnapshotLocked`/`buildPlayerSnapshotLocked` in `state.go` or a snapshot file), set the new field from the player:
```go
		UnlockedRecipeIDs: append([]string(nil), p.UnlockedRecipeIDs...),
```

- [ ] **Step 6: Run tests + build**

Run: `go test ./server/internal/game/ -run 'TestEnsurePlayer_SeedsAndUnlocksRecipes|TestEnsurePlayer_BackwardCompatShim' -v`
Expected: PASS. Then from `server/`: `go build ./...` (catches any other `EnsurePlayerWithUpgrades` call sites needing the new arg — fix each by passing `nil` unless it has recipe data).

> If `go build` reports other callers of `EnsurePlayerWithUpgrades`, update each to pass the new final argument (`nil` for test/non-join callers). Search: `grep -rn EnsurePlayerWithUpgrades server/`.

- [ ] **Step 7: Commit**

```bash
git add -A server/internal/game server/pkg/protocol/messages.go server/internal/ws/handlers.go
git commit -m "feat(recipes): seed Player.UnlockedRecipeIDs from profile at join + snapshot it"
```

---

### Task 4: `purchase_recipe` command

**Files:**
- Modify: `server/pkg/protocol/messages.go` (add `PurchaseRecipeCommandMessage`)
- Create: `server/internal/game/state_recipe_purchase.go`
- Modify: `server/internal/ws/handlers.go` (add a `case "purchase_recipe":` arm near the `purchase_item` case ~1055; add `"purchase_recipe"` to `isPausableCommand` if world-mutating commands are gated there)
- Test: `server/internal/game/state_recipe_purchase_test.go`

**Interfaces:**
- Consumes: `getRecipeDef`, `hasRecipePurchaseCapability`, `unlockRecipeForPlayerLocked`, `s.FOW`, `shopLockedLocked`.
- Produces:
  - `protocol.PurchaseRecipeCommandMessage struct { Type string; BuildingID string; RecipeID string }`
  - `func (s *GameState) PurchaseRecipe(playerID, buildingID, recipeID string)` (public, locks)
  - `func (s *GameState) handlePurchaseRecipeLocked(playerID, buildingID, recipeID string)`

- [ ] **Step 1: Write the failing test**

```go
package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// markDiscovered records buildingID in the player's FOW KnownBuildings so the
// neutral-shop discovery gate passes. Mirrors how purchase tests set discovery;
// adjust the field path if the FOW struct differs (grep KnownBuildings in
// fow_test.go / state_shop_test.go).
func markRecipeShopDiscovered(s *GameState, playerID, buildingID string) {
	fow := s.FOW[playerID]
	if fow == nil {
		return
	}
	if fow.KnownBuildings == nil {
		fow.KnownBuildings = map[string]struct{}{}
	}
	fow.KnownBuildings[buildingID] = struct{}{}
}

func setupRecipePurchase(t *testing.T) (*GameState, *Player) {
	t.Helper()
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 7)
	s.EnsurePlayer("p1")
	s.mu.Lock()
	addRecipeShop(s, "rs-1") // helper from Task 2's test file (same package)
	if s.buildingsByID == nil {
		s.buildingsByID = map[string]*protocol.BuildingTile{}
	}
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		s.buildingsByID[b.ID] = b
	}
	s.initShopBuildingsLocked()
	// Force a known stock so the test is independent of the sampler.
	s.buildingsByID["rs-1"].RecipeInventory = []protocol.RecipeStockEntry{{RecipeID: "fire_sword", Quantity: 1}}
	p := s.Players["p1"]
	p.Resources["gold"] = 1000
	markRecipeShopDiscovered(s, "p1", "rs-1")
	s.mu.Unlock()
	return s, p
}

func TestPurchaseRecipe_Success(t *testing.T) {
	s, p := setupRecipePurchase(t)
	s.PurchaseRecipe("p1", "rs-1", "fire_sword")
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.playerKnowsRecipeLocked("p1", "fire_sword") {
		t.Fatal("recipe should be unlocked after purchase")
	}
	cost := 150
	if p.Resources["gold"] != 1000-cost {
		t.Fatalf("gold = %d, want %d", p.Resources["gold"], 1000-cost)
	}
	if s.buildingsByID["rs-1"].RecipeInventory[0].Quantity != 0 {
		t.Fatal("stock should decrement to 0 after purchase")
	}
}

func TestPurchaseRecipe_RejectsWhenUnaffordable(t *testing.T) {
	s, p := setupRecipePurchase(t)
	s.mu.Lock()
	p.Resources["gold"] = 10 // < 150
	s.mu.Unlock()
	s.PurchaseRecipe("p1", "rs-1", "fire_sword")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.playerKnowsRecipeLocked("p1", "fire_sword") {
		t.Fatal("recipe must NOT unlock when unaffordable")
	}
	if p.Resources["gold"] != 10 {
		t.Fatalf("gold should be unchanged, got %d", p.Resources["gold"])
	}
}

func TestPurchaseRecipe_RejectsUndiscovered(t *testing.T) {
	s, _ := setupRecipePurchase(t)
	s.mu.Lock()
	delete(s.FOW["p1"].KnownBuildings, "rs-1")
	s.mu.Unlock()
	s.PurchaseRecipe("p1", "rs-1", "fire_sword")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.playerKnowsRecipeLocked("p1", "fire_sword") {
		t.Fatal("undiscovered recipe shop purchase must be rejected")
	}
}
```

> Before running, confirm the FOW field path in `markRecipeShopDiscovered` matches the real `s.FOW[playerID].KnownBuildings` shape: grep `KnownBuildings` in `server/internal/game/*.go`. `handlePurchaseItemLocked` (state_items.go:293-299) reads `fow.KnownBuildings[building.ID]` via `_, discovered := fow.KnownBuildings[...]`, so the value type is whatever that map holds — match it. If discovery is set through a helper rather than direct map write, use that helper.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/internal/game/ -run TestPurchaseRecipe -v`
Expected: FAIL — `PurchaseRecipe` undefined.

- [ ] **Step 3: Add the protocol message**

In `server/pkg/protocol/messages.go` near `PurchaseItemCommandMessage` (846-850):
```go
// PurchaseRecipeCommandMessage buys one recipe from a Recipe Shop, unlocking it
// for crafting this match.
type PurchaseRecipeCommandMessage struct {
	Type       string `json:"type"`
	BuildingID string `json:"buildingId"`
	RecipeID   string `json:"recipeId"`
}
```

- [ ] **Step 4: Create `state_recipe_purchase.go`**

```go
package game

// PurchaseRecipe is the public entry point for buying a recipe from a Recipe
// Shop. Acquires s.mu and delegates.
func (s *GameState) PurchaseRecipe(playerID, buildingID, recipeID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlePurchaseRecipeLocked(playerID, buildingID, recipeID)
}

// handlePurchaseRecipeLocked validates and executes a recipe purchase from a
// neutral Recipe Shop. Validation failures are silent no-ops (mirrors
// handlePurchaseItemLocked). On success: deduct gold, unlock the recipe for the
// match, decrement shop stock. Must be called under s.mu.
func (s *GameState) handlePurchaseRecipeLocked(playerID, buildingID, recipeID string) {
	player, ok := s.Players[playerID]
	if !ok {
		return
	}
	building := s.getBuildingByIDLocked(buildingID)
	if building == nil || !building.Visible {
		return
	}
	if getMetadataBool(building.Metadata, "underConstruction") {
		return
	}
	if !hasRecipePurchaseCapability(building) {
		return
	}
	// Neutral recipe shops require FOW discovery + not guard-locked, same as
	// item shops.
	if building.OwnerID == nil || *building.OwnerID != neutralPlayerID {
		return
	}
	fow := s.FOW[playerID]
	if fow == nil {
		return
	}
	if _, discovered := fow.KnownBuildings[building.ID]; !discovered {
		return
	}
	if s.shopLockedLocked(building) {
		return
	}
	// Recipe must be in this shop's inventory with stock remaining.
	stockIdx := -1
	for i, e := range building.RecipeInventory {
		if e.RecipeID == recipeID {
			stockIdx = i
			break
		}
	}
	if stockIdx < 0 || building.RecipeInventory[stockIdx].Quantity <= 0 {
		return
	}
	def, ok := getRecipeDef(recipeID)
	if !ok {
		return
	}
	if player.Resources["gold"] < def.CostGold {
		return
	}
	player.Resources["gold"] -= def.CostGold
	s.unlockRecipeForPlayerLocked(player, recipeID)
	building.RecipeInventory[stockIdx].Quantity--
}
```

- [ ] **Step 5: Add the WS dispatch arm**

In `server/internal/ws/handlers.go`, after the `purchase_item` case (~1070) add:

```go
		case "purchase_recipe":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "must join a match before sending commands"})
				continue
			}
			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "match not found"})
				continue
			}
			var msg protocol.PurchaseRecipeCommandMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "invalid purchase_recipe payload"})
				continue
			}
			match.State.PurchaseRecipe(client.PlayerID(), msg.BuildingID, msg.RecipeID)
```

If `isPausableCommand` (handlers.go:290-297) enumerates world-mutating commands, add `"purchase_recipe"` so it's dropped while paused (match `purchase_item`'s treatment — confirm whether `purchase_item` is listed and mirror it).

- [ ] **Step 6: Run tests + build**

Run: `go test ./server/internal/game/ -run TestPurchaseRecipe -v` — PASS. From `server/`: `go build ./...` — clean.

- [ ] **Step 7: Commit**

```bash
git add server/pkg/protocol/messages.go server/internal/game/state_recipe_purchase.go server/internal/game/state_recipe_purchase_test.go server/internal/ws/handlers.go
git commit -m "feat(recipes): purchase_recipe command unlocks a recipe at a Recipe Shop"
```

---

### Task 5: Artificer building + Vault ingredient helpers

**Files:**
- Create: `server/internal/game/catalog/buildings/artificer.json`
- Modify: `server/internal/game/state_items.go` (add vault-by-itemID helpers near `removeItemFromVaultByInstanceLocked` ~line 156)
- Create: `server/internal/game/state_buildings_capability.go` (small helper file)
- Test: `server/internal/game/vault_ingredient_test.go`

**Interfaces:**
- Produces:
  - `func (s *GameState) playerOwnsBuiltCapabilityLocked(playerID, capability string) bool` (generalizes `playerHasMarketplaceLocked`)
  - `func vaultItemCountLocked(player *Player, itemID string) int` (sum of stacks of itemID across vault entries)
  - `func (s *GameState) removeOneItemFromVaultByItemIDLocked(player *Player, itemID string) bool` (removes one unit; decrements a stack or drops the entry)

- [ ] **Step 1: Write the failing test**

```go
package game

import "testing"

func TestVaultIngredientHelpers(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	s.EnsurePlayer("p1")
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.Players["p1"]

	broad, _ := getItemDef("broad_sword")
	ring, _ := getItemDef("fire_ring")
	s.addItemToVaultLocked(p, broad)
	s.addItemToVaultLocked(p, broad)
	s.addItemToVaultLocked(p, ring)

	if got := vaultItemCountLocked(p, "broad_sword"); got != 2 {
		t.Fatalf("broad_sword count = %d, want 2", got)
	}
	if got := vaultItemCountLocked(p, "fire_ring"); got != 1 {
		t.Fatalf("fire_ring count = %d, want 1", got)
	}
	if !s.removeOneItemFromVaultByItemIDLocked(p, "broad_sword") {
		t.Fatal("remove should succeed")
	}
	if got := vaultItemCountLocked(p, "broad_sword"); got != 1 {
		t.Fatalf("after remove: broad_sword count = %d, want 1", got)
	}
	if s.removeOneItemFromVaultByItemIDLocked(p, "no_such_item") {
		t.Fatal("remove of missing item should fail")
	}
}

func TestPlayerOwnsBuiltCapability(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	s.EnsurePlayer("p1")
	s.mu.Lock()
	defer s.mu.Unlock()
	owner := "p1"
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, BuildingTileForTest("art-1", "artificer", &owner, []string{"crafting"}))
	if s.buildingsByID == nil {
		s.buildingsByID = map[string]*BuildingTileAlias{}
	}
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		s.buildingsByID[b.ID] = b
	}
	if !s.playerOwnsBuiltCapabilityLocked("p1", "crafting") {
		t.Fatal("player should own a built crafting building")
	}
	if s.playerOwnsBuiltCapabilityLocked("p2", "crafting") {
		t.Fatal("p2 owns nothing")
	}
}
```

> `BuildingTileForTest`/`BuildingTileAlias` are placeholders for the real `protocol.BuildingTile` construction — write this test using `protocol.BuildingTile{ID, BuildingType, Visible:true, Occupied:true, OwnerID, Capabilities, Metadata:map[string]interface{}{}}` directly and `map[string]*protocol.BuildingTile{}` for `buildingsByID`, mirroring `addRecipeShop`/`addShopBuilding`. Replace the placeholder names with that concrete construction.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/internal/game/ -run 'TestVaultIngredientHelpers|TestPlayerOwnsBuiltCapability' -v`
Expected: FAIL — helpers undefined.

- [ ] **Step 3: Add the vault helpers**

In `server/internal/game/state_items.go`, after `removeItemFromVaultByInstanceLocked` (line 171):

```go
// vaultItemCountLocked returns the total number of units of itemID held in the
// player's vault (summing stacks). Must be called under s.mu.
func vaultItemCountLocked(player *Player, itemID string) int {
	n := 0
	for _, vi := range player.Vault {
		if vi.ItemID == itemID {
			n += vi.Stacks
		}
	}
	return n
}

// removeOneItemFromVaultByItemIDLocked removes a single unit of itemID from the
// player's vault: decrements a stack if one has Stacks>1, else drops the entry.
// Returns false if no matching entry exists. Must be called under s.mu.
func (s *GameState) removeOneItemFromVaultByItemIDLocked(player *Player, itemID string) bool {
	for i, vi := range player.Vault {
		if vi.ItemID != itemID {
			continue
		}
		if vi.Stacks > 1 {
			vi.Stacks--
			return true
		}
		player.Vault = append(player.Vault[:i], player.Vault[i+1:]...)
		return true
	}
	return false
}
```

- [ ] **Step 4: Add the capability helper**

Create `server/internal/game/state_buildings_capability.go`:

```go
package game

import "webrts/server/pkg/protocol"

// playerOwnsBuiltCapabilityLocked reports whether playerID owns at least one
// fully-built (not under-construction), visible building whose capability list
// includes capability. Generalizes playerHasMarketplaceLocked. Must be called
// under s.mu.
func (s *GameState) playerOwnsBuiltCapabilityLocked(playerID, capability string) bool {
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if !b.Visible {
			continue
		}
		if b.OwnerID == nil || *b.OwnerID != playerID {
			continue
		}
		if getMetadataBool(b.Metadata, "underConstruction") {
			continue
		}
		for _, c := range b.Capabilities {
			if c == capability {
				return true
			}
		}
	}
	return false
}

var _ = protocol.BuildingTile{} // keep the import if not otherwise referenced
```

(Remove the trailing `var _` line if `protocol` ends up referenced elsewhere in the file; it's only there to avoid an unused-import error if the helper compiles without it. If `getMetadataBool` already lives in `package game` without needing `protocol` here, drop the import entirely.)

- [ ] **Step 5: Create the Artificer building def**

`server/internal/game/catalog/buildings/artificer.json`:
```json
{
  "type": "artificer",
  "class": "player",
  "buildable": true,
  "width": 3,
  "height": 3,
  "maxHp": 400,
  "buildSeconds": 25,
  "resourceCost": {
    "gold": 200,
    "wood": 150
  },
  "capabilities": ["crafting"],
  "spawnUnitTypes": [],
  "metadata": {},
  "color": "#0e7490",
  "label": "Artificer",
  "hotkey": "f"
}
```

> Confirm hotkey `f` doesn't collide with another buildable building's hotkey: `grep -rn '"hotkey"' server/internal/game/catalog/buildings/`. If `f` is taken, pick a free letter and note it for Plan 3.

- [ ] **Step 6: Run tests + build**

Run: `go test ./server/internal/game/ -run 'TestVaultIngredientHelpers|TestPlayerOwnsBuiltCapability' -v` — PASS. From `server/`: `go build ./...` — clean (the new `artificer` building auto-loads via the embed).

- [ ] **Step 7: Commit**

```bash
git add server/internal/game/catalog/buildings/artificer.json server/internal/game/state_items.go server/internal/game/state_buildings_capability.go server/internal/game/vault_ingredient_test.go
git commit -m "feat(recipes): add Artificer building + vault ingredient and capability helpers"
```

---

### Task 6: `craft_item` command

**Files:**
- Modify: `server/pkg/protocol/messages.go` (add `CraftItemCommandMessage`)
- Create: `server/internal/game/state_crafting.go`
- Modify: `server/internal/ws/handlers.go` (add `case "craft_item":`; `isPausableCommand` if applicable)
- Test: `server/internal/game/state_crafting_test.go`

**Interfaces:**
- Consumes: `getRecipeDef`, `getItemDef`, `playerKnowsRecipeLocked`, `playerOwnsBuiltCapabilityLocked`, `vaultItemCountLocked`, `removeOneItemFromVaultByItemIDLocked`, `addItemToVaultLocked`, the vault capacity check pattern.
- Produces:
  - `protocol.CraftItemCommandMessage struct { Type string; RecipeID string }`
  - `func (s *GameState) CraftItem(playerID, recipeID string) bool` (public, locks; returns whether a craft succeeded — used by the recorder seam in Task 8)
  - `func (s *GameState) handleCraftItemLocked(playerID, recipeID string) bool`

- [ ] **Step 1: Write the failing test**

```go
package game

import "testing"

func setupCraft(t *testing.T, gold int, ingredients []string) (*GameState, *Player) {
	t.Helper()
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 3)
	s.EnsurePlayerWithUpgrades("p1", nil, nil, nil, []string{"fire_sword"})
	s.mu.Lock()
	owner := "p1"
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
		ID: "art-1", BuildingType: "artificer", Visible: true, Occupied: true,
		OwnerID: &owner, Capabilities: []string{"crafting"}, Metadata: map[string]interface{}{},
	})
	if s.buildingsByID == nil {
		s.buildingsByID = map[string]*protocol.BuildingTile{}
	}
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		s.buildingsByID[b.ID] = b
	}
	p := s.Players["p1"]
	p.Resources["gold"] = gold
	for _, id := range ingredients {
		def, _ := getItemDef(id)
		s.addItemToVaultLocked(p, def)
	}
	s.mu.Unlock()
	return s, p
}

func TestCraftItem_Success(t *testing.T) {
	s, p := setupCraft(t, 1000, []string{"broad_sword", "fire_ring"})
	if !s.CraftItem("p1", "fire_sword") {
		t.Fatal("craft should succeed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if vaultItemCountLocked(p, "broad_sword") != 0 || vaultItemCountLocked(p, "fire_ring") != 0 {
		t.Fatal("inputs should be consumed")
	}
	if vaultItemCountLocked(p, "fire_sword") != 1 {
		t.Fatal("output should be added to vault")
	}
	if p.Resources["gold"] != 1000-150 {
		t.Fatalf("gold = %d, want %d", p.Resources["gold"], 850)
	}
}

func TestCraftItem_RejectsMissingIngredient(t *testing.T) {
	s, p := setupCraft(t, 1000, []string{"broad_sword"}) // no fire_ring
	if s.CraftItem("p1", "fire_sword") {
		t.Fatal("craft must fail without all ingredients")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if vaultItemCountLocked(p, "broad_sword") != 1 {
		t.Fatal("inputs must NOT be consumed on a failed craft")
	}
	if p.Resources["gold"] != 1000 {
		t.Fatal("gold must be unchanged on a failed craft")
	}
}

func TestCraftItem_RejectsUnknownRecipe(t *testing.T) {
	s, _ := setupCraft(t, 1000, []string{"broad_sword", "ice_ring"})
	// frost_sword is craftable by ingredients but NOT in the player's unlocked set.
	if s.CraftItem("p1", "frost_sword") {
		t.Fatal("craft must fail for a recipe the player hasn't unlocked")
	}
}

func TestCraftItem_RejectsNoArtificer(t *testing.T) {
	s, _ := setupCraft(t, 1000, []string{"broad_sword", "fire_ring"})
	s.mu.Lock()
	// Remove the artificer.
	s.MapConfig.Buildings = s.MapConfig.Buildings[:0]
	for k := range s.buildingsByID {
		delete(s.buildingsByID, k)
	}
	s.mu.Unlock()
	if s.CraftItem("p1", "fire_sword") {
		t.Fatal("craft must fail without a built Artificer")
	}
}
```

(Add `import "webrts/server/pkg/protocol"` to the test file.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/internal/game/ -run TestCraftItem -v`
Expected: FAIL — `CraftItem` undefined.

- [ ] **Step 3: Add the protocol message**

```go
// CraftItemCommandMessage crafts one recipe at the player's Artificer,
// consuming the recipe's input items from the vault plus gold.
type CraftItemCommandMessage struct {
	Type     string `json:"type"`
	RecipeID string `json:"recipeId"`
}
```

- [ ] **Step 4: Create `state_crafting.go`**

```go
package game

// CraftItem is the public entry point for crafting a recipe at an Artificer.
// Acquires s.mu, delegates, and returns whether a craft succeeded. The boolean
// lets the caller (WS handler) fire the account-wide recipe-record seam only on
// success.
func (s *GameState) CraftItem(playerID, recipeID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handleCraftItemLocked(playerID, recipeID)
}

// handleCraftItemLocked validates and executes a craft. Returns true on success.
// Validation failures are silent no-ops (return false). On success: consume one
// of each input item from the vault, deduct gold, add the output item to the
// vault. Must be called under s.mu.
func (s *GameState) handleCraftItemLocked(playerID, recipeID string) bool {
	player, ok := s.Players[playerID]
	if !ok {
		return false
	}
	// Must own a built Artificer.
	if !s.playerOwnsBuiltCapabilityLocked(playerID, "crafting") {
		return false
	}
	// Recipe must be unlocked this match.
	if !s.playerKnowsRecipeLocked(playerID, recipeID) {
		return false
	}
	def, ok := getRecipeDef(recipeID)
	if !ok {
		return false
	}
	outDef, ok := getItemDef(def.Output)
	if !ok {
		return false
	}
	// Afford gold.
	if player.Resources["gold"] < def.CostGold {
		return false
	}
	// Vault must contain every input, accounting for duplicates (e.g. a recipe
	// that needs 2 of the same item).
	needed := make(map[string]int, len(def.Inputs))
	for _, in := range def.Inputs {
		needed[in]++
	}
	for itemID, count := range needed {
		if vaultItemCountLocked(player, itemID) < count {
			return false
		}
	}
	// Vault must have room for the output. Crafted swords are equipment (always
	// need a free slot). The net change is (remove >=2 inputs, add 1 output), so
	// capacity never actually grows — but check defensively against the post-
	// removal state by verifying capacity > 0 after removals would be simplest.
	// Inputs are removed first (freeing slots), so an equipment output always
	// fits: removing >=2 entries then adding 1 cannot exceed capacity. We still
	// guard addItemToVaultLocked's return below.

	// Consume inputs.
	for itemID, count := range needed {
		for k := 0; k < count; k++ {
			if !s.removeOneItemFromVaultByItemIDLocked(player, itemID) {
				// Should never happen (counts were verified above); abort safely.
				return false
			}
		}
	}
	// Deduct gold.
	player.Resources["gold"] -= def.CostGold
	// Produce output.
	if !s.addItemToVaultLocked(player, outDef) {
		// Extremely unlikely (we just freed >=2 slots). Refund to avoid losing
		// the player's gold + items on a capacity edge case.
		player.Resources["gold"] += def.CostGold
		for itemID, count := range needed {
			for k := 0; k < count; k++ {
				inDef, _ := getItemDef(itemID)
				s.addItemToVaultLocked(player, inDef)
			}
		}
		return false
	}
	return true
}
```

> Note on map iteration: `needed` is iterated to consume inputs, but the *outcome* (which items removed, gold spent, output added) does not depend on iteration order — every input is consumed exactly `count` times regardless of order — so this does not violate the determinism constraint. (The constraint forbids iteration order *driving outcomes*, e.g. picking one of several.)

- [ ] **Step 5: Add the WS dispatch arm**

In `handlers.go`, after the `purchase_recipe` case:

```go
		case "craft_item":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "must join a match before sending commands"})
				continue
			}
			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "match not found"})
				continue
			}
			var msg protocol.CraftItemCommandMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "invalid craft_item payload"})
				continue
			}
			match.State.CraftItem(client.PlayerID(), msg.RecipeID)
```

(The success-bool return is wired to the profile recorder in Task 8 — for now the return value is intentionally ignored here.)

Add `"craft_item"` to `isPausableCommand` if it lists world-mutating commands.

- [ ] **Step 6: Run tests + build**

Run: `go test ./server/internal/game/ -run TestCraftItem -v` — PASS (all four). From `server/`: `go build ./...` — clean.

- [ ] **Step 7: Commit**

```bash
git add server/pkg/protocol/messages.go server/internal/game/state_crafting.go server/internal/game/state_crafting_test.go server/internal/ws/handlers.go
git commit -m "feat(recipes): craft_item command consumes vault ingredients + gold for the output item"
```

---

### Task 7: Profile `KnownRecipeIDs` field + v7→v8 migration + recorder method

**Files:**
- Modify: `server/internal/profile/types.go` (`CurrentVersion` 7→8; add `KnownRecipeIDs`)
- Modify: `server/internal/profile/store.go` (`migrateProfile` init)
- Modify: `server/internal/profile/manager.go` (add `RecordKnownRecipe`; init field in `newDefaultProfile`)
- Test: `server/internal/profile/recipe_profile_test.go`

**Interfaces:**
- Produces:
  - `PlayerProfile.KnownRecipeIDs []string` (JSON `knownRecipeIds`)
  - `func (m *Manager) RecordKnownRecipe(playerID, recipeID string) error` — idempotent, sorted append via `WithLocked`.

- [ ] **Step 1: Write the failing test**

```go
package profile

import "testing"

func TestMigrate_InitsKnownRecipeIDs(t *testing.T) {
	p := &PlayerProfile{Version: 7} // a v7 profile with the field absent
	migrateProfile(p)
	if p.KnownRecipeIDs == nil {
		t.Fatal("migration should initialize KnownRecipeIDs to non-nil")
	}
	if p.Version != CurrentVersion {
		t.Fatalf("version = %d, want %d", p.Version, CurrentVersion)
	}
	if CurrentVersion != 8 {
		t.Fatalf("CurrentVersion = %d, want 8", CurrentVersion)
	}
}

func TestRecordKnownRecipe_Idempotent(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	pid := "00000000-0000-0000-0000-000000000001"
	if err := m.RecordKnownRecipe(pid, "fire_sword"); err != nil {
		t.Fatal(err)
	}
	if err := m.RecordKnownRecipe(pid, "fire_sword"); err != nil {
		t.Fatal(err)
	}
	if err := m.RecordKnownRecipe(pid, "frost_sword"); err != nil {
		t.Fatal(err)
	}
	p, err := m.Get(pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.KnownRecipeIDs) != 2 {
		t.Fatalf("KnownRecipeIDs = %v, want 2 unique", p.KnownRecipeIDs)
	}
	// Sorted.
	if p.KnownRecipeIDs[0] != "fire_sword" || p.KnownRecipeIDs[1] != "frost_sword" {
		t.Fatalf("KnownRecipeIDs not sorted: %v", p.KnownRecipeIDs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/internal/profile/ -run 'TestMigrate_InitsKnownRecipeIDs|TestRecordKnownRecipe_Idempotent' -v`
Expected: FAIL — `KnownRecipeIDs`/`RecordKnownRecipe` undefined and `CurrentVersion` is 7.

- [ ] **Step 3: Add the field + bump version**

In `server/internal/profile/types.go`:
- Change `const CurrentVersion = 7` → `const CurrentVersion = 8`.
- Add to `PlayerProfile` (after `CreditedMatchIDs`):
```go
	// KnownRecipeIDs is the set of crafting recipe IDs this player has crafted
	// at least once, unlocking them for crafting in all future matches. Added in
	// schema version 8. Sorted, deduped. nil == empty.
	KnownRecipeIDs []string `json:"knownRecipeIds"`
```

- [ ] **Step 4: Migration + default init**

In `store.go` `migrateProfile`, before the `p.Version = CurrentVersion` stamp:
```go
	// v7 -> v8: initialize KnownRecipeIDs (recipe crafting unlock ledger).
	if p.KnownRecipeIDs == nil {
		p.KnownRecipeIDs = []string{}
	}
```

In `manager.go` `newDefaultProfile`, add to the struct literal:
```go
		KnownRecipeIDs: []string{},
```

- [ ] **Step 5: Add `RecordKnownRecipe`**

In `manager.go` (after `CommitDominionPoints`):
```go
// RecordKnownRecipe records recipeID into the player's permanent KnownRecipeIDs
// set (idempotent, sorted). Called fire-and-forget after a successful craft so
// the recipe is craftable in all future matches. No-op on empty recipeID.
func (m *Manager) RecordKnownRecipe(playerID, recipeID string) error {
	if recipeID == "" {
		return nil
	}
	return m.WithLocked(playerID, func(p *PlayerProfile) error {
		for _, id := range p.KnownRecipeIDs {
			if id == recipeID {
				return nil // already known
			}
		}
		p.KnownRecipeIDs = append(p.KnownRecipeIDs, recipeID)
		sort.Strings(p.KnownRecipeIDs)
		return nil
	})
}
```

Add `"sort"` to `manager.go` imports.

- [ ] **Step 6: Run tests**

Run: `go test ./server/internal/profile/ -run 'TestMigrate_InitsKnownRecipeIDs|TestRecordKnownRecipe_Idempotent' -v` — PASS. Then `go test ./server/internal/profile/ -count=1` — full profile package green (no migration regressions).

- [ ] **Step 7: Commit**

```bash
git add server/internal/profile/types.go server/internal/profile/store.go server/internal/profile/manager.go server/internal/profile/recipe_profile_test.go
git commit -m "feat(profile): add KnownRecipeIDs with v7->v8 migration and RecordKnownRecipe"
```

---

### Task 8: Wire craft → profile (RecipeRecorder seam) + expose `KnownRecipeIDs` over the profile API

**Files:**
- Modify: `server/internal/game/manager.go` (add `RecipeRecorder` interface + `MatchManager` field + setter; wire a `GameState` handler in `newMatchLocked`)
- Modify: `server/internal/game/state.go` (add `recipeCraftedHandler func(playerID, recipeID string)` field + `SetRecipeCraftedHandler`)
- Modify: `server/internal/game/state_crafting.go` (invoke the handler on success, fire-and-forget)
- Modify: `server/cmd/api/main.go` (`manager.SetRecipeRecorder(profileManager)`)
- Modify: the profile HTTP GET handler (read-first; grep `profile_handlers.go`) to include `KnownRecipeIDs` in the response so the client can send it at join
- Test: `server/internal/game/recipe_recorder_test.go`

**Interfaces:**
- Consumes: `RecordKnownRecipe` (Task 7), `CraftItem` (Task 6).
- Produces:
  - `type RecipeRecorder interface { RecordKnownRecipe(playerID, recipeID string) error }` (declared in `package game` so it does not import `profile`)
  - `MatchManager.SetRecipeRecorder(r RecipeRecorder)`
  - `GameState.SetRecipeCraftedHandler(func(playerID, recipeID string))` + the handler invoked on a successful craft.

- [ ] **Step 1: Write the failing test**

```go
package game

import (
	"sync"
	"testing"
)

func TestCraft_FiresRecipeCraftedHandler(t *testing.T) {
	s, _ := setupCraft(t, 1000, []string{"broad_sword", "fire_ring"}) // helper from Task 6 test file

	var mu sync.Mutex
	var got [][2]string
	done := make(chan struct{}, 1)
	s.SetRecipeCraftedHandler(func(playerID, recipeID string) {
		mu.Lock()
		got = append(got, [2]string{playerID, recipeID})
		mu.Unlock()
		select {
		case done <- struct{}{}:
		default:
		}
	})

	if !s.CraftItem("p1", "fire_sword") {
		t.Fatal("craft should succeed")
	}
	// Handler is fire-and-forget (may run in a goroutine); wait briefly.
	<-done
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0][0] != "p1" || got[0][1] != "fire_sword" {
		t.Fatalf("handler got %v, want one (p1, fire_sword)", got)
	}
}

func TestCraft_NoHandlerFireOnFailure(t *testing.T) {
	s, _ := setupCraft(t, 10, []string{"broad_sword", "fire_ring"}) // gold 10 < 150
	fired := false
	s.SetRecipeCraftedHandler(func(playerID, recipeID string) { fired = true })
	if s.CraftItem("p1", "fire_sword") {
		t.Fatal("craft should fail (unaffordable)")
	}
	if fired {
		t.Fatal("handler must not fire on a failed craft")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/internal/game/ -run 'TestCraft_FiresRecipeCraftedHandler|TestCraft_NoHandlerFireOnFailure' -v`
Expected: FAIL — `SetRecipeCraftedHandler` undefined.

- [ ] **Step 3: Add the handler field + setter on GameState**

In `server/internal/game/state.go`, add a field to `GameState` (near the existing immediate-dominion handler field — grep `SetImmediateDominionPointDropHandler` to find it) and a setter:

```go
// recipeCraftedHandler is invoked (fire-and-forget) after a successful craft so
// the recipe can be recorded to the player's persistent profile. nil in tests
// that don't set it. Off the tick path by construction (craft is a command).
recipeCraftedHandler func(playerID, recipeID string)
```
```go
// SetRecipeCraftedHandler installs the post-craft callback. Safe to call once at
// match construction.
func (s *GameState) SetRecipeCraftedHandler(fn func(playerID, recipeID string)) {
	s.recipeCraftedHandler = fn
}
```

- [ ] **Step 4: Invoke it on successful craft**

In `state_crafting.go` `handleCraftItemLocked`, just before `return true`, capture the handler and fire it without blocking under the lock:

```go
	if s.recipeCraftedHandler != nil {
		handler := s.recipeCraftedHandler
		go handler(playerID, recipeID)
	}
	return true
```

(Spawning a goroutine keeps the profile disk write off the caller's path and out from under `s.mu`, matching the fire-and-forget invariant. The handler closes over only the two string args — no `*Unit`/state pointer escapes.)

- [ ] **Step 5: Add the manager interface + setter + wiring**

In `server/internal/game/manager.go`, near `DominionPointCommitter` (line 16):
```go
// RecipeRecorder persists a crafted recipe into a player's profile. Declared in
// package game so the game package does not import profile (the *profile.Manager
// satisfies it). Mirrors DominionPointCommitter.
type RecipeRecorder interface {
	RecordKnownRecipe(playerID, recipeID string) error
}
```
Add a field + setter to `MatchManager` (mirror `committer`/`SetDominionPointCommitter`, lines 24/37):
```go
	recipeRecorder RecipeRecorder
```
```go
func (m *MatchManager) SetRecipeRecorder(r RecipeRecorder) { m.recipeRecorder = r }
func (m *MatchManager) getRecipeRecorder() RecipeRecorder  { return m.recipeRecorder }
```
In `newMatchLocked`, where `SetImmediateDominionPointDropHandler` is installed (manager.go:66-77), install the craft handler the same way:
```go
	match.State.SetRecipeCraftedHandler(func(playerID, recipeID string) {
		recorder := m.getRecipeRecorder()
		if recorder == nil {
			return
		}
		if err := recorder.RecordKnownRecipe(playerID, recipeID); err != nil {
			slog.Warn("RecordKnownRecipe failed", "playerID", playerID, "recipeID", recipeID, "err", err)
		}
	})
```
(The handler is already invoked via `go` inside `handleCraftItemLocked`, so this closure runs off-thread; no extra goroutine here. Ensure `slog` is imported in manager.go — it likely is.)

- [ ] **Step 6: Wire the concrete recorder in main.go**

In `server/cmd/api/main.go`, after `manager.SetDominionPointCommitter(profileManager)` (line 51):
```go
	manager.SetRecipeRecorder(profileManager)
```
(`*profile.Manager` already satisfies `RecipeRecorder` via Task 7's `RecordKnownRecipe`.)

- [ ] **Step 7: Expose `KnownRecipeIDs` over the profile GET API**

Grep `server/internal/http/profile_handlers.go` for the GET-profile response builder (the handler that serializes a `PlayerProfile` or a DTO for `GET /api/profile/...`). If it returns the `PlayerProfile` directly, `KnownRecipeIDs` already serializes (json tag from Task 7) — add an assertion-only test or confirm by reading the handler. If it maps to a response DTO, add a `KnownRecipeIDs []string \`json:"knownRecipeIds"\`` field to that DTO and copy it from the profile. Make the minimal change so the client (Plan 3) can read `knownRecipeIds` from the profile endpoint and echo it in `JoinMatchMessage`.

> This is a read-first step: open `profile_handlers.go`, find the GET handler, and make the smallest change that surfaces `KnownRecipeIDs`. If it already returns the raw profile, no code change is needed — note that in the commit message.

- [ ] **Step 8: Run tests + build**

Run: `go test ./server/internal/game/ -run 'TestCraft_FiresRecipeCraftedHandler|TestCraft_NoHandlerFireOnFailure' -v` — PASS. From `server/`: `go build ./...` — clean (main.go + manager wiring compile).

- [ ] **Step 9: Commit**

```bash
git add -A server/internal/game server/cmd/api/main.go server/internal/http
git commit -m "feat(recipes): record first craft to profile KnownRecipeIDs (fire-and-forget) + expose via profile API"
```

---

### Task 9: Full-package verification

**Files:** none (verification only).

- [ ] **Step 1: Game package suite**

Run (from `server/`): `go test ./internal/game/ -count=1`
Expected: PASS.

- [ ] **Step 2: Profile package suite**

Run: `go test ./internal/profile/ -count=1`
Expected: PASS.

- [ ] **Step 3: Build everything**

Run: `go build ./...`
Expected: clean.

- [ ] **Step 4: Broader suite (note pre-existing failures)**

Run: `go test ./... -count=1`
Expected: PASS except the known pre-existing `internal/ws` `TestSPBaseline_StructuralShape` (a stale golden baseline that also fails on `main`). If THIS branch's snapshot additions (`RecipeInventory`, `UnlockedRecipeIDs`) changed the SP baseline shape, that test may now drift differently — if so, regenerate the baseline intentionally with `go test -run TestSPBaseline_StructuralShape -update ./internal/ws/...` and review the diff to confirm it only adds the new optional fields (both are `omitempty`, so they should NOT appear unless a recipe shop / unlocked recipe is present in the baseline fixture). Note any baseline regeneration in the commit.

- [ ] **Step 5: Commit (only if a baseline regen or incidental fix was needed)**

```bash
git add -A
git commit -m "test: regenerate SP baseline for recipe snapshot fields"
```

---

## Self-review checklist (completed during authoring)

- **Spec coverage:** Recipe catalog + 2+ inputs + gold (T1); Recipe Shop random subset per match (T2); account-wide unlock seeding (T3) + first-craft persistence (T7/T8); buy at neutral Recipe Shop (T4); Artificer player building + craft consumes vault + gold (T5/T6); v7→v8 migration (T7). Client UI + threading `knownRecipeIds` into `JoinMatchMessage` is explicitly Plan 3.
- **Placeholders:** the three read-first steps (T2 step 6 population call site, T4 FOW field path, T8 step 7 profile DTO) are flagged with the exact grep to run and the concrete neighboring pattern to mirror — not vague "handle it" directions. The T5 test uses real `protocol.BuildingTile` construction (the placeholder helper names are explicitly called out to replace).
- **Type consistency:** `RecipeDef`/`getRecipeDef`/`ListRecipeDefs`; `RecipeStockEntry`/`BuildingTile.RecipeInventory`; `Player.UnlockedRecipeIDs`/`playerKnowsRecipeLocked`/`unlockRecipeForPlayerLocked`; `PurchaseRecipe`/`handlePurchaseRecipeLocked`; `CraftItem`/`handleCraftItemLocked`; `KnownRecipeIDs`/`RecordKnownRecipe`/`RecipeRecorder`/`SetRecipeCraftedHandler` — names match across tasks.
- **Determinism:** the only randomness (T2 recipe sample) uses `s.rngLoot` over a sorted recipe list with sorted building iteration; the craft input-consumption map iteration does not drive outcomes (noted inline).
- **Naming carried from Plan 1:** recipes output `frost_sword` (not `ice_sword`).
