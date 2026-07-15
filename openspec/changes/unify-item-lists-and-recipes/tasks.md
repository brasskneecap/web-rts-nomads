# Tasks

Sequenced so the tree stays green at every step: the item-side merge lands and
passes **before** lists are introduced, and lists land **before** any consumer is
rewired to them.

## 1. Fold crafting onto the item (server)

- [x] 1.1 Add `ItemCrafting` struct + `ItemDef.Crafting *ItemCrafting` and an `ItemDef.IsCraftable()` predicate in `items.go`
- [x] 1.2 Validate the crafting block in `validateItemDef`: ≥2 inputs, every input a known item, no self-reference, neither cost negative
- [x] 1.3 Hand-fold the 7 shipped recipes into their item defs (`crafting` block on `fire_sword`, `frost_sword`, `lightning_sword`, `fire_shield`, `frost_shield`, `lightning_shield`, `scimitar` — the last keeps `starter: true`), preserving today's numbers exactly
- [x] 1.4 Rewrite `state_crafting.go` to resolve craft cost + inputs off `ItemDef.Crafting` instead of `getRecipeDef`
- [x] 1.5 Rewrite `state_recipe_purchase.go` to charge `Crafting.RecipeCostGold` off the item def
- [x] 1.6 Rewrite `state_recipe_shop.go`: stock pool = all craftable items; `starterRecipeIDs()` → `starterCraftableItemIDs()` scanning item defs
- [x] 1.7 Rewrite `item_editor.go` `SaveEditorItem` to write only the item (the request already carries a `crafting` block); delete the recipe-sync + `DeleteRecipeOverride` calls
- [x] 1.8 Delete `recipes.go`, `catalog/recipes/**`, the recipe half of `recipe_persistence.go`, and every recipe test file; port their coverage onto items
- [x] 1.9 ~~Add the boot-time one-shot migration that folds any leftover `catalog/recipes/**` overlay file into the matching item overlay's `crafting` block~~ — **NOT NEEDED, not implemented.** The risk it guarded (an author-created craftable item whose crafting data lived only in a recipe overlay file) turned out not to exist: `git ls-tree HEAD -- catalog/recipes/` accounts for every file that was on disk — the 7 shipped recipes and 1 list, all hand-folded in 1.3/4.3. There were zero untracked recipe overlays. Writing a migration for an empty set would be dead code from birth.
- [x] 1.10 `go build ./... && go test ./...` green

## 2. Rename the wire and the profile (server + client)

- [x] 2.1 `PlayerSnapshot.UnlockedRecipeIDs` → `UnlockedCraftableIDs` (`unlockedCraftableIds`); `Player.UnlockedRecipeIDs` → `UnlockedCraftableIDs`
- [x] 2.2 `RecipeStockEntry.RecipeID` → `ItemID`; `purchase_recipe` and `craft_item` payload field `recipeId` → `itemId` (message **type names** stay — the player action is unchanged)
- [x] 2.3 `JoinMatchMessage.KnownRecipeIDs` → `KnownCraftableIDs`
- [x] 2.4 Profile: `PlayerProfile.KnownRecipeIDs` → `KnownCraftableIDs` + v8→v9 migration (pure key rename, values carried verbatim), with a test asserting a v8 profile's learned IDs survive into v9 unchanged
- [x] 2.5 Mirror every rename in `client/src/game-portal/src/game/network/protocol.ts`, `GameClient.ts`, `useGameClient.ts`
- [x] 2.6 `go test ./...` + `vue-tsc -b` + `vitest run` green

## 3. Client reads crafting off the item

- [x] 3.1 Add `ItemDef.crafting` to `itemDefs.ts`; delete `recipeDefs.ts`, `RECIPE_DEF_MAP`, `fetchRecipeDefs`, `initRecipeDefs`
- [x] 3.2 `GameState.getShopCatalogSnapshot()` — recipe entries resolve off `ItemDef.crafting`; recipe icon keys off `item.tier` (was `RecipeDef.Rarity`)
- [x] 3.3 `GameState.getCraftCatalogSnapshot()` — same
- [x] 3.4 **Fix the price bug**: `getBuildingActions` recipe-purchase branch charges `recipeCostGold`, not the craft cost (it currently shows `recipe.costGold` — the button lies about the price)
- [x] 3.5 `ItemEditorPanel.vue` — the Crafting section now reads/writes the item's own `crafting` block; drop `recipesByOutput`
- [x] 3.6 Client suite green, with a test pinning the three prices apart (buy / craft / learn) on one item

## 4. The unified list (server)

