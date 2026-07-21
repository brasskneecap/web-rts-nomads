package game

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────────────────────
// TestTrapCharacterization — Task 1.1 of the "Trapper traps → abilities"
// migration.
//
// This test PINS the stats of a PLANTED trap for each of the 4 Bronze trap
// perks (caltrops, fire_pit, explosive_trap, marker_trap), including
// fire_pit's per-rank scaling (bronze/silver/gold). It is the behavior
// invariant the whole migration must preserve: later phases refactor HOW
// traps get placed, but the planted Trap's fields must not change.
//
// Each case spawns a Trapper archer that owns ONLY the one bronze trap perk
// under test (no Silver/Gold trap-modifier perks), so trapModifiersForUnitLocked
// returns the identity TrapModifiers{1.0, 1.0, 1.0, 1.0} and the planted
// Trap's fields equal the base (or per-rank) config read straight out of the
// catalog JSON — see server/internal/game/catalog/perks/trapper/<id>/<id>.json.
//
// Expected numbers below are transcribed directly from those JSON files:
//   - caltrops:       damagePerSecond 6, durationSeconds 12, radius 60, slowMultiplier 0.35
//   - explosive_trap:  burstDamage 75, durationSeconds 20, explosionRadius 100, triggerRadius 50
//   - marker_trap:    durationSeconds 12, markDuration 4, markMultiplier 0.2, radius 115
//   - fire_pit:       base {damagePerSecond 16, radius 55}, silver {28, 75}, gold {45, 95},
//     durationSeconds 10 (flat across ranks — not in configByRank)
func TestTrapCharacterization(t *testing.T) {
	const eps = 1e-6

	// spawnLoneTrapper builds a Trapper archer that owns exactly ONE trap
	// ABILITY (the bronze trap under test), at the given rank, with no
	// modifier perks — reusing the same spawnPlayerUnitLocked + grantTrapAbility
	// idiom as the rest of trap_test.go / silver_perks_test.go. Falls back to
	// "soldier" if archer isn't in the catalog, mirroring the existing helpers
	// in trap_test.go.
	spawnLoneTrapper := func(s *GameState, abilityID, rank string) *Unit {
		u := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
		if u == nil {
			u = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
		}
		u.Visible = true
		u.ProgressionPath = unitPathTrapper
		u.Rank = rank
		u.PerkIDs = nil   // ensure no modifier perks are present
		u.Abilities = nil // ensure no other trap ability is present
		grantTrapAbility(u, abilityID)
		return u
	}

	type wantTrap struct {
		trapType         string
		radius           float64
		triggerRadius    float64 // explosive_trap only
		damagePerSecond  float64 // caltrops, fire_pit
		slowMultiplier   float64 // caltrops only
		burstDamage      int     // explosive_trap only
		markMultiplier   float64 // marker_trap only
		markDuration     float64 // marker_trap only
		remainingSeconds float64
	}

	cases := []struct {
		name   string
		perkID string
		rank   string
		want   wantTrap
	}{
		{
			name:   "caltrops/bronze",
			perkID: "caltrops",
			rank:   unitRankBronze,
			want: wantTrap{
				trapType:         "caltrops",
				radius:           60,
				damagePerSecond:  6,
				slowMultiplier:   0.35,
				remainingSeconds: 12,
			},
		},
		{
			name:   "explosive_trap/bronze",
			perkID: "explosive_trap",
			rank:   unitRankBronze,
			want: wantTrap{
				trapType:         "explosive_trap",
				radius:           100, // explosionRadius
				triggerRadius:    50,
				burstDamage:      75,
				remainingSeconds: 20,
			},
		},
		{
			name:   "marker_trap/bronze",
			perkID: "marker_trap",
			rank:   unitRankBronze,
			want: wantTrap{
				trapType:         "marker_trap",
				radius:           115,
				markMultiplier:   0.2,
				markDuration:     4,
				remainingSeconds: 12,
			},
		},
		{
			name:   "fire_pit/bronze",
			perkID: "fire_pit",
			rank:   unitRankBronze,
			want: wantTrap{
				trapType:         "fire_pit",
				radius:           55,
				damagePerSecond:  16,
				remainingSeconds: 10,
			},
		},
		{
			name:   "fire_pit/silver",
			perkID: "fire_pit",
			rank:   unitRankSilver,
			want: wantTrap{
				trapType:         "fire_pit",
				radius:           75,
				damagePerSecond:  28,
				remainingSeconds: 10,
			},
		},
		{
			name:   "fire_pit/gold",
			perkID: "fire_pit",
			rank:   unitRankGold,
			want: wantTrap{
				trapType:         "fire_pit",
				radius:           95,
				damagePerSecond:  45,
				remainingSeconds: 10,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTrapState(t)
			s.mu.Lock()
			defer s.mu.Unlock()

			s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}

			unit := spawnLoneTrapper(s, tc.perkID, tc.rank)
			if len(unit.Abilities) != 1 || unit.Abilities[0] != tc.perkID {
				t.Fatalf("setup: unit should own exactly ability [%q], got %v", tc.perkID, unit.Abilities)
			}

			beforeCount := len(s.Traps)
			s.plantTrapLocked(unit, mustTrapAbilityConfig(t, tc.perkID, tc.rank))
			if len(s.Traps) != beforeCount+1 {
				t.Fatalf("plantTrapLocked: expected exactly 1 trap planted, got %d new traps",
					len(s.Traps)-beforeCount)
			}
			trap := s.Traps[len(s.Traps)-1]

			if trap.TrapType != tc.want.trapType {
				t.Errorf("TrapType: got %q, want %q", trap.TrapType, tc.want.trapType)
			}
			if math.Abs(trap.Radius-tc.want.radius) > eps {
				t.Errorf("Radius: got %.6f, want %.6f", trap.Radius, tc.want.radius)
			}
			if math.Abs(trap.RemainingSeconds-tc.want.remainingSeconds) > eps {
				t.Errorf("RemainingSeconds: got %.6f, want %.6f", trap.RemainingSeconds, tc.want.remainingSeconds)
			}

			switch tc.want.trapType {
			case "caltrops":
				if math.Abs(trap.DamagePerSecond-tc.want.damagePerSecond) > eps {
					t.Errorf("DamagePerSecond: got %.6f, want %.6f", trap.DamagePerSecond, tc.want.damagePerSecond)
				}
				if math.Abs(trap.SlowMultiplier-tc.want.slowMultiplier) > eps {
					t.Errorf("SlowMultiplier: got %.6f, want %.6f", trap.SlowMultiplier, tc.want.slowMultiplier)
				}
			case "fire_pit":
				if math.Abs(trap.DamagePerSecond-tc.want.damagePerSecond) > eps {
					t.Errorf("DamagePerSecond: got %.6f, want %.6f", trap.DamagePerSecond, tc.want.damagePerSecond)
				}
			case "explosive_trap":
				if math.Abs(trap.TriggerRadius-tc.want.triggerRadius) > eps {
					t.Errorf("TriggerRadius: got %.6f, want %.6f", trap.TriggerRadius, tc.want.triggerRadius)
				}
				if trap.BurstDamage != tc.want.burstDamage {
					t.Errorf("BurstDamage: got %d, want %d", trap.BurstDamage, tc.want.burstDamage)
				}
			case "marker_trap":
				if math.Abs(trap.MarkMultiplier-tc.want.markMultiplier) > eps {
					t.Errorf("MarkMultiplier: got %.6f, want %.6f", trap.MarkMultiplier, tc.want.markMultiplier)
				}
				if math.Abs(trap.MarkDuration-tc.want.markDuration) > eps {
					t.Errorf("MarkDuration: got %.6f, want %.6f", trap.MarkDuration, tc.want.markDuration)
				}
			}
		})
	}
}
