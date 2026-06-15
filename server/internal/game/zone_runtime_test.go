package game

import (
	"encoding/json"
	"testing"

	"webrts/server/pkg/protocol"
)

// --- test harness -----------------------------------------------------------

func newZoneTestState(zones []protocol.Zone) *GameState {
	s := &GameState{Players: map[string]*Player{}}
	// Two real players on the same team (co-op single-team posture).
	s.Players["p1"] = &Player{ID: "p1", TeamID: 1, Metrics: NewMatchMetrics()}
	s.Players["p2"] = &Player{ID: "p2", TeamID: 1, Metrics: NewMatchMetrics()}
	s.MapConfig = protocol.MapConfig{CellSize: 64, GridCols: 64, GridRows: 64, Zones: normalizeZones(zones)}
	s.installZonesLocked()
	return s
}

func rectCells(x0, y0, x1, y1 int) [][2]int {
	var out [][2]int
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			out = append(out, [2]int{x, y})
		}
	}
	return out
}

func (s *GameState) addZoneTestUnit(owner string, cx, cy int) *Unit {
	s.nextUnitID++
	u := &Unit{
		ID:      s.nextUnitID,
		OwnerID: owner,
		X:       (float64(cx) + 0.5) * s.MapConfig.CellSize,
		Y:       (float64(cy) + 0.5) * s.MapConfig.CellSize,
		HP:      100,
		Visible: true,
	}
	s.Units = append(s.Units, u)
	return u
}

func presenceZone(id string, cells [][2]int, anchor [2]int, owner string, adjacent ...string) protocol.Zone {
	return protocol.Zone{
		ID:            id,
		Anchor:        protocol.GridCoord{X: anchor[0], Y: anchor[1]},
		Cells:         cells,
		Capture:       protocol.ZoneCapture{Type: "presence", Config: json.RawMessage(`{"captureSeconds":2}`)},
		StartingOwner: owner,
		Adjacent:      adjacent,
	}
}

func zoneOwner(s *GameState, id string) string {
	rt := s.zoneRuntimeByIDLocked(id)
	if rt == nil {
		return ""
	}
	return rt.Owner
}

// --- install ----------------------------------------------------------------

func TestInstallZones_OwnerFromStartingOwner(t *testing.T) {
	s := newZoneTestState([]protocol.Zone{
		presenceZone("seed", rectCells(0, 0, 4, 4), [2]int{2, 2}, "p1"),
		presenceZone("north", rectCells(0, 5, 4, 9), [2]int{2, 7}, "", "seed"),
	})
	if got := zoneOwner(s, "seed"); got != "p1" {
		t.Fatalf("seed owner = %q; want p1", got)
	}
	if got := zoneOwner(s, "north"); got != protocol.ZoneCaptureNeutralOwner {
		t.Fatalf("north owner = %q; want neutral", got)
	}
	if id, ok := s.zoneOwnerForCellLocked(gridPoint{X: 2, Y: 7}); !ok || id != "north" {
		t.Fatalf("cell (2,7) -> (%q,%v); want north,true", id, ok)
	}
}

// --- presence ---------------------------------------------------------------

func TestPresenceCapture_SoleTeamCaptures(t *testing.T) {
	s := newZoneTestState([]protocol.Zone{
		presenceZone("seed", rectCells(0, 0, 4, 4), [2]int{2, 2}, "p1"),
		presenceZone("north", rectCells(0, 5, 4, 9), [2]int{2, 7}, "neutral", "seed"),
	})
	s.addZoneTestUnit("p1", 2, 7) // inside north

	// captureSeconds = 2; dt 0.5 → flips on the 4th tick (progress reaches 2.0).
	for i := 0; i < 3; i++ {
		s.tickZonesLocked(0.5)
		if zoneOwner(s, "north") != "neutral" {
			t.Fatalf("north captured too early on tick %d", i+1)
		}
	}
	s.tickZonesLocked(0.5)
	if got := zoneOwner(s, "north"); got != protocol.ZoneCaptureTeamOwner {
		t.Fatalf("north owner after capture = %q; want team", got)
	}
}

