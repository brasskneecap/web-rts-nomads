package game

import "sort"

// ─────────────────────────────────────────────────────────────────────────────
// Ability riders — RUNTIME (T2).
//
// T1 (perk_defs.go) landed the schema: PerkDef.AbilityRiders is a list of
// AbilityRider{Target, Trigger, Actions} fragments a perk grafts onto a
// target ability's existing trigger. This file is the generic engine that
// GATHERS a caster's owned-perk rider fragments for (abilityID, trigger) and
// RUNS them, mirroring abilityScalarModifiersForCasterLocked's owned-perk
// walk (ability_modifiers.go) and fireChannelBeamTickLocked's context-build +
// run-loop shape (ability_channel.go).
//
// NOT done here: wiring into the live channel tick (T4) and "fraction of
// tick damage" amount resolution for a rider's own deal_damage authoring
// (T3). Both are separate tasks; this file only builds the runner.
// ─────────────────────────────────────────────────────────────────────────────

// ownedRiderFragment pairs one AbilityRider with the id of the perk that owns
// it — the owning perk id is not part of AbilityRider itself (it's PerkDef-
// scoped data), but the runner needs it for deterministic ordering and for
// building a stable trace path per fragment.
type ownedRiderFragment struct {
	perkID string
	rider  AbilityRider
}

// gatherOwnedRiderFragmentsLocked walks caster.PerkIDs, collecting every
// AbilityRider on an owned perk that targets (abilityID, trigger), then sorts
// the result by owning perk id — NEVER by caster.PerkIDs slice order or map
// iteration order (sim determinism rule, AI_RULES.md). Ties (a single perk
// contributing more than one matching rider) keep that perk's authored
// Actions-list order via a stable sort. Safe on nil caster / empty abilityID
// (returns nil). Caller holds s.mu (read or write).
func (s *GameState) gatherOwnedRiderFragmentsLocked(caster *Unit, abilityID string, trigger TriggerType) []ownedRiderFragment {
	if caster == nil || abilityID == "" {
		return nil
	}
	var frags []ownedRiderFragment
	for _, perkID := range caster.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		for i := range def.AbilityRiders {
			r := def.AbilityRiders[i]
			if r.Target != abilityID || r.Trigger != trigger {
				continue
			}
			frags = append(frags, ownedRiderFragment{perkID: perkID, rider: r})
		}
	}
	sort.SliceStable(frags, func(i, j int) bool { return frags[i].perkID < frags[j].perkID })
	return frags
}

// ownedRiderFragmentsForLocked returns, in DETERMINISTIC order, every
// AbilityRider on the caster's owned perks that targets (abilityID,
// trigger). Order is sorted by owning perk id (see
// gatherOwnedRiderFragmentsLocked) and never depends on caster.PerkIDs slice
// order or map iteration order. Safe on nil caster / empty abilityID
// (returns nil). Caller holds s.mu (read or write).
func (s *GameState) ownedRiderFragmentsForLocked(caster *Unit, abilityID string, trigger TriggerType) []AbilityRider {
	frags := s.gatherOwnedRiderFragmentsLocked(caster, abilityID, trigger)
	if len(frags) == 0 {
		return nil
	}
	out := make([]AbilityRider, len(frags))
	for i, f := range frags {
		out[i] = f.rider
	}
	return out
}

// runAbilityRidersForCasterLocked runs every owned-perk rider fragment for
// (abilityID, trigger) in perk-id-sorted order (gatherOwnedRiderFragmentsLocked),
// each in its OWN fresh RuntimeAbilityContext — so one fragment's deal_damage
// cannot leak its ctx.lastAppliedDamage into another fragment's (or the
// caller's) decision-making. Each fragment's ctx is seeded the same way
// fireChannelBeamTickLocked seeds a channel tick's ctx: InitialTarget and
// CurrentEventUnitID both bound to target.ID, program/abilityDef resolved
// from getAbilityDef(abilityID) (nil when the target ability id isn't
// registered — a rider's own actions carry their own targeting via
// select_targets/deal_damage Target queries, so a program-less run is still
// meaningful), plus the triggering event's damage bound as
// ctx.Named["trigger_damage"] (ctxScalar) so a rider action can reference it
// via a ContextRef. Each fragment's actions run through the op-budget guard
// exactly like runTriggerActionsLocked. No-op when caster/target is nil or
// caster owns no matching rider. Caller holds s.mu write lock.
func (s *GameState) runAbilityRidersForCasterLocked(caster, target *Unit, abilityID string, trigger TriggerType, triggerDamage int) {
	if caster == nil || target == nil {
		return
	}
	frags := s.gatherOwnedRiderFragmentsLocked(caster, abilityID, trigger)
	if len(frags) == 0 {
		return
	}
	var program *AbilityProgram
	var abilityDefPtr *AbilityDef
	if def, ok := getAbilityDef(abilityID); ok {
		program = def.Program
		abilityDefPtr = &def
	}
	for _, frag := range frags {
		ctx := &RuntimeAbilityContext{
			CasterID:           caster.ID,
			AbilityID:          abilityID,
			InitialTarget:      target.ID,
			CurrentEventUnitID: target.ID,
			program:            program,
			abilityDef:         abilityDefPtr,
			Named: map[string]ContextValue{
				"trigger_damage": {Kind: ctxScalar, Scalar: float64(triggerDamage)},
			},
			Trace: s.previewTrace,
			now:   s.previewClock,
		}
		path := "rider[" + frag.perkID + "]"
		for i := range frag.rider.Actions {
			if ctx.opsExhausted() {
				break
			}
			s.executeActionLocked(ctx, &frag.rider.Actions[i], path)
		}
	}
}
