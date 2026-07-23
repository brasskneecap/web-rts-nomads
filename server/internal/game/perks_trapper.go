package game

import "webrts/server/pkg/protocol"

// ═════════════════════════════════════════════════════════════════════════════
// TRAPPER PERKS
//
// This file owns the perk-modifier pipeline that aggregates every Silver /
// Gold Trapper perk into the effective trap stats applied at plant time.
// The Bronze Trapper perks (caltrops, fire_pit, explosive_trap, marker_trap)
// are case arms in tickUnitPerkStateLocked + light state on UnitPerkState;
// the broader trap data model + lifecycle (plant / decay / on-stay / detonate
// / cleanup) lives in trap.go because it is core simulation, not perk code.
//
// This split mirrors the per-path file convention documented in perks.go:
// the perk-specific helpers (modifiers, gating) belong in perks_trapper.go;
// the shared simulation backbone they call into (trap entities, zone
// effects, placement timer) belongs in trap.go.
//
// MODIFIER PIPELINE
//
// Resolves the effective trap stats for a Trapper unit by aggregating all
// perk-driven modifiers the unit currently owns. This pipeline is the single
// place where perk modifiers are combined before they are applied to a
// newly-planted trap (plantTrapLocked) or to the placement cooldown
// (tickTrapPlacementLocked).
//
// DESIGN PRINCIPLES
//   - Base trap definitions in catalog/perks JSON are IMMUTABLE.
//   - Modifiers are resolved once per plant / per cooldown reset.
//   - Trap struct fields continue to be an immutable snapshot — the modifier
//     pipeline bakes values in at plant time; ticks don't re-evaluate perks.
//   - Slow amplification uses slow-amount math (1 - mult) so it composes with
//     reductive multipliers the way a player expects.
//
// EXTENSION POINTS (plug future perks in here):
//
//   1. Gold-tier GLOBAL perks that scale all traps:
//        Add a `case "<perk_id>":` branch inside trapModifiersForUnitLocked.
//
//   2. Gold-tier / Silver-tier TRAP-SPECIFIC perks that only apply to one
//      Bronze trap type (e.g. "stronger caltrops slow"):
//        - Set requiresPerk in the JSON to the Bronze trap ID
//          (caltrops/fire_pit/explosive_trap/marker_trap). The existing
//          eligibility filter in perks.go handles gating automatically.
//        - Resolve them via trapSpecificModifiersForUnitLocked (below) which
//          returns modifiers that ONLY apply if the trap being planted
//          matches the gated trap type. Caller (plantTrapLocked) passes the
//          trap type so the resolver can selectively apply them.
// ═════════════════════════════════════════════════════════════════════════════

// TrapModifiers is the aggregated set of multiplicative modifiers that apply
// to every trap planted by a unit. All fields default to 1.0 (no change).
// Multipliers compose multiplicatively so stacking perks behave predictably.
type TrapModifiers struct {
	// DurationMultiplier scales Trap.RemainingSeconds (trap entity lifetime).
	// Does NOT scale marker_trap.markDuration (that is a debuff lifetime, not
	// a trap-entity lifetime — handled separately if ever needed).
	DurationMultiplier float64

	// RadiusMultiplier scales Trap.Radius and (for explosive_trap)
	// Trap.TriggerRadius.
	RadiusMultiplier float64

	// EffectMultiplier scales per-trap effect magnitudes:
	//   - DamagePerSecond (caltrops, fire_pit)
	//   - BurstDamage (explosive_trap)
	//   - MarkMultiplier (marker_trap)
	//   - MarkDuration (marker_trap): scaled — longer mark window when amplified.
	//   - SlowMultiplier is applied via amplifySlow (slow-amount math, not
	//     direct multiplication) so a stronger slow multiplies the *slow*,
	//     not the base speed.
	EffectMultiplier float64
}

func newTrapModifiers() TrapModifiers {
	return TrapModifiers{
		DurationMultiplier: 1.0,
		RadiusMultiplier:   1.0,
		EffectMultiplier:   1.0,
	}
}

