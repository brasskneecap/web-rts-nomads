# Unit Build Requirements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow unit definitions to declare prerequisite buildings, then wire Archer→Blacksmith end-to-end: the Archer train action is greyed out with a "Requires: Blacksmith" tooltip until the player owns a fully-built Blacksmith.

**Architecture:** New `RequiresBuildings []string` field on `UnitDef` (catalog-driven). Server enforces in `TrainUnit` and publishes a per-player `LockedUnitTypes` set in each `PlayerSnapshot`. Client reads that set in `getBuildingActions` to set `disabled: true` + a tooltip on the affected train actions.

**Tech Stack:** Go 1.x (server, embedded JSON catalog), TypeScript / Vue 3 (client), Go `testing` for server tests.

**Spec:** [`docs/superpowers/specs/2026-05-19-unit-build-requirements-design.md`](../specs/2026-05-19-unit-build-requirements-design.md)

---

## File Touch List

**Server (Go):**
- Modify: `server/internal/game/unit_defs.go` — add `RequiresBuildings` field + load-time validation
- Modify: `server/internal/game/catalog/units/human/archer/archer.json` — set `"requiresBuildings": ["blacksmith"]`
- Modify: `server/internal/game/state_production.go` — add 3 helpers + gate inside `TrainUnit`
- Modify: `server/internal/game/state.go` — populate `PlayerSnapshot.LockedUnitTypes` at 3 call sites
- Modify: `server/pkg/protocol/messages.go` — add `LockedUnitTypes` to `PlayerSnapshot`
- Create: `server/internal/game/unit_build_requirements_test.go` — Go tests

**Client (TypeScript / Vue):**
- Modify: `client/src/game-portal/src/game/network/protocol.ts` — add `lockedUnitTypes?: string[]` to `PlayerSnapshot`
- Modify: `client/src/game-portal/src/game/maps/unitDefs.ts` — add `requiresBuildings?: string[]` to `UnitDef`
- Modify: `client/src/game-portal/src/game/core/GameState.ts` — thread `lockedUnitTypes` into `getBuildingActions`, store on `GameState`, add `formatBuildingName` helper

**Snapshot fixtures (may regen):**
- `server/internal/ws/testdata/sp_baseline_outbound.json` — check after server changes; with `omitempty` it should not need updating unless the baseline fixture has a player with a locked unit type.

---

## Task 1: Add `RequiresBuildings` to `UnitDef` with load-time validation

**Files:**
- Modify: `server/internal/game/unit_defs.go`

- [ ] **Step 1: Add the field to `UnitDef`**

In `server/internal/game/unit_defs.go`, locate the `UnitDef` struct (starts around line 28). After the `TargetableTypes` field (last existing field, currently around line 124), insert:

```go
	// RequiresBuildings is the list of building types the player must own
	// fully built (Visible, not underConstruction) before this unit can
	// be trained. Empty/omitted = no requirement. Multiple entries are
	// ANDed. Validated at load time against the building catalog.
	RequiresBuildings []string `json:"requiresBuildings,omitempty"`
```

- [ ] **Step 2: Add load-time validation in `loadUnitDefsByType`**

In the same file, inside `loadUnitDefsByType` (starts around line 136), find the existing validation block right after `def.MaxMana < 0` check (around line 207). Add this new validation block just before the `if _, dup := result[def.Type]; dup {` line:

```go
			for _, b := range def.RequiresBuildings {
				if _, ok := getBuildingDef(b); !ok {
					panic(rel + `: requiresBuildings entry "` + b +
						`" is not a registered building type`)
				}
			}
```

- [ ] **Step 3: Build the server to verify no compile errors and catalog still loads**

Run:

```bash
cd server && go build ./...
```

Expected: builds cleanly. (Catalog load runs in `init`, but is only exercised when the test or main binary starts. Step 5 will exercise it.)

- [ ] **Step 4: Write a load-time validation test**

Create a new file `server/internal/game/unit_build_requirements_test.go` with this content:

```go
package game

import (
	"testing"
)

// TestRequiresBuildings_FieldExistsOnUnitDef verifies the new field is
// readable on a loaded UnitDef. A missing field means later tasks can't
// compile.
func TestRequiresBuildings_FieldExistsOnUnitDef(t *testing.T) {
	def, ok := getUnitDef("archer")
	if !ok {
		t.Fatal("archer unit def not registered")
	}
	// At this point in the plan the archer.json change has not landed
	// yet, so the field exists but is empty. Reading it confirms the
	// type compiles.
	_ = def.RequiresBuildings
}
```

- [ ] **Step 5: Run the test**

Run:

```bash
cd server && go test ./internal/game/ -run TestRequiresBuildings_FieldExistsOnUnitDef -v
```

Expected: PASS (the test only reads the field).

- [ ] **Step 6: Commit**

```bash
git add server/internal/game/unit_defs.go server/internal/game/unit_build_requirements_test.go
git commit -m "feat: add RequiresBuildings field to UnitDef with catalog validation"
```

---

## Task 2: Wire Archer → Blacksmith requirement in catalog

**Files:**
- Modify: `server/internal/game/catalog/units/human/archer/archer.json`

- [ ] **Step 1: Update archer.json**

In `server/internal/game/catalog/units/human/archer/archer.json`, add the `requiresBuildings` field. The current file ends with `bounds` as the last field. Add `requiresBuildings` between `meatCost` and `spawnSeconds` (alphabetical-ish, matches the surrounding ordering style — but order is not significant for JSON parsing):

Before:

```json
  "meatCost": 3,
  "spawnSeconds": 10,
