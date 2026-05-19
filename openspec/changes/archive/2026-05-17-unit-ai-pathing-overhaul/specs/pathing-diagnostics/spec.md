## ADDED Requirements

### Requirement: Unit carries lightweight pathing diagnostics

Each unit SHALL maintain a `PathDiagnostics` struct with integer counters: `RepathCount`, `StuckTriggerCount`, and `LastStuckTick`. These counters SHALL be incremented in-place by the existing stuck watchdog and repath code paths. No additional allocations or locks are required.

`UnreachableStrikeCount` was originally part of this diagnostic surface but has been removed alongside the unit-target strike system (see `unreachable-target-escalation` capability — drift mode replaces strike escalation for unit targets, no per-unit strike counter exists anymore). `UnreachableBuildingStrikeCount` remains on `Unit` directly for the building-target escalation system but is not snapshotted.

#### Scenario: Repath increments counter

- **WHEN** `repathUnitLocked` is called for any reason (watchdog, stun expiry, obstacle)
- **THEN** `unit.PathDiagnostics.RepathCount` is incremented by 1

#### Scenario: Stuck trigger increments counter

- **WHEN** the stuck watchdog determines a unit has moved less than the speed-aware threshold in the sample window
- **THEN** `unit.PathDiagnostics.StuckTriggerCount` is incremented by 1 and `unit.PathDiagnostics.LastStuckTick` is set to the current tick

#### Scenario: Diagnostics included in match snapshot

- **WHEN** the server builds a `MatchSnapshotMessage` (`Snapshot`, `SnapshotForPlayer`, or `snapshotUnfilteredLocked`)
- **THEN** each `UnitSnapshot` includes `RepathCount`, `StuckTriggerCount`, and `LastStuckTick` fields with `omitempty` JSON tags (zero values drop from the wire)

#### Scenario: Diagnostics reset between waves

- **WHEN** a new wave begins (prep → active transition in `tickWaveLocked`)
- **THEN** `PathDiagnostics` counters and `UnreachableBuildingStrikeCount` SHALL be reset to zero for every unit in `s.Units`

### Requirement: Match snapshot identifies persistently stuck units

`MatchSnapshotMessage` SHALL include a derived `PersistentlyStuckUnits []int` field listing IDs of units whose `PathDiagnostics.StuckTriggerCount >= 4` in the current wave. This list SHALL be computed at snapshot time by `persistentlyStuckUnitsLocked` and SHALL NOT be stored on `GameState`. The field SHALL use `omitempty` so it drops from the wire when empty.

#### Scenario: Stuck unit appears in summary

- **WHEN** a unit has triggered the stuck watchdog 4 or more times in the current wave
- **THEN** its ID appears in the `PersistentlyStuckUnits` array of every subsequent snapshot until the wave resets

#### Scenario: Recovered unit disappears after wave reset

- **WHEN** a wave resets and a previously-stuck unit's diagnostics are cleared
- **THEN** the unit no longer appears in `PersistentlyStuckUnits`

### Requirement: Player command handlers carry profiling instrumentation

The following player command handlers SHALL be wrapped with `defer profileStart("cmd.<name>")()` so their wall-clock cost surfaces in the existing `WEBRTS_TICK_PROFILE=1` summary output: `MoveUnits`, `AttackMoveUnits`, `AttackWithUnits`, `PatrolUnits`, `SetUnitStance`, `GatherWithUnits`.

The profiler SHALL remain a no-op when `WEBRTS_TICK_PROFILE` is unset (env var read once via `sync.Once`), so the instrumentation imposes zero cost in production.

#### Scenario: cmd.MoveUnits appears in profile output

- **WHEN** the server runs with `WEBRTS_TICK_PROFILE=1` and a player issues at least one move command in the 100-tick profile window
- **THEN** the `[tick-profile]` summary block contains a `cmd.MoveUnits` line with avg, max, and sum durations

#### Scenario: Instrumentation is zero-cost without the env var

- **WHEN** the server runs without `WEBRTS_TICK_PROFILE`
- **THEN** `profileStart("cmd.MoveUnits")` returns a no-op closure and command handlers pay only one bool check + one closure allocation per call
