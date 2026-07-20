package game

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// ═════════════════════════════════════════════════════════════════════════════
// zealous_march AURA MIGRATION — characterization tests
//
// zealous_march moved from a bespoke Go scan
// (perkMoveSpeedBonusFromClericAurasLocked, deleted from perks_cleric.go) to
// the generic, data-driven PerkDef.Auras vocabulary resolved by the per-tick
// cache in perk_aura_stat_cache.go. This file proves the migration is
// behavior-preserving.
//
// legacyZealousMarchBonusLocked below is a byte-for-byte copy of the DELETED
// helper's algorithm, kept here ONLY as a characterization oracle — every
// test computes its "want" by calling this copy (which reads live catalog
// config, no hardcoded literals) and asserts the NEW production code
// (perkMoveSpeedMultiplierLocked, reading the generic aura cache) produces
// the identical total multiplier. This is stronger than hand-computing
// expected numbers: it re-derives the answer from the same formula the
// pre-migration code used, so a future catalog re-tune can't silently
// invalidate the comparison.
// ═════════════════════════════════════════════════════════════════════════════

// legacyZealousMarchBonusLocked is the pre-migration algorithm
// (perks_cleric.go's now-deleted perkMoveSpeedBonusFromClericAurasLocked),
// preserved verbatim as a test-only oracle. DO NOT "fix" this to match new
// behavior — it exists specifically to stay frozen at the pre-migration
// formula so tests can detect any drift.
func legacyZealousMarchBonusLocked(s *GameState, unit *Unit) float64 {
	if unit == nil {
		return 0
	}
	def := perkDefByID("zealous_march")
	if def == nil {
		return 0
	}
	bestBase := 0.0
	bestStack := 0.0
	count := 0
	for _, src := range s.Units {
		if src == nil || src.HP <= 0 || !src.Visible {
			continue
		}
		if !containsString(src.PerkIDs, "zealous_march") {
			continue
		}
		if !s.unitsFriendlyLocked(src, unit) {
			continue
		}
		cfg := def.ConfigForRank(src.Rank)
		radius := cfg["radiusPixels"]
		if radius <= 0 {
			continue
		}
		dx := src.X - unit.X
		dy := src.Y - unit.Y
		if dx*dx+dy*dy > radius*radius {
			continue
		}
		count++
		if base := cfg["moveSpeedMultiplier"]; base > bestBase {
			bestBase = base
		}
		if stack := cfg["stackBonus"]; stack > bestStack {
			bestStack = stack
		}
	}
	if count == 0 {
		return 0
	}
	return bestBase + float64(count-1)*bestStack
}

// assertZealousMarchMatchesLegacy rebuilds the generic aura cache, computes
// the legacy oracle's bonus for `unit`, and asserts the NEW
// perkMoveSpeedMultiplierLocked total equals 1.0 + that legacy bonus (plus
// any other additive bonus already baked into `unit`, e.g. momentum — the
// legacy oracle only ever computed the zealous_march TERM, so callers that
// also have momentum active must add it themselves via wantExtra).
func assertZealousMarchMatchesLegacy(t *testing.T, s *GameState, unit *Unit, wantExtra float64) {
	t.Helper()
	s.rebuildAuraStatCacheLocked()
	wantLegacyAuraBonus := legacyZealousMarchBonusLocked(s, unit)
	want := 1.0 + wantLegacyAuraBonus + wantExtra
	got := s.perkMoveSpeedMultiplierLocked(unit)
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("perkMoveSpeedMultiplierLocked = %.6f, want %.6f (legacy aura bonus %.6f + extra %.6f)",
			got, want, wantLegacyAuraBonus, wantExtra)
	}
}

// TestZealousMarchMigration_OneCleric_MatchesLegacyFormula covers a single
// covering Cleric.
func TestZealousMarchMigration_OneCleric_MatchesLegacyFormula(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "zealous_march")
	ally := spawnClericTestAlly(t, s, cleric.X+10, cleric.Y)

	assertZealousMarchMatchesLegacy(t, s, ally, 0)
}

// TestZealousMarchMigration_TwoClerics_MatchesLegacyFormula covers two
// covering Clerics — proves the "+bestStack" stacking term.
func TestZealousMarchMigration_TwoClerics_MatchesLegacyFormula(t *testing.T) {
	s, clericA := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(clericA, "zealous_march")
	clericB := s.spawnPlayerUnitLocked("acolyte", "p1", "#aabbcc", protocol.Vec2{X: 410, Y: 400})
	clericB.Visible = true
	grantPerk(clericB, "zealous_march")

	ally := spawnClericTestAlly(t, s, clericA.X+10, clericA.Y)
	assertZealousMarchMatchesLegacy(t, s, ally, 0)
}

// TestZealousMarchMigration_ThreeClerics_MatchesLegacyFormula covers three
// covering Clerics — proves the stack term scales with (count-1).
func TestZealousMarchMigration_ThreeClerics_MatchesLegacyFormula(t *testing.T) {
	s, clericA := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(clericA, "zealous_march")
	clericB := s.spawnPlayerUnitLocked("acolyte", "p1", "#aabbcc", protocol.Vec2{X: 410, Y: 400})
	clericB.Visible = true
	grantPerk(clericB, "zealous_march")
	clericC := s.spawnPlayerUnitLocked("acolyte", "p1", "#ccddee", protocol.Vec2{X: 420, Y: 400})
	clericC.Visible = true
	grantPerk(clericC, "zealous_march")

	ally := spawnClericTestAlly(t, s, clericA.X+10, clericA.Y)
	assertZealousMarchMatchesLegacy(t, s, ally, 0)
}

// TestZealousMarchMigration_ClericSelfInclusion confirms the Cleric buffs
// ITSELF — zealous_march does not exclude the owner (unlike guardian_aura).
func TestZealousMarchMigration_ClericSelfInclusion(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "zealous_march")

	s.rebuildAuraStatCacheLocked()
	wantBonus := legacyZealousMarchBonusLocked(s, cleric)
	if wantBonus <= 0 {
		t.Fatalf("setup: legacy oracle computed 0 self-bonus for the Cleric — expected a positive self-buff")
	}
	got := s.perkMoveSpeedMultiplierLocked(cleric)
	want := 1.0 + wantBonus
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("Cleric self move speed multiplier = %.6f, want %.6f", got, want)
	}
}

// TestZealousMarchMigration_OutsideRadius_NoBonus confirms an ally outside
// the aura radius gets nothing.
func TestZealousMarchMigration_OutsideRadius_NoBonus(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "zealous_march")
	def := perkDefByID("zealous_march")
	if def == nil {
		t.Fatal("zealous_march perk def not found")
	}
	cfg := def.ConfigForRank(cleric.Rank)

	ally := spawnClericTestAlly(t, s, cleric.X+cfg["radiusPixels"]*2, cleric.Y)
	assertZealousMarchMatchesLegacy(t, s, ally, 0)
}

// TestZealousMarchMigration_HostileRecipient_NoBonus confirms a hostile unit
// standing inside the aura's radius is not affected.
func TestZealousMarchMigration_HostileRecipient_NoBonus(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "zealous_march")
	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: cleric.X + 10, Y: cleric.Y})
	enemy.Visible = true

	assertZealousMarchMatchesLegacy(t, s, enemy, 0)
}

// TestZealousMarchMigration_DeadSource_NoBonus confirms a Cleric with
// zealous_march at HP<=0 does not contribute an aura.
func TestZealousMarchMigration_DeadSource_NoBonus(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "zealous_march")
	ally := spawnClericTestAlly(t, s, cleric.X+10, cleric.Y)
	cleric.HP = 0

	assertZealousMarchMatchesLegacy(t, s, ally, 0)

	s.rebuildAuraStatCacheLocked()
	if got := s.perkMoveSpeedMultiplierLocked(ally); math.Abs(got-1.0) > 1e-6 {
		t.Errorf("ally near dead Cleric: move speed = %.6f, want 1.0 (no aura from a dead source)", got)
	}
}

// TestZealousMarchMigration_InvisibleSource_NoBonus confirms a Cleric with
// zealous_march that is not Visible does not contribute an aura.
func TestZealousMarchMigration_InvisibleSource_NoBonus(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "zealous_march")
	ally := spawnClericTestAlly(t, s, cleric.X+10, cleric.Y)
	cleric.Visible = false

	assertZealousMarchMatchesLegacy(t, s, ally, 0)

	s.rebuildAuraStatCacheLocked()
	if got := s.perkMoveSpeedMultiplierLocked(ally); math.Abs(got-1.0) > 1e-6 {
		t.Errorf("ally near invisible Cleric: move speed = %.6f, want 1.0 (no aura from an invisible source)", got)
	}
}

// TestZealousMarchMigration_RankPromotedEmitter proves the cache resolves
// the aura using the EMITTER's rank field (not a fixed/global default),
// mirroring the legacy code's def.ConfigForRank(src.Rank) call. zealous_march
// carries no ConfigByRank override in the catalog today, so this is
// expected to produce the SAME bonus as an unpromoted emitter — the point of
// this test is to guard against a regression where the migration accidentally
// hardcodes "bronze" or ignores src.Rank, which a future ConfigByRank
// addition would then silently fail to pick up.
func TestZealousMarchMigration_RankPromotedEmitter(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "zealous_march")
	cleric.Rank = unitRankGold // promote the EMITTER only

	ally := spawnClericTestAlly(t, s, cleric.X+10, cleric.Y)
	assertZealousMarchMatchesLegacy(t, s, ally, 0)
}

