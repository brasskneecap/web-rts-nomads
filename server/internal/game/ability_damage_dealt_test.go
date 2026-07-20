package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// ═════════════════════════════════════════════════════════════════════════════
// on_damage_dealt — composable ability trigger tests
//
// Semantics under test: on_damage_dealt fires for the ATTACKING unit
// (DamageSource.AttackerUnitID), for every ability it knows whose program
// declares a matching trigger — see fireOnDamageDealtLocked's doc comment
// (ability_damage_dealt.go). DamageScope (nil/omitted ⇒ any damage) is the
// author-facing filter (ability_program.go).
//
// Registration: test abilities are injected into the runtimeAbilities overlay
// via registerRuntimeTestAbility (ability_cast_program_test.go) — same
// mechanism ability_unit_death_test.go uses, no disk I/O.
// ═════════════════════════════════════════════════════════════════════════════

// onDamageDealtTrigger builds a bare on_damage_dealt trigger with the given
// id, scope (nil ⇒ any damage), and actions.
func onDamageDealtTrigger(id string, scope *DamageTriggerScope, actions ...AbilityActionDef) AbilityTriggerDef {
	return AbilityTriggerDef{ID: id, Type: TriggerOnDamageDealt, DamageScope: scope, Actions: actions}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 1 — no DamageScope fires on ANY category (basic attack AND ability).
// ─────────────────────────────────────────────────────────────────────────────
func TestAbilityOnDamageDealt_NoScopeFiresOnAnyCategory(t *testing.T) {
	s := newDeathPipelineState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	ability := programAbility("test_dmgdealt_any", onDamageDealtTrigger("react", nil))
	registerRuntimeTestAbility(t, ability)

	attacker := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 0, Y: 0})
	attacker.Visible = true
	attacker.Abilities = []string{ability.ID}

	victim := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 50, Y: 0})
	victim.MaxHP, victim.HP = 1000, 1000
	victim.Visible = true

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	s.applyUnitDamageWithSourceLocked(victim, 10, DamageSource{
		AttackerUnitID: attacker.ID, Kind: "melee", Category: DamageCategoryBasicAttack,
	})
	s.applyUnitDamageWithSourceLocked(victim, 10, DamageSource{
		AttackerUnitID: attacker.ID, Kind: "ability", Category: DamageCategoryAbility, SourceAbilityID: "some_other_ability",
	})

	if got := traceTriggerFireCount(tr, "react"); got != 2 {
		t.Fatalf("no-scope trigger fired %d times, want exactly 2 (once per damage instance)", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 2 — categories:["basic_attack"] fires on a melee auto-attack, does NOT
// fire on ability damage.
// ─────────────────────────────────────────────────────────────────────────────
func TestAbilityOnDamageDealt_BasicAttackScope_OnlyFiresOnBasicAttack(t *testing.T) {
	s := newDeathPipelineState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	scope := &DamageTriggerScope{Categories: []DamageCategory{DamageCategoryBasicAttack}}
	ability := programAbility("test_dmgdealt_basic", onDamageDealtTrigger("react", scope))
	registerRuntimeTestAbility(t, ability)

	attacker := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 0, Y: 0})
	attacker.Visible = true
	attacker.Abilities = []string{ability.ID}

	victim := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 50, Y: 0})
	victim.MaxHP, victim.HP = 1000, 1000
	victim.Visible = true

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	s.applyUnitDamageWithSourceLocked(victim, 10, DamageSource{
		AttackerUnitID: attacker.ID, Kind: "melee", Category: DamageCategoryBasicAttack,
	})
	if got := traceTriggerFireCount(tr, "react"); got != 1 {
		t.Fatalf("basic_attack-scoped trigger fired %d times on a basic attack, want exactly 1", got)
	}

	s.applyUnitDamageWithSourceLocked(victim, 10, DamageSource{
		AttackerUnitID: attacker.ID, Kind: "ability", Category: DamageCategoryAbility, SourceAbilityID: "some_other_ability",
	})
	if got := traceTriggerFireCount(tr, "react"); got != 1 {
		t.Fatalf("basic_attack-scoped trigger fired %d times total after ability damage, want still exactly 1 (must not fire on ability damage)", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 3 — categories:["ability"] is the inverse of Test 2.
// ─────────────────────────────────────────────────────────────────────────────
func TestAbilityOnDamageDealt_AbilityScope_OnlyFiresOnAbilityDamage(t *testing.T) {
	s := newDeathPipelineState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	scope := &DamageTriggerScope{Categories: []DamageCategory{DamageCategoryAbility}}
	ability := programAbility("test_dmgdealt_ability_cat", onDamageDealtTrigger("react", scope))
	registerRuntimeTestAbility(t, ability)

	attacker := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 0, Y: 0})
	attacker.Visible = true
	attacker.Abilities = []string{ability.ID}

	victim := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 50, Y: 0})
	victim.MaxHP, victim.HP = 1000, 1000
	victim.Visible = true

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	s.applyUnitDamageWithSourceLocked(victim, 10, DamageSource{
		AttackerUnitID: attacker.ID, Kind: "melee", Category: DamageCategoryBasicAttack,
	})
	if got := traceTriggerFireCount(tr, "react"); got != 0 {
		t.Fatalf("ability-scoped trigger fired %d times on a basic attack, want exactly 0", got)
	}

	s.applyUnitDamageWithSourceLocked(victim, 10, DamageSource{
		AttackerUnitID: attacker.ID, Kind: "ability", Category: DamageCategoryAbility, SourceAbilityID: "some_other_ability",
	})
	if got := traceTriggerFireCount(tr, "react"); got != 1 {
		t.Fatalf("ability-scoped trigger fired %d times on ability damage, want exactly 1", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 4 — abilityId:"X" fires only for damage carrying that exact
// SourceAbilityID, regardless of category.
// ─────────────────────────────────────────────────────────────────────────────
func TestAbilityOnDamageDealt_AbilityIDScope_OnlyFiresForThatAbility(t *testing.T) {
	s := newDeathPipelineState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	const watchedAbilityID = "test_dmgdealt_watched_source"
	scope := &DamageTriggerScope{AbilityID: watchedAbilityID}
	ability := programAbility("test_dmgdealt_abilityid", onDamageDealtTrigger("react", scope))
	registerRuntimeTestAbility(t, ability)

	attacker := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 0, Y: 0})
	attacker.Visible = true
	attacker.Abilities = []string{ability.ID}

	victim := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 50, Y: 0})
	victim.MaxHP, victim.HP = 1000, 1000
	victim.Visible = true

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	// A different ability's damage: must NOT fire.
	s.applyUnitDamageWithSourceLocked(victim, 10, DamageSource{
		AttackerUnitID: attacker.ID, Kind: "ability", Category: DamageCategoryAbility, SourceAbilityID: "some_other_ability",
	})
	if got := traceTriggerFireCount(tr, "react"); got != 0 {
		t.Fatalf("abilityId-scoped trigger fired %d times for an unrelated ability's damage, want exactly 0", got)
	}

	// A plain basic attack (no SourceAbilityID at all): must NOT fire.
	s.applyUnitDamageWithSourceLocked(victim, 10, DamageSource{
		AttackerUnitID: attacker.ID, Kind: "melee", Category: DamageCategoryBasicAttack,
	})
	if got := traceTriggerFireCount(tr, "react"); got != 0 {
		t.Fatalf("abilityId-scoped trigger fired %d times for a basic attack, want exactly 0", got)
	}

	// The watched ability's own damage: must fire.
	s.applyUnitDamageWithSourceLocked(victim, 10, DamageSource{
		AttackerUnitID: attacker.ID, Kind: "ability", Category: DamageCategoryAbility, SourceAbilityID: watchedAbilityID,
	})
	if got := traceTriggerFireCount(tr, "react"); got != 1 {
		t.Fatalf("abilityId-scoped trigger fired %d times for the watched ability's damage, want exactly 1", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 5 — a unit with no ability declaring on_damage_dealt: nothing fires,
// and the cheap-path membership check itself reports false without ever
// resolving/scanning the (unrelated) ability's Program.
// ─────────────────────────────────────────────────────────────────────────────
func TestAbilityOnDamageDealt_NoQualifyingAbility_NothingFires(t *testing.T) {
	s := newDeathPipelineState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// An ability the attacker owns, but it declares NO on_damage_dealt trigger.
	unrelated := programAbility("test_dmgdealt_unrelated", AbilityTriggerDef{ID: "on_cast", Type: TriggerOnCastComplete})
	registerRuntimeTestAbility(t, unrelated)

	if abilityDeclaresOnDamageDealtTrigger(unrelated.ID) {
		t.Fatalf("abilityDeclaresOnDamageDealtTrigger(%q) = true, want false — this ability declares no on_damage_dealt trigger", unrelated.ID)
	}

	attacker := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 0, Y: 0})
	attacker.Visible = true
	attacker.Abilities = []string{unrelated.ID}

	victim := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 50, Y: 0})
	victim.MaxHP, victim.HP = 1000, 1000
	victim.Visible = true

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	before := len(tr.Events)
	s.applyUnitDamageWithSourceLocked(victim, 10, DamageSource{
		AttackerUnitID: attacker.ID, Kind: "melee", Category: DamageCategoryBasicAttack,
	})
	if len(tr.Events) != before {
		t.Fatalf("damage on a unit with no qualifying ability produced %d new trace events, want 0 (fireOnDamageDealtLocked must be a true no-op here)", len(tr.Events)-before)
	}
	if attacker.OnDamageDealtDispatchActive {
		t.Fatal("OnDamageDealtDispatchActive left set after a no-op dispatch")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 6 — re-entrancy: an on_damage_dealt trigger whose own action deals
// MORE damage (attributed to the same attacker, exactly like every deal_damage
// action stamps AttackerUnitID = ctx.CasterID) must not recurse. The guard
// (Unit.OnDamageDealtDispatchActive) blocks the nested re-entry, so the
// trigger fires exactly once for the original hit and the reaction's own
// damage lands without triggering a second reaction.
// ─────────────────────────────────────────────────────────────────────────────
func TestAbilityOnDamageDealt_SelfTriggeringActionDoesNotRecurse(t *testing.T) {
	s := newDeathPipelineState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	const reactionAmount = 20
	ability := programAbility("test_dmgdealt_reentrant", currentEventDamageTrigger("react", TriggerOnDamageDealt, reactionAmount))
	registerRuntimeTestAbility(t, ability)

	attacker := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 0, Y: 0})
	attacker.Visible = true
	attacker.Abilities = []string{ability.ID}

	victim := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 50, Y: 0})
	victim.MaxHP, victim.HP = 1000, 1000
	victim.Visible = true

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	const initialAmount = 10
	landed := s.applyUnitDamageWithSourceLocked(victim, initialAmount, DamageSource{
		AttackerUnitID: attacker.ID, Kind: "melee", Category: DamageCategoryBasicAttack,
	})
	if landed != initialAmount {
		t.Fatalf("setup error: initial hit landed %d, want %d (unmitigated)", landed, initialAmount)
	}

	// Exactly one fire — the guard must have blocked the reaction's own
	// deal_damage from re-triggering a second fire for the same attacker.
	if got := traceTriggerFireCount(tr, "react"); got != 1 {
		t.Fatalf("self-triggering on_damage_dealt fired %d times, want exactly 1 (re-entrancy guard did not bound recursion)", got)
	}
	// The reaction damage must still have LANDED (the guard blocks the
	// re-dispatch, not the deal_damage action itself).
	wantVictimHP := 1000 - initialAmount - reactionAmount
	if victim.HP != wantVictimHP {
		t.Fatalf("victim HP = %d, want %d (initial %d + reaction %d both landed)", victim.HP, wantVictimHP, initialAmount, reactionAmount)
	}
	// The guard must be cleared after the (bounded) dispatch completes, so a
	// LATER, independent hit can fire normally.
	if attacker.OnDamageDealtDispatchActive {
		t.Fatal("OnDamageDealtDispatchActive left set (true) after fireOnDamageDealtLocked returned — guard not released")
	}
	s.applyUnitDamageWithSourceLocked(victim, initialAmount, DamageSource{
		AttackerUnitID: attacker.ID, Kind: "melee", Category: DamageCategoryBasicAttack,
	})
	if got := traceTriggerFireCount(tr, "react"); got != 2 {
		t.Fatalf("a later, independent hit fired %d times total, want 2 (guard must not permanently latch)", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 7 — determinism: when a unit knows two abilities that both declare a
// matching on_damage_dealt trigger, they run in ABILITY-ID order, not
// Unit.Abilities slot order. The slot order below is deliberately the
// REVERSE of id order to disprove any slot-order dependence.
// ─────────────────────────────────────────────────────────────────────────────
func TestAbilityOnDamageDealt_MultipleAbilities_RunInIDOrder(t *testing.T) {
	s := newDeathPipelineState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	abilityZ := programAbility("test_dmgdealt_zzz", onDamageDealtTrigger("on_z", nil))
	abilityA := programAbility("test_dmgdealt_aaa", onDamageDealtTrigger("on_a", nil))
	registerRuntimeTestAbility(t, abilityZ)
	registerRuntimeTestAbility(t, abilityA)
	if abilityA.ID >= abilityZ.ID {
		t.Fatalf("test setup error: want abilityA.ID (%q) < abilityZ.ID (%q)", abilityA.ID, abilityZ.ID)
	}

	attacker := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 0, Y: 0})
	attacker.Visible = true
	// Slot order: Z before A — the reverse of ability-id order.
	attacker.Abilities = []string{abilityZ.ID, abilityA.ID}

	victim := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 50, Y: 0})
	victim.MaxHP, victim.HP = 1000, 1000
	victim.Visible = true

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	s.applyUnitDamageWithSourceLocked(victim, 10, DamageSource{
		AttackerUnitID: attacker.ID, Kind: "melee", Category: DamageCategoryBasicAttack,
	})

	var order []string
	for _, e := range tr.Events {
		if e.Type == "trigger_fired" && (e.Path == "on_a" || e.Path == "on_z") {
			order = append(order, e.Path)
		}
	}
	if len(order) != 2 {
		t.Fatalf("got %d trigger_fired events for on_a/on_z, want exactly 2 (trace: %+v)", len(order), tr.Events)
	}
	if order[0] != "on_a" || order[1] != "on_z" {
		t.Fatalf("fire order = %v, want [on_a on_z] (ability-id order: %q < %q), not Unit.Abilities slot order", order, abilityA.ID, abilityZ.ID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 8 — validation: damageScope is on_damage_dealt-only.
// ─────────────────────────────────────────────────────────────────────────────
func TestValidateProgram_DamageScope_RejectedOnNonDamageDealtTrigger(t *testing.T) {
	prog := &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryUnit},
		Triggers: []AbilityTriggerDef{
			{ID: "t1", Type: TriggerOnCastComplete, DamageScope: &DamageTriggerScope{}, Actions: []AbilityActionDef{
				{ID: "a", Type: ActionSelectTargets, Target: &TargetQueryDef{Source: SrcCaster}},
			}},
		},
	}
	if !hasCode(validateAbilityProgram(prog), "invalid_damage_scope_placement") {
		t.Error("want invalid_damage_scope_placement for damageScope on a non-on_damage_dealt trigger")
	}
}

