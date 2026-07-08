# Dodge/Block + Shield Items Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every unit stackable dodge/block chances (base 5% dodge; Vanguard rank-scaled block), wire the dormant evasion seam into melee/projectile/pierce basic attacks with dodge/block popups, and ship six new items (rusty/steel shields, elven cloak, three crafted elemental retaliation shields) using the proc-effects system for on-being-struck procs.

**Architecture:** Dodge/block follow the armor three-part pattern: path-rank assignment on the Unit + `EquipmentBonus` aggregation + read-time sum (`evasionForUnit`). The existing `projectileHitsLocked` seam (rolls `rngCombat`, the isolated stream salted `0x5` for exactly this) is renamed `attackHitsLocked`, gains the 75% cap and block-first attribution, and is called from the three basic-attack landing sites. On-struck procs reuse `ItemOnHitProc` + `executeProcEffectLocked` wholesale — defender becomes the `ProcSource`, attacker the target.

**Tech Stack:** Go 1.22 (server module `webrts/server`, root `server/`), embedded JSON catalogs, Vue 3 + TypeScript SPA (vitest) under `client/src/game-portal/`.

**Spec:** `docs/superpowers/specs/2026-07-07-dodge-block-shields-design.md`

## Global Constraints

- Branch: all work on `crafted-items` (already created; verify, never switch).
- AI_RULES: IDs not pointers across ticks; `*Unit` params fine within a tick; `Locked` = caller holds s.mu; no new RNG sources.
- Evasion rolls on `s.rngCombat` ONLY (seeded `seed ^ 0x5`); struck-proc rolls on `s.rngPerks` ONLY, appended AFTER existing on-hit rolls at each site.
- Evasion scope: melee committed swings, basic-attack projectile landings, pierce victims. NEVER: proc bolts/beams (`SkipOnHitEffects`), splash, DoTs, traps, abilities, perk secondary hits (cleave/whirlwind), building attacks.
- Evaded hit = full whiff: no damage, no on-hit procs/elemental, no crit visual, no struck-proc retaliation. Attacker cooldown already spent.
- Constants: `baseUnitDodgeChance = 0.05`, `evasionCapTotal = 0.75`. Vanguard block per rank: bronze 0.10, silver 0.125, gold 0.15.
- Block attributed before dodge on an avoided roll.
- Item stat validation: dodge/block modifiers must satisfy `0 <= v < 1`.
- Behavior of all EXISTING content unchanged except: every unit now dodges 5% of basic attacks (this is the feature, not a regression).
- All Go test commands from `server/`; client tests: `cd client/src/game-portal && npm test`.
- Known pre-existing test failures (fail identically on main; NEVER fix, only confirm no NEW failures): internal/game `TestTwinBronze_CatalogNode_LoadedCorrectly`, cmd/api `TestServerReadyLineAndStdinShutdown`, internal/ws `TestSPBaseline_StructuralShape` (sometimes).
- `gofmt -l` flags every file on this Windows checkout (CRLF artifact) — use `go vet` + `go build` as the format/sanity gates; do not rewrite line endings.
- Commit messages: short imperative, matching repo style.

---

### Task 1: Dodge/block stat plumbing (items → equipment bonus → path ranks → `evasionForUnit`)

**Files:**
- Modify: `server/internal/game/items.go:48-56` (ItemModifiers), `:265-289` (validateItemDef)
- Modify: `server/internal/game/state_items.go:36-49` (UnitEquipmentBonus), `:230-240` (modifier aggregation)
- Modify: `server/internal/game/path_defs.go:94-102` (pathRankStatsJSON)
- Modify: `server/internal/game/progression.go:92+` (pathModifierDef), `:408-418` (applyRankModifiersLocked)
- Modify: `server/internal/game/state.go` Unit struct (~line 137, after `Armor int`)
- Modify: `server/internal/game/projectile_defs.go:204-214` (evasionForUnit real impl + `baseUnitDodgeChance`)
- Modify: `server/internal/game/catalog/units/human/soldier/paths/vanguard/vanguard.json` (rank blocks)
- Modify: `server/internal/game/projectile_defs_test.go:112-128` (the evasionForUnit-returns-zero assertion inverts)
- Test: `server/internal/game/evasion_stats_test.go` (new)

**Interfaces:**
- Consumes: existing `TargetEvasion` struct, `recomputeUnitEquipmentBonusLocked`, `applyRankModifiersLocked`.
- Produces: `baseUnitDodgeChance = 0.05` const; `Unit.PathDodgeChance`, `Unit.PathBlockChance float64`; `UnitEquipmentBonus.DodgeChance/BlockChance float64`; `ItemModifiers.DodgeChance/BlockChance float64` (json `dodgeChance`/`blockChance`); `evasionForUnit(u *Unit) TargetEvasion` returning real totals. Tasks 2-4 rely on `evasionForUnit`.

- [ ] **Step 1: Write the failing tests**

`server/internal/game/evasion_stats_test.go`:
```go
package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestEvasion_BaseDodgeOnFreshUnit: every unit dodges at the game-wide base
// with zero block, before any path or equipment contribution.
func TestEvasion_BaseDodgeOnFreshUnit(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xD0D6E)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	ev := evasionForUnit(u)
	if ev.DodgeChance != baseUnitDodgeChance {
		t.Errorf("fresh unit dodge = %v, want base %v", ev.DodgeChance, baseUnitDodgeChance)
	}
	if ev.BlockChance != 0 {
		t.Errorf("fresh unit block = %v, want 0", ev.BlockChance)
	}
}

// TestEvasion_VanguardBlockScalesWithRank guards the shipped catalog: the
// Vanguard path authors a per-rank blockChance that climbs bronze→gold.
// Asserted as invariants (positive, strictly increasing), not pinned numbers.
func TestEvasion_VanguardBlockScalesWithRank(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xD0D6F)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	var prev float64
	for _, rank := range []string{"bronze", "silver", "gold"} {
		u.ProgressionPath = "vanguard"
		u.Rank = rank
		s.applyRankModifiersLocked(u, false)
		ev := evasionForUnit(u)
		if ev.BlockChance <= prev {
			t.Errorf("vanguard %s block = %v, want > previous rank's %v", rank, ev.BlockChance, prev)
		}
		// Dodge keeps the game-wide base — the path doesn't author dodge.
		if ev.DodgeChance != baseUnitDodgeChance {
			t.Errorf("vanguard %s dodge = %v, want base %v", rank, ev.DodgeChance, baseUnitDodgeChance)
		}
		prev = ev.BlockChance
	}
}

// TestEvasion_EquipmentAddsAdditively: item dodge/block modifiers stack onto
// base + path additively through the equipment bonus.
func TestEvasion_EquipmentAddsAdditively(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xD0D70)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	u.EquipmentBonus.DodgeChance = 0.15
	u.EquipmentBonus.BlockChance = 0.05
	ev := evasionForUnit(u)
	if want := baseUnitDodgeChance + 0.15; ev.DodgeChance != want {
		t.Errorf("dodge = %v, want %v (base + equipment)", ev.DodgeChance, want)
	}
	if ev.BlockChance != 0.05 {
		t.Errorf("block = %v, want 0.05 (equipment only)", ev.BlockChance)
	}
}

// TestValidateItemDef_DodgeBlockRange: item dodge/block modifiers outside
// [0,1) are content authoring errors.
func TestValidateItemDef_DodgeBlockRange(t *testing.T) {
	good := &ItemDef{ID: "ok", Kind: ItemKindEquipment, Modifiers: &ItemModifiers{DodgeChance: 0.15, BlockChance: 0.1}}
	if err := validateItemDef(good); err != nil {
		t.Fatalf("valid dodge/block modifiers rejected: %v", err)
	}
	negDodge := &ItemDef{ID: "bad1", Modifiers: &ItemModifiers{DodgeChance: -0.1}}
	if err := validateItemDef(negDodge); err == nil {
		t.Error("expected error for negative dodgeChance, got nil")
	}
	fullBlock := &ItemDef{ID: "bad2", Modifiers: &ItemModifiers{BlockChance: 1.0}}
	if err := validateItemDef(fullBlock); err == nil {
		t.Error("expected error for blockChance >= 1, got nil")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd server && go test ./internal/game/ -run "TestEvasion_|TestValidateItemDef_DodgeBlockRange" -v`
Expected: FAIL to compile — `undefined: baseUnitDodgeChance`, unknown fields `DodgeChance`/`BlockChance`.

- [ ] **Step 3: Implement the plumbing**

