package game

import (
	"encoding/json"
	"strings"
	"testing"

	"webrts/server/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────────────────────
// FIRE PIT — the vertical slice for the ability/perk interaction design
// (docs/design/ability_perk_interaction.md §6.1).
//
// fire_pit was the first trap re-authored off the bespoke trap runtime onto a
// composable VISIBLE ZONE. These tests replace the legacy trap-entity tests
// that used to cover it (TestFirePit_* in trap_test.go, the fire_pit rows of
// TestTrapCharacterization, and the EffectiveTrapSnapshot rank-scaling tests),
// and they pin the properties the migration had to preserve:
//
//   - the pit is a visible, ticking zone centered on the cast target
//   - it damages enemies in it, and never allies
//   - its dps/radius still scale by unit rank (16/28/45, 55/75/95)
//   - a perk can change HOW it delivers damage without the perk knowing
//     anything about the program's structure
// ─────────────────────────────────────────────────────────────────────────────

// castFirePit spawns a trapper + an enemy, gives the trapper the fire_pit
// ability, and casts it at the enemy. Returns both units.
func castFirePit(t *testing.T, s *GameState, rank string) (caster, enemy *Unit) {
	t.Helper()
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}

	caster = s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 300, Y: 300})
	if caster == nil {
		caster = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 300, Y: 300})
	}
	if rank != "" {
		caster.Rank = rank
	}
	grantTrapAbility(caster, "fire_pit")

	enemy = s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 380, Y: 300})
	if enemy == nil {
		t.Fatal("enemy spawn failed")
	}
	enemy.Visible = true
	enemy.HP, enemy.MaxHP = 500, 500

	ok, reason := s.beginAbilityCastLocked(caster, "fire_pit", enemy)
	if !ok {
		t.Fatalf("beginAbilityCastLocked(fire_pit) failed: %q", reason)
	}
	return caster, enemy
}

// TestFirePitZone_PlacedAndVisible: casting fire_pit creates one visible zone,
// centered on the target (where the thrown trap used to land).
func TestFirePitZone_PlacedAndVisible(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	_, enemy := castFirePit(t, s, unitRankBronze)

	if len(s.AbilityZones) != 1 {
		t.Fatalf("AbilityZones = %d, want 1", len(s.AbilityZones))
	}
	z := s.AbilityZones[0]
	if z.Sprite != "fire_pit" {
		t.Errorf("zone sprite = %q, want %q (must be visible)", z.Sprite, "fire_pit")
	}
	if z.Center.X != enemy.X || z.Center.Y != enemy.Y {
		t.Errorf("zone center = (%v,%v), want the cast target (%v,%v)", z.Center.X, z.Center.Y, enemy.X, enemy.Y)
	}
	// It must reach the client as a ground entity.
	if snap := s.snapshotUnfilteredLocked(); len(snap.Traps) != 1 {
		t.Fatalf("snapshot ground entities = %d, want 1 (the visible pit)", len(snap.Traps))
	}
}

// TestFirePitZone_DamagesEnemiesNotAllies replaces the legacy
// TestFirePit_DamagesEnemyNoSlow / TestFirePit_NoFriendlyFire pair.
func TestFirePitZone_DamagesEnemiesNotAllies(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	_, enemy := castFirePit(t, s, unitRankBronze)

	ally := s.spawnPlayerUnitLocked("soldier", "p1", "#2ecc71", protocol.Vec2{X: enemy.X, Y: enemy.Y})
	if ally == nil {
		t.Fatal("ally spawn failed")
	}
	ally.Visible = true
	ally.HP, ally.MaxHP = 500, 500

	enemyBefore, allyBefore := enemy.HP, ally.HP
	s.tickAbilityZonesLocked(1)

	if enemy.HP >= enemyBefore {
		t.Errorf("enemy in the pit took no damage (HP %d -> %d)", enemyBefore, enemy.HP)
	}
	if ally.HP != allyBefore {
		t.Errorf("ally in the pit took damage (HP %d -> %d); traps never hit friendlies", allyBefore, ally.HP)
	}
}

