## Context

The authoritative simulation lives in the Go server (`server/internal/game/`); all state mutates inside `GameState.Update(dt)` under a single lock (`s.mu`), and a `MatchSnapshotMessage` is serialised per-viewer each tick and streamed to the Vue client (`client/src/game-portal/src/`). Maps are catalog-driven: `//go:embed catalog/maps/*.json` in `maps.go`, parsed into `protocol.MapConfig`. The map editor (`MapEditorPanel.vue`) edits a `MapConfig` in place and saves it back through `SaveMapCatalogEntry`, which writes the authored form to disk and overlays a hydrated copy into the runtime map registry so edits are live without a restart.

Two existing systems are the templates this change clones:

- **Spatial data is stored as cell sets.** `MapConfig.Terrain` is `[]TerrainTile` (`{x,y,terrain}`), authored compactly on disk as `"grass": [[x,y],…]`. Obstacles, buildings, and placed units are per-cell entries. There is no polygon, point-in-polygon, or boundary-loop code anywhere in the codebase. Zones follow the same idiom: a zone *is* a set of cells.
- **The objective system** (`objective_defs.go`, `objective_handlers.go`, `objective_runtime.go`, added by `campaign-objectives-and-metrics`) is a `map[string]objectiveHandler` registry keyed by a `type` string, with `parseConfig` / `validate` / `initialize` / `evaluate` hooks. Per-match runtime state lives on `GameState.Objectives []objectiveRuntime`; the `evaluate` hook receives the full `*GameState`; completion is sticky/absorbing; per-viewer state is projected into the snapshot by `buildVictorySnapshotForViewerLocked`. The zone-control runtime is a near-isomorphic copy of this.

Building placement has a single chokepoint: `BuildBuilding` (`state_buildings.go:49`), whose footprint loop (`:84-100`) already walks every cell of the proposed building checking the blocked-cell cache and unit overlap. The build-gate is one more predicate in that loop.

Constraints for this change (from `AI_RULES.md`):

- **Server is authoritative.** Zone ownership and capture progress are computed server-side; the client renders snapshot fields only and never simulates capture.
- **Targets by id, resolved + validated every tick.** Zone runtime references zones, buildings, and units by id; it never persists a `*Unit` / `*BuildingTile`. Every lookup is nil/dead/visibility/ownership checked at point of use.
- **Tick determinism.** Capture evaluation must not read wall-clock time, unseeded RNG, or rely on Go map iteration order to drive outcomes. Progress timers advance off the tick `dt`. Iteration over zones uses the stable authored slice order.
- **`*Locked` discipline.** All zone mutation happens in `…Locked` methods called from inside `Update` while holding `s.mu`; no I/O on the tick path.
- **No backwards-compat shims.** `Zones` is additive; old maps load with an empty slice. There is no legacy zone format to migrate.

## Goals / Non-Goals

**Goals:**

- One authorable zone model on `MapConfig` that the editor paints and the runtime consumes, with the perimeter/interior split derived rather than stored.
- A node-centric editor flow: drop a node, grow a polygonal footprint a tile at a time, configure capture, and link nodes into an adjacency graph.
- A registry of three configurable capture mechanics that a designer selects per zone, extensible to a fourth by registering one handler.
- A territorial-spread loop: start holding a seed zone, capture only zones adjacent to a held zone, and unlock building in a zone exactly when you control it.
- A `capture_zone` objective that reuses the existing objective system to make territorial conquest a (possibly required) win condition.

**Non-Goals:**

- Sub-cell geometry. The "polygon" is the derived perimeter of a grid-cell set.
- A balanced PvP domination mode. The mechanics target co-op-vs-AI; competitive tuning is separate.
- Bespoke control-point structure art, per-zone fog rules, and capture reward payloads (all future polish).

## Decisions

### Decision: A zone is a set of cells; perimeter is derived, never stored

Each `Zone` stores `cells [][2]int` (its members) and an `anchor [2]int` (the node). The renderer and the runtime derive the rest:

- **Perimeter cell** = a member cell with at least one 4-neighbour that is not a member of the same zone.
- **Interior cell** = a member cell whose four 4-neighbours are all members.

This makes the headline requirements fall out for free. "Extend the perimeter one tile at a time" is "add one cell to the set" — the ring re-derives. "Perimeter darker, interior lighter" is a pure render pass. "Zones must not overlap; both recalibrate" is enforced by a single `cell → zoneId` ownership: painting a cell into the active zone removes it from whatever zone held it, and both zones' perimeters recompute because perimeter was never materialised.

