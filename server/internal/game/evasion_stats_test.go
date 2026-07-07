package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestEvasion_BaseDodgeOnFreshUnit: every unit dodges at the game-wide base
// with zero block, before any path or equipment contribution.
func TestEvasion_BaseDodgeOnFreshUnit(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xD0D6E)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	ev := evasionForUnit(u)
	if ev.DodgeChance != baseUnitDodgeChance {
		t.Errorf("fresh unit dodge = %v, want base %v", ev.DodgeChance, baseUnitDodgeChance)
	}
	if ev.BlockChance != 0 {
		t.Errorf("fresh unit block = %v, want 0", ev.BlockChance)
	}
}

// TestEvasion_VanguardBlockScalesWithRank guards the shipped catalog: the
// Vanguard path authors a per-rank blockChance that climbs bronze→gold.
// Asserted as invariants (positive, strictly increasing), not pinned numbers.
func TestEvasion_VanguardBlockScalesWithRank(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xD0D6F)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	var prev float64
	for _, rank := range []string{"bronze", "silver", "gold"} {
		u.ProgressionPath = "vanguard"
		u.Rank = rank
		s.applyRankModifiersLocked(u, false)
		ev := evasionForUnit(u)
		if ev.BlockChance <= prev {
			t.Errorf("vanguard %s block = %v, want > previous rank's %v", rank, ev.BlockChance, prev)
		}
		// Dodge keeps the game-wide base — the path doesn't author dodge.
		if ev.DodgeChance != baseUnitDodgeChance {
			t.Errorf("vanguard %s dodge = %v, want base %v", rank, ev.DodgeChance, baseUnitDodgeChance)
		}
		prev = ev.BlockChance
	}
}

// TestEvasion_EquipmentAddsAdditively: item dodge/block modifiers stack onto
// base + path additively through the equipment bonus.
func TestEvasion_EquipmentAddsAdditively(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xD0D70)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	u.EquipmentBonus.DodgeChance = 0.15
	u.EquipmentBonus.BlockChance = 0.05
	ev := evasionForUnit(u)
	if want := baseUnitDodgeChance + 0.15; ev.DodgeChance != want {
		t.Errorf("dodge = %v, want %v (base + equipment)", ev.DodgeChance, want)
	}
	if ev.BlockChance != 0.05 {
		t.Errorf("block = %v, want 0.05 (equipment only)", ev.BlockChance)
	}
}

// TestValidateItemDef_DodgeBlockRange: item dodge/block modifiers outside
// [0,1) are content authoring errors.
func TestValidateItemDef_DodgeBlockRange(t *testing.T) {
	good := &ItemDef{ID: "ok", Kind: ItemKindEquipment, Modifiers: &ItemModifiers{DodgeChance: 0.15, BlockChance: 0.1}}
	if err := validateItemDef(good); err != nil {
		t.Fatalf("valid dodge/block modifiers rejected: %v", err)
	}
	negDodge := &ItemDef{ID: "bad1", Modifiers: &ItemModifiers{DodgeChance: -0.1}}
	if err := validateItemDef(negDodge); err == nil {
		t.Error("expected error for negative dodgeChance, got nil")
	}
	fullBlock := &ItemDef{ID: "bad2", Modifiers: &ItemModifiers{BlockChance: 1.0}}
	if err := validateItemDef(fullBlock); err == nil {
		t.Error("expected error for blockChance >= 1, got nil")
	}
}
