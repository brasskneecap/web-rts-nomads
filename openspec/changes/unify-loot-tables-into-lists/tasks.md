# Tasks

Sequenced so the tree stays green at each step: the weighted list lands first,
then the table, then the consumers are rewired, then the old catalog dies.

## 1. Weighted lists (server)

- [x] 1.1 `ListDef` gains `MaxRoll int` + `Entries []ListEntry{Item,Min,Max}`; add `IsWeighted()` and `ItemIDs()` (form-agnostic membership)
- [x] 1.2 Validation: exactly one form (never both `items` and `entries`); a weighted list's entries tile `1..maxRoll` with no gaps and no overlaps; every item resolves
- [x] 1.3 `rollListLocked(list)` — weighted lists roll their die, uniform lists pick evenly; both draw from `s.rngLoot`
- [x] 1.4 Tests: weighted roll respects the distribution; gap / overlap / both-forms rejected; `ItemIDs()` identical for uniform and weighted lists of the same members

## 2. Tables (server)

- [x] 2.1 `tables.go` — `TableDef{ID,Name,MaxRoll,Rows}`, `TableRow{Min,Max, List|Resources|Nothing}` + `catalog/tables/` embed + loader
- [x] 2.2 Validation: rows tile `1..maxRoll` (no gaps, no overlaps); each row has EXACTLY one outcome; a named list resolves; resource keys are in the allowed set
- [x] 2.3 Runtime overlay + `SaveTableDef` / `DeleteTableOverride` + `LoadPersistedTablesIntoOverlay()` wired into `main.go`
- [x] 2.4 `rollTableLocked(table)` → (items, resources) — rolls the die, resolves the row, rolls the list when it names one
- [x] 2.5 Tests: each outcome kind; gap/overlap/two-outcome rows rejected; deterministic under a seed

## 3. Migrate the catalog

- [x] 3.1 The 7 `item_subtable`s → weighted lists in `catalog/lists/`, roll ranges preserved verbatim (`basic_weapons` maxRoll 15, `rare_weapons` 20, the four merchant ones 100)
- [x] 3.2 The 3 tables → `catalog/tables/`, with the 2 resource bundles inlined as `resources` rows
- [x] 3.3 **`raider_loot`'s 51–60 gap becomes an explicit `nothing` row** — absorbing it into a neighbour would silently turn a 10% no-drop into 10% more loot
- [x] 3.4 Delete `catalog/neutral_groups/loot_tables.json`
- [x] 3.5 **Migration-fidelity test**: roll every face 1..100 of each migrated table and assert the outcome is identical to the pre-migration table's, face by face (the numbers are pinned in the test as the pre-migration ground truth)

## 4. Rewire the consumers (server)

- [x] 4.1 `state_loot_drops.go` — camp loot rolls a `TableDef` row: list → item, resources → grant, nothing → no chest
- [x] 4.2 `state_shop.go` — `rollShopLootTableLocked` rolls a `TableDef`, collecting distinct items and skipping resource / nothing rows
- [x] 4.3 `state_shop.go` — `sampleItemsFromListLocked` becomes weight-aware (weighted list → sample by weight; uniform → unchanged)
- [x] 4.4 `neutral_group_defs.go` — `LootTable` resolves a `TableDef`; `LootList` still resolves a `ListDef`; still at most one
- [x] 4.5 Delete `loot_table_defs.go`, `PackagedItem`, `LootTableEntry`, `LootTableDef`, `SetMerchantItemAvailability`, `merchantSubtableForCategory`, `renormalizeSubtable` (all dead once the above land)
- [x] 4.6 `go build ./... && go test ./...` green

## 5. Routes

- [x] 5.1 `GET /catalog/tables`, `POST /tables`, `DELETE /tables/{id}` (validation errors → 400)
- [x] 5.2 Route tests: create → serves; gap rejected as 400; delete

## 6. The Tables tab (client)

- [x] 6.1 `tableDefs.ts` (type + store + `fetchTables`), `tableEditorApi.ts` (save/delete)
- [x] 6.2 `listDefs.ts` — the weighted form; `listEditorValidation.ts` — coverage checks
- [x] 6.3 `ListEditorPanel.vue` — a Uniform / Weighted toggle; weighted mode shows per-item ranges + a coverage readout
- [x] 6.4 `TableEditorPanel.vue` — max roll, rows (list / resources / nothing), coverage strip showing what each range yields as a %
- [x] 6.5 Third tab in `ItemCatalogEditor.vue` (Items | Lists | Tables), each pane `v-show`n on a wrapper so only one shows
- [x] 6.6 ~~Map editor — the loot-table selector is fed by `fetchTables`~~ — **N/A, no such selector exists.** Camp loot binds at the neutral-group catalog level (`catalog/neutral_groups/tier_N.json`), which has no editor UI, and `shopLootTableId` is map-JSON-only. There was no loot-table dropdown in either map editor to repoint. The Tables tab authors the tables; binding them stays a catalog edit, as it was before.
- [x] 6.7 Tests: coverage validation (gap / overlap blocks save, complete die allows it); percentages shown; only one tab's panel visible

## 7. Verify

- [x] 7.1 Full `go test ./...`, `vue-tsc -b`, `vitest run`
- [x] 7.2 Confirm the migration-fidelity test (3.5) actually fails if a range is off by one — a guard that cannot fail is worthless
- [ ] 7.3 **Still open — manual.** Drive it in the running app: author a weighted list and a table, bind the table to a camp, clear it, and confirm the drop distribution matches what the editor showed
