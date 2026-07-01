# Craft Tab in the Match Menu — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "Craft" tab to the in-match menu (`MatchMenu.vue`) — alongside Shop / Upgrades / Vault — where a player crafts an unlocked recipe at their Artificer without selecting the building.

**Architecture:** A new read-only `GameState.getCraftCatalogSnapshot()` (sibling of `getShopCatalogSnapshot()`) derives the tab's data from state the server already sends (unlocked recipe ids, recipe catalog, Vault, building list). It flows through the existing UI snapshot into `MatchMenu`, which renders it and emits a `craft` event routed to the existing `GameClient.sendCraftItem(recipeId)`. Client-only — the server's `craft_item` command is unchanged and authoritative.

**Tech Stack:** Vue 3, TypeScript, Vite, **vitest** (`npm test`). Type-check/build: `npm run build`. Work from `client/src/game-portal/`.

## Global Constraints

(from `CLAUDE.md` + the spec)

- **Client renders server state only.** No gameplay simulation client-side. `craftable`/have-need are UX hints; the server re-validates every `craft_item`. Gold is displayed, never client-gated (matches the Shop tab / Artificer action bar).
- **No literal `cursor:` declarations** in new component CSS — the global rules handle it. `cursor: not-allowed` is acceptable only for a disabled/forbidden state, per-state.
- **`desktopBridge.ts` is the only file importing `@tauri-apps/api`** — this plan touches none of that.
- **Only unlocked recipes are listed** (`localPlayerUnlockedRecipeIds`); locked recipes are out of scope. Tab is always visible; content is gated by Artificer ownership. Tab label: `Craft`.
- **`craftable = localPlayerHasArtificer() && every ingredient have ≥ need`.**

Spec: `docs/superpowers/specs/2026-07-01-craft-tab-match-menu-design.md`.

---

### Task 1: `getCraftCatalogSnapshot` + `localPlayerHasArtificer` on GameState

**Files:**
- Modify: `client/src/game-portal/src/game/core/GameState.ts` (add types near `ShopCatalogEntry` ~line 548-568; add the two methods near `getShopCatalogSnapshot` ~line 2606)
- Test: `client/src/game-portal/src/game/core/GameState.craftCatalog.test.ts` (create)

**Interfaces:**
- Consumes: `this.localPlayerId`, `this.localPlayerUnlockedRecipeIds` (`string[]`), `this.localPlayerVault` (`VaultItemSnapshot[]` with `itemId`/`stacks`), `this.mapConfig.buildings` (`BuildingTile[]`), `RECIPE_DEF_MAP` (`../maps/recipeDefs`).
- Produces (exported from `GameState.ts`):
  - `interface CraftCatalogIngredient { itemId: string; have: number; need: number }`
  - `interface CraftCatalogEntry { recipeId: string; name: string; output: string; costGold: number; ingredients: CraftCatalogIngredient[]; craftable: boolean }`
  - `GameState.localPlayerHasArtificer(): boolean`
  - `GameState.getCraftCatalogSnapshot(): CraftCatalogEntry[]`

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it, beforeEach } from 'vitest'
import { GameState } from './GameState'
import { initRecipeDefs } from '../maps/recipeDefs'
import type { BuildingTile, VaultItemSnapshot } from '../network/protocol'

beforeEach(() => {
  initRecipeDefs([
    { id: 'fire_sword', name: 'Fire Sword', inputs: ['broad_sword', 'fire_ring'], costGold: 150, output: 'fire_sword' },
  ])
})

function artificer(ownerId: string, over: Partial<BuildingTile> = {}): BuildingTile {
  return {
    id: 'art-1', x: 0, y: 0, buildingType: 'artificer', width: 3, height: 3,
    occupied: true, visible: true, ownerId, capabilities: ['crafting'],
    ...over,
  } as BuildingTile
}

function vault(...ids: string[]): VaultItemSnapshot[] {
  return ids.map((itemId, i) => ({ instanceId: i + 1, itemId, stacks: 1 }))
}

