# Projectiles Editor — Design

**Date:** 2026-07-13
**Branch:** `reference-def-editors` (shared; sub-project 2 of 3 — Effects ✅ → **Projectiles** → Perks-wiring)
**Program context:** World editor, "reference-def editors" batch. Built after Effects so a newly-authored effect (via the now-overlay-aware `/catalog/effects`) can be referenced by a new projectile's follow/impact dropdowns in the same session.

## Problem & goal

`ProjectileDef` (the catalog of projectiles/beams — `fire_bolt`, `frost_bolt`, `holy_bolt`, …) is catalog-loaded, embed-only, with a `GET /catalog/projectiles` read route but **no writable overlay and no editor**. Abilities reference projectiles by id (dropdown), and projectiles reference effects by id, but authors cannot create or tune a projectile def. Add a Projectiles editor — create/edit/delete `ProjectileDef`s via a writable overlay — mirroring the shipped Abilities/Effects editors.

## Foundational findings (from recon + `projectile_defs.go:74-161`)

- `ProjectileDef` — **6 fields**:
  - `ID string json:"id"` — must match the containing dir name.
  - `Kind EmitterKind json:"kind,omitempty"` — `"projectile" | "beam"`; empty ⇒ projectile (back-compat). `EmitterKind` consts at `projectile_defs.go:28-40`.
  - `DurationMs int json:"durationMs,omitempty"` — beam flash lifetime (ms); ignored for projectiles; on a beam, `<=0` defaults to `defaultBeamDurationMs` (260).
  - `Speed float64 json:"speed"` — travel speed px/s; `<=0` defaults to `defaultProjectileSpeed`; ignored for beams.
  - `FollowEffect string json:"followEffect,omitempty"` — **→ EffectDef id** (plays on the projectile in flight).
  - `ImpactEffect string json:"impactEffect,omitempty"` — **→ EffectDef id** (plays on the target on landing).
- **Loader NORMALIZES + VALIDATES** (`loadProjectileDefs:143-154`): normalize empty `Kind`→projectile, beam `DurationMs<=0`→default, `Speed<=0`→default; validate `Kind ∈ {projectile, beam}` (panic otherwise). So `validateProjectileDef` must both normalize (in place) AND return an error for an invalid kind — mirroring `validateAbilityDef` (validate+normalize), NOT the pure-validate `validateEffectDef`.
- Catalog layout: per-id subfolder `catalog/projectiles/<id>/<id>.json`. Loader panics on non-dir / empty-id / id≠dir / invalid-kind / duplicate.
- Readers: `getProjectileDef(id) (ProjectileDef, bool)` and `ListProjectileDefs() []ProjectileDef` — both **by value**, both **embed-only today** (plain package var `projectileDefsByID`, no mutex). Must be converted overlay-aware (like `getAbilityDef`/`getEffectDef`).
- **Cross-def references:** `FollowEffect` and `ImpactEffect` → `EffectDef` ids. The editor's two effect dropdowns are fed by `GET /catalog/effects` (already exists AND is now overlay-aware after the Effects editor — so newly-authored effects appear). Referenced ids are resolved fail-safe at runtime (unknown effect = no effect).
- **Determinism:** `ProjectileDef.Speed` affects the **simulation** (projectile travel), so — like units/abilities — editing a projectile def during a live match would shift sim. This is the same authoring-time-only invariant already accepted for the other overlay editors (not a new concern).
- Shared infra to reuse (all present): `editorValidationError`/`IsEditorValidationError`, `registerEditorRoutes` mux (now with `/effects` from sub-project 1), `GET /catalog/projectiles` (`registerAbilityCatalogRoutes`), the client editor triad, and the client `fetchEffectIds` helper already exported from `game/abilities/abilityEditorApi.ts` (reused for the effect dropdowns).

## Decisions (from brainstorming)

1. **Full field coverage** (all 6 fields editable).
2. **`kind` is a hardcoded closed-enum dropdown** in the panel (`['projectile', 'beam']`) — not a `/catalog/*` endpoint (the set is fixed in Go).
3. **`followEffect`/`impactEffect` are validated dropdowns** fed by `/catalog/effects` (reusing `fetchEffectIds`), each with a blank "(none)" option.
4. **`validateProjectileDef` validates + normalizes in place** (Kind-empty→projectile, beam-duration default, speed default; invalid-kind → error), shared by loader + save.
5. **Overlay-aware reader conversion** required (readers are embed-only today).

## Architecture

### §1 Server — writable overlay (`projectile_persistence.go`, mirrors `effect_persistence.go`)
- `projectileIDPattern = ^[a-z0-9_]+$`; `resolveProjectilesDir()` (env `PROJECTILE_CATALOG_DIR`, else dev `internal/game/catalog/projectiles`); `runtimeProjectiles map[string]ProjectileDef` + `runtimeProjectilesMu sync.RWMutex`.
- `SaveProjectileDef` (id-regex, `validateProjectileDef`, `MkdirAll <dir>/<id>`, `WriteFile <id>.json`, register overlay), `ProjectileIsEmbedded`, `DeleteProjectileOverride` (id-regex, remove file + empty dir, remove overlay entry, return `existed`), `LoadPersistedProjectilesIntoOverlay` + walk + parse.
- `projectile_defs.go`: make `getProjectileDef`/`ListProjectileDefs` overlay-wins (consult `runtimeProjectiles` first); `ListProjectileDefs` merges overlay over embed, stays sorted.

