package game

import (
	"encoding/json"
	"math"
	"testing"
)

// ═════════════════════════════════════════════════════════════════════════════
// shared_suffering → ability-rider migration characterization
//
// See docs/superpowers/plans/2026-07-19-perk-ability-riders-tier-b.md Task 4.
//
// These tests pin the OBSERVABLE effect of the Siphoner's shared_suffering
// echo by driving it through the REAL production channel-tick call site
// (beginAbilityChannelLocked + GameState.Update), never by calling
// applySharedSufferingLocked directly — that Go helper is deleted at the end
// of this migration (perks_siphoner.go), so a test that called it directly
// could not survive the cutover. Routing through the shared tick call site
// (ability_channel.go, tickUnitChannelLocked) means these tests are
// agnostic to whether that call site invokes the old Go helper or the new
// data-driven ability rider — they must stay green across the whole
// migration with NO edits to their bodies (only the Gold-combo test's
// EXPECTATION changes, and only because that behavior delta is the one
// explicitly accepted by the plan).
//
// Expected values are derived from the shared_suffering perk's own catalog
// Config (never hardcoded balance numbers) and from the OBSERVED primary-tick
// damage each run actually produced (never a hardcoded DamagePerTick), so the
// same assertion logic also proves the soul_leech proportionality case.
// ═════════════════════════════════════════════════════════════════════════════

// sharedSufferingMigrationScene is the target-set matrix used by every test
// in this file: a Siphoner channeling Siphon Life on a primary target, with
// one neighbor in each of the five categories the old Go loop discriminated
// between: hostile in-radius (echoed), hostile out-of-radius (not echoed),
// allied in-radius (not echoed — wrong relation), invisible hostile in-radius
// (not echoed — visibility gate), and the primary itself (excluded, it
// already took the full primary tick).
type sharedSufferingMigrationScene struct {
	s                    *GameState
	siphoner             *Unit
	primary              *Unit
	hostileInRadius      *Unit
	hostileOutOfRadius   *Unit
	allyInRadius         *Unit
	invisibleInRadius    *Unit
}

// buildSharedSufferingMigrationScene spawns the matrix above. perkIDs is
// appended to the siphoner's owned perks verbatim (e.g. just
// ["shared_suffering"] for the base case, or +"soul_leech"/+"ascended_corruption"
// for the other cases in this file). Every combat-capable unit has
// Damage/MoveSpeed zeroed (neuterIncidentalCombat) so nothing but the
// channel tick itself can move an HP number during the single Update() call
// each test drives. Returns with s.mu NOT held.
func buildSharedSufferingMigrationScene(t *testing.T, perkIDs ...string) sharedSufferingMigrationScene {
	t.Helper()
	s := newChannelTestState(t)
	def := getSiphonLifeDef(t)

	ss := perkDefByID("shared_suffering")
	if ss == nil {
		t.Fatal("shared_suffering perk def missing")
	}
	radius := ss.Config["radius"]
	if radius <= 0 {
		t.Fatalf("shared_suffering radius config = %v, want > 0", radius)
	}

	s.mu.Lock()
	siphoner := spawnSiphoner(t, s, "p1", 100, 100, 5000)
	siphoner.PerkIDs = append(siphoner.PerkIDs, perkIDs...)
	neuterIncidentalCombat(siphoner)

	// Primary target: well within siphon_life's castRange (220) of the
	// siphoner, with a deep HP pool so the single tick this file drives never
	// kills it (that would make the death pipeline remove the unit mid-tick
	// and complicate the before/after HP snapshot).
	primary := spawnChannelEnemy(t, s, 100+def.CastRange.Resolve(siphoner)*0.4, 100, 100000, 100000)
	neuterIncidentalCombat(primary)

	hostileInRadius := spawnChannelEnemy(t, s, primary.X+radius*0.5, primary.Y, 100000, 100000)
	neuterIncidentalCombat(hostileInRadius)

	hostileOutOfRadius := spawnChannelEnemy(t, s, primary.X+radius*2, primary.Y, 100000, 100000)
	neuterIncidentalCombat(hostileOutOfRadius)

	// Full HP so distributeSiphonHealLocked (allyHealRadius=220 on
	// siphon_life — easily reaches a neighbor spawned near the primary) can
	// never move this unit's HP: missing-HP is 0, so healUnitLocked's
	// overheal branch touches Shield, never HP.
	allyInRadius := spawnChannelAlly(t, s, "p1", primary.X-radius*0.5, primary.Y, 200, 200)
	neuterIncidentalCombat(allyInRadius)

	invisibleInRadius := spawnChannelEnemy(t, s, primary.X+radius*0.3, primary.Y+5, 100000, 100000)
	invisibleInRadius.Visible = false
	neuterIncidentalCombat(invisibleInRadius)

	s.mu.Unlock()

	return sharedSufferingMigrationScene{
		s:                  s,
		siphoner:           siphoner,
		primary:            primary,
		hostileInRadius:    hostileInRadius,
		hostileOutOfRadius: hostileOutOfRadius,
		allyInRadius:       allyInRadius,
		invisibleInRadius:  invisibleInRadius,
	}
}

