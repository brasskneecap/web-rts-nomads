## Context

The existing item / shop system has three pieces:

1. **Item catalog** (`server/internal/game/items.go`) — embedded JSON files under `catalog/items/`. Each `ItemDef` carries a `RequiredBuilding` string that gates the item by **building type** (every marketplace sells every item with `requiredBuilding: "marketplace"`).
2. **Marketplace** (`catalog/buildings/marketplace.json`) — a player-buildable building with the `item-purchase` capability. There is no per-instance shop state; the shop is the union of every marketplace-eligible item.
3. **Purchase handler** (`handlePurchaseItemLocked` in `state_items.go`) — validates building ownership, capability, item-catalog membership, the `RequiredBuilding` type gate, vault capacity, and gold; then mutates the player's vault.

Adjacent systems we will reuse:

- **`PlayerFOW.KnownBuildings`** (`fow.go`) — per-player map keyed by building ID of every building the player has ever seen, populated by `recomputeFOWLocked`. This is exactly the "discovered" relation; we do not need a new vision field.
- **Loot tables** (`loot_table_defs.go`) — already roll items from a packaged-items tree using the match's seeded RNG. The same rolling machinery can populate a shop's inventory at match start.
- **Neutral camp guard pattern** (`state_neutral_camps.go`) — already spawns guard squads with `AliveUnitIDs`, lifecycle tied to wave transitions. We will NOT inherit the wave-respawn behavior: shop guards spawn once at match start and stay dead once cleared.

Constraints from `AI_RULES.md`:

- ID-based targeting: guards are referenced by `[]int` of unit IDs, never by pointer.
- `*Locked` discipline: all new helpers assume `s.mu` is held and end in `Locked`.
- Determinism: inventory rolls and guard spawns use the seeded RNG, iterated in sorted order.
- No tick-path I/O: discovery and purchase resolution are per-event, not per-tick.

## Goals / Non-Goals

**Goals:**

- One shared notion of "shop inventory" applicable to player marketplaces and neutral shops alike.
- Three authoring modes selectable per building: rolled loot table, fixed list, catalog-filter fallback (marketplace legacy behavior). Modes are mutually exclusive per instance, declared on the building.
- Guard-lock pattern that is opt-in per neutral shop instance — an unguarded `neutral-shop` works the same as one whose guards have been cleared.
- Discovery is per-player and sticky, sourced from existing FOW.
- A unified Shop UI plus a click-to-focus path from any shop building on the map.

**Non-Goals:**

- No conquering / capturing of neutral shops by combat damage. They have no HP / no `BuildingTile.Occupied` flip; they unlock when their guard squad dies and stay neutral-owned forever.
- No shop UI for resource buildings, towers, or any building without the `item-purchase` capability. The capability remains the single source of truth for "shop or not."
- No inventory restocking, refreshing, or random shuffling during the match. Inventory is set once at match start and stays static thereafter.
- No multi-player adversarial gating (e.g. "if Alice clears the shop, Bob can't buy"). Cleared shops are open to any player who has discovered them.
- No editor-UI work in this change beyond authoring metadata in the existing metadata editor; the editor already supports arbitrary keys.

## Decisions

### Decision: Shop inventory is per-building, populated at match start, immutable thereafter

`BuildingTile` gains three first-class fields:

```go
ShopInventory      []string `json:"shopInventory,omitempty"`      // runtime: current item IDs sold
ShopLootTableID    string   `json:"shopLootTableId,omitempty"`    // authored: roll source
ShopFixedInventory []string `json:"shopFixedInventory,omitempty"` // authored: fixed list
```

At match start, a new `populateShopInventoriesLocked()` walks every `BuildingTile` whose `Capabilities` contains `"item-purchase"` and fills `ShopInventory` exactly once using this precedence:

1. If `ShopFixedInventory` is non-empty → `ShopInventory = ShopFixedInventory` (copy).
2. Else if `ShopLootTableID` is non-empty → roll the table via the existing loot-table machinery, accumulate item IDs into `ShopInventory`.
3. Else, for `BuildingType == "marketplace"` only → fill from the catalog filter (every item with `requiredBuilding: "marketplace"`). This preserves today's behavior for legacy maps.
4. Else → `ShopInventory` stays nil (the building advertises `item-purchase` but has nothing to sell). This is a configuration warning, not a panic.

Marketplace buildings constructed at runtime (via the existing build pipeline) go through the same population step when construction completes.

