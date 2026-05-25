package game

import (
	"log/slog"

	"webrts/server/pkg/protocol"
)

// summonOffsetY is the fixed vertical pixel offset at which a summoned unit
// appears below its caster. summonOffsetX is the horizontal spacing between
// adjacent spawns when an ability summons more than one unit per cast (e.g.
// raise_skeleton, summonCount = 3). Both constants — no RNG — so summoning
// preserves tick determinism.
const (
	summonOffsetY = 48.0
	summonOffsetX = 48.0
)

// spawnSummonedUnitLocked spawns def.SummonCount units of def.SummonUnitType
// near the caster, owned by the same player and rendered in the caster's
// color. Invoked from resolveAbilityCastOnTargetLocked when an ability has
// SummonUnitType set (raise_skeleton is the first such ability).
//
// For SummonCount > 1 the spawns are fanned out in a horizontal row below the
// caster, centered on the caster's X. The offset for index i in a row of N is:
//
//	dx = (i - (N-1)/2.0) * summonOffsetX
//
// so N=1 reproduces the original "directly below" behaviour and N=3 places
// the spawns at (X-48, X, X+48) below the caster. Deterministic, no RNG.
//
// An unknown SummonUnitType is logged once and the call returns without
// spawning anything — mana has already been spent by resolveAbilityCastLocked
// at this point, so this matches the "silent miss, channel survives" semantic
// the existing projectile/effect catalog lookups use.
//
// Caller holds s.mu.
func (s *GameState) spawnSummonedUnitLocked(caster *Unit, def AbilityDef) {
	if caster == nil || def.SummonUnitType == "" {
		return
	}
	if _, ok := getUnitDef(def.SummonUnitType); !ok {
		slog.Warn("ability summon: unknown unit type",
			"ability", def.ID,
			"unit_type", def.SummonUnitType,
			"caster_id", caster.ID,
		)
		return
	}
	count := def.SummonCount
	if count < 1 {
		count = 1 // defensive: loader normalises this, but never trust a 0 here
	}
	mid := float64(count-1) / 2.0
	for i := 0; i < count; i++ {
		dx := (float64(i) - mid) * summonOffsetX
		spawn := protocol.Vec2{X: caster.X + dx, Y: caster.Y + summonOffsetY}
		s.spawnPlayerUnitLocked(def.SummonUnitType, caster.OwnerID, caster.Color, spawn)
	}
}
