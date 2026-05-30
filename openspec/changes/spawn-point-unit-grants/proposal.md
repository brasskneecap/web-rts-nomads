## Why

The `additional_worker` profile upgrade currently spawns extra workers at the **townhall center**, not at the player's spawn-point, and stores its count in a worker-specific `Player.ExtraStartingWorkers int` field. Both choices block the upgrade catalog from growing: any future "extra starting soldier" or "extra starting ranger" upgrade would need a new field, a new spawn loop, and an arbitrary anchor.

This change introduces one generic, spawn-point-anchored helper that any "grant a unit to a player" source can call — profile upgrades at match start today, wave upgrades mid-match next — so unit-granting features can be added with catalog JSON alone.

## What Changes

- Add a generic server-side helper `spawnUnitsForPlayerAtSpawnPointLocked(player, unitType, count)` that resolves the player's authored `spawn-point` building, finds the nearest walkable cell to it, and spawns `count` units of `unitType` for that player. When the player has no assigned spawn-point, the helper SHALL log a warning and spawn nothing (no townhall fallback).
- **BREAKING (internal):** Replace `Player.ExtraStartingWorkers int` with `Player.ExtraStartingUnits map[string]int` keyed by `unitType`. The `extraStartingUnit` profile-upgrade effect handler SHALL write `player.ExtraStartingUnits[effect.UnitType] += rank * effect.CountPerRank`. The match-start spawn loop SHALL iterate the map in sorted key order and call the new helper.
- Change the `additional_worker` upgrade's spawn anchor from townhall center to the player's spawn-point. Behavior is otherwise unchanged: rank 1 grants 1 worker, rank 2 grants 2 workers, spawned during `EnsurePlayerWithUpgrades` after authored placed units.
- Add a new wave-upgrade effect type `spawnUnit` (`{"type": "spawnUnit", "unitType": "<id>", "count": <int>}`) handled in `applyUpgradeLocked`. When picked, the effect calls the same generic helper to spawn the unit at the player's spawn-point.
- Ship two new wave upgrades using the new effect:
  - `spawn_soldier_rare` — rare-tier, `unlimited: true`, spawns 1 soldier on pick.
  - `spawn_ranger_rare` — rare-tier, `unlimited: true`, spawns 1 ranger on pick.
- Authored map "spawn units" (the `spawn-point` building's `metadata.spawnUnits`) and all other authored placed units continue to spawn unchanged — this change does not touch the existing map-driven spawn path.

## Capabilities

### New Capabilities
- `spawn-point-unit-grants`: generic catalog-driven path for granting units to a player at their assigned spawn-point, used by profile upgrades at match start and wave upgrades mid-match.

### Modified Capabilities
<!-- profile-upgrades exists as a spec only inside the in-flight legend-points-upgrade-system change; it has not been archived to openspec/specs/. The touchpoints with that change (renaming ExtraStartingWorkers, re-anchoring the additional_worker spawn) are captured in Impact below and in tasks.md, not as a delta spec. -->

## Impact

- **Server (Go):**
  - `server/internal/game/state.go` — replace `Player.ExtraStartingWorkers int` with `Player.ExtraStartingUnits map[string]int`; initialize the map in `EnsurePlayerWithUpgrades`; replace the `spawnExtraStartingWorkersLocked` call site with a loop that iterates the map and calls the new helper.
  - `server/internal/game/state_spawn.go` — remove `spawnExtraStartingWorkersLocked` (townhall-anchored), add `spawnUnitsForPlayerAtSpawnPointLocked` (spawn-point-anchored). Add a private helper that returns the player's assigned `spawn-point` `BuildingTile` (looked up via the same `playerLabel` mechanism `findPlayerLabelLocked` already uses).
  - `server/internal/game/profile_upgrade_defs.go` — change the `extraStartingUnit` handler's `applyAtMatchStart` to write into `ExtraStartingUnits[effect.UnitType]` instead of `ExtraStartingWorkers`.
  - `server/internal/game/profile_upgrade_match_test.go` — update the assertion that reads `ExtraStartingWorkers` to read the new map.
  - `server/internal/game/upgrade_defs.go` — add `upgradeEffectTypeSpawnUnit = "spawnUnit"`; extend the load-time switch to require `effect.UnitType` (must be in unit catalog) and `effect.Count > 0`.
  - `server/internal/game/upgrade_apply.go` — add a new case in `applyUpgradeLocked`'s switch for `upgradeEffectTypeSpawnUnit` that calls the shared helper.
  - `server/internal/game/catalog/upgrades/spawn_soldier_rare.json`, `spawn_ranger_rare.json` — new files.
  - Existing in-flight change `legend-points-upgrade-system` references `ExtraStartingWorkers`; its `additional_worker.json` and effect handler are updated by this change. The two changes will need to be archived in order, with the later one inheriting the renamed field.
- **Client (Vue/TS):** no required changes. The two new wave upgrades render through the existing `WaveUpgradeModal.vue` card pipeline (name + description + rarity).
- **No invariant changes:** no new `*Unit` or `*BuildingTile` fields on tick-loop structs; the helper takes IDs / player handles and returns nothing. Spawn is gated by the same `s.mu` lock all spawn paths already hold.
