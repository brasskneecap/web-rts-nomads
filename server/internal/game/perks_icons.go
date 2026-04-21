package game

import "webrts/server/pkg/protocol"

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

// activeBuffIconsLocked returns the entries whose timed or conditional buff
// is currently active on the unit, in a stable order. The client uses this
// list to render floating indicator icons near the sprite (see CanvasRenderer
// drawUnitActiveBuffs). Returns nil when nothing is active so the slice is
// omitted from the JSON snapshot. Every entry has Stacks=1 currently; future
// stacking buffs populate Stacks > 1 to trigger the count badge client-side.
//
// Kept as a single switch so adding a new active-buff perk only requires one
// case here plus the matching runtime hook case elsewhere in this file.
//
// ADD NEW VISUALLY-INDICATED BUFFS HERE.
func (s *GameState) activeBuffIconsLocked(unit *Unit) []protocol.ActiveEffectIcon {
	if unit == nil {
		return nil
	}
	var active []protocol.ActiveEffectIcon
	// addIcon dedupes by ID — if the same icon id is added more than once
	// (e.g. guardian_aura from owning the perk AND from being inside
	// another ally's aura, or rallying_banner from two overlapping
	// friendly banners) the stacks field accumulates rather than producing
	// side-by-side duplicate icons. The client renders a count badge
	// whenever stacks >= 2.
	addIcon := func(id string, stacks int) {
		for i := range active {
			if active[i].ID == id {
				active[i].Stacks += stacks
				return
			}
		}
		active = append(active, protocol.ActiveEffectIcon{ID: id, Stacks: stacks})
	}
	for _, perkID := range unit.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		switch perkID {

		case "bloodlust":
			if unit.PerkState.BloodlustBonus > 0 {
				addIcon(perkID, 1)
			}
		case "relentless":
			if unit.PerkState.RelentlessRemaining > 0 {
				addIcon(perkID, 1)
			}
		case "momentum":
			if unit.PerkState.MomentumRemaining > 0 {
				addIcon(perkID, 1)
			}
		case "whirlwind_core":
			if unit.PerkState.WhirlwindActiveRemaining > 0 {
				addIcon(perkID, 1)
			}
		case "berserk_state":
			// Conditional passive: show while below HP threshold so the
			// player can see the buff kick in and fall off as HP changes.
			if unit.MaxHP > 0 {
				hpFraction := float64(unit.HP) / float64(unit.MaxHP)
				if hpFraction <= def.Config["hpThresholdPercent"] {
					addIcon(perkID, 1)
				}
			}

		case "last_stand":
			// Show while the unit is below the HP threshold — indicates both
			// the damage reduction and that the one-shot taunt is (or was) live.
			if unit.MaxHP > 0 {
				hpFraction := float64(unit.HP) / float64(unit.MaxHP)
				if hpFraction <= def.Config["hpThresholdPercent"] {
					addIcon(perkID, 1)
				}
			}

		case "brace":
			// Show while the unit is surrounded by enough enemies to trigger
			// the armor bonus. Uses countEnemiesInRangeLocked, which is also
			// used by perkBonusArmorLocked, so the icon appears exactly when
			// the armor bonus is live.
			enemyThreshold := int(def.Config["enemyThreshold"])
			if s.countEnemiesInRangeLocked(unit, def.Config["radius"], enemyThreshold) >= enemyThreshold {
				addIcon(perkID, 1)
			}

		case "bulwark":
			// Show while the unit has been stationary long enough for the
			// shield regen to be active.
			if unit.PerkState.StationarySeconds >= def.Config["stationaryThresholdSeconds"] {
				addIcon(perkID, 1)
			}

		case "interlock":
			// Show the buff icon while any ally is in range. Re-uses the same
			// helper as perkBonusArmorLocked so the icon appears exactly when
			// the armor bonus is live.
			if def != nil && s.hasAllyInRangeLocked(unit, def.Config["radius"]) {
				addIcon(perkID, 1)
			}

		case "guardian_aura":
			// Always emit for the owner — the aura is passive and ever-present
			// as long as the unit is alive and has the perk.
			addIcon(perkID, 1)

		case "pain_share":
			// Always emit for the owner — passive ever-present redirect.
			addIcon(perkID, 1)

		// rallying_banner intentionally has no buff icon on the owner — the
		// banner renders as a placed entity on the ground (sprite + radius
		// circle), making an additional icon-on-Vanguard redundant.

		// ── add cases for new visually-indicated buffs below this line ──────
		}
	}

	// guardian_aura recipient buff: show the aura icon on allies currently
	// under any guardian_aura. Stack count == number of distinct emitters
	// covering this unit (from the aura cache's Sources counter). addIcon
	// dedupes with the owner-case above, so a Vanguard who owns the perk
	// AND stands in a teammate's aura sees 1 icon with "2" stacks, not two
	// side-by-side icons.
	if aura := s.guardianAuraCache[unit.ID]; aura.Sources > 0 {
		addIcon("guardian_aura", aura.Sources)
	}

	// rallying_banner recipient buff: show the icon on allied units inside any
	// active friendly banner radius. Mirrors the guardian_aura recipient pattern.
	// The banner owner will also get this icon when standing in their own banner
	// radius, which is correct — they ARE benefiting from it. Each overlapping
	// banner contributes a stack (addIcon dedupes IDs and sums stacks), so two
	// overlapping banners render as one icon with a "2" count badge.
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
			addIcon("rallying_banner", 1)
		}
	}

	return active
}

