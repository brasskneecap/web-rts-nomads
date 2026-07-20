package game

import (
	"math"
	"testing"
)

// ═══════════════════════════════════════════════════════════════════════════
// mark_of_weakness perk → ability migration — characterization tests.
//
// PHASE B (this revision, post-migration): drives every scenario through the
// REAL granted ability (catalog/abilities/mark_of_weakness) — manual cast via
// beginAbilityCastLocked, and the action-bar auto-cast loop
// (tickUnitAutoCastLocked) for the autonomous-fire scenarios — instead of the
// deleted bespoke applyMarkOfWeaknessAoELocked / tickMarkOfWeaknessPerkLocked
// / PerkState.MarkOfWeakness* fields. Every expected value is still derived
// from the mark_of_weakness PERK's Config map (unchanged by the migration;
// the ability's JSON bakes the same numbers) — never a hardcoded literal —
// so this file's pinned values are directly comparable to the pre-migration
// (Phase A) baseline that was green before this change landed.
//
// See the migration's final report for the full byte-identical-vs-normalized
// breakdown. Short version: debuff values (armor, healing-received, radius,
// duration, mana cost, refresh-not-stack semantics) are byte-identical.
// Auto-cast cadence/targeting and in-combat gating are NORMALIZED — see
// TestMarkOfWeakness_Migration_AutoCast_* below for what changed and why.
// ═══════════════════════════════════════════════════════════════════════════

const markOfWeaknessAbilityID = "mark_of_weakness"

// markOfWeaknessCfg returns the mark_of_weakness PERK's live Config map — the
// single source of truth for every pinned value in this file (radius,
// durationSeconds, armorReduction, healingReceivedMultiplier, manaCost,
// castRange, cooldownSeconds). The perk's Config is unchanged by the
// migration; only its DRIVER moved from bespoke Go to a granted ability whose
// JSON bakes these same numbers.
func markOfWeaknessCfg(t *testing.T) map[string]float64 {
	t.Helper()
	def := perkDefByID("mark_of_weakness")
	if def == nil {
		t.Fatal("mark_of_weakness perk def missing")
	}
	return def.Config
}

// spawnBareUnit spawns a minimal hand-built unit (mirrors the enemy2/closer
// literals already used in siphoner_perks_test.go) and registers it via
// addUnitLocked so getUnitByIDLocked can resolve it. Caller holds s.mu.
func spawnBareUnit(s *GameState, ownerID string, x, y float64, visible bool) *Unit {
	u := &Unit{
		ID:      s.nextUnitID,
		OwnerID: ownerID,
		Visible: visible,
		HP:      100, MaxHP: 100,
		X: x, Y: y,
	}
	s.nextUnitID++
	s.addUnitLocked(u)
	return u
}

// grantMarkOfWeaknessAbility attaches the mark_of_weakness perk to siphoner
// and recomputes its Abilities slice (PerkDef.GrantsAbilities → Abilities,
// path_ability_defs.go Step 4) so the granted ability actually appears on the
// unit — the real production wiring, not a test shortcut. Caller holds s.mu.
func grantMarkOfWeaknessAbility(s *GameState, siphoner *Unit) {
	siphoner.PerkIDs = append(siphoner.PerkIDs, "mark_of_weakness")
	s.assignUnitPathAbilitiesLocked(siphoner)
}

// castMarkOfWeakness drives a manual cast of the granted ability at target,
// failing the test if the cast is rejected. Manual cast (not auto-cast) is
// used for the debuff-value/AoE-shape/refresh scenarios below so they are
// deterministic and independent of the auto-cast target selector — the
// selector itself gets its own dedicated tests further down.
func castMarkOfWeakness(t *testing.T, s *GameState, siphoner, target *Unit) {
	t.Helper()
	ok, reason := s.beginAbilityCastLocked(siphoner, markOfWeaknessAbilityID, target)
	if !ok {
		t.Fatalf("cast failed: %s", reason)
	}
}

