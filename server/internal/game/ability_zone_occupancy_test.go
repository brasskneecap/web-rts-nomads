package game

import (
	"encoding/json"
	"testing"

	"webrts/server/pkg/protocol"
)

// ═════════════════════════════════════════════════════════════════════════════
// on_zone_enter / on_zone_exit occupancy triggers
//
// Prior to this change, TriggerOnZoneEnter/TriggerOnZoneExit were reachable
// enum values with zero producers: AbilityZone carried no occupancy state, so
// nothing could ever detect the alive+visible-in-radius transition an
// "enter"/"exit" trigger is supposed to fire on. tickAbilityZonesLocked now
// recomputes occupancy every tick (zoneOccupantIDsLocked), diffs it against
// last tick's (diffSortedUnitIDs), and fires the corresponding trigger via
// fireAbilityZoneOccupancyEventLocked, which also binds CurrentEventUnitID so
// select_targets{source:"current_event"} resolves to the specific unit that
// crossed the boundary. See ability_zone.go's file doc (OCCUPANCY section)
// for the full design rationale this test suite locks in.
//
// This is authored-only: no catalog ability compiles these triggers (see
// TestCatalog_NoAbilityUsesZoneEnterExitTriggers at the bottom), so nothing
// here changes production behavior — only what an author can wire in the
// ability editor.
// ═════════════════════════════════════════════════════════════════════════════

// spawnTestZone builds and registers an AbilityZone directly via
// spawnAbilityZoneLocked (bypassing the create_zone action's JSON/position
// plumbing, which these occupancy tests don't need — they drive Center/
// Radius/Triggers directly for precise control over who's inside). Caller
// holds s.mu.
func spawnTestZone(s *GameState, caster *Unit, center protocol.Vec2, radius, duration, tickInterval float64, triggers []AbilityTriggerDef) *AbilityZone {
	z := &AbilityZone{
		AbilityID:     "test_zone",
		CasterID:      caster.ID,
		OwnerPlayerID: caster.OwnerID,
		Center:        center,
		Radius:        radius,
		Remaining:     duration,
		TickInterval:  tickInterval,
		Triggers:      triggers,
	}
	s.spawnAbilityZoneLocked(z)
	return z
}

// currentEventDamageTrigger builds a trigger whose single action pair binds
// "current_event" (the unit fireAbilityZoneOccupancyEventLocked bound) and
// deals amount fire damage to exactly that unit — the canonical shape an
// author would use to react to who entered/exited.
func currentEventDamageTrigger(id string, ttype TriggerType, amount int) AbilityTriggerDef {
	cfg, _ := json.Marshal(map[string]any{"amount": amount, "type": "fire"})
	return AbilityTriggerDef{
		ID:   id,
		Type: ttype,
		Actions: []AbilityActionDef{
			{
				ID: "sel", Type: ActionSelectTargets,
				Outputs: map[string]string{"targets": "hit"},
				Target:  &TargetQueryDef{Source: SrcCurrentEvent},
			},
			{
				ID: "dmg", Type: ActionDealDamage,
				Input:  map[string]ContextRef{"targets": {Key: "hit"}},
				Config: json.RawMessage(cfg),
			},
		},
	}
}

// traceTriggerFireCount counts "trigger_fired" events recorded for triggerID.
func traceTriggerFireCount(tr *AbilityExecutionTrace, triggerID string) int {
	n := 0
	for _, e := range tr.Events {
		if e.Type == "trigger_fired" && e.Path == triggerID {
			n++
		}
	}
	return n
}

