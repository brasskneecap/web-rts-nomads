# Item Editor — Plan B: Client UI — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The Item Editor UI: a fifth MainMenu entry, `/item-editor` route + view shell, an `ItemEditorPanel` (sidebar + seven accordion form sections) wired to Plan A's endpoints, icon gallery + upload with a bundled→server icon fallback, and one small server addition (`GET /items/{id}/availability`) so the form loads an item's current shop placement truthfully.

**Architecture:** Mirrors the Map Editor trio (view shell → panel component → service module). Form logic lives in a PURE module (`itemEditorForm.ts`: def+availability+recipe → form state → save request) so it unit-tests without any DOM or HTTP; `itemEditorApi.ts` wraps the endpoints following `catalog.ts` conventions. Icon resolution: bundled glob first (`itemAssets.ts`), server URL fallback (absorbing and deleting the orphaned `itemCatalogImages.ts`).

**Tech Stack:** Vue 3 `<script setup>` + TypeScript, vitest (happy-dom), Go 1.22 for the one server endpoint. Client root: `client/src/game-portal`.

**Spec:** `docs/superpowers/specs/2026-07-09-item-editor-design.md` (Plan B sections)

## Global Constraints

- Branch: `ui-item-editor` (verify, never switch).
- Client is a VIEW; the server validates. The editor surfaces the server's `{"error":"validation_failed","message":...}` beside the Save button — no client-side re-implementation of validation beyond input types.
- **Cursor rule (repo-binding):** component CSS must NOT declare `cursor` (global rules own it); only `cursor: not-allowed` on forbidden states is allowed.
- UI conventions to mirror (from MapEditorPanel/Editor.vue): accordion `editor-section` / `editor-section__summary` / `editor-section__body` + `control-group` label+input rows with RAW HTML inputs (no shared form components exist); `UiButton` (`components/ui/UiButton.vue`, props disabled/selected/size) for actions; `ExitButton` top-right in the view shell; scoped styles, dark-slate labels `rgba(226,232,240,0.86)`.
- Service conventions (from `game/maps/catalog.ts`): `const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''`; GET = fetch → `if (!ok) throw new Error(...status)` → unwrap field; POST reads specific statuses before generic; custom Error subclasses co-located with typed readonly payload + explicit `this.name`.
- Percent↔fraction: UI shows percentages (chance, dodge/block), wire stores fractions; conversions live ONLY in `itemEditorForm.ts`.
- Known Plan-A quirk to carry: in dev the writable dir IS the embed source, so `/catalog/items` reports `overridden: true` for everything — the sidebar badge ships anyway (it's correct in packaged builds); add a one-line comment where the badge renders.
- **Menu sign caveat:** `main-menu.png` has FOUR planks baked in; five entries require respaced `top` values (55.97 → 75.25 span, even 4.82 steps) and the fifth label will not sit on its own plank until the art is updated. Proceed with respaced values; the in-app sign tuner (`localStorage 'webrts.signTuner'='1'` + backtick) lets the user re-tune later.
- Test commands: client `cd client/src/game-portal && npm test` and `npm run build`; server `cd server && go test ./internal/... `. No NEW failures (known flaky: cmd/api TestServerReadyLineAndStdinShutdown).
- Fetch mocking: the repo has NO fetch-mocking precedent — `itemEditorApi.test.ts` introduces `vi.stubGlobal('fetch', vi.fn())`, restored in `afterEach` (`vi.unstubAllGlobals()`). Keep it contained to that one file.
- Commit messages: short imperative.

---

### Task 1: Server — `GET /items/{id}/availability`

**Files:**
- Modify: `server/internal/game/item_editor.go` (add `GetItemAvailability`)
- Modify: `server/internal/http/editor_handlers.go` (GET branch in the `/items/` handler)
- Test: append to `server/internal/game/item_editor_test.go` and `server/internal/http/editor_routes_test.go`

**Interfaces:**
- Consumes: `getItemDef`, `getItemListDef`, `getRecipeListDef`, `getRecipeDef`, `getPackagedItem`, `merchantSubtableForCategory` (all committed in Plan A).
- Produces: `game.GetItemAvailability(id string) (EditorAvailability, bool)` (bool=false when the item doesn't exist) — loot weight reported as the row's current d100 width; wire `GET /items/{id}/availability` → 200 `EditorAvailability` JSON | 404.

- [ ] **Step 1: Failing tests**

Append to `server/internal/game/item_editor_test.go`:
```go
// TestGetItemAvailability_ReflectsAllFourSurfaces: after a full-surface save,
// availability reads back exactly what was requested (weight ≈ requested,
// rounding tolerance from renormalization).
func TestGetItemAvailability_ReflectsAllFourSurfaces(t *testing.T) {
	editorEnv(t)
	req := EditorItemSaveRequest{
		Item: ItemDef{ID: "avail_probe", DisplayName: "Probe", IconKey: "avail_probe",
			Kind: ItemKindEquipment, Tier: ItemTierCommon, Category: "Weapon", SlotKind: "any", CostGold: 5},
		Recipe: &EditorRecipeSpec{Inputs: []string{"broad_sword", "fire_ring"}, CostGold: 10},
		Availability: EditorAvailability{
			Marketplace: true, WanderingMerchant: false,
			LootTable:  EditorLootAvailability{Enabled: true, Weight: 20},
			RecipeList: true,
		},
	}
	if err := SaveEditorItem(req); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, ok := GetItemAvailability("avail_probe")
	if !ok {
		t.Fatal("item exists, availability must resolve")
	}
	if !got.Marketplace || got.WanderingMerchant {
		t.Errorf("list flags wrong: %+v", got)
	}
	if !got.LootTable.Enabled || got.LootTable.Weight < 15 || got.LootTable.Weight > 25 {
		t.Errorf("loot flag/weight wrong (want enabled, ~20): %+v", got.LootTable)
	}
	if !got.RecipeList {
		t.Errorf("recipeList flag wrong: %+v", got)
	}
	// Unknown item → ok=false.
	if _, ok := GetItemAvailability("no_such_item_at_all"); ok {
		t.Error("unknown item must report ok=false")
	}
	// A shipped item with no placements reports all-false without error.
	if av, ok := GetItemAvailability("frost_sword"); !ok || av.Marketplace {
		t.Errorf("frost_sword: ok=%v av=%+v (crafted-only item, not in marketplace)", ok, av)
	}
}
```
Append to `server/internal/http/editor_routes_test.go`:
```go
func TestItemAvailabilityRoute(t *testing.T) {
	t.Setenv("ITEM_CATALOG_DIR", t.TempDir())
	t.Setenv("RECIPE_CATALOG_DIR", t.TempDir())
	t.Setenv("NEUTRAL_GROUPS_DIR", t.TempDir())
	srv := newTestRouter(t)
	resp, err := srv.Client().Get(srv.URL + "/items/frost_sword/availability")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var av struct {
		Marketplace bool `json:"marketplace"`
		LootTable   struct {
			Enabled bool `json:"enabled"`
			Weight  int  `json:"weight"`
		} `json:"lootTable"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&av); err != nil {
		t.Fatalf("decode: %v", err)
	}
	miss, err := srv.Client().Get(srv.URL + "/items/no_such_item/availability")
	if err != nil {
		t.Fatalf("GET miss: %v", err)
	}
	defer miss.Body.Close()
	if miss.StatusCode != 404 {
		t.Fatalf("unknown item expected 404, got %d", miss.StatusCode)
	}
}
```

- [ ] **Step 2: Verify failure** — `cd server && go test ./internal/game/ -run TestGetItemAvailability -v && go test ./internal/http/ -run TestItemAvailabilityRoute -v` → compile failures.

- [ ] **Step 3: Implement**

`item_editor.go`:
```go
// GetItemAvailability reports where an item is currently placed across the
// four editor-managed surfaces. ok is false when the item id resolves to no
// def. Loot weight is the row's current d100 width in the item's
// category-mapped merchant subtable.
func GetItemAvailability(id string) (EditorAvailability, bool) {
	def, ok := getItemDef(id)
	if !ok {
		return EditorAvailability{}, false
	}
	var av EditorAvailability
	if list, ok := getItemListDef("marketplace"); ok {
		av.Marketplace = containsString(list.Items, id)
	}
	if list, ok := getItemListDef("wandering_merchant"); ok {
		av.WanderingMerchant = containsString(list.Items, id)
	}
	if sub, ok := getPackagedItem(merchantSubtableForCategory(def.Category)); ok {
		for _, e := range sub.Entries {
			if e.Item == id {
				av.LootTable.Enabled = true
				av.LootTable.Weight = e.Max - e.Min + 1
				break
			}
		}
	}
	if list, ok := getRecipeListDef("druid_recipes_1"); ok {
		av.RecipeList = containsString(list.Recipes, id)
	}
	return av, true
}
```
(`containsString`: reuse the game package's existing helper — grep for its exact name/location; adapt if different.)

`editor_handlers.go` — in the `/items/` handler, add a GET branch (before the image POST branch):
```go
		if rest, isAvail := strings.CutSuffix(id, "/availability"); isAvail && r.Method == http.MethodGet {
			av, found := game.GetItemAvailability(rest)
			if !found {
				writeJSONError(w, http.StatusNotFound, "not_found", "no item "+rest)
				return
			}
			writeJSON(w, av)
			return
		}
