# Twin Bronze — Soldier Advancement Node 8 — Implementation Spec

## Overview

Twin Bronze is the 8th node of the Soldier advancement track. A player who owns it grants every Soldier they spawn a **second bronze perk**, drawn from the same per-path bronze pool as the first, deduplicated against the first pick, and granted at the **same rank-up moment** (transition from base → bronze). On the HUD, the second bronze perk occupies slot 12 of the action grid — the cell immediately to the right of the gold perk — and only renders when the perk has actually been granted to that unit.

The perk grant fires inside the deterministic tick loop so the RTS ID-by-reference / `*Locked` discipline from `.claude/rules/AI_RULES.md` applies in full.

## Assumptions & Constraints

- **Determinism**: the second bronze pick draws from `s.rngPerks` immediately after the first. Same seed + same advancement set on the same player → same two perks every run. Confirmed: `progression.go:271` calls `assignUnitPerkLocked` which uses `s.rngPerks.Intn(len(pool))` at `perks.go:927`.
- **Authoritative server**: client never picks perks. Client only renders what the server's snapshot reports in `perkIds`.
- **Per-player scope**: enemy / neutral / other-player units never get a 4th slot. The advancement bakes into the *owning* player's `EffectiveUnitDefs` at match start, and the grant logic reads `s.Players[unit.OwnerID]`.
- **Match start binds the advancement**: a Twin Bronze purchase mid-session takes effect on the **next** match. In-flight units never retroactively gain the slot.
- **Slot 12 is currently a reserved empty cell** (see `GameState.ts:2665-2666` — the comment "Slot 12: reserved empty cell at bottom-right"). Twin Bronze reclaims it.
- **No equivalent profile-upgrade system grants extra perk slots today.**
- **Existing PerkIDs shape**: `[]string` slice ordered by rank-up order. Comment at `state.go:127` says "index 0 = Bronze, 1 = Silver, 2 = Gold" — loosened in §3.
- **Cost**: proposing **300 LP**.

## High-Level Architecture

```
Match Join
  └─ EnsurePlayerWithUpgrades (already shipped)
       └─ applyAdvancementsToEffectiveDefsLocked
            └─ unitExtraPerkSlot handler (NEW)
                  └─ sets Player.ExtraPerkSlots[unitType][tier] = true
                                           │
Tick loop (per-unit, when XP crosses rank threshold)              │
  └─ addUnitXPLocked → assignUnitPerkLocked  ◄────────────────────┘
       └─ first pick: pool[rngPerks.Intn(len)]
       └─ NEW: if Player.ExtraPerkSlots[unitType]["bronze"] && unit.Rank == bronze
              and pool (post-dedup) is non-empty,
              second pick: pool'[rngPerks.Intn(len')]
                                           │
WS snapshot (UnitSnapshot.PerkIDs) ───────►┘
                                           │
                                           ▼
                              Client GameState.getPerkActionItems
                                  → emits 3 OR 4 ActionItems
                              SelectionHud action grid (slot 9-12)
```

## Run / Tick Model — what changes

`assignUnitPerkLocked` is called once per rank promotion. Today it appends exactly one perk to `unit.PerkIDs`. After this change, when the just-completed promotion is to **bronze** AND the unit's owner has Twin Bronze for this unit type, it appends a **second** bronze perk drawn from the same pool, with the first pick excluded.

The second pick happens **synchronously, inside the same `assignUnitPerkLocked` call**, in the same tick, drawing the next number from `s.rngPerks`. This preserves determinism and replay correctness.

## Data Models

### Backend — Player (state.go)

Add one field on the existing `Player` struct (around line 626, alongside `EffectiveUnitDefs`):

```go
// ExtraPerkSlots records advancement-granted extra perk tiers per unit type.
// Outer key: unitType (e.g. "soldier"). Inner key: tier ("bronze" / "silver" /
// "gold"). Value true means assignUnitPerkLocked grants a SECOND perk of that
// tier at the rank-up moment. Computed once at match start by the
// unitExtraPerkSlot effect handler in advancementEffectRegistry and never
// mutated thereafter. Nil-map fast path: a player with no extra-slot
// advancements has a nil map, and the perk-grant logic short-circuits.
ExtraPerkSlots map[string]map[string]bool
```

