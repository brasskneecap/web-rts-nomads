package game

import (
	"fmt"

	"webrts/server/pkg/protocol"
)

// Beam is a beam visual entity the client renders as a line between two points.
// There are two flavors, distinguished by Momentary:
//
//   - Channeled (Momentary == false): exists for the duration of a unit's
//     channel and carries NO simulation state — all damage, mana, and stop
//     logic is driven by the Unit's Channel* fields. Its endpoints are the
//     LIVE caster/target positions (resolved by the client each frame), so it
//     is removed when the channel stops or when either participant leaves the
//     game (removeBeamForUnitLocked / removeBeamForTargetLocked).
//
//   - Momentary (Momentary == true): a one-shot "zap" fired by an emitter of
//     EmitterKindBeam (e.g. an item on-hit proc). Its damage is applied at
//     spawn time by the firing site; the Beam is purely a short-lived visual
//     that decays over RemainingSeconds. Its endpoints are FROZEN at fire time
//     (OriginX/Y → TargetX/Y) so the flash still renders even if the target
//     dies from the same hit or moves during the flash — it is NOT removed by
//     participant-removal paths.
//
// ID-not-pointer rule: CasterUnitID and TargetUnitID are integer IDs.
type Beam struct {
	// ID is the stable wire identifier for this beam (e.g. "beam-0").
	ID string
	// CasterUnitID is the ID of the unit channeling the ability (or the VISUAL
	// origin of a momentary proc beam — the attacker for the primary hit, or
	// the previous victim on a bounce hop). Drives the client's origin-lift
	// sprite lookup; not used for damage credit (see AttackerUnitID).
	CasterUnitID int
	// TargetUnitID is the ID of the enemy unit being drained (or hit, for a
	// momentary proc beam).
	TargetUnitID int
	// OwnerPlayerID is the player who owns the caster (for FOW filtering).
	OwnerPlayerID string
	// AbilityID is the ability driving this beam (e.g. "siphon_life"). Empty
	// for momentary proc beams (they aren't tied to an ability).
	AbilityID string
	// Variant is the client-side renderer variant (e.g. "siphon_life",
	// "lightning_bolt") — for momentary beams this is the emitter def id, which
	// selects assets/beams/<variant>/.
	Variant string

	// ── Momentary (one-shot proc zap) fields — all zero for channel beams ────
	// Momentary marks a self-contained, short-lived beam flash whose endpoints
	// are frozen and whose lifetime is RemainingSeconds. Channel beams leave
	// this false and use live participant positions instead.
	Momentary bool
	// RemainingSeconds counts a momentary beam down to removal (see
	// tickBeamsLocked). Unused (0) for channel beams.
	RemainingSeconds float64
	// OriginX/Y and TargetX/Y are the frozen world endpoints of a momentary
	// beam, snapshot from the attacker/target positions at fire time. Unused
	// for channel beams, whose endpoints are the live unit positions.
	OriginX, OriginY float64
	TargetX, TargetY float64

	// ── Deferred proc damage — momentary beams only ─────────────────────────
	// A beam is instantaneous, so its damage would otherwise land on the SAME
	// tick as the triggering hit and merge into that hit's floating number. To
	// read as its own number, the damage is deferred by DamageDelayRemaining:
	// tickBeamsLocked applies PendingDamage to TargetUnitID once the delay
	// elapses (a beat after the flash appears), then zeroes PendingDamage so it
	// lands exactly once. CasterUnitID is the attacker for attribution.
	PendingDamage        int
	DamageType           DamageType
	DamageDelayRemaining float64
	// ImpactEffect is the effect id played on the target when the deferred
	// damage lands (e.g. "fizzle"), mirroring a projectile's on-land impact.
	ImpactEffect string
	// AttackerUnitID credits the deferred damage's kill/XP. Distinct from
	// CasterUnitID because a bounce hop's beam VISUALLY leaves the previous
	// victim, but the kill must still credit the original wielder. Defaults to
	// CasterUnitID for the primary hit (attacker == visual origin).
	AttackerUnitID int
	// SourceAbilityID carries ProcSource.SourceAbilityID through to the
	// deferred damage's DamageSource (applyBeamPendingDamageLocked) so a
	// chain-bounce kill fired from an authored ability (chain_lightning-shape,
	// via fireAbilityChainLocked) attributes to that ability for on_unit_death
	// purposes. "" for every non-ability proc beam (equipment/item/perk),
	// matching ProcSource.SourceAbilityID's zero-value contract. Distinct from
	// AbilityID above, which is the CHANNEL-beam field (siphon_life) and is
	// never set on a momentary beam.
	SourceAbilityID string
	// SlowMultiplier / SlowDurationSeconds: an on-hit chill carried from the
	// proc config, applied to TargetUnitID when the deferred damage lands (via
	// ApplySlowLocked). Zero ⇒ no slow.
	SlowMultiplier      float64
	SlowDurationSeconds float64
	// BurnDamagePerSecond / BurnDurationSeconds: an on-hit burn carried from the
	// proc config, igniting TargetUnitID when the deferred damage lands. Credit
	// goes to AttackerUnitID (the original wielder). Zero ⇒ no burn.
	BurnDamagePerSecond float64
	BurnDurationSeconds float64

	// ── Composable impact (launch_beam's redesign) ──────────────────────────
	// The authored-impact-actions momentary flavor, mirroring
	// Projectile.ImpactActions/ImpactOpsBudget/ImpactDamageMultiplier
	// (projectile.go) for a beam instead of a projectile. Set exactly for a
	// beam spawned by the composable launch_beam action
	// (ability_exec_beam.go); nil/zero for every proc-fired momentary beam
	// (spawnMomentaryDamageBeamLocked) and every channel beam, which never
	// carry impact actions at all — landing damage there goes through
	// PendingDamage instead (see tickBeamsLocked).
	//
	// ImpactActions is the compiled on_beam_impact trigger's actions, carried
	// across tick boundaries as plain data (AbilityActionDef — never *Unit,
	// per AI_RULES), same discipline as Projectile.ImpactActions /
	// AbilityZone.Triggers.
	ImpactActions []AbilityActionDef
	// ImpactOpsBudget is the shared, cross-tick op-budget counter this beam's
	// impact will decrement when it fires — mirrors
	// Projectile.ImpactOpsBudget (see ability_exec_projectile.go's CROSS-TICK
	// OP BUDGET section for the full design this reuses verbatim).
	ImpactOpsBudget *int
	// CasterID is the ORIGINAL caster for the impact ctx — distinct from
	// CasterUnitID, which is the VISUAL origin of the beam (may differ on a
	// future bounce-hop shape, mirroring the projectile CasterUnitID/
	// AttackerUnitID split). Resolved to *Unit at point of use, never stored
	// as a pointer.
	CasterID int
	// AbilityIDForCtx is the ability id threaded into the impact ctx (so
	// deal_damage etc. fold the caster's spell modifiers) — distinct from
	// AbilityID, which is the CHANNEL-beam field (siphon_life) and is never
	// set on a momentary beam.
	AbilityIDForCtx string
	// ImpactDamageMultiplier carries the LAUNCHING ctx's
	// damageEffectivenessMultiplier forward to the impact ctx
	// fireBeamImpactLocked builds on a later tick — mirrors
	// Projectile.ImpactDamageMultiplier. 0 is treated as 1.0 (no scaling) by
	// RuntimeAbilityContext.effectiveDamageMultiplier().
	ImpactDamageMultiplier float64
	// CarriedNamed snapshots the LAUNCHING ctx's Named map (ctx.Named) at the
	// moment this beam was spawned by a launch_beam action running INSIDE
	// another beam's on_beam_impact trigger — the mechanism that lets
	// chain_lightning's authored bounce chain (compileChainLightningActions,
	// ability_compile.go) accumulate its "already hit" ctxUnitSet across
	// hops. Every hop's on_beam_impact fires in a BRAND NEW
	// RuntimeAbilityContext (fireBeamImpactLocked below builds one fresh per
	// beam, same as fireProjectileImpactLocked) — without this field, that
	// fresh ctx.Named would start empty every hop, silently forgetting every
	// unit store_targets(merge:true) had accumulated so far and letting the
	// chain double back onto an already-struck victim (or the primary
	// target). nil for every beam that is NOT itself a launch_beam-spawned
	// nested relaunch (the ordinary single-hop case, every proc beam, every
	// channel beam) — fireBeamImpactLocked falls back to a fresh empty map
	// exactly as before when this is nil.
	CarriedNamed map[string]ContextValue
	// impactFired guards ImpactActions from running more than once: set true
	// the first time tickBeamsLocked's delay countdown reaches zero, so a
	// later tick (or the RemainingSeconds expiry safety net) can never re-fire
	// it.
	impactFired bool
}