// TestAbilityZoneOccupancy_EnterFiresOnceOnCross is the core semantic: enter
// fires exactly once the tick a unit crosses into the zone, not on every
// subsequent tick it remains inside.
func TestAbilityZoneOccupancy_EnterFiresOnceOnCross(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", -1000, 0) // parked well outside every zone below so it never contaminates occupancy counts
	walker := teamCombatUnit(t, s, "p2", 200, 0)   // starts outside the zone
	walker.HP, walker.MaxHP = 100, 100

	spawnTestZone(s, caster, protocol.Vec2{X: 0, Y: 0}, 50, 10, 0, []AbilityTriggerDef{
		currentEventDamageTrigger("enter", TriggerOnZoneEnter, 10),
	})

	s.tickAbilityZonesLocked(0.1) // still outside: no fire
	if walker.HP != 100 {
		t.Fatalf("walker.HP = %d before crossing in, want 100 (no premature enter fire)", walker.HP)
	}

	walker.X = 0 // cross into the zone
	s.tickAbilityZonesLocked(0.1)
	if walker.HP != 90 {
		t.Fatalf("walker.HP = %d after crossing in, want 90 (one enter fire of 10 dmg)", walker.HP)
	}

	// Remaining stationary inside for several more ticks: enter must not refire.
	for i := 0; i < 5; i++ {
		s.tickAbilityZonesLocked(0.1)
	}
	if walker.HP != 90 {
		t.Fatalf("walker.HP = %d after 5 more ticks stationary inside, want 90 (enter fired only once)", walker.HP)
	}
}

// TestAbilityZoneOccupancy_ExitFiresOnceOnLeave mirrors the enter test for
// on_zone_exit: fires exactly once the tick a unit leaves, never again while
// it stays gone.
func TestAbilityZoneOccupancy_ExitFiresOnceOnLeave(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", -1000, 0) // parked well outside every zone below so it never contaminates occupancy counts
	walker := teamCombatUnit(t, s, "p2", 0, 0)     // starts inside the zone
	walker.HP, walker.MaxHP = 100, 100

	spawnTestZone(s, caster, protocol.Vec2{X: 0, Y: 0}, 50, 10, 0, []AbilityTriggerDef{
		currentEventDamageTrigger("exit", TriggerOnZoneExit, 7),
	})

	s.tickAbilityZonesLocked(0.1) // enters this tick; no on_zone_enter trigger registered, so no fire
	if walker.HP != 100 {
		t.Fatalf("walker.HP = %d after initial entry, want 100 (no exit trigger should fire yet)", walker.HP)
	}

	walker.X = 200 // leave the zone
	s.tickAbilityZonesLocked(0.1)
	if walker.HP != 93 {
		t.Fatalf("walker.HP = %d after leaving, want 93 (one exit fire of 7 dmg)", walker.HP)
	}

	for i := 0; i < 5; i++ {
		s.tickAbilityZonesLocked(0.1)
	}
	if walker.HP != 93 {
		t.Fatalf("walker.HP = %d after 5 more ticks outside, want 93 (exit fired only once)", walker.HP)
	}
}

// TestAbilityZoneOccupancy_StationaryInsideFiresNeitherAfterInitialEnter
// covers a unit that spawns inside a zone and never moves: it fires enter
// exactly once and never fires exit as long as it never leaves.
func TestAbilityZoneOccupancy_StationaryInsideFiresNeitherAfterInitialEnter(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", -1000, 0) // parked well outside every zone below so it never contaminates occupancy counts
	walker := teamCombatUnit(t, s, "p2", 0, 0)
	walker.HP, walker.MaxHP = 100, 100

	spawnTestZone(s, caster, protocol.Vec2{X: 0, Y: 0}, 50, 10, 0, []AbilityTriggerDef{
		currentEventDamageTrigger("enter", TriggerOnZoneEnter, 10),
		currentEventDamageTrigger("exit", TriggerOnZoneExit, 7),
	})

	for i := 0; i < 6; i++ {
		s.tickAbilityZonesLocked(0.1)
	}
	if walker.HP != 90 {
		t.Fatalf("walker.HP = %d after 6 ticks stationary inside, want 90 (exactly one enter fire, no exit)", walker.HP)
	}
}