// trapModifiersForUnitLocked aggregates all GLOBAL trap-affecting perks owned
// by the unit. Returns the identity (all 1.0) if the unit owns none.
//
// Must be called under s.mu write lock.
func (s *GameState) trapModifiersForUnitLocked(unit *Unit) TrapModifiers {
	m := newTrapModifiers()
	if unit == nil {
		return m
	}
	// Archer "Master Huntsman" advancement (unitTrapEffectMul / unitTrapRadiusMul):
	// additive bonus fractions seeded onto the unit from its effective def, folded
	// in as (1 + bonus) multipliers. Compose multiplicatively with the perk-driven
	// modifiers below (wider_nets, amplified_effects), so a ×2 advancement and a
	// perk multiplier stack as the player expects.
	if unit.TrapEffectBonus > 0 {
		m.EffectMultiplier *= 1 + unit.TrapEffectBonus
	}
	if unit.TrapRadiusBonus > 0 {
		m.RadiusMultiplier *= 1 + unit.TrapRadiusBonus
	}
	for _, perkID := range unit.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		switch perkID {
		// ── Silver: global trap modifiers ─────────────────────────────────
		// extended_setup and wider_nets have NO case here, for the same reason
		// amplified_effects below has none: they are pure data now (ability-stat
		// rows) and carry no config for this aggregator to read. A case here
		// would multiply by a missing key's zero and wipe every trap's numbers.
		// rapid_deployment is no longer a global trap modifier: placement
		// cadence is the trap ability's cooldown, and rapid_deployment now
		// shortens it as a data perk via AbilityModifier.CooldownMult (see its
		// catalog JSON). It intentionally has no case here.
		// amplified_effects intentionally has NO case here. Its contributions
		// are all data now — abilityDamage for damage, inflicted-stat rows for
		// the slow and the mark — so it carries no config for this aggregator
		// to read. A case here would multiply EffectMultiplier by a missing
		// key's zero and wipe every trap's numbers.

		// ── EXTENSION POINT: Gold-tier GLOBAL modifiers plug in here.
		//    Follow the same pattern: multiplicative composition into the
		//    appropriate field. Keep perk IDs stable across JSON + code.
		}
	}
	return m
}

// TrapSpecificModifiers holds modifiers that only apply to a specific trap
// type, determined at plant time. Unlike TrapModifiers (which always apply),
// these are gated on the trap type matching the perk's intent.
//
// Resolved alongside TrapModifiers in plantTrapLocked, but only after the
// trap type is known. New trap-specific perks (future Silver/Gold) plug in
// by adding a case to trapSpecificModifiersForUnitLocked below.
type TrapSpecificModifiers struct {
	// explosive_chain migrated to a pure data perk: an abilityFields row adds to
	// explosive_trap's Detonation zone maxTicks (one extra blast per +1), so the
	// legacy trap runtime no longer carries an aftershock delay from the perk.

	// barbed_field migrated to a pure data perk: a has_perk gate in caltrops'
	// program applies a "Barbed" stacking status whose per-stack tick is the
	// ramp. No trap-modifier field is needed — see the perk's json.

	// exposed_weakness migrated to a pure data perk: it is now a has_perk gate
	// inside marker_trap's program that adds a `damageDealt` (Weaken) status to
	// the mark. No trap-modifier field is needed — see the perk's json and
	// stat_modifiers.go statDamageDealt.

	// lasting_flames (silver): fire_pit-only. Switches the fire pit into
	// "damage-as-debuff" mode by snapshotting the burn-debuff duration onto
	// the trap. The burn's DPS is derived from the fire pit's own
	// DamagePerSecond at tick time, so this resolver doesn't carry a DPS.
	LastingFlamesBurnDuration float64

	// ── Gold: ascendant_infusion (adaptive per Bronze trap) ──────────────────
	// Electrified Caltrops (caltrops)
	InfusionElectrifiedBonusDamage     int
	InfusionElectrifiedStunChance      float64
	InfusionElectrifiedStunDuration    float64
	InfusionElectrifiedStunCooldownSec float64
	InfusionElectrifiedStunDamage      int
	// Reactive Flames (fire_pit)
	InfusionReactiveFlamesRadius float64
	InfusionReactiveFlamesDamage int
	// Scatter Bomb (explosive_trap)
	InfusionScatterBombCount        int
	InfusionScatterBombSpawnRadius  float64
	InfusionScatterBombChildSeconds float64
	// Shared Pain (marker_trap)
	InfusionSharedPainFraction float64

	// ── Gold: overload_protocol (adaptive per Bronze trap) ───────────────────
	// Spike Surge (caltrops)
	OverloadSpikeSurgeBurstDamage  int
	OverloadSpikeSurgeSlowMult     float64
	OverloadSpikeSurgeSlowDuration float64
	// Flame Collapse (fire_pit). RadiusMult is applied to the trap's already-
	// multiplied zone radius at plant time to derive the absolute explosion
	// radius snapshotted onto the Trap struct.
	OverloadFlameCollapseRadiusMult  float64
	OverloadFlameCollapseDamage      int
	OverloadFlameCollapseBurnDPS     float64
	OverloadFlameCollapseBurnSeconds float64
	// Cataclysm Blast (explosive_trap)
	OverloadCataclysmRadiusMult   float64
	OverloadCataclysmDelaySeconds float64
	// Client-side visual inflate applied to the trap's sprite while this
	// perk is active. 0 = no change (client treats as 1×). Purely cosmetic.
	OverloadCataclysmSpriteScale float64
	// Sprite scale applied to the "explosion" EffectSnapshot fired by each
	// Cataclysm secondary blast — tunable separately from the trap's barrel
	// sprite size so the explosion VFX can be sized for the spectacle without
	// inflating the trap itself.
	OverloadCataclysmExplosionSpriteScale float64
	// Final Exposure (marker_trap)
	OverloadFinalExposureDamage int
}

