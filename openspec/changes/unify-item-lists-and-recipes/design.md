## Context

Four entities in the item economy overlap:

| Entity | Shape | Authorable? |
|---|---|---|
| `RecipeDef` | `{id, name, inputs[], costGold, unlockCostGold, output, starter, rarity}` | via the item editor's Crafting section |
| `ItemListDef` | `{id, name, items[]}` | **no** |
| `RecipeListDef` | `{id, name, recipes[]}` | **no** |
| loot `packagedItem` | d100-ranged entries, or a gold/wood bundle | **no** |

`RecipeDef` is already a 1:1 shadow of its output item. All seven shipped recipes have `id == output`, every output is unique, and `SaveEditorItem` — the only production writer — hardcodes `ID: item.ID, Output: item.ID` ([item_editor.go:96-104](../../../server/internal/game/item_editor.go#L96-L104)). `Name` duplicates `DisplayName`; `Rarity` is derived from the output item's tier. The def carries no information the item doesn't already have or couldn't hold.

The two list types are structurally identical and both are *write-dead*: `SaveItemListDef` and `SaveRecipeListDef` exist and work, but no HTTP route or UI calls them ([recipe_persistence.go:200-245](../../../server/internal/game/recipe_persistence.go#L200-L245)). Consequently `druid_recipes_1.json` is orphaned, no shipped map sets `recipeList`, and neither list type has a startup overlay loader — so even if something *did* write one, it would vanish on restart.

Loot is the odd one out and stays that way: it is a genuinely richer model (weighted d100 ranges, no-drop gaps, gold/wood resource bundles) and only neutral **camps** drop anything ([state_loot_drops.go:233](../../../server/internal/game/state_loot_drops.go#L233)) — units and buildings never do.

## Goals / Non-Goals

**Goals:**
- One authorable grouping primitive (`ListDef`) that four consumers read in their own terms.
- Craftability becomes a property of an item, not a parallel entity keyed by the same id.
- Designers can answer "what drops here / what does this sell / what can this forge make" entirely in the editor.
- Zero gameplay regression: the same items are craftable, at the same prices, learned the same way.

**Non-Goals:**
- Replacing the weighted loot system. Lists are the *uniform* loot case; `loot_tables.json` keeps weights, no-drop gaps, and resource bundles.
- Making units or buildings drop loot. Camps remain the only drop source.
- Multiple recipes producing one item, or a recipe whose id differs from its output. Neither exists, neither is reachable today, and the merge deliberately forecloses both.
- A shop/loot-table editor. This change makes *lists* authorable and bindable; the weighted loot tables stay JSON-authored.

## Decisions

### D1 — Crafting lives on `ItemDef` as an optional block; ≥2 inputs *is* craftability

```go
type ItemCrafting struct {
    Inputs         []string `json:"inputs"`                    // >= 2, consumed per craft
    CraftCostGold  int      `json:"craftCostGold"`             // charged at the Artificer, per craft
    RecipeCostGold int      `json:"recipeCostGold"`            // charged once at a Recipe Shop, to learn
    Starter        bool     `json:"starter,omitempty"`         // pre-learned by every player
}
type ItemDef struct {
    // ...
    CostGold int           `json:"costGold"`                   // buy the finished item outright
    Crafting *ItemCrafting `json:"crafting,omitempty"`         // nil => not craftable
}
```

`def.Crafting != nil` is the single craftability predicate (`ItemDef.IsCraftable()`), replacing "a `RecipeDef` whose output is this item exists". The ≥2-inputs rule carries over from `validateRecipeDef` as a validation on the block.

*Alternative considered:* keep the fields flat on `ItemDef` (`craftInputs`, `craftCostGold`, …). Rejected — a nil-able block makes "not craftable" one check instead of four, and the editor's save request already speaks `crafting` as a block ([item_editor.go](../../../server/internal/game/item_editor.go)), so the wire shape is already this.

Note this is *not* a re-introduction of the `IsRecipe`/`RecipeCost` mirror fields that were just deleted. Those were a **duplicate** of a `RecipeDef` that remained the source of truth. Here the item becomes the *only* home; there is nothing left to drift from.

### D2 — One untyped `ListDef`; the consuming building supplies the meaning

```go
type ListDef struct {
    ID    string   `json:"id"`
    Name  string   `json:"name"`
    Items []string `json:"items"`   // item IDs; every one must resolve
}
```
Lives at `catalog/lists/<id>.json`. Replaces both `ItemListDef` and `RecipeListDef`.

A list carries no `kind`. Meaning is assigned at the point of consumption:

| Consumer | Capability | Reads a list as | Charges |
|---|---|---|---|
| Shop | `item-purchase` | items on the shelf | `item.CostGold` |
| Recipe Shop | `recipe-purchase` | recipes for sale | `item.Crafting.RecipeCostGold` |
| Artificer | `crafting` | craftable scope ∩ learned | `item.Crafting.CraftCostGold` |
| Camp | (neutral group) | uniform drop pool | free |

*Alternative considered:* a typed list (`kind: shop | recipe | loot`) validated at author time. Rejected per the user's decision — typing forecloses reusing one list in two roles (an "elemental gear" list that a Marketplace sells, a Forge crafts, and a camp drops), and the runtime filter is cheap. The safety a type would have bought is recovered as an **editor warning** (D7).

### D3 — Crafting consumers filter non-craftable members; they never error

A Recipe Shop or Artificer given a list containing `broad_sword` (not craftable) silently skips it. This is what makes untyped lists safe: a list is always *usable*, just possibly empty for a given consumer. An empty resolved set means the building offers nothing — which is already a representable state.

### D4 — One binding key, `list`. No aliases. The maps are migrated, and the old keys become a load error.

Buildings bind a list via map metadata `list`, and **that is the only key any consumer reads**. `itemList` and `recipeList` are not aliased, not fallen back to, and not silently tolerated.

This is affordable because the legacy keys are almost unused: a repo-wide search finds **exactly one** occurrence — `"itemList": "wandering_merchant"` at [forest-1.json:557](../../../server/internal/game/catalog/maps/forest-1.json#L557) — and **zero** maps set `recipeList` anywhere. So the migration is a one-line edit to one shipped map, and an alias would be permanent complexity bought to serve a single line.

The danger is not the shipped map, it's an *unshipped* one: maps are editable at runtime and land in the `MAP_CATALOG_DIR` overlay, so a locally-authored map could still carry `itemList`. Dropping that key silently would wipe that shop's stock configuration with no signal — the shop would just quietly fall back to its building-type default and nobody would notice until a playtest.

So the old keys get a **load-time validation error**, not an alias:

> `forest-1: building shop-2 uses metadata key "itemList", which was replaced by "list". Rename the key.`

Map overlay loading already recovers per-file panics into logged skips, so a stale map surfaces as a loud warning and a skipped map rather than a crash or a silent misconfiguration. The error is a migration aid with a shelf life — once no map trips it, it can be deleted.

The existing consumer-side distinction survives: a **marketplace** takes its list verbatim, a **neutral shop** treats it as a *sampled pool* ([state_shop.go:268-281](../../../server/internal/game/state_shop.go#L268-L281)). That is a property of the building, not the list, and is unchanged.

### D5 — A camp names **either** a weighted loot table **or** a list; never both

`NeutralGroup` gains `LootList string` alongside the existing `LootTable string`. Setting both is a **load-time validation error**, not a precedence rule — a silent winner between two loot sources is exactly the kind of thing that gets mis-authored and never noticed.

A list as a loot source is deliberately the *simple* case, and its simplicity is the point:
- uniform odds across members,
- always drops (no gap = no "nothing" outcome),
- items only (no gold/wood — resource bundles remain the weighted system's job).

It rolls on `s.rngLoot` like everything else, preserving seeded determinism.

### D6 — The learn/unlock mechanic survives; a list *scopes* an Artificer, it does not grant

Artificer offers `learned ∩ (list, if bound)`. No list bound → `learned` (today's behavior exactly). This keeps recipe-hunting as progression while letting a building be thematically narrow (a Dwarven Forge that only makes weapons).

*Alternative considered:* the list IS the craftable set, deleting learning entirely. Rejected by the user — it would delete Recipe Shops, recipe cost, and the progression loop along with it.

### D7 — Authoring safety comes from editor warnings, not runtime errors

Because lists are untyped, the editor is where mistakes get caught. The Lists tab surfaces, per list, a non-blocking warning when a member is not craftable — phrased as consequence, not rule: *"3 of 5 items are not craftable — a Recipe Shop or Artificer will ignore them."* It never blocks the save, because that same list may be perfectly correct as shop stock or a loot pool.

### D8 — The wire keeps its shape; the field names stop lying

Every `recipeId` crossing the wire today *is already an item id*. So:
- `purchase_recipe` and `craft_item` **message types keep their names** — the player action is unchanged; you still buy a recipe and still craft an item. Their payload field `recipeId` becomes `itemId`.
- `RecipeStockEntry.recipeId` → `itemId`.
- `PlayerSnapshot.unlockedRecipeIds` → `unlockedCraftableIds`.

**No value changes anywhere** — only key names. This is why the merge is cheap.

### D9 — Profile: key rename, schema bump, values untouched

`PlayerProfile.KnownRecipeIDs` → `KnownCraftableIDs`, with a v8→v9 migration that moves the array across ([profile/store.go:154-157](../../../server/internal/profile/store.go#L154-L157) is the v7→v8 precedent). The IDs inside are already item IDs, so the migration is a rename, not a remap. A player who has learned `fire_sword` still knows `fire_sword`.

### D10 — Lists get the overlay + startup loader that they never had

`catalog/lists/` gets the same three-layer treatment items already have: embedded defaults, a writable overlay dir (`LIST_CATALOG_DIR`), and **`LoadPersistedListsIntoOverlay()` wired into `cmd/api/main.go`**. Today's list save functions have no loader, so an authored list would not survive a restart. This change is what makes lists actually authorable, so the loader is load-bearing, not a nicety.

### D11 — The editor gets its first tabs, as a shared component

The item editor becomes **Items | Lists**. Editors currently have no tab pattern (they compose `EditorShell` = sidebar | main). Rather than bolt a bespoke tablist onto `ItemEditorPanel.vue`, lift the accessible `role="tablist"` pattern already proven in [MatchMenu.vue:22-46](../../../client/src/game-portal/src/components/MatchMenu.vue#L22-L46) into a shared `components/editor/EditorTabs.vue`, so the unit and building editors can adopt it later.

The Lists panel reuses the existing editor furniture — `EditorSidebar` (list of lists), `SectionCard`, `RepeatableList` (members, same control the crafting ingredients already use), `ValidationChecklist` (warnings).

### D12 — Recipe icons key off the item's tier

The Recipe Shop renders `${rarity}_recipe` icons from `RecipeDef.Rarity`, which was path-derived from the recipe's `catalog/recipes/<tier>/` folder — and that folder was chosen from the *output item's tier* on save. So `RecipeDef.Rarity` was always just `ItemDef.Tier` laundered through a directory name. It becomes a direct read of `item.Tier`, and `catalog/recipes/<tier>/` folders disappear with no visual change.

## Risks / Trade-offs

- **Author-created recipe overlay files are stranded by the deletion.** Any craftable item an author made through the editor has its crafting data in a `catalog/recipes/**/<id>.json` overlay file, which this change stops reading. → Boot-time one-shot migration: fold any leftover `catalog/recipes/**` file into the matching item overlay's `crafting` block, log each fold, then leave the file in place (harmless, ignored). The seven shipped recipes are hand-folded in the change itself.

- **Untyped lists let you bind a nonsense list to a building** (a list of potions on an Artificer → it can craft nothing). → Accepted deliberately; recovered as an editor warning (D7) and a visibly empty craft menu. The alternative (typed lists) costs more than it saves.

- **Profile migration touches saved player data.** A bug here loses players' learned recipes. → The migration is a pure key rename with no value transformation, gets its own test asserting a v8 profile's IDs survive verbatim into v9, and `KnownCraftableIDs` is additive-safe (an unknown id is ignored at join, not fatal).

- **`purchase_recipe`/`craft_item` payload key rename is a hard client/server version break.** → Not mitigated, and does not need to be: this is a single-binary game where the SPA ships with the server. There is no mixed-version deployment to support. Worth stating explicitly so nobody adds a compat shim "just in case."

- **`state_shop.go`'s stocking chain is a 5-way precedence ladder** and the unified `list` key slots into the middle of it. A mistake here silently changes what every shop stocks. → The precedence order is pinned by a test per branch (fixed inventory > neutral sampling > loot table > list > building-type default), and the shipped `forest-1.json` is asserted to stock exactly what it stocked before its key was migrated — a real map, not a fixture.

- **Dropping the metadata aliases could silently erase a locally-authored map's shop config.** Maps are editable at runtime and land in the `MAP_CATALOG_DIR` overlay, so a map outside the repo could still carry `itemList`. → The legacy keys are a *load error*, not an ignored key (D4). A stale map surfaces as a loud, named warning and a skipped map — never as a shop that quietly falls back to its default and gets noticed three playtests later.

- **Scope.** This lands catalog, protocol, profile, simulation, and two editors in one change. → The sequencing in `tasks.md` keeps the server green at each step: the item-side merge lands and passes *before* lists are introduced, and lists land *before* any consumer is rewired.
