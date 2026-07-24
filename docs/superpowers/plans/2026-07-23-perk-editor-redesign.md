# Perk Editor Redesign — Vertical Slice Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild the Perk Editor around a card + inspector model that shows only the modifiers a perk actually has — matching the Ability Builder's philosophy and visual language — proving the whole pattern end-to-end on the three modifier types `Amplified Effects` uses (Unit Stat, Ability Stat, Ability Field), with the classic editor kept reachable for the not-yet-migrated types.

**Architecture:** A new `PerkBuilderPanel.vue` composes the shared `editor/` toolkit (`EditorShell` 4-column grid, `SectionCard`, `EditorField`) plus the `forge` theme. A single `usePerkBuilder()` composable is `provide()`d (mirroring `useAbilityBuilder`). `form` remains an `AuthoredPerkDef`; the center column renders a **projection** of `form`'s modifier arrays as read-only summary cards; the inspector column edits whichever card is `selected`, writing back into the source array by index. Because non-selected/un-migrated arrays are never touched, every existing perk round-trips byte-for-byte. The new panel ships beside the classic `PerkEditorPanel.vue` behind a per-mount toggle so the 7 not-yet-migrated modifier types stay editable in the classic UI.

**Tech Stack:** Vue 3 `<script setup>` + TypeScript, Pinia-free local composable, Vitest + `@vue/test-utils`, existing `editor/` toolkit and `--ed-*` / `forge` design tokens.

---

## Scope

**In this slice:**
- The 4-column shell (Perk List | Modifier Stack + Summary | Inspector | Perk Setup).
- Perk summary card.
- Modifier stack: read-only summary cards for **all 10** modifier kinds (so a perk is never misrepresented), color/icon accent per kind, per-card select + duplicate + delete.
- "+ Add Modifier" menu and quick-add row.
- Inspector with **full editing** for: Unit Stat Modifier, Ability Stat Modifier, Ability Field Modifier. For the other 7 kinds the inspector shows the summary plus a "edit in classic editor" note.
- Perk Setup column: Identity, Eligibility, Tooltip, Config, Config-By-Rank.
- Toggle in both mount points; classic editor untouched and still default-reachable.

**Deferred to follow-up plans (NOT this slice):** rich inspectors for Ability Modifier, Ability Rider, Aura, Grant Ability, Perk Modifier, Config Value, Cosmetic Effect; retiring the classic `PerkEditorPanel.vue`; removing the toggle.

## File Structure

All new files under `client/src/game-portal/src/components/perk-editor/` unless noted.

| File | Responsibility |
|---|---|
| `perkModifierModel.ts` | Pure model: `ModifierKind`, `KIND_META` (label/accent/icon/order/arrayKey/kindType), `buildModifierList(form, labels)` → ordered `ModifierEntry[]` with display summary. The single source of truth for "what cards does this perk have". |
| `perkModifierModel.test.ts` | Unit tests for the projection + summaries. |
| `abilityFieldOptions.ts` | Pure helpers ported from the classic panel: `walkProgramActions`, `actionsForTarget`, `fieldsForAction`, `fieldPreview`. Drives the Ability Field inspector's Action/Field dropdowns. |
| `abilityFieldOptions.test.ts` | Unit tests for nested-action discovery + numeric-field filtering. |
| `PerkBuilderContext.ts` | `provide/inject` wiring (`PerkBuilderKey`, `usePerkBuilderContext`). |
| `usePerkBuilder.ts` | State composable: perks/form/selection/catalogs + load/select/new/save/remove + modifier add/remove/duplicate + per-array write-back helpers. |
| `PerkSidebar.vue` | Unit → Path → Perk expandable folders + search + New Perk, styled with `--ed-*` tokens. |
| `PerkSummaryCard.vue` | Icon + name + id + description + wired badge + association + modifier-count chip. |
| `PerkModifierCard.vue` | One compact, read-only summary card: accent bar, type label, one-line summary, select-on-click, duplicate/delete controls. |
| `PerkModifierStack.vue` | The vertical card list + "+ Add Modifier" menu + quick-add row. |
| `PerkModifierInspector.vue` | Edits the selected modifier; dispatches to per-kind sub-editors; fallback note for un-migrated kinds. |
| `PerkSetupColumn.vue` | Identity / Eligibility / Tooltip / Config / Config-By-Rank in `SectionCard`s. |
| `PerkBuilderPanel.vue` | Root: owns `usePerkBuilder()`, provides it, composes the four columns into `EditorShell`, mounts `EditorHeader`. |
| `PerkBuilderPanel.test.ts` | Component tests: projection renders, round-trip save preservation, selection → inspector edit → save. |
| `views/PerkEditor.vue` (modify) | Add classic/new toggle. |
| `components/world-editor/WorldEditorPanel.vue` (modify) | Add classic/new toggle for `activeScreen === 'perks'`. |

Reused as-is (no changes): `perkEditorForm.ts`, `perkEditorApi.ts`, `statRegistry.ts`, `EditorShell.vue`, `SectionCard.vue`, `EditorField.vue`, `confirmDelete.ts`, `PerkEditorPanel.vue` (classic).

## Conventions the executor must know

- **Client type-check is `vue-tsc -b`** (build mode), run from `client/src/game-portal/`. `--noEmit` false-cleans because the root tsconfig is solution-style.
- **Run one test file:** from `client/src/game-portal/`, `npx vitest run src/components/perk-editor/<file>.test.ts`.
- **Wire shapes are frozen.** All types come from `@/game/perks/perkEditorForm` (`AuthoredPerkDef`, `PerkStatModifier`, `PerkAbilityStat`, `AbilityFieldModifier`, `AbilityModifier`, `AbilityRider`, `PerkAura`, `PerkModifier`, `PerkEffectShape`, `PerkEditorForm`, `saveRequestFromForm`, `formFromDef`, `createBlankForm`). Do not add fields to them.
- **Percent boundary:** `PerkAbilityStat.pct` is a FRACTION on the wire (`0.15`), whole percent in the UI (`15`). Same as the classic panel.
- **Blank-stripping on write-back:** a cleared numeric input is `''`, never `0`; an omitted optional must stay absent (never round-trip as `0`/`""`). The classic panel's `abilityStatsFromRows` / `abilityFieldsFromRows` / `statModifiersFromRows` encode these rules — port them faithfully.
- **CSS cursor rule (CLAUDE.md):** never write `cursor: default/pointer/auto` in component CSS; `cursor: not-allowed` is allowed per-state. The global rules handle the game cursor.
- **No git commits** — the user handles all staging/commits. The "Commit" steps below are written per the skill's format; **do not run them.** Stop at each commit point for the user instead.

---

## Task 1: The pure modifier model

**Files:**
- Create: `client/src/game-portal/src/components/perk-editor/perkModifierModel.ts`
- Test: `client/src/game-portal/src/components/perk-editor/perkModifierModel.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// perkModifierModel.test.ts
import { describe, expect, it } from 'vitest'
import { buildModifierList, KIND_META, type ModifierLabels } from './perkModifierModel'
import type { AuthoredPerkDef } from '@/game/perks/perkEditorForm'

const labels: ModifierLabels = {
  statLabel: (id) => ({ abilityDamage: 'Ability Damage', maxHp: 'Max Health' }[id] ?? id),
  abilityStatLabel: (id) => ({ damageTaken: 'Vulnerable (Damage Taken)', moveSpeed: 'Move Speed' }[id] ?? id),
  abilityLabel: (id) => ({ marker_trap: 'Marker Trap' }[id] ?? id),
}

const amplified: AuthoredPerkDef = {
  id: 'amplified_effects',
  statModifiers: [{ stat: 'abilityDamage', op: 'multiply', value: 1.35 }],
  abilityStats: [
    { stat: 'damageTaken', flat: 0.15 },
    { stat: 'moveSpeed', flat: -0.15 },
  ],
  abilityFields: [{ target: 'marker_trap', action: 'mark', field: 'duration', op: 'multiply', value: 1.35 }],
}

describe('buildModifierList', () => {
  it('projects every modifier array into ordered, addressable entries', () => {
    const list = buildModifierList(amplified, labels)
    expect(list.map((e) => e.kind)).toEqual(['unitStat', 'abilityStat', 'abilityStat', 'abilityField'])
    // each entry addresses back into its source array by index
    expect(list.map((e) => [e.arrayKey, e.index])).toEqual([
      ['statModifiers', 0], ['abilityStats', 0], ['abilityStats', 1], ['abilityFields', 0],
    ])
  })

  it('builds human summaries using the injected label lookups', () => {
    const [unit, vuln, slow, field] = buildModifierList(amplified, labels)
    expect(unit.summary).toBe('Ability Damage ×1.35')
    expect(vuln.summary).toBe('Vulnerable (Damage Taken) +15%')
    expect(slow.summary).toBe('Move Speed −15%')
    expect(field.summary).toBe('Marker Trap ▸ mark ▸ duration ×1.35')
  })

  it('tags each entry with its kind meta (accent + label)', () => {
    const [unit] = buildModifierList(amplified, labels)
    expect(unit.meta).toBe(KIND_META.unitStat)
    expect(KIND_META.unitStat.label).toBe('Unit Stat Modifier')
    expect(KIND_META.abilityStat.accent).not.toBe(KIND_META.unitStat.accent)
  })

  it('emits nothing for a perk with no modifiers', () => {
    expect(buildModifierList({ id: 'empty' }, labels)).toEqual([])
  })

  it('renders a grant-ability entry per granted id and a config entry per key', () => {
    const list = buildModifierList(
      { id: 'p', grantsAbilities: ['dash', 'blink'], config: { radius: 120 } },
      labels,
    )
    expect(list.filter((e) => e.kind === 'grantAbility')).toHaveLength(2)
    expect(list.filter((e) => e.kind === 'configValue')).toHaveLength(1)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run (from `client/src/game-portal/`): `npx vitest run src/components/perk-editor/perkModifierModel.test.ts`
Expected: FAIL — `buildModifierList` / `KIND_META` not found.

- [ ] **Step 3: Write the implementation**

```ts
// perkModifierModel.ts
import type { AuthoredPerkDef } from '@/game/perks/perkEditorForm'

// The full set of modifier kinds a perk can carry. Order in this union is NOT
// the render order — KIND_ORDER below is.
export type ModifierKind =
  | 'unitStat' | 'abilityStat' | 'abilityField' | 'abilityModifier'
  | 'abilityRider' | 'aura' | 'grantAbility' | 'perkModifier'
  | 'configValue' | 'effect'

// Which arrays on AuthoredPerkDef each kind projects from. `single` arrays hold
// one object (effect); `map` is the config record; everything else is a list.
type SourceShape = 'list' | 'map' | 'single'

export interface KindMeta {
  kind: ModifierKind
  label: string
  /** Hex accent used for the card's left bar / icon tint. */
  accent: string
  /** Short glyph shown in the card header (swap for real art later). */
  icon: string
  /** The AuthoredPerkDef key this kind reads/writes. */
  arrayKey: keyof AuthoredPerkDef
  shape: SourceShape
  /** Whether the inspector can fully edit this kind in the current slice. */
  editable: boolean
}

// One card in the stack. `arrayKey`/`index` address back into `form` so the
// inspector edits the source in place — non-addressed arrays are never touched,
// which is what makes every un-migrated modifier round-trip byte-for-byte.
export interface ModifierEntry {
  kind: ModifierKind
  meta: KindMeta
  arrayKey: keyof AuthoredPerkDef
  index: number
  /** Stable key for v-for / selection. */
  id: string
  /** One-line, human-readable description of what the modifier does. */
  summary: string
}

