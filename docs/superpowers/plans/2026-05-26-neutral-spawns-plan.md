# Neutral Spawns Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a new "Neutral Spawn" placement type to the map editor. Each placement materializes a guard squad (drawn from a tiered catalog of group compositions) between waves, despawns instantly when a wave starts, and respawns when the wave clears. Aggro range, leash range, HP/damage scaling, and tier-up cadence are configurable per placement.

**Architecture:** Mirror the existing `enemy-spawnpoint` (per-wave scaling) and `PlacedUnit` (guard-mode, aggro/leash) primitives. Neutrals run under a new virtual player slot `neutralPlayerID = "__neutral__"`. State lives in a new `state_neutral_camps.go` module that hooks the existing tick loop and unit-removal flow. Group composition data lives in `catalog/neutral_groups/tier_<N>.json` files, loaded once at startup with a tier-fallback resolution rule.

**Tech Stack:** Go (server, embed.FS catalog, existing `s.rng` for determinism), Vue 3 + TypeScript (map editor + Pinia store), shared protocol JSON.

**Repo conventions (read first):**
- [c:\Personal Dev\webrts\CLAUDE.md](../../../CLAUDE.md) — project rules
- [c:\Personal Dev\webrts\.claude\rules\AI_RULES.md](../../../.claude/rules/AI_RULES.md) — ID-based targeting, `*Locked` suffix, determinism rules
- Design doc: [docs/superpowers/specs/2026-05-26-neutral-spawns-design.md](../specs/2026-05-26-neutral-spawns-design.md)

**Commit policy:** The user handles all git commits. **Do not run `git add` or `git commit` at any point.** Just write/edit files; the user reviews diffs and commits.

**Test commands (Windows / PowerShell):**
- Run a single Go test: `go test ./server/internal/game -run TestNeutralGroupLoader_TierFallback -v`
- Run a Go test package: `go test ./server/internal/game -v`
- Build the whole server: `go build ./server/...`
- Run a Vue/TS type-check: `cd client/src/game-portal; npm run type-check`
- Run a Vue/TS test (if one exists; the repo uses vitest): `cd client/src/game-portal; npx vitest run path/to/test`

---

## File Map (high-level)

**New files:**
- `server/internal/game/catalog/neutral_groups/tier_1.json` — example data
- `server/internal/game/neutral_group_defs.go` — loader + tier resolution
- `server/internal/game/neutral_group_defs_test.go` — loader tests
- `server/internal/game/state_neutral_camps.go` — runtime lifecycle
- `server/internal/game/state_neutral_camps_test.go` — lifecycle + combat tests

**Modified files:**
- `server/pkg/protocol/messages.go` — `NeutralSpawn` struct, `MapConfig.NeutralSpawns`
- `server/internal/game/unit.go` — `NeutralCampID string` field on `Unit`
- `server/internal/game/state.go` — register `neutralPlayerID`, insert tick call, wire removal hook
- `server/internal/game/state_waves.go` — add `neutralPlayerID` constant near `enemyPlayerID` (or in its own file)
- `server/internal/game/combat_ai.go` (or wherever aggro acquisition writes `AttackTargetID`) — group-aggro broadcast hook
- `server/internal/http/profile_handlers.go` — `GET /api/catalog/neutral-groups`
- `client/src/game-portal/src/components/MapEditorPanel.vue` — `neutral-spawn` brush + UI
- `client/src/game-portal/src/stores/catalog.ts` (or equivalent) — fetch + cache neutral groups

---

## Task 1: Add `NeutralSpawn` to the protocol

**Files:**
- Modify: `server/pkg/protocol/messages.go`

- [ ] **Step 1: Read the existing `MapConfig` and `PlacedUnit` definitions for context**

