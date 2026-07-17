# Composable Abilities — Phase 5b (Flow Editor UI) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. This is a large Vue UI — the plan specifies the state **contract** + each component's **responsibility/props/emits** precisely; implementers write idiomatic Vue against those contracts (inlining every line of every component is not feasible at this size). Steps use checkbox (`- [ ]`) syntax.

**Goal:** Replace the field-oriented `AbilityEditorPanel.vue` with a **flow-based composable ability editor**: a three-region layout (ability-list · overview+flow+palette · schema-driven inspector) that renders an ability as a sequence of trigger/action cards, edits any node through a registry-driven inspector, validates live per-card, and saves both the top-level cast-setup fields AND the composable `Program`. Legacy abilities display their compiled flow (read-only until converted).

**Layout adjustment (approved):** the design-doc §9 center-right **Preview** region is Phase 6 work, so 5b builds a **three-region** layout on the existing `EditorShell` (`sidebar | main | rail`): **sidebar** = ability list; **main** = header (Save top-right) + compact Overview card + Flow + Action Palette (stacked); **rail** = Inspector. Phase 6 drops the live preview into a new region later.

**Architecture:** State lives in a `useAbilityBuilder()` composable (the repo uses composables + local refs, NOT Pinia). Components are presentational, driven by the composable. Reuse the shared `components/editor/` toolkit (`EditorShell`, `EditorSidebar`, `EditorHeader`, `EditorField`, `SectionCard`, `FilterableSelect`, `RepeatableList`, `GameScrollArea`, `UiButton`). Consume the Phase-5a endpoints (`fetchActionSchema`, `validateAbilityProgram`, `convertAbility`, catalog `program ?? compiledProgram`).

**Tech Stack:** Vue 3 `<script setup lang="ts">` + Vitest. Client tests `cd client/src/game-portal && npm run test -- <file>`; **type-check is `npx vue-tsc -b` (build mode — NOT `--noEmit`)**. Do NOT run `git commit`. Known-unrelated failing test: `ListEditorPanel.test.ts` (ignore).

**PROJECT CONVENTIONS (must follow — see CLAUDE.md):**
- **Custom cursor:** component CSS must NOT write literal `cursor:` values (`pointer`/`default`/`auto` etc.) — global rules in `style.css` handle interactive/disabled cursors. Only `cursor: not-allowed` is allowed, per-state, for forbidden actions (unaffordable/locked). Interactive + disabled elements: write nothing.
- **Fonts:** use the `--font-*` tokens (Cinzel/Alegreya SC/Source Sans 3), not raw families.
- **Styling:** Lords of Conquest look — dark wood/brass panels, gold headings/borders, compact density. Reuse the `editor/` toolkit + `editor-controls.css` chrome (auto-imported by `EditorShell`). Save control stays top-right (`EditorHeader`). Do NOT make it look like a SaaS workflow tool.
- **Client is a view of server state** — never simulate gameplay client-side.

**Existing shapes to consume (from Phase 5a):**
- `abilityEditorApi.ts`: `fetchAuthoredAbilityDefs()` (entries now carry `program`/`schemaVersion`/`compiledProgram`/`runnable`/`generatedDescription`), `saveEditorAbility(def)`, `deleteEditorAbility(id)`, `fetchActionSchema(): ActionSchemaBundle`, `validateAbilityProgram(ability): ValidationIssue[]`, `convertAbility(id): {ability,warnings,runnable}`, `fetchEffectIds`/`fetchProjectileIds`/`fetchDamageTypes`/`fetchAbilityCategories`/`fetchAutoCastSelectors`, `fetchAuthoredUnitDefs`.
- `program/abilityProgram.ts`: `AbilityProgram`, `AbilityTriggerDef`, `AbilityActionDef`, `TargetQueryDef`, `ContextRef`, `parseProgram`/`serializeProgram`.
- `program/programSchema.ts`: `ActionSchemaBundle{actions,enums}`, `ActionSchema{type,fields,runnable}`, `SchemaField{key,label,control,options?,section?}`, `schemaForAction(bundle,type)`.
- `program/programValidation.ts`: `ValidationIssue{path,code,message,severity}`, `issuesForPath(issues,path)`, `hasBlockingError(issues)`.
- `abilityEditorForm.ts`: `AuthoredAbilityDef` (identity + cast-setup + `program?`/`schemaVersion?`/`compiledProgram?`/`runnable?`), `MODELED_KEYS`, `formFromDef`, `saveRequestFromForm` (strips display fields).
- Mount point: `views/AbilityEditor.vue` renders `<AbilityEditorPanel />` — swap to the new panel.

