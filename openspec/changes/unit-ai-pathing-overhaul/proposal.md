## Why

Units and enemy AI exhibited pathing failures — stuck units that never reached their destination, target oscillation that created AI "infinite loops", per-unit A* storms that froze the simulation at 1/10th speed, and click-time hitches up to 566ms on group move commands. These bugs compounded on larger maps and in high-unit-count scenarios. Live profiling under `WEBRTS_TICK_PROFILE=1` traced the slowdowns to four distinct per-tick A* storms and one O(K) command-handler bottleneck; this change resolves all five.

## What Changes

- **Drift mode for unreachable unit targets**: When A* to a unit attack target fails, the unit no longer retries every 3 ticks — it enters drift mode and walks straight-line toward the target's last known coordinates each tick (no A*), halting silently at impassable terrain. Replaces the strike-count escalation that originally targeted unit targets.
- **Strike-count escalation + in-combat retry throttle for building targets**: A separate strike system for building targets (1 strike = 40-tick cooldown, 2 = 120, 3 = clear + fall back to objective). Per-tick A* retries in `tickUnitCombatLocked`'s unit-vs-building branch are throttled at `approachRepathCooldownTicks` and feed the escalation, so a stuck attacker drops its target within ~9 ticks instead of looping at the throttle cadence forever.
- **Per-unit re-evaluation cooldown** (`NextCombatEvalTick`): Bounds the rate at which a unit cycles among unreachable building candidates when the single-slot `UnreachableBuildingTargetID` memo overwrites itself each tick.
- **Target-drop retarget stagger**: When `shouldDropCurrentTargetLocked` clears a unit's target inside `evaluateCombatLocked`, the unit's next re-evaluation is staggered by `unit.ID % retargetStaggerTicks` (5). Prevents the simultaneous mass-retarget spike when a wave clears (every attacker loses its target the same tick, all run sub-cell A* together, freezing for ~200ms). Determinism preserved via ID-modulo (no RNG). Applies symmetrically to player and enemy units.
- **Speed-aware stuck-unit watchdog**: 40% of expected travel distance in one sample window, with a floor of 8px. Replaces the hardcoded 6px threshold that produced false-positives in dense clusters and false-negatives for fast units.
- **Stun-expiry forced repath**: When a unit's stun expires mid-path, schedule one repath so the unit doesn't walk along a stale route that may now pass through a newly-placed building.
- **Guard grace window scaled to profile**: `RetargetIntervalTicks + 5` instead of a flat 20 ticks. Removes snap-home flicker on short-interval profiles.
- **Global objective search rate-limiter**: Caps `assignEnemyObjectiveLocked` to at most one call per 5 ticks across the whole match, with a strike-3 bypass for the urgent fallback path.
- **Shared sub-cell blocked map for group commands**: `MoveUnits` / `AttackMoveUnits` / `PatrolUnits` / `AttackWithUnits` build the sub-cell pathing blocked map once per command (per plane) and pass it through to each unit, eliminating O(K) redundant rebuilds.
- **Leader-follower group pathing**: Group commands run one full A* for a representative leader and derive each follower's path by copying the leader's middle waypoints with a per-unit endpoint substituted. A line-of-sight sample from the follower's start to the leader's first waypoint gates the splice; LoS-blocked followers fall back to per-unit A*. Drops K-unit command cost from K sub-cell A*s to 1.
- **PathDiagnostics on Unit + PersistentlyStuckUnits in snapshot**: Lightweight per-unit counters (`RepathCount`, `StuckTriggerCount`, `LastStuckTick`) exposed via the existing snapshot path. Top-level `PersistentlyStuckUnits []int` derived at snapshot time identifies units that triggered the watchdog 4+ times in the current wave.
- **Command-handler profiling instrumentation**: `MoveUnits`, `AttackMoveUnits`, `AttackWithUnits`, `PatrolUnits`, `SetUnitStance`, `GatherWithUnits` wrapped with `profileStart("cmd.*")` so click-time costs surface in the same `WEBRTS_TICK_PROFILE=1` output as the tick loop.

## Capabilities

### New Capabilities

- `pathing-diagnostics`: Structured per-unit pathing telemetry (stuck triggers, repath counts) exposed on the match snapshot, plus a derived `PersistentlyStuckUnits` list. Player command handlers carry the same profiling hooks under the existing env-var gate.

### Modified Capabilities

- `unit-movement`: Speed-aware watchdog, stun-expiry repath, drift mode for unreachable unit targets, shared sub-cell blocked map for group commands, and leader-follower group pathing for both move and attack commands.
- `enemy-ai`: Guard grace window scaled to profile, global objective search rate-limiter, per-unit re-evaluation cooldown after failed acquisition.
- `unreachable-target-escalation`: Building-target strike escalation (with in-combat retry throttle + memo-aware objective search). Unit-target strike escalation is REMOVED (replaced by drift mode).

## Impact

- **Backend (Go)**: `state.go`, `state_movement.go`, `state_combat.go`, `state_waves.go`, `combat_ai.go`, `combat_ai_retreat.go`, `combat_ai_scoring.go`, `combat_ai_profiles.go`. Also `server/dev.bat` (development env var for the profiler).
- **No API/protocol changes** other than additive `UnitSnapshot` fields (`RepathCount`, `StuckTriggerCount`, `LastStuckTick`) and the top-level `PersistentlyStuckUnits []int`, all with `omitempty`. Clients without explicit support for these fields parse cleanly.
- **No breaking changes**: all behavioural changes are backwards-compatible with existing save state and map formats. Drift mode replaces strike escalation for unit targets without changing any wire-visible state.
- **Frontend**: Optional. The new debug fields can be rendered in a debug overlay; no required client changes.
- **Tests removed**: `server/internal/game/combat_ai_unreachable_test.go` validated the deleted unit-target strike system. Deleted alongside its system.