// spawnBeamLocked creates a new Beam entity, appends it to s.Beams, and
// returns it. Called by beginAbilityChannelLocked when a channel starts.
//
// Caller holds s.mu write lock.
func (s *GameState) spawnBeamLocked(caster *Unit, target *Unit, abilityID, variant string) *Beam {
	b := &Beam{
		ID:            fmt.Sprintf("beam-%d", s.nextBeamID),
		CasterUnitID:  caster.ID,
		TargetUnitID:  target.ID,
		OwnerPlayerID: caster.OwnerID,
		AbilityID:     abilityID,
		Variant:       variant,
	}
	s.nextBeamID++
	s.Beams = append(s.Beams, b)
	return b
}

// spawnMomentaryBeamLocked creates a self-contained one-shot beam flash from
// the attacker to the target, used by EmitterKindBeam procs. Endpoints are
// frozen at the current unit positions and the beam decays over durationMs.
// It carries NO damage — the caller applies damage separately (a beam is
// instantaneous, so damage lands at fire time, not on the visual's removal).
//
// Caller holds s.mu write lock.
func (s *GameState) spawnMomentaryBeamLocked(attacker, target *Unit, variant string, durationMs int) *Beam {
	if durationMs <= 0 {
		durationMs = defaultBeamDurationMs
	}
	b := &Beam{
		ID:               fmt.Sprintf("beam-%d", s.nextBeamID),
		CasterUnitID:     attacker.ID,
		AttackerUnitID:   attacker.ID,
		TargetUnitID:     target.ID,
		OwnerPlayerID:    attacker.OwnerID,
		Variant:          variant,
		Momentary:        true,
		RemainingSeconds: float64(durationMs) / 1000.0,
		OriginX:          attacker.X,
		OriginY:          attacker.Y,
		TargetX:          target.X,
		TargetY:          target.Y,
	}
	s.nextBeamID++
	s.Beams = append(s.Beams, b)
	return b
}

