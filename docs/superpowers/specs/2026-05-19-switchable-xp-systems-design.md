# Switchable XP Systems — Design

**Date:** 2026-05-19
**Status:** Approved (brainstorming) — pending spec review

## Summary

Add a server-wide switch between two experience-gaining systems:

- **`classic`** (default, unchanged): kill bonus + banked damage-dealt XP +
  soldier-tank-contribution XP, all scaled by the global `xpGainMultiplier`
  (0.2). This is exactly today's behavior.
- **`split`**: when an enemy unit dies, a single per-unit *experience* value
  is split evenly among every eligible friendly unit, as raw (unscaled) XP.
  This fully replaces the classic payouts so the total XP that can enter a
  map is precisely bounded by the sum of enemy experience values.

The switch lives in the existing global tuning singleton
(`catalog/tuning/gameplay_tuning.json`); changing it requires a server
restart. Default is `classic`, so the existing test suite and current
balance are provably untouched.

## Goals

- Tight, readable control over total XP available on a map (`split` mode).
- Zero behavior change in `classic` mode (verbatim relocation of existing
  payout calls).
- One isolated place for the mode branch; minimal, mechanically-checkable
  edit surface at the existing kill sites (19 invariant-pair sites across 5
  files, plus 1 building-payout site).

## Non-goals

- No lobby/per-match selection, no runtime/debug toggle (explicitly chosen:
  global tuning JSON only).
- No client/protocol/UI changes — this is a server simulation change.
- Buildings grant **no** XP in `split` mode (units-only concept).
- Not "fixing" the pre-existing inconsistency where trap kills do not grant
  soldier-tank XP (see §6). Classic behavior is preserved exactly as-is and
  the inconsistency is flagged for the balance team, not changed here.

## Decisions (resolved during brainstorming)

| Question | Decision |
|---|---|
| How is the switch controlled? | Global `gameplay_tuning.json`, server restart. |
| Does `split` replace or stack on classic payouts? | **Fully replace.** |
| Is the experience value scaled by the 0.2 multiplier? | **No — literal/raw.** Fractions still accumulate per unit. |
| No eligible recipients on death? | **XP is lost** (no killer fallback). |
| Enemy building destruction in `split`? | **No XP** to anyone. |
| Eligibility radius unit? | World pixels (same as vision/splash). |
| Recipient ownership scope? | Any non-enemy, alive, visible unit (the existing `unitCanGainXPLocked` set) — pools across all human players in co-op. Approved. |

## 1. Configuration

New block in `server/internal/game/catalog/tuning/gameplay_tuning.json`:

```json
"experience": {
  "mode": "classic",
  "splitDefaultXP": 10,
  "splitEligibilityRadius": 500
}
```

- `mode`: `"classic"` | `"split"`. Default `"classic"`.
- `splitDefaultXP`: fallback experience for an enemy whose unit def omits
  the `experience` field. Default `10`.
- `splitEligibilityRadius`: proximity radius in world pixels, measured from
  the dying unit's position at the moment of death. Default `500`.

`tuning_defs.go`:

- Add `ExperienceTuning` struct + `Experience ExperienceTuning
  \`json:"experience"\`` field on `GameplayTuning`.
- `init()` validation: `mode` ∈ {`classic`,`split`} (panic otherwise);
  `splitDefaultXP >= 0`; `splitEligibilityRadius > 0`.
- `Version` stays `1` (additive field; the `Version != 1` panic check is
  unchanged).
- A test seam is added to swap `gameplayTuningSingleton` and restore it (see
  §7). This is the only practical way to exercise `split` mode in unit
  tests given the embedded-JSON singleton.

## 2. Per-unit experience value

- `UnitDef` (`unit_defs.go`) gains:
  `Experience *int \`json:"experience,omitempty"\``.
  Pointer type so the catalog distinguishes **absent → use
  `splitDefaultXP` (10)** from **explicit `0` → this unit grants no XP**
  (legitimate for summons / temporary units).
- `Unit` (`state.go`) gains `XPValue int`, seeded **once at spawn** in
  `state_spawn.go` (matches the `BaseDamage` / `BaseMaxHP` idiom — keeps
  def lookups out of the death pipeline):
  - `def.Experience == nil` → `gameplayTuning().Experience.SplitDefaultXP`
  - else → `*def.Experience`
  Seeded in both modes; harmlessly unused in `classic`.

