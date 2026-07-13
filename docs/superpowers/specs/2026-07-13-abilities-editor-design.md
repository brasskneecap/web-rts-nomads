# Abilities Editor — Design

**Date:** 2026-07-13
**Program context:** World editor, sub-project 2 (data editors), continued.
Follows the Item editor and Unit-Types editor. Adds a create/edit/delete editor
for `AbilityDef`s via the same proven writable-overlay + editor triad.

## Problem & goal

The world editor can already author unit *types* and items, and place units on a
map with per-instance rank/items/perks. But **abilities** — the spells and passives
units cast — are catalog-loaded from embedded JSON with **no overlay, no editor,
and no HTTP surface**. An author cannot create a new spell or tune an existing one
without editing Go-embedded JSON and rebuilding.

This sub-project adds an **Abilities editor**: full-coverage `AbilityDef`
authoring through a writable overlay, mirroring the Unit-Types editor 1:1, exposed
both as a world-editor toolbar category and a standalone `/ability-editor` route.

## Foundational findings (from recon — bind the design)

- **`AbilityDef` is already catalog-loaded from embedded JSON**, one dir per id
  (`server/internal/game/catalog/abilities/<id>/<id>.json`), loaded by
  `loadAbilityDefs()` (`ability_defs.go:502-576`) into `abilityDefsByID`. Readers:
  `getAbilityDef(id)` (`:580`), `ListAbilityDefs()` (`:586`). **No writable overlay,
  no `ability_persistence.go`, no `ability_editor.go`, no HTTP route exist yet** —
  unlike Units and Items which have the full triad. The struct is editor-ready.
- **`AbilityDef` is large (~50 fields)** grouped into conditional **mechanic
  families**, most abilities using only one:
  - *Always relevant:* identity (`ID`, `DisplayName`, `Type`, `Category`, `Tags`,
    `DamageType`), targeting (`CanTargetSelf/Allies/Enemies`, `TargetsPoint`,
    `TargetCount`), range (`CastRange`), cost/timing (`CastTime`, `ManaCost`,
    `Cooldown`), auto-cast trio (`SupportsAutoCast`, `AutoCastTargetSelector`,
    `DefaultAutoCast`), presentation/refs (`Icon`, `CasterAnimation`,
    `EffectOnTarget`, `EffectAtPoint`, `EffectScale`, `BurnEffectAtPoint`,
    `Projectile`).
  - *Basic:* `HealAmount`, `DamageAmount`, `DamagePerSecond`, `MinorDamage`,
    `SummonUnitType`, `SummonCount`.
  - *Channel-beam:* `ChannelType`, `TickIntervalSeconds`, `ManaCostPerTick`,
    `DamagePerTick`, `HealingMultiplier`, `AllyHealRadius`.
  - *Charge-fire missiles:* `ChargeRequired`, `ManaToChargeRatio`, `MissileCount`,
    `DamagePerMissile`, `Targeting`, `AllowDuplicateTargets`, `MissileDelayMs`.
  - *Meteor ground-hazard:* `ImpactDelaySeconds`, `BurnDurationSeconds`,
    `BurnDamagePerTick`, `BurnTickIntervalSeconds`, `BurnRadius`.
  - *Arch-mage spell:* `Radius`, `ProjectileSpeed`, `ProjectileScale`, `Duration`,
    `ChainCount`, `BounceRange`, `BounceDamageFalloff`, `PullStrength`,
    `SlowMultiplier`, `SlowDurationSeconds`.
- **All references are by string id, resolved fail-safe** (unknown id = no-op):
  `Projectile` → `ProjectileDef` (`projectile_defs.go`, embed-only),
  `EffectOnTarget`/`EffectAtPoint`/`BurnEffectAtPoint` → `EffectDef`
  (`effect_defs.go`, embed-only), `SummonUnitType` → a unit-type id,
  `AutoCastTargetSelector` → a key in the auto-cast selector registry
  (`autocast_selectors.go`), `Category`/`DamageType` → extensible enums
  (`ability_category.go`, damage-type registry). **None of these are exposed over
  HTTP yet** — the editor adds read endpoints so the pickers are validated.
