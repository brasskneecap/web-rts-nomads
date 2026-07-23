package game

import (
	"encoding/json"
	"math"
	"testing"
)

// abilityStatsTestConfig marshals a create_zone-shaped config with a radius and
// a duration, the two kinded fields the fold is meant to reach.
func abilityStatsTestConfig(radius, duration float64) json.RawMessage {
	b, _ := json.Marshal(map[string]any{
		"name":         "Test Zone",
		"radius":       radius,
		"duration":     duration,
		"tickInterval": 1,
	})
	return b
}

func decodedConfig(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("config did not decode: %v", err)
	}
	return out
}

func TestFoldAbilityStat_Arithmetic(t *testing.T) {
	cases := []struct {
		name            string
		base, flat, pct float64
		want            float64
	}{
		{"identity", 55, 0, 0, 55},
		{"flat only", 55, 10, 0, 65},
		{"pct only", 55, 0, 0.15, 63.25},
		// (base + flat) x (1 + pct) — the flat lands BEFORE the percentage, so a
		// flat bonus is itself amplified by a percentage bonus. Stated explicitly
		// because the other order is just as plausible and would give 73.25.
		{"flat then pct", 55, 10, 0.15, 74.75},
		// Two +15% sources pool ADDITIVELY to +30%, not multiplicatively to
		// +32.25%. This is the property a player reads off a tooltip.
		{"additive pooling", 100, 0, 0.30, 130},
		// A hostile/mistaken over-reduction clamps at 0 rather than producing a
		// negative radius (which reads as "absent" downstream, not as "small").
		{"clamped at zero", 55, 0, -2, 0},
		{"negative flat clamped", 10, -50, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Tolerance, not equality: these are float64 folds (55 x 1.15 is
			// 63.24999999999999 in binary floating point). Determinism is not at
			// risk — the same inputs always give the same bits — so the test
			// asserts the arithmetic, not the representation.
			if got := foldAbilityStat(tc.base, tc.flat, tc.pct); math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("foldAbilityStat(%v, %v, %v) = %v, want %v", tc.base, tc.flat, tc.pct, got, tc.want)
			}
		})
	}
}

// TestApplyAbilityStats_NoSources_IsUntouched pins the zero-cost path: a caster
// with no ability stats must get its config back byte-identical, so every
// existing ability is unchanged by this mechanism's existence.
func TestApplyAbilityStats_NoSources_IsUntouched(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	caster := spawnProjTestUnit(t, s, "p1", 0, 0)

	in := abilityStatsTestConfig(55, 10)
	out := s.applyAbilityStatsToConfigLocked(caster, "", ActionCreateZone, in)
	if string(out) != string(in) {
		t.Errorf("config was rewritten for a caster with no ability stats:\n got %s\nwant %s", out, in)
	}
}

// TestApplyAbilityStats_BroadAndScoped is the acceptance case for the whole
// design: fire_pit owns TWO duration fields of the same kind at different depths
// (the zone's 10s lifetime, the burn status's 8s), so the three addressing
// levels must be independently targetable.
func TestApplyAbilityStats_BroadAndScoped(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	zoneCfg := abilityStatsTestConfig(55, 10)
	statusCfg, _ := json.Marshal(map[string]any{"name": "Burning", "duration": 8.0, "tickInterval": 1})

	t.Run("scoped zone duration leaves the status duration alone", func(t *testing.T) {
		caster := spawnProjTestUnit(t, s, "p1", 0, 0)
		caster.AbilityStats = map[string]AbilityStatMod{"create_zone.duration": {Flat: 2}}

		zone := decodedConfig(t, s.applyAbilityStatsToConfigLocked(caster, "", ActionCreateZone, zoneCfg))
		if zone["duration"] != 12.0 {
			t.Errorf("zone duration = %v, want 12", zone["duration"])
		}
		if zone["radius"] != 55.0 {
			t.Errorf("radius must be untouched by a duration stat, got %v", zone["radius"])
		}
		status := decodedConfig(t, s.applyAbilityStatsToConfigLocked(caster, "", ActionApplyStatusDuration, statusCfg))
		if status["duration"] != 8.0 {
			t.Errorf("status duration = %v, want 8 (a create_zone-scoped stat must not reach it)", status["duration"])
		}
	})

	t.Run("scoped status duration leaves the zone duration alone", func(t *testing.T) {
		caster := spawnProjTestUnit(t, s, "p1", 0, 0)
		caster.AbilityStats = map[string]AbilityStatMod{"apply_status_duration.duration": {Flat: 2}}

		zone := decodedConfig(t, s.applyAbilityStatsToConfigLocked(caster, "", ActionCreateZone, zoneCfg))
		if zone["duration"] != 10.0 {
			t.Errorf("zone duration = %v, want 10", zone["duration"])
		}
		status := decodedConfig(t, s.applyAbilityStatsToConfigLocked(caster, "", ActionApplyStatusDuration, statusCfg))
		if status["duration"] != 10.0 {
			t.Errorf("status duration = %v, want 10", status["duration"])
		}
	})

	t.Run("broad duration reaches both", func(t *testing.T) {
		caster := spawnProjTestUnit(t, s, "p1", 0, 0)
		caster.AbilityStats = map[string]AbilityStatMod{"duration": {Flat: 2}}

		zone := decodedConfig(t, s.applyAbilityStatsToConfigLocked(caster, "", ActionCreateZone, zoneCfg))
		if zone["duration"] != 12.0 {
			t.Errorf("zone duration = %v, want 12", zone["duration"])
		}
		status := decodedConfig(t, s.applyAbilityStatsToConfigLocked(caster, "", ActionApplyStatusDuration, statusCfg))
		if status["duration"] != 10.0 {
			t.Errorf("status duration = %v, want 10", status["duration"])
		}
	})

	t.Run("broad and scoped pool into one fold", func(t *testing.T) {
		caster := spawnProjTestUnit(t, s, "p1", 0, 0)
		caster.AbilityStats = map[string]AbilityStatMod{
			"duration":             {Flat: 2},
			"create_zone.duration": {Pct: 0.5},
		}
		// (10 + 2) x (1 + 0.5) = 18 — one fold, not two sequential applications.
		zone := decodedConfig(t, s.applyAbilityStatsToConfigLocked(caster, "", ActionCreateZone, zoneCfg))
		if zone["duration"] != 18.0 {
			t.Errorf("zone duration = %v, want 18", zone["duration"])
		}
	})
}

