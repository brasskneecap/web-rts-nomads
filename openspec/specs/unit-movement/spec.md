# unit-movement Specification

## Purpose

Defines the unit movement pathing behaviours: the speed-aware stuck watchdog
threshold, forced repath on stun expiry, drift mode for unreachable unit
targets, and the shared sub-cell blocked map / leader-follower path optimisations
for group move and shared-target attack commands.

## Requirements

### Requirement: Stuck watchdog threshold is proportional to unit move speed

The stuck-unit watchdog SHALL compute its progress threshold as `math.Max(8.0, unit.MoveSpeed * perkMultiplier * stuckSampleInterval * 0.4)` (40% of expected travel distance in one sample window). The result SHALL be squared before comparison because the watchdog compares squared distances (`ddx*ddx + ddy*ddy < thresholdSq`). The minimum of 8px (stored as `64.0` squared) prevents false-negatives for very slow units.

The `stuckSampleInterval` constant (currently `0.6` in `state.go`) SHALL be referenced by name rather than hardcoded so future tuning automatically updates the threshold. The captured `perkMoveSpeedMultiplierLocked` SHALL also be reused by the movement step itself, avoiding a duplicate map lookup in the hot path.

#### Scenario: Fast unit uses a correctly calibrated threshold

- **WHEN** a unit has `MoveSpeed = 200` and no perk multiplier
- **THEN** the stuck threshold is `200 * 1.0 * 0.6 * 0.4 = 48px` (linear), stored as `2304` for the squared comparison

#### Scenario: Slow unit uses a lower threshold, avoiding false stuck detection

- **WHEN** a unit has `MoveSpeed = 60` and no perk multiplier
- **THEN** the stuck threshold is `60 * 1.0 * 0.6 * 0.4 = 14.4px` (linear), stored as `≈207` squared

#### Scenario: Threshold never falls below minimum

- **WHEN** a unit has `MoveSpeed = 10` (`10 * 1.0 * 0.6 * 0.4 = 2.4px` before clamping)
- **THEN** the threshold is clamped to `8px` linear (`64.0` squared)

#### Scenario: Perk speed bonus is included in threshold

- **WHEN** a unit has `MoveSpeed = 100` and a perk speed multiplier of 1.5
- **THEN** the stuck threshold is `100 * 1.5 * 0.6 * 0.4 = 36px` linear (`1296` squared)

### Requirement: Stun expiry triggers a forced repath

When a unit's `StunnedRemaining` transitions from positive to zero or below inside the unit loop in `state.go`, and the unit has an active path (`Moving == true && len(Path) > 0`), the server SHALL call `repathUnitLocked(unit, blocked)` once for that unit. If the repath itself fails (returns false), `Moving` is set to false and `Path` is cleared.

Detection pattern:

```go
wasStunned := unit.StunnedRemaining > 0
unit.StunnedRemaining = math.Max(0, unit.StunnedRemaining - dt)
// ... later, before the stun-gate continue ...
if wasStunned && unit.StunnedRemaining <= 0 && unit.Moving && len(unit.Path) > 0 {
    if !s.repathUnitLocked(unit, blocked) {
        unit.Moving = false
        unit.Path = nil
    }
}
```

#### Scenario: Unit repathed after stun expires mid-path

- **WHEN** a unit's stun expires and it has a non-empty path
- **THEN** `repathUnitLocked` is called exactly once that tick and the unit resumes movement along the newly computed path

#### Scenario: Unit with no path is unaffected

- **WHEN** a unit's stun expires and `Moving == false`
- **THEN** no repath is triggered

#### Scenario: Repath fails gracefully if path is now fully blocked

- **WHEN** `repathUnitLocked` returns false after stun expiry
- **THEN** `Moving` is set to false, `Path` is cleared, and the unit idles until a new order or AI retarget issues a fresh path

### Requirement: Drift mode for unreachable unit targets

When `assignAttackApproachPathLocked` cannot produce a path to a unit target (either `findNearestWalkable` fails or `findPath` returns empty), the unit SHALL enter **drift mode** via `enterAttackDriftLocked`: `AttackDrifting = true`, `TargetX/Y` set to the target's current coordinates, `Path = nil`, `Moving = true`, `Status = "Moving To Attack"`. Drift mode SHALL replace the strike-count escalation that previously fired here.