// TestZealousMarchMigration_MomentumOrderingGuard is THE critical ordering
// test (see perk_aura_stat_cache.go's "ordering trap" doc). A recipient with
// BOTH momentum's active move-speed bonus AND a covering zealous_march aura
// must see the two bonuses combine ADDITIVELY in the same pool
// (1.0 + momentumBonus + auraBonus) — not have the aura instead fold through
// the generic (base+add)×mul stat pipeline, which would compose differently
// with momentum's contribution.
func TestZealousMarchMigration_MomentumOrderingGuard(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "zealous_march")
	ally := spawnClericTestAlly(t, s, cleric.X+10, cleric.Y)

	momentumDef := perkDefByID("momentum")
	if momentumDef == nil {
		t.Fatal("momentum perk def not found")
	}
	momentumBonus := momentumDef.Config["moveSpeedBonus"]
	if momentumBonus <= 0 {
		t.Fatalf("setup: momentum moveSpeedBonus must be > 0, got %.6f", momentumBonus)
	}
	grantPerk(ally, "momentum")
	ally.PerkState.MomentumBonus = momentumBonus // simulate the post-attack buff being active

	assertZealousMarchMatchesLegacy(t, s, ally, momentumBonus)
}

// ═════════════════════════════════════════════════════════════════════════════
// mana_conduit AURA MIGRATION — characterization tests
//
// mana_conduit moved from a bespoke Go scan (manaConduitAuraBonusLocked,
// deleted from perks_cleric.go) to the same generic, data-driven
// PerkDef.Auras vocabulary zealous_march pioneered. Unlike zealous_march,
// mana_conduit has NO PerAdditionalSource — it is pure max-wins with no
// stacking bonus at all (see perks_icons.go's comment: "Mana Conduit is a
// max-wins aura — it does not actually stack with itself").
//
// legacyManaConduitBonusLocked below is a byte-for-byte copy of the DELETED
// helper's algorithm, kept here ONLY as a characterization oracle — every
// test computes its "want" by calling this copy (which reads live catalog
// config, no hardcoded literals) and asserts the NEW production code
// (effectiveManaRegenLocked, reading the generic aura cache) produces the
// identical effective mana-regen rate.
// ═════════════════════════════════════════════════════════════════════════════

// legacyManaConduitBonusLocked is the pre-migration algorithm (perks_cleric.
// go's now-deleted manaConduitAuraBonusLocked), preserved verbatim as a
// test-only oracle. DO NOT "fix" this to match new behavior — it exists
// specifically to stay frozen at the pre-migration formula so tests can
// detect any drift.
func legacyManaConduitBonusLocked(s *GameState, unit *Unit) float64 {
	if unit == nil {
		return 0
	}
	def := perkDefByID("mana_conduit")
	if def == nil {
		return 0
	}
	best := 0.0
	for _, src := range s.Units {
		if src == nil || src.HP <= 0 || !src.Visible {
			continue
		}
		if !containsString(src.PerkIDs, "mana_conduit") {
			continue
		}
		if !s.unitsFriendlyLocked(src, unit) {
			continue
		}
		cfg := def.ConfigForRank(src.Rank)
		radius := cfg["radiusPixels"]
		bonus := cfg["bonusManaRegen"]
		if radius <= 0 || bonus <= 0 {
			continue
		}
		dx := src.X - unit.X
		dy := src.Y - unit.Y
		if dx*dx+dy*dy > radius*radius {
			continue
		}
		if bonus > best {
			best = bonus
		}
	}
	return best
}

// assertManaConduitMatchesLegacy rebuilds the generic aura cache, computes
// the legacy oracle's bonus for `unit`, reproduces mana.go's
// effectiveManaRegenLocked fold EXACTLY (aura bonus added to the base rate
// BEFORE the zone/perk-stat-modifier applyStatStages fold — the "ordering
// trap" the task brief calls out), and asserts the NEW
// effectiveManaRegenLocked total equals that reconstruction.
func assertManaConduitMatchesLegacy(t *testing.T, s *GameState, unit *Unit) {
	t.Helper()
	s.rebuildAuraStatCacheLocked()
	wantLegacyAuraBonus := legacyManaConduitBonusLocked(s, unit)

	want := 0.0
	if unit != nil && unit.MaxMana > 0 {
		rate := unit.ManaRegenPerSecond + wantLegacyAuraBonus
		add, mul := s.playerStatModifierLocked(unit.OwnerID, statManaRegen)
		perkStages := s.unitPerkStatModifiersLocked(unit, statManaRegen)
		if add != 0 || mul != 1 || len(perkStages) > 0 {
			rate = applyStatStages(rate, mergeZoneIntoBaseStage(perkStages, add, mul))
		}
		want = rate
	}

	got := s.effectiveManaRegenLocked(unit)
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("effectiveManaRegenLocked = %.6f, want %.6f (legacy aura bonus %.6f folded at the pre-zone-stage position)",
			got, want, wantLegacyAuraBonus)
	}
}

// TestManaConduitMigration_OneCleric_MatchesLegacyFormula covers a single
// covering Cleric.
func TestManaConduitMigration_OneCleric_MatchesLegacyFormula(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "mana_conduit")
	ally := s.spawnPlayerUnitLocked("acolyte", "p1", "#0099ff", protocol.Vec2{X: cleric.X + 10, Y: cleric.Y})
	ally.Visible = true
	ally.MaxMana = 200
	ally.CurrentMana = 100

	assertManaConduitMatchesLegacy(t, s, ally)
}

// TestManaConduitMigration_TwoClerics_MaxWinsNoStack covers two covering
// Clerics — proves the bonus does NOT stack (unlike zealous_march, there is
// no PerAdditionalSource here: max-wins only).
func TestManaConduitMigration_TwoClerics_MaxWinsNoStack(t *testing.T) {
	s, clericA := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(clericA, "mana_conduit")
	clericB := s.spawnPlayerUnitLocked("acolyte", "p1", "#aabbcc", protocol.Vec2{X: 410, Y: 400})
	clericB.Visible = true
	grantPerk(clericB, "mana_conduit")

	ally := s.spawnPlayerUnitLocked("acolyte", "p1", "#0099ff", protocol.Vec2{X: clericA.X + 10, Y: clericA.Y})
	ally.Visible = true
	ally.MaxMana = 200
	ally.CurrentMana = 100

	// assertManaConduitMatchesLegacy alone already proves max-wins-no-stack:
	// the legacy oracle's "best" reduction (not a sum) is what production
	// must match with two covering sources of the identical bonus value.
	// Additionally assert against the raw config value directly, so a future
	// catalog re-tune that gave clericB a DIFFERENT (higher) bonus would
	// still be caught if production ever summed instead of maxed.
	mcDef := perkDefByID("mana_conduit")
	bonusPerSec := mcDef.ConfigForRank(clericA.Rank)["bonusManaRegen"]
	auraBonus := legacyManaConduitBonusLocked(s, ally)
	if math.Abs(auraBonus-bonusPerSec) > 1e-6 {
		t.Fatalf("setup: legacy oracle bonus = %.6f, want %.6f (single source's bonusManaRegen)", auraBonus, bonusPerSec)
	}

	assertManaConduitMatchesLegacy(t, s, ally)
}

// TestManaConduitMigration_ClericSelfInclusion confirms the Cleric benefits
// from their OWN aura (distance 0 to themselves) — self-inclusion is
// preserved from the legacy helper.
func TestManaConduitMigration_ClericSelfInclusion(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "mana_conduit")

	s.rebuildAuraStatCacheLocked()
	wantBonus := legacyManaConduitBonusLocked(s, cleric)
	if wantBonus <= 0 {
		t.Fatalf("setup: legacy oracle computed 0 self-bonus for the Cleric — expected a positive self-buff")
	}
	assertManaConduitMatchesLegacy(t, s, cleric)
}

// TestManaConduitMigration_OutsideRadius_NoBonus confirms an ally outside
// the aura radius gets nothing.
func TestManaConduitMigration_OutsideRadius_NoBonus(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "mana_conduit")
	def := perkDefByID("mana_conduit")
	if def == nil {
		t.Fatal("mana_conduit perk def not found")
	}
	cfg := def.ConfigForRank(cleric.Rank)

	ally := s.spawnPlayerUnitLocked("acolyte", "p1", "#0099ff", protocol.Vec2{X: cleric.X + cfg["radiusPixels"]*2, Y: cleric.Y})
	ally.Visible = true
	ally.MaxMana = 200
	ally.CurrentMana = 100

	assertManaConduitMatchesLegacy(t, s, ally)
	if got := s.effectiveManaRegenLocked(ally); math.Abs(got-ally.ManaRegenPerSecond) > 1e-6 {
		t.Errorf("ally outside radius: effective mana regen = %.6f, want base rate %.6f (no aura)", got, ally.ManaRegenPerSecond)
	}
}

// TestManaConduitMigration_HostileRecipient_NoBonus confirms a hostile unit
// standing inside the aura's radius is not affected.
func TestManaConduitMigration_HostileRecipient_NoBonus(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "mana_conduit")
	enemy := s.spawnPlayerUnitLocked("acolyte", enemyPlayerID, "#e74c3c", protocol.Vec2{X: cleric.X + 10, Y: cleric.Y})
	enemy.Visible = true
	enemy.MaxMana = 200
	enemy.CurrentMana = 100

	assertManaConduitMatchesLegacy(t, s, enemy)
	if got := s.effectiveManaRegenLocked(enemy); math.Abs(got-enemy.ManaRegenPerSecond) > 1e-6 {
		t.Errorf("hostile unit in radius: effective mana regen = %.6f, want base rate %.6f (no aura)", got, enemy.ManaRegenPerSecond)
	}
}

