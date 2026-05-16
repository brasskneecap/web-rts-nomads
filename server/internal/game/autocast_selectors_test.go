package game

import "testing"

// healLikeDef is an ability with Heal's targeting shape but a fixed numeric
// cast range, so range tests don't depend on the caster's AttackRange.
func healLikeDef(castRange float64) AbilityDef {
	return AbilityDef{
		CanTargetSelf:    true,
		CanTargetAllies:  true,
		CanTargetEnemies: false,
		CastRange:        CastRange(castRange),
	}
}

func lowestHPSelector(t *testing.T) AutoCastSelector {
	t.Helper()
	fn, ok := getAutoCastSelector("lowest_hp_percentage_ally_in_range")
	if !ok {
		t.Fatal("lowest_hp_percentage_ally_in_range not registered")
	}
	return fn
}

// ── Picks the lowest HP% ally; ignores full-HP allies ────────────────────────

func TestSelector_PicksLowestHPPercentAlly(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	a := spawnProjTestUnit(t, s, "p1", 50, 0)
	a.HP = 400 // 80%
	b := spawnProjTestUnit(t, s, "p1", 60, 0)
	b.HP = 100 // 20%  ← lowest
	full := spawnProjTestUnit(t, s, "p1", 70, 0) // 100% — excluded
	_ = full

	got := lowestHPSelector(t)(s, caster, healLikeDef(200))
	if got != b {
		t.Fatalf("selected %v; want the 20%% ally b (id %d)", idOf(got), b.ID)
	}
	_ = a
}

func TestSelector_NilWhenNoneBelowFull(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	sel := lowestHPSelector(t)
	// No other units at all.
	if got := sel(s, caster, healLikeDef(200)); got != nil {
		t.Errorf("no allies → want nil, got %v", idOf(got))
	}
	// An ally exists but is at full HP.
	mate := spawnProjTestUnit(t, s, "p1", 30, 0) // HP == MaxHP
	if got := sel(s, caster, healLikeDef(200)); got != nil {
		t.Errorf("only full-HP ally → want nil, got %v", idOf(got))
	}
	_ = mate
}

// ── Respects cast range ──────────────────────────────────────────────────────

func TestSelector_RespectsCastRange(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	far := spawnProjTestUnit(t, s, "p1", 300, 0)
	far.HP = 50 // 10% — lowest, but out of a 200px range
	near := spawnProjTestUnit(t, s, "p1", 150, 0)
	near.HP = 400 // 80% — higher %, but in range

	got := lowestHPSelector(t)(s, caster, healLikeDef(200))
	if got != near {
		t.Errorf("out-of-range lower-%% ally must be ignored: got %v, want near (id %d)", idOf(got), near.ID)
	}
}

// ── Considers only allies (enemies / neutrals filtered) ──────────────────────

func TestSelector_AlliesOnly(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	enemy := spawnProjTestUnit(t, s, enemyPlayerID, 40, 0)
	enemy.HP = 1 // critically low but hostile (enemy team)
	neutral := spawnProjTestUnit(t, s, "neutral", 45, 0)
	setTeam(s, "neutral", 2) // different (non-enemy) team ⇒ not an ally
	neutral.HP = 1 // critically low but not friendly
	ally := spawnProjTestUnit(t, s, "p1", 50, 0)
	ally.HP = 450 // 90% — the only valid (friendly) candidate

	got := lowestHPSelector(t)(s, caster, healLikeDef(200))
	if got != ally {
		t.Errorf("must pick the friendly ally, not enemy/neutral: got %v want ally (id %d)", idOf(got), ally.ID)
	}
	_ = enemy
	_ = neutral
}

// ── Caster itself is eligible iff the ability can target self ────────────────

func TestSelector_CasterSelfWhenLowestAndCanTargetSelf(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	caster.HP = 50 // 10% — lowest of all
	ally := spawnProjTestUnit(t, s, "p1", 50, 0)
	ally.HP = 400 // 80%

	sel := lowestHPSelector(t)

	// can_target_self = true → caster heals itself.
	if got := sel(s, caster, healLikeDef(200)); got != caster {
		t.Errorf("with CanTargetSelf, lowest-%% caster should select itself; got %v", idOf(got))
	}
	// can_target_self = false → caster excluded, falls to the ally.
	noSelf := AbilityDef{CanTargetAllies: true, CastRange: 200}
	if got := sel(s, caster, noSelf); got != ally {
		t.Errorf("without CanTargetSelf, caster must be skipped; got %v want ally (id %d)", idOf(got), ally.ID)
	}
}

// ── Tie broken by closest distance, then lowest unit ID ──────────────────────