### §2 Server — `validateProjectileDef` (shared load + save gate, validate + normalize)
- Extract from the loader into `validateProjectileDef(def *ProjectileDef) error`: normalize `Kind==""→EmitterKindProjectile`; return error if `Kind ∉ {EmitterKindProjectile, EmitterKindBeam}`; normalize `Kind==beam && DurationMs<=0 → defaultBeamDurationMs`; normalize `Speed<=0 → defaultProjectileSpeed`. Loader calls it (panicking on its error) and keeps its id-empty / id≠dir / duplicate-id panics inline. Does NOT check id.

### §3 Server — editor wrapper + HTTP
- `projectile_editor.go`: `EditorProjectileSaveRequest{ Projectile ProjectileDef json:"projectile" }`; `SaveEditorProjectile` (id-regex → `editorValidationError`, `validateProjectileDef` → `editorValidationError`, then `SaveProjectileDef`); `DeleteEditorProjectile(id)` → `DeleteProjectileOverride`.
- `editor_handlers.go`: `POST /projectiles` + `DELETE /projectiles/{id}` — byte-for-byte the `/effects` pair (201 saved / 400 validation_failed / invalid_id / 404 / deleted|reset via `ProjectileIsEmbedded`).
- `cmd/api/main.go`: `game.LoadPersistedProjectilesIntoOverlay()` next to the other overlay loaders.
- `GET /catalog/projectiles` already exists (now overlay-merged).

### §4 Client — editor triad (`game/projectiles/*`)
- `projectileEditorForm.ts`: `AuthoredProjectileDef` (`id`, `kind?`, `durationMs?`, `speed?`, `followEffect?`, `impactEffect?` + index sig), `MODELED_KEYS`, `remainder`, `createBlankForm`/`formFromDef`/`saveRequestFromForm` — pure, tested.
- `projectileEditorApi.ts`: `fetchAuthoredProjectileDefs()` (GET `/catalog/projectiles` → `{projectiles}`), `saveEditorProjectile()` (POST `/projectiles` `{projectile}`, 400 `validation_failed` → local `EditorValidationError`), `deleteEditorProjectile()` (DELETE → `'deleted'|'reset'`).
- `components/ProjectileEditorPanel.vue`: left list (from `/catalog/projectiles`) + "New"; form — `id` (text, disabled when editing), `kind` (`<select>` over `['projectile', 'beam']`), `durationMs` (`v-model.number`), `speed` (`v-model.number`), `followEffect` + `impactEffect` (`<select>` from `fetchEffectIds()` + blank "(none)"). Save/validation inline; Delete/Reset worded from status; `data-test="projectile-row"`; no literal `cursor:`.
- `views/ProjectileEditor.vue` + `/projectile-editor` route (`meta: { hideDominionPanel: true }`).

### §5 Wiring (current world-editor screen-switch pattern)
- `WorldEditorToolbar.vue`: `projectiles` → `enabled: true` (+ toolbar test).
- `WorldEditorPanel.vue` (the refactored full-screen-switch panel — NOT the old modal pattern): add `'projectiles'` to the `EditorScreen` union; add `case 'projectiles':` to the shared `case 'items': case 'unit-types': case 'abilities': case 'effects':` fall-through arm (`activeScreen.value = id`); add `<ProjectileEditorPanel v-else-if="activeScreen === 'projectiles'" />` to the `<section class="we-screen">` chain; import `ProjectileEditorPanel`; drop `projectiles` from the `default:` "coming soon" comment. `toolbarActiveId` already generalizes — leave it.

## Error handling
- Invalid save (bad kind, bad id) → `editorValidationError` → 400 `validation_failed` → panel inline error.
- Delete of embedded = reset-to-default; overlay-only = true delete.
- Unknown `followEffect`/`impactEffect` at runtime → no effect (fail-safe); the dropdowns prevent typos.
- Overlay dir unwritable / bad startup file → best-effort skip, logged, never fatal.

## Testing
- **Go:** `SaveProjectileDef` → overlay → `getProjectileDef` round-trip incl. normalization (a saved def with empty Kind comes back `projectile`, `Speed<=0` comes back defaulted); overlay-wins both readers; `validateProjectileDef` shared by loader + save (invalid kind rejected); disk round-trip (save → clear overlay → reload) + embed-revert (override a real embedded id → delete reverts); bad-id reject; `POST /projectiles` 201/400, `DELETE /projectiles/{id}` deleted/reset/404.
- **Client (vitest):** form transforms (blank/from-def/save-request, remainder), API 400 → `EditorValidationError`, panel mounts + lists + kind/effect selects.
- **Build gates:** server `go build`/`vet`/`test`; client `npm run build` + `npm run test`.
- **Manual E2E:** create a new projectile (id, kind, speed, followEffect, impactEffect) → confirm it appears in the Abilities editor's projectile dropdown; reference a newly-authored effect in the followEffect dropdown; edit an embedded projectile's speed → reset → reverts.

## Out of scope
- Projectile/beam **art** (sprite sheets `assets/projectiles/<id>.png`, `assets/beams/<id>/`) authoring/upload — sub-project 4.
- New `ProjectileDef` fields beyond the 6 shipped (the struct has a `TODO(tuning)` for future ones).
- Creating new **effects** from within the projectile editor (that's the Effects editor; the projectile editor only references effects by id).

## Global constraints
- Server-authoritative sim: `ProjectileDef.Speed` affects sim; the overlay is authoring-time-only (accepted invariant, same as units/abilities). No `game`→`profile` write.
- `projectileIDPattern` is the path-traversal gate on every id entry point.
- No literal `cursor:` in new component CSS except `cursor: not-allowed` on forbidden states.
- Build gates as above; per-task commits with explicit `git add` (never `-A`).
- Mirror the effect/ability editor idioms exactly; the WorldEditorPanel wiring uses the CURRENT screen-switch pattern (not the retired modal pattern the abilities plan described).
