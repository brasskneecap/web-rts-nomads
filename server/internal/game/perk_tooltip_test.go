package game

// ═════════════════════════════════════════════════════════════════════════════
// PERK TOOLTIP QA TESTS
//
// Covers four areas mandated by the tooltip-feature QA sweep:
//   A. Fire pit rank-scaling regression (the bug that was just fixed).
//   B. caltrops + wider_nets stacking scenario.
//   C. Non-trapper unit returns nil from EffectiveTrapSnapshotLocked.
//   D. No-bronze-trap unit returns nil from EffectiveTrapSnapshotLocked.
//   E. Meta-test: every tooltipTemplate token references a real config key
//      (or a known EffectiveTrapSnapshot field for {trap.*} tokens).
// ═════════════════════════════════════════════════════════════════════════════

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"webrts/server/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────────────────────
// helpers shared by this file
// ─────────────────────────────────────────────────────────────────────────────

// newTooltipState is identical to newTrapSilverState — a minimal GameState with
// player "p1" registered and the lock released.
func newTooltipState(t *testing.T) *GameState {
	t.Helper()
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 77)
	s.mu.Lock()
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.mu.Unlock()
	return s
}

// spawnUnitWithPerks spawns a unit of the given unitType for "p1", assigns the
// supplied perkIDs and rank, and returns the unit with the lock held.
// The caller MUST hold s.mu before calling, or use the helper pattern below.
func spawnUnitWithPerks(s *GameState, unitType string, rank string, perkIDs []string) *Unit {
	u := s.spawnPlayerUnitLocked(unitType, "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if u == nil {
		// fall back for environments where archer catalog may be absent
		u = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	}
	if u == nil {
		return nil
	}
	u.Visible = true
	u.Rank = rank
	u.PerkIDs = append([]string{}, perkIDs...)
	return u
}

// ─────────────────────────────────────────────────────────────────────────────
// A. Fire pit rank scaling — regression test for ConfigForRank fix
// ─────────────────────────────────────────────────────────────────────────────

func TestEffectiveTrapSnapshot_Caltrops_WiderNets_Radius(t *testing.T) {
	s := newTooltipState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := spawnUnitWithPerks(s, "archer", unitRankBronze, []string{"wider_nets"})
	if u == nil {
		t.Fatal("failed to spawn unit")
	}
	grantTrapAbility(u, "caltrops")

	snap := s.EffectiveTrapSnapshotLocked(u)
	if snap == nil {
		t.Fatal("EffectiveTrapSnapshotLocked returned nil for caltrops + wider_nets unit")
	}

	caltropsRadius := mustTrapAbilityConfig(t, "caltrops", unitRankBronze).Radius
	widerNets := perkDefByID("wider_nets").Config["radiusMultiplier"]
	assertFloatEq(t, "Radius (caltrops + wider_nets)", snap.Radius, caltropsRadius*widerNets)
}

// TestEffectiveTrapSnapshot_Caltrops_NoModifier_Radius verifies caltrops without
// wider_nets keeps radius at 60.
func TestEffectiveTrapSnapshot_Caltrops_NoModifier_Radius(t *testing.T) {
	s := newTooltipState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := spawnUnitWithPerks(s, "archer", unitRankBronze, nil)
	if u == nil {
		t.Fatal("failed to spawn unit")
	}
	grantTrapAbility(u, "caltrops")

	snap := s.EffectiveTrapSnapshotLocked(u)
	if snap == nil {
		t.Fatal("EffectiveTrapSnapshotLocked returned nil for caltrops-only unit")
	}

	caltropsRadius := mustTrapAbilityConfig(t, "caltrops", unitRankBronze).Radius
	assertFloatEq(t, "Radius (caltrops, no modifier)", snap.Radius, caltropsRadius)
}

// ─────────────────────────────────────────────────────────────────────────────
// C. Non-trapper unit returns nil
// ─────────────────────────────────────────────────────────────────────────────