// spawnMomentaryDamageBeamLocked spawns a one-shot beam flash from a frozen
// origin point to `to` and schedules `damage` (typed) to land on `to` after
// `delaySec`, credited to src.OwnerUnitID. fromUnitID is the VISUAL origin
// unit (drives the client's origin-lift sprite lookup; 0 when the beam leaves
// a non-unit source) and fromX/Y freeze the beam's start — the visual origin
// and the kill credit differ on a bounce hop, where the beam leaps off a
// victim but the original source still gets the kill.
//
// Caller holds s.mu write lock.
func (s *GameState) spawnMomentaryDamageBeamLocked(src ProcSource, fromUnitID int, fromX, fromY float64, to *Unit, variant string, damage int, dmgType DamageType, impactEffect string, durationMs int, delaySec float64) *Beam {
	if durationMs <= 0 {
		durationMs = defaultBeamDurationMs
	}
	b := &Beam{
		ID:                   fmt.Sprintf("beam-%d", s.nextBeamID),
		CasterUnitID:         fromUnitID,
		AttackerUnitID:       src.OwnerUnitID,
		TargetUnitID:         to.ID,
		OwnerPlayerID:        src.OwnerPlayerID,
		Variant:              variant,
		Momentary:            true,
		RemainingSeconds:     float64(durationMs) / 1000.0,
		OriginX:              fromX,
		OriginY:              fromY,
		TargetX:              to.X,
		TargetY:              to.Y,
		PendingDamage:        damage,
		DamageType:           dmgType,
		DamageDelayRemaining: delaySec,
		ImpactEffect:         impactEffect,
		SourceAbilityID:      src.SourceAbilityID,
	}
	s.nextBeamID++
	s.Beams = append(s.Beams, b)
	return b
}

