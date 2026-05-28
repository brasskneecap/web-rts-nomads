## ADDED Requirements

### Requirement: Kill drops fund the Legend Point currency

The system SHALL grant Legend Points to the killing player when a non-allied unit dies, using the rates defined in `gameplay_tuning.json`. The base drop rate SHALL be 5% chance per kill for 1 Legend Point, configurable via `legendPoints.perKillBaseDropChance` and `legendPoints.perKillBaseAmount`. Per-unit overrides defined under `unitOverrides` SHALL take precedence over the base values. Drops SHALL use the match's seeded loot RNG so replays are deterministic. Drops accumulated during a match SHALL persist to `PlayerProfile.LegendPoints` and `PlayerProfile.LifetimeLegendPoints` at match end.

#### Scenario: Enemy unit dies to a player attack and drop roll succeeds
- **WHEN** an enemy-owned unit is killed by a player-owned unit and the seeded RNG roll is below 0.05
- **THEN** the killing player's `Player.RunLegendPointDrops` increments by 1 and, at match end, `PlayerProfile.LegendPoints` and `PlayerProfile.LifetimeLegendPoints` each increase by the same total

#### Scenario: Friendly fire does not drop Legend Points
- **WHEN** a unit dies to damage from a unit owned by the same player
- **THEN** no Legend Point drop is rolled and no points are awarded

#### Scenario: Per-unit override replaces base rate
- **WHEN** a unit type has an entry in `unitOverrides` with `legendPointDropChance = 0.5` and the unit is killed by a different player
- **THEN** the drop chance used for the roll is 0.5, not the base 0.05

### Requirement: Catalog-driven profile upgrade definitions

The system SHALL load profile upgrade definitions from `server/internal/game/catalog/profile-upgrades/<id>.json`. Each definition SHALL declare an `id`, `name`, `description`, `maxRanks > 0`, a `costPerRank` array whose length equals `maxRanks`, and a typed `effect` object. The catalog SHALL be validated at startup: missing fields, mismatched `costPerRank` length, duplicate IDs, or unknown `effect.type` values MUST cause the server to refuse to start. The catalog SHALL be addable-to without changes to existing definitions or call sites — new effect types are introduced by registering a new effect handler.

#### Scenario: Server starts with valid catalog
- **WHEN** the server boots with `additional_worker.json`, `physical_power.json`, and `magic_power.json` under `catalog/profile-upgrades/`
- **THEN** the server starts successfully and each definition is retrievable by ID

#### Scenario: Catalog with duplicate IDs is rejected
- **WHEN** two files in the catalog directory declare the same `id`
- **THEN** the server panics at startup with a message naming both files

#### Scenario: Catalog with mismatched cost array is rejected
- **WHEN** a definition declares `maxRanks: 3` but `costPerRank: [10, 20]`
- **THEN** the server panics at startup with a message naming the offending file

#### Scenario: Unknown effect type is rejected
- **WHEN** a definition declares `effect.type: "fooBar"` with no registered handler
- **THEN** the server panics at startup with a message naming the offending file and the unknown type

### Requirement: Initial profile upgrade catalog

The system SHALL ship with the following three profile upgrades on first release:

- `additional_worker` — `maxRanks: 2`, `costPerRank: [25, 100]`, effect `extraStartingUnit { unitType: "worker", countPerRank: 1 }`.
- `physical_power` — `maxRanks: 10`, `costPerRank: [10, 20, 30, 40, 50, 60, 70, 80, 90, 100]`, effect `damageMultiplierByType { damageTypeClass: "physical", multiplierPerRank: 0.10 }`.
- `magic_power` — `maxRanks: 10`, `costPerRank: [10, 20, 30, 40, 50, 60, 70, 80, 90, 100]`, effect `damageMultiplierByType { damageTypeClass: "nonPhysical", multiplierPerRank: 0.10 }`.

#### Scenario: Catalog API returns the three initial upgrades
- **WHEN** the client requests `GET /api/catalog/profile-upgrades`
- **THEN** the response body contains exactly three upgrade definitions with IDs `additional_worker`, `physical_power`, `magic_power` and their declared cost arrays

### Requirement: Persistent owned-rank state per profile

The system SHALL persist purchased ranks on `PlayerProfile.OwnedUpgradeRanks` as a `map[string]int` keyed by upgrade ID. The `PlayerProfile` schema version SHALL be incremented when this field is introduced. Profiles written under the prior version SHALL be migrated forward on read by initializing `OwnedUpgradeRanks` to an empty map; no existing field SHALL be removed or renamed.

