## ADDED Requirements

### Requirement: Zone aura definition

A zone definition SHALL optionally carry an array of aura effects. Each aura SHALL carry a `type` discriminator and, for the initial `stat_modifier` type, an embedded `StatModifier` expressed in the shared stat-modifier vocabulary (the same stat ids and operations used by perks, buffs, and other modifier sources). The system SHALL NOT define special-purpose aura effect types (such as a worker-gold multiplier or a unit-health-regen effect); aura bonuses SHALL be expressed only as stat modifiers. Each aura SHALL carry a `scope` defaulting to `global`. Auras SHALL be validated at map load, naming the map file and zone id on failure.

#### Scenario: Zone declares stat-modifier auras
- **WHEN** a zone declares `auras: [{type: "stat_modifier", modifier: {stat: "healthRegen", operation: "add", value: 2}}, {type: "stat_modifier", modifier: {stat: "goldGatherRate", operation: "multiply", value: 1.15}}]`
- **THEN** the zone loads with two stat-modifier auras and no special-purpose effect type is introduced

#### Scenario: Zone without auras loads unchanged
- **WHEN** a zone declares no `auras`
- **THEN** the zone loads with an empty aura list and grants no bonuses

#### Scenario: Aura referencing an unknown stat rejected at load
- **WHEN** a zone aura references a stat id absent from the stat registry
- **THEN** the catalog loader fails at startup naming the map file and zone id

#### Scenario: Aura with an invalid operation rejected at load
- **WHEN** a zone aura declares an operation other than `add` or `multiply`
- **THEN** the catalog loader fails at startup naming the map file and zone id

### Requirement: Player Zone Aura Manager

The system SHALL provide a per-player Zone Aura Manager that determines the zones a player currently owns, collects those zones' active aura modifiers, aggregates them into the player's stat-modifier set, and exposes the result to the existing stat pipeline. Units, workers, and buildings SHALL NOT individually poll zone ownership; the manager SHALL be the single owner of zone-to-player bonus aggregation. Aggregation SHALL iterate zones in the stable authored order and SHALL reuse the existing team/ally relationship so a team-owned zone feeds every allied player's aggregate.

#### Scenario: Manager aggregates owned-zone auras
- **WHEN** a player owns a zone granting `+2 healthRegen` and a second zone granting `+3 healthRegen`
- **THEN** the player's aggregated `healthRegen` additive contribution is `+5`

#### Scenario: Units do not poll zones
- **WHEN** a unit's effective stat is read on the hot path
- **THEN** it resolves the owner's aggregated modifier set in constant time without scanning zones

#### Scenario: Team-owned zone feeds allied players
- **WHEN** a zone is owned by the shared team and two allied players are on that team
- **THEN** both allied players' aggregates include that zone's auras

### Requirement: Aura application gated on current ownership

Aura effects SHALL apply to a player only while that player (or an ally) currently controls the granting zone. When a zone has no player owner (neutral), its auras SHALL apply to no one.

#### Scenario: Owned zone grants its bonuses
- **WHEN** a player controls a zone with a `+10% moveSpeed` aura
- **THEN** that player's units move at the increased speed

#### Scenario: Neutral zone grants nothing
- **WHEN** a zone with auras is owned by `neutral`
- **THEN** no player receives its bonuses

### Requirement: Ownership-change aura transfer and teardown

When a zone's ownership changes, the system SHALL remove the previous owner's bonuses from that zone and apply the bonuses to the new owner, recomputing each affected player's aggregated modifier set. When ownership is lost (the zone becomes neutral or is captured by an enemy), the previous owner's bonuses from that zone SHALL be removed immediately. Recompute SHALL be triggered by the ownership change event, not by per-tick polling, and SHALL route through the single point at which a zone's owner is reassigned.

#### Scenario: Capture transfers bonuses to the new owner
- **WHEN** player B captures a zone previously owned by player A
- **THEN** player A's aggregate no longer includes that zone's auras and player B's aggregate now includes them

#### Scenario: Losing a zone removes its bonuses immediately
- **WHEN** a player loses control of a zone (it flips to neutral or an enemy)
- **THEN** that zone's auras are removed from the player's aggregate without waiting for a subsequent recompute