// TestEffectiveTrapSnapshot_NonTrapper_ReturnsNil verifies that a soldier with
// bloodlust (a non-trap perk) returns nil — the gate preserves the contract.
func TestEffectiveTrapSnapshot_NonTrapper_ReturnsNil(t *testing.T) {
	s := newTooltipState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 300, Y: 300})
	if u == nil {
		t.Fatal("failed to spawn soldier unit")
	}
	u.PerkIDs = []string{"bloodlust"}
	u.Rank = unitRankBronze

	snap := s.EffectiveTrapSnapshotLocked(u)
	if snap != nil {
		t.Errorf("EffectiveTrapSnapshotLocked returned non-nil for soldier with bloodlust; want nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// D. Archer with only silver perks (no bronze trap anchor) returns nil
// ─────────────────────────────────────────────────────────────────────────────

// TestEffectiveTrapSnapshot_NoBronzeTrapPerk_ReturnsNil verifies that a trapper
// archer that owns only silver perks (no caltrops/fire_pit/explosive_trap/marker_trap)
// returns nil — the helper requires a bronze trap perk to anchor the snapshot.
func TestEffectiveTrapSnapshot_NoBronzeTrapPerk_ReturnsNil(t *testing.T) {
	s := newTooltipState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := spawnUnitWithPerks(s, "archer", unitRankSilver, []string{"wider_nets", "extended_setup"})
	if u == nil {
		t.Fatal("failed to spawn unit")
	}

	snap := s.EffectiveTrapSnapshotLocked(u)
	if snap != nil {
		t.Errorf("EffectiveTrapSnapshotLocked returned non-nil for archer with no bronze trap perk; want nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// E. Meta-test: tooltipTemplate token → config key validation
// ─────────────────────────────────────────────────────────────────────────────

// tokenRe matches any {…} token in a tooltipTemplate.
var tokenRe = regexp.MustCompile(`\{([^}]+)\}`)

// extractTokenKey strips format suffixes from a raw token string:
//
//	"key%"   → "key"
//	"key+%"  → "key"
//	"key:N"  → "key"
//	"key"    → "key"
func extractTokenKey(raw string) string {
	// strip :N
	if idx := strings.IndexByte(raw, ':'); idx != -1 {
		raw = raw[:idx]
	}
	// strip +%
	raw = strings.TrimSuffix(raw, "+%")
	// strip %
	raw = strings.TrimSuffix(raw, "%")
	return raw
}

// TestTooltipTemplate_AllTokensReferenceValidKeys is a table-driven meta-test
// that walks every PerkDef in the catalog and validates each token in its
// tooltipTemplate against its config. It catches author typos before they
// reach the client and produce silent empty-string substitutions.
func TestTooltipTemplate_AllTokensReferenceValidKeys(t *testing.T) {
	allRanks := []string{unitRankBronze, unitRankSilver, unitRankGold}

	for _, def := range ListPerkDefs() {
		def := def // capture
		t.Run(fmt.Sprintf("perk=%s", def.ID), func(t *testing.T) {
			// Build the merged config across all ranks for this perk once;
			// shared across every template variant we validate below.
			mergedConfig := make(map[string]float64)
			for k, v := range def.Config {
				mergedConfig[k] = v
			}
			for _, rank := range allRanks {
				for k, v := range def.ConfigByRank[rank] {
					mergedConfig[k] = v
				}
			}

			// Collect every template variant to validate: the base
			// tooltipTemplate plus any by-trap / by-owned-perk variants.
			// Each entry carries a label so error messages identify which
			// variant has the bad token.
			type templateVariant struct {
				label    string
				template string
			}
			variants := make([]templateVariant, 0, 4)
			if def.TooltipTemplate != "" {
				variants = append(variants, templateVariant{label: "tooltipTemplate", template: def.TooltipTemplate})
			}
			for trapKey, body := range def.TooltipTemplateByTrap {
				variants = append(variants, templateVariant{
					label:    fmt.Sprintf("tooltipTemplateByTrap[%q]", trapKey),
					template: body,
				})
			}
			for ownedKey, body := range def.TooltipTemplateByOwnedPerk {
				variants = append(variants, templateVariant{
					label:    fmt.Sprintf("tooltipTemplateByOwnedPerk[%q]", ownedKey),
					template: body,
				})
			}
			if len(variants) == 0 {
				t.Skip("no template variants — static description only")
			}

			for _, v := range variants {
				matches := tokenRe.FindAllStringSubmatch(v.template, -1)
				for _, match := range matches {
					raw := match[1] // e.g. "key%", "key:2"
					key := extractTokenKey(raw)

					// Every remaining token must exist in the merged config. The
					// {trap.*} token family (validated against
					// protocol.EffectiveTrapSnapshot) was retired along with the
					// four bronze trap perks (caltrops/fire_pit/explosive_trap/
					// marker_trap) — they are now pool abilities, whose place_trap
					// action config carries no tooltipTemplate, so no catalog perk
					// should author a {trap.*} token anymore. If one reappears it
					// will fail here for not being a config key, which is correct.
					if _, ok := mergedConfig[key]; !ok {
						t.Errorf("%s — {%s}: perk %q — key %q not found in config (or any configByRank override)", v.label, raw, def.ID, key)
					}
				}
			}
		})
	}
}
