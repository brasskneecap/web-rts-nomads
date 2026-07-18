# Composable Abilities — Phase 6b: Visual Canvas Replay Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replay the ability-preview harness's per-tick server state into the REAL `CanvasRenderer` inside the ability editor, with playback (play/pause/scrub/speed), cast/AoE overlays, and playhead↔trace sync — plus a minimal `play_presentation` + animation-marker executor slice so the three anchor abilities (greater_heal, shatter, meteor) actually render their effects.

**Architecture:** The server harness (`RunAbilityPreview`) already steps an isolated, deterministic `GameState`. This phase (a) captures one unfiltered `MatchSnapshotMessage` per tick and returns the sequence, (b) implements `play_presentation` + an `on_animation_marker` scheduler so those snapshots actually contain effects/damage for the fixtures, and (c) adds a client canvas panel that feeds each captured frame into the real `CanvasRenderer` — reusing the proven pattern in `AbilityAnimationViewer.vue`.

**Tech Stack:** Go (server executor + harness), TypeScript/Vue 3 (client canvas + playback), existing `CanvasRenderer`/`Camera`/`GameState` client classes, existing `protocol.MatchSnapshotMessage` wire format.

---

## Locked Scope Decisions (do not re-litigate during execution)

1. **Snapshot-driven playback, NOT an injectable-clock rework.** Authoritative visuals (unit positions, HP, projectile/effect `progress`, status) travel inside each per-tick snapshot and are always correct. The renderer's wall-clock (`performance.now()`) is used ONLY for cosmetic sub-animation (walk-cycle phase). Consequence: a *paused* frame's cosmetic sprite phase may be slightly off; forward playback is faithful. The design doc's "N3 injectable clock" rework is explicitly OUT of scope for 6b.

2. **Minimal visual-executor slice = `play_presentation` + `on_animation_marker` scheduler ONLY.** These light up all three anchor abilities. `launch_projectile` (arcane_bolt / chain / charge / orb-vortex) stays deferred to the later executor pass — no fixture needs it and its impact-re-entry is the risky part.

3. **Reuse, don't reinvent.** Server: `s.Snapshot()` / `snapshotUnfilteredLocked()` for capture; `playEffectAtPointLocked` / `playEffectOnUnitLocked` / `queueEffectLocked` for presentation. Client: the `AbilityAnimationViewer.vue` "standalone GameState → CanvasRenderer" pattern.

4. **Determinism preserved.** No wall-clock, no unseeded RNG in any server code added here. The marker scheduler keys off an accumulating sim clock advanced by `dt`, never `time.Now()`.

5. **Production must stay byte-identical until an ability is authored v2.** No catalog ability is v2, and the marker scheduler / `play_presentation` executor only run inside a program execution (`SchemaVersion>=2 && Program!=nil`). `go test ./...` must be green after every task.

---

## Carry-Forward Context (state at start of 6b)

