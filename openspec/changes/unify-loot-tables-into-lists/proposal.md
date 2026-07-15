## Why

`unify-item-lists-and-recipes` collapsed every grouping of items into one authorable `ListDef` — except loot, which was left alone because it was "a genuinely different shape". It isn't. A loot `item_subtable` is *literally* a weighted list: item IDs with roll ranges. The only reason it looked different is that it was trapped inside `loot_tables.json` with no editor, alongside two things that genuinely aren't lists (resource bundles) and one thing that genuinely isn't a list either (the table that weights them together).

So the item economy still has a hidden fourth entity, `packagedItems`, that no one can author, and a `tables` block that can only be edited by hand-writing JSON.

## What Changes

- **BREAKING** `packagedItems` is deleted. Its 7 `item_subtable`s become **weighted lists** in `catalog/lists/`. Its 2 `resource_bundle`s become inline rows on the tables that used them.
- **`ListDef` gains a weighted form.** A list is either:
  - **uniform** — `items: [...]`, every member equally likely; or
  - **weighted** — `maxRoll` + `entries: [{item, min, max}]`, each member owning a slice of the die.
  Exactly one form per list. Weighted lists are what loot subtables were.
- **`TableDef` is a new first-class, authorable entity** (`catalog/tables/<id>.json`). A table is a `maxRoll` plus rows that each own a roll range and one outcome: **roll a list**, **grant resources**, or **nothing**.
- **BREAKING** Roll ranges must cover `1..maxRoll` with **no gaps and no overlaps**. Today `raider_loot` expresses its 10% no-drop as an implicit gap; it now says so out loud with a `nothing` row. Gaps become a validation error, so a hole can only ever be deliberate.
- **Weights apply everywhere a list is read.** A shop bound to a weighted list samples it *by weight*, so a rare item is rare on the shelf as well as in a chest. (Today neutral shops sample their list uniformly — this is a deliberate behavior change.)
- A **Tables tab** joins Items and Lists in the item editor, with coverage validation that shows exactly which rolls land where.
- `SetMerchantItemAvailability`, `merchantSubtableForCategory` and `renormalizeSubtable` are deleted — dead code, called only by tests, and superseded by editing the list directly.

## Capabilities

### New Capabilities
- `loot-tables`: The `TableDef` entity — a weighted roll over lists, resource grants, and no-drop outcomes — and the coverage rules that make a table total.

### Modified Capabilities
- `item-lists`: `ListDef` gains a weighted form (`maxRoll` + ranged entries), and weights now apply wherever a list is read, not just in loot.
- `item-catalog-editor`: A third tab, Tables, and the coverage validation behind it.

## Impact

**Deleted**
- `catalog/neutral_groups/loot_tables.json`, `PackagedItem`, `LootTableEntry`, `LootTableDef`, `SetMerchantItemAvailability`, `merchantSubtableForCategory`, `renormalizeSubtable`

**Server**
- `lists.go` — `ListDef` gains `MaxRoll` + `Entries`; `ItemIDs()` and `IsWeighted()` helpers; coverage validation
- `tables.go` (new) — `TableDef`, loader, overlay, coverage validation
- `state_loot_drops.go` — rolls a table row: list / resources / nothing
- `state_shop.go` — `rollShopLootTableLocked` rolls a `TableDef`; `sampleItemsFromListLocked` becomes weight-aware
- `neutral_group_defs.go` — `LootTable` now names a `TableDef`
- HTTP — `GET /catalog/tables`, `POST /tables`, `DELETE /tables/{id}`

**Client**
- `tableDefs.ts`, `TableEditorPanel.vue`, third tab in `ItemCatalogEditor.vue`
- `listDefs.ts` — the weighted form
- Map editor — the loot-table selector now lists `TableDef`s

**Data migration**
- 7 subtables → weighted lists; 3 tables → `catalog/tables/`; 2 resource bundles → inline table rows. Every existing subtable is already gap-free, so no rebalancing is needed; `raider_loot`'s 51–60 gap becomes an explicit `nothing` row preserving its exact 10%.
