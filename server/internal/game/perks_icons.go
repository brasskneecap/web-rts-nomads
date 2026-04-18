package game

import "math"

// isAdvancingTowardEnemyLocked returns true when unit is Moving, has at least
// one visible enemy, and its velocity vector (toward the next waypoint in
// Path) aligns with the vector to the nearest visible enemy within a 60° cone
// (dot product / (|a||b|) ≥ alignmentCosMin from steady_advance config).
//
// Returns false if Path is empty (unit is not actually advancing) or if no
// visible enemy exists.
//
// Must be called under s.mu (read or write) lock.
func (s *GameState) isAdvancingTowardEnemyLocked(unit *Unit) bool {
	if unit == nil || !unit.Moving || len(unit.Path) == 0 {
		return false
	}

	def := perkDefByID("steady_advance")
	if def == nil {
		return false
	}
	alignmentCosMin := def.Config["alignmentCosMin"]

	// Velocity vector: from current position toward next waypoint.
	next := unit.Path[0]
	vx := next.X - unit.X
	vy := next.Y - unit.Y
	vLen := math.Sqrt(vx*vx + vy*vy)
	if vLen < 1e-9 {
		return false // unit is already at the waypoint; no meaningful direction
	}

	// Find the nearest visible enemy and compute cosine alignment.
	var nearestEnemy *Unit
	var nearestDistSq float64
	for _, candidate := range s.Units {
		if candidate == nil || candidate.ID == unit.ID {
			continue
		}
		if candidate.OwnerID == unit.OwnerID {
			continue
		}
		if candidate.HP <= 0 || !candidate.Visible {
			continue
		}
		dx := candidate.X - unit.X
		dy := candidate.Y - unit.Y
		dSq := dx*dx + dy*dy
		if nearestEnemy == nil || dSq < nearestDistSq {
			nearestEnemy = candidate
			nearestDistSq = dSq
		}
	}
	if nearestEnemy == nil {
		return false
	}

	// Cosine between velocity vector and vector to nearest enemy.
	ex := nearestEnemy.X - unit.X
	ey := nearestEnemy.Y - unit.Y
	eLen := math.Sqrt(ex*ex + ey*ey)
	if eLen < 1e-9 {
		return false
	}
	cosine := (vx*ex + vy*ey) / (vLen * eLen)
	return cosine >= alignmentCosMin
}

// countEnemiesInRangeLocked returns the number of visible, alive enemy units
// (different OwnerID) within radius of unit, up to a maximum of limit. If limit
// is <= 0 all enemies are counted. O(N) per call; early-exit once limit is hit.
//
// Used by both perkIncomingDamageMultiplierLocked (brace condition) and
// activeBuffIconsLocked (brace buff-icon) to avoid duplicating the scan loop.
//
// Must be called under s.mu (read or write) lock.
func (s *GameState) countEnemiesInRangeLocked(unit *Unit, radius float64, limit int) int {
	if unit == nil || radius <= 0 {
		return 0
	}
	radiusSq := radius * radius
	count := 0
	for _, candidate := range s.Units {
		if candidate == nil || candidate.ID == unit.ID {
			continue
		}
		if candidate.OwnerID == unit.OwnerID {
			continue
		}
		if candidate.HP <= 0 || !candidate.Visible {
			continue
		}
		dx := candidate.X - unit.X
		dy := candidate.Y - unit.Y
		if dx*dx+dy*dy <= radiusSq {
			count++
			if limit > 0 && count >= limit {
				return count
			}
		}
	}
	return count
}

// hasAllyInRangeLocked returns true when any allied (same OwnerID), visible,
// alive unit — excluding self — is within radius of unit. O(N) per call.
//
// Must be called under s.mu (read or write) lock.
func (s *GameState) hasAllyInRangeLocked(unit *Unit, radius float64) bool {
	if unit == nil || radius <= 0 {
		return false
	}
	radiusSq := radius * radius
	for _, candidate := range s.Units {
		if candidate == nil || candidate.ID == unit.ID {
			continue
		}
		if candidate.OwnerID != unit.OwnerID {
			continue
		}
		if candidate.HP <= 0 || !candidate.Visible {
			continue
		}
		dx := candidate.X - unit.X
		dy := candidate.Y - unit.Y
		if dx*dx+dy*dy <= radiusSq {
			return true
		}
	}
	return false
}

