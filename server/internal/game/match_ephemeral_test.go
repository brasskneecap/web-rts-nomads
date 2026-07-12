package game

import "testing"

// TestEphemeralMatch_SuppressesDominionCommit: an ephemeral match's end-of-game
// DP commit path must NOT call the committer, while a normal match must.
func TestEphemeralMatch_SuppressesDominionCommit(t *testing.T) {
	// Normal match commits.
	mmNormal := NewMatchManager()
	cNormal := newFakeCommitter()
	mmNormal.SetDominionPointCommitter(cNormal)
	normal := mmNormal.NewMatch(DefaultMapID())
	normal.State.EnsurePlayer("p1")
	normal.State.mu.Lock()
	normal.State.Players["p1"].RunDominionPointDrops = 5
	normal.State.mu.Unlock()
	normal.loop.OnGameOver()
	if cNormal.get("p1") != 5 {
		t.Fatalf("normal match: committer should receive 5, got %d", cNormal.get("p1"))
	}

	// Ephemeral match suppresses.
	mmEph := NewMatchManager()
	cEph := newFakeCommitter()
	mmEph.SetDominionPointCommitter(cEph)
	eph := mmEph.NewEphemeralMatch(DefaultMapID())
	if !eph.State.Ephemeral {
		t.Fatal("NewEphemeralMatch must set State.Ephemeral")
	}
	eph.State.EnsurePlayer("p1")
	eph.State.mu.Lock()
	eph.State.Players["p1"].RunDominionPointDrops = 5
	eph.State.mu.Unlock()
	eph.loop.OnGameOver()
	if cEph.get("p1") != 0 {
		t.Fatalf("ephemeral match must not commit DP, got %d", cEph.get("p1"))
	}
}

// TestFindOrCreateMatch_SkipsEphemeral: a normal join must never be handed an
// ephemeral match sharing the same map id.
func TestFindOrCreateMatch_SkipsEphemeral(t *testing.T) {
	mm := NewMatchManager()
	eph := mm.NewEphemeralMatch(DefaultMapID())
	got := mm.FindOrCreateMatch(DefaultMapID())
	if got.ID == eph.ID {
		t.Fatal("FindOrCreateMatch reused an ephemeral match")
	}
}
