## 1. Investigation

- [x] 1.1 Confirm the two grant files exist (`catalog/units/human/apprentice/paths/{cleric,arch_mage}/abilities/silver.json`) and that `pathAbilityGrantsFor` / `assignUnitPathAbilitiesLocked` read them; confirm no other `paths/*/abilities/*.json` exist
- [x] 1.2 In `caster_phase2_test.go`, identify exactly which tests depend on the real grant files vs. which set `unit.Abilities` directly (the latter are unaffected); list the synthetic-fixture seam (`pathAbilityGrantsByKey` + `pathModifierKey`)
- [x] 1.3 Confirm no archived Phase-2 file and no engine file (`ability_priority.go`, `path_ability_defs.go`, `tickUnitAutoCastLocked`, `AbilityDef.DamageAmount`) needs to change

## 2. Remove Grant Content

- [x] 2.1 Delete `catalog/units/human/apprentice/paths/cleric/abilities/silver.json` and `catalog/units/human/apprentice/paths/arch_mage/abilities/silver.json`
- [x] 2.2 Remove the now-empty `cleric/abilities/` and `arch_mage/abilities/` directories
- [x] 2.3 Build + force catalog init; confirm it loads without panic and `pathAbilityGrantsFor` returns empty for every `(path,rank)`

## 3. Mark Dormant Defs

- [x] 3.1 Add an additive top-level `description` to `catalog/abilities/greater_heal/greater_heal.json` stating it is dormant/unwired pending a future perk-gated replacement of `heal` (gating perk TBD by user); change no other field
- [x] 3.2 Add an additive top-level `description` to `catalog/abilities/arcane_bolt/arcane_bolt.json` stating it is dormant/unwired pending TBD acquisition; change no other field
- [x] 3.3 Build + force catalog init; confirm both defs still load, validate (`Category`/`DamageType`), and are resolvable via `getAbilityDef`

## 4. Test Rework (synthetic-fixture)

- [x] 4.1 Delete `TestPhase2_PromotionGrant_ClericGetHealLine` and `TestPhase2_PromotionGrant_ArchMageGetOffensive`
- [x] 4.2 Remove/adjust the grant-file-dependent portions of `TestPhase2_GrantedAbilityInSnapshot`, `TestPhase2_PromotionGrant_MultiRankCatchupNoDuplicates`, `TestPhase2_PromotionGrant_Idempotent`, `TestPhase2_PromotionGrant_RNGFree` so nothing asserts the deleted real content
- [x] 4.3 Add synthetic-fixture grant-engine tests: inject a synthetic `(path,rank)` grant (set `pathAbilityGrantsByKey[pathModifierKey(p,r)]` to a synthetic id backed by a synthetic in-test `AbilityDef`, with cleanup/restore) and assert `assignUnitPathAbilitiesLocked` (a) appends granted ids in order, (b) is idempotent (append-iff-absent on re-invocation), (c) multi-rank catch-up applies each crossed rank with no duplicates, (d) is RNG-free (two seeded runs → identical `unit.Abilities`)
- [x] 4.4 Confirm priority / tiebreak / `DamageAmount` tests are untouched and still pass (they set `unit.Abilities` directly; dormant defs still resolve)
- [x] 4.5 No hardcoded balance/tunable numbers — derive from defs/constants per the project rule

## 5. Record Deferred Direction

- [x] 5.1 Update the `project_caster_archetype_design` memory: greater_heal/arcane_bolt grants removed; defs dormant; acquisition deferred (greater_heal = perk-gated replacement of `heal`, perk TBD; arcane_bolt TBD); Phase 2 engine retained
- [x] 5.2 Confirm this change's `design.md` "Deferred Design Direction" section captures the same (already written)

## 6. Verification & Sign-off

- [x] 6.1 `go build ./...` and `go vet ./internal/game/` clean
- [x] 6.2 Full server suite `go test ./...` (in `server/`) green — the no-regression gate
- [x] 6.3 Confirm diff touches only the 2 deleted grant files, the 2 dormant-def `description` edits, `caster_phase2_test.go`, and the memory; no engine/protocol/client/archived-Phase-2 files
- [x] 6.4 Re-read the proposal acceptance: grants gone, dormant defs still load+validate, `per-path-ability-kits` canonical spec amended at archive-time sync, suite green, archived Phase 2 untouched