The per-unit movement loop in `state.go` SHALL detect drift state (`Moving == true && len(Path) == 0 && AttackDrifting == true`) and take one straight-line step toward `TargetX/Y` per tick. If the next cell is not walkable for a ground unit, drift SHALL halt silently by setting `Moving = false` and `AttackDrifting = false` — no repath, no escalation. If the unit reaches within `unitRadius` of `TargetX/Y`, drift also halts.

Drift state SHALL be cleared in `clearCombatTargetLocked`, `resetUnitMovementLocked`, on successful `assignUnitPathWithSubBlocked` inside `assignAttackApproachPathLocked`, and whenever the movement loop sets `Moving = false`.

`refreshUnitAttackApproachLocked` SHALL early-return when `AttackDrifting && !force`. Drifting units rely on AI re-evaluation (at `RetargetIntervalTicks` cadence) for path retries, not per-tick combat tick refresh — without this gate the in-range check against `TargetX/Y` (set at drift entry) re-fires A* every tick a moving target wiggles.

#### Scenario: A* failure enters drift mode

- **WHEN** `assignAttackApproachPathLocked` runs and `findPath` returns no waypoints
- **THEN** the unit's `AttackDrifting` is true, `Path` is nil, `Moving` is true, and `TargetX/Y` matches the target's current position

#### Scenario: Drifting unit steps toward target each tick

- **WHEN** the per-unit movement loop ticks a unit in drift mode
- **THEN** the unit moves one step (at `MoveSpeed * perkMult * slowFactor * dt`) toward `TargetX/Y` along the unit→target vector

#### Scenario: Drift halts at impassable terrain

- **WHEN** the next cell along a drifting unit's step is not walkable
- **THEN** the unit sets `Moving = false`, `AttackDrifting = false`, takes no movement step that tick, and skips repath

#### Scenario: Drift halts when target's last known position is reached

- **WHEN** a drifting unit is within `unitRadius` of `TargetX/Y`
- **THEN** the unit sets `Moving = false` and `AttackDrifting = false`; subsequent combat tick handles in-range attack or AI re-eval picks the next action

#### Scenario: Drift state cleared by target clear

- **WHEN** `clearCombatTargetLocked` runs on a drifting unit
- **THEN** `AttackDrifting` is set to false alongside `AttackTargetID` / `AttackBuildingTargetID`

#### Scenario: Per-tick refresh skipped while drifting

- **WHEN** `refreshUnitAttackApproachLocked` is called for a unit with `AttackDrifting == true` and `force == false`
- **THEN** the function returns immediately without running A*

### Requirement: Group commands share one sub-cell blocked map

`MoveUnits`, `AttackMoveUnits`, `PatrolUnits`, and `AttackWithUnits` SHALL build the per-plane sub-cell blocked map exactly once per command via `buildGroupSubBlockedLocked` and pass it through to each unit's path-assignment call. Per-unit rebuilds inside `buildUnitPathBlockedLocked` are pure waste: every member of a shared-`OrderID` group excludes the same peer set (same-OrderID + self), producing an identical map.

`buildGroupSubBlockedLocked` SHALL return `(ground, flyer)` maps; either may be nil when no unit of that plane exists in the group. Callers pick the right map per-unit based on `unit.Flyer`.

The path-assignment entry points SHALL accept an optional precomputed sub-blocked map:

- `assignUnitPathWithSubBlocked(unit, dest, blocked, subBlocked, reservedGoals)` — when `subBlocked` is non-nil, skip the internal `buildUnitPathBlockedLocked` call.
- `assignAttackApproachPathLockedWithSubBlocked(unit, target, blocked, subBlocked)` — same pattern.

The original signatures (`assignUnitPath`, `assignAttackApproachPathLocked`) SHALL remain available for single-unit callers (repaths, retargets, single-unit moves) and call the With variant with `subBlocked = nil`.

#### Scenario: 20-unit MoveUnits builds one sub-blocked map

- **WHEN** a player issues `MoveUnits` with 20 selected ground units
- **THEN** `buildUnitPathBlockedLocked` is invoked exactly once (one ground exemplar) for that command, regardless of selection size

#### Scenario: Mixed-plane selection builds one map per plane

