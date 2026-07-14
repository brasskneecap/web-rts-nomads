package game

import (
	"encoding/json"
	"strings"
	"testing"
)

// validClericShapedPathFile returns a pathCatalogFile that mirrors the shape
// of the shipped cleric.json (see catalog/units/human/acolyte/paths/cleric/
// cleric.json) but under a synthetic path id, so tests never depend on nor
// perturb the real shipped catalog. All referenced ids (projectile, damage
// type, ability) are real registered catalog entries — reusing the same ones
// cleric.json uses, since those are already proven valid by the fact the
// package's init() didn't panic.
func validClericShapedPathFile(pathID string) *pathCatalogFile {
	abilities := []string{"greater_heal"}
	return &pathCatalogFile{
		Path:            pathID,
		Bounds:          json.RawMessage(`{"halfWidth":21,"top":-77,"bottom":39}`),
		VisionRange:     400,
		Projectile:      "holy_bolt",
		DamageType:      DamageType("holy"),
		AttackType:      "swing",
		ProjectileScale: 1.0,
		Abilities:       &abilities,
		ChannelLoop:     &ChannelLoopRange{Start: 0, End: 1},
		Ranks: map[string]pathRankStatsJSON{
			unitRankBronze: {MaxHPMultiplier: 1.10, DamageMultiplier: 1.10, AttackSpeedMultiplier: 1.00, MoveSpeedMultiplier: 1.00, AttackRangeMultiplier: 1.00},
		},
	}
}

func TestValidatePathFile_ValidClericShapedFile_ReturnsNil(t *testing.T) {
	file := validClericShapedPathFile("test_valid_cleric_shaped")
	if err := validatePathFile(file, "test_valid_cleric_shaped"); err != nil {
		t.Fatalf("validatePathFile(valid file) = %v, want nil", err)
	}
}

// TestValidatePathFile_AttackOriginPresent_StillValid confirms attackOrigin
// is opaque client-render passthrough data, like Bounds — validatePathFile
// must not reject (or otherwise care about) its contents.
func TestValidatePathFile_AttackOriginPresent_StillValid(t *testing.T) {
	file := validClericShapedPathFile("test_attack_origin_valid")
	file.AttackOrigin = json.RawMessage(`{"default":{"x":0,"y":-30},"byFacing":{"east":{"x":14,"y":-28}}}`)
	if err := validatePathFile(file, "test_attack_origin_valid"); err != nil {
		t.Fatalf("validatePathFile(attackOrigin present) = %v, want nil", err)
	}
}

func TestValidatePathFile_MissingPath_ReturnsError(t *testing.T) {
	file := validClericShapedPathFile("")
	err := validatePathFile(file, "some_dir")
	if err == nil {
		t.Fatal("validatePathFile(missing path) = nil, want error")
	}
	if !strings.Contains(err.Error(), `missing "path" field`) {
		t.Errorf("error = %q, want it to mention missing path field", err.Error())
	}
}

func TestValidatePathFile_PathMismatch_ReturnsError(t *testing.T) {
	file := validClericShapedPathFile("cleric")
	err := validatePathFile(file, "not_cleric")
	if err == nil {
		t.Fatal("validatePathFile(path/dir mismatch) = nil, want error")
	}
	if !strings.Contains(err.Error(), `"cleric"`) || !strings.Contains(err.Error(), `"not_cleric"`) {
		t.Errorf("error = %q, want it to name both %q and %q", err.Error(), "cleric", "not_cleric")
	}
}

func TestValidatePathFile_UnregisteredProjectile_ReturnsError(t *testing.T) {
	file := validClericShapedPathFile("test_bad_projectile")
	file.Projectile = "not_a_real_projectile_xyz"
	err := validatePathFile(file, "test_bad_projectile")
	if err == nil {
		t.Fatal("validatePathFile(unregistered projectile) = nil, want error")
	}
	if !strings.Contains(err.Error(), "not_a_real_projectile_xyz") {
		t.Errorf("error = %q, want it to name the bad projectile id", err.Error())
	}
}

func TestValidatePathFile_InvalidDamageType_ReturnsError(t *testing.T) {
	file := validClericShapedPathFile("test_bad_damage_type")
	file.DamageType = DamageType("not_a_real_damage_type_xyz")
	err := validatePathFile(file, "test_bad_damage_type")
	if err == nil {
		t.Fatal("validatePathFile(invalid damageType) = nil, want error")
	}
	if !strings.Contains(err.Error(), "not_a_real_damage_type_xyz") {
		t.Errorf("error = %q, want it to name the bad damage type", err.Error())
	}
}

func TestValidatePathFile_NegativeProjectileScale_ReturnsError(t *testing.T) {
	file := validClericShapedPathFile("test_bad_scale")
	file.ProjectileScale = -1
	err := validatePathFile(file, "test_bad_scale")
	if err == nil {
		t.Fatal("validatePathFile(negative projectileScale) = nil, want error")
	}
	if !strings.Contains(err.Error(), "projectileScale") {
		t.Errorf("error = %q, want it to mention projectileScale", err.Error())
	}
}

