# Ability Icon Picker + Upload — Design

**Date:** 2026-07-13
**Branch:** `ability-icon-editor` (off `main`)
**Program context:** Enhancement to the merged Abilities editor (world-editor
sub-project 2). Pulls the item-editor's icon gallery + runtime upload pattern
forward for abilities (the spec for the abilities editor had deferred runtime
art upload to sub-project 4; the user is bringing icon upload forward now).

## Problem & goal

The Abilities editor's `Icon` field is a plain text input, and `AbilityDef.Icon`
is dead placeholder data the client never reads — ability action-bar icons are
resolved **purely by ability id** (`ActionIcon.vue` →
`resolveAbilityIconImage(abilityId, projectileId)`). So an authored ability with
a new id (e.g. `test_heal`) has no icon unless a bundled `assets/abilities/<id>/`
folder happens to exist, and there is no way to reuse an existing icon or supply
a custom one.

Goal: give the ability icon field the same UX the item editor has —
1. a **gallery** to pick from the bundled ability icons, and
2. an **upload/import** to supply a custom PNG, served at runtime —
so authored abilities can have real, chosen icons.

## Foundational findings (from recon — bind the design)

**Item icon pattern (the template to mirror):**
- Server upload: `POST /items/{id}/image` (`editor_handlers.go`, inside the
  `/items/` handler) — reads **raw PNG bytes** (not multipart) with
  `http.MaxBytesReader(..., 256*1024+1)`, calls `game.SaveItemIcon(id, data)`,
  returns 201 `icon_saved` / 400 `icon_rejected`.
- Server serve: `GET /catalog/items/{id}/image` (`router.go`) —
  `game.ReadItemIcon(id)`, `Content-Type: image/png`, 404 if absent.
- Storage (`item_persistence.go`): `SaveItemIcon` validates id against
  `^[a-z0-9_]+$`, **requires the def to exist**, size-caps (`maxItemIconBytes =
  256*1024`), `png.DecodeConfig` validates a real PNG, writes
  `<ITEM_CATALOG_DIR>/_icons/<id>.png`, and **forces `def.IconKey = id`** +
  re-saves the def. `_icons` (`itemIconsSubdirName`) is skipped by every def
  walk. `DeleteItemOverride` removes `_icons/<id>.png`.
- `ItemDef.IconKey string` holds a **key** (a bundled icon-library key, or the
  item id after an upload) — not a path.
- Client: `itemAssets.ts` eagerly globs bundled `assets/icons/**` +
  `assets/items/**`; `getItemAssetImage(key)` resolves **bundled-first, else
  server** `${API_BASE}/catalog/items/{key}/image` with a `serverIconCache` +
  `serverIconFailed` eviction set (retries on reload); `getItemImageSourceUrl`
  gives an `<img src>`; `listIconGroups()`/`iconKeysByGroup` drive the gallery.
  `ItemEditorPanel.vue` icon section: preview `<img>`, "Choose from gallery"
  (grid over `galleryKeys` with group chips), file input →
  `onIconFileChosen` → `uploadItemIcon(id, file)` (raw Blob POST) → set
  `iconKey = id`. `pickGalleryIcon(key)` sets `iconKey = key`.

**Ability icon current state (what differs):**
- `abilityAssets.ts` globs `assets/abilities/**/*.png` keyed by the **directory
  name = ability id** (the PNG filename inside is arbitrary, e.g.
  `fireball/sprite.png`). `resolveAbilityIconImage(abilityId, projectileId?)` =
  bundled ability art by id → projectile-image fallback. **No server fallback.**
- Bundled ability icons are **horizontal multi-frame sprite strips**;
  `ActionIcon.vue` draws only the first frame (`drawActionSpriteFirstFrame`).
  Item icons are single-frame.
- `AbilityDef.Icon string \`json:"icon,omitempty"\`` is a placeholder path
  ("TODO/abilities/…"), copied into `AbilitySnapshot.Icon` on the wire, but the
  **client never reads it for rendering** — resolution is purely by ability id.
- **No ability icon upload/serve exists**: no `SaveAbilityIcon`/`ReadAbilityIcon`,
  the `/abilities/` handler is DELETE-only, no `/catalog/abilities/{id}/image`.
- Bundled ability icon ids today (7): `arcane_bolt`, `arcane_missiles`,
  `arcane_orb`, `chain_lightning`, `fireball`, `meteor`, `shatter`. Flat per-id
  folders, **no group dimension** (unlike items' `assets/icons/<group>/`).

## Decisions (from brainstorming)

1. **Full item-parity:** a gallery of all bundled ability icons **plus** a
   separate custom upload served at runtime (user choice).
2. **Repurpose `AbilityDef.Icon` as the resolution *key***, mirroring
   `ItemDef.IconKey`: empty ⇒ resolve by ability id (unchanged default); a
   bundled ability-icon folder name ⇒ that bundled icon; the ability's own id
   after an upload ⇒ the uploaded custom icon. (The field is currently dead
   placeholder data, so reusing it is clean; the JSON tag `icon` is kept.)
3. **The render path must honor the key** (the one architectural change): the
   client starts reading the ability's icon key (already on the wire as
   `AbilitySnapshot.Icon`) and resolving with it, falling back to ability id
   when empty. Required so a *gallery pick* actually changes what renders for a
   differently-named ability. Presentation-only; client stays a pure view.
