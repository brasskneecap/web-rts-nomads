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

func TestPresenceCapture_CapturingFlagTransitions(t *testing.T) {
	s := newZoneTestState([]protocol.Zone{
		presenceZone("seed", rectCells(0, 0, 4, 4), [2]int{2, 2}, "p1"),
		presenceZone("north", rectCells(0, 5, 4, 9), [2]int{2, 7}, "neutral", "seed"),
	})
	rt := s.zoneRuntimeByIDLocked("north")

	// Empty zone → not capturing.
	s.tickZonesLocked(0.5)
	if rt.Capturing {
		t.Fatal("empty zone should not be flagged Capturing")
	}

	// A sole human in the (capturable) zone → capturing while progress advances.
	human := s.addZoneTestUnit("p1", 2, 7)
	s.tickZonesLocked(0.5)
	if !rt.Capturing {
		t.Fatal("zone should be Capturing while a sole team advances progress")
	}

	// A hostile joins → contested → progress frozen → not capturing.
	hostile := s.addZoneTestUnit(enemyPlayerID, 3, 7)
	s.tickZonesLocked(0.5)
	if rt.Capturing {
		t.Fatal("contested zone must not be flagged Capturing")
	}
	if !rt.Contested {
		t.Fatal("zone should be contested with a hostile present")
	}

	// Hostile leaves → capturing resumes.
	hostile.HP = 0
	s.tickZonesLocked(0.5)
	if !rt.Capturing {
		t.Fatal("zone should resume Capturing once uncontested")
	}

	// Human leaves → not capturing.
	human.HP = 0
	s.tickZonesLocked(0.5)
	if rt.Capturing {
		t.Fatal("abandoned zone should not be flagged Capturing")
	}
}

