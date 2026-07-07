package game

import "testing"

// TestProcEffectCatalog_ShippedDefsLoad guards the shipped catalog: the three
// effects extracted from the elemental swords load with their identity fields
// intact. Payload numbers are asserted as invariants (positive, in-range),
// not pinned values, so a balance tweak doesn't break the test.
func TestProcEffectCatalog_ShippedDefsLoad(t *testing.T) {
	cases := []struct {
		id             string
		wantElement    DamageType
		wantProjectile string
	}{
		{"fire_bolt_ignite", DamageFire, "fire_bolt"},
		{"frost_bolt_chill", DamageCold, "frost_bolt"},
		{"lightning_chain", DamageLightning, "lightning_bolt"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			def, ok := getProcEffectDef(tc.id)
			if !ok {
				t.Fatalf("%s not in proc catalog", tc.id)
			}
			if def.ID != tc.id {
				t.Errorf("ID = %q, want %q", def.ID, tc.id)
			}
			if def.Damage <= 0 {
				t.Errorf("Damage want > 0, got %d", def.Damage)
			}
			if def.DamageType != tc.wantElement {
				t.Errorf("DamageType = %s, want %s", def.DamageType, tc.wantElement)
			}
			if def.ProjectileID != tc.wantProjectile {
				t.Errorf("ProjectileID = %q, want %q", def.ProjectileID, tc.wantProjectile)
			}
		})
	}

	// Per-effect payload wiring (invariants, not numbers).
	fire, _ := getProcEffectDef("fire_bolt_ignite")
	if fire.BurnDamagePerSecond <= 0 || fire.BurnDurationSeconds <= 0 {
		t.Errorf("fire_bolt_ignite needs a positive burn, got %v dps / %v s", fire.BurnDamagePerSecond, fire.BurnDurationSeconds)
	}
	frost, _ := getProcEffectDef("frost_bolt_chill")
	if !(frost.SlowMultiplier > 0 && frost.SlowMultiplier < 1) || frost.SlowDurationSeconds <= 0 {
		t.Errorf("frost_bolt_chill needs a chill in (0,1) with positive duration, got %v / %v s", frost.SlowMultiplier, frost.SlowDurationSeconds)
	}
	chain, _ := getProcEffectDef("lightning_chain")
	if chain.BounceCount <= 0 || chain.BounceRange <= 0 {
		t.Errorf("lightning_chain needs a real chain, got count=%d range=%v", chain.BounceCount, chain.BounceRange)
	}
}

// TestValidateProcEffectDef exercises the load-time validation rules.
func TestValidateProcEffectDef(t *testing.T) {
	good := &ProcEffectDef{ID: "ok", ProcEffectParams: ProcEffectParams{Damage: 10, DamageType: DamageFire, ProjectileID: "fire_bolt"}}
	if err := validateProcEffectDef(good); err != nil {
		t.Fatalf("valid def rejected: %v", err)
	}
	noDamage := &ProcEffectDef{ID: "bad1", ProcEffectParams: ProcEffectParams{Damage: 0, DamageType: DamageFire, ProjectileID: "fire_bolt"}}
	if err := validateProcEffectDef(noDamage); err == nil {
		t.Error("expected error for damage <= 0, got nil")
	}
	badType := &ProcEffectDef{ID: "bad2", ProcEffectParams: ProcEffectParams{Damage: 10, DamageType: DamageType("plasma"), ProjectileID: "fire_bolt"}}
	if err := validateProcEffectDef(badType); err == nil {
		t.Error("expected error for unregistered damage type, got nil")
	}
	noProjectile := &ProcEffectDef{ID: "bad3", ProcEffectParams: ProcEffectParams{Damage: 10, DamageType: DamageFire}}
	if err := validateProcEffectDef(noProjectile); err == nil {
		t.Error("expected error for empty projectileID, got nil")
	}
	negScale := &ProcEffectDef{ID: "bad4", ProcEffectParams: ProcEffectParams{Damage: 10, DamageType: DamageFire, ProjectileID: "fire_bolt", ProjectileScale: -1}}
	if err := validateProcEffectDef(negScale); err == nil {
		t.Error("expected error for negative projectileScale, got nil")
	}
}