4. **Gallery/preview render the first frame** of the sprite strip (reuse
   `drawActionSpriteFirstFrame` on a small canvas), not a plain `<img>`.
   Uploaded custom icons are treated as single-frame.
5. No group dimension for the ability gallery (abilities have no icon groups) —
   a flat grid of bundled ability-icon keys.

## Architecture

### §1 Server — `AbilityDef.Icon` as the icon key
- No struct change (field already exists). Its **meaning** changes from
  placeholder path → resolution key. `validateAbilityDef` needs no new rule
  (any string is a legal key; an unresolvable key falls back gracefully at
  render, same discipline as unknown effect/projectile ids).
- Existing catalog files carry placeholder `icon` paths
  (`"TODO/abilities/fireball.png"`). These will no longer resolve as bundled
  keys, so those abilities fall back to **resolve-by-id** (their real bundled
  art) — i.e. behavior is unchanged for shipped abilities. (Optional cleanup:
  blank the placeholder `icon` values in the catalog so the field reads true;
  not required for correctness. Deferred, noted.)

### §2 Server — icon persistence (mirror `item_persistence.go`)
In `ability_persistence.go`:
- `const maxAbilityIconBytes = 256 * 1024`, `const abilityIconsSubdirName = "_icons"`.
- `func SaveAbilityIcon(id string, data []byte) error`: gate id on
  `abilityIDPattern`; require `getAbilityDef(id)` exists (must save the ability
  before uploading); size cap; `png.DecodeConfig` validates a real PNG; resolve
  dir via `resolveAbilitiesDir()`; write `<dir>/_icons/<id>.png` (0644, mkdir
  0755); **force `def.Icon = id`** and re-save the def via `SaveAbilityDef`.
- `func ReadAbilityIcon(id string) ([]byte, bool)`: id-gate, read
  `<dir>/_icons/<id>.png`.
- `DeleteAbilityOverride` also removes `_icons/<id>.png`.
- The persisted-abilities walk (`loadPersistedAbilitiesFromDir`) must **skip the
  `_icons` subdir** (mirrors the item walk skipping `_icons`) so an icon PNG is
  never parsed as a def JSON.

### §3 Server — HTTP endpoints (mirror the item icon routes)
- Extend the `/abilities/` handler (`editor_handlers.go`, today DELETE-only): add
  a branch `strings.CutSuffix(id, "/image")` + `r.Method == POST` →
  `io.ReadAll(http.MaxBytesReader(w, r.Body, 256*1024+1))` →
  `game.SaveAbilityIcon(rest, data)` → 400 `icon_rejected` / 201 `icon_saved`
  `{"id","status"}`. The existing DELETE `/abilities/{id}` path is preserved.
- Add `GET /catalog/abilities/{id}/image` (`router.go`, mirror
  `/catalog/items/`): split `{id}/image`, `game.ReadAbilityIcon(id)`,
  `Content-Type: image/png`, 404 if absent. (This coexists with the existing
  `GET /catalog/abilities` list route — a distinct path prefix `/catalog/abilities/`.)

### §4 Client — resolution: gallery list + server fallback (`abilityAssets.ts`)
- `listAbilityIconKeys(): string[]` → sorted bundled ability-icon folder names
  (for the gallery).
- Add a bundled→server fallback keyed by the icon key, mirroring
  `itemAssets.getItemAssetImage`: `serverAbilityIconCache` + `serverAbilityIconFailed`
  eviction; resolve order for a given `(iconKey, abilityId, projectileId)`:
  1. bundled-by-`iconKey` (if `iconKey` set and bundled),
  2. server `${API_BASE}/catalog/abilities/{iconKey}/image` (if `iconKey` set),
  3. bundled-by-`abilityId` (current default when no key),
  4. server `${API_BASE}/catalog/abilities/{abilityId}/image`,
  5. projectile-image fallback.
- Keep the existing `resolveAbilityIconImage(abilityId, projectileId)` working
  (delegates with an empty key) so nothing else breaks; add the key-aware entry
  point `resolveAbilityIconImageKeyed(iconKey, abilityId, projectileId)` and a
  `getAbilityIconSourceUrl(key)` for `<img>`/preview use.