func TestSelector_TieBreak_ClosestThenID(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	// Equal HP% (50%), equal distance (100) → lower unit ID wins.
	left := spawnProjTestUnit(t, s, "p1", -100, 0) // spawned first → lower ID
	left.HP = 250
	right := spawnProjTestUnit(t, s, "p1", 100, 0)
	right.HP = 250
	sel := lowestHPSelector(t)
	if got := sel(s, caster, healLikeDef(300)); got != left {
		t.Errorf("equal %% and distance → lowest ID (left, id %d); got %v", left.ID, idOf(got))
	}

	// Now add an equal-% ally that is strictly closer → distance wins.
	close := spawnProjTestUnit(t, s, "p1", 50, 0)
	close.HP = 250
	if got := sel(s, caster, healLikeDef(300)); got != close {
		t.Errorf("closer equal-%% ally should win on distance; got %v want close (id %d)", idOf(got), close.ID)
	}
}

// ── Registry: lookup, extensibility, resolve entrypoint, guards ──────────────

func TestAutoCastRegistry(t *testing.T) {
	if _, ok := getAutoCastSelector("lowest_hp_percentage_ally_in_range"); !ok {
		t.Error("builtin selector should be registered")
	}
	if _, ok := getAutoCastSelector("nope_not_a_selector"); ok {
		t.Error("unknown selector must report ok=false")
	}

	called := false
	RegisterAutoCastSelector("test_custom_selector", func(*GameState, *Unit, AbilityDef) *Unit {
		called = true
		return nil
	})
	fn, ok := getAutoCastSelector("test_custom_selector")
	if !ok {
		t.Fatal("custom selector not registered")
	}
	fn(nil, nil, AbilityDef{})
	if !called {
		t.Error("registered custom selector was not invoked")
	}

	assertPanics(t, "empty name", func() { RegisterAutoCastSelector("", selectSelf) })
	assertPanics(t, "nil fn", func() { RegisterAutoCastSelector("x", nil) })
}

func TestResolveAutoCastTargetLocked(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	ally := spawnProjTestUnit(t, s, "p1", 50, 0)
	ally.HP = 100

	// No selector named → nil.
	if got := s.resolveAutoCastTargetLocked(caster, AbilityDef{}); got != nil {
		t.Errorf("empty AutoCastTargetSelector → nil; got %v", idOf(got))
	}
	// Unknown selector → nil.
	if got := s.resolveAutoCastTargetLocked(caster, AbilityDef{AutoCastTargetSelector: "ghost"}); got != nil {
		t.Errorf("unknown selector → nil; got %v", idOf(got))
	}
	// Real heal selector via the entrypoint picks the damaged ally.
	def := healLikeDef(200)
	def.AutoCastTargetSelector = "lowest_hp_percentage_ally_in_range"
	if got := s.resolveAutoCastTargetLocked(caster, def); got != ally {
		t.Errorf("resolve via entrypoint should pick the damaged ally; got %v", idOf(got))
	}
}

// ── Placeholder selectors behave sensibly ────────────────────────────────────

func TestSelector_Stubs(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	caster := spawnProjTestUnit(t, s, "p1", 0, 0)

	// "self": returns caster only when the ability can target self.
	selfSel, _ := getAutoCastSelector("self")
	if got := selfSel(s, caster, AbilityDef{CanTargetSelf: true}); got != caster {
		t.Errorf("self selector with CanTargetSelf should return caster; got %v", idOf(got))
	}
	if got := selfSel(s, caster, AbilityDef{CanTargetSelf: false}); got != nil {
		t.Errorf("self selector without CanTargetSelf should return nil; got %v", idOf(got))
	}

	// "closest_enemy_in_range": closest visible hostile the ability can hit.
	enemySel, _ := getAutoCastSelector("closest_enemy_in_range")
	offensive := AbilityDef{CanTargetEnemies: true, CastRange: 300}
	if got := enemySel(s, caster, offensive); got != nil {
		t.Errorf("no enemies → nil; got %v", idOf(got))
	}
	near := spawnProjTestUnit(t, s, enemyPlayerID, 80, 0)
	far := spawnProjTestUnit(t, s, enemyPlayerID, 200, 0)
	if got := enemySel(s, caster, offensive); got != near {
		t.Errorf("closest enemy expected (near id %d); got %v", near.ID, idOf(got))
	}
	near.Visible = false // invisible → skipped, falls to far
	if got := enemySel(s, caster, offensive); got != far {
		t.Errorf("invisible enemy must be skipped; want far (id %d) got %v", far.ID, idOf(got))
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func idOf(u *Unit) any {
	if u == nil {
		return "nil"
	}
	return u.ID
}

func assertPanics(t *testing.T, what string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: expected panic, got none", what)
		}
	}()
	fn()
}
