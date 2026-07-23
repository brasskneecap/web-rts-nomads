package game

import (
	"encoding/json"
	"strings"
	"testing"

	"webrts/server/pkg/protocol"
)

// ═════════════════════════════════════════════════════════════════════════════
// RunAbilityPreview (Phase 6a, Task 2)
//
// RunAbilityPreview is a deterministic, server-only harness that runs an
// ability's composable program end-to-end inside an isolated GameState: it
// spawns a caster + scene units, casts the ability once, steps Update with
// tracing on, and reports the resulting execution trace plus a compact
// per-unit HP-before/after summary. Used by the (Task 3) preview endpoint so
// the ability editor can show "what does this ability actually do" without
// touching a live match.
// ═════════════════════════════════════════════════════════════════════════════

// previewTraceHasType reports whether evs contains at least one event of
// type typ. Named distinctly from ability_exec_trace_test.go's traceHasType
// (which takes a *AbilityExecutionTrace, not a slice) to avoid a redeclare.
func previewTraceHasType(evs []AbilityExecutionTraceEvent, typ string) bool {
	for _, e := range evs {
		if e.Type == typ {
			return true
		}
	}
	return false
}

func TestRunAbilityPreview_GreaterHeal(t *testing.T) {
	def, ok := getAbilityDef("greater_heal")
	if !ok {
		t.Fatal(`getAbilityDef("greater_heal") = _, false`)
	}
	res, err := RunAbilityPreview(PreviewRequest{
		Ability: def, Seed: 1, CasterX: 0, CasterY: 0, Target: -1, DurationSeconds: 2,
		Units: []PreviewSceneUnit{
			{Team: "ally", X: 40, Y: 0, HP: 20, MaxHP: 100},
			{Team: "ally", X: 80, Y: 0, HP: 60, MaxHP: 100},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Error != "" {
		t.Fatalf("unexpected cast failure: %q", res.Error)
	}
	if !previewTraceHasType(res.Trace, "healing_applied") {
		t.Fatalf("no healing_applied event recorded: %+v", res.Trace)
	}
	if res.Units[0].HPAfter <= res.Units[0].HPBefore {
		t.Fatalf("ally0 not healed: %+v", res.Units[0])
	}
}

func TestRunAbilityPreview_ShatterDamages(t *testing.T) {
	// shatter is a point-target instant AoE + slow; an enemy near the cast
	// point must take damage.
	def, ok := getAbilityDef("shatter")
	if !ok {
		t.Fatal(`getAbilityDef("shatter") = _, false`)
	}
	res, err := RunAbilityPreview(PreviewRequest{
		Ability: def, Seed: 1, CasterX: 0, CasterY: 0, CastX: 50, CastY: 0, Target: -1, DurationSeconds: 1,
		Units: []PreviewSceneUnit{{Team: "enemy", X: 50, Y: 0, HP: 200, MaxHP: 200}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Error != "" {
		t.Fatalf("unexpected cast failure: %q", res.Error)
	}
	if !previewTraceHasType(res.Trace, "damage_applied") {
		t.Fatalf("no damage_applied event recorded: %+v", res.Trace)
	}
	for _, e := range res.Trace {
		if e.Type == "damage_applied" && e.Path == "" {
			t.Errorf("damage_applied event has empty Path (should carry the acting action's flow path): %+v", e)
		}
	}
	if res.Units[0].HPAfter >= res.Units[0].HPBefore {
		t.Fatal("enemy not damaged")
	}
}

// TestRunAbilityPreview_CapturesFrames proves RunAbilityPreview captures one
// PreviewFrame at t=0 (the initial scene) plus one after every Update tick
// (Phase 6a Task 3), and that a queued play_presentation effect (Phase 6b
// Task 1) actually reaches the captured wire snapshot.
func TestRunAbilityPreview_CapturesFrames(t *testing.T) {
	def, ok := getAbilityDef("shatter")
	if !ok {
		t.Fatal(`getAbilityDef("shatter") = _, false`)
	}
	const durationSeconds = 1.0
	res, err := RunAbilityPreview(PreviewRequest{
		Ability: def, Seed: 1, CasterX: 0, CasterY: 0, CastX: 50, CastY: 0, Target: -1, DurationSeconds: durationSeconds,
		Units: []PreviewSceneUnit{{Team: "enemy", X: 50, Y: 0, HP: 200, MaxHP: 200}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Error != "" {
		t.Fatalf("unexpected cast failure: %q", res.Error)
	}

	// Mirror the harness's own tick-count derivation (ability_preview.go's
	// "── Step ──" section) rather than hardcoding it, so a change to
	// previewTickDT/previewMaxTicks can't silently desync this test.
	wantTicks := int(durationSeconds / previewTickDT)
	if durationSeconds > 0 && wantTicks == 0 {
		wantTicks = 1
	}
	if wantTicks > previewMaxTicks {
		wantTicks = previewMaxTicks
	}
	if len(res.Frames) != wantTicks+1 {
		t.Fatalf("len(Frames) = %d; want %d (1 initial + %d ticks)", len(res.Frames), wantTicks+1, wantTicks)
	}
	if res.Frames[0].Tick != 0 {
		t.Errorf("Frames[0].Tick = %d; want 0", res.Frames[0].Tick)
	}
	if res.Frames[0].Time != 0 {
		t.Errorf("Frames[0].Time = %v; want 0", res.Frames[0].Time)
	}
	for i := 1; i < len(res.Frames); i++ {
		if res.Frames[i].Time < res.Frames[i-1].Time {
			t.Fatalf("Frames[%d].Time = %v < Frames[%d].Time = %v; want monotonic non-decreasing",
				i, res.Frames[i].Time, i-1, res.Frames[i-1].Time)
		}
	}

	foundShatterEffect := false
	for _, f := range res.Frames {
		for _, e := range f.Snapshot.Effects {
			if e.Name == "shatter" {
				foundShatterEffect = true
				break
			}
		}
		if foundShatterEffect {
			break
		}
	}
	if !foundShatterEffect {
		t.Errorf("no captured frame's Snapshot.Effects contains a %q effect", "shatter")
	}
}

// TestRunAbilityPreview_MeteorFramesShowDelayedImpact proves the
// on_animation_marker scheduler's delayed impact (Phase 6b Task 2) actually
// reaches the captured frame timeline: an early frame shows meteor's falling
// presentation queued at the cast point, and only a strictly later frame
// shows the enemy's HP having dropped below its scene-request starting
// value.
func TestRunAbilityPreview_MeteorFramesShowDelayedImpact(t *testing.T) {
	def, ok := getAbilityDef("meteor")
	if !ok {
		t.Fatal(`getAbilityDef("meteor") = _, false`)
	}
	// meteor is schemaVersion:2 as of the composable-abilities migration:
	// the RAW def's EffectAtPoint is cleared (Program is the sole authority),
	// so the expected effect name is recovered from the compiled Program via
	// abilityMechanicsShadow — but the PREVIEW itself must run the real
	// (unmodified) def, not the shadow, so it actually exercises the shipped
	// v2 behavior rather than a synthesized legacy shape.
	wantEffect := abilityMechanicsShadow(def).EffectAtPoint
	if wantEffect == "" {
		t.Fatal("recovered EffectAtPoint from the shipped Program is empty")
	}
	enemyStartHP := 500
	req := PreviewRequest{
		Ability: def, Seed: 1, CasterX: 0, CasterY: 0, CastX: 0, CastY: 0, Target: -1, DurationSeconds: 2,
		Units: []PreviewSceneUnit{{Team: "enemy", X: 0, Y: 0, HP: enemyStartHP, MaxHP: enemyStartHP}},
	}
	res, err := RunAbilityPreview(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Error != "" {
		t.Fatalf("unexpected cast failure: %q", res.Error)
	}

	effectFrame := -1
	for i, f := range res.Frames {
		for _, e := range f.Snapshot.Effects {
			if e.Name == wantEffect {
				effectFrame = i
				break
			}
		}
		if effectFrame >= 0 {
			break
		}
	}
	if effectFrame < 0 {
		t.Fatalf("no captured frame shows the %q fall effect: frames=%d", wantEffect, len(res.Frames))
	}

	damageFrame := -1
	for i, f := range res.Frames {
		for _, u := range f.Snapshot.Units {
			if u.OwnerID == previewEnemyOwner && u.HP < enemyStartHP {
				damageFrame = i
				break
			}
		}
		if damageFrame >= 0 {
			break
		}
	}
	if damageFrame < 0 {
		t.Fatalf("no captured frame shows the enemy's HP reduced below its starting %d", enemyStartHP)
	}
	if damageFrame <= effectFrame {
		t.Errorf("impact-damage frame (%d) should be strictly AFTER the fall-effect frame (%d) — the marker-scheduled impact must not land in the same tick the fall effect was queued", damageFrame, effectFrame)
	}
}

// TestRunAbilityPreview_MeteorLeavesBurningCraterVFX proves the composable
// create_zone action renders its lingering Presentation effect (meteor's
// "burning_crater"), matching the legacy GroundHazard path
// (spawnGroundHazardLocked -> playEffectAtPointForDurationLocked). Regression
// guard: create_zone previously spawned the damage zone but never played its
// crater VFX (a phase-6 TODO), so a previewed/converted meteor left no burning
// crater. The effect id + scale are read from the meteor catalog def, never
// hardcoded.
func TestRunAbilityPreview_MeteorLeavesBurningCraterVFX(t *testing.T) {
	def, ok := getAbilityDef("meteor")
	if !ok {
		t.Fatal(`getAbilityDef("meteor") = _, false`)
	}
	// meteor is schemaVersion:2 as of the composable-abilities migration: the
	// RAW def's BurnEffectAtPoint/EffectScale (checked below) are cleared and
	// recovered from the compiled Program instead — see abilityMechanicsShadow.
	// The PREVIEW itself still runs the real (unmodified) def below.
	recovered := abilityMechanicsShadow(def)
	if recovered.BurnEffectAtPoint == "" {
		t.Skip("meteor has no burnEffectAtPoint configured")
	}
	req := PreviewRequest{
		Ability: def, Seed: 1, CasterX: 0, CasterY: 0, CastX: 0, CastY: 0, Target: -1, DurationSeconds: 2,
		Units: []PreviewSceneUnit{{Team: "enemy", X: 0, Y: 0, HP: 500, MaxHP: 500}},
	}
	res, err := RunAbilityPreview(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Error != "" {
		t.Fatalf("unexpected cast failure: %q", res.Error)
	}

	var craterScale float64
	found := false
	for _, f := range res.Frames {
		for _, e := range f.Snapshot.Effects {
			if e.Name == recovered.BurnEffectAtPoint {
				found = true
				craterScale = e.SizeScale
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatalf("no captured frame shows the %q lingering crater VFX — create_zone must play its Presentation effect", recovered.BurnEffectAtPoint)
	}
	// The crater is sized by the compiled zone's PresentationScale, which
	// carries the ability's effectScale (parity with the legacy GroundHazard
	// crater) — recovered here since the raw v2 def's own EffectScale is cleared.
	if recovered.EffectScale > 0 && craterScale != recovered.EffectScale {
		t.Errorf("crater VFX sizeScale = %v; want the ability's effectScale %v", craterScale, recovered.EffectScale)
	}
}

func TestRunAbilityPreview_Deterministic(t *testing.T) {
	def, ok := getAbilityDef("greater_heal")
	if !ok {
		t.Fatal(`getAbilityDef("greater_heal") = _, false`)
	}
	req := PreviewRequest{
		Ability: def, Seed: 1, CasterX: 0, CasterY: 0, Target: -1, DurationSeconds: 2,
		Units: []PreviewSceneUnit{
			{Team: "ally", X: 40, Y: 0, HP: 20, MaxHP: 100},
			{Team: "ally", X: 80, Y: 0, HP: 60, MaxHP: 100},
		},
	}
	a, errA := RunAbilityPreview(req)
	if errA != nil {
		t.Fatal(errA)
	}
	b, errB := RunAbilityPreview(req)
	if errB != nil {
		t.Fatal(errB)
	}
	if len(a.Trace) != len(b.Trace) {
		t.Fatalf("trace length differs: %d vs %d", len(a.Trace), len(b.Trace))
	}
	for i := range a.Trace {
		if a.Trace[i].Type != b.Trace[i].Type || a.Trace[i].Time != b.Trace[i].Time {
			t.Fatalf("trace differs at %d: %+v vs %+v", i, a.Trace[i], b.Trace[i])
		}
	}
	if len(a.Units) != len(b.Units) {
		t.Fatalf("unit result count differs: %d vs %d", len(a.Units), len(b.Units))
	}
	if a.Units[0].HPAfter != b.Units[0].HPAfter {
		t.Fatal("unit result differs")
	}
}

// TestRunAbilityPreview_DeferredCustomActionSkipsHonestly retargets this
// "skips deferred" coverage a THIRD time: launch_vortex gained a registered
// Execute (arcane_orb's composable migration), joining launch_projectile
// (arcane_bolt/fireball/chain_lightning, Phase 6c) as fully executor-runnable
// — see TestCompileExecutorRunnableClassification. arcane_orb previously
// served this role before launch_vortex had an executor (see git history).
//
// No remaining CATALOG ability can exercise this contract:
//   - siphon_life (a channel ability, def.IsChannelAbility()) now migrated:
//     RunAbilityPreview's RequestAbilityCast -> beginAbilityCastLocked ->
//     beginAbilityChannelLocked DOES reach the executor (that function fires
//     the channel_beam action's trigger once its own gating passes — see
//     ability_channel.go's file doc comment). But channel_beam is a
//     REGISTERED action (Execute != nil, ability_exec_channel.go), so it
//     can't stand in for an unregistered/deferred one any more either.
//   - arcane_missiles (a charge-fire passive) is rejected outright by
//     beginAbilityCastLocked's IsPassive() guard (failCastLocked) — it can
//     never begin a cast, let alone reach the executor.
//
// So this test registers a SYNTHETIC point-targeted, zero-cast-time ability
// under a scratch id whose compiled program is a single bare ActionCustom
// action (no registered descriptor) — this is exactly the shape arcane_orb
// used to have, hand-authored directly instead of compiled from a legacy
// def, since there is no longer a real mechanic that compiles to
// ActionCustom AND reaches the executor. It still proves the thing this test
// exists to prove: an unregistered/deferred action type degrades honestly
// (Runnable=false, non-empty Warnings, action_skipped in the trace) rather
// than silently no-oping or panicking.
func TestRunAbilityPreview_DeferredCustomActionSkipsHonestly(t *testing.T) {
	def := AbilityDef{
		ID:            "deferred_custom_action_preview_fixture",
		DisplayName:   "Deferred Custom Action Fixture",
		Type:          AbilitySpell,
		TargetsPoint:  true,
		CastRange:     400,
		CastTime:      0,
		SchemaVersion: 2,
		Program: &AbilityProgram{
			Entry: AbilityEntryDef{Type: EntryGroundPoint, Range: 400},
			Triggers: []AbilityTriggerDef{{
				ID:   "cast",
				Type: TriggerOnCastComplete,
				Actions: []AbilityActionDef{
					{ID: "deferred", Type: ActionCustom},
				},
			}},
		},
	}
	res, err := RunAbilityPreview(PreviewRequest{
		Ability: def, Seed: 1, CasterX: 0, CasterY: 0, Target: -1, CastX: 100, CastY: 0, DurationSeconds: 1,
		Units: []PreviewSceneUnit{{Team: "enemy", X: 100, Y: 0, HP: 500, MaxHP: 500}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Runnable {
		t.Fatal("synthetic fixture should be non-runnable (bare, unregistered ActionCustom)")
	}
	if len(res.Warnings) == 0 {
		t.Fatal("expected non-empty degradation warnings")
	}
	if !previewTraceHasType(res.Trace, "action_skipped") {
		t.Fatalf("expected action_skipped in trace: %+v", res.Trace)
	}
}

// TestRunAbilityPreview_MeteorNowFullyRunnable locks in the Phase 6b Task 1
// consequence for meteor specifically: its compiled program uses only
// play_presentation, select_targets, deal_damage, and create_zone — every one
// of which now has a registered Execute — so it is fully executor-runnable
// with no degradation warnings. (The impact trigger itself is NOT invoked by
// this preview run — animation-marker scheduling is deferred to Task 2 — this
// only asserts the structural Runnable/Warnings classification.)
func TestRunAbilityPreview_MeteorNowFullyRunnable(t *testing.T) {
	def, ok := getAbilityDef("meteor")
	if !ok {
		t.Fatal(`getAbilityDef("meteor") = _, false`)
	}
	res, err := RunAbilityPreview(PreviewRequest{
		Ability: def, Seed: 1, CastX: 0, CastY: 0, Target: -1, DurationSeconds: 1,
		Units: []PreviewSceneUnit{{Team: "enemy", X: 0, Y: 0, HP: 500, MaxHP: 500}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Runnable {
		t.Fatal("meteor should be fully runnable now that play_presentation has a registered Execute")
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("expected no degradation warnings, got: %v", res.Warnings)
	}
	if previewTraceHasType(res.Trace, "action_skipped") {
		t.Fatalf("expected no action_skipped in trace: %+v", res.Trace)
	}
}

// countUnitsByType tallies the given wire snapshot's units by UnitType,
// keyed on the catalog type string (e.g. "adept", "raider", "soldier").
func countUnitsByType(units []protocol.UnitSnapshot) map[string]int {
	counts := make(map[string]int)
	for _, u := range units {
		counts[u.UnitType]++
	}
	return counts
}

// TestRunAbilityPreview_SceneUnitTypes_CasterAdeptEnemyRaiderAllySoldier
// proves the preview harness differentiates its spawned units by role
// (Task 1 of the ability-builder-ui-corrections plan): the caster spawns as
// the catalog "adept" type, enemy scene units spawn as "raider", and ally
// scene units keep the pre-existing "soldier" type — so the editor's
// preview canvas can visually distinguish caster/ally from enemy. Asserted
// via UnitType counts in the initial captured frame (never HP/damage
// numbers — those are catalog-derived and irrelevant here).
func TestRunAbilityPreview_SceneUnitTypes_CasterAdeptEnemyRaiderAllySoldier(t *testing.T) {
	def, ok := getAbilityDef("shatter")
	if !ok {
		t.Fatal(`getAbilityDef("shatter") = _, false`)
	}
	res, err := RunAbilityPreview(PreviewRequest{
		Ability: def, Seed: 1, CasterX: 0, CasterY: 0, CastX: 50, CastY: 0, Target: -1, DurationSeconds: 0,
		Units: []PreviewSceneUnit{
			{Team: "ally", X: 40, Y: 0, HP: 100, MaxHP: 100},
			{Team: "enemy", X: 50, Y: 0, HP: 200, MaxHP: 200},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Frames) == 0 {
		t.Fatal("expected at least the initial frame")
	}

	counts := countUnitsByType(res.Frames[0].Snapshot.Units)
	if counts[previewCasterUnitType] != 1 {
		t.Errorf("caster unit type %q count = %d; want 1 (units by type: %+v)", previewCasterUnitType, counts[previewCasterUnitType], counts)
	}
	if counts[previewEnemySceneUnitType] != 1 {
		t.Errorf("enemy scene unit type %q count = %d; want 1 (units by type: %+v)", previewEnemySceneUnitType, counts[previewEnemySceneUnitType], counts)
	}
	if counts[previewAllySceneUnitType] != 1 {
		t.Errorf("ally scene unit type %q count = %d; want 1 (units by type: %+v)", previewAllySceneUnitType, counts[previewAllySceneUnitType], counts)
	}
	if previewEnemySceneUnitType == previewAllySceneUnitType {
		t.Fatalf("previewEnemySceneUnitType and previewAllySceneUnitType must differ for the editor to visually distinguish them, both are %q", previewEnemySceneUnitType)
	}

	// Belt-and-suspenders: no unit in the scene carries the enemy scene
	// unit's type under the ally's owner.
	for _, u := range res.Frames[0].Snapshot.Units {
		if u.OwnerID == previewCasterOwner && u.UnitType == previewEnemySceneUnitType {
			t.Errorf("unit %+v owned by caster side has the enemy scene unit type %q", u, previewEnemySceneUnitType)
		}
	}
}

// previewOverlayKeys returns the runtimeAbilities overlay keys that look
// like a RunAbilityPreview registration (the "__ability_preview_" prefix
// nextPreviewAbilityID mints every id under), for asserting the overlay is
// left clean after a run without depending on any single call's exact id.
func previewOverlayKeys() []string {
	const prefix = "__ability_preview_"
	runtimeAbilitiesMu.RLock()
	defer runtimeAbilitiesMu.RUnlock()
	var keys []string
	for id := range runtimeAbilities {
		if strings.HasPrefix(id, prefix) {
			keys = append(keys, id)
		}
	}
	return keys
}

// TestRunAbilityPreview_OverlayCleanAfterRun proves the runtimeAbilities
// overlay carries no trace of a preview run once RunAbilityPreview returns
// (the deferred delete fired: no "__ability_preview_*" key survives), and
// that a catalog ability previewed under the SAME real id still resolves to
// the catalog def afterward (the preview registers under a per-call unique
// id -- see nextPreviewAbilityID -- so it never touches the real
// "greater_heal" overlay slot in the first place).
func TestRunAbilityPreview_OverlayCleanAfterRun(t *testing.T) {
	def, ok := getAbilityDef("greater_heal")
	if !ok {
		t.Fatal(`getAbilityDef("greater_heal") = _, false`)
	}
	if _, err := RunAbilityPreview(PreviewRequest{
		Ability: def, Seed: 1, CasterX: 0, CasterY: 0, Target: -1, DurationSeconds: 1,
		Units: []PreviewSceneUnit{{Team: "ally", X: 40, Y: 0, HP: 20, MaxHP: 100}},
	}); err != nil {
		t.Fatal(err)
	}

	if leftover := previewOverlayKeys(); len(leftover) != 0 {
		t.Fatalf("runtimeAbilities still has preview keys after RunAbilityPreview returned: %v", leftover)
	}

	after, ok := getAbilityDef("greater_heal")
	if !ok {
		t.Fatal(`getAbilityDef("greater_heal") = _, false after a preview run`)
	}
	// greater_heal is schemaVersion:2 in the live catalog as of the
	// composable-abilities migration, so the invariant this test guards is
	// "the catalog def is UNCHANGED by the preview run" — compared against
	// the SAME def captured before the preview, not hardcoded to "must be
	// legacy" (which was only ever true because no catalog ability had been
	// migrated yet when this test was written).
	if after.SchemaVersion != def.SchemaVersion || (after.Program == nil) != (def.Program == nil) {
		t.Fatalf("catalog greater_heal was mutated by preview: before schemaVersion=%d program-nil=%v, after schemaVersion=%d program-nil=%v",
			def.SchemaVersion, def.Program == nil, after.SchemaVersion, after.Program == nil)
	}
}

// TestRunAbilityPreview_ConcurrentCallsDoNotCrossContaminate proves the
// per-call unique registration id (nextPreviewAbilityID) actually isolates
// two overlapping RunAbilityPreview calls that register DIFFERENT programs:
// each call's caster must execute its OWN program, not whichever one
// happens to be registered under a shared key when its cast resolves. Runs
// a slow-resolving (non-zero cast time) heal concurrently with a
// zero-cast-time damage ability so their registration windows overlap, then
// asserts each result only shows its own ability's effect.
func TestRunAbilityPreview_ConcurrentCallsDoNotCrossContaminate(t *testing.T) {
	healDef, ok := getAbilityDef("greater_heal") // castTime 1.0s — registered for a while
	if !ok {
		t.Fatal(`getAbilityDef("greater_heal") = _, false`)
	}
	shatterDef, ok := getAbilityDef("shatter") // castTime 0 — resolves instantly
	if !ok {
		t.Fatal(`getAbilityDef("shatter") = _, false`)
	}

	healReq := PreviewRequest{
		Ability: healDef, Seed: 1, CasterX: 0, CasterY: 0, Target: -1, DurationSeconds: 2,
		Units: []PreviewSceneUnit{{Team: "ally", X: 40, Y: 0, HP: 20, MaxHP: 100}},
	}
	shatterReq := PreviewRequest{
		Ability: shatterDef, Seed: 2, CasterX: 0, CasterY: 0, CastX: 50, CastY: 0, Target: -1, DurationSeconds: 1,
		Units: []PreviewSceneUnit{{Team: "enemy", X: 50, Y: 0, HP: 200, MaxHP: 200}},
	}

	const rounds = 20
	healResults := make(chan PreviewResult, rounds)
	shatterResults := make(chan PreviewResult, rounds)
	errs := make(chan error, rounds*2)
	done := make(chan struct{})

	go func() {
		for i := 0; i < rounds; i++ {
			res, err := RunAbilityPreview(healReq)
			if err != nil {
				errs <- err
				continue
			}
			healResults <- res
		}
		done <- struct{}{}
	}()
	go func() {
		for i := 0; i < rounds; i++ {
			res, err := RunAbilityPreview(shatterReq)
			if err != nil {
				errs <- err
				continue
			}
			shatterResults <- res
		}
		done <- struct{}{}
	}()
	<-done
	<-done
	close(healResults)
	close(shatterResults)
	close(errs)

	for err := range errs {
		t.Fatalf("RunAbilityPreview error: %v", err)
	}

	for res := range healResults {
		if res.Error != "" {
			t.Fatalf("heal run failed: %q", res.Error)
		}
		if !previewTraceHasType(res.Trace, "healing_applied") {
			t.Errorf("heal run missing healing_applied (cross-contaminated by shatter?): %+v", res.Trace)
		}
		if previewTraceHasType(res.Trace, "damage_applied") {
			t.Errorf("heal run unexpectedly contains damage_applied (cross-contaminated by shatter): %+v", res.Trace)
		}
		if res.Units[0].HPAfter <= res.Units[0].HPBefore {
			t.Errorf("heal run: ally not healed: %+v", res.Units[0])
		}
	}
	for res := range shatterResults {
		if res.Error != "" {
			t.Fatalf("shatter run failed: %q", res.Error)
		}
		if !previewTraceHasType(res.Trace, "damage_applied") {
			t.Errorf("shatter run missing damage_applied (cross-contaminated by heal?): %+v", res.Trace)
		}
		if previewTraceHasType(res.Trace, "healing_applied") {
			t.Errorf("shatter run unexpectedly contains healing_applied (cross-contaminated by heal): %+v", res.Trace)
		}
		if res.Units[0].HPAfter >= res.Units[0].HPBefore {
			t.Errorf("shatter run: enemy not damaged: %+v", res.Units[0])
		}
	}

	if leftover := previewOverlayKeys(); len(leftover) != 0 {
		t.Fatalf("runtimeAbilities still has preview keys after all concurrent runs returned: %v", leftover)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Preview isolation: ONLY the ability under test may act.
//
// Regression: the harness used to APPEND the previewed ability to the caster's
// catalog loadout. previewCasterUnitType is "adept", which ships arcane_bolt
// (defaultAutoCast:true), so seedDefaultAutoCastLocked switched auto-cast on
// for it and the Update loop below fired it at scene units for the whole
// preview — with CurrentMana at 999,999, repeatedly. The author saw casts they
// never asked for.
//
// Note these assert EXACT values on purpose. The pre-existing preview tests
// only assert >/</presence, which is exactly why they all passed while this
// bug was live: the stray arcane_bolt damage was absorbed into their
// inequalities.
// ─────────────────────────────────────────────────────────────────────────────

func TestRunAbilityPreview_CasterCarriesOnlyAbilityUnderTest(t *testing.T) {
	def, ok := getAbilityDef("greater_heal")
	if !ok {
		t.Fatal(`getAbilityDef("greater_heal") = _, false`)
	}
	res, err := RunAbilityPreview(PreviewRequest{
		Ability: def, Seed: 1, CasterX: 0, CasterY: 0, Target: -1, DurationSeconds: 1,
		Units:   []PreviewSceneUnit{{Team: "ally", X: 40, Y: 0, HP: 20, MaxHP: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Frames) == 0 {
		t.Fatal("no frames captured")
	}
	// The caster is the only unit spawned at the request's caster position.
	var casterAbilities []protocol.AbilitySnapshot
	found := false
	for _, u := range res.Frames[0].Snapshot.Units {
		if u.X == 0 && u.Y == 0 {
			casterAbilities = u.Abilities
			found = true
			break
		}
	}
	if !found {
		t.Fatal("caster not present in frame 0")
	}
	if len(casterAbilities) != 1 {
		t.Fatalf("caster loadout = %v (%d abilities), want exactly 1 (the ability under test)",
			casterAbilities, len(casterAbilities))
	}
}

func TestRunAbilityPreview_NoUnrequestedAutoCast_HealPreviewDealsZeroDamage(t *testing.T) {
	def, ok := getAbilityDef("greater_heal")
	if !ok {
		t.Fatal(`getAbilityDef("greater_heal") = _, false`)
	}
	// Guard the test's own premise: greater_heal can never damage an enemy, so
	// ANY damage here is unambiguous cross-contamination from another ability.
	if def.CanTargetEnemies {
		t.Fatal("greater_heal unexpectedly canTargetEnemies; this test's premise is broken")
	}
	const enemyHP = 200
	// Enemy sits well inside arcane_bolt's 400 castRange. 4s clears the adept's
	// cast time + GCD several times over, so a leaked auto-cast would land.
	res, err := RunAbilityPreview(PreviewRequest{
		Ability: def, Seed: 1, CasterX: 0, CasterY: 0, Target: -1, DurationSeconds: 4,
		Units: []PreviewSceneUnit{
			{Team: "ally", X: 40, Y: 0, HP: 20, MaxHP: 100},
			{Team: "enemy", X: 150, Y: 0, HP: enemyHP, MaxHP: enemyHP},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if previewTraceHasType(res.Trace, "damage_applied") {
		t.Fatalf("a heal preview produced damage_applied events — another ability acted: %+v", res.Trace)
	}
	enemy := res.Units[1]
	if enemy.HPAfter != enemyHP {
		t.Fatalf("enemy HP %d -> %d during a HEAL preview; want unchanged %d (unrequested auto-cast leaked)",
			enemy.HPBefore, enemy.HPAfter, enemyHP)
	}
}

// arcane_missiles is a charge-fire PASSIVE: it has no castable action and only
// fires once the caster's Arcane Charge crosses its threshold. In the preview
// the caster spends no mana, so without a seeded charge the passive never fires.
// PreviewRequest.CasterCharge exists to make it testable — seed the charge at/
// above the threshold and the volley auto-fires at an in-range enemy.
func TestRunAbilityPreview_ChargeFirePassive_FiresWhenSeeded(t *testing.T) {
	def, ok := getAbilityDef("arcane_missiles")
	if !ok {
		t.Fatal(`getAbilityDef("arcane_missiles") = _, false`)
	}
	if !def.IsChargeFirePassive() {
		t.Fatal("arcane_missiles is not recognized as a charge-fire passive; test premise broken")
	}
	const enemyHP = 400
	scene := []PreviewSceneUnit{{Team: "enemy", X: 60, Y: 0, HP: enemyHP, MaxHP: enemyHP}}

	// Charge threshold is 30 (arcane_missiles.json). Seed exactly one volley's
	// worth. 3s is ample for the staggered bolts to travel to a close enemy.
	seeded, err := RunAbilityPreview(PreviewRequest{
		Ability: def, Seed: 1, CasterX: 0, CasterY: 0, Target: -1,
		CasterCharge: 30, DurationSeconds: 3, Units: scene,
	})
	if err != nil {
		t.Fatal(err)
	}
	if seeded.Error != "" {
		t.Fatalf("unexpected preview error for a passive: %q", seeded.Error)
	}
	if !previewTraceHasType(seeded.Trace, "charge_volley_queued") {
		t.Fatalf("seeded charge did not queue a volley: %+v", seeded.Trace)
	}
	if seeded.Units[0].HPAfter >= seeded.Units[0].HPBefore {
		t.Fatalf("enemy took no missile damage with seeded charge: %+v", seeded.Units[0])
	}

	// Control: with no seeded charge the passive stays dormant — no volley, no
	// damage — proving CasterCharge is what enables the test.
	dormant, err := RunAbilityPreview(PreviewRequest{
		Ability: def, Seed: 1, CasterX: 0, CasterY: 0, Target: -1,
		CasterCharge: 0, DurationSeconds: 3, Units: scene,
	})
	if err != nil {
		t.Fatal(err)
	}
	if previewTraceHasType(dormant.Trace, "charge_volley_queued") {
		t.Fatalf("volley fired with zero seeded charge: %+v", dormant.Trace)
	}
	if dormant.Units[0].HPAfter != enemyHP {
		t.Fatalf("enemy HP changed (%d -> %d) with no seeded charge; passive should be dormant",
			dormant.Units[0].HPBefore, dormant.Units[0].HPAfter)
	}
}

// TestRunAbilityPreview_CasterPerksDriveConditionals proves the editor can
// preview both sides of a perk-gated branch by GRANTING the perk, which is what
// replaced the old ConditionalOverrides map.
//
// The distinction matters. An override proved the THEN branch produced some
// effect; it could not tell you whether the CONDITION was authored correctly. A
// has_perk naming a perk that does not exist — or naming the wrong one —
// previewed identically to a correct one. Granting the perk runs the real
// evaluator, so a typo shows up as the branch simply not being taken.
func TestRunAbilityPreview_CasterPerksDriveConditionals(t *testing.T) {
	def, ok := getAbilityDef("fire_pit")
	if !ok {
		t.Fatal(`getAbilityDef("fire_pit") = _, false`)
	}
	// Read the gating perk out of the ability's own condition rather than
	// hardcoding it, so renaming either side fails this test loudly instead of
	// silently making it assert nothing.
	perkID := onlyConditionalPerkID(t, def)

	run := func(perks []string) PreviewResult {
		t.Helper()
		res, err := RunAbilityPreview(PreviewRequest{
			Ability: def, Seed: 1, CasterX: 0, CasterY: 0, CastX: 40, CastY: 0, Target: -1,
			DurationSeconds: 1,
			Units:           []PreviewSceneUnit{{Team: "enemy", X: 40, Y: 0, HP: 500, MaxHP: 500}},
			CasterPerks:     perks,
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.Error != "" {
			t.Fatalf("unexpected cast failure: %q", res.Error)
		}
		return res
	}

	t.Run("no perks takes ELSE", func(t *testing.T) {
		res := run(nil)
		if !previewTraceHasType(res.Trace, "condition_failed") {
			t.Error("expected the ELSE branch: a perkless caster fails has_perk")
		}
		if previewTraceHasType(res.Trace, "conditional_taken") {
			t.Error("THEN branch taken by a caster that owns no perks")
		}
	})

	t.Run("granting the gating perk takes THEN", func(t *testing.T) {
		res := run([]string{perkID})
		if !previewTraceHasType(res.Trace, "conditional_taken") {
			t.Fatalf("granting %q did not take the THEN branch", perkID)
		}
	})

	t.Run("granting an unrelated perk still takes ELSE", func(t *testing.T) {
		// The whole point: the branch responds to WHICH perk is owned, which an
		// override could never demonstrate.
		other := ""
		for _, d := range ListPerkDefs() {
			if d.ID != perkID {
				other = d.ID
				break
			}
		}
		if other == "" {
			t.Skip("catalog has only one perk")
		}
		res := run([]string{other})
		if previewTraceHasType(res.Trace, "conditional_taken") {
			t.Errorf("owning %q took a branch gated on %q", other, perkID)
		}
	})

	t.Run("an unknown perk id is ignored rather than failing the run", func(t *testing.T) {
		res := run([]string{"no_such_perk"})
		if res.Error != "" {
			t.Errorf("a stale perk id failed the run: %q", res.Error)
		}
	})
}

// TestRunAbilityPreview_ForcedBranchRendersItsVisual is the END-TO-END check
// behind "the burning effect never shows up in the preview". Two independent
// things had to be true for the editor to render it, and neither was:
//
//  1. The burn lives behind a has_perk branch, and the preview caster owned no
//     perks — so the THEN side was unreachable. Nothing the author did to the
//     visual could have mattered. The preview now grants perks instead.
//  2. The visual is only anchored to the afflicted unit when play_presentation
//     sets bindToStatusDuration; without it the same action renders one effect
//     at the CAST POINT instead of on the enemy.
//
// Asserting on captured FRAMES (not just the trace) is deliberate: frames are
// literally what the preview canvas draws, so this fails if the effect is
// produced but never reaches the wire.
func TestRunAbilityPreview_PerkGatedBranchRendersItsVisual(t *testing.T) {
	def, ok := getAbilityDef("fire_pit")
	if !ok {
		t.Fatal(`getAbilityDef("fire_pit") = _, false`)
	}
	res, err := RunAbilityPreview(PreviewRequest{
		Ability: def, Seed: 1, CasterX: 0, CasterY: 0, CastX: 40, CastY: 0, Target: -1,
		DurationSeconds: 2,
		Units:           []PreviewSceneUnit{{Team: "enemy", X: 40, Y: 0, HP: 500, MaxHP: 500}},
		CasterPerks:     []string{onlyConditionalPerkID(t, def)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Error != "" {
		t.Fatalf("unexpected cast failure: %q", res.Error)
	}

	// The enemy is the only scene unit, so anything anchored elsewhere (or
	// anchored to nothing — the at-point shape reports id 0) is a regression.
	var framesWithFlame, maxPerFrame int
	for _, f := range res.Frames {
		n := 0
		for _, e := range f.Snapshot.Effects {
			if e.Name != "burning" {
				continue
			}
			n++
			if e.AnchorUnitID == 0 {
				t.Fatalf("burn flame reached the preview unanchored (rendered at the cast point, not on the enemy): %+v", e)
			}
		}
		if n > 0 {
			framesWithFlame++
		}
		if n > maxPerFrame {
			maxPerFrame = n
		}
	}
	if framesWithFlame == 0 {
		t.Fatal("no burning effect in any preview frame — the forced branch's visual never reached the canvas")
	}
	// One flame, however many times the pit re-applies the refreshing burn.
	if maxPerFrame != 1 {
		t.Errorf("up to %d simultaneous burn flames in one frame, want 1", maxPerFrame)
	}
}

// onlyConditionalActionID finds the single `conditional` action in def's
// program and returns its authored id, failing if there isn't exactly one.
// Tests key overrides off this rather than a hardcoded string so that renaming
// (or adding a second) conditional in the catalog surfaces as a loud failure
// instead of a test that quietly stops exercising the override path.
// onlyConditionalPerkID returns the perk id the ability's single conditional
// gates on, read from the authored condition. Reading it (rather than
// hardcoding "lasting_flames") is what makes a rename fail loudly.
func onlyConditionalPerkID(t *testing.T, def AbilityDef) string {
	t.Helper()
	id := onlyConditionalActionID(t, def)
	var perkID string
	var walk func(any)
	raw, err := json.Marshal(def.Program)
	if err != nil {
		t.Fatalf("marshal program: %v", err)
	}
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("unmarshal program: %v", err)
	}
	walk = func(v any) {
		if perkID != "" {
			return
		}
		switch node := v.(type) {
		case map[string]any:
			if node["id"] == id {
				if cfg, ok := node["config"].(map[string]any); ok {
					if conds, ok := cfg["conditions"].([]any); ok {
						for _, c := range conds {
							cm, ok := c.(map[string]any)
							if !ok {
								continue
							}
							if op, _ := cm["op"].(string); op != condOpHasPerk && op != condOpNotPerk {
								continue
							}
							if right, ok := cm["right"].(string); ok {
								perkID = right
								return
							}
						}
					}
				}
			}
			for _, sub := range node {
				walk(sub)
			}
		case []any:
			for _, sub := range node {
				walk(sub)
			}
		}
	}
	walk(tree)
	if perkID == "" {
		t.Fatalf("ability %q conditional %q gates on no perk", def.ID, id)
	}
	return perkID
}

func onlyConditionalActionID(t *testing.T, def AbilityDef) string {
	t.Helper()
	if def.Program == nil {
		t.Fatalf("ability %q has no v2 program", def.ID)
	}
	var found []string
	var walkActions func(actions []AbilityActionDef)
	var walkTriggers func(triggers []AbilityTriggerDef)
	walkTriggers = func(triggers []AbilityTriggerDef) {
		for i := range triggers {
			walkActions(triggers[i].Actions)
		}
	}
	walkActions = func(actions []AbilityActionDef) {
		for i := range actions {
			a := &actions[i]
			if a.Type == ActionConditional {
				found = append(found, a.ID)
			}
			walkTriggers(a.Children)
			// Branch/body/container actions live in the opaque Config bag —
			// decode just enough to recurse without duplicating the executor's
			// typed shapes.
			var bag struct {
				Triggers []AbilityTriggerDef `json:"triggers"`
				Then     []AbilityActionDef  `json:"then"`
				Else     []AbilityActionDef  `json:"else"`
				Body     []AbilityActionDef  `json:"body"`
				Actions  []AbilityActionDef  `json:"actions"`
			}
			if len(a.Config) > 0 {
				_ = json.Unmarshal(a.Config, &bag)
			}
			walkTriggers(bag.Triggers)
			walkActions(bag.Then)
			walkActions(bag.Else)
			walkActions(bag.Body)
			walkActions(bag.Actions)
		}
	}
	walkTriggers(def.Program.Triggers)

	if len(found) != 1 {
		t.Fatalf("expected exactly one conditional in %q's program, found %d %v", def.ID, len(found), found)
	}
	return found[0]
}

// The previewer must show what a match shows. A previewed kill leaves a body,
// because a real kill does — otherwise an author watching the preview sees the
// target blink out of existence and builds around behaviour the game no longer
// has.
//
// Asserted on the FRAMES (the wire snapshots the editor replays), not on the
// GameState, because the frames are the only thing the client ever sees.
func TestRunAbilityPreview_AKillLeavesACorpseInTheFrames(t *testing.T) {
	def, ok := getAbilityDef("fire_pit")
	if !ok {
		t.Fatal(`getAbilityDef("fire_pit") = _, false`)
	}
	res, err := RunAbilityPreview(PreviewRequest{
		Ability: def, Seed: 1, CasterX: 0, CasterY: 0, CastX: 50, CastY: 0, Target: 0,
		DurationSeconds: 6,
		// 1 HP: the first tick of the pit kills it.
		Units: []PreviewSceneUnit{{Team: "enemy", X: 50, Y: 0, HP: 1, MaxHP: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Error != "" {
		t.Fatalf("unexpected cast failure: %q", res.Error)
	}

	sawCorpse := false
	for _, f := range res.Frames {
		if len(f.Snapshot.Corpses) > 0 {
			sawCorpse = true
			// The body must carry who it was — that is what the editor shows
			// when the author clicks it.
			if f.Snapshot.Corpses[0].UnitType == "" {
				t.Error("preview corpse has no unitType; the client cannot identify the body")
			}
			break
		}
	}
	if !sawCorpse {
		t.Error("no frame contained a corpse — the preview does not mirror what a match shows")
	}
}

// ═════════════════════════════════════════════════════════════════════════════
// AlliesAttack — previewing an ability whose effect lands on someone ELSE'S
// damage (PreviewRequest.AlliesAttack)
// ═════════════════════════════════════════════════════════════════════════════

// Off by default: the preview's baseline contract is that only the ability
// under test moves or deals damage.
func TestRunAbilityPreview_AlliesAreInertByDefault(t *testing.T) {
	def, ok := getAbilityDef("marker_trap")
	if !ok {
		t.Fatal(`getAbilityDef("marker_trap") = _, false`)
	}
	res, err := RunAbilityPreview(PreviewRequest{
		Ability: def, Seed: 1, CasterX: 600, CasterY: 500, Target: 0, DurationSeconds: 4,
		Units: []PreviewSceneUnit{
			{Team: "enemy", X: 720, Y: 500, HP: 500, MaxHP: 500},
			{Team: "ally", X: 520, Y: 500, HP: 100, MaxHP: 100},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Units[0].HPAfter != res.Units[0].HPBefore {
		t.Errorf("enemy HP changed (%d -> %d) with no attacker; marker_trap deals no damage of its own",
			res.Units[0].HPBefore, res.Units[0].HPAfter)
	}
}

// THE reason AlliesAttack exists. marker_trap's whole effect is that its victim
// takes more damage FROM OTHER SOURCES — it deals none itself. With every scene
// unit frozen and disarmed there was nothing in a preview that could show the
// mark working, so this asserts the thing an author actually wants to see: the
// same allied attackers, over the same run, take MORE health off a marked enemy
// than an unmarked one.
//
// The two runs are identical but for the trap, and the magnitude comes from the
// ability's own authored change_stat rather than a pinned number, so a rebalance
// moves the expectation with it.
func TestRunAbilityPreview_AlliesAttack_MarkAmplifiesTheirDamage(t *testing.T) {
	def, ok := getAbilityDef("marker_trap")
	if !ok {
		t.Fatal(`getAbilityDef("marker_trap") = _, false`)
	}
	// An ability that does nothing at all, so the control run has the same
	// caster, same allies, same order and same tick count — only no mark.
	inert := AbilityDef{
		ID: "preview_inert_control", DisplayName: "Inert", Type: def.Type,
		CanTargetEnemies: true, CastRange: def.CastRange, SchemaVersion: 2,
		Program: &AbilityProgram{
			Entry:    def.Program.Entry,
			Triggers: []AbilityTriggerDef{{ID: "cast", Type: TriggerOnCastComplete}},
		},
	}

	run := func(a AbilityDef) PreviewUnitResult {
		t.Helper()
		res, err := RunAbilityPreview(PreviewRequest{
			Ability: a, Seed: 1, CasterX: 600, CasterY: 500, Target: 0, DurationSeconds: 8,
			AlliesAttack: true,
			Units: []PreviewSceneUnit{
				{Team: "enemy", X: 720, Y: 500, HP: 2000, MaxHP: 2000},
				{Team: "ally", X: 660, Y: 500, HP: 200, MaxHP: 200},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.Error != "" {
			t.Fatalf("cast failed: %q", res.Error)
		}
		return res.Units[0]
	}

	control := run(inert)
	marked := run(def)

	controlDealt := control.HPBefore - control.HPAfter
	markedDealt := marked.HPBefore - marked.HPAfter
	if controlDealt <= 0 {
		t.Fatalf("the allies never attacked at all (enemy %d -> %d); AlliesAttack is not engaging them",
			control.HPBefore, control.HPAfter)
	}
	if markedDealt <= controlDealt {
		t.Errorf("marked enemy took %d damage, unmarked took %d — the mark changed nothing",
			markedDealt, controlDealt)
	}
}

// The mirror of the AlliesAttack test, and THE reason EnemiesAttack exists:
// exposed_weakness's whole effect is that a marked enemy deals LESS damage — a
// change to the enemy's own outgoing damage, invisible unless that enemy swings.
// EnemiesAttack sends it at an (inert) ally; the same marked enemy, over the same
// run, takes LESS health off the ally when its caster owns exposed_weakness than
// when it does not. Both runs cast the identical marker_trap (so the enemy is
// marked either way — Vulnerable is incoming and does not touch its OUTGOING
// damage); only the perk that adds the Weaken differs. Magnitude is asserted as
// an inequality, not a pinned number, so a rebalance carries the test.
func TestRunAbilityPreview_EnemiesAttack_ExposedWeaknessReducesTheirDamage(t *testing.T) {
	def, ok := getAbilityDef("marker_trap")
	if !ok {
		t.Fatal(`getAbilityDef("marker_trap") = _, false`)
	}

	run := func(casterPerks []string) PreviewUnitResult {
		t.Helper()
		// The enemy sits between the caster and the ally, but MUCH closer to the
		// ally, so the raider acquires the ally rather than the (team-mate of the
		// ally) caster soldier — the HP delta we measure is on the ally, index 1.
		// The caster stays within cast range (120 < 220) of the enemy, and the
		// marker zone (radius 115 at the enemy) still covers the ally at 730 so the
		// mark keeps refreshing through the fight.
		res, err := RunAbilityPreview(PreviewRequest{
			Ability: def, Seed: 1, CasterX: 600, CasterY: 500, Target: 0, DurationSeconds: 8,
			EnemiesAttack: true,
			CasterPerks:   casterPerks,
			Units: []PreviewSceneUnit{
				{Team: "enemy", X: 700, Y: 500, HP: 2000, MaxHP: 2000},
				{Team: "ally", X: 730, Y: 500, HP: 2000, MaxHP: 2000},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.Error != "" {
			t.Fatalf("cast failed: %q", res.Error)
		}
		return res.Units[1] // the ally the enemy is swinging at
	}

	control := run(nil)                           // enemy marked (Vulnerable) but NOT weakened
	weakened := run([]string{"exposed_weakness"}) // enemy also Weakened -> deals less

	controlDealt := control.HPBefore - control.HPAfter
	weakenedDealt := weakened.HPBefore - weakened.HPAfter
	if controlDealt <= 0 {
		t.Fatalf("the enemy never attacked the ally (ally %d -> %d); EnemiesAttack is not engaging them",
			control.HPBefore, control.HPAfter)
	}
	if weakenedDealt >= controlDealt {
		t.Errorf("weakened enemy dealt %d to the ally, un-weakened dealt %d — exposed_weakness changed nothing",
			weakenedDealt, controlDealt)
	}
}

// abilityTraceEvents drops the trace records the DAMAGE PIPELINE emits for the
// ability preview (recordPreviewDamageTraceLocked — every landed hit in the
// scene, whoever dealt it), leaving only what the executor itself recorded.
//
// The two are distinguishable by Path: an executor event always carries the
// flow path of the action that produced it, and the pipeline seam has no action
// to name. Tests that count "what did the ability do" want this; tests about
// the floating damage numbers want the whole trace.
func abilityTraceEvents(evs []AbilityExecutionTraceEvent) []AbilityExecutionTraceEvent {
	out := make([]AbilityExecutionTraceEvent, 0, len(evs))
	for _, e := range evs {
		if e.Type == "damage_applied" && e.Path == "" {
			continue
		}
		out = append(out, e)
	}
	return out
}

// The trace is what the editor's floating damage numbers AND its event log are
// built from, so an ally's swing has to reach it — otherwise the allies fight
// invisibly and the whole point of AlliesAttack is lost.
//
// It also has to report what LANDED, not what was swung: the same attacker
// hitting the same target must show a bigger number while the mark is on it.
// That is the observation an author is actually making, and reporting the
// pre-mitigation amount would show an identical number either way and look
// exactly like the mark doing nothing.
//
// Compared across two RUNS rather than across one run's own timeline: how long
// a mark survives is the ability's business (marker_trap refreshes it every
// tick while the victim stands in the zone), and a test that depends on it
// expiring breaks the next time that authoring changes.
func TestRunAbilityPreview_AlliesAttack_HitsReachTheTraceAndShowTheMark(t *testing.T) {
	def, ok := getAbilityDef("marker_trap")
	if !ok {
		t.Fatal(`getAbilityDef("marker_trap") = _, false`)
	}
	// An ability that does nothing, so the control run differs only by the mark.
	inert := AbilityDef{
		ID: "preview_inert_control", DisplayName: "Inert", Type: def.Type,
		CanTargetEnemies: true, CastRange: def.CastRange, SchemaVersion: 2,
		Program: &AbilityProgram{
			Entry:    def.Program.Entry,
			Triggers: []AbilityTriggerDef{{ID: "cast", Type: TriggerOnCastComplete}},
		},
	}

	hits := func(a AbilityDef) []int {
		t.Helper()
		res, err := RunAbilityPreview(PreviewRequest{
			Ability: a, Seed: 1, CasterX: 600, CasterY: 500, Target: 0, DurationSeconds: 8,
			AlliesAttack: true,
			Units: []PreviewSceneUnit{
				{Team: "enemy", X: 720, Y: 500, HP: 2000, MaxHP: 2000},
				{Team: "ally", X: 660, Y: 500, HP: 200, MaxHP: 200},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		var amounts []int
		for _, e := range res.Trace {
			// Pathless damage_applied = the pipeline seam, i.e. damage the
			// ability under test did not deal itself. See abilityTraceEvents.
			if e.Type != "damage_applied" || e.Path != "" {
				continue
			}
			if got, _ := e.Payload["category"].(string); got != string(DamageCategoryBasicAttack) {
				t.Errorf("unexpected damage category %q in the trace", got)
			}
			if e.Payload["attacker"] == nil {
				t.Error("a traced hit names no attacker; the log row cannot say who swung")
			}
			if n, ok := e.Payload["amount"].(int); ok {
				amounts = append(amounts, n)
			}
		}
		return amounts
	}

	plain := hits(inert)
	marked := hits(def)
	if len(plain) < 2 {
		t.Fatalf("only %d ally hits reached the trace; the editor would show no damage numbers", len(plain))
	}
	if len(marked) == 0 {
		t.Fatal("no ally hits reached the trace on the marked run")
	}
	if marked[0] <= plain[0] {
		t.Errorf("an ally hit landed for %d on a marked enemy vs %d on an unmarked one — either the mark is not "+
			"amplifying, or the trace is reporting pre-mitigation damage", marked[0], plain[0])
	}
}

// ═════════════════════════════════════════════════════════════════════════════
// Ability-targeted modifiers must work in the preview (the scratch-id alias)
// ═════════════════════════════════════════════════════════════════════════════

// THE bug this guards. RunAbilityPreview registers the ability under a unique
// scratch id so concurrent previews cannot collide — and that id is what
// reached the executor, so every perk row that names an ability BY ID
// ("target": "marker_trap") silently matched nothing. The whole ability-
// targeted half of perk authoring did nothing in the one place abilities are
// tested, which is how a working +35% mark duration came to be re-authored as
// a second +2 row by an author who had no way to see the first one land.
//
// Asserted against the SAME numbers a real cast produces (see
// TestPreviewAliases_MatchesALiveCast below) rather than against literals, so
// a rebalance of amplified_effects moves both together.
func TestRunAbilityPreview_AbilityTargetedPerkRowsApply(t *testing.T) {
	def, ok := getAbilityDef("marker_trap")
	if !ok {
		t.Fatal(`getAbilityDef("marker_trap") = _, false`)
	}
	run := func(perks []string) (zoneDuration, markDuration float64) {
		t.Helper()
		res, err := RunAbilityPreview(PreviewRequest{
			Ability: def, Seed: 1, CasterX: 600, CasterY: 500, Target: 0, DurationSeconds: 2,
			CasterUnitType: "archer", CasterPath: "trapper", CasterRank: "silver",
			CasterPerks:    perks,
			Units:          []PreviewSceneUnit{{Team: "enemy", X: 720, Y: 500, HP: 5000, MaxHP: 5000}},
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range res.Trace {
			switch e.Type {
			case "zone_created":
				if v, ok := e.Payload["duration"].(float64); ok && zoneDuration == 0 {
					zoneDuration = v
				}
			case "status_duration_applied":
				if v, ok := e.Payload["duration"].(float64); ok && markDuration == 0 {
					markDuration = v
				}
			}
		}
		return
	}

	bareZone, bareMark := run(nil)
	if bareZone == 0 || bareMark == 0 {
		t.Fatalf("preview produced no zone/mark at all (zone=%v mark=%v)", bareZone, bareMark)
	}

	_, perkMark := run([]string{"amplified_effects"})
	if perkMark <= bareMark {
		t.Errorf("mark duration with amplified_effects = %v, unmodified = %v — an ability-targeted perk row did not reach the preview",
			perkMark, bareMark)
	}
	// amplified_effects lengthens the MARK (its abilityFields row on
	// marker_trap.mark.duration), not the zone: it no longer carries a
	// zone-duration abilityStats row (that row double-counted the duration and
	// stretched the zone 12s->14s — removed). So the zone duration is
	// deliberately UNCHANGED by the perk; only the mark assertion above proves
	// an ability-targeted perk row reaches the preview.
}

// The preview is only worth trusting if it agrees with a real cast. Same perk,
// same ability, one run through RunAbilityPreview and one through
// RequestAbilityCast on an ordinary GameState — the durations must match.
func TestPreviewAliases_MatchesALiveCast(t *testing.T) {
	def, ok := getAbilityDef("marker_trap")
	if !ok {
		t.Fatal(`getAbilityDef("marker_trap") = _, false`)
	}

	// ── live cast ──
	s := newTrapSilverState(t)
	s.mu.Lock()
	caster := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	grantTrapAbility(caster, "marker_trap")
	caster.PerkIDs = []string{"amplified_effects"}
	caster.CurrentMana, caster.MaxMana = 9999, 9999
	victim := s.spawnPlayerUnitLocked("raider", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 450, Y: 400})
	victim.Visible = true
	victim.MaxHP, victim.HP = 5000, 5000
	casterID, victimID := caster.ID, victim.ID
	s.mu.Unlock()

	if ok, reason := s.RequestAbilityCast("p1", casterID, "marker_trap", victimID, 450, 400); !ok {
		t.Fatalf("live cast failed: %s", reason)
	}
	s.Update(0.05)

	s.mu.Lock()
	var liveMark float64
	for _, st := range s.AbilityStatuses {
		if st != nil && st.TargetUnitID == victimID {
			liveMark = st.Remaining
		}
	}
	s.mu.Unlock()
	if liveMark == 0 {
		t.Fatal("the live cast applied no mark; this test's setup is wrong, not the alias")
	}

	// ── preview ──
	res, err := RunAbilityPreview(PreviewRequest{
		Ability: def, Seed: 1, CasterX: 600, CasterY: 500, Target: 0, DurationSeconds: 1,
		CasterUnitType: "archer", CasterPath: "trapper", CasterRank: "silver",
		CasterPerks:    []string{"amplified_effects"},
		Units:          []PreviewSceneUnit{{Team: "enemy", X: 720, Y: 500, HP: 5000, MaxHP: 5000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var previewMark float64
	for _, e := range res.Trace {
		if e.Type == "status_duration_applied" {
			if v, ok := e.Payload["duration"].(float64); ok {
				previewMark = v
				break
			}
		}
	}

	// The live figure is one tick into its life; compare against the authored
	// duration the preview reports, allowing that single tick.
	if diff := previewMark - liveMark; diff < 0 || diff > 0.06 {
		t.Errorf("preview mark duration %v disagrees with the live cast's %v (one tick elapsed); the preview is not showing what a match does",
			previewMark, liveMark)
	}
}