3a. `items.go` — extend `ItemModifiers` (after `MaxShield`):
```go
	MaxShield   int     `json:"maxShield,omitempty"`
	// DodgeChance / BlockChance are additive probability contributions to the
	// wearer's evasion stats (0.15 = +15%). Validated to [0,1) at load; the
	// combined dodge+block total is capped at evasionCapTotal at roll time,
	// not here, so stacked items display honestly.
	DodgeChance float64 `json:"dodgeChance,omitempty"`
	BlockChance float64 `json:"blockChance,omitempty"`
}
```

3b. `items.go` — extend `validateItemDef` (add before the `OnHitElemental` loop):
```go
	if m := def.Modifiers; m != nil {
		if m.DodgeChance < 0 || m.DodgeChance >= 1 {
			return fmt.Errorf("item %q modifiers.dodgeChance %v out of range [0,1)", def.ID, m.DodgeChance)
		}
		if m.BlockChance < 0 || m.BlockChance >= 1 {
			return fmt.Errorf("item %q modifiers.blockChance %v out of range [0,1)", def.ID, m.BlockChance)
		}
	}
```

3c. `state_items.go` — extend `UnitEquipmentBonus` (after `MaxShield int`):
```go
	MaxShield   int
	// DodgeChance / BlockChance sum the equipped items' additive evasion
	// contributions (see ItemModifiers). Read by evasionForUnit.
	DodgeChance float64
	BlockChance float64
```
and the aggregation loop in `recomputeUnitEquipmentBonusLocked` (inside `if def.Modifiers != nil`, after the MaxShield line):
```go
			unit.EquipmentBonus.MaxShield += def.Modifiers.MaxShield
			unit.EquipmentBonus.DodgeChance += def.Modifiers.DodgeChance
			unit.EquipmentBonus.BlockChance += def.Modifiers.BlockChance
```

3d. `path_defs.go` — extend `pathRankStatsJSON`:
```go
	Armor                 int     `json:"armor"`
	// DodgeChance / BlockChance are per-rank ADDITIVE evasion contributions
	// (0.10 = +10%). Absent (0) means the path contributes nothing; the
	// game-wide base (baseUnitDodgeChance) always applies on top.
	DodgeChance float64 `json:"dodgeChance"`
	BlockChance float64 `json:"blockChance"`
```
and thread the two fields through wherever `pathRankStatsJSON` is copied into `pathModifierDef` (`progression.go:92+` — add matching `DodgeChance`, `BlockChance float64` fields to `pathModifierDef` and copy them at the same place `Armor` is copied; find it with `grep -n "Armor" server/internal/game/path_defs.go server/internal/game/progression.go`).

3e. `state.go` — Unit struct, directly after `Armor int` (~line 137):
```go
	Armor                int
	// PathDodgeChance / PathBlockChance are the progression path's per-rank
	// additive evasion contributions, assigned by applyRankModifiersLocked
	// (zero for pathless units). Combined with the game-wide base and the
	// equipment bonus at read time by evasionForUnit — mirroring how Armor
	// is path-assigned here and equipment-extended in effectiveArmorLocked.
	PathDodgeChance float64
	PathBlockChance float64
```

3f. `progression.go` — `applyRankModifiersLocked`, next to `unit.Armor = pathDef.Armor`:
```go
	unit.Armor = pathDef.Armor
	unit.PathDodgeChance = pathDef.DodgeChance
	unit.PathBlockChance = pathDef.BlockChance
```

3g. `projectile_defs.go` — replace the `evasionForUnit` stub (and its TODO comment) with:
```go
// baseUnitDodgeChance is the game-wide innate dodge probability every unit
// has before path and equipment contributions. Base block is 0 — block comes
// only from paths (Vanguard) and items (shields).
const baseUnitDodgeChance = 0.05

// evasionForUnit returns a unit's live evasion totals: game-wide base +
// progression-path rank contribution + equipped-item bonus, each additive.
// The combined cap is NOT applied here (attackHitsLocked clamps at roll
// time) so displayed stats stay honest.
func evasionForUnit(u *Unit) TargetEvasion {
	return TargetEvasion{
		DodgeChance: baseUnitDodgeChance + u.PathDodgeChance + u.EquipmentBonus.DodgeChance,
		BlockChance: u.PathBlockChance + u.EquipmentBonus.BlockChance,
	}
}
```

3h. `vanguard.json` — add to each rank block:
```json
    "bronze":  { ..., "armor": 100, "blockChance": 0.10 },
    "silver":  { ..., "armor": 150, "blockChance": 0.125 },
    "gold":    { ..., "armor": 200, "blockChance": 0.15 }
```
(keep every existing field; only add `blockChance`).

3i. `projectile_defs_test.go:124-128` — the old zero-evasion assertion inverts; replace those lines with:
```go
	// Every unit now carries the game-wide base dodge, so a real spawned
	// unit's profile is non-zero by design.
	u := spawnProjTestUnit(t, s, "p1", 400, 400)
	if ev := evasionForUnit(u); ev.DodgeChance != baseUnitDodgeChance || ev.BlockChance != 0 {
		t.Errorf("evasionForUnit(spawned soldier) = %+v; want base dodge %v / block 0", ev, baseUnitDodgeChance)
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd server && go test ./internal/game/ -run "TestEvasion_|TestValidateItemDef_DodgeBlockRange|TestProjectileHit" -v`
Expected: PASS (all new tests + the updated projectile_defs tests).

- [ ] **Step 5: Whole package + commit**

Run: `cd server && go build ./... && go test ./internal/game/`
Expected: PASS (only the known pre-existing failure).
```bash
git add server/internal/game/items.go server/internal/game/state_items.go server/internal/game/path_defs.go server/internal/game/progression.go server/internal/game/state.go server/internal/game/projectile_defs.go server/internal/game/projectile_defs_test.go server/internal/game/evasion_stats_test.go server/internal/game/catalog/units/human/soldier/paths/vanguard/vanguard.json
git commit -m "Add dodge/block stat plumbing: base + path rank + equipment, Vanguard block"
```

---

### Task 2: `attackHitsLocked` — rename, 75% cap, block-first attribution

**Files:**
- Modify: `server/internal/game/projectile_defs.go:216-234` (rename + rework `projectileHitsLocked`)
- Modify: `server/internal/game/projectile_defs_test.go` (rename call sites; tests at lines 80, 112, 134)
- Test: append to `server/internal/game/evasion_stats_test.go`

**Interfaces:**
- Consumes: `TargetEvasion`, `s.rngCombat`.
- Produces: `evasionCapTotal = 0.75` const; `(s *GameState) attackHitsLocked(ev TargetEvasion) (hit bool, avoidedBy string)` — `avoidedBy` is `""` on hit, `"block"` or `"dodge"` on avoid. Tasks 3-4 call it. `projectileHitsLocked` ceases to exist.

- [ ] **Step 1: Write the failing tests** (append to `evasion_stats_test.go`)

