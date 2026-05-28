# Neutral Camp Loot Drops Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a treasure-chest loot system that rewards clearing a neutral camp. When the last unit of a camp dies, the server rolls the camp's `loot_table` against `s.rngLoot`. On a hit, a persistent chest spawns at the camp center. Players right-click the chest with a selected unit; the unit walks over, the chest despawns, and contents go to vault + resources. Hovering the chest shows a tooltip with the pre-rolled contents.

**Architecture:** New world entity `LootDrop` with its own snapshot. Loot contents are catalog-driven JSON (`loot_tables.json`) with two packaged-item kinds (resource bundle, item sub-table) and d100 outer rolls. New `OrderPickupLoot` mirrors the existing gather flow. Loot is pre-rolled at kill time, stored on the chest, granted at pickup — keeps replay determinism input-timing-independent.

**Tech Stack:** Go (server, embed.FS catalog, existing `s.rngLoot`), Vue 3 + TypeScript (client rendering / input / tooltip / HUD toast), shared protocol JSON.

**Repo conventions:**
- [c:\Personal Dev\webrts\CLAUDE.md](../../../CLAUDE.md) — project rules
- [c:\Personal Dev\webrts\.claude\rules\AI_RULES.md](../../../.claude/rules/AI_RULES.md) — ID-based targeting, `*Locked` suffix, determinism
- Design doc: [docs/superpowers/specs/2026-05-27-neutral-camp-loot-drops-design.md](../specs/2026-05-27-neutral-camp-loot-drops-design.md)

**Commit policy:** **Do not run `git add` or `git commit` at any point.** The user handles all commits. Write/edit files only.

**Test commands (Windows / PowerShell):**
- Single Go test: `go test ./server/internal/game -run TestLootTableLoader_Loads -v` (from project root)
- Package: `go test ./server/internal/game -v`
- Server build: `go build ./server/...`
- Client type-check: from `client/src/game-portal`, `npx vue-tsc --noEmit`

---

## File Map (high-level)

**New files:**
- `server/internal/game/catalog/neutral_groups/loot_tables.json`
- `server/internal/game/loot_table_defs.go` + `loot_table_defs_test.go`
- `server/internal/game/state_loot_drops.go` + `state_loot_drops_test.go`
- `server/internal/game/state_loot_pickup.go` + `state_loot_pickup_test.go`

**Modified files:**
- `server/internal/game/catalog/neutral_groups/tier_1.json` — add `lootTable` to one group
- `server/internal/game/neutral_group_defs.go` — `LootTable` field + validation
- `server/internal/game/state_neutral_camps.go` — `SpawnedGroupID`, wipe-trigger hook, despawn state ordering
- `server/internal/game/state.go` — `OrderPickupLoot` + string mapping; `Unit.PickupLootID`; `GameState.LootDrops` + `nextLootDropID`; insert `tickLootDropsLocked` in tick order; clear `PickupLootID` in all order-replacement sites
- `server/internal/game/state_items.go` — `grantResourceToPlayerLocked` helper
- `server/internal/game/state_movement.go`, `state_workers.go` — clear `unit.PickupLootID = ""` alongside existing sticky-target clears
- `server/internal/ws/handlers.go` — new `"pickup_loot_command"` case
- `server/pkg/protocol/messages.go` — new wire types + snapshot fields
- `client/src/game-portal/src/game/network/protocol.ts` — mirrored types
- `client/src/game-portal/src/game/core/GameState.ts` — `lootDrops` collection + apply
- `client/src/game-portal/src/game/network/NetworkClient.ts` — `sendPickupLootCommand`
- `client/src/game-portal/src/game/rendering/*` — chest sprite layer + minimap POI
- `client/src/game-portal/src/game/input/*` — right-click & hover hit-test
- `client/src/game-portal/src/components/*` — tooltip + HUD toast

---

## Task 1: Author the example loot-table catalog file

**Files:**
- Create: `server/internal/game/catalog/neutral_groups/loot_tables.json`

- [ ] **Step 1: Confirm `broad_sword` and `scimitar` exist in the item catalog**

Run: `ls "server/internal/game/catalog/items/equipment/common/"`
Expected: `broad_sword.json` and other common-tier items. If `scimitar.json` doesn't ship today, substitute another existing common-tier weapon id and note the substitution in your report. The plan downstream tests derive names from the catalog so substitutions are absorbed without further changes.

- [ ] **Step 2: Write the file**

Path: `server/internal/game/catalog/neutral_groups/loot_tables.json`

```json
{
  "packagedItems": {
    "small_resource_bundle": {
      "kind": "resource_bundle",
      "resources": { "gold": 50, "wood": 15 }
    },
    "medium_resource_bundle": {
      "kind": "resource_bundle",
      "resources": { "gold": 100, "wood": 45 }
    },
    "basic_weapons": {
      "kind": "item_subtable",
      "entries": [
        { "item": "broad_sword", "min": 1, "max": 10 },
        { "item": "scimitar",    "min": 11, "max": 15 }
      ]
    }
  },
  "tables": {
    "raider_loot": [
      { "entry": "small_resource_bundle", "min": 1,  "max": 10 },
      { "entry": "basic_weapons",         "min": 11, "max": 20 }
    ]
  }
}
```

(If you substituted weapon ids in Step 1, use those.)

---

## Task 2: Add `LootTable` to `NeutralGroup` + `SpawnedGroupID` to `NeutralCamp`

**Files:**
- Modify: `server/internal/game/neutral_group_defs.go`
- Modify: `server/internal/game/state_neutral_camps.go`
- Modify: `server/internal/game/catalog/neutral_groups/tier_1.json`

- [ ] **Step 1: Add `LootTable` field to `NeutralGroup`**

In [neutral_group_defs.go](../../../server/internal/game/neutral_group_defs.go), find `type NeutralGroup struct` and add:

```go
type NeutralGroup struct {
    ID          string                         `json:"id"`
    Name        string                         `json:"name"`
    Composition []NeutralGroupCompositionEntry `json:"composition"`
    // LootTable is an optional reference into loot_tables.json. Empty
    // means the camp drops nothing on wipe. Non-empty values are
    // validated at catalog load — a missing table id panics, matching
    // the unit-type validation pattern in this loader.
    LootTable string `json:"lootTable,omitempty"`
}
```

- [ ] **Step 2: Validate `LootTable` references at catalog load**

In `loadNeutralGroupsByTier`, within the per-group validation loop (after the composition entries are validated), add:

```go
if g.LootTable != "" {
    if _, ok := getLootTable(g.LootTable); !ok {
        panic(rel + ": group " + g.ID + ` references unknown lootTable "` + g.LootTable + `"`)
    }
}
```

