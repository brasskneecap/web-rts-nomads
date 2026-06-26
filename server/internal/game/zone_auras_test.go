package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// --- helpers -----------------------------------------------------------------

func statMod(stat, op string, val float64) protocol.StatModifier {
	return protocol.StatModifier{Stat: stat, Operation: op, Value: val}
}

func statAura(stat, op string, val float64) protocol.ZoneAura {
	return protocol.ZoneAura{Type: protocol.ZoneAuraTypeStatModifier, Scope: protocol.ZoneAuraScopeGlobal, Modifier: statMod(stat, op, val)}
}

// auraZone builds a presence zone carrying the given auras.
func auraZone(id, owner string, auras ...protocol.ZoneAura) protocol.Zone {
	z := presenceZone(id, rectCells(0, 0, 2, 2), [2]int{1, 1}, owner)
	z.Auras = auras
	return z
}

// --- vocabulary: stacking rule ----------------------------------------------

func TestStatModifierStacking_AddsSumMulsProduct(t *testing.T) {
	set := newPlayerStatModifierSet()
	set.fold(statMod(statHealthRegen, statOpAdd, 2))
	set.fold(statMod(statHealthRegen, statOpAdd, 3))
	set.fold(statMod(statGoldGatherRate, statOpMultiply, 1.15))
	set.fold(statMod(statGoldGatherRate, statOpMultiply, 1.10))

	if add, mul := set.resolve(statHealthRegen); add != 5 || mul != 1 {
		t.Fatalf("healthRegen resolve = (%v,%v); want (5,1)", add, mul)
	}
	add, mul := set.resolve(statGoldGatherRate)
	if mul < 1.2649 || mul > 1.2651 { // 1.15 * 1.10 = 1.265
		t.Fatalf("goldGatherRate mul = %v; want ~1.265 (product, not sum)", mul)
	}
	if add != 0 {
		t.Fatalf("goldGatherRate add = %v; want 0", add)
	}
}

func TestStatModifierStacking_AddBeforeMultiply(t *testing.T) {
	set := newPlayerStatModifierSet()
	set.fold(statMod(statArmor, statOpAdd, 2))
	set.fold(statMod(statArmor, statOpMultiply, 1.5))
	add, mul := set.resolve(statArmor)
	if got := applyStatModifier(10, add, mul); got != 18 { // (10+2)*1.5
		t.Fatalf("(10+add)*mul = %v; want 18", got)
	}
}

func TestStatModifierSet_EmptyResolvesIdentity(t *testing.T) {
	set := newPlayerStatModifierSet()
	if add, mul := set.resolve(statMoveSpeed); add != 0 || mul != 1 {
		t.Fatalf("empty resolve = (%v,%v); want (0,1)", add, mul)
	}
	var nilSet PlayerStatModifierSet
	if add, mul := nilSet.resolve(statMoveSpeed); add != 0 || mul != 1 {
		t.Fatalf("nil resolve = (%v,%v); want (0,1)", add, mul)
	}
}

// --- validation --------------------------------------------------------------

func TestValidateStatModifier(t *testing.T) {
	if err := validateStatModifier("ctx", statMod(statArmor, statOpAdd, 5)); err != nil {
		t.Fatalf("valid modifier rejected: %v", err)
	}
	if err := validateStatModifier("ctx", statMod("notAStat", statOpAdd, 1)); err == nil {
		t.Fatal("unknown stat accepted; want error")
	}
	if err := validateStatModifier("ctx", statMod(statArmor, "divide", 1)); err == nil {
		t.Fatal("bad operation accepted; want error")
	}
	if err := validateStatModifier("ctx", statMod(statArmor, statOpMultiply, 0)); err == nil {
		t.Fatal("zero-multiply accepted; want error")
	}
}

func TestValidateZones_RejectsUnknownAuraStat(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("validateZones did not panic on unknown aura stat")
		}
	}()
	zones := normalizeZones([]protocol.Zone{
		auraZone("z", "p1", statAura("notAStat", statOpAdd, 1)),
	})
	validateZones("test.json", zones)
}

// --- manager: collection + stacking -----------------------------------------