func TestPresenceCapture_ContestedFreezes(t *testing.T) {
	s := newZoneTestState([]protocol.Zone{
		presenceZone("seed", rectCells(0, 0, 4, 4), [2]int{2, 2}, "p1"),
		presenceZone("north", rectCells(0, 5, 4, 9), [2]int{2, 7}, "neutral", "seed"),
	})
	s.addZoneTestUnit("p1", 2, 7)
	s.addZoneTestUnit(enemyPlayerID, 3, 7) // contesting enemy inside

	for i := 0; i < 10; i++ {
		s.tickZonesLocked(0.5)
	}
	rt := s.zoneRuntimeByIDLocked("north")
	if rt.Owner != "neutral" {
		t.Fatalf("contested north should not be captured, owner = %q", rt.Owner)
	}
	if !rt.Contested {
		t.Fatal("north should be flagged contested")
	}
	if rt.Progress != 0 {
		t.Fatalf("contested progress should be frozen at 0, got %v", rt.Progress)
	}
}

func TestPresenceCapture_LockedWithoutAdjacency(t *testing.T) {
	// "north" is NOT adjacent to a team-owned zone (its only neighbour, "far",
	// is neutral), so a p1 unit standing in it must not make progress.
	s := newZoneTestState([]protocol.Zone{
		presenceZone("far", rectCells(0, 0, 4, 4), [2]int{2, 2}, "neutral"),
		presenceZone("north", rectCells(0, 5, 4, 9), [2]int{2, 7}, "neutral", "far"),
	})
	s.addZoneTestUnit("p1", 2, 7)
	for i := 0; i < 10; i++ {
		s.tickZonesLocked(0.5)
	}
	if got := zoneOwner(s, "north"); got != "neutral" {
		t.Fatalf("locked north should stay neutral, got %q", got)
	}
}

func TestPresenceCapture_UnlocksNeighbourAfterCapture(t *testing.T) {
	s := newZoneTestState([]protocol.Zone{
		presenceZone("seed", rectCells(0, 0, 4, 4), [2]int{2, 2}, "p1"),
		presenceZone("mid", rectCells(0, 5, 4, 9), [2]int{2, 7}, "neutral", "seed"),
		presenceZone("far", rectCells(0, 10, 4, 14), [2]int{2, 12}, "neutral", "mid"),
	})
	s.addZoneTestUnit("p1", 2, 7)  // capturing mid
	s.addZoneTestUnit("p1", 2, 12) // sitting in far (locked until mid is ours)

	// Far must stay neutral until mid is captured.
	for i := 0; i < 4; i++ {
		s.tickZonesLocked(0.5)
	}
	if zoneOwner(s, "mid") != protocol.ZoneCaptureTeamOwner {
		t.Fatalf("mid should be captured by the team")
	}
	if zoneOwner(s, "far") != "neutral" {
		t.Fatalf("far should still be locked right after mid capture, got %q", zoneOwner(s, "far"))
	}
	// Now that mid is ours, far becomes capturable.
	for i := 0; i < 4; i++ {
		s.tickZonesLocked(0.5)
	}
	if got := zoneOwner(s, "far"); got != protocol.ZoneCaptureTeamOwner {
		t.Fatalf("far should capture once mid is held, got %q", got)
	}
}

// --- capture prerequisite links ---------------------------------------------