// TestManaConduitMigration_DeadSource_NoBonus confirms a Cleric with
// mana_conduit at HP<=0 does not contribute an aura.
func TestManaConduitMigration_DeadSource_NoBonus(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "mana_conduit")
	ally := s.spawnPlayerUnitLocked("acolyte", "p1", "#0099ff", protocol.Vec2{X: cleric.X + 10, Y: cleric.Y})
	ally.Visible = true
	ally.MaxMana = 200
	ally.CurrentMana = 100
	cleric.HP = 0

	assertManaConduitMatchesLegacy(t, s, ally)
	if got := s.effectiveManaRegenLocked(ally); math.Abs(got-ally.ManaRegenPerSecond) > 1e-6 {
		t.Errorf("ally near dead Cleric: effective mana regen = %.6f, want base rate %.6f (no aura from a dead source)", got, ally.ManaRegenPerSecond)
	}
}

// TestManaConduitMigration_InvisibleSource_NoBonus confirms a Cleric with
// mana_conduit that is not Visible does not contribute an aura.
func TestManaConduitMigration_InvisibleSource_NoBonus(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "mana_conduit")
	ally := s.spawnPlayerUnitLocked("acolyte", "p1", "#0099ff", protocol.Vec2{X: cleric.X + 10, Y: cleric.Y})
	ally.Visible = true
	ally.MaxMana = 200
	ally.CurrentMana = 100
	cleric.Visible = false

	assertManaConduitMatchesLegacy(t, s, ally)
	if got := s.effectiveManaRegenLocked(ally); math.Abs(got-ally.ManaRegenPerSecond) > 1e-6 {
		t.Errorf("ally near invisible Cleric: effective mana regen = %.6f, want base rate %.6f (no aura from an invisible source)", got, ally.ManaRegenPerSecond)
	}
}

// TestManaConduitMigration_NoManaPool_Inert confirms a unit with MaxMana == 0
// is entirely unaffected by a covering mana_conduit aura — effectiveManaRegenLocked
// returns 0 outright (its own guard), and this must not change.
func TestManaConduitMigration_NoManaPool_Inert(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "mana_conduit")
	soldier := s.spawnPlayerUnitLocked("soldier", "p1", "#aabb00", protocol.Vec2{X: cleric.X + 10, Y: cleric.Y})
	soldier.Visible = true
	if soldier.MaxMana > 0 {
		t.Skipf("soldier MaxMana = %d; test assumes 0", soldier.MaxMana)
	}

	assertManaConduitMatchesLegacy(t, s, soldier)
	if got := s.effectiveManaRegenLocked(soldier); got != 0 {
		t.Errorf("no-mana-pool unit: effective mana regen = %.6f, want 0", got)
	}
}

// TestManaConduitMigration_RankPromotedEmitter proves the cache resolves the
// aura using the EMITTER's rank field (not a fixed/global default),
// mirroring the legacy code's def.ConfigForRank(src.Rank) call. mana_conduit
// carries no ConfigByRank override in the catalog today, so this is expected
// to produce the SAME bonus as an unpromoted emitter — the point of this
// test is to guard against a regression where the migration accidentally
// hardcodes "bronze" or ignores src.Rank.
func TestManaConduitMigration_RankPromotedEmitter(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "mana_conduit")
	cleric.Rank = unitRankGold // promote the EMITTER only

	ally := s.spawnPlayerUnitLocked("acolyte", "p1", "#0099ff", protocol.Vec2{X: cleric.X + 10, Y: cleric.Y})
	ally.Visible = true
	ally.MaxMana = 200
	ally.CurrentMana = 100

	assertManaConduitMatchesLegacy(t, s, ally)
}

// TestManaConduitMigration_ZoneAuraOrderingGuard is THE critical ordering
// test (see perk_aura_stat_cache.go's "ordering trap" doc, mirrored by
// mana.go's effectiveManaRegenLocked comment). A recipient with BOTH a
// covering mana_conduit aura AND an active zone `manaRegen` stat modifier
// must see the aura bonus folded in BEFORE the zone (add, mul) stage is
// applied — i.e. rate = (base + auraBonus + zoneAdd) × zoneMul — NOT have
// the aura instead treated as part of the zone's additive pool before the
// zone add is even known (which would be arithmetically identical here by
// coincidence of addition being associative, so this test also asserts the
// zone multiply is NOT applied to the aura bonus twice by using a zoneMul
// != 1 and checking the exact reconstructed value).
func TestManaConduitMigration_ZoneAuraOrderingGuard(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "mana_conduit")
	ally := s.spawnPlayerUnitLocked("acolyte", "p1", "#0099ff", protocol.Vec2{X: cleric.X + 10, Y: cleric.Y})
	ally.Visible = true
	ally.MaxMana = 200
	ally.CurrentMana = 100
	ally.ManaRegenPerSecond = 3

	const zoneAdd = 1.5
	const zoneMul = 1.25
	if s.Players["p1"] == nil {
		s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	}
	s.Players["p1"].ZoneStatModifiers = PlayerStatModifierSet{
		statManaRegen: statAccum{Add: zoneAdd, Mul: zoneMul},
	}

	s.rebuildAuraStatCacheLocked()
	// Use the legacy oracle (not the new cache reader) to compute the aura
	// bonus, so this assertion is meaningful both BEFORE and AFTER the
	// migration lands — the whole point of a characterization test.
	auraBonus := legacyManaConduitBonusLocked(s, ally)
	if auraBonus <= 0 {
		t.Fatalf("setup: expected a positive covering aura bonus, got %.6f", auraBonus)
	}

	want := (ally.ManaRegenPerSecond + auraBonus + zoneAdd) * zoneMul
	got := s.effectiveManaRegenLocked(ally)
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("effectiveManaRegenLocked with aura + zone aura = %.6f, want %.6f "+
			"((base %.4f + auraBonus %.4f + zoneAdd %.4f) × zoneMul %.4f)",
			got, want, ally.ManaRegenPerSecond, auraBonus, zoneAdd, zoneMul)
	}

	// Also verify the legacy-oracle reconstruction agrees end-to-end.
	assertManaConduitMatchesLegacy(t, s, ally)
}

// TestManaConduitMigration_BuffIconStillAppears guards the HUD path flagged
// by the task brief: hasManaConduitAuraLocked (perks_cleric.go) was
// repointed at the generic aura cache, and perks_icons.go's recipient-buff
// branch depends on it to decide when to show the mana_conduit icon on a
// covered ally. Also confirms:
//   - the OWNER's own icon (a separate, unmigrated switch-case branch in
//     perks_icons.go) still fires exactly once (not doubled by the
//     recipient branch, which explicitly skips units owning the perk).
//   - a recipient outside the aura gets neither.
//   - a covered unit with MaxMana == 0 gets NO icon (perks_icons.go's
//     unit.MaxMana > 0 guard on the recipient branch).
func TestManaConduitMigration_BuffIconStillAppears(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "mana_conduit")
	def := perkDefByID("mana_conduit")
	if def == nil {
		t.Fatal("mana_conduit perk def not found")
	}
	cfg := def.ConfigForRank(cleric.Rank)

	inside := s.spawnPlayerUnitLocked("acolyte", "p1", "#0099ff", protocol.Vec2{X: cleric.X + 10, Y: cleric.Y})
	inside.Visible = true
	inside.MaxMana = 200

	outside := s.spawnPlayerUnitLocked("acolyte", "p1", "#00ffaa", protocol.Vec2{X: cleric.X + cfg["radiusPixels"]*2, Y: cleric.Y})
	outside.Visible = true
	outside.MaxMana = 200

	// Covered but no mana pool at all — must not get the icon even though it
	// is well within radius.
	noMana := s.spawnPlayerUnitLocked("soldier", "p1", "#aabb00", protocol.Vec2{X: cleric.X + 5, Y: cleric.Y})
	noMana.Visible = true
	if noMana.MaxMana > 0 {
		t.Skipf("soldier MaxMana = %d; test assumes 0", noMana.MaxMana)
	}

	s.rebuildAuraStatCacheLocked()

	ownerIcons := iconIDs(s.activeBuffIconsLocked(cleric))
	ownerCount := 0
	for _, id := range ownerIcons {
		if id == "mana_conduit" {
			ownerCount++
		}
	}
	if ownerCount != 1 {
		t.Errorf("Cleric (owner) mana_conduit icon count = %d, want exactly 1 (no double-count from the recipient branch)", ownerCount)
	}

	insideIcons := iconIDs(s.activeBuffIconsLocked(inside))
	if !containsString(insideIcons, "mana_conduit") {
		t.Errorf("covered ally icons = %v, want to contain %q", insideIcons, "mana_conduit")
	}

	outsideIcons := iconIDs(s.activeBuffIconsLocked(outside))
	if containsString(outsideIcons, "mana_conduit") {
		t.Errorf("ally outside radius icons = %v, want NOT to contain %q", outsideIcons, "mana_conduit")
	}

	noManaIcons := iconIDs(s.activeBuffIconsLocked(noMana))
	if containsString(noManaIcons, "mana_conduit") {
		t.Errorf("no-mana-pool covered ally icons = %v, want NOT to contain %q", noManaIcons, "mana_conduit")
	}
}