```

After:

```json
  "meatCost": 3,
  "requiresBuildings": ["blacksmith"],
  "spawnSeconds": 10,
```

- [ ] **Step 2: Add a test asserting the catalog content**

Append to `server/internal/game/unit_build_requirements_test.go`:

```go
// TestArcher_RequiresBlacksmith verifies the archer catalog declares the
// blacksmith requirement. Regression guard against an accidental JSON
// edit that drops the field.
func TestArcher_RequiresBlacksmith(t *testing.T) {
	def, ok := getUnitDef("archer")
	if !ok {
		t.Fatal("archer unit def not registered")
	}
	if len(def.RequiresBuildings) != 1 || def.RequiresBuildings[0] != "blacksmith" {
		t.Errorf("archer.RequiresBuildings = %v; want [\"blacksmith\"]", def.RequiresBuildings)
	}
}

// TestSoldier_NoRequirements verifies the soldier (and by implication
// other unrequired units) is not gated. Regression guard against
// accidentally adding requirements to other units.
func TestSoldier_NoRequirements(t *testing.T) {
	def, ok := getUnitDef("soldier")
	if !ok {
		t.Fatal("soldier unit def not registered")
	}
	if len(def.RequiresBuildings) != 0 {
		t.Errorf("soldier.RequiresBuildings = %v; want []", def.RequiresBuildings)
	}
}
```

- [ ] **Step 3: Run the tests**

Run:

```bash
cd server && go test ./internal/game/ -run "TestArcher_RequiresBlacksmith|TestSoldier_NoRequirements" -v
```

Expected: both PASS. If either fails, re-check archer.json (the field must be a JSON array, not a string).

- [ ] **Step 4: Commit**

```bash
git add server/internal/game/catalog/units/human/archer/archer.json server/internal/game/unit_build_requirements_test.go
git commit -m "feat: archer requires fully-built blacksmith"
```

---

## Task 3: Add `playerHasBuildingTypeLocked` and `playerMeetsUnitRequirementsLocked` helpers

**Files:**
- Modify: `server/internal/game/state_production.go`
- Modify: `server/internal/game/unit_build_requirements_test.go`

- [ ] **Step 1: Write the failing test for `playerHasBuildingTypeLocked`**

Append to `server/internal/game/unit_build_requirements_test.go`:

```go
import (
	"webrts/server/pkg/protocol"
)

// newRequirementsTestState builds a GameState with player "p1" already
// ensured and no buildings. Tests add buildings as needed.
func newRequirementsTestState(t *testing.T) (*GameState, string) {
	t.Helper()
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	const playerID = "p1"
	s.EnsurePlayer(playerID)
	return s, playerID
}

// addBuildingToState injects a building owned by ownerID into the state
// and re-indexes buildingsByID. Caller must hold s.mu.
func addBuildingToState(s *GameState, id, buildingType, ownerID string, underConstruction bool, visible bool) {
	owner := ownerID
	meta := map[string]interface{}{}
	if underConstruction {
		meta["underConstruction"] = true
	}
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
		ID:           id,
		BuildingType: buildingType,
		Width:        2,
		Height:       2,
		Visible:      visible,
		OwnerID:      &owner,
		Capabilities: []string{},
		Metadata:     meta,
	})
	if s.buildingsByID == nil {
		s.buildingsByID = map[string]*protocol.BuildingTile{}
	}
	last := &s.MapConfig.Buildings[len(s.MapConfig.Buildings)-1]
	s.buildingsByID[last.ID] = last
}

