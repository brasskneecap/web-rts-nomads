# Design Review: Player Order Semantics for WebRTS

## Overview

This document reviews the existing combat/AI/targeting systems in the authoritative Go server, audits the current `ManualAttackTarget` flow against the rules in `AI_RULES.md`, and proposes a unified design for player order semantics: a sticky **attack-target** order, clean **retarget**, **command-supersedes-attack**, and a complete set of standing orders / stances (**Patrol**, **Hold**, **Move-and-Attack**, **Force-Move**). This is RTS-paradigm work: a 20 Hz tick ([loop.go:25,32](../../server/internal/game/loop.go#L25)) deterministic simulation under a single `s.mu` lock, with intent messages over WS.

## Assumptions & Constraints

- Tick rate is fixed at 20 Hz (`Loop.ticker = time.NewTicker(50 * time.Millisecond)`). Order semantics must produce identical outcomes given the same intent stream and seed.
- All target references must remain ID-based per `AI_RULES.md`; new fields holding live target/anchor information follow the same store-by-ID, resolve-each-tick rule.
- Single state lock. All command entry points (`MoveUnits`, `AttackWithUnits`, `AttackMoveUnits`, future `SetStance`, `Patrol`, `ForceMove`) acquire `s.mu` at the top and hand off to `*Locked` helpers (existing pattern: [state_combat.go:8-10](../../server/internal/game/state_combat.go#L8-L10), [state_movement.go:9-11](../../server/internal/game/state_movement.go#L9-L11)).
- Intent ordering stability: WS messages already arrive single-reader per client ([handlers.go:56-455](../../server/internal/ws/handlers.go#L56-L455)), and the simulation drains them via direct `match.State.X(...)` calls at WS read time. There is no separate intent queue; a command applies to the next `Update(dt)` because WS handlers and the tick loop both take `s.mu`. We keep that model — no need to introduce a queue for this work.
- Single-player vs lockstep: the codebase is currently authoritative-server, not lockstep peer-to-peer. We do not need command-tick scheduling. Sticky orders are simply unit-resident state.
- The client is dumb: it sends intents and renders snapshots. No client-side simulation of stance behavior.

## Audit: Current `ManualAttackTarget` Flow

The rules doc claims:
> Player-issued attack commands bypass the AI's retarget/leash/retreat logic as long as the target is still valid.

**Verdict: mostly correct, with two real gaps.**

### What works

- Set on player intent: [state_combat.go:30](../../server/internal/game/state_combat.go#L30) (`AttackWithUnits` sets `ManualAttackTarget = true`). Anchor is centered on target, not unit ([state_combat.go:31-36](../../server/internal/game/state_combat.go#L31-L36)), so the leash is biased toward where the player pointed.
- Retarget suppression: [combat_ai.go:150-152](../../server/internal/game/combat_ai.go#L150-L152) short-circuits `evaluateCombatLocked` when `ManualAttackTarget && AttackTargetID != 0`. This skips score-based retargeting and the retreat check.
- Leash bypass on validity drop: [combat_ai_scoring.go:14-19](../../server/internal/game/combat_ai_scoring.go#L14-L19) returns "do not drop" when `ManualAttackTarget` is set, so a chasing unit will not abandon a player-assigned target merely because it has crossed the anchor's leash radius.
- Cleared on validity drop: `shouldDropCurrentTargetLocked` returns true for nil/dead/invisible/own-team, and `clearCombatTargetLocked` ([combat_ai.go:216-225](../../server/internal/game/combat_ai.go#L216-L225)) wipes both `AttackTargetID` and `ManualAttackTarget`.
- Cleared on subsequent player order: `resetUnitMovementLocked` ([state_movement.go:232-257](../../server/internal/game/state_movement.go#L232-L257)) clears both flags. Every order entry point routes through this.

### Gap 1: `tickUnitCombatLocked` clears `ManualAttackTarget` on the wrong condition

[state_combat.go:99-104](../../server/internal/game/state_combat.go#L99-L104):
```go
if target == nil || !target.Visible {
    unit.AttackTargetID = 0
    unit.ManualAttackTarget = false
    ...
}
```

This branch fires when a target is invalid at combat-resolution time. The `Visible` check is fine, but the **scoring/AI side** drops on `target.HP <= 0` and `OwnerID == unit.OwnerID` too ([combat_ai_scoring.go:11](../../server/internal/game/combat_ai_scoring.go#L11)). Combat-resolution should match the same predicate set, otherwise:
- A unit whose target died this tick (HP == 0 but not yet removed) will still execute `unit.AttackTargetID != 0` branch, attempt to read distance, then on the next tick the cleanup happens via `removeUnitLocked`. Functionally fine but the divergence is a footgun. **Recommendation: extract a shared `combatTargetIsValidLocked(unit, target)` helper and call it from both [state_combat.go:99-104](../../server/internal/game/state_combat.go#L99-L104) and [combat_ai_scoring.go:11-13](../../server/internal/game/combat_ai_scoring.go#L11-L13). Single source of truth for "is this target still valid?".**

### Gap 2: Pathing failure can strand a sticky-attacking unit silently

`assignUnitPath` ([state_movement.go:129-180](../../server/internal/game/state_movement.go#L129-L180)) sets `unit.Path = nil; unit.Moving = false` when `findNearestWalkableAvailable` or `findPath` returns empty. `applyCombatTargetLocked` calls `refreshUnitAttackApproachLocked` ([combat_ai.go:201](../../server/internal/game/combat_ai.go#L201)) which calls `assignUnitPath`. If the path build fails, `AttackTargetID` and `ManualAttackTarget` remain set, but the unit has no path and `Moving == false`. On the next combat tick, `tickUnitCombatLocked` checks distance — if out of range, it calls `refreshUnitAttackApproachLocked` again with `force=true` ([state_combat.go:158](../../server/internal/game/state_combat.go#L158)), so it does retry. **This is fine for transient blockages**, but if the target is permanently unreachable (other side of an obstacle, surrounded by allies blocking the closest cells), the unit will burn a re-path attempt every tick and report `Status = "Moving To Attack"` while standing still. Cosmetic, not a correctness bug, but worth noting as a future polish.

### Gap 3 (terminology only): `ManualMove` and `ManualAttackTarget` are loosely coupled

`ManualMove` is set by `MoveUnits` and consumed in two places:
- [combat_ai.go:129-131](../../server/internal/game/combat_ai.go#L129-L131): skips the entire combat evaluation pass for a unit moving under a manual move order with no current attack target. This is the de-facto **Force-Move** behavior, but only for the duration of a single move command, and it is implicitly cleared the moment the unit stops ([state.go:668, 674](../../server/internal/game/state.go#L668)).
- [state.go:668, 674](../../server/internal/game/state.go#L668): cleared on stop.

So today the flag set `{ManualMove, ManualAttackTarget}` is doing the work of an order/stance enum: there is implicit Move (`ManualMove=true, AttackTarget=0`), implicit A-Move (no flags via `AttackMoveUnits` which intentionally does NOT set `ManualMove` — see [state_movement.go:70-127](../../server/internal/game/state_movement.go#L70-L127) vs [state_movement.go:9-68](../../server/internal/game/state_movement.go#L9-L68)), and implicit Sticky-Attack (`ManualAttackTarget=true`).

This works but is fragile. Adding Patrol and Hold without a unifying enum will multiply the boolean matrix and make the AI gating ([combat_ai.go:118-133](../../server/internal/game/combat_ai.go#L118-L133)) hard to reason about.

---

## Proposed Design

### Core data model: introduce `OrderType` + `OrderState`, fold `ManualMove`/`ManualAttackTarget` into it

Add to `Unit` (in [state.go](../../server/internal/game/state.go)):

```go
type OrderType int
const (
    OrderIdle           OrderType = iota // no standing order; default acquisition
    OrderMove                            // force-move: ignore enemies en route
    OrderAttackMove                      // a-move: break off to engage acquired enemies
    OrderAttackTarget                    // sticky attack on AttackTargetID/AttackBuildingTargetID
    OrderHold                            // do not move; engage in-range enemies only
    OrderPatrol                          // cycle waypoints; engage acquired enemies; resume
)

type OrderState struct {
    Type         OrderType
    // Destination for Move / AttackMove. Re-used as patrol "current waypoint".
    DestX, DestY float64
    // Patrol-only: the OTHER endpoint of the patrol leg. PatrolWaypoints can
    // grow to an []Vec2 later if N-point patrol is wanted.
    PatrolReturnX, PatrolReturnY float64
    // Patrol/AttackMove resume-anchor: when combat ends, unit returns toward
    // OrderState.DestX/Y (or PatrolReturn for patrol).
    // For Hold this is the hold position the unit returns to if shoved.
    HoldX, HoldY float64
}
```

And on Unit:
```go
Order OrderState
```

**Removed fields**: `ManualMove bool`, `ManualAttackTarget bool`. Their behavior is fully expressible via `Order.Type`.

**Kept as-is**: `AttackTargetID int`, `AttackBuildingTargetID string`, `CombatAnchorX/Y`, `Path`, `TargetX/Y`, `Moving`. These remain the low-level simulation state.

#### Why a single enum, not parallel flags

The current `ManualMove`/`ManualAttackTarget` pair forces the gating logic in [combat_ai.go:118-133](../../server/internal/game/combat_ai.go#L118-L133) and [combat_ai.go:150-152](../../server/internal/game/combat_ai.go#L150-L152) to enumerate combinations. Patrol and Hold add at least two more behaviors. With six order types and one or two state fields, every combat/movement decision has a single switch on `Order.Type` instead of N booleans.

**It also collapses the "what is this unit doing right now from the player's intent" question to one read.** That field becomes the contract that QA writes assertions against and the renderer can display in a tooltip.

### Order-to-AI behavior table

| Order | Combat AI evaluation | Leash behavior | Retreat allowed | Retarget allowed | On combat end |
|---|---|---|---|---|---|
| Idle | Yes | Profile leash, anchor = current pos (slides for enemies, see [combat_ai.go:111-116](../../server/internal/game/combat_ai.go#L111-L116)) | Yes | Yes | Stay |
| Move | Skipped entirely if no target | N/A | N/A | N/A | Continue path |
| AttackMove | Yes; if target acquired, engage | Profile leash, anchor = `DestX/Y` (the move destination) | Yes (existing) | Yes | Resume path toward `DestX/Y` |
| AttackTarget | Skipped (sticky) while target valid | Bypassed (existing behavior) | No (existing behavior) | No (existing behavior) | Order completes; demote to Idle |
| Hold | Yes, but only acquires targets currently in `AttackRange`; no chase | N/A — never moves to engage | No (cannot retreat, would break Hold) | Yes (within in-range set) | Stay at hold position |
| Patrol | Yes; if target acquired, engage | Profile leash, anchor = current patrol waypoint (`DestX/Y`) | Yes | Yes | Resume movement toward current waypoint; on arrival, swap waypoints |

### State machine: where each order hooks into existing code

The existing tick is:
1. `tickCombatAILocked` ([combat_ai.go:92-134](../../server/internal/game/combat_ai.go#L92-L134)): retargeting / scoring / leash / retreat.
2. `tickUnitCombatLocked` ([state_combat.go:92-235](../../server/internal/game/state_combat.go#L92-L235)): firing + per-target movement maintenance.
3. Movement step in `Update` ([state.go:667-716](../../server/internal/game/state.go#L667-L716)): walking `Path`.

The new `Order` field is consulted in three new gates:

#### Gate A: `tickCombatAILocked` — should this unit even evaluate combat?

Replace the current branch at [combat_ai.go:118-133](../../server/internal/game/combat_ai.go#L118-L133):

- `OrderMove`: continue (skip combat eval entirely, including `decayThreatLocked` for this unit's own targets — but be careful: cross-unit threat additions from `addThreatLocked` callers in `combat_ai_threat.go` still need to write to this unit's table because attackers hitting it need their threat recorded. **Decision: still call `decayThreatLocked` for force-moving units (it's cheap and preserves the threat table for when the order ends), just skip `evaluateCombatLocked`.**)
- `OrderAttackTarget`: existing short-circuit at [combat_ai.go:150-152](../../server/internal/game/combat_ai.go#L150-L152) keyed on `Order.Type == OrderAttackTarget && AttackTargetID != 0` instead of the old bool.
- `OrderHold`: enter `evaluateCombatLocked` but with a hold-aware `selectBestTargetLocked` filter (see Gate B). Suppress retreat (`shouldRetreatLocked`) — Hold doesn't retreat.
- `OrderAttackMove` / `OrderPatrol` / `OrderIdle`: full evaluation.

#### Gate B: `selectBestTargetLocked` — Hold filters to in-range only

Add at the top of `selectBestTargetLocked` ([combat_ai_scoring.go:47](../../server/internal/game/combat_ai_scoring.go#L47)):
- If `unit.Order.Type == OrderHold`, ignore `profile.DetectionRange` and only iterate hostiles within `unit.AttackRange`. Same scoring; no changes to weights.
- All other orders: unchanged.

#### Gate C: `applyCombatTargetLocked` / `tickUnitCombatLocked` — Hold never assigns an approach path

In `applyCombatTargetLocked` ([combat_ai.go:193-214](../../server/internal/game/combat_ai.go#L193-L214)): if `unit.Order.Type == OrderHold`, do **not** call `refreshUnitAttackApproachLocked` and do **not** call `assignUnitPath` for buildings. The unit just sets the target and fires from current position next tick.

In `tickUnitCombatLocked` ([state_combat.go:153-162](../../server/internal/game/state_combat.go#L153-L162)): if target is out of range AND `Order.Type == OrderHold`, drop the target instead of pathing toward it.

#### Gate D: AttackMove / Patrol "resume after combat"

When a unit on AttackMove or Patrol kills its current target (or its target dies / leaves leash), `clearCombatTargetLocked` runs. We need a follow-up: if `Order.Type == OrderAttackMove || OrderPatrol`, re-issue movement toward `Order.DestX/Y` if the unit is not already there.

Cleanest hook: at the end of `evaluateCombatLocked`, when `best.Kind == combatTargetNone` and `unit.AttackTargetID == 0 && unit.AttackBuildingTargetID == ""`, check `Order.Type` and call a new `resumeStandingOrderLocked(unit, blocked)` helper. For Patrol, also handle waypoint flip when distance to current `DestX/Y` is below an arrival threshold.

#### Patrol waypoint flip

A small piece of logic in `state.go` `Update` movement block (or in `resumeStandingOrderLocked`):

```
if Order.Type == OrderPatrol &&
   distance(unit, Order.DestX, Order.DestY) < arrivalRadius &&
   no current attack target {
       swap (Order.DestX/Y, Order.PatrolReturnX/Y)
       reissue path toward new DestX/Y
}
```

`arrivalRadius` ~ 16-24 px (same magnitude as the simplification thresholds in `simplifyLeadingWaypoints`).

### Interaction with leash / retreat

- **Leash**: `targetInsideLeashLocked` ([combat_ai_retreat.go:116-121](../../server/internal/game/combat_ai_retreat.go#L116-L121)) reads `unit.CombatAnchorX/Y`. We continue to use that anchor; the order entry points set it appropriately:
  - Move/AttackMove: anchor = destination (already done at [state_movement.go:36-37,60-61,96-97,119-120](../../server/internal/game/state_movement.go#L36)).
  - AttackTarget: anchor = target position (already done at [state_combat.go:35-36](../../server/internal/game/state_combat.go#L35-L36)).
  - Hold: anchor = hold position (set at order entry).
  - Patrol: anchor = current waypoint, **updated on each waypoint flip**.
- **Retreat**: `shouldRetreatLocked` is gated by `Order.Type` in Gate A: skipped for Move, AttackTarget (existing), Hold (new). Allowed for Idle, AttackMove, Patrol.

### Player command transitions (defaults consistent with RTS conventions)

Right-click world → existing `MoveUnits` path. Recommend defaults consistent with how today's code already differentiates:

- **Plain right-click on ground** (no modifier): `OrderMove` (force-move). Today's code calls `MoveUnits` and sets `ManualMove = true` which already produces force-move semantics in [combat_ai.go:129](../../server/internal/game/combat_ai.go#L129). Keeping this default preserves current player muscle memory.
- **Plain right-click on enemy**: `OrderAttackTarget` (existing `AttackWithUnits`). Today's behavior, sticky.
- **`A` then click ground**: `OrderAttackMove`. Today's `attack_move_command` path.
- **`A` then click enemy**: `OrderAttackTarget`. Same as direct right-click on enemy. Already the behavior in [GameClient.ts:289-300](../../client/src/game-portal/src/game/core/GameClient.ts#L289-L300).
- **`H` (no click)**: `OrderHold` immediately at current position. New.
- **`P` then two clicks** (or `P` + click for one waypoint, with current pos as the other): `OrderPatrol`. New.
- **Shift-click anything**: queued order, **out of scope for this design** — flag in Open Questions.
- **Force-move modifier**: today, default right-click is already force-move (`ManualMove = true`). Recommend keeping that as-is. **No separate modifier key needed**, but if the user wants to flip the default to AttackMove (some RTS do this), the modifier would be a single shift-toggle in `InputManager.onRightClick`.

### Sticky-order audit for the new design

Carry the `AI_RULES.md` invariants forward:

- `Order.DestX/Y`, `HoldX/Y`, `PatrolReturnX/Y` are floats, not pointers. No targeting-ID fields are added by this design — the only IDs used are the existing `AttackTargetID` and `AttackBuildingTargetID`, which already follow the rules.
- **No new "the unit I'm patrolling to defend" or "the enemy I'm currently engaging from patrol" field is needed.** `AttackTargetID` already captures the latter and is resolved each tick. If a future "guard unit X" order lands, that ID **must** be a `GuardTargetID int` resolved each tick via `getUnitByIDLocked` with the standard nil/HP/Visible/ownership guard.
- Sticky-order behavior (Patrol/AttackMove) is implemented as a state machine over `Order.Type` plus `AttackTargetID`. Both are tick-deterministic (no map iteration order, no wall-clock). No new RNG draws are needed; if a future design wants randomized patrol jitter, draw from `s.rngSpawn` (or add a fourth named stream `rngOrders`).

### `clearCombatTargetLocked` and `resetUnitMovementLocked` updates

`clearCombatTargetLocked` ([combat_ai.go:216-225](../../server/internal/game/combat_ai.go#L216-L225)) currently sets `Status = "Idle"` when not moving. With orders, after clearing the target it should NOT demote `Order.Type` — Patrol and AttackMove want to stay in their order and resume. The demote-to-Idle only happens when:
- `Order.Type == OrderAttackTarget` and target became invalid → `Order.Type = OrderIdle` (player's commit is over).
- The order's destination is reached (Move, AttackMove) and no target is being engaged → `Order.Type = OrderIdle`.

`resetUnitMovementLocked` ([state_movement.go:232-257](../../server/internal/game/state_movement.go#L232-L257)) is the universal "new player command, wipe prior state" routine. It must clear `Order.Type` to `OrderIdle` (callers will then set the appropriate new `Order.Type`). The existing `AttackTargetID = 0`, `AttackBuildingTargetID = ""` clears must continue. **Also clear `unit.Path`, `Moving`, `Gathering`, `Building`, etc., as it does today** — this is the single seam that guarantees "any new player command cancels the active attack order" requested in part 3 of the spec.

### Retarget cleanup — what gets cleared on a fresh attack order

When a player issues a new attack target while a unit is mid-engagement, `AttackWithUnits` calls `resetUnitMovementLocked` first ([state_combat.go:26](../../server/internal/game/state_combat.go#L26)). That already:
- Wipes `AttackTargetID`, `AttackBuildingTargetID`, `Attacking`, `Path`, `Moving`, `ManualMove`, `ManualAttackTarget` (in the new design: `Order.Type`).
- Does **not** clear `ThreatTable` — and that is correct. Threat is a longer-lived AI signal, decayed over time by `decayThreatLocked`. A retarget should not erase the AI's memory of who has been hitting it.
- Does **not** clear in-flight `Projectiles`. Also correct: shots already fired should land on their original target. `landProjectileLocked` ([projectile.go:107-120](../../server/internal/game/projectile.go#L107-L120)) handles attacker-died and target-died/invisible cases gracefully.
- Does **not** touch `CombatAnchorX/Y`. The new `AttackWithUnits` overwrites it ([state_combat.go:35-36](../../server/internal/game/state_combat.go#L35-L36)), which is right — the new target's position is the new anchor.

**No new cleanup is required for retarget.** The existing `resetUnitMovementLocked` is the single chokepoint, and adding `unit.Order = OrderState{Type: OrderIdle}` to it (followed by the caller setting the new order) covers all four behaviors requested in parts 1–3 of the spec.

### Failure modes

1. **Patrol target unreachable.** A unit on Patrol whose path to the next waypoint cannot be solved (`assignUnitPath` returns `Path = nil`). Behavior: stay on current order, retry pathing each tick (cheap because A* against a stable blocked set is sub-millisecond at our map sizes). Player-visible status should change from "Patrolling" to "Patrol Blocked" so the symptom is legible. Do **not** drop the order; pathing may succeed once buildings are destroyed or units move.
2. **Hold-position unit shoved out of position by separation.** `applyUnitSeparationLocked` ([state_movement.go:259-308](../../server/internal/game/state_movement.go#L259-L308)) can move units a few pixels per tick. A Hold unit may drift. Decide: tolerate up to N px of drift, then path back to `Order.HoldX/Y` once combat ends. **Recommended: ignore drift entirely for now.** It's pixel-scale and Hold's contract is "don't actively chase," not "remain on the exact pixel." Re-evaluate if QA reports visible drift after sustained combat.
3. **AttackTarget kited beyond profile leash, then target dies.** Existing `shouldDropCurrentTargetLocked` returns the target invalid (HP <= 0) and `clearCombatTargetLocked` runs. With the new design, demote `Order.Type` to `OrderIdle`. The unit then evaluates the world fresh and may engage a closer enemy. Acceptable.
4. **Player issues `Hold` to a unit currently mid-path on AttackMove.** `resetUnitMovementLocked` clears `Path` and `Moving`. New order set: `Order.Type = OrderHold`, `Order.HoldX/Y = unit.X, unit.Y`. Unit stops on a dime. This is the documented RTS behavior.
5. **Reconnect mid-order.** Server snapshot already includes `Status` text and `TargetX/Y` ([messages.go:299-301](../../server/pkg/protocol/messages.go#L299-L301)). For the client to render the right cursor/icon for a unit's standing order, **add `Order` (or just `OrderType` as a string) to `UnitSnapshot`**. The server is authoritative; the client just renders. On reconnect, the client gets a fresh snapshot with `Order.Type` and renders accordingly. No client-side reconciliation needed.

### Determinism / single-lock concerns

- All new order state (`OrderType`, `OrderState`) is unit-resident and mutated only from `*Locked` paths. No new wall-clock reads, no new map iteration that drives outcomes.
- Patrol waypoint flips are derived from a single `distance(unit, waypoint) < arrivalRadius` check evaluated in tick order. Deterministic.
- No new RNG calls are introduced. If "patrol jitter" or "hold rotation" is added later, allocate a named RNG stream (`rngOrders`) following the existing pattern ([state.go:265-289](../../server/internal/game/state.go#L265-L289)).
- Intent ordering: WS handlers serialize per-client by Go's WS reader. Two clients sending commands "simultaneously" have their handlers contend on `s.mu`; whichever wins acquires first. This is already the case for all existing commands and is acceptable for an authoritative server (no lockstep guarantees needed across clients).

## API / Network: enumerate new intents

We do **not** need to design wire format here, but the surface to add is:

- `set_stance_command` — payload: `unitIds: int[]`, `stance: "hold" | "idle"`. Idle is the "release Hold/Patrol back to default" command.
- `patrol_command` — payload: `unitIds: int[]`, `destination: Vec2`. Server uses each unit's current position as the second waypoint (most common UX). For two-click N-point patrol, queue it as a follow-up design.
- `attack_move_command` — already exists ([messages.go:199-203](../../server/pkg/protocol/messages.go#L199-L203)). Repurpose: now sets `Order.Type = OrderAttackMove` instead of clearing flags.
- `move_command` — already exists. Repurpose: sets `Order.Type = OrderMove`.
- `attack_command` — already exists. Repurpose: sets `Order.Type = OrderAttackTarget`.

A **force-move modifier** does not need a new wire message: `move_command` with the existing semantics IS the force-move, since today's `MoveUnits` sets `ManualMove = true` and the AI already skips evaluation for moving manual-move units. Just rename the `OrderType` constant from `OrderMove` to make this explicit.

If the player wants the inverted default (right-click = AttackMove, modifier = ForceMove), the wire change is one new bool on `move_command` (`forceMove?: bool`) and a branch in `MoveUnits` to pick `OrderType`. Defer until product picks the default.

## Implementation Handoff

### For `go-backend-engineer`

Files to touch:
- [state.go](../../server/internal/game/state.go): add `OrderType` enum + `OrderState` struct; replace `ManualMove` and `ManualAttackTarget` fields with `Order OrderState` on `Unit`. Update the field comments around [state.go:73-80](../../server/internal/game/state.go#L73-L80) to point at the new design.
- [state_movement.go](../../server/internal/game/state_movement.go):
  - In `MoveUnits` (single + multi paths), set `unit.Order = OrderState{Type: OrderMove, DestX: dest.X, DestY: dest.Y}` instead of `ManualMove = true`.
  - In `AttackMoveUnits`, set `OrderAttackMove` with `DestX/Y`.
  - In `resetUnitMovementLocked`, set `unit.Order = OrderState{Type: OrderIdle}`.
- [state_combat.go](../../server/internal/game/state_combat.go):
  - In `AttackWithUnits`, set `OrderAttackTarget` with `DestX/Y = target.X/Y` (these become the resume anchor / leash anchor).
  - Replace the `ManualAttackTarget = false` clear at [state_combat.go:102](../../server/internal/game/state_combat.go#L102) with the shared validity-helper call (Gap 1 from audit).
- [combat_ai.go](../../server/internal/game/combat_ai.go):
  - In `tickCombatAILocked`, replace the `ManualMove` gate ([combat_ai.go:129](../../server/internal/game/combat_ai.go#L129)) with `if unit.Order.Type == OrderMove && unit.AttackTargetID == 0 && unit.AttackBuildingTargetID == "" { continue }`.
  - In `evaluateCombatLocked`, replace `if unit.ManualAttackTarget && unit.AttackTargetID != 0` with `if unit.Order.Type == OrderAttackTarget && unit.AttackTargetID != 0`.
  - Add Gate A logic for `OrderHold` (suppress retreat).
  - In `applyCombatTargetLocked`, suppress approach pathing when `OrderHold`.
  - In `clearCombatTargetLocked`, demote `Order.Type` to `OrderIdle` only if it was `OrderAttackTarget`. Leave Patrol/AttackMove intact.
  - Add `resumeStandingOrderLocked(unit, blocked)`: re-issues path toward `Order.DestX/Y` for AttackMove/Patrol when no target. For Patrol, also handle waypoint flip when arrival radius reached.
  - Add new entry points: `SetUnitStance(playerID string, unitIDs []int, stance string)`, `PatrolUnits(playerID string, unitIDs []int, dest Vec2)`. Both follow the pattern of `MoveUnits`: lock, validate ownership, `resetUnitMovementLocked`, set `Order`, set `CombatAnchor`, kick off path if movement is needed.
- [combat_ai_scoring.go](../../server/internal/game/combat_ai_scoring.go): in `selectBestTargetLocked`, prepend the Hold filter that bounds detection to `unit.AttackRange`.
- [combat_ai_retreat.go](../../server/internal/game/combat_ai_retreat.go): in `shouldRetreatLocked`, return false early if `unit.Order.Type` is `OrderAttackTarget` or `OrderHold` or `OrderMove` (today the AttackTarget path returns early because of the [combat_ai.go:150](../../server/internal/game/combat_ai.go#L150) short-circuit; Hold/Move need explicit suppression).
- [state_combat.go](../../server/internal/game/state_combat.go): extract `combatTargetIsValidLocked(unit *Unit, target *Unit) bool` and use it from both [state_combat.go:99-104](../../server/internal/game/state_combat.go#L99-L104) and [combat_ai_scoring.go:11-13](../../server/internal/game/combat_ai_scoring.go#L11-L13) (closes audit Gap 1).
- [messages.go](../../server/pkg/protocol/messages.go): add `SetStanceCommandMessage`, `PatrolCommandMessage`. Add `Order string` (e.g. "idle" / "move" / "attack" / "attack_move" / "hold" / "patrol") to `UnitSnapshot` for client rendering.
- [handlers.go](../../server/internal/ws/handlers.go): add cases for `set_stance_command` and `patrol_command` mirroring the existing `attack_move_command` handler shape.
- `server/internal/game/state_spawn.go`: spawned units default to `Order = OrderState{Type: OrderIdle}`. Enemy units may also start in `OrderIdle` — their existing `assignEnemyObjectiveLocked` path ([combat_ai.go:174-177](../../server/internal/game/combat_ai.go#L174-L177), [combat_ai_retreat.go:123-140](../../server/internal/game/combat_ai_retreat.go#L123-L140)) is unchanged.

Tests to add:
- A unit with `OrderHold` does not move when an enemy walks just outside its `AttackRange`, and does fire when an enemy steps inside.
- A unit with `OrderPatrol` swaps waypoints on arrival.
- A unit with `OrderPatrol` engages an enemy that crosses its detection range, then resumes patrol movement after the enemy dies.
- `OrderAttackTarget` survives the leash bypass (existing test if any; otherwise add).
- Issuing `MoveUnits` to a unit currently on `OrderAttackTarget` clears the prior order and starts fresh; the prior `AttackTargetID` is zero.
- `OrderMove` (force-move) does not engage enemies even when one is well inside detection range.
- Retarget cleanup: A unit firing a projectile at target A, then issued `AttackWithUnits` against target B — the in-flight projectile still lands on A; `AttackTargetID == B`.
- Determinism: two seeded matches replaying the same intent stream produce identical unit states tick-by-tick.

Non-goals:
- Shift-queued orders.
- N-point patrols (>2 waypoints).
- Per-unit stance defaults (per-unit "is Aggressive / Defensive / Stand Ground" tri-state).
- Group formations on patrol/AMove (beyond what `buildFormationTargets` already does at order issuance).

### For `vue-frontend-engineer`

Files to touch:
- `client/src/game-portal/src/game/network/protocol.ts`: add `SetStanceCommandMessage` and `PatrolCommandMessage` type definitions. Extend `UnitSnapshot` with the new `order` field.
- `client/src/game-portal/src/game/network/NetworkClient.ts`: add `sendStanceCommand(unitIds, stance)` and `sendPatrolCommand(unitIds, x, y)`. Mirror the existing `sendAttackMoveCommand` shape.
- [GameState.ts](../../client/src/game-portal/src/game/core/GameState.ts):
  - Extend `UnitTargetingMode` type (currently `'move' | 'gather' | 'repair' | 'attack'` per [GameState.ts:247](../../client/src/game-portal/src/game/core/GameState.ts#L247)) to include `'patrol'`.
  - `Hold` does not need a targeting mode — it fires immediately when the user presses `H` (no second click).
- [InputManager.ts](../../client/src/game-portal/src/game/input/InputManager.ts):
  - In `getHotkeyAction`, add `h: 'hold'` (immediate) and `p: 'patrol'` (begins targeting mode).
  - Cursor for patrol mode: reuse the move cursor for now or add a dedicated SVG (don't bikeshed).
- `client/src/game-portal/src/game/core/GameClient.ts`:
  - In the targeting-mode dispatch around [GameClient.ts:289-301](../../client/src/game-portal/src/game/core/GameClient.ts#L289-L301), add a `'patrol'` branch that calls `sendPatrolCommand`.
  - Wire the `'hold'` selection action to `sendStanceCommand(unitIds, 'hold')` directly (no two-click flow).
- Selection action menus (whichever file builds the per-unit/per-group action lists — `getUnitActions` / `getGroupActions` referenced from [GameState.ts:1263](../../client/src/game-portal/src/game/core/GameState.ts#L1263)): add Hold and Patrol entries with the standard `id`, `hotkey`, `disabled` shape. They should appear for any unit with the `attack` capability (same gate as Attack action).
- HUD/tooltip: render the unit's current `order` field somewhere (tooltip, status bar). Optional: small icon overlay on the selected unit indicating Hold/Patrol/AMove.

Non-goals:
- Shift-queue UI.
- Patrol path preview (drawing the line between waypoints) — nice-to-have, separate ticket.
- Stance dropdown UI (Aggressive/Defensive/Stand Ground tri-state).

### For `qa-engineer`

Acceptance criteria — every one of these must hold:

1. **Sticky attack-target**: Player right-clicks an enemy at the edge of detection range. Selected unit chases the target across the map; the unit does NOT switch to a closer enemy that crosses its path; the unit does NOT retreat from melee threats while pursuing. The order ends only when the target dies, becomes invisible, or the player issues a new command.
2. **Retarget**: Player right-clicks enemy A; one tick later right-clicks enemy B. The unit's `AttackTargetID` becomes B. Any projectiles already in flight against A still land on A (or drop silently if A is gone). The unit's `ThreatTable` retains entries from A's prior interactions (verify in snapshot).
3. **Command supersedes attack**: Unit on `OrderAttackTarget` against B. Player issues a Move command. Unit's `Order.Type == OrderMove`, `AttackTargetID == 0`, `Path` populated toward the move destination. The unit walks past B without engaging.
4. **Hold**: Unit at position P with `OrderHold`. Enemy at distance `AttackRange + 5px`: unit does not move, does not fire. Enemy at distance `AttackRange - 5px`: unit fires. Enemy outside attack range walks past: unit does not chase. After the engaged enemy dies, unit's `Order.Type` is still `OrderHold` and the unit is still at (or near) P.
5. **Patrol**: Unit issued patrol from A to B. Unit walks A→B, on arrival walks B→A, repeats indefinitely. Enemy crosses unit's detection range during transit: unit engages, kills/loses target, then resumes movement toward whichever waypoint it was heading to before engagement. `Order.Type` remains `OrderPatrol` throughout.
6. **AttackMove**: Unit issued AMove to point D. Unit walks toward D; enemy enters detection range; unit breaks off and engages; after killing the target, unit resumes path to D. `Order.Type` remains `OrderAttackMove` throughout.
7. **Force-Move**: Unit issued Move (default right-click) to point D. Enemy steps directly in front of unit; unit walks past without engaging. (Verify by checking `Order.Type == OrderMove` and `AttackTargetID == 0` every tick during transit.)
8. **Determinism**: Run a 60-second scripted match (record intents); re-run with the same `matchSeed`; compare every tick's unit positions, HPs, and `Order` fields. Diff must be zero.
9. **Reconnect**: Disconnect a client mid-match while their units are on Patrol; reconnect within the 30s grace window. Snapshot received by the reconnected client shows `Order.Type == "patrol"` and units continue patrolling. No client-side state needed.
10. **Performance budget**: At 200 player units + 200 enemy units, all on AttackMove/Patrol, `tickCombatAILocked` + `tickUnitCombatLocked` together should complete in under 5 ms per tick (current budget; verify it has not regressed). The new `OrderType` switch is a single-byte read per unit per gate — overhead must be unmeasurable.
11. **Edge case: Hold under retreat conditions**: Unit on Hold attacked by a melee threat that would normally trigger retreat (`shouldRetreatLocked == true` but for the order). Unit must NOT retreat. Confirm by checking `Path` remains nil throughout.
12. **Edge case: Patrol with one waypoint at unreachable position**: Unit cannot path to B (blocked by buildings). Unit's `Status` reads "Patrol Blocked" (or equivalent), `Order.Type` remains `OrderPatrol`, no crash. When the obstacle is removed, unit resumes.
13. **Edge case: AttackTarget on a target that becomes invisible** (e.g. fog of war when implemented): `shouldDropCurrentTargetLocked` returns true, `clearCombatTargetLocked` fires, `Order.Type` demotes to `OrderIdle`. Unit does not stand frozen.
14. **Wire-format compatibility**: New `order` field on `UnitSnapshot` is `omitempty` so an old client receiving a snapshot from a new server still parses (graceful degradation). New clients sending `set_stance_command` to an old server receive an "unknown message type" error — acceptable, since this lands as a coordinated release.

### Cross-cutting

Both engineers must agree on:
- The `OrderType` string values that appear in `UnitSnapshot.order`. Recommend lowercase snake: `"idle"`, `"move"`, `"attack_move"`, `"attack_target"`, `"hold"`, `"patrol"`. Defined once in `protocol/messages.go` as untyped string constants for the wire and mirrored in TS.
- Wire message names: `set_stance_command`, `patrol_command`. Match the existing snake_case convention (`attack_move_command`).
- Stance values inside `set_stance_command`: `"hold"`, `"idle"`. Anything else → error.

## Open Questions

1. **Default right-click behavior**: today, default right-click on ground is force-move. StarCraft uses force-move as default; Warcraft 3 / AoE use attack-move as default. Pick one. The `InputManager.onRightClick` change is one line either way.
2. **Patrol UX**: one-click (current pos = second waypoint) or two-click (player picks both)? Recommend one-click for v1; two-click is a follow-up.
3. **Hold interaction with auto-acquired buildings**: should a Hold unit attack adjacent enemy buildings within its `AttackRange`? Recommend yes (consistent with attacking adjacent enemy units) but flag for product confirmation.
4. **Standing order persistence on rank-up**: when a unit ranks up mid-Patrol, does it retain the order? Recommend yes (rank-up doesn't touch `Order`). Confirm with product.
5. **Per-unit-type defaults**: are there units (workers, siege) where Hold or Patrol should be hidden from the action menu? Recommend hiding for non-`attack`-capable units (consistent with how `selectedUnitsCanAttack()` already gates the Attack action in [InputManager.ts:296,506](../../client/src/game-portal/src/game/input/InputManager.ts#L296)).
6. **Should `AttackMove` allow chasing past the leash anchor's `LeashDistance`?** Today AMove relies on the destination as the anchor and the standard leash. If players expect a-moved units to chase indefinitely (until the destination is reached), the leash check needs an order-aware override. Recommend leaving leash on for AMove (prevents pull-the-army-across-the-map kiting). Flag for confirmation.
7. **Shift-queue (deferred but worth flagging now)**: this design adds `OrderState` as a single struct. Queueing means extending to `[]OrderState`. Do that intentionally as a v2 — do not pre-bake the queue in v1, but leave a comment in the struct that says "queue-ready: this becomes []OrderState for shift-queue."
