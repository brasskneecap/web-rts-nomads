# Composable Abilities — Phase 4 (Legacy Compiler + Guarded Live Wiring) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `compileLegacyAbility` (flat `AbilityDef` → `AbilityProgram`), prove the executor reproduces legacy gameplay for the supported subset (golden equivalence tests, unmodified AND modified casters), add spell-modifier damage scaling to the executor, and wire the executor into the live cast path **only for `SchemaVersion >= 2` abilities** — leaving every shipped (legacy) ability's live behavior byte-identical.

**Architecture:** New `ability_compile.go` (compiler) + additions to the executor for modifier scaling. The live cast path (`resolveAbilityCastLocked` / `resolveAbilityCastAtPointLocked`) gains a single `if def.SchemaVersion >= 2 && def.Program != nil` branch that runs the executor's `on_cast_complete`; the `else` is the untouched legacy path. Since **no catalog ability is `SchemaVersion >= 2`**, live shipped behavior is unchanged; the branch exists so newly-authored composable abilities (Phase 5) actually run.

**Scope decision (important — read before implementing).** A *full* reroute of every legacy ability through the executor is NOT possible yet: projectile delivery, chain/bounce, channel, charge-fire, delayed-impact timing (animation markers), on-target/at-point VFX, arcane_orb's moving zone, and the per-target perk hook (`onPerkAbilityResolvedLocked`) are all deferred (Phase 3b/6). Therefore:
- The compiler produces **structurally correct** programs for ALL 11 abilities (for editor display + future execution), but only the executor-**supported** subset is proven **behavior-identical** by golden tests.
- **Golden equivalence subset (fully executor-runnable today):** `heal`, `greater_heal`, `shatter` (instant point-AoE + slow), `raise_skeleton` (summon). These cover heal / multi-heal / instant-AoE+CC / summon.
- **Deferred-mechanic abilities** (`arcane_bolt`, `fireball`, `chain_lightning`, `arcane_orb`, `siphon_life`, `arcane_missiles`, `meteor`) get **structural** compile tests only; their execution equivalence lands as the deferred executors do. This matches the user's migration guidance ("do not silently rewrite the catalog"; conversion is explicit/opt-in).
- **Live wiring is `SchemaVersion>=2`-gated**, so it touches zero shipped content but makes composable authoring real.

**Tech Stack:** Go. `cd server && go test ./internal/game/ -run <Name> -count=1`. Test scenes via `NewGameStateWithSeed` + the spawn helpers used in Phase 3 (`newProjectileTestState`/`teamCombatUnit`/`setTeam`/`spawnPlayerUnitLocked`). Do NOT run `git commit` — the user handles commits.

**Key existing seams (verified):**
- Cast gates: `beginAbilityCastLocked` (`ability_cast.go:75`) reads `def.ChannelType/IsPassive/TargetsPoint/CanTarget*/WithinCastRange` + `effectiveSpellLocked` for mana/cd/casttime, then at `CastTime<=0` calls `buildCastTargetSetLocked` + `resolveAbilityCastLocked`. `beginAbilityCastAtPointLocked` (`:174`) mirrors it for point casts → `resolveAbilityCastAtPointLocked` (`:232`).
- Resolve (the ONLY branch points): `resolveAbilityCastLocked(caster, def, targets []*Unit)` (`:467`) spends `eff.ManaCost` then loops `resolveAbilityCastOnTargetLocked`. `resolveAbilityCastAtPointLocked(caster, def, eff, x, y)` (`:232`) spends mana then branches orb/hazard/instant-AoE.
- Modifiers: `resolveEffectiveSpell(def, mods)` (`spell_modifier.go:170`) accumulates per-field adds/muls; `SpellModFieldDamage` applied to `def.DamageAmount`→`eff.Damage`. `collectSpellModifiersLocked(caster, def)` (`:224`) = `caster.SpellModifiers` + (empty) perk seam. `SpellModifier.appliesTo(def)` matches on school/tag. `effectiveSpellLocked(caster, def) EffectiveSpell` (`:252`).
- Executor: `runProgramTriggersLocked(ctx, triggers, ttype)`; `RuntimeAbilityContext{CasterID, AbilityID, InitialTarget, CastPoint/ImpactPosition protocol.Vec2, Selected, Named, program *AbilityProgram, ...}`; `TriggerOnCastComplete`. deal_damage Execute currently applies raw config `Amount` (the `TODO(phase-4)` site).
- `buildCastTargetSetLocked(caster, def, primary *Unit) []*Unit` (`autocast_selectors.go:236`) — legacy multi-target selection (asc HP%, cap TargetCount, primary + focus force-included). The compiled heal query must reproduce THIS.

