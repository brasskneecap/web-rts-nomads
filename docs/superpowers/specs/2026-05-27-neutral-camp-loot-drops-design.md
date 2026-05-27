# Neutral Camp Loot Drops — Design

Status: Approved (brainstorming complete, ready for implementation planning)
Author: Cody (brainstormed with Claude)
Date: 2026-05-27

## Summary

Add a treasure-chest loot system that rewards clearing a neutral camp. When the last unit of a camp dies, the server rolls the camp's named loot table against `s.rngLoot`; on a hit it spawns a single persistent `LootDrop` (chest) at the camp center. Players right-click the chest with a selected unit; that unit walks to the chest, picks it up on proximity, and the chest's contents are granted to the player — items into the vault, resources directly onto `player.Resources`. Tables, sub-tables, and resource bundles are catalog-driven JSON so designers can author drops without touching simulation code.

The chest itself is a new world entity (no prior art in the codebase). Everything else reuses existing primitives: `s.rngLoot`, `ItemDef` catalog, vault helpers, the neutral-camp lifecycle, and the right-click order pipeline.

## Goals

1. Camps with a `loot_table` reference roll for a single chest drop on the wipe transition (`AliveUnitIDs` >0 → 0).
2. Loot tables are pure JSON; references into existing `ItemDef` IDs are validated at startup (panic on missing — matches existing catalog discipline).
3. Pickup is a right-click order on the chest with a selected unit, modeled on the existing `gather_command` flow.
4. Chests are persistent — they remain until collected, surviving wave transitions.
5. Determinism preserved — every roll (top-level table, sub-table) uses `s.rngLoot`. Same seed + same kill sequence = same drops.
6. AI_RULES compliance — loot/unit references stored by ID, resolved and validated at point-of-use, no `*LootDrop` or `*Unit` persisted across ticks.
7. "Random" group selection still produces a deterministic, well-defined drop: whichever group rolled at spawn time is the group whose `loot_table` is consulted at wipe time.

## Non-Goals

- Multi-chest drops per camp wipe (one chest per wipe, full stop).
- Chest expiry / despawn timers.
- FOW-gated chest visibility (chests are POI dots, always visible — see Wire Protocol §6).
- Loot ownership / "claimable by killer only". V1 lets any player on the match pick up any chest. Co-op-vs-PvP arbitration deferred.
- Loot tables varying by tier independently of the group reference. Tier affects which groups roll; the group's `loot_table` is the only loot input.
- Currencies other than `gold` and `wood`. Other resource keys are accepted by the schema but unused until balance asks for them.
- Authoring of additional tables beyond a single example `raider_loot`. The plumbing supports N tables; the content is deferred.

## Catalog Schema

**Location:** `server/internal/game/catalog/neutral_groups/loot_tables.json` (single file, sibling of `tier_<N>.json`).

Single file over a folder: there's exactly one schema, the file is small and human-skim-able, and a single file keeps the diff surface tight when designers tweak drop rates.

### Top-level shape

