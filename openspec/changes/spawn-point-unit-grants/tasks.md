## 1. Generalize `Player` storage for extra starting units

- [x] 1.1 In `server/internal/game/state.go`, replace the `ExtraStartingWorkers int` field on `Player` with `ExtraStartingUnits map[string]int` (with its comment updated to describe the per-unit-type tally).
- [x] 1.2 In `EnsurePlayerWithUpgrades` (same file), initialize `ExtraStartingUnits` to a non-nil empty map at the same point the other maps (`UnitSpawnTimeMultipliers`, etc.) are initialized.

## 2. Update the `extraStartingUnit` profile-upgrade effect handler

- [x] 2.1 In `server/internal/game/profile_upgrade_defs.go`, change the `extraStartingUnit` handler's `applyAtMatchStart` to write `player.ExtraStartingUnits[effect.UnitType] += rank * effect.CountPerRank` (creating the key on first write). Add a guard that initializes the map if it is nil to keep the handler safely callable from tests.
- [x] 2.2 Update `server/internal/game/profile_upgrade_match_test.go` to assert on `ExtraStartingUnits["worker"]` instead of `ExtraStartingWorkers`. Keep the existing zero-value coverage as a check that an unowned upgrade leaves the map empty (no `"worker"` key).

## 3. Add the generic spawn helper and the spawn-point lookup

- [x] 3.1 In `server/internal/game/state_spawn.go`, add `findPlayerSpawnPointLocked(playerID string) *protocol.BuildingTile`. Walk `s.MapConfig.Buildings`, match on `BuildingType == "spawn-point"`, resolve the spawn-point's townhall via `resolveSpawnPointTownhallLocked`, and return the spawn-point when that townhall's `OwnerID` matches the player. `metadata.playerLabel` is NOT required — maps without authored labels (e.g. `enemy-test-small`) still pair each spawn-point with the nearest townhall via `resolveSpawnPointTownhallLocked`'s distance fallback.
- [x] 3.2 In the same file, add `spawnUnitsForPlayerAtSpawnPointLocked(player *Player, unitType string, count int)`. Resolve the spawn-point via 3.1; log a `slog.Warn` and return when nil. Compute the spawn-point world center from `(X + Width/2) * CellSize`, `(Y + Height/2) * CellSize`. Snapshot `blocked` once. Loop `count` times calling `findNearestWalkable` → `gridToWorldCenter` → `spawnPlayerUnitLocked(unitType, player.ID, player.Color, spawnPos)`. Log a per-iteration warning and continue when `findNearestWalkable` or `spawnPlayerUnitLocked` fails for one index. Guard against `player == nil`, `count <= 0`, and an empty `unitType` by logging and returning early.

## 4. Replace the townhall-anchored start-of-match spawn call

- [x] 4.1 In `server/internal/game/state_spawn.go`, delete `spawnExtraStartingWorkersLocked`.
- [x] 4.2 In `server/internal/game/state.go` `EnsurePlayerWithUpgrades`, remove the `spawnExtraStartingWorkersLocked(player, townhall, color)` call. Replace it with a loop that gathers the keys of `player.ExtraStartingUnits`, sorts them with `sort.Strings`, and for each key calls `s.spawnUnitsForPlayerAtSpawnPointLocked(player, unitType, count)`.
- [x] 4.3 Drop the now-unused `townhall` argument from any private call paths that fed only this function, where present. (No additional private call paths existed; the value is still used by the downstream `player joined match slot` diagnostic log.)

## 5. Add the `spawnUnit` wave-upgrade effect type

