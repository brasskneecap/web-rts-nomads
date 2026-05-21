# cleric-bronze-perks Specification

## Purpose

Defines the four authored Cleric Bronze perks (`greater_heal`, `sanctuary`, `battle_prayer`, `mana_conduit`) — their `PerkDef.Config` tuning surface, the runtime hooks each plugs into (post-cast resolve hook, aura mitigation, per-tick scan), the ability-swap mechanic that turns Heal into Greater Heal when the perk is granted, and Battle Prayer's cross-unit buff state with recast-threshold semantics that interact with `cleric-focus-target`. The post-cast hook `onPerkAbilityResolvedLocked` introduced here is the seam for future ally-targeted perk-conditioned post-effects.

## Requirements

### Requirement: Cleric Bronze perk pool contains exactly four perks

`catalog/units/human/apprentice/paths/cleric/perks/bronze.json` SHALL list exactly four perk definitions with IDs: `greater_heal`, `sanctuary`, `battle_prayer`, `mana_conduit`. Each perk SHALL have a non-empty `Config map[string]float64` defining every tunable value (radii, percentages, durations, multipliers, caps). No tuning value SHALL be hardcoded in the Go runtime; all values SHALL be read from `def.Config[...]` at use sites, matching the existing perk-runtime convention (`rallying_banner`, `last_stand`, etc.).

#### Scenario: Cleric Bronze pool loads exactly four perks

- **WHEN** the perk catalog is loaded
- **THEN** the Cleric Bronze pool contains exactly four entries with IDs `greater_heal`, `sanctuary`, `battle_prayer`, `mana_conduit`

#### Scenario: Each perk has Config entries for all required keys

- **WHEN** the perk catalog is loaded
- **THEN** every Cleric Bronze perk's `Config` map has non-zero values for the keys named in this spec's per-perk requirements

#### Scenario: Cleric reaching Bronze receives one of the four perks

- **WHEN** a Cleric is awarded a Bronze rank perk via `assignUnitPerkLocked`
- **THEN** exactly one of `greater_heal`, `sanctuary`, `battle_prayer`, `mana_conduit` is appended to `unit.PerkIDs`

### Requirement: `greater_heal` perk swaps `"heal"` for `"greater_heal"` in `Unit.Abilities`

When `greater_heal` is appended to `unit.PerkIDs` by `assignUnitPerkLocked` (or any other perk grant path), the grant routine SHALL locate `"heal"` in `unit.Abilities`. If found, the slot SHALL be overwritten with `"greater_heal"` in place (slot index preserved so action-bar ordering is stable). The unit's autocast and cooldown state for `"heal"` SHALL be migrated to `"greater_heal"` (`AutoCastEnabled["greater_heal"] = AutoCastEnabled["heal"]`, then the `"heal"` key SHALL be deleted; same for `AbilityCooldowns`). If `"heal"` is not present (defensive — a Cleric should always own Heal), the swap SHALL be a no-op.

#### Scenario: Swap replaces heal in the abilities slot

- **WHEN** a Cleric with `Abilities: ["heal"]` is granted `greater_heal`
- **THEN** `Abilities == ["greater_heal"]` after the grant

#### Scenario: Slot index preserved on swap

- **WHEN** a hypothetical caster has `Abilities: ["arcane_bolt", "heal"]` and is granted `greater_heal`
- **THEN** `Abilities == ["arcane_bolt", "greater_heal"]` (slot 1 swapped in place)

#### Scenario: Autocast state migrates with the swap

- **WHEN** a Cleric had `AutoCastEnabled["heal"] = true` and `AbilityCooldowns["heal"] = 1.5` at grant time
- **THEN** after the grant `AutoCastEnabled["greater_heal"] = true`, `AbilityCooldowns["greater_heal"] = 1.5`, and the `"heal"` keys are absent from both maps

#### Scenario: Swap on a unit without heal is a safe no-op