// runOneChannelTick starts the channel and drives exactly one tick
// (Update(TickIntervalSeconds), matching the established convention in
// ability_compile_golden_channel_perks_test.go that dt==interval fires
// exactly one channel tick per Update call). Returns the HP delta
// (before-after) for every unit in the scene, keyed by unit ID.
func runOneChannelTick(t *testing.T, sc sharedSufferingMigrationScene) map[int]int {
	t.Helper()
	def := getSiphonLifeDef(t)

	sc.s.mu.Lock()
	ok, reason := sc.s.beginAbilityChannelLocked(sc.siphoner, "siphon_life", sc.primary)
	sc.s.mu.Unlock()
	if !ok {
		t.Fatalf("beginAbilityChannelLocked failed: %q", reason)
	}

	before := map[int]int{}
	sc.s.mu.RLock()
	for _, u := range sc.s.Units {
		if u != nil {
			before[u.ID] = u.HP
		}
	}
	sc.s.mu.RUnlock()

	sc.s.Update(def.TickIntervalSeconds)

	delta := map[int]int{}
	sc.s.mu.RLock()
	defer sc.s.mu.RUnlock()
	for id, hpBefore := range before {
		u := sc.s.getUnitByIDLocked(id)
		hpAfter := 0
		if u != nil {
			hpAfter = u.HP
		}
		delta[id] = hpBefore - hpAfter // positive = HP lost
	}
	return delta
}

// TestSharedSufferingMigration_BaseCase_ExactTargetSetAndDamage is the
// byte-identical proof for the common case (shared_suffering owned WITHOUT
// ascended_corruption): exactly the hostile, visible, in-radius neighbor
// takes the echo, at exactly round(observedPrimaryTickDamage * sharePct);
// every other neighbor category takes zero.
func TestSharedSufferingMigration_BaseCase_ExactTargetSetAndDamage(t *testing.T) {
	sc := buildSharedSufferingMigrationScene(t, "shared_suffering")
	ss := perkDefByID("shared_suffering")
	sharePct := ss.Config["damageSharePercent"]
	if sharePct <= 0 {
		t.Fatalf("shared_suffering damageSharePercent config = %v, want > 0", sharePct)
	}

	delta := runOneChannelTick(t, sc)

	primaryTickDamage := delta[sc.primary.ID]
	if primaryTickDamage <= 0 {
		t.Fatalf("primary took no tick damage; delta = %d — scene setup is broken", primaryTickDamage)
	}
	wantEcho := int(math.Round(float64(primaryTickDamage) * sharePct))
	if wantEcho <= 0 {
		t.Fatalf("computed wantEcho = %d, want > 0 (sharePct=%v drifted to a no-op)", wantEcho, sharePct)
	}

	if got := delta[sc.hostileInRadius.ID]; got != wantEcho {
		t.Errorf("hostile in-radius echo damage = %d, want %d (round(%d * %v))", got, wantEcho, primaryTickDamage, sharePct)
	}
	if got := delta[sc.hostileOutOfRadius.ID]; got != 0 {
		t.Errorf("hostile out-of-radius took echo damage: delta = %d, want 0", got)
	}
	if got := delta[sc.allyInRadius.ID]; got != 0 {
		t.Errorf("allied in-radius took echo damage: delta = %d, want 0", got)
	}
	if got := delta[sc.invisibleInRadius.ID]; got != 0 {
		t.Errorf("invisible hostile in-radius took echo damage: delta = %d, want 0", got)
	}
}

