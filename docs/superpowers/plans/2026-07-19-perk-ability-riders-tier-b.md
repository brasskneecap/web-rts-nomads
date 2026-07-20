# Perk→Ability Riders (Tier B) + Perk-Editor Surface — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a perk graft composable action fragments onto another ability's trigger ("riders"), pilot it by expressing the Siphoner's `shared_suffering` as data, and give BOTH Tier A scalar modifiers and Tier B riders a real editing surface in the Perk editor.

**Architecture:** A rider is `{ target: <abilityId>, trigger: <triggerType>, actions: [...AbilityActionDef] }` stored on `PerkDef`. At runtime, a generic `runAbilityRidersForCasterLocked` gathers every owned perk's rider fragments for a fired `(ability, trigger)` pair and runs their actions in deterministic order, seeded with the triggering event's context (target, tick damage). Scalars (Tier A, already built) compose multiplicatively; riders (this) compose additively — N perks modifying one ability is fragment concatenation + scalar multiplication, no pairwise wiring. The Perk editor reuses the Ability builder's `FlowActionCard`/`SchemaField`/`TargetQueryEditor` widgets so riders are authored in the identical vocabulary the Ability editor uses.

**Tech Stack:** Go (server sim, `internal/game`), Vue 3 + TypeScript (`client/src/game-portal`). Existing composable-ability program model (schemaVersion 2: `triggers[] → actions[]`, `actionRegistry`).

**Scope note (decided):** Pilot = the rider MECHANISM + `shared_suffering` migrated to FULLY-DATA rider + editor surface. `chain_siphon` and `dark_renewal` need NEW engine action primitives (chain-target, heal-overflow→shield) and stay Go. `withering_beam` (stacking status) is the next rider after this lands.

**Perk-modifies-perk deferral (decided after investigation):** `ascended_corruption` (Gold) overlays `shared_suffering` with `sharedRadiusMultiplier:1.5` AND `sharedDamageSharePercentBonus:0.2`. A `TargetQueryDef.Radius` is a static `float64` on the action's `Target` field (NOT in `Config`), so the loop-var substitution can't inject an effective radius — a fully-data byte-identical rider for the Gold combo is impossible without a SECOND new primitive (target-query radius refs). Rather than special-case the generic runner with `shared_suffering`-specific glue, the rider authors BASE values (radius 120, share 0.4) as pure editable data, and the `ascended_corruption` overlay becomes the concrete first driver of the "perk-modifies-perk" mechanism (Tier B.5). Consequence: for a siphoner owning BOTH `shared_suffering` and `ascended_corruption`, the Gold overlay is temporarily inert (radius stays 120, share stays 0.4). This is a documented, bounded behavior change — the ONLY behavior delta in this pilot — flagged in the characterization test and memory, NOT hidden. Base `shared_suffering` (the common case) stays byte-identical.

---

## File Structure

**Backend (`server/internal/game/`):**
- `perk_defs.go` — MODIFY: add `AbilityRider` struct + `PerkDef.AbilityRiders []AbilityRider`; validate.
- `ability_riders.go` — CREATE: `runAbilityRidersForCasterLocked` (the generic runner) + `ownedRiderFragmentsForLocked` (deterministic gather).
- `ability_channel.go` — MODIFY: at the `shared_suffering` hook site (~516), call the rider runner for `(unit.ChannelAbilityID, on_beam_tick)`; remove the `applySharedSufferingLocked` call once the rider is proven.
- `perks_siphoner.go` — MODIFY (last): delete `applySharedSufferingLocked` + `sharedSufferingEffectiveConfigLocked`'s now-unused base branch (keep the Gold overlay accessor the runner consults).
- `catalog/perks/siphoner/shared_suffering/shared_suffering.json` — MODIFY: add the `abilityRiders` entry.
- `ability_riders_test.go` — CREATE: multi-perk composition + determinism.
- `perks_siphoner_shared_suffering_migration_test.go` — CREATE: characterization (byte-identical echo).

