package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// setupHostileTargetingPair builds a GameState with p1 (team 0) and p2
// (team 1, hostile to p1) via the shared team-combat test helpers
// (team_combat_test.go / projectile_defs_test.go): newProjectileTestState,
// teamCombatUnit, setTeam. Lock is held on return; caller must Unlock.
func setupHostileTargetingPair(t *testing.T) *GameState {
	t.Helper()
	s := newProjectileTestState(t)
	s.mu.Lock()
	setTeam(s, "p1", 0)
	setTeam(s, "p2", 1)
	return s
}

func TestResolveTargetQueryRadiusRelationsOrdering(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	caster.HP, caster.MaxHP = 100, 100

	e1 := teamCombatUnit(t, s, "p2", 50, 0)
	e1.HP, e1.MaxHP = 30, 100

	e2 := teamCombatUnit(t, s, "p2", 100, 0)
	e2.HP, e2.MaxHP = 80, 100

	_ = teamCombatUnit(t, s, "p2", 999, 0) // out of radius

	ctx := &RuntimeAbilityContext{CasterID: caster.ID}
	q := TargetQueryDef{
		Source:    SrcAllInScene,
		Origin:    OriginCaster,
		Relations: []TargetRelation{RelEnemy},
		Radius:    200,
		Ordering:  OrderLowestHealthPct,
		MaxCount:  2,
	}
	got := s.resolveTargetQueryLocked(ctx, q)
	if len(got) != 2 || got[0] != e1.ID || got[1] != e2.ID {
		t.Fatalf("got %v, want [%d %d]", got, e1.ID, e2.ID)
	}
}

func TestResolveTargetQuery_AllySelfRelations_ExcludesEnemies(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	ally := teamCombatUnit(t, s, "p1", 10, 0)
	_ = teamCombatUnit(t, s, "p2", 20, 0) // enemy, must be excluded

	ctx := &RuntimeAbilityContext{CasterID: caster.ID}
	q := TargetQueryDef{
		Source:    SrcAllInScene,
		Origin:    OriginCaster,
		Relations: []TargetRelation{RelSelf, RelAlly},
		Radius:    1000,
		Ordering:  OrderUnitID,
	}
	got := s.resolveTargetQueryLocked(ctx, q)
	want := []int{caster.ID, ally.ID}
	if got[0] > got[1] {
		want = []int{ally.ID, caster.ID}
	}
	if len(got) != 2 {
		t.Fatalf("got %v, want exactly caster+ally (len 2)", got)
	}
	for _, id := range got {
		if id != caster.ID && id != ally.ID {
			t.Fatalf("got %v, unexpected id %d (enemy leaked in)", got, id)
		}
	}
	_ = want
}

func TestResolveTargetQuery_IncludeInitialTarget_ForcesOutOfRadiusTarget(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	farEnemy := teamCombatUnit(t, s, "p2", 500, 0) // outside the radius below

	ctx := &RuntimeAbilityContext{CasterID: caster.ID, InitialTarget: farEnemy.ID}
	q := TargetQueryDef{
		Source:               SrcAllInScene,
		Origin:               OriginCaster,
		Relations:            []TargetRelation{RelEnemy},
		Radius:               50, // farEnemy is well outside this
		IncludeInitialTarget: true,
		Ordering:             OrderUnitID,
	}
	got := s.resolveTargetQueryLocked(ctx, q)
	found := false
	for _, id := range got {
		if id == farEnemy.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("got %v, want farEnemy (%d) forced in via IncludeInitialTarget", got, farEnemy.ID)
	}
}

func TestResolveTargetQuery_ExcludeSource_RemovesCasterFromAllInSceneQuery(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	ally := teamCombatUnit(t, s, "p1", 10, 0)

	ctx := &RuntimeAbilityContext{CasterID: caster.ID}
	q := TargetQueryDef{
		Source:        SrcAllInScene,
		Origin:        OriginCaster,
		Relations:     []TargetRelation{RelSelf, RelAlly},
		Radius:        1000,
		ExcludeSource: true,
		Ordering:      OrderUnitID,
	}
	got := s.resolveTargetQueryLocked(ctx, q)
	if len(got) != 1 || got[0] != ally.ID {
		t.Fatalf("got %v, want [%d] (caster excluded)", got, ally.ID)
	}
}