// Test 8b — an unknown/unspecified DamageCategory in Categories is rejected.
func TestValidateProgram_DamageScope_UnknownCategoryRejected(t *testing.T) {
	prog := &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryUnit},
		Triggers: []AbilityTriggerDef{
			{ID: "t1", Type: TriggerOnDamageDealt, DamageScope: &DamageTriggerScope{
				Categories: []DamageCategory{"not_a_real_category"},
			}, Actions: []AbilityActionDef{
				{ID: "a", Type: ActionSelectTargets, Target: &TargetQueryDef{Source: SrcCurrentEvent}},
			}},
		},
	}
	if !hasCode(validateAbilityProgram(prog), "unknown_damage_category") {
		t.Error("want unknown_damage_category for a garbage category string")
	}

	// DamageCategoryUnspecified ("") is a gap marker, never an authorable
	// filter value — must ALSO be rejected, not silently accepted as "any".
	progUnspecified := &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryUnit},
		Triggers: []AbilityTriggerDef{
			{ID: "t1", Type: TriggerOnDamageDealt, DamageScope: &DamageTriggerScope{
				Categories: []DamageCategory{DamageCategoryUnspecified},
			}, Actions: []AbilityActionDef{
				{ID: "a", Type: ActionSelectTargets, Target: &TargetQueryDef{Source: SrcCurrentEvent}},
			}},
		},
	}
	if !hasCode(validateAbilityProgram(progUnspecified), "unknown_damage_category") {
		t.Error("want unknown_damage_category for DamageCategoryUnspecified (\"\") — it is a gap marker, not an authorable value")
	}
}