**Frontend (`client/src/game-portal/src/`):**
- `game/perks/perkEditorForm.ts` — MODIFY: model `abilityModifiers` + `abilityRiders` (add to `MODELED_KEYS`, type them).
- `components/PerkEditorPanel.vue` — MODIFY: add "Ability Modifiers" section (rows) + "Ability Riders" section (target+trigger+embedded action list).
- `components/perk-editor/RiderEditor.vue` — CREATE: one rider's editor (target ability select, trigger select, action list) reusing ability-builder widgets.
- Reuse (no change): `components/ability-builder/FlowActionCard.vue`, `SchemaField.vue`, `TargetQueryEditor.vue`, `composables/useAbilityBuilder.ts` mutation helpers / `programTree.ts`.

---

## Task 1: Rider schema on PerkDef (backend)

**Files:**
- Modify: `server/internal/game/perk_defs.go`
- Test: `server/internal/game/ability_riders_test.go` (create)

- [ ] **Step 1 — failing test:** a `PerkDef` JSON with an `abilityRiders: [{ target, trigger, actions: [...] }]` entry decodes into `def.AbilityRiders` with the target/trigger/one action preserved; a rider with an empty `target` OR an unknown `trigger` OR that fails the existing program action validator is reported by perk validation.
- [ ] **Step 2 — run it, confirm it fails** (field/struct undefined).
- [ ] **Step 3 — implement:** add
  ```go
  type AbilityRider struct {
      Target  string             `json:"target"`
      Trigger TriggerType        `json:"trigger"`
      Actions []AbilityActionDef `json:"actions,omitempty"`
  }
  ```
  Add `AbilityRiders []AbilityRider \`json:"abilityRiders,omitempty"\`` to `PerkDef`. In perk validation: reject empty `Target`; reject a `Trigger` not in the known trigger set (reuse the enum source `ProgramEnums()`/the `TriggerType` consts); validate each action through the SAME program action validator the ability editor uses (`ability_program_validate.go`). Mirror how `AbilityModifiers` validation was added.
- [ ] **Step 4 — run tests, confirm pass.**
- [ ] **Step 5 — commit.** (Controller note: user handles all commits — do NOT run git commit; leave changes staged-in-working-tree.)

**Context for implementer:** `AbilityModifier`/`PerkDef.AbilityModifiers` were just added in this same file — follow that exact pattern for field + validation placement. `AbilityActionDef` is defined in `ability_program.go`. The program action validator lives in `ability_program_validate.go` (`for i, action := range trig.Actions` at ~line 100 is the per-action entry point).

---

## Task 2: Generic rider runner + multi-perk composition (backend)

**Files:**
- Create: `server/internal/game/ability_riders.go`
- Test: `server/internal/game/ability_riders_test.go` (extend)