func TestZoneCapturable_LinkSemantics(t *testing.T) {
	owned := presenceZone("owned", rectCells(0, 0, 2, 2), [2]int{1, 1}, "p1")        // team-owned
	other := presenceZone("other", rectCells(0, 3, 2, 5), [2]int{1, 4}, "neutral")   // not owned
	noLink := presenceZone("noLink", rectCells(5, 0, 7, 2), [2]int{6, 1}, "neutral") // ungated
	anyZone := presenceZone("any", rectCells(5, 3, 7, 5), [2]int{6, 4}, "neutral", "owned", "other")
	allZone := presenceZone("all", rectCells(5, 6, 7, 8), [2]int{6, 7}, "neutral", "owned", "other")
	allZone.RequireAllLinks = true
	s := newZoneTestState([]protocol.Zone{owned, other, noLink, anyZone, allZone})

	cap := func(id string) bool { return s.zoneCapturableByLocked(s.zoneRuntimeByIDLocked(id), "p1") }

	if !cap("noLink") {
		t.Fatal("a zone with no links must always be capturable (ungated)")
	}
	if !cap("any") {
		t.Fatal("any-link zone: owning one prerequisite should unlock it")
	}
	if cap("all") {
		t.Fatal("all-link zone: 'other' is not owned, so it must stay locked")
	}
	// Own the second prerequisite too → the all-link zone unlocks.
	s.zoneRuntimeByIDLocked("other").Owner = protocol.ZoneCaptureTeamOwner
	if !cap("all") {
		t.Fatal("all-link zone: with both prerequisites owned it should unlock")
	}
}

// --- presence capture sub-zone ----------------------------------------------

func TestPresenceCapture_RequiresCaptureSubZone(t *testing.T) {
	seed := presenceZone("seed", rectCells(0, 0, 4, 4), [2]int{2, 2}, "p1")
	north := presenceZone("north", rectCells(0, 5, 4, 9), [2]int{2, 7}, "neutral", "seed")
	north.CaptureCells = [][2]int{{2, 7}} // capture sub-zone is a single cell
	s := newZoneTestState([]protocol.Zone{seed, north})

	// A unit inside the zone but OUTSIDE the capture sub-zone makes no progress.
	u := s.addZoneTestUnit("p1", 0, 5)
	for i := 0; i < 10; i++ {
		s.tickZonesLocked(0.5)
	}
	if zoneOwner(s, "north") != protocol.ZoneCaptureNeutralOwner {
		t.Fatalf("should not capture from outside the capture sub-zone, got %q", zoneOwner(s, "north"))
	}

	// Move the unit into the capture sub-zone — now it captures.
	u.X = (2 + 0.5) * s.MapConfig.CellSize
	u.Y = (7 + 0.5) * s.MapConfig.CellSize
	for i := 0; i < 4; i++ {
		s.tickZonesLocked(0.5)
	}
	if zoneOwner(s, "north") != protocol.ZoneCaptureTeamOwner {
		t.Fatalf("should capture from inside the capture sub-zone, got %q", zoneOwner(s, "north"))
	}
}

func TestCaptureZoneOccupiedByHuman(t *testing.T) {
	north := presenceZone("north", rectCells(0, 5, 4, 9), [2]int{2, 7}, "neutral")
	north.CaptureCells = [][2]int{{2, 7}}

	s := newZoneTestState([]protocol.Zone{north})
	if s.captureZoneOccupiedByHumanLocked("north") {
		t.Fatal("empty zone should not be occupied")
	}
	// Human in the zone body but outside the capture sub-zone → not occupied.
	s.addZoneTestUnit("p1", 0, 5)
	if s.captureZoneOccupiedByHumanLocked("north") {
		t.Fatal("human outside the capture sub-zone should not count as occupied")
	}
	// Human inside the capture sub-zone → occupied.
	s.addZoneTestUnit("p1", 2, 7)
	if !s.captureZoneOccupiedByHumanLocked("north") {
		t.Fatal("human in the capture sub-zone should count as occupied")
	}

	// An enemy in the capture sub-zone is not human occupancy.
	s2 := newZoneTestState([]protocol.Zone{north})
	s2.addZoneTestUnit(enemyPlayerID, 2, 7)
	if s2.captureZoneOccupiedByHumanLocked("north") {
		t.Fatal("an enemy unit should not count as human occupancy")
	}
}

