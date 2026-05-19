## 1. Investigation & Anchor Verification

- [x] 1.1 Re-confirm `tickUnitAutoCastLocked` (ability_autocast.go ~:74-112) first-ready structure and its exact gate set (autocast-enabled, `SupportsAutoCast`, `AbilityCooldowns[id] <= 0`, `CurrentMana >= def.ManaCost`, non-nil `resolveAutoCastTargetLocked`), the pre-flight guards, and the "one cast/unit/tick, not while `CastAbilityID != ""`" rules
- [x] 1.2 Re-confirm `resolveAutoCastTargetLocked` / the selector registry (autocast_selectors.go) and that `closest_enemy_in_range` (`selectClosestEnemyInRange`) is registered and returns a valid within-tick `*Unit`
- [x] 1.3 Re-confirm the grant seam: `addUnitXPLocked` (progression.go ~:232-260) calls `assignUnitPerkLocked` per crossed rank; confirm `assignUnitPerkLocked` (perks.go ~:610) as the structural twin and how it resolves the unit's path+rank
- [x] 1.4 Re-confirm `path_defs.go` loader pattern (`//go:embed catalog/units`, `(path,rank)` keying, `bronze/silver/gold` validation panic) to mirror in `path_ability_defs.go`
- [x] 1.5 Re-confirm `abilityStatesLocked` builds `AbilitySnapshot` from `unit.Abilities` (so a granted ability needs no protocol change) and that `beginAbilityCastLocked` stores `unit.CastTargetID int` (ID-based; AI_RULES)
- [x] 1.6 Confirm the canonical specs to delta against exist (`openspec/specs/ability-category/spec.md`, `caster-combat-profile/spec.md`); list every existing autocast-enabled ability `Category` value (to size the empty/unknown fallback correctly)

## 2. Path Ability Loader (`path_ability_defs.go`)

- [x] 2.1 Create `path_ability_defs.go` mirroring `path_defs.go`: embed FS, parse `catalog/units/human/apprentice/paths/<path>/abilities/<rank>.json` of shape `{ "grant": ["<id>", …] }` into `pathAbilityGrantsByKey` keyed by `(path,rank)`
- [x] 2.2 Validate at load: unknown rank dir → panic (naming file+rank); a granted id with no registered `AbilityDef` → panic (naming file+id); missing file → empty grant (no error)
- [x] 2.3 Add an accessor (twin of the `path_defs.go` getter) returning the ordered grant slice for a `(path,rank)`
- [x] 2.4 Build; confirm package compiles and existing catalog still loads (no grant files yet → all cells empty)

## 3. Promotion Grant (`assignUnitPathAbilitiesLocked`)

- [x] 3.1 Add `assignUnitPathAbilitiesLocked(unit)`: resolve `(path,rank)`, append each granted id to `unit.Abilities` **iff not already present**, in catalog order; no RNG
- [x] 3.2 Call it from `addUnitXPLocked` immediately after `s.assignUnitPerkLocked(unit)` inside the per-rank loop
- [x] 3.3 Confirm granted spell abilities initialise autocast/cooldown state lazily exactly as base abilities (no spawn-path change required)
- [x] 3.4 Build + run existing server suite (no grant files authored yet → behaviour unchanged; this is a pre-kit regression baseline)

## 4. Priority Scoring Engine (`ability_priority.go`)

- [x] 4.1 Create `ability_priority.go` with `scoreAutoCastCandidateLocked(unit, def, target) float64` switching on `def.Category`
- [x] 4.2 Implement `heal` (ally HP-deficit + bounded nearby-damaged-allies bonus), `offensive` (target value / cluster / finishing — reuse existing scoring helpers where present), `buff_ally`, `summon` formulas per design.md Decision 2
- [x] 4.3 Implement the empty/unknown-`Category` fallback so a lone valid-target candidate scores strictly above `minActivationScore` (Decision 3 — protects the no-regression invariant for uncategorised abilities)
- [x] 4.4 Define `minActivationScore` and category-weight table (small Go map keyed by `AbilityCategory`, NOT on `CombatProfile`); document each constant as a placeholder pending the balance pass
- [x] 4.5 Verify scoring reads only live unit/world state + ordered slices — no map iteration, no wall-clock, no unseeded RNG

## 5. Autocast Rework (`tickUnitAutoCastLocked`)

- [x] 5.1 Restructure to gather → score → pick: gather candidates with the **unchanged** gate set + slot index; score each via `scoreAutoCastCandidateLocked`; pick max
- [x] 5.2 Deterministic tiebreak: ascending slot index, then ability id; below `minActivationScore` → cast nothing
- [x] 5.3 Preserve verbatim: pre-flight guards, "one cast/unit/tick", "never while `CastAbilityID != ""`", cooldown arming on the chosen cast, `beginAbilityCastLocked` call shape
- [x] 5.4 Build + `go vet`

## 6. Ability `DamageAmount` Primitive + Author Cleric / Arch Mage Kits

(`DamageAmount` surfaced during implementation — Decision 7. The offensive ability needs it to be non-hollow; it lands before the Arch Mage def is authored.)

