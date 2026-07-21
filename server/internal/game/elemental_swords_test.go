package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestCraftedSwords_LoadAllThree verifies that all three crafted elemental
// swords load from the catalog with the correct flat-damage modifier,
// on-hit elemental bonus, and proc definition.
func TestCraftedSwords_LoadAllThree(t *testing.T) {
	cases := []struct {
		id          string
		wantElement DamageType
		wantAbility string
	}{
		{id: "fire_sword", wantElement: DamageFire, wantAbility: "fire_bolt"},
		{id: "frost_sword", wantElement: DamageCold, wantAbility: "frost_bolt"},
		{id: "lightning_sword", wantElement: DamageLightning, wantAbility: "chain_lightning"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.id, func(t *testing.T) {
			def, ok := getItemDef(tc.id)
			if !ok {
				t.Fatalf("%s: not found in catalog", tc.id)
			}

			// Positive flat damage modifier (exact value is a catalog tunable).
			if def.Modifiers == nil || def.Modifiers.Damage <= 0 {
				t.Fatalf("%s: expected a positive Modifiers.Damage, got %+v", tc.id, def.Modifiers)
			}

			// onHitElemental must contain an entry of the correct element with a
			// positive amount. The amount itself is a balance tunable.
			found := false
			for _, e := range def.OnHitElemental {
				if e.Type == tc.wantElement && e.Amount > 0 {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("%s: expected an onHitElemental entry of type %s with a positive amount, got %+v", tc.id, tc.wantElement, def.OnHitElemental)
			}

			proc := firstProcFor(t, def, ProcOnHit)
			if proc.Chance <= 0 || proc.Chance > 1 {
				t.Errorf("%s: proc chance %v is not a valid probability in (0,1]", tc.id, proc.Chance)
			}
			// The sword's proc casts a registered ability of the sword's element.
			if proc.Ability != tc.wantAbility {
				t.Errorf("%s: proc ability want %q, got %q", tc.id, tc.wantAbility, proc.Ability)
			}
			adef, ok := getAbilityDef(proc.Ability)
			if !ok {
				t.Fatalf("%s: proc ability %q is not a registered ability", tc.id, proc.Ability)
			}
			if adef.DamageType != tc.wantElement {
				t.Errorf("%s: proc ability damageType want %s, got %s", tc.id, tc.wantElement, adef.DamageType)
			}
		})
	}
}

// TestFrostSword_ProcCastsFrostBolt_EndToEnd drives the full-circle path: an
// on-hit proc that names an ability launches that ability (free, no mana) at
// the struck target, and the landed Frost Bolt both damages AND chills via the
// composed status — the same chill composition Shatter uses. No cold-slow track
// involved.
func TestFrostSword_ProcCastsFrostBolt_EndToEnd(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xF205)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	attacker.CurrentMana = 0 // prove the proc is FREE (no mana required)

	target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 1000, MaxHP: 1000, X: 60, Y: 0, MoveSpeed: 100, AttackSpeed: 1.0}
	s.nextUnitID++
	s.addUnitLocked(target)

	// Force the frost_sword-style ability proc to fire (Chance 1.0), then land
	// the launched projectile on the target.
	attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{{Chance: 1.0, Ability: "frost_bolt"}}
	s.rollEquipmentProcsLocked(attacker, target)

	if len(s.Projectiles) == 0 {
		t.Fatal("frost_bolt proc cast launched no projectile")
	}
	dead := []int{}
	s.landProjectileLocked(s.Projectiles[0], target, &dead)

	if target.HP >= 1000 {
		t.Errorf("frost_bolt impact dealt no damage: HP = %d, want < 1000", target.HP)
	}
	// Chilled via the composition (overlay + real move/attack slow), NOT the
	// cold-slow track.
	if got := s.unitOverlayColorLocked(target); got != "#96d6ff" {
		t.Errorf("target not tinted by frost_bolt chill: overlay = %q, want #96d6ff", got)
	}
	if mult := s.perkMoveSpeedMultiplierLocked(target); mult >= 1.0 {
		t.Errorf("target move speed not slowed by frost_bolt chill: multiplier = %v, want < 1", mult)
	}
}