// TestSharedSufferingMigration_EchoScalesWithAlreadyScaledPrimaryDamage
// proves the soul_leech interaction: since the echo is a FRACTION of the
// PRIMARY TICK's already-scaled damage (not a fresh, independently-scaled
// number), a caster whose primary tick damage is boosted (soul_leech's
// damageMultiplier) must see a proportionally larger echo — echo/primary
// stays pinned at sharePct in both runs, and the boosted run's primary (and
// therefore echo) is strictly larger than the unboosted run's.
func TestSharedSufferingMigration_EchoScalesWithAlreadyScaledPrimaryDamage(t *testing.T) {
	ss := perkDefByID("shared_suffering")
	sharePct := ss.Config["damageSharePercent"]

	base := buildSharedSufferingMigrationScene(t, "shared_suffering")
	baseDelta := runOneChannelTick(t, base)
	basePrimary := baseDelta[base.primary.ID]
	baseEcho := baseDelta[base.hostileInRadius.ID]

	soulLeech := perkDefByID("soul_leech")
	if soulLeech == nil {
		t.Fatal("soul_leech perk def missing")
	}
	if soulLeech.Config["damageMultiplier"] == 1.0 {
		t.Fatal("soul_leech damageMultiplier config drifted to a no-op 1.0 — this case can't prove anything about proportional scaling")
	}

	boosted := buildSharedSufferingMigrationScene(t, "shared_suffering", "soul_leech")
	boostedDelta := runOneChannelTick(t, boosted)
	boostedPrimary := boostedDelta[boosted.primary.ID]
	boostedEcho := boostedDelta[boosted.hostileInRadius.ID]

	if boostedPrimary <= basePrimary {
		t.Fatalf("soul_leech did not increase primary tick damage: base=%d boosted=%d", basePrimary, boostedPrimary)
	}
	if boostedEcho <= baseEcho {
		t.Errorf("echo did not scale up with the boosted primary tick: base echo=%d boosted echo=%d", baseEcho, boostedEcho)
	}

	wantBaseEcho := int(math.Round(float64(basePrimary) * sharePct))
	wantBoostedEcho := int(math.Round(float64(boostedPrimary) * sharePct))
	if baseEcho != wantBaseEcho {
		t.Errorf("base echo = %d, want %d (round(%d * %v))", baseEcho, wantBaseEcho, basePrimary, sharePct)
	}
	if boostedEcho != wantBoostedEcho {
		t.Errorf("boosted echo = %d, want %d (round(%d * %v))", boostedEcho, wantBoostedEcho, boostedPrimary, sharePct)
	}
}

// TestSharedSufferingMigration_GoldCombo_AscendedCorruptionOverlayIsInert
// documents the ONE accepted behavior delta of this migration (plan Task 4 /
// the "Perk-modifies-perk deferral" section). PRE-migration, a Siphoner
// owning BOTH shared_suffering AND ascended_corruption (Gold) got a layered
// radius×1.5 / share+0.2 overlay via sharedSufferingEffectiveConfigLocked
// (still proven live, on its own, by
// TestAscendedCorruption_SharedSufferingLayering in
// siphoner_gold_perks_test.go — that helper is intentionally kept, it is
// just no longer CONSULTED by the live echo path). POST-migration (this
// test's current, updated form), the rider authors ONLY the base values
// (radius 120, share 0.4) as data — TargetQueryDef.Radius is a static
// float64 with no ref-based override mechanism yet, so a byte-identical
// data-only rider for the Gold combo is not possible without a second new
// primitive (target-query radius refs), deferred to Tier B.5. This test was
// UPDATED (in the same migration step that wired the rider, per its own
// original doc comment) from asserting the layered share to asserting the
// BASE share now applies even with ascended_corruption owned — that is the
// one accepted regression this migration ships. Every other test in this
// file stayed green with zero edits across the whole migration.
func TestSharedSufferingMigration_GoldCombo_AscendedCorruptionOverlayIsInert(t *testing.T) {
	ss := perkDefByID("shared_suffering")
	asc := perkDefByID("ascended_corruption")
	if asc == nil {
		t.Fatal("ascended_corruption perk def missing")
	}
	if asc.Config["sharedDamageSharePercentBonus"] <= 0 || asc.Config["sharedRadiusMultiplier"] <= 1.0 {
		t.Fatalf("ascended_corruption overlay config drifted to a no-op — this test can't prove the overlay is inert vs. absent-by-coincidence")
	}

	sc := buildSharedSufferingMigrationScene(t, "shared_suffering", "ascended_corruption")
	delta := runOneChannelTick(t, sc)

	primaryTickDamage := delta[sc.primary.ID]
	if primaryTickDamage <= 0 {
		t.Fatalf("primary took no tick damage; delta = %d", primaryTickDamage)
	}

	// Base share only — the Gold overlay's +sharedDamageSharePercentBonus is
	// NOT applied post-migration (Tier B.5 will restore it).
	baseShare := ss.Config["damageSharePercent"]
	wantEcho := int(math.Round(float64(primaryTickDamage) * baseShare))

	if got := delta[sc.hostileInRadius.ID]; got != wantEcho {
		t.Errorf("Gold-combo echo damage = %d, want %d (round(%d * baseShare=%v)) — post-migration the overlay must be inert, not layered",
			got, wantEcho, primaryTickDamage, baseShare)
	}
}

