package game

import (
	"os"
	"testing"
)

// TestMain neutralizes the StartWaveBonus debug toggle for the entire game test
// package. StartWaveBonus is a player.json switch the user flips on and off; when
// on, a match opens in the wave "upgrade" phase (simulation frozen) with an
// RNG-seeded start-of-match offer. That would non-deterministically break the
// many tests here that build a match and tick it from t=0 on a wave-enabled map
// (e.g. FOW reveal, movement, combat) — none of which are about the start bonus.
//
// Forcing it off here makes those tests independent of the committed toggle
// value, so it can be turned on or off without breaking the suite. Tests that
// specifically exercise the start bonus opt back in via
// withStartWaveBonusToggle(t, true). Production reads the committed value
// unchanged.
func TestMain(m *testing.M) {
	restore := SetStartWaveBonusForTest(false)
	code := m.Run()
	restore()
	os.Exit(code)
}
