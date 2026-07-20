package game

import (
	"strings"
	"testing"
)

// validPerkWithStatModifier returns a base PerkDef whose only variable is
// the supplied stat modifier, so each validation subtest exercises exactly
// one failure mode in isolation — mirrors validPerkWithRider
// (ability_riders_test.go).
func validPerkWithStatModifier(m PerkStatModifier) *PerkDef {
	return &PerkDef{
		ID:            "test_perk",
		DisplayName:   "Test Perk",
		StatModifiers: []PerkStatModifier{m},
	}
}

func TestValidatePerkDef_StatModifiers_RejectsUnknownStat(t *testing.T) {
	def := validPerkWithStatModifier(PerkStatModifier{
		Stat: "not_a_real_stat", Op: statOpAdd, Value: 5,
	})
	err := validatePerkDef(def)
	if err == nil {
		t.Fatal("want error for unknown stat, got nil")
	}
	// The message is designer-facing (typo diagnosis) — it must name the
	// offending stat so a non-Go author can spot their mistake.
	if !strings.Contains(err.Error(), "not_a_real_stat") {
		t.Fatalf("error %q does not name the offending stat", err.Error())
	}
}

func TestValidatePerkDef_StatModifiers_RejectsInvalidOp(t *testing.T) {
	def := validPerkWithStatModifier(PerkStatModifier{
		Stat: statMoveSpeed, Op: "subtract", Value: 5,
	})
	if err := validatePerkDef(def); err == nil {
		t.Fatal("want error for invalid op, got nil")
	}
}

func TestValidatePerkDef_StatModifiers_RejectsUnknownStage(t *testing.T) {
	def := validPerkWithStatModifier(PerkStatModifier{
		Stat: statMoveSpeed, Op: statOpAdd, Value: 5, Stage: "mid",
	})
	if err := validatePerkDef(def); err == nil {
		t.Fatal("want error for unknown stage, got nil")
	}
}

func TestValidatePerkDef_StatModifiers_RejectsMultiplyOnNonMultiplyStat(t *testing.T) {
	// No stat in the live registry currently has AllowMultiply=false, so this
	// test registers a synthetic fixture entry to exercise the guard, then
	// restores the registry exactly as it found it.
	const fixtureStat = "test_no_multiply_stat"
	orig, hadOrig := statRegistryByID[fixtureStat]
	statRegistryByID[fixtureStat] = statDef{ID: fixtureStat, Label: "Test No Multiply Stat", AllowMultiply: false}
	t.Cleanup(func() {
		if hadOrig {
			statRegistryByID[fixtureStat] = orig
		} else {
			delete(statRegistryByID, fixtureStat)
		}
	})

	def := validPerkWithStatModifier(PerkStatModifier{
		Stat: fixtureStat, Op: statOpMultiply, Value: 2,
	})
	if err := validatePerkDef(def); err == nil {
		t.Fatal("want error for multiply on a stat with AllowMultiply=false, got nil")
	}

	// Add is still fine on the same non-multiply stat.
	defAdd := validPerkWithStatModifier(PerkStatModifier{
		Stat: fixtureStat, Op: statOpAdd, Value: 2,
	})
	if err := validatePerkDef(defAdd); err != nil {
		t.Fatalf("want add on non-multiply stat to pass, got: %v", err)
	}
}

// validPerkWithAuraStatModifier returns a base PerkDef with one valid,
// otherwise-passing PerkAura whose only variable is the supplied
// StatModifiers entry — mirrors validPerkWithStatModifier above, isolating
// each aura-specific validation subtest to exactly the field under test.
func validPerkWithAuraStatModifier(m PerkStatModifier) *PerkDef {
	return &PerkDef{
		ID:          "test_perk",
		DisplayName: "Test Perk",
		Auras: []PerkAura{{
			Radius:        100,
			Targets:       "allies",
			StatModifiers: []PerkStatModifier{m},
		}},
	}
}

// TestValidatePerkDef_AuraStatModifiers_RejectsMultiply pins the Problem-1
// fix: rebuildAuraStatCacheLocked (perk_aura_stat_cache.go) reads an aura
// StatModifiers entry's Value straight into a raw additive accumulator and
// never looks at Op at all, so an authored "multiply" would previously
// silently behave like "add" with no error anywhere — the exact class of
// inert-but-authorable field the standing "remove what's unused" directive
// exists to prevent. Op must now be rejected at load time instead.
func TestValidatePerkDef_AuraStatModifiers_RejectsMultiply(t *testing.T) {
	def := validPerkWithAuraStatModifier(PerkStatModifier{
		Stat: statMoveSpeed, Op: statOpMultiply, Value: 1.5,
	})
	err := validatePerkDef(def)
	if err == nil {
		t.Fatal("want error for op=multiply on an aura stat modifier, got nil")
	}
	// Designer-facing: must plainly say auras are additive-only.
	if !strings.Contains(err.Error(), "additive") {
		t.Fatalf("error %q does not explain the additive-only limitation", err.Error())
	}
}

