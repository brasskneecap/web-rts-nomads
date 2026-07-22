package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// abilityDamageTestDef is a minimal ability whose only relevant property is a
// damage amount to scale.
func abilityDamageTestDef() AbilityDef {
	return AbilityDef{ID: "test_ability_damage", DisplayName: "Test"}
}

// TestAbilityDamageStat covers the unit-level ability-damage stat
// (docs/design/ability_perk_interaction.md D3): abilities a unit casts scale by
// it, it defaults to identity so introducing it changed nothing, and — the
// whole point — it accumulates from ANY source through the shared stat
// chokepoint, not just from perks.
func TestAbilityDamageStat(t *testing.T) {
	const base = 100

	t.Run("identity by default (no regression for existing abilities)", func(t *testing.T) {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x9A01)
		s.mu.Lock()
		defer s.mu.Unlock()

		caster := s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{})
		if caster == nil {
			t.Fatal("spawn failed")
		}
		if got := s.effectiveAbilityDamageLocked(caster, abilityDamageTestDef(), base); got != base {
			t.Fatalf("damage = %d, want %d unchanged (stat defaults to 1x)", got, base)
		}
	})

	t.Run("authored unit base scales ability damage", func(t *testing.T) {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x9A02)
		s.mu.Lock()
		defer s.mu.Unlock()

		def := baseStatTestDef(map[string]float64{statAbilityDamage: 1.5})
		caster := s.spawnUnitFromDefLocked(def, def.Type, "p1", "#fff", protocol.Vec2{})

		if got := s.effectiveAbilityDamageLocked(caster, abilityDamageTestDef(), base); got != 150 {
			t.Fatalf("damage = %d, want 150 (100 x 1.5 authored base)", got)
		}
	})

	t.Run("a status contributes, proving the shared stat chokepoint", func(t *testing.T) {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x9A03)
		s.mu.Lock()
		defer s.mu.Unlock()

		caster := s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{})
		if caster == nil {
			t.Fatal("spawn failed")
		}
		// A status is a different emitter from a perk — if this folds, so do
		// items/advancements/auras, which all route through the same pool.
		spawnTestStatusWithMods(s, caster, 5, []PerkStatModifier{
			{Stat: statAbilityDamage, Op: statOpAdd, Value: 0.25},
		})

		if got := s.effectiveAbilityDamageLocked(caster, abilityDamageTestDef(), base); got != 125 {
			t.Fatalf("damage = %d, want 125 (100 x (1.0 + 0.25 status))", got)
		}
	})

	t.Run("authored base and a status compose", func(t *testing.T) {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x9A04)
		s.mu.Lock()
		defer s.mu.Unlock()

		def := baseStatTestDef(map[string]float64{statAbilityDamage: 1.5})
		caster := s.spawnUnitFromDefLocked(def, def.Type, "p1", "#fff", protocol.Vec2{})
		spawnTestStatusWithMods(s, caster, 5, []PerkStatModifier{
			{Stat: statAbilityDamage, Op: statOpAdd, Value: 0.5},
		})

		// (1.5 base + 0.5 status) = 2.0
		if got := s.effectiveAbilityDamageLocked(caster, abilityDamageTestDef(), base); got != 200 {
			t.Fatalf("damage = %d, want 200 (100 x (1.5 + 0.5))", got)
		}
	})

	t.Run("registered and base-authorable, identity default", func(t *testing.T) {
		if !isKnownStat(statAbilityDamage) {
			t.Error("abilityDamage should be a registered stat")
		}
		if !isBaseAuthorableStat(statAbilityDamage) {
			t.Error("abilityDamage should be base-authorable")
		}
		if got := statBaseDefault(statAbilityDamage); got != 1 {
			t.Errorf("statBaseDefault(abilityDamage) = %v, want 1 (identity multiplier)", got)
		}
		if !isFractionStat(statAbilityDamage) {
			t.Error("abilityDamage has a fixed 1.0 baseline, so an add is percentage points ⇒ IsFraction")
		}
		// A fixed-1.0-baseline multiplier must accept a base above 1 without
		// tripping the fraction clamp that guards 0-1 stats like lifesteal.
		valid := baseStatTestDef(map[string]float64{statAbilityDamage: 1.5})
		if err := validateUnitDef(&valid); err != nil {
			t.Errorf("abilityDamage base of 1.5 rejected: %v", err)
		}
	})
}