#### Scenario: New profile starts with no owned ranks
- **WHEN** a brand-new profile is created via `GetOrCreate`
- **THEN** `OwnedUpgradeRanks` is an empty (non-nil) map and the profile's `Version` equals the new current version

#### Scenario: Existing v1 profile is migrated on read
- **WHEN** the server loads a profile that was written under the prior schema version and lacks `ownedUpgradeRanks`
- **THEN** the loaded profile's `OwnedUpgradeRanks` is an empty map and subsequent mutations write the new schema version on save

### Requirement: Purchase endpoint

The system SHALL expose `POST /api/profile/upgrades/purchase` taking a JSON body `{ "upgradeId": "<id>" }` and the `X-Player-ID` header. The endpoint SHALL atomically: verify the upgrade exists, verify the current rank is less than `maxRanks`, verify the player has at least `costPerRank[currentRank]` Legend Points, debit that cost from `LegendPoints`, and increment the rank. On success it SHALL return the updated profile; on failure it SHALL return a 400 with one of the error codes `unknown_upgrade`, `max_rank_reached`, `insufficient_legend_points`.

#### Scenario: Successful first rank purchase
- **WHEN** a player with 25 Legend Points POSTs `{ "upgradeId": "additional_worker" }`
- **THEN** the response is 200 with `OwnedUpgradeRanks["additional_worker"] == 1` and `LegendPoints == 0`

#### Scenario: Second rank costs more
- **WHEN** a player at `additional_worker` rank 1 with 99 Legend Points POSTs a purchase for `additional_worker`
- **THEN** the response is 400 with `error: "insufficient_legend_points"` and no profile mutation occurs

#### Scenario: Cannot exceed max rank
- **WHEN** a player at `additional_worker` rank 2 POSTs a purchase for `additional_worker`
- **THEN** the response is 400 with `error: "max_rank_reached"`

#### Scenario: Unknown upgrade id is rejected
- **WHEN** a player POSTs `{ "upgradeId": "not_a_real_upgrade" }`
- **THEN** the response is 400 with `error: "unknown_upgrade"`

### Requirement: Refund endpoint

The system SHALL expose `POST /api/profile/upgrades/refund` taking a JSON body `{ "upgradeId": "<id>" }` and the `X-Player-ID` header. The endpoint SHALL atomically: verify the upgrade exists, verify the current rank is at least 1, refund `costPerRank[currentRank - 1]` Legend Points back to `LegendPoints`, and decrement the rank by 1. `LifetimeLegendPoints` SHALL NOT change on refund. On success it SHALL return the updated profile; on failure it SHALL return a 400 with one of the error codes `unknown_upgrade`, `not_owned`.

#### Scenario: Refund of last-acquired rank returns its exact cost
- **WHEN** a player at `additional_worker` rank 2 with 0 Legend Points POSTs `{ "upgradeId": "additional_worker" }` to the refund endpoint
- **THEN** the response is 200 with `OwnedUpgradeRanks["additional_worker"] == 1` and `LegendPoints == 100`

#### Scenario: Refunding an upgrade at rank 0 is rejected
- **WHEN** a player who has never purchased `physical_power` POSTs a refund for it
- **THEN** the response is 400 with `error: "not_owned"` and no profile mutation occurs

#### Scenario: Refund does not credit lifetime
- **WHEN** a player refunds a rank
- **THEN** `LifetimeLegendPoints` is unchanged by the refund

### Requirement: Profile and catalog read endpoints expose upgrades

The `GET /api/profile` response SHALL include the player's `ownedUpgradeRanks` and a catalog of profile upgrade definitions sufficient for the client to render the panel without a second request. The catalog SHALL also be available standalone at `GET /api/catalog/profile-upgrades`.

#### Scenario: Profile response includes catalog and owned ranks
- **WHEN** the client requests `GET /api/profile` with a valid `X-Player-ID`
- **THEN** the JSON body contains both `profile.ownedUpgradeRanks` (object, keyed by upgrade id) and `profileUpgradeCatalog` (array of definitions)

### Requirement: Match-start application of owned ranks

When a player joins a match, the server SHALL read the player's `OwnedUpgradeRanks` once and snapshot the resulting effects onto the in-match `Player` struct. The snapshot SHALL include precomputed `PhysicalDamageMultiplier`, `MagicDamageMultiplier`, and `ExtraStartingWorkers` derived from the catalog effects. Mutations to the underlying profile during the match SHALL NOT affect the running match. Effect application SHALL be deterministic: iterating ranks in upgrade-ID order MUST produce the same final values for the same input.