```json
{
  "packagedItems": {
    "small_resource_bundle":  { "kind": "resource_bundle", "resources": { "gold": 50, "wood": 15 } },
    "medium_resource_bundle": { "kind": "resource_bundle", "resources": { "gold": 100, "wood": 45 } },
    "basic_weapons": {
      "kind": "item_subtable",
      "entries": [
        { "item": "broad_sword", "min": 1,  "max": 10 },
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

### Schema rules (enforced at load time, panic on violation)

**Packaged items:**
- `kind` is `"resource_bundle"` or `"item_subtable"`. No other values.
- `resource_bundle.resources`: map of `resourceKey → amount`. `amount > 0`. Keys must be in the canonical set `{"gold","wood"}` for now (whitelist; new keys require a code change so we don't typo-grant a phantom resource).
- `item_subtable.entries`: at least one entry. Each `item` must resolve in `itemCatalogSingleton` (panic with file+entry index on miss). `min >= 1`, `max >= min`. Ranges may NOT overlap within a sub-table (panic on overlap). Gaps within `[1, max]` are legal — they roll into "nothing in this sub-table" which means the chest still spawned but the sub-table grant is skipped. *(Edge case rationale: the outer table already filters out the no-drop case; a sub-table gap is rare but valid. We log a warning at runtime when it hits.)*

**Tables:**
- Each table is an ordered list of `{ entry, min, max }`.
- `entry` must be a key in `packagedItems` (panic on miss).
- `min >= 1`, `max <= 100`, `max >= min`. Ranges may NOT overlap within a table (panic on overlap). Gaps are legal and represent "no drop" outcomes — this is the only mechanism for `chance < 100%` drops.

### Public loader API (new file `server/internal/game/loot_table_defs.go`)

- `type PackagedItemKind string` with constants `PackagedItemResourceBundle`, `PackagedItemSubtable`.
- `type LootTableEntry struct { Entry string; Min, Max int }`
- `type LootSubtableEntry struct { Item string; Min, Max int }`
- `type PackagedItem struct { Kind PackagedItemKind; Resources map[string]int; Entries []LootSubtableEntry }` (one of the two payloads populated by Kind).
- `type LootTableDef = []LootTableEntry` (per-table is just the ordered entries).
- `getLootTable(id string) (LootTableDef, bool)` — table lookup.
- `getPackagedItem(id string) (PackagedItem, bool)` — packaged item lookup.
- `validateLootCatalogAgainstItemsLocked()` — called from `init()` after both `itemCatalogSingleton` and the loot catalog are loaded; panics if any sub-table item id is missing.

Loaded once at package init via the existing `embed.FS` pattern; immutable after load.

## Group Reference

`NeutralGroup` (in `server/internal/game/neutral_group_defs.go`) gains one optional field:

```go
type NeutralGroup struct {
    ID          string                         `json:"id"`
    Name        string                         `json:"name"`
    Composition []NeutralGroupCompositionEntry `json:"composition"`
    LootTable   string                         `json:"lootTable,omitempty"` // NEW; empty = no drop
}
```

- Empty/absent `lootTable` = camp drops nothing on wipe. No chest is spawned.
- Non-empty `lootTable` must resolve via `getLootTable` (panic at catalog load time on missing — matches existing `unitType` validation in `loadNeutralGroupsByTier`).

**"Random" group → table resolution:** the random pick happens at camp *spawn* time (`spawnGroupForCampLocked`, already implemented). The camp does not currently record which specific group it rolled. Add one field to `NeutralCamp`:

```go
SpawnedGroupID string // id of the group rolled this respawn cycle; "" before first spawn
```

`spawnGroupForCampLocked` sets `camp.SpawnedGroupID = group.ID` after a successful resolve. The wipe-detection path reads `camp.SpawnedGroupID` to find the right `loot_table`. This keeps loot deterministic with respect to the spawn roll (same `s.rngSpawn` sequence → same group → same table → same `s.rngLoot` outcome).

## Runtime Lifecycle

New module: `server/internal/game/state_loot_drops.go`.

### Runtime struct

```go
type LootDrop struct {
    ID         string  // monotonically allocated, e.g. "loot-<n>"
    X, Y       float64 // world coords (camp center)
    SourceCampID string // for debug / observability; not used by gameplay
    ResourceGrants map[string]int // pre-rolled at chest spawn; granted on pickup
    ItemGrants     []string       // pre-rolled ItemDef IDs; granted on pickup
}
```

**Why pre-roll at spawn vs roll at pickup:** the sub-table item roll is part of the deterministic drop outcome from the original kill. Pre-rolling at spawn means save/replay determinism is preserved across all paths (the chest sitting on the ground is part of state; rolling on pickup would couple loot outcome to *when* the player chooses to pick up, which is an input-timing dependency and a determinism hazard).

Owned by `GameState`:
- `LootDrops map[string]*LootDrop` — primary registry keyed by ID. Map order is not used for game outcomes; snapshot iteration is sorted.
- `nextLootDropID int` — monotonic counter (under `s.mu`).

### Trigger: wipe detection

Modify `onUnitRemovedFromCampLocked` in `state_neutral_camps.go` to detect the >0 → 0 transition and call a new helper:

```go
// inside onUnitRemovedFromCampLocked, after the AliveUnitIDs slice has been spliced:
if len(camp.AliveUnitIDs) == 0 && camp.State == NeutralCampActive {
    s.maybeDropChestForCampLocked(camp)
}
```

The `State == NeutralCampActive` guard is critical: `despawnNeutralCampLocked` (wave start) also drives the slice to length 0 via `removeUnitLocked`, but it transitions State to `NeutralCampWaveHidden` *after* the splice. To avoid double-firing on wave-start despawn, `despawnNeutralCampLocked` must transition `State = NeutralCampWaveHidden` *before* iterating removals. Verify ordering during implementation; the spec requires that wave-start despawn does NOT drop chests (those units weren't killed by the player).

**Alternative considered & rejected:** adding a "was killed by player" flag on each unit. Heavier, redundant with the State guard, and adds a per-unit field for one branch.

### `maybeDropChestForCampLocked(camp *NeutralCamp)`

1. Resolve the group's loot table:
   - If `camp.SpawnedGroupID == ""` → no-op (defensive; shouldn't happen if a wipe just occurred).
   - `tier := resolveNeutralTier(camp.CurrentTier)`; `group, ok := getNeutralGroup(tier, camp.SpawnedGroupID)`. If `!ok` or `group.LootTable == ""` → no-op.
   - `table, ok := getLootTable(group.LootTable)`. If `!ok` → log warn, no-op (catalog load should have caught this).
2. Roll the outer 1..100: `roll := s.rngLoot.Intn(100) + 1`.
3. Find the entry whose `[Min, Max]` contains `roll`. If none (gap) → no chest. Return.
4. Resolve the entry's `packagedItems[entry.Entry]`:
   - `resource_bundle`: collect `pkg.Resources` directly into a fresh `resourceGrants` map.
   - `item_subtable`: roll `1..maxOfEntries(pkg.Entries)` using `s.rngLoot`. If the roll lands in a gap, the sub-table grants nothing (log info, chest still spawns with empty `ItemGrants`). On hit, append `entry.Item` to `itemGrants`. *(One item per sub-table hit. Multi-item sub-tables can be expressed by chaining tables — out of scope for v1.)*
5. Skip chest creation entirely if both `resourceGrants` and `itemGrants` are empty (sub-table-gap-only outcome): the result is indistinguishable from "no drop" so don't litter the map with an empty chest. Log info.
6. `s.spawnLootDropLocked(camp, resourceGrants, itemGrants)` — allocates next ID, computes world center from `(camp.X, camp.Y)` × `CellSize`, inserts into `s.LootDrops`.

### Pickup completion

Distance check runs in `tickLootDropsLocked` (called once per tick, after movement). For each unit with `OrderPickupLoot` and a non-zero `PickupLootID`:

1. Resolve `drop := s.LootDrops[unit.PickupLootID]`. If `nil` → drop was already collected; clear the order, set unit to `OrderIdle`. (AI_RULES rule 3 validation.)
2. Resolve `player := s.Players[unit.OwnerID]`. If `nil` → drop the order silently.
3. Distance check against `unitRadius + lootPickupRadius` (suggest `lootPickupRadius = cellSize * 0.5`, tuned during implementation). If out of range → leave the order alone (the movement system is already steering the unit toward `drop.X, drop.Y`).
4. In range → call `grantLootDropToPlayerLocked(player, drop)` then `delete(s.LootDrops, drop.ID)`, clear `unit.PickupLootID`, set `unit.Order = OrderState{Type: OrderIdle}`.

### `grantLootDropToPlayerLocked(player *Player, drop *LootDrop)`

1. Resources: for each `(key, amount)` in `drop.ResourceGrants`, call `s.grantResourceToPlayerLocked(player, key, amount)` (new helper, see §grantResourceToPlayerLocked).
2. Items: for each `itemID` in `drop.ItemGrants`:
   - `def, ok := s.itemCatalog[itemID]`. If `!ok` → log warn, skip (catalog drift; should not happen).
   - `success := s.addItemToVaultLocked(player, def)`.
   - If `!success` (vault full) → grant fails for *this item*. See §Vault-full decision below.
3. Emit a `LootCollectedNotification` (new wire type, §Wire Protocol) so the client can render a HUD toast listing what was picked up.

### `grantResourceToPlayerLocked(player *Player, key string, amount int)`

New helper, lives in `state_items.go` (resources logically belong with vault/items handling). Pure mutation: `player.Resources[key] += amount`. Centralized so future bookkeeping (gain telemetry, achievements, capped resources) has one entry point. **Required by this work — does not exist today.**

### Vault-full decision

**Behavior:** chest is consumed, resources are granted, individual items that can't fit are *lost* (with a HUD notification listing the unfittable items).

**Rationale:** every alternative is worse. Leaving the chest on the ground couples player UX (manage your vault) to world state (chest persists, blocks the camp area, clutters minimap) and creates a partial-pickup state machine. Returning items to the chest with some-resources-granted creates a half-claimed entity. Lost items with a clear notification is honest, simple, and matches the existing item-purchase pattern where "vault full" is a hard cap that the player has to manage. The notification text is the leverage that teaches the player to clear vault before fights.

## Wire Protocol

### New types (`server/pkg/protocol/messages.go`)

```go
// LootDropSnapshot is the per-tick wire view of one ground-loot chest. Sent
// unfiltered (no FOW gating) so chests behave like POI dots on the minimap —
// the player can always navigate back to an uncollected chest.
//
// Resources and ItemIDs mirror the chest's pre-rolled contents so the client
// can render a hover tooltip showing what the chest contains *before* the
// player collects it. The same values are granted to the player on pickup
// (with vault-full handling per §Vault-full decision).
type LootDropSnapshot struct {
    ID string  `json:"id"`
    X  float64 `json:"x"`
    Y  float64 `json:"y"`
    // IconKey is a stable string the client maps to its chest sprite. v1 uses
    // a single "treasure_chest" key; future tiers could vary the visual.
    IconKey   string         `json:"iconKey"`
    Resources map[string]int `json:"resources,omitempty"`
    ItemIDs   []string       `json:"itemIds,omitempty"`
}

