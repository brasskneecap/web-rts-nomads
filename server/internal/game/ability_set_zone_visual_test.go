package game

import (
	"encoding/json"
	"testing"

	"webrts/server/pkg/protocol"
)

// setZoneVisualZone builds an armed zone whose on_zone_enter runs one
// set_zone_visual action with the given config, an enemy already standing
// inside it, and returns the zone pointer so a test can inspect it after ticks.
func setZoneVisualZone(t *testing.T, s *GameState, cfg string) *AbilityZone {
	t.Helper()
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}
	caster := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 300, Y: 300})
	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 300, Y: 300})
	if caster == nil || enemy == nil {
		t.Fatal("unit spawn failed")
	}
	enemy.Visible = true
	enemy.HP, enemy.MaxHP = 500, 500

	z := &AbilityZone{
		AbilityID: "test", CasterID: caster.ID, OwnerPlayerID: "p1",
		Center: protocol.Vec2{X: 300, Y: 300}, Radius: 60, Remaining: 10, TickInterval: 1,
		Sprite: "object:explosive_trap",
		Triggers: []AbilityTriggerDef{{
			Type: TriggerOnZoneEnter,
			Actions: []AbilityActionDef{{
				Type:   ActionSetZoneVisual,
				Config: json.RawMessage(cfg),
			}},
		}},
	}
	s.spawnAbilityZoneLocked(z)
	return z
}

// TestSetZoneVisual_PersistSwapsVisual: with persist=true, on_zone_enter
// permanently swaps the firing zone's visible animation (idle → raised spikes
// that stay), and the zone lives on.
func TestSetZoneVisual_PersistSwapsVisual(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	z := setZoneVisualZone(t, s, `{"animation":"object:caltrops@electrified","persist":true}`)
	if z.Sprite != "object:explosive_trap" {
		t.Fatalf("precondition: sprite = %q", z.Sprite)
	}
	s.tickAbilityZonesLocked(0.1) // enemy inside ⇒ on_zone_enter fires

	if z.Sprite != "object:caltrops@electrified" {
		t.Errorf("persist swap: sprite = %q, want object:caltrops@electrified", z.Sprite)
	}
	if len(s.AbilityZones) != 1 {
		t.Errorf("zone count = %d, want 1 (persist swap must not end the zone)", len(s.AbilityZones))
	}
}

// TestSetZoneVisual_PlayOnceSpawnsTransientDecal: with persist=false and a
// non-effect animation, on_zone_enter plays it ONCE by spawning a transient
// visual-only decal zone at the trap — the trap's own zone keeps its idle
// sprite (not swapped).
func TestSetZoneVisual_PlayOnceSpawnsTransientDecal(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	z := setZoneVisualZone(t, s, `{"animation":"object:fire_pit","persist":false,"duration":2}`)
	s.tickAbilityZonesLocked(0.1)

	// The trap's own visual is unchanged (play-once doesn't swap it).
	if z.Sprite != "object:explosive_trap" {
		t.Errorf("play-once must not swap the zone sprite; got %q", z.Sprite)
	}
	// A transient decal zone was spawned to play the animation once.
	var decal *AbilityZone
	for _, az := range s.AbilityZones {
		if az != z && az.Sprite == "object:fire_pit" {
			decal = az
		}
	}
	if decal == nil {
		t.Fatalf("play-once should spawn a transient decal zone showing object:fire_pit; zones=%d", len(s.AbilityZones))
	}
	if len(decal.Triggers) != 0 {
		t.Errorf("transient decal zone must be gameplay-inert (no triggers), got %d", len(decal.Triggers))
	}
}

// TestSetZoneVisual_NoOpOutsideAZone: safe to author anywhere; no-ops (traces a
// skip) when not running inside a zone.
func TestSetZoneVisual_NoOpOutsideAZone(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	caster := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 300, Y: 300})
	if caster == nil {
		t.Fatal("spawn failed")
	}
	tr := runOneActionProgram(t, s, caster.ID, 0, ActionSetZoneVisual, `{"animation":"effect:explosion"}`, nil)
	if !traceHas(tr, "zone_visual_skipped") {
		t.Errorf("set_zone_visual outside a zone should trace a skip: %+v", tr.Events)
	}
}
