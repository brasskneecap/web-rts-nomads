## MODIFIED Requirements

### Requirement: Cleric Bronze perk pool contains exactly four perks

`catalog/units/human/apprentice/paths/cleric/perks/bronze.json` SHALL list exactly four perk definitions with IDs: `sanctuary`, `battle_prayer`, `bolstering_prayer`, `mana_conduit`. Each perk SHALL have a non-empty `Config map[string]float64` defining every tunable value (radii, percentages, durations, multipliers, caps, armor amounts). No tuning value SHALL be hardcoded in the Go runtime; all values SHALL be read from `def.Config[...]` at use sites, matching the existing perk-runtime convention (`rallying_banner`, `last_stand`, etc.).

#### Scenario: Cleric Bronze pool loads exactly four perks

- **WHEN** the perk catalog is loaded
- **THEN** the Cleric Bronze pool contains exactly four entries with IDs `sanctuary`, `battle_prayer`, `bolstering_prayer`, `mana_conduit`
- **AND** `greater_heal` is NOT present in the Cleric Bronze pool

#### Scenario: Each perk has Config entries for all required keys

- **WHEN** the perk catalog is loaded
- **THEN** every Cleric Bronze perk's `Config` map has non-zero values for the keys named in this spec's per-perk requirements

#### Scenario: Cleric reaching Bronze receives one of the four perks

- **WHEN** a Cleric is awarded a Bronze rank perk via `assignUnitPerkLocked`
- **THEN** exactly one of `sanctuary`, `battle_prayer`, `bolstering_prayer`, `mana_conduit` is appended to `unit.PerkIDs`
- **AND** `greater_heal` is never appended to `unit.PerkIDs` by this path (it is no longer a perk)

### Requirement: Battle Prayer enables full-HP focus recast via threshold

When a Cleric owns at least one heal-buff perk (`battle_prayer` or `bolstering_prayer`) and has an active focus target at full HP, the auto-cast Heal selector SHALL evaluate whether to refresh the buff(s) using the rule: cast IF, for any heal-buff perk the caster owns, `focus.PerkState.<perk>Remaining < perkDef.Config["recastThresholdPercent"] * perkDef.Config["buffDurationSeconds"]`. When the condition is true and the Cleric has the mana and cooldown to cast, the cast SHALL proceed with the focus target as the primary heal target (or force-included for multi-target). The condition SHALL be false when the caster owns neither heal-buff perk, preserving the "don't cast Heal on full-HP allies" default.

#### Scenario: Stale battle_prayer on full-HP focus triggers recast

- **WHEN** a Cleric with `battle_prayer` (and not `bolstering_prayer`) has focus on a full-HP ally whose `BattlePrayerRemaining` is `0.0` and `buffDurationSeconds == 5.0`, `recastThresholdPercent == 0.30`
- **THEN** Heal is cast on the focus target this tick (mana/cooldown permitting)

#### Scenario: Stale bolstering_prayer on full-HP focus triggers recast

- **WHEN** a Cleric with `bolstering_prayer` (and not `battle_prayer`) has focus on a full-HP ally whose `BolsteringPrayerRemaining` is `0.0` and `buffDurationSeconds == 5.0`, `recastThresholdPercent == 0.30`
- **THEN** Heal is cast on the focus target this tick (mana/cooldown permitting)

#### Scenario: Either-stale qualifies when caster owns both

- **WHEN** a Cleric (hypothetically — via debug spawn) owns both `battle_prayer` and `bolstering_prayer`, focus is full HP, `BattlePrayerRemaining == 4.0` (fresh), `BolsteringPrayerRemaining == 0.0` (stale)
- **THEN** Heal is cast on the focus this tick (the bolstering buff is below threshold)

#### Scenario: Fresh-both does not trigger recast

- **WHEN** the same dual-perk Cleric has focus on a full-HP ally with both buffs above the recast threshold
- **THEN** no recast is issued this tick

#### Scenario: Threshold logic does not fire for Cleric without any heal-buff perk

- **WHEN** a Cleric without either `battle_prayer` or `bolstering_prayer` has a focus on a full-HP ally
- **THEN** the recast-threshold path is never evaluated and the standard "don't cast on full-HP" behavior is preserved

## ADDED Requirements

### Requirement: `bolstering_prayer` perk applies a flat-armor buff to every Heal target

