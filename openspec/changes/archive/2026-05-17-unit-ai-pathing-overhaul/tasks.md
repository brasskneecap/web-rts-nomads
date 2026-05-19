## 1. Investigation & Audit

- [x] 1.1 Read `pathing.go`, `state_movement.go`, `debug_path.go` in full to confirm current stuck-watchdog threshold location and repath call sites
- [x] 1.2 Read `combat_ai.go` to confirm unreachable-target memo fields (`UnreachableTargetID`, `UnreachableUntilTick`) and where they are set/cleared
- [x] 1.3 Read `combat_ai_retreat.go` fully — confirm the two failure sites where `unit.UnreachableTargetID` is set
- [x] 1.4 Read `combat_ai_profiles.go` to confirm `RetargetIntervalTicks` field name and where `guardReturnGraceTicks` constant is defined
- [x] 1.5 Locate `assignEnemyObjectiveLocked` and `NextObjectiveSearchTick` in `combat_ai.go` and confirm the per-unit throttle pattern at `s.Tick < unit.NextObjectiveSearchTick`

## 2. Unit Struct & State Fields

- [x] 2.1 ~~Add `UnreachableStrikeCount int`~~ — superseded by drift mode; field never landed (see section 11)
- [x] 2.2 Add `UnreachableBuildingStrikeCount int` to `Unit` for building-target escalation
- [x] 2.3 Add `PathDiagnostics` struct with `RepathCount`, `StuckTriggerCount`, `LastStuckTick`
- [x] 2.4 Embed `PathDiagnostics` on `Unit`
- [x] 2.5 Add `nextGlobalObjectiveSearchTick int` field to `GameState`

## 3. Speed-Aware Stuck Watchdog

- [x] 3.1 In `state.go` movement loop, capture `perkMoveSpeedMultiplierLocked` once per unit iteration and reuse for both the stuck threshold and the movement step
- [x] 3.2 Increment `unit.PathDiagnostics.StuckTriggerCount` and set `unit.PathDiagnostics.LastStuckTick` when the stuck condition fires
- [x] 3.3 Increment `unit.PathDiagnostics.RepathCount` at the top of `repathUnitLocked()` in `state_movement.go`

## 4. Stun-Expiry Repath

- [x] 4.1 Detect stun-expiry transition via `wasStunned := unit.StunnedRemaining > 0` before the decrement
- [x] 4.2 Add the repath trigger after stun decay and before the stun-gate `continue`
- [x] 4.3 Verify the stun-gate `continue` still correctly skips movement for units where stun has NOT yet expired

## 5. Unreachable Target Escalation — Building Targets

- [x] 5.1 Add `applyBuildingUnreachableEscalationLocked` with tiered backoff (40 → 120 → drop + objective fallback)
- [x] 5.2 Call from initial-acquisition site in `applyCombatTargetLocked` (when `findBestBuildingAttackPositionLocked` returns nil); clear `AttackBuildingTargetID = ""` in same branch
- [x] 5.3 Inline equivalent escalation in `assignEnemyObjectiveLocked` (when its own `findBestBuildingAttackPositionLocked` returns nil); strike-3 falls through to townhall
- [x] 5.4 Reset `UnreachableBuildingStrikeCount` / `UnreachableBuildingTargetID` at success paths in both `applyCombatTargetLocked` and `assignEnemyObjectiveLocked`
- [x] 5.5 Memo-aware nearest-building search in `state_waves.go` (`findNearestAttackablePlayerBuildingLocked`, `findNearestAttackableBuildingForPlayerLocked`)

## 6. Guard Grace Window Fix

- [x] 6.1 Replace `guardReturnGraceTicks` constant usage with `resolveCombatProfile(unit).RetargetIntervalTicks + 5`
- [x] 6.2 Remove the `guardReturnGraceTicks = 20` constant from `combat_ai.go` (no longer referenced)

## 7. Global Objective Search Rate Limiter

- [x] 7.1 Replace the per-unit-only throttle in `combat_ai.go` with the two-level gate (per-unit + global); per-unit backoff stays inside the success path
- [x] 7.2 Strike-3 fallback calls `assignEnemyObjectiveLocked` directly to bypass the global gate

