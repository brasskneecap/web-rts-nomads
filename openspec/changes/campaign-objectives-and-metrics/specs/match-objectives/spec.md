## ADDED Requirements

### Requirement: Generic objective definition schema

The system SHALL load objective definitions from each campaign level's `objectives` array in `server/internal/game/catalog/campaigns/<id>.json`. Each definition SHALL declare an `id` (unique within the level), a `type` string that names a registered objective handler, a human `description`, a `scope` value of `"team"` or `"player"`, a `required` boolean, and a typed `config` object whose shape is dictated by the handler for the declared type. The catalog loader SHALL panic at server startup with a message naming the campaign file, the level id, and the offending objective id if any of: the `type` is not registered; `config` fails handler validation; `scope` is present but not one of the two allowed values; `id` collides with another objective in the same level.

#### Scenario: Valid objective definition loads
- **WHEN** a campaign level JSON declares an objective with type `kill_camps`, scope `team`, required `true`, and config `{count: 3}`
- **THEN** the catalog loads without error and the objective is retrievable from the level's `Objectives` slice with its declared scope and required flag

#### Scenario: Unknown objective type panics at startup
- **WHEN** a campaign level JSON declares an objective with type `fly_to_moon` for which no handler is registered
- **THEN** the server panics during catalog load with a message naming the campaign file, the level id, the objective id, and the unknown type

#### Scenario: Duplicate objective id within a level panics
- **WHEN** a campaign level JSON declares two objectives with the same `id`
- **THEN** the server panics during catalog load with a message naming the campaign file, the level id, and the duplicate id

#### Scenario: Default scope is team
- **WHEN** a campaign level JSON declares an objective with no `scope` field
- **THEN** the loaded objective's scope equals `team`

#### Scenario: Default required is false
- **WHEN** a campaign level JSON declares an objective with no `required` field
- **THEN** the loaded objective's required flag equals `false`

### Requirement: Objective handler registry

The system SHALL expose a `map[string]objectiveHandler` registry keyed by the objective type string. Each handler SHALL provide three functions: a `parseConfig` that converts a raw JSON config into a typed config struct, a `validate` that panics on invalid config values, and an `evaluate` that updates a mutable `ObjectiveState`. Registering a new objective type SHALL require only adding a single entry to the registry; no existing call site in the loader or tick path SHALL need to change.

#### Scenario: Registry exposes all initial handlers
- **WHEN** the server boots
- **THEN** the registry contains handlers for `kill_camps`, `build_buildings`, `collect_resource`, `kill_camps_before_wave`, `rank_units`, and `survive_waves`

#### Scenario: New handler is reachable without engine changes
- **WHEN** a future change registers a handler with a new type key
- **THEN** campaign JSONs can reference that type and the catalog loader resolves it without modification to the loader, evaluator, or snapshot serialiser

### Requirement: Initial objective type — `kill_camps`

The `kill_camps` handler SHALL accept a config of `{campTier?: int, count: int}`. `count` MUST be greater than zero; `campTier` if present MUST be greater than or equal to one. Evaluation SHALL read `MatchMetrics.NeutralCampsKilled` when `campTier` is omitted, and `MatchMetrics.NeutralCampsKilledByTier[campTier]` otherwise. The objective SHALL complete when the read value is greater than or equal to `count`.

#### Scenario: Counts any tier when campTier is omitted
- **WHEN** an objective `{count: 3}` is being evaluated and the player has cleared 1 tier-1 camp and 2 tier-2 camps
- **THEN** the objective is completed because the team's total camps cleared is 3

#### Scenario: Filters to declared tier
- **WHEN** an objective `{campTier: 1, count: 3}` is being evaluated and the player has cleared 2 tier-1 camps and 4 tier-2 camps
- **THEN** the objective is not completed because only 2 tier-1 camps have been cleared

### Requirement: Initial objective type — `build_buildings`

The `build_buildings` handler SHALL accept a config of `{buildingType: string, count: int}`. `count` MUST be greater than zero; `buildingType` MUST match a known entry in the building catalog (validation runs at startup). Evaluation SHALL read `MatchMetrics.BuildingsBuiltByType[buildingType]`. The objective SHALL complete when the read value is greater than or equal to `count`.

#### Scenario: Player-scope build objective tracks each player independently
- **WHEN** an objective `{buildingType: "barracks", count: 1}` with `scope: "player"` is in play and Player A has built a barracks but Player B has not
- **THEN** Player A's view shows the objective complete while Player B's view shows it incomplete

### Requirement: Initial objective type — `collect_resource`

The `collect_resource` handler SHALL accept a config of `{resource: "gold" | "wood", amount: int}`. `amount` MUST be greater than zero; `resource` MUST be exactly one of the two allowed literals (any other value panics at startup). Evaluation SHALL read `MatchMetrics.TotalGoldEarned` or `MatchMetrics.TotalWoodEarned` according to the declared resource. The objective SHALL complete when the read value is greater than or equal to `amount`.