export interface ModifierLabels {
  statLabel: (id: string) => string
  abilityStatLabel: (id: string) => string
  abilityLabel: (id: string) => string
}

// Colors: distinct per the redesign spec (purple/orange/blue/green/red…). These
// are the only per-type color source in the editor — introduce them here so the
// cards, add-menu, and inspector all read one table.
export const KIND_META: Record<ModifierKind, KindMeta> = {
  unitStat:       { kind: 'unitStat',       label: 'Unit Stat Modifier',    accent: '#a78bfa', icon: '◈', arrayKey: 'statModifiers',   shape: 'list',   editable: true },
  abilityStat:    { kind: 'abilityStat',    label: 'Ability Stat Modifier', accent: '#f59e0b', icon: '✦', arrayKey: 'abilityStats',    shape: 'list',   editable: true },
  abilityField:   { kind: 'abilityField',   label: 'Ability Field Modifier',accent: '#38bdf8', icon: '⊹', arrayKey: 'abilityFields',   shape: 'list',   editable: true },
  abilityModifier:{ kind: 'abilityModifier',label: 'Ability Modifier',      accent: '#2dd4bf', icon: '×', arrayKey: 'abilityModifiers',shape: 'list',   editable: false },
  abilityRider:   { kind: 'abilityRider',   label: 'Ability Rider',         accent: '#f0846c', icon: '⇥', arrayKey: 'abilityRiders',   shape: 'list',   editable: false },
  aura:           { kind: 'aura',           label: 'Aura',                  accent: '#86c46b', icon: '◎', arrayKey: 'auras',           shape: 'list',   editable: false },
  grantAbility:   { kind: 'grantAbility',   label: 'Grant Ability',         accent: '#e7c88a', icon: '➕', arrayKey: 'grantsAbilities', shape: 'list',   editable: false },
  perkModifier:   { kind: 'perkModifier',   label: 'Perk Modifier',         accent: '#ec4899', icon: '⧉', arrayKey: 'perkModifiers',   shape: 'list',   editable: false },
  configValue:    { kind: 'configValue',    label: 'Config Value',          accent: '#94a3b8', icon: '#', arrayKey: 'config',          shape: 'map',    editable: false },
  effect:         { kind: 'effect',         label: 'Cosmetic Effect',       accent: '#818cf8', icon: '✧', arrayKey: 'effect',          shape: 'single', editable: false },
}

// Render order in the stack: the modifiers that DEFINE the perk first, cosmetic
// / plumbing last. Matches the redesign mock (Unit Stat, Ability Stat, Ability
// Field, …).
export const KIND_ORDER: ModifierKind[] = [
  'unitStat', 'abilityStat', 'abilityField', 'abilityModifier',
  'abilityRider', 'aura', 'perkModifier', 'grantAbility', 'effect', 'configValue',
]

// ── summary helpers ─────────────────────────────────────────────────────────
const MINUS = '−' // U+2212, matches the classic panel's preview typography

function mult(v: number): string { return `×${v}` }
function signedPct(fraction: number): string {
  const pct = Math.round(fraction * 1000) / 10
  return pct >= 0 ? `+${pct}%` : `${MINUS}${Math.abs(pct)}%`
}
function signedFlat(v: number): string {
  return v >= 0 ? `+${v}` : `${MINUS}${Math.abs(v)}`
}
function fieldPreview(op: string | undefined, value: number): string {
  if (op === 'add') return signedFlat(value)
  return mult(value) // multiply / amplify both read as ×value in the summary
}

function summarize(kind: ModifierKind, raw: unknown, labels: ModifierLabels): string {
  switch (kind) {
    case 'unitStat': {
      const m = raw as { stat: string; op: string; value: number }
      return `${labels.statLabel(m.stat)} ${m.op === 'multiply' ? mult(m.value) : signedFlat(m.value)}`
    }
    case 'abilityStat': {
      const m = raw as { stat: string; ability?: string; flat?: number; pct?: number }
      const parts: string[] = []
      if (typeof m.flat === 'number') parts.push(signedFlat(m.flat).endsWith('%') ? signedFlat(m.flat) : signedPct(m.flat))
      if (typeof m.pct === 'number') parts.push(signedPct(m.pct))
      const scope = m.ability ? ` (${labels.abilityLabel(m.ability)})` : ''
      return `${labels.abilityStatLabel(m.stat)} ${parts.join(' ') || '—'}${scope}`
    }
    case 'abilityField': {
      const m = raw as { target: string; action: string; field: string; op?: string; value: number }
      return `${labels.abilityLabel(m.target)} ▸ ${m.action} ▸ ${m.field} ${fieldPreview(m.op, m.value)}`
    }
    case 'abilityModifier': {
      const m = raw as { target: string }
      return labels.abilityLabel(m.target)
    }
    case 'abilityRider': {
      const m = raw as { target: string; actions?: unknown[] }
      return `${labels.abilityLabel(m.target)} (+${m.actions?.length ?? 0} action${(m.actions?.length ?? 0) === 1 ? '' : 's'})`
    }
    case 'aura': {
      const m = raw as { radius: number; targets: string }
      return `${m.targets}, r${m.radius}`
    }
    case 'grantAbility': return labels.abilityLabel(String(raw))
    case 'perkModifier': {
      const m = raw as { target: string; ops?: unknown[] }
      return `${m.target} (${m.ops?.length ?? 0} op${(m.ops?.length ?? 0) === 1 ? '' : 's'})`
    }
    case 'configValue': {
      const [key, value] = raw as [string, number]
      return `${key} = ${value}`
    }
    case 'effect': {
      const m = raw as { name?: string; target?: string }
      return m.name ? `${m.name}${m.target ? ` → ${m.target}` : ''}` : '(effect)'
    }
  }
}

