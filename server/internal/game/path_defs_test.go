package game

import "testing"

// TestPathCatalog_ShippedPathsHaveAllRanks pins the structural invariants —
// every shipped promotion path has all three rank rows loaded with positive
// multipliers. Deliberately does NOT pin specific numbers; that's balance
// work and those edits happen in JSON. A failure here means either a rank
// row is missing from a path's JSON, the loader dropped it, or a multiplier
// accidentally went to zero/negative (which would nerf the unit to nothing).
func TestPathCatalog_ShippedPathsHaveAllRanks(t *testing.T) {
	paths := []string{unitPathVanguard, unitPathBerserker, unitPathTrapper, unitPathMarksman, unitPathCleric, unitPathArchMage}
	ranks := []string{unitRankBronze, unitRankSilver, unitRankGold}

	for _, p := range paths {
		for _, r := range ranks {
			mod := pathModifierFor(p, r)
			if mod == identityPathModifier {
				t.Errorf("%s/%s resolved to identityPathModifier — missing catalog row?", p, r)
				continue
			}
			if mod.Path != p || mod.Rank != r {
				t.Errorf("%s/%s row has wrong (Path,Rank): got (%s,%s)", p, r, mod.Path, mod.Rank)
			}
			if mod.MaxHPMultiplier <= 0 {
				t.Errorf("%s/%s MaxHPMultiplier must be > 0; got %.3f", p, r, mod.MaxHPMultiplier)
			}
			if mod.DamageMultiplier <= 0 {
				t.Errorf("%s/%s DamageMultiplier must be > 0; got %.3f", p, r, mod.DamageMultiplier)
			}
			if mod.AttackSpeedMultiplier <= 0 {
				t.Errorf("%s/%s AttackSpeedMultiplier must be > 0; got %.3f", p, r, mod.AttackSpeedMultiplier)
			}
			if mod.MoveSpeedMultiplier <= 0 {
				t.Errorf("%s/%s MoveSpeedMultiplier must be > 0; got %.3f", p, r, mod.MoveSpeedMultiplier)
			}
			if mod.Armor < 0 {
				t.Errorf("%s/%s Armor must be >= 0; got %d", p, r, mod.Armor)
			}
		}
	}
}

// TestPathCatalog_NoneUsesGoDefaultCurve confirms that the unitPathNone
// branch in pathModifierFor routes through defaultRankCurve (Go code) and
// not the JSON catalog. "none" is a system fallback for path-less ranked
// units (workers etc.), not a player-facing path.
func TestPathCatalog_NoneUsesGoDefaultCurve(t *testing.T) {
	for _, r := range []string{unitRankBronze, unitRankSilver, unitRankGold} {
		got := pathModifierFor(unitPathNone, r)
		want := defaultRankCurve[r]
		if got != want {
			t.Errorf("%s: got %+v, want %+v (must come from defaultRankCurve)", r, got, want)
		}
	}
}

// TestPathCatalog_BaseRankAlwaysIdentity locks the one path-agnostic rule:
// units at base rank get identity multipliers regardless of path. Handled
// by the base-rank short-circuit inside pathModifierFor, so it keeps
// working even if someone adds a base-rank block to a JSON file (which
// the loader explicitly rejects via validRankName).
func TestPathCatalog_BaseRankAlwaysIdentity(t *testing.T) {
	paths := []string{unitPathNone, unitPathVanguard, unitPathBerserker, unitPathTrapper, unitPathMarksman, unitPathCleric, unitPathArchMage}
	for _, path := range paths {
		got := pathModifierFor(path, unitRankBase)
		if got != identityPathModifier {
			t.Errorf("base rank for %q: got %+v, want identity %+v", path, got, identityPathModifier)
		}
	}
}

// TestPathCatalog_UnknownPathFallsBackToIdentity confirms the loader's
// "fail loud" contract: a typo in a path id does NOT accidentally match
// another row. The caller gets identity, so affected units appear in-game
// with unmodified base stats — obvious to QA, not a silent mis-match.
func TestPathCatalog_UnknownPathFallsBackToIdentity(t *testing.T) {
	got := pathModifierFor("not_a_real_path", unitRankSilver)
	if got != identityPathModifier {
		t.Errorf("unknown path should fall back to identity; got %+v", got)
	}
}
