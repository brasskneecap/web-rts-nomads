# Switchable XP Systems Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a server-wide switch (`classic` | `split`) where `split` mode distributes a per-enemy `experience` value evenly, as raw XP, among nearby/contributing friendly units — fully replacing classic payouts — while `classic` remains the default and byte-for-byte unchanged.

**Architecture:** A single new dispatcher `awardUnitDeathXPLocked(dead, killer)` replaces the invariant `awardKillXPLocked`+`payoutDamageDealtXPLocked` pair at every kill site. It branches on a global tuning field: `classic` calls the original two functions verbatim; `split` runs a new even-split algorithm. The two remaining payout functions (soldier-tank, building) get a one-line split guard and stay where they are.

**Tech Stack:** Go 1.x, standard library only (`math`, `sort`, `testing`). No client/protocol/persistence changes.

**Spec:** `docs/superpowers/specs/2026-05-19-switchable-xp-systems-design.md`

---

## File Structure

| File | Responsibility | Change |
|---|---|---|
| `server/internal/game/catalog/tuning/gameplay_tuning.json` | Tuning data | Add `experience` block |
| `server/internal/game/tuning_defs.go` | Tuning struct + validation | Add `ExperienceTuning`, field, validation |
| `server/internal/game/unit_defs.go` | Catalog unit schema | Add `Experience *int` |
| `server/internal/game/state.go` | `Unit` runtime struct | Add `XPValue int` |
| `server/internal/game/state_spawn.go` | Unit construction | Seed `XPValue` in both spawn paths |
| `server/internal/game/progression.go` | XP logic | Mode constants, `resolveUnitXPValue`, `addUnitXPRawFloatLocked`, `awardSplitDeathXPLocked`, `awardUnitDeathXPLocked`, split guards |
| `server/internal/game/damage_pipeline.go` | Kill sites (×2) | Replace pair |
| `server/internal/game/state_combat.go` | Kill sites (×3) | Replace pair |
| `server/internal/game/perks_attack.go` | Kill sites (×2) | Replace pair |
| `server/internal/game/perks_marksman.go` | Kill site (×1) | Replace pair |
| `server/internal/game/trap.go` | Kill sites (×11, pair-only) | Replace pair in place |
| `server/internal/game/experience_split_test.go` | New test file | Test seam + split tests |

**Working directory for all commands:** `c:/Personal Dev/webrts/server`

**Branch:** Already on `feature/switchable-xp-systems`.

---

## Task 1: Experience tuning config, struct, and validation

**Files:**
- Modify: `server/internal/game/catalog/tuning/gameplay_tuning.json`
- Modify: `server/internal/game/tuning_defs.go`
- Modify: `server/internal/game/progression.go` (add mode constants)
- Test: `server/internal/game/experience_split_test.go` (create)

- [ ] **Step 1: Add mode constants to progression.go**

In `server/internal/game/progression.go`, add to the existing `const` block that
starts at line 27 (the one containing `xpGainMultiplier`), immediately after the
`rankUpFxDurationSecs = 1.4` line:

```go
	// Experience-system selector values. The active mode is read from
	// gameplayTuning().Experience.Mode (catalog/tuning/gameplay_tuning.json).
	// "classic" = kill bonus + damage-dealt + soldier-tank payouts (legacy).
	// "split"   = a single per-enemy experience value, divided evenly among
	//             eligible recipients as raw XP, fully replacing the above.
	experienceModeClassic = "classic"
	experienceModeSplit   = "split"
```

- [ ] **Step 2: Add the `experience` block to gameplay_tuning.json**

In `server/internal/game/catalog/tuning/gameplay_tuning.json`, change the
`unitOverrides` line (line 30) from:

```json
  "unitOverrides": {}
}
```

to:

```json
  "unitOverrides": {},
  "experience": {
    "mode": "classic",
    "splitDefaultXP": 10,
    "splitEligibilityRadius": 500
  }
}
```

- [ ] **Step 3: Add `ExperienceTuning` struct, field, and validation to tuning_defs.go**

In `server/internal/game/tuning_defs.go`, add this struct after the
`UnitLegendPointOverride` struct (after line 55):

```go
// ExperienceTuning selects the experience-gaining system and tunes the
// "split" mode. Mode "classic" leaves all legacy payouts unchanged; "split"
// distributes each enemy's experience value evenly among eligible recipients.
type ExperienceTuning struct {
	// Mode is "classic" (legacy payouts) or "split" (even per-enemy split).
	Mode string `json:"mode"`
	// SplitDefaultXP is the experience used when an enemy's UnitDef omits the
	// "experience" field. Must be >= 0.
	SplitDefaultXP int `json:"splitDefaultXP"`
	// SplitEligibilityRadius is the proximity radius in world pixels, measured
	// from the dying unit at the moment of death. Must be > 0.
	SplitEligibilityRadius float64 `json:"splitEligibilityRadius"`
}
```

Add the field to `GameplayTuning` (after the `UnitOverrides` field, line 19):

```go
	Experience    ExperienceTuning                   `json:"experience"`
```

Add validation in `init()`, immediately before the final
`gameplayTuningSingleton = t` line (line 102):

