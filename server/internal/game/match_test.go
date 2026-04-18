package game

import (
	"testing"
	"time"
)

// newTestMatch creates a Match and registers it with a MatchManager so that
// the timer callback's DeleteMatch call has a valid target. The match is
// stopped and deleted from the manager by the caller's cleanup.
func newTestMatch(t *testing.T) (*Match, *MatchManager) {
	t.Helper()
	mgr := NewMatchManager()
	match := NewMatch("test-match", DefaultMapID())
	mgr.mu.Lock()
	mgr.matches[match.ID] = match
	mgr.mu.Unlock()
	return match, mgr
}

// TestSchedulePlayerRemoval_CancelWithinGrace verifies that cancelling a
// pending removal before the grace window expires leaves the player's state
// intact and no removal happens after the original grace period.
func TestSchedulePlayerRemoval_CancelWithinGrace(t *testing.T) {
	match, mgr := newTestMatch(t)
	defer match.Stop()

	const grace = 80 * time.Millisecond
	playerID := "player-1"
	match.State.EnsurePlayer(playerID)

	if match.PlayerCount() != 1 {
		t.Fatalf("expected 1 player before schedule, got %d", match.PlayerCount())
	}

	match.SchedulePlayerRemoval(playerID, grace, mgr)

	if match.PendingCleanupCount() != 1 {
		t.Fatalf("expected 1 pending cleanup after schedule, got %d", match.PendingCleanupCount())
	}

	// Cancel before the timer fires.
	cancelled := match.CancelPlayerRemoval(playerID)
	if !cancelled {
		t.Fatal("CancelPlayerRemoval returned false; expected true for a pending removal")
	}

	if match.PendingCleanupCount() != 0 {
		t.Fatalf("expected 0 pending cleanups after cancel, got %d", match.PendingCleanupCount())
	}

	// Wait well past the original grace window to confirm the callback did not fire.
	time.Sleep(grace * 3)

	if match.PlayerCount() != 1 {
		t.Fatalf("player should still exist after cancel, got %d players", match.PlayerCount())
	}
}

// TestSchedulePlayerRemoval_FiresAfterGrace verifies that a removal that is
// NOT cancelled executes after the grace window and the player is removed.
func TestSchedulePlayerRemoval_FiresAfterGrace(t *testing.T) {
	match, mgr := newTestMatch(t)
	defer match.Stop()

	const grace = 50 * time.Millisecond
	playerID := "player-2"
	match.State.EnsurePlayer(playerID)

	done := make(chan struct{})
	// Wrap RemovePlayer so we can get notified when the callback fires.
	// We do this by polling; the timer fires asynchronously.

	match.SchedulePlayerRemoval(playerID, grace, mgr)

	// Poll until the player is gone or we time out.
	deadline := time.Now().Add(grace * 10)
	for time.Now().Before(deadline) {
		if match.PlayerCount() == 0 {
			close(done)
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	select {
	case <-done:
		// Good — player was removed.
	default:
		t.Fatal("player was not removed after grace window expired")
	}

	if match.PendingCleanupCount() != 0 {
		t.Fatalf("expected 0 pending cleanups after timer fired, got %d", match.PendingCleanupCount())
	}
}

// TestCancelPlayerRemoval_DoubleCancelIsSafe verifies that calling
// CancelPlayerRemoval twice for the same player does not panic.
func TestCancelPlayerRemoval_DoubleCancelIsSafe(t *testing.T) {
	match, mgr := newTestMatch(t)
	defer match.Stop()

	const grace = 200 * time.Millisecond
	playerID := "player-3"
	match.State.EnsurePlayer(playerID)

	match.SchedulePlayerRemoval(playerID, grace, mgr)

	first := match.CancelPlayerRemoval(playerID)
	if !first {
		t.Fatal("first CancelPlayerRemoval should return true")
	}

	// Second call: nothing pending, must return false without panic.
	second := match.CancelPlayerRemoval(playerID)
	if second {
		t.Fatal("second CancelPlayerRemoval should return false")
	}
}

// TestDeleteMatch_StopsPendingTimers verifies that deleting a match via the
// manager stops all pending player-removal timers so they don't fire into a
// deleted match. No panics is the key assertion.
func TestDeleteMatch_StopsPendingTimers(t *testing.T) {
	match, mgr := newTestMatch(t)

	const grace = 100 * time.Millisecond
	for _, id := range []string{"p1", "p2", "p3"} {
		match.State.EnsurePlayer(id)
		match.SchedulePlayerRemoval(id, grace, mgr)
	}

	if match.PendingCleanupCount() != 3 {
		t.Fatalf("expected 3 pending cleanups, got %d", match.PendingCleanupCount())
	}

	// Delete the match; this must stop all timers.
	mgr.DeleteMatch(match.ID)

	// Wait long enough that all timers would have fired if not stopped.
	time.Sleep(grace * 4)

	// The match was deleted from the manager; no panic should have occurred.
	if match.PendingCleanupCount() != 0 {
		t.Fatalf("expected 0 pending cleanups after DeleteMatch, got %d", match.PendingCleanupCount())
	}
}
