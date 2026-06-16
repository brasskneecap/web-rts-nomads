# Capture-triggered spawns with selectable alliance

**Date:** 2026-06-16
**Status:** Approved design, pending implementation plan

## Problem

The map editor exposes a "Trigger Capture Zone" control on enemy spawnpoints
**and** a separate "Spawn Timing" dropdown (wave-based: Game Start / Always /
Specific Wave / Every Wave From / Every Nth Wave). These clash: when
`triggerCaptureZoneId` is set the server bypasses all wave gating
([state_waves.go:307-317](../../../server/internal/game/state_waves.go#L307)),
so the wave settings silently become dead code while still being editable.

Separately:

1. The current trigger fires while **any human unit occupies** the zone's
   capture region — not specifically while the zone is being captured.
2. Spawned units are **always** `__enemy__`
   ([state_spawn.go](../../../server/internal/game/state_spawn.go)). There is no
   way to make them `__neutral__`, so they always fight alongside/against the
   neutral camps as enemies and cannot be made to ignore neutrals.

## Goal

A spawnpoint mode where:

- Spawns activate **only while the linked zone's capture progress is actively
  advancing**, and stop the instant capture is contested, abandoned, or
  completed.
- The author can declare the spawned units as **enemy** or **neutral** aligned,
  so neutral-aligned defenders do not fight the neutral camps already on the
  map.
- The editor clash is removed structurally.

## Decisions (from brainstorming)

| Question | Decision |
|---|---|
| What "being captured" means | Capture **progress actively advancing** (timer ticking up; not contested/stalled/owned). |
| Clash model | A **new Spawn Timing mode** "While Zone Being Captured", mutually exclusive with the wave modes. |
| Alliance option scope | New `spawnAlliance` field on **every** spawnpoint, default `enemy`. |
| Existing `triggerCaptureZoneId` | **Repurpose** to the new "being captured" semantics; migrate existing maps. |
| Invalid zone (clear/control_point) | Editor zone picker **lists only presence/claim** zones (timed-capture). |

## Design

### 1. Map data — `metadata` on an `enemy-spawnpoint`

- `triggerCaptureZoneId` (string, zone id) — **repurposed**. Presence of this
  field selects the "While Zone Being Captured" Spawn Timing mode. Semantics
  change from "while a human occupies the region" to "while the zone's capture
  progress is advancing". Mutually exclusive with the wave fields
  (`gameStart`, `waveNumber`, `startingWave`, `waveInterval`): the editor clears
  one set when the other is chosen.
- `spawnAlliance` (string, `"enemy"` | `"neutral"`, default `"enemy"`) — **new**.
  Applies to all spawnpoints regardless of timing mode. Absent ⇒ `enemy`
  (backward compatible).

### 2. Server — "is this zone being captured?" signal

- Add `Capturing bool` to `zoneRuntime`
  ([zone_defs.go](../../../server/internal/game/zone_defs.go)). Reset to
  `false` at the top of each zone's evaluation in `tickZonesLocked`, exactly
  like the existing `Contested` flag
  ([zone_runtime.go:211-213](../../../server/internal/game/zone_runtime.go#L211)).
- The **presence** mechanic sets `rt.Capturing = true` on the tick it executes
  `rt.Progress += dt` (the uncontested, capturable, not-yet-owned branch —
  [zone_handlers.go:169-176](../../../server/internal/game/zone_handlers.go#L169)).
- The **claim** mechanic sets `rt.Capturing = true` on the tick it executes
  `rt.Progress += dt` (tower present & defending —
  [zone_handlers.go:284-288](../../../server/internal/game/zone_handlers.go#L284)).
- **clear** and **control_point** have no timed progress and never set the flag.
- New helper `zoneCapturingLocked(zoneID string) bool` in `zone_runtime.go`
  (mirrors `captureZoneOccupiedByHumanLocked`, which it replaces at the spawn
  call site). Returns false for unknown zones.

### 3. Server — spawn gate

In `tickEnemySpawnpointsLocked`
([state_waves.go:307-317](../../../server/internal/game/state_waves.go#L307)),
the `hasTrigger` branch switches its condition from
`captureZoneOccupiedByHumanLocked(triggerZoneID)` to
`zoneCapturingLocked(triggerZoneID)`. When not capturing → re-arm the interval
and `continue` (unchanged structure). The zone is captured/contested/abandoned ⇒
flag is false ⇒ spawns stop.

**Tick ordering / 1-tick lag.** Spawnpoints tick
([state.go:2479](../../../server/internal/game/state.go#L2479)) **before** zones
([state.go:2891](../../../server/internal/game/state.go#L2891)). The spawn gate
therefore reads the `Capturing` value computed during the **previous** tick — a
deterministic ≤1-tick lag (≤50 ms). This is acceptable and avoids reordering the
tick loop (other systems read zone owner state mid-tick at the current
position). No reorder is performed.

### 4. Server — alliance-aware spawning

In the spawn loop
([state_waves.go:460](../../../server/internal/game/state_waves.go#L460)), read
`spawnAlliance` from metadata once per spawnpoint and select the spawner:

- `"neutral"` ⇒ `spawnNeutralUnitLocked(unitType, spawnPos)`
- otherwise ⇒ `spawnEnemyUnitLocked(unitType, spawnPos)` (unchanged default)

All downstream handling (wave stat scaling, `OrderID`, targeting/pathing to the
nearest player townhall, objective seeding) is identical for both. Because
neutral-aligned units share the `__neutral__` owner, the combat AI treats them
as friendly to the neutral camps (no infighting) and hostile to players
(they still advance and attack). Camp despawn only removes units carrying a
`NeutralCampID`
([state_neutral_camps.go:157-174](../../../server/internal/game/state_neutral_camps.go#L157)),
which spawnpoint units do not have, so they are **not** culled on wave
transitions.

### 5. Editor UI ([MapEditorPanel.vue](../../../client/src/game-portal/src/components/MapEditorPanel.vue))

- **Spawn Timing dropdown**: add option **"While Zone Being Captured"**.
  - `editWaveMode` computed: return `'capture'` when `triggerCaptureZoneId` is
    present (checked before the wave-field checks).
  - `updateEditWaveMode`: when switching **to** `capture`, clear
    `gameStart`/`waveNumber`/`startingWave`/`waveInterval`; when switching to any
    wave mode, delete `triggerCaptureZoneId`. Guarantees mutual exclusion.
  - When `capture` is selected: hide the wave-number field, show a **zone
    picker** populated only with zones whose `capture.type` is `presence` or
    `claim`. Selecting a zone sets `triggerCaptureZoneId`.
- **Spawn Alliance selector**: a new always-visible `Enemy / Neutral` dropdown
  for spawnpoints, bound to `spawnAlliance` (default `enemy`).
- Remove the standalone "Trigger Capture Zone" control (folded into the timing
  dropdown).

### 6. Migration

Existing `triggerCaptureZoneId` usages adopt the new "being captured"
semantics. The only in-repo usage is `forest-1.json` (`zone-3`). During
implementation, review that spawnpoint + zone-3 to confirm the new semantics
read sensibly; adjust the map if needed. No schema migration code is required
(the field name is unchanged).

## Testing

Server (Go, table/scenario tests alongside `zone_runtime_test.go` /
`wave_interval_test.go`):

- `Capturing` is true exactly on ticks where presence/claim progress advances;
  false when contested, empty, already-owned, or for clear/control_point zones.
- Spawn gate fires only while `zoneCapturingLocked` is true; re-arms interval and
  stops when capture completes or is interrupted.
- `spawnAlliance:"neutral"` yields units with `OwnerID == __neutral__`; they do
  not target neutral camp units and survive a wave transition (not despawned).
- `spawnAlliance` absent / `"enemy"` is unchanged from current behavior.

Per project test rules: derive expected values from catalog/config, assert
invariants (owner ids, flag transitions) rather than pinning balance numbers.

## Out of scope

- Anchoring/guarding behavior for capture defenders (they reuse the existing
  advance-to-nearest-townhall targeting; the capturing player is in/near the
  zone, so engagement happens there).
- Any change to clear/control_point mechanics or to neutral camp AI.
- AND-combining wave timing with capture triggering.

## Risk notes

- **1-tick lag** on the capture signal (documented above) — accepted.
- **Repurposed semantics** change existing-map behavior — accepted by the
  author; forest-1 to be reviewed during implementation.
- **Neutral spawnpoint units are a new kind of `__neutral__` unit** (no camp).
  Implementation must confirm no other system assumes all `__neutral__` units
  belong to a camp (e.g. metrics, loot, FoW). Covered by the wave-transition
  survival test.