// TestApplyAbilityStats_AbsentFieldStaysAbsent guards the rule that a stat never
// MATERIALISES a value. A "+2s duration" must not give a zone that authored no
// duration an out-of-nowhere 2s lifetime — the action's own default has to win.
func TestApplyAbilityStats_AbsentFieldStaysAbsent(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	caster.AbilityStats = map[string]AbilityStatMod{"duration": {Flat: 2}, "radius": {Flat: 5}}

	in, _ := json.Marshal(map[string]any{"name": "No Duration", "radius": 55.0})
	out := decodedConfig(t, s.applyAbilityStatsToConfigLocked(caster, "", ActionCreateZone, in))
	if _, present := out["duration"]; present {
		t.Errorf("an unauthored duration was materialised: %v", out["duration"])
	}
	if out["radius"] != 60.0 {
		t.Errorf("radius = %v, want 60 (the authored field still folds)", out["radius"])
	}
}

// TestApplyAbilityStats_TickIntervalIsNeverScaled is the consequence of the Kind
// exclusions: tickInterval shares Control "duration" with the real duration
// field, and scaling it would change a zone's DPS instead of its lifetime.
func TestApplyAbilityStats_TickIntervalIsNeverScaled(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	caster.AbilityStats = map[string]AbilityStatMod{"duration": {Flat: 5, Pct: 1}}

	out := decodedConfig(t, s.applyAbilityStatsToConfigLocked(caster, "", ActionCreateZone, abilityStatsTestConfig(55, 10)))
	if out["tickInterval"] != 1.0 {
		t.Errorf("tickInterval = %v, want 1 — it must never carry a duration stat", out["tickInterval"])
	}
}

// TestApplyAbilityStats_UnresolvedRefIsSkipped covers the ordering contract with
// resolveConfigVars: an unbound "$param" or loop var is left as a STRING, and
// the fold must skip it rather than coerce it to a number.
func TestApplyAbilityStats_UnresolvedRefIsSkipped(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	caster.AbilityStats = map[string]AbilityStatMod{"duration": {Flat: 2}}

	in, _ := json.Marshal(map[string]any{"name": "Z", "duration": "$unbound", "radius": 55.0})
	out := decodedConfig(t, s.applyAbilityStatsToConfigLocked(caster, "", ActionCreateZone, in))
	if out["duration"] != "$unbound" {
		t.Errorf("duration = %v, want the untouched %q", out["duration"], "$unbound")
	}
}