---

### Task 1: `useAbilityBuilder` composable — the state backbone

**Files:**
- Create: `client/src/game-portal/src/components/ability-builder/useAbilityBuilder.ts`
- Create: `client/src/game-portal/src/components/ability-builder/programTree.ts` (pure tree ops)
- Test: `client/src/game-portal/src/components/ability-builder/programTree.test.ts`, `useAbilityBuilder.test.ts`

- [ ] **Step 1 — pure tree ops first (TDD).** `programTree.ts` — pure functions over an `AbilityProgram` (immutable-style: return a new program, don't mutate, so undo/redo is snapshot-based):
  - `nodePath` model: a node is addressed by a `NodeRef` = `{ kind: 'ability' } | { kind: 'trigger'; triggerId: string } | { kind: 'action'; triggerId: string; actionId: string }` (Phase 5b addresses root triggers + their actions + nested via id; keep the id-based addressing consistent with the validation `path` where practical).
  - Ops (each returns a new `AbilityProgram`): `addTrigger(prog, type)`, `removeTrigger(prog, triggerId)`, `addAction(prog, triggerId, actionType)` (new action with a generated unique id + `Disabled:false` default), `removeAction(prog, triggerId, actionId)`, `moveAction(prog, triggerId, actionId, dir: 'up'|'down')`, `duplicateAction(prog, triggerId, actionId)`, `setActionDisabled(prog, triggerId, actionId, disabled)`, `updateAction(prog, triggerId, actionId, patch)` (merge into the action, e.g. Config/Target/Input), `updateTrigger`, `findAction`/`findTrigger`. Generate ids via a small counter/uuid-ish helper (deterministic-enough for the editor; e.g. `a{n}`/`t{n}` from a max-existing scan so ids stay unique).
  - Tests: add/remove/move/duplicate/update round-trips; ids stay unique; updateAction merges Config without dropping other fields; moveAction bounds-checks.

- [ ] **Step 2 — `useAbilityBuilder` composable.** Returns reactive state + operations. Contract (TS):
  ```ts
  interface AbilityBuilderState {
    abilities: Ref<AuthoredAbilityDef[]>          // list for the sidebar
    schema: Ref<ActionSchemaBundle | null>        // fetchActionSchema()
    catalogs: Ref<{ effects:string[]; projectiles:string[]; damageTypes:string[]; categories:string[]; autoCastSelectors:string[]; unitTypes:string[] }>
    // current editing target:
    editing: Ref<boolean>
    form: Ref<AuthoredAbilityDef>                  // identity + cast-setup (reactive)
    program: Ref<AbilityProgram>                   // the flow tree being edited (program ?? compiledProgram)
    isLegacy: Ref<boolean>                         // schemaVersion<2 (editing the COMPILED view — read-only until converted)
    runnable: Ref<boolean>
    selected: Ref<NodeRef>                         // drives the inspector
    issues: Ref<ValidationIssue[]>                 // from validateAbilityProgram (debounced)
    dirty: Ref<boolean>
    canUndo: Ref<boolean>; canRedo: Ref<boolean>
    busy: Ref<boolean>; saveError: Ref<string>; savedLabel: Ref<string>; warnings: Ref<string[]>
  }
  // operations: load(), selectAbility(id), newAbility(), select(ref), 
  //   addTrigger/removeTrigger/addAction/removeAction/moveAction/duplicateAction/toggleActionDisabled,
  //   updateForm(patch), updateAction(ref, patch), updateTrigger(ref, patch),
  //   undo(), redo(), save(), convert(), remove()  (delete/reset)
  ```
  Behavior:
  - `load()` — parallel fetch: abilities, action schema, catalogs. 
  - `selectAbility(id)` — set `form` from `formFromDef(def)`; `program` from `def.program ?? def.compiledProgram ?? emptyProgram()`; `isLegacy = (def.schemaVersion ?? 0) < 2`; select `{kind:'ability'}`; reset undo stack + dirty.
  - Every mutation op pushes the PRE-state onto the undo stack, applies the new program/form, sets `dirty=true`, and triggers debounced validation. `undo/redo` swap snapshots.
  - `validate` (debounced ~300ms): build the candidate `AbilityDef` (form + program + schemaVersion:2), call `validateAbilityProgram`, set `issues`.
  - `save()` — build the save def: `saveRequestFromForm(form)` for the top-level fields, set `schemaVersion:2` + `program: serializeProgram(program)`, POST via `saveEditorAbility`; on `EditorValidationError` show the message; on success reload + reselect. GATE: refuse to save when `hasBlockingError(issues)` (surface it). **Contract:** a saved v2 ability keeps its top-level cast-setup/targeting fields (form) AND its program — both are written (Phase-4 requirement).
  - `convert()` — `convertAbility(form.id)`, load the returned def into `form`/`program`, set `warnings`, mark dirty (user reviews + saves). Only offered when `isLegacy`.
  - `dirty`/unsaved-guard flag exposed for the panel to confirm before discarding.
  - Undo/redo snapshots = structuredClone of `{form, program, selected}` (cap stack at ~50).

- [ ] **Step 3–4.** Test the composable's pure-ish logic where feasible (mock the api module): selectAbility populates form+program+isLegacy; a mutation sets dirty + can undo; undo restores; save gates on blocking error. `vue-tsc -b` clean.

- [ ] **Step 5: Commit** — "feat(ability-editor): useAbilityBuilder state backbone + program tree ops".

---

### Task 2: `AbilityBuilderPanel.vue` shell + layout + list

**Files:**
- Create: `client/src/game-portal/src/components/ability-builder/AbilityBuilderPanel.vue`
- Modify: `client/src/game-portal/src/views/AbilityEditor.vue` (mount the new panel)
- Test: `client/src/game-portal/src/components/ability-builder/AbilityBuilderPanel.test.ts`

- [ ] Build the shell on `EditorShell`:
  - `#sidebar`: `EditorSidebar` (reuse the existing ability-list grouping by damage school + search + new/duplicate, mirroring `AbilityEditorPanel.vue`'s `sidebarGroups`). Emits select/new/duplicate → composable.
  - `#main`: `EditorHeader` (title = display name/id, badge = damageType, breadcrumb = type•category, file path `server/internal/game/catalog/abilities/{id}/{id}.json`, **Save** top-right, `save-disabled` when busy/no-id/`hasBlockingError`, saved-label, error; a **Convert to Composable** button shown only when `isLegacy`; a **Delete/Reset** action). Then the stacked body: `<AbilityOverviewCard/>` (Task 6) + `<AbilityFlow/>` (Task 3) inside `GameScrollArea` + `<ActionPalette/>` (Task 5) pinned at the bottom.
  - `#rail`: `<ItemInspector/>` (Task 4).
  - Empty state (no ability selected) + loading state.
  - Unsaved-changes guard: if `dirty`, confirm before `selectAbility`/`newAbility`/navigating away (a simple `window.confirm` or a small in-panel confirm — match how other editors handle it, or use a confirm dialog if one exists in the toolkit).
  - Wire `onMounted(() => builder.load())`.
- Swap `views/AbilityEditor.vue` to render `<AbilityBuilderPanel />` (keep the old `AbilityEditorPanel.vue` file in the repo for now; just stop mounting it).
- Test: mounts, shows empty state, selecting an ability from a mocked list renders the header + flow region.
- [ ] **Commit** — "feat(ability-editor): AbilityBuilderPanel shell + three-region layout".

---

### Task 3: `AbilityFlow` + `FlowTriggerCard` + `FlowActionCard`

**Files:** `ability-builder/AbilityFlow.vue`, `FlowTriggerCard.vue`, `FlowActionCard.vue` (+ test)

- [ ] Render `program` as a vertical sequence of trigger cards; each trigger card lists its action cards; nested triggers (a `create_zone` action's zone `on_zone_tick`, a presentation's markers, an action's `Children`) render indented recursively (bounded — render root triggers + `Presentations[].triggers` + one level of action `Children`; deeper nesting shows a "nested flow" affordance).
- **FlowTriggerCard**: header (trigger type label + optional name + timing summary e.g. marker/tickInterval), collapse/expand, "Add action" affordance (opens palette focus / or the bottom palette targets the selected trigger), selection (click selects the trigger → inspector). Validation badge if any issue path maps to this trigger (`issuesForPath`).
- **FlowActionCard**: compact one-line summary (action display name + a human summary of its config/target, e.g. "Deal Damage — 140 fire to enemies within 230"), selection (click → inspector), per-card controls: move up/down, duplicate, disable (toggle — disabled cards render dimmed), delete. A **"display-only"** chip when the action type is not `runnable` (from `schemaForAction(schema,type).runnable`). Validation badge (error=red, warning=amber) from `issuesForPath(issues, actionPath)`.
- Compact summaries: a `summarizeAction(action, schema)` helper produces the one-liner from the action's type + config + target query (best-effort; unknown → the type label).
- Reorder via up/down buttons (drag-and-drop is explicitly NOT required for the first pass per design §9 — a `TODO` note is fine).
- Styling: LoC cards (wood/brass, gold accents), compact. Selected card gets a gold border/highlight. NO literal `cursor:` in CSS.
- [ ] Test: renders a program's triggers+actions; clicking an action emits/selects it; disabled action shows dimmed; a non-runnable action shows the display-only chip; an action with a matching issue shows the badge.
- [ ] **Commit** — "feat(ability-editor): flow view with trigger/action cards".

---

### Task 4: Schema-driven `ItemInspector` + `SchemaField`

**Files:** `ability-builder/ItemInspector.vue`, `SchemaField.vue`, `TargetQueryEditor.vue` (+ tests)

- [ ] **ItemInspector**: edits `selected`:
  - `{kind:'ability'}` → identity + cast-setup sections (reuse `EditorField`/`SectionCard`/`FilterableSelect`): Display Name, Type, Category (enum from catalogs), Damage Type (enum), Tags (comma list), Icon (reuse the existing icon field/gallery from `AbilityEditorPanel.vue` if cheap, else a simple path field), Entry (type enum + relations multiselect + range sentinel-or-number), Mana/Cooldown/Cast Time, Auto-cast trio. Edits call `updateForm(patch)`.
  - `{kind:'trigger'}` → trigger type (enum from `schema.enums.triggerTypes`), name, timing (marker/frame/tickInterval per type). `updateTrigger`.
  - `{kind:'action'}` → **schema-driven**: look up `schemaForAction(schema, action.type)`; render each `SchemaField` for `action.config`, grouped by `section`. Plus a Target section (if the action takes targets): a `TargetQueryEditor` for `action.target` OR an Input `targets` ContextRef picker. `updateAction`.
  - Show the selected node's validation issues at the top of the inspector (`issuesForPath`).
- [ ] **SchemaField**: given a `SchemaField` descriptor + a bound value, render the right control by `control`: `number`→number input; `text`→text; `boolean`→checkbox; `enum`→`FilterableSelect` (options from `field.options` or the relevant `schema.enums`); `multiselect`→checkbox group; `duration`/`percentage`→number with unit affordance; `sentinel_number`→the match-attack-range checkbox+number pattern (reuse from `AbilityEditorPanel.vue`); `asset`→a select of effect/projectile ids; `context_ref`→a picker of valid context keys for the current trigger (best-effort list from enums; full context-gating is a later refinement — `TODO`); `target_query`→delegate to `TargetQueryEditor`; `animation_marker`/`nested_triggers`→a simple text/placeholder with a `TODO(phase-6)` note. Unknown control → a text input fallback (never crash). Emits `update` with the new value.
- [ ] **TargetQueryEditor**: edit a `TargetQueryDef` — source (enum), origin (enum), relations (multiselect), radius (sentinel-or-number), ordering (enum), maxCount (number), includeInitialTarget/excludeSource (checkboxes). Emits the updated query.
- [ ] Tests: selecting an action renders its schema fields; editing a field calls updateAction with the merged config; the ability node renders identity+cast-setup; an enum field renders options.
- [ ] **Commit** — "feat(ability-editor): schema-driven inspector + SchemaField + target-query editor".

---

### Task 5: `ActionPalette`

**Files:** `ability-builder/ActionPalette.vue` (+ test)

- [ ] Searchable, categorized palette of action types (categories per design §9: Targets / Combat / World / Resources / Flow / Presentation — map each action type to a category via a small table). Each entry shows the action label + a **"display-only"** marker when not `runnable`. Search filters by label/type. Clicking an entry calls `addAction(selectedTriggerId, type)` (adds to the currently-selected trigger; if an ability/action is selected, resolve to its owning trigger; if nothing suitable is selected, disable the palette with a hint "select a trigger"). Drag-and-drop is NOT required (design §9).
- Non-runnable actions are still addable (the author may be building for a future phase) but clearly marked; OR disable them — DECISION: keep addable + marked (so authored programs can be built ahead of executor support, matching the compiler's structural completeness). Document the choice.
- [ ] Test: renders categories; search filters; clicking an action emits add with the type; non-runnable actions show the marker.
- [ ] **Commit** — "feat(ability-editor): searchable categorized action palette".

---

### Task 6: `AbilityOverviewCard` + convert flow + validation/save wiring + undo/redo

**Files:** `ability-builder/AbilityOverviewCard.vue`, `ConvertDialog.vue` (+ tests); wire in `AbilityBuilderPanel.vue`

- [ ] **AbilityOverviewCard**: compact summary of the selected ability — icon, display name, category, mana/cooldown/cast-time, a one-line entry-targeting summary (e.g. "Ground point · enemies · range 400"), tags, and the generated description (from `form.generatedDescription`; editable override is a smaller concern — reuse the existing description-override card behavior if cheap, else show generated read-only with a "override" affordance). Clicking a summary group selects the ability node in the inspector (navigation surface). A "display-only" banner when `!runnable` explaining the ability uses mechanics the runtime doesn't execute yet.
- [ ] **ConvertDialog**: shown when the user clicks Convert on a legacy ability — lists the `warnings[]` from `convert()` and requires explicit confirm (then the converted form+program load; user saves). If `runnable` is false, emphasize the degradation.
- [ ] **Wiring in the panel**: Save button disabled when `hasBlockingError(issues)` (with a tooltip/summary of the blocking issues); a validation summary strip (error/warning counts) in/under the header; undo/redo buttons + `Ctrl+Z`/`Ctrl+Shift+Z` (or `Ctrl+Y`) keybindings scoped to the panel; unsaved-changes confirm on discard/navigate. `savedLabel` after save.
- [ ] Tests: overview renders summary from a form; convert dialog shows warnings; Save disabled when a blocking error is present.
- [ ] **Commit** — "feat(ability-editor): overview card, convert flow, validation gating, undo/redo".

---

### Task 7: Integration polish + run the app

**Files:** cross-component fixes; no new components expected

- [ ] Full `npm run test` (only the known `ListEditorPanel.test.ts` may fail) + `npx vue-tsc -b` clean.
- [ ] Verify the CLAUDE.md cursor rule: grep the new `ability-builder/` CSS for literal `cursor:` values (only `not-allowed` allowed) — remove any others.
- [ ] LAUNCH THE APP and drive the editor end-to-end (use the project run skill / start the client+server): select a legacy ability (see its compiled flow + display-only badge), select a node (inspector renders schema controls), add an action from the palette, edit a field, see live validation, and — for `raise_skeleton` (fully runnable) — convert + save a v2 ability and confirm it round-trips (reload shows the authored program). Capture screenshots of the editor for review.
- [ ] **Commit** — "chore(ability-editor): phase 5b integration polish".

---

## Self-review notes

- **Save contract:** a v2 save writes BOTH the top-level cast-setup/targeting fields (so the `begin*` cast gates work — Phase-4 requirement) AND `program`; `saveRequestFromForm` strips display-only fields (`generatedDescription`/`compiledProgram`/`runnable`).
- **Legacy vs authored:** legacy abilities render `compiledProgram` read-only with a Convert affordance; authored (v2) render/edit `program` directly. `runnable` drives a "display-only" treatment, never "broken".
- **Single-source:** inspector controls come from `fetchActionSchema` (registry), validation from `validateAbilityProgram` (server), enums from the schema bundle — no hardcoded control lists.
- **Conventions:** no literal `cursor:` in component CSS (global rules; `not-allowed` only); `--font-*` tokens; reuse `editor/` toolkit; Save top-right; LoC styling; `vue-tsc -b` build-mode type-check.
- **Deferred (not silent):** live in-editor preview (Phase 6 region), drag-and-drop reorder, timeline/compact flow views, copy-paste, convert-inline-to-named trigger, full context-reference gating in the inspector, animation-marker authoring UI — each a `TODO` at its site.
- **Autonomous build:** per the user's choice, build all tasks without visual checkpoints, then Task 7 launches the app + captures screenshots for end review.
