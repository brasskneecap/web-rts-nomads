package game

import (
	"reflect"
	"sort"
	"testing"
)

// clearPerkOverlayForTest resets the runtime perk overlay to empty and rebuilds
// the registry back to the pure-embedded baseline. runtimePerks and
// perkDefsByID are process-global, so every test that calls SavePerkDef /
// DeletePerkOverride / LoadPersistedPerksIntoOverlay MUST register this via
// t.Cleanup.
func clearPerkOverlayForTest(t *testing.T) {
	t.Helper()
	runtimePerksMu.Lock()
	for k := range runtimePerks {
		delete(runtimePerks, k)
	}
	runtimePerksMu.Unlock()
	rebuildPerkRegistry()
}

// withIsolatedPerkCatalogDir points PERK_CATALOG_DIR at a fresh t.TempDir() so
// Save/Delete/Load in this test never touch the real source catalog, and
// registers cleanup of the process-global overlay. The embedded baseline
// (embeddedPerkDefs) is unaffected by this env var — captured once from the
// real go:embed data at process init.
func withIsolatedPerkCatalogDir(t *testing.T) {
	t.Helper()
	t.Setenv("PERK_CATALOG_DIR", t.TempDir())
	t.Cleanup(func() { clearPerkOverlayForTest(t) })
}

func TestSaveAndOverlayPerkDef(t *testing.T) {
	withIsolatedPerkCatalogDir(t)
	def := &PerkDef{ID: "test_perk", DisplayName: "Test Perk"}
	if err := SavePerkDef(def); err != nil {
		t.Fatalf("SavePerkDef: %v", err)
	}
	got, ok := perkDefLookup("test_perk")
	if !ok || got.DisplayName != "Test Perk" {
		t.Fatalf("overlay def not resolved: ok=%v got=%+v", ok, got)
	}
	if PerkIsEmbedded("test_perk") {
		t.Fatal("test_perk should not be embedded")
	}
	existed, err := DeletePerkOverride("test_perk")
	if err != nil || !existed {
		t.Fatalf("delete existed=%v err=%v", existed, err)
	}
	if _, ok := perkDefLookup("test_perk"); ok {
		t.Fatal("def still resolvable after delete")
	}
}

