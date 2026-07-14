package game

import "testing"

// TestWiredPerkIDs_EveryEntryExistsInEmbeddedCatalog catches typos in the
// hand-maintained wiredPerkIDs set (perk_wired.go): every id it lists must
// resolve against the real embedded catalog, or the set has drifted from
// the actual perk ids (e.g. a rename on one side and not the other).
func TestWiredPerkIDs_EveryEntryExistsInEmbeddedCatalog(t *testing.T) {
	for id := range wiredPerkIDs {
		if _, ok := perkDefLookup(id); !ok {
			t.Errorf("wiredPerkIDs contains %q, but no embedded perk def resolves to it — typo?", id)
		}
	}
}

// TestWiredPerkIDs_EveryEmbeddedPerkIsWired investigates (rather than
// blindly asserting) whether every shipped perk has a matching Go handler.
// As of this task, a full manual audit of every perk-dispatch file (plus
// trap.go for increased_deployment and projectile.go/state_combat.go for
// pierce, which key off their perk id outside the perks_*.go dispatch files)
// found a production Go reference for ALL 72 shipped perk ids — see the
// coordinator report for the file-by-file breakdown. This test pins that
// finding as a regression guard: if a future catalog addition or a removed
// handler breaks the "every shipped perk is wired" invariant, this fails
// loud with the exact offending id(s) rather than silently shipping
// wired=false on something that used to work.
//
// Per the task brief: if this ever legitimately needs to change (a new
// shipped perk that is intentionally NOT yet wired), that's a real product
// decision — do not "fix" this test by guessing the perk into wiredPerkIDs.
func TestWiredPerkIDs_EveryEmbeddedPerkIsWired(t *testing.T) {
	var inert []string
	for _, def := range snapshotPerkDefs() {
		if !isWiredPerk(def.ID) {
			inert = append(inert, def.ID)
		}
	}
	if len(inert) > 0 {
		t.Errorf("embedded perks with no Go handler (wired=false): %v — confirm this is expected before landing", inert)
	}
}

// TestListPerkDefs_SetsWiredTrueForKnownWiredID confirms ListPerkDefs
// actually populates the new Wired field (not just that wiredPerkIDs
// exists) using bloodlust, a real embedded perk with a well-known handler
// (perks.go / perks_attack.go / perks_icons.go).
func TestListPerkDefs_SetsWiredTrueForKnownWiredID(t *testing.T) {
	if !isWiredPerk("bloodlust") {
		t.Fatalf("setup: bloodlust expected to be in wiredPerkIDs")
	}
	var found bool
	for _, def := range ListPerkDefs() {
		if def.ID != "bloodlust" {
			continue
		}
		found = true
		if !def.Wired {
			t.Errorf("ListPerkDefs()[bloodlust].Wired = false, want true")
		}
	}
	if !found {
		t.Fatalf("setup: bloodlust not present in ListPerkDefs() — catalog changed?")
	}
}

// TestListPerkDefs_RegistryPerkDefsLeaveWiredFalse locks the documented
// scoping: Wired is only ever set on ListPerkDefs' returned copies, never on
// the registry's own *PerkDef values. This is what keeps perkDefLookup /
// snapshotPerkDefs (used all over combat/perk assignment code) from having
// to care about the field at all.
func TestListPerkDefs_RegistryPerkDefsLeaveWiredFalse(t *testing.T) {
	def, ok := perkDefLookup("bloodlust")
	if !ok {
		t.Fatalf("setup: perkDefLookup(bloodlust) not found")
	}
	if def.Wired {
		t.Errorf("perkDefLookup(bloodlust).Wired = true, want false — Wired must only be set on ListPerkDefs' copies")
	}
}
