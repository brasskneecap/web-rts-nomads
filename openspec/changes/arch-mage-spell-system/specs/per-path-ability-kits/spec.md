## MODIFIED Requirements

### Requirement: Promotion grants path abilities deterministically and idempotently

`assignUnitPathAbilitiesLocked(unit)` SHALL be the canonical recompute of `unit.Abilities` from its `(UnitType, ProgressionPath, Rank)` plus any recorded per-rank pool picks. It is called from `addUnitXPLocked` immediately after `assignUnitPerkLocked` (once per crossed rank) and from `DebugSpawnUnit` after path/rank assignment. The function SHALL produce identical results on repeated invocation (idempotent) and SHALL introduce no RNG — the only progression RNG remains the path *choice* in `assignUnitPathOnRankUpLocked` and the one-time spell-pool *roll* at rank-up (see the arch-mage-spell-pools capability), both of which record their result on the unit before this recompute reads it.

Composition (in order):

1. Start with the unit def's `Abilities` (e.g., acolyte → `["heal"]`).
2. If `pathAbilitiesByPath[unit.ProgressionPath]` is set (declared via the path JSON's `"abilities"` field), REPLACE the list with the path-level override. This covers the "cleric is a unit with greater_heal" baseline.
3. For each rank `R` the unit has reached (bronze → silver → gold up to its current rank), append any `(path, R)` rank-grants from `pathAbilityGrantsByKey` ADDITIVELY, in catalog list order, skipping any id already present. Rank-grants compose on top of the path-level override — they remain the right tool for "silver cleric also gains X" composable content.
4. For each rank `R` the unit has reached, append the unit's RECORDED pool pick for `R` (if any) ADDITIVELY, skipping any id already present. The pick is the spell id recorded by the one-time rank-up roll (arch-mage-spell-pools capability); this step performs NO RNG — it only reads the recorded value. This composes on top of steps 2–3.
5. Migrate `AutoCastEnabled` / `AbilityCooldowns` entries by position: when the new list at index `i` differs from the existing `unit.Abilities[i]`, migrate the old entry's value to the new key and delete the old key. Indices that don't change are skipped; indices beyond the old length are fresh slots with default state.

Granted/overridden ability entries SHALL initialise their autocast/cooldown maps lazily exactly as base abilities do — no spawn-path change is required.

#### Scenario: Path-level override fires on promotion

- **WHEN** an acolyte with `Abilities: ["heal"]` is promoted to the cleric path (any rank)
- **THEN** after `assignUnitPathAbilitiesLocked`, `unit.Abilities == ["greater_heal"]`

#### Scenario: Multi-rank catch-up composes overrides and grants without duplicates

- **WHEN** a single XP gain advances a unit on the cleric path from base to gold, with synthetic rank-grants for (cleric, silver) = `["synth_silver"]` and (cleric, gold) = `["synth_gold"]`
- **THEN** the final `unit.Abilities` is `["greater_heal", "synth_silver", "synth_gold"]` (override first, grants appended in rank order), with no duplicates

#### Scenario: Recorded pool pick is appended by the recompute

- **WHEN** an Arch Mage unit has a recorded bronze pool pick of `fireball` and `assignUnitPathAbilitiesLocked` runs
- **THEN** `fireball` appears in `unit.Abilities` (appended after any path override and rank grants), and repeated recompute produces the same list with no duplicate and no RNG draw

#### Scenario: Re-invocation is idempotent

- **WHEN** `assignUnitPathAbilitiesLocked` runs a second time on a unit whose abilities are already in the resolved state
- **THEN** `unit.Abilities` is unchanged (recompute produces the same list, no duplicates appended)

#### Scenario: Recompute is RNG-free and deterministic

- **WHEN** two seeded runs promote the same unit along the same path with the same inputs
- **THEN** the resulting `unit.Abilities` order and contents are identical between runs

#### Scenario: AutoCast and Cooldown state migrate across same-index swaps

- **WHEN** an acolyte with `AutoCastEnabled["heal"] = true` and `AbilityCooldowns["heal"] = 1.5` is promoted to (cleric, bronze)
- **THEN** after the recompute `AutoCastEnabled["greater_heal"] = true`, `AbilityCooldowns["greater_heal"] = 1.5`, and the `"heal"` keys are absent from both maps
