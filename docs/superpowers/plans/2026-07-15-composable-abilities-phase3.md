# Composable Abilities — Phase 3 (Runtime Executor) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a deterministic trigger/action **executor** that runs an `AbilityProgram` by calling the existing authoritative gameplay seams (never reimplementing damage/heal/summon/pull/CC), with a typed `RuntimeAbilityContext`, target-query resolution, a generalized zone entity, and an execution trace. **The live cast path is NOT rerouted yet** — the executor is exercised by tests against authored programs and stays dormant for real catalog abilities until Phase 4 (the legacy compiler) can guarantee every ability has a program.

**Architecture:** New `ability_exec*.go` files beside the Phase 2 model. The executor walks `AbilityProgram` triggers of a given `TriggerType`, decodes each action's `Config` via the Phase 2 registry, resolves its target set, and dispatches to a per-action `Execute` function that adapts to an existing `*Locked` seam. A new `AbilityZone` entity carries a compiled `on_zone_tick` trigger and is ticked in `Update`. All entity refs are **IDs**, re-resolved + validated at point of use (per the ID-targeting invariant). All paths run under `s.mu` (`*Locked`), contain no wall-clock, and use only the seeded RNG.

**Tech Stack:** Go. Table/integration tests build a real `GameState` via `NewGameStateWithSeed(cfg, seed)` and assert HP deltas + trace events. `cd server && go test ./internal/game/ -run <Name> -count=1`.

**Reference — seams the executor calls (exact signatures, verified):**
- `applyUnitDamageWithSourceLocked(target *Unit, damage int, src DamageSource) int` — `perks_defense.go:49`. `DamageSource{AttackerUnitID int; Kind string; DamageType DamageType}` — `damage_pipeline.go:54`.
- `applyAbilitySplashDamageLocked(ownerUnitID int, ownerPlayerID string, cx, cy, radius float64, damage int, dmgType DamageType, primaryID int)` — `state_combat.go:428`.
- `applyClericHealLocked(caster, target *Unit, amount int, meta HealMeta)` — `perks_cleric.go:575`; `healMetaPrimaryAbility() HealMeta` — `perks_cleric.go:525`.
- `spawnSummonedUnitLocked(caster *Unit, def AbilityDef)` — `ability_summon.go:38`.
- `applyPullInRadiusLocked(caster *Unit, cx, cy, radius, strength, duration float64) int` — `spell_pull.go:43`.
- `applyProcSlowLocked(targetID int, multiplier, duration float64, dmgType DamageType)` — `combat_ai_cc.go:113`; `ApplyStunLocked(targetID int, duration float64)` — `:34`; `applyProcBurnLocked(targetID int, dps, duration float64, attackerUnitID int)` — `:134`.
- `spendUnitManaLocked(unit *Unit, cost int) bool` — `mana.go:92`.
- `getUnitByIDLocked(id int) *Unit` — `state_helpers.go:10`; `distanceSquared(ax, ay, bx, by float64) float64` — `state_helpers.go:102`; `unitsHostileLocked(a, b *Unit) bool` — `state_combat.go:151`; `unitsFriendlyLocked(a, b *Unit) bool` — `:158`.
- Zone reference pattern: `GroundHazard` + `tickGroundHazardsLocked` — `ground_hazard.go`; `s.GroundHazards`/`s.nextGroundHazardID` on `GameState` (`state.go:995,1319`), ticked in `Update` after combat/trap/projectile and before `drainPendingDeathsLocked` (`state.go:~2813`).

**Standing rules:** `*Locked` = caller holds `s.mu`. Determinism: iterate `s.Units` slice (stable order), no map iteration for outcomes, seeded RNG only. Do NOT run `git commit` — the user handles staging/commits.

**Scope & explicit deferrals (do NOT build these in Phase 3):**
- Trigger types built: `on_cast_complete`, `on_action_complete`, `on_zone_tick`, `custom` (via `trigger_event`/named triggers).
- **Deferred to a later phase** (need scheduling infra / presentation / preview): `on_animation_marker`, `on_projectile_impact`, `on_status_tick`/`on_status_expire`, `on_zone_enter`/`on_zone_exit`, `on_target_hit`, `on_damage_dealt`, `on_unit_death`, `on_charge_full`.
- Actions built: `select_targets`, `deal_damage`, `restore_health`, `summon_unit`, `apply_force`, `apply_status`, `remove_status`, `modify_resource`, `create_zone`, `store_targets`, `filter_targets`, `wait`, `conditional`, `repeat`, `trigger_event`.
- **Deferred** (presentation/preview phase): `play_presentation`, `play_sound`, `change_render_layer`, `camera_shake`, `launch_projectile`, `move_unit`. These remain known-but-descriptorless (validator skips them; the executor no-ops with a trace `action_skipped` event).
- **Spell-modifier scaling** (perk/item `+% damage`) is NOT applied to action config amounts in Phase 3 (literal amounts; damage still routes through the mitigation pipeline). Parity with the legacy `effectiveSpellLocked` path is a **Phase 4** concern noted in Task 3.

---

### Task 0: Fix the two Phase-2 carry-forwards

**Files:**
- Modify: `server/internal/game/ability_program_validate.go`
- Test: `server/internal/game/ability_program_validate_test.go` (add cases)

- [ ] **Step 1: Write failing tests.**

```go
func TestKnownActionTypesCoversAllConsts(t *testing.T) {
	// Guard against drift: every ActionType const must be in knownActionTypes.
	for _, at := range allActionTypes {
		if !isKnownActionType(at) {
			t.Errorf("allActionTypes contains %q but isKnownActionType is false", at)
		}
	}
	if len(allActionTypes) != len(knownActionTypes) {
		t.Errorf("allActionTypes (%d) and knownActionTypes (%d) out of sync", len(allActionTypes), len(knownActionTypes))
	}
}

func TestActionEnabledDefaultsTrueWhenAbsent(t *testing.T) {
	// An action authored WITHOUT "disabled" must be enabled (default-on).
	var a AbilityActionDef
	if err := json.Unmarshal([]byte(`{"id":"a","type":"deal_damage"}`), &a); err != nil {
		t.Fatal(err)
	}
	if !a.IsEnabled() {
		t.Error("action with absent disabled key should be enabled")
	}
	// An explicit "disabled": true turns it off.
	var b AbilityActionDef
	if err := json.Unmarshal([]byte(`{"id":"b","type":"deal_damage","disabled":true}`), &b); err != nil {
		t.Fatal(err)
	}
	if b.IsEnabled() {
		t.Error(`action with "disabled":true should be disabled`)
	}
}
```