// PickupLootCommandMessage is the right-click order for a unit to walk to and
// pick up a ground chest. Mirrors the GatherCommandMessage shape exactly so
// the client transport / replay layers treat it uniformly.
type PickupLootCommandMessage struct {
    Type     string `json:"type"`     // "pickup_loot_command"
    UnitIDs  []int  `json:"unitIds"`
    TargetID string `json:"targetId"` // LootDrop.ID
}

// LootCollectedNotification is pushed to the collecting player when a pickup
// completes. Lets the HUD render "+50 gold, +1 Broad Sword" toast. Items that
// failed to fit in the vault appear in OverflowItemIDs.
type LootCollectedNotification struct {
    Type             string         `json:"type"` // "loot_collected"
    PlayerID         string         `json:"playerId"`
    LootDropID       string         `json:"lootDropId"`
    Resources        map[string]int `json:"resources,omitempty"`
    ItemIDs          []string       `json:"itemIds,omitempty"`
    OverflowItemIDs  []string       `json:"overflowItemIds,omitempty"`
}
```

### Added field on `MatchSnapshotMessage`

```go
LootDrops []LootDropSnapshot `json:"lootDrops,omitempty"`
```

Iteration order is sorted by `LootDrop.ID` for determinism / stable diffs.

### Added field on `UnitSnapshot`

```go
PickupLootID string `json:"pickupLootId,omitempty"`
```

So the client can render a "carrying chest" or "moving to pickup" indicator on the assigned unit. Cleared when the order finishes or is replaced.

## Pickup Mechanic

### Order plumbing (server)

New `OrderType`: `OrderPickupLoot` (added to the existing `iota` block in `state.go`). Adds a fourth name to `orderTypeString` (new constant `protocol.OrderStringPickupLoot = "pickupLoot"`).

New field on `Unit`: `PickupLootID string` — empty when not pickup-bound. Stored by ID per AI_RULES; resolved at point-of-use in `tickLootDropsLocked` each tick.

New public entry point in a new file `server/internal/game/state_loot_pickup.go`:

```go
func (s *GameState) PickupLootWithUnits(playerID string, unitIDs []int, lootDropID string)
```

- Acquires `s.mu`.
- Mirrors `MoveUnits` validation: filter by ownership, drop dead units, drop units that don't exist.
- Resolves `drop := s.LootDrops[lootDropID]`. If missing → no-op (silent, matches existing pattern for stale resource-node gathers).
- For each valid unit:
  - `s.resetUnitMovementLocked(unit, orderID)` (shared movement order ID for the selected group, same pattern as `GatherWithUnits`).
  - `unit.Order = OrderState{Type: OrderPickupLoot, DestX: drop.X, DestY: drop.Y}`.
  - `unit.PickupLootID = drop.ID`.
  - `s.assignUnitPath(unit, protocol.Vec2{X: drop.X, Y: drop.Y}, blocked, nil)`.
- All units in a multi-select walk toward the same chest; whoever arrives first picks it up; the rest fall through the `drop == nil` branch in `tickLootDropsLocked` and clear back to `OrderIdle`.

### Combat AI integration

`OrderPickupLoot` behaves like `OrderMove` for combat-AI purposes:
- Combat AI does NOT engage on the way (no auto-aggro, matches `OrderMove` semantics — the player wants the chest, not a fight).
- Any other order (Move, Attack, AttackMove, Hold, Gather, Build) issued to the unit clears `PickupLootID` and replaces `Order`. Wherever existing code clears `GatherTargetID` on order change (see `state_movement.go:MoveUnits` and analogues), add a matching `PickupLootID = ""` line.

### Client right-click routing

In `client/src/game-portal/src/game/input/InputManager.ts` (or whichever file owns right-click dispatch — confirm during implementation), extend the right-click target-classification chain to check "is the hovered entity a LootDrop?" *before* the move-to-ground fallback:

```
Right-click priority (existing → with new step):
  1. enemy unit          → AttackCommand
  2. resource node       → GatherCommand
  3. deposit-cap building → DepositCommand
  4. LootDrop            → PickupLootCommand   ← NEW (before fallback)
  5. ground              → MoveCommand