## 3. Integration (Approach A, refined)

At **every** kill site the invariant pair is
`awardKillXPLocked(killer)` immediately followed by
`payoutDamageDealtXPLocked(dead)`. That pair — and only that pair — is
replaced by a single call to a new function:

```go
// progression.go
func (s *GameState) awardUnitDeathXPLocked(dead, killer *Unit) {
    if dead == nil {
        return
    }
    if gameplayTuning().Experience.Mode == experienceModeSplit {
        s.awardSplitDeathXPLocked(dead) // §4; killer intentionally ignored
        return
    }
    // classic — verbatim relocation, same order as before
    if killer != nil {
        s.awardKillXPLocked(killer)
    }
    s.payoutDamageDealtXPLocked(dead)
}
```

- **classic branch** calls exactly the two original functions in the
  original order → byte-for-byte prior behavior. (Sites that passed a
  guaranteed-non-nil killer are unaffected by the `killer != nil` guard;
  it only formalizes what every site already ensured.)
- **split branch** runs §4 and ignores `killer`.

The other two payout functions are **left at their existing call sites
unchanged** and made split-safe with a single early-return guard at the top
of each:

```go
func (s *GameState) awardSoldierTankKillXPLocked(defeatedUnitID int) {
    if gameplayTuning().Experience.Mode == experienceModeSplit {
        return
    }
    // ...existing body unchanged...
}

func (s *GameState) payoutBuildingDamageDealtXPLocked(buildingID string) {
    if gameplayTuning().Experience.Mode == experienceModeSplit {
        return
    }
    // ...existing body unchanged...
}
```

Net effect:
- `classic`: identical to today (the pair is relocated, not altered;
  tank/building payouts run their existing bodies).
- `split`: only the dying enemy's `XPValue` enters the map; tank payout and
  building payout return early; classic kill/damage payouts are not called.

### Call sites to convert

Each site's local attacker/owner maps to `killer`, the dying unit to
`dead`. Sites that additionally call `awardSoldierTankKillXPLocked(dead.ID)`
keep that separate call (now split-guarded).

| File | Lines (approx) | `dead` | `killer` | Has tank call? |
|---|---|---|---|---|
| `damage_pipeline.go` | 119–121 | `target` | `attackerUnit` | yes |
| `damage_pipeline.go` | 153–155 | `target` | `ownerUnit` (trap owner) | yes |
| `state_combat.go` | 255–257 | `attacker` (reflect death) | `target` | yes |
| `state_combat.go` | 271–273 | `target` | `attacker` | yes |
| `state_combat.go` | 319–321 | `u` (splash victim) | `attacker` | yes |
| `state_combat.go` | 431 | building destruction | — | (building payout; split-guarded) |
| `perks_attack.go` | 231–233 | `candidate` | `attacker` | yes |
| `perks_attack.go` | 310–312 | `secondary` | `attacker` | yes |
| `perks_marksman.go` | 595–597 | `candidate` | `attacker` | yes |
| `trap.go` | 350, 384, 418, 526, 812, 1270, 1325, 1382, 1446, 1629, 1662 (each with the following `payout…` line) | `unit`/`victim`/`u` | `ownerUnit`/`owner` | **no** (pair only — unchanged) |

Trap sites are pair-only today (no tank call) and remain pair-only. The
unified function does **not** add a tank payout, so trap-kill behavior in
`classic` is unchanged.

The implementation plan must enumerate and verify each site individually;
"is the pair faithfully preserved per site" is the entire `classic`
regression surface and is mechanically checkable.

## 4. Split algorithm

`awardSplitDeathXPLocked(dead *Unit)`, per enemy death in `split` mode:

1. If `dead.XPValue <= 0` → return (unit grants no XP).
2. Build the eligible recipient set, deduplicated by unit ID
   (`map[int]bool`):
   - **Proximity:** iterate `s.Units`; include `u` where
     `s.unitCanGainXPLocked(u)` and squared distance from `u` to `dead` ≤
     `radius²` (squared-distance comparison, mirroring
     `applySplashDamageLocked`). `radius =
     gameplayTuning().Experience.SplitEligibilityRadius`.
   - **Contributors:** for each `attackerID` in `dead.DamageDealtByUnit`
     (already populated in all modes by `recordDamageDealtLocked`), resolve
     via `s.getUnitByIDLocked`; include if `s.unitCanGainXPLocked`. This
     covers "dealt damage at any point", including a contributor that
     survived but walked out of range.
