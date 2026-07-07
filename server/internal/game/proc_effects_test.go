package game

import "testing"

// TestResolveProcEffectParams_OverridePrecedence: a non-zero override field
// replaces the def's value; a zero field keeps the def's. Covers every
// overridable knob so a new field can't silently skip precedence.
func TestResolveProcEffectParams_OverridePrecedence(t *testing.T) {
	def := ProcEffectDef{
		ID: "test_effect",
		ProcEffectParams: ProcEffectParams{
			Damage: 25, DamageType: DamageLightning, ProjectileID: "lightning_bolt",
			ProjectileScale: 1, BounceCount: 2, BounceRange: 200, BounceDamageFalloff: 5,
			SlowMultiplier: 0.75, SlowDurationSeconds: 2,
			BurnDamagePerSecond: 8, BurnDurationSeconds: 3,
		},
	}

	// Zero overrides ⇒ params identical to the def's payload.
	if got := resolveProcEffectParams(def, ProcEffectOverrides{}); got != def.ProcEffectParams {
		t.Errorf("zero overrides must return the def's params verbatim:\n got %+v\nwant %+v", got, def.ProcEffectParams)
	}

	// Every knob overridden ⇒ every knob replaced; identity fields untouched.
	o := ProcEffectOverrides{
		Damage: 40, ProjectileScale: 3, BounceCount: 4, BounceRange: 300, BounceDamageFalloff: 10,
		SlowMultiplier: 0.5, SlowDurationSeconds: 4, BurnDamagePerSecond: 12, BurnDurationSeconds: 6,
	}
	got := resolveProcEffectParams(def, o)
	want := ProcEffectParams{
		Damage: 40, DamageType: DamageLightning, ProjectileID: "lightning_bolt",
		ProjectileScale: 3, BounceCount: 4, BounceRange: 300, BounceDamageFalloff: 10,
		SlowMultiplier: 0.5, SlowDurationSeconds: 4, BurnDamagePerSecond: 12, BurnDurationSeconds: 6,
	}
	if got != want {
		t.Errorf("full overrides:\n got %+v\nwant %+v", got, want)
	}

	// Partial override: one field replaced, the rest keep the def's values.
	partial := resolveProcEffectParams(def, ProcEffectOverrides{BounceCount: 4})
	if partial.BounceCount != 4 {
		t.Errorf("BounceCount override lost: got %d, want 4", partial.BounceCount)
	}
	if partial.Damage != 25 || partial.BounceRange != 200 || partial.SlowMultiplier != 0.75 {
		t.Errorf("non-overridden fields must keep def values, got %+v", partial)
	}
	// Identity fields can never change through overrides.
	if partial.DamageType != DamageLightning || partial.ProjectileID != "lightning_bolt" {
		t.Errorf("identity fields mutated: %+v", partial)
	}
}
