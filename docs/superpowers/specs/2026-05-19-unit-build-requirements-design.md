# Unit Build Requirements — Design

**Date:** 2026-05-19
**Status:** Approved (brainstorming)

## Goal

Allow unit definitions to declare prerequisite buildings that must exist
(fully built, owned by the player) before that unit can be trained.

Scope of this change: introduce the system end-to-end and wire up exactly
one requirement — **Archer requires a fully-built Blacksmith.** All other
units (Worker, Soldier, Acolyte) are unaffected. Adding the next
requirement after this lands is a one-line JSON edit + a unit test.

## Acceptance Criteria

1. With no Blacksmith owned, the Barracks action panel shows the Archer
   icon greyed out. Hovering the icon shows a tooltip: **"Requires:
   Blacksmith"**. Clicking it is a no-op.
2. Once the player owns a fully-built Blacksmith (any one of them), the
   Archer icon becomes clickable on the next snapshot tick and behaves
   normally.
3. If the Blacksmith is destroyed (or all of them are), the Archer icon
   greys back out on the next snapshot tick. Already-queued Archer
   productions complete normally; new train commands are silently
   rejected by the server.
4. A modded client that sends `TrainUnitCommandMessage{ UnitType: "archer" }`
   while the player has no Blacksmith does not produce an Archer and does
   not deduct resources.
5. A Blacksmith that is still **under construction** does not satisfy the
   requirement (matches the existing upgrade-purchase gate).

## Non-Goals

- Per-tier requirements (e.g. "needs tier-2 Blacksmith"). Out of scope.
- HP-based requirements (e.g. "needs an undamaged Blacksmith"). Out of
  scope.
- Building-build requirements (e.g. "Blacksmith requires Townhall").
  This spec is units-only.
- Cancelling already-queued production when the requirement lapses.
  Mid-production units complete normally; only new commands are gated.

## Data Model

### Server: `UnitDef.RequiresBuildings`

Add one optional field to `UnitDef` in
[`server/internal/game/unit_defs.go`](../../../server/internal/game/unit_defs.go):

```go
// RequiresBuildings is the list of building types the player must own
// fully built (Visible, not underConstruction) before this unit can be
// trained. Empty/omitted = no requirement. Multiple entries are ANDed.
// Validated at load time against the building catalog.
RequiresBuildings []string `json:"requiresBuildings,omitempty"`
```

### Validation at catalog load

Inside `loadUnitDefsByType` in `unit_defs.go`, after the other
validation panics, add:

```go
for _, b := range def.RequiresBuildings {
    if _, ok := getBuildingDef(b); !ok {
        panic(rel + `: requiresBuildings entry "` + b +
            `" is not a registered building type`)
    }
}
```

This mirrors how `projectile`, `combatProfile`, and `damageType` are
validated. A typo in `archer.json` becomes a startup panic, not a silent
"requirement that is never met."

### Catalog change

[`server/internal/game/catalog/units/human/archer/archer.json`](../../../server/internal/game/catalog/units/human/archer/archer.json)
gains:

```json
"requiresBuildings": ["blacksmith"]
```

No other unit JSON is changed.

### Wire protocol

`PlayerSnapshot` ([`server/pkg/protocol/messages.go`](../../../server/pkg/protocol/messages.go) line ~511)
gains one optional field:

```go
// LockedUnitTypes lists the unit types this player currently cannot
// train because their RequiresBuildings list is unsatisfied. Empty/
// omitted = no locks. The client uses this to grey out train actions.
LockedUnitTypes []string `json:"lockedUnitTypes,omitempty"`
```

Mirror in client protocol ([`client/src/game-portal/src/game/network/protocol.ts`](../../../client/src/game-portal/src/game/network/protocol.ts)):

```ts
lockedUnitTypes?: string[]
```

### Client UnitDef

[`client/src/game-portal/src/game/maps/unitDefs.ts`](../../../client/src/game-portal/src/game/maps/unitDefs.ts)
gains a parallel field on the typed `UnitDef`:

```ts
requiresBuildings?: string[]
```

So the value authored in `archer.json` round-trips into client code for
tooltip rendering.

## Server Logic

### New helpers in `state_production.go`

```go
// playerHasBuildingTypeLocked returns true if the player owns at least
// one Visible, fully-built (not underConstruction) building of the
// given type. Must be called under s.mu.
func (s *GameState) playerHasBuildingTypeLocked(playerID, buildingType string) bool {
    for i := range s.MapConfig.Buildings {
        b := &s.MapConfig.Buildings[i]
        if !b.Visible {
            continue
        }
        if b.BuildingType != buildingType {
            continue
        }
        if b.OwnerID == nil || *b.OwnerID != playerID {
            continue
        }
        if getMetadataBool(b.Metadata, "underConstruction") {
            continue
        }
        return true
    }
    return false
}

