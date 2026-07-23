package game

// Section: per-hit damage event coverage.
//
// The client derives floating damage numbers from HP-diff, so two hits that
// land on one unit within a single snapshot collapse into one number equal to
// their sum. hit_damage_events.go restores the per-hit granularity: every hit
// that removes HP pushes a (UnitID, Damage) entry the client can use to split
// the popup back into individual numbers. These tests assert the invariants
// the client relies on — one entry per landed hit, and the entries reconcile
// exactly with the HP actually lost — without pinning any balance numbers.

import "testing"

// sumHitDamageForUnit totals the per-tick hit entries recorded for a unit.
func sumHitDamageForUnit(s *GameState, unitID int) (count, total int) {
	for _, e := range s.hitDamageEventsThisTick {
		if e.UnitID == unitID {
			count++
			total += e.Damage
		}
	}
	return count, total
}

// TestHitDamageEvents_TwoSimultaneousHits verifies that two separate hits on
// one target in the same tick produce two distinct per-hit entries whose sum
// equals the HP lost — exactly the signal the client needs to render "12" "12"
// instead of a combined "24".
func TestHitDamageEvents_TwoSimultaneousHits(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xB17)
	s.mu.Lock()
	defer s.mu.Unlock()

	target := &Unit{
		ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier",
		Visible: true, HP: 100, MaxHP: 100,
	}
	s.nextUnitID++
	s.addUnitLocked(target)

	s.resetHitDamageEventsThisTickLocked()
	startHP := target.HP

	// Two independent hits land this tick (e.g. two soldiers striking together).
	landed1 := s.applyUnitDamageWithSourceLocked(target, 12, DamageSource{})
	landed2 := s.applyUnitDamageWithSourceLocked(target, 12, DamageSource{})

	count, total := sumHitDamageForUnit(s, target.ID)
	if count != 2 {
		t.Fatalf("want 2 per-hit entries for two hits, got %d; queue=%+v", count, s.hitDamageEventsThisTick)
	}
	// Invariant: the per-hit entries must reconcile with the actual HP loss so
	// the client's exact-match split fires. Derive expected from the observed
	// HP delta rather than the input amounts (mitigation may reduce them).
	if hpLost := startHP - target.HP; total != hpLost {
		t.Errorf("per-hit sum %d != HP lost %d (landed %d + %d); client would fall back to combined number",
			total, hpLost, landed1, landed2)
	}
}

// TestHitDamageEvents_SuppressHitSplit verifies the opt-out: a damage instance
// flagged to combine its popup records NO per-hit entry, so N such hits on one
// unit in a tick collapse into ONE summed number (a stacking DoT like caltrops'
// Barbed reads as its total, not N split numbers). The damage still lands
// normally — only the split channel is suppressed.
func TestHitDamageEvents_SuppressHitSplit(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xB19)
	s.mu.Lock()
	defer s.mu.Unlock()

	target := &Unit{
		ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier",
		Visible: true, HP: 1000, MaxHP: 1000,
	}
	s.nextUnitID++
	s.addUnitLocked(target)

	s.resetHitDamageEventsThisTickLocked()
	startHP := target.HP

	// Three flagged hits land this tick (three Barbed stacks ticking together).
	landed := 0
	for i := 0; i < 3; i++ {
		landed += s.applyUnitDamageWithSourceLocked(target, 6, DamageSource{SuppressHitSplit: true})
	}

	if count, _ := sumHitDamageForUnit(s, target.ID); count != 0 {
		t.Fatalf("SuppressHitSplit must record NO per-hit entries, got %d; queue=%+v", count, s.hitDamageEventsThisTick)
	}
	if landed <= 0 {
		t.Fatal("flagged damage landed nothing")
	}
	if hpLost := startHP - target.HP; hpLost != landed {
		t.Fatalf("flagged damage did not land normally: HP loss %d != landed %d", hpLost, landed)
	}

	// Control: without the flag the split channel is still the default.
	s.resetHitDamageEventsThisTickLocked()
	s.applyUnitDamageWithSourceLocked(target, 6, DamageSource{})
	if count, _ := sumHitDamageForUnit(s, target.ID); count != 1 {
		t.Fatalf("an unflagged hit should record one per-hit entry, got %d", count)
	}
}

// TestHitDamageEvents_FullyMitigatedHitEmitsNothing guards the lower edge: a
// hit that removes no HP (fully absorbed / mitigated to zero) must not emit a
// phantom per-hit entry, or the client would try to split a popup that never
// appeared.
func TestHitDamageEvents_FullyMitigatedHitEmitsNothing(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xB18)
	s.mu.Lock()
	defer s.mu.Unlock()

	target := &Unit{
		ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier",
		Visible: true, HP: 100, MaxHP: 100,
		Shield: 50, // absorbs the whole hit → no HP loss
	}
	s.nextUnitID++
	s.addUnitLocked(target)

	s.resetHitDamageEventsThisTickLocked()
	s.applyUnitDamageWithSourceLocked(target, 40, DamageSource{})

	if count, _ := sumHitDamageForUnit(s, target.ID); count != 0 {
		t.Errorf("shield-absorbed hit (no HP loss) should emit no per-hit entry, got %d; queue=%+v",
			count, s.hitDamageEventsThisTick)
	}
}
