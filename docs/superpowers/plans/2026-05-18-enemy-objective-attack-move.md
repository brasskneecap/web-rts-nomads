# Enemy Objective Attack-Move & Unreachable-Target Retargeting — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make routed enemies attack-move toward a sticky townhall objective (engaging in-range units/buildings via normal scoring) instead of hard-targeting the townhall, and drop+switch off unreachable AI-acquired unit targets instead of drifting.

**Architecture:** The townhall stops being a pre-pathed building attack-target. A new `enemyAdvanceToObjectiveLocked` (modeled on `resumeStandingOrderLocked`'s `OrderAttackMove` case) replaces `assignEnemyObjectiveLocked`: resolve a sticky `ObjectiveBuildingID`, plain-move toward it, return early while already `Moving` (the anti-churn guard), and fall back to `acquireNearestBlockingHostileLocked` only when the objective is fully walled off. Unreachable AI-acquired unit targets are memoed (`UnreachableUnitTargetID`/`UnreachableUnitUntilTick`), cleared, and skipped by `selectBestTargetLocked` for a cooldown; player-issued (`OrderAttackTarget`) targets keep drift mode.

**Tech Stack:** Go (module `webrts/server`, rooted at `server/`). Tests in package `game` under `server/internal/game/`. Spec: [docs/superpowers/specs/2026-05-18-enemy-objective-attack-move-design.md](../specs/2026-05-18-enemy-objective-attack-move-design.md).

**Conventions:**
- All Go commands run from `c:/Personal Dev/webrts/server`.
- `*Locked` functions assume `s.mu` is held. Targets stored by ID, resolved + validated at point-of-use ([.claude/rules/AI_RULES.md](../../../.claude/rules/AI_RULES.md)).
- Per project rule: tests assert invariants / derive expected values; never pin balance or tick-tuning numbers.

---

## File Structure

| File | Responsibility | Change |
|---|---|---|
| `server/internal/game/state.go` | `Unit` struct | Add `ObjectiveBuildingID`, `UnreachableUnitTargetID`, `UnreachableUnitUntilTick` fields |
| `server/internal/game/state_waves.go` | spawn + building/townhall lookups | Add `getNearestPlayerTownhallBuildingLocked`; building-ID determinism tiebreaks; set `ObjectiveBuildingID` at spawn |
| `server/internal/game/combat_ai_retreat.go` | enemy objective + approach pathing | Replace `assignEnemyObjectiveLocked` with `enemyAdvanceToObjectiveLocked`; provenance branch in `assignAttackApproachPathLockedWithSubBlocked` |
| `server/internal/game/combat_ai.go` | combat eval + escalation | Repoint the two `assignEnemyObjectiveLocked` callers |
| `server/internal/game/combat_ai_scoring.go` | target selection | Skip unreachable-unit candidates in the primary unit loop |
| `server/internal/game/enemy_blocked_objective_test.go` | anti-spawn-freeze regression | Update doc comment for the new mechanism; assertion unchanged |
| `server/internal/game/enemy_objective_attack_move_test.go` | new behavior tests | Create |
| `server/internal/game/enemy_unreachable_unit_retarget_test.go` | new unreachable-unit tests | Create |

---

## Task 1: Add Unit state fields

**Files:**
- Modify: `server/internal/game/state.go` (near the existing `UnreachableBuilding*` fields, ~lines 280-287)

- [ ] **Step 1: Add the three fields**

In `server/internal/game/state.go`, find this block (around line 281-287):

```go
	// UnreachableBuildingTargetID / UnreachableUntilTick memo the last building
	// that failed A* so the scoring loop can skip it for the cooldown window,
	// forcing selection of a different building instead of hammering pathfinding
	// every tick against an inaccessible one. Unit-target unreachability uses
	// drift-mode (AttackDrifting) instead — no memo needed.
	UnreachableBuildingTargetID string
	UnreachableUntilTick        int
```

Replace it with (updates the stale "no memo needed" comment and adds the new fields):

```go
	// UnreachableBuildingTargetID / UnreachableUntilTick memo the last building
	// that failed A* so the scoring loop can skip it for the cooldown window,
	// forcing selection of a different building instead of hammering pathfinding
	// every tick against an inaccessible one.
	UnreachableBuildingTargetID string
	UnreachableUntilTick        int
	// ObjectiveBuildingID is the building this routed enemy is attack-moving
	// toward (its sticky objective — typically the assigned/nearest townhall).
	// Distinct from ObjectiveID (static victory-point lock) and TargetPlayerID
	// (player-routing preference). Resolved + validated at point-of-use every
	// time a repath is needed via getBuildingByIDLocked; never a cached pointer.
	// Re-acquired only when the current objective building dies/disappears, so
	// advancing enemies do not re-pick an objective every tick (anti-churn).
	ObjectiveBuildingID string
	// UnreachableUnitTargetID / UnreachableUnitUntilTick memo the last
	// AI-acquired unit target that failed A*, so selectBestTargetLocked skips it
	// for the cooldown window and the enemy switches to a reachable target
	// instead of drifting. Single-slot (last unreachable unit); with >=2
	// simultaneously unreachable units the staggered re-eval self-corrects.
	// Player-issued (OrderAttackTarget) targets are NOT memoed — they keep
	// drift mode (see assignAttackApproachPathLockedWithSubBlocked).
	UnreachableUnitTargetID int
	UnreachableUnitUntilTick int
```

- [ ] **Step 2: Verify the package still builds**

Run: `cd "c:/Personal Dev/webrts/server" && go build ./internal/game/`
Expected: no output, exit 0 (new fields default-zero, no behavior change yet).

- [ ] **Step 3: Commit**

```bash
cd "c:/Personal Dev/webrts" && git add server/internal/game/state.go && git commit -m "feat: add ObjectiveBuildingID + UnreachableUnit memo fields to Unit

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Townhall building lookup + determinism tiebreaks

A sticky objective needs the townhall **building** (to store its ID), not just a position. The nearest-building helpers also need a deterministic tiebreak (AI_RULES: deterministic under seed).

**Files:**
- Modify: `server/internal/game/state_waves.go` — `findNearestAttackablePlayerBuildingLocked` (~402-426), `findNearestAttackableBuildingForPlayerLocked` (~433-458); add `getNearestPlayerTownhallBuildingLocked`
- Test: `server/internal/game/enemy_objective_attack_move_test.go` (create)

- [ ] **Step 1: Write the failing determinism + helper test**

Create `server/internal/game/enemy_objective_attack_move_test.go`:

```go
package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// newObjectiveTestState builds an obstacle-free map with one player townhall
// plus any `extra` buildings, all assembled into the MapConfig BEFORE
// NewGameStateWithSeed so the building index is constructed once and correctly
// (no post-construction slice append, which would reallocate the backing array
// and invalidate the index's element pointers). Returns the locked GameState.
func newObjectiveTestState(t *testing.T, extra ...protocol.BuildingTile) *GameState {
	t.Helper()
	const cell = 64.0
	cols, rows := 40, 24
	townhall := protocol.BuildingTile{
		GridCoord: protocol.GridCoord{X: 2, Y: 10}, ID: "townhall-1",
		BuildingType: "townhall", Width: 2, Height: 2,
		Occupied: true, Visible: true, OwnerID: &townhallOwnerID,
		Metadata: map[string]interface{}{"hp": 5000.0, "maxHp": 5000.0},
	}
	buildings := append([]protocol.BuildingTile{townhall}, extra...)
	cfg := protocol.MapConfig{
		ID: "obj-test", Name: "obj-test",
		Width: float64(cols) * cell, Height: float64(rows) * cell,
		GridCols: cols, GridRows: rows, CellSize: cell,
		Obstacles: []protocol.ObstacleTile{},
		Buildings: buildings,
	}
	s := NewGameStateWithSeed(cfg, 42)
	s.mu.Lock()
	return s
}

// townhallOwnerID is a package-level addressable "p1" so BuildingTile.OwnerID
// (a *string) can point at a stable address shared across the test fixtures.
var townhallOwnerID = "p1"

// objBuilding is a fixture constructor for an extra attackable player building.
func objBuilding(id, btype string, gx, gy, w, h int, hp float64) protocol.BuildingTile {
	return protocol.BuildingTile{
		GridCoord: protocol.GridCoord{X: gx, Y: gy}, ID: id,
		BuildingType: btype, Width: w, Height: h,
		Occupied: true, Visible: true, OwnerID: &townhallOwnerID,
		Metadata: map[string]interface{}{"hp": hp, "maxHp": hp},
	}
}

func TestGetNearestPlayerTownhallBuilding_ReturnsTownhall(t *testing.T) {
	s := newObjectiveTestState(t)
	defer s.mu.Unlock()

	b := s.getNearestPlayerTownhallBuildingLocked(2000, 768)
	if b == nil || b.ID != "townhall-1" {
		t.Fatalf("want townhall-1, got %v", b)
	}
}

// Determinism: two attackable buildings the same distance from the enemy must
// resolve to the same one every call (lower ID wins), so seeded replays agree.
// a-tower (gx=11) right edge = 12*64 = 768; z-tower (gx=20) left edge =
// 20*64 = 1280; enemy at x=1024 (= (768+1280)/2), y=736 inside both towers'
// y-span [704,768] -> distanceToBuilding == 256 for BOTH (genuinely tied), so
// only the ID tiebreak decides, and it must pick "a-tower" every call.
func TestFindNearestAttackablePlayerBuilding_DeterministicTiebreak(t *testing.T) {
	s := newObjectiveTestState(t,
		objBuilding("a-tower", "tower", 11, 11, 1, 1, 100),
		objBuilding("z-tower", "tower", 20, 11, 1, 1, 100),
	)
	defer s.mu.Unlock()

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c",
		protocol.Vec2{X: 1024, Y: 736})
	s.initializeCombatUnitLocked(enemy)

	for i := 0; i < 20; i++ {
		got := s.findNearestAttackablePlayerBuildingLocked(enemy)
		if got == nil || got.ID != "a-tower" {
			t.Fatalf("nondeterministic/incorrect tiebreak: call %d got %v want a-tower", i, got)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd "c:/Personal Dev/webrts/server" && go test ./internal/game/ -run 'TestGetNearestPlayerTownhallBuilding_ReturnsTownhall|TestFindNearestAttackablePlayerBuilding_DeterministicTiebreak' -count=1 -v`
Expected: build failure — `s.getNearestPlayerTownhallBuildingLocked` undefined.

- [ ] **Step 3: Add the building helper and tiebreaks**

In `server/internal/game/state_waves.go`, add after `getNearestPlayerTownhallCenterLocked` (ends ~line 379):

```go
// getNearestPlayerTownhallBuildingLocked returns the live, occupied,
// non-enemy townhall building geographically nearest to (x,y), or nil. The
// building-returning companion to getNearestPlayerTownhallCenterLocked, used
// to seed an enemy's sticky ObjectiveBuildingID at spawn. Deterministic: ties
// resolve to the lower building ID.
func (s *GameState) getNearestPlayerTownhallBuildingLocked(x, y float64) *protocol.BuildingTile {
	var best *protocol.BuildingTile
	bestDistSq := math.MaxFloat64
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.BuildingType != "townhall" || !b.Occupied || !b.Visible {
			continue
		}
		if b.OwnerID == nil || *b.OwnerID == enemyPlayerID {
			continue
		}
		hp, _, ok := getBuildingHP(b)
		if !ok || hp <= 0 {
			continue
		}
		cx := (float64(b.X) + float64(b.Width)/2) * s.MapConfig.CellSize
		cy := (float64(b.Y) + float64(b.Height)/2) * s.MapConfig.CellSize
		distSq := distanceSquared(x, y, cx, cy)
		if distSq < bestDistSq || (distSq == bestDistSq && (best == nil || b.ID < best.ID)) {
			bestDistSq = distSq
			best = b
		}
	}
	return best
}
```

In the same file, in `findNearestAttackablePlayerBuildingLocked`, change:

```go
		dist := s.distanceToBuilding(enemy.X, enemy.Y, b)
		if dist < bestDistSq {
			bestDistSq = dist
			best = b
		}
```

to:

```go
		dist := s.distanceToBuilding(enemy.X, enemy.Y, b)
		if dist < bestDistSq || (dist == bestDistSq && (best == nil || b.ID < best.ID)) {
			bestDistSq = dist
			best = b
		}
```

In `findNearestAttackableBuildingForPlayerLocked`, apply the identical change to its `dist < bestDistSq` block.

> Correctness note: the test fixtures build all buildings into `MapConfig.Buildings` **before** `NewGameStateWithSeed` (via the `newObjectiveTestState(t, extra...)` helper), so the building index is constructed once by the constructor and no post-construction slice append can reallocate the backing array and invalidate index pointers. No manual reindex helper is needed or called.

- [ ] **Step 4: Run to verify it passes**

Run: `cd "c:/Personal Dev/webrts/server" && go test ./internal/game/ -run 'TestGetNearestPlayerTownhallBuilding_ReturnsTownhall|TestFindNearestAttackablePlayerBuilding_DeterministicTiebreak' -count=1 -v`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
cd "c:/Personal Dev/webrts" && git add server/internal/game/state_waves.go server/internal/game/enemy_objective_attack_move_test.go && git commit -m "feat: townhall building lookup + deterministic nearest-building tiebreak

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Add `enemyAdvanceToObjectiveLocked`

Add the new function alongside the old `assignEnemyObjectiveLocked` (callers repointed in Task 4, so the build stays green here).

**Files:**
- Modify: `server/internal/game/combat_ai_retreat.go` (add function after `assignEnemyObjectiveLocked`, ~line 407)
- Test: `server/internal/game/enemy_objective_attack_move_test.go`

- [ ] **Step 1: Write the failing behavior tests**

Append to `server/internal/game/enemy_objective_attack_move_test.go`:

```go
// An enemy with no objective yet self-acquires the townhall and plain-moves
// toward it WITHOUT setting an attack-building target or strike escalation.
func TestEnemyAdvanceToObjective_PlainMoveNoHardTarget(t *testing.T) {
	s := newObjectiveTestState(t)
	defer s.mu.Unlock()

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c",
		protocol.Vec2{X: 2200, Y: 768})
	enemy.Visible = true
	enemy.MoveSpeed = 150
	s.initializeCombatUnitLocked(enemy)

	blocked := s.getBlockedCellsLocked()
	s.enemyAdvanceToObjectiveLocked(enemy, blocked)

	if enemy.ObjectiveBuildingID != "townhall-1" {
		t.Fatalf("want sticky ObjectiveBuildingID=townhall-1, got %q", enemy.ObjectiveBuildingID)
	}
	if enemy.AttackBuildingTargetID != "" {
		t.Fatalf("must NOT hard-target the building while advancing; got %q", enemy.AttackBuildingTargetID)
	}
	if enemy.UnreachableBuildingStrikeCount != 0 {
		t.Fatalf("must NOT run strike escalation; got %d", enemy.UnreachableBuildingStrikeCount)
	}
	if !enemy.Moving {
		t.Fatal("enemy should be moving toward the townhall")
	}
}

// While already advancing, the function must early-return without recomputing
// a path — the per-tick anti-churn guard.
func TestEnemyAdvanceToObjective_NoRepathWhileMoving(t *testing.T) {
	s := newObjectiveTestState(t)
	defer s.mu.Unlock()

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c",
		protocol.Vec2{X: 2200, Y: 768})
	enemy.Visible = true
	enemy.MoveSpeed = 150
	s.initializeCombatUnitLocked(enemy)
	blocked := s.getBlockedCellsLocked()

	s.enemyAdvanceToObjectiveLocked(enemy, blocked)
	pathBefore := append([]protocol.Vec2(nil), enemy.Path...)
	tx, ty := enemy.TargetX, enemy.TargetY

	s.enemyAdvanceToObjectiveLocked(enemy, blocked) // second call, still Moving

	if enemy.TargetX != tx || enemy.TargetY != ty || len(enemy.Path) != len(pathBefore) {
		t.Fatal("advancing enemy must not recompute its path while Moving")
	}
}

