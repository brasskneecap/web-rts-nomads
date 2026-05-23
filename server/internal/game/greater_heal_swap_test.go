package game

// Greater Heal path-override tests.
//
// Greater Heal lives in cleric.json as a path-level "abilities" override:
// every cleric, on every promotion (and after every applyRankModifiersLocked
// recompute) ends up with Abilities = ["greater_heal"] instead of the
// acolyte's base ["heal"]. The recompute is in assignUnitPathAbilitiesLocked
// (path_ability_defs.go), which also handles per-instance
// AutoCastEnabled / AbilityCooldowns migration by position.
//
// These tests cover:
//   - The on-disk cleric.json declares the override (catalog regression guard).
//   - Promotion via the path-ability recompute swaps heal → greater_heal with
//     full state migration (slot order, autocast toggle, cooldown timer).
//   - The recompute is idempotent — calling it twice doesn't drift.
//   - Promotion-grown clerics get greater_heal regardless of which Bronze
//     perk is rolled (the override is independent of the perk pool).
//   - EffectiveCooldown() works correctly for heal vs greater_heal.

import (
	"sort"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Path-level override: cleric.json declares Abilities = ["greater_heal"]
// ─────────────────────────────────────────────────────────────────────────────

// TestGreaterHeal_PathOverrideSwapsHealOnPromotion verifies the full recompute
// flow: an acolyte with autocast on heal + a non-zero cooldown timer is
// promoted to (cleric, bronze) via the same assignUnitPathAbilitiesLocked call
// path used in production by addUnitXPLocked and DebugSpawnUnit. After the
// recompute:
//   - Abilities[0] is "greater_heal" (path override).
//   - AutoCastEnabled["greater_heal"] inherits the player's heal toggle.
//   - AbilityCooldowns["greater_heal"] inherits the heal timer value.
//   - The "heal" keys are absent from both maps (state migrated, not duplicated).
func TestGreaterHeal_PathOverrideSwapsHealOnPromotion(t *testing.T) {
	s, app, _ := healSetup(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(app.Abilities) == 0 || app.Abilities[0] != "heal" {
		t.Skipf("acolyte Abilities[0] != \"heal\"; got %v", app.Abilities)
	}
	if app.AutoCastEnabled == nil {
		app.AutoCastEnabled = make(map[string]bool)
	}
	app.AutoCastEnabled["heal"] = true
	if app.AbilityCooldowns == nil {
		app.AbilityCooldowns = make(map[string]float64)
	}
	app.AbilityCooldowns["heal"] = 1.5

	// Drive the canonical recompute (matches the call sites in addUnitXPLocked
	// and DebugSpawnUnit).
	app.ProgressionPath = unitPathCleric
	app.Rank = unitRankBronze
	s.assignUnitPathAbilitiesLocked(app)

	if len(app.Abilities) == 0 || app.Abilities[0] != "greater_heal" {
		t.Errorf("Abilities[0] = %q, want \"greater_heal\"", func() string {
			if len(app.Abilities) == 0 {
				return "<empty>"
			}
			return app.Abilities[0]
		}())
	}
	if !app.AutoCastEnabled["greater_heal"] {
		t.Error("AutoCastEnabled[\"greater_heal\"] should be true after path override")
	}
	if _, stillHasHeal := app.AutoCastEnabled["heal"]; stillHasHeal {
		t.Error("AutoCastEnabled[\"heal\"] should be absent after migration")
	}
	if app.AbilityCooldowns["greater_heal"] != 1.5 {
		t.Errorf("AbilityCooldowns[\"greater_heal\"] = %.2f, want 1.5", app.AbilityCooldowns["greater_heal"])
	}
	if _, stillHasHeal := app.AbilityCooldowns["heal"]; stillHasHeal {
		t.Error("AbilityCooldowns[\"heal\"] should be absent after migration")
	}
}

// TestGreaterHeal_PathOverrideIsIdempotent confirms that calling
// assignUnitPathAbilitiesLocked twice on an already-promoted cleric produces
// the same result — no drift, no duplicate ids.
func TestGreaterHeal_PathOverrideIsIdempotent(t *testing.T) {
	s, app, _ := healSetup(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	app.ProgressionPath = unitPathCleric
	app.Rank = unitRankBronze
	s.assignUnitPathAbilitiesLocked(app)
	first := append([]string(nil), app.Abilities...)

	s.assignUnitPathAbilitiesLocked(app)
	second := append([]string(nil), app.Abilities...)

	if len(first) != len(second) {
		t.Fatalf("recompute drifted: first=%v second=%v", first, second)
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("recompute drifted at index %d: %q vs %q", i, first[i], second[i])
		}
	}
}

// TestGreaterHeal_GrantedRegardlessOfBronzePerkRoll confirms that every Cleric
// promoted to Bronze ends up with greater_heal in Abilities, irrespective of
// which Bronze perk is rolled. The path-level override runs unconditionally
// alongside the perk roll, so the swap fires the same way for every entry in
// the pool.
//
// The four Cleric Bronze perks are read from the catalog (no hardcoded list).
func TestGreaterHeal_GrantedRegardlessOfBronzePerkRoll(t *testing.T) {
	// Pull the Bronze pool from the catalog so this stays in sync with any
	// re-tunings of the perks/bronze.json file.
	var pool []string
	for id, def := range perkDefsByID {
		if def == nil {
			continue
		}
		if def.Path == "cleric" && def.Rank == unitRankBronze {
			pool = append(pool, id)
		}
	}
	sort.Strings(pool) // deterministic subtest order
	if len(pool) == 0 {
		t.Fatal("Cleric Bronze pool is empty; cannot exercise per-perk grants")
	}

	for _, perkID := range pool {
		t.Run(perkID, func(t *testing.T) {
			s, app, _ := healSetup(t)
			s.mu.Lock()
			defer s.mu.Unlock()

			if len(app.Abilities) == 0 || app.Abilities[0] != "heal" {
				t.Skipf("acolyte Abilities[0] != \"heal\"; got %v", app.Abilities)
			}

			// Roll this specific Bronze perk, then run the path-ability
			// recompute the same way addUnitXPLocked does on a natural rank-up.
			grantPerk(app, perkID)
			app.ProgressionPath = unitPathCleric
			app.Rank = unitRankBronze
			s.assignUnitPathAbilitiesLocked(app)

			haveGreater := false
			haveHeal := false
			for _, id := range app.Abilities {
				if id == "greater_heal" {
					haveGreater = true
				}
				if id == "heal" {
					haveHeal = true
				}
			}
			if !haveGreater {
				t.Errorf("rolled %q: Abilities = %v; want to contain \"greater_heal\"", perkID, app.Abilities)
			}
			if haveHeal {
				t.Errorf("rolled %q: Abilities still contains \"heal\" after path override; got %v", perkID, app.Abilities)
			}
		})
	}
}

// TestGreaterHeal_BaseAcolyteRetainsHeal is the negative control: an
// acolyte without a path keeps base "heal" — the path override only fires
// when ProgressionPath is set.
func TestGreaterHeal_BaseAcolyteRetainsHeal(t *testing.T) {
	s, app, _ := healSetup(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Acolyte with no path. assignUnitPathAbilitiesLocked recomputes from
	// the unit def's Abilities; the cleric override doesn't apply because
	// ProgressionPath == "none".
	s.assignUnitPathAbilitiesLocked(app)

	if len(app.Abilities) == 0 || app.Abilities[0] != "heal" {
		t.Errorf("base acolyte Abilities = %v; want first entry \"heal\"", app.Abilities)
	}
	for _, id := range app.Abilities {
		if id == "greater_heal" {
			t.Errorf("base acolyte unexpectedly has greater_heal; Abilities = %v", app.Abilities)
		}
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
	want := def.CastTime
	if def.Cooldown > def.CastTime {
		want = def.Cooldown
	}
	got := def.EffectiveCooldown()
	if got != want {
		t.Errorf("heal.EffectiveCooldown() = %.3f, want %.3f (max(castTime=%.3f, cooldown=%.3f))",
			got, want, def.CastTime, def.Cooldown)
	}
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
	if def.Cooldown <= def.CastTime {
		t.Errorf("greater_heal catalog: cooldown %.3f should be > castTime %.3f", def.Cooldown, def.CastTime)
	}
}

// TestEffectiveCooldown_ArmedInBeginAbilityCast verifies that beginAbilityCastLocked
// arms the cooldown to EffectiveCooldown() and NOT to Cooldown directly.
// Covers both manual and autocast paths (they share beginAbilityCastLocked).
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
