## 1. Investigation & Anchor Verification

- [x] 1.1 Confirm `combatProfiles` map literal and the `"support"` / `"archer"` entries in `combat_ai_profiles.go` (verify `support` has `RetreatDistance`/`RetreatTriggerMeleeRange`/`Backline` and `MaxChaseDistance: 110`/`AoERadius: 70`/`AoECluster: 18`; `archer` has no retreat and `MaxChaseDistance: 180`)
- [x] 1.2 Confirm `effectiveLeashDistance` clamps leash up to `AttackRange` but there is **no** equivalent clamp for `MaxChaseDistance`; confirm `resolveCombatProfile` reads `UnitDef.CombatProfile` directly, the load-time panic at `unit_defs.go` for unknown `combatProfile`, and that `UnitDef.Archetype` is **not** load-validated
- [x] 1.3 Confirm scoring edit sites in `combat_ai_scoring.go`: the `support`/`mage` branch in `unitStrategicValue` (~:313), the attacker `case "enemy_archer", "support":` in `unitTypePreference` (~:382), and the `targetProfile.Name == "support"` target checks (~:357, :361, :365). Confirm none of `unitStrategicValue`/`unitTypePreference` read `MaxChaseDistance`/`AoERadius`/`AoECluster`
- [x] 1.4 Confirm the archetype-scoped upgrade path: `upgrade_apply.go` `upgradeScopeArchetype` matching on `unit.Archetype` (`:55-56` and the buff equivalent `:101-102`); confirm `swift_strikes_common`/`swift_strikes_rare` are `scope: archetype`, `archetype: "archer"`; confirm the Apprentice currently matches them and that no `archetype: "caster"` upgrade exists
- [x] 1.5 Confirm `cleric.json`/`arch_mage.json` carry no `type`/`archetype`/`combatProfile` and promotion does not swap `unit.UnitType` (paths resolve profile through `apprentice.json`)
- [x] 1.6 Confirm the `DamageType` registry pattern in `damage_type.go` and the `damageType` load-validation panic in `ability_defs.go`; confirm `AbilityDef` struct location and current `apprentice.json` / `heal.json` contents; confirm no client code branches on the snapshot `archetype` value

## 2. Caster Combat Profile (must precede catalog flip — load-order constraint)

