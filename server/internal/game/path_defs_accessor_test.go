package game

import (
	"reflect"
	"testing"
)

// TestPathCatalogAccessors_MatchDirectMapReads locks the concurrency-safe
// accessor layer added on top of the 10 package-global path-catalog maps in
// path_defs.go. Each accessor must return exactly what a direct map read
// would return (same value, same ok) — this is a pure wrapper, not a
// behavior change. Values are read from the maps themselves (or asserted as
// structural facts: ok/non-empty/contains) rather than hardcoded balance
// numbers, per project convention.
func TestPathCatalogAccessors_MatchDirectMapReads(t *testing.T) {
	// --- pathModifierLookup / pathModifierFor(cleric/bronze) ---
	key := pathModifierKey(unitPathCleric, unitRankBronze)
	wantMod, wantOk := pathModifiersByKey[key]
	gotMod, gotOk := pathModifierLookup(key)
	if !wantOk {
		t.Fatalf("setup: pathModifiersByKey[%q] missing — catalog not loaded?", key)
	}
	// reflect.DeepEqual, not ==: pathModifierDef now carries a BaseStats map
	// (per-rank absolute values for fieldless stats), and a struct containing a
	// map is not comparable.
	if gotOk != wantOk || !reflect.DeepEqual(gotMod, wantMod) {
		t.Errorf("pathModifierLookup(%q) = (%+v, %v), want (%+v, %v)", key, gotMod, gotOk, wantMod, wantOk)
	}
	if gotMod.Path != unitPathCleric {
		t.Errorf("pathModifierLookup(%q).Path = %q, want %q", key, gotMod.Path, unitPathCleric)
	}

	// --- pathsForUnitType(acolyte) ---
	wantPaths := pathsByUnitType["acolyte"]
	gotPaths := pathsForUnitType("acolyte")
	if len(gotPaths) == 0 {
		t.Fatalf("pathsForUnitType(\"acolyte\") is empty — expected at least cleric")
	}
	if len(gotPaths) != len(wantPaths) {
		t.Errorf("pathsForUnitType(\"acolyte\") len = %d, want %d", len(gotPaths), len(wantPaths))
	}
	found := false
	for _, p := range gotPaths {
		if p == unitPathCleric {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("pathsForUnitType(\"acolyte\") = %v, want it to include %q", gotPaths, unitPathCleric)
	}

	// Mutating the returned slice must not affect the package state (copy
	// semantics) — this is the whole point of the accessor existing.
	if len(gotPaths) > 0 {
		gotPaths[0] = "__mutated__"
		again := pathsForUnitType("acolyte")
		if len(again) > 0 && again[0] == "__mutated__" {
			t.Errorf("pathsForUnitType returned a shared slice, not a copy — mutation leaked into package state")
		}
	}

	// --- pathBoundsFor(cleric) ---
	wantBounds, wantBoundsOk := pathBoundsByPath[unitPathCleric]
	gotBounds, gotBoundsOk := pathBoundsFor(unitPathCleric)
	if !wantBoundsOk {
		t.Fatalf("setup: pathBoundsByPath[%q] missing", unitPathCleric)
	}
	if gotBoundsOk != wantBoundsOk || string(gotBounds) != string(wantBounds) {
		t.Errorf("pathBoundsFor(%q) = (%s, %v), want (%s, %v)", unitPathCleric, gotBounds, gotBoundsOk, wantBounds, wantBoundsOk)
	}

	// --- pathProjectileFor(cleric) ---
	wantProjectile, wantProjectileOk := pathProjectileByPath[unitPathCleric]
	gotProjectile, gotProjectileOk := pathProjectileFor(unitPathCleric)
	if !wantProjectileOk || gotProjectileOk != wantProjectileOk || gotProjectile != wantProjectile {
		t.Errorf("pathProjectileFor(%q) = (%q, %v), want (%q, %v)", unitPathCleric, gotProjectile, gotProjectileOk, wantProjectile, wantProjectileOk)
	}

	// --- pathDamageTypeFor(cleric) ---
	wantDamageType, wantDamageTypeOk := pathDamageTypeByPath[unitPathCleric]
	gotDamageType, gotDamageTypeOk := pathDamageTypeFor(unitPathCleric)
	if !wantDamageTypeOk || gotDamageTypeOk != wantDamageTypeOk || gotDamageType != wantDamageType {
		t.Errorf("pathDamageTypeFor(%q) = (%q, %v), want (%q, %v)", unitPathCleric, gotDamageType, gotDamageTypeOk, wantDamageType, wantDamageTypeOk)
	}

	// --- pathProjectileScaleFor(cleric) ---
	wantScale, wantScaleOk := pathProjectileScaleByPath[unitPathCleric]
	gotScale, gotScaleOk := pathProjectileScaleFor(unitPathCleric)
	if !wantScaleOk || gotScaleOk != wantScaleOk || gotScale != wantScale {
		t.Errorf("pathProjectileScaleFor(%q) = (%v, %v), want (%v, %v)", unitPathCleric, gotScale, gotScaleOk, wantScale, wantScaleOk)
	}

	// --- pathAttackTypeFor(vanguard) ---
	wantAttackType, wantAttackTypeOk := pathAttackTypeByPath[unitPathVanguard]
	gotAttackType, gotAttackTypeOk := pathAttackTypeFor(unitPathVanguard)
	if !wantAttackTypeOk || gotAttackTypeOk != wantAttackTypeOk || gotAttackType != wantAttackType {
		t.Errorf("pathAttackTypeFor(%q) = (%q, %v), want (%q, %v)", unitPathVanguard, gotAttackType, gotAttackTypeOk, wantAttackType, wantAttackTypeOk)
	}

	// --- pathVisionRangeFor(marksman) ---
	wantVision, wantVisionOk := pathVisionRangeByPath[unitPathMarksman]
	gotVision, gotVisionOk := pathVisionRangeFor(unitPathMarksman)
	if !wantVisionOk || gotVisionOk != wantVisionOk || gotVision != wantVision {
		t.Errorf("pathVisionRangeFor(%q) = (%v, %v), want (%v, %v)", unitPathMarksman, gotVision, gotVisionOk, wantVision, wantVisionOk)
	}

	// --- pathChannelLoopFor(siphoner) ---
	wantLoop, wantLoopOk := pathChannelLoopByPath[unitPathSiphoner]
	gotLoop, gotLoopOk := pathChannelLoopFor(unitPathSiphoner)
	if !wantLoopOk || gotLoopOk != wantLoopOk || gotLoop != wantLoop {
		t.Errorf("pathChannelLoopFor(%q) = (%+v, %v), want (%+v, %v)", unitPathSiphoner, gotLoop, gotLoopOk, wantLoop, wantLoopOk)
	}

	// --- pathAbilitiesFor(cleric) ---
	wantAbilities, wantAbilitiesOk := pathAbilitiesByPath[unitPathCleric]
	gotAbilities, gotAbilitiesOk := pathAbilitiesFor(unitPathCleric)
	if !wantAbilitiesOk {
		t.Fatalf("setup: pathAbilitiesByPath[%q] missing", unitPathCleric)
	}
	if gotAbilitiesOk != wantAbilitiesOk || len(gotAbilities) != len(wantAbilities) {
		t.Errorf("pathAbilitiesFor(%q) = (%v, %v), want (%v, %v)", unitPathCleric, gotAbilities, gotAbilitiesOk, wantAbilities, wantAbilitiesOk)
	}
	for i := range wantAbilities {
		if i >= len(gotAbilities) || gotAbilities[i] != wantAbilities[i] {
			t.Errorf("pathAbilitiesFor(%q)[%d] = %v, want %v", unitPathCleric, i, gotAbilities, wantAbilities)
			break
		}
	}
	// Copy semantics: mutating the returned slice must not affect package state.
	if len(gotAbilities) > 0 {
		original := gotAbilities[0]
		gotAbilities[0] = "__mutated__"
		again, _ := pathAbilitiesFor(unitPathCleric)
		if len(again) > 0 && again[0] != original {
			t.Errorf("pathAbilitiesFor returned a shared slice, not a copy — mutation leaked into package state")
		}
	}

	// --- unknown path: every accessor must report ok=false, matching a
	// direct miss on an unregistered key. ---
	if _, ok := pathModifierLookup(pathModifierKey("not_a_real_path", unitRankBronze)); ok {
		t.Errorf("pathModifierLookup(unknown) ok = true, want false")
	}
	if _, ok := pathVisionRangeFor("not_a_real_path"); ok {
		t.Errorf("pathVisionRangeFor(unknown) ok = true, want false")
	}
	if _, ok := pathProjectileFor("not_a_real_path"); ok {
		t.Errorf("pathProjectileFor(unknown) ok = true, want false")
	}
	if _, ok := pathDamageTypeFor("not_a_real_path"); ok {
		t.Errorf("pathDamageTypeFor(unknown) ok = true, want false")
	}
	if _, ok := pathAttackTypeFor("not_a_real_path"); ok {
		t.Errorf("pathAttackTypeFor(unknown) ok = true, want false")
	}
	if _, ok := pathProjectileScaleFor("not_a_real_path"); ok {
		t.Errorf("pathProjectileScaleFor(unknown) ok = true, want false")
	}
	if _, ok := pathAbilitiesFor("not_a_real_path"); ok {
		t.Errorf("pathAbilitiesFor(unknown) ok = true, want false")
	}
	if _, ok := pathChannelLoopFor("not_a_real_path"); ok {
		t.Errorf("pathChannelLoopFor(unknown) ok = true, want false")
	}
	if _, ok := pathBoundsFor("not_a_real_path"); ok {
		t.Errorf("pathBoundsFor(unknown) ok = true, want false")
	}
	if got := pathsForUnitType("not_a_real_unit_type"); len(got) != 0 {
		t.Errorf("pathsForUnitType(unknown) = %v, want empty", got)
	}
}

// TestPathCatalogAccessors_ConcurrentReadsDoNotRace exercises the accessors
// from many goroutines simultaneously. Run with -race; this is the
// concurrency contract the next task (writable editor overlay) depends on.
func TestPathCatalogAccessors_ConcurrentReadsDoNotRace(t *testing.T) {
	const goroutines = 8
	const iterations = 200

	done := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < iterations; j++ {
				_, _ = pathModifierLookup(pathModifierKey(unitPathCleric, unitRankBronze))
				_, _ = pathVisionRangeFor(unitPathMarksman)
				_, _ = pathProjectileFor(unitPathCleric)
				_, _ = pathDamageTypeFor(unitPathCleric)
				_, _ = pathAttackTypeFor(unitPathVanguard)
				_, _ = pathProjectileScaleFor(unitPathCleric)
				_, _ = pathAbilitiesFor(unitPathCleric)
				_, _ = pathChannelLoopFor(unitPathSiphoner)
				_, _ = pathBoundsFor(unitPathCleric)
				_ = pathsForUnitType("acolyte")
			}
		}()
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}
}