// Objective destroyed mid-advance -> re-acquire the nearest player building.
func TestEnemyAdvanceToObjective_ReacquireOnObjectiveLoss(t *testing.T) {
	s := newObjectiveTestState(t, objBuilding("tower-1", "tower", 30, 11, 1, 1, 100))
	defer s.mu.Unlock()

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c",
		protocol.Vec2{X: 2200, Y: 768})
	enemy.Visible = true
	enemy.MoveSpeed = 150
	enemy.ObjectiveBuildingID = "townhall-1"
	s.initializeCombatUnitLocked(enemy)
	blocked := s.getBlockedCellsLocked()

	// Destroy the townhall.
	s.MapConfig.Buildings[0].Metadata["hp"] = 0.0
	enemy.Moving = false
	s.enemyAdvanceToObjectiveLocked(enemy, blocked)

	if enemy.ObjectiveBuildingID != "tower-1" {
		t.Fatalf("want re-acquired ObjectiveBuildingID=tower-1, got %q", enemy.ObjectiveBuildingID)
	}
}

// Objective exists but is fully walled off -> fall back to engaging the
// nearest hostile (anti-spawn-freeze), still without hard-targeting.
func TestEnemyAdvanceToObjective_PartitionFallsBackToBlocker(t *testing.T) {
	s := newObjectiveTestState(t)
	defer s.mu.Unlock()
	ownerID := "p1"

	// Full vertical unit-wall partition at x=1200 (same construction as the
	// walled-off regression test).
	for y := 10.0; y <= s.MapHeight-10.0; y += 20.0 {
		w := s.spawnPlayerUnitLocked("soldier", ownerID, "#3498db", protocol.Vec2{X: 1200, Y: y})
		w.Visible = true
		w.MaxHP, w.HP = 1000, 1000
		w.MoveSpeed = 0
		w.Damage = 0
		w.Capabilities = nil
		s.initializeCombatUnitLocked(w)
	}
	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c",
		protocol.Vec2{X: 1800, Y: 768})
	enemy.Visible = true
	enemy.MoveSpeed = 150
	enemy.ObjectiveBuildingID = "townhall-1"
	s.initializeCombatUnitLocked(enemy)
	blocked := s.getBlockedCellsLocked()

	enemy.Moving = false
	s.enemyAdvanceToObjectiveLocked(enemy, blocked)

	if !enemy.Moving {
		t.Fatal("walled-off enemy must move toward a blocking hostile, not freeze")
	}
	if enemy.AttackBuildingTargetID != "" || enemy.UnreachableBuildingStrikeCount != 0 {
		t.Fatalf("fallback must not hard-target/escalate; bld=%q strikes=%d",
			enemy.AttackBuildingTargetID, enemy.UnreachableBuildingStrikeCount)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd "c:/Personal Dev/webrts/server" && go test ./internal/game/ -run 'TestEnemyAdvanceToObjective_' -count=1 -v`
Expected: build failure — `s.enemyAdvanceToObjectiveLocked` undefined.

- [ ] **Step 3: Implement `enemyAdvanceToObjectiveLocked`**

In `server/internal/game/combat_ai_retreat.go`, immediately after the closing brace of `assignEnemyObjectiveLocked` (~line 407), add:

```go
// enemyAdvanceToObjectiveLocked is the routed-enemy analog of
// resumeStandingOrderLocked's OrderAttackMove case: the enemy plain-moves
// toward a sticky objective building and lets normal in-range scoring engage
// whatever it meets (unit or building) on the way. It NEVER sets
// AttackBuildingTargetID, computes a perimeter slot, or runs strike escalation
// — the townhall is destroyed via ordinary scoreBuildingTargetLocked once it
// comes into detection range and commits like any other building.
//
// Fallback chain (see docs/superpowers/specs/2026-05-18-enemy-objective-
// attack-move-design.md):
//  1. Resolve & validate the sticky objective building; re-acquire the nearest
//     player building (honoring TargetPlayerID) when it died/disappeared. With
//     no building at all, fall back to a townhall-center position; with none,
//     idle (the evaluateCombatLocked cooldown guard prevents per-tick retry).
//  2. If already Moving toward it, return — the per-tick anti-churn guard that
//     removes the old re-acquisition stutter.
//  3. assignUnitPath to the objective position (a plain move).
//  4. If that path is impossible (objective fully walled off), engage the
//     nearest hostile anywhere so killing through reopens the route; the
//     drop-on-death -> re-advance flow then resumes.
func (s *GameState) enemyAdvanceToObjectiveLocked(unit *Unit, blocked map[gridPoint]bool) {
	// (1) Resolve / re-acquire the sticky objective building. isValidHostile-
	// BuildingTarget handles a nil building and validates visible/hostile/hp>0.
	building := s.getBuildingByIDLocked(unit.ObjectiveBuildingID)
	if !s.isValidHostileBuildingTarget(unit, building) {
		building = nil
		unit.ObjectiveBuildingID = ""
		if unit.TargetPlayerID != "" {
			building = s.findNearestAttackableBuildingForPlayerLocked(unit, unit.TargetPlayerID)
		}
		if building == nil {
			building = s.findNearestAttackablePlayerBuildingLocked(unit)
		}
		if building != nil {
			unit.ObjectiveBuildingID = building.ID
		}
	}

	var objectivePos protocol.Vec2
	if building != nil {
		objectivePos = s.buildingCenterLocked(building)
	} else if thc := s.getNearestPlayerTownhallCenterLocked(unit.X, unit.Y); thc != nil {
		objectivePos = *thc
	} else {
		// Nothing to advance on. Idle in place; evaluateCombatLocked's
		// NextObjectiveSearchTick / global cooldown stops this re-running
		// every tick.
		return
	}

	// (2) Already advancing — do not recompute. This is the guard that
	// removes the per-tick re-acquisition stutter.
	if unit.Moving {
		return
	}

	// (3) Plain move toward the objective. No attack target, no escalation.
	unit.AttackTargetID = 0
	unit.AttackBuildingTargetID = ""
	unit.Attacking = false
	unit.Status = "Advancing"
	s.assignUnitPath(unit, objectivePos, blocked, nil)
	if unit.Moving {
		return
	}

	// (4) Objective exists but is completely partitioned off by a wall of
	// units/terrain. Push the enemy at the nearest hostile anywhere; normal
	// in-range scoring engages it as the enemy closes, and killing through
	// reopens the route. (acquireNearestBlockingHostileLocked already issues a
	// movement path and ignores the DetectionRange cap.)
	s.acquireNearestBlockingHostileLocked(unit, blocked)
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd "c:/Personal Dev/webrts/server" && go test ./internal/game/ -run 'TestEnemyAdvanceToObjective_' -count=1 -v`
Expected: PASS (all four `TestEnemyAdvanceToObjective_*`).

- [ ] **Step 5: Commit**

```bash
cd "c:/Personal Dev/webrts" && git add server/internal/game/combat_ai_retreat.go server/internal/game/enemy_objective_attack_move_test.go && git commit -m "feat: add enemyAdvanceToObjectiveLocked (sticky attack-move objective)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Repoint callers, retire `assignEnemyObjectiveLocked`

**Files:**
- Modify: `server/internal/game/combat_ai.go` — `evaluateCombatLocked` (~line 323) and `applyBuildingUnreachableEscalationLocked` (~line 437-439)
- Modify: `server/internal/game/combat_ai_retreat.go` — delete `assignEnemyObjectiveLocked` (~lines 357-407)
- Test: `server/internal/game/enemy_objective_attack_move_test.go`

- [ ] **Step 1: Write the failing integration test**

Append to `server/internal/game/enemy_objective_attack_move_test.go`:

```go
// End-to-end through the real sim: a routed enemy with a clear lane reaches
// and destroys the townhall via normal scoring (no hard-target while
// advancing), and engages an in-range player unit on the way then resumes.
func TestEnemy_AttackMovesToTownhall_EngagesEnRoute(t *testing.T) {
	s := newObjectiveTestState(t)
	ownerID := "p1"

	// One defender between the enemy and the townhall.
	def := s.spawnPlayerUnitLocked("soldier", ownerID, "#3498db",
		protocol.Vec2{X: 900, Y: 768})
	def.Visible = true
	def.MaxHP, def.HP = 60, 60
	def.MoveSpeed = 0
	s.initializeCombatUnitLocked(def)
	defID := def.ID

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c",
		protocol.Vec2{X: 2200, Y: 768})
	enemy.Visible = true
	enemy.MaxHP, enemy.HP = 2000, 2000
	enemy.Damage = 25
	enemy.MoveSpeed = 180
	s.initializeCombatUnitLocked(enemy)
	enemyID := enemy.ID
	s.mu.Unlock()

	tickN(s, 600)

	s.mu.RLock()
	defer s.mu.RUnlock()
	e := s.unitsByID[enemyID]
	if e == nil {
		t.Fatal("enemy disappeared")
	}
	if d := s.unitsByID[defID]; d != nil && d.HP > 0 {
		t.Fatalf("enemy should have engaged & killed the en-route defender (hp=%.0f)", d.HP)
	}
	hp, _, _ := getBuildingHP(&s.MapConfig.Buildings[0])
	if hp >= 5000.0 {
		t.Fatalf("enemy should have reached and damaged the townhall; hp still %.0f", hp)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd "c:/Personal Dev/webrts/server" && go test ./internal/game/ -run TestEnemy_AttackMovesToTownhall_EngagesEnRoute -count=1 -v`
Expected: FAIL — with the old `assignEnemyObjectiveLocked` still wired, the enemy hard-targets/perimeter-paths; the test asserting the new attack-move behavior fails or times out. (If it happens to pass incidentally, Step 3 still required for the behavior change and is verified by the full suite in Task 7.)

- [ ] **Step 3: Repoint the two callers**

In `server/internal/game/combat_ai.go`, in `evaluateCombatLocked`, change line ~323 from:

```go
			s.assignEnemyObjectiveLocked(unit, ctx.blocked)
```

to:

```go
			s.enemyAdvanceToObjectiveLocked(unit, ctx.blocked)
```

In the same file, in `applyBuildingUnreachableEscalationLocked` (strike-3 block), change:

```go
			if !s.acquireNearestBlockingHostileLocked(unit, blocked) {
				s.assignEnemyObjectiveLocked(unit, blocked)
			}
```

to (the new function already encapsulates the acquire-blocker fallback as its step 4, so a single call is sufficient and non-redundant):

```go
			s.enemyAdvanceToObjectiveLocked(unit, blocked)
```

Also update the now-stale comment a few lines above at ~line 306-310 (it names the deleted function). Change the comment text `assignEnemyObjectiveLocked` → `enemyAdvanceToObjectiveLocked` in that comment block and in the `state.go:275` / `state.go:589` comments (`NextObjectiveSearchTick` / `nextGlobalObjectiveSearchTick`) and `combat_ai.go:86` (`enemyObjectiveSearchCooldownTicks`). These are comment-only edits.

- [ ] **Step 4: Delete `assignEnemyObjectiveLocked`**

In `server/internal/game/combat_ai_retreat.go`, delete the entire `assignEnemyObjectiveLocked` function (the `func (s *GameState) assignEnemyObjectiveLocked(...) { ... }` block, ~lines 357-407, ending at the brace before `enemyAdvanceToObjectiveLocked`).

Verify no references remain:

Run: `cd "c:/Personal Dev/webrts" && git grep -n "assignEnemyObjectiveLocked" -- '*.go'`
Expected: no output (zero matches).

- [ ] **Step 5: Run to verify it passes + package builds**

Run: `cd "c:/Personal Dev/webrts/server" && go build ./... && go test ./internal/game/ -run TestEnemy_AttackMovesToTownhall_EngagesEnRoute -count=1 -v`
Expected: build exit 0; test PASS.

- [ ] **Step 6: Commit**

```bash
cd "c:/Personal Dev/webrts" && git add server/internal/game/combat_ai.go server/internal/game/combat_ai_retreat.go server/internal/game/state.go server/internal/game/enemy_objective_attack_move_test.go && git commit -m "feat: route enemies through enemyAdvanceToObjective; retire assignEnemyObjectiveLocked

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Seed `ObjectiveBuildingID` at spawn

So the objective is sticky from spawn (not first re-acquired lazily).

**Files:**
- Modify: `server/internal/game/state_waves.go` — the spawn block (~lines 367-393)
- Test: `server/internal/game/enemy_objective_attack_move_test.go`

- [ ] **Step 1: Write the failing test**

Append to `server/internal/game/enemy_objective_attack_move_test.go`:

```go
// The spawn-time objective resolver seeds ObjectiveBuildingID for routed
// enemies (targetPlayerLabel and default), and leaves it empty for stay-at-
// spawn / static-objective units. Tests the extracted helper directly to
// avoid driving the wave-timer machinery.
func TestSpawnObjectiveSeeding(t *testing.T) {
	s := newObjectiveTestState(t)
	defer s.mu.Unlock()

	enemy := s.spawnEnemyUnitLocked("raider", protocol.Vec2{X: 2200, Y: 768})
	if enemy == nil {
		t.Fatal("spawnEnemyUnitLocked returned nil")
	}
	s.seedEnemyObjectiveAtSpawnLocked(enemy, "", protocol.Vec2{X: 2200, Y: 768})
	if enemy.ObjectiveBuildingID != "townhall-1" {
		t.Fatalf("default route should seed townhall-1; got %q", enemy.ObjectiveBuildingID)
	}

	stay := s.spawnEnemyUnitLocked("raider", protocol.Vec2{X: 2200, Y: 700})
	s.seedEnemyObjectiveAtSpawnLocked(stay, "__none__", protocol.Vec2{X: 2200, Y: 700})
	if stay.ObjectiveBuildingID != "" {
		t.Fatalf("stay-at-spawn (__none__) must NOT seed an objective; got %q", stay.ObjectiveBuildingID)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd "c:/Personal Dev/webrts/server" && go test ./internal/game/ -run TestSpawnObjectiveSeeding -count=1 -v`
Expected: build failure — `s.seedEnemyObjectiveAtSpawnLocked` undefined.

- [ ] **Step 3: Extract and call the seeding helper**

In `server/internal/game/state_waves.go`, add this helper (place it just before the spawn loop function or directly after it):

```go
// seedEnemyObjectiveAtSpawnLocked sets a routed enemy's sticky
// ObjectiveBuildingID at spawn so it does not have to lazily re-acquire on its
// first no-target eval. Mirrors the spawn routing rules: __none__ and static-
// objective (ObjectiveID already set) units get no objective; targetPlayerLabel
// units prefer that player's townhall; the default routes to the nearest
// player townhall. spawnPos is the unit's spawn position (origin for the
// nearest-townhall search).
func (s *GameState) seedEnemyObjectiveAtSpawnLocked(unit *Unit, targetPlayerLabel string, spawnPos protocol.Vec2) {
	if unit == nil || unit.ObjectiveID != "" || targetPlayerLabel == "__none__" {
		return
	}
	var b *protocol.BuildingTile
	if targetPlayerLabel != "" && unit.TargetPlayerID != "" {
		b = s.findNearestAttackableBuildingForPlayerLocked(unit, unit.TargetPlayerID)
	}
	if b == nil {
		b = s.getNearestPlayerTownhallBuildingLocked(spawnPos.X, spawnPos.Y)
	}
	if b != nil {
		unit.ObjectiveBuildingID = b.ID
	}
}
```

Then wire it into the spawn loop in `tickEnemySpawnpointsLocked`. The `objectiveId != ""` branch already sets `unit.ObjectiveID`; the `targetPlayerLabel != ""` branch already sets `unit.TargetPlayerID`. Add a single call at the end of the per-unit setup, right after the existing `if objectiveId != "" { ... } else if targetPlayerLabel == "__none__" { ... } else if targetPlayerLabel != "" { ... } else { ... }` chain (i.e., immediately before the `}` that closes the `for i := 0; i < spawnCount; i++` loop body, after the default-route block ending ~line 393):

```go
				s.seedEnemyObjectiveAtSpawnLocked(unit, targetPlayerLabel, spawnPos)
```

This is additive — the existing `assignUnitPath` move calls in those branches are unchanged; the new line only records the sticky objective ID so `enemyAdvanceToObjectiveLocked` keeps it stable instead of lazily re-resolving.

- [ ] **Step 4: Run to verify it passes**

Run: `cd "c:/Personal Dev/webrts/server" && go test ./internal/game/ -run TestSpawnObjectiveSeeding -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd "c:/Personal Dev/webrts" && git add server/internal/game/state_waves.go server/internal/game/enemy_objective_attack_move_test.go && git commit -m "feat: seed sticky ObjectiveBuildingID at enemy spawn

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Unreachable AI-acquired unit target → drop & switch

**Files:**
- Modify: `server/internal/game/combat_ai_retreat.go` — `assignAttackApproachPathLockedWithSubBlocked` (~lines 84-139), provenance branch on the three `enterAttackDriftLocked` call sites
- Modify: `server/internal/game/combat_ai_scoring.go` — `selectBestTargetLocked` primary unit loop (~lines 87-101)
- Test: `server/internal/game/enemy_unreachable_unit_retarget_test.go` (create)

- [ ] **Step 1: Write the failing tests**

Create `server/internal/game/enemy_unreachable_unit_retarget_test.go`:

```go
package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// AI-acquired unit target that becomes unreachable: the enemy must drop it,
// memo it on the unreachable cooldown, and NOT enter drift mode. (drift =
// AttackDrifting true with a straight-line TargetX/Y and no path.)
func TestUnreachableUnit_AIAcquired_DropsNotDrift(t *testing.T) {
	s := newObjectiveTestState(t)
	defer s.mu.Unlock()
	ownerID := "p1"

	// Full unit-wall partition; a lone player unit sits behind it (unreachable).
	for y := 10.0; y <= s.MapHeight-10.0; y += 20.0 {
		w := s.spawnPlayerUnitLocked("soldier", ownerID, "#3498db", protocol.Vec2{X: 1200, Y: y})
		w.Visible = true
		w.MaxHP, w.HP = 1000, 1000
		w.MoveSpeed = 0
		w.Damage = 0
		w.Capabilities = nil
		s.initializeCombatUnitLocked(w)
	}
	behind := s.spawnPlayerUnitLocked("soldier", ownerID, "#3498db", protocol.Vec2{X: 400, Y: 768})
	behind.Visible = true
	behind.MaxHP, behind.HP = 50, 50
	s.initializeCombatUnitLocked(behind)
	behindID := behind.ID

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c",
		protocol.Vec2{X: 1300, Y: 768})
	enemy.Visible = true
	enemy.MaxHP, enemy.HP = 800, 800
	enemy.MoveSpeed = 150
	s.initializeCombatUnitLocked(enemy)
	blocked := s.getBlockedCellsLocked()

	// Force the unreachable unit as the AI-acquired target.
	enemy.AttackTargetID = behindID
	target := s.unitsByID[behindID]
	s.assignAttackApproachPathLocked(enemy, target, blocked)

	if enemy.AttackDrifting {
		t.Fatal("AI-acquired unreachable unit must NOT enter drift mode")
	}
	if enemy.UnreachableUnitTargetID != behindID {
		t.Fatalf("unreachable unit must be memoed; got %d want %d",
			enemy.UnreachableUnitTargetID, behindID)
	}
	if enemy.AttackTargetID != 0 {
		t.Fatalf("unreachable target must be dropped; AttackTargetID=%d", enemy.AttackTargetID)
	}
}

// selectBestTargetLocked must skip a unit while it is on the unreachable
// cooldown, so the enemy picks a reachable alternative instead.
func TestUnreachableUnit_SkippedBySelection(t *testing.T) {
	s := newObjectiveTestState(t)
	defer s.mu.Unlock()
	ownerID := "p1"

	near := s.spawnPlayerUnitLocked("soldier", ownerID, "#3498db", protocol.Vec2{X: 1260, Y: 768})
	near.Visible = true
	near.MaxHP, near.HP = 50, 50
	s.initializeCombatUnitLocked(near)
	nearID := near.ID

	far := s.spawnPlayerUnitLocked("soldier", ownerID, "#3498db", protocol.Vec2{X: 1240, Y: 768})
	far.Visible = true
	far.MaxHP, far.HP = 50, 50
	s.initializeCombatUnitLocked(far)
	farID := far.ID

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c",
		protocol.Vec2{X: 1250, Y: 768})
	enemy.Visible = true
	enemy.MaxHP, enemy.HP = 800, 800
	s.initializeCombatUnitLocked(enemy)

	// Mark `far` unreachable for a window that covers this tick.
	enemy.UnreachableUnitTargetID = farID
	enemy.UnreachableUnitUntilTick = s.Tick + 100

	profile := resolveCombatProfile(enemy)
	idx := newCombatSpatialIndex(combatSpatialBucketSize)
	for _, u := range s.Units {
		if u != nil && u.Visible && u.HP > 0 {
			idx.add(u)
		}
	}
	best := s.selectBestTargetLocked(enemy, profile, combatEvalContext{index: idx, blocked: s.getBlockedCellsLocked()})
	if best.Kind != combatTargetUnit || best.Unit == nil || best.Unit.ID != nearID {
		t.Fatalf("must skip unreachable %d and pick reachable %d; got kind=%d unit=%v",
			farID, nearID, best.Kind, best.Unit)
	}
}

// Player-issued (OrderAttackTarget) unreachable unit still uses drift mode —
// the deliberate "the player explicitly chose this fight" invariant.
func TestUnreachableUnit_PlayerIssued_StillDrifts(t *testing.T) {
	s := newObjectiveTestState(t)
	defer s.mu.Unlock()
	ownerID := "p1"

	for y := 10.0; y <= s.MapHeight-10.0; y += 20.0 {
		w := s.spawnPlayerUnitLocked("soldier", ownerID, "#3498db", protocol.Vec2{X: 1200, Y: y})
		w.Visible = true
		w.MaxHP, w.HP = 1000, 1000
		w.MoveSpeed = 0
		w.Damage = 0
		w.Capabilities = nil
		s.initializeCombatUnitLocked(w)
	}
	enemyVictim := s.spawnPlayerUnitLocked("soldier", "p2", "#9b59b6", protocol.Vec2{X: 400, Y: 768})
	enemyVictim.Visible = true
	enemyVictim.MaxHP, enemyVictim.HP = 50, 50
	s.initializeCombatUnitLocked(enemyVictim)
	victimID := enemyVictim.ID

	player := s.spawnPlayerUnitLocked("soldier", ownerID, "#3498db", protocol.Vec2{X: 1300, Y: 768})
	player.Visible = true
	player.MaxHP, player.HP = 800, 800
	player.MoveSpeed = 150
	s.initializeCombatUnitLocked(player)
	player.Order = OrderState{Type: OrderAttackTarget}
	player.AttackTargetID = victimID
	blocked := s.getBlockedCellsLocked()

	s.assignAttackApproachPathLocked(player, s.unitsByID[victimID], blocked)

	if !player.AttackDrifting {
		t.Fatal("player-issued unreachable target must still drift")
	}
	if player.UnreachableUnitTargetID != 0 {
		t.Fatalf("player-issued target must NOT be memoed/dropped; memo=%d", player.UnreachableUnitTargetID)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd "c:/Personal Dev/webrts/server" && go test ./internal/game/ -run TestUnreachableUnit_ -count=1 -v`
Expected: FAIL — `TestUnreachableUnit_AIAcquired_DropsNotDrift` fails (enemy drifts instead of dropping); `TestUnreachableUnit_SkippedBySelection` fails (unreachable unit not skipped).

- [ ] **Step 3: Add a provenance helper + branch the drift call sites**

In `server/internal/game/combat_ai_retreat.go`, add this helper above `assignAttackApproachPathLockedWithSubBlocked` (~line 79):

```go
// dropUnreachableAITargetLocked handles an AI-acquired unit target that A*
// could not reach: memo it on a short cooldown so selectBestTargetLocked skips
// it, then clear the target so the next eval picks a reachable alternative (or
// resumes the objective advance when none is in range). Player-issued targets
// (OrderAttackTarget) do NOT come here — they keep drift mode, preserving the
// "the player explicitly chose this fight" invariant.
func (s *GameState) dropUnreachableAITargetLocked(unit, target *Unit) {
	unit.UnreachableUnitTargetID = target.ID
	unit.UnreachableUnitUntilTick = s.Tick + unreachableTargetCooldownTicks
	s.clearCombatTargetLocked(unit)
}
```

> `unreachableTargetCooldownTicks` is the existing constant already used for building strike-1 (combat_ai.go). Reusing it satisfies the spec's two constraints (outlasts the retarget stagger; short enough to re-aggro a returned unit quickly) without introducing a new hardcoded tunable. If a distinct unit value is later desired it can be added then — YAGNI now.

In `assignAttackApproachPathLockedWithSubBlocked`, edit the **three** `enterAttackDriftLocked` sites precisely as below (do not introduce a duplicate `return`; `go vet` flags unreachable code).

**Site 1 — `goalCell` not walkable (~lines 106-110).** Replace:

```go
	goalCell, ok := s.findNearestWalkable(targetCell, blocked)
	if !ok {
		s.enterAttackDriftLocked(unit, target)
		return
	}
```

with:

```go
	goalCell, ok := s.findNearestWalkable(targetCell, blocked)
	if !ok {
		if unit.Order.Type == OrderAttackTarget {
			s.enterAttackDriftLocked(unit, target)
		} else {
			s.dropUnreachableAITargetLocked(unit, target)
		}
		return
	}
```

**Site 2 — empty `fullPath` (~lines 112-116).** Replace:

```go
	fullPath := s.findPath(startCell, goalCell, blocked, nil)
	if len(fullPath) == 0 {
		s.enterAttackDriftLocked(unit, target)
		return
	}
```

with:

```go
	fullPath := s.findPath(startCell, goalCell, blocked, nil)
	if len(fullPath) == 0 {
		if unit.Order.Type == OrderAttackTarget {
			s.enterAttackDriftLocked(unit, target)
		} else {
			s.dropUnreachableAITargetLocked(unit, target)
		}
		return
	}
```

**Site 3 — `assignUnitPath` failed (~lines 133-138, end of function).** Replace:

```go
	if !unit.Moving {
		s.enterAttackDriftLocked(unit, target)
	}
}
```

with (no `return` — this is the last statement in the function):

```go
	if !unit.Moving {
		if unit.Order.Type == OrderAttackTarget {
			s.enterAttackDriftLocked(unit, target)
		} else {
			s.dropUnreachableAITargetLocked(unit, target)
		}
	}
}
```

- [ ] **Step 4: Skip unreachable units in selection**

In `server/internal/game/combat_ai_scoring.go`, in `selectBestTargetLocked`'s **primary** unit loop, after the existing plane check and before the leash check (between lines ~92 and ~94):

```go
		if !unitCanTargetPlane(unit, hostile) {
			continue
		}
```

add immediately below it:

```go
		// Skip a unit memoed unreachable (A* failed within the cooldown window)
		// so the enemy switches to a reachable target instead of re-picking the
		// one it cannot path to. Mirrors the building skip a few blocks down.
		if hostile.ID == unit.UnreachableUnitTargetID && s.Tick < unit.UnreachableUnitUntilTick {
			continue
		}
```

> Scope note (matches spec §3.6): the skip is applied to the primary detection-range unit loop only. The threat-table retaliation loop and taunt override are left untouched — those are deliberate, sensitive bypass mechanisms, and the memo's job is to stop *fresh acquisition* of the unreachable unit, not to suppress retaliation.

- [ ] **Step 5: Run to verify it passes**

Run: `cd "c:/Personal Dev/webrts/server" && go test ./internal/game/ -run TestUnreachableUnit_ -count=1 -v`
Expected: PASS (all three).

- [ ] **Step 6: Commit**

```bash
cd "c:/Personal Dev/webrts" && git add server/internal/game/combat_ai_retreat.go server/internal/game/combat_ai_scoring.go server/internal/game/enemy_unreachable_unit_retarget_test.go && git commit -m "feat: drop+switch off unreachable AI-acquired unit targets (keep drift for player-issued)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: Update regression test, full suite, self-review

**Files:**
- Modify: `server/internal/game/enemy_blocked_objective_test.go` (doc comment only)

- [ ] **Step 1: Update the walled-off regression test doc comment**

In `server/internal/game/enemy_blocked_objective_test.go`, the behavioral assertion (enemy engages a wall unit, never freezes) is unchanged and still valid. Update only the stale mechanism description. Replace the comment phrase:

```
// in a perpetual failed-pathfind loop.
```

with:

```
// in a perpetual failed-pathfind loop. Under the objective-attack-move model
// the path runs enemyAdvanceToObjectiveLocked: the townhall is unreachable
// (full partition) so step 4 falls back to acquireNearestBlockingHostile,
// the enemy closes on the wall, and normal in-range scoring acquires a wall
// unit — same observable invariant, new mechanism.
```

And in the same comment block replace the line:

```
//     a blocker is the unreachable-building fallback this test is exercising.
```

with:

```
//     a blocker is the objective-unreachable fallback this test is exercising.
```

- [ ] **Step 2: Run the walled-off regression test**

Run: `cd "c:/Personal Dev/webrts/server" && go test ./internal/game/ -run TestEnemy_WalledOffFromBuilding_EngagesBlockers -count=1 -v`
Expected: PASS (behavior preserved via the new mechanism).

- [ ] **Step 3: Run the full game package suite**

Run: `cd "c:/Personal Dev/webrts/server" && go test ./internal/game/ -count=1`
Expected: `ok  	webrts/server/internal/game`. If any pre-existing test now fails, it is exercising old hard-target/drift behavior that the spec deliberately changes — read it, and if its assertion encodes superseded behavior (e.g. expects `AttackBuildingTargetID` set while advancing, or expects drift for an AI-acquired unreachable unit), update the assertion to the new invariant and note it in the commit. Do NOT loosen an assertion that is still a valid invariant.

- [ ] **Step 4: Run the broader build + vet**

Run: `cd "c:/Personal Dev/webrts/server" && go build ./... && go vet ./internal/game/`
Expected: both exit 0, no output.

- [ ] **Step 5: Plan self-review checklist**

Verify against the spec ([docs/superpowers/specs/2026-05-18-enemy-objective-attack-move-design.md](../specs/2026-05-18-enemy-objective-attack-move-design.md)):
- Spec §1 attack-move model → Tasks 3, 4 (`enemyAdvanceToObjectiveLocked` + caller repoint).
- Spec §2 state + sticky-on-loss → Tasks 1, 3, 5.
- Spec §3 code changes (new fn, caller repoint, spawn seed, retire old, provenance branch, selection skip) → Tasks 3, 4, 5, 6.
- Spec §3.1 4-step fallback incl. partition → Task 3 (`TestEnemyAdvanceToObjective_PartitionFallsBackToBlocker`).
- Spec Decision 2 (TargetPlayerID-first re-acquire) → Task 3 step 3 + Task 5.
- Spec Decision 3 / §5 unreachable-unit → Task 6.
- Spec determinism (ID tiebreak) → Task 2.
- Spec testing intent (advance/destroy, en-route engage, re-acquire, partition, unreachable-unit drop+switch, player-issued drift, anti-freeze regression) → Tasks 2–7 tests.
Confirm no remaining `assignEnemyObjectiveLocked` reference (`git grep -n assignEnemyObjectiveLocked -- '*.go'` → empty). Confirm method/field names are consistent across tasks: `ObjectiveBuildingID`, `UnreachableUnitTargetID`, `UnreachableUnitUntilTick`, `enemyAdvanceToObjectiveLocked`, `seedEnemyObjectiveAtSpawnLocked`, `getNearestPlayerTownhallBuildingLocked`, `dropUnreachableAITargetLocked`.

- [ ] **Step 6: Final commit**

```bash
cd "c:/Personal Dev/webrts" && git add server/internal/game/enemy_blocked_objective_test.go && git commit -m "test: update walled-off regression doc for objective-attack-move mechanism

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Notes for the implementing engineer

- **Determinism is a hard constraint** ([.claude/rules/AI_RULES.md](../../../.claude/rules/AI_RULES.md)): never iterate a map to drive an outcome; the building-ID tiebreak (Task 2) exists for this reason.
- **`*Locked` discipline:** every new function added here is `*Locked` and assumes `s.mu` held. Tests acquire `s.mu.Lock()` (mutation) or `s.mu.RLock()` (post-`tickN` assertions) exactly as the existing tests in this package do.
- **ID-not-pointer:** `ObjectiveBuildingID` / `UnreachableUnitTargetID` are stored as IDs and resolved+validated at point-of-use (`getBuildingByIDLocked` + `isValidHostileBuildingTarget`, `unitsByID`/`getUnitByIDLocked`). Do not cache `*BuildingTile` / `*Unit` on the struct.
- If `rebuildBuildingIndexLocked` does not exist under that exact name, find the actual index-population site with `git grep -n "buildingsByID\[" -- '*.go'` and use it (see Task 2 Step 3 note).
- The QA engineer (qa-engineer subagent) owns the final "no per-tick churn" stress assertion and any additional edge coverage; the tests here establish the behavioral invariants the spec requires.
