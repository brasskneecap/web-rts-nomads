package game

import "testing"

// TestPlaceTrapAction exercises the place_trap composable action (Phase 1 of
// the "Trapper traps -> abilities" migration) with a SYNTHETIC ability
// program — no trap ability exists in the catalog yet (that's Phase 2). This
// proves the action correctly builds a TrapConfig from its decoded config and
// plants it via the existing plantTrapLocked seam, appending a *Trap to
// s.Traps with the configured stats.
func TestPlaceTrapAction(t *testing.T) {
	t.Run("plants a trap with the configured stats", func(t *testing.T) {
		s := setupHostileTargetingPair(t)
		defer s.mu.Unlock()

		caster := teamCombatUnit(t, s, "p1", 0, 0)
		before := len(s.Traps)

		tr := runOneActionProgram(t, s, caster.ID, 0, ActionPlaceTrap,
			`{"trapType":"explosive_trap","explosionRadius":100,"triggerRadius":50,"burstDamage":75,"durationSeconds":20}`,
			nil)

		after := len(s.Traps)
		if after-before != 1 {
			t.Fatalf("len(s.Traps) delta = %d; want 1", after-before)
		}
		trap := s.Traps[len(s.Traps)-1]
		if trap.TrapType != "explosive_trap" {
			t.Errorf("trap.TrapType = %q; want %q", trap.TrapType, "explosive_trap")
		}
		if trap.Radius != 100 {
			t.Errorf("trap.Radius = %v; want 100 (from explosionRadius)", trap.Radius)
		}
		if trap.TriggerRadius != 50 {
			t.Errorf("trap.TriggerRadius = %v; want 50", trap.TriggerRadius)
		}
		if trap.BurstDamage != 75 {
			t.Errorf("trap.BurstDamage = %v; want 75", trap.BurstDamage)
		}
		if trap.OwnerUnitID != caster.ID {
			t.Errorf("trap.OwnerUnitID = %v; want caster.ID %v", trap.OwnerUnitID, caster.ID)
		}
		if !traceHas(tr, "trap_placed") {
			t.Fatalf("missing trap_placed trace event: %+v", tr.Events)
		}
	})

	t.Run("configByRank overrides the base field at the caster's rank", func(t *testing.T) {
		s := setupHostileTargetingPair(t)
		defer s.mu.Unlock()

		caster := teamCombatUnit(t, s, "p1", 0, 0)
		caster.Rank = "silver"
		before := len(s.Traps)

		runOneActionProgram(t, s, caster.ID, 0, ActionPlaceTrap,
			`{"trapType":"fire_pit","radius":80,"damagePerSecond":10,"durationSeconds":15,
			  "configByRank":{"silver":{"damagePerSecond":28}}}`,
			nil)

		after := len(s.Traps)
		if after-before != 1 {
			t.Fatalf("len(s.Traps) delta = %d; want 1", after-before)
		}
		trap := s.Traps[len(s.Traps)-1]
		if trap.TrapType != "fire_pit" {
			t.Errorf("trap.TrapType = %q; want %q", trap.TrapType, "fire_pit")
		}
		if trap.DamagePerSecond != 28 {
			t.Errorf("trap.DamagePerSecond = %v; want 28 (silver rank override, not the bronze base of 10)", trap.DamagePerSecond)
		}
		if trap.Radius != 80 {
			t.Errorf("trap.Radius = %v; want 80 (unaffected base field, no override for it)", trap.Radius)
		}
	})

	t.Run("nil caster is a no-op (no trap planted, no panic)", func(t *testing.T) {
		s := setupHostileTargetingPair(t)
		defer s.mu.Unlock()

		before := len(s.Traps)
		// casterID 999999 does not resolve to any unit.
		runOneActionProgram(t, s, 999999, 0, ActionPlaceTrap,
			`{"trapType":"caltrops","radius":50,"durationSeconds":10}`, nil)

		if len(s.Traps) != before {
			t.Fatalf("len(s.Traps) = %d; want unchanged %d when caster is nil", len(s.Traps), before)
		}
	})
}

func TestPlaceTrapConfig_ToTrapConfig(t *testing.T) {
	c := placeTrapConfig{
		TrapType:        "fire_pit",
		DurationSeconds: 15,
		Radius:          80,
		DamagePerSecond: 10,
		ConfigByRank: map[string]map[string]float64{
			"silver": {"damagePerSecond": 28},
			"gold":   {"damagePerSecond": 40, "radius": 120},
		},
	}

	if got := c.toTrapConfig(""); got.DamagePerSecond != 10 {
		t.Errorf("bronze (no override): DamagePerSecond = %v; want 10", got.DamagePerSecond)
	}
	if got := c.toTrapConfig("silver"); got.DamagePerSecond != 28 || got.Radius != 80 {
		t.Errorf("silver override: got DamagePerSecond=%v Radius=%v; want 28, 80", got.DamagePerSecond, got.Radius)
	}
	if got := c.toTrapConfig("gold"); got.DamagePerSecond != 40 || got.Radius != 120 {
		t.Errorf("gold override: got DamagePerSecond=%v Radius=%v; want 40, 120", got.DamagePerSecond, got.Radius)
	}
}
