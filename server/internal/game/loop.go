package game

import (
	"sync"
	"time"
)

type Broadcaster interface {
	BroadcastSnapshot()
}

type Loop struct {
	state       *GameState
	broadcaster Broadcaster
	ticker      *time.Ticker
	quit        chan struct{}
	stopOnce    sync.Once
	OnGameOver  func() // called once when the game state transitions to game-over
}

func NewLoop(state *GameState, broadcaster Broadcaster) *Loop {
	return &Loop{
		state:       state,
		broadcaster: broadcaster,
		ticker:      time.NewTicker(50 * time.Millisecond), // 20 ticks/sec
		quit:        make(chan struct{}),
	}
}

func (l *Loop) Start() {
	go func() {
		const dt = 1.0 / 20.0

		// gameOverFired gates the one-shot terminal side effects (dominion-point
		// commit, teardown scheduling). It fires the tick the match first reaches
		// a terminal outcome — victory OR defeat.
		gameOverFired := false

		// simHalted freezes the simulation (no more Update calls). It is
		// DECOUPLED from game-over: a continue-play match (campaign with required
		// objectives) reaches victory — firing OnGameOver once to bank the win —
		// but keeps ticking so the player can "Continue Playing". The sim halts
		// only on a real defeat, or on victory in a non-continue match.
		//
		// Even once halted the loop KEEPS broadcasting the frozen final state
		// until Stop(). Halting on the game-over tick would make the end screen
		// depend on a single snapshot delivery; a client that missed that one
		// packet froze with no end screen (see the frozen forest-1 post-mortem).
		simHalted := false

		for {
			select {
			case <-l.ticker.C:
				if !simHalted {
					l.state.Update(dt)
					if !gameOverFired && l.state.IsGameOver() {
						gameOverFired = true
						if l.OnGameOver != nil {
							l.OnGameOver()
						}
					}
					if l.state.IsSimulationHalted() {
						simHalted = true
					}
				}
				l.broadcaster.BroadcastSnapshot()

			case <-l.quit:
				l.ticker.Stop()
				return
			}
		}
	}()
}

// Stop signals the tick loop to exit. Safe to call multiple times.
func (l *Loop) Stop() {
	l.stopOnce.Do(func() {
		close(l.quit)
	})
}
