# Unit-Types Editor v2 — Phase 5b (Path Editor UI) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make promotion **paths** first-class authorable entities in the unit editor — a faction-filtered tree where each base unit expands to its paths, a "Path Unit" creation mode with rank grid + perk-pool authoring, reusing the already-path-aware sprite preview and art ingest.

**Architecture:** Mirror the existing `unitEditorForm.ts` lossless round-trip (`MODELED_KEYS` + remainder bag) for a new `AuthoredPathDef`. Add a parallel `pathEditorApi.ts` mirroring `unitEditorApi.ts`. Restructure the panel's flat unit list into a tree and add a Base/Path mode toggle. The rank grid renders inherited-base × multiplier = result so authors never edit multipliers blind. Perk pools show each perk's server-supplied `wired`/`inert` label.

**Tech Stack:** Vue 3 `<script setup>` + TypeScript, Vitest + happy-dom.

**DEPENDS ON Phase 5a** — needs the routes: `GET /catalog/paths` (full merged), `POST /paths`, `DELETE /paths/{id}`, `POST /perks`, `DELETE /perks/{unit}/{path}/{rank}`, and `Wired` on `/catalog/perks`. Do not start until 5a's routes exist.

**Reference spec:** `docs/superpowers/specs/2026-07-13-unit-types-editor-v2-design.md` §7.1 (author-facing tree + sections table), §7.3 (wired/inert UI honesty).

**Global gates (from `client/src/game-portal`):**
- Typecheck: `npx vue-tsc -b` (build mode — `--noEmit` false-cleans).
- Build: `npm run build`
- Test: `npm run test`
- No literal `cursor:` in new component CSS except `cursor: not-allowed` on forbidden-action states.
- Do NOT modify the item editor or the old map editor (`MapEditorPanel.vue`, `views/Editor.vue`). The world-editor unit panel IS the one edited.

**Reference facts established by recon (do not re-derive):**
- Form round-trip: `unitEditorForm.ts` — `AuthoredUnitDef` typed superset + `[key:string]: any` (lines 6-52), `MODELED_KEYS` (56-64, includes `'pathChances'`), `remainder` bag (66-68), `createBlankForm`/`formFromDef`/`saveRequestFromForm` (104-126). Round-trip test at `unitEditorForm.test.ts`.
- Panel: `UnitTypeEditorPanel.vue` (1204 lines). Faction pills (template 4-43, `factionFilter` :389, `unitCountByFaction` :405-412). Flat left list (template 55-69, `visibleUnits` :395-399, `units` ref :367). `selectUnit` :764-782, `newUnit` :814-828 (`selectedType===null` = new discriminator), `save` :830-844, `removeUnit` :846-860. Accordion `openSections` reactive Set :480, `toggleSection` :481-484. Map-row mirror pattern (`MapRow`, `rowsFromMap`/`mapFromRows`, `pathChancesRows`) :486-508.
- Client path model (`unitDefs.ts`): `PATHS_BY_UNIT_TYPE_MAP` (:153, unit→path ids) + `initPathsByUnitType` (:155), `PATH_BOUNDS_MAP` (:142). No `AuthoredPathDef` type exists yet. `pathChances` is a `Record<string,number>` on the authored unit.
- Client API: `unitEditorApi.ts` — `EditorValidationError` (11-18, `.serverMessage`), `fetchAuthoredUnitDefs` (22-27), `saveEditorUnit` (29-40), `deleteEditorUnit` (42-47). `editorCatalogApi.ts` — `throwIfValidationFailed` (33-37), `fetchFactions`, `saveUnitArt` (97-113, **already accepts `path?`**). `catalog.ts` `fetchUnitDefs` (170-191) already returns `{units, paths, pathsByUnit}`.
- Sprite preview: `UnitSpritePreview.vue` props include `pathKey?` (:114-124); **path always wins over unitKey** in bounds/sprite/portrait resolution. Panel currently passes only `:unit-key` (:76). For a path form, pass `:path-key="pathForm.path" :unit-key="pathForm.parentUnit"`.
- Toolbar flags: `worldEditorToolbar.test.ts` pins `enabled` for `unit-paths`/`perks` — flipping either to `true` requires updating that test.

---

## File Structure