- [ ] **Step 2: Run** `cd server && go test ./internal/game/ -run 'TestKnownActionTypesCovers|TestActionEnabledDefaults' -count=1` → FAIL (`allActionTypes` undefined; `IsEnabled` undefined; `disabled` not decoded).

- [ ] **Step 3: Implement.** In `ability_program_validate.go`:
  - Replace the hand-maintained `knownActionTypes` map with a single canonical slice and derive the map from it:

```go
// allActionTypes is the canonical list of every ActionType. isKnownActionType
// derives from it so the two cannot drift (guarded by a test).
var allActionTypes = []ActionType{
	ActionSelectTargets, ActionStoreTargets, ActionFilterTargets, ActionDealDamage,
	ActionRestoreHealth, ActionApplyStatus, ActionRemoveStatus, ActionCreateZone,
	ActionLaunchProjectile, ActionSummonUnit, ActionMoveUnit, ActionApplyForce,
	ActionModifyResource, ActionTriggerEvent, ActionPlayPresentation, ActionPlaySound,
	ActionChangeRenderLayer, ActionCameraShake, ActionWait, ActionConditional,
	ActionRepeat, ActionCustom,
}

var knownActionTypes = func() map[ActionType]bool {
	m := make(map[ActionType]bool, len(allActionTypes))
	for _, t := range allActionTypes {
		m[t] = true
	}
	return m
}()
```

  - Solve the `Enabled` default by **inverting the field's JSON semantics** so the zero value means enabled — this is the clean fix and avoids a lossy "always force true" normalization that would make disabling impossible. Rename the field on `AbilityActionDef` (in `ability_program.go`) from `Enabled bool` to `Disabled bool` with json tag `"disabled,omitempty"`. Then absent/false ⇒ enabled (the authoring default), and only an explicit `"disabled": true` turns an action off. Add an accessor `func (a AbilityActionDef) IsEnabled() bool { return !a.Disabled }` and use `IsEnabled()` everywhere the executor/validator checks enablement (Task 3's `executeActionLocked`, the `no_behavior` count if it should ignore disabled actions — keep counting all for now).

  - Update every existing construction/test that set `Enabled: true` to simply omit the field (the new default is enabled), and the two Phase-2 v2 fixtures: remove `"enabled": true` from every action in `greaterHealV2JSON`/`meteorV2JSON` and the design doc §5 JSON (the field is now `disabled`, default off). Re-run the Phase-2 fixture tests to confirm they still pass with the field removed.

  `normalizeAbilityProgramDefaults` is therefore NOT needed for enablement. (The test `TestActionEnabledDefaultsTrueWhenAbsent` above asserts `IsEnabled()` on a decoded action with no `disabled` key.)

  - **TS side (mirror the rename):** in `client/.../abilities/program/abilityProgram.ts`, the `AbilityActionDef` interface's enablement field must match — rename `enabled?: boolean` to `disabled?: boolean` (default-on semantics; no accessor needed in TS yet, but add a doc comment "omitted/false = enabled"). Update `client/.../abilities/program/fixtures.test.ts` (and any `abilityProgram.test.ts` literal) to drop `enabled: true` from action literals. Run `cd client/src/game-portal && npm run test -- abilityProgram fixtures && npx vue-tsc -b` → clean. Grep `client/.../program` for `enabled` to confirm none of the OLD field usage remains.

- [ ] **Step 4: Run** the two new tests + full package `go test ./internal/game/ -count=1` → PASS. After removing `"enabled": true` from the fixtures (now default-on), re-run the Phase 2 fixture tests (`TestGreaterHealV2Fixture`, `TestMeteorV2Fixture`) and the TS fixture/round-trip tests to confirm they still pass. Also `grep -rn "\.Enabled\b" server/internal/game` to confirm no lingering reference to the old field name remains (must be zero after the rename).

- [ ] **Step 5: Commit** — checkpoint: "fix(abilities): guard knownActionTypes drift; normalize action Enabled default".

---

### Task 1: Executor context, trace, and the `Execute` seam

**Files:**
- Create: `server/internal/game/ability_exec.go`
- Test: `server/internal/game/ability_exec_test.go`

> **Decision (applies to Tasks 1, 2, 5):** positions use the existing `protocol.Vec2` (`{X,Y float64}`), NOT a new local `Vec2` type — `protocol.Vec2` is already the position idiom across `internal/game` (`ability_summon.go`, `spawnPlayerUnitLocked`, …). Wherever this plan writes `Vec2`, read `protocol.Vec2`.

- [ ] **Step 1: Write the failing test** (construction + trace recording).

```go
func TestRuntimeContextAndTrace(t *testing.T) {
	tr := &AbilityExecutionTrace{}
	tr.record(0, "cast_started", "", nil)
	if len(tr.Events) != 1 || tr.Events[0].Type != "cast_started" {
		t.Fatalf("trace not recorded: %+v", tr.Events)
	}
	ctx := &RuntimeAbilityContext{CasterID: 7, Named: map[string]ContextValue{}}
	ctx.Named["x"] = ContextValue{Kind: ctxUnitSet, UnitIDs: []int{1, 2}}
	if got := ctx.Named["x"].UnitIDs; len(got) != 2 {
		t.Fatalf("context set = %v", got)
	}
}
```

- [ ] **Step 2: Run** → FAIL (undefined types).

- [ ] **Step 3: Implement** `ability_exec.go`:

```go
package game

// Vec2 is a world-space position used by the ability executor's context.
type Vec2 struct{ X, Y float64 }

type ctxValueKind int

const (
	ctxNone ctxValueKind = iota
	ctxUnitID
	ctxUnitSet
	ctxPosition
)

// ContextValue is one typed runtime value bound under a name (an output binding)
// or read from a ContextRef. Entity refs are IDs, resolved at point of use.
type ContextValue struct {
	Kind     ctxValueKind
	UnitID   int
	UnitIDs  []int
	Position Vec2
}

// RuntimeAbilityContext is the typed context one execution runs under. All entity
// references are unit IDs (re-resolved + validated via getUnitByIDLocked at use).
// Trace is nil in production (zero overhead) and non-nil for preview/tests.
type RuntimeAbilityContext struct {
	CasterID       int
	AbilityID      string
	CastID         int64
	InitialTarget  int // 0 = none
	CastPoint      Vec2
	EventPosition  Vec2
	ImpactPosition Vec2
	ZoneCenter     Vec2
	OwnerUnitID    int // owner of the current zone/status/projectile, if any
	Selected       []int // most recent select_targets output (previous_action_targets)
	Named          map[string]ContextValue
	Trace          *AbilityExecutionTrace
	depth          int // recursion guard for trigger_event / named triggers
}

// AbilityExecutionTrace is the ordered event log the executor emits when non-nil.
// It is the single source for the preview timeline + event log (later phase).
type AbilityExecutionTrace struct{ Events []AbilityExecutionTraceEvent }

type AbilityExecutionTraceEvent struct {
	Time    float64        `json:"t"`
	Type    string         `json:"type"`
	Path    string         `json:"path,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`
}

func (tr *AbilityExecutionTrace) record(t float64, typ, path string, payload map[string]any) {
	if tr == nil {
		return
	}
	tr.Events = append(tr.Events, AbilityExecutionTraceEvent{Time: t, Type: typ, Path: path, Payload: payload})
}

