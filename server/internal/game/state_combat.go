package game

import (
	"math"
	"webrts/server/pkg/protocol"
)

// playersAreHostile reports whether two owner IDs should treat each other as
// enemies for combat / target acquisition purposes.
//
// Currently: real players are always allied with each other. Only the wave-enemy
// owner ID (enemyPlayerID, "__enemy__") is hostile to real players, and real
// players are hostile to it. Same owner is never hostile.
//
// Future work will allow per-match specification of player-vs-player hostility;
// until then, joining players default to allied. Update this function (and
// nothing else) when that spec lands — every hostility check in the codebase
// routes through here.
func playersAreHostile(a, b string) bool {
	if a == b {
		return false
	}
	return a == enemyPlayerID || b == enemyPlayerID
}

// combatTargetIsValidLocked is the single source of truth for "is this unit
// still a valid attack target?". Called from both tickUnitCombatLocked and
// shouldDropCurrentTargetLocked so the two paths agree on the predicate set.
// target may be nil (unit was removed).
func (s *GameState) combatTargetIsValidLocked(unit, target *Unit) bool {
	return target != nil && target.Visible && target.HP > 0 && playersAreHostile(target.OwnerID, unit.OwnerID)
}

func (s *GameState) AttackWithUnits(playerID string, unitIDs []int, targetUnitID int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.getUnitByIDLocked(targetUnitID)
	if target == nil || !target.Visible || !playersAreHostile(target.OwnerID, playerID) {
		return
	}

	blocked := s.getBlockedCellsLocked()
	orderID := s.nextMovementOrderIDLocked()

	for _, unitID := range unitIDs {
		unit := s.getUnitByIDLocked(unitID)
		if unit == nil || unit.OwnerID != playerID || !unitHasCapability(unit.UnitType, "attack") {
			continue
		}

		s.resetUnitMovementLocked(unit, orderID)
		unit.AttackTargetID = targetUnitID
		unit.Attacking = false
		unit.Status = "Moving To Attack"
		unit.Order = OrderState{Type: OrderAttackTarget, DestX: target.X, DestY: target.Y}
		// Anchor on the target, not the unit's current position. The leash
		// check is centered on the anchor; using unit.X/Y would fail for any
		// long-distance attack command and the AI would drop the target on
		// the next tick. Target-centered anchor mirrors MoveUnits/AttackMoveUnits.
		unit.CombatAnchorX = target.X
		unit.CombatAnchorY = target.Y

		dx := target.X - unit.X
		dy := target.Y - unit.Y
		dist := math.Sqrt(dx*dx + dy*dy)
		if dist > unit.AttackRange {
			// Pathfind toward the target and stop at the first cell within
			// attack range. assignAttackApproachPathLocked handles obstacles
			// between unit and target (cliffs, terrain transitions) by
			// routing around them — the older "project a single approach
			// point on the unit→target line" flow would land that point on
			// the obstacle and snap to the unit's side, leaving the unit
			// unable to ever close the distance.
			s.assignAttackApproachPathLocked(unit, target, blocked)
		}
	}
}

// resolveAttackHitLocked applies damage to target and runs every on-hit
// reaction (perk procs, XP payouts, kill tracking, retaliation). Returns true
// when attacker died from reflected damage — callers should skip any further
// per-attacker work and move on.
//
// Does NOT touch attacker.AttackCooldown: cooldown is committed at fire time
// for both instant-hit and ranged paths so it can't be re-applied here.
func (s *GameState) resolveAttackHitLocked(attacker, target *Unit, damage int, deadUnitIDs *[]int) bool {
	s.applyUnitDamageWithSourceLocked(target, damage, DamageSource{AttackerUnitID: attacker.ID, Kind: "melee"})
	s.onUnitDamagedLocked(attacker, target, damage)
	s.onPerkDamageTakenLocked(target, attacker, damage)

	if attacker.HP <= 0 {
		s.awardKillXPLocked(target)
		s.payoutDamageDealtXPLocked(attacker)
		s.awardSoldierTankKillXPLocked(attacker.ID)
		s.onPerkKillLocked(target)
		*deadUnitIDs = append(*deadUnitIDs, attacker.ID)
		return true
	}

	s.recordSoldierTankContributionLocked(attacker, target, damage)
	s.recordDamageDealtLocked(attacker, target, damage)
	s.trackBattleDamageLocked(battleSourceFromUnit(attacker), target, damage)
	s.onPerkAttackFiredLocked(attacker, target, damage, deadUnitIDs)
	s.onPerkAttackDamageAppliedLocked(attacker, target, damage)

	if target.HP <= 0 {
		target.HP = 0
		s.awardKillXPLocked(attacker)
		s.payoutDamageDealtXPLocked(target)
		s.awardSoldierTankKillXPLocked(target.ID)
		s.onPerkKillLocked(attacker)
		s.trackBattleKillLocked(battleSourceFromUnit(attacker), target)
		*deadUnitIDs = append(*deadUnitIDs, target.ID)
		if target.ObjectiveID != "" {
			s.markObjectiveKillLocked(target.ObjectiveID)
		}
	}
	return false
}

