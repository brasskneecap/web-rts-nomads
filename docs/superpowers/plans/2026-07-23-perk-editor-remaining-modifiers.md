# Perk Editor — Migrate Remaining Modifier Types Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring the 6 remaining modifier kinds — Ability Modifier, Grant Ability, Perk Modifier, Aura, Ability Rider, Cosmetic Effect — up to full editability in the new Perk Builder's Inspector, then (optionally, gated) retire the classic editor and its toggle.

**Architecture:** Continues the slice from `2026-07-23-perk-editor-redesign.md`. The projection/round-trip model is unchanged: each modifier is a card addressing back into one `AuthoredPerkDef` array by `{arrayKey, index}`; the Inspector edits the selected element and writes a CLEANED wire object through `usePerkBuilder.updateSelected`. This plan (1) extends the hub with `single`-shape support (for `effect`), a rider schema/catalog fetch, per-kind `DEFAULTS`, and flips `editable: true`; (2) adds one Inspector branch per kind, reusing the existing `RiderEditor.vue` and `AuraEditor.vue`; (3) un-hides `effect` in the stack. **Config Value stays Setup-column-only** (not a card) — see Decision below.

**Tech Stack:** Vue 3 `<script setup>` + TS, Vitest + `@vue/test-utils`, existing `editor/` toolkit + `perk-editor/` components.

---

## Decision (overridable): Config Value is NOT a card

The original redesign spec listed "Config Value" as a modifier type. But config is perk-wide plumbing that wired Go handlers read, and it is already fully edited in the **Setup column** (`PerkSetupColumn.vue`). Surfacing it *also* as stack cards is a dual editing surface for the same data. **This plan keeps `configValue` in `HIDDEN` (Setup-only) and does not build a card inspector for it.** If you'd rather have config as cards too, that's a small addition (map-shape mutation in the hub + a config-value inspector branch) — say so and it becomes Task 7b. Everything else in this plan is unaffected.

## Prerequisite state (from the completed slice)

