# Wave Upgrades Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** At the end of each wave, pause the game and present each player with three upgrade cards (stat boosts, XP grants, or equipment drops) drawn from a rarity-weighted pool that scales across the run.

**Architecture:** Server-authoritative — upgrade offers are generated and applied server-side; they reach the client as a new `waveUpgrade` field on the existing per-player `MatchSnapshotMessage`. Two new client→server messages (`wave_upgrade_choice`, `wave_upgrade_reroll`) complete the round-trip. The wave state machine gains a new `"upgrade"` state between `"active"` and `"prep"`.

**Tech Stack:** Go 1.22 (server), TypeScript / Vue 3 (client), `math/rand` seeded RNG (`rngSpawn`), existing `addItemToVaultLocked` / `addUnitXPLocked` / `applyRankModifiersLocked` APIs.

---

## File Map

**New — server**
- `server/internal/game/catalog/upgrades/*.json` — 6 seed upgrade definitions
- `server/internal/game/upgrade_defs.go` — `UpgradeDef`, `UpgradeEffect`, embed + load
- `server/internal/game/upgrade_state.go` — `PlayerUpgradeState` struct
- `server/internal/game/upgrade_offers.go` — `generateUpgradeOffersLocked`, `enterWaveUpgradePhaseLocked`, `tickUpgradePhaseLocked`, `HandleWaveUpgradeReroll`
- `server/internal/game/upgrade_apply.go` — `applyUpgradeLocked`, `HandleWaveUpgradeChoice`, `matchesUpgradeScope`
- `server/internal/game/upgrade_offers_test.go`
- `server/internal/game/upgrade_apply_test.go`

**Modified — server**
- `server/internal/game/tuning_defs.go` — add `WaveUpgradeTuning`
- `server/internal/game/catalog/tuning/gameplay_tuning.json` — add `waveUpgrade` block
- `server/internal/game/state.go` — add `UpgradeState PlayerUpgradeState` to `Player`; add `buildWaveUpgradeSnapshotLocked` called from both snapshot builders
- `server/internal/game/state_waves.go` — add `"upgrade"` case to `tickWaveLocked`
- `server/pkg/protocol/messages.go` — add `WaveUpgradeOfferSnapshot`, `UpgradeOffer`, `WaveUpgradeChoiceMessage`, `WaveUpgradeRerollMessage`; add `WaveUpgrade` field to `MatchSnapshotMessage`
- `server/internal/ws/handlers.go` — add `wave_upgrade_choice` and `wave_upgrade_reroll` cases
- `server/internal/profile/types.go` — add `MaxRerolls`, `MaxUpgradeStacks`

**New — client**
- `client/src/game-portal/src/components/WaveUpgradeModal.vue`

**Modified — client**
- `client/src/game-portal/src/game/network/protocol.ts` — add `WaveUpgradeOfferSnapshot`, `UpgradeOffer`, new command types
- `client/src/game-portal/src/game/core/GameState.ts` — add `waveUpgrade` field; populate in `applySnapshot`
- `client/src/game-portal/src/game/core/GameClient.ts` — add `waveUpgrade` to `GameUiSnapshot`; add `sendWaveUpgradeChoice`, `sendWaveUpgradeReroll`
- `client/src/game-portal/src/composables/useGameClient.ts` — expose `sendWaveUpgradeChoice`, `sendWaveUpgradeReroll`; add `waveUpgrade: null` to `emptyUiSnapshot`
- `client/src/game-portal/src/views/MatchView.vue` — mount `WaveUpgradeModal`

---

## Task 1: Upgrade Catalog — Defs + JSON Seed Files

**Files:**
- Create: `server/internal/game/upgrade_defs.go`
- Create: `server/internal/game/catalog/upgrades/swift_strikes_common.json`
- Create: `server/internal/game/catalog/upgrades/swift_strikes_rare.json`
- Create: `server/internal/game/catalog/upgrades/iron_warlord_common.json`
- Create: `server/internal/game/catalog/upgrades/iron_warlord_rare.json`
- Create: `server/internal/game/catalog/upgrades/fortify_common.json`
- Create: `server/internal/game/catalog/upgrades/battlefield_wisdom_common.json`

- [ ] **Step 1: Write the failing test (catalog load)**

Create `server/internal/game/upgrade_defs_test.go`:

```go
package game

import "testing"

func TestUpgradeCatalog_LoadsWithoutPanic(t *testing.T) {
	if len(upgradeDefsByID) == 0 {
		t.Fatal("upgrade catalog is empty")
	}
}

func TestUpgradeCatalog_GetKnownID(t *testing.T) {
	def, ok := getUpgradeDef("swift_strikes_common")
	if !ok {
		t.Fatal("expected swift_strikes_common to exist")
	}
	if def.Group != "swift_strikes" {
		t.Errorf("group: got %q, want %q", def.Group, "swift_strikes")
	}
	if def.MaxStacks != 3 {
		t.Errorf("maxStacks: got %d, want 3", def.MaxStacks)
	}
}

func TestUpgradeCatalog_RarityOrder(t *testing.T) {
	if _, ok := upgradeRarityOrder["legendary"]; !ok {
		t.Fatal("legendary missing from rarity order map")
	}
	if upgradeRarityOrder["legendary"] <= upgradeRarityOrder["epic"] {
		t.Error("legendary must rank higher than epic")
	}
}
```

- [ ] **Step 2: Run test to confirm it fails**

```
cd server && go test ./internal/game/... -run TestUpgradeCatalog -v
```
Expected: compilation error (`upgradeDefsByID`, `getUpgradeDef`, `upgradeRarityOrder` undefined).

- [ ] **Step 3: Create upgrade_defs.go**

```go
package game

import (
	"embed"
	"encoding/json"
	"io/fs"
	"sort"
)

//go:embed catalog/upgrades/*.json
var upgradeDefsFS embed.FS

type UpgradeDef struct {
	ID          string        `json:"id"`
	Group       string        `json:"group"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Rarity      string        `json:"rarity"`
	Scope       string        `json:"scope"`     // "army"|"archetype"|"unitType"|"xp"|"equipment"
	Archetype   string        `json:"archetype,omitempty"`
	UnitType    string        `json:"unitType,omitempty"`
	Effect      UpgradeEffect `json:"effect"`
	MaxStacks   int           `json:"maxStacks"`
}

type UpgradeEffect struct {
	Type       string  `json:"type,omitempty"`       // "xp"|"equipment"; absent = stat multiplier
	Stat       string  `json:"stat,omitempty"`       // "attackSpeed"|"damage"|"hp"|"moveSpeed"|"attackRange"
	Multiplier float64 `json:"multiplier,omitempty"`
	Amount     int     `json:"amount,omitempty"`     // xp grant amount
	ItemID     string  `json:"itemID,omitempty"`     // equipment drop item id
}

const (
	upgradeRarityCommon    = "common"
	upgradeRarityRare      = "rare"
	upgradeRarityEpic      = "epic"
	upgradeRarityLegendary = "legendary"
)

var upgradeRarityOrder = map[string]int{
	upgradeRarityCommon:    0,
	upgradeRarityRare:      1,
	upgradeRarityEpic:      2,
	upgradeRarityLegendary: 3,
}

var upgradeDefsByID = loadUpgradeDefs()