// TestResolveTargetQuery_ExcludeCurrentEvent_RemovesEventUnitFromRadiusQuery
// proves Gap 2's fix: a "hit an enemy, then splash to nearby OTHER enemies"
// query (all_in_scene, origin: current_event_position) must not include the
// current-event unit itself — it's an enemy at distance 0 from its own
// position, so without ExcludeCurrentEvent it always leaks back in.
func TestResolveTargetQuery_ExcludeCurrentEvent_RemovesEventUnitFromRadiusQuery(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	hitUnit := teamCombatUnit(t, s, "p2", 300, 0) // the "current event" unit
	near1 := teamCombatUnit(t, s, "p2", 350, 0)   // 50px from hitUnit
	near2 := teamCombatUnit(t, s, "p2", 300, 150) // 150px from hitUnit
	_ = teamCombatUnit(t, s, "p2", 300, 900)      // well outside the radius below

	ctx := &RuntimeAbilityContext{
		CasterID:           caster.ID,
		CurrentEventUnitID: hitUnit.ID,
		EventPosition:      protocol.Vec2{X: hitUnit.X, Y: hitUnit.Y},
	}
	q := TargetQueryDef{
		Source:              SrcAllInScene,
		Origin:              OriginCurrentEventPos,
		Relations:           []TargetRelation{RelEnemy},
		Radius:              200,
		ExcludeCurrentEvent: true,
		Ordering:            OrderUnitID,
	}
	got := s.resolveTargetQueryLocked(ctx, q)
	wantSet := map[int]bool{near1.ID: true, near2.ID: true}
	if len(got) != len(wantSet) {
		t.Fatalf("got %v, want exactly [%d %d] (hit unit excluded, out-of-radius bystander excluded)", got, near1.ID, near2.ID)
	}
	for _, id := range got {
		if !wantSet[id] {
			t.Fatalf("got %v, unexpected id %d", got, id)
		}
		if id == hitUnit.ID {
			t.Fatalf("got %v, current-event unit %d must be excluded by ExcludeCurrentEvent", got, hitUnit.ID)
		}
	}
}

// TestResolveTargetQuery_ExcludeCurrentEvent_DefaultOff_EventUnitIncluded
// guards the byte-identical default: every query authored before this field
// existed leaves ExcludeCurrentEvent false, so the current-event unit stays
// in the result set exactly like before this fix.
func TestResolveTargetQuery_ExcludeCurrentEvent_DefaultOff_EventUnitIncluded(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	hitUnit := teamCombatUnit(t, s, "p2", 300, 0)

	ctx := &RuntimeAbilityContext{
		CasterID:           caster.ID,
		CurrentEventUnitID: hitUnit.ID,
		EventPosition:      protocol.Vec2{X: hitUnit.X, Y: hitUnit.Y},
	}
	q := TargetQueryDef{
		Source:    SrcAllInScene,
		Origin:    OriginCurrentEventPos,
		Relations: []TargetRelation{RelEnemy},
		Radius:    200,
		Ordering:  OrderUnitID,
		// ExcludeCurrentEvent left unset (false).
	}
	got := s.resolveTargetQueryLocked(ctx, q)
	found := false
	for _, id := range got {
		if id == hitUnit.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("got %v, want hit unit %d included by default (ExcludeCurrentEvent unset)", got, hitUnit.ID)
	}
}

func TestResolveTargetQuery_EmptyRelations_MatchesAllLivingUnits(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	ally := teamCombatUnit(t, s, "p1", 10, 0)
	enemy := teamCombatUnit(t, s, "p2", 20, 0)

	ctx := &RuntimeAbilityContext{CasterID: caster.ID}
	q := TargetQueryDef{
		Source:   SrcAllInScene,
		Origin:   OriginCaster,
		Radius:   1000,
		Ordering: OrderUnitID,
	}
	got := s.resolveTargetQueryLocked(ctx, q)
	if len(got) != 3 {
		t.Fatalf("got %v, want all 3 units (caster, ally, enemy) with no relation filter", got)
	}
	for _, id := range []int{caster.ID, ally.ID, enemy.ID} {
		found := false
		for _, g := range got {
			if g == id {
				found = true
			}
		}
		if !found {
			t.Fatalf("got %v, missing unit %d", got, id)
		}
	}
}