// TestFireSword_ProcCastsFireBolt_EndToEnd proves fire_sword's ability proc
// lands the Fire Bolt and that its burn is a real damage-over-time COMPOSITION
// (apply_status_duration + on_tick deal_damage), not the bespoke BurnStacks
// proc effect: the target keeps losing HP as the status ticks.
func TestFireSword_ProcCastsFireBolt_EndToEnd(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xF12E)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	attacker.CurrentMana = 0 // free proc

	target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 1000, MaxHP: 1000, X: 60, Y: 0}
	s.nextUnitID++
	s.addUnitLocked(target)

	attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{{Chance: 1.0, Ability: "fire_bolt"}}
	s.rollEquipmentProcsLocked(attacker, target)
	if len(s.Projectiles) == 0 {
		t.Fatal("fire_bolt proc cast launched no projectile")
	}
	dead := []int{}
	s.landProjectileLocked(s.Projectiles[0], target, &dead)

	afterImpact := target.HP
	if afterImpact >= 1000 {
		t.Errorf("fire_bolt impact dealt no direct damage: HP = %d, want < 1000", afterImpact)
	}
	// The burn tint marks the composed DoT status.
	if got := s.unitOverlayColorLocked(target); got == "" {
		t.Errorf("target not tinted by fire_bolt burn status")
	}
	// Tick the status once past its interval — the on_tick deal_damage must burn
	// the afflicted unit for more HP.
	s.tickAbilityStatusesLocked(1.1)
	if target.HP >= afterImpact {
		t.Errorf("fire_bolt burn dealt no damage-over-time: HP %d did not drop below post-impact %d", target.HP, afterImpact)
	}
}

func TestFireSword_EndToEnd(t *testing.T) {
	def, ok := getItemDef("fire_sword")
	if !ok {
		t.Fatalf("fire_sword not found")
	}
	if def.Modifiers == nil || def.Modifiers.Damage <= 0 {
		t.Fatalf("fire_sword should grant positive physical damage, got %+v", def.Modifiers)
	}
	proc := firstProcFor(t, def, ProcOnHit)
	if proc.Ability != "fire_bolt" {
		t.Fatalf("fire_sword proc should cast fire_bolt, got ability=%q", proc.Ability)
	}
	if proc.Chance <= 0 || proc.Chance > 1 {
		t.Fatalf("fire_sword proc chance %v is not a valid probability in (0,1]", proc.Chance)
	}

	// The fire on-hit amount is a catalog tunable; derive the expected values
	// below from the def rather than pinning them so a rebalance can't break
	// this mechanic test.
	var wantFire int
	for _, e := range def.OnHitElemental {
		if e.Type == DamageFire {
			wantFire = e.Amount
			break
		}
	}
	if wantFire <= 0 {
		t.Fatalf("fire_sword should have a positive fire on-hit amount, got %+v", def.OnHitElemental)
	}

	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xF12E)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	attacker.AttackDamageType = ""
	// Equip the fire sword via direct slot set + recompute.
	attacker.InventorySize++
	attacker.Equipped = append(attacker.Equipped, &EquippedItem{InstanceID: s.allocItemInstanceIDLocked(), ItemID: "fire_sword", Stacks: 1})
	s.recomputeUnitEquipmentBonusLocked(attacker)

	if attacker.EquipmentBonus.OnHitElemental[DamageFire] != wantFire {
		t.Fatalf("equipped fire_sword: fire on-hit = %d, want %d", attacker.EquipmentBonus.OnHitElemental[DamageFire], wantFire)
	}

	target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 100, MaxHP: 100}
	s.nextUnitID++
	s.addUnitLocked(target)

	s.resetDamageTypeHintsThisTickLocked()
	s.resetMinorDamageEventsThisTickLocked()
	deadUnitIDs := []int{}
	// Physical 10 + fire on-hit as a separate instance → 100 - 10 - wantFire.
	const physical = 10
	wantHP := target.MaxHP - physical - wantFire
	s.resolveAttackHitLocked(attacker, target, physical, &deadUnitIDs)
	if target.HP != wantHP {
		t.Fatalf("expected HP %d (100 - %d physical - %d fire), got %d", wantHP, physical, wantFire, target.HP)
	}
	// The sword's flat fire component renders as its own side-popup (minor
	// event), not a tint on the main number.
	if e := findMinorEvent(s, target.ID, "fire"); e == nil || e.Damage != wantFire {
		t.Fatalf("expected a fire minor (side) popup of %d from the sword's elemental instance; queue: %+v", wantFire, s.minorDamageEventsThisTick)
	}
}