// buildModifierList: the projection. Reads every modifier array in KIND_ORDER
// and emits one entry per element (per key for config, one for effect), each
// addressing back into `form`.
export function buildModifierList(form: AuthoredPerkDef, labels: ModifierLabels): ModifierEntry[] {
  const out: ModifierEntry[] = []
  for (const kind of KIND_ORDER) {
    const meta = KIND_META[kind]
    const source = form[meta.arrayKey]
    if (source == null) continue
    if (meta.shape === 'list') {
      const list = source as unknown[]
      list.forEach((raw, index) => {
        out.push({ kind, meta, arrayKey: meta.arrayKey, index, id: `${kind}:${index}`, summary: summarize(kind, raw, labels) })
      })
    } else if (meta.shape === 'map') {
      Object.entries(source as Record<string, number>).forEach(([k, v], index) => {
        out.push({ kind, meta, arrayKey: meta.arrayKey, index, id: `${kind}:${k}`, summary: summarize(kind, [k, v], labels) })
      })
    } else { // single
      out.push({ kind, meta, arrayKey: meta.arrayKey, index: 0, id: kind, summary: summarize(kind, source, labels) })
    }
  }
  return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run src/components/perk-editor/perkModifierModel.test.ts`
Expected: PASS (5 tests).

- [ ] **Step 5: Type-check**

Run (from `client/src/game-portal/`): `npx vue-tsc -b`
Expected: no new errors.

- [ ] **Step 6: Commit** *(do not run — stop here for the user to commit)*

```bash
git add client/src/game-portal/src/components/perk-editor/perkModifierModel.ts client/src/game-portal/src/components/perk-editor/perkModifierModel.test.ts
git commit -m "feat(perk-editor): pure modifier-list projection model"
```

---

## Task 2: Ability-field option helpers (ported, pure)

These are lifted verbatim (as pure functions) from the classic panel's `walkProgramActions` / `actionsForTarget` / `fieldsForAction` / `abilityFieldRowPreview` so the new Ability Field inspector can offer the same real Action/Field dropdowns.

**Files:**
- Create: `client/src/game-portal/src/components/perk-editor/abilityFieldOptions.ts`
- Test: `client/src/game-portal/src/components/perk-editor/abilityFieldOptions.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// abilityFieldOptions.test.ts
import { describe, expect, it } from 'vitest'
import { actionsForTarget, fieldsForAction, fieldPreview } from './abilityFieldOptions'

const markerTrap = {
  id: 'marker_trap',
  program: {
    triggers: [{
      id: 'cast', type: 'on_cast_complete',
      actions: [{
        id: 'zone', type: 'create_zone',
        config: {
          name: 'Marker Zone', radius: 115, duration: 12,
          triggers: [{
            id: 'entered', type: 'on_tick',
            actions: [
              { id: 'pick_enemy', type: 'select_targets', target: { radius: 110 } },
              { id: 'mark', type: 'apply_status_duration', config: { name: 'Marked', duration: 4 } },
            ],
          }],
        },
      }],
    }],
  },
}
const defs = { marker_trap: markerTrap }

describe('actionsForTarget', () => {
  it('discovers actions nested inside a zone trigger', () => {
    const ids = actionsForTarget(defs, 'marker_trap').map((a) => a.id)
    expect(ids).toContain('zone')
    expect(ids).toContain('mark')       // two levels down
    expect(ids).toContain('pick_enemy')
  })
  it('returns nothing for a tag: target or unknown ability', () => {
    expect(actionsForTarget(defs, 'tag:trap')).toEqual([])
    expect(actionsForTarget(defs, 'nope')).toEqual([])
  })
})

describe('fieldsForAction', () => {
  it('offers only numeric config keys', () => {
    const fields = fieldsForAction(defs, 'marker_trap', 'mark')
    expect(fields).toContain('duration')
    expect(fields).not.toContain('name')
  })
  it('offers target.radius when the action has a target query radius', () => {
    expect(fieldsForAction(defs, 'marker_trap', 'pick_enemy')).toContain('target.radius')
  })
})

describe('fieldPreview', () => {
  it('reads a multiplier as a signed percent', () => {
    expect(fieldPreview('multiply', 1.35)).toBe('+35%')
    expect(fieldPreview('multiply', 0.8)).toBe('−20%')
  })
  it('reads an add as a signed flat', () => {
    expect(fieldPreview('add', 2)).toBe('+2')
    expect(fieldPreview('add', -1)).toBe('−1')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/components/perk-editor/abilityFieldOptions.test.ts`
Expected: FAIL — module not found.

- [ ] **Step 3: Write the implementation** (port from `PerkEditorPanel.vue:698-764`)

```ts
// abilityFieldOptions.ts
// Pure helpers for the Ability Field inspector. Ported from the classic
// PerkEditorPanel so both editors offer the identical, program-derived
// Action/Field dropdowns. `defs` is the map of ability id -> { id, program }
// (from fetchAuthoredAbilityDefs()).

export type AbilityDefLite = { id: string; program?: unknown }

interface WalkedAction { id: string; type: string; config?: Record<string, unknown> }

// walkProgramActions yields every action at any depth: nested children, a
// create_zone's own triggers, an apply_status_duration's triggers. A perk
// targets by AUTHORED ACTION ID, and those ids live at every level.
export function walkProgramActions(program: unknown): WalkedAction[] {
  const out: WalkedAction[] = []
  const seen = new Set<unknown>()
  const visitActions = (actions: unknown) => {
    if (!Array.isArray(actions)) return
    for (const raw of actions) {
      const a = raw as { id?: string; type?: string; config?: Record<string, unknown>; children?: unknown }
      if (!a || typeof a !== 'object') continue
      if (a.id) out.push({ id: a.id, type: String(a.type ?? ''), config: a.config })
      visitTriggers(a.children)
      if (a.config && typeof a.config === 'object') visitTriggers((a.config as { triggers?: unknown }).triggers)
    }
  }
  const visitTriggers = (triggers: unknown) => {
    if (!Array.isArray(triggers) || seen.has(triggers)) return
    seen.add(triggers)
    for (const raw of triggers) {
      const t = raw as { actions?: unknown }
      if (t && typeof t === 'object') visitActions(t.actions)
    }
  }
  const p = program as { triggers?: unknown; namedTriggers?: Record<string, unknown> } | undefined
  visitTriggers(p?.triggers)
  for (const t of Object.values(p?.namedTriggers ?? {})) visitTriggers([t])
  return out
}

export function actionsForTarget(defs: Record<string, AbilityDefLite>, target: string): { id: string; label: string }[] {
  const def = defs[target.trim()]
  if (!def) return [] // includes "tag:" targets — no single program to show
  return walkProgramActions(def.program).map((a) => ({ id: a.id, label: a.type ? `${a.id} (${a.type})` : a.id }))
}

export function fieldsForAction(defs: Record<string, AbilityDefLite>, target: string, actionID: string): string[] {
  const def = defs[target.trim()]
  if (!def || !actionID) return []
  const action = walkProgramActions(def.program).find((a) => a.id === actionID)
  if (!action) return []
  const out = Object.entries(action.config ?? {}).filter(([, v]) => typeof v === 'number').map(([k]) => k)
  const raw = action as unknown as { target?: { radius?: unknown } }
  if (typeof raw.target?.radius === 'number') out.push('target.radius')
  return out.sort()
}

// fieldPreview: "+35%" for a ×1.35 multiply, "+2" for an add. U+2212 minus to
// match the rest of the editor's typography.
export function fieldPreview(op: string | undefined, value: number): string {
  if (typeof value !== 'number' || Number.isNaN(value)) return ''
  if (op === 'add') return value >= 0 ? `+${value}` : `−${Math.abs(value)}`
  const pct = Math.round((value - 1) * 1000) / 10
  if (pct === 0) return 'no change'
  return pct > 0 ? `+${pct}%` : `−${Math.abs(pct)}%`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run src/components/perk-editor/abilityFieldOptions.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit** *(do not run)*

```bash
git add client/src/game-portal/src/components/perk-editor/abilityFieldOptions.ts client/src/game-portal/src/components/perk-editor/abilityFieldOptions.test.ts
git commit -m "feat(perk-editor): pure ability-field option helpers"
```

---

## Task 3: The builder context + composable

**Files:**
- Create: `client/src/game-portal/src/components/perk-editor/PerkBuilderContext.ts`
- Create: `client/src/game-portal/src/components/perk-editor/usePerkBuilder.ts`

There is no separate unit test for the composable — its behavior is exercised through `PerkBuilderPanel.test.ts` (Task 9), mirroring how `useAbilityBuilder` is covered through its panel.

- [ ] **Step 1: Write `PerkBuilderContext.ts`**

```ts
// PerkBuilderContext.ts — provide/inject wiring, mirrors AbilityBuilderContext.
import { inject, type InjectionKey } from 'vue'
import type { usePerkBuilder } from './usePerkBuilder'

export type PerkBuilder = ReturnType<typeof usePerkBuilder>
export const PerkBuilderKey: InjectionKey<PerkBuilder> = Symbol('perkBuilder')

export function usePerkBuilderContext(): PerkBuilder {
  const b = inject(PerkBuilderKey)
  if (!b) throw new Error('PerkBuilder not provided')
  return b
}
```

- [ ] **Step 2: Write `usePerkBuilder.ts`**

```ts
// usePerkBuilder.ts — the single state object PerkBuilderPanel provides. Holds
// the loaded catalog, the open perk's form, the selected modifier, and every
// mutation. `form` stays an AuthoredPerkDef; modifier edits write back into its
// arrays in place, so untouched arrays round-trip byte-for-byte.
import { computed, ref, shallowRef } from 'vue'
import {
  createBlankForm, formFromDef, saveRequestFromForm,
  type AuthoredPerkDef, type PerkEditorForm,
} from '@/game/perks/perkEditorForm'
import {
  fetchAuthoredPerkDefs, saveEditorPerk, deleteEditorPerk, EditorValidationError,
} from '@/game/perks/perkEditorApi'
import { fetchAuthoredAbilityDefs } from '@/game/abilities/abilityEditorApi'
import { fetchUnitDefs } from '@/game/maps/catalog'
import { allStatDefs, selfStatDefs } from '@/game/stats/statRegistry'
import { buildModifierList, KIND_META, type ModifierKind, type ModifierLabels } from './perkModifierModel'
import type { AbilityDefLite } from './abilityFieldOptions'

export interface AbilityStatDef { id: string; label: string; flatOnly?: boolean; inflicted?: boolean }
/** Address of the selected modifier: which array + which index. */
export interface Selection { arrayKey: keyof AuthoredPerkDef; index: number }

export function usePerkBuilder() {
  const perks = ref<AuthoredPerkDef[]>([])
  const form = ref<PerkEditorForm>(createBlankForm())
  const selectedId = ref<string | null>(null)
  const selected = shallowRef<Selection | null>(null)
  const busy = ref(false)
  const loadError = ref('')
  const saveError = ref('')
  const statusMessage = ref('')

  // Catalogs
  const abilityIds = ref<string[]>([])
  const abilityDefsById = ref<Record<string, AbilityDefLite>>({})
  const abilityStatDefs = ref<AbilityStatDef[]>([])
  const pathsByUnit = ref<Record<string, string[]>>({})

  const selfStatDefsList = selfStatDefs()
  const auraStatDefsList = allStatDefs()

  // Label lookups for the projection summaries.
  const labels = computed<ModifierLabels>(() => ({
    statLabel: (id) => selfStatDefsList.find((d) => d.id === id)?.label
      ?? auraStatDefsList.find((d) => d.id === id)?.label ?? id,
    abilityStatLabel: (id) => abilityStatDefs.value.find((d) => d.id === id)?.label ?? id,
    abilityLabel: (id) => perks.value.length ? id : id, // ability display names not fetched; id is the label
  }))

  const modifiers = computed(() => buildModifierList(form.value, labels.value))
  const selectedEntry = computed(() =>
    selected.value == null ? null
      : modifiers.value.find((e) => e.arrayKey === selected.value!.arrayKey && e.index === selected.value!.index) ?? null)

  function selectModifier(sel: Selection | null) { selected.value = sel }

  // ── mutation helpers ───────────────────────────────────────────────────────
  // All writes go through replaceArray so form.value is reassigned (keeps the
  // computed projection + any watchers reactive) rather than mutated in place.
  function replaceArray(kind: ModifierKind, next: unknown[]) {
    const key = KIND_META[kind].arrayKey
    form.value = { ...form.value, [key]: next.length ? next : undefined }
  }
  function listFor(kind: ModifierKind): unknown[] {
    return ((form.value[KIND_META[kind].arrayKey] as unknown[]) ?? []).slice()
  }

  const DEFAULTS: Partial<Record<ModifierKind, () => unknown>> = {
    unitStat: () => ({ stat: selfStatDefsList[0]?.id ?? '', op: 'add', value: 0 }),
    abilityStat: () => ({ stat: '' }),
    abilityField: () => ({ target: '', action: '', field: '', op: 'multiply', value: 0 }),
  }

  function addModifier(kind: ModifierKind) {
    const make = DEFAULTS[kind]
    if (!make) return // un-migrated kinds are added via the classic editor for now
    const next = listFor(kind)
    next.push(make())
    replaceArray(kind, next)
    selected.value = { arrayKey: KIND_META[kind].arrayKey, index: next.length - 1 }
  }

  function removeModifier(sel: Selection) {
    const kind = (Object.keys(KIND_META) as ModifierKind[]).find((k) => KIND_META[k].arrayKey === sel.arrayKey)!
    const next = listFor(kind)
    next.splice(sel.index, 1)
    replaceArray(kind, next)
    if (selected.value && selected.value.arrayKey === sel.arrayKey && selected.value.index === sel.index) selected.value = null
  }

  function duplicateModifier(sel: Selection) {
    const kind = (Object.keys(KIND_META) as ModifierKind[]).find((k) => KIND_META[k].arrayKey === sel.arrayKey)!
    const next = listFor(kind)
    next.splice(sel.index + 1, 0, structuredClone(next[sel.index]))
    replaceArray(kind, next)
    selected.value = { arrayKey: sel.arrayKey, index: sel.index + 1 }
  }

  // updateSelected replaces the selected element with a new object (callers
  // build the cleaned wire object; blank-stripping lives in the inspector).
  function updateSelected(next: unknown) {
    if (!selected.value) return
    const kind = (Object.keys(KIND_META) as ModifierKind[]).find((k) => KIND_META[k].arrayKey === selected.value!.arrayKey)!
    const list = listFor(kind)
    list[selected.value.index] = next
    replaceArray(kind, list)
  }

  // ── load / select / persist ────────────────────────────────────────────────
  async function reload() {
    try { perks.value = await fetchAuthoredPerkDefs(); loadError.value = '' }
    catch (e) { loadError.value = e instanceof Error ? e.message : String(e) }
  }

  function selectPerk(def: AuthoredPerkDef) {
    form.value = formFromDef(def)
    selectedId.value = def.id
    selected.value = null
    saveError.value = ''; statusMessage.value = ''
  }

  function newPerk() {
    form.value = createBlankForm()
    selectedId.value = null
    selected.value = null
    saveError.value = ''; statusMessage.value = ''
  }

  async function save() {
    saveError.value = ''; statusMessage.value = ''; busy.value = true
    try {
      await saveEditorPerk(saveRequestFromForm(form.value))
      await reload()
      selectedId.value = form.value.id
      statusMessage.value = 'Saved.'
    } catch (e) {
      saveError.value = e instanceof EditorValidationError ? e.serverMessage : e instanceof Error ? e.message : String(e)
    } finally { busy.value = false }
  }

  async function removePerk(): Promise<'deleted' | 'reset' | null> {
    if (!selectedId.value) return null
    busy.value = true; saveError.value = ''; statusMessage.value = ''
    try {
      const status = await deleteEditorPerk(selectedId.value)
      await reload(); newPerk()
      statusMessage.value = status === 'deleted' ? 'Deleted.' : 'Reset to default.'
      return status
    } catch (e) {
      saveError.value = e instanceof EditorValidationError ? e.serverMessage : e instanceof Error ? e.message : String(e)
      return null
    } finally { busy.value = false }
  }

  async function load() {
    await reload()
    try { pathsByUnit.value = (await fetchUnitDefs()).pathsByUnit } catch { pathsByUnit.value = {} }
    try {
      const defs = await fetchAuthoredAbilityDefs()
      abilityIds.value = defs.map((a) => a.id)
      abilityDefsById.value = Object.fromEntries(defs.map((a) => [a.id, a]))
    } catch { abilityIds.value = []; abilityDefsById.value = {} }
    try {
      const res = await fetch('/catalog/ability-stats')
      if (res.ok) abilityStatDefs.value = ((await res.json()) as { stats?: AbilityStatDef[] }).stats ?? []
    } catch { /* offline: leave empty */ }
  }

  return {
    // state
    perks, form, selectedId, selected, busy, loadError, saveError, statusMessage,
    abilityIds, abilityDefsById, abilityStatDefs, pathsByUnit, selfStatDefsList, auraStatDefsList,
    // derived
    modifiers, selectedEntry,
    // actions
    load, reload, selectPerk, newPerk, save, removePerk,
    selectModifier, addModifier, removeModifier, duplicateModifier, updateSelected,
  }
}
```

- [ ] **Step 3: Type-check**

Run (from `client/src/game-portal/`): `npx vue-tsc -b`
Expected: no new errors. (If `fetchAuthoredAbilityDefs`/`fetchUnitDefs`/`fetchAuthoredPerkDefs` import paths differ, fix to match `PerkEditorPanel.vue`'s imports — they are the same functions used there.)

- [ ] **Step 4: Commit** *(do not run)*

```bash
git add client/src/game-portal/src/components/perk-editor/PerkBuilderContext.ts client/src/game-portal/src/components/perk-editor/usePerkBuilder.ts
git commit -m "feat(perk-editor): usePerkBuilder composable + context"
```

---

## Task 4: PerkModifierCard (read-only summary card)

**Files:**
- Create: `client/src/game-portal/src/components/perk-editor/PerkModifierCard.vue`

- [ ] **Step 1: Write the component**

```vue
<template>
  <div
    class="pm-card"
    :class="{ 'pm-card--on': selected }"
    :style="{ '--mk-accent': entry.meta.accent }"
    :data-test="`perk-modifier-card`"
    :data-kind="entry.kind"
    role="button"
    tabindex="0"
    @click="$emit('select')"
    @keydown.enter.prevent="$emit('select')"
    @keydown.space.prevent="$emit('select')"
  >
    <span class="pm-card__accent" aria-hidden="true" />
    <span class="pm-card__icon" aria-hidden="true">{{ entry.meta.icon }}</span>
    <span class="pm-card__body">
      <span class="pm-card__type">{{ entry.meta.label }}</span>
      <span class="pm-card__summary">{{ entry.summary }}</span>
    </span>
    <span class="pm-card__actions">
      <button type="button" class="pm-card__act" title="Duplicate" aria-label="Duplicate modifier" @click.stop="$emit('duplicate')">⧉</button>
      <button type="button" class="pm-card__act" title="Delete" aria-label="Delete modifier" @click.stop="$emit('delete')">✕</button>
    </span>
  </div>
</template>

<script setup lang="ts">
import type { ModifierEntry } from './perkModifierModel'
defineProps<{ entry: ModifierEntry; selected: boolean }>()
defineEmits<{ select: []; duplicate: []; delete: [] }>()
</script>

<style scoped>
/* Dark-steel card, thin bronze border, per-kind accent bar, soft inner shadow —
   the forge-theme .ed-card look, applied by hand here since a modifier card is
   a bespoke compact row rather than a titled SectionCard. */
.pm-card {
  position: relative;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px 8px 14px;
  background: #0b0906;
  border: 1px solid rgba(226, 182, 92, 0.34);
  border-radius: var(--ed-radius);
  box-shadow: inset 0 1px 0 rgba(242, 208, 144, 0.06);
  min-width: 0;
}
.pm-card--on {
  border-color: var(--ed-brass);
  box-shadow: 0 0 0 1px rgba(212, 168, 71, 0.35), inset 0 1px 0 rgba(242, 208, 144, 0.06);
}
.pm-card__accent {
  position: absolute;
  left: 0; top: 4px; bottom: 4px;
  width: 3px;
  border-radius: 3px;
  background: var(--mk-accent);
}
.pm-card__icon {
  flex: 0 0 auto;
  width: 20px;
  text-align: center;
  color: var(--mk-accent);
  font-size: 0.95rem;
}
.pm-card__body { display: flex; flex-direction: column; gap: 1px; min-width: 0; flex: 1 1 auto; }
.pm-card__type {
  font-size: 0.62rem;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--mk-accent);
}
.pm-card__summary {
  font-size: 0.82rem;
  color: var(--ed-text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.pm-card__actions { display: flex; gap: 2px; opacity: 0; flex: 0 0 auto; }
.pm-card:hover .pm-card__actions,
.pm-card--on .pm-card__actions { opacity: 1; }
.pm-card__act {
  padding: 2px 6px;
  font-size: 0.76rem;
  line-height: 1;
  color: var(--ed-text-dim);
  background: none;
  border: 1px solid transparent;
  border-radius: 4px;
}
.pm-card__act:hover { color: var(--ed-brass); border-color: var(--ed-line); }
</style>
```

- [ ] **Step 2: Type-check**

Run: `npx vue-tsc -b`
Expected: no new errors.

- [ ] **Step 3: Commit** *(do not run)*

```bash
git add client/src/game-portal/src/components/perk-editor/PerkModifierCard.vue
git commit -m "feat(perk-editor): read-only modifier summary card"
```

---

## Task 5: PerkModifierStack (card list + add menu + quick-add)

**Files:**
- Create: `client/src/game-portal/src/components/perk-editor/PerkModifierStack.vue`

- [ ] **Step 1: Write the component**

```vue
<template>
  <div class="pm-stack">
    <p v-if="builder.modifiers.value.length === 0" class="pm-stack__empty">
      No modifiers yet. Add one below to describe what this perk does.
    </p>

    <PerkModifierCard
      v-for="entry in builder.modifiers.value"
      :key="entry.id"
      :entry="entry"
      :selected="isSelected(entry)"
      @select="builder.selectModifier({ arrayKey: entry.arrayKey, index: entry.index })"
      @duplicate="builder.duplicateModifier({ arrayKey: entry.arrayKey, index: entry.index })"
      @delete="builder.removeModifier({ arrayKey: entry.arrayKey, index: entry.index })"
    />

    <div class="pm-stack__add">
      <div class="pm-stack__add-menu">
        <button type="button" class="pm-stack__add-btn" data-test="add-modifier" @click="menuOpen = !menuOpen">
          + Add Modifier
        </button>
        <ul v-if="menuOpen" class="pm-stack__menu" role="menu">
          <li v-for="k in ADDABLE" :key="k">
            <button
              type="button"
              role="menuitem"
              :style="{ '--mk-accent': KIND_META[k].accent }"
              @click="pick(k)"
            >
              <span class="pm-stack__menu-dot" aria-hidden="true" />
              {{ KIND_META[k].label }}
              <span v-if="!KIND_META[k].editable" class="pm-stack__menu-soon">classic</span>
            </button>
          </li>
        </ul>
      </div>

      <!-- Quick-add: shortcuts to the same addModifier for the migrated kinds. -->
      <div class="pm-stack__quick">
        <button v-for="k in QUICK" :key="k" type="button" :data-test="`quick-add-${k}`" @click="pick(k)">
          + {{ SHORT[k] }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import PerkModifierCard from './PerkModifierCard.vue'
import { usePerkBuilderContext } from './PerkBuilderContext'
import { KIND_META, KIND_ORDER, type ModifierEntry, type ModifierKind } from './perkModifierModel'

const builder = usePerkBuilderContext()
const menuOpen = ref(false)

// Full add-menu (every kind, in render order); un-migrated kinds are marked
// "classic" and, when picked, route the user to the classic editor for now.
const ADDABLE = KIND_ORDER
// Quick-add shows only the kinds the inspector can fully edit this slice.
const QUICK: ModifierKind[] = KIND_ORDER.filter((k) => KIND_META[k].editable)
const SHORT: Record<ModifierKind, string> = {
  unitStat: 'Unit Stat', abilityStat: 'Ability Stat', abilityField: 'Ability Field',
  abilityModifier: 'Ability Mod', abilityRider: 'Rider', aura: 'Aura',
  grantAbility: 'Grant', perkModifier: 'Perk Mod', configValue: 'Config', effect: 'Effect',
}

function isSelected(e: ModifierEntry): boolean {
  const s = builder.selected.value
  return !!s && s.arrayKey === e.arrayKey && s.index === e.index
}

function pick(kind: ModifierKind) {
  menuOpen.value = false
  if (!KIND_META[kind].editable) {
    // Un-migrated: nothing to add here yet. Surface it rather than silently no-op.
    builder.saveError.value = `${KIND_META[kind].label} isn't editable in the new builder yet — use the Classic editor.`
    return
  }
  builder.addModifier(kind)
}
</script>

<style scoped>
.pm-stack { display: flex; flex-direction: column; gap: 8px; min-width: 0; }
.pm-stack__empty { margin: 4px 2px; font-size: 0.82rem; color: var(--ed-text-dim); }
.pm-stack__add { margin-top: 4px; display: flex; flex-direction: column; gap: 8px; }
.pm-stack__add-menu { position: relative; }
.pm-stack__add-btn {
  width: 100%;
  padding: 8px;
  border: 1px dashed var(--ed-line-strong);
  border-radius: var(--ed-radius);
  background: rgba(212, 168, 71, 0.06);
  color: var(--ed-brass);
  font-weight: 700;
  font-size: 0.8rem;
}
.pm-stack__menu {
  position: absolute; z-index: 5; left: 0; right: 0; top: calc(100% + 4px);
  margin: 0; padding: 4px; list-style: none;
  background: var(--ed-sticky-bg);
  border: 1px solid var(--ed-line-strong);
  border-radius: var(--ed-radius);
  box-shadow: 0 8px 20px rgba(0, 0, 0, 0.5);
}
.pm-stack__menu button {
  display: flex; align-items: center; gap: 8px; width: 100%;
  padding: 6px 8px; background: none; border: 0; border-radius: 4px;
  color: var(--ed-text); font-size: 0.8rem; text-align: left;
}
.pm-stack__menu button:hover { background: rgba(212, 168, 71, 0.1); }
.pm-stack__menu-dot { width: 8px; height: 8px; border-radius: 2px; background: var(--mk-accent); flex: 0 0 auto; }
.pm-stack__menu-soon { margin-left: auto; font-size: 0.62rem; letter-spacing: 0.08em; text-transform: uppercase; color: var(--ed-text-dim); }
.pm-stack__quick { display: flex; flex-wrap: wrap; gap: 6px; }
.pm-stack__quick button {
  padding: 4px 8px; font-size: 0.72rem;
  border: 1px solid var(--ed-line); border-radius: 4px;
  background: var(--ed-field); color: var(--ed-text-dim);
}
.pm-stack__quick button:hover { color: var(--ed-brass); border-color: var(--ed-line-strong); }
</style>
```

- [ ] **Step 2: Type-check + Commit** *(do not run commit)*

Run: `npx vue-tsc -b` → no new errors.

```bash
git add client/src/game-portal/src/components/perk-editor/PerkModifierStack.vue
git commit -m "feat(perk-editor): modifier stack with add menu + quick-add"
```

---

## Task 6: PerkModifierInspector (edits the selected card)

**Files:**
- Create: `client/src/game-portal/src/components/perk-editor/PerkModifierInspector.vue`

This is the edit surface. It reads `builder.selectedEntry`, reads the underlying element from `form`, and on every change rebuilds a **cleaned** wire object and calls `builder.updateSelected(...)`. Blank-stripping / percent-conversion rules are ported from the classic panel.

- [ ] **Step 1: Write the component**

```vue
<template>
  <SectionCard v-if="entry" :title="entry.meta.label" :style="{ '--mk-accent': entry.meta.accent }" data-test="perk-inspector">
    <!-- ── Unit Stat Modifier ─────────────────────────────────────────────── -->
    <template v-if="entry.kind === 'unitStat'">
      <EditorField label="Stat">
        <select v-model="unitStat.stat" aria-label="Stat" @change="commitUnitStat">
          <option v-for="d in builder.selfStatDefsList" :key="d.id" :value="d.id">{{ d.label }}</option>
        </select>
      </EditorField>
      <EditorField label="Operation">
        <select v-model="unitStat.op" aria-label="Operation" @change="commitUnitStat">
          <option value="add">Add</option>
          <option value="multiply">Multiply</option>
        </select>
      </EditorField>
      <EditorField label="Value">
        <input v-model.number="unitStat.value" type="number" step="0.05" aria-label="Value" @input="commitUnitStat" />
      </EditorField>
      <EditorField label="Stage">
        <select v-model="unitStat.stage" aria-label="Stage" @change="commitUnitStat">
          <option value="intrinsic">Intrinsic (scales this unit's own base only)</option>
          <option value="base">Base</option>
          <option value="final">Final (applied after everything else)</option>
        </select>
      </EditorField>
    </template>

    <!-- ── Ability Stat Modifier ──────────────────────────────────────────── -->
    <template v-else-if="entry.kind === 'abilityStat'">
      <EditorField label="Stat">
        <select v-model="abilityStat.stat" aria-label="Ability Stat" @change="commitAbilityStat">
          <option value="">Pick a stat…</option>
          <optgroup label="Ability shape">
            <option v-for="d in shapeStats" :key="d.id" :value="d.id">{{ d.label }}</option>
          </optgroup>
          <optgroup label="Applies to target">
            <option v-for="d in targetStats" :key="d.id" :value="d.id">{{ d.label }}</option>
          </optgroup>
          <option v-if="abilityStat.stat && !knownStat" :value="abilityStat.stat">{{ abilityStat.stat }}</option>
        </select>
      </EditorField>
      <EditorField label="Ability Filter" hint="(blank = all abilities)">
        <input v-model="abilityStat.ability" list="perk-builder-ability-ids" aria-label="Ability" @input="commitAbilityStat" />
      </EditorField>
      <EditorField label="Flat">
        <input v-model.number="abilityStat.flat" type="number" step="0.5" aria-label="Flat" @input="commitAbilityStat" />
      </EditorField>
      <EditorField v-if="allowsPct" label="Percent" hint="(whole %)">
        <input v-model.number="abilityStat.pct" type="number" step="5" aria-label="Percent" @input="commitAbilityStat" />
      </EditorField>
    </template>

    <!-- ── Ability Field Modifier ─────────────────────────────────────────── -->
    <template v-else-if="entry.kind === 'abilityField'">
      <EditorField label="Ability">
        <input v-model="abilityField.target" list="perk-builder-ability-ids" placeholder="ability or tag:trap" aria-label="Ability" @input="commitAbilityField" />
      </EditorField>
      <EditorField label="Action">
        <select v-model="abilityField.action" aria-label="Action" @change="commitAbilityField">
          <option value="">Pick an action…</option>
          <option v-for="a in actions" :key="a.id" :value="a.id">{{ a.label }}</option>
          <option v-if="abilityField.action && !actions.some((a) => a.id === abilityField.action)" :value="abilityField.action">
            {{ abilityField.action }} (not in this ability)
          </option>
        </select>
      </EditorField>
      <EditorField label="Field">
        <select v-model="abilityField.field" aria-label="Field" @change="commitAbilityField">
          <option value="">Pick a field…</option>
          <option v-for="f in fields" :key="f" :value="f">{{ f }}</option>
          <option v-if="abilityField.field && !fields.includes(abilityField.field)" :value="abilityField.field">
            {{ abilityField.field }} (not on this action)
          </option>
        </select>
      </EditorField>
      <EditorField label="Operation">
        <select v-model="abilityField.op" aria-label="Operation" @change="commitAbilityField">
          <option value="multiply">× multiply</option>
          <option value="add">+ add</option>
          <option value="amplify">amplify</option>
        </select>
      </EditorField>
      <EditorField label="Value">
        <input v-model.number="abilityField.value" type="number" step="0.05" aria-label="Value" @input="commitAbilityField" />
      </EditorField>
      <EditorField label="Stage">
        <select v-model="abilityField.stage" aria-label="Stage" @change="commitAbilityField">
          <option value="">Base</option>
          <option value="intrinsic">Intrinsic</option>
          <option value="final">Final</option>
        </select>
      </EditorField>
      <p class="pi-preview">{{ fieldPreview(abilityField.op, abilityField.value as number) }}</p>
    </template>

    <!-- ── Un-migrated kinds ──────────────────────────────────────────────── -->
    <template v-else>
      <p class="pi-note">
        <strong>{{ entry.summary }}</strong>
      </p>
      <p class="pi-note pi-note--dim">
        Editing <em>{{ entry.meta.label }}</em> in the new builder is coming soon.
        Open this perk in the <strong>Classic</strong> editor to change it. Saving here
        preserves it unchanged.
      </p>
    </template>
  </SectionCard>

  <div v-else class="pi-empty" data-test="perk-inspector-empty">
    <p>Select a modifier to edit it, or add one from the stack.</p>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import SectionCard from '@/components/editor/SectionCard.vue'
import EditorField from '@/components/editor/EditorField.vue'
import { usePerkBuilderContext } from './PerkBuilderContext'
import { actionsForTarget, fieldsForAction, fieldPreview } from './abilityFieldOptions'
import type { PerkStatModifier, PerkAbilityStat, AbilityFieldModifier } from '@/game/perks/perkEditorForm'

const builder = usePerkBuilderContext()
const entry = computed(() => builder.selectedEntry.value)

// Local editable drafts, re-seeded whenever the selection changes. Each commit*
// rebuilds the cleaned wire object and writes it back through updateSelected.
const unitStat = reactive({ stat: '', op: 'add' as 'add' | 'multiply', value: 0 as number | '', stage: 'base' as 'intrinsic' | 'base' | 'final' })
const abilityStat = reactive({ stat: '', ability: '', flat: '' as number | '', pct: '' as number | '' })
const abilityField = reactive({ target: '', action: '', field: '', op: 'multiply', value: '' as number | '', stage: '' })

function current<T>(): T | undefined {
  const e = entry.value
  if (!e) return undefined
  return (builder.form.value[e.arrayKey] as unknown[])?.[e.index] as T
}

watch(entry, (e) => {
  if (!e) return
  if (e.kind === 'unitStat') {
    const m = current<PerkStatModifier>()!
    Object.assign(unitStat, { stat: m.stat, op: m.op, value: m.value, stage: m.stage ?? 'base' })
  } else if (e.kind === 'abilityStat') {
    const m = current<PerkAbilityStat>()!
    Object.assign(abilityStat, {
      stat: m.stat, ability: m.ability ?? '',
      flat: m.flat ?? '', pct: m.pct === undefined ? '' : Math.round(m.pct * 1000) / 10,
    })
  } else if (e.kind === 'abilityField') {
    const m = current<AbilityFieldModifier>()!
    Object.assign(abilityField, { target: m.target ?? '', action: m.action ?? '', field: m.field ?? '', op: m.op || 'multiply', value: m.value ?? '', stage: m.stage ?? '' })
  }
}, { immediate: true })

// ── Unit Stat: omit stage when "base" (server default) to round-trip byte-for-byte
function commitUnitStat() {
  const value = typeof unitStat.value === 'number' && !Number.isNaN(unitStat.value) ? unitStat.value : 0
  const next: PerkStatModifier = { stat: unitStat.stat, op: unitStat.op, value, ...(unitStat.stage !== 'base' ? { stage: unitStat.stage } : {}) }
  builder.updateSelected(next)
}

// ── Ability Stat: whole percent in UI -> fraction on wire; drop 0/blank fields
const knownStat = computed(() => builder.abilityStatDefs.value.some((d) => d.id === abilityStat.stat))
const shapeStats = computed(() => builder.abilityStatDefs.value.filter((d) => !d.inflicted))
const targetStats = computed(() => builder.abilityStatDefs.value.filter((d) => d.inflicted))
const allowsPct = computed(() => {
  const def = builder.abilityStatDefs.value.find((d) => d.id === abilityStat.stat)
  return !def?.flatOnly
})
function commitAbilityStat() {
  const next: PerkAbilityStat = { stat: abilityStat.stat.trim() }
  const ability = abilityStat.ability.trim()
  if (ability) next.ability = ability
  if (typeof abilityStat.flat === 'number' && !Number.isNaN(abilityStat.flat) && abilityStat.flat !== 0) next.flat = abilityStat.flat
  if (allowsPct.value && typeof abilityStat.pct === 'number' && !Number.isNaN(abilityStat.pct) && abilityStat.pct !== 0) next.pct = abilityStat.pct / 100
  builder.updateSelected(next)
}

// ── Ability Field: multiply/blank-stage omitted; Action/Field options from the program
const actions = computed(() => actionsForTarget(builder.abilityDefsById.value, abilityField.target))
const fields = computed(() => fieldsForAction(builder.abilityDefsById.value, abilityField.target, abilityField.action))
function commitAbilityField() {
  const value = typeof abilityField.value === 'number' && !Number.isNaN(abilityField.value) ? abilityField.value : 0
  const next: AbilityFieldModifier = { target: abilityField.target.trim(), action: abilityField.action.trim(), field: abilityField.field.trim(), value }
  if (abilityField.op && abilityField.op !== 'multiply') next.op = abilityField.op
  if (abilityField.stage) next.stage = abilityField.stage
  builder.updateSelected(next)
}
</script>

<style scoped>
.pi-empty { padding: 16px; color: var(--ed-text-dim); font-size: 0.82rem; }
.pi-note { margin: 0; font-size: 0.82rem; color: var(--ed-text); }
.pi-note--dim { color: var(--ed-text-dim); }
.pi-preview { margin: 0; font-size: 0.8rem; color: var(--mk-accent); font-weight: 700; }
</style>
```

- [ ] **Step 2: Type-check + Commit** *(do not run commit)*

Run: `npx vue-tsc -b` → no new errors.

```bash
git add client/src/game-portal/src/components/perk-editor/PerkModifierInspector.vue
git commit -m "feat(perk-editor): modifier inspector (unit/ability stat + ability field)"
```

---

## Task 7: PerkSummaryCard + PerkSetupColumn

**Files:**
- Create: `client/src/game-portal/src/components/perk-editor/PerkSummaryCard.vue`
- Create: `client/src/game-portal/src/components/perk-editor/PerkSetupColumn.vue`

- [ ] **Step 1: Write `PerkSummaryCard.vue`**

```vue
<template>
  <div class="ps-summary" @click="builder.selectModifier(null)" data-test="perk-summary">
    <img v-if="iconUrl" :src="iconUrl" class="ps-summary__icon" alt="" />
    <div v-else class="ps-summary__icon ps-summary__icon--empty" aria-hidden="true">◆</div>
    <div class="ps-summary__body">
      <div class="ps-summary__head">
        <span class="ps-summary__name">{{ form.displayName || form.id || 'New Perk' }}</span>
        <span class="ps-summary__badge" :class="form.wired ? 'is-wired' : 'is-inert'">
          {{ form.wired ? 'Wired' : 'Not Wired' }}
        </span>
      </div>
      <div class="ps-summary__meta">
        <span class="ps-summary__id">{{ form.id }}</span>
        <span v-if="form.path" class="ps-summary__chip">{{ form.path }}</span>
        <span class="ps-summary__chip">{{ count }} Modifier{{ count === 1 ? '' : 's' }}</span>
      </div>
      <p v-if="form.description" class="ps-summary__desc">{{ form.description }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { usePerkBuilderContext } from './PerkBuilderContext'

const builder = usePerkBuilderContext()
const form = computed(() => builder.form.value)
const count = computed(() => builder.modifiers.value.length)
// Perk icons are keyed strings, not URLs, in this slice — show the placeholder
// glyph. (Wiring the real icon renderer is a follow-up; the classic editor also
// only shows the raw key.)
const iconUrl = computed<string | undefined>(() => undefined)
</script>

<style scoped>
.ps-summary {
  display: flex; gap: 12px; align-items: flex-start;
  padding: 12px;
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
  background: rgba(15, 23, 42, 0.25);
}
.ps-summary__icon { width: 48px; height: 48px; flex: 0 0 auto; object-fit: contain; image-rendering: pixelated; border-radius: 6px; }
.ps-summary__icon--empty { display: flex; align-items: center; justify-content: center; color: var(--ed-brass-dim); background: var(--ed-field); }
.ps-summary__body { min-width: 0; flex: 1 1 auto; display: flex; flex-direction: column; gap: 4px; }
.ps-summary__head { display: flex; align-items: center; gap: 8px; }
.ps-summary__name { font-family: var(--font-title); font-size: 1rem; font-weight: 700; color: var(--ed-text); }
.ps-summary__badge { font-size: 0.6rem; font-weight: 700; letter-spacing: 0.08em; text-transform: uppercase; padding: 2px 6px; border-radius: 4px; }
.ps-summary__badge.is-wired { color: var(--ed-ok); border: 1px solid var(--ed-ok); }
.ps-summary__badge.is-inert { color: var(--ed-danger); border: 1px solid var(--ed-danger); }
.ps-summary__meta { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
.ps-summary__id { font-size: 0.72rem; color: var(--ed-text-dim); font-family: var(--font-mono, monospace); }
.ps-summary__chip { font-size: 0.66rem; padding: 1px 6px; border-radius: 10px; background: var(--ed-field); color: var(--ed-text-dim); }
.ps-summary__desc { margin: 2px 0 0; font-size: 0.8rem; color: var(--ed-text-dim); }
</style>
```

- [ ] **Step 2: Write `PerkSetupColumn.vue`** (Identity / Eligibility / Tooltip / Config / Config-By-Rank)

```vue
<template>
  <GameScrollArea class="ps-setup">
    <SectionCard title="Identity">
      <EditorField label="Id">
        <input v-model="form.id" :disabled="builder.selectedId.value !== null" aria-label="Id" />
      </EditorField>
      <EditorField label="Display Name">
        <input v-model="form.displayName" aria-label="Display Name" />
      </EditorField>
      <EditorField label="Description">
        <textarea v-model="form.description" rows="2" aria-label="Description" />
      </EditorField>
      <EditorField label="Icon">
        <input v-model="form.icon" aria-label="Icon" />
      </EditorField>
    </SectionCard>

    <SectionCard title="Eligibility">
      <EditorField label="Association" hint="(catalog folder)">
        <select v-if="builder.selectedId.value === null" v-model="association" data-test="association-select">
          <option value="">Generic</option>
          <optgroup v-for="[unit, ps] in sortedPathsByUnit" :key="unit" :label="unitLabel(unit)">
            <option v-for="p in ps" :key="p" :value="p">{{ p }}</option>
          </optgroup>
        </select>
        <input v-else :value="form.path || 'generic'" disabled />
      </EditorField>
      <EditorField label="Requires Perk">
        <input v-model="form.requiresPerk" list="perk-builder-perk-ids" placeholder="(none)" />
      </EditorField>
      <EditorField label="Requires Ability">
        <input v-model="form.requiresAbility" list="perk-builder-ability-ids" placeholder="(none)" />
      </EditorField>
    </SectionCard>

    <SectionCard title="Tooltip">
      <EditorField label="Generated" hint="(read-only)">
        <textarea :value="generated" rows="2" readonly class="perk-editor__generated" />
      </EditorField>
      <EditorField label="Override Template" hint="(overrides generated when set)">
        <textarea v-model="form.tooltipTemplate" rows="3" />
      </EditorField>
    </SectionCard>

    <SectionCard title="Config" collapsible default-collapsed>
      <p v-if="configRows.length === 0" class="ps-setup__hint">No config values.</p>
      <div v-for="(row, idx) in configRows" :key="idx" class="ps-setup__map-row">
        <input v-model="row.key" placeholder="key" :aria-label="`Config ${idx + 1} key`" />
        <input v-model.number="row.value" type="number" :aria-label="`Config ${idx + 1} value`" />
        <button type="button" class="ps-setup__del" title="Remove" @click="removeConfig(idx)">✕</button>
      </div>
      <button type="button" class="ps-setup__add" @click="addConfig">+ Add Config Value</button>
    </SectionCard>

    <SectionCard title="Rank Config" collapsible default-collapsed>
      <p class="ps-setup__hint">JSON: rank → (key → number). Blank for none.</p>
      <textarea class="ps-setup__json" rows="5" :value="configByRankText" @input="onRankInput(($event.target as HTMLTextAreaElement).value)" />
      <p v-if="configByRankError" class="ps-setup__error">{{ configByRankError }}</p>
    </SectionCard>
  </GameScrollArea>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import SectionCard from '@/components/editor/SectionCard.vue'
import EditorField from '@/components/editor/EditorField.vue'
import GameScrollArea from '@/components/ui/GameScrollArea.vue'
import { usePerkBuilderContext } from './PerkBuilderContext'

const builder = usePerkBuilderContext()
const form = computed(() => builder.form.value)

const generated = computed(() => form.value.generatedDescription?.trim() || '(no typed data to generate from)')

const association = computed<string>({
  get: () => form.value.path ?? '',
  set: (v) => { builder.form.value = { ...builder.form.value, path: v || undefined } },
})
const sortedPathsByUnit = computed<Array<[string, string[]]>>(() =>
  Object.entries(builder.pathsByUnit.value)
    .sort((a, b) => a[0].localeCompare(b[0]))
    .map(([u, ps]) => [u, [...ps].sort((x, y) => x.localeCompare(y))]))
function unitLabel(u: string): string { return u ? u[0].toUpperCase() + u.slice(1) : u }

// Config rows kept in sync with form.config (same idiom as the classic panel).
interface MapRow { key: string; value: number }
const configRows = ref<MapRow[]>([])
watch(() => builder.selectedId.value, () => {
  configRows.value = Object.entries(form.value.config ?? {}).map(([key, value]) => ({ key, value }))
  syncRankText(form.value.configByRank)
}, { immediate: true })
function addConfig() { configRows.value.push({ key: '', value: 0 }) }
function removeConfig(i: number) { configRows.value.splice(i, 1) }
watch(configRows, (rows) => {
  const out: Record<string, number> = {}
  for (const r of rows) if (r.key) out[r.key] = r.value
  builder.form.value = { ...builder.form.value, config: Object.keys(out).length ? out : undefined }
}, { deep: true })

// Config-by-rank JSON textarea (invalid JSON held + flagged, never dropped).
const configByRankText = ref('')
const configByRankError = ref('')
function syncRankText(def?: Record<string, Record<string, number>>) {
  configByRankText.value = def && Object.keys(def).length ? JSON.stringify(def, null, 2) : ''
  configByRankError.value = ''
}
function onRankInput(value: string) {
  configByRankText.value = value
  const trimmed = value.trim()
  if (!trimmed) { builder.form.value = { ...builder.form.value, configByRank: undefined }; configByRankError.value = ''; return }
  try { builder.form.value = { ...builder.form.value, configByRank: JSON.parse(trimmed) }; configByRankError.value = '' }
  catch { configByRankError.value = 'Invalid JSON — not saved until fixed.' }
}
</script>

<style scoped>
.ps-setup { display: flex; flex-direction: column; gap: var(--ed-gap); min-height: 0; padding-right: 4px; }
.ps-setup__hint { margin: 0; font-size: 0.76rem; color: var(--ed-text-dim); }
.ps-setup__error { margin: 0; font-size: 0.76rem; color: var(--ed-danger); }
.ps-setup__map-row { display: grid; grid-template-columns: 1fr 1fr auto; gap: 6px; align-items: center; }
.ps-setup__add { align-self: flex-start; padding: 4px 8px; font-size: 0.74rem; border: 1px solid var(--ed-line); border-radius: 4px; background: var(--ed-field); color: var(--ed-brass); }
.ps-setup__del { padding: 2px 6px; border: 1px solid transparent; border-radius: 4px; background: none; color: var(--ed-text-dim); }
.ps-setup__del:hover { color: var(--ed-danger); border-color: var(--ed-line); }
.ps-setup__json { width: 100%; font-family: var(--font-mono, monospace); }
</style>
```

- [ ] **Step 3: Type-check + Commit** *(do not run commit)*

Run: `npx vue-tsc -b` → no new errors.

```bash
git add client/src/game-portal/src/components/perk-editor/PerkSummaryCard.vue client/src/game-portal/src/components/perk-editor/PerkSetupColumn.vue
git commit -m "feat(perk-editor): summary card + perk setup column"
```

---

## Task 8: PerkSidebar + PerkBuilderPanel (root)

**Files:**
- Create: `client/src/game-portal/src/components/perk-editor/PerkSidebar.vue`
- Create: `client/src/game-portal/src/components/perk-editor/PerkBuilderPanel.vue`

- [ ] **Step 1: Write `PerkSidebar.vue`** (2-level expandable folders + search + New Perk, `--ed-*` tokens)

```vue
<template>
  <UiPanel variant="warRoomInner" :padding="0" class="pk-side">
    <div class="pk-side__inner">
      <div class="pk-side__actions">
        <UiButton size="sm" variant="active" data-test="perk-new" @click="$emit('new')">+ New Perk</UiButton>
        <input v-model="search" type="search" placeholder="Search perks…" aria-label="Search perks" />
      </div>
      <GameScrollArea class="pk-side__scroll">
        <p v-if="loadError" class="pk-side__error">{{ loadError }}</p>
        <div v-for="group in groups" :key="group.unit" class="pk-side__group">
          <button type="button" class="pk-side__unit" :aria-expanded="expanded.has(group.unit)" @click="toggle(group.unit)">
            <span class="pk-side__chev">{{ expanded.has(group.unit) ? '▾' : '▸' }}</span>
            {{ unitLabel(group.unit) }}
          </button>
          <template v-if="expanded.has(group.unit)">
            <div v-for="pg in group.paths" :key="pg.path" class="pk-side__path">
              <button v-if="pg.path" type="button" class="pk-side__path-label" :aria-expanded="expanded.has(group.unit + '/' + pg.path)" @click="toggle(group.unit + '/' + pg.path)">
                <span class="pk-side__chev">{{ expanded.has(group.unit + '/' + pg.path) ? '▾' : '▸' }}</span>
                {{ pg.path }}
              </button>
              <ul v-if="!pg.path || expanded.has(group.unit + '/' + pg.path)">
                <li v-for="p in pg.perks" :key="p.id">
                  <button type="button" data-test="perk-row" :class="{ 'is-on': p.id === selectedId }" @click="$emit('select', p.id)">
                    {{ p.id }} <span v-if="p.displayName">— {{ p.displayName }}</span>
                    <span v-if="!p.wired" class="pk-side__inert">inert</span>
                  </button>
                </li>
              </ul>
            </div>
          </template>
        </div>
      </GameScrollArea>
    </div>
  </UiPanel>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import UiPanel from '@/components/ui/UiPanel.vue'
import UiButton from '@/components/ui/UiButton.vue'
import GameScrollArea from '@/components/ui/GameScrollArea.vue'
import type { AuthoredPerkDef } from '@/game/perks/perkEditorForm'

const props = defineProps<{
  perks: AuthoredPerkDef[]
  pathsByUnit: Record<string, string[]>
  selectedId: string | null
  loadError: string
}>()
defineEmits<{ select: [string]; new: [] }>()

const search = ref('')
const expanded = ref(new Set<string>())
function toggle(key: string) {
  const s = new Set(expanded.value)
  s.has(key) ? s.delete(key) : s.add(key)
  expanded.value = s
}
function unitLabel(u: string): string { return u && u !== 'Generic' ? u[0].toUpperCase() + u.slice(1) : u }

const pathToUnit = computed(() => {
  const m = new Map<string, string>()
  for (const [u, ps] of Object.entries(props.pathsByUnit)) for (const p of ps) m.set(p, u)
  return m
})

interface Grp { unit: string; paths: Array<{ path: string; perks: AuthoredPerkDef[] }> }
const groups = computed<Grp[]>(() => {
  const q = search.value.trim().toLowerCase()
  const match = (p: AuthoredPerkDef) => !q || p.id.toLowerCase().includes(q) || (p.displayName ?? '').toLowerCase().includes(q)
  const byUnitPath = new Map<string, Map<string, AuthoredPerkDef[]>>()
  const generic: AuthoredPerkDef[] = []
  for (const p of props.perks) {
    if (!match(p)) continue
    const path = p.path ?? ''
    const unit = path ? pathToUnit.value.get(path) : undefined
    if (!path || !unit) { generic.push(p); continue }
    if (!byUnitPath.has(unit)) byUnitPath.set(unit, new Map())
    const paths = byUnitPath.get(unit)!
    if (!paths.has(path)) paths.set(path, [])
    paths.get(path)!.push(p)
  }
  const out: Grp[] = [...byUnitPath.entries()].sort((a, b) => a[0].localeCompare(b[0])).map(([unit, paths]) => ({
    unit,
    paths: [...paths.entries()].sort((a, b) => a[0].localeCompare(b[0])).map(([path, ps]) => ({ path, perks: [...ps].sort((x, y) => x.id.localeCompare(y.id)) })),
  }))
  if (generic.length) out.push({ unit: 'Generic', paths: [{ path: '', perks: [...generic].sort((x, y) => x.id.localeCompare(y.id)) }] })
  return out
})
</script>

<style scoped>
.pk-side { height: 100%; min-height: 0; }
.pk-side__inner { height: 100%; min-height: 0; display: flex; flex-direction: column; gap: 8px; padding: 10px; box-sizing: border-box; }
.pk-side__actions { display: flex; flex-direction: column; gap: 6px; }
.pk-side__scroll { flex: 1 1 auto; min-height: 0; }
.pk-side__error { font-size: 0.78rem; color: var(--ed-danger); }
.pk-side__group { display: flex; flex-direction: column; gap: 2px; }
.pk-side__unit { display: flex; align-items: center; gap: 6px; width: 100%; margin-top: 8px; padding: 3px 4px; background: none; border: 0; text-align: left; font-size: 0.82rem; font-weight: 700; color: var(--ed-brass); }
.pk-side__path { padding-left: 8px; }
.pk-side__path-label { display: flex; align-items: center; gap: 6px; width: 100%; padding: 2px 4px; background: none; border: 0; text-align: left; font-size: 0.76rem; color: var(--ed-text-dim); }
.pk-side__chev { flex: 0 0 auto; font-size: 0.66rem; }
.pk-side ul { list-style: none; margin: 0; padding: 0 0 0 10px; display: flex; flex-direction: column; gap: 3px; }
.pk-side li button { width: 100%; border: 1px solid transparent; border-radius: 6px; background: var(--ed-field); color: var(--ed-text); padding: 6px 8px; font-size: 0.76rem; text-align: left; }
.pk-side li button.is-on { border-color: var(--ed-line-strong); box-shadow: inset 2px 0 0 var(--ed-brass); }
.pk-side__inert { margin-left: 4px; font-size: 0.58rem; letter-spacing: 0.06em; text-transform: uppercase; color: var(--ed-danger); }
</style>
```

- [ ] **Step 2: Write `PerkBuilderPanel.vue`** (root — provides the builder, composes 4 columns)

```vue
<template>
  <EditorShell>
    <template #sidebar>
      <PerkSidebar
        :perks="builder.perks.value"
        :paths-by-unit="builder.pathsByUnit.value"
        :selected-id="builder.selectedId.value"
        :load-error="builder.loadError.value"
        @select="onSelect"
        @new="builder.newPerk"
      />
    </template>

    <template #main>
      <EditorHeader
        :title="builder.form.value.displayName || builder.form.value.id || 'New Perk'"
        :badge="builder.form.value.wired ? 'Wired' : 'Not Wired'"
        :badge-color="builder.form.value.wired ? 'var(--ed-ok)' : 'var(--ed-danger)'"
        :id="builder.form.value.id"
        :id-editable="builder.selectedId.value === null"
        :saving="builder.busy.value"
        :save-disabled="builder.busy.value || !builder.form.value.id"
        :saved-label="builder.statusMessage.value"
        :error="builder.saveError.value"
        remove-label="Delete / Reset"
        @save="builder.save"
        @remove="onRemove"
        @update:id="(v) => (builder.form.value = { ...builder.form.value, id: v })"
      />
      <GameScrollArea class="pk-main">
        <PerkSummaryCard />
        <PerkModifierStack />
      </GameScrollArea>
    </template>

    <template #inspector>
      <PerkModifierInspector />
    </template>

    <template #rail>
      <PerkSetupColumn />
    </template>
  </EditorShell>

  <datalist id="perk-builder-perk-ids">
    <option v-for="p in builder.perks.value" :key="p.id" :value="p.id" />
  </datalist>
  <datalist id="perk-builder-ability-ids">
    <option v-for="id in builder.abilityIds.value" :key="id" :value="id" />
  </datalist>
</template>

<script setup lang="ts">
import { onMounted, provide } from 'vue'
import EditorShell from '@/components/editor/EditorShell.vue'
import EditorHeader from '@/components/editor/EditorHeader.vue'
import GameScrollArea from '@/components/ui/GameScrollArea.vue'
import { confirmDelete } from '@/components/editor/confirmDelete'
import PerkSidebar from './PerkSidebar.vue'
import PerkSummaryCard from './PerkSummaryCard.vue'
import PerkModifierStack from './PerkModifierStack.vue'
import PerkModifierInspector from './PerkModifierInspector.vue'
import PerkSetupColumn from './PerkSetupColumn.vue'
import { usePerkBuilder } from './usePerkBuilder'
import { PerkBuilderKey } from './PerkBuilderContext'

const builder = usePerkBuilder()
provide(PerkBuilderKey, builder)

function onSelect(id: string) {
  const def = builder.perks.value.find((p) => p.id === id)
  if (def) builder.selectPerk(def)
}
async function onRemove() {
  if (!builder.selectedId.value) return
  if (!(await confirmDelete('perk', builder.selectedId.value, undefined, 'If it ships with the game it will reset to its built-in default; a custom one is removed.'))) return
  await builder.removePerk()
}

onMounted(builder.load)
</script>

<style scoped>
.pk-main { display: flex; flex-direction: column; gap: var(--ed-gap); min-height: 0; }
</style>
```

- [ ] **Step 3: Type-check**

Run: `npx vue-tsc -b`
Expected: no new errors. `EditorHeader`'s exact prop/emit names must match its `defineProps`/`defineEmits` — open `client/src/game-portal/src/components/editor/EditorHeader.vue` and reconcile (`title, badge, badgeColor, id, idEditable, saving, saveDisabled, savedLabel, error, removeLabel`; emits `save`, `remove`, `update:id`). Adjust the bindings if any name differs.

- [ ] **Step 4: Commit** *(do not run)*

```bash
git add client/src/game-portal/src/components/perk-editor/PerkSidebar.vue client/src/game-portal/src/components/perk-editor/PerkBuilderPanel.vue
git commit -m "feat(perk-editor): sidebar + PerkBuilderPanel root (4-column shell)"
```

---

## Task 9: Component tests — projection, round-trip, selection→edit

**Files:**
- Create: `client/src/game-portal/src/components/perk-editor/PerkBuilderPanel.test.ts`

These are the acceptance tests for the slice. They reuse the classic suite's fixtures/stub style. The **round-trip** tests are the guarantee that no functionality is lost.

- [ ] **Step 1: Write the failing tests**

```ts
// PerkBuilderPanel.test.ts
import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import PerkBuilderPanel from './PerkBuilderPanel.vue'

const markerTrap = {
  id: 'marker_trap',
  program: { triggers: [{ id: 'cast', type: 'on_cast_complete', actions: [{ id: 'zone', type: 'create_zone', config: { name: 'Z', radius: 115, duration: 12, triggers: [{ id: 'entered', type: 'on_tick', actions: [{ id: 'pick_enemy', type: 'select_targets', target: { radius: 110 } }, { id: 'mark', type: 'apply_status_duration', config: { name: 'Marked', duration: 4 } }] }] } }] }] },
}

const amplified = {
  id: 'amplified_effects',
  displayName: 'Amplified Effects',
  path: 'trapper',
  statModifiers: [{ stat: 'abilityDamage', op: 'multiply', value: 1.35 }],
  abilityStats: [{ stat: 'damageTaken', flat: 0.15 }, { stat: 'moveSpeed', flat: -0.15 }],
  abilityFields: [{ target: 'marker_trap', action: 'mark', field: 'duration', op: 'multiply', value: 1.35 }],
  wired: false,
}

// A perk carrying an un-migrated kind (aura) to prove pass-through preservation.
const zealous = {
  id: 'zealous_march', path: 'cleric',
  auras: [{ radius: 192, targets: 'allies', includeSelf: true, stacking: 'max', perAdditionalSource: 0.05, statModifiers: [{ stat: 'moveSpeed', op: 'add', value: 0.3 }] }],
  wired: false,
}

function stub(perk: Record<string, unknown>, sink?: Array<Record<string, unknown>>) {
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const u = String(url)
    if (init?.method === 'POST' && u.endsWith('/perks')) {
      sink?.push((JSON.parse(String(init.body)) as { perk: Record<string, unknown> }).perk)
      return { ok: true, status: 200, json: async () => ({}) }
    }
    if (u.endsWith('/catalog/perks')) return { ok: true, status: 200, json: async () => ({ perks: [perk] }) }
    if (u.endsWith('/catalog/units')) return { ok: true, status: 200, json: async () => ({ units: [], paths: [], pathsByUnit: { archer: ['trapper'], acolyte: ['cleric'] } }) }
    if (u.endsWith('/catalog/abilities')) return { ok: true, status: 200, json: async () => ({ abilities: [markerTrap] }) }
    if (u.endsWith('/catalog/ability-stats')) return { ok: true, status: 200, json: async () => ({ stats: [{ id: 'damageTaken', label: 'Vulnerable (Damage Taken)', inflicted: true }, { id: 'moveSpeed', label: 'Move Speed', inflicted: true }] }) }
    return { ok: true, status: 200, json: async () => ({}) }
  }) as unknown as typeof fetch)
}

async function openPerk(wrapper: VueWrapper, unit: string, path: string) {
  await wrapper.findAll('.pk-side__unit').find((b) => b.text().includes(unit))!.trigger('click')
  await wrapper.findAll('.pk-side__path-label').find((b) => b.text().includes(path))!.trigger('click')
  await wrapper.find('[data-test="perk-row"]').trigger('click')
  await flushPromises()
}

afterEach(() => vi.restoreAllMocks())

describe('PerkBuilderPanel', () => {
  it('projects a perk into one card per modifier, in kind order', async () => {
    stub(amplified)
    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await openPerk(wrapper, 'Archer', 'trapper')

    const cards = wrapper.findAll('[data-test="perk-modifier-card"]')
    expect(cards).toHaveLength(4)
    expect(cards.map((c) => c.attributes('data-kind'))).toEqual(['unitStat', 'abilityStat', 'abilityStat', 'abilityField'])
    expect(cards[0].text()).toContain('Ability Damage ×1.35')
    expect(cards[3].text()).toContain('Marker Trap ▸ mark ▸ duration ×1.35')
  })

  it('shows the empty inspector until a card is selected, then edits that card', async () => {
    stub(amplified)
    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await openPerk(wrapper, 'Archer', 'trapper')

    expect(wrapper.find('[data-test="perk-inspector-empty"]').exists()).toBe(true)

    await wrapper.findAll('[data-test="perk-modifier-card"]')[0].trigger('click')
    const inspector = wrapper.find('[data-test="perk-inspector"]')
    expect(inspector.exists()).toBe(true)
    expect((inspector.find('select[aria-label="Stat"]').element as HTMLSelectElement).value).toBe('abilityDamage')
    expect((inspector.find('input[aria-label="Value"]').element as HTMLInputElement).value).toBe('1.35')
  })

  it('round-trips Amplified Effects unedited through save (all 3 slice arrays byte-identical)', async () => {
    const sink: Array<Record<string, unknown>> = []
    stub(amplified, sink)
    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await openPerk(wrapper, 'Archer', 'trapper')

    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()

    expect(sink).toHaveLength(1)
    expect(sink[0].statModifiers).toEqual(amplified.statModifiers)
    expect(sink[0].abilityStats).toEqual(amplified.abilityStats)
    expect(sink[0].abilityFields).toEqual(amplified.abilityFields)
    expect('generatedDescription' in sink[0]).toBe(false)
  })

  it('preserves an un-migrated kind (aura) through save without editing it', async () => {
    const sink: Array<Record<string, unknown>> = []
    stub(zealous, sink)
    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await openPerk(wrapper, 'Acolyte', 'cleric')

    // The aura renders as a (read-only) card in the new UI…
    expect(wrapper.find('[data-test="perk-modifier-card"][data-kind="aura"]').exists()).toBe(true)

    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()
    expect(sink[0].auras).toEqual(zealous.auras)
  })

  it('edits an ability-field value and round-trips the change (multiply stays off the wire)', async () => {
    const sink: Array<Record<string, unknown>> = []
    stub(amplified, sink)
    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await openPerk(wrapper, 'Archer', 'trapper')

    await wrapper.find('[data-test="perk-modifier-card"][data-kind="abilityField"]').trigger('click')
    const valueInput = wrapper.find('[data-test="perk-inspector"] input[aria-label="Value"]')
    await valueInput.setValue('1.5')

    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()
    const fields = sink[0].abilityFields as Array<{ value: number; op?: string }>
    expect(fields[0].value).toBe(1.5)
    expect(fields[0]).not.toHaveProperty('op') // multiply is default, omitted
  })

  it('adds a unit-stat modifier via quick-add and it appears as a new card', async () => {
    stub(amplified)
    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await openPerk(wrapper, 'Archer', 'trapper')

    await wrapper.find('[data-test="quick-add-unitStat"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-test="perk-modifier-card"][data-kind="unitStat"]')).toHaveLength(2)
    // the new card is auto-selected → inspector open on it
    expect(wrapper.find('[data-test="perk-inspector"]').exists()).toBe(true)
  })
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `npx vitest run src/components/perk-editor/PerkBuilderPanel.test.ts`
Expected: FAIL (component not yet wired end-to-end / selectors missing).

- [ ] **Step 3: Make them pass**

Fix whatever the tests surface — most likely: `EditorHeader` prop/emit mismatches, a `GameScrollArea` import path, or a stat-label lookup. Do NOT change the tests to fit a bug; change the components. Re-run until green.

Run: `npx vitest run src/components/perk-editor/PerkBuilderPanel.test.ts`
Expected: PASS (6 tests).

- [ ] **Step 4: Full type-check + full test run**

Run (from `client/src/game-portal/`): `npx vue-tsc -b && npx vitest run`
Expected: no new type errors; all tests pass (classic `PerkEditorPanel.test.ts` still green — it is untouched).

- [ ] **Step 5: Commit** *(do not run)*

```bash
git add client/src/game-portal/src/components/perk-editor/PerkBuilderPanel.test.ts
git commit -m "test(perk-editor): projection, round-trip, and selection-edit coverage"
```

---

## Task 10: Wire the toggle into both mount points

The new panel ships beside the classic one. A small toggle lets the user switch; the classic editor is required for the 7 un-migrated modifier kinds.

**Files:**
- Modify: `client/src/game-portal/src/views/PerkEditor.vue`
- Modify: `client/src/game-portal/src/components/world-editor/WorldEditorPanel.vue:1620`

- [ ] **Step 1: Update `views/PerkEditor.vue`**

```vue
<template>
  <div class="perk-editor-view">
    <div class="perk-editor-view__topbar">
      <div class="perk-editor-view__mode" role="group" aria-label="Editor mode">
        <button type="button" :class="{ 'is-on': mode === 'builder' }" @click="mode = 'builder'">New Builder</button>
        <button type="button" :class="{ 'is-on': mode === 'classic' }" @click="mode = 'classic'">Classic</button>
      </div>
      <ExitButton @click="router.push('/')" />
    </div>
    <PerkBuilderPanel v-if="mode === 'builder'" />
    <PerkEditorPanel v-else />
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import PerkEditorPanel from '@/components/PerkEditorPanel.vue'
import PerkBuilderPanel from '@/components/perk-editor/PerkBuilderPanel.vue'
import ExitButton from '@/components/ui/ExitButton.vue'
const router = useRouter()
// Default to the new builder so the redesign is what users see; Classic remains
// one click away for the modifier kinds it can't edit yet.
const mode = ref<'builder' | 'classic'>('builder')
</script>

<style scoped>
.perk-editor-view { position: relative; width: 100%; height: 100%; min-height: 0; display: flex; overflow: hidden; }
.perk-editor-view__topbar { position: absolute; top: 16px; right: 16px; z-index: 20; display: flex; gap: 8px; align-items: center; }
.perk-editor-view__mode { display: flex; border: 1px solid var(--ed-line-strong); border-radius: 6px; overflow: hidden; }
.perk-editor-view__mode button { padding: 4px 10px; font-size: 0.72rem; background: var(--ed-field); color: var(--ed-text-dim); border: 0; }
.perk-editor-view__mode button.is-on { background: rgba(212, 168, 71, 0.18); color: var(--ed-brass); }
</style>
```

- [ ] **Step 2: Update `WorldEditorPanel.vue`** — replace the single mount at line ~1620 with a toggled pair.

Find:
```vue
      <PerkEditorPanel v-else-if="activeScreen === 'perks'" />
```
Replace with:
```vue
      <template v-else-if="activeScreen === 'perks'">
        <div class="world-editor__perk-mode" role="group" aria-label="Perk editor mode">
          <button type="button" :class="{ 'is-on': perkMode === 'builder' }" @click="perkMode = 'builder'">New Builder</button>
          <button type="button" :class="{ 'is-on': perkMode === 'classic' }" @click="perkMode = 'classic'">Classic</button>
        </div>
        <PerkBuilderPanel v-if="perkMode === 'builder'" />
        <PerkEditorPanel v-else />
      </template>
```

Add the import beside the existing `PerkEditorPanel` import (line ~1642):
```ts
import PerkBuilderPanel from '@/components/perk-editor/PerkBuilderPanel.vue'
```

Add the ref in that component's `<script setup>` (near the other screen state):
```ts
const perkMode = ref<'builder' | 'classic'>('builder')
```
(If `ref` is not already imported in this file, add it to the existing `vue` import.)

Add scoped style:
```css
.world-editor__perk-mode { display: flex; gap: 6px; margin-bottom: 8px; }
.world-editor__perk-mode button { padding: 3px 10px; font-size: 0.72rem; background: var(--ed-field); color: var(--ed-text-dim); border: 1px solid var(--ed-line); border-radius: 5px; }
.world-editor__perk-mode button.is-on { background: rgba(212, 168, 71, 0.18); color: var(--ed-brass); border-color: var(--ed-line-strong); }
```

- [ ] **Step 3: Type-check + full test run**

Run (from `client/src/game-portal/`): `npx vue-tsc -b && npx vitest run`
Expected: clean type-check; all tests pass.

- [ ] **Step 4: Manual smoke check** *(user-driven — do not launch Playwright)*

Ask the user to open the Perks editor (standalone route and the world-editor tab), confirm: the new builder loads, `Amplified Effects` shows 4 accented cards, selecting a card opens the inspector, editing a value + Save works, and the Classic toggle still opens the old editor for a perk with an aura/rider.

- [ ] **Step 5: Commit** *(do not run)*

```bash
git add client/src/game-portal/src/views/PerkEditor.vue client/src/game-portal/src/components/world-editor/WorldEditorPanel.vue
git commit -m "feat(perk-editor): mount new builder beside classic behind a toggle"
```

---

## Self-Review (completed against the redesign spec)

**Spec coverage:**
- Four-column layout → Task 8 (`EditorShell` sidebar/main/inspector/rail). ✔
- Left sidebar Unit→Path→Perk expandable + search + New Perk → Task 8 `PerkSidebar`. ✔
- Summary card (icon/name/id/description/wired/association/modifier-count chip) → Task 7 `PerkSummaryCard`. ✔
- Modifier builder = vertical card list, only kinds that exist → Tasks 1 (projection) + 5 (stack). ✔
- Per-type color accent + icon → Task 1 `KIND_META`; Task 4 renders accent bar/icon. ✔ (new convention, as flagged.)
- Card header with select + duplicate + delete; compact/read-only → Task 4. ✔ (Expand/collapse on a card is intentionally dropped — the chosen model makes cards read-only summaries and the Inspector the edit surface; noted for the user.)
- "+ Add Modifier" menu + quick-add row → Task 5. ✔
- Inspector edits selected modifier; summary when none selected → Task 6 (+ summary card is the "nothing selected" anchor). ✔
- Perk Setup column: Identity / Eligibility / Tooltip / Config / Rank Config → Task 7 `PerkSetupColumn`. ✔
- Visual language (dark steel, bronze borders, corner ticks, inner shadow, accents) → forge theme via `EditorShell`/`SectionCard` + hand-applied on `PerkModifierCard`. ✔
- All existing functionality preserved → Task 9 round-trip tests, incl. pass-through of un-migrated kinds. ✔

**Deferred (explicit, not gaps):** rich inspectors for the 7 non-slice kinds; real perk-icon rendering in the summary/sidebar; retiring the classic panel + toggle. These are follow-up plans.

**Placeholder scan:** no TBD/TODO; every code step carries complete code. ✔
**Type consistency:** `Selection`, `ModifierEntry`, `KIND_META`, `usePerkBuilder` return shape, and `PerkBuilderKey` names are used identically across Tasks 3/4/5/6/7/8/9. ✔

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-07-23-perk-editor-redesign.md`. Two execution options:

1. **Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.
2. **Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints.

Which approach?