// TestValidatePerkDef_AuraStatModifiers_RejectsNonBaseStage is the Stage
// analog of the multiply-rejection test above: rebuildAuraStatCacheLocked
// never reads sm.Stage either, so a "final" (or "intrinsic") stage would
// previously be silently treated as base with no error.
func TestValidatePerkDef_AuraStatModifiers_RejectsNonBaseStage(t *testing.T) {
	def := validPerkWithAuraStatModifier(PerkStatModifier{
		Stat: statMoveSpeed, Op: statOpAdd, Value: 0.3, Stage: statStageFinal,
	})
	err := validatePerkDef(def)
	if err == nil {
		t.Fatal("want error for stage=final on an aura stat modifier, got nil")
	}
	if !strings.Contains(err.Error(), "base-stage") {
		t.Fatalf("error %q does not explain the base-stage-only limitation", err.Error())
	}
}

// TestValidatePerkDef_AuraStatModifiers_AcceptsAddAtBase confirms the only
// implemented combination (Op == "add", Stage omitted or "base") still
// passes — both an omitted Stage and an explicit "base" must be accepted,
// since Stage's zero value IS the authoring default.
func TestValidatePerkDef_AuraStatModifiers_AcceptsAddAtBase(t *testing.T) {
	cases := []struct {
		name string
		mod  PerkStatModifier
	}{
		{"add, empty stage defaults to base", PerkStatModifier{Stat: statMoveSpeed, Op: statOpAdd, Value: 0.3}},
		{"add, explicit base stage", PerkStatModifier{Stat: statMoveSpeed, Op: statOpAdd, Value: 0.3, Stage: statStageBase}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def := validPerkWithAuraStatModifier(tc.mod)
			if err := validatePerkDef(def); err != nil {
				t.Fatalf("want valid aura stat modifier to pass, got: %v", err)
			}
		})
	}
}

// TestValidatePerkDef_ZealousMarch_StillValidates guards the real catalog
// perk this whole task piloted the aura schema on: it must still pass
// validatePerkDef after the Problem-1 op/stage constraint was added, proving
// the constraint doesn't regress the one shipped aura.
func TestValidatePerkDef_ZealousMarch_StillValidates(t *testing.T) {
	def := requirePerkDef(t, "zealous_march")
	if len(def.Auras) == 0 {
		t.Fatal("zealous_march has no Auras — fixture perk changed shape")
	}
	if err := validatePerkDef(&def); err != nil {
		t.Fatalf("zealous_march must still validate: %v", err)
	}
}

// TestValidatePerkDef_StatModifiers_RejectsAuraOnlyStatAtTopLevel pins the
// AuraOnly guard: a stat with no top-level fold site (armorPercent,
// projectileDamageReduction — statDef.AuraOnly, stat_modifiers.go) must be
// rejected on a top-level PerkDef.StatModifiers entry, the same "no inert
// authorable fields" shape as the PerCompanion-on-top-level rejection above.
func TestValidatePerkDef_StatModifiers_RejectsAuraOnlyStatAtTopLevel(t *testing.T) {
	for _, stat := range []string{statArmorPercent, statProjectileDamageReduction} {
		t.Run(stat, func(t *testing.T) {
			def := validPerkWithStatModifier(PerkStatModifier{
				Stat: stat, Op: statOpAdd, Value: 0.2,
			})
			err := validatePerkDef(def)
			if err == nil {
				t.Fatalf("want error for aura-only stat %q at top level, got nil", stat)
			}
			if !strings.Contains(err.Error(), stat) {
				t.Fatalf("error %q does not name the offending stat", err.Error())
			}
			if !strings.Contains(err.Error(), "auras[]") {
				t.Fatalf("error %q does not point the designer at auras[]", err.Error())
			}
		})
	}
}

// TestValidatePerkDef_AuraStatModifiers_AcceptsAuraOnlyStat confirms the
// IDENTICAL aura-only stats ARE accepted inside auras[].statModifiers — their
// valid, intended home. This must not regress alongside the top-level
// rejection above.
func TestValidatePerkDef_AuraStatModifiers_AcceptsAuraOnlyStat(t *testing.T) {
	for _, stat := range []string{statArmorPercent, statProjectileDamageReduction} {
		t.Run(stat, func(t *testing.T) {
			def := validPerkWithAuraStatModifier(PerkStatModifier{
				Stat: stat, Op: statOpAdd, Value: 0.2,
			})
			if err := validatePerkDef(def); err != nil {
				t.Fatalf("want aura-only stat %q inside auras[].statModifiers to pass, got: %v", stat, err)
			}
		})
	}
}

