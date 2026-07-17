# Composable Abilities — Phase 6a (Trace-Driven Execution Preview) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Give the editor an **accurate, deterministic execution preview** driven by the executor's own trace — not by inferring behavior from visuals (design §10's core principle). A server preview harness runs an ability's composable program in an isolated `GameState` with tracing on, steps the sim deterministically, and returns an `AbilityExecutionTrace` + a compact result (per-unit HP/mana, zones). The editor renders a **timeline + event log** from the trace and a minimal **scene config + Run** control, with log rows that select the corresponding flow node.

**Scope decision (why 6a, not the full visual canvas).** Design §10's visual "replay into the real `CanvasRenderer`" needs (a) a deterministic/injectable clock threaded through `CanvasRenderer.render()` + effect/projectile/overlay frame math (a real renderer rework), and (b) the still-deferred presentation/projectile/animation-marker executors — without which most abilities would render incompletely. The **trace** is the design's stated source of truth ("expose debug events from the execution engine rather than inferring from visuals"), works TODAY for the executor-runnable subset, and is honest about deferred actions (they appear as `action_skipped`). **6a delivers the trace-driven preview; the visual canvas + clock rework is 6b**, gated on the presentation executors.

**Architecture:** A `previewTrace`/`previewClock` on `GameState` (nil/0 in production → zero behavior change) that the executor's ctx-build sites read to activate + timestamp tracing. `RunAbilityPreview` reuses the golden-test harness pattern (isolated `GameState` via `NewGameStateWithSeed`, spawn scene, cast, step `Update(dt)`). It always runs the ability's **composable program** (authored `Program` for v2, else `compileLegacyAbility(def)` — same as the convert/golden path), so the preview shows composable behavior; a deferred-mechanic ability's trace shows the `action_skipped` events honestly. `POST /abilities/preview` wraps it. The editor's preview panel lives in the main column as a Flow⇄Preview view toggle (the `EditorShell` rail stays the Inspector; §9's center-right region becomes 6b's visual canvas).

**Determinism:** `NewGameStateWithSeed(cfg, seed)`; the executor + tick paths are seeded/no-wall-clock; the harness accumulates its own `dt` for timestamps. Same request → same trace.

**Tech Stack:** Go (`server/internal/game`, `server/internal/http`) + TS/Vue (`client/.../ability-builder/`). `cd server && go test ./internal/game/ ./internal/http/`; client `npm run test` + `npx vue-tsc -b`. Do NOT run `git commit`.

**Key existing seams (verified):**
- Executor: `runProgramTriggersLocked(ctx, triggers, TriggerOnCastComplete)`; `RuntimeAbilityContext{... Trace *AbilityExecutionTrace}`; `ctx.trace(typ, path, payload)` (nil-safe; currently records time 0). `AbilityExecutionTrace{Events []AbilityExecutionTraceEvent{Time,Type,Path,Payload}}`. The executor entry points that build a ctx: `resolveAbilityProgramCastLocked(caster, def, primary, point)` (`ability_cast.go`, the Phase-4 wiring) and `fireAbilityZoneTickLocked(z)` (`ability_zone.go`).
- Cast: `RequestAbilityCast(playerID, casterUnitID, abilityID, targetUnitID, targetX, targetY) (bool, string)` (`ability_autocast.go:313`) or `beginAbilityCastLocked`/`beginAbilityCastAtPointLocked`. `Update(dt float64)` (`state.go:2739`) is the tick.
- Compile: `compileLegacyAbility(def) *AbilityProgram`, `ConvertLegacyAbility(id)`, `AbilityProgramRunnable(prog)`.
- Scene: golden tests (`ability_compile_golden_test.go`) show the isolated-`GameState`+spawn pattern (`NewGameStateWithSeed`, `spawnPlayerUnitLocked`/`teamCombatUnit`, two hostile players). READ them for the exact helpers.
- Overlay registration for a non-catalog/preview ability: `runtimeAbilities` (see `ability_persistence.go` / how `ability_cast_program_test.go` injected a v2 ability).

---

### Task 1: Executor trace-mode plumbing (activation + timestamps)

**Files:**
- Modify: `server/internal/game/state.go` (2 fields on `GameState`), `ability_exec.go` (`now` on ctx + `trace` stamps it), `ability_cast.go` + `ability_zone.go` (wire Trace/now at the 2 ctx sites).
- Test: `server/internal/game/ability_exec_trace_test.go`