```

- [ ] **Step 4: Verify pass + commit**

Run: the Step-2 commands → PASS; `cd server && go build ./...` clean.
```bash
git add server/internal/game/item_editor.go server/internal/game/item_editor_test.go server/internal/http/editor_handlers.go server/internal/http/editor_routes_test.go
git commit -m "Add GET /items/{id}/availability for the editor form"
```

---

### Task 2: Client service + pure form module

**Files:**
- Create: `client/src/game-portal/src/game/items/itemEditorApi.ts`
- Create: `client/src/game-portal/src/game/items/itemEditorForm.ts`
- Test: `client/src/game-portal/src/game/items/itemEditorForm.test.ts`, `client/src/game-portal/src/game/items/itemEditorApi.test.ts`

**Interfaces:**
- Consumes: `ItemDef` type (`@/game/maps/itemDefs`), `RecipeDef` type (`@/game/maps/recipeDefs` — verify export name by reading the file), catalog.ts conventions.
- Produces (exact shapes Tasks 4-5 rely on):
```ts
// itemEditorApi.ts
export type ProcEffectDef = { id: string; damage: number; damageType: string; projectileID: string;
  projectileScale?: number; bounceCount?: number; bounceRange?: number; bounceDamageFalloff?: number;
  slowMultiplier?: number; slowDurationSeconds?: number; burnDamagePerSecond?: number; burnDurationSeconds?: number }
export type ItemAvailability = { marketplace: boolean; wanderingMerchant: boolean;
  lootTable: { enabled: boolean; weight: number }; recipeList: boolean }
export type EditorSaveRequest = { item: Record<string, unknown>; recipe: { inputs: string[]; costGold: number } | null; availability: ItemAvailability }
export class EditorValidationError extends Error { readonly serverMessage: string }
export async function fetchProcEffectDefs(): Promise<ProcEffectDef[]>
export async function fetchItemAvailability(id: string): Promise<ItemAvailability>
export async function saveEditorItem(req: EditorSaveRequest): Promise<void>   // throws EditorValidationError on 400 validation_failed
export async function deleteEditorItem(id: string): Promise<'deleted' | 'reset'>
export async function uploadItemIcon(id: string, file: Blob): Promise<void>
export function itemIconUrl(id: string): string                                // `${API_BASE}/catalog/items/${id}/image`

// itemEditorForm.ts
export type ProcForm = { enabled: boolean; effect: string; chancePct: number;
  damage: number | null; projectileScale: number | null; bounceCount: number | null; bounceRange: number | null;
  bounceDamageFalloff: number | null; slowMultiplier: number | null; slowDurationSeconds: number | null;
  burnDamagePerSecond: number | null; burnDurationSeconds: number | null }   // null = "no override"
export type ItemEditorForm = { id: string; isNew: boolean; displayName: string; description: string;
  iconKey: string; tier: string; category: string; slotKind: string; costGold: number;
  mods: { hp: number; damage: number; armor: number; attackSpeed: number; moveSpeed: number;
    healthRegen: number; maxShield: number; dodgePct: number; blockPct: number };
  elemental: { type: string; amount: number }[];
  onHit: ProcForm; onStruck: ProcForm;
  crafting: { enabled: boolean; inputA: string; inputB: string; costGold: number };
  availability: ItemAvailability }
