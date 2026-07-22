package game

import (
	"encoding/json"
	"testing"
)

// TestHasPerkCondition covers the operator that lets an ability branch on a
// perk BY NAME, in its own program — the readability rule that replaced the
// capability indirection (docs/design/ability_perk_interaction.md).
func TestHasPerkCondition(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()
	caster := teamCombatUnit(t, s, "p1", 0, 0)

	cond := func(op, perkID string) []AbilityConditionDef {
		raw, _ := json.Marshal(perkID)
		return []AbilityConditionDef{{Op: op, Right: raw}}
	}
	ctx := &RuntimeAbilityContext{CasterID: caster.ID, Named: map[string]ContextValue{}}

	t.Run("false when the caster does not own the perk", func(t *testing.T) {
		caster.PerkIDs = nil
		if s.evaluateConditionsLocked(ctx, cond(condOpHasPerk, "lasting_flames")) {
			t.Error("has_perk should be false without the perk")
		}
		if !s.evaluateConditionsLocked(ctx, cond(condOpNotPerk, "lasting_flames")) {
			t.Error("not_perk should be true without the perk")
		}
	})

	t.Run("true when the caster owns it", func(t *testing.T) {
		caster.PerkIDs = []string{"lasting_flames"}
		if !s.evaluateConditionsLocked(ctx, cond(condOpHasPerk, "lasting_flames")) {
			t.Error("has_perk should be true with the perk owned")
		}
		if s.evaluateConditionsLocked(ctx, cond(condOpNotPerk, "lasting_flames")) {
			t.Error("not_perk should be false with the perk owned")
		}
	})

	t.Run("a different owned perk does not satisfy it", func(t *testing.T) {
		caster.PerkIDs = []string{"wider_nets"}
		if s.evaluateConditionsLocked(ctx, cond(condOpHasPerk, "lasting_flames")) {
			t.Error("has_perk must match the named perk exactly")
		}
	})

	t.Run("an empty perk id never matches", func(t *testing.T) {
		caster.PerkIDs = []string{"lasting_flames"}
		if s.evaluateConditionsLocked(ctx, cond(condOpHasPerk, "")) {
			t.Error("an empty perk id must not match")
		}
	})
}

// TestConditionalElseBranch covers the if/else shape: one node, two branches,
// so an inverted condition can never drift out of sync with a sibling.
func TestConditionalElseBranch(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()
	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 30, 0)
	enemy.HP, enemy.MaxHP = 500, 500

	// then: 10 damage · else: 40 damage — HP tells us which branch ran.
	cfg := `{"conditions":[{"op":"has_perk","right":"lasting_flames"}],
	         "then":[{"id":"t","type":"deal_damage","config":{"amount":10}}],
	         "else":[{"id":"e","type":"deal_damage","config":{"amount":40}}]}`

	t.Run("else runs when the condition fails", func(t *testing.T) {
		caster.PerkIDs = nil
		before := enemy.HP
		runOneActionProgram(t, s, caster.ID, enemy.ID, ActionConditional, cfg, []int{enemy.ID})
		if got := before - enemy.HP; got != 40 {
			t.Errorf("damage = %d, want 40 (else branch)", got)
		}
	})

	t.Run("then runs when the condition holds", func(t *testing.T) {
		caster.PerkIDs = []string{"lasting_flames"}
		before := enemy.HP
		runOneActionProgram(t, s, caster.ID, enemy.ID, ActionConditional, cfg, []int{enemy.ID})
		if got := before - enemy.HP; got != 10 {
			t.Errorf("damage = %d, want 10 (then branch)", got)
		}
	})
}

// TestCatalog_AbilityFieldTargetsResolve is the catalog-integrity backstop for
// the check deliberately skipped during load (catalog init order makes a
// cross-catalog lookup unreliable there): every shipped perk's, item's and unit
// type's ability-FIELD target must actually resolve, and every modifier must
// name an action + field its target ability's program really contains.
//
// This is what stops a perk from silently doing nothing after an ability is
// re-authored — the exact failure the parameter system was built to prevent and
// this replaces.
func TestCatalog_AbilityFieldTargetsResolve(t *testing.T) {
	checkTarget := func(t *testing.T, label, target string) {
		t.Helper()
		if _, isTag := strippedTagTarget(target); isTag {
			return // a tag may legitimately match nothing yet
		}
		if _, ok := getAbilityDef(target); !ok {
			t.Errorf("%s targets unknown ability %q", label, target)
		}
	}
	check := func(t *testing.T, label string, mods []AbilityFieldModifier) {
		t.Helper()
		for _, m := range mods {
			checkTarget(t, label+" abilityFields", m.Target)
		}
		if err := validateAbilityFieldModifiers(label, mods); err != nil {
			t.Errorf("%v", err)
		}
	}
	for _, def := range ListPerkDefs() {
		check(t, "perk \""+def.ID+"\"", def.AbilityFields)
	}
	for _, def := range ListItemDefs() {
		check(t, "item \""+def.ID+"\"", def.AbilityFields)
	}
	for _, def := range ListUnitDefs() {
		check(t, "unit \""+def.Type+"\"", def.AbilityFields)
	}
}