// TestAbilityZoneOccupancy_ZeroTickInterval verifies enter/exit fire on a
// zone whose TickInterval is 0 (no on_zone_tick at all) — the
// `if z.TickInterval > 0` guard in tickAbilityZonesLocked must wrap only the
// on_zone_tick loop, not the occupancy diff.
func TestAbilityZoneOccupancy_ZeroTickInterval(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", -1000, 0) // parked well outside every zone below so it never contaminates occupancy counts
	walker := teamCombatUnit(t, s, "p2", 200, 0)
	walker.HP, walker.MaxHP = 100, 100

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	spawnTestZone(s, caster, protocol.Vec2{X: 0, Y: 0}, 50, 10, 0 /* tickInterval */, []AbilityTriggerDef{
		currentEventDamageTrigger("enter", TriggerOnZoneEnter, 10),
		{ID: "tick", Type: TriggerOnZoneTick, Actions: nil}, // must never fire with TickInterval<=0
	})

	walker.X = 0
	s.tickAbilityZonesLocked(0.1)

	if walker.HP != 90 {
		t.Fatalf("walker.HP = %d, want 90 (enter must fire even with tickInterval:0)", walker.HP)
	}
	if traceTriggerFireCount(tr, "tick") != 0 {
		t.Fatalf("on_zone_tick fired %d times with tickInterval:0, want 0", traceTriggerFireCount(tr, "tick"))
	}
}

// TestAbilityZoneOccupancy_BindsEnteringUnit is the binding requirement:
// select_targets{source:"current_event"} inside an on_zone_enter trigger must
// resolve to exactly the unit that entered — not some other unit that also
// happens to be near the zone.
func TestAbilityZoneOccupancy_BindsEnteringUnit(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", -1000, 0) // parked well outside every zone below so it never contaminates occupancy counts
	walker := teamCombatUnit(t, s, "p2", 200, 0)
	walker.HP, walker.MaxHP = 100, 100
	bystander := teamCombatUnit(t, s, "p2", 0, 0) // already inside, never leaves
	bystander.HP, bystander.MaxHP = 100, 100

	spawnTestZone(s, caster, protocol.Vec2{X: 0, Y: 0}, 50, 10, 0, []AbilityTriggerDef{
		currentEventDamageTrigger("enter", TriggerOnZoneEnter, 10),
	})

	// Tick 1: bystander's initial presence is its "enter" too (it started
	// inside) — walker is still outside. Only bystander should take damage.
	s.tickAbilityZonesLocked(0.1)
	if bystander.HP != 90 {
		t.Fatalf("bystander.HP = %d after its own entry, want 90", bystander.HP)
	}
	if walker.HP != 100 {
		t.Fatalf("walker.HP = %d while still outside, want 100", walker.HP)
	}

	// Tick 2: walker crosses in. Only walker should take damage this time;
	// bystander (already an occupant, unmoved) must not be re-hit.
	walker.X = 0
	s.tickAbilityZonesLocked(0.1)
	if walker.HP != 90 {
		t.Fatalf("walker.HP = %d after crossing in, want 90 (bound to the entering unit)", walker.HP)
	}
	if bystander.HP != 90 {
		t.Fatalf("bystander.HP = %d after walker's entry, want 90 unchanged (must not be re-hit)", bystander.HP)
	}
}

