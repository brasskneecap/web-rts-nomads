package game

import (
	"sort"
	"sync"
	"testing"
)

// fakeCommitter records every CommitDominionPoints call so tests can assert
// which players were credited what amount.
type fakeCommitter struct {
	mu    sync.Mutex
	calls map[string]int
}

func newFakeCommitter() *fakeCommitter {
	return &fakeCommitter{calls: make(map[string]int)}
}

func (f *fakeCommitter) CommitDominionPoints(playerID string, earned int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[playerID] += earned
	return nil
}

func (f *fakeCommitter) get(playerID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[playerID]
}

func (f *fakeCommitter) playerIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := make([]string, 0, len(f.calls))
	for id := range f.calls {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// TestHumanPlayerMatchSummaries_SkipsAIAndSorts verifies that the game-state
// helper that the OnGameOver hook iterates over returns only real players,
// in deterministic order, with the right DominionPointsEarned totals.
func TestHumanPlayerMatchSummaries_SkipsAIAndSorts(t *testing.T) {
	s := NewGameState(GetMapConfigByID(DefaultMapID()))
	s.EnsurePlayer("p_zebra")
	s.EnsurePlayer("p_alpha")

	s.mu.Lock()
	s.Players["p_alpha"].RunDominionPointDrops = 4
	s.Players["p_zebra"].RunDominionPointDrops = 7
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, RunDominionPointDrops: 999}
	s.Players[neutralPlayerID] = &Player{ID: neutralPlayerID, RunDominionPointDrops: 999}
	s.mu.Unlock()

	summaries := s.HumanPlayerMatchSummaries()
	if len(summaries) != 2 {
		t.Fatalf("expected 2 human summaries, got %d (%+v)", len(summaries), summaries)
	}
	if summaries[0].PlayerID != "p_alpha" || summaries[1].PlayerID != "p_zebra" {
		t.Errorf("summaries not sorted by player ID: %+v", summaries)
	}
	if summaries[0].DominionPointsEarned != 4 {
		t.Errorf("p_alpha DominionPointsEarned: want 4, got %d", summaries[0].DominionPointsEarned)
	}
	if summaries[1].DominionPointsEarned != 7 {
		t.Errorf("p_zebra DominionPointsEarned: want 7, got %d", summaries[1].DominionPointsEarned)
	}
}

// TestMatchManager_OnGameOver_CommitsDominionPointsToCommitter wires a fake
// committer, primes a match with non-zero RunDominionPointDrops, fires the
// OnGameOver callback directly, and verifies the committer received the
// expected per-player totals. AI and zero-earned players must NOT receive
// commit calls.
func TestMatchManager_OnGameOver_CommitsDominionPointsToCommitter(t *testing.T) {
	mm := NewMatchManager()
	committer := newFakeCommitter()
	mm.SetDominionPointCommitter(committer)

	match := mm.NewMatch("default")
	t.Cleanup(func() { mm.DeleteMatch(match.ID) })

	match.State.EnsurePlayer("p1")
	match.State.EnsurePlayer("p2")
	match.State.EnsurePlayer("p3_no_drops")

	match.State.mu.Lock()
	match.State.Players["p1"].RunDominionPointDrops = 12
	match.State.Players["p2"].RunDominionPointDrops = 3
	// p3_no_drops left at zero — must not be committed.
	match.State.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, RunDominionPointDrops: 9999}
	match.State.mu.Unlock()

	// Fire the OnGameOver hook directly. The real game-over transition runs
	// it from the tick goroutine; bypassing the tick here keeps the test
	// deterministic and independent of the simulation.
	match.loop.OnGameOver()

	if got := committer.get("p1"); got != 12 {
		t.Errorf("committer.p1: want 12, got %d", got)
	}
	if got := committer.get("p2"); got != 3 {
		t.Errorf("committer.p2: want 3, got %d", got)
	}
	if got := committer.get("p3_no_drops"); got != 0 {
		t.Errorf("committer.p3_no_drops: want 0 (no call), got %d", got)
	}
	if got := committer.get(enemyPlayerID); got != 0 {
		t.Errorf("committer should not be called for AI player; got %d", got)
	}

	ids := committer.playerIDs()
	if len(ids) != 2 || ids[0] != "p1" || ids[1] != "p2" {
		t.Errorf("only p1 and p2 should have been committed; got %v", ids)
	}
}

// TestMatchManager_OnGameOver_NoCommitterIsNoOp verifies that omitting the
// committer (the default for tests) does not crash and does not block match
// deletion scheduling.
func TestMatchManager_OnGameOver_NoCommitterIsNoOp(t *testing.T) {
	mm := NewMatchManager()
	// Deliberately no SetDominionPointCommitter call.

	match := mm.NewMatch("default")
	t.Cleanup(func() { mm.DeleteMatch(match.ID) })

	match.State.EnsurePlayer("p1")
	match.State.mu.Lock()
	match.State.Players["p1"].RunDominionPointDrops = 5
	match.State.mu.Unlock()

	// Should not panic.
	match.loop.OnGameOver()
}