// spawnBeamWithImpactActionsLocked spawns a one-shot beam flash from a frozen
// origin point (fromX/Y) to `to`, carrying a compiled on_beam_impact
// trigger's actions to run once ImpactDelaySeconds elapses — the beam
// analogue of fireProjectileWithImpactActionsLocked (projectile.go). It
// carries NO baked damage (unlike spawnMomentaryDamageBeamLocked): the
// impact's own deal_damage action (if authored) is what applies damage, via
// fireBeamImpactLocked/tickBeamsLocked.
//
// casterID credits the impact ctx's caster (AI_RULES: stored as an ID, never
// a pointer). fromUnitID is the VISUAL origin unit (drives the client's
// origin-lift sprite lookup; 0 when the beam leaves a non-unit source) and
// fromX/Y freeze the beam's start, mirroring spawnMomentaryDamageBeamLocked's
// identical caster/visual-origin split. durationMs<=0 defaults to
// defaultBeamDurationMs.
//
// carriedNamed is a snapshot of the LAUNCHING ctx's Named map to thread onto
// the spawned beam's CarriedNamed (see that field's doc comment) — nil for
// every non-chained call site (spawnBeamWithImpactActionsLocked's sole
// caller, launch_beam's Execute, always passes ctx.Named, which is nil/empty
// for a top-level cast and populated when this launch_beam is itself running
// inside another beam's on_beam_impact).
//
// Caller holds s.mu write lock.
func (s *GameState) spawnBeamWithImpactActionsLocked(casterID, fromUnitID int, fromX, fromY float64, to *Unit, variant, abilityID string, impactActions []AbilityActionDef, budget *int, dmgMult float64, durationMs int, delaySec float64, carriedNamed map[string]ContextValue) *Beam {
	if durationMs <= 0 {
		durationMs = defaultBeamDurationMs
	}
	var ownerPlayerID string
	if caster := s.getUnitByIDLocked(casterID); caster != nil {
		ownerPlayerID = caster.OwnerID
	}
	b := &Beam{
		ID:                     fmt.Sprintf("beam-%d", s.nextBeamID),
		CasterUnitID:           fromUnitID,
		AttackerUnitID:         casterID,
		TargetUnitID:           to.ID,
		OwnerPlayerID:          ownerPlayerID,
		Variant:                variant,
		Momentary:              true,
		RemainingSeconds:       float64(durationMs) / 1000.0,
		OriginX:                fromX,
		OriginY:                fromY,
		TargetX:                to.X,
		TargetY:                to.Y,
		DamageDelayRemaining:   delaySec,
		ImpactActions:          impactActions,
		ImpactOpsBudget:        budget,
		CasterID:               casterID,
		AbilityIDForCtx:        abilityID,
		ImpactDamageMultiplier: dmgMult,
		CarriedNamed:           cloneNamedContext(carriedNamed),
	}
	s.nextBeamID++
	s.Beams = append(s.Beams, b)
	return b
}