// --- locked (home) zones ----------------------------------------------------

func TestLockedZone_TeamOwnedAndNotCapturable(t *testing.T) {
	home := presenceZone("home", rectCells(0, 0, 4, 4), [2]int{2, 2}, "neutral")
	home.LockedSpawnLabel = "player1" // linked to player1's starting point
	adj := presenceZone("adj", rectCells(0, 5, 4, 9), [2]int{2, 7}, "neutral", "home")
	s := newZoneTestState([]protocol.Zone{home, adj})

	// Linked home starts team-owned even though StartingOwner was neutral.
	if got := zoneOwner(s, "home"); got != protocol.ZoneCaptureTeamOwner {
		t.Fatalf("locked home should start team-owned, got %q", got)
	}

	// An enemy sitting in the home zone never captures it or contests it.
	s.addZoneTestUnit(enemyPlayerID, 2, 2)
	for i := 0; i < 20; i++ {
		s.tickZonesLocked(0.5)
	}
	if got := zoneOwner(s, "home"); got != protocol.ZoneCaptureTeamOwner {
		t.Fatalf("locked home must stay team-owned, got %q", got)
	}
	if s.zoneRuntimeByIDLocked("home").Contested {
		t.Fatal("locked home should never be contested")
	}

	// The locked home seeds the frontier — an adjacent zone is capturable.
	s.addZoneTestUnit("p1", 2, 7)
	for i := 0; i < 4; i++ {
		s.tickZonesLocked(0.5)
	}
	if got := zoneOwner(s, "adj"); got != protocol.ZoneCaptureTeamOwner {
		t.Fatalf("adjacent zone should capture using the locked home as frontier, got %q", got)
	}
}

// --- clear ------------------------------------------------------------------

func clearZone(id string, cells [][2]int, anchor [2]int, owner string, adjacent ...string) protocol.Zone {
	return protocol.Zone{
		ID:            id,
		Anchor:        protocol.GridCoord{X: anchor[0], Y: anchor[1]},
		Cells:         cells,
		Capture:       protocol.ZoneCapture{Type: "clear"},
		StartingOwner: owner,
		Adjacent:      adjacent,
	}
}

func TestClearCapture_FlipsWhenHostilesGone(t *testing.T) {
	s := newZoneTestState([]protocol.Zone{
		presenceZone("seed", rectCells(0, 0, 4, 4), [2]int{2, 2}, "p1"),
		clearZone("guarded", rectCells(0, 5, 4, 9), [2]int{2, 7}, "neutral", "seed"),
	})
	guard := s.addZoneTestUnit(neutralPlayerID, 2, 7)

	s.tickZonesLocked(0.1)
	if zoneOwner(s, "guarded") != "neutral" {
		t.Fatal("guarded zone should not capture while a hostile is inside")
	}
	// Guard dies / leaves.
	guard.HP = 0
	s.tickZonesLocked(0.1)
	if got := zoneOwner(s, "guarded"); got != protocol.ZoneCaptureTeamOwner {
		t.Fatalf("guarded zone should flip to the team once cleared, got %q", got)
	}
}

// --- control_point ----------------------------------------------------------

func TestControlPointCapture_FollowsAnchorStructure(t *testing.T) {
	p1 := "p1"
	s := newZoneTestState([]protocol.Zone{
		presenceZone("seed", rectCells(0, 0, 4, 4), [2]int{2, 2}, "p1"),
		{
			ID:            "point",
			Anchor:        protocol.GridCoord{X: 2, Y: 7},
			Cells:         rectCells(0, 5, 4, 9),
			Capture:       protocol.ZoneCapture{Type: "control_point"},
			StartingOwner: "neutral",
			Adjacent:      []string{"seed"},
		},
	})
	s.MapConfig.Buildings = []protocol.BuildingTile{{
		GridCoord: protocol.GridCoord{X: 2, Y: 7}, ID: "cp", BuildingType: "tower",
		Width: 1, Height: 1, Visible: true, OwnerID: &p1,
	}}

	s.tickZonesLocked(0.1)
	if got := zoneOwner(s, "point"); got != protocol.ZoneCaptureTeamOwner {
		t.Fatalf("control_point zone should flip to the team when a human owns the structure, got %q", got)
	}
}