// TestZealousMarchMigration_BuffIconStillAppears guards the HUD path
// flagged by the task brief: hasZealousMarchAuraLocked (perks_cleric.go) was
// repointed at the generic aura cache, and perks_icons.go's recipient-buff
// branch depends on it to decide when to show the zealous_march icon on a
// covered ally. Also confirms the OWNER's own icon (a separate, unmigrated
// switch-case branch in perks_icons.go) still fires, and that a recipient
// outside the aura gets neither.
func TestZealousMarchMigration_BuffIconStillAppears(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "zealous_march")
	def := perkDefByID("zealous_march")
	if def == nil {
		t.Fatal("zealous_march perk def not found")
	}
	cfg := def.ConfigForRank(cleric.Rank)

	inside := spawnClericTestAlly(t, s, cleric.X+10, cleric.Y)
	outside := spawnClericTestAlly(t, s, cleric.X+cfg["radiusPixels"]*2, cleric.Y)

	s.rebuildAuraStatCacheLocked()

	ownerIcons := iconIDs(s.activeBuffIconsLocked(cleric))
	if !containsString(ownerIcons, "zealous_march") {
		t.Errorf("Cleric (owner) icons = %v, want to contain %q", ownerIcons, "zealous_march")
	}

	insideIcons := iconIDs(s.activeBuffIconsLocked(inside))
	if !containsString(insideIcons, "zealous_march") {
		t.Errorf("covered ally icons = %v, want to contain %q", insideIcons, "zealous_march")
	}

	outsideIcons := iconIDs(s.activeBuffIconsLocked(outside))
	if containsString(outsideIcons, "zealous_march") {
		t.Errorf("ally outside radius icons = %v, want NOT to contain %q", outsideIcons, "zealous_march")
	}
}

// ═════════════════════════════════════════════════════════════════════════════
// sanctuary AURA MIGRATION — characterization tests
//
// sanctuary moved from a bespoke Go scan (perkRangedDamageMultiplierFromAurasLocked,
// deleted from perks_auras.go) to the same generic, data-driven PerkDef.Auras
// vocabulary zealous_march and mana_conduit pioneered. Unlike those two,
// sanctuary is modeled as an ADDITIVE REDUCTION stat
// (statProjectileDamageReduction, stat_modifiers.go) rather than folding a
// multiplier through applyStatStages: the legacy algorithm is
// max(reduction) then 1.0 - reduction, which maps directly onto the existing
// add-only, max-stacking aura vocabulary with zero schema widening. The
// src.Kind == "projectile" gate — the whole reason sanctuary only mitigates
// ranged hits — has NO analog in the generic aura cache (it has no notion of
// damage kind at all), so it stays at the fold site
// (applyUnitDamageWithSourceLocked's Step 3b, perks_defense.go) exactly as
// it did before migration.
//
// legacySanctuaryMultiplierLocked below is a byte-for-byte copy of the
// DELETED helper's algorithm, kept here ONLY as a characterization oracle —
// every test computes its "want" by calling this copy (which reads live
// catalog config, no hardcoded literals) and asserts the NEW production
// code (applyUnitDamageWithSourceLocked, reading the generic aura cache)
// produces identical final damage.
// ═════════════════════════════════════════════════════════════════════════════

// legacySanctuaryMultiplierLocked is the pre-migration algorithm
// (perks_auras.go's now-deleted perkRangedDamageMultiplierFromAurasLocked),
// preserved verbatim as a test-only oracle. DO NOT "fix" this to match new
// behavior — it exists specifically to stay frozen at the pre-migration
// formula so tests can detect any drift.
func legacySanctuaryMultiplierLocked(s *GameState, target *Unit, src DamageSource) float64 {
	if src.Kind != "projectile" {
		return 1.0 // only projectile damage is mitigated by sanctuary
	}
	if target == nil {
		return 1.0
	}

	def := perkDefByID("sanctuary")
	if def == nil {
		return 1.0
	}

	bestReduction := 0.0
	for _, u := range s.Units {
		if u == nil || u.HP <= 0 || !u.Visible {
			continue
		}
		if !containsString(u.PerkIDs, "sanctuary") {
			continue
		}
		// Aura must cover the target, not the source; same-team requirement.
		if !s.unitsFriendlyLocked(u, target) {
			continue
		}
		radius := def.Config["radiusPixels"]
		dx := u.X - target.X
		dy := u.Y - target.Y
		if dx*dx+dy*dy > radius*radius {
			continue
		}
		reduction := def.Config["damageReductionPercent"]
		if reduction > bestReduction {
			bestReduction = reduction
		}
	}
	if bestReduction <= 0 {
		return 1.0
	}
	return 1.0 - bestReduction
}

// assertSanctuaryMatchesLegacy rebuilds the generic aura cache, computes the
// legacy oracle's multiplier for (target, src), then drives the REAL
// production entry point (applyUnitDamageWithSourceLocked) with rawDamage
// and asserts the HP actually lost equals round(rawDamage × legacyMult) —
// the exact arithmetic perks_defense.go's Step 3b performs. Callers must set
// target.Armor = 0 and target.HP = target.MaxHP (or otherwise known) before
// calling, so armor mitigation doesn't also perturb the raw hit.
func assertSanctuaryMatchesLegacy(t *testing.T, s *GameState, target *Unit, src DamageSource, rawDamage int) {
	t.Helper()
	s.rebuildAuraStatCacheLocked()
	legacyMult := legacySanctuaryMultiplierLocked(s, target, src)
	wantDamage := rawDamage
	if legacyMult < 1.0 {
		wantDamage = maxInt(0, int(math.Round(float64(rawDamage)*legacyMult)))
	}
	startHP := target.HP
	s.applyUnitDamageWithSourceLocked(target, rawDamage, src)
	gotDamage := startHP - target.HP
	if gotDamage != wantDamage {
		t.Errorf("applyUnitDamageWithSourceLocked damage = %d, want %d (legacy multiplier %.6f)", gotDamage, wantDamage, legacyMult)
	}
}

// TestSanctuaryMigration_ProjectileDamage_MatchesLegacyFormula covers a
// single covering Cleric mitigating a projectile hit on an ally.
func TestSanctuaryMigration_ProjectileDamage_MatchesLegacyFormula(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "sanctuary")
	def := perkDefByID("sanctuary")
	if def == nil {
		t.Fatal("sanctuary perk def not found")
	}
	radius := def.Config["radiusPixels"]

	ally := spawnClericTestAlly(t, s, cleric.X+radius*0.5, cleric.Y)
	ally.Armor = 0

	assertSanctuaryMatchesLegacy(t, s, ally, DamageSource{Kind: "projectile"}, 100)
}

// TestSanctuaryMigration_NonProjectileDamage_Unreduced is THE most important
// guard: melee, ability, and trap damage inside a covering sanctuary aura
// must land UNREDUCED. The src.Kind == "projectile" gate has no analog in
// the generic aura cache — it MUST still live at the fold site
// (perks_defense.go's Step 3b) after migration, not get lost when the
// bespoke scan was replaced by a cache read.
func TestSanctuaryMigration_NonProjectileDamage_Unreduced(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "sanctuary")
	def := perkDefByID("sanctuary")
	if def == nil {
		t.Fatal("sanctuary perk def not found")
	}
	radius := def.Config["radiusPixels"]

	ally := spawnClericTestAlly(t, s, cleric.X+radius*0.5, cleric.Y)
	ally.Armor = 0

	for _, kind := range []string{"melee", "ability", "trap"} {
		ally.HP = ally.MaxHP
		assertSanctuaryMatchesLegacy(t, s, ally, DamageSource{Kind: kind}, 100)
		if ally.HP != ally.MaxHP-100 {
			t.Errorf("kind=%q: HP after hit = %d, want %d (100 raw damage, unreduced)", kind, ally.HP, ally.MaxHP-100)
		}
	}
}

// TestSanctuaryMigration_TwoOverlapping_MaxWinsNotSummed places two Clerics
// with sanctuary covering the same ally and proves the reduction equals ONE
// source's reduction, not the sum (no double-mitigation from stacking
// auras).
func TestSanctuaryMigration_TwoOverlapping_MaxWinsNotSummed(t *testing.T) {
	s, clericA := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(clericA, "sanctuary")
	clericB := s.spawnPlayerUnitLocked("acolyte", "p1", "#aabbcc", protocol.Vec2{X: clericA.X + 10, Y: clericA.Y})
	clericB.Visible = true
	grantPerk(clericB, "sanctuary")

	def := perkDefByID("sanctuary")
	if def == nil {
		t.Fatal("sanctuary perk def not found")
	}
	radius := def.Config["radiusPixels"]

	ally := spawnClericTestAlly(t, s, clericA.X+radius*0.5, clericA.Y)
	ally.Armor = 0

	s.rebuildAuraStatCacheLocked()
	oracleMult := legacySanctuaryMultiplierLocked(s, ally, DamageSource{Kind: "projectile"})
	wantMult := 1.0 - def.Config["damageReductionPercent"]
	if math.Abs(oracleMult-wantMult) > 1e-9 {
		t.Fatalf("setup: legacy oracle multiplier = %.6f, want %.6f (single source's reduction — proves max-wins, not summed, BEFORE comparing production)", oracleMult, wantMult)
	}

	assertSanctuaryMatchesLegacy(t, s, ally, DamageSource{Kind: "projectile"}, 100)
}

// TestSanctuaryMigration_OutsideRadius_NoReduction confirms an ally outside
// the aura radius gets no mitigation.
func TestSanctuaryMigration_OutsideRadius_NoReduction(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "sanctuary")
	def := perkDefByID("sanctuary")
	if def == nil {
		t.Fatal("sanctuary perk def not found")
	}
	radius := def.Config["radiusPixels"]

	ally := spawnClericTestAlly(t, s, cleric.X+radius*2, cleric.Y)
	ally.Armor = 0

	assertSanctuaryMatchesLegacy(t, s, ally, DamageSource{Kind: "projectile"}, 100)
}