```

LootDrops are rendered as a non-blocking world entity (a sprite layer in the same render pass as resource nodes); the input system queries the LootDrop store for hit-testing.

### Hover tooltip (client)

Hovering the chest sprite shows a tooltip listing the pre-rolled contents the player will receive on pickup. Contents come straight off `LootDropSnapshot.Resources` and `LootDropSnapshot.ItemIDs`; item display names + icons are resolved through the existing client-side `ItemDef` catalog store (the same store used by the vault HUD), so labels stay consistent across the game.

Tooltip layout (top to bottom, only sections with content render):
- "Treasure Chest" title
- Resources block — one row per (key, amount), e.g. "+50 gold, +15 wood"
- Items block — one row per item id, e.g. "Broad Sword (Common Weapon)" with the item icon. Pulled from the catalog store; falls back to the raw id if the catalog hasn't loaded yet.

Implementation reuses the existing tooltip primitive used for vault items / unit info — no new tooltip framework. Hover is detected by the same hit-test path that drives the right-click pickup classification (§Client right-click routing).

## File Map

**New (server):**
- `server/internal/game/catalog/neutral_groups/loot_tables.json` — example data with `raider_loot`
- `server/internal/game/loot_table_defs.go` — catalog loader + public lookup API
- `server/internal/game/state_loot_drops.go` — `LootDrop` runtime, `maybeDropChestForCampLocked`, `spawnLootDropLocked`, `tickLootDropsLocked`, `grantLootDropToPlayerLocked`, snapshot helper
- `server/internal/game/state_loot_pickup.go` — `PickupLootWithUnits` entry point + per-unit order assignment

**Changed (server):**
- `server/internal/game/state.go` — register `OrderPickupLoot` constant + string mapping; add `LootDrops map[string]*LootDrop` + `nextLootDropID int` fields on `GameState`; insert `s.tickLootDropsLocked()` into the existing tick order (after movement tick, before snapshot build)
- `server/internal/game/state_neutral_camps.go` — wipe-transition guard in `onUnitRemovedFromCampLocked`; set `camp.SpawnedGroupID` in `spawnGroupForCampLocked`; reorder `despawnNeutralCampLocked` to transition state before removals
- `server/internal/game/neutral_group_defs.go` — add `LootTable` field to `NeutralGroup`; validate `LootTable` references at catalog load (panic-on-miss)
- `server/internal/game/state_items.go` — add `grantResourceToPlayerLocked` helper
- `server/internal/game/state_movement.go`, `state_workers.go`, etc. — clear `unit.PickupLootID = ""` wherever existing order-replacement clears other sticky target fields
- `server/internal/ws/handlers.go` — new `"pickup_loot_command"` case routing to `match.State.PickupLootWithUnits`
- `server/pkg/protocol/messages.go` — `LootDropSnapshot`, `PickupLootCommandMessage`, `LootCollectedNotification`, `OrderStringPickupLoot`, `MatchSnapshotMessage.LootDrops`, `UnitSnapshot.PickupLootID`

**New / changed (client):**
- `client/src/game-portal/src/game/network/protocol.ts` — mirror the three new message types and the snapshot fields
- `client/src/game-portal/src/game/network/NetworkClient.ts` — `sendPickupLootCommand(unitIds, lootDropId)` helper
- `client/src/game-portal/src/game/core/GameState.ts` (or equivalent client-side state mirror) — `lootDrops` collection synced from snapshots
- `client/src/game-portal/src/game/render/*` — new `LootDropLayer` rendering chest sprites at `(x, y)`; add chest icon to the minimap POI layer (always visible, no FOW gating)
- `client/src/game-portal/src/game/input/InputManager.ts` (path to confirm) — right-click hit-test against `lootDrops` before falling through to move; hover hit-test feeds the tooltip
- `client/src/game-portal/src/components/*` — chest hover tooltip component (reuses the existing tooltip primitive) reading `LootDropSnapshot.Resources` + `LootDropSnapshot.ItemIDs`; HUD toast component subscribing to `loot_collected` notifications, listing resources and item names from the existing item catalog store

## Test Coverage (high-level)

**Catalog loader (`loot_table_defs_test.go`):**
- Valid `loot_tables.json` parses correctly.
- Unknown `item` ID in a sub-table → load-time panic with file path.
- Unknown `entry` ID in a table → panic.
- Overlapping ranges within a table or sub-table → panic.
- `kind` other than the two known values → panic.
- Group whose `lootTable` references a missing table → panic at neutral-group load time.

**Drop logic (`state_loot_drops_test.go`):**
- Wipe transition spawns one chest when the seed lands inside an entry's range.
- Wipe transition spawns no chest when the seed lands in a top-level gap.
- Sub-table gap → no chest spawned (resource-bundle-empty + item-empty short-circuit).
- Determinism: same seed + same kill order → identical chest contents across two runs. **No hardcoded item IDs or amounts** — derive expected drops from the loaded catalog × the documented roll values.
- Wave-start despawn does NOT spawn chests (state transition ordering invariant).
- "Random" group: chest contents follow the rolled group's `loot_table`, not some other group's.

**Pickup (`state_loot_pickup_test.go`):**
- Right-click pickup → unit walks to chest → on proximity, chest disappears, vault grows, resources increment.
- Multi-select pickup → only one unit collects; others fall back to idle without errors.
- Replacement orders (Move / Attack / Gather) clear `PickupLootID` and the chest stays on the ground.
- Vault full → resources still granted, items listed in `OverflowItemIDs` of the notification, chest still consumed.
- Pickup attempt on a stale `LootDropID` (already collected) → silent no-op, no panic.

**Wire / FOW:**
- `LootDropSnapshot` is included unfiltered (visible to all players, no FOW gating).
- Chest persists across wave transitions (active → prep → active → prep).
- `LootDropSnapshot.Resources` and `LootDropSnapshot.ItemIDs` match the values the player actually receives on pickup (or would receive minus vault-overflow). Hover tooltip pre-pickup must show the same contents the post-pickup notification confirms.

**Tunables hygiene** (project rule):
- Expected vault contents and resource deltas are read from `loot_tables.json` + `itemCatalogSingleton`, never pinned as literal numbers in tests.

## AI_RULES Compliance Summary

- `LootDrop` is referenced by `string` ID throughout. Units store `PickupLootID string`; the registry (`s.LootDrops`) is the single source of truth.
- Every `s.LootDrops[id]` lookup in `tickLootDropsLocked` and `PickupLootWithUnits` checks for `nil` before use and clears the order on miss (AI_RULES rule 3).
- `*LootDrop` is never persisted on a struct that survives the tick. It exists only as a tick-local working value inside `tickLootDropsLocked` and `grantLootDropToPlayerLocked`.
- `*Unit` is passed into `grantLootDropToPlayerLocked` only as a tick-local helper parameter (within-tick `*Unit` is allowed per AI_RULES rule 4).
- `tickLootDropsLocked`, `maybeDropChestForCampLocked`, `spawnLootDropLocked`, `grantLootDropToPlayerLocked`, `grantResourceToPlayerLocked` are all `Locked`-suffixed and assume `s.mu` is held.
- All RNG is `s.rngLoot`. No `math/rand`, no wall-clock, no map iteration order driving outcomes.
- Snapshot iteration sorts by `LootDrop.ID` for deterministic wire output.

## Out of Scope (Deferred)

- Chest expiry / despawn timers.
- Loot ownership (claimable-by-killer / team).
- Tiered chest visuals (single `IconKey = "treasure_chest"` v1).
- Multi-item sub-tables (one item per sub-table hit; chain through additional tables if needed).
- Resource keys beyond `gold` / `wood`.
- Loot tables that vary independently of group (the group is the table key for v1).
- Mid-pickup interruption recovery (e.g. unit dies en route → another unit auto-takes over). V1: surviving selected units already have the order via the multi-select fan-out; no auto-reassignment if the whole selection dies.
- Replay metadata for chests beyond what's already in the snapshot stream.

## Implementation Handoff

**For `go-backend-engineer`:** files listed in §File Map (server side). Land in this order to keep PRs reviewable: (1) loot catalog loader + tests, (2) `LootDrop` runtime + wipe trigger + drop tests with a stubbed `OrderIdle` (no pickup yet), (3) `OrderPickupLoot` + pickup tick + ws handler + grant helpers, (4) `LootCollectedNotification` wire-up. Each step shippable on its own.

**For `vue-frontend-engineer`:** wait for Phase 2 (chests visible) before starting render; pickup command needs Phase 3 server-side. Component structure follows existing resource-node layer + gather-command flow — no new abstractions.

**For `qa-engineer`:** the determinism, vault-full, and wave-transition tests are the load-bearing ones — drops and pickup are otherwise straightforward right-click flows that mirror gather.