---

### Task 1: `compileLegacyAbility` — the compiler

**Files:**
- Create: `server/internal/game/ability_compile.go`
- Test: `server/internal/game/ability_compile_test.go`

- [ ] **Step 1: Write failing structural tests** for the executor-supported patterns first (greater_heal + shatter + raise_skeleton), asserting the compiled `*AbilityProgram` shape.

```go
func TestCompileGreaterHealStructure(t *testing.T) {
	def, _ := getAbilityDef("greater_heal")
	prog := compileLegacyAbility(def)
	if prog == nil || len(prog.Triggers) != 1 || prog.Triggers[0].Type != TriggerOnCastComplete {
		t.Fatalf("bad trigger shape: %+v", prog)
	}
	acts := prog.Triggers[0].Actions
	// select_targets (self+ally, lowest hp%, maxCount=targetCount, includeInitial) → restore_health(15,holy) → play_presentation(healing_glow)
	if acts[0].Type != ActionSelectTargets || acts[1].Type != ActionRestoreHealth {
		t.Fatalf("bad action sequence: %+v", acts)
	}
	if acts[0].Target == nil || acts[0].Target.MaxCount != def.TargetCount {
		t.Fatalf("select maxCount != targetCount")
	}
	// restore_health amount comes from HealAmount, school from DamageType
	var rh restoreHealthConfig
	_ = json.Unmarshal(acts[1].Config, &rh)
	if rh.Amount != def.HealAmount || rh.School != def.DamageType {
		t.Fatalf("heal cfg mismatch: %+v", rh)
	}
	if err := validateAbilityProgram(prog); errsOf(err) { t.Fatalf("compiled program invalid") }
}
```
(Use whatever validate signature exists — `validateAbilityProgram` returns `[]ValidationIssue`; assert no `error`-severity issue. Write a small `hasError([]ValidationIssue) bool` helper.)

Add similar structural tests: `TestCompileShatterStructure` (select_targets origin=cast_point radius=Radius relations=enemy → deal_damage(DamageAmount, DamageType) + apply_status(slow, SlowMultiplier, SlowDurationSeconds, school=DamageType) + play_presentation(effectAtPoint)); `TestCompileRaiseSkeletonStructure` (summon_unit unitType=SummonUnitType count=SummonCount); `TestCompileBasicHealStructure` (heal: targetCount 1 → select_targets source=initial_target → restore_health).

- [ ] **Step 2: Run** → FAIL (`compileLegacyAbility` undefined).

