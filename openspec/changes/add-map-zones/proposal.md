## Why

Maps today are a flat grid where any cell a player can reach is a cell they can build on (subject only to footprint/blocked/tier checks in `BuildBuilding`). There is no notion of *territory* — no authorable region a team must earn before it can expand into it, and no spatial objective beyond "kill these units / survive these waves." Designers have asked for a capture-the-territory loop: a map carved into discrete **zones** where a team starts holding a seed zone, pushes outward by capturing adjacent zones, and unlocks the ability to build in each zone only once it controls that zone. Capturing a zone should also be expressible as a (possibly required) objective so a level can be won by territorial conquest.

This requires two things the engine does not have:

1. **An authoring surface.** A way to paint zones in the map editor — drop a node, grow a polygonal footprint a tile at a time, configure how the zone is captured, and link zones into an adjacency graph that defines the capture order.
2. **A runtime.** Per-match zone control state, three configurable capture mechanics, an adjacency gate that enforces "you may only capture a zone bordering one you already hold," a build-gate that ties construction rights to zone ownership, and a `capture_zone` objective type that plugs into the existing objective system.

The objective system added by `campaign-objectives-and-metrics` already gives us the exact extension shape to clone: a registry of typed handlers, per-tick evaluation against the authoritative `GameState`, sticky completion, and per-viewer snapshot exposure. Zones reuse that shape rather than inventing a new one.

## What Changes

### Data model (protocol)

- Add `MapConfig.Zones []Zone`. Each `Zone` carries `id`, optional `name`, an `anchor` grid cell (the editor "node"), a `cells` member set, a `capture` config (`{type, …per-type fields}`), a `startingOwner`, and an `adjacent []string` list of linked zone ids. Stored compact on disk (per-zone cell list) via the existing catalog renderer.
- A cell belongs to **exactly one** zone. The perimeter/interior distinction is **derived** (a member cell with any non-member 4-neighbour is perimeter), never stored.
- Add a per-tick `ZoneSnapshot` (`id`, `owner`, `contested`, `progress`) to the match snapshot; zone geometry + adjacency are static and travel once in the welcome payload.

### Editor (TS / Vue 3)

- Add a `zone` brush mode to `MapEditorPanel.vue`. Clicking an empty cell drops a node and creates a zone with a default 5×5 footprint.
- Clicking an existing node opens a zone popup: edit name, choose capture type and its fields, set starting owner, and three actions — **Draw Zone** (enter expand mode: left-click adds a cell, right-click removes one, Esc exits), **Link** (click another node to toggle a bidirectional adjacency edge), **Delete**.
- Overlap rule: painting into a cell owned by another zone reassigns it to the active zone (last-writer-wins); both zones' derived perimeters recompute.
- Render zones on the editor canvas: perimeter cells darker grey, interior lighter, adjacency drawn as lines between nodes.

### Runtime (Go)

- Add `s.Zones []zoneRuntime` to `GameState`, installed from `MapConfig.Zones` at match start, mirroring `s.Objectives`. Each runtime carries the static `Zone` def plus mutable `Owner`, `Progress`, and `Contested`. Zones are referenced by id and resolved + validated each tick.
- Add a `zoneCaptureRegistry` mirroring `objectiveRegistry`, with three handlers: `control_point` (own the pre-placed structure on the node, captured by occupation), `presence` (a capture-progress timer that fills while exactly one team occupies the zone), and `clear` (flip to the team that kills the last hostile inside).
- **Adjacency gate:** a team may capture zone `Z` only if it already owns a zone listed in `Z.adjacent`. The seed frontier comes from `startingOwner`. This gate applies to all three mechanics.
- **Build-gate:** add one guard to `BuildBuilding`'s footprint loop — any footprint cell inside a zone requires that zone's owner to be allied with the builder. Cells in no zone are unaffected. Building is the reward for control, never a prerequisite for capture.
- **Objective:** register a `capture_zone` objective type whose config references zone id(s); evaluation reads `s.Zones` ownership. `required: true` gates victory; completion is sticky.

