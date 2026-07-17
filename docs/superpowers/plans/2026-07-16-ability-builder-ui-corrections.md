# Ability Builder — UI Corrections (Reference Plan)

Precedes the Phase 7 nested-authoring plan (`2026-07-16-composable-abilities-phase7-nested-authoring.md`). Pure UI/IA restructure — no change to the program data model, executor, or the v2 gating invariant.

## Target layout

```
┌─────────┬────────────────────────┬────────────────────────┐
│ Sidebar │ Header + toolbar       │   [ renderer ]         │  ← always visible,
│ abili-  │ Tabs: Identity │ Build │   ▶ Run Preview        │    outside the tabs
│ ties    │ ───────────────────────│   scene controls       │
│         │  tab content           │   timeline / log       │
│         │  (flow ≈ 1/3 width)    │   summary              │
├─────────┴────────────────────────┴────────────────────────┤
│ Inspector strip — fields for the flow-selected node       │  ← spans the bottom
└───────────────────────────────────────────────────────────┘
```

**Decisions (user, 2026-07-16):**
- Rail Inspector **goes away**. Trigger/action editing moves to a strip spanning the bottom, driven by flow selection.
- Identity / Entry / Cast Setup leave the inspector and become the **Identity tab**.
- Preview lives outside the tab system — renderer pinned top-right, **always mounted**, `Run Preview` underneath it. Playback state survives tab switches.
- Flow gets ~1/3 of its current width; the reclaimed space goes to preview.
- Preview scene: caster **Adept**, enemies **Raider**.

## Constraints

- `EditorShell` is shared by UnitType/Item/Table/List/AbilityEditor panels — **all changes to it must be additive** (new optional slot/prop; unused ⇒ byte-identical layout for those five).
- No literal `cursor:` in component CSS (`not-allowed` only). `--font-*` / `--ed-*` tokens.
- `useAbilityBuilder` edits stay immutable-op only (shallowRef + structuredClone undo/redo).
- Adept has known casting-fallback + facing-prime quirks (see `project_ability_animation_viewer` memory) — expect to verify the cast animation actually plays, not just that the unit spawns.

## Tasks

### Task 1 — Go: preview scene models
`server/internal/game/ability_preview.go:79` — `previewSceneUnitType = "soldier"` is one const used for caster, allies **and** enemies (`:216` caster, `:243` scene units).

Split into `previewCasterUnitType = "adept"` and `previewEnemyUnitType = "raider"`; allies keep the existing type. Keep the existing "unknown catalog type" guard on both (`:219`, `:245`) — a missing type must degrade honestly, not panic.

**Tests:** caster spawns as adept; enemy scene unit spawns as raider; ally does not. Don't pin HP/damage numbers — derive from catalog (`feedback_no_hardcoded_tunables_in_tests`).
**Verify:** `cd server && go test ./... -count=1`

### Task 2 — EditorShell: additive `bottom` slot + wide rail
`components/editor/EditorShell.vue`

- New optional `#bottom` slot → a full-width row under the grid (spans sidebar→rail). Absent ⇒ no DOM, no layout change.
- New optional `wideRail` prop → rail column `minmax(280px,340px)` becomes something like `minmax(420px, 1.1fr)` with main `minmax(0, 1fr)`, giving flow ≈1/3. Default false ⇒ existing five editors untouched.
- Keep the `max-width:1200px` rail-drop behavior working for both modes.

**Test:** rendering without the new slot/prop produces the same structure as before (guards the five consumers).

### Task 3 — Split ItemInspector
`ItemInspector.vue` (22KB) currently branches on `selected.kind`: `ability` (`:17-160` Identity / Entry read-only / Cast Setup), `trigger` (`:160-208`), `action` (`:208-250`).

- **Extract** the `ability` branch → `IdentityTab.vue` (main-area form, no longer selection-gated).
- **Extract** trigger/action branches → `InspectorBar.vue` — horizontal bottom strip. Same schema-driven fields (`schemaForAction`, `SchemaField`, `TargetQueryEditor`); relayout from a vertical rail stack to a horizontal row of field groups. Keep commit-on-blur (it protects undo granularity).
- Empty state when selection is `ability`/none: a hint, not a blank bar.
- Delete `ItemInspector.vue` once both extractions land; update `ItemInspector.test.ts` into tests for each new component.

### Task 4 — AbilityBuilderPanel restructure
- Tabs `Identity | Build Ability` replace the `Flow | Preview` toggle (`:86-105`). Reset to `Identity` on ability select/new (mirrors the existing `mainView` reset rationale at `:162-168`).
- `#rail` ⇒ `AbilityPreviewPanel`, always mounted (both tabs).
- `#bottom` ⇒ `InspectorBar`.
- `wideRail` on.
- Identity tab ⇒ `IdentityTab` (+ `AbilityOverviewCard`); Build tab ⇒ `AbilityFlow`.
- Remove the `ActionPalette` mount at `:112`.

### Task 5 — AbilityPreviewPanel reorder + idle renderer
Current order: run row → scene controls → `v-if="result"` canvas → banner → timeline → log → summary.

Target: **canvas first and always mounted** → Run Preview → scene controls → banner → timeline → log → summary.

`AbilityPreviewCanvas` must handle `frames: []` (idle placeholder — empty stage, no crash, controls disabled). Today it's only ever mounted with a populated `result.frames`, so this is a new state to build, not just a `v-if` move.

### Task 6 — Add Action dialog replaces the permanent palette
Delete `ActionPalette.vue`; add `AddActionDialog.vue`, opened by the flow's "+ Action" (`FlowTriggerCard.vue:60`, which today only selects the trigger and defers to the palette).

- Modal, same pattern as `ConvertDialog.vue` (fixed overlay, sibling of the shell).
- Search input on top; **category filter chips underneath it**: Targets, Combat, World, Resources, Flow, Presentation, Other — reuse `CATEGORY_BY_TYPE` / `CATEGORY_ORDER` verbatim from `ActionPalette.vue:96-123` (they're already the right taxonomy).
- Filtered list ⇒ pick ⇒ `builder.addAction(triggerId, type)` ⇒ close. `addAction` already auto-selects the new node, so the bottom InspectorBar focuses it on close.
- Keep the `display-only` chip for `runnable: false` and keep letting authors add those (`ActionPalette.vue:162-166` rationale — structure ahead of executor support).
- Dialog opens with the target trigger passed in explicitly, not read from selection.

### Task 7 — Presentations inline
`AbilityFlow.vue:16-38` renders a separate read-only Presentations section at the bottom.

Remove it. Instead, under a `play_presentation` action card, show the presentation its `config.presentationId` resolves to (`prog.presentations.find(p => p.id === cfg.presentationId)` — the indirection at `ability_exec_presentation.go:112-126`), rendering the asset + its trigger summary in context.

Stays **read-only** here — nested trigger editing is Phase 7's job. Leave the NodeRef TODO comment accurate rather than deleting it.

**Verify:** `cd client/src/game-portal && npx vue-tsc -b && npx vitest run` (3 pre-existing `ListEditorPanel.test.ts` failures unrelated).

### Task 8 — Live verification
Playwright at `http://localhost:5173/#/ability-editor` (dismiss "CLICK ANYWHERE TO BEGIN"). Check: tabs switch without unmounting the renderer; Run Preview under the renderer works; shatter/meteor still replay; enemies render as Raider and the caster as Adept **with its cast animation actually playing**; Add Action dialog filters by category; play_presentation shows its presentation inline; bottom strip edits a selected action and undo/redo still works.

## Out of scope
Nested-trigger authoring (Phase 7). Presentation node editing. The deferred executor pass.
