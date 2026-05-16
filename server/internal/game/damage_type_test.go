package game

import "testing"

// ── Default: Physical when unspecified ───────────────────────────────────────

func TestDamageType_DefaultsToPhysicalWhenUnspecified(t *testing.T) {
	if got := (DamageType("")).OrPhysical(); got != DamagePhysical {
		t.Errorf(`DamageType("").OrPhysical() = %q; want %q`, got, DamagePhysical)
	}
	// A damage event that never set a type must resolve to physical.
	if got := (DamageSource{}).ResolvedDamageType(); got != DamagePhysical {
		t.Errorf("DamageSource{}.ResolvedDamageType() = %q; want %q", got, DamagePhysical)
	}
	// A real attribution that omits the type (the common existing call shape)
	// still resolves physical.
	src := DamageSource{AttackerUnitID: 7, Kind: "projectile"}
	if got := src.ResolvedDamageType(); got != DamagePhysical {
		t.Errorf("untyped projectile source resolved %q; want %q", got, DamagePhysical)
	}
	// An explicitly-set type is preserved (not overridden by the default).
	if got := DamageFire.OrPhysical(); got != DamageFire {
		t.Errorf("DamageFire.OrPhysical() = %q; want %q", got, DamageFire)
	}
}

// ── Damage type attaches correctly to damage events ──────────────────────────

func TestDamageType_AttachesToDamageEvent(t *testing.T) {
	src := DamageSource{AttackerUnitID: 3, Kind: "projectile", DamageType: DamageFire}
	if src.ResolvedDamageType() != DamageFire {
		t.Errorf("ResolvedDamageType() = %q; want %q", src.ResolvedDamageType(), DamageFire)
	}
	// The tag must survive being passed by value (the pipeline copies the
	// struct freely).
	pass := func(d DamageSource) DamageType { return d.ResolvedDamageType() }
	if got := pass(src); got != DamageFire {
		t.Errorf("damage type lost across by-value pass: got %q want %q", got, DamageFire)
	}

	// End-to-end through the real damage pipeline: the type rides along on the
	// event and, being flavor/metadata only today, must NOT alter the damage
	// numbers. Two identical targets, equal damage, different schools → equal
	// HP loss (proves no resistance/weakness math has leaked in yet).
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	a := spawnProjTestUnit(t, s, "p1", 100, 100)
	fireTarget := spawnProjTestUnit(t, s, "enemy", 400, 400)
	physTarget := spawnProjTestUnit(t, s, "enemy", 420, 400)

	const dmg = 40
	s.applyUnitDamageWithSourceLocked(fireTarget, dmg, DamageSource{AttackerUnitID: a.ID, Kind: "projectile", DamageType: DamageFire})
	s.applyUnitDamageWithSourceLocked(physTarget, dmg, DamageSource{AttackerUnitID: a.ID, Kind: "projectile", DamageType: DamagePhysical})

	if fireTarget.HP != physTarget.HP {
		t.Errorf("typed damage changed the numbers: fire target HP=%d, physical target HP=%d (should be equal — damage type is flavor only today)", fireTarget.HP, physTarget.HP)
	}
	if fireTarget.HP != fireTarget.MaxHP-dmg {
		t.Errorf("fire-typed damage applied %d; want exactly %d", fireTarget.MaxHP-fireTarget.HP, dmg)
	}
}

// ── Extensible registry ──────────────────────────────────────────────────────

func TestDamageType_RegistryRecognisesBuiltins(t *testing.T) {
	for _, dt := range []DamageType{
		DamagePhysical, DamageFire, DamageFrost, DamageLightning, DamageArcane, DamageHoly,
	} {
		if !IsValidDamageType(dt) {
			t.Errorf("IsValidDamageType(%q) = false; want true (builtin)", dt)
		}
	}
	if IsValidDamageType("") {
		t.Error(`IsValidDamageType("") = true; the empty/unspecified value must not be a valid registered type`)
	}
	if IsValidDamageType("not_a_real_school") {
		t.Error("IsValidDamageType reported an unregistered type as valid")
	}

	all := DamageTypes()
	if len(all) < 6 {
		t.Fatalf("DamageTypes() returned %d entries; want at least the 6 builtins", len(all))
	}
	for i := 1; i < len(all); i++ {
		if all[i-1] > all[i] {
			t.Errorf("DamageTypes() not sorted: %q before %q", all[i-1], all[i])
		}
	}
}

func TestDamageType_RegisterExtends(t *testing.T) {
	const custom DamageType = "test_shadow"
	if IsValidDamageType(custom) {
		t.Fatalf("%q already registered before test", custom)
	}
	RegisterDamageType(custom)
	if !IsValidDamageType(custom) {
		t.Errorf("after RegisterDamageType(%q), IsValidDamageType = false", custom)
	}
	found := false
	for _, dt := range DamageTypes() {
		if dt == custom {
			found = true
		}
	}
	if !found {
		t.Errorf("%q not present in DamageTypes() after registration", custom)
	}
}

func TestDamageType_RegisterEmptyPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("RegisterDamageType(\"\") did not panic; the empty value is reserved for unspecified")
		}
	}()
	RegisterDamageType("")
}
