package game

// Section: Siphoner Gold perk unit tests.
//
// Covers:
//   - beam_mastery: Siphon Life damage / heal / mana cost / range scalers,
//     and the +chainAdditionalTargetBonus that fires when paired with
//     chain_siphon.
//   - ascended_corruption: adaptive enhancements for each of the four
//     Silver perks (chain_siphon, amplify_damage, dark_renewal,
//     shared_suffering). One test per silver perk pairing verifies the
//     matching effective-config helper returns the layered values.
//   - repurposed_life: on-death mana pulse when an enemy actively being
//     drained by this Siphoner dies. Verifies trigger conditions and that
//     allies (including the Siphoner) get mana restored.
//
// Setup mirrors newSiphonerSilverState — same Acolyte spawn, ranked to Gold
// instead so the gold-rank pool is the natural assignment target if a future
// test exercises that path.

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

func newSiphonerGoldState(t *testing.T) (s *GameState, siphoner, enemy *Unit) {
	t.Helper()
	s = NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x6017)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner = s.spawnPlayerUnitLocked("acolyte", "p1", "#9b59b6", protocol.Vec2{X: 400, Y: 400})
	siphoner.Visible = true
	siphoner.HP = siphoner.MaxHP
	siphoner.AttackRange = 1000
	siphoner.MaxMana = 200
	siphoner.CurrentMana = 200
	siphoner.ProgressionPath = "siphoner"
	siphoner.Rank = unitRankGold
	s.assignUnitPathAbilitiesLocked(siphoner)

	enemy = &Unit{
		ID:       s.nextUnitID,
		OwnerID:  enemyPlayerID,
		UnitType: "soldier",
		Visible:  true,
		X:        600, Y: 400,
		HP: 200, MaxHP: 200, Armor: 0,
		AttackRange: 100,
		AttackSpeed: 1.0,
		Damage:      10,
		MoveSpeed:   50,
		Color:       "#aa0000",
	}
	s.nextUnitID++
	s.addUnitLocked(enemy)
	return s, siphoner, enemy
}

// ─────────────────────────────────────────────────────────────────────────────
// beam_mastery
// ─────────────────────────────────────────────────────────────────────────────

func TestBeamMastery_ChannelModifiersComposeMultiplicatively(t *testing.T) {
	s, siphoner, _ := newSiphonerGoldState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Baseline: no perks → identity modifiers.
	mods := s.abilityScalarModifiersForCasterLocked(siphoner, "siphon_life")
	if mods.DamageMult != 1.0 || mods.HealMult != 1.0 || mods.ManaCostMult != 1.0 || mods.RangeMult != 1.0 {
		t.Fatalf("baseline modifiers should all be 1.0, got %+v", mods)
	}

	// With beam_mastery only.
	siphoner.PerkIDs = append(siphoner.PerkIDs, "beam_mastery")
	bm := perkDefByID("beam_mastery")
	wantDmg := bm.Config["damageMultiplier"]
	wantHeal := bm.Config["healingMultiplier"]
	wantMana := bm.Config["manaCostMultiplier"]
	wantRange := bm.Config["rangeMultiplier"]

	mods = s.abilityScalarModifiersForCasterLocked(siphoner, "siphon_life")
	if math.Abs(mods.DamageMult-wantDmg) > 1e-9 {
		t.Errorf("beam_mastery damage mult: got %.3f, want %.3f", mods.DamageMult, wantDmg)
	}
	if math.Abs(mods.HealMult-wantHeal) > 1e-9 {
		t.Errorf("beam_mastery heal mult: got %.3f, want %.3f", mods.HealMult, wantHeal)
	}
	if math.Abs(mods.ManaCostMult-wantMana) > 1e-9 {
		t.Errorf("beam_mastery mana cost mult: got %.3f, want %.3f", mods.ManaCostMult, wantMana)
	}
	if math.Abs(mods.RangeMult-wantRange) > 1e-9 {
		t.Errorf("beam_mastery range mult: got %.3f, want %.3f", mods.RangeMult, wantRange)
	}

	// Stacked with soul_leech: damage and heal multipliers compose multiplicatively.
	siphoner.PerkIDs = append(siphoner.PerkIDs, "soul_leech")
	sl := perkDefByID("soul_leech")
	mods = s.abilityScalarModifiersForCasterLocked(siphoner, "siphon_life")
	wantStackedDmg := wantDmg * sl.Config["damageMultiplier"]
	if math.Abs(mods.DamageMult-wantStackedDmg) > 1e-9 {
		t.Errorf("beam_mastery × soul_leech damage mult: got %.3f, want %.3f", mods.DamageMult, wantStackedDmg)
	}
}