// activeDebuffIconsLocked returns the debuff entries currently on the unit,
// in a stable order. The client renders these as a separate row from
// ActiveBuffs so buffs and debuffs stay visually distinct. Each entry's
// Stacks field reflects how many simultaneous sources are contributing the
// debuff (currently only mark and burn stack per source — other debuffs
// always report 1). The client shows a small count badge when Stacks >= 2.
//
// Unlike buffs, debuff ids are raw icon ids (not perk ids) because debuffs
// can land on units that don't own the causing perk — Taunted and Marked are
// applied to arbitrary targets by another unit's perk.
//
// ADD NEW DEBUFF ICONS HERE as new debuff mechanics are added. Keep the
// order stable so icon positions don't flicker on the client.
func (s *GameState) activeDebuffIconsLocked(unit *Unit) []protocol.ActiveEffectIcon {
	if unit == nil {
		return nil
	}
	var active []protocol.ActiveEffectIcon
	// Same dedupe-and-sum semantics as activeBuffIconsLocked so any future
	// debuff that can be added from multiple entry points merges into a
	// single icon with a stack count rather than producing visual dupes.
	addIcon := func(id string, stacks int) {
		for i := range active {
			if active[i].ID == id {
				active[i].Stacks += stacks
				return
			}
		}
		active = append(active, protocol.ActiveEffectIcon{ID: id, Stacks: stacks})
	}
	if unit.TauntedByUnitID != 0 && unit.TauntRemaining > 0 {
		addIcon("debuff-taunted", 1)
	}
	if unit.PerkState.WeakenedRemaining > 0 {
		addIcon("debuff-weakened", 1)
	}
	if unit.PerkState.anyMarkActive() {
		addIcon("debuff-marked", len(unit.PerkState.MarkStacks))
	}
	if unit.StunnedRemaining > 0 {
		addIcon("debuff-stunned", 1)
	}
	if unit.SlowedRemaining > 0 {
		addIcon("debuff-slowed", 1)
	}
	if len(unit.PerkState.BurnStacks) > 0 {
		addIcon("debuff-burning", len(unit.PerkState.BurnStacks))
	}
	return active
}

// perkCooldownsLocked returns one entry per owned perk whose activation is
// currently gated by a ticking cooldown timer. Ready perks (Remaining == 0)
// are omitted so the client only draws the clock-wipe overlay while it is
// meaningful. Total is the rank/modifier-adjusted cooldown length that would
// be written back to the state field on the next reset — used by the client
// to compute the wipe fraction (remaining / total).
//
// ADD NEW COOLDOWN-INDICATED PERKS HERE.
//
// Must be called under s.mu (read or write) lock.
func (s *GameState) perkCooldownsLocked(unit *Unit) []protocol.PerkCooldownSnapshot {
	if unit == nil {
		return nil
	}
	var cds []protocol.PerkCooldownSnapshot
	add := func(id string, remaining, total float64) {
		if remaining <= 0 || total <= 0 {
			return
		}
		cds = append(cds, protocol.PerkCooldownSnapshot{
			PerkID:    id,
			Remaining: remaining,
			Total:     total,
		})
	}
	for _, perkID := range unit.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		cfg := def.ConfigForRank(unit.Rank)
		switch perkID {
		case "whirlwind_core":
			// Suppress the overlay while the whirlwind is actively spinning —
			// the unit is mid-ability, not waiting. The cooldown-phase timer
			// is what should be surfaced to the HUD.
			if unit.PerkState.WhirlwindActiveRemaining > 0 {
				continue
			}
			add(perkID, unit.PerkState.WhirlwindCooldownRemaining, cfg["cooldownSeconds"])
		case "rallying_banner":
			add(perkID, unit.PerkState.BannerCooldownRemaining, cfg["cooldownSeconds"])
		case "caltrops", "fire_pit", "explosive_trap", "marker_trap":
			// Trap perks share a single TrapPlaceCooldownRemaining field on
			// the unit because an archer only owns one bronze trap perk at a
			// time. Total factors in rapid_deployment's CooldownMultiplier so
			// the wipe matches the actual wait.
			mods := s.trapModifiersForUnitLocked(unit)
			add(perkID, unit.PerkState.TrapPlaceCooldownRemaining, cfg["placeIntervalSeconds"]*mods.CooldownMultiplier)
		}
	}
	return cds
}