export function createBlankForm(): ItemEditorForm
export function formFromDef(def: ItemDef, availability: ItemAvailability, recipe: { inputs: string[]; costGold: number } | null): ItemEditorForm
export function saveRequestFromForm(form: ItemEditorForm): EditorSaveRequest
```

- [ ] **Step 1: Failing form-module tests**

`itemEditorForm.test.ts`:
```ts
import { describe, expect, it } from 'vitest'
import { createBlankForm, formFromDef, saveRequestFromForm } from './itemEditorForm'
import type { ItemDef } from '../maps/itemDefs'

const fireShield: ItemDef = {
  id: 'fire_shield', displayName: 'Fire Shield', iconKey: 'fire_shield',
  kind: 'equipment', tier: 'rare', slotKind: 'any', costGold: 0, category: 'Shield',
  modifiers: { armor: 35, blockChance: 0.15 },
  onStruckProc: { chance: 0.1, effect: 'fire_bolt_ignite', damage: 25, damageType: 'fire', projectileID: 'fire_bolt' },
}
const avail = { marketplace: false, wanderingMerchant: false, lootTable: { enabled: false, weight: 0 }, recipeList: true }

describe('formFromDef / saveRequestFromForm round-trip', () => {
  it('converts fractions to percents and back', () => {
    const form = formFromDef(fireShield, avail, { inputs: ['steel_shield', 'fire_ring'], costGold: 150 })
    expect(form.mods.blockPct).toBe(15)
    expect(form.onStruck.enabled).toBe(true)
    expect(form.onStruck.chancePct).toBe(10)
    expect(form.onStruck.effect).toBe('fire_bolt_ignite')
    expect(form.crafting.enabled).toBe(true)
    expect(form.crafting.inputA).toBe('steel_shield')
    expect(form.isNew).toBe(false)

    const req = saveRequestFromForm(form)
    const item = req.item as Record<string, any>
    expect(item.modifiers.blockChance).toBeCloseTo(0.15)
    expect(item.modifiers.armor).toBe(35)
    expect(item.modifiers.dodgeChance).toBeUndefined() // zero mods omitted
    expect(item.onStruckProc).toEqual({ chance: 0.1, effect: 'fire_bolt_ignite' }) // overrides all null → omitted
    expect(item.onHitProc).toBeUndefined() // disabled proc omitted
    expect(req.recipe).toEqual({ inputs: ['steel_shield', 'fire_ring'], costGold: 150 })
    expect(req.availability.recipeList).toBe(true)
  })

  it('includes only non-null proc overrides', () => {
    const form = createBlankForm()
    form.id = 'x'
    form.onHit.enabled = true
    form.onHit.effect = 'lightning_chain'
    form.onHit.chancePct = 25
    form.onHit.bounceCount = 4
    const item = saveRequestFromForm(form).item as Record<string, any>
    expect(item.onHitProc).toEqual({ chance: 0.25, effect: 'lightning_chain', bounceCount: 4 })
  })

  it('blank form: no recipe, everything off, empty elemental', () => {
    const form = createBlankForm()
    expect(form.isNew).toBe(true)
    const req = saveRequestFromForm(form)
    expect(req.recipe).toBeNull()
    const item = req.item as Record<string, any>
    expect(item.modifiers).toBeUndefined()
    expect(item.onHitElemental).toBeUndefined()
  })

  it('elemental rows with zero amounts are dropped', () => {
    const form = createBlankForm()
    form.id = 'x'
    form.elemental = [{ type: 'fire', amount: 5 }, { type: 'cold', amount: 0 }]
    const item = saveRequestFromForm(form).item as Record<string, any>
    expect(item.onHitElemental).toEqual([{ type: 'fire', amount: 5 }])
  })
})
```

- [ ] **Step 2: Failing api tests** (`itemEditorApi.test.ts` — first fetch mocking in the repo, contained here):
```ts
import { afterEach, describe, expect, it, vi } from 'vitest'
import { EditorValidationError, deleteEditorItem, fetchItemAvailability, saveEditorItem } from './itemEditorApi'

function stubFetch(status: number, body: unknown) {
  const mock = vi.fn(async () => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }))
  vi.stubGlobal('fetch', mock)
  return mock
}
afterEach(() => vi.unstubAllGlobals())

describe('itemEditorApi', () => {
  it('saveEditorItem posts to /items and resolves on 201', async () => {
    const mock = stubFetch(201, { id: 'x', status: 'saved' })
    await saveEditorItem({ item: { id: 'x' }, recipe: null, availability: { marketplace: false, wanderingMerchant: false, lootTable: { enabled: false, weight: 0 }, recipeList: false } })
    expect(mock).toHaveBeenCalledOnce()
    const [url, init] = mock.mock.calls[0] as unknown as [string, RequestInit]
    expect(url.endsWith('/items')).toBe(true)
    expect(init.method).toBe('POST')
  })
  it('saveEditorItem throws EditorValidationError with the server message on 400', async () => {
    stubFetch(400, { error: 'validation_failed', message: 'item id "X" must match ^[a-z0-9_]+$' })
    await expect(saveEditorItem({ item: { id: 'X' }, recipe: null, availability: { marketplace: false, wanderingMerchant: false, lootTable: { enabled: false, weight: 0 }, recipeList: false } }))
      .rejects.toSatisfy((e: unknown) => e instanceof EditorValidationError && (e as EditorValidationError).serverMessage.includes('must match'))
  })
  it('deleteEditorItem returns the status field', async () => {
    stubFetch(200, { id: 'x', status: 'reset' })
    await expect(deleteEditorItem('x')).resolves.toBe('reset')
  })
  it('fetchItemAvailability unwraps the availability object', async () => {
    stubFetch(200, { marketplace: true, wanderingMerchant: false, lootTable: { enabled: true, weight: 12 }, recipeList: false })
    const av = await fetchItemAvailability('x')
    expect(av.lootTable.weight).toBe(12)
  })
})
```

- [ ] **Step 3: Verify failure** — `cd client/src/game-portal && npm test` → the two new files fail (modules missing).

- [ ] **Step 4: Implement `itemEditorApi.ts`** (follow catalog.ts conventions exactly):
```ts
import type { ItemAvailability } from './itemEditorForm' // or define here and import in form — pick ONE home: define ItemAvailability in itemEditorApi.ts and have itemEditorForm.ts import it.

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

export type ProcEffectDef = { /* as in Interfaces */ }
export type ItemAvailability = { marketplace: boolean; wanderingMerchant: boolean; lootTable: { enabled: boolean; weight: number }; recipeList: boolean }
export type EditorSaveRequest = { item: Record<string, unknown>; recipe: { inputs: string[]; costGold: number } | null; availability: ItemAvailability }