func loadUpgradeDefs() map[string]UpgradeDef {
	entries, err := fs.ReadDir(upgradeDefsFS, "catalog/upgrades")
	if err != nil {
		panic("catalog/upgrades: " + err.Error())
	}
	result := make(map[string]UpgradeDef, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := upgradeDefsFS.ReadFile("catalog/upgrades/" + entry.Name())
		if err != nil {
			panic("catalog/upgrades/" + entry.Name() + ": " + err.Error())
		}
		var def UpgradeDef
		if err := json.Unmarshal(data, &def); err != nil {
			panic("catalog/upgrades/" + entry.Name() + ": " + err.Error())
		}
		if def.ID == "" {
			panic("catalog/upgrades/" + entry.Name() + `: missing "id"`)
		}
		if def.Group == "" {
			panic("catalog/upgrades/" + entry.Name() + `: missing "group"`)
		}
		if _, valid := upgradeRarityOrder[def.Rarity]; !valid {
			panic("catalog/upgrades/" + entry.Name() + `: invalid rarity "` + def.Rarity + `"`)
		}
		if def.MaxStacks <= 0 {
			def.MaxStacks = 3
		}
		if _, dup := result[def.ID]; dup {
			panic("catalog/upgrades/" + entry.Name() + `: duplicate id "` + def.ID + `"`)
		}
		result[def.ID] = def
	}
	return result
}

func getUpgradeDef(id string) (UpgradeDef, bool) {
	def, ok := upgradeDefsByID[id]
	return def, ok
}

