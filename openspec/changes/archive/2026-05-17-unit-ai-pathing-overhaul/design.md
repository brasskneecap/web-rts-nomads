## Context

The game server runs a deterministic tick-based simulation at ~20Hz under a single write lock (`s.mu`). All unit movement, combat AI evaluation, and pathfinding happen inside this locked tick loop, and player command handlers (`MoveUnits`, `AttackWithUnits`, …) also run under the same lock outside the tick. The codebase already had safeguards for several degenerate cases (stuck-unit watchdog, guard grace window), but several edge cases produced runaway per-tick A* costs that froze the simulation. Investigation under `WEBRTS_TICK_PROFILE=1` exposed three distinct storm vectors and one click-time bottleneck; this change resolves all four.

Key files affected: `pathing.go`, `state.go`, `state_movement.go`, `state_combat.go`, `state_waves.go`, `combat_ai.go`, `combat_ai_retreat.go`, `combat_ai_profiles.go`, `combat_ai_scoring.go`.

Constraints:

- All changes must remain deterministic under the existing seed-based RNG.
- Tick budget is ~50ms; any new per-tick work must be O(1) or bounded.
- No new external dependencies.
- Existing `*Locked` method conventions must be preserved.

## Goals / Non-Goals

**Goals:**

- Eliminate every observed per-tick A* storm: stuck-unit repathing in dense clusters; in-combat retry against unreachable building targets; re-evaluation cycling among unreachable buildings; per-unit refresh against unreachable unit targets.
- Make multi-unit player commands (`MoveUnits` / `AttackMoveUnits` / `PatrolUnits` / `AttackWithUnits`) scale O(1) in A* cost regardless of selection size.
- Fix stun-residual path invalidation.
- Fix guard snap-home jitter for short-`RetargetIntervalTicks` profiles.
- Bound worst-case tick cost from `assignEnemyObjectiveLocked` on large maps.
- Add structured per-unit pathing diagnostics + command-handler profiling instrumentation.

**Non-Goals:**

- Rewriting the A* algorithm itself or changing grid resolution.
- Changing the separation system's algorithmic complexity (O(N²) over local clusters accepted).
- Adding new combat AI profiles or changing scoring weights.
- Any client-facing gameplay behaviour changes; this is a correctness + perf overhaul.

## Decisions

### Decision 1: Drift mode replaces strike-count escalation for unit targets

**Chosen**: When `findPath` fails for a unit's attack target, the unit enters **drift mode**: `AttackDrifting = true`, `TargetX/Y` set to the target's current coordinates, `Path = nil`, `Moving = true`. The per-unit movement loop has a dedicated drift branch that steps straight-line toward `TargetX/Y` each tick, checks walkability, and silently halts when the next cell is blocked (no repath). The next AI re-evaluation re-runs `applyCombatTargetLocked` and either finds a fresh path (exiting drift) or re-enters drift.

The strike-count system for unit targets (`UnreachableTargetID`, `UnreachableStrikeCount`, `applyUnitUnreachableEscalationLocked`) is **deleted**. The memo-skip in `selectBestTargetLocked` for unit candidates is also removed — drift is itself the "I tried, can't path, keep walking" state, no memo needed.

**Why this beats strike escalation for units**: the strike system converged correctly in 9 ticks but produced expensive cycles in the meantime (one full sub-cell A* per 3 ticks per stuck unit). Drift replaces every per-tick A* with a single walkability check (~µs) and lets separation pressure resolve crowd-blockage organically. When the target becomes reachable (target moves, ally clears the path), the next AI eval re-tries A* automatically — same recovery, no oscillation.

**Alternatives considered**:

- *Strike-count escalation* (originally chosen, since rejected): converges but burns budget during the 9-tick cycle, scales O(K) in A*.
- *Pick next-best target on first failure*: changes selection feel; doesn't help when ALL targets are unreachable.
- *Drift mode* (chosen): zero A* during failure state, cheap recovery, preserves "always doing something" UX.

### Decision 2: In-combat retry throttle + escalation for building targets

**Chosen**: `tickUnitCombatLocked`'s unit-vs-building branch wraps its retry `assignUnitPath` call in a `NextApproachRepathTick` cooldown (3 ticks) AND, on failure, calls `applyBuildingUnreachableEscalationLocked`. The escalation is what eventually clears the target via `clearCombatTargetLocked` on strike 3, since `shouldDropCurrentTargetLocked` for building targets is intentionally sticky (only drops on destruction).

**Why both pieces**: without the throttle, even one stuck attacker against an unreachable building runs a full sub-cell A* (~12-15ms per call) every tick. Without the escalation, the AI re-evaluation cycle never drops the building target (combat_ai_scoring.go:49-65 stickiness) and the unit retries forever at the throttle cadence.

**Why building targets keep strikes when unit targets don't**: drift mode would have enemy waves piling up at impassable walls — not a perf failure but a gameplay failure. The strike system gives the AI a clean exit to "pick a different building or the townhall objective" after three failures, which is the right wave-AI behaviour. Unit targets are handled differently because the "wait at a wall" outcome is fine when the target is another mobile unit (it'll usually walk into range).

