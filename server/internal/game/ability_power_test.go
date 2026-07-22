package game

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// TestAbilityPower_IsRegisteredAsAFlatPool pins the stat's shape. abilityPower
// is deliberately NOT modelled like abilityDamage: that one is a fixed-1.0
// multiplier (so an `add` means percentage points and its base default is 1),
// while this is a flat pool an ability draws on through a ratio (so an `add` is
// a whole amount and its base default is 0).
func TestAbilityPower_IsRegisteredAsAFlatPool(t *testing.T) {
	if !isKnownStat(statAbilityPower) {
		t.Fatal("abilityPower should be a registered stat")
	}
	if !isBaseAuthorableStat(statAbilityPower) {
		t.Error("abilityPower should be base-authorable — a unit type carries its own")
	}
	if isFractionStat(statAbilityPower) {
		t.Error("abilityPower is a flat pool, not a fixed-1.0 multiplier ⇒ an add is a whole amount, not percentage points")
	}
	if got := statBaseDefault(statAbilityPower); got != 0 {
		t.Errorf("statBaseDefault(abilityPower) = %v, want 0 (a unit with no base contributes nothing)", got)
	}
}

// TestAbilityScalingTerms_ZeroRatiosAreIdentity is the no-migration property:
// every deal_damage authored before ratios existed has both at 0 and must be
// bit-for-bit unaffected.
func TestAbilityScalingTerms_ZeroRatiosAreIdentity(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	caster.Damage = 40
	caster.BaseStats = map[string]float64{statAbilityPower: 100}

	if got := s.abilityScalingTermsLocked(caster, 0, 0); got != 0 {
		t.Errorf("scaling terms with both ratios 0 = %v, want 0", got)
	}
	if got := s.abilityScalingTermsLocked(nil, 1, 1); got != 0 {
		t.Errorf("scaling terms with a nil caster = %v, want 0", got)
	}
}

// TestAbilityScalingTerms_AdditiveTerms covers the arithmetic and the hybrid
// case an enum-style "pick a scaling mode" design would have forbidden.
func TestAbilityScalingTerms_AdditiveTerms(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	caster.Damage = 40
	caster.BaseStats = map[string]float64{statAbilityPower: 100}

	cases := []struct {
		name             string
		adRatio, apRatio float64
		want             float64
	}{
		{"ability power only", 0, 1, 100},
		{"half ability power", 0, 0.5, 50},
		{"ratio above 1 is legal", 0, 1.5, 150},
		{"attack damage only", 1, 0, 40},
		{"hybrid — both contribute", 0.5, 0.5, 70},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.abilityScalingTermsLocked(caster, tc.adRatio, tc.apRatio); math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("terms(ad=%v, ap=%v) = %v, want %v", tc.adRatio, tc.apRatio, got, tc.want)
			}
		})
	}
}

// TestAbilityPower_DoTAndBurstGainTheSameTotal is THE test this whole design
// exists for.
//
// The user's original concern: "a flat increase is much stronger on tick damage
// vs regular damage." It is — a flat +100 on an 8-tick burn is +800, versus +100
// on a nuke. The RATIO is the normalization: the burn authors 1/8th, so both
// sources gain exactly 100 total from 100 ability power, and a designer can move
// power between them without the DoT silently running away.
func TestAbilityPower_DoTAndBurstGainTheSameTotal(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	caster.BaseStats = map[string]float64{statAbilityPower: 100}

	const ticks = 8

	// A burst nuke: one application at full ratio.
	burst := s.abilityScalingTermsLocked(caster, 0, 1.0)

	// An 8-tick burn: the ratio is PER APPLICATION, so it authors 1/ticks and
	// applies that many times.
	perTick := s.abilityScalingTermsLocked(caster, 0, 1.0/ticks)
	dot := perTick * ticks

	if math.Abs(burst-dot) > 1e-9 {
		t.Errorf("burst gained %v but the DoT gained %v — the ratio failed to normalize them", burst, dot)
	}
	if burst != 100 {
		t.Errorf("100 ability power at ratio 1.0 should contribute 100, got %v", burst)
	}

	// And the failure mode being avoided: the SAME flat bonus applied per tick
	// without a ratio would be `ticks` times stronger on the DoT.
	naiveFlat := s.abilityScalingTermsLocked(caster, 0, 1.0) * ticks
	if naiveFlat <= burst {
		t.Fatalf("test is not exercising the hazard: naive per-tick flat %v should exceed burst %v", naiveFlat, burst)
	}
}

// TestAbilityPower_ScalesWithModifiers proves ability power reaches abilities
// through the SAME stat chokepoint everything else uses — so a perk, item,
// status or zone aura that adds ability power needs no per-ability wiring.
func TestAbilityPower_ScalesWithModifiers(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	caster.BaseStats = map[string]float64{statAbilityPower: 50}

	base := s.abilityScalingTermsLocked(caster, 0, 1)
	if base != 50 {
		t.Fatalf("authored base ability power = %v, want 50", base)
	}

	// A status granting +30 ability power folds through effectiveStatLocked.
	s.AbilityStatuses = append(s.AbilityStatuses, &AbilityStatus{
		TargetUnitID:  caster.ID,
		Remaining:     10,
		StatModifiers: []PerkStatModifier{{Stat: statAbilityPower, Op: statOpAdd, Value: 30}},
	})
	if got := s.abilityScalingTermsLocked(caster, 0, 1); got != 80 {
		t.Errorf("ability power with a +30 status = %v, want 80", got)
	}
}

