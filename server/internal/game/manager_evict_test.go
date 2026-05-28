package game

import "testing"

// TestEvictPlayerFromOtherMatches_RemovesPlayerAndDeletesEmptyMatch
// simulates "player joins a new game while still in a prior match" — the
// prior match must be evicted, and if it's now empty it must be deleted.
func TestEvictPlayerFromOtherMatches_RemovesPlayerAndDeletesEmptyMatch(t *testing.T) {
	mm := NewMatchManager()

	prior := mm.NewMatch("default")
	prior.State.EnsurePlayer("p1")
	if !prior.HasPlayer("p1") {
		t.Fatal("setup: p1 should be in prior match")
	}

	// Player joins a new game — exceptMatchID is empty.
	mm.EvictPlayerFromOtherMatches("p1", "")

	if prior.HasPlayer("p1") {
		t.Error("p1 should have been removed from prior match")
	}
	if _, exists := mm.GetMatch(prior.ID); exists {
		t.Error("prior match should have been deleted (no remaining clients)")
	}
}

// TestEvictPlayerFromOtherMatches_ExemptsExceptMatch verifies the player
// is NOT evicted from the match they are currently joining.
func TestEvictPlayerFromOtherMatches_ExemptsExceptMatch(t *testing.T) {
	mm := NewMatchManager()

	keep := mm.NewMatch("default")
	keep.State.EnsurePlayer("p1")

	mm.EvictPlayerFromOtherMatches("p1", keep.ID)

	if !keep.HasPlayer("p1") {
		t.Error("p1 should still be in the exempt match")
	}
	if _, exists := mm.GetMatch(keep.ID); !exists {
		t.Error("exempt match should not have been deleted")
	}
}

// TestEvictPlayerFromOtherMatches_CancelsGraceTimer verifies that a
// player scheduled for grace-window removal in a prior match is evicted
// immediately, with the grace timer cancelled.
func TestEvictPlayerFromOtherMatches_CancelsGraceTimer(t *testing.T) {
	mm := NewMatchManager()

	prior := mm.NewMatch("default")
	prior.State.EnsurePlayer("p1")
	prior.SchedulePlayerRemoval("p1", PlayerRemovalGrace, mm)
	if prior.PendingCleanupCount() != 1 {
		t.Fatal("setup: prior match should have one pending cleanup")
	}

	mm.EvictPlayerFromOtherMatches("p1", "")

	if prior.PendingCleanupCount() != 0 {
		t.Error("pending cleanup should have been cancelled by eviction")
	}
	if prior.HasPlayer("p1") {
		t.Error("p1 should have been removed by eviction")
	}
	if _, exists := mm.GetMatch(prior.ID); exists {
		t.Error("prior match should be deleted after eviction")
	}
}
