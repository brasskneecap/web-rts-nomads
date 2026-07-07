package game

// Section 2.6 + ability-multi-target spec scenarios.
//
// Covers:
//   - 2.6: Single-target heal no-regression (duplicate of 14.24 from a
//     different angle — testing the resolver directly rather than via autocast).
//   - Multi-target selector: lowest-HP%, tie-break, full-HP exclusion,
//     force-include semantics, per-target hook call.
//   - TargetCount normalisation (default = 1 for omitted key).
//   - AbilitySnapshot.TargetCount surface.

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────────────────────
// 2.6 — Single-target heal is byte-identical to pre-change behavior
// ─────────────────────────────────────────────────────────────────────────────

// TestNoRegression_SingleTargetHealUnchanged verifies that a heal-only
// Acolyte (no greater_heal, no focus) heals exactly one ally, deducts mana,
// and plays healing_glow — byte-identical to the pre-multi-target behavior.
// This documents the invariant: TargetCount == 1 must produce identical
// results to the original single-target resolver.
func TestNoRegression_SingleTargetHealUnchanged(t *testing.T) {
	s, app, ally := healSetup(t)
	def := healDef(t)

	s.mu.Lock()
	// Confirm preconditions: single-target heal, no focus, no perk.
	if def.TargetCount != 1 {
		s.mu.Unlock()
		t.Skipf("heal.TargetCount = %d, want 1 — single-target regression not applicable", def.TargetCount)
	}
	for _, p := range app.PerkIDs {
		if p == "greater_heal" {
			s.mu.Unlock()
			t.Skip("app has greater_heal; skip single-target regression")
		}
	}
	if app.FocusTargetID != 0 {
		s.mu.Unlock()
		t.Fatal("precondition: acolyte should have no focus")
	}

	allyID := ally.ID
	wantHP := ally.HP + def.HealAmount
	startMana := app.CurrentMana
	wantMana := startMana - def.ManaCost

	// Spawn a second ally that must NOT be healed (no multi-target spill-over).
	bystander := s.spawnPlayerUnitLocked("soldier", "p1", "#aabb00", protocol.Vec2{X: 450, Y: 410})
	bystander.MaxHP = 500
	bystander.HP = ally.HP - 10 // slightly more injured
	bystander.Visible = true
	bystanderID := bystander.ID
	bystanderHPBefore := bystander.HP

	ok, reason := s.beginAbilityCastLocked(app, "heal", ally)
	s.mu.Unlock()
	if !ok {
		t.Fatalf("beginAbilityCastLocked: %q", reason)
	}

	advance(s, 25)

	s.mu.RLock()
	defer s.mu.RUnlock()
	a := s.unitsByID[allyID]
	b := s.unitsByID[bystanderID]
	if a.HP != wantHP {
		t.Errorf("primary ally HP = %d; want %d (single-target heal)", a.HP, wantHP)
	}
	if app.CurrentMana != wantMana {
		t.Errorf("caster mana = %d; want %d", app.CurrentMana, wantMana)
	}
	// Bystander must be untouched.
	if b != nil && b.HP != bystanderHPBefore {
		t.Errorf("bystander HP changed: %d → %d; single-target heal must NOT spill to second ally", bystanderHPBefore, b.HP)
	}
	// Exactly one healing_glow for the primary.
	if glow := queuedEffectFor(s, "healing_glow", allyID); glow == nil {
		t.Error("healing_glow should play on primary ally")
	}
	// No healing_glow on the bystander.
	if glow := queuedEffectFor(s, "healing_glow", bystanderID); glow != nil {
		t.Errorf("healing_glow must NOT play on bystander in a single-target heal")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Multi-target selector: lowest-HP% ordering and full-HP exclusion
// ─────────────────────────────────────────────────────────────────────────────

// TestMultiTarget_ThreeInjuredAlliesAllReceiveHeal verifies that three injured
// allies with TargetCount==3 all receive the effect via buildCastTargetSetLocked.
func TestMultiTarget_ThreeInjuredAlliesAllReceiveHeal(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(cleric.Abilities) == 0 || cleric.Abilities[0] != "heal" {
		t.Skipf("acolyte Abilities[0] != \"heal\"")
	}
	// Grant-pipeline test: promote to (cleric, bronze) so the path-ability
	// grant runs the heal → greater_heal swap.
	promoteToBronzeCleric(s, cleric)

	def, ok := getAbilityDef("greater_heal")
	if !ok {
		t.Fatal("greater_heal def not found")
	}
	if def.TargetCount < 3 {
		t.Skipf("greater_heal TargetCount = %d, need >= 3", def.TargetCount)
	}

	a1 := spawnAlly(t, s, "p1", 430, 400)
	a1.HP = a1.MaxHP * 2 / 10
	a2 := spawnAlly(t, s, "p1", 440, 400)
	a2.HP = a2.MaxHP * 5 / 10
	a3 := spawnAlly(t, s, "p1", 450, 400)
	a3.HP = a3.MaxHP * 7 / 10

	targets := s.buildCastTargetSetLocked(cleric, def, a1)

	if len(targets) != 3 {
		t.Fatalf("expected 3 targets, got %d", len(targets))
	}
	idSet := map[int]bool{a1.ID: true, a2.ID: true, a3.ID: true}
	for _, tgt := range targets {
		if !idSet[tgt.ID] {
			t.Errorf("unexpected target id %d", tgt.ID)
		}
	}
}

// TestMultiTarget_OnlyOneAllyInRange verifies that when only one ally is in
// range, a TargetCount==3 cast still succeeds (partial fills are valid).
func TestMultiTarget_OnlyOneAllyInRange(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(cleric.Abilities) == 0 || cleric.Abilities[0] != "heal" {
		t.Skipf("acolyte Abilities[0] != \"heal\"")
	}
	// Grant-pipeline test: promote to (cleric, bronze) so the path-ability
	// grant runs the heal → greater_heal swap.
	promoteToBronzeCleric(s, cleric)

	def, ok := getAbilityDef("greater_heal")
	if !ok {
		t.Fatal("greater_heal def not found")
	}

	a := spawnAlly(t, s, "p1", 430, 400)
	a.HP = a.MaxHP / 2

	targets := s.buildCastTargetSetLocked(cleric, def, a)

	if len(targets) == 0 {
		t.Error("cast with only 1 in-range ally should still produce 1 target (partial fill)")
	}
	if targets[0].ID != a.ID {
		t.Errorf("single-ally target: got id %d, want %d", targets[0].ID, a.ID)
	}
}

// TestMultiTarget_FullHPAlliesExcludedByDefault confirms full-HP allies are not
// selected by buildCastTargetSetLocked when no force-include applies.
func TestMultiTarget_FullHPAlliesExcludedByDefault(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(cleric.Abilities) == 0 || cleric.Abilities[0] != "heal" {
		t.Skipf("acolyte Abilities[0] != \"heal\"")
	}
	// Grant-pipeline test: promote to (cleric, bronze) so the path-ability
	// grant runs the heal → greater_heal swap.
	promoteToBronzeCleric(s, cleric)

	def, ok := getAbilityDef("greater_heal")
	if !ok {
		t.Fatal("greater_heal def not found")
	}

	injured1 := spawnAlly(t, s, "p1", 430, 400)
	injured1.HP = injured1.MaxHP * 4 / 10
	injured2 := spawnAlly(t, s, "p1", 440, 400)
	injured2.HP = injured2.MaxHP * 6 / 10
	fullHP := spawnAlly(t, s, "p1", 450, 400)
	fullHP.HP = fullHP.MaxHP

	targets := s.buildCastTargetSetLocked(cleric, def, injured1)

	for _, tgt := range targets {
		if tgt.ID == fullHP.ID {
			t.Errorf("full-HP ally (id %d) should not appear in multi-target set without force-include", fullHP.ID)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Force-include semantics
// ─────────────────────────────────────────────────────────────────────────────

// TestMultiTarget_ForceInclude_DisplacesHighestHPNaturalPick verifies the
// spec's worked example: natural picks {A 10%, B 40%, C 60%}, force-include
// D at 100% → result is {A, B, D}.
func TestMultiTarget_ForceInclude_DisplacesHighestHPNaturalPick(t *testing.T) {
	s, _ := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Manufacture an HP-labeled set directly.
	a := spawnAlly(t, s, "p1", 430, 400)
	a.MaxHP = 100
	a.HP = 10 // 10%
	b := spawnAlly(t, s, "p1", 440, 400)
	b.MaxHP = 100
	b.HP = 40 // 40%
	c := spawnAlly(t, s, "p1", 450, 400)
	c.MaxHP = 100
	c.HP = 60 // 60%
	d := spawnAlly(t, s, "p1", 460, 400)
	d.MaxHP = 100
	d.HP = 100 // 100% — force-include candidate

	cands := []*Unit{a, b, c}
	castTargetSortByHPPct(cands)
	result := castTargetForceInclude(cands, d, 3)

	if len(result) != 3 {
		t.Fatalf("result set size = %d, want 3", len(result))
	}
	dFound := false
	cFound := false
	for _, u := range result {
		if u.ID == d.ID {
			dFound = true
		}
		if u.ID == c.ID {
			cFound = true
		}
	}
	if !dFound {
		t.Errorf("force-include unit D (id %d) not in result", d.ID)
	}
	if cFound {
		t.Errorf("C (60%%, id %d) should have been displaced by force-include of D (100%%)", c.ID)
	}
}

// TestMultiTarget_ForceInclude_AlreadyPresentIsNoOp verifies that force-including
// a unit already in the set does not duplicate it.
func TestMultiTarget_ForceInclude_AlreadyPresentIsNoOp(t *testing.T) {
	s, _ := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	a := spawnAlly(t, s, "p1", 430, 400)
	a.HP = a.MaxHP / 2
	b := spawnAlly(t, s, "p1", 440, 400)
	b.HP = b.MaxHP * 3 / 4

	cands := []*Unit{a, b}
	castTargetSortByHPPct(cands)
	// Force-include a, which is already in cands.
	result := castTargetForceInclude(cands, a, 3)

	if len(result) != 2 {
		t.Errorf("force-include of existing unit should not grow set: got %d, want 2", len(result))
	}
	count := 0
	for _, u := range result {
		if u.ID == a.ID {
			count++
		}
	}
	if count != 1 {
		t.Errorf("force-include existing: unit appears %d times, want exactly 1", count)
	}
}

// TestMultiTarget_ForceInclude_InvalidUnitIgnored confirms that a nil
// force-include is silently ignored.
func TestMultiTarget_ForceInclude_InvalidUnitIgnored(t *testing.T) {
	s, _ := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	a := spawnAlly(t, s, "p1", 430, 400)
	a.HP = a.MaxHP / 2

	cands := []*Unit{a}
	result := castTargetForceInclude(cands, nil, 3)

	if len(result) != 1 || result[0].ID != a.ID {
		t.Errorf("nil force-include should be no-op; got %v", result)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Post-cast hook: called once per target
// ─────────────────────────────────────────────────────────────────────────────

// TestMultiTarget_PostCastHookCalledPerTarget verifies that
// onPerkAbilityResolvedLocked is called once per resolved target by checking
// the battle_prayer buff is stamped on every target in a 3-target cast.
func TestMultiTarget_PostCastHookCalledPerTarget(t *testing.T) {
	s, cleric := newClericBronzeState(t)

	s.mu.Lock()
	if len(cleric.Abilities) == 0 || cleric.Abilities[0] != "heal" {
		s.mu.Unlock()
		t.Skipf("acolyte Abilities[0] != \"heal\"")
	}
	// Grant-pipeline test: promote to (cleric, bronze) so the path-ability
	// grant runs the heal → greater_heal swap.
	promoteToBronzeCleric(s, cleric)
	grantPerk(cleric, "battle_prayer")

	def, ok := getAbilityDef("greater_heal")
	if !ok {
		s.mu.Unlock()
		t.Fatal("greater_heal def not found")
	}
	if def.TargetCount < 3 {
		s.mu.Unlock()
		t.Skipf("greater_heal TargetCount = %d, need >= 3", def.TargetCount)
	}

	bpDef := perkDefByID("battle_prayer")
	if bpDef == nil {
		s.mu.Unlock()
		t.Fatal("battle_prayer def not found")
	}
	wantDuration := bpDef.ConfigForRank(cleric.Rank)["buffDurationSeconds"]

	a1 := spawnAlly(t, s, "p1", 430, 400)
	a1.HP = a1.MaxHP * 3 / 10
	a2 := spawnAlly(t, s, "p1", 440, 400)
	a2.HP = a2.MaxHP * 5 / 10
	a3 := spawnAlly(t, s, "p1", 450, 400)
	a3.HP = a3.MaxHP * 7 / 10
	a1ID, a2ID, a3ID := a1.ID, a2.ID, a3.ID

	ok, reason := s.beginAbilityCastLocked(cleric, "greater_heal", a1)
	s.mu.Unlock()
	if !ok {
		t.Fatalf("beginAbilityCastLocked: %q", reason)
	}

	advance(s, 25)

	const advanceDt = 25 * 0.05 // 1.25s advance window; buff decays during this time

	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, id := range []int{a1ID, a2ID, a3ID} {
		u := s.unitsByID[id]
		if u == nil {
			continue
		}
		// Assert the hook fired per target: buff was stamped and is actively decaying.
		if u.PerkState.BattlePrayerRemaining <= 0 {
			t.Errorf("ally id %d: BattlePrayerRemaining = %.3f; hook did not fire (buff absent)", id, u.PerkState.BattlePrayerRemaining)
		}
		if u.PerkState.BattlePrayerRemaining > wantDuration {
			t.Errorf("ally id %d: BattlePrayerRemaining = %.3f exceeds configured %.3f", id, u.PerkState.BattlePrayerRemaining, wantDuration)
		}
		if u.PerkState.BattlePrayerRemaining < wantDuration-advanceDt {
			t.Errorf("ally id %d: BattlePrayerRemaining = %.3f decayed too far from %.3f (advance budget %.3f)",
				id, u.PerkState.BattlePrayerRemaining, wantDuration, advanceDt)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TargetCount catalog defaults and normalisation
// ─────────────────────────────────────────────────────────────────────────────

// TestTargetCount_DefaultsToOne verifies that the base heal ability (no
// targetCount JSON key) loads with TargetCount == 1.
func TestTargetCount_DefaultsToOne(t *testing.T) {
	def, ok := getAbilityDef("heal")
	if !ok {
		t.Fatal(`getAbilityDef("heal") returned false`)
	}
	if def.TargetCount != 1 {
		t.Errorf("heal.TargetCount = %d, want 1 (default for single-target ability)", def.TargetCount)
	}
}

// TestTargetCount_GreaterHealIsMultiTarget confirms greater_heal is a
// multi-target ability. The exact target count is a balance tunable owned by
// greater_heal.json, so this asserts the multi-target invariant (>= 2) rather
// than pinning the number.
func TestTargetCount_GreaterHealIsMultiTarget(t *testing.T) {
	def, ok := getAbilityDef("greater_heal")
	if !ok {
		t.Fatal(`getAbilityDef("greater_heal") returned false`)
	}
	if def.TargetCount < 2 {
		t.Errorf("greater_heal.TargetCount = %d, want >= 2 (multi-target)", def.TargetCount)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AbilitySnapshot.TargetCount surface
// ─────────────────────────────────────────────────────────────────────────────

// TestAbilitySnapshot_TargetCountSingleTarget verifies that a unit with heal
// has an ability snapshot reporting TargetCount == 1.
func TestAbilitySnapshot_TargetCountSingleTarget(t *testing.T) {
	s, app, _ := healSetup(t)
	s.mu.RLock()
	defer s.mu.RUnlock()

	snaps := s.abilityStatesLocked(app)
	for _, ab := range snaps {
		if ab.ID == "heal" {
			if ab.TargetCount != 1 {
				t.Errorf("heal ability snapshot: TargetCount = %d, want 1", ab.TargetCount)
			}
			return
		}
	}
	t.Skip("heal not in acolyte's ability snapshot; skip TargetCount surface test")
}

// TestAbilitySnapshot_TargetCountGreaterHeal verifies that a unit with
// greater_heal has an ability snapshot reporting TargetCount == 3.
func TestAbilitySnapshot_TargetCountGreaterHeal(t *testing.T) {
	s, app, _ := healSetup(t)
	s.mu.Lock()

	if len(app.Abilities) == 0 || app.Abilities[0] != "heal" {
		s.mu.Unlock()
		t.Skip("app doesn't have heal in slot 0")
	}
	// Grant-pipeline test: promote the acolyte to (cleric, bronze) so the
	// path-ability grant runs the heal → greater_heal swap.
	promoteToBronzeCleric(s, app)
	s.mu.Unlock()

	s.mu.RLock()
	defer s.mu.RUnlock()
	snaps := s.abilityStatesLocked(app)
	for _, ab := range snaps {
		if ab.ID == "greater_heal" {
			if ab.TargetCount != 3 {
				t.Errorf("greater_heal ability snapshot: TargetCount = %d, want 3", ab.TargetCount)
			}
			return
		}
	}
	t.Skip("greater_heal not in ability snapshot; skip TargetCount surface test")
}