// TestAbilityPower_RatiosAreEditable is the rule this project holds every
// mechanism to: a value the simulation reads MUST be visible and editable in the
// editor. It exists because the ratios shipped without it — the config struct
// and the executor were wired, but the two SchemaField entries were silently
// dropped, so ability-power scaling worked and was completely unauthorable.
//
// More generally: an action's every numeric config field should be declared in
// its Schema, or the only way to set it is by hand-editing JSON.
func TestAbilityPower_RatiosAreEditable(t *testing.T) {
	desc, ok := lookupActionDescriptor(ActionDealDamage)
	if !ok {
		t.Fatal("deal_damage is not registered")
	}
	for _, key := range []string{"adRatio", "apRatio"} {
		f, found := schemaFieldByKey(desc, key)
		if !found {
			t.Errorf("deal_damage declares no schema field %q — the executor reads it, so the editor must be able to set it", key)
			continue
		}
		if !isNumericControl(f.Control) {
			t.Errorf("deal_damage field %q has control %q, want a numeric control", key, f.Control)
		}
		if f.Label == "" {
			t.Errorf("deal_damage field %q has no label", key)
		}
		// A ratio is NOT a magnitude — it must carry no Kind, or a broad
		// "+15% damage" ability stat would scale the RATIO instead of the
		// damage, compounding with itself.
		if f.Kind != "" {
			t.Errorf("deal_damage field %q must carry no Kind (got %q) — it is a coefficient, not a magnitude", key, f.Kind)
		}
	}
}

// TestPreview_CasterHasRealStats guards two bugs that made damage ratios look
// broken in the editor preview — the one place abilities are actually tested.
//
//  1. The preview's synthetic Player was a bare struct, so its
//     PhysicalDamageMultiplier was 0. applyPlayerUpgradesAtSpawnLocked scales a
//     spawned unit's damage by it, so the preview caster spawned with ZERO
//     damage. Pre-existing and invisible until an ability could scale off the
//     caster: nothing read caster damage before adRatio existed.
//  2. The preview separately zeroed caster.Damage for its "no combat noise"
//     contract. That contract is now met by dropping the "attack" capability
//     and marking the caster NonCombat instead, which is what canAutoAttack
//     actually gates on — so the stat the ability reads is left intact.
//
// Asserted through the REAL preview entry point, because both bugs lived in
// setup code a unit test of the executor cannot see.
func TestPreview_CasterHasRealStats(t *testing.T) {
	base, ok := getAbilityDef("fire_pit")
	if !ok {
		t.Fatal("fire_pit missing from the catalog")
	}

	run := func(t *testing.T, adRatio float64) int {
		t.Helper()
		prog := withADRatioOnDirectDamage(t, base, adRatio)
		def := base
		def.Program = prog
		res, err := RunAbilityPreview(PreviewRequest{
			Ability: def, Seed: 1,
			Units:           []PreviewSceneUnit{{Team: "enemy", X: 60, HP: 500, MaxHP: 500}},
			Target:          0,
			CastX:           60,
			DurationSeconds: 4,
		})
		if err != nil {
			t.Fatalf("preview failed: %v", err)
		}
		if len(res.Units) != 1 {
			t.Fatalf("expected 1 scene unit result, got %d", len(res.Units))
		}
		return res.Units[0].HPBefore - res.Units[0].HPAfter
	}

	plain := run(t, 0)
	scaled := run(t, 1.0)
	if plain <= 0 {
		t.Fatalf("baseline preview dealt %d damage — the scene is not exercising the pit", plain)
	}
	if scaled <= plain {
		t.Errorf("adRatio 1.0 dealt %d, no more than the unscaled %d — the preview caster has no attack damage", scaled, plain)
	}
}

// withADRatioOnDirectDamage returns a copy of def's program with adRatio set on
// the "direct_dmg" action. Patched through raw JSON: that action is nested
// inside create_zone's config triggers, so a typed walk would have to know every
// container shape.
func withADRatioOnDirectDamage(t *testing.T, def AbilityDef, ratio float64) *AbilityProgram {
	t.Helper()
	raw, err := json.Marshal(def.Program)
	if err != nil {
		t.Fatal(err)
	}
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		t.Fatal(err)
	}
	var walk func(any)
	walk = func(v any) {
		switch tv := v.(type) {
		case map[string]any:
			if tv["id"] == "direct_dmg" {
				if cfg, ok := tv["config"].(map[string]any); ok {
					cfg["adRatio"] = ratio
				}
			}
			for _, child := range tv {
				walk(child)
			}
		case []any:
			for _, child := range tv {
				walk(child)
			}
		}
	}
	walk(tree)
	patched, err := json.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	var out AbilityProgram
	if err := json.Unmarshal(patched, &out); err != nil {
		t.Fatal(err)
	}
	return &out
}

// TestAbilityStatLabels_AreDistinguishable guards the pairing that caused real
// confusion: abilityDamage and abilityPower are adjacent in every stat picker,
// both start with "Ability", and do DIFFERENT things — one is a percentage
// amplifier (base 1.0), the other a flat pool (base 0). A designer reading only
// the labels has to be able to tell them apart, so the multiplicative one
// carries "%" in its name.
func TestAbilityStatLabels_AreDistinguishable(t *testing.T) {
	label := func(id string) string {
		for _, d := range statRegistry {
			if d.ID == id {
				return d.Label
			}
		}
		t.Fatalf("stat %q is not registered", id)
		return ""
	}
	damage, power := label(statAbilityDamage), label(statAbilityPower)
	if !strings.Contains(damage, "%") {
		t.Errorf("abilityDamage label %q must carry %q — it is a multiplier, and %q sits beside it in every picker",
			damage, "%", power)
	}
	if strings.Contains(power, "%") {
		t.Errorf("abilityPower label %q must NOT carry %q — it is a flat pool, not a percentage", power, "%")
	}
	if damage == power {
		t.Errorf("abilityDamage and abilityPower share the label %q", damage)
	}
}
