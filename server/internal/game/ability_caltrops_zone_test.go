package game

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────────────────────
// CALTROPS — the second trap migrated off the bespoke trap runtime onto a
// composable visible zone. Replaces the legacy TestCaltrops_* trap-entity tests
// and the caltrops row of TestTrapCharacterization.
//
// Its distinguishing feature vs fire_pit is the SLOW, which is what forced the
// statOpAmplify operation into existence: slowMultiplier is inverse-sense
// (lower = stronger), so amplified_effects cannot scale it with a plain
// multiply without making the slow WEAKER.
// ─────────────────────────────────────────────────────────────────────────────

func castCaltrops(t *testing.T, s *GameState) (caster, enemy *Unit) {
	t.Helper()
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}

	caster = s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 300, Y: 300})
	if caster == nil {
		caster = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 300, Y: 300})
	}
	grantTrapAbility(caster, "caltrops")

	enemy = s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 380, Y: 300})
	if enemy == nil {
		t.Fatal("enemy spawn failed")
	}
	enemy.Visible = true
	enemy.HP, enemy.MaxHP = 500, 500

	ok, reason := s.beginAbilityCastLocked(caster, "caltrops", enemy)
	if !ok {
		t.Fatalf("beginAbilityCastLocked(caltrops) failed: %q", reason)
	}
	return caster, enemy
}

// TestCaltropsZone_DamagesAndSlowsEnemiesNotAllies replaces the legacy
// TestCaltrops_SlowsAndDamagesEnemy / _AllyInZoneUnaffected /
// _PersistsAcrossMultipleEnemies trio.
func TestCaltropsZone_DamagesAndSlowsEnemiesNotAllies(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	_, enemy := castCaltrops(t, s)

	if len(s.AbilityZones) != 1 {
		t.Fatalf("AbilityZones = %d, want 1", len(s.AbilityZones))
	}
	if got := s.AbilityZones[0].Sprite; got != "caltrops" {
		t.Errorf("zone sprite = %q, want %q (must be visible)", got, "caltrops")
	}

	ally := s.spawnPlayerUnitLocked("soldier", "p1", "#2ecc71", protocol.Vec2{X: enemy.X, Y: enemy.Y})
	if ally == nil {
		t.Fatal("ally spawn failed")
	}
	ally.Visible = true
	ally.HP, ally.MaxHP = 500, 500

	enemyBefore, allyBefore := enemy.HP, ally.HP
	s.tickAbilityZonesLocked(1)

	if enemy.HP >= enemyBefore {
		t.Errorf("enemy in the field took no damage (HP %d -> %d)", enemyBefore, enemy.HP)
	}
	if ally.HP != allyBefore {
		t.Errorf("ally took damage (HP %d -> %d); traps never hit friendlies", allyBefore, ally.HP)
	}
	// The slow is delivered as a status carrying a moveSpeed change_stat.
	slowed := false
	for _, st := range s.AbilityStatuses {
		if st != nil && st.TargetUnitID == enemy.ID {
			slowed = true
		}
	}
	if !slowed {
		t.Error("enemy in the caltrops field carries no slow status")
	}
	for _, st := range s.AbilityStatuses {
		if st != nil && st.TargetUnitID == ally.ID {
			t.Error("ally was slowed by a friendly caltrops field")
		}
	}
}

// TestCaltropsZone_ModifierPerksReachIt mirrors the fire_pit coverage: the
// global Silver perks still change the field after it left the legacy trap
// aggregator.
func TestCaltropsZone_ModifierPerksReachIt(t *testing.T) {
	cases := []struct{ perkID, param string }{
		{"extended_setup", "duration"},
		{"wider_nets", "radius"},
		{"amplified_effects", "dps"},
	}
	for _, tc := range cases {
		t.Run(tc.perkID+" scales "+tc.param, func(t *testing.T) {
			s := newTrapState(t)
			s.mu.Lock()
			defer s.mu.Unlock()

			caster, _ := castCaltrops(t, s)
			base := effTrapField(t, s, caster, "caltrops", tc.param)

			caster.PerkIDs = []string{tc.perkID}
			got := effTrapField(t, s, caster, "caltrops", tc.param)

			if got == base {
				t.Fatalf("%s had NO effect on the migrated field's %s", tc.perkID, tc.param)
			}
			if got <= base {
				t.Errorf("%s should increase %s: %v -> %v", tc.perkID, tc.param, base, got)
			}
		})
	}
}

// TestCaltropsZone_AmplifiedEffectsStrengthensTheSlow is the reason
// statOpAmplify exists. slowMultiplier is INVERSE-SENSE: 0.35 means "slowed to
// 35% speed", so a stronger slow is a LOWER number. A plain multiply of 1.35
// would raise it to 0.4725 — a WEAKER slow than the base, while the perk
// advertises stronger slows.
//
// The expected value is derived from the legacy amplifySlow helper, which is
// the shipped definition of "amplify a slow", so this also pins that the data
// op reproduces the old Go math exactly.
func TestCaltropsZone_AmplifiedEffectsStrengthensTheSlow(t *testing.T) {

	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster, _ := castCaltrops(t, s)
	base := effTrapField(t, s, caster, "caltrops", "slowMultiplier")

	caster.PerkIDs = []string{"amplified_effects"}
	got := effTrapField(t, s, caster, "caltrops", "slowMultiplier")

	if got >= base {
		t.Fatalf("amplified_effects made the slow WEAKER: %v -> %v (lower is stronger for an inverse-sense multiplier)", base, got)
	}

	effectMult := perkDefByID("amplified_effects").Config["effectMultiplier"]
	want := amplifySlow(base, effectMult)
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("slowMultiplier = %v, want %v (amplifySlow(%v, %v) — the shipped definition of amplifying a slow)",
			got, want, base, effectMult)
	}
}

// TestAmplifyTowardZero covers the op itself, independent of any trap.
func TestAmplifyTowardZero(t *testing.T) {
	cases := []struct{ value, factor, want float64 }{
		{0.35, 1.35, 0.1225}, // the caltrops case
		{0.7, 1.0, 0.7},      // identity factor changes nothing
		{1.0, 2.0, 1.0},      // nothing to amplify at/above 1
		{1.5, 2.0, 1.5},      // above 1 returned unchanged
		{0.5, 3.0, 0.0},      // clamps at a full reduction
	}
	for _, c := range cases {
		if got := amplifyTowardZero(c.value, c.factor); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("amplifyTowardZero(%v, %v) = %v, want %v", c.value, c.factor, got, c.want)
		}
	}
	// Must agree with the shipped slow-amplification helper it generalizes.
	if got, want := amplifyTowardZero(0.35, 1.35), amplifySlow(0.35, 1.35); math.Abs(got-want) > 1e-9 {
		t.Errorf("amplifyTowardZero disagrees with amplifySlow: %v vs %v", got, want)
	}
}