// cloneNamedContext returns a shallow copy of a RuntimeAbilityContext.Named
// map, or nil if src is empty/nil. A shallow copy is sufficient: every writer
// of ctx.Named (store_targets, bindActionOutputsLocked) always REPLACES a
// key's ContextValue with a freshly built one rather than mutating an
// existing UnitIDs slice in place, so a later hop's writes can never reach
// back and corrupt an earlier hop's (or a sibling beam's) snapshot.
func cloneNamedContext(src map[string]ContextValue) map[string]ContextValue {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]ContextValue, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// fireBeamImpactLocked runs a launch_beam-spawned momentary beam's compiled
// on_beam_impact actions — the beam analogue of fireProjectileImpactLocked
// (projectile.go). Builds a fresh RuntimeAbilityContext bound to this beam's
// caster/target/position, re-resolves the ability def (so deal_damage folds
// the caster's spell modifiers exactly once, at parity with the projectile
// impact path), and runs b.ImpactActions through the shared executor,
// honoring the shared cross-tick op budget (b.ImpactOpsBudget).
//
// ctx.Named seeds from b.CarriedNamed (see that field's doc comment) instead
// of always starting empty: a chain's accumulated "hit" ctxUnitSet must
// survive from one hop's beam into the next hop's freshly built ctx, since
// each hop's on_beam_impact runs in its OWN RuntimeAbilityContext (this
// function is called once per beam, on whatever later tick its delay
// elapses). cloneNamedContext defends the ORIGINAL b.CarriedNamed against
// this ctx's own writes, in case anything else ever reads it back (defensive
// only — nothing does today).
//
// Caller holds s.mu write lock.
func (s *GameState) fireBeamImpactLocked(b *Beam) {
	def, ok := getAbilityDef(b.AbilityIDForCtx)
	named := cloneNamedContext(b.CarriedNamed)
	if named == nil {
		named = map[string]ContextValue{}
	}
	ctx := &RuntimeAbilityContext{
		CasterID:                      b.CasterID,
		AbilityID:                     b.AbilityIDForCtx,
		InitialTarget:                 b.TargetUnitID,
		ImpactPosition:                protocol.Vec2{X: b.TargetX, Y: b.TargetY},
		EventPosition:                 protocol.Vec2{X: b.TargetX, Y: b.TargetY},
		CurrentEventUnitID:            b.TargetUnitID,
		Named:                         named,
		Trace:                         s.previewTrace,
		now:                           s.previewClock,
		sharedOpsRemaining:            b.ImpactOpsBudget,
		damageEffectivenessMultiplier: b.ImpactDamageMultiplier,
	}
	if ok {
		ctx.program = def.Program
		ctx.abilityDef = &def
	}
	path := "on_beam_impact"
	for i := range b.ImpactActions {
		if ctx.opsExhausted() {
			break
		}
		s.executeActionLocked(ctx, &b.ImpactActions[i], path)
	}
}

