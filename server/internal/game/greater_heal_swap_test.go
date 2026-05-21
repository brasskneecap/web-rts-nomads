package game

// Section 5.4 + post-playtest EffectiveCooldown tests.
//
// Covers:
//   - 5.4: greater_heal perk swap via applyPerkGrantedHooksLocked (both natural
//     grant and DebugSpawnUnit path)
//   - EffectiveCooldown() method: heal (castTime > cooldown) and greater_heal
//     (cooldown > castTime) verify the max(Cooldown, CastTime) clamp.

import "testing"

// ─────────────────────────────────────────────────────────────────────────────
// 5.4 — Perk swap: natural grant and DebugSpawnUnit path
// ─────────────────────────────────────────────────────────────────────────────

// TestGreaterHeal_PerkSwapsAbility_NaturalAndDebug is a table-driven test that
// verifies the greater_heal perk swap fires correctly for both grant paths:
//
//  1. Natural grant via applyPerkGrantedHooksLocked (called from
//     assignUnitPerkLocked and from DebugSpawnUnit).
//  2. Direct grantPerk (low-level, no hooks) followed by manual hook call —
//     documents the raw state for comparison.
//
// In both cases the expected post-state is:
//   - Abilities contains "greater_heal" at the same slot index as "heal" was.
//   - AutoCastEnabled["greater_heal"] == true (migrated from "heal").
//   - AutoCastEnabled["heal"] key absent.
//   - AbilityCooldowns["greater_heal"] == 1.5 (migrated from "heal").
//   - AbilityCooldowns["heal"] key absent.
func TestGreaterHeal_PerkSwapsAbility_NaturalAndDebug(t *testing.T) {
	type testCase struct {
		name  string
		grant func(s *GameState, unit *Unit)
	}

	cases := []testCase{
		{
			name: "applyPerkGrantedHooksLocked",
			grant: func(s *GameState, unit *Unit) {
				unit.PerkIDs = append(unit.PerkIDs, "greater_heal")
				s.applyPerkGrantedHooksLocked(unit, "greater_heal")
			},
		},
		{
			name: "grantPerk+manualHookCall",
			grant: func(s *GameState, unit *Unit) {
				grantPerk(unit, "greater_heal")
				s.applyGreaterHealPerkSwapLocked(unit)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, app, _ := healSetup(t)
			s.mu.Lock()
			defer s.mu.Unlock()

			// Precondition: unit has "heal" in slot 0, autocast enabled, cooldown set.
			if len(app.Abilities) == 0 || app.Abilities[0] != "heal" {
				t.Skipf("%s: apprentice Abilities[0] != \"heal\"; got %v — prerequisite not met", tc.name, app.Abilities)
			}
			if app.AutoCastEnabled == nil {
				app.AutoCastEnabled = make(map[string]bool)
			}
			app.AutoCastEnabled["heal"] = true
			if app.AbilityCooldowns == nil {
				app.AbilityCooldowns = make(map[string]float64)
			}
			app.AbilityCooldowns["heal"] = 1.5

			tc.grant(s, app)

			// Assert ability swap.
			if len(app.Abilities) == 0 || app.Abilities[0] != "greater_heal" {
				t.Errorf("Abilities[0] = %q, want \"greater_heal\"", func() string {
					if len(app.Abilities) == 0 {
						return "<empty>"
					}
					return app.Abilities[0]
				}())
			}

			// Assert autocast migrated.
			if !app.AutoCastEnabled["greater_heal"] {
				t.Error("AutoCastEnabled[\"greater_heal\"] should be true after swap")
			}
			if _, stillHasHeal := app.AutoCastEnabled["heal"]; stillHasHeal {
				t.Error("AutoCastEnabled[\"heal\"] should be absent after swap")
			}

			// Assert cooldown migrated.
			if app.AbilityCooldowns["greater_heal"] != 1.5 {
				t.Errorf("AbilityCooldowns[\"greater_heal\"] = %.2f, want 1.5", app.AbilityCooldowns["greater_heal"])
			}
			if _, stillHasHeal := app.AbilityCooldowns["heal"]; stillHasHeal {
				t.Error("AbilityCooldowns[\"heal\"] should be absent after swap")
			}
		})
	}
}