func TestBeamMastery_AddsChainTargetWhenPairedWithChainSiphon(t *testing.T) {
	s, siphoner, primary := newSiphonerGoldState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "chain_siphon", "beam_mastery")
	cs := perkDefByID("chain_siphon")
	bm := perkDefByID("beam_mastery")
	chainRange := cs.Config["chainRange"]
	wantCount := int(cs.Config["additionalTargetCount"] + bm.Config["chainAdditionalTargetBonus"])

	// Spawn (wantCount + 2) enemies inside chainRange — the perk should pick exactly wantCount.
	for i := 0; i < wantCount+2; i++ {
		spawnEnemyAt(s, primary.X+chainRange*0.2*float64(i+1), primary.Y)
	}
	targets := s.chainSiphonTargetsLocked(siphoner, primary)
	if len(targets) != wantCount {
		t.Errorf("beam_mastery + chain_siphon target count = %d, want %d", len(targets), wantCount)
	}
}

func TestBeamMastery_ChainAdditionalTargetBonusInertWithoutChainSiphon(t *testing.T) {
	s, siphoner, primary := newSiphonerGoldState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Beam Mastery WITHOUT Chain Siphon: chain target count stays 0.
	siphoner.PerkIDs = append(siphoner.PerkIDs, "beam_mastery")
	spawnEnemyAt(s, primary.X+30, primary.Y)
	targets := s.chainSiphonTargetsLocked(siphoner, primary)
	if len(targets) != 0 {
		t.Errorf("beam_mastery alone should not summon chain targets, got %d", len(targets))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ascended_corruption — adaptive per-silver-perk enhancements
// ─────────────────────────────────────────────────────────────────────────────

func TestAscendedCorruption_ChainSiphonLayering(t *testing.T) {
	s, siphoner, _ := newSiphonerGoldState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "chain_siphon", "ascended_corruption")
	cs := perkDefByID("chain_siphon")
	asc := perkDefByID("ascended_corruption")
	cfg := s.chainSiphonEffectiveConfigLocked(siphoner)

	wantCount := cs.Config["additionalTargetCount"] + asc.Config["chainAdditionalTargetCountBonus"]
	wantRange := cs.Config["chainRange"] * asc.Config["chainRangeMultiplier"]
	wantDmg := cs.Config["secondaryDamageMultiplier"] + asc.Config["chainSecondaryDamageMultiplierBonus"]
	wantHeal := cs.Config["secondaryHealingMultiplier"] + asc.Config["chainSecondaryHealingMultiplierBonus"]

	if math.Abs(cfg["additionalTargetCount"]-wantCount) > 1e-9 {
		t.Errorf("chain additionalTargetCount: got %.2f, want %.2f", cfg["additionalTargetCount"], wantCount)
	}
	if math.Abs(cfg["chainRange"]-wantRange) > 1e-9 {
		t.Errorf("chain range: got %.2f, want %.2f", cfg["chainRange"], wantRange)
	}
	if math.Abs(cfg["secondaryDamageMultiplier"]-wantDmg) > 1e-9 {
		t.Errorf("chain secondary damage mult: got %.3f, want %.3f", cfg["secondaryDamageMultiplier"], wantDmg)
	}
	if math.Abs(cfg["secondaryHealingMultiplier"]-wantHeal) > 1e-9 {
		t.Errorf("chain secondary heal mult: got %.3f, want %.3f", cfg["secondaryHealingMultiplier"], wantHeal)
	}
}

func TestAscendedCorruption_AmplifyDamageLayering(t *testing.T) {
	s, siphoner, _ := newSiphonerGoldState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "amplify_damage", "ascended_corruption")
	ad := perkDefByID("amplify_damage")
	asc := perkDefByID("ascended_corruption")
	cfg := s.amplifyDamageEffectiveConfigLocked(siphoner)

	if got, want := cfg["radius"], ad.Config["radius"]*asc.Config["amplifyRadiusMultiplier"]; math.Abs(got-want) > 1e-9 {
		t.Errorf("amplify radius: got %.2f, want %.2f", got, want)
	}
	if got, want := cfg["durationSeconds"], ad.Config["durationSeconds"]*asc.Config["amplifyDurationMultiplier"]; math.Abs(got-want) > 1e-9 {
		t.Errorf("amplify duration: got %.2f, want %.2f", got, want)
	}
	if got, want := cfg["damageTakenMultiplier"], ad.Config["damageTakenMultiplier"]+asc.Config["amplifyDamageTakenMultiplierBonus"]; math.Abs(got-want) > 1e-9 {
		t.Errorf("amplify damage taken mult: got %.3f, want %.3f", got, want)
	}
}

