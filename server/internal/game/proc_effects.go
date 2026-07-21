package game

// ProcSource identifies who/where a proc effect fires from. IDs, never
// pointers (AI_RULES §Target References). Non-unit sources — traps,
// buildings, world events — leave OwnerUnitID 0: no kill credit or XP is
// attributed and the effect originates at OriginX/Y.
type ProcSource struct {
	OwnerUnitID   int
	OwnerPlayerID string
	OriginX       float64
	OriginY       float64
	// SourceAbilityID names the composable ability this proc fire is
	// delivering, if any — "" means "not from an ability" (equipment on-hit
	// procs, item procs, perk-triggered procs; see procSourceFromUnit, whose
	// zero value keeps every existing ProcSource{} call site behaving exactly
	// as before). Widened the same way DamageSource.SourceAbilityID was (see
	// its doc comment, damage_pipeline.go) so this compiles/behaves unchanged
	// everywhere except the one call site that sets it.
	//
	// Threaded through to the Beam this fires (fireProcBeamLocked ->
	// spawnMomentaryDamageBeamLocked -> Beam.SourceAbilityID) so
	// applyBeamPendingDamageLocked's DamageSource carries attribution all the
	// way to a chain-bounce kill — closing the gap noted at
	// DamageSource.SourceAbilityID's "KNOWN GAP" comment: chain_lightning's
	// bounce hops previously carried no ability attribution because they
	// route through this equipment-proc beam mechanism.
	//
	// Set at: fireAbilityChainLocked (ability_cast.go), the sole ability call
	// site of executeProcEffectLocked, from def.ID. Left empty at every
	// equipment/item/perk proc call site (procSourceFromUnit and both
	// state_combat.go on-hit call sites) — those have no ability id to carry.
	SourceAbilityID string
}

// procSourceFromUnit is the common-case constructor: the effect fires from
// the unit's current position with kill credit to that unit.
func procSourceFromUnit(u *Unit) ProcSource {
	return ProcSource{OwnerUnitID: u.ID, OwnerPlayerID: u.OwnerID, OriginX: u.X, OriginY: u.Y}
}

// executeProcEffectLocked fires one proc effect from src at target. Routes by
// the emitted effect's declared kind: a beam-kind def zaps the target
// instantly (damage deferred a beat so it pops as its own number), a
// projectile-kind def (the default, incl. unknown ids) fires a flying bolt
// that lands later. Contains NO RNG — whether an effect fires is the
// trigger's business (equipment rolls its chance against rngPerks; an ability
// or trap calls this directly). Caller holds s.mu write lock.
func (s *GameState) executeProcEffectLocked(src ProcSource, target *Unit, p ProcEffectParams) {
	if target == nil || p.Damage <= 0 {
		return
	}
	if def, ok := getProjectileDef(p.ProjectileID); ok && def.IsBeam() {
		s.fireProcBeamLocked(src, target, p, def)
	} else {
		s.fireProcProjectileLocked(src, target, p)
	}
}