// TestGreaterHeal_SwapNoOpWhenHealAbsent confirms the swap is a safe no-op when
// "heal" is not in unit.Abilities. The Abilities slice must be unchanged.
func TestGreaterHeal_SwapNoOpWhenHealAbsent(t *testing.T) {
	s, app, _ := healSetup(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove "heal" from the abilities list so the precondition is absent.
	app.Abilities = []string{"some_other_ability"}

	before := make([]string, len(app.Abilities))
	copy(before, app.Abilities)

	s.applyGreaterHealPerkSwapLocked(app)

	if len(app.Abilities) != len(before) || app.Abilities[0] != before[0] {
		t.Errorf("Abilities changed after no-op swap: got %v, want %v", app.Abilities, before)
	}
}

// TestGreaterHeal_SlotIndexPreserved verifies that when "heal" is NOT at index 0
// (e.g. Abilities = ["arcane_bolt", "heal"]) the swap preserves the slot.
func TestGreaterHeal_SlotIndexPreserved(t *testing.T) {
	s, app, _ := healSetup(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	app.Abilities = []string{"arcane_bolt", "heal"}
	grantPerk(app, "greater_heal")
	s.applyGreaterHealPerkSwapLocked(app)

	if len(app.Abilities) != 2 {
		t.Fatalf("Abilities length = %d, want 2", len(app.Abilities))
	}
	if app.Abilities[0] != "arcane_bolt" {
		t.Errorf("Abilities[0] = %q, want \"arcane_bolt\" (slot 0 unchanged)", app.Abilities[0])
	}
	if app.Abilities[1] != "greater_heal" {
		t.Errorf("Abilities[1] = %q, want \"greater_heal\" (slot 1 swapped)", app.Abilities[1])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// EffectiveCooldown — post-playtest spec
// ─────────────────────────────────────────────────────────────────────────────

// TestEffectiveCooldown_HealClampsToCastTime confirms that for the base Heal
// ability (castTime > cooldown) EffectiveCooldown() returns the cast time.
// Values are derived from the catalog, not hardcoded.
func TestEffectiveCooldown_HealClampsToCastTime(t *testing.T) {
	def, ok := getAbilityDef("heal")
	if !ok {
		t.Fatal(`getAbilityDef("heal") = _, false`)
	}
	// Spec: EffectiveCooldown = max(Cooldown, CastTime).
	want := def.CastTime
	if def.Cooldown > def.CastTime {
		want = def.Cooldown
	}
	got := def.EffectiveCooldown()
	if got != want {
		t.Errorf("heal.EffectiveCooldown() = %.3f, want %.3f (max(castTime=%.3f, cooldown=%.3f))",
			got, want, def.CastTime, def.Cooldown)
	}
	// Invariant: must be >= cast time (the unit is committed for at least this long).
	if got < def.CastTime {
		t.Errorf("heal.EffectiveCooldown() %.3f < castTime %.3f; must be >= castTime", got, def.CastTime)
	}
}

// TestEffectiveCooldown_GreaterHealUsesConfiguredCooldown confirms that for
// greater_heal (cooldown > castTime) EffectiveCooldown() returns the cooldown.
func TestEffectiveCooldown_GreaterHealUsesConfiguredCooldown(t *testing.T) {
	def, ok := getAbilityDef("greater_heal")
	if !ok {
		t.Fatal(`getAbilityDef("greater_heal") = _, false`)
	}
	want := def.CastTime
	if def.Cooldown > def.CastTime {
		want = def.Cooldown
	}
	got := def.EffectiveCooldown()
	if got != want {
		t.Errorf("greater_heal.EffectiveCooldown() = %.3f, want %.3f (max(castTime=%.3f, cooldown=%.3f))",
			got, want, def.CastTime, def.Cooldown)
	}
	// For greater_heal the cooldown must be strictly greater than the cast time
	// per spec (cooldown drives the wipe, not cast time).
	if def.Cooldown <= def.CastTime {
		t.Errorf("greater_heal catalog: cooldown %.3f should be > castTime %.3f", def.Cooldown, def.CastTime)
	}
}

// TestEffectiveCooldown_ArmedInBeginAbilityCast verifies that beginAbilityCastLocked
// arms the cooldown to EffectiveCooldown() and NOT to Cooldown directly.
// This covers both manual and autocast paths (they share beginAbilityCastLocked).
func TestEffectiveCooldown_ArmedInBeginAbilityCast(t *testing.T) {
	s, app, ally := healSetup(t)
	def := healDef(t)

	s.mu.Lock()
	ok, reason := s.beginAbilityCastLocked(app, "heal", ally)
	s.mu.Unlock()
	if !ok {
		t.Fatalf("beginAbilityCastLocked failed: %q", reason)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	gotCD := app.AbilityCooldowns["heal"]
	wantCD := def.EffectiveCooldown()
	if gotCD != wantCD {
		t.Errorf("AbilityCooldowns[\"heal\"] = %.3f, want %.3f (EffectiveCooldown)", gotCD, wantCD)
	}
}
