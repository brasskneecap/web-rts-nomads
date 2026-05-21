## 1. Catalog edits

- [x] 1.1 Create `server/internal/game/catalog/units/human/apprentice/paths/cleric/abilities/bronze.json` with `{ "grant": ["greater_heal"] }`.
- [x] 1.2 In `server/internal/game/catalog/units/human/apprentice/paths/cleric/perks/bronze.json`:
  - Remove the `greater_heal` entry.
  - Add the `bolstering_prayer` entry with `Config: { buffDurationSeconds: 5.0, armorBonus: 50, recastThresholdPercent: 0.30 }`.
  - Confirm the file ends with exactly four entries: `sanctuary`, `battle_prayer`, `bolstering_prayer`, `mana_conduit` (order matches the existing convention — alphabetical or whatever the file's current style is; not significant for selection).
- [x] 1.3 Add a `perk-bolstering-prayer` icon entry in `server/internal/game/catalog/action-icons.json` (placeholder asset OK pending art). NOTE: id uses hyphens to match the existing `perk-battle-prayer` convention; the bronze.json `icon` field references the same hyphenated id.

## 2. Move the heal → greater_heal swap to the path-ability path

- [x] 2.1 In `server/internal/game/perks.go`, rename `applyGreaterHealPerkSwapLocked` → `applyGreaterHealSwapLocked` (drop "Perk" from the name). Update its doc comment to remove the perk framing — it is now a generic "Cleric upgrade heal" swap that can be triggered from any grant path. Body is unchanged.
- [x] 2.2 Remove the `greater_heal` case from `applyPerkGrantedHooksLocked` (in `perks.go`). Update the function's doc comment block: remove the "greater_heal swap" example and note that this seam currently has no consumers but is retained for future ability-replacing perks.
- [x] 2.3 In `server/internal/game/path_ability_defs.go`, extend `assignUnitPathAbilitiesLocked` so that immediately before appending an ability id it checks if the id is `"greater_heal"`; if so, call `s.applyGreaterHealSwapLocked(unit)` first. The existing `containsAbility(unit, abilityID)` guard then short-circuits the append because the swap put `"greater_heal"` into `Abilities`. When `"heal"` was absent, the swap is a no-op and the normal append runs. Add a short doc-comment note that this branch is the active consumer of the swap helper.
- [x] 2.4 In `server/internal/game/debug_spawn.go`, `DebugSpawnUnit` currently sets `unit.ProgressionPath` and `unit.Rank` directly without calling `assignUnitPathAbilitiesLocked`. Add a call to `s.assignUnitPathAbilitiesLocked(unit)` immediately after the path/rank assignment block (around the existing line 102 area) and before the perk-grant loop, so a debug-spawned Bronze Cleric receives `greater_heal` (with the heal-swap) the same way a promotion-grown Cleric does. Update the surrounding doc comment to mention the path-ability grant alongside the existing perk-hook explanation. Audit any other manual-promotion call sites for the same gap. (Audit: progression.go already calls `assignUnitPathAbilitiesLocked` in `addUnitXPLocked`; only debug_spawn needed the gap closed.)
- [x] 2.5 Update tests that currently invoke `s.applyPerkGrantedHooksLocked(cleric, "greater_heal")` to drive the swap — they no longer compile cleanly under the change (perk-def lookup returns nil, the hook short-circuits). Replaced all call sites: `greater_heal_swap_test.go` (table-driven; (a) `assignUnitPathAbilitiesLocked` + (b) direct swap), `cleric_bronze_perks_test.go` (deleted the now-duplicate `TestGreaterHeal_PerkSwapsAbility`; remaining sites use new `promoteToBronzeCleric(s, cleric)` helper), `ability_multi_target_test.go` (5 sites use the helper), `cleric_determinism_test.go` (1 site uses the helper). Pattern (a) chosen for grant-pipeline tests, (b) chosen for swap-only tests, called out in comments.

## 3. Bolstering Prayer — perk state, hook, decay

- [x] 3.1 In `server/internal/game/perks.go`, add `BolsteringPrayerRemaining float64` and `BolsteringPrayerArmor float64` to `UnitPerkState`. Place them in the existing `// ── battle_prayer (cleric bronze)` block — extend it to cover both perks — and add a parallel doc-comment block describing the cross-unit pattern, refresh-max semantics, and decay location.
- [x] 3.2 Extend `onPerkAbilityResolvedLocked` with a `case "bolstering_prayer":` branch parallel to the existing `battle_prayer` case. Read `Config["buffDurationSeconds"]` and `Config["armorBonus"]`, gate on `def.Category == AbilityCategoryHeal` (already the function-level gate — confirm the gate stays outside the switch), apply refresh-longer / refresh-stronger to `BolsteringPrayerRemaining` and `BolsteringPrayerArmor`. No change to call sites of the hook.
- [x] 3.3 In `server/internal/game/state.go`, in the per-unit `Update()` loop, add a decay block immediately after the existing `BattlePrayerRemaining` decay: subtract `dt`, clamp to 0, reset `BolsteringPrayerArmor` to 0 when `BolsteringPrayerRemaining` hits 0. Mirror the comment style.

## 4. Armor aggregation

- [x] 4.1 In `server/internal/game/perks_defense.go`, add `perkBonusArmorFromBuffsLocked(unit) int` that returns `int(math.Round(unit.PerkState.BolsteringPrayerArmor))` when `BolsteringPrayerRemaining > 0`, else 0. Doc-comment block in the file's existing style.
- [x] 4.2 Update `effectiveArmorLocked` to add the new helper's return into the `flatBonus` aggregation. Insert it after `perkBonusArmorFromAurasLocked` so the order is base → perk → banner → aura → buff. The percent-multiplier section above is untouched (Bolstering Prayer is a flat bonus, not a percent).

## 5. Autocast — generalised recast-threshold

- [x] 5.1 In `server/internal/game/autocast_selectors.go`, define a small registry of heal-buff perks (`battle_prayer`, `bolstering_prayer`) describing for each: perk id, the function that reads the buff's `Remaining` field from a `*UnitPerkState`, and the config key that holds the buff's full duration. Keep it package-private and table-style for easy extension.
- [x] 5.2 Replace the existing `containsString(caster.PerkIDs, "battle_prayer")` checks in both the full-HP-focus selector and the force-include focus path with a helper `casterOwnsAnyHealBuffPerk(caster)` that loops the registry.
- [x] 5.3 Replace the existing single-perk threshold evaluation (`focus.PerkState.BattlePrayerRemaining < thresholdPct * duration`) with `focusHasStaleHealBuff(caster, focus)` that returns true if ANY heal-buff perk the caster owns has its corresponding `Remaining` below `recastThresholdPercent * buffDurationSeconds`. Each perk reads its own config, so future heal-buff perks can use different durations cleanly.
- [x] 5.4 Confirm the existing tests `TestBattlePrayer_RecastThresholdTriggersFullHPCast`, `TestBattlePrayer_FreshBuffNoRecast`, and `TestBattlePrayer_NoRecastWithoutPerk` (in `cleric_bronze_perks_test.go`) still pass unmodified — they exercise the generalised path with only the `battle_prayer` registry entry exercised.

## 6. HUD icon

- [x] 6.1 In `server/internal/game/perks_icons.go`, mirror the existing `battle_prayer` cross-unit buff-icon block: emit `bolstering_prayer` when `unit.PerkState.BolsteringPrayerRemaining > 0`. The icon id matches the `perk-bolstering_prayer` action-icon catalog entry from task 1.3.

## 7. Tests

- [x] 7.1 In `server/internal/game/greater_heal_swap_test.go` (existing): adapt the test that grants the `greater_heal` perk and asserts the swap → grant the unit Bronze rank on the Cleric path instead, and assert the same end state (`Abilities == ["greater_heal"]`, autocast/cooldown migrated). Drop the perk-based variant; the path-ability grant is the new canonical entry point.
- [x] 7.2 Add a new test: a Cleric promoted to Bronze who rolls each of the four pool perks (`sanctuary`, `battle_prayer`, `bolstering_prayer`, `mana_conduit`) ends up with `Abilities` containing `"greater_heal"` and not `"heal"`. Confirms the per-path grant fires independently of the perk roll.
- [x] 7.3 In `server/internal/game/cleric_bronze_perks_test.go`, add `TestBolsteringPrayer_*` tests modeled exactly on the existing `TestBattlePrayer_*` family:
  - `AppliesBuffOnHeal` — single-target heal stamps the armor buff.
  - `BuffAppliedToAllGreaterHealTargets` — multi-target heal stamps on every target.
  - `RefreshNotStack` — re-cast refresh-longer/refresh-stronger, never additive.
  - `DecaysInUpdateLoop` — per-tick decay, armor field zeroed on expiry.
  - `GrantsArmorBonus` — direct `effectiveArmorLocked` assertion: base + Bolstering equals expected total; stacks additively with another flat-armor source (e.g. `interlock`) but is not multiplied by percent-armor.
  - `AppliesToNonClericAlly` — buff stamps and grants armor on a Soldier with no perks.
  - `RecastThresholdTriggersFullHPCast` — stale buff on full-HP focus triggers recast.
  - `FreshBuffNoRecast` — fresh buff does not trigger recast.
- [x] 7.4 Add `TestBolstering_And_BattlePrayer_StackIndependently`: heal a single ally with two Clerics, one with each perk, and assert both `BattlePrayerRemaining`+`BattlePrayerMultiplier` AND `BolsteringPrayerRemaining`+`BolsteringPrayerArmor` are populated. Advance the loop and confirm both decay independently to 0.
- [x] 7.5 Determinism test: extend `cleric_determinism_test.go` (or add a sibling) so two identical seeded runs of a Cleric with `bolstering_prayer` healing the same target produce byte-identical `BolsteringPrayerRemaining` snapshots at every tick.
- [x] 7.6 No-hardcoded-tunables guard: every new test reads its expected armor / duration / recast-threshold values from `perkDefByID("bolstering_prayer").Config[...]`. No literal `50` or `5.0` in test code beyond the catalog read. (Matches the existing `feedback-no-hardcoded-tunables-in-tests` rule.)
- [x] 7.7 Backfill the path-ability grant test suite (`server/internal/game/caster_defer_test.go` or the equivalent currently exercising the synthetic-fixture path): add a real-catalog test asserting `(cleric, bronze)` resolves to `["greater_heal"]` from the loader (not a synthetic injection). Keep the synthetic test as the engine-only regression guard for future grants.

## 8. Spec sync

- [x] 8.1 Confirm `openspec list` shows the change as `ADDED`/`MODIFIED`/`REMOVED` deltas matching the two spec files under `specs/`.
- [x] 8.2 After implementation + tests pass, run `openspec validate` against the change directory and resolve any reported issues.
- [ ] 8.3 Sync specs (`openspec sync` / archive workflow) so `openspec/specs/cleric-bronze-perks/spec.md` and `openspec/specs/per-path-ability-kits/spec.md` reflect the deltas.
