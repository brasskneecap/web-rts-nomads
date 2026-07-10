# Item Editor — Design

**Date:** 2026-07-09
**Branch:** `ui-item-editor`
**Status:** Approved
**Depends on:** proc-effects system (`catalog/procs`, `ItemOnHitProc` reference schema), dodge/block modifiers, recipe/crafting system — all in this branch's history.

## Problem

New equipment and equipment changes currently require hand-editing embedded
catalog JSON (items, recipes, item lists, loot tables) plus dropping icon
PNGs into the client source tree — a code-change-and-rebuild loop. We want a
Map-Editor-style in-app tool: create and modify equipment (icon, stats,
elemental/proc powers, cost, crafting recipe, shop availability) from a UI,
with edits persisting like map-editor saves do.

The Map Editor is the architectural template throughout: `/maps` POST +
writable-dir overlay (`MAP_CATALOG_DIR`, `LoadPersistedMapsIntoOverlay`,
overlay-wins readers, live registration, per-file panic recovery) on the
server; MainMenu plank → route shell → panel component → service module on
the client.

## Decisions (made during brainstorming)

1. **Icons: pick-from-existing + upload.** The editor offers a gallery of
   every bundled icon (including `assets/misc/**`) AND a PNG upload path.
   Uploads are stored server-side and served over HTTP, finishing the
   orphaned `itemCatalogImages.ts` stub (`/catalog/items/{id}/image`).
   Client resolution order: bundled glob first, server URL fallback.
2. **Availability = four surfaces:** Player marketplace
   (`items/lists/marketplace.json` membership), Wandering merchant
   (`items/lists/wandering_merchant.json` membership), Neutral merchant
   rolled stock (`merchant_basic` loot-table entry, with weight), Recipe
   shop (automatic when the item has a recipe; optional membership in the
   curated `druid_recipes_1` recipe list). Per-map `shopFixedInventory` is
   explicitly out of scope — that is a Map Editor concern.
3. **All three power layers editable:** stat modifiers (hp, damage, armor,
   attackSpeed, moveSpeed, healthRegen, maxShield, dodgeChance,
   blockChance), on-hit elemental rows, and proc triggers (`onHitProc` /
   `onStruckProc`: effect reference from `catalog/procs` + chance +
   overrides). Proc effect DEFINITIONS are referenced, never authored here
   (a future Effect Editor may do that).
4. **Equipment only.** Consumables are excluded this branch (different
   effect form shape; add a tab when needed).
5. **Persistence: writable overlay dirs mirroring maps.** In dev the
   writable dir is the embedded source dir itself, so editor saves are
   ordinary git-visible JSON edits. Packaged builds write beside the app
   and overlay the embed.
6. **No auth**, same as the Map Editor. Server-side validation is the gate.

## Architecture

Two implementation plans, one spec:

- **Plan A — server persistence + API.** Independently testable via curl.
- **Plan B — client UI.** Consumes Plan A.

### Plan A: server

#### A1. Writable overlays (pattern: `maps.go:487-724`)

| Catalog | Env override | Dev default dir |
|---|---|---|
| Items | `ITEM_CATALOG_DIR` | `<cwd>/internal/game/catalog/items` |
| Recipes | `RECIPE_CATALOG_DIR` | `<cwd>/internal/game/catalog/recipes` |

- `LoadPersistedItemsIntoOverlay()` / `LoadPersistedRecipesIntoOverlay()`
  called once at startup (wired in `cmd/api/main.go` next to the maps
  call): walk the writable dir, hydrate + validate each file with the SAME
  validators the embed loaders use (`validateItemDef`,
  `validateRecipeDef`), recover per-file panics into logged skips (one bad
  file never crashes startup).
- Overlay precedence: runtime/persisted wins over embed, implemented in the
  readers (`getItemDef`, `ListItemDefs`, `getRecipeDef`, `ListRecipeDefs`,
  `getItemListDef`, `ListItemListDefs`, `getRecipeListDef`,
  `getLootTableDef`) exactly as `GetMapConfigByID`/`ListMapCatalogSummaries`
  do for maps. Overlay maps guarded by a mutex (they mutate at runtime,
  unlike the init-only embed maps).
