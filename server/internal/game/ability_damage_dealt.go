package game

import (
	"sort"

	"webrts/server/pkg/protocol"
)

// ═════════════════════════════════════════════════════════════════════════════
// on_damage_dealt — composable ability trigger
//
// Semantics: "the attacking unit dealt damage" — fired for every ability the
// ATTACKER (DamageSource.AttackerUnitID) knows whose program declares an
// on_damage_dealt trigger whose DamageScope (ability_program.go) admits this
// instance. Unlike on_unit_death (ability_unit_death.go, which reacts to ONE
// specific ability — the one that dealt the killing blow — and so only ever
// needs a single getAbilityDef lookup), on_damage_dealt must consider EVERY
// ability the attacker owns, since any of them might carry a reactive
// trigger. That "scan the unit's own abilities" shape is what makes the
// hot-path cost argument below (and the precomputed set) necessary in a way
// on_unit_death never needed.
//
// PRODUCTION SAFETY / AUTHORABILITY: unlike on_unit_death and the zone-
// occupancy triggers, on_damage_dealt IS intended to be authored on real
// catalog content (this is the whole point of the task) — there is no
// "no catalog ability may use this trigger" guard here.
// ═════════════════════════════════════════════════════════════════════════════

// programDeclaresTrigger reports whether prog has at least one top-level
// trigger of type t. Nil-safe (a legacy/uncompiled ability has Program==nil).
// Deliberately only looks at TOP-LEVEL triggers — exactly what
// runProgramTriggersLocked itself dispatches from — not nested triggers
// inside create_zone/apply_status/launch_projectile/beam configs or action
// Children, mirroring on_unit_death's shape (a top-level program trigger,
// see fireOnUnitDeathLocked's doc comment).
func programDeclaresTrigger(prog *AbilityProgram, t TriggerType) bool {
	if prog == nil {
		return false
	}
	for i := range prog.Triggers {
		if prog.Triggers[i].Type == t {
			return true
		}
	}
	return false
}

// onDamageDealtAbilityIDs is the set of STATIC/EMBEDDED-catalog ability ids
// whose Program declares an on_damage_dealt trigger, built ONCE at package
// load — mirrors allActionTypes/knownTriggerTypes (ability_program_validate.go).
//
// THIS is the hot-path cost argument: applyUnitDamageWithSourceLocked runs on
// every single damage instance in the game, so fireOnDamageDealtLocked must
// be near-zero cost for the overwhelmingly common case (an attacking unit
// that owns no ability with this trigger). Without this set, answering "does
// ability X declare on_damage_dealt" would require resolving X's AbilityDef
// and scanning its Program.Triggers slice — for EVERY ability the attacker
// owns, on EVERY hit. With this set, it's one map lookup per owned ability
// id. The set itself is built exactly once (package init), never rebuilt on
// the tick path.
//
// Scoped to abilityDefsByID (the embedded catalog), NOT getAbilityDef's
// overlay-aware resolution: the runtimeAbilities overlay (ability_persistence.go
// / ability_preview.go) exists only for the ability editor's live-preview /
// hot-reload workflow and for tests — it is never populated on a real
// match's boot path — so this static snapshot already covers every
// production ability. abilityDeclaresOnDamageDealtTrigger below still answers
// correctly for an overlay-shadowed id (dev/test only); it just pays one
// extra, still-O(1), overlay lookup to do it instead of trusting this set
// blindly.
var onDamageDealtAbilityIDs = buildOnDamageDealtAbilityIDs()

func buildOnDamageDealtAbilityIDs() map[string]bool {
	out := make(map[string]bool)
	for id, def := range abilityDefsByID {
		if programDeclaresTrigger(def.Program, TriggerOnDamageDealt) {
			out[id] = true
		}
	}
	return out
}