### Backend — UnitAdvancementEffect (advancement_defs.go)

Extend the existing struct with two new optional fields used only by the `unitExtraPerkSlot` kind:

```go
type UnitAdvancementEffect struct {
    Kind   string `json:"kind"`
    // existing fields:
    Stat   string `json:"stat,omitempty"`
    Amount int    `json:"amount,omitempty"`
    // unitExtraPerkSlot fields:
    Tier   string `json:"tier,omitempty"` // "bronze" | "silver" | "gold"
    Rank   int    `json:"rank,omitempty"` // reserved; ignored by current handler. See §4.
}
```

`Tier` is the perk tier the second slot draws from. `Rank` is carried through from the frontend type for future symmetry but the MVP handler validates `Tier == "bronze"` and `Rank == 1`.

### Unit state — `Unit.PerkIDs`

**Decision: keep `[]string`, no shape change.**

The "index 0 = Bronze, 1 = Silver, 2 = Gold" comment was always shorthand for "rank-up order"; the underlying slice doesn't enforce positions. The grant order will be: append first bronze, append second bronze (if Twin Bronze), append silver on next rank-up, append gold on next rank-up. Slice grows from 3 → 4 entries naturally for Twin Bronze soldiers.

Update the doc-comment to reflect this:

```go
PerkIDs []string // assigned perk ids, in rank-up order. Length is typically
                 // 3 (one per tier). Length 4 indicates the owner had a
                 // unitExtraPerkSlot advancement granting a second pick at
                 // the same rank as one of the existing tiers (see advancement
                 // handler "unitExtraPerkSlot" and assignUnitPerkLocked).
```

**Frontend mirror**: `perkIds?: string[]` is already `string[]` (see `GameState.ts:3078`). No type change.

## API Contract

**No new WS messages or HTTP endpoints.** Everything rides on existing channels:

- **Match join HTTP request** already carries `acquiredAdvancementIds`. No payload change.
- **Snapshot `UnitSnapshot.PerkIDs`** (`messages.go:797`) already serializes `[]string`. A 4-element array is wire-compatible with the existing schema.
- **Advancement catalog endpoint** automatically picks up the new node and the new effect kind once the catalog file and registry handler are added.

## State & Synchronization

- **Match start**: client sends `acquiredAdvancementIds`. Server calls `applyAdvancementsToEffectiveDefsLocked(player)`. The new `unitExtraPerkSlot` handler populates `player.ExtraPerkSlots[node.UnitType][effect.Tier] = true`.
- **Rank-up tick** (inside `s.mu` write lock, called from `addUnitXPLocked` in the tick loop):
  1. `assignUnitPerkLocked(unit)` — appends primary perk.
  2. `maybeAssignExtraPerkLocked(unit)` (new) — checks the owner's `ExtraPerkSlots`, returns early if not enabled or not the right tier; otherwise calls `perkPoolForRankLocked` again (which auto-dedupes against already-owned ids in `perks.go:993-1006`) and appends.
- **Snapshot emission** is unchanged — `unit.PerkIDs` flows as-is.
- **Mid-match purchase**: ignored for the current match. The next call to `EnsurePlayerWithUpgrades` (next match join) picks up the new ID from the profile snapshot at that moment.

## Failure Modes

1. **Bronze pool size == 1**: after the first grant, `perkPoolForRankLocked` for the same rank returns nil (the only entry is filtered out as already-owned), and the cascade falls through to lower ranks — meaningless for bronze. Returns nil. **Second grant is silently skipped.** Matches the existing "Gold pool empty → silent fallback" behavior at `perks.go:961-968`.

2. **`perkPoolForRankLocked` cascade**: current cascade at `perks.go:973-982` — Bronze starting rank has NO fallback. So the second pick stays bronze-only. Returns nil → silently skipped.

3. **Owner left the match between rank-up and snapshot**: `s.Players[unit.OwnerID]` may be nil. The new check must guard. Skip the second grant.