**New files (`client/src/game-portal/src/`):**
- `game/units/pathEditorForm.ts` — `AuthoredPathDef`, `PathEditorForm`, `MODELED_PATH_KEYS`, round-trip trio.
- `game/units/pathEditorApi.ts` — `fetchPaths`/`savePath`/`deletePath`/`savePerks`/`deletePerks`, `PathDefFull`/`PerkEntry` types.
- `game/units/pathEditorForm.test.ts`, `pathEditorApi.test.ts` — tests.
- `components/PathRankGrid.vue` — the 3×N rank grid with inherited×mult=result.
- `components/PerkPoolEditor.vue` — bronze/silver/gold pool lists with wired/inert labels.

**Modified files:**
- `components/UnitTypeEditorPanel.vue` — tree list, Base/Path mode toggle, path-form sections, save/delete wiring, "Add Path" action.
- `game/units/editorCatalogApi.ts` — add `fetchArchetypes`-style `fetchPerkCatalog` if not present (for wired flags + pool pickers).

---

## Task 1: `pathEditorForm.ts` — lossless path form round-trip

**Files:**
- Create: `client/src/game-portal/src/game/units/pathEditorForm.ts`
- Test: `client/src/game-portal/src/game/units/pathEditorForm.test.ts`

- [ ] **Step 1: Write failing round-trip tests.** Mirror `unitEditorForm.test.ts`: `formFromDef(def)` then `saveRequestFromForm(form)` deep-equals the original def for a `cleric`-shaped def carrying an unknown key (proves remainder preservation), and preserves the undefined-vs-0 distinction on multiplier fields.

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Implement** the trio verbatim from `unitEditorForm.ts`'s pattern:

```ts
export interface AuthoredPathDef {
  path?: string
  parentUnit?: string      // client-only: owning unit type (not persisted in the file; sent alongside)
  description?: string
  visionRange?: number
  projectile?: string
  damageType?: string
  attackType?: string
  projectileScale?: number
  abilities?: string[]     // replace-list
  channelLoop?: { start: number; end: number }
  bounds?: unknown
  ranks?: Record<'bronze' | 'silver' | 'gold', PathRankStats>
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  [key: string]: any
}
export interface PathRankStats {
  maxHPMultiplier?: number; maxMPMultiplier?: number; healthRegenMultiplier?: number
  damageMultiplier?: number; attackSpeedMultiplier?: number; moveSpeedMultiplier?: number
  attackRange?: number; attackRangeMultiplier?: number
  armor?: number; dodgeChance?: number; blockChance?: number
}
const MODELED_PATH_KEYS = ['path','description','visionRange','projectile','damageType',
  'attackType','projectileScale','abilities','channelLoop','bounds','ranks'] as const
export interface PathEditorForm extends AuthoredPathDef { remainder: Record<string, unknown> }
export function createBlankPathForm(parentUnit: string): PathEditorForm { … }
export function pathFormFromDef(def: AuthoredPathDef, parentUnit: string): PathEditorForm { … }
export function saveRequestFromPathForm(form: PathEditorForm): { unit: string; path: AuthoredPathDef } { … }
```
`parentUnit` is a client-only routing field — split it out in `saveRequestFromPathForm` into the POST `{ unit, path }` shape, and never let it leak into `remainder` or the persisted file.

- [ ] **Step 4: Run** round-trip tests → PASS. `npx vue-tsc -b`.

- [ ] **Step 5: Commit.**

---

## Task 2: `pathEditorApi.ts` — path + perk CRUD client

**Files:**
- Create: `client/src/game-portal/src/game/units/pathEditorApi.ts`
- Test: `client/src/game-portal/src/game/units/pathEditorApi.test.ts`

- [ ] **Step 1: Write failing tests** with a mocked `fetch`: `fetchPaths()` GETs `/catalog/paths` and returns the array; `savePath({unit,path})` POSTs `/paths` and throws `EditorValidationError` on a 400 `validation_failed` body; `deletePath(id)` DELETEs `/paths/{id}`; `savePerks({unit,path,rank,perks})` POSTs `/perks`; `fetchPerkCatalog()` GETs `/catalog/perks` and surfaces `wired`.

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Implement** mirroring `unitEditorApi.ts` exactly (reuse `EditorValidationError` + `throwIfValidationFailed`):

