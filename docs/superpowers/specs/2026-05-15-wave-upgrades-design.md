# Wave Upgrades — Design Spec

**Date:** 2026-05-15
**Status:** Approved

---

## Overview

At the end of each wave the game pauses and each player is presented with three upgrade cards to choose from. Upgrades range from stat boosts to equipment drops and XP grants, growing rarer and more powerful as the run progresses. The system is designed to produce "god-run" moments where a player who picks well feels increasingly unstoppable.

---

## 1. Data Model

### Upgrade Definitions

New JSON catalog at `catalog/upgrades/*.json`, one file per upgrade. Follows the same embed/load pattern as unit and building defs.

```json
{
  "id": "swift_strikes_common",
  "group": "swift_strikes",
  "name": "Swift Strikes",
  "description": "+8% attack speed to all Ranged units",
  "rarity": "common",
  "scope": "archetype",
  "archetype": "ranged",
  "effect": { "stat": "attackSpeed", "multiplier": 1.08 },
  "maxStacks": 3
}
```

**Fields:**

| Field | Type | Notes |
|---|---|---|
| `id` | string | Globally unique. Naming convention: `<group>_<rarity>`. |
| `group` | string | Ties rarity variants together. Stack cap is per-group. |
| `name` | string | Display name shown on the card. |
| `description` | string | One-line effect summary shown on the card. |
| `rarity` | string | `"common"` \| `"rare"` \| `"epic"` \| `"legendary"` |
| `scope` | string | `"army"` \| `"archetype"` \| `"unitType"` \| `"xp"` \| `"equipment"` |
| `archetype` | string | Required when scope = `"archetype"`. Matches `Unit.Archetype`. |
| `unitType` | string | Required when scope = `"unitType"`. Matches `Unit.UnitType`. |
| `effect` | object | See Effect Types below. |
| `maxStacks` | int | Max times this group can be taken in one run (default 3). |

**Effect types (MVP):**

- **Stat multiplier:** `{ "stat": "attackSpeed" | "damage" | "hp" | "moveSpeed" | "attackRange", "multiplier": float }`
- **XP grant:** `{ "type": "xp", "amount": int }` — player picks target unit
- **Equipment drop:** `{ "type": "equipment", "itemID": string }` — deposited to vault

### Player Upgrade State (in-match, per player)

```go
type PlayerUpgradeState struct {
    UpgradeStacks    map[string]int // group → times taken this run
    RerollsRemaining int            // resets to MaxRerolls at each wave
    MaxRerolls       int            // legend-incrementable, default 1
    MaxUpgradeStacks int            // legend-incrementable, default 3
}
```

`UpgradeStacks` resets between runs. `MaxRerolls` and `MaxUpgradeStacks` are persistent player stats incremented by the legend system.

### Tuning Config (`gameplay_tuning.json`)

```json
{
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
  }
}
```

`timerSeconds` is the soft-timer duration. `rarityScalePerWave` adjusts weights each wave (weights are clamped to ≥ 0). Milestone waves guarantee at least one card of `milestoneMinRarity` or higher.

---

## 2. Server-side Flow

```
Wave cleared
  └─ transition to WaveUpgradePhase
       ├─ for each player: generateUpgradeOffersLocked(playerID)
       │    ├─ build eligible pool (filter maxed groups)
       │    ├─ apply rarity weights (wave number + milestone)
       │    ├─ sample 3 distinct upgrades
       │    └─ send WaveUpgradeOfferMessage to client
       ├─ set per-player upgradeDeadline = now + timerSeconds
       └─ each tick: check deadlines
            ├─ deadline passed → auto-apply first card, mark resolved
            └─ all resolved → transition to next wave
```

**Reroll path:** client sends `WaveUpgradeRerollMessage` → server checks `RerollsRemaining > 0` → generates 3 new cards from eligible pool → decrements `RerollsRemaining` → sends new `WaveUpgradeOfferMessage`. Timer does NOT reset on reroll.

**Choice path:** client sends `WaveUpgradeChoiceMessage` → server calls `applyUpgradeLocked(playerID, upgradeID, targetUnitID)` → marks player resolved.

**Multiplayer:** next wave does not start until every player is resolved (picked or timed out). Players who resolve early see a "Waiting for others…" state on their modal.

---

## 3. Protocol Messages

### Server → Client

```
WaveUpgradeOfferMessage
  wave:        int
  offers:      []UpgradeOffer     // always 3
  rerollsLeft: int
  deadline:    int64              // unix ms

UpgradeOffer
  id:                 string
  group:              string
  name:               string
  description:        string
  rarity:             string
  scope:              string
  stackCurrent:       int
  stackMax:           int         // player's effective cap (legend-adjusted)
  requiresTargetUnit: bool        // true for xp grants
```

### Client → Server

```
WaveUpgradeChoiceMessage
  upgradeID:    string
  targetUnitID: int     // only set when requiresTargetUnit = true

WaveUpgradeRerollMessage
  (no payload)
```

---

## 4. Client UI

**Layout:** centered overlay modal. The game canvas is dimmed behind a dark panel. The modal cannot be dismissed — the player must pick or let the timer expire.

**Timer:** a horizontal progress bar across the top of the modal counts down from `timerSeconds` to 0. Colour shifts green → yellow → red in the final 5 seconds.

**Cards:** three cards displayed side by side. Each card shows: rarity label (colour-coded), upgrade name, description, current stack count / max (e.g. "Stack 1 / 3"). Clicking a card confirms the choice immediately.

**Rarity colour coding:**

| Rarity | Border / label colour |
|---|---|
| Common | Grey (`#64748b`) |
| Rare | Indigo (`#6366f1`) |
| Epic | Gold (`#f59e0b`) |
| Legendary | Red (`#ef4444`) |

**Reroll button:** below the cards, visible when `rerollsLeft > 0`. Disabled (greyed) at 0. Shows remaining count.

**XP grant secondary step:** when a player selects an XP grant card, the modal transitions to a unit-picker list. Player clicks a unit; that unit receives the XP. The timer continues running during this step.

**Waiting state:** once a player has resolved, their modal collapses to a "Waiting for other players…" banner with a count of remaining players.

---

## 5. Effect Application

`applyUpgradeLocked(playerID, upgradeID, targetUnitID int)`:

1. Look up `UpgradeDef` by `upgradeID`.
2. Increment `UpgradeStacks[def.group]`.
3. Dispatch on effect type:
   - **Stat multiplier** — iterate all player-owned units matching scope; multiply the relevant `Base*` stat; call `applyRankModifiersLocked` to recompute derived stats. Same path as rank-ups.
   - **XP grant** — resolve `targetUnitID`; add `effect.amount` XP; trigger rank-up check via existing XP flow.
   - **Equipment drop** — call existing vault deposit flow with `effect.itemID`.

---

## 6. Legend System Hooks

Two persistent player stats the legend system can increment without touching wave upgrade logic:

| Stat | Default | Effect |
|---|---|---|
| `MaxRerolls` | 1 | Rerolls available per wave |
| `MaxUpgradeStacks` | 3 | Override per-upgrade `maxStacks` when higher |

At wave start `RerollsRemaining` is reset to `MaxRerolls`. `MaxUpgradeStacks` overrides a def's own `maxStacks` if the player's legend-granted cap is higher (allows pushing past the authored default for a specific upgrade without re-authoring the JSON).

---

## Out of Scope (this iteration)

- Upgrade synergy bonuses (two upgrades together granting a bonus)
- Upgrade preview / history screen during the run
- Per-upgrade reroll exclusions ("never show this again")
- Paid rerolls (gold cost) — deferred to post-legend-system pass