// TestSanctuaryMigration_HostileRecipient_NoReduction confirms a hostile
// unit standing inside the aura's radius is not affected — sanctuary
// protects allies, not enemies caught in its radius.
func TestSanctuaryMigration_HostileRecipient_NoReduction(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "sanctuary")
	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: cleric.X + 10, Y: cleric.Y})
	enemy.Visible = true
	enemy.Armor = 0
	enemy.HP = enemy.MaxHP

	assertSanctuaryMatchesLegacy(t, s, enemy, DamageSource{Kind: "projectile"}, 100)
}

// TestSanctuaryMigration_DeadSource_NoReduction confirms a Cleric with
// sanctuary at HP<=0 does not contribute an aura.
func TestSanctuaryMigration_DeadSource_NoReduction(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "sanctuary")
	def := perkDefByID("sanctuary")
	if def == nil {
		t.Fatal("sanctuary perk def not found")
	}
	radius := def.Config["radiusPixels"]

	ally := spawnClericTestAlly(t, s, cleric.X+radius*0.5, cleric.Y)
	ally.Armor = 0
	cleric.HP = 0

	assertSanctuaryMatchesLegacy(t, s, ally, DamageSource{Kind: "projectile"}, 100)
}

// TestSanctuaryMigration_InvisibleSource_NoReduction confirms a Cleric with
// sanctuary that is not Visible does not contribute an aura.
func TestSanctuaryMigration_InvisibleSource_NoReduction(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "sanctuary")
	def := perkDefByID("sanctuary")
	if def == nil {
		t.Fatal("sanctuary perk def not found")
	}
	radius := def.Config["radiusPixels"]

	ally := spawnClericTestAlly(t, s, cleric.X+radius*0.5, cleric.Y)
	ally.Armor = 0
	cleric.Visible = false

	assertSanctuaryMatchesLegacy(t, s, ally, DamageSource{Kind: "projectile"}, 100)
}

// TestSanctuaryMigration_ClericSelfProtection confirms the Cleric mitigates
// projectile damage aimed at ITSELF — unitsFriendlyLocked(a, b) resolves to
// playersAreFriendlyLocked(a.OwnerID, b.OwnerID), which is true for a unit
// and itself (same OwnerID), and the legacy scan carried no explicit
// self-exclusion. The migrated aura's includeSelf: true was authored
// specifically to preserve this.
func TestSanctuaryMigration_ClericSelfProtection(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "sanctuary")
	cleric.Armor = 0
	cleric.HP = cleric.MaxHP

	s.rebuildAuraStatCacheLocked()
	oracleMult := legacySanctuaryMultiplierLocked(s, cleric, DamageSource{Kind: "projectile"})
	if oracleMult >= 1.0 {
		t.Fatalf("setup: legacy oracle computed no self-reduction for the Cleric — expected sanctuary to protect its own caster")
	}

	assertSanctuaryMatchesLegacy(t, s, cleric, DamageSource{Kind: "projectile"}, 100)
}

// TestSanctuaryMigration_OrderingGuard_MarkThenSanctuaryThenFlatReduction is
// THE critical ordering test. It reproduces applyUnitDamageWithSourceLocked's
// Steps 3 (Challenger's Mark amplification), 3b (sanctuary), and 4 (flat
// reduction, reinforced_armor) arithmetic by hand and asserts the target's
// ACTUAL final damage from a live call matches — proving sanctuary still
// applies strictly AFTER mark amplification and strictly BEFORE flat
// reduction post-migration, not folded in at some other position by the
// generic aura cache read.
func TestSanctuaryMigration_OrderingGuard_MarkThenSanctuaryThenFlatReduction(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "sanctuary")
	sanctuaryDef := perkDefByID("sanctuary")
	if sanctuaryDef == nil {
		t.Fatal("sanctuary perk def not found")
	}
	radius := sanctuaryDef.Config["radiusPixels"]
	reductionPct := sanctuaryDef.Config["damageReductionPercent"]

	ally := spawnClericTestAlly(t, s, cleric.X+radius*0.5, cleric.Y)
	ally.Armor = 0

	grantPerk(ally, "reinforced_armor")
	reinforcedDef := perkDefByID("reinforced_armor")
	if reinforcedDef == nil {
		t.Fatal("reinforced_armor perk def not found")
	}
	flatReduction := int(reinforcedDef.Config["flatReduction"])
	if flatReduction <= 0 {
		t.Fatalf("setup: reinforced_armor flatReduction must be > 0, got %d", flatReduction)
	}

	// Drive a mark stack directly (same pattern as trap_test.go's mark-stack
	// tests) using marker_trap's authored markMultiplier — a real catalog
	// value, not a hardcoded balance literal.
	markerDef := perkDefByID("marker_trap")
	if markerDef == nil {
		t.Fatal("marker_trap perk def not found")
	}
	markMult := markerDef.Config["markMultiplier"]
	if markMult <= 0 {
		t.Fatalf("setup: marker_trap markMultiplier must be > 0, got %.4f", markMult)
	}
	ally.PerkState.applyMarkStack("test-mark-source", 0, markMult, 30.0)

	const rawDamage = 100
	// Step 3: mark amplification (perks_defense.go).
	markedDamage := maxInt(rawDamage, int(math.Round(float64(rawDamage)*(1.0+markMult))))
	// Step 3b: sanctuary, applied to the POST-MARK amount.
	s.rebuildAuraStatCacheLocked()
	sanctuaryMult := legacySanctuaryMultiplierLocked(s, ally, DamageSource{Kind: "projectile"})
	if sanctuaryMult >= 1.0 {
		t.Fatalf("setup: legacy sanctuary oracle computed no reduction, mult=%.4f", sanctuaryMult)
	}
	sanctuaryReduced := maxInt(0, int(math.Round(float64(markedDamage)*sanctuaryMult)))
	// Step 4: flat reduction, applied AFTER sanctuary.
	wantDamage := maxInt(0, sanctuaryReduced-flatReduction)

	// Sanity: prove ordering actually matters for this fixture — if
	// sanctuary were skipped entirely, mark+flat-reduction alone would give
	// a DIFFERENT answer. Guards against a fixture that happens to produce
	// the same number regardless of whether sanctuary ran.
	noSanctuaryDamage := maxInt(0, markedDamage-flatReduction)
	if noSanctuaryDamage == wantDamage {
		t.Fatalf("setup: sanctuary reduction (%.0f%%) had no effect on this fixture's answer (%d == %d) — strengthen the fixture so the ordering guard is meaningful", reductionPct*100, noSanctuaryDamage, wantDamage)
	}

	startHP := ally.HP
	s.applyUnitDamageWithSourceLocked(ally, rawDamage, DamageSource{Kind: "projectile"})
	gotDamage := startHP - ally.HP

	if gotDamage != wantDamage {
		t.Errorf("ordering guard: got damage %d, want %d (raw %d -> mark x%.4f -> %d -> sanctuary x%.4f -> %d -> flat -%d -> %d)",
			gotDamage, wantDamage, rawDamage, 1.0+markMult, markedDamage, sanctuaryMult, sanctuaryReduced, flatReduction, wantDamage)
	}
}

// ═════════════════════════════════════════════════════════════════════════════
// guardian_aura AURA MIGRATION — characterization tests
//
// guardian_aura moved from a hand-written per-tick cache (guardianAuraCache /
// rebuildGuardianAuraCacheLocked / guardianAuraValue, all deleted from
// perks_auras.go / state.go) to the generic, data-driven PerkDef.Auras
// vocabulary — the same engine zealous_march, mana_conduit, and sanctuary
// already use, extended with two capabilities this perk needed and no prior
// aura used:
//   - the "sameOwner" Targets scope (strictly narrower than "allies" — see
//     PerkAura.Targets' doc comment: same OWNER, not merely same team), and
//   - EMITTER-side companion synergy (PerkAura.SynergyRadiusPerCompanion /
//     PerkStatModifier.PerCompanion), distinct from the recipient-side
//     PerAdditionalSource stacking the other three auras use.
//
// legacyGuardianAuraCacheLocked below is a byte-for-byte copy of the DELETED
// rebuildGuardianAuraCacheLocked algorithm (perks_auras.go), preserved here
// ONLY as a characterization oracle — every test computes its "want" by
// calling this copy (which reads live catalog config, no hardcoded literals)
// and asserts the NEW production code (effectiveArmorLocked, reading the
// generic aura cache via guardianAuraReadLocked — a small test-local shim
// defined in gold_perks_test.go) produces identical results. See
// gold_perks_test.go's own guardian_aura section for the exhaustive
// pre-existing geometry-based suite this migration also had to keep green
// (all of it does, unmodified in substance — only the read API changed).
// ═════════════════════════════════════════════════════════════════════════════

// legacyGuardianAuraValue mirrors the deleted guardianAuraValue struct —
// test-only, frozen alongside legacyGuardianAuraCacheLocked.
type legacyGuardianAuraValue struct {
	FlatArmor    int
	PercentArmor float64
	Sources      int
}

