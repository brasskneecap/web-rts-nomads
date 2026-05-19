package game

// caster_defer_test.go — tests for the defer-caster-ability-content change.
//
// The placeholder Cleric/Arch Mage grant files were removed (greater_heal /
// arcane_bolt acquisition is deferred), so the Phase-2 content-asserting
// promotion tests were deleted. The per-path grant ENGINE
// (assignUnitPathAbilitiesLocked + the (path,rank) lookup) still ships and
// must stay covered — these tests exercise it via an injected SYNTHETIC grant
// so coverage has zero dependence on authored catalog content. The snapshot
// mechanism is re-tested by adding an ability id directly (no promotion).
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
// asserts: granted ids are appended after the base abilities in catalog order;
// a second invocation is idempotent (append-iff-absent).
func TestDefer_GrantEngine_AppendsInOrderAndIdempotent(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	app := s.spawnPlayerUnitLocked("apprentice", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if app == nil {
		t.Fatal("failed to spawn apprentice")
	}
	base := append([]string(nil), app.Abilities...)

	withSyntheticPathGrant(t, unitPathCleric, unitRankSilver, []string{"synth_grant_a", "synth_grant_b"})
	app.ProgressionPath = unitPathCleric
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

	app := s.spawnPlayerUnitLocked("apprentice", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if app == nil {
		t.Fatal("failed to spawn apprentice")
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

// TestDefer_GrantEngine_RNGFree asserts the grant is deterministic: two seeded
// states with the same forced path + same synthetic grant produce identical
// unit.Abilities (the grant introduces no RNG; only the path *choice* is RNG
// and it is bypassed by forcing ProgressionPath).
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
		app := s.spawnPlayerUnitLocked("apprentice", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
		if app == nil {
			t.Fatal("failed to spawn apprentice")
		}
		app.ProgressionPath = unitPathCleric
		silverThreshold := rankDefByName(unitRankSilver).XPThreshold
		s.addUnitXPLocked(app, silverThreshold+1)
		return append([]string(nil), app.Abilities...)
	}

	a := runSim(11111)
	b := runSim(22222)
	if len(a) != len(b) {
		t.Fatalf("abilities differ across seeds: %v vs %v (grant must be RNG-free)", a, b)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("ability[%d] differs across seeds: %q vs %q (RNG-free violation)", i, a[i], b[i])
		}
	}
	if !abilitiesContain(a, "synth_rngfree") {
		t.Errorf("synthetic grant not applied via addUnitXPLocked path; Abilities=%v", a)
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

	app := s.spawnPlayerUnitLocked("apprentice", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if app == nil {
		t.Fatal("failed to spawn apprentice")
	}
	if abilitiesContain(app.Abilities, "greater_heal") {
		t.Fatal("precondition: apprentice must not already have greater_heal (it is dormant/ungranted)")
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
