package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// apprenticeMaxMana returns the apprentice's catalog-authored mana pool size
// (catalog/units/human/apprentice/apprentice.json → maxMana). Auto-cast tests
// derive expected post-cast mana from this rather than hardcoding 50.
func apprenticeMaxMana(t *testing.T) int {
	t.Helper()
	def, ok := getUnitDef("apprentice")
	if !ok {
		t.Fatal(`getUnitDef("apprentice") = _, false; want the catalog-authored apprentice`)
	}
	return def.MaxMana
}

// autoCastSetup: an apprentice (p1, has the auto-cast-capable "heal") and a
// friendly soldier damaged by `missing` HP, within the apprentice's cast
// range. Lock NOT held on return.
//
// NOTE: the catalog now seeds heal auto-cast ON by default at spawn (per
// heal.json's defaultAutoCast: true). Toggle-flow tests below were written
// against the legacy "off by default" baseline, so we explicitly disable
// auto-cast here to preserve their semantics. Tests that want to assert
// default-on behaviour read AutoCastEnabled before this helper clears it.
func autoCastSetup(t *testing.T, missing int) (s *GameState, app, ally *Unit) {
	t.Helper()
	s = newProjectileTestState(t)
	s.mu.Lock()
	app = s.spawnPlayerUnitLocked("apprentice", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	app.Visible = true
	if app.AutoCastEnabled != nil {
		delete(app.AutoCastEnabled, "heal")
	}
	ally = spawnProjTestUnit(t, s, "p1", 460, 400)
	ally.HP = ally.MaxHP - missing
	s.mu.Unlock()
	return s, app, ally
}

// ── Toggle on/off; ownership; non-auto-cast / unknown ability is a no-op ──────

func TestAutoCast_ToggleOnOff(t *testing.T) {
	s, app, _ := autoCastSetup(t, 20)
	s.mu.Lock()
	defer s.mu.Unlock()

	en, ch := s.toggleAutoCastLocked(app, "heal")
	if !en || !ch {
		t.Fatalf("first toggle: enabled=%v changed=%v; want true/true", en, ch)
	}
	if !s.autoCastEnabledLocked(app, "heal") {
		t.Error("auto-cast should now be enabled for heal")
	}
	en, ch = s.toggleAutoCastLocked(app, "heal")
	if en || !ch {
		t.Errorf("second toggle: enabled=%v changed=%v; want false/true", en, ch)
	}

	// Ability the unit doesn't have / unknown id → no effect, no state.
	en, ch = s.toggleAutoCastLocked(app, "not_an_ability")
	if en || ch {
		t.Errorf("toggling an ability the unit lacks must be a no-op; got %v/%v", en, ch)
	}
	if _, present := app.AutoCastEnabled["not_an_ability"]; present {
		t.Error("no-op toggle must not create a state entry")
	}
}

func TestAutoCast_ToggleOwnershipAndNoEffectCases(t *testing.T) {
	s, app, _ := autoCastSetup(t, 20)

	// Public path: wrong owner → silent no-op.
	if en, ch := s.ToggleAutoCast("someone_else", app.ID, "heal"); en || ch {
		t.Errorf("toggling another player's unit must no-op; got %v/%v", en, ch)
	}
	// Correct owner → toggles.
	if en, ch := s.ToggleAutoCast("p1", app.ID, "heal"); !en || !ch {
		t.Errorf("owner toggle should enable; got %v/%v", en, ch)
	}

	// An ability the unit has but that does NOT support auto-cast: no effect.
	s.mu.Lock()
	app.Abilities = append(app.Abilities, "__no_autocast__") // unresolved ⇒ !SupportsAutoCast path
	en, ch := s.toggleAutoCastLocked(app, "__no_autocast__")
	s.mu.Unlock()
	if en || ch {
		t.Errorf("right-click on a non-auto-cast ability must have no effect; got %v/%v", en, ch)
	}
}

// ── Triggers when ready; honors mana / cooldown / target / cast_time ─────────

func TestAutoCast_TriggersWhenReady(t *testing.T) {
	def := healDef(t)
	// Missing exactly one heal's worth → a single auto-cast restores it fully,
	// regardless of how healAmount is tuned in the catalog.
	s, app, ally := autoCastSetup(t, def.HealAmount)
	s.mu.Lock()
	allyID := ally.ID
	startMana := app.CurrentMana
	wantMana := startMana - def.ManaCost
	s.toggleAutoCastLocked(app, "heal")
	s.mu.Unlock()

	advance(s, 30) // loop initiates the cast, then it resolves (1s)

	s.mu.RLock()
	defer s.mu.RUnlock()
	a := s.unitsByID[allyID]
	if a.HP != a.MaxHP {
		t.Errorf("auto-cast should have healed the ally to full; HP=%d/%d", a.HP, a.MaxHP)
	}
	if app.CurrentMana != wantMana {
		t.Errorf("exactly one heal should have been auto-cast (mana %d→%d, -%d manaCost from catalog); got %d", startMana, wantMana, def.ManaCost, app.CurrentMana)
	}
}

func TestAutoCast_NoTriggerInsufficientMana(t *testing.T) {
	def := healDef(t)
	s, app, ally := autoCastSetup(t, 20)
	s.mu.Lock()
	app.CurrentMana = def.ManaCost - 1 // one short of the catalog manaCost
	app.ManaRegenPerSecond = 0         // isolate the mana gate (no regen lifting it over the cost)
	allyID := ally.ID
	startHP := ally.HP
	s.toggleAutoCastLocked(app, "heal")
	s.mu.Unlock()

	advance(s, 30)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if app.CastAbilityID != "" {
		t.Error("must not start a cast with insufficient mana")
	}
	if s.unitsByID[allyID].HP != startHP {
		t.Error("ally must not be healed when caster lacks mana")
	}
}

func TestAutoCast_NoTriggerOnCooldown(t *testing.T) {
	s, app, ally := autoCastSetup(t, 20)
	s.mu.Lock()
	allyID := ally.ID
	startHP := ally.HP
	s.toggleAutoCastLocked(app, "heal")
	app.AbilityCooldowns = map[string]float64{"heal": 1.0} // 1s cooldown remaining
	s.mu.Unlock()

	advance(s, 10) // 0.5s — still on cooldown
	s.mu.RLock()
	mid := s.unitsByID[allyID].HP
	s.mu.RUnlock()
	if mid != startHP || app.CastAbilityID != "" {
		t.Error("must not auto-cast while the ability is on cooldown")
	}

	advance(s, 40) // cooldown decays, then it casts + resolves
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.unitsByID[allyID].HP <= startHP {
		t.Error("after cooldown expires, auto-cast should fire")
	}
}

func TestAutoCast_NoTriggerNoValidTarget(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	app := s.spawnPlayerUnitLocked("apprentice", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	full := spawnProjTestUnit(t, s, "p1", 460, 400) // ally at FULL HP → not a target
	enemy := spawnProjTestUnit(t, s, enemyPlayerID, 470, 400)
	enemy.HP = 1        // hurt but hostile (enemy team) → heal can't target it
	enemy.AttackSpeed = 0 // disarm: caster now kites instead of killing it, so
	enemy.Damage = 0      // prevent it from damaging the ally and creating a target
	startMana := app.CurrentMana // spawned full from catalog maxMana
	s.toggleAutoCastLocked(app, "heal")
	s.mu.Unlock()
	_ = full

	advance(s, 30)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if app.CastAbilityID != "" || app.CurrentMana != startMana {
		t.Errorf("no valid heal target → no cast; CastAbilityID=%q mana=%d want %d", app.CastAbilityID, app.CurrentMana, startMana)
	}
}

func TestAutoCast_PicksCorrectAlly(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	app := s.spawnPlayerUnitLocked("apprentice", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	// Catalog seeds heal autocast ON at spawn; clear so the toggle below moves
	// the state in the asserted direction (off → on) the test was written for.
	delete(app.AutoCastEnabled, "heal")
	high := spawnProjTestUnit(t, s, "p1", 440, 400)
	high.HP = high.MaxHP - 10 // ~98%
	low := spawnProjTestUnit(t, s, "p1", 470, 400)
	low.HP = 50 // 10% — lowest
	lowID := low.ID
	s.toggleAutoCastLocked(app, "heal")
	s.mu.Unlock()

	// Tick until the auto-cast loop initiates the cast.
	for i := 0; i < 20 && app.CastAbilityID == ""; i++ {
		s.Update(0.05)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if app.CastTargetID != lowID {
		t.Errorf("auto-cast should target the lowest-HP%% ally (id %d); got %d", lowID, app.CastTargetID)
	}
}

func TestAutoCast_RespectsCastTimeNoStacking(t *testing.T) {
	def := healDef(t)
	// One heal fully restores → only one cast needed (missing == one heal's worth).
	s, app, _ := autoCastSetup(t, def.HealAmount)
	s.mu.Lock()
	startMana := app.CurrentMana
	wantMana := startMana - def.ManaCost
	s.toggleAutoCastLocked(app, "heal")
	s.mu.Unlock()

	// Drive to first initiation, then watch the whole cast: it must not be
	// re-initiated while in progress (mana deducts exactly once, on complete).
	sawCasting := false
	for i := 0; i < 40; i++ {
		s.Update(0.05)
		s.mu.RLock()
		if app.CastAbilityID == "heal" {
			sawCasting = true
		}
		s.mu.RUnlock()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !sawCasting {
		t.Fatal("expected the auto-cast to start a heal cast")
	}
	if app.CurrentMana != wantMana {
		t.Errorf("cast_time must be respected (no stacked re-cast): exactly one heal, mana want %d (%d - %d manaCost from catalog) got %d", wantMana, startMana, def.ManaCost, app.CurrentMana)
	}
}

// ── Per-unit-instance state; multiple abilities coexist ──────────────────────

func TestAutoCast_StateIsPerUnitInstance(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.spawnPlayerUnitLocked("apprentice", "p1", "#3498db", protocol.Vec2{X: 100, Y: 100})
	b := s.spawnPlayerUnitLocked("apprentice", "p1", "#3498db", protocol.Vec2{X: 200, Y: 100})
	// Clear catalog-seeded defaults so this test isolates per-instance toggle
	// isolation (its actual subject) rather than the spawn-time default.
	delete(a.AutoCastEnabled, "heal")
	delete(b.AutoCastEnabled, "heal")

	s.toggleAutoCastLocked(a, "heal")
	if !s.autoCastEnabledLocked(a, "heal") {
		t.Fatal("A should have heal auto-cast enabled")
	}
	if s.autoCastEnabledLocked(b, "heal") {
		t.Error("B must NOT inherit A's auto-cast toggle (per-unit-instance state)")
	}
	if len(b.AutoCastEnabled) != 0 {
		t.Errorf("B's auto-cast map should be untouched, got %v", b.AutoCastEnabled)
	}
}

func TestAutoCast_MultipleAbilitiesCoexist(t *testing.T) {
	s, app, _ := autoCastSetup(t, 20)
	s.mu.Lock()
	defer s.mu.Unlock()

	// heal via the real toggle path...
	s.toggleAutoCastLocked(app, "heal")
	// ...and a second ability's toggle held simultaneously (state-level: the
	// framework keys auto-cast per ability id and the loop iterates the
	// ordered Abilities slice, so N abilities can be enabled at once. Only
	// "heal" is authored today, so the 2nd is asserted at the state layer).
	if app.AutoCastEnabled == nil {
		t.Fatal("expected auto-cast map after enabling heal")
	}
	app.AutoCastEnabled["future_ability"] = true

	if !app.AutoCastEnabled["heal"] || !app.AutoCastEnabled["future_ability"] {
		t.Error("multiple abilities must be independently auto-cast-enabled at once")
	}
	// Toggling heal off leaves the other untouched.
	s.toggleAutoCastLocked(app, "heal")
	if app.AutoCastEnabled["heal"] || !app.AutoCastEnabled["future_ability"] {
		t.Error("toggling one ability must not affect another's auto-cast state")
	}
}

// ── Snapshot exposure + public cast entrypoint ───────────────────────────────

func TestAutoCast_SnapshotAndRequestCast(t *testing.T) {
	s, app, ally := autoCastSetup(t, 20)
	s.mu.Lock()
	s.toggleAutoCastLocked(app, "heal")
	states := s.abilityStatesLocked(app)
	s.mu.Unlock()

	if len(states) != 1 || states[0].ID != "heal" {
		t.Fatalf("expected one ability snapshot for heal; got %+v", states)
	}
	wantManaCost := healDef(t).ManaCost // snapshot must mirror catalog heal.manaCost
	if !states[0].SupportsAutoCast || !states[0].AutoCast || states[0].ManaCost != wantManaCost {
		t.Errorf("ability snapshot wrong (ManaCost want %d from catalog): %+v", wantManaCost, states[0])
	}

	// Public standard-cast entrypoint: ownership enforced, then delegates to
	// the Part 8 lifecycle.
	if ok, reason := s.RequestAbilityCast("someone_else", app.ID, "heal", ally.ID); ok || reason != castFailNotOwned {
		t.Errorf("non-owner cast must fail with %q; got (%v,%q)", castFailNotOwned, ok, reason)
	}
	if ok, reason := s.RequestAbilityCast("p1", app.ID, "heal", ally.ID); !ok {
		t.Errorf("owner cast should start: %q", reason)
	}
}