// TestPlayerHasBuildingTypeLocked covers the four corners: present and
// fully built (true), under construction (false), invisible (false),
// wrong type (false).
func TestPlayerHasBuildingTypeLocked(t *testing.T) {
	s, p1 := newRequirementsTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.playerHasBuildingTypeLocked(p1, "blacksmith") {
		t.Error("no blacksmith yet; want false")
	}

	addBuildingToState(s, "bs-built", "blacksmith", p1, false, true)
	if !s.playerHasBuildingTypeLocked(p1, "blacksmith") {
		t.Error("fully-built blacksmith present; want true")
	}

	// Mid-construction does NOT count.
	addBuildingToState(s, "bs-uc", "blacksmith", "p2", true, true)
	if s.playerHasBuildingTypeLocked("p2", "blacksmith") {
		t.Error("only mid-construction blacksmith; want false")
	}

	// Invisible does NOT count.
	addBuildingToState(s, "bs-inv", "blacksmith", "p3", false, false)
	if s.playerHasBuildingTypeLocked("p3", "blacksmith") {
		t.Error("only invisible blacksmith; want false")
	}

	// Wrong type does NOT match.
	if s.playerHasBuildingTypeLocked(p1, "barracks") {
		t.Error("no barracks present; want false")
	}
}
```

- [ ] **Step 2: Run the test — must fail (helper not defined)**

Run:

```bash
cd server && go test ./internal/game/ -run TestPlayerHasBuildingTypeLocked -v
```

Expected: FAIL with `undefined: ... .playerHasBuildingTypeLocked`.

- [ ] **Step 3: Implement `playerHasBuildingTypeLocked` and `playerMeetsUnitRequirementsLocked`**

In `server/internal/game/state_production.go`, append at the end of the file (after `joinProductionUnitTypes`):

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

- [ ] **Step 4: Run the test — must pass**

Run:

```bash
cd server && go test ./internal/game/ -run TestPlayerHasBuildingTypeLocked -v
```

Expected: PASS.

- [ ] **Step 5: Add a test for `playerMeetsUnitRequirementsLocked`**

Append to `server/internal/game/unit_build_requirements_test.go`:

```go
// TestPlayerMeetsUnitRequirementsLocked verifies the AND semantics of
// RequiresBuildings and the "unknown unit type" defensive branch.
func TestPlayerMeetsUnitRequirementsLocked(t *testing.T) {
	s, p1 := newRequirementsTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Soldier has no requirements → always true.
	if !s.playerMeetsUnitRequirementsLocked(p1, "soldier") {
		t.Error("soldier has no requirements; want true")
	}

	// Archer requires blacksmith. Without one → false.
	if s.playerMeetsUnitRequirementsLocked(p1, "archer") {
		t.Error("no blacksmith; archer requirements should not be met")
	}

	// Mid-construction blacksmith → still false.
	addBuildingToState(s, "bs-uc", "blacksmith", p1, true, true)
	if s.playerMeetsUnitRequirementsLocked(p1, "archer") {
		t.Error("mid-construction blacksmith; archer requirements should not be met")
	}

	// Fully-built blacksmith → true.
	addBuildingToState(s, "bs-built", "blacksmith", p1, false, true)
	if !s.playerMeetsUnitRequirementsLocked(p1, "archer") {
		t.Error("fully-built blacksmith; archer requirements should be met")
	}

	// Unknown unit type → false (defensive).
	if s.playerMeetsUnitRequirementsLocked(p1, "no_such_unit") {
		t.Error("unknown unit type; want false")
	}
}
```

- [ ] **Step 6: Run the new test**

Run:

```bash
cd server && go test ./internal/game/ -run TestPlayerMeetsUnitRequirementsLocked -v
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add server/internal/game/state_production.go server/internal/game/unit_build_requirements_test.go
git commit -m "feat: server helpers to check unit build requirements"
```

---

## Task 4: Gate `TrainUnit` on requirements

**Files:**
- Modify: `server/internal/game/state_production.go`
- Modify: `server/internal/game/unit_build_requirements_test.go`

- [ ] **Step 1: Write the failing test**

Append to `server/internal/game/unit_build_requirements_test.go`:

```go
// trainAndAssertNoOp calls TrainUnit and asserts no production was
// queued and no resources were deducted. preGold and preWood capture
// the player's resources before the call. Caller must NOT hold s.mu.
func trainAndAssertNoOp(t *testing.T, s *GameState, playerID, buildingID, unitType string) {
	t.Helper()
	s.mu.RLock()
	player := s.Players[playerID]
	preGold := player.Resources["gold"]
	preWood := player.Resources["wood"]
	preQueueLen := len(s.Productions[buildingID])
	s.mu.RUnlock()

	s.TrainUnit(playerID, buildingID, unitType)

	s.mu.RLock()
	defer s.mu.RUnlock()
	if got := len(s.Productions[buildingID]); got != preQueueLen {
		t.Errorf("queue length after TrainUnit(%q) = %d; want %d (no-op expected)", unitType, got, preQueueLen)
	}
	if player.Resources["gold"] != preGold {
		t.Errorf("gold after TrainUnit(%q) = %d; want %d (no-op expected)", unitType, player.Resources["gold"], preGold)
	}
	if player.Resources["wood"] != preWood {
		t.Errorf("wood after TrainUnit(%q) = %d; want %d (no-op expected)", unitType, player.Resources["wood"], preWood)
	}
}

// addBarracks injects a barracks owned by playerID and returns its ID.
// Caller must hold s.mu.
func addBarracks(s *GameState, playerID string) string {
	bid := "barracks-1"
	owner := playerID
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
		ID:             bid,
		BuildingType:   "barracks",
		Width:          3,
		Height:         3,
		Visible:        true,
		OwnerID:        &owner,
		Capabilities:   []string{"unit-spawner"},
		SpawnUnitTypes: []string{"soldier", "archer"},
		Metadata:       map[string]interface{}{},
	})
	if s.buildingsByID == nil {
		s.buildingsByID = map[string]*protocol.BuildingTile{}
	}
	last := &s.MapConfig.Buildings[len(s.MapConfig.Buildings)-1]
	s.buildingsByID[last.ID] = last
	return bid
}

