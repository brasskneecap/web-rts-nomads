# Dodge/Block Attributes + Shield Items — Design

**Date:** 2026-07-07
**Branch:** `crafted-items`
**Status:** Approved
**Depends on:** proc-effects system (`catalog/procs/`, `executeProcEffectLocked`, `ItemOnHitProc` reference schema) — merged into this branch's history.

## Problem

Units have no avoidance stats: every basic attack that resolves, lands. The
codebase shipped a dormant evasion seam for exactly this feature —
`TargetEvasion{DodgeChance, BlockChance}`, `projectileHitsLocked` (rolls
`s.rngCombat`, the isolated seeded stream), and the `evasionForUnit` stub
whose TODO says "source real dodge/block from UnitDef / perks / equipment
when an evasion system is added" (`projectile_defs.go:187-234`). Nothing
calls it from live code.

We want: dodge chance and block chance as stackable unit attributes, four
new purchasable items that grant them, and three crafted elemental shields
that retaliate with the existing proc effects when their wearer is struck.

## Decisions (made during brainstorming)

1. **Scope of evasion: basic attacks only.** Melee swings and basic-attack
   projectiles (including each pierce-arrow victim) can be dodged/blocked.
   Proc bolts/beams, splash, burns/DoTs, traps, abilities, and
   perk-generated secondary hits (cleave, whirlwind sweeps) are "effects" —
   they always land.
2. **Block = full negation**, mechanically identical to dodge; they differ
   only in which items/paths feed them and in the popup shown. Matches the
   existing `avoid = dodge + block` seam. Partial-mitigation block is a
   possible future promotion that would not touch the items.
3. **Additive stacking, combined 75% cap.** All sources sum per stat; the
   cap is applied once at roll time (`min(dodge+block, 0.75)`), never
   stored, so displayed stats stay honest.
4. **Bases:** all units dodge 5% (one game-wide constant); block 0%.
   Vanguard authors per-rank `blockChance`: 0.10 / 0.125 / 0.15
   (bronze/silver/gold). Path JSONs may author per-rank `dodgeChance` /
   `blockChance` overrides generally.
5. **Elemental shield retaliation:** triggers on basic-attack hits that
   LAND on the wearer (post-evasion; dodged/blocked hits do not trigger),
   against melee and ranged attackers alike, 10% chance per item, rolled on
   `rngPerks`. Reuses the shipped proc effect defs unchanged. Proc damage
   cannot re-trigger struck-procs (`SkipOnHitEffects` discipline — no
   loops).
6. **Acquisition:** rusty shield 75g (common), steel shield 150g
   (uncommon), elven cloak 150g (uncommon accessory) — all sold on the
   standard marketplace list. Elemental shields are crafted only:
   steel_shield + <element>_ring at the Artificer for 150g, mirroring the
   elemental sword recipes.

## Architecture

### 1. Unit stats (`unit_defs.go`, path defs, stat pipeline)

- `Unit` gains `DodgeChance`, `BlockChance float64` — recomputed fields with
  the same lifecycle as Armor/AttackSpeed: base → path rank → equipment,
  reapplied wherever rank/equipment recompute happens today
  (`applyRankModifiersLocked` / `recomputeUnitEquipmentBonusLocked`).
- `baseUnitDodgeChance = 0.05` — one game-wide constant. Base block is 0.
- Path rank blocks (e.g. `catalog/units/human/soldier/paths/vanguard/
  vanguard.json`) may author `dodgeChance` / `blockChance` per rank —
  absent means "no path contribution". Vanguard ships 0.10 / 0.125 / 0.15
  block.
- `ItemModifiers` gains `dodgeChance` / `blockChance` (`float64`,
  `omitempty`); `UnitEquipmentBonus` aggregates them additively like every
  other modifier.

### 2. Evasion roll (rename + wire the dormant seam)

- `evasionForUnit(u)` returns the unit's real recomputed totals.
- `projectileHitsLocked` is renamed `attackHitsLocked` (melee uses it now)
  and gains block-vs-dodge attribution: single `rngCombat.Float64()` roll
  against `min(dodge+block, evasionCapTotal)`; if the roll avoids, the
  avoided-by determination checks block first (shields feel active), dodge
  second. Returns (hit bool, avoidedBy string) or equivalent enum.
  `evasionCapTotal = 0.75`.
- One roll per basic-attack hit. Every unit now has ≥5% avoidance, so RNG
  is consumed on every basic-attack hit — deterministic under seed, on the
  `rngCombat` stream that exists for exactly this purpose (isolated from
  perk/loot streams; adding rolls here perturbs nothing else).
- **Hooks:**
  - Melee: `applyDelayedAttackLocked` unit-vs-unit branch, after target
    validation, before `resolveAttackHitLocked`.
  - Ranged: `landProjectileLocked` non-`SkipOnHitEffects` path, before
    `resolveAttackHitLocked`.
  - Pierce: each victim in the pierce corridor rolls independently.
- **Evaded hit = full whiff:** no damage, no on-hit procs, no elemental
  instances, no threat/XP side effects. The attacker's cooldown/animation
  are already spent (committed-swing contract unchanged).
- Buildings never evade (stats are unit-only). Unit-vs-building swings are
  untouched.

### 3. On-being-hit procs (`onStruckProc`)

- `ItemDef` gains `OnStruckProc *ItemOnHitProc` (`json:"onStruckProc"`) —
  the SAME struct as `onHitProc`: chance + effect reference + overrides.
  Validation, `ResolveParams()`, and the resolved-payload `MarshalJSON`
  (client wire contract) are inherited for free.
