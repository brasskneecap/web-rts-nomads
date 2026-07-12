# World Editor — Sub-project 1: Editor Shell + Play/Reset + Item Popup — Design

**Date:** 2026-07-12
**Branch:** `world-editor`
**Status:** Approved
**Program context:** First of ~4 sub-projects toward an all-in-one, WC3-style world editor. This spec covers ONLY the editor shell. Later sub-projects (own specs): (2) data editors as writable overlays, (3) campaign authoring, (4) runtime custom-art foundation.

## Problem & goal

The team wants a single canvas-based editor — inspired by the Warcraft III world editor — to author every kind of game content (units, items, buildings, maps, abilities, effects, projectiles, perks, unit paths, campaigns) and iterate fast: edit content, drop it on a map, click it to adjust, and playtest instantly. The #1 goal is **iteration speed**; a strong secondary goal is **player-to-player content sharing without rebuilding the game** (WC3 map-sharing model).

This first sub-project builds the **shell**: the unified canvas + toolbar, per-placed-unit editing, an embedded Item/Equipment editor, and a Play→Pause-resets harness. It uses content whose art already ships in the build. It does NOT build the deep "create/edit a unit TYPE" data editors or the runtime custom-art pipeline (those are later sub-projects).

## Foundational findings (from recon — bind the design)