- **WHEN** a group contains both ground and flyer units
- **THEN** `buildGroupSubBlockedLocked` returns two non-nil maps, and each unit's pathing call uses the map matching its `Flyer` flag

#### Scenario: Single-unit move bypasses group helper

- **WHEN** `MoveUnits` is called with one valid unit
- **THEN** the early-return single-unit branch runs `assignUnitPath` directly (no group-helper allocation)

### Requirement: Leader-follower paths for group moves

`MoveUnits`, `AttackMoveUnits`, and `PatrolUnits` SHALL route shared-`OrderID` groups via `assignGroupPathsLocked`: one full sub-cell A* for a representative leader, followers reuse the leader's middle waypoints with their own formation slot substituted as the endpoint. A line-of-sight sample (`lineWalkableLocked`) from each follower's start to the leader's first waypoint gates the splice; on LoS failure that follower falls back to a per-unit `assignUnitPathWithSubBlocked`.

The leader SHALL be selected as the unit whose current position is closest to its own destination (formation slot) — its A* route is the most representative of the corridor the rest of the group needs to cover.

When the leader's path computation fails (no valid path from anywhere in the group's start area), the helper SHALL fall back to per-unit `assignUnitPathWithSubBlocked` for every unit.

#### Scenario: Tight group share one A*

- **WHEN** 20 units selected within a tight cluster issue a move command to a clear destination
- **THEN** exactly one sub-cell A* runs (the leader's); 19 LoS checks pass and 19 follower paths are constructed by copy-and-substitute-endpoint

#### Scenario: Spread-out group falls back per-unit when needed

- **WHEN** half a selection sits on either side of an impassable wall
- **THEN** followers whose LoS to the leader's first waypoint is blocked fall back to their own `assignUnitPathWithSubBlocked` call; followers with clear LoS use the shared path

#### Scenario: Single-unit command bypasses the helper

- **WHEN** any group helper is invoked with one unit
- **THEN** the helper calls `assignUnitPathWithSubBlocked` directly without leader-selection logic

#### Scenario: Leader can't path → group falls back

- **WHEN** the leader's A* returns no waypoints
- **THEN** every follower runs its own per-unit `assignUnitPathWithSubBlocked`

### Requirement: Leader-follower paths for shared-target attacks

`AttackWithUnits` SHALL route shared-`OrderID` attacker groups via `assignAttackGroupPathsLocked`: only units currently out of attack range require pathing; among those, the leader is the unit with the **shortest** `AttackRange` (its stop point is the deepest along the approach corridor). The leader runs `assignAttackApproachPathLockedWithSubBlocked` normally. Each follower then walks the leader's path waypoints to find the first one within its own `AttackRange` of the target — that waypoint becomes the follower's stop. The follower's path is the leader's path truncated at that index, copied (not slice-shared) so per-unit Path mutations during movement do not affect the leader.

A LoS sample from each follower's start to the leader's first waypoint gates the reuse; on failure that follower runs its own `assignAttackApproachPathLockedWithSubBlocked`.

When the leader cannot path (drift mode or no route), the helper SHALL fall back to per-unit attack-approach pathing for every other attacker.

The existing "per-unit natural formation by AttackRange" behaviour SHALL be preserved — ranged units still stop at their own range, melee still pushes into engagement.

#### Scenario: Mixed-range group shares one A*

- **WHEN** 10 melee + 10 archer units right-click an enemy unit out of range
- **THEN** the leader is a melee unit (shortest range), one sub-cell A* runs, and each archer's path is the leader's path truncated at the archer's range from the target

#### Scenario: In-range attackers skipped

- **WHEN** some attackers are already within `AttackRange` of the target
- **THEN** those units do not enter `needsPath` and no path computation runs for them; their `AttackTargetID` / anchor are still set by the caller

#### Scenario: Leader couldn't path → followers run own A*

- **WHEN** the shortest-range unit's attack-approach pathing fails (leader.Moving is false after the call)
- **THEN** every other attacker runs `assignAttackApproachPathLockedWithSubBlocked` individually

#### Scenario: LoS blocked → that follower runs own A*

- **WHEN** a single follower's start position has no line-of-sight to the leader's first waypoint
- **THEN** that one follower runs its own attack-approach pathing; the rest still use the shared leader path
