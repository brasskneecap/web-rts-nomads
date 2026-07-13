# Unit-Types Editor v2 — Design Spec

**Successor to `2026-07-12-unit-types-editor-design.md`.** v1 shipped a data editor for
the base `UnitDef` and explicitly deferred three things as non-goals: **art authoring**,
**promotion-path editing**, and **faction management**. v2 folds all three back in, so
that everything involved in creating a unit — identity, stats, cost, art, animations,
attack origins, promotion paths, and per-rank perk pools — is authorable from one screen.

## 0. The north star

**A unit — with its paths, art, stats, costs, origins, and progression — can be taken from
nothing to shipped entirely inside the editor, with zero Go or TypeScript changes required to
get it across the finish line.**

Every decision below is measured against that. Where v2 falls short of it, the spec says so
plainly rather than hiding it (see §7.3 and §12) — a gap you can see is a gap you can close;
a gap you discover at save time is a bug.

**v2 reaches the north star for:** identity, faction, archetype, stats, cost, gating, art,
animations, attack origins, promotion paths, and rank stat curves.

**v2 does not reach it for perks** — a perk's *behavior* still requires a Go case arm. That is
a known, accepted, temporary shortfall (§7.3). The perk system needs a data-driven rethink
before it can satisfy the north star, and that rethink is deliberately **not** in this version.

---

## 0.1 Decisions taken (these are settled; the rest of the spec assumes them)

| # | Decision | Rationale |
|---|---|---|
| D1 | **Unify the editor, not the engine.** A promotion path stays a sparse overlay + rank multiplier table in the engine. The editor *presents* it as a first-class authorable entity. | Making paths literal `UnitDef`s would rewrite `progression.go`, spawn, and the wire snapshots — large refactor of live combat code, and it buys nothing the editor can't deliver anyway. |
| D2 | **Pack sprites in the browser; the server stores bytes.** The panel ingests a PixelLab export folder, packs the sheets client-side with canvas, previews instantly from memory, and POSTs packed PNGs + `sprites.json` to the server. | A browser panel cannot write to `client/src/assets` or run npm. Shelling out to Node from Go requires Node on the machine and breaks in the packaged Tauri build. |
| D3 | **Per-facing attack origins**, authored by dragging a crosshair on the animation preview. Falls back to a single default origin, then to today's derived geometry. | A bow occupies a different screen position facing north vs. east; a single origin is wrong for some facings by construction. |
| D4 | **File-backed faction registry** (`faction.json` per faction dir + `GET/POST/DELETE` routes). | Lets a faction exist before it has units, and gives real display names instead of raw ids. |
| D5 | **Perks: pool authoring only in v2.** The editor assigns existing perks to a path's bronze/silver/gold pools and tunes their config. Creating genuinely new perk *behavior* is out of scope and deferred to a data-driven perk redesign. | The engine grants perks by id and branches on that id in Go. Making perks data-driven is a system redesign, not an editor feature, and bolting it onto v2 would balloon the change. |
| D6 | **A save that would produce a broken promotion setup is rejected inline, never written.** No partial writes, no "revert on next boot", no auto-repair. | The server *panics at startup* on a dangling `pathChances` key. An editor that can write one is an editor that can brick the game. §9.1. |

---

## 1. What the code actually does today (the three collisions)

This section is load-bearing. Three of the requirements collide with current behavior, and
the design is shaped by those collisions.

### 1.1 The art pipeline is build-time, not runtime

`npm run pack:sprites` → `client/src/game-portal/scripts/pack-unit-sprites.mjs` walks
`src/assets/units/<faction>/<unit>/` (and `.../paths/<path>/`), reads the author-supplied
`metadata.json` (a PixelLab export), and emits:

- `packed/rotations.png` — 1 column × N rows (one row per facing)
- `packed/<anim>.png` — columns = frames, rows = facings
- `sprites.json` — the generated manifest (`key`, `size`, `rotations`, `animations`, `packedAt`)