- [ ] **Step 1: Write the failing test.** A traced executor run records timestamped events.

```go
func TestExecutorTraceModeRecordsTimestampedEvents(t *testing.T) {
	s := newProjectileTestState(t) // or the golden-test builder
	caster := /* p1 */; enemy := /* p2 hostile, in range */
	def := AbilityDef{ID: "x", SchemaVersion: 2, DamageType: DamageFire, Program: &AbilityProgram{
		Triggers: []AbilityTriggerDef{{ID: "t", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{
			{ID: "sel", Type: ActionSelectTargets, Outputs: map[string]string{"targets": "e"},
				Target: &TargetQueryDef{Source: SrcInitialTarget}},
			{ID: "dmg", Type: ActionDealDamage, Input: map[string]ContextRef{"targets": {Key: "e"}},
				Config: []byte(`{"amount":10,"type":"fire"}`)}}}}}}
	tr := &AbilityExecutionTrace{}
	s.mu.Lock()
	s.previewTrace = tr
	s.previewClock = 1.25
	s.resolveAbilityProgramCastLocked(caster, def, enemy, protocol.Vec2{})
	s.previewTrace = nil
	s.mu.Unlock()
	if len(tr.Events) == 0 { t.Fatal("no trace events recorded in preview mode") }
	var sawDamage bool
	for _, e := range tr.Events { if e.Type == "damage_applied" { sawDamage = true; if e.Time != 1.25 { t.Errorf("event time = %v, want 1.25", e.Time) } } }
	if !sawDamage { t.Fatal("no damage_applied event") }
}

func TestExecutorTraceOffByDefault(t *testing.T) {
	// previewTrace nil ⇒ production path: no panic, no trace, zero behavior change.
	// (Run an existing executor test path with previewTrace unset — the existing
	//  suite already covers this; add a focused assertion that ctx.Trace is nil when
	//  s.previewTrace is nil by checking a cast produces the same HP delta as before.)
}
```

- [ ] **Step 2: Run** → FAIL (`previewTrace`/`previewClock`/`now` undefined).

- [ ] **Step 3: Implement.**
  - `state.go`: add to `GameState` (near other runtime-only fields): `previewTrace *AbilityExecutionTrace` and `previewClock float64` with a doc comment: "Non-nil only during a preview harness run (RunAbilityPreview). When set, the executor attaches it to every RuntimeAbilityContext it builds and stamps events with previewClock (the harness's accumulated sim time). Nil in real matches ⇒ zero tracing overhead + no behavior change."
  - `ability_exec.go`: add `now float64` to `RuntimeAbilityContext` (doc: "sim time stamped onto trace events; set from GameState.previewClock at ctx build; 0 in production"). Change `ctx.trace` to record `ctx.now` instead of `0`.
  - `ability_cast.go` `resolveAbilityProgramCastLocked`: when building the ctx, set `Trace: s.previewTrace, now: s.previewClock` (guarded — these are nil/0 in production, so no change). 
  - `ability_zone.go` `fireAbilityZoneTickLocked`: same — set `ctx.Trace = s.previewTrace; ctx.now = s.previewClock` on the per-tick ctx it builds (so zone burn ticks are traced with their firing time).
  - CONFIRM there are no OTHER executor ctx-build sites that need wiring (grep `RuntimeAbilityContext{`); wire each executor-entry ctx (not test-only ones). Report the sites.

- [ ] **Step 4: Run** the new tests + FULL `go test ./internal/game/ -count=1` (this touches the live executor + Update path — must be green; production behavior unchanged since previewTrace is nil everywhere but the harness) + build/vet/gofmt. Green.

- [ ] **Step 5: Commit** — "feat(abilities): executor preview-trace mode (activation + timestamps), off by default".

---

### Task 2: `RunAbilityPreview` harness

**Files:**
- Create: `server/internal/game/ability_preview.go`
- Test: `server/internal/game/ability_preview_test.go`

- [ ] **Step 1: Write failing tests.** Define the request/result shapes + assert deterministic trace/results for a few abilities.

