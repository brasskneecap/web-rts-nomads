## Why

The current shop system is static: every player-built `marketplace` sells the same items (everything in `catalog/items/*.json` with `requiredBuilding: "marketplace"`), there is no neutral "merchant" the player can discover and buy from, and the inventory of any shop cannot change after the catalog is compiled. This blocks two designs the game needs: (1) per-instance shop personality (the wandering merchant in the woods sells different things than the marketplace you build at home), and (2) mid-match discovery rewards (clearing a neutral camp unlocks a real reason to scout).

This change moves shop inventory off the global item catalog and onto each individual building instance, adds a new neutral shop building type that players can locate and unlock during exploration, and ships a unified Shop UI that lists every shop the player can currently buy from.

## What Changes

- **Per-instance inventory on every shop building.** Add `shopInventory` (current item IDs sold) plus optional `shopLootTableId` and `shopFixedInventory` to `BuildingTile`. At match start, every building with the `item-purchase` capability is populated by: (a) rolling its `shopLootTableId` into `shopInventory` when set, (b) taking `shopFixedInventory` as-is when set, or (c) for the player-buildable `marketplace`, falling back to the legacy "all items with `requiredBuilding: marketplace`" so existing maps keep working unchanged. Authors can mix-and-match the three modes per building.
- **New neutral shop building type `neutral-shop`** (class `neutral`, `buildable: false`, has `item-purchase` capability). Placeable in the map editor like any other authored building; can carry `shopLootTableId` or `shopFixedInventory` in its metadata for the inventory mode chosen by the author. Owned at runtime by the existing `neutralPlayerID` slot.
- **Guard-locking** for neutral shops. A neutral shop may declare a guard squad in its metadata (`guardGroupId`, plus the same scaling fields `NeutralSpawn` supports). At match start the guards spawn at the building's perimeter, and the building's `shopLocked` flag is `true` while any guard has HP > 0. When the last guard dies, `shopLocked` flips to `false` and stays false for the rest of the match — clearing is permanent.
- **Discovery gate.** Every player tracks which neutral shops they have personally discovered. A shop is "discovered" by playerID the first tick that any unit owned by playerID has vision on the shop's center cell (using the existing FOW reveal pipeline; no new vision system). Discovered status is sticky — losing vision does not undo it.
- **Purchase validation extended.** `handlePurchaseItemLocked` now requires: (1) the target building exists and has `item-purchase` capability, (2) the requested item ID is in that building's current `shopInventory`, (3) for neutral shops, the purchasing player has discovered the shop AND `shopLocked` is `false`, (4) for player marketplaces, today's "must be owned by purchasing player" rule continues to apply. The legacy item-level `requiredBuilding` field becomes informational only (kept for backward compatibility; superseded by per-building `shopInventory`).
- **New unified Shop screen** (`ShopPanel.vue`). Lists every shop the player can currently buy from — their own owned marketplaces and every discovered, unlocked neutral shop — as tabs/sections. Each tab shows that shop's `shopInventory`, gold cost, vault capacity check, and a Buy action. Clicking any owned marketplace or any discovered neutral shop on the map opens the same panel focused on that shop's tab.
- **Protocol additions.** The `BuildingTile` payload sent to a client includes `shopInventory: []string`, `shopLocked: bool`, and a per-viewer `shopDiscovered: bool`. The existing `PurchaseItemMessage` does not change shape — server-side validation simply reads the new fields.

## Capabilities

### New Capabilities
- `per-building-shop-inventories`: per-instance shop inventory, neutral shop buildings with guard-gated discovery, and the unified Shop UI built on top.

### Modified Capabilities
<!-- The existing item / marketplace behavior is not in openspec/specs/ today (only described in code and inline tests), so there is no archived spec to delta. The behavioral diff is captured fully under the new capability above. -->

## Impact

- **Server (Go):**
  - `server/pkg/protocol/messages.go` — extend `BuildingTile` with `ShopInventory []string`, `ShopLootTableID string`, `ShopFixedInventory []string`, `ShopLocked bool`, `ShopGuardUnitIDs []int`, `ShopDiscovered bool`. The last is filled per-viewer by the snapshot filter; the rest are persistent in `MapConfig.Buildings` (with appropriate `omitempty`).
  - `server/internal/game/catalog/buildings/neutral-shop.json` — new building def.
  - `server/internal/game/state_items.go` — `handlePurchaseItemLocked` reads from the building's `ShopInventory` instead of (or in addition to) the catalog filter; adds discovery + lock gates.
  - `server/internal/game/state_buildings.go` (or a new `state_shop.go`) — populate `ShopInventory` at match start from `ShopLootTableID` (using the existing `loot_table_defs.go` roll machinery) or `ShopFixedInventory`. Marketplace fallback: catalog filter.
  - `server/internal/game/state_neutral_camps.go` or a sibling — spawn guard units for `neutral-shop` buildings at match start (or first reveal — see design), associate them with the building via `ShopGuardUnitIDs`, and flip `ShopLocked` to `false` when the last alive guard's `HP <= 0`.
  - FOW reveal pipeline — when a shop building's cell is first revealed for a player, mark `ShopDiscovered` true for that player (per-player snapshot enrichment in the existing FOW filter).
- **Client (Vue/TS):**
  - `client/src/game-portal/src/components/ShopPanel.vue` — new component implementing the unified shop screen.
  - `client/src/game-portal/src/composables/useShop.ts` (or extension to an existing composable) — aggregates buildings with `shopDiscovered: true` or owned by the player + `item-purchase` capability, into the panel's tab list.
  - `client/src/game-portal/src/game/network/protocol.ts` — mirror the new building fields.
  - Existing per-building selection: clicking an owned marketplace or a discovered neutral shop opens `ShopPanel` focused on that tab (extension of the existing selection flow).
- **Map editor:** authors can place `neutral-shop` buildings and optionally declare `shopLootTableId`, `shopFixedInventory`, and `guardGroupId` in the building's metadata, using the existing metadata-editing UI. No new editor primitive required.
- **No invariant changes:** `ShopGuardUnitIDs` is `[]int`, matching AI_RULES' ID-based-targeting rule. The discovery state and lock are read off the building each purchase attempt; no new pointer fields on tick-loop structs.
- **Backward compatibility:** existing maps with player-built marketplaces continue to sell the catalog-filtered set; no migration needed. The `RequiredBuilding` field on `ItemDef` is preserved and remains informational. New maps can opt into the per-instance inventory by setting `shopLootTableId` or `shopFixedInventory`.
