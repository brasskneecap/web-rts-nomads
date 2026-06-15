## ADDED Requirements

### Requirement: Per-match zone control state

The system SHALL install a per-match zone runtime from `MapConfig.Zones` at match start, one runtime per authored zone, mirroring the objective runtime installation. Each zone runtime SHALL carry the static zone definition plus mutable control state: the current `owner` (initialised from the zone's `startingOwner`), a capture `progress` value, and a `contested` flag. Zone runtimes SHALL reference zones, buildings, and units by id and SHALL resolve and validate those references each tick they are needed; the runtime SHALL NOT persist a resolved `*Unit` or `*BuildingTile` across ticks.

#### Scenario: Owner initialises from startingOwner
- **WHEN** a match starts on a map whose zone declares `startingOwner: "neutral"`
- **THEN** that zone's runtime owner is `neutral` at the first tick

#### Scenario: Seed zone starts team-owned
- **WHEN** a match starts on a map whose zone declares `startingOwner` of the team's slot
- **THEN** that zone's runtime owner is the team from the first tick, forming the capture frontier

#### Scenario: Zone-free match installs no zone runtime
- **WHEN** a match starts on a map with no zones
- **THEN** no zone runtime is installed and the per-tick zone evaluation is a no-op

### Requirement: Capture mechanic registry

The system SHALL expose a registry of zone capture mechanics keyed by the `capture.type` string, mirroring the objective handler registry. Each mechanic SHALL provide a `parseConfig` that converts the raw capture config into a typed struct, a `validate` that panics at startup on invalid config naming the map file and zone id, and an `evaluate` that updates a zone runtime's control state for one tick. Registering a new mechanic SHALL require only adding one registry entry; no change to the tick loop, the capturability gate, the build-gate, or the snapshot SHALL be required.

#### Scenario: Registry exposes the three initial mechanics
- **WHEN** the server boots
- **THEN** the registry contains mechanics for `control_point`, `presence`, and `clear`

#### Scenario: Unknown capture type rejected at load
- **WHEN** a map JSON declares a zone with `capture.type: "teleport"` for which no mechanic is registered
- **THEN** the catalog loader fails at startup naming the map file, the zone id, and the unknown type

#### Scenario: New mechanic reachable without engine changes
- **WHEN** a future change registers a capture mechanic with a new type key
- **THEN** map JSONs can reference that type and the loader resolves it without modifying the tick loop, gate, build-gate, or snapshot

### Requirement: Adjacency capturability gate

A team SHALL be able to capture a zone only if it already owns at least one zone listed in that zone's `adjacent` set (ownership including allies of the team). A zone not satisfying this gate for any team SHALL be non-capturable, and its capture mechanic SHALL NOT advance its control state that tick — in particular a `presence` timer on a non-capturable zone SHALL NOT advance. The gate SHALL be recomputed from current ownership each tick.

#### Scenario: Adjacent-owned unlocks capture
- **WHEN** zone B is adjacent to zone A, the team owns A, and the team meets B's capture mechanic
- **THEN** B is capturable and its mechanic advances toward flipping B to the team

#### Scenario: Non-adjacent zone stays locked
- **WHEN** zone C is adjacent only to zone B, the team owns neither A nor B, and the team's units sit inside C
- **THEN** C is non-capturable and its capture mechanic does not advance

#### Scenario: Capturing a zone unlocks its neighbours
- **WHEN** the team captures zone B (adjacent to A which it held), and zone C is adjacent to B
- **THEN** C becomes capturable on the subsequent ticks

### Requirement: Capture mechanic — `control_point`

The `control_point` mechanic SHALL set a capturable zone's owner to the owner of the building occupying the zone's `anchor` cell, resolved by id and validated each tick. A destroyed or owner-less structure SHALL leave the zone owner unchanged. Capture SHALL be achievable by occupying the structure (taking ownership of it through the existing building-ownership path); the mechanic SHALL NOT require constructing a new building inside the zone.

#### Scenario: Owning the anchor structure flips the zone
- **WHEN** a capturable zone uses `control_point` and the team comes to own the building on the zone's anchor
- **THEN** the zone's owner becomes the team

#### Scenario: Destroyed structure does not transfer the zone
- **WHEN** the building on a `control_point` zone's anchor is destroyed
- **THEN** the zone owner is left unchanged rather than resolving against a missing building

### Requirement: Capture mechanic — `presence`

The `presence` mechanic SHALL accept a config of `{captureSeconds: number}` where `captureSeconds` MUST be greater than zero. For a capturable zone, the mechanic SHALL advance the zone's `progress` by the tick delta while exactly one non-owning team has units inside the zone's cells and the current owner has none; SHALL set `contested = true` and freeze `progress` while more than one team has units inside; and SHALL flip the owner to the sole present team and reset `progress` when `progress` reaches `captureSeconds`. Membership of a unit in a zone SHALL be determined by the unit's grid cell belonging to the zone.

#### Scenario: Sole occupant captures over time
- **WHEN** a capturable `presence` zone with `captureSeconds: 5` has only the attacking team's units inside for 5 seconds of ticks
- **THEN** the zone's owner flips to that team and progress resets

#### Scenario: Contested presence freezes progress
- **WHEN** a `presence` zone has units from two teams inside
- **THEN** the zone is marked contested and its capture progress does not advance that tick

#### Scenario: Invalid captureSeconds rejected at load
- **WHEN** a `presence` zone declares `captureSeconds: 0`
- **THEN** the catalog loader fails at startup naming the map file and the zone id

### Requirement: Capture mechanic — `clear`

The `clear` mechanic SHALL flip a capturable zone's owner to the capturing team once no hostile unit (neutral or enemy) remains inside the zone's cells, and the ownership SHALL be sticky thereafter. Until the zone is cleared of hostiles it SHALL remain at its starting owner.

#### Scenario: Clearing hostiles captures the zone
- **WHEN** a capturable `clear` zone starts with neutral guards inside and the team kills the last guard inside the zone
- **THEN** the zone's owner flips to the team and remains so

#### Scenario: Remaining hostile blocks capture
- **WHEN** a `clear` zone still has one neutral unit inside its cells
- **THEN** the zone is not captured

### Requirement: Ownership-gated building

`BuildBuilding` SHALL reject a placement whose footprint includes any cell that belongs to a zone whose owner is not allied with the building player. Footprint cells that belong to no zone SHALL NOT be restricted by this rule. A rejected placement SHALL NOT spend resources, consistent with the function's other rejection paths.

#### Scenario: Building in an uncontrolled zone is rejected
- **WHEN** a player attempts to place a building whose footprint overlaps a zone owned by `neutral`
- **THEN** the placement is rejected and no resources are spent

#### Scenario: Building in a controlled zone is allowed
- **WHEN** a player attempts to place a building entirely inside a zone owned by the player's team
- **THEN** the placement proceeds subject to the existing footprint/blocked/tier/cost checks

#### Scenario: Building outside any zone is unaffected
- **WHEN** a player places a building whose footprint touches no zone cell
- **THEN** the zone rule does not apply and placement follows the existing checks

#### Scenario: Footprint straddling a controlled and an uncontrolled zone is rejected
- **WHEN** a building footprint covers one cell in a team-owned zone and one cell in a neutral zone
- **THEN** the placement is rejected because not every zone-owned footprint cell is allied with the builder

### Requirement: `capture_zone` objective type

The system SHALL register a `capture_zone` objective handler accepting a config of `{zoneIds: string[], requireAll?: bool}` where `zoneIds` MUST be non-empty and each id MUST name a zone in the match's map (validated at load). Evaluation SHALL read current zone ownership from the zone runtime and complete the objective when the evaluating scope's team owns the referenced zone(s): all of them when `requireAll` is true, otherwise any one. Completion SHALL be sticky per the objective system's absorbing-completion semantics, and an objective marked `required` SHALL gate victory through the existing required-objective rule.

#### Scenario: Capturing the referenced zone completes the objective
- **WHEN** a `capture_zone` objective references `["zone-north"]` and the team captures `zone-north`
- **THEN** the objective is marked completed

#### Scenario: requireAll waits for every zone
- **WHEN** a `capture_zone` objective references `["a","b"]` with `requireAll: true` and the team owns `a` but not `b`
- **THEN** the objective is not completed until the team also owns `b`

#### Scenario: Completion is sticky after losing the zone
- **WHEN** a `capture_zone` objective completes on capturing a zone and the team later loses that zone
- **THEN** the objective remains completed

#### Scenario: Unknown zone id rejected at load
- **WHEN** a `capture_zone` objective references a zone id absent from the level's map
- **THEN** the catalog loader fails at startup naming the offending objective and zone id

### Requirement: Zone snapshot exposure

The system SHALL deliver static zone geometry (cells, anchor, adjacency, capture type) to the client once in the welcome payload, and SHALL include a per-tick `ZoneSnapshot` carrying each zone's `id`, current `owner`, `contested` flag, and capture `progress` in the match snapshot. The client SHALL render owner tint, contested state, capture progress, and locked-versus-capturable indication from these fields and SHALL NOT compute capture outcomes itself.

#### Scenario: Static geometry sent once
- **WHEN** a player joins a match on a zoned map
- **THEN** the welcome payload contains the zones' cells, anchors, adjacency, and capture types

#### Scenario: Per-tick snapshot carries mutable control state
- **WHEN** a match snapshot is built during play
- **THEN** it contains a `ZoneSnapshot` per zone with the zone's current owner, contested flag, and capture progress

### Requirement: Deterministic, tick-path-safe zone evaluation

Zone capture evaluation SHALL run inside the tick loop while holding the state lock, SHALL be deterministic under a fixed seed and input sequence (no wall-clock time, no unseeded RNG, no reliance on Go map iteration order to drive ownership outcomes), and SHALL perform no I/O. Iteration over zones for evaluation SHALL use the stable authored zone order.

#### Scenario: Identical inputs produce identical capture timelines
- **WHEN** two runs use the same seed, the same map, and the same unit/command sequence
- **THEN** every zone's ownership and capture progress match tick-for-tick across the two runs

#### Scenario: Evaluation performs no I/O
- **WHEN** the tick loop runs zone evaluation
- **THEN** no filesystem, network, or profile-store access occurs on that code path