func TestAscendedCorruption_DarkRenewalRaisesCapsAndAllyCount(t *testing.T) {
	s, siphoner, _ := newSiphonerGoldState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "dark_renewal", "ascended_corruption")
	dr := perkDefByID("dark_renewal")
	asc := perkDefByID("ascended_corruption")
	cfg := s.darkRenewalEffectiveConfigLocked(siphoner)

	if got, want := cfg["maxSelfShield"], dr.Config["maxSelfShield"]+asc.Config["darkMaxSelfShieldBonus"]; math.Abs(got-want) > 1e-9 {
		t.Errorf("dark maxSelfShield: got %.0f, want %.0f", got, want)
	}
	if got, want := cfg["maxAllyShield"], dr.Config["maxAllyShield"]+asc.Config["darkMaxAllyShieldBonus"]; math.Abs(got-want) > 1e-9 {
		t.Errorf("dark maxAllyShield: got %.0f, want %.0f", got, want)
	}
	wantAllyCount := 1.0 + asc.Config["darkAdditionalAllyShieldTargets"]
	if got := cfg["allyTargetCount"]; math.Abs(got-wantAllyCount) > 1e-9 {
		t.Errorf("dark allyTargetCount: got %.0f, want %.0f", got, wantAllyCount)
	}
}

func TestAscendedCorruption_DarkRenewalSpillsToMultipleAllies(t *testing.T) {
	s, siphoner, _ := newSiphonerGoldState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "dark_renewal", "ascended_corruption")
	cfg := s.darkRenewalEffectiveConfigLocked(siphoner)
	maxSelf := int(cfg["maxSelfShield"])
	maxAlly := int(cfg["maxAllyShield"])
	allyCount := int(cfg["allyTargetCount"])
	if allyCount < 2 {
		t.Fatalf("setup expected allyTargetCount >= 2, got %d", allyCount)
	}

	// Spawn `allyCount` allies inside the ally radius so every slot has a recipient.
	allies := make([]*Unit, 0, allyCount)
	for i := 0; i < allyCount; i++ {
		allies = append(allies, spawnAllyAt(s, siphoner.X+10+float64(i*5), siphoner.Y))
	}

	siphoner.HP = siphoner.MaxHP
	// Pour enough excess to fill self + every ally.
	available := maxSelf + maxAlly*allyCount + 10
	banked := s.applyDarkRenewalExcessLocked(siphoner, available, cfg["allyRadius"])

	wantBanked := maxSelf + maxAlly*allyCount
	if banked != wantBanked {
		t.Errorf("multi-ally banked = %d, want %d (self+%d×ally)", banked, wantBanked, allyCount)
	}
	// Every ally should be at cap.
	for i, ally := range allies {
		if got := totalShieldFromPoolsLocked(ally); got != maxAlly {
			t.Errorf("ally %d shield = %d, want %d (cap)", i, got, maxAlly)
		}
	}
}

