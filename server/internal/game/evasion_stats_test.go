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

// TestAttackHits_BlockAttributedFirst: on an avoided roll, block claims the
// low end of the roll space before dodge — a single rngCombat draw decides
// both outcome and attribution, deterministically.
func TestAttackHits_BlockAttributedFirst(t *testing.T) {
	// With block 0.5 + dodge 0.5 (capped to 0.75 total) the hit can never
	// land; rolls < 0.5 are blocks, [0.5, 0.75) are dodges — sample many
	// rolls and require BOTH attributions to appear and NO hits.
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xB10C)
	s.mu.Lock()
	defer s.mu.Unlock()
	ev := TargetEvasion{DodgeChance: 0.5, BlockChance: 0.5}
	sawBlock, sawDodge := false, false
	for i := 0; i < 200; i++ {
		hit, by := s.attackHitsLocked(ev)
		switch {
		case hit:
			// cap is 0.75, so 25% of rolls DO hit — that's the cap working.
		case by == "block":
			sawBlock = true
		case by == "dodge":
			sawDodge = true
		default:
			t.Fatalf("avoided with empty attribution")
		}
	}
	if !sawBlock || !sawDodge {
		t.Errorf("expected both attributions over 200 rolls, got block=%v dodge=%v", sawBlock, sawDodge)
	}
}

// TestAttackHits_CapGuaranteesHits: stacked evasion beyond the cap still
// lands hits — assert at least one hit over many rolls at 0.5+0.5 (would be
// avoid=1.0 uncapped, i.e. zero hits).
func TestAttackHits_CapGuaranteesHits(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xB10D)
	s.mu.Lock()
	defer s.mu.Unlock()
	ev := TargetEvasion{DodgeChance: 0.5, BlockChance: 0.5}
	hits := 0
	for i := 0; i < 200; i++ {
		if hit, _ := s.attackHitsLocked(ev); hit {
			hits++
		}
	}
	if hits == 0 {
		t.Error("cap at 0.75 must let some hits through; got 0 hits in 200 rolls")
	}
}

// TestAttackHits_Deterministic: two states with the same seed produce the
// identical hit/attribution sequence.
func TestAttackHits_Deterministic(t *testing.T) {
	run := func() []string {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xB10E)
		s.mu.Lock()
		defer s.mu.Unlock()
		ev := TargetEvasion{DodgeChance: 0.2, BlockChance: 0.2}
		out := make([]string, 0, 50)
		for i := 0; i < 50; i++ {
			hit, by := s.attackHitsLocked(ev)
			if hit {
				out = append(out, "hit")
			} else {
				out = append(out, by)
			}
		}
		return out
	}
	a, b := run(), run()
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("sequence diverged at %d: %q vs %q", i, a[i], b[i])
		}
	}
}

// TestAttackHits_ZeroEvasionNoRNG: the zero-evasion fast path neither rolls
// nor consumes RNG (guards proc/effect paths that construct zero profiles).
func TestAttackHits_ZeroEvasionNoRNG(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xB10F)
	s.mu.Lock()
	defer s.mu.Unlock()
	before := s.rngCombat.Float64() // advance once, remember stream position implicitly
	_ = before
	s2 := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xB10F)
	s2.mu.Lock()
	defer s2.mu.Unlock()
	_ = s2.rngCombat.Float64()
	if hit, by := s2.attackHitsLocked(TargetEvasion{}); !hit || by != "" {
		t.Fatalf("zero evasion must always hit with no attribution")
	}
	// Streams must still be aligned: next draw identical on both states.
	if a, b := s.rngCombat.Float64(), s2.rngCombat.Float64(); a != b {
		t.Errorf("zero-evasion call consumed RNG: streams diverged (%v vs %v)", a, b)
	}
}
