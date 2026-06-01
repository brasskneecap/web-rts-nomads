## ADDED Requirements

### Requirement: Per-building shop inventory replaces catalog-filter gate

The system SHALL store the set of item IDs sold by each shop building on the building instance itself, as `BuildingTile.ShopInventory []string`. Authors SHALL declare a building's inventory source via one of two metadata fields, `ShopFixedInventory []string` (explicit list of item IDs) or `ShopLootTableID string` (key into the loot-table catalog). At match start, the system SHALL populate `ShopInventory` exactly once per building with the `item-purchase` capability, using this precedence: (1) a non-empty `ShopFixedInventory` is copied verbatim, (2) otherwise a non-empty `ShopLootTableID` is rolled via the seeded loot RNG and the resulting item IDs are appended, (3) otherwise, ONLY for `BuildingType == "marketplace"`, the inventory is filled from a focused starter list `defaultMarketplaceStarterInventory` (currently `broad_sword` + `potion_common_heal`). The legacy "every catalog item whose `RequiredBuilding == 'marketplace'`" set is intentionally not used as the fallback — authors who want a richer marketplace declare it via `shopFixedInventory` on the map JSON. Any other building with the `item-purchase` capability and no inventory source SHALL log a warning and leave `ShopInventory` nil. A player-built marketplace whose construction completes mid-match SHALL run the same population step at completion.

#### Scenario: Fixed-inventory neutral shop sells exactly the authored items
- **WHEN** the map authors a `neutral-shop` with `metadata.shopFixedInventory = ["broad_sword", "minor_healing_potion"]`
- **THEN** after match start `building.ShopInventory` equals `["broad_sword", "minor_healing_potion"]` and no other items are purchasable from that building

#### Scenario: Loot-table-rolled inventory is deterministic under the match seed
- **WHEN** the map authors a `neutral-shop` with `metadata.shopLootTableId = "merchant_basic"` and the match is run twice with the same seed
- **THEN** both runs populate `building.ShopInventory` with the same item IDs in the same order

#### Scenario: Player-built marketplace with no inventory source ships with the starter list
- **WHEN** a player builds a `marketplace` on a map that declares neither `shopFixedInventory` nor `shopLootTableId` on the building
- **THEN** `building.ShopInventory` equals `defaultMarketplaceStarterInventory` — currently `["broad_sword", "potion_common_heal"]`

#### Scenario: Non-marketplace shop with no inventory source sells nothing
- **WHEN** a building has the `item-purchase` capability, `BuildingType != "marketplace"`, and neither `ShopFixedInventory` nor `ShopLootTableID` is set
- **THEN** `building.ShopInventory` is nil, an `slog.Warn` names the building ID, and any purchase attempt against that building no-ops

#### Scenario: Unknown loot-table ID does not panic the match
- **WHEN** a building declares `shopLootTableId = "nonexistent_table"`
- **THEN** match start completes successfully, the building's `ShopInventory` stays nil, and an `slog.Error` names the offending building ID and table key

### Requirement: New `neutral-shop` building type

The system SHALL ship a new building definition `catalog/buildings/neutral-shop.json` with `class: "neutral"`, `buildable: false`, capabilities `["item-purchase"]`, and `maxHp: 0` (not destructible). The building SHALL be brushable in the map editor's neutral building palette and SHALL be loaded into game state with `OwnerID == neutralPlayerID` for every `MapConfig.Buildings` entry whose `BuildingType == "neutral-shop"`.

#### Scenario: Map editor exposes the neutral-shop brush
- **WHEN** the map editor's building palette is queried for neutral-class buildings
- **THEN** `neutral-shop` appears in the available brush set

#### Scenario: Neutral shop is owned by the neutral player at match start
- **WHEN** a map authors a `neutral-shop` at grid (12, 8) and a match starts
- **THEN** the building's `OwnerID` equals `neutralPlayerID`, `Visible == true`, and `Occupied == true`

### Requirement: Guard-locking for neutral shops

A neutral shop MAY declare in its metadata a `guardGroupId` (and optionally `guardStartingTier`, `guardAggroRange`, `guardLeashRange` mirroring `NeutralSpawn`). At match start, the system SHALL spawn the declared guard squad at walkable cells adjacent to the building, store their unit IDs in `BuildingTile.ShopGuardUnitIDs []int`, and consider the building `locked` while any guard ID resolves to a unit with `HP > 0`. Guards SHALL NOT respawn after being killed. A neutral shop with no `guardGroupId` SHALL spawn no guards and SHALL be unlocked from the start.

#### Scenario: Guarded shop is locked until guards are cleared
- **WHEN** a `neutral-shop` declares `metadata.guardGroupId = "small_raider_group"` and the match starts
- **THEN** the resulting `BuildingTile.ShopGuardUnitIDs` is non-empty, the lock-evaluation helper returns `true`, and the snapshot payload sets `shopLocked: true`