- [ ] **Step 3: Implement** `compileLegacyAbility(def AbilityDef) *AbilityProgram`. It inspects the flat fields and emits the program. Build the entry from the legacy targeting flags:
  - `entry.Type`: `TargetsPoint`→`EntryGroundPoint`; else if `CanTargetSelf||CanTargetAllies||CanTargetEnemies`→`EntryUnit`; else if `IsPassive()`→`EntryPassive`; else `EntryNoTarget`. `entry.Relations` from CanTarget* flags (self/ally/enemy). `entry.Range = def.CastRange`.
  - Primary `on_cast_complete` trigger whose actions are built per mechanic. Implement helper builders and compose in this precedence (most-specific first, matching `describeAbility`/resolve precedence):
    - `ChannelType != ""` → a single `custom`-type action carrying the legacy fields (channel is not composable yet; structural placeholder). Document.
    - `IsChargeFirePassive()` → `custom` action placeholder (charge-fire deferred).
    - `SummonUnitType != ""` → `summon_unit{unitType, count}`.
    - `HealAmount > 0` → heal branch: if `TargetCount <= 1` → `select_targets{source: initial_target}`; else `select_targets{source: all_in_scene, origin: caster, relations: [self,ally per flags], radius: <see note>, ordering: lowest_health_percentage, maxCount: TargetCount, includeInitialTarget: true}`. Then `restore_health{amount: HealAmount, school: DamageType}`. Then if `EffectOnTarget != ""` a `play_presentation{asset: EffectOnTarget, attach: previous targets}` (deferred-exec, structural).
    - Offensive (`DamageAmount > 0` or `DamagePerSecond > 0`):
      - `Projectile != ""` → `launch_projectile{projectile: Projectile, ...}` carrying the damage on impact (structural; launch_projectile deferred-exec). Include `ChainCount`/`BounceRange`/`BounceDamageFalloff` on the projectile action's config when `ChainCount>0`.
      - else if `ImpactDelaySeconds > 0` (meteor) → a `play_presentation{asset: EffectAtPoint, scale: EffectScale}` with an `on_animation_marker "impact"` child trigger doing `select_targets{origin: impact_position, radius: Radius, relations:[enemy]}` → `deal_damage{DamageAmount, DamageType}` → (if `BurnDurationSeconds>0`) `create_zone{radius: BurnRadius, duration: BurnDurationSeconds, tickInterval: BurnTickIntervalSeconds, presentation: BurnEffectAtPoint, triggers:[on_zone_tick → select(zone_center, BurnRadius, enemy)+deal_damage(BurnDamagePerTick, DamageType)]}`. (Matches design §5.2; execution of the marker gate is deferred — structural.)
      - else if `TargetsPoint && Radius > 0` (shatter) → `select_targets{origin: cast_point, radius: Radius, relations:[enemy]}` → `deal_damage{DamageAmount, DamageType}` (+ slow below) + `play_presentation{EffectAtPoint, EffectScale}`.
      - else (instant single-target) → `deal_damage{source: initial_target, amount: DamageAmount, type: DamageType}` (via `Input{targets: initial_target}` or a `select_targets{source: initial_target}` preceding).
      - Slow: if `SlowMultiplier > 0 && SlowMultiplier < 1` → append `apply_status{status: "slow", multiplier: SlowMultiplier, duration: SlowDurationSeconds, school: DamageType}` on the damaged set.
      - Pull: if `PullStrength > 0 && Radius > 0 && Duration > 0` (unit-target pull) → `apply_force{strength: PullStrength, duration: Duration}` on the set. (arcane_orb's point-pull is `custom`/deferred since it's a moving zone.)
    - DamagePerSecond without impact (arcane_orb vortex) → `custom` placeholder (moving zone deferred).

  RADIUS NOTE for the heal multi-select: the compiled query must reproduce `buildCastTargetSetLocked` (asc HP%, cap TargetCount, primary force-included). Task 4's golden test is the arbiter — tune the query (radius source, ordering, includeInitialTarget) until greater_heal's executor-selected set equals `buildCastTargetSetLocked`'s. If a legacy-only nuance (focus target) can't be reproduced by a `TargetQueryDef`, document the narrowed equivalence ("equal when no focus target set") rather than forcing it.

  Emit `SchemaVersion: 2` on the returned program's owning def? NO — `compileLegacyAbility` returns only the `*AbilityProgram`; it does not mutate the def or set schemaVersion. Callers (golden tests, future convert) decide.

  Config encoding: build each action's `Config` via `json.Marshal` of the typed config struct (`restoreHealthConfig`, `dealDamageConfig`, `applyStatusConfig`, `summonUnitConfig`, `createZoneConfig`, etc.) so the compiled program round-trips and the executor decodes it. Give every action a stable `ID` (e.g. `"sel"`, `"dmg"`, `"heal"`, `"slow"`, `"zone"`, `"burn_tick"`).

- [ ] **Step 4: Run** the structural tests → PASS. `go build ./... && go vet && gofmt -l`. The compiler is pure (no lock needed — it only reads a def and builds a struct; mark it as such, no `Locked` suffix).

- [ ] **Step 5: Commit** — "feat(abilities): compileLegacyAbility (flat def → composable program)".

---

### Task 2: Full-catalog compile smoke + validation

**Files:**
- Test: `server/internal/game/ability_compile_catalog_test.go`

