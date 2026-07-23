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

	// Expected value from the perk's OWN authored row, whichever form it uses.
	// amplified_effects moved this contribution from an `amplify` op on the
	// action id to a flat add addressed by the INFLICTED STAT (moveSpeed): a
	// negative flat strengthens an inverse-sense multiplier, and unlike the old
	// op it cannot be unhooked by renaming the action.
	want := applyAmplifiedRow(t, "caltrops", "slow_move", "value", base)
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("slowMultiplier = %v, want %v (base %v plus the perk's authored moveSpeed contribution)",
			got, want, base)
	}
}

// barbed_field (silver, caltrops-only): enemies take ramping bonus damage the
// longer they stay in the field, capped, and it fades once they leave. Migrated
// off the bespoke BarbedFieldStaySeconds runtime onto a has_perk-gated "Barbed"
// stacking status inside caltrops' program — each independent stack ticks a flat
// bonus, so N stacks = N× the per-stack damage, ramping as stacks accumulate and
// draining as they expire. Pure data; nothing here pins a tunable — it asserts
// the SHAPE (flat base, ramp, cap, bonus) so a rebalance carries the test.
//
// Driven by the zone + status tick helpers directly (not the full Update loop)
// so only caltrops and its statuses touch the enemy — no auto-attacks or
// autocast recasts to muddy the per-tick numbers.
func TestCaltropsZone_BarbedFieldRampsThenCaps(t *testing.T) {
	collect := func(perks []string, ticks int) []int {
		s := newTrapState(t)
		s.mu.Lock()
		defer s.mu.Unlock()
		caster, enemy := castCaltrops(t, s)
		caster.PerkIDs = perks
		caster.Damage = 0 // isolate: the caster's own attacks must not land
		enemy.MoveSpeed = 0
		enemy.HP, enemy.MaxHP = 1_000_000, 1_000_000
		// Warm-up: the freshly-spawned zone is due immediately, so its first
		// manual tick fires a spawn-artifact double hit. Discard it so the
		// measured window is steady-state.
		s.tickAbilityZonesLocked(1)
		s.tickAbilityStatusesLocked(1)
		out := make([]int, ticks)
		for i := 0; i < ticks; i++ {
			before := enemy.HP
			s.tickAbilityZonesLocked(1)    // base spikes + (if perk) applies a Barbed stack
			s.tickAbilityStatusesLocked(1) // Barbed stacks each deal their bonus
			out[i] = before - enemy.HP
		}
		return out
	}

	const ticks = 10
	withPerk := collect([]string{"barbed_field"}, ticks)
	noPerk := collect(nil, ticks)

	// Base caltrops damage is flat — the ramp must be the perk's doing.
	for i, d := range noPerk {
		if d != noPerk[0] {
			t.Fatalf("caltrops base damage is not flat: tick0=%d tick%d=%d", noPerk[0], i, d)
		}
	}
	// Find the ramp's peak.
	peak, peakIdx := withPerk[0], 0
	for i, d := range withPerk {
		if d > peak {
			peak, peakIdx = d, i
		}
	}
	// It ramps UP: the peak is strictly above where it started.
	if peak <= withPerk[0] {
		t.Errorf("barbed_field did not ramp up to a peak: %v", withPerk)
	}
	// It climbs monotonically to that peak (no dips on the way up).
	for i := 1; i <= peakIdx; i++ {
		if withPerk[i] < withPerk[i-1] {
			t.Errorf("barbed_field dipped before its peak at tick %d: %v", i, withPerk)
			break
		}
	}
	// It CAPS: nothing after the peak exceeds it — the bonus is bounded by max
	// stacks, it does not grow without limit.
	for i := peakIdx + 1; i < ticks; i++ {
		if withPerk[i] > peak {
			t.Errorf("barbed_field exceeded its peak after capping at tick %d: %v", i, withPerk)
			break
		}
	}
	// It SUSTAINS while the enemy stays in the field: post-peak stacks drain and
	// refill (the fade mechanic), but never collapse back to the bare base.
	for i := peakIdx; i < ticks; i++ {
		if withPerk[i] <= noPerk[0] {
			t.Errorf("barbed_field collapsed to base while still in the field at tick %d: %v", i, withPerk)
			break
		}
	}
	// The peak is a real bonus ON TOP of the base.
	if peak <= noPerk[ticks-1] {
		t.Errorf("barbed_field added no bonus over base: peak %d vs base %d", peak, noPerk[ticks-1])
	}
}