// ── AoE shape: radius, visibility, allegiance ───────────────────────────────

func TestMarkOfWeakness_Migration_PulseMarksOnlyVisibleHostilesInRadius(t *testing.T) {
	s, siphoner, anchor := newSiphonerBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantMarkOfWeaknessAbility(s, siphoner)
	cfg := markOfWeaknessCfg(t)
	radius := cfg["radius"]

	inRadius := spawnBareUnit(s, enemyPlayerID, anchor.X+radius*0.5, anchor.Y, true)
	outRadius := spawnBareUnit(s, enemyPlayerID, anchor.X+radius+50, anchor.Y, true)
	invisibleHostile := spawnBareUnit(s, enemyPlayerID, anchor.X+radius*0.25, anchor.Y, false)
	ally := spawnBareUnit(s, siphoner.OwnerID, anchor.X+radius*0.25, anchor.Y+5, true)

	castMarkOfWeakness(t, s, siphoner, anchor)

	marked := func(u *Unit) bool { return s.unitHasActiveAbilityStatusLocked(u.ID, markOfWeaknessAbilityID) }

	if !marked(anchor) {
		t.Error("the AoE anchor itself (the cast target) should be marked")
	}
	if !marked(inRadius) {
		t.Error("hostile inside radius should be marked")
	}
	if marked(outRadius) {
		t.Error("hostile outside radius should NOT be marked")
	}
	if marked(invisibleHostile) {
		t.Error("invisible hostile should NOT be marked")
	}
	if marked(ally) {
		t.Error("allied unit should NOT be marked")
	}
}

// TestMarkOfWeakness_Migration_AoEAnchorsOnTargetNotCaster proves the AoE is
// centered on the Siphoner's CAST TARGET (the anchor), not the Siphoner's own
// position — the program's select_targets action uses
// origin:"initial_target_position", not "caster".
func TestMarkOfWeakness_Migration_AoEAnchorsOnTargetNotCaster(t *testing.T) {
	s, siphoner, anchor := newSiphonerBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantMarkOfWeaknessAbility(s, siphoner)
	cfg := markOfWeaknessCfg(t)
	radius := cfg["radius"]

	// Siphoner sits at (400,400); anchor sits at (600,400) (200 apart, per
	// newSiphonerBronzeState). Put one hostile near the CASTER only, and one
	// near the ANCHOR only, both well outside the other point's radius.
	nearCasterOnly := spawnBareUnit(s, enemyPlayerID, siphoner.X+radius*0.5, siphoner.Y, true)
	nearAnchorOnly := spawnBareUnit(s, enemyPlayerID, anchor.X+radius*0.5, anchor.Y, true)

	castMarkOfWeakness(t, s, siphoner, anchor)

	if s.unitHasActiveAbilityStatusLocked(nearCasterOnly.ID, markOfWeaknessAbilityID) {
		t.Error("a hostile near the CASTER (not the anchor/target) must not be marked — AoE centers on the target")
	}
	if !s.unitHasActiveAbilityStatusLocked(nearAnchorOnly.ID, markOfWeaknessAbilityID) {
		t.Error("a hostile near the ANCHOR/target must be marked — AoE centers on the target")
	}
}

// ── Debuff values: armor + healing-received (the double-apply regression guard) ──