```go
type PreviewSceneUnit struct {
	Team   string  `json:"team"`   // "ally" | "enemy"
	X, Y   float64 `json:"x","y"`
	HP     int     `json:"hp"`
	MaxHP  int     `json:"maxHp"`
}
type PreviewRequest struct {
	Ability   AbilityDef         `json:"ability"`   // authored def (may be legacy or v2); harness compiles if needed
	Seed      int64              `json:"seed"`
	CasterX, CasterY float64     `json:"casterX","casterY"`
	Units     []PreviewSceneUnit `json:"units"`     // allies + enemies around the caster
	Target    int                `json:"target"`    // index into Units for a unit-target cast (-1 = none)
	CastX, CastY float64         `json:"castX","castY"` // for point casts
	DurationSeconds float64      `json:"durationSeconds"` // how long to simulate (capped)
}
type PreviewUnitResult struct { Index int; Team string; HPBefore, HPAfter int }
type PreviewResult struct {
	Trace    []AbilityExecutionTraceEvent `json:"trace"`
	Units    []PreviewUnitResult          `json:"units"`
	CasterManaSpent int                   `json:"casterManaSpent"`
	Runnable bool                         `json:"runnable"`  // AbilityProgramRunnable of the previewed program
	Warnings []string                     `json:"warnings"`  // deferred-action notices (reuse convert's)
}

func TestRunAbilityPreview_GreaterHeal(t *testing.T) {
	def, _ := getAbilityDef("greater_heal")
	res, err := RunAbilityPreview(PreviewRequest{ Ability: def, Seed: 1,
		CasterX: 0, CasterY: 0, Target: -1, DurationSeconds: 2,
		Units: []PreviewSceneUnit{{Team:"ally", X:40, Y:0, HP:20, MaxHP:100}, {Team:"ally", X:80, Y:0, HP:60, MaxHP:100}} })
	if err != nil { t.Fatal(err) }
	// healing_applied events present; the injured allies ended higher.
	if !traceHasType(res.Trace, "healing_applied") { t.Fatal("no healing_applied") }
	if res.Units[0].HPAfter <= res.Units[0].HPBefore { t.Fatal("ally not healed") }
}

func TestRunAbilityPreview_Deterministic(t *testing.T) {
	// same request twice → identical trace (types+times+payload) + identical unit results.
}

func TestRunAbilityPreview_MeteorShowsSkipped(t *testing.T) {
	// meteor (deferred marker/presentation) → trace contains action_skipped for play_presentation;
	// Runnable=false; Warnings non-empty. (Honest: preview shows what WOULD run.)
}
```
(Add `traceHasType` helper. Adapt spawn to the real golden-test helpers.)

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement** `RunAbilityPreview(req PreviewRequest) (PreviewResult, error)` (a top-level func; acquires `s.mu` internally like other entry points):
  - Build the **preview def**: `previewDef := req.Ability`; if `previewDef.SchemaVersion < 2 || previewDef.Program == nil` → `previewDef.Program = compileLegacyAbility(req.Ability); previewDef.SchemaVersion = 2` (run the composable view). Validate it (`validateAbilityDef`); on error return it.
  - `s := NewGameStateWithSeed(previewMapConfig(), req.Seed)` (use a minimal map config — read how NewGameStateWithSeed is called in tests). Register `previewDef` so `getAbilityDef` finds it: inject into `runtimeAbilities` under its mutex for the life of this call, and REMOVE it after (defer) so the global overlay isn't polluted — OR (cleaner) spawn the caster and directly resolve without the catalog lookup. DECISION: since the cast path calls `getAbilityDef(abilityID)`, inject into `runtimeAbilities` with a **preview-unique id** (e.g. `"__preview__"` or the def id) and `defer` its removal. Guard against concurrent previews with the runtimeAbilities mutex (acceptable — previews are infrequent editor calls). Report the approach.
  - Spawn: a caster for player `p1` at (CasterX,CasterY) with mana ≥ the ability's cost and the ability granted; each `req.Units` as `p1` (ally) or `p2` (enemy, hostile) at its position with HP/MaxHP. Set the two players on opposing teams (reuse the golden-test team setup). Record each unit's HPBefore + the caster's mana-before.
  - Attach trace + clock: `tr := &AbilityExecutionTrace{}; s.previewTrace = tr; s.previewClock = 0`.
  - Issue the cast: unit-target (`req.Target >= 0`) via `RequestAbilityCast(p1, casterID, previewAbilityID, targetUnitID, 0, 0)`; point (`TargetsPoint`) via `RequestAbilityCast(..., 0, req.CastX, req.CastY)`. (RequestAbilityCast routes point vs unit by def.TargetsPoint.)
  - Step the sim: `dt := previewDt` (e.g. 0.05); `n := min(ceil(req.DurationSeconds/dt), maxPreviewTicks)` (cap e.g. 400). Loop: `s.previewClock += dt` (or set before) then `s.Update(dt)` — so cast-time resolution, zone ticks, etc. all fire and get traced with real times. (Update acquires s.mu itself; do NOT hold the lock across Update — set previewTrace/clock as fields read under the lock inside Update's executor calls. Confirm the locking: set previewTrace on `s` before the loop under a brief lock, then call Update which locks internally.)
  - Collect: `res.Trace = tr.Events`; per-unit HPAfter (re-resolve each spawned unit id → HP; dead units → 0); `CasterManaSpent = manaBefore - casterMana`; `Runnable = AbilityProgramRunnable(previewDef.Program)`; `Warnings` = the deferred-action notices (reuse `degradationWarnings` from ConvertLegacyAbility, or `AbilityProgramRunnable`-derived). Clear `s.previewTrace = nil`.
  - Return. (Cap everything; a runaway program is bounded by the executor's op-budget + the tick cap.)

- [ ] **Step 4: Run** all preview tests + `-count=3` determinism + full `go test ./internal/game/ -count=1` + build/vet/gofmt. Green. Confirm `runtimeAbilities` is not left polluted after a preview (a follow-up `getAbilityDef(previewId)` returns not-found).

- [ ] **Step 5: Commit** — "feat(abilities): RunAbilityPreview harness (deterministic trace + result)".

---

### Task 3: `POST /abilities/preview` endpoint

**Files:**
- Modify: `server/internal/http/editor_handlers.go` (route)
- Test: `server/internal/http/editor_preview_test.go`

- [ ] **Step 1: Write the failing httptest.** `POST /abilities/preview` body `{ ...PreviewRequest... }` → 200 `PreviewResult` JSON with `trace`/`units`/`runnable`. Malformed JSON → 400. A body with a bad ability program → 200 with a trace/validation reflecting it OR 400 if the preview def fails validation (pick + document — prefer 400 `preview_failed` on a hard error, 200 otherwise). Bound the body size (MaxBytesReader) since the request carries a full def.

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement** the route (register as exact `/abilities/preview` before the `/abilities/` catch-all, like `/abilities/validate`): POST only (405 else); decode `game.PreviewRequest` (400 `invalid_json`); `res, err := game.RunAbilityPreview(req)`; on err → 400 `preview_failed` with the message; else `writeJSON(w, res)`. Add a sane default scene if `req.Units` is empty (e.g. one enemy in range) so a bare request still previews something — OR require units and 400 if empty (document; prefer a sensible default so the editor's first Run works out of the box).

- [ ] **Step 4: Run** + full `go test ./internal/http/ -count=1` (existing ability routes still green — precedence intact) + build/vet/gofmt. Green.

- [ ] **Step 5: Commit** — "feat(abilities): preview endpoint".

---

### Task 4: TS preview API + types

**Files:**
- Create: `client/.../abilities/program/programPreview.ts`
- Modify: `client/.../abilities/abilityEditorApi.ts`
- Test: `client/.../abilities/program/programPreview.test.ts`, extend `abilityEditorApi.test.ts`

- [ ] TS mirrors of `PreviewRequest`/`PreviewResult`/`PreviewSceneUnit`/`PreviewUnitResult` + `AbilityExecutionTraceEvent {t:number,type:string,path?:string,payload?:Record<string,unknown>}` (match the Go json tags — note the event's `Time` tag is `t`). `runAbilityPreview(req): Promise<PreviewResult>` in `abilityEditorApi.ts` (POST `/abilities/preview`, follow the existing fetch idiom). A `parsePreviewResult` defensive shaper (trace→[], units→[]). Tests: mock fetch; `runAbilityPreview` posts + returns typed result; `parsePreviewResult` coerces missing fields.
- [ ] `npm run test -- programPreview abilityEditorApi` PASS; `npx vue-tsc -b` clean.
- [ ] **Commit** — "feat(abilities): TS preview API + trace types".

---

### Task 5: Editor Preview panel (timeline + event log + scene + Run)

**Files:**
- Create: `client/.../ability-builder/AbilityPreviewPanel.vue`, `PreviewTimeline.vue`, `PreviewEventLog.vue`, `PreviewSceneControls.vue`
- Modify: `AbilityBuilderPanel.vue` (a Flow ⇄ Preview view toggle in the main column)
- Test: component tests

- [ ] **AbilityPreviewPanel** (injects the builder): a **Run Preview** button that builds a `PreviewRequest` from the current `builder.form` + `builder.program` (as a full `AbilityDef` — `saveRequestFromForm(form)` + `schemaVersion:2` + `serializeProgram(program)`) + the scene config, calls `runAbilityPreview`, stores the `PreviewResult`. Shows `busy`/`error`. Requires the ability to be in a saveable-ish state (but preview should work even mid-edit — it sends the current program). Disable Run while `builder.busy`.
  - **PreviewSceneControls**: minimal scene config — number of enemies + allies (each spawned at a spread of positions with a default HP, e.g. enemies 100/100, allies pre-damaged so heals show), a target selector (first enemy / first ally / point), a seed, a duration (default 2s). Keep it compact; sensible defaults so one click previews.
  - **PreviewTimeline**: a horizontal time axis with a marker per trace event (colored by type: cast/target/damage/heal/zone/skip/error), positioned by `event.t` over the run duration. Clicking a marker highlights the event in the log.
  - **PreviewEventLog**: tabbed (Overview / Damage / Healing / Targets / Context / Validation) or a filterable list; each row `= {t.toFixed(2)}s  {type}  {summary from payload}` (e.g. "1.40s damage_applied Enemy 1 ← 140 fire"). Clicking a row with a `path` → `builder.select(refFromPath(path))` (parse `triggers[i].actions[j]` → the NodeRef by resolving indices to ids from `builder.program`) so the flow + inspector focus the offending/acting node. `action_skipped` rows get a muted "deferred" style. A Validation tab surfaces `builder.issues`.
  - Also show the `PreviewResult.units` (HP before→after per unit) + `casterManaSpent` + a `Runnable`/`Warnings` banner (deferred-mechanic honesty: "This preview ran the composable program; N actions are display-only and were skipped — they'll run once a later phase lands.").
- **AbilityBuilderPanel**: add a segmented control in the main column header area to switch the main body between **Flow** (the current flow+palette) and **Preview** (the AbilityPreviewPanel). The Inspector rail stays. (The §9 center-right visual canvas is 6b.)
- Styling: LoC (dark wood/brass, gold); no literal `cursor:` except `not-allowed`. Reuse toolkit.
- [ ] Tests: PreviewPanel Run calls `runAbilityPreview` with a request built from form+program+scene; timeline renders a marker per event; log row with a path calls `builder.select`; the deferred-warning banner shows when `runnable:false`.
- [ ] **Commit** — "feat(ability-editor): trace-driven preview panel (timeline + event log + scene)".

---

## Self-review notes

- **Zero production impact:** `previewTrace`/`previewClock` are nil/0 in real matches; the executor's ctx wiring is a guarded read; nothing traces or changes behavior outside a `RunAbilityPreview` call. Full `go test ./...` must stay green.
- **Determinism:** seeded `GameState`, no wall-clock, harness-accumulated time for stamps — same request → same trace (tested `-count=3`).
- **Honesty about deferred mechanics:** the preview runs the composable program; deferred actions (projectile/presentation/marker/channel/charge/moving-zone) appear as `action_skipped` and drive the `Runnable:false` + `Warnings` banner — the preview never fakes what the runtime can't do yet.
- **Reuse, not reimplement:** the harness reuses `compileLegacyAbility` + the golden-test scene pattern + the real `Update` tick + the real executor. The trace is the executor's own, not a re-derivation.
- **Type consistency Go↔TS:** `PreviewRequest`/`PreviewResult`/`AbilityExecutionTraceEvent` (event time json tag `t`) mirrored exactly.
- **Deferred to 6b (not silent):** the visual `CanvasRenderer` replay + deterministic/injectable clock (N3), overlays (ranges/hitboxes/render-layers), playback scrubbing of a rendered scene, and full visual fidelity (needs the presentation/projectile/marker executors). Each noted here + at the panel's Flow⇄Preview toggle (the Preview region is where 6b's canvas mounts).
