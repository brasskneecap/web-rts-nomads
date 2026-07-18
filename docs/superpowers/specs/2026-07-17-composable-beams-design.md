# Composable Beams — Design Spec

**Date:** 2026-07-17
**Status:** Draft for review
**Depends on:** the composable-abilities rebuild (executor, compiler, golden-parity harness, cross-tick op budget)

## Goal

Make **beam** a first-class composable primitive and retire the last two
delivery shims in the ability catalog:

- **chain_lightning** — today a *faked* migration: its program says
  `launch_projectile` + `chainCount`, but the runtime discards that and calls
  `fireAbilityChainLocked` (the equipment-proc bounce machinery). Nothing about
  the chain is authored.
- **siphon_life** — today an *honest but baked* migration: a `channel_beam`
  action bakes a `channelSpec` and calls the real `startChannelLocked`, but the
  per-tick damage+heal is the hardcoded `tickUnitChannelLocked` loop, not
  authored actions.

After this phase both abilities run authored programs, and a new `launch_beam`
action + `on_beam_impact` / `on_beam_tick` triggers exist for future content.

## Decisions already made (with the user)

1. **Beam is a first-class action**, not a `travelMode` on `launch_projectile`.
2. **Both** chain_lightning and siphon_life migrate this phase.

## Non-goals

- Rewriting the channel *lifecycle* (interrupt-on-move/stun/mana-out, cast
  lock, beam despawn) as a pure status-machine. The channel state machine
  (`beginAbilityChannelLocked` / `tickUnitChannelLocked` / stop/clear) stays the
  lifecycle owner. See "The siphon_life perk seam" for why.
- Changing the Siphoner perks' behavior or their determinism. They must remain
  byte-identical.
- Touching momentary *proc* beams (equipment/item on-hit zaps). They keep their
  current `PendingDamage`/frozen-endpoint path untouched.

---

## Part 1 — The `launch_beam` action

New `ActionLaunchBeam` ("launch_beam"). It is the beam analogue of
`launch_projectile`: spawn + deliver-at-impact only, with the payload composed
in a nested `on_beam_impact` trigger. A beam is *instantaneous* (no travel), so
"impact" is scheduled a short beat after spawn (the existing momentary-beam
`DamageDelayRemaining` mechanism) so the damage number reads as its own popup
rather than merging into the triggering event.

### Config (`launchBeamConfig`)

```go
type launchBeamConfig struct {
    Variant           string              `json:"variant,omitempty"`            // renderer variant (assets/beams/<variant>/); default "lightning_bolt"
    SpawnOrigin       TargetOrigin        `json:"spawnOrigin,omitempty"`        // where the beam STARTS; "" = caster (same TargetOrigin vocab launch_projectile.SpawnOrigin reuses)
    ImpactDelaySeconds float64            `json:"impactDelaySeconds,omitempty"` // beat before on_beam_impact fires; 0 = defaultBeamImpactDelay
    DurationMs        int                 `json:"durationMs,omitempty"`         // flash lifetime; 0 = defaultBeamDurationMs
    Triggers          []AbilityTriggerDef `json:"triggers,omitempty"`           // the nested on_beam_impact trigger (config.triggers slot, exactly like launch_projectile)
}
```

- **Target query:** its own `TargetQueryDef{Source: SrcInitialTarget}` (who the
  beam hits), same as `launch_projectile` — a beam hits one unit; the query
  doubles as the alive-guard.
- `Triggers` is the `config.triggers` slot the editor already understands via
  `CONFIG_TRIGGER_ACTION_TYPES` (add `launch_beam` to that set client-side, and
  to the Go `configTriggerActionTypes` equivalent). It is NOT `Children` — it
  fires on *impact*, never via `on_action_complete`.

### Beam entity changes (`beam.go`)

Momentary beams gain a *third* mode alongside "carries baked PendingDamage" and
"pure visual flash": **carries authored impact actions**. New fields on `Beam`:

```go
ImpactActions   []AbilityActionDef // authored on_beam_impact actions; when non-empty, run these at impact instead of PendingDamage
ImpactOpsBudget *int               // shared cross-hop op budget (chain safety); mirrors Projectile.ImpactOpsBudget
CasterID        int                // the ORIGINAL caster (for the impact context), distinct from CasterUnitID (visual origin, = previous victim on a hop)
AbilityIDForCtx string             // ability id to build the RuntimeAbilityContext (attribution / spell-mod fold)
```