// trapSpecificModifiersForUnitLocked resolves trap-type-specific modifiers.
// trapType gates which perks apply — a perk that only affects caltrops is
// silent when an explosive_trap is being planted by the same unit.
//
// EXTENSION POINT: add new cases here for perks that upgrade one specific
// Bronze trap type. Keep the trapType gate explicit so a mis-owned perk
// cannot accidentally leak into the wrong trap.
//
// Must be called under s.mu write lock.
func (s *GameState) trapSpecificModifiersForUnitLocked(unit *Unit, trapType string) TrapSpecificModifiers {
	var m TrapSpecificModifiers
	if unit == nil {
		return m
	}
	for _, perkID := range unit.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		switch perkID {
		case "lasting_flames":
			if trapType == "fire_pit" {
				// "Damage-as-debuff" mode: duration is the only tunable. The
				// burn DPS is the fire pit's own EffectMultiplier-scaled
				// DamagePerSecond at tick time (see trap.go fire_pit branch).
				m.LastingFlamesBurnDuration = def.Config["burnDurationSeconds"]
			}

		// ── Gold: ascendant_infusion — adaptive, one payload per trap type ───
		case "ascendant_infusion":
			switch trapType {
			case "caltrops":
				m.InfusionElectrifiedBonusDamage = int(def.Config["electrifiedBonusDamagePerTick"])
				m.InfusionElectrifiedStunChance = def.Config["electrifiedStunChance"]
				m.InfusionElectrifiedStunDuration = def.Config["electrifiedStunDuration"]
				m.InfusionElectrifiedStunCooldownSec = def.Config["electrifiedStunCooldownSeconds"]
				m.InfusionElectrifiedStunDamage = int(def.Config["electrifiedStunDamage"])
			case "fire_pit":
				m.InfusionReactiveFlamesRadius = def.Config["reactiveFlamesRadius"]
				m.InfusionReactiveFlamesDamage = int(def.Config["reactiveFlamesDamage"])
			case "explosive_trap":
				m.InfusionScatterBombCount = int(def.Config["scatterBombCount"])
				m.InfusionScatterBombSpawnRadius = def.Config["scatterBombSpawnRadius"]
				m.InfusionScatterBombChildSeconds = def.Config["scatterBombChildDurationSeconds"]
			case "marker_trap":
				m.InfusionSharedPainFraction = def.Config["sharedPainFraction"]
			}

		// ── Gold: overload_protocol — adaptive, one payload per trap type ────
		case "overload_protocol":
			switch trapType {
			case "caltrops":
				m.OverloadSpikeSurgeBurstDamage = int(def.Config["spikeSurgeBurstDamage"])
				m.OverloadSpikeSurgeSlowMult = def.Config["spikeSurgeSlowMultiplier"]
				m.OverloadSpikeSurgeSlowDuration = def.Config["spikeSurgeSlowDurationSeconds"]
			case "fire_pit":
				m.OverloadFlameCollapseRadiusMult = def.Config["flameCollapseRadiusMultiplier"]
				m.OverloadFlameCollapseDamage = int(def.Config["flameCollapseDamage"])
				m.OverloadFlameCollapseBurnDPS = def.Config["flameCollapseBurnDamagePerSecond"]
				m.OverloadFlameCollapseBurnSeconds = def.Config["flameCollapseBurnDurationSeconds"]
			case "explosive_trap":
				m.OverloadCataclysmRadiusMult = def.Config["cataclysmRadiusMultiplier"]
				m.OverloadCataclysmDelaySeconds = def.Config["cataclysmSecondaryDelaySeconds"]
				m.OverloadCataclysmSpriteScale = def.Config["cataclysmSpriteScale"]
				m.OverloadCataclysmExplosionSpriteScale = def.Config["cataclysmExplosionSpriteScale"]
			case "marker_trap":
				m.OverloadFinalExposureDamage = int(def.Config["finalExposureBurstDamage"])
			}

		// ── EXTENSION POINT: trap-specific Silver/Gold upgrades plug in here.
		//    Gate each case on the matching trapType string so the perk stays
		//    silent on the wrong trap type. Config keys live in the perk's
		//    JSON entry; snapshot them onto TrapSpecificModifiers so the
		//    plant site can bake them into Trap fields.
		}
	}
	return m
}