```go
	switch t.Experience.Mode {
	case experienceModeClassic, experienceModeSplit:
	default:
		panic(fmt.Sprintf("catalog/tuning/gameplay_tuning.json: experience.mode must be %q or %q, got %q", experienceModeClassic, experienceModeSplit, t.Experience.Mode))
	}
	if t.Experience.SplitDefaultXP < 0 {
		panic(fmt.Sprintf("catalog/tuning/gameplay_tuning.json: experience.splitDefaultXP must be >= 0, got %d", t.Experience.SplitDefaultXP))
	}
	if t.Experience.SplitEligibilityRadius <= 0 {
		panic(fmt.Sprintf("catalog/tuning/gameplay_tuning.json: experience.splitEligibilityRadius must be > 0, got %v", t.Experience.SplitEligibilityRadius))
	}
```

- [ ] **Step 4: Write the failing test**

Create `server/internal/game/experience_split_test.go`:

```go
package game

import (
	"testing"
)

func TestExperienceTuning_DefaultsLoaded(t *testing.T) {
	et := gameplayTuning().Experience
	if et.Mode != experienceModeClassic {
		t.Errorf("default experience.mode = %q, want %q", et.Mode, experienceModeClassic)
	}
	if et.SplitDefaultXP != 10 {
		t.Errorf("default experience.splitDefaultXP = %d, want 10", et.SplitDefaultXP)
	}
	if et.SplitEligibilityRadius != 500 {
		t.Errorf("default experience.splitEligibilityRadius = %v, want 500", et.SplitEligibilityRadius)
	}
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/game/ -run TestExperienceTuning_DefaultsLoaded -v`
Expected: PASS. (The package would `panic` at `init` if the JSON/struct were
inconsistent, so a green run also proves validation accepts the shipped file.)

- [ ] **Step 6: Verify the whole game package still builds and passes**

Run: `go build ./... && go test ./internal/game/ -count=1`
Expected: build succeeds; all existing tests still pass (default mode is
`classic`, so nothing legacy changed).

- [ ] **Step 7: Commit**

```bash
git add server/internal/game/catalog/tuning/gameplay_tuning.json server/internal/game/tuning_defs.go server/internal/game/progression.go server/internal/game/experience_split_test.go
git commit -m "feat(xp): add experience-mode tuning (classic|split)"
```

---

## Task 2: `UnitDef.Experience` field and `Unit.XPValue` spawn seeding

**Files:**
- Modify: `server/internal/game/unit_defs.go:80` (after `LegendPointAmount`)
- Modify: `server/internal/game/state.go:100` (near `XP int`)
- Modify: `server/internal/game/state_spawn.go` (two spawn paths)
- Modify: `server/internal/game/progression.go` (add `resolveUnitXPValue`)
- Test: `server/internal/game/experience_split_test.go`

- [ ] **Step 1: Write the failing test**

Append to `server/internal/game/experience_split_test.go`:

```go
import "webrts/server/pkg/protocol" // add to the existing import block

func intPtr(v int) *int { return &v }

func TestResolveUnitXPValue(t *testing.T) {
	// Absent → splitDefaultXP (10 by shipped tuning).
	if got := resolveUnitXPValue(UnitDef{}); got != gameplayTuning().Experience.SplitDefaultXP {
		t.Errorf("absent experience: got %d, want %d", got, gameplayTuning().Experience.SplitDefaultXP)
	}
	// Explicit value honored.
	if got := resolveUnitXPValue(UnitDef{Experience: intPtr(7)}); got != 7 {
		t.Errorf("explicit 7: got %d, want 7", got)
	}
	// Explicit 0 honored (unit grants no XP) — NOT treated as absent.
	if got := resolveUnitXPValue(UnitDef{Experience: intPtr(0)}); got != 0 {
		t.Errorf("explicit 0: got %d, want 0", got)
	}
}

func TestSpawnSeedsXPValue(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()
	enemy := s.spawnEnemyUnitLocked("raider", protocol.Vec2{X: 100, Y: 100})
	if enemy == nil {
		t.Fatal("spawnEnemyUnitLocked returned nil")
	}
	if enemy.XPValue != gameplayTuning().Experience.SplitDefaultXP {
		t.Errorf("raider XPValue = %d, want %d (splitDefaultXP)", enemy.XPValue, gameplayTuning().Experience.SplitDefaultXP)
	}
}
```

(If `experience_split_test.go` already imports `testing` only, replace its
import block with:)

```go
import (
	"testing"

	"webrts/server/pkg/protocol"
)
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/game/ -run 'TestResolveUnitXPValue|TestSpawnSeedsXPValue' -v`
Expected: FAIL — `undefined: resolveUnitXPValue` and `enemy.XPValue undefined`.

- [ ] **Step 3: Add `Experience *int` to `UnitDef`**

In `server/internal/game/unit_defs.go`, immediately after the
`LegendPointAmount int` field (line 80) and before the blank line preceding
`VisionRange`:

```go
	// Experience is the raw XP this unit yields when killed in "split" mode
	// (catalog/tuning/gameplay_tuning.json experience.mode). Pointer so the
	// catalog can distinguish absent (→ splitDefaultXP) from an explicit 0
	// (unit grants no XP). Ignored entirely in "classic" mode.
	Experience *int `json:"experience,omitempty"`
```

- [ ] **Step 4: Add `XPValue int` to `Unit`**

In `server/internal/game/state.go`, immediately after `XP int` (line 100):

```go
	XPValue              int // raw XP yielded when killed in "split" mode; seeded at spawn
```

- [ ] **Step 5: Add `resolveUnitXPValue` to progression.go**

In `server/internal/game/progression.go`, add after `addUnitXPFloatLocked`
(after line 280):

