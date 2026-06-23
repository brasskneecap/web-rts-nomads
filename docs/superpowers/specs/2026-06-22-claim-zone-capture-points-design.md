# Claim Zone — Multiple Capture Points

**Date:** 2026-06-22
**Status:** Design approved, pending implementation plan

## Problem

A claim zone today has exactly **one** capture point: a single 2×2 build slot
derived from the zone's anchor node. The team builds a tower on that slot and
defends it for `defendSeconds` to capture the zone
([`evaluateClaimCapture`](../../../server/internal/game/zone_handlers.go),
[`isClaimSlotCell`](../../../server/internal/game/zone_handlers.go)). The count
"1" is hard-coded into the slot geometry.

Map authors want to configure a claim zone with **N** capture points so a zone
requires building and defending multiple towers to be captured.

## Requirements (settled during brainstorming)

1. **Independent capture.** Each capture point is its own mini-claim with its
   own defend timer and its own captured state. The zone flips to the team only
   once **all** points are individually captured.
2. **Sticky per point.** Once a point reaches `defendSeconds` it is captured for
   good — losing its tower afterward does not revert it. (Matches the existing
   whole-zone sticky claim behavior, applied per point.)
3. **Hand-placed slots.** The map author places each 2×2 slot by hand in the map
   editor (a new "Place Capture Point" tool), rather than auto-deriving them.
4. **Per-point HUD progress.** Each point reports its own defend progress and
   captured state to the client so the HUD can show "2/3 points held" plus an
   individual timer bar per point.
5. **Shared config.** All points in a zone share one `defendSeconds` and one
   `towerType`. There is no per-point duration, per-point tower type, per-point
   owner, or "any M of N" threshold (explicitly out of scope — YAGNI).
6. **Backward compatible.** Every existing single-point claim map keeps working
   with no data change: a claim zone with no explicit points falls back to a
   single slot at its anchor.

## Approach

Capture-point geometry lives as a **typed top-level field on `Zone`**, mirroring
the existing precedent: presence zones store their `captureCells` geometry as a
top-level `Zone` field (not inside the opaque `capture.config`) so that both the
server geometry checks and the client renderer can read it directly. Claim
points follow the same pattern.

The shared timing config (`defendSeconds`, `towerType`) stays in
`claimCaptureConfig`, exactly as the presence mechanic keeps `captureSeconds` in
config while its geometry lives top-level.

### Data model — protocol

`server/pkg/protocol/messages.go` and
`client/src/game-portal/src/game/network/protocol.ts`:

- **`Zone`** gains one optional field, parallel to `CaptureCells`. Each entry is
  a grid cell that is the **top-left of a 2×2 tower slot**:

  ```go
  ClaimPoints [][2]int `json:"claimPoints,omitempty"`
  ```
  ```ts
  claimPoints?: [number, number][]
  ```
  **Empty/absent ⇒ a single slot at `Anchor`** (the backward-compatible
  fallback). No existing map data changes.
- **`claimCaptureConfig`** is unchanged: `defendSeconds` + `towerType` are shared
  by all points.
- **`ZoneSnapshot`** gains a per-point array, in the same authored order as
  `Zone.ClaimPoints`. `Progress` is a normalised 0..1 fraction (defend timer ÷
  `defendSeconds`), consistent with the existing `ZoneSnapshot.Progress`
  convention:

  ```go
  ClaimPoints []ZoneClaimPointSnapshot `json:"claimPoints,omitempty"`

  type ZoneClaimPointSnapshot struct {
      Progress float64 `json:"progress"`
      Captured bool    `json:"captured,omitempty"`
  }
  ```
  ```ts
  claimPoints?: { progress: number; captured?: boolean }[]
  ```

### Runtime — server

`server/internal/game/zone_runtime.go` / `zone_handlers.go`:

- `zoneRuntime` gains a mutable per-point slice, built once in
  `installZonesLocked`:
  ```go
  type claimPointState struct {
      Progress float64 // defend-timer accumulator (seconds)
      Captured bool
  }
  ```
  Field `claimPoints []claimPointState`, sized to `len(Def.ClaimPoints)`, or `1`
  when `ClaimPoints` is empty (anchor fallback). This is pure per-tick working
  state — it stores no `*Unit` / `*BuildingTile`, so the "targets by ID, not
  pointer" invariant is satisfied trivially.
- A small helper resolves the effective point list for a runtime:
  `claimPointCells(rt) [][2]int` returns `rt.Def.ClaimPoints`, or
  `[][2]int{{rt.Def.Anchor.X, rt.Def.Anchor.Y}}` when empty.