// TestTrainUnit_ArcherRequiresBlacksmith_NoBuilding: with a barracks but
// no blacksmith, TrainUnit("archer") is a no-op (no queue, no cost).
func TestTrainUnit_ArcherRequiresBlacksmith_NoBuilding(t *testing.T) {
	s, p1 := newRequirementsTestState(t)
	s.mu.Lock()
	s.Players[p1].Resources = map[string]int{"gold": 1000, "wood": 1000}
	bid := addBarracks(s, p1)
	s.mu.Unlock()

	trainAndAssertNoOp(t, s, p1, bid, "archer")
}

// TestTrainUnit_ArcherRequiresBlacksmith_UnderConstruction: with a
// mid-construction blacksmith, TrainUnit("archer") is still a no-op.
func TestTrainUnit_ArcherRequiresBlacksmith_UnderConstruction(t *testing.T) {
	s, p1 := newRequirementsTestState(t)
	s.mu.Lock()
	s.Players[p1].Resources = map[string]int{"gold": 1000, "wood": 1000}
	bid := addBarracks(s, p1)
	addBuildingToState(s, "bs-uc", "blacksmith", p1, true, true)
	s.mu.Unlock()

	trainAndAssertNoOp(t, s, p1, bid, "archer")
}

// TestTrainUnit_ArcherRequiresBlacksmith_Built: with a fully-built
// blacksmith, TrainUnit("archer") queues production and deducts cost.
func TestTrainUnit_ArcherRequiresBlacksmith_Built(t *testing.T) {
	s, p1 := newRequirementsTestState(t)
	s.mu.Lock()
	s.Players[p1].Resources = map[string]int{"gold": 1000, "wood": 1000}
	bid := addBarracks(s, p1)
	addBuildingToState(s, "bs-built", "blacksmith", p1, false, true)
	preGold := s.Players[p1].Resources["gold"]
	preWood := s.Players[p1].Resources["wood"]
	archerDef, _ := getUnitDef("archer")
	s.mu.Unlock()

	s.TrainUnit(p1, bid, "archer")

	s.mu.RLock()
	defer s.mu.RUnlock()
	if got := len(s.Productions[bid]); got != 1 {
		t.Fatalf("expected 1 production queued; got %d", got)
	}
	if s.Productions[bid][0].UnitType != "archer" {
		t.Errorf("queued unit type = %q; want %q", s.Productions[bid][0].UnitType, "archer")
	}
	wantGold := preGold - archerDef.ResourceCost["gold"]
	wantWood := preWood - archerDef.ResourceCost["wood"]
	if s.Players[p1].Resources["gold"] != wantGold {
		t.Errorf("gold = %d; want %d", s.Players[p1].Resources["gold"], wantGold)
	}
	if s.Players[p1].Resources["wood"] != wantWood {
		t.Errorf("wood = %d; want %d", s.Players[p1].Resources["wood"], wantWood)
	}
}

// TestTrainUnit_SoldierUnaffected: with no blacksmith, soldier is still
// trainable. Regression guard against accidentally gating unrequired
// units.
func TestTrainUnit_SoldierUnaffected(t *testing.T) {
	s, p1 := newRequirementsTestState(t)
	s.mu.Lock()
	s.Players[p1].Resources = map[string]int{"gold": 1000, "wood": 1000}
	bid := addBarracks(s, p1)
	s.mu.Unlock()

	s.TrainUnit(p1, bid, "soldier")

	s.mu.RLock()
	defer s.mu.RUnlock()
	if got := len(s.Productions[bid]); got != 1 {
		t.Fatalf("soldier should queue without a blacksmith; got %d productions", got)
	}
}
```

- [ ] **Step 2: Run the tests — must fail on the "NoBuilding" / "UnderConstruction" cases**

Run:

```bash
cd server && go test ./internal/game/ -run "TestTrainUnit_Archer|TestTrainUnit_Soldier" -v
```

Expected: `TestTrainUnit_ArcherRequiresBlacksmith_NoBuilding` and `_UnderConstruction` FAIL (TrainUnit currently allows archer regardless of buildings — the queue will be 1, not 0, and gold will be deducted). `_Built` and `Soldier` should PASS.

- [ ] **Step 3: Add the gate in `TrainUnit`**

In `server/internal/game/state_production.go`, in the `TrainUnit` function, locate the check:

```go
	if !containsString(building.SpawnUnitTypes, unitType) {
		return
	}
