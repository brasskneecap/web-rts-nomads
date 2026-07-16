# Effects Editor — Design

**Date:** 2026-07-13
**Branch:** `reference-def-editors` (shared with the Projectiles editor + Perks wiring; off `main` base `d4fbff8`)
**Program context:** World editor, "reference-def editors" batch — sub-project 1 of 3 (Effects → Projectiles → Perks-wiring). Effects is built first because it is a leaf (references nothing), so a newly-authored effect can immediately be referenced by a new projectile/ability.

## Problem & goal

`EffectDef` (the catalog of visual effects — `healing_glow`, `holy_explosion`, `fizzle`, `burning`, `meteor`, …) is catalog-loaded, embed-only, with a `GET /catalog/effects` read route but **no writable overlay and no editor**. Abilities and projectiles reference effects by id (dropdowns), but authors cannot create or tune an effect def. Add an Effects editor — create/edit/delete `EffectDef`s via a writable overlay — mirroring the shipped Abilities editor triad.

## Foundational findings (from recon — bind the design)

- `EffectDef` (`server/internal/game/effect_defs.go:74-98`) is small — **4 fields**:
  - `ID string json:"id"` — must match the containing dir name AND the client `assets/effects/<id>/` folder.
  - `SpritePath string json:"spritePath,omitempty"` — optional logical pointer; the client renders from its own `assets/effects/<id>/sprites.json`, so this is inert on the client. Kept editable for full coverage.
  - `Duration float64 json:"duration"` — seconds; drives the client animation timeline. `Duration < 0` panics at load. `0` falls back to the pipeline's 1.0s default.
  - `Anchor EffectAnchor json:"anchor,omitempty"` — `"center" | "feet" | "head"`; empty ⇒ center. A closed enum (`isValidEffectAnchor`, `effect_defs.go:50-57`).
- **Leaf node:** `EffectDef` references no other def. It is the *target* of `ProjectileDef` (`FollowEffect`/`ImpactEffect`) and `AbilityDef` (`EffectOnTarget`/`EffectAtPoint`/`BurnEffectAtPoint`), not a source. So the editor needs **no cross-def dropdowns** — only the `anchor` closed enum.
- Catalog layout: **per-id subfolder** `catalog/effects/<id>/<id>.json` (`loadEffectDefs`, `effect_defs.go:105-143`). Loader panics on: non-dir entry, empty id, id≠dir, `Duration<0`, invalid anchor, duplicate id.
- Readers: `getEffectDef(id) (EffectDef, bool)` (`:147`) and `ListEffectDefs() []EffectDef` (`:153`) — both **by value**, and both **embed-only today** (plain package var `effectDefsByID`, no mutex). Unlike `getAbilityDef` (already overlay-aware), these must be converted to consult the overlay.
- **Presentation-only:** `EffectDef` fields drive the client animation (`queueEffectLocked`), not the deterministic simulation. So there is **no determinism concern** (contrast with `ProjectileDef.Speed`, which does affect sim).
- Shared infra to reuse (all present): `editorValidationError`/`IsEditorValidationError` (`item_editor.go`), `registerEditorRoutes` mux (`editor_handlers.go`), `GET /catalog/effects` already wired (`registerAbilityCatalogRoutes`, `router.go:63`), the client editor triad (`game/abilities/abilityEditorForm.ts` / `abilityEditorApi.ts` / `components/AbilityEditorPanel.vue`), and the boot-time `LoadPersisted*IntoOverlay` calls in `cmd/api/main.go`.

## Decisions (from brainstorming)

1. **Full field coverage** (all 4 fields editable), consistent with the Unit-Types/Abilities editors.
2. **Anchor is a hardcoded closed-enum dropdown** in the panel (`center`/`feet`/`head`), not a `/catalog/*` endpoint — the set is fixed in Go (`isValidEffectAnchor`), unlike the extensible damage-type/category enums.
3. **Effect ART is out of scope.** The editor edits the def (`duration`/`anchor`/`spritePath`); new effects reference existing bundled art (`assets/effects/<id>/`) by id. Uploading new multi-frame effect sprite-sheets is a bigger art-pipeline piece (sub-project 4).
4. **Overlay-aware reader conversion** is required (the readers are embed-only today).

## Architecture

### §1 Server — writable overlay (`effect_persistence.go`, mirrors `ability_persistence.go`)
- `effectIDPattern = ^[a-z0-9_]+$` (id gate + path-traversal guard).
- `resolveEffectsDir()`: env `EFFECT_CATALOG_DIR`, else dev tree `internal/game/catalog/effects`.
- `runtimeEffects map[string]EffectDef` under `runtimeEffectsMu sync.RWMutex`.
- `SaveEffectDef(def *EffectDef)`: id-regex gate, `validateEffectDef`, `MkdirAll <dir>/<id>`, `WriteFile <dir>/<id>/<id>.json`, register into the overlay.
- `EffectIsEmbedded(id)`: `_, ok := effectDefsByID[id]`.
- `DeleteEffectOverride(id)`: id-regex gate, remove `<dir>/<id>/<id>.json` (+ best-effort empty `<id>` dir), remove overlay entry, return `existed`.
- `LoadPersistedEffectsIntoOverlay()` + `loadPersistedEffectsFromDir` + `parsePersistedEffectFile`: startup walk, best-effort, per-file skip-on-error (mirror the unit/ability loaders).
- `effect_defs.go`: make `getEffectDef` and `ListEffectDefs` **overlay-wins** (consult `runtimeEffects` before `effectDefsByID`); `ListEffectDefs` merges overlay over embed, stays sorted.