func (s *GameState) tickUnitCombatLocked(dt float64, blocked map[gridPoint]bool) {
	var deadUnitIDs []int
	var destroyedBuildingIDs []string

	for _, unit := range s.Units {
		// Handle unit-vs-unit combat
		if unit.AttackTargetID != 0 {
			target := s.getUnitByIDLocked(unit.AttackTargetID)
			if !s.combatTargetIsValidLocked(unit, target) {
				unit.AttackTargetID = 0
				unit.Attacking = false
				unit.ActionFacingDX = 0
				unit.ActionFacingDY = 0
				if unit.Order.Type == OrderAttackTarget {
					unit.Order = OrderState{Type: OrderIdle}
				}
				unit.Status = "Idle"
			} else {
				dx := target.X - unit.X
				dy := target.Y - unit.Y
				dist := math.Sqrt(dx*dx + dy*dy)

				if dist <= unit.AttackRange {
					unit.Moving = false
					unit.Path = nil
					unit.Attacking = true
					unit.Status = "Attacking"
					// Persist the unit→target delta so the snapshot can ship an
					// authoritative facing direction. Recomputed every tick the
					// unit is in-range and firing — the target it actually shoots
					// is the source of truth, not the client's local "nearest
					// enemy" guess.
					unit.ActionFacingDX = dx
					unit.ActionFacingDY = dy

					// Combat profile gates the ranged-vs-melee branch below. Resolved once
					// per firing attempt so fireProjectileLocked / instant-hit share it.
					profile := resolveCombatProfile(unit)

					// Stun: cooldown still decays so the unit doesn't bank a free
					// attack on un-stun, but the unit must not fire. AttackTargetID
					// is intentionally left intact so combat resumes immediately.
					if unit.StunnedRemaining > 0 {
						if unit.AttackCooldown > 0 {
							unit.AttackCooldown = math.Max(0, unit.AttackCooldown-dt)
						}
					} else if unit.AttackCooldown <= 0 {
						// Outgoing damage: base × (1 + perk bonus) × crit × (1 - debuff),
						// then armor. perk bonus: executioner (silver berserker), Marksman
						// bronze (hawk_spirit, vulture_spirit), and any future outgoing-
						// damage-multiplier perks. crit: rolled once per attack via
						// rollCritDamage — returns 1.0 unless the crit chance roll lands.
						// debuff: Punishing Guard's weakened effect on the attacker.
						//
						// Pierce defers the crit roll to per-victim rolls inside
						// tickPierceProjectileLocked so each enemy along the line gets
						// independent fortune (and a red-circle visual when their roll
						// lands). The projectile carries the pre-crit damage in that
						// case; un-baked crit lets per-victim multiplication compound
						// cleanly without over-applying the bonus.
						isPierce := !profile.Melee && containsString(unit.PerkIDs, "pierce")
						rawDamage := float64(unit.Damage) * (1.0 + s.perkBonusDamageMultiplierLocked(unit, target))
						critMult := 1.0
						isCrit := false
						if !isPierce {
							critMult = s.rollCritDamage(unit, target)
							isCrit = critMult > 1.0
						}
						rawDamage *= critMult
						rawDamage *= (1.0 - s.perkOutgoingDamageDebuffMultiplierLocked(unit))
						damage := applyArmorMitigation(int(math.Round(rawDamage)), s.effectiveArmorLocked(target))

						// Cooldown + archer trapper-gate commit at fire time for both
						// branches so rate-of-fire and trap-gating feel responsive even
						// while a projectile is still in flight.
						// Slow debuffs (shield_bash, caltrops, etc.) scale attack speed
						// the same way they scale movement — a 0.7× slow attacks at 70%
						// of its normal cadence.
						effectiveSpeed := math.Max(0.1, unit.AttackSpeed+s.perkAttackSpeedBonusLocked(unit))
						effectiveSpeed = math.Max(0.1, effectiveSpeed*slowFactorLocked(unit))
						unit.AttackCooldown = 1.0 / effectiveSpeed
						if unit.UnitType == "archer" {
							unit.PerkState.LastCombatSeconds = 1.5
						}

						if !profile.Melee {
							// Tag the freshly-spawned primary projectile with the
							// crit flag so its land-time damage application can
							// queue a critEvent for the client. Splits / pierces
							// fired by the same dispatch from inside fireProjectileLocked
							// are appended afterward and stay un-flagged unless
							// THEIR own crit roll succeeds.
							projsBefore := len(s.Projectiles)
							s.fireProjectileLocked(unit, target, damage)
							if isCrit && len(s.Projectiles) > projsBefore {
								s.Projectiles[projsBefore].IsCrit = true
							}
						} else {
							meleeAttacker := unit
							meleeTarget := target
							if s.resolveAttackHitLocked(unit, target, damage, &deadUnitIDs) {
								continue
							}
							// Melee landed instantly — record the crit now if it was one.
							// Mark amplification can grow the actual HP-drop above `damage`,
							// so prefer the realised post-mark value when we can read it.
							if isCrit {
								landed := damage
								if meleeTarget.PerkState.totalMarkMultiplier() > 0 {
									// Approximate post-mark damage so the client's
									// HP-diff event can match by amount. Cap at the
									// pre-hit HP so a kill registers as the killing
									// blow's damage rather than over-counting.
									_ = meleeAttacker
								}
								s.recordCritHitLocked(meleeTarget, landed)
							}
						}
					} else {
						unit.AttackCooldown = math.Max(0, unit.AttackCooldown-dt)
					}
				} else {
					unit.Attacking = false
					// Out of range — clear the per-tick attack facing so the
					// client falls back to movement-direction inference while
					// the unit chases.
					unit.ActionFacingDX = 0
					unit.ActionFacingDY = 0
					// Hold units never move to engage. If the target walked out of
					// attack range, drop it and stay put rather than giving chase.
					if unit.Order.Type == OrderHold {
						s.clearCombatTargetLocked(unit)
						continue
					}
					unit.Status = "Moving To Attack"
					profile := resolveCombatProfile(unit)
					if !unit.Moving {
						s.refreshUnitAttackApproachLocked(unit, target, profile, blocked, true)
					} else {
						s.refreshUnitAttackApproachLocked(unit, target, profile, blocked, false)
					}
				}
			}
			continue
		}

		// Handle unit-vs-building combat
		if unit.AttackBuildingTargetID != "" {
			building := s.getBuildingByIDLocked(unit.AttackBuildingTargetID)
			if building == nil {
				unit.AttackBuildingTargetID = ""
				unit.Attacking = false
				unit.ActionFacingDX = 0
				unit.ActionFacingDY = 0
				unit.Status = "Idle"
				continue
			}
			hp, _, hpOk := getBuildingHP(building)
			if !hpOk || hp <= 0 {
				unit.AttackBuildingTargetID = ""
				unit.Attacking = false
				unit.ActionFacingDX = 0
				unit.ActionFacingDY = 0
				unit.Status = "Idle"
			} else {
				dist := s.distanceToBuilding(unit.X, unit.Y, building)

				if dist <= unit.AttackRange {
					unit.Moving = false
					unit.Path = nil
					unit.Attacking = true
					unit.Status = "Attacking"
					center := s.buildingCenterLocked(building)
					unit.ActionFacingDX = center.X - unit.X
					unit.ActionFacingDY = center.Y - unit.Y

					// Stun: cooldown still decays, but the unit must not fire.
					// AttackBuildingTargetID is left intact so combat resumes on un-stun.
					if unit.StunnedRemaining > 0 {
						if unit.AttackCooldown > 0 {
							unit.AttackCooldown = math.Max(0, unit.AttackCooldown-dt)
						}
					} else if unit.AttackCooldown <= 0 {
						damage := unit.Damage
						newHP := hp - float64(damage)
						building.Metadata["hp"] = newHP
						s.onBuildingDamagedLocked(unit, building, damage)
						s.recordDamageDealtBuildingLocked(unit, building.ID, damage)
						// Slow scales attack cadence against buildings too.
						buildingAttackSpeed := math.Max(0.1, unit.AttackSpeed*slowFactorLocked(unit))
						unit.AttackCooldown = 1.0 / buildingAttackSpeed
						if newHP <= 0 {
							building.Metadata["hp"] = 0.0
							s.payoutBuildingDamageDealtXPLocked(building.ID)
							destroyedBuildingIDs = append(destroyedBuildingIDs, building.ID)
						}
					} else {
						unit.AttackCooldown = math.Max(0, unit.AttackCooldown-dt)
					}
				} else {
					unit.Attacking = false
					unit.ActionFacingDX = 0
					unit.ActionFacingDY = 0
					// Hold units never move to engage buildings either.
					if unit.Order.Type == OrderHold {
						unit.AttackBuildingTargetID = ""
						unit.Status = "Idle"
						continue
					}
					unit.Status = "Moving To Attack"
					if !unit.Moving {
						// Re-path to the same claimed position rather than recalculating,
						// so enemies don't all converge on the same closest cell.
						s.assignUnitPath(unit, protocol.Vec2{X: unit.TargetX, Y: unit.TargetY}, blocked, nil)
					}
				}
			}
			continue
		}

		if unit.AttackCooldown > 0 {
			unit.AttackCooldown = math.Max(0, unit.AttackCooldown-dt)
		}
	}

	for _, id := range deadUnitIDs {
		s.removeUnitLocked(id)
	}
	for _, id := range destroyedBuildingIDs {
		s.destroyBuildingLocked(id)
	}
}

