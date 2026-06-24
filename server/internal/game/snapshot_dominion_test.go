package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestSnapshotGameOver_YourDominionPointsEarned_FOWBranch exercises the
// FOW (filtered) branch of snapshotForPlayerLocked. Players added via
// EnsurePlayer receive a FOW entry, so snapshotForPlayerLocked takes the
// filtered path. The test verifies that each viewer sees their own
// MatchDominionPointsEarned — not the other player's — in the game-over
// snapshot field YourDominionPointsEarned.
func TestSnapshotGameOver_YourDominionPointsEarned_FOWBranch(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)

	// Add two players via EnsurePlayer — this initialises FOW for each,
	// so snapshotForPlayerLocked will take the FOW (filtered) branch.
	s.EnsurePlayer("p1")
	s.EnsurePlayer("p2")

	// Directly set the per-match earned totals. These are plain int fields;
	// no lock needed for setup before any goroutine accesses the state.
	s.Players["p1"].MatchDominionPointsEarned = 4
	s.Players["p2"].MatchDominionPointsEarned = 9

	// Mark p2 as lost to trigger game-over state.
	if s.lostPlayerIDs == nil {
		s.lostPlayerIDs = map[string]bool{}
	}
	s.lostPlayerIDs["p2"] = true

	// SnapshotForPlayer acquires the RLock internally — safe to call directly.
	snapP1 := s.SnapshotForPlayer("p1")
	snapP2 := s.SnapshotForPlayer("p2")

	// Both snapshots must carry a game-over block (lostPlayerIDs is non-empty).
	if snapP1.GameOver == nil {
		t.Fatal("p1 snapshot: expected GameOver != nil when lostPlayerIDs is set")
	}
	if snapP2.GameOver == nil {
		t.Fatal("p2 snapshot: expected GameOver != nil when lostPlayerIDs is set")
	}

	// Each viewer must see their OWN earned total.
	if snapP1.GameOver.YourDominionPointsEarned != 4 {
		t.Errorf("p1 snapshot: YourDominionPointsEarned: want 4, got %d",
			snapP1.GameOver.YourDominionPointsEarned)
	}
	if snapP2.GameOver.YourDominionPointsEarned != 9 {
		t.Errorf("p2 snapshot: YourDominionPointsEarned: want 9, got %d",
			snapP2.GameOver.YourDominionPointsEarned)
	}

	// Sanity: p1's snapshot must NOT show p2's total.
	if snapP1.GameOver.YourDominionPointsEarned == 9 {
		t.Errorf("p1 snapshot shows p2's total (9); field is not per-viewer")
	}
}

// TestSnapshotGameOver_YourDominionPointsEarned_UnfilteredBranch exercises
// the unfiltered (no-FOW) branch of snapshotForPlayerLocked. Players whose
// entries are inserted directly into s.Players (bypassing EnsurePlayer) get
// no FOW entry, so the snapshot takes the unfiltered path. Same per-viewer
// assertion as the FOW branch test.
func TestSnapshotGameOver_YourDominionPointsEarned_UnfilteredBranch(t *testing.T) {
	s := NewGameState(protocol.MapConfig{ID: "test", Width: 100, Height: 100})

	// Insert players directly — no FOW entry is created, so the snapshot
	// takes the unfiltered branch (fow == nil check in snapshotForPlayerLocked).
	s.Players["p1"] = &Player{
		ID:                        "p1",
		Metrics:                   NewMatchMetrics(),
		Resources:                 map[string]int{},
		MatchDominionPointsEarned: 4,
	}
	s.Players["p2"] = &Player{
		ID:                        "p2",
		Metrics:                   NewMatchMetrics(),
		Resources:                 map[string]int{},
		MatchDominionPointsEarned: 9,
	}

	// Mark p2 as lost to trigger game-over state.
	if s.lostPlayerIDs == nil {
		s.lostPlayerIDs = map[string]bool{}
	}
	s.lostPlayerIDs["p2"] = true

	snapP1 := s.SnapshotForPlayer("p1")
	snapP2 := s.SnapshotForPlayer("p2")

	if snapP1.GameOver == nil {
		t.Fatal("p1 snapshot: expected GameOver != nil when lostPlayerIDs is set")
	}
	if snapP2.GameOver == nil {
		t.Fatal("p2 snapshot: expected GameOver != nil when lostPlayerIDs is set")
	}

	if snapP1.GameOver.YourDominionPointsEarned != 4 {
		t.Errorf("p1 snapshot: YourDominionPointsEarned: want 4, got %d",
			snapP1.GameOver.YourDominionPointsEarned)
	}
	if snapP2.GameOver.YourDominionPointsEarned != 9 {
		t.Errorf("p2 snapshot: YourDominionPointsEarned: want 9, got %d",
			snapP2.GameOver.YourDominionPointsEarned)
	}

	// Sanity: p1 must not see p2's total.
	if snapP1.GameOver.YourDominionPointsEarned == 9 {
		t.Errorf("p1 snapshot shows p2's total (9); field is not per-viewer")
	}
}