- [ ] **Step 1 — failing test (composition + determinism):** a caster owning TWO perks, each with a rider on `(ability="test_ability", trigger=on_beam_tick)` whose action is a `deal_damage` to the caster's target, produces BOTH damage applications in one runner call; and the actions execute in a deterministic order independent of `PerkIDs` slice order (sort key = perk id). Assert order by a trace/side-effect that distinguishes the two.
- [ ] **Step 2 — run it, confirm it fails.**
- [ ] **Step 3 — implement:**
  ```go
  // ownedRiderFragmentsForLocked returns, in deterministic (perk-id-sorted)
  // order, every AbilityRider on the caster's owned perks that targets
  // (abilityID, trigger). Deterministic order is required — sim code must not
  // depend on PerkIDs slice/map order.
  func (s *GameState) ownedRiderFragmentsForLocked(caster *Unit, abilityID string, trigger TriggerType) []AbilityRider

  // runAbilityRidersForCasterLocked runs every owned-perk rider fragment for
  // (abilityID, trigger). ctx is seeded with the primary target and the
  // triggering event's damage bound as a named context value ("trigger_damage")
  // so a rider deal_damage can express a fraction of it. Each fragment runs its
  // actions via s.executeActionLocked, in the runner's own context (NOT the base
  // trigger's ctx) so it can't corrupt fireChannelBeamTickLocked's returned
  // tickDamage. No-op when the caster owns no matching rider.
  func (s *GameState) runAbilityRidersForCasterLocked(caster, target *Unit, abilityID string, trigger TriggerType, triggerDamage int)
  ```
  Bind `ctx.Named["trigger_damage"] = ContextValue{Kind: ctxScalar, Scalar: float64(triggerDamage)}` (mirrors `runLoopBodyLocked`'s var binding, ability_exec_loop.go:102). Build the `RuntimeAbilityContext` the same way `fireChannelBeamTickLocked` does (CasterID/AbilityID/InitialTarget/CurrentEventUnitID/program/abilityDef/Named). Run each fragment's actions with the `opsExhausted()` guard, exactly like `runTriggerActionsLocked` (ability_exec_loop.go:61).
- [ ] **Step 4 — run tests, confirm pass.**
- [ ] **Step 5 — commit** (staged only).

**Context for implementer:** Determinism rule (AI_RULES.md): no map-iteration-order-driven outcomes in sim. Sort fragments by owning perk id. `RuntimeAbilityContext` shape is in `ability_exec.go:35`. `executeActionLocked` + `opsExhausted` are the existing action-run primitives.

---

## Task 3: deal_damage amount from bound context (backend)

**Files:**
- Modify: `server/internal/game/ability_program_registry.go` (deal_damage decode/execute) OR a shared config-substitution pass — implementer picks the seam the loop-var path already uses.
- Test: `server/internal/game/ability_riders_test.go` (extend)

- [ ] **Step 1 — failing test:** a `deal_damage` action inside a rider, configured to deal a fraction of `trigger_damage` (e.g. 40%), applies `round(triggerDamage * 0.4)` when `trigger_damage` is bound in ctx.Named, and its own `ctx.lastAppliedDamage` does NOT leak back to the base tick (runner uses a separate ctx — already true from Task 2).
- [ ] **Step 2 — run it, confirm it fails.**
- [ ] **Step 3 — implement:** determine how the EXISTING `deal_damage amount: "a"` loop-var reference resolves a named scalar into the int `Amount` (there is a config-templating/substitution step — `dealDamageConfig.Amount` is a plain `int`, so a string ref must be substituted pre-decode; see the comment at `ability_program_registry.go:410-412` and `loopVarValue`). Reuse that SAME mechanism so a rider's `deal_damage` can reference `trigger_damage` with a multiplier. Prefer the smallest extension that reuses the loop-var substitution rather than a new bespoke ref field. If the existing mechanism only supports a bare variable (no ×mult), add a `mult` alongside the ref (or bind the pre-multiplied value in the runner as a second named var `share_amount` — the runner already computes context, so binding `round(triggerDamage * sharePct)` directly as a scalar the rider references is the LEAST-invasive option; choose this if the ref mechanism doesn't cleanly carry a multiplier).
- [ ] **Step 4 — run tests, confirm pass.**
- [ ] **Step 5 — commit** (staged only).

**Context for implementer:** The genuinely simplest behavior-preserving route: the runner binds `share_amount = round(triggerDamage * effectiveSharePct)` as a named scalar and the rider's `deal_damage` references THAT — pushing the arithmetic into the runner (Go) and keeping `deal_damage` unchanged. This is acceptable and preferred if extending `deal_damage`'s ref parsing is more than trivial. Document whichever seam you choose.

---

## Task 4: Migrate shared_suffering to a rider; prove byte-identical; delete Go (backend)

**Files:**
- Modify: `server/internal/game/ability_channel.go` (~516), `catalog/perks/siphoner/shared_suffering/shared_suffering.json`, `perks_siphoner.go`
- Test: `server/internal/game/perks_siphoner_shared_suffering_migration_test.go` (create)

