## ADDED Requirements

### Requirement: Achievement IDs live in a single Go file

The Go server SHALL maintain achievement identifiers in exactly one file: `server/internal/steam/achievements.go`. Every achievement reported by game logic SHALL reference a constant declared in that file. No achievement string literal SHALL appear elsewhere in the Go codebase.

#### Scenario: Single source of truth

- **WHEN** game code reports an achievement
- **THEN** the call references a constant from `server/internal/steam/achievements.go` rather than a string literal
- **AND** the constant name maps 1:1 to an achievement id configured in the Steam dashboard

### Requirement: Game logic reports achievements via the `SteamBridge`

Game logic SHALL report achievements only through `SteamBridge.ReportAchievement(id)`. Game logic SHALL NOT depend on whether the bridge is the `NoopBridge` or the IPC-backed bridge. The call SHALL be fire-and-forget (see `steam-bridge` "ReportAchievement is non-blocking").

#### Scenario: Report from inside the tick loop is non-blocking

- **WHEN** the simulation's tick loop fires `bridge.ReportAchievement(<id>)` during a tick
- **THEN** the call returns within microseconds
- **AND** the tick proceeds with no observable stall

#### Scenario: Report in Steam context

- **WHEN** the simulation triggers an achievement-relevant event with the IPC-backed bridge in use
- **THEN** the bridge enqueues `{"op":"report_achievement","id":"<id>"}` for the IPC writer goroutine
- **AND** the Rust shell, on dequeuing, calls `SteamUserStats::SetAchievement` followed by `StoreStats`

#### Scenario: Report in non-Steam context

- **WHEN** the simulation triggers an achievement-relevant event with the `NoopBridge` in use
- **THEN** the bridge call is a no-op
- **AND** no error is surfaced to game logic

### Requirement: Achievement-trigger idempotency lives at the appropriate layer

For each achievement, the layer responsible for "fire at most once per qualifying outcome" SHALL be one of:

- **Naturally single-fire game events** (e.g., "first wave cleared" — the game's run state has no concept of clearing the first wave more than once in a run): no dedup needed. The triggering event itself is the dedup.
- **Multi-fire events** (e.g., "kill 100 enemies in one run") — game logic SHALL track a per-run "already-awarded" set keyed by achievement constant, and the `ReportAchievement` call SHALL be gated by membership in that set. The set is in-memory and is discarded when the run ends.
- **Cross-run progress achievements** (e.g., "win 10 games") — game logic SHALL track the underlying counter in the persistent profile JSON and call `ReportAchievement` only on the qualifying transition (`counter == 10` → fire once).

Game logic SHALL NOT rely on the Steam SDK's own "already-awarded" check as the sole dedup mechanism. The SDK's check works (`SetAchievement` is idempotent at the Steam side), but the IPC + Steam round-trip per spurious call wastes the fire-and-forget queue slot and clutters the log; per-run tracking keeps the wire quieter.

#### Scenario: Smoke-test achievement is single-fire by event nature

- **WHEN** game logic emits the "first wave cleared" event during a single-player run
- **THEN** `ReportAchievement(ACH_FIRST_WAVE)` is called exactly once
- **AND** no in-memory dedup set is required because the event itself cannot recur within a run

#### Scenario: Multi-fire event uses an in-memory per-run set

- **WHEN** a hypothetical "kill 100 enemies" achievement is added and the simulation kills its 100th enemy
- **THEN** the game-logic layer adds `ACH_HUNDRED_KILLS` to the in-memory per-run dedup set
- **AND** subsequent qualifying-event ticks check the set and skip the `ReportAchievement` call
- **AND** the dedup set is cleared when the run ends

#### Scenario: Cross-run progress achievement gates on the persistent transition

- **WHEN** a hypothetical "win 10 games" achievement is added and the profile's wins counter increments from 9 to 10
- **THEN** `ReportAchievement(ACH_TEN_WINS)` is called exactly once at that transition
- **AND** subsequent wins (counter goes 10 → 11) do NOT call `ReportAchievement` again

### Requirement: Achievements earned while Steam unavailable are accepted-loss

When the `SteamBridge` is the `NoopBridge` (Steam not running at launch, or non-Steam packaged build), or when the IPC-backed bridge returns `steam_channel_closed` or `steam_unavailable`, achievements triggered during that session SHALL be silently dropped. The system SHALL NOT persist a pending-achievements queue under the user-data directory.

#### Scenario: Single-player session in non-Steam build

- **WHEN** a player triggers the smoke-test event in a non-Steam packaged build
- **THEN** the achievement is not awarded
- **AND** no error is surfaced to game logic or to the player
- **AND** no `pending_achievements.json` (or equivalent) file is written

#### Scenario: Steam goes away mid-session

- **WHEN** the IPC channel closes mid-session and game code subsequently fires an achievement
- **THEN** the report is silently dropped (per `steam-bridge` channel-closed semantics)
- **AND** no recovery attempt is made when Steam becomes available again

Note: revisit this design call (accept-loss) only if telemetry shows a meaningful population of players hitting an achievement while Steam is unavailable. A pending-queue is a future enhancement, not in this change.

### Requirement: Achievement smoke-test on Phase 2 completion

The change SHALL wire and ship exactly one achievement (the smoke-test achievement) end to end (game event → bridge → shell → Steam) by Phase 2 completion. The full achievement list is out of scope for this change and is handled as a content-design task.

#### Scenario: Smoke-test achievement awarded

- **WHEN** a player triggers the smoke-test event during a single-player run on the packaged Steam build
- **THEN** Steam shows the achievement unlocked notification for that player
- **AND** the player's Steam profile reflects the awarded achievement

#### Scenario: Smoke-test achievement skipped outside Steam

- **WHEN** the same event triggers in a non-Steam build (e.g., the dev loop)
- **THEN** the bridge call is a no-op and no error is logged