- [ ] **Step 1: Write the test.** For EVERY ability in `ListAbilityDefs()` (all 11): `prog := compileLegacyAbility(def)`; assert `prog != nil`, `validateAbilityProgram(prog)` has no error-severity issue, and every action type used is in `allActionTypes`. Additionally, assert a per-ability expectation table: map ability id → the set of top-level action types the compiler should emit (e.g. `heal`→{select_targets, restore_health}, `fireball`→{launch_projectile}, `raise_skeleton`→{summon_unit}, `meteor`→{play_presentation}, `siphon_life`→{custom}, `arcane_missiles`→{custom}, `arcane_orb`→{custom}). Derive the table from the catalog fields, don't hardcode balance numbers — assert action-type *shape*, not tunable values. Classify each ability `executorRunnable` (heal/greater_heal/shatter/raise_skeleton) vs `deferredMechanic` and assert the classification via a documented predicate (e.g. "runnable iff compiled program uses only actions with a registered Execute").

- [ ] **Step 2–4:** Run → make pass. This locks the compile shape for all 11 and guarantees none is un-compilable. Full package green.

- [ ] **Step 5: Commit** — "test(abilities): full-catalog compile + validation smoke".

---

### Task 3: Spell-modifier damage scaling in the executor

**Files:**
- Modify: `server/internal/game/spell_modifier.go` (extract a reusable damage-scaling helper) OR add to `ability_exec.go`
- Modify: `server/internal/game/ability_exec.go` (`abilityDef *AbilityDef` on ctx)
- Modify: `server/internal/game/ability_program_registry.go` (deal_damage scales)
- Test: `server/internal/game/ability_exec_modifier_test.go`

- [ ] **Step 1: Write the failing parity test.** A caster with a `+50% damage` (or `+10 flat`) `SpellModifier` matching the ability's school. Compare: (a) legacy `effectiveSpellLocked(caster, def).Damage` for a def with `DamageAmount = base`; (b) the executor's scaled amount for a `deal_damage{amount: base}` with `ctx.abilityDef = &def`. Assert they're EQUAL.

```go
func TestExecutorDamageScalingMatchesLegacy(t *testing.T) {
	s := /* state + caster */
	def := AbilityDef{ID: "x", DamageType: DamageFire, DamageAmount: 100}
	caster.SpellModifiers = []SpellModifier{ /* +50% fire damage, multiply, appliesTo fire */ }
	legacy := s.effectiveSpellLocked(caster, def).Damage // == 150
	got := s.effectiveAbilityDamageLocked(caster, def, 100)
	if got != legacy {
		t.Fatalf("scaling mismatch: executor=%d legacy=%d", got, legacy)
	}
}
```
(Build the SpellModifier exactly as an existing spell_modifier test does — read `spell_modifier_test.go` for the struct shape + how `appliesTo` matches a school.)

- [ ] **Step 2: Run** → FAIL (`effectiveAbilityDamageLocked` undefined).

- [ ] **Step 3: Implement.**
  - Add `func (s *GameState) effectiveAbilityDamageLocked(caster *Unit, def AbilityDef, base int) int` in `spell_modifier.go`. It must produce the SAME number `resolveEffectiveSpell` would for `SpellModFieldDamage` applied to `base`: collect `collectSpellModifiersLocked(caster, def)`, accumulate adds/muls for `SpellModFieldDamage` over modifiers where `m.appliesTo(def)`, apply `round(max(0, base+adds)*mul)`. REUSE the accumulation: the cleanest DRY move is to refactor `resolveEffectiveSpell`'s inner per-field `apply` into a small reusable `applySpellModField(mods, field, base) float64` that both `resolveEffectiveSpell` and `effectiveAbilityDamageLocked` call — do this refactor behavior-preservingly and re-run ALL existing spell_modifier tests to confirm identical results.
  - Add unexported `abilityDef *AbilityDef` to `RuntimeAbilityContext` (doc: "set at top-level cast resolution so deal_damage can apply the caster's spell-modifiers for this ability's school/tags; nil ⇒ no scaling, e.g. zone-tick/DoT which legacy applies raw").
  - In deal_damage Execute (`ability_program_registry.go`), replace the raw amount + the `TODO(phase-4)` with: `amount := c.Amount; if ctx.abilityDef != nil { amount = s.effectiveAbilityDamageLocked(caster, *ctx.abilityDef, c.Amount) }` (resolve `caster := s.getUnitByIDLocked(ctx.CasterID)` first; if nil, use raw). Apply `amount`. Keep the rest identical.
  - IMPORTANT PARITY NOTE (document at the call site): legacy burn/DoT (`BurnDamagePerTick`) is applied RAW (not modifier-scaled). Zone-tick ctx has `abilityDef == nil` (Task 5 / zone spawn does not set it), so zone burn stays raw — matching legacy. Only the top-level cast damage (where ctx.abilityDef is set) scales.

