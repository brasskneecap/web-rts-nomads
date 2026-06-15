## 1. Protocol types

- [x] 1.1 In `server/pkg/protocol/messages.go`, add a `Zone` struct: `ID string`, `Name string` (omitempty), `Anchor GridCoord`, `Cells [][2]int` (compact `[x,y]` pairs), `Capture ZoneCapture`, `StartingOwner string`, `Adjacent []string` (omitempty). Add `ZoneCapture` with `Type string` and a raw `Config json.RawMessage` (per-mechanic config, parsed server-side).
- [x] 1.2 Add `Zones []Zone \`json:"zones,omitempty"\`` to `MapConfig`.
- [x] 1.3 Add `ZoneSnapshot` struct: `ID string`, `Owner string`, `Contested bool` (omitempty), `Progress float64` (omitempty). Add `Zones []ZoneSnapshot \`json:"zones,omitempty"\`` to the match snapshot message, and add the static zone geometry to the welcome payload message.
- [x] 1.4 Confirm the JSON shape matches the compact terrain encoding so the existing catalog renderer (`RenderCatalogEntryJSON`) emits `cells` as a flat `[[x,y],…]` list; extend the renderer only if zones need bespoke compaction.

## 2. Server: zone load, validation, hydration

- [x] 2.1 In `server/internal/game/maps.go`, normalise `Zones` on load: nil → empty slice; default `StartingOwner` to the `neutral` sentinel when empty; default `Name` to the id when empty.
- [x] 2.2 Add `validateZonesLocked`-style load validation (panic at startup, naming the map file): unique zone `id`; single-owner cell membership (no cell listed by two zones); every `Adjacent` id names an existing zone; `Capture.Type` is registered; the mechanic's `validate` passes. Normalise adjacency to symmetric (if A lists B, ensure B lists A).
- [x] 2.3 Carry `Zones` through `cloneMapConfig` (deep-copy `Cells`, `Adjacent`, and `Capture.Config`) and through `hydrateEntryInPlace` so editor saves and the runtime overlay preserve zones.
- [x] 2.4 Tests: map with no zones loads empty; valid zones load; duplicate id panics; overlapping cells panic naming the cell; dangling adjacency panics; unknown capture type panics; adjacency normalised symmetric.

## 3. Server: capture mechanic registry

- [x] 3.1 Create `server/internal/game/zone_defs.go` with `zoneCaptureHandler` (`parseConfig(raw) (any, error)`, `validate(filename, zoneID string, cfg any)`, `evaluate(s *GameState, rt *zoneRuntime, dt float64)`), a `zoneCaptureRegistry map[string]zoneCaptureHandler`, a `registerZoneCapture(typeKey, handler)` helper (panics on duplicate / missing hook, like `registerObjective`), `GetZoneCaptureHandler`, and a sorted `ListZoneCaptureTypes()` for editor schema discovery.
- [x] 3.2 Define `zoneRuntime` struct: `Def protocol.Zone`, `Owner string`, `Progress float64`, `Contested bool`. Add `Zones []zoneRuntime` to `GameState` in `state.go`.
- [x] 3.3 Implement `installZonesLocked(cfg protocol.MapConfig)` — build the `Zones` slice (owner ← `StartingOwner`, progress 0, contested false) and the `map[gridPoint]string` cell→zoneId index used by lookups. Call it once at match start alongside objective install.
- [x] 3.4 Implement `zoneOwnerForCellLocked(cell gridPoint) (zoneID string, ok bool)` and an `(s *GameState) zoneRuntimeByIDLocked(id string) *zoneRuntime` resolver. Implement an ally check helper reusing the existing team/owner relationship (`zonesAlliedLocked(ownerA, ownerB string)`).

## 4. Server: per-tick zone evaluation (adjacency gate + mechanics)

- [x] 4.1 Implement `tickZonesLocked(dt float64)`: no-op when `Zones` empty. Compute, per team, the capturable frontier — a zone is capturable by team T iff some zone in its `Adjacent` is currently owned by T or an ally. Iterate zones in stable authored order; for each capturable zone, dispatch to its registered mechanic's `evaluate`. Reset `Contested` at the top of each zone's evaluation.
- [x] 4.2 Call `tickZonesLocked` from `GameState.Update`, after movement/combat resolve (so unit positions and building ownership are settled) and before the objective evaluation and snapshot build.
- [x] 4.3 Create `server/internal/game/zone_handlers.go`. Implement `control_point`: resolve the building on `Def.Anchor` by id, validate (non-nil, visible, HP>0), set `rt.Owner` to its owner; leave owner unchanged when missing/owner-less.
- [x] 4.4 Implement `presence` (config `{captureSeconds float64}`, must be > 0): scan units whose grid cell is in the zone via the cell index; partition by team; sole non-owning team present and owner absent → advance `rt.Progress += dt`; multiple teams → `rt.Contested = true`, freeze; `rt.Progress >= captureSeconds` → flip `rt.Owner`, reset progress.
- [x] 4.5 Implement `clear` (config `{}`): if no hostile (neutral/enemy) unit occupies the zone's cells, flip `rt.Owner` to the capturing team and keep it (sticky); otherwise leave unchanged.
- [x] 4.6 Register the three mechanics in an `init()` in `zone_handlers.go`.
- [x] 4.7 Tests: adjacency gate unlocks/locks correctly and capturing unlocks neighbours; `presence` captures over `captureSeconds`, freezes when contested, does not advance on a locked zone; `clear` flips only when the last hostile inside dies; `control_point` follows the anchor structure's owner and ignores a destroyed structure; a two-run determinism replay produces identical ownership timelines tick-for-tick.

