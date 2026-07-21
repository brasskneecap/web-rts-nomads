package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// ═════════════════════════════════════════════════════════════════════════════
// Per-unit BASE stats (UnitDef.BaseStats → Unit.BaseStats, read via
// unitBaseStat). The first step toward "a unit carries a base value for ANY
// registered stat": critChance / critMultiplier — which had a hardcoded global
// default and no typed field — become per-unit-type authorable.
// ═════════════════════════════════════════════════════════════════════════════

// baseStatTestDef is a minimal, valid combat unit def; tests vary its BaseStats.
func baseStatTestDef(baseStats map[string]float64) UnitDef {
	return UnitDef{
		Type: "test_base_stat_unit", Name: "Test", HP: 100, Damage: 10,
		AttackRange: 50, AttackSpeed: 1.0, MoveSpeed: 100,
		BaseStats: baseStats,
	}
}

// TestUnitBaseStat_Resolver covers the pure resolver: authored value wins, else
// the registered default; nil-safe.
func TestUnitBaseStat_Resolver(t *testing.T) {
	// Nil unit → default.
	if got := unitBaseStat(nil, statCritChance); got != defaultCritChance {
		t.Fatalf("nil unit critChance = %v, want default %v", got, defaultCritChance)
	}
	// Unit with no BaseStats → default.
	bare := &Unit{}
	if got := unitBaseStat(bare, statCritChance); got != defaultCritChance {
		t.Fatalf("no-BaseStats unit critChance = %v, want default %v", got, defaultCritChance)
	}
	if got := unitBaseStat(bare, statCritMult); got != defaultCritMultiplier {
		t.Fatalf("no-BaseStats unit critMultiplier = %v, want default %v", got, defaultCritMultiplier)
	}
	// Authored value wins.
	authored := &Unit{BaseStats: map[string]float64{statCritChance: 0.3, statCritMult: 2.5}}
	if got := unitBaseStat(authored, statCritChance); got != 0.3 {
		t.Fatalf("authored critChance = %v, want 0.3", got)
	}
	if got := unitBaseStat(authored, statCritMult); got != 2.5 {
		t.Fatalf("authored critMultiplier = %v, want 2.5", got)
	}
}

// TestBaseStats_SeededAtSpawn proves the def's BaseStats are copied onto the
// spawned unit (and are a distinct map, not the shared catalog def's).
func TestBaseStats_SeededAtSpawn(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xB45E)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := baseStatTestDef(map[string]float64{statCritChance: 0.25})
	u := s.spawnUnitFromDefLocked(def, def.Type, "p1", "#fff", protocol.Vec2{})
	if u == nil {
		t.Fatal("spawn returned nil")
	}
	if got := u.BaseStats[statCritChance]; got != 0.25 {
		t.Fatalf("spawned unit BaseStats[critChance] = %v, want 0.25", got)
	}
	// Mutating the unit's map must not touch the def's authored map.
	u.BaseStats[statCritChance] = 0.99
	if def.BaseStats[statCritChance] != 0.25 {
		t.Fatalf("def BaseStats scribbled by per-unit mutation: %v", def.BaseStats[statCritChance])
	}

	// A def with no BaseStats spawns a unit with a nil map (byte-identical).
	u2 := s.spawnUnitFromDefLocked(baseStatTestDef(nil), "test_base_stat_unit2", "p1", "#fff", protocol.Vec2{})
	if u2.BaseStats != nil {
		t.Fatalf("unauthored unit should carry a nil BaseStats map, got %v", u2.BaseStats)
	}
}

// TestCrit_AuthoredBaseStat proves an authored per-unit base crit chance /
// multiplier flows through the crit read sites.
func TestCrit_AuthoredBaseStat(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xB45F)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := baseStatTestDef(map[string]float64{statCritChance: 0.30, statCritMult: 2.75})
	u := s.spawnUnitFromDefLocked(def, def.Type, "p1", "#fff", protocol.Vec2{})

	// No perks/auras: effective crit chance == the authored base (no target mark).
	if got := s.unitCritChanceLocked(u, nil); got != 0.30 {
		t.Fatalf("unitCritChanceLocked with authored base = %v, want 0.30", got)
	}
	if got := s.unitCritMultiplierLocked(u); got != 2.75 {
		t.Fatalf("unitCritMultiplierLocked with authored base = %v, want 2.75", got)
	}
}

// TestCrit_UnauthoredByteIdentical proves a unit that authors no base crit
// behaves exactly as before this system existed — the global defaults.
func TestCrit_UnauthoredByteIdentical(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xB460)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := s.spawnUnitFromDefLocked(baseStatTestDef(nil), "test_base_stat_unit", "p1", "#fff", protocol.Vec2{})
	if got := s.unitCritChanceLocked(u, nil); got != defaultCritChance {
		t.Fatalf("unauthored unitCritChanceLocked = %v, want global default %v", got, defaultCritChance)
	}
	if got := s.unitCritMultiplierLocked(u); got != defaultCritMultiplier {
		t.Fatalf("unauthored unitCritMultiplierLocked = %v, want global default %v", got, defaultCritMultiplier)
	}
}

// TestValidateUnitDef_BaseStats covers the load-time authoring rules.
func TestValidateUnitDef_BaseStats(t *testing.T) {
	cases := []struct {
		name      string
		baseStats map[string]float64
		wantErr   bool
	}{
		{"valid critChance", map[string]float64{statCritChance: 0.2}, false},
		{"valid critMultiplier", map[string]float64{statCritMult: 2.5}, false},
		{"non-authorable stat (damage has a typed field)", map[string]float64{statDamage: 5}, true},
		{"unknown stat", map[string]float64{"not_a_stat": 1}, true},
		{"critChance out of range", map[string]float64{statCritChance: 1.5}, true},
		{"negative critMultiplier", map[string]float64{statCritMult: -1}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def := baseStatTestDef(tc.baseStats)
			err := validateUnitDef(&def)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateUnitDef baseStats=%v: err=%v, wantErr=%v", tc.baseStats, err, tc.wantErr)
			}
		})
	}
}
