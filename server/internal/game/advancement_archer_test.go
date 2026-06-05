package game

// QA tests for the Archer advancement track (items 1–7: HP/damage/attack-speed
// stat nodes + spawn-EXP node). These mirror the Soldier advancement QA tests
// but cover the new unitStatMul (percentage) effect kind used by the attack-speed
// nodes. Item 8 (multi-arrow / trap modifiers) is a separate combat feature and
// is intentionally absent from the catalog, so it is not exercised here.
//
// Expected values are derived from the catalog (getUnitDef + GetAdvancementDef)
// rather than hardcoded, so a balance retune of archer.json or advancements.json
// does not silently invalidate these tests.

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// archerFullTrack lists the archer advancement node IDs in prerequisite-chain
// (file) order — items 1 through 7.
var archerFullTrack = []string{
	"archer_hp_1",
	"archer_damage_1",
	"archer_attack_speed_1",
	"archer_keen_recruits",
	"archer_hp_2",
	"archer_damage_2",
	"archer_attack_speed_2",
}

// statAddAmount sums the flat bonus applied to `stat` by the owned node IDs
// (unitStatAdd effects only). Derived from the catalog so the test carries no
// balance constants.
func statAddAmount(t *testing.T, ids []string, stat string) int {
	t.Helper()
	total := 0
	for _, id := range ids {
		node, ok := GetAdvancementDef(id)
		if !ok {
			t.Fatalf("advancement %q not in catalog", id)
		}
		for _, e := range node.Effects {
			if e.Kind == "unitStatAdd" && e.Stat == stat {
				total += e.Amount
			}
		}
	}
	return total
}

// statMulFactor returns the multiplicative factor applied to `stat` by the owned
// node IDs (unitStatMul effects only). Order matches sorted-ID application in
// applyAdvancementsToEffectiveDefsLocked closely enough that a 1e-9 epsilon
// absorbs float-association differences.
func statMulFactor(t *testing.T, ids []string, stat string) float64 {
	t.Helper()
	factor := 1.0
	for _, id := range ids {
		node, ok := GetAdvancementDef(id)
		if !ok {
			t.Fatalf("advancement %q not in catalog", id)
		}
		for _, e := range node.Effects {
			if e.Kind == "unitStatMul" && e.Stat == stat {
				factor *= 1 + e.Percent/100
			}
		}
	}
	return factor
}

// spawnExpTotal sums spawn EXP granted by the owned node IDs.
func spawnExpTotal(t *testing.T, ids []string) int {
	t.Helper()
	total := 0
	for _, id := range ids {
		node, ok := GetAdvancementDef(id)
		if !ok {
			t.Fatalf("advancement %q not in catalog", id)
		}
		for _, e := range node.Effects {
			if e.Kind == "unitSpawnExp" {
				total += e.Amount
			}
		}
	}
	return total
}

func spawnArcherWith(t *testing.T, ids []string, seed int64) *Unit {
	t.Helper()
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), seed)
	s.EnsurePlayerWithUpgrades("p1", nil, nil, ids)
	s.mu.Lock()
	archer := s.spawnPlayerUnitLocked("archer", "p1", "#ff0000", protocol.Vec2{X: 400, Y: 400})
	s.mu.Unlock()
	if archer == nil {
		t.Fatal("spawnPlayerUnitLocked returned nil for archer")
	}
	return archer
}

// ─── HP nodes (unitStatAdd maxHp) ─────────────────────────────────────────────

func TestArcherAdvancement_HP1_MaxHPSet(t *testing.T) {
	catalogDef, ok := getUnitDef("archer")
	if !ok {
		t.Skip("archer not in unit catalog")
	}
	ids := []string{"archer_hp_1"}
	wantHP := catalogDef.HP + statAddAmount(t, ids, "maxHp")

	archer := spawnArcherWith(t, ids, 42)
	if archer.HP != wantHP || archer.MaxHP != wantHP {
		t.Errorf("archer_hp_1: HP/MaxHP want %d (catalog %d + advancement), got HP=%d MaxHP=%d",
			wantHP, catalogDef.HP, archer.HP, archer.MaxHP)
	}
}