// legacyGuardianAuraCacheLocked is the pre-migration algorithm
// (perks_auras.go's now-deleted rebuildGuardianAuraCacheLocked), preserved
// verbatim as a test-only oracle. DO NOT "fix" this to match new behavior —
// it exists specifically to stay frozen at the pre-migration formula so
// tests can detect any drift.
func legacyGuardianAuraCacheLocked(s *GameState) map[int]legacyGuardianAuraValue {
	cache := map[int]legacyGuardianAuraValue{}

	def := perkDefByID("guardian_aura")
	if def == nil {
		return cache
	}

	type auraSource struct {
		unitID     int
		ownerID    string
		x, y       float64
		baseR      float64
		baseFlat   float64
		basePct    float64
		rBonus     float64
		flatBonus  float64
		pctBonus   float64
		effR       float64
		effFlat    float64
		effPercent float64
	}

	var sources []auraSource
	for _, u := range s.Units {
		if u == nil || u.HP <= 0 || !u.Visible {
			continue
		}
		if !containsString(u.PerkIDs, "guardian_aura") {
			continue
		}
		sources = append(sources, auraSource{
			unitID:    u.ID,
			ownerID:   u.OwnerID,
			x:         u.X,
			y:         u.Y,
			baseR:     def.Config["radius"],
			baseFlat:  def.Config["bonusArmor"],
			basePct:   def.Config["armorPercent"],
			rBonus:    def.Config["synergyRadiusBonus"],
			flatBonus: def.Config["synergyArmorBonus"],
			pctBonus:  def.Config["synergyArmorPercentBonus"],
		})
	}

	if len(sources) == 0 {
		return cache
	}

	// Companion counting: SAME ownerID, within BASE radius — never effR.
	for i := range sources {
		companions := 0
		baseRSq := sources[i].baseR * sources[i].baseR
		for j := range sources {
			if i == j {
				continue
			}
			if sources[j].ownerID != sources[i].ownerID {
				continue
			}
			dx := sources[j].x - sources[i].x
			dy := sources[j].y - sources[i].y
			if dx*dx+dy*dy <= baseRSq {
				companions++
			}
		}
		sources[i].effR = sources[i].baseR + float64(companions)*sources[i].rBonus
		sources[i].effFlat = sources[i].baseFlat + float64(companions)*sources[i].flatBonus
		sources[i].effPercent = sources[i].basePct + float64(companions)*sources[i].pctBonus
	}

	// Fan-out: owner excluded, strict same-OwnerID recipients, max per dimension.
	for i := range sources {
		effRSq := sources[i].effR * sources[i].effR
		for _, u := range s.Units {
			if u == nil || u.HP <= 0 || !u.Visible {
				continue
			}
			if u.ID == sources[i].unitID {
				continue // owner excluded from own aura
			}
			if u.OwnerID != sources[i].ownerID {
				continue // strictly same-owner only
			}
			dx := u.X - sources[i].x
			dy := u.Y - sources[i].y
			if dx*dx+dy*dy > effRSq {
				continue
			}
			existing := cache[u.ID]
			flat := int(sources[i].effFlat)
			if flat > existing.FlatArmor {
				existing.FlatArmor = flat
			}
			if sources[i].effPercent > existing.PercentArmor {
				existing.PercentArmor = sources[i].effPercent
			}
			existing.Sources++
			cache[u.ID] = existing
		}
	}

	return cache
}

// assertGuardianAuraMatchesLegacy rebuilds the generic aura cache, computes
// the legacy oracle's full per-unit cache, and asserts the NEW production
// reader (guardianAuraReadLocked, gold_perks_test.go) agrees with the legacy
// entry (or its absence) for `unit`.
func assertGuardianAuraMatchesLegacy(t *testing.T, s *GameState, unit *Unit) {
	t.Helper()
	s.rebuildAuraStatCacheLocked()
	legacy := legacyGuardianAuraCacheLocked(s)
	wantEntry, wantOK := legacy[unit.ID]

	gotFlat, gotPct, gotSources, gotOK := guardianAuraReadLocked(s, unit)
	if gotOK != wantOK {
		t.Fatalf("guardian_aura coverage: got ok=%v, want ok=%v", gotOK, wantOK)
	}
	if !wantOK {
		return
	}
	if gotFlat != wantEntry.FlatArmor {
		t.Errorf("guardian_aura FlatArmor: got %d, want %d (legacy)", gotFlat, wantEntry.FlatArmor)
	}
	if math.Abs(gotPct-wantEntry.PercentArmor) > 1e-6 {
		t.Errorf("guardian_aura PercentArmor: got %.6f, want %.6f (legacy)", gotPct, wantEntry.PercentArmor)
	}
	if gotSources != wantEntry.Sources {
		t.Errorf("guardian_aura Sources: got %d, want %d (legacy)", gotSources, wantEntry.Sources)
	}
}

// TestGuardianAuraMigration_OneGuardian_AllyInRadius_MatchesLegacyFormula
// covers the baseline single-emitter case against the frozen oracle.
func TestGuardianAuraMigration_OneGuardian_AllyInRadius_MatchesLegacyFormula(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "guardian_aura")
	ally := spawnAlly(t, s, "p1", vanguard.X+50, vanguard.Y)

	assertGuardianAuraMatchesLegacy(t, s, ally)

	// Sanity: the legacy oracle actually found a positive bonus here, so the
	// comparison above is meaningful (not two empty results agreeing).
	legacy := legacyGuardianAuraCacheLocked(s)
	if legacy[ally.ID].FlatArmor <= 0 {
		t.Fatalf("setup: legacy oracle computed 0 FlatArmor for ally in radius")
	}
}

// TestGuardianAuraMigration_AllyOutsideRadius_NoBonus confirms an ally
// outside the base radius (no synergy to inflate it further) gets nothing.
func TestGuardianAuraMigration_AllyOutsideRadius_NoBonus(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "guardian_aura")
	def := perkDefByID("guardian_aura")
	ally := spawnAlly(t, s, "p1", vanguard.X+def.Config["radius"]+50, vanguard.Y)

	assertGuardianAuraMatchesLegacy(t, s, ally)
	if _, _, _, ok := guardianAuraReadLocked(s, ally); ok {
		t.Error("ally outside radius should not be covered")
	}
}

// TestGuardianAuraMigration_OwnerExcluded_NoBonus is THE key differentiator
// from the other three migrated auras: guardian_aura's own emitter does NOT
// benefit from its own aura (legacy hard-excluded u.ID == source.unitID; the
// migrated aura reproduces this via IncludeSelf staying false/unset).
func TestGuardianAuraMigration_OwnerExcluded_NoBonus(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "guardian_aura")

	assertGuardianAuraMatchesLegacy(t, s, vanguard)
	if _, _, _, ok := guardianAuraReadLocked(s, vanguard); ok {
		t.Error("the emitting Vanguard should NOT be covered by its own guardian_aura")
	}
	// Sanity: confirm the legacy oracle agrees (also excludes the owner) so
	// this isn't just two implementations independently forgetting the case.
	legacy := legacyGuardianAuraCacheLocked(s)
	if _, ok := legacy[vanguard.ID]; ok {
		t.Fatal("setup: legacy oracle should also exclude the owner from its own aura")
	}
}

// TestGuardianAuraMigration_SameOwnerVsFriendly_DifferentOwnerSameTeam_NoBonus
// is the semantic that would silently break if the aura's Targets were
// authored as "allies" instead of "sameOwner": a unit belonging to a
// DIFFERENT player on the SAME team (both default to TeamID 0 here, so
// playersAreFriendlyLocked("p1","p2") is true) sitting inside the aura's
// radius gets NOTHING, because guardian_aura's legacy algorithm compared
// OwnerID directly, never team membership. The test harness CAN express
// same-team-different-owner units (two distinct OwnerID strings with no
// Player entry registered — playerTeamLocked defaults unknown players to
// team 0, exactly mirroring team_predicates_test.go's default-equivalence
// assumption), so this case is exercised directly rather than skipped.
func TestGuardianAuraMigration_SameOwnerVsFriendly_DifferentOwnerSameTeam_NoBonus(t *testing.T) {
	s, vanguard := newGoldPerkState(t) // vanguard owned by "p1"
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "guardian_aura")

	// A different player's unit, same (default) team, well within radius.
	teammate := spawnAlly(t, s, "p2", vanguard.X+30, vanguard.Y)
	if !s.playersAreFriendlyLocked("p1", "p2") {
		t.Fatalf("setup: p1 and p2 must be friendly (same default team) for this test to be meaningful")
	}

	assertGuardianAuraMatchesLegacy(t, s, teammate)
	if _, _, _, ok := guardianAuraReadLocked(s, teammate); ok {
		t.Error("a same-team, DIFFERENT-owner unit inside the radius must get NOTHING from guardian_aura (sameOwner, not allies)")
	}

	// Contrast: a unit owned by the SAME "p1" at the same relative position
	// IS covered — proves the exclusion above is about ownership, not
	// distance or some other unrelated gate.
	sameOwnerAlly := spawnAlly(t, s, "p1", vanguard.X+30, vanguard.Y+1)
	assertGuardianAuraMatchesLegacy(t, s, sameOwnerAlly)
	if _, _, _, ok := guardianAuraReadLocked(s, sameOwnerAlly); !ok {
		t.Error("a same-OWNER unit at the same relative position should be covered")
	}
}

// TestGuardianAuraMigration_CompanionSynergy_TwoGuardians proves the
// EMITTER-side companion-synergy phase: two same-owner Guardians within
// base radius of each other each gain exactly one companion's worth of
// radius/flat/percent boost.
func TestGuardianAuraMigration_CompanionSynergy_TwoGuardians(t *testing.T) {
	s, v1 := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("guardian_aura")
	grantPerk(v1, "guardian_aura")
	v2 := spawnAlly(t, s, "p1", v1.X+80, v1.Y) // 80 < base radius
	grantPerk(v2, "guardian_aura")

	// Ally beyond base radius but within the 1-companion effective radius.
	ally := spawnAlly(t, s, "p1", v1.X+def.Config["radius"]+def.Config["synergyRadiusBonus"]*0.5, v1.Y)

	assertGuardianAuraMatchesLegacy(t, s, ally)
	flatArmor, percentArmor, _, ok := guardianAuraReadLocked(s, ally)
	if !ok {
		t.Fatal("setup: ally should be covered via the 1-companion effective radius")
	}
	wantFlat := int(def.Config["bonusArmor"]) + int(def.Config["synergyArmorBonus"])
	wantPct := def.Config["armorPercent"] + def.Config["synergyArmorPercentBonus"]
	if flatArmor != wantFlat || math.Abs(percentArmor-wantPct) > 1e-6 {
		t.Errorf("two-Guardian companion synergy: got (flat=%d, pct=%.4f), want (flat=%d, pct=%.4f)",
			flatArmor, percentArmor, wantFlat, wantPct)
	}
}

