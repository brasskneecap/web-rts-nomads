package game

import "webrts/server/pkg/protocol"

// ═════════════════════════════════════════════════════════════════════════════
// on_unit_death — composable ability trigger
//
// Semantics: "a unit killed BY this ability" — not "a unit that happened to
// die while this ability was active", not "any death anywhere". The killing
// DamageSource must carry SourceAbilityID (see that field's doc comment in
// damage_pipeline.go for exactly which damage paths set it, which propagate
// it through redirect/share, and which deliberately never do).
//
// Fired once per drained death from drainPendingDeathsLocked, as a peer to
// its existing per-death reactions (onSiphonVictimDeathLocked,
// awardUnitDeathXPLocked, onPerkKillLocked, trackBattleKillLocked, ...) — see
// that function's call site for why this must run BEFORE removeUnitLocked.
//
// PRODUCTION SAFETY: on_unit_death is authored-only, exactly like
// on_zone_enter/on_zone_exit. compileLegacyAbility never emits it, so no
// catalog ability's compiled program can reach this file's logic — see
// TestCatalog_NoAbilityUsesOnUnitDeathTrigger (ability_unit_death_test.go).
// ═════════════════════════════════════════════════════════════════════════════

// fireOnUnitDeathLocked fires victim's killing ability's on_unit_death
// trigger(s), if the ability that dealt the killing blow (src.SourceAbilityID)
// declares any. No-op when: src carries no ability id, the ability is
// unknown, or it has no compiled Program (every legacy, SchemaVersion<2
// ability — see the production-safety guard above).
//
// CasterID/OwnerUnitID are bound to src.AttackerUnitID (the unit that landed
// the killing blow, resolved fresh — never cached — by every downstream
// select_targets{source:"caster"} the same way every other executor entry
// point resolves it). CurrentEventUnitID binds victim's own ID so
// select_targets{source:"current_event"} resolves to the corpse — mirroring
// fireAbilityZoneOccupancyEventLocked's CurrentEventUnitID binding
// (ability_zone.go), the same seam (SrcCurrentEvent,
// ability_exec_targeting.go). A query targeting the corpse needs
// aliveState:"dead" (or "any") since the default alive-filter would otherwise
// exclude it — see applyTargetFiltersLocked's AliveState handling
// (ability_exec_targeting.go).
//
// abilityDef/program are set (mirroring fireScheduledMarkerLocked, not the
// leaner fireAbilityZoneTickLocked/fireAbilityZoneOccupancyEventLocked
// shape): on_unit_death is a top-level program trigger — like on_cast_start/
// on_cast_complete — not a zone-instance-scoped reaction, so its deal_damage
// actions scale with the caster's spell modifiers exactly like a fresh cast
// would, and its trigger_event actions can resolve the program's
// NamedTriggers.
//
// Each fire gets its own fresh RuntimeAbilityContext, so ctx.opsUsed starts
// at 0 every time — N deaths this tick means N independently-budgeted fires,
// never a shared/accumulated maxExecutionOps ceiling across them.
//
// Must be called under s.mu, and BEFORE removeUnitLocked(victim.ID) — see the
// call site's comment in drainPendingDeathsLocked (damage_pipeline.go).
func (s *GameState) fireOnUnitDeathLocked(victim *Unit, src DamageSource) {
	if victim == nil || src.SourceAbilityID == "" {
		return
	}
	def, ok := getAbilityDef(src.SourceAbilityID)
	if !ok || def.Program == nil {
		return
	}
	ctx := &RuntimeAbilityContext{
		CasterID:           src.AttackerUnitID,
		AbilityID:          def.ID,
		OwnerUnitID:        src.AttackerUnitID,
		EventPosition:      protocol.Vec2{X: victim.X, Y: victim.Y},
		CurrentEventUnitID: victim.ID,
		Named:              map[string]ContextValue{},
		Trace:              s.previewTrace,
		now:                s.previewClock,
		program:            def.Program,
		abilityDef:         &def,
	}
	s.runProgramTriggersLocked(ctx, def.Program.Triggers, TriggerOnUnitDeath)
}
