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
	// Cost/timing are balance knobs that live in heal.json and are meant to be
	// tuned freely — assert they loaded as sane values, NOT specific numbers.
	// A specific-number assertion here would break on every balance tweak,
	// which defeats the purpose of catalog JSON. Pinning exact numbers belongs
	// in TestAbility_AllAttributesLoadFromJSON, which uses its own inline
	// fixture and is decoupled from the live catalog.
	if def.ManaCost <= 0 {
		t.Errorf("ManaCost = %d; want > 0 (loaded from catalog)", def.ManaCost)
	}
	if def.HealAmount <= 0 {
		t.Errorf("HealAmount = %d; want > 0 (heal ability must restore HP)", def.HealAmount)
	}
	if def.CastTime < 0 {
		t.Errorf("CastTime = %v; want >= 0", def.CastTime)
	}
	if def.Cooldown < 0 {
		t.Errorf("Cooldown = %v; want >= 0", def.Cooldown)
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

	// Heal must appear in the registry listing. Deliberately does NOT pin the
	// total count — adding future abilities to the catalog is expected and
	// must not break this test.
	found := false
	for _, d := range ListAbilityDefs() {
		if d.ID == "heal" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ListAbilityDefs() does not contain heal; got %+v", ListAbilityDefs())
	}
}

func TestCastRangeJSONRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   CastRange
		want string
	}{
		{"literal", CastRange(220), "220"},
		{"sentinel", CastRange(CastRangeMatchAttackRange), `"match_attack_range"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(raw) != tc.want {
				t.Fatalf("marshal = %s, want %s", raw, tc.want)
			}
			var back CastRange
			if err := json.Unmarshal(raw, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if back.MatchesAttackRange() != tc.in.MatchesAttackRange() || (!tc.in.MatchesAttackRange() && back != tc.in) {
				t.Fatalf("round-trip = %v, want %v", back, tc.in)
			}
		})
	}
}

func TestValidateAbilityDef(t *testing.T) {
	t.Run("rejects unknown category", func(t *testing.T) {
		def := AbilityDef{ID: "x", Category: "not_a_category"}
		if err := validateAbilityDef(&def); err == nil {
			t.Fatal("expected error for unknown category")
		}
	})
	t.Run("rejects burn without impact delay", func(t *testing.T) {
		def := AbilityDef{ID: "x", BurnDurationSeconds: 3, BurnTickIntervalSeconds: 1}
		if err := validateAbilityDef(&def); err == nil {
			t.Fatal("expected error: burn requires impactDelaySeconds > 0")
		}
	})
	t.Run("normalizes target and summon counts", func(t *testing.T) {
		def := AbilityDef{ID: "x"}
		if err := validateAbilityDef(&def); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if def.TargetCount != 1 || def.SummonCount != 1 {
			t.Fatalf("TargetCount=%d SummonCount=%d, want 1/1", def.TargetCount, def.SummonCount)
		}
	})
	t.Run("normalizes channel healing multiplier", func(t *testing.T) {
		def := AbilityDef{ID: "x", ChannelType: "beam"}
		_ = validateAbilityDef(&def)
		if def.HealingMultiplier != 1.0 {
			t.Fatalf("HealingMultiplier=%v, want 1.0", def.HealingMultiplier)
		}
	})
}
