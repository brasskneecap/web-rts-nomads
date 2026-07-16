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
	def := &PerkDef{ID: "test_perk", DisplayName: "Test Perk", Rank: unitRankBronze}
	if err := SavePerkDef(def); err != nil {
		t.Fatalf("SavePerkDef: %v", err)
	}
	got, ok := perkDefLookup("test_perk")
	if !ok || got.DisplayName != "Test Perk" || got.Rank != unitRankBronze {
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
	if err := SavePerkDef(&PerkDef{ID: "disk_perk", DisplayName: "On Disk", Rank: unitRankSilver}); err != nil {
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
	if got, ok := perkDefLookup("disk_perk"); !ok || got.DisplayName != "On Disk" || got.Rank != unitRankSilver {
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

func TestSavePerkDefRejectsBadRank(t *testing.T) {
	withIsolatedPerkCatalogDir(t)
	if err := SavePerkDef(&PerkDef{ID: "bad_rank_perk", Rank: "platinum"}); err == nil {
		t.Fatal("expected rank rejection for a non bronze/silver/gold rank")
	}
	if _, ok := perkDefLookup("bad_rank_perk"); ok {
		t.Fatal("bad-rank perk must not be registered after a rejected save")
	}
}

// TestPerkSelectionEquivalence pins the id-addressed catalog against the known
// shipped soldier/berserker perk pools. The expected id sets were read from the
// pre-flip pool files (catalog/units/human/soldier/paths/berserker/perks/
// {bronze,silver,gold}.json) and match the committed catalog/perks/<id>.json
// unitType/path/rank fields. eligiblePerksForUnitAtRank is the exact filter the
// rank-up assignment pipeline uses, so equal id sets here prove selection
// behavior is unchanged by the flip.
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