- `usePerkBuilder.ts` exposes: `form`, `selected`, `selectedEntry`, `modifiers`, `abilityIds`, `abilityDefsById`, `abilityStatDefs`, `selfStatDefsList`, `auraStatDefsList`, `pathsByUnit`, and actions `addModifier`, `removeModifier`, `duplicateModifier`, `updateSelected` (list + guarded map/single no-op), `save` (calls `saveEditorPerk(saveRequestFromForm(form.value))`).
- `perkModifierModel.ts`: `KIND_META` (per-kind label/accent/icon/arrayKey/shape/**editable**), `KIND_ORDER`, `buildModifierList`. `editable: true` currently only for `unitStat`/`abilityStat`/`abilityField`.
- `PerkModifierInspector.vue`: dispatches on `entry.kind`; has full branches for the 3 slice kinds, a "use Classic" note for the rest, and an empty state.
- `PerkModifierStack.vue`: `HIDDEN = ['configValue','effect']`; add-menu tags non-`editable` kinds "classic" and refuses to add them.
- Existing reusable sub-editors: `perk-editor/RiderEditor.vue` (props `modelValue: AbilityRider, abilityIds: string[], schema, catalogs`; emits `update:modelValue`) and `perk-editor/AuraEditor.vue` (props `modelValue: AuraRow, statDefs`; emits `update:modelValue`; exports `AuraRow`).
- Round-trip fixtures already used by the classic suite (`components/PerkEditorPanel.test.ts`): `beamMasteryPerk()` (abilityModifiers), `zealousMarchPerk()` (auras), `sharedSufferingPerk()` (abilityRiders). Copy their shapes into the new tests.

## Conventions (unchanged)

- Client type-check: `npx vue-tsc -b` from `client/src/game-portal/`. Run one test: `npx vitest run src/components/perk-editor/<file>.test.ts`.
- Wire shapes frozen — import from `@/game/perks/perkEditorForm`. Never write a blank as `0`/`""`; omit defaults (so round-trips are byte-identical).
- **No git commits** — every "Commit" step is do-not-run; stop for the user.
- No `cursor:` CSS in components (CLAUDE.md).
- 4 pre-existing unrelated test failures exist on this branch (`ListEditorPanel.test.ts`, `worldEditorToolbar.test.ts`) — out of scope; confirm no NEW failures.

## File Structure

| File | Change |
|---|---|
| `components/perk-editor/perkModifierModel.ts` | Flip `editable: true` for the 6 kinds as each task lands. |
| `components/perk-editor/usePerkBuilder.ts` | Add `single`-shape support to add/update/remove; per-kind `DEFAULTS`; rider `schema`/`catalogs`/`riderCatalogs` fetch; effect/grant clean-on-save. |
| `components/perk-editor/PerkModifierInspector.vue` | Add one branch per kind (inline for scalar kinds; host `RiderEditor`/`AuraEditor` for those). |
| `components/perk-editor/PerkModifierStack.vue` | `HIDDEN` drops `effect` (Task 7). |
| `components/perk-editor/PerkBuilderPanel.test.ts` | Add a round-trip test per kind. |
| `views/PerkEditor.vue`, `components/world-editor/WorldEditorPanel.vue` | (Task 8, optional) remove toggle, mount only `PerkBuilderPanel`. |
| `components/PerkEditorPanel.vue` + `PerkEditorPanel.test.ts` | (Task 8, optional) delete/retire. |

---

## Task 1: Hub — single-shape support, DEFAULTS, rider catalogs

**Files:**
- Modify: `client/src/game-portal/src/components/perk-editor/usePerkBuilder.ts`

No new test file — exercised by Tasks 2–7's panel tests. This task only adds capability; it flips no `editable` flags, so behavior is unchanged until later tasks.

- [ ] **Step 1: Add rider schema/catalog fetch**

Add imports at the top of `usePerkBuilder.ts` (mirror the classic `PerkEditorPanel.vue`'s imports exactly — verify names/paths there):

```ts
import {
  fetchAbilityCategories, fetchActionSchema, fetchAutoCastSelectors,
  fetchDamageTypes, fetchEffectIds, fetchProjectileIds,
} from '@/game/abilities/abilityEditorApi'
import type { ActionSchemaBundle } from '@/game/abilities/program/programSchema'
import type { AbilityBuilderCatalogs } from '@/components/ability-builder/useAbilityBuilder'
import { listObjectSpriteKeys } from '@/game/rendering/objectSprites'
import { fetchAuthoredUnitDefs } from '@/game/units/unitEditorApi'
```

Add refs alongside the other catalog refs:

```ts
  const riderSchema = ref<ActionSchemaBundle | null>(null)
  const riderCatalogs = ref<AbilityBuilderCatalogs>({
    effects: [], projectiles: [], damageTypes: [], categories: [], autoCastSelectors: [], unitTypes: [],
    objectSprites: listObjectSpriteKeys(),
    perks: [],
  })
```

At the END of `load()`, add the same parallel fetch the classic panel runs (non-fatal on failure):

```ts
    try {
      const [schema, effects, projectiles, damageTypes, categories, autoCastSelectors, units] = await Promise.all([
        fetchActionSchema(), fetchEffectIds(), fetchProjectileIds(), fetchDamageTypes(),
        fetchAbilityCategories(), fetchAutoCastSelectors(), fetchAuthoredUnitDefs(),
      ])
      riderSchema.value = schema
      riderCatalogs.value = {
        effects, projectiles, damageTypes, categories, autoCastSelectors,
        unitTypes: units.map((u) => u.type), objectSprites: listObjectSpriteKeys(), perks: [],
      }
    } catch { riderSchema.value = null }
```

Add `riderSchema, riderCatalogs` to the composable's returned object.

- [ ] **Step 2: Add per-kind DEFAULTS**

Replace the existing `DEFAULTS` object with one covering every editable kind (list + single). `aura` seeds a valid PerkAura (radius > 0 so the projection/summary render); `effect` is created via the single path (below), so it also needs a factory:

```ts
  const DEFAULTS: Partial<Record<ModifierKind, () => unknown>> = {
    unitStat: () => ({ stat: selfStatDefsList[0]?.id ?? '', op: 'add', value: 0 }),
    abilityStat: () => ({ stat: '' }),
    abilityField: () => ({ target: '', action: '', field: '', op: 'multiply', value: 0 }),
    abilityModifier: () => ({ target: '' }),
    grantAbility: () => '',
    perkModifier: () => ({ target: '', ops: [{ targetKey: '', op: 'mult', sourceKey: '' }] }),
    aura: () => ({ radius: 128, targets: 'allies', stacking: 'max', statModifiers: [] }),
    effect: () => ({ name: '' }),
  }
```

- [ ] **Step 3: Add `single`-shape handling to add/update/remove**

Update `addModifier`, `updateSelected`, `removeModifier` so `shape === 'single'` writes/clears the single field (the map shape stays a no-op — config is Setup-only):

```ts
  function addModifier(kind: ModifierKind) {
    const make = DEFAULTS[kind]
    if (!make) return
    const meta = KIND_META[kind]
    if (meta.shape === 'single') {
      form.value = { ...form.value, [meta.arrayKey]: make() }
      selected.value = { arrayKey: meta.arrayKey, index: 0 }
      return
    }
    if (meta.shape !== 'list') return
    const next = listFor(kind)
    next.push(make())
    replaceArray(kind, next)
    selected.value = { arrayKey: meta.arrayKey, index: next.length - 1 }
  }

  function updateSelected(next: unknown) {
    if (!selected.value) return
    const kind = kindForArrayKey(selected.value.arrayKey)
    if (KIND_META[kind].shape === 'single') {
      form.value = { ...form.value, [selected.value.arrayKey]: next }
      return
    }
    if (KIND_META[kind].shape !== 'list') return
    const list = listFor(kind)
    list[selected.value.index] = next
    replaceArray(kind, list)
  }

  function removeModifier(sel: Selection) {
    const kind = kindForArrayKey(sel.arrayKey)
    if (KIND_META[kind].shape === 'single') {
      form.value = { ...form.value, [sel.arrayKey]: undefined }
      if (selected.value && selected.value.arrayKey === sel.arrayKey) selected.value = null
      return
    }
    if (KIND_META[kind].shape !== 'list') return
    const next = listFor(kind)
    next.splice(sel.index, 1)
    replaceArray(kind, next)
    const s = selected.value
    if (s && s.arrayKey === sel.arrayKey) {
      if (s.index === sel.index) selected.value = null
      else if (s.index > sel.index) selected.value = { arrayKey: s.arrayKey, index: s.index - 1 }
    }
  }
```

- [ ] **Step 4: Clean empty effect / empty grants on save**

In `save()`, build a cleaned form before persisting so a half-added effect (blank name) or a blank grant-ability string never ships:

```ts
  async function save() {
    saveError.value = ''; statusMessage.value = ''; busy.value = true
    try {
      const cleaned: PerkEditorForm = { ...form.value }
      if (cleaned.effect && !cleaned.effect.name?.trim()) cleaned.effect = undefined
      if (cleaned.grantsAbilities) {
        const grants = cleaned.grantsAbilities.map((g) => g.trim()).filter(Boolean)
        cleaned.grantsAbilities = grants.length ? grants : undefined
      }
      await saveEditorPerk(saveRequestFromForm(cleaned))
      await reload()
      selectedId.value = form.value.id
      statusMessage.value = 'Saved.'
    } catch (e) {
      saveError.value = e instanceof EditorValidationError ? e.serverMessage : e instanceof Error ? e.message : String(e)
    } finally { busy.value = false }
  }
```

- [ ] **Step 5: Verify** — `npx vue-tsc -b` clean; `npx vitest run src/components/perk-editor/` still 25/25 (no editable flags flipped yet, so behavior unchanged).

- [ ] **Step 6: Commit** *(do not run)*

```bash
git add client/src/game-portal/src/components/perk-editor/usePerkBuilder.ts
git commit -m "feat(perk-editor): hub support for single-shape + rider catalogs + per-kind defaults"
```

---

## Task 2: Ability Modifier inspector

**Files:**
- Modify: `perkModifierModel.ts` (flip `abilityModifier.editable = true`)
- Modify: `PerkModifierInspector.vue` (add branch)
- Modify: `PerkBuilderPanel.test.ts` (round-trip test)

- [ ] **Step 1: Failing round-trip test** — add to `PerkBuilderPanel.test.ts`. Use the `beam_mastery` fixture (4 mults) and extend the shared `stub` to serve it and an ability list. Add a dedicated stub + test:

```ts
  it('loads beam_mastery ability modifier and round-trips it unedited', async () => {
    const beam = {
      id: 'beam_mastery', path: 'siphoner',
      abilityModifiers: [{ target: 'siphon_life', damageMult: 1.5, healMult: 1.5, manaCostMult: 0.5, rangeMult: 1.25 }],
      wired: false,
    }
    const sink: Array<Record<string, unknown>> = []
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      const u = String(url)
      if (init?.method === 'POST' && u.endsWith('/perks')) { sink.push((JSON.parse(String(init.body)) as { perk: Record<string, unknown> }).perk); return { ok: true, status: 200, json: async () => ({}) } }
      if (u.endsWith('/catalog/perks')) return { ok: true, status: 200, json: async () => ({ perks: [beam] }) }
      if (u.endsWith('/catalog/units')) return { ok: true, status: 200, json: async () => ({ units: [], paths: [], pathsByUnit: { acolyte: ['siphoner'] } }) }
      if (u.endsWith('/catalog/abilities')) return { ok: true, status: 200, json: async () => ({ abilities: [{ id: 'siphon_life' }] }) }
      return { ok: true, status: 200, json: async () => ({}) }
    }) as unknown as typeof fetch)

    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await openPerk(wrapper, 'Acolyte', 'siphoner')

    const card = wrapper.find('[data-test="perk-modifier-card"][data-kind="abilityModifier"]')
    expect(card.exists()).toBe(true)
    await card.trigger('click')
    const inspector = wrapper.find('[data-test="perk-inspector"]')
    expect((inspector.find('input[aria-label="Target Ability"]').element as HTMLInputElement).value).toBe('siphon_life')
    expect((inspector.find('input[aria-label="Damage mult"]').element as HTMLInputElement).value).toBe('1.5')

    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()
    expect(sink[0].abilityModifiers).toEqual(beam.abilityModifiers)
  })
```

Run it → FAILS (no `abilityModifier` inspector branch; card shows the "Classic" note, no `Target Ability` input).

- [ ] **Step 2: Flip editable** in `perkModifierModel.ts`: `abilityModifier: { …, editable: true }`.

- [ ] **Step 3: Add the inspector branch.** In `PerkModifierInspector.vue`, add after the `abilityField` branch (before the un-migrated `<template v-else>`):

```html
    <template v-else-if="entry.kind === 'abilityModifier'">
      <EditorField label="Target Ability">
        <input v-model="abilityMod.target" list="perk-builder-ability-ids" placeholder="ability id" aria-label="Target Ability" @input="commitAbilityMod" />
      </EditorField>
      <EditorField label="Damage ×"><input v-model.number="abilityMod.damageMult" type="number" step="0.05" placeholder="—" aria-label="Damage mult" @input="commitAbilityMod" /></EditorField>
      <EditorField label="Heal ×"><input v-model.number="abilityMod.healMult" type="number" step="0.05" placeholder="—" aria-label="Heal mult" @input="commitAbilityMod" /></EditorField>
      <EditorField label="Mana cost ×"><input v-model.number="abilityMod.manaCostMult" type="number" step="0.05" placeholder="—" aria-label="Mana mult" @input="commitAbilityMod" /></EditorField>
      <EditorField label="Range ×"><input v-model.number="abilityMod.rangeMult" type="number" step="0.05" placeholder="—" aria-label="Range mult" @input="commitAbilityMod" /></EditorField>
      <EditorField label="Cooldown ×"><input v-model.number="abilityMod.cooldownMult" type="number" step="0.05" placeholder="—" aria-label="Cooldown mult" @input="commitAbilityMod" /></EditorField>
    </template>
```

Add to `<script setup>` (import the type; add draft + seeding + commit). Import: `import type { AbilityModifier } from '@/game/perks/perkEditorForm'`.

```ts
const abilityMod = reactive({ target: '', damageMult: '' as number | '', healMult: '' as number | '', manaCostMult: '' as number | '', rangeMult: '' as number | '', cooldownMult: '' as number | '' })
const ABILITY_MOD_MULT_KEYS = ['damageMult', 'healMult', 'manaCostMult', 'rangeMult', 'cooldownMult'] as const
```

Extend the `watch(entry, …)` with an `abilityModifier` case:

```ts
  } else if (e.kind === 'abilityModifier') {
    const m = current<AbilityModifier>()!
    abilityMod.target = m.target
    for (const k of ABILITY_MOD_MULT_KEYS) abilityMod[k] = m[k] ?? ''
  }
```

Add the commit (drop blank mults; a blank must never write `0`):

```ts
function commitAbilityMod() {
  const next: AbilityModifier = { target: abilityMod.target.trim() }
  for (const k of ABILITY_MOD_MULT_KEYS) {
    const v = abilityMod[k]
    if (typeof v === 'number' && !Number.isNaN(v)) next[k] = v
  }
  builder.updateSelected(next)
}
```

- [ ] **Step 4:** Run the test → PASS. `npx vue-tsc -b` clean. Full `src/components/perk-editor/` green.

- [ ] **Step 5: Commit** *(do not run)* — `feat(perk-editor): ability-modifier inspector`

---

## Task 3: Grant Ability inspector

**Files:** `perkModifierModel.ts` (editable), `PerkModifierInspector.vue`, `PerkBuilderPanel.test.ts`.

Each `grantsAbilities[i]` is a bare string, so the element edited by the inspector IS a string.

- [ ] **Step 1: Failing test** (a perk with `grantsAbilities: ['dash','blink']`): select the first grant card, assert its `input[aria-label="Granted Ability"]` value is `dash`; change it to `sprint`; Save; assert `sink[0].grantsAbilities` equals `['sprint','blink']`. Run → FAILS.

- [ ] **Step 2: Flip** `grantAbility.editable = true`.

- [ ] **Step 3: Inspector branch:**

```html
    <template v-else-if="entry.kind === 'grantAbility'">
      <EditorField label="Granted Ability" hint="(ability id this perk adds to the unit)">
        <input v-model="grant.id" list="perk-builder-ability-ids" aria-label="Granted Ability" @input="commitGrant" />
      </EditorField>
    </template>
```

Script:

```ts
const grant = reactive({ id: '' })
// in watch(entry):
  } else if (e.kind === 'grantAbility') {
    grant.id = current<string>() ?? ''
  }
function commitGrant() { builder.updateSelected(grant.id.trim()) }
```

(Empty grants are dropped on save by the hub's Task 1 clean step.)

- [ ] **Step 4:** test PASS; `vue-tsc -b` clean.
- [ ] **Step 5: Commit** *(do not run)* — `feat(perk-editor): grant-ability inspector`

---

## Task 4: Perk Modifier inspector (nested ops)

**Files:** `perkModifierModel.ts` (editable), `PerkModifierInspector.vue`, `PerkBuilderPanel.test.ts`.

`PerkModifier` = `{ target, ops: [{ targetKey, op: 'mult'|'add', sourceKey }] }`. Port the classic panel's `cleanPerkModifiers` semantics: drop ops missing `targetKey`/`sourceKey`; a modifier with no usable ops is emitted as `{ target, ops: [] }` here (the CARD exists because the user added it) — but on SAVE it must round-trip a real fixture exactly, so cleaning happens in the commit: if no usable ops, still keep the object (so the card persists while editing) but the round-trip test uses a fixture whose ops ARE complete.

- [ ] **Step 1: Failing test** using `ascended_corruption`-style fixture: `{ id, path:'siphoner', config:{...}, perkModifiers:[{ target:'beam_mastery', ops:[{ targetKey:'damageMultiplier', op:'mult', sourceKey:'boost' }] }] }`. Select the perkModifier card; assert `input[aria-label="Target Perk"]` = `beam_mastery` and the op row's `Target key`/`Source key`; Save unedited; assert `sink[0].perkModifiers` deep-equals the fixture. Run → FAILS.

- [ ] **Step 2: Flip** `perkModifier.editable = true`.

- [ ] **Step 3: Inspector branch** (inline nested ops — add/remove op rows):

```html
    <template v-else-if="entry.kind === 'perkModifier'">
      <EditorField label="Target Perk" hint="(enhanced when the owner also has it)">
        <input v-model="perkMod.target" list="perk-builder-perk-ids" placeholder="perk id" aria-label="Target Perk" @input="commitPerkMod" />
      </EditorField>
      <div v-for="(op, i) in perkMod.ops" :key="i" class="pi-op-row">
        <EditorField label="Target key"><input v-model="op.targetKey" :aria-label="`Op ${i + 1} target key`" @input="commitPerkMod" /></EditorField>
        <EditorField label="Op">
          <select v-model="op.op" :aria-label="`Op ${i + 1} op`" @change="commitPerkMod">
            <option value="mult">× multiply</option>
            <option value="add">+ add</option>
          </select>
        </EditorField>
        <EditorField label="Source key"><input v-model="op.sourceKey" list="perk-builder-own-config-keys" :aria-label="`Op ${i + 1} source key`" @input="commitPerkMod" /></EditorField>
        <button type="button" class="pi-op-del" title="Remove op" @click="removePerkModOp(i)">✕</button>
      </div>
      <button type="button" class="pi-op-add" @click="addPerkModOp">+ Add Op</button>
      <datalist id="perk-builder-own-config-keys">
        <option v-for="k in ownConfigKeys" :key="k" :value="k" />
      </datalist>
    </template>
```

Script (import `PerkModifier`, `PerkConfigOp`):

```ts
const perkMod = reactive<{ target: string; ops: { targetKey: string; op: 'mult' | 'add'; sourceKey: string }[] }>({ target: '', ops: [] })
const ownConfigKeys = computed(() => Object.keys(builder.form.value.config ?? {}))
// in watch(entry):
  } else if (e.kind === 'perkModifier') {
    const m = current<PerkModifier>()!
    perkMod.target = m.target
    perkMod.ops = (m.ops ?? []).map((o) => ({ targetKey: o.targetKey, op: o.op, sourceKey: o.sourceKey }))
  }
function addPerkModOp() { perkMod.ops.push({ targetKey: '', op: 'mult', sourceKey: '' }); commitPerkMod() }
function removePerkModOp(i: number) { perkMod.ops.splice(i, 1); commitPerkMod() }
function commitPerkMod() {
  const ops: PerkConfigOp[] = perkMod.ops
    .map((o) => ({ targetKey: o.targetKey.trim(), op: o.op, sourceKey: o.sourceKey.trim() }))
    .filter((o) => o.targetKey && o.sourceKey)
  const next: PerkModifier = { target: perkMod.target.trim(), ops }
  builder.updateSelected(next)
}
```

Add minimal CSS: `.pi-op-row { display: grid; grid-template-columns: 1fr auto 1fr auto; gap: 6px; align-items: end; } .pi-op-add, .pi-op-del { … }` (no `cursor:`).

- [ ] **Step 4:** test PASS; `vue-tsc -b` clean.
- [ ] **Step 5: Commit** *(do not run)* — `feat(perk-editor): perk-modifier inspector with nested ops`

---

## Task 5: Aura inspector (reuse AuraEditor)

**Files:** `perkModifierModel.ts` (editable), `PerkModifierInspector.vue`, `PerkBuilderPanel.test.ts`.

`AuraEditor.vue` edits an `AuraRow` (UI shape), not `PerkAura` (wire). Port the classic panel's `rowsFromAuras`/`aurasFromRows` conversions so the inspector reads the selected `PerkAura` → `AuraRow` for the editor, and writes `AuraRow` → `PerkAura` back. The single row (not a list) is edited — the aura the card points at.

- [ ] **Step 1: Failing test** using the `zealous_march` aura fixture (radius 192, allies, includeSelf, perAdditionalSource 0.05, moveSpeed +0.3, stacking max). Open it (path `cleric`), select the `aura` card, assert `AuraEditor` renders with radius `192`; Save unedited; assert `sink[0].auras` deep-equals the fixture. Run → FAILS (aura currently shows the "Classic" note).

- [ ] **Step 2: Flip** `aura.editable = true`.

- [ ] **Step 3: Inspector branch** hosting `AuraEditor`:

```html
    <template v-else-if="entry.kind === 'aura'">
      <AuraEditor :model-value="auraRow" :stat-defs="builder.auraStatDefsList" @update:model-value="onAuraRow" />
    </template>
```

Script (import `AuraEditor`, its `AuraRow` type, and `PerkAura`, `PerkStatModifier`):

```ts
import AuraEditor, { type AuraRow } from './AuraEditor.vue'
import type { PerkAura, PerkStatModifier } from '@/game/perks/perkEditorForm'

const auraRow = ref<AuraRow>({ radius: 128, targets: 'allies', includeSelf: false, perAdditionalSource: '', statRows: [], ringColor: '' })

function rowFromAura(a: PerkAura): AuraRow {
  return {
    radius: a.radius, targets: a.targets, includeSelf: a.includeSelf ?? false,
    perAdditionalSource: a.perAdditionalSource ?? '',
    statRows: (a.statModifiers ?? []).map((m) => ({ stat: m.stat, value: m.value })),
    ringColor: a.ringColor ?? '',
  }
}
function auraFromRow(row: AuraRow): PerkAura {
  const radius = typeof row.radius === 'number' && !Number.isNaN(row.radius) ? row.radius : 0
  const statModifiers: PerkStatModifier[] = []
  for (const sr of row.statRows) {
    if (!sr.stat) continue
    const value = typeof sr.value === 'number' && !Number.isNaN(sr.value) ? sr.value : 0
    statModifiers.push({ stat: sr.stat, op: 'add', value })
  }
  const aura: PerkAura = { radius, targets: row.targets, stacking: 'max', statModifiers }
  if (row.includeSelf) aura.includeSelf = true
  if (typeof row.perAdditionalSource === 'number' && !Number.isNaN(row.perAdditionalSource)) aura.perAdditionalSource = row.perAdditionalSource
  if (row.ringColor) aura.ringColor = row.ringColor
  return aura
}
// in watch(entry):
  } else if (e.kind === 'aura') {
    auraRow.value = rowFromAura(current<PerkAura>()!)
  }
function onAuraRow(row: AuraRow) { auraRow.value = row; builder.updateSelected(auraFromRow(row)) }
```

Note: `aurasFromRows` in the classic panel omits an aura with no stat rows; here the CARD already exists, so we always emit the aura (the round-trip fixture has a stat row, so it's byte-identical). If you want "empty aura is dropped," add that to the hub's save clean — not required for round-trip correctness.

- [ ] **Step 4:** test PASS; `vue-tsc -b` clean.
- [ ] **Step 5: Commit** *(do not run)* — `feat(perk-editor): aura inspector reusing AuraEditor`

---

## Task 6: Ability Rider inspector (reuse RiderEditor)

**Files:** `perkModifierModel.ts` (editable), `PerkModifierInspector.vue`, `PerkBuilderPanel.test.ts`.

`RiderEditor.vue` takes an `AbilityRider` (already the wire shape) + `abilityIds`/`schema`/`catalogs` (now provided by the hub from Task 1). It emits `update:modelValue` with the edited rider.

- [ ] **Step 1: Failing test** using the `shared_suffering` fixture (target `siphon_life`, trigger `on_tick`, 2 actions). The stub must also serve `/catalog/action-schema`, `/catalog/damage-types`, `/catalog/abilities`, etc. (copy the classic suite's `stubFetchWithRider`). Open it, assert two `[data-test="flow-action-card"]` render; edit the deal_damage `amountMult` from 0.4 → 0.6 via the rider's inspector; Save; assert `sink[0].abilityRiders[0].actions[1].config.amountMult === 0.6` and the untouched action round-trips. Also add an unedited round-trip assertion. Run → FAILS.

- [ ] **Step 2: Flip** `abilityRider.editable = true`.

- [ ] **Step 3: Inspector branch** hosting `RiderEditor`:

```html
    <template v-else-if="entry.kind === 'abilityRider'">
      <RiderEditor
        v-if="builder.riderSchema.value"
        :model-value="riderModel"
        :ability-ids="builder.abilityIds.value"
        :schema="builder.riderSchema.value"
        :catalogs="builder.riderCatalogs.value"
        @update:model-value="onRider"
      />
      <p v-else class="pi-note pi-note--dim">Loading ability schema…</p>
    </template>
```

Script (import `RiderEditor`, `AbilityRider`):

```ts
import RiderEditor from './RiderEditor.vue'
import type { AbilityRider } from '@/game/perks/perkEditorForm'

const riderModel = computed<AbilityRider>(() => current<AbilityRider>() ?? { target: '', trigger: '', actions: [] })
function onRider(r: AbilityRider) { builder.updateSelected(r) }
```

(No local draft is needed — `RiderEditor` is controlled: it takes `riderModel` and emits the full updated rider, which we write straight back.)

- [ ] **Step 4:** test PASS; `vue-tsc -b` clean. (If `RiderEditor` requires a `data-test="rider-inspector"` hook the test uses, confirm it exists — the classic suite already relies on it.)
- [ ] **Step 5: Commit** *(do not run)* — `feat(perk-editor): ability-rider inspector reusing RiderEditor`

---

## Task 7: Cosmetic Effect inspector + un-hide effect in the stack

**Files:** `perkModifierModel.ts` (editable), `usePerkBuilder.ts` (already single-shape ready), `PerkModifierStack.vue` (HIDDEN), `PerkModifierInspector.vue`, `PerkBuilderPanel.test.ts`.

- [ ] **Step 1: Failing test:** a perk with `effect: { name: 'burning', target: 'enemies', durationSeconds: 3 }`. After un-hiding, assert an `effect` card renders; select it; assert `input[aria-label="Effect name"]` = `burning`; Save unedited; assert `sink[0].effect` deep-equals the fixture. Also a second test: add an effect via quick-add, leave the name blank, Save → assert `'effect' in sink[0] === false` (blank effect dropped by the hub clean). Run → FAILS.

- [ ] **Step 2: Un-hide effect** in `PerkModifierStack.vue`: `const HIDDEN: ModifierKind[] = ['configValue']`.

- [ ] **Step 3: Flip** `effect.editable = true` in `perkModifierModel.ts`.

- [ ] **Step 4: Inspector branch:**

```html
    <template v-else-if="entry.kind === 'effect'">
      <EditorField label="Effect name"><input v-model="effect.name" aria-label="Effect name" @input="commitEffect" /></EditorField>
      <EditorField label="Target">
        <select v-model="effect.target" aria-label="Effect target" @change="commitEffect">
          <option value="">(none)</option>
          <option value="self">self</option>
          <option value="enemies">enemies</option>
        </select>
      </EditorField>
      <EditorField label="Size scale"><input v-model.number="effect.sizeScale" type="number" step="0.1" placeholder="—" aria-label="Effect size scale" @input="commitEffect" /></EditorField>
      <EditorField label="Duration (s)"><input v-model.number="effect.durationSeconds" type="number" step="0.5" placeholder="—" aria-label="Effect duration" @input="commitEffect" /></EditorField>
      <EditorField label="Variant"><input v-model="effect.variant" aria-label="Effect variant" @input="commitEffect" /></EditorField>
    </template>
```

Script (import `PerkEffectShape`):

```ts
const effect = reactive({ name: '', target: '', sizeScale: '' as number | '', durationSeconds: '' as number | '', variant: '' })
// in watch(entry):
  } else if (e.kind === 'effect') {
    const m = current<PerkEffectShape>()!
    Object.assign(effect, { name: m.name ?? '', target: m.target ?? '', sizeScale: m.sizeScale ?? '', durationSeconds: m.durationSeconds ?? '', variant: m.variant ?? '' })
  }
function commitEffect() {
  const next: PerkEffectShape = { name: effect.name }
  if (effect.target) next.target = effect.target
  if (typeof effect.sizeScale === 'number' && !Number.isNaN(effect.sizeScale)) next.sizeScale = effect.sizeScale
  if (typeof effect.durationSeconds === 'number' && !Number.isNaN(effect.durationSeconds)) next.durationSeconds = effect.durationSeconds
  if (effect.variant) next.variant = effect.variant
  builder.updateSelected(next)
}
```

(The card stays visible with a blank name while editing; the hub's save clean drops a blank-name effect.)

- [ ] **Step 5:** both tests PASS; `vue-tsc -b` clean; full `src/components/perk-editor/` green. All add-menu kinds now editable → no "classic" tags remain except `configValue` (which is filtered out anyway), so the add-menu shows only real, addable kinds.

- [ ] **Step 6: Commit** *(do not run)* — `feat(perk-editor): cosmetic-effect inspector; effect now a card`

---

## Task 8 (GATED — only after you've used the new builder and are confident): retire the classic editor

Do NOT start this until you've exercised the new builder on real perks (riders/auras/perk-mods especially) and are satisfied. This removes the safety net.

**Files:**
- Modify: `views/PerkEditor.vue`, `components/world-editor/WorldEditorPanel.vue` — drop the toggle; mount only `PerkBuilderPanel`.
- Delete: `components/PerkEditorPanel.vue` and `components/PerkEditorPanel.test.ts` — OR keep the test by porting any still-unique assertions into `PerkBuilderPanel.test.ts` first.

- [ ] **Step 1: Coverage parity check.** Before deleting `PerkEditorPanel.test.ts`, list every behavior it asserts that `PerkBuilderPanel.test.ts` does NOT yet cover (e.g. association select writes `path`; generic-vs-path grouping; generatedDescription read-only + stripped on save; aura-only stats excluded from Unit Stat dropdown but present in Aura dropdown). Port each missing one into `PerkBuilderPanel.test.ts` and make it pass. Do not delete until parity holds.

- [ ] **Step 2: Simplify `views/PerkEditor.vue`** back to a single-panel view (remove `mode` ref + toggle; render only `PerkBuilderPanel`). Remove the classic import.

- [ ] **Step 3: Simplify `WorldEditorPanel.vue`** — replace the `<template v-else-if="activeScreen === 'perks'">` block with `<PerkBuilderPanel v-else-if="activeScreen === 'perks'" />`; remove `perkMode` ref, the classic import, and the `.world-editor__perk-mode` CSS.

- [ ] **Step 4: Delete** `PerkEditorPanel.vue` + `PerkEditorPanel.test.ts`. Grep the repo for any other importers (`Grep "PerkEditorPanel"`); there should be none left besides docs.

- [ ] **Step 5: Verify** `npx vue-tsc -b` clean; full `npx vitest run` shows no new failures (only the 4 pre-existing unrelated ones); `src/components/perk-editor/` fully green.

- [ ] **Step 6: Commit** *(do not run)* — `refactor(perk-editor): retire classic editor and toggle`

---

## Self-Review (against the deferred set)

- Ability Modifier → Task 2. ✔  Grant Ability → Task 3. ✔  Perk Modifier → Task 4. ✔  Aura → Task 5 (reuses `AuraEditor`). ✔  Ability Rider → Task 6 (reuses `RiderEditor`). ✔  Cosmetic Effect → Task 7 (single-shape). ✔
- Config Value → intentionally Setup-only (Decision), not a card. ✔ (flagged as overridable)
- Retire classic + remove toggle → Task 8, gated on confidence + coverage parity. ✔
- Round-trip preservation carried per kind by TDD tests against real fixtures (beam_mastery, ascended_corruption-style, zealous_march, shared_suffering). ✔
- Placeholder scan: every code step has concrete code. ✔
- Type consistency: `abilityMod`/`grant`/`perkMod`/`auraRow`/`riderModel`/`effect` drafts, `commit*` names, `ABILITY_MOD_MULT_KEYS`, hub `riderSchema`/`riderCatalogs`, and the `single`-shape add/update/remove are used consistently across tasks. ✔

## Notes / risks

- **Inspector file growth:** `PerkModifierInspector.vue` gains 6 branches. If it becomes unwieldy, a reasonable refactor (not required here) is to extract each kind's fields into `perk-editor/inspector/<Kind>Fields.vue` and keep the inspector a thin dispatcher — the rider/aura branches already point that way. Flag as DONE_WITH_CONCERNS if it crosses ~400 lines.
- **RiderEditor controlled-vs-draft:** it's used controlled here (model in, full model out). Confirm `RiderEditor` doesn't rely on internal state that a wholesale `modelValue` replacement would stomp mid-edit; the classic panel drove it the same way, so this should hold — the Task 6 edit-round-trip test guards it.
- **Aura "empty is dropped":** the classic editor dropped a zero-contribution aura; here the card persists. If you want empty auras auto-dropped on save, add it to the hub `save()` clean (like effect/grants). Not needed for round-trip correctness.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-07-23-perk-editor-remaining-modifiers.md`. Two execution options:

1. **Subagent-Driven (recommended)** — fresh subagent per task + two-stage review, same as the slice.
2. **Inline Execution** — batch with checkpoints.

Which approach — and do you accept the Config-Value-stays-in-Setup decision, or want it added as cards too?