func TestPresenceCapture_NotCapturingOnceOwned(t *testing.T) {
	s := newZoneTestState([]protocol.Zone{
		presenceZone("seed", rectCells(0, 0, 4, 4), [2]int{2, 2}, "p1"),
		presenceZone("north", rectCells(0, 5, 4, 9), [2]int{2, 7}, "neutral", "seed"),
	})
	s.addZoneTestUnit("p1", 2, 7)
	rt := s.zoneRuntimeByIDLocked("north")

	// Capture it (captureSeconds=2, dt 0.5 → 4 ticks).
	for i := 0; i < 4; i++ {
		s.tickZonesLocked(0.5)
	}
	if rt.Owner != protocol.ZoneCaptureTeamOwner {
		t.Fatalf("north should be captured, owner = %q", rt.Owner)
	}
	// Now team-owned: the unit still stands inside, but there is nothing to
	// capture, so Capturing must be false.
	s.tickZonesLocked(0.5)
	if rt.Capturing {
		t.Fatal("an already team-owned zone must not be flagged Capturing")
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

func TestZoneCapturingLocked(t *testing.T) {
	s := newZoneTestState([]protocol.Zone{
		presenceZone("z", rectCells(0, 0, 2, 2), [2]int{1, 1}, "neutral"),
	})
	if s.zoneCapturingLocked("z") {
		t.Fatal("a zone with no advancing capture should not be capturing")
	}
	s.zoneRuntimeByIDLocked("z").Capturing = true
	if !s.zoneCapturingLocked("z") {
		t.Fatal("helper should reflect the runtime Capturing flag")
	}
	if s.zoneCapturingLocked("does-not-exist") {
		t.Fatal("an unknown zone must report not-capturing")
	}
}

func TestEnemySpawnTrigger_GatedByCaptureProgress(t *testing.T) {
	cfg := protocol.MapConfig{
		ID: "trigger-test", CellSize: 64, GridCols: 32, GridRows: 32,
		Width: 32 * 64, Height: 32 * 64,
		Zones: []protocol.Zone{
			// Ungated presence zone (captureSeconds=2) so a sole p1 unit advances it.
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
	// Tick zones first so the Capturing flag is fresh, then the spawnpoints.
	step := func() {
		s.tickZonesLocked(1.0)
		s.tickEnemySpawnpointsLocked(1.0, blocked)
	}

	// Nobody capturing → dormant.
	for i := 0; i < 5; i++ {
		step()
	}
	if enemies() != 0 {
		t.Fatalf("spawnpoint must stay dormant while the zone is not being captured, got %d", enemies())
	}

	// A human enters and starts capturing → spawnpoint activates.
	s.nextUnitID++
	s.Units = append(s.Units, &Unit{
		ID: s.nextUnitID, OwnerID: "p1",
		X: (7 + 0.5) * cfg.CellSize, Y: (7 + 0.5) * cfg.CellSize,
		HP: 100, Visible: true,
	})
	step()
	if enemies() < 1 {
		t.Fatalf("spawnpoint should spawn while the zone is being captured, got %d", enemies())
	}

	// Let the capture finish; the unit keeps standing in the (now team-owned)
	// zone, but there is nothing left to capture → the spawnpoint goes dormant.
	for i := 0; i < 5; i++ {
		step()
	}
	if got := s.zoneRuntimeByIDLocked("cap").Owner; got != protocol.ZoneCaptureTeamOwner {
		t.Fatalf("cap should be captured by now, owner = %q", got)
	}
	before := enemies()
	for i := 0; i < 5; i++ {
		step()
	}
	if enemies() != before {
		t.Fatalf("spawnpoint must be dormant after capture completes; spawned %d more", enemies()-before)
	}
}

func TestEnemySpawn_AllianceSelectsOwner(t *testing.T) {
	cfg := protocol.MapConfig{
		ID: "alliance-test", CellSize: 64, GridCols: 32, GridRows: 32,
		Width: 32 * 64, Height: 32 * 64,
		Buildings: []protocol.BuildingTile{
			// Default (no spawnAlliance) → enemy-aligned (backward compatible).
			{GridCoord: protocol.GridCoord{X: 5, Y: 5}, ID: "enemySpawn", BuildingType: "enemy-spawnpoint",
				Width: 1, Height: 1, Visible: true, Occupied: true,
				Metadata: map[string]interface{}{"gameStart": true, "unitType": "raider", "spawnCount": float64(1)}},
			// spawnAlliance=neutral → neutral-aligned.
			{GridCoord: protocol.GridCoord{X: 25, Y: 25}, ID: "neutralSpawn", BuildingType: "enemy-spawnpoint",
				Width: 1, Height: 1, Visible: true, Occupied: true,
				Metadata: map[string]interface{}{"gameStart": true, "unitType": "raider", "spawnCount": float64(1), "spawnAlliance": "neutral"}},
		},
	}
	s := NewGameStateWithSeed(cfg, 7)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tickEnemySpawnpointsLocked(0.05, map[gridPoint]bool{})

	count := func(owner string) int {
		n := 0
		for _, u := range s.Units {
			if u.OwnerID == owner {
				n++
			}
		}
		return n
	}
	if got := count(enemyPlayerID); got != 1 {
		t.Fatalf("default spawnpoint should spawn 1 enemy-aligned unit, got %d", got)
	}
	if got := count(neutralPlayerID); got != 1 {
		t.Fatalf("spawnAlliance=neutral should spawn 1 neutral-aligned unit, got %d", got)
	}
	// Neutral-aligned spawn units belong to no camp, so camp/wave despawn
	// (which only removes camp-tracked units) will not cull them.
	for _, u := range s.Units {
		if u.OwnerID == neutralPlayerID && u.NeutralCampID != "" {
			t.Fatalf("spawnpoint neutral unit must not be tied to a camp, got NeutralCampID=%q", u.NeutralCampID)
		}
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

func TestClaimCapture_CapturingFlag(t *testing.T) {
	seed := presenceZone("seed", rectCells(0, 0, 4, 4), [2]int{2, 2}, "p1")
	claim := claimZone("claim", [2]int{6, 6}, rectCells(5, 5, 9, 9), "seed")
	s := newZoneTestState([]protocol.Zone{seed, claim})
	rt := s.zoneRuntimeByIDLocked("claim")

	// No tower → defend timer can't advance → not capturing.
	s.tickZonesLocked(0.5)
	if rt.Capturing {
		t.Fatal("claim with no tower must not be flagged Capturing")
	}

	// Completed team tower on the slot → defend timer advances → capturing.
	tower := s.placeClaimTower("p1", 6, 6)
	s.tickZonesLocked(0.5)
	if !rt.Capturing {
		t.Fatal("claim should be Capturing while a defended tower stands")
	}

	// Tower destroyed → timer resets → not capturing.
	tower.Visible = false
	s.tickZonesLocked(0.5)
	if rt.Capturing {
		t.Fatal("claim must not be Capturing once the tower is gone")
	}

	// Defend to completion → captured → no longer capturing.
	tower.Visible = true
	for i := 0; i < 8; i++ {
		s.tickZonesLocked(0.5)
	}
	if rt.Owner != protocol.ZoneCaptureTeamOwner {
		t.Fatalf("claim should be captured, owner = %q", rt.Owner)
	}
	s.tickZonesLocked(0.5)
	if rt.Capturing {
		t.Fatal("an already-claimed zone must not be flagged Capturing")
	}
}

func TestNearestCapturingUnitPos(t *testing.T) {
	s := newZoneTestState([]protocol.Zone{
		presenceZone("cap", rectCells(5, 5, 9, 9), [2]int{7, 7}, "neutral"),
	})
	rt := s.zoneRuntimeByIDLocked("cap")

	if s.nearestCapturingUnitPosLocked(rt, 0, 0) != nil {
		t.Fatal("no capturer in the region → nil")
	}
	// Enemy / neutral units in the region are not "the capturing team".
	s.addZoneTestUnit(enemyPlayerID, 7, 7)
	s.addZoneTestUnit(neutralPlayerID, 8, 8)
	if s.nearestCapturingUnitPosLocked(rt, 0, 0) != nil {
		t.Fatal("only the capturing (human) team should count")
	}
	// A human unit inside the region is the capturer.
	capUnit := s.addZoneTestUnit("p1", 6, 6)
	pos := s.nearestCapturingUnitPosLocked(rt, 0, 0)
	if pos == nil || pos.X != capUnit.X || pos.Y != capUnit.Y {
		t.Fatalf("want capturer pos (%v,%v), got %v", capUnit.X, capUnit.Y, pos)
	}
	// A human unit OUTSIDE the capture region does not count.
	s2 := newZoneTestState([]protocol.Zone{
		presenceZone("cap", rectCells(5, 5, 9, 9), [2]int{7, 7}, "neutral"),
	})
	s2.addZoneTestUnit("p1", 20, 20) // far outside
	if s2.nearestCapturingUnitPosLocked(s2.zoneRuntimeByIDLocked("cap"), 0, 0) != nil {
		t.Fatal("a unit outside the capture region must not count as a capturer")
	}
}

func TestEnemySpawnTrigger_PresenceTargetsCapturer(t *testing.T) {
	townhallOwner := "p1"
	cfg := protocol.MapConfig{
		ID: "presence-trigger", CellSize: 64, GridCols: 40, GridRows: 40,
		Width: 40 * 64, Height: 40 * 64,
		Zones: []protocol.Zone{
			presenceZone("cap", rectCells(5, 5, 9, 9), [2]int{7, 7}, "neutral"),
		},
		Buildings: []protocol.BuildingTile{
			// A far-away townhall — the "wrong" target the defender must NOT pick.
			{GridCoord: protocol.GridCoord{X: 35, Y: 35}, ID: "townhall-1", BuildingType: "townhall",
				Width: 2, Height: 2, Visible: true, Occupied: true, OwnerID: &townhallOwner,
				Metadata: map[string]interface{}{"hp": 5000.0, "maxHp": 5000.0}},
			{GridCoord: protocol.GridCoord{X: 20, Y: 20}, ID: "es", BuildingType: "enemy-spawnpoint",
				Width: 1, Height: 1, Visible: true, Occupied: true,
				Metadata: map[string]interface{}{
					"triggerCaptureZoneId": "cap",
					"spawnDelaySeconds":    float64(0),
					"spawnIntervalSeconds": float64(1),
					"unitType":             "raider",
					"spawnCount":           float64(1),
				}},
		},
	}
	s := NewGameStateWithSeed(cfg, 7)
	s.mu.Lock()
	defer s.mu.Unlock()
	blocked := map[gridPoint]bool{}

	// A player unit captures the presence zone.
	s.nextUnitID++
	capUnit := &Unit{
		ID: s.nextUnitID, OwnerID: "p1",
		X: (7 + 0.5) * cfg.CellSize, Y: (7 + 0.5) * cfg.CellSize,
		HP: 100, Visible: true,
	}
	s.Units = append(s.Units, capUnit)

	s.tickZonesLocked(1.0) // sets Capturing
	s.tickEnemySpawnpointsLocked(1.0, blocked)

	var def *Unit
	for _, u := range s.Units {
		if u.OwnerID == enemyPlayerID {
			def = u
			break
		}
	}
	if def == nil {
		t.Fatal("presence capture trigger should spawn a defender")
	}
	// The defender heads for the capturer, not the far townhall.
	dCap := distanceSquared(def.TargetX, def.TargetY, capUnit.X, capUnit.Y)
	thX := (35.0 + 1) * cfg.CellSize
	thY := (35.0 + 1) * cfg.CellSize
	dTH := distanceSquared(def.TargetX, def.TargetY, thX, thY)
	if dCap >= dTH {
		t.Fatalf("defender should head for the capturer (d²=%.0f) not the townhall (d²=%.0f); dest=(%.0f,%.0f)",
			dCap, dTH, def.TargetX, def.TargetY)
	}
	if def.ObjectiveBuildingID != "" {
		t.Fatalf("a presence defender has no building objective; got %q", def.ObjectiveBuildingID)
	}
}

func TestClaimZoneTowerLocked(t *testing.T) {
	seed := presenceZone("seed", rectCells(0, 0, 4, 4), [2]int{2, 2}, "p1")
	claim := claimZone("claim", [2]int{6, 6}, rectCells(5, 5, 9, 9), "seed")
	s := newZoneTestState([]protocol.Zone{seed, claim})

	if got := s.claimZoneTowerLocked("claim"); got != nil {
		t.Fatalf("no tower built yet → want nil, got %v", got)
	}
	tower := s.placeClaimTower("p1", 6, 6)
	got := s.claimZoneTowerLocked("claim")
	if got == nil || got.ID != tower.ID {
		t.Fatalf("want the claim tower %q, got %v", tower.ID, got)
	}
	if s.claimZoneTowerLocked("seed") != nil {
		t.Fatal("a presence zone has no claim tower")
	}
	if s.claimZoneTowerLocked("missing") != nil {
		t.Fatal("unknown zone → nil")
	}
}

func TestEnemySpawnTrigger_TargetsClaimTower(t *testing.T) {
	towerOwner := "p1"
	cfg := protocol.MapConfig{
		ID: "claim-trigger", CellSize: 64, GridCols: 32, GridRows: 32,
		Width: 32 * 64, Height: 32 * 64,
		Zones: []protocol.Zone{
			claimZone("cap", [2]int{6, 6}, rectCells(5, 5, 9, 9)),
		},
		Buildings: []protocol.BuildingTile{
			// The team's claim tower on the zone slot — what defenders must rush.
			{GridCoord: protocol.GridCoord{X: 6, Y: 6}, ID: "claim-tower", BuildingType: "Tower",
				Width: 1, Height: 2, Visible: true, OwnerID: &towerOwner,
				Metadata: map[string]interface{}{"hp": 500.0, "maxHp": 500.0}},
			{GridCoord: protocol.GridCoord{X: 20, Y: 20}, ID: "es", BuildingType: "enemy-spawnpoint",
				Width: 1, Height: 1, Visible: true, Occupied: true,
				Metadata: map[string]interface{}{
					"triggerCaptureZoneId": "cap",
					"spawnDelaySeconds":    float64(0),
					"spawnIntervalSeconds": float64(1),
					"unitType":             "raider",
					"spawnCount":           float64(1),
				}},
		},
	}
	s := NewGameStateWithSeed(cfg, 7)
	s.mu.Lock()
	defer s.mu.Unlock()
	blocked := map[gridPoint]bool{}

	// Tower stands → claim is "being captured" → spawnpoint fires this tick.
	s.tickZonesLocked(1.0)
	s.tickEnemySpawnpointsLocked(1.0, blocked)

	var spawned *Unit
	for _, u := range s.Units {
		if u.OwnerID == enemyPlayerID {
			spawned = u
			break
		}
	}
	if spawned == nil {
		t.Fatal("capture-trigger spawnpoint should have spawned a defender")
	}
	if spawned.ObjectiveBuildingID != "claim-tower" {
		t.Fatalf("capture defender should target the claim tower, got ObjectiveBuildingID=%q", spawned.ObjectiveBuildingID)
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

// --- multi-point claim install -----------------------------------------------

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

// --- multi-point claim geometry ----------------------------------------------

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

// --- multi-point claim capture (per-point independent, sticky) --------------

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

func TestZoneSnapshot_CarriesClaimPoints(t *testing.T) {
	seed := presenceZone("seed", rectCells(0, 0, 4, 4), [2]int{2, 2}, "p1")
	claim := multiPointClaimZone("claim", [][2]int{{6, 6}, {10, 6}}, rectCells(5, 5, 14, 9), "seed")
	s := newZoneTestState([]protocol.Zone{seed, claim})

	// Defend point 1 to completion first (defendSeconds=3, dt 0.5 → 6 ticks = 3.0s).
	s.placeClaimTower("p1", 6, 6)
	for i := 0; i < 6; i++ {
		s.tickZonesLocked(0.5)
	}
	rt := s.zoneRuntimeByIDLocked("claim")
	if !rt.claimPoints[0].Captured {
		t.Fatal("setup: point 1 should be captured after 6 ticks (3.0s >= defendSeconds=3)")
	}
	// Now start defending point 2 — partway through (1 tick = 0.5s, still mid-defend).
	s.placeClaimTower("p1", 10, 6)
	s.tickZonesLocked(0.5)

	snaps := s.zoneSnapshotsLocked()
	var snap *protocol.ZoneSnapshot
	for i := range snaps {
		if snaps[i].ID == "claim" {
			snap = &snaps[i]
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