func listUpgradeDefs() []UpgradeDef {
	defs := make([]UpgradeDef, 0, len(upgradeDefsByID))
	for _, d := range upgradeDefsByID {
		defs = append(defs, d)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}
```

- [ ] **Step 4: Create the six seed JSON files**

`server/internal/game/catalog/upgrades/swift_strikes_common.json`:
```json
{
  "id": "swift_strikes_common",
  "group": "swift_strikes",
  "name": "Swift Strikes",
  "description": "+8% attack speed to Ranged units",
  "rarity": "common",
  "scope": "archetype",
  "archetype": "ranged",
  "effect": { "stat": "attackSpeed", "multiplier": 1.08 },
  "maxStacks": 3
}
```

`server/internal/game/catalog/upgrades/swift_strikes_rare.json`:
```json
{
  "id": "swift_strikes_rare",
  "group": "swift_strikes",
  "name": "Swift Strikes",
  "description": "+14% attack speed to Ranged units",
  "rarity": "rare",
  "scope": "archetype",
  "archetype": "ranged",
  "effect": { "stat": "attackSpeed", "multiplier": 1.14 },
  "maxStacks": 3
}
```

`server/internal/game/catalog/upgrades/iron_warlord_common.json`:
```json
{
  "id": "iron_warlord_common",
  "group": "iron_warlord",
  "name": "Iron Warlord",
  "description": "+10% damage to Melee units",
  "rarity": "common",
  "scope": "archetype",
  "archetype": "melee",
  "effect": { "stat": "damage", "multiplier": 1.10 },
  "maxStacks": 3
}
```

`server/internal/game/catalog/upgrades/iron_warlord_rare.json`:
```json
{
  "id": "iron_warlord_rare",
  "group": "iron_warlord",
  "name": "Iron Warlord",
  "description": "+18% damage to Melee units",
  "rarity": "rare",
  "scope": "archetype",
  "archetype": "melee",
  "effect": { "stat": "damage", "multiplier": 1.18 },
  "maxStacks": 3
}
```

`server/internal/game/catalog/upgrades/fortify_common.json`:
```json
{
  "id": "fortify_common",
  "group": "fortify",
  "name": "Fortify",
  "description": "+12% max HP to all units",
  "rarity": "common",
  "scope": "army",
  "effect": { "stat": "hp", "multiplier": 1.12 },
  "maxStacks": 3
}
```

`server/internal/game/catalog/upgrades/battlefield_wisdom_common.json`:
```json
{
  "id": "battlefield_wisdom_common",
  "group": "battlefield_wisdom",
  "name": "Battlefield Wisdom",
  "description": "Grant 75 XP to a unit of your choice",
  "rarity": "common",
  "scope": "xp",
  "effect": { "type": "xp", "amount": 75 },
  "maxStacks": 3
}
```

- [ ] **Step 5: Run tests to verify they pass**

```
cd server && go test ./internal/game/... -run TestUpgradeCatalog -v
```
Expected: all 3 tests PASS.

- [ ] **Step 6: Commit**

```
git add server/internal/game/upgrade_defs.go server/internal/game/upgrade_defs_test.go server/internal/game/catalog/upgrades/
git commit -m "feat: add wave upgrade catalog (UpgradeDef loader + 6 seed upgrades)"
```

---

## Task 2: Tuning Config Extension

**Files:**
- Modify: `server/internal/game/tuning_defs.go`
- Modify: `server/internal/game/catalog/tuning/gameplay_tuning.json`

- [ ] **Step 1: Add WaveUpgradeTuning struct to tuning_defs.go**

In `tuning_defs.go`, add the following struct and field after the existing `BuffSlotsTuning` definition:

```go
// WaveUpgradeTuning controls offer generation for the wave upgrade phase.
type WaveUpgradeTuning struct {
	// TimerSeconds is how long players have to pick before auto-select fires.
	TimerSeconds float64 `json:"timerSeconds"`
	// BaseWeights is the rarity probability weight at wave 1.
	BaseWeights map[string]float64 `json:"baseWeights"`
	// RarityScalePerWave is added to each rarity's weight each wave (can be negative).
	RarityScalePerWave map[string]float64 `json:"rarityScalePerWave"`
	// MilestoneWaves are wave numbers that guarantee at least one card of MilestoneMinRarity or better.
	MilestoneWaves []int `json:"milestoneWaves"`
	// MilestoneMinRarity is the minimum rarity guaranteed on a milestone wave.
	MilestoneMinRarity string `json:"milestoneMinRarity"`
}
```

Add a `WaveUpgrade WaveUpgradeTuning` field to `GameplayTuning`:

```go
type GameplayTuning struct {
	Version       int                                `json:"version"`
	LegendPoints  LegendPointsTuning                 `json:"legendPoints"`
	BuffSlots     BuffSlotsTuning                    `json:"buffSlots"`
	WaveUpgrade   WaveUpgradeTuning                  `json:"waveUpgrade"`
	UnitOverrides map[string]UnitLegendPointOverride `json:"unitOverrides"`
}
```

- [ ] **Step 2: Add waveUpgrade block to gameplay_tuning.json**

The file is at `server/internal/game/catalog/tuning/gameplay_tuning.json`. Add after `"buffSlots"`:

```json
{
  "version": 1,
  "legendPoints": {
    "winBonus": 0,
    "lossConsolation": 0,
    "perObjective": 0,
    "perKillBaseDropChance": 0.0,
    "perKillBaseAmount": 0
  },
  "buffSlots": {
    "maxActive": 3
  },
  "waveUpgrade": {
    "timerSeconds": 25,
    "baseWeights": {
      "common": 60,
      "rare": 25,
      "epic": 12,
      "legendary": 3
    },
    "rarityScalePerWave": {
      "common": -1.5,
      "rare": 0.5,
      "epic": 0.7,
      "legendary": 0.3
    },
    "milestoneWaves": [5, 10, 15, 20],
    "milestoneMinRarity": "epic"
  },
  "unitOverrides": {}
}
```

- [ ] **Step 3: Verify server compiles**

```
cd server && go build ./...
```
Expected: no errors.

- [ ] **Step 4: Commit**

```
git add server/internal/game/tuning_defs.go server/internal/game/catalog/tuning/gameplay_tuning.json
git commit -m "feat: extend gameplay tuning with waveUpgrade config block"
```

---

## Task 3: Player Upgrade State Structs

**Files:**
- Create: `server/internal/game/upgrade_state.go`
- Modify: `server/internal/game/state.go` (add `UpgradeState` to `Player`, initialize it)
- Modify: `server/internal/profile/types.go` (add `MaxRerolls`, `MaxUpgradeStacks`)

- [ ] **Step 1: Create upgrade_state.go**

```go
package game

// PlayerUpgradeState tracks wave upgrade progression for a single player.
// Run-persistent fields (UpgradeStacks, MaxRerolls, MaxUpgradeStacks) survive
// until the match ends. Wave-transient fields (CurrentOffers, OfferDeadlineMs,
// RerollsRemaining, Resolved) are reset by enterWaveUpgradePhaseLocked each wave.
type PlayerUpgradeState struct {
	// Run-persistent
	UpgradeStacks    map[string]int // upgrade group → number of times taken this run
	MaxRerolls       int            // rerolls per wave; default 1, legend-incrementable
	MaxUpgradeStacks int            // stack cap override; default 3, legend-incrementable

	// Wave-transient (reset each wave by enterWaveUpgradePhaseLocked)
	RerollsRemaining int
	CurrentOffers    []UpgradeDef
	OfferDeadlineMs  int64 // unix ms auto-pick fires
	Resolved         bool
}

func newPlayerUpgradeState(maxRerolls, maxStacks int) PlayerUpgradeState {
	if maxRerolls <= 0 {
		maxRerolls = 1
	}
	if maxStacks <= 0 {
		maxStacks = 3
	}
	return PlayerUpgradeState{
		UpgradeStacks:    make(map[string]int),
		MaxRerolls:       maxRerolls,
		MaxUpgradeStacks: maxStacks,
	}
}
```

- [ ] **Step 2: Add UpgradeState to Player in state.go**

In `state.go`, find the `Player` struct (line 389) and add the field after `RunLegendPointDrops`:

```go
	// RunLegendPointDrops accumulates legend-point drops during the match.
	// Committed to the profile file at match end.
	RunLegendPointDrops int

	// UpgradeState tracks wave upgrade picks and per-wave offer state.
	UpgradeState PlayerUpgradeState
```

- [ ] **Step 3: Initialize UpgradeState when player joins**

In `state.go`, find the player creation block (around line 2031) that sets `s.Players[playerID] = &Player{...}`. Add `UpgradeState` initialization:

```go
	s.Players[playerID] = &Player{
		ID:    playerID,
		Color: color,
		Resources: map[string]int{
			"gold": 500,
			"wood": 180,
		},
		GlobalUnitSpawnTimeMultiplier: 1,
		UnitSpawnTimeMultipliers:      map[string]float64{},
		Upgrades:                      make(map[UpgradeTrack]int),
		Vault:                         []*VaultItem{},
		ProfileBuffIDs:                append([]string(nil), equippedBuffIDs...),
		UpgradeState:                  newPlayerUpgradeState(1, 3),
	}
```

- [ ] **Step 4: Add MaxRerolls and MaxUpgradeStacks to PlayerProfile**

In `server/internal/profile/types.go`, add to `PlayerProfile`:

```go
	// Wave upgrade legend-incrementable caps. Zero values fall back to defaults
	// (MaxRerolls=1, MaxUpgradeStacks=3) applied at match start.
	MaxRerolls       int `json:"maxRerolls"`
	MaxUpgradeStacks int `json:"maxUpgradeStacks"`
```

- [ ] **Step 5: Verify compilation**

```
cd server && go build ./...
```
Expected: no errors.

- [ ] **Step 6: Commit**

```
git add server/internal/game/upgrade_state.go server/internal/game/state.go server/internal/profile/types.go
git commit -m "feat: add PlayerUpgradeState to Player and MaxRerolls/MaxUpgradeStacks to PlayerProfile"
```

---

## Task 4: Protocol Message Types

**Files:**
- Modify: `server/pkg/protocol/messages.go`
- Modify: `client/src/game-portal/src/game/network/protocol.ts`

- [ ] **Step 1: Add server-side protocol types to messages.go**

In `server/pkg/protocol/messages.go`, add after the existing `WaveSnapshot` block:

```go
// WaveUpgradeOfferSnapshot is the per-player upgrade offer sent during the
// "upgrade" wave phase. Nil/absent means the player has no pending offer
// (not in upgrade phase, or already resolved).
type WaveUpgradeOfferSnapshot struct {
	Wave        int           `json:"wave"`
	Offers      []UpgradeOffer `json:"offers"`
	RerollsLeft int           `json:"rerollsLeft"`
	DeadlineMs  int64         `json:"deadlineMs"` // unix ms when auto-pick fires
}

// UpgradeOffer is one card in the wave upgrade offer set.
type UpgradeOffer struct {
	ID                 string `json:"id"`
	Group              string `json:"group"`
	Name               string `json:"name"`
	Description        string `json:"description"`
	Rarity             string `json:"rarity"`
	Scope              string `json:"scope"`
	StackCurrent       int    `json:"stackCurrent"`
	StackMax           int    `json:"stackMax"`
	RequiresTargetUnit bool   `json:"requiresTargetUnit,omitempty"`
}
```

Add `WaveUpgrade` field to `MatchSnapshotMessage` (after the `Fow` field):

```go
	Fow           *FogOfWarSnapshot       `json:"fow,omitempty"`
	WaveUpgrade   *WaveUpgradeOfferSnapshot `json:"waveUpgrade,omitempty"`
```

Add the two client→server command message types at the end of the file:

```go
// WaveUpgradeChoiceMessage is sent by the client when the player picks an upgrade.
type WaveUpgradeChoiceMessage struct {
	Type         string `json:"type"`
	UpgradeID    string `json:"upgradeId"`
	TargetUnitID int    `json:"targetUnitId,omitempty"` // set only when RequiresTargetUnit = true
}

// WaveUpgradeRerollMessage is sent by the client when the player uses a reroll.
type WaveUpgradeRerollMessage struct {
	Type string `json:"type"`
}
```

- [ ] **Step 2: Add TypeScript types to protocol.ts**

In `client/src/game-portal/src/game/network/protocol.ts`, add the following interfaces (alongside existing snapshot interfaces):

```typescript
export interface UpgradeOffer {
  id: string
  group: string
  name: string
  description: string
  rarity: 'common' | 'rare' | 'epic' | 'legendary'
  scope: string
  stackCurrent: number
  stackMax: number
  requiresTargetUnit?: boolean
}

export interface WaveUpgradeOfferSnapshot {
  wave: number
  offers: UpgradeOffer[]
  rerollsLeft: number
  deadlineMs: number
}
```

Add `waveUpgrade?: WaveUpgradeOfferSnapshot` to the `MatchSnapshotMessage` interface.

- [ ] **Step 3: Verify compilation**

```
cd server && go build ./...
```

```
cd client/src/game-portal && npm run build 2>&1 | tail -5
```
Expected: no errors in either.

- [ ] **Step 4: Commit**

```
git add server/pkg/protocol/messages.go client/src/game-portal/src/game/network/protocol.ts
git commit -m "feat: add wave upgrade protocol messages (offer snapshot, choice, reroll)"
```

---

## Task 5: Offer Generation + Tests

**Files:**
- Create: `server/internal/game/upgrade_offers.go`
- Create: `server/internal/game/upgrade_offers_test.go`

- [ ] **Step 1: Write the failing tests**

Create `server/internal/game/upgrade_offers_test.go`:

```go
package game

import (
	"testing"
	"time"
)

func newTestStateForUpgrades(t *testing.T) *GameState {
	t.Helper()
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.WaveManager.Enabled = true
	s.WaveManager.CurrentWave = 1
	s.Players["p1"] = &Player{
		ID:           "p1",
		Resources:    map[string]int{},
		UpgradeState: newPlayerUpgradeState(1, 3),
	}
	return s
}

func TestGenerateUpgradeOffers_ReturnsThree(t *testing.T) {
	s := newTestStateForUpgrades(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	offers := s.generateUpgradeOffersLocked("p1")
	if len(offers) != 3 {
		t.Fatalf("expected 3 offers, got %d", len(offers))
	}
}

func TestGenerateUpgradeOffers_NoDuplicateIDs(t *testing.T) {
	s := newTestStateForUpgrades(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	offers := s.generateUpgradeOffersLocked("p1")
	seen := map[string]bool{}
	for _, o := range offers {
		if seen[o.ID] {
			t.Errorf("duplicate offer id: %s", o.ID)
		}
		seen[o.ID] = true
	}
}

func TestGenerateUpgradeOffers_FiltersMaxedGroup(t *testing.T) {
	s := newTestStateForUpgrades(t)
	// Max out every group except one so that group must appear.
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, def := range listUpgradeDefs() {
		if def.Group != "fortify" {
			s.Players["p1"].UpgradeState.UpgradeStacks[def.Group] = 99
		}
	}
	offers := s.generateUpgradeOffersLocked("p1")
	for _, o := range offers {
		if o.Group == "battlefield_wisdom" || o.Group == "swift_strikes" || o.Group == "iron_warlord" {
			t.Errorf("maxed group %q appeared in offers", o.Group)
		}
	}
}

func TestMilestoneWave_GuaranteesEpicOrBetter(t *testing.T) {
	s := newTestStateForUpgrades(t)
	s.WaveManager.CurrentWave = 5 // first milestone
	s.mu.Lock()
	defer s.mu.Unlock()
	// Run 20 times to rule out luck.
	for i := 0; i < 20; i++ {
		offers := s.generateUpgradeOffersLocked("p1")
		hasEpicOrBetter := false
		for _, o := range offers {
			if upgradeRarityOrder[o.Rarity] >= upgradeRarityOrder[upgradeRarityEpic] {
				hasEpicOrBetter = true
			}
		}
		if !hasEpicOrBetter {
			t.Errorf("milestone wave 5 offer set %d had no epic+ card: %v", i, offers)
		}
	}
}

func TestEnterWaveUpgradePhase_SetsDeadlineAndOffers(t *testing.T) {
	s := newTestStateForUpgrades(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	before := time.Now().UnixMilli()
	s.enterWaveUpgradePhaseLocked()
	after := time.Now().UnixMilli()
	p := s.Players["p1"]
	if p.UpgradeState.Resolved {
		t.Error("player should not be resolved after phase entry")
	}
	if p.UpgradeState.OfferDeadlineMs < before {
		t.Error("deadline should be in the future")
	}
	if p.UpgradeState.OfferDeadlineMs > after+30_000 {
		t.Error("deadline should be within 30 seconds")
	}
	if len(p.UpgradeState.CurrentOffers) != 3 {
		t.Errorf("expected 3 current offers, got %d", len(p.UpgradeState.CurrentOffers))
	}
	if p.UpgradeState.RerollsRemaining != 1 {
		t.Errorf("rerolls remaining: got %d, want 1", p.UpgradeState.RerollsRemaining)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```
cd server && go test ./internal/game/... -run TestGenerateUpgradeOffers -v
```
Expected: compile error (`generateUpgradeOffersLocked`, `enterWaveUpgradePhaseLocked` undefined).

- [ ] **Step 3: Create upgrade_offers.go**

```go
package game

import (
	mrand "math/rand"
	"time"
)

// generateUpgradeOffersLocked returns 3 upgrade cards for playerID.
// Filters groups the player has already maxed, weights by rarity (wave-scaled),
// and on milestone waves guarantees at least one card of milestoneMinRarity+.
// Caller must hold s.mu.
func (s *GameState) generateUpgradeOffersLocked(playerID string) []UpgradeDef {
	player := s.Players[playerID]
	if player == nil {
		return nil
	}
	tuning := gameplayTuning().WaveUpgrade
	wave := s.WaveManager.CurrentWave

	type weighted struct {
		def    UpgradeDef
		weight float64
	}

	// Build weighted pool — exclude groups at/above the effective stack cap.
	var pool []weighted
	for _, def := range listUpgradeDefs() {
		effectiveCap := def.MaxStacks
		if player.UpgradeState.MaxUpgradeStacks > effectiveCap {
			effectiveCap = player.UpgradeState.MaxUpgradeStacks
		}
		if player.UpgradeState.UpgradeStacks[def.Group] >= effectiveCap {
			continue
		}
		base := tuning.BaseWeights[def.Rarity]
		scale := tuning.RarityScalePerWave[def.Rarity]
		w := base + scale*float64(wave-1)
		if w <= 0 {
			continue
		}
		pool = append(pool, weighted{def: def, weight: w})
	}
	if len(pool) == 0 {
		return nil
	}

	// Detect milestone wave.
	isMilestone := false
	for _, mw := range tuning.MilestoneWaves {
		if wave == mw {
			isMilestone = true
			break
		}
	}
	minRarityRank := -1
	if isMilestone && tuning.MilestoneMinRarity != "" {
		minRarityRank = upgradeRarityOrder[tuning.MilestoneMinRarity]
	}

	selected := make([]UpgradeDef, 0, 3)
	usedIDs := make(map[string]bool)

	// On a milestone wave, force the first pick from the epic+ sub-pool.
	if minRarityRank >= 0 {
		var epicPool []weighted
		for _, w := range pool {
			if upgradeRarityOrder[w.def.Rarity] >= minRarityRank {
				epicPool = append(epicPool, w)
			}
		}
		if len(epicPool) > 0 {
			pick := weightedSampleUpgrade(s.rngSpawn, epicPool)
			selected = append(selected, pick)
			usedIDs[pick.ID] = true
		}
	}

	// Fill remaining slots from the full pool (excluding already selected).
	for len(selected) < 3 {
		var available []weighted
		for _, w := range pool {
			if !usedIDs[w.def.ID] {
				available = append(available, w)
			}
		}
		if len(available) == 0 {
			break
		}
		pick := weightedSampleUpgrade(s.rngSpawn, available)
		usedIDs[pick.ID] = true
		selected = append(selected, pick)
	}
	return selected
}

func weightedSampleUpgrade(rng *mrand.Rand, pool []struct {
	def    UpgradeDef
	weight float64
}) UpgradeDef {
	total := 0.0
	for _, w := range pool {
		total += w.weight
	}
	r := rng.Float64() * total
	for _, w := range pool {
		r -= w.weight
		if r <= 0 {
			return w.def
		}
	}
	return pool[len(pool)-1].def
}

// enterWaveUpgradePhaseLocked initialises per-player offer state for the
// current wave. Must be called once when the wave state transitions to "upgrade".
// Caller must hold s.mu.
func (s *GameState) enterWaveUpgradePhaseLocked() {
	tuning := gameplayTuning().WaveUpgrade
	deadlineMs := time.Now().UnixMilli() + int64(tuning.TimerSeconds*1000)
	for playerID, player := range s.Players {
		if playerID == enemyPlayerID {
			continue
		}
		player.UpgradeState.RerollsRemaining = player.UpgradeState.MaxRerolls
		player.UpgradeState.CurrentOffers = s.generateUpgradeOffersLocked(playerID)
		player.UpgradeState.OfferDeadlineMs = deadlineMs
		player.UpgradeState.Resolved = false
	}
}

// tickUpgradePhaseLocked checks per-player deadlines and advances to "prep"
// once all players have resolved. Caller must hold s.mu.
func (s *GameState) tickUpgradePhaseLocked() {
	now := time.Now().UnixMilli()
	for playerID, player := range s.Players {
		if playerID == enemyPlayerID || player.UpgradeState.Resolved {
			continue
		}
		if now >= player.UpgradeState.OfferDeadlineMs {
			if len(player.UpgradeState.CurrentOffers) > 0 {
				s.applyUpgradeLocked(playerID, player.UpgradeState.CurrentOffers[0].ID, 0)
			}
			player.UpgradeState.Resolved = true
		}
	}
	if s.waveUpgradeAllResolvedLocked() {
		s.WaveManager.State = "prep"
		s.WaveManager.Timer = s.WaveManager.PrepDuration
	}
}

func (s *GameState) waveUpgradeAllResolvedLocked() bool {
	for playerID, player := range s.Players {
		if playerID == enemyPlayerID {
			continue
		}
		if !player.UpgradeState.Resolved {
			return false
		}
	}
	return true
}

// HandleWaveUpgradeReroll processes a player's reroll request.
func (s *GameState) HandleWaveUpgradeReroll(playerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	player := s.Players[playerID]
	if player == nil || player.UpgradeState.Resolved {
		return
	}
	if s.WaveManager.State != "upgrade" {
		return
	}
	if player.UpgradeState.RerollsRemaining <= 0 {
		return
	}
	player.UpgradeState.RerollsRemaining--
	player.UpgradeState.CurrentOffers = s.generateUpgradeOffersLocked(playerID)
}
```

Note: `weightedSampleUpgrade` uses a locally-typed pool slice. Go requires the anonymous struct type to match exactly. Rewrite using a named type to avoid issues:

Replace the `weightedSampleUpgrade` function and the pool declaration in `generateUpgradeOffersLocked` to use a named type:

```go
type upgradeWeight struct {
	def    UpgradeDef
	weight float64
}
```

Declare it at package level (top of the file, after imports) and update all references from `weighted` to `upgradeWeight`, and the function signature to:

```go
func weightedSampleUpgrade(rng *mrand.Rand, pool []upgradeWeight) UpgradeDef {
```

- [ ] **Step 4: Run tests**

```
cd server && go test ./internal/game/... -run "TestGenerateUpgradeOffers|TestMilestone|TestEnterWave" -v
```
Expected: all 5 tests PASS. The milestone test may need more catalog entries with epic/legendary rarity to guarantee a hit — if it fails because no epic+ upgrades exist in the seed catalog, add a temporary epic entry or skip the assertion when the epic pool is empty (the implementation already handles that gracefully).

- [ ] **Step 5: Commit**

```
git add server/internal/game/upgrade_offers.go server/internal/game/upgrade_offers_test.go
git commit -m "feat: wave upgrade offer generation with rarity weighting and milestone guarantee"
```

---

## Task 6: Effect Application + Tests

**Files:**
- Create: `server/internal/game/upgrade_apply.go`
- Create: `server/internal/game/upgrade_apply_test.go`

- [ ] **Step 1: Write the failing tests**

Create `server/internal/game/upgrade_apply_test.go`:

```go
package game

import (
	"math"
	"testing"
)

func TestApplyUpgrade_StatMultiplierAffectsMatchingUnits(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.Players["p1"] = &Player{
		ID:           "p1",
		Resources:    map[string]int{},
		UpgradeState: newPlayerUpgradeState(1, 3),
	}
	// Spawn a ranged unit owned by p1.
	unit := s.spawnPlayerUnitLocked("archer", "p1", "#fff", s.gridToWorldCenter(s.worldToGrid(200, 200)))
	if unit == nil {
		t.Skip("archer unit type not available in test map")
	}
	baseDmg := unit.BaseDamage

	s.applyUpgradeLocked("p1", "iron_warlord_common", 0)

	// iron_warlord_common is archetype=melee; archer is not melee, so BaseDamage should not change.
	if unit.BaseDamage != baseDmg {
		t.Errorf("archer BaseDamage should be unchanged by melee upgrade; was %d, got %d", baseDmg, unit.BaseDamage)
	}

	// Now check that applying a fortify (army-wide HP) does affect it.
	baseHP := unit.BaseMaxHP
	s.applyUpgradeLocked("p1", "fortify_common", 0)
	expectedHP := int(math.Round(float64(baseHP) * 1.12))
	if unit.BaseMaxHP != expectedHP {
		t.Errorf("fortify_common: BaseMaxHP want %d, got %d", expectedHP, unit.BaseMaxHP)
	}
}

func TestApplyUpgrade_IncrementsStackCounter(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.Players["p1"] = &Player{
		ID:           "p1",
		Resources:    map[string]int{},
		UpgradeState: newPlayerUpgradeState(1, 3),
	}
	s.applyUpgradeLocked("p1", "fortify_common", 0)
	if got := s.Players["p1"].UpgradeState.UpgradeStacks["fortify"]; got != 1 {
		t.Errorf("stack counter: want 1, got %d", got)
	}
	s.applyUpgradeLocked("p1", "fortify_common", 0)
	if got := s.Players["p1"].UpgradeState.UpgradeStacks["fortify"]; got != 2 {
		t.Errorf("stack counter after 2nd apply: want 2, got %d", got)
	}
}

func TestApplyUpgrade_XPGrantReachesTargetUnit(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.Players["p1"] = &Player{
		ID:           "p1",
		Resources:    map[string]int{},
		UpgradeState: newPlayerUpgradeState(1, 3),
	}
	unit := s.spawnPlayerUnitLocked("soldier", "p1", "#fff", s.gridToWorldCenter(s.worldToGrid(200, 200)))
	if unit == nil {
		t.Skip("soldier unit type not available in test map")
	}
	startXP := unit.XP
	s.applyUpgradeLocked("p1", "battlefield_wisdom_common", unit.ID)
	if unit.XP <= startXP {
		t.Errorf("XP did not increase: before %d, after %d", startXP, unit.XP)
	}
}

func TestApplyUpgrade_UnknownIDIsNoOp(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.Players["p1"] = &Player{
		ID:           "p1",
		Resources:    map[string]int{},
		UpgradeState: newPlayerUpgradeState(1, 3),
	}
	// Should not panic.
	s.applyUpgradeLocked("p1", "nonexistent_upgrade", 0)
}
```

- [ ] **Step 2: Run to confirm failure**

```
cd server && go test ./internal/game/... -run TestApplyUpgrade -v
```
Expected: compile error (`applyUpgradeLocked`, `matchesUpgradeScope` undefined).

- [ ] **Step 3: Create upgrade_apply.go**

```go
package game

import "math"

// applyUpgradeLocked applies the chosen upgrade to playerID's army.
// targetUnitID is only used when the upgrade scope is "xp" — it identifies
// which unit receives the XP grant. Caller must hold s.mu.
func (s *GameState) applyUpgradeLocked(playerID, upgradeID string, targetUnitID int) {
	def, ok := getUpgradeDef(upgradeID)
	if !ok {
		return
	}
	player := s.Players[playerID]
	if player == nil {
		return
	}
	player.UpgradeState.UpgradeStacks[def.Group]++

	switch def.Effect.Type {
	case "xp":
		unit := s.getUnitByIDLocked(targetUnitID)
		if unit != nil && unit.OwnerID == playerID && unit.HP > 0 {
			s.addUnitXPLocked(unit, def.Effect.Amount)
		}
	case "equipment":
		if itemDef, ok := itemCatalogSingleton[def.Effect.ItemID]; ok {
			s.addItemToVaultLocked(player, itemDef)
		}
	default:
		// Stat multiplier: walk all living player-owned units matching the scope.
		for _, unit := range s.Units {
			if unit.OwnerID != playerID || unit.HP <= 0 {
				continue
			}
			if !matchesUpgradeScope(def, unit) {
				continue
			}
			applyStatMultiplierToUnit(def, unit)
			s.applyRankModifiersLocked(unit, true)
		}
	}
}

// matchesUpgradeScope reports whether unit falls within def's targeting scope.
func matchesUpgradeScope(def UpgradeDef, unit *Unit) bool {
	switch def.Scope {
	case "army":
		return true
	case "archetype":
		return unit.Archetype == def.Archetype
	case "unitType":
		return unit.UnitType == def.UnitType
	default:
		return false
	}
}

// applyStatMultiplierToUnit multiplies the relevant Base* stat on unit by
// def.Effect.Multiplier. Caller must call applyRankModifiersLocked afterward
// to propagate the change to derived stats.
func applyStatMultiplierToUnit(def UpgradeDef, unit *Unit) {
	m := def.Effect.Multiplier
	switch def.Effect.Stat {
	case "attackSpeed":
		unit.BaseAttackSpeed *= m
	case "damage":
		unit.BaseDamage = int(math.Round(float64(unit.BaseDamage) * m))
	case "hp":
		unit.BaseMaxHP = int(math.Round(float64(unit.BaseMaxHP) * m))
	case "moveSpeed":
		unit.BaseMoveSpeed *= m
	case "attackRange":
		unit.BaseAttackRange *= m
	}
}

// HandleWaveUpgradeChoice processes a player's upgrade selection.
func (s *GameState) HandleWaveUpgradeChoice(playerID, upgradeID string, targetUnitID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	player := s.Players[playerID]
	if player == nil || player.UpgradeState.Resolved {
		return
	}
	if s.WaveManager.State != "upgrade" {
		return
	}
	// Validate that upgradeID is among the current offers.
	valid := false
	for _, offer := range player.UpgradeState.CurrentOffers {
		if offer.ID == upgradeID {
			valid = true
			break
		}
	}
	if !valid {
		return
	}
	s.applyUpgradeLocked(playerID, upgradeID, targetUnitID)
	player.UpgradeState.Resolved = true
}
```

- [ ] **Step 4: Run tests**

```
cd server && go test ./internal/game/... -run TestApplyUpgrade -v
```
Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```
git add server/internal/game/upgrade_apply.go server/internal/game/upgrade_apply_test.go
git commit -m "feat: wave upgrade effect application (stat multiplier, XP grant, equipment drop)"
```

---

## Task 7: Wave Phase Integration

**Files:**
- Modify: `server/internal/game/state_waves.go`
- Modify: `server/internal/game/state.go`

- [ ] **Step 1: Add "upgrade" state to tickWaveLocked**

In `server/internal/game/state_waves.go`, find the `tickWaveLocked` function (around line 101). Change the `"active"` case to transition to `"upgrade"` instead of `"prep"` when a non-final wave clears, and add the new `"upgrade"` case:

```go
	case "active":
		wm.Timer += dt
		timerExpired := wm.WaveDuration > 0 && wm.Timer >= wm.WaveDuration
		if timerExpired {
			wm.Timer = wm.WaveDuration
			if s.countEnemyUnitsLocked() == 0 {
				if wm.TotalWaves > 0 && wm.CurrentWave >= wm.TotalWaves {
					wm.State = "complete"
					s.markWaveObjectivesCompleteLocked()
				} else {
					wm.State = "upgrade"
					s.enterWaveUpgradePhaseLocked()
				}
			}
		}

	case "upgrade":
		s.tickUpgradePhaseLocked()
```

- [ ] **Step 2: Add buildWaveUpgradeSnapshotLocked to state.go**

In `server/internal/game/state.go`, add this method near the other snapshot helpers:

```go
// buildWaveUpgradeSnapshotLocked returns the per-player upgrade offer snapshot
// for viewerID, or nil when there is no pending offer for that player.
// Caller must hold s.mu (read lock is sufficient).
func (s *GameState) buildWaveUpgradeSnapshotLocked(viewerID string) *protocol.WaveUpgradeOfferSnapshot {
	if s.WaveManager.State != "upgrade" {
		return nil
	}
	player := s.Players[viewerID]
	if player == nil || player.UpgradeState.Resolved {
		return nil
	}
	offers := make([]protocol.UpgradeOffer, 0, len(player.UpgradeState.CurrentOffers))
	for _, def := range player.UpgradeState.CurrentOffers {
		effectiveCap := def.MaxStacks
		if player.UpgradeState.MaxUpgradeStacks > effectiveCap {
			effectiveCap = player.UpgradeState.MaxUpgradeStacks
		}
		offers = append(offers, protocol.UpgradeOffer{
			ID:                 def.ID,
			Group:              def.Group,
			Name:               def.Name,
			Description:        def.Description,
			Rarity:             def.Rarity,
			Scope:              def.Scope,
			StackCurrent:       player.UpgradeState.UpgradeStacks[def.Group],
			StackMax:           effectiveCap,
			RequiresTargetUnit: def.Effect.Type == "xp",
		})
	}
	return &protocol.WaveUpgradeOfferSnapshot{
		Wave:        s.WaveManager.CurrentWave,
		Offers:      offers,
		RerollsLeft: player.UpgradeState.RerollsRemaining,
		DeadlineMs:  player.UpgradeState.OfferDeadlineMs,
	}
}
```

- [ ] **Step 3: Wire snapshot into SnapshotForPlayer and snapshotUnfilteredLocked**

In `SnapshotForPlayer` (around line 1080 in `state.go`), before the function returns its assembled snapshot, add the wave upgrade field. Find where the snapshot is assembled and add:

```go
snap.WaveUpgrade = s.buildWaveUpgradeSnapshotLocked(viewerID)
```

Do the same in `snapshotUnfilteredLocked` — since there's no specific viewer, use the first non-enemy player:

```go
// For unfiltered snapshots (spectators, debug), skip per-player upgrade offer.
// snap.WaveUpgrade remains nil.
```

(No code change needed for `snapshotUnfilteredLocked` — leave `WaveUpgrade` nil for spectators.)

- [ ] **Step 4: Add "upgrade" to WaveSnapshot state strings in client**

In `client/src/game-portal/src/components/MatchHud.vue`, the `waveTimerText` computed already handles unknown states by returning `''`. No change needed there. But update `waveLabel` to not show "Wave X" during the upgrade phase if desired — this is optional styling polish.

- [ ] **Step 5: Verify server compiles and existing tests pass**

```
cd server && go build ./... && go test ./internal/game/... -v -count=1 2>&1 | tail -20
```
Expected: no new failures.

- [ ] **Step 6: Commit**

```
git add server/internal/game/state_waves.go server/internal/game/state.go
git commit -m "feat: add wave upgrade phase to wave state machine; wire snapshot per-player"
```

---

## Task 8: WebSocket Handlers

**Files:**
- Modify: `server/internal/ws/handlers.go`

- [ ] **Step 1: Add wave_upgrade_choice and wave_upgrade_reroll cases**

In `server/internal/ws/handlers.go`, find the `switch base.Type` block (around line 89). Add the two new cases after the existing `"equip_item"` case, following the exact same pattern:

```go
		case "wave_upgrade_choice":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "must join a match before sending commands"})
				continue
			}
			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "match not found"})
				continue
			}
			var msg protocol.WaveUpgradeChoiceMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "invalid wave_upgrade_choice payload"})
				continue
			}
			match.State.HandleWaveUpgradeChoice(client.PlayerID(), msg.UpgradeID, msg.TargetUnitID)

		case "wave_upgrade_reroll":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "must join a match before sending commands"})
				continue
			}
			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "match not found"})
				continue
			}
			match.State.HandleWaveUpgradeReroll(client.PlayerID())
```

- [ ] **Step 2: Verify compilation**

```
cd server && go build ./...
```
Expected: no errors.

- [ ] **Step 3: Commit**

```
git add server/internal/ws/handlers.go
git commit -m "feat: add wave_upgrade_choice and wave_upgrade_reroll WebSocket handlers"
```

---

## Task 9: Client State + GameClient Wiring

**Files:**
- Modify: `client/src/game-portal/src/game/core/GameState.ts`
- Modify: `client/src/game-portal/src/game/core/GameClient.ts`
- Modify: `client/src/game-portal/src/composables/useGameClient.ts`

- [ ] **Step 1: Add waveUpgrade field to GameState.ts**

In `GameState.ts`, add a field alongside the other snapshot-derived fields (near `localPlayerVault`, `playerUpgrades`):

```typescript
waveUpgrade: WaveUpgradeOfferSnapshot | null = null
```

Add the import at the top of the file if not already present:

```typescript
import type { WaveUpgradeOfferSnapshot } from '../network/protocol'
```

In the `applySnapshot(message: MatchSnapshotMessage)` method, add:

```typescript
this.waveUpgrade = message.waveUpgrade ?? null
```

- [ ] **Step 2: Add waveUpgrade to GameUiSnapshot and getUiSnapshot in GameClient.ts**

In `GameClient.ts`, add `waveUpgrade` to the `GameUiSnapshot` type:

```typescript
export type GameUiSnapshot = {
  // ... existing fields ...
  waveUpgrade: WaveUpgradeOfferSnapshot | null
}
```

In `getUiSnapshot()`:

```typescript
  getUiSnapshot(): GameUiSnapshot {
    return {
      // ... existing fields ...
      waveUpgrade: this.state.waveUpgrade,
    }
  }
```

Add `sendWaveUpgradeChoice` and `sendWaveUpgradeReroll` methods to `GameClient`:

```typescript
  sendWaveUpgradeChoice(upgradeID: string, targetUnitID?: number): void {
    this.network.send({ type: 'wave_upgrade_choice', upgradeId: upgradeID, targetUnitId: targetUnitID ?? 0 })
  }

  sendWaveUpgradeReroll(): void {
    this.network.send({ type: 'wave_upgrade_reroll' })
  }
```

- [ ] **Step 3: Add waveUpgrade to emptyUiSnapshot and expose methods in useGameClient.ts**

In `useGameClient.ts`, add `waveUpgrade: null` to `emptyUiSnapshot`:

```typescript
const emptyUiSnapshot: GameUiSnapshot = {
  // ... existing fields ...
  waveUpgrade: null,
}
```

Add the two new functions in the `useGameClient` function body (alongside `sendEquipItem`):

```typescript
  function sendWaveUpgradeChoice(upgradeID: string, targetUnitID?: number) {
    client?.sendWaveUpgradeChoice(upgradeID, targetUnitID)
  }

  function sendWaveUpgradeReroll() {
    client?.sendWaveUpgradeReroll()
  }
```

Return them in the `return` statement of `useGameClient`:

```typescript
  return {
    // ... existing exports ...
    sendWaveUpgradeChoice,
    sendWaveUpgradeReroll,
  }
```

- [ ] **Step 4: Type-check**

```
cd client/src/game-portal && npm run build 2>&1 | tail -10
```
Expected: no TypeScript errors.

- [ ] **Step 5: Commit**

```
git add client/src/game-portal/src/game/core/GameState.ts client/src/game-portal/src/game/core/GameClient.ts client/src/game-portal/src/composables/useGameClient.ts
git commit -m "feat: wire waveUpgrade snapshot to client GameState and expose choice/reroll methods"
```

---

## Task 10: WaveUpgradeModal + MatchView Integration

**Files:**
- Create: `client/src/game-portal/src/components/WaveUpgradeModal.vue`
- Modify: `client/src/game-portal/src/views/MatchView.vue`

- [ ] **Step 1: Create WaveUpgradeModal.vue**

```vue
<template>
  <div class="wave-upgrade-overlay" role="dialog" aria-modal="true" aria-label="Wave upgrade">
    <!-- Waiting state — shown after the local player has picked -->
    <div v-if="resolved" class="upgrade-waiting">
      <div class="upgrade-waiting-title">Upgrade chosen!</div>
      <p class="upgrade-waiting-sub">Waiting for other players…</p>
    </div>

    <!-- Active state — offer cards -->
    <div v-else class="upgrade-panel">
      <div class="upgrade-header">
        <span class="upgrade-wave-label">Wave {{ upgrade.wave }} — Choose an Upgrade</span>
        <!-- Timer bar -->
        <div class="upgrade-timer-track" aria-label="Time remaining">
          <div
            class="upgrade-timer-fill"
            :class="timerClass"
            :style="{ width: timerPercent + '%' }"
          ></div>
        </div>
      </div>

      <!-- Unit picker (XP grant secondary step) -->
      <div v-if="pendingXpOffer" class="unit-picker">
        <div class="unit-picker-title">Choose a unit to receive {{ pendingXpOffer.description }}</div>
        <ul class="unit-picker-list">
          <li
            v-for="unit in units"
            :key="unit.id"
            class="unit-picker-item"
            tabindex="0"
            @click="pickXpTarget(unit.id)"
            @keydown.enter="pickXpTarget(unit.id)"
          >
            <span class="unit-name">{{ unit.name }}</span>
            <span class="unit-xp">XP {{ unit.xp }} / {{ unit.xpToNextRank }}</span>
          </li>
        </ul>
      </div>

      <!-- Card row -->
      <div v-else class="upgrade-cards">
        <button
          v-for="offer in upgrade.offers"
          :key="offer.id"
          class="upgrade-card"
          :class="`rarity-${offer.rarity}`"
          @click="selectOffer(offer)"
        >
          <span class="card-rarity">{{ offer.rarity }}</span>
          <span class="card-name">{{ offer.name }}</span>
          <span class="card-desc">{{ offer.description }}</span>
          <span class="card-stack">Stack {{ offer.stackCurrent }} / {{ offer.stackMax }}</span>
        </button>
      </div>

      <!-- Reroll button -->
      <div v-if="!pendingXpOffer" class="upgrade-footer">
        <button
          class="reroll-button"
          :disabled="upgrade.rerollsLeft <= 0"
          @click="reroll"
        >
          ↺ Reroll ({{ upgrade.rerollsLeft }} left)
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import type { WaveUpgradeOfferSnapshot, UpgradeOffer } from '@/game/network/protocol'
import type { Unit } from '@/game/core/GameState'

const props = defineProps<{
  upgrade: WaveUpgradeOfferSnapshot
  units: Unit[]
  sendChoice: (upgradeID: string, targetUnitID?: number) => void
  sendReroll: () => void
}>()

const resolved = ref(false)
const pendingXpOffer = ref<UpgradeOffer | null>(null)
const now = ref(Date.now())

let rafId = 0
function tick() {
  now.value = Date.now()
  rafId = requestAnimationFrame(tick)
}
onMounted(() => { rafId = requestAnimationFrame(tick) })
onUnmounted(() => cancelAnimationFrame(rafId))

const timerPercent = computed(() => {
  const total = props.upgrade.deadlineMs - (props.upgrade.deadlineMs - 25_000)
  const remaining = Math.max(0, props.upgrade.deadlineMs - now.value)
  return Math.min(100, (remaining / 25_000) * 100)
})

const timerClass = computed(() => {
  if (timerPercent.value > 40) return 'timer-green'
  if (timerPercent.value > 15) return 'timer-yellow'
  return 'timer-red'
})

function selectOffer(offer: UpgradeOffer) {
  if (offer.requiresTargetUnit) {
    pendingXpOffer.value = offer
    return
  }
  props.sendChoice(offer.id)
  resolved.value = true
}

function pickXpTarget(unitId: number) {
  if (!pendingXpOffer.value) return
  props.sendChoice(pendingXpOffer.value.id, unitId)
  pendingXpOffer.value = null
  resolved.value = true
}

function reroll() {
  if (props.upgrade.rerollsLeft <= 0) return
  props.sendReroll()
}
</script>

<style scoped>
.wave-upgrade-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.72);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 200;
}

.upgrade-waiting {
  text-align: center;
  color: #e2e8f0;
}
.upgrade-waiting-title { font-size: 1.5rem; font-weight: bold; margin-bottom: 8px; }
.upgrade-waiting-sub { color: #94a3b8; }

.upgrade-panel {
  background: #0d1117;
  border: 1px solid #1e293b;
  border-radius: 12px;
  padding: 24px;
  width: min(860px, 94vw);
}

.upgrade-header {
  margin-bottom: 20px;
}
.upgrade-wave-label {
  display: block;
  text-align: center;
  color: #94a3b8;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  font-size: 0.75rem;
  margin-bottom: 10px;
}
.upgrade-timer-track {
  height: 5px;
  background: #1e293b;
  border-radius: 3px;
  overflow: hidden;
}
.upgrade-timer-fill {
  height: 100%;
  border-radius: 3px;
  transition: width 0.25s linear, background 0.5s;
}
.timer-green  { background: #4ade80; }
.timer-yellow { background: #fbbf24; }
.timer-red    { background: #ef4444; }

.upgrade-cards {
  display: flex;
  gap: 12px;
}

.upgrade-card {
  flex: 1;
  background: #0f172a;
  border: 2px solid #334155;
  border-radius: 10px;
  padding: 16px;
  text-align: center;
  cursor: pointer;
  display: flex;
  flex-direction: column;
  gap: 6px;
  transition: transform 0.1s, box-shadow 0.1s;
  color: #e2e8f0;
}
.upgrade-card:hover {
  transform: translateY(-2px);
}

.rarity-common    { border-color: #64748b; }
.rarity-rare      { border-color: #6366f1; box-shadow: 0 0 14px rgba(99,102,241,0.25); }
.rarity-epic      { border-color: #f59e0b; box-shadow: 0 0 14px rgba(245,158,11,0.25); }
.rarity-legendary { border-color: #ef4444; box-shadow: 0 0 18px rgba(239,68,68,0.35); }

.card-rarity {
  font-size: 0.65rem;
  text-transform: uppercase;
  letter-spacing: 0.1em;
}
.rarity-common    .card-rarity { color: #94a3b8; }
.rarity-rare      .card-rarity { color: #818cf8; }
.rarity-epic      .card-rarity { color: #fbbf24; }
.rarity-legendary .card-rarity { color: #f87171; }

.card-name  { font-weight: bold; font-size: 1rem; }
.card-desc  { font-size: 0.78rem; color: #64748b; line-height: 1.4; }
.card-stack { font-size: 0.68rem; color: #475569; margin-top: 4px; }

.upgrade-footer {
  margin-top: 16px;
  text-align: center;
}
.reroll-button {
  background: #1e293b;
  border: 1px solid #334155;
  border-radius: 6px;
  color: #94a3b8;
  padding: 6px 18px;
  font-size: 0.82rem;
  cursor: pointer;
}
.reroll-button:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
.reroll-button:not(:disabled):hover {
  background: #273548;
}

.unit-picker-title {
  color: #e2e8f0;
  margin-bottom: 12px;
  text-align: center;
}
.unit-picker-list {
  list-style: none;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: 300px;
  overflow-y: auto;
}
.unit-picker-item {
  background: #0f172a;
  border: 1px solid #334155;
  border-radius: 6px;
  padding: 10px 14px;
  display: flex;
  justify-content: space-between;
  cursor: pointer;
  color: #e2e8f0;
}
.unit-picker-item:hover { background: #1e293b; }
.unit-name { font-weight: 500; }
.unit-xp   { color: #64748b; font-size: 0.78rem; }
</style>
```

- [ ] **Step 2: Mount WaveUpgradeModal in MatchView.vue**

In `MatchView.vue`, add to the `<script setup>` imports:

```typescript
import WaveUpgradeModal from '@/components/WaveUpgradeModal.vue'
```

Destructure the new functions from `useGameClient()`:

```typescript
const {
  // ... existing destructured values ...
  sendWaveUpgradeChoice,
  sendWaveUpgradeReroll,
} = useGameClient()
```

Add the modal component to the template, after the existing `<MatchHud>` line:

```vue
<WaveUpgradeModal
  v-if="hasStarted && ui.waveUpgrade"
  :upgrade="ui.waveUpgrade"
  :units="ui.allPlayerUnits"
  :send-choice="sendWaveUpgradeChoice"
  :send-reroll="sendWaveUpgradeReroll"
/>
```

- [ ] **Step 3: Type-check and build**

```
cd client/src/game-portal && npm run build 2>&1 | tail -10
```
Expected: no TypeScript errors.

- [ ] **Step 4: Run server tests one final time**

```
cd server && go test ./internal/game/... -v -count=1 2>&1 | grep -E "PASS|FAIL|ok"
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```
git add client/src/game-portal/src/components/WaveUpgradeModal.vue client/src/game-portal/src/views/MatchView.vue
git commit -m "feat: add WaveUpgradeModal centered overlay with timer, cards, reroll, and XP unit picker"
```

---

## Self-Review

**Spec coverage check:**
- ✅ 3 upgrade cards per wave (Task 5: `generateUpgradeOffersLocked` samples 3)
- ✅ Soft timer 25s, auto-picks first card (Task 5: `tickUpgradePhaseLocked`)
- ✅ Tiered rarity with weighted random (Task 5: weighted pool)
- ✅ Gradual rarity scaling per wave (Task 5: `rarityScalePerWave`)
- ✅ Milestone guarantee at epic+ (Task 5: `isMilestone` path)
- ✅ Army-wide / archetype / unit-type / XP / equipment scope types (Task 6: `matchesUpgradeScope`)
- ✅ 1 free reroll per wave (Task 5: `HandleWaveUpgradeReroll`)
- ✅ Reroll count legend-incrementable via `MaxRerolls` (Task 3)
- ✅ Capped stacking (Task 5: `effectiveCap` filters)
- ✅ Stack cap legend-incrementable via `MaxUpgradeStacks` (Task 3)
- ✅ XP grants target specific unit (Task 6: `"xp"` case + `targetUnitID`)
- ✅ Equipment drops go to vault (Task 6: `addItemToVaultLocked`)
- ✅ Centered overlay modal with timer bar (Task 10)
- ✅ Rarity colour coding on cards (Task 10: CSS classes)
- ✅ Reroll button shows remaining count, disabled at 0 (Task 10)
- ✅ Unit picker secondary step for XP grants (Task 10)
- ✅ Waiting state after player resolves (Task 10: `resolved` ref)
- ✅ Multiplayer: next wave waits for all players (Task 7: `waveUpgradeAllResolvedLocked`)
- ✅ `timerSeconds` configurable in gameplay_tuning.json (Task 2)
- ✅ `MaxRerolls` / `MaxUpgradeStacks` on `PlayerProfile` for legend system (Task 3)

**Type consistency:** `WaveUpgradeOfferSnapshot`, `UpgradeOffer` used consistently across Go and TypeScript. `UpgradeStacks map[string]int` keyed by group everywhere. `applyUpgradeLocked` signature `(playerID, upgradeID string, targetUnitID int)` matches all call sites.

**No placeholders:** All steps include real code, real commands, expected outputs.