**Alternatives considered:**

- *Per-purchase catalog lookup (no per-building state):* The cleanest server change but kills (1) instance variance, (2) loot-table rolls, (3) the "discovered shop" UI which needs an authoritative list per building. Rejected — moving the inventory onto the building is the whole point of the feature.
- *Live inventory mutation (e.g. shops restock between waves):* Out of scope per Non-Goals. We add no restocking RPC; the field is set once.

### Decision: `neutral-shop` is a new building type that reuses the existing neutral player slot

Add `server/internal/game/catalog/buildings/neutral-shop.json` with:

```json
{
  "type": "neutral-shop",
  "class": "neutral",
  "buildable": false,
  "width": 2,
  "height": 2,
  "maxHp": 0,
  "buildSeconds": 0,
  "resourceCost": {},
  "capabilities": ["item-purchase"],
  ...
}
```

At runtime, neutral-shop buildings in `MapConfig.Buildings` are owned by the existing `neutralPlayerID` slot (the same one neutral camp units use). No new ownership concept is introduced. `class: "neutral"` lets the editor brush it; `buildable: false` keeps it out of the player's build menu.

**Alternatives considered:**

- *Reuse the `marketplace` building type with a `neutral: true` metadata flag:* Overloads one type with two ownership models. Rejected — type-based dispatch is cleaner.
- *Make every neutral building a `neutral-shop`:* Too restrictive; future neutral buildings (a quest-giver, a shrine, etc.) would not naturally inherit shop semantics.

### Decision: Guard squad spawns once, lock flips off permanently

A neutral-shop instance may declare in its metadata:

```json
{
  "guardGroupId": "small_raider_group",
  "guardStartingTier": 1,
  "guardAggroRange": 250,
  "guardLeashRange": 320
}
```

At match start, `spawnShopGuardsLocked()` reads each neutral-shop's metadata, rolls a group from the existing neutral-group catalog (or takes the explicit `guardGroupId`), spawns the units around the building's perimeter using the existing `getTownhallSpawnPositionsLocked`-style placement helper, and stores their IDs on the building as `ShopGuardUnitIDs []int`. `ShopLocked` is `true` while any of those IDs resolves to a live unit (`getUnitByIDLocked` returns non-nil with `HP > 0`).

The lock state is **computed**, not stored: a small `shopLockedLocked(building) bool` helper iterates `ShopGuardUnitIDs`. The persisted `ShopLocked` field is filled at snapshot time so the client sees a stable boolean. Once every guard ID resolves to nil or HP=0, the helper returns false forever — guards do not respawn (unlike neutral camps).

An unguarded neutral shop (`guardGroupId` empty) spawns no guards, `ShopGuardUnitIDs` is empty, and `shopLockedLocked` returns false from the start.

**Alternatives considered:**

- *Reuse `NeutralCamp` directly:* `NeutralCamp` respawns its squad on every wave-clear (`NeutralCampWaveHidden` → spawn). That is wrong for a one-time unlock. Rejected.
- *Store `ShopLocked` as ground truth and update it on every unit-kill event:* More state, more bugs. Computing it on read is O(guard count) and shop count × wave-end frequency is trivially small.

### Decision: Discovery reuses `PlayerFOW.KnownBuildings`

A building is **discovered by playerID** when `s.FOW[playerID].KnownBuildings[building.ID] != nil`. This is set by the existing `recomputeFOWLocked` pass the first time any unit owned by the player has vision on the building's cells. No new field, no new write path.