- **`CastRange` is a custom type** (`ability_defs.go:50-87`) that unmarshals as a
  number **or** the string `"match_attack_range"` (sentinel `-1`), and has **no
  `MarshalJSON`** — a naive disk write flattens the sentinel to `-1` and loses the
  authored form. Must be handled (§5).
- **Commander abilities are a different, hardcoded system** (`commander_abilities.go`,
  `commanderAbilityRegistry` map — Smite/Blessing). Not catalog-loaded; editing them
  is a Go code change. Out of scope.
- **The overlay/editor/handler template is proven** by Units:
  `unit_persistence.go` (env `UNIT_CATALOG_DIR`, `runtimeUnits` overlay, id regex,
  `SaveUnitDef`/`DeleteUnitOverride`/`UnitIsEmbedded`/`LoadPersistedUnitsIntoOverlay`),
  `unit_editor.go` (`EditorUnitSaveRequest`, `SaveEditorUnit`, `DeleteEditorUnit`),
  `editor_handlers.go` (`POST /units`, `DELETE /units/{id}`),
  `router.go` (`GET /catalog/units`), and the client triad
  `game/units/unitEditorForm.ts` + `unitEditorApi.ts` + `UnitTypeEditorPanel.vue`.
  Abilities follow this 1:1.

## Decisions (from brainstorming)

1. **Full field coverage, family-gated.** Every field is editable, but the panel
   shows a mechanic-family selector that reveals only the chosen family's fields
   plus the always-relevant identity/targeting/range/cost/auto-cast/refs block.
2. **Validated dropdowns for all id-reference fields**, fed by new small
   `GET /catalog/*` read endpoints — no free-text ids, no typo-broken abilities.
3. **Commander abilities out of scope** (hardcoded Go, not catalog data).
4. **Icon stays a string reference** (defaulting to the ability id, resolved against
   already-bundled art). No runtime art upload — that is sub-project 4.
5. **Dual editor surface:** a world-editor toolbar popup (flip `abilities` enabled)
   AND a standalone `/ability-editor` route/view, mirroring the Unit-Types editor.

## Architecture

### §1 Server — writable overlay (mirrors `unit_persistence.go`)

- New `server/internal/game/ability_persistence.go`:
  - `abilityIDPattern = ^[a-z0-9_]+$` (guards id; blocks path traversal).
  - `resolveAbilitiesDir()`: env `ABILITY_CATALOG_DIR`, else dev tree
    `internal/game/catalog/abilities`.
  - `runtimeAbilities map[string]AbilityDef` under `runtimeAbilitiesMu sync.RWMutex`.
  - `SaveAbilityDef(def)`: id-regex gate, `validateAbilityDef`, `MkdirAll`,
    `WriteFile` to `<dir>/<id>/<id>.json`, register into the overlay.
  - `DeleteAbilityOverride(id)`: id-regex gate, remove file + overlay entry,
    return `existed`.
  - `AbilityIsEmbedded(id)`: true if the id ships in the embed (→ delete = "reset").
  - `LoadPersistedAbilitiesIntoOverlay()`: startup walk of the dir, best-effort,
    per-file skip-on-error (mirrors `loadPersistedUnitsFromDir`).
- `ability_defs.go`: teach `getAbilityDef` and `ListAbilityDefs` to consult
  `runtimeAbilities` (overlay-wins) before `abilityDefsByID` (embed) — the same
  one-line pattern as `getUnitDef`/`getItemDef`.
- **Refactor:** extract the load-time validation currently inline in
  `loadAbilityDefs` into a reusable `validateAbilityDef(def *AbilityDef) error`
  (id non-empty, valid `damageType`, valid `category`, burn requires impact-delay +
  tick-interval, `TargetCount<1→1`, `SummonCount<1→1`, channel `HealingMultiplier
  0→1.0`, charge-fire `ManaToChargeRatio 0→1.0`, etc.). `loadAbilityDefs` and
  `SaveAbilityDef` both call it, so save and load enforce one gate (mirrors the
  `validateUnitDef` extraction). Load-time id==dir check stays in the loader.
- `LoadPersistedAbilitiesIntoOverlay()` is called at startup alongside the existing
  `LoadPersisted{Units,Items,...}IntoOverlay` calls.