## 8. Pathing Diagnostics & Snapshot

- [x] 8.1 Add `PathDiagnostics` sub-fields to `UnitSnapshot` in `server/pkg/protocol/messages.go` with `omitempty` JSON tags; add `PersistentlyStuckUnits []int` to `MatchSnapshotMessage`
- [x] 8.2 Populate the new fields in all three snapshot functions (`Snapshot`, `SnapshotForPlayer`, `snapshotUnfilteredLocked`); compute `PersistentlyStuckUnits` via the new `persistentlyStuckUnitsLocked` helper
- [x] 8.3 Per-wave reset: zero out `PathDiagnostics` and `UnreachableBuildingStrikeCount` for all units at the prep → active transition

## 9. Live-Profile Investigation (post-initial-implementation)

- [x] 9.1 Add `WEBRTS_TICK_PROFILE=1` to `server/dev.bat` so the existing tick profiler runs in dev
- [x] 9.2 Wrap player command handlers (`MoveUnits`, `AttackMoveUnits`, `AttackWithUnits`, `PatrolUnits`, `SetUnitStance`, `GatherWithUnits`) with `defer profileStart("cmd.*")()`

## 10. In-Combat A* Storm Fix — Unit-vs-Building

Found via profiling: `tickUnitCombatLocked`'s unit-vs-building branch ran a full sub-cell A* every tick when a unit's target was unreachable mid-combat, regardless of the initial-acquisition memo system.

- [x] 10.1 Wrap the retry `assignUnitPath` call in `state_combat.go` (unit-vs-building branch) with `NextApproachRepathTick` cooldown using `approachRepathCooldownTicks` (3 ticks)
- [x] 10.2 On retry failure, call `applyBuildingUnreachableEscalationLocked` so the AI eventually drops the target (since `shouldDropCurrentTargetLocked` is sticky on building targets)
- [x] 10.3 On retry success, reset `NextApproachRepathTick = 0`

## 11. Drift Mode — Unit-vs-Unit Unreachable Targets

Replaced the original strike-count escalation for unit targets (Decision 1 in `design.md`). Strike escalation converged correctly but burned A* budget during the 9-tick cycle; drift replaces every per-tick A* with one walkability check.

- [x] 11.1 Add `Unit.AttackDrifting bool` field
- [x] 11.2 Add `enterAttackDriftLocked(unit, target)` helper in `combat_ai_retreat.go`
- [x] 11.3 In `assignAttackApproachPathLocked`, call `enterAttackDriftLocked` instead of `applyUnitUnreachableEscalationLocked` at the two failure sites
- [x] 11.4 Add drift-movement branch in `state.go` per-unit loop: when `Moving && len(Path)==0 && AttackDrifting`, step toward `TargetX/Y`, halt on wall or arrival
- [x] 11.5 Gate `refreshUnitAttackApproachLocked` on `AttackDrifting && !force` so per-tick combat tick doesn't re-fire A* against a moving target
- [x] 11.6 Clear `AttackDrifting` in `clearCombatTargetLocked`, `resetUnitMovementLocked`, on successful `assignUnitPathWithSubBlocked` inside `assignAttackApproachPathLocked`, and when the movement loop sets `Moving = false`
- [x] 11.7 Delete `applyUnitUnreachableEscalationLocked` and the `UnreachableTargetID` / `UnreachableStrikeCount` fields; remove unit-target memo checks from `combat_ai_scoring.go`
- [x] 11.8 Delete `combat_ai_unreachable_test.go` (validated the removed system)

## 12. Per-Unit Re-Evaluation Cooldown

Found via profiling: enemy units cycled rapidly through unreachable buildings because the single-slot `UnreachableBuildingTargetID` memo overwrote itself on each pick.

- [x] 12.1 Add `Unit.NextCombatEvalTick int` field
- [x] 12.2 Set the cooldown in `evaluateCombatLocked` after `applyCombatTargetLocked` when the unit ends without target and not Moving
- [x] 12.3 Honor the cooldown at the top of `evaluateCombatLocked`'s no-target evaluate branch

