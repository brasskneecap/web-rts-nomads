# Composable Abilities Phase 7: Nested-Trigger Authoring — Reference Plan

**Goal:** Make nested triggers editable in the flow editor, so meteor's impact damage AND its burning-crater DoT can be authored directly.

**Acceptance fixture:** Convert meteor → v2, then edit the `impact` trigger's damage and the crater's `burn` DoT in the flow view. Meteor nests 3 levels:
`cast → play_presentation(p_meteor)` … `p_meteor.triggers[impact] → sel + dmg + zone` … `zone.config.triggers[burn] → bsel + bdmg`.

**Scope decisions (user-confirmed 2026-07-16):**
- Include the Go validator recursion into `create_zone`'s `config.triggers`.
- Nested triggers + their actions become editable at any depth. Presentation *nodes* stay read-only headers (their asset/scale are already edited via the `play_presentation` action config — don't create a second editor for the same value).

---

## Ground truth (verified against the Go source, 2026-07-16)

These facts drove the design; re-verify before contradicting them.

1. **Only two nesting slots exist in practice.**
   - `AbilityActionDef.Children` (`children`) — sibling of `config`, fired via `on_action_complete`.
   - `createZoneConfig.Triggers` (`config.triggers`) — decoded *only* by `create_zone` (`ability_zone.go:166,213-219`).
   - `ZoneDef.Triggers` / `StatusDef.Triggers` / `ProjectileSpawnDef.Triggers` are **dead model types** — no ActionType decodes them. `apply_status` uses `applyStatusConfig` (no triggers); `launch_projectile` has no descriptor at all. There is **no** `config.zone.triggers`.
   - Adjacent but out of scope: `conditionalConfig.Then` (`config.then`) and `repeatConfig.Actions` (`config.actions`) nest *actions* without triggers — deliberately in config, not Children, so they don't auto-fire.

2. **The validator does not descend into action config** (`ability_program_validate.go:160-171` TODO). Meteor's `burn`/`bsel`/`bdmg` subtree gets no validation and no duplicate-id check today. Task 1 fixes this.

3. **Validator path grammar** (index-based, fully qualified, unbounded depth):
   `triggers[0]` · `triggers[0].actions[1]` · `triggers[0].actions[1].children[0].actions[0]` · `namedTriggers[foo].actions[0]` · `presentations[0].triggers[0].actions[2]`

4. **Trace path grammar is id-based, local, and carries NO ancestry.** Each boundary restarts the path. Meteor traces as three disjoint namespaces:
   `cast.actions[meteor]` → `marker[impact].actions[sel|dmg|zone]` → `burn.actions[bsel|bdmg]`.
   Roots by entry point: `trg.ID` (cast/zone-tick/children), `marker[<marker>]` (`ability_marker.go:180`), `conditional.then`, `repeat`, `namedTrigger[<id>]`.
   ⇒ `refFromPath` **cannot walk down** a trace path; it must **search by id**.

5. **Ids share one global namespace** across a validation pass (`seenIDs`, triggers+actions together), but ids inside `config.triggers` are never visited → currently uncheckable. Task 1 closes this. Presentation ids are never duplicate-checked (out of scope).

---

## Design

### Path model (`programTree.ts`) — the core enabler

`NodeRef`'s flat `{triggerId, actionId}` cannot address nested nodes. Replace with a typed id-chain:

```ts
export type NodeSeg =
  | { kind: 'presentation'; id: string }
  | { kind: 'trigger'; id: string }
  | { kind: 'action'; id: string }

export type NodePath = NodeSeg[]

export type NodeRef =
  | { kind: 'ability' }
  | { kind: 'trigger'; path: NodePath }   // last seg is a trigger
  | { kind: 'action'; path: NodePath }    // last seg is an action
```

Meteor's crater damage = `[{presentation:p_meteor},{trigger:impact},{action:zone},{trigger:burn},{action:bdmg}]`.

**Why typed segments, not a bare `string[]`:** segment 0 is ambiguous otherwise (a root trigger id vs a presentation id), and ids aren't guaranteed unique across those two namespaces. Explicit container kinds cost verbosity only in tests, and every op funnels through one resolver.

**Why ids, not the validator's index grammar:** indices shift under add/remove; ids are stable identity. This preserves the existing rationale at `ItemInspector.vue:298`. Index paths are *derived* for validation lookup (see below).

### Resolution + mutation

- `resolveNode(prog, path)` — walks segments. Container per step: root → `prog.triggers` / `prog.presentations`; presentation → `p.triggers`; trigger → `t.actions`; action → nested trigger slots.
- `nestedTriggersFor(action)` — union of `action.children` and `action.config.triggers` (replaces the duck-typed one-slot-wins read at `FlowTriggerCard.vue:127-132`, which currently hides config triggers whenever `children` is non-empty).
- `slotOfNestedTrigger(action, id)` → `'children' | 'config'` — write-back targets the slot the id was found in.
- **Add rule:** `create_zone` → `config.triggers`; every other action → `children`.
- `updateNodeAt(prog, path, fn)` — generic immutable spine rebuild. **Every** op (add/remove/move/duplicate/update/setDisabled) is expressed through it. This is what makes depth free.

### Derived validation paths

`indexPathFor(prog, path)` → validator grammar, for `issuesForPath` lookup:
`triggers[i]` · `presentations[p].triggers[i]` · `${parent}.actions[j]` · `${parent}.children[k]` · `${parent}.config.triggers[k]` ← **new segment, must match Task 1's Go grammar exactly.**

### Latent bug to fix in the same pass

`duplicateAction` (`programTree.ts:141-153`) `structuredClone`s an action and re-ids only the top level. Duplicating a `create_zone` clones `burn`/`bsel`/`bdmg` verbatim → id collision. Fix: re-id the whole cloned subtree via `nextUniqueId`.

---

## Tasks

### Task 1 — Go: validator recursion into `create_zone` config triggers
**Files:** `server/internal/game/ability_program_validate.go`, `ability_program_validate_test.go`

In `walkAction`, reuse the *already-decoded* descriptor config (don't decode twice); when `action.Type == ActionCreateZone` and decode succeeded, walk `cfg.Triggers` with path `fmt.Sprintf("%s.config.triggers[%d]", path, i)`. Only recurse on successful decode.

Nested triggers then get duplicate-id checks and the `invalid_tick_interval` check for free — note this means a compiled meteor's `burn` trigger finally gets its tick check actually run (`ability_compile.go:432-434` sets `TickInterval` specifically to satisfy it, but the check could never fire).

Update the TODO block at `:160-171` to drop the now-done zone case, keeping status/projectile listed as still-dead.

**Tests:** nested trigger with `tickInterval: 0` → error at `triggers[0].actions[0].config.triggers[0]`; duplicate id between a root action and a nested action → `duplicate_id`; compiled meteor produces no *new* errors (guard against the recursion breaking the existing fixture); malformed `create_zone` config still reports `invalid_config` once, not twice.

**Verify:** `cd server && go test ./... -count=1`

### Task 2 — TS: path model + resolver
**Files:** `programTree.ts`, `programTree.test.ts`

`NodeSeg`/`NodePath`/`NodeRef`, `resolveNode`, `nestedTriggersFor`, `slotOfNestedTrigger`, `findNodePathById` (tree search — needed by Task 5), `indexPathFor`. Pure functions, no Vue.

**Tests:** resolve at each of meteor's 3 depths; union slot read (action with BOTH `children` and `config.triggers` — structurally legal, nothing forbids it); `indexPathFor` emits the Task 1 grammar; unresolvable path → `undefined`, never a throw.

### Task 3 — TS: `updateNodeAt` + all ops at depth
**Files:** `programTree.ts`, `programTree.test.ts`

Rewrite `addTrigger`/`removeTrigger`/`addAction`/`removeAction`/`moveAction`/`duplicateAction`/`setActionDisabled`/`updateAction`/`updateTrigger` in terms of `updateNodeAt`, taking `NodePath`. Fix the duplicate-subtree re-id.

**Tests:** every op at depth 3; input program never mutated (immutability is what undo/redo rests on); duplicate of a `create_zone` mints fresh ids for the whole subtree; add-trigger targets `config.triggers` for `create_zone` and `children` otherwise.

### Task 4 — TS: `refFromPath` for both grammars
**Files:** `refFromPath.ts`, `refFromPath.test.ts`

Index grammar: add `presentations[i].triggers[j]`, `.children[k]`, `.config.triggers[k]`. Id grammar: resolve by **search** (`findNodePathById`) since trace paths lack ancestry; add a `marker[X]` root that matches a trigger by `timing.marker === X`, not by id. `conditional.then` / `repeat` / `namedTrigger[...]` → `null`, documented as honestly unaddressable.

**Tests:** `burn.actions[bdmg]` and `marker[impact].actions[sel]` resolve to full nested paths against the compiled-meteor fixture. (Meteor's marker string and trigger id are both `impact` — pick a fixture where they *differ* so the test can't pass by coincidence.)

### Task 5 — TS: `useAbilityBuilder` ops → paths
**Files:** `useAbilityBuilder.ts`, `useAbilityBuilder.test.ts`

Ops take `NodePath`. `addAction` still auto-selects the new node (now at depth). Undo/redo unchanged — `NodePath` is plain data, so `structuredClone` keeps working.

### Task 6 — Consumer migration (single task; the compiler enumerates the call sites)
**Files:** `FlowTriggerCard.vue`, `FlowActionCard.vue`, `AbilityFlow.vue`, `ItemInspector.vue`, `ActionPalette.vue` + their tests

- `FlowTriggerCard` renders nested triggers as real recursive child cards (self-referential component), not the dead label at `:50-57`. Nested add-trigger/add-action affordances. Delete the read-only TODO comments.
- `AbilityFlow` presentations section: nested triggers become editable cards; presentation header stays read-only. Delete the TODO at `:16-21`.
- `FlowActionCard`: `path` prop replaces `triggerId`; `isSelected` compares paths.
- `ItemInspector`: `selectedTrigger`/`selectedAction` via `resolveNode`; `selectedPath` via `indexPathFor`. Already schema-driven ⇒ mostly free.
- `ActionPalette`: `activeTriggerPath` replaces `activeTriggerId`; drop the root-only note at `:79`.

Watch indentation depth at 3 levels in the rail-width flow column. No literal `cursor:` in CSS (`not-allowed` only).

**Verify:** `cd client/src/game-portal && npx vue-tsc -b && npx vitest run` (3 pre-existing `ListEditorPanel.test.ts` failures are unrelated).

### Task 7 — Acceptance verification
Playwright at `http://localhost:5173/#/ability-editor` (dismiss the "CLICK ANYWHERE TO BEGIN" splash). Convert meteor → edit `impact`'s `dmg` amount → edit `burn`'s `bdmg` amount → confirm validation badges appear on nested nodes (proves Task 1 ↔ Task 2 grammars agree) → Preview tab still replays → click a `burn.actions[bdmg]` trace row and confirm it selects the nested action (proves Task 4).

---

## Invariant

Production stays byte-identical: no catalog ability is v2, all new paths gated behind `SchemaVersion>=2 && Program!=nil`. Task 1 is the only server change and only adds *diagnostics* — it must not alter execution.

## Out of scope
`conditional.then` / `repeat` `config.actions` action-nesting; presentation node editing; `namedTriggers` authoring; the deferred executor pass (`launch_projectile` / channel / charge / moving-zone); status/projectile config triggers (dead until something decodes them).