### §2 Server — editor thin-wrapper + HTTP

- New `server/internal/game/ability_editor.go` (mirrors `unit_editor.go`):
  - `EditorAbilitySaveRequest{ Ability AbilityDef }`.
  - `SaveEditorAbility(req)`: calls `SaveAbilityDef`, wrapping a validation error as
    the in-package `editorValidationError` (→ HTTP 400 via `IsEditorValidationError`).
  - `DeleteEditorAbility(id)`: calls `DeleteAbilityOverride`.
- `server/internal/http/editor_handlers.go` — add, byte-for-byte the `/units` shape:
  - `POST /abilities`: decode `game.EditorAbilitySaveRequest` → `SaveEditorAbility`
    → 400 on `IsEditorValidationError`, 500 otherwise, else `201 {"id","status":"saved"}`.
  - `DELETE /abilities/{id}`: id non-empty and contains no `/` → `DeleteEditorAbility`
    → `{"status":"deleted"|"reset"}` (reset when `AbilityIsEmbedded`).
- `server/internal/http/router.go` — add read routes (mirror `GET /catalog/units`):
  - `GET /catalog/abilities` → `ListAbilityDefs()` (also the editor's browse/edit list).
  - `GET /catalog/projectiles` → projectile ids.
  - `GET /catalog/effects` → effect ids.
  - `GET /catalog/autocast-selectors` → selector-registry keys.
  - `GET /catalog/ability-categories` → `AbilityCategories()`.
  - `GET /catalog/damage-types` → damage-type registry keys.
  Each is a trivial list handler; the projectile/effect/selector/category/damage
  registries already exist in-package and only need a `List*` accessor if absent.

### §3 Client — editor triad (mirrors `game/units/*`)

- New `client/src/game-portal/src/game/abilities/abilityEditorForm.ts`:
  - `AuthoredAbilityDef` interface — the modeled superset of `AbilityDef`, with
    `CastRange` typed `number | 'match_attack_range'` (§5).
  - `MODELED_KEYS` allowlist + opaque `remainder` bag for lossless round-trip of any
    unmodeled/future keys (same pattern as `unitEditorForm.ts`).
  - `createBlankForm()`, `formFromDef(def)`, `saveRequestFromForm(form)` — pure, no
    DOM/HTTP, unit-tested.
- New `client/src/game-portal/src/game/abilities/abilityEditorApi.ts`:
  - `fetchAuthoredAbilityDefs()` (GET `/catalog/abilities`).
  - `fetchProjectileIds()`, `fetchEffectIds()`, `fetchAutoCastSelectors()`,
    `fetchAbilityCategories()`, `fetchDamageTypes()` — feed the dropdowns.
  - `saveEditorAbility(form)` (POST `/abilities` with `{ability}`, 400
    `validation_failed` → typed `EditorValidationError`).
  - `deleteEditorAbility(id)` (DELETE `/abilities/{id}` → `'deleted'|'reset'`).
- New `client/src/game-portal/src/components/AbilityEditorPanel.vue`:
  - **Always-shown block:** identity (id/displayName/type/category/tags/damageType),
    targeting, range, cost/timing, the auto-cast trio (the selector dropdown +
    `DefaultAutoCast` are gated on `SupportsAutoCast`), icon, and the effect/
    projectile/summon reference dropdowns.
  - **Mechanic-family selector** → reveals exactly one family's fields:
    Basic / Channel-beam / Charge-fire / Meteor / Arch-mage.
  - Left list of existing defs (from `/catalog/abilities`) to edit/delete/reset;
    a "New" action creates a blank form. Embedded defs show "Reset to default"
    (delete → reset); overlay-only defs show "Delete".
  - No literal `cursor:` declarations (global cursor rules apply);
    `cursor: not-allowed` only on forbidden-action states.

### §4 Wiring

- `components/world-editor/WorldEditorToolbar.vue`: `{ id: 'abilities', ...,
  enabled: true }`; update `WorldEditorToolbar` test to expect Abilities enabled.
- `components/world-editor/WorldEditorPanel.vue`: `AbilityEditorPanel` embeds as the
  "Abilities" toolbar popup (same mechanism as the Items / Unit-Types popups).
- New `client/src/game-portal/src/views/AbilityEditor.vue` + route
  `/ability-editor` (`meta: { hideDominionPanel: true }`) — mirrors
  `views/UnitTypeEditor.vue` and the `/unit-type-editor` route.

### §5 `CastRange` round-trip (the one real hazard)

`CastRange` reads number-or-`"match_attack_range"` but has no `MarshalJSON`, so a
naive disk write of an authored `AbilityDef` flattens `"match_attack_range"` to
`-1`. Fix on the server side: add a `MarshalJSON` to `CastRange` that emits the
string `"match_attack_range"` for the sentinel and the number otherwise, so
`SaveAbilityDef`'s `json.MarshalIndent` round-trips faithfully (symmetric with its
existing `UnmarshalJSON`). The client `AuthoredAbilityDef` types the field as
`number | 'match_attack_range'` and the panel offers a "match attack range" toggle
that swaps between the numeric input and the sentinel. This is the abilities
analogue of the item editor's disk-shadow concern; adding `MarshalJSON` is the
minimal, symmetric fix (no separate disk-shadow struct needed unless another field
turns out to have the same asymmetry).