### §5 Client — render path honors the key
- `protocol.ts`: surface the ability snapshot's `icon` (already sent by the
  server as `AbilitySnapshot.Icon`) on the client `AbilitySnapshot` type.
- Thread that `icon` key into the `iconDef` `ActionIcon.vue` consumes for
  `kind: 'ability'`, and have `ActionIcon.vue` call the keyed resolver
  (`resolveAbilityIconImageKeyed(iconDef.iconKey, iconDef.type, iconDef.projectile)`),
  falling back to id when the key is empty. **No behavior change** for any
  ability whose `icon` is empty/placeholder (falls to resolve-by-id).
- Scope: the ability action/selection icon only. Commander abilities
  (`CommanderActionBar.vue`, hardcoded defs) are **out of scope**.

### §6 Client — editor panel + API
- `abilityEditorApi.ts`: `uploadAbilityIcon(id, file)` → raw-Blob POST to
  `/abilities/{id}/image` (mirror `uploadItemIcon`); `abilityIconUrl(id)`.
- `AbilityEditorPanel.vue`: replace the plain `icon` text input with the
  item-style icon section:
  - **Preview** rendering the first frame (small canvas via
    `drawActionSpriteFirstFrame`, sourced from `getAbilityIconSourceUrl(form.icon
    || form.id)`).
  - **"Choose from gallery"** → overlay grid over `listAbilityIconKeys()`; each
    cell renders the first frame; click → `form.icon = key`, close.
  - **File upload** `<input type="file" accept="image/png">` →
    `onIconFileChosen`: guard "save the ability first" (mirror the item guard),
    `await uploadAbilityIcon(form.id, file)`, set `form.icon = form.id`, evict
    the server-icon cache so the preview re-resolves.
  - No literal `cursor:` declarations.

## Error handling
- Upload before the ability def exists → server rejects (def-exists guard); the
  panel pre-guards with an inline message ("save the ability before uploading an
  icon"), mirroring items.
- Non-PNG / oversized upload → 400 `icon_rejected` → inline error.
- Unresolvable icon key at render (bad/edited key, deleted icon) → graceful
  fallback chain (bundled-by-id → projectile → nothing), never a crash — same
  discipline as unknown effect/projectile ids.
- Delete/reset an ability → its uploaded `_icons/<id>.png` is removed; the def
  reverts (embedded default or gone).

## Testing
- **Server (Go):** `SaveAbilityIcon` round-trip (write → `ReadAbilityIcon`
  returns the bytes; `def.Icon` forced to id; def re-saved); reject non-PNG and
  oversized; reject when the def doesn't exist; id-pattern/path-traversal reject;
  `DeleteAbilityOverride` removes the icon; `loadPersistedAbilitiesFromDir` skips
  `_icons` (an icon PNG never parsed as a def). Endpoints: `POST
  /abilities/{id}/image` 201/400, `GET /catalog/abilities/{id}/image` 200
  image/png / 404, DELETE `/abilities/{id}` still works.
- **Client (vitest):** `listAbilityIconKeys()` returns the bundled keys; keyed
  resolver prefers bundled-by-key then server then id then projectile;
  `uploadAbilityIcon` posts a raw Blob to the right URL; panel gallery-pick sets
  `form.icon`, upload sets `form.icon = form.id`. Build gate `npm run build`.
- **Manual E2E (hard gate):** in the editor, edit an ability → **Choose from
  gallery** → pick `fireball` → Play → confirm the ability's action-bar icon is
  fireball's. Then **Upload** a custom PNG → Play → confirm the custom icon
  renders. Delete/reset → confirm revert. Confirm existing shipped abilities'
  icons are unchanged (placeholder `icon` → resolve-by-id).

## Out of scope
- Group dimension / grouped gallery for ability icons (abilities have no groups).
- Commander ability icons (hardcoded defs, separate glob).
- Generalizing runtime art upload to other art categories (projectiles, effects,
  units) — that remains sub-project 4.
- Multi-frame authoring for uploaded custom icons (uploads are single-frame).
- MSI/packaged-build bundling of uploaded icons beyond the existing
  `ABILITY_CATALOG_DIR` overlay (authoring-time, same as items).

## Global constraints
- Server-authoritative sim unchanged; the icon is pure presentation, client stays
  a view. No `game`→`profile` write.
- Icon storage is the `ABILITY_CATALOG_DIR` overlay (`_icons` subdir),
  authoring-time; id regex `^[a-z0-9_]+$` is the path-traversal gate on every id
  entry point (save-icon, read-icon).
- No literal `cursor:` in new/changed component CSS except `cursor: not-allowed`
  on forbidden states.
- Build gates: server `go build`/`vet`/`test` (not gofmt); client `npm run build`
  (`vue-tsc -b`) + `npm run test` from `client/src/game-portal`.
- Follow the item-icon idioms exactly; do not invent a parallel mechanism.