When a Cleric owning `bolstering_prayer` resolves a `heal` or `greater_heal` cast, the post-cast hook `onPerkAbilityResolvedLocked` SHALL — for `bolstering_prayer` — apply a time-limited flat-armor buff to every target the cast landed on. The buff is stored on the *target's* `UnitPerkState` as:

- `BolsteringPrayerRemaining float64` — seconds remaining; set to `Config["buffDurationSeconds"]` on application
- `BolsteringPrayerArmor float64` — flat armor bonus while the buff is active; set to `Config["armorBonus"]` on application

Re-application onto a target with an existing Bolstering Prayer buff SHALL refresh (not stack) the buff: `BolsteringPrayerRemaining` SHALL be set to `max(current, Config["buffDurationSeconds"])` and `BolsteringPrayerArmor` SHALL be set to `max(current, Config["armorBonus"])` (refresh-longer + refresh-stronger semantics, matching `battle_prayer` and the existing mark-stack refresh rules).

`effectiveArmorLocked(unit)` SHALL include `unit.PerkState.BolsteringPrayerArmor` (rounded to int) in the flat-armor accumulator whenever `unit.PerkState.BolsteringPrayerRemaining > 0`. The bonus SHALL be applied to the *target* of the buff (the unit being modified), regardless of whether the target owns the `bolstering_prayer` perk themselves.

The `bolstering_prayer` and `battle_prayer` buffs are independent: a unit healed by two Clerics — one of each flavor — gains both `BolsteringPrayerArmor` and `BattlePrayerMultiplier` simultaneously, each subject to its own refresh-longer / refresh-stronger semantics.

#### Scenario: Single-target Heal applies one armor buff

- **WHEN** a Cleric with `bolstering_prayer` casts base Heal (`TargetCount: 1`) on ally A
- **THEN** A's `BolsteringPrayerRemaining` becomes `buffDurationSeconds` and `BolsteringPrayerArmor` becomes `armorBonus`

#### Scenario: Greater Heal applies the buff to all targets

- **WHEN** a Cleric with `bolstering_prayer` casts Greater Heal (`TargetCount: 3`) and hits three allies
- **THEN** all three allies have `BolsteringPrayerRemaining == buffDurationSeconds` and `BolsteringPrayerArmor == armorBonus`

#### Scenario: Buff refreshes duration on recast (does not stack)

- **WHEN** a target with `BolsteringPrayerRemaining == 1.0` receives a fresh Bolstering Prayer'd Heal at `t`, configured `buffDurationSeconds == 5.0`
- **THEN** `BolsteringPrayerRemaining` becomes `5.0` (refresh-longer), not `6.0`; `BolsteringPrayerArmor` is at-most the configured `armorBonus`

#### Scenario: Buff grants armor bonus while active

- **WHEN** a unit has `BolsteringPrayerRemaining > 0` and `BolsteringPrayerArmor == 50`
- **THEN** `effectiveArmorLocked(unit)` includes `+50` in its flat-armor accumulator beyond the unit's base/perk/aura/banner armor

#### Scenario: Buff applies to allies who do not own `bolstering_prayer`

- **WHEN** a non-Cleric ally without `bolstering_prayer` is healed by a Cleric who does own it
- **THEN** the ally receives the buff and gains the armor bonus

#### Scenario: Bolstering Prayer and Battle Prayer stack independently on the same unit

- **WHEN** a unit is healed by a Cleric with `battle_prayer` and (in the same tick or another) by a Cleric with `bolstering_prayer`
- **THEN** the unit has both `BattlePrayerRemaining > 0 && BattlePrayerMultiplier > 0` AND `BolsteringPrayerRemaining > 0 && BolsteringPrayerArmor > 0`, each refreshing independently on re-application

### Requirement: Bolstering Prayer cross-unit decay runs in `state.go Update()`

`BolsteringPrayerRemaining` SHALL be decremented by `dt` every tick in the per-unit loop in `state.go Update()`, in the same block that decays `BattlePrayerRemaining` / `WeakenedRemaining` / `TauntRemaining`. When `BolsteringPrayerRemaining` reaches `<= 0`, `BolsteringPrayerArmor` SHALL also be reset to `0.0` so a stale armor bonus never applies. Decay SHALL NOT happen inside `tickUnitPerkStateLocked` because the buff lives on targets that may not own the `bolstering_prayer` perk.