#### Scenario: Player with no purchases gets default multipliers
- **WHEN** a player with empty `OwnedUpgradeRanks` joins a match
- **THEN** their `Player.PhysicalDamageMultiplier == 1.0`, `Player.MagicDamageMultiplier == 1.0`, and `Player.ExtraStartingWorkers == 0`

#### Scenario: Physical power rank 3 yields +30% physical multiplier
- **WHEN** a player with `physical_power` at rank 3 joins a match
- **THEN** their `Player.PhysicalDamageMultiplier == 1.30` and `Player.MagicDamageMultiplier == 1.0`

#### Scenario: Mid-match purchase does not affect active match
- **WHEN** a player completes a purchase via the HTTP endpoint while a match they are in is running
- **THEN** the running match's `Player.PhysicalDamageMultiplier` is unchanged for that match

### Requirement: Damage pipeline applies per-player multipliers

Outgoing damage from a player-owned unit SHALL be multiplied by that player's `PhysicalDamageMultiplier` when the resolved damage type is `DamagePhysical`, and by `MagicDamageMultiplier` otherwise. The multiplication SHALL be applied once per damage event, in the existing damage pipeline, after type resolution and before final mitigation. Unowned (neutral / enemy AI) units SHALL be unaffected.

#### Scenario: Physical attack from a rank-3 physical_power player
- **WHEN** a unit owned by a player with `PhysicalDamageMultiplier = 1.30` deals an attack with base damage 100 and physical damage type
- **THEN** the damage event applied to the target has a base of 130 (before any further wave/item/buff modifiers)

#### Scenario: Magic attack from a rank-3 physical_power player
- **WHEN** a unit owned by a player with `PhysicalDamageMultiplier = 1.30` and `MagicDamageMultiplier = 1.0` deals an attack with damage type `fire`
- **THEN** the damage event is unmodified by physical_power (still base 100 from the magic-multiplier branch)

#### Scenario: Enemy AI is unaffected
- **WHEN** an enemy-AI-owned unit attacks a player unit
- **THEN** no profile upgrade multiplier is applied to the enemy attack

### Requirement: Extra starting workers spawn alongside authored units

For each rank of `additional_worker` a player owns, the server SHALL spawn one extra unit of the configured `unitType` near the player's claimed townhall during match setup. Extra workers SHALL be placed on a walkable cell via the existing nearest-walkable search. If no walkable cell is available within the search radius, the server SHALL log a warning and skip the unit rather than crashing. Authored placed units from the map SHALL spawn unconditionally; profile upgrade workers are additive.

#### Scenario: Rank 2 player gets two extra workers
- **WHEN** a player with `additional_worker` rank 2 joins a match
- **THEN** in addition to the map-authored starting units, two additional workers are spawned for that player on walkable cells near the claimed townhall

#### Scenario: No walkable cell is handled gracefully
- **WHEN** the area around the townhall is fully blocked and no walkable cell can be found
- **THEN** a warning is logged and the match continues without crashing; the player simply does not receive that extra worker

### Requirement: UI Upgrades panel

The Profile view's Upgrades tab SHALL render every upgrade definition returned by the catalog. For each upgrade it SHALL display name, description, current rank vs. `maxRanks`, next-rank cost (or "Maxed" when at cap), and a refund value equal to the cost of the player's current rank. The panel SHALL provide Buy and Refund actions that call the corresponding HTTP endpoints and re-render against the returned profile. The placeholder "Coming Soon" card SHALL be removed.

#### Scenario: Catalog renders all initial upgrades
- **WHEN** the Profile view's Upgrades tab is opened with the three initial upgrades present in the catalog
- **THEN** three cards are visible, each showing the upgrade name, description, current rank / max rank, and next-rank cost

#### Scenario: Buy action debits points and re-renders rank
- **WHEN** the user clicks Buy on `additional_worker` and has at least 25 Legend Points
- **THEN** the displayed Legend Point total decreases by 25 and the rank display advances to "1 / 2"

#### Scenario: Refund returns points and decrements rank
- **WHEN** the user clicks Refund on `additional_worker` at rank 1
- **THEN** the displayed Legend Point total increases by 25 and the rank display becomes "0 / 2"

#### Scenario: Maxed upgrade hides Buy action
- **WHEN** an upgrade is at `maxRanks`
- **THEN** the Buy button is disabled or replaced with a "Maxed" label, and Refund remains available
