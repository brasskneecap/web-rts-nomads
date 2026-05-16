package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// addTownhall appends a visible townhall owned by ownerID. Caller holds s.mu.
func addTownhall(s *GameState, id, ownerID string) {
	o := ownerID
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
		ID: id, BuildingType: "townhall", Visible: true, OwnerID: &o,
	})
}

// destroyTownhall marks the townhall with the given id non-visible (the loss
// check skips non-visible townhalls — same as a destroyed one). Caller holds s.mu.
func destroyTownhall(s *GameState, id string) {
	for i := range s.MapConfig.Buildings {
		if s.MapConfig.Buildings[i].ID == id {
			s.MapConfig.Buildings[i].Visible = false
		}
	}
}

// Co-op: a team is defeated only when ALL members' townhalls are gone —
// losing one ally's townhall does NOT end the match (teammates carry it).
func TestTeam_DefeatRequiresAllMembers(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	addTownhall(s, "th_p1", "p1")
	addTownhall(s, "th_p2", "p2")
	setTeam(s, "p1", 0)
	setTeam(s, "p2", 0) // same team (co-op)

	s.checkPlayerLossLocked()
	if len(s.lostPlayerIDs) != 0 {
		t.Fatalf("nobody should be lost while both townhalls stand: %v", s.lostPlayerIDs)
	}

	// One ally's townhall falls — the team carries on, NOBODY is lost.
	destroyTownhall(s, "th_p1")
	s.checkPlayerLossLocked()
	if s.lostPlayerIDs["p1"] || s.lostPlayerIDs["p2"] || len(s.lostPlayerIDs) != 0 {
		t.Errorf("losing one teammate's townhall must not defeat the co-op team: %v", s.lostPlayerIDs)
	}

	// Last townhall of the team falls — now the WHOLE team is defeated.
	destroyTownhall(s, "th_p2")
	s.checkPlayerLossLocked()
	if !s.lostPlayerIDs["p1"] || !s.lostPlayerIDs["p2"] {
		t.Errorf("when the team's last townhall falls, ALL members must be lost; got %v", s.lostPlayerIDs)
	}
}

// PvP: opposing teams fall independently — eliminating one team does not
// defeat the other.
func TestTeam_DefeatIsPerTeamIndependent(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	addTownhall(s, "th_a", "pa")
	addTownhall(s, "th_b", "pb")
	setTeam(s, "pa", 0)
	setTeam(s, "pb", 1) // different teams

	s.checkPlayerLossLocked() // register both owners while their TH stand

	destroyTownhall(s, "th_a") // team 0 wiped; team 1 intact
	s.checkPlayerLossLocked()

	if !s.lostPlayerIDs["pa"] {
		t.Error("pa's team lost its only townhall — pa must be defeated")
	}
	if s.lostPlayerIDs["pb"] {
		t.Error("pb is on a different, intact team — must NOT be defeated")
	}
}

// Extensibility: flipping a teammate to a different team changes the defeat
// outcome with no other wiring (pure data) — the bake-in at the victory layer.
func TestTeam_DefeatFollowsTeamFlip(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	addTownhall(s, "th_x", "px") // px has no townhall of its own after this falls
	addTownhall(s, "th_y", "py")
	setTeam(s, "px", 0)
	setTeam(s, "py", 0) // allied — py's townhall covers px

	s.checkPlayerLossLocked() // register px & py while their TH stand

	destroyTownhall(s, "th_x")
	s.checkPlayerLossLocked()
	if s.lostPlayerIDs["px"] {
		t.Fatal("while allied with py (who still has a townhall), px must not be defeated")
	}

	// Flip py to another team: px is now alone on team 0 with no townhalls.
	setTeam(s, "py", 4)
	s.checkPlayerLossLocked()
	if !s.lostPlayerIDs["px"] {
		t.Error("after py leaves the team, px (alone, no townhall) must be defeated")
	}
}