// TestValidatePerkDef_ShippedAuraPerks_StillValidate guards all four shipped
// aura perks against the AuraOnly guard added above — none of them may
// regress just because armorPercent/projectileDamageReduction now reject at
// top level (none of the four author those stats at top level; this test
// exists to catch it immediately if a future edit accidentally moves one).
func TestValidatePerkDef_ShippedAuraPerks_StillValidate(t *testing.T) {
	for _, id := range []string{"zealous_march", "mana_conduit", "sanctuary", "guardian_aura"} {
		t.Run(id, func(t *testing.T) {
			def := requirePerkDef(t, id)
			if len(def.Auras) == 0 {
				t.Fatalf("%s has no Auras — fixture perk changed shape", id)
			}
			if err := validatePerkDef(&def); err != nil {
				t.Fatalf("%s must still validate: %v", id, err)
			}
		})
	}
}

// validPerkWithAuraRingColor returns a base PerkDef with one valid,
// otherwise-passing PerkAura whose only variable is the supplied RingColor —
// mirrors validPerkWithAuraStatModifier, isolating the RingColor validation
// subtests to exactly the field under test.
func validPerkWithAuraRingColor(color string) *PerkDef {
	return &PerkDef{
		ID:          "test_perk",
		DisplayName: "Test Perk",
		Auras: []PerkAura{{
			Radius:    100,
			Targets:   "allies",
			RingColor: color,
		}},
	}
}

// TestValidatePerkDef_Aura_RingColor_AcceptsValidHexForms pins the three
// accepted CSS hex color shapes (#rgb, #rrggbb, #rrggbbaa) plus the
// empty/absent default.
func TestValidatePerkDef_Aura_RingColor_AcceptsValidHexForms(t *testing.T) {
	cases := []string{"", "#fff", "#FFF", "#fef08a", "#FEF08A", "#fef08aff", "#12345678"}
	for _, color := range cases {
		t.Run(color, func(t *testing.T) {
			def := validPerkWithAuraRingColor(color)
			if err := validatePerkDef(def); err != nil {
				t.Fatalf("want ringColor %q to pass, got: %v", color, err)
			}
		})
	}
}

// TestValidatePerkDef_Aura_RingColor_RejectsInvalidValues pins the reject
// path: a typo'd or non-hex ringColor must fail loudly at load/save time
// rather than silently falling back to the player color at render time —
// the same "no silently-ignored authored value" discipline as every other
// validatePerkDef guard.
func TestValidatePerkDef_Aura_RingColor_RejectsInvalidValues(t *testing.T) {
	cases := []string{"red", "fef08a", "#ff", "#fefg08", "#fef08a12345", "rgb(1,2,3)"}
	for _, color := range cases {
		t.Run(color, func(t *testing.T) {
			def := validPerkWithAuraRingColor(color)
			err := validatePerkDef(def)
			if err == nil {
				t.Fatalf("want error for invalid ringColor %q, got nil", color)
			}
			if !strings.Contains(err.Error(), color) {
				t.Fatalf("error %q does not name the offending value %q", err.Error(), color)
			}
		})
	}
}

func TestValidatePerkDef_StatModifiers_AcceptsValidEntries(t *testing.T) {
	cases := []struct {
		name string
		mod  PerkStatModifier
	}{
		{"add, empty stage defaults to base", PerkStatModifier{Stat: statMoveSpeed, Op: statOpAdd, Value: 5}},
		{"add, explicit base stage", PerkStatModifier{Stat: statMoveSpeed, Op: statOpAdd, Value: 5, Stage: statStageBase}},
		{"multiply, final stage", PerkStatModifier{Stat: statMoveSpeed, Op: statOpMultiply, Value: 2, Stage: statStageFinal}},
		{"multiply on a stat with AllowMultiply=true", PerkStatModifier{Stat: statArmor, Op: statOpMultiply, Value: 1.5}},
		{"normal (non-aura-only) stat maxHp", PerkStatModifier{Stat: statMaxHp, Op: statOpAdd, Value: 20}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def := validPerkWithStatModifier(tc.mod)
			if err := validatePerkDef(def); err != nil {
				t.Fatalf("want valid stat modifier to pass, got: %v", err)
			}
		})
	}
}