// Test 8c — AbilityID paired with a Categories list that excludes "ability"
// is self-contradictory (ability-attributed damage always carries Category
// "ability") and must be rejected.
func TestValidateProgram_DamageScope_ContradictoryAbilityIDCategoriesRejected(t *testing.T) {
	prog := &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryUnit},
		Triggers: []AbilityTriggerDef{
			{ID: "t1", Type: TriggerOnDamageDealt, DamageScope: &DamageTriggerScope{
				AbilityID:  "some_ability",
				Categories: []DamageCategory{DamageCategoryBasicAttack},
			}, Actions: []AbilityActionDef{
				{ID: "a", Type: ActionSelectTargets, Target: &TargetQueryDef{Source: SrcCurrentEvent}},
			}},
		},
	}
	if !hasCode(validateAbilityProgram(prog), "contradictory_damage_scope") {
		t.Error("want contradictory_damage_scope for abilityId + categories excluding \"ability\"")
	}
}

// Test 8d — valid combinations must NOT be flagged by any of the three new
// checks (no false positives).
func TestValidateProgram_DamageScope_ValidCombinationsAccepted(t *testing.T) {
	cases := []struct {
		name  string
		scope *DamageTriggerScope
	}{
		{"nil (any)", nil},
		{"empty struct (any)", &DamageTriggerScope{}},
		{"categories only", &DamageTriggerScope{Categories: []DamageCategory{DamageCategoryBasicAttack}}},
		{"abilityId only", &DamageTriggerScope{AbilityID: "some_ability"}},
		{"abilityId + ability category", &DamageTriggerScope{AbilityID: "some_ability", Categories: []DamageCategory{DamageCategoryAbility}}},
		{"abilityId + ability category + others", &DamageTriggerScope{AbilityID: "some_ability", Categories: []DamageCategory{DamageCategoryAbility, DamageCategoryPerk}}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			prog := &AbilityProgram{
				Entry: AbilityEntryDef{Type: EntryUnit},
				Triggers: []AbilityTriggerDef{
					{ID: "t1", Type: TriggerOnDamageDealt, DamageScope: c.scope, Actions: []AbilityActionDef{
						{ID: "a", Type: ActionSelectTargets, Target: &TargetQueryDef{Source: SrcCurrentEvent}},
					}},
				},
			}
			for _, code := range []string{"invalid_damage_scope_placement", "unknown_damage_category", "contradictory_damage_scope"} {
				if hasCode(validateAbilityProgram(prog), code) {
					t.Errorf("valid scope %+v was flagged with %q", c.scope, code)
				}
			}
		})
	}
}
