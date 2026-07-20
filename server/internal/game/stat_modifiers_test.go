package game

import (
	"math"
	"testing"
)

// TestApplyStatStages covers the stage-fold math in isolation: base-only
// add, base-only multiply, a combined base add+multiply, a final-stage
// multiply applied strictly AFTER a base-stage multiply (proving stage
// ORDER matters, not just the accumulated numbers), and the empty/nil-map
// identity case relied on by every stat-read fold site.
func TestApplyStatStages(t *testing.T) {
	const eps = 1e-9

	t.Run("nil stages is identity", func(t *testing.T) {
		got := applyStatStages(10, nil)
		if math.Abs(got-10) > eps {
			t.Fatalf("got %v, want 10 (base unchanged)", got)
		}
	})

	t.Run("empty stages map is identity", func(t *testing.T) {
		got := applyStatStages(10, map[string]statStageAccum{})
		if math.Abs(got-10) > eps {
			t.Fatalf("got %v, want 10 (base unchanged)", got)
		}
	})

	t.Run("base-only add", func(t *testing.T) {
		stages := map[string]statStageAccum{
			statStageBase: {Add: 5, Mul: 1},
		}
		got := applyStatStages(10, stages)
		if math.Abs(got-15) > eps {
			t.Fatalf("got %v, want 15", got)
		}
	})

	t.Run("base-only multiply", func(t *testing.T) {
		stages := map[string]statStageAccum{
			statStageBase: {Add: 0, Mul: 2},
		}
		got := applyStatStages(10, stages)
		if math.Abs(got-20) > eps {
			t.Fatalf("got %v, want 20", got)
		}
	})

	t.Run("base add and multiply combined", func(t *testing.T) {
		stages := map[string]statStageAccum{
			statStageBase: {Add: 10, Mul: 2},
		}
		// (10 + 10) * 2 = 40
		got := applyStatStages(10, stages)
		if math.Abs(got-40) > eps {
			t.Fatalf("got %v, want 40", got)
		}
	})

	t.Run("final multiply applies strictly after base multiply — order matters", func(t *testing.T) {
		stages := map[string]statStageAccum{
			statStageBase:  {Add: 10, Mul: 2},
			statStageFinal: {Add: 0, Mul: 2},
		}
		// ((10 + 10) * 2) * 2 = 80 — NOT 10 + 10*2*2 = 50, and NOT a single
		// pooled multiply of 10*4=40 — proves base folds completely before
		// final is applied at all.
		got := applyStatStages(10, stages)
		if math.Abs(got-80) > eps {
			t.Fatalf("got %v, want 80 (order-sensitive fold)", got)
		}
	})

	t.Run("final stage alone, no base contribution", func(t *testing.T) {
		stages := map[string]statStageAccum{
			statStageFinal: {Add: 5, Mul: 1},
		}
		got := applyStatStages(10, stages)
		if math.Abs(got-15) > eps {
			t.Fatalf("got %v, want 15", got)
		}
	})

	t.Run("intrinsic, base, and final stages combined — full three-stage order", func(t *testing.T) {
		stages := map[string]statStageAccum{
			statStageIntrinsic: {Add: 0, Mul: 2},
			statStageBase:      {Add: 10, Mul: 2},
			statStageFinal:     {Add: 0, Mul: 2},
		}
		// base=10: intrinsic ×2 → 20; base (+10)×2 → 60; final ×2 → 120.
		// ((10×2 + 10) × 2) × 2 = 120.
		got := applyStatStages(10, stages)
		if math.Abs(got-120) > eps {
			t.Fatalf("got %v, want 120 (intrinsic → base → final order-sensitive fold)", got)
		}
	})

	t.Run("intrinsic-only multiply equals base × mul, unaffected by absent base/final", func(t *testing.T) {
		stages := map[string]statStageAccum{
			statStageIntrinsic: {Add: 0, Mul: 3},
		}
		got := applyStatStages(10, stages)
		if math.Abs(got-30) > eps {
			t.Fatalf("got %v, want 30 (base × intrinsic mul)", got)
		}
	})

	t.Run("intrinsic multiply does NOT scale a base-stage additive term", func(t *testing.T) {
		// This is the exact shape that motivated the intrinsic stage: an
		// intrinsic ×1.15 (e.g. hawk_spirit's damage multiplier) must scale
		// ONLY the unit's own base value, never a base-stage add (e.g. a
		// zone aura's flat damage bonus) folded in afterward.
		var base, intrinsicMul, baseAdd, baseMul float64 = 8, 1.15, 8, 1.2
		stages := map[string]statStageAccum{
			statStageIntrinsic: {Add: 0, Mul: intrinsicMul},
			statStageBase:      {Add: baseAdd, Mul: baseMul},
		}
		// (8 × 1.15 + 8) × 1.2 = 20.639999999999997 — NOT (8+8) × 1.15 × 1.2,
		// which would scale the base-stage add by the intrinsic multiplier too.
		// want is computed with the same runtime float64 ops applyStatStages
		// performs (not compiler constant-folded) so this is an exact-equality
		// check, not an epsilon one.
		got := applyStatStages(base, stages)
		want := (base*intrinsicMul + baseAdd) * baseMul
		if got != want {
			t.Fatalf("got %v, want %v (exact — this is the hawk_spirit zone-aura shape)", got, want)
		}
	})

	t.Run("omitting intrinsic reproduces the pre-intrinsic base+final result exactly — no regression", func(t *testing.T) {
		// Identical to the "final multiply applies strictly after base
		// multiply" case above, proving that a stages map with no intrinsic
		// entry folds byte-identically now that statStages has grown a third
		// (leading) entry — already-migrated perks that only ever author
		// base/final stages must see zero change.
		stages := map[string]statStageAccum{
			statStageBase:  {Add: 10, Mul: 2},
			statStageFinal: {Add: 0, Mul: 2},
		}
		got := applyStatStages(10, stages)
		if math.Abs(got-80) > eps {
			t.Fatalf("got %v, want 80 (intrinsic absent must be a true no-op)", got)
		}
	})
}

