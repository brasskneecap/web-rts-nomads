# Neutral Spawns — Design

Status: Approved (brainstorming complete, ready for implementation planning)
Author: Cody (brainstormed with Claude)
Date: 2026-05-26

## Summary

Add a new "Neutral Spawn" placement type to the map editor. A neutral spawn is a tile-anchored spawner that, between waves, materializes a guard squad of "neutral" units hostile to the player. The squad despawns the instant a wave starts and respawns after the wave clears. Each spawn references a *group* — a named composition of unit types — drawn from a tier file in the catalog. Tier can scale up automatically every N waves.

Conceptually this is a hybrid of the two existing primitives:
- `enemy-spawnpoint` (a building-tile spawner with per-wave HP/damage scaling)
- `PlacedUnit` (a stationary guard with aggro/leash range, anchored to a position)

Neutrals reuse existing wave-faction unit defs (`raider`, `raider_ranged`, …) — no new unit catalog files are required. They're retagged at spawn time under a new virtual player slot `NeutralPlayerID = "neutral"`.

## Goals

1. Mapper places "Neutral Spawn" tiles in the map editor with the same ergonomics as enemy spawns.
2. Each placement picks a tier and either a specific group or "Random".
3. Tier files live at `server/internal/game/catalog/neutral_groups/tier_<N>.json` and contain multiple named groups.
4. Tier files are sparse: requesting tier K when only tier 1 and 3 exist resolves to tier 3 if K ≥ 3, else tier 1.
5. Neutrals despawn at wave start, respawn at wave end.
6. Per-placement scaling: aggro range, leash range, base HP/damage multipliers, and per-wave HP/damage multipliers (matches enemy-spawnpoint pattern).
7. Per-placement tier-up cadence ("Tier Up Every N Waves", 0 = off).
8. Engaging one neutral pulls the whole camp (group aggro).
9. Determinism preserved — all randomness uses the seeded `s.rng`.

## Non-Goals

- Authoring tier 2+ data files. The plumbing supports them; the data is deferred.
- Minimap icon, loot drops, or rewards for clearing a camp.
- Cross-faction interaction between neutrals and wave enemies (they are temporally disjoint by lifecycle design).

## Catalog: Neutral Group Definitions

**Location:** `server/internal/game/catalog/neutral_groups/tier_<N>.json` (sibling of `units/`, `buildings/`, `maps/`).

**Loaded by:** `server/internal/game/neutral_group_defs.go` at startup, built into `neutralGroupsByTier map[int]NeutralGroupTier`.

**Example — `tier_1.json`:**
```json
{
  "tier": 1,
  "groups": [
    {
      "id": "small_raider_group",
      "name": "Small Raider Group",
      "composition": [
        { "unitType": "raider", "count": 2 },
        { "unitType": "raider_ranged", "count": 2 }
      ]
    }
  ]
}
```

**Schema rules:**
- `tier` (int): redundant with the filename but kept for clarity / validation.
- `groups[].id` (string): stable key referenced by map JSON. Snake-case.
- `groups[].name` (string): display label in the editor dropdown.
- `groups[].composition[]`: `unitType` must resolve via `getUnitDef`; `count` is the number of that unit type to spawn (min 1).
- No per-group HP/damage overrides. Scaling lives on the placement, not the group, to keep one source of truth for tunables.

**Public API (loader):**
- `getNeutralGroup(tier int, id string) (NeutralGroup, bool)`
- `randomNeutralGroup(tier int, rng *rand.Rand) (NeutralGroup, bool)`
- `resolveTier(requested int) int` — returns the largest available tier ≤ requested; returns 0 (sentinel "no tiers loaded") if even tier 1 is missing.

## Map Editor: Placement Record & UI

**New brush mode** in [MapEditorPanel.vue](../../../client/src/game-portal/src/components/MapEditorPanel.vue): `neutral-spawn`.

**Placement record** (saved in `MapConfig`, new array `neutralSpawns: NeutralSpawn[]`):

