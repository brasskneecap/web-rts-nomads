## ADDED Requirements

### Requirement: A forced-displacement effect moves target units toward a center over a duration

The system SHALL provide a forced-displacement (pull) control effect that, for a set of affected units referenced by ID, applies a deterministic per-tick position delta toward a pull center for a bounded duration. Each affected unit and the pull center SHALL be resolved and validated every tick (a removed or dead unit is dropped). The per-tick delta SHALL be a pure function of the unit's current position, the center, `pullStrength`, and the tick delta-time — no wall-clock time and no unseeded randomness. The effect SHALL end when its duration elapses.

#### Scenario: A pulled unit moves toward the center each tick

- **WHEN** a forced-displacement effect is active on an enemy unit offset from the pull center
- **THEN** each tick the unit's position moves toward the center by an amount derived from `pullStrength`, until the effect's duration elapses

#### Scenario: Displacement is deterministic under a seed

- **WHEN** two seeded runs apply the same pull to the same units from the same positions
- **THEN** the resulting per-tick positions are identical across runs

#### Scenario: A removed target is dropped mid-pull

- **WHEN** a pulled unit dies or is removed while the effect is active
- **THEN** the effect stops displacing that unit and raises no error, continuing for any remaining targets

### Requirement: Displacement overrides normal movement without corrupting pathing

While a unit is under forced displacement, the displacement SHALL take precedence over its normal move/path advancement for that tick, and when the effect ends the unit SHALL resume normal AI/movement from its current position without a stale path driving it back. Displacement SHALL target enemy units of the caster only (it SHALL NOT pull allies or the caster). The collision behavior of pulled units (clip-through vs. clamp-to-walkable) SHALL be a single explicit, documented decision applied consistently (see design.md).

#### Scenario: Pulled unit does not simultaneously path elsewhere

- **WHEN** a unit is mid-move on an order and a pull takes effect
- **THEN** for the duration the pull drives its position, and normal path advancement does not fight the displacement in the same tick

#### Scenario: Unit resumes cleanly after the pull ends

- **WHEN** a forced-displacement effect on a unit ends
- **THEN** the unit resumes normal movement/AI from its displaced position with no stale pre-pull path snapping it back

#### Scenario: Only enemies are pulled

- **WHEN** a pull center overlaps both allied and hostile units relative to the caster
- **THEN** only hostile units are displaced; allies and the caster are unaffected

### Requirement: Pull strength is modifier-eligible

The displacement's `pullStrength` SHALL be exposed as a modifier-eligible field so that spell modifiers (e.g. an Arch Mage perk) can scale it at cast time via the spell-modifier pipeline, without mutating the base spell definition.

#### Scenario: A pull-strength modifier scales the displacement

- **WHEN** an active modifier applies `+30%` to `pullStrength` on a pulling spell
- **THEN** the effect resolves with a proportionally larger per-tick displacement, and the base spell definition is unchanged
