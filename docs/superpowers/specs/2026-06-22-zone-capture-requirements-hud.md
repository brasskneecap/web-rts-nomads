# Zone Capture Requirements HUD + Ghost Tower

**Date:** 2026-06-22
**Status:** Design approved, pending implementation plan

## Problem

When a player moves units into a capturable zone, the game gives little
explanation of *what* is required to capture it. The only in-world feedback is
the zone outline, the presence capture sub-zone / claim build slots, and a single
top-of-screen progress bar. New players don't know they need to build a tower,
hold the area, clear enemies, etc., or why a zone they're standing in isn't being
captured (locked behind an adjacent zone, contested, etc.).

Two additions address this:

1. **Zone Capture Requirements panel** — a HUD panel below the objectives panel
   listing, for each zone the player's team currently occupies but doesn't own,
   the requirement to capture it plus live status.
2. **Ghost tower at claim capture points** — a translucent tower sprite drawn on
   each un-built claim capture point, so the player sees exactly what to build
   and where.

Both are **client-side presentation only** — no protocol or server changes. The
client already receives everything needed: per-tick `ZoneSnapshot`s
(owner, contested, progress, per-point `claimPoints`), the static
`MapConfig.zones` (capture type, cells, adjacency, claimPoints), the unit list
(positions + owners), and the building list.

## Feature A — Zone Capture Requirements panel

### Placement & visibility

A new component `ZoneCapturePanel.vue` mounted in the top-right HUD column in
`Match.vue`, **stacked directly below** the campaign objectives panel
(`MatchObjectivesPanel`). When there are no objectives (e.g. Custom Game), it
simply occupies the top of that column. The existing objectives anchor styling
(`.match-objectives-anchor`) is reused / extended so the two panels share the
column and gap.

The panel renders **one card per zone** that satisfies BOTH:
- the player's team has at least one live unit inside the zone's cells, AND
- the zone is **not** team-owned (still capturable).

This includes **locked** zones (adjacency gate unmet) so the card can explain why
no progress is happening. When no zone qualifies, the panel renders nothing
(`v-if` on a non-empty list), exactly like `MatchObjectivesPanel`.

Unlike the objectives panel (campaign-only), this panel is active on **any** map
that has zones — campaign or custom.

### Per-card content

Each card shows the zone name, a **requirement line** (how to capture, by type),
and a **live status line**. Capture types:

- **claim** — requirement `Build & defend {N} tower{s}` where N = number of
  capture points (`claimPoints.length`, or 1 for the anchor fallback). Status:
  `{held}/{N} points held`, where `held` counts `claimPoints[i].captured` from
  the snapshot; plus the most-advanced in-flight point's progress as a small bar
  (snapshot `progress`, already the max in-flight fraction).
- **presence** — requirement `Hold the zone`. Status: `Capturing… {pct}%` from
  the snapshot `progress`; replaced by `⚠ Contested!` when `snapshot.contested`;
  replaced by `🔒 Locked — capture an adjacent zone first` when the adjacency
  gate is unmet (see below).
- **clear** — requirement `Defeat all enemies in the zone`. Status: the count of
  hostile (enemy/neutral) units currently inside the zone, e.g. `3 enemies
  remain`, or `Clearing…` when unknown.
- **control_point** — requirement `Hold a structure on the point`. Status:
  whether a team structure occupies the anchor cell (`Structure held` /
  `No structure yet`).

A card carries a left accent in the zone's owner color
(`snapshot.ownerColor`, falling back to neutral grey). Styling matches the
parchment aesthetic of `MatchObjectivesPanel` (warm sepia, soft shadow).

### Locked (adjacency) computation — client mirror

A presence/clear/control_point zone is **capturable** when, mirroring the server
`zoneCapturableByLocked`:
- `adjacent` is empty ⇒ always capturable (ungated), OR
- `requireAllLinks` ⇒ every adjacent zone is team-owned, OR
- otherwise ⇒ any one adjacent zone is team-owned.

