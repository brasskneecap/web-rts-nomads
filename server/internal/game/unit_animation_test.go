package game

import "testing"

// Animation slots are client-derived from unit.Status (see unitAnimation.ts
// pickAnimation, typechecked via vue-tsc — there is no client test runner).
// These tests cover the server side of the contract the spec lists: the
// status the client maps to each slot triggers automatically and the
// "Casting" slot never conflicts with "Attacking"/"Idle".

// ── Attacking triggers on basic attack (existing slot — verified) ────────────

func TestAnimation_AttackingTriggersOnBasicAttack(t *testing.T) {
	s, unit := newOrderTestState(t)

	s.mu.Lock()
	// Hostile enemy well within AttackRange (80) so the unit engages.
	spawnOrderEnemy(t, s, unit.X+50, unit.Y)
	unitID := unit.ID
	s.mu.Unlock()

	tickN(s, 10)

	s.mu.RLock()
	defer s.mu.RUnlock()
	u := s.unitsByID[unitID]
	if u.Status != unitStatusAttacking {
		t.Errorf("basic attack should drive Status=%q, got %q", unitStatusAttacking, u.Status)
	}
	if !u.Attacking {
		t.Error("unit.Attacking should be true while attacking")
	}
	if u.Casting {
		t.Error("a basic attack must NOT set the casting slot")
	}
}

// ── Casting triggers on spell cast (the Part-5 primitive) ────────────────────

func TestAnimation_CastingTriggersOnSpellCast(t *testing.T) {
	s, unit := newOrderTestState(t)

	s.mu.Lock()
	defer s.mu.Unlock()
	if unit.Casting {
		t.Fatal("fresh unit should not be casting")
	}

	s.beginUnitCastingLocked(unit)

	if !unit.Casting {
		t.Error("beginUnitCastingLocked should set Casting=true")
	}
	if unit.Status != unitStatusCasting {
		t.Errorf("casting Status should be %q, got %q", unitStatusCasting, unit.Status)
	}
	if unit.Attacking {
		t.Error("casting must clear Attacking (the two slots are mutually exclusive)")
	}
}

// ── Casting interrupts idle ──────────────────────────────────────────────────

func TestAnimation_CastingInterruptsIdle(t *testing.T) {
	s, unit := newOrderTestState(t)
	unitID := unit.ID

	// No enemies → the unit settles to Idle.
	tickN(s, 5)
	s.mu.Lock()
	u := s.unitsByID[unitID]
	if u.Status != unitStatusIdle {
		s.mu.Unlock()
		t.Fatalf("precondition: unit should be Idle, got %q", u.Status)
	}
	s.beginUnitCastingLocked(u)
	gotStatus := u.Status
	gotCasting := u.Casting
	s.mu.Unlock()

	if gotStatus != unitStatusCasting || !gotCasting {
		t.Errorf("casting should interrupt Idle: Status=%q Casting=%v; want %q / true", gotStatus, gotCasting, unitStatusCasting)
	}
}

// ── Casting does not conflict with the per-tick combat writer ────────────────

func TestAnimation_CastingDoesNotConflictWithCombat(t *testing.T) {
	s, unit := newOrderTestState(t)

	s.mu.Lock()
	enemy := spawnOrderEnemy(t, s, unit.X+50, unit.Y) // in AttackRange
	unitID, enemyID := unit.ID, enemy.ID
	enemyStartHP := enemy.HP
	s.beginUnitCastingLocked(unit)
	s.mu.Unlock()

	// Tick with an enemy parked in range. combatAI runs before unitCombat each
	// tick; the cast-lock guard must still win so the end-of-tick status the
	// client sees is "Casting" and the unit deals no damage while casting.
	tickN(s, 20)

	s.mu.RLock()
	u := s.unitsByID[unitID]
	e := s.unitsByID[enemyID]
	if u.Status != unitStatusCasting {
		t.Errorf("status should stay %q through combat ticks while casting, got %q", unitStatusCasting, u.Status)
	}
	if u.Attacking {
		t.Error("a casting unit must not also be Attacking")
	}
	if e != nil && e.HP != enemyStartHP {
		t.Errorf("casting unit must not deal damage: enemy HP %d → %d", enemyStartHP, e.HP)
	}
	s.mu.RUnlock()

	// Ending the cast lets combat resume cleanly — the slot transitions
	// Casting → Attacking without getting stuck.
	s.mu.Lock()
	s.endUnitCastingLocked(u)
	if u.Casting {
		s.mu.Unlock()
		t.Fatal("endUnitCastingLocked should clear Casting")
	}
	s.mu.Unlock()

	tickN(s, 10)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if u.Status != unitStatusAttacking {
		t.Errorf("after cast ends, combat should resume (Status=%q), got %q", unitStatusAttacking, u.Status)
	}
	if u.Casting {
		t.Error("Casting should remain false after the cast ended")
	}
}

// ── Casting and Attacking are mutually exclusive ─────────────────────────────

func TestAnimation_CastingAndAttackingMutuallyExclusive(t *testing.T) {
	s, unit := newOrderTestState(t)

	s.mu.Lock()
	spawnOrderEnemy(t, s, unit.X+50, unit.Y)
	unitID := unit.ID
	s.mu.Unlock()

	// Engage → Attacking, not Casting.
	tickN(s, 8)
	s.mu.Lock()
	u := s.unitsByID[unitID]
	if !u.Attacking || u.Casting {
		s.mu.Unlock()
		t.Fatalf("precondition: expected Attacking && !Casting, got Attacking=%v Casting=%v", u.Attacking, u.Casting)
	}
	// Begin casting mid-combat → flips to Casting, clears Attacking.
	s.beginUnitCastingLocked(u)
	if !u.Casting || u.Attacking {
		s.mu.Unlock()
		t.Fatalf("beginUnitCasting should give Casting && !Attacking, got Casting=%v Attacking=%v", u.Casting, u.Attacking)
	}
	s.mu.Unlock()

	// They never co-occur across subsequent combat ticks either.
	tickN(s, 15)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if u.Casting && u.Attacking {
		t.Error("Casting and Attacking must never both be true")
	}
	if u.Casting && u.Status != unitStatusCasting {
		t.Errorf("while Casting, Status must be %q, got %q", unitStatusCasting, u.Status)
	}
}
