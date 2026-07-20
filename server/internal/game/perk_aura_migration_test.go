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