func TestArcherAdvancement_NoAdvancement_BaseStats(t *testing.T) {
	catalogDef, ok := getUnitDef("archer")
	if !ok {
		t.Skip("archer not in unit catalog")
	}
	archer := spawnArcherWith(t, nil, 42)
	if archer.HP != catalogDef.HP || archer.MaxHP != catalogDef.HP {
		t.Errorf("no advancements: HP/MaxHP want %d (catalog base), got HP=%d MaxHP=%d",
			catalogDef.HP, archer.HP, archer.MaxHP)
	}
	if archer.AttackSpeed != catalogDef.AttackSpeed {
		t.Errorf("no advancements: AttackSpeed want %v (catalog base), got %v",
			catalogDef.AttackSpeed, archer.AttackSpeed)
	}
}

// ─── Damage node (unitStatAdd damage) ─────────────────────────────────────────

func TestArcherAdvancement_Damage1_BaseDamageSet(t *testing.T) {
	catalogDef, ok := getUnitDef("archer")
	if !ok {
		t.Skip("archer not in unit catalog")
	}
	ids := []string{"archer_hp_1", "archer_damage_1"}
	wantDamage := catalogDef.Damage + statAddAmount(t, ids, "damage")

	archer := spawnArcherWith(t, ids, 42)
	if archer.BaseDamage != wantDamage {
		t.Errorf("archer_damage_1: BaseDamage want %d (catalog %d + advancement), got %d",
			wantDamage, catalogDef.Damage, archer.BaseDamage)
	}
}

// ─── Attack-speed nodes (unitStatMul attackSpeed) ─────────────────────────────

func TestArcherAdvancement_AttackSpeed1_ScalesByPercent(t *testing.T) {
	catalogDef, ok := getUnitDef("archer")
	if !ok {
		t.Skip("archer not in unit catalog")
	}
	ids := []string{"archer_hp_1", "archer_damage_1", "archer_attack_speed_1"}
	wantSpeed := catalogDef.AttackSpeed * statMulFactor(t, ids, "attackSpeed")

	archer := spawnArcherWith(t, ids, 42)
	if math.Abs(archer.AttackSpeed-wantSpeed) > 1e-9 {
		t.Errorf("archer_attack_speed_1: AttackSpeed want %v (catalog %v ×factor), got %v",
			wantSpeed, catalogDef.AttackSpeed, archer.AttackSpeed)
	}
	// BaseAttackSpeed is seeded from the same effective def at spawn.
	if math.Abs(archer.BaseAttackSpeed-wantSpeed) > 1e-9 {
		t.Errorf("archer_attack_speed_1: BaseAttackSpeed want %v, got %v", wantSpeed, archer.BaseAttackSpeed)
	}
}

func TestArcherAdvancement_AttackSpeed_StacksMultiplicatively(t *testing.T) {
	catalogDef, ok := getUnitDef("archer")
	if !ok {
		t.Skip("archer not in unit catalog")
	}
	wantSpeed := catalogDef.AttackSpeed * statMulFactor(t, archerFullTrack, "attackSpeed")

	archer := spawnArcherWith(t, archerFullTrack, 42)
	if math.Abs(archer.AttackSpeed-wantSpeed) > 1e-9 {
		t.Errorf("full track: AttackSpeed want %v (catalog %v × both +10%% nodes), got %v",
			wantSpeed, catalogDef.AttackSpeed, archer.AttackSpeed)
	}
	// Two +10% nodes must compound, not merely add: factor must exceed 1.20.
	if got := statMulFactor(t, archerFullTrack, "attackSpeed"); got <= 1.20 {
		t.Errorf("attack-speed factor want >1.20 (multiplicative), got %v", got)
	}
}

// ─── Spawn EXP node (unitSpawnExp) ────────────────────────────────────────────

func TestArcherAdvancement_KeenRecruits_SpawnExp(t *testing.T) {
	if _, ok := getUnitDef("archer"); !ok {
		t.Skip("archer not in unit catalog")
	}
	ids := []string{"archer_hp_1", "archer_damage_1", "archer_attack_speed_1", "archer_keen_recruits"}
	wantXP := spawnExpTotal(t, ids)

	archer := spawnArcherWith(t, ids, 42)
	if archer.XP != wantXP {
		t.Errorf("archer_keen_recruits: XP want %d, got %d", wantXP, archer.XP)
	}
}

