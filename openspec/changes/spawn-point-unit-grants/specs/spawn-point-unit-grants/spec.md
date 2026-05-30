## ADDED Requirements

### Requirement: Generic spawn-point-anchored unit grant helper

The system SHALL expose a single server-side helper, `spawnUnitsForPlayerAtSpawnPointLocked(player, unitType, count)`, that spawns `count` units of the catalog-registered `unitType` for `player`, placed at the nearest walkable grid cell to the center of the `spawn-point` building associated with that player's claimed townhall. Association SHALL be resolved by walking `MapConfig.Buildings` and returning the first spawn-point whose `resolveSpawnPointTownhallLocked` result is owned by the player — the same townhall-association logic the rest of the engine uses (explicit `metadata.townhallId` when set, otherwise nearest townhall by distance); `metadata.playerLabel` SHALL NOT be required. The helper SHALL be the sole code path used by every "grant units to a player" feature; map-authored placed units and `spawn-point` `metadata.spawnUnits` are out of scope and retain their existing spawn pipelines. The helper SHALL be called only while `s.mu` is held.

#### Scenario: Helper spawns N units of the requested type at the spawn-point
- **WHEN** the helper is invoked with `(player, "worker", 3)` and the player has a `spawn-point` building at grid cell (10, 10) on a map where (10, 10) and its neighbors are walkable
- **THEN** three new worker units owned by `player` are added to the game state, each positioned at the world-space center of a walkable cell reachable from (10, 10) via `findNearestWalkable`

#### Scenario: Helper accepts any catalog unit type, not only "worker"
- **WHEN** the helper is invoked with `(player, "soldier", 1)` and `"soldier"` is registered in the unit catalog
- **THEN** one new soldier unit owned by `player` is spawned at the player's spawn-point

#### Scenario: Helper skips and warns when player has no spawn-point
- **WHEN** the helper is invoked for a player whose claimed townhall has no associated `spawn-point` building (no metadata.playerLabel match)
- **THEN** the helper SHALL log a warning identifying the player ID, unit type, and count, AND SHALL return without spawning any unit; the townhall position SHALL NOT be used as a fallback anchor

#### Scenario: Helper skips and warns when no walkable cell exists near the spawn-point
- **WHEN** the helper is invoked and `findNearestWalkable` returns no walkable cell within its search radius around the spawn-point center
- **THEN** the helper SHALL log a warning naming the player ID, unit type, and the spawn index that failed, AND SHALL continue with the next requested unit (one skipped spawn does not abort the remaining count)

### Requirement: Profile upgrades grant starting units via a generic per-type tally

The system SHALL track per-player extra-starting-unit grants as `Player.ExtraStartingUnits map[string]int`, keyed by unit type. The `extraStartingUnit` profile-upgrade effect handler SHALL add `rank * effect.CountPerRank` to the entry for `effect.UnitType` (creating the entry when absent). At match start, after authored placed units have spawned, the system SHALL iterate `ExtraStartingUnits` in sorted key order and call the generic spawn helper once per entry. The legacy field `Player.ExtraStartingWorkers` SHALL be removed.

#### Scenario: Rank 2 additional_worker grants two workers at the spawn-point
- **WHEN** a player joins a match owning the `additional_worker` profile upgrade at rank 2 and the map authors a player-class spawn-point for that player's slot
- **THEN** `player.ExtraStartingUnits["worker"]` equals 2 after upgrade application, AND two worker units are spawned at walkable cells adjacent to the player's spawn-point during `EnsurePlayerWithUpgrades`

#### Scenario: Multiple distinct unit-type grants from different upgrades accumulate independently
- **WHEN** a hypothetical second `extraStartingUnit` profile upgrade with `unitType: "soldier"` exists at rank 1 alongside `additional_worker` at rank 2, and the player owns both
- **THEN** `player.ExtraStartingUnits` equals `{"soldier": 1, "worker": 2}` and three total units (1 soldier + 2 workers) are spawned at the player's spawn-point at match start

#### Scenario: Spawn order is deterministic across runs
- **WHEN** `player.ExtraStartingUnits` contains more than one entry
- **THEN** the spawn loop visits entries in sorted key order, so the assigned unit IDs and the resulting blocked-cell occupancy are identical across re-runs with the same map and player join order

### Requirement: Authored map units continue to spawn unchanged