#### Scenario: Max-health aura tracks ownership
- **WHEN** a player captures a zone granting a `maxHealth` bonus and later loses it
- **THEN** the player's units gain the increased maximum health on capture and return to their prior maximum on loss, preserving health fraction across the change

### Requirement: Multiple-zone stacking

A player owning multiple zones SHALL receive the combined bonuses of all owned zones' auras, stacked through the shared modifier stacking rule: additive modifiers for the same stat sum, and multiplicative modifiers for the same stat multiply.

#### Scenario: Additive bonuses across zones sum
- **WHEN** a player owns one zone granting `+2 healthRegen` and another granting `+3 healthRegen`
- **THEN** the player's units receive `+5 healthRegen`

#### Scenario: Multiplicative bonuses across zones multiply
- **WHEN** a player owns two zones each granting `×1.15 goldGatherRate`
- **THEN** the player's effective gold gather rate multiplier is `×1.3225`

### Requirement: Map editor aura authoring

The map editor SHALL allow each capturable zone to configure its aura effects: add an aura, remove an aura, select the stat from the shared registry, select the operation (`add` or `multiply`), and configure the value. Authored auras SHALL persist into the zone's `auras` array through the existing map-save path. The stat selector SHALL be driven by the stat registry so an unknown stat cannot be authored.

#### Scenario: Author adds an aura to a zone
- **WHEN** an editor user opens a zone, adds an aura, selects `healthRegen` / `add` / `2`
- **THEN** the zone's `auras` array gains `{type: "stat_modifier", modifier: {stat: "healthRegen", operation: "add", value: 2}}` and persists on save

#### Scenario: Author removes an aura
- **WHEN** an editor user removes an aura row from a zone
- **THEN** that aura is removed from the zone's `auras` array and the change persists on save

#### Scenario: Stat selector is registry-driven
- **WHEN** an editor user opens the stat selector for an aura
- **THEN** the options are exactly the registered stat ids with their display labels

### Requirement: Zone inspection UI

When a captured zone is selected, the client SHALL display the zone's name, its current owner (player name and color from the zone snapshot), and the list of granted bonuses formatted from the zone's aura definitions using the shared stat labels (for example `+2 Health Regen`, `+15% Gold Gather Rate`, `+10% Move Speed`). The UI SHALL read aura definitions from the static zone data and ownership from the live snapshot, and SHALL NOT compute bonus application itself.

#### Scenario: Selecting an owned zone shows owner and bonuses
- **WHEN** a player selects a zone they control that grants `+2 healthRegen` and `+15% goldGatherRate`
- **THEN** the panel shows the owner and a bonuses list reading `+2 Health Regen` and `+15% Gold Gather Rate`

#### Scenario: Bonuses formatted from static aura data
- **WHEN** the inspection panel renders a zone's bonuses
- **THEN** the values come from the zone's static aura definitions and the labels from the shared stat registry, not from any per-tick computed field

#### Scenario: Zone with no auras shows no bonuses section
- **WHEN** a player selects a captured zone that defines no auras
- **THEN** the panel shows the owner and no bonuses are listed

### Requirement: Extension points for future aura types and scopes

The aura system SHALL be structured so future aura kinds and scopes can be added without changing the v1 path. The aura `type` discriminator SHALL allow new kinds (such as periodic effects, unit spawning, resource generation, vision bonuses, or enemy debuffs) to be added by handling a new type in the aggregator/manager, leaving the zone schema, the editor save path, and the ownership-change hook unchanged. The aura `scope` field SHALL allow a future local/radius scope to be added by interpreting a non-`global` scope at evaluation time, leaving the global path unchanged.

#### Scenario: Unknown aura type does not break global stat auras
- **WHEN** a zone declares a registered future aura type alongside a `stat_modifier` aura
- **THEN** the `stat_modifier` aura still aggregates correctly and the new type is handled by its own code path

#### Scenario: Scope defaults to global
- **WHEN** a `stat_modifier` aura omits `scope`
- **THEN** it is treated as `global` and applies to all of the owner's units, workers, and buildings regardless of position
