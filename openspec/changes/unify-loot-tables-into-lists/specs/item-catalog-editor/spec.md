## ADDED Requirements

### Requirement: The item editor has a Tables tab

The item editor SHALL present three tabs: **Items**, **Lists**, and **Tables**. The Tables tab SHALL provide full create, edit and delete of tables, with the same authoring affordances as lists.

Only the active tab's panel SHALL be shown; a tab REPLACES the work surface rather than sitting beside it.

#### Scenario: Switching to Tables
- **WHEN** an author selects the Tables tab
- **THEN** the table authoring surface replaces the panel that was showing, and the sidebar lists the catalog's tables

#### Scenario: Authoring a table end to end
- **WHEN** an author creates a table, sets its max roll, adds rows covering it, and saves
- **THEN** the table is persisted and is selectable wherever a loot table is chosen

### Requirement: The editor shows what every roll lands on

For any table, and for any weighted list, the editor SHALL display the coverage of the die: which range yields which outcome, and what percentage of rolls that is.

An author SHALL be able to read the distribution they authored without doing arithmetic.

#### Scenario: Coverage is shown per row
- **WHEN** an author edits a table with a row covering 1–50 of a 100-roll die
- **THEN** the editor shows that row as 50% of rolls

#### Scenario: A no-drop row reads as a percentage
- **WHEN** a table has a `nothing` row covering 10 of 100 rolls
- **THEN** the editor shows it as a 10% chance of dropping nothing

### Requirement: The editor blocks an incomplete die

A gap, an overlap, or a range outside `1..maxRoll` SHALL block the save, with a message naming the offending rolls.

#### Scenario: A gap blocks the save
- **WHEN** an author leaves rolls 51–60 uncovered
- **THEN** the editor refuses the save and says which rolls land nowhere

#### Scenario: An overlap blocks the save
- **WHEN** two rows both claim roll 40
- **THEN** the editor refuses the save and says which rolls are claimed twice

#### Scenario: A complete die saves
- **WHEN** every roll from 1 to maxRoll is claimed exactly once
- **THEN** the coverage reads as complete and the save is allowed
