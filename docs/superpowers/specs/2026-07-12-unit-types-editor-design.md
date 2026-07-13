# Unit-Types Editor — Design Spec

**Sub-project 2 of the WC3-style all-in-one world editor.** A data editor for
authoring and editing unit type definitions (`UnitDef`), following the
writable-overlay + disk-marshaling template established by the item editor
(branch `ui-item-editor`). Wires into the world editor toolbar's currently
disabled `unit-types` category, and is also reachable as a standalone route
(mirroring `/item-editor`).

**Branch:** `unit-types-editor` (new; copies patterns from the item editor,
does not modify it).

---

## 1. Goal

Let an author, from inside the world editor (or a standalone route):

- **Edit** any of the ~19 existing unit types across the 4 factions — full
  coverage of every authored `UnitDef` field.
- **Create** a brand-new unit type from a blank form (pick faction + type id,
  fill fields). New units have no art and render via the renderer's placeholder
  path until the future art-packaging sub-project (sub-project 4) lets art be
  authored/uploaded.
- **Delete** an edit: remove the writable override so the type reverts to its
  embedded definition, or, for an editor-created type, remove it entirely.

Edited/created unit types are immediately usable by **placing them in the world
editor** (`PlacedUnit`) and running an ephemeral playtest.

### Non-goals (explicitly out of scope for v1)