- [x] 4.1 Add `ListDef {id, name, items[]}` + `catalog/lists/` embed, loader, and validation (≥1 member, every member a known item)
- [x] 4.2 Runtime overlay + `SaveListDef` / `DeleteListOverride` + **`LoadPersistedListsIntoOverlay()` wired into `cmd/api/main.go`** (lists have never had a loader — without this an authored list dies on restart)
- [x] 4.3 Migrate the 3 existing lists into `catalog/lists/` (`marketplace`, `wandering_merchant`, `druid_recipes_1`); delete `ItemListDef`, `RecipeListDef`, and their catalog dirs
- [x] 4.4 `GET /catalog/lists` replaces `/catalog/item-lists` + `/catalog/recipe-lists`
- [x] 4.5 `POST /lists` + `DELETE /lists/{id}` write routes (none exist today) with validation errors mapped to 400
- [x] 4.6 `go test ./...` green

## 5. Wire the consumers to lists (server)

- [x] 5.1 Migrate the maps: `forest-1.json:557` `"itemList": "wandering_merchant"` → `"list": "wandering_merchant"`. This is the **only** occurrence in the repo; no map uses `recipeList` at all
- [x] 5.2 `listIDForBuilding(b)` helper: reads metadata `list` and **nothing else** — no aliases, no fallbacks
- [x] 5.3 Map load validation: a building carrying `itemList` or `recipeList` is a **load error** naming the map, the building, and the key to rename. Never silently ignored — a dropped key would erase a shop's stock config with no signal
- [x] 5.4 Shop stocking — slot the unified list into `populateShopInventoryForBuildingLocked`'s precedence ladder without disturbing `shopFixedInventory` / neutral sampling / `shopLootTableId`
- [x] 5.5 Recipe Shop — pool = the bound list's **craftable** members (non-craftable silently skipped), else all craftable items
- [x] 5.6 Crafting building — offered set = `learned ∩ (list if bound)`; no list bound = everything learned (today's behavior)
- [x] 5.7 Camp loot — `NeutralGroup.LootList` alongside `LootTable`; uniform pick on `rngLoot`, always drops; **naming both is a load-time error**
- [x] 5.8 Tests: precedence ladder pinned per branch, asserted against the real migrated `forest-1.json` (not a fixture) so the shipped map's stock is covered; a map with a legacy key fails to load with the rename message; mixed-list Recipe Shop skips non-craftables; list-scoped Artificer intersects with learned; camp list drops uniformly and deterministically under a seed

## 6. The Lists tab (client)

- [x] 6.1 Extract `components/editor/EditorTabs.vue` from the `role="tablist"` pattern in `MatchMenu.vue` (the editors have no tab pattern today; make it shared so unit/building editors can adopt it)
- [x] 6.2 Add `listDefs.ts` (type + store + `fetchLists`) and `listEditorApi.ts` (save/delete)
- [x] 6.3 `ListEditorPanel.vue` — `EditorSidebar` of lists, name field, member `RepeatableList`, `ValidationChecklist`
- [x] 6.4 Non-blocking warning when members are not craftable, phrased as consequence: "3 of 5 items are not craftable — a Recipe Shop or crafting building will ignore them"
- [x] 6.5 Wrap `ItemEditorPanel` + `ListEditorPanel` in the Items | Lists tabs
- [x] 6.6 Map editor (`MapEditorPanel.vue` + `WorldEditorPanel.vue`): collapse the separate Item List / Recipe List selectors into one **List** selector writing the `list` key. Both panels currently carry a near-identical copy of these controls — neither may keep writing a legacy key
- [x] 6.7 Tests: create → save → appears in the map editor dropdown; empty list refused; warning shows but does not block a save

## 7. Verify

- [x] 7.1 Full `go test ./...`, `vue-tsc -b`, `vitest run` — all green (Go suite, 391 client tests / 66 files, typecheck clean)
- [ ] 7.2 **Still open — manual.** Drive it in the running app: author a list, bind it to a Recipe Shop and an Artificer, buy the recipe, craft the item, confirm the three prices are each charged once and only where they belong
- [x] 7.3 Load the migrated `forest-1.json` and confirm its shop stocks exactly what it stocked before the change (same items, same quantities) — the map's key changed, its behavior must not have
- [x] 7.4 Camp list loot: covered by automated tests (`TestListLootDrop_UniformAndAlwaysDrops` — always drops, one item, no resources, every member appears across 60 seeds; `TestListLootDrop_Deterministic` — same seed, same drop). These call the drop path directly rather than clearing a camp in a live match, so the full camp-wipe → chest → pickup flow is still only exercised by the existing weighted-loot tests
- [x] 7.5 Grep the whole repo for `itemList` / `recipeList` and confirm zero remaining references outside this change's own docs