// TestMarkOfWeakness_Migration_EffectiveArmorAndHealingReceived is the
// DOUBLE-APPLY regression guard called out by the migration brief: the
// bespoke markOfWeaknessArmorReductionLocked / markOfWeaknessHealingReceived-
// MultiplierLocked reads were deleted from effectiveArmorLocked / healUnit-
// Locked in the SAME change that made mark_of_weakness author an
// apply_status(armor, healingReceived) — so armor drops by EXACTLY the
// configured reduction (not double it), and a heal lands at EXACTLY the
// configured multiplier (not multiplier squared).
func TestMarkOfWeakness_Migration_EffectiveArmorAndHealingReceived(t *testing.T) {
	s, siphoner, anchor := newSiphonerBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantMarkOfWeaknessAbility(s, siphoner)
	cfg := markOfWeaknessCfg(t)
	armorReduction := int(math.Round(cfg["armorReduction"]))
	healMult := cfg["healingReceivedMultiplier"]

	armorBefore := s.effectiveArmorLocked(anchor)

	castMarkOfWeakness(t, s, siphoner, anchor)

	armorAfter := s.effectiveArmorLocked(anchor)
	wantArmorAfter := armorBefore - armorReduction
	if wantArmorAfter < 0 {
		wantArmorAfter = 0
	}
	if armorAfter != wantArmorAfter {
		t.Errorf("effective armor after mark: got %d, want %d (before %d, reduction %d — exactly ONE application, not double: %d would mean double-apply)",
			armorAfter, wantArmorAfter, armorBefore, armorReduction, armorBefore-2*armorReduction)
	}

	anchor.HP = 1
	anchor.MaxHP = 1000
	s.healUnitLocked(anchor, 100)
	wantGain := int(math.Round(100 * healMult))
	squaredGain := int(math.Round(100 * healMult * healMult))
	if got := anchor.HP - 1; got != wantGain {
		t.Errorf("healing landed: got +%d HP, want +%d (mult %.2f — exactly ONE application; a squared %d would mean double-apply)",
			got, wantGain, healMult, squaredGain)
	}
}

// ── Expiry ───────────────────────────────────────────────────────────────────

func TestMarkOfWeakness_Migration_ExpiresAfterDuration(t *testing.T) {
	s, siphoner, anchor := newSiphonerBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantMarkOfWeaknessAbility(s, siphoner)
	cfg := markOfWeaknessCfg(t)
	duration := cfg["durationSeconds"]

	armorBefore := s.effectiveArmorLocked(anchor)
	castMarkOfWeakness(t, s, siphoner, anchor)
	if s.effectiveArmorLocked(anchor) == armorBefore {
		t.Fatal("setup: mark should be reducing armor right after landing")
	}
	if len(s.AbilityStatuses) != 1 {
		t.Fatalf("setup: expected exactly 1 AbilityStatus, got %d", len(s.AbilityStatuses))
	}

	// Advance the REAL status-decay loop (ability_status.go) past the full
	// duration in one shot.
	s.tickAbilityStatusesLocked(duration + 0.1)

	if len(s.AbilityStatuses) != 0 {
		t.Errorf("status should have expired and been removed, %d remaining", len(s.AbilityStatuses))
	}
	if got := s.effectiveArmorLocked(anchor); got != armorBefore {
		t.Errorf("armor after expiry: got %d, want %d (back to baseline)", got, armorBefore)
	}
	anchor.HP = 1
	anchor.MaxHP = 1000
	s.healUnitLocked(anchor, 100)
	if anchor.HP-1 != 100 {
		t.Errorf("healing after expiry should land at full value: got +%d, want +100", anchor.HP-1)
	}
}

// ── Refresh semantics: overlapping casts refresh, not stack ────────────────

