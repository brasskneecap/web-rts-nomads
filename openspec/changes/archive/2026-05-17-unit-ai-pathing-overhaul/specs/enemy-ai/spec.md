## ADDED Requirements

### Requirement: Guard grace window scales to the unit's retarget interval

The delay before a guard unit begins pathing back to its anchor after losing its target SHALL be `resolveCombatProfile(unit).RetargetIntervalTicks + 5` ticks, not a fixed constant. This ensures the grace window always spans at least one full retarget evaluation cycle for every profile. The previous `guardReturnGraceTicks = 20` constant SHALL be removed entirely.

#### Scenario: Short-retarget-interval guard has a short but valid grace window

- **WHEN** a unit with `RetargetIntervalTicks = 4` loses its target
- **THEN** the guard return grace window is `4 + 5 = 9 ticks` before snap-home begins

#### Scenario: Long-retarget-interval guard has a longer grace window

- **WHEN** a unit with `RetargetIntervalTicks = 20` loses its target
- **THEN** the guard return grace window is `20 + 5 = 25 ticks`

#### Scenario: New target acquired within grace window cancels snap-home

- **WHEN** a unit acquires a new attack target before the grace window expires
- **THEN** `tickGuardReturnLocked` does not issue a return path and the unit continues engaging

### Requirement: Enemy objective search is rate-limited globally

A `nextGlobalObjectiveSearchTick` field on `GameState` SHALL gate all calls to `assignEnemyObjectiveLocked` from the no-target evaluation branch in `evaluateCombatLocked`. At most one map-wide A* objective search SHALL execute per 5 simulation ticks (~250ms), regardless of how many units are simultaneously idle.

The per-unit `NextObjectiveSearchTick` cooldown SHALL be set **only inside the success path** (after `assignEnemyObjectiveLocked` actually runs). Setting it unconditionally would advance globally-gated units' per-unit cooldown to `Tick + enemyObjectiveSearchCooldownTicks` even when skipped, degrading the 5-tick global cadence.

The strike-3 fallback in `applyBuildingUnreachableEscalationLocked` SHALL bypass the global gate (call `assignEnemyObjectiveLocked` directly). Strike-3 is considered urgent and must not be indefinitely deferred when many units are simultaneously cycling through unreachable buildings.

#### Scenario: First idle unit triggers objective search immediately

- **WHEN** a unit becomes idle and `s.Tick >= s.nextGlobalObjectiveSearchTick`
- **THEN** `assignEnemyObjectiveLocked` runs for that unit, and `s.nextGlobalObjectiveSearchTick` is set to `s.Tick + 5`

#### Scenario: Subsequent idle units are deferred within the same window

- **WHEN** a second unit becomes idle in the same tick or within 4 ticks of the first
- **THEN** `assignEnemyObjectiveLocked` is NOT called for the second unit that tick, and that unit's `NextObjectiveSearchTick` is NOT advanced

#### Scenario: All idle units eventually receive objectives

- **WHEN** 20 units are simultaneously idle after the global rate limit resets
- **THEN** each unit receives an objective assignment within approximately `20 * 5 = 100 ticks` (~5 seconds) at the global rate of one per 5 ticks

#### Scenario: Strike-3 fallback bypasses the global limiter

- **WHEN** a unit hits its third unreachable-building strike and calls `assignEnemyObjectiveLocked` as part of the escalation
- **THEN** this call proceeds regardless of `nextGlobalObjectiveSearchTick`

### Requirement: Per-unit re-evaluation cooldown after failed acquisition

