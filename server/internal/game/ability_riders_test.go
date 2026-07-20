package game

import (
	"encoding/json"
	"testing"
)

// TestPerkDef_AbilityRiders_DecodesTargetTriggerAndActions verifies that a
// PerkDef decoded from JSON containing an "abilityRiders" entry populates
// PerkDef.AbilityRiders with the target ability id, the trigger the rider's
// actions graft onto, and the actions themselves — unmodified.
func TestPerkDef_AbilityRiders_DecodesTargetTriggerAndActions(t *testing.T) {
	raw := `{
		"id": "test_perk",
		"displayName": "Test Perk",
		"abilityRiders": [
			{
				"target": "siphon_life",
				"trigger": "on_tick",
				"actions": [
					{"id": "rider_dmg", "type": "deal_damage", "config": {"amount": 5}}
				]
			}
		]
	}`

	var def PerkDef
	if err := json.Unmarshal([]byte(raw), &def); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(def.AbilityRiders) != 1 {
		t.Fatalf("want 1 rider, got %d", len(def.AbilityRiders))
	}
	rider := def.AbilityRiders[0]
	if rider.Target != "siphon_life" {
		t.Errorf("target = %q, want siphon_life", rider.Target)
	}
	if rider.Trigger != TriggerOnTick {
		t.Errorf("trigger = %q, want %q", rider.Trigger, TriggerOnTick)
	}
	if len(rider.Actions) != 1 {
		t.Fatalf("want 1 action, got %d", len(rider.Actions))
	}
	action := rider.Actions[0]
	if action.ID != "rider_dmg" {
		t.Errorf("action.ID = %q, want rider_dmg", action.ID)
	}
	if action.Type != ActionDealDamage {
		t.Errorf("action.Type = %q, want %q", action.Type, ActionDealDamage)
	}
	var cfg dealDamageConfig
	if err := json.Unmarshal(action.Config, &cfg); err != nil {
		t.Fatalf("decode preserved action config: %v", err)
	}
	if cfg.Amount != 5 {
		t.Errorf("action config amount = %d, want 5", cfg.Amount)
	}
}

// validPerkWithRider returns a base PerkDef whose only variable is the
// supplied rider, so each validation subtest exercises exactly one failure
// mode in isolation.
func validPerkWithRider(rider AbilityRider) *PerkDef {
	return &PerkDef{
		ID:            "test_perk",
		DisplayName:   "Test Perk",
		AbilityRiders: []AbilityRider{rider},
	}
}

func TestValidatePerkDef_AbilityRiders_RejectsEmptyTarget(t *testing.T) {
	def := validPerkWithRider(AbilityRider{
		Target:  "",
		Trigger: TriggerOnTick,
		Actions: []AbilityActionDef{
			{ID: "a", Type: ActionDealDamage, Config: json.RawMessage(`{"amount":5}`)},
		},
	})
	if err := validatePerkDef(def); err == nil {
		t.Fatal("want error for empty rider target, got nil")
	}
}

func TestValidatePerkDef_AbilityRiders_RejectsUnknownTrigger(t *testing.T) {
	def := validPerkWithRider(AbilityRider{
		Target:  "siphon_life",
		Trigger: TriggerType("not_a_real_trigger"),
		Actions: []AbilityActionDef{
			{ID: "a", Type: ActionDealDamage, Config: json.RawMessage(`{"amount":5}`)},
		},
	})
	if err := validatePerkDef(def); err == nil {
		t.Fatal("want error for unknown rider trigger, got nil")
	}
}

func TestValidatePerkDef_AbilityRiders_RejectsInvalidAction(t *testing.T) {
	def := validPerkWithRider(AbilityRider{
		Target:  "siphon_life",
		Trigger: TriggerOnTick,
		Actions: []AbilityActionDef{
			{ID: "a", Type: "no_such_action"},
		},
	})
	if err := validatePerkDef(def); err == nil {
		t.Fatal("want error for unsupported action type inside rider, got nil")
	}
}