```go
// TestAttackHits_BlockAttributedFirst: on an avoided roll, block claims the
// low end of the roll space before dodge — a single rngCombat draw decides
// both outcome and attribution, deterministically.
func TestAttackHits_BlockAttributedFirst(t *testing.T) {
	// With block 0.5 + dodge 0.5 (capped to 0.75 total) the hit can never
	// land; rolls < 0.5 are blocks, [0.5, 0.75) are dodges — sample many
	// rolls and require BOTH attributions to appear and NO hits.
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xB10C)
	s.mu.Lock()
	defer s.mu.Unlock()
	ev := TargetEvasion{DodgeChance: 0.5, BlockChance: 0.5}
	sawBlock, sawDodge := false, false
	for i := 0; i < 200; i++ {
		hit, by := s.attackHitsLocked(ev)
		switch {
		case hit:
			// cap is 0.75, so 25% of rolls DO hit — that's the cap working.
		case by == "block":
			sawBlock = true
		case by == "dodge":
			sawDodge = true
		default:
			t.Fatalf("avoided with empty attribution")
		}
	}
	if !sawBlock || !sawDodge {
		t.Errorf("expected both attributions over 200 rolls, got block=%v dodge=%v", sawBlock, sawDodge)
	}
}

// TestAttackHits_CapGuaranteesHits: stacked evasion beyond the cap still
// lands hits — assert at least one hit over many rolls at 0.5+0.5 (would be
// avoid=1.0 uncapped, i.e. zero hits).
func TestAttackHits_CapGuaranteesHits(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xB10D)
	s.mu.Lock()
	defer s.mu.Unlock()
	ev := TargetEvasion{DodgeChance: 0.5, BlockChance: 0.5}
	hits := 0
	for i := 0; i < 200; i++ {
		if hit, _ := s.attackHitsLocked(ev); hit {
			hits++
		}
	}
	if hits == 0 {
		t.Error("cap at 0.75 must let some hits through; got 0 hits in 200 rolls")
	}
}

// TestAttackHits_Deterministic: two states with the same seed produce the
// identical hit/attribution sequence.
func TestAttackHits_Deterministic(t *testing.T) {
	run := func() []string {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xB10E)
		s.mu.Lock()
		defer s.mu.Unlock()
		ev := TargetEvasion{DodgeChance: 0.2, BlockChance: 0.2}
		out := make([]string, 0, 50)
		for i := 0; i < 50; i++ {
			hit, by := s.attackHitsLocked(ev)
			if hit {
				out = append(out, "hit")
			} else {
				out = append(out, by)
			}
		}
		return out
	}
	a, b := run(), run()
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("sequence diverged at %d: %q vs %q", i, a[i], b[i])
		}
	}
}

// TestAttackHits_ZeroEvasionNoRNG: the zero-evasion fast path neither rolls
// nor consumes RNG (guards proc/effect paths that construct zero profiles).
func TestAttackHits_ZeroEvasionNoRNG(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xB10F)
	s.mu.Lock()
	defer s.mu.Unlock()
	before := s.rngCombat.Float64() // advance once, remember stream position implicitly
	_ = before
	s2 := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xB10F)
	s2.mu.Lock()
	defer s2.mu.Unlock()
	_ = s2.rngCombat.Float64()
	if hit, by := s2.attackHitsLocked(TargetEvasion{}); !hit || by != "" {
		t.Fatalf("zero evasion must always hit with no attribution")
	}
	// Streams must still be aligned: next draw identical on both states.
	if a, b := s.rngCombat.Float64(), s2.rngCombat.Float64(); a != b {
		t.Errorf("zero-evasion call consumed RNG: streams diverged (%v vs %v)", a, b)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd server && go test ./internal/game/ -run "TestAttackHits_" -v`
Expected: FAIL to compile — `undefined: attackHitsLocked` (method), `undefined: evasionCapTotal`.

- [ ] **Step 3: Implement**

Replace `projectileHitsLocked` (`projectile_defs.go:216-234`) entirely with:
```go
// evasionCapTotal is the ceiling on a unit's combined dodge+block avoidance:
// however stacked its gear, every unit is hittable at least 1-in-4. Applied
// at roll time only — stored/displayed stats stay uncapped and honest.
const evasionCapTotal = 0.75

// attackHitsLocked decides whether a basic attack connecting with a target
// lands. Hit/miss is driven entirely by the *target's* evasion: a single
// deterministic roll is taken against s.rngCombat (seeded per-match,
// isolated from every other RNG stream so this never perturbs
// perk/loot/cosmetic determinism). The roll space is partitioned block-first
// — an avoided roll below the block portion reports "block", the remainder
// "dodge" — so shields read as active in the popup. RNG is only consumed
// when the target actually has evasion; every real unit does (base dodge),
// but zero-evasion profiles (tests, effects) skip the draw.
//
// Returns (true, "") on a landed hit, (false, "block"|"dodge") on an avoid.
// Caller holds s.mu.
func (s *GameState) attackHitsLocked(ev TargetEvasion) (bool, string) {
	if !ev.HasEvasion() {
		return true, ""
	}
	avoid := ev.DodgeChance + ev.BlockChance
	if avoid > evasionCapTotal {
		avoid = evasionCapTotal
	}
	roll := s.rngCombat.Float64()
	if roll >= avoid {
		return true, ""
	}
	blockPortion := ev.BlockChance
	if blockPortion > avoid {
		blockPortion = avoid
	}
	if roll < blockPortion {
		return false, "block"
	}
	return false, "dodge"
}
```

Update `projectile_defs_test.go`: every `s.projectileHitsLocked(ev)` call becomes `hit, _ := s.attackHitsLocked(ev)` (adjust the three tests at lines 80/112/134 accordingly — assertions keep their meaning: guaranteed-avoid cases at line 86-93 use totals ≤ 0.75 or assert `!hit`; note the `avoid >= 1.0` guaranteed-miss case is GONE because the cap makes total avoidance impossible — update `TestProjectileHit_WithEvasionStats` to assert capped behavior instead: totals ≥ 0.75 avoid at most 75% (cannot be asserted as "always false" anymore; convert that sub-case to assert that SOME rolls hit, mirroring TestAttackHits_CapGuaranteesHits, or delete the sub-case since the new test covers it — prefer delete + comment).

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd server && go test ./internal/game/ -run "TestAttackHits_|TestProjectileHit" -v`
Expected: PASS. Also `grep -rn "projectileHitsLocked" server/` returns nothing.

- [ ] **Step 5: Commit**

```bash
git add server/internal/game/projectile_defs.go server/internal/game/projectile_defs_test.go server/internal/game/evasion_stats_test.go
git commit -m "Rename projectileHitsLocked to attackHitsLocked: 75% cap + block-first attribution"
```

---

### Task 3: Wire evasion into combat + dodge/block popups (server + protocol)

**Files:**
- Create: `server/internal/game/evade_events.go`
- Modify: `server/internal/game/state_combat.go:514-584` (melee branch of `applyDelayedAttackLocked`)
- Modify: `server/internal/game/projectile.go:654-708` (`landProjectileLocked`), `:531-584` (pierce `hitOne`)
- Modify: `server/pkg/protocol/messages.go` (~1615 area: event struct; ~1818 area: snapshot field)
- Modify: `server/internal/game/state.go` (snapshot assembly + reset — mirror `MinorDamageEvents` at every site)
- Test: `server/internal/game/evasion_combat_test.go` (new)

**Interfaces:**
- Consumes: `attackHitsLocked`, `evasionForUnit` (Tasks 1-2), `resolveAttackHitLocked`, `combatTargetIsValidLocked`.
- Produces: `(s *GameState) recordEvadeEventLocked(target *Unit, kind string)`; `protocol.EvadeEventSnapshot{UnitID int; Kind string}` on the snapshot as `evadeEvents`; evasion rolls live at the three basic-attack sites. Task 4 inserts its struck-proc hook AFTER these rolls (landed hits only).

- [ ] **Step 1: Write the failing tests**

`server/internal/game/evasion_combat_test.go`:
```go
package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// evasionTestPair spawns a MELEE attacker ("p1" soldier — the melee branch of
// applyDelayedAttackLocked must run) and a hostile target with deep HP at
// melee range. Target evasion is then forced via EquipmentBonus.
func evasionTestPair(t *testing.T, s *GameState) (attacker, target *Unit) {
	t.Helper()
	attacker = s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	target = &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 1_000_000, MaxHP: 1_000_000, X: 10, Y: 0}
	s.nextUnitID++
	s.addUnitLocked(target)
	return attacker, target
}

// forceAvoid pins the target's evasion at the cap so every basic attack on it
// is avoided at most 75% — for whiff-semantics tests we instead use a helper
// that retries until a whiff occurs, asserting semantics of THAT whiff.
// Simpler and deterministic: set block=0.75 dodge=0 → every avoided roll is a
// block; then scan a bounded number of swings for at least one whiff.
func forceMaxBlock(u *Unit) {
	u.EquipmentBonus.DodgeChance = 0
	u.EquipmentBonus.BlockChance = 0.75
	u.PathDodgeChance = -baseUnitDodgeChance // cancel base dodge: all avoids attribute to block
}