```ts
export interface PathDefFull extends AuthoredPathDef { /* server merged shape, incl. parentUnit resolved */ }
export interface PerkEntry { id: string; displayName?: string; wired: boolean; unitType?: string; path?: string; rank?: string /* … */ }
export async function fetchPaths(): Promise<PathDefFull[]> { /* GET /catalog/paths */ }
export async function savePath(req: { unit: string; path: AuthoredPathDef }): Promise<void> { /* POST /paths */ }
export async function deletePath(id: string): Promise<{ status: string }> { /* DELETE /paths/{id} */ }
export async function savePerks(req: { unit: string; path: string; rank: string; perks: PerkEntry[] }): Promise<void> { /* POST /perks */ }
export async function deletePerks(unit: string, path: string, rank: string): Promise<{ status: string }> { /* DELETE /perks/{unit}/{path}/{rank} */ }
export async function fetchPerkCatalog(): Promise<PerkEntry[]> { /* GET /catalog/perks */ }
```

- [ ] **Step 4: Run** tests → PASS. `npx vue-tsc -b`.

- [ ] **Step 5: Commit.**

---

## Task 3: Panel — tree list + Base/Path creation mode

**Files:**
- Modify: `client/src/game-portal/src/components/UnitTypeEditorPanel.vue`
- Test: `client/src/game-portal/src/components/UnitTypeEditorPanel.tree.test.ts` (new)

- [ ] **Step 1: Write a failing component test.** Mount the panel with mocked API returning one unit `archer` with paths `['marksman','trapper']`. Assert: the tree renders `archer` with an expand affordance; expanding shows `marksman`/`trapper` child rows; clicking `+ New` shows a Base/Path choice; choosing "Path Unit" requires selecting a parent unit before the path form's Save enables.

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Implement.**
  - Add `paths` ref (`PathDefFull[]`) + `pathsByUnit` (from `fetchPaths()`/`PATHS_BY_UNIT_TYPE_MAP`), loaded in `reload()`.
  - Convert the flat `<ul>` (template 58-68) to a tree: each `visibleUnits` row gets an expand toggle; when expanded, render its path child rows (from `pathsByUnit[u.type]`). Selecting a child enters **path-edit mode**.
  - Add an `editorMode` ref: `'unit' | 'path'`. Add `selectedPath` ref (path id or null). The unit panel's `selectedType===null` discriminator is preserved for unit mode; path mode uses `(parentUnit, selectedPath)`.
  - Replace `+ New Unit` (template 56) with a `+ New ▾` that offers "Base Unit" (existing `newUnit()`) and "Path Unit" (`newPath()` — asks for parent unit via a small select seeded from `visibleUnits`, then `pathForm.value = createBlankPathForm(parent)`, `editorMode='path'`, `selectedPath=null`).
  - Path form Save disabled until `pathForm.parentUnit && pathForm.path`.

- [ ] **Step 4: Run** tree test + full client suite → PASS. `npx vue-tsc -b`.

- [ ] **Step 5: Commit.**

---

## Task 4: Path form sections + rank grid + perk pools

**Files:**
- Create: `client/src/game-portal/src/components/PathRankGrid.vue`, `components/PerkPoolEditor.vue`
- Modify: `UnitTypeEditorPanel.vue`
- Test: `components/PathRankGrid.test.ts`

