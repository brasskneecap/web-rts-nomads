package game

import (
	"encoding/json"
	"testing"
)

// TestAbilityZoneBurnTicks exercises the create_zone action end-to-end: a
// burning-crater zone fires its on_zone_tick trigger (select_targets +
// deal_damage, reusing the existing executor + target-query — no hand-rolled
// damage here) on a fixed cadence and expires after its Duration.
//
// Tick cadence: TickInterval=0.5, Duration=1.0 ⇒ exactly 3 ticks over the
// zone's life. spawnAbilityZoneLocked arms the first tick IMMEDIATELY
// (tickTimer=0, matching GroundHazard's same-tick-as-impact burn pacing —
// see spawnAbilityZoneLocked's doc comment), so fires are due at simulated
// t=0 (immediate), t=0.5, and t=1.0 — one more than the naive
// floor(Duration/Interval)=2 would predict, mirroring GroundHazard's own
// accumulator-overshoot extra tick (see TestAbilityCompileGolden_Meteor).
// tickAbilityZonesLocked compares against a small epsilon so straddling
// float error doesn't produce an off-by-one tick. The stepping below (10 x
// 0.1s) is the scenario the task specified; 0.1 is not exact in binary but
// the epsilon guard keeps the tick count exact at 3.
func TestAbilityZoneBurnTicks(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 30, 0)
	enemy.HP, enemy.MaxHP = 100, 100

	zoneCfg := json.RawMessage(`{
		"name":"Burning Crater","radius":120,"duration":1.0,"tickInterval":0.5,
		"triggers":[{"id":"tick","type":"on_tick","actions":[
			{"id":"s","type":"select_targets","outputs":{"targets":"hits"},
				"target":{"source":"all_in_scene","origin":"zone_center","radius":120,"relations":["enemy"]}},
			{"id":"d","type":"deal_damage","input":{"targets":{"key":"hits"}},"config":{"amount":12,"type":"fire"}}
		]}]}`)

	ctx := &RuntimeAbilityContext{CasterID: caster.ID, AbilityID: "meteor", Named: map[string]ContextValue{}}
	desc, ok := lookupActionDescriptor(ActionCreateZone)
	if !ok {
		t.Fatal("create_zone action not registered")
	}
	cfg, err := desc.Decode(zoneCfg)
	if err != nil {
		t.Fatal(err)
	}
	desc.Execute(s, ctx, cfg, nil)

	if len(s.AbilityZones) != 1 {
		t.Fatalf("zone not spawned: %d", len(s.AbilityZones))
	}

	// Advance simulated time in 0.1s steps for a total of 1.0s: expect exactly
	// 3 ticks over the zone's lifetime (due at t=0 [immediate], t=0.5, and
	// t=1.0).
	for i := 0; i < 10; i++ {
		s.tickAbilityZonesLocked(0.1)
	}

	if enemy.HP != 64 {
		t.Fatalf("enemy HP = %d, want 64 (100 - 3*12)", enemy.HP)
	}
	if len(s.AbilityZones) != 0 {
		t.Fatalf("zone should have expired: %d", len(s.AbilityZones))
	}
}

// TestAbilityZoneBurnTicks_ExactStepping is the same scenario stepped in
// float-exact increments (2 x 0.5s) as a cross-check that the tick count is a
// property of the cadence, not of the stepping granularity used to reach it.
// With tickTimer armed at 0, the first 0.5s step already has TWO ticks due
// (t=0 and t=0.5, since dt happens to equal TickInterval exactly) — see
// tickAbilityZonesLocked's loop, which fires every tick due within a single
// call — so this scenario's 3 total ticks land as 2 fires in the first call
// and 1 in the second, still summing to the same 3 as the fine-grained
// stepping above.
func TestAbilityZoneBurnTicks_ExactStepping(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 30, 0)
	enemy.HP, enemy.MaxHP = 100, 100

	zoneCfg := json.RawMessage(`{
		"name":"Burning Crater","radius":120,"duration":1.0,"tickInterval":0.5,
		"triggers":[{"id":"tick","type":"on_tick","actions":[
			{"id":"s","type":"select_targets","outputs":{"targets":"hits"},
				"target":{"source":"all_in_scene","origin":"zone_center","radius":120,"relations":["enemy"]}},
			{"id":"d","type":"deal_damage","input":{"targets":{"key":"hits"}},"config":{"amount":12,"type":"fire"}}
		]}]}`)

	ctx := &RuntimeAbilityContext{CasterID: caster.ID, AbilityID: "meteor", Named: map[string]ContextValue{}}
	desc, _ := lookupActionDescriptor(ActionCreateZone)
	cfg, err := desc.Decode(zoneCfg)
	if err != nil {
		t.Fatal(err)
	}
	desc.Execute(s, ctx, cfg, nil)

	s.tickAbilityZonesLocked(0.5)
	s.tickAbilityZonesLocked(0.5)

	if enemy.HP != 64 {
		t.Fatalf("enemy HP = %d, want 64 (100 - 3*12)", enemy.HP)
	}
	if len(s.AbilityZones) != 0 {
		t.Fatalf("zone should have expired: %d", len(s.AbilityZones))
	}
}

// TestTickAbilityZonesLocked_NoZonesIsNoop guards the wiring into the live
// Update() loop: with no zones spawned (the case for every existing test and
// every match until a zone-spawning ability ships), the tick call must be a
// zero-cost no-op.
func TestTickAbilityZonesLocked_NoZonesIsNoop(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	if s.AbilityZones != nil {
		t.Fatalf("AbilityZones should start nil, got %v", s.AbilityZones)
	}
	s.tickAbilityZonesLocked(0.1) // must not panic
	if len(s.AbilityZones) != 0 {
		t.Fatalf("expected no zones, got %d", len(s.AbilityZones))
	}
}

// TestAbilityZone_OwnerLeftMatch_DropsZone mirrors tickGroundHazardsLocked's
// owner-left-match cull.
func TestAbilityZone_OwnerLeftMatch_DropsZone(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)

	ctx := &RuntimeAbilityContext{CasterID: caster.ID, AbilityID: "meteor", Named: map[string]ContextValue{}}
	desc, _ := lookupActionDescriptor(ActionCreateZone)
	cfg, err := desc.Decode(json.RawMessage(`{"radius":50,"duration":5,"tickInterval":1}`))
	if err != nil {
		t.Fatal(err)
	}
	desc.Execute(s, ctx, cfg, nil)
	if len(s.AbilityZones) != 1 {
		t.Fatalf("zone not spawned: %d", len(s.AbilityZones))
	}

	delete(s.Players, "p1")
	s.tickAbilityZonesLocked(0.1)
	if len(s.AbilityZones) != 0 {
		t.Fatalf("zone should be dropped once owner leaves the match: %d", len(s.AbilityZones))
	}
}