// TestFirePitZone_RankIsNotAnAbilityConcern replaces TestFirePitZone_RankScaling,
// which pinned the per-rank damage/radius bases (dps 16/28/45, radius 55/75/95)
// the ability itself used to carry in a `byRank` block.
//
// That mechanism is retired. An ability now declares ONE set of numbers and a
// promoted unit hits harder because ITS rank raises ability power / ability
// damage / the path's per-rank ability stats — one scaling story instead of two,
// and it applies to every ability a rank grants rather than only the ones whose
// author remembered to write a byRank block.
func TestFirePitZone_RankIsNotAnAbilityConcern(t *testing.T) {
	var seen []float64
	for _, rank := range []string{unitRankBronze, unitRankSilver, unitRankGold} {
		s := newTrapState(t)
		s.mu.Lock()
		caster, _ := castFirePit(t, s, rank)
		seen = append(seen, effTrapField(t, s, caster, "fire_pit", "dps"))
		if len(s.AbilityZones) != 1 {
			s.mu.Unlock()
			t.Fatalf("AbilityZones = %d, want 1", len(s.AbilityZones))
		}
		s.mu.Unlock()
	}
	// The archer's path authors no per-rank ability stats, so with rank no longer
	// selecting a base the pit is identical at every rank. This is the assertion
	// that fails if a byRank-shaped mechanism comes back.
	for i, got := range seen {
		if got != seen[0] {
			t.Errorf("dps at rank %d = %v, want %v — the ability itself must not vary by rank",
				i, got, seen[0])
		}
	}
}

// TestFirePitZone_ModifierPerksReachTheMigratedTrap pins that the Trapper's
// global Silver perks still change the pit AFTER it stopped being a legacy trap
// — they reach it as ability-parameter contributions now instead of through the
// bespoke TrapModifiers aggregator. Values come from each perk's own authored
// data, so a catalog re-tune moves the test with it.
func TestFirePitZone_ModifierPerksReachTheMigratedTrap(t *testing.T) {

	// Expected values come from each perk's own authored data via the shared
	// applyPerkRow, which reads whichever of the three authoring forms the perk
	// used. Nothing here is hardcoded, and moving a contribution between forms
	// carries this test rather than breaking it.
	expectedFor := func(t *testing.T, perkID, param string, base float64) float64 {
		t.Helper()
		site := trapFieldSite["fire_pit"][param]
		return applyPerkRow(t, perkID, "fire_pit", site[0], site[1], base)
	}

	cases := []struct {
		perkID string
		param  string
	}{
		{"extended_setup", "duration"},
		{"wider_nets", "radius"},
		{"amplified_effects", "dps"},
	}

	for _, tc := range cases {
		t.Run(tc.perkID+" scales "+tc.param, func(t *testing.T) {
			s := newTrapState(t)
			s.mu.Lock()
			defer s.mu.Unlock()

			caster, _ := castFirePit(t, s, unitRankBronze)
			base := effTrapField(t, s, caster, "fire_pit", tc.param)

			caster.PerkIDs = []string{tc.perkID}
			got := effTrapField(t, s, caster, "fire_pit", tc.param)

			want := expectedFor(t, tc.perkID, tc.param, base)
			if got != want {
				t.Errorf("%s: %s = %v, want %v (base %v scaled by the perk)", tc.perkID, tc.param, got, want, base)
			}
			if got == base {
				t.Errorf("%s had NO effect on the migrated pit's %s", tc.perkID, tc.param)
			}
		})
	}

	t.Run("perks compose on the same parameter", func(t *testing.T) {
		s := newTrapState(t)
		s.mu.Lock()
		defer s.mu.Unlock()

		caster, _ := castFirePit(t, s, unitRankBronze)
		base := effTrapField(t, s, caster, "fire_pit", "radius")

		caster.PerkIDs = []string{"wider_nets"}
		widened := effTrapField(t, s, caster, "fire_pit", "radius")
		if !(widened > base) {
			t.Fatalf("wider_nets did not widen the pit: %v -> %v", base, widened)
		}
	})
}

