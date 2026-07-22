package game

import (
	"encoding/json"
	"testing"
)

// spawnZoneFromConfig runs a create_zone action with the given raw config for a
// caster, returning the caster. Mirrors the setup in ability_zone_test.go.
func spawnZoneFromConfig(t *testing.T, s *GameState, caster *Unit, raw string) {
	t.Helper()
	ctx := &RuntimeAbilityContext{CasterID: caster.ID, AbilityID: "test_zone", Named: map[string]ContextValue{}}
	desc, ok := lookupActionDescriptor(ActionCreateZone)
	if !ok {
		t.Fatal("create_zone action not registered")
	}
	cfg, err := desc.Decode(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	desc.Execute(s, ctx, cfg, nil)
}

// TestVisibleZone_SerializedWhenSpriteSet covers the opt-in visibility contract
// (docs/design/ability_perk_interaction.md §8): a create_zone that names a
// sprite is serialized as a persistent ground entity the client can render;
// one that does not stays server-only, exactly as every zone behaved before.
func TestVisibleZone_SerializedWhenSpriteSet(t *testing.T) {
	t.Run("no sprite ⇒ invisible (wire unchanged)", func(t *testing.T) {
		s := setupHostileTargetingPair(t)
		defer s.mu.Unlock()

		caster := teamCombatUnit(t, s, "p1", 0, 0)
		spawnZoneFromConfig(t, s, caster,
			`{"name":"Burning Crater","radius":120,"duration":5,"tickInterval":1}`)

		if len(s.AbilityZones) != 1 {
			t.Fatalf("zone not spawned: %d", len(s.AbilityZones))
		}
		if got := s.visibleZoneSnapshotsLocked(); len(got) != 0 {
			t.Fatalf("zone without a sprite must not be serialized, got %d snapshot(s)", len(got))
		}
		if snap := s.snapshotUnfilteredLocked(); len(snap.Traps) != 0 {
			t.Fatalf("snapshot carried %d ground entities for an invisible zone; want 0", len(snap.Traps))
		}
	})

	t.Run("sprite ⇒ serialized with the zone's live geometry", func(t *testing.T) {
		s := setupHostileTargetingPair(t)
		defer s.mu.Unlock()

		caster := teamCombatUnit(t, s, "p1", 40, -20)
		spawnZoneFromConfig(t, s, caster,
			`{"name":"Fire Pit","radius":55,"duration":10,"tickInterval":1,
			  "sprite":"fire_pit","spriteScale":1.5}`)

		zones := s.visibleZoneSnapshotsLocked()
		if len(zones) != 1 {
			t.Fatalf("visible zone snapshots = %d, want 1", len(zones))
		}
		z := zones[0]
		if z.Type != "fire_pit" {
			t.Errorf("Type = %q, want %q (the authored sprite id)", z.Type, "fire_pit")
		}
		if z.Radius != 55 {
			t.Errorf("Radius = %v, want 55", z.Radius)
		}
		if z.RemainingSeconds != 10 {
			t.Errorf("RemainingSeconds = %v, want 10", z.RemainingSeconds)
		}
		if z.ScaleMultiplier != 1.5 {
			t.Errorf("ScaleMultiplier = %v, want 1.5", z.ScaleMultiplier)
		}
		if z.OwnerID != caster.OwnerID {
			t.Errorf("OwnerID = %q, want %q", z.OwnerID, caster.OwnerID)
		}
		// Zone is centered on the caster when no position ref is authored.
		if z.X != caster.X || z.Y != caster.Y {
			t.Errorf("center = (%v,%v), want caster (%v,%v)", z.X, z.Y, caster.X, caster.Y)
		}
		if z.ID == "" {
			t.Error("visible zone snapshot must carry a stable id for client tracking")
		}

		// It must actually reach the wire, not just the helper.
		if snap := s.snapshotUnfilteredLocked(); len(snap.Traps) != 1 {
			t.Fatalf("snapshot ground entities = %d, want 1", len(snap.Traps))
		}
	})

	t.Run("remaining seconds tick down on the wire", func(t *testing.T) {
		s := setupHostileTargetingPair(t)
		defer s.mu.Unlock()

		caster := teamCombatUnit(t, s, "p1", 0, 0)
		spawnZoneFromConfig(t, s, caster,
			`{"name":"Fire Pit","radius":55,"duration":10,"tickInterval":1,"sprite":"fire_pit"}`)

		s.tickAbilityZonesLocked(2)

		zones := s.visibleZoneSnapshotsLocked()
		if len(zones) != 1 {
			t.Fatalf("visible zone snapshots = %d, want 1", len(zones))
		}
		if got := zones[0].RemainingSeconds; got <= 0 || got > 8.0001 {
			t.Errorf("RemainingSeconds = %v, want ~8 after a 2s tick", got)
		}
	})

	t.Run("expired visible zone stops being serialized", func(t *testing.T) {
		s := setupHostileTargetingPair(t)
		defer s.mu.Unlock()

		caster := teamCombatUnit(t, s, "p1", 0, 0)
		spawnZoneFromConfig(t, s, caster,
			`{"name":"Fire Pit","radius":55,"duration":1,"tickInterval":1,"sprite":"fire_pit"}`)

		s.tickAbilityZonesLocked(2) // outlive it

		if len(s.AbilityZones) != 0 {
			t.Fatalf("zone should have expired: %d", len(s.AbilityZones))
		}
		if got := s.visibleZoneSnapshotsLocked(); len(got) != 0 {
			t.Fatalf("expired zone still serialized: %d snapshot(s)", len(got))
		}
	})
}

// TestVisibleZone_DoesNotDisturbTraps guards the shared-array decision: visible
// zones ride the same snapshot array as traps, so a trap and a zone must both
// appear rather than one clobbering the other.
func TestVisibleZone_DoesNotDisturbTraps(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	placeTrap(s, "caltrops", caster.OwnerID, caster.ID, 200, 200, 60, 12)
	spawnZoneFromConfig(t, s, caster,
		`{"name":"Fire Pit","radius":55,"duration":10,"tickInterval":1,"sprite":"fire_pit"}`)

	snap := s.snapshotUnfilteredLocked()
	if len(snap.Traps) != 2 {
		t.Fatalf("ground entities = %d, want 2 (one trap + one visible zone)", len(snap.Traps))
	}
	byType := map[string]bool{}
	for _, g := range snap.Traps {
		byType[g.Type] = true
	}
	if !byType["caltrops"] || !byType["fire_pit"] {
		t.Errorf("want both a caltrops trap and a fire_pit zone, got %v", byType)
	}
}