"Team-owned" for an adjacent zone id = its `ZoneSnapshot.owner` is the team
sentinel or a friendly player (not neutral/enemy/empty). Claim zones are
standalone (never locked). When a zone is occupied-by-me, unowned, and **not**
capturable, the card shows the Locked status.

### Data flow

A **pure builder function** `buildZoneCaptureCards(...)` in a new module
(`game/zones/zoneCaptureCards.ts`) walks `mapConfig.zones` and produces the
view-model array below. A computed in the match layer (`useGameClient` / the `ui`
view-model, alongside `ui.objectives`) calls it each tick and passes the result
as a prop to `ZoneCapturePanel.vue`. Keeping the builder pure (no DOM, no Vue) is
what makes it unit-testable (see Testing):

```ts
type ZoneCaptureCard = {
  id: string
  name: string
  type: 'claim' | 'presence' | 'clear' | 'control_point'
  requirement: string      // "Build & defend 2 towers"
  status: string           // "1/2 points held"
  state: 'progress' | 'contested' | 'locked' | 'idle'
  progress: number         // 0..1 for the bar (0 when n/a)
  ownerColor: string | null
}
```

Inputs: `GameState.zoneSnapshotsById`, `GameState.mapConfig.zones`,
`GameState.units`, `GameState.mapConfig.cellSize`, team membership
(`teamId` map + `localPlayerId`).

**Units-in-zone test:** for each zone, build a `Set` of cell keys from
`zone.cells`; a unit counts when it is alive, on the local team, and its grid
cell (`floor(unit.x / cellSize)`, `floor(unit.y / cellSize)`) is in the set.
Hostile-in-zone (for the clear count) uses the same membership test against
enemy/neutral owners. Cell sets are memoised per zone id (static geometry).

The component takes the `ZoneCaptureCard[]` as a prop, exactly like
`MatchObjectivesPanel` takes `objectives`.

## Feature B — Ghost tower at claim capture points

In `CanvasRenderer`'s existing claim-slot block (the per-point loop that draws
the cyan 2×2 build outline for an unowned claim zone), add: for each capture
point that is **not captured** (`snapshot.claimPoints[i]?.captured !== true`)
and has **no building currently occupying its slot**, draw the tower sprite as a
translucent "ghost":

- Sprite: `getBuildingSprite(towerType)` where `towerType` is the zone's
  configured claim tower type (`capture.config.towerType`, default `'Tower'`).
- Draw inside the point's 2×2 footprint at `ctx.globalAlpha ≈ 0.35`, beneath /
  within the existing cyan outline (which stays as the "build here" frame).
- Suppression: if a building (under construction or complete) already covers any
  cell of the point's slot, skip the ghost — the real building sprite already
  shows what's there. Building presence is tested against `mapConfig.buildings`
  by footprint overlap with the 2×2.
- The whole claim block is already hidden once the zone is team-owned, so ghosts
  disappear on full capture; per-point ghosts disappear as each point is
  captured or built.

`getBuildingSprite` may return `null` before the sprite image loads — when null,
skip the ghost for that frame (the cyan outline still shows). No new asset
loading is introduced; the tower sprite is already loaded for normal rendering.

## Out of scope (YAGNI)

- No protocol or server changes — all derivation is client-side from existing
  snapshot + map data.
- No timed status for clear / control_point (those mechanics have no capture
  timer); their cards show requirement + a simple binary/count status.
- The ghost uses the zone's single configured `towerType`; no per-point tower
  types.
- No new sprites/assets.

## Testing

- The card view-model builder is pure (inputs: zones, snapshots, units, cellSize,
  team) and unit-testable without a DOM: assert claim N/held counts, presence
  contested/locked/progress states, clear enemy counts, control_point structure
  state, and that owned or unoccupied zones produce no card. This is the primary
  automated coverage.
- Component render (`ZoneCapturePanel.vue`) and the canvas ghost are verified by
  typecheck + manual in-editor/in-match smoke (canvas + HUD aren't unit-tested in
  this codebase, matching existing convention).
