package game

import (
	"testing"
)

// noopBroadcaster satisfies Broadcaster without doing anything.
type noopBroadcaster struct{}

func (noopBroadcaster) BroadcastSnapshot() {}

func TestLoop_Stop_Idempotent(t *testing.T) {
	state := NewGameState(GetMapConfigByID(DefaultMapID()))
	loop := NewLoop(state, noopBroadcaster{})
	loop.Start()

	// First Stop should succeed without panic.
	loop.Stop()

	// Second and third Stop calls must not panic.
	loop.Stop()
	loop.Stop()
}
