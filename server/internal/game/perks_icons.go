package game

import "webrts/server/pkg/protocol"

// recomputePerkPredicateCacheLocked refreshes the per-tick BraceActive /
// InterlockActive flags on every unit so the snapshot path can read O(1)
// instead of running the historical countEnemiesInRangeLocked /
// hasAllyInRangeLocked scans for each consumer. Uses the shared per-tick
// spatial index built at the top of Update — units outside the perk's
// radius are not even visited.
//
// Sets perkPredicateCacheTick = s.Tick so downstream helpers know the cache
// is fresh and can safely read from it. Helpers called outside this window
// fall back to a live scan.
//
// Caller holds s.mu.
func (s *GameState) recomputePerkPredicateCacheLocked() {
	s.perkPredicateCacheTick = s.Tick
	if len(s.Units) == 0 {
		return
	}
	braceDef := perkDefByID("brace")
	interlockDef := perkDefByID("interlock")
	for _, unit := range s.Units {
		if unit == nil {
			continue
		}
		// Reset to false first so units that lose the perk (rare; usually
		// only via death) don't keep a stale active flag.
		unit.PerkState.BraceActive = false
		unit.PerkState.InterlockActive = false
		if len(unit.PerkIDs) == 0 || unit.HP <= 0 {
			continue
		}
		for _, perkID := range unit.PerkIDs {
			switch perkID {
			case "brace":
				if braceDef == nil {
					continue
				}
				radius := braceDef.Config["radius"]
				threshold := int(braceDef.Config["enemyThreshold"])
				count := s.countEnemiesInRangeLocked(unit, radius, threshold)
				if count >= threshold {
					unit.PerkState.BraceActive = true
				}
			case "interlock":
				if interlockDef == nil {
					continue
				}
				if s.hasAllyInRangeLocked(unit, interlockDef.Config["radius"]) {
					unit.PerkState.InterlockActive = true
				}
			}
		}
	}
}