// TestEvasion_MeleeWhiffIsFullWhiff: an avoided melee swing deals no damage,
// fires no on-hit procs, and records an evade event of the attributed kind.
func TestEvasion_MeleeWhiffIsFullWhiff(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xE7A1)
	s.mu.Lock()
	defer s.mu.Unlock()
	attacker, target := evasionTestPair(t, s)
	forceMaxBlock(target)
	attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{{Chance: 1.0, Params: ProcEffectParams{Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"}}}

	sawWhiff := false
	for i := 0; i < 100 && !sawWhiff; i++ {
		hpBefore := target.HP
		projsBefore := len(s.Projectiles)
		evadesBefore := len(s.evadeEventsThisTick)
		attacker.AttackWindupTargetID = target.ID
		deadUnitIDs := []int{}
		s.applyDelayedAttackLocked(attacker, &deadUnitIDs, &[]string{})
		if target.HP == hpBefore {
			sawWhiff = true
			if len(s.Projectiles) != projsBefore {
				t.Error("whiffed swing must not fire on-hit proc bolts")
			}
			if len(s.evadeEventsThisTick) != evadesBefore+1 {
				t.Fatalf("whiff must record exactly one evade event, got %d new", len(s.evadeEventsThisTick)-evadesBefore)
			}
			ev := s.evadeEventsThisTick[len(s.evadeEventsThisTick)-1]
			if ev.UnitID != target.ID || ev.Kind != "block" {
				t.Errorf("evade event = %+v, want {UnitID:%d Kind:block}", ev, target.ID)
			}
		}
	}
	if !sawWhiff {
		t.Fatal("75% block never produced a whiff in 100 swings — wiring missing?")
	}
}

// TestEvasion_ProjectileLandingRollsEvasion: a basic-attack arrow can be
// avoided at landing; an avoided landing deals no damage and records the
// evade event. Proc bolts (SkipOnHitEffects) are immune — separate test.
func TestEvasion_ProjectileLandingRollsEvasion(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xE7A2)
	s.mu.Lock()
	defer s.mu.Unlock()
	attacker, target := evasionTestPair(t, s)
	forceMaxBlock(target)

	sawWhiff := false
	for i := 0; i < 100 && !sawWhiff; i++ {
		hpBefore := target.HP
		dead := []int{}
		s.landProjectileLocked(&Projectile{ID: "arrow", OwnerUnitID: attacker.ID, OwnerPlayerID: attacker.OwnerID, TargetUnitID: target.ID, Damage: 10}, target, &dead)
		if target.HP == hpBefore {
			sawWhiff = true
		}
	}
	if !sawWhiff {
		t.Fatal("75% block never produced an arrow whiff in 100 landings — wiring missing?")
	}
}

// TestEvasion_ProcBoltIgnoresEvasion: a SkipOnHitEffects bolt always lands —
// no roll, no rngCombat consumption, damage always applies.
func TestEvasion_ProcBoltIgnoresEvasion(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xE7A3)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, target := evasionTestPair(t, s)
	forceMaxBlock(target)

	for i := 0; i < 20; i++ {
		hpBefore := target.HP
		dead := []int{}
		s.landProjectileLocked(&Projectile{ID: "bolt", TargetUnitID: target.ID, Damage: 10, DamageType: DamageFire, SkipOnHitEffects: true}, target, &dead)
		if target.HP != hpBefore-10 {
			t.Fatalf("proc bolt %d: damage %d→%d, proc bolts must never be evaded", i, hpBefore, target.HP)
		}
	}
}

// TestEvasion_SnapshotCarriesEvadeEvents: recorded evade events appear on the
// wire snapshot and clear next tick (mirrors minorDamageEvents lifecycle).
func TestEvasion_SnapshotCarriesEvadeEvents(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xE7A4)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, target := evasionTestPair(t, s)
	s.recordEvadeEventLocked(target, "dodge")
	events := s.snapshotEvadeEventsLocked()
	if len(events) != 1 || events[0].UnitID != target.ID || events[0].Kind != "dodge" {
		t.Fatalf("snapshot events = %+v, want one {UnitID:%d Kind:dodge}", events, target.ID)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd server && go test ./internal/game/ -run "TestEvasion_Melee|TestEvasion_Projectile|TestEvasion_Proc|TestEvasion_Snapshot" -v`
Expected: FAIL to compile — `undefined: s.evadeEventsThisTick`, `recordEvadeEventLocked`, `snapshotEvadeEventsLocked`.

- [ ] **Step 3: Implement the evade-event channel**

3a. `server/pkg/protocol/messages.go` — next to `MinorDamageEventSnapshot` (~line 1619):
```go
// EvadeEventSnapshot is a one-tick "the defender avoided a basic attack"
// event: the client floats "Dodged!" / "Blocked!" over the unit. Kind is
// "dodge" or "block".
type EvadeEventSnapshot struct {
	UnitID int    `json:"unitId"`
	Kind   string `json:"kind"`
}
```
and on the snapshot message struct (next to `MinorDamageEvents`, ~line 1818):
```go
	EvadeEvents []EvadeEventSnapshot `json:"evadeEvents,omitempty"`
```

3b. `server/internal/game/evade_events.go` (new file — mirror `minor_damage_events.go`'s shape exactly; read that file first and copy its structure):
```go
package game

import "webrts/server/pkg/protocol"

// evadeEvent is one avoided basic attack this tick — the defender's ID and
// which stat avoided it ("dodge" | "block"). Drained into the snapshot each
// tick like minorDamageEvent.
type evadeEvent struct {
	UnitID int
	Kind   string
}

// recordEvadeEventLocked queues a dodge/block popup over the defender.
// Caller holds s.mu.
func (s *GameState) recordEvadeEventLocked(target *Unit, kind string) {
	if target == nil || kind == "" {
		return
	}
	s.evadeEventsThisTick = append(s.evadeEventsThisTick, evadeEvent{UnitID: target.ID, Kind: kind})
}

// snapshotEvadeEventsLocked converts this tick's evade events to their wire
// form. Caller holds s.mu.
func (s *GameState) snapshotEvadeEventsLocked() []protocol.EvadeEventSnapshot {
	if len(s.evadeEventsThisTick) == 0 {
		return nil
	}
	out := make([]protocol.EvadeEventSnapshot, len(s.evadeEventsThisTick))
	for i, e := range s.evadeEventsThisTick {
		out[i] = protocol.EvadeEventSnapshot{UnitID: e.UnitID, Kind: e.Kind}
	}
	return out
}
```

3c. `state.go` — add the field `evadeEventsThisTick []evadeEvent` next to `minorDamageEventsThisTick` on GameState; add `EvadeEvents: s.snapshotEvadeEventsLocked(),` at EVERY snapshot assembly site that sets `MinorDamageEvents` (find them: `grep -n "snapshotMinorDamageEventsLocked()" server/internal/game/state.go` — three sites), and clear it wherever minor damage events are reset (`grep -n "resetMinorDamageEventsThisTickLocked\|minorDamageEventsThisTick = " server/internal/game/`): add `s.evadeEventsThisTick = s.evadeEventsThisTick[:0]` at the same reset point.

3d. Melee hook — `state_combat.go`, in `applyDelayedAttackLocked`'s unit branch. The roll goes ONLY in the melee tail (ranged attacks roll at landing instead), i.e. immediately AFTER the `if !profile.Melee { ... return }` block and BEFORE `s.logAttackTiming("melee-land", ...)`:
```go
		// Evasion: the committed swing connects visually, but the defender may
		// dodge/block it — a full whiff (no damage, no on-hit effects). Rolled
		// here for melee only; ranged attacks roll when the projectile LANDS
		// (landProjectileLocked), so a dodge happens when the arrow arrives.
		if hit, avoidedBy := s.attackHitsLocked(evasionForUnit(target)); !hit {
			s.recordEvadeEventLocked(target, avoidedBy)
			return
		}
```

3e. Projectile hook — `projectile.go`, in `landProjectileLocked`, immediately AFTER the `if proj.SkipOnHitEffects { ... return }` block and BEFORE `attacker := s.getUnitByIDLocked(proj.OwnerUnitID)`:
```go
	// Evasion: a basic-attack projectile can be dodged/blocked at LANDING —
	// full whiff. Proc bolts took the SkipOnHitEffects return above and are
	// never evaded (effects always land).
	if hit, avoidedBy := s.attackHitsLocked(evasionForUnit(target)); !hit {
		s.recordEvadeEventLocked(target, avoidedBy)
		return
	}
```

3f. Pierce hook — `projectile.go`, in the `hitOne` closure, immediately AFTER `proj.PierceHits = append(proj.PierceHits, target.ID)` (append first so an evaded victim isn't re-rolled on later ticks) and BEFORE the damage computation:
```go
		// Evasion: each pierce victim rolls independently. Appended to
		// PierceHits above regardless, so an evaded victim is spent — the
		// arrow doesn't retry them next tick.
		if hitRoll, avoidedBy := s.attackHitsLocked(evasionForUnit(target)); !hitRoll {
			s.recordEvadeEventLocked(target, avoidedBy)
			return
		}
```

- [ ] **Step 4: Run the new tests, then the whole package**

Run: `cd server && go test ./internal/game/ -run "TestEvasion_" -v`
Expected: PASS.
Run: `cd server && go test ./internal/game/ 2>&1 | grep -E "^(--- FAIL|FAIL|ok)"`
Expected: only the known pre-existing failure. NOTE: some existing combat tests may now see 5%-dodge whiffs under their fixed seeds and fail. For each: the fix is to pin the TARGET's evasion to zero in the test setup (`target.PathDodgeChance = -baseUnitDodgeChance`) — NEVER weaken the assertion, NEVER change the seed hunting for a pass. Add a shared helper to `evasion_combat_test.go`:
```go
// disableEvasion pins a unit's evasion to zero so legacy always-hit tests
// keep their contract under the new base dodge.
func disableEvasion(u *Unit) { u.PathDodgeChance = -baseUnitDodgeChance }
```
and use it in any failing legacy test's setup. List every test you touch in the commit message body.

- [ ] **Step 5: Commit**

```bash
git add server/internal/game/ server/pkg/protocol/messages.go
git commit -m "Wire evasion into melee/projectile/pierce hits; add dodge/block wire events"
```

---

### Task 4: On-being-struck procs (`onStruckProc` — elemental shield retaliation)

**Files:**
- Modify: `server/internal/game/items.go` (ItemDef field, validation refactor)
- Modify: `server/internal/game/state_items.go` (UnitEquipmentBonus.OnStruckProcs + recompute)
- Modify: `server/internal/game/state_combat.go` (`resolveAttackHitLocked` — the struck hook; `rollEquipmentStruckProcsLocked`)
- Test: `server/internal/game/struck_proc_test.go` (new)

**Interfaces:**
- Consumes: `ItemOnHitProc` (+ `ResolveParams`, `MarshalJSON` — inherited untouched), `EquipmentProc`, `executeProcEffectLocked`, `procSourceFromUnit`, evasion hooks from Task 3 (struck procs fire only on LANDED hits — the whiff `return`s in Task 3 guarantee this for free).
- Produces: `ItemDef.OnStruckProc *ItemOnHitProc` (json `onStruckProc`); `UnitEquipmentBonus.OnStruckProcs []EquipmentProc`; `(s *GameState) rollEquipmentStruckProcsLocked(defender, attacker *Unit)` called from `resolveAttackHitLocked`. Task 5's shield JSONs author `onStruckProc`.

- [ ] **Step 1: Write the failing tests**

`server/internal/game/struck_proc_test.go`:
```go
package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// struckProcPair: defender ("p1") wearing a chance-1.0 struck proc, hostile
// attacker with deep HP. Defender evasion disabled so every hit lands.
func struckProcPair(t *testing.T, s *GameState) (defender, attacker *Unit) {
	t.Helper()
	defender = s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	defender.HP, defender.MaxHP = 1_000_000, 1_000_000
	disableEvasion(defender)
	defender.EquipmentBonus.OnStruckProcs = []EquipmentProc{{Chance: 1.0, Params: ProcEffectParams{Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"}}}
	attacker = &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 1_000_000, MaxHP: 1_000_000, X: 10, Y: 0}
	s.nextUnitID++
	s.addUnitLocked(attacker)
	return defender, attacker
}

// TestStruckProc_FiresAtAttackerOnLandedHit: a landed basic attack on the
// wearer launches the retaliation bolt AT THE ATTACKER, owned by the wearer.
func TestStruckProc_FiresAtAttackerOnLandedHit(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x57C1)
	s.mu.Lock()
	defer s.mu.Unlock()
	defender, attacker := struckProcPair(t, s)

	dead := []int{}
	s.resolveAttackHitLocked(attacker, defender, 10, &dead)

	if len(s.Projectiles) != 1 {
		t.Fatalf("expected 1 retaliation bolt, got %d projectiles", len(s.Projectiles))
	}
	bolt := s.Projectiles[0]
	if bolt.TargetUnitID != attacker.ID || bolt.OwnerUnitID != defender.ID {
		t.Errorf("bolt target=%d owner=%d, want target=%d owner=%d (defender retaliates at attacker)", bolt.TargetUnitID, bolt.OwnerUnitID, attacker.ID, defender.ID)
	}
	if !bolt.SkipOnHitEffects {
		t.Error("retaliation bolt must skip the on-hit hub (no proc loops)")
	}
}

// TestStruckProc_NotTriggeredByProcDamage: proc-bolt damage landing on the
// wearer must NOT retaliate (SkipOnHitEffects bypasses resolveAttackHitLocked).
func TestStruckProc_NotTriggeredByProcDamage(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x57C2)
	s.mu.Lock()
	defer s.mu.Unlock()
	defender, attacker := struckProcPair(t, s)

	dead := []int{}
	s.landProjectileLocked(&Projectile{ID: "bolt", OwnerUnitID: attacker.ID, TargetUnitID: defender.ID, Damage: 10, DamageType: DamageFire, SkipOnHitEffects: true}, defender, &dead)
	if len(s.Projectiles) != 0 {
		t.Fatalf("proc damage must not trigger retaliation, got %d projectiles", len(s.Projectiles))
	}
}

// TestStruckProc_DeadDefenderDoesNotRetaliate: a hit that kills the wearer
// fires no retaliation.
func TestStruckProc_DeadDefenderDoesNotRetaliate(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x57C3)
	s.mu.Lock()
	defer s.mu.Unlock()
	defender, attacker := struckProcPair(t, s)
	defender.HP, defender.MaxHP = 1, 1

	dead := []int{}
	s.resolveAttackHitLocked(attacker, defender, 1_000_000, &dead)
	if len(s.Projectiles) != 0 {
		t.Fatalf("dead defender must not retaliate, got %d projectiles", len(s.Projectiles))
	}
}