// ─── Full track stack ─────────────────────────────────────────────────────────

func TestArcherAdvancement_FullTrack_AllBonusesStack(t *testing.T) {
	catalogDef, ok := getUnitDef("archer")
	if !ok {
		t.Skip("archer not in unit catalog")
	}
	wantHP := catalogDef.HP + statAddAmount(t, archerFullTrack, "maxHp")
	wantDamage := catalogDef.Damage + statAddAmount(t, archerFullTrack, "damage")
	wantSpeed := catalogDef.AttackSpeed * statMulFactor(t, archerFullTrack, "attackSpeed")
	wantXP := spawnExpTotal(t, archerFullTrack)

	archer := spawnArcherWith(t, archerFullTrack, 42)

	if archer.HP != wantHP || archer.MaxHP != wantHP {
		t.Errorf("full track: HP/MaxHP want %d, got HP=%d MaxHP=%d", wantHP, archer.HP, archer.MaxHP)
	}
	if archer.BaseDamage != wantDamage {
		t.Errorf("full track: BaseDamage want %d, got %d", wantDamage, archer.BaseDamage)
	}
	if math.Abs(archer.AttackSpeed-wantSpeed) > 1e-9 {
		t.Errorf("full track: AttackSpeed want %v, got %v", wantSpeed, archer.AttackSpeed)
	}
	if archer.XP != wantXP {
		t.Errorf("full track: XP want %d, got %d", wantXP, archer.XP)
	}
}

// ─── Determinism ──────────────────────────────────────────────────────────────

func TestArcherAdvancement_Determinism_SameSeedSameResult(t *testing.T) {
	if _, ok := getUnitDef("archer"); !ok {
		t.Skip("archer not in unit catalog")
	}
	const seed = int64(54321)

	type record struct {
		hp, maxHP, baseDamage, xp int
		attackSpeed               float64
	}
	run := func() record {
		u := spawnArcherWith(t, archerFullTrack, seed)
		return record{hp: u.HP, maxHP: u.MaxHP, baseDamage: u.BaseDamage, xp: u.XP, attackSpeed: u.AttackSpeed}
	}
	if r1, r2 := run(), run(); r1 != r2 {
		t.Errorf("determinism failure: run1=%+v run2=%+v", r1, r2)
	}
}

// ─── unitStatMul catalog validation ───────────────────────────────────────────

func TestAdvancementCatalogValidation_UnitStatMul_ZeroPercent_Panics(t *testing.T) {
	mustPanicLoader(t, "unitStatMul percent == 0", func() {
		loadAdvancementDefsFromRaw("test.json", advancementTrackFile{
			UnitType: "archer",
			Nodes: []UnitAdvancementNode{
				{ID: "x", Kind: "minor", Cost: 10, Effects: []UnitAdvancementEffect{{Kind: "unitStatMul", Stat: "attackSpeed", Percent: 0}}},
			},
		})
	})
}

func TestAdvancementCatalogValidation_UnitStatMul_InvalidStat_Panics(t *testing.T) {
	mustPanicLoader(t, "unitStatMul invalid stat", func() {
		loadAdvancementDefsFromRaw("test.json", advancementTrackFile{
			UnitType: "archer",
			Nodes: []UnitAdvancementNode{
				{ID: "x", Kind: "minor", Cost: 10, Effects: []UnitAdvancementEffect{{Kind: "unitStatMul", Stat: "critChance", Percent: 10}}},
			},
		})
	})
}

// TestAdvancementCatalogValidation_UnitStatMul_AttackSpeed_Valid verifies a
// well-formed unitStatMul attackSpeed effect loads without panicking.
func TestAdvancementCatalogValidation_UnitStatMul_AttackSpeed_Valid(t *testing.T) {
	loadAdvancementDefsFromRaw("test.json", advancementTrackFile{
		UnitType: "archer",
		Nodes: []UnitAdvancementNode{
			{ID: "qa_atkspd_valid", Kind: "minor", Cost: 25, Effects: []UnitAdvancementEffect{{Kind: "unitStatMul", Stat: "attackSpeed", Percent: 10}}},
		},
	})
}
