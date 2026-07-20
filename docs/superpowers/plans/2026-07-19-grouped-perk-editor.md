# Grouped Perk Editor (Unit → Path) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Replace the perk editor's flat perk list with a Unit → Path → perks (+ Generic) grouped sidebar, and let "New Perk" pick a target association so it saves into the right `catalog/perks/<path|generic>/` folder.

**Architecture:** Pure frontend. The backend is already association-aware — `SavePerkDef` writes under `<assoc>/<id>/` based on `PerkDef.Path` (Task C4), and `path` is a modeled key in the perk form so `saveRequestFromForm` already sends it. This plan only changes `PerkEditorPanel.vue` (grouping + a create-time association picker), reusing `fetchUnitDefs().pathsByUnit` for the Unit↔Path topology.

**Tech Stack:** Vue 3 + TypeScript, Vitest. Client at `client/src/game-portal`. Type-check with `npx vue-tsc -b` (build mode — `--noEmit` false-cleans in this repo).

**Scope (locked with user):** Group the sidebar + create-into-group. Existing perks' association stays **read-only** (no moving a perk between folders in-editor).

---

## Context an implementer needs

- Perk editor component: `client/src/game-portal/src/components/PerkEditorPanel.vue`. Sidebar is a flat `<ul><li v-for="p in perks">` ([lines 3-19](client/src/game-portal/src/components/PerkEditorPanel.vue#L3-L19)). `perks` is `ref<AuthoredPerkDef[]>` loaded via `fetchAuthoredPerkDefs()` in `reload()` (onMounted). `newPerk()` resets `form` to `createBlankForm()`. `save()` calls `saveRequestFromForm(form.value)` → `saveEditorPerk(...)`.
- Each `AuthoredPerkDef` now has `path?: string` — its association (owning promotion path; `undefined`/`''` = generic), folder-derived by the server. This is what we group by.
- `fetchUnitDefs()` (`client/src/game-portal/src/game/maps/catalog.ts:179`) returns `{ units, paths, pathsByUnit: Record<string, string[]> }`. `pathsByUnit` maps unitType → its path ids. Invert it for path→unit; use it directly for the create-dropdown's Unit→Path options.
- `perkEditorForm.ts`: `path` is in `MODELED_KEYS`, and `saveRequestFromForm` emits modeled keys that are defined (drops `undefined`). So: set `form.path = '<pathId>'` for a path-associated perk, or leave it `undefined` for generic (the server's `perkAssocDir('')` → `generic`).
- Existing test file: `client/src/game-portal/src/components/PerkEditorPanel.test.ts` — read it to learn the fetch-stub + mount idiom, and extend it.

Run all client commands from `client/src/game-portal`.

---

## File Structure

- Modify: `client/src/game-portal/src/components/PerkEditorPanel.vue` — grouped sidebar + create-time association `<select>`; add a `pathsByUnit` fetch on mount and a `groupedPerks` computed.
- Test: `client/src/game-portal/src/components/PerkEditorPanel.test.ts` — grouping + create-into-group assertions.

No new files needed (the topology helper is a small local computed; if it grows, a `perkGrouping.ts` helper is acceptable, but prefer inline first).

---

## Task 1: Load path→unit topology and build the grouped model

**Files:**
- Modify: `PerkEditorPanel.vue` (script)
- Test: `PerkEditorPanel.test.ts`

- [ ] **Step 1: Write a failing test for the grouped model**

Read `PerkEditorPanel.test.ts` first to match its stub idiom (it stubs `fetch`/the perk API and mounts the component). Add a test that stubs the perks endpoint to return perks with `path` values (e.g. one `siphoner`, one `trapper`, one generic with no `path`) AND stubs `fetchUnitDefs` / `/catalog/units` to return `pathsByUnit: { acolyte: ['cleric','siphoner'], archer: ['marksman','trapper'] }`. Assert that after mount the rendered sidebar contains a group header for `Acolyte` and a sub-header for `Siphoner` above the siphoner perk, and a `Generic` group containing the generic perk. (Match however the existing tests query the DOM — data-test attributes or text.)

- [ ] **Step 2: Run it, verify it FAILS**

Run: `npx vitest run src/components/PerkEditorPanel.test.ts`
Expected: FAIL (no group headers rendered yet).

- [ ] **Step 3: Fetch `pathsByUnit` on mount and build path→unit**

In `PerkEditorPanel.vue` script: import `fetchUnitDefs` from `@/game/maps/catalog`. Add `const pathsByUnit = ref<Record<string, string[]>>({})` and a `const pathToUnit = computed(() => { const m = new Map<string,string>(); for (const [u, ps] of Object.entries(pathsByUnit.value)) for (const p of ps) m.set(p, u); return m })`. In `onMounted` (or `reload`), also load it:

```ts
onMounted(async () => {
  await reload()
  try {
    pathsByUnit.value = (await fetchUnitDefs()).pathsByUnit
  } catch {
    pathsByUnit.value = {} // non-fatal: fall back to an ungrouped/Generic-only view
  }
})
```

- [ ] **Step 4: Build the `groupedPerks` computed**

Produce a stable, sorted structure the template renders:

```ts
interface PerkGroup { unit: string; paths: Array<{ path: string; perks: AuthoredPerkDef[] }> }
// Units sorted alpha; paths within a unit sorted alpha; perks within a path sorted by id.
// Perks with no path (or an unknown path) collect under a synthetic 'generic' group,
// rendered LAST with unit label 'Generic'.
const groupedPerks = computed<PerkGroup[]>(() => {
  const byUnitPath = new Map<string, Map<string, AuthoredPerkDef[]>>()
  const generic: AuthoredPerkDef[] = []
  for (const p of perks.value) {
    const path = p.path ?? ''
    const unit = path ? pathToUnit.value.get(path) : undefined
    if (!path || !unit) { generic.push(p); continue }
    if (!byUnitPath.has(unit)) byUnitPath.set(unit, new Map())
    const paths = byUnitPath.get(unit)!
    if (!paths.has(path)) paths.set(path, [])
    paths.get(path)!.push(p)
  }
  const groups: PerkGroup[] = [...byUnitPath.entries()]
    .sort((a, b) => a[0].localeCompare(b[0]))
    .map(([unit, paths]) => ({
      unit,
      paths: [...paths.entries()]
        .sort((a, b) => a[0].localeCompare(b[0]))
        .map(([path, ps]) => ({ path, perks: ps.sort((x, y) => x.id.localeCompare(y.id)) })),
    }))
  if (generic.length) {
    groups.push({ unit: 'Generic', paths: [{ path: '', perks: generic.sort((x, y) => x.id.localeCompare(y.id)) }] })
  }
  return groups
})
```

- [ ] **Step 5: Render the grouped sidebar**

Replace the flat `<ul><li v-for="p in perks">` block with nested groups. Add simple collapse state (`const collapsed = ref(new Set<string>())`, keyed by `unit` and `unit+'/'+path`; a header click toggles). Keep the existing per-perk `<button data-test="perk-row" ...>` exactly as-is inside the innermost loop so existing selection behavior + tests keep working. Example structure:

```vue
<aside class="perk-editor__list">
  <button type="button" class="perk-editor__new" :disabled="busy" @click="newPerk()">+ New Perk</button>
  <p v-if="loadError" class="perk-editor__error">{{ loadError }}</p>
  <div v-for="group in groupedPerks" :key="group.unit" class="perk-editor__group">
    <h4 class="perk-editor__group-unit" @click="toggle(group.unit)">{{ group.unit }}</h4>
    <template v-if="!collapsed.has(group.unit)">
      <div v-for="pg in group.paths" :key="pg.path" class="perk-editor__group-path">
        <h5 v-if="pg.path" class="perk-editor__group-path-label" @click="toggle(group.unit + '/' + pg.path)">
          {{ pg.path }}
        </h5>
        <ul v-if="!collapsed.has(group.unit + '/' + pg.path)">
          <li v-for="p in pg.perks" :key="p.id">
            <button type="button" data-test="perk-row" :class="{ 'is-selected': p.id === selectedId }" @click="selectPerk(p)">
              {{ p.id }} <span v-if="p.displayName">— {{ p.displayName }}</span>
              <span v-if="!p.wired" class="perk-editor__badge perk-editor__badge--inert">inert</span>
            </button>
          </li>
        </ul>
      </div>
    </template>
  </div>
</aside>
```

Add a `function toggle(key: string) { const s = new Set(collapsed.value); s.has(key) ? s.delete(key) : s.add(key); collapsed.value = s }`. Add minimal scoped styles for `.perk-editor__group-unit` / `__group-path-label` (bold header, pointer affordance via existing cursor rules — do NOT write literal `cursor:` per project CSS convention).

- [ ] **Step 6: Run tests + type-check**

Run: `npx vitest run src/components/PerkEditorPanel.test.ts && npx vue-tsc -b`
Expected: PASS + clean. (Existing selection tests still pass because `data-test="perk-row"` is unchanged.)

- [ ] **Step 7: Commit**

```bash
git add client/src/game-portal/src/components/PerkEditorPanel.vue client/src/game-portal/src/components/PerkEditorPanel.test.ts
git commit -m "feat(perk-editor): group the perk list by Unit → Path (+ Generic)"
```

---

## Task 2: Create-into-group — pick association for a NEW perk

**Files:**
- Modify: `PerkEditorPanel.vue` (template Eligibility section + `newPerk`)
- Test: `PerkEditorPanel.test.ts`

- [ ] **Step 1: Write a failing test**

Add a test: mount, click "New Perk", the Association control is now an editable `<select>` (not the read-only text shown when editing an existing perk). Select the `siphoner` option, fill an id/displayName, trigger save, and assert the request passed to the perk save API carries `path: 'siphoner'`. Add a second case: choosing "Generic" results in a save request with NO `path` (or `path` undefined). Stub the save API the way the existing save test does.

- [ ] **Step 2: Run it, verify it FAILS**

Run: `npx vitest run src/components/PerkEditorPanel.test.ts`
Expected: FAIL (association is currently always a read-only input).

- [ ] **Step 3: Make Association editable for new perks only**

In the Eligibility `<section>`, replace the always-read-only association input with a conditional:

```vue
<label>
  Association <span class="perk-editor__hint">(catalog folder)</span>
  <select v-if="selectedId === null" v-model="associationSelection">
    <option value="">Generic</option>
    <optgroup v-for="[unit, ps] in sortedPathsByUnit" :key="unit" :label="unit">
      <option v-for="path in ps" :key="path" :value="path">{{ path }}</option>
    </optgroup>
  </select>
  <input v-else :value="form.path || 'generic'" disabled />
</label>
```

Add:
```ts
// Editing an existing perk: association is fixed (read-only, from its folder).
// Creating a new perk: the user picks the target association so SavePerkDef
// writes it into catalog/perks/<path|generic>/. '' = generic (form.path stays
// undefined so saveRequestFromForm omits it → server defaults to generic).
const associationSelection = computed<string>({
  get: () => form.value.path ?? '',
  set: (v) => { form.value.path = v || undefined },
})
const sortedPathsByUnit = computed<Array<[string, string[]]>>(() =>
  Object.entries(pathsByUnit.value)
    .sort((a, b) => a[0].localeCompare(b[0]))
    .map(([u, ps]) => [u, [...ps].sort((x, y) => x.localeCompare(y))]),
)
```

- [ ] **Step 4: Ensure `newPerk` starts generic (and optionally support a preset)**

`newPerk()` already calls `createBlankForm()` (path undefined = generic) — that's the correct default. OPTIONAL nicety: give each path group a `+` affordance that calls `newPerk('siphoner')`; if you add it, change the signature to `function newPerk(presetPath?: string) { ... ; if (presetPath) form.value.path = presetPath }` and wire a small `+` button in the group-path header. Not required for the core feature; only add if it's clean.

- [ ] **Step 5: Run tests + type-check + broad sanity**

Run: `npx vitest run src/components/PerkEditorPanel.test.ts && npx vue-tsc -b`
Then: `npx vitest run src/components/PerkEditorPanel src/game/perks`
Expected: PASS + clean. (Two pre-existing unrelated failures — `worldEditorToolbar`, `ListEditorPanel` — are NOT yours; ignore them.)

- [ ] **Step 6: Commit**

```bash
git add client/src/game-portal/src/components/PerkEditorPanel.vue client/src/game-portal/src/components/PerkEditorPanel.test.ts
git commit -m "feat(perk-editor): choose Unit/Path association when creating a new perk"
```

---

## Self-Review checklist (author)

- **Grouping data:** every perk lands in exactly one group; perks with unknown/empty `path` → Generic; nothing dropped. Units/paths/perks all deterministically sorted.
- **Selection unaffected:** the inner `data-test="perk-row"` button is byte-identical to today, so existing selection/edit/save/delete tests still pass.
- **Create routing:** a new perk with `associationSelection = 'siphoner'` produces a save request with `path: 'siphoner'`; Generic omits `path`. The server (already association-aware) writes it to the right folder — verify by reading the request the API stub received, not by trusting the UI.
- **Existing perk association stays read-only** (no folder move) — the `<select>` only renders when `selectedId === null`.
- **CSS:** no literal `cursor:` declarations (project convention — global rules handle it).
- **Degradation:** if `fetchUnitDefs` fails, `pathsByUnit` is `{}` → all perks fall into Generic and the create-select shows only "Generic"; the editor still works.

---

## Execution note

Backend is untouched (already association-aware from Task C4). This is two small frontend tasks on one component; a single implementer subagent per task with a spec check is sufficient. No server build/test needed — gate on `vitest` + `vue-tsc -b`.
