# Enemy Objective Attack-Move & Unreachable-Target Retargeting — Design

**Date:** 2026-05-18
**Status:** Approved (design); pending implementation plan
**Scope:** Server only (`server/internal/game/`). No client/frontend changes.

## Problem

Routed enemy (wave/spawn) units currently model the player's townhall as a hard
**building attack target**. On spawn an enemy gets a plain move to the townhall
center ([state_waves.go:384-392](../../../server/internal/game/state_waves.go#L384-L392)),
but whenever it has no in-range target, `assignEnemyObjectiveLocked`
([combat_ai_retreat.go:357-407](../../../server/internal/game/combat_ai_retreat.go#L357-L407))
converts the townhall into an `AttackBuildingTargetID`, computes a precise
perimeter attack slot via `findBestBuildingAttackPositionLocked`, and A\*'s to
that slot.

When player units shuffle in front of the advancing enemy, that perimeter-slot
A\* fails and a **3-strike escalation** (40-tick cooldown → 120-tick cooldown →
"acquire nearest blocking hostile") fires, then the strike resets, the same
townhall is re-picked, and A\* re-runs one step further along the route. Each
cycle re-enters `selectBestTargetLocked` + `assignUnitPath`. The retarget
stagger / global search rate-limiter from the prior `unit-ai-pathing-overhaul`
exist specifically to spread out this churn — they suppress the symptom rather
than removing the cause. The root cause is that **the objective is modeled as a
hard building attack-target that demands a successful path to a precise
perimeter cell**, and that demand fails repeatedly as units move, producing the
visible stutter.

Separately, when an enemy holds an AI-acquired **unit** target that the player
pulls to an unreachable backline, the enemy enters **drift mode**
([`enterAttackDriftLocked`](../../../server/internal/game/combat_ai_retreat.go#L262-L274)):
it straight-lines toward the unreachable unit and silently halts at the
obstruction, not re-evaluating until the next AI tick — instead of switching to
a target it can actually fight. (Pure *kiting* — moving the unit far but still
reachable — is already correctly handled by the leash-drop in
`shouldDropCurrentTargetLocked`. The gap is specifically the *unreachable*
case where A\* fails.)

## Goals

1. Eliminate the advance-time re-acquisition churn (the stutter) at its source.
2. Make the townhall a persistent **move objective**, not a hard attack target.
3. While advancing, engage the best in-range hostile — **unit or building** —
   then resume advancing (standard Attack-Move semantics).
4. On objective loss, re-acquire the nearest player building (sticky; only on
   loss, never per tick).
5. When an AI-acquired unit target becomes unreachable, drop it and switch to a
   reachable available target rather than drifting uselessly.
6. Preserve all existing Guard / Hold / static-objective (`ObjectiveID`) /
   player-issued-attack (`OrderAttackTarget`) behavior. Preserve determinism
   under seed.

## Non-Goals

- No change to player unit AI, player-issued orders, or the client.
- No removal of the building-unreachable escalation for the *in-range* building
  commit path or for player-issued building attacks (still correct there).
- No active per-candidate A\* probing during retarget (explicitly rejected for
  cost — see Decision 3).

## Resolved Design Questions

1. **Buildings encountered en route:** standard Attack-Move. While advancing,
   the enemy engages the highest-scoring hostile in detection range — unit *or*
   building (tower, wall, barracks) — via the existing scoring path, then
   resumes toward the objective.
2. **Objective fallback when the assigned townhall is destroyed/absent:** the
   nearest attackable player building of any kind, honoring `TargetPlayerID`
   first (the player this enemy was spawned to attack), then nearest any
   player.
3. **Unreachable unit-target retarget cost:** drop + best in-range with ~1-tick
   latency. Mark the unreachable unit on a short cooldown so it is not
   re-picked, drop it, re-select the best *other* in-range target by normal
   score. No extra A\* per candidate. Rejected: active multi-A\* probing (re-
   introduces the A\* storms the pathing overhaul eliminated).

## Section 1 — Behavioral Model

A routed enemy is a **permanent Attack-Mover toward a sticky objective**:

- It always has a move destination = its objective building's position; it
  walks there.
- The existing combat-anchor slide
  ([combat_ai.go:154-169](../../../server/internal/game/combat_ai.go#L154-L169))
  keeps its leash centered on its *current* position while it has no attack
  target, so `selectBestTargetLocked` engages the best in-range hostile (unit
  or building) and, on kill, the enemy resumes walking. This is the same
  battle-tested path used by player `OrderAttackMove` / `OrderPatrol`; we feed
  it instead of `assignEnemyObjectiveLocked`.
- The townhall is destroyed through **normal in-range scoring**: on arrival the
  townhall is an in-range building, `scoreBuildingTargetLocked` picks it,
  `applyCombatTargetLocked` commits, and the building stickiness in
  [combat_ai_scoring.go:58-64](../../../server/internal/game/combat_ai_scoring.go#L58-L64)
  holds it until destroyed. No pre-pathed perimeter slot, no hard target while
  advancing.

**Stutter elimination:** while `unit.Moving` toward the objective, the enemy
recomputes nothing — no building attack-slot A\*, no 3-strike escalation.
Player units shuffling in front become ordinary in-range targets engaged via
the normal scoring path; they no longer trigger objective re-acquisition.

## Section 2 — State Model (ID-not-pointer compliant)

New `Unit` fields:

- `ObjectiveBuildingID string` — the building this enemy is advancing on.
  Distinct from `ObjectiveID` (static victory-point lock — untouched) and
  `TargetPlayerID` (player-routing preference — reused). Resolved to a position
  **at point-of-use** every time a repath is needed, via
  `getBuildingByIDLocked`, with the canonical validity guard (`nil` /
  `hp <= 0` / not hostile). Never cached as a `*BuildingTile`.
- `UnreachableUnitTargetID int` + `UnreachableUnitUntilTick int` — single-slot
  memo for the last AI-acquired unit target that failed A\*, with an expiry
  tick. Mirrors the existing `UnreachableBuildingTargetID` /
  `UnreachableUntilTick` fields. Single slot is sufficient: with ≥2
  simultaneously unreachable units the system self-corrects within a couple of
  staggered re-eval ticks.

**Sticky re-acquisition rule (anti-churn core):** `ObjectiveBuildingID` is set
once at spawn and only re-resolved when the current objective building is
destroyed / missing / no longer hostile. Re-acquisition picks the nearest
attackable player building (honoring `TargetPlayerID` first, else nearest any
player) with a **building-ID tiebreak for determinism**. This O(buildings) scan
runs only on objective loss, never per tick.

## Section 3 — Code Changes

1. **New `enemyAdvanceToObjectiveLocked(unit, blocked)`** — modeled on
   `resumeStandingOrderLocked`'s `OrderAttackMove` case
   ([combat_ai.go:456-470](../../../server/internal/game/combat_ai.go#L456-L470)):
   resolve & validate the sticky objective building; if dead/missing,
   re-acquire the nearest player building (per Decision 2) and store its ID; if
   `unit.Moving` already heading there, return; else `assignUnitPath` to the
   building's position. **Never sets `AttackBuildingTargetID`, never computes a
   perimeter slot, never escalates.** If no live player building exists, fall
   back to `getNearestPlayerTownhallCenterLocked`; if that is also nil, the
   enemy idles in place (cooldown-guarded) until a target reappears.

2. **`evaluateCombatLocked` enemy no-target branch**
   ([combat_ai.go:296-330](../../../server/internal/game/combat_ai.go#L296-L330)):
   replace the `assignEnemyObjectiveLocked` call with
   `enemyAdvanceToObjectiveLocked`. Keep the Guard / Hold / `ObjectiveID`
   early-returns and the existing cheap cooldown guards (`unit.Moving`,
   `NextObjectiveSearchTick`, `nextGlobalObjectiveSearchTick`) — they become
   near-inert because the unit is normally `Moving`, but remain as cheap
   safety guards.

3. **Spawn** ([state_waves.go:367-393](../../../server/internal/game/state_waves.go#L367-L393)):
   set `unit.ObjectiveBuildingID` to the resolved townhall **building**. Add a
   small `*protocol.BuildingTile`-returning townhall helper alongside the
   existing center helpers (`getPlayerTownhallCenterLocked` /
   `getNearestPlayerTownhallCenterLocked`). The `__none__` (stay-at-spawn),
   `objectiveId` (static objective), and Guard/Hold paths do **not** set
   `ObjectiveBuildingID` — behavior unchanged.

4. **Retire `assignEnemyObjectiveLocked`**: its two callers — the
   `evaluateCombatLocked` no-target branch and the
   `applyBuildingUnreachableEscalationLocked` strike-3 fallback
   ([combat_ai.go:438](../../../server/internal/game/combat_ai.go#L438)) —
   both repoint to `enemyAdvanceToObjectiveLocked`. Delete the now-dead
   per-objective perimeter-slot + escalation block inside the old function.

5. **Unreachable AI-acquired unit target** — in
   [`assignAttackApproachPathLocked`](../../../server/internal/game/combat_ai_retreat.go#L75-L139),
   branch on target provenance:
   - `unit.Order.Type == OrderAttackTarget` (player-issued): **keep drift**
     mode. "The player explicitly chose this fight" is an existing deliberate
     invariant ([combat_ai_scoring.go:15-20](../../../server/internal/game/combat_ai_scoring.go#L15-L20)).
   - AI-acquired (all other cases): record the unit in
     `UnreachableUnitTargetID` / `UnreachableUnitUntilTick`,
     `clearCombatTargetLocked`, and let the next evaluation re-select. No drift.

6. **`selectBestTargetLocked` unit-candidate loop**
   ([combat_ai_scoring.go:87-101](../../../server/internal/game/combat_ai_scoring.go#L87-L101)):
   skip any candidate where
   `candidate.ID == unit.UnreachableUnitTargetID && s.Tick <
   unit.UnreachableUnitUntilTick` — exactly the pattern used for the building
   skip at [state_waves.go:415-417](../../../server/internal/game/state_waves.go#L415-L417).
   When the best remaining in-range target is also unreachable the same cheap
   loop repeats next eval, bounded by the existing `NextCombatEvalTick`
   retarget stagger. When no reachable in-range target exists, the no-target
   branch falls through to `enemyAdvanceToObjectiveLocked` — the enemy resumes
   marching toward the townhall, which flanks around the obstruction and opens
   new engagements.

**Unreachable-unit cooldown duration** is a tuning constant with two stated
constraints: (a) long enough to outlast the retarget-stagger window so the bad
unit is not instantly re-picked; (b) short enough that a unit brought back into
reach re-aggros within ~1–2 s. The exact tick value is chosen during
implementation/QA. Per project convention, tests assert the *invariant*, never
a pinned tick number.

## Retained Unchanged

- `applyBuildingUnreachableEscalationLocked` + `UnreachableBuilding*` fields:
  still used by the in-range building commit path and player-issued building
  attacks.
- `acquireNearestBlockingHostileLocked`: rare strike-3 safety net for the
  in-range building case; kept as-is.
- Retarget stagger / global objective-search rate-limiter: cheap, still guard
  wave-clear retarget bursts.
- All Guard / Hold / `ObjectiveID` static-objective behavior.
- All client/frontend code.
- Drift mode for player-issued (`OrderAttackTarget`) unreachable targets.

## Determinism

- Objective re-acquisition: nearest-building scan must use a building-ID
  tiebreak so equal-distance ties resolve deterministically (add to the
  nearest-building helper if absent).
- Unreachable skip: pure ID + tick comparison — deterministic.
- No new wall-clock, unseeded RNG, or map-iteration-order-driven outcomes.

## Edge Cases

- **Townhall destroyed mid-advance:** next not-`Moving` eval finds
  `ObjectiveBuildingID` invalid → re-acquire nearest player building (sticky to
  the new one).
- **Player has no buildings yet / fully wiped:** fall back to
  `getNearestPlayerTownhallCenterLocked`; if nil, idle in place (cooldown
  prevents per-tick churn) until a target appears.
- **Wall sealing the path:** the wall is an in-range building → scored and
  attacked via the normal path (Decision 1). Enemies do not get permanently
  stuck.
- **Player kites a reachable unit far away:** unchanged — handled by the
  existing leash-drop, not by the new unreachable rule.
- **Two simultaneously unreachable units:** single-slot memo self-corrects
  within a couple of staggered re-eval ticks.

## Testing Intent (server-only)

- Enemy advances to the townhall as a plain move (no `AttackBuildingTargetID`
  set while advancing).
- Enemy engages an in-range player unit en route, then resumes advancing after
  the kill.
- Enemy attacks and destroys the townhall on arrival via normal scoring.
- Townhall destroyed mid-advance → enemy re-acquires the nearest player
  building.
- Shifting wall of player units in front of an advancing enemy → bounded
  `PathDiagnostics.RepathCount` (no per-tick repath churn). Exact bound is the
  QA engineer's call; assert it as an invariant, not a pinned number.
- Player pulls the targeted unit behind impassable terrain → enemy drops it and
  engages a reachable unit instead; with no reachable unit → enemy resumes
  advancing to the townhall.
- Kited-but-reachable unit → still behaves via leash-drop (regression guard).
- Player-issued attack (`OrderAttackTarget`) on an unreachable unit → still
  drifts (scope guard).
- Update/replace `enemy_blocked_objective_test.go`; reconcile expectations with
  the archived `unit-ai-pathing-overhaul` specs (drift-mode decision is
  superseded for AI-acquired unit targets).