### §2 Server — `validateEffectDef` (shared load + save gate)
- Extract the loader's inline content checks into `validateEffectDef(def *EffectDef) error`: `Duration < 0` → error; `Anchor != "" && !isValidEffectAnchor(Anchor)` → error. Loader calls it (panicking on its error) and keeps its own id-empty / id≠dir / duplicate-id panics inline. `SaveEffectDef` calls it too (→ `editorValidationError` in the editor wrapper).

### §3 Server — editor wrapper + HTTP
- `effect_editor.go`: `EditorEffectSaveRequest{ Effect EffectDef json:"effect" }`; `SaveEditorEffect` (id-regex → `editorValidationError`, then `validateEffectDef` → `editorValidationError`, then `SaveEffectDef`); `DeleteEditorEffect(id)` → `DeleteEffectOverride`.
- `editor_handlers.go`: `POST /effects` + `DELETE /effects/{id}` — byte-for-byte the `/abilities` pair (201 `{id,status:"saved"}`; 400 `validation_failed`/`invalid_id`; 404 `not_found`; `deleted`/`reset` via `EffectIsEmbedded`).
- `cmd/api/main.go`: add `game.LoadPersistedEffectsIntoOverlay()` next to the other overlay loaders.
- `GET /catalog/effects` already exists (now returns overlay-merged defs).

### §4 Client — editor triad (`game/effects/*`, mirrors `game/abilities/*`)
- `effectEditorForm.ts`: `AuthoredEffectDef` (`id`, `spritePath?`, `duration?`, `anchor?` + index signature), `MODELED_KEYS`, `remainder` lossless bag, `createBlankForm`/`formFromDef`/`saveRequestFromForm` — pure, unit-tested.
- `effectEditorApi.ts`: `fetchAuthoredEffectDefs()` (GET `/catalog/effects` → `{effects}`), `saveEditorEffect()` (POST `/effects` with `{effect}`, 400 `validation_failed` → local `EditorValidationError`), `deleteEditorEffect()` (DELETE → `'deleted'|'reset'`).
- `components/EffectEditorPanel.vue`: left list of defs (from `/catalog/effects`) + "New"; form fields — `id` (text, disabled when editing an existing def), `spritePath` (text), `duration` (`v-model.number`), `anchor` (`<select>` over a hardcoded `['', 'center', 'feet', 'head']` with a blank = default-center option); Save → validation error inline; Delete/Reset (worded from the returned status). No literal `cursor:`.
- `views/EffectEditor.vue` + `/effect-editor` route (`meta: { hideDominionPanel: true }`), mirroring `/ability-editor`.

### §5 Wiring
- `WorldEditorToolbar.vue`: `effects` → `enabled: true` (update the toolbar test).
- `WorldEditorPanel.vue`: `EffectEditorPanel` as the "Effects" toolbar popup (`effectsPopupOpen`), mirroring all the `abilitiesPopupOpen` touch points (import / ref / `onToolbarSelect` case / `toolbarActiveId` / modal), and drop `effects` from the default-case "coming soon" comment.

## Error handling
- Invalid save (`Duration<0`, bad anchor, bad id) → `editorValidationError` → HTTP 400 `validation_failed` → panel inline error, form preserved.
- Delete of an embedded effect = reset-to-shipped-default; delete of an overlay-only effect = true delete.
- Overlay dir unwritable / bad file at startup → best-effort skip-on-error, logged, never fatal.
- A new effect with no matching bundled art renders nothing on the client (graceful — same as an ability referencing an unbundled effect); flagged as the known art-out-of-scope limitation.

## Testing
- **Go:** `SaveEffectDef` → overlay → `getEffectDef` round-trip; overlay-wins for both readers; `validateEffectDef` shared by loader + save (a def that fails load fails save — `Duration<0`, bad anchor); delete-resets-embedded vs deletes-overlay-only; `ListEffectDefs` merges overlay over embed. `POST /effects` 201/400, `DELETE /effects/{id}` deleted/reset/404.
- **Client (vitest):** pure form transforms (blank/from-def/save-request, remainder preservation), API 400 → `EditorValidationError`, panel mounts + lists, anchor select options, toolbar renders Effects enabled.
- **Build gates:** server `go build`/`vet`/`test`; client `npm run build` (`vue-tsc -b`) + `npm run test`.
- **Manual E2E:** create a new effect def (id, duration, anchor) → confirm it appears in the Abilities/Projectiles editor effect dropdowns; edit an embedded effect's duration → confirm; reset an embedded effect → reverts to shipped def.

## Out of scope
- Effect **art** (sprite-sheet/manifest) authoring or upload — sub-project 4.
- Any new `EffectDef` field beyond the 4 shipped (the struct has a `TODO(tuning)` for future fields; not in scope).
- Cross-def editing (effects reference nothing).

## Global constraints
- Server-authoritative sim unchanged; effect defs are presentation-only (no determinism concern). No `game`→`profile` write.
- Overlay writes are authoring-time; `effectIDPattern` is the path-traversal gate on every id entry point.
- No literal `cursor:` in new component CSS except `cursor: not-allowed` on forbidden states.
- Build gates as above; per-task commits with explicit `git add` (never `-A`).
- Mirror the ability-editor idioms exactly; do not invent a parallel mechanism.
