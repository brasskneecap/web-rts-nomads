# Craft Tab in the Match Menu — Design

**Date:** 2026-07-01
**Status:** Approved (brainstorming) — ready for implementation plan

## Summary

Add a fourth tab, **Craft**, to the in-match menu (`MatchMenu.vue`), alongside
Shop / Upgrades / Vault. It lets a player craft an unlocked recipe at their
Artificer from the menu, instead of only via the Artificer building's
selection action bar. Client-only: the server's existing `craft_item` command
already validates and executes the craft.

## Goals

- A "Craft" tab, always visible (like Upgrades), listing the player's **unlocked
  recipes** with ingredient have/need and a Craft button.
- Craft from the menu with no need to select the Artificer building.
- Clear gated states: no Artificer → hint; no unlocked recipes → hint.

## Non-Goals

- Showing locked / not-yet-unlocked recipes (only unlocked ones are listed).
- Any server change — `craft_item` already exists and is authoritative.
- Client-side gold gating (server rejects an unaffordable craft; gold is shown,
  matching the Shop tab and the Artificer action bar).

## Key Decisions (from brainstorming)

| Decision | Choice |
|---|---|
| Tab contents | Only recipes in `localPlayerUnlockedRecipeIds`. |
| Tab visibility | Always visible; content gated by Artificer ownership. |
| Tab label | "Craft". |
| Craft enablement | `craftable = owns a built Artificer AND all ingredients in Vault`. |
| Gold | Displayed, not client-gated. |

## Architecture

A new read-only `GameState.getCraftCatalogSnapshot()` (sibling of the existing
`getShopCatalogSnapshot()`) produces the tab's data each frame from client
state the server already sends (unlocked recipe ids, the recipe catalog, the
Vault, and the building list). `MatchMenu.vue` renders it and emits a `craft`
event that flows to the existing `GameClient.sendCraftItem(recipeId)`.

### Data model (new types on `GameState.ts`)

```ts
export interface CraftCatalogIngredient { itemId: string; have: number; need: number }
export interface CraftCatalogEntry {
  recipeId: string
  name: string
  output: string
  costGold: number
  ingredients: CraftCatalogIngredient[]
  craftable: boolean
}
```

### GameState methods

- `localPlayerHasArtificer(): boolean` — true if `mapConfig.buildings` contains a
  building owned by `localPlayerId` whose `capabilities` include `crafting` and
  that is not under construction (`metadata.underConstruction !== true`). Mirrors
  the existing ownership/`item-purchase` checks.
- `getCraftCatalogSnapshot(): CraftCatalogEntry[]` — for each id in
  `localPlayerUnlockedRecipeIds`, resolve `RECIPE_DEF_MAP`; skip unknown ids.
  Compute `have` per input item from `localPlayerVault` (sum of `stacks`), `need`
  as the count of that input in `recipe.inputs` (duplicates aggregated). Sort the
  entries deterministically (by display name, then id). `craftable =
  localPlayerHasArtificer() && every ingredient have ≥ need`. This reuses the
  same have/need logic already implemented for the Artificer action bar in
  `getBuildingActions` (Plan 3).

### Snapshot plumbing

The UI snapshot built in `GameClient` (the object with `shopCatalog`,
`shopRerollsRemaining`, `vault`, …) gains:
- `craftCatalog: this.state.getCraftCatalogSnapshot()`
- `hasArtificer: this.state.localPlayerHasArtificer()`

### `MatchMenu.vue`

- `TABS` gains `{ id: 'craft', label: 'Craft' }` (there are already 4 tab slots;
  only 3 were used).
- New props: `craftCatalog?: CraftCatalogEntry[]`, `hasArtificer?: boolean`.
- New emit: `craft: [recipeId: string]`.
- Craft tab body:
  - `!hasArtificer` → hint: "Build an Artificer to craft." (no rows)
  - `hasArtificer && craftCatalog.length === 0` → hint: "Buy a recipe at a Recipe
    Shop to craft it here."
  - else → one row/card per entry: the output item icon + `name`, the ingredient
    list rendered as `itemId have/need` (the have/need pair styled red when
    `have < need`), the gold cost, and a **Craft** button `:disabled="!entry.craftable"`
    that emits `craft` with `recipeId`.
- Tooltip/icon rendering reuses the existing item-icon + tooltip helpers already
  used by the Shop tab where applicable.

### `Match.vue` / `useGameClient.ts`

- `Match.vue` passes `craftCatalog` and `hasArtificer` from the snapshot into
  `MatchMenu`, and handles its `craft` emit by calling a `useGameClient` method.
- `useGameClient.ts` exposes `craftItem(recipeId)` → `GameClient.sendCraftItem(recipeId)`
  (the sender already exists from Plan 3). Add the wrapper if absent.

### Optional nicety

A `C` hotkey to jump to the Craft tab, matching the existing `U`/`V` tab
hotkeys, if it's a cheap addition in the parent's hotkey handling.

## Testing

vitest on `getCraftCatalogSnapshot`, mirroring `GameState.shopCatalog.test.ts`:
construct a `GameState` with `localPlayerId`, `localPlayerUnlockedRecipeIds`, a
Vault, and a `crafting`-capable building owned by the player →
- all ingredients present → the entry's `have/need` are correct and `craftable`
  is true;
- missing an ingredient → `craftable` false;
- no owned Artificer → `craftable` false for every entry and
  `localPlayerHasArtificer()` is false;
- an unlocked id with no catalog def is skipped.

## Touched files

- `client/.../game/core/GameState.ts` — `CraftCatalogEntry`/`CraftCatalogIngredient`
  types, `getCraftCatalogSnapshot`, `localPlayerHasArtificer`.
- `client/.../game/core/GameState.craftCatalog.test.ts` (new).
- `client/.../game/core/GameClient.ts` — add `craftCatalog`/`hasArtificer` to the
  UI snapshot.
- `client/.../components/MatchMenu.vue` — tab, props, emit, craft body.
- `client/.../views/Match.vue` — pass props, handle `craft` emit.
- `client/.../composables/useGameClient.ts` — `craftItem(recipeId)` wrapper.