3. If the set is empty → **XP is lost**, return (no killer fallback).
4. `share := float64(dead.XPValue) / float64(len(set))`.
5. For each unit in the set, award `share` via a new
   `addUnitXPRawFloatLocked(unit, share)`:
   - Same per-unit `XPProgressRemainder` accumulator and one-rank-at-a-time
     advance logic as `addUnitXPFloatLocked`, **but without the
     `xpGainMultiplier` (0.2) scaling** — `share` is added raw.
   - Fractions (e.g. 0.5) accumulate per unit until they form whole XP and
     cross rank thresholds normally.

`addUnitXPRawFloatLocked` and `addUnitXPFloatLocked` share the
`XPProgressRemainder` field. Only one mode is active per server run, so
scaled and raw contributions never mix into the same accumulator.

### Determinism

The contributor set is built by iterating a Go map (`DamageDealtByUnit`),
whose iteration order is nondeterministic. This does **not** violate the
determinism invariant: every recipient receives the identical `share`
added exactly once, and set membership is order-independent — iteration
order does not drive any outcome. This is called out explicitly so code
review does not flag it as a false positive.

### Eligibility ownership

`unitCanGainXPLocked` already defines eligibility as any non-enemy, alive,
visible unit. The split therefore pools across all non-enemy units,
including across multiple human players in co-op. This is the intended
shared-pool behavior and is consistent with the existing invariant.

## 5. Components changed

| Component | Change |
|---|---|
| `catalog/tuning/gameplay_tuning.json` | New `experience` block. |
| `tuning_defs.go` | `ExperienceTuning` struct, field, validation, test seam, mode constants. |
| `unit_defs.go` | `UnitDef.Experience *int`. |
| `state.go` | `Unit.XPValue int`. |
| `state_spawn.go` | Seed `XPValue` at spawn (both spawn paths). |
| `progression.go` | `awardUnitDeathXPLocked`, `awardSplitDeathXPLocked`, `addUnitXPRawFloatLocked`; split guards on `awardSoldierTankKillXPLocked` and `payoutBuildingDamageDealtXPLocked`. |
| 5 kill-site files (`damage_pipeline.go`, `state_combat.go`, `perks_attack.go`, `perks_marksman.go`, `trap.go`) | Replace the invariant pair with `awardUnitDeathXPLocked(dead, killer)`. |

No client, protocol, or persistence changes.

## 6. Pre-existing inconsistency (flagged, not changed)

Trap kills currently do **not** grant soldier-tank XP, while combat kills
do. This design preserves that behavior exactly. It is flagged here for the
balance team to decide intentionally; "migrating" it as a side effect of
this change is explicitly out of scope (per repo rule: trust the code,
flag the doc — don't silently change working behavior).

## 7. Testing

Go unit tests in the `game` package, following `death_pipeline_test.go`
patterns. A test helper swaps `gameplayTuningSingleton` and restores it via
`t.Cleanup` (only practical seam for the global-singleton switch).

**Classic regression**
- Default `classic` → the entire existing suite stays green with no test
  edits.
- Targeted asserts that `awardUnitDeathXPLocked` reproduces the original
  pair at representative sites (melee kill, trap kill, splash kill,
  reflect death) and that building destruction still pays in `classic`.

**Split mode**
- 4 eligible recipients, `experience = E` → each gains `E/4`.
- 20 eligible, `E = 10` → 0.5 each; assert `XPProgressRemainder`
  accumulation and eventual rank-up after enough kills.
- Contributor that dealt damage then moved out of range → still eligible.
- Contributor that died before the kill → excluded (`unitCanGainXPLocked`).
- Zero eligible → XP lost, no panic, no unit gains.
- `experience` absent → `splitDefaultXP`; explicit `0` → no XP; explicit
  value honored.
- Enemy building destroyed → no unit gains XP.
- Soldier-tank payout suppressed in `split`.

**Conventions**
- Expected values are derived from the tuning config and the unit's
  `experience` field — never pinned balance literals (per repo testing
  rule). Balance-independent invariants asserted, e.g.
  `Σ distributed == experience` and `share == experience / len(set)`.

QA sign-off via the `qa-engineer` subagent after implementation;
determinism and the per-site preservation check are squarely in scope.

## Open questions

None. All resolved during brainstorming.
