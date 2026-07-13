# World Editor Shell — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A new WC3-style world editor (copied from the map editor into its own folder) where you place units/buildings/terrain, click a placed unit to set its rank/items/perks, edit items via the embedded item editor, and Play→Pause an ephemeral test match that runs real objectives but persists no rewards.

**Architecture:** Server gains (1) an `Ephemeral` match flag that no-ops the three sim-reachable profile hooks, and (2) `PlacedUnit` rank/items/perks applied at spawn. Client gets a new `/world-editor` route with a panel duplicated from `MapEditorPanel.vue` (the old editor is never touched), a top toolbar, an extended placed-unit popup, an embedded item-editor popup, and a play/reset harness that swaps the editor canvas to a real `GameClient` running an ephemeral match.

**Tech Stack:** Go 1.22 (server, module `webrts/server`, root `server/`), Vue 3 + TypeScript SPA (`client/src/game-portal`, vitest), server-authoritative sim.

**Spec:** `docs/superpowers/specs/2026-07-12-world-editor-shell-design.md`

## Global Constraints

- Branch: all work on `world-editor` (already created; verify, never switch).
- **Never modify** `client/.../views/Editor.vue` or `client/.../components/MapEditorPanel.vue` — the old map editor must keep working. The world editor is a COPY that diverges.
- Reuse the existing `/maps` catalog for saves (both editors share it); no new maps catalog.
- Playtest is an **ephemeral** match: objectives run (they evaluate in the tick loop regardless), but no reward persists. Server gates the three `game/manager.go` hooks (immediate DP commit, `OnGameOver` DP commit, `RecordKnownRecipe`) on `state.Ephemeral`; the editor client never sends the reward POSTs.
- `PlacedUnit` gains `rank string`, `items []string`, `perks []string` (json omitempty; fully back-compat — old maps unaffected, map editor ignores them). Unknown refs at hydrate → drop-with-`slog.Warn`, never fatal.
- Reserved scratch map id for a never-saved map's Play: `__world_editor_scratch__`.
- AI_RULES apply (IDs not pointers across ticks; `Locked` = caller holds `s.mu`; deterministic sim). The `game/` package must NOT import `profile`.
- All Go commands from `server/`; client from `client/src/game-portal` (`npm run test`, `npm run build`). Known pre-existing failures unrelated to this work (fail identically on main): cmd/api `TestServerReadyLineAndStdinShutdown`; possibly internal/ws `TestSPBaseline_StructuralShape`. No NEW failures.
- `gofmt -l` flags all files on this checkout (CRLF); use `go vet`/`go build` as gates.
- Commit messages: short imperative.

---

### Task 1: Server — `Ephemeral` match flag + reward-hook gating + ephemeral match creation

**Files:**
- Modify: `server/internal/game/state.go` (add `Ephemeral bool` field near `CampaignLevelID` ~L855)
- Modify: `server/internal/game/match.go` (`NewMatch` → set ephemeral)
- Modify: `server/internal/game/manager.go` (ephemeral match constructor; `FindOrCreateMatch` skips ephemeral; gate the 3 hook closures)
- Modify: `server/pkg/protocol/messages.go` + `server/internal/ws/handlers.go` (`join_match` `ephemeral` field → ephemeral match)
- Test: `server/internal/game/match_ephemeral_test.go` (new)

**Interfaces:**
- Consumes: `NewMatchManager`, `fakeCommitter` (test double at `manager_commit_dominion_test.go:11`), `NewGameState`, `GetMapConfigByID`, `DefaultMapID`.
- Produces: `GameState.Ephemeral bool`; `(m *MatchManager) NewEphemeralMatch(mapID string) *Match`; `join_match` message field `Ephemeral bool` (`json:"ephemeral,omitempty"`). Tasks 3/8 send `ephemeral:true`.

- [ ] **Step 1: Write the failing test**

`server/internal/game/match_ephemeral_test.go`:
```go
package game

import "testing"

// TestEphemeralMatch_SuppressesDominionCommit: an ephemeral match's end-of-game
// DP commit path must NOT call the committer, while a normal match must.
func TestEphemeralMatch_SuppressesDominionCommit(t *testing.T) {
	// Normal match commits.
	mmNormal := NewMatchManager()
	cNormal := newFakeCommitter()
	mmNormal.SetDominionPointCommitter(cNormal)
	normal := mmNormal.NewMatch(DefaultMapID())
	normal.State.mu.Lock()
	normal.State.recordMatchSummaryDominionForTest("p1", 5)
	normal.State.mu.Unlock()
	normal.loop.OnGameOver()
	if cNormal.get("p1") != 5 {
		t.Fatalf("normal match: committer should receive 5, got %d", cNormal.get("p1"))
	}

	// Ephemeral match suppresses.
	mmEph := NewMatchManager()
	cEph := newFakeCommitter()
	mmEph.SetDominionPointCommitter(cEph)
	eph := mmEph.NewEphemeralMatch(DefaultMapID())
	if !eph.State.Ephemeral {
		t.Fatal("NewEphemeralMatch must set State.Ephemeral")
	}
	eph.State.mu.Lock()
	eph.State.recordMatchSummaryDominionForTest("p1", 5)
	eph.State.mu.Unlock()
	eph.loop.OnGameOver()
	if cEph.get("p1") != 0 {
		t.Fatalf("ephemeral match must not commit DP, got %d", cEph.get("p1"))
	}
}

// TestFindOrCreateMatch_SkipsEphemeral: a normal join must never be handed an
// ephemeral match sharing the same map id.
func TestFindOrCreateMatch_SkipsEphemeral(t *testing.T) {
	mm := NewMatchManager()
	eph := mm.NewEphemeralMatch(DefaultMapID())
	got := mm.FindOrCreateMatch(DefaultMapID())
	if got.ID == eph.ID {
		t.Fatal("FindOrCreateMatch reused an ephemeral match")
	}
}
```
NOTE: `recordMatchSummaryDominionForTest` is a tiny test seam — if `HumanPlayerMatchSummaries()` can be made to return a non-zero `DominionPointsEarned` another way (e.g. an existing setter or by driving a DP drop), use that instead and delete this helper reference. Grep `HumanPlayerMatchSummaries` and `DominionPointsEarned` to find how a summary's earned value is populated; add the smallest test-only helper (in this file, guarded by `_test.go`) that sets it, OR adjust the test to drive `rollDominionPointDropLocked`. Implement whichever is cleanest; the assertion (ephemeral → 0 commits, normal → N) is the contract.

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./internal/game/ -run "TestEphemeralMatch_|TestFindOrCreateMatch_SkipsEphemeral" -v`
Expected: FAIL to compile — `undefined: NewEphemeralMatch`, `State.Ephemeral`.

- [ ] **Step 3: Add the `Ephemeral` field** (`state.go`, next to `CampaignLevelID` ~L855):
```go
	// CampaignLevelID ... (existing comment)
	CampaignLevelID string

	// Ephemeral marks a throwaway editor-playtest match: the full sim and
	// objective evaluation run, but reward persistence is suppressed (see the
	// gated hooks in manager.go). Set at construction, never changes.
	Ephemeral bool