// amplifySlow composes a slow multiplier with an effect-strength multiplier
// using slow-amount math. baseMult=0.7 (slow to 70%) with effectMult=1.35
// becomes slow-amount 0.30 → 0.405 → new mult 0.595. Clamped to [0, 1].
func amplifySlow(baseMult, effectMult float64) float64 {
	if baseMult >= 1.0 {
		return baseMult
	}
	slowAmount := 1.0 - baseMult
	amplified := slowAmount * effectMult
	if amplified < 0 {
		amplified = 0
	}
	if amplified > 1.0 {
		amplified = 1.0
	}
	return 1.0 - amplified
}

// EffectiveTrapStats is a debug/test-facing view of the effective stats a
// unit's next trap will be planted with. Used by tests and future debug
// tooling; not consumed by runtime simulation.
type EffectiveTrapStats struct {
	PerkID          string
	DurationSeconds float64
	Radius          float64
	TriggerRadius   float64 // explosive_trap only, else 0
	PlaceInterval   float64
	DamagePerSecond float64 // caltrops, fire_pit
	BurstDamage     int     // explosive_trap
	SlowMultiplier  float64 // caltrops
	MarkMultiplier  float64 // marker_trap
	MarkDuration    float64 // marker_trap

	// ── Silver trap-specific upgrade stats (zero when the gating perk is absent) ──
	LastingFlamesBurnDuration float64 // fire_pit + lasting_flames (burn DPS == fire_pit DamagePerSecond)
}

// EffectiveTrapSnapshotLocked computes the live compounded trap stats for a
// unit's bronze trap perk and returns them as a protocol.EffectiveTrapSnapshot
// ready to embed in the unit's tick snapshot. Returns nil if the unit owns no
// bronze trap perk.
//
// The computation mirrors DebugEffectiveTrapStats exactly; the only difference
// is that it returns a *protocol.EffectiveTrapSnapshot (wire type) rather than
// the internal EffectiveTrapStats (test type), and BurstDamage is rounded to int.
//
// Must be called under s.mu read or write lock.
func (s *GameState) EffectiveTrapSnapshotLocked(unit *Unit) *protocol.EffectiveTrapSnapshot {
	stats, ok := s.DebugEffectiveTrapStats(unit)
	if !ok {
		return nil
	}
	snap := &protocol.EffectiveTrapSnapshot{
		PerkID:                    stats.PerkID,
		DurationSeconds:           stats.DurationSeconds,
		Radius:                    stats.Radius,
		TriggerRadius:             stats.TriggerRadius,
		PlaceInterval:             stats.PlaceInterval,
		DamagePerSecond:           stats.DamagePerSecond,
		BurstDamage:               stats.BurstDamage,
		SlowMultiplier:            stats.SlowMultiplier,
		MarkMultiplier:            stats.MarkMultiplier,
		MarkDuration:              stats.MarkDuration,
		LastingFlamesBurnDuration: stats.LastingFlamesBurnDuration,
	}
	return snap
}