func TestAscendedCorruption_SharedSufferingLayering(t *testing.T) {
	s, siphoner, _ := newSiphonerGoldState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "shared_suffering", "ascended_corruption")
	ss := perkDefByID("shared_suffering")
	asc := perkDefByID("ascended_corruption")
	cfg := s.sharedSufferingEffectiveConfigLocked(siphoner)

	if got, want := cfg["radius"], ss.Config["radius"]*asc.Config["sharedRadiusMultiplier"]; math.Abs(got-want) > 1e-9 {
		t.Errorf("shared radius: got %.2f, want %.2f", got, want)
	}
	if got, want := cfg["damageSharePercent"], ss.Config["damageSharePercent"]+asc.Config["sharedDamageSharePercentBonus"]; math.Abs(got-want) > 1e-9 {
		t.Errorf("shared share%%: got %.3f, want %.3f", got, want)
	}
}

func TestAscendedCorruption_DoesNothingWithoutSilverPerk(t *testing.T) {
	s, siphoner, _ := newSiphonerGoldState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Owning only ascended_corruption with no Silver perk: every helper still
	// returns base config unchanged (because the consumer ALSO requires the
	// Silver perk to be owned before doing any work).
	siphoner.PerkIDs = append(siphoner.PerkIDs, "ascended_corruption")

	// chain_siphon helper still layers, but the chain runtime never fires
	// because the consumer gates on perk ownership. Sanity: effective config
	// has the bonus, but the upstream chain function bails.
	primary := spawnEnemyAt(s, siphoner.X+50, siphoner.Y)
	spawnEnemyAt(s, primary.X+30, primary.Y)
	targets := s.chainSiphonTargetsLocked(siphoner, primary)
	if len(targets) != 0 {
		t.Errorf("chain targets should be empty without chain_siphon perk, got %d", len(targets))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// repurposed_life
// ─────────────────────────────────────────────────────────────────────────────

// spendsMana drops `unit`'s mana by `amount` (clamped). Used by tests that
// want to verify a restore lands real mana rather than no-op'ing on a full pool.
func spendsMana(unit *Unit, amount int) {
	unit.CurrentMana -= amount
	if unit.CurrentMana < 0 {
		unit.CurrentMana = 0
	}
}

// TestRepurposedLife_TriggersWhenSiphonerOwnTickKills exercises the
// scenario the channel-stop hook was added to fix: the Siphoner's own
// Siphon Life tick lands the killing blow. Before the fix, the channel
// auto-stopped at the post-validate inside tickUnitChannelLocked BEFORE
// drainPendingDeathsLocked ran, so the channel-target back-reference the
// drain hook looked for was already cleared — the perk silently missed
// every kill the Siphoner delivered themselves.
func TestRepurposedLife_TriggersWhenSiphonerOwnTickKills(t *testing.T) {
	s, siphoner, enemy := newSiphonerGoldState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "repurposed_life")
	def := perkDefByID("repurposed_life")
	amount := int(def.Config["manaRestoreAmount"])

	ally := spawnAllyAt(s, siphoner.X+100, siphoner.Y)
	ally.MaxMana = 100
	ally.CurrentMana = 20
	spendsMana(siphoner, 50)
	startAlly := ally.CurrentMana
	startSiphoner := siphoner.CurrentMana

	// Start the channel through the real entry point so the post-validate
	// path inside tickUnitChannelLocked runs end-to-end.
	enemy.HP = 1 // one-shot kill on the very first tick
	ok, reason := s.beginAbilityChannelLocked(siphoner, "siphon_life", enemy)
	if !ok {
		t.Fatalf("beginAbilityChannelLocked failed: %s", reason)
	}

	// Drive one full channel tick. The damage applies, target.HP -> 0,
	// post-validate fails, stopUnitChannelLocked runs, clearChannelState-
	// Locked fires repurposed_life BEFORE clearing the channel fields.
	s.tickUnitChannelLocked(siphoner, siphoner.ChannelTickInterval)
	s.drainPendingDeathsLocked()

	if enemy.HP > 0 {
		t.Fatalf("expected enemy to die from siphon tick; HP %d", enemy.HP)
	}
	// Ally is the cleanest authoritative signal: they don't pay mana cost
	// or spend mana for the channel tick, so their delta is exactly the
	// perk's restore amount (no other mana sources active in this test).
	if got := ally.CurrentMana - startAlly; got != amount {
		t.Errorf("ally mana on Siphoner-killing-blow: got +%d, want +%d", got, amount)
	}
	// The Siphoner's own delta nets out to (restore − ManaCostPerTick) for
	// the killing tick (they paid 1 mana to cast the tick that triggered
	// the restore). Use a lower bound that accounts for any tick mana cost
	// rather than asserting the full amount.
	// abilityMechanicsShadow recovers ManaCostPerTick off the compiled
	// Program — the live siphon_life catalog def is schemaVersion:2, so its
	// own flat ManaCostPerTick field is cleared (see ConvertLegacyAbility).
	rawChannelCfg, _ := getAbilityDef("siphon_life")
	channelCfg := abilityMechanicsShadow(rawChannelCfg)
	expectedSiphonerGain := amount - channelCfg.ManaCostPerTick
	if got := siphoner.CurrentMana - startSiphoner; got < expectedSiphonerGain {
		t.Errorf("siphoner mana on Siphoner-killing-blow: got +%d, want >= +%d (restore %d − tick cost %d)",
			got, expectedSiphonerGain, amount, channelCfg.ManaCostPerTick)
	}
}

func TestRepurposedLife_TriggersOnSiphonedVictimDeath(t *testing.T) {
	s, siphoner, enemy := newSiphonerGoldState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "repurposed_life")
	def := perkDefByID("repurposed_life")
	amount := int(def.Config["manaRestoreAmount"])

	// Spawn an ally inside the (default 500) radius. Drain mana on both so
	// the restore has room to land.
	ally := spawnAllyAt(s, siphoner.X+100, siphoner.Y)
	ally.MaxMana = 100
	ally.CurrentMana = 20
	spendsMana(siphoner, 50)
	startAlly := ally.CurrentMana
	startSiphoner := siphoner.CurrentMana

	// Wire the Siphoner to actively channel Siphon Life on the enemy.
	siphoner.ChannelAbilityID = "siphon_life"
	siphoner.ChannelTargetID = enemy.ID

	// Kill the enemy through the canonical damage path so the death pipeline
	// runs (including onSiphonVictimDeathLocked).
	enemy.HP = 1
	s.applyUnitDamageWithSourceLocked(enemy, 999, DamageSource{
		AttackerUnitID: siphoner.ID,
		Kind:           "ability",
	})
	s.drainPendingDeathsLocked()

	if got := ally.CurrentMana - startAlly; got != amount {
		t.Errorf("ally mana restored: got +%d, want +%d", got, amount)
	}
	if got := siphoner.CurrentMana - startSiphoner; got != amount {
		t.Errorf("siphoner mana restored: got +%d, want +%d (self should be included)", got, amount)
	}
}