A new field `Unit.NextCombatEvalTick` SHALL gate `evaluateCombatLocked` for units without a current target. When `applyCombatTargetLocked` finishes leaving the unit without `AttackTargetID`, without `AttackBuildingTargetID`, and not `Moving` (and not `AttackDrifting`), `evaluateCombatLocked` SHALL set `unit.NextCombatEvalTick = s.Tick + profile.RetargetIntervalTicks` (or `enemyObjectiveSearchCooldownTicks` if the profile's interval is zero). When the unit successfully acquires a target or enters drift mode, `NextCombatEvalTick` SHALL be reset to 0.

At the top of `evaluateCombatLocked`, when `!hasTarget && s.Tick < unit.NextCombatEvalTick`, the function SHALL early-return.

This cooldown breaks the cycle where a unit with multiple unreachable buildings in detection range picks A → fails → memo overwrites to A → next tick picks B (memo skips A) → fails → memo overwrites to B (losing A's memo) → next tick picks A again, … . The single-slot `UnreachableBuildingTargetID` cannot track multiple memos; the per-unit re-eval cooldown bounds the cycle's tick budget.

#### Scenario: Failed acquisition sets the cooldown

- **WHEN** `evaluateCombatLocked` runs, `applyCombatTargetLocked` is invoked, and the unit ends with no target and not Moving
- **THEN** `unit.NextCombatEvalTick` is set to `s.Tick + RetargetIntervalTicks` (or `enemyObjectiveSearchCooldownTicks` when the profile interval is zero)

#### Scenario: Successful acquisition clears the cooldown

- **WHEN** the unit ends `evaluateCombatLocked` with an AttackTargetID, AttackBuildingTargetID, or Moving=true
- **THEN** `unit.NextCombatEvalTick` is set to 0

#### Scenario: Cooldown gates re-entry

- **WHEN** a unit without a target enters `evaluateCombatLocked` while `s.Tick < unit.NextCombatEvalTick`
- **THEN** the function returns immediately without calling `selectBestTargetLocked` or `applyCombatTargetLocked`

### Requirement: Target-drop retarget stagger

When `shouldDropCurrentTargetLocked` triggers `clearCombatTargetLocked` inside `evaluateCombatLocked`, the unit SHALL set `NextCombatEvalTick = s.Tick + (unit.ID % retargetStaggerTicks)` (with `retargetStaggerTicks = 5`). This extends the per-unit re-evaluation cooldown from "only after failed acquisition" to "also after target loss via AI drop", spreading simultaneous mass retargets across consecutive ticks rather than compressing them into one tick.

The stagger SHALL be deterministic — keyed off `unit.ID` (assigned sequentially at spawn), not any RNG — so seeded simulation replays remain reproducible.

The stagger SHALL apply to both player and enemy units since both flow through `evaluateCombatLocked` in `tickCombatAILocked`'s evaluate pass. No owner-specific gating.

The retreat-clear path (`shouldRetreatLocked` → `clearCombatTargetLocked` → `issueRetreatLocked` → early return) SHALL NOT set the stagger: the unit is immediately given a new movement order (retreat destination) and would not re-evaluate that tick regardless. Limiting the stagger to the `shouldDropCurrentTargetLocked` site keeps the change scoped to "target died / target invalid" — the exact case that produces synchronous mass-retargets.

Without this stagger, a wave clear causing N attackers to all lose targets the same tick produces N sub-cell A* calls in one tick — for N=10 on a populated map, ~100 ms of A* work compressed into a single 50 ms tick budget, perceived as a freeze.

#### Scenario: Mass retarget after wave clear

- **WHEN** N player units lose their attack targets in the same tick because the enemies they were attacking just died
- **THEN** each unit sets `NextCombatEvalTick = s.Tick + (unit.ID % 5)`; over the next 5 ticks the retargets distribute across the population (no single tick handles all N units)

#### Scenario: AoE kill triggers staggered enemy retarget

- **WHEN** an AoE attack kills multiple player units and the surviving enemies that were attacking them all drop targets the same tick
- **THEN** the same stagger applies (the helper is owner-agnostic), spreading enemy retargets across 5 ticks instead of compressing them

#### Scenario: Single unit losing target still re-evaluates quickly

- **WHEN** one unit (ID = 7) loses its target in isolation
- **THEN** the unit re-evaluates at `s.Tick + (7 % 5) = s.Tick + 2` — a 100 ms delay, below human perception threshold

#### Scenario: Stagger is deterministic across replays

- **WHEN** two seeded simulation runs reach the same tick state with the same unit IDs losing targets
- **THEN** the same units re-evaluate on the same future ticks (ID-modulo is RNG-free)

#### Scenario: Retreat clear bypasses stagger

- **WHEN** `shouldRetreatLocked` causes `clearCombatTargetLocked` and `issueRetreatLocked` runs
- **THEN** `NextCombatEvalTick` is NOT advanced by the stagger; the unit's retreat path takes over and the next AI eval would short-circuit on `Moving = true` anyway

#### Scenario: Player attack-target order bypasses stagger

- **WHEN** a unit with `Order.Type == OrderAttackTarget` and a non-zero `AttackTargetID` enters `evaluateCombatLocked`
- **THEN** the function returns at the player-sticky check before reaching the stagger site; the player's explicit command is unaffected
