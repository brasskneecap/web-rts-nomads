package game

// caster_defer_test.go — grant-engine tests via synthetic fixtures.
//
// The per-(path, rank) ability-grant ENGINE
// (assignUnitPathAbilitiesLocked + the (path, rank) lookup) ships and is
// covered here via synthetic injection so coverage has zero dependence on
// authored catalog content. No (path, rank) cell currently has an authored
// grant file — the cleric's greater_heal lives in the path-level "abilities"
// override on cleric.json (see path_defs.go's pathAbilitiesByPath), NOT in
// this rank-grant system. The rank-grant system remains for future
// "silver cleric also gets X" composable content.
//
// No hardcoded balance/tunable numbers: ids are synthetic, ranks come from
// constants, XP thresholds are derived from rank defs.

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// withSyntheticPathGrant injects ids as the grant for (path,rank) into the
// package-global pathAbilityGrantsByKey and restores prior state via
// t.Cleanup. Tests using it must not call t.Parallel() (shared global).
func withSyntheticPathGrant(t *testing.T, path, rank string, ids []string) {
	t.Helper()
	key := pathModifierKey(path, rank)
	prev, had := pathAbilityGrantsByKey[key]
	pathAbilityGrantsByKey[key] = append([]string(nil), ids...)
	t.Cleanup(func() {
		if had {
			pathAbilityGrantsByKey[key] = prev
		} else {
			delete(pathAbilityGrantsByKey, key)
		}
	})
}

// abilitiesContain reports whether unit holds abilityID.
func abilitiesContain(abilities []string, id string) bool {
	for _, a := range abilities {
		if a == id {
			return true
		}
	}
	return false
}

// TestDefer_GrantEngine_AppendsInOrderAndIdempotent drives
// assignUnitPathAbilitiesLocked directly with a synthetic 2-id grant and
// asserts: granted ids are appended in catalog order; a second invocation is
// idempotent.
//
// Uses unitPathVanguard to exercise the rank-grant engine independently of
// any path-level "abilities" override (cleric/siphoner/arch_mage overrides
// would replace the base ability list, masking the "appended after base"
// assertion). Vanguard declares no override, so its grant behavior matches the
// test's "base preserved as prefix" expectation. (arch_mage was used here
// before it gained its arcane_missiles override in arch-mage-spell-system.)
func TestDefer_GrantEngine_AppendsInOrderAndIdempotent(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	app := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if app == nil {
		t.Fatal("failed to spawn acolyte")
	}
	base := append([]string(nil), app.Abilities...)

	withSyntheticPathGrant(t, unitPathVanguard, unitRankSilver, []string{"synth_grant_a", "synth_grant_b"})
	app.ProgressionPath = unitPathVanguard
	app.Rank = unitRankSilver

	s.assignUnitPathAbilitiesLocked(app)

	// Base abilities preserved as a prefix; synthetic ids appended in order.
	if len(app.Abilities) != len(base)+2 {
		t.Fatalf("after grant len(Abilities)=%d; want %d (base %d + 2 synthetic)", len(app.Abilities), len(base)+2, len(base))
	}
	for i, b := range base {
		if app.Abilities[i] != b {
			t.Errorf("base ability at %d changed: got %q want %q", i, app.Abilities[i], b)
		}
	}
	if app.Abilities[len(base)] != "synth_grant_a" || app.Abilities[len(base)+1] != "synth_grant_b" {
		t.Errorf("granted ids not appended in catalog order; tail=%v", app.Abilities[len(base):])
	}

	// Idempotent: re-invocation must not append again.
	before := append([]string(nil), app.Abilities...)
	s.assignUnitPathAbilitiesLocked(app)
	if len(app.Abilities) != len(before) {
		t.Errorf("re-invocation not idempotent: len %d -> %d", len(before), len(app.Abilities))
	}
	for i := range before {
		if app.Abilities[i] != before[i] {
			t.Errorf("re-invocation changed ability[%d]: %q -> %q", i, before[i], app.Abilities[i])
		}
	}
}