// TestAbilityZoneOccupancy_DeterministicOrderAcrossRuns exercises several
// units entering on the same tick and asserts (a) they fire in ascending
// unit-ID order and (b) running the identical scenario twice produces an
// identical firing order — the property that rules out any
// map-iteration-order leak into which order enter fires in.
func TestAbilityZoneOccupancy_DeterministicOrderAcrossRuns(t *testing.T) {
	run := func(t *testing.T) []int {
		s := setupHostileTargetingPair(t)
		defer s.mu.Unlock()

		caster := teamCombatUnit(t, s, "p1", -1000, 0) // parked well outside every zone below so it never contaminates occupancy counts
		// Spawn several units outside the zone, all owned by p2 so relation
		// never filters any of them out.
		var walkers []*Unit
		for i := 0; i < 6; i++ {
			w := teamCombatUnit(t, s, "p2", 500+float64(i), 0)
			w.HP, w.MaxHP = 1000, 1000
			walkers = append(walkers, w)
		}

		tr := &AbilityExecutionTrace{}
		s.previewTrace = tr

		spawnTestZone(s, caster, protocol.Vec2{X: 0, Y: 0}, 50, 10, 0, []AbilityTriggerDef{
			currentEventDamageTrigger("enter", TriggerOnZoneEnter, 1),
		})
		s.tickAbilityZonesLocked(0.1) // all still outside, nothing fires yet

		// Move every walker inside on the SAME tick, in descending creation
		// order, so insertion order alone can't be mistaken for the sort.
		for i := len(walkers) - 1; i >= 0; i-- {
			walkers[i].X = 0
		}
		s.tickAbilityZonesLocked(0.1)

		var order []int
		for _, e := range tr.Events {
			if e.Type != "damage_applied" {
				continue
			}
			if id, ok := e.Payload["unit"].(int); ok {
				order = append(order, id)
			}
		}
		return order
	}

	orderA := run(t)
	orderB := run(t)

	if len(orderA) != 6 {
		t.Fatalf("got %d enter fires, want 6", len(orderA))
	}
	for i := 1; i < len(orderA); i++ {
		if orderA[i-1] >= orderA[i] {
			t.Fatalf("enter fire order %v is not strictly ascending by unit ID", orderA)
		}
	}
	if len(orderA) != len(orderB) {
		t.Fatalf("run A order %v and run B order %v have different lengths", orderA, orderB)
	}
	for i := range orderA {
		if orderA[i] != orderB[i] {
			t.Fatalf("non-deterministic firing order: run A = %v, run B = %v", orderA, orderB)
		}
	}
}

// TestAbilityZoneOccupancy_ExpiryFiresFinalExit locks in the "enter fired,
// exit never did" trap: a zone whose Remaining expires with a unit still
// inside fires on_zone_exit for that unit before the zone is dropped.
func TestAbilityZoneOccupancy_ExpiryFiresFinalExit(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", -1000, 0) // parked well outside every zone below so it never contaminates occupancy counts
	walker := teamCombatUnit(t, s, "p2", 0, 0)     // starts inside, never leaves
	walker.HP, walker.MaxHP = 100, 100

	spawnTestZone(s, caster, protocol.Vec2{X: 0, Y: 0}, 50, 0.2 /* duration */, 0, []AbilityTriggerDef{
		currentEventDamageTrigger("enter", TriggerOnZoneEnter, 10),
		currentEventDamageTrigger("exit", TriggerOnZoneExit, 7),
	})

	s.tickAbilityZonesLocked(0.1) // enter fires; zone still alive (Remaining 0.1 > epsilon)
	if walker.HP != 90 {
		t.Fatalf("walker.HP = %d after entry, want 90", walker.HP)
	}
	if len(s.AbilityZones) != 1 {
		t.Fatalf("zone should still be alive after first tick: %d zones", len(s.AbilityZones))
	}

	s.tickAbilityZonesLocked(0.1) // Remaining hits ~0: expires, firing final exit
	if walker.HP != 83 {
		t.Fatalf("walker.HP = %d after expiry, want 83 (90 - 7 final exit fire)", walker.HP)
	}
	if len(s.AbilityZones) != 0 {
		t.Fatalf("zone should have expired: %d zones remain", len(s.AbilityZones))
	}
}

// TestAbilityZoneOccupancy_UnitDeathInsideFiresExit: a unit that dies while
// inside the zone (HP<=0, still present in s.Units — drainPendingDeathsLocked
// runs later in Update than tickAbilityZonesLocked) drops out of the
// alive+visible occupant set exactly like a unit that walked out, so exit
// fires the same tick it dies.
func TestAbilityZoneOccupancy_UnitDeathInsideFiresExit(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", -1000, 0) // parked well outside every zone below so it never contaminates occupancy counts
	walker := teamCombatUnit(t, s, "p1", 0, 0)     // ally of caster: sidesteps the
	walker.HP, walker.MaxHP = 100, 100             // hostile+invisible splash-parity filter below

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	spawnTestZone(s, caster, protocol.Vec2{X: 0, Y: 0}, 50, 10, 0, []AbilityTriggerDef{
		{ID: "exit", Type: TriggerOnZoneExit, Actions: nil},
	})

	s.tickAbilityZonesLocked(0.1) // initial entry; no on_zone_enter trigger, so nothing to assert yet
	if traceTriggerFireCount(tr, "exit") != 0 {
		t.Fatalf("exit fired before death, want 0 fires")
	}

	walker.HP = 0 // dies inside the zone
	s.tickAbilityZonesLocked(0.1)
	if traceTriggerFireCount(tr, "exit") != 1 {
		t.Fatalf("exit fired %d times the tick the unit died, want exactly 1", traceTriggerFireCount(tr, "exit"))
	}

	// Must not refire on a later tick just because the corpse is still
	// (conceptually) at the same position.
	s.tickAbilityZonesLocked(0.1)
	if traceTriggerFireCount(tr, "exit") != 1 {
		t.Fatalf("exit fired again on a later tick after death: %d total fires, want 1", traceTriggerFireCount(tr, "exit"))
	}
}