The system SHALL continue to spawn authored placed units (`MapConfig.PlacedUnits` whose `playerSlot` matches the player's label) and `spawn-point` authored units (`metadata.spawnUnits` on the `spawn-point` building) during match start without modification. The new helper SHALL NOT replace or duplicate either path.

#### Scenario: Authored placed unit still spawns at its authored cell
- **WHEN** a map authors a `PlacedUnits` entry with `playerSlot: "player1"`, `unitType: "worker"`, `x: 5`, `y: 5` and a player joins that slot
- **THEN** a worker is spawned at the nearest walkable cell to (5, 5), as today, regardless of whether the player also owns the `additional_worker` upgrade

#### Scenario: Spawn-point's metadata.spawnUnits continues to spawn at the spawn-point
- **WHEN** the player's spawn-point declares `metadata.spawnUnits: [{"unitType": "worker", "count": 3}]`
- **THEN** three workers spawn from the spawn-point via the existing spawn-point pipeline, independently of any profile upgrades

### Requirement: Wave-upgrade `spawnUnit` effect type

The system SHALL recognize a new wave-upgrade effect type `"spawnUnit"` on `UpgradeDef.Effect`. The effect SHALL declare `unitType string` (must reference a known unit catalog entry) and `count int` (must be > 0). At catalog load time, definitions whose effect type is `"spawnUnit"` SHALL be rejected with a startup panic when `unitType` is empty, when `unitType` is not in the unit catalog, or when `count <= 0`. When such an upgrade is picked by a player during a wave-end choice, `applyUpgradeLocked` SHALL call `spawnUnitsForPlayerAtSpawnPointLocked(player, effect.UnitType, effect.Count)`.

#### Scenario: Server panics on load when spawnUnit upgrade is malformed
- **WHEN** the server boots with an upgrade JSON declaring `effect: {"type": "spawnUnit", "unitType": "", "count": 1}`
- **THEN** the server panics at startup with a message naming the offending file and the empty `unitType`

#### Scenario: Server panics on load when spawnUnit references an unknown unit type
- **WHEN** the server boots with an upgrade JSON declaring `effect: {"type": "spawnUnit", "unitType": "ghost_pirate", "count": 1}` and no `ghost_pirate` exists in the unit catalog
- **THEN** the server panics at startup with a message naming the offending file and the unknown unit type

#### Scenario: Picking a spawnUnit upgrade spawns the unit at the player's spawn-point
- **WHEN** a player picks an upgrade with `effect: {"type": "spawnUnit", "unitType": "soldier", "count": 1}` during the wave-upgrade phase
- **THEN** one new soldier owned by that player is added to the game state at a walkable cell adjacent to the player's spawn-point, and the wave-upgrade phase resolves normally

#### Scenario: spawnUnit upgrade is `unlimited` and can be picked across consecutive waves
- **WHEN** a player picks `spawn_soldier_rare` (with `unlimited: true`) at wave 3 and the upgrade is offered again at wave 5 and picked again
- **THEN** both picks succeed, two soldiers are spawned in total, and `UpgradeStacks` is not incremented for the `unlimited` group

### Requirement: Initial wave-upgrade catalog for `spawnUnit`

The system SHALL ship two wave upgrades using the `spawnUnit` effect on first release of this change:

- `spawn_soldier_rare` — `rarity: "rare"`, `unlimited: true`, `scope: "army"`, `effect: {"type": "spawnUnit", "unitType": "soldier", "count": 1}`, distinct `group: "spawn_soldier"`.
- `spawn_archer_rare` — `rarity: "rare"`, `unlimited: true`, `scope: "army"`, `effect: {"type": "spawnUnit", "unitType": "archer", "count": 1}`, distinct `group: "spawn_archer"`. (Named `archer` because the unit catalog has no `ranger`; `archer` is the ranged human unit.)

Both definitions SHALL pass `loadUpgradeDefs` validation and SHALL appear in the offer pool for any wave whose rare weight is positive.

#### Scenario: Both new upgrades load successfully
- **WHEN** the server boots with `spawn_soldier_rare.json` and `spawn_archer_rare.json` under `catalog/upgrades/`
- **THEN** the server starts successfully and both definitions are retrievable via `getUpgradeDef("spawn_soldier_rare")` and `getUpgradeDef("spawn_archer_rare")`

#### Scenario: Both upgrades appear in the rare-weighted offer pool
- **WHEN** wave-upgrade offers are generated for a player at any wave where rare weight is non-zero and no upgrade has been previously taken
- **THEN** both `spawn_soldier_rare` and `spawn_archer_rare` are eligible to appear in the player's three-card offer
