package game

import (
	"testing"
)

// TestGameState_RNGStreams_SeedStable asserts that two GameState instances
// created with the same seed produce identical sequences from rngPerks and
// rngCosmetic. This proves the named-stream plumbing works correctly: each
// stream is seeded deterministically from matchSeed and will not drift across
// matches that share the same seed value.
func TestGameState_RNGStreams_SeedStable(t *testing.T) {
	const seed = int64(0xDEADBEEFCAFE)
	const draws = 20

	cfg := GetMapConfigByID(DefaultMapID())
	a := NewGameStateWithSeed(cfg, seed)
	b := NewGameStateWithSeed(cfg, seed)

	for i := 0; i < draws; i++ {
		wantPerks := a.rngPerks.Int63()
		gotPerks := b.rngPerks.Int63()
		if wantPerks != gotPerks {
			t.Fatalf("rngPerks draw %d: seed=%d got %d, want %d", i, seed, gotPerks, wantPerks)
		}

		wantCosmetic := a.rngCosmetic.Int63()
		gotCosmetic := b.rngCosmetic.Int63()
		if wantCosmetic != gotCosmetic {
			t.Fatalf("rngCosmetic draw %d: seed=%d got %d, want %d", i, seed, gotCosmetic, wantCosmetic)
		}
	}
}

// TestGameState_RNGStreams_DifferentSeeds asserts that two different seeds
// produce distinct sequences, confirming the streams are not accidentally
// sharing state or defaulting to the same constant.
func TestGameState_RNGStreams_DifferentSeeds(t *testing.T) {
	cfg := GetMapConfigByID(DefaultMapID())
	a := NewGameStateWithSeed(cfg, 111)
	b := NewGameStateWithSeed(cfg, 222)

	same := true
	for i := 0; i < 10; i++ {
		if a.rngPerks.Int63() != b.rngPerks.Int63() {
			same = false
			break
		}
	}
	if same {
		t.Fatal("rngPerks produced identical sequences for different seeds — streams are not independent")
	}
}