- [ ] **Step 4: Run** parity test + ALL spell_modifier tests + full package → PASS. Confirm the refactor didn't change any existing effective-spell number.

- [ ] **Step 5: Commit** — "feat(abilities): executor damage respects caster spell-modifiers (legacy parity)".

---

### Task 4: Golden equivalence — compiled-executor-run == legacy-run

**Files:**
- Test: `server/internal/game/ability_compile_golden_test.go`

- [ ] **Step 1: Write the golden tests.** For each of `heal`, `greater_heal`, `shatter`, `raise_skeleton`, run BOTH paths on two identical scenes (same seed, same unit layout) and assert identical gameplay outcomes:
  - **Legacy run:** cast via the real path — `resolveAbilityCastLocked(caster, def, buildCastTargetSetLocked(caster, def, primary))` for unit-target (heal/greater_heal/raise_skeleton), or `resolveAbilityCastAtPointLocked(caster, def, eff, x, y)` for shatter (point). (Match how the real cast resolves; you may call the resolve function directly with the same inputs `beginAbilityCastLocked` would produce — mana already handled inside resolve.)
  - **Executor run:** `prog := compileLegacyAbility(def)`; build `ctx := &RuntimeAbilityContext{CasterID: caster.ID, AbilityID: def.ID, InitialTarget: primary.ID, CastPoint: {x,y} (for point), abilityDef: &def, program: prog, Named: map}`; spend mana the same way (`s.spendUnitManaLocked(caster, eff.ManaCost)`); `s.runProgramTriggersLocked(ctx, prog.Triggers, TriggerOnCastComplete)`.
  - **Assert equivalence:** the two scenes end with identical per-unit HP, identical mana spent, identical slow state (shatter: `SlowedRemaining/ColdSlowedRemaining`), identical summon count + owner (raise_skeleton). Compare by unit position/index across the two scenes.
  - Run each with **(a) an UNMODIFIED caster** and **(b) a caster with a matching-school +damage `SpellModifier`** (for shatter — proves Task 3 scaling parity; heal modifiers already flow through `applyClericHealLocked` in both paths).
  - If greater_heal's selected SET diverges from `buildCastTargetSetLocked`, tune the Task 1 compiled query until it matches; if a genuine semantic gap remains (focus targeting), narrow the asserted scenario (no focus target) and document it in the test + Task 1.

- [ ] **Step 2–4:** Run → make pass (tuning the compiler query in Task 1's file as needed — that's expected iteration, re-run Task 1/2 tests after any compiler change). `-count=3` for determinism. Full package + `go test ./...` green.

- [ ] **Step 5: Commit** — "test(abilities): golden equivalence (compiled executor == legacy) for heal/greater_heal/shatter/summon".

---

### Task 5: Guarded live wiring (`SchemaVersion>=2` → executor)

**Files:**
- Modify: `server/internal/game/ability_cast.go` (branch in `resolveAbilityCastLocked` + `resolveAbilityCastAtPointLocked`)
- Test: `server/internal/game/ability_cast_program_test.go`

- [ ] **Step 1: Write failing tests** that cast an authored `SchemaVersion:2` ability through the REAL cast entry point and assert the executor ran, plus a legacy-unchanged regression:

```go
func TestLiveCast_SchemaV2_UnitHeal_RoutesToExecutor(t *testing.T) {
	// Register a SchemaVersion:2 heal via the runtime overlay (SaveAbilityDef) or
	// by injecting into the def lookup the way other cast tests set up an ability.
	// It has top-level targeting fields (CanTargetAllies=true, CastRange, ManaCost,
	// CastTime=0) AND a Program (on_cast_complete → select_targets(initial_target)
	// → restore_health(15,holy)). Grant it to the caster.
	// Cast via beginAbilityCastLocked(caster, id, allyTarget).
	// Assert the ally was healed by 15 (executor ran) and mana was spent.
}

func TestLiveCast_LegacyAbility_UnaffectedByWiring(t *testing.T) {
	// Cast the catalog legacy "heal" (SchemaVersion 0, Program nil) via the real
	// path; assert it heals exactly as before (legacy resolve path taken). This
	// guards that the new branch only triggers for SchemaVersion>=2.
}
```
(Read an existing cast test — e.g. `TestHeal_RestoresHPAndDeductsMana` in `ability_cast_test.go` — for the canonical way to register/grant an ability + a caster with mana, and reuse that harness. For the v2 ability, set it up the same way but with `SchemaVersion:2` + `Program`.)

- [ ] **Step 2: Run** → FAIL (executor not wired; the v2 heal does nothing).