// TestSnapshotGameOver_YourDominionPointsEarned_IncludesWinLossBonus verifies
// that YourDominionPointsEarned in the game-over snapshot includes the win/loss
// bonus from tuning, not just the per-kill drops. It uses the FOW branch (two
// EnsurePlayer registrations) and sets non-zero WinBonus and LossConsolation in
// the tuning singleton, restoring them on cleanup. Expected values are derived
// from the tuning singleton (never hardcoded) to satisfy the no-hardcoded-
// tunables rule.
func TestSnapshotGameOver_YourDominionPointsEarned_IncludesWinLossBonus(t *testing.T) {
	// Override tuning to give non-zero bonuses for the duration of this test.
	prevWin := gameplayTuningSingleton.DominionPoints.WinBonus
	prevLoss := gameplayTuningSingleton.DominionPoints.LossConsolation
	gameplayTuningSingleton.DominionPoints.WinBonus = 10
	gameplayTuningSingleton.DominionPoints.LossConsolation = 3
	t.Cleanup(func() {
		gameplayTuningSingleton.DominionPoints.WinBonus = prevWin
		gameplayTuningSingleton.DominionPoints.LossConsolation = prevLoss
	})

	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.EnsurePlayer("p1")
	s.EnsurePlayer("p2")

	// Give each player some per-kill drops.
	s.Players["p1"].MatchDominionPointsEarned = 5
	s.Players["p2"].MatchDominionPointsEarned = 2

	// p2 lost; p1 is the winner.
	if s.lostPlayerIDs == nil {
		s.lostPlayerIDs = map[string]bool{}
	}
	s.lostPlayerIDs["p2"] = true

	snapP1 := s.SnapshotForPlayer("p1")
	snapP2 := s.SnapshotForPlayer("p2")

	if snapP1.GameOver == nil {
		t.Fatal("p1 snapshot: expected GameOver != nil")
	}
	if snapP2.GameOver == nil {
		t.Fatal("p2 snapshot: expected GameOver != nil")
	}

	// Read expected values from the tuning singleton — never from literals.
	wantWinBonus := gameplayTuningSingleton.DominionPoints.WinBonus
	wantLossConsolation := gameplayTuningSingleton.DominionPoints.LossConsolation

	wantP1 := s.Players["p1"].MatchDominionPointsEarned + wantWinBonus
	wantP2 := s.Players["p2"].MatchDominionPointsEarned + wantLossConsolation

	if snapP1.GameOver.YourDominionPointsEarned != wantP1 {
		t.Errorf("p1 (winner) YourDominionPointsEarned: want %d (drops=%d + winBonus=%d), got %d",
			wantP1, s.Players["p1"].MatchDominionPointsEarned, wantWinBonus,
			snapP1.GameOver.YourDominionPointsEarned)
	}
	if snapP2.GameOver.YourDominionPointsEarned != wantP2 {
		t.Errorf("p2 (loser) YourDominionPointsEarned: want %d (drops=%d + lossConsolation=%d), got %d",
			wantP2, s.Players["p2"].MatchDominionPointsEarned, wantLossConsolation,
			snapP2.GameOver.YourDominionPointsEarned)
	}
}

// TestSnapshotGameOver_YourDominionPointsEarned_ZeroOmitted verifies that
// when a player has earned zero dominion points, the game-over snapshot's
// YourDominionPointsEarned is zero (omitempty means it won't appear on wire,
// but the struct field stays zero and other assertions still hold).
func TestSnapshotGameOver_YourDominionPointsEarned_ZeroOmitted(t *testing.T) {
	s := NewGameState(protocol.MapConfig{ID: "test", Width: 100, Height: 100})

	s.Players["p1"] = &Player{
		ID:        "p1",
		Metrics:   NewMatchMetrics(),
		Resources: map[string]int{},
		// MatchDominionPointsEarned intentionally left at zero.
	}

	if s.lostPlayerIDs == nil {
		s.lostPlayerIDs = map[string]bool{}
	}
	s.lostPlayerIDs["p1"] = true

	snap := s.SnapshotForPlayer("p1")

	if snap.GameOver == nil {
		t.Fatal("expected GameOver != nil when lostPlayerIDs is set")
	}
	if snap.GameOver.YourDominionPointsEarned != 0 {
		t.Errorf("expected YourDominionPointsEarned == 0 for player with no earnings, got %d",
			snap.GameOver.YourDominionPointsEarned)
	}
}