- [x] 2.1 Add a `"caster"` entry to the `combatProfiles` map: value equal to `"support"` **except** `Name: "caster"`, `MaxChaseDistance` set near the `archer` envelope (≈180, not support's 110), `AoERadius: 0`, and the `AoECluster` target weight `0`. Write the deltas inline in the literal entry. Do not edit `"support"` or `"archer"`.
- [x] 2.2 Build the server and confirm it compiles with the new profile registered (catalog still references `archer` at this point, so no panic)

## 3. AI Scoring Wiring (caster == support)

- [x] 3.1 In `unitStrategicValue`, extend the `profile.Name == "support" || profile.Name == "mage"` condition to also include `"caster"`
- [x] 3.2 In `unitTypePreference`, add `"caster"` to the existing `case "enemy_archer", "support":` attacker group
- [x] 3.3 In `unitTypePreference`, at every `targetProfile.Name == "support"` check (archer / mage / cavalry-skirmisher branches), add `|| targetProfile.Name == "caster"`
- [x] 3.4 Verify no scoring branch special-cases `caster` differently from `support`, and that the Decision-1 profile-number deltas are not referenced by any scoring branch

## 4. AbilityCategory Enum & AbilityDef Field

- [x] 4.1 Create `ability_category.go` mirroring `damage_type.go`: `type AbilityCategory string`; consts `AbilityCategoryHeal/BuffAlly/Summon/Offensive`; `abilityCategoryRegistry`; `RegisterAbilityCategory` (panic on empty); `IsValidAbilityCategory`; `AbilityCategories()` (sorted)
- [x] 4.2 Add `Category AbilityCategory \`json:"category,omitempty"\`` to the `AbilityDef` struct
- [x] 4.3 Add load validation in the ability loader mirroring the `damageType` panic: `if def.Category != "" && !IsValidAbilityCategory(def.Category) { panic(rel + ...) }`
- [x] 4.4 Build and run the existing server suite to confirm the inert field/enum breaks nothing

## 5. Catalog Flips

- [x] 5.1 In `catalog/units/human/apprentice/apprentice.json`, change **both** `"archetype"` and `"combatProfile"` from `"archer"` to `"caster"`; leave all other fields unchanged (this is the line that forfeits Swift Strikes eligibility — intended, Decision 5)
- [x] 5.2 In `catalog/abilities/heal/heal.json`, add the additive line `"category": "heal"`; leave all other fields unchanged
- [x] 5.3 Build and start the server to confirm the catalog loads without panic (validates 2.1 ordering and 4.3 validation)

## 6. Tests

The full server suite is the no-regression gate for everything **outside** the enumerated Intended Behavioural Deltas. The delta-specific tests below assert each delta is the intended one.

- [x] 6.1 Test: `resolveCombatProfile` returns `"caster"` for an Apprentice; the `"caster"` profile equals `"support"` on every field **except** `Name` (`"caster"`), `MaxChaseDistance` (asserted *not shrunk below the `archer` baseline* — compare to `combatProfiles["archer"].MaxChaseDistance`, do not pin a literal), `AoERadius` (asserted `== 0`), and the `AoECluster` weight (asserted `== 0`)
- [x] 6.2 Test: `"archer"` and `"support"` profile entries are unchanged by this change (field-by-field equality vs their documented values)
- [x] 6.3 Test: `unitStrategicValue` is equal for an otherwise-identical unit under `caster` vs `support` (proves the Decision-1 profile-number deltas do not leak into strategic value); `unitTypePreference` returns equal values for a `caster` vs `support` attacker, and for a `caster` vs `support` target evaluated by archer/mage/cavalry/skirmisher
- [x] 6.4 Test: a `caster`-profiled unit retreats from a closing melee attacker where an `archer`-profiled unit would not (kiting behaviour — Delta 1)
- [x] 6.5 Test (Delta 3 — Swift Strikes forfeiture): an Apprentice with `archetype: "caster"` does **not** match the `archer`-scoped `swift_strikes_*` upgrade via the `upgradeScopeArchetype` path, and matches no archetype-scoped upgrade currently in the catalog; a regression-pin comment records that this is intended, not a bug
- [x] 6.6 Test: `IsValidAbilityCategory` true for the four registered values, false for `""` and unknown; `RegisterAbilityCategory("")` panics; `AbilityCategories()` is sorted/stable
- [x] 6.7 Test: `heal` loads with `Category == AbilityCategoryHeal`; an ability with no category loads with `Category == ""`; an ability with an invalid category panics at load (loader uses a compile-time embed.FS; the validation predicate is exercised inline with a documented note — see test comment)
- [x] 6.8 Test (heal-autocast guarantee, two parts): (a) the heal-autocast gating path (mana / cooldown / selector predicate) is unchanged by the profile flip — assert the gate decisions are identical for a fixed unit state pre/post; (b) **scoped seeded-replay tripwire**: in a no-melee scenario where the Apprentice never retreats, the set of ticks heal is auto-cast on is identical pre/post change for the same seed and inputs. Document in the test that cadence divergence *under retreat* is expected and out of scope for this assertion
- [x] 6.9 Run the full server test suite (`go test ./...` in `server/`) as the no-regression gate; all pass

## 7. Verification & Sign-off

- [x] 7.1 Confirm no protocol/snapshot/frontend changes were introduced (this change's footprint = `combat_ai_profiles.go`, `combat_ai_scoring.go`, `ability_defs.go`, 2 catalog JSON, new `ability_category.go`/`caster_archetype_test.go`, and `ability_autocast_test.go` decoupling; no client/protocol/snapshot/upgrade-JSON edits. The other pre-modified `*_test.go`/`progression.go` and the untracked `client/.../paths/` assets dir + `docs/design/...` predate this session and are not part of this change)
- [x] 7.2 Confirm `vue-tsc` typecheck is unaffected (zero client/TypeScript files in this change's footprint → unaffected by construction) and the full server suite is green (`go test ./...` exit 0, independently re-run)
- [x] 7.3 Re-read `docs/design/caster_archetype.md` Phase 1 acceptance criteria and confirm each is met, and that every item in `proposal.md` *Intended Behavioural Deltas* is realised and covered by a test (caster profile + its two deltas → 6.1/6.2; Apprentice kites → 6.4; enemy focus-fire shift → 6.3; Swift Strikes forfeited → 6.5; heal-gating untouched + scoped tripwire → 6.8; suite green → 6.9. The design doc's "byte-identical heal autocast" wording is intentionally superseded by the accurate two-part guarantee, as documented in proposal/design)