- List/loot-table files (`items/lists/marketplace.json`,
  `items/lists/wandering_merchant.json`, `recipes/lists/druid_recipes_1.json`,
  `neutral_groups/loot_tables.json`) are saved WHOLE when availability
  changes, into the same writable dirs, and overlaid the same way.
- Tier→subdirectory convention preserved on write: items save to
  `<dir>/<category-path>/<tier>/<id>.json` matching the embed layout
  (weapons/armor/accessories/shields/consumables by `category`, lists
  under `lists/`). Editor-created categories default under `misc/<tier>/`.
- Save registers the def into the overlay under lock immediately (live
  without restart). Running matches keep equip-time-resolved
  `EquipmentBonus` snapshots — unchanged semantics; new equips/matches see
  the new def.

#### A2. HTTP API (pattern: `/maps` POST at `router.go:102-148`)

- `POST /items` — body:
  ```json
  {
    "item": { ...full ItemDef wire shape... },
    "recipe": { "inputs": ["steel_shield","fire_ring"], "costGold": 150 } | null,
    "availability": {
      "marketplace": true,
      "wanderingMerchant": false,
      "lootTable": { "enabled": true, "weight": 1 },
      "recipeList": true
    }
  }
  ```
  Validates item (+ recipe when present) with the load-time validators; 400
  with `{"error":"validation_failed","message":"<detail>"}` on failure (the
  load-time validators produce one message, not per-field errors; the editor
  UI surfaces the message beside the Save button). On success: writes item
  JSON (+ recipe JSON named `<itemID>.json` with `output = itemID`),
  updates the four availability files (add/remove membership; loot-table
  entry with weight), registers everything live, returns 201
  `{id, status:"saved"}`.
- `DELETE /items/{id}` — removes the writable override + its recipe +
  its availability memberships that the override introduced. For embedded
  items this is "reset to default" (embed remains); for editor-created
  items it is a true delete. 404 when no override exists.
- `POST /items/{id}/image` — multipart or raw PNG body; validates content
  type + size (≤ 256 KB) + decodes as PNG; stores at
  `<items dir>/_icons/<id>.png` (`_icons/` is skipped by the item-def
  walk, like `lists/`). ALWAYS sets the item override's `iconKey` to the
  item id, so the id↔icon-URL mapping is unambiguous (picking a bundled
  gallery icon instead sets `iconKey` to that bundled key). 201 on
  success.
- `GET /catalog/items/{id}/image` — serves the stored PNG
  (`image/png`, 404 when absent). This is the exact URL the orphaned
  client stub already requests.
- `GET /catalog/procs` — read-only list of `ProcEffectDef`s (sorted by
  id), for the editor's effect dropdown. Mirrors `/catalog/items`.
- Vite dev proxy gains `/items` (vite.config.ts proxy list).

#### A3. Validation additions

- Item id: `^[a-z0-9_]+$`, non-empty. An id matching an embedded item is
  an OVERRIDE of that item (intentional and supported); an id matching an
  existing overlay item updates it.
- Recipe inputs must reference existing item ids (embed or overlay);
  self-referencing recipes rejected.
- Availability updates are idempotent (adding an existing member is a
  no-op; removing a non-member is a no-op).

### Plan B: client

#### B1. Menu + route

- `MainMenu.vue` `DEFAULTS.entries` gains
  `{ label: 'Item Editor', to: '/item-editor' }` between Map Editor and
  Settings; the five entries' `top` percentages re-space evenly across the
  sign region currently spanning 55.97→75.25.
- Router: `{ path: '/item-editor', component: ItemEditor }`.
- `ItemEditor.vue` — shell identical in shape to `Editor.vue`: exit button
  (`router.push('/')`) + `<ItemEditorPanel />`.