func TestRepurposedLife_NoFireWhenVictimNotBeingSiphoned(t *testing.T) {
	s, siphoner, enemy := newSiphonerGoldState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "repurposed_life")
	ally := spawnAllyAt(s, siphoner.X+100, siphoner.Y)
	ally.MaxMana = 100
	ally.CurrentMana = 20
	startAlly := ally.CurrentMana

	// Siphoner is NOT channeling — kill the enemy anyway. No mana should land.
	enemy.HP = 1
	s.applyUnitDamageWithSourceLocked(enemy, 999, DamageSource{
		AttackerUnitID: siphoner.ID,
		Kind:           "ability",
	})
	s.drainPendingDeathsLocked()

	if ally.CurrentMana != startAlly {
		t.Errorf("ally mana changed despite no active siphon: %d -> %d", startAlly, ally.CurrentMana)
	}
}

func TestRepurposedLife_RespectsRadiusAndManaCap(t *testing.T) {
	s, siphoner, enemy := newSiphonerGoldState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "repurposed_life")
	def := perkDefByID("repurposed_life")
	radius := def.Config["radius"]
	amount := int(def.Config["manaRestoreAmount"])

	// One ally inside radius (drained), one outside (drained), one inside but
	// already capped (mana == max — restore should clamp).
	insideDrained := spawnAllyAt(s, siphoner.X+radius*0.5, siphoner.Y)
	insideDrained.MaxMana = 100
	insideDrained.CurrentMana = 0
	outside := spawnAllyAt(s, siphoner.X+radius*2, siphoner.Y)
	outside.MaxMana = 100
	outside.CurrentMana = 0
	insideFull := spawnAllyAt(s, siphoner.X+radius*0.3, siphoner.Y)
	insideFull.MaxMana = amount // already at MaxMana; cannot gain more
	insideFull.CurrentMana = amount

	siphoner.ChannelAbilityID = "siphon_life"
	siphoner.ChannelTargetID = enemy.ID
	enemy.HP = 1
	s.applyUnitDamageWithSourceLocked(enemy, 999, DamageSource{
		AttackerUnitID: siphoner.ID,
		Kind:           "ability",
	})
	s.drainPendingDeathsLocked()

	if insideDrained.CurrentMana != amount {
		t.Errorf("inside-drained ally: got %d, want %d", insideDrained.CurrentMana, amount)
	}
	if outside.CurrentMana != 0 {
		t.Errorf("outside-radius ally should not gain mana, got %d", outside.CurrentMana)
	}
	if insideFull.CurrentMana != amount {
		t.Errorf("capped ally should stay at max %d, got %d", amount, insideFull.CurrentMana)
	}
}