```go
// resolveUnitXPValue returns the raw XP a unit of this def yields when killed
// in "split" mode. Absent experience falls back to the tuned default; an
// explicit 0 means the unit grants no XP. Mode-agnostic — the value is seeded
// at spawn and simply unused in "classic" mode.
func resolveUnitXPValue(def UnitDef) int {
	if def.Experience != nil {
		return *def.Experience
	}
	return gameplayTuning().Experience.SplitDefaultXP
}
```

- [ ] **Step 6: Seed `XPValue` in the def-based spawn path**

In `server/internal/game/state_spawn.go`, in `spawnUnitFromDefLocked`, add to
the `&Unit{...}` literal immediately after the `Rank: unitRankBase,` line
(line 83):

```go
		XPValue:            resolveUnitXPValue(def),
```

- [ ] **Step 7: Seed `XPValue` in the raider fallback spawn path**

In `server/internal/game/state_spawn.go`, in `spawnRaiderUnitLocked`, add to
the `&Unit{...}` literal immediately after the `Rank: unitRankBase,` line
(line 141). This path has no `UnitDef`, so use the tuned default directly:

```go
		XPValue:            gameplayTuning().Experience.SplitDefaultXP,
```

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/game/ -run 'TestResolveUnitXPValue|TestSpawnSeedsXPValue' -v`
Expected: PASS.

- [ ] **Step 9: Full regression**

Run: `go build ./... && go test ./internal/game/ -count=1`
Expected: build + all tests pass.

- [ ] **Step 10: Commit**

```bash
git add server/internal/game/unit_defs.go server/internal/game/state.go server/internal/game/state_spawn.go server/internal/game/progression.go server/internal/game/experience_split_test.go
git commit -m "feat(xp): add UnitDef.Experience and seed Unit.XPValue at spawn"
```

---

## Task 3: `addUnitXPRawFloatLocked` (raw, unscaled float XP)

**Files:**
- Modify: `server/internal/game/progression.go` (after `addUnitXPFloatLocked`)
- Test: `server/internal/game/experience_split_test.go`

- [ ] **Step 1: Write the failing test**

Append to `server/internal/game/experience_split_test.go`:

```go
func TestAddUnitXPRawFloat_NoMultiplierAndAccumulates(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 100, Y: 100})

	// 0.5 alone must not yet count as whole XP, but must be retained.
	s.addUnitXPRawFloatLocked(u, 0.5)
	if u.XP != 0 {
		t.Errorf("after +0.5: XP = %d, want 0", u.XP)
	}
	if u.XPProgressRemainder != 0.5 {
		t.Errorf("after +0.5: remainder = %v, want 0.5", u.XPProgressRemainder)
	}

	// Another 0.5 completes a whole point — RAW, with NO 0.2 scaling applied.
	s.addUnitXPRawFloatLocked(u, 0.5)
	if u.XP != 1 {
		t.Errorf("after +0.5 again: XP = %d, want 1 (raw, unscaled)", u.XP)
	}
	if u.XPProgressRemainder != 0 {
		t.Errorf("after +0.5 again: remainder = %v, want 0", u.XPProgressRemainder)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/game/ -run TestAddUnitXPRawFloat_NoMultiplierAndAccumulates -v`
Expected: FAIL — `undefined: s.addUnitXPRawFloatLocked`.

- [ ] **Step 3: Implement `addUnitXPRawFloatLocked`**

In `server/internal/game/progression.go`, add immediately after
`addUnitXPFloatLocked` (after line 280, before `assignUnitPathOnRankUpLocked`):

```go
// addUnitXPRawFloatLocked is addUnitXPFloatLocked WITHOUT the xpGainMultiplier
// scaling: `amount` is the literal XP, accumulated through the same per-unit
// XPProgressRemainder so sub-1 fractions (e.g. 0.5) eventually form whole XP
// and cross rank thresholds. Used only by "split" mode. Because exactly one
// mode is active per server run, scaled (addUnitXPFloatLocked) and raw
// contributions never mix into the same accumulator.
func (s *GameState) addUnitXPRawFloatLocked(unit *Unit, amount float64) {
	if !s.unitCanGainXPLocked(unit) || amount <= 0 {
		return
	}
	total := unit.XPProgressRemainder + amount
	wholeXP := int(math.Floor(total))
	unit.XPProgressRemainder = total - float64(wholeXP)
	if wholeXP <= 0 {
		return
	}
	s.addUnitXPLocked(unit, wholeXP)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/game/ -run TestAddUnitXPRawFloat_NoMultiplierAndAccumulates -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/game/progression.go server/internal/game/experience_split_test.go
git commit -m "feat(xp): add addUnitXPRawFloatLocked for unscaled split XP"
```

---

## Task 4: `awardSplitDeathXPLocked` (split algorithm) + test seam

**Files:**
- Modify: `server/internal/game/progression.go` (add function + `sort` import)
- Test: `server/internal/game/experience_split_test.go` (test seam + cases)

- [ ] **Step 1: Add the test seam**

Append to `server/internal/game/experience_split_test.go`:

```go
// withExperienceTuning swaps the global Experience tuning for the duration of
// the test and restores it via Cleanup. Mutates a package singleton, so tests
// using it MUST NOT call t.Parallel().
func withExperienceTuning(t *testing.T, et ExperienceTuning) {
	t.Helper()
	prev := gameplayTuningSingleton.Experience
	gameplayTuningSingleton.Experience = et
	t.Cleanup(func() { gameplayTuningSingleton.Experience = prev })
}

func splitTuning(radius float64) ExperienceTuning {
	return ExperienceTuning{Mode: experienceModeSplit, SplitDefaultXP: 10, SplitEligibilityRadius: radius}
}
```

- [ ] **Step 2: Write the failing tests**

Append to `server/internal/game/experience_split_test.go`:

```go
func TestSplit_EvenDivisionAmongInRangeUnits(t *testing.T) {
	withExperienceTuning(t, splitTuning(500))
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 1000, Y: 1000})
	enemy.XPValue = 10

	var recips []*Unit
	for i := 0; i < 4; i++ {
		u := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 1000 + float64(i*10), Y: 1000})
		recips = append(recips, u)
	}

	s.awardSplitDeathXPLocked(enemy)

	// 10 / 4 = 2.5 each → 2 whole XP + 0.5 remainder.
	for i, u := range recips {
		if u.XP != 2 || u.XPProgressRemainder != 0.5 {
			t.Errorf("recipient %d: XP=%d remainder=%v, want XP=2 remainder=0.5", i, u.XP, u.XPProgressRemainder)
		}
	}
}

