package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// healDef returns the catalog-authored Heal ability. Tests derive expected
// HP / mana deltas from this rather than hardcoding numbers, so tuning
// catalog/abilities/heal/heal.json never breaks a behavioral test — only a
// genuine regression in the cast logic does.
func healDef(t *testing.T) AbilityDef {
	t.Helper()
	def, ok := getAbilityDef("heal")
	if !ok {
		t.Fatal(`getAbilityDef("heal") = _, false; want the catalog-authored Heal`)
	}
	return def
}

// healSetup spawns an apprentice (p1) and a damaged friendly soldier within
// the apprentice's cast range. The ally is left missing strictly more HP than
// a single heal restores (derived from the catalog), so exact-heal assertions
// never collide with the no-overheal clamp regardless of how healAmount is
// tuned in JSON. Lock is NOT held on return.
func healSetup(t *testing.T) (s *GameState, app, ally *Unit) {
	t.Helper()
	s = newProjectileTestState(t)
	def := healDef(t)
	s.mu.Lock()
	app = s.spawnPlayerUnitLocked("apprentice", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	app.Visible = true
	// Catalog seeds heal auto-cast ON for player units at spawn
	// (heal.json → defaultAutoCast: true). The tests using this helper want
	// to exercise the MANUAL cast path; auto-cast firing during their tick
	// advance would mutate ally HP behind the assertions. Clear so each test
	// runs from a known baseline. Tests that care about the seeded default
	// read AutoCastEnabled before calling helpers like this.
	if app.AutoCastEnabled != nil {
		delete(app.AutoCastEnabled, "heal")
	}
	ally = spawnProjTestUnit(t, s, "p1", 450, 400)  // 50px away, within 220 range
	ally.HP = ally.MaxHP - def.HealAmount - 20       // missing > one heal, so +HealAmount never clips MaxHP
	s.mu.Unlock()
	return s, app, ally
}

func advance(s *GameState, ticks int) {
	for i := 0; i < ticks; i++ {
		s.Update(0.05)
	}
}

// ── Heal restores correct HP, deducts mana, plays healing_glow ───────────────

func TestHeal_RestoresHPAndDeductsMana(t *testing.T) {
	s, app, ally := healSetup(t)
	def := healDef(t)

	s.mu.Lock()
	allyID := ally.ID
	wantHP := ally.HP + def.HealAmount
	startMana := app.CurrentMana
	wantMana := startMana - def.ManaCost
	ok, reason := s.beginAbilityCastLocked(app, "heal", ally)
	s.mu.Unlock()
	if !ok {
		t.Fatalf("beginAbilityCastLocked failed: %q", reason)
	}

	advance(s, 25) // past the full cast time

	s.mu.RLock()
	defer s.mu.RUnlock()
	a := s.unitsByID[allyID]
	if a.HP != wantHP {
		t.Errorf("ally HP = %d; want %d (healed +%d from catalog)", a.HP, wantHP, def.HealAmount)
	}
	if app.CurrentMana != wantMana {
		t.Errorf("caster mana = %d; want %d (%d start - %d manaCost, deducted on completion)", app.CurrentMana, wantMana, startMana, def.ManaCost)
	}
	if app.CastAbilityID != "" || app.Casting {
		t.Errorf("cast should be cleared after completion: CastAbilityID=%q Casting=%v", app.CastAbilityID, app.Casting)
	}
	if queuedEffectFor(s, "healing_glow", allyID) == nil {
		t.Error("healing_glow effect should have played on the heal target")
	}
}

func TestHeal_CannotOverheal(t *testing.T) {
	s, app, ally := healSetup(t)
	s.mu.Lock()
	ally.HP = ally.MaxHP - 1 // missing < any positive heal, so the heal must clamp at MaxHP
	allyID := ally.ID
	ok, _ := s.beginAbilityCastLocked(app, "heal", ally)
	s.mu.Unlock()
	if !ok {
		t.Fatal("cast should start")
	}
	advance(s, 25)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if a := s.unitsByID[allyID]; a.HP != a.MaxHP {
		t.Errorf("ally HP = %d; want MaxHP %d (no overheal beyond max)", a.HP, a.MaxHP)
	}
}

// ── Synchronous initiation failures (graceful, no cast started) ───────────────

func TestHeal_InsufficientManaFailsGracefully(t *testing.T) {
	s, app, ally := healSetup(t)
	def := healDef(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	app.CurrentMana = def.ManaCost - 1 // one short of the catalog mana cost
	ok, reason := s.beginAbilityCastLocked(app, "heal", ally)
	if ok || reason != castFailNotEnoughMana {
		t.Errorf("expected (false, %q); got (%v, %q)", castFailNotEnoughMana, ok, reason)
	}
	if app.CastAbilityID != "" || app.Casting {
		t.Error("no cast should have started on insufficient mana")
	}
	if app.CurrentMana != def.ManaCost-1 {
		t.Errorf("mana must be untouched on failed initiation, got %d want %d", app.CurrentMana, def.ManaCost-1)
	}
	if app.LastCastFailure != castFailNotEnoughMana {
		t.Errorf("LastCastFailure = %q; want %q", app.LastCastFailure, castFailNotEnoughMana)
	}
}

func TestHeal_OutOfRangeFailsGracefully(t *testing.T) {
	s, app, _ := healSetup(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	far := spawnProjTestUnit(t, s, "p1", 400+500, 400) // 500px > 220 range
	ok, reason := s.beginAbilityCastLocked(app, "heal", far)
	if ok || reason != castFailOutOfRange {
		t.Errorf("expected (false, %q); got (%v, %q)", castFailOutOfRange, ok, reason)
	}
}

func TestHeal_CannotTargetEnemy(t *testing.T) {
	s, app, _ := healSetup(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	enemy := spawnProjTestUnit(t, s, enemyPlayerID, 450, 400) // in range but hostile
	ok, reason := s.beginAbilityCastLocked(app, "heal", enemy)
	if ok || reason != castFailInvalidTarget {
		t.Errorf("heal must not target an enemy: got (%v, %q)", ok, reason)
	}
}

func TestHeal_CanTargetSelf(t *testing.T) {
	s, app, _ := healSetup(t)
	s.mu.Lock()
	app.HP = app.MaxHP - 1 // missing < any positive heal, so self-heal clamps to MaxHP
	appID := app.ID
	ok, reason := s.beginAbilityCastLocked(app, "heal", app)
	s.mu.Unlock()
	if !ok {
		t.Fatalf("heal should be castable on self: %q", reason)
	}
	advance(s, 25)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if a := s.unitsByID[appID]; a.HP != a.MaxHP {
		t.Errorf("self-heal: HP = %d; want MaxHP %d (was MaxHP-1, heal clamps to max)", a.HP, a.MaxHP)
	}
}

// ── Cast time + caster lock ──────────────────────────────────────────────────

func TestHeal_CastTimePreventsInstantResolution(t *testing.T) {
	s, app, ally := healSetup(t)
	s.mu.Lock()
	allyID := ally.ID
	startHP := ally.HP
	ok, _ := s.beginAbilityCastLocked(app, "heal", ally)
	// Immediately after initiation: nothing resolved yet.
	if !ok || app.CastAbilityID != "heal" || !app.Casting {
		t.Fatalf("cast should be in progress: ok=%v id=%q casting=%v", ok, app.CastAbilityID, app.Casting)
	}
	s.mu.Unlock()

	advance(s, 10) // 0.5s — still mid-cast (cast time is 1.0s)
	s.mu.RLock()
	if s.unitsByID[allyID].HP != startHP {
		t.Errorf("heal resolved before cast time elapsed (HP changed at 0.5s)")
	}
	if !app.Casting || app.CastAbilityID != "heal" {
		t.Error("caster should still be mid-cast at 0.5s")
	}
	s.mu.RUnlock()

	advance(s, 20) // past 1.0s total
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.unitsByID[allyID].HP == startHP {
		t.Error("heal should have resolved after the full cast time")
	}
}

func TestHeal_CasterLockedDuringCast(t *testing.T) {
	s, app, ally := healSetup(t)
	s.mu.Lock()
	// An enemy sits in the apprentice's attack range: normally it would shoot.
	enemy := spawnProjTestUnit(t, s, enemyPlayerID, 400+120, 400)
	enemy.MoveSpeed = 0
	appID := app.ID
	ok, _ := s.beginAbilityCastLocked(app, "heal", ally)
	s.mu.Unlock()
	if !ok {
		t.Fatal("cast should start")
	}

	// Mid-cast: caster is animation-locked to "Casting" and does NOT attack
	// (no projectile from it) despite an enemy in range.
	advance(s, 10) // 0.5s, still casting
	s.mu.RLock()
	a := s.unitsByID[appID]
	if !a.Casting || a.Status != unitStatusCasting {
		t.Errorf("caster not locked: Casting=%v Status=%q (want true / %q)", a.Casting, a.Status, unitStatusCasting)
	}
	for _, p := range s.Projectiles {
		if p.OwnerUnitID == appID {
			t.Error("caster fired a projectile while casting — it must be locked (cannot attack)")
		}
	}
	s.mu.RUnlock()
}

// ── Async cancellation: target dies mid-cast (graceful, no cost) ─────────────

func TestHeal_TargetDiesMidCastCancelsGracefully(t *testing.T) {
	s, app, ally := healSetup(t)
	s.mu.Lock()
	allyID := ally.ID
	startMana := app.CurrentMana
	ok, _ := s.beginAbilityCastLocked(app, "heal", ally)
	s.mu.Unlock()
	if !ok {
		t.Fatal("cast should start")
	}

	advance(s, 5) // partway through the cast
	s.mu.Lock()
	s.unitsByID[allyID].HP = 0 // target dies mid-cast
	s.mu.Unlock()

	advance(s, 10)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if app.CastAbilityID != "" || app.Casting {
		t.Error("cast should be cancelled when the target dies mid-cast")
	}
	if app.LastCastFailure != castFailTargetLost {
		t.Errorf("LastCastFailure = %q; want %q", app.LastCastFailure, castFailTargetLost)
	}
	if app.CurrentMana != startMana {
		t.Errorf("a cancelled cast must not spend mana; mana = %d, want %d (unchanged)", app.CurrentMana, startMana)
	}
}

// ── Decision: cast is uninterruptible (damage does NOT cancel it) ────────────

func TestHeal_UninterruptibleByDamage(t *testing.T) {
	s, app, ally := healSetup(t)
	def := healDef(t)
	s.mu.Lock()
	allyID := ally.ID
	wantHP := ally.HP + def.HealAmount
	startMana := app.CurrentMana
	wantMana := startMana - def.ManaCost
	ok, _ := s.beginAbilityCastLocked(app, "heal", ally)
	s.mu.Unlock()
	if !ok {
		t.Fatal("cast should start")
	}

	advance(s, 5)
	s.mu.Lock()
	// Hit the caster mid-cast — per the design decision this must NOT cancel.
	s.applyUnitDamageWithSourceLocked(app, 10, DamageSource{Kind: "test"})
	stillCasting := app.Casting && app.CastAbilityID == "heal"
	s.mu.Unlock()
	if !stillCasting {
		t.Fatal("taking damage must not cancel the cast (uninterruptible)")
	}

	advance(s, 25)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if a := s.unitsByID[allyID]; a.HP != wantHP {
		t.Errorf("cast should have completed despite damage: ally HP = %d, want %d", a.HP, wantHP)
	}
	if app.CurrentMana != wantMana {
		t.Errorf("completed cast should spend mana: mana = %d, want %d (%d - %d manaCost)", app.CurrentMana, wantMana, startMana, def.ManaCost)
	}
}