// EditorValidationError carries the server's validation message for inline
// display beside the Save button (the server is the validator — see spec).
export class EditorValidationError extends Error {
  readonly serverMessage: string
  constructor(message: string) {
    super(message)
    this.name = 'EditorValidationError'
    this.serverMessage = message
  }
}

export async function fetchProcEffectDefs(): Promise<ProcEffectDef[]> {
  const response = await fetch(`${API_BASE}/catalog/procs`)
  if (!response.ok) throw new Error(`Failed to load proc effects: ${response.status}`)
  const data = (await response.json()) as { procs: ProcEffectDef[] }
  return data.procs
}

export async function fetchItemAvailability(id: string): Promise<ItemAvailability> {
  const response = await fetch(`${API_BASE}/items/${encodeURIComponent(id)}/availability`)
  if (!response.ok) throw new Error(`Failed to load availability: ${response.status}`)
  return (await response.json()) as ItemAvailability
}

export async function saveEditorItem(req: EditorSaveRequest): Promise<void> {
  const response = await fetch(`${API_BASE}/items`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  })
  if (response.status === 400) {
    const body = (await response.json().catch(() => null)) as { message?: string } | null
    throw new EditorValidationError(body?.message ?? 'Validation failed')
  }
  if (!response.ok) {
    const text = await response.text().catch(() => response.statusText)
    throw new Error(text || `Server error ${response.status}`)
  }
}

export async function deleteEditorItem(id: string): Promise<'deleted' | 'reset'> {
  const response = await fetch(`${API_BASE}/items/${encodeURIComponent(id)}`, { method: 'DELETE' })
  if (!response.ok) {
    const text = await response.text().catch(() => response.statusText)
    throw new Error(text || `Server error ${response.status}`)
  }
  const body = (await response.json()) as { status: 'deleted' | 'reset' }
  return body.status
}

export async function uploadItemIcon(id: string, file: Blob): Promise<void> {
  const response = await fetch(`${API_BASE}/items/${encodeURIComponent(id)}/image`, {
    method: 'POST',
    headers: { 'Content-Type': 'image/png' },
    body: file,
  })
  if (!response.ok) {
    const text = await response.text().catch(() => response.statusText)
    throw new Error(text || `Icon upload failed (${response.status})`)
  }
}

export function itemIconUrl(id: string): string {
  return `${API_BASE}/catalog/items/${encodeURIComponent(id)}/image`
}
```

- [ ] **Step 5: Implement `itemEditorForm.ts`** — pure transforms. Complete implementation:
```ts
import type { ItemDef } from '../maps/itemDefs'
import type { EditorSaveRequest, ItemAvailability } from './itemEditorApi'

export type ProcForm = { /* as in Interfaces */ }
export type ItemEditorForm = { /* as in Interfaces */ }

const blankProc = (): ProcForm => ({ enabled: false, effect: '', chancePct: 10,
  damage: null, projectileScale: null, bounceCount: null, bounceRange: null, bounceDamageFalloff: null,
  slowMultiplier: null, slowDurationSeconds: null, burnDamagePerSecond: null, burnDurationSeconds: null })

export function createBlankForm(): ItemEditorForm {
  return {
    id: '', isNew: true, displayName: '', description: '', iconKey: '', tier: 'common',
    category: 'Weapon', slotKind: 'any', costGold: 0,
    mods: { hp: 0, damage: 0, armor: 0, attackSpeed: 0, moveSpeed: 0, healthRegen: 0, maxShield: 0, dodgePct: 0, blockPct: 0 },
    elemental: [],
    onHit: blankProc(), onStruck: blankProc(),
    crafting: { enabled: false, inputA: '', inputB: '', costGold: 150 },
    availability: { marketplace: false, wanderingMerchant: false, lootTable: { enabled: false, weight: 10 }, recipeList: false },
  }
}

function procFormFromWire(p: ItemDef['onHitProc']): ProcForm {
  if (!p) return blankProc()
  return { enabled: true, effect: p.effect ?? '', chancePct: Math.round(p.chance * 100),
    // Wire carries RESOLVED values; overrides are not distinguishable from
    // base values on the wire, so loading an item shows resolved numbers as
    // placeholders and leaves overrides null (= inherit). Editing a field
    // sets an override.
    damage: null, projectileScale: null, bounceCount: null, bounceRange: null, bounceDamageFalloff: null,
    slowMultiplier: null, slowDurationSeconds: null, burnDamagePerSecond: null, burnDurationSeconds: null }
}

export function formFromDef(def: ItemDef, availability: ItemAvailability, recipe: { inputs: string[]; costGold: number } | null): ItemEditorForm {
  const m = def.modifiers ?? {}
  return {
    id: def.id, isNew: false, displayName: def.displayName, description: def.description ?? '',
    iconKey: def.iconKey, tier: def.tier, category: def.category ?? 'Weapon', slotKind: def.slotKind, costGold: def.costGold,
    mods: { hp: m.hp ?? 0, damage: m.damage ?? 0, armor: m.armor ?? 0, attackSpeed: m.attackSpeed ?? 0,
      moveSpeed: m.moveSpeed ?? 0, healthRegen: m.healthRegen ?? 0, maxShield: m.maxShield ?? 0,
      dodgePct: Math.round((m.dodgeChance ?? 0) * 100), blockPct: Math.round((m.blockChance ?? 0) * 100) },
    elemental: (def.onHitElemental ?? []).map((e) => ({ ...e })),
    onHit: procFormFromWire(def.onHitProc), onStruck: procFormFromWire(def.onStruckProc),
    crafting: recipe
      ? { enabled: true, inputA: recipe.inputs[0] ?? '', inputB: recipe.inputs[1] ?? '', costGold: recipe.costGold }
      : { enabled: false, inputA: '', inputB: '', costGold: 150 },
    availability: { ...availability, lootTable: { ...availability.lootTable, weight: availability.lootTable.weight || 10 } },
  }
}