func (s *GameState) tickBuildingCombatLocked(dt float64) {
	var deadUnitIDs []int

	for i := range s.MapConfig.Buildings {
		building := &s.MapConfig.Buildings[i]
		if !building.Visible || building.OwnerID == nil {
			continue
		}
		if building.Metadata != nil && building.Metadata["underConstruction"] == true {
			continue
		}

		def, ok := getBuildingDef(building.BuildingType)
		if !ok || def.Damage <= 0 || def.AttackRange <= 0 || def.AttackSpeed <= 0 {
			continue
		}

		if building.Metadata == nil {
			building.Metadata = map[string]interface{}{}
		}

		cooldown, _ := getMetadataFloat(building.Metadata, "attackCooldown")
		if cooldown > 0 {
			cooldown = math.Max(0, cooldown-dt)
			building.Metadata["attackCooldown"] = cooldown
		}

		target := s.findNearestHostileUnitForBuildingLocked(building, *building.OwnerID, def.AttackRange)
		if target == nil || cooldown > 0 {
			continue
		}

		// Route through the attributed helper so shield (blood_engine) absorbs
		// first, and indirect kills (Shared Pain, pain_share) get enqueued for
		// cleanup. The manual HP<=0 block below handles the primary target kill.
		s.applyUnitDamageWithSourceLocked(target, def.Damage, DamageSource{AttackerBuildingID: building.ID, Kind: "building"})
		// Debug: bucket this damage under (building.OwnerID, building.BuildingType).
		// Defensive structures like towers accumulate here.
		s.trackBattleDamageLocked(battleSourceFromBuilding(building), target, def.Damage)
		building.Metadata["attackCooldown"] = 1.0 / def.AttackSpeed
		if target.HP <= 0 {
			target.HP = 0
			s.trackBattleKillLocked(battleSourceFromBuilding(building), target)
			deadUnitIDs = append(deadUnitIDs, target.ID)
		}
	}

	for _, id := range deadUnitIDs {
		s.removeUnitLocked(id)
	}
}

