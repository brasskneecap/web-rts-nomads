package game

import "testing"

// TestPathCatalog_MultipliersMatchLegacyStackedValues pins every (path, rank)
// in catalog/paths/*.json to the EXACT numbers the game produced under the
// old `rankProgression × pathModifier` stacking scheme, before the refactor
// to per-path JSON. Guards against accidental drift from a JSON edit: the
// catalog is the authoritative data source now, so a typo here would silently
// change in-game stats. Update this table INTENTIONALLY when balance is
// retuned — a failure here means "a JSON value changed"; confirm that was
// the intent before editing these expectations.
func TestPathCatalog_MultipliersMatchLegacyStackedValues(t *testing.T) {
	type want struct {
		hp, dmg, as, ms float64
		armor           int
	}

	// Legacy rank-only curve (for none + trapper until tuned):
	//   bronze: HP 1.10, Dmg 1.10, AS 1.00
	//   silver: HP 1.20, Dmg 1.25, AS 1.10
	//   gold:   HP 1.35, Dmg 1.50, AS 1.25
	//
	// Legacy path × rank products (vanguard, berserker).
	cases := []struct {
		name string
		path string
		rank string
		want want
	}{
		// vanguard
		{"vanguard/bronze", unitPathVanguard, unitRankBronze, want{1.210, 1.10, 0.950, 1.00, 54}},
		{"vanguard/silver", unitPathVanguard, unitRankSilver, want{1.440, 1.25, 1.100, 1.00, 54}},
		{"vanguard/gold", unitPathVanguard, unitRankGold, want{1.755, 1.65, 1.250, 1.00, 54}},
		// berserker
		{"berserker/bronze", unitPathBerserker, unitRankBronze, want{0.990, 1.21, 1.1000, 1.15, 18}},
		{"berserker/silver", unitPathBerserker, unitRankSilver, want{1.140, 1.50, 1.2650, 1.15, 18}},
		{"berserker/gold", unitPathBerserker, unitRankGold, want{1.350, 1.95, 1.5625, 1.15, 18}},
		// trapper (untuned, mirrors default curve)
		{"trapper/bronze", unitPathTrapper, unitRankBronze, want{1.10, 1.10, 1.00, 1.00, 0}},
		{"trapper/silver", unitPathTrapper, unitRankSilver, want{1.20, 1.25, 1.10, 1.00, 0}},
		{"trapper/gold", unitPathTrapper, unitRankGold, want{1.35, 1.50, 1.25, 1.00, 0}},
		// none (default curve for pathless ranked units)
		{"none/bronze", unitPathNone, unitRankBronze, want{1.10, 1.10, 1.00, 1.00, 0}},
		{"none/silver", unitPathNone, unitRankSilver, want{1.20, 1.25, 1.10, 1.00, 0}},
		{"none/gold", unitPathNone, unitRankGold, want{1.35, 1.50, 1.25, 1.00, 0}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pathModifierFor(tc.path, tc.rank)
			if got.MaxHPMultiplier != tc.want.hp {
				t.Errorf("MaxHPMultiplier: got %.4f, want %.4f", got.MaxHPMultiplier, tc.want.hp)
			}
			if got.DamageMultiplier != tc.want.dmg {
				t.Errorf("DamageMultiplier: got %.4f, want %.4f", got.DamageMultiplier, tc.want.dmg)
			}
			if got.AttackSpeedMultiplier != tc.want.as {
				t.Errorf("AttackSpeedMultiplier: got %.4f, want %.4f", got.AttackSpeedMultiplier, tc.want.as)
			}
			if got.MoveSpeedMultiplier != tc.want.ms {
				t.Errorf("MoveSpeedMultiplier: got %.4f, want %.4f", got.MoveSpeedMultiplier, tc.want.ms)
			}
			if got.Armor != tc.want.armor {
				t.Errorf("Armor: got %d, want %d", got.Armor, tc.want.armor)
			}
		})
	}
}

// TestPathCatalog_BaseRankAlwaysIdentity locks in the one path-agnostic rule:
// units at base rank get identity multipliers regardless of path. This path
// is handled by the base-rank short-circuit inside pathModifierFor, not the
// JSON catalog, so it must keep working even if someone adds a base-rank
// block to a JSON file (which the loader rejects via validRankName).
func TestPathCatalog_BaseRankAlwaysIdentity(t *testing.T) {
	paths := []string{unitPathNone, unitPathVanguard, unitPathBerserker, unitPathTrapper}
	for _, path := range paths {
		got := pathModifierFor(path, unitRankBase)
		if got != identityPathModifier {
			t.Errorf("base rank for %q: got %+v, want identity %+v", path, got, identityPathModifier)
		}
	}
}

// TestPathCatalog_UnknownPathFallsBackToIdentity confirms the loader's "fail
// loud" contract: a typo in a path id (e.g. "vangurad") does NOT accidentally
// match some other row. Instead the caller gets identity, so in-game the
// affected unit appears with its unmodified base stats — obvious in QA.
func TestPathCatalog_UnknownPathFallsBackToIdentity(t *testing.T) {
	got := pathModifierFor("not_a_real_path", unitRankSilver)
	if got != identityPathModifier {
		t.Errorf("unknown path should fall back to identity; got %+v", got)
	}
}