// --- capture_zone objective -------------------------------------------------

func TestCaptureZoneObjective_CompletesAndSticky(t *testing.T) {
	s := newZoneTestState([]protocol.Zone{
		presenceZone("seed", rectCells(0, 0, 4, 4), [2]int{2, 2}, "p1"),
		presenceZone("north", rectCells(0, 5, 4, 9), [2]int{2, 7}, "neutral", "seed"),
	})
	installObjective(t, s, "take_north", "capture_zone", "team", true,
		captureZoneConfig{ZoneIDs: []string{"north"}})

	s.evaluateObjectivesLocked()
	if s.Objectives[0].TeamState.Completed {
		t.Fatal("objective should be incomplete before north is captured")
	}

	// Capture north.
	s.addZoneTestUnit("p1", 2, 7)
	for i := 0; i < 4; i++ {
		s.tickZonesLocked(0.5)
	}
	s.evaluateObjectivesLocked()
	if !s.Objectives[0].TeamState.Completed {
		t.Fatal("objective should complete once north is owned")
	}

	// Lose the zone — objective stays completed (sticky).
	s.zoneRuntimeByIDLocked("north").Owner = protocol.ZoneCaptureNeutralOwner
	s.evaluateObjectivesLocked()
	if !s.Objectives[0].TeamState.Completed {
		t.Fatal("capture_zone completion must be sticky after losing the zone")
	}
}

// --- build-gate helpers -----------------------------------------------------

func TestZoneBuildGate_AllowsOwnerDeniesNeutral(t *testing.T) {
	s := newZoneTestState([]protocol.Zone{
		presenceZone("owned", rectCells(0, 0, 4, 4), [2]int{2, 2}, "p1"),
		presenceZone("neutralZone", rectCells(0, 5, 4, 9), [2]int{2, 7}, "neutral", "owned"),
	})
	// Cell in p1's zone is buildable by p1 / its ally p2, not by an enemy.
	ownedID, _ := s.zoneOwnerForCellLocked(gridPoint{X: 2, Y: 2})
	ownedRT := s.zoneRuntimeByIDLocked(ownedID)
	if !s.zonesAlliedLocked(ownedRT.Owner, "p1") {
		t.Fatal("p1 should be allowed to build in its own zone")
	}
	if !s.zonesAlliedLocked(ownedRT.Owner, "p2") {
		t.Fatal("ally p2 should be allowed to build in the team's zone")
	}
	// Cell in the neutral zone is not buildable by anyone.
	nID, _ := s.zoneOwnerForCellLocked(gridPoint{X: 2, Y: 7})
	nRT := s.zoneRuntimeByIDLocked(nID)
	if s.zonesAlliedLocked(nRT.Owner, "p1") {
		t.Fatal("no one should build in a neutral zone")
	}
	// Cell in no zone is unrestricted (not owned by any zone).
	if _, ok := s.zoneOwnerForCellLocked(gridPoint{X: 40, Y: 40}); ok {
		t.Fatal("cell outside any zone should not be zone-owned")
	}
}

// --- enemy spawn trigger ----------------------------------------------------