```

- [ ] **Step 4: Ephemeral match construction** (`match.go` + `manager.go`)

`match.go` — extract an options constructor (keep `NewMatch` as a thin wrapper so existing callers are unchanged):
```go
func NewMatch(id string, mapID string) *Match { return newMatchWithEphemeral(id, mapID, false) }

func newMatchWithEphemeral(id string, mapID string, ephemeral bool) *Match {
	state := NewGameState(GetMapConfigByID(mapID))
	state.Ephemeral = ephemeral
	match := &Match{ID: id, MapID: state.GetMapConfig().ID, State: state /* ...existing fields... */}
	match.loop = NewLoop(state, match)
	match.loop.Start()
	return match
}
```
(Read the current `NewMatch` body and preserve every field it sets on `Match`; only insert `state.Ephemeral = ephemeral` before `NewLoop`.)

`manager.go` — add the manager-level ephemeral constructor beside `NewMatch`/`newMatchLocked`:
```go
// NewEphemeralMatch creates a fresh throwaway match for editor playtesting.
// It is registered so snapshots/commands route, but FindOrCreateMatch never
// reuses it (see the Ephemeral skip there), and its reward hooks no-op.
func (m *MatchManager) NewEphemeralMatch(mapID string) *Match {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.newMatchLockedEphemeral(mapID, true)
}
```
Refactor `newMatchLocked(mapID string) *Match` → `newMatchLockedEphemeral(mapID string, ephemeral bool) *Match` and keep `newMatchLocked(mapID string) *Match { return m.newMatchLockedEphemeral(mapID, false) }`. Inside, the only change is calling `newMatchWithEphemeral(id, mapID, ephemeral)` instead of `NewMatch(id, mapID)`.

- [ ] **Step 5: Gate the three hook closures + `FindOrCreateMatch`** (`manager.go`)

At the top of each closure captured in `newMatchLockedEphemeral`, early-return when the match is ephemeral (read live at fire time — safe, the flag is set before `loop.Start`):
- Immediate DP handler (~L90): first line inside → `if match.State.Ephemeral { return }`.
- Recipe-crafted handler (~L107): first line inside → `if match.State.Ephemeral { return }`.
- `OnGameOver` (~L117): first line inside → `if match.State.Ephemeral { return }` (before the committer loop; if `OnGameOver` also contains continue-play/teardown logic that must still run for ephemeral matches, gate ONLY the committer loop instead — read the full closure and gate the minimal reward portion).

`FindOrCreateMatch` (~L147) — add `&& !match.State.Ephemeral` to the reuse predicate so an ephemeral match is never handed to a normal join.

- [ ] **Step 6: `join_match` ephemeral field → ephemeral match**

`server/pkg/protocol/messages.go` — the `join_match` message struct (grep `CachedMapHashes` to find it, ~L696) gains:
```go
	Ephemeral bool `json:"ephemeral,omitempty"`
```
`server/internal/ws/handlers.go` (~L403) — when creating a match on join, branch on ephemeral:
```go
	if match == nil {
		if msg.Ephemeral {
			match = h.manager.NewEphemeralMatch(mapID)
		} else {
			match = h.manager.FindOrCreateMatch(mapID)
		}
	}
```
(Leave the `msg.MatchID` resume branch above untouched.)

- [ ] **Step 7: Run tests + whole package + commit**

Run: `cd server && go test ./internal/game/ -run "TestEphemeralMatch_|TestFindOrCreateMatch_SkipsEphemeral" -v`
Expected: PASS.
Run: `cd server && go build ./... && go test ./internal/game/ ./internal/ws/ 2>&1 | grep -E "^(--- FAIL|FAIL|ok)"`
Expected: `ok` (no new failures).
```bash
git add server/internal/game/state.go server/internal/game/match.go server/internal/game/manager.go server/internal/game/match_ephemeral_test.go server/pkg/protocol/messages.go server/internal/ws/handlers.go
git commit -m "Add ephemeral test-match flag that suppresses reward persistence"
```

---

### Task 2: Server — `PlacedUnit` rank/items/perks applied at spawn

**Files:**
- Modify: `server/pkg/protocol/messages.go` (`PlacedUnit` struct + `UnmarshalJSON`)
- Modify: `server/internal/game/maps.go` (`hydratePlacedUnits` — validate new fields)
- Modify: `server/internal/game/state_spawn.go` (`spawnPlacedUnitsForPlayerLocked` — apply rank/items/perks)
- Test: `server/internal/game/placed_unit_instance_test.go` (new)

**Interfaces:**
- Consumes: `applyRankModifiersLocked(unit, preserveHealthPercent bool)`, `recomputeUnitEquipmentBonusLocked(unit)`, `getItemDef`, `getPerkDef` (grep exact name), `getUnitDef`, `spawnPlayerUnitLocked`.
- Produces: `PlacedUnit.Rank string`, `.Items []string`, `.Perks []string`; a spawn that applies them. Task 6 (client) writes these fields.

- [ ] **Step 1: Write the failing test**

`server/internal/game/placed_unit_instance_test.go`:
```go
package game

import (
	"encoding/json"
	"testing"

	"webrts/server/pkg/protocol"
)