#### Scenario: Buff decays each tick

- **WHEN** a unit has `BolsteringPrayerRemaining == 5.0` at tick T
- **THEN** at tick T+1 it is `5.0 - dt`

#### Scenario: Buff expiry resets armor field

- **WHEN** `BolsteringPrayerRemaining` decays from a positive value to `<= 0`
- **THEN** both `BolsteringPrayerRemaining` and `BolsteringPrayerArmor` are `0.0` after the decay step, and `effectiveArmorLocked` no longer includes the bonus

#### Scenario: Non-cleric ally's buff decays the same way

- **WHEN** a Soldier with no perks has an active Bolstering Prayer buff applied to it
- **THEN** the buff decays the same way as on any other unit

### Requirement: Bolstering Prayer emits a buff icon on the recipient

`activeBuffIconsLocked` SHALL emit a `bolstering_prayer` buff icon on every unit with `BolsteringPrayerRemaining > 0`, with remaining duration sourced from `BolsteringPrayerRemaining`. The icon SHALL be emitted regardless of whether the unit owns the perk (cross-unit buff convention).

#### Scenario: Healed ally shows Bolstering Prayer buff icon with remaining duration

- **WHEN** an ally has `BolsteringPrayerRemaining > 0`
- **THEN** the HUD displays a Bolstering Prayer buff icon on that ally with the remaining-duration display reflecting `BolsteringPrayerRemaining`

#### Scenario: Buff icon disappears when buff expires

- **WHEN** `BolsteringPrayerRemaining` decays to `0`
- **THEN** the buff icon is no longer emitted in `activeBuffIconsLocked` for that unit

#### Scenario: Both icons co-exist when both buffs are active

- **WHEN** a unit has `BattlePrayerRemaining > 0` AND `BolsteringPrayerRemaining > 0`
- **THEN** both the `battle_prayer` and `bolstering_prayer` buff icons are emitted on that unit

### Requirement: Bolstering Prayer preserves simulation determinism

Bolstering Prayer SHALL be deterministic under a fixed seed and identical inputs:

- Buff application order SHALL follow the multi-target selector's deterministic order (no map iteration).
- Cross-unit decay SHALL iterate `s.Units` in deterministic slice order.
- The armor contribution to `effectiveArmorLocked` SHALL be a pure function of `BolsteringPrayerRemaining`/`BolsteringPrayerArmor` — no RNG, no time-of-wall-clock.

#### Scenario: Two replays with same seed produce identical buff state

- **WHEN** the same seeded match is run twice with identical inputs and a Cleric who acquires `bolstering_prayer`
- **THEN** every per-tick `BolsteringPrayerRemaining`/`BolsteringPrayerArmor` snapshot on every buffed unit is identical between the two runs

## REMOVED Requirements

### Requirement: `greater_heal` perk swaps `"heal"` for `"greater_heal"` in `Unit.Abilities`

**Reason:** Greater Heal is no longer a perk. It is a path-level baseline declared in `cleric.json`'s `"abilities"` override field (loaded by `path_defs.go` into `pathAbilitiesByPath`). Every promotion or recompute via `assignUnitPathAbilitiesLocked` replaces the base apprentice `["heal"]` with the cleric path's `["greater_heal"]` and migrates `AutoCastEnabled` / `AbilityCooldowns` by position. See the `per-path-ability-kits` delta for the path-override mechanism.

**Migration:** Existing units holding the obsolete `greater_heal` perk id in `PerkIDs` keep the id (perk-def lookup returns nil and every hook handles that defensively). Their `Abilities` already contain `"greater_heal"` from the original swap (or now from the path-level override on next recompute), so the unit is functionally equivalent to a Cleric promoted under the new rules.

### Requirement: Greater Heal ability JSON declares `targetCount: 3` and category `heal`

**Reason:** This requirement was scoped to the `greater_heal` perk's enablement and is redundant now that the ability is a path baseline. The ability JSON itself is unchanged — the same `targetCount: 3` / `category: "heal"` / `manaCost: 10` / `healAmount: 10` requirements are enforced by the path-level override validator in `path_defs.go` (which panics at load on a `"abilities"` entry with no registered `AbilityDef`) and by the existing `ability-multi-target` capability spec, which continues to govern multi-target heal semantics.
