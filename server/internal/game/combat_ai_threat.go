package game

import (
	"math"
	"webrts/server/pkg/protocol"
)

func (s *GameState) decayThreatLocked(unit *Unit, dt float64, index *combatSpatialIndex) {
	profile := resolveCombatProfile(unit)
	if unit.TauntRemaining > 0 {
		unit.TauntRemaining = math.Max(0, unit.TauntRemaining-dt)
		if unit.TauntRemaining == 0 {
			unit.TauntedByUnitID = 0
		}
	}

	for hostileID, entry := range unit.ThreatTable {
		hostile := s.getUnitByIDLocked(hostileID)
		if hostile == nil || !hostile.Visible || hostile.HP <= 0 || !s.playersAreHostileLocked(hostile.OwnerID, unit.OwnerID) {
			delete(unit.ThreatTable, hostileID)
			continue
		}

		detectRange := effectiveDetectionRange(unit, profile)
		if distanceSquared(unit.X, unit.Y, hostile.X, hostile.Y) <= detectRange*detectRange {
			entry.LastSeenTick = s.Tick
		}

		decayRate := profile.ThreatDecayPerSecond
		if s.Tick-entry.LastSeenTick > combatThreatVisibilityForgetTicks {
			decayRate *= 2
		}
		entry.Value = math.Max(0, entry.Value-decayRate*dt)
		if entry.Value <= 0.01 {
			delete(unit.ThreatTable, hostileID)
		}
	}

	if !profile.Melee || profile.PassiveMeleeThreat <= 0 {
		return
	}
	for _, hostile := range index.query(unit.X, unit.Y, combatMeleeProximityRadius) {
		if !s.playersAreHostileLocked(hostile.OwnerID, unit.OwnerID) || hostile.HP <= 0 {
			continue
		}
		s.addThreatLocked(unit, hostile, profile.PassiveMeleeThreat*dt, false)
	}
}

func (s *GameState) getThreatValueLocked(unit *Unit, hostileID int) float64 {
	if entry, ok := unit.ThreatTable[hostileID]; ok {
		return entry.Value
	}
	return 0
}

func (s *GameState) addThreatLocked(unit, hostile *Unit, amount float64, forceSeen bool) {
	if unit == nil || hostile == nil || !s.playersAreHostileLocked(unit.OwnerID, hostile.OwnerID) || amount <= 0 {
		return
	}
	s.initializeCombatUnitLocked(unit)
	entry := unit.ThreatTable[hostile.ID]
	if entry == nil {
		entry = &ThreatEntry{}
		unit.ThreatTable[hostile.ID] = entry
	}
	entry.Value += amount
	detectRange := effectiveDetectionRange(unit, resolveCombatProfile(unit))
	if forceSeen || distanceSquared(unit.X, unit.Y, hostile.X, hostile.Y) <= detectRange*detectRange {
		entry.LastSeenTick = s.Tick
	}
	entry.LastActiveTick = s.Tick
}

func (s *GameState) onUnitDamagedLocked(attacker, target *Unit, damage int) {
	if attacker == nil || target == nil || damage <= 0 {
		return
	}
	amount := float64(damage) * resolveCombatProfile(target).ThreatFromDamage * resolveCombatProfile(attacker).ThreatGenerationMultiplier
	s.addThreatLocked(target, attacker, amount, true)

	for _, ally := range s.Units {
		if !s.unitsFriendlyLocked(ally, target) || ally.ID == target.ID || ally.HP <= 0 || !ally.Visible {
			continue
		}
		if distanceSquared(ally.X, ally.Y, target.X, target.Y) > combatBacklineDefenseRadius*combatBacklineDefenseRadius {
			continue
		}
		bonus := float64(damage) * 0.2
		if resolveCombatProfile(ally).Frontline {
			bonus *= 1.5
		}
		s.addThreatLocked(ally, attacker, bonus, true)
	}
}

func (s *GameState) onBuildingDamagedLocked(attacker *Unit, building *protocol.BuildingTile, damage int) {
	if attacker == nil || building == nil || damage <= 0 || building.OwnerID == nil {
		return
	}
	for _, ally := range s.Units {
		if !s.playersAreFriendlyLocked(ally.OwnerID, *building.OwnerID) || ally.HP <= 0 || !ally.Visible {
			continue
		}
		if distanceSquared(ally.X, ally.Y, attacker.X, attacker.Y) > combatThreatStructureSplashRadius*combatThreatStructureSplashRadius {
			continue
		}
		bonus := float64(damage) * 0.35
		if resolveCombatProfile(ally).Frontline {
			bonus *= 1.35
		}
		s.addThreatLocked(ally, attacker, bonus, true)
	}
}

func (s *GameState) AddSupportThreatLocked(source *Unit, center protocol.Vec2, radius, baseThreat float64) {
	if source == nil || baseThreat <= 0 {
		return
	}
	for _, unit := range s.Units {
		if !s.playersAreHostileLocked(unit.OwnerID, source.OwnerID) || unit.HP <= 0 || !unit.Visible {
			continue
		}
		if distanceSquared(unit.X, unit.Y, center.X, center.Y) > radius*radius {
			continue
		}
		s.addThreatLocked(unit, source, baseThreat*resolveCombatProfile(source).ThreatGenerationMultiplier, true)
	}
}

func (s *GameState) ApplyTauntLocked(targetUnitID, taunterUnitID int, duration float64) {
	target := s.getUnitByIDLocked(targetUnitID)
	taunter := s.getUnitByIDLocked(taunterUnitID)
	if target == nil || taunter == nil || !s.playersAreHostileLocked(target.OwnerID, taunter.OwnerID) || duration <= 0 {
		return
	}
	target.TauntedByUnitID = taunterUnitID
	target.TauntRemaining = duration
	s.addThreatLocked(target, taunter, 60, true)
}