**Alternatives considered:**

- (a) **Store an ordered boundary polygon + fill via point-in-polygon.** Rejected. Introduces polygon math the codebase has nowhere else, makes per-tile edits and overlap-resolution awkward, and chokes on non-simple shapes and holes. The word "polygon" in the request describes the *visual result*, not the storage.
- (b) **Store perimeter cells and interior cells as two separate sets.** Rejected. Doubles the authored data, and any edit must keep the two sets consistent — exactly the bookkeeping deriving avoids.

### Decision: Single-owner cell membership keyed by an in-editor `cell → zoneId` map

Overlap resolution is "new zone wins." In the editor this is a `Map<cellKey, zoneId>` rebuilt from the zone list; painting reassigns the key and drops the cell from the previous owner's `cells`. On the server, `installZonesLocked` builds the same index once (`map[gridPoint]string`) and panics at load if the authored data violates single-ownership (two zones claiming one cell) — authored maps are static, so a violation is a build error, consistent with how the objective loader treats bad catalog data.

`zoneOwnerForCellLocked(cell)` is then an O(1) lookup used by the build-gate and the `presence` / `clear` occupancy scans.

### Decision: The node is anchor, config hub, and adjacency endpoint

The editor "node" the brush drops is persisted as `Zone.anchor`. It serves three roles with one field: it is the cell the popup attaches to (click the node → edit the zone), it is the endpoint the adjacency graph links (`Link` connects two nodes), and at runtime it is where `control_point` capture resolves (the structure on the anchor). Storing one cell rather than a separate "center" plus "control-point location" keeps the model minimal and the three roles aligned.

### Decision: Capture mechanics are a registry, isomorphic to the objective registry

`zoneCaptureRegistry map[string]zoneCaptureHandler` with `parseConfig(raw) (any, error)`, `validate(filename, zoneID string, cfg any)`, and `evaluate(s *GameState, rt *zoneRuntime, dt float64)`. Three handlers ship:

- **`control_point`** — `evaluate` resolves the building occupying `Zone.anchor` (by id, validated) and sets the zone owner to that building's owner; a neutral/destroyed structure leaves the zone at its current owner or neutral. Captured by *occupying* the structure, not building it — see the deadlock decision below.
- **`presence`** — config `{captureSeconds: float}`. `evaluate` scans units whose grid cell is in the zone (via `zoneOwnerForCellLocked` membership), partitions them by team, and: if exactly one non-owning team is present and the current owner is not present, advances `rt.Progress` by `dt`; if multiple teams are present, sets `rt.Contested = true` and freezes progress; on `rt.Progress >= captureSeconds`, flips `rt.Owner` and resets progress.
- **`clear`** — config `{}` (hostiles are whatever neutral/enemy units occupy the zone at match start and spawn into it). `evaluate` flips the owner to the capturing team once no hostile unit remains inside; sticky thereafter.

Adding a fourth mechanic is one `registerZoneCapture` call plus a handler — no change to the tick loop, the gate, or the snapshot.

**Alternatives considered:** a fixed `switch` on `capture.type` in the tick loop. Rejected for the same reason the objective system uses a registry — it special-cases engine call sites and blocks the editor from discovering available types via a `ListZoneCaptureTypes()` helper.

### Decision: Adjacency is an authored bidirectional graph; capturability is gated on it

Zones are painted as discrete regions with gaps, so cell-touch adjacency would almost never fire. Adjacency is therefore **authored**: `Zone.adjacent []string` lists linked zone ids, and the editor `Link` action toggles the edge on both endpoints. The loader validates that every id in `adjacent` exists and that edges are symmetric (normalising to symmetric if one side is missing).

The runtime capturability gate, applied before any mechanic can flip ownership: a team `T` may capture zone `Z` only if `Z` has at least one neighbour in `Z.adjacent` currently owned by `T` (or an ally of `T`). The seed comes from `startingOwner` — at least one zone must start team-owned or nothing is ever capturable. `tickZonesLocked` computes the per-team capturable frontier from current ownership each tick, then runs each capturable zone's mechanic. A non-capturable zone's mechanic does not run, so presence timers on locked zones do not secretly advance.

**Alternatives considered:** derived-by-proximity (adjacent if cells within N tiles). Rejected — fuzzy, lets a stray nearby zone become unintentionally adjacent, and needs an authored threshold anyway. Authored links give designers exact control over capture order, which is the point of the mechanic.

### Decision: Ownership is team-scoped, stored as a player/team id