// abilityDeclaresOnDamageDealtTrigger reports whether abilityID's CURRENTLY
// effective definition (overlay-first, the same resolution getAbilityDef
// uses) declares an on_damage_dealt trigger. O(1) in the common case: an
// RLock + a map-miss against the (normally EMPTY in production)
// runtimeAbilities overlay, then one map lookup against the load-time static
// set (onDamageDealtAbilityIDs) — never a scan of the ability's own
// Program.Triggers. Only when the overlay actually shadows THIS SPECIFIC id
// (dev/test only) does it fall back to checking that def's Program directly.
//
// No "Locked" suffix — like getAbilityDef (ability_defs.go), this manages
// its own lock (runtimeAbilitiesMu) and never touches GameState/s.mu, so it
// is callable with or without s.mu held.
func abilityDeclaresOnDamageDealtTrigger(abilityID string) bool {
	runtimeAbilitiesMu.RLock()
	overlayDef, inOverlay := runtimeAbilities[abilityID]
	runtimeAbilitiesMu.RUnlock()
	if inOverlay {
		return programDeclaresTrigger(overlayDef.Program, TriggerOnDamageDealt)
	}
	return onDamageDealtAbilityIDs[abilityID]
}

// damageTriggerScopeMatches reports whether scope (nil ⇒ ANY damage) admits
// src. See DamageTriggerScope's doc comment (ability_program.go) for the
// authoring semantics; validateAbilityProgram already rejects the
// self-contradictory AbilityID+Categories combination at authoring time, so
// this is a pure AND of two independent filters.
func damageTriggerScopeMatches(scope *DamageTriggerScope, src DamageSource) bool {
	if scope == nil {
		return true
	}
	if len(scope.Categories) > 0 {
		matched := false
		for _, c := range scope.Categories {
			if c == src.Category {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if scope.AbilityID != "" && scope.AbilityID != src.SourceAbilityID {
		return false
	}
	return true
}

// matchedDamageDealtTrigger pairs one matching AbilityTriggerDef with the
// AbilityDef (and its id) it belongs to, so fireOnDamageDealtLocked can sort
// matches by owning ability id (determinism) before running any of them —
// mirrors ownedRiderFragment's shape (ability_riders.go).
type matchedDamageDealtTrigger struct {
	abilityID string
	def       AbilityDef
	trigger   *AbilityTriggerDef
}

// fireOnDamageDealtLocked fires the ATTACKING unit's on_damage_dealt
// trigger(s) for one landed damage instance, for every ability it knows
// whose program declares a trigger of that type whose DamageScope admits src.
//
// attacker is resolved fresh from src.AttackerUnitID (never cached — AI_RULES
// target-by-ID discipline) and validated: src.AttackerUnitID == 0 means "not
// from a unit" (a building/trap/anonymous hit) and is the single cheapest
// bail-out; a resolved-but-dead attacker (HP<=0, e.g. already lethally hit
// earlier this same tick but not yet drained) is also skipped — a corpse
// does not react to the damage it dealt a moment before dying.
//
// RE-ENTRANCY: guarded by attacker.OnDamageDealtDispatchActive (state.go) —
// see that field's doc comment for why a ctx-scoped depth counter alone
// cannot bound this (each fire gets a brand-new RuntimeAbilityContext, so
// nothing on ctx survives across fires). A deal_damage action inside this
// trigger always attributes its damage to ctx.CasterID == attacker.ID (see
// ability_program_registry.go's deal_damage Execute), so a self-triggering
// program would otherwise recurse without bound the instant it dealt any
// further damage.
//
// damage is the amount that actually landed on target.HP (the post-mitigation
// value applyUnitDamageWithSourceLocked's own bookkeeping uses) — bound onto
// each fire's ctx as ctx.Named["trigger_damage"] (ctxScalar), reusing the
// exact name runAbilityRidersForCasterLocked binds (ability_riders.go) so an
// authored deal_damage action can reference either seam identically via
// AmountRef.
//
// Must be called under s.mu write lock, from applyUnitDamageWithSourceLocked's
// single canonical HP-loss point (perks_defense.go).
func (s *GameState) fireOnDamageDealtLocked(target *Unit, damage int, src DamageSource) {
	if target == nil || damage <= 0 || src.AttackerUnitID == 0 {
		return
	}
	attacker := s.getUnitByIDLocked(src.AttackerUnitID)
	if attacker == nil || attacker.HP <= 0 {
		return
	}
	if len(attacker.Abilities) == 0 {
		return // cheapest possible bail — nothing to scan at all
	}
	if attacker.OnDamageDealtDispatchActive {
		return // re-entrancy guard — see this field's doc comment (state.go)
	}

	// Gather: for each owned ability id, the cheap membership check first
	// (no Program resolution/scan for the common non-matching case); only an
	// ability that DOES declare the trigger type pays for a def resolution +
	// a scan of its own (small) Triggers slice to find scope-matching
	// trigger(s).
	var matches []matchedDamageDealtTrigger
	for _, id := range attacker.Abilities {
		if !abilityDeclaresOnDamageDealtTrigger(id) {
			continue
		}
		def, ok := getAbilityDef(id)
		if !ok || def.Program == nil {
			continue
		}
		for i := range def.Program.Triggers {
			trg := &def.Program.Triggers[i]
			if trg.Type != TriggerOnDamageDealt {
				continue
			}
			if !damageTriggerScopeMatches(trg.DamageScope, src) {
				continue
			}
			matches = append(matches, matchedDamageDealtTrigger{abilityID: id, def: def, trigger: trg})
		}
	}
	if len(matches) == 0 {
		return
	}
	// Determinism: run in ability-id order (then authored trigger order
	// within one ability, via a stable sort) — NEVER attacker.Abilities slot
	// order (authored/spawn order, not guaranteed sorted) and never map
	// iteration order (sim determinism rule, AI_RULES.md).
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].abilityID < matches[j].abilityID })

	attacker.OnDamageDealtDispatchActive = true
	defer func() { attacker.OnDamageDealtDispatchActive = false }()

	for i := range matches {
		m := &matches[i]
		ctx := &RuntimeAbilityContext{
			CasterID:      attacker.ID,
			AbilityID:     m.abilityID,
			OwnerUnitID:   attacker.ID,
			EventPosition: protocol.Vec2{X: target.X, Y: target.Y},
			// Bind the DAMAGED unit both ways (mirrors
			// runAbilityRidersForCasterLocked's target binding, ability_riders.go)
			// so an authored select_targets can reach it via either
			// source:"initial_target" or source:"current_event" — on_unit_death
			// only needs CurrentEventUnitID (its "corpse" binding is inherently
			// event-shaped), but on_damage_dealt's damaged unit is also a natural
			// InitialTarget for a reactive deal_damage/apply_status aimed straight
			// back at whoever was just hit.
			InitialTarget:      target.ID,
			CurrentEventUnitID: target.ID,
			Named: map[string]ContextValue{
				"trigger_damage": {Kind: ctxScalar, Scalar: float64(damage)},
			},
			Trace:      s.previewTrace,
			now:        s.previewClock,
			program:    m.def.Program,
			abilityDef: &m.def,
		}
		// Mirrors runProgramTriggersLocked's own condition-gate exactly
		// (ability_exec.go) — TODO(phase-3b): trg.Conditions is currently
		// always treated as passing; wired here so this trigger evaluates
		// identically to every other trigger type once that lands.
		if !s.triggerConditionsPassLocked(ctx, m.trigger) {
			ctx.trace("condition_failed", m.trigger.ID, nil)
			continue
		}
		ctx.trace("trigger_fired", m.trigger.ID, map[string]any{"type": string(m.trigger.Type)})
		s.runTriggerActionsLocked(ctx, m.trigger, m.trigger.ID)
	}
}
