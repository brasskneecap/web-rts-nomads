package game

// countEnemiesInRangeLocked returns the number of visible, alive enemy units
// (different OwnerID) within radius of unit, up to a maximum of limit. If limit
// is <= 0 all enemies are counted. O(N) per call; early-exit once limit is hit.
//
// Used by both perkBonusArmorLocked (brace condition) and
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
	if unit == nil {
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
			// the armor bonus. Uses countEnemiesInRangeLocked, which is also
			// used by perkBonusArmorLocked, so the icon appears exactly when
			// the armor bonus is live.
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

		// rallying_banner intentionally has no buff icon on the owner — the
		// banner renders as a placed entity on the ground (sprite + radius
		// circle), making an additional icon-on-Vanguard redundant.

		// ── add cases for new visually-indicated buffs below this line ──────
		}
	}

	// guardian_aura recipient buff: show the aura icon on allies that are
	// currently under a guardian_aura. This is separate from the owner's buff
	// icon above because recipients don't own the perk themselves.
	if aura := s.guardianAuraCache[unit.ID]; aura.FlatArmor > 0 || aura.PercentArmor > 0 {
		active = append(active, "guardian_aura")
	}

	// rallying_banner recipient buff: show the icon on allied units inside any
	// active friendly banner radius. Mirrors the guardian_aura recipient pattern.
	// The banner owner will also get this icon when standing in their own banner
	// radius, which is correct — they ARE benefiting from it.
	for _, b := range s.Banners {
		if b.OwnerPlayerID != unit.OwnerID {
			continue
		}
		if b.RemainingSeconds <= 0 {
			continue
		}
		dx := unit.X - b.X
		dy := unit.Y - b.Y
		if dx*dx+dy*dy <= b.Radius*b.Radius {
			active = append(active, "rallying_banner")
			break
		}
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