// activeBuffIconsLocked returns the perk ids whose timed or conditional buff
// is currently active on the unit, in a stable order. The client uses this
// list to render floating indicator icons near the sprite (see CanvasRenderer
// drawUnitActiveBuffs). Returns nil when nothing is active so the slice is
// omitted from the JSON snapshot.
//
// Kept as a single switch so adding a new active-buff perk only requires one
// case here plus the matching runtime hook case elsewhere in this file.
//
// ADD NEW VISUALLY-INDICATED BUFFS HERE.
func (s *GameState) activeBuffIconsLocked(unit *Unit) []string {
	if unit == nil || len(unit.PerkIDs) == 0 {
		return nil
	}
	var active []string
	for _, perkID := range unit.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		switch perkID {

		case "bloodlust":
			if unit.PerkState.BloodlustBonus > 0 {
				active = append(active, perkID)
			}
		case "relentless":
			if unit.PerkState.RelentlessRemaining > 0 {
				active = append(active, perkID)
			}
		case "momentum":
			if unit.PerkState.MomentumRemaining > 0 {
				active = append(active, perkID)
			}
		case "whirlwind_core":
			if unit.PerkState.WhirlwindActiveRemaining > 0 {
				active = append(active, perkID)
			}
		case "berserk_state":
			// Conditional passive: show while below HP threshold so the
			// player can see the buff kick in and fall off as HP changes.
			if unit.MaxHP > 0 {
				hpFraction := float64(unit.HP) / float64(unit.MaxHP)
				if hpFraction <= def.Config["hpThresholdPercent"] {
					active = append(active, perkID)
				}
			}

		case "last_stand":
			// Show while the unit is below the HP threshold — indicates both
			// the damage reduction and that the one-shot taunt is (or was) live.
			if unit.MaxHP > 0 {
				hpFraction := float64(unit.HP) / float64(unit.MaxHP)
				if hpFraction <= def.Config["hpThresholdPercent"] {
					active = append(active, perkID)
				}
			}

		case "brace":
			// Show while the unit is surrounded by enough enemies to trigger
			// the damage reduction. Uses countEnemiesInRangeLocked, which is
			// also used by perkIncomingDamageMultiplierLocked, so the icon
			// appears exactly when the damage reduction is live.
			enemyThreshold := int(def.Config["enemyThreshold"])
			if s.countEnemiesInRangeLocked(unit, def.Config["radius"], enemyThreshold) >= enemyThreshold {
				active = append(active, perkID)
			}

		case "bulwark":
			// Show while the unit has been stationary long enough for the
			// shield regen to be active.
			if unit.PerkState.StationarySeconds >= def.Config["stationaryThresholdSeconds"] {
				active = append(active, perkID)
			}

		case "steady_advance":
			// Show the buff icon while the unit is actively advancing toward
			// the nearest visible enemy within the alignment cone. Re-uses the
			// same helper as perkIncomingDamageMultiplierLocked so the icon
			// appears exactly when the damage reduction is live.
			if unit.Moving && s.isAdvancingTowardEnemyLocked(unit) {
				active = append(active, perkID)
			}

		case "interlock":
			// Show the buff icon while any ally is in range. Re-uses the same
			// helper as perkBonusArmorLocked so the icon appears exactly when
			// the armor bonus is live.
			if def != nil && s.hasAllyInRangeLocked(unit, def.Config["radius"]) {
				active = append(active, perkID)
			}

		case "guardian_aura":
			// Always emit for the owner — the aura is passive and ever-present
			// as long as the unit is alive and has the perk.
			active = append(active, perkID)

		case "pain_share":
			// Always emit for the owner — passive ever-present redirect.
			active = append(active, perkID)

		case "rallying_banner":
			// Show while at least one banner planted by this unit is still active.
			for _, b := range s.Banners {
				if b.OwnerUnitID == unit.ID && b.RemainingSeconds > 0 {
					active = append(active, perkID)
					break
				}
			}

		// ── add cases for new visually-indicated buffs below this line ──────
		}
	}

	// guardian_aura recipient buff: show the aura icon on allies that are
	// currently under a guardian_aura. This is separate from the owner's buff
	// icon above because recipients don't own the perk themselves.
	if aura, ok := s.guardianAuraCache[unit.ID]; ok && aura > 0 {
		active = append(active, "guardian_aura")
	}

	return active
}

// activeDebuffIconsLocked returns the icon ids for debuffs currently on the
// unit, in a stable order. The client renders these as a separate row from
// ActiveBuffs so buffs and debuffs stay visually distinct.
//
// Unlike buffs, debuff ids are raw icon ids (not perk ids). This is because
// debuffs can land on units that don't own the causing perk — Taunted and
// Marked are applied to arbitrary targets by another unit's perk.
//
// ADD NEW DEBUFF ICONS HERE as new debuff mechanics are added. Keep the
// order stable so icon positions don't flicker on the client.
func (s *GameState) activeDebuffIconsLocked(unit *Unit) []string {
	if unit == nil {
		return nil
	}
	var active []string
	if unit.TauntedByUnitID != 0 && unit.TauntRemaining > 0 {
		active = append(active, "debuff-taunted")
	}
	if unit.PerkState.WeakenedRemaining > 0 {
		active = append(active, "debuff-weakened")
	}
	if unit.PerkState.MarkedRemaining > 0 {
		active = append(active, "debuff-marked")
	}
	if unit.StunnedRemaining > 0 {
		active = append(active, "debuff-stunned")
	}
	if unit.SlowedRemaining > 0 {
		active = append(active, "debuff-slowed")
	}
	return active
}
