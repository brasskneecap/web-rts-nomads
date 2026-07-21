package game

import (
	"math"
	"testing"
)

// ═════════════════════════════════════════════════════════════════════════════
// effectiveStatLocked / unitStatStagesLocked — the stat chokepoint.
//
// These two helpers replaced the per-stat "unitPerkStatModifiersLocked +
// playerStatModifierLocked + applyStatStages" boilerplate that every read
// site hand-rolled — and, crucially, that MOST read sites forgot to feed the
// STATUS emitter (unitStatusStatModifiersLocked) into. Before the chokepoint a
// status-authored change_stat only reached armor and healingReceived; after
// it, every stat's read site folds perk + status + (zone) uniformly. These
// tests pin that new uniformity: a synthetic status carrying a change_stat now
// visibly moves each read site that used to ignore it — moveSpeed (the
// chill-as-composition goal), damage, manaRegen — while the existing
// armor/healingReceived coverage (ability_status_stat_modifiers_test.go) still
// holds.
// ═════════════════════════════════════════════════════════════════════════════

const effStatEps = 1e-9

// TestEffectiveStatLocked_FoldsStatusEmitter proves the chokepoint folds the
// status emitter (the half most read sites used to drop) and stays identity
// for a nil unit / unregistered stat / no-modifier unit.
func TestEffectiveStatLocked_FoldsStatusEmitter(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	target := teamCombatUnit(t, s, "p2", 50, 0)

	// nil unit → base unchanged.
	if got := s.effectiveStatLocked(nil, 100, statArmor); got != 100 {
		t.Fatalf("nil unit: got %v, want 100 (base unchanged)", got)
	}
	// Unregistered stat → base unchanged.
	if got := s.effectiveStatLocked(target, 100, "not_a_real_stat"); got != 100 {
		t.Fatalf("unknown stat: got %v, want 100 (base unchanged)", got)
	}
	// No modifiers active → base unchanged (identity).
	if got := s.effectiveStatLocked(target, 100, statArmor); got != 100 {
		t.Fatalf("no modifiers: got %v, want 100 (identity)", got)
	}

	// One status {armor, add, -30} and one {armor, multiply, 0.5} compose as
	// (100 - 30) * 0.5 = 35 through the base stage.
	spawnTestStatusWithMods(s, target, 5, []PerkStatModifier{
		{Stat: statArmor, Op: statOpAdd, Value: -30},
		{Stat: statArmor, Op: statOpMultiply, Value: 0.5},
	})
	if got := s.effectiveStatLocked(target, 100, statArmor); math.Abs(got-35) > effStatEps {
		t.Fatalf("status-folded armor = %v, want 35 ((100-30)*0.5)", got)
	}
}

// TestMoveSpeedMultiplier_StatusChangeStat_Slows is the chill-as-composition
// proof: a status carrying change_stat(moveSpeed, multiply, 0.5) now halves the
// multiplier perkMoveSpeedMultiplierLocked returns — WITHOUT any cold-slow
// track involvement. This is the read-site change that lets chill (and every
// future slow) be authored as a plain moveSpeed stat modifier.
func TestMoveSpeedMultiplier_StatusChangeStat_Slows(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	target := teamCombatUnit(t, s, "p2", 50, 0)
	target.MoveSpeed = 100 // the read site early-returns when MoveSpeed == 0

	before := s.perkMoveSpeedMultiplierLocked(target)

	spawnTestStatusWithMods(s, target, 5, []PerkStatModifier{
		{Stat: statMoveSpeed, Op: statOpMultiply, Value: 0.5},
	})

	after := s.perkMoveSpeedMultiplierLocked(target)
	if math.Abs(after-before*0.5) > effStatEps {
		t.Fatalf("moveSpeed multiplier with active {moveSpeed,multiply,0.5} status = %v, want %v (before %v × 0.5)", after, before*0.5, before)
	}
}

// TestEffectiveDamageRaw_StatusChangeStat_Scales proves the damage HUD read
// site (effectiveDamageRawLocked) now reflects a status-authored damage
// change_stat — it previously folded only the PERK emitter.
func TestEffectiveDamageRaw_StatusChangeStat_Scales(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	attacker := teamCombatUnit(t, s, "p1", 0, 0)
	before := s.effectiveDamageRawLocked(attacker, 0)
	if before <= 0 {
		t.Fatalf("setup: base raw damage = %v, want > 0", before)
	}

	spawnTestStatusWithMods(s, attacker, 5, []PerkStatModifier{
		{Stat: statDamage, Op: statOpMultiply, Value: 0.5},
	})

	after := s.effectiveDamageRawLocked(attacker, 0)
	if math.Abs(after-before*0.5) > effStatEps {
		t.Fatalf("raw damage with active {damage,multiply,0.5} status = %v, want %v (before %v × 0.5)", after, before*0.5, before)
	}
}

// TestEffectiveManaRegen_StatusChangeStat_Adds proves the manaRegen read site
// now folds the status emitter.
func TestEffectiveManaRegen_StatusChangeStat_Adds(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	// Give the unit a mana pool and a base regen so the read site is live.
	caster.MaxMana = 100
	caster.CurrentMana = 50
	caster.ManaRegenPerSecond = 2

	before := s.effectiveManaRegenLocked(caster)
	if math.Abs(before-2) > effStatEps {
		t.Fatalf("setup: base mana regen = %v, want 2", before)
	}

	spawnTestStatusWithMods(s, caster, 5, []PerkStatModifier{
		{Stat: statManaRegen, Op: statOpAdd, Value: 3},
	})

	after := s.effectiveManaRegenLocked(caster)
	if math.Abs(after-5) > effStatEps {
		t.Fatalf("mana regen with active {manaRegen,add,3} status = %v, want 5 (2 + 3)", after)
	}
}
