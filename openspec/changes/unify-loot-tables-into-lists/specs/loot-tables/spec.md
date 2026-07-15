## ADDED Requirements

### Requirement: A table is a weighted roll over lists, resources, and nothing

The catalog SHALL provide a table entity: a `maxRoll` die plus rows, where each row owns a roll range and exactly ONE outcome:

- **list** — roll that list and grant the item it yields
- **resources** — grant the named resources (gold / wood)
- **nothing** — grant nothing

A row declaring zero outcomes, or more than one, SHALL be rejected at load. A row naming a list that does not resolve SHALL be rejected. A row granting a resource outside the allowed set SHALL be rejected.

#### Scenario: A table rolls a list
- **WHEN** a table is rolled and the roll lands on a row naming a list
- **THEN** that list is rolled, and the item it yields is granted

#### Scenario: A table grants resources
- **WHEN** the roll lands on a row granting resources
- **THEN** exactly those resources are granted, and no item is

#### Scenario: A table drops nothing
- **WHEN** the roll lands on a `nothing` row
- **THEN** no item and no resources are granted, and no chest is produced

#### Scenario: A row with two outcomes is rejected
- **WHEN** a row names both a list and a resource grant
- **THEN** the catalog SHALL reject the table at load

### Requirement: A table's rows cover every roll, exactly once

A table's rows SHALL tile `1..maxRoll` with no gaps and no overlaps. Every possible roll SHALL land on exactly one row.

A gap SHALL be a validation error, not an implicit no-drop: "nothing happens" is expressed by a `nothing` row, so that it is visible, nameable, and readable as a percentage.

#### Scenario: A gap is rejected
- **WHEN** a table's rows leave a roll uncovered
- **THEN** the catalog SHALL reject the table, naming the uncovered range

#### Scenario: An overlap is rejected
- **WHEN** two rows claim the same roll
- **THEN** the catalog SHALL reject the table, naming the overlapping range

#### Scenario: A no-drop chance is expressed, not implied
- **WHEN** an author wants a table to drop nothing 10% of the time on a d100
- **THEN** they add a `nothing` row covering 10 rolls, and the table validates

### Requirement: Table rolls are deterministic under a seed

A table roll, and any list roll it triggers, SHALL draw from the seeded loot RNG, so a fixed seed reproduces the same outcome.

#### Scenario: Same seed, same drop
- **WHEN** the same table is rolled twice under the same seed
- **THEN** both rolls produce the same outcome

## REMOVED Requirements

### Requirement: Loot is defined by packaged items

**Reason**: `packagedItems` held two unrelated things under one key. Its `item_subtable` entries were *literally* weighted lists — item IDs with roll ranges — and had no reason to exist as a separate, unauthorable entity once lists were unified. Its `resource_bundle` entries existed only so two tables could share a `{gold, wood}` pair, which does not justify a catalog of its own.

**Migration**: The 7 `item_subtable`s become weighted lists in `catalog/lists/`, keeping their exact roll ranges. The 2 `resource_bundle`s become inline `resources` rows on the tables that referenced them. `catalog/neutral_groups/loot_tables.json` is deleted in favour of `catalog/tables/<id>.json`.