// TestSharedSufferingMigration_RiderValuesMatchConfig guards against the
// rider JSON's authored literals (select_targets' TargetQueryDef.Radius and
// deal_damage's amountMult — see shared_suffering.json) silently drifting
// from the perk's own config block (radius / damageSharePercent). The
// characterization tests above derive their EXPECTED echo from
// config.damageSharePercent, so a damageSharePercent retune that forgets to
// update the rider's amountMult is already caught by them (both sides read
// the same config key, so they can't diverge without a test failure) — but
// a radius retune is only PARTIALLY caught there: the in-radius neighbor in
// buildSharedSufferingMigrationScene sits well inside the config radius, so
// those tests would keep passing even if the rider's authored radius (a
// literal, independent of config.radius) drifted away from it. This test
// closes that gap directly: it reads the perk's own AbilityRiders entry and
// asserts its authored Radius/amountMult equal the config block, so ANY
// future retune that edits only one side (config or rider JSON) fails
// loudly here, without needing a geometry precise enough to expose it.
func TestSharedSufferingMigration_RiderValuesMatchConfig(t *testing.T) {
	def := perkDefByID("shared_suffering")
	if def == nil {
		t.Fatal("shared_suffering perk def missing")
	}
	cfg := def.ConfigForRank("")
	wantRadius := cfg["radius"]
	wantSharePct := cfg["damageSharePercent"]
	if wantRadius <= 0 || wantSharePct <= 0 {
		t.Fatalf("shared_suffering config radius=%v damageSharePercent=%v, want both > 0", wantRadius, wantSharePct)
	}

	var rider *AbilityRider
	for i := range def.AbilityRiders {
		r := &def.AbilityRiders[i]
		if r.Target == "siphon_life" && r.Trigger == TriggerOnBeamTick {
			rider = r
			break
		}
	}
	if rider == nil {
		t.Fatal("shared_suffering has no AbilityRiders entry targeting siphon_life/on_beam_tick")
	}

	var gotRadius float64
	var foundSelectTargets bool
	var gotAmountMult float64
	var foundDealDamage bool
	for _, a := range rider.Actions {
		switch a.Type {
		case ActionSelectTargets:
			if a.Target == nil {
				t.Fatalf("rider's select_targets action %q has a nil Target query", a.ID)
			}
			gotRadius = a.Target.Radius
			foundSelectTargets = true
		case ActionDealDamage:
			var c dealDamageConfig
			if err := json.Unmarshal(a.Config, &c); err != nil {
				t.Fatalf("rider's deal_damage action %q: config decode failed: %v", a.ID, err)
			}
			gotAmountMult = c.AmountMult
			foundDealDamage = true
		}
	}
	if !foundSelectTargets {
		t.Fatal("rider has no select_targets action")
	}
	if !foundDealDamage {
		t.Fatal("rider has no deal_damage action")
	}

	if gotRadius != wantRadius {
		t.Errorf("rider select_targets Radius = %v, want %v (perk config.radius) — rider JSON and perk config have drifted apart", gotRadius, wantRadius)
	}
	if gotAmountMult != wantSharePct {
		t.Errorf("rider deal_damage amountMult = %v, want %v (perk config.damageSharePercent) — rider JSON and perk config have drifted apart", gotAmountMult, wantSharePct)
	}
}