function procWireFromForm(p: ProcForm): Record<string, unknown> | undefined {
  if (!p.enabled || !p.effect) return undefined
  const wire: Record<string, unknown> = { chance: p.chancePct / 100, effect: p.effect }
  const overrides: [string, number | null][] = [
    ['damage', p.damage], ['projectileScale', p.projectileScale], ['bounceCount', p.bounceCount],
    ['bounceRange', p.bounceRange], ['bounceDamageFalloff', p.bounceDamageFalloff],
    ['slowMultiplier', p.slowMultiplier], ['slowDurationSeconds', p.slowDurationSeconds],
    ['burnDamagePerSecond', p.burnDamagePerSecond], ['burnDurationSeconds', p.burnDurationSeconds],
  ]
  for (const [key, v] of overrides) {
    if (v !== null && v !== 0) wire[key] = v
  }
  return wire
}

export function saveRequestFromForm(form: ItemEditorForm): EditorSaveRequest {
  const m = form.mods
  const modifiers: Record<string, number> = {}
  if (m.hp) modifiers.hp = m.hp
  if (m.damage) modifiers.damage = m.damage
  if (m.armor) modifiers.armor = m.armor
  if (m.attackSpeed) modifiers.attackSpeed = m.attackSpeed
  if (m.moveSpeed) modifiers.moveSpeed = m.moveSpeed
  if (m.healthRegen) modifiers.healthRegen = m.healthRegen
  if (m.maxShield) modifiers.maxShield = m.maxShield
  if (m.dodgePct) modifiers.dodgeChance = m.dodgePct / 100
  if (m.blockPct) modifiers.blockChance = m.blockPct / 100

  const elemental = form.elemental.filter((e) => e.amount > 0 && e.type)

  const item: Record<string, unknown> = {
    id: form.id, displayName: form.displayName, iconKey: form.iconKey || form.id,
    kind: 'equipment', tier: form.tier, category: form.category, slotKind: form.slotKind,
    costGold: form.costGold,
  }
  if (form.description) item.description = form.description
  if (Object.keys(modifiers).length > 0) item.modifiers = modifiers
  if (elemental.length > 0) item.onHitElemental = elemental
  const onHit = procWireFromForm(form.onHit)
  if (onHit) item.onHitProc = onHit
  const onStruck = procWireFromForm(form.onStruck)
  if (onStruck) item.onStruckProc = onStruck

  return {
    item,
    recipe: form.crafting.enabled
      ? { inputs: [form.crafting.inputA, form.crafting.inputB], costGold: form.crafting.costGold }
      : null,
    availability: {
      ...form.availability,
      recipeList: form.availability.recipeList && form.crafting.enabled,
    },
  }
}
```

- [ ] **Step 6: Verify pass + commit**

Run: `cd client/src/game-portal && npm test && npm run build` → all green, typecheck clean.
```bash
git add client/src/game-portal/src/game/items/itemEditorApi.ts client/src/game-portal/src/game/items/itemEditorForm.ts client/src/game-portal/src/game/items/itemEditorForm.test.ts client/src/game-portal/src/game/items/itemEditorApi.test.ts
git commit -m "Add item editor service module + pure form transforms"
```

---

### Task 3: Icon gallery listing + server-URL fallback

**Files:**
- Modify: `client/src/game-portal/src/game/rendering/itemAssets.ts`
- Delete: `client/src/game-portal/src/game/rendering/itemCatalogImages.ts` (orphan, absorbed)
- Test: `client/src/game-portal/src/game/rendering/itemAssets.test.ts` (new)

**Interfaces:**
- Produces: `listItemAssetKeys(): string[]` (sorted, for the gallery); `getItemImageSourceUrl(iconKey: string): string` (bundled URL when the glob has the key, else the server `/catalog/items/{key}/image` URL — for `<img :src>` in the editor); `getItemAssetImage` gains a lazy server-URL fallback so IN-GAME rendering of uploaded icons works (miss → construct an `Image` pointed at the server URL, cache it, return it; canvas code already tolerates not-yet-loaded images via `img.complete` checks).

- [ ] **Step 1: Failing tests**

`itemAssets.test.ts`:
```ts
import { describe, expect, it } from 'vitest'
import { getItemImageSourceUrl, listItemAssetKeys } from './itemAssets'

describe('itemAssets gallery + fallback', () => {
  it('lists bundled keys sorted and non-empty', () => {
    const keys = listItemAssetKeys()
    expect(keys.length).toBeGreaterThan(10)
    expect([...keys].sort()).toEqual(keys)
    expect(keys).toContain('fire_sword')
  })
  it('bundled key resolves to a bundled URL, unknown key to the server route', () => {
    expect(getItemImageSourceUrl('fire_sword')).not.toContain('/catalog/items/')
    expect(getItemImageSourceUrl('brand_new_upload')).toBe('/catalog/items/brand_new_upload/image')
  })
})
```
(Note: `import.meta.glob` works under vitest with the vite config; if the glob resolves empty under the test env, check `vite.config.ts` test settings before assuming a code bug.)

- [ ] **Step 2: Verify failure** — `npm test` → new file fails (exports missing).

- [ ] **Step 3: Implement** (append to itemAssets.ts):
```ts
const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

// listItemAssetKeys returns every bundled icon key, sorted — the item
// editor's gallery picker enumerates these.
export function listItemAssetKeys(): string[] {
  return [...images.keys()].sort()
}

// getItemImageSourceUrl resolves an iconKey to an <img src>: the bundled
// asset URL when the build contains it, else the server-served uploaded-icon
// route (finishing the orphaned itemCatalogImages.ts stub, now deleted).
export function getItemImageSourceUrl(iconKey: string): string {
  const key = iconKey.toLowerCase()
  const bundled = urlsByKey.get(key)
  if (bundled) return bundled
  return `${API_BASE}/catalog/items/${encodeURIComponent(key)}/image`
}
```
This requires keeping the glob's URL per key: alongside the existing `images` map population loop, also fill `const urlsByKey = new Map<string, string>()` with `urlsByKey.set(match[1].toLowerCase(), url)`.

In-game fallback — extend `getItemAssetImage`:
```ts
// serverIconCache holds lazily-created Images for iconKeys with no bundled
// asset (editor-uploaded icons served by the Go server). Canvas callers
// already guard on img.complete/naturalWidth, so a still-loading image is
// safe to return.
const serverIconCache = new Map<string, HTMLImageElement>()