```

Immediately after that block, **before** the `if len(s.Productions[buildingID]) >= unitProductionMaxQueue {` line, insert:

```go
	if !s.playerMeetsUnitRequirementsLocked(playerID, unitType) {
		return
	}
```

- [ ] **Step 4: Run the tests — all four must pass**

Run:

```bash
cd server && go test ./internal/game/ -run "TestTrainUnit_Archer|TestTrainUnit_Soldier" -v
```

Expected: all four PASS.

- [ ] **Step 5: Run the broader game test suite to catch regressions**

Run:

```bash
cd server && go test ./internal/game/ -count=1
```

Expected: PASS. If anything fails, investigate — it likely indicates an existing test was relying on archer-trainable behavior we just gated.

- [ ] **Step 6: Commit**

```bash
git add server/internal/game/state_production.go server/internal/game/unit_build_requirements_test.go
git commit -m "feat: gate TrainUnit on RequiresBuildings"
```

---

## Task 5: Add `LockedUnitTypes` to `PlayerSnapshot` and populate it in snapshots

**Files:**
- Modify: `server/pkg/protocol/messages.go`
- Modify: `server/internal/game/state_production.go`
- Modify: `server/internal/game/state.go`
- Modify: `server/internal/game/unit_build_requirements_test.go`

- [ ] **Step 1: Add `LockedUnitTypes` to `PlayerSnapshot`**

In `server/pkg/protocol/messages.go`, locate the `PlayerSnapshot` struct (line ~511). Add this field after `VaultCapacity` (line ~522) and before `ActiveBuffs`:

```go
	// LockedUnitTypes lists the unit types this player currently cannot
	// train because their RequiresBuildings list is unsatisfied. Empty
	// or omitted = no locks. The client uses this to grey out train
	// actions in the building action panel.
	LockedUnitTypes []string `json:"lockedUnitTypes,omitempty"`
```

- [ ] **Step 2: Write the failing test for `lockedUnitTypesForPlayerLocked`**

Append to `server/internal/game/unit_build_requirements_test.go`:

```go
// TestLockedUnitTypesForPlayerLocked verifies the helper returns the
// set of unit types whose RequiresBuildings list is unsatisfied.
func TestLockedUnitTypesForPlayerLocked(t *testing.T) {
	s, p1 := newRequirementsTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// No buildings → archer is locked.
	locked := s.lockedUnitTypesForPlayerLocked(p1)
	if !containsString(locked, "archer") {
		t.Errorf("with no blacksmith, expected archer in locked set; got %v", locked)
	}
	// Soldier has no requirements and must never appear.
	if containsString(locked, "soldier") {
		t.Errorf("soldier has no requirements; should not appear in locked set; got %v", locked)
	}

	// Fully-built blacksmith → archer unlocks.
	addBuildingToState(s, "bs-built", "blacksmith", p1, false, true)
	locked = s.lockedUnitTypesForPlayerLocked(p1)
	if containsString(locked, "archer") {
		t.Errorf("with fully-built blacksmith, archer should not be locked; got %v", locked)
	}
}
```

- [ ] **Step 3: Run the test — must fail (helper not defined)**

Run:

```bash
cd server && go test ./internal/game/ -run TestLockedUnitTypesForPlayerLocked -v
```

Expected: FAIL with `undefined: ... .lockedUnitTypesForPlayerLocked`.

- [ ] **Step 4: Implement `lockedUnitTypesForPlayerLocked`**

In `server/internal/game/state_production.go`, append at the end of the file:

```go
// lockedUnitTypesForPlayerLocked returns the set of unit types the
// player currently cannot train due to unmet RequiresBuildings.
// Iterates ListUnitDefs() once per player per snapshot — runs at
// snapshot cadence, not on the simulation hot path. Returns nil (not an
// empty slice) when nothing is locked so the protocol's omitempty drops
// the field from the wire. Must be called under s.mu.
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

- [ ] **Step 5: Run the test — must pass**

Run:

```bash
cd server && go test ./internal/game/ -run TestLockedUnitTypesForPlayerLocked -v
```

Expected: PASS.

- [ ] **Step 6: Populate `LockedUnitTypes` at the three snapshot call sites in `state.go`**

In `server/internal/game/state.go`, there are three sites that build a `PlayerSnapshot` (lines ~1020, ~1330, ~1625 — all have the same shape). For each, add `LockedUnitTypes: s.lockedUnitTypesForPlayerLocked(player.ID),` inside the struct literal. The diff at each site looks like:

Before:

```go
		playerSnap := protocol.PlayerSnapshot{
			PlayerID:      player.ID,
			Color:         player.Color,
			TeamID:        player.TeamID,
			Resources:     s.getPlayerResourceStocksLocked(player),
			Upgrades:      s.playerUpgradeSnapshotsLocked(player.ID),
			TownHallTier:  s.townhallTierForPlayerLocked(player.ID),
			Vault:         s.playerVaultSnapshotsLocked(player.ID),
			VaultCapacity: s.vaultCapacityForPlayerLocked(player.ID),
		}
```

After:

```go
		playerSnap := protocol.PlayerSnapshot{
			PlayerID:        player.ID,
			Color:           player.Color,
			TeamID:          player.TeamID,
			Resources:       s.getPlayerResourceStocksLocked(player),
			Upgrades:        s.playerUpgradeSnapshotsLocked(player.ID),
			TownHallTier:    s.townhallTierForPlayerLocked(player.ID),
			Vault:           s.playerVaultSnapshotsLocked(player.ID),
			VaultCapacity:   s.vaultCapacityForPlayerLocked(player.ID),
			LockedUnitTypes: s.lockedUnitTypesForPlayerLocked(player.ID),
		}
```

Apply this change at all three locations (around lines 1020, 1330, 1625 — use Grep on the `Upgrades:      s.playerUpgradeSnapshotsLocked` literal to find them all).

- [ ] **Step 7: Build and run full server tests**

Run:

```bash
cd server && go build ./... && go test ./internal/game/ -count=1
```

Expected: builds + tests PASS. If a snapshot fixture test fails (`server/internal/ws/...`), see Step 8.

- [ ] **Step 8: Refresh `sp_baseline_outbound.json` if and only if the fixture test fails**

There is a baseline snapshot fixture at `server/internal/ws/testdata/sp_baseline_outbound.json`. With `omitempty`, the new field only appears when the baseline player has a locked unit type. Check whether the baseline test fails:

```bash
cd server && go test ./internal/ws/ -v
```

If the snapshot fixture test fails because the diff now includes `lockedUnitTypes: ["archer"]`, locate the fixture's update command in the failing test output (most fixture tests print a `go test -run TestX -update` hint). Run that update command, then re-run the test to confirm PASS. If the test passes without update, leave the fixture alone.

- [ ] **Step 9: Commit**

```bash
git add server/pkg/protocol/messages.go server/internal/game/state_production.go server/internal/game/state.go server/internal/game/unit_build_requirements_test.go
# Stage the fixture only if you updated it in step 8.
git status  # verify
git commit -m "feat: publish LockedUnitTypes in PlayerSnapshot"
```

If you also updated `sp_baseline_outbound.json`, stage and amend or include in the same commit:

```bash
git add server/internal/ws/testdata/sp_baseline_outbound.json
git commit --amend --no-edit
```

---

## Task 6: Mirror `lockedUnitTypes` in the client protocol type

**Files:**
- Modify: `client/src/game-portal/src/game/network/protocol.ts`

- [ ] **Step 1: Add the field to `PlayerSnapshot` (TypeScript)**

In `client/src/game-portal/src/game/network/protocol.ts`, locate the `PlayerSnapshot` type (line ~360). Add the new field after `vaultCapacity?`:

Before:

```ts
export type PlayerSnapshot = {
  playerId: string
  color: string
  teamId: number
  resources: ResourceStockSnapshot[]
  upgrades?: PlayerUpgradeSnapshot[]
  townHallTier?: number
  vault?: VaultItemSnapshot[]
  vaultCapacity?: number
}
```

After:

```ts
export type PlayerSnapshot = {
  playerId: string
  color: string
  teamId: number
  resources: ResourceStockSnapshot[]
  upgrades?: PlayerUpgradeSnapshot[]
  townHallTier?: number
  vault?: VaultItemSnapshot[]
  vaultCapacity?: number
  /** Unit types this player cannot train because their server-side
   *  RequiresBuildings list is unsatisfied. Absent/empty = no locks. */
  lockedUnitTypes?: string[]
}
```

- [ ] **Step 2: Compile-check the client**

There's no dedicated `typecheck` script in `package.json`. The build script runs `vue-tsc -b && vite build`, so type errors fail the build. Use:

```bash
cd client/src/game-portal && npx vue-tsc -b
```

Expected: exits 0 with no output (or only previously-existing warnings unrelated to this change).

- [ ] **Step 3: Commit**

```bash
git add client/src/game-portal/src/game/network/protocol.ts
git commit -m "feat(client): add lockedUnitTypes to PlayerSnapshot type"
```

---

## Task 7: Mirror `requiresBuildings` on the client `UnitDef`

**Files:**
- Modify: `client/src/game-portal/src/game/maps/unitDefs.ts`

- [ ] **Step 1: Add the field to `UnitDef` (TypeScript)**

In `client/src/game-portal/src/game/maps/unitDefs.ts`, locate the `UnitDef` type (line ~45). Add `requiresBuildings?: string[]` after `targetableTypes?`:

Before:

```ts
  /** Target classes this unit's attacks can hit. When absent the server derives a default at spawn (projectile attacks → ground+flyer, otherwise ground only). */
  targetableTypes?: UnitTargetClass[]
}
```

After:

```ts
  /** Target classes this unit's attacks can hit. When absent the server derives a default at spawn (projectile attacks → ground+flyer, otherwise ground only). */
  targetableTypes?: UnitTargetClass[]
  /** Building types the player must own fully-built before this unit
   *  can be trained. Server is authoritative; client uses this only to
   *  render the requirement tooltip on locked train actions. */
  requiresBuildings?: string[]
}
```

- [ ] **Step 2: Compile-check the client**

```bash
cd client/src/game-portal && npx vue-tsc -b
```

Expected: exits 0.

- [ ] **Step 3: Commit**

```bash
git add client/src/game-portal/src/game/maps/unitDefs.ts
git commit -m "feat(client): add requiresBuildings to UnitDef type"
```

---

## Task 8: Capture `lockedUnitTypes` on the client `GameState` and thread it into `getBuildingActions`

**Files:**
- Modify: `client/src/game-portal/src/game/core/GameState.ts`

- [ ] **Step 1: Add the storage field on `GameState`**

In `client/src/game-portal/src/game/core/GameState.ts`, locate the `playerUpgrades` field declaration (around line 471):

```ts
  // Permanent per-player upgrade state. Populated from the local player's
  // PlayerSnapshot every tick. Empty until the server sends upgrade data.
  playerUpgrades: PlayerUpgradeSnapshot[] = []
```

Immediately after it, add:

```ts
  // Unit types the local player cannot currently train (RequiresBuildings
  // unsatisfied). Populated from the local player's PlayerSnapshot every
  // tick. Empty until the server says otherwise.
  lockedUnitTypes: string[] = []
```

- [ ] **Step 2: Populate it in the snapshot reader**

In the same file, locate the snapshot-reading block that already pulls `localPlayer.upgrades` (around line 2195):

```ts
    if (localPlayer.upgrades !== undefined) {
      this.playerUpgrades = localPlayer.upgrades
    }
```

Add immediately after that:

```ts
    if (localPlayer.lockedUnitTypes !== undefined) {
      this.lockedUnitTypes = localPlayer.lockedUnitTypes
    } else {
      this.lockedUnitTypes = []
    }
```

(The `else` clause is necessary: the server omits the field via `omitempty` when the locked set is empty, and we must clear stale state, otherwise the action stays greyed after the Blacksmith finishes.)

- [ ] **Step 3: Add a `formatBuildingName` helper**

In the same file, search for `formatSpawnUnitType` — it's the existing label-formatting helper near `getBuildingActions`. Add a sibling helper directly above or below it. The exact location: the function lives near line 2832 based on earlier grep. Place this helper just above `getBuildingActions`:

```ts
function formatBuildingName(buildingType: string): string {
  const def = BUILDING_DEF_MAP.get(buildingType)
  if (def?.label) return def.label
  // Fallback: capitalise the type string.
  if (!buildingType) return ''
  return buildingType.charAt(0).toUpperCase() + buildingType.slice(1)
}
```

If `BUILDING_DEF_MAP` is not already imported in this file, search the top of the file for an existing `BUILDING_DEF_MAP` import — based on the grep earlier (`build-` action handling at line 385 in `GameClient.ts` uses `BUILDING_DEF_MAP`), it likely lives in `client/src/game-portal/src/game/maps/buildingDefs.ts`. If `GameState.ts` does not already import it, add to the existing import block from that module (look for any line `import { ... } from '../maps/buildingDefs'` or similar) or add a fresh `import { BUILDING_DEF_MAP } from '../maps/buildingDefs'` near the other `../maps/...` imports.

- [ ] **Step 4: Update the `getBuildingActions` signature and unit-spawner block**

In the same file, locate `getBuildingActions` (line ~2573). Update the signature to accept `lockedUnitTypes`:

Before:

```ts
function getBuildingActions(
  building: BuildingTile,
  upgrades: PlayerUpgradeSnapshot[] = [],
  vaultState?: { vault: VaultItemSnapshot[]; vaultCapacity: number; vaultPanelOpen: boolean },
  townHallTier: number = 0,
): ActionItem[] {
```

After:

```ts
function getBuildingActions(
  building: BuildingTile,
  upgrades: PlayerUpgradeSnapshot[] = [],
  vaultState?: { vault: VaultItemSnapshot[]; vaultCapacity: number; vaultPanelOpen: boolean },
  townHallTier: number = 0,
  lockedUnitTypes: ReadonlySet<string> = new Set(),
): ActionItem[] {
```

Then find the `unit-spawner` block (around line 2618):

Before:

```ts
  if (building.capabilities.includes('unit-spawner')) {
    let hasTrainable = false
    for (const unitType of building.spawnUnitTypes ?? []) {
      const def = UNIT_DEF_MAP.get(unitType)
      if (def) {
        const cost = Object.entries(def.resourceCost ?? {})
          .filter(([, amount]) => amount > 0)
          .map(([id, amount]) => ({ resourceId: id, amount, accent: RESOURCE_ACCENT[id] ?? '#94a3b8' }))
        actions.push({
          id: `train-${unitType}`,
          label: def.trainLabel,
          iconDef: { kind: 'unit', type: unitType },
          cost,
        })
        hasTrainable = true
      }
    }
    if (hasTrainable) {
      actions.push({ id: 'set-spawn-point', label: 'Set Rally Point' })
    }
  }
```

After:

```ts
  if (building.capabilities.includes('unit-spawner')) {
    let hasTrainable = false
    for (const unitType of building.spawnUnitTypes ?? []) {
      const def = UNIT_DEF_MAP.get(unitType)
      if (def) {
        const cost = Object.entries(def.resourceCost ?? {})
          .filter(([, amount]) => amount > 0)
          .map(([id, amount]) => ({ resourceId: id, amount, accent: RESOURCE_ACCENT[id] ?? '#94a3b8' }))
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
    if (hasTrainable) {
      actions.push({ id: 'set-spawn-point', label: 'Set Rally Point' })
    }
  }
```

- [ ] **Step 5: Pass `lockedUnitTypes` at the call site**

In the same file, the call site is around line 2023. Update it to pass the locked set:

Before:

```ts
          : getBuildingActions(
              selectedBuilding,
              this.playerUpgrades,
              {
                vault: this.localPlayerVault,
                vaultCapacity: this.localPlayerVaultCapacity,
                vaultPanelOpen: this.vaultPanelOpen,
              },
              this.townHallTier,
            ),
```

After:

```ts
          : getBuildingActions(
              selectedBuilding,
              this.playerUpgrades,
              {
                vault: this.localPlayerVault,
                vaultCapacity: this.localPlayerVaultCapacity,
                vaultPanelOpen: this.vaultPanelOpen,
              },
              this.townHallTier,
              new Set(this.lockedUnitTypes),
            ),
```

- [ ] **Step 6: Typecheck the client**

```bash
cd client/src/game-portal && npx vue-tsc -b
```

Expected: exits 0. Common failure modes:
- `Cannot find name 'BUILDING_DEF_MAP'` — the import in Step 3 was missed.
- `Property 'tooltipTitle' does not exist on type 'ActionItem'` — re-verify the `ActionItem` type already has `tooltipTitle` / `tooltipBody` (the existing upgrade-purchase branch above uses them; if those work, the new ones will work too).

- [ ] **Step 7: Commit**

```bash
git add client/src/game-portal/src/game/core/GameState.ts
git commit -m "feat(client): grey out train action when unit is build-requirement-locked"
```

---

## Task 9: Manual UI verification

**Files:** none (manual playtest).

- [ ] **Step 1: Start dev server + client**

Open two terminals.

Terminal 1 (server, from repo root):

```bash
cd server && go run ./cmd/api
```

Terminal 2 (Vite dev client, from repo root):

```bash
cd client/src/game-portal && npm run dev
```

Open the URL Vite prints (typically `http://localhost:5173`).

- [ ] **Step 2: Verify greyed Archer with tooltip (no Blacksmith)**

1. Start a new single-player game.
2. Build a Barracks (or use a map seed that starts with one).
3. Select the Barracks. Confirm the Archer action icon is visible but greyed/disabled.
4. Hover the Archer icon. Confirm a tooltip appears: "Requires: Blacksmith".
5. Click the Archer icon. Confirm nothing happens — no resources deducted, no production queued.
6. Confirm the Soldier action (and any other un-gated trainable) is still clickable normally.

- [ ] **Step 3: Verify unlock when Blacksmith finishes**

1. Build a Blacksmith. While it is under construction, re-select the Barracks. Archer should remain greyed.
2. Wait for Blacksmith to finish. On the very next visible snapshot, the Archer icon should become clickable.
3. Click Archer. Confirm production queues normally and gold + wood are deducted.

- [ ] **Step 4: Verify re-lock when Blacksmith is destroyed**

1. With a fully-built Blacksmith, queue 1-2 Archers (do not let them all finish).
2. Destroy the Blacksmith (either attack with units, or use the building's demolish action if available).
3. As soon as the Blacksmith is gone, re-select the Barracks. The Archer action should grey back out.
4. The queued Archers should still complete normally.
5. After the queue drains, click the (now greyed) Archer icon — confirm no new production starts.

- [ ] **Step 5: If any of the above fail**

Stop and investigate. Most likely causes:
- Step 2 fail: client `lockedUnitTypes` not being read from the snapshot — re-check Task 8 Step 2.
- Step 3 fail: server `lockedUnitTypesForPlayerLocked` not being recomputed every snapshot — re-check Task 5 Step 6 (the field must be set inside the per-player loop, not cached).
- Step 4 fail: server `playerHasBuildingTypeLocked` not treating destroyed buildings as gone — check whether `Visible=false` or HP-zero buildings are filtered correctly.

Document any issue found here as a follow-up task before claiming the plan complete.

- [ ] **Step 6: No commit needed** (manual playtest only — no file changes).

---

## Self-Review Notes

After writing this plan, the following spec sections were verified to have implementing tasks:

| Spec section | Task(s) |
|---|---|
| Data Model — `UnitDef.RequiresBuildings` | Task 1 |
| Data Model — load-time validation | Task 1 Step 2 |
| Catalog change (archer) | Task 2 |
| Wire protocol — server `LockedUnitTypes` | Task 5 Step 1 |
| Wire protocol — client `lockedUnitTypes` | Task 6 |
| Client UnitDef — `requiresBuildings?` | Task 7 |
| `playerHasBuildingTypeLocked` | Task 3 Step 3 |
| `playerMeetsUnitRequirementsLocked` | Task 3 Step 3 |
| `TrainUnit` gate | Task 4 Step 3 |
| `lockedUnitTypesForPlayerLocked` + snapshot population | Task 5 Steps 4 + 6 |
| `getBuildingActions` greying + tooltip | Task 8 Step 4 |
| `formatBuildingName` helper | Task 8 Step 3 |
| Caller plumbing for `lockedUnitTypes` | Task 8 Step 5 + storage on `GameState` (Step 1+2) |
| Tests — no-Blacksmith no-op | Task 4 Step 1 |
| Tests — mid-construction no-op | Task 4 Step 1 |
| Tests — fully-built succeeds | Task 4 Step 1 |
| Tests — Soldier regression | Task 4 Step 1 |
| Tests — `lockedUnitTypesForPlayerLocked` | Task 5 Step 2 |
| Manual frontend acceptance | Task 9 |

No `TBD`/`TODO`/placeholder steps remain. Each step is bounded (2-5 min of work) and includes the exact code or command.
