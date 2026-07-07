# Proc Effects System — Design

**Date:** 2026-07-06
**Branch:** `proc-effects`
**Status:** Approved

## Problem

On-hit proc effects (fire bolt + burn, frost bolt + slow, lightning chain) are
welded to the equipment path in three places:

1. The definition lives inline on `ItemDef.OnHitProc` (`items.go`).
2. The runtime copy lives on `UnitEquipmentBonus.OnHitProcs` as `EquipmentProc`
   (`state_items.go`).
3. The only trigger is the equipment on-hit hook
   (`rollEquipmentProcsLocked`, `state_combat.go`), which owns both the chance
   roll and the effect execution.

We want the same effects fireable from perks, abilities, traps, buildings, and
consumable-granted buffs — including **non-unit sources** — without copying the
payload numbers around.

## Decisions (made during brainstorming)

1. **Split roll from execute.** The reusable core is
   `executeProcEffectLocked(source, target, params)` — no RNG inside,
   deterministic by construction. Chance rolling stays with each trigger
   (equipment on-hit keeps its roll against `rngPerks`; an ability or trap
   calls execute directly).
2. **Named proc-effect catalog with inline overrides.** Effects are authored
   once in `catalog/procs/*.json` and referenced by ID. A reference may
   override any tuning knob — damage, scale, bounce count/range/falloff,
   slow, burn (e.g. a trap uses `frost_bolt_chill` but with `damage: 40`; an
   ability upgrade bumps `lightning_chain` to `bounceCount: 4`). Dynamic
   tunability is a design goal: ability upgrades must be able to scale
   effects without authoring new defs. Only `damageType` and `projectileID`
   are fixed per def (they are the effect's identity).
3. **Sources may be non-units.** The source is a value struct carrying an
   owner unit ID (0 = none), owner player ID, and origin coordinates — IDs,
   never pointers, per AI_RULES.

## Architecture

### New file: `server/internal/game/proc_effect_defs.go`

Catalog loader following the `projectile_defs.go` pattern:

- `//go:embed catalog/procs` — flat layout `catalog/procs/<id>.json` (proc
  effects carry no client assets, so no per-id directory is needed).
- Filename must match the JSON `id`; mismatch panics at startup.
- Loaded by a **var initializer** (not `init()`) so `items.go` validation can
  reference it via Go's dependency-ordered var initialization — the same trick
  `itemCatalogSingleton` uses for the loot-table loader.

```go
// ProcEffectDef is a reusable, trigger-agnostic effect payload. It is
// today's EquipmentProc minus Chance: whether it fires is the trigger's
// business, what happens when it fires is defined here.
type ProcEffectDef struct {
    ID                  string     `json:"id"`
    Damage              int        `json:"damage"`
    DamageType          DamageType `json:"damageType"`
    ProjectileID        string     `json:"projectileID"`
    ProjectileScale     float64    `json:"projectileScale,omitempty"`
    BounceCount         int        `json:"bounceCount,omitempty"`
    BounceRange         float64    `json:"bounceRange,omitempty"`
    BounceDamageFalloff int        `json:"bounceDamageFalloff,omitempty"`
    SlowMultiplier      float64    `json:"slowMultiplier,omitempty"`
    SlowDurationSeconds float64    `json:"slowDurationSeconds,omitempty"`
    BurnDamagePerSecond float64    `json:"burnDamagePerSecond,omitempty"`
    BurnDurationSeconds float64    `json:"burnDurationSeconds,omitempty"`
}
```

Startup validation: `damage > 0`, registered `damageType`, non-empty
`projectileID` (same rules `validateItemDef` enforces today).

### New file: `server/internal/game/proc_effects.go`

The execute core:

```go
// ProcSource identifies who/where a proc effect fires from. Non-unit
// sources (traps, buildings) set OwnerUnitID = 0 — no kill credit / XP is
// awarded and the projectile originates at OriginX/Y.
type ProcSource struct {
    OwnerUnitID   int
    OwnerPlayerID string
    OriginX       float64
    OriginY       float64
}

// procSourceFromUnit is the common-case constructor.
func procSourceFromUnit(u *Unit) ProcSource

// ProcEffectParams is a resolved ProcEffectDef (def + overrides applied).
// Same fields as ProcEffectDef minus ID.
type ProcEffectParams struct { ... }

// executeProcEffectLocked routes by the emitter def's kind: beam-kind zaps
// instantly with deferred damage; projectile-kind (default) fires a homing
// bolt. No RNG — callers own their chance rolls. Caller holds s.mu.
func (s *GameState) executeProcEffectLocked(src ProcSource, target *Unit, p ProcEffectParams)
```

`fireOnHitProcProjectileLocked` and `fireOnHitProcBeamLocked` are refactored
in place (they stay in `projectile.go`) to take `(src ProcSource, target
*Unit, p ProcEffectParams, ...)` instead of `(attacker *Unit, proc
EquipmentProc, ...)`. Behavior notes:

- `Projectile.OwnerUnitID` / beam attacker fields already store IDs; a 0
  owner-unit ID must flow through landing / kill-credit paths without
  awarding XP or crashing. The landing code already nil-checks
  `getUnitByIDLocked` results, so this is an audit + test, not a rewrite.
- Bounce-chain exclusion of the attacker keys off `src.OwnerUnitID`; 0 is
  simply never matched.
- `SkipOnHitEffects` semantics are unchanged — a proc cannot trigger a proc.

### Item schema change (`items.go`, `state_items.go`)

`ItemOnHitProc` becomes a reference + overrides:

```json
"onHitProc": { "chance": 0.1, "effect": "frost_bolt_chill" }
```

```go
// ProcEffectOverrides (defined in proc_effects.go) is the reusable
// override bag — every trigger that references an effect embeds it, so
// items, perks, abilities, and traps all share one override vocabulary.
type ProcEffectOverrides struct {
    Damage              int     `json:"damage,omitempty"`
    ProjectileScale     float64 `json:"projectileScale,omitempty"`
    BounceCount         int     `json:"bounceCount,omitempty"`
    BounceRange         float64 `json:"bounceRange,omitempty"`
    BounceDamageFalloff int     `json:"bounceDamageFalloff,omitempty"`
    SlowMultiplier      float64 `json:"slowMultiplier,omitempty"`
    SlowDurationSeconds float64 `json:"slowDurationSeconds,omitempty"`
    BurnDamagePerSecond float64 `json:"burnDamagePerSecond,omitempty"`
    BurnDurationSeconds float64 `json:"burnDurationSeconds,omitempty"`
}

type ItemOnHitProc struct {
    Chance              float64 `json:"chance"`
    Effect              string  `json:"effect"` // ProcEffectDef ID, required
    ProcEffectOverrides         // embedded; JSON fields flatten inline
}
```

`validateItemDef` additionally checks the referenced effect exists. Override
semantics: zero value = "use the def's value" (consistent with existing
`ProjectileScale` 0-means-inherit convention). Every tuning knob — damage,
scale, bounce count/range/falloff, slow, burn — is overridable so future
consumers (ability upgrades in particular) can scale an effect without
authoring new defs. Only `damageType` and `projectileID` are fixed per def:
they define the effect's identity (visuals, element, CC payload); a different
element is a different effect.

Known limitation of zero-means-inherit: an override cannot disable a def's
non-zero field (e.g. bounce a chaining effect down to 0 hops). No sentinel
value until a real case needs it — author a chainless def instead.

Override resolution lives in one shared helper so future upgrade systems
reuse it rather than reimplementing precedence:

```go
// resolveProcEffectParams applies non-zero override fields onto a copy of
// the def. The single precedence implementation for ALL consumers.
func resolveProcEffectParams(def ProcEffectDef, o ProcEffectOverrides) ProcEffectParams
```

Runtime resolution happens **at equip time** in
`recomputeUnitEquipmentBonusLocked`:

```go
type EquipmentProc struct {
    Chance float64
    Params ProcEffectParams // def + overrides, resolved once
}
```

The per-hit path stays catalog-lookup-free, same as today.
`rollEquipmentProcsLocked` shrinks to: roll chance → 
`s.executeProcEffectLocked(procSourceFromUnit(attacker), target, proc.Params)`.

### New catalog defs (`catalog/procs/`)

Extracted verbatim from the three shipped swords:

| File | Payload |
|---|---|
| `fire_bolt_ignite.json` | 25 fire, `fire_bolt`, burn 8 dps / 3 s |
| `frost_bolt_chill.json` | 25 cold, `frost_bolt`, scale 2, slow 0.75× / 2 s |
| `lightning_chain.json` | 25 lightning, `lightning_bolt`, bounce ×2 / range 200 / falloff 5 |

Sword JSONs migrate to `{ "chance": 0.1, "effect": "<id>" }`. In-game
behavior must be identical before/after.

## Error handling

- Unknown effect ID on an item → startup panic via `validateItemDef` (catalog
  coherence discipline, matches existing loaders).
- Effect referencing an unknown `projectileID` → falls back to default
  projectile speed/no effects at execute time, same as today's behavior for
  unknown IDs (projectile-kind default).
- `executeProcEffectLocked` with nil target → no-op guard (mirrors existing
  nil guards).

## Testing

- **Migrated:** `beam_proc_test.go`, `burn_proc_test.go`, `proc_slow_test.go`,
  `equipment_onhit_proc_test.go`, `item_onhit_def_test.go`,
  `elemental_swords_test.go` — updated to the new structs; all assertions
  about in-game behavior unchanged.
- **Catalog wiring guards** (`TestFireSword_ProcIsWiredToBurn`,
  `TestLightningSword_ProcIsWiredToChain`) — updated to assert the sword
  references the right effect ID *and* the resolved params carry the expected
  payload.
- **New:** proc catalog loader (id/filename coherence, validation failures);
  override precedence via `resolveProcEffectParams` (override beats def, zero
  keeps def — covering damage, scale, bounce, slow, burn); non-unit
  source execution (OwnerUnitID = 0: projectile lands, damage applies, no XP
  awarded, no panic; beam chain excludes nobody extra).

## Out of scope

- No new *callers* (perks, abilities, traps) in this branch — it builds the
  seam so those become one-liners later.
- No client changes: the client already renders projectiles/beams from server
  state; nothing in the wire protocol changes.
- No changes to `flame_sword.json` (it has no proc).