// TestAbilityZoneOccupancy_UnitInvisibleInsideFiresExit: a unit that goes
// invisible while inside (stealth/FOW) is treated the same as one that left —
// occupancy is alive+visible, so exit fires the tick visibility drops, and
// re-entry (visibility restored) fires a fresh enter.
func TestAbilityZoneOccupancy_UnitInvisibleInsideFiresExit(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", -1000, 0) // parked well outside every zone below so it never contaminates occupancy counts
	walker := teamCombatUnit(t, s, "p1", 0, 0)     // ally: sidesteps the hostile-visibility filter
	walker.HP, walker.MaxHP = 100, 100

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	spawnTestZone(s, caster, protocol.Vec2{X: 0, Y: 0}, 50, 10, 0, []AbilityTriggerDef{
		{ID: "enter", Type: TriggerOnZoneEnter, Actions: nil},
		{ID: "exit", Type: TriggerOnZoneExit, Actions: nil},
	})

	s.tickAbilityZonesLocked(0.1) // initial entry fires "enter" once
	if traceTriggerFireCount(tr, "enter") != 1 {
		t.Fatalf("enter fired %d times on initial presence, want 1", traceTriggerFireCount(tr, "enter"))
	}

	walker.Visible = false
	s.tickAbilityZonesLocked(0.1)
	if traceTriggerFireCount(tr, "exit") != 1 {
		t.Fatalf("exit fired %d times the tick visibility dropped, want exactly 1", traceTriggerFireCount(tr, "exit"))
	}

	walker.Visible = true // re-appears, still spatially inside
	s.tickAbilityZonesLocked(0.1)
	if traceTriggerFireCount(tr, "enter") != 2 {
		t.Fatalf("enter fired %d times total after re-appearing, want 2 (fresh enter on re-entry)", traceTriggerFireCount(tr, "enter"))
	}
}

// TestAbilityZoneOccupancy_ReentrantCreateZoneDoesNotCorruptAbilityZones is
// the re-entrancy check: an on_zone_enter trigger whose action creates a new
// zone (create_zone) must not corrupt or lose either zone even though
// tickAbilityZonesLocked is still mid-iteration over s.AbilityZones when the
// nested create_zone action runs.
func TestAbilityZoneOccupancy_ReentrantCreateZoneDoesNotCorruptAbilityZones(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", -1000, 0) // parked well outside every zone below so it never contaminates occupancy counts
	walker := teamCombatUnit(t, s, "p2", 200, 0)

	outer := spawnTestZone(s, caster, protocol.Vec2{X: 0, Y: 0}, 50, 10, 0, []AbilityTriggerDef{
		{
			ID:   "enter",
			Type: TriggerOnZoneEnter,
			Actions: []AbilityActionDef{
				{ID: "spawn_child", Type: ActionCreateZone, Config: json.RawMessage(`{"radius":5,"duration":5,"tickInterval":1}`)},
			},
		},
	})

	if len(s.AbilityZones) != 1 {
		t.Fatalf("setup: expected 1 zone before entry, got %d", len(s.AbilityZones))
	}

	walker.X = 0 // crosses into outer, firing the nested create_zone
	s.tickAbilityZonesLocked(0.1)

	if len(s.AbilityZones) != 2 {
		t.Fatalf("s.AbilityZones = %d zones after nested create_zone fired, want 2 (outer survives, child spawned)", len(s.AbilityZones))
	}
	foundOuter, foundChild := false, false
	for _, z := range s.AbilityZones {
		switch z.ID {
		case outer.ID:
			foundOuter = true
		default:
			foundChild = true
			if z.Radius != 5 {
				t.Errorf("child zone radius = %v, want 5 (config was not corrupted)", z.Radius)
			}
		}
	}
	if !foundOuter {
		t.Fatal("outer zone lost from s.AbilityZones after nested create_zone")
	}
	if !foundChild {
		t.Fatal("child zone never made it into s.AbilityZones")
	}

	// A further tick must not panic or corrupt state, and both zones keep
	// ticking down independently.
	s.tickAbilityZonesLocked(0.1)
	if len(s.AbilityZones) != 2 {
		t.Fatalf("s.AbilityZones = %d zones after a further tick, want 2", len(s.AbilityZones))
	}
}

