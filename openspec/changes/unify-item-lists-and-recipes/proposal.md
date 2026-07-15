## Why

The item economy has three overlapping grouping concepts that all mean "a named set of item IDs" — `ItemListDef`, `RecipeListDef`, and (loosely) loot subtables — and a fourth entity, `RecipeDef`, that is already a strict 1:1 duplicate of the item it produces. Nothing can author any of them: item lists and recipe lists have working Go save functions that no HTTP route or UI ever calls, so `druid_recipes_1.json` sits orphaned in the catalog and every shop's stock is effectively hardcoded.

The result is that a designer cannot answer "what can drop off this camp / what does this shop sell / what can this forge make" without editing JSON by hand, and the same conceptual grouping has to be re-authored once per consumer. Collapsing recipes into items and unifying the groupings into one authorable **List** makes all four questions the same question.

## What Changes

- **BREAKING** `RecipeDef` is deleted. Craftability moves onto `ItemDef` as a crafting block (`inputs`, `craftCostGold`, `recipeCostGold`, `starter`). An item is craftable iff it declares ≥2 inputs. `catalog/recipes/` and `recipes.go` are removed; the seven shipped recipes fold into their item defs.
  - This is a *strict narrowing*, not a risky migration: every shipped recipe already has `id == output`, every output is unique, and `SaveEditorItem` — the only production writer of recipes — hardcodes `ID: item.ID, Output: item.ID`.
- **BREAKING** `ItemListDef` and `RecipeListDef` merge into a single untyped `ListDef` (`{id, name, items[]}`) under `catalog/lists/`. A list is just a named set of item IDs; **the consuming building decides what it means**.
- Lists become authorable: new `POST`/`DELETE` routes and a **Lists tab** in the item editor (the editor's first tabbed surface: Items | Lists).
- Each consumer interprets a list in its own terms:
  - **Shop** (`item-purchase`) — sells the members at their item cost.
  - **Recipe Shop** (`recipe-purchase`) — sells the *recipe* for each craftable member at its recipe cost.
  - **Artificer** (`crafting`) — crafts members at their craft cost, **intersected with what the player has learned**. No list = everything the player knows (today's behavior).
  - **Camp loot** — drops one member, uniform odds, always drops.
- The learn/unlock mechanic **survives**. A list on an Artificer scopes a building (a Dwarven Forge that only makes weapons); it does not grant recipes.
- The existing weighted loot system (`loot_tables.json`: d100 ranges, no-drop gaps, gold/wood resource bundles) **survives unchanged**. A camp selects *either* a weighted loot table *or* a list. A list is the simple uniform case; nothing is lost.
- Non-craftable members of a list bound to a crafting building are silently ignored at runtime, and flagged as a warning in the editor.
- Fixes a live bug in passing: the recipe-shop action bar prices recipes with `recipe.costGold` (the *craft* cost) while the server charges `unlockCostGold` — so the button lies about the price.

## Capabilities

### New Capabilities
- `item-crafting`: Craftability as a property of an item (inputs, craft cost, recipe cost, starter), the learn/unlock mechanic, and how Artificers and Recipe Shops resolve what they offer.
- `item-lists`: The `ListDef` entity — a named, untyped set of item IDs — and the per-consumer semantics that give a list meaning (shop stock, recipe stock, craftable scope, loot pool).
- `item-catalog-editor`: The authoring surface — the tabbed Items | Lists editor, list CRUD, and the validation/warnings that keep authored data honest.

### Modified Capabilities
<!-- None. No existing spec in openspec/specs/ covers items, shops, crafting, or loot;
     the eleven current specs are all unit/ability/AI/pathing capabilities. -->

## Impact

**Deleted**
- `server/internal/game/recipes.go`, `catalog/recipes/**` (7 recipe defs + 1 recipe list)
- `RecipeDef`, `RecipeListDef`, `SaveRecipeDef`, `DeleteRecipeOverride`, `SaveRecipeListDef`, `starterRecipeIDs`
- `client/src/game-portal/src/game/maps/recipeDefs.ts`

**Server**
- `items.go` — `ItemDef` gains the crafting block; `ItemListDef` → `ListDef` moves to `catalog/lists/`
- `state_crafting.go`, `state_recipe_shop.go`, `state_recipe_purchase.go` — resolve against items + the building's list
- `state_shop.go` — the 5-way stocking precedence chain must absorb the unified list key without breaking `shopFixedInventory` / `shopLootTableId` / neutral-shop sampling
- `state_loot_drops.go`, `neutral_group_defs.go` — a camp may name a list instead of a loot table
- `item_editor.go` — save request already carries a `crafting` block; it now writes the item only
- `internal/http/router.go`, `editor_handlers.go` — new list write routes
- `pkg/protocol/messages.go` — `unlockedRecipeIds` → `unlockedCraftableIds`; `RecipeStockEntry.recipeId` → item id (values unchanged — every recipe id already *is* an item id)
- `internal/profile/` — `KnownRecipeIDs` → `KnownCraftableIDs`, schema bump (key rename only; values are untouched)

**Client**
- `ItemEditorPanel.vue` — first tabbed editor (Items | Lists); crafting section unchanged
- New list editor panel + `listDefs.ts`
- `GameState.ts` — shop/craft/action-bar snapshots resolve crafting off `ItemDef`; recipe-shop price bug fixed
- `MapEditorPanel.vue` / `WorldEditorPanel.vue` — the separate Item List / Recipe List selectors collapse into one **List** selector writing a single `list` key

**Data migration**
- Item defs absorb their recipe's crafting block; `catalog/lists/` gains the three existing lists. Player profiles keep their values under a renamed key.
- **BREAKING** The building metadata keys `itemList` and `recipeList` are replaced by `list`, with **no alias**. The maps are migrated instead: repo-wide there is exactly one occurrence (`forest-1.json:557`) and no map uses `recipeList` at all, so an alias would be permanent complexity bought for a single line. A map still carrying a legacy key **fails to load with a rename message** rather than being silently ignored — quietly dropping the key would erase that shop's stock configuration with no signal.