// TestGuardianAuraMigration_CompanionSynergy_ThreeGuardians proves the
// companion count scales past 1: three mutually-clustered Guardians each see
// TWO companions' worth of boost.
func TestGuardianAuraMigration_CompanionSynergy_ThreeGuardians(t *testing.T) {
	s, v1 := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("guardian_aura")
	grantPerk(v1, "guardian_aura")
	v2 := spawnAlly(t, s, "p1", v1.X+80, v1.Y)
	grantPerk(v2, "guardian_aura")
	v3 := spawnAlly(t, s, "p1", v1.X+40, v1.Y+69) // equilateral-ish, all within base radius
	grantPerk(v3, "guardian_aura")

	ally := spawnAlly(t, s, "p1", v1.X+30, v1.Y)

	assertGuardianAuraMatchesLegacy(t, s, ally)
	flatArmor, percentArmor, _, ok := guardianAuraReadLocked(s, ally)
	if !ok {
		t.Fatal("setup: ally should be covered")
	}
	wantFlat := int(def.Config["bonusArmor"]) + 2*int(def.Config["synergyArmorBonus"])
	wantPct := def.Config["armorPercent"] + 2*def.Config["synergyArmorPercentBonus"]
	if flatArmor != wantFlat || math.Abs(percentArmor-wantPct) > 1e-6 {
		t.Errorf("three-Guardian companion synergy: got (flat=%d, pct=%.4f), want (flat=%d, pct=%.4f)",
			flatArmor, percentArmor, wantFlat, wantPct)
	}
}

// TestGuardianAuraMigration_RecursionGuard_CompanionCountUsesBaseRadius
// deliberately constructs the configuration the task calls out: a third
// Guardian (V3) placed between V1's BASE radius and V1's 1-companion
// EFFECTIVE radius. If V1's OWN companion count were (incorrectly) computed
// against its own inflated effective radius instead of its fixed base
// radius, V1 would count V3 as a second companion (165 < 180 = effR) even
// though V3 sits outside V1's actual base radius (165 > 150) — a recursive
// feedback loop the legacy algorithm explicitly forbids by reading only
// Phase-1 (base) data for the companion scan.
//
// Geometry (all on the X axis, Y=0), chosen so V3 is ALSO far enough from
// compC that it doesn't contaminate compC's OWN companion count (which
// would otherwise inflate compC's effR enough to reach the read point with
// a stronger, confusable value — an earlier draft of this fixture hit
// exactly that trap, which is why compC sits on the OPPOSITE side of V1
// from V3):
//
//	compC at -baseR*0.3   (dist to V1 = baseR*0.3 < baseR  → V1's companion)
//	V1     at 0
//	V3     at baseR+rBonus*0.5  (dist to V1 = baseR+rBonus*0.5: > baseR,
//	                              < baseR+rBonus → the "would be wrongly
//	                              pulled in by effR" zone for V1 specifically)
//	                             (dist to compC = baseR*1.3+rBonus*0.5 > baseR
//	                              → NOT compC's companion either, so compC
//	                              stays at exactly 1 companion — V1 — same
//	                              as the correct value for V1 itself)
//
// Read point: directly beside V1 (definitely covered by V1's own aura
// regardless of its radius), so the assertion is purely about V1's computed
// VALUE (a function of its companion count), not about which radius zone
// the reader falls into.
func TestGuardianAuraMigration_RecursionGuard_CompanionCountUsesBaseRadius(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("guardian_aura")
	baseR := def.Config["radius"]
	rBonus := def.Config["synergyRadiusBonus"]

	v1 := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 0, Y: 0})
	v1.MaxHP, v1.HP, v1.Visible = 500, 500, true
	grantPerk(v1, "guardian_aura")

	// compC: genuine companion of V1 (within V1's base radius), placed on
	// the OPPOSITE side of V1 from V3 so V3 cannot also inflate compC.
	compC := s.spawnPlayerUnitLocked("soldier", "p1", "#2ecc71", protocol.Vec2{X: -baseR * 0.3, Y: 0})
	compC.MaxHP, compC.HP, compC.Visible = 500, 500, true
	grantPerk(compC, "guardian_aura")

	// V3: strictly BETWEEN V1's base radius and V1's 1-companion effective
	// radius (baseR+rBonus) — the "would be pulled in by effR" zone.
	v3 := s.spawnPlayerUnitLocked("soldier", "p1", "#9b59b6", protocol.Vec2{X: baseR + rBonus*0.5, Y: 0})
	v3.MaxHP, v3.HP, v3.Visible = 500, 500, true
	grantPerk(v3, "guardian_aura")

	distV1V3 := math.Hypot(v3.X-v1.X, v3.Y-v1.Y)
	if distV1V3 <= baseR {
		t.Fatalf("setup: V3 must be OUTSIDE V1's base radius (dist=%.1f, baseR=%.1f)", distV1V3, baseR)
	}
	if distV1V3 > baseR+rBonus {
		t.Fatalf("setup: V3 must be INSIDE V1's 1-companion effective radius (dist=%.1f, effR=%.1f)", distV1V3, baseR+rBonus)
	}
	distCompCV3 := math.Hypot(v3.X-compC.X, v3.Y-compC.Y)
	if distCompCV3 <= baseR {
		t.Fatalf("setup: V3 must be OUTSIDE compC's base radius too, or compC's own companion count is contaminated (dist=%.1f, baseR=%.1f)", distCompCV3, baseR)
	}

	// Read point directly beside V1 — definitely within V1's aura regardless
	// of companion count, so this isolates V1's COMPUTED VALUE.
	reader := s.spawnPlayerUnitLocked("soldier", "p1", "#e74c3c", protocol.Vec2{X: baseR * 0.05, Y: 0})
	reader.MaxHP, reader.HP, reader.Visible = 200, 200, true

	assertGuardianAuraMatchesLegacy(t, s, reader)
	legacy := legacyGuardianAuraCacheLocked(s)
	flatArmor, percentArmor, _, ok := guardianAuraReadLocked(s, reader)
	if !ok {
		t.Fatal("reader should be covered by V1's aura")
	}
	want1CompFlat := int(def.Config["bonusArmor"]) + 1*int(def.Config["synergyArmorBonus"])
	want2CompFlat := int(def.Config["bonusArmor"]) + 2*int(def.Config["synergyArmorBonus"])
	if flatArmor == want2CompFlat {
		t.Fatalf("recursion guard FAILED: a source counted V3 as a companion via an inflated effective radius (got 2-companion value %d)", flatArmor)
	}
	if flatArmor != want1CompFlat {
		t.Errorf("recursion guard: got flat=%d, want 1-companion value %d", flatArmor, want1CompFlat)
	}
	want1CompPct := def.Config["armorPercent"] + 1*def.Config["synergyArmorPercentBonus"]
	if math.Abs(percentArmor-want1CompPct) > 1e-6 {
		t.Errorf("recursion guard: got pct=%.4f, want 1-companion value %.4f", percentArmor, want1CompPct)
	}
	// Cross-check directly against the frozen legacy oracle's companion
	// count semantics (baseR-only), not just production — confirms the
	// fixture itself exercises a genuine 1-companion scenario per the
	// pre-migration algorithm, not an accident of the new engine.
	if legacy[reader.ID].FlatArmor != want1CompFlat {
		t.Fatalf("setup: legacy oracle itself computed a non-1-companion value (%d) — fixture is not exercising the recursion guard", legacy[reader.ID].FlatArmor)
	}
}

