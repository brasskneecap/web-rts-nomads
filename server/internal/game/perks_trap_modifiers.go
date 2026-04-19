package game

// ═════════════════════════════════════════════════════════════════════════════
// TRAP MODIFIER PIPELINE
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

	// CooldownMultiplier scales placeIntervalSeconds when the placement
	// cooldown is reset after planting.
	CooldownMultiplier float64

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
		CooldownMultiplier: 1.0,
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
	for _, perkID := range unit.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		switch perkID {
		// ── Silver: global trap modifiers ─────────────────────────────────
		case "extended_setup":
			m.DurationMultiplier *= def.Config["durationMultiplier"]
		case "wider_nets":
			m.RadiusMultiplier *= def.Config["radiusMultiplier"]
		case "rapid_deployment":
			m.CooldownMultiplier *= def.Config["cooldownMultiplier"]
		case "amplified_effects":
			m.EffectMultiplier *= def.Config["effectMultiplier"]

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
	// AftershockDelaySeconds > 0 means the trap should fire a second blast
	// after this delay. Currently set by explosive_chain (applies only to
	// explosive_trap).
	AftershockDelaySeconds float64

	// barbed_field (silver): caltrops-only ramping bonus DPS. RampPerSecond
	// is added to the victim-specific damage-per-second for every second the
	// victim has been in a barbed zone (BarbedFieldStaySeconds). MaxBonusDPS
	// caps the bonus so ramp cannot spiral unbounded.
	BarbedFieldRampPerSec  float64
	BarbedFieldMaxBonusDPS float64

	// exposed_weakness (silver): marker_trap-only. Fraction of outgoing damage
	// reduction stamped onto marked victims (e.g. 0.20 = deal 20% less damage).
	// Piggybacks the shared WeakenedRemaining/WeakenedMultiplier state used by
	// Vanguard's punishing_guard — the outgoing-damage debuff plumbing is
	// already live in perkOutgoingDamageDebuffMultiplierLocked.
	ExposedWeakenedMultiplier float64

	// lasting_flames (silver): fire_pit-only. Burn DoT stamped on victims when
	// they LEAVE a lasting fire pit. The trap records the burn params it would
	// apply at exit; tickTrapperSilverDebuffsLocked detects the exit transition
	// and moves the armed params onto the victim's Burn* fields.
	LastingFlamesBurnDPS      float64
	LastingFlamesBurnDuration float64
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
		case "explosive_chain":
			if trapType == "explosive_trap" {
				m.AftershockDelaySeconds = def.Config["aftershockDelaySeconds"]
			}
		case "barbed_field":
			if trapType == "caltrops" {
				m.BarbedFieldRampPerSec = def.Config["rampPerSecond"]
				m.BarbedFieldMaxBonusDPS = def.Config["maxBonusDamagePerSecond"]
			}
		case "exposed_weakness":
			if trapType == "marker_trap" {
				m.ExposedWeakenedMultiplier = def.Config["weakenedMultiplier"]
			}
		case "lasting_flames":
			if trapType == "fire_pit" {
				m.LastingFlamesBurnDPS = def.Config["burnDamagePerSecond"]
				m.LastingFlamesBurnDuration = def.Config["burnDurationSeconds"]
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
	BarbedFieldRampPerSec     float64 // caltrops + barbed_field
	BarbedFieldMaxBonusDPS    float64 // caltrops + barbed_field
	ExposedWeakenedMultiplier float64 // marker_trap + exposed_weakness
	LastingFlamesBurnDPS      float64 // fire_pit + lasting_flames
	LastingFlamesBurnDuration float64 // fire_pit + lasting_flames
	AftershockDelaySeconds    float64 // explosive_trap + explosive_chain
}

// DebugEffectiveTrapStats computes the effective planted-trap stats for the
// unit's currently owned Bronze trap perk (if any). Returns zero-value and
// false if the unit owns no trap perk.
//
// Safe to call under s.mu write lock.
func (s *GameState) DebugEffectiveTrapStats(unit *Unit) (EffectiveTrapStats, bool) {
	if unit == nil {
		return EffectiveTrapStats{}, false
	}
	var def *PerkDef
	for _, id := range unit.PerkIDs {
		switch id {
		case "caltrops", "fire_pit", "explosive_trap", "marker_trap":
			def = perkDefByID(id)
		}
		if def != nil {
			break
		}
	}
	if def == nil {
		return EffectiveTrapStats{}, false
	}
	m := s.trapModifiersForUnitLocked(unit)
	specific := s.trapSpecificModifiersForUnitLocked(unit, def.ID)
	out := EffectiveTrapStats{
		PerkID:          def.ID,
		DurationSeconds: def.Config["durationSeconds"] * m.DurationMultiplier,
		PlaceInterval:   def.Config["placeIntervalSeconds"] * m.CooldownMultiplier,
	}
	switch def.ID {
	case "caltrops":
		out.Radius = def.Config["radius"] * m.RadiusMultiplier
		out.DamagePerSecond = def.Config["damagePerSecond"] * m.EffectMultiplier
		out.SlowMultiplier = amplifySlow(def.Config["slowMultiplier"], m.EffectMultiplier)
		out.BarbedFieldRampPerSec = specific.BarbedFieldRampPerSec * m.EffectMultiplier
		out.BarbedFieldMaxBonusDPS = specific.BarbedFieldMaxBonusDPS * m.EffectMultiplier
	case "fire_pit":
		out.Radius = def.Config["radius"] * m.RadiusMultiplier
		out.DamagePerSecond = def.Config["damagePerSecond"] * m.EffectMultiplier
		out.LastingFlamesBurnDPS = specific.LastingFlamesBurnDPS * m.EffectMultiplier
		out.LastingFlamesBurnDuration = specific.LastingFlamesBurnDuration * m.EffectMultiplier
	case "explosive_trap":
		out.Radius = def.Config["explosionRadius"] * m.RadiusMultiplier
		out.TriggerRadius = def.Config["triggerRadius"] * m.RadiusMultiplier
		base := int(def.Config["burstDamage"])
		out.BurstDamage = int(float64(base)*m.EffectMultiplier + 0.5)
		out.AftershockDelaySeconds = specific.AftershockDelaySeconds
	case "marker_trap":
		out.Radius = def.Config["radius"] * m.RadiusMultiplier
		out.MarkMultiplier = def.Config["markMultiplier"] * m.EffectMultiplier
		out.MarkDuration = def.Config["markDuration"] * m.EffectMultiplier
		out.ExposedWeakenedMultiplier = specific.ExposedWeakenedMultiplier * m.EffectMultiplier
	}
	return out, true
}