func TestEnemySpawnTrigger_GatedByCaptureZoneOccupancy(t *testing.T) {
	cfg := protocol.MapConfig{
		ID: "trigger-test", CellSize: 64, GridCols: 32, GridRows: 32,
		Width: 32 * 64, Height: 32 * 64,
		Zones: []protocol.Zone{
			presenceZone("cap", rectCells(5, 5, 9, 9), [2]int{7, 7}, "neutral"),
		},
		Buildings: []protocol.BuildingTile{{
			GridCoord: protocol.GridCoord{X: 20, Y: 20}, ID: "es", BuildingType: "enemy-spawnpoint",
			Width: 1, Height: 1, Visible: true, Occupied: true,
			Metadata: map[string]interface{}{
				"triggerCaptureZoneId": "cap",
				"spawnDelaySeconds":    float64(0),
				"spawnIntervalSeconds": float64(1),
				"unitType":             "raider",
				"spawnCount":           float64(1),
			},
		}},
	}
	s := NewGameStateWithSeed(cfg, 7)
	s.mu.Lock()
	defer s.mu.Unlock()

	blocked := map[gridPoint]bool{}
	enemies := func() int {
		n := 0
		for _, u := range s.Units {
			if u.OwnerID == enemyPlayerID {
				n++
			}
		}
		return n
	}

	// Capture zone empty → the triggered spawnpoint stays dormant.
	for i := 0; i < 5; i++ {
		s.tickEnemySpawnpointsLocked(1.0, blocked)
	}
	if enemies() != 0 {
		t.Fatalf("spawnpoint should be dormant while capture zone is empty, got %d enemies", enemies())
	}

	// A human unit enters the capture zone → the spawnpoint activates.
	s.nextUnitID++
	s.Units = append(s.Units, &Unit{
		ID: s.nextUnitID, OwnerID: "p1",
		X: (7 + 0.5) * cfg.CellSize, Y: (7 + 0.5) * cfg.CellSize,
		HP: 100, Visible: true,
	})
	s.tickEnemySpawnpointsLocked(1.0, blocked)
	if enemies() < 1 {
		t.Fatalf("spawnpoint should spawn while a player occupies the capture zone, got %d enemies", enemies())
	}
}

// --- claim ------------------------------------------------------------------

func claimZone(id string, anchor [2]int, cells [][2]int, adjacent ...string) protocol.Zone {
	return protocol.Zone{
		ID:            id,
		Anchor:        protocol.GridCoord{X: anchor[0], Y: anchor[1]},
		Cells:         cells,
		Capture:       protocol.ZoneCapture{Type: "claim", Config: json.RawMessage(`{"defendSeconds":3,"towerType":"Tower"}`)},
		StartingOwner: "neutral",
		Adjacent:      adjacent,
	}
}

func (s *GameState) placeClaimTower(owner string, x, y int) *protocol.BuildingTile {
	o := owner
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
		GridCoord: protocol.GridCoord{X: x, Y: y}, ID: "twr", BuildingType: "Tower",
		Width: 1, Height: 2, Visible: true, OwnerID: &o,
		Metadata: map[string]interface{}{}, // not under construction = completed
	})
	return &s.MapConfig.Buildings[len(s.MapConfig.Buildings)-1]
}

func TestClaimCapture_BuildDefendAndReset(t *testing.T) {
	seed := presenceZone("seed", rectCells(0, 0, 4, 4), [2]int{2, 2}, "p1")
	claim := claimZone("claim", [2]int{6, 6}, rectCells(5, 5, 9, 9), "seed")
	s := newZoneTestState([]protocol.Zone{seed, claim})

	// No tower → no claim progress.
	for i := 0; i < 5; i++ {
		s.tickZonesLocked(0.5)
	}
	if zoneOwner(s, "claim") != protocol.ZoneCaptureNeutralOwner {
		t.Fatalf("claim should not capture without a tower, got %q", zoneOwner(s, "claim"))
	}

	// Build a completed team Tower on the slot, defend partway, then lose it.
	tower := s.placeClaimTower("p1", 6, 6)
	s.tickZonesLocked(0.5)
	s.tickZonesLocked(0.5) // progress = 1.0 of 3
	if zoneOwner(s, "claim") != protocol.ZoneCaptureNeutralOwner {
		t.Fatal("claim should not capture mid-defend")
	}
	tower.Visible = false // tower destroyed
	s.tickZonesLocked(0.5)
	if p := s.zoneRuntimeByIDLocked("claim").Progress; p != 0 {
		t.Fatalf("losing the tower should reset the defend timer, got %v", p)
	}

	// Rebuild and defend the full duration → capture to the team.
	tower.Visible = true
	for i := 0; i < 5; i++ {
		s.tickZonesLocked(0.5)
		if zoneOwner(s, "claim") != protocol.ZoneCaptureNeutralOwner {
			t.Fatalf("claimed too early at tick %d", i)
		}
	}
	s.tickZonesLocked(0.5) // 6th tick → progress reaches 3
	if got := zoneOwner(s, "claim"); got != protocol.ZoneCaptureTeamOwner {
		t.Fatalf("claim should capture to the team after defending, got %q", got)
	}
}