## Error handling

- Save with an invalid field (bad category/damageType, malformed burn config,
  bad id) → server `validateAbilityDef` returns an `editorValidationError` → HTTP
  400 `validation_failed` → panel shows the message inline, form preserved.
- Unknown reference id in a saved ability never crashes: runtime resolution is
  already fail-safe (unknown projectile/effect/summon/selector = no-op). The
  dropdowns prevent typos at authoring time regardless.
- Delete of an embedded ability = "reset to shipped default" (overlay entry + file
  removed, embed re-surfaces); delete of an overlay-only ability = true delete.
- Overlay dir unwritable / load error at startup → best-effort skip-on-error, logged,
  never fatal (mirrors the unit/item overlay loaders).

## Testing

- **Go:** `AbilityDef` save → overlay → `getAbilityDef` round-trip, **including
  `CastRange` sentinel preservation** (`"match_attack_range"` survives
  marshal→unmarshal); `validateAbilityDef` shared by save and load (a def that
  fails load also fails save); delete-resets-embedded vs deletes-overlay-only;
  `ListAbilityDefs` merges overlay over embed. New `/catalog/*` handlers return the
  expected id sets.
- **Client (vitest):** pure form transforms (`createBlankForm`/`formFromDef`/
  `saveRequestFromForm`, `remainder` preservation, family-gating reveals the right
  fields), `CastRange` toggle ↔ value mapping, API 400 → `EditorValidationError`,
  toolbar renders Abilities enabled.
- **Build gates:** server `go vet` / `go build` / `go test`; client `npm run build`
  (`vue-tsc -b`, enforces `noUnusedLocals`) + `npm run test`.
- **Manual E2E:** author a new heal-family ability in the editor → assign it to a
  placed unit in the world editor → Play → confirm it casts and auto-casts per the
  trio settings. Then edit an existing embedded ability, save, Play to confirm the
  change takes, then "Reset to default" and confirm it reverts to the shipped def.

## Out of scope

- **Commander abilities** (`commander_abilities.go`) — hardcoded Go registry, not
  catalog data; editing them is a code change.
- **Creating new projectiles / effects / auto-cast selectors** — this editor
  references existing ids only. Each of those would get its own overlay editor later
  (same template), a separate sub-project.
- **Runtime icon/art upload** for new ability art — `Icon` here is a string ref to
  already-bundled art; the runtime custom-art pipeline is sub-project 4.
- Undo/redo and multi-def batch operations beyond single create/edit/delete/reset.

## Global constraints

- Server-authoritative sim unchanged; the editor only writes catalog data an
  author opts into. No client-side gameplay logic.
- Overlay writes are dev/desktop authoring-time; the id regex is the path-traversal
  gate, server validation is the correctness gate (no auth, matching the existing
  item/unit editor handlers).
- No literal `cursor:` declarations in new component CSS except `cursor: not-allowed`
  on forbidden-action states.
- Follow the existing item/unit editor idioms; do not introduce a new abstraction
  where the proven triad fits.