// TestDefer_GrantEngine_MultiRankCatchupNoDuplicates drives the real rank-up
// path (addUnitXPLocked) across multiple crossed ranks with synthetic grants
// at bronze/silver/gold and asserts every crossed rank's grant is applied
// exactly once (no duplicates) and the unit reaches gold.
func TestDefer_GrantEngine_MultiRankCatchupNoDuplicates(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	app := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if app == nil {
		t.Fatal("failed to spawn acolyte")
	}

	withSyntheticPathGrant(t, unitPathCleric, unitRankBronze, []string{"synth_bronze"})
	withSyntheticPathGrant(t, unitPathCleric, unitRankSilver, []string{"synth_silver"})
	withSyntheticPathGrant(t, unitPathCleric, unitRankGold, []string{"synth_gold"})

	app.ProgressionPath = unitPathCleric // forced — bypasses RNG path choice
	goldThreshold := rankDefByName(unitRankGold).XPThreshold
	s.addUnitXPLocked(app, goldThreshold+1)

	if app.Rank != unitRankGold {
		t.Errorf("rank after large XP grant = %q; want %q", app.Rank, unitRankGold)
	}
	for _, id := range []string{"synth_bronze", "synth_silver", "synth_gold"} {
		if !abilitiesContain(app.Abilities, id) {
			t.Errorf("crossed-rank grant %q missing after catch-up; Abilities=%v", id, app.Abilities)
		}
	}
	seen := map[string]int{}
	for _, id := range app.Abilities {
		seen[id]++
		if seen[id] > 1 {
			t.Errorf("ability %q duplicated after multi-rank catch-up", id)
		}
	}
}

// TestDefer_GrantEngine_RNGFree asserts the PATH-ABILITY grant is deterministic:
// two seeded states with the same forced path + same synthetic grant get the
// synthetic ability appended (deterministically) regardless of seed. The grant
// engine itself uses no RNG — only the path *choice* does, and the test bypasses
// it by forcing ProgressionPath.
//
// Note: a Silver rank-up also triggers assignUnitPerkLocked, whose
// Bronze-cascade fallback can roll any of the Cleric Bronze perks. The
// path-level "abilities" override on cleric.json (path_defs.go's
// pathAbilitiesByPath) independently swaps base "heal" for "greater_heal"
// on every promotion regardless of seed. Both effects are deterministic
// under a fixed seed but vary across seeds, so this test checks the grant
// engine's contribution (the synthetic id appended for the Silver grant)
// rather than asserting byte-equal ability slices across seeds.
func TestDefer_GrantEngine_RNGFree(t *testing.T) {
	withSyntheticPathGrant(t, unitPathCleric, unitRankSilver, []string{"synth_rngfree"})

	runSim := func(seed int64) []string {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), seed)
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.Players["p1"] == nil {
			s.Players["p1"] = &Player{
				ID:                            "p1",
				Resources:                     map[string]int{"gold": 9999, "wood": 9999},
				GlobalUnitSpawnTimeMultiplier: 1,
				UnitSpawnTimeMultipliers:      map[string]float64{},
				Upgrades:                      map[UpgradeTrack]int{},
				Vault:                         []*VaultItem{},
			}
		}
		app := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
		if app == nil {
			t.Fatal("failed to spawn acolyte")
		}
		app.ProgressionPath = unitPathCleric
		silverThreshold := rankDefByName(unitRankSilver).XPThreshold
		s.addUnitXPLocked(app, silverThreshold+1)
		return append([]string(nil), app.Abilities...)
	}

	a := runSim(11111)
	b := runSim(22222)
	// The synthetic grant is deterministic regardless of which perk landed in
	// the Silver→Bronze-cascade roll; both runs must contain it.
	if !abilitiesContain(a, "synth_rngfree") || !abilitiesContain(b, "synth_rngfree") {
		t.Errorf("synthetic grant not applied via addUnitXPLocked path; a=%v b=%v", a, b)
	}
	// And the grant engine's contribution to the ability slice (i.e. the
	// synthetic id, indexed from the end since perk-induced changes only
	// affect existing slots and never re-order the grant append) must match.
	if a[len(a)-1] != b[len(b)-1] {
		t.Errorf("grant-appended ability differs across seeds: %q vs %q (grant must be RNG-free)",
			a[len(a)-1], b[len(b)-1])
	}
}