// playerMeetsUnitRequirementsLocked returns true if every building type
// in def.RequiresBuildings is satisfied for playerID. Empty list = true.
// Unknown unitType = false (defensive; should be unreachable because
// callers verify the def exists first). Must be called under s.mu.
func (s *GameState) playerMeetsUnitRequirementsLocked(playerID, unitType string) bool {
    def, ok := getUnitDef(unitType)
    if !ok {
        return false
    }
    for _, required := range def.RequiresBuildings {
        if !s.playerHasBuildingTypeLocked(playerID, required) {
            return false
        }
    }
    return true
}
```

`playerHasBlacksmithLocked` in `state_upgrades.go` stays exactly as it
is — it has a different predicate (matches by `"upgrade-purchase"`
capability, not by `BuildingType == "blacksmith"`). Different question,
different helper.

### `TrainUnit` gate

In [`server/internal/game/state_production.go`](../../../server/internal/game/state_production.go),
inside `TrainUnit`, add the requirement check **before** the
affordability check (cheaper to evaluate, and the order doesn't change
observable behavior — all failures are silent no-ops):

```go
if !containsString(building.SpawnUnitTypes, unitType) {
    return
}
if !s.playerMeetsUnitRequirementsLocked(playerID, unitType) {  // ← new
    return
}
if len(s.Productions[buildingID]) >= unitProductionMaxQueue {
    return
}
```

### Snapshot construction

A new helper in `state_production.go`:

```go
// lockedUnitTypesForPlayerLocked returns the set of unit types the
// player currently cannot train due to unmet RequiresBuildings. Iterates
// ListUnitDefs() once per player per snapshot — cheap, runs at human-
// readable cadence, not on the hot tick path. Must be called under s.mu.
func (s *GameState) lockedUnitTypesForPlayerLocked(playerID string) []string {
    var locked []string
    for _, def := range ListUnitDefs() {
        if len(def.RequiresBuildings) == 0 {
            continue
        }
        if !s.playerMeetsUnitRequirementsLocked(playerID, def.Type) {
            locked = append(locked, def.Type)
        }
    }
    return locked
}
```

This is called from the three places in `state.go` that already call
`playerUpgradeSnapshotsLocked` (lines ~1029, ~1340, ~1635), populating
`PlayerSnapshot.LockedUnitTypes`. Each call site already holds `s.mu`.

Performance note: snapshot construction runs at snapshot cadence
(roughly per-tick), not per-tick-simulation. The inner work is O(units ×
requirements × buildings-per-player) which is trivially small (≤4 unit
types with requirements, ≤2 requirements each, a few dozen buildings).

## Client Logic

### `getBuildingActions` in `GameState.ts`

In [`client/src/game-portal/src/game/core/GameState.ts`](../../../client/src/game-portal/src/game/core/GameState.ts)
(starting at line ~2618, the `unit-spawner` block), thread the
per-player `lockedUnitTypes` set through from the caller. The signature
gains one optional argument:

```ts
function getBuildingActions(
  building: BuildingTile,
  upgrades: PlayerUpgradeSnapshot[] = [],
  vaultState?: { vault: ...; vaultCapacity: number; vaultPanelOpen: boolean },
  townHallTier: number = 0,
  lockedUnitTypes: ReadonlySet<string> = new Set(),  // ← new
): ActionItem[]
```

Inside the `unit-spawner` block, when pushing each train action:

```ts
for (const unitType of building.spawnUnitTypes ?? []) {
  const def = UNIT_DEF_MAP.get(unitType)
  if (def) {
    const cost = ...
    const isLocked = lockedUnitTypes.has(unitType)
    const requires = def.requiresBuildings ?? []

    actions.push({
      id: `train-${unitType}`,
      label: def.trainLabel,
      iconDef: { kind: 'unit', type: unitType },
      cost,
      disabled: isLocked,
      tooltipTitle: isLocked ? def.trainLabel : undefined,
      tooltipBody: isLocked
        ? `Requires: ${requires.map(formatBuildingName).join(', ')}`
        : undefined,
    })
    hasTrainable = true
  }
}
```

`formatBuildingName` is a small helper near the existing
`formatSpawnUnitType` in `GameState.ts` — looks up the building def by
type and returns its `.label` (e.g. `"blacksmith"` → `"Blacksmith"`).
Falls back to the type string with the first letter capitalised if no
def is found.

### Caller plumbing

Every call site that already passes `upgrades` to `getBuildingActions`
needs to also pass the current player's `lockedUnitTypes` set, built
once per render from `localPlayerSnapshot.lockedUnitTypes ?? []`. There
is one call site to update in `GameState.ts` (already grep'd in the
spike; will be enumerated in the implementation plan).

The existing action-handler in `GameClient.ts` (line ~393:
`this.network.sendTrainUnitCommand(...)`) does not need to change — the
`disabled` flag on the action already prevents click dispatch through
the existing HUD machinery (the same path that suppresses clicks on
disabled upgrade buttons).

## Data Flow Per Tick

1. Server tick → snapshot construction. For each player,
   `lockedUnitTypesForPlayerLocked(playerID)` runs once. Result is
   attached to `PlayerSnapshot.LockedUnitTypes`.
2. Snapshot serialised → sent over WS.
3. Client receives snapshot → updates store with new `lockedUnitTypes`.
4. HUD re-renders → `getBuildingActions` reads the locked set →
   train-archer action gets `disabled: true` + tooltip when archer is
   in the set.
5. Player clicks the Barracks → sees greyed Archer button + tooltip
   "Requires: Blacksmith".

## Failure Modes & Edge Cases

| Scenario | Behavior |
|---|---|
| Player has no Blacksmith, clicks Archer | Button is disabled client-side. Even if click somehow dispatches, server's `TrainUnit` silently no-ops. No resources spent, no production queued. |
| Blacksmith mid-construction | Locked. Matches upgrade-purchase behavior. |
| Blacksmith just finished this tick | Unlocked on the very next snapshot. |
| Blacksmith destroyed mid-Archer-queue | Already-queued Archer productions complete. New train commands rejected. Matches how the upgrade gate behaves (existing upgrades don't refund if you lose your Blacksmith). |
| Player owns multiple Blacksmiths, loses one | Still satisfied. Any one fully-built Blacksmith unlocks. |
| Train command races a Blacksmith destruction | Resolved under `s.mu`. Whichever side of the lock the destruction lands determines the outcome. Deterministic. |
| Modded client bypasses disabled flag | Server's `TrainUnit` is authoritative — rejects the command. |
| `requiresBuildings: ["blacksmth"]` (typo) | Catalog load panics at startup. |

## Testing

### Go (server)

New test file or addition to existing `state_production_test.go`:

- `TestTrainUnit_ArcherRequiresBlacksmith_NoBuilding`: seed player + Barracks,
  no Blacksmith. Call `TrainUnit("archer")`. Assert production queue is
  empty and player resources are unchanged.
- `TestTrainUnit_ArcherRequiresBlacksmith_UnderConstruction`: seed
  player + Barracks + Blacksmith with `metadata.underConstruction=true`.
  `TrainUnit("archer")` is a no-op.
- `TestTrainUnit_ArcherRequiresBlacksmith_Built`: seed player + Barracks
  + fully-built Blacksmith. `TrainUnit("archer")` queues production and
  deducts cost.
- `TestTrainUnit_ArcherRequiresBlacksmith_DestroyedMidQueue`: queue an
  Archer with a Blacksmith present, destroy the Blacksmith
  (set `Visible=false` or remove), tick to completion. The queued Archer
  spawns; a subsequent `TrainUnit("archer")` call is a no-op.
- `TestTrainUnit_SoldierUnaffected`: with no Blacksmith, `TrainUnit("soldier")`
  succeeds (regression guard — only Archer is gated).
- `TestLockedUnitTypes_SnapshotContents`: build a `lockedUnitTypesForPlayerLocked`
  call against a seeded state, assert the returned slice contains
  `"archer"` when no Blacksmith is present and is empty when one is.

### Manual frontend

- Start a new game, build a Barracks. Confirm Archer is greyed with the
  expected tooltip.
- Build a Blacksmith. As soon as it finishes, the Archer button
  ungreys.
- Queue several Archers. Destroy the Blacksmith. Confirm queued ones
  finish, the button greys, and clicking it does nothing.

## File Touch List (summary)

- **Server**
  - `server/internal/game/unit_defs.go` — add `RequiresBuildings` field
    and load-time validation.
  - `server/internal/game/catalog/units/human/archer/archer.json` — set
    `"requiresBuildings": ["blacksmith"]`.
  - `server/internal/game/state_production.go` — add
    `playerHasBuildingTypeLocked`, `playerMeetsUnitRequirementsLocked`,
    `lockedUnitTypesForPlayerLocked`; gate inside `TrainUnit`.
  - `server/internal/game/state.go` — populate
    `PlayerSnapshot.LockedUnitTypes` at the three existing player-
    snapshot call sites.
  - `server/pkg/protocol/messages.go` — add `LockedUnitTypes` to
    `PlayerSnapshot`.
  - `server/internal/game/state_production_test.go` (or new file) —
    add tests above.

- **Client**
  - `client/src/game-portal/src/game/network/protocol.ts` — add
    `lockedUnitTypes?: string[]` to the player snapshot type.
  - `client/src/game-portal/src/game/maps/unitDefs.ts` — add
    `requiresBuildings?: string[]` to the typed `UnitDef`.
  - `client/src/game-portal/src/game/core/GameState.ts` — thread
    `lockedUnitTypes` into `getBuildingActions`, add disabled + tooltip,
    add `formatBuildingName` helper.

- **Test data / snapshot fixtures**
  - `server/internal/ws/testdata/sp_baseline_outbound.json` — likely
    needs a regenerate to include the new `lockedUnitTypes` field
    (omitempty means it only appears when non-empty, so the fixture may
    not need to change at all; verify during implementation).