## 5. Server: ownership-gated building

- [x] 5.1 In `server/internal/game/state_buildings.go` `BuildBuilding`, inside the existing footprint loop (`:84-100`), add: `if zoneID, ok := s.zoneOwnerForCellLocked(cell); ok { if !s.zonesAlliedLocked(zoneRuntime.Owner, playerID) { return } }`. Cells in no zone are unaffected; rejection spends no resources (matches existing early returns).
- [x] 5.2 Tests: build rejected in a neutral zone (no resource spend); allowed in a team-owned zone; unaffected outside any zone; rejected when a footprint straddles a controlled and an uncontrolled zone.

## 6. Server: `capture_zone` objective

- [x] 6.1 In `server/internal/game/objective_handlers.go`, register `capture_zone` (config `{zoneIds []string, requireAll bool}`): `validate` requires non-empty `zoneIds` and (where the map is resolvable at load) that each id names a zone; `evaluate` reads zone ownership from `s.Zones`, completes when the scope's team owns the referenced zone(s) — all if `requireAll`, else any. Rely on the objective system's sticky completion.
- [x] 6.2 Tests: completes on capturing the referenced zone; `requireAll` waits for all; stays completed after the zone is later lost; `required: true` gates victory in AND with other required objectives.

## 7. Server: snapshot

- [x] 7.1 Implement `zoneSnapshotsLocked() []protocol.ZoneSnapshot` (id, owner, contested, progress) and include it in the per-viewer match snapshot. Include the static zone geometry (cells, anchor, adjacency, capture type) in the welcome payload build.
- [x] 7.2 Test: snapshot carries one `ZoneSnapshot` per zone with current owner/contested/progress; welcome payload carries the static geometry.

## 8. Client: protocol mirror + map config types

- [x] 8.1 In `client/src/game-portal/src/game/network/protocol.ts` (and the map-config type module), mirror `Zone`, `ZoneCapture`, and `ZoneSnapshot`, and add `zones` to the map config and to the snapshot/welcome shapes.
- [x] 8.2 Add a small zone-geometry helper module: derive perimeter vs interior from a cell set (member cell with a non-member 4-neighbour = perimeter), and build a `cellKey → zoneId` index. Used by both the editor and in-match rendering.

## 9. Client: editor zone brush

- [x] 9.1 In `MapEditorPanel.vue`, add `'zone'` to the `brushMode` union and palette. Add editor state for the selected zone id and the current zone sub-mode (`idle | draw | link`).
- [x] 9.2 Node placement: clicking an empty cell in zone mode creates a zone (generated unique id, anchor = clicked cell, default 5x5 cells clipped to bounds) and selects it.
- [x] 9.3 Zone popup on node click: edit `name`; capture-type selector (driven by the mechanics list) that swaps the per-type field set (`presence` → `captureSeconds`; others → their fields); `startingOwner` selector; buttons **Draw Zone**, **Link**, **Delete**.
- [x] 9.4 Draw/expand mode: left-click adds a cell to the selected zone, right-click removes a member cell, Esc or re-clicking the node exits. Maintain the `cellKey → zoneId` index on every edit.
- [x] 9.5 Overlap reassign: adding a cell owned by another zone removes it from that zone's `cells` and assigns it to the active zone; recompute both perimeters.
- [x] 9.6 Link mode: after **Link**, clicking another zone's node toggles a symmetric adjacency edge on both zones; self-link is a no-op.
- [x] 9.7 Delete: remove the zone and strip its id from every other zone's `adjacent`.
- [x] 9.8 Follow the project cursor convention — do not write literal `cursor:` declarations in component CSS for the new zone controls (the global rules handle it; `not-allowed` only for forbidden-action states).

## 10. Client: editor + in-match rendering

- [x] 10.1 Editor canvas: render zone perimeter cells in darker grey and interior in lighter, derived each frame; draw adjacency edges as lines between nodes; update immediately on paint/remove/link.
- [x] 10.2 In-match rendering: from `ZoneSnapshot` + static geometry, tint zones by owner, indicate contested and capture progress, and visually distinguish locked vs capturable zones for the viewing team. Render snapshot fields only — no client-side capture simulation.

## 11. Catalog: demonstration map

- [x] 11.1 Author a demonstration map under `server/internal/game/catalog/maps/` with a seed zone (`startingOwner` = the team) linked to two zones, each using a different capture mechanic (e.g. `presence` and `clear`), plus a third zone reachable only after the first is captured — exercising the adjacency frontier.
- [x] 11.2 Add a `capture_zone` objective (`required: true`) on the demonstration level so territorial conquest wins the round; include one optional `capture_zone` for the third zone.
- [x] 11.3 Smoke test: load the demonstration map, install zones + objectives, simulate capturing the frontier, and assert build-gating opens on the captured zone and the required objective completes.

## 12. Verification

- [x] 12.1 `go build ./...`, `go vet`, and the zone test suite pass (18 zone tests green; the 4 unrelated failures in the game package are pre-existing/flaky, verified against a clean tree). Client `vue-tsc -b` introduces zero new type errors (2 pre-existing unrelated errors remain in `useGameClient.ts` / `GameClient.ts`).
- [ ] 12.2 Manual editor pass: paint a zone, extend it, link two zones, set each capture type, save, reload — geometry and config round-trip.
- [ ] 12.3 Manual match pass on the demonstration map: confirm the seed zone is buildable, an adjacent zone is capturable and a non-adjacent one is locked, capturing opens building rights, and the required `capture_zone` objective wins the round.
