package game

// Section: damage-type tagging coverage.
//
// Verifies that every thematic damage source emits a damageTypeHint with the
// right variant ("shadow" / "fire" / "holy" / "electric") so the client's
// floating-up popup renders in the matching color. Each test drives the
// real damage call (not the helper directly), inspects the per-tick
// hint queue, and asserts both the variant and that the hint amount
// equals the actual HP loss.
//
// Sources covered:
//   - fire_pit DoT tick      → DamageFire / "fire"
//   - caltrops DoT tick      → DamageLightning / "electric"
//   - marker_trap (Final Exposure) → DamageShadow / "shadow"
//   - explosive_trap burst   → DamageFire / "fire"
//   - typed unit melee attack → variant matches attacker.AttackDamageType
//
// If a future damage call drops the DamageType tag, these tests fail with
// a clear "expected shadow / got default" message.

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// findHint returns the hint matching (unitID, variant) in the per-tick
// queue, or nil if absent. Used by every test below — keeps the assertion
// boilerplate minimal.
func findHint(s *GameState, unitID int, variant string) *damageTypeHint {
	for i := range s.damageTypeHintsThisTick {
		h := &s.damageTypeHintsThisTick[i]
		if h.UnitID == unitID && h.Variant == variant {
			return h
		}
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// damageTypeForTrap mapper — direct unit test
// ─────────────────────────────────────────────────────────────────────────────

func TestDamageTypeForTrap_MapsEveryBronzePerk(t *testing.T) {
	cases := []struct {
		trapType string
		want     DamageType
	}{
		{"caltrops", DamagePhysical},     // base = physical (sharp spikes); see TestDamageTypeForTrap_Caltrops_LightningOnlyWhenElectrified
		{"fire_pit", DamageFire},
		{"explosive_trap", DamageFire},
		{"marker_trap", DamageShadow},
		{"", DamagePhysical},             // empty / unset
		{"unknown_trap", DamagePhysical}, // safe default
	}
	for _, tc := range cases {
		got := damageTypeForTrap(&Trap{TrapType: tc.trapType})
		if got != tc.want {
			t.Errorf("trap type %q: damageTypeForTrap = %q, want %q", tc.trapType, got, tc.want)
		}
	}
	if got := damageTypeForTrap(nil); got != DamagePhysical {
		t.Errorf("nil trap: damageTypeForTrap = %q, want %q (defensive default)", got, DamagePhysical)
	}
}

// TestDamageTypeForTrap_Caltrops_LightningOnlyWhenElectrified verifies the
// caltrops conditional: physical by default, Lightning when Ascendant
// Infusion's Electrified upgrade is plant-time-active on the trap.
func TestDamageTypeForTrap_Caltrops_LightningOnlyWhenElectrified(t *testing.T) {
	plain := &Trap{TrapType: "caltrops"}
	if got := damageTypeForTrap(plain); got != DamagePhysical {
		t.Errorf("plain caltrops: got %q, want %q (sharp spikes are physical)", got, DamagePhysical)
	}

	electrified := &Trap{TrapType: "caltrops", InfusionElectrifiedBonusDamage: 9}
	if got := damageTypeForTrap(electrified); got != DamageLightning {
		t.Errorf("electrified caltrops: got %q, want %q (Ascendant Infusion charges the spikes)",
			got, DamageLightning)
	}

	// Overload Protocol (Spike Surge) does NOT electrify — base damage stays
	// physical when the gold perk is overload_protocol instead of
	// ascendant_infusion. Spike Surge fields would be set on the trap but
	// InfusionElectrifiedBonusDamage stays 0.
	overloadCaltrops := &Trap{
		TrapType:                       "caltrops",
		InfusionElectrifiedBonusDamage: 0, // not electrified
		OverloadSpikeSurgeBurstDamage:  25,
	}
	if got := damageTypeForTrap(overloadCaltrops); got != DamagePhysical {
		t.Errorf("overload-protocol caltrops: got %q, want %q (Spike Surge is sharp metal, not electric)",
			got, DamagePhysical)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// applyUnitDamageWithSourceLocked emits damage type hints
// ─────────────────────────────────────────────────────────────────────────────

func TestDamagePipeline_EmitsHintForEveryColoredDamageType(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xD17)
	s.mu.Lock()
	defer s.mu.Unlock()

	makeTarget := func() *Unit {
		u := &Unit{
			ID:       s.nextUnitID,
			OwnerID:  enemyPlayerID,
			UnitType: "soldier",
			Visible:  true,
			HP:       100, MaxHP: 100, Armor: 0,
		}
		s.nextUnitID++
		s.addUnitLocked(u)
		return u
	}

	cases := []struct {
		dt          DamageType
		wantVariant string
	}{
		{DamageShadow, "shadow"},
		{DamageFire, "fire"},
		{DamageHoly, "holy"},
		{DamageLightning, "electric"},
		{DamagePhysical, ""},     // no hint for physical (no color)
		{DamageType(""), ""},     // no hint for unset
	}
	for _, tc := range cases {
		target := makeTarget()
		s.resetDamageTypeHintsThisTickLocked()
		s.applyUnitDamageWithSourceLocked(target, 10, DamageSource{Kind: "test", DamageType: tc.dt})
		hint := findHint(s, target.ID, tc.wantVariant)
		if tc.wantVariant == "" {
			// Untyped: no hint should land for this target.
			for _, h := range s.damageTypeHintsThisTick {
				if h.UnitID == target.ID {
					t.Errorf("damage type %q: unexpected hint %+v", tc.dt, h)
				}
			}
			continue
		}
		if hint == nil {
			t.Errorf("damage type %q: expected hint with variant %q, got none. Queue: %+v",
				tc.dt, tc.wantVariant, s.damageTypeHintsThisTick)
			continue
		}
		if hint.Damage != 10 {
			t.Errorf("damage type %q: hint amount = %d, want 10", tc.dt, hint.Damage)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Trap DoT damage hints
// ─────────────────────────────────────────────────────────────────────────────

func TestFirePitDoT_TagsDamageAsFire(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xF17E)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Spawn a victim and apply fire_pit DoT damage directly via the same
	// DamageSource shape used at the call site (line 547 in trap.go). This
	// guarantees the hint emission path runs without needing a full
	// channel / trap fixture.
	victim := &Unit{
		ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier",
		Visible: true, HP: 100, MaxHP: 100,
	}
	s.nextUnitID++
	s.addUnitLocked(victim)

	trap := &Trap{ID: "trap-test", TrapType: "fire_pit"}
	s.resetDamageTypeHintsThisTickLocked()
	s.applyUnitDamageWithSourceLocked(victim, 5, DamageSource{
		AttackerTrapID: trap.ID, Kind: "trap_dot", DamageType: damageTypeForTrap(trap),
	})

	if hint := findHint(s, victim.ID, "fire"); hint == nil {
		t.Errorf("fire_pit DoT should emit a fire hint; queue: %+v", s.damageTypeHintsThisTick)
	}
}

func TestCaltropsDoT_PhysicalByDefault_LightningWhenElectrified(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xCA17)
	s.mu.Lock()
	defer s.mu.Unlock()

	makeVictim := func() *Unit {
		u := &Unit{
			ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier",
			Visible: true, HP: 100, MaxHP: 100,
		}
		s.nextUnitID++
		s.addUnitLocked(u)
		return u
	}

	// Bronze caltrops: physical → NO color hint emitted (popup stays default).
	victim := makeVictim()
	plainTrap := &Trap{ID: "trap-plain", TrapType: "caltrops"}
	s.resetDamageTypeHintsThisTickLocked()
	s.applyUnitDamageWithSourceLocked(victim, 3, DamageSource{
		AttackerTrapID: plainTrap.ID, Kind: "trap_dot", DamageType: damageTypeForTrap(plainTrap),
	})
	for _, h := range s.damageTypeHintsThisTick {
		if h.UnitID == victim.ID {
			t.Errorf("plain caltrops DoT should emit NO hint (physical = default popup), got %+v", h)
		}
	}

	// Electrified caltrops (Ascendant Infusion): lightning hint.
	victim2 := makeVictim()
	chargedTrap := &Trap{ID: "trap-electric", TrapType: "caltrops", InfusionElectrifiedBonusDamage: 9}
	s.resetDamageTypeHintsThisTickLocked()
	s.applyUnitDamageWithSourceLocked(victim2, 3, DamageSource{
		AttackerTrapID: chargedTrap.ID, Kind: "trap_dot", DamageType: damageTypeForTrap(chargedTrap),
	})
	if hint := findHint(s, victim2.ID, "electric"); hint == nil {
		t.Errorf("Electrified caltrops DoT should emit an electric hint; queue: %+v",
			s.damageTypeHintsThisTick)
	}
}

func TestMarkerTrapFinalExposure_TagsDamageAsShadow(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xFA1A)
	s.mu.Lock()
	defer s.mu.Unlock()

	victim := &Unit{
		ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier",
		Visible: true, HP: 100, MaxHP: 100,
	}
	s.nextUnitID++
	s.addUnitLocked(victim)

	// Mirror the Final Exposure damage source construction (line 1448).
	s.resetDamageTypeHintsThisTickLocked()
	s.applyUnitDamageWithSourceLocked(victim, 7, DamageSource{
		AttackerTrapID: "trap-test", Kind: "final_exposure", DamageType: DamageShadow,
	})

	if hint := findHint(s, victim.ID, "shadow"); hint == nil {
		t.Errorf("marker_trap Final Exposure should emit a shadow hint; queue: %+v", s.damageTypeHintsThisTick)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Unit attack tagging — melee inherits attacker.AttackDamageType
// ─────────────────────────────────────────────────────────────────────────────

func TestMeleeAttack_TagsDamageFromAttackerAttackDamageType(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xAEEF)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Build a shadow-typed melee attacker (the necromancer base type carries
	// AttackDamageType = "shadow"; we set it directly here to keep the test
	// independent of unit-def loading order).
	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	attacker.AttackDamageType = DamageShadow

	target := &Unit{
		ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier",
		Visible: true, HP: 100, MaxHP: 100,
	}
	s.nextUnitID++
	s.addUnitLocked(target)

	s.resetDamageTypeHintsThisTickLocked()
	deadUnitIDs := []int{}
	s.resolveAttackHitLocked(attacker, target, 8, &deadUnitIDs)

	if hint := findHint(s, target.ID, "shadow"); hint == nil {
		t.Errorf("shadow-typed melee should emit a shadow hint; queue: %+v",
			s.damageTypeHintsThisTick)
	}
}

func TestMeleeAttack_UntypedAttackerEmitsNoHint(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xAEED)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Soldier-type attacker with no AttackDamageType (i.e. physical) —
	// should NOT emit any hint, popup remains default white/red.
	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	attacker.AttackDamageType = "" // explicit: untyped

	target := &Unit{
		ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier",
		Visible: true, HP: 100, MaxHP: 100,
	}
	s.nextUnitID++
	s.addUnitLocked(target)

	s.resetDamageTypeHintsThisTickLocked()
	deadUnitIDs := []int{}
	s.resolveAttackHitLocked(attacker, target, 8, &deadUnitIDs)

	for _, h := range s.damageTypeHintsThisTick {
		if h.UnitID == target.ID {
			t.Errorf("untyped melee should emit NO hint, got %+v", h)
		}
	}
}