- Executor entry: `resolveAbilityProgramCastLocked` ([ability_cast.go:317](../../server/internal/game/ability_cast.go#L317)) builds `RuntimeAbilityContext{CasterID, abilityDef, program, CastPoint, ImpactPosition, Trace: s.previewTrace, now: s.previewClock}` and calls `runProgramTriggersLocked(ctx, prog.Triggers, TriggerOnCastComplete)`.
- Action registry: `registerAction(ActionDescriptor{Type, Decode, Validate, Schema, Execute})`. `play_presentation` currently has NO descriptor → traced `action_skipped`, and `AbilityProgramRunnable` returns false / `degradationWarnings` lists it.
- `RuntimeAbilityContext` (persist IDs/positions, never `*Unit`): `CasterID int`, `InitialTarget int`, `CastPoint/ImpactPosition/ZoneCenter protocol.Vec2`, `OwnerUnitID int`, `Named map[string]ContextValue`, `now float64`, `program *AbilityProgram`, `abilityDef *AbilityDef`, `opsUsed int`, `currentActionPath string`.
- Presentation model: `PresentationInstanceDef{ID, Asset, PositionRef ContextRef, AttachRef *ContextRef, Scale, RenderLayer, Animation, Triggers []AbilityTriggerDef}` stored on `AbilityProgram.Presentations`. Marker trigger shape: `AbilityTriggerDef{Type: TriggerOnAnimationMarker, Timing: &TriggerTiming{Marker, DelaySeconds}}`.
- Meteor compile ([ability_compile.go:367](../../server/internal/game/ability_compile.go#L367)): `on_cast_complete` runs one `play_presentation{PresentationID:"p_meteor"}`; the damage+zone live in `Presentations["p_meteor"].Triggers[impact]` (`TriggerOnAnimationMarker`, `Marker:"impact"`) — currently `DelaySeconds` is NOT set, so the scheduler has no time to fire at. Task 4 fixes this.
- Effect seams: `playEffectAtPointLocked(id, x, y, scale)` and `playEffectOnUnitLocked(unit, id)` → `queueEffectLocked` → `s.activeEffects` → serialized into `MatchSnapshotMessage.Effects` (`EffectSnapshot{ID, Name, AnchorUnitID, X, Y, Progress, SizeScale, Variant, Anchor}`).
- Snapshot capture point: `s.snapshotUnfilteredLocked() protocol.MatchSnapshotMessage` ([state.go:2520](../../server/internal/game/state.go#L2520)) — unfiltered (no FOW), the exact format the client already renders.
- Harness: `RunAbilityPreview(req PreviewRequest) (PreviewResult, error)` ([ability_preview.go:159](../../server/internal/game/ability_preview.go#L159)) steps `s.Update(previewTickDT)` for N ticks. `PreviewResult{Trace, Units, CasterManaSpent, Runnable, Warnings, Error}`.
- Client precedent: `AbilityAnimationViewer.vue` builds a `new GameState()`, `new Camera()`, `new CanvasRenderer(canvas, state, camera)`, mutates `state.units/projectiles/effects/beams`, calls `renderer.render()` in a RAF loop. Client `GameState` shape and the snapshot-apply path live in `client/src/game-portal/src/game/core/GameState.ts` + `network/protocol.ts`.
- Existing 6a preview UI: `AbilityPreviewPanel.vue`, `PreviewTimeline.vue`, `PreviewEventLog.vue`, `PreviewSceneControls.vue`, `refFromPath.ts`, `traceEventDisplay.ts`, `programPreview.ts` (`runAbilityPreview` API in `abilityEditorApi.ts`). Flow⇄Preview toggle in `AbilityBuilderPanel.vue` (`mainView`).

---

## File Structure

**Server (Go):**
- Modify `server/internal/game/ability_exec_presentation.go` (CREATE) — `play_presentation` ActionDescriptor.
- Create `server/internal/game/ability_marker.go` — marker scheduler (`scheduledMarker`, `scheduleMarkerTriggersLocked`, `tickAbilityMarkersLocked`).
- Modify `server/internal/game/state.go` — add `simTime float64`, `pendingMarkers []scheduledMarker`; advance `simTime` and call `tickAbilityMarkersLocked` in `Update`.
- Modify `server/internal/game/ability_compile.go` — thread `ImpactDelaySeconds` into the meteor impact trigger's `Timing.DelaySeconds`.
- Modify `server/internal/game/ability_program_runnable.go` — `play_presentation` is now runnable; drop from `degradationWarnings`.
- Modify `server/internal/game/ability_preview.go` — capture per-tick frames into `PreviewResult.Frames`.
- Modify `server/pkg/protocol/messages.go` — (only if a trimmed frame type is chosen; default: reuse `MatchSnapshotMessage`).

**Client (TypeScript/Vue):**
- Modify `client/.../abilities/program/programPreview.ts` — `PreviewFrame` type + parse.
- Create `client/.../components/ability-builder/AbilityPreviewCanvas.vue` — canvas replay (GameState+Camera+CanvasRenderer).
- Create `client/.../components/ability-builder/previewPlayback.ts` — pure playback-clock helper (frame index from elapsed + speed; seek).
- Create `client/.../components/ability-builder/PreviewOverlays.ts` — cast-range / AoE-radius overlay draw helpers.
- Modify `client/.../components/ability-builder/AbilityPreviewPanel.vue` — mount the canvas above the timeline/log; wire playhead↔event-log.

---

## Task 1: `play_presentation` executor (server)

**Files:**
- Create: `server/internal/game/ability_exec_presentation.go`
- Test: `server/internal/game/ability_exec_presentation_test.go`
- Modify: `server/internal/game/ability_program_runnable.go`

**Approach:** Register an `ActionDescriptor` for `ActionPlayPresentation`. Config is the union of the two compiled shapes already in `ability_compile.go`: at-point (`playPresentationAtPointConfig{Asset, Position ContextRef, Scale, RenderLayer, PresentationID}`) and on-target (`playPresentationOnTargetConfig{Asset, OncePerTarget}` with `Input["attach"]` → a unit-set context key). Decode both by trying the fuller struct; the presence of `Input["attach"]` selects the on-target path at Execute time.

Execute logic (caller holds `s.mu`):
1. If `Input["attach"]` resolves to a unit set → for each unit id, `getUnitByIDLocked` (nil-guard), `playEffectOnUnitLocked(u, asset)`. Respect `OncePerTarget` (it always is here — one effect per unit).
2. Else resolve a world position from `Config.Position` ContextRef (`castPoint`→`ctx.CastPoint`, `impactPosition`→`ctx.ImpactPosition`, `zoneCenter`→`ctx.ZoneCenter`; else `ctx.CastPoint`) and call `playEffectAtPointLocked(asset, x, y, scale)`.
3. If `Config.PresentationID != ""` AND `ctx.program.Presentations[PresentationID]` exists with marker triggers → call `s.scheduleMarkerTriggersLocked(ctx, presentation)` (Task 2). This is how `play_presentation` kicks off meteor's delayed impact.
4. Emit a trace event `presentation_played` at `ctx.currentActionPath` with payload `{asset, presentationId}`.
5. Empty asset or unregistered effect → no-op (matches `playEffectAtPointLocked` fail-safe), still schedule markers if a PresentationID is present (meteor's impact must fire even if the fall VFX asset is missing — parity with legacy `playEffectAtPointLocked` playing "regardless of hits").

**Validate:** empty `Asset` AND empty `PresentationID` → one `warning` issue ("play_presentation has neither asset nor presentation"); never an error (a marker-only presentation kick is legal).

**Schema:** fields `asset` (asset), `position` (context-ref), `scale` (number), `renderLayer` (enum, optional), `presentationId` (text, optional). `Runnable: true`.

- [ ] **Step 1 (test-first):** Write `TestPlayPresentation_AtPoint_QueuesEffect`: build a ctx with `CastPoint={100,200}`, run a `play_presentation{Asset:"shatter", Position:{Key:"castPoint"}, Scale:2}` action via `executeActionLocked`; assert `len(s.activeEffects)==1` with name "shatter", X/Y 100/200, sizeScale 2. Use an existing registered effect id (`shatter`) so `getEffectDef` succeeds.
- [ ] **Step 2:** Write `TestPlayPresentation_OnTarget_QueuesPerUnit`: bind a `Named["healTargets"]` unit set of 2 units, run `play_presentation` with `Input["attach"]={Key:"healTargets"}` and `Asset:"<a registered on-unit effect>"`; assert one effect queued per unit anchored to each unit id.
- [ ] **Step 3:** Run both → FAIL (no descriptor).
- [ ] **Step 4:** Implement the descriptor in `ability_exec_presentation.go`; register it in an `init()` alongside the other actions.
- [ ] **Step 5:** Run → PASS.
- [ ] **Step 6:** Update `ability_program_runnable.go`: remove `ActionPlayPresentation` from the deferred/skipped set so `AbilityProgramRunnable(compileLegacyAbility(shatter))` is now true and `degradationWarnings` no longer lists play_presentation. Update the existing runnable/degradation tests to match (shatter/greater_heal become runnable; a projectile ability stays non-runnable via `launch_projectile`).
- [ ] **Step 7:** `go test ./server/...` green. Commit.

**Spec-review focus:** position ContextRef resolution matches the keys the compiler emits (`castPoint`, `impactPosition`); `OncePerTarget` honored; nil-unit guard on the attach path; marker-kick still fires when the asset is empty/unregistered.

---

## Task 2: `on_animation_marker` scheduler (server)

**Files:**
- Create: `server/internal/game/ability_marker.go`
- Test: `server/internal/game/ability_marker_test.go`
- Modify: `server/internal/game/state.go` (fields + Update wiring)

**Approach:** A deterministic, sim-time-driven scheduler. When `play_presentation` (Task 1) kicks off a presentation that has `TriggerOnAnimationMarker` triggers, enqueue each such trigger to fire once at `s.simTime + delay`, where `delay = Timing.DelaySeconds` (Task 4 makes the meteor compiler set this from `impactDelaySeconds`). A marker with no delay fires on the next tick (delay 0).

Persist only ID-based/immutable data across ticks (AI_RULES.md): a `scheduledMarker` stores `fireAtSimTime float64`, the reconstructable context primitives (`casterID int`, `abilityID string`, `impactPos/castPoint/zoneCenter protocol.Vec2`, `ownerUnitID int`, `initialTarget int`), and the trigger's actions (`[]AbilityActionDef` — immutable def data, same as zones store their trigger defs). At fire time it re-resolves the program via `getAbilityDef(abilityID)` and rebuilds a fresh `RuntimeAbilityContext` (never a stored `*Unit`).

```go
// ability_marker.go
type scheduledMarker struct {
    fireAtSimTime float64
    casterID      int
    abilityID     string
    marker        string
    castPoint     protocol.Vec2
    impactPos     protocol.Vec2
    zoneCenter    protocol.Vec2
    ownerUnitID   int
    initialTarget int
    actions       []AbilityActionDef
}

// scheduleMarkerTriggersLocked enqueues pres's on_animation_marker triggers
// to fire at ctx.now + Timing.DelaySeconds. Caller holds s.mu. Called from
// play_presentation's Execute. Uses ctx.now (== s.simTime at the enqueuing
// tick) as the base so preview and production agree.
func (s *GameState) scheduleMarkerTriggersLocked(ctx *RuntimeAbilityContext, pres PresentationInstanceDef) {
    for _, trg := range pres.Triggers {
        if trg.Type != TriggerOnAnimationMarker { continue }
        delay := 0.0
        if trg.Timing != nil { delay = trg.Timing.DelaySeconds }
        s.pendingMarkers = append(s.pendingMarkers, scheduledMarker{
            fireAtSimTime: s.simTime + delay,
            casterID:      ctx.CasterID,
            abilityID:     ctx.AbilityID,
            marker:        markerName(trg.Timing),
            castPoint:     ctx.CastPoint,
            impactPos:     ctx.ImpactPosition,
            zoneCenter:    ctx.ZoneCenter,
            ownerUnitID:   ctx.OwnerUnitID,
            initialTarget: ctx.InitialTarget,
            actions:       trg.Actions,
        })
    }
}

// tickAbilityMarkersLocked fires every pending marker whose time has arrived,
// in scheduled order, rebuilding a fresh context each. Caller holds s.mu.
// Determinism: iterates a slice (ordered), keyed off s.simTime (dt-driven),
// never wall-clock. Trace/now come from s.previewTrace/previewClock so preview
// runs capture marker-fired events; production leaves them nil/0.
func (s *GameState) tickAbilityMarkersLocked() {
    if len(s.pendingMarkers) == 0 { return }
    remaining := s.pendingMarkers[:0]
    for _, m := range s.pendingMarkers {
        if m.fireAtSimTime > s.simTime { remaining = append(remaining, m); continue }
        def, ok := getAbilityDef(m.abilityID)
        if !ok || def.Program == nil { continue } // ability gone; drop
        ctx := &RuntimeAbilityContext{
            CasterID: m.casterID, AbilityID: m.abilityID, program: def.Program,
            abilityDef: &def, Named: map[string]ContextValue{},
            CastPoint: m.castPoint, ImpactPosition: m.impactPos, ZoneCenter: m.zoneCenter,
            OwnerUnitID: m.ownerUnitID, InitialTarget: m.initialTarget,
            Trace: s.previewTrace, now: s.previewClock,
        }
        for i := range m.actions {
            if ctx.opsUsed >= maxExecutionOps { break }
            s.executeActionLocked(ctx, &m.actions[i], "marker["+m.marker+"]")
        }
    }
    s.pendingMarkers = remaining
}
```

`markerName` is a tiny helper returning `Timing.Marker` (or `""`).

State wiring in `state.go`:
- Add fields: `simTime float64`, `pendingMarkers []scheduledMarker`.
- In `Update(dt)`, after the existing `tickAbilityZonesLocked(dt)` call and inside the same locked region: `s.simTime += dt` FIRST, then `s.tickAbilityMarkersLocked()`. (simTime advances every tick in production too — it is cheap and must be monotonic for the scheduler; it is only *read* by the scheduler, which no-ops when `pendingMarkers` is empty, i.e. always in production today.)

**Determinism note for reviewers:** `simTime` is a pure `dt` accumulator; identical seed+dt sequence ⇒ identical fire ordering. The scheduler must NOT sort by anything nondeterministic and must NOT read `s.previewClock` for timing (only for trace stamping).

- [ ] **Step 1 (test-first):** `TestMarkerScheduler_FiresAfterDelay`: construct a program whose `Presentations["p"]` has an `impact` marker trigger (`Timing.DelaySeconds=0.3`) with a single `deal_damage` on an enemy in `Named`; drive it by enqueuing via `scheduleMarkerTriggersLocked` then stepping `Update(0.05)` and asserting damage lands on the tick where `simTime>=0.3`, not before.
- [ ] **Step 2:** `TestMarkerScheduler_FiresOnce`: after firing, further `Update`s do not re-fire (pendingMarkers drained).
- [ ] **Step 3:** `TestMarkerScheduler_DropsWhenAbilityGone`: schedule against an abilityID not in the registry → tick drops it, no panic.
- [ ] **Step 4:** Run → FAIL.
- [ ] **Step 5:** Implement `ability_marker.go` + `state.go` wiring.
- [ ] **Step 6:** Run → PASS; full `go test ./server/...` green (existing determinism/snapshot tests unaffected — `pendingMarkers` empty in every existing test since no ability is v2). Commit.

**Spec-review focus:** no `*Unit`/`*AbilityProgram`-from-another-owner stored across ticks (program re-resolved by id at fire time ✓); op-budget respected; `simTime` advanced exactly once per Update; production no-op proven (test that a legacy cast never populates `pendingMarkers`).

---

## Task 3: Per-tick snapshot capture in the harness (server)

**Files:**
- Modify: `server/internal/game/ability_preview.go`
- Test: `server/internal/game/ability_preview_test.go`

**Approach:** Add `Frames []PreviewFrame` to `PreviewResult`. A `PreviewFrame` wraps the tick index, sim time, and the unfiltered snapshot:

```go
type PreviewFrame struct {
    Tick     int                          `json:"tick"`
    Time     float64                      `json:"t"`
    Snapshot protocol.MatchSnapshotMessage `json:"snapshot"`
}
```

In `RunAbilityPreview`, capture a frame at t=0 (after setup, before the first Update — the initial scene) and after each `s.Update(previewTickDT)`:
```go
capture := func() {
    s.mu.Lock()
    snap := s.snapshotUnfilteredLocked()
    s.mu.Unlock()
    res.Frames = append(res.Frames, PreviewFrame{Tick: len(res.Frames), Time: s.previewClock, Snapshot: snap})
}
```
Call `capture()` once before the loop and once at the end of each loop iteration. Frame count is bounded by `previewMaxTicks+1` (already capped). `snapshotUnfilteredLocked` acquires no lock itself (it's `...Locked`) — wrap in `s.mu.Lock()` as shown (RRLock is fine too; match the harness's existing lock usage which uses `s.mu.Lock()`).

Keep the existing `Trace`/`Units`/`CasterManaSpent` collection unchanged.

- [ ] **Step 1 (test-first):** `TestRunAbilityPreview_CapturesFrames`: preview shatter for `DurationSeconds=1.0`; assert `len(res.Frames) == ticks+1`, `res.Frames[0].Tick==0`, monotonic `Time`, and that at least one late frame's `Snapshot.Effects` contains the "shatter" effect (proves Task 1 output reaches the snapshot).
- [ ] **Step 2:** `TestRunAbilityPreview_MeteorFramesShowDelayedImpact`: preview meteor; assert an early frame has the fall effect and a later frame (after impactDelay) shows the enemy HP dropped in `Snapshot.Units` (proves Task 2 marker firing reaches the snapshot).
- [ ] **Step 3:** Run → FAIL.
- [ ] **Step 4:** Implement capture.
- [ ] **Step 5:** Run → PASS. Verify the `/abilities/preview` endpoint response now includes `frames` (no handler change needed — it marshals `PreviewResult` whole; confirm the 512KB response cap in `editor_handlers.go` is large enough for ~400 small-scene frames, bump if a test shows truncation). Commit.

**Spec-review focus:** frame count bounded; capture under the lock; no per-tick log spam (this is a return value, not a log — complies with diagnostics rules); determinism (same seed ⇒ same frames).

---

## Task 4: Thread `impactDelaySeconds` into the compiled meteor marker (server)

**Files:**
- Modify: `server/internal/game/ability_compile.go` (`compileMeteorActions`)
- Test: `server/internal/game/ability_compile_test.go`

**Approach:** In `compileMeteorActions`, set the impact trigger's timing delay from the legacy field so the Task 2 scheduler has a concrete fire time:
```go
impactTrigger := AbilityTriggerDef{
    ID: "impact", Type: TriggerOnAnimationMarker,
    Timing:  &TriggerTiming{Marker: "impact", DelaySeconds: def.ImpactDelaySeconds},
    Actions: impactActions,
}
```
This is the ONLY change; the presentation/zone structure is unchanged.

- [ ] **Step 1 (test-first):** extend the meteor compile test to assert `Presentations[0].Triggers[0].Timing.DelaySeconds == def.ImpactDelaySeconds` (read the expected from the meteor catalog JSON, NOT a hardcoded number — per the no-hardcoded-tunables rule).
- [ ] **Step 2:** Run → FAIL. Implement. Run → PASS.
- [ ] **Step 3:** Re-run the golden-equivalence tests (`ability_compile_golden_test.go`) — they compare gameplay outcomes, and with Task 2 live the compiled meteor now deals its impact damage via the scheduler. If a golden test previously tolerated meteor's impact being inert, update it to assert the impact NOW lands (this is the intended behavior change, gated to v2/preview only). Commit.

**Spec-review focus:** delay sourced from the def, not hardcoded; golden tests reflect that meteor impact now fires.

---

## Task 5: Client preview-frame types (TypeScript)

**Files:**
- Modify: `client/.../abilities/program/programPreview.ts`
- Test: `client/.../abilities/program/programPreview.test.ts`

**Approach:** Mirror `PreviewFrame` and extend the parsed `PreviewResult`. The `Snapshot` shape is the existing client match-snapshot type (reuse whatever `network/protocol.ts` exports for an applied snapshot; if the client models snapshots structurally rather than by a named type, type `snapshot` as that structural type — do NOT invent a parallel type). Add `frames: PreviewFrame[]` to the parse, defaulting to `[]` for older responses (back-compat with 6a).

- [ ] **Step 1 (test-first):** parse a fixture response with `frames:[{tick:0,t:0,snapshot:{units:[...],effects:[...]}}]`; assert `result.frames.length===1` and fields survive.
- [ ] **Step 2:** Run → FAIL. Implement parse. Run → PASS. `vue-tsc -b` clean. Commit.

---

## Task 6: `AbilityPreviewCanvas.vue` — the canvas replay (Vue)

**Files:**
- Create: `client/.../components/ability-builder/AbilityPreviewCanvas.vue`
- Create: `client/.../components/ability-builder/previewPlayback.ts`
- Test: `client/.../components/ability-builder/previewPlayback.test.ts`

**Approach:** Adapt the `AbilityAnimationViewer.vue` skeleton (canvas ref, `new GameState()`, `new Camera()`, `new CanvasRenderer(canvas, state, camera)`, RAF loop, `renderer.destroy()` on unmount, jsdom 2d-context bail). Differences:
- Instead of an authored timeline, the component takes `props.frames: PreviewFrame[]` and `props.currentTick: number` (v-model or prop+emit).
- `applyFrame(i)`: copy `frames[i].snapshot` fields onto the standalone `GameState` — `state.units`, `state.projectiles`, `state.effects`, `state.beams`, and zones/buildings as the client GameState expects. Reuse the client's existing "apply snapshot to GameState" path if one is exported (preferred — single source of truth for snapshot→state); otherwise assign the arrays directly like `AbilityAnimationViewer` does.
- Camera framing: frame the scene's unit bounding box (all units across the first frame) with padding, like `AbilityAnimationViewer.refreshCamera` but computed from the actual scene extent.
- `previewPlayback.ts` is a PURE helper (unit-testable, no DOM): given `{playing, speed, startedAtMs, nowMs, frameCount, seekTick}` returns the current frame index. Wall-clock only advances the *index*; the rendered visuals within a frame are authoritative from the snapshot (snapshot-driven decision). Scrubbing sets `seekTick` and pauses.

`previewPlayback.ts` core:
```ts
export function frameIndexAt(opts: {
  playing: boolean; startedAtMs: number; nowMs: number;
  speed: number; frameCount: number; frameDtSeconds: number; seekTick: number;
}): number {
  if (!opts.playing) return clampTick(opts.seekTick, opts.frameCount)
  const elapsed = ((opts.nowMs - opts.startedAtMs) / 1000) * opts.speed
  const idx = opts.seekTick + Math.floor(elapsed / opts.frameDtSeconds)
  return clampTick(idx, opts.frameCount)
}
```
(`frameDtSeconds` = the harness `previewTickDT` = 0.05, exported as a shared constant.)

- [ ] **Step 1 (test-first):** `previewPlayback.test.ts` — `frameIndexAt` clamps at 0 and `frameCount-1`, advances with elapsed×speed, honors `seekTick` when paused. Cover speed=2 and a mid-scrub resume.
- [ ] **Step 2:** Run → FAIL. Implement `previewPlayback.ts`. Run → PASS.
- [ ] **Step 3:** Build `AbilityPreviewCanvas.vue`. No literal `cursor:` in CSS (CLAUDE.md); use `--font-*` tokens for any text. jsdom-safe (bail if no 2d context) so it doesn't break the component test suite.
- [ ] **Step 4:** `vue-tsc -b` clean; existing client tests green. Commit.

**Spec-review focus:** snapshot→GameState uses the shared apply path if available (no parallel snapshot decoder); renderer destroyed on unmount; no wall-clock used for anything but the frame index.

---

## Task 7: Playback controls + overlays (Vue)

**Files:**
- Create: `client/.../components/ability-builder/PreviewOverlays.ts`
- Modify: `client/.../components/ability-builder/AbilityPreviewCanvas.vue`
- Test: `client/.../components/ability-builder/previewOverlays.test.ts`

**Approach:**
- Controls row under the canvas: Play/Pause, a scrub `<input type=range min=0 :max="frameCount-1">` bound to `currentTick`, a speed selector (0.5×/1×/2×), and a time readout (`{{ (currentTick*0.05).toFixed(2) }}s`). Dragging the scrubber pauses and seeks.
- `PreviewOverlays.ts`: pure functions returning draw primitives (center+radius circles in WORLD coords) for (a) the caster's cast range ring and (b) AoE radius at the cast/impact point, given the ability's entry range + the fixtures' radius. The canvas draws these on a transparent overlay `<canvas>` layered over the renderer canvas (do NOT modify `CanvasRenderer` — overlays are preview-only chrome), converting world→screen via the same `Camera`.
- Overlay visibility toggles (checkboxes): "Cast range", "AoE radius". Default on.

- [ ] **Step 1 (test-first):** `previewOverlays.test.ts` — world→screen circle projection matches `Camera` math for a known zoom/center; radius scales with zoom.
- [ ] **Step 2:** Run → FAIL. Implement `PreviewOverlays.ts`. Run → PASS.
- [ ] **Step 3:** Add controls + overlay canvas to `AbilityPreviewCanvas.vue`. Overlay canvas sits in the same stacking context, `pointer-events:none`, redrawn each RAF from the current camera.
- [ ] **Step 4:** `vue-tsc -b` + tests green. Commit.

**Spec-review focus:** overlays never touch `CanvasRenderer`; scrub pauses playback; world→screen uses the real `Camera`.

---

## Task 8: Wire canvas into the preview panel + playhead↔trace sync (Vue)

**Files:**
- Modify: `client/.../components/ability-builder/AbilityPreviewPanel.vue`
- Modify: `client/.../components/ability-builder/PreviewEventLog.vue` (emit seek)
- Test: `client/.../components/ability-builder/AbilityPreviewPanel.test.ts` (extend)

**Approach:**
- Mount `AbilityPreviewCanvas` at the top of `AbilityPreviewPanel`, above the existing 6a timeline + event log. Feed it `result.frames` and a `currentTick` ref owned by the panel.
- Two-way playhead sync:
  - Clicking a trace event in `PreviewEventLog` seeks the canvas: map the event's `t` (sim time) → nearest frame index (`Math.round(t/0.05)`), set `currentTick`, pause.
  - As playback advances `currentTick`, highlight the trace events whose `t` falls in `[currentTick*0.05, (currentTick+1)*0.05)` in the event log and move the `PreviewTimeline` playhead. Reuse the existing 6a event-selection mechanism (the row-click that jumps the inspector via `refFromPath`) — add a parallel "active at playhead" highlight without disturbing the click-to-inspect behavior.
- Keep the existing text result summary (mana spent, HP deltas) — it stays useful alongside the canvas.

- [ ] **Step 1 (test-first):** extend the panel test: given a `result` with `frames` + a `trace` event at `t=0.35`, clicking that event sets `currentTick` to 7 (0.35/0.05) and pauses; advancing `currentTick` to 7 marks that event active.
- [ ] **Step 2:** Run → FAIL. Implement. Run → PASS.
- [ ] **Step 3:** `vue-tsc -b` + full client test suite green. Commit.

**Spec-review focus:** click-to-inspect (6a) still works; playhead highlight is additive; time→frame mapping uses the shared 0.05 constant.

---

## Task 9: Live verification (Playwright)

**Files:** scratchpad script only (no repo change).

- [ ] **Step 1:** With the app running (`:5173` + backend), script Playwright to: open `/#/ability-editor`, select **Shatter** → Preview → Run Preview → assert the canvas shows the ground-burst effect on a mid-playback frame; scrub to t=0 and to end. Screenshot.
- [ ] **Step 2:** Repeat for **Meteor**: assert an early frame shows the falling effect and a post-impact frame shows the enemy HP bar reduced (marker fired) + crater effect. Screenshot.
- [ ] **Step 3:** Repeat for **Greater Heal**: assert the on-target heal effect renders on the injured ally. Screenshot.
- [ ] **Step 4:** Deliver the three screenshots to the user via `SendUserFile`.

---

## Final Review (after all tasks)

- [ ] Dispatch a holistic reviewer: production byte-identical (no ability v2; `pendingMarkers` empty in prod; `simTime` cheap accumulator), determinism (marker firing keyed off `dt`-driven `simTime`, snapshot capture reproducible), AI_RULES compliance (no cross-tick `*Unit`; program re-resolved by id in the scheduler), CLAUDE.md frontend rules (no literal cursors, `--font-*` tokens, `vue-tsc -b`), diagnostics rules (frames are a return value, not per-tick logs), Go↔TS contract (`PreviewFrame`/`frames`), full `go test ./...` + client tests + build green.
- [ ] Update memory `project_composable_abilities.md`: Phase 6b complete (snapshot-driven canvas replay + play_presentation + marker scheduler); note launch_projectile / channel / charge / moving-zone executors still deferred to the "executor pass"; the design's injectable-clock (N3) rework still deferred.

---

## Self-Review Notes (author)

- **Spec coverage:** design §9 center-right visual region → Tasks 6–8; playback scrub → Task 7; overlays → Task 7; trace sync → Task 8; the "real renderer / real execution trace, not a fake" requirement → snapshot-driven from the authoritative harness (Tasks 3+6). The injectable-clock N3 rework is deliberately deferred (locked decision 1).
- **Deferred-executor gap:** `launch_projectile` stays stubbed → projectile abilities (arcane_bolt/chain/orb/missiles) still preview without their travel visual. This is intentional and disclosed; the canvas fills in for free when that executor lands.
- **Type consistency:** `PreviewFrame{Tick,Time,Snapshot}` (Go) ↔ `PreviewFrame{tick,t,snapshot}` (TS); `frameDtSeconds`/`previewTickDT` = 0.05 shared both sides; scheduler stores IDs+positions+action defs only.
- **Biggest risk:** the marker scheduler's determinism and its production no-op. Both are pinned by tests (Task 2 Steps 2–3, 6) and the holistic review. If `getAbilityDef` is not the correct resolver name for a preview-registered ability (it registers into `runtimeAbilities` under a unique id), the scheduler must use the SAME resolver the executor uses to fetch `def.Program` — confirm the resolver name during Task 2 Step 5 and match it.