- [x] 5.1 In `server/internal/game/upgrade_defs.go`, add `const upgradeEffectTypeSpawnUnit = "spawnUnit"`. Add `UnitType string `json:"unitType,omitempty"`` and `Count int `json:"count,omitempty"`` to `UpgradeEffect` (alongside the existing fields).
- [x] 5.2 In the load-time `switch def.Effect.Type` in `loadUpgradeDefs`, add a `case upgradeEffectTypeSpawnUnit` branch that requires `def.Effect.UnitType != ""`, `getUnitDef(def.Effect.UnitType)` returns ok, and `def.Effect.Count > 0`. Panic with a descriptive message naming the offending file on any failure.
- [x] 5.3 In `server/internal/game/upgrade_apply.go` `applyUpgradeLocked`, add `case upgradeEffectTypeSpawnUnit:` that calls `s.spawnUnitsForPlayerAtSpawnPointLocked(player, def.Effect.UnitType, def.Effect.Count)`. Place the case before the `default` (stat) branch so it doesn't fall through to stat application.

## 6. Ship the two new wave-upgrade JSONs

- [x] 6.1 Create `server/internal/game/catalog/upgrades/spawn_soldier_rare.json` with `id: "spawn_soldier_rare"`, `group: "spawn_soldier"`, `name: "Recruit: Soldier"`, `description: "Spawn 1 soldier at your spawn point."`, `rarity: "rare"`, `scope: "army"`, `effect: { "type": "spawnUnit", "unitType": "soldier", "count": 1 }`, `unlimited: true`. (Confirmed `"soldier"` is in the catalog at `catalog/units/human/soldier/`.)
- [x] 6.2 Create `server/internal/game/catalog/upgrades/spawn_archer_rare.json` (the unit catalog has `archer`, not `ranger`) with `id: "spawn_archer_rare"`, `group: "spawn_archer"`, `name: "Recruit: Archer"`, `description: "Spawn 1 archer at your spawn point."`, `unitType: "archer"`, otherwise identical to 6.1.

## 7. Tests

- [x] 7.1 Added `TestExtraStartingUnits_SpawnsNearSpawnPoint` in `server/internal/game/spawn_point_unit_grants_test.go`. It exercises the default map (`enemy-test-small`, which has unlabelled spawn-points spatially associated with townhalls), claims a player with `additional_worker` rank 2, then asserts the two highest-ID workers are closer (or equal) to the spawn-point center than to the townhall center.
- [x] 7.2 Added `TestExtraStartingUnits_NoSpawnPointWarnsAndSkips` in the same file. Strips every spawn-point from `MapConfig.Buildings` before joining the upgrade-owning player; asserts the player's unit count matches the no-upgrade baseline (i.e. nothing extra was spawned). The warning is emitted via `slog.Warn` (visible in test output); the count assertion is the regression-safe gate.
- [x] 7.3 Added `TestWaveUpgrade_SpawnUnit_PicksSpawnUnitAtSpawnPoint` (table-driven for `spawn_soldier_rare` and `spawn_archer_rare`). Calls `applyUpgradeLocked` directly to avoid having to set `WaveManager.State` and `CurrentOffers`; asserts +1 unit per pick, and that `UpgradeStacks` is unchanged across two picks (because the upgrades are `unlimited: true`).
- [ ] 7.4 **Deferred.** `loadUpgradeDefs` does its validation inline in a single function. Unit-testing the panic paths would require extracting a per-def validator and re-injecting an in-memory FS — a refactor outside the spec scope. The runtime safety net is: `go build` exercises the loader at init, and any malformed `spawnUnit` JSON panics the server before it serves traffic. The two shipped JSONs are exercised by 7.3, which is the meaningful coverage.
- [x] 7.5 Ran `go test ./server/internal/game/... -count=1`. Only failure is `TestWaveStatBuffs_SpawnedUnitReceivesStackedMultipliers`, which was already failing on `main` pre-change (verified by stashing this change and re-running). All other tests, including the renamed-field `TestProfileUpgrade_NoUpgrades_DefaultMultipliers` and `TestProfileUpgrade_AdditionalWorkerRank2_ExtraWorkers`, pass.

## 8. Reconciliation notes

- [x] 8.1 Note in the PR description that the in-flight `legend-points-upgrade-system` change references `ExtraStartingWorkers` in its design.md / spec.md text; whichever change archives second must rewrite those references to `ExtraStartingUnits["worker"]` so the archived spec matches the merged code. (Captured in proposal.md Impact section and here in tasks.md; will be added to the PR description at commit time.)