#### B2. `ItemEditorPanel.vue`

- **Left sidebar:** searchable, category/tier-grouped list of all items
  (from `/catalog/items`), icons rendered; a "New Item" button seeds a
  blank equipment def; edited-vs-shipped badges (an item with a writable
  override shows a dot; server exposes this via an `overridden: true`
  field on `ListItemDefs` wire output).
- **Form sections (right pane):**
  1. *Identity* — id (locked once saved), displayName, description, tier
     (dropdown), category, slotKind, allowedUnitTypes.
  2. *Icon* — current icon preview; gallery picker over every bundled icon
     key (from the client glob) + upload control (POSTs to
     `/items/{id}/image`); uploaded icons preview via the server URL.
  3. *Stats* — numeric inputs for the 9 modifiers; dodge/block as
     percentages in the UI, stored as fractions.
  4. *Elemental* — repeatable rows {damage type dropdown, amount}.
  5. *Procs* — two identical sub-forms (On hit / When struck): enable
     toggle, effect dropdown (`/catalog/procs`), chance (percent), and
     collapsible override fields (damage, projectile scale, bounce
     count/range/falloff, slow, burn) with placeholder text showing the
     effect's base values.
  6. *Cost & Availability* — costGold; checkboxes Player marketplace /
     Wandering merchant; Neutral merchant toggle + weight; "Add recipe to
     curated recipe list" checkbox (enabled only when crafting is on).
  7. *Crafting* — toggle; when on: two input-item pickers (searchable,
     from the catalog), craft gold cost. Saving maintains the recipe.
- **Actions:** Save (POST /items, inline field errors from the 400
  payload), Reset to default (DELETE /items/{id}, embedded items only
  show this when overridden; editor-created items show Delete).
- **Service module:** `game/items/itemEditorApi.ts` — typed wrappers for
  the new endpoints, mirroring `game/maps/catalog.ts` conventions
  (API_BASE, error classes).
- **Icon fallback wiring:** `itemAssets.ts` gains a server-URL fallback:
  when `getItemAssetImage(iconKey)` misses the bundle, it lazily loads
  `${API_BASE}/catalog/items/${iconKey}/image` and caches the result
  (absorbing the orphaned `itemCatalogImages.ts`, which is deleted).

#### B3. Custom-cursor rule

Per repo rules: no literal `cursor:` declarations in component CSS except
`not-allowed` for forbidden states. The gallery picker and pickers use the
global interactive-element rules.

## Error handling

- Server validation failures → 400 with field-keyed errors; the form
  highlights fields inline. Network failures → toast + preserved form
  state (no data loss on failed save).
- Icon upload rejects non-PNG / oversized files client-side first,
  server-side authoritatively.
- Startup overlay loading logs-and-skips corrupt files (maps precedent).
- Concurrent editor instances: last-write-wins (same as maps; no locking).

## Testing

- **Go:** overlay load precedence + reset-to-default; save round-trip
  (item + recipe + all four availability surfaces); validator rejections
  (bad id, unknown effect, self-recipe, bad damage type); icon upload
  (PNG sniffing, size cap, serve round-trip, `_icons/` excluded from def
  walk); live registration (save then `getItemDef` under lock sees it);
  one-bad-file startup resilience.
- **Client (vitest):** itemEditorApi request/response mapping; form↔wire
  transforms (percent↔fraction, overrides omitted when zero); icon
  fallback ordering (bundle hit never fetches; miss fetches once and
  caches); MainMenu renders five entries in order.
- **Manual:** create an item end-to-end in the running app (upload icon,
  set stats + proc, mark marketplace + crafted), start a match, buy/craft
  it, verify tooltip/icon/proc in-game.

## Out of scope

- Consumable editing, proc-effect (catalog/procs) authoring, per-map
  `shopFixedInventory`, auth/roles, editing units/perks/abilities, loot
  tables other than `merchant_basic`, deleting embedded items, multi-user
  edit locking.