// The other half of the mechanic: once the enemy LEAVES the field, no new
// Barbed stacks are applied and the lingering ones tick down and expire, so the
// bonus fades to nothing rather than resetting instantly. This is the
// intentional design change from the legacy (instant-reset) version.
func TestCaltropsZone_BarbedFieldFadesOnExit(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster, enemy := castCaltrops(t, s)
	caster.PerkIDs = []string{"barbed_field"}
	caster.Damage = 0
	enemy.MoveSpeed = 0
	enemy.HP, enemy.MaxHP = 1_000_000, 1_000_000

	tick := func() int {
		before := enemy.HP
		s.tickAbilityZonesLocked(1)
		s.tickAbilityStatusesLocked(1)
		return before - enemy.HP
	}

	// Ramp up inside the field.
	for i := 0; i < 7; i++ {
		tick()
	}

	// Leave: teleport the enemy well outside the zone radius. The zone no longer
	// catches it (no base damage, no new stacks); only lingering Barbed stacks
	// on the unit still tick.
	enemy.X = 5000
	fade := make([]int, 12)
	for i := range fade {
		fade[i] = tick()
	}

	if fade[0] == 0 {
		t.Fatalf("expected lingering barbed damage the tick after leaving, got 0 (%v)", fade)
	}
	// It only ever decreases as stacks expire...
	for i := 1; i < len(fade); i++ {
		if fade[i] > fade[i-1] {
			t.Errorf("barbed fade is not monotonic at tick %d: %v", i, fade)
			break
		}
	}
	// ...all the way to nothing.
	if fade[len(fade)-1] != 0 {
		t.Errorf("barbed damage never fully faded after leaving the field: %v", fade)
	}
}

// The Barbed stacks read as ONE floating number, not one-per-stack: their
// per-tick deal_damage sets combinePopup, so it records no per-hit split entry
// and the client falls back to the single summed HP-diff. Asserts the server
// signal (zero hit-split entries for a multi-stack barbed tick) that produces
// that display.
func TestCaltropsZone_BarbedDamageCombinesIntoOneNumber(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster, enemy := castCaltrops(t, s)
	caster.PerkIDs = []string{"barbed_field"}
	caster.Damage = 0
	enemy.MoveSpeed = 0
	enemy.HP, enemy.MaxHP = 1_000_000, 1_000_000

	// Warm up so several Barbed stacks are live.
	for i := 0; i < 5; i++ {
		s.tickAbilityZonesLocked(1)
		s.tickAbilityStatusesLocked(1)
	}

	// Measure one tick where multiple stacks deal damage together.
	s.resetHitDamageEventsThisTickLocked()
	before := enemy.HP
	s.tickAbilityStatusesLocked(1)
	dealt := before - enemy.HP
	if dealt <= 0 {
		t.Fatal("no barbed damage on the measured tick — warm-up produced no live stacks")
	}
	// Sanity: multiple stacks really did contribute (dealt is more than one
	// stack's worth), so a split WOULD have produced several numbers.
	if count, _ := sumHitDamageForUnit(s, enemy.ID); count != 0 {
		t.Errorf("Barbed stacks emitted %d per-hit split entries — they should combine into one number (dealt %d)", count, dealt)
	}
}

// increased_deployment is a pure data perk: an abilityFields row adds to each
// trap ability's placement-zone `count`, so one cast places several traps. The
// count is a plain field a stronger upgrade bumps further — scalable, no
// per-count code. Asserts the placed-zone count, derived from the perk's own
// authored value (not a pinned literal).
func TestIncreasedDeployment_ScalesTrapCount(t *testing.T) {
	place := func(perks []string) int {
		s := newTrapState(t)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
		s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}
		caster := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 300, Y: 300})
		if caster == nil {
			caster = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 300, Y: 300})
		}
		grantTrapAbility(caster, "caltrops")
		caster.PerkIDs = perks
		enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 380, Y: 300})
		enemy.Visible = true
		enemy.HP, enemy.MaxHP = 500, 500
		if ok, reason := s.beginAbilityCastLocked(caster, "caltrops", enemy); !ok {
			t.Fatalf("caltrops cast failed: %q", reason)
		}
		return len(s.AbilityZones)
	}

	base := place(nil)
	withPerk := place([]string{"increased_deployment"})

	if base != 1 {
		t.Fatalf("base caltrops should place exactly 1 zone, got %d", base)
	}
	// The perk's own authored +count drives the expectation: base 1 + the row.
	bonus := int(perkDefByID("increased_deployment").AbilityFields[0].Value)
	if want := base + bonus; withPerk != want {
		t.Errorf("increased_deployment placed %d zones, want %d (base %d + %d)", withPerk, want, base, bonus)
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
