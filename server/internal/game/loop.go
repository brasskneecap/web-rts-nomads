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

		for {
			select {
			case <-l.ticker.C:
				l.state.Update(dt)
				l.broadcaster.BroadcastSnapshot()

				if l.state.IsGameOver() {
					l.ticker.Stop()
					l.stopOnce.Do(func() { close(l.quit) })
					if l.OnGameOver != nil {
						l.OnGameOver()
					}
					return
				}

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