// TestResolveTargetQuery_MixedRelations_VisibilityIsPerCandidate guards the
// code-review fix: the enemy-visibility parity check must be evaluated per
// candidate against THAT candidate's own relation to the caster, not
// query-wide off q.Relations. A Relations:[RelAlly, RelEnemy] query with an
// invisible ally must still return the ally (visibility never applies to
// allies) while still excluding an invisible enemy.
func TestResolveTargetQuery_MixedRelations_VisibilityIsPerCandidate(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	invisibleAlly := teamCombatUnit(t, s, "p1", 10, 0)
	invisibleAlly.Visible = false
	invisibleEnemy := teamCombatUnit(t, s, "p2", 20, 0)
	invisibleEnemy.Visible = false
	visibleEnemy := teamCombatUnit(t, s, "p2", 30, 0)

	ctx := &RuntimeAbilityContext{CasterID: caster.ID}
	q := TargetQueryDef{
		Source:    SrcAllInScene,
		Origin:    OriginCaster,
		Relations: []TargetRelation{RelAlly, RelEnemy},
		Radius:    1000,
		Ordering:  OrderUnitID,
	}
	got := s.resolveTargetQueryLocked(ctx, q)

	wantSet := map[int]bool{invisibleAlly.ID: true, visibleEnemy.ID: true}
	if len(got) != len(wantSet) {
		t.Fatalf("got %v, want exactly [%d %d] (invisible ally in, invisible enemy out, caster excluded by RelSelf not being requested)", got, invisibleAlly.ID, visibleEnemy.ID)
	}
	for _, id := range got {
		if !wantSet[id] {
			t.Fatalf("got %v, unexpected id %d", got, id)
		}
	}
	for _, id := range got {
		if id == invisibleEnemy.ID {
			t.Fatalf("got %v, invisible enemy %d must be excluded", got, invisibleEnemy.ID)
		}
	}
}

// TestResolveTargetQuery_OrderRandom_DeterministicUnderSeed guards seeded-
// shuffle determinism: two independently-constructed GameStates, seeded and
// populated identically, must produce the IDENTICAL shuffled order for the
// same query (not merely the same set of ids).
func TestResolveTargetQuery_OrderRandom_DeterministicUnderSeed(t *testing.T) {
	build := func(t *testing.T) (*GameState, int, []int) {
		t.Helper()
		s := newProjectileTestState(t)
		s.mu.Lock()
		setTeam(s, "p1", 0)
		setTeam(s, "p2", 1)
		caster := teamCombatUnit(t, s, "p1", 0, 0)
		enemyIDs := make([]int, 0, 5)
		for i := 0; i < 5; i++ {
			e := teamCombatUnit(t, s, "p2", float64(10*(i+1)), 0)
			enemyIDs = append(enemyIDs, e.ID)
		}
		return s, caster.ID, enemyIDs
	}

	s1, casterID1, enemyIDs1 := build(t)
	defer s1.mu.Unlock()
	s2, casterID2, enemyIDs2 := build(t)
	defer s2.mu.Unlock()

	q := TargetQueryDef{
		Source:    SrcAllInScene,
		Origin:    OriginCaster,
		Relations: []TargetRelation{RelEnemy},
		Radius:    1000,
		Ordering:  OrderRandom,
	}
	got1 := s1.resolveTargetQueryLocked(&RuntimeAbilityContext{CasterID: casterID1}, q)
	got2 := s2.resolveTargetQueryLocked(&RuntimeAbilityContext{CasterID: casterID2}, q)

	if len(got1) != 5 || len(got2) != 5 {
		t.Fatalf("got1=%v got2=%v, want length 5 from both identically-seeded states", got1, got2)
	}
	if len(got1) != len(got2) {
		t.Fatalf("got1=%v got2=%v, lengths differ", got1, got2)
	}
	for i := range got1 {
		if got1[i] != got2[i] {
			t.Fatalf("seeded shuffle not deterministic: got1=%v got2=%v differ at index %d", got1, got2, i)
		}
	}

	// Both slices must also be an exact permutation of the expected enemy
	// id sets (same set, any order) — guards against the shuffle silently
	// dropping/duplicating ids while still "matching" via the equality
	// check above.
	assertPermutation := func(t *testing.T, got, want []int) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("got %v, want permutation of %v (length mismatch)", got, want)
		}
		wantSet := make(map[int]int, len(want))
		for _, id := range want {
			wantSet[id]++
		}
		for _, id := range got {
			wantSet[id]--
		}
		for id, count := range wantSet {
			if count != 0 {
				t.Fatalf("got %v, not a permutation of %v (id %d count off by %d)", got, want, id, count)
			}
		}
	}
	assertPermutation(t, got1, enemyIDs1)
	assertPermutation(t, got2, enemyIDs2)
}