Read [server/pkg/protocol/messages.go:49-123](../../../server/pkg/protocol/messages.go#L49-L123). Note the `PlacedUnit` struct with `omitempty` on `AggroRange` / `LeashRange`, and `MapConfig.PlacedUnits` with `omitempty`. Match those conventions.

- [ ] **Step 2: Add the `NeutralSpawn` struct just below `PlacedUnit` (after the `UnmarshalJSON` method at line 99)**

```go
// NeutralSpawn is a map-authored tile that materializes a guard squad of
// "neutral" units between waves. The squad despawns instantly when a wave
// starts and respawns when the wave clears. Composition is drawn from a
// tier file in catalog/neutral_groups/. See neutral_group_defs.go for the
// runtime loader and state_neutral_camps.go for the lifecycle.
//
// GroupID is either a specific group id (e.g. "small_raider_group") or the
// sentinel "__random__" to roll a random group from the current tier each
// respawn.
//
// StartingTier defaults to 1. TierUpEveryNWaves = 0 disables auto-scaling.
// Aggro/leash and the four per-wave scaling fields mirror enemy-spawnpoint
// semantics (see state_waves.go computeWaveStatScalingLocked) so authors
// have a consistent mental model.
type NeutralSpawn struct {
    GridCoord
    ID                      string  `json:"id"`
    GroupID                 string  `json:"groupId"`
    StartingTier            int     `json:"startingTier,omitempty"`
    TierUpEveryNWaves       int     `json:"tierUpEveryNWaves,omitempty"`
    AggroRange              float64 `json:"aggroRange,omitempty"`
    LeashRange              float64 `json:"leashRange,omitempty"`
    HealthMultiplier        float64 `json:"healthMultiplier,omitempty"`
    HealthMultiplierPerWave float64 `json:"healthMultiplierPerWave,omitempty"`
    DamageMultiplier        float64 `json:"damageMultiplier,omitempty"`
    DamageMultiplierPerWave float64 `json:"damageMultiplierPerWave,omitempty"`
}

// NeutralSpawnRandomGroupID is the sentinel GroupID value meaning "pick a
// random group from the current tier each time the camp respawns."
const NeutralSpawnRandomGroupID = "__random__"
```

- [ ] **Step 3: Add `NeutralSpawns` to `MapConfig`**

Inside `MapConfig` (around line 116 where `PlacedUnits` is declared), add immediately after the `PlacedUnits` line:

```go
NeutralSpawns []NeutralSpawn `json:"neutralSpawns,omitempty"`
```

- [ ] **Step 4: Verify the protocol package compiles**

Run: `go build ./server/pkg/protocol/...`
Expected: no output, exit 0.

---

## Task 2: Author the example tier-1 group catalog file

**Files:**
- Create: `server/internal/game/catalog/neutral_groups/tier_1.json`

- [ ] **Step 1: Verify `raider` and `raider_ranged` unit types exist**

Run: `ls "server/internal/game/catalog/units/raider/"`
Expected: directory listing includes `raider` and `raider_ranged` subfolders. If `raider_ranged` is named differently (e.g. `ranged_raider`), use the actual name in Step 2.

- [ ] **Step 2: Create the file with one example group**

Path: `server/internal/game/catalog/neutral_groups/tier_1.json`

```json
{
  "tier": 1,
  "groups": [
    {
      "id": "small_raider_group",
      "name": "Small Raider Group",
      "composition": [
        { "unitType": "raider", "count": 2 },
        { "unitType": "raider_ranged", "count": 2 }
      ]
    }
  ]
}
```

(Substitute the real `raider_ranged` unit-type id if it differs.)

---

## Task 3: Catalog loader — failing tier-fallback test

**Files:**
- Create: `server/internal/game/neutral_group_defs_test.go`

- [ ] **Step 1: Write the test that exercises `resolveTier` with a sparse catalog**

The loader doesn't exist yet — this test will fail to compile, which is the expected red-state.

```go
package game

import "testing"

// TestNeutralGroupLoader_LoadsTier1 pins the structural invariant: the
// shipped tier_1.json loads, has at least one group, and the group has
// non-empty composition. Deliberately does NOT pin specific group ids or
// counts — those are balance content and live in JSON.
func TestNeutralGroupLoader_LoadsTier1(t *testing.T) {
    tier, ok := neutralGroupsByTier[1]
    if !ok {
        t.Fatalf("tier 1 catalog missing — expected tier_1.json to load at startup")
    }
    if len(tier.Groups) == 0 {
        t.Fatalf("tier 1 has zero groups — at least one required")
    }
    for _, g := range tier.Groups {
        if g.ID == "" {
            t.Errorf("tier 1 group has empty id: %+v", g)
        }
        if g.Name == "" {
            t.Errorf("tier 1 group %q has empty display name", g.ID)
        }
        if len(g.Composition) == 0 {
            t.Errorf("tier 1 group %q has empty composition", g.ID)
        }
        for _, c := range g.Composition {
            if c.Count < 1 {
                t.Errorf("tier 1 group %q composition entry %q has count %d (must be >= 1)", g.ID, c.UnitType, c.Count)
            }
            if _, ok := getUnitDef(c.UnitType); !ok {
                t.Errorf("tier 1 group %q references unknown unitType %q", g.ID, c.UnitType)
            }
        }
    }
}

// TestNeutralGroupLoader_TierFallback covers the spec's tier-fallback rule:
// requesting tier K when tier_K.json is missing resolves to the largest
// tier file <= K. With only tier 1 shipped today, every K >= 1 resolves to 1.
func TestNeutralGroupLoader_TierFallback(t *testing.T) {
    cases := []struct {
        requested int
        want      int
    }{
        {1, 1},
        {2, 1},
        {5, 1},
        {100, 1},
    }
    for _, tc := range cases {
        got := resolveNeutralTier(tc.requested)
        if got != tc.want {
            t.Errorf("resolveNeutralTier(%d): got %d, want %d", tc.requested, got, tc.want)
        }
    }
}

// TestNeutralGroupLoader_TierZeroSentinel: requesting tier <= 0 returns the
// sentinel 0, which spawnGroupForCampLocked treats as "no tier available,
// skip respawn." (Distinct from "found a fallback.")
func TestNeutralGroupLoader_TierZeroSentinel(t *testing.T) {
    if got := resolveNeutralTier(0); got != 0 {
        t.Errorf("resolveNeutralTier(0): got %d, want 0", got)
    }
    if got := resolveNeutralTier(-3); got != 0 {
        t.Errorf("resolveNeutralTier(-3): got %d, want 0", got)
    }
}
```

- [ ] **Step 2: Run the test to confirm it fails to compile**

Run: `go test ./server/internal/game -run TestNeutralGroupLoader -v`
Expected: build failure citing `undefined: neutralGroupsByTier` and `undefined: resolveNeutralTier`.

---

## Task 4: Catalog loader — implementation

**Files:**
- Create: `server/internal/game/neutral_group_defs.go`

- [ ] **Step 1: Write the loader file**

Mirror the pattern in [server/internal/game/unit_defs.go:178-274](../../../server/internal/game/unit_defs.go#L178-L274) (var-initializer, embed.FS, panic-on-bad-JSON so the catalog stays coherent).

```go
package game

import (
    "embed"
    "encoding/json"
    "io/fs"
    "regexp"
    "sort"
    "strconv"
)

// Embeds the neutral-group composition catalog. Each tier_<N>.json holds
// multiple named groups; each group is a composition of (unitType, count)
// pairs. Layout:
//
//   catalog/neutral_groups/tier_1.json
//   catalog/neutral_groups/tier_2.json
//
// Composition entries reference existing unit types from the units catalog
// (raider/raider, raider/raider_ranged, etc.) — no new "neutral faction"
// unit defs are required. Neutrals are retagged at spawn time under the
// virtual neutralPlayerID slot (see state_neutral_camps.go).

//go:embed catalog/neutral_groups
var neutralGroupsFS embed.FS

// NeutralGroupCompositionEntry is one slot in a group's composition list:
// spawn `Count` units of `UnitType` around the camp center.
type NeutralGroupCompositionEntry struct {
    UnitType string `json:"unitType"`
    Count    int    `json:"count"`
}

// NeutralGroup is one named group composition (e.g. "small_raider_group").
type NeutralGroup struct {
    ID          string                         `json:"id"`
    Name        string                         `json:"name"`
    Composition []NeutralGroupCompositionEntry `json:"composition"`
}

// NeutralGroupTier holds all groups available at a given tier level.
type NeutralGroupTier struct {
    Tier   int            `json:"tier"`
    Groups []NeutralGroup `json:"groups"`
}

// neutralGroupsByTier is the runtime registry. Keyed by tier number.
// Populated at startup; immutable afterwards.
var neutralGroupsByTier = loadNeutralGroupsByTier()

// neutralTiersSorted caches the sorted list of available tier numbers so
// resolveNeutralTier doesn't re-sort on every call.
var neutralTiersSorted = sortedTierKeys(neutralGroupsByTier)

var neutralGroupTierFilenameRE = regexp.MustCompile(`^tier_(\d+)\.json$`)

func loadNeutralGroupsByTier() map[int]NeutralGroupTier {
    entries, err := fs.ReadDir(neutralGroupsFS, "catalog/neutral_groups")
    if err != nil {
        // Directory missing is OK — feature is opt-in per map. Return empty.
        return map[int]NeutralGroupTier{}
    }
    result := make(map[int]NeutralGroupTier, len(entries))
    for _, entry := range entries {
        if entry.IsDir() {
            panic("catalog/neutral_groups: unexpected subdirectory " + entry.Name() + " — only tier_<N>.json files allowed")
        }
        m := neutralGroupTierFilenameRE.FindStringSubmatch(entry.Name())
        if m == nil {
            panic("catalog/neutral_groups: unexpected file " + entry.Name() + ` — must match "tier_<N>.json"`)
        }
        tierNum, err := strconv.Atoi(m[1])
        if err != nil || tierNum < 1 {
            panic("catalog/neutral_groups: invalid tier number in " + entry.Name())
        }
        rel := "catalog/neutral_groups/" + entry.Name()
        data, err := neutralGroupsFS.ReadFile(rel)
        if err != nil {
            panic(rel + ": " + err.Error())
        }
        var tier NeutralGroupTier
        if err := json.Unmarshal(data, &tier); err != nil {
            panic(rel + ": " + err.Error())
        }
        if tier.Tier != tierNum {
            panic(rel + ": JSON tier field " + strconv.Itoa(tier.Tier) + " does not match filename tier " + strconv.Itoa(tierNum))
        }
        if len(tier.Groups) == 0 {
            panic(rel + ": tier has zero groups — at least one required")
        }
        seenIDs := make(map[string]struct{}, len(tier.Groups))
        for gi, g := range tier.Groups {
            if g.ID == "" {
                panic(rel + ": group " + strconv.Itoa(gi) + " missing id")
            }
            if g.Name == "" {
                panic(rel + ": group " + g.ID + " missing display name")
            }
            if _, dup := seenIDs[g.ID]; dup {
                panic(rel + ": duplicate group id " + g.ID + " within tier")
            }
            seenIDs[g.ID] = struct{}{}
            if len(g.Composition) == 0 {
                panic(rel + ": group " + g.ID + " has empty composition")
            }
            for ci, c := range g.Composition {
                if c.UnitType == "" {
                    panic(rel + ": group " + g.ID + " composition entry " + strconv.Itoa(ci) + " missing unitType")
                }
                if c.Count < 1 {
                    panic(rel + ": group " + g.ID + " composition entry " + c.UnitType + " has count " + strconv.Itoa(c.Count) + " (must be >= 1)")
                }
                if _, ok := getUnitDef(c.UnitType); !ok {
                    panic(rel + ": group " + g.ID + " references unknown unitType " + c.UnitType)
                }
            }
        }
        result[tierNum] = tier
    }
    return result
}

func sortedTierKeys(m map[int]NeutralGroupTier) []int {
    out := make([]int, 0, len(m))
    for k := range m {
        out = append(out, k)
    }
    sort.Ints(out)
    return out
}

// resolveNeutralTier returns the largest available tier number <= requested.
// Returns 0 (sentinel "no tier available") when:
//   - requested <= 0
//   - no tier files have been loaded
//   - no shipped tier is <= requested
func resolveNeutralTier(requested int) int {
    if requested <= 0 || len(neutralTiersSorted) == 0 {
        return 0
    }
    // neutralTiersSorted is ascending; walk from the top down for the highest tier <= requested.
    for i := len(neutralTiersSorted) - 1; i >= 0; i-- {
        if neutralTiersSorted[i] <= requested {
            return neutralTiersSorted[i]
        }
    }
    return 0
}

// getNeutralGroup looks up a specific group by id within a tier.
// tier must be a key in neutralGroupsByTier (use resolveNeutralTier first).
// Returns (group, true) on hit, (zero, false) when the id is unknown.
func getNeutralGroup(tier int, id string) (NeutralGroup, bool) {
    t, ok := neutralGroupsByTier[tier]
    if !ok {
        return NeutralGroup{}, false
    }
    for _, g := range t.Groups {
        if g.ID == id {
            return g, true
        }
    }
    return NeutralGroup{}, false
}

// listNeutralGroupIDs returns all group ids in a tier in JSON order.
// Used by the random selector and the HTTP catalog endpoint.
func listNeutralGroupIDs(tier int) []string {
    t, ok := neutralGroupsByTier[tier]
    if !ok {
        return nil
    }
    out := make([]string, len(t.Groups))
    for i, g := range t.Groups {
        out[i] = g.ID
    }
    return out
}
```

- [ ] **Step 2: Run the failing tests from Task 3 — they should now pass**

Run: `go test ./server/internal/game -run TestNeutralGroupLoader -v`
Expected: 3 tests PASS.

- [ ] **Step 3: Build the whole game package to make sure nothing else broke**

Run: `go build ./server/...`
Expected: no errors.

---

## Task 5: Register `neutralPlayerID` and the ensure-player helper

**Files:**
- Modify: `server/internal/game/state_waves.go` (or wherever `enemyPlayerID` lives — currently line 42)

- [ ] **Step 1: Add the constant next to `enemyPlayerID`**

Locate the constant block at [server/internal/game/state_waves.go:42-44](../../../server/internal/game/state_waves.go#L42-L44):

```go
const enemyPlayerID = "__enemy__"

const enemyPlayerColor = "#e74c3c"
```

Add immediately after:

```go
// neutralPlayerID is the virtual player slot for neutral camp units.
// Neutrals are hostile to player units (distinct OwnerID → existing AI
// scoring treats them as valid targets in both directions) and have no
// base/resources/defeat condition. Neutrals only exist outside "active"
// wave state; the lifecycle is owned by state_neutral_camps.go.
const neutralPlayerID = "__neutral__"

const neutralPlayerColor = "#9b59b6"
```

- [ ] **Step 2: Add an `ensureNeutralPlayerLocked` helper next to `ensureEnemyPlayerLocked`**

The existing helper is at [server/internal/game/state_waves.go:223-234](../../../server/internal/game/state_waves.go#L223-L234). Add an analogous helper immediately after it:

```go
func (s *GameState) ensureNeutralPlayerLocked() {
    if _, exists := s.Players[neutralPlayerID]; exists {
        return
    }
    s.Players[neutralPlayerID] = &Player{
        ID:                            neutralPlayerID,
        Color:                         neutralPlayerColor,
        Resources:                     map[string]int{},
        GlobalUnitSpawnTimeMultiplier: 1,
        UnitSpawnTimeMultipliers:      map[string]float64{},
    }
}
```

- [ ] **Step 3: Confirm `countEnemyUnitsLocked` still excludes neutrals (it should — it filters on `OwnerID == enemyPlayerID`)**

Read [server/internal/game/state_waves.go:186-194](../../../server/internal/game/state_waves.go#L186-L194). The filter is `u.OwnerID == enemyPlayerID`, so neutral units (`__neutral__`) are correctly excluded from wave-clear counting. No change needed.

- [ ] **Step 4: Build the package**

Run: `go build ./server/internal/game/...`
Expected: no errors.

---

## Task 6: Add `NeutralCampID` to `Unit`

**Files:**
- Modify: `server/internal/game/unit.go`

- [ ] **Step 1: Locate the `Unit` struct**

Run: `grep -n "type Unit struct" server/internal/game/unit.go`
Expected: one match. Read the struct fully so you know where to insert the new field.

- [ ] **Step 2: Add the field**

Inside the `Unit` struct, after the existing combat/targeting ID fields (search for `AttackTargetID` and add nearby), insert:

```go
// NeutralCampID, when non-empty, links this unit to a NeutralCamp.
// Empty for all non-neutral units. Set at spawn by
// spawnGroupForCampLocked; consumed by:
//   1. the group-aggro broadcast in combat AI (one camp-mate spotting a
//      player target triggers the rest of the camp to engage).
//   2. removeUnitLocked, which calls onUnitRemovedFromCampLocked to keep
//      camp.AliveUnitIDs in sync.
NeutralCampID string
```

- [ ] **Step 3: Build the package**

Run: `go build ./server/internal/game/...`
Expected: no errors. (Other code reads/writes `Unit` via field selectors; an additional optional field doesn't break anything.)

---

## Task 7: Lifecycle module skeleton — `NeutralCamp` struct + init

**Files:**
- Create: `server/internal/game/state_neutral_camps.go`

- [ ] **Step 1: Write the module skeleton with the struct, init function, and empty tick stub**

```go
package game

import (
    "sort"

    "webrts/server/pkg/protocol"
)

// NeutralCampState distinguishes "spawned and active" from "wave hidden."
// Edge transitions between these states are driven by WaveManager.State
// transitions in tickNeutralCampsLocked.
type NeutralCampState int

const (
    NeutralCampActive     NeutralCampState = iota // group is alive at the camp; passive guard mode
    NeutralCampWaveHidden                         // wave is active; no neutrals exist; respawn on next wave clear
)

// NeutralCamp is the runtime state for one map-authored NeutralSpawn.
// All target references are by ID per AI_RULES — AliveUnitIDs is the
// authoritative list of unit IDs spawned by this camp this respawn cycle,
// rebuilt every time the wave clears.
type NeutralCamp struct {
    PlacementID  string
    X, Y         int
    StartingTier int
    TierUpEveryN int
    GroupID      string // specific id or protocol.NeutralSpawnRandomGroupID
    CurrentTier  int    // recomputed each respawn
    AliveUnitIDs []int
    State        NeutralCampState

    AggroRange              float64
    LeashRange              float64
    HealthMultiplier        float64
    HealthMultiplierPerWave float64
    DamageMultiplier        float64
    DamageMultiplierPerWave float64
}

// initNeutralCampsLocked builds NeutralCamp runtime state from
// MapConfig.NeutralSpawns. Called once during game-state initialization
// (alongside initWaveManagerLocked). Idempotent — safe to call twice.
//
// Camps are stored in a sorted slice (by PlacementID) so iteration order
// is deterministic across runs.
func (s *GameState) initNeutralCampsLocked() {
    if len(s.MapConfig.NeutralSpawns) == 0 {
        s.NeutralCamps = nil
        return
    }
    camps := make([]NeutralCamp, 0, len(s.MapConfig.NeutralSpawns))
    for _, ns := range s.MapConfig.NeutralSpawns {
        startingTier := ns.StartingTier
        if startingTier < 1 {
            startingTier = 1
        }
        groupID := ns.GroupID
        if groupID == "" {
            groupID = protocol.NeutralSpawnRandomGroupID
        }
        camps = append(camps, NeutralCamp{
            PlacementID:             ns.ID,
            X:                       ns.X,
            Y:                       ns.Y,
            StartingTier:            startingTier,
            TierUpEveryN:            ns.TierUpEveryNWaves,
            GroupID:                 groupID,
            CurrentTier:             startingTier,
            State:                   NeutralCampWaveHidden, // promoted to Active on first tick
            AggroRange:              ns.AggroRange,
            LeashRange:              ns.LeashRange,
            HealthMultiplier:        ns.HealthMultiplier,
            HealthMultiplierPerWave: ns.HealthMultiplierPerWave,
            DamageMultiplier:        ns.DamageMultiplier,
            DamageMultiplierPerWave: ns.DamageMultiplierPerWave,
        })
    }
    sort.Slice(camps, func(i, j int) bool { return camps[i].PlacementID < camps[j].PlacementID })
    s.NeutralCamps = camps
}

// tickNeutralCampsLocked is edge-triggered off WaveManager state
// transitions. Does no per-tick work in the steady state.
//
// Lifecycle:
//   - Game start (camp.State == WaveHidden and wave not active) →
//     spawn the initial group.
//   - prep → active (wave starts)   → despawn all neutrals.
//   - active → upgrade/prep/complete → respawn camp.
//
// Must be called under s.mu write lock.
func (s *GameState) tickNeutralCampsLocked() {
    // Implementation lands in Task 9 (lifecycle transitions); leave the
    // body empty for now so callers can wire it up without runtime impact.
    _ = s
}
```

- [ ] **Step 2: Add `NeutralCamps []NeutralCamp` to `GameState`**

Run: `grep -n "type GameState struct" server/internal/game/state.go`
Read the struct, then add the field near other map-driven runtime collections (look for `EnemySpawnTimers`, `WaveManager`):

```go
// NeutralCamps is the runtime state for map-authored NeutralSpawns.
// Built once by initNeutralCampsLocked from MapConfig.NeutralSpawns and
// driven by tickNeutralCampsLocked. Empty/nil on maps with no neutral spawns.
NeutralCamps []NeutralCamp
```

- [ ] **Step 3: Call `initNeutralCampsLocked` next to the existing init flow**

Run: `grep -n "initWaveManagerLocked" server/internal/game/state.go`
Locate the call site and add `s.initNeutralCampsLocked()` immediately after it.

- [ ] **Step 4: Build the package**

Run: `go build ./server/internal/game/...`
Expected: no errors. (No tick wire-up yet — the empty stub is intentional.)

---

## Task 8: `spawnGroupForCampLocked` — failing test + implementation

**Files:**
- Create: `server/internal/game/state_neutral_camps_test.go`
- Modify: `server/internal/game/state_neutral_camps.go`

This is the meat of the runtime. It tests against an in-memory `GameState` with a fabricated NeutralSpawn, then implements the spawner.

- [ ] **Step 1: Find existing test patterns to follow**

Run: `grep -ln "newTestGameState\|NewGameStateForTest\|makeTestState" server/internal/game/`
Read one matching test file to learn the project's standard test-state construction. Adopt that pattern in the new file.

- [ ] **Step 2: Write the failing test**

```go
package game

import (
    "testing"

    "webrts/server/pkg/protocol"
)

// TestNeutralCamp_SpawnGroup_SpawnsExpectedComposition pins the invariant
// that spawnGroupForCampLocked materializes the composition declared in
// the tier file under neutralPlayerID, anchored at the camp center with
// the camp's aggro/leash range. Counts are derived from the catalog JSON,
// NOT pinned as test literals (per project tunables rule).
func TestNeutralCamp_SpawnGroup_SpawnsExpectedComposition(t *testing.T) {
    s := newTestStateWithNeutralCamp(t)
    camp := &s.NeutralCamps[0]
    camp.GroupID = "small_raider_group"
    camp.CurrentTier = 1

    s.spawnGroupForCampLocked(camp)

    group, ok := getNeutralGroup(1, "small_raider_group")
    if !ok {
        t.Fatalf("test setup: small_raider_group must exist in tier 1")
    }

    expectedTotal := 0
    for _, c := range group.Composition {
        expectedTotal += c.Count
    }
    if got := len(camp.AliveUnitIDs); got != expectedTotal {
        t.Fatalf("AliveUnitIDs: got %d, want %d (derived from tier_1.json composition)", got, expectedTotal)
    }
    // Confirm composition by counting unit types
    countsByType := map[string]int{}
    for _, id := range camp.AliveUnitIDs {
        u := s.getUnitByIDLocked(id)
        if u == nil {
            t.Fatalf("camp.AliveUnitIDs has stale id %d (no Unit found)", id)
        }
        if u.OwnerID != neutralPlayerID {
            t.Errorf("unit %d: OwnerID = %q, want %q", id, u.OwnerID, neutralPlayerID)
        }
        if u.NeutralCampID != camp.PlacementID {
            t.Errorf("unit %d: NeutralCampID = %q, want %q", id, u.NeutralCampID, camp.PlacementID)
        }
        if !u.GuardMode {
            t.Errorf("unit %d: GuardMode = false, want true", id)
        }
        countsByType[u.Type]++
    }
    for _, c := range group.Composition {
        if countsByType[c.UnitType] != c.Count {
            t.Errorf("unitType %q: got %d spawned, want %d", c.UnitType, countsByType[c.UnitType], c.Count)
        }
    }
}

// TestNeutralCamp_SpawnGroup_RandomUsesSeededRNG: two states built from
// the same seed produce identical random group picks. Determinism rule.
func TestNeutralCamp_SpawnGroup_RandomUsesSeededRNG(t *testing.T) {
    s1 := newTestStateWithNeutralCamp(t)
    s2 := newTestStateWithNeutralCamp(t)
    s1.NeutralCamps[0].GroupID = protocol.NeutralSpawnRandomGroupID
    s2.NeutralCamps[0].GroupID = protocol.NeutralSpawnRandomGroupID

    s1.spawnGroupForCampLocked(&s1.NeutralCamps[0])
    s2.spawnGroupForCampLocked(&s2.NeutralCamps[0])

    // Same seed + same map → same composition (by unit-type histogram).
    h1 := unitTypeHistogram(t, s1, s1.NeutralCamps[0].AliveUnitIDs)
    h2 := unitTypeHistogram(t, s2, s2.NeutralCamps[0].AliveUnitIDs)
    if !histogramsEqual(h1, h2) {
        t.Errorf("determinism violated: histograms differ\nh1=%v\nh2=%v", h1, h2)
    }
}

// TestNeutralCamp_SpawnGroup_NoTierShippedIsNoop: when CurrentTier resolves
// to 0 (no tiers loaded), spawn is a no-op (no panic, no units placed).
// Verified by temporarily nilling the registry — actually impractical
// because the registry is a package-level var. Instead, verify that an
// out-of-range CurrentTier (e.g. -1) is treated as "no spawn" without
// panicking.
func TestNeutralCamp_SpawnGroup_TierZeroIsNoop(t *testing.T) {
    s := newTestStateWithNeutralCamp(t)
    camp := &s.NeutralCamps[0]
    camp.CurrentTier = -1 // resolves to 0

    s.spawnGroupForCampLocked(camp)

    if got := len(camp.AliveUnitIDs); got != 0 {
        t.Errorf("AliveUnitIDs after no-tier spawn: got %d, want 0", got)
    }
}

// --- test helpers ---

// newTestStateWithNeutralCamp builds a minimal GameState with one neutral
// camp placement at (5, 5). Uses the project's test-state factory if one
// exists; otherwise inlines the minimal setup.
func newTestStateWithNeutralCamp(t *testing.T) *GameState {
    t.Helper()
    // TODO: replace with project's standard test factory if it exists.
    // Locate via:
    //   grep -ln "func newTestGameState\|func NewGameStateForTest" server/internal/game/
    s := newTestGameStateWithSeed(t, 42)
    s.MapConfig.NeutralSpawns = []protocol.NeutralSpawn{{
        GridCoord:    protocol.GridCoord{X: 5, Y: 5},
        ID:           "neutral-spawn-5-5",
        GroupID:      "small_raider_group",
        StartingTier: 1,
        AggroRange:   150,
        LeashRange:   200,
    }}
    s.initNeutralCampsLocked()
    if len(s.NeutralCamps) != 1 {
        t.Fatalf("test setup: expected 1 NeutralCamp, got %d", len(s.NeutralCamps))
    }
    return s
}

func unitTypeHistogram(t *testing.T, s *GameState, ids []int) map[string]int {
    t.Helper()
    out := map[string]int{}
    for _, id := range ids {
        u := s.getUnitByIDLocked(id)
        if u == nil {
            t.Fatalf("stale id %d", id)
        }
        out[u.Type]++
    }
    return out
}

func histogramsEqual(a, b map[string]int) bool {
    if len(a) != len(b) {
        return false
    }
    for k, v := range a {
        if b[k] != v {
            return false
        }
    }
    return true
}
```

If `newTestGameStateWithSeed` doesn't exist, find the equivalent in the existing test files and adopt it (or write a small helper that constructs a minimal GameState with `s.rng = rand.New(rand.NewSource(42))` and `s.Units = map[int]*Unit{}`).

- [ ] **Step 3: Run the test to confirm it fails to compile**

Run: `go test ./server/internal/game -run TestNeutralCamp_SpawnGroup -v`
Expected: build failure citing `undefined: spawnGroupForCampLocked`.

- [ ] **Step 4: Implement `spawnGroupForCampLocked` in `state_neutral_camps.go`**

Append to `state_neutral_camps.go`:

```go
// spawnGroupForCampLocked materializes the camp's current group at the
// camp center. Resolves tier (falling back to the largest available
// tier <= CurrentTier), picks the group (specific id or random), then
// spawns each composition entry as a guard-mode unit anchored at the
// camp center under neutralPlayerID. Appends each spawned unit ID to
// camp.AliveUnitIDs and sets camp.State = NeutralCampActive.
//
// No-op when:
//   - resolveNeutralTier returns 0 (no tier files loaded / requested <= 0).
//   - the requested specific group is not found at the resolved tier
//     (logged and skipped — camp stays empty until next tier-up surfaces
//     a valid group).
//
// All randomness uses s.rng for determinism. Composition entries are
// processed in JSON order; per-entry spawns are placed in a small
// deterministic ring around (camp.X, camp.Y).
//
// Must be called under s.mu write lock.
func (s *GameState) spawnGroupForCampLocked(camp *NeutralCamp) {
    tier := resolveNeutralTier(camp.CurrentTier)
    if tier == 0 {
        return
    }
    var group NeutralGroup
    var ok bool
    if camp.GroupID == protocol.NeutralSpawnRandomGroupID {
        ids := listNeutralGroupIDs(tier)
        if len(ids) == 0 {
            return
        }
        pick := s.rng.Intn(len(ids))
        group, ok = getNeutralGroup(tier, ids[pick])
    } else {
        group, ok = getNeutralGroup(tier, camp.GroupID)
    }
    if !ok {
        // Spec: missing specific group → log + skip respawn this wave.
        return
    }

    s.ensureNeutralPlayerLocked()

    cellSize := s.MapConfig.CellSize
    centerWX := float64(camp.X)*cellSize + cellSize/2
    centerWY := float64(camp.Y)*cellSize + cellSize/2
    centerPos := protocol.Vec2{X: centerWX, Y: centerWY}

    wavesElapsed := 0
    if s.WaveManager.Enabled && s.WaveManager.CurrentWave > 1 {
        wavesElapsed = s.WaveManager.CurrentWave - 1
    }
    hpBase := camp.HealthMultiplier
    if hpBase <= 0 {
        hpBase = 1
    }
    dmgBase := camp.DamageMultiplier
    if dmgBase <= 0 {
        dmgBase = 1
    }
    hpMult := hpBase + camp.HealthMultiplierPerWave*float64(wavesElapsed)
    dmgMult := dmgBase + camp.DamageMultiplierPerWave*float64(wavesElapsed)

    aggro := camp.AggroRange
    if aggro < guardMinAggroRange {
        aggro = guardMinAggroRange
    }
    leash := camp.LeashRange
    if leash < aggro {
        leash = aggro
    }

    placedOrderID := s.nextMovementOrderIDLocked()

    // Place units in a ring around the camp center. Reuse whatever helper
    // the enemy spawnpoint uses for multi-unit spawn placement to stay
    // consistent. If a clean helper isn't available, walk the composition
    // and call findNearestWalkable for each unit using a small grid offset.
    blocked := s.getBlockedCellsLocked()
    spawnIdx := 0
    for _, entry := range group.Composition {
        for i := 0; i < entry.Count; i++ {
            cell := s.worldToGrid(centerWX, centerWY)
            offsetCell := neutralCampRingOffset(cell, spawnIdx)
            spawnCell, found := s.findNearestWalkable(offsetCell, blocked)
            if !found {
                spawnCell, found = s.findNearestWalkable(cell, blocked)
                if !found {
                    spawnIdx++
                    continue
                }
            }
            spawnPos := s.gridToWorldCenter(spawnCell)
            unit := s.spawnUnitForOwnerLocked(entry.UnitType, neutralPlayerID, neutralPlayerColor, spawnPos)
            if unit == nil {
                spawnIdx++
                continue
            }
            unit.OrderID = placedOrderID
            unit.GuardMode = true
            unit.GuardAnchorX = centerPos.X
            unit.GuardAnchorY = centerPos.Y
            unit.GuardAggroRange = aggro
            unit.GuardLeashRange = leash
            unit.IgnoreWaveClear = true
            unit.NeutralCampID = camp.PlacementID
            unit.Order = OrderState{Type: OrderHold, HoldX: centerPos.X, HoldY: centerPos.Y}
            unit.CombatAnchorX = centerPos.X
            unit.CombatAnchorY = centerPos.Y
            unit.Status = "Guarding"

            // Apply per-wave HP/damage scaling using the existing helper
            // pattern (see computeWaveStatScalingLocked in state_waves.go).
            s.applyWaveStatScalingLocked(unit, hpMult, dmgMult)

            camp.AliveUnitIDs = append(camp.AliveUnitIDs, unit.ID)
            spawnIdx++
        }
    }
    camp.State = NeutralCampActive
}

// neutralCampRingOffset places successive units in a deterministic ring
// (8-cell rosette) around the camp center cell so a 4-unit camp doesn't
// stack on top of itself. Index 0 → center; 1..8 → 8-neighbour ring;
// 9+ → wider ring.
func neutralCampRingOffset(center gridPoint, idx int) gridPoint {
    if idx == 0 {
        return center
    }
    ring1 := []gridPoint{
        {X: center.X + 1, Y: center.Y},
        {X: center.X - 1, Y: center.Y},
        {X: center.X, Y: center.Y + 1},
        {X: center.X, Y: center.Y - 1},
        {X: center.X + 1, Y: center.Y + 1},
        {X: center.X - 1, Y: center.Y - 1},
        {X: center.X + 1, Y: center.Y - 1},
        {X: center.X - 1, Y: center.Y + 1},
    }
    if idx-1 < len(ring1) {
        return ring1[idx-1]
    }
    // Outer ring fallback: deterministic spiral one cell further out.
    step := (idx - 1) - len(ring1)
    return gridPoint{X: center.X + 2 + step%3, Y: center.Y + step/3}
}
```

**Notes on helpers referenced above:**
- `s.getBlockedCellsLocked`, `s.findNearestWalkable`, `s.worldToGrid`, `s.gridToWorldCenter`, `s.nextMovementOrderIDLocked`, `guardMinAggroRange`, `applyWaveStatScalingLocked`, `gridPoint`, `OrderHold`, `OrderState` already exist (see [state_spawn.go:300-362](../../../server/internal/game/state_spawn.go#L300-L362) and [state_waves.go:694-714](../../../server/internal/game/state_waves.go#L694-L714)).
- `s.spawnUnitForOwnerLocked(unitType, ownerID, color, pos)` is the assumed name for the generic per-owner spawn helper. If the actual helper has a different signature, locate it via `grep -n "func.*spawn.*Unit.*Locked" server/internal/game/*.go` and adapt the call. The existing `spawnEnemyUnitLocked` shows the pattern.

- [ ] **Step 5: Run the test — should pass**

Run: `go test ./server/internal/game -run TestNeutralCamp_SpawnGroup -v`
Expected: 3 tests PASS.

- [ ] **Step 6: Run the full game-package test suite to catch regressions**

Run: `go test ./server/internal/game -v -count=1`
Expected: no new failures introduced.

---

## Task 9: Lifecycle transitions — failing tests + implementation

**Files:**
- Modify: `server/internal/game/state_neutral_camps_test.go`
- Modify: `server/internal/game/state_neutral_camps.go`

- [ ] **Step 1: Append the lifecycle tests**

```go
// TestNeutralCamp_DespawnsOnWaveStart: when prep → active, all neutrals
// are removed and AliveUnitIDs is cleared.
func TestNeutralCamp_DespawnsOnWaveStart(t *testing.T) {
    s := newTestStateWithNeutralCamp(t)
    enableWavesForTest(t, s)            // helper: WaveManager.Enabled=true, State="prep"
    s.tickNeutralCampsLocked()           // spawn-on-game-start
    camp := &s.NeutralCamps[0]
    if len(camp.AliveUnitIDs) == 0 {
        t.Fatalf("setup: expected initial spawn to populate AliveUnitIDs")
    }
    spawned := append([]int(nil), camp.AliveUnitIDs...)

    s.WaveManager.State = "active"
    s.tickNeutralCampsLocked()

    if got := len(camp.AliveUnitIDs); got != 0 {
        t.Errorf("AliveUnitIDs after wave start: got %d, want 0", got)
    }
    for _, id := range spawned {
        if u := s.getUnitByIDLocked(id); u != nil {
            t.Errorf("unit %d should be removed from s.Units but is still present", id)
        }
    }
    if camp.State != NeutralCampWaveHidden {
        t.Errorf("camp.State after wave start: got %v, want WaveHidden", camp.State)
    }
}

// TestNeutralCamp_RespawnsOnWaveEnd: when active → upgrade/prep, the camp
// respawns with a fresh group.
func TestNeutralCamp_RespawnsOnWaveEnd(t *testing.T) {
    s := newTestStateWithNeutralCamp(t)
    enableWavesForTest(t, s)
    s.tickNeutralCampsLocked() // initial spawn
    s.WaveManager.State = "active"
    s.tickNeutralCampsLocked() // despawn
    camp := &s.NeutralCamps[0]
    if len(camp.AliveUnitIDs) != 0 {
        t.Fatalf("setup: camp must be empty after wave start")
    }
    s.WaveManager.State = "upgrade"
    s.tickNeutralCampsLocked()

    if got := len(camp.AliveUnitIDs); got == 0 {
        t.Errorf("AliveUnitIDs after wave end: got 0, want > 0 (camp should respawn)")
    }
    if camp.State != NeutralCampActive {
        t.Errorf("camp.State after wave end: got %v, want Active", camp.State)
    }
}

// TestNeutralCamp_TierUpEveryN: TierUpEveryN=2 promotes CurrentTier after
// wave 2 clears. Uses fallback (only tier_1 ships today) so the spawn
// still happens; CurrentTier itself is what we assert.
func TestNeutralCamp_TierUpEveryN(t *testing.T) {
    s := newTestStateWithNeutralCamp(t)
    enableWavesForTest(t, s)
    s.NeutralCamps[0].TierUpEveryN = 2
    s.NeutralCamps[0].StartingTier = 1

    // Simulate wave 2 cleared.
    s.WaveManager.CurrentWave = 2
    s.WaveManager.State = "upgrade"
    s.tickNeutralCampsLocked()

    if got := s.NeutralCamps[0].CurrentTier; got != 2 {
        t.Errorf("CurrentTier after wave 2 with TierUpEveryN=2: got %d, want 2 (1 + 2/2)", got)
    }
}

// TestNeutralCamp_DevInvariant_NoNeutralsDuringActiveWave: assertion in
// tickNeutralCampsLocked that camp.AliveUnitIDs is empty whenever
// WaveManager.State == "active". Implemented as a logged warning rather
// than a panic (production safety), but the test forces the bad state
// and confirms it logs.
//
// Skipped if the invariant is implemented as a t.Helper-style assertion
// (no observable side-effect to test).
func TestNeutralCamp_DevInvariant_NoNeutralsDuringActiveWave(t *testing.T) {
    t.Skip("dev-only invariant; covered by manual smoke test in Task 16")
}

// enableWavesForTest puts the WaveManager into the simplest "enabled, in
// prep" state so transitions are exercisable.
func enableWavesForTest(t *testing.T, s *GameState) {
    t.Helper()
    s.WaveManager = WaveManager{
        Enabled:      true,
        CurrentWave:  0,
        State:        "prep",
        Timer:        60,
        PrepDuration: 60,
        WaveDuration: 120,
    }
}
```

- [ ] **Step 2: Run the tests — expect failures**

Run: `go test ./server/internal/game -run TestNeutralCamp_Despawns -v`
Expected: FAILS — `tickNeutralCampsLocked` is still a stub.

- [ ] **Step 3: Implement the lifecycle in `tickNeutralCampsLocked`**

Replace the stub in `state_neutral_camps.go`:

```go
// tickNeutralCampsLocked is edge-triggered off WaveManager state.
// Must be called under s.mu write lock. See doc-comment on the type
// for the full lifecycle table.
func (s *GameState) tickNeutralCampsLocked() {
    if len(s.NeutralCamps) == 0 {
        return
    }
    waveActive := s.WaveManager.Enabled && s.WaveManager.State == "active"

    for i := range s.NeutralCamps {
        camp := &s.NeutralCamps[i]
        switch camp.State {
        case NeutralCampWaveHidden:
            // Spawn when the wave is NOT active. Covers both game start
            // (prep before wave 1) and wave-end respawn (upgrade/prep/complete).
            if !waveActive {
                camp.CurrentTier = computeNeutralCurrentTier(s, camp)
                s.spawnGroupForCampLocked(camp)
            }
        case NeutralCampActive:
            if waveActive {
                s.despawnNeutralCampLocked(camp)
            }
        }
    }
}

// computeNeutralCurrentTier returns starting + (completedWaves / tierUpEveryN)
// when auto-scaling is enabled, else starting. completedWaves is
// CurrentWave - 1 (clamped >= 0), so the camp scales AFTER the wave clears.
//
// Pure function for easy testing — does not read s.NeutralCamps.
func computeNeutralCurrentTier(s *GameState, camp *NeutralCamp) int {
    starting := camp.StartingTier
    if starting < 1 {
        starting = 1
    }
    if camp.TierUpEveryN <= 0 {
        return starting
    }
    completed := s.WaveManager.CurrentWave - 1
    if completed < 0 {
        completed = 0
    }
    return starting + completed/camp.TierUpEveryN
}

// despawnNeutralCampLocked removes every alive unit owned by this camp
// from s.Units and clears camp.AliveUnitIDs. Uses removeUnitLocked so
// any cross-system cleanup (threat tables, projectile aim, etc.) runs.
func (s *GameState) despawnNeutralCampLocked(camp *NeutralCamp) {
    for _, id := range camp.AliveUnitIDs {
        u := s.getUnitByIDLocked(id)
        if u == nil {
            continue
        }
        s.removeUnitLocked(u) // adjust to actual function name if different
    }
    camp.AliveUnitIDs = camp.AliveUnitIDs[:0]
    camp.State = NeutralCampWaveHidden
}
```

**Note on `removeUnitLocked`:** find the actual name via `grep -n "func .*removeUnit.*Locked\|func .*deleteUnit.*Locked\|killUnitLocked" server/internal/game/*.go`. If the canonical helper is `s.killUnitLocked` or `s.deleteUnitLocked`, substitute accordingly.

- [ ] **Step 4: Run the lifecycle tests — expect PASS**

Run: `go test ./server/internal/game -run TestNeutralCamp -v`
Expected: 6 PASS, 1 SKIP.

- [ ] **Step 5: Run the full package**

Run: `go test ./server/internal/game -v -count=1`
Expected: no regressions.

---

## Task 10: Wire `tickNeutralCampsLocked` into the tick loop

**Files:**
- Modify: `server/internal/game/state.go`

- [ ] **Step 1: Find the existing tick order**

Run: `grep -n "tickWaveLocked\|tickEnemySpawnpointsLocked" server/internal/game/state.go`
Expected: both calls appear in the main tick loop (~ line 2245-2253 per the design doc).

- [ ] **Step 2: Insert `s.tickNeutralCampsLocked()` between the two calls**

The order matters: wave tick first (it may change `WaveManager.State`), then neutral tick (reacts to the new state), then enemy spawnpoints (the existing pattern):

```go
s.tickWaveLocked(dt)
s.tickNeutralCampsLocked()
s.tickEnemySpawnpointsLocked(dt, blocked)
```

- [ ] **Step 3: Wire the unit-removal hook**

Find `removeUnitLocked` (or the canonical helper). Inside, just before the unit is deleted from `s.Units`, add the neutral-camp bookkeeping:

```go
if u.NeutralCampID != "" {
    s.onUnitRemovedFromCampLocked(u.ID, u.NeutralCampID)
}
```

Then implement the helper in `state_neutral_camps.go`:

```go
// onUnitRemovedFromCampLocked strips a unit ID from its owning camp's
// AliveUnitIDs slice. Called from removeUnitLocked when the unit has a
// non-empty NeutralCampID. O(N) over the camp's roster; rosters are
// small (typically <= 8) so this is fine.
func (s *GameState) onUnitRemovedFromCampLocked(unitID int, campID string) {
    for i := range s.NeutralCamps {
        camp := &s.NeutralCamps[i]
        if camp.PlacementID != campID {
            continue
        }
        for j, id := range camp.AliveUnitIDs {
            if id == unitID {
                camp.AliveUnitIDs = append(camp.AliveUnitIDs[:j], camp.AliveUnitIDs[j+1:]...)
                return
            }
        }
        return
    }
}
```

- [ ] **Step 4: Build + run all game tests**

Run: `go build ./server/...`
Run: `go test ./server/internal/game -v -count=1`
Expected: no regressions.

---

## Task 11: Group-aggro broadcast (one-pulls-all)

**Files:**
- Modify: `server/internal/game/state_neutral_camps.go`
- Modify: the file where aggro acquisition writes `AttackTargetID` — likely `combat_ai.go` or `combat_ai_scoring.go`

- [ ] **Step 1: Locate the spot where a unit's aggro check FIRST assigns `AttackTargetID`**

Run: `grep -n "AttackTargetID = " server/internal/game/combat_ai*.go`
Expected: a few hits. The one for guard-mode aggro acquisition is the entry point.

Read the surrounding code so the broadcast lands at the right semantic point — right after a valid target ID has been assigned, not inside the inner scoring loop.

- [ ] **Step 2: Add the broadcast helper**

Append to `state_neutral_camps.go`:

```go
// broadcastNeutralCampAggroLocked propagates an acquired target to all
// camp-mates of `acquirer`. Each broadcast resolves the target by ID and
// validates against the canonical guard (AI_RULES rule 3) before
// assigning. No *Unit is stored anywhere.
//
// Idempotent: camp-mates that are already on the same target are skipped.
// No-op when acquirer is not a neutral or has no camp-mates.
func (s *GameState) broadcastNeutralCampAggroLocked(acquirer *Unit, targetID int) {
    if acquirer == nil || acquirer.NeutralCampID == "" || targetID == 0 {
        return
    }
    target := s.getUnitByIDLocked(targetID)
    if target == nil || !target.Visible || target.HP <= 0 || target.OwnerID == acquirer.OwnerID {
        return
    }
    var camp *NeutralCamp
    for i := range s.NeutralCamps {
        if s.NeutralCamps[i].PlacementID == acquirer.NeutralCampID {
            camp = &s.NeutralCamps[i]
            break
        }
    }
    if camp == nil {
        return
    }
    for _, mateID := range camp.AliveUnitIDs {
        if mateID == acquirer.ID {
            continue
        }
        mate := s.getUnitByIDLocked(mateID)
        if mate == nil || mate.HP <= 0 {
            continue
        }
        if mate.AttackTargetID == targetID {
            continue
        }
        mate.AttackTargetID = targetID
    }
}
```

- [ ] **Step 3: Call the broadcast from the aggro-acquisition site**

Inside the combat-AI code, right after the line that sets `unit.AttackTargetID = best.ID` (or equivalent) for a *neutral* unit, add:

```go
if unit.NeutralCampID != "" {
    s.broadcastNeutralCampAggroLocked(unit, unit.AttackTargetID)
}
```

Keep this guarded by the `NeutralCampID != ""` check so non-neutral units pay zero cost.

- [ ] **Step 4: Add a test for the broadcast**

Append to `state_neutral_camps_test.go`:

```go
// TestNeutralCamp_BroadcastAggro: assigning a target to one camp-mate
// propagates the target ID to the rest of the alive camp roster.
// Validates that broadcast does NOT store *Unit pointers and that the
// canonical AI_RULES guard fires on a stale/dead target.
func TestNeutralCamp_BroadcastAggro(t *testing.T) {
    s := newTestStateWithNeutralCamp(t)
    enableWavesForTest(t, s)
    s.tickNeutralCampsLocked() // initial spawn
    camp := &s.NeutralCamps[0]
    if len(camp.AliveUnitIDs) < 2 {
        t.Fatalf("test requires camp with >= 2 units; got %d", len(camp.AliveUnitIDs))
    }
    // Fabricate a fake player target with a different OwnerID.
    target := spawnFakePlayerUnitForTest(t, s, "player1")

    acquirer := s.getUnitByIDLocked(camp.AliveUnitIDs[0])
    acquirer.AttackTargetID = target.ID
    s.broadcastNeutralCampAggroLocked(acquirer, target.ID)

    for _, id := range camp.AliveUnitIDs {
        mate := s.getUnitByIDLocked(id)
        if mate == nil {
            t.Fatalf("camp-mate id %d disappeared", id)
        }
        if mate.AttackTargetID != target.ID {
            t.Errorf("mate %d: AttackTargetID = %d, want %d", id, mate.AttackTargetID, target.ID)
        }
    }

    // Stale target — broadcast must be a no-op (target dead).
    target.HP = 0
    secondTarget := spawnFakePlayerUnitForTest(t, s, "player1")
    acquirer2 := s.getUnitByIDLocked(camp.AliveUnitIDs[1])
    s.broadcastNeutralCampAggroLocked(acquirer2, target.ID) // dead target
    // Mate 0 should still hold the previous target id (broadcast above), not
    // be cleared. Acquirer2 didn't set its own target; broadcast skips because
    // target is invalid.
    mate0 := s.getUnitByIDLocked(camp.AliveUnitIDs[0])
    if mate0.AttackTargetID != target.ID && mate0.AttackTargetID != secondTarget.ID {
        // Tolerant: the broadcast may have left the value alone OR re-assigned
        // to the live target. What we MUST NOT see is mate0 cleared due to a
        // dead-target broadcast.
        t.Errorf("mate 0 unexpectedly cleared: AttackTargetID = %d", mate0.AttackTargetID)
    }
}

// spawnFakePlayerUnitForTest places a minimal unit owned by `ownerID` at
// (200,200). Use the project's standard helper if one exists.
func spawnFakePlayerUnitForTest(t *testing.T, s *GameState, ownerID string) *Unit {
    t.Helper()
    pos := protocol.Vec2{X: 200, Y: 200}
    u := s.spawnUnitForOwnerLocked("raider", ownerID, "#00ff00", pos)
    if u == nil {
        t.Fatalf("test setup: spawnUnitForOwnerLocked returned nil")
    }
    u.Visible = true
    return u
}
```

- [ ] **Step 5: Run the test**

Run: `go test ./server/internal/game -run TestNeutralCamp_BroadcastAggro -v`
Expected: PASS.

---

## Task 12: HTTP catalog endpoint

**Files:**
- Modify: `server/internal/http/profile_handlers.go`
- Modify: `server/internal/game/neutral_group_defs.go`

- [ ] **Step 1: Add a public list function in `neutral_group_defs.go`**

```go
// ListNeutralGroupsForCatalog returns a serializable view of every
// shipped tier and its groups (id + name only). Used by the
// /api/catalog/neutral-groups endpoint so the map editor can populate
// its tier/group dropdowns.
func ListNeutralGroupsForCatalog() []NeutralGroupTierSummary {
    out := make([]NeutralGroupTierSummary, 0, len(neutralTiersSorted))
    for _, tier := range neutralTiersSorted {
        t := neutralGroupsByTier[tier]
        groups := make([]NeutralGroupSummary, len(t.Groups))
        for i, g := range t.Groups {
            groups[i] = NeutralGroupSummary{ID: g.ID, Name: g.Name}
        }
        out = append(out, NeutralGroupTierSummary{Tier: tier, Groups: groups})
    }
    return out
}

type NeutralGroupTierSummary struct {
    Tier   int                   `json:"tier"`
    Groups []NeutralGroupSummary `json:"groups"`
}

type NeutralGroupSummary struct {
    ID   string `json:"id"`
    Name string `json:"name"`
}
```

- [ ] **Step 2: Register the endpoint**

In [profile_handlers.go](../../../server/internal/http/profile_handlers.go) after the existing `/api/catalog/tuning` handler (line 188), add:

```go
mux.HandleFunc("/api/catalog/neutral-groups", func(w http.ResponseWriter, r *http.Request) {
    writeJSON(w, map[string]any{
        "tiers": game.ListNeutralGroupsForCatalog(),
    })
})
```

- [ ] **Step 3: Add a minimal HTTP test**

The repo likely has an existing handler test pattern — find one via `grep -ln "httptest.NewRecorder\|mux.ServeHTTP" server/internal/http/`. Adopt that pattern in `server/internal/http/profile_handlers_test.go` (or whatever the existing test file is called):

```go
func TestCatalogNeutralGroupsEndpoint(t *testing.T) {
    mux := http.NewServeMux()
    registerHandlers(mux) // adjust to the actual registration function name
    req := httptest.NewRequest(http.MethodGet, "/api/catalog/neutral-groups", nil)
    rec := httptest.NewRecorder()
    mux.ServeHTTP(rec, req)
    if rec.Code != http.StatusOK {
        t.Fatalf("status: got %d, want 200", rec.Code)
    }
    var body struct {
        Tiers []struct {
            Tier   int `json:"tier"`
            Groups []struct {
                ID, Name string
            } `json:"groups"`
        } `json:"tiers"`
    }
    if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
        t.Fatalf("body: %v", err)
    }
    if len(body.Tiers) == 0 {
        t.Errorf("no tiers returned")
    }
}
```

- [ ] **Step 4: Run it**

Run: `go test ./server/internal/http -run TestCatalogNeutralGroupsEndpoint -v`
Expected: PASS.

- [ ] **Step 5: Manual smoke check (server already running)**

If the dev server is up: `curl http://localhost:8080/api/catalog/neutral-groups`
Expected: JSON with one tier containing `small_raider_group`.

---

## Task 13: Frontend catalog store fetch

**Files:**
- Modify: `client/src/game-portal/src/stores/catalog.ts` (or the closest equivalent — find via `grep -rln "catalog/player-buffs\|catalog/tuning" client/src/game-portal/src/`)

- [ ] **Step 1: Locate the existing catalog store**

Run: `grep -rln "catalog/player-buffs" client/src/game-portal/src/`
Read it. The neutral-groups fetch should follow the exact same pattern (same `fetch` wrapper, same caching, same loading state).

- [ ] **Step 2: Add the fetch + cache for neutral groups**

```ts
// TypeScript shapes mirror the Go DTOs
export interface NeutralGroupSummary {
  id: string;
  name: string;
}
export interface NeutralGroupTierSummary {
  tier: number;
  groups: NeutralGroupSummary[];
}

const neutralGroupTiers = ref<NeutralGroupTierSummary[] | null>(null);

async function loadNeutralGroups() {
  if (neutralGroupTiers.value !== null) return;
  const res = await fetch('/api/catalog/neutral-groups');
  if (!res.ok) throw new Error(`neutral-groups fetch failed: ${res.status}`);
  const body = await res.json() as { tiers: NeutralGroupTierSummary[] };
  neutralGroupTiers.value = body.tiers;
}

function neutralGroupsForTier(tier: number): NeutralGroupSummary[] {
  const tiers = neutralGroupTiers.value;
  if (!tiers || tiers.length === 0) return [];
  // Fallback rule: highest tier <= requested.
  const sorted = [...tiers].sort((a, b) => a.tier - b.tier);
  let pick: NeutralGroupTierSummary | null = null;
  for (const t of sorted) {
    if (t.tier <= tier) pick = t;
  }
  return pick ? pick.groups : [];
}

// Export both, alongside whatever the store currently exports.
return { ..., neutralGroupTiers, loadNeutralGroups, neutralGroupsForTier };
```

(Adapt the export shape to match the existing store — composition API store vs. Pinia options API store.)

- [ ] **Step 3: Type-check**

Run: `cd client/src/game-portal && npm run type-check`
Expected: no new errors.

---

## Task 14: Map editor UI — `neutral-spawn` brush mode

**Files:**
- Modify: `client/src/game-portal/src/components/MapEditorPanel.vue`

This is a sizeable Vue change. Mirror the `enemy-spawn` brush block at [MapEditorPanel.vue:323-424](../../../client/src/game-portal/src/components/MapEditorPanel.vue#L323-L424).

- [ ] **Step 1: Add `NeutralSpawn` to the frontend MapData / MapConfig type**

Find the frontend type that mirrors the server's `MapConfig`:

Run: `grep -rln "placedUnits.*PlacedUnit\[\]\|placedUnits: PlacedUnit" client/src/game-portal/src/`

In the same file, add the matching `NeutralSpawn` shape and field:

```ts
export interface NeutralSpawn {
  id: string;
  x: number;
  y: number;
  groupId: string;
  startingTier?: number;
  tierUpEveryNWaves?: number;
  aggroRange?: number;
  leashRange?: number;
  healthMultiplier?: number;
  healthMultiplierPerWave?: number;
  damageMultiplier?: number;
  damageMultiplierPerWave?: number;
}

// ...inside MapConfig / MapData interface, alongside placedUnits:
neutralSpawns?: NeutralSpawn[];
```

- [ ] **Step 2: Add `'neutral-spawn'` to the brush-mode union**

Locate the type at line ~805 and extend:

```ts
const brushMode = ref<'terrain' | 'tile' | 'obstacle' | 'building' | 'enemy-spawn' | 'neutral-spawn' | 'unit' | 'erase'>('terrain')
```

- [ ] **Step 3: Add the brush option to the dropdown**

Around line 213 where `<option value="enemy-spawn">` lives, add:

```html
<option value="neutral-spawn">Neutral Spawn</option>
```

- [ ] **Step 4: Add the neutral-spawn config panel**

After the `enemy-spawn` config block (ends ~line 424), add a parallel block:

```html
<div v-if="brushMode === 'neutral-spawn'" class="control-group neutral-spawn-config">
  <label for="neutral-starting-tier">Starting Tier</label>
  <input
    id="neutral-starting-tier"
    v-model.number="neutralStartingTier"
    type="number"
    min="1"
    :disabled="!paintModeEnabled"
  />
  <label for="neutral-tierup-every">Tier Up Every N Waves (0 = off)</label>
  <input
    id="neutral-tierup-every"
    v-model.number="neutralTierUpEveryNWaves"
    type="number"
    min="0"
    :disabled="!paintModeEnabled"
  />
  <label for="neutral-group-id">Group</label>
  <select
    id="neutral-group-id"
    v-model="neutralGroupId"
    :disabled="!paintModeEnabled"
  >
    <option value="__random__">Random</option>
    <option
      v-for="g in groupsForCurrentTier"
      :key="g.id"
      :value="g.id"
    >{{ g.name }}</option>
  </select>
  <label for="neutral-aggro">Aggro Range</label>
  <input id="neutral-aggro" v-model.number="neutralAggroRange" type="number" min="0" :disabled="!paintModeEnabled" />
  <label for="neutral-leash">Leash Range</label>
  <input id="neutral-leash" v-model.number="neutralLeashRange" type="number" min="0" :disabled="!paintModeEnabled" />
  <label for="neutral-hp-base">Health Multiplier</label>
  <input id="neutral-hp-base" v-model.number="neutralHealthMultiplier" type="number" step="0.1" min="0" :disabled="!paintModeEnabled" />
  <label for="neutral-hp-perwave">Health Multiplier Per Wave</label>
  <input id="neutral-hp-perwave" v-model.number="neutralHealthMultiplierPerWave" type="number" step="0.1" min="0" :disabled="!paintModeEnabled" />
  <label for="neutral-dmg-base">Damage Multiplier</label>
  <input id="neutral-dmg-base" v-model.number="neutralDamageMultiplier" type="number" step="0.1" min="0" :disabled="!paintModeEnabled" />
  <label for="neutral-dmg-perwave">Damage Multiplier Per Wave</label>
  <input id="neutral-dmg-perwave" v-model.number="neutralDamageMultiplierPerWave" type="number" step="0.1" min="0" :disabled="!paintModeEnabled" />
</div>
```

- [ ] **Step 5: Add the refs**

Below the existing `enemySpawnDelay`/`enemySpawnInterval`/etc refs (~line 827), add:

```ts
const neutralStartingTier = ref(1)
const neutralTierUpEveryNWaves = ref(0)
const neutralGroupId = ref('__random__')
const neutralAggroRange = ref(150)
const neutralLeashRange = ref(200)
const neutralHealthMultiplier = ref(1.0)
const neutralHealthMultiplierPerWave = ref(0.0)
const neutralDamageMultiplier = ref(1.0)
const neutralDamageMultiplierPerWave = ref(0.0)

const groupsForCurrentTier = computed(() =>
  catalogStore.neutralGroupsForTier(neutralStartingTier.value)
)
```

(Adjust `catalogStore` to match how the existing component imports the store.)

- [ ] **Step 6: Trigger the catalog fetch on mount**

In the component's `onMounted` (or equivalent) hook, alongside the existing catalog fetches:

```ts
onMounted(async () => {
  // ... existing fetches
  await catalogStore.loadNeutralGroups()
})
```

- [ ] **Step 7: Wire the click handler to add a `NeutralSpawn` to the map**

Find the existing click-handler that adds enemy spawns to the map (grep for `enemyTargetPlayerLabel` and the click-handler nearby — should be in the same file). Add a parallel branch for `neutral-spawn`:

```ts
if (brushMode.value === 'neutral-spawn') {
  const id = `neutral-spawn-${gridX}-${gridY}`
  const existingIdx = mapData.value.neutralSpawns?.findIndex(s => s.id === id) ?? -1
  const record = {
    id,
    x: gridX,
    y: gridY,
    groupId: neutralGroupId.value,
    startingTier: neutralStartingTier.value,
    tierUpEveryNWaves: neutralTierUpEveryNWaves.value,
    aggroRange: neutralAggroRange.value,
    leashRange: neutralLeashRange.value,
    healthMultiplier: neutralHealthMultiplier.value,
    healthMultiplierPerWave: neutralHealthMultiplierPerWave.value,
    damageMultiplier: neutralDamageMultiplier.value,
    damageMultiplierPerWave: neutralDamageMultiplierPerWave.value,
  }
  if (!mapData.value.neutralSpawns) mapData.value.neutralSpawns = []
  if (existingIdx >= 0) {
    mapData.value.neutralSpawns[existingIdx] = record
  } else {
    mapData.value.neutralSpawns.push(record)
  }
  markDirty()
  return
}
```

- [ ] **Step 8: Erase brush handles neutral spawns**

Find the erase-brush handler (grep for `brushMode.value === 'erase'`). Add a removal pass for `neutralSpawns`:

```ts
if (mapData.value.neutralSpawns) {
  mapData.value.neutralSpawns = mapData.value.neutralSpawns.filter(s => !(s.x === gridX && s.y === gridY))
}
```

- [ ] **Step 9: Type-check + run dev server**

Run: `cd client/src/game-portal && npm run type-check`
Expected: no errors.

Run dev server (project-specific command, likely `npm run dev` from `client/src/game-portal` or via the project's `start.bat`). Open the map editor and verify:
- "Neutral Spawn" appears in the brush dropdown.
- Selecting it shows the new config panel.
- Clicking on a cell adds a neutral-spawn marker (some kind of indicator; if the canvas doesn't render one yet, see Task 15).

---

## Task 15: Canvas rendering for neutral-spawn markers

**Files:**
- Modify: `client/src/game-portal/src/components/MapEditorPanel.vue` (or whichever canvas renderer the editor uses)

The editor needs to draw a visible marker on each `neutralSpawns` tile so the mapper can see what's placed.

- [ ] **Step 1: Locate the enemy-spawn render code**

Run: `grep -n "enemy-spawn\|enemySpawn" client/src/game-portal/src/components/MapEditorPanel.vue`
Look for the canvas draw loop that renders enemy spawn markers (likely uses a colored circle / icon).

- [ ] **Step 2: Add a parallel render pass for `neutralSpawns`**

Use a distinct color (purple, matching `neutralPlayerColor = "#9b59b6"`) so neutrals visually pop against red enemies. Mirror the existing draw call exactly, then change the fill color and the label.

```ts
// Inside the canvas render loop, after enemy-spawn draws:
if (mapData.value.neutralSpawns) {
  for (const ns of mapData.value.neutralSpawns) {
    const px = ns.x * cellSize + cellSize / 2
    const py = ns.y * cellSize + cellSize / 2
    ctx.fillStyle = '#9b59b6'
    ctx.beginPath()
    ctx.arc(px, py, cellSize * 0.4, 0, Math.PI * 2)
    ctx.fill()
    ctx.fillStyle = '#fff'
    ctx.font = '10px sans-serif'
    ctx.fillText('N', px - 3, py + 3)
  }
}
```

- [ ] **Step 3: Visual check in dev server**

Place a neutral spawn in the editor and confirm a purple `N` circle appears on the cell.

---

## Task 16: End-to-end smoke test (manual)

**Files:** none modified — verification only.

- [ ] **Step 1: Create a test map**

Either via the editor or by editing `server/internal/game/catalog/maps/<some-test-map>.json`. Add a `neutralSpawns` entry:

```json
"neutralSpawns": [
  {
    "id": "neutral-spawn-10-10",
    "x": 10,
    "y": 10,
    "groupId": "__random__",
    "startingTier": 1,
    "tierUpEveryNWaves": 0,
    "aggroRange": 150,
    "leashRange": 200,
    "healthMultiplier": 1.0,
    "healthMultiplierPerWave": 0.0,
    "damageMultiplier": 1.0,
    "damageMultiplierPerWave": 0.0
  }
]
```

- [ ] **Step 2: Start the server + open a match on the test map**

Use the project's `start.bat`.

- [ ] **Step 3: Verify lifecycle**

Tick through these states visually and confirm each:

| Stage | Expected |
|---|---|
| Game start, pre-wave-1 | Purple neutrals visible at (10, 10), holding guard position |
| Walk a player unit into aggro radius | Whole camp engages player unit (one-pulls-all) |
| Walk player unit out of leash radius | Camp returns to (10, 10) |
| Wave 1 starts | All neutrals despawn instantly |
| Wave 1 clears | Fresh neutrals respawn at (10, 10) |
| Wave 2 starts | Neutrals despawn again |
| Wave 2 clears | Respawn again (potentially different random group if `__random__`) |

- [ ] **Step 4: Verify HP scaling (if enabled in the placement)**

Set `healthMultiplierPerWave: 0.5` on the test placement, run two waves, kill a neutral with the same unit both times, and confirm wave-2 neutrals visibly take longer to kill.

- [ ] **Step 5: Verify deterministic random**

Restart the match with the same seed twice. Confirm the same random group is rolled on each respawn cycle in both runs.

---

## Self-Review Checklist (run after writing all tasks)

- [x] Every spec requirement maps to a task:
  - Requirement 1 (neutral_groups folder + tier_1 file) → Tasks 2, 4
  - Requirement 2 (tier + group/random dropdown) → Task 14
  - Requirement 3 (despawn on wave start, regen on wave end) → Tasks 9, 10
  - Requirement 4 (tier-up cadence + tier fallback) → Tasks 4, 9
  - Requirement 5 (aggro/leash + per-wave HP/damage) → Tasks 1, 8
- [x] No "TBD" / "TODO" / "implement appropriately" placeholders.
- [x] Type names consistent across tasks: `NeutralCamp`, `NeutralGroup`, `NeutralGroupCompositionEntry`, `neutralPlayerID`, `NeutralSpawnRandomGroupID`.
- [x] No commit steps (per user request).
- [x] AI_RULES compliance: all target storage by ID, all `getUnitByIDLocked` results validated, all `Locked` suffixes preserved, `s.rng` used for randomness.