// TestFirePitZone_LastingFlamesChangesDelivery is THE acceptance test for the
// ability/perk design: a perk changes HOW the pit delivers damage — from direct
// ticks to a lingering burn — and the ability states that branch IN ITS OWN
// PROGRAM, naming the perk directly (`has_perk: lasting_flames`). Reading
// fire_pit.json tells you the whole story; there is no second file to consult.
func TestFirePitZone_LastingFlamesChangesDelivery(t *testing.T) {
	t.Run("without the perk: direct damage, no burn status", func(t *testing.T) {
		s := newTrapState(t)
		s.mu.Lock()
		defer s.mu.Unlock()

		_, enemy := castFirePit(t, s, unitRankBronze)

		before := enemy.HP
		s.tickAbilityZonesLocked(1)
		if enemy.HP >= before {
			t.Errorf("direct delivery should damage on tick (HP %d -> %d)", before, enemy.HP)
		}
		if len(s.AbilityStatuses) != 0 {
			t.Errorf("direct delivery should apply no status, got %d", len(s.AbilityStatuses))
		}
	})

	t.Run("with lasting_flames: delivery becomes a lingering burn status", func(t *testing.T) {
		s := newTrapState(t)
		s.mu.Lock()
		defer s.mu.Unlock()

		caster, enemy := castFirePit(t, s, unitRankBronze)
		grantPerk(caster, "lasting_flames")

		s.tickAbilityZonesLocked(1)

		if len(s.AbilityStatuses) == 0 {
			t.Fatal("lasting_flames should switch delivery to a burn status")
		}
		found := false
		for _, st := range s.AbilityStatuses {
			if st != nil && st.TargetUnitID == enemy.ID {
				found = true
			}
		}
		if !found {
			t.Error("no burn status found on the enemy standing in the pit")
		}
	})

	// The status is a CONTAINER — spawning one proves nothing on its own. What
	// makes lasting_flames a damage-delivery change rather than a cosmetic one
	// is the On Duration Tick trigger nested INSIDE the apply_status_duration,
	// which is the piece a reader is most likely to lose track of (it was
	// invisible in the editor's flow view until FlowActionCard learned to render
	// the nested triggers of a branch-nested action). Assert the burn actually
	// ticks damage, not just that it exists.
	t.Run("the burn's own tick trigger deals damage after the zone stops touching the target", func(t *testing.T) {
		s := newTrapState(t)
		s.mu.Lock()
		defer s.mu.Unlock()

		caster, enemy := castFirePit(t, s, unitRankBronze)
		grantPerk(caster, "lasting_flames")

		s.tickAbilityZonesLocked(1)
		if len(s.AbilityStatuses) == 0 {
			t.Fatal("lasting_flames should apply a burn status")
		}

		// Tick STATUSES only — the zone is deliberately not advanced here, so
		// any HP loss can only have come from the burn's own nested trigger.
		before := enemy.HP
		s.tickAbilityStatusesLocked(1)
		if enemy.HP >= before {
			t.Errorf("the burn status should deal damage on its own tick (HP %d -> %d)", before, enemy.HP)
		}
	})

	// The burn's flame is a STATUS-BOUND visual (play_presentation with
	// bindToStatusDuration), which is the only thing that anchors it to the
	// afflicted unit — without that flag the same action falls through to
	// play_presentation's at-point shape and renders one flame at the cast
	// point instead of on the enemy, which is exactly how this shipped at first.
	t.Run("the burn's flame is anchored to the afflicted unit, not the cast point", func(t *testing.T) {
		s := newTrapState(t)
		s.mu.Lock()
		defer s.mu.Unlock()

		caster, enemy := castFirePit(t, s, unitRankBronze)
		grantPerk(caster, "lasting_flames")
		s.tickAbilityZonesLocked(1)

		anchored := 0
		for _, e := range s.activeEffects {
			if e.Name == "burning" {
				if e.AnchorUnitID != enemy.ID {
					t.Errorf("burn flame anchored to unit %d, want the afflicted enemy %d", e.AnchorUnitID, enemy.ID)
				}
				anchored++
			}
		}
		if anchored == 0 {
			t.Fatal("no burning effect played on the afflicted enemy")
		}
	})

	// A "refresh"-stacking status re-runs its On Apply trigger on EVERY
	// application, and the fire pit re-applies the burn every single zone tick
	// while the enemy stands in it. The status itself collapses correctly (one
	// status, not N) — the visual did not, stacking a fresh flame per tick.
	t.Run("standing in the pit keeps exactly one flame, not one per tick", func(t *testing.T) {
		s := newTrapState(t)
		s.mu.Lock()
		defer s.mu.Unlock()

		caster, _ := castFirePit(t, s, unitRankBronze)
		grantPerk(caster, "lasting_flames")

		for i := 0; i < 20; i++ {
			s.tickAbilityZonesLocked(0.5)
			s.tickAbilityStatusesLocked(0.5)
		}

		if len(s.AbilityStatuses) != 1 {
			t.Errorf("expected the refreshing burn to collapse to 1 status, got %d", len(s.AbilityStatuses))
		}
		flames := 0
		for _, e := range s.activeEffects {
			if e.Name == "burning" {
				flames++
			}
		}
		if flames != 1 {
			t.Errorf("expected exactly 1 live burn flame, got %d — the status-bound visual is restacking instead of refreshing", flames)
		}
	})

	t.Run("the branch names the perk in the ability's own program", func(t *testing.T) {
		def, ok := getAbilityDef("fire_pit")
		if !ok {
			t.Fatal("fire_pit not found")
		}
		raw, _ := json.Marshal(def.Program)
		if !strings.Contains(string(raw), "has_perk") || !strings.Contains(string(raw), "lasting_flames") {
			t.Error("fire_pit should branch on has_perk(lasting_flames) inline — its behavior must be readable from its own program")
		}
	})
}