## 13. Shared Sub-Cell Blocked Map for Group Commands

Found via profiling: `cmd.MoveUnits` averaged 54ms (max 566ms) on K-unit groups because each unit rebuilt the sub-cell blocked map from scratch.

- [x] 13.1 Add `assignUnitPathWithSubBlocked` (state_movement.go) — optional precomputed `subBlocked` parameter; preserve `assignUnitPath` as wrapper passing nil
- [x] 13.2 Add `assignAttackApproachPathLockedWithSubBlocked` (combat_ai_retreat.go) — same pattern
- [x] 13.3 Add `buildGroupSubBlockedLocked(units, blocked) (ground, flyer)` helper
- [x] 13.4 Wire `MoveUnits`, `AttackMoveUnits`, `PatrolUnits`, `AttackWithUnits` to build the per-plane map once and pass it through to each unit's path call

## 14a. Target-Drop Retarget Stagger

Found via profiling after the leader-follower work: a single wave-clear tick produced `unitCombat max = 123 ms` and `combatAI.evaluate max = 63 ms` because ~10 player units all lost their (just-killed) targets the same tick and ran `selectBestTargetLocked` + `applyCombatTargetLocked` together. Spreading via `unit.ID % retargetStaggerTicks` flattens the spike.

- [x] 14a.1 Add the `retargetStaggerTicks = 5` constant near the other combat-tuning constants in `combat_ai.go`
- [x] 14a.2 In `evaluateCombatLocked`, after `shouldDropCurrentTargetLocked` triggers `clearCombatTargetLocked`, set `unit.NextCombatEvalTick = s.Tick + (unit.ID % retargetStaggerTicks)` so the existing no-target gate spreads re-acquisitions
- [x] 14a.3 Do NOT add the stagger to the retreat-clear or strike-3-escalation paths (they immediately give the unit a new path/objective; staggering would idle them unnecessarily)
- [x] 14a.4 Verify symmetry: stagger applies to both player and enemy units (both flow through the same `evaluateCombatLocked` site)

## 14. Leader-Follower Group Pathing

After the shared-map fix, the remaining cost was K sub-cell A*s per command. Leader-follower drops this to one in the common (tight-cluster) case.

- [x] 14.1 Add `lineWalkableLocked(startX, startY, endX, endY, blocked, flyer)` LoS helper
- [x] 14.2 Add `assignGroupPathsLocked(units, destinations, blocked, groundSub, flyerSub)` helper for move/attack-move/patrol
- [x] 14.3 Wire `MoveUnits`, `AttackMoveUnits`, `PatrolUnits` to use the helper
- [x] 14.4 Add `assignAttackGroupPathsLocked(attackers, target, blocked, groundSub, flyerSub)` for shared-target attacks; leader = shortest-AttackRange unit; followers truncate at their own range
- [x] 14.5 Wire `AttackWithUnits` to use the attack-group helper

## 15. Verification

- [x] 15.1 `go build ./...` clean
- [x] 15.2 `go test ./...` — only pre-existing heal failures (`TestHeal_RestoresHPAndDeductsMana`, `TestHeal_UninterruptibleByDamage`, `TestAbility_HealLoadsFromCatalog`) reproduce on baseline; unrelated to pathing changes
- [x] 15.3 Live profile under `WEBRTS_TICK_PROFILE=1` after each fix; verified each storm vector resolved (final `unitCombat avg 1.2ms`, `cmd.MoveUnits avg 0.8ms`)
- [ ] 15.4 Manual playthrough: large enemy group at a chokepoint behind a building — verify units do not get permanently stuck
- [ ] 15.5 Manual playthrough: stun a moving unit mid-path; verify it repaths on expiry (no wall-walking)
- [ ] 15.6 Manual playthrough: confirm `PathDiagnostics` and `PersistentlyStuckUnits` populate correctly in dev-tools snapshot for a stuck unit
- [ ] 15.7 Manual playthrough: 20+ unit selection move/attack — confirm click hitch is gone