// TestApplyAbilityStats_CountStaysDecodable is the regression guard for a bug
// this mechanism introduced and the fold now rounds away: loop.iterations is a
// Go `int`, so a folded 3.45 would make encoding/json reject the ENTIRE config
// and executeActionLocked would skip the action. A "+15% count" would delete the
// loop instead of lengthening it — silently, with only a trace event.
func TestApplyAbilityStats_CountStaysDecodable(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	// A pct here is rejected at load now (see TestValidateAbilityStats_CountIsFlatOnly)
	// but the fold must still be safe for a def built outside the catalog, so this
	// exercises the belt-and-braces path with both contributions present.
	caster.AbilityStats = map[string]AbilityStatMod{"count": {Flat: 1, Pct: 0.15}}

	in, _ := json.Marshal(map[string]any{"iterations": 3})
	out := s.applyAbilityStatsToConfigLocked(caster, "", ActionLoop, in)

	got := decodedConfig(t, out)
	if v, ok := got["iterations"].(float64); !ok || v != math.Trunc(v) {
		t.Fatalf("iterations = %v, want a whole number", got["iterations"])
	}
	// And it must survive the real typed decode the executor performs.
	desc, ok := lookupActionDescriptor(ActionLoop)
	if !ok {
		t.Fatal("loop is not registered")
	}
	if _, err := desc.Decode(out); err != nil {
		t.Fatalf("folded count config no longer decodes: %v", err)
	}
	// The flat +1 lands; the pct is dropped rather than rounding to a surprise.
	if got := decodedConfig(t, out)["iterations"]; got != 4.0 {
		t.Errorf("iterations = %v, want 4 (flat +1 applied, pct ignored)", got)
	}
}

// TestValidateAbilityStats_CountIsFlatOnly pins the rule that a whole quantity
// takes no percentage. Real counts are small — 3 bounces, 2 summons — so a pct is
// either a no-op (+15%% of 3 rounds back to 3) or a cliff (+50%% jumps to 5),
// with nothing usable in between. Failing at load beats rounding to nothing.
func TestValidateAbilityStats_CountIsFlatOnly(t *testing.T) {
	if err := validateAbilityStats("unit \"x\"", map[string]AbilityStatMod{"count": {Pct: 0.15}}); err == nil {
		t.Error("a percentage on the broad count stat was accepted")
	}
	if err := validateAbilityStats("unit \"x\"", map[string]AbilityStatMod{"loop.count": {Pct: 0.5}}); err == nil {
		t.Error("a percentage on a scoped count stat was accepted")
	}
	if err := validateAbilityStats("unit \"x\"", map[string]AbilityStatMod{"count": {Flat: 1}}); err != nil {
		t.Errorf("a flat count bonus was rejected: %v", err)
	}
	// Radius and duration are continuous, so a percentage is the natural unit.
	if err := validateAbilityStats("unit \"x\"", map[string]AbilityStatMod{"radius": {Pct: 0.15}, "duration": {Pct: 0.5}}); err != nil {
		t.Errorf("a percentage on a continuous stat was rejected: %v", err)
	}
}

// TestAbilityStatDefs_FlatOnlyIsSurfaced makes sure the editor can render the
// rule rather than re-deriving it: count rows advertise FlatOnly, continuous
// rows do not.
func TestAbilityStatDefs_FlatOnlyIsSurfaced(t *testing.T) {
	for _, d := range AbilityStatDefs() {
		// Two independent reasons a row is flat-only, and the editor renders the
		// same control for both: a whole quantity (a percentage of 3 bounces
		// rounds to nothing) and an INFLICTED stat (often inverse-sense, so a
		// percentage has no single reading).
		want := d.Kind == abilityStatKindCount || d.Inflicted
		if d.FlatOnly != want {
			t.Errorf("stat %q (kind %q, inflicted %v) FlatOnly = %v, want %v", d.ID, d.Kind, d.Inflicted, d.FlatOnly, want)
		}
	}
}

// TestValidateAbilityStats_RejectsUnknownIDs is the load-time contract: a typo'd
// stat id must fail loudly at authoring time, never sit there doing nothing.
func TestValidateAbilityStats_RejectsUnknownIDs(t *testing.T) {
	if err := validateAbilityStats("unit \"x\"", map[string]AbilityStatMod{"raduis": {Pct: 0.15}}); err == nil {
		t.Error("a misspelled stat id was accepted")
	}
	if err := validateAbilityStats("unit \"x\"", map[string]AbilityStatMod{"create_zone.duration": {Flat: 1}}); err != nil {
		t.Errorf("a valid scoped id was rejected: %v", err)
	}
	// Damage is a Kind, but deliberately NOT an offered stat — it is served by
	// abilityPower/abilityDamage. Authoring it must fail rather than silently
	// create a second, weaker damage-scaling path.
	if err := validateAbilityStats("unit \"x\"", map[string]AbilityStatMod{"damage": {Pct: 0.2}}); err == nil {
		t.Error("abilityStats accepted a damage row — damage must go through abilityPower/abilityDamage")
	}
}