func TestSplit_FractionAccumulatesOverManyKills(t *testing.T) {
	withExperienceTuning(t, splitTuning(500))
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	// 20 recipients, enemy worth 10 → 0.5 each per kill.
	var recips []*Unit
	for i := 0; i < 20; i++ {
		recips = append(recips, s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 1000 + float64(i), Y: 1000}))
	}
	for k := 0; k < 4; k++ {
		enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 1005, Y: 1000})
		enemy.XPValue = 10
		s.awardSplitDeathXPLocked(enemy)
	}
	// 4 kills × 0.5 = 2.0 whole XP, 0 remainder.
	for i, u := range recips {
		if u.XP != 2 || u.XPProgressRemainder != 0 {
			t.Errorf("recipient %d: XP=%d remainder=%v, want XP=2 remainder=0", i, u.XP, u.XPProgressRemainder)
		}
	}
}

func TestSplit_OutOfRangeContributorStillEligible(t *testing.T) {
	withExperienceTuning(t, splitTuning(100)) // tight radius
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 1000, Y: 1000})
	enemy.XPValue = 8

	near := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 1010, Y: 1000}) // within 100
	far := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 5000, Y: 5000})  // far away
	// `far` dealt damage at some point → recorded on the enemy's ledger.
	s.recordDamageDealtLocked(far, enemy, 3)

	s.awardSplitDeathXPLocked(enemy)

	// Eligible set = {near (proximity), far (contributor)} → 8/2 = 4 each.
	if near.XP != 4 {
		t.Errorf("near.XP = %d, want 4", near.XP)
	}
	if far.XP != 4 {
		t.Errorf("far.XP = %d, want 4 (contributor despite being out of range)", far.XP)
	}
}

func TestSplit_DeadContributorExcluded(t *testing.T) {
	withExperienceTuning(t, splitTuning(100))
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 1000, Y: 1000})
	enemy.XPValue = 10
	alive := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 1010, Y: 1000})
	dead := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 1010, Y: 1010})
	s.recordDamageDealtLocked(dead, enemy, 5)
	dead.HP = 0 // unitCanGainXPLocked must exclude it

	s.awardSplitDeathXPLocked(enemy)

	if alive.XP != 10 {
		t.Errorf("alive.XP = %d, want 10 (sole eligible recipient)", alive.XP)
	}
	if dead.XP != 0 {
		t.Errorf("dead.XP = %d, want 0 (excluded)", dead.XP)
	}
}

func TestSplit_NoEligibleRecipients_XPLost(t *testing.T) {
	withExperienceTuning(t, splitTuning(50))
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 1000, Y: 1000})
	enemy.XPValue = 10
	farAlly := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 9000, Y: 9000})

	s.awardSplitDeathXPLocked(enemy) // must not panic; nobody gains

	if farAlly.XP != 0 || farAlly.XPProgressRemainder != 0 {
		t.Errorf("farAlly gained XP (%d / %v); want none — XP should be lost", farAlly.XP, farAlly.XPProgressRemainder)
	}
}

