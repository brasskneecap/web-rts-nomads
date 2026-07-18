# Composable Beams Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a first-class `launch_beam` composable action (+ `on_beam_impact` / `on_beam_tick` triggers) and retire the last two delivery shims — chain_lightning (faked `launch_projectile`+`chainCount`) and siphon_life (baked `channelSpec`) — so both run authored programs with byte-identical behavior.

**Architecture:** `launch_beam` mirrors `launch_projectile`: spawn + deliver-at-impact, payload composed in a nested `on_beam_impact` trigger. The existing `Beam` entity gains an `ImpactActions`/op-budget seam copied verbatim from `Projectile.ImpactActions`. chain_lightning compiles to N literally-nested beams (compiler unroll, per-hop falloff baked, visited-set exclusion). siphon_life keeps its channel *lifecycle* and perk hooks in Go; only its per-tick **damage** becomes an authored `on_beam_tick`/`deal_damage`, with `deal_damage` becoming the single damage authority the perks read back.

**Tech Stack:** Go (server/internal/game), TypeScript/Vue 3 (client mirror + editor schema). Golden equivalence tests are the parity oracle throughout.

**Spec:** `docs/superpowers/specs/2026-07-17-composable-beams-design.md`

> **STATUS: COMPLETE (2026-07-17).** All tasks landed; full suite green both sides (server all packages; client vue-tsc clean + 828 vitest pass, 3 pre-existing ListEditorPanel failures unrelated). Notable deviations from this plan, all recorded in the `project_composable_abilities` memory: chain resolves SEQUENTIALLY (user-accepted gameplay change); added `dealDamageConfig.FlatOffset` for multiplicative-mod parity; `fireAbilityChainLocked` KEPT as the golden test's legacy oracle (not deleted); the action-icon sub-task was dropped (that icon system isn't keyed by ActionType — flow editor uses text labels); two latent `tickBeamsLocked` bugs were found+fixed. Uncommitted, per the user's git rule.

---

## File Structure

**Server — new:**
- `server/internal/game/ability_exec_beam.go` — `launchBeamConfig`, its `registerAction`, `on_beam_impact` unwrap in Execute (mirrors `ability_exec_projectile.go`).
- `server/internal/game/ability_exec_beam_test.go` — unit tests for a single non-chaining beam.
- `server/internal/game/ability_compile_golden_beam_chain_test.go` — chain_lightning parity (may extend the existing chain golden test instead).

**Server — modified:**
- `ability_program.go` — new `TriggerOnBeamImpact`/`TriggerOnBeamTick`/`ActionLaunchBeam` consts.
- `ability_program_validate.go` — `allActionTypes`, the cast-time action list (line 13), the config.triggers recursion switch (line 140-159).
- `ability_program_enums.go` — hand-maintained `triggerTypes` list (line 19-25).
- `ability_program_registry.go` — `deal_damage` records applied amount; (schema for `launch_beam` lives in `ability_exec_beam.go`).
- `beam.go` — `Beam` gains `ImpactActions`/`ImpactOpsBudget`/`CasterID`/`AbilityIDForCtx`/`ImpactDamageMultiplier`; `spawnBeamWithImpactActionsLocked`; `tickBeamsLocked` runs impact actions.
- `ability_exec.go` — `RuntimeAbilityContext.lastAppliedDamage int` field.
- `ability_channel.go` — the tick loop fires `on_beam_tick` and reads back `lastAppliedDamage`; `channelSpecFor` unchanged.
- `ability_exec_channel.go` — `channel_beam` Execute spawns the beam via the new seam and stores the tick trigger (or a sibling change — see Task 3).
- `ability_compile.go` — chain_lightning unroll compiler; retire the `ChainCount>0` branch in `compileProjectileActions`; siphon_life tick-trigger compile.
- `ability_exec_projectile.go` — delete `executeChainLightningShimLocked` + the `ChainCount>0` dispatch + the chain fields on `launchProjectileConfig`.
- `ability_cast.go` — delete `fireAbilityChainLocked` + the `case eff.ChainCount>0` dispatch (only after Task 0 confirms it is unreferenced elsewhere).
- `ability_describe.go` — beam-aware description (`ActionLaunchBeam`, `on_beam_impact`).
- `catalog/abilities/chain_lightning/*.json`, `catalog/abilities/siphon_life/siphon_life.json` — regenerated authored programs.
- `catalog/action-icons.json` — `launch_beam` icon.

**Server — test maps that fail on drift (must update):**
- `ability_program_validate_test.go` (`TestKnownActionTypesCoversAllConsts`), `ability_program_schema_test.go`, `ability_program_schema_targeting_test.go` (lines 30, 120, 146), `ability_compile_catalog_test.go` (lines 142-146, 283, 359-360), `ability_program_enums_test.go`.

**Client — modified:**
- `game/abilities/program/abilityProgram.ts` — `TriggerType`/`ActionType` unions gain the new values.
- `components/ability-builder/programTree.ts` — add `launch_beam` to `CONFIG_TRIGGER_ACTION_TYPES`.
- `game/abilities/program/programSchema.ts` (+ `.test.ts`) — remove `chainCount`-shim assumptions if any; add beam action.
- `components/ActionIcon.vue` — `launch_beam` icon mapping.

---

## Task 0: Recon spikes (no production code) — ✅ RESOLVED 2026-07-17

**Findings (grounding all later tasks):**

- **Q1 — bounce arithmetic (chain_lightning).** The chain runs in
  `fireProcBeamLocked` (projectile.go:845-898) + `nearestChainBounceTargetLocked`
  (perks_siphoner.go:657-682), NOT `executeProcEffectLocked` (which only routes
  beam-vs-projectile). Per-hop damage is exact INTEGER `p.Damage -
  p.BounceDamageFalloff*hop` (hop 1..BounceCount, computed off the ORIGINAL
  damage, no rounding), and the chain STOPS when `dmg <= 0` or no target found.
  Primary hit = full damage. Visited set is seeded `{primary target, caster}`
  and each chosen hop is added. Next target = nearest hostile within
  `BounceRange` of the PREVIOUS victim's position (not the caster), tie-break
  lowest unit ID. Beam origin per hop = previous victim.