A zone's `Owner` is a player id string (or the `neutral` sentinel), but the build-gate, the objective, and the adjacency gate all ask "is X allied with the owner," not "is X exactly the owner." This matches the current co-op single-shared-team reality: a teammate can build in a zone the team holds. The ally check reuses the existing team/owner relationship used elsewhere in combat (same-team resolution); zones add no new team concept.

### Decision: Capture mechanics never require building (no deadlock)

"You must control a zone to build in it" plus "you must own an adjacent zone to capture a zone" would deadlock if capturing a zone required *constructing* something inside it — you can't build in a zone you don't yet control. All three mechanics are therefore satisfiable with units alone: occupy a pre-placed structure (`control_point`), stand in the zone (`presence`), or kill what's inside (`clear`). Build access is strictly the *reward* for controlling a zone, never a *prerequisite* for capturing it. This is an invariant any future mechanic must preserve.

### Decision: Build-gate is one predicate in the existing footprint loop

`BuildBuilding`'s footprint loop already rejects on blocked cells. Add: for each footprint cell, look up `zoneOwnerForCellLocked(cell)`; if the cell is in a zone and that zone's owner is not allied with the builder, reject the placement (no resource spend, consistent with the existing early returns). Cells in no zone are unaffected — the rule restricts zones, it does not partition the whole map. This keeps the gate O(footprint) and colocated with the other placement predicates.

### Decision: Zone runtime mirrors `objectiveRuntime`; geometry is static, control is per-tick

`GameState.Zones []zoneRuntime`, installed once by `installZonesLocked` at match start (like `SetCampaignLevelLocked` installs objectives). Each `zoneRuntime` = `{Def Zone, Owner string, Progress float64, Contested bool}`. The static geometry (`cells`, `anchor`, `adjacent`, `capture`) is sent once in the welcome payload; the per-tick snapshot carries only the mutable `ZoneSnapshot{id, owner, contested, progress}`, keeping the steady-state wire small (zones rarely change). The client derives perimeter/interior and locked-vs-capturable from the static geometry plus current ownership, exactly as the editor does.

### Decision: `capture_zone` is an objective type, not a parallel victory system

Rather than invent a second win-condition path, capturing a zone is exposed as a `capture_zone` objective (`registerObjective("capture_zone", …)`), config `{zoneIds: []string, requireAll?: bool}`. Its `evaluate` reads `s.Zones` ownership and marks the objective complete when the team owns the referenced zone(s). `required: true` then gates victory through the existing AND-of-required-objectives rule, and completion is sticky (capturing once completes the objective even if the zone is later lost), matching the objective system's absorbing-completion semantics. This reuses scope (team/player), the snapshot projection, and the recap UI with zero new victory plumbing.

## Risks / Trade-offs

- **Non-contiguous or holed zones.** Erasing interior cells can split a zone or punch a hole; the model allows it and the perimeter derivation handles it. The editor surfaces it visually (the ring re-derives) rather than forbidding it. Acceptable — a designer who wants a clean region can see the result immediately.
- **Sticky objective vs "must hold to win."** A `capture_zone` required objective completes on first capture and stays complete even if the team loses the zone before the match ends. If a future level wants "hold all zones at match end," that is a new objective variant, not a change to this one. Called out so the semantics are a deliberate choice, not an accident.
- **Presence-scan cost.** `presence` evaluation scans units against zone membership each tick. With membership as an O(1) `cell → zoneId` lookup keyed off each unit's grid cell, this is O(units) per tick over zones that are currently capturable, not O(units × zones). Locked zones are skipped entirely by the adjacency gate.
- **Editor overlap surprise.** "New zone wins" means a careless stroke can silently eat a neighbour's cells. Mitigated by the immediate perimeter recompute (the shrunken zone visibly changes) and by overlap only triggering while actively drawing into the active zone.

## Migration

`Zones` is additive on `MapConfig`; every existing map JSON loads with an empty zone slice and unchanged behaviour (no zones ⇒ build-gate never fires, no zone snapshots, no `capture_zone` objectives). The only authored data is the new demonstration map. No schema version bump, no data migration, no removal of an existing system.

## Open Questions

- Should `control_point` resolve against *any* building on the anchor, or only buildings carrying a specific capability/type? Shipping with "any building on the anchor cell, owner wins" and revisiting if designers need a dedicated flag type.
- Should a contested `presence` zone *decay* progress toward neutral, or merely *freeze*? Shipping with freeze (simpler, deterministic); decay is a tuning follow-up.