### Decision 3: Per-unit re-evaluation cooldown after failed acquisition

**Chosen**: New field `Unit.NextCombatEvalTick`. Set by `evaluateCombatLocked` after `applyCombatTargetLocked` if the unit ended without `AttackTargetID`, `AttackBuildingTargetID`, or `Moving = true`. Gates re-entry into the no-target acquire branch for `RetargetIntervalTicks` ticks (or `enemyObjectiveSearchCooldownTicks` if profile interval is zero).

**Why this exists**: the single-slot `UnreachableBuildingTargetID` memo only excludes ONE building per unit. When all visible buildings are unreachable, the AI cycles through them rapidly — pick A, fail, memo=A, next tick pick B (memo skips A), fail, memo=B (overwriting A's memo), next tick pick A again, … . The cooldown breaks this cycle by parking the unit for a short interval after any failed acquisition.

**Alternatives considered**:

- *Multi-slot memo (set of buildings)*: more thorough but bigger refactor; not needed once the cycle is throttled.
- *Increase global objective search cooldown*: only helps the no-buildings-found fallback path, not the buildings-exist-but-all-unreachable cycle.

### Decision 4: Shared sub-cell blocked map for group commands

**Chosen**: `MoveUnits` / `AttackMoveUnits` / `PatrolUnits` / `AttackWithUnits` build the per-plane sub-cell blocked map **once** via `buildGroupSubBlockedLocked` and pass it through to each unit's `assignUnitPathWithSubBlocked` / `assignAttackApproachPathLockedWithSubBlocked` call. `buildUnitPathBlockedLocked` excludes same-`OrderID` peers + `self`, and every member of a group shares the OrderID by construction, so the map is identical for every unit and re-computing it per-unit was pure waste.

**Measured impact**: `cmd.MoveUnits` 54ms → 1ms average on a 9-unit selection.

### Decision 5: Leader-follower paths for group commands

**Chosen**: `assignGroupPathsLocked` and `assignAttackGroupPathsLocked` route a shared-`OrderID` group via one sub-cell A* (the "leader") and derive each follower's path by copying the leader's waypoints, substituting the follower's endpoint (formation slot for moves, per-range stop for attacks). A line-of-sight sample from the follower's start to the leader's first waypoint gates the splice; on LoS failure that follower falls back to a per-unit A*.

For attacks, the leader is the **shortest-range unit** (its stop is the deepest along the approach corridor, so every follower with `AttackRange ≥ leader.AttackRange` can truncate the same path at its own range). For moves, the leader is the unit closest to its formation slot (most representative route).

**Why this works**: a group's units start in roughly the same area and head to roughly the same destination. Their paths differ in start position and per-unit endpoint, but the middle traversal is identical. The cheap LoS check (~µs) catches the rare case where a follower is on the wrong side of an obstacle relative to the leader.

**Expected impact**: K-unit `cmd.MoveUnits` drops from K sub-cell A*s to 1, scaling O(1) instead of O(K).

### Decision 6: Speed-aware stuck watchdog threshold

**Chosen**: Compute a per-unit threshold inside the per-unit movement loop:

```go
threshold := math.Max(8.0, unit.MoveSpeed * perkSpeedMult * stuckSampleInterval * 0.4)
thresholdSq := threshold * threshold
```

40% of expected travel distance in one `stuckSampleInterval` (0.6s), squared to match the squared-distance comparison. Minimum of 8px guards against false-negatives on very slow units. The captured `perkSpeedMult` is also reused by the movement step itself, avoiding a duplicate `perkMoveSpeedMultiplierLocked` call.

### Decision 7: Schedule forced repath on stun expiry

**Chosen**: In the per-unit tick at `state.go` (around the existing stun decay), capture `wasStunned := unit.StunnedRemaining > 0` before the decrement, then after the decrement test `wasStunned && unit.StunnedRemaining <= 0 && unit.Moving && len(unit.Path) > 0` → `s.repathUnitLocked(unit, blocked)`. One repath per stun-expiry event; falls back to clearing `Moving`/`Path` if repath itself fails.

### Decision 8: Scale guard grace window to the unit's RetargetIntervalTicks

**Chosen**: `unit.NextGuardReturnTick = s.Tick + resolveCombatProfile(unit).RetargetIntervalTicks + 5`. The constant `guardReturnGraceTicks` is deleted entirely (no more flat fallback). Correct by construction for every profile.

### Decision 9: Global objective search rate-limiter

**Chosen**: `GameState.nextGlobalObjectiveSearchTick` gates `assignEnemyObjectiveLocked` to at most one call per 5 ticks across the whole match. The per-unit `NextObjectiveSearchTick` remains as a secondary guard for units that just got an objective. The strike-3 fallback path (when a unit's building target hits strike 3) calls `assignEnemyObjectiveLocked` directly, bypassing the global gate — strike-3 is considered urgent and must not be indefinitely deferred.

### Decision 10: PathDiagnostics struct on Unit + PersistentlyStuckUnits in snapshot

**Chosen**: Lightweight `PathDiagnostics` struct embedded on `Unit` with `RepathCount`, `StuckTriggerCount`, `LastStuckTick`. Counters are incremented in-place by the existing watchdog / repath code paths — zero extra allocation. The match snapshot exposes these fields per unit (omitempty) plus a top-level `PersistentlyStuckUnits []int` derived list (units with `StuckTriggerCount >= 4` in the current wave). Counters reset on the prep → active wave transition.

### Decision 11: Target-drop retarget stagger

**Chosen**: When `shouldDropCurrentTargetLocked` clears a unit's target inside `evaluateCombatLocked`, set `unit.NextCombatEvalTick = s.Tick + (unit.ID % retargetStaggerTicks)` with `retargetStaggerTicks = 5`. The existing `NextCombatEvalTick` gate at the no-target branch picks it up and spreads simultaneous mass-retargets across 5 consecutive ticks.

**Why**: live profiling after the first round of fixes showed `unitCombat max = 123 ms`, `combatAI.evaluate max = 63 ms` on a single tick coinciding with a wave clear. Root cause: ~10 player units all lost their (just-killed) enemy targets in the same tick, ran `selectBestTargetLocked` + `applyCombatTargetLocked` (each with its own sub-cell A*) simultaneously. The work itself is unavoidable; compressing it into one tick produces a perceptible ~200 ms freeze.

ID-modulo stagger is RNG-free (preserves seeded determinism), bounded (max 4-tick = 200 ms delay before the highest-ID unit re-engages — below human reaction threshold), and applies to both player and enemy units automatically because `evaluateCombatLocked` runs for every combat unit regardless of owner.

**Why limited to the `shouldDropCurrentTargetLocked` site (not `clearCombatTargetLocked` directly)**: `clearCombatTargetLocked` is called from many paths — player commands replacing targets, strike-3 escalation chaining into `assignEnemyObjectiveLocked`, retreat → `issueRetreatLocked`. None of those want a stagger applied. The drop-via-AI-eval path is the only one that produces the synchronous mass-retarget pattern; isolating the stagger there keeps the change minimal.

**Alternatives considered**:

- *Global retarget rate limiter* (analogous to `nextGlobalObjectiveSearchTick`): caps simultaneous retargets across the whole match. Simpler conceptually but feels sluggish when only a few units retarget at once (they'd be forced to wait even when there's plenty of budget).
- *Hash-based stagger* (e.g. `(unit.ID * prime) % spread`): equivalent quality, no advantage over modulo.
- *Time-of-target-death-based stagger*: would handle units that lose targets across multiple ticks, but adds state and is unnecessary — units losing targets across multiple ticks already naturally spread.

### Decision 12: Command-handler profiling instrumentation

**Chosen**: `MoveUnits`, `AttackMoveUnits`, `AttackWithUnits`, `PatrolUnits`, `SetUnitStance`, `GatherWithUnits` are each wrapped with `defer profileStart("cmd.<name>")()`. Profiles surface command-handler time under the same `WEBRTS_TICK_PROFILE=1` mechanism as the tick loop — zero cost when the profiler is disabled. Used during this overhaul to identify the `cmd.MoveUnits` click-time bottleneck; left in place as a permanent debug hook for future regressions.

## Risks / Trade-offs

- **Drift toward an unreachable target stops at a wall** — by design. A wave of enemies whose only target is locked behind impassable terrain will pile up at the wall. That's a *gameplay* signal to the level designer, not a sim bug; perf is bounded (one walkability check per drifting unit per tick).
- **Leader-follower LoS fallback** — when a player selects units that span an obstacle, half the group hits the per-unit-A* fallback. Still O(K) in the worst case for that command, but typical-case (tight selection) is O(1).
- **Per-unit re-eval cooldown delays re-acquisition by RetargetIntervalTicks** after a failure. Acceptable: the cycle that the cooldown breaks was burning budget for the same outcome (no target acquired).
- **Strike-count escalation may cause units to ignore a building that became reachable after 3 failures** — strike count resets on any successful path to any building, so once the unit makes meaningful progress elsewhere, previously-unreachable buildings are reconsidered.

## Migration Plan

All changes are internal to the tick loop and command handlers; no save-state schema changes; no protocol changes other than additive snapshot fields with `omitempty`. Existing sessions benefit immediately on server restart. No rollback strategy beyond reverting the commit.

## Open Questions

- The 5-tick global objective search limit and 3-tick approach-repath cooldown haven't been tuned against very large maps (100+ unit armies). Revisit after extended playtesting.
- Should drift mode have a maximum duration before forcing a target drop? Currently a unit can drift indefinitely if no AI re-eval picks a new target. In practice the AI eval cycle keeps things moving, but edge cases (paralysed AI, all targets dead) could leave a drifting unit "stuck" forever.
