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
// blindly asserting) whether every shipped perk actually does something —
// isWiredPerk's OR of "has a Go handler" (wiredPerkIDs) and "carries typed
// data the generic engine executes" (perkHasTypedBehavior). A full manual
// audit of every perk-dispatch file (plus trap.go for
// increased_deployment and projectile.go/state_combat.go for pierce, which
// key off their perk id outside the perks_*.go dispatch files) found a
// production Go reference for 69 of the 72 shipped perk ids; the remaining
// three (hold_the_line, hawk_spirit, vulture_spirit) have ZERO Go
// references anywhere in this package — they are wired purely via
// StatModifiers. This test pins the combined finding as a regression
// guard: if a future catalog addition or a removed handler/StatModifier
// breaks the "every shipped perk is wired" invariant, this fails loud with
// the exact offending id(s) rather than silently shipping wired=false on
// something that used to work.
//
// Per the task brief: if this ever legitimately needs to change (a new
// shipped perk that is intentionally NOT yet wired), that's a real product
// decision — do not "fix" this test by guessing the perk into wiredPerkIDs.
func TestWiredPerkIDs_EveryEmbeddedPerkIsWired(t *testing.T) {
	var inert []string
	for _, def := range snapshotPerkDefs() {
		if !isWiredPerk(*def) {
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
	if _, ok := wiredPerkIDs["bloodlust"]; !ok {
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

// syntheticUnwiredPerkID is a made-up id that must never collide with a real
// catalog entry (and therefore must never appear in wiredPerkIDs) — the
// tests below construct PerkDef literals directly rather than going through
// the catalog loader, since they're only exercising isWiredPerk /
// perkHasTypedBehavior's field checks, not catalog validation.
const syntheticUnwiredPerkID = "__test_synthetic_unwired_perk__"

// TestIsWiredPerk_StatModifiersOnly_ReportsWired locks the typed-data half
// of "wired": a perk with a non-empty StatModifiers list and NO entry in
// wiredPerkIDs must still report wired, because unitPerkStatModifiersLocked
// executes StatModifiers unconditionally with no perk-specific Go.
func TestIsWiredPerk_StatModifiersOnly_ReportsWired(t *testing.T) {
	if _, ok := wiredPerkIDs[syntheticUnwiredPerkID]; ok {
		t.Fatalf("setup: %q unexpectedly present in wiredPerkIDs", syntheticUnwiredPerkID)
	}
	def := PerkDef{
		ID:            syntheticUnwiredPerkID,
		StatModifiers: []PerkStatModifier{{Stat: "moveSpeed", Op: "add", Value: 1}},
	}
	if !isWiredPerk(def) {
		t.Errorf("isWiredPerk(StatModifiers-only, no wiredPerkIDs entry) = false, want true")
	}
}

// TestIsWiredPerk_AbilityRidersOnly_ReportsWired mirrors the StatModifiers
// case for AbilityRiders — the "graft actions onto an ability trigger" half
// of the typed-data engine.
func TestIsWiredPerk_AbilityRidersOnly_ReportsWired(t *testing.T) {
	if _, ok := wiredPerkIDs[syntheticUnwiredPerkID]; ok {
		t.Fatalf("setup: %q unexpectedly present in wiredPerkIDs", syntheticUnwiredPerkID)
	}
	def := PerkDef{
		ID:            syntheticUnwiredPerkID,
		AbilityRiders: []AbilityRider{{Target: "some_ability", Trigger: TriggerOnCastStart}},
	}
	if !isWiredPerk(def) {
		t.Errorf("isWiredPerk(AbilityRiders-only, no wiredPerkIDs entry) = false, want true")
	}
}

// TestIsWiredPerk_AbilityModifiersOnly_ReportsWired mirrors the StatModifiers
// case for AbilityModifiers — the "scale an ability's scalar" half of the
// typed-data engine.
func TestIsWiredPerk_AbilityModifiersOnly_ReportsWired(t *testing.T) {
	if _, ok := wiredPerkIDs[syntheticUnwiredPerkID]; ok {
		t.Fatalf("setup: %q unexpectedly present in wiredPerkIDs", syntheticUnwiredPerkID)
	}
	def := PerkDef{
		ID:               syntheticUnwiredPerkID,
		AbilityModifiers: []AbilityModifier{{Target: "some_ability", CooldownMult: 1.1}},
	}
	if !isWiredPerk(def) {
		t.Errorf("isWiredPerk(AbilityModifiers-only, no wiredPerkIDs entry) = false, want true")
	}
}

// TestIsWiredPerk_GrantsAbilitiesOnly_ReportsWired mirrors the StatModifiers
// case for GrantsAbilities — the "unlock a new castable" half of the
// typed-data engine.
func TestIsWiredPerk_GrantsAbilitiesOnly_ReportsWired(t *testing.T) {
	if _, ok := wiredPerkIDs[syntheticUnwiredPerkID]; ok {
		t.Fatalf("setup: %q unexpectedly present in wiredPerkIDs", syntheticUnwiredPerkID)
	}
	def := PerkDef{
		ID:              syntheticUnwiredPerkID,
		GrantsAbilities: []string{"some_ability"},
	}
	if !isWiredPerk(def) {
		t.Errorf("isWiredPerk(GrantsAbilities-only, no wiredPerkIDs entry) = false, want true")
	}
}

// TestIsWiredPerk_AurasOnly_ReportsWired mirrors the StatModifiers case for
// Auras — the "emit stat changes to nearby units" half of the typed-data
// engine (perk_aura_stat_cache.go). A PerkAura with an EMPTY StatModifiers
// list must NOT count (it does nothing), matching the "carries data the
// generic engine executes" bar the other typed-data fields hold to.
func TestIsWiredPerk_AurasOnly_ReportsWired(t *testing.T) {
	if _, ok := wiredPerkIDs[syntheticUnwiredPerkID]; ok {
		t.Fatalf("setup: %q unexpectedly present in wiredPerkIDs", syntheticUnwiredPerkID)
	}
	withModifiers := PerkDef{
		ID: syntheticUnwiredPerkID,
		Auras: []PerkAura{{
			Radius:        100,
			Targets:       "allies",
			StatModifiers: []PerkStatModifier{{Stat: "moveSpeed", Op: "add", Value: 0.1}},
		}},
	}
	if !isWiredPerk(withModifiers) {
		t.Errorf("isWiredPerk(Auras-with-StatModifiers, no wiredPerkIDs entry) = false, want true")
	}

	emptyAura := PerkDef{
		ID:    syntheticUnwiredPerkID,
		Auras: []PerkAura{{Radius: 100, Targets: "allies"}},
	}
	if isWiredPerk(emptyAura) {
		t.Errorf("isWiredPerk(Auras-with-no-StatModifiers) = true, want false — an aura with nothing to grant does nothing")
	}
}

// TestIsWiredPerk_NoGoHandlerNoTypedData_ReportsNotWired keeps the badge
// meaningful: a perk with neither a Go handler nor any typed-data field
// populated must report NOT wired — this is the actual case the editor's
// "inert" badge exists to catch. Also covers Effect deliberately NOT
// counting as typed behavior (see perk_wired.go's doc comment): a perk with
// only a VFX descriptor and no Go arm referencing its id would never have
// that effect queued.
func TestIsWiredPerk_NoGoHandlerNoTypedData_ReportsNotWired(t *testing.T) {
	if _, ok := wiredPerkIDs[syntheticUnwiredPerkID]; ok {
		t.Fatalf("setup: %q unexpectedly present in wiredPerkIDs", syntheticUnwiredPerkID)
	}
	def := PerkDef{
		ID:     syntheticUnwiredPerkID,
		Effect: &PerkEffect{Name: "whirlwind", Target: "self"},
	}
	if isWiredPerk(def) {
		t.Errorf("isWiredPerk(no Go handler, no typed data, Effect-only) = true, want false")
	}
}

// TestListPerkDefs_SetsWiredTrueForDataDrivenPerk confirms ListPerkDefs
// correctly reports Wired=true for hold_the_line, a real embedded perk that
// was migrated OFF a Go handler onto pure StatModifiers (task 1c/1e) and is
// therefore NOT in wiredPerkIDs — this is the exact scenario the OR
// redefinition exists to fix (a genuinely-working perk must never badge
// "inert" just because it has no Go case anymore).
func TestListPerkDefs_SetsWiredTrueForDataDrivenPerk(t *testing.T) {
	if _, ok := wiredPerkIDs["hold_the_line"]; ok {
		t.Fatalf("setup: hold_the_line unexpectedly present in wiredPerkIDs — audit stale?")
	}
	def := perkDefByID("hold_the_line")
	if def == nil || len(def.StatModifiers) == 0 {
		t.Fatalf("setup: hold_the_line expected to carry StatModifiers")
	}
	var found bool
	for _, cp := range ListPerkDefs() {
		if cp.ID != "hold_the_line" {
			continue
		}
		found = true
		if !cp.Wired {
			t.Errorf("ListPerkDefs()[hold_the_line].Wired = false, want true (data-driven via StatModifiers)")
		}
	}
	if !found {
		t.Fatalf("setup: hold_the_line not present in ListPerkDefs() — catalog changed?")
	}
}

// TestMigratedStatPerks_HaveNoDuplicateConfigKeys guards the final piece of
// the single-source-of-truth migration for hold_the_line, hawk_spirit, and
// vulture_spirit: their catalog JSON must carry StatModifiers and MUST NOT
// also carry a parallel Config entry duplicating the same number (the old
// bonusMaxHP / attackSpeedBonus / damageMultiplier / critChanceBonus keys).
// A designer editing the value in the editor's stat dropdown only ever
// touches StatModifiers — if a future catalog edit reintroduces one of these
// keys, this test fails loudly instead of leaving a stale, contradictory
// number sitting in Config that nothing reads.
func TestMigratedStatPerks_HaveNoDuplicateConfigKeys(t *testing.T) {
	deletedKeys := []string{"bonusMaxHP", "attackSpeedBonus", "damageMultiplier", "critChanceBonus"}
	for _, id := range []string{"hold_the_line", "hawk_spirit", "vulture_spirit"} {
		def := perkDefByID(id)
		if def == nil {
			t.Fatalf("perk %q not found in catalog", id)
		}
		if len(def.StatModifiers) == 0 {
			t.Errorf("perk %q: expected non-empty StatModifiers (data-driven migration), got none", id)
		}
		for _, key := range deletedKeys {
			if _, ok := def.Config[key]; ok {
				t.Errorf("perk %q: Config still carries deleted key %q = %v — dual-source-of-truth reintroduced, remove it and rely on StatModifiers", id, key, def.Config[key])
			}
		}
	}
}