// TestPlacedUnit_RankItemsPerksRoundTrip: the new fields survive JSON
// round-trip (back-compat: absent fields decode to zero values).
func TestPlacedUnit_RankItemsPerksRoundTrip(t *testing.T) {
	raw := `{"x":3,"y":4,"id":"u1","playerSlot":"player1","unitType":"soldier","rank":"silver","items":["fire_sword"],"perks":["p_a","p_b"]}`
	var pu protocol.PlacedUnit
	if err := json.Unmarshal([]byte(raw), &pu); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if pu.Rank != "silver" || len(pu.Items) != 1 || pu.Items[0] != "fire_sword" || len(pu.Perks) != 2 {
		t.Fatalf("fields lost: %+v", pu)
	}
	// Legacy shape (no new fields) still decodes.
	var legacy protocol.PlacedUnit
	if err := json.Unmarshal([]byte(`{"x":0,"y":0,"id":"u2","playerSlot":"enemy","unitType":"soldier"}`), &legacy); err != nil {
		t.Fatalf("legacy unmarshal: %v", err)
	}
	if legacy.Rank != "" || legacy.Items != nil || legacy.Perks != nil {
		t.Errorf("legacy should have zero new fields: %+v", legacy)
	}
}

// TestSpawnPlacedUnit_AppliesRankItemsPerks: a placed player unit with rank +
// items + perks spawns with that rank applied, the item equipped, and the
// perks assigned. Uses the DefaultMapID map's grid; places a soldier for p1.
func TestSpawnPlacedUnit_AppliesRankItemsPerks(t *testing.T) {
	cfg := GetMapConfigByID(DefaultMapID())
	cfg.PlacedUnits = []protocol.PlacedUnit{{
		GridCoord: protocol.GridCoord{X: 5, Y: 5}, ID: "pu1", PlayerSlot: "player1",
		UnitType: "soldier", Rank: "silver", Items: []string{"fire_sword"}, Perks: []string{},
	}}
	s := NewGameStateWithSeed(cfg, 7)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.EnsurePlayerLocked("p1") // adds player, assigns to a slot; grep the exact locked adder
	// Resolve the spawned unit for p1 of type soldier.
	var spawned *Unit
	for _, u := range s.Units {
		if u != nil && u.OwnerID == "p1" && u.UnitType == "soldier" {
			spawned = u
			break
		}
	}
	if spawned == nil {
		t.Fatal("placed soldier did not spawn for p1")
	}
	if spawned.Rank != "silver" {
		t.Errorf("rank = %q, want silver", spawned.Rank)
	}
	equippedFire := false
	for _, e := range spawned.Equipped {
		if e != nil && e.ItemID == "fire_sword" {
			equippedFire = true
		}
	}
	if !equippedFire {
		t.Error("fire_sword not equipped on the placed unit")
	}
}
```
NOTE on the test's player/slot wiring: the exact "add a player and map them to slot player1 then spawn placed units" call may be `EnsurePlayerWithUpgrades` (state.go:3605, which calls `spawnPlacedUnitsForPlayerLocked` at 3704) rather than a bare `EnsurePlayerLocked`. Read state.go around 3605–3704 and drive whatever locked entry actually triggers placed-unit spawn for the player mapped to `player1`. The assertion (rank applied, item equipped) is the contract; adapt the setup to the real spawn trigger. If mapping a player to the `player1` slot needs a slot-claim helper, use the one `spawnPlacedUnitsForPlayerLocked` relies on (`findPlayerLabelLocked`).

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./internal/game/ -run "TestPlacedUnit_|TestSpawnPlacedUnit_" -v`
Expected: FAIL — new fields don't exist / not applied.

- [ ] **Step 3: Extend `PlacedUnit`** (`messages.go`):
```go
type PlacedUnit struct {
	GridCoord
	ID         string   `json:"id"`
	PlayerSlot string   `json:"playerSlot"`
	UnitType   string   `json:"unitType"`
	AggroRange float64  `json:"aggroRange,omitempty"`
	LeashRange float64  `json:"leashRange,omitempty"`
	Rank       string   `json:"rank,omitempty"`
	Items      []string `json:"items,omitempty"`
	Perks      []string `json:"perks,omitempty"`
}
```
And in `UnmarshalJSON`'s `rawShape`, add the three fields + copy them:
```go
		Rank     string   `json:"rank"`
		Items    []string `json:"items"`
		Perks    []string `json:"perks"`
```
```go
	p.Rank = raw.Rank
	p.Items = raw.Items
	p.Perks = raw.Perks
```

- [ ] **Step 4: Validate at hydrate** (`maps.go` `hydratePlacedUnits`, before `out = append(out, entry)`):
```go
		// Drop unknown per-instance refs (never fatal), mirroring the
		// unknown-unitType discipline above.
		if entry.Rank != "" && !isValidRankForUnitType(entry.UnitType, entry.Rank) {
			slog.Warn("hydratePlacedUnits: dropping unknown rank", "unitType", entry.UnitType, "rank", entry.Rank)
			entry.Rank = ""
		}
		entry.Items = filterKnownItemIDs(entry.Items)
		entry.Perks = filterKnownPerkIDsForUnit(entry.UnitType, entry.Perks)
```
Implement the three helpers in `maps.go` (or nearest def file) using existing lookups:
```go
func isValidRankForUnitType(unitType, rank string) bool {
	// A rank is valid if the unit type has a path/rank modifier for it. Grep
	// pathModifierFor / the rank list source; return whether `rank` is a known
	// rank string for this unit. If ranks are global (bronze/silver/gold),
	// validate against that global set.
	return rankIsKnown(rank) // replace with the real check found in progression.go
}
func filterKnownItemIDs(ids []string) []string {
	out := ids[:0:0]
	for _, id := range ids {
		if _, ok := getItemDef(id); ok {
			out = append(out, id)
		} else {
			slog.Warn("hydratePlacedUnits: dropping unknown item", "item", id)
		}
	}
	return out
}
func filterKnownPerkIDsForUnit(unitType string, ids []string) []string {
	out := ids[:0:0]
	for _, id := range ids {
		if perkExistsForUnit(unitType, id) { // grep the perk lookup used by ListPerkDefs / assignUnitPerkLocked
			out = append(out, id)
		} else {
			slog.Warn("hydratePlacedUnits: dropping unknown perk", "unitType", unitType, "perk", id)
		}
	}
	return out
}
```
Read `progression.go` (rank source) and `perks.go` / `perk_defs.go` (perk lookup) to fill `rankIsKnown` / `perkExistsForUnit` with the real accessors. Keep them tolerant (unknown → drop, warn).