4. **Advancement id removed from catalog mid-deploy** while a stale profile still references it: handled by the existing graceful-skip path in `applyAdvancementsToEffectiveDefsLocked` (`advancement_defs.go:307-310`).

## Backend Implementation Handoff

### 1. Add the registry entry — advancement_defs.go

Add to `advancementEffectRegistry`:

```go
// unitExtraPerkSlot grants a second perk of the named tier at rank-up. The
// match-start applier flips a flag on the Player rather than mutating the
// UnitDef, because the effect fires at rank-up time (perks.go), not spawn
// time. Tier is one of "bronze" / "silver" / "gold"; Rank is reserved for
// future "two silvers / two golds" expansion and must be 1 today.
"unitExtraPerkSlot": {
    validate: func(src string, effect UnitAdvancementEffect) {
        switch effect.Tier {
        case "bronze", "silver", "gold":
            // valid
        default:
            panic(src + `: effect "unitExtraPerkSlot" tier must be "bronze", "silver", or "gold", got "` + effect.Tier + `"`)
        }
        if effect.Rank != 1 {
            panic(src + `: effect "unitExtraPerkSlot" rank must be 1 (only single extra slot is supported today)`)
        }
    },
    applyAtMatchStart: func(def *UnitDef, effect UnitAdvancementEffect) {
        // No-op on UnitDef — this handler signals via Player.ExtraPerkSlots,
        // which is populated by a sibling pass in applyAdvancementsToEffectiveDefsLocked.
    },
},
```

**Important**: `applyAtMatchStart` cannot mutate `Player` directly because its signature is `(*UnitDef, UnitAdvancementEffect)`. Add a second pass on the Player struct in `applyAdvancementsToEffectiveDefsLocked` (around line 322), iterating each owned advancement and routing `unitExtraPerkSlot` effects:

```go
// Inside applyAdvancementsToEffectiveDefsLocked, after the existing effect-handler loop:
for _, id := range ids {
    node, ok := advancementNodesByID[id]
    if !ok {
        continue
    }
    for _, eff := range node.Effects {
        if eff.Kind != "unitExtraPerkSlot" {
            continue
        }
        if player.ExtraPerkSlots == nil {
            player.ExtraPerkSlots = make(map[string]map[string]bool, 1)
        }
        tiers, hasUnit := player.ExtraPerkSlots[node.UnitType]
        if !hasUnit {
            tiers = make(map[string]bool, 1)
            player.ExtraPerkSlots[node.UnitType] = tiers
        }
        tiers[eff.Tier] = true
    }
}
```

### 2. Add the Player field — state.go around line 626

Add `ExtraPerkSlots map[string]map[string]bool` to `Player` as specified above. Update the comment on `PerkIDs` at line 127.

### 3. Add the second-pick path — perks.go `assignUnitPerkLocked`

Modify the existing function at `perks.go:919` to call a new helper at the end:

```go
func (s *GameState) assignUnitPerkLocked(unit *Unit) {
    if unit == nil || unit.Rank == unitRankBase {
        return
    }
    pool := s.perkPoolForRankLocked(unit, unit.Rank)
    if len(pool) == 0 {
        return
    }
    perkID := pool[s.rngPerks.Intn(len(pool))].ID
    unit.PerkIDs = append(unit.PerkIDs, perkID)
    s.applyPerkGrantedHooksLocked(unit, perkID)

    // NEW: extra-slot pass (Twin Bronze and any future unitExtraPerkSlot
    // advancement). Draws from the same rngPerks stream immediately after the
    // primary pick so replay determinism is preserved. The pool re-query
    // automatically excludes the perk just appended via the "already owned"
    // filter in eligiblePerksAfterFiltersLocked.
    s.maybeAssignExtraPerkLocked(unit)
}

// maybeAssignExtraPerkLocked appends one additional perk to unit.PerkIDs when
// the owning player holds a unitExtraPerkSlot advancement for this unit type
// at the unit's current rank tier. Silently no-ops when the owner is absent,
// the advancement isn't owned, or the post-dedup pool is empty. Caller holds s.mu.
func (s *GameState) maybeAssignExtraPerkLocked(unit *Unit) {
    if unit == nil {
        return
    }
    player, ok := s.Players[unit.OwnerID]
    if !ok || player.ExtraPerkSlots == nil {
        return
    }
    tiers, hasUnit := player.ExtraPerkSlots[unit.UnitType]
    if !hasUnit || !tiers[unit.Rank] {
        return
    }
    pool := s.perkPoolForRankLocked(unit, unit.Rank)
    if len(pool) == 0 {
        return // bronze pool exhausted by RequiresPerk filters or pool size 1
    }
    perkID := pool[s.rngPerks.Intn(len(pool))].ID
    unit.PerkIDs = append(unit.PerkIDs, perkID)
    s.applyPerkGrantedHooksLocked(unit, perkID)
}
```