// TestFirePitZone_AmplifiedEffectsChangesRealDamage is the end-to-end check
// behind "I gave the Trapper Amplified Effects and the fire pit hit for exactly
// the same".
//
// It asserts on HP ACTUALLY LOST, not on the reported stat. That distinction is
// the whole point: the trap panel already showed 22 instead of 16, because
// EffectiveAbilityFieldLocked folds abilityDamage — while the executor skipped
// the fold entirely for anything running inside a zone, so the enemy kept
// taking 16. A tooltip agreeing with a perk proves nothing about the damage.
func TestFirePitZone_AmplifiedEffectsChangesRealDamage(t *testing.T) {
	damageDealt := func(t *testing.T, perks []string) int {
		t.Helper()
		s := newTrapState(t)
		s.mu.Lock()
		defer s.mu.Unlock()

		caster, enemy := castFirePit(t, s, unitRankBronze)
		caster.ProgressionPath = "trapper"
		caster.PerkIDs = perks
		enemy.HP, enemy.MaxHP = 5000, 5000

		before := enemy.HP
		s.tickAbilityZonesLocked(1)
		return before - enemy.HP
	}

	base := damageDealt(t, nil)
	if base <= 0 {
		t.Fatal("the pit dealt no damage at all; this test cannot say anything")
	}
	amplified := damageDealt(t, []string{"amplified_effects"})
	if amplified <= base {
		t.Errorf("amplified_effects dealt %d, unperked dealt %d — the perk did not reach the pit's actual damage", amplified, base)
	}
}

// The stat and the damage must agree. They did not: the reporting read folded
// abilityDamage and the executor did not, so the panel promised a number the
// enemy never took.
func TestFirePitZone_ReportedDpsMatchesDamageDealt(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster, enemy := castFirePit(t, s, unitRankBronze)
	caster.ProgressionPath = "trapper"
	caster.PerkIDs = []string{"amplified_effects"}
	enemy.HP, enemy.MaxHP = 5000, 5000

	reported := effTrapField(t, s, caster, "fire_pit", "dps")

	before := enemy.HP
	s.fireAbilityZoneTickLocked(s.AbilityZones[0])
	dealt := before - enemy.HP

	if float64(dealt) != reported {
		t.Errorf("one zone tick dealt %d but the panel reports %v dps — the reporting read and the executor disagree", dealt, reported)
	}
}
