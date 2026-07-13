package game

import "testing"

func floatPtr(v float64) *float64 { return &v }

// An absent healthRegenRate inherits the global default. This is the case every
// unit in the catalog is in today, so it must not change behavior.
func TestResolveHealthRegenRate_AbsentInheritsDefault(t *testing.T) {
	def := UnitDef{Type: "no_regen_authored"}
	if got := resolveHealthRegenRate(def); got != defaultHealthRegenPerSecond {
		t.Fatalf("resolveHealthRegenRate(absent) = %v, want the global default %v", got, defaultHealthRegenPerSecond)
	}
}

// An authored value wins over the default.
func TestResolveHealthRegenRate_AuthoredWins(t *testing.T) {
	def := UnitDef{Type: "fast_healer", HealthRegenRate: floatPtr(3.5)}
	if got := resolveHealthRegenRate(def); got != 3.5 {
		t.Fatalf("resolveHealthRegenRate(3.5) = %v, want 3.5", got)
	}
}

// THE reason the field is a pointer: an authored 0 means "never regenerates"
// (a construct, a skeleton), and must NOT be silently rewritten to the default.
// If this field is ever changed to a plain float64, this test fails — which is
// the point.
func TestResolveHealthRegenRate_AuthoredZeroIsHonored(t *testing.T) {
	def := UnitDef{Type: "construct", HealthRegenRate: floatPtr(0)}
	if got := resolveHealthRegenRate(def); got != 0 {
		t.Fatalf("resolveHealthRegenRate(authored 0) = %v, want 0 — an authored zero must not fall back to the default", got)
	}
}

func TestValidateUnitDef_RejectsNegativeHealthRegenRate(t *testing.T) {
	def := floorValidUnit()
	def.HealthRegenRate = floatPtr(-1)
	if err := validateUnitDef(&def); err == nil {
		t.Fatal("expected a negative healthRegenRate to be rejected")
	}
}

func TestValidateUnitDef_AcceptsZeroAndPositiveHealthRegenRate(t *testing.T) {
	for _, rate := range []float64{0, 0.2, 10} {
		def := floorValidUnit()
		def.HealthRegenRate = floatPtr(rate)
		if err := validateUnitDef(&def); err != nil {
			t.Fatalf("healthRegenRate %v must be valid, got %v", rate, err)
		}
	}
}
