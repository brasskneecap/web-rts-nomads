## ADDED Requirements

### Requirement: Path ability grants are deferred; only the mechanism ships

The per-path ability-grant **mechanism** (the loader, `assignUnitPathAbilitiesLocked`, the `(path,rank)` grant lookup) SHALL remain present and behaviourally covered by tests, but the catalog SHALL NOT grant any real ability on promotion. No `paths/<path>/abilities/<rank>.json` grant files SHALL exist for the Acolyte line; every `(path,rank)` SHALL resolve to an empty grant. Acquisition of the dormant abilities is explicitly deferred: `greater_heal` is intended as a future perk-gated *replacement* of base `heal` (the gating perk is undesigned), and `arcane_bolt`'s acquisition is TBD. The dormant `greater_heal` / `arcane_bolt` `AbilityDef`s SHALL remain valid (load + validate + resolvable by id) so the engine and dormant-def tests keep working.

#### Scenario: No promotion grants any ability by default

- **WHEN** an Acolyte is promoted on the Cleric or Arch Mage path to any rank
- **THEN** `assignUnitPathAbilitiesLocked` appends nothing (no grant files exist) and the unit's `Abilities` is unchanged by the grant step

#### Scenario: Grant mechanism stays covered via a synthetic fixture

- **WHEN** a synthetic `(path,rank)` grant is injected at test time
- **THEN** `assignUnitPathAbilitiesLocked` appends the granted ids in catalog order, is idempotent (append-iff-absent across multi-rank catch-up and re-invocation), and is RNG-free — proving the mechanism without any authored catalog content

#### Scenario: Dormant ability defs remain valid

- **WHEN** the ability catalog loads
- **THEN** `greater_heal` and `arcane_bolt` load and validate (registered `Category`/`DamageType`) and are resolvable by id, even though nothing grants them

## REMOVED Requirements

### Requirement: Cleric and Arch Mage starter kits are authored

**Reason**: The Phase 2 abilities `greater_heal`/`arcane_bolt` and their unconditional promotion grants were placeholder content, not a deliberate design. The user has deferred the acquisition models (`greater_heal` = perk-gated replacement of `heal`, gating perk TBD; `arcane_bolt` acquisition TBD), so requiring authored kits asserts an unintended design. The grant *mechanism* is retained and is now covered by synthetic-fixture tests (see the ADDED requirement); only the requirement to ship authored kit *content* is removed.

**Migration**: No runtime migration. The two grant files (`paths/{cleric,arch_mage}/abilities/silver.json`) are deleted; `greater_heal`/`arcane_bolt` remain as dormant, valid, clearly-labelled defs for future acquisition work. Consumers that expected a promoted Cleric/Arch Mage to auto-hold these abilities must instead wait for the deferred acquisition design (tracked in the change's design.md and project memory). All other `per-path-ability-kits` requirements (loader, deterministic idempotent grant, granted-ability-in-snapshot, offensive `DamageAmount` on resolve) are unaffected.