- **Promotion-path editing.** Paths live in a separate embedded catalog tree
  (`catalog/units/<faction>/<unit>/paths/…`, loaded by `path_defs.go`) and have
  their own disabled toolbar category (`unit-paths`). This editor edits only the
  base `UnitDef`. The `pathChances` field IS modeled (it's a field on `UnitDef`),
  but the path *definitions* it references are not authored here.
- **Sprite / art authoring.** The three art blobs (`attackVisual`, `bounds`,
  `shadow`) and spritesheet assets are NOT authored in v1. They round-trip
  untouched on edit; blank-created units have none and render as placeholders.
  Art authoring is the art-packaging sub-project.
- **Wiring new units into build menus / training.** A new unit type is
  referenced by nothing until the author sets `requiresBuildings` / `capabilities`
  and a building trains it. v1 makes new types *placeable*, not *trainable*.

---

## 2. Architecture

Mirror the item editor end to end.

### 2.1 Server (`server/internal/game/`)

New file **`unit_persistence.go`** (mirrors `item_persistence.go`):

- Env var **`UNIT_CATALOG_DIR`**; `resolveUnitsDir()` — env override, else the
  dev source dir `internal/game/catalog/units`.
- Overlay state: `runtimeUnitsMu sync.RWMutex` + `runtimeUnits map[string]*UnitDef`.
- **`LoadPersistedUnitsIntoOverlay()`** startup hook, called from
  `server/cmd/api/main.go` (alongside `LoadPersistedItemsIntoOverlay`).
  Walks `<dir>/<faction>/<type>/<type>.json`, parses, registers each in the
  overlay.
- Overlay-wins reads: **`getUnitDef(type)`** consults `runtimeUnits` first, then
  the embedded `unitDefsByType`; **`ListUnitDefs()`** returns the merged set
  (overlay entries shadow embed entries of the same type).
- **`SaveUnitDef(def)`**: validate → write `<dir>/<faction>/<type>/<type>.json`
  (authored JSON) → register in overlay. Overlay registered only AFTER a
  successful write.
- **`DeleteUnitOverride(type)`**: remove the override file(s) and the overlay
  entry. If the type also exists in the embed, `getUnitDef` then returns the
  embedded def (revert); if editor-created, it's gone.
- **`unitIDPattern = ^[a-z0-9_]+$`** guards both `type` and `faction` path
  segments against traversal, enforced at persistence and handler layers.

**Refactor (`unit_defs.go`):** extract the loader's inline validation into a
standalone **`validateUnitDef(def UnitDef) error`**. Today the checks live inside
the `loadUnitDefsByType` var-initializer and `panic` on failure. Factor them into
an error-returning function that both the loader (wrap panic) and the editor
(surface error) call. This mirrors how items expose `validateItemDef`.

New file **`unit_editor.go`** (mirrors `item_editor.go`): `SaveEditorUnit` /
`DeleteEditorUnit` orchestrators — validate-first-then-write, `faction`
required, type-id uniqueness enforced on create. Validation errors wrapped as
`editorValidationError` (reuse the existing item-editor wrapper /
`IsEditorValidationError`, or a units equivalent with identical shape).

### 2.2 Server HTTP (`server/internal/http/`)

In `editor_handlers.go` `registerEditorRoutes`, add:

- `POST /units` — save (create or edit) a unit def.
- `DELETE /units/{id}` — delete override / editor-created unit
  (`strings.Contains(id,"/")` guard + `unitIDPattern`).

In `router.go`, add a read route:

- `GET /catalog/units` — returns the merged `ListUnitDefs()` as authored JSON,
  including the art blobs, for the editor to load.

### 2.3 Desktop (`desktop/src-tauri/src/supervisor.rs`)

Add a 5th catalog env dir alongside the existing four: **`UNIT_CATALOG_DIR`** →
`catalog/units`, set from `locate_repo_catalog_dir()`. Without this, the packaged
Tauri build hits the "units directory not found" failure the item editor already
fixed for items.

### 2.4 Client (`client/src/game-portal/`)

- **`game/units/unitEditorApi.ts`** — `saveEditorUnit()` (`POST /units`),
  `deleteEditorUnit()` (`DELETE /units/{id}`), `fetchUnitDefsForEditor()`
  (`GET /catalog/units`), `EditorValidationError`. Mirrors `itemEditorApi.ts`.
- **`game/units/unitEditorForm.ts`** — the form model. `createBlankForm()`,
  `formFromDef(def)`, `saveRequestFromForm(form)`. Holds every modeled field as
  typed state PLUS an opaque `remainder` object carrying unmodeled keys (the 3
  art blobs + any unknown future keys). `formFromDef` → `saveRequestFromForm` on
  any def is the identity (lossless round-trip). Mirrors `itemEditorForm.ts`.
- **`components/UnitTypeEditorPanel.vue`** — the sectioned collapsible form
  (see §3), a unit list with New / Delete, and a faction selector. Self-contained
  (no required props), fetches its catalog on mount — same shape as
  `ItemEditorPanel.vue` so it can be mounted bare in the world-editor popup.
- **`views/UnitTypeEditor.vue`** + route **`/unit-type-editor`** in
  `router/index.ts` (mirrors `ItemEditor.vue` / `/item-editor`).
- **World editor wiring:**
  - `components/world-editor/WorldEditorToolbar.vue:33` — flip the `unit-types`
    entry to `enabled: true`.
  - `components/world-editor/WorldEditorPanel.vue` — add a `unitTypesPopupOpen`
    ref, a `case 'unit-types':` in the toolbar-select handler, a modal overlay
    hosting `<UnitTypeEditorPanel />` (identical `we-modal` pattern to the item
    popup added in the shell branch), and an active-highlight line. NOTE: the
    world editor panel is a copy that has already diverged from
    `MapEditorPanel.vue`; the old map editor must NOT be touched.

### 2.5 Extend the client `UnitDef` type

The client TS `UnitDef` (`game/maps/unitDefs.ts`) is currently a partial view.
The editor form needs to carry every authored field for lossless round-trip.
Rather than widen the runtime `UnitDef` used by the renderer (risking regressions),
the editor form defines its OWN complete authored shape in `unitEditorForm.ts`
(all modeled fields + `remainder`), decoupled from the render-time `UnitDef`.
The renderer's `UnitDef` is left unchanged.

---

## 3. Form sections (full field coverage)

All fields from the Go `UnitDef` struct EXCEPT the `-`-tagged advancement-only
fields (`BonusArrows`, `TrapEffectBonus`, `TrapRadiusBonus` — never authored) and
the 3 art blobs (round-tripped, not shown):

| Section | Fields |
|---|---|
| **Identity** | `type`, `name`, `faction`, `archetype`, `trainLabel` |
| **Stats** | `hp`, `armor`, `damage`, `attackRange`, `attackSpeed`, `moveSpeed`, `splashRadius` |
| **Cost** | `resourceCost` (map string→int, add/remove rows), `meatCost`, `spawnSeconds` |
| **Combat** | `combatProfile`, `attackType`, `damageType`, `targetableTypes` (multi), `projectile`, `projectileScale` |
| **Resources** | `goldGatherAmount`, `woodGatherAmount` |
| **Mana** | `maxMana`, `manaRegenRate`, `channelLoop` (`{start,end}`) |
| **Vision** | `visionRange`, `flyer` |
| **Abilities** | `abilities` (string list), `capabilities` (string list) |
| **Gating** | `requiresBuildings` (string list), `pathChances` (map string→float, add/remove rows) |
| **Rewards** | `dominionPointDropChance`, `dominionPointAmount`, `spawnExp`, `experience` (nullable int) |
| **Flags** | `nonCombat` |

**Cross-reference field UX:** where a catalog endpoint already exists, list
fields (`requiresBuildings`, `abilities`, `damageType`, `projectile`) SHOULD be
selection controls populated from that catalog for good UX; where none exists
they fall back to validated free-text. Whichever is used, the server
`validateUnitDef` is the authority — the UI must never be the only guard.

---

## 4. Data flow & lossless round-trip

**Load:** `GET /catalog/units` → merged authored `[]UnitDef` (incl. art blobs).
The client splits each def into (modeled fields → typed form state) +
(everything else → `remainder`). In practice `remainder` = the 3 art blobs +
any unknown keys.

**Edit:** form mutates typed fields; `remainder` rides along untouched.

**Save:** `saveRequestFromForm` recombines typed fields + `remainder` into one
authored `UnitDef` JSON, POSTs to `/units`. Server `validateUnitDef` →
`SaveUnitDef` writes `<UNIT_CATALOG_DIR>/<faction>/<type>/<type>.json` and
registers the overlay. Subsequent `getUnitDef(type)` returns the overlay copy —
a placed unit of that type spawns with the new stats immediately.

**Blank create:** `createBlankForm` starts zeroed + empty `remainder`; author
picks `faction` + `type`. No art blobs → placeholder render.

**Delete:** `DELETE /units/{id}` removes the override file + overlay entry;
reverts to embed if present, else removed.

---

## 5. Validation, blast radius & safety

- **`validateUnitDef`** enforces: `type` & `faction` non-empty and match
  `unitIDPattern`; type-id uniqueness on create; `damageType` in the registry
  (when set); `combatProfile` / `projectile` / `requiresBuildings` references
  exist (when set); `pathChances` weights valid; numeric ranges (`hp` > 0,
  chances in [0,1], non-negative costs). Validate-first-then-write — a bad def
  never touches disk.

- **Live-match snapshot check (REQUIRED verification during planning).** The
  item editor shipped a bug where `GameState.itemCatalog` snapshotted the
  embed-only singleton, making editor items invisible to gameplay. Before
  implementation, verify whether any per-match unit-catalog snapshot exists that
  spawn reads instead of `getUnitDef`. If spawn reads `getUnitDef` live → edits
  apply to running ephemeral playtests for free. If a snapshot exists → the
  snapshot MUST source from the merged `ListUnitDefs()`. This is a plan task, not
  an assumption.

- **Advancement copies.** `Player.EffectiveUnitDefs` derive from the base def at
  spawn via `applyRankModifiersLocked`; editing the base flows through. No
  separate write path.

- **New-type reachability (stated limitation).** A brand-new type is referenced
  by nothing (no building trains it, no camp spawns it). It is usable by placing
  it in the world editor (`PlacedUnit`) — this editor's purpose. Wiring into
  build menus is the author's job via `requiresBuildings` / `capabilities`. Not a
  bug.

- **Hydrate/placement drop precedent.** `hydratePlacedUnits` already drops
  unknown `unitType` with a warning. A placed unit whose type was later deleted
  degrades gracefully (dropped), never crashes.

---

## 6. Error handling

- **Validation failures** → structured `editorValidationError`; panel shows the
  message inline (e.g. "faction is required", "type 'archer' already exists",
  "unknown projectile 'foo'"). No write occurs.
- **Path traversal / bad id** → `unitIDPattern` rejects at handler + persistence.
- **Disk write failure** → 500 with error text; overlay registered only after a
  successful write, so a failed save never leaves inconsistent in-memory state.
- **Delete of non-existent / embed-only-no-override** → no-op success (revert
  semantics), matching items.
- **Client fetch failure** → panel shows a load error and disables save rather
  than posting a partial def.
- **Blank unit in playtest** → renders via the placeholder path (degraded
  visuals, never a crash). Spec requires hardening the no-`bounds` case in
  `unitSprites.ts` so a blank unit yields a simple placeholder shape.

---

## 7. Testing

**Server:**
- `validateUnitDef`: a valid def passes; one case per rejection reason
  (empty type, empty faction, bad id pattern, duplicate type on create, unknown
  damageType, unknown projectile, unknown requiresBuildings, out-of-range chance).
- `SaveUnitDef` / `DeleteUnitOverride` round-trip: write → `getUnitDef` returns
  the overlay copy → delete → reverts to embed.
- **Lossless round-trip**: a def carrying `attackVisual` / `bounds` / `shadow`
  survives save→reload with those blobs byte-preserved.
- Overlay-wins ordering: an override of an embedded type shadows the embed in
  `ListUnitDefs`.

**Client:**
- `unitEditorForm`: `formFromDef` → `saveRequestFromForm` is identity on a full
  def including art blobs (lossless).
- `createBlankForm` produces a minimally-valid shell (faction/type settable, no
  art).

**Integration:**
- `POST /units` then `GET /catalog/units` shows the merged edit.

**Manual E2E (milestone proof):**
1. World editor → Unit Types → edit `archer` `damage` → Save.
2. Place an archer → Play → it fights with the new damage → Stop.
3. Create a blank unit (pick faction + id) → place it → it renders as a
   placeholder and is selectable.
4. Delete the archer override → its stats revert to the embedded default.

---

## 8. Global constraints (bind every implementation task)

- Branch `unit-types-editor`; never modify the item editor, and never modify the
  old map editor (`MapEditorPanel.vue`, `views/Editor.vue` — zero diff).
- Follow the item-editor overlay + disk-marshaling template; do not invent a new
  persistence pattern.
- `game/` package must NOT import `profile`; `Locked` = caller holds `s.mu`;
  deterministic sim (no wall-clock / unseeded rand in sim paths).
- Store targets/refs by ID/string key, never by pointer across ticks (AI_RULES).
- No literal `cursor:` declarations in new component CSS except `cursor:
  not-allowed` on forbidden-action states.
- `unitIDPattern = ^[a-z0-9_]+$` for `type` and `faction` path segments.
- Overlay registered only after a successful disk write.
- All Go commands from `server/`; client from `client/src/game-portal`
  (`npm run test`, `npm run build`). `gofmt -l` flags the whole checkout (CRLF) —
  use `go vet` / `go build` as gates. Known pre-existing test failures
  (cmd/api `TestServerReadyLineAndStdinShutdown`; possibly ws
  `TestSPBaseline_StructuralShape`) are unrelated; introduce no NEW failures.
- Commit messages: short imperative.