#### Scenario: Lock flips to unlocked when the last guard dies
- **WHEN** every unit ID in `BuildingTile.ShopGuardUnitIDs` resolves to either nil or `HP <= 0`
- **THEN** the lock-evaluation helper returns `false`, the snapshot payload sets `shopLocked: false`, and the lock SHALL stay `false` for the remainder of the match even if new units happen to share IDs

#### Scenario: Unguarded neutral shop is unlocked from match start
- **WHEN** a `neutral-shop` declares no `guardGroupId` and the match starts
- **THEN** `BuildingTile.ShopGuardUnitIDs` is empty and `shopLocked` is `false` from tick 0

### Requirement: Discovery is per-player and sourced from existing FOW

A neutral shop SHALL be considered **discovered** by `playerID` exactly when `s.FOW[playerID].KnownBuildings[building.ID]` is non-nil. The system SHALL NOT introduce a separate discovery store; the existing `recomputeFOWLocked` pipeline populates `KnownBuildings` the first time any vision source belonging to `playerID` covers the building's cells. The snapshot filter SHALL set `BuildingTile.ShopDiscovered = (KnownBuildings[building.ID] != nil)` per-viewer for buildings with the `item-purchase` capability.

#### Scenario: Player who has never had vision on a neutral shop sees it as undiscovered
- **WHEN** a player joins a match containing a `neutral-shop` that no player-owned unit has ever revealed
- **THEN** that player's snapshot of the building has `shopDiscovered: false`

#### Scenario: Discovery sticks after vision is lost
- **WHEN** a player's unit has vision on a `neutral-shop` for one tick and then moves out of range
- **THEN** subsequent snapshots to that player still report `shopDiscovered: true` (via the FOW `KnownBuildings` cache)

#### Scenario: Player-owned marketplaces are discovered immediately
- **WHEN** a player constructs a marketplace
- **THEN** that player's snapshots of the building have `shopDiscovered: true` from the tick of completion

### Requirement: Purchase validation reads from per-building inventory and discovery state

The `handlePurchaseItemLocked` server entry point SHALL validate, in order:

1. The target building exists, is `Visible`, has the `item-purchase` capability, and is not `underConstruction`.
2. The requested item ID appears in `building.ShopInventory`.
3. If the building's `OwnerID == neutralPlayerID`: the purchasing player has discovered the building (per the FOW rule above) AND the building is not `locked`.
4. If the building's `OwnerID` is a real player: that owner ID equals the purchasing player ID.
5. The player has enough gold and enough vault capacity (existing checks).

Validation failures SHALL be silent no-ops (no panic, no error response). The legacy `ItemDef.RequiredBuilding` field SHALL be preserved on the def for display purposes but SHALL NOT participate in this validation chain.

#### Scenario: Purchase of a non-stocked item is rejected
- **WHEN** a player attempts to purchase `broad_sword` from a building whose `ShopInventory` does not contain that ID
- **THEN** no gold is deducted, no vault entry is created, and the response state is unchanged

#### Scenario: Purchase from a locked neutral shop is rejected
- **WHEN** a player who has discovered a neutral shop attempts to purchase an item from it while at least one guard still has `HP > 0`
- **THEN** the purchase no-ops and the player's gold is unchanged

#### Scenario: Purchase from an undiscovered neutral shop is rejected
- **WHEN** a player whose FOW does not list a neutral shop in `KnownBuildings` attempts to purchase from it (e.g., via a forged client message)
- **THEN** the purchase no-ops

#### Scenario: Cleared neutral shop is purchasable by any discoverer
- **WHEN** Player A clears the guards of a neutral shop and Player B has also discovered it
- **THEN** both Player A and Player B can purchase from the shop independently

### Requirement: Unified Shop UI with click-to-focus

The client SHALL render a single `ShopPanel.vue` that lists every shop the active player can buy from — buildings owned by that player with the `item-purchase` capability AND neutral shops the player has discovered. Each shop SHALL be a tab/section in the panel. The panel SHALL be openable from a "Shop" entry in the match HUD (focused on the first tab), and from clicking any qualifying shop building on the map (focused on that building's tab). Tabs SHALL be ordered owned-first, then discovered-neutral by stable building ID. Locked neutral shops SHALL appear as a visible tab with a lock indicator and SHALL disable their Buy actions in place of disabling the entire tab.

#### Scenario: Newly discovered neutral shop appears in the shop panel
- **WHEN** a player's unit reveals a neutral shop for the first time and the player opens the Shop panel
- **THEN** the newly discovered shop appears as a new tab in the panel alongside the player's own marketplaces

#### Scenario: Clicking an owned marketplace opens the shop panel focused on it
- **WHEN** the player clicks one of their own marketplace buildings on the map
- **THEN** the Shop panel opens with that marketplace's tab active

#### Scenario: Locked shop tab disables Buy but remains visible
- **WHEN** the player opens the Shop panel and one of the discovered neutral shops is currently locked
- **THEN** that shop's tab is present with a lock indicator and the per-item Buy buttons are disabled, while other (unlocked) tabs remain fully interactive