**ID-by-reference compliance** (per AI_RULES.md): `unit *Unit` is a within-tick working value. We do not persist any `*Unit` / `*Player` past this call. `player` is read-only here; we do not store it anywhere. `PerkIDs` is a slice of strings (IDs), not pointers — already compliant.

### 4. Add the advancement node — catalog/units/human/soldier/advancements.json

Append as node index 7 (the 8th entry):

```json
{
  "id": "soldier_twin_bronze",
  "name": "Twin Bronze",
  "description": "Soldiers gain a second Bronze perk at promotion. The two perks are drawn from the same path's Bronze pool and are always distinct.",
  "kind": "major",
  "cost": 300,
  "effects": [
    {
      "kind": "unitExtraPerkSlot",
      "tier": "bronze",
      "rank": 1
    }
  ]
}
```

### 5. Backend files / tasks summary

- Edit `server/internal/game/state.go`: add `ExtraPerkSlots` field to `Player`; update `PerkIDs` doc comment.
- Edit `server/internal/game/advancement_defs.go`: add `Tier` / `Rank` fields to `UnitAdvancementEffect`; register `unitExtraPerkSlot` in `advancementEffectRegistry`; add the per-Player second pass inside `applyAdvancementsToEffectiveDefsLocked`.
- Edit `server/internal/game/perks.go`: add `maybeAssignExtraPerkLocked`; call it from `assignUnitPerkLocked` after the primary append; update the function-level doc comment.
- Edit `server/internal/game/catalog/units/human/soldier/advancements.json`: append `soldier_twin_bronze` node.
- Add tests in `server/internal/game/`.

**Backend non-goals**: do NOT change the `applyAtMatchStart` handler signature. Do NOT mutate `UnitDef` for this effect. Do NOT add a new wire message. Do NOT touch the snapshot builders — `PerkIDs []string` already flows.

## Frontend Implementation Handoff

### 1. Reclaim slot 12 for the second bronze — GameState.ts `getPerkActionItems`

Current implementation at `GameState.ts:3075-3111` hardcodes `PERK_RANKS = ['bronze', 'silver', 'gold']` and emits exactly 3 ActionItems indexed by tier order. The grid composition at `GameState.ts:2657-2667` pads up to slot 8, appends 3 perks, then appends one `emptySlot` for slot 12.

Update both:

```ts
// GameState.ts ~line 3075
function getPerkActionItems(unit: Unit): ActionItem[] {
  // First 3 cells: the canonical bronze/silver/gold tier slots (index 0/1/2
  // in unit.perkIds, in rank-up order). Always emitted — locked placeholders
  // for ranks not yet reached.
  const items: ActionItem[] = PERK_RANKS.map((rank, i) =>
    buildPerkSlot(unit, unit.perkIds?.[i], rank, /*tier label*/ rank),
  )

  // Twin Bronze (and any future unitExtraPerkSlot advancement) appends a
  // second perk to unit.perkIds AT THE SAME RANK as one of the existing tiers.
  // The grant ordering is: primary bronze → secondary bronze → silver → gold.
  // So when perkIds.length > 3, the SECOND entry (index 1) is the second
  // bronze; we render it as a fourth cell at the end (slot 12 of the grid).
  //
  // Locked-state policy: the 4th cell is NOT rendered unless the second
  // perk has actually been granted. We do not draw an empty placeholder
  // for the owner's advancement before the unit reaches Bronze rank.
  if ((unit.perkIds?.length ?? 0) > PERK_RANKS.length) {
    // perkIds layout when Twin Bronze is owned and the unit has promoted to bronze:
    //   [0] = primary bronze
    //   [1] = secondary bronze
    //   [2] = silver (if reached)
    //   [3] = gold (if reached)
    items[0] = buildPerkSlot(unit, unit.perkIds?.[0], 'bronze', 'bronze')
    items[1] = buildPerkSlot(unit, unit.perkIds?.[2], 'silver', 'silver') // shifted
    items[2] = buildPerkSlot(unit, unit.perkIds?.[3], 'gold', 'gold')     // shifted
    items.push(buildPerkSlot(unit, unit.perkIds?.[1], 'bronze', 'bronze'))
  }
  return items
}

// Extracted shared builder — the existing function body lifted verbatim.
function buildPerkSlot(
  unit: Unit,
  perkId: string | undefined,
  rank: 'bronze' | 'silver' | 'gold',
  tierLabel: 'bronze' | 'silver' | 'gold',
): ActionItem {
  const def = perkId ? PERK_DEF_MAP.get(perkId) : undefined
  const rankLabel = tierLabel.charAt(0).toUpperCase() + tierLabel.slice(1)
  if (def) {
    const cd = perkId
      ? unit.perkCooldowns?.find((c) => c.perkId === perkId)
      : undefined
    return {
      id: def.icon ?? 'perk-locked',
      label: def.displayName,
      kind: 'perk' as const,
      perkRank: rank,
      tooltipTitle: `${def.displayName} (${rankLabel})`,
      tooltipBody: formatPerkTooltip(def, unit),
      disabled: true,
      cooldownRemaining: cd?.remaining,
      cooldownTotal: cd?.total,
    }
  }
  return {
    id: 'lock',
    label: `${rankLabel} Perk (locked)`,
    kind: 'perk' as const,
    perkRank: rank,
    tooltipTitle: `${rankLabel} Perk`,
    tooltipBody: 'Locked — earn this rank to unlock.',
    disabled: true,
  }
}
```

### 2. Update the grid composition — GameState.ts:2657-2667

Replace the hardcoded "perks + 1 empty slot" tail:

```ts
const perkActions = getPerkActionItems(unit)
const actions = buildMenuOpen
  ? regularActions
  : [
      ...topActions,
      // Pad to 8 so perks always land starting at slot 9 (bottom-left)
      // regardless of how many action/ability slots are filled.
      ...Array<ActionItem>(Math.max(0, 8 - topActions.length)).fill(emptySlot),
      ...perkActions,
      // When perkActions has length 3 (no extra slot), pad slot 12 with an
      // empty cell. When length 4 (Twin Bronze granted), the 4th cell IS
      // slot 12.
      ...(perkActions.length < 4 ? [emptySlot] : []),
    ]
```

### 3. No new CSS class needed

`action-cell--perk-bronze` already exists (see `SelectionHud.vue:1681`) and the 4th cell uses `perkRank: 'bronze'` so it inherits the same bronze border tint. The action grid is a 4-column CSS grid — slot 12 already has a defined position. No template change in SelectionHud.vue. **Verify** by inspecting `SelectionHud.vue:285-407` — the loop `v-for="i in GRID_SIZE"` already paints cell 12 the same way it paints 9/10/11.

### 4. Frontend files / tasks summary

- Edit `client/src/game-portal/src/game/core/GameState.ts`: refactor `getPerkActionItems` and the action-grid composition as above.
- No edits to `client/src/game-portal/src/components/SelectionHud.vue` needed. (Verify the rendered output in the dev loop.)
- No changes to the advancement tree UI — it reads the server-served catalog and renders nodes generically. Twin Bronze appears automatically once the backend catalog includes it.