// TestMergeZoneIntoBaseStage covers the merge helper every fold site uses to
// combine a zone-aura (add, mul) pair with a perk stat-modifier pool before
// calling applyStatStages.
func TestMergeZoneIntoBaseStage(t *testing.T) {
	const eps = 1e-9

	t.Run("identity zone pair returns stages unchanged (nil stays nil)", func(t *testing.T) {
		got := mergeZoneIntoBaseStage(nil, 0, 1)
		if got != nil {
			t.Fatalf("got %+v, want nil", got)
		}
	})

	t.Run("identity zone pair leaves a populated map untouched", func(t *testing.T) {
		in := map[string]statStageAccum{statStageFinal: {Add: 5, Mul: 1}}
		got := mergeZoneIntoBaseStage(in, 0, 1)
		if len(got) != 1 || got[statStageFinal] != (statStageAccum{Add: 5, Mul: 1}) {
			t.Fatalf("got %+v, want unchanged", got)
		}
	})

	t.Run("non-identity zone pair seeds a nil map's base stage", func(t *testing.T) {
		got := mergeZoneIntoBaseStage(nil, 5, 2)
		acc, ok := got[statStageBase]
		if !ok {
			t.Fatalf("want base stage populated, got %+v", got)
		}
		if math.Abs(acc.Add-5) > eps || math.Abs(acc.Mul-2) > eps {
			t.Fatalf("got %+v, want {Add:5 Mul:2}", acc)
		}
	})

	t.Run("non-identity zone pair combines with an existing base-stage perk entry", func(t *testing.T) {
		in := map[string]statStageAccum{statStageBase: {Add: 3, Mul: 2}}
		got := mergeZoneIntoBaseStage(in, 5, 2)
		acc := got[statStageBase]
		if math.Abs(acc.Add-8) > eps || math.Abs(acc.Mul-4) > eps {
			t.Fatalf("got %+v, want {Add:8 Mul:4} (adds sum, muls multiply)", acc)
		}
	})

	t.Run("non-identity zone pair does not disturb an existing final-stage entry", func(t *testing.T) {
		in := map[string]statStageAccum{statStageFinal: {Add: 1, Mul: 3}}
		got := mergeZoneIntoBaseStage(in, 5, 2)
		if got[statStageFinal] != (statStageAccum{Add: 1, Mul: 3}) {
			t.Fatalf("final stage got %+v, want unchanged {Add:1 Mul:3}", got[statStageFinal])
		}
		base := got[statStageBase]
		if math.Abs(base.Add-5) > eps || math.Abs(base.Mul-2) > eps {
			t.Fatalf("base stage got %+v, want {Add:5 Mul:2}", base)
		}
	})
}