// TestStruckProc_RangedAttackerGetsBoltBack: an arrow landing (full ranged
// path through landProjectileLocked → resolveAttackHitLocked) triggers
// retaliation homing back at the shooter.
func TestStruckProc_RangedAttackerGetsBoltBack(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x57C4)
	s.mu.Lock()
	defer s.mu.Unlock()
	defender, attacker := struckProcPair(t, s)

	dead := []int{}
	s.landProjectileLocked(&Projectile{ID: "arrow", OwnerUnitID: attacker.ID, OwnerPlayerID: attacker.OwnerID, TargetUnitID: defender.ID, Damage: 10}, defender, &dead)
	found := false
	for _, p := range s.Projectiles {
		if p.SkipOnHitEffects && p.TargetUnitID == attacker.ID && p.OwnerUnitID == defender.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("ranged attacker must eat a retaliation bolt after their arrow lands")
	}
}

// TestValidateItemDef_OnStruckProc: onStruckProc obeys the same rules as
// onHitProc (chance range, effect required + registered).
func TestValidateItemDef_OnStruckProc(t *testing.T) {
	good := &ItemDef{ID: "ok", Kind: ItemKindEquipment, OnStruckProc: &ItemOnHitProc{Chance: 0.1, Effect: "fire_bolt_ignite"}}
	if err := validateItemDef(good); err != nil {
		t.Fatalf("valid onStruckProc rejected: %v", err)
	}
	unknown := &ItemDef{ID: "bad", OnStruckProc: &ItemOnHitProc{Chance: 0.1, Effect: "no_such_effect"}}
	if err := validateItemDef(unknown); err == nil {
		t.Error("expected error for unregistered onStruckProc.effect, got nil")
	}
	badChance := &ItemDef{ID: "bad2", OnStruckProc: &ItemOnHitProc{Chance: 1.5, Effect: "fire_bolt_ignite"}}
	if err := validateItemDef(badChance); err == nil {
		t.Error("expected error for onStruckProc.chance > 1, got nil")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd server && go test ./internal/game/ -run "TestStruckProc_|TestValidateItemDef_OnStruckProc" -v`
Expected: FAIL to compile — ItemDef has no field `OnStruckProc`, EquipmentBonus has no `OnStruckProcs`.

- [ ] **Step 3: Implement**

3a. `items.go` — ItemDef, after `OnHitProc`:
```go
	OnHitProc        *ItemOnHitProc        `json:"onHitProc,omitempty"`
	// OnStruckProc is the on-BEING-hit mirror of OnHitProc: when a basic
	// attack LANDS on the wearer (post-evasion — dodged/blocked hits never
	// trigger it), the wearer rolls Chance and, on success, fires the
	// referenced proc effect back at the attacker. Same schema, validation,
	// resolution, and wire marshaling as OnHitProc.
	OnStruckProc     *ItemOnHitProc        `json:"onStruckProc,omitempty"`
```

3b. `items.go` — refactor `validateItemDef`'s proc branch into a shared helper and validate both fields (replace the existing `if p := def.OnHitProc; p != nil { ... }` block):
```go
	if err := validateItemProcRef(def.ID, "onHitProc", def.OnHitProc); err != nil {
		return err
	}
	if err := validateItemProcRef(def.ID, "onStruckProc", def.OnStruckProc); err != nil {
		return err
	}
```
with (new function below `validateItemDef`):
```go
// validateItemProcRef checks one proc trigger reference (onHitProc or
// onStruckProc — both share the ItemOnHitProc schema) against the proc
// catalog and the override sanity rules.
func validateItemProcRef(itemID, field string, p *ItemOnHitProc) error {
	if p == nil {
		return nil
	}
	if p.Chance < 0 || p.Chance > 1 {
		return fmt.Errorf("item %q %s.chance %v out of range [0,1]", itemID, field, p.Chance)
	}
	if p.Effect == "" {
		return fmt.Errorf("item %q %s.effect is required (a catalog/procs id)", itemID, field)
	}
	if _, ok := getProcEffectDef(p.Effect); !ok {
		return fmt.Errorf("item %q %s.effect %q is not a registered proc effect", itemID, field, p.Effect)
	}
	if p.Damage < 0 {
		return fmt.Errorf("item %q %s.damage override %v must be >= 0", itemID, field, p.Damage)
	}
	if p.ProjectileScale < 0 {
		return fmt.Errorf("item %q %s.projectileScale override %v must be >= 0", itemID, field, p.ProjectileScale)
	}
	return nil
}
```

3c. `state_items.go` — `UnitEquipmentBonus`, after `OnHitProcs`:
```go
	// OnStruckProcs is one entry per equipped item that defines an
	// onStruckProc — rolled when a basic attack lands ON the wearer.
	OnStruckProcs []EquipmentProc
```
and in `recomputeUnitEquipmentBonusLocked`, after the OnHitProc block:
```go
		if p := def.OnStruckProc; p != nil {
			if params, ok := p.ResolveParams(); ok {
				unit.EquipmentBonus.OnStruckProcs = append(unit.EquipmentBonus.OnStruckProcs, EquipmentProc{Chance: p.Chance, Params: params})
			}
		}
```

3d. `state_combat.go` — new function below `rollEquipmentProcsLocked`:
```go
// rollEquipmentStruckProcsLocked rolls the DEFENDER's on-being-struck procs
// after a basic attack lands on them, firing each success back at the
// attacker (the defender is the ProcSource — kill credit and origin are
// theirs). Guards: both units alive (a killing blow doesn't retaliate; a
// dead attacker can't be retaliated against), and proc damage never reaches
// here (SkipOnHitEffects paths bypass resolveAttackHitLocked), so no loops.
// Rolls consume rngPerks AFTER the attacker's on-hit rolls at the same site,
// keeping the stream order deterministic. Caller holds s.mu.
func (s *GameState) rollEquipmentStruckProcsLocked(defender, attacker *Unit) {
	if defender == nil || attacker == nil || len(defender.EquipmentBonus.OnStruckProcs) == 0 {
		return
	}
	if defender.HP <= 0 || attacker.HP <= 0 || !attacker.Visible {
		return
	}
	for _, proc := range defender.EquipmentBonus.OnStruckProcs {
		if proc.Chance <= 0 || proc.Params.Damage <= 0 {
			continue
		}
		if s.rngPerks.Float64() < proc.Chance {
			s.executeProcEffectLocked(procSourceFromUnit(defender), attacker, proc.Params)
		}
	}
}
```
Call site: inside `resolveAttackHitLocked` (opens at state_combat.go:292), immediately AFTER the existing `s.applyEquipmentOnHitEffectsLocked(attacker, target)` call (line ~323) — read the surrounding function first; the call must run after the hit's damage and attacker-side effects so "defender died from this hit" is observable:
```go
	s.applyEquipmentOnHitEffectsLocked(attacker, target)
	// On-being-struck retaliation: the DEFENDER's gear answers a landed
	// basic attack (elemental shields). Runs after the attacker-side on-hit
	// effects so rngPerks stream order is stable and a lethal hit (target
	// now dead) correctly fails the alive-guard inside.
	s.rollEquipmentStruckProcsLocked(target, attacker)
```
IMPORTANT: verify where `applyEquipmentOnHitEffectsLocked` is called inside `resolveAttackHitLocked` relative to the damage application and dead-target handling — if the target's HP is zeroed before this line, the alive-guard inside `rollEquipmentStruckProcsLocked` handles it; if `resolveAttackHitLocked` early-returns on kill BEFORE reaching the on-hit line, the retaliation is correctly skipped too. Read the function; do not blindly paste.

- [ ] **Step 4: Run tests, then whole package**

Run: `cd server && go test ./internal/game/ -run "TestStruckProc_|TestValidateItemDef" -v`
Expected: PASS.
Run: `cd server && go test ./internal/game/ 2>&1 | grep -E "^(--- FAIL|FAIL|ok)"`
Expected: only the known pre-existing failure.

- [ ] **Step 5: Commit**

```bash
git add server/internal/game/items.go server/internal/game/state_items.go server/internal/game/state_combat.go server/internal/game/struck_proc_test.go
git commit -m "Add onStruckProc: defender gear retaliates through the proc-effect system"
```

---

### Task 5: Catalog content — six items, three recipes, two lists

**Files:**
- Create: `server/internal/game/catalog/items/shields/common/rusty_shield.json`
- Create: `server/internal/game/catalog/items/shields/uncommon/steel_shield.json`
- Create: `server/internal/game/catalog/items/shields/rare/fire_shield.json`
- Create: `server/internal/game/catalog/items/shields/rare/frost_shield.json`
- Create: `server/internal/game/catalog/items/shields/rare/lightning_shield.json`
- Create: `server/internal/game/catalog/items/accessories/uncommon/elven_cloak.json`
- Create: `server/internal/game/catalog/recipes/rare/fire_shield.json`, `frost_shield.json`, `lightning_shield.json`
- Modify: `server/internal/game/catalog/items/lists/marketplace.json`
- Modify: `server/internal/game/catalog/recipes/lists/druid_recipes_1.json`
- Test: `server/internal/game/shield_items_test.go` (new)

**Interfaces:**
- Consumes: `onStruckProc` schema (Task 4), `dodgeChance`/`blockChance` modifiers (Task 1), proc effects `fire_bolt_ignite`/`frost_bolt_chill`/`lightning_chain` (already shipped), existing rings `fire_ring`/`ice_ring`/`lightning_ring`, `steel_shield` as a recipe input.
- Produces: the six item IDs + three recipe IDs used by shops/crafting at runtime.

- [ ] **Step 1: Write the failing test**

`server/internal/game/shield_items_test.go`:
```go
package game

import (
	"encoding/json"
	"testing"
)

// TestShieldItems_CatalogWiring guards the six new defs: identity, tier,
// category, and stat invariants (positive armor; block/dodge in (0,1)).
// Numbers are catalog-owned tunables — assert invariants, not values.
func TestShieldItems_CatalogWiring(t *testing.T) {
	cases := []struct {
		id        string
		tier      ItemTier
		wantBlock bool
		wantDodge bool
	}{
		{"rusty_shield", ItemTierCommon, true, false},
		{"steel_shield", ItemTierUncommon, true, false},
		{"fire_shield", ItemTierRare, true, false},
		{"frost_shield", ItemTierRare, true, false},
		{"lightning_shield", ItemTierRare, true, false},
		{"elven_cloak", ItemTierUncommon, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			def, ok := getItemDef(tc.id)
			if !ok {
				t.Fatalf("%s not in catalog", tc.id)
			}
			if def.Tier != tc.tier {
				t.Errorf("tier = %s, want %s", def.Tier, tc.tier)
			}
			if def.Modifiers == nil || def.Modifiers.Armor <= 0 {
				t.Fatalf("%s must grant positive armor, got %+v", tc.id, def.Modifiers)
			}
			if tc.wantBlock && !(def.Modifiers.BlockChance > 0 && def.Modifiers.BlockChance < 1) {
				t.Errorf("blockChance = %v, want in (0,1)", def.Modifiers.BlockChance)
			}
			if tc.wantDodge && !(def.Modifiers.DodgeChance > 0 && def.Modifiers.DodgeChance < 1) {
				t.Errorf("dodgeChance = %v, want in (0,1)", def.Modifiers.DodgeChance)
			}
		})
	}
}

// TestElementalShields_StruckProcWiring: each elemental shield references its
// element's shipped proc effect via onStruckProc at a valid chance.
func TestElementalShields_StruckProcWiring(t *testing.T) {
	wiring := map[string]string{
		"fire_shield":      "fire_bolt_ignite",
		"frost_shield":     "frost_bolt_chill",
		"lightning_shield": "lightning_chain",
	}
	for id, effect := range wiring {
		def, ok := getItemDef(id)
		if !ok {
			t.Fatalf("%s not in catalog", id)
		}
		p := def.OnStruckProc
		if p == nil {
			t.Fatalf("%s has no onStruckProc", id)
		}
		if p.Effect != effect {
			t.Errorf("%s effect = %q, want %q", id, p.Effect, effect)
		}
		if p.Chance <= 0 || p.Chance > 1 {
			t.Errorf("%s chance %v not a valid probability in (0,1]", id, p.Chance)
		}
		if _, ok := p.ResolveParams(); !ok {
			t.Errorf("%s onStruckProc does not resolve", id)
		}
	}
}

// TestShieldRecipes_Wiring: steel_shield + element ring → element shield,
// mirroring the sword recipes.
func TestShieldRecipes_Wiring(t *testing.T) {
	wiring := map[string][2]string{
		"fire_shield":      {"steel_shield", "fire_ring"},
		"frost_shield":     {"steel_shield", "ice_ring"},
		"lightning_shield": {"steel_shield", "lightning_ring"},
	}
	for id, inputs := range wiring {
		def, ok := getRecipeDef(id)
		if !ok {
			t.Fatalf("recipe %s not in catalog", id)
		}
		if len(def.Inputs) != 2 || def.Inputs[0] != inputs[0] || def.Inputs[1] != inputs[1] {
			t.Errorf("recipe %s inputs = %v, want %v", id, def.Inputs, inputs)
		}
		if def.Output != id {
			t.Errorf("recipe %s output = %q, want %q", id, def.Output, id)
		}
		if def.CostGold <= 0 {
			t.Errorf("recipe %s costGold = %d, want > 0", id, def.CostGold)
		}
	}
}

// TestOnStruckProc_MarshalEmitsResolvedPayload guards the client wire
// contract for the struck-proc mirror, same as the onHitProc wire test.
func TestOnStruckProc_MarshalEmitsResolvedPayload(t *testing.T) {
	def, ok := getItemDef("fire_shield")
	if !ok {
		t.Fatal("fire_shield not in catalog")
	}
	data, err := json.Marshal(def.OnStruckProc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal wire: %v", err)
	}
	params, _ := def.OnStruckProc.ResolveParams()
	if wire["effect"] != def.OnStruckProc.Effect {
		t.Errorf("wire effect = %v, want %v", wire["effect"], def.OnStruckProc.Effect)
	}
	if wire["damage"] != float64(params.Damage) {
		t.Errorf("wire damage = %v, want %v (resolved)", wire["damage"], params.Damage)
	}
	if wire["damageType"] != string(params.DamageType) {
		t.Errorf("wire damageType = %v, want %v", wire["damageType"], params.DamageType)
	}
}
```
NOTE: `getRecipeDef` — confirm the exact recipe-lookup function name with `grep -n "func getRecipeDef\|func GetRecipeDef" server/internal/game/*.go` and use what exists; if the accessor differs (e.g. `recipeCatalogSingleton[id]`), adapt the test to the existing accessor. Same for `ItemTierCommon/Uncommon/Rare` constant names — `grep -n "ItemTier" server/internal/game/items.go`.

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./internal/game/ -run "TestShieldItems_|TestElementalShields_|TestShieldRecipes_|TestOnStruckProc_Marshal" -v`
Expected: FAIL — items/recipes not in catalog.

- [ ] **Step 3: Author the catalog files**

`catalog/items/shields/common/rusty_shield.json`:
```json
{
  "id": "rusty_shield",
  "displayName": "Rusty Shield",
  "description": "+10 armor. +5% block chance.",
  "iconKey": "rusty_shield",
  "kind": "equipment",
  "tier": "common",
  "category": "Shield",
  "slotKind": "any",
  "allowedUnitTypes": [],
  "costGold": 75,
  "modifiers": { "armor": 10, "blockChance": 0.05 }
}
```

`catalog/items/shields/uncommon/steel_shield.json`:
```json
{
  "id": "steel_shield",
  "displayName": "Steel Shield",
  "description": "+25 armor. +10% block chance.",
  "iconKey": "steel_shield",
  "kind": "equipment",
  "tier": "uncommon",
  "category": "Shield",
  "slotKind": "any",
  "allowedUnitTypes": [],
  "costGold": 150,
  "modifiers": { "armor": 25, "blockChance": 0.10 }
}
```

`catalog/items/shields/rare/fire_shield.json`:
```json
{
  "id": "fire_shield",
  "displayName": "Fire Shield",
  "description": "+35 armor. +15% block chance. 10% chance when hit to launch a firebolt (25 fire damage) at the attacker, setting them ablaze (8 fire/sec for 3s).",
  "iconKey": "fire_shield",
  "kind": "equipment",
  "tier": "rare",
  "category": "Shield",
  "slotKind": "any",
  "allowedUnitTypes": [],
  "costGold": 0,
  "modifiers": { "armor": 35, "blockChance": 0.15 },
  "onStruckProc": { "chance": 0.1, "effect": "fire_bolt_ignite" }
}
```

`catalog/items/shields/rare/frost_shield.json`:
```json
{
  "id": "frost_shield",
  "displayName": "Frost Shield",
  "description": "+35 armor. +15% block chance. 10% chance when hit to launch a frostbolt (25 cold damage) at the attacker, chilling them (25% slower attack and move speed for 2s).",
  "iconKey": "frost_shield",
  "kind": "equipment",
  "tier": "rare",
  "category": "Shield",
  "slotKind": "any",
  "allowedUnitTypes": [],
  "costGold": 0,
  "modifiers": { "armor": 35, "blockChance": 0.15 },
  "onStruckProc": { "chance": 0.1, "effect": "frost_bolt_chill" }
}
```

`catalog/items/shields/rare/lightning_shield.json`:
```json
{
  "id": "lightning_shield",
  "displayName": "Lightning Shield",
  "description": "+35 armor. +15% block chance. 10% chance when hit to zap the attacker with a lightning bolt (25 lightning damage) that arcs to 2 nearby enemies for 5 less each.",
  "iconKey": "lightning_shield",
  "kind": "equipment",
  "tier": "rare",
  "category": "Shield",
  "slotKind": "any",
  "allowedUnitTypes": [],
  "costGold": 0,
  "modifiers": { "armor": 35, "blockChance": 0.15 },
  "onStruckProc": { "chance": 0.1, "effect": "lightning_chain" }
}
```

`catalog/items/accessories/uncommon/elven_cloak.json`:
```json
{
  "id": "elven_cloak",
  "displayName": "Elven Cloak",
  "description": "+15 armor. +15% dodge chance.",
  "iconKey": "elven_cloak",
  "kind": "equipment",
  "tier": "uncommon",
  "category": "Accessory",
  "slotKind": "any",
  "allowedUnitTypes": [],
  "costGold": 150,
  "modifiers": { "armor": 15, "dodgeChance": 0.15 }
}
```

`catalog/recipes/rare/fire_shield.json`:
```json
{ "id": "fire_shield", "name": "Fire Shield", "inputs": ["steel_shield", "fire_ring"], "costGold": 150, "output": "fire_shield" }
```
`catalog/recipes/rare/frost_shield.json`:
```json
{ "id": "frost_shield", "name": "Frost Shield", "inputs": ["steel_shield", "ice_ring"], "costGold": 150, "output": "frost_shield" }
```
`catalog/recipes/rare/lightning_shield.json`:
```json
{ "id": "lightning_shield", "name": "Lightning Shield", "inputs": ["steel_shield", "lightning_ring"], "costGold": 150, "output": "lightning_shield" }
```

`catalog/items/lists/marketplace.json` — add three IDs:
```json
{
  "id": "marketplace",
  "name": "Marketplace",
  "items": [
    "broad_sword",
    "leather_armor",
    "half_plate",
    "plate_armor",
    "enchanted_ring",
    "potion_common_heal",
    "rusty_shield",
    "steel_shield",
    "elven_cloak"
  ]
}
```

`catalog/recipes/lists/druid_recipes_1.json` — add the three recipes:
```json
{
  "id": "druid_recipes_1",
  "name": "Druid Recipes I",
  "recipes": ["fire_sword", "frost_sword", "lightning_sword", "fire_shield", "frost_shield", "lightning_shield"]
}
```

Icon note: the six `iconKey`s reference client art that may not exist yet. Check how the client resolves missing item icons (`grep -rn "iconKey" client/src/game-portal/src --include="*.ts" -l` and look for a fallback). If there is a graceful fallback, ship as-is and note it; if a missing icon breaks rendering, point each `iconKey` at an existing placeholder asset (e.g. `"iconKey": "plate_armor"` style reuse) and note the six pending art keys in your report.

- [ ] **Step 4: Run the tests, then whole module**

Run: `cd server && go test ./internal/game/ -run "TestShieldItems_|TestElementalShields_|TestShieldRecipes_|TestOnStruckProc_Marshal" -v`
Expected: PASS.
Run: `cd server && go test ./... -count=1 2>&1 | grep -E "^(--- FAIL|FAIL)"`
Expected: only the known pre-existing failures.

- [ ] **Step 5: Commit**

```bash
git add server/internal/game/catalog/ server/internal/game/shield_items_test.go
git commit -m "Add shield items, elven cloak, and elemental shield recipes"
```

---

### Task 6: Client — tooltips, types, dodge/block popups

**Files:**
- Modify: `client/src/game-portal/src/game/maps/itemDefs.ts:25-89` (ItemModifiers + ItemDef types)
- Modify: `client/src/game-portal/src/game/items/itemRules.ts:37-70` (tooltip lines)
- Modify: `client/src/game-portal/src/game/rendering/CanvasRenderer.ts` (~1219-1298 area: consume `evadeEvents`)
- Modify: the client message/snapshot type that mirrors the Go snapshot (find it: `grep -rn "minorDamageEvents" client/src/game-portal/src --include="*.ts" -l`) — add `evadeEvents`
- Test: `client/src/game-portal/src/game/items/itemRules.evasion.test.ts` (new)

**Interfaces:**
- Consumes: wire fields from Tasks 3-5: `modifiers.dodgeChance/blockChance`, `onStruckProc` (resolved payload — same shape as `onHitProc`), snapshot `evadeEvents: [{unitId, kind}]`.
- Produces: tooltip lines; floating "Dodged!"/"Blocked!" popups.

- [ ] **Step 1: Write the failing tooltip test**

`client/src/game-portal/src/game/items/itemRules.evasion.test.ts`:
```ts
import { describe, expect, it } from 'vitest'
import { buildItemTooltipBody } from './itemRules'
import type { ItemDef } from '../maps/itemDefs'

const fireShield: ItemDef = {
  id: 'fire_shield', displayName: 'Fire Shield', iconKey: 'fire_shield',
  kind: 'equipment', tier: 'rare', slotKind: 'any', costGold: 0,
  modifiers: { armor: 35, blockChance: 0.15 },
  onStruckProc: { chance: 0.1, effect: 'fire_bolt_ignite', damage: 25, damageType: 'fire', projectileID: 'fire_bolt' },
}

const elvenCloak: ItemDef = {
  id: 'elven_cloak', displayName: 'Elven Cloak', iconKey: 'elven_cloak',
  kind: 'equipment', tier: 'uncommon', slotKind: 'any', costGold: 150,
  modifiers: { armor: 15, dodgeChance: 0.15 },
}

describe('buildItemTooltipBody — dodge/block + struck procs', () => {
  it('renders block chance and the when-hit proc line', () => {
    const body = buildItemTooltipBody(fireShield)
    expect(body).toContain('+35 Armor')
    expect(body).toMatch(/\+15% Block Chance/i)
    expect(body).toMatch(/10% when hit/i)
    expect(body).toContain('25') // proc damage
    expect(body.toLowerCase()).toContain('fire')
  })
  it('renders dodge chance', () => {
    const body = buildItemTooltipBody(elvenCloak)
    expect(body).toMatch(/\+15% Dodge Chance/i)
  })
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd client/src/game-portal && npm test`
Expected: the new file FAILS (type error on `onStruckProc` / missing tooltip lines); existing tests pass.

- [ ] **Step 3: Implement types + tooltip**

3a. `itemDefs.ts` — `ItemModifiers` gains:
```ts
  maxShield?: number
  /** Additive dodge/block probability (0.15 = +15%). */
  dodgeChance?: number
  blockChance?: number
```
and `ItemDef` gains (after `onHitProc`, same wire shape — the server marshals the resolved payload for both):
```ts
  onStruckProc?: { chance: number; effect?: string; damage: number; damageType: string; projectileID: string }
```

3b. `itemRules.ts` — in the modifiers section (after the maxShield line):
```ts
    if (m.maxShield)   parts.push(`+${m.maxShield} Max Shield`)
    if (m.dodgeChance) parts.push(`+${Math.round(m.dodgeChance * 100)}% Dodge Chance`)
    if (m.blockChance) parts.push(`+${Math.round(m.blockChance * 100)}% Block Chance`)
```
and after the `onHitProc` block:
```ts
  if (def.onStruckProc) {
    const pct = Math.round(def.onStruckProc.chance * 100)
    const elem = def.onStruckProc.damageType.charAt(0).toUpperCase() + def.onStruckProc.damageType.slice(1)
    parts.push(`${pct}% when hit: ${def.onStruckProc.damage} ${elem} bolt at the attacker`)
  }
```

- [ ] **Step 4: Implement the popups**

4a. Find the client snapshot type carrying `minorDamageEvents` (`grep -rn "minorDamageEvents" client/src/game-portal/src --include="*.ts"`). Add alongside it:
```ts
  evadeEvents?: { unitId: number; kind: 'dodge' | 'block' }[]
```
in the same type(s), and mirror any plumbing that forwards `minorDamageEvents` from the websocket message to the renderer.

4b. `CanvasRenderer.ts` — where `message.minorDamageEvents` is consumed (~lines 1219-1298), add a sibling consumption of `message.evadeEvents`: for each event, spawn a floating popup over the unit with text `kind === 'block' ? 'Blocked!' : 'Dodged!'`, using the same popup mechanism as minor damage but with a neutral color (block: `#cbd5e1` tailwind slate-300; dodge: `#fde68a` tailwind amber-200) and no damage number. Follow the EXACT existing popup-spawn pattern in that section (the `minorPool` handling and the popup type at lines 477-493) — read it first, mirror it; do not invent a new mechanism. Evade popups are NOT keyed to HP diffs (the unit took no damage), so spawn them directly rather than through the HP-diff pool.

- [ ] **Step 5: Run client tests + typecheck**

Run: `cd client/src/game-portal && npm test`
Expected: all pass (new evasion tooltip tests + existing suite).
Run: `cd client/src/game-portal && npm run build`
Expected: vue-tsc + vite clean.

- [ ] **Step 6: Commit**

```bash
git add client/src/game-portal/src/game/maps/itemDefs.ts client/src/game-portal/src/game/items/itemRules.ts client/src/game-portal/src/game/items/itemRules.evasion.test.ts client/src/game-portal/src/game/rendering/CanvasRenderer.ts
git commit -m "Client: dodge/block tooltip lines, onStruckProc when-hit line, evade popups"
```
(also `git add` whatever snapshot-type file gained `evadeEvents` in step 4a.)

---

### Task 7: Full verification sweep

**Files:** fixes only if verification finds drift.

**Interfaces:** consumes everything above; produces the green-branch done-signal.

- [ ] **Step 1: Vet + build + full server suite**

Run: `cd server && go vet ./... && go build ./... && go test ./... -count=1 2>&1 | grep -E "^(--- FAIL|FAIL)"`
Expected: vet/build clean; only the known pre-existing failures (see Global Constraints).

- [ ] **Step 2: Determinism spot-checks**

Run: `cd server && go test ./internal/game/ -run "TestAttackHits_Deterministic|TestOnHitProc_Deterministic" -count=5`
Expected: PASS ×5.

- [ ] **Step 3: Client suite + typecheck**

Run: `cd client/src/game-portal && npm test && npm run build`
Expected: green + clean.

- [ ] **Step 4: Leftover grep**

Run: `cd server && grep -rn "projectileHitsLocked" . ; grep -rn "TODO: source real dodge/block" internal/game/`
Expected: no matches for either (old name gone; the dormant-seam TODO comment removed by Task 1).

- [ ] **Step 5: Commit any verification fixes**

```bash
git add -A server/ client/
git commit -m "Dodge/block + shields: verification fixes"
```
(Skip if nothing changed.)