The renderer picks these up through **eager Vite globs** at
[unitSprites.ts:129-144](../../client/src/game-portal/src/game/rendering/unitSprites.ts#L129-L144):

```ts
const manifestGlob = import.meta.glob<SpriteManifest>('../../assets/units/**/sprites.json', { eager: true, import: 'default' })
const stripGlob    = import.meta.glob<string>('../../assets/units/**/packed/*.png', { eager: true, query: '?url', import: 'default' })
```

**Consequence:** art dropped on disk is invisible until Vite reloads or rebuilds. A browser
panel can neither write the source tree nor trigger a repack. Previewing *newly ingested*
art — and rendering it in a playtest — therefore requires a **runtime sprite overlay**
that shadows the globbed built-ins, mirroring the server's existing overlay-over-embed
model for `UnitDef`. This overlay does not exist and is the architectural centerpiece of v2.

### 1.2 `attackVisual.originX/originY` is dead code for every unit that has art

[CanvasRenderer.ts:3608-3624](../../client/src/game-portal/src/game/rendering/CanvasRenderer.ts#L3608-L3624)
`getProjectileOriginLift()` prefers `spriteBodyCenterLift()` — which derives the origin
*geometrically* from `bounds` + sprite height + the 15% transparent padding +
`TARGET_BODY_CENTER_FRACTION = 0.3` — and only falls back to the authored
`attackVisual.originX/originY` when the unit has **no sprite set**. Archer authors
`"originX": 24, "originY": 5` in
[archer.json](../../server/internal/game/catalog/units/human/archer/archer.json) and the
renderer ignores it.

The three origin-ish concepts in the renderer today:

| Concept | Where | Authored? |
|---|---|---|
| Projectile spawn lift | `getProjectileOriginLift` (frac `0.3`) | No — derived from bounds |
| Beam/channel anchor | `beamOrigin` + `BEAM_BODY_ANCHOR_FRACTION = 0.5` | Nominally via `sprites.json`… |
| Projectile *impact* lift on the target | `getProjectileTargetLift` | No — derived |

…and **`beamOrigin` is authored in zero `sprites.json` files** (verified: it exists in the TS
types at [unitSprites.ts:84](../../client/src/game-portal/src/game/rendering/unitSprites.ts#L84)
and is read at :292, but no manifest declares it, so it always resolves to `{0,0}`). It is
also *silently destroyed by a re-pack*: `readPreservedOverrides` is wired into `packObject`
but **not** `packUnit`.

**Consequence:** "edit where projectiles/spells come from" is a real engine change, not a
form field. The good news is that because `beamOrigin` is a dead no-op, folding beam origins
into the new authored block costs nothing and changes nothing visually.

**Server note:** projectile *simulation* origin is `attacker.X / attacker.Y` (the feet
anchor) — see [projectile.go:245-262](../../server/internal/game/projectile.go#L245-L262).
The server never reads any art blob. Everything in §5 is a **client-side visual lift** on
top of the server origin. No simulation behavior changes.

### 1.3 A path is not a unit

Paths are loaded by `path_defs.go` into the unexported `pathCatalogFile`: a sparse overlay
(`visionRange`, `projectile`, `damageType`, `attackType`, `projectileScale`, `abilities`,
`channelLoop`, `bounds`) plus `ranks: {bronze|silver|gold}` of stat multipliers. The unit
keeps its `unitType` forever; `ProgressionPath` is a separate field on `Unit`.

Also: **paths and perks have no writable overlay and no write endpoints at all.** They are
embed-only, and `unit_persistence.go` deliberately `SkipDir`s `paths/`
([unit_persistence.go:18](../../server/internal/game/unit_persistence.go#L18): *"a separate
catalog dimension owned by path_defs.go, not by this editor"*). v2 fills that reservation.

---

## 2. Scope

### In scope

1. **Faction filter + faction creation** (D4).
2. **Archetype dropdown** sourced from the real closed set, plus required/defaulted base stats.
3. **Animation & rotation viewer** for the selected unit or path.
4. **Art ingestion**: drop a PixelLab export folder → packed in-browser → previewed → persisted.
5. **Per-facing attack-origin editor** with live preview (D3).
6. **Path entities**: create a path like a unit, attach it to a base unit, author its rank
   stat grid and its bronze/silver/gold perk pools (D1).

### Out of scope (explicitly)

- **Perk *behavior* authoring.** A perk with new behavior requires a Go `switch perkID` case
  arm across `perks*.go`. The editor can author perk **config** (tuning existing effect
  hooks) and perk **pool membership**. It cannot invent a new effect. This is a hard
  engine limitation and must be surfaced in the UI, not hidden.
- Ability / projectile / effect *definition* editors (separate toolbar categories).
- Sprite *generation*. The editor ingests an export; it does not draw art.

---

## 3. Pillar A — Factions, archetypes, and stat floors

The smallest, lowest-risk pillar. No engine risk; ship it first.

### 3.1 Faction registry (new)

Today faction is *only* a directory name — no allowlist, no registry — and the editor's
faction `<select>` is a hardcoded array at
[UnitTypeEditorPanel.vue:227](../../client/src/game-portal/src/components/UnitTypeEditorPanel.vue#L227),
so a new server faction dir never appears in the editor.

**New:** `catalog/units/<faction>/faction.json`

```json
{ "id": "witherborne", "displayName": "Witherborne", "order": 4 }
```

- New `server/internal/game/faction_defs.go`: `FactionDef`, `//go:embed`-backed loader,
  `ListFactions()`. A faction directory **without** a `faction.json` is still valid — it
  synthesizes `{id: <dirname>, displayName: titlecase(<dirname>)}`, so the four existing
  factions need no new files and nothing breaks.
- Writable overlay in `faction_persistence.go`, mirroring `unit_persistence.go` (same
  `UNIT_CATALOG_DIR`, same `unitIDPattern = ^[a-z0-9_]+$` guard, overlay registered only
  after a successful disk write).
- Routes: `GET /catalog/factions`, `POST /factions`, `DELETE /factions/{id}`.
- **Delete guard:** refuse to delete a faction that still owns units; return a validation
  error naming them.

### 3.2 Faction filter (client)

A filter bar across the top of the unit list: `All | <faction…>`, sourced from
`GET /catalog/factions` merged with the distinct factions actually present on the fetched
unit defs (so a faction dir with units but no `faction.json` still shows). Replace the
hardcoded `FACTIONS` array. `+ New Faction` opens a small inline form (id + display name).

### 3.3 Archetype dropdown

Archetype is free text today and **validated nowhere**. `resolveUnitArchetype`
([state_spawn.go:176-181](../../server/internal/game/state_spawn.go#L176-L181)) defaults it
to the unit type string, and `resolveCombatProfile` uses it as a key into `combatProfiles` —
a bogus archetype silently degrades to the `soldier` profile.

The real closed set is the `combatProfiles` keys
([combat_ai_profiles.go:3](../../server/internal/game/combat_ai_profiles.go#L3)): `soldier,
archer, mage, cavalry, catapult, raider, bruiser, skirmisher, enemy_archer, enemy_siege,
support, caster, flyer_skirmisher, boss`.

- New `ListArchetypes()` + `GET /catalog/archetypes` returning those keys.
- Editor renders a `<select>` **with a "custom…" escape hatch** (free text), because
  archetype is unvalidated today and I will not retroactively break an authored value.
- `validateUnitDef` gains a **warning**, not an error, for an archetype outside the set.
  Surfaced in the panel as an inline caution ("falls back to the soldier combat profile").

> **Naming hazard to document, not fix:** "archetype" is overloaded. `UnitDef.Archetype` is
> a combat-profile key; the `spell-pools.json` "archetype" key is a *promotion-path id*.
> The editor must label them distinctly (`Combat Archetype` vs `Spell Pool`) so the author
> is never asked to reconcile two different meanings of one word.

### 3.4 Required fields and sane defaults

Today a blank-created unit is all-zeros: 0 HP, 0 move speed — it spawns and does nothing.

- **Server:** extend `validateUnitDef` with floors. Verified against the catalog: no unit has
  a zero `hp`, `moveSpeed`, or `attackSpeed`, so requiring `hp > 0` and `moveSpeed > 0` is
  safe. Attack fields are conditional: **if `damage > 0` then `attackRange > 0` and
  `attackSpeed > 0`** — so a unit authored with no attack at all (`damage` is `omitempty`,
  absent == 0) stays legal.

  > **Corrected during implementation.** An earlier draft of this line justified the
  > conditional with "non-combat units like `worker` omit their attack fields." That is
  > **false**: `worker.json` carries `nonCombat: true` **and** `damage: 3, attackRange: 60,
  > attackSpeed: 1`. In this codebase `nonCombat` means *"not counted as an army unit"*, not
  > *"cannot attack"* — and in fact **every one of the 14 shipped units has `damage > 0`**, so
  > an unconditional attack floor would pass the whole catalog identically. The conditional is
  > still the right rule, but it is a forward-looking allowance for editor-authored defs, not a
  > concession to existing content. Worth knowing before anyone writes gameplay logic assuming
  > `nonCombat` implies harmless.
- **Guard task (mandatory):** a test that runs the new validator over **every def in the
  embedded catalog** and asserts all pass. Any rule that fails an existing unit is too
  strict and must be relaxed — the catalog is the authority, not my guess.
- **Client:** `createBlankForm()` seeds its stat block by **cloning a template def fetched
  from the catalog** (`soldier`), not from hardcoded literals — so the defaults track
  balance changes instead of silently rotting. Fall back to a minimal constant block only
  if the template is unavailable.

---

## 4. Pillar B — The runtime sprite overlay (infrastructure)

**This is the piece everything else depends on.** Without it, ingested art cannot be
previewed or playtested without a rebuild.

### 4.1 Server: art storage + serving

- New env `UNIT_ASSETS_DIR`, resolved like `resolveUnitsDir()`: env override, else the dev
  source tree `client/src/game-portal/src/assets/units`. Add it to the Tauri supervisor
  alongside the existing catalog dirs (packaged build → app-data dir).
- `POST /units/{type}/art` — body carries the packed artifacts produced by the browser
  packer: `sprites.json`, each `packed/*.png` (base64 or multipart), the original
  `metadata.json`, optional `portrait.png`, and optionally the raw frames. Writes them under
  `<UNIT_ASSETS_DIR>/<faction>/<unit>/` (or `.../paths/<path>/` for a path).
- `GET /catalog/unit-art` — enumerates every art folder present in the writable dir:
  `[{ key, faction, unit, path?, manifest }]`.
- `GET /assets/units/...` — read-only static serve of `UNIT_ASSETS_DIR`, so the client can
  fetch `packed/*.png` at runtime.
- Path guards: every segment must match `unitIDPattern`; reject traversal at both handler
  and persistence layers. Enforce a per-file and per-request size cap (mirror the existing
  256KB item-icon cap, scaled — sheets are larger).

### 4.2 Client: overlay-aware `unitSprites.ts`

- Add a `runtimeSpriteSets: Map<string, UnitSpriteSet>` consulted **before** the globbed
  built-ins in `getUnitSpriteSet()`. Overlay wins, exactly like `getUnitDef`.
- `loadRuntimeSpriteSets()` — fetch `GET /catalog/unit-art`, build a `UnitSpriteSet` per
  entry from the HTTP-served sheets, register it. Called at app boot **and** after any
  editor art save (so the playtest immediately renders the new art).
- ~~**Refactor rotations to row-offset sourcing.**~~ **CUT — this was based on a false premise.**

  > The original claim: `loadPackedRotations()` slices the rotation strip via an offscreen
  > canvas + `toDataURL`, which would taint the canvas for an HTTP-served sheet and throw
  > `SecurityError`.
  >
  > **That does not happen here.** `vite.config.ts` proxies API routes to the Go server in dev,
  > and in the packaged build the Go binary serves the SPA. Art served from `/assets/...` is
  > **same-origin in both cases**, so the canvas is never tainted and the existing slicing works
  > unchanged for runtime art.
  >
  > Cutting it also avoids a bug the refactor would have introduced: `getUnitPortraitUrl`
  > returns `rotations.south.src` **straight into an `<img>` tag**. A row-offset source's `.src`
  > is the whole 8-row vertical strip, so every portrait without a dedicated `portrait.png`
  > would have rendered as a column of all eight facings. `loadPackedRotations` is reused as-is.

- **Add `/assets` to the Vite dev proxy.** Any server route the SPA calls that is missing from
  `vite.config.ts`'s `proxy` block silently 404s in `npm run dev`. This is not hypothetical:
  `/units` and `/factions` shipped without proxy entries, so **the unit editor's Save and Delete
  were dead in dev** (`GET /catalog/units` worked, because `/catalog` *is* proxied — which is
  exactly why it went unnoticed). Fixed during Phase 2 planning. Adding a write endpoint on the
  server means adding its prefix here.

### 4.3 Vite watch exclusion

The server writes into `client/src/.../assets/units/` in dev. Vite will notice and HMR-reload
— potentially blowing away unsaved editor form state mid-edit. Add `server.watch.ignored` for
`src/assets/units/**/packed/**` and `**/sprites.json` in `vite.config.ts`. The runtime overlay
means we no longer *need* the reload.

**Tradeoff, stated plainly:** after this, a plain CLI `npm run pack:sprites` in dev also stops
hot-reloading; you restart the dev server to pick up committed art changes. That is an
acceptable trade for not losing editor state on every save, and it matches how the packed
output is actually consumed (build-time).

---

## 5. Pillar C — Animation viewer + browser packer

### 5.1 Animation & rotation viewer

A preview pane in the panel, driven entirely by existing primitives — `getUnitSpriteSet(path,
unitType)` and `getUnitFrame(set, anim, dir, frameIndex)` — on a `requestAnimationFrame` loop.

- **Rotation strip:** all 8 facings side by side, from the rotations sheet.
- **Animation player:** animation `<select>` (whatever the manifest actually contains —
  `walking`, `attacking`, `casting`, `chopping`, `repairing`, `carrying_gold`), a facing
  selector (the 8-way wheel), play/pause, a frame scrubber, and an FPS readout.
- **Honesty in the UI:** the panel must show which animations are *missing* and what they
  fall back to (`ANIMATION_FALLBACK`: `carrying_gold → walking`, `casting → attacking`) —
  no unit currently ships a `casting.png` despite a stale comment claiming Acolyte does.
- Draw at `UNIT_SPRITE_SCALE` with `imageSmoothingEnabled = false`, and overlay the `bounds`
  box + the 15% top/bottom padding guides, since those drive every anchor in the game.

### 5.2 Browser packer

New `game/units/spritePacking.ts`, reproducing `pack-unit-sprites.mjs`'s layout exactly:

- Accepts either export shape: flat `{character, frames}` or `{states: [{character, folder, frames}]}` (first state only — multi-state is unsupported upstream too).
- `size` = `character.size.{width,height}`, default 64×64.
- Animation slug = `name.split('-')[0].toLowerCase()` (`"Walking-1656a518"` → `walking`).
- Row order = `['north','south','east','west','north-east','south-east','south-west','north-west']`, filtered to the facings actually present.
- `rotations.png` = 1 column × N rows. `<anim>.png` = frames (columns) × facings (rows).
- Emits the identical `sprites.json` shape.

**Structure it as a pure layout core + a rasterizer adapter** (`canvas` in the browser,
`pngjs` in Node), so the layout math is testable without a DOM.

**Drift control — mandatory:** a golden-fixture conformance test. Check in a tiny synthetic
export (2 facings × 2 frames of 4×4 PNGs, each frame a distinct solid color so a wrong
row/column placement is detectable) **plus its `.mjs`-generated output** as a committed golden.
The test asserts:
- the TS core's `sprites.json` manifest is deep-equal to the golden (ignoring `packedAt`);
- each sheet, decoded to RGBA, is byte-identical to the golden sheet's RGBA.

> **Corrected during Phase 3 planning — two points the first draft got wrong:**
> 1. **The comparison is on DECODED RGBA + the manifest JSON, not the encoded PNG file.** The
>    CLI encodes with `pngjs`; the browser encodes with `canvas.toBlob`. They produce different
>    PNG *container* bytes for identical pixels, so encoded-byte equality is neither achievable
>    nor required — editor-ingested art and CLI-committed art need only decode to the same
>    pixels, never be the same file.
> 2. **The test does not run the `.mjs` packer at test time.** Running it against an arbitrary
>    fixture dir would need either modifying the CLI (which hardcodes its input root) or
>    polluting the asset tree. Instead the golden is generated once from the real CLI and
>    committed; the test rasterizes the TS core's blit plan with `pngjs` (a devDependency, pure
>    JS, works in vitest) and compares RGBA against the committed golden. The **browser canvas
>    rasterizer is validated only by the E2E** — happy-dom has no working canvas — exactly like
>    Phase 2's image-decode path.

Two packers that must agree will drift unless a test forces them to; the golden is that force.
(A shared pure-JS layout module imported by both was considered and rejected — the CLI runs as a
bare `node` script while the TS core is Vite-bundled, and sharing one module across that runtime
boundary costs more than the golden test saves.)

`pack-unit-sprites.mjs` is **not modified** — it remains the offline/CI path for committed art
and the source of the golden fixture.

### 5.3 Ingest flow

1. Author drops a PixelLab export folder onto the panel (`<input webkitdirectory>` /
   File System Access API).
2. The panel packs it in-memory, renders the result straight into the animation viewer —
   **preview before persist**, which is exactly the "is it correct?" check requested.
3. On confirm, `POST /units/{type}/art`; the server writes the files; the client registers the
   new set into the runtime overlay. The unit now renders correctly in a playtest with no
   rebuild.
4. Validation surfaced in-panel *before* upload: missing facings, frame-size mismatch across a
   row (the Node packer throws on this), zero-frame animations, unrecognized slugs.

---

## 6. Pillar D — Per-facing attack origins

### 6.1 New authored block

On `UnitDef` **and** on the path catalog file (paths already override `bounds`, so they need
their own origins):

```json
"attackOrigin": {
  "default":  { "x": 0,  "y": -34 },
  "byFacing": { "east": { "x": 14, "y": -30 }, "north": { "x": -6, "y": -33 } }
}
```

Coordinates are the same screen-space lift the renderer already applies (offsets from
`unit.x/unit.y`), so what the crosshair shows is literally what the renderer uses.

### 6.2 Precedence in `getProjectileOriginLift`

Authored wins; otherwise **exactly today's behavior**:

1. `attackOrigin.byFacing[facing]`
2. `attackOrigin.default`
3. `spriteBodyCenterLift()` — current derived geometry (sprite-backed units)
4. legacy `attackVisual.originX/originY` — current behavior for sprite-less placeholders
5. `DEFAULT_BODY_CENTER_OFFSET_Y`

**No migration, and zero visual diff on day one.** We do *not* bake the derived value into the
catalog JSON. `attackOrigin` stays absent until the author actually drags the crosshair; the
editor merely *displays* the derived value as the starting position. That keeps art/bounds
changes auto-following the geometry for every unit nobody has hand-tuned.

The same block also becomes the **beam/channel anchor**, replacing the dead `beamOrigin` (which
no manifest authors, so this is a pure no-op replacement). When unauthored, beams keep their
existing `BEAM_BODY_ANCHOR_FRACTION = 0.5` fallback and projectiles keep `0.3` — the per-use
defaults are preserved, so nothing moves.

### 6.3 Two renderer changes this forces

Both are easy to get wrong; call them out as their own tasks.

- **The lift is now facing-dependent, and the projectile loop doesn't know the facing.**
  `getProjectileOriginLift` is cached per `unitType`. It needs the *owner's facing at the
  moment of firing*, which lives in the `unitAnim` state cache. Add a
  `unitAnim.currentDirection(unitId)` getter and widen the cache key to
  `${unitType}|${path}|${facing}`.
- **Snapshot the lift at spawn, per projectile.** If the lift is recomputed every frame from
  the owner's *current* facing, a unit that turns mid-flight drags its in-flight arrow's origin
  sideways (the origin interpolates the whole flight line). Cache
  `projectileOriginLiftById: Map<projId, {x,y}>` on first sight, evict when the projectile
  disappears. This also fixes the **dead-owner** case — the owner can be gone before the
  projectile lands, and the lookup must degrade to `attackOrigin.default` (else 0), never crash.

### 6.4 Origin editor UI

Inside the animation viewer: a draggable crosshair overlaid on the sprite, plus numeric
x/y inputs. A facing selector switches which `byFacing` entry is being edited; an
"apply to all facings" action writes `default`. A **"fire a test projectile"** button plays the
attacking animation and launches a ghost projectile from the authored origin toward a dummy
target, so the author sees the actual result rather than a static dot.

---

## 7. Pillar E — Paths as first-class editor entities

### 7.1 What the author sees (D1)

The list becomes a faction-filtered tree: each base unit expands to show its paths. Creating an
entity asks **Base Unit** or **Path Unit**:

- **Base Unit** → today's form, plus the new Art and Attack Origin sections.
- **Path Unit** → requires a parent unit (which determines the faction and the directory), then:

| Section | Fields |
|---|---|
| Identity | `path` id, parent unit (locked after create), `description` |
| Overlay | `visionRange`, `projectile`, `damageType`, `attackType`, `projectileScale`, `abilities` (**replace-list**, not additive), `channelLoop`, `bounds` |
| Rank grid | 3 rows (bronze/silver/gold) × `maxHPMultiplier`, `maxMPMultiplier`, `healthRegenMultiplier`, `damageMultiplier`, `attackSpeedMultiplier`, `moveSpeedMultiplier`, `attackRange` (flat), `attackRangeMultiplier`, `armor`, `dodgeChance`, `blockChance` |
| Perk pools | bronze / silver / gold lists — add / remove / reorder |
| Art | same ingestion + viewer as a base unit (path art lives at `assets/.../paths/<path>/`) |
| Attack Origin | same per-facing editor |

Creating a path offers **"add to `<unit>`'s pathChances (weight 1)"** in the same action — that
is the "a unit adds another unit as its path" operation from the request, expressed against the
real data model.

The rank grid must render the **inherited base stat and the resulting value** next to each
multiplier (e.g. `damage 18 × 1.75 = 31`), or the author is editing multipliers blind.

### 7.2 Path & perk persistence (net-new backend)

Paths and perks are embed-only today. Both need the `unit_persistence.go` treatment.

**`path_persistence.go`** — reuses `UNIT_CATALOG_DIR` (paths live under the same tree; no new
env var).

- The path loader currently **panics** on bad data and populates ~8 package-global maps at
  `init()`. Refactor: extract an error-returning `registerPathDef(unitType, faction, file)
  error` used by both the embed loader (which wraps errors in a panic, preserving fail-loud
  startup) and the editor (which surfaces them). Same shape as v1's `validateUnitDef`
  extraction.
- Keep **one** registry keyed by path id holding the parsed file; rebuild the derived maps
  (`pathModifiersByKey`, `pathVisionRangeByPath`, `pathsByUnitType`, …) under an `RWMutex` on
  write. These are read on rank-up / spawn / item / upgrade — **not** on the tick hot path — so
  an RWMutex is fine.
- Saving a path must register it into `pathsByUnitType` so `pathChances` validation and
  `rollProgressionPathLocked` can see it.
- `LoadPersistedPathsIntoOverlay()` at startup, walking the writable tree's `paths/` subdirs.
  The existing `SkipDir("paths")` in `unit_persistence.go` **stays** — units and paths remain
  separately owned; the new loader owns `paths/`.

**`perk_persistence.go`** — writes whole `perks/<rank>.json` arrays.

- **Fix a real latent bug while here:** perk ids are global and a duplicate id **silently
  overwrites** ([perk_defs.go:306](../../server/internal/game/perk_defs.go#L306)). With an
  editor that can create perks, this becomes a live footgun. Validation must **reject** a
  duplicate perk id across the whole catalog, with an error naming the other owner.

**Determinism is preserved:** path rolls sort their keys and perk pools sort by id before
drawing from the seeded RNG. Nothing here introduces map-iteration-order dependence.

### 7.3 Perks in v2: pools only, and the shortfall is visible (D5)

`assignUnitPerkLocked` grants a perk id; the *behavior* comes from a `switch perkID` case arm
across `perks.go`, `perks_attack.go`, `perks_marksman.go`, etc. **The editor can author a
perk's config, tooltip, icon, and pool membership — it cannot create a new effect.** A perk
whose id has no Go case arm is inert. This is the one place v2 misses the north star, and it
is accepted for this version.

Two non-negotiable consequences for the UI:

- Every perk is labelled **"wired"** (a Go handler exists for its id) or **"inert"**
  (config-only, no handler yet). The server can compute this — the set of handled ids is
  knowable — and it ships on the perk payload rather than being guessed client-side.
- The "new perk" action states the limitation up front. A silently no-op perk is the worst
  possible outcome of this editor, and the second-worst is one that *looks* authored.

**Empty perk pools must be safe.** An editor-created path starts with no perk files at all.
Verify that `assignUnitPerkLocked` guards the empty-pool case *before* indexing —
`pool[s.rngPerks.Intn(len(pool))]` with `len(pool) == 0` is an `Intn(0)` panic. If the guard
is missing, add it (a rank-up with no eligible perk grants nothing and continues). This is a
prerequisite for Phase 5, not an afterthought: without it, the first playtest of a
newly-authored path crashes the server on the unit's first Bronze promotion.

### 7.4 The perk rethink (deferred, but design for it)

Two structural warts make the perk system the blocker to the north star. Naming them here so
the eventual redesign isn't relitigated from scratch:

1. **Behavior is Go-side.** Data-driven perks need composable effect primitives (stat deltas,
   triggers, on-hit hooks, auras) that a perk def *assembles*, rather than an id the engine
   branches on. That is the actual work item.
2. **A perk def is bound to one `(unit, path, rank)` by its file location**, and perk ids are
   global with **silent overwrite on duplicate**. Sharing a perk across paths means duplicating
   the id, which silently clobbers. The clean fix is a shared library —
   `catalog/perks/<id>.json` carrying explicit or wildcard `unitType`/`path`/`rank`, with path
   files referencing ids — and it pairs naturally with (1).

v2 must not make either harder: the perk pool file format is unchanged, and the duplicate-id
rejection added in §7.2 is the first step of (2).

---

## 8. HTTP surface

New routes. All writes mirror the item/unit editor contract: validate-first-then-write,
`editorValidationError` → 400 `{error: "validation_failed", message}`, overlay registered only
after a successful disk write.

| Route | Method | Purpose |
|---|---|---|
| `/catalog/factions` | GET | faction registry (merged) |
| `/factions`, `/factions/{id}` | POST, DELETE | create / delete (delete refuses if units remain) |
| `/catalog/archetypes` | GET | `combatProfiles` keys |
| `/catalog/paths` | GET | **full** merged path defs (today only bounds + topology are served) |
| `/paths`, `/paths/{id}` | POST, DELETE | create / delete a path |
| `/perks` | POST | write a whole `perks/<rank>.json` for a `(unit, path, rank)` |
| `/perks/{unit}/{path}/{rank}` | DELETE | drop a rank pool |
| `/catalog/unit-art` | GET | enumerate writable art folders + manifests |
| `/units/{type}/art` | POST | ingest packed art |
| `/assets/units/...` | GET | static serve of `UNIT_ASSETS_DIR` |

**Free wins — already implemented, zero callers, needed for the dropdowns:**
`ListProjectileDefs()`, `ListAbilityDefs()`, `ListEffectDefs()` exist and are wired to nothing.
Expose them as `/catalog/projectiles`, `/catalog/abilities`, `/catalog/effects` so the
`projectile`, `abilities`, and `damageType` fields become real pickers instead of free text
that only fails at save time.

---

## 9. Validation & safety

- Every id segment (`type`, `faction`, `path`, `rank`, perk `id`) matches
  `unitIDPattern = ^[a-z0-9_]+$`, enforced at **both** the handler and persistence layers.
  This is the traversal guard for every path we now write.
- Validate-first-then-write, everywhere. A bad def never touches disk; the overlay is
  registered only after a successful write, so a failed save can't leave inconsistent
  in-memory state.
- **The server is the authority.** Dropdowns are UX; `validateUnitDef` /
  `validatePathDef` / `validatePerkDef` must independently reject anything invalid. The UI is
  never the only guard.
- Deleting a faction with units, or a unit with paths → validation error naming the
  dependents, not a silent orphan.
- Art writes are size-capped and extension-checked (`.png` / `.json` only).

### 9.1 Promotion integrity: a broken `pathChances` can never be saved (D6)

**This is the single most dangerous new failure mode in v2.** `path_defs.go` cross-validates
every `UnitDef.PathChances` key against `pathsByUnitType` at load and **panics** on a dangling
reference. An editor that can write one is an editor that can brick the server on next boot —
and it would brick it *after* the author closed the editor, with a stack trace pointing at a
loader they never touched.

**Rule: the save is rejected. We do not write and revert, we do not auto-repair, we do not
write a partial def.** The unsaved form stays exactly as the author left it, with an inline
error on the offending row telling them what is missing and how to fix it.

`validatePathChancesLocked(def)` runs on **every unit save** and on **every path delete**, and
enforces, for each key in `pathChances`:

| Check | Failure message (inline, on the row) |
|---|---|
| The path exists under *this unit* — `catalog/units/<faction>/<unit>/paths/<key>/<key>.json` is present in the merged (overlay + embed) registry | *"No path `marksman` exists on `archer`. Create the path first, or remove this row."* |
| Its `path` field equals its directory name | *"Path `marksman` is misconfigured: its `path` field says `marksmen`."* |
| Its `ranks` table defines at least one of bronze / silver / gold | *"Path `marksman` has no rank curve. A unit promoted into it would gain nothing."* |
| Weight is `>= 0`, and the weights sum to `> 0` | *"Path weights must sum to more than 0, or the promotion roll has nothing to draw from."* |

Each message names the thing, says why it's wrong, and says what to do. A validation error the
author can't act on is just a locked door.

**Deletion is the same rule from the other side.** Deleting a path that any unit's
`pathChances` still references is rejected, listing the referencing units — because allowing it
produces the identical boot panic. The editor offers the fix (drop the reference) as an
explicit action; it does not perform it silently, because silently editing a *different* unit
than the one on screen is exactly the kind of surprise that erodes trust in an editor.

**Ordering, so the author is never trapped:** creating a path via the base unit's **"Add Path"**
action writes the path file *first*, then adds the `pathChances` row. The intermediate state
(path exists, nothing references it) is valid and boots cleanly. The reverse order is not, and
must not be reachable through any UI affordance.

**Tests are non-optional here** (§10): a dangling key is rejected on unit save; a referenced
path is rejected on delete; and — the real proof — after any editor-driven sequence, a fresh
server boot over the writable catalog does not panic.

---

## 10. Testing

**Server**
- `validateUnitDef` floors: valid passes; one case per rejection; **plus the whole embedded
  catalog passes** (the guard against over-strict rules).
- Faction registry: dir without `faction.json` synthesizes a default; save/delete round-trip;
  delete-with-units is refused.
- Path persistence: save → `pathModifierFor` / `pathsByUnitType` reflect it → delete reverts to
  embed. Overlay-wins ordering.
- Perk persistence: rank-pool round-trip; **duplicate perk id across the catalog is rejected**.
- **Empty perk pool is safe**: a path with no perk files, ranked to Bronze, grants nothing and
  does not panic (`Intn(0)`).
- **Promotion integrity (§9.1)** — one test per rule in the table: dangling key rejected on unit
  save; `path` field / directory mismatch rejected; rank-less path rejected; zero-sum weights
  rejected; delete of a referenced path rejected, naming the referencing units.
- **The boot-panic proof:** drive an editor-shaped sequence against a temp
  `UNIT_CATALOG_DIR` (create path → save unit with `pathChances` → delete path attempt), then
  re-run the catalog loader over that directory and assert it loads without panicking. This is
  the test that actually protects the thing §9.1 is about; the unit-level rejections above are
  how it stays green.

**Client**
- `spritePacking`: **golden-fixture conformance against the `.mjs` packer** (manifest deep-equal
  ignoring `packedAt`; sheet bytes identical). This is the single most important new test — it
  is the only thing preventing two packers from drifting.
- Both export shapes (flat and `states[]`) pack identically.
- Runtime overlay: a registered runtime sprite set shadows a globbed built-in of the same key.
- Rotations row-offset refactor: `getUnitFrame` idle path and `getUnitPortraitImage` still
  resolve correctly.
- Origin precedence: authored `byFacing` > authored `default` > derived `spriteBodyCenterLift` >
  legacy `attackVisual` — with a case per rung.
- Origin lift is **snapshotted per projectile** (owner turns mid-flight → origin does not move;
  owner dies mid-flight → no crash, falls back to default).
- Form round-trip stays lossless (v1's `remainder` invariant) with the new modeled keys removed
  from the remainder bag.

**Manual E2E (the milestone proof — this is the whole feature in one pass)**
1. Create a faction → it appears in the filter.
2. Create a base unit in it → defaults applied → it is placeable and functional (not a 0-HP statue).
3. Drop an art folder → packs → animations and rotations play in the viewer → save → **place it and playtest with no rebuild**.
4. Drag the attack origin per facing → fire a test projectile → save → the arrow leaves the bow in a real match.
5. Add a path to it → set the rank grid → assign perk pools → playtest, rank the unit to Bronze, confirm the path rolls, the multipliers apply, and a perk is granted.

---

## 11. Phasing

Each phase is independently shippable and independently verifiable. The risky infrastructure is
deliberately front-loaded, because Phases 3–5 are all worthless without Phase 2.

| Phase | Content | Risk |
|---|---|---|
| **1** | Factions (registry + filter + create), archetype dropdown, stat floors + template defaults, catalog endpoints for projectiles/abilities/effects | Low — no engine change |
| **2** | **Runtime sprite overlay**, rotations row-offset refactor, animation & rotation viewer (existing art only) | **High** — the load-bearing infra |
| **3** | Browser packer + conformance test, art ingest endpoint, preview-before-persist | Medium |
| **4** | Per-facing attack origins: authored block, renderer precedence, per-projectile lift snapshot, origin editor | Medium — touches live combat rendering |
| **5** | Path entities: path/perk persistence + write routes, path form, rank grid, perk pools | Medium — touches progression |

---

## 12. Known limitations to state out loud

1. **Perks are the one gap to the north star.** A perk without a Go case arm is inert; the
   editor authors config and pool membership, never behavior. Accepted for v2, deferred to the
   perk redesign (§7.4). Everything else about a unit — including its paths and rank curves —
   ships from the editor with no code changes.
2. **Cross-path perk reuse duplicates ids** until the shared perk library lands.
3. **Multi-state PixelLab exports are unsupported** (the existing packer takes `states[0]`);
   the editor inherits this and must say so on ingest rather than silently dropping states.
4. **CLI `pack:sprites` stops hot-reloading in dev** once the Vite watch exclusion lands (§4.3).
5. **Committed vs. authored art:** editor-ingested art lands in the writable assets dir. In dev
   that *is* the source tree, so it is immediately committable. In the packaged desktop build it
   is app-data — local to that machine until exported. This mirrors how `UNIT_CATALOG_DIR`
   already behaves and is not a new concept, but it will surprise someone.

---

## 13. Global constraints (bind every implementation task)

- Follow the item/unit-editor overlay + disk-marshaling template. Do not invent a new
  persistence pattern.
- `Locked` suffix = caller holds `s.mu`. Deterministic sim: no wall-clock, no unseeded rand, no
  map-iteration order driving outcomes. Targets stored by ID, never by pointer across ticks
  (AI_RULES).
- **No simulation behavior changes.** Every origin/art change in this spec is a client-side
  visual lift. The server's projectile origin remains `attacker.X / attacker.Y`.
- No literal `cursor:` declarations in new component CSS, except `cursor: not-allowed` on
  forbidden-action states.
- Do not modify the item editor or the old map editor (`MapEditorPanel.vue`, `views/Editor.vue`)
  — zero diff. The world editor panel IS edited (it's the copy).
- Client type-check is `vue-tsc -b` (build mode) — `--noEmit` false-cleans because the root
  tsconfig is solution-style.
- Go commands from `server/`; client from `client/src/game-portal`. `gofmt -l` flags the whole
  checkout (CRLF) — use `go vet` / `go build` as gates. Known pre-existing failures (cmd/api
  `TestServerReadyLineAndStdinShutdown`) are unrelated; introduce no NEW failures.
- `worldEditorToolbar.test.ts` pins the `enabled` flags for `unit-paths` / `perks` — flipping a
  category to `true` requires updating that test.