- **Q2 — visited-set exclusion is NOT expressible today.** `filterTargetsConfig`
  has only Relations/AliveState/MaxCount/Ordering (ability_exec_flow.go:116-121);
  `TargetQueryDef.Filters` is an explicit no-op (ability_exec_targeting.go:377
  `TODO(phase-3b)`); the only exclusions are `ExcludeSource` (drops caster) and
  `ExcludeCurrentEvent` (drops the trigger unit), both single-unit
  (ability_exec_targeting.go:261-272). `store_targets` BINDS (replaces) a named
  `ctxUnitSet` (ability_exec_flow.go:193-201) — no append. **Genuine chaining
  therefore needs TWO small, reusable additions (Task 2 Step 0):** (a) an
  exclude-named-set field on `filterTargetsConfig`/`TargetQueryDef` that drops
  IDs present in a named `ctxUnitSet` (mirror how `SrcNamedContext` already
  READS one, ability_exec_targeting.go:100-117); (b) an append/merge mode on
  `store_targets` so the visited set accumulates across hops.
- **Q3 — siphon fold is clean.** Legacy tick = `int(math.Round(DamagePerTick *
  mods.DamageMult))` (ability_channel.go:384), NO `effectiveAbilityDamageLocked`
  call. `effectiveAbilityDamageLocked` (spell_modifier.go:299-302) is IDENTITY
  for siphon_life today (no shipped spell mod targets it; `perkSpellModifiers`
  returns nil). So the authored `deal_damage` reproduces the legacy number iff
  the loop sets `ctx.damageEffectivenessMultiplier = mods.DamageMult`
  (`round(round(DamagePerTick) * mods.DamageMult) == round(DamagePerTick *
  mods.DamageMult)` since DamagePerTick is integer). `mods.DamageMult` is set by
  `soul_leech`/`beam_mastery` only, applied ONCE at the base line — no
  double-fold. Heal + all perks derive from the scaled `tickDamage`.
- **Q4 — `fireAbilityChainLocked` deletion.** Three references: the legacy
  `case eff.ChainCount > 0` dispatch (ability_cast.go:742-747, dead for shipped
  v2 content but a live v1 path), `executeChainLightningShimLocked`
  (ability_exec_projectile.go:380), and the unit test
  (ability_chain_bounce_attribution_test.go:205). Equipment `lightning_chain`
  procs call `executeProcEffectLocked` DIRECTLY (state_combat.go:528,553) and do
  NOT route through it. Safe to delete ONLY after removing the legacy dispatch
  AND rewriting the attribution test to exercise the new launch_beam chain path
  (the "bounce kill fires on_unit_death" assertion stays valid — keep it,
  re-pointed). The bounce mechanic itself (`fireProcBeamLocked`) stays — shared
  with equipment procs + chain_siphon.

**Files:** none created.

- [ ] **Step 1: Confirm `fireAbilityChainLocked` is chain_lightning-only.**

Run:
```bash
cd "c:/Personal Dev/webrts" && rg -n "fireAbilityChainLocked" server/
```
Expected: definition at `ability_cast.go:794`, one dispatch at `ability_cast.go:747` (`case eff.ChainCount > 0`), and one call from `executeChainLightningShimLocked` (`ability_exec_projectile.go:380`). Equipment procs must NOT appear (they call `executeProcEffectLocked` directly — `state_combat.go:528,553`). Record: safe to delete once both dispatches are removed.

- [ ] **Step 2: Determine whether legacy chain bounce forbids re-hitting a victim.**