- [ ] **Step 1: Write a failing test for `PathRankGrid`.** Given `baseStats` (e.g. `damage: 18`) and a `ranks` model with `bronze.damageMultiplier = 1.75`, the grid renders the resolved value `31.5` (or rounded per game convention) next to the multiplier input, and emits `update:ranks` on edit. Assert the inherited base and result both show — the §7.1 "editing multipliers blind" guard.

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Implement.**
  - `PathRankGrid.vue`: props `baseStats` (from the parent unit def) + `ranks` model; 3 rows (bronze/silver/gold) × the 11 rank columns (`maxHPMultiplier`, `maxMPMultiplier`, `healthRegenMultiplier`, `damageMultiplier`, `attackSpeedMultiplier`, `moveSpeedMultiplier`, `attackRange` flat, `attackRangeMultiplier`, `armor`, `dodgeChance`, `blockChance`). Each multiplier cell shows `base × mult = result` inline. `v-model:ranks`. Preserve undefined-vs-0 (raw `@input`, not `v-model.number`, matching the `healthRegenRate` precedent).
  - **Attack-range conflict (decided):** when a rank row has BOTH a flat `attackRange` override and an `attackRangeMultiplier`, the grid renders **both** computed values and shows a caution that the flat override wins in-game (this matches `applyRankModifiersLocked`'s precedence — flat wins). Do not silently hide the multiplier's would-be result; surface the conflict so the author sees what the sim will actually use.
  - `PerkPoolEditor.vue`: props `unit`, `path`, the three pools + the perk catalog (`fetchPerkCatalog`). Per rank: add/remove/reorder perk ids from a picker; each perk shows a **wired** badge or an **inert** caution ("config-only — no Go handler yet; grants nothing in a match"). Saving a pool calls `savePerks`. Empty pools are valid (say so, don't error).
  - In the panel, add path-mode sections (reuse the accordion): **Identity** (path id, parent unit locked after create, description), **Overlay** (visionRange, projectile picker, damageType, attackType, projectileScale, abilities replace-list, channelLoop, bounds), **Rank grid** (`PathRankGrid`), **Perk pools** (`PerkPoolEditor`), **Art** (`UnitSpritePreview` with `:path-key="pathForm.path" :unit-key="pathForm.parentUnit"` + the existing art drop zone passing `path` to `saveUnitArt`), **Attack Origin** (existing per-facing editor bound to the path's `attackOrigin`). Hide unit-only sections (Cost, Gating/pathChances, Rewards, Flags) in path mode.

- [ ] **Step 4: Run** rank-grid test + full suite → PASS. `npx vue-tsc -b`, `npm run build`.

- [ ] **Step 5: Commit.**

---

## Task 5: Save/delete wiring + "Add Path" ordering

**Files:**
- Modify: `UnitTypeEditorPanel.vue`
- Test: `components/UnitTypeEditorPanel.pathSave.test.ts` (new)

- [ ] **Step 1: Write a failing test.** In path mode, editing the form and clicking Save calls `savePath` with `saveRequestFromPathForm(pathForm)`; a 400 `validation_failed` surfaces `EditorValidationError.serverMessage` inline. The base-unit "Add Path" action calls `savePath` **first**, then adds the pathChances row to the parent unit and saves the unit (order asserted — path write precedes the pathChances reference, per §9.1).

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Implement.**
  - `savePath()` panel method: `await savePath(saveRequestFromPathForm(pathForm.value))` → `reload()` → re-select the path → refresh preview. Errors → `saveError`.
  - `removePath()`: `await deletePath(selectedPath.value)` → `reload()`. A 400 (path still referenced) surfaces the server message listing referencing units.
  - "Add Path to `<unit>`" affordance on a base unit: creates the path (writes the file), then adds `{ [pathId]: 1 }` to the unit's `pathChances` and saves the unit — **path first, then reference**. Never the reverse (that's the boot-panic ordering §9.1 forbids).
  - After art save in path mode, re-register the runtime overlay so the path renders in playtest without rebuild (reuse the existing `flushPreviewOverlay`/`loadRuntimeSpriteSets` path, now keyed by path).

- [ ] **Step 4: Run** save test + full suite → PASS. `npx vue-tsc -b`, `npm run build`.

- [ ] **Step 5: Commit.**

---

## Task 6: Enable the toolbar categories

**Files:**
- Find + modify: the world-editor toolbar config enabling `unit-paths` / `perks`
- Modify: `worldEditorToolbar.test.ts` (update pinned `enabled` flags)

- [ ] **Step 1:** Flip `unit-paths` (and `perks` if it's a separate category) to `enabled: true` in the toolbar config. Update the pinned expectation in `worldEditorToolbar.test.ts`.
- [ ] **Step 2: Run** `npm run test` → PASS. `npx vue-tsc -b`, `npm run build`.
- [ ] **Step 3: Commit.**

---

## Final review + E2E milestone (§10)

After all 6 tasks, dispatch a final reviewer, then run the spec's E2E milestone #5 manually against live dev servers (isolated `UNIT_CATALOG_DIR`/`UNIT_ASSETS_DIR`, never the source tree):

> Add a path to a base unit → set the rank grid → assign perk pools → playtest, rank the unit to Bronze, confirm the path rolls, the multipliers apply, and a perk is granted.

This closes the north star for paths: a path taken from nothing to shipped entirely in the editor, no code changes. Perks remain the one documented gap (§12) — a perk with no Go handler is labelled inert and grants nothing, by design.