- `evaluateClaimCapture` becomes a per-point loop:
  - For each point whose `Captured` is false: resolve the completed team tower
    on that point's 2×2 slot (matching `towerType` if set). Tower present →
    advance `claimPoints[i].Progress += dt`; on reaching `defendSeconds` set
    `Captured = true` and reset its `Progress` to 0. Tower missing/destroyed →
    reset that point's `Progress` to 0 (the team must keep it alive — unchanged
    semantics, now per point).
  - Captured points are skipped (sticky).
  - When **every** point is captured → set `rt.Owner =
    protocol.ZoneCaptureTeamOwner` (existing zone-level sticky capture).
  - `rt.Capturing = true` if any point advanced its timer this tick.
  - `rt.Progress = ` the maximum in-flight point fraction (uncaptured points
    with a standing tower), so the existing top-of-screen "Defending" bar keeps
    showing the most-progressed point. Per-point bars come from the snapshot
    array.

### Geometry & gate helpers — server

These change from "the anchor slot" to "any point's slot"
(`server/internal/game/zone_handlers.go`):

- `isClaimSlotCell(rt, cell)` → true when `cell` falls in **any** point's 2×2
  block (iterate `claimPointCells`).
- `claimTowerOnSlotLocked` → takes a point's top-left cell (or index), scans that
  point's 2×2 for a completed team tower. Used per-point by the evaluator.
- `claimSlotBuildableLocked` (build-gate exception in
  `state_buildings.go`) → buildable when `cell` is in **any** point's slot and
  (if `towerType` set) the building being placed is that tower. Claim stays
  standalone (no adjacent foothold required).
- `claimZoneTowerLocked(zoneID)` (enemy capture-trigger target,
  `state_waves.go`) → returns the first standing team tower among the
  **uncaptured** points in authored order, so capture-triggered defenders rush a
  point that is actively being built/defended. Deterministic (authored order, no
  RNG, no map iteration).

### Editor — MapEditorPanel.vue

- A new sub-mode for a selected claim zone: **"Place Capture Point"**. Clicking
  inside the zone appends the clicked cell to `selectedZone.claimPoints` (as a
  2×2 slot top-left); right-clicking a placed point removes it. Mirrors the
  existing `captureDraw` / `move` sub-mode wiring.
- The existing "Defend Duration" and "Tower Type" inputs remain (shared config).
- A claim zone with zero placed points falls back to the anchor slot, so the
  current single-slot authoring flow is unchanged for authors who place none.
- A short hint communicates that all placed points must be captured.

### Rendering — CanvasRenderer.ts

- The claim-slot highlight (currently one 2×2 at `zone.anchor`,
  ~`CanvasRenderer.ts:1878`) loops over `zone.claimPoints` (or `[zone.anchor]`
  fallback), drawing each 2×2 slot. Uncaptured points render as the cyan build
  slot; captured points render in the team color / checked state, keyed off the
  snapshot's per-point `captured` flag.
- The capture HUD shows "N/M points" for a multi-point claim zone. Per-point
  progress is read from `ZoneSnapshot.claimPoints[i].progress`. The existing
  top-of-screen single progress bar continues to work off `ZoneSnapshot.progress`
  (the max in-flight point).

### Tests — zone_runtime_test.go

- Two-point claim zone: captures only when **both** points have been defended to
  completion; capturing one leaves the zone neutral until the second completes.
- Per-point stickiness: after one point is captured, destroying its tower does
  not revert it, while the second point's timer still resets when its tower
  falls.
- Build gate: `claimSlotBuildableLocked` allows the tower on **every** placed
  point's slot and rejects non-slot cells / wrong building type.
- `claimZoneTowerLocked` returns a standing tower among uncaptured points and nil
  when none stand.
- Determinism: repeated replay of a multi-point capture yields identical
  per-point progress and owner.
- Existing single-point claim tests remain green via the anchor fallback (no
  edits needed to those cases).

## Out of scope (YAGNI)

- Per-point `defendSeconds` or `towerType`.
- Per-point owner / contestable ownership (a captured point is a team effort; a
  single `Captured` bool suffices).
- "Capture any M of N" thresholds — all placed points are required.
- Auto-generated slot layouts — slots are hand-placed only.

## Backward compatibility

The single new `Zone.claimPoints` field is optional and omitted from all
existing map JSON. An absent/empty list yields exactly one slot at the anchor —
byte-for-byte the current behavior. No catalog migration is required.
