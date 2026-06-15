package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestZoneDemoMap_FullCaptureLoop loads the authored zone-demo catalog map and
// drives the territorial loop end to end:
//   - the player starts owning the seed zone (build rights there from tick 0);
//   - the adjacent clear zone (no hostiles inside) captures immediately;
//   - the adjacent presence zone captures after its timer;
//   - the far zone stays LOCKED until its only neighbour (north) is held;
//   - build-gating opens on a zone exactly when the team captures it;
//   - a required capture_zone objective completes on capture.
func TestZoneDemoMap_FullCaptureLoop(t *testing.T) {
	cfg := GetMapConfigByID("zone-demo")
	if len(cfg.Zones) != 4 {
		t.Fatalf("zone-demo should author 4 zones, got %d", len(cfg.Zones))
	}

	s := &GameState{Players: map[string]*Player{}}
	s.Players["player1"] = &Player{ID: "player1", TeamID: 1, Metrics: NewMatchMetrics()}
	s.MapConfig = cfg
	s.installZonesLocked()

	owner := func(id string) string { return s.zoneRuntimeByIDLocked(id).Owner }

	// Seed is owned from the start; the player can build there immediately.
	if owner("seed") != "player1" {
		t.Fatalf("seed should start player1-owned, got %q", owner("seed"))
	}
	seedCell := gridPoint{X: cfg.Zones[0].Cells[0][0], Y: cfg.Zones[0].Cells[0][1]}
	if zid, ok := s.zoneOwnerForCellLocked(seedCell); !ok || !s.zonesAlliedLocked(s.zoneRuntimeByIDLocked(zid).Owner, "player1") {
		t.Fatal("player1 should be able to build in the seed zone from the start")
	}

	// Before any ticks, the player cannot build in north (neutral).
	if s.zonesAlliedLocked(owner("north"), "player1") {
		t.Fatal("north should not be buildable before capture")
	}

	// One tick: the clear zone (east, no hostiles inside, adjacent to seed)
	// captures immediately; north still needs its presence timer.
	s.tickZonesLocked(0.1)
	if owner("east") != protocol.ZoneCaptureTeamOwner {
		t.Fatalf("east (clear, no hostiles) should capture to the team immediately, got %q", owner("east"))
	}
	if owner("north") != protocol.ZoneCaptureNeutralOwner {
		t.Fatalf("north should not capture without occupation, got %q", owner("north"))
	}

	// Occupy north and run its 3s presence timer.
	s.addZoneTestUnit("player1", cfg.Zones[1].Anchor.X, cfg.Zones[1].Anchor.Y)
	// Far must stay locked while north is still neutral.
	s.addZoneTestUnit("player1", cfg.Zones[3].Anchor.X, cfg.Zones[3].Anchor.Y)

	// Install a required capture_zone objective on north.
	installObjective(t, s, "take_north", "capture_zone", "team", true,
		captureZoneConfig{ZoneIDs: []string{"north"}})

	for i := 0; i < 40 && owner("north") != protocol.ZoneCaptureTeamOwner; i++ {
		s.tickZonesLocked(0.1)
	}
	if owner("north") != protocol.ZoneCaptureTeamOwner {
		t.Fatalf("north should capture to the team after its presence timer, got %q", owner("north"))
	}
	// Build rights opened on north exactly on capture.
	if !s.zonesAlliedLocked(owner("north"), "player1") {
		t.Fatal("north should be buildable once captured")
	}
	// The required objective completes.
	s.evaluateObjectivesLocked()
	if !s.Objectives[0].TeamState.Completed {
		t.Fatal("capture_zone objective should complete once north is held")
	}

	// Far was locked until north fell; now (with a unit already inside) it
	// becomes capturable and eventually flips.
	for i := 0; i < 40 && owner("far") != protocol.ZoneCaptureTeamOwner; i++ {
		s.tickZonesLocked(0.1)
	}
	if owner("far") != protocol.ZoneCaptureTeamOwner {
		t.Fatalf("far should capture once north (its neighbour) is held, got %q", owner("far"))
	}
}