```ts
interface NeutralSpawn {
  id: string;                        // auto-generated: `neutral-spawn-${x}-${y}`
  x: number;                         // grid coords
  y: number;
  groupId: string;                   // specific id or "__random__" sentinel
  startingTier: number;              // default 1
  tierUpEveryNWaves: number;         // 0 = never auto-scale
  aggroRange: number;                // default 150 (matches PlacedUnit)
  leashRange: number;                // default 200
  healthMultiplier: number;          // default 1.0
  healthMultiplierPerWave: number;   // default 0.0
  damageMultiplier: number;          // default 1.0
  damageMultiplierPerWave: number;   // default 0.0
}
```

**Editor panel UI:**

| Field | Control | Default |
|---|---|---|
| Starting Tier | Number input, min 1 | 1 |
| Tier Up Every N Waves | Number input, min 0 | 0 |
| Group | Dropdown: `Random` + all groups in `tier_<startingTier>.json` | `Random` |
| Aggro Range | Number input | 150 |
| Leash Range | Number input | 200 |
| Health Multiplier | Number input | 1.0 |
| Health Multiplier Per Wave | Number input | 0.0 |
| Damage Multiplier | Number input | 1.0 |
| Damage Multiplier Per Wave | Number input | 0.0 |

The Group dropdown is anchored to `startingTier` for preview purposes only. At runtime, "Random" rolls against the *current* tier (which may have advanced past `startingTier`).

**Frontend catalog fetch:** `GET /api/catalog/neutral-groups` returns `{ tiers: [{ tier, groups: [{id, name}] }] }`. Cached in the existing catalog Pinia store on editor mount.

## Runtime Lifecycle

New module: `server/internal/game/state_neutral_camps.go`.

### Runtime struct

```go
type NeutralCampState int

const (
    NeutralCampActive NeutralCampState = iota
    NeutralCampWaveHidden
)

type NeutralCamp struct {
    PlacementID    string   // matches the saved record id
    X, Y           int
    StartingTier   int
    TierUpEveryN   int
    GroupID        string   // "__random__" or specific id
    CurrentTier    int      // recomputed at each respawn
    AliveUnitIDs   []int    // ID-based, per AI_RULES
    State          NeutralCampState

    AggroRange     float64
    LeashRange     float64
    HealthMult     float64
    HealthMultPerWave float64
    DamageMult     float64
    DamageMultPerWave float64
}
```

Owned by `GameState` as `NeutralCamps []NeutralCamp` (or map keyed by `PlacementID` — implementation choice during planning).

### Tick integration

In `state.go`, insert one new call into the existing tick order:

```go
tickWaveLocked(dt)
tickNeutralCampsLocked()       // <-- new
tickEnemySpawnpointsLocked(dt, blocked)
```

`tickNeutralCampsLocked` is edge-triggered off `WaveManager` state transitions. No per-tick work in the steady state.

### Lifecycle transitions

| Wave transition | Action |
|---|---|
| Game start (first tick) | `spawnGroupForCampLocked(camp)` for every camp |
| `prep` → `active` (wave starts) | For each camp: despawn all `AliveUnitIDs` instantly via `removeUnitLocked`; clear ID list; set state = `wave-hidden` |
| Wave clears (`active` → next prep / `complete`) | For each camp: recompute `CurrentTier`, resolve group, `spawnGroupForCampLocked(camp)`, state = `active` |
| Unit death | `onUnitRemovedFromCampLocked(unitID)` hook in `removeUnitLocked` strips the dead unit's ID from its camp. No respawn until next wave clear. |

### `spawnGroupForCampLocked` flow

1. `tier := resolveTier(camp.CurrentTier)`. If `tier == 0`, log and skip (no tier files loaded).
2. Resolve the group:
   - If `camp.GroupID == "__random__"`, call `randomNeutralGroup(tier, s.rng)`.
   - Otherwise, `getNeutralGroup(tier, camp.GroupID)`. If missing, log and skip this respawn (camp stays empty until a future tier-up surfaces a valid group).