func TestClaimBuildGate_SlotExceptionAndType(t *testing.T) {
	seed := presenceZone("seed", rectCells(0, 0, 4, 4), [2]int{2, 2}, "p1")
	claim := claimZone("claim", [2]int{6, 6}, rectCells(5, 5, 9, 9), "seed")
	s := newZoneTestState([]protocol.Zone{seed, claim})
	rt := s.zoneRuntimeByIDLocked("claim")

	if !s.claimSlotBuildableLocked(rt, gridPoint{X: 6, Y: 6}, "Tower") {
		t.Fatal("the Tower should be buildable on the claim slot")
	}
	if s.claimSlotBuildableLocked(rt, gridPoint{X: 6, Y: 6}, "barracks") {
		t.Fatal("a non-tower building must not use the claim-slot exception")
	}
	if s.claimSlotBuildableLocked(rt, gridPoint{X: 9, Y: 9}, "Tower") {
		t.Fatal("a non-slot cell must not be buildable via the exception")
	}
	// Claim is standalone — the slot stays buildable with no adjacent foothold.
	s.zoneRuntimeByIDLocked("seed").Owner = protocol.ZoneCaptureNeutralOwner
	if !s.claimSlotBuildableLocked(rt, gridPoint{X: 6, Y: 6}, "Tower") {
		t.Fatal("the claim slot should be buildable without an adjacent foothold")
	}
}

// --- snapshot ---------------------------------------------------------------

func TestZoneSnapshots_CarryControlState(t *testing.T) {
	s := newZoneTestState([]protocol.Zone{
		presenceZone("seed", rectCells(0, 0, 4, 4), [2]int{2, 2}, "p1"),
	})
	snaps := s.zoneSnapshotsLocked()
	if len(snaps) != 1 {
		t.Fatalf("want 1 zone snapshot, got %d", len(snaps))
	}
	if snaps[0].ID != "seed" || snaps[0].Owner != "p1" {
		t.Fatalf("snapshot = %+v; want id=seed owner=p1", snaps[0])
	}
}

// --- determinism ------------------------------------------------------------

func TestZoneCapture_DeterministicReplay(t *testing.T) {
	run := func() (string, float64) {
		s := newZoneTestState([]protocol.Zone{
			presenceZone("seed", rectCells(0, 0, 4, 4), [2]int{2, 2}, "p1"),
			presenceZone("north", rectCells(0, 5, 4, 9), [2]int{2, 7}, "neutral", "seed"),
		})
		s.addZoneTestUnit("p1", 2, 7)
		for i := 0; i < 3; i++ {
			s.tickZonesLocked(0.5)
		}
		rt := s.zoneRuntimeByIDLocked("north")
		return rt.Owner, rt.Progress
	}
	o1, p1 := run()
	o2, p2 := run()
	if o1 != o2 || p1 != p2 {
		t.Fatalf("non-deterministic replay: (%q,%v) vs (%q,%v)", o1, p1, o2, p2)
	}
}