// collectAllTriggerTypesForProductionGuard walks every trigger reachable from
// prog, INCLUDING descending into create_zone's AND apply_status's
// Config-embedded nested triggers (unlike collectProgramActionTypes/
// programIsExecutorRunnable in ability_compile_catalog_test.go, which
// deliberately do not — see their doc comments). on_zone_enter/on_zone_exit
// can only ever be authored inside a create_zone's nested "triggers", and
// on_status_tick/on_status_expire only inside an apply_status's, so a
// production-safety guard must look in both even though the general-purpose
// structural helpers don't.
func collectAllTriggerTypesForProductionGuard(prog *AbilityProgram) []TriggerType {
	var out []TriggerType
	var walkTrigger func(trig AbilityTriggerDef)
	var walkAction func(a AbilityActionDef)

	walkAction = func(a AbilityActionDef) {
		for _, child := range a.Children {
			walkTrigger(child)
		}
		switch a.Type {
		case ActionCreateZone:
			d, ok := lookupActionDescriptor(ActionCreateZone)
			if !ok {
				return
			}
			cfg, err := d.Decode(a.Config)
			if err != nil {
				return
			}
			zc, ok := cfg.(createZoneConfig)
			if !ok {
				return
			}
			for _, child := range zc.Triggers {
				walkTrigger(child)
			}
		case ActionApplyStatus:
			d, ok := lookupActionDescriptor(ActionApplyStatus)
			if !ok {
				return
			}
			cfg, err := d.Decode(a.Config)
			if err != nil {
				return
			}
			ac, ok := cfg.(applyStatusConfig)
			if !ok {
				return
			}
			for _, child := range ac.Triggers {
				walkTrigger(child)
			}
		}
	}
	walkTrigger = func(trig AbilityTriggerDef) {
		out = append(out, trig.Type)
		for _, a := range trig.Actions {
			walkAction(a)
		}
	}

	for _, trig := range prog.Triggers {
		walkTrigger(trig)
	}
	for _, pres := range prog.Presentations {
		for _, trig := range pres.Triggers {
			walkTrigger(trig)
		}
	}
	for _, trig := range prog.NamedTriggers {
		walkTrigger(trig)
	}
	return out
}

// TestCatalog_NoAbilityUsesZoneEnterExitTriggers is the production-unchanged
// guard: on_zone_enter/on_zone_exit are authored-only (reachable solely
// through the ability editor) — no catalog ability's compiled program may
// use either, today or as a regression later.
func TestCatalog_NoAbilityUsesZoneEnterExitTriggers(t *testing.T) {
	for _, def := range ListAbilityDefs() {
		def := def
		t.Run(def.ID, func(t *testing.T) {
			prog := catalogProgram(def)
			for _, tt := range collectAllTriggerTypesForProductionGuard(prog) {
				if tt == TriggerOnZoneEnter || tt == TriggerOnZoneExit {
					t.Fatalf("ability %q compiles a %s trigger; on_zone_enter/on_zone_exit must stay editor-only (never compiler-emitted)", def.ID, tt)
				}
			}
		})
	}
}
