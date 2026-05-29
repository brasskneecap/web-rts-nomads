package game

import (
	"encoding/json"
	"sync"
	"testing"

	"webrts/server/pkg/protocol"
)

// TestMarshalSnapshot_NoRaceWithTickMutations is the regression test for the
// "index out of range" panic seen inside encoding/json's mapEncoder during
// BroadcastSnapshot. Several wire structs intentionally alias live state maps
// (notably BuildingTile.Metadata, which the tick loop rebuilds with
// productionRemainingSeconds / builderCount / etc. every tick). Marshal MUST
// run under s.mu RLock or the encoder iterating a shared map will race with
// the tick loop growing it and crash.
//
// Run under -race: the detector will catch any future regression where the
// marshal path runs outside the lock, even before the panic window is hit.
func TestMarshalSnapshot_NoRaceWithTickMutations(t *testing.T) {
	state := NewGameState(GetMapConfigByID(DefaultMapID()))

	state.mu.Lock()
	if len(state.MapConfig.Buildings) == 0 {
		state.mu.Unlock()
		t.Fatal("test setup: map has no buildings to exercise the snapshot path")
	}
	// Force a non-nil Metadata map on every building so the encoder always
	// has a map to walk. Mirrors what the tick loop does when a building
	// starts producing a unit (state_buildings.go:735).
	for i := range state.MapConfig.Buildings {
		state.MapConfig.Buildings[i].Metadata = map[string]interface{}{
			"tier": float64(1),
		}
	}
	state.mu.Unlock()

	const iters = 300
	var wg sync.WaitGroup
	wg.Add(2)

	// Writer: hammer building Metadata maps under the write lock, both
	// growing existing maps and replacing them with new ones — same pattern
	// as the production loop in state_buildings.go.
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			state.mu.Lock()
			for j := range state.MapConfig.Buildings {
				b := &state.MapConfig.Buildings[j]
				if i%17 == 0 {
					b.Metadata = map[string]interface{}{}
				}
				b.Metadata["productionRemainingSeconds"] = float64(i)
				b.Metadata["productionTotalSeconds"] = float64(10)
				b.Metadata["productionQueueLength"] = i
				b.Metadata["builderCount"] = i % 3
				b.Metadata["currentWorkers"] = i % 5
				b.Metadata["maxWorkers"] = 5
			}
			state.mu.Unlock()
		}
	}()

	// Reader: hammer the marshal-under-lock path. With the architectural
	// fix this acquires RLock for the entire marshal, so it must be free
	// of race-detector hits against the writer above.
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			payload, err := state.MarshalSnapshot("match-1", int64(i))
			if err != nil {
				t.Errorf("MarshalSnapshot failed: %v", err)
				return
			}
			var msg protocol.MatchSnapshotMessage
			if err := json.Unmarshal(payload, &msg); err != nil {
				t.Errorf("snapshot payload is not valid JSON: %v", err)
				return
			}
		}
	}()

	wg.Wait()
}

// TestMarshalWelcomeMessage_NoRaceWithTickMutations is the welcome-envelope
// counterpart: WelcomeMessage embeds the live MapConfig, whose Buildings
// slice shares BuildingTile.Metadata with the tick loop. MarshalWelcomeMessage
// must hold s.mu RLock across the encoder iteration of those maps.
func TestMarshalWelcomeMessage_NoRaceWithTickMutations(t *testing.T) {
	state := NewGameState(GetMapConfigByID(DefaultMapID()))

	state.mu.Lock()
	if len(state.MapConfig.Buildings) == 0 {
		state.mu.Unlock()
		t.Fatal("test setup: map has no buildings to exercise the welcome path")
	}
	for i := range state.MapConfig.Buildings {
		state.MapConfig.Buildings[i].Metadata = map[string]interface{}{
			"tier": float64(1),
		}
	}
	state.mu.Unlock()

	const iters = 200
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			state.mu.Lock()
			for j := range state.MapConfig.Buildings {
				b := &state.MapConfig.Buildings[j]
				if i%17 == 0 {
					b.Metadata = map[string]interface{}{}
				}
				b.Metadata["productionRemainingSeconds"] = float64(i)
				b.Metadata["builderCount"] = i % 3
			}
			state.mu.Unlock()
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			payload, err := state.MarshalWelcomeMessage("p1", "match-1")
			if err != nil {
				t.Errorf("MarshalWelcomeMessage failed: %v", err)
				return
			}
			var msg protocol.WelcomeMessage
			if err := json.Unmarshal(payload, &msg); err != nil {
				t.Errorf("welcome payload is not valid JSON: %v", err)
				return
			}
		}
	}()

	wg.Wait()
}

// TestMarshalSnapshotForPlayer_NoRaceWithTickMutations is the per-player
// counterpart to TestMarshalSnapshot_NoRaceWithTickMutations. Exercises the
// FOW-filtered and own-building branches of snapshotForPlayerLocked.
func TestMarshalSnapshotForPlayer_NoRaceWithTickMutations(t *testing.T) {
	state := NewGameState(GetMapConfigByID(DefaultMapID()))
	playerID := "p1"
	state.EnsurePlayer(playerID)

	state.mu.Lock()
	if len(state.MapConfig.Buildings) == 0 {
		state.mu.Unlock()
		t.Fatal("test setup: map has no buildings to exercise the snapshot path")
	}
	// Mark every building as owned by the viewer so the isOwn branch
	// (state.go: `buildings = append(buildings, *b)`) fires, which is the
	// shallow-copy site that previously aliased Metadata into the wire
	// snapshot.
	for i := range state.MapConfig.Buildings {
		state.MapConfig.Buildings[i].OwnerID = &playerID
		state.MapConfig.Buildings[i].Metadata = map[string]interface{}{
			"tier": float64(1),
		}
	}
	state.mu.Unlock()

	// Build FOW for the viewer so the FOW-filtered branch runs.
	state.Update(0.05)

	const iters = 300
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			state.mu.Lock()
			for j := range state.MapConfig.Buildings {
				b := &state.MapConfig.Buildings[j]
				if i%19 == 0 {
					b.Metadata = map[string]interface{}{}
				}
				b.Metadata["productionRemainingSeconds"] = float64(i)
				b.Metadata["builderCount"] = i % 3
				b.Metadata["currentWorkers"] = i % 5
			}
			state.mu.Unlock()
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			payload, err := state.MarshalSnapshotForPlayer(playerID, "match-1", int64(i))
			if err != nil {
				t.Errorf("MarshalSnapshotForPlayer failed: %v", err)
				return
			}
			var msg protocol.MatchSnapshotMessage
			if err := json.Unmarshal(payload, &msg); err != nil {
				t.Errorf("snapshot payload is not valid JSON: %v", err)
				return
			}
		}
	}()

	wg.Wait()
}
