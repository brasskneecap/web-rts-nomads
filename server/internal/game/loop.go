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

		// Once the game is over the simulation freezes (no more Update calls)
		// but the loop KEEPS broadcasting the final state until Stop() —
		// in production that's DeleteMatch at the end of OnGameOver's
		// 15-second wind-down. Halting on the game-over tick would make the
		// end screen depend on a single snapshot delivery; a client that
		// missed that one packet froze with no end screen (see the frozen
		// forest-1 match post-mortem).
		gameOverFired := false

		for {
			select {
			case <-l.ticker.C:
				if !gameOverFired {
					l.state.Update(dt)
					if l.state.IsGameOver() {
						gameOverFired = true
						if l.OnGameOver != nil {
							l.OnGameOver()
						}
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