export function getItemAssetImage(iconKey: string): HTMLImageElement | null {
  const key = iconKey.toLowerCase()
  const bundled = images.get(key)
  if (bundled) return bundled
  const cached = serverIconCache.get(key)
  if (cached) return cached
  const img = loadImage(`${API_BASE}/catalog/items/${encodeURIComponent(key)}/image`)
  serverIconCache.set(key, img)
  return img
}
```
CAREFUL: `hasItemAsset` must keep its BUNDLED-only meaning (recipe-icon fallback logic depends on it) — do not touch it. Check `getItemAssetImage`'s existing callers (`grep -rn "getItemAssetImage" client/src/game-portal/src`) for any that treat `null` as "render placeholder": with the fallback they now receive a maybe-never-loading Image for genuinely bogus keys; confirm each call site guards on `complete`/`naturalWidth` (the CanvasRenderer pattern) — report any that doesn't and guard it.
Delete `itemCatalogImages.ts`.

- [ ] **Step 4: Verify pass + commit**

Run: `npm test && npm run build` → green.
```bash
git add client/src/game-portal/src/game/rendering/itemAssets.ts client/src/game-portal/src/game/rendering/itemAssets.test.ts
git rm client/src/game-portal/src/game/rendering/itemCatalogImages.ts
git commit -m "Icon gallery listing + server-URL fallback; absorb itemCatalogImages orphan"
```

---

### Task 4: Menu entry, route, view shell

**Files:**
- Modify: `client/src/game-portal/src/views/MainMenu.vue:97-110` (DEFAULTS entries + respaced tops)
- Modify: `client/src/game-portal/src/router/index.ts` (route + import)
- Create: `client/src/game-portal/src/views/ItemEditor.vue`
- Create: `client/src/game-portal/src/components/ItemEditorPanel.vue` (STUB in this task — renders a placeholder `<div>`; Task 5 fills it — so the route works end-to-end immediately)
- Test: `client/src/game-portal/src/views/MainMenu.entries.test.ts` (new)

**Interfaces:**
- Produces: route `/item-editor`; `ItemEditor.vue` shell (ExitButton + `<ItemEditorPanel />`); five menu entries in order Start Game / Profile / Map Editor / Item Editor / Settings.

- [ ] **Step 1: Failing test**

`MainMenu.entries.test.ts` (pure data test — import the component module and inspect DEFAULTS via a named export; add `export const MENU_ENTRIES = DEFAULTS.entries` to MainMenu.vue's script for testability):
```ts
import { describe, expect, it } from 'vitest'
import { MENU_ENTRIES } from '@/views/MainMenu.vue'

describe('main menu entries', () => {
  it('has five entries with Item Editor between Map Editor and Settings', () => {
    const labels = MENU_ENTRIES.map((e) => e.label)
    expect(labels).toEqual(['Start Game', 'Profile', 'Map Editor', 'Item Editor', 'Settings'])
    const editor = MENU_ENTRIES.find((e) => e.label === 'Item Editor')
    expect(editor?.to).toBe('/item-editor')
    // tops strictly increasing within the sign span
    const tops = MENU_ENTRIES.map((e) => e.top)
    expect([...tops].sort((a, b) => a - b)).toEqual(tops)
    expect(tops[0]).toBeCloseTo(55.97, 1)
    expect(tops[4]).toBeCloseTo(75.25, 1)
  })
})
```

- [ ] **Step 2: Verify failure** — `npm test` → fails (no MENU_ENTRIES export / 4 entries).

- [ ] **Step 3: Implement**

MainMenu.vue DEFAULTS entries become (same span, even 4.82 steps — NOTE the art caveat from Global Constraints; leave a comment):
```ts
  entries: [
    // Five entries respaced across the four-plank sign art: labels no longer
    // align 1:1 with planks until main-menu.png gains a fifth plank. Re-tune
    // with the sign tuner (localStorage 'webrts.signTuner'='1' + backtick).
    { label: 'Start Game', to: '/war-room', top: 55.97 },
    { label: 'Profile', to: '/profile', top: 60.79 },
    { label: 'Map Editor', to: '/editor', top: 65.61 },
    { label: 'Item Editor', to: '/item-editor', top: 70.43 },
    { label: 'Settings', to: '/options', top: 75.25 },
  ] as Entry[],
```
and add `export const MENU_ENTRIES = DEFAULTS.entries` (script setup: use a separate `<script lang="ts">` block for the named export, the standard Vue pattern for exporting constants from an SFC).

Router: `import ItemEditor from '@/views/ItemEditor.vue'` + `{ path: '/item-editor', component: ItemEditor },` directly under the `/editor` route.

`ItemEditor.vue` (mirror Editor.vue exactly):
```vue
<template>
  <div class="item-editor-view">
    <div class="item-editor-topbar item-editor-topbar--right">
      <ExitButton @click="router.push('/')" />
    </div>
    <ItemEditorPanel />
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import ItemEditorPanel from '@/components/ItemEditorPanel.vue'
import ExitButton from '@/components/ui/ExitButton.vue'

const router = useRouter()
</script>

<style scoped>
.item-editor-view {
  position: relative;
  z-index: 1;
  width: 100%;
  height: 100%;
  min-height: 0;
  min-width: 0;
  display: flex;
  overflow: hidden;
}
.item-editor-topbar {
  position: absolute;
  top: 16px;
  z-index: 20;
}
.item-editor-topbar--right {
  right: 16px;
}
</style>
```
`ItemEditorPanel.vue` stub: `<template><div class="item-editor-panel">Item Editor — under construction</div></template>` (Task 5 replaces it).

- [ ] **Step 4: Verify pass + commit**

Run: `npm test && npm run build` → green.
```bash
git add client/src/game-portal/src/views/MainMenu.vue client/src/game-portal/src/views/MainMenu.entries.test.ts client/src/game-portal/src/router/index.ts client/src/game-portal/src/views/ItemEditor.vue client/src/game-portal/src/components/ItemEditorPanel.vue
git commit -m "Add Item Editor menu entry, route, and view shell"
```

---

### Task 5: `ItemEditorPanel.vue`

**Files:**
- Rewrite: `client/src/game-portal/src/components/ItemEditorPanel.vue` (the Task-4 stub becomes the real panel)

**Interfaces:**
- Consumes: everything from Tasks 1-4 — `fetchItemDefs`, `fetchRecipeDefs` (catalog.ts), `fetchProcEffectDefs`, `fetchItemAvailability`, `saveEditorItem`, `deleteEditorItem`, `uploadItemIcon`, `EditorValidationError`, `itemIconUrl` (itemEditorApi.ts), `createBlankForm`/`formFromDef`/`saveRequestFromForm` (itemEditorForm.ts), `listItemAssetKeys`/`getItemImageSourceUrl` (itemAssets.ts), `TIER_COLORS` (itemRules.ts), `UiButton`.
- Produces: the working editor page. No props/emits (self-contained, like the stub).

**Component specification** (the implementer builds this as ONE SFC mirroring MapEditorPanel's conventions — accordion `editor-section` blocks, `control-group` rows, raw inputs; ~600-800 lines is expected):

**Script setup structure (complete logic skeleton — flesh out with the markup below):**
```ts
import { computed, onMounted, reactive, ref } from 'vue'
import UiButton from '@/components/ui/UiButton.vue'
import { fetchItemDefs, fetchRecipeDefs } from '@/game/maps/catalog'
import type { ItemDef } from '@/game/maps/itemDefs'
import { EditorValidationError, deleteEditorItem, fetchItemAvailability, fetchProcEffectDefs, itemIconUrl, saveEditorItem, uploadItemIcon } from '@/game/items/itemEditorApi'
import type { ProcEffectDef } from '@/game/items/itemEditorApi'
import { createBlankForm, formFromDef, saveRequestFromForm } from '@/game/items/itemEditorForm'
import type { ItemEditorForm } from '@/game/items/itemEditorForm'
import { getItemImageSourceUrl, listItemAssetKeys } from '@/game/rendering/itemAssets'
import { TIER_COLORS } from '@/game/items/itemRules'