func (s *GameState) findNearestHostileUnitForBuildingLocked(building *protocol.BuildingTile, ownerID string, attackRange float64) *Unit {
	var best *Unit
	bestDistSq := attackRange * attackRange

	for _, unit := range s.Units {
		if !unit.Visible || unit.HP <= 0 || !playersAreHostile(unit.OwnerID, ownerID) {
			continue
		}

		dist := s.distanceToBuilding(unit.X, unit.Y, building)
		distSq := dist * dist
		if distSq > bestDistSq {
			continue
		}

		best = unit
		bestDistSq = distSq
	}

	return best
}

func (s *GameState) unitsAreInMutualMeleeLocked(a, b *Unit) bool {
	if a == nil || b == nil {
		return false
	}
	if !playersAreHostile(a.OwnerID, b.OwnerID) {
		return false
	}
	aProfile := resolveCombatProfile(a)
	bProfile := resolveCombatProfile(b)
	if !aProfile.Melee || !bProfile.Melee {
		return false
	}
	if a.AttackTargetID != b.ID && b.AttackTargetID != a.ID {
		return false
	}
	const meleeContactPadding = 8.0
	aRange := math.Max(a.AttackRange, unitRadius+meleeContactPadding)
	bRange := math.Max(b.AttackRange, unitRadius+meleeContactPadding)
	return distanceSquared(a.X, a.Y, b.X, b.Y) <= aRange*aRange || distanceSquared(a.X, a.Y, b.X, b.Y) <= bRange*bRange
}