**Frontend non-goals**: do NOT add a new snapshot field. Do NOT add a "advancement held" gate on the 4th cell at the component level (the snapshot's `perkIds.length` is the sole signal — server-authoritative). Do NOT render a 4th "locked" placeholder for owners of Twin Bronze whose unit hasn't promoted yet.

## QA — Acceptance Criteria

1. **Baseline regression — player without Twin Bronze**: spawn a Soldier, force XP to 100. Assert `unit.PerkIDs` has length 1 and the entry is one of the path-pool bronze IDs. Advance to 350, length is 2. Advance to 750, length is 3.

2. **Twin Bronze owner — bronze rank-up grants two distinct perks**:
   - Create a player with full prerequisite chain including `soldier_twin_bronze`.
   - Spawn a Soldier owned by that player.
   - Force XP to 100. Assert `len(unit.PerkIDs) == 2`, both entries are in the Vanguard or Berserker bronze pool, and the two entries are distinct.
   - Advance to 350, length is 3 (silver added). Advance to 750, length is 4 (gold added).

3. **Pool dedup invariant**: across 100 deterministic seed iterations, assert `unit.PerkIDs[0] != unit.PerkIDs[1]` whenever `len(unit.PerkIDs) >= 2` and the unit owner has Twin Bronze.

4. **Bronze pool size 1 edge case**: stub the bronze pool and verify the second grant is silently skipped — `len(unit.PerkIDs) == 1`, no panic, no error log.

5. **Determinism — replay reproducibility**: identical seed + identical advancement set ⇒ identical two perk IDs picked.

6. **Mid-session purchase has no effect on in-flight match**: snapshot a `Player`'s `ExtraPerkSlots` at match join; mutate the underlying profile during the match; assert `Player.ExtraPerkSlots` is unchanged after a subsequent rank-up.

7. **Enemy / neutral units never get the slot**: spawn a Soldier with `OwnerID == enemyPlayerID`. Force XP to 100. Assert `len(unit.PerkIDs) == 1`.

8. **HUD rendering — frontend snapshot test**: feed a `Unit` with `perkIds: ["retaliation", "hold_the_line"]` to `getPerkActionItems`. Assert the returned array length is 4, the last cell's `id` matches the icon for `hold_the_line`, and the first three are bronze-real / silver-locked / gold-locked respectively.

9. **HUD rendering — locked state**: feed a `Unit` with `perkIds: []` (unpromoted Soldier of a Twin-Bronze owner). Assert the returned array length is 3, NOT 4.

10. **Catalog load — validation panics on bad effect data**:
    - `tier == "invalid"` → loader panic.
    - `rank == 0` → loader panic.
    - Missing `tier` field → loader panic.

## Out of Scope

- **No automatic re-roll** of perks for a Soldier that already promoted past bronze when the player purchased Twin Bronze. The advancement applies only from the next match.
- **No player-controlled perk selection.** The second bronze is RNG-rolled.
- **No re-roll currency / item** that lets the player swap the second bronze in-match.
- **No HUD affordance to indicate "this unit's owner has Twin Bronze".** The 4th cell appearing is the only signal.
- **No support for two silvers / two golds today.** The `Rank` field in `UnitAdvancementEffect` is reserved for future expansion.
- **No interaction with `assignUnitPathAbilitiesLocked`** — the second bronze grants a perk, not an ability.
- **No catalog hard-error for pool size 1.**

## Cross-cutting Pins

- **Effect kind string** is `"unitExtraPerkSlot"` everywhere — frontend type (`profile.ts:9`) and backend registry key.
- **Tier string vocabulary** is `"bronze" | "silver" | "gold"` matching `unitRankBronze / unitRankSilver / unitRankGold` constants in `progression.go:9-13`.
- **Advancement node ID** is `"soldier_twin_bronze"`.
- **Slice ordering invariant**: `unit.PerkIDs` is in **rank-up grant order** (primary bronze, secondary bronze, silver, gold). The frontend `getPerkActionItems` remap relies on this — if grant ordering ever changes, both the comment on `Unit.PerkIDs` AND the frontend remap must be updated together.

## Open Questions (for product/design)

1. **Cost of `soldier_twin_bronze`**: proposing 300 LP.
2. **Should the 4th HUD cell render as a locked placeholder before the unit promotes to bronze?** Spec decision is "no — only appears at grant time".
3. **Future: stacking with hypothetical Triple Bronze?** Out of scope; map shape would need to change from `bool` to `int` count.