func TestValidatePathFile_NegativeChannelLoopStart_ReturnsError(t *testing.T) {
	file := validClericShapedPathFile("test_bad_channel_start")
	file.ChannelLoop = &ChannelLoopRange{Start: -1, End: 1}
	err := validatePathFile(file, "test_bad_channel_start")
	if err == nil {
		t.Fatal("validatePathFile(negative channelLoop.start) = nil, want error")
	}
	if !strings.Contains(err.Error(), "channelLoop.start") {
		t.Errorf("error = %q, want it to mention channelLoop.start", err.Error())
	}
}

func TestValidatePathFile_ChannelLoopEndBeforeStart_ReturnsError(t *testing.T) {
	file := validClericShapedPathFile("test_bad_channel_end")
	file.ChannelLoop = &ChannelLoopRange{Start: 5, End: 2}
	err := validatePathFile(file, "test_bad_channel_end")
	if err == nil {
		t.Fatal("validatePathFile(channelLoop.end < start) = nil, want error")
	}
	if !strings.Contains(err.Error(), "channelLoop.end") {
		t.Errorf("error = %q, want it to mention channelLoop.end", err.Error())
	}
}

func TestValidatePathFile_EmptyAbilityID_ReturnsError(t *testing.T) {
	file := validClericShapedPathFile("test_bad_ability_empty")
	abilities := []string{""}
	file.Abilities = &abilities
	err := validatePathFile(file, "test_bad_ability_empty")
	if err == nil {
		t.Fatal("validatePathFile(empty ability id) = nil, want error")
	}
	if !strings.Contains(err.Error(), `"abilities"`) {
		t.Errorf("error = %q, want it to mention abilities", err.Error())
	}
}

func TestValidatePathFile_UnregisteredAbility_ReturnsError(t *testing.T) {
	file := validClericShapedPathFile("test_bad_ability_unknown")
	abilities := []string{"not_a_real_ability_xyz"}
	file.Abilities = &abilities
	err := validatePathFile(file, "test_bad_ability_unknown")
	if err == nil {
		t.Fatal("validatePathFile(unregistered ability) = nil, want error")
	}
	if !strings.Contains(err.Error(), "not_a_real_ability_xyz") {
		t.Errorf("error = %q, want it to name the bad ability id", err.Error())
	}
}

func TestValidatePathFile_UnknownRankName_ReturnsError(t *testing.T) {
	file := validClericShapedPathFile("test_bad_rank")
	file.Ranks = map[string]pathRankStatsJSON{
		"platinum": {MaxHPMultiplier: 1.0},
	}
	err := validatePathFile(file, "test_bad_rank")
	if err == nil {
		t.Fatal("validatePathFile(unknown rank name) = nil, want error")
	}
	if !strings.Contains(err.Error(), "platinum") {
		t.Errorf("error = %q, want it to name the bad rank %q", err.Error(), "platinum")
	}
}

// --- registerPathFileLocked ---
//
// registerPathFileLocked's contract requires the caller to hold
// pathCatalogMu.Lock(); these tests take it explicitly even though they run
// single-threaded, matching real (future editor) call sites rather than
// leaning on init()'s single-threaded exemption.

func TestRegisterPathFileLocked_ValidFile_PopulatesMaps(t *testing.T) {
	pathID := "test_register_valid"
	unitKey := "test_unit_register_valid"
	file := validClericShapedPathFile(pathID)

	pathCatalogMu.Lock()
	err := registerPathFileLocked(unitKey, file)
	pathCatalogMu.Unlock()
	if err != nil {
		t.Fatalf("registerPathFileLocked(valid file) = %v, want nil", err)
	}
	t.Cleanup(func() {
		pathCatalogMu.Lock()
		defer pathCatalogMu.Unlock()
		delete(pathBoundsByPath, pathID)
		delete(pathVisionRangeByPath, pathID)
		delete(pathProjectileByPath, pathID)
		delete(pathDamageTypeByPath, pathID)
		delete(pathAttackTypeByPath, pathID)
		delete(pathProjectileScaleByPath, pathID)
		delete(pathChannelLoopByPath, pathID)
		delete(pathAbilitiesByPath, pathID)
		delete(pathModifiersByKey, pathModifierKey(pathID, unitRankBronze))
		delete(pathsByUnitType, unitKey)
	})

	if _, ok := pathModifierLookup(pathModifierKey(pathID, unitRankBronze)); !ok {
		t.Errorf("pathModifiersByKey[%s/bronze] not populated", pathID)
	}
	if paths := pathsForUnitType(unitKey); len(paths) != 1 || paths[0] != pathID {
		t.Errorf("pathsForUnitType(%q) = %v, want [%q]", unitKey, paths, pathID)
	}
	if _, ok := pathAbilitiesFor(pathID); !ok {
		t.Errorf("pathAbilitiesByPath[%q] not populated", pathID)
	}
	if _, ok := pathChannelLoopFor(pathID); !ok {
		t.Errorf("pathChannelLoopByPath[%q] not populated", pathID)
	}
}