- [ ] **Step 5: Apply at spawn** (`state_spawn.go` `spawnPlacedUnitsForPlayerLocked`, right after `unit := s.spawnPlayerUnitLocked(...)` and the nil check):
```go
		if unit == nil {
			slog.Warn("...", "unitType", entry.UnitType)
			continue
		}
		s.applyPlacedUnitInstanceLocked(unit, entry)
```
New helper (same file):
```go
// applyPlacedUnitInstanceLocked stamps a placed unit's authored rank, items,
// and perks onto the freshly spawned unit. Each is optional; unknown refs were
// already dropped at hydrate. Caller holds s.mu.
func (s *GameState) applyPlacedUnitInstanceLocked(unit *Unit, entry protocol.PlacedUnit) {
	if entry.Rank != "" {
		unit.Rank = entry.Rank
		s.assignUnitPathOnRankUpLocked(unit) // grep: this is what rank-up calls to set ProgressionPath; include only if needed for the rank to take effect
		s.applyRankModifiersLocked(unit, false)
	}
	for _, itemID := range entry.Items {
		if _, ok := getItemDef(itemID); !ok {
			continue
		}
		// Equip into the next free slot (InventorySize is rank-gated; a placed
		// unit at a higher rank has slots). Append an EquippedItem directly and
		// recompute — the same shape handleEquipItemLocked uses.
		s.equipItemDirectLocked(unit, itemID)
	}
	if len(entry.Items) > 0 {
		s.recomputeUnitEquipmentBonusLocked(unit)
	}
	for _, perkID := range entry.Perks {
		unit.PerkIDs = append(unit.PerkIDs, perkID)
		s.applyPerkGrantedHooksLocked(unit, perkID)
	}
}

// equipItemDirectLocked puts an item into the unit's next free equipment slot
// without going through the vault/player flow (placed units aren't tied to a
// vault). Fills the first nil slot; if none, appends (placed-unit authoring
// intent wins over the normal slot cap). Caller holds s.mu; caller recomputes.
func (s *GameState) equipItemDirectLocked(unit *Unit, itemID string) {
	item := &EquippedItem{InstanceID: s.allocItemInstanceIDLocked(), ItemID: itemID, Stacks: 1}
	for i := range unit.Equipped {
		if unit.Equipped[i] == nil {
			unit.Equipped[i] = item
			return
		}
	}
	unit.Equipped = append(unit.Equipped, item)
}
```
IMPORTANT: read `handleEquipItemLocked` (state_items.go:521) and `recomputeUnitEquipmentBonusLocked` (state_items.go:226) first — mirror how `Equipped` is sized/indexed there. If `InventorySize` is 0 at spawn before rank modifiers, apply rank BEFORE equipping (rank sets InventorySize). Order in `applyPlacedUnitInstanceLocked`: rank first (sets InventorySize), then items, then perks. Verify `allocItemInstanceIDLocked` exists (grep); if instance IDs are allocated differently, use that. Also confirm `assignUnitPathOnRankUpLocked` is needed — if `applyRankModifiersLocked` reads `unit.ProgressionPath` and a placed unit has none, you may need to assign a default path or leave path empty (base modifiers). Test drives the real behavior; adjust to what makes rank actually apply.

- [ ] **Step 6: Run tests + whole package + commit**

Run: `cd server && go test ./internal/game/ -run "TestPlacedUnit_|TestSpawnPlacedUnit_" -v`
Expected: PASS.
Run: `cd server && go build ./... && go test ./internal/game/ 2>&1 | grep -E "^(--- FAIL|FAIL|ok)"`
Expected: `ok`.
```bash
git add server/internal/game/state_spawn.go server/internal/game/maps.go server/pkg/protocol/messages.go server/internal/game/placed_unit_instance_test.go
git commit -m "Apply placed-unit rank/items/perks at spawn"
```

---

### Task 3: Client — protocol types + `ephemeral` join option