`tickBeamsLocked`: when `DamageDelayRemaining` elapses on a beam with non-empty
`ImpactActions`, build a `RuntimeAbilityContext`
(`CasterID`, `AbilityID=AbilityIDForCtx`, `CurrentEventUnitID=TargetUnitID`,
`ImpactPosition = TargetX/Y or live target pos`, shared `ImpactOpsBudget`) and
run the impact trigger's actions via the executor — mirroring
`fireProjectileImpactLocked` / `proj.ImpactActions` exactly. Legacy
`PendingDamage` beams are unchanged (the `ImpactActions == nil` branch).

This is the **same seam pattern already proven for projectile impact** — we are
copying `proj.ImpactActions`, not inventing a mechanism.

### New trigger: `on_beam_impact` (`TriggerOnBeamImpact`)

Fires once, when a launched beam reaches its target (after `ImpactDelaySeconds`).
Context at fire time:
- `CurrentEventUnitID` = the unit the beam hit
- `ImpactPosition` = that unit's position
- shared op budget so a chain of re-launched beams can't runaway.

Chaining is authored: inside `on_beam_impact` the author does `deal_damage`
(to `current_event`), `select_targets` (nearby, `excludeCurrentEvent`), then
another `launch_beam` (`spawnOrigin: current_event_position`, target =
`previous_action_targets`). Depth is bounded by the op budget, exactly like
arcane_orb's relaunch chain.

---

## Part 2 — chain_lightning migration

Legacy tuning: `DamageAmount`, `DamageType`, `ChainCount` (N hops),
`BounceRange` (search radius from each victim), `BounceDamageFalloff` (per-hop
damage reduction), `MinorDamage`.

### Authored shape — compiler unrolls to `ChainCount` depth

The genuinely-composable expression of "hit target, then arc to the nearest
not-yet-hit enemy within BounceRange, N times, losing BounceDamageFalloff each
hop" is **N literally-nested `launch_beam` actions**, each:

```
launch_beam(variant, spawnOrigin: <caster | current_event_position>, target: <initial_target | previous_action_targets>):
  on_beam_impact:
    deal_damage(amount = amountAtThisHop, type)          # to current_event
    store_targets(into: "hit")                           # accumulate visited set
    select_targets(source: all_in_scene, origin: current_event_position,
                   relations: enemy, radius: BounceRange, maxCount: 1,
                   ordering: closest, excludeCurrentEvent: true,
                   filter: not-in("hit"))                 # next victim, not already hit
    launch_beam(... next hop ...)
```

Two sub-problems and their resolutions:

- **Per-hop falloff.** Unrolling bakes each level's reduced `amount` literally
  (`amountAtHop_k = falloff(amount, k)` computed by the compiler using the SAME
  arithmetic `executeProcEffectLocked` uses today). This keeps the number
  visible in the editor and folds through `ctx.abilityDef` once per hop like any
  `deal_damage`. **Parity risk:** the compiler's falloff arithmetic must match
  the legacy bounce falloff exactly — pinned by the golden test.

- **No re-hitting a visited enemy.** The legacy bounce tracks visited victims.
  Reproduce with a named-context visited set: `store_targets(into:"hit")` after
  each impact, and `filter_targets`/query-filter excluding members of `"hit"`
  on the next `select_targets`. **This requires** a query filter that can
  exclude a named set — check whether `TargetQueryDef.Filters` +
  `ExcludeCurrentEvent` already covers it or whether a small `filter_targets`
  extension (exclude named context) is needed. **OPEN — resolve in Task 0.**

**Fallback if the visited-set proves too costly for exact parity:** keep the
unroll but accept "nearest within range, may re-hit" only if the legacy path
*also* can re-hit (verify against `executeProcEffectLocked`). If legacy forbids
re-hits, the visited set is mandatory — do not ship a parity-breaking
approximation.

### Delivery

`launch_beam`'s impact actions run through the same beam entity, so
chain_lightning still "resolves as Beams only, never a Projectile" — the
existing `TestAbilityCompileGolden_ChainLightning` assertion (no Projectile
spawned; beams ticked via `tickBeamsLocked`) stays valid and becomes the parity
oracle.