func TestRegisterPathFileLocked_DuplicateRankDefinition_ReturnsError(t *testing.T) {
	pathID := "test_register_dup_rank"
	unitKey := "test_unit_register_dup_rank"
	file := validClericShapedPathFile(pathID)
	// Strip the fields that would themselves collide on a second call so
	// this test isolates the rank-duplicate path specifically.
	file.ChannelLoop = nil
	file.Abilities = nil

	pathCatalogMu.Lock()
	if err := registerPathFileLocked(unitKey, file); err != nil {
		pathCatalogMu.Unlock()
		t.Fatalf("first registerPathFileLocked call = %v, want nil", err)
	}
	pathCatalogMu.Unlock()
	t.Cleanup(func() {
		pathCatalogMu.Lock()
		defer pathCatalogMu.Unlock()
		delete(pathBoundsByPath, pathID)
		delete(pathVisionRangeByPath, pathID)
		delete(pathProjectileByPath, pathID)
		delete(pathDamageTypeByPath, pathID)
		delete(pathAttackTypeByPath, pathID)
		delete(pathProjectileScaleByPath, pathID)
		delete(pathModifiersByKey, pathModifierKey(pathID, unitRankBronze))
		delete(pathsByUnitType, unitKey)
	})

	// Same (path, rank) registered again from a second "file" — simulates
	// two directories claiming the same path id.
	dup := validClericShapedPathFile(pathID)
	dup.ChannelLoop = nil
	dup.Abilities = nil

	pathCatalogMu.Lock()
	err := registerPathFileLocked(unitKey, dup)
	pathCatalogMu.Unlock()
	if err == nil {
		t.Fatal("registerPathFileLocked(duplicate rank) = nil, want error")
	}
	if !strings.Contains(err.Error(), pathID) {
		t.Errorf("error = %q, want it to name %q", err.Error(), pathID)
	}
}

func TestRegisterPathFileLocked_DuplicateChannelLoopOverride_ReturnsError(t *testing.T) {
	pathID := "test_register_dup_channel"
	unitKey := "test_unit_register_dup_channel"
	file := validClericShapedPathFile(pathID)
	file.Ranks = nil // isolate: no rank rows, so only channelLoop can collide
	file.Abilities = nil

	pathCatalogMu.Lock()
	if err := registerPathFileLocked(unitKey, file); err != nil {
		pathCatalogMu.Unlock()
		t.Fatalf("first registerPathFileLocked call = %v, want nil", err)
	}
	pathCatalogMu.Unlock()
	t.Cleanup(func() {
		pathCatalogMu.Lock()
		defer pathCatalogMu.Unlock()
		delete(pathBoundsByPath, pathID)
		delete(pathVisionRangeByPath, pathID)
		delete(pathProjectileByPath, pathID)
		delete(pathDamageTypeByPath, pathID)
		delete(pathAttackTypeByPath, pathID)
		delete(pathProjectileScaleByPath, pathID)
		delete(pathChannelLoopByPath, pathID)
		delete(pathsByUnitType, unitKey)
	})

	dup := validClericShapedPathFile(pathID)
	dup.Ranks = nil
	dup.Abilities = nil

	pathCatalogMu.Lock()
	err := registerPathFileLocked(unitKey, dup)
	pathCatalogMu.Unlock()
	if err == nil {
		t.Fatal("registerPathFileLocked(duplicate channelLoop) = nil, want error")
	}
	if !strings.Contains(err.Error(), pathID) {
		t.Errorf("error = %q, want it to name %q", err.Error(), pathID)
	}
}

func TestRegisterPathFileLocked_DuplicateAbilitiesOverride_ReturnsError(t *testing.T) {
	pathID := "test_register_dup_abilities"
	unitKey := "test_unit_register_dup_abilities"
	file := validClericShapedPathFile(pathID)
	file.Ranks = nil
	file.ChannelLoop = nil

	pathCatalogMu.Lock()
	if err := registerPathFileLocked(unitKey, file); err != nil {
		pathCatalogMu.Unlock()
		t.Fatalf("first registerPathFileLocked call = %v, want nil", err)
	}
	pathCatalogMu.Unlock()
	t.Cleanup(func() {
		pathCatalogMu.Lock()
		defer pathCatalogMu.Unlock()
		delete(pathBoundsByPath, pathID)
		delete(pathVisionRangeByPath, pathID)
		delete(pathProjectileByPath, pathID)
		delete(pathDamageTypeByPath, pathID)
		delete(pathAttackTypeByPath, pathID)
		delete(pathProjectileScaleByPath, pathID)
		delete(pathAbilitiesByPath, pathID)
		delete(pathsByUnitType, unitKey)
	})

	dup := validClericShapedPathFile(pathID)
	dup.Ranks = nil
	dup.ChannelLoop = nil

	pathCatalogMu.Lock()
	err := registerPathFileLocked(unitKey, dup)
	pathCatalogMu.Unlock()
	if err == nil {
		t.Fatal("registerPathFileLocked(duplicate abilities override) = nil, want error")
	}
	if !strings.Contains(err.Error(), pathID) {
		t.Errorf("error = %q, want it to name %q", err.Error(), pathID)
	}
}