// tickBeamsLocked advances momentary beams: it lands their deferred proc damage
// once the delay elapses and removes the flashes that have expired. Channel
// beams (Momentary == false) are untouched — their lifetime is owned by the
// channel state machine, not a timer. No RNG, no cross-tick pointers: keeps
// simulation determinism.
//
// RE-ENTRANT APPEND HAZARD: a composable on_beam_impact trigger can itself
// contain a launch_beam action (chain_lightning's authored bounce chain —
// ability_compile.go's compileChainLightningActions), which appends a brand
// new *Beam to s.Beams from INSIDE fireBeamImpactLocked, i.e. while this very
// loop is mid-range over the pre-call s.Beams. The old
// `kept := s.Beams[:0]; for _, b := range s.Beams` idiom is unsafe here: the
// range expression is evaluated once (fixed length) at loop start, so a beam
// appended mid-loop is invisible to the current iteration, AND kept aliases
// the SAME backing array s.Beams is being appended to — the final
// `s.Beams = kept` (or an append-triggered reallocation racing with kept's
// own writes) silently drops or corrupts whatever the impact just spawned.
// Fixed below by iterating a stable snapshot (original) instead of s.Beams
// itself, building kept into a fresh backing array, and — after the loop —
// re-attaching anything appended past the snapshot's length (impact
// processing only ever appends to s.Beams, never removes, so everything at
// index >= len(original) in the live s.Beams is exactly what got spawned
// during this tick and must survive into the next one).
//
// Caller holds s.mu write lock.
func (s *GameState) tickBeamsLocked(dt float64) {
	if len(s.Beams) == 0 {
		return
	}
	var deadUnitIDs []int
	original := s.Beams
	kept := make([]*Beam, 0, len(original))
	for _, b := range original {
		if b.Momentary {
			if len(b.ImpactActions) > 0 {
				// Composable launch_beam impact: deferred a beat after the
				// flash appears, exactly like PendingDamage below, but runs
				// the authored on_beam_impact actions instead of a single
				// baked damage number. impactFired guards it from ever firing
				// twice (fireBeamImpactLocked itself does no such guarding).
				if !b.impactFired {
					b.DamageDelayRemaining -= dt
					if b.DamageDelayRemaining <= 0 {
						b.impactFired = true
						s.fireBeamImpactLocked(b)
					}
				}
			} else if b.PendingDamage > 0 {
				// Deferred proc damage lands a beat AFTER the triggering hit
				// so it reads as its own damage number. Apply exactly once
				// when the delay elapses (applyBeamPendingDamageLocked zeroes
				// PendingDamage).
				b.DamageDelayRemaining -= dt
				if b.DamageDelayRemaining <= 0 {
					s.applyBeamPendingDamageLocked(b, &deadUnitIDs)
				}
			}
			b.RemainingSeconds -= dt
			if b.RemainingSeconds <= 0 {
				// Safety net: if the flash somehow expired before the delay
				// elapsed (delay >= duration), still land the damage so a
				// rolled proc — or an authored impact — is never silently
				// dropped.
				if len(b.ImpactActions) > 0 {
					if !b.impactFired {
						b.impactFired = true
						s.fireBeamImpactLocked(b)
					}
				} else if b.PendingDamage > 0 {
					s.applyBeamPendingDamageLocked(b, &deadUnitIDs)
				}
				continue // flash finished — drop
			}
		}
		kept = append(kept, b)
	}
	// Carry forward any beam a nested launch_beam appended to s.Beams DURING
	// the loop above (see the RE-ENTRANT APPEND HAZARD doc comment) — it lives
	// past the original snapshot's length in the current, live s.Beams.
	//
	// INVARIANT this re-attach relies on: impact actions (fireBeamImpactLocked,
	// called above) must only ever APPEND to s.Beams, never remove or reorder
	// it mid-loop. original is a snapshot (a slice header aliasing the same
	// backing array s.Beams had at loop start); a mid-loop in-place removal or
	// reorder of s.Beams would shift indices out from under that aliased
	// snapshot and this slice-past-len(original) re-attach would silently
	// drop or duplicate beams instead of carrying forward exactly the ones
	// spawned during this tick.
	if len(s.Beams) > len(original) {
		kept = append(kept, s.Beams[len(original):]...)
	}
	s.Beams = kept
	// Remove anything the deferred damage just killed, mirroring
	// tickProjectilesLocked. Momentary beams are skipped by the removal paths,
	// so a beam that just killed its own target keeps flashing.
	for _, id := range deadUnitIDs {
		s.removeUnitLocked(id)
	}
}