func TestMarkOfWeakness_Migration_OverlappingCastsRefreshNotDouble(t *testing.T) {
	s, siphoner, anchor := newSiphonerBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantMarkOfWeaknessAbility(s, siphoner)
	cfg := markOfWeaknessCfg(t)
	duration := cfg["durationSeconds"]
	cooldown := cfg["cooldownSeconds"]

	castMarkOfWeakness(t, s, siphoner, anchor)
	if len(s.AbilityStatuses) != 1 {
		t.Fatalf("expected exactly 1 AbilityStatus after first cast, got %d", len(s.AbilityStatuses))
	}
	armorAfterOnePulse := s.effectiveArmorLocked(anchor)

	// Let some time pass (not enough to expire) then re-cast: the ability's
	// own cooldown must clear first (armed at cfg["cooldownSeconds"] on cast).
	s.AbilityStatuses[0].Remaining -= 1.0
	siphoner.AbilityCooldowns[markOfWeaknessAbilityID] = 0
	siphoner.GlobalCooldownRemaining = 0
	castMarkOfWeakness(t, s, siphoner, anchor)

	if len(s.AbilityStatuses) != 1 {
		t.Errorf("overlapping cast must REFRESH the single existing status, not stack a second one: got %d AbilityStatuses", len(s.AbilityStatuses))
	}
	if got := s.effectiveArmorLocked(anchor); got != armorAfterOnePulse {
		t.Errorf("overlapping cast changed effective armor: got %d, want %d (refresh must not stack the flat reduction, only reset Remaining)",
			got, armorAfterOnePulse)
	}
	if s.AbilityStatuses[0].Remaining < duration {
		t.Errorf("overlapping cast should refresh Remaining back to full duration, got %.2f want >= %.2f", s.AbilityStatuses[0].Remaining, duration)
	}
	_ = cooldown
}

// ── Mana gating ──────────────────────────────────────────────────────────────

func TestMarkOfWeakness_Migration_ManaGating(t *testing.T) {
	s, siphoner, anchor := newSiphonerBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantMarkOfWeaknessAbility(s, siphoner)
	cfg := markOfWeaknessCfg(t)
	manaCost := int(cfg["manaCost"])

	siphoner.CurrentMana = manaCost - 1 // strictly insufficient
	ok, reason := s.beginAbilityCastLocked(siphoner, markOfWeaknessAbilityID, anchor)

	if ok {
		t.Fatal("cast should be rejected with insufficient mana")
	}
	if reason != castFailNotEnoughMana {
		t.Errorf("rejection reason = %q, want %q", reason, castFailNotEnoughMana)
	}
	if siphoner.CurrentMana != manaCost-1 {
		t.Error("mana should not be spent when the cast is rejected")
	}
	if len(s.AbilityStatuses) != 0 {
		t.Error("no status should be spawned when the cast is rejected for insufficient mana")
	}
}