describe('GameState.localPlayerHasArtificer', () => {
  it('is true only for an own, built crafting building', () => {
    const s = new GameState()
    s.localPlayerId = 'p1'
    s.mapConfig.buildings = [artificer('p1')]
    expect(s.localPlayerHasArtificer()).toBe(true)

    s.mapConfig.buildings = [artificer('p2')] // someone else's
    expect(s.localPlayerHasArtificer()).toBe(false)

    s.mapConfig.buildings = [artificer('p1', { metadata: { underConstruction: true } })]
    expect(s.localPlayerHasArtificer()).toBe(false)

    s.mapConfig.buildings = []
    expect(s.localPlayerHasArtificer()).toBe(false)
  })
})

describe('GameState.getCraftCatalogSnapshot', () => {
  function mk(): GameState {
    const s = new GameState()
    s.localPlayerId = 'p1'
    s.localPlayerUnlockedRecipeIds = ['fire_sword']
    s.mapConfig.buildings = [artificer('p1')]
    return s
  }

  it('reports have/need per ingredient and craftable when all present + artificer owned', () => {
    const s = mk()
    s.localPlayerVault = vault('broad_sword', 'fire_ring')
    const entries = s.getCraftCatalogSnapshot()
    expect(entries).toHaveLength(1)
    const e = entries[0]
    expect(e.recipeId).toBe('fire_sword')
    expect(e.name).toBe('Fire Sword')
    expect(e.output).toBe('fire_sword')
    expect(e.costGold).toBe(150)
    expect(e.ingredients).toEqual([
      { itemId: 'broad_sword', have: 1, need: 1 },
      { itemId: 'fire_ring', have: 1, need: 1 },
    ])
    expect(e.craftable).toBe(true)
  })

  it('is not craftable when an ingredient is missing', () => {
    const s = mk()
    s.localPlayerVault = vault('broad_sword') // no fire_ring
    const e = s.getCraftCatalogSnapshot()[0]
    expect(e.ingredients.find((i) => i.itemId === 'fire_ring')?.have).toBe(0)
    expect(e.craftable).toBe(false)
  })

  it('is not craftable when the player owns no artificer', () => {
    const s = mk()
    s.mapConfig.buildings = []
    s.localPlayerVault = vault('broad_sword', 'fire_ring')
    expect(s.getCraftCatalogSnapshot()[0].craftable).toBe(false)
  })

  it('skips unlocked ids with no catalog def', () => {
    const s = mk()
    s.localPlayerUnlockedRecipeIds = ['fire_sword', 'unknown_recipe']
    s.localPlayerVault = vault('broad_sword', 'fire_ring')
    expect(s.getCraftCatalogSnapshot().map((e) => e.recipeId)).toEqual(['fire_sword'])
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run (from `client/src/game-portal/`): `npm test -- craftCatalog`
Expected: FAIL — `getCraftCatalogSnapshot` / `localPlayerHasArtificer` don't exist.

> READ-FIRST before implementing: open `GameState.ts` and confirm the field names used above exist on the class: `localPlayerId`, `localPlayerUnlockedRecipeIds`, `localPlayerVault`, and `mapConfig.buildings`. They were added in earlier work (unlocked recipes in the recipe-crafting client plan; vault + mapConfig pre-exist). If any differ, match the real names. Confirm `VaultItemSnapshot` has `itemId` and `stacks` (it does — used by `getBuildingActions`).

- [ ] **Step 3: Add the types**

Near the `ShopCatalogEntry` interface in `GameState.ts` (around line 548-568), add:

```ts
export interface CraftCatalogIngredient {
  itemId: string
  have: number
  need: number
}
export interface CraftCatalogEntry {
  recipeId: string
  name: string
  output: string
  costGold: number
  ingredients: CraftCatalogIngredient[]
  craftable: boolean
}
```

Add `import { RECIPE_DEF_MAP } from '../maps/recipeDefs'` if not already imported in `GameState.ts` (Task 4 of the recipe-crafting client plan imported it for `getBuildingActions` — reuse the existing import; do not duplicate).

- [ ] **Step 4: Add the two methods**

Add near `getShopCatalogSnapshot` (after it is fine), inside the `GameState` class:

```ts
  // localPlayerHasArtificer reports whether the local player owns at least one
  // fully-built (not under-construction) building with the "crafting"
  // capability — the client-side mirror of the server's craft gate. Used to
  // gate the Craft tab's buttons + empty-state hint.
  localPlayerHasArtificer(): boolean {
    if (!this.localPlayerId) return false
    for (const b of this.mapConfig.buildings) {
      if (b.ownerId !== this.localPlayerId) continue
      if (b.metadata?.['underConstruction'] === true) continue
      if (b.capabilities?.includes('crafting')) return true
    }
    return false
  }

  // getCraftCatalogSnapshot builds the Craft tab's data from the player's
  // unlocked recipes: per recipe, the ingredient have/need (have summed from the
  // Vault, need counted from recipe.inputs incl. duplicates) and whether it is
  // craftable right now. Server re-validates on craft_item — this is a UX hint.
  getCraftCatalogSnapshot(): CraftCatalogEntry[] {
    const hasArtificer = this.localPlayerHasArtificer()
    const have = new Map<string, number>()
    for (const vi of this.localPlayerVault) {
      have.set(vi.itemId, (have.get(vi.itemId) ?? 0) + (vi.stacks ?? 1))
    }
    const entries: CraftCatalogEntry[] = []
    // Deterministic order: iterate the sorted unlocked ids.
    for (const recipeId of [...this.localPlayerUnlockedRecipeIds].sort()) {
      const recipe = RECIPE_DEF_MAP.get(recipeId)
      if (!recipe) continue
      const need = new Map<string, number>()
      for (const input of recipe.inputs) need.set(input, (need.get(input) ?? 0) + 1)
      let allPresent = true
      const ingredients: CraftCatalogIngredient[] = []
      for (const input of recipe.inputs) {
        if (ingredients.some((i) => i.itemId === input)) continue // dedup display
        const needCount = need.get(input) ?? 0
        const haveCount = have.get(input) ?? 0
        if (haveCount < needCount) allPresent = false
        ingredients.push({ itemId: input, have: haveCount, need: needCount })
      }
      entries.push({
        recipeId: recipe.id,
        name: recipe.name,
        output: recipe.output,
        costGold: recipe.costGold,
        ingredients,
        craftable: hasArtificer && allPresent,
      })
    }
    entries.sort((a, b) => a.name.localeCompare(b.name) || a.recipeId.localeCompare(b.recipeId))
    return entries
  }
```

- [ ] **Step 5: Run test to verify it passes**

Run: `npm test -- craftCatalog` — PASS (all cases). Then `npm run build` — clean.

- [ ] **Step 6: Commit**

```bash
git add src/game/core/GameState.ts src/game/core/GameState.craftCatalog.test.ts
git commit -m "feat(client): add getCraftCatalogSnapshot + localPlayerHasArtificer to GameState"
```

---

### Task 2: Snapshot plumbing + `craftItem` command wrapper

**Files:**
- Modify: `client/src/game-portal/src/game/core/GameClient.ts` (the UI snapshot object, ~line 300-325, that lists `shopCatalog`, `vault`, …)
- Modify: `client/src/game-portal/src/composables/useGameClient.ts` (add a `craftItem` wrapper near `sendPurchaseItem` ~line 233-234, export it ~line 301)
- Test: none (type-checked by `npm run build`; the snapshot values are exercised in Task 3's manual smoke + Task 1's unit test of the underlying method)

**Interfaces:**
- Consumes: `GameState.getCraftCatalogSnapshot()`, `GameState.localPlayerHasArtificer()` (Task 1); `GameClient.sendCraftItem(recipeId)` (already exists from the recipe-crafting client plan).
- Produces:
  - The UI snapshot object gains `craftCatalog: CraftCatalogEntry[]` and `hasArtificer: boolean`.
  - `useGameClient` returns a `craftItem(recipeId: string)` function that calls `client?.sendCraftItem(recipeId)`.

- [ ] **Step 1: Add snapshot fields**

In `GameClient.ts`, in the UI snapshot object literal that already contains `shopCatalog: this.state.getShopCatalogSnapshot(),` and `shopRerollsRemaining: this.state.localPlayerShopRerollsRemaining,` (~line 312-313), add:

```ts
      craftCatalog: this.state.getCraftCatalogSnapshot(),
      hasArtificer: this.state.localPlayerHasArtificer(),
```

(If this object has an explicit TypeScript type/interface, add the two fields there too — search the file for where the snapshot's return type is declared and add `craftCatalog: CraftCatalogEntry[]` and `hasArtificer: boolean`, importing the type from `./GameState`.)

- [ ] **Step 2: Add the `craftItem` wrapper**

In `useGameClient.ts`, next to `sendPurchaseItem` (line 233-234):

```ts
  function craftItem(recipeId: string) {
    client?.sendCraftItem(recipeId)
  }
```

and add `craftItem` to the returned object (the `return { … }` near line 300-301 that exposes `sendPurchaseItem`).

- [ ] **Step 3: Type-check**

Run: `npm run build` — clean (confirms `sendCraftItem` exists on `GameClient`, the snapshot fields type-check, and `CraftCatalogEntry` is importable). There is no unit test for this wiring; the build is the gate.

- [ ] **Step 4: Commit**

```bash
git add src/game/core/GameClient.ts src/composables/useGameClient.ts
git commit -m "feat(client): expose craftCatalog + hasArtificer in the UI snapshot and a craftItem command"
```

---

### Task 3: The Craft tab in `MatchMenu.vue` + `Match.vue` wiring

**Files:**
- Modify: `client/src/game-portal/src/components/MatchMenu.vue` (`TABS` ~line 178-182; props ~188-220; emits ~298-302; add a Craft tab body in the template)
- Modify: `client/src/game-portal/src/views/Match.vue` (the `<MatchMenu … />` usage — pass the new props + handle `@craft`)
- Test: none (Vue template; verified by `npm run build` + manual smoke — this repo does not unit-test SFC templates)

**Interfaces:**
- Consumes: `craftCatalog`/`hasArtificer` from the UI snapshot (Task 2); `useGameClient.craftItem` (Task 2); the `CraftCatalogEntry` type from `@/game/core/GameState`.
- Produces: a `craft` emit (`[recipeId: string]`) from `MatchMenu`; a Craft tab rendering the catalog.

- [ ] **Step 1: Add the tab, props, and emit in `MatchMenu.vue`**

`TABS` (line 178-182) — add the 4th entry (there are already 4 tab slots):

```ts
const TABS: TabDef[] = [
  { id: 'shop', label: 'Shop' },
  { id: 'upgrades', label: 'Upgrades' },
  { id: 'vault', label: 'Vault' },
  { id: 'craft', label: 'Craft' },
]
```

Import the type at the top (next to the existing `import type { ShopCatalogEntry, Unit } from '@/game/core/GameState'`):

```ts
import type { ShopCatalogEntry, Unit, CraftCatalogEntry } from '@/game/core/GameState'
```

Add to `defineProps` (and its `withDefaults`):

```ts
  craftCatalog?: CraftCatalogEntry[]
  hasArtificer?: boolean
```
```ts
  craftCatalog: () => [],
  hasArtificer: false,
```

Add to `defineEmits` (line 298-302):

```ts
  craft: [recipeId: string]
```

- [ ] **Step 2: Add the Craft tab body to the template**

In `MatchMenu.vue`'s template, find where the tab panels are rendered (the `v-if="activeTabId === 'shop'"` / `'upgrades'` / `'vault'` panels). Add a sibling panel for `craft`, matching the surrounding panel/scroll markup and class names used by the other tabs:

```html
<div v-if="activeTabId === 'craft'" class="menu-panel craft-panel">
  <p v-if="!hasArtificer" class="menu-hint">Build an Artificer to craft items.</p>
  <p v-else-if="craftCatalog.length === 0" class="menu-hint">
    Buy a recipe at a Recipe Shop to craft it here.
  </p>
  <div v-else class="craft-list">
    <div v-for="entry in craftCatalog" :key="entry.recipeId" class="craft-row">
      <div class="craft-row__head">
        <span class="craft-row__name">{{ entry.name }}</span>
        <span class="craft-row__cost">{{ entry.costGold }} gold</span>
      </div>
      <ul class="craft-row__ingredients">
        <li
          v-for="ing in entry.ingredients"
          :key="ing.itemId"
          :class="{ 'craft-ing--short': ing.have < ing.need }"
        >{{ ing.itemId }} {{ ing.have }}/{{ ing.need }}</li>
      </ul>
      <button
        type="button"
        class="craft-row__btn"
        :disabled="!entry.craftable"
        @click="emit('craft', entry.recipeId)"
      >Craft</button>
    </div>
  </div>
</div>
```

Add matching styles in the component's `<style>` block using the existing panel/hint/button class conventions. **Do NOT write any literal `cursor:` declaration** — the global rules cover it; a disabled button may use `cursor: not-allowed` only as a per-state rule if the existing disabled buttons do. Prefer to reuse existing menu/button classes over new bespoke ones where they fit.

> READ-FIRST: skim the existing `shop`/`vault` panel markup + their container/scroll classes so the Craft panel visually matches (same outer panel class, same empty-hint styling). Reuse `menu-hint`/panel classes if they already exist under different names; adapt the class names above to the real ones.

- [ ] **Step 3: Wire `Match.vue`**

READ-FIRST: find the `<MatchMenu … />` element in `views/Match.vue` (it already binds `:shop-catalog`, `:upgrades`, `:vault`, `@purchase`, `@reroll`, etc.). Add the two prop bindings and the emit handler, sourcing values from the same UI snapshot the other props come from and calling the `craftItem` wrapper from `useGameClient`:

```html
  :craft-catalog="ui.craftCatalog"
  :has-artificer="ui.hasArtificer"
  @craft="craftItem"
```

(Use the actual snapshot variable name in scope — match how `:shop-catalog` is bound. Ensure `craftItem` is pulled from `useGameClient()` alongside the existing `sendPurchaseItem`/`rerollShop` in this file.)

- [ ] **Step 4: (Optional) `C` hotkey to open the Craft tab**

If the parent maps `U`→upgrades / `V`→vault for the menu tabs (search `activeTab`/`'vault'` in `Match.vue` / `InputManager.ts`), add a `C`→`craft` mapping the same way. If no such per-tab hotkey mapping exists in a single obvious place, skip this — it's a nicety, not a requirement. Note in the report whether it was added.

- [ ] **Step 5: Type-check + build**

Run: `npm run build` — clean.

- [ ] **Step 6: Commit**

```bash
git add src/components/MatchMenu.vue src/views/Match.vue
git commit -m "feat(client): add a Craft tab to the match menu"
```

---

### Task 4: Full client verification

**Files:** none (verification only).

- [ ] **Step 1: Run the whole client test suite**

Run (from `client/src/game-portal/`): `npm test`
Expected: PASS — existing tests plus the new `GameState.craftCatalog.test.ts`.

- [ ] **Step 2: Type-check + build**

Run: `npm run build`
Expected: clean (`vue-tsc -b` + `vite build`).

- [ ] **Step 3: Commit (only if an incidental fix was needed)**

```bash
git add -A
git commit -m "test: full client verification for the Craft tab"
```

---

## Manual smoke (optional, not a task gate)

Run server + client on the branch: unlock a recipe at a Recipe Shop, build an Artificer, open the match menu → **Craft** tab. With ingredients in the Vault the row's Craft button is enabled → clicking crafts (server produces the item in the Vault). Without an Artificer the tab shows the "Build an Artificer" hint and buttons are disabled; with an Artificer but no unlocked recipes it shows the "Buy a recipe" hint.

## Self-review checklist (completed during authoring)

- **Spec coverage:** always-visible Craft tab (T3), unlocked-only list + have/need + craftable (T1), gated empty states (T3), craft from menu → `sendCraftItem` (T2/T3), no server change, gold shown not gated (T1/T3), optional `C` hotkey (T3). All spec sections map to a task.
- **Placeholders:** the three READ-FIRST steps (GameState field names, MatchMenu panel classes, Match.vue MatchMenu binding + hotkey location) each name the exact anchor to match; all code steps show complete code. No "TBD"/vague steps.
- **Type consistency:** `CraftCatalogEntry`/`CraftCatalogIngredient`, `getCraftCatalogSnapshot`, `localPlayerHasArtificer`, `craftCatalog`/`hasArtificer`, `craftItem`, `craft` emit — names consistent across T1→T3.
- **Determinism/constraints:** snapshot iterates sorted unlocked ids; no `cursor:` literals mandated; client-only, server re-validates.