### Retire the shim

- Delete the `ChainCount > 0` branch in `compileProjectileActions`
  (ability_compile.go) and the `Amount/Type/MinorDamage/ChainCount/BounceRange/
  BounceDamageFalloff` fields from `launchProjectileConfig`.
- Delete `fireAbilityChainLocked` and the `case eff.ChainCount > 0` dispatch in
  `resolveAbilityCastOnTargetLocked` (ability_cast.go) — **only** once nothing
  else routes through it. (Equipment `lightning_chain` procs use
  `executeProcEffectLocked` directly, NOT `fireAbilityChainLocked`, so that path
  is unaffected — verify in Task 0.)

---

## Part 3 — siphon_life migration (the crux)

### The perk seam problem

`tickUnitChannelLocked`'s per-tick body does two things:
1. **Base effect:** `deal_damage(spec.DamagePerTick)` to the target +
   `distributeSiphonHealLocked(healAmount)` (heal = damage × HealingMultiplier).
2. **Perk augmentations** that read that tick's `tickDamage`/`healAmount`:
   `applyChainSiphonBeamsLocked`, `applySharedSufferingLocked`,
   `tickWitheringBeamChannelLocked`, plus `siphonLifeChannelModifiersForCaster`
   (damage/heal/mana/range scalers) and `repurposed_life` on stop.

The perks are **cross-cutting augmentations owned by the perk system**, keyed to
"a siphon_life channel tick happened and dealt D damage / healed H." They are
not per-ability authored content and must not move into the program.

### Resolution — genuine base effect, runtime lifecycle + perk augmentation

Keep the channel *lifecycle* (`beginAbilityChannelLocked` / the
`tickUnitChannelLocked` loop shell / stop/clear / beam spawn) as-is. Replace
only the **base effect block** with an authored `on_beam_tick` trigger the loop
fires each interval. The loop:

1. resolves + validates the target (unchanged),
2. applies the perk *scalers* to get the effective mana cost (unchanged),
3. checks/spends mana (unchanged),
4. **fires `on_beam_tick`** — an authored trigger of
   `deal_damage(DamagePerTick)` + `restore_health(caster/ally, ×HealingMultiplier)`
   — via the executor, and **captures how much damage was dealt** (executor
   returns applied damage, or the trigger writes it to a context key the loop
   reads),
5. feeds that captured `tickDamage`/`healAmount` into the existing perk hooks
   (`applyChainSiphonBeamsLocked`, `applySharedSufferingLocked`,
   `tickWitheringBeamChannelLocked`) **unchanged**.

So the **base drain+heal is authored** (editable in the flow view, describe-able,
the "genuine" part), while the **channel lifecycle and perks stay runtime**.
This is strictly more genuine than today (where the base effect is also
hardcoded) and preserves every perk byte-identically.

### New trigger: `on_beam_tick` (`TriggerOnBeamTick`)

Fires once per channel tick interval, with the channel target as
`CurrentEventUnitID`. Timing/cadence is owned by the channel loop
(`ChannelTickInterval`), not by the trigger — the trigger is the *effect*, the
loop is the *clock*. (This mirrors how the vortex's `on_projectile_tick` effect
is clocked by the projectile, not self-scheduled.)

### The captured-damage seam

The perks need the tick's *effective* damage. Today it's a local
`tickDamage := round(DamagePerTick × mods.DamageMult)`. After migration:
- The authored `deal_damage` folds `DamagePerTick` through `ctx.abilityDef`
  (spell mods) as usual. The **Siphoner damage-mult perk** must fold through the
  same seam so the number matches. Confirm `siphonLifeChannelModifiers`'
  `DamageMult` is expressible as a spell-mod on the ability (so `deal_damage`
  picks it up) — **OPEN, Task 0.** If it is NOT, the loop must apply the scaler
  and hand the pre-scaled amount to the trigger, or capture the post-fold
  applied damage from the executor and scale the perks off that.
- Cleanest: the executor's `deal_damage` returns applied damage; the loop reads
  it and drives the perks + the heal off it (heal already derives from
  tickDamage). This keeps ONE source of truth for "how much this tick hit for."

### Fold-once discipline