(`getLootTable` will exist after Task 4. Until then this validation will fail to compile — that's fine, just leave it. Tasks ordered so the compile is clean at the end of Task 4.)

- [ ] **Step 3: Add `SpawnedGroupID` to `NeutralCamp`**

In [state_neutral_camps.go](../../../server/internal/game/state_neutral_camps.go), add a field to `NeutralCamp`:

```go
// SpawnedGroupID is the id of the group rolled at the most recent
// spawnGroupForCampLocked call. Used at wipe time to find the right
// loot_table — even when GroupID is the "__random__" sentinel.
// Empty before the first spawn.
SpawnedGroupID string
```

- [ ] **Step 4: Record `SpawnedGroupID` after a successful group resolve**

In `spawnGroupForCampLocked`, after the group has been resolved (specific or random) and `ok` is true, before the spawn loop, add:

```go
camp.SpawnedGroupID = group.ID
```

- [ ] **Step 5: Author a loot-table reference on a shipped group**

Open [server/internal/game/catalog/neutral_groups/tier_1.json](../../../server/internal/game/catalog/neutral_groups/tier_1.json) and add `"lootTable": "raider_loot"` to one of the shipped groups, e.g.:

```json
{
  "id": "small_raider_group",
  "name": "Small Raider Group",
  "lootTable": "raider_loot",
  "composition": [
    { "unitType": "raider", "count": 2 },
    { "unitType": "ranged_raider", "count": 2 }
  ]
}
```

- [ ] **Step 6: Verify build is still BROKEN (Tasks 3-4 fix it)**

Run: `go build ./server/...`
Expected: FAIL with `undefined: getLootTable`. This confirms Step 2 is in place; the next two tasks build the loader.

---

## Task 3: Loader — failing tests

**Files:**
- Create: `server/internal/game/loot_table_defs_test.go`

- [ ] **Step 1: Write the test file (build is still broken; that's the expected red state)**

```go
package game

import "testing"

// TestLootTableLoader_Loads pins the structural invariants of the shipped
// loot_tables.json. Deliberately does NOT pin specific drop chances or
// quantities — those are balance content. Derives expectations from the
// loaded data.
func TestLootTableLoader_Loads(t *testing.T) {
    table, ok := getLootTable("raider_loot")
    if !ok {
        t.Fatalf("expected raider_loot table to be loaded")
    }
    if len(table) == 0 {
        t.Fatalf("raider_loot table has zero entries")
    }
    for _, e := range table {
        if e.Min < 1 || e.Max < e.Min || e.Max > 100 {
            t.Errorf("raider_loot entry %q: invalid range [%d,%d]", e.Entry, e.Min, e.Max)
        }
        if _, ok := getPackagedItem(e.Entry); !ok {
            t.Errorf("raider_loot entry references unknown packaged item %q", e.Entry)
        }
    }
}

// TestLootTableLoader_PackagedItems pins that each packaged item parses to
// one of the two known kinds and references existing item defs (when a
// sub-table).
func TestLootTableLoader_PackagedItems(t *testing.T) {
    bundle, ok := getPackagedItem("small_resource_bundle")
    if !ok {
        t.Fatalf("small_resource_bundle missing")
    }
    if bundle.Kind != PackagedItemResourceBundle {
        t.Errorf("small_resource_bundle kind = %v, want resource_bundle", bundle.Kind)
    }
    if len(bundle.Resources) == 0 {
        t.Errorf("small_resource_bundle has no resources")
    }
    for k, v := range bundle.Resources {
        if k != "gold" && k != "wood" {
            t.Errorf("small_resource_bundle has unexpected resource key %q", k)
        }
        if v <= 0 {
            t.Errorf("small_resource_bundle.%s = %d, want > 0", k, v)
        }
    }

    weapons, ok := getPackagedItem("basic_weapons")
    if !ok {
        t.Fatalf("basic_weapons missing")
    }
    if weapons.Kind != PackagedItemSubtable {
        t.Errorf("basic_weapons kind = %v, want item_subtable", weapons.Kind)
    }
    if len(weapons.Entries) == 0 {
        t.Errorf("basic_weapons has zero sub-table entries")
    }
    for i, e := range weapons.Entries {
        if e.Min < 1 || e.Max < e.Min {
            t.Errorf("basic_weapons[%d] invalid range [%d,%d]", i, e.Min, e.Max)
        }
        // Sub-table item ids must resolve through the existing item catalog
        // (validated at load time; this asserts the assertion).
        if _, ok := getItemDef(e.Item); !ok {
            t.Errorf("basic_weapons[%d] references unknown item %q", i, e.Item)
        }
    }
}

// TestLootTableLoader_UnknownLookups verifies miss paths return (zero, false).
func TestLootTableLoader_UnknownLookups(t *testing.T) {
    if _, ok := getLootTable("nope_not_a_table"); ok {
        t.Errorf("expected miss on unknown table id")
    }
    if _, ok := getPackagedItem("nope_not_an_item"); ok {
        t.Errorf("expected miss on unknown packaged-item id")
    }
}
```

**`getItemDef` already exists** in `items.go` (the item catalog accessor). If the test references the wrong name, fix to the actual public accessor for `ItemDef` lookups.

- [ ] **Step 2: Confirm tests fail to compile**

Run: `go test ./server/internal/game -run TestLootTableLoader -v`
Expected: build failure citing `undefined: getLootTable`, `undefined: PackagedItemResourceBundle`, etc.

---

## Task 4: Loader — implementation

**Files:**
- Create: `server/internal/game/loot_table_defs.go`

- [ ] **Step 1: Write the loader**

Mirror the existing `neutral_group_defs.go` pattern: var-initializer + `//go:embed`, panic-on-bad-data, immutable after load.

```go
package game

import (
    "embed"
    "encoding/json"
    "sort"
    "strconv"
)

// Embeds the loot-table catalog. Single file because there's exactly one
// schema and designers tune drop rates as percentages — having one file
// keeps the diff surface tight. New loot tables are added by appending
// JSON keys, not new files.
//
// Schema rules (panic at load on violation):
//   - packagedItems[id].kind ∈ {"resource_bundle","item_subtable"}
//   - resource_bundle.resources keys ∈ {"gold","wood"} (whitelist),
//     amounts > 0
//   - item_subtable.entries[*].item must resolve in the item catalog;
//     min >= 1, max >= min, ranges may not overlap
//   - tables[*][*] entries: entry must resolve in packagedItems;
//     min >= 1, max <= 100, max >= min, ranges may not overlap
//
// See loot_tables.json for the canonical content example.

//go:embed catalog/neutral_groups/loot_tables.json
var lootTablesFS embed.FS

type PackagedItemKind string

const (
    PackagedItemResourceBundle PackagedItemKind = "resource_bundle"
    PackagedItemSubtable       PackagedItemKind = "item_subtable"
)

// LootTableEntry is one row in a top-level loot table.
type LootTableEntry struct {
    Entry string `json:"entry"`
    Min   int    `json:"min"`
    Max   int    `json:"max"`
}

// LootSubtableEntry is one row in a sub-table (item_subtable kind).
type LootSubtableEntry struct {
    Item string `json:"item"`
    Min  int    `json:"min"`
    Max  int    `json:"max"`
}

// PackagedItem is one entry in packagedItems. Only the fields matching
// Kind are populated; the other is zero.
type PackagedItem struct {
    Kind      PackagedItemKind
    Resources map[string]int      // resource_bundle only
    Entries   []LootSubtableEntry // item_subtable only
}

// LootTableDef is the ordered list of entries for one table.
type LootTableDef = []LootTableEntry

type rawPackagedItem struct {
    Kind      PackagedItemKind    `json:"kind"`
    Resources map[string]int      `json:"resources"`
    Entries   []LootSubtableEntry `json:"entries"`
}

type rawLootCatalog struct {
    PackagedItems map[string]rawPackagedItem `json:"packagedItems"`
    Tables        map[string][]LootTableEntry `json:"tables"`
}

var (
    packagedItemsByID = map[string]PackagedItem{}
    lootTablesByID    = map[string]LootTableDef{}
)

// Whitelist of accepted resource keys. New keys require a code change so
// designers don't typo-grant a phantom resource.
var validLootResourceKeys = map[string]struct{}{
    "gold": {},
    "wood": {},
}

func init() {
    loadLootTableCatalog()
}

func loadLootTableCatalog() {
    rel := "catalog/neutral_groups/loot_tables.json"
    data, err := lootTablesFS.ReadFile(rel)
    if err != nil {
        // File not embedded — feature is effectively disabled but the
        // server still boots. Return without populating the maps.
        return
    }
    var raw rawLootCatalog
    if err := json.Unmarshal(data, &raw); err != nil {
        panic(rel + ": " + err.Error())
    }

    // Validate and store packaged items first; tables reference them.
    for id, pkg := range raw.PackagedItems {
        if id == "" {
            panic(rel + ": packaged item with empty id")
        }
        switch pkg.Kind {
        case PackagedItemResourceBundle:
            if len(pkg.Resources) == 0 {
                panic(rel + ": " + id + ": resource_bundle has no resources")
            }
            for k, v := range pkg.Resources {
                if _, ok := validLootResourceKeys[k]; !ok {
                    panic(rel + ": " + id + ": resource key " + k + " not in whitelist")
                }
                if v <= 0 {
                    panic(rel + ": " + id + "." + k + " = " + strconv.Itoa(v) + " (must be > 0)")
                }
            }
            packagedItemsByID[id] = PackagedItem{
                Kind:      PackagedItemResourceBundle,
                Resources: pkg.Resources,
            }
        case PackagedItemSubtable:
            if len(pkg.Entries) == 0 {
                panic(rel + ": " + id + ": item_subtable has no entries")
            }
            validateSubtableRanges(rel, id, pkg.Entries)
            packagedItemsByID[id] = PackagedItem{
                Kind:    PackagedItemSubtable,
                Entries: pkg.Entries,
            }
        case "":
            panic(rel + ": " + id + ": missing kind")
        default:
            panic(rel + ": " + id + ": unknown kind " + string(pkg.Kind))
        }
    }

    // Validate and store tables. Sub-table item-id existence is checked
    // against the item catalog by validateLootCatalogAgainstItemsLocked,
    // called from a separate init slot that runs after items.go has loaded.
    for tableID, entries := range raw.Tables {
        if tableID == "" {
            panic(rel + ": table with empty id")
        }
        if len(entries) == 0 {
            panic(rel + ": table " + tableID + " has no entries")
        }
        for _, e := range entries {
            if e.Min < 1 || e.Max < e.Min || e.Max > 100 {
                panic(rel + ": table " + tableID + " entry " + e.Entry +
                    ": invalid range [" + strconv.Itoa(e.Min) + "," + strconv.Itoa(e.Max) + "]")
            }
            if _, ok := packagedItemsByID[e.Entry]; !ok {
                panic(rel + ": table " + tableID + " references unknown packaged item " + e.Entry)
            }
        }
        validateTableRanges(rel, tableID, entries)
        lootTablesByID[tableID] = entries
    }

    // Sub-table item refs are validated separately because the item catalog
    // may load after this file at package init time. Call this guard from
    // wherever the package finishes initializing both catalogs.
    validateLootCatalogAgainstItems()
}

func validateSubtableRanges(rel, id string, entries []LootSubtableEntry) {
    sorted := append([]LootSubtableEntry(nil), entries...)
    sort.Slice(sorted, func(i, j int) bool { return sorted[i].Min < sorted[j].Min })
    for i, e := range sorted {
        if e.Min < 1 || e.Max < e.Min {
            panic(rel + ": " + id + ": invalid sub-table range [" + strconv.Itoa(e.Min) + "," + strconv.Itoa(e.Max) + "]")
        }
        if i > 0 && sorted[i].Min <= sorted[i-1].Max {
            panic(rel + ": " + id + ": overlapping sub-table ranges around " + strconv.Itoa(e.Min))
        }
    }
}

func validateTableRanges(rel, tableID string, entries []LootTableEntry) {
    sorted := append([]LootTableEntry(nil), entries...)
    sort.Slice(sorted, func(i, j int) bool { return sorted[i].Min < sorted[j].Min })
    for i := 1; i < len(sorted); i++ {
        if sorted[i].Min <= sorted[i-1].Max {
            panic(rel + ": " + tableID + ": overlapping table ranges around " + strconv.Itoa(sorted[i].Min))
        }
    }
}

// validateLootCatalogAgainstItems walks every sub-table item ref and
// panics on misses. Separated from the main loader so we can call it
// after the item catalog has loaded.
func validateLootCatalogAgainstItems() {
    for id, pkg := range packagedItemsByID {
        if pkg.Kind != PackagedItemSubtable {
            continue
        }
        for _, e := range pkg.Entries {
            if _, ok := getItemDef(e.Item); !ok {
                panic("catalog/neutral_groups/loot_tables.json: " + id +
                    " references unknown item " + e.Item)
            }
        }
    }
}

func getLootTable(id string) (LootTableDef, bool) {
    t, ok := lootTablesByID[id]
    return t, ok
}

func getPackagedItem(id string) (PackagedItem, bool) {
    p, ok := packagedItemsByID[id]
    return p, ok
}
```

**Note on `getItemDef`:** locate the actual accessor name in `items.go`. If it's `ItemDefByID`, `getItemByID`, etc., substitute throughout.

- [ ] **Step 2: Run the loader tests — should pass now**

Run: `go test ./server/internal/game -run TestLootTableLoader -v`
Expected: 3 PASS.

- [ ] **Step 3: Build the whole game package**

Run: `go build ./server/...`
Expected: clean. Task 2's `getLootTable` reference in `loadNeutralGroupsByTier` now resolves.

- [ ] **Step 4: Run all neutral-camp tests as a regression check**

Run: `go test ./server/internal/game -run "TestNeutralCamp|TestNeutralGroupLoader" -v -count=1`
Expected: all pass (we added the `lootTable` field but with `omitempty` so existing JSON unmarshalling still works).

---

## Task 5: Protocol additions (server)

**Files:**
- Modify: `server/pkg/protocol/messages.go`

- [ ] **Step 1: Add the new order-string constant**

Find the existing block at [messages.go:8-20](../../../server/pkg/protocol/messages.go#L8-L20):

```go
const (
    OrderStringIdle         = "idle"
    ...
    OrderStringFocusFollow  = "focus_follow"
)
```

Add immediately before the closing paren:

```go
    OrderStringPickupLoot   = "pickup_loot"
```

- [ ] **Step 2: Add the new wire types**

Find a sensible insertion point — adjacent to `GatherCommandMessage` (around line 371) for the command, and adjacent to the other snapshot/notification types for the others. Concretely:

After `GatherCommandMessage`:

```go
// PickupLootCommandMessage is the right-click "go collect that chest"
// order. Mirrors GatherCommandMessage exactly so transport/replay
// layers handle it uniformly. Server validates ownership + chest existence.
type PickupLootCommandMessage struct {
    Type     string `json:"type"`
    UnitIDs  []int  `json:"unitIds"`
    TargetID string `json:"targetId"` // LootDrop.ID
}
```

After `NotificationMessage`:

```go
// LootCollectedNotification is pushed to the collecting player when a
// chest pickup completes. The HUD renders a toast listing the resources
// and items received. Items that couldn't fit in the vault are listed in
// OverflowItemIDs so the toast can show "+50 gold, Broad Sword (lost —
// vault full)".
type LootCollectedNotification struct {
    Type            string         `json:"type"` // "loot_collected"
    PlayerID        string         `json:"playerId"`
    LootDropID      string         `json:"lootDropId"`
    Resources       map[string]int `json:"resources,omitempty"`
    ItemIDs         []string       `json:"itemIds,omitempty"`
    OverflowItemIDs []string       `json:"overflowItemIds,omitempty"`
}
```

Adjacent to other `*Snapshot` types (search the file for `WaveSnapshot` and group nearby):

```go
// LootDropSnapshot is the per-tick wire view of one ground-loot chest.
// Sent unfiltered (no FOW gating) so chests behave like POI dots on the
// minimap — the player can always navigate back to an uncollected chest.
//
// Resources and ItemIDs mirror the chest's pre-rolled contents so the
// client can render a hover tooltip showing what the chest contains
// before the player collects it. These are the same values granted on
// pickup (less vault-overflow items).
type LootDropSnapshot struct {
    ID        string         `json:"id"`
    X         float64        `json:"x"`
    Y         float64        `json:"y"`
    IconKey   string         `json:"iconKey"`
    Resources map[string]int `json:"resources,omitempty"`
    ItemIDs   []string       `json:"itemIds,omitempty"`
}
```

- [ ] **Step 3: Add `LootDrops` to `MatchSnapshotMessage`**

In the `MatchSnapshotMessage` struct, alongside the other recently-added `NeutralCamps` field:

```go
LootDrops    []LootDropSnapshot      `json:"lootDrops,omitempty"`
```

- [ ] **Step 4: Add `PickupLootID` to `UnitSnapshot`**

Find `UnitSnapshot` and add (near `FocusTargetID`):

```go
PickupLootID string `json:"pickupLootId,omitempty"`
```

- [ ] **Step 5: Build the protocol package**

Run: `go build ./server/pkg/protocol/...`
Expected: clean.

---

## Task 6: `OrderPickupLoot` constant + `Unit.PickupLootID` field

**Files:**
- Modify: `server/internal/game/state.go`

- [ ] **Step 1: Add `OrderPickupLoot` to the OrderType iota block**

Locate the existing block at [state.go:20-34](../../../server/internal/game/state.go#L20-L34). Append after `OrderFocusFollow`:

```go
    // OrderPickupLoot: walk to a treasure chest and collect it. Sticky
    // unit field PickupLootID is stored by ID per AI_RULES; the
    // tickLootDropsLocked path resolves and validates each tick.
    // Combat AI does NOT engage on the way (matches OrderMove semantics).
    OrderPickupLoot
)
```

- [ ] **Step 2: Add the wire-format mapping**

In `orderTypeString`, before the `default`:

```go
case OrderPickupLoot:
    return protocol.OrderStringPickupLoot
```

- [ ] **Step 3: Add the `PickupLootID` field to `Unit`**

Find `type Unit struct` (in state.go). After `FocusTargetID` (or near the other sticky-target ID fields), add:

```go
// PickupLootID links this unit to a LootDrop it is walking to collect.
// Empty when not pickup-bound. Stored as ID (not pointer) per AI_RULES;
// resolved each tick in tickLootDropsLocked. Cleared by any
// order-replacement helper that already clears other sticky targets
// (GatherTargetID etc.) — see state_movement.go / state_workers.go.
PickupLootID string
```

- [ ] **Step 4: Build**

Run: `go build ./server/...`
Expected: clean.

---

## Task 7: `LootDrop` runtime struct + `grantResourceToPlayerLocked`

**Files:**
- Modify: `server/internal/game/state.go`
- Modify: `server/internal/game/state_items.go`

- [ ] **Step 1: Add `LootDrops` registry to `GameState`**

In `type GameState struct`, near `NeutralCamps`:

```go
// LootDrops is the registry of ground-loot chests currently in the
// world. Keyed by stable string id (allocated via nextLootDropID).
// Drops persist until collected — no automatic expiry, no wave-start
// despawn. See state_loot_drops.go for the spawn/pickup lifecycle.
LootDrops      map[string]*LootDrop
nextLootDropID int
```

Initialize in `NewGameStateWithSeed` next to the other map allocations:

```go
LootDrops: map[string]*LootDrop{},
```

(Find the existing struct literal in `NewGameStateWithSeed`; insert alongside the other empty maps like `Units`, `Players`, etc.)

- [ ] **Step 2: Add the `grantResourceToPlayerLocked` helper**

In [state_items.go](../../../server/internal/game/state_items.go), append near the other player-vault helpers:

```go
// grantResourceToPlayerLocked adds an amount of a single resource type
// to player.Resources. Centralizes the mutation so future hooks
// (telemetry, achievements, resource caps) live in one place.
//
// amount <= 0 is a no-op. key need not pre-exist in the map.
//
// Must be called under s.mu write lock.
func (s *GameState) grantResourceToPlayerLocked(player *Player, key string, amount int) {
    if player == nil || amount <= 0 || key == "" {
        return
    }
    if player.Resources == nil {
        player.Resources = map[string]int{}
    }
    player.Resources[key] += amount
}
```

- [ ] **Step 3: Build**

Run: `go build ./server/...`
Expected: FAIL — `undefined: LootDrop`. That's expected; Task 8 creates the struct.

---

## Task 8: `LootDrop` struct + drop-logic failing tests

**Files:**
- Create: `server/internal/game/state_loot_drops.go`
- Create: `server/internal/game/state_loot_drops_test.go`

- [ ] **Step 1: Create the module skeleton with the struct and an empty drop function**

```go
package game

import (
    "log/slog"
    "sort"
    "strconv"

    "webrts/server/pkg/protocol"
)

// LootDrop is a server-authoritative ground-loot chest. Contents are
// pre-rolled at spawn time so save/replay determinism is independent of
// when the player chooses to pick the chest up. The chest persists in
// world space until a friendly unit walks within pickup range; on
// collection contents transfer to player.Resources / player.Vault.
//
// All references to this struct from other state (e.g. Unit.PickupLootID)
// are by string ID per AI_RULES — never persist a *LootDrop on another
// struct that survives the tick.
type LootDrop struct {
    ID             string
    X, Y           float64
    SourceCampID   string // for debug only; not used by gameplay
    ResourceGrants map[string]int
    ItemGrants     []string
    IconKey        string
}

// chestIconKeyDefault is the v1 sprite. Tier-varying visuals are out of
// scope; future work can vary by source camp tier.
const chestIconKeyDefault = "treasure_chest"

// maybeDropChestForCampLocked is called when a neutral camp transitions
// from AliveUnitIDs>0 to 0 due to player damage (NOT due to wave-start
// despawn — see the State guard in onUnitRemovedFromCampLocked).
//
// Rolls the camp's loot table once on s.rngLoot:
//   - top-level 1..100
//   - in-range entry → resolve packaged item
//     - resource_bundle: grants collected verbatim
//     - item_subtable: roll 1..max(entries); on hit, append item id
//   - gap on top-level → no chest
//   - sub-table gap → no item, but bundle/empty branch still creates a
//     chest if there were resources to grant; otherwise no chest.
//
// Stub for Task 8; full body in Task 9.
//
// Must be called under s.mu write lock.
func (s *GameState) maybeDropChestForCampLocked(camp *NeutralCamp) {
    _ = camp
}

// spawnLootDropLocked allocates a new chest ID and inserts the drop into
// s.LootDrops. World coords are computed from the camp center.
//
// Must be called under s.mu write lock.
func (s *GameState) spawnLootDropLocked(camp *NeutralCamp, resources map[string]int, items []string) *LootDrop {
    cellSize := s.MapConfig.CellSize
    s.nextLootDropID++
    id := "loot-" + strconv.Itoa(s.nextLootDropID)
    drop := &LootDrop{
        ID:             id,
        X:              float64(camp.X)*cellSize + cellSize/2,
        Y:              float64(camp.Y)*cellSize + cellSize/2,
        SourceCampID:   camp.PlacementID,
        ResourceGrants: resources,
        ItemGrants:     items,
        IconKey:        chestIconKeyDefault,
    }
    if s.LootDrops == nil {
        s.LootDrops = map[string]*LootDrop{}
    }
    s.LootDrops[id] = drop
    return drop
}

// lootDropSnapshotsLocked returns the wire view of every chest. Sorted by
// ID for deterministic snapshot output.
//
// Must be called under s.mu read lock.
func (s *GameState) lootDropSnapshotsLocked() []protocol.LootDropSnapshot {
    if len(s.LootDrops) == 0 {
        return nil
    }
    ids := make([]string, 0, len(s.LootDrops))
    for id := range s.LootDrops {
        ids = append(ids, id)
    }
    sort.Strings(ids)
    out := make([]protocol.LootDropSnapshot, 0, len(ids))
    for _, id := range ids {
        d := s.LootDrops[id]
        out = append(out, protocol.LootDropSnapshot{
            ID:        d.ID,
            X:         d.X,
            Y:         d.Y,
            IconKey:   d.IconKey,
            Resources: d.ResourceGrants,
            ItemIDs:   d.ItemGrants,
        })
    }
    return out
}

var _ = slog.Default // silence unused-import in the skeleton; remove in Task 9
```

- [ ] **Step 2: Write failing tests for drop logic**

```go
package game

import (
    "testing"
)

// TestLootDrop_DropsOnHit pins that a roll inside an entry's range
// produces exactly one chest in s.LootDrops with non-empty contents.
// Concrete contents are derived from the catalog, NOT pinned as literals.
func TestLootDrop_DropsOnHit(t *testing.T) {
    s := newTestStateForLootDrops(t, 1) // seed chosen so the first roll lands inside small_resource_bundle
    camp := &s.NeutralCamps[0]
    camp.SpawnedGroupID = "small_raider_group"
    s.maybeDropChestForCampLocked(camp)

    if got := len(s.LootDrops); got != 1 {
        t.Fatalf("LootDrops after a hit: got %d, want 1", got)
    }
    var drop *LootDrop
    for _, d := range s.LootDrops {
        drop = d
    }
    if len(drop.ResourceGrants) == 0 && len(drop.ItemGrants) == 0 {
        t.Errorf("drop has no contents — should be impossible on a top-level hit")
    }
}

// TestLootDrop_NoDropOnTopLevelGap: when the roll lands in a gap on the
// top-level table (here, raider_loot covers 1..20; rolls 21..100 are a
// gap), no chest spawns.
func TestLootDrop_NoDropOnTopLevelGap(t *testing.T) {
    s := newTestStateForLootDrops(t, 999) // seed chosen so the first roll lands in 21..100
    camp := &s.NeutralCamps[0]
    camp.SpawnedGroupID = "small_raider_group"
    s.maybeDropChestForCampLocked(camp)

    if got := len(s.LootDrops); got != 0 {
        t.Errorf("LootDrops after a gap: got %d, want 0", got)
    }
}

// TestLootDrop_Deterministic: two states with the same seed produce
// identical drop contents.
func TestLootDrop_Deterministic(t *testing.T) {
    s1 := newTestStateForLootDrops(t, 42)
    s2 := newTestStateForLootDrops(t, 42)
    s1.NeutralCamps[0].SpawnedGroupID = "small_raider_group"
    s2.NeutralCamps[0].SpawnedGroupID = "small_raider_group"
    s1.maybeDropChestForCampLocked(&s1.NeutralCamps[0])
    s2.maybeDropChestForCampLocked(&s2.NeutralCamps[0])

    if len(s1.LootDrops) != len(s2.LootDrops) {
        t.Fatalf("drop count differs: %d vs %d", len(s1.LootDrops), len(s2.LootDrops))
    }
    var d1, d2 *LootDrop
    for _, d := range s1.LootDrops {
        d1 = d
    }
    for _, d := range s2.LootDrops {
        d2 = d
    }
    if d1 == nil && d2 == nil {
        return // both empty, still deterministic
    }
    if (d1 == nil) != (d2 == nil) {
        t.Fatalf("one drop nil, other not: %v vs %v", d1, d2)
    }
    if len(d1.ResourceGrants) != len(d2.ResourceGrants) || len(d1.ItemGrants) != len(d2.ItemGrants) {
        t.Errorf("contents differ: %v / %v vs %v / %v", d1.ResourceGrants, d1.ItemGrants, d2.ResourceGrants, d2.ItemGrants)
    }
}

// TestLootDrop_NoGroupNoTable: camp with no SpawnedGroupID is a no-op.
func TestLootDrop_NoGroupNoTable(t *testing.T) {
    s := newTestStateForLootDrops(t, 1)
    s.NeutralCamps[0].SpawnedGroupID = "" // explicit
    s.maybeDropChestForCampLocked(&s.NeutralCamps[0])
    if got := len(s.LootDrops); got != 0 {
        t.Errorf("LootDrops when SpawnedGroupID empty: got %d, want 0", got)
    }
}

// newTestStateForLootDrops builds a minimal GameState with one neutral
// camp at (5,5) seeded for deterministic loot rolls. Reuses the existing
// test factory from state_neutral_camps_test.go where possible.
func newTestStateForLootDrops(t *testing.T, seed int64) *GameState {
    t.Helper()
    return newTestGameStateForNeutralCampTests(t, seed) // already used by Batch C/D tests
    // The factory's MapConfig already has a NeutralSpawn at (5,5)
    // referencing small_raider_group — the same group we authored a
    // lootTable on in Task 2 Step 5.
}
```

Adjust the seed values (1, 999, 42) once you run the test and see actual rolls — pick seeds that hit each branch. The expected roll behavior:
- Roll 1-10 → `small_resource_bundle`
- Roll 11-20 → `basic_weapons` (sub-table roll)
- Roll 21-100 → no drop

You may also need to adjust `newTestStateForLootDrops` if the existing neutral-camp test factory doesn't expose a single seeded camp. Look at `newTestGameStateForNeutralCampTests` in `state_neutral_camps_test.go` and adopt the same pattern.

- [ ] **Step 3: Verify the tests fail**

Run: `go test ./server/internal/game -run TestLootDrop -v`
Expected: 4 FAIL (stub doesn't roll anything).

---

## Task 9: Implement `maybeDropChestForCampLocked`

**Files:**
- Modify: `server/internal/game/state_loot_drops.go`

- [ ] **Step 1: Replace the stub with the full body**

```go
func (s *GameState) maybeDropChestForCampLocked(camp *NeutralCamp) {
    if camp == nil || camp.SpawnedGroupID == "" {
        return
    }
    tier := resolveNeutralTier(camp.CurrentTier)
    if tier == 0 {
        return
    }
    group, ok := getNeutralGroup(tier, camp.SpawnedGroupID)
    if !ok || group.LootTable == "" {
        return
    }
    table, ok := getLootTable(group.LootTable)
    if !ok {
        slog.Warn("maybeDropChestForCampLocked: loot table missing",
            "campID", camp.PlacementID, "lootTable", group.LootTable)
        return
    }

    // Top-level d100. Gap = no drop.
    roll := s.rngLoot.Intn(100) + 1
    var hit *LootTableEntry
    for i := range table {
        if roll >= table[i].Min && roll <= table[i].Max {
            hit = &table[i]
            break
        }
    }
    if hit == nil {
        return
    }

    pkg, ok := getPackagedItem(hit.Entry)
    if !ok {
        slog.Warn("maybeDropChestForCampLocked: packaged item missing (catalog drift)",
            "campID", camp.PlacementID, "entry", hit.Entry)
        return
    }

    var resources map[string]int
    var items []string

    switch pkg.Kind {
    case PackagedItemResourceBundle:
        // Defensive copy so subsequent edits to the catalog map don't
        // leak into LootDrop state (the catalog is supposed to be
        // immutable but copy is cheap and removes the surface).
        resources = make(map[string]int, len(pkg.Resources))
        for k, v := range pkg.Resources {
            resources[k] = v
        }
    case PackagedItemSubtable:
        maxRoll := 0
        for _, e := range pkg.Entries {
            if e.Max > maxRoll {
                maxRoll = e.Max
            }
        }
        if maxRoll == 0 {
            slog.Warn("maybeDropChestForCampLocked: sub-table has no max range",
                "campID", camp.PlacementID, "entry", hit.Entry)
            return
        }
        subRoll := s.rngLoot.Intn(maxRoll) + 1
        for _, e := range pkg.Entries {
            if subRoll >= e.Min && subRoll <= e.Max {
                items = append(items, e.Item)
                break
            }
        }
        // Sub-table gap is legal but produces no item. Fall through.
    }

    if len(resources) == 0 && len(items) == 0 {
        // Sub-table gap with no resource side — indistinguishable from
        // "no drop", so don't litter the world.
        return
    }

    s.spawnLootDropLocked(camp, resources, items)
}
```

- [ ] **Step 2: Delete the temporary `slog.Default` reference**

Remove `var _ = slog.Default` from the skeleton if it's still there — `slog` is now used legitimately above.

- [ ] **Step 3: Run the drop tests**

Run: `go test ./server/internal/game -run TestLootDrop -v`
Expected: 4 PASS. Adjust seed values in the tests if needed — the actual seed→roll mapping depends on `s.rngLoot`'s state at the call site.

- [ ] **Step 4: Full package regression**

Run: `go test ./server/internal/game -count=1`
Expected: no NEW failures.

---

## Task 10: Wipe-trigger hook + despawn state-ordering fix

**Files:**
- Modify: `server/internal/game/state_neutral_camps.go`

- [ ] **Step 1: Reorder `despawnNeutralCampLocked` to flip state BEFORE removals**

Find the existing function. The current order is:
1. Snapshot AliveUnitIDs
2. For each id: removeUnitLocked(id)
3. Clear camp.AliveUnitIDs
4. camp.State = NeutralCampWaveHidden

Change to:
1. Snapshot AliveUnitIDs
2. **`camp.State = NeutralCampWaveHidden`** (moved up)
3. For each id: removeUnitLocked(id) — the hook now sees `State == NeutralCampWaveHidden` and skips the chest drop
4. Clear camp.AliveUnitIDs

```go
func (s *GameState) despawnNeutralCampLocked(camp *NeutralCamp) {
    toRemove := append([]int(nil), camp.AliveUnitIDs...)
    // Flip state BEFORE the per-unit removals fire the hook. The
    // wipe-trigger in onUnitRemovedFromCampLocked is gated on
    // State == NeutralCampActive specifically so that wave-start
    // despawns do not spawn chests (those units weren't killed by
    // the player).
    camp.State = NeutralCampWaveHidden
    for _, id := range toRemove {
        u := s.getUnitByIDLocked(id)
        if u == nil {
            continue
        }
        s.removeUnitLocked(id)
    }
    camp.AliveUnitIDs = camp.AliveUnitIDs[:0]
}
```

- [ ] **Step 2: Add the wipe-detection hook in `onUnitRemovedFromCampLocked`**

Find the function. After the splice that removes the dead unit's ID from `camp.AliveUnitIDs`, add:

```go
// Wipe-trigger: when the player's combat drove this camp from >0
// units to 0, roll the loot table. The State guard ensures
// wave-start despawn (which also drives the slice to 0) does NOT
// fire this — despawnNeutralCampLocked flips State to WaveHidden
// before invoking removeUnitLocked.
if len(camp.AliveUnitIDs) == 0 && camp.State == NeutralCampActive {
    s.maybeDropChestForCampLocked(camp)
}
```

- [ ] **Step 3: Add tests for both branches of the wipe trigger**

In `state_loot_drops_test.go`, append:

```go
// TestLootDrop_WipeTriggersDrop: killing all camp units one by one
// fires the drop on the last kill.
func TestLootDrop_WipeTriggersDrop(t *testing.T) {
    s := newTestStateForLootDrops(t, 1) // seed that hits a drop
    enableWavesForTest(t, s)
    s.tickNeutralCampsLocked() // initial spawn

    camp := &s.NeutralCamps[0]
    camp.GroupID = "small_raider_group"
    // Ensure the spawned group is the one with the loot table.
    if camp.SpawnedGroupID != "small_raider_group" {
        t.Skipf("test seed produced a different random group (%q); choose a seed that lands on small_raider_group", camp.SpawnedGroupID)
    }

    ids := append([]int(nil), camp.AliveUnitIDs...)
    if len(ids) == 0 {
        t.Fatalf("setup: expected initial spawn to populate AliveUnitIDs")
    }
    for _, id := range ids {
        u := s.getUnitByIDLocked(id)
        if u == nil {
            continue
        }
        s.removeUnitLocked(u.ID)
    }
    if got := len(s.LootDrops); got == 0 {
        t.Errorf("LootDrops after camp wipe: got 0, want > 0 (or a no-drop seed)")
    }
}

// TestLootDrop_WaveStartDoesNotDrop: state transition to WaveHidden
// before the per-unit removals means the wipe hook does NOT fire.
func TestLootDrop_WaveStartDoesNotDrop(t *testing.T) {
    s := newTestStateForLootDrops(t, 1)
    enableWavesForTest(t, s)
    s.tickNeutralCampsLocked() // initial spawn

    s.WaveManager.State = "active"
    s.tickNeutralCampsLocked() // triggers despawnNeutralCampLocked

    if got := len(s.LootDrops); got != 0 {
        t.Errorf("LootDrops after wave-start despawn: got %d, want 0", got)
    }
}
```

- [ ] **Step 4: Run the new tests**

Run: `go test ./server/internal/game -run "TestLootDrop" -v -count=1`
Expected: all PASS (existing 4 + 2 new).

- [ ] **Step 5: Run the full TestNeutralCamp suite to make sure the despawn-ordering change didn't regress anything**

Run: `go test ./server/internal/game -run TestNeutralCamp -v -count=1`
Expected: 11 PASS.

---

## Task 11: Snapshot wiring (server)

**Files:**
- Modify: `server/internal/game/state.go`

- [ ] **Step 1: Add `LootDrops` to all three `MatchSnapshotMessage` return blocks**

The pattern is identical to the recent `NeutralCamps` addition. There are three `return protocol.MatchSnapshotMessage{...}` blocks in `state.go`. In each, alongside `NeutralCamps: s.neutralCampSnapshotsLocked()`, add:

```go
LootDrops: s.lootDropSnapshotsLocked(),
```

Use `replace_all` on the literal substring:
```
NeutralCamps:           s.neutralCampSnapshotsLocked(),
```
replacing with:
```
NeutralCamps:           s.neutralCampSnapshotsLocked(),
		LootDrops:              s.lootDropSnapshotsLocked(),
```

(Mind the tab indentation — match the surrounding rows.)

- [ ] **Step 2: Build**

Run: `go build ./server/...`
Expected: clean.

---

## Task 12: `PickupLootWithUnits` + order-replacement cleanup

**Files:**
- Create: `server/internal/game/state_loot_pickup.go`
- Modify: `server/internal/game/state_movement.go`
- Modify: `server/internal/game/state_workers.go`

- [ ] **Step 1: Find existing `GatherTargetID = ""` sites that need a parallel `PickupLootID = ""`**

Run: `grep -n 'GatherTargetID = ""' server/internal/game/*.go`

For each site, add an adjacent line `unit.PickupLootID = ""`. These are typically inside `MoveUnits`, `AttackWithUnits`, `HoldPositionWithUnits`, `resetUnitMovementLocked`, etc. Mirror exactly — any place existing code clears a sticky-target ID on order change, also clear the loot-pickup one.

- [ ] **Step 2: Create the pickup entry point**

```go
package game

import (
    "webrts/server/pkg/protocol"
)

// PickupLootWithUnits is the player-issued "right-click chest with the
// selection" command. Validates each unit (alive, owned by player) and
// the target chest (exists). Assigns the OrderPickupLoot path; the
// actual collection happens in tickLootDropsLocked when the first unit
// reaches proximity.
//
// Multi-select: all units walk toward the same chest; the first arrival
// collects it; the rest see drop == nil next tick and fall back to
// OrderIdle. This is the same fan-out / first-arrival pattern as gather.
//
// Acquires s.mu.
func (s *GameState) PickupLootWithUnits(playerID string, unitIDs []int, lootDropID string) {
    s.mu.Lock()
    defer s.mu.Unlock()
    defer profileStart("cmd.PickupLootWithUnits")()

    drop, ok := s.LootDrops[lootDropID]
    if !ok || drop == nil {
        return // stale id; silently no-op (same pattern as stale gather)
    }
    target := protocol.Vec2{X: drop.X, Y: drop.Y}

    blocked := s.getBlockedCellsLocked()
    orderID := s.nextMovementOrderIDLocked()

    for _, uid := range unitIDs {
        unit := s.getUnitByIDLocked(uid)
        if unit == nil || unit.OwnerID != playerID || unit.HP <= 0 {
            continue
        }
        s.resetUnitMovementLocked(unit, orderID)
        unit.Order = OrderState{
            Type:  OrderPickupLoot,
            DestX: target.X,
            DestY: target.Y,
        }
        unit.PickupLootID = drop.ID
        s.assignUnitPath(unit, target, blocked, nil)
    }
}
```

Look up the actual `assignUnitPath` / `resetUnitMovementLocked` signatures — they may differ. Pattern is to mirror `GatherWithUnits` ([state_workers.go:74-118](../../../server/internal/game/state_workers.go#L74-L118)).

- [ ] **Step 3: Build**

Run: `go build ./server/...`
Expected: clean.

---

## Task 13: `tickLootDropsLocked` — proximity, grant, despawn

**Files:**
- Modify: `server/internal/game/state_loot_drops.go`
- Modify: `server/internal/game/state.go`

- [ ] **Step 1: Append the tick + grant helpers to `state_loot_drops.go`**

```go
// lootPickupRadius is the world-pixel distance from a chest center
// within which a unit on OrderPickupLoot collects the chest. Tuned to
// half a cell — generous enough that movement steering doesn't oscillate
// at the boundary; tight enough that two units approaching from
// different sides can't both "arrive" in the same tick.
const lootPickupRadius = 0.5 // multiplied by cellSize at use site

// tickLootDropsLocked drains chest pickups each tick. For every unit on
// OrderPickupLoot, validates the chest still exists and the unit is
// close enough; on success grants contents to the owning player and
// despawns the chest.
//
// Stale-id pickup attempts (chest already collected by a faster ally)
// quietly fall back to OrderIdle — no error, no toast.
//
// Must be called under s.mu write lock.
func (s *GameState) tickLootDropsLocked() {
    cellSize := s.MapConfig.CellSize
    pickupDistSq := (lootPickupRadius * cellSize) * (lootPickupRadius * cellSize)

    for _, unit := range s.Units {
        if unit == nil || unit.Order.Type != OrderPickupLoot || unit.PickupLootID == "" {
            continue
        }
        drop, ok := s.LootDrops[unit.PickupLootID]
        if !ok || drop == nil {
            // Chest already collected; clear and idle. AI_RULES rule 3.
            unit.PickupLootID = ""
            unit.Order = OrderState{Type: OrderIdle}
            continue
        }
        player := s.Players[unit.OwnerID]
        if player == nil {
            unit.PickupLootID = ""
            unit.Order = OrderState{Type: OrderIdle}
            continue
        }
        dx := unit.X - drop.X
        dy := unit.Y - drop.Y
        if dx*dx+dy*dy > pickupDistSq {
            // Still in transit — movement system is steering toward
            // (drop.X, drop.Y). Leave the order alone.
            continue
        }
        // Collect.
        s.grantLootDropToPlayerLocked(player, drop)
        delete(s.LootDrops, drop.ID)
        unit.PickupLootID = ""
        unit.Order = OrderState{Type: OrderIdle}
    }
}

// grantLootDropToPlayerLocked transfers a chest's pre-rolled contents to
// the player. Resources are granted unconditionally; items that don't
// fit the vault are dropped on the floor (metaphorically) and reported
// via OverflowItemIDs on the LootCollectedNotification.
//
// Returns the notification payload so the caller can dispatch it.
//
// Must be called under s.mu write lock.
func (s *GameState) grantLootDropToPlayerLocked(player *Player, drop *LootDrop) protocol.LootCollectedNotification {
    notif := protocol.LootCollectedNotification{
        Type:       "loot_collected",
        PlayerID:   player.ID,
        LootDropID: drop.ID,
    }
    if len(drop.ResourceGrants) > 0 {
        notif.Resources = make(map[string]int, len(drop.ResourceGrants))
        for k, v := range drop.ResourceGrants {
            s.grantResourceToPlayerLocked(player, k, v)
            notif.Resources[k] = v
        }
    }
    for _, itemID := range drop.ItemGrants {
        def, ok := getItemDef(itemID)
        if !ok {
            // Catalog drift — should not happen since loader validates
            // at startup. Log and skip.
            slog.Warn("grantLootDropToPlayerLocked: unknown item id (catalog drift)",
                "playerID", player.ID, "itemID", itemID)
            continue
        }
        ok = s.addItemToVaultLocked(player, def)
        if ok {
            notif.ItemIDs = append(notif.ItemIDs, itemID)
        } else {
            notif.OverflowItemIDs = append(notif.OverflowItemIDs, itemID)
        }
    }
    return notif
}
```

Note: `addItemToVaultLocked` may have a different signature. Look up its actual signature in [state_items.go:113](../../../server/internal/game/state_items.go#L113) and adjust the call. Same for `getItemDef`.

- [ ] **Step 2: Wire `tickLootDropsLocked` into the main tick loop**

Find the existing tick order in `state.go`. Insert `s.tickLootDropsLocked()` AFTER the movement tick and BEFORE the snapshot build. Look for nearby ticks like `tickEnemySpawnpointsLocked`, `tickProjectilesLocked` — pickup proximity depends on unit positions, so it has to run after movement.

```go
// (existing) movement tick
// (existing) other ticks
s.tickLootDropsLocked()
// (existing) snapshot or end-of-tick housekeeping
```

If unsure about the exact spot, place it after `tickNeutralCampsLocked` (it runs at a sensible point in the loop already established for similar concerns).

- [ ] **Step 3: Add pickup tests**

In `state_loot_drops_test.go`, append:

```go
// TestLootDrop_PickupGrantsContents: unit standing on a chest with
// OrderPickupLoot collects it on the next tick.
func TestLootDrop_PickupGrantsContents(t *testing.T) {
    s := newTestStateForLootDrops(t, 1)
    enableWavesForTest(t, s)
    camp := &s.NeutralCamps[0]
    camp.SpawnedGroupID = "small_raider_group"
    // Force a known drop by calling the spawner directly.
    drop := s.spawnLootDropLocked(camp,
        map[string]int{"gold": 50, "wood": 15}, nil)

    // Build a fake player + unit at the chest position.
    player := &Player{ID: "p1", Resources: map[string]int{}}
    s.Players["p1"] = player
    unit := &Unit{ID: 9001, OwnerID: "p1", HP: 100, X: drop.X, Y: drop.Y}
    unit.Order = OrderState{Type: OrderPickupLoot, DestX: drop.X, DestY: drop.Y}
    unit.PickupLootID = drop.ID
    s.Units[unit.ID] = unit

    s.tickLootDropsLocked()

    if _, still := s.LootDrops[drop.ID]; still {
        t.Errorf("chest still present after pickup")
    }
    if player.Resources["gold"] != 50 {
        t.Errorf("gold = %d, want 50", player.Resources["gold"])
    }
    if player.Resources["wood"] != 15 {
        t.Errorf("wood = %d, want 15", player.Resources["wood"])
    }
    if unit.PickupLootID != "" {
        t.Errorf("PickupLootID not cleared: %q", unit.PickupLootID)
    }
    if unit.Order.Type != OrderIdle {
        t.Errorf("Order.Type = %v, want OrderIdle", unit.Order.Type)
    }
}

// TestLootDrop_StalePickupNoOp: chest already collected; unit fall back
// to idle silently.
func TestLootDrop_StalePickupNoOp(t *testing.T) {
    s := newTestStateForLootDrops(t, 1)
    player := &Player{ID: "p1", Resources: map[string]int{}}
    s.Players["p1"] = player
    unit := &Unit{ID: 9001, OwnerID: "p1", HP: 100}
    unit.Order = OrderState{Type: OrderPickupLoot}
    unit.PickupLootID = "loot-999" // never existed
    s.Units[unit.ID] = unit

    s.tickLootDropsLocked()

    if unit.PickupLootID != "" {
        t.Errorf("PickupLootID not cleared on stale id")
    }
    if unit.Order.Type != OrderIdle {
        t.Errorf("expected fallback to OrderIdle, got %v", unit.Order.Type)
    }
}
```

- [ ] **Step 4: Run pickup tests**

Run: `go test ./server/internal/game -run TestLootDrop -v -count=1`
Expected: all PASS.

- [ ] **Step 5: Full regression**

Run: `go test ./server/internal/game -count=1`
Expected: no new failures.

---

## Task 14: WS handler + notification dispatch

**Files:**
- Modify: `server/internal/ws/handlers.go`
- Modify: `server/internal/game/state_loot_drops.go`

- [ ] **Step 1: Add the `pickup_loot_command` case**

In [handlers.go](../../../server/internal/ws/handlers.go) (search for `gather_command` ~line 485), add an adjacent case:

```go
case "pickup_loot_command":
    var msg protocol.PickupLootCommandMessage
    if err := json.Unmarshal(raw, &msg); err != nil {
        client.SendError(protocol.ErrorMessage{
            Type:    "error",
            Message: "invalid pickup_loot_command payload",
        })
        return
    }
    match.State.PickupLootWithUnits(client.PlayerID(), msg.UnitIDs, msg.TargetID)
```

Adjust to match the exact case-block style used by adjacent cases.

- [ ] **Step 2: Dispatch `LootCollectedNotification` from the grant path**

This requires sending a message to the specific player who collected. Look at how other notifications reach a single player — likely via a `MatchClient` interface or similar. Search for `LegendPointDrop` or other per-player notifications to find the pattern, then wire `grantLootDropToPlayerLocked`'s return value through `tickLootDropsLocked` to the player's send channel.

If a clean dispatch path doesn't exist, the simplest mechanism is to enqueue the notification onto `GameState` for the snapshot loop to pick up (`s.pendingLootNotifications []protocol.LootCollectedNotification` cleared per-tick).

Concrete pattern:
- Add to `GameState`: `pendingLootNotifications []protocol.LootCollectedNotification`
- In `tickLootDropsLocked`, after `grantLootDropToPlayerLocked`, append the returned notification
- In the match-broadcast loop (find it via `Snapshot()`'s caller), flush these notifications to the relevant player after sending the snapshot

Concrete location and signature need confirmation from the existing notification flow. If you can't find a clean pattern in 10 minutes of grep, STOP and report BLOCKED with what you tried.

- [ ] **Step 3: Build + regression**

Run: `go build ./server/... && go test ./server/internal/game -count=1`
Expected: clean.

---

## Task 15: Client protocol mirroring + state sync

**Files:**
- Modify: `client/src/game-portal/src/game/network/protocol.ts`
- Modify: `client/src/game-portal/src/game/core/GameState.ts`
- Modify: `client/src/game-portal/src/game/network/NetworkClient.ts` (or equivalent)

- [ ] **Step 1: Mirror the wire types**

In [protocol.ts](../../../client/src/game-portal/src/game/network/protocol.ts), add (near the other snapshot/command types):

```ts
export interface LootDropSnapshot {
  id: string
  x: number
  y: number
  iconKey: string
  resources?: Record<string, number>
  itemIds?: string[]
}

export interface PickupLootCommandMessage {
  type: 'pickup_loot_command'
  unitIds: number[]
  targetId: string
}

export interface LootCollectedNotification {
  type: 'loot_collected'
  playerId: string
  lootDropId: string
  resources?: Record<string, number>
  itemIds?: string[]
  overflowItemIds?: string[]
}

// New order-string constant
export const OrderStringPickupLoot = 'pickup_loot'
```

Then on `MatchSnapshotMessage`:

```ts
lootDrops?: LootDropSnapshot[]
```

And on `UnitSnapshot`:

```ts
pickupLootId?: string
```

- [ ] **Step 2: Apply the snapshot field**

In [GameState.ts](../../../client/src/game-portal/src/game/core/GameState.ts), add a field:

```ts
// Live ground-loot chests, keyed by id. Populated from
// MatchSnapshotMessage.lootDrops each tick. Used by the world render
// layer and the right-click input dispatch.
lootDropsById: Map<string, LootDropSnapshot> = new Map()
```

In `applySnapshot`, alongside the existing `neutralCampSnapshotsById` rebuild:

```ts
this.lootDropsById.clear()
if (message.lootDrops) {
  for (const drop of message.lootDrops) {
    this.lootDropsById.set(drop.id, drop)
  }
}
```

- [ ] **Step 3: Add the `sendPickupLootCommand` helper**

Find the existing `sendGatherCommand` or equivalent — typically in `NetworkClient.ts` or a websocket wrapper. Mirror exactly:

```ts
sendPickupLootCommand(unitIds: number[], lootDropId: string) {
  this.send({
    type: 'pickup_loot_command',
    unitIds,
    targetId: lootDropId,
  } satisfies PickupLootCommandMessage)
}
```

Adjust to match the actual wrapper's API.

- [ ] **Step 4: Type-check**

Run: `cd client/src/game-portal && npx vue-tsc --noEmit`
Expected: clean.

---

## Task 16: Client — render, right-click, tooltip, HUD toast

**Files:**
- Modify: `client/src/game-portal/src/game/rendering/CanvasRenderer.ts` (or wherever world entities render)
- Modify: `client/src/game-portal/src/game/rendering/minimapLayers.ts`
- Modify: client input dispatcher (likely `InputManager.ts` or a Pinia store)
- Create / modify: tooltip + HUD toast components

This task has multiple visual subcomponents but they're tightly coupled. Subtasks below.

- [ ] **Subtask 16.1: Render chest sprites in the world canvas**

Find where the main world render loop draws other ground-level entities (resource nodes, banners). Add a chest render pass:

```ts
// LootDrop sprites — drawn after terrain but before units so dropped
// units in the same cell pass over the chest visually. Use a stable
// sprite key for v1 ("treasure_chest"); the IconKey field on the
// snapshot will let future tiers swap visuals without protocol change.
for (const drop of this.state.lootDropsById.values()) {
  // Use the same sprite-asset lookup that resource nodes use.
  const sprite = getSpriteForIconKey(drop.iconKey)
  if (!sprite) {
    // Fallback: brown square so missing sprites are visible in dev.
    ctx.fillStyle = '#8B6914'
    ctx.fillRect(drop.x - 12, drop.y - 12, 24, 24)
    continue
  }
  ctx.drawImage(sprite, drop.x - sprite.width / 2, drop.y - sprite.height / 2)
}
```

Hand-tune the position offsets to match how other entities render in this codebase. The asset for `treasure_chest` is not yet committed — coordinate with the user about an actual sprite asset; until then the brown-square fallback ships.

- [ ] **Subtask 16.2: Add chest dots to the minimap POI layer**

In `minimapLayers.ts`, extend the existing POI rendering loop to also iterate `lootDropsById` and paint a small chest-colored dot (e.g. amber `#f5b400`). Should sit at the chest's `(x, y)` projected to minimap coords, same math as the neutral camp POI logic. Always-visible (no FOW gating).

- [ ] **Subtask 16.3: Right-click hit-test for chest pickup**

Find the right-click handler. Insert chest-target detection BEFORE the move-to-ground fallback:

```ts
// Right-click target classification (existing order preserved):
//   1. enemy unit          → AttackCommand
//   2. resource node       → GatherCommand
//   3. deposit-cap         → DepositCommand
//   4. LootDrop chest      → PickupLootCommand  ← NEW
//   5. ground              → MoveCommand
const hitChest = findLootDropAtScreenPos(state, screenX, screenY)
if (hitChest && selectedUnitIds.length > 0) {
  network.sendPickupLootCommand(selectedUnitIds, hitChest.id)
  return
}
```

`findLootDropAtScreenPos` iterates `state.lootDropsById`, projects each chest's world position to screen, and returns the first hit within a small radius (16-24 screen px).

- [ ] **Subtask 16.4: Hover tooltip**

Use the existing tooltip primitive (the same one used by vault items in the HUD — search for `<UiTooltip>`, `useTooltip`, or similar). Wire it onto the world-canvas hover state: when hover hits a LootDrop, render a tooltip with:

```
Treasure Chest
+50 gold, +15 wood          (only if resources non-empty)
Broad Sword (Common Weapon) (one row per itemId, with icon)
```

Item display names come from the existing client-side item catalog store. If an itemId doesn't resolve (catalog not loaded), fall back to rendering the raw id.

- [ ] **Subtask 16.5: HUD toast on `loot_collected`**

Subscribe to the new notification type in the existing notification dispatcher. Render a toast listing resources received and items received; show overflow items with a "(vault full — lost)" suffix and a tinted background.

- [ ] **Subtask 16.6: Type-check + visual smoke test**

Run: `cd client/src/game-portal && npx vue-tsc --noEmit`
Expected: clean.

Then start the dev server, place a neutral spawn on a test map with the small_raider_group, start a match, kill the camp, observe:
- Chest appears at camp center
- Hover shows tooltip with contents
- Right-click with a selected unit sends them to collect
- Chest disappears on pickup; resource HUD updates
- Vault opens to show new item (if any rolled)
- Minimap shows amber dot at chest position until collected

---

## Task 17: Manual smoke test

**Files:** none modified — verification only.

- [ ] **Step 1: Create a test map with a single neutral camp using `small_raider_group`**

Either via the in-game editor or by editing one of the catalog maps. The placement should be near a player townhall so the player can clear it within a tick or two of starting the match.

- [ ] **Step 2: Start the dev server + open a match on the test map**

Use `start.bat`.

- [ ] **Step 3: Verify the full loop**

| Stage | Expected |
|---|---|
| Pre-wave | Neutral camp at the placement; chest is NOT yet on the field. |
| Player attacks and clears the camp | Chest spawns at camp center. Minimap shows amber dot. |
| Hover the chest | Tooltip shows pre-rolled contents (either resources or items, or both). |
| Right-click chest with a selected combat unit | Unit walks to the chest. Order shows as pickup-loot. |
| Unit arrives | Chest disappears, HUD toast shows received contents, gold/wood counters update, vault shows new item (if any). |
| Re-play with same seed twice | Identical drop both runs (determinism). |
| Don't collect; let wave 1 start | Neutral camp despawns. Chest remains on the field. |
| Wave 1 ends, camp respawns | A fresh chest does NOT appear (the old one is still there); the new camp gets a new chest only when wiped. |

- [ ] **Step 4: Vault-full smoke**

Buy items to fill the vault before wiping a camp. Wipe the camp on a seed that rolls an item drop. Collect the chest. Verify:
- HUD toast lists the item under "vault full — lost"
- The toast still confirms any resource side of the drop (resources don't need vault slots)
- The chest is still consumed (no second pickup attempt allowed)

---

## Self-Review Checklist

- [x] Every spec requirement maps to a task:
  - Catalog file (Task 1, 4)
  - LootTable group reference + SpawnedGroupID (Task 2)
  - Loader with validation (Tasks 3-4)
  - Wipe trigger + state-ordering invariant (Task 10)
  - LootDrop runtime + spawn at kill (Tasks 7-10)
  - Wire protocol additions (Task 5)
  - OrderPickupLoot + PickupLootID (Task 6)
  - Pickup entry point + tick (Tasks 12-13)
  - WS handler + notifications (Task 14)
  - Client types + state + dispatch (Task 15)
  - World render + minimap POI + right-click + tooltip + HUD toast (Task 16)
  - Vault-full handling (Task 13 grant path + Task 16.5 toast)
  - Determinism (Task 9: single s.rngLoot stream)

- [x] No "TBD" / "TODO" / placeholder steps left for the engineer.

- [x] Type names consistent: `LootDrop`, `LootDropSnapshot`, `LootTableEntry`, `LootSubtableEntry`, `PackagedItem`, `PackagedItemKind`, `OrderPickupLoot`, `OrderStringPickupLoot`, `PickupLootID`, `SpawnedGroupID`, `grantResourceToPlayerLocked`, `grantLootDropToPlayerLocked`, `maybeDropChestForCampLocked`, `tickLootDropsLocked`, `PickupLootWithUnits`.

- [x] No commit steps (per user policy).

- [x] AI_RULES compliance: all chest references stored by ID; `getUnitByIDLocked` / `s.LootDrops[id]` results validated before use; no `*LootDrop` persisted across ticks.
