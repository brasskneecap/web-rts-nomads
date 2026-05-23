## Why

Phase 2 (archived) shipped two **placeholder** caster abilities — `greater_heal` and `arcane_bolt` — wired into unconditional Cleric / Arch Mage promotion grants. Those grants were assumed content authored to make the new systems exercisable, not a deliberate design. The intended acquisition models are now known to differ and are **explicitly deferred by the user**: `greater_heal` is to be a *perk-gated replacement* of base `heal` (the gating perk is undesigned/TBD), and `arcane_bolt`'s acquisition is also TBD. So the shipped behaviour (a promoted Cleric auto-gains `greater_heal` *alongside* `heal`) contradicts intent. This change removes the speculative content so the codebase stops asserting an unintended design, while keeping the sound, mechanism-agnostic Phase 2 engine fully intact and tested.

## What Changes

- **Remove the two grant files** `catalog/units/human/acolyte/paths/{cleric,arch_mage}/abilities/silver.json` (and the now-empty `abilities/` directories). No ability is auto-granted on promotion anymore.
- **Keep `greater_heal.json` / `arcane_bolt.json` as dormant defs** — data unchanged so they still load and validate, with an added header `description` marking them deliberately dormant/unwired and recording the intended (deferred) acquisition: `greater_heal` = future perk-gated replacement of `heal`; `arcane_bolt` = TBD.
- **Rework Phase 2 tests (synthetic-fixture approach):** delete the content-asserting tests that depend on the real grant files (`TestPhase2_PromotionGrant_ClericGetHealLine`, `TestPhase2_PromotionGrant_ArchMageGetOffensive`, and the grant-file-dependent portions of `GrantedAbilityInSnapshot` / `MultiRankCatchupNoDuplicates` / `Idempotent` / `RNGFree`). Replace with grant-engine tests driven by an **in-test synthetic grant** so `assignUnitPathAbilitiesLocked` ordering / append-iff-absent / multi-rank catch-up / RNG-free determinism stay covered with zero dependence on authored content. Priority / tiebreak / `DamageAmount` tests are unaffected (they set `unit.Abilities` directly; the dormant defs still exist).
- **Amend the `per-path-ability-kits` capability:** soften the "Cleric and Arch Mage starter kits are authored" requirement to "the grant mechanism exists and is fixture-tested; no real abilities are granted yet; acquisition deferred". All other requirements of that capability (loader, deterministic idempotent grant, granted-ability-in-snapshot, `DamageAmount`-on-resolve) are unchanged and still hold.
- **Record the deferred design direction** in this change's `design.md` and in project memory.
- **Out of scope (deliberately):** designing the gating perk, designing `arcane_bolt`'s acquisition, any change to the Phase 2 engine, and any edit to the archived Phase 2 change.

## Capabilities

### New Capabilities

<!-- None. -->

### Modified Capabilities

- `per-path-ability-kits`: The "Cleric and Arch Mage starter kits are authored" requirement is replaced — the mechanism is retained and fixture-tested, but no real abilities are granted and acquisition is explicitly deferred. The capability's other requirements (loader, idempotent ordered grant, snapshot surfacing, offensive `DamageAmount` on resolve) are unchanged.

## Impact

- **Catalog (JSON)**: delete `catalog/units/human/acolyte/paths/cleric/abilities/silver.json` and `.../arch_mage/abilities/silver.json` (+ empty `abilities/` dirs). Edit `catalog/abilities/greater_heal/greater_heal.json` and `catalog/abilities/arcane_bolt/arcane_bolt.json` (additive `description` only — still valid, still load).
- **Tests (Go)**: `server/internal/game/caster_phase2_test.go` — remove content-coupled grant tests, add synthetic-fixture grant-engine tests. Full server suite remains the gate.
- **Spec**: `openspec/specs/per-path-ability-kits/spec.md` — one requirement softened (MODIFIED delta); rest untouched.
- **No engine / protocol / client / determinism impact**: no tick-loop logic changes, no `*Locked`/ID-targeting changes, data + test changes only.
- **Not touched**: the archived Phase 2 change, the Phase 2 engine files (`ability_priority.go`, `path_ability_defs.go`, `tickUnitAutoCastLocked`, `AbilityDef.DamageAmount`), and the `ability-priority-selection` / `ability-category` canonical specs.