// TestDefer_GrantedAbilityInSnapshot re-tests the snapshot-surfacing mechanism
// WITHOUT a promotion grant: an ability id present in unit.Abilities (added
// directly here) must appear in abilityStatesLocked with def-derived fields and
// a working autocast toggle. Uses the dormant greater_heal def, which also
// confirms dormant defs still load and resolve via getAbilityDef.
func TestDefer_GrantedAbilityInSnapshot(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	def, ok := getAbilityDef("greater_heal")
	if !ok {
		t.Fatal(`getAbilityDef("greater_heal") = _, false; the dormant def must still load and resolve`)
	}

	app := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if app == nil {
		t.Fatal("failed to spawn acolyte")
	}
	if abilitiesContain(app.Abilities, "greater_heal") {
		t.Fatal("precondition: acolyte must not already have greater_heal (it is dormant/ungranted)")
	}
	// Add it directly — the snapshot path reads unit.Abilities regardless of
	// how an id got there; this isolates the snapshot mechanism from grants.
	app.Abilities = append(app.Abilities, "greater_heal")

	snaps := s.abilityStatesLocked(app)
	var got *protocol.AbilitySnapshot
	for i := range snaps {
		if snaps[i].ID == "greater_heal" {
			got = &snaps[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("greater_heal missing from AbilitySnapshot; got %+v", snaps)
	}
	if got.ManaCost != def.ManaCost {
		t.Errorf("snapshot ManaCost=%d; want %d (from def)", got.ManaCost, def.ManaCost)
	}
	if got.SupportsAutoCast != def.SupportsAutoCast {
		t.Errorf("snapshot SupportsAutoCast=%v; want %v (from def)", got.SupportsAutoCast, def.SupportsAutoCast)
	}
	if got.CooldownTotal != def.Cooldown {
		t.Errorf("snapshot CooldownTotal=%v; want %v (from def)", got.CooldownTotal, def.Cooldown)
	}
	if got.CooldownRemaining != 0 {
		t.Errorf("snapshot CooldownRemaining=%v; want 0 (not on cooldown)", got.CooldownRemaining)
	}
	if got.AutoCast {
		t.Error("snapshot AutoCast should default false (not toggled)")
	}

	// Toggle on; the next snapshot must reflect it (no new protocol field).
	s.toggleAutoCastLocked(app, "greater_heal")
	for _, sn := range s.abilityStatesLocked(app) {
		if sn.ID == "greater_heal" && !sn.AutoCast {
			t.Error("after toggle, snapshot AutoCast=false; want true")
		}
	}
}

// TestPathAbilities_ClericPathOverridesHealWithGreaterHeal is the real-catalog
// regression guard for the path-level "abilities" override on cleric.json.
// It asserts that pathAbilitiesByPath["cleric"] resolves to ["greater_heal"]
// — i.e. that the override field on cleric.json is correctly authored and
// loaded. The runtime effect (unit.Abilities = ["greater_heal"] after a
// promotion) is covered separately in greater_heal_swap_test.go.
func TestPathAbilities_ClericPathOverridesHealWithGreaterHeal(t *testing.T) {
	got, ok := pathAbilitiesByPath[unitPathCleric]
	if !ok {
		t.Fatalf("pathAbilitiesByPath[%q] not present; cleric.json's \"abilities\" override is missing or unloaded", unitPathCleric)
	}
	if len(got) != 1 || got[0] != "greater_heal" {
		t.Errorf("pathAbilitiesByPath[%q] = %v; want [\"greater_heal\"]", unitPathCleric, got)
	}
}