func TestSplit_ZeroXPValue_NoAward(t *testing.T) {
	withExperienceTuning(t, splitTuning(500))
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 1000, Y: 1000})
	enemy.XPValue = 0 // explicit "no XP"
	ally := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 1005, Y: 1000})

	s.awardSplitDeathXPLocked(enemy)

	if ally.XP != 0 || ally.XPProgressRemainder != 0 {
		t.Errorf("ally gained XP from a 0-XP enemy (%d / %v)", ally.XP, ally.XPProgressRemainder)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/game/ -run 'TestSplit_' -v`
Expected: FAIL — `undefined: s.awardSplitDeathXPLocked`.

- [ ] **Step 4: Add the `sort` import to progression.go**

In `server/internal/game/progression.go`, change the import block (lines 3-5)
from:

```go
import (
	"math"
)
```

to:

```go
import (
	"math"
	"sort"
)
```

- [ ] **Step 5: Implement `awardSplitDeathXPLocked`**

In `server/internal/game/progression.go`, add immediately after
`awardKillXPLocked` (after line 513):

```go
// awardSplitDeathXPLocked distributes a dead enemy's raw XPValue evenly among
// every eligible recipient: friendly units (per unitCanGainXPLocked) either
// within SplitEligibilityRadius of the death position OR that ever dealt
// damage to it. No eligible recipients ⇒ the XP is lost (no killer fallback).
// Used only in "split" mode. Recipient IDs are sorted before payout so the
// distribution is deterministic regardless of map iteration order (per the
// determinism invariant) — order does not change the equal share anyway.
func (s *GameState) awardSplitDeathXPLocked(dead *Unit) {
	if dead == nil || dead.XPValue <= 0 {
		return
	}

	recipients := map[int]*Unit{}

	// Proximity: any eligible unit within the radius of the death position.
	radius := gameplayTuning().Experience.SplitEligibilityRadius
	radiusSq := radius * radius
	for _, u := range s.Units {
		if u == nil || !s.unitCanGainXPLocked(u) {
			continue
		}
		dx := u.X - dead.X
		dy := u.Y - dead.Y
		if dx*dx+dy*dy <= radiusSq {
			recipients[u.ID] = u
		}
	}

	// Contributors: any unit that ever dealt damage to this enemy. The ledger
	// is populated in every mode by recordDamageDealtLocked.
	for attackerID := range dead.DamageDealtByUnit {
		if _, seen := recipients[attackerID]; seen {
			continue
		}
		attacker := s.getUnitByIDLocked(attackerID)
		if attacker == nil || !s.unitCanGainXPLocked(attacker) {
			continue
		}
		recipients[attackerID] = attacker
	}

	if len(recipients) == 0 {
		return // no eligible recipients → XP is lost
	}

	ids := make([]int, 0, len(recipients))
	for id := range recipients {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	share := float64(dead.XPValue) / float64(len(ids))
	for _, id := range ids {
		s.addUnitXPRawFloatLocked(recipients[id], share)
	}
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/game/ -run 'TestSplit_' -v`
Expected: PASS (all six).

- [ ] **Step 7: Full regression**

Run: `go test ./internal/game/ -count=1`
Expected: all tests pass (legacy still on `classic`).

- [ ] **Step 8: Commit**

```bash
git add server/internal/game/progression.go server/internal/game/experience_split_test.go
git commit -m "feat(xp): implement awardSplitDeathXPLocked even-split algorithm"
```

---

## Task 5: `awardUnitDeathXPLocked` dispatcher + split guards

**Files:**
- Modify: `server/internal/game/progression.go` (dispatcher + 2 guards)
- Test: `server/internal/game/experience_split_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `server/internal/game/experience_split_test.go`:

```go
func TestDispatcher_ClassicReproducesPair(t *testing.T) {
	// Default tuning = classic. awardUnitDeathXPLocked(dead, killer) must equal
	// the legacy pair: killer gets the kill bonus, contributors get damage XP.
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	killer := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 100, Y: 100})
	dead := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 120, Y: 100})
	contributor := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 140, Y: 100})
	s.recordDamageDealtLocked(contributor, dead, 30)

	// Baseline reference: a second, identical setup run through the legacy pair.
	rs := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	rs.mu.Lock()
	rk := rs.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 100, Y: 100})
	rd := rs.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 120, Y: 100})
	rc := rs.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 140, Y: 100})
	rs.recordDamageDealtLocked(rc, rd, 30)
	rs.awardKillXPLocked(rk)
	rs.payoutDamageDealtXPLocked(rd)
	rs.mu.Unlock()

	s.awardUnitDeathXPLocked(dead, killer)

	if killer.XP != rk.XP || killer.XPProgressRemainder != rk.XPProgressRemainder {
		t.Errorf("killer XP %d/%v != legacy %d/%v", killer.XP, killer.XPProgressRemainder, rk.XP, rk.XPProgressRemainder)
	}
	if contributor.XP != rc.XP || contributor.XPProgressRemainder != rc.XPProgressRemainder {
		t.Errorf("contributor XP %d/%v != legacy %d/%v", contributor.XP, contributor.XPProgressRemainder, rc.XP, rc.XPProgressRemainder)
	}
}

func TestDispatcher_SplitRoutesAndSuppressesTank(t *testing.T) {
	withExperienceTuning(t, splitTuning(500))
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	dead := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 1000, Y: 1000})
	dead.XPValue = 10
	killer := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 1005, Y: 1000})

	// A soldier that tanked damage from `dead`. In split mode the tank payout
	// must be suppressed — this unit only earns its split share (it is in range).
	tanker := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 1010, Y: 1000})
	s.recordSoldierTankContributionLocked(dead, tanker, 100)

	s.awardUnitDeathXPLocked(dead, killer)
	s.awardSoldierTankKillXPLocked(dead.ID) // guarded → no-op in split

	// killer + tanker both in range → 10/2 = 5 each. Tank payout added nothing.
	if killer.XP != 5 {
		t.Errorf("killer.XP = %d, want 5 (split share only)", killer.XP)
	}
	if tanker.XP != 5 {
		t.Errorf("tanker.XP = %d, want 5 (split share only; tank payout suppressed)", tanker.XP)
	}
}
```

> Note: `recordSoldierTankContributionLocked(target, attacker, damage)` records
> that `attacker` tanked damage *from* `target`; here `dead` is the damage
> source and `tanker` the soldier. Confirm argument order against
> `progression.go:495` when implementing the test.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/game/ -run 'TestDispatcher_' -v`
Expected: FAIL — `undefined: s.awardUnitDeathXPLocked`.

- [ ] **Step 3: Implement the dispatcher**

In `server/internal/game/progression.go`, add immediately after
`awardSplitDeathXPLocked` (from Task 4):

```go
// awardUnitDeathXPLocked is the single entry point for "a unit just died,
// settle its XP". It replaces the legacy awardKillXPLocked+payoutDamageDealtXPLocked
// pair at every kill site. `killer` may be nil (matching the legacy pair's
// nil-safety) and is ignored in split mode.
//
//   - classic: verbatim relocation of the legacy pair, in the original order.
//   - split:   even per-enemy split (killer intentionally unused).
func (s *GameState) awardUnitDeathXPLocked(dead, killer *Unit) {
	if dead == nil {
		return
	}
	if gameplayTuning().Experience.Mode == experienceModeSplit {
		s.awardSplitDeathXPLocked(dead)
		return
	}
	if killer != nil {
		s.awardKillXPLocked(killer)
	}
	s.payoutDamageDealtXPLocked(dead)
}
```

- [ ] **Step 4: Add the split guard to `awardSoldierTankKillXPLocked`**

In `server/internal/game/progression.go`, `awardSoldierTankKillXPLocked`
(line 515) currently starts:

```go
func (s *GameState) awardSoldierTankKillXPLocked(defeatedUnitID int) {
	if defeatedUnitID == 0 {
		return
	}
```

Change it to:

```go
func (s *GameState) awardSoldierTankKillXPLocked(defeatedUnitID int) {
	if gameplayTuning().Experience.Mode == experienceModeSplit {
		return // split mode fully replaces classic payouts
	}
	if defeatedUnitID == 0 {
		return
	}
```

- [ ] **Step 5: Add the split guard to `payoutBuildingDamageDealtXPLocked`**

In `server/internal/game/progression.go`, `payoutBuildingDamageDealtXPLocked`
(line 480) currently starts:

```go
func (s *GameState) payoutBuildingDamageDealtXPLocked(buildingID string) {
	m, ok := s.buildingDamageDealt[buildingID]
	if !ok {
		return
	}
```

Change it to:

```go
func (s *GameState) payoutBuildingDamageDealtXPLocked(buildingID string) {
	if gameplayTuning().Experience.Mode == experienceModeSplit {
		return // buildings grant no XP in split mode
	}
	m, ok := s.buildingDamageDealt[buildingID]
	if !ok {
		return
	}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/game/ -run 'TestDispatcher_|TestSplit_|TestExperienceTuning_|TestAddUnitXPRawFloat|TestResolveUnitXPValue|TestSpawnSeedsXPValue' -v`
Expected: PASS.

- [ ] **Step 7: Full regression (classic still default → suite unchanged)**

Run: `go test ./internal/game/ -count=1`
Expected: all tests pass.

- [ ] **Step 8: Commit**

```bash
git add server/internal/game/progression.go server/internal/game/experience_split_test.go
git commit -m "feat(xp): add awardUnitDeathXPLocked dispatcher + split guards"
```

---

## Task 6: Convert combat & perk kill sites to the dispatcher

At each site the legacy block is three adjacent lines (same indentation):

```go
s.awardKillXPLocked(<KILLER>)
s.payoutDamageDealtXPLocked(<DEAD>)
s.awardSoldierTankKillXPLocked(<DEAD>.ID)
```

Replace **only the first two** with one call; **keep the third line exactly as
is** (it is now split-guarded):

```go
s.awardUnitDeathXPLocked(<DEAD>, <KILLER>)
s.awardSoldierTankKillXPLocked(<DEAD>.ID)
```

Preserve the existing leading tab indentation of each line.

**Files & exact site map:**

| File | Approx. lines | `<DEAD>` | `<KILLER>` |
|---|---|---|---|
| `server/internal/game/damage_pipeline.go` | 119–121 | `target` | `attackerUnit` |
| `server/internal/game/damage_pipeline.go` | 153–155 | `target` | `ownerUnit` |
| `server/internal/game/state_combat.go` | 255–257 | `attacker` | `target` |
| `server/internal/game/state_combat.go` | 271–273 | `target` | `attacker` |
| `server/internal/game/state_combat.go` | 319–321 | `u` | `attacker` |
| `server/internal/game/perks_attack.go` | 231–233 | `candidate` | `attacker` |
| `server/internal/game/perks_attack.go` | 310–312 | `secondary` | `attacker` |
| `server/internal/game/perks_marksman.go` | 595–597 | `candidate` | `attacker` |

> The state_combat.go:255 site is the reflect-death case: the original code is
> `awardKillXPLocked(target)` / `payoutDamageDealtXPLocked(attacker)` /
> `awardSoldierTankKillXPLocked(attacker.ID)`, so `<DEAD>=attacker`,
> `<KILLER>=target` → `s.awardUnitDeathXPLocked(attacker, target)`.

- [ ] **Step 1: Edit `damage_pipeline.go` site 1 (≈119)**

Replace:
```go
					s.awardKillXPLocked(attackerUnit)
					s.payoutDamageDealtXPLocked(target)
```
with:
```go
					s.awardUnitDeathXPLocked(target, attackerUnit)
```
Leave the following `s.awardSoldierTankKillXPLocked(target.ID)` line unchanged.

- [ ] **Step 2: Edit `damage_pipeline.go` site 2 (≈153)**

Replace:
```go
						s.awardKillXPLocked(ownerUnit)
						s.payoutDamageDealtXPLocked(target)
```
with:
```go
						s.awardUnitDeathXPLocked(target, ownerUnit)
```
Leave the following `s.awardSoldierTankKillXPLocked(target.ID)` line unchanged.

- [ ] **Step 3: Edit `state_combat.go` site 1 — reflect death (≈255)**

Replace:
```go
		s.awardKillXPLocked(target)
		s.payoutDamageDealtXPLocked(attacker)
```
with:
```go
		s.awardUnitDeathXPLocked(attacker, target)
```
Leave the following `s.awardSoldierTankKillXPLocked(attacker.ID)` line unchanged.

- [ ] **Step 4: Edit `state_combat.go` site 2 (≈271)**

Replace:
```go
		s.awardKillXPLocked(attacker)
		s.payoutDamageDealtXPLocked(target)
```
with:
```go
		s.awardUnitDeathXPLocked(target, attacker)
```
Leave the following `s.awardSoldierTankKillXPLocked(target.ID)` line unchanged.

- [ ] **Step 5: Edit `state_combat.go` site 3 — splash (≈319)**

Replace:
```go
				s.awardKillXPLocked(attacker)
				s.payoutDamageDealtXPLocked(u)
```
with:
```go
				s.awardUnitDeathXPLocked(u, attacker)
```
Leave the following `s.awardSoldierTankKillXPLocked(u.ID)` line unchanged.

- [ ] **Step 6: Edit `perks_attack.go` site 1 (≈231)**

Replace:
```go
				s.awardKillXPLocked(attacker)
				s.payoutDamageDealtXPLocked(candidate)
```
with:
```go
				s.awardUnitDeathXPLocked(candidate, attacker)
```
Leave the following `s.awardSoldierTankKillXPLocked(candidate.ID)` line unchanged.

- [ ] **Step 7: Edit `perks_attack.go` site 2 (≈310)**

Replace:
```go
		s.awardKillXPLocked(attacker)
		s.payoutDamageDealtXPLocked(secondary)
```
with:
```go
		s.awardUnitDeathXPLocked(secondary, attacker)
```
Leave the following `s.awardSoldierTankKillXPLocked(secondary.ID)` line unchanged.

- [ ] **Step 8: Edit `perks_marksman.go` (≈595)**

Replace:
```go
				s.awardKillXPLocked(attacker)
				s.payoutDamageDealtXPLocked(candidate)
```
with:
```go
				s.awardUnitDeathXPLocked(candidate, attacker)
```
Leave the following `s.awardSoldierTankKillXPLocked(candidate.ID)` line unchanged.

- [ ] **Step 9: Build and full regression**

Run: `go build ./... && go test ./internal/game/ -count=1`
Expected: build + all tests pass. (Default `classic` ⇒ the dispatcher's classic
branch reproduces the pair exactly; the existing suite is the regression check.)

- [ ] **Step 10: Commit**

```bash
git add server/internal/game/damage_pipeline.go server/internal/game/state_combat.go server/internal/game/perks_attack.go server/internal/game/perks_marksman.go
git commit -m "refactor(xp): route combat/perk kill sites through awardUnitDeathXPLocked"
```

---

## Task 7: Convert trap.go kill sites (pair-only, in place)

Every trap site is a **pair only** (no soldier-tank line), nested inside an
existing `if <OWNER> != nil {` guard:

```go
if <OWNER> != nil {
	s.awardKillXPLocked(<OWNER>)
	s.payoutDamageDealtXPLocked(<DEAD>)
}
```

Replace the two inner lines with one, **leaving the `if <OWNER> != nil {`
guard, braces, and all surrounding lines (trackBattleKill, deadUnitIDs,
`continue`) exactly as they are**:

```go
if <OWNER> != nil {
	s.awardUnitDeathXPLocked(<DEAD>, <OWNER>)
}
```

> **Do NOT lift the call out of the `if <OWNER> != nil` guard.** Keeping it
> inside both (a) preserves classic byte-for-byte (today, when the trapper is
> dead, neither payout runs) and (b) realizes the spec decision that a trap
> kill whose trapper is dead yields no split XP — the XP is lost. This is
> intentional, per the approved spec's "No recipients → XP lost" decision.

**Exact site map (variable names verified against current source):**

| File:line (approx) | `<OWNER>` | `<DEAD>` |
|---|---|---|
| `trap.go`:350–351 | `ownerUnit` | `unit` |
| `trap.go`:384–385 | `ownerUnit` | `unit` |
| `trap.go`:418–419 | `ownerUnit` | `unit` |
| `trap.go`:526–527 | `ownerUnit` | `unit` |
| `trap.go`:812–813 | `owner` | `unit` |
| `trap.go`:1270–1271 | `ownerUnit` | `unit` |
| `trap.go`:1325–1326 | `ownerUnit` | `unit` |
| `trap.go`:1382–1383 | `ownerUnit` | `u` |
| `trap.go`:1446–1447 | `owner` | `u` |
| `trap.go`:1629–1630 | `ownerUnit` | `victim` |
| `trap.go`:1662–1663 | `ownerUnit` | `victim` |

- [ ] **Step 1: Edit each trap.go site**

For each row above, find the adjacent pair
`s.awardKillXPLocked(<OWNER>)` + `s.payoutDamageDealtXPLocked(<DEAD>)`
(preserving its leading tabs) and replace the two lines with the single line
`s.awardUnitDeathXPLocked(<DEAD>, <OWNER>)`. Because the pair text repeats,
edit by anchoring on enough surrounding context (the enclosing
`if <OWNER> != nil {` and the following `s.trackBattleKillLocked(...)`) to make
each edit unambiguous. Work top-to-bottom so later line numbers stay valid.

- [ ] **Step 2: Verify no production caller of the legacy pair remains**

Run: `rg -n "awardKillXPLocked|payoutDamageDealtXPLocked" server/internal/game --glob '!*_test.go'`
Expected: matches ONLY in `progression.go` — the definitions plus the two
calls inside `awardUnitDeathXPLocked`. Any other production hit is a missed
site; fix it before proceeding.

- [ ] **Step 3: Build and full regression**

Run: `go build ./... && go test ./internal/game/ -count=1`
Expected: build + all tests pass (classic default ⇒ legacy behavior intact,
including trap kills still not granting soldier-tank XP — unchanged).

- [ ] **Step 4: Commit**

```bash
git add server/internal/game/trap.go
git commit -m "refactor(xp): route trap kill sites through awardUnitDeathXPLocked"
```

---

## Task 8: End-to-end split integration test + repo conventions check

**Files:**
- Test: `server/internal/game/experience_split_test.go`

- [ ] **Step 1: Write an end-to-end split test through the real death path**

Append to `server/internal/game/experience_split_test.go`:

```go
// Drives a real melee kill (resolveAttackHitLocked → awardUnitDeathXPLocked)
// in split mode and asserts the dispatcher routes to the split algorithm:
// the killer and a nearby ally each get half the enemy's XPValue, and the
// classic kill bonus (25 × 0.2) does NOT appear.
func TestSplit_EndToEndMeleeKill(t *testing.T) {
	withExperienceTuning(t, splitTuning(500))
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	killer := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 1000, Y: 1000})
	killer.Damage = 9999 // one-shot
	ally := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 1010, Y: 1000})
	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 1005, Y: 1000})
	enemy.HP = 1
	enemy.MaxHP = 1
	enemy.XPValue = 10

	var dead []int
	s.resolveAttackHitLocked(killer, enemy, killer.Damage, &dead)

	// Eligible = {killer, ally} (both in range) → 10/2 = 5 each, no kill bonus.
	if killer.XP != 5 {
		t.Errorf("killer.XP = %d, want 5 (split share, no classic kill bonus)", killer.XP)
	}
	if ally.XP != 5 {
		t.Errorf("ally.XP = %d, want 5 (in-range split share)", ally.XP)
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./internal/game/ -run TestSplit_EndToEndMeleeKill -v`
Expected: PASS. If the killer also shows the classic kill bonus, a kill site
was missed in Task 6 — re-run Task 7 Step 2's `rg` check.

- [ ] **Step 3: Repo-convention check (no pinned balance literals)**

Re-read `experience_split_test.go`. Confirm every expected number is derived
from a test-supplied input (`enemy.XPValue`, recipient count, the
test's own `splitTuning` radius) — NOT from a hardcoded copy of a catalog or
`progression.go` balance constant. The only literals are the test's own
inputs and arithmetic on them. Fix any that pin a production tunable.

- [ ] **Step 4: Final full regression (both modes exercised)**

Run: `go test ./internal/game/ -count=1`
Expected: all pass — existing suite (classic) untouched, new split suite green.

- [ ] **Step 5: Commit**

```bash
git add server/internal/game/experience_split_test.go
git commit -m "test(xp): end-to-end split-mode kill integration test"
```

- [ ] **Step 6: QA handoff**

Dispatch the `qa-engineer` subagent to verify: (a) the `classic` regression
surface — every one of the 19 pair sites faithfully preserved (diff review +
full suite); (b) determinism of `awardSplitDeathXPLocked` (sorted recipient
IDs; no map-order dependence); (c) the trap-with-dead-trapper "XP lost" path;
(d) split/scaled accumulator isolation. Address any findings before declaring
the feature done.

---

## Self-Review

**Spec coverage:**

- §1 config (mode/splitDefaultXP/splitEligibilityRadius, default classic, validation, version 1) → Task 1 ✓
- §2 `UnitDef.Experience *int` (absent vs explicit 0), `Unit.XPValue` seeded both spawn paths → Task 2 ✓
- §3 dispatcher replaces the pair; classic verbatim; split guards on tank + building payouts; per-site table → Tasks 5, 6, 7 ✓
- §4 split algorithm (XPValue≤0 guard, proximity squared-distance, contributor union via `DamageDealtByUnit`, empty→lost, raw float share); determinism via sorted IDs → Task 4 ✓
- §6 trap kills still no soldier-tank XP in classic; trap-dead-trapper → XP lost in split → Task 7 (explicit "do not lift out of guard") ✓
- §7 test seam, classic regression via existing suite, all split cases, no pinned literals, QA sign-off → Tasks 4, 5, 8 ✓

**Placeholder scan:** No TBD/TODO; every code step shows complete code; every command has expected output. ✓

**Type consistency:** `experienceModeClassic/Split` (string consts, progression.go) used identically in tuning_defs.go validation, dispatcher, and both guards. `ExperienceTuning{Mode, SplitDefaultXP, SplitEligibilityRadius}` consistent across struct, JSON, seam, tests. `resolveUnitXPValue(UnitDef) int`, `addUnitXPRawFloatLocked(*Unit, float64)`, `awardSplitDeathXPLocked(*Unit)`, `awardUnitDeathXPLocked(dead, killer *Unit)` — signatures consistent between definition and all call sites/tests. ✓

One spec→plan refinement worth noting: the plan pins down a spec ambiguity at trap sites — the dispatcher call stays *inside* the existing `if <owner> != nil` guard. This both preserves classic verbatim and directly implements the spec's approved "trap kill with dead trapper → XP lost" decision; no spec change needed.
