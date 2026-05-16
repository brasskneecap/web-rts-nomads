package game

import (
	"encoding/json"
	"testing"
)

// ── Targeting validation (self / ally / enemy) ───────────────────────────────

func TestAbility_TargetingValidation(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 100, 100)
	ally := spawnProjTestUnit(t, s, "p1", 120, 100)
	// enemyPlayerID is always-hostile / never-friendly regardless of team,
	// so it is the team-model-correct way to express "an enemy unit".
	enemy := spawnProjTestUnit(t, s, enemyPlayerID, 140, 100)
	deadAlly := spawnProjTestUnit(t, s, "p1", 160, 100)
	deadAlly.HP = 0

	// Heal-like: self + allies, never enemies.
	heal := AbilityDef{CanTargetSelf: true, CanTargetAllies: true, CanTargetEnemies: false}
	cases := []struct {
		name   string
		target *Unit
		want   bool
	}{
		{"self", caster, true},
		{"ally", ally, true},
		{"enemy", enemy, false},
		{"dead ally", deadAlly, false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		if got := s.canAbilityTargetUnitLocked(heal, caster, c.target); got != c.want {
			t.Errorf("heal canAbilityTargetUnitLocked(%s) = %v; want %v", c.name, got, c.want)
		}
	}

	// Offensive ability: enemies only.
	bolt := AbilityDef{CanTargetEnemies: true}
	if !s.canAbilityTargetUnitLocked(bolt, caster, enemy) {
		t.Error("offensive ability should be able to target an enemy")
	}
	if s.canAbilityTargetUnitLocked(bolt, caster, ally) || s.canAbilityTargetUnitLocked(bolt, caster, caster) {
		t.Error("offensive ability must not target allies or self")
	}

	// Nil caster is never valid.
	if s.canAbilityTargetUnitLocked(heal, nil, ally) {
		t.Error("nil caster must invalidate targeting")
	}
}

// ── Cast range validation (incl. match_attack_range) ─────────────────────────

func TestAbility_CastRangeValidation(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	caster.AttackRange = 220 // archer-ish

	near := spawnProjTestUnit(t, s, "p1", 200, 0) // 200 px away
	far := spawnProjTestUnit(t, s, "p1", 300, 0)  // 300 px away

	// Fixed numeric range.
	fixed := AbilityDef{CastRange: 250, CanTargetAllies: true}
	if !fixed.WithinCastRange(caster, near) {
		t.Error("target 200px away should be within a 250px cast range")
	}
	if fixed.WithinCastRange(caster, far) {
		t.Error("target 300px away should be outside a 250px cast range")
	}

	// match_attack_range mirrors the caster's AttackRange (220).
	matched := AbilityDef{CastRange: CastRangeMatchAttackRange, CanTargetAllies: true}
	if !matched.CastRange.MatchesAttackRange() {
		t.Error("CastRangeMatchAttackRange must report MatchesAttackRange()")
	}
	if got := matched.CastRange.Resolve(caster); got != 220 {
		t.Errorf("match cast range Resolve() = %v; want caster AttackRange 220", got)
	}
	if !matched.WithinCastRange(caster, near) {
		t.Error("200px away should be within match_attack_range (220)")
	}
	if matched.WithinCastRange(caster, far) {
		t.Error("300px away should be outside match_attack_range (220)")
	}

	// Self is always in range; zero/negative concrete range can't reach others.
	if !fixed.WithinCastRange(caster, caster) {
		t.Error("a unit should always be within cast range of itself")
	}
	zero := AbilityDef{CastRange: 0}
	if zero.WithinCastRange(caster, near) {
		t.Error("a zero cast range cannot reach a distinct target")
	}
	if got := matched.CastRange.Resolve(nil); got != 0 {
		t.Errorf("match Resolve(nil caster) = %v; want 0", got)
	}
}

// ── All attributes load correctly from an ability definition ─────────────────