func TestPerkDiskRoundTripAndRevert(t *testing.T) {
	withIsolatedPerkCatalogDir(t)
	// disk round-trip: save, clear overlay, reload from disk.
	if err := SavePerkDef(&PerkDef{ID: "disk_perk", DisplayName: "On Disk"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	runtimePerksMu.Lock()
	delete(runtimePerks, "disk_perk")
	runtimePerksMu.Unlock()
	rebuildPerkRegistry()
	if _, ok := perkDefLookup("disk_perk"); ok {
		t.Fatal("expected miss after clearing overlay (disk_perk is not embedded)")
	}
	LoadPersistedPerksIntoOverlay()
	if got, ok := perkDefLookup("disk_perk"); !ok || got.DisplayName != "On Disk" {
		t.Fatalf("disk reload failed: ok=%v got=%+v", ok, got)
	}

	// embed-revert: override a real embedded perk, then delete reverts to it.
	var embeddedID string
	for _, d := range snapshotPerkDefs() {
		if PerkIsEmbedded(d.ID) {
			embeddedID = d.ID
			break
		}
	}
	if embeddedID == "" {
		t.Skip("no embedded perks to test revert")
	}
	original := embeddedPerkDefs[embeddedID]
	override := original
	override.DisplayName = original.DisplayName + " (edited)"
	if err := SavePerkDef(&override); err != nil {
		t.Fatalf("override save: %v", err)
	}
	if got, _ := perkDefLookup(embeddedID); got.DisplayName != original.DisplayName+" (edited)" {
		t.Fatal("overlay did not win over embed")
	}
	if _, err := DeletePerkOverride(embeddedID); err != nil {
		t.Fatalf("revert delete: %v", err)
	}
	if !PerkIsEmbedded(embeddedID) {
		t.Fatal("embedded id lost embedded status")
	}
	if got, _ := perkDefLookup(embeddedID); got.DisplayName != original.DisplayName {
		t.Fatalf("did not revert to embedded default: %+v", got)
	}
}

func TestSavePerkDefRejectsBadID(t *testing.T) {
	withIsolatedPerkCatalogDir(t)
	if err := SavePerkDef(&PerkDef{ID: "Bad/../x"}); err == nil {
		t.Fatal("expected id-pattern rejection")
	}
	// DeletePerkOverride is also gated: a bad id is a no-op, never an error.
	existed, err := DeletePerkOverride("Bad/../x")
	if err != nil || existed {
		t.Fatalf("DeletePerkOverride(bad id) existed=%v err=%v, want false/nil", existed, err)
	}
}

// TestPerkSelectionEquivalence pins the id-addressed catalog against the known
// shipped soldier/berserker perk pools. The expected id sets were read from the
// pre-flip pool files (catalog/units/human/soldier/paths/berserker/perks/
// {bronze,silver,gold}.json) and match soldier/berserker's authored
// perksByRank. eligiblePerksForUnitAtRank is the exact filter the rank-up
// assignment pipeline uses, so equal id sets here prove selection behavior is
// unchanged by the flip.
func TestPerkSelectionEquivalence(t *testing.T) {
	unit := &Unit{UnitType: "soldier", ProgressionPath: "berserker"}
	cases := []struct {
		rank string
		want []string
	}{
		{unitRankBronze, []string{"bloodlust", "cleaving_rage", "frenzy_core", "relentless", "savage_strikes"}},
		{unitRankSilver, []string{"blood_sustain", "executioner", "momentum"}},
		{unitRankGold, []string{"berserk_state", "blood_engine", "whirlwind_core"}},
	}
	for _, tc := range cases {
		var got []string
		for _, def := range eligiblePerksForUnitAtRank(unit, tc.rank) {
			got = append(got, def.ID)
		}
		sort.Strings(got)
		sort.Strings(tc.want)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("soldier/berserker %s: eligible ids = %v, want %v", tc.rank, got, tc.want)
		}
	}
}

// TestEligiblePerksForUnitAtRank_RefsAreSoleSource pins B1's refs-only model:
// eligiblePerksForUnitAtRank must include a perk referenced via the path's
// PerksByRank even when that perk's OWN association (Path) would not match
// the unit's path, must EXCLUDE a perk whose Path DOES match the unit's path
// but is NOT referenced (the old catalog scan is gone — this is the key
// regression guard against silently reintroducing it), must dedup a perk id
// repeated in the authored list, and must keep the ID-sorted output
// determinism invariant that rngPerks.Intn relies on.
func TestEligiblePerksForUnitAtRank_RefsAreSoleSource(t *testing.T) {
	withIsolatedPerkCatalogDir(t)
	withIsolatedPathCatalogDir(t)

	// Path does NOT match soldier's berserker path, so this perk can only
	// appear in the pool via the explicit reference.
	if err := SavePerkDef(&PerkDef{ID: "ref_only_perk", DisplayName: "Ref Only", Path: "nobody"}); err != nil {
		t.Fatalf("SavePerkDef(ref_only_perk): %v", err)
	}
	// Path DOES match berserker but this perk is NOT referenced in
	// PerksByRank — under the refs-only model it must be excluded even
	// though the old field-scan would have matched it.
	if err := SavePerkDef(&PerkDef{ID: "unreferenced_match_perk", DisplayName: "Unreferenced Match", Path: "berserker"}); err != nil {
		t.Fatalf("SavePerkDef(unreferenced_match_perk): %v", err)
	}

	if err := SavePathDef("soldier", &pathCatalogFile{
		Path: "berserker",
		// ref_only_perk listed twice to also pin dedup of a repeated id.
		PerksByRank: map[string][]string{unitRankBronze: {"ref_only_perk", "ref_only_perk"}},
	}); err != nil {
		t.Fatalf("SavePathDef(berserker): %v", err)
	}

	unit := &Unit{UnitType: "soldier", ProgressionPath: "berserker"}
	pool := eligiblePerksForUnitAtRank(unit, unitRankBronze)

	counts := map[string]int{}
	for _, def := range pool {
		counts[def.ID]++
	}
	if counts["ref_only_perk"] != 1 {
		t.Fatalf("ref_only_perk count = %d, want 1 (pool=%v)", counts["ref_only_perk"], perkIDs(pool))
	}
	if counts["unreferenced_match_perk"] != 0 {
		t.Fatalf("unreferenced_match_perk count = %d, want 0 — field-matched but unreferenced perks must not roll (pool=%v)", counts["unreferenced_match_perk"], perkIDs(pool))
	}
	if len(pool) != 1 {
		t.Fatalf("pool = %v, want exactly [ref_only_perk]", perkIDs(pool))
	}
	for i := 1; i < len(pool); i++ {
		if pool[i-1].ID > pool[i].ID {
			t.Fatalf("pool not ID-sorted: %v", perkIDs(pool))
		}
	}
}

// TestEligiblePerksForUnitAtRank_NoRefsYieldsEmptyPool asserts that when a
// path has no PerksByRank entry for a given rank, eligiblePerksForUnitAtRank
// returns an empty pool — NOT a fallback scan of the whole catalog for
// field-matching perks. This is the direct regression guard for B1: refs are
// the SOLE source, so an unauthored rank must roll nothing rather than
// silently reviving the old auto-match behavior.
func TestEligiblePerksForUnitAtRank_NoRefsYieldsEmptyPool(t *testing.T) {
	withIsolatedPerkCatalogDir(t)
	withIsolatedPathCatalogDir(t)

	// Path matches soldier's berserker path exactly — under the old scan this
	// would have rolled. PerksByRank below deliberately omits bronze.
	if err := SavePerkDef(&PerkDef{ID: "would_have_matched_perk", DisplayName: "Would Have Matched", Path: "berserker"}); err != nil {
		t.Fatalf("SavePerkDef(would_have_matched_perk): %v", err)
	}
	if err := SavePathDef("soldier", &pathCatalogFile{
		Path:        "berserker",
		PerksByRank: map[string][]string{unitRankSilver: {"would_have_matched_perk"}},
	}); err != nil {
		t.Fatalf("SavePathDef(berserker): %v", err)
	}

	unit := &Unit{UnitType: "soldier", ProgressionPath: "berserker"}
	pool := eligiblePerksForUnitAtRank(unit, unitRankBronze)
	if len(pool) != 0 {
		t.Fatalf("bronze pool with no authored refs = %v, want empty", perkIDs(pool))
	}
}

// perkIDs is a small test helper for readable failure messages.
func perkIDs(pool []*PerkDef) []string {
	ids := make([]string, len(pool))
	for i, def := range pool {
		ids[i] = def.ID
	}
	return ids
}

func TestPerkDefAccessors_ConcurrentReadsDoNotRace(t *testing.T) {
	const goroutines = 8
	const iterations = 200

	done := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < iterations; j++ {
				_, _ = perkDefLookup("battle_prayer")
				_ = snapshotPerkDefs()
				_ = perkDefByID("battle_prayer")
			}
		}()
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}
}