Read `proc_effects.go` `executeProcEffectLocked` and its bounce-target selection. Find whether it tracks a visited set (a victim can't be hit twice) or picks nearest-in-range unconditionally.

Run:
```bash
cd "c:/Personal Dev/webrts" && rg -n "bounce|Bounce|visited|alreadyHit|excludeIDs" server/internal/game/proc_effects.go
```
Record the exact selection rule. **This dictates whether the authored chain needs a visited-set (`store_targets`+exclude) or a plain nearest-in-range query.** Also record the exact per-hop falloff arithmetic (integer rounding) so the compiler can reproduce it byte-for-byte.

- [ ] **Step 3: Determine whether an authored query can exclude a named "already-hit" set.**

Read `ability_exec_targeting.go` `candidatePoolIDsLocked` and the filter application. Check whether `TargetQueryDef.Filters` or a `store_targets`+`filter_targets` composition can exclude unit IDs already in a named context.

Run:
```bash
cd "c:/Personal Dev/webrts" && rg -n "ExcludeCurrentEvent|Filters|filter_targets|Named\[" server/internal/game/ability_exec_targeting.go server/internal/game/ability_exec_actions.go
```
Record: does the visited-set exclusion already exist, or is a small `filter_targets` extension (exclude-named-context) needed? If needed, it becomes an added step in Task 2.

- [ ] **Step 4: Determine whether legacy siphon_life channel damage is spell-mod-folded.**

Read `ability_channel.go:367-402` (already known: `tickDamage = round(DamagePerTick × mods.DamageMult)` — NO `effectiveAbilityDamageLocked` call) and `siphonLifeChannelModifiersForCasterLocked` (perks_siphoner.go). Confirm the legacy tick does NOT apply `effectiveAbilityDamageLocked`. Then check whether siphon_life has any spell mods in the catalog that would make an authored `deal_damage`'s fold non-identity.

Run:
```bash
cd "c:/Personal Dev/webrts" && rg -n "siphonLifeChannelModifiersForCasterLocked" server/internal/game/perks_siphoner.go
```
Record: (a) legacy tick damage = `round(DamagePerTick × mods.DamageMult)`, no spell fold; (b) therefore the authored `deal_damage` must NOT introduce a spell fold that changes the number — achieved by setting `ctx.damageEffectivenessMultiplier = mods.DamageMult` and relying on `deal_damage`'s existing `effectiveAbilityDamageLocked` folding to identity for siphon_life. If siphon_life DOES carry a spell mod that alters damage, flag it — parity requires the legacy path to have folded it too, else the migration changes the number.

- [ ] **Step 5: Decision gate.** If Steps 2-4 reveal that siphon_life parity requires more machinery than the genuineness gain justifies (heal-distribution + perks already stay runtime; only the damage tick becomes authored), record a recommendation and **stop to confirm scope with the user** before Task 3. chain_lightning (Tasks 1-2) proceeds regardless.

---

## Task 1: `launch_beam` action + `on_beam_impact` trigger + Beam impact-actions seam

**Files:**
- Modify: `server/internal/game/ability_program.go` (enum consts)
- Modify: `server/internal/game/ability_program_validate.go:10-17,140-159`
- Modify: `server/internal/game/ability_program_enums.go:19-25`
- Modify: `server/internal/game/beam.go`
- Create: `server/internal/game/ability_exec_beam.go`
- Modify: `server/internal/game/ability_program_schema_targeting_test.go:30,120`
- Test: `server/internal/game/ability_exec_beam_test.go`

- [ ] **Step 1: Add the enum consts.**

In `ability_program.go`, add to the `TriggerType` block (after `TriggerOnProjectileTick`, line ~38):
```go
	// TriggerOnBeamImpact fires once when a launch_beam beam reaches its
	// target (after impactDelaySeconds). Carries the hit unit as
	// CurrentEventUnitID and its position as ImpactPosition — mirrors
	// TriggerOnProjectileImpact for the instantaneous-beam case.
	TriggerOnBeamImpact TriggerType = "on_beam_impact"
	// TriggerOnBeamTick fires once per channel tick interval for a channeled
	// beam (siphon_life). The channel loop owns the clock; this trigger is
	// the per-tick EFFECT. Carries the channel target as CurrentEventUnitID.
	TriggerOnBeamTick TriggerType = "on_beam_tick"
```
Add to the `ActionType` block (after `ActionChannelBeam`, line ~99):
```go
	// ActionLaunchBeam spawns an instantaneous beam from SpawnOrigin to its
	// resolved target and fires the nested on_beam_impact trigger a beat
	// later. The beam analogue of ActionLaunchProjectile.
	ActionLaunchBeam ActionType = "launch_beam"
```

- [ ] **Step 2: Register the action type in the validation/enum lists.**

In `ability_program_validate.go`, add `ActionLaunchBeam` to `allActionTypes` (line 10-17) and — if `launch_beam` may be a cast-time delivery — to the cast-time action list at line 13 (mirror `ActionLaunchProjectile`'s membership). In `ability_program_enums.go`, add `string(TriggerOnBeamImpact)` and `string(TriggerOnBeamTick)` to the hand-maintained `triggerTypes` literal (line 19-25).

- [ ] **Step 3: Run the drift-guard tests — expect FAIL until schema + registration exist.**

Run:
```bash
cd "c:/Personal Dev/webrts/server" && go test ./internal/game/ -run 'TestKnownActionTypesCoversAllConsts|TestProgramEnums' -v
```
Expected: `TestKnownActionTypesCoversAllConsts` PASSES (const now in `allActionTypes`); the schema-count test will fail until Step 6 registers the action.

- [ ] **Step 4: Add the impact-actions seam to `Beam`.**

In `beam.go`, add to the `Beam` struct (after the momentary fields, ~line 61), documenting the third momentary flavor:
```go
	// ── Authored impact actions (launch_beam) — momentary beams only ─────────
	// When ImpactActions is non-empty, tickBeamsLocked runs these authored
	// actions when DamageDelayRemaining elapses INSTEAD of applying
	// PendingDamage — the beam analogue of Projectile.ImpactActions. The chain
	// is bounded by ImpactOpsBudget (shared by pointer across re-launched
	// hops), mirroring Projectile.ImpactOpsBudget.
	ImpactActions   []AbilityActionDef
	ImpactOpsBudget *int
	// CasterID is the ORIGINAL caster for the impact RuntimeAbilityContext
	// (attribution, spell-mod fold), distinct from CasterUnitID (the VISUAL
	// origin — the previous victim on a bounce hop).
	CasterID int
	// AbilityIDForCtx is the ability id used to build the impact context's
	// abilityDef/program. Distinct from AbilityID (the channel-beam field).
	AbilityIDForCtx string
	// ImpactDamageMultiplier carries the launching context's
	// effectiveDamageMultiplier() forward to the impact actions, mirroring
	// Projectile.ImpactDamageMultiplier.
	ImpactDamageMultiplier float64
```

- [ ] **Step 5: Add `spawnBeamWithImpactActionsLocked` and run impact actions in `tickBeamsLocked`.**

In `beam.go`, add the spawn helper (mirror `spawnMomentaryDamageBeamLocked`, but carrying actions instead of PendingDamage):
```go
// spawnBeamWithImpactActionsLocked spawns a momentary beam from a frozen
// origin to `to` and schedules its authored on_beam_impact actions to run
// after delaySec. fromUnitID is the VISUAL origin (previous victim on a hop),
// casterID/abilityID build the impact context, budget is the shared cross-hop
// op budget. Mirrors fireProjectileWithImpactActionsLocked for the beam case.
//
// Caller holds s.mu write lock.
func (s *GameState) spawnBeamWithImpactActionsLocked(casterID, fromUnitID int, fromX, fromY float64, to *Unit, variant, abilityID string, impactActions []AbilityActionDef, budget *int, dmgMult float64, durationMs int, delaySec float64) *Beam {
	if durationMs <= 0 {
		durationMs = defaultBeamDurationMs
	}
	b := &Beam{
		ID:                     fmt.Sprintf("beam-%d", s.nextBeamID),
		CasterUnitID:           fromUnitID,
		CasterID:               casterID,
		TargetUnitID:           to.ID,
		OwnerPlayerID:          s.beamOwnerForCasterLocked(casterID),
		Variant:                variant,
		Momentary:              true,
		RemainingSeconds:       float64(durationMs) / 1000.0,
		OriginX:                fromX,
		OriginY:                fromY,
		TargetX:                to.X,
		TargetY:                to.Y,
		ImpactActions:          impactActions,
		ImpactOpsBudget:        budget,
		AbilityIDForCtx:        abilityID,
		ImpactDamageMultiplier: dmgMult,
		DamageDelayRemaining:   delaySec,
	}
	s.nextBeamID++
	s.Beams = append(s.Beams, b)
	return b
}
```
(Add a tiny `beamOwnerForCasterLocked(id int) string` helper that resolves the caster's `OwnerID`, or inline it — match how `spawnMomentaryDamageBeamLocked` sources `OwnerPlayerID`.)

In `tickBeamsLocked` (line 200-221), extend the momentary branch: BEFORE the `PendingDamage > 0` block, handle impact actions:
```go
			if len(b.ImpactActions) > 0 {
				b.DamageDelayRemaining -= dt
				if b.DamageDelayRemaining <= 0 && !b.impactFired {
					b.impactFired = true
					s.fireBeamImpactLocked(b)
				}
			} else if b.PendingDamage > 0 {
				// ... existing PendingDamage path unchanged ...
			}
```
Add `impactFired bool` to `Beam`. Add `fireBeamImpactLocked` (mirror `fireProjectileImpactLocked`, `projectile.go:801`):
```go
// fireBeamImpactLocked builds the impact RuntimeAbilityContext and runs the
// beam's authored on_beam_impact actions. Mirrors fireProjectileImpactLocked.
//
// Caller holds s.mu write lock.
func (s *GameState) fireBeamImpactLocked(b *Beam) {
	def, ok := getAbilityDef(b.AbilityIDForCtx)
	ctx := &RuntimeAbilityContext{
		CasterID:                      b.CasterID,
		AbilityID:                     b.AbilityIDForCtx,
		InitialTarget:                 b.TargetUnitID,
		ImpactPosition:                protocol.Vec2{X: b.TargetX, Y: b.TargetY},
		EventPosition:                 protocol.Vec2{X: b.TargetX, Y: b.TargetY},
		CurrentEventUnitID:            b.TargetUnitID,
		Named:                         map[string]ContextValue{},
		Trace:                         s.previewTrace,
		now:                           s.previewClock,
		sharedOpsRemaining:            b.ImpactOpsBudget,
		damageEffectivenessMultiplier: b.ImpactDamageMultiplier,
	}
	if ok {
		ctx.program = def.Program
		ctx.abilityDef = &def
	}
	for i := range b.ImpactActions {
		if ctx.opsExhausted() {
			break
		}
		s.executeActionLocked(ctx, &b.ImpactActions[i], "on_beam_impact")
	}
}
```
Note: the impact resolves the target's FROZEN position (`b.TargetX/Y`) since the beam is instantaneous and the target may have moved/died; `CurrentEventUnitID` is still the id so `deal_damage` re-validates HP at apply time.

- [ ] **Step 6: Add `launch_beam`'s config + registration.**

Create `ability_exec_beam.go`:
```go
package game

import "encoding/json"

// launch_beam executor — the beam analogue of launch_projectile
// (ability_exec_projectile.go). Spawns an instantaneous beam from SpawnOrigin
// to its resolved target and fires the nested on_beam_impact trigger a beat
// later via the Beam.ImpactActions seam (beam.go).

type launchBeamConfig struct {
	Variant            string              `json:"variant,omitempty"`
	SpawnOrigin        TargetOrigin        `json:"spawnOrigin,omitempty"`
	ImpactDelaySeconds float64             `json:"impactDelaySeconds,omitempty"`
	DurationMs         int                 `json:"durationMs,omitempty"`
	Triggers           []AbilityTriggerDef `json:"triggers,omitempty"`
}

func (launchBeamConfig) actionConfig() {}

const defaultBeamVariant = "lightning_bolt"

func init() {
	registerAction(ActionDescriptor{
		Type: ActionLaunchBeam,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c launchBeamConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(launchBeamConfig)
			var out []ValidationIssue
			if c.ImpactDelaySeconds < 0 {
				out = append(out, ValidationIssue{Code: "invalid_property", Message: "launch_beam impactDelaySeconds must be >= 0", Severity: "error"})
			}
			return out
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "target", Label: "Target", Control: "target_query", Section: "Targeting", TargetQueryFields: targetQueryFieldsSourceOnly},
			{Key: "variant", Label: "Beam Variant", Control: "text", Section: "Presentation"},
			{Key: "spawnOrigin", Label: "Spawn Origin", Control: "select", Section: "Advanced", Options: launchProjectileSpawnOriginOptions},
			{Key: "impactDelaySeconds", Label: "Impact Delay (s)", Control: "number", Section: "Advanced"},
			{Key: "durationMs", Label: "Flash Duration (ms)", Control: "number", Section: "Presentation"},
		}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(launchBeamConfig)
			caster := s.getUnitByIDLocked(ctx.CasterID)
			if caster == nil {
				return nil
			}
			// Shared cross-hop op budget (mirrors launch_projectile Execute).
			var budget *int
			if ctx.sharedOpsRemaining != nil {
				budget = ctx.sharedOpsRemaining
			} else {
				remaining := maxExecutionOps - ctx.opsUsed
				budget = &remaining
			}
			var impactActions []AbilityActionDef
			for _, trig := range c.Triggers {
				if trig.Type == TriggerOnBeamImpact {
					impactActions = trig.Actions
					break
				}
			}
			variant := c.Variant
			if variant == "" {
				variant = defaultBeamVariant
			}
			origin := s.resolveOriginLocked(ctx, c.SpawnOrigin, nil)
			delay := c.ImpactDelaySeconds
			if delay == 0 {
				delay = defaultBeamImpactDelay
			}
			hit := make([]int, 0, len(targets))
			for _, id := range targets {
				target := s.getUnitByIDLocked(id)
				if target == nil || target.HP <= 0 {
					continue
				}
				s.spawnBeamWithImpactActionsLocked(caster.ID, caster.ID, origin.X, origin.Y, target, variant, ctx.AbilityID, impactActions, budget, ctx.effectiveDamageMultiplier(), c.DurationMs, delay)
				hit = append(hit, id)
				ctx.trace("beam_launched", ctx.currentActionPath, map[string]any{"target": id, "variant": variant})
			}
			return hit
		},
	})
}
```
Add `const defaultBeamImpactDelay = 0.1` near `defaultBeamDurationMs` in `beam.go` (match the proc-beam delay used today; verify the exact value the momentary damage beam uses and reuse it). Confirm `launchProjectileSpawnOriginOptions` is exported/visible (it lives in `ability_exec_projectile.go`).

- [ ] **Step 7: Update the schema-targeting test map.**

In `ability_program_schema_targeting_test.go`, add `ActionLaunchBeam` to the source-only set (line 30) and to the per-action expected map (line 120): `ActionLaunchBeam: targetQueryFieldsSourceOnly`.

- [ ] **Step 8: Write the failing unit test — single non-chaining beam.**

Create `ability_exec_beam_test.go`:
```go
package game

import "testing"

// A launch_beam action with an on_beam_impact deal_damage should: spawn a
// momentary beam immediately, deal NO damage until the impact delay elapses,
// then deal the authored damage exactly once.
func TestLaunchBeam_ImpactDamageAfterDelay(t *testing.T) {
	s := newBeamTestState(t)
	caster := s.spawnTestUnit(t, /* team A */)
	enemy := s.spawnTestUnit(t, /* team B */)
	enemy.HP = 100

	prog := &AbilityProgram{Triggers: []AbilityTriggerDef{{
		ID: "cast", Type: TriggerOnCastComplete,
		Actions: []AbilityActionDef{{
			ID: "beam", Type: ActionLaunchBeam,
			Target: &TargetQueryDef{Source: SrcInitialTarget},
			Config: marshalConfig(launchBeamConfig{
				Variant:            "lightning_bolt",
				ImpactDelaySeconds: 0.1,
				Triggers: []AbilityTriggerDef{{
					ID: "impact", Type: TriggerOnBeamImpact,
					Actions: []AbilityActionDef{{
						ID: "dmg", Type: ActionDealDamage,
						Target: &TargetQueryDef{Source: SrcCurrentEvent},
						Config: marshalConfig(dealDamageConfig{Amount: 40}),
					}},
				}},
			}),
		}},
	}}}

	ctx := &RuntimeAbilityContext{CasterID: caster.ID, AbilityID: "test_beam", InitialTarget: enemy.ID, program: prog, Named: map[string]ContextValue{}}
	s.runProgramTriggersLocked(ctx, prog.Triggers, TriggerOnCastComplete)

	if got := s.getUnitByIDLocked(enemy.ID).HP; got != 100 {
		t.Fatalf("damage applied before impact delay: HP=%d want 100", got)
	}
	if len(s.Beams) != 1 {
		t.Fatalf("beam not spawned: got %d beams", len(s.Beams))
	}
	s.tickBeamsLocked(0.2) // elapse the 0.1s delay
	if got := s.getUnitByIDLocked(enemy.ID).HP; got != 60 {
		t.Fatalf("impact damage wrong: HP=%d want 60", got)
	}
	s.tickBeamsLocked(0.2) // must not double-apply
	if got := s.getUnitByIDLocked(enemy.ID).HP; got != 60 {
		t.Fatalf("impact damage applied twice: HP=%d want 60", got)
	}
}
```
(Use the existing test scaffolding helpers — find the canonical `newTestState`/`spawnTestUnit` idiom in `ability_exec_*_test.go` and match it; the pseudo-helpers above are placeholders for those.)

- [ ] **Step 9: Run it red, then green.**

Run:
```bash
cd "c:/Personal Dev/webrts/server" && go test ./internal/game/ -run 'TestLaunchBeam_ImpactDamageAfterDelay' -v
```
Expected after Steps 1-7: PASS. If the impact never fires, verify `impactFired`/`DamageDelayRemaining` wiring in `tickBeamsLocked`.

- [ ] **Step 10: Full game-package test + commit.**

Run:
```bash
cd "c:/Personal Dev/webrts/server" && go test ./internal/game/ 2>&1 | tail -20
```
Expected: all green. Then commit (`launch_beam` action + `on_beam_impact` + Beam impact seam). *(Commits are the user's to make — surface the checkpoint, do not run `git commit`.)*

---

## Task 2: chain_lightning authored program + compiler unroll + parity

**Files:**
- Modify: `server/internal/game/ability_compile.go` (unroll builder; remove `ChainCount>0` branch at line 470-476)
- Modify: `server/internal/game/ability_exec_projectile.go` (delete `executeChainLightningShimLocked` + the `ChainCount>0` dispatch + `launchProjectileConfig` chain fields)
- Modify: `server/internal/game/ability_cast.go` (delete `fireAbilityChainLocked` + `case eff.ChainCount>0`, per Task 0 Step 1)
- Modify: `server/internal/game/catalog/abilities/chain_lightning/*.json` (regenerated)
- Modify/Test: `server/internal/game/ability_compile_golden_projectile_test.go` (`TestAbilityCompileGolden_ChainLightning`)
- Modify: `ability_compile_catalog_test.go:145,283` (chain_lightning now expects `ActionLaunchBeam`, not `ActionLaunchProjectile`)

- [ ] **Step 0: Add the two targeting primitives (Task 0 Q2).** TDD each.

  (a) **Exclude-named-set.** Add `ExcludeRef *ContextRef` to `filterTargetsConfig`
  (ability_exec_flow.go) AND `TargetQueryDef` (ability_program.go + the TS
  mirror). In `applyTargetFiltersLocked` (ability_exec_targeting.go:261-272,
  beside the `ExcludeSource`/`ExcludeCurrentEvent` drops), when `ExcludeRef` is
  set, read `ctx.Named[ExcludeRef.Key]` as a `ctxUnitSet` and drop those IDs —
  mirror the `SrcNamedContext` read at ability_exec_targeting.go:100-117. No-op
  when the key is absent/empty. Unit test: a query with a named "hit" set
  excludes exactly those IDs.

  (b) **Append mode on `store_targets`.** Add `Merge bool` to `storeTargetsConfig`
  (ability_exec_flow.go:193-201). When `Merge` and `ctx.Named[As]` already holds
  a `ctxUnitSet`, union the incoming IDs into it (dedup, deterministic order)
  instead of replacing. Unit test: two successive merge-stores accumulate.

  These are general, reusable primitives (any "spread/chain without repeats"
  ability needs them) — keep them minimal and independently tested.

- [ ] **Step 1: Write the unroll compiler.**

Add `compileChainLightningActions(def AbilityDef) []AbilityActionDef` in `ability_compile.go`. It emits ONE top-level `launch_beam` whose `on_beam_impact` deals hop-0 damage, accumulates the victim into a named `"hit"` set (`store_targets{Merge:true}`), selects the next victim, and nests another `launch_beam`, recursing to `ChainCount` depth. **Exact per-hop damage (Task 0 Q1):** integer `def.DamageAmount - def.BounceDamageFalloff*hop` (hop 0 = full; NO rounding); if a level's amount is `<= 0`, STOP emitting deeper levels (matches legacy's `dmg <= 0` break). **Next-victim query:** `SrcAllInScene`, `Origin: current_event_position`, `Relations:[enemy]`, `Radius: def.BounceRange`, `Ordering: closest`, `MaxCount: 1`, `ExcludeSource: true` (caster), `ExcludeRef: {Key:"hit"}` (all prior victims). **Beam:** hop 0 targets `SrcInitialTarget` from caster origin; deeper hops target `SrcPreviousActionTargets` with `SpawnOrigin: current_event_position`. Pseudocode structure:
```go
func compileChainLightningActions(def AbilityDef) []AbilityActionDef {
	return []AbilityActionDef{buildChainHop(def, 0)}
}
func buildChainHop(def AbilityDef, hop int) AbilityActionDef {
	amount := chainHopAmount(def.DamageAmount, def.BounceDamageFalloff, hop) // matches legacy falloff exactly
	impact := []AbilityActionDef{
		{ID: "dmg", Type: ActionDealDamage, Target: &TargetQueryDef{Source: SrcCurrentEvent}, Config: marshalConfig(dealDamageConfig{Amount: amount, Type: def.DamageType})},
	}
	if hop+1 < def.ChainCount {
		impact = append(impact,
			// store the just-hit unit, pick the nearest not-yet-hit enemy in BounceRange, launch the next beam from that victim
			/* store_targets + select_targets(excludeCurrentEvent + not-in visited, radius: BounceRange, maxCount:1, ordering:closest) */,
			nestedLaunchBeam(def, hop+1),
		)
	}
	target := &TargetQueryDef{Source: SrcInitialTarget}
	spawnOrigin := TargetOrigin("") // caster for hop 0
	if hop > 0 {
		target = &TargetQueryDef{Source: SrcPreviousActionTargets}
		spawnOrigin = OriginCurrentEventPosition
	}
	return AbilityActionDef{ID: fmt.Sprintf("beam%d", hop), Type: ActionLaunchBeam, Target: target,
		Config: marshalConfig(launchBeamConfig{Variant: chainVariant(def), SpawnOrigin: spawnOrigin, Triggers: []AbilityTriggerDef{{ID: "impact", Type: TriggerOnBeamImpact, Actions: impact}}})}
}
```
Wire `compileProjectileActions` (line 465) to call `compileChainLightningActions(def)` when `def.ChainCount > 0` instead of setting the shim fields (remove the `cfg.Amount = ...` block, line 470-476).

- [ ] **Step 2: Regenerate chain_lightning's catalog JSON.**

Follow the established regenerate idiom (a throwaway `TestZZRegen...` that registers the legacy fixture in the runtimeAbilities overlay, calls `ConvertLegacyAbility("chain_lightning")`, writes the result, then is deleted — see the arcane_orb regen precedent). Confirm the emitted program has N nested `launch_beam`/`on_beam_impact` levels and no `chainCount` field.

- [ ] **Step 3: Extend the golden parity test — expect it to drive the implementation.**

In `TestAbilityCompileGolden_ChainLightning`, keep the existing "no Projectile spawned; beams only" assertions and ADD per-hop assertions: identical total damage, per-hop damage sequence, hop count, victim id set, and beam count between the legacy fixture path and the executor path — run for an UNMODIFIED caster and a caster with each relevant spell-mod loadout.

Run:
```bash
cd "c:/Personal Dev/webrts/server" && go test ./internal/game/ -run 'TestAbilityCompileGolden_ChainLightning' -v
```
Expected: PASS. If per-hop damage diverges, the falloff arithmetic in `chainHopAmount` doesn't match legacy — reconcile against Task 0 Step 2's recorded rule. If victims diverge, the visited-set/exclusion is wrong.

- [ ] **Step 4: Retire the shim code.**

Delete from `ability_exec_projectile.go`: the `if c.ChainCount > 0` dispatch (line 277-279) and `executeChainLightningShimLocked` (line 350-387); delete the chain fields from `launchProjectileConfig` (`Amount/Type/MinorDamage/ChainCount/BounceRange/BounceDamageFalloff`, line 396-401) — but ONLY these; leave the vortex `TickInterval` shim intact. Delete from `ability_cast.go`: `fireAbilityChainLocked` (line 794-813) and the `case eff.ChainCount > 0` arm (line 742-747), per Task 0 Q4. **Rewrite** `ability_chain_bounce_attribution_test.go` — it calls `fireAbilityChainLocked` directly (line 205) to prove a bounce KILL fires `on_unit_death` for the correct attribution. That behavior must still hold via the new path: re-point the test to run chain_lightning's authored program through the executor and assert the same on_unit_death attribution for a bounce-killed victim. Update `ability_compile_catalog_test.go:145` (`"chain_lightning": {ActionLaunchBeam}`) and `:283`.

- [ ] **Step 5: Full test + commit checkpoint.**

Run:
```bash
cd "c:/Personal Dev/webrts/server" && go test ./internal/game/ 2>&1 | tail -20
```
Expected: all green, including the chain golden and the catalog action-type maps. Surface the checkpoint.

---

## Task 3: `on_beam_tick` + `deal_damage` readback + siphon_life damage tick authored

> Gated on Task 0 Step 5 (scope confirmation). heal-distribution, channel lifecycle, and all perks stay runtime; only the per-tick DAMAGE becomes authored.

**Files:**
- Modify: `server/internal/game/ability_exec.go` (`RuntimeAbilityContext.lastAppliedDamage`)
- Modify: `server/internal/game/ability_program_registry.go` (`deal_damage` records applied amount)
- Modify: `server/internal/game/ability_channel.go` (tick loop fires `on_beam_tick`, reads back damage)
- Modify: `server/internal/game/ability_exec_channel.go` / `ability_compile.go` (siphon_life carries an `on_beam_tick` trigger + spawns the beam via the new variant)
- Modify: `server/internal/game/catalog/abilities/siphon_life/siphon_life.json`
- Test: `server/internal/game/ability_channel_test.go`

- [ ] **Step 1: Add the applied-damage readback to the context.**

In `ability_exec.go`, add `lastAppliedDamage int` to `RuntimeAbilityContext` (near `Selected`), doc: "total damage the most recent deal_damage action applied — read by the siphon_life channel loop to feed heal + perks off a single authority." In `deal_damage`'s Execute (`ability_program_registry.go:367-397`), accumulate: initialize `ctx.lastAppliedDamage = 0` at the top of the loop body region and add `amount` for each unit actually hit (or set once to `amount` — match the siphon single-target case; for multi-target, sum). Set it even when 0 hits (0).

- [ ] **Step 2: Fire `on_beam_tick` from the channel loop.**

In `ability_channel.go`'s `tickUnitChannelLocked`, replace the inlined base `tickDamage`/`applyUnitDamageWithSourceLocked` block (line 384-402) — for `SchemaVersion>=2` defs — with: set `ctx.damageEffectivenessMultiplier = mods.DamageMult`, fire the compiled `on_beam_tick` trigger via `runProgramTriggersLocked`, then read `tickDamage := ctx.lastAppliedDamage`. The heal (`healAmount`) and ALL perk calls below (line 408-438) stay UNCHANGED, now reading the `tickDamage` that came from the authored `deal_damage`. Legacy (`SchemaVersion<2`) defs keep the inlined block. Build the tick `ctx` once per channel-start or per tick (mirror how `beginAbilityChannelLocked` builds its ctx, line 288-299); the abilityDef fold must be identity for siphon (Task 0 Step 4).

- [ ] **Step 3: Compile siphon_life's `on_beam_tick` + beam spawn.**

Extend `compileChannelBeamAction` (ability_compile.go) so a converted siphon_life carries an `on_beam_tick` trigger of a single `deal_damage(DamagePerTick, DamageType)` targeting the channel target (`SrcInitialTarget`/current channel target). The channel beam VISUAL keeps spawning via `startChannelLocked`/`spawnBeamLocked` (unchanged — the channeled beam, not a momentary one). Regenerate `siphon_life.json` via the throwaway-regen idiom.

- [ ] **Step 4: Parity test — plain caster.**

Extend `ability_compile_golden_channel_test.go`: assert identical per-tick damage, heal, and mana drain between the legacy fixture and the executor path for a NON-perk caster, across the full channel duration.

Run:
```bash
cd "c:/Personal Dev/webrts/server" && go test ./internal/game/ -run 'Golden.*Channel|Siphon' -v
```
Expected: PASS. Divergence in per-tick damage ⇒ the fold/multiplier wiring in Step 2 doesn't reproduce `round(DamagePerTick × mods.DamageMult)` — reconcile against Task 0 Step 4.

- [ ] **Step 5: Commit checkpoint.**

---

## Task 4: siphon_life perk parity

**Files:**
- Test: `server/internal/game/ability_compile_golden_channel_test.go` (+ siphoner perk test files as needed)

- [ ] **Step 1: Golden parity across every Siphoner perk.**

Extend the channel golden test to run the legacy vs executor comparison for a caster carrying each of: `soul_leech`, `beam_mastery` (the damage/heal/mana/range scalers), `chain_siphon`, `shared_suffering`, `withering_beam`, `repurposed_life`, `dark_renewal`. Assert identical: per-tick damage, secondary/chain beam damage + count, shared-suffering echo, withering stacks, heal distribution (self vs ally vs shield cascade), and repurposed_life mana-on-kill. Derive expected values from the fixtures — pin no literals.

Run:
```bash
cd "c:/Personal Dev/webrts/server" && go test ./internal/game/ -run 'Siphon|Golden.*Channel' -v
```
Expected: PASS for all perks. Any divergence localizes to the `tickDamage` readback (Task 3 Step 1) being a different number than the legacy local computation — the perks all key off it.

- [ ] **Step 2: Fold-once verification.**

Add an assertion that the Siphoner damage mult is applied EXACTLY once (not once as `ctx.damageEffectivenessMultiplier` and again as a spell-mod fold). Run the whole game package and commit.

Run:
```bash
cd "c:/Personal Dev/webrts/server" && go test ./internal/game/ 2>&1 | tail -20
```

---

## Task 5: Editor / schema / describe / icon + client mirror

**Files:**
- Modify: `client/src/game-portal/src/game/abilities/program/abilityProgram.ts`
- Modify: `client/src/game-portal/src/components/ability-builder/programTree.ts`
- Modify: `client/src/game-portal/src/components/ActionIcon.vue`
- Modify: `server/internal/game/ability_describe.go`
- Modify: `server/internal/game/catalog/action-icons.json`
- Test: `client/src/game-portal/src/game/abilities/program/programSchema.test.ts`

- [ ] **Step 1: Client enum mirror.**

In `abilityProgram.ts`, add `'on_beam_impact'` and `'on_beam_tick'` to the `TriggerType` union (line 26-41) and `'launch_beam'` to the `ActionType` union (line 45-68).

- [ ] **Step 2: Route nested beam triggers into `config.triggers`.**

In `programTree.ts`, add `'launch_beam'` to `CONFIG_TRIGGER_ACTION_TYPES` so `on_beam_impact` authors into `config.triggers` (matching `launch_projectile`).

- [ ] **Step 3: Action icon.**

Add a `launch_beam` entry to `catalog/action-icons.json` and the icon mapping in `ActionIcon.vue` (reuse the lightning/beam glyph).

- [ ] **Step 4: Beam-aware describe.**

In `ability_describe.go`, add `case ActionLaunchBeam:` (near line 274) and handle `TriggerOnBeamImpact` (near line 337) so the generated tooltip describes the chain from the authored program. Update `ability_describe_test.go` expectations for chain_lightning and siphon_life.

- [ ] **Step 5: Typecheck + client tests.**

Run:
```bash
cd "c:/Personal Dev/webrts/client/src/game-portal" && npx vue-tsc -b && npx vitest run 2>&1 | tail -15
```
Expected: typecheck clean; tests green (the 3 pre-existing `ListEditorPanel.test.ts` failures excepted). Then:
```bash
cd "c:/Personal Dev/webrts/server" && go test ./internal/game/ -run 'Describe' -v
```

- [ ] **Step 6: Commit checkpoint.**

---

## Task 6: Cleanup + honesty audit

**Files:**
- Modify: (dead-field removals surfaced by the compiler)
- Modify: the catalog-status doc/memory

- [ ] **Step 1: Remove now-dead legacy fields.**

Search for `ChainCount`/`BounceRange`/`BounceDamageFalloff` references that only exist to feed the retired shim. Remove any that are now unreferenced (the compiler/tests will flag live ones). Leave `AbilityDef`'s legacy fields that the ConvertLegacyAbility fixtures still read.

Run:
```bash
cd "c:/Personal Dev/webrts" && rg -n "ChainCount|fireAbilityChainLocked|executeChainLightningShimLocked" server/
```
Expected: only legacy-fixture / conversion-input references remain; no live runtime dispatch.

- [ ] **Step 2: Update the catalog honesty status.**

Update the composable-abilities status (spec/memory) to reflect chain_lightning and siphon_life as genuine (target: 11 genuine, 0 shims for these two). Note siphon_life's runtime-lifecycle/perk seam explicitly so a future reader doesn't mistake it for a remaining shim.

- [ ] **Step 3: Full suite, both sides.**

Run:
```bash
cd "c:/Personal Dev/webrts/server" && go test ./internal/game/ 2>&1 | tail -20
cd "c:/Personal Dev/webrts/client/src/game-portal" && npx vue-tsc -b && npx vitest run 2>&1 | tail -10
```
Expected: green (minus the 3 pre-existing client failures). Surface the final checkpoint for the user to commit.

---

## Self-review notes

- **Parity is the spine:** every runtime change is gated by a golden test comparing legacy-fixture vs executor, unmodified AND modified caster. No behavior change ships un-pinned.
- **siphon_life scope is deliberately narrow:** only the damage tick + beam visual become authored; heal-distribution, lifecycle, and perks stay runtime because they are perk-cascade logic, not per-ability content. Task 0 Step 5 is the gate to confirm this is worth it before Task 3.
- **The `deal_damage` readback (`ctx.lastAppliedDamage`) is the one new general seam** — it makes `deal_damage` the single damage authority so the perks can't drift from the authored number. Keep it minimal (a sum the action sets), and verify fold-once (Task 4 Step 2).
- **Type consistency:** const names `TriggerOnBeamImpact`/`TriggerOnBeamTick`/`ActionLaunchBeam` with string values `on_beam_impact`/`on_beam_tick`/`launch_beam`; config type `launchBeamConfig`; helpers `spawnBeamWithImpactActionsLocked`/`fireBeamImpactLocked`. Used identically in every task above.