func TestValidatePerkDef_AbilityRiders_AcceptsValidRider(t *testing.T) {
	def := validPerkWithRider(AbilityRider{
		Target:  "siphon_life",
		Trigger: TriggerOnTick,
		Actions: []AbilityActionDef{
			{ID: "a", Type: ActionDealDamage, Config: json.RawMessage(`{"amount":5}`)},
		},
	})
	if err := validatePerkDef(def); err != nil {
		t.Fatalf("want valid rider to pass validation, got: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Runtime (T2): ownedRiderFragmentsForLocked / runAbilityRidersForCasterLocked
// ─────────────────────────────────────────────────────────────────────────────

// riderDealDamageAction returns a single-action rider that deals amount
// damage to the ability's initial target — mirrors the SrcInitialTarget
// pattern used throughout the executor tests (ability_exec_beam_test.go,
// ability_compile.go's compiled channel-tick trigger).
func riderDealDamageAction(id string, amount int) []AbilityActionDef {
	return []AbilityActionDef{
		{
			ID:     id,
			Type:   ActionDealDamage,
			Target: &TargetQueryDef{Source: SrcInitialTarget},
			Config: marshalConfig(dealDamageConfig{Amount: amount}),
		},
	}
}

// TestOwnedRiderFragmentsForLocked_NilSafe covers the nil-caster and
// empty-abilityID guards.
func TestOwnedRiderFragmentsForLocked_NilSafe(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	if got := s.ownedRiderFragmentsForLocked(nil, "siphon_life", TriggerOnTick); got != nil {
		t.Fatalf("nil caster: got %v, want nil", got)
	}
	caster := &Unit{PerkIDs: []string{"test_rider_perk_alpha"}}
	if got := s.ownedRiderFragmentsForLocked(caster, "", TriggerOnTick); got != nil {
		t.Fatalf("empty abilityID: got %v, want nil", got)
	}
}

// TestRunAbilityRidersForCasterLocked_ComposesAndOrdersByPerkID gives the
// caster TWO owned perks, each carrying a rider on the SAME
// (siphon_life, on_beam_tick) that deals a distinct amount of damage to the
// initial target. It asserts:
//  1. Composition — both fragments' damage lands from one
//     runAbilityRidersForCasterLocked call.
//  2. Determinism — with caster.PerkIDs authored in the REVERSE of perk-id
//     sort order, the fragments still execute in perk-id-sorted order (proven
//     via the trace, not caster.PerkIDs order).
func TestRunAbilityRidersForCasterLocked_ComposesAndOrdersByPerkID(t *testing.T) {
	withIsolatedPerkCatalogDir(t)

	// perk ids deliberately chosen so alpha < beta by string sort.
	const (
		alphaPerkID = "test_rider_perk_alpha"
		betaPerkID  = "test_rider_perk_beta"
		alphaDamage = 7
		betaDamage  = 13
	)
	alphaPerk := &PerkDef{
		ID:          alphaPerkID,
		DisplayName: "Test Rider Perk Alpha",
		AbilityRiders: []AbilityRider{
			{Target: "siphon_life", Trigger: TriggerOnTick, Actions: riderDealDamageAction("alpha_dmg", alphaDamage)},
		},
	}
	if err := SavePerkDef(alphaPerk); err != nil {
		t.Fatalf("SavePerkDef(alphaPerk): %v", err)
	}
	betaPerk := &PerkDef{
		ID:          betaPerkID,
		DisplayName: "Test Rider Perk Beta",
		AbilityRiders: []AbilityRider{
			{Target: "siphon_life", Trigger: TriggerOnTick, Actions: riderDealDamageAction("beta_dmg", betaDamage)},
		},
	}
	if err := SavePerkDef(betaPerk); err != nil {
		t.Fatalf("SavePerkDef(betaPerk): %v", err)
	}

	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	// Authored in the REVERSE of perk-id sort order (beta before alpha) —
	// execution order must NOT follow this.
	caster.PerkIDs = []string{betaPerkID, alphaPerkID}
	target := spawnProjTestUnit(t, s, enemyPlayerID, 100, 0)
	target.HP = 500
	target.MaxHP = 500

	// ownedRiderFragmentsForLocked itself must return fragments in perk-id
	// order regardless of caster.PerkIDs order.
	frags := s.ownedRiderFragmentsForLocked(caster, "siphon_life", TriggerOnTick)
	if len(frags) != 2 {
		t.Fatalf("want 2 fragments, got %d", len(frags))
	}
	var firstCfg, secondCfg dealDamageConfig
	if err := json.Unmarshal(frags[0].Actions[0].Config, &firstCfg); err != nil {
		t.Fatalf("decode frags[0] config: %v", err)
	}
	if err := json.Unmarshal(frags[1].Actions[0].Config, &secondCfg); err != nil {
		t.Fatalf("decode frags[1] config: %v", err)
	}
	if firstCfg.Amount != alphaDamage || secondCfg.Amount != betaDamage {
		t.Fatalf("fragment order = [%d, %d], want [%d, %d] (perk-id sorted: alpha before beta)", firstCfg.Amount, secondCfg.Amount, alphaDamage, betaDamage)
	}

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	s.runAbilityRidersForCasterLocked(caster, target, "siphon_life", TriggerOnTick, 999)

	// Composition: both hits landed.
	wantHP := 500 - alphaDamage - betaDamage
	if target.HP != wantHP {
		t.Fatalf("target.HP = %d, want %d (both %d and %d rider hits must land)", target.HP, wantHP, alphaDamage, betaDamage)
	}

	// Determinism: the trace's damage_applied events must show alpha's path
	// firing before beta's, even though caster.PerkIDs listed beta first.
	var order []string
	for _, ev := range tr.Events {
		if ev.Type != "damage_applied" {
			continue
		}
		order = append(order, ev.Path)
	}
	if len(order) != 2 {
		t.Fatalf("want 2 damage_applied trace events, got %d: %+v", len(order), tr.Events)
	}
	wantAlphaPath := "rider[" + alphaPerkID + "].actions[alpha_dmg]"
	wantBetaPath := "rider[" + betaPerkID + "].actions[beta_dmg]"
	if order[0] != wantAlphaPath || order[1] != wantBetaPath {
		t.Fatalf("trace order = %v, want [%q, %q] (alpha before beta, perk-id sorted)", order, wantAlphaPath, wantBetaPath)
	}
}

// TestRunAbilityRidersForCasterLocked_NoOpWhenNoMatchingRider covers the
// no-op paths: nil caster, nil target, and a caster who owns perks but none
// with a matching (abilityID, trigger) rider.
func TestRunAbilityRidersForCasterLocked_NoOpWhenNoMatchingRider(t *testing.T) {
	withIsolatedPerkCatalogDir(t)

	perk := &PerkDef{
		ID:          "test_rider_perk_gamma",
		DisplayName: "Test Rider Perk Gamma",
		AbilityRiders: []AbilityRider{
			{Target: "siphon_life", Trigger: TriggerOnTick, Actions: riderDealDamageAction("gamma_dmg", 9)},
		},
	}
	if err := SavePerkDef(perk); err != nil {
		t.Fatalf("SavePerkDef(perk): %v", err)
	}

	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	caster.PerkIDs = []string{"test_rider_perk_gamma"}
	target := spawnProjTestUnit(t, s, enemyPlayerID, 100, 0)
	target.HP = 500
	target.MaxHP = 500

	// Nil caster / target: must not panic.
	s.runAbilityRidersForCasterLocked(nil, target, "siphon_life", TriggerOnTick, 0)
	s.runAbilityRidersForCasterLocked(caster, nil, "siphon_life", TriggerOnTick, 0)
	if target.HP != 500 {
		t.Fatalf("target.HP = %d after nil-caster/target calls, want unchanged 500", target.HP)
	}

	// Wrong ability id: gamma's rider targets siphon_life, not this id.
	s.runAbilityRidersForCasterLocked(caster, target, "some_other_ability", TriggerOnTick, 0)
	if target.HP != 500 {
		t.Fatalf("target.HP = %d after non-matching abilityID, want unchanged 500", target.HP)
	}

	// Wrong trigger: gamma's rider fires on_beam_tick, not on_cast_complete.
	s.runAbilityRidersForCasterLocked(caster, target, "siphon_life", TriggerOnCastComplete, 0)
	if target.HP != 500 {
		t.Fatalf("target.HP = %d after non-matching trigger, want unchanged 500", target.HP)
	}
}