- `UnitEquipmentBonus` gains `OnStruckProcs []EquipmentProc`, resolved at
  equip time alongside `OnHitProcs`.
- New hook `rollEquipmentStruckProcsLocked(defender, attacker *Unit)` —
  mirror of `rollEquipmentProcsLocked`; called from the two basic-attack
  landing sites (melee + projectile, post-evasion, after damage applies) with
  the ATTACKER as the proc target:
  `executeProcEffectLocked(procSourceFromUnit(defender), attacker, params)`.
- Rolls on `rngPerks` (same stream as weapon procs; consumption points are
  new, appended after the existing on-hit rolls at each site, so ordering
  is deterministic).
- Not triggered by: dodged/blocked hits, proc/beam/splash/DoT damage,
  building attacks. A dead defender's gear does not retaliate (defender HP
  check before rolling).

### 4. New items + recipes (catalog only)

`catalog/items/shields/` (new category directory; category string
"Shield"):

| File | Tier | Payload |
|---|---|---|
| `shields/common/rusty_shield.json` | common | armor +10, blockChance +0.05, 75g |
| `shields/uncommon/steel_shield.json` | uncommon | armor +25, blockChance +0.10, 150g |
| `shields/rare/fire_shield.json` | rare | armor +35, blockChance +0.15, onStruckProc {0.1, fire_bolt_ignite} |
| `shields/rare/frost_shield.json` | rare | armor +35, blockChance +0.15, onStruckProc {0.1, frost_bolt_chill} |
| `shields/rare/lightning_shield.json` | rare | armor +35, blockChance +0.15, onStruckProc {0.1, lightning_chain} |

`catalog/items/accessories/uncommon/elven_cloak.json`: armor +15,
dodgeChance +0.15, 150g.

Recipes (`catalog/recipes/rare/`), mirroring the sword recipes:

```json
{ "id": "fire_shield", "name": "Fire Shield", "inputs": ["steel_shield", "fire_ring"], "costGold": 150, "output": "fire_shield" }
{ "id": "frost_shield", "name": "Frost Shield", "inputs": ["steel_shield", "ice_ring"], "costGold": 150, "output": "frost_shield" }
{ "id": "lightning_shield", "name": "Lightning Shield", "inputs": ["steel_shield", "lightning_ring"], "costGold": 150, "output": "lightning_shield" }
```

Marketplace list gains rusty_shield, steel_shield, elven_cloak. Recipe list
gains the three shield recipes (wherever the sword recipes are listed).
Icons: `iconKey` per item following existing asset conventions (client
assets to be added under the same directories as other item icons; if art
is not ready, reuse a placeholder key the client already ships).

### 5. Wire + client

- **Evasion popups:** new minor combat event on the existing damage-event
  channel (the same pathway that floats damage numbers): kind
  `"dodge" | "block"`, floated over the defender as "Dodged!" / "Blocked!".
  Server emits; client renders; no client-side rolls.
- **Item tooltips** (`itemRules.ts`): add `+X% block chance` /
  `+X% dodge chance` lines from the new modifier fields, and a
  "When hit: ..." line for `onStruckProc` mirroring the existing weapon
  "on hit" line (the resolved payload arrives via the inherited
  MarshalJSON, so the client reads the same fields it already reads for
  weapons).
- Client `ItemDef` TS mirror gains the two modifier fields + optional
  `onStruckProc` (same shape as `onHitProc`).
- Unit snapshot: dodge/block totals are NOT added to the per-tick unit
  snapshot in this branch (no HUD stat display was requested); tooltips are
  item-level only. Future HUD work can add them.

### 6. Determinism & concurrency

- All new rolls under `s.mu` inside the tick, on existing seeded streams
  (`rngCombat` for evasion, `rngPerks` for struck-procs). No wall-clock, no
  new RNG sources, no map-iteration-driven outcomes.
- No pointers stored across ticks: struck-procs resolve defender/attacker
  within the tick via existing patterns.

## Error handling

- Unknown `onStruckProc.effect` → startup panic via `validateItemDef`
  (same rule as `onHitProc`).
- Negative/out-of-range `dodgeChance`/`blockChance` on items → reject in
  `validateItemDef` (must be 0 ≤ v < 1).
- Recipe inputs referencing missing items → existing recipe validation
  applies unchanged.

## Testing

- **Evasion core:** deterministic under fixed seed (two runs, identical
  dodge/block sequence); cap enforced (stacked items > 75% still hit ≥25%
  of the time — assert roll clamp, not statistics); block attributed before
  dodge; zero-evasion... no longer exists — assert the 5% base applies to a
  fresh unit.
- **Hooks:** melee whiff (no damage, no on-hit procs, no threat), projectile
  whiff, pierce per-victim roll, proc bolt/beam/splash/DoT bypass evasion
  entirely (no rngCombat consumption on those paths).
- **Vanguard:** per-rank block from the path JSON (0.10/0.125/0.15).
- **Struck-procs:** fires at melee attacker; fires at ranged attacker
  (bolt homes back); does NOT fire on dodged/blocked hits; proc damage
  never re-triggers struck-procs; dead defender doesn't retaliate.
- **Items/recipes:** catalog wiring guards for all six items + three
  recipes (invariants, not pinned numbers, per file convention); wire test
  for `onStruckProc` MarshalJSON resolved payload.
- **Client:** tooltip lines for block/dodge/when-hit; vitest suite green.

## Out of scope

- Partial-mitigation block, perk-granted dodge/block (the stat pipeline
  supports them; no perk ships them yet), HUD display of unit dodge/block
  totals, evasion for buildings, shield-specific equip-slot restrictions
  (slot rules unchanged), shop/loot table appearances beyond the
  marketplace list.