### Catalog data

- Author a demonstration map with a small zone graph (one seed zone + two captured-via-adjacency zones, one per capture mechanic) and a `capture_zone` objective, to exercise the full loop end to end.

## Capabilities

### New Capabilities

- `map-zones` — the authorable zone model and editor: the `Zone` schema on `MapConfig`, single-owner cell membership with derived perimeter/interior, the `zone` brush (node placement, draw/expand, link, delete, overlap-reassign), and editor rendering.
- `zone-control` — the runtime territorial system: per-match zone control state, the capture-mechanic registry (`control_point`, `presence`, `clear`), the adjacency capturability gate, ownership-gated building, the `capture_zone` objective type, and zone snapshot exposure.

## Impact

- **Protocol:** `MapConfig` gains `Zones []Zone` (new `Zone`, `ZoneCapture` types). Match snapshot gains `Zones []ZoneSnapshot`; welcome payload carries static zone geometry + adjacency. Map JSON gains a `zones` array — additive, old maps load with an empty slice.
- **Server (Go):**
  - `server/pkg/protocol/messages.go` — `Zone`, `ZoneCapture`, `ZoneSnapshot`, `MapConfig.Zones`.
  - `server/internal/game/maps.go` — hydrate/normalise zones on load; validate the adjacency graph and cell-ownership invariant; carry zones through `cloneMapConfig` and the catalog renderer.
  - `server/internal/game/zone_defs.go` (new) — `zoneRuntime`, `zoneCaptureRegistry`, `registerZoneCapture`, parse/validate/install.
  - `server/internal/game/zone_handlers.go` (new) — the three capture handlers.
  - `server/internal/game/zone_runtime.go` (new) — `installZonesLocked`, per-tick `tickZonesLocked` (adjacency gate + capture evaluation), `zoneOwnerForCellLocked`, `zoneSnapshotsLocked`.
  - `server/internal/game/state.go` — `Zones []zoneRuntime` field; install at match start; call `tickZonesLocked` in `Update`; include zone snapshots.
  - `server/internal/game/state_buildings.go` — build-gate guard in `BuildBuilding`'s footprint loop.
  - `server/internal/game/objective_handlers.go` — register `capture_zone`.
  - `server/internal/game/catalog/maps/*.json` — demonstration map.
- **Client (Vue / TS):**
  - `client/src/game-portal/src/components/MapEditorPanel.vue` — `zone` brush, popup, draw/link/delete, overlap, editor rendering.
  - `client/src/game-portal/src/game/network/protocol.ts` and map config types — mirror `Zone` / `ZoneSnapshot`.
  - In-match rendering — owner tint + contested + capture-progress + locked-vs-capturable indication from `ZoneSnapshot`.
- **Invariants:** zones referenced by id, resolved + validated each tick (no persisted `*Unit` / `*BuildingTile`); capture evaluation is deterministic (no wall-clock, no unseeded RNG, no map-iteration-order-driven outcomes) and runs inside the tick loop under `s.mu`; no I/O on the tick path; the client renders snapshot fields only and never simulates capture.

## Out of Scope

- **Free-form zone shapes beyond grid cells.** Zones are sets of grid cells; the "polygon" is the derived perimeter of that set. No sub-cell geometry.
- **PvP zone flipping back and forth as a balance system.** The model supports loss of a zone (presence/control_point can flip), but tuning a competitive domination mode is a separate concern. The shipped mechanics target the co-op single-team-vs-AI posture.
- **A dedicated capturable-structure building type with its own art/animation.** `control_point` capture resolves against whatever building sits on the anchor; bespoke control-point art is future polish.
- **Per-zone fog-of-war rules.** Zone geometry is treated as always-known map structure; control state is always visible in the snapshot. Hiding enemy zone control under shroud is out of scope.
- **Reward payloads on zone capture** (legend points, items). The objective completes; bonus rewards on capture are a future change.