// unitTrapAbilityIDLocked returns the trap ability id the unit knows (one of
// caltrops / fire_pit / explosive_trap / marker_trap), or "" if none. A Trapper
// rolls exactly one trap from its bronze ability pool, so at most one matches.
// This replaced the old "scan PerkIDs for a bronze trap perk" resolution once
// the four traps became pool abilities. Caller holds s.mu.
func unitTrapAbilityIDLocked(unit *Unit) string {
	if unit == nil {
		return ""
	}
	for _, id := range unit.Abilities {
		switch id {
		case "caltrops", "fire_pit", "explosive_trap", "marker_trap":
			return id
		}
	}
	return ""
}

// effectiveTrapStatsFromParamsLocked builds the effective-trap view for a
// MIGRATED trap — one authored as a composable zone whose stats are ability
// parameters. Returns ok=false for a trap still on the legacy place_trap path,
// so the caller falls through to the old resolution during the migration.
//
// Parameter names are the shared trap vocabulary the migrated trap abilities
// author: dps / radius / duration / slowMultiplier / markMultiplier /
// markDuration / burstDamage / triggerRadius. A trap simply omits the ones it
// has no concept of, and the corresponding field stays zero exactly as it did
// under the legacy switch.
//
// Caller holds s.mu.
func (s *GameState) effectiveTrapStatsFromParamsLocked(unit *Unit, trapID string) (EffectiveTrapStats, bool) {
	// Where each trap authors the numbers the tooltip reports, as {action, field}
	// addresses into its program. This replaces the ability-PARAMETER block the
	// traps used to declare: the program is what actually runs, so reading it
	// directly removes the second source of truth rather than moving it.
	type site struct{ action, field string }
	sites, known := map[string]map[string]site{
		"fire_pit": {
			"radius": {"pit", "radius"}, "duration": {"pit", "duration"},
			"dps": {"direct_dmg", "amount"},
		},
		"caltrops": {
			"radius": {"field", "radius"}, "duration": {"field", "duration"},
			"dps": {"spikes", "amount"}, "slowMultiplier": {"slow_move", "value"},
		},
		"explosive_trap": {
			"radius": {"arm", "radius"}, "duration": {"arm", "duration"},
			"burstDamage": {"blast", "amount"},
		},
		"marker_trap": {
			"radius": {"zone", "radius"}, "duration": {"zone", "duration"},
			"markMultiplier": {"vulnerable", "value"}, "markDuration": {"mark", "duration"},
		},
	}[trapID]
	if !known {
		return EffectiveTrapStats{}, false
	}
	read := func(key string) float64 {
		st, ok := sites[key]
		if !ok {
			return 0
		}
		v, ok := s.EffectiveAbilityFieldLocked(unit, trapID, st.action, st.field)
		if !ok {
			return 0
		}
		return v
	}
	// Placement cadence is the ability's cooldown, folded with the same perk
	// cooldown modifier the cast path applies (rapid_deployment).
	interval := def_EffectiveCooldown(trapID)
	if mods := s.abilityScalarModifiersForCasterLocked(unit, trapID); mods.CooldownMult > 0 {
		interval *= mods.CooldownMult
	}
	return EffectiveTrapStats{
		PerkID:          trapID,
		DurationSeconds: read("duration"),
		PlaceInterval:   interval,
		// explosive_trap now has ONE radius doing both jobs (trigger + blast), so
		// TriggerRadius mirrors Radius rather than being a second authored number.
		Radius:          read("radius"),
		TriggerRadius:   read("radius"),
		DamagePerSecond: read("dps"),
		BurstDamage:     int(read("burstDamage")),
		SlowMultiplier:  read("slowMultiplier"),
		MarkMultiplier:  read("markMultiplier"),
		MarkDuration:    read("markDuration"),
	}, true
}