3. For each `{unitType, count}` in the composition, place `count` units in a small deterministic ring around `(camp.X, camp.Y)` (reuse existing helpers used by `tickEnemySpawnpointsLocked` when `spawnCount > 1`).
4. For each spawned unit:
   - `OwnerID = NeutralPlayerID`
   - `GuardMode = true`, `GuardAnchorX/Y = camp.X, camp.Y` (camp center, **not** each unit's own spawn offset)
   - `GuardAggroRange = camp.AggroRange`, `GuardLeashRange = camp.LeashRange`
   - `NeutralCampID = camp.PlacementID` (new field, see below)
   - Apply scaling — same formula as `state_spawn.go:671-681`:
     - `HP *= camp.HealthMult * (1 + camp.HealthMultPerWave * (waveNumber - 1))`
     - `Damage *= camp.DamageMult * (1 + camp.DamageMultPerWave * (waveNumber - 1))`
   - Append unit ID to `camp.AliveUnitIDs`.

### Tier resolution

`CurrentTier = StartingTier + floor(completedWaves / TierUpEveryN)` when `TierUpEveryN > 0`, else `StartingTier`. Computed fresh each respawn so live map edits take effect on the next save/reload.

`completedWaves` is read off `WaveManager` (current wave - 1, clamped ≥ 0).

### Determinism

- All RNG calls use `s.rng` (the seeded game RNG). No `math/rand`.
- Camps iterate in sorted `PlacementID` order during respawn.

### Dev-only invariant

In `tickNeutralCampsLocked`, assert that `len(camp.AliveUnitIDs) == 0` for every camp whenever `WaveManager.State == "active"`. Catches lifecycle bugs early.

## Combat Behavior

### Team / ownership

- New player slot constant: `NeutralPlayerID = "neutral"`. Virtual — no base, no resources, no defeat condition. Registered alongside `EnemyPlayerID` in the slot list.
- Neutrals have `OwnerID = NeutralPlayerID`, `Visible = true` (subject to normal fog-of-war).
- Existing AI scoring already filters by `target.OwnerID == unit.OwnerID` (per AI_RULES). Neutrals being a distinct owner means player units and neutrals naturally target each other with no special-case code.

### Guard behavior

- Each neutral uses the existing `GuardMode` machinery (`spawnPlacedEnemyUnitsLocked` in `state_spawn.go:300` is the reference implementation).
- Anchor is the **camp center**, not the unit's spawn ring position. The whole squad shares one leash radius and pacing point.

### Group aggro ("one-pulls-all")

- New field on `Unit`: `NeutralCampID string` (empty for non-neutral units).
- When a neutral unit acquires a player target through the normal aggro check, it broadcasts to all camp-mates (same `NeutralCampID`) and assigns the target via `ManualAttackTarget` semantics — except the validation guard from AI_RULES rule 3 still runs at point-of-use each tick:
  ```go
  target := s.getUnitByIDLocked(camp_mate.AttackTargetID)
  if target == nil || !target.Visible || target.HP <= 0 || target.OwnerID == camp_mate.OwnerID {
      // drop and resume guard
  }
  ```
- The broadcast resolves the target by ID and validates *before* assigning to camp-mates. No `*Unit` pointers stored anywhere.

### Neutral vs. wave-enemy interaction

Not a concern by construction. Neutrals only exist outside `active` waves; wave enemies only exist during `active` waves. Two populations, temporally disjoint. The `dev-only invariant` above guards against regression.

## Protocol Changes

`server/pkg/protocol/messages.go`:

```go
type NeutralSpawn struct {
    ID                       string  `json:"id"`
    X                        int     `json:"x"`
    Y                        int     `json:"y"`
    GroupID                  string  `json:"groupId"`
    StartingTier             int     `json:"startingTier"`
    TierUpEveryNWaves        int     `json:"tierUpEveryNWaves"`
    AggroRange               float64 `json:"aggroRange"`
    LeashRange               float64 `json:"leashRange"`
    HealthMultiplier         float64 `json:"healthMultiplier"`
    HealthMultiplierPerWave  float64 `json:"healthMultiplierPerWave"`
    DamageMultiplier         float64 `json:"damageMultiplier"`
    DamageMultiplierPerWave  float64 `json:"damageMultiplierPerWave"`
}

type MapConfig struct {
    // ... existing fields ...
    NeutralSpawns []NeutralSpawn `json:"neutralSpawns,omitempty"`
}
```

Backward compatibility: omitted `neutralSpawns` parses as an empty slice; existing maps continue to load unchanged.

## File Map

**New (server):**
- `server/internal/game/catalog/neutral_groups/tier_1.json` — example data
- `server/internal/game/neutral_group_defs.go` — loader + public API
- `server/internal/game/state_neutral_camps.go` — runtime lifecycle

**Changed (server):**
- `server/internal/game/state.go` — register `NeutralPlayerID`; insert `tickNeutralCampsLocked()` between wave tick and spawnpoint tick; wire `removeUnitLocked` to the camp-cleanup hook
- `server/internal/game/unit.go` — add `NeutralCampID string` field on `Unit`
- `server/pkg/protocol/messages.go` — add `NeutralSpawn` struct + field on `MapConfig`
- `server/internal/api/catalog.go` (or equivalent) — add `GET /api/catalog/neutral-groups` handler

**New / changed (client):**
- `client/src/game-portal/src/components/MapEditorPanel.vue` — `neutral-spawn` brush + config panel
- `client/src/game-portal/src/stores/catalog.ts` (or equivalent) — fetch + cache neutral-groups catalog
- Map-editor save/load round-trip — include `neutralSpawns` array

## Test Coverage (high-level)

To be elaborated in writing-plans.

**Catalog loader:**
- Parses tier_1.json correctly into the in-memory map.
- `resolveTier` returns the highest available tier ≤ requested.
- Missing `unitType` in composition surfaces an error at load time.
- Missing entire catalog (no tier files) is logged but doesn't crash startup.

**Lifecycle:**
- Spawn-at-game-start: camps spawn during initial tick.
- Despawn-on-wave-start: when `prep → active`, all neutral unit IDs are removed from `Units` map; `AliveUnitIDs` is cleared.
- Respawn-on-wave-end: when wave clears, fresh group spawns at camp.
- Tier-up cadence: with `TierUpEveryN=2`, `CurrentTier == StartingTier + 1` after wave 2 clears.
- Tier fallback in respawn: `StartingTier=4` resolves to whichever lower tier file exists.
- Dev-invariant: no alive neutrals while wave is active.

**Combat:**
- Player unit in aggro range → neutral attacks.
- Leash: neutral chases out of leash range → returns to camp center.
- One-pulls-all: aggro one neutral, all camp-mates target the same player unit (validated, not pointer-passed).

**Determinism:**
- Same seed + same map → identical group picks across two runs.

**Tunables hygiene** (project rule — no hardcoded balance numbers in tests):
- Expected HP/damage values are derived from the unit def JSON × the scaling formula, never pinned as literals.

## AI_RULES Compliance Summary

- All target storage is by ID: `AliveUnitIDs []int`, `NeutralCampID string`. No `*Unit` persisted across ticks.
- Every `getUnitByIDLocked` result is validated against the canonical `nil / HP / Visible / ownership` guard before use.
- `tickNeutralCampsLocked` is `Locked`-suffixed and runs inside the existing tick loop under `s.mu`.
- All RNG calls use `s.rng`; iteration order is sorted by `PlacementID`.
- The new `OwnerID = NeutralPlayerID` slot is registered alongside `EnemyPlayerID` so existing ownership-comparison checks Just Work.

## Out of Scope (Deferred)

- Tier 2+ data files (plumbing ready; data deferred).
- Minimap / fog-of-war special-casing — neutrals inherit the existing unit icon and FoW behavior.
- Loot drops, currency rewards, or XP gains for clearing a camp.
- Camp visual indicator on the map (e.g. a banner). May be added later as a pure-frontend layer.