- [ ] **Step 1 — characterization test FIRST (against current Go):** pin `applySharedSufferingLocked`'s observable effect for a scenario matrix: {owns / doesn't own shared_suffering} × {with / without soul_leech damage scaling}, all WITHOUT `ascended_corruption` (the base case, which must stay byte-identical). Assert: exact echo damage per neighbor, exactly which neighbors are hit (base radius 120), and that the primary target is excluded. Derive expected values from the perk JSON config (NO hardcoded balance numbers per AI_RULES / user rule). Run green against current Go. ALSO add a SEPARATE test that documents the deferred Gold overlay: with `ascended_corruption` owned, current Go applies radius×1.5 + share+0.2 — assert that value now, and leave a loud comment that after the rider migration this combo temporarily loses the overlay (Tier B.5 restores it). This test will be UPDATED in step 3 to the new (base) behavior.
- [ ] **Step 2 — author the rider data** in `shared_suffering.json` with BASE values (fully editable, no Go bridge):
  ```json
  "abilityRiders": [{
    "target": "siphon_life",
    "trigger": "on_beam_tick",
    "actions": [
      { "id": "echo_targets", "type": "select_targets",
        "target": { "source": "all_in_scene", "origin": "initial_target_position",
                    "relations": ["enemy"], "radius": 120,
                    "excludeCurrentEvent": true, "aliveState": "alive" } },
      { "id": "echo_dmg", "type": "deal_damage",
        "config": { "amountRef": "trigger_damage", "amountMult": 0.4, "type": "shadow" } }
    ]
  }]
  ```
  (Exact `select_targets` query keys: match how existing splash queries author "enemies around the primary target, excluding it" — see `compileMeteorActions`/`compileProjectileImpactTrigger` and the `all_in_scene` + origin/exclude pattern in `ability_compile.go`. The intent: every visible hostile within 120 of the primary target, primary excluded.) The runner binds `trigger_damage` (raw tick damage); `deal_damage`'s `amountRef` reads it RAW and applies `amountMult` (see T3). Keep the `Kind:"shared_suffering"` damage-source tag. The minor-damage popup + shadowburst VFX: if the composable path can't reproduce them exactly, note the delta explicitly — VFX/popup cosmetics are a documented acceptable diff; DAMAGE and TARGET SET must be byte-identical for the base case.
- [ ] **Step 3 — wire the runner, keep Go OFF:** at ability_channel.go ~516 replace `s.applySharedSufferingLocked(...)` with `s.runAbilityRidersForCasterLocked(unit, target, unit.ChannelAbilityID, TriggerOnBeamTick, tickDamage)`. Run the characterization test — it must stay green through the rider path.
- [ ] **Step 4 — delete** `applySharedSufferingLocked` and the recursion-guard field usage if now dead; keep `sharedSufferingEffectiveConfigLocked` (runner uses it). Run `go test ./...`.
- [ ] **Step 5 — commit** (staged only).

**Context for implementer:** Current Go: `applySharedSufferingLocked` (perks_siphoner.go:1140), called at ability_channel.go:516 with `tickDamage`. It echoes `round(primaryDamage * sharePct)` to visible hostiles within `radius` of the primary target, excluding it, tagged `Kind:"shared_suffering"`, with a minor-damage popup + shadowburst VFX. `TriggerOnBeamTick` const is in ability_program.go:55. If any behavior can't be expressed purely in data yet, the rider may consult Go helpers via the runner — flag every such bridge as tech debt in the memory file, do not hide it.

---

## Task 5: Model abilityModifiers + abilityRiders in the editor form (frontend)

**Files:**
- Modify: `client/src/game-portal/src/game/perks/perkEditorForm.ts`
- Test: colocated `*.spec.ts` if the project has frontend unit tests; otherwise assert via `vue-tsc -b` type-check + a round-trip check in the panel.

