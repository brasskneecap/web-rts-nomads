# unreachable-target-escalation Specification

## Purpose

Defines the building-target unreachability escalation system: tiered strike-count
backoff on A* failure, the in-combat building retry throttle, objective searches
skipping memoised unreachable buildings, and clearing the building target on
initial-acquisition nil-pos. (Unit-target A* failure escalation is now handled by
drift mode in the `unit-movement` capability — the prior strike-count system for
unit targets has been removed.)

## Requirements

### Requirement: Building target A* failures escalate through strike count tiers

When a unit's A* to its current building attack target returns no path, the unit SHALL increment `UnreachableBuildingStrikeCount` against `UnreachableBuildingTargetID` and apply tiered backoff:

- Strike 1: write `UnreachableUntilTick = s.Tick + 40` (existing cooldown).
- Strike 2: write `UnreachableUntilTick = s.Tick + 120`.
- Strike 3+: call `clearCombatTargetLocked` to drop the target, reset `UnreachableBuildingStrikeCount = 0`, and (when the unit is not in `GuardMode` and not on `OrderHold`) call `assignEnemyObjectiveLocked` directly to pick a fallback objective.

The escalation SHALL fire from two sites: `applyCombatTargetLocked` (initial acquisition, when `findBestBuildingAttackPositionLocked` returns nil) and `tickUnitCombatLocked`'s unit-vs-building retry branch (steady-state, when `assignUnitPath` fails after a previously-valid target).

#### Scenario: First strike uses the existing cooldown

- **WHEN** A* returns nil for a unit's current building target for the first time
- **THEN** `UnreachableBuildingTargetID` is set, `UnreachableBuildingStrikeCount = 1`, and `UnreachableUntilTick = s.Tick + 40`

#### Scenario: Second strike triples the cooldown

- **WHEN** the same building fails A* a second time while still memoised
- **THEN** `UnreachableBuildingStrikeCount = 2` and `UnreachableUntilTick = s.Tick + 120`

#### Scenario: Third strike clears the target and falls through to objective

- **WHEN** the same building fails A* a third time and the unit is not in `GuardMode` and not on `OrderHold`
- **THEN** `clearCombatTargetLocked` is called, `UnreachableBuildingStrikeCount` is reset to 0, and `assignEnemyObjectiveLocked` runs to pick a fallback objective

#### Scenario: Guard unit on strike 3 returns to anchor instead of hunting

- **WHEN** a `GuardMode` unit hits its third building strike
- **THEN** `clearCombatTargetLocked` is called, strike count resets, and `assignEnemyObjectiveLocked` is NOT called — `tickGuardReturnLocked` walks the unit back to its anchor

#### Scenario: Successful path resets the strike count

- **WHEN** A* returns a non-nil path to any building target
- **THEN** `UnreachableBuildingStrikeCount` is set to 0 and `UnreachableBuildingTargetID` is cleared

### Requirement: In-combat building retry throttle

`tickUnitCombatLocked`'s unit-vs-building branch SHALL gate its retry `assignUnitPath` call by `NextApproachRepathTick`. When the retry fails (unit not Moving afterwards), the cooldown is set to `s.Tick + approachRepathCooldownTicks` (3 ticks). On the same failure path the throttle SHALL also invoke `applyBuildingUnreachableEscalationLocked` so the AI eventually drops the target — `shouldDropCurrentTargetLocked` is intentionally sticky on building targets and would otherwise loop forever.

When the retry succeeds (unit Moving), `NextApproachRepathTick` is reset to 0.

#### Scenario: First retry runs at most once per 3 ticks

- **WHEN** a unit holding a building target is out of attack range and `Moving = false`
- **THEN** at most one `assignUnitPath` call fires per `approachRepathCooldownTicks` ticks for that unit, regardless of how many ticks elapse

#### Scenario: Successful retry clears the cooldown

- **WHEN** the retry's `assignUnitPath` succeeds and sets `Moving = true`
- **THEN** `NextApproachRepathTick` is reset to 0 so subsequent failures (e.g. mid-route obstruction) retry immediately

#### Scenario: Repeated failures escalate within bounded ticks

- **WHEN** a building target is genuinely unreachable and the throttled retry fires three times
- **THEN** `applyBuildingUnreachableEscalationLocked` reaches strike 3 within 9 ticks and the target is cleared (no indefinite per-tick A* storm)

### Requirement: Objective search skips memoised unreachable buildings

`findNearestAttackablePlayerBuildingLocked` and `findNearestAttackableBuildingForPlayerLocked` SHALL skip any candidate building whose ID matches the requesting unit's `UnreachableBuildingTargetID` while `s.Tick < UnreachableUntilTick`. Without this, the strike-3 fallback's `assignEnemyObjectiveLocked` call picks the same just-blacklisted building again, restarting the escalation at strike 1 — an infinite cycle.

#### Scenario: Nearest-building search ignores blacklisted candidate

- **WHEN** an enemy unit calls `findNearestAttackablePlayerBuildingLocked` and the geometrically-closest building's ID equals `enemy.UnreachableBuildingTargetID` with `s.Tick < enemy.UnreachableUntilTick`
- **THEN** the function returns the next-nearest building instead, or nil if no others qualify (allowing the townhall-fallback path)

### Requirement: Initial-acquisition nil-pos clears the building target

When `applyCombatTargetLocked` selects a building but `findBestBuildingAttackPositionLocked` returns nil, the function SHALL clear `unit.AttackBuildingTargetID = ""` and call `applyBuildingUnreachableEscalationLocked` instead of leaving `AttackBuildingTargetID` set. Leaving the field set causes `tickUnitCombatLocked` to re-path to stale `TargetX/Y` coordinates indefinitely.

Similarly, `assignEnemyObjectiveLocked` SHALL only set `unit.AttackBuildingTargetID = building.ID` inside the `pos != nil` branch — never write it speculatively.

#### Scenario: Nil-pos branch in applyCombatTargetLocked clears the target

- **WHEN** `applyCombatTargetLocked` selects a building and `findBestBuildingAttackPositionLocked` returns nil
- **THEN** `unit.AttackBuildingTargetID` is cleared to `""` and `applyBuildingUnreachableEscalationLocked` is called

#### Scenario: assignEnemyObjectiveLocked only writes target on success

- **WHEN** `assignEnemyObjectiveLocked` runs and `findBestBuildingAttackPositionLocked` returns nil for the picked building
- **THEN** `unit.AttackBuildingTargetID` is NOT set; the function writes the unreachable memo and either falls through to the townhall path (strike 3) or returns