func TestMarkOfWeakness_Migration_ManaSpentOnSuccessfulCast(t *testing.T) {
	s, siphoner, anchor := newSiphonerBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantMarkOfWeaknessAbility(s, siphoner)
	cfg := markOfWeaknessCfg(t)
	manaCost := int(cfg["manaCost"])
	manaBefore := siphoner.CurrentMana

	castMarkOfWeakness(t, s, siphoner, anchor)

	if siphoner.CurrentMana != manaBefore-manaCost {
		t.Errorf("mana spent: got %d, want %d", manaBefore-siphoner.CurrentMana, manaCost)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Auto-cast wiring — replaces the deleted bespoke
// tickMarkOfWeaknessPerkLocked driver. NORMALIZED vs. the old driver in a
// few ways documented per-test below; see the migration report for the full
// list.
// ═══════════════════════════════════════════════════════════════════════════

// TestMarkOfWeakness_Migration_AutoCast_FiresOnClosestEnemyInRange proves the
// granted ability auto-casts by default (DefaultAutoCast:true in the
// catalog) and picks a target via the "closest_enemy_in_range" selector —
// the auto-cast target selector registry has no "prefer current channel
// target" selector (siphonerAfflictionAnchorLocked, the old driver's
// anchor-picking helper, is Siphoner-Bronze-specific and still used by
// lingering_hex/amplify_damage, but auto-cast selectors are a separate,
// ability-generic registry — see autocast_selectors.go). This is a
// NORMALIZED targeting rule: the old driver preferred the Siphoner's ACTIVE
// Siphon Life channel target over a closer enemy; the ability always picks
// the closest enemy in range regardless of channel state.
func TestMarkOfWeakness_Migration_AutoCast_FiresOnClosestEnemyInRange(t *testing.T) {
	s, siphoner, farEnemy := newSiphonerBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantMarkOfWeaknessAbility(s, siphoner)

	closer := spawnBareUnit(s, enemyPlayerID, siphoner.X+50, siphoner.Y, true)

	// Pretend the Siphoner is channeling the FAR enemy — under the OLD
	// bespoke driver this would have anchored the AoE on farEnemy despite
	// closer being nearer. The ability's auto-cast selector ignores this.
	siphoner.ChannelTargetID = farEnemy.ID

	s.tickUnitAutoCastLocked(siphoner)

	if len(s.AbilityStatuses) != 1 {
		t.Fatalf("expected auto-cast to fire exactly once, got %d statuses", len(s.AbilityStatuses))
	}
	if s.AbilityStatuses[0].TargetUnitID != closer.ID {
		t.Errorf("auto-cast target = %d, want the CLOSEST enemy (%d), not the channel target (%d)",
			s.AbilityStatuses[0].TargetUnitID, closer.ID, farEnemy.ID)
	}
}

// TestMarkOfWeakness_Migration_AutoCast_HoldsFireWithoutTarget mirrors the
// old driver's "no anchor in range -> hold fire, no mana spent, no cooldown
// armed" behavior — byte-identical in OUTCOME (nothing happens), reached via
// the generic auto-cast loop's own selector-returns-nil gate instead of
// siphonerAfflictionAnchorLocked.
func TestMarkOfWeakness_Migration_AutoCast_HoldsFireWithoutTarget(t *testing.T) {
	s, siphoner, enemy := newSiphonerBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantMarkOfWeaknessAbility(s, siphoner)
	enemy.X = siphoner.X + 9999 // out of the ability's castRange

	manaBefore := siphoner.CurrentMana
	s.tickUnitAutoCastLocked(siphoner)

	if len(s.AbilityStatuses) != 0 {
		t.Error("no cast should fire when no enemy is in range")
	}
	if siphoner.CurrentMana != manaBefore {
		t.Error("mana should not be spent when no cast fires")
	}
	if siphoner.AbilityCooldowns[markOfWeaknessAbilityID] > 0 {
		t.Error("cooldown should not arm when no cast fires")
	}
}

// TestMarkOfWeakness_Migration_AutoCast_HoldsFireWithoutMana mirrors the old
// driver's mana gate for the autonomous-fire path specifically.
func TestMarkOfWeakness_Migration_AutoCast_HoldsFireWithoutMana(t *testing.T) {
	s, siphoner, _ := newSiphonerBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantMarkOfWeaknessAbility(s, siphoner)
	siphoner.CurrentMana = 0

	s.tickUnitAutoCastLocked(siphoner)

	if len(s.AbilityStatuses) != 0 {
		t.Error("no cast should fire without enough mana")
	}
}

// TestMarkOfWeakness_Migration_AutoCast_SuppressedByMoveOrder documents a
// NORMALIZED divergence: the old bespoke driver (tickUnitPerkStateLocked)
// ran regardless of the Siphoner's current Order, so it could pulse Mark of
// Weakness while the unit was walking to a Move destination. The generic
// auto-cast loop (tickUnitAutoCastLocked) suppresses ALL auto-cast — this
// ability included — while OrderMove is active, so a Siphoner walking
// somewhere will NOT auto-cast Mark of Weakness mid-walk, even with a valid
// enemy in range. See ability_autocast.go's OrderMove guard doc comment.
func TestMarkOfWeakness_Migration_AutoCast_SuppressedByMoveOrder(t *testing.T) {
	s, siphoner, _ := newSiphonerBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantMarkOfWeaknessAbility(s, siphoner)
	siphoner.Order.Type = OrderMove

	s.tickUnitAutoCastLocked(siphoner)

	if len(s.AbilityStatuses) != 0 {
		t.Error("NORMALIZED BEHAVIOR CHANGE CHECK: auto-cast should be suppressed while the Siphoner has an active Move order (see test doc comment) — if this now fails, the normalization no longer holds and the report must be updated")
	}
}