// applyBeamPendingDamageLocked lands a momentary beam's deferred proc damage on
// its target, then clears PendingDamage so it can never apply twice. Bypasses
// the on-hit hub (direct HP pipeline, like a SkipOnHitEffects proc bolt) so a
// proc can't trigger another proc. If the target is already gone/dead the zap
// fizzles harmlessly — same "lost the target" semantics a projectile has.
//
// Caller holds s.mu write lock.
func (s *GameState) applyBeamPendingDamageLocked(b *Beam, deadUnitIDs *[]int) {
	damage := b.PendingDamage
	b.PendingDamage = 0 // land exactly once, even across the safety-net path
	if damage <= 0 {
		return
	}
	target := s.getUnitByIDLocked(b.TargetUnitID)
	if target == nil || target.HP <= 0 || !target.Visible {
		return
	}
	// Category depends on WHO fired this momentary beam: an ability's chain
	// bounce (chain_lightning, via fireAbilityChainLocked stamping
	// ProcSource.SourceAbilityID) carries SourceAbilityID through to here and
	// is DamageCategoryAbility; an equipment/item proc beam (Kind stays
	// "item-proc" — no ability ever attaches its id) never sets
	// SourceAbilityID and is DamageCategoryItem. Same beam-bounce mechanism,
	// two different sources — see SourceAbilityID's doc comment above.
	category := DamageCategoryItem
	if b.SourceAbilityID != "" {
		category = DamageCategoryAbility
	}
	s.applyUnitDamageWithSourceLocked(target, damage, DamageSource{
		AttackerUnitID:  b.AttackerUnitID,
		Kind:            "item-proc",
		Category:        category,
		DamageType:      b.DamageType,
		SourceAbilityID: b.SourceAbilityID,
	})
	// On-hit slow: routed to the cold (chill) or physical track by the beam's
	// damage type. No-op on zero / out-of-range values.
	s.applyProcSlowLocked(target.ID, b.SlowMultiplier, b.SlowDurationSeconds, b.DamageType)
	// On-hit burn: ignite the target with a fire DoT, credited to the original
	// wielder (AttackerUnitID). No-op when the proc carries no burn.
	s.applyProcBurnLocked(target.ID, b.BurnDamagePerSecond, b.BurnDurationSeconds, b.AttackerUnitID)
	if b.ImpactEffect != "" {
		s.playEffectOnUnitLocked(target, b.ImpactEffect)
	}
	if target.HP <= 0 {
		target.HP = 0
		// Ability-attributed beam kills (chain_lightning-shape, primary or
		// bounce hop) defer removal + kill-XP/on_unit_death to the
		// attributed pending-death drain that applyUnitDamageWithSourceLocked
		// already enqueued — mirrors landProjectileLocked's
		// proj.SourceKind=="ability" carve-out (projectile.go): appending to
		// deadUnitIDs here would strip the unit via tickBeamsLocked's
		// immediate removeUnitLocked BEFORE drainPendingDeathsLocked runs
		// later this same tick, which is exactly the "already removed by the
		// primary call site" skip path — silently discarding XP/kill
		// bookkeeping AND making an authored ability's on_unit_death
		// unreachable for every beam-delivered kill. Non-ability proc beams
		// (equipment/item procs — b.SourceAbilityID == "") keep their legacy
		// immediate removal; they never routed through the drain's XP path
		// to begin with, so this is unchanged for them.
		if b.SourceAbilityID == "" {
			*deadUnitIDs = append(*deadUnitIDs, target.ID)
		}
	}
}

// removeBeamForUnitLocked drops any CHANNEL beam whose CasterUnitID == unitID.
// Called from stopUnitChannelLocked and clearUnitChannelLocked, and also
// from removeUnitLocked so a dying caster's beam doesn't linger. Momentary
// beams are skipped: they carry frozen endpoints and must complete their brief
// flash even if the caster is removed the same tick.
//
// Caller holds s.mu write lock.
func (s *GameState) removeBeamForUnitLocked(unitID int) {
	if len(s.Beams) == 0 {
		return
	}
	kept := s.Beams[:0]
	for _, b := range s.Beams {
		if !b.Momentary && b.CasterUnitID == unitID {
			continue // drop
		}
		kept = append(kept, b)
	}
	s.Beams = kept
}