`DamagePerTick` must fold exactly once. Today the loop folds via `mods` then
rounds once. The authored `deal_damage` folds via `ctx.abilityDef`. Ensure the
Siphoner damage mult is NOT applied twice (once as a spell-mod fold and again as
a perk scaler). Pin with the golden channel test
(`ability_compile_golden_channel_test.go`) run unmodified AND with a Siphoner
caster carrying each perk.

---

## Part 4 — Parity strategy (the safety net)

Every change is gated by golden equivalence tests that run the frozen legacy
fixture through the legacy path and the shipped catalog def through the
executor, asserting identical outcomes — **unmodified and modified caster**:

- **chain_lightning:** extend `TestAbilityCompileGolden_ChainLightning`. Assert
  identical total damage, per-hop damage, hop count, victim set, beam count, and
  "no Projectile spawned," for casters with 0 / partial / full spell-mod
  loadouts.
- **siphon_life:** extend `ability_compile_golden_channel_test.go`. Assert
  identical per-tick damage, heal distribution (self/ally/dark_renewal),
  mana drain, and channel duration — for a plain caster AND a Siphoner carrying
  each of chain_siphon / shared_suffering / withering_beam / repurposed_life /
  dark_renewal / soul_leech / beam_mastery.

No test pins a balance number literally; expected values derive from the catalog
def / legacy fixture (per the no-hardcoded-tunables rule).

---

## Part 5 — Editor / schema

- `ActionLaunchBeam` gets an `ActionFieldSchema` (variant, spawnOrigin,
  impactDelay, duration + a `target` target_query with
  `targetQueryFieldsSourceOnly`, and the nested-trigger slot).
- Add `launch_beam` to `CONFIG_TRIGGER_ACTION_TYPES` (client `programTree.ts`)
  and the Go equivalent so `on_beam_impact` routes into `config.triggers`.
- Register `on_beam_impact` / `on_beam_tick` in the TS `TriggerType` union +
  Go `TriggerType` enum + the hand-maintained trigger lists
  (`ProgramEnums.triggerTypes`, validator).
- `describeAbility` becomes beam-aware (the generated tooltip must describe the
  chain and the drain from the authored program, not the retired flat fields).
- Add a `launch_beam` action icon (`action-icons.json`, `ActionIcon.vue`).

---

## Open questions (resolve in Task 0, before writing runtime code)

1. **Visited-set exclusion** for chain bounces — does existing query
   filtering already exclude a named context, or is a small `filter_targets`
   extension required? Does legacy bounce forbid re-hits (making the set
   mandatory) or allow them?
2. **Siphoner damage-mult fold** — is it a spell-mod (picked up by authored
   `deal_damage`) or a channel-only perk scaler? Determines whether the loop or
   the trigger owns the number.
3. **Executor `deal_damage` applied-amount readback** — does the action already
   surface how much it dealt (for the perk seam), or must we add an output?
4. **`fireAbilityChainLocked` sole caller** — confirm only chain_lightning
   routes through it and equipment procs do not, so it can be deleted.

---

## Phased breakdown (→ convert to an executing plan after review)

- **Task 0 — Recon spikes.** Answer the four open questions with code
  references + throwaway probes. No production code.
- **Task 1 — `launch_beam` action + `on_beam_impact` trigger + Beam
  `ImpactActions`.** New action, new trigger, beam-entity impact-actions path,
  op budget. Unit tests for a single non-chaining beam (spawn → impact → damage).
- **Task 2 — chain_lightning authored program + compiler unroll.** Replace the
  shim; golden parity (unmodified + modified). Retire `fireAbilityChainLocked`
  and the `launchProjectileConfig` chain fields.
- **Task 3 — `on_beam_tick` trigger + siphon_life base-effect authoring.**
  Loop fires the authored tick trigger; capture applied damage; perks read it.
  Golden parity for a plain caster.
- **Task 4 — siphon_life perk parity.** Golden parity across all seven
  Siphoner perks; fold-once verification.
- **Task 5 — Editor/schema/describe + icon.** Schema, config-trigger routing,
  trigger enums, beam-aware describe, action icon. Client tests.
- **Task 6 — Cleanup + audit.** Remove dead legacy fields; update the catalog
  honesty status (target: 11 genuine, 0 shims for these two).