const items = ref<ItemDef[]>([])            // full catalog, refreshed after saves
const recipesByOutput = ref(new Map<string, { inputs: string[]; costGold: number }>())
const procEffects = ref<ProcEffectDef[]>([])
const loadError = ref('')
const search = ref('')
const selectedId = ref('')                  // '' = nothing selected
const form = ref<ItemEditorForm | null>(null)
const openSection = ref('identity')         // accordion state
const saving = ref(false)
const saveError = ref('')                   // EditorValidationError message shown beside Save
const saveOk = ref(false)
const galleryOpen = ref(false)
const galleryKeys = listItemAssetKeys()

const equipmentItems = computed(() =>
  items.value.filter((d) => d.kind === 'equipment' &&
    (search.value === '' || d.id.includes(search.value.toLowerCase()) || d.displayName.toLowerCase().includes(search.value.toLowerCase()))))
// group by tier for the sidebar; TIER_COLORS drives the badge color.

async function reloadCatalog() {
  const [defs, recipes] = await Promise.all([fetchItemDefs(), fetchRecipeDefs().catch(() => [])])
  items.value = defs
  const map = new Map<string, { inputs: string[]; costGold: number }>()
  for (const r of recipes) map.set(r.output, { inputs: r.inputs, costGold: r.costGold })
  recipesByOutput.value = map
}

onMounted(async () => {
  try {
    await reloadCatalog()
    procEffects.value = await fetchProcEffectDefs()
  } catch (err) {
    loadError.value = err instanceof Error ? err.message : String(err)
  }
})

async function selectItem(id: string) {
  const def = items.value.find((d) => d.id === id)
  if (!def) return
  selectedId.value = id
  saveError.value = ''
  saveOk.value = false
  const availability = await fetchItemAvailability(id).catch(() => ({
    marketplace: false, wanderingMerchant: false, lootTable: { enabled: false, weight: 10 }, recipeList: false }))
  form.value = formFromDef(def, availability, recipesByOutput.value.get(id) ?? null)
}

function newItem() {
  selectedId.value = ''
  saveError.value = ''
  saveOk.value = false
  form.value = createBlankForm()
}

async function save() {
  if (!form.value) return
  saving.value = true
  saveError.value = ''
  saveOk.value = false
  try {
    await saveEditorItem(saveRequestFromForm(form.value))
    saveOk.value = true
    form.value.isNew = false
    selectedId.value = form.value.id
    await reloadCatalog()
  } catch (err) {
    saveError.value = err instanceof EditorValidationError ? err.serverMessage
      : err instanceof Error ? err.message : String(err)
  } finally {
    saving.value = false
  }
}

async function removeOrReset() {
  if (!form.value || form.value.isNew) return
  try {
    const status = await deleteEditorItem(form.value.id)
    await reloadCatalog()
    if (status === 'deleted') newItem()
    else await selectItem(form.value.id) // reset: reload the embedded version
  } catch (err) {
    saveError.value = err instanceof Error ? err.message : String(err)
  }
}

async function onIconFileChosen(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file || !form.value) return
  if (form.value.isNew || !form.value.id) {
    saveError.value = 'Save the item once before uploading an icon.'
    return
  }
  try {
    await uploadItemIcon(form.value.id, file)
    form.value.iconKey = form.value.id // server forces iconKey to the id
    await reloadCatalog()
  } catch (err) {
    saveError.value = err instanceof Error ? err.message : String(err)
  } finally {
    input.value = ''
  }
}

function pickGalleryIcon(key: string) {
  if (form.value) form.value.iconKey = key
  galleryOpen.value = false
}