The snapshot filter (`buildingSnapshotForLocked` or equivalent) sets the wire-protocol field `BuildingTile.ShopDiscovered = (KnownBuildings[building.ID] != nil)` per viewer. For player-owned marketplaces the field is `true` immediately (the player built it, so it's in `KnownBuildings`).

**Alternatives considered:**

- *Separate `s.ShopDiscovery map[buildingID]map[playerID]bool`:* Duplicates state. Rejected.
- *Always-discovered (no fog gate):* Defeats the exploration reward. Rejected.

### Decision: Purchase validation reads from the building, not the catalog filter

`handlePurchaseItemLocked` gains the following checks in addition to the existing ones, in this order:

1. (existing) building exists, visible, not under construction, has `item-purchase` capability.
2. (new) item ID is in `building.ShopInventory`. The legacy `def.RequiredBuilding == building.BuildingType` check is **removed** as a hard gate — it becomes informational on the item def, since per-building inventory is now authoritative.
3. (new) for buildings whose owner is `neutralPlayerID`: `building.ID ∈ s.FOW[playerID].KnownBuildings` (discovered) AND `shopLockedLocked(building) == false` (cleared).
4. (existing) for player-owned buildings: `*building.OwnerID == playerID`.
5. (existing) afford check, vault-capacity check, deduct, add to vault.

**Alternative considered:** keep `RequiredBuilding` as an additional AND-gate. Rejected — it conflicts with the new "the shop carries its inventory" model. Migrating an item to be available in a non-marketplace shop should be a per-building authoring change, not a catalog edit.

### Decision: ShopPanel.vue is one component with tabs, opened from multiple entry points

Component lives at `client/src/game-portal/src/components/ShopPanel.vue`. State: a Pinia / composable selector that emits `(shops: Shop[], focusedShopId: string | null)`. The shop list is derived as:

```
shops = [
  ...buildings owned by me with item-purchase capability,
  ...buildings owned by neutralPlayerID with item-purchase capability AND shopDiscovered=true
]
```

Each shop becomes a tab; tab order is owned-first then discovered-by-id. Each tab renders the shop's `shopInventory` with item icon, name, description, gold cost, vault-affordability, and a Buy button. A locked neutral shop shows the tab with a 🔒 indicator and a "Guards remain" message in place of the Buy buttons.

Entry points:

- A "Shop" button on the existing match HUD opens the panel with `focusedShopId = null` (defaults to first tab).
- Clicking any owned marketplace or discovered neutral shop building on the map opens the same panel with `focusedShopId = building.id`.

This is one component, one view-model, one set of test cases.

## Risks / Trade-offs

- **Risk:** Loot-table rolls produce different inventories on replay if a different seed leaks in (e.g., the editor's "preview" RNG vs the match RNG). → **Mitigation:** `populateShopInventoriesLocked` uses `s.rngLoot` (the same seeded RNG `loot_table_defs.go` already uses for kill drops). A snapshot test pins one map's roll output to the seed to catch unintended RNG bleed.
- **Risk:** Snapshot size grows by one extra `shopInventory []string` per visible building. → **Mitigation:** `omitempty` keeps non-shop buildings free; for shops the list is short (typically <12 IDs). Snapshot growth is a few bytes per shop per snapshot, negligible against current frame sizes.
- **Risk:** A map author lists a `shopLootTableId` that does not exist in the loot-table catalog. → **Mitigation:** Validate at match-start population, log an `slog.Error` naming the offending building ID, and leave `ShopInventory` nil so the building sells nothing rather than panicking the match.
- **Risk:** `neutralPlayerID` ownership today is used only by camp units; making it a building owner could surprise code that filters `OwnerID == nil` for "unowned." → **Mitigation:** Audit `getBuildingOwner*`, `Visible`, and selection code for neutral-vs-unowned assumptions during implementation; the existing `enemy-spawnpoint` building already uses non-player ownership, so the path is precedented.
- **Trade-off:** `ShopLocked` is computed on read rather than mirrored. Each read iterates the guard ID list (~5 guards typical). This is fine for snapshot-time and purchase-time use, but if a future feature needs lock state inside the tick loop (e.g., a passive radius effect), we will need to mirror it into a cached boolean. Out of scope today.

## Migration Plan

No external migration. Existing maps with player-built marketplaces continue to work via the catalog-filter fallback in `populateShopInventoriesLocked`. The `RequiredBuilding` field on `ItemDef` is preserved (still parseable, no longer gating); a future change can remove it once no map relies on it.

Rollout in order:

1. Schema fields on `BuildingTile` (server protocol + client mirror).
2. `populateShopInventoriesLocked` + the marketplace fallback path.
3. Purchase validation extension.
4. `neutral-shop` building def + guard-spawn helper + lock computation.
5. FOW-driven `ShopDiscovered` plumb-through in the snapshot filter.
6. `ShopPanel.vue` + composable + entry points.
7. Tests (server-side gating + client component).

Rollback: revert the change. No persistent profile state is touched.

## Open Questions

- Inventory pricing: today's `ItemDef.CostGold` is global. Should per-instance shops be able to override per-item price? **Decision deferred** — keep global pricing for v1; revisit if game design needs price variance per shop.
- UI iconography for "locked by guards": a 🔒 plus a count of remaining guards versus a guard-portrait list. **Defer to implementation;** the component's interface allows either.