#### Scenario: Cumulative tracking ignores spending
- **WHEN** an objective `{resource: "gold", amount: 500}` is being evaluated, the player has earned 600 gold via deposits, and the player has spent 400 gold on buildings
- **THEN** the objective is completed because the cumulative-earned counter is 600

### Requirement: Initial objective type — `kill_camps_before_wave`

The `kill_camps_before_wave` handler SHALL accept a config of `{campTier?: int, count: int, beforeWave: int}`. `count` and `beforeWave` MUST each be greater than zero. Evaluation SHALL match the `kill_camps` read pattern for camps killed. The objective SHALL complete when the read value is greater than or equal to `count`. The objective SHALL fail when, at evaluation time, `state.Completed` is `false`, `GameState.WaveManager.CurrentWave` is greater than or equal to `beforeWave`, and `GameState.WaveManager.State` equals `"active"`.

#### Scenario: Completing before the deadline locks the win
- **WHEN** an objective `{count: 3, beforeWave: 5}` is in play, the player clears the third camp during wave 4 preparation, and the wave clock subsequently reaches wave 5 active
- **THEN** the objective state is `completed: true, failed: false` and the active-wave check does not flip the failed bit

#### Scenario: Missing the deadline fails permanently
- **WHEN** an objective `{count: 3, beforeWave: 5}` is in play, the player has only cleared 2 camps when wave 5 transitions to active, and the player later clears the third camp
- **THEN** the objective state was set to `failed: true` at the wave-5 transition and remains `failed: true` after the third camp is cleared

### Requirement: Initial objective type — `rank_units`

The `rank_units` handler SHALL accept a config of `{rank: "bronze" | "silver" | "gold", count: int}`. `count` MUST be greater than zero; `rank` MUST be exactly one of the three allowed literals. Evaluation SHALL read `MatchMetrics.UnitsByRank[rank]` which represents the count of units currently alive at that rank or higher. The objective SHALL complete when the read value is greater than or equal to `count`. The handler SHALL be documented as "currently-at-or-above-rank" semantics rather than cumulative rank-ups.

#### Scenario: Counts current rank-or-higher units
- **WHEN** an objective `{rank: "bronze", count: 5}` is in play and the player has 3 bronze units, 1 silver unit, and 1 gold unit currently alive
- **THEN** the objective is completed because 5 units are at bronze or higher

#### Scenario: Losing units below the threshold reverses progress before completion
- **WHEN** an objective `{rank: "bronze", count: 5}` is in play, the player has reached 5 bronze units, all 5 are killed before completion is detected, and the objective state has not yet been marked completed
- **THEN** the objective's `current` value falls back to 0 and the objective is still incomplete

#### Scenario: Completion is sticky once observed
- **WHEN** an objective `{rank: "bronze", count: 5}` is in play and at some point in time the player has had 5 bronze units alive, triggering completion in a prior tick
- **THEN** the objective remains `completed: true` even if all bronze units die in a later tick

### Requirement: Initial objective type — `survive_waves`

The `survive_waves` handler SHALL accept a config of `{wavesToSurvive: int}`. `wavesToSurvive` MUST be greater than zero. Evaluation SHALL read `MatchMetrics.WavesCleared`. The objective SHALL complete when the read value is greater than or equal to `wavesToSurvive`. The handler exists so that wave-completion victory conditions migrated from the legacy `surviveWaves` map condition flow through the registry from day one; authoring it as `required: true` is how a campaign level expresses a wave-completion win condition through the objective system. The legacy wave/townhall victory rule continues to AND with this — see the victory-rule requirement under the `campaign-progression` capability.

#### Scenario: Wave count threshold completes the objective
- **WHEN** an objective `{wavesToSurvive: 3}` is being evaluated and the team has just cleared its third wave (`MatchMetrics.WavesCleared == 3`)
- **THEN** the objective is completed

#### Scenario: Pre-threshold wave clears do not complete
- **WHEN** an objective `{wavesToSurvive: 5}` is being evaluated and the team has cleared 4 waves
- **THEN** the objective state is `current: 4, completed: false`

### Requirement: Per-player match metrics struct

The system SHALL track per-player match metrics on a `MatchMetrics` value carried by the `Player` struct. The struct SHALL include cumulative counters for gold earned, wood earned, enemies killed, buildings built (total and by building type), neutral camps killed (total and by tier), units trained (total and by unit type), waves cleared; and a derived map of units currently alive by rank. All maps SHALL be initialised to non-nil empty maps when a `Player` is constructed. Counter fields SHALL only ever increase during a match. The `UnitsByRank` map SHALL be recomputed (not incremented) on rank-changing events.