- [x] 6.1 Add `DamageAmount int \`json:"damageAmount,omitempty"\`` to `AbilityDef` (next to `HealAmount`)
- [x] 6.2 In `resolveAbilityCastLocked`, after the `HealAmount` step, apply `if def.DamageAmount > 0 && target.HP > 0 { s.applyUnitDamageWithSourceLocked(target, def.DamageAmount, DamageSource{AttackerUnitID: caster.ID, Kind: "ability", DamageType: def.DamageType.OrPhysical()}) }`. No new enum/validation (plain magnitude like `HealAmount`)
- [x] 6.3 Build + `go vet`; confirm inert for existing abilities (only `heal`, no `damageAmount`)
- [x] 6.4 Author the Cleric heal-line ability def(s) (e.g. `catalog/abilities/greater_heal/greater_heal.json`) with `category: "heal"`, `supportsAutoCast`, `autoCastTargetSelector: "lowest_hp_percentage_ally_in_range"`, mana/cooldown/castRange per existing conventions
- [x] 6.5 Author the Arch Mage offensive ability def(s) with `category: "offensive"`, `damageAmount`, a `damageType`, `supportsAutoCast`, and `autoCastTargetSelector: "closest_enemy_in_range"`
- [x] 6.6 Author the grant files: `catalog/units/human/apprentice/paths/{cleric,arch_mage}/abilities/{bronze,silver,gold}.json` (`{ "grant": [...] }`) — ≥1 Cleric heal-line, ≥1 Arch Mage offensive
- [x] 6.7 Build + start: confirm catalog loads without panic (validates 2.2 ability-id validation and the new defs)

## 7. Tests

The full server suite is the no-regression gate. The no-regression equivalence (7.1) MUST pass before the multi-ability tests are trusted.

- [x] 7.1 **No-regression gate:** seeded-replay equivalence — a heal-only (un-promoted) Apprentice casts heal on the identical set of ticks pre/post the autocast rework, same seed+inputs. Also assert the gather phase reproduces the exact prior gate set for a single candidate
- [x] 7.2 Test: a lone autocast ability with empty/unregistered `Category` still fires (fallback ≥ `minActivationScore`)
- [x] 7.3 Test: highest-scored ready ability wins over an earlier slot; gated (cooldown/mana/no-target) abilities are never scored/cast; one cast/unit/tick; not while casting
- [x] 7.4 Test: deterministic tiebreak (equal scores → lower slot index, then smaller id); below `minActivationScore` → casts nothing
- [x] 7.4a Test (review follow-up): real-tick tiebreak — two equal-scoring abilities (`heal`+`greater_heal`) driven through `tickUnitAutoCastLocked`; both slot orderings asserted (lower slot always wins), proving slot-driven not id/iteration-driven
- [x] 7.5 Test: priority correctness — heal vs offensive chosen by situation (critically-low ally → heal; no damaged ally + valid enemy → offensive); heal scores by ally HP-deficit ordering
- [x] 7.6 Test: direct unit tests of `buff_ally` and `summon` scoring branches (no authored ability exercises them end-to-end — documented)
- [x] 7.7 Test: `path_ability_defs.go` loader — file shape parsed; unknown ability id panics; unknown rank panics; missing cell = empty grant
- [x] 7.8 Test: promotion grants — Cleric gains ≥1 heal-line, Arch Mage gains ≥1 offensive (`closest_enemy_in_range`); multi-rank catch-up grants every crossed rank with no duplicates; re-invocation idempotent; RNG-free (two seeded runs identical `unit.Abilities`)
- [x] 7.9 Test: a granted ability appears in `AbilitySnapshot` (autocast toggle + cooldown) with no new protocol field
- [x] 7.9a Test (`DamageAmount`): an ability with `DamageAmount > 0` damages its target on resolve via `applyUnitDamageWithSourceLocked` (attributed to caster, typed by `DamageType`); `0`/absent deals none; a lethal cast runs the normal death pipeline (no parallel path)
- [x] 7.10 Test: seeded multi-ability replay determinism (same seed+inputs → identical per-ability cast-tick sets)
- [x] 7.11 Run the full server suite (`go test ./...` in `server/`); all pass. Derive expected values from catalog/defs — no pinned balance numbers (per project rule)

## 8. Frontend Verification & Sign-off

- [x] 8.1 Confirm no protocol/snapshot change: diff touches only Go + catalog JSON + new test files; no `pkg/protocol`/`messages.go`/client edits
- [x] 8.2 Verify the action bar renders post-promotion abilities, per-ability autocast toggle, and cooldown overlay from the existing `AbilitySnapshot` (read-only verification; `vue-tsc` unaffected since no client change)
- [x] 8.3 Sync the delta specs (incl. the `ability-category` MODIFIED delta) into `openspec/specs/` at archive time; confirm `openspec validate --specs` passes
- [x] 8.4 Re-read `docs/design/caster_archetype.md` Phase 2 section + proposal; confirm every Phase-2 deliverable is realised and tested and the no-regression invariant holds