func TestRepurposedLife_OnlyOwningSiphonerFires(t *testing.T) {
	s, siphonerA, enemy := newSiphonerGoldState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// siphonerA owns the perk; siphonerB does NOT but is also channeling.
	// Only A's mana pulse should fire (not 2× from B too).
	siphonerA.PerkIDs = append(siphonerA.PerkIDs, "repurposed_life")
	siphonerA.ChannelAbilityID = "siphon_life"
	siphonerA.ChannelTargetID = enemy.ID

	siphonerB := spawnAllyAt(s, siphonerA.X+50, siphonerA.Y)
	siphonerB.ChannelAbilityID = "siphon_life"
	siphonerB.ChannelTargetID = enemy.ID
	siphonerB.MaxMana = 100
	siphonerB.CurrentMana = 20

	def := perkDefByID("repurposed_life")
	amount := int(def.Config["manaRestoreAmount"])
	spendsMana(siphonerA, 50)
	startA := siphonerA.CurrentMana
	startB := siphonerB.CurrentMana

	enemy.HP = 1
	s.applyUnitDamageWithSourceLocked(enemy, 999, DamageSource{
		AttackerUnitID: siphonerA.ID,
		Kind:           "ability",
	})
	s.drainPendingDeathsLocked()

	// Both should get exactly one pulse (from A's perk).
	if got := siphonerA.CurrentMana - startA; got != amount {
		t.Errorf("siphonerA mana: got +%d, want +%d", got, amount)
	}
	if got := siphonerB.CurrentMana - startB; got != amount {
		t.Errorf("siphonerB mana: got +%d, want +%d (one pulse only)", got, amount)
	}
}

