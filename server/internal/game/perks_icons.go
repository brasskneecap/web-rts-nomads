package game

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
			// the damage reduction. Evaluated fresh each call.
			enemyThreshold := int(def.Config["enemyThreshold"])
			radius := def.Config["radius"]
			radiusSq := radius * radius
			enemyCount := 0
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
					enemyCount++
					if enemyCount >= enemyThreshold {
						break
					}
				}
			}
			if enemyCount >= enemyThreshold {
				active = append(active, perkID)
			}

		case "bulwark":
			// Show while the unit has been stationary long enough for the
			// shield regen to be active.
			if unit.PerkState.StationarySeconds >= def.Config["stationaryThresholdSeconds"] {
				active = append(active, perkID)
			}

		// ── add cases for new visually-indicated buffs below this line ──────
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
	return active
}