func TestAbility_AllAttributesLoadFromJSON(t *testing.T) {
	raw := `{
	  "id": "heal",
	  "displayName": "Heal",
	  "type": "spell",
	  "canTargetSelf": true,
	  "canTargetAllies": true,
	  "canTargetEnemies": false,
	  "castRange": "match_attack_range",
	  "castTime": 1.0,
	  "manaCost": 5,
	  "cooldown": 0,
	  "damageType": "holy",
	  "supportsAutoCast": true,
	  "autoCastTargetSelector": "lowest_hp_percentage_ally_in_range",
	  "icon": "TODO/heal.png",
	  "casterAnimation": "Casting",
	  "effectOnTarget": "healing_glow"
	}`

	var def AbilityDef
	if err := json.Unmarshal([]byte(raw), &def); err != nil {
		t.Fatalf("unmarshal AbilityDef: %v", err)
	}

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"ID", def.ID, "heal"},
		{"DisplayName", def.DisplayName, "Heal"},
		{"Type", def.Type, AbilitySpell},
		{"CanTargetSelf", def.CanTargetSelf, true},
		{"CanTargetAllies", def.CanTargetAllies, true},
		{"CanTargetEnemies", def.CanTargetEnemies, false},
		{"CastTime", def.CastTime, 1.0},
		{"ManaCost", def.ManaCost, 5},
		{"Cooldown", def.Cooldown, 0.0},
		{"DamageType", def.DamageType, DamageHoly},
		{"SupportsAutoCast", def.SupportsAutoCast, true},
		{"AutoCastTargetSelector", def.AutoCastTargetSelector, "lowest_hp_percentage_ally_in_range"},
		{"Icon", def.Icon, "TODO/heal.png"},
		{"CasterAnimation", def.CasterAnimation, "Casting"},
		{"EffectOnTarget", def.EffectOnTarget, "healing_glow"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v; want %v", c.name, c.got, c.want)
		}
	}
	if !def.CastRange.MatchesAttackRange() {
		t.Error(`castRange:"match_attack_range" should report MatchesAttackRange()`)
	}
	if def.DamageType.OrPhysical() != DamageHoly {
		t.Errorf("DamageType.OrPhysical() = %v; want holy", def.DamageType.OrPhysical())
	}
	if def.CasterAnimationOrCasting() != "Casting" {
		t.Errorf("CasterAnimationOrCasting() = %q; want Casting", def.CasterAnimationOrCasting())
	}

	// Numeric / -1 cast range forms also load; -1 is the match sentinel.
	var numeric AbilityDef
	if err := json.Unmarshal([]byte(`{"id":"x","castRange":250}`), &numeric); err != nil {
		t.Fatalf("numeric castRange unmarshal: %v", err)
	}
	if numeric.CastRange.MatchesAttackRange() || float64(numeric.CastRange) != 250 {
		t.Errorf("numeric castRange mis-parsed: %v", numeric.CastRange)
	}
	var negOne AbilityDef
	if err := json.Unmarshal([]byte(`{"id":"x","castRange":-1}`), &negOne); err != nil {
		t.Fatalf("-1 castRange unmarshal: %v", err)
	}
	if !negOne.CastRange.MatchesAttackRange() {
		t.Error("castRange:-1 should be the match-attack-range sentinel")
	}

	// An unrecognised castRange string is a hard authoring error.
	var bad AbilityDef
	if err := json.Unmarshal([]byte(`{"id":"x","castRange":"forever"}`), &bad); err == nil {
		t.Error(`castRange:"forever" should fail to unmarshal`)
	}

	// CasterAnimation defaults to "Casting" when omitted.
	var noAnim AbilityDef
	_ = json.Unmarshal([]byte(`{"id":"x"}`), &noAnim)
	if noAnim.CasterAnimationOrCasting() != unitStatusCasting {
		t.Errorf("default caster animation = %q; want %q", noAnim.CasterAnimationOrCasting(), unitStatusCasting)
	}
}

// ── Catalog loads the authored Heal ability (Part 8) ─────────────────────────
//
// (Part 6 originally asserted the catalog was empty; Part 8 authored
// catalog/abilities/heal/heal.json, so this now verifies that real catalog
// file loads with every attribute intact — the catalog-backed counterpart to
// TestAbility_AllAttributesLoadFromJSON.)

func TestAbility_HealLoadsFromCatalog(t *testing.T) {
	def, ok := getAbilityDef("heal")
	if !ok {
		t.Fatal(`getAbilityDef("heal") = _, false; want the catalog-authored Heal`)
	}
	if def.ID != "heal" || def.DisplayName != "Heal" || def.Type != AbilitySpell {
		t.Errorf("identity: id=%q name=%q type=%q", def.ID, def.DisplayName, def.Type)
	}
	if def.ManaCost != 5 || def.HealAmount != 5 || def.CastTime != 1.0 || def.Cooldown != 0 {
		t.Errorf("cost/timing: mana=%d heal=%d castTime=%v cd=%v", def.ManaCost, def.HealAmount, def.CastTime, def.Cooldown)
	}
	if !def.CastRange.MatchesAttackRange() {
		t.Error("heal castRange should be match_attack_range")
	}
	if def.DamageType != DamageHoly {
		t.Errorf("damageType = %q; want holy", def.DamageType)
	}
	if !def.CanTargetSelf || !def.CanTargetAllies || def.CanTargetEnemies {
		t.Errorf("targeting flags wrong: self=%v allies=%v enemies=%v", def.CanTargetSelf, def.CanTargetAllies, def.CanTargetEnemies)
	}
	if !def.SupportsAutoCast || def.AutoCastTargetSelector != "lowest_hp_percentage_ally_in_range" {
		t.Errorf("auto-cast: supports=%v selector=%q", def.SupportsAutoCast, def.AutoCastTargetSelector)
	}
	if def.CasterAnimationOrCasting() != "Casting" || def.EffectOnTarget != "healing_glow" {
		t.Errorf("anim=%q effectOnTarget=%q", def.CasterAnimationOrCasting(), def.EffectOnTarget)
	}

	if defs := ListAbilityDefs(); len(defs) != 1 || defs[0].ID != "heal" {
		t.Errorf("ListAbilityDefs() = %+v; want exactly [heal]", defs)
	}
}