func TestAddUnitManaLocked_ClampsToMaxMana(t *testing.T) {
	s, _, _ := newSiphonerGoldState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	ally := spawnAllyAt(s, 100, 100)
	ally.MaxMana = 50
	ally.CurrentMana = 45

	// Restoring 10 should clamp at MaxMana — only 5 of 10 lands.
	gained := s.addUnitManaLocked(ally, 10)
	if gained != 5 {
		t.Errorf("addUnitManaLocked banked %d, want 5 (cap room)", gained)
	}
	if ally.CurrentMana != 50 {
		t.Errorf("ally mana = %d, want 50 (capped)", ally.CurrentMana)
	}

	// No-op on full pool.
	gained = s.addUnitManaLocked(ally, 100)
	if gained != 0 {
		t.Errorf("addUnitManaLocked on full pool banked %d, want 0", gained)
	}

	// No-op on dead unit.
	ally.HP = 0
	gained = s.addUnitManaLocked(ally, 100)
	if gained != 0 {
		t.Errorf("addUnitManaLocked on dead unit banked %d, want 0", gained)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Cast range — beam_mastery range scaler reaches the channel target check
// ─────────────────────────────────────────────────────────────────────────────

// ─────────────────────────────────────────────────────────────────────────────
// Diagnostic: shadow damage-type hints for the user's reported loadout
// (soul_leech + chain_siphon + ascended_corruption channeling siphon_life).
// Verifies the hint flows end-to-end: applyUnitDamageWithSourceLocked queues
// the hint, the per-tick queue holds it, and the snapshot ships it on the
// wire so the client can color the popup dark purple.
// ─────────────────────────────────────────────────────────────────────────────

func TestSiphonLife_PrimaryDamageEmitsShadowDamageTypeHint(t *testing.T) {
	s, siphoner, primary := newSiphonerGoldState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Exactly the loadout the user reported.
	siphoner.PerkIDs = append(siphoner.PerkIDs, "soul_leech", "chain_siphon", "ascended_corruption")

	// Start the channel manually so we can drive the channel tick deterministically.
	ok, reason := s.beginAbilityChannelLocked(siphoner, "siphon_life", primary)
	if !ok {
		t.Fatalf("beginAbilityChannelLocked failed: %s", reason)
	}

	// Drain any hints from the channel-start path.
	s.resetDamageTypeHintsThisTickLocked()

	primaryStartHP := primary.HP

	// Drive one channel tick worth of dt. The channel arms itself with
	// ChannelNextTickIn = tickInterval, so we need dt == tickInterval to
	// fire exactly one tick.
	s.tickUnitChannelLocked(siphoner, siphoner.ChannelTickInterval)

	primaryHPLoss := primaryStartHP - primary.HP
	if primaryHPLoss <= 0 {
		t.Fatalf("primary took no damage from siphon tick — channel not firing? HP %d -> %d",
			primaryStartHP, primary.HP)
	}

	// Find the primary's hint in the per-tick queue.
	var primaryHint *damageTypeHint
	for i := range s.damageTypeHintsThisTick {
		h := &s.damageTypeHintsThisTick[i]
		if h.UnitID == primary.ID {
			primaryHint = h
			break
		}
	}
	if primaryHint == nil {
		t.Fatalf("expected a damageTypeHint for primary target (id=%d), got none. Queue contents: %+v",
			primary.ID, s.damageTypeHintsThisTick)
	}
	if primaryHint.Variant != "shadow" {
		t.Errorf("primary hint variant = %q, want %q", primaryHint.Variant, "shadow")
	}
	if primaryHint.Damage != primaryHPLoss {
		t.Errorf("primary hint damage = %d, want %d (must equal HP-diff so client can exact-match)",
			primaryHint.Damage, primaryHPLoss)
	}

	// Verify it serialises into the snapshot wire format too.
	snap := s.snapshotDamageTypeHintsLocked()
	if len(snap) == 0 {
		t.Fatal("snapshotDamageTypeHintsLocked returned empty — hint never reaches the client")
	}
	var primaryInSnap bool
	for _, e := range snap {
		if e.UnitID == primary.ID && e.Variant == "shadow" && e.Damage == primaryHPLoss {
			primaryInSnap = true
			break
		}
	}
	if !primaryInSnap {
		t.Errorf("primary hint missing from snapshot output: %+v", snap)
	}
}

func TestSiphonLife_ChainSecondaryEmitsShadowDamageTypeHint(t *testing.T) {
	s, siphoner, primary := newSiphonerGoldState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "soul_leech", "chain_siphon", "ascended_corruption")

	// Spawn a chain victim within range of the primary so chain_siphon
	// picks it. Bounce range = effective chainRange (chain_siphon base ×
	// ascended_corruption's chainRangeMultiplier).
	cfg := s.chainSiphonEffectiveConfigLocked(siphoner)
	chainRange := cfg["chainRange"]
	chainVictim := spawnEnemyAt(s, primary.X+chainRange*0.5, primary.Y)

	ok, reason := s.beginAbilityChannelLocked(siphoner, "siphon_life", primary)
	if !ok {
		t.Fatalf("beginAbilityChannelLocked failed: %s", reason)
	}
	s.resetDamageTypeHintsThisTickLocked()

	chainStartHP := chainVictim.HP
	s.tickUnitChannelLocked(siphoner, siphoner.ChannelTickInterval)
	chainHPLoss := chainStartHP - chainVictim.HP
	if chainHPLoss <= 0 {
		t.Fatalf("chain victim took no damage — chain_siphon not firing? HP %d -> %d",
			chainStartHP, chainVictim.HP)
	}

	// Find chain victim's hint.
	var chainHint *damageTypeHint
	for i := range s.damageTypeHintsThisTick {
		h := &s.damageTypeHintsThisTick[i]
		if h.UnitID == chainVictim.ID {
			chainHint = h
			break
		}
	}
	if chainHint == nil {
		t.Fatalf("expected a damageTypeHint for chain victim (id=%d), got none. Queue: %+v",
			chainVictim.ID, s.damageTypeHintsThisTick)
	}
	if chainHint.Variant != "shadow" {
		t.Errorf("chain hint variant = %q, want %q", chainHint.Variant, "shadow")
	}
	if chainHint.Damage != chainHPLoss {
		t.Errorf("chain hint damage = %d, want %d (must equal HP-diff)", chainHint.Damage, chainHPLoss)
	}
}

func TestBeamMastery_RangeMultiplierReachesIsValidChannelTarget(t *testing.T) {
	s, siphoner, enemy := newSiphonerGoldState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Verify the channel range multiplier helper reports the perk's value
	// only when the ability is siphon_life (other channels return 1.0).
	siphoner.PerkIDs = append(siphoner.PerkIDs, "beam_mastery")
	bm := perkDefByID("beam_mastery")
	want := bm.Config["rangeMultiplier"]

	siphonDef, ok := getAbilityDef("siphon_life")
	if !ok {
		t.Fatal("siphon_life ability def missing")
	}
	if got := s.channelRangeMultiplierForCasterLocked(siphoner, siphonDef); math.Abs(got-want) > 1e-9 {
		t.Errorf("range mult for siphon_life: got %.3f, want %.3f", got, want)
	}

	// Position the enemy past base CastRange but within scaled range. The
	// scaled range check must accept this target while the unscaled check
	// (multiplier 1.0) would reject it.
	baseRange := siphonDef.CastRange.Resolve(siphoner)
	enemy.X = siphoner.X + baseRange*1.10 // 10% beyond base, well inside 1.25×

	if siphonDef.WithinCastRange(siphoner, enemy) {
		t.Fatal("setup: enemy should be OUTSIDE base cast range")
	}
	if !s.isValidChannelTargetLocked(siphoner, siphonDef, enemy) {
		t.Errorf("scaled range check should accept enemy inside %.2f× CastRange", want)
	}
}
