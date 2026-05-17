## ADDED Requirements

### Requirement: `AbilityCategory` is an extensible, registered string enum

The system SHALL define an `AbilityCategory` string type with registered values `heal` (`AbilityCategoryHeal`), `buff_ally` (`AbilityCategoryBuffAlly`), `summon` (`AbilityCategorySummon`), and `offensive` (`AbilityCategoryOffensive`). It SHALL follow the existing `DamageType` registry pattern: a registry map, a `RegisterAbilityCategory` function that panics on an empty id, an `IsValidAbilityCategory` predicate, and an `AbilityCategories()` accessor returning the registered values in a stable sorted order.

The empty value (`""`) SHALL be the reserved "unspecified" default: it SHALL NOT be registerable and `IsValidAbilityCategory("")` SHALL return false, identical to `DamageType` semantics.

#### Scenario: Registered categories validate

- **WHEN** `IsValidAbilityCategory` is called with `heal`, `buff_ally`, `summon`, or `offensive`
- **THEN** it returns true for each

#### Scenario: Empty and unknown values do not validate

- **WHEN** `IsValidAbilityCategory` is called with `""` or an unregistered string
- **THEN** it returns false

#### Scenario: Registering an empty category panics

- **WHEN** `RegisterAbilityCategory("")` is called
- **THEN** it panics, reserving the empty value as "unspecified"

#### Scenario: Accessor returns a stable sorted list

- **WHEN** `AbilityCategories()` is called
- **THEN** it returns all registered categories in deterministic sorted order

### Requirement: `AbilityDef` carries an optional `Category` field validated at load

`AbilityDef` SHALL have a `Category AbilityCategory` field serialised as the optional JSON key `"category"` (omitted when empty). At ability catalog load, the loader SHALL panic if a definition's `Category` is non-empty and not a registered category, mirroring the existing `damageType` load validation.

A definition with no `"category"` key SHALL load successfully with `Category == ""`. In Phase 1 no code reads `Category`; the field SHALL have no effect on ability behaviour, autocast selection, or any wire/snapshot shape.

#### Scenario: Ability without a category loads with empty default

- **WHEN** an ability JSON file omits the `"category"` key
- **THEN** the definition loads successfully and its `Category` is `""`

#### Scenario: Ability with a valid category loads

- **WHEN** an ability JSON file declares `"category": "heal"`
- **THEN** the definition loads successfully with `Category == AbilityCategoryHeal`

#### Scenario: Ability with an invalid category panics at load

- **WHEN** an ability JSON file declares a non-empty `"category"` that is not registered
- **THEN** the catalog load panics with a message naming the offending file and category

#### Scenario: Category is inert in Phase 1

- **WHEN** any ability is cast or auto-cast in Phase 1
- **THEN** the presence or value of `Category` changes no behaviour, no autocast decision, and no snapshot field

### Requirement: The `heal` ability is tagged `category: heal`

`catalog/abilities/heal/heal.json` SHALL gain an additive `"category": "heal"` field. All other fields of `heal.json` SHALL be unchanged, and the `Category` tag SHALL be inert: it SHALL NOT alter heal's runtime behaviour, autocast gating, or any wire/snapshot shape.

#### Scenario: Heal definition exposes the heal category

- **WHEN** the `heal` ability definition is loaded
- **THEN** its `Category` is `AbilityCategoryHeal` and every other field matches its pre-change value

### Requirement: Heal-autocast guarantee is gating-equivalence plus a no-melee tripwire

The heal autocast selector (`lowest_hp_percentage_ally_in_range`, `castRange: match_attack_range`) is position-gated, and the `caster` combat profile deliberately moves the unit (retreat). A blanket "byte-identical heal autocast under any fixed seed" claim is therefore false and SHALL NOT be asserted: once a `caster`-profiled Apprentice retreats, its position changes, which legitimately changes which allies are in heal range and thus which ticks heal fires on. That divergence is correct behaviour, not a regression.

The system SHALL instead guarantee: (a) the heal-autocast **gating logic** (mana availability, cooldown, and the `SupportsAutoCast` / selector predicate) is unchanged by the `caster` profile flip and the `Category` tag; and (b) in a scenario with **no melee threat** (the Apprentice never retreats, so position is held constant), the set of ticks heal is auto-cast on is identical pre/post change for the same seed and inputs.

#### Scenario: Heal-autocast gating is unchanged by the profile flip and category tag

- **WHEN** the heal-autocast gate (mana / cooldown / selector predicate) is evaluated for a fixed unit and world state before and after this change
- **THEN** the gate's decision (cast / do not cast, and the selected target) is identical between the two

#### Scenario: No profile↔cadence coupling in a no-melee seeded replay

- **WHEN** a seeded match with an autocasting Apprentice and **no melee threat to it** is run before and after this change with the same seed and inputs
- **THEN** the set of ticks on which heal is cast is identical between the two runs (a tripwire for unintended profile↔cadence coupling)

#### Scenario: Cadence divergence under retreat is expected, not asserted against

- **WHEN** a `caster`-profiled Apprentice retreats from a melee attacker and its heal-target-in-range set changes as a result
- **THEN** the resulting change in heal cast ticks is treated as correct intended behaviour and is explicitly outside the byte-identical guarantee