- [ ] **Step 1:** add `abilityModifiers?: AbilityModifier[]` and `abilityRiders?: AbilityRider[]` to `AuthoredPerkDef`, with TS types mirroring the Go structs (modifier: `{ target: string } & partial mults`; rider: `{ target: string; trigger: string; actions: AbilityActionDef[] }` — reuse the ability-builder's existing action TS type). Add both keys to `MODELED_KEYS`.
- [ ] **Step 2:** confirm `formFromDef`/`saveRequestFromForm` round-trip both (they will once modeled; previously they fell into `remainder`).
- [ ] **Step 3:** `vue-tsc -b` clean.
- [ ] **Step 4 — commit** (staged only).

**Context:** `MODELED_KEYS` and the `remainder` bucket are at perkEditorForm.ts:28-61. The ability action TS type is used by `useAbilityBuilder.ts` — import/reuse it, do not redefine.

---

## Task 6: "Ability Modifiers" editor section (frontend)

**Files:**
- Modify: `client/src/game-portal/src/components/PerkEditorPanel.vue`

- [ ] **Step 1:** add a new `<section>` "Ability Modifiers": a list of rows, each row = a target-ability `<input>`/datalist (ability ids, fetched like the existing ability datalist) + numeric inputs for the mult fields (damageMult, healMult, manaCostMult, rangeMult, cooldownMult, radiusMult, durationMult). Mirror the EXISTING config-rows pattern (`configRows`/`rowsFromMap`/`addConfigRow`, PerkEditorPanel.vue:173-186): a `watch(deep)` writes back to `form.value.abilityModifiers`. Leave a mult blank/0 = omit (identity).
- [ ] **Step 2:** load an existing perk with an `abilityModifiers` entry (e.g. `beam_mastery`) → the section renders its values; edit + save → round-trips through `saveRequestFromForm`.
- [ ] **Step 3:** `vue-tsc -b` clean. No literal `cursor:` declarations (CLAUDE.md rule).
- [ ] **Step 4 — commit** (staged only).

**Context:** Do NOT add component-level `cursor:` CSS. Follow the config-rows idiom already in the file. `beam_mastery`/`soul_leech` are real perks with `abilityModifiers` to test against.

---

## Task 7: "Ability Riders" editor section reusing ability-builder widgets (frontend)

**Files:**
- Create: `client/src/game-portal/src/components/perk-editor/RiderEditor.vue`
- Modify: `client/src/game-portal/src/components/PerkEditorPanel.vue`

- [ ] **Step 1 — RiderEditor.vue:** props = one `AbilityRider` (v-model). Renders: target-ability select (datalist of ability ids); trigger select whose options are the chosen ability's REAL triggers (fetch the ability's program and list its trigger types, falling back to `ProgramEnums().triggerTypes`); and an action list authored with the SAME widgets the Ability builder uses — `FlowActionCard.vue` per action + a "+ Action" using the action-type enum, driven by `SchemaField.vue`/`TargetQueryEditor.vue`. Reuse `useAbilityBuilder.ts`'s `NodePath` mutation helpers (`addAction`/`removeAction`/`moveAction`/`updateActionConfig`) against the rider's `actions` array, or the smallest adaptation thereof.
- [ ] **Step 2 — PerkEditorPanel.vue:** add an "Ability Riders" `<section>`: list of `RiderEditor` (one per rider) + "+ Rider" / remove. Bind to `form.value.abilityRiders`.
- [ ] **Step 3:** load `shared_suffering` (now carrying an `abilityRiders` entry) → the section renders target `siphon_life`, trigger `on_beam_tick`, and the `select_targets`/`deal_damage` actions; edit an action's config → writes back; save → round-trips.
- [ ] **Step 4:** `vue-tsc -b` clean; no literal `cursor:` CSS.
- [ ] **Step 5 — commit** (staged only).

**Context:** The reusable authoring widgets and their state API were mapped in recon: `AbilityFlow.vue`/`FlowTriggerCard.vue`/`FlowActionCard.vue`/`SchemaField.vue`/`TargetQueryEditor.vue`, `useAbilityBuilder.ts` (`addAction` line ~183, `removeAction` ~191, `moveAction` ~195, `updateActionConfig` ~223) over `programTree.ts` `NodePath`. A rider's `actions` is a flat `AbilityActionDef[]` (one trigger's action list) — simpler than the full program tree, so scope the reuse to the action-list portion.

---

## Task 8: Final review + docs

- [ ] Full `go test ./...` green; `vue-tsc -b` clean.
- [ ] Update memory `project_perk_ability_modifiers.md`: Tier B rider mechanism DONE, shared_suffering piloted, the `share_amount`/effective-config Go bridge + the `ascended_corruption→shared_suffering` perk-modifies-perk item flagged as Tier B.5 next; editor now surfaces both modifiers and riders.
- [ ] Dispatch a final code-reviewer over the whole diff.

---

## Self-review checklist (controller, before executing)
- Spec coverage: mechanism (T1-T3), pilot migration (T4), editor for BOTH modifier kinds (T5-T7) — matches "push into Tier B" + "editable via Perk editor" + "two perks compose" (T2 proves additive composition; Tier A already multiplies).
- Determinism: T2 sorts fragments by perk id — no map-order dependence.
- No hidden behavior change: T4 is characterization-gated byte-identical; cosmetic VFX/popup deltas allowed only if explicitly documented.
- Deferred & named: chain_siphon/dark_renewal (new primitives), withering_beam (next rider), ascended_corruption→shared_suffering (perk-modifies-perk, Tier B.5).