- [ ] **Step 3: Implement the branch.**
  - In `resolveAbilityCastLocked(caster, def, targets)` — at the TOP, after the `caster==nil` guard, add:
    ```go
    if def.SchemaVersion >= 2 && def.Program != nil {
        s.resolveAbilityProgramCastLocked(caster, def, /* primary */ firstNonNil(targets), protocol.Vec2{})
        return
    }
    ```
    (Keep the existing legacy body unchanged in the `else` flow — i.e. everything after the branch runs only for legacy.)
  - In `resolveAbilityCastAtPointLocked(caster, def, eff, x, y)` — after the `caster==nil` guard, add the same branch with `resolveAbilityProgramCastLocked(caster, def, nil, protocol.Vec2{X:x, Y:y})` and `return` before the orb/hazard/instant branches.
  - Add `resolveAbilityProgramCastLocked(caster *Unit, def AbilityDef, primary *Unit, point protocol.Vec2)` (new, in `ability_cast.go` or `ability_exec.go`): resolve `eff := s.effectiveSpellLocked(caster, def)`; `if !s.spendUnitManaLocked(caster, eff.ManaCost) { caster.LastCastFailure = castFailNotEnoughMana; return }`; build `ctx := &RuntimeAbilityContext{CasterID: caster.ID, AbilityID: def.ID, program: def.Program, abilityDef: &def, Named: map[string]ContextValue{}, CastPoint: point, ImpactPosition: point}`; set `ctx.InitialTarget = primary.ID` when primary != nil; `s.runProgramTriggersLocked(ctx, def.Program.Triggers, TriggerOnCastComplete)`. (Mana parity: legacy `resolveAbilityCastLocked`/`resolveAbilityCastAtPointLocked` both spend `eff.ManaCost` once — match that.)
  - Do NOT change `beginAbilityCastLocked`/`beginAbilityCastAtPointLocked` — a v2 ability keeps top-level `CanTarget*`/`TargetsPoint`/`CastRange`/`ManaCost`/`CastTime`, so the existing gates work unchanged. (Document this contract: "a SchemaVersion>=2 ability MUST still populate the top-level cast-setup + targeting fields; the Program owns only the resolve-time behavior." Phase 5's editor writes both.)

- [ ] **Step 4: Run** the two new tests → PASS (v2 heal heals via executor; legacy heal unchanged). Then FULL regression — this edits the live resolve path, so every existing cast test must still pass: `go build ./... && go vet ./internal/game/ && go test ./internal/game/ -count=1 && go test ./... -count=1`. Any legacy cast test regression ⇒ BLOCKED (the branch must be inert for SchemaVersion<2). `gofmt -l`.

- [ ] **Step 5: Commit** — "feat(abilities): route SchemaVersion>=2 casts through the executor (legacy path untouched)".

---

## Self-review notes

- **Legacy live behavior is byte-identical:** the only live-path change (Task 5) is a `SchemaVersion >= 2` branch; no catalog ability sets that, so every shipped cast takes the unchanged legacy path. Task 5's regression test + full-suite green enforce it. (Phase-3 dormancy is now relaxed ONLY for authored v2 abilities.)
- **Type/name consistency:** `compileLegacyAbility`, `effectiveAbilityDamageLocked`, `applySpellModField`, `resolveAbilityProgramCastLocked`, `ctx.abilityDef` used consistently across tasks. deal_damage's scaling reads `ctx.abilityDef` (set only at top-level cast, nil for zone ticks → burn stays raw = legacy parity).
- **Equivalence honesty:** golden tests cover the 4 executor-runnable abilities (unmodified + modified). The other 7 get structural compile tests only; their execution equivalence is gated on the deferred executors (projectile/chain/channel/charge/marker/orb) — each noted as `custom`/deferred in the compiler with a `TODO(phase-N)`.
- **Modifier parity:** executor damage now scales identically to legacy `eff.Damage` (Task 3 parity test); heal scaling already flows through `applyClericHealLocked` in both paths; burn/DoT stays raw in both.
- **Spec coverage (design §4 / §11.4):** compiler for all patterns ✓ (T1) · meteor+greater_heal compile ✓ (T1/T2) · route resolve through executor ✓ but SchemaVersion-gated (T5, scoped per the deferred-executor reality) · behavior-identical verification ✓ for the supported subset (T4).
- **Deferred to later phases (not silent):** full legacy reroute (needs projectile/chain/channel/charge/marker/orb executors + perk-hook action + presentation); the editor-facing compiled-program display + `POST /abilities/{id}/convert` endpoint (Phase 5); marker-gated meteor execution (Phase 6). Each is a `TODO` at its site.

## Carry-forwards to Phase 5 (editor) + beyond — from the final holistic review (2026-07-15)

Phase 4 landed green + legacy-byte-identical: the only live-path change is a `SchemaVersion>=2 && Program!=nil` branch at the top of the two resolve functions; no catalog ability is v2 (grep empty), so all shipped casts are unchanged. Compiler is pure; damage-modifier scaling shares the legacy `applySpellModField` fold; golden equivalence proven for heal/greater_heal/shatter/raise_skeleton (unmodified + modified). Carry-forwards:

1. **A v2 ability MUST still populate the top-level cast-setup + targeting fields** (`castRange`, `canTarget*`/`targetsPoint`, `manaCost`, `castTime`, `type`) — the `begin*` gates read them directly and are untouched. **Phase 5's editor must write BOTH** the top-level fields and the `Program`, or a v2 ability won't initiate/route. (Design §6: the Program owns only resolve-time behavior.)
2. **greater_heal's `buildCastTargetSetLocked` focus-target / full-HP-exclusion behavior has no `TargetQueryDef` equivalent** — a converted greater_heal selects differently in the focus/full-HP-ally case. Documented in the golden test's NARROWED-EQUIVALENCE CAVEAT. Needs a query-language extension (e.g. an `excludeFullHealth`/`focusAware` filter) before greater_heal is faithfully convertible.
3. **The executor fires neither `onPerkAbilityResolvedLocked` (per-target perk hook) nor VFX** (`play_presentation`/`effectOnTarget` has no executor → `action_skipped`). A converted ability loses perk-on-resolve triggers (battle_prayer, etc.) + on-target VFX until those land. NOTE: `restore_health` DOES route through `applyClericHealLocked`, so heal-output scaling/overheal/Judgement dispatch is preserved — only the generic perk fan-out + VFX are missing.
4. **Deferred-mechanic abilities compile structurally but are executor no-ops** (channel/charge/orb→`custom`; projectile/chain→`launch_projectile` no descriptor; meteor→`play_presentation` + a marker trigger that never fires). `TestCompileExecutorRunnableClassification` is the guardrail. **Phase 5 must surface "display-only vs runnable"** so the editor doesn't imply a converted meteor/fireball works live.
5. **(minor) Empty-target coupling:** the v2 unit-target branch sits after `len(targets)==0` and passes only `targets[0]` as `InitialTarget`; the executor never runs for a unit-target v2 ability whose legacy `buildCastTargetSetLocked` returns empty. Holds in practice (primary is force-included); note when the editor authors unusual targeting.
6. **(minor) Paired-obligation config structs:** `playPresentationOnTargetConfig`/`playPresentationAtPointConfig`/`launchProjectileConfig` are compiler-only round-trip structs with NO decoder. When Phase 6 adds `play_presentation`/`launch_projectile` executors, their decoders MUST match these exact JSON shapes or compiled output mis-decodes.