// traceCtx is a small helper so executor code can emit an event without a nil check.
func (ctx *RuntimeAbilityContext) trace(typ, path string, payload map[string]any) {
	if ctx.Trace != nil {
		ctx.Trace.record(ctx.EventPosition.X*0, typ, path, payload) // time filled by scheduler later; 0 here
	}
}
```

Note the `trace` time is 0 in Phase 3 (synchronous execution has no simulation clock threaded yet; the preview phase supplies real times). Keep the field for forward-compat.

Also add the `Execute` function field to `ActionDescriptor` in `ability_program_registry.go`:

```go
// Execute applies the action. targets is the resolved target-set (unit IDs) the
// executor prepared from the action's Target query / Input refs. It returns the
// action's output unit IDs (e.g. select_targets returns its selection, deal_damage
// returns the units actually hit) which the executor binds per action.Outputs.
// nil Execute ⇒ the action is a no-op in the runtime (e.g. presentation actions
// deferred to a later phase).
Execute func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int
```

Leave the three existing descriptors' `Execute` nil for now (wired in Task 3).

- [ ] **Step 4: Run** the new test + `go build ./...` + full package → PASS/clean.

- [ ] **Step 5: Commit** — "feat(abilities): executor context + trace + Execute seam".

---

### Task 2: Target-query resolution

**Files:**
- Create: `server/internal/game/ability_exec_targeting.go`
- Test: `server/internal/game/ability_exec_targeting_test.go`

- [ ] **Step 1: Write the failing test.** Build a real `GameState` with a caster + allies + enemies at known positions and assert the resolver's output for a few queries.

```go
func TestResolveTargetQueryRadiusRelationsOrdering(t *testing.T) {
	s := NewGameStateWithSeed(testMapConfig(), 1) // use existing test helper for a minimal map
	caster := s.addTestUnit("acolyte", "p1", 0, 0, 100, 100)   // (helper: type, owner, x, y, hp, maxhp)
	e1 := s.addTestUnit("raider", "p2", 50, 0, 30, 100)
	e2 := s.addTestUnit("raider", "p2", 100, 0, 80, 100)
	_ = s.addTestUnit("raider", "p2", 999, 0, 100, 100)        // out of radius
	ctx := &RuntimeAbilityContext{CasterID: caster.ID}
	q := TargetQueryDef{
		Source: SrcAllInScene, Origin: OriginCaster, Relations: []TargetRelation{RelEnemy},
		Radius: 200, Ordering: OrderLowestHealthPct, MaxCount: 2,
	}
	got := s.resolveTargetQueryLocked(ctx, q)
	// e1 (30%) before e2 (80%), far enemy excluded by radius, capped at 2
	if len(got) != 2 || got[0] != e1.ID || got[1] != e2.ID {
		t.Fatalf("got %v, want [%d %d]", got, e1.ID, e2.ID)
	}
}
```

(If a `NewGameStateWithSeed` + minimal add-unit test helper does not already exist, first check the existing game tests for the canonical way they spawn units in-test — e.g. `spawnPlayerUnitLocked` or a `_test.go` helper — and use that exact mechanism instead of `addTestUnit`. Do NOT invent a parallel spawn path; match how sibling tests build a `GameState` with units and two players on opposing teams. Report the helper you used.)

- [ ] **Step 2: Run** → FAIL (undefined `resolveTargetQueryLocked`).

- [ ] **Step 3: Implement** `ability_exec_targeting.go`:

- `resolveOriginLocked(ctx *RuntimeAbilityContext, origin TargetOrigin, ref *ContextRef) Vec2` — map each `TargetOrigin` to a position: `caster`→caster pos (via `getUnitByIDLocked(ctx.CasterID)`), `cast_point`→`ctx.CastPoint`, `impact_position`→`ctx.ImpactPosition`, `current_event_position`→`ctx.EventPosition`, `zone_center`→`ctx.ZoneCenter`, `initial_target_position`→initial target's pos, `named_context_value`→resolve `ref` to a position/unit pos. Unknown/`caster` default. Validate resolved units (nil → zero Vec2).
- `candidatePoolLocked(ctx, q) []int` — by `q.Source`: `all_in_scene`→every `s.Units` id; `previous_action_targets`→`ctx.Selected`; `named_context`→`ctx.Named[ref].UnitIDs/UnitID`; `caster`→`[ctx.CasterID]`; `initial_target`→`[ctx.InitialTarget]`.
- `relationMatchesLocked(caster, u *Unit, rels []TargetRelation) bool` — classify `u` vs `caster`: self (`u.ID==caster.ID`), ally (`unitsFriendlyLocked && !self`), enemy (`unitsHostileLocked`); return true if any requested relation matches. `neutral` → for Phase 3 treat as "hostile-and-not-owned" only if an existing neutral predicate exists; otherwise leave a `// TODO(phase-3b): neutral relation` and never match it (document). Empty `rels` = no relation filter (match all).
- `resolveTargetQueryLocked(ctx, q TargetQueryDef) []int`:
  1. `caster := getUnitByIDLocked(ctx.CasterID)`; if nil return nil.
  2. `origin := resolveOriginLocked(...)`.
  3. Build candidate `*Unit` list from `candidatePoolLocked`, resolving each id and dropping nil.
  4. Filter: alive (`u.HP > 0` unless `q.AliveState=="dead"`/`"any"`), relations, radius (`distanceSquared(origin, u) <= r*r` when `q.Radius > 0`; note `q.Radius < 0` (match-attack-range sentinel) resolves to the caster's attack range — reuse `CastRange(q.Radius).Resolve(caster)` semantics or the caster's `AttackRange` when negative), `ExcludeSource` (drop caster id), visibility if the existing splash path filters on it (match `applyAbilitySplashDamageLocked`'s hostile-living-visible predicate for enemy queries — read that function and mirror its filters so query results match what damage would actually hit).
  5. `IncludeInitialTarget`: ensure `ctx.InitialTarget` is present (prepend if valid and passes relation, dedupe).
  6. Order per `q.Ordering` with a **fully-ordered tiebreak on unit.ID** (deterministic): `closest`/`farthest` by dist² to origin; `lowest_health`/`highest_health` by HP; `lowest_health_percentage`/`highest` by `HP/MaxHP` (integer cross-multiply like `castTargetSortByHPPct` in `autocast_selectors.go` — reuse if exported, else mirror); `unit_id` ascending; `random` → deterministic shuffle via `s.rngCombat` (document RNG consumption).
  7. Cap to `q.MaxCount` (>0). Return ids.

- [ ] **Step 4: Run** the new test + full package → PASS. Add 2-3 more sub-tests: ally+self ordering, `includeInitialTarget`, `excludeSource`, empty-relations match-all.

- [ ] **Step 5: Commit** — "feat(abilities): target-query resolver (origin/relations/radius/ordering/count)".

---

### Task 3: Executor loop + select_targets / deal_damage / restore_health

**Files:**
- Modify: `server/internal/game/ability_exec.go` (executor loop)
- Modify: `server/internal/game/ability_program_registry.go` (wire the three `Execute` fns)
- Test: `server/internal/game/ability_exec_run_test.go`

- [ ] **Step 1: Write the failing integration test** — a Greater-Heal-shaped program end to end.

```go
func TestExecuteGreaterHealFlow(t *testing.T) {
	s := NewGameStateWithSeed(testMapConfig(), 1)
	caster := s.addTestUnit("acolyte", "p1", 0, 0, 100, 100)
	a1 := s.addTestUnit("acolyte", "p1", 40, 0, 20, 100)  // most hurt
	a2 := s.addTestUnit("acolyte", "p1", 80, 0, 60, 100)
	prog := &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryUnit, Relations: []TargetRelation{RelSelf, RelAlly}},
		Triggers: []AbilityTriggerDef{{ID: "t", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{
			{ID: "sel", Type: ActionSelectTargets, Outputs: map[string]string{"targets": "heals"},
				Target: &TargetQueryDef{Source: SrcAllInScene, Origin: OriginCaster,
					Relations: []TargetRelation{RelSelf, RelAlly}, Radius: 300,
					Ordering: OrderLowestHealthPct, MaxCount: 3, IncludeInitialTarget: true}},
			{ID: "heal", Type: ActionRestoreHealth,
				Input: map[string]ContextRef{"targets": {Key: "heals"}},
				Config: json.RawMessage(`{"amount":15,"school":"holy"}`)},
		}}},
	}
	tr := &AbilityExecutionTrace{}
	ctx := &RuntimeAbilityContext{CasterID: caster.ID, AbilityID: "greater_heal", Named: map[string]ContextValue{}, Trace: tr}
	s.runProgramTriggersLocked(ctx, prog.Triggers, TriggerOnCastComplete)
	if a1.HP != 35 || a2.HP != 75 {
		t.Fatalf("heals wrong: a1=%d a2=%d", a1.HP, a2.HP)
	}
	if !traceHas(tr, "targets_selected") || !traceHas(tr, "healing_applied") {
		t.Fatalf("missing trace events: %+v", tr.Events)
	}
}
```

(`traceHas` = small test helper scanning `tr.Events` by Type.)

- [ ] **Step 2: Run** → FAIL (undefined `runProgramTriggersLocked`).

- [ ] **Step 3: Implement.**

In `ability_exec.go`:

```go
// runProgramTriggersLocked fires every trigger of type ttype (conditions
// permitting) in order, executing its enabled actions. Caller holds s.mu.
func (s *GameState) runProgramTriggersLocked(ctx *RuntimeAbilityContext, triggers []AbilityTriggerDef, ttype TriggerType) {
	for ti := range triggers {
		trg := &triggers[ti]
		if trg.Type != ttype {
			continue
		}
		if !s.triggerConditionsPassLocked(ctx, trg) { // Phase 3: no conditions authored yet → always true
			ctx.trace("condition_failed", trg.ID, nil)
			continue
		}
		ctx.trace("trigger_fired", trg.ID, map[string]any{"type": string(trg.Type)})
		for ai := range trg.Actions {
			s.executeActionLocked(ctx, &trg.Actions[ai], trg.ID)
		}
	}
}

// executeActionLocked resolves an action's target set and dispatches to its
// registered Execute. Disabled actions and descriptorless/deferred actions
// (nil Execute) are skipped with a trace event. Caller holds s.mu.
func (s *GameState) executeActionLocked(ctx *RuntimeAbilityContext, a *AbilityActionDef, path string) {
	apath := path + ".actions[" + a.ID + "]"
	if !a.IsEnabled() {
		return
	}
	desc, ok := lookupActionDescriptor(a.Type)
	if !ok || desc.Execute == nil {
		ctx.trace("action_skipped", apath, map[string]any{"type": string(a.Type)})
		return
	}
	cfg, err := desc.Decode(a.Config)
	if err != nil {
		ctx.trace("validation_error", apath, map[string]any{"error": err.Error()})
		return
	}
	targets := s.resolveActionTargetsLocked(ctx, a)
	ctx.trace("action_started", apath, map[string]any{"type": string(a.Type), "targets": len(targets)})
	out := desc.Execute(s, ctx, cfg, targets)
	s.bindActionOutputsLocked(ctx, a, out)
	ctx.trace("action_completed", apath, nil)
	// Fire any inline follow-up triggers on this action.
	s.runProgramTriggersLocked(ctx, a.Children, TriggerOnActionComplete)
}

// resolveActionTargetsLocked prepares the target-set for an action: its own
// Target query if present; else an Input "targets" ContextRef; else the most
// recent selection (previous_action_targets).
func (s *GameState) resolveActionTargetsLocked(ctx *RuntimeAbilityContext, a *AbilityActionDef) []int {
	if a.Target != nil {
		return s.resolveTargetQueryLocked(ctx, *a.Target)
	}
	if ref, ok := a.Input["targets"]; ok {
		return ctx.resolveTargetRef(ref)
	}
	return append([]int(nil), ctx.Selected...)
}

// bindActionOutputsLocked stores an action's returned ids under its Outputs
// bindings and updates ctx.Selected (previous_action_targets).
func (s *GameState) bindActionOutputsLocked(ctx *RuntimeAbilityContext, a *AbilityActionDef, out []int) {
	if out != nil {
		ctx.Selected = out
	}
	for _, key := range a.Outputs { // map value is the context key
		ctx.Named[key] = ContextValue{Kind: ctxUnitSet, UnitIDs: append([]int(nil), out...)}
	}
}
```

Add `ctx.resolveTargetRef(ref ContextRef) []int` (reads `Named[ref.Key]`, or the special keys `"selected"/"previous_action_targets"`→`ctx.Selected`, `"initial_target"`→`[ctx.InitialTarget]`), and a stub `triggerConditionsPassLocked` returning true (conditions are a later phase; leave a `// TODO(phase-3b)` and evaluate `len(trg.Conditions)==0` fast-path).

In `ability_program_registry.go`, set the three `Execute` fns:
- `ActionSelectTargets.Execute` — targets are already resolved by `resolveActionTargetsLocked` (from the action's Target query); just emit a trace and return them:
```go
Execute: func(s *GameState, ctx *RuntimeAbilityContext, _ ActionConfig, targets []int) []int {
	ctx.trace("targets_selected", "", map[string]any{"count": len(targets)})
	return targets
},
```
- `ActionDealDamage.Execute`:
```go
Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
	c := cfg.(dealDamageConfig)
	dt := c.Type.OrPhysical()
	hit := make([]int, 0, len(targets))
	for _, id := range targets {
		u := s.getUnitByIDLocked(id)
		if u == nil || u.HP <= 0 {
			continue
		}
		s.applyUnitDamageWithSourceLocked(u, c.Amount, DamageSource{AttackerUnitID: ctx.CasterID, Kind: "ability", DamageType: dt})
		hit = append(hit, id)
		ctx.trace("damage_applied", "", map[string]any{"unit": id, "amount": c.Amount, "type": string(dt)})
	}
	return hit
},
```
(NOTE for Phase 4: legacy parity requires scaling `c.Amount` by the caster's spell-modifier for this school before applying; Phase 3 uses the literal amount. Leave a `// TODO(phase-4): spell-modifier scaling` here.)
- `ActionRestoreHealth.Execute`:
```go
Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
	c := cfg.(restoreHealthConfig)
	caster := s.getUnitByIDLocked(ctx.CasterID)
	if caster == nil {
		return nil
	}
	healed := make([]int, 0, len(targets))
	for _, id := range targets {
		u := s.getUnitByIDLocked(id)
		if u == nil || u.HP <= 0 {
			continue
		}
		s.applyClericHealLocked(caster, u, c.Amount, healMetaPrimaryAbility())
		healed = append(healed, id)
		ctx.trace("healing_applied", "", map[string]any{"unit": id, "amount": c.Amount})
	}
	return healed
},
```

- [ ] **Step 4: Run** the integration test + full package → PASS. Verify determinism: run the test with `-count=3` (stable). Confirm no existing test regressed.

- [ ] **Step 5: Commit** — "feat(abilities): executor loop + select_targets/deal_damage/restore_health".

---

### Task 4: summon_unit, apply_force, apply_status, modify_resource

**Files:**
- Create: `server/internal/game/ability_exec_actions.go` (descriptors + Execute for the four)
- Test: `server/internal/game/ability_exec_actions_test.go`

- [ ] **Step 1: Write failing tests** — one per action asserting the seam effect:
  - `apply_force` on enemies in a radius pulls them (assert `PullRemaining > 0` after execute).
  - `apply_status` slow on an enemy sets its slow track (assert `SlowedRemaining/ColdSlowedRemaining` per school).
  - `summon_unit` spawns N units of the type for the caster's owner (assert unit count delta).
  - `modify_resource` spends/restores caster mana (assert mana delta).

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement** in `ability_exec_actions.go`. Add typed configs + register descriptors (Decode/Validate/Schema/Execute) for each, mirroring Task 3 registry style:

- `summon_unit`: config `{unitType string, count int}`. Execute builds an `AbilityDef{SummonUnitType, SummonCount}` shim and calls `spawnSummonedUnitLocked(caster, shim)` (the existing seam reads those two fields). Validate: `unitType != ""`. (Do NOT re-implement fan-out; the seam owns it.)
- `apply_force`: config `{strength float64, duration float64, radius float64}`. Execute resolves an origin (default caster/`ctx` — use the action's target query origin if present, else caster position) and calls `applyPullInRadiusLocked(caster, ox, oy, radius, strength, duration)`. If the action has an explicit target set instead of a radius, apply pull per-target via `applyPullLocked` (check it exists: `spell_pull.go:29 applyPullLocked(unit, cx, cy, strength, duration)`) — prefer the radius form for parity with arcane_orb. Trace `force_applied`.
- `apply_status`: config `{status string, multiplier float64, duration float64, dps float64}`. Execute per target: `status=="slow"`→`applyProcSlowLocked(id, multiplier, duration, casterSchool)` (school from a config `school`/`type` field or the ability's damage type; for Phase 3 pass a config `school DamageType` so cold→chill routing works); `status=="stun"`→`ApplyStunLocked(id, duration)`; `status=="burn"`→`applyProcBurnLocked(id, dps, duration, ctx.CasterID)`. Unknown status → trace `action_skipped`. Trace `status_applied`.
- `remove_status`: config `{status string}`. Phase 3: only clear the generic tracks it can (`slow`→zero `SlowedRemaining/ColdSlowedRemaining`; `stun`→zero `StunnedRemaining`). Document that arbitrary author statuses are a later phase.
- `modify_resource`: config `{resource string, amount int}` (resource "mana"; negative amount spends, positive restores). Execute on the caster: spend via `spendUnitManaLocked` (guarded) or restore via the mana field clamp (reuse the existing mana add path — read `mana.go` for the restore helper; if none, add mana clamped to max mirroring `spendUnitManaLocked`'s field). Trace `resource_modified`.

Register all in an `init()` in this file (or extend the Task 3 init — keep one init per file to avoid ordering confusion). Add each new `ActionType` to the schema with sensible controls (`number`, `duration`, `enum`, `text`).

- [ ] **Step 4: Run** all four tests + full package → PASS.

- [ ] **Step 5: Commit** — "feat(abilities): summon/force/status/resource action executors".

---

### Task 5: Zone generalization (`AbilityZone`) + create_zone

**Files:**
- Create: `server/internal/game/ability_zone.go` (the entity + tick + create_zone Execute)
- Modify: `server/internal/game/state.go` (add `AbilityZones []*AbilityZone` + `nextAbilityZoneID int` to `GameState`; init to 1; call `tickAbilityZonesLocked(dt)` in `Update` right after `tickGroundHazardsLocked`)
- Test: `server/internal/game/ability_zone_test.go`

- [ ] **Step 1: Write the failing test** — a burn zone ticks damage on enemies in radius over time.

```go
func TestAbilityZoneBurnTicks(t *testing.T) {
	s := NewGameStateWithSeed(testMapConfig(), 1)
	caster := s.addTestUnit("acolyte", "p1", 0, 0, 100, 100)
	enemy := s.addTestUnit("raider", "p2", 30, 0, 100, 100)
	zoneCfg := json.RawMessage(`{
		"name":"Burning Crater","radius":120,"duration":1.0,"tickInterval":0.5,
		"triggers":[{"id":"tick","type":"on_zone_tick","timing":{"tickInterval":0.5},"actions":[
			{"id":"s","type":"select_targets","outputs":{"targets":"hits"},
				"target":{"source":"all_in_scene","origin":"zone_center","radius":120,"relations":["enemy"]}},
			{"id":"d","type":"deal_damage","input":{"targets":{"key":"hits"}},"config":{"amount":12,"type":"fire"}}
		]}]}`)
	ctx := &RuntimeAbilityContext{CasterID: caster.ID, AbilityID: "meteor", CastPoint: Vec2{0, 0}, Named: map[string]ContextValue{}}
	desc, _ := lookupActionDescriptor(ActionCreateZone)
	cfg, _ := desc.Decode(zoneCfg)
	desc.Execute(s, ctx, cfg, nil)
	if len(s.AbilityZones) != 1 {
		t.Fatalf("zone not spawned: %d", len(s.AbilityZones))
	}
	// advance ~1s in 0.1s steps; expect 2 ticks (t=0.5, 1.0) => 24 damage
	for i := 0; i < 10; i++ {
		s.tickAbilityZonesLocked(0.1)
	}
	if enemy.HP != 76 {
		t.Fatalf("enemy HP = %d, want 76 (100 - 2*12)", enemy.HP)
	}
	if len(s.AbilityZones) != 0 {
		t.Fatalf("zone should have expired: %d", len(s.AbilityZones))
	}
}
```

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement.**

`ability_zone.go`:
```go
package game

import "strconv"

// AbilityZone is the composable, tick-driven spatial zone spawned by a create_zone
// action. It generalizes GroundHazard (which stays as the legacy delayed-impact
// primitive): a zone carries a compiled on_zone_tick trigger fired every
// TickInterval for Duration seconds. Server-only, never serialized; damage rides
// the authoritative pipeline via the nested actions' seams.
type AbilityZone struct {
	ID            string
	AbilityID     string
	CasterID      int
	OwnerPlayerID string
	Center        Vec2
	Radius        float64
	Remaining     float64
	TickInterval  float64
	tickTimer     float64
	Triggers      []AbilityTriggerDef // decoded on_zone_tick trigger(s)
}

func abilityZoneIDString(id int) string { return "zone-" + strconv.Itoa(id) }

// spawnAbilityZoneLocked appends a zone; snapshots all values at spawn (live
// tuning cannot retroactively change active zones — matches GroundHazard/Trap).
func (s *GameState) spawnAbilityZoneLocked(z *AbilityZone) {
	id := s.nextAbilityZoneID
	s.nextAbilityZoneID++
	z.ID = abilityZoneIDString(id)
	s.AbilityZones = append(s.AbilityZones, z)
}

// tickAbilityZonesLocked advances every zone by dt, firing its on_zone_tick
// trigger each TickInterval, and culls expired / orphaned-owner zones. Must run
// under s.mu after combat/trap ticks and before drainPendingDeathsLocked (same
// slot discipline as tickGroundHazardsLocked). Filter-into-front-of-slice.
func (s *GameState) tickAbilityZonesLocked(dt float64) {
	if len(s.AbilityZones) == 0 {
		return
	}
	kept := s.AbilityZones[:0]
	for _, z := range s.AbilityZones {
		if _, ok := s.Players[z.OwnerPlayerID]; !ok {
			continue // owner left the match
		}
		z.tickTimer -= dt
		for z.tickTimer <= 0 && z.Remaining > 0 {
			z.tickTimer += z.TickInterval
			ctx := &RuntimeAbilityContext{
				CasterID: z.CasterID, AbilityID: z.AbilityID, OwnerUnitID: z.CasterID,
				ZoneCenter: z.Center, EventPosition: z.Center, Named: map[string]ContextValue{},
			}
			s.runProgramTriggersLocked(ctx, z.Triggers, TriggerOnZoneTick)
		}
		z.Remaining -= dt
		if z.Remaining > 0 {
			kept = append(kept, z)
		}
	}
	s.AbilityZones = kept
}
```

(Guard `TickInterval <= 0` at spawn — validation already forbids it, but defensively clamp to avoid the inner loop spinning; if `TickInterval <= 0`, set it so no tick fires and log once.)

`create_zone` descriptor (register in `ability_exec_actions.go` or here): config type
```go
type createZoneConfig struct {
	Name         string              `json:"name"`
	PositionRef  *ContextRef         `json:"position"`
	Radius       float64             `json:"radius"`
	Duration     float64             `json:"duration"`
	TickInterval float64             `json:"tickInterval"`
	OwnerRef     *ContextRef         `json:"owner"`
	Presentation string              `json:"presentation"`
	Triggers     []AbilityTriggerDef `json:"triggers"`
}
func (createZoneConfig) actionConfig() {}
```
Decode: `json.Unmarshal`. Validate: `radius>0`, `duration>0`, `tickInterval>0`. Execute: resolve center from `config.PositionRef` (via `ctx` — default `ctx.CastPoint`/`ctx.ImpactPosition`), build `&AbilityZone{AbilityID: ctx.AbilityID, CasterID: ctx.CasterID, OwnerPlayerID: <caster owner>, Center, Radius, Remaining: duration, TickInterval, Triggers: config.Triggers}`, call `spawnAbilityZoneLocked`. Trace `zone_created`. (Presentation id is recorded but not rendered in Phase 3 — deferred.) Return nil.

`state.go`: add the two fields near `GroundHazards`/`nextGroundHazardID`, init `nextAbilityZoneID: 1` beside `nextGroundHazardID: 1`, and add `s.tickAbilityZonesLocked(dt)` immediately after the existing `tickGroundHazardsLocked` call in `Update` (keep the same profiled-section discipline — if sections are profiled, add a sibling profiled block; read the surrounding code and match it).

- [ ] **Step 4: Run** the zone test + full package → PASS. Add a determinism sub-test (`-count=3`). Confirm the zone fires exactly `floor` ticks and expires.

- [ ] **Step 5: Commit** — "feat(abilities): AbilityZone entity + create_zone; wire tick into Update".

---

### Task 6: Flow actions — store/filter/wait/conditional/repeat/trigger_event

**Files:**
- Create: `server/internal/game/ability_exec_flow.go`
- Test: `server/internal/game/ability_exec_flow_test.go`

- [ ] **Step 1: Write failing tests:**
  - `store_targets` copies the current target set into a named binding (assert `ctx.Named[key]`).
  - `filter_targets` narrows the incoming set by a relation/alive predicate (assert count).
  - `repeat` runs its child actions N times (assert a counter effect, e.g. N stacks of damage on one target).
  - `conditional` runs child actions only when a simple condition holds (assert branch taken/skipped).
  - `trigger_event` invokes a NamedTrigger by id and executes its actions; a self-referential named trigger is stopped by the recursion guard (assert it does not loop forever — bounded depth).

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement.**
- `store_targets`: config `{as string}`. Execute binds `targets` under `ctx.Named[cfg.As]`, returns targets unchanged.
- `filter_targets`: config carries a `TargetQueryDef`-lite (relations + aliveState + maxCount + ordering) applied to the INCOMING `targets` (source = the passed set, not the scene). Reuse `resolveTargetQueryLocked` machinery by treating the incoming ids as the candidate pool (`Source: previous_action_targets` semantics). Return filtered ids.
- `wait`: config `{seconds float64}`. Phase 3 synchronous execution has no scheduler → `wait` records a `wait` trace event and is otherwise a **no-op** (document that timed waits become real once the tick-scheduler lands in the preview/marker phase). Do NOT block.
- `conditional`: config `{}` + `a.Conditions`. Execute evaluates `a.Conditions` via `evaluateConditionLocked(ctx, cond)` (implement the minimal set: compare a context numeric — e.g. `selected_count` — against a literal with eq/ne/lt/lte/gt/gte; `has`/`not` for presence). If all pass, run `a.Children` (as `TriggerOnActionComplete`? no — run the child ACTIONS directly). Model conditional's branch as `a.Children[0].Actions` (a single inline "then" trigger) OR add a `{then:[...actions]}` in config. DECISION: put the then-branch actions in the FIRST child trigger of the action (`a.Children[0].Actions`) fired only when conditions pass; document it. Emit `condition_failed` trace when skipped.
- `repeat`: config `{count int}`. Execute runs the action's child trigger's actions `count` times (`a.Children[0].Actions`), passing the same ctx (so each iteration sees prior bindings). Guard `count` to a sane max (e.g. 64) with a trace warning if exceeded — prevents authored infinite fan-out.
- `trigger_event`: config `{trigger string}`. Execute looks up `ctx`-associated program's `NamedTriggers[cfg.Trigger]` — but the executor loop currently only has the trigger slice, not the whole program. FIX: thread the `*AbilityProgram` (or its `NamedTriggers` map) onto `RuntimeAbilityContext` as `program *AbilityProgram` (unexported) set at the top-level entry (`runProgramTriggersLocked` callers set `ctx.program`). `trigger_event` then finds the named trigger and executes its actions with `ctx.depth+1`, refusing when `ctx.depth >= maxTriggerDepth` (e.g. 16) → emit `validation_error`/`recursion_guard` trace and stop. This delivers named-trigger invocation + the circular-invocation runtime guard.

Add `ctx.program *AbilityProgram` + set it wherever an execution starts (Task 3's callers, the zone tick, and any test). Update `runProgramTriggersLocked` callers accordingly (small change).

- [ ] **Step 4: Run** all flow tests + full package → PASS. The recursion test must terminate (bounded depth) and assert the guard trace.

- [ ] **Step 5: Commit** — "feat(abilities): flow actions (store/filter/wait/conditional/repeat/trigger_event) + recursion guard".

---

### Task 7: End-to-end authored-program integration + zero-live-impact assertion

**Files:**
- Test: `server/internal/game/ability_exec_integration_test.go`

- [ ] **Step 1: Write the integration tests.**
  1. **Greater Heal (full v2 program):** decode the Phase-2 `greaterHealV2JSON` into `AbilityDef`, build a scene, run `on_cast_complete` via the executor, assert the three lowest-HP allies are healed by 15 (capped at max) and the trace contains `targets_selected` + `healing_applied` ×N. (`play_presentation` is skipped with an `action_skipped` trace — assert that too.)
  2. **Meteor impact→zone (marker bypassed):** decode `meteorV2JSON`; since `on_animation_marker` is deferred, directly fire the presentation's `impact` trigger actions by calling `runProgramTriggersLocked(ctx, meteorImpactTriggerActionsAsSlice, ...)` — OR construct a ctx with `ImpactPosition` set and invoke the impact trigger's actions to assert: enemies in r230 take 140, and a Burning Crater `AbilityZone` is created that, when ticked for 4s, deals 12 fire every 0.5s. This proves the meteor gameplay pipeline end-to-end minus the (deferred) marker firing.
  3. **Zero live impact:** assert that NOTHING in the live cast path calls the executor yet — grep-style test is not feasible, so instead assert behaviorally: cast the LEGACY meteor/greater_heal through the existing `resolveAbilityCastLocked` path (as an existing test already does) and confirm identical results — i.e., the executor's presence has not changed legacy resolution. (If an existing legacy cast test already covers this, reference it in a comment instead of duplicating.)

- [ ] **Step 2–4:** Run → make them pass (they should, given Tasks 1-6). Full package `go test ./internal/game/ -count=1` + `go build ./...` + `go vet` clean.

- [ ] **Step 5: Commit** — "test(abilities): executor end-to-end for Greater Heal + Meteor gameplay pipeline".

---

## Self-review notes

- **Type/name consistency:** `runProgramTriggersLocked`, `executeActionLocked`, `resolveActionTargetsLocked`, `resolveTargetQueryLocked`, `spawnAbilityZoneLocked`, `tickAbilityZonesLocked`, `ActionDescriptor.Execute`, `RuntimeAbilityContext` (+ unexported `program`, `depth`) are used consistently across tasks. `ctx.trace(...)` / `AbilityExecutionTrace.record(...)` are the only trace entry points.
- **Determinism:** every candidate iteration is over `s.Units` slice order with unit.ID tiebreaks; `random` ordering consumes `s.rngCombat` (documented); zone ticks use a fixed `dt` accumulator (no wall-clock). No map iteration drives outcomes.
- **ID discipline:** context holds unit IDs; every use re-resolves via `getUnitByIDLocked` and validates `nil`/`HP`/relation before acting — matching the AI_RULES targeting invariant.
- **Zero live behavior change:** the executor is never called from `resolveAbilityCastLocked`/`tickUnitCastLocked` in Phase 3. The only new `Update` wiring is `tickAbilityZonesLocked`, which is a no-op until a `create_zone` runs — and nothing runs `create_zone` outside tests until Phase 4 reroutes casts. Legacy `GroundHazard` is untouched.
- **Spec coverage (design §3 / §11.3):** executor + registry Execute over seams ✓ · RuntimeAbilityContext ✓ · target queries ✓ · tracing ✓ · Zone generalization (R1) ✓. Golden-vs-legacy comparison and the live-path reroute are **Phase 4** (compiler) — Phase 3 proves the executor against authored programs.
- **Explicit deferrals (not silent):** marker/projectile/status-tick/enter-exit/death/charge triggers; presentation & projectile & move actions; spell-modifier scaling; `neutral` relation; real timed `wait`. Each is a `// TODO(phase-N)` at its site.

## Carry-forwards to Phase 4 (compiler + live wiring) — from the final holistic review (2026-07-15)

Phase 3 landed green + dormant: the executor is exercised only by tests; no cast-path file calls it; the sole `Update` addition (`tickAbilityZonesLocked`) is a no-op until a `create_zone` runs. Determinism verified (slice iteration + unit.ID tiebreaks + seeded `rngCombat`; no map-order or wall-clock). Guards done: `maxTriggerDepth=16` (stack) + `maxExecutionOps=100000` (total work, breaks every multiplier loop). These MUST be handled when Phase 4 wires the executor into `resolveAbilityCastLocked`:

1. **Spell-modifier damage scaling (the load-bearing one).** `deal_damage` applies the raw config `Amount` — no `effectiveSpellLocked` perk/item scaling (`TODO(phase-4)` at `ability_program_registry.go`). The legacy path scales magnitudes; wiring live without this makes program-authored damage diverge from legacy numbers. Apply the caster's spell-modifier for the ability's school to `deal_damage`/`restore_health`/DoT amounts at execute time.
2. **Set `ctx.program` at the cast entry point.** `trigger_event` no-ops (`no_program` trace) when `ctx.program == nil`. The live cast entry must populate it or named triggers silently do nothing (zone ticks intentionally leave it nil).
3. **Guards already in place** — no new work; just confirm the op budget is acceptable for the worst-case authored spell before shipping.
4. **Deferred trigger sources/origins still unwired** — `SrcCurrentEvent`/`SrcSourceObject`, `OriginProjectilePos`/`OriginStatusOwner`/`OriginSummonedUnit`, and animation-marker/projectile-impact/status-tick trigger FIRING are `TODO(phase-3b)` (resolve to nil/caster today). Any wired spell relying on projectile-impact or status-tick triggers needs these threaded into `RuntimeAbilityContext` first. (Meteor's marker-gated impact is proven at the gameplay level in Task 7 by invoking the impact trigger directly; the marker *scheduler* is Phase 6.)
5. **Presentation actions deferred to Phase 6** — the six unregistered actions (play_presentation, change_render_layer, play_sound, camera_shake, launch_projectile, move_unit), `create_zone`'s presentation, and real timed `wait` produce no client-visible output yet. Live wiring gives correct gameplay but no VFX/timing until Phase 6.
6. **Query semantics not yet enforced** — `q.MinCount`, `q.Filters`, `q.RequireLineOfSight` validate on the wire but have no runtime effect; a program assuming LoS/MinCount gating will over-target when wired live.
7. **`create_zone` OwnerRef** — zones are always caster-owned (`TODO(phase-3b)`); multi-owner zones need OwnerRef resolution first.

Minor (pre-preview polish, not blocking): the trace type `"condition_failed"` is emitted for BOTH a trigger-condition failure and a `conditional` else-branch (distinguishable only by path); trace verb tense is mixed (`conditional_taken`/`wait`/`repeat` bare vs `*_applied` past-tense). Normalize before the Phase-6 preview consumer is built.