function toggleSection(key: string) {
  openSection.value = openSection.value === key ? '' : key
}
// selectedOverridden: computed — items.find(selectedId)?.overridden (needs
// `overridden?: boolean` added to the client ItemDef type — one-line change
// in itemDefs.ts, part of this task). Sidebar badge comment: in dev builds
// every item reports overridden (writable dir == embed source) — expected.
```

**Template layout (two-pane; markup pattern per section — replicate the `control-group` idiom for every field listed):**
```vue
<template>
  <div class="item-editor-panel">
    <aside class="item-editor-sidebar">
      <div class="sidebar-actions">
        <UiButton size="sm" @click="newItem">New Item</UiButton>
        <input v-model="search" type="text" placeholder="Search items…" aria-label="Search items" />
      </div>
      <div class="sidebar-list">
        <!-- group equipmentItems by tier; per item: icon <img :src="getItemImageSourceUrl(d.iconKey)">,
             displayName colored by TIER_COLORS[d.tier], overridden dot when d.overridden
             (dev note: all items show it in dev builds — see comment), click → selectItem(d.id),
             selected state via a class bound to selectedId === d.id -->
      </div>
    </aside>

    <section v-if="form" class="item-editor-main">
      <!-- Section 1: Identity (open by default) -->
      <section class="editor-section" :class="{ 'editor-section--open': openSection === 'identity' }">
        <button class="editor-section__summary" type="button" @click="toggleSection('identity')">Identity</button>
        <div v-if="openSection === 'identity'" class="editor-section__body">
          <div class="control-group">
            <label for="ie-id">ID <span class="field-hint">(lowercase, digits, underscores; locked after save)</span></label>
            <input id="ie-id" v-model.trim="form.id" type="text" :disabled="!form.isNew" />
          </div>
          <!-- displayName (text), description (textarea rows=3), tier (select: common/uncommon/rare/epic/legendary),
               category (select: Weapon/Armor/Shield/Accessory), slotKind (select: any/weapon/armor/accessory) -->
        </div>
      </section>

      <!-- Section 2: Icon -->
      <!--  preview <img :src="getItemImageSourceUrl(form.iconKey || form.id)" class="icon-preview">
            + UiButton "Choose from gallery" → galleryOpen=true
            + <input type="file" accept="image/png" @change="onIconFileChosen"> styled as a labeled control
            + gallery overlay when galleryOpen: grid of <button> per galleryKeys entry with <img> + key caption,
              click → pickGalleryIcon(key); a Close UiButton. -->

      <!-- Section 3: Stats — number inputs (v-model.number) for the 7 flat mods,
           dodgePct/blockPct labeled "Dodge Chance %" / "Block Chance %" (min 0 max 99) -->

      <!-- Section 4: Elemental — v-for rows over form.elemental:
           select type (fire/cold/lightning/holy/shadow/physical) + number amount + remove button;
           "Add elemental damage" UiButton pushes {type:'fire', amount:5} -->

      <!-- Section 5: Procs — two identical sub-blocks (form.onHit "On hit" / form.onStruck "When struck"):
           enable checkbox; when enabled: effect select over procEffects (option label: `${p.id} — ${p.damage} ${p.damageType}`),
           chancePct number (1..100), collapsible "Overrides" fieldset with the 9 nullable number inputs —
           each input placeholder shows the selected effect's base value (find procEffects by form.onX.effect),
           empty input ⇒ null ⇒ inherit. Use a small helper `numOrNull` on @change since v-model.number yields 0 for cleared inputs:
           bind :value + @input="p.damage = ev.target.value === '' ? null : Number(ev.target.value)" pattern (write a tiny
           local function `bindNullable(p, key)` or inline handlers). -->

      <!-- Section 6: Cost & Availability — costGold number; checkboxes marketplace / wanderingMerchant;
           lootTable.enabled checkbox + weight number (1..90, hint "share of the merchant roll");
           recipeList checkbox :disabled="!form.crafting.enabled" with hint "(requires crafting)" -->

      <!-- Section 7: Crafting — enabled checkbox; when on: two selects (inputA/inputB) over equipmentItems
           (option: displayName (id)), craft costGold number -->

      <div class="editor-actions">
        <UiButton :disabled="saving || !form.id" @click="save">{{ saving ? 'Saving…' : 'Save' }}</UiButton>
        <UiButton v-if="!form.isNew" size="sm" @click="removeOrReset">
          {{ selectedOverridden ? (isEmbedded ? 'Reset to default' : 'Delete') : 'Delete' }}
        </UiButton>
        <span v-if="saveError" class="save-error" role="alert">{{ saveError }}</span>
        <span v-else-if="saveOk" class="save-ok">Saved ✓</span>
      </div>
    </section>
    <section v-else class="item-editor-main item-editor-main--empty">
      <p v-if="loadError" role="alert">{{ loadError }}</p>
      <p v-else>Select an item or create a new one.</p>
    </section>
  </div>
</template>
```
`isEmbedded` distinction: the wire has no "embedded" flag — approximate with `overridden` + delete-status feedback (the DELETE response says `deleted` vs `reset`; after the call, act on the returned status as the code above does). The button label may simply read "Delete / Reset" — implementer's judgment, note the choice.

**Styles:** scoped; two-pane flex (sidebar 280px, main flex-1, both `overflow-y: auto`); replicate MapEditorPanel's `.editor-section*` and `.control-group` styles LOCALLY (they're scoped to MapEditorPanel and not shared — copy the ~40 lines rather than de-scoping them; note the duplication as accepted convention-following). NO `cursor` declarations. `.save-error { color: #fca5a5 }`, `.save-ok { color: #86efac }`.

- [ ] **Step 1: Implement the panel per the specification above** (no new unit tests — the logic lives in the already-tested pure modules; repo convention doesn't component-test panels; `npm run build`'s vue-tsc is the type gate).

- [ ] **Step 2: Type + suite gate**

Run: `cd client/src/game-portal && npm test && npm run build`
Expected: green + clean. Also add `overridden?: boolean` to `ItemDef` in `itemDefs.ts` (this task).

- [ ] **Step 3: Manual end-to-end against the dev stack**

Start the Go server (`cd server && go run ./cmd/api`) and vite (`cd client/src/game-portal && npm run dev`), open the SPA → Item Editor:
1. Create a new item (id `manual_probe`, stats, a proc, marketplace on) → Save → "Saved ✓"; confirm the JSON landed: `git status` shows the new catalog file.
2. Select `fire_shield` → form populates incl. availability + recipe.
3. Upload any small PNG to `manual_probe` → icon preview switches to the server URL.
4. Delete `manual_probe` → returns to blank; `git status` shows the file gone; REVERT any remaining catalog membership edits (`git checkout -- server/internal/game/catalog/`) before committing.
Record what you did in the report. If any step fails on the SERVER side, stop and report; UI defects: fix.

- [ ] **Step 4: Commit**

```bash
git checkout -- server/internal/game/catalog/ 2>/dev/null || true
git add client/src/game-portal/src/components/ItemEditorPanel.vue client/src/game-portal/src/game/maps/itemDefs.ts
git commit -m "Implement ItemEditorPanel: sidebar, form sections, save/delete/icon flows"
```

---

### Task 6: Verification sweep

**Files:** fixes only.

- [ ] **Step 1: Full gates** — `cd client/src/game-portal && npm test && npm run build`; `cd server && go vet ./... && go build ./... && go test ./... -count=1 2>&1 | grep -E "^(--- FAIL|FAIL)"` → nothing beyond known flaky.
- [ ] **Step 2: Catalog hygiene** — `git status` must show no stray `server/internal/game/catalog/` modifications from manual testing; revert any.
- [ ] **Step 3: Commit fixes if any** — `git add -A client/ server/ && git commit -m "Item editor UI: verification fixes"` (skip if clean).
