# Claim Zone Capture Points Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a map author configure a claim zone with N independently-captured 2×2 tower slots instead of the single hard-coded anchor slot.

**Architecture:** Capture-point geometry is a new optional top-level `ClaimPoints` field on `protocol.Zone` (mirroring how presence's `CaptureCells` lives top-level). Each point gets its own defend timer + captured flag in a per-tick `claimPointState` slice on `zoneRuntime`; the zone flips to the team once every point is captured. An empty `ClaimPoints` falls back to a single slot at the anchor, so all existing maps are byte-for-byte unchanged.

**Tech Stack:** Go backend (`server/internal/game`, `server/pkg/protocol`), Vue 3 + TypeScript client (`client/src/game-portal`). Backend tests via `go test`; frontend verified via `vue-tsc` typecheck.

**Design doc:** [docs/superpowers/specs/2026-06-22-claim-zone-capture-points-design.md](../specs/2026-06-22-claim-zone-capture-points-design.md)

---

## File Structure

**Backend (Go):**
- `server/pkg/protocol/messages.go` — add `Zone.ClaimPoints`, `ZoneSnapshot.ClaimPoints`, new `ZoneClaimPointSnapshot` type.
- `server/internal/game/zone_runtime.go` — `claimPointState` slice on `zoneRuntime`, build it in `installZonesLocked`, `claimPointCells` helper, per-point snapshot emit, `rt.Progress` aggregate.
- `server/internal/game/zone_handlers.go` — multi-point `isClaimSlotCell`, `claimTowerOnSlotLocked`, `claimSlotBuildableLocked`, `claimZoneTowerLocked`, and the rewritten `evaluateClaimCapture`.
- `server/internal/game/zone_runtime_test.go` — new multi-point test cases.

**Frontend (TypeScript/Vue):**
- `client/src/game-portal/src/game/network/protocol.ts` — `Zone.claimPoints`, `ZoneSnapshot.claimPoints` mirrors.
- `client/src/game-portal/src/components/MapEditorPanel.vue` — "Place Capture Point" sub-mode, add/remove helpers, click handlers, UI button + hint.
- `client/src/game-portal/src/game/rendering/CanvasRenderer.ts` — draw all claim slots, per-point captured state, "N/M points" HUD.

Backend tasks (1–6) are ordered so the package compiles and tests pass after each. Frontend tasks (7–9) follow.

---

## Task 1: Protocol — add `Zone.ClaimPoints` (Go + ts)

**Files:**
- Modify: `server/pkg/protocol/messages.go` (the `Zone` struct, ~line 100-123)
- Modify: `client/src/game-portal/src/game/network/protocol.ts` (the `Zone` type, ~line 247-268)

- [ ] **Step 1: Add the Go field**

In `messages.go`, inside `type Zone struct`, add after the `CaptureCells [][2]int` field (around line 116):

```go
	// ClaimPoints is the optional list of capture-point slots for the "claim"
	// capture mechanic: each entry is the top-left cell of a 2x2 tower slot. The
	// team must build and defend a tower on EVERY point to capture the zone; each
	// point is captured independently and stays captured (sticky) once defended.
	// EMPTY/absent ⇒ a single slot at Anchor (backward-compatible default). Only
	// meaningful when Capture.Type == "claim".
	ClaimPoints [][2]int `json:"claimPoints,omitempty"`
```

- [ ] **Step 2: Add the ts mirror**

In `protocol.ts`, inside the `Zone` type, add after the `captureCells?: [number, number][]` field (~line 262):

```ts
  /** Capture-point slots for the CLAIM mechanic: each is the top-left cell of a
   *  2x2 tower slot. The team must build + defend a tower on every point to
   *  capture the zone. Empty/absent ⇒ a single slot at `anchor`. */
  claimPoints?: [number, number][]
```

- [ ] **Step 3: Verify the backend compiles**

Run: `cd "c:\Personal Dev\webrts\server" && go build ./...`
Expected: no output (success).

- [ ] **Step 4: Verify the client typechecks**

Run: `cd "c:\Personal Dev\webrts\client\src\game-portal" && npx vue-tsc -b`
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add server/pkg/protocol/messages.go client/src/game-portal/src/game/network/protocol.ts
git commit -m "feat(zones): add Zone.claimPoints field for multi-point claim zones"
```

---

## Task 2: Protocol — add per-point snapshot type (Go + ts)

**Files:**
- Modify: `server/pkg/protocol/messages.go` (`ZoneSnapshot` struct, ~line 1438-1445)
- Modify: `client/src/game-portal/src/game/network/protocol.ts` (`ZoneSnapshot` type, ~line 271-279)

- [ ] **Step 1: Add the Go snapshot type + field**

In `messages.go`, immediately above `type ZoneSnapshot struct`, add the per-point type, then add the field inside `ZoneSnapshot`:

```go
// ZoneClaimPointSnapshot is the per-tick wire view of one claim capture point.
// Progress is a normalised 0..1 fraction of the shared defendSeconds; Captured
// is set once the point has been defended to completion (sticky). Entries are
// in the same authored order as Zone.ClaimPoints.
type ZoneClaimPointSnapshot struct {
	Progress float64 `json:"progress"`
	Captured bool    `json:"captured,omitempty"`
}
```

Then inside `type ZoneSnapshot struct`, add after the existing `Progress` / `OwnerColor` fields:

```go
	// ClaimPoints carries per-capture-point progress for a multi-point claim
	// zone, in authored order. Empty for non-claim zones. Drives the "N/M points"
	// HUD and per-point timer bars.
	ClaimPoints []ZoneClaimPointSnapshot `json:"claimPoints,omitempty"`
```

- [ ] **Step 2: Add the ts mirror**

In `protocol.ts`, inside the `ZoneSnapshot` type, add after the `progress?: number` field (~line 275):

```ts
  /** Per-capture-point progress for a multi-point CLAIM zone, in the same
   *  order as the zone's `claimPoints`. `progress` is a 0..1 fraction of the
   *  shared defend duration; `captured` flips true once the point is held. */
  claimPoints?: { progress: number; captured?: boolean }[]
```

- [ ] **Step 3: Verify backend compiles + client typechecks**

Run: `cd "c:\Personal Dev\webrts\server" && go build ./...`
Expected: success.
Run: `cd "c:\Personal Dev\webrts\client\src\game-portal" && npx vue-tsc -b`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add server/pkg/protocol/messages.go client/src/game-portal/src/game/network/protocol.ts
git commit -m "feat(zones): add ZoneClaimPointSnapshot for per-point claim progress"
```

---

## Task 3: Runtime — per-point state slice + `claimPointCells` helper

**Files:**
- Modify: `server/internal/game/zone_runtime.go` (`zoneRuntime` struct ~line 22-43; `installZonesLocked` ~line 19-64)
- Modify: `server/internal/game/zone_handlers.go` (add `claimPointCells` helper)
- Test: `server/internal/game/zone_runtime_test.go`

- [ ] **Step 1: Write the failing test**

Add to `zone_runtime_test.go`. This drives a two-point claim zone and asserts the runtime built two independent point states at install. Add a multi-point claim zone builder helper alongside the existing `claimZone` helper:

```go
func multiPointClaimZone(id string, points [][2]int, cells [][2]int, adjacent ...string) protocol.Zone {
	anchor := [2]int{points[0][0], points[0][1]}
	return protocol.Zone{
		ID:            id,
		Anchor:        protocol.GridCoord{X: anchor[0], Y: anchor[1]},
		Cells:         cells,
		Capture:       protocol.ZoneCapture{Type: "claim", Config: json.RawMessage(`{"defendSeconds":3,"towerType":"Tower"}`)},
		ClaimPoints:   points,
		StartingOwner: "neutral",
		Adjacent:      adjacent,
	}
}

func TestInstallZones_BuildsClaimPointStates(t *testing.T) {
	seed := presenceZone("seed", rectCells(0, 0, 4, 4), [2]int{2, 2}, "p1")
	claim := multiPointClaimZone("claim", [][2]int{{6, 6}, {10, 6}}, rectCells(5, 5, 14, 9), "seed")
	s := newZoneTestState([]protocol.Zone{seed, claim})

	rt := s.zoneRuntimeByIDLocked("claim")
	if got := len(rt.claimPoints); got != 2 {
		t.Fatalf("two-point claim zone should build 2 point states, got %d", got)
	}
	// A claim zone with NO explicit points falls back to a single anchor slot.
	single := claimZone("single", [2]int{20, 20}, rectCells(19, 19, 23, 23))
	s2 := newZoneTestState([]protocol.Zone{single})
	if got := len(s2.zoneRuntimeByIDLocked("single").claimPoints); got != 1 {
		t.Fatalf("anchor-fallback claim zone should build 1 point state, got %d", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd "c:\Personal Dev\webrts\server" && go test ./internal/game/ -run TestInstallZones_BuildsClaimPointStates -v`
Expected: FAIL — compile error (`rt.claimPoints` undefined / `ClaimPoints` unknown field on the builder). That is the expected red.

- [ ] **Step 3: Add the `claimPointState` type + runtime field**

In `zone_runtime.go`, add the type above `type zoneRuntime struct` and a field inside it (after `locked bool`):

```go
// claimPointState is the per-tick mutable state of one claim capture point.
// Progress is the defend-timer accumulator (seconds); Captured is sticky once
// the point has been defended for the zone's shared defendSeconds. Stores no
// entity references — pure tick working state.
type claimPointState struct {
	Progress float64
	Captured bool
}
```

Inside `zoneRuntime`, add:

```go
	// claimPoints holds one state per claim capture point, in the same order as
	// Def.ClaimPoints (or a single entry for the anchor fallback). Built once at
	// install; nil for non-claim zones.
	claimPoints []claimPointState
```

- [ ] **Step 4: Add the `claimPointCells` helper**

In `zone_handlers.go`, add near `isClaimSlotCell`:

```go
// claimPointCells returns the top-left cell of each claim capture-point slot
// for rt: the authored Def.ClaimPoints, or a single anchor slot when none are
// authored (backward-compatible default). Used to size the per-point state and
// to drive all claim slot geometry.
func claimPointCells(rt *zoneRuntime) [][2]int {
	if len(rt.Def.ClaimPoints) > 0 {
		return rt.Def.ClaimPoints
	}
	return [][2]int{{rt.Def.Anchor.X, rt.Def.Anchor.Y}}
}
```

- [ ] **Step 5: Size the slice in `installZonesLocked`**

In `zone_runtime.go`, inside `installZonesLocked`, after the `zoneRuntime{...}` is appended to `s.Zones`, build the per-point slice for claim zones. Change the append + loop block (currently appends then indexes cells). Replace the append so it captures the index, then add the claim-point sizing:

```go
		rt := zoneRuntime{
			Def:          z,
			Owner:        owner,
			captureCfg:   cfg,
			captureCells: captureCells,
			locked:       locked,
		}
		if z.Capture.Type == "claim" {
			rt.claimPoints = make([]claimPointState, len(claimPointCells(&rt)))
		}
		s.Zones = append(s.Zones, rt)
		for _, c := range z.Cells {
			s.zoneCellIndex[gridPoint{X: c[0], Y: c[1]}] = z.ID
		}
```

Note: `claimPointCells(&rt)` reads `rt.Def`, which is already set, so it is safe to call on the local `rt` before the append.

- [ ] **Step 6: Run the test to verify it passes**

Run: `cd "c:\Personal Dev\webrts\server" && go test ./internal/game/ -run TestInstallZones_BuildsClaimPointStates -v`
Expected: PASS.

- [ ] **Step 7: Run the full zone suite to confirm no regressions**

Run: `cd "c:\Personal Dev\webrts\server" && go test ./internal/game/ -run 'Zone|Claim|Presence|Clear|Control' -v`
Expected: PASS (existing claim/presence/clear tests still green).

- [ ] **Step 8: Commit**

```bash
git add server/internal/game/zone_runtime.go server/internal/game/zone_handlers.go server/internal/game/zone_runtime_test.go
git commit -m "feat(zones): build per-point claim state at install with anchor fallback"
```

---

## Task 4: Geometry & gate helpers — make them multi-point

**Files:**
- Modify: `server/internal/game/zone_handlers.go` (`isClaimSlotCell` ~217, `claimTowerOnSlotLocked` ~225, `claimZoneTowerLocked` ~252, `claimSlotBuildableLocked` ~270)
- Test: `server/internal/game/zone_runtime_test.go`

- [ ] **Step 1: Write the failing test**

Add to `zone_runtime_test.go`. Asserts the build gate accepts a tower on EVERY point and `claimZoneTowerLocked` finds a standing tower on the second point:

```go
func TestClaimMultiPoint_BuildGateAndTowerLookup(t *testing.T) {
	seed := presenceZone("seed", rectCells(0, 0, 4, 4), [2]int{2, 2}, "p1")
	claim := multiPointClaimZone("claim", [][2]int{{6, 6}, {10, 6}}, rectCells(5, 5, 14, 9), "seed")
	s := newZoneTestState([]protocol.Zone{seed, claim})
	rt := s.zoneRuntimeByIDLocked("claim")

	// The Tower is buildable on BOTH point slots, not just the first.
	if !s.claimSlotBuildableLocked(rt, gridPoint{X: 6, Y: 6}, "Tower") {
		t.Fatal("tower should be buildable on point 1 slot")
	}
	if !s.claimSlotBuildableLocked(rt, gridPoint{X: 10, Y: 6}, "Tower") {
		t.Fatal("tower should be buildable on point 2 slot")
	}
	// A cell in no point's slot is rejected.
	if s.claimSlotBuildableLocked(rt, gridPoint{X: 14, Y: 9}, "Tower") {
		t.Fatal("a non-slot cell must not be buildable")
	}
	// A standing tower on point 2 is found by claimZoneTowerLocked.
	if s.claimZoneTowerLocked("claim") != nil {
		t.Fatal("no tower built yet → nil")
	}
	s.placeClaimTower("p1", 10, 6) // on point 2
	if got := s.claimZoneTowerLocked("claim"); got == nil {
		t.Fatal("a tower standing on point 2 should be found")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd "c:\Personal Dev\webrts\server" && go test ./internal/game/ -run TestClaimMultiPoint_BuildGateAndTowerLookup -v`
Expected: FAIL — the second-point assertions fail because the helpers only consider the anchor slot.

- [ ] **Step 3: Rewrite `isClaimSlotCell` for any point**

Replace the existing `isClaimSlotCell` body:

```go
// isClaimSlotCell reports whether cell falls in ANY claim capture-point's 2x2
// build slot — the 2x2 block whose top-left is each point cell.
func isClaimSlotCell(rt *zoneRuntime, cell gridPoint) bool {
	for _, p := range claimPointCells(rt) {
		ax, ay := p[0], p[1]
		if cell.X >= ax && cell.X <= ax+1 && cell.Y >= ay && cell.Y <= ay+1 {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Add a per-point tower lookup, keep the slot-scan as a helper**

Replace `claimTowerOnSlotLocked` with a version that scans a single given point's 2x2, and keep the "any point" entry used by `claimZoneTowerLocked`:

```go
// claimTowerAtPointLocked returns the team-owned, fully-built tower occupying
// the 2x2 slot whose top-left is point (matching towerType if set), or nil.
// Under-construction buildings do not count.
func (s *GameState) claimTowerAtPointLocked(point [2]int, towerType string) *protocol.BuildingTile {
	ax, ay := point[0], point[1]
	for dy := 0; dy < 2; dy++ {
		for dx := 0; dx < 2; dx++ {
			b := s.buildingAtCellLocked(gridPoint{X: ax + dx, Y: ay + dy})
			if b == nil || !b.Visible || b.OwnerID == nil {
				continue
			}
			if !isHumanOwner(*b.OwnerID) {
				continue
			}
			if getMetadataBool(b.Metadata, "underConstruction") {
				continue
			}
			if towerType != "" && b.BuildingType != towerType {
				continue
			}
			return b
		}
	}
	return nil
}
```

Note: this replaces the old `claimTowerOnSlotLocked(rt, towerType)` entirely. The evaluator (Task 5) and `claimZoneTowerLocked` call the per-point version.

- [ ] **Step 5: Rewrite `claimZoneTowerLocked` to scan uncaptured points**

```go
// claimZoneTowerLocked returns the first completed team tower standing on an
// UNCAPTURED claim point of the named zone (authored order), or nil. Used to
// point capture-trigger enemy spawns at a structure the team is actively using
// to capture the zone. Deterministic (authored order, no RNG).
func (s *GameState) claimZoneTowerLocked(zoneID string) *protocol.BuildingTile {
	rt := s.zoneRuntimeByIDLocked(zoneID)
	if rt == nil {
		return nil
	}
	cfg, ok := rt.captureCfg.(claimCaptureConfig)
	if !ok {
		return nil
	}
	points := claimPointCells(rt)
	for i, p := range points {
		if i < len(rt.claimPoints) && rt.claimPoints[i].Captured {
			continue // captured points need no defending
		}
		if tower := s.claimTowerAtPointLocked(p, cfg.TowerType); tower != nil {
			return tower
		}
	}
	return nil
}
```

- [ ] **Step 6: `claimSlotBuildableLocked` already delegates to `isClaimSlotCell`**

Confirm `claimSlotBuildableLocked` (lines ~270-281) still reads correctly — it calls `isClaimSlotCell(rt, cell)` (now multi-point) and checks the `claimCaptureConfig.TowerType`. No change needed beyond the `isClaimSlotCell` rewrite. Re-read it to confirm it compiles against the new helpers.

- [ ] **Step 7: Run the test to verify it passes**

Run: `cd "c:\Personal Dev\webrts\server" && go test ./internal/game/ -run TestClaimMultiPoint_BuildGateAndTowerLookup -v`
Expected: PASS.

- [ ] **Step 8: Run the existing claim suite (anchor-fallback regression)**

Run: `cd "c:\Personal Dev\webrts\server" && go test ./internal/game/ -run 'Claim' -v`
Expected: PASS — existing single-point tests (`TestClaimZoneTowerLocked`, `TestClaimBuildGate_SlotExceptionAndType`, `TestEnemySpawnTrigger_TargetsClaimTower`) still green via the anchor fallback. Note: these call the removed `claimTowerOnSlotLocked` only indirectly through the evaluator / `claimZoneTowerLocked`, so they should compile. If any test references `claimTowerOnSlotLocked` by name, it will fail to compile — fix it in the next step.

- [ ] **Step 9: Commit**

```bash
git add server/internal/game/zone_handlers.go server/internal/game/zone_runtime_test.go
git commit -m "feat(zones): make claim slot geometry + tower lookup multi-point"
```

---

## Task 5: Evaluator — capture each point independently, flip on all-captured

**Files:**
- Modify: `server/internal/game/zone_handlers.go` (`evaluateClaimCapture` ~288-307)
- Test: `server/internal/game/zone_runtime_test.go`

- [ ] **Step 1: Write the failing test**

Add to `zone_runtime_test.go`. Drives a two-point zone: it must NOT capture with only one point defended, must capture once both are defended, and a captured point stays captured after its tower falls:

```go
func TestClaimMultiPoint_AllPointsRequiredAndSticky(t *testing.T) {
	seed := presenceZone("seed", rectCells(0, 0, 4, 4), [2]int{2, 2}, "p1")
	claim := multiPointClaimZone("claim", [][2]int{{6, 6}, {10, 6}}, rectCells(5, 5, 14, 9), "seed")
	s := newZoneTestState([]protocol.Zone{seed, claim})

	// Defend ONLY point 1 to completion (defendSeconds=3, dt 0.5 → 6 ticks).
	t1 := s.placeClaimTower("p1", 6, 6)
	for i := 0; i < 7; i++ {
		s.tickZonesLocked(0.5)
	}
	if zoneOwner(s, "claim") != protocol.ZoneCaptureNeutralOwner {
		t.Fatalf("zone must NOT capture with only 1 of 2 points held, got %q", zoneOwner(s, "claim"))
	}
	rt := s.zoneRuntimeByIDLocked("claim")
	if !rt.claimPoints[0].Captured {
		t.Fatal("point 1 should be captured")
	}
	// Point 1's tower is destroyed — it stays captured (sticky per point).
	t1.Visible = false
	s.tickZonesLocked(0.5)
	if !rt.claimPoints[0].Captured {
		t.Fatal("a captured point must stay captured after its tower falls")
	}
	// Now defend point 2 → the whole zone flips to the team.
	s.placeClaimTower("p1", 10, 6)
	for i := 0; i < 7; i++ {
		s.tickZonesLocked(0.5)
	}
	if got := zoneOwner(s, "claim"); got != protocol.ZoneCaptureTeamOwner {
		t.Fatalf("zone should capture once BOTH points are held, got %q", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd "c:\Personal Dev\webrts\server" && go test ./internal/game/ -run TestClaimMultiPoint_AllPointsRequiredAndSticky -v`
Expected: FAIL — the current single-slot evaluator captures the zone as soon as one tower is defended (and `rt.claimPoints` indexing may panic), so the "must not capture with 1 of 2" assertion fails.

- [ ] **Step 3: Rewrite `evaluateClaimCapture`**

Replace the whole function body:

```go
// evaluateClaimCapture advances a per-point defend timer for every uncaptured
// claim capture point that has a completed team tower standing on its 2x2 slot.
// Each point captures independently once its timer reaches the shared
// defendSeconds and stays captured (sticky). The zone flips to the team only
// once EVERY point is captured. A point with no/destroyed tower resets its own
// timer — the team must keep each tower alive for the full duration.
//
// rt.Progress is set to the max in-flight point fraction so the existing
// top-of-screen "Defending" bar shows the most-progressed point; per-point
// progress travels in the snapshot. rt.Capturing is set if any point advanced.
func evaluateClaimCapture(s *GameState, rt *zoneRuntime, dt float64) {
	if isHumanOwner(rt.Owner) {
		return // already claimed (sticky)
	}
	cfg, ok := rt.captureCfg.(claimCaptureConfig)
	if !ok || cfg.DefendSeconds <= 0 {
		return
	}
	rt.Progress = 0 // recomputed below as the max in-flight point fraction (0..1)
	points := claimPointCells(rt)
	allCaptured := true
	for i, p := range points {
		if i >= len(rt.claimPoints) {
			break // defensive: should not happen (sized at install)
		}
		ps := &rt.claimPoints[i]
		if ps.Captured {
			continue
		}
		tower := s.claimTowerAtPointLocked(p, cfg.TowerType)
		if tower == nil {
			ps.Progress = 0 // no tower → restart this point's defend timer
			allCaptured = false
			continue
		}
		rt.Capturing = true // this point's defend timer is advancing this tick
		ps.Progress += dt
		if frac := ps.Progress / cfg.DefendSeconds; frac > rt.Progress {
			rt.Progress = frac
		}
		if ps.Progress >= cfg.DefendSeconds {
			ps.Captured = true
			ps.Progress = 0
		} else {
			allCaptured = false
		}
	}
	if rt.Progress > 1 {
		rt.Progress = 1
	}
	if allCaptured {
		rt.Owner = protocol.ZoneCaptureTeamOwner
		rt.Progress = 0
		rt.Capturing = false
	}
}
```

Note: `tickZonesLocked` resets `Contested` and `Capturing` each tick but NOT `Progress`. The presence handler keeps `Progress` as a raw accumulator; this claim handler now recomputes `rt.Progress` as a normalised 0..1 max-point fraction every tick, which is why the `rt.Progress = 0` reset sits at the top of the function (shown in the code above).

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd "c:\Personal Dev\webrts\server" && go test ./internal/game/ -run TestClaimMultiPoint_AllPointsRequiredAndSticky -v`
Expected: PASS.

- [ ] **Step 5: Run the full claim + capturing-flag suite**

Run: `cd "c:\Personal Dev\webrts\server" && go test ./internal/game/ -run 'Claim|Capturing|EnemySpawnTrigger' -v`
Expected: PASS. The existing `TestClaimCapture_BuildDefendAndReset` and `TestClaimCapture_CapturingFlag` run through the anchor fallback (single point) and must remain green. If `TestClaimCapture_BuildDefendAndReset` asserts `rt.Progress` raw seconds anywhere it will need adjusting — it checks `Progress != 0` after a tower is destroyed, which still holds (a missing tower resets the single point's `ps.Progress` to 0, and `rt.Progress` recomputes to 0).

- [ ] **Step 6: Commit**

```bash
git add server/internal/game/zone_handlers.go server/internal/game/zone_runtime_test.go
git commit -m "feat(zones): capture each claim point independently, flip on all-held"
```

---

## Task 6: Snapshot — emit per-point progress

**Files:**
- Modify: `server/internal/game/zone_runtime.go` (`zoneSnapshotsLocked` ~258-274)
- Test: `server/internal/game/zone_runtime_test.go`

- [ ] **Step 1: Write the failing test**

Add to `zone_runtime_test.go`:

```go
func TestZoneSnapshot_CarriesClaimPoints(t *testing.T) {
	seed := presenceZone("seed", rectCells(0, 0, 4, 4), [2]int{2, 2}, "p1")
	claim := multiPointClaimZone("claim", [][2]int{{6, 6}, {10, 6}}, rectCells(5, 5, 14, 9), "seed")
	s := newZoneTestState([]protocol.Zone{seed, claim})

	// Defend point 1 to completion; start point 2.
	s.placeClaimTower("p1", 6, 6)
	s.placeClaimTower("p1", 10, 6)
	for i := 0; i < 7; i++ { // point 1 captured (>=3s), point 2 mid-defend
		s.tickZonesLocked(0.5)
	}
	var snap *protocol.ZoneSnapshot
	for i, sn := range s.zoneSnapshotsLocked() {
		if sn.ID == "claim" {
			snap = &s.zoneSnapshotsLocked()[i]
		}
	}
	if snap == nil || len(snap.ClaimPoints) != 2 {
		t.Fatalf("claim snapshot should carry 2 per-point entries, got %+v", snap)
	}
	if !snap.ClaimPoints[0].Captured {
		t.Fatal("point 1 should report captured in the snapshot")
	}
	if snap.ClaimPoints[1].Captured {
		t.Fatal("point 2 should not yet be captured")
	}
	if snap.ClaimPoints[1].Progress <= 0 || snap.ClaimPoints[1].Progress >= 1 {
		t.Fatalf("point 2 should report mid-defend fraction, got %v", snap.ClaimPoints[1].Progress)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd "c:\Personal Dev\webrts\server" && go test ./internal/game/ -run TestZoneSnapshot_CarriesClaimPoints -v`
Expected: FAIL — `snap.ClaimPoints` is always empty (not emitted yet).

- [ ] **Step 3: Emit per-point snapshot in `zoneSnapshotsLocked`**

In `zone_runtime.go`, add a helper above `zoneSnapshotsLocked` and populate the new field. Helper:

```go
// claimPointSnapshotsLocked projects a claim zone's per-point control state for
// the snapshot, in authored order. Returns nil for non-claim zones (no point
// state). Progress is a 0..1 fraction of the shared defendSeconds.
func claimPointSnapshotsLocked(rt *zoneRuntime) []protocol.ZoneClaimPointSnapshot {
	if len(rt.claimPoints) == 0 {
		return nil
	}
	cfg, ok := rt.captureCfg.(claimCaptureConfig)
	if !ok || cfg.DefendSeconds <= 0 {
		return nil
	}
	out := make([]protocol.ZoneClaimPointSnapshot, len(rt.claimPoints))
	for i := range rt.claimPoints {
		frac := rt.claimPoints[i].Progress / cfg.DefendSeconds
		if frac < 0 {
			frac = 0
		}
		if frac > 1 {
			frac = 1
		}
		// A captured point reads as fully filled regardless of its reset timer.
		if rt.claimPoints[i].Captured {
			frac = 1
		}
		out[i] = protocol.ZoneClaimPointSnapshot{Progress: frac, Captured: rt.claimPoints[i].Captured}
	}
	return out
}
```

Then in `zoneSnapshotsLocked`, add the field to the appended struct:

```go
		out = append(out, protocol.ZoneSnapshot{
			ID:          rt.Def.ID,
			Owner:       rt.Owner,
			Contested:   rt.Contested,
			Progress:    zoneProgressFraction(rt),
			OwnerColor:  s.zoneOwnerColorLocked(rt.Owner),
			ClaimPoints: claimPointSnapshotsLocked(rt),
		})
```

Note: `zoneProgressFraction` currently switches on `claimCaptureConfig` and reads `rt.Progress / cfg.DefendSeconds`. Since the evaluator now sets `rt.Progress` to an already-normalised 0..1 fraction (not raw seconds) for claim zones, dividing again by `DefendSeconds` would be wrong. Fix `zoneProgressFraction` to treat the claim case as already-normalised: in its `switch`, change the `claimCaptureConfig` branch to `return clamp01(rt.Progress)` directly rather than dividing. Concretely, replace the function:

```go
func zoneProgressFraction(rt *zoneRuntime) float64 {
	switch cfg := rt.captureCfg.(type) {
	case presenceCaptureConfig:
		if cfg.CaptureSeconds <= 0 {
			return 0
		}
		return clamp01(rt.Progress / cfg.CaptureSeconds)
	case claimCaptureConfig:
		// Claim sets rt.Progress to an already-normalised max-point fraction.
		return clamp01(rt.Progress)
	}
	return 0
}

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd "c:\Personal Dev\webrts\server" && go test ./internal/game/ -run TestZoneSnapshot_CarriesClaimPoints -v`
Expected: PASS.

- [ ] **Step 5: Run the whole game package + determinism check**

Run: `cd "c:\Personal Dev\webrts\server" && go test ./internal/game/`
Expected: PASS (all tests, including `TestZoneCapture_DeterministicReplay` and the existing presence/claim suites).

- [ ] **Step 6: Commit**

```bash
git add server/internal/game/zone_runtime.go server/internal/game/zone_runtime_test.go
git commit -m "feat(zones): emit per-point claim progress in zone snapshot"
```

---

## Task 7: Editor — "Place Capture Point" sub-mode

**Files:**
- Modify: `client/src/game-portal/src/components/MapEditorPanel.vue` (sub-mode type ~1522; template actions ~635-667; click handlers ~3063, 3119-3138; add helpers near `addCaptureCellToSelectedZone` ~2030)

- [ ] **Step 1: Extend the sub-mode union type**

At `MapEditorPanel.vue` ~line 1522, add `'claimPoint'`:

```ts
const zoneSubMode = ref<'idle' | 'place' | 'draw' | 'captureDraw' | 'move' | 'claimPoint'>('idle')
```

- [ ] **Step 2: Add add/remove helpers**

Near `addCaptureCellToSelectedZone` (~line 2030), add:

```ts
/**
 * Add (cx, cy) as a claim capture-point slot top-left on the selected zone.
 * The cell must be inside the zone's cells. Deduplicates. Each point becomes a
 * 2x2 tower slot the team must build + defend.
 */
function addClaimPointToSelectedZone(cx: number, cy: number) {
  const id = selectedZoneId.value
  if (!id) return
  const zones = model.value.zones ?? []
  const zone = zones.find((z) => z.id === id)
  if (!zone) return
  const inZone = zone.cells.some(([x, y]) => x === cx && y === cy)
  if (!inZone) return
  const existing = zone.claimPoints ?? []
  if (existing.some(([x, y]) => x === cx && y === cy)) return // dedup
  model.value = {
    ...model.value,
    zones: zones.map((z) =>
      z.id === id
        ? { ...z, claimPoints: [...(z.claimPoints ?? []), [cx, cy] as [number, number]] }
        : z,
    ),
  }
}

/** Remove the claim capture-point at (cx, cy) from the selected zone. */
function removeClaimPointFromSelectedZone(cx: number, cy: number) {
  const id = selectedZoneId.value
  if (!id) return
  const zones = (model.value.zones ?? []).map((z) =>
    z.id === id
      ? { ...z, claimPoints: (z.claimPoints ?? []).filter(([x, y]) => !(x === cx && y === cy)) }
      : z,
  )
  model.value = { ...model.value, zones }
}
```

- [ ] **Step 3: Add the editor button + hint (claim zones only)**

In the `zone-brush-config__actions` block (~line 635-657), add a button after the `captureDraw` button (~line 647), shown only for claim zones:

```html
                <button
                  v-if="!selectedZone.lockedSpawnLabel && selectedZone.capture.type === 'claim'"
                  type="button"
                  :class="{ 'zone-brush-config__action--active': zoneSubMode === 'claimPoint' }"
                  @click="zoneSubMode = zoneSubMode === 'claimPoint' ? 'idle' : 'claimPoint'"
                >Place Capture Point</button>
```

And a hint after the `move` hint (~line 665-667):

```html
              <div v-if="zoneSubMode === 'claimPoint'" class="zone-brush-config__hint">
                Click inside the zone to add a 2&#xD7;2 capture point · Right-click removes · all points must be captured · Esc exits
              </div>
```

- [ ] **Step 4: Wire the left-click handler**

In the pointer-down handler, after the `captureDraw` block (~line 3138), add a `claimPoint` block:

```ts
  // Claim capture-point placement: left-click adds a 2x2 slot inside the zone.
  if (zoneSubMode.value === 'claimPoint' && selectedZoneId.value && !isSpaceHeld) {
    const cell = getGridCellAtScreen(screen.x, screen.y)
    if (cell) addClaimPointToSelectedZone(cell.x, cell.y)
    return
  }
```

- [ ] **Step 5: Wire the right-click remove handler**

Extend the right-click block (~line 3064) to also handle `claimPoint`. Replace the condition + body:

```ts
  // Right-click in Zone draw / captureDraw / claimPoint mode: remove from the active set.
  if (
    event.button === 2 &&
    (zoneSubMode.value === 'draw' || zoneSubMode.value === 'captureDraw' || zoneSubMode.value === 'claimPoint') &&
    selectedZoneId.value
  ) {
    event.preventDefault()
    const cell = getGridCellAtScreen(screen.x, screen.y)
    if (cell) {
      if (zoneSubMode.value === 'draw') {
        removeCellFromSelectedZone(cell.x, cell.y)
      } else if (zoneSubMode.value === 'captureDraw') {
        removeCaptureCellFromSelectedZone(cell.x, cell.y)
      } else {
        removeClaimPointFromSelectedZone(cell.x, cell.y)
      }
    }
    return
  }
```

- [ ] **Step 6: Clear claimPoints when leaving claim type**

In `onZoneCaptureTypeChange` (~line 1930), mirror the `captureCells` cleanup so switching away from claim drops orphan points. After the `if (type !== 'presence') delete next.captureCells` line:

```ts
    if (type !== 'claim') delete next.claimPoints
```

- [ ] **Step 7: Typecheck the client**

Run: `cd "c:\Personal Dev\webrts\client\src\game-portal" && npx vue-tsc -b`
Expected: no errors.

- [ ] **Step 8: Commit**

```bash
git add client/src/game-portal/src/components/MapEditorPanel.vue
git commit -m "feat(editor): place multiple claim capture points per zone"
```

---

## Task 8: Renderer — draw all claim slots + captured state

**Files:**
- Modify: `client/src/game-portal/src/game/rendering/CanvasRenderer.ts` (claim slot block at lines 1878-1893; capture bar `drawCaptureProgressBar` at lines 480-525)

The overlay loop in `drawZoneOverlay` (line 1789) already has the per-zone snapshot in scope as the local `const snap = this.state.zoneSnapshotsById.get(zone.id)` (line 1790). Reuse it — no new lookup map is needed.

- [ ] **Step 1: Loop the claim-slot highlight over all points**

Replace the claim block at lines 1878-1893. Draw a 2×2 slot for every point in `zone.claimPoints` (or `[zone.anchor]` fallback), tinting captured points (read from `snap.claimPoints`) green vs. the cyan build pulse for outstanding ones:

```ts
      // Claim mechanic: highlight each 2x2 capture-point slot so the player
      // knows where to build towers. Captured points render green; outstanding
      // points pulse cyan. Whole block hidden once the zone is team-owned.
      if (zone.capture?.type === 'claim' && !isAlly) {
        const points: [number, number][] =
          zone.claimPoints && zone.claimPoints.length > 0
            ? zone.claimPoints
            : [[zone.anchor.x, zone.anchor.y]]
        const slot = 2 * cellSize
        const pulse = 0.6 + 0.4 * Math.sin(this.renderTime / 350)
        points.forEach((p, i) => {
          const sx = p[0] * cellSize
          const sy = p[1] * cellSize
          const captured = snap.claimPoints?.[i]?.captured ?? false
          if (captured) {
            ctx.fillStyle = 'rgba(74,222,128,0.20)' // green: point held
            ctx.fillRect(sx, sy, slot, slot)
            ctx.strokeStyle = 'rgba(74,222,128,0.9)'
            ctx.lineWidth = 2.5 / this.camera.zoom
            ctx.setLineDash([])
            ctx.strokeRect(sx, sy, slot, slot)
          } else {
            ctx.fillStyle = 'rgba(34,211,238,0.16)' // cyan build slot
            ctx.fillRect(sx, sy, slot, slot)
            ctx.strokeStyle = `rgba(34,211,238,${pulse.toFixed(2)})`
            ctx.lineWidth = 2.5 / this.camera.zoom
            ctx.setLineDash([6 / this.camera.zoom, 4 / this.camera.zoom])
            ctx.strokeRect(sx, sy, slot, slot)
            ctx.setLineDash([])
          }
        })
      }
```

`snap` is guaranteed non-null here — the loop `continue`s on `if (!snap)` at line 1791.

- [ ] **Step 2: Capture the snapshot for the bar's best zone**

In `drawCaptureProgressBar`, the loop (lines 487-496) keeps `bestZone` but discards its `snap`. Add a parallel `bestSnap` so the label can read per-point state. Change the loop's local declarations (line 484-486) and the assignment inside the `if` (lines 492-494):

```ts
    let bestZone: Zone | null = null
    let bestSnap: ZoneSnapshot | null = null
    let bestProgress = 0
    let bestContested = false
    for (const zone of zones) {
      const snap = this.state.zoneSnapshotsById.get(zone.id)
      if (!snap || snap.progress === undefined) continue
      const p = snap.progress
      if (p > 0 && p < 1 && p > bestProgress) {
        bestProgress = p
        bestZone = zone
        bestSnap = snap
        bestContested = snap.contested === true
      }
    }
    if (!bestZone) return
```

Ensure `ZoneSnapshot` is imported in this file — it shares the import line with `Zone` from `../network/protocol`. If `ZoneSnapshot` is not already imported, add it to that existing import statement (search the file head for `import type { ... Zone ... }`).

- [ ] **Step 3: Append the point count to the bar label**

Replace the `verb` / `label` lines (504-505):

```ts
    const verb = bestZone.capture?.type === 'claim' ? 'Defending' : 'Capturing'
    let suffix = ''
    if (bestZone.capture?.type === 'claim') {
      const pts = bestSnap?.claimPoints
      if (pts && pts.length > 1) {
        const held = pts.filter((pt) => pt.captured).length
        suffix = ` (${held}/${pts.length} points)`
      }
    }
    const label = `${bestZone.name || bestZone.id} — ${verb}${suffix}`
```

The single-point claim and presence labels are unchanged (`suffix` stays empty).

- [ ] **Step 4: Typecheck the client**

Run: `cd "c:\Personal Dev\webrts\client\src\game-portal" && npx vue-tsc -b`
Expected: no errors.

- [ ] **Step 5: Build the client to confirm the bundle compiles**

Run: `cd "c:\Personal Dev\webrts\client\src\game-portal" && npm run build`
Expected: build succeeds.

- [ ] **Step 6: Commit**

```bash
git add client/src/game-portal/src/game/rendering/CanvasRenderer.ts
git commit -m "feat(render): draw every claim capture point with captured state"
```

---

## Task 9: End-to-end verification

**Files:** none (verification only)

- [ ] **Step 1: Full backend test run**

Run: `cd "c:\Personal Dev\webrts\server" && go test ./...`
Expected: PASS across all packages.

- [ ] **Step 2: Full client typecheck + build**

Run: `cd "c:\Personal Dev\webrts\client\src\game-portal" && npm run build`
Expected: success.

- [ ] **Step 3: Manual smoke (optional, recommended)**

Launch the app, open the Map Editor, add a claim zone, draw its cells, click "Place Capture Point" twice to drop two slots, set Defend Duration, save, and play the map. Confirm: both slots render; building + defending one slot fills it green but does not capture the zone; defending the second flips the zone to the team color.

- [ ] **Step 4: Final commit (if any verification fixups were needed)**

```bash
git add -A
git commit -m "test(zones): end-to-end verification fixups for multi-point claim"
```

---

## Notes for the implementer

- **The `claimTowerOnSlotLocked` → `claimTowerAtPointLocked` rename (Task 4)** removes the old function. Grep the package for `claimTowerOnSlotLocked` after the rename and update any straggler call sites (there should be none outside `zone_handlers.go` after the evaluator rewrite).
- **`rt.Progress` semantics changed** from "raw accumulator seconds" (old claim) to "max in-flight point fraction 0..1" (new claim). Task 6 Step 3 fixes `zoneProgressFraction` for this. Presence zones are unaffected (still raw seconds ÷ captureSeconds).
- **Determinism:** the evaluator iterates `claimPointCells` (authored slice order) and `claimZoneTowerLocked` returns the first standing tower in authored order — no map iteration, no RNG, no wall-clock. The determinism test in Task 6 Step 5 guards this.
- **No catalog migration:** `claimPoints` is `omitempty`; existing maps serialize and load identically.
