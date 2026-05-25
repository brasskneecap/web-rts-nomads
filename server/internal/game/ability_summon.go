package game

import (
	"log/slog"

	"webrts/server/pkg/protocol"
)

// summonOffsetY is the fixed vertical pixel offset at which a summoned unit
// appears below its caster. A constant (not RNG-derived) so summoning
// preserves tick determinism.
const summonOffsetY = 48.0

// spawnSummonedUnitLocked spawns a unit of def.SummonUnitType near the caster,
// owned by the same player and rendered in the caster's color. Invoked from
// resolveAbilityCastOnTargetLocked when an ability has SummonUnitType set
// (raise_skeleton is the first such ability).
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
	spawn := protocol.Vec2{X: caster.X, Y: caster.Y + summonOffsetY}
	s.spawnPlayerUnitLocked(def.SummonUnitType, caster.OwnerID, caster.Color, spawn)
}
