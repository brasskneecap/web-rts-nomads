package ws

import (
	"sync"
	"testing"
)

// TestClient_Accessors_ConcurrentReadWrite verifies that SetMatchID/MatchID and
// SetPlayerID/PlayerID are safe under concurrent access. Running with -race will
// catch any unsynchronised path that slips through.
func TestClient_Accessors_ConcurrentReadWrite(t *testing.T) {
	// We test the accessor methods directly without a real websocket connection
	// by constructing a Client with a nil Conn. Accessors only touch the mutex
	// and string fields, so nil Conn is safe here.
	c := &Client{}

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			c.SetMatchID("match-1")
		}()
		go func() {
			defer wg.Done()
			_ = c.MatchID()
		}()
	}

	wg.Wait()

	wg.Add(goroutines * 2)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			c.SetPlayerID("player-1")
		}()
		go func() {
			defer wg.Done()
			_ = c.PlayerID()
		}()
	}
	wg.Wait()
}

// TestClient_Accessors_SetAndGet verifies basic get/set round-trips.
func TestClient_Accessors_SetAndGet(t *testing.T) {
	tests := []struct {
		name     string
		matchID  string
		playerID string
	}{
		{"normal IDs", "match-42", "player-7"},
		{"empty IDs", "", ""},
		{"only matchID", "match-1", ""},
		{"only playerID", "", "player-99"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{}
			c.SetMatchID(tt.matchID)
			c.SetPlayerID(tt.playerID)

			if got := c.MatchID(); got != tt.matchID {
				t.Errorf("MatchID() = %q; want %q", got, tt.matchID)
			}
			if got := c.PlayerID(); got != tt.playerID {
				t.Errorf("PlayerID() = %q; want %q", got, tt.playerID)
			}
		})
	}
}