#### Scenario: New player starts with zeroed metrics
- **WHEN** a `Player` is constructed at match start
- **THEN** every numeric metric field equals zero and every map field is non-nil and empty

#### Scenario: Cumulative gold earned ignores spending
- **WHEN** a worker deposits 100 gold and the player later spends 80 gold on a building
- **THEN** `TotalGoldEarned` equals 100, not 20

### Requirement: Event hooks update metrics

The system SHALL update `MatchMetrics` from existing event paths so no new tick scans are introduced. Specifically: resource deposits SHALL increment the resource-earned counters; building construction reaching full HP SHALL increment the per-type and total building counters; confirmed enemy kills SHALL increment the attacker's `TotalEnemiesKilled`; a neutral camp transitioning to cleared SHALL increment the camp counters on the team that landed the killing blow; a wave transitioning out of `"active"` to `"upgrade"` or `"complete"` SHALL increment `WavesCleared`; unit rank-up events SHALL recompute `UnitsByRank` on the owner.

#### Scenario: Friendly fire does not increment enemy kills
- **WHEN** a unit owned by Player A kills another unit owned by Player A
- **THEN** Player A's `TotalEnemiesKilled` is unchanged

#### Scenario: Wave clear increments exactly once
- **WHEN** the wave manager transitions out of `"active"` on a single tick
- **THEN** every team-aligned player's `WavesCleared` increases by exactly one and subsequent ticks do not increase it again until the next wave clears

### Requirement: Sticky completion and failure

An objective whose `state.Completed` is `true` SHALL remain `completed: true` for the remainder of the match regardless of subsequent metric changes. An objective whose `state.Failed` is `true` SHALL remain `failed: true` for the remainder of the match. The evaluator MUST short-circuit on entry when either bit is set. An objective SHALL NOT be both completed and failed at the same time; if a handler is in a position to set both in the same tick, completion takes precedence.

#### Scenario: Completed objectives ignore later metric drops
- **WHEN** an objective `{count: 3}` reads metric value 3 in tick T and is marked completed, then reads metric value 2 in tick T+1
- **THEN** the objective remains `completed: true, current: 3` (current is not decremented after completion)

### Requirement: Scope semantics

A team-scope objective SHALL be evaluated against the sum of all `Player.Metrics` on the team and SHALL display the same progress to every viewer. A player-scope objective SHALL be evaluated against the viewer's own `Player.Metrics` and SHALL display per-viewer progress.

#### Scenario: Team-scope reports identical progress for every viewer
- **WHEN** a team-scope objective `{count: 5}` is in play with Player A at 2 camps and Player B at 1 camp
- **THEN** both players' snapshots show `current: 3` for that objective

#### Scenario: Player-scope reports each viewer their own progress
- **WHEN** a player-scope objective `{buildingType: "barracks", count: 1}` is in play with Player A having built a barracks and Player B having not
- **THEN** Player A's snapshot shows `completed: true` and Player B's snapshot shows `completed: false`

### Requirement: Snapshot exposure

The system SHALL include per-player `metrics` on every `Players[i]` block of the match snapshot so end-of-round screens can render comparison columns. The snapshot's `objectives` array SHALL be per-viewer: team-scope entries SHALL carry team-aggregated progress; player-scope entries SHALL carry the viewer's own progress. Every `ObjectiveSnapshot` SHALL include `id`, `type`, `description`, `scope`, `required`, `current`, `requiredCount`, `completed`, and `failed`.

#### Scenario: Snapshot carries per-player metrics for every player in the lobby
- **WHEN** a match snapshot is built for Player A in a two-player lobby
- **THEN** the snapshot contains a `Players[]` array with two entries, each carrying a populated `metrics` block including both players' totals

### Requirement: Tick-path purity

Objective evaluation SHALL read only `Player.Metrics`, `GameState` fields, and the parsed objective configs. No evaluator SHALL perform I/O (filesystem, network, profile store) on the tick path. No evaluator SHALL allocate per-tick maps or slices for the team-scope aggregation path beyond what an O(N players × M objectives) scan already requires.

#### Scenario: Evaluation does not touch the profile store
- **WHEN** the tick loop runs `evaluateObjectivesLocked()`
- **THEN** no call into `profile.Manager` occurs on that code path

### Requirement: Map `VictoryConditions` removed

The system SHALL no longer support a `victoryConditions` field on `MapConfig` or its serialised JSON. The catalog loader SHALL panic at startup if any `catalog/maps/*.json` declares a `victoryConditions` field. The map editor SHALL no longer surface a Victory Conditions authoring UI.

#### Scenario: Map JSON with legacy field is rejected
- **WHEN** a map file `catalog/maps/legacy.json` declares a non-empty `victoryConditions` array
- **THEN** the server panics at startup with a message naming the file and instructing the operator to migrate the conditions into the relevant campaign level's `objectives` array