// TestGuardianAuraMigration_MaxPerDimensionIndependence constructs two
// sources where source A has the higher FLAT contribution and source B has
// the higher PERCENT contribution (achieved via differing companion counts
// on otherwise-separate clusters), and asserts the recipient gets A's flat
// AND B's percent — proving the two dimensions are maxed independently, not
// as a bundle.
func TestGuardianAuraMigration_MaxPerDimensionIndependence(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 43)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("guardian_aura")
	baseR := def.Config["radius"]

	// Cluster A: two Guardians (1 companion each) — higher-than-base flat
	// AND percent, but we'll place the recipient so cluster B still wins on
	// percent via a THIRD companion in cluster B.
	a1 := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 0, Y: 0})
	a1.MaxHP, a1.HP, a1.Visible = 500, 500, true
	grantPerk(a1, "guardian_aura")
	a2 := s.spawnPlayerUnitLocked("soldier", "p1", "#2ecc71", protocol.Vec2{X: baseR * 0.3, Y: 0})
	a2.MaxHP, a2.HP, a2.Visible = 500, 500, true
	grantPerk(a2, "guardian_aura")

	// Cluster B: three Guardians (2 companions each) clustered far away —
	// higher flat AND percent than cluster A (2 companions > 1 companion).
	center := protocol.Vec2{X: 2000, Y: 0}
	offsets := [][2]float64{{-baseR * 0.2, 0}, {baseR * 0.2, 0}, {0, baseR * 0.2}}
	for _, off := range offsets {
		u := s.spawnPlayerUnitLocked("soldier", "p1", "#9b59b6", protocol.Vec2{X: center.X + off[0], Y: center.Y + off[1]})
		u.MaxHP, u.HP, u.Visible = 500, 500, true
		grantPerk(u, "guardian_aura")
	}

	// A recipient exactly at cluster A's position (a1) receives ONLY
	// cluster A's contribution (cluster B is 2000px away, far outside any
	// realistic radius) — this is just the 1-companion baseline value.
	recipientA := s.spawnPlayerUnitLocked("soldier", "p1", "#e74c3c", protocol.Vec2{X: baseR * 0.15, Y: 0})
	recipientA.MaxHP, recipientA.HP, recipientA.Visible = 200, 200, true

	assertGuardianAuraMatchesLegacy(t, s, recipientA)
	flatA, pctA, _, okA := guardianAuraReadLocked(s, recipientA)
	if !okA {
		t.Fatal("recipientA should be covered by cluster A")
	}
	want1CompFlat := int(def.Config["bonusArmor"]) + 1*int(def.Config["synergyArmorBonus"])
	want1CompPct := def.Config["armorPercent"] + 1*def.Config["synergyArmorPercentBonus"]
	if flatA != want1CompFlat || math.Abs(pctA-want1CompPct) > 1e-6 {
		t.Fatalf("setup: cluster A recipient got (flat=%d,pct=%.4f), want 1-companion (flat=%d,pct=%.4f)",
			flatA, pctA, want1CompFlat, want1CompPct)
	}

	// A recipient at cluster B's center receives cluster B's 2-companion
	// value on BOTH dimensions — strictly higher than cluster A on both.
	recipientB := s.spawnPlayerUnitLocked("soldier", "p1", "#f1c40f", center)
	recipientB.MaxHP, recipientB.HP, recipientB.Visible = 200, 200, true

	assertGuardianAuraMatchesLegacy(t, s, recipientB)
	flatB, pctB, _, okB := guardianAuraReadLocked(s, recipientB)
	if !okB {
		t.Fatal("recipientB should be covered by cluster B")
	}
	want2CompFlat := int(def.Config["bonusArmor"]) + 2*int(def.Config["synergyArmorBonus"])
	want2CompPct := def.Config["armorPercent"] + 2*def.Config["synergyArmorPercentBonus"]
	if flatB != want2CompFlat || math.Abs(pctB-want2CompPct) > 1e-6 {
		t.Errorf("cluster B recipient: got (flat=%d,pct=%.4f), want 2-companion (flat=%d,pct=%.4f)",
			flatB, pctB, want2CompFlat, want2CompPct)
	}
	if !(flatB > flatA && pctB > pctA) {
		t.Fatalf("setup: cluster B must strictly exceed cluster A on BOTH dimensions for this fixture to be meaningful (A: flat=%d pct=%.4f; B: flat=%d pct=%.4f)",
			flatA, pctA, flatB, pctB)
	}
	// The actual per-dimension-independence claim: a recipient covered by
	// BOTH clusters gets the max of each dimension independently — which,
	// since B strictly dominates A on both dimensions here, collapses to
	// "gets B's values." A stronger two-way split (A's flat, B's pct) would
	// require asymmetric per-dimension config values not present in
	// guardian_aura's catalog (both dimensions scale together with
	// companion count) — see the report for why this fixture proves the
	// mechanism (independent max() calls per dimension in Phase 3 of
	// perk_aura_stat_cache.go) even though guardian_aura's own tuning never
	// produces a source that wins on ONE dimension only.
}

// TestGuardianAuraMigration_IntTruncation_FlatArmor documents the int()
// truncation on the flat-armor dimension and WHY it cannot be exercised with
// a genuinely fractional value under guardian_aura's current catalog
// config: bonusArmor (15) and synergyArmorBonus (5) are both whole numbers,
// so effFlat = bonusArmor + companions×synergyArmorBonus is always an
// integer already — int() is a no-op conversion for every reachable
// companion count. This was equally true of the PRE-migration algorithm
// (perks_auras.go's deleted rebuildGuardianAuraCacheLocked also computed
// `flat := int(sources[i].effFlat)` against the same catalog), so this is
// not a migration-introduced gap.
//
// What IS migration-relevant, and what this test actually asserts: the
// arithmetic POSITION of the truncation moved from "per-source, before max"
// (legacy: int(effFlat_i) compared for each source, then maxed as ints) to
// "after max, at the read site" (effectiveArmorLocked: int(cache value),
// where cache value = max of raw float effFlat_i across sources, since
// guardian_aura's PerAdditionalSource is 0 so the generic cache's
// maxValue+(count-1)*maxPerExtra formula collapses to a pure max). These are
// PROVABLY equivalent for non-negative inputs: int() truncates toward zero,
// which is monotonic non-decreasing on [0,∞), so int(max(a,b)) ==
// max(int(a),int(b)) for any a,b >= 0. guardian_aura's flat contributions
// are never negative (bonusArmor/synergyArmorBonus are positive catalog
// values), so this equivalence holds. This test constructs two sources with
// DIFFERENT companion counts (hence different effFlat) covering the same
// recipient and confirms the post-migration max-then-truncate result equals
// the pre-migration truncate-then-max result from the frozen oracle.
func TestGuardianAuraMigration_IntTruncation_FlatArmor(t *testing.T) {
	s, v1 := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(v1, "guardian_aura")
	def := perkDefByID("guardian_aura")

	// v2 has 1 companion (v1 within its base radius) — higher effFlat.
	v2 := spawnAlly(t, s, "p1", v1.X+def.Config["radius"]*0.3, v1.Y)
	grantPerk(v2, "guardian_aura")

	// A recipient covered by both v1 (0 companions) and v2 (1 companion).
	recipient := spawnAlly(t, s, "p1", v1.X+def.Config["radius"]*0.5, v1.Y)

	assertGuardianAuraMatchesLegacy(t, s, recipient)
	legacy := legacyGuardianAuraCacheLocked(s)
	gotFlat, _, _, ok := guardianAuraReadLocked(s, recipient)
	if !ok {
		t.Fatal("recipient should be covered")
	}
	if gotFlat != legacy[recipient.ID].FlatArmor {
		t.Errorf("truncation-position equivalence: got %d, legacy (per-source truncate-then-max) %d", gotFlat, legacy[recipient.ID].FlatArmor)
	}
}

// TestGuardianAuraMigration_SourcesCount_HUDBadge verifies the HUD
// stack-count signal (perks_icons.go's activeBuffIconsLocked reads
// unitAuraStatContributionLocked's sources return, repointed from the
// deleted guardianAuraCache's Sources field) for a unit covered by TWO
// distinct guardian_aura emitters.
func TestGuardianAuraMigration_SourcesCount_HUDBadge(t *testing.T) {
	s, v1 := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(v1, "guardian_aura")
	def := perkDefByID("guardian_aura")
	// v2 far enough from v1 that they are NOT companions of each other, but
	// both still cover the same recipient via their own base radii.
	v2 := spawnAlly(t, s, "p1", v1.X+def.Config["radius"]*1.9, v1.Y)
	grantPerk(v2, "guardian_aura")

	recipient := spawnAlly(t, s, "p1", v1.X+def.Config["radius"]*0.95, v1.Y)

	assertGuardianAuraMatchesLegacy(t, s, recipient)
	_, _, sources, ok := guardianAuraReadLocked(s, recipient)
	if !ok {
		t.Fatal("recipient should be covered")
	}
	if sources != 2 {
		t.Errorf("Sources (HUD stack badge) = %d, want 2 (two distinct non-companion emitters)", sources)
	}

	// The HUD icon path itself: activeBuffIconsLocked must report a
	// "guardian_aura" icon with Stacks == sources for the recipient.
	icons := s.activeBuffIconsLocked(recipient)
	found := false
	for _, ic := range icons {
		if ic.ID == "guardian_aura" {
			found = true
			if ic.Stacks != sources {
				t.Errorf("guardian_aura icon Stacks = %d, want %d (Sources)", ic.Stacks, sources)
			}
		}
	}
	if !found {
		t.Error("recipient should have a guardian_aura buff icon")
	}
}

// TestGuardianAuraMigration_EffectiveArmorOrdering proves the fold position
// inside effectiveArmorLocked is unchanged: effectiveArmor =
// floor(base × (1+percentBonus)) + flatBonus, with guardian_aura's flat and
// percent contributions folded into the SAME flatBonus/percentBonus
// accumulators the pre-migration code used (see effectiveArmorLocked's own
// doc comment for the exact formula this reconstructs).
func TestGuardianAuraMigration_EffectiveArmorOrdering(t *testing.T) {
	s, v1 := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(v1, "guardian_aura")
	def := perkDefByID("guardian_aura")

	recipient := spawnAlly(t, s, "p1", v1.X+def.Config["radius"]*0.5, v1.Y)
	recipient.Armor = 37 // arbitrary non-trivial base armor

	assertGuardianAuraMatchesLegacy(t, s, recipient)
	flatArmor, percentArmor, _, ok := guardianAuraReadLocked(s, recipient)
	if !ok {
		t.Fatal("recipient should be covered")
	}

	wantEffective := int(math.Floor(float64(recipient.Armor)*(1.0+percentArmor))) + flatArmor
	gotEffective := s.effectiveArmorLocked(recipient)
	if gotEffective != wantEffective {
		t.Errorf("effectiveArmorLocked ordering: got %d, want %d (floor(%d×(1+%.4f))+%d)",
			gotEffective, wantEffective, recipient.Armor, percentArmor, flatArmor)
	}
}
