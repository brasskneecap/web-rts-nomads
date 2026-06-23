# Worker Advancements (Farm menu) — Design

Date: 2026-06-22

## Goal

Add an 8-node advancement track for the **worker** unit, surfaced in the **Farm**
menu of the kingdom view, mirroring the existing soldier/archer tracks shown in
the Barracks. Relocate the "extra starting worker" capability out of the Profile
Upgrades panel and into this track.

## The 8 nodes

Purchase order is left-to-right (each node requires the previous one), exactly
like soldier/archer. Costs mirror the soldier/archer curve (50/50/50, major 150,
100/100/100, major 300).

| # | Name (proposed) | Kind | Cost | Effect |
|---|-----------------|------|------|--------|
| 1 | Extra Hand | minor | 50 | Start each match with +1 worker |
| 2 | Quick Feet | minor | 50 | moveSpeed +20 |
| 3 | Thrifty Hire | minor | 50 | gold cost −15 |
| 4 | **Seasoned Lumberjacks** | **major** | 150 | woodGatherAmount +4 |
| 5 | Quick Feet II | minor | 100 | moveSpeed +20 |
| 6 | Thrifty Hire II | minor | 100 | gold cost −15 |
| 7 | Extra Hand II | minor | 100 | Start each match with +1 worker |
| 8 | **Gold Rush** | **major** | 300 | goldGatherAmount +5 |

Notes:
- You explicitly flagged #4 as the "big" upgrade → it's a `major` (square medal slot).
- To fully mirror soldier/archer, I also made the **capstone #8 a `major` (300 DP)**.
  If you'd rather #8 stay a minor (100 DP), say so.
- Worker base stats today: moveSpeed 100, goldGatherAmount 10, woodGatherAmount 6,
  resourceCost.gold 100. After the full track: moveSpeed 140, goldGather 15,
  woodGather 10, gold cost 70, and +2 starting workers.

## Backend changes

All in `server/internal/game`.

1. **New effect stats on `unitStatAdd`** (`advancement_defs.go`): extend the
   validator + applier `switch` to accept `"goldGatherAmount"`, `"woodGatherAmount"`,
   and `"goldCost"`.
   - `goldGatherAmount` / `woodGatherAmount` → `def.GoldGatherAmount` / `def.WoodGatherAmount` (direct int fields).
   - `goldCost` → `def.ResourceCost["gold"] += amount` (amount is **negative**, e.g. −15).
     **Map-copy guard:** `applyAdvancementsToEffectiveDefsLocked` shallow-copies the
     `UnitDef` struct, so `ResourceCost` (a map) is still shared with the catalog.
     The `goldCost` applier will replace `def.ResourceCost` with a fresh copy before
     mutating it, so we never corrupt the global catalog def.
   - Update the doc comment that enumerates valid stats.

2. **New effect kind `unitExtraStartingUnit`** (`advancement_defs.go`): grants
   `Amount` extra starting units of the node's own unit type. Like `unitExtraPerkSlot`,
   the `applyAtMatchStart(def, effect)` hook can't see the `Player`, so it's a no-op
   on the def and a **second pass** in `applyAdvancementsToEffectiveDefsLocked`
   populates `player.ExtraStartingUnits[node.UnitType] += effect.Amount`. This is the
   same map the existing `extraStartingUnit` profile upgrade fills, and it's read at
   `state.go:3195` right after advancements are applied (`state.go:3186`), so the
   extra worker spawns through the existing spawn-point grant path.

3. **New catalog file** `catalog/units/human/worker/advancements.json` with the 8
   nodes above (the loader auto-discovers it; no Go registration needed).

## Frontend changes

All in `client/src/game-portal/src`.

1. **`views/FarmView.vue`**: replace the empty `MetaSceneView` with a
   `UnitRosterScene` carrying a single worker entry (`paths: []`), mirroring
   `BarracksView.vue`. Clicking the worker portrait opens its advancement track in
   the same parchment popup the Barracks uses — no new UI component.

2. **`views/Advancements.vue`**: register `worker` in `PORTRAIT_MAP`,
   `UNIT_DISPLAY_ORDER`, and the `unitDisplayName` overrides; import the existing
   worker portrait at `assets/units/human/worker/portrait.png`.

3. **`components/profile/ProfileUpgradesPanel.vue`**: hide the `additional_worker`
   card so the extra-worker upgrade is now purchased only from the Farm track
   (relocating, not deleting — see below).

4. **`types/profile.ts`**: extend the `UnitAdvancementEffect` union if it strictly
   enumerates effect kinds (the panel only renders name/description/cost/kind, so no
   render-logic change is expected).

## What happens to `additional_worker` (the profile upgrade) — FULL HARD DELETE

Per your guidance ("as clean as possible; the Profile Upgrades page goes away
eventually"), `additional_worker` is removed entirely and the extra-worker
capability is fully reborn as worker advancement nodes #1 and #7.

- **Delete** `catalog/profile-upgrades/additional_worker.json`. The frontend Profile
  Upgrades panel lists from the catalog API, so the card disappears automatically —
  no panel edit needed.
- The generic `extraStartingUnit` profile-upgrade handler **stays** (it's reusable
  infra and the registry/validation tests still exercise the machinery via
  `physical_power`/`magic_power`); it simply has no catalog entry anymore.
- Repoint the tests that used `additional_worker` only as a fixture for machinery
  that isn't going away yet:
  - `server/internal/http/profile_upgrade_handlers_test.go` — repoint
    purchase/refund/toggle/list tests to `physical_power` (maxRanks 10,
    costPerRank [10..100]); seeded DP and rank expectations adjusted to the
    physical_power curve, and `additional_worker` removed from the list-response
    assertion.
  - `server/internal/profile/store_test.go` — v2→v3 migration fixture sample key
    changed from `additional_worker` to `physical_power` (migration is
    catalog-agnostic; this is just sample data).
  - `server/internal/game/spawn_point_unit_grants_test.go` — repoint the two
    `ExtraStartingUnits` spawn-anchor tests to grant via worker advancements
    `["worker_extra_1","worker_extra_2"]` (the 4th `EnsurePlayerWithUpgrades` arg),
    since that's the new extra-worker mechanism.
  - `server/internal/game/profile_upgrade_defs_test.go` — drop the
    `additional_worker` case from `…ThreeInitialDefsLoaded` (rename to Two…),
    delete `…AdditionalWorkerEffect`, change `…ListIsSortedByID` floor from `<3` to `<2`.
  - `server/internal/game/profile_upgrade_match_test.go` — delete
    `…AdditionalWorkerRank2_ExtraWorkers` (coverage moves to the new worker test).

## Testing

- Backend: a new `advancement_worker_test.go` — acquire all 8 worker nodes, run
  `applyAdvancementsToEffectiveDefsLocked`, and assert the effective worker def's
  moveSpeed / goldGather / woodGather / `ResourceCost["gold"]` and
  `ExtraStartingUnits["worker"]`. Per the "no hardcoded tunables" rule, **every**
  expected value is derived from `getUnitDef("worker")` plus the node effect amounts
  read back from the catalog (`GetAdvancementDef`), never pinned literals. Includes a
  regression assertion that the shared catalog `ResourceCost` map is **not** mutated
  (the map-copy guard).
- Frontend: manual smoke via the Farm menu (open worker, purchase chain).

## Out of scope

- No new worker portrait/art (the existing portrait is reused).
- No farm building changes (farm remains a non-spawner; this is meta-progression).