func TestCollectZoneAuraModifiers_StacksAcrossOwnedZones(t *testing.T) {
	s := newZoneTestState([]protocol.Zone{
		auraZoneAt("a", "p1", 0, 0, statAura(statHealthRegen, statOpAdd, 2), statAura(statGoldGatherRate, statOpMultiply, 1.15)),
		auraZoneAt("b", "p1", 5, 5, statAura(statHealthRegen, statOpAdd, 3), statAura(statGoldGatherRate, statOpMultiply, 1.15)),
		auraZoneAt("c", "neutral", 10, 10, statAura(statHealthRegen, statOpAdd, 99)), // not owned: must not count
	})
	set := s.collectZoneAuraModifiersLocked("p1")
	if add, _ := set.resolve(statHealthRegen); add != 5 {
		t.Fatalf("healthRegen add = %v; want 5 (2+3, neutral zone excluded)", add)
	}
	if _, mul := set.resolve(statGoldGatherRate); mul < 1.3224 || mul > 1.3226 { // 1.15^2
		t.Fatalf("goldGatherRate mul = %v; want ~1.3225", mul)
	}
}

// --- manager: ownership transfer + teardown ---------------------------------

func TestZoneAura_OwnershipTransferAndLoss(t *testing.T) {
	s := newZoneTestState([]protocol.Zone{
		// "north" starts neutral; "seed" is p1's foothold so north is capturable.
		seedZoneAt("seed", "p1", 0, 0),
		func() protocol.Zone {
			z := presenceZone("north", rectCells(0, 5, 2, 7), [2]int{1, 6}, "neutral", "seed")
			z.Auras = []protocol.ZoneAura{statAura(statHealthRegen, statOpAdd, 4)}
			return z
		}(),
	})
	// Initially neutral: no bonus for p1.
	s.recomputeAllZoneAuraModifiersLocked()
	if add, _ := s.Players["p1"].ZoneStatModifiers.resolve(statHealthRegen); add != 0 {
		t.Fatalf("pre-capture healthRegen add = %v; want 0", add)
	}

	// Capture flips north to the team via the chokepoint, which recomputes.
	north := s.zoneRuntimeByIDLocked("north")
	s.setZoneOwnerLocked(north, protocol.ZoneCaptureTeamOwner)
	if add, _ := s.Players["p1"].ZoneStatModifiers.resolve(statHealthRegen); add != 4 {
		t.Fatalf("post-capture healthRegen add = %v; want 4", add)
	}
	// Allied teammate p2 also benefits from the team-owned zone.
	if add, _ := s.Players["p2"].ZoneStatModifiers.resolve(statHealthRegen); add != 4 {
		t.Fatalf("teammate healthRegen add = %v; want 4 (team-owned zone feeds allies)", add)
	}

	// Lose the zone (flip to neutral): bonus removed immediately.
	s.setZoneOwnerLocked(north, protocol.ZoneCaptureNeutralOwner)
	if add, _ := s.Players["p1"].ZoneStatModifiers.resolve(statHealthRegen); add != 0 {
		t.Fatalf("post-loss healthRegen add = %v; want 0", add)
	}
}

// --- read site: armor --------------------------------------------------------

func TestEffectiveArmor_ZoneAuraFold(t *testing.T) {
	s := newZoneTestState([]protocol.Zone{
		auraZoneAt("a", "p1", 0, 0, statAura(statArmor, statOpAdd, 5), statAura(statArmor, statOpMultiply, 2)),
	})
	s.recomputeAllZoneAuraModifiersLocked()
	u := &Unit{ID: 1, OwnerID: "p1", Armor: 10, HP: 100, Visible: true}
	// (10*(1+0) + 0 + 5) * 2 = 30
	if got := s.effectiveArmorLocked(u); got != 30 {
		t.Fatalf("effective armor = %d; want 30", got)
	}
	// A unit owned by nobody special resolves identity → base armor unchanged.
	other := &Unit{ID: 2, OwnerID: "p2", Armor: 10, HP: 100, Visible: true}
	_ = other
}

// --- extra harness helpers ---------------------------------------------------

func auraZoneAt(id, owner string, x, y int, auras ...protocol.ZoneAura) protocol.Zone {
	z := presenceZone(id, rectCells(x, y, x+2, y+2), [2]int{x + 1, y + 1}, owner)
	z.Auras = auras
	return z
}

func seedZoneAt(id, owner string, x, y int) protocol.Zone {
	return presenceZone(id, rectCells(x, y, x+2, y+2), [2]int{x + 1, y + 1}, owner)
}
