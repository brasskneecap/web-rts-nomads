package game

import (
	"sync/atomic"
	"testing"
	"time"
)

type countingBroadcaster struct{ n atomic.Int64 }

func (b *countingBroadcaster) BroadcastSnapshot() { b.n.Add(1) }

// Regression test for the frozen-end-screen bug: the tick loop used to halt
// on the same tick it detected game over, so exactly ONE snapshot ever
// carried the game-over payload — a client that missed that single packet
// froze with no end screen and nothing further to receive.
//
// The loop must instead keep broadcasting the (frozen) final state until
// Stop() is called — in production that's DeleteMatch at the end of the
// 15-second wind-down — while still firing OnGameOver exactly once.
func TestLoop_KeepsBroadcastingAfterGameOverUntilStopped(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	s.mu.Lock()
	s.victoryAchieved = true
	s.mu.Unlock()

	b := &countingBroadcaster{}
	l := NewLoop(s, b)
	var fired atomic.Int64
	l.OnGameOver = func() { fired.Add(1) }

	l.Start()
	defer l.Stop()

	// The loop ticks at 20Hz; five broadcasts arrive within ~250ms. Poll with
	// a generous deadline so a loaded CI box doesn't flake.
	deadline := time.Now().Add(5 * time.Second)
	for b.n.Load() < 5 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if got := b.n.Load(); got < 5 {
		t.Fatalf("loop stopped broadcasting after game over: got %d broadcasts, want >= 5", got)
	}
	if got := fired.Load(); got != 1 {
		t.Fatalf("OnGameOver should fire exactly once, fired %d times", got)
	}
}