// removeBeamByIDLocked drops the beam with the given stable wire ID. No-op
// when no beam matches (the caller may be cleaning up state that was already
// removed via a different path — e.g. removeBeamForTargetLocked firing on a
// dead chain victim before chain_siphon's per-tick sync runs).
//
// Caller holds s.mu write lock.
func (s *GameState) removeBeamByIDLocked(id string) {
	if len(s.Beams) == 0 || id == "" {
		return
	}
	kept := s.Beams[:0]
	for _, b := range s.Beams {
		if b.ID == id {
			continue // drop
		}
		kept = append(kept, b)
	}
	s.Beams = kept
}

// removeBeamForTargetLocked drops any CHANNEL beam whose TargetUnitID ==
// targetID. Called from removeUnitLocked so a beam whose target died is
// dropped immediately. The channel tick also catches this on the next tick,
// but removing the beam here keeps the visual state clean during the same tick
// the target dies. Momentary beams are skipped: a proc zap that KILLS its
// target must still flash, so it lives on its own timer regardless of the
// target's removal.
//
// Caller holds s.mu write lock.
func (s *GameState) removeBeamForTargetLocked(targetID int) {
	if len(s.Beams) == 0 {
		return
	}
	kept := s.Beams[:0]
	for _, b := range s.Beams {
		if !b.Momentary && b.TargetUnitID == targetID {
			continue // drop
		}
		kept = append(kept, b)
	}
	s.Beams = kept
}

// beamSnapshotsLocked builds the wire-format beam slice for a snapshot.
// When fow is nil (spectator / unfiltered Snapshot()), all beams are
// included. When fow is non-nil (SnapshotForPlayer), a beam is included only
// when the caster OR the target is visible to the viewer — matching the
// pattern projectiles use.
//
// Caller holds s.mu (read lock is sufficient).
func (s *GameState) beamSnapshotsLocked(fow *PlayerFOW) []protocol.BeamSnapshot {
	if len(s.Beams) == 0 {
		return nil
	}
	var out []protocol.BeamSnapshot
	for _, b := range s.Beams {
		if fow != nil {
			// FOW filter: include the beam when either endpoint is visible.
			// Momentary beams carry frozen coords (their participants may have
			// died) so they filter on those; channel beams resolve the live
			// caster/target positions.
			var visible bool
			if b.Momentary {
				visible = fow.isClearAtWorld(b.OriginX, b.OriginY, s.MapConfig.CellSize) ||
					fow.isClearAtWorld(b.TargetX, b.TargetY, s.MapConfig.CellSize)
			} else {
				if caster := s.getUnitByIDLocked(b.CasterUnitID); caster != nil {
					visible = fow.isClearAtWorld(caster.X, caster.Y, s.MapConfig.CellSize)
				}
				if !visible {
					if target := s.getUnitByIDLocked(b.TargetUnitID); target != nil {
						visible = fow.isClearAtWorld(target.X, target.Y, s.MapConfig.CellSize)
					}
				}
			}
			if !visible {
				continue
			}
		}
		snap := protocol.BeamSnapshot{
			ID:           b.ID,
			CasterUnitId: b.CasterUnitID,
			TargetUnitId: b.TargetUnitID,
			OwnerId:      b.OwnerPlayerID,
			AbilityId:    b.AbilityID,
			Variant:      b.Variant,
		}
		// Momentary beams send their frozen endpoints so the client renders the
		// flash from coords instead of live unit positions (which may be gone).
		if b.Momentary {
			snap.Momentary = true
			snap.OriginX = b.OriginX
			snap.OriginY = b.OriginY
			snap.TargetX = b.TargetX
			snap.TargetY = b.TargetY
		}
		out = append(out, snap)
	}
	return out
}