// def_EffectiveCooldown is the ability's authored cooldown, or 0 when the
// ability is unknown.
func def_EffectiveCooldown(abilityID string) float64 {
	if d, ok := getAbilityDef(abilityID); ok {
		return d.EffectiveCooldown()
	}
	return 0
}

// DebugEffectiveTrapStats computes the effective planted-trap stats for the
// unit's currently owned trap ABILITY (if any). Returns zero-value and false if
// the unit knows no trap ability.
//
// The base stats come from the trap ability's place_trap action config
// (trapConfigFromAbilityLocked) — the single source of truth now that the
// bronze trap perks are gone — with fire_pit's per-rank overrides applied via
// toTrapConfig(unit.Rank), then the same Silver/Gold perk modifiers folded on
// top exactly as before. The placement interval folds the ability-cooldown
// modifier (rapid_deployment, now an AbilityModifier.CooldownMult) instead of
// the retired TrapModifiers.CooldownMultiplier.
//
// Safe to call under s.mu write lock.
func (s *GameState) DebugEffectiveTrapStats(unit *Unit) (EffectiveTrapStats, bool) {
	trapID := unitTrapAbilityIDLocked(unit)
	if trapID == "" {
		return EffectiveTrapStats{}, false
	}
	// MIGRATED traps (authored as composable visible zones) carry their stats as
	// ability PARAMETERS, not as a place_trap config. Build the snapshot from
	// the resolved parameters instead — which is strictly better than the legacy
	// path, because the resolution chokepoint has already folded in every perk,
	// item and advancement contribution, so the tooltip cannot drift from what
	// the ability actually does.
	if stats, ok := s.effectiveTrapStatsFromParamsLocked(unit, trapID); ok {
		return stats, true
	}
	tc, ok := trapConfigFromAbilityLocked(trapID, unit.Rank)
	if !ok {
		return EffectiveTrapStats{}, false
	}
	m := s.trapModifiersForUnitLocked(unit)
	specific := s.trapSpecificModifiersForUnitLocked(unit, trapID)
	cdMods := s.abilityScalarModifiersForCasterLocked(unit, trapID)
	out := EffectiveTrapStats{
		PerkID:          trapID,
		DurationSeconds: tc.DurationSeconds * m.DurationMultiplier,
		PlaceInterval:   tc.PlaceIntervalSeconds * cdMods.CooldownMult,
	}
	switch trapID {
	case "caltrops":
		out.Radius = tc.Radius * m.RadiusMultiplier
		out.DamagePerSecond = tc.DamagePerSecond * m.EffectMultiplier
		out.SlowMultiplier = amplifySlow(tc.SlowMultiplier, m.EffectMultiplier)
	case "fire_pit":
		out.Radius = tc.Radius * m.RadiusMultiplier
		out.DamagePerSecond = tc.DamagePerSecond * m.EffectMultiplier
		// Burn DPS in lasting_flames mode == fire pit's DamagePerSecond, so
		// it's already reflected in out.DamagePerSecond. Only the duration
		// needs its own debug field.
		out.LastingFlamesBurnDuration = specific.LastingFlamesBurnDuration * m.DurationMultiplier
	case "explosive_trap":
		out.Radius = tc.ExplosionRadius * m.RadiusMultiplier
		out.TriggerRadius = tc.TriggerRadius * m.RadiusMultiplier
		base := int(tc.BurstDamage)
		out.BurstDamage = int(float64(base)*m.EffectMultiplier + 0.5)
	case "marker_trap":
		out.Radius = tc.Radius * m.RadiusMultiplier
		out.MarkMultiplier = tc.MarkMultiplier * m.EffectMultiplier
		out.MarkDuration = tc.MarkDuration * m.EffectMultiplier
	}
	return out, true
}