- **Simulation is fully server-authoritative** (Go tick loop, 20/s; browser only sends commands + renders snapshots). "Play" must run a real server-side `GameState`/`Loop`; there is no client-side simulator to reuse.
- **Map/campaign/objective DATA already shares at runtime** — `MapConfig` serializes to JSON, travels over the WS (gzip'd, content-hashed via `mapContentCache`), and persists to disk (`/maps` POST → writable overlay → `LoadPersistedMapsIntoOverlay`). A desktop-build player can receive a map and re-host it.
- **Maps reference art by string key only** — no binary travels with a map. So content that reuses EXISTING art is already fully shareable; only brand-NEW art is blocked.
- **All art except item icons is build-time bundled** via `import.meta.glob`. Item icons are the one runtime upload+serve precedent (`POST /items/{id}/image`, `GET /catalog/items/{id}/image`, bundle→server fallback). Generalizing that is sub-project 4, out of scope here.
- **`PlacedUnit` today carries only** `GridCoord`, `ID`, `PlayerSlot`, `UnitType`, `AggroRange`, `LeashRange` — no per-instance rank/items/perks.

## Decisions (from brainstorming)

1. **First milestone = shell spine + play/reset + Item editor popup** ("all-in-one" from day one, but only the Item data editor is wired; other categories are visible-but-disabled).
2. **Play presents as an in-editor mode** — swap the editor canvas to the real game renderer in place; a Play/Pause/Stop bar overlays. No navigation to a separate "game" screen.
3. **Approach A** — copy the map editor into a new `world-editor/` folder and extend it; reuse the maps catalog (`/maps`) for saves so both editors coexist and produce the same `MapConfig`; swap to the game renderer for Play.
4. **Extend `PlacedUnit`** with optional per-instance rank/items/perks, applied at spawn, so placed units are meaningful in a playtest.
5. **Play = the map played as the game would** (its configured player slots / waves / objectives), the author observing/controlling — NOT a reduced sandbox.
6. **Toolbar shows the full future category set**, with unbuilt categories disabled ("coming soon"), so the vision is visible.

## Architecture

### §1 Structure & placement (client)

- New: `client/src/game-portal/src/views/WorldEditor.vue` (route `/world-editor`), and a `components/world-editor/` folder.
- The map editor's canvas + palette + placement logic is **copied** (not imported/shared) into `world-editor/WorldEditorCanvas.vue`. The existing `Editor.vue` / `MapEditorPanel.vue` are never modified — the old editor stays fully working and reachable in parallel.
- **Top toolbar** (`world-editor/WorldEditorToolbar.vue`): category buttons. Clicking a category opens a floating **popup panel** anchored below the toolbar (not a full-screen mode). Milestone-1 ACTIVE: Terrain, Obstacles, Buildings, Units (placement palettes), **Items** (embedded existing editor), and **Play**. DISABLED "coming soon": Abilities, Effects, Projectiles, Perks, Unit Types, Unit Paths, Campaigns.
- MainMenu entry deferred to a later polish pass; for this milestone the route is reachable directly (dev) — a menu plank can be added once the shell is proven. (If desired, a temporary MainMenu entry is a one-line add; flagged, not required.)

### §2 Editing interactions

- Reuse the copied palettes to paint terrain and drop obstacles/buildings/units — identical to the map editor.
- **New: click a placed unit → select + open a Unit Instance popup** (`world-editor/UnitInstancePopup.vue`): edit
  - `playerSlot` (dropdown of the map's slots)
  - `rank` (dropdown of the unit's ranks, e.g. bronze/silver/gold)
  - `items` (multi-select from the item catalog, filtered by the unit's allowed types where applicable)
  - `perks` (multi-select from that unit type's perk catalog)
  - `aggroRange` / `leashRange` (existing).
  These persist onto the `PlacedUnit`.
- Click a placed building → its existing placement/owner/metadata props (reused from the map editor). Building TYPE editing is a later sub-project.
- Selection state, delete, and drag reuse the map editor's mechanisms.

### §3 PlacedUnit extension (server + protocol)

- `server/pkg/protocol/messages.go` — `PlacedUnit` gains:
  ```go
  Rank  string   `json:"rank,omitempty"`
  Items []string `json:"items,omitempty"`
  Perks []string `json:"perks,omitempty"`
  ```
  Fully back-compat: absent = current behavior. `UnmarshalJSON`'s legacy path is unaffected.
- Spawn wiring: the placed-unit spawn path (`spawnPlacedUnitsForPlayerLocked` in `state.go`, called via `state_spawn.go`) applies, when present:
  - `Rank` → set `unit.Rank` + `applyRankModifiersLocked` (reuses rank stat pipeline).
  - `Items` → append to the unit's equipment + `recomputeUnitEquipmentBonusLocked` (reuses the equip pipeline, which already resolves procs/modifiers).
  - `Perks` → assign the perk IDs onto the unit's perk state (reuses existing perk assignment).
- Validation at hydrate (`hydratePlacedUnits`, `maps.go`): unknown item/perk id or invalid rank for the unit type → drop that field with a `slog.Warn` (mirrors the existing "unknown unitType → drop entry" discipline), never a hard failure.
- Client mirror: `client/.../game/maps/` `PlacedUnit`/`MapConfig` types gain `rank?`, `items?: string[]`, `perks?: string[]`; the world editor reads/writes them; the map editor is untouched (it just ignores the new optional fields).

### §4 Data & persistence

- The editor's working document is a `MapConfig`. Save/load through the **existing `/maps` catalog** — same overlay, content hashing, 409 campaign-conflict flow, and player-to-player transfer that already work. No new maps catalog.
- New / Load (from `/maps` list) / Save picker, mirroring the map editor's. Start state = an empty map (like the current map editor).
- The world editor and old map editor read/write the same map catalog; a map authored in one opens in the other (the new per-instance unit fields are simply absent on map-editor-authored maps and ignored by it).

### §5 Play / Reset harness

- **Play** (`world-editor/usePlaytest.ts` composable):
  1. Persist the current in-memory `MapConfig` to the maps overlay so the server can start a match that includes unsaved placements. Use the working map's id when it has one; a brand-new, never-saved map is persisted under a reserved scratch id (`__world_editor_scratch__`) so Play works before the author has named/saved their map.
  2. Start a **real server-authoritative solo playtest match** on that map id, via the existing match-start flow (the same path the game's "Start Game" uses for a single player against the map's configured opponents/waves).
  3. Swap the editor canvas to the game's live renderer (the match `CanvasRenderer` + `NetworkClient` snapshot stream) and wire input, overlaid with a **Play/Pause/Stop bar**.
- **Pause / Stop (reset)**:
  1. Stop and discard the match — `Loop.Stop()` + drop the `GameState` (server), disconnect the snapshot stream (client).
  2. Swap the canvas back to the editor renderer and re-render the editor's **untouched** `MapConfig`.
  - Because the editor retains its own authoritative map, placed units "snap back" for free — there is nothing to undo; the mutated match state is simply thrown away.
- Playtest outcome is never persisted. The scratch overlay map is the working map's own id (overwritten on the next Play), so no cleanup is needed.
- Desktop build (embedded server) runs this locally; the dev flow uses the running Go server. Both go through the same match API — no new match-creation primitive is needed if the overlay+match-by-id path is used; if a "start match from posted MapConfig" endpoint proves cleaner during implementation, it is an allowed refinement (documented in the plan).

### §6 Reuse & isolation

- The existing `ItemEditorPanel.vue` embeds **unchanged** as the Items popup.
- The existing map editor (`Editor.vue`, `MapEditorPanel.vue`) is **never modified**; it remains the fallback until the world editor is refined.
- The world editor's canvas is a copy — divergence from the map editor is expected and fine (no shared-component coupling to maintain).

## Error handling

- Play on an unstartable map (missing required start setup) → inline error in the Play bar; match start/teardown failures surface there, editor state preserved.
- Per-instance references (item/perk/rank) validated server-side at hydrate; invalid ones are dropped with a warning, never crashing a spawn or a save.
- Save conflicts (campaign level ownership) reuse the existing 409 + `?reassignLevel=true` flow.
- Concurrent editor instances / unsaved-edit loss on load: last-write-wins (same as the map editor); a dirty-state guard on New/Load is a nice-to-have, not required this milestone.

## Testing

- **Server (Go):** `PlacedUnit` round-trip — a map with rank/items/perks saves, hydrates, and a started match spawns those units at the set rank with the items equipped and perks applied; unknown refs dropped-with-warning; old maps (no new fields) load and behave identically.
- **Client (vitest):** pure transforms for the Unit Instance popup (form ↔ `PlacedUnit` fields); maps-service reuse; toolbar renders the full category set with correct enabled/disabled state.
- **Play/reset (integration-heavy):** a thin harness test that Play starts a match and Stop tears it down cleanly (no leaked loop/goroutine), plus a manual E2E: drop two hostile units → set one's rank/items/perks → Play → watch them fight → Pause → both snap back to placement.

## Out of scope (this sub-project)

- Creating/editing unit TYPES, buildings, projectiles, abilities, effects, perks as catalog defs (sub-project 2).
- Campaign authoring UI (sub-project 3).
- Runtime custom-art loading / upload / packing / portable content-pack format (sub-project 4) — i.e. brand-new art. Everything in this milestone uses art already in the build.
- A MainMenu entry / polished navigation (deferrable one-liner).
- Undo/redo, multi-select-heavy editing beyond what the map editor already provides.

## Roadmap (program context, NOT this spec)

2. **Data editors** — writable overlays (the item-editor pattern) for buildings, then unit types + paths + perks, then projectiles/abilities/effects. Content reusing existing art becomes shareable the moment its data has an overlay.
3. **Campaign authoring** — objectives/rewards/level graph on the existing campaign-from-map-tag system.
4. **Runtime custom-art foundation** — generalize item-icon upload/serve to every art category, an in-editor pack pipeline (generalizing `scripts/pack-unit-sprites.mjs`), and a portable content-pack format bundling map JSON + custom defs + custom art for true brand-new-art sharing.
