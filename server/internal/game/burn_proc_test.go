package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestOnHitProc_BurnIgnitesOnLand drives the fire_sword path: a proc bolt
// carrying a burn lands on its target and ignites it via the shared burn
// system (a fire DoT ticking BurnDamagePerSecond over BurnDurationSeconds).
// Values are read from the proc so a balance tweak can't break the test.
func TestOnHitProc_BurnIgnitesOnLand(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xB021)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 1_000_000, MaxHP: 1_000_000, X: 50, Y: 0}
	s.nextUnitID++
	s.addUnitLocked(target)

	proc := EquipmentProc{Chance: 1.0, Params: ProcEffectParams{
		Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt",
		BurnDamagePerSecond: 8, BurnDurationSeconds: 3,
	}}
	attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{proc}

	// Fire the proc, then land the bolt (its own projectile).
	s.rollEquipmentProcsLocked(attacker, target)
	if len(s.Projectiles) != 1 {
		t.Fatalf("expected 1 proc bolt, got %d", len(s.Projectiles))
	}
	// Not on fire before the bolt lands.
	if len(target.PerkState.BurnStacks) != 0 {
		t.Fatalf("target should not be burning before the bolt lands, got %d stacks", len(target.PerkState.BurnStacks))
	}

	dead := []int{}
	s.landProjectileLocked(s.Projectiles[0], target, &dead)

	// Landing ignites exactly one weapon-sourced burn stack carrying the proc's
	// DPS/duration.
	if len(target.PerkState.BurnStacks) != 1 {
		t.Fatalf("expected 1 burn stack after land, got %d", len(target.PerkState.BurnStacks))
	}
	stack := target.PerkState.BurnStacks[0]
	if stack.SourceKind != burnSourceWeapon {
		t.Errorf("burn SourceKind = %q, want %q (weapon, not trap)", stack.SourceKind, burnSourceWeapon)
	}
	if stack.DPS != proc.Params.BurnDamagePerSecond {
		t.Errorf("burn DPS = %v, want %v", stack.DPS, proc.Params.BurnDamagePerSecond)
	}
	if stack.Remaining != proc.Params.BurnDurationSeconds {
		t.Errorf("burn Remaining = %v, want %v", stack.Remaining, proc.Params.BurnDurationSeconds)
	}
	if stack.OwnerUnitID != attacker.ID {
		t.Errorf("burn OwnerUnitID = %d, want %d (the wielder)", stack.OwnerUnitID, attacker.ID)
	}

	// maxBurnRemaining feeds the client's burningRemaining → burning overlay.
	if got := target.PerkState.maxBurnRemaining(); got != proc.Params.BurnDurationSeconds {
		t.Errorf("maxBurnRemaining = %v, want %v", got, proc.Params.BurnDurationSeconds)
	}

	// Tick the burn system to completion. The DoT deals fire damage over its
	// duration; the stack expires once its Remaining runs out. Cap iterations so
	// a wiring regression can't hang the test.
	const dt = 1.0 / gameTicksPerSecond
	hpBefore := target.HP
	ticks := int((proc.Params.BurnDurationSeconds+1.0)/dt) + 1
	for i := 0; i < ticks && len(target.PerkState.BurnStacks) > 0; i++ {
		s.tickTrapperSilverDebuffsLocked(dt)
	}
	lost := hpBefore - target.HP

	// The burn dealt fire damage and never exceeds the authored total
	// (DPS × duration). Asserted as an invariant band, not a pinned number.
	if lost <= 0 {
		t.Errorf("burn dealt no damage over its duration (HP unchanged)")
	}
	maxTotal := int(proc.Params.BurnDamagePerSecond*proc.Params.BurnDurationSeconds) + 1
	if lost > maxTotal {
		t.Errorf("burn dealt %d damage, exceeds authored total ~%d (DPS %v × %vs)", lost, maxTotal, proc.Params.BurnDamagePerSecond, proc.Params.BurnDurationSeconds)
	}
	// The burn stack expires — a unit doesn't burn forever.
	if len(target.PerkState.BurnStacks) != 0 {
		t.Errorf("burn stack should expire by end of duration, got %d remaining", len(target.PerkState.BurnStacks))
	}
}

// TestBurningEffect_DefIsAnchored guards the burning effect's catalog def: it
// must be registered with a valid anchor, since that anchor is the server-side
// source of truth for where the client paints the burning overlay
// (burningOverlayAnchorLocked → UnitSnapshot.BurningAnchor). Asserted as an
// invariant (any valid anchor), not pinned to a specific value, so re-anchoring
// the flame in the catalog doesn't break the test.
func TestBurningEffect_DefIsAnchored(t *testing.T) {
	def, ok := getEffectDef("burning")
	if !ok {
		t.Fatal("burning effect not registered — catalog/effects/burning/burning.json missing?")
	}
	if !isValidEffectAnchor(def.Anchor.OrCenter()) {
		t.Errorf("burning anchor %q is not a valid EffectAnchor", def.Anchor)
	}
}

// TestBurningOverlayAnchor_OnlyWhenBurning verifies the snapshot seam: a unit
// reports the burning anchor only while it is actually on fire (so the field is
// omitted from the wire otherwise), and the reported anchor matches the
// catalog def.
func TestBurningOverlayAnchor_OnlyWhenBurning(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xB022)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 100, MaxHP: 100}
	s.nextUnitID++
	s.addUnitLocked(u)

	// Not burning ⇒ no anchor (omitted from the wire).
	if got := s.burningOverlayAnchorLocked(u); got != "" {
		t.Errorf("un-burnt unit reported anchor %q, want \"\"", got)
	}

	// Ignite it, then the anchor mirrors the catalog def.
	s.applyProcBurnLocked(u.ID, 8, 3, u.ID)
	def, _ := getEffectDef("burning")
	want := string(def.Anchor.OrCenter())
	if got := s.burningOverlayAnchorLocked(u); got != want {
		t.Errorf("burning unit reported anchor %q, want %q (from catalog)", got, want)
	}
}

// TestFireSword_ProcIsWiredToBurn guards the shipped catalog: the fire_sword
// proc carries a real burn (positive DPS for a positive duration). Asserted as
// invariants, not pinned numbers, so a balance tweak doesn't break it.
func TestFireSword_ProcIsWiredToBurn(t *testing.T) {
	def, ok := getItemDef("fire_sword")
	if !ok {
		t.Fatal("fire_sword not in catalog")
	}
	p := def.OnHitProc
	if p == nil {
		t.Fatal("fire_sword has no onHitProc")
	}
	if p.BurnDamagePerSecond <= 0 {
		t.Errorf("fire_sword burn needs a positive DPS, got %v", p.BurnDamagePerSecond)
	}
	if p.BurnDurationSeconds <= 0 {
		t.Errorf("fire_sword burn needs a positive duration, got %v", p.BurnDurationSeconds)
	}
}