// countEnemiesInRangeLocked returns the number of visible, alive enemy units
// (different OwnerID) within radius of unit, up to a maximum of limit. If limit
// is <= 0 all enemies are counted. Uses the per-tick spatial index when
// available (the hot path; built at the top of Update). Falls back to the
// linear scan for tests / call sites that drive helpers directly without
// constructing the index first.
//
// Used by both perkBonusArmorLocked (brace condition) and
// activeBuffIconsLocked (brace buff-icon) — though both now go through the
// per-tick predicate cache (PerkState.BraceActive) on the snapshot path.
//
// Must be called under s.mu (read or write) lock.
func (s *GameState) countEnemiesInRangeLocked(unit *Unit, radius float64, limit int) int {
	if unit == nil || radius <= 0 {
		return 0
	}
	if s.unitSpatialIndex != nil {
		count := 0
		for _, candidate := range s.unitSpatialIndex.query(unit.X, unit.Y, radius) {
			if candidate == nil || candidate.ID == unit.ID {
				continue
			}
			if candidate.OwnerID == unit.OwnerID {
				continue
			}
			// index already filtered HP > 0 && Visible at build time
			count++
			if limit > 0 && count >= limit {
				return count
			}
		}
		return count
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
// alive unit — excluding self — is within radius of unit. Uses the per-tick
// spatial index when available.
//
// Must be called under s.mu (read or write) lock.
func (s *GameState) hasAllyInRangeLocked(unit *Unit, radius float64) bool {
	if unit == nil || radius <= 0 {
		return false
	}
	if s.unitSpatialIndex != nil {
		for _, candidate := range s.unitSpatialIndex.query(unit.X, unit.Y, radius) {
			if candidate == nil || candidate.ID == unit.ID {
				continue
			}
			if candidate.OwnerID != unit.OwnerID {
				continue
			}
			return true
		}
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
			// The spin animation is now driven by EffectSnapshot ("whirlwind"
			// name) rather than a WhirlwindAnimRemaining timer on PerkState.
			// No buff icon is surfaced here — the client reads the EffectSnapshot
			// directly to drive the overlay animation.
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
			// Show for the duration of the armor-bonus + taunt window so the
			// icon tracks the live bonus rather than current HP (which can rise
			// back above the threshold mid-window without ending the buff).
			if unit.PerkState.LastStandRemaining > 0 {
				addIcon(perkID, 1)
			}

		case "brace":
			// Prefer the per-tick predicate cache so both the armor bonus
			// (perkBonusArmorLocked) and this icon agree without paying the
			// O(N) scan twice per snapshot. Fall back to live scan when
			// the cache is stale (direct-call tests outside Update).
			active := unit.PerkState.BraceActive
			if s.perkPredicateCacheTick != s.Tick {
				threshold := int(def.Config["enemyThreshold"])
				active = s.countEnemiesInRangeLocked(unit, def.Config["radius"], threshold) >= threshold
			}
			if active {
				addIcon(perkID, 1)
			}

		case "interlock":
			// Prefer the per-tick predicate cache — see brace above.
			active := unit.PerkState.InterlockActive
			if def != nil && s.perkPredicateCacheTick != s.Tick {
				active = s.hasAllyInRangeLocked(unit, def.Config["radius"])
			}
			if active {
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

		case "sanctuary":
			// Cleric bronze passive: always emit for the owner so the player
			// sees the aura is live whenever the Cleric is alive. Allies
			// inside the aura get a separate recipient-style icon below.
			addIcon(perkID, 1)

		case "mana_conduit":
			// Cleric bronze passive: always emit for the owner. The mana-regen
			// effect is keyed on nearby injured allies, but the perk itself is
			// the durable thing the player wants to see represented.
			addIcon(perkID, 1)

		case "divine_aegis":
			// Cleric silver passive: always emit on the owner so the player
			// can see the pulsing aura is live. The single-charge protection
			// itself surfaces on recipients via the cross-unit branch below.
			addIcon(perkID, 1)

		case "restoration_aura":
			// Cleric silver passive: always emit on the owner — pulses are
			// time-based, so the HUD signals "this aura is on" rather than
			// trying to flash at each pulse moment (which would be
			// distracting and inconsistent across multiple Clerics).
			addIcon(perkID, 1)

		case "zealous_march":
			// Cleric silver passive: always emit on the owner. Recipients
			// (allies inside the aura) get their own icon via the cross-unit
			// branch below so allies without the perk still display the
			// movement-speed buff while it is live.
			addIcon(perkID, 1)

		case "divine_healer":
			// Cleric silver passive: always emit on the owner — the multiplier
			// is always-on, the HUD just signals "this Cleric heals stronger".
			addIcon(perkID, 1)

		case "divine_intervention", "beacon_of_life", "divine_judgement":
			// Cleric gold passives: always emit for the owner so the player
			// can see the perk is owned. Intervention's cooldown wipes via
			// perkCooldownsLocked; beacon and judgement are reactive (fire on
			// heal events) so there's no per-unit "active" state to gate on.
			addIcon(perkID, 1)

		// Siphoner bronze perks are intentionally NOT emitted on the owner:
		//   - soul_leech is a conditional damage/heal multiplier on Siphon
		//     Life — it doesn't stat-buff the Siphoner itself.
		//   - withering_beam, lingering_hex, mark_of_weakness all act on
		//     enemies; their visual presence is the corresponding debuff
		//     icon on the affected unit (debuff-withering-beam, etc.).
		// Convention: a floating buff badge on a unit should signal an
		// active stat benefit on THAT unit. Perk-owned indicators that
		// only buff allies / debuff enemies live on the affected target.
		// Cooldown wipes for the two autonomous AoEs surface via
		// perkCooldownsLocked → SelectionHud's perk slot.

		// battle_prayer's icon lives on the BUFFED TARGET (cross-unit buff),
		// not on the perk owner. Surfaced below this loop so allies who don't
		// own the perk still display the buff icon while it is active.

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

	// battle_prayer recipient buff: surfaced on any unit currently carrying a
	// non-zero BattlePrayerRemaining, regardless of whether they own the perk
	// (the buff lives on the healed target, not the Cleric). Mirrors the
	// cross-unit-debuff pattern used for WeakenedRemaining in activeDebuffIconsLocked.
	if unit.PerkState.BattlePrayerRemaining > 0 {
		addIcon("battle_prayer", 1)
	}

	// bolstering_prayer recipient buff: surfaced under the same cross-unit
	// rule as battle_prayer above — the buff lives on the healed target, so
	// any unit with BolsteringPrayerRemaining > 0 shows the icon, perk-owner
	// or not. Both icons co-exist when both buffs are simultaneously active.
	if unit.PerkState.BolsteringPrayerRemaining > 0 {
		addIcon("bolstering_prayer", 1)
	}

	// divine_aegis recipient buff: surfaced on any unit carrying a non-zero
	// DivineAegisRemaining charge. The recipient need not own the perk —
	// same cross-unit pattern as BattlePrayer / BolsteringPrayer. addIcon
	// dedupes with the owner-case above so a Cleric standing in their own
	// aura sees 1 icon (single charge — overlapping clerics refresh the
	// same charge, they do not stack).
	if unit.PerkState.DivineAegisRemaining > 0 {
		addIcon("divine_aegis", 1)
	}

	// divine_intervention recipient buff: surfaced on any unit currently
	// inside the brief invulnerability window stamped by a save. The icon
	// reads as "this unit cannot be killed right now" — a clear visual tell
	// to the player that follow-up burst will whiff. The owner-side icon
	// (above) shows perk ownership; this one shows active protection.
	if unit.PerkState.InvulnerabilityRemaining > 0 {
		addIcon("divine_intervention", 1)
	}

	// zealous_march recipient buff: emit the icon while ANY allied Cleric
	// with zealous_march has this unit inside their aura. Reuses the same
	// scan as perkMoveSpeedBonusFromClericAurasLocked so the icon appears
	// exactly when the move-speed bonus is live. addIcon dedupes with the
	// owner-case above so a Cleric standing in another Cleric's aura
	// (or their own friendly area) sees a single icon, not two.
	if s.hasZealousMarchAuraLocked(unit) {
		addIcon("zealous_march", 1)
	}

	// divine_intervention recipient buff: a unit currently inside a brief
	// invulnerability window (typically just saved from death by a nearby
	// Cleric) shows the intervention icon so the player can see they are
	// untouchable for the moment. The buff lives on the recipient — not
	// the saving cleric — same cross-unit pattern as the prayer / aegis
	// buffs above. Decays in state.go alongside the other cross-unit timers.
	if unit.PerkState.InvulnerabilityRemaining > 0 {
		addIcon("divine_intervention", 1)
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
	// Hunter's Mark (Marksman) — distinct icon from challengers_mark/marker_trap
	// because its mechanic is different (crit-chance bonus, not damage amp).
	// Stack count reflects number of distinct sources currently marking.
	if unit.PerkState.huntersMarkCount() > 0 {
		addIcon("debuff-hunters-mark", unit.PerkState.huntersMarkCount())
	}
	// Siphoner bronze afflictions — cross-unit debuffs that live on the
	// affected enemy regardless of whether the unit owns the source perk.
	// Withering Beam shows stack count so the player can see how close it
	// is to maxStacks; Lingering Hex and Mark of Weakness are non-stacking
	// and always report a single icon.
	if unit.PerkState.WitheringBeamRemaining > 0 && unit.PerkState.WitheringBeamStacks > 0 {
		addIcon("debuff-withering-beam", unit.PerkState.WitheringBeamStacks)
	}
	if unit.PerkState.LingeringHexRemaining > 0 {
		addIcon("debuff-lingering-hex", 1)
	}
	if unit.PerkState.MarkOfWeaknessRemaining > 0 {
		addIcon("debuff-mark-of-weakness", 1)
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
		// whirlwind_core no longer has a cooldown — it's an RNG proc on every
		// attack, so there's nothing to countdown-overlay on the perk icon.
		case "rallying_banner":
			add(perkID, unit.PerkState.BannerCooldownRemaining, cfg["cooldownSeconds"])
		case "caltrops", "fire_pit", "explosive_trap", "marker_trap":
			// Trap perks share a single TrapPlaceCooldownRemaining field on
			// the unit because an archer only owns one bronze trap perk at a
			// time. Total factors in rapid_deployment's CooldownMultiplier so
			// the wipe matches the actual wait.
			mods := s.trapModifiersForUnitLocked(unit)
			add(perkID, unit.PerkState.TrapPlaceCooldownRemaining, cfg["placeIntervalSeconds"]*mods.CooldownMultiplier)
		case "divine_aegis":
			// Silver cleric pulse — surfaces the wipe so the player can see
			// when the next protection wave will land on their nearby allies.
			add(perkID, unit.PerkState.DivineAegisPulseRemaining, cfg["intervalSeconds"])
		case "restoration_aura":
			// Silver cleric pulse — same wipe convention as divine_aegis so
			// the player can read the next heal's cadence at a glance.
			add(perkID, unit.PerkState.RestorationPulseRemaining, cfg["intervalSeconds"])
		case "divine_intervention":
			// Gold cleric save — surfaces the cooldown wipe so the player
			// knows when their next clutch save is available.
			add(perkID, unit.PerkState.DivineInterventionCooldownRemaining, cfg["cooldownSeconds"])
		case "lingering_hex":
			// Siphoner bronze autonomous AoE — wipe shows when the next
			// hex pulse will fire. Total uses cooldownSeconds from the
			// perk config (set on every successful pulse).
			add(perkID, unit.PerkState.LingeringHexCooldownRemaining, cfg["cooldownSeconds"])
		case "mark_of_weakness":
			// Siphoner bronze autonomous AoE — same cooldown wipe convention
			// as lingering_hex.
			add(perkID, unit.PerkState.MarkOfWeaknessCooldownRemaining, cfg["cooldownSeconds"])
		}
	}
	return cds
}