- **WHEN** the grant runs on a unit whose `Abilities` does not contain `"heal"`
- **THEN** `Abilities` is unchanged and no error is raised

### Requirement: Greater Heal ability JSON declares `targetCount: 3` and category `heal`

`catalog/abilities/greater_heal/greater_heal.json` SHALL declare:

- `"category": "heal"` (matching base Heal's category)
- `"targetCount": 3`
- A `manaCost` strictly greater than base Heal's `manaCost` (proposed default: `10`)
- A `healAmount` matching the base Heal's per-target heal (proposed default: `10`)
- `"canTargetAllies": true`, `"canTargetSelf": true`, `"canTargetEnemies": false`
- An `autoCastTargetSelector` that produces a multi-target list (extending or aliasing the existing `lowest_hp_percentage_ally_in_range` selector)

#### Scenario: Greater Heal loads with TargetCount 3

- **WHEN** `greater_heal.json` is loaded
- **THEN** the resulting `AbilityDef` has `TargetCount == 3`, `Category == AbilityCategoryHeal`, and the cost/heal values configured in the file

### Requirement: `sanctuary` perk reduces ranged damage to nearby allies via an aura

The `sanctuary` perk SHALL be a passive aura registered in `perks_auras.go`. Its `Config` SHALL include `"radiusPixels"` and `"damageReductionPercent"` (in `[0.0, 1.0)` — a 0.25 value means 25% damage reduction). The damage pipeline SHALL, when applying damage to a unit and `src.Kind == "projectile"`, call `perkRangedDamageMultiplierFromAurasLocked(target, src)` to obtain a multiplier `m ∈ (0, 1]`. The applied damage SHALL be `originalDamage * m` (mitigation applied multiplicatively into the existing pipeline). For non-projectile damage sources (`Kind != "projectile"`) the multiplier SHALL be `1.0` (no effect).

#### Scenario: Sanctuary reduces projectile damage to allies in range

- **WHEN** an allied target stands within `sanctuary.radiusPixels` of any same-team unit owning `sanctuary`, and a projectile attack deals 100 damage with `src.Kind == "projectile"`
- **THEN** the applied damage is `100 * (1 - damageReductionPercent)`

#### Scenario: Sanctuary does not reduce melee damage

- **WHEN** an allied target inside a Sanctuary aura takes melee damage (`src.Kind == "melee"`)
- **THEN** the applied damage is unchanged

#### Scenario: Sanctuary does not reduce trap damage

- **WHEN** an allied target inside a Sanctuary aura is hit by a trap (`src.Kind == "trap"` or a more-specific trap kind)
- **THEN** the applied damage is unchanged

#### Scenario: Overlapping Sanctuary auras take the strongest reduction, do not stack

- **WHEN** an allied target is inside two Sanctuary auras whose `damageReductionPercent` are `0.20` and `0.30`
- **THEN** the applied damage is `originalDamage * (1 - 0.30) == originalDamage * 0.70` (max wins, not multiplied together)

#### Scenario: Target outside aura is unaffected

- **WHEN** an allied target stands outside every Sanctuary's `radiusPixels`
- **THEN** projectile damage is not reduced by Sanctuary

### Requirement: `battle_prayer` perk applies an attack-speed buff to every Heal target

When a Cleric owning `battle_prayer` resolves a `heal` or `greater_heal` cast, the post-cast hook `onPerkAbilityResolvedLocked` SHALL — for `battle_prayer` — apply a time-limited attack-speed buff to every target the cast landed on. The buff is stored on the *target's* `UnitPerkState` as:

- `BattlePrayerRemaining float64` — seconds remaining; set to `Config["buffDurationSeconds"]` on application
- `BattlePrayerMultiplier float64` — attack-speed bonus fraction (e.g., `0.25` = +25% attack speed); set to `Config["attackSpeedMultiplier"]` on application

Re-application onto a target with an existing Battle Prayer buff SHALL refresh (not stack) the buff: `BattlePrayerRemaining` SHALL be set to `max(current, Config["buffDurationSeconds"])` and `BattlePrayerMultiplier` SHALL be set to `max(current, Config["attackSpeedMultiplier"])` (refresh-longer + refresh-stronger semantics, matching existing mark-stack refresh rules).

`perkAttackSpeedBonusLocked(unit)` SHALL add `unit.PerkState.BattlePrayerMultiplier` to the attack-speed bonus accumulator whenever `unit.PerkState.BattlePrayerRemaining > 0`. The bonus SHALL be applied to the *target* of the buff (the unit being modified), regardless of whether the target owns the `battle_prayer` perk themselves.

#### Scenario: Single-target Heal applies one buff

- **WHEN** a Cleric with `battle_prayer` casts base Heal (`TargetCount: 1`) on ally A
- **THEN** A's `BattlePrayerRemaining` becomes `buffDurationSeconds` and `BattlePrayerMultiplier` becomes `attackSpeedMultiplier`

#### Scenario: Greater Heal applies the buff to all targets

- **WHEN** a Cleric with both `battle_prayer` and `greater_heal` casts Heal and hits three allies
- **THEN** all three allies have `BattlePrayerRemaining == buffDurationSeconds` and `BattlePrayerMultiplier == attackSpeedMultiplier`

#### Scenario: Buff refreshes duration on recast (does not stack)

- **WHEN** a target with `BattlePrayerRemaining == 1.0` receives a fresh Battle Prayer'd Heal at `t`, configured `buffDurationSeconds == 5.0`
- **THEN** `BattlePrayerRemaining` becomes `5.0` (refresh-longer), not `6.0`; `BattlePrayerMultiplier` is at-most the configured multiplier

#### Scenario: Buff grants attack-speed bonus while active

- **WHEN** a unit has `BattlePrayerRemaining > 0` and `BattlePrayerMultiplier == 0.25`
- **THEN** `perkAttackSpeedBonusLocked(unit)` includes `+0.25` in the bonus accumulator

#### Scenario: Buff applies to allies who do not own `battle_prayer`

- **WHEN** a non-Cleric ally without `battle_prayer` is healed by a Cleric who does own it
- **THEN** the ally receives the buff and gains the attack-speed bonus

### Requirement: Battle Prayer cross-unit decay runs in `state.go Update()`

`BattlePrayerRemaining` SHALL be decremented by `dt` every tick in the per-unit loop in `state.go Update()`, alongside `WeakenedRemaining` and `TauntRemaining`. When `BattlePrayerRemaining` reaches `0`, `BattlePrayerMultiplier` SHALL also be reset to `0.0` so a stale multiplier never grants a bonus. Decay SHALL NOT happen inside `tickUnitPerkStateLocked` because the buff lives on targets that may not own the `battle_prayer` perk.

#### Scenario: Buff decays each tick

- **WHEN** a unit has `BattlePrayerRemaining == 5.0` at tick T
- **THEN** at tick T+1 it is `5.0 - dt`

#### Scenario: Buff expiry resets multiplier

- **WHEN** `BattlePrayerRemaining` decays from a positive value to `<= 0`
- **THEN** both `BattlePrayerRemaining` and `BattlePrayerMultiplier` are `0.0` after the decay step

#### Scenario: Non-cleric ally's buff decays the same way

- **WHEN** a Soldier with no perks has an active Battle Prayer buff applied to it
- **THEN** the buff decays the same way as on any other unit

### Requirement: Battle Prayer enables full-HP focus recast via threshold

When a Cleric owns `battle_prayer` and has an active focus target at full HP, the auto-cast Heal selector SHALL evaluate whether to refresh the buff using the rule: cast IF `focus.PerkState.BattlePrayerRemaining < Config["recastThresholdPercent"] * Config["buffDurationSeconds"]` (default threshold ~0.30, i.e., recast when under 30% of buff duration remains). When the condition is true and the Cleric has the mana and cooldown to cast, the cast SHALL proceed with the focus target as the primary heal target (or force-included for multi-target). The condition SHALL be false when the caster does not own `battle_prayer`, preserving the "don't cast Heal on full-HP allies" default.

#### Scenario: Stale buff on full-HP focus triggers recast

- **WHEN** a Cleric with `battle_prayer` has focus on a full-HP ally whose `BattlePrayerRemaining` is `0.0` and `buffDurationSeconds == 5.0`, `recastThresholdPercent == 0.30`
- **THEN** Heal is cast on the focus target this tick (mana/cooldown permitting)

#### Scenario: Fresh buff on full-HP focus does not trigger recast

- **WHEN** the focus's `BattlePrayerRemaining` is `4.0`, `buffDurationSeconds == 5.0`, threshold `0.30` (1.5 seconds)
- **THEN** the recast condition is false and Heal is not cast this tick

#### Scenario: Threshold logic does not fire for Cleric without battle_prayer

- **WHEN** a Cleric without `battle_prayer` has a focus on a full-HP ally
- **THEN** the recast-threshold path is never evaluated and the standard "don't cast on full-HP" behavior is preserved

### Requirement: `mana_conduit` perk grants per-tick bonus mana regen scaling with injured allies in radius

`mana_conduit` SHALL be a self-effect on the Cleric, evaluated in `tickUnitPerkStateLocked`. Its `Config` SHALL include `"radiusPixels"`, `"bonusManaRegenPerAlly"` (mana per second per injured ally), and `"maxAlliesCounted"` (integer cap). Each tick the case SHALL:

1. Iterate `s.Units` (deterministic order).
2. Count units `u` where `u != cleric`, `u.OwnerID` is on the same team, `u.HP > 0`, `u.Visible == true`, `u.HP < u.MaxHP`, and the squared distance to the Cleric is `<= radiusPixels^2`.
3. Cap the count at `maxAlliesCounted`.
4. Add `bonusManaRegenPerAlly * count * dt` to `unit.CurrentMana`, clamped at `unit.MaxMana`.

When no injured allies are in range (count = 0) the bonus is zero (mana_conduit does not subtract from existing regen).

#### Scenario: Cleric near no injured allies gains zero bonus mana

- **WHEN** a Cleric with `mana_conduit` is alone or only near full-HP allies
- **THEN** `mana_conduit` adds `0` to the Cleric's mana this tick

#### Scenario: Cleric near three injured allies gains 3× bonus

- **WHEN** three injured same-team allies are inside `radiusPixels` and `maxAlliesCounted >= 3`
- **THEN** the Cleric gains `3 * bonusManaRegenPerAlly * dt` extra mana this tick (clamped at `MaxMana`)

#### Scenario: Count is capped by maxAlliesCounted

- **WHEN** five injured allies are in range and `maxAlliesCounted == 3`
- **THEN** the bonus is `3 * bonusManaRegenPerAlly * dt`, not `5 *`

#### Scenario: Full-HP allies do not count as injured

- **WHEN** five full-HP allies are in range and zero injured
- **THEN** the bonus is `0`

#### Scenario: Enemies do not count

- **WHEN** an injured enemy is in range and no allies are injured
- **THEN** the bonus is `0`

#### Scenario: Bonus mana is clamped at MaxMana

- **WHEN** the Cleric is already at `CurrentMana == MaxMana` and the bonus would push it over
- **THEN** `CurrentMana` ends the tick equal to `MaxMana`, not above

### Requirement: New post-cast hook `onPerkAbilityResolvedLocked` is invoked once per target

`resolveAbilityCastLocked` SHALL call `onPerkAbilityResolvedLocked(caster, def, target)` after each target's per-target effect lands. The function SHALL iterate `caster.PerkIDs` and switch on perk id, calling the appropriate handler. Battle Prayer is the only initial consumer (it applies the buff to `target`). The hook SHALL be the seam for any future ally-targeted perk-conditioned post-cast effect.

#### Scenario: Hook is called once per resolved target

- **WHEN** a `TargetCount == 3` cast resolves on three targets
- **THEN** `onPerkAbilityResolvedLocked` is invoked three times, once per target

#### Scenario: Hook is not called when caster has no perks

- **WHEN** the caster owns no perks
- **THEN** the hook is invoked but does nothing (defensive; iterates an empty `PerkIDs`)

### Requirement: All Cleric Bronze perks emit appropriate HUD icons

`activeBuffIconsLocked` / `activeDebuffIconsLocked` (in `perks_icons.go`) SHALL emit:

- A passive perk-owned icon for `sanctuary` and `mana_conduit` on the Cleric (so the player sees the perk is active).
- A timed buff icon for `battle_prayer` on every unit with `BattlePrayerRemaining > 0`, with remaining duration sourced from `BattlePrayerRemaining`.
- An ability-bar icon for `greater_heal` replaces the `heal` icon (already implied by the ability swap; no separate per-perk icon needed for the perk itself unless a polish pass adds one).

#### Scenario: Cleric with sanctuary shows a passive icon

- **WHEN** a Cleric owns `sanctuary`
- **THEN** the HUD displays a Sanctuary passive icon on the Cleric

#### Scenario: Healed ally shows Battle Prayer buff icon with remaining duration

- **WHEN** an ally has `BattlePrayerRemaining > 0`
- **THEN** the HUD displays a Battle Prayer buff icon on that ally with the remaining-duration display reflecting `BattlePrayerRemaining`

#### Scenario: Buff icon disappears when buff expires

- **WHEN** `BattlePrayerRemaining` decays to `0`
- **THEN** the buff icon is no longer emitted in `activeBuffIconsLocked` for that unit

### Requirement: Catalog includes action-icon entries for new perks and `focus_target`

`catalog/action-icons.json` SHALL register icon entries for `perk-greater_heal`, `perk-sanctuary`, `perk-battle_prayer`, `perk-mana_conduit`, and `focus_target` (the action button). Without these entries the HUD falls back to its default placeholder, which is acceptable for development but SHALL be remedied before this change ships.

#### Scenario: All five icon ids are registered

- **WHEN** the action-icon catalog is loaded
- **THEN** the registry contains entries for `perk-greater_heal`, `perk-sanctuary`, `perk-battle_prayer`, `perk-mana_conduit`, and `focus_target`

### Requirement: Cleric Bronze perks preserve simulation determinism

All four Cleric Bronze perks SHALL be deterministic under a fixed seed and identical inputs:

- `sanctuary` aura iteration SHALL use the deterministic `s.Units` slice; tie-breaking when multiple auras cover one target SHALL be by ascending aura-owner `unit.ID`.
- `mana_conduit` neighbor counting SHALL use the deterministic `s.Units` slice; capping SHALL happen after iteration (not via map iteration).
- `battle_prayer` buff application order SHALL follow the multi-target selector's deterministic order.
- `greater_heal` target selection SHALL be deterministic per `ability-multi-target` rules (HP%-asc then unit.ID-asc).
- No `math/rand` use outside the seeded perk-pool RNG (`s.rngPerks`), and the perk-pool RNG SHALL only be used at grant time.

#### Scenario: Two replays with same seed produce identical perk outcomes

- **WHEN** the same seeded match is run twice with identical inputs and a Cleric who acquires each of the four perks
- **THEN** every perk's tick-level effect (Battle Prayer applications, Mana Conduit bonus per tick, Sanctuary mitigations applied, Greater Heal target sets) is identical between the two runs

#### Scenario: Buff/aura iteration order is independent of map iteration

- **WHEN** a unit owns multiple buffs/auras whose iteration could otherwise reorder by map traversal
- **THEN** every per-tick effect resolves in `unit.PerkIDs` slot order or `s.Units` slot order — never by map iteration