**Files:**
- Modify: `client/src/game-portal/src/game/network/protocol.ts` (`PlacedUnit` type; `join_match` message type)
- Modify: `client/src/game-portal/src/game/network/NetworkClient.ts` (thread `ephemeral` into the join message)
- Modify: `client/src/game-portal/src/game/core/GameClient.ts` (accept + forward an `ephemeral` option)
- Test: `client/src/game-portal/src/game/network/joinEphemeral.test.ts` (new, if NetworkClient's join is unit-testable; else typecheck-only)

**Interfaces:**
- Consumes: Task 1's `join_match` `ephemeral` wire field.
- Produces: TS `PlacedUnit` with `rank?`, `items?`, `perks?`; `NetworkClient.setEphemeral(v: boolean)` (or a `connect({ephemeral})` option); `GameClient.start({ephemeral})`. Tasks 6/8 use these.

- [ ] **Step 1: Extend the TS `PlacedUnit`** (`protocol.ts` ~L149):
```ts
export type PlacedUnit = {
  id: string
  x: number
  y: number
  playerSlot: PlacedUnitSlot
  unitType: string
  aggroRange?: number
  leashRange?: number
  rank?: string
  items?: string[]
  perks?: string[]
}
```
And add `ephemeral?: boolean` to the `join_match` `ClientMessage` variant (grep `'join_match'` in protocol.ts).

- [ ] **Step 2: Thread `ephemeral` through the client core**

`NetworkClient.ts`: add a field `private ephemeral = false` + `setEphemeral(v: boolean) { this.ephemeral = v }`, and include `ephemeral: this.ephemeral || undefined` in the `joinMessage` (~L313).
`GameClient.ts`: `start(options)` (~L190) — accept `options.ephemeral` and call `this.network.setEphemeral(!!options.ephemeral)` before `this.network.connect(options)`. (Read the current `start` options type and extend it.)

- [ ] **Step 3: Test / typecheck**

If a focused unit test of the join payload is feasible (construct a `NetworkClient`, stub the socket, call the join path, assert `ephemeral:true` in the sent message — follow `GameClient.recipeCommands.test.ts` for how the client core is tested), write `joinEphemeral.test.ts`. If the socket isn't easily stubbable, rely on `npm run build` (vue-tsc) as the gate and note it.
Run: `cd client/src/game-portal && npm run build` (+ `npm run test` if a test was added).
Expected: clean.

- [ ] **Step 4: Commit**
```bash
git add client/src/game-portal/src/game/network/protocol.ts client/src/game-portal/src/game/network/NetworkClient.ts client/src/game-portal/src/game/core/GameClient.ts
git commit -m "Client: PlacedUnit rank/items/perks types + ephemeral join option"
```

---

### Task 4: Client — `/world-editor` route + view + duplicated panel

**Files:**
- Create: `client/src/game-portal/src/views/WorldEditor.vue`
- Create: `client/src/game-portal/src/components/world-editor/WorldEditorPanel.vue` (verbatim copy of `MapEditorPanel.vue`, renamed)
- Modify: `client/src/game-portal/src/router/index.ts` (route + import)
- Test: manual (build + load)

**Interfaces:**
- Produces: route `/world-editor`; `WorldEditorPanel.vue` (`defineModel<MapConfig>`), the base the later client tasks extend. The old `MapEditorPanel.vue`/`Editor.vue` are untouched.

- [ ] **Step 1: Duplicate the panel** (verbatim; this is a file copy, not a rewrite):
```bash
mkdir -p client/src/game-portal/src/components/world-editor
cp client/src/game-portal/src/components/MapEditorPanel.vue client/src/game-portal/src/components/world-editor/WorldEditorPanel.vue
```
No content edits yet — it must remain byte-identical logic so it loads and behaves exactly like the map editor. (Later tasks diverge it.)

- [ ] **Step 2: Create the view** (`views/WorldEditor.vue`, mirroring `Editor.vue`):
```vue
<template>
  <div class="world-editor-view">
    <div class="world-editor-topbar world-editor-topbar--right">
      <ExitButton @click="router.push('/')" />
    </div>
    <WorldEditorPanel v-model="editorMap" />
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import WorldEditorPanel from '@/components/world-editor/WorldEditorPanel.vue'
import ExitButton from '@/components/ui/ExitButton.vue'
import { createEditorMapConfig } from '@/game/maps/mapConfig'

const router = useRouter()
const editorMap = ref(createEditorMapConfig())
</script>

<style scoped>
.world-editor-view {
  position: relative; z-index: 1; width: 100%; height: 100%;
  min-height: 0; min-width: 0; display: flex; overflow: hidden;
}
.world-editor-topbar { position: absolute; top: 16px; z-index: 20; }
.world-editor-topbar--right { right: 16px; }
</style>
```

- [ ] **Step 3: Route** (`router/index.ts`): import `WorldEditor` alongside `Editor` (top of file), and add after the `/editor` line:
```ts
{ path: '/world-editor', component: WorldEditor, meta: { hideMenuChrome: true } },
```

- [ ] **Step 4: Build + manual load**

Run: `cd client/src/game-portal && npm run build`
Expected: clean (a verbatim copy + one route compiles).
Manual: `npm run dev` + start the Go server, navigate to `/world-editor` — the editor loads and behaves like the map editor (paint terrain, place a unit, save). Report what you verified.

- [ ] **Step 5: Commit**
```bash
git add client/src/game-portal/src/views/WorldEditor.vue client/src/game-portal/src/components/world-editor/WorldEditorPanel.vue client/src/game-portal/src/router/index.ts
git commit -m "Add /world-editor route + view + panel duplicated from map editor"
```

---

### Task 5: Client — top toolbar (category set: active + coming-soon)

**Files:**
- Create: `client/src/game-portal/src/components/world-editor/WorldEditorToolbar.vue`
- Modify: `client/src/game-portal/src/components/world-editor/WorldEditorPanel.vue` (mount the toolbar; expose section/tool activation + a Play stub)
- Test: `client/src/game-portal/src/components/world-editor/worldEditorToolbar.test.ts` (new)

**Interfaces:**
- Consumes: the copied panel's existing tool/section state (`openSection`/`toggleSection`, `activeBrushMode`).
- Produces: `WorldEditorToolbar.vue` emitting `select(category)`; `WORLD_EDITOR_CATEGORIES` constant. Task 7 adds the Items category action; Task 8 wires Play.

- [ ] **Step 1: Failing test** (`worldEditorToolbar.test.ts`):
```ts
import { describe, expect, it } from 'vitest'
import { WORLD_EDITOR_CATEGORIES } from './WorldEditorToolbar.vue'

describe('world editor toolbar categories', () => {
  it('lists the full vision with the milestone-1 ones enabled', () => {
    const byId = Object.fromEntries(WORLD_EDITOR_CATEGORIES.map((c) => [c.id, c]))
    // Full future set is present (visible), enabled flags reflect this milestone.
    expect(byId.units.enabled).toBe(true)
    expect(byId.buildings.enabled).toBe(true)
    expect(byId.terrain.enabled).toBe(true)
    expect(byId.items.enabled).toBe(true)
    expect(byId.play.enabled).toBe(true)
    // Later sub-projects: visible but disabled ("coming soon").
    expect(byId.abilities.enabled).toBe(false)
    expect(byId.effects.enabled).toBe(false)
    expect(byId.projectiles.enabled).toBe(false)
    expect(byId.perks.enabled).toBe(false)
    expect(byId.campaigns.enabled).toBe(false)
  })
})
```
(Export `WORLD_EDITOR_CATEGORIES` from `WorldEditorToolbar.vue` via a plain `<script lang="ts">` block, mirroring the `MENU_ENTRIES` two-block pattern in `MainMenu.vue`.)

- [ ] **Step 2: Verify fail** — `cd client/src/game-portal && npm run test -- worldEditorToolbar` → fails (module missing).

- [ ] **Step 3: Implement `WorldEditorToolbar.vue`**:
```vue
<template>
  <div class="we-toolbar" role="toolbar">
    <button
      v-for="c in categories"
      :key="c.id"
      type="button"
      class="we-toolbar__btn"
      :class="{ 'we-toolbar__btn--disabled': !c.enabled, 'we-toolbar__btn--active': activeId === c.id }"
      :disabled="!c.enabled"
      :title="c.enabled ? c.label : c.label + ' — coming soon'"
      @click="c.enabled && emit('select', c.id)"
    >
      {{ c.label }}
    </button>
  </div>
</template>

<script lang="ts">
export type WorldEditorCategory = { id: string; label: string; enabled: boolean }
// Full vision, with milestone-1 categories enabled and later sub-projects
// visible-but-disabled so the roadmap is discoverable in the UI.
export const WORLD_EDITOR_CATEGORIES: WorldEditorCategory[] = [
  { id: 'terrain', label: 'Terrain', enabled: true },
  { id: 'obstacles', label: 'Obstacles', enabled: true },
  { id: 'buildings', label: 'Buildings', enabled: true },
  { id: 'units', label: 'Units', enabled: true },
  { id: 'items', label: 'Items', enabled: true },
  { id: 'unit-types', label: 'Unit Types', enabled: false },
  { id: 'unit-paths', label: 'Unit Paths', enabled: false },
  { id: 'perks', label: 'Perks', enabled: false },
  { id: 'abilities', label: 'Abilities', enabled: false },
  { id: 'effects', label: 'Effects', enabled: false },
  { id: 'projectiles', label: 'Projectiles', enabled: false },
  { id: 'campaigns', label: 'Campaigns', enabled: false },
  { id: 'play', label: '▶ Play', enabled: true },
]
</script>

<script setup lang="ts">
defineProps<{ activeId?: string }>()
const emit = defineEmits<{ select: [string] }>()
const categories = WORLD_EDITOR_CATEGORIES
</script>

<style scoped>
.we-toolbar { display: flex; gap: 4px; padding: 6px 8px; flex-wrap: wrap;
  background: rgba(3, 8, 14, 0.92); border-bottom: 1px solid rgba(148,163,184,0.22); }
.we-toolbar__btn { padding: 6px 10px; border-radius: 8px; font-size: 0.78rem;
  color: #f8fafc; background: rgba(25,35,52,0.9); border: 1px solid rgba(148,163,184,0.2); }
.we-toolbar__btn--active { border-color: rgba(215,187,132,0.7); }
.we-toolbar__btn--disabled { opacity: 0.4; cursor: not-allowed; }
</style>
```
(NO literal `cursor` on enabled buttons — only `not-allowed` on the disabled state, per repo rules.)

- [ ] **Step 4: Mount it in `WorldEditorPanel.vue`** at the top of the template, wiring `@select` to the panel's existing tool activation: terrain/obstacles/buildings/units → set the panel's `activeBrushMode` / open the matching existing section; `items` → open the Items popup (stub in this task: `itemsPopupOpen.value = true`, popup added in Task 7); `play` → `emit`/call a `startPlaytest()` stub (real in Task 8). Add `const itemsPopupOpen = ref(false)` and a `function onToolbarSelect(id: string) { ... }` mapping category → existing behavior. Keep the copied accordion sections intact beneath — the toolbar is an additive top strip.

- [ ] **Step 5: Test + build + commit**

Run: `cd client/src/game-portal && npm run test -- worldEditorToolbar && npm run build`
Expected: PASS + clean.
```bash
git add client/src/game-portal/src/components/world-editor/WorldEditorToolbar.vue client/src/game-portal/src/components/world-editor/worldEditorToolbar.test.ts client/src/game-portal/src/components/world-editor/WorldEditorPanel.vue
git commit -m "World editor: top toolbar with full category set (milestone-1 active)"
```

---

### Task 6: Client — placed-unit instance editing (rank / items / perks)

**Files:**
- Create: `client/src/game-portal/src/components/world-editor/placedUnitInstance.ts` (pure form transforms)
- Modify: `client/src/game-portal/src/components/world-editor/WorldEditorPanel.vue` (extend the existing per-unit edit popup with rank/items/perks; load catalogs)
- Test: `client/src/game-portal/src/components/world-editor/placedUnitInstance.test.ts` (new)

**Interfaces:**
- Consumes: `fetchUnitDefs`, `fetchItemDefs`, `fetchPerkDefs` (catalog.ts); the copied panel's `selectedEditPlacedUnit` / `updateSelectedPlacedUnit` (MapEditorPanel L1456–1524 region).
- Produces: pure helpers `ranksForUnitType(unitDefs, unitType): string[]`, `perksForUnitType(perkDefs, unitType): PerkDef[]`, and `applyInstanceEdit(unit, patch): PlacedUnit`. Rank/items/perks persist onto the `PlacedUnit`.

- [ ] **Step 1: Failing test** (`placedUnitInstance.test.ts`):
```ts
import { describe, expect, it } from 'vitest'
import { applyInstanceEdit, perksForUnitType } from './placedUnitInstance'
import type { PlacedUnit } from '@/game/network/protocol'

const base: PlacedUnit = { id: 'u1', x: 1, y: 1, playerSlot: 'player1', unitType: 'soldier' }

describe('placed unit instance edits', () => {
  it('applies rank/items/perks onto the placed unit, dropping empties', () => {
    const next = applyInstanceEdit(base, { rank: 'silver', items: ['fire_sword'], perks: ['p_a'] })
    expect(next.rank).toBe('silver')
    expect(next.items).toEqual(['fire_sword'])
    expect(next.perks).toEqual(['p_a'])
    // Clearing rank drops the field (kept out of the wire).
    const cleared = applyInstanceEdit(next, { rank: '', items: [], perks: [] })
    expect(cleared.rank).toBeUndefined()
    expect(cleared.items).toBeUndefined()
    expect(cleared.perks).toBeUndefined()
  })
  it('filters perks to the unit type', () => {
    const perkDefs = [
      { id: 'p_a', unitType: 'soldier' } as any,
      { id: 'p_b', unitType: 'archer' } as any,
    ]
    expect(perksForUnitType(perkDefs, 'soldier').map((p) => p.id)).toEqual(['p_a'])
  })
})
```
(Verify the real `PerkDef` shape from `fetchPerkDefs`/catalog types for how a perk is associated to a unit type — the field may be `unitType`, `unit`, or derived from a path; adapt `perksForUnitType` and the test's fixture to the real shape. Grep the `PerkDef` type.)

- [ ] **Step 2: Verify fail** — `npm run test -- placedUnitInstance` → module missing.

- [ ] **Step 3: Implement `placedUnitInstance.ts`**:
```ts
import type { PlacedUnit } from '@/game/network/protocol'

export type InstancePatch = { rank: string; items: string[]; perks: string[] }

// applyInstanceEdit returns a new PlacedUnit with rank/items/perks set, dropping
// empty values so they stay off the wire (omitempty parity with the server).
export function applyInstanceEdit(unit: PlacedUnit, patch: InstancePatch): PlacedUnit {
  const next: PlacedUnit = { ...unit }
  if (patch.rank) next.rank = patch.rank
  else delete next.rank
  if (patch.items.length) next.items = [...patch.items]
  else delete next.items
  if (patch.perks.length) next.perks = [...patch.perks]
  else delete next.perks
  return next
}

// ranksForUnitType returns the rank strings a unit type can hold (for the rank
// dropdown). Source it from the unit def's rank/path data returned by
// fetchUnitDefs; if ranks are global, return the global list.
export function ranksForUnitType(unitDefs: unknown[], unitType: string): string[] {
  // Implement against the real UnitDef shape (grep fetchUnitDefs return type);
  // return e.g. ['bronze','silver','gold'] filtered to what this unit supports.
  return ['bronze', 'silver', 'gold']
}

// perksForUnitType filters the perk catalog to perks valid for a unit type.
export function perksForUnitType<T extends { id: string; unitType?: string }>(
  perkDefs: T[], unitType: string,
): T[] {
  return perkDefs.filter((p) => !p.unitType || p.unitType === unitType)
}
```
(Fill `ranksForUnitType` / `perksForUnitType` against the real def shapes; keep `applyInstanceEdit` exactly as the test asserts.)

- [ ] **Step 4: Wire into the panel's unit-edit popup** — in `WorldEditorPanel.vue`, extend the existing per-placed-unit edit popup (the `selectedEditPlacedUnit` block, MapEditorPanel L1456–1524 region) with: a **Rank** `<select>` (options `ranksForUnitType`), an **Items** multi-select (from `fetchItemDefs`), a **Perks** multi-select (`perksForUnitType(fetchPerkDefs, unit.unitType)`). On change, call `applyInstanceEdit` and write the result back into `placedUnits` + `model` (reuse the panel's existing `updateSelectedPlacedUnit` mutation path so it stays in sync). Load the item/unit/perk catalogs in `onMounted` (Promise.all with the existing catalog fetches). Guard: only show rank/items/perks for `playerSlot` that is a real player (not neutral), matching how instance data is meaningful.

- [ ] **Step 5: Test + build + commit**

Run: `cd client/src/game-portal && npm run test -- placedUnitInstance && npm run build`
Expected: PASS + clean.
Manual: place a soldier → click it → set rank silver + add fire_sword + a perk → confirm it persists on re-click; save + reload the map keeps it.
```bash
git add client/src/game-portal/src/components/world-editor/placedUnitInstance.ts client/src/game-portal/src/components/world-editor/placedUnitInstance.test.ts client/src/game-portal/src/components/world-editor/WorldEditorPanel.vue
git commit -m "World editor: edit placed-unit rank/items/perks"
```

---

### Task 7: Client — embed the Item/Equipment editor as a popup

**Files:**
- Modify: `client/src/game-portal/src/components/world-editor/WorldEditorPanel.vue` (Items popup hosting `ItemEditorPanel.vue`)
- Test: manual (build + open)

**Interfaces:**
- Consumes: the existing `ItemEditorPanel.vue` (unchanged), the toolbar `items` action (Task 5's `itemsPopupOpen`).
- Produces: a modal overlay hosting the item editor; closing returns to the map. No prop/emit contract change on `ItemEditorPanel`.

- [ ] **Step 1: Implement the popup** — in `WorldEditorPanel.vue`, add an overlay shown when `itemsPopupOpen`:
```vue
  <div v-if="itemsPopupOpen" class="we-modal-overlay">
    <div class="we-modal we-modal--wide">
      <div class="we-modal__header">
        <span>Item / Equipment Editor</span>
        <UiButton size="sm" @click="itemsPopupOpen = false">Close</UiButton>
      </div>
      <div class="we-modal__body">
        <ItemEditorPanel />
      </div>
    </div>
  </div>
```
Import `ItemEditorPanel from '@/components/ItemEditorPanel.vue'` and `UiButton`. Styles: a fixed full-screen dimmed overlay with a large centered panel (`min(1100px, 94vw)` × `min(88vh)`, `overflow: hidden`; the item editor scrolls internally). No literal `cursor` declarations. Because `ItemEditorPanel` mounts its own catalog fetches on mount, opening the popup is enough; edits it makes (saving items) are immediately reflected when the user later edits placed-unit items (they re-fetch on popup use or on next unit-edit — acceptable for a dev tool; note it).

- [ ] **Step 2: Build + manual + commit**

Run: `cd client/src/game-portal && npm run build`
Expected: clean.
Manual: toolbar → Items → the full item editor opens in a modal; create/edit an item; Close → back to the map.
```bash
git add client/src/game-portal/src/components/world-editor/WorldEditorPanel.vue
git commit -m "World editor: embed the item editor as a toolbar popup"
```

---

### Task 8: Client — ephemeral Play / Reset harness

**Files:**
- Create: `client/src/game-portal/src/components/world-editor/usePlaytest.ts`
- Create: `client/src/game-portal/src/components/world-editor/PlaytestBar.vue`
- Modify: `client/src/game-portal/src/components/world-editor/WorldEditorPanel.vue` (play canvas + bar; wire toolbar Play)
- Test: `client/src/game-portal/src/components/world-editor/usePlaytest.test.ts` (thin) + manual E2E

**Interfaces:**
- Consumes: `saveMapCatalogFile` (persist current map before Play), `GameClient` (Task 3's `start({ephemeral})`), the panel's current `model` (MapConfig) + `placedUnits`.
- Produces: `usePlaytest(getMapFile, playCanvasRef)` → `{ playing, start, stop }`; a `PlaytestBar.vue` (Play/Pause/Stop). Reset is implicit — stopping restores the editor render because the editor's `model` never changed.

- [ ] **Step 1: Thin failing test** (`usePlaytest.test.ts`) — verify the state machine + scratch-id logic without a real match:
```ts
import { describe, expect, it, vi } from 'vitest'
import { scratchMapId, resolvePlaytestMapId } from './usePlaytest'

describe('playtest map id resolution', () => {
  it('uses the working map id when saved, else the scratch id', () => {
    expect(resolvePlaytestMapId({ id: 'my_map' } as any)).toBe('my_map')
    expect(resolvePlaytestMapId({ id: 'editor-draft' } as any)).toBe(scratchMapId)
    expect(resolvePlaytestMapId({ id: '' } as any)).toBe(scratchMapId)
  })
})
```
(`editor-draft` is `createEditorMapConfig`'s default id — treat it and empty as "never really saved" → scratch id. Confirm the default id constant.)

- [ ] **Step 2: Verify fail** — `npm run test -- usePlaytest` → module missing.

- [ ] **Step 3: Implement `usePlaytest.ts`**:
```ts
import { ref } from 'vue'
import type { MapConfig } from '@/game/network/protocol'
import type { MapCatalogFile } from '@/game/maps/catalog'
import { saveMapCatalogFile } from '@/game/maps/catalog'
import { GameClient } from '@/game/core/GameClient'

export const scratchMapId = '__world_editor_scratch__'

// resolvePlaytestMapId picks the id to run: the working map's real id, or the
// reserved scratch id for a never-saved draft.
export function resolvePlaytestMapId(map: Pick<MapConfig, 'id'>): string {
  if (!map.id || map.id === 'editor-draft') return scratchMapId
  return map.id
}

export function usePlaytest(getPlayCanvas: () => HTMLCanvasElement | null) {
  const playing = ref(false)
  let client: GameClient | null = null

  // start: persist the current editor map (so the server can match it,
  // including unsaved placements), then run an ephemeral match on the play
  // canvas via a real GameClient.
  async function start(file: MapCatalogFile) {
    const canvas = getPlayCanvas()
    if (!canvas) return
    const mapId = resolvePlaytestMapId(file.map)
    // Persist under the resolved id (scratch for drafts) so join_match can find it.
    await saveMapCatalogFile({ ...file, id: mapId, map: { ...file.map, id: mapId } })
    client = new GameClient(canvas, mapId)
    await client.start({ ephemeral: true })
    playing.value = true
  }

  // stop: tear down the match. The editor's own MapConfig is untouched, so the
  // caller simply re-shows the editor canvas — placements "snap back" for free.
  function stop() {
    if (client) {
      client.destroy()
      client = null
    }
    playing.value = false
  }

  return { playing, start, stop }
}
```
(Confirm `GameClient` exposes `destroy()` — grep; `useGameClient.ts` calls `destroy`. Confirm `saveMapCatalogFile` accepts the shape; reuse the panel's `exportedCatalogFile` computed as the `file` argument. If persisting a scratch id collides with campaign-conflict validation, the scratch map carries no campaign block so it won't 409.)

- [ ] **Step 4: `PlaytestBar.vue`** — a small overlay bar with Play/Pause/Stop:
```vue
<template>
  <div class="playtest-bar">
    <UiButton size="sm" @click="emit('stop')">■ Stop &amp; reset</UiButton>
    <span class="playtest-bar__label">Playtest (ephemeral — no rewards)</span>
  </div>
</template>
<script setup lang="ts">
import UiButton from '@/components/ui/UiButton.vue'
const emit = defineEmits<{ stop: [] }>()
</script>
<style scoped>
.playtest-bar { position: absolute; top: 12px; left: 50%; transform: translateX(-50%);
  z-index: 30; display: flex; align-items: center; gap: 10px; padding: 6px 12px;
  background: rgba(3,8,14,0.92); border: 1px solid rgba(215,187,132,0.5); border-radius: 10px; color: #f4d27a; }
.playtest-bar__label { font-size: 0.75rem; }
</style>
```
(Milestone-1: a single Stop-and-reset control satisfies "pause resets". A true pause-without-teardown is a later refinement — note it. Objective/victory feedback is whatever the match renderer already shows.)

- [ ] **Step 5: Wire into `WorldEditorPanel.vue`** — add a hidden play canvas (`<canvas ref="playCanvas" v-show="playing" class="we-play-canvas">`) overlaying the editor canvas area; `const { playing, start, stop } = usePlaytest(() => playCanvas.value)`. Toolbar `play` action → `start(exportedCatalogFile.value)`; when `playing`, hide the editor canvas + show the play canvas + `PlaytestBar` (`@stop="stop()"`). On stop, `v-show` flips back to the editor canvas — the editor `model` is untouched so it re-renders the pre-play placement. Ensure the play canvas has real layout size (the `CanvasRenderer` reads `clientWidth/Height`).

- [ ] **Step 6: Test + build + manual E2E + commit**

Run: `cd client/src/game-portal && npm run test -- usePlaytest && npm run build`
Expected: PASS + clean.
Manual E2E (the milestone's proof): drop two hostile units → give one rank/items/perks → toolbar ▶ Play → the canvas becomes a live match, the units fight, objectives (if any) evaluate → Stop → the units snap back to placement; verify (server logs / profile) NO Dominion Points were awarded.
```bash
git add client/src/game-portal/src/components/world-editor/usePlaytest.ts client/src/game-portal/src/components/world-editor/usePlaytest.test.ts client/src/game-portal/src/components/world-editor/PlaytestBar.vue client/src/game-portal/src/components/world-editor/WorldEditorPanel.vue
git commit -m "World editor: ephemeral Play/reset harness"
```

---

### Task 9: Verification sweep

**Files:** fixes only.

- [ ] **Step 1: Full gates**

Run: `cd server && go vet ./... && go build ./... && go test ./... -count=1 2>&1 | grep -E "^(--- FAIL|FAIL)"`
Expected: only the known pre-existing failures (Global Constraints).
Run: `cd client/src/game-portal && npm run test && npm run build`
Expected: green + clean.

- [ ] **Step 2: Old-editor untouched check**

Run: `cd "$(git rev-parse --show-toplevel)" && git diff --stat main..world-editor -- client/src/game-portal/src/components/MapEditorPanel.vue client/src/game-portal/src/views/Editor.vue`
Expected: NO changes to either file (the old map editor is untouched).

- [ ] **Step 3: Catalog hygiene** — manual playtesting writes real catalog/map files in dev; `git status` must show no stray `server/internal/game/catalog/` or scratch-map changes committed. Revert any (`git checkout -- server/internal/game/catalog/`). The `__world_editor_scratch__` map file, if written to the maps dir during manual testing, should NOT be committed — add it to the revert.

- [ ] **Step 4: Commit any fixes**
```bash
git add -A client/ server/
git commit -m "World editor shell: verification fixes"
```
(Skip if clean.)
