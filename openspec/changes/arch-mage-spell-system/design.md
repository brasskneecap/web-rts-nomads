## Context

The server is authoritative and deterministic: the tick loop runs under `s.mu` (`*Locked` methods assume the lock), targets are referenced by ID and re-resolved/validated every tick, and no simulation outcome may depend on wall-clock time, unseeded `math/rand`, or map-iteration order. Catalog definitions are embedded JSON, loaded once at startup and validated with panics on bad data.

Much of what the Arch Mage needs already exists and MUST be reused rather than reimplemented:

- **Spell registry** = the `AbilityDef` catalog (`catalog/abilities/<id>/<id>.json` + `loadAbilityDefs`). It is flat, typed, and load-validated. `arcane_bolt` already launches an ability projectile.
- **AoE damage** = `applySplashDamageLocked(attacker, primaryTarget, damage, deadUnitIDs)` — hits every hostile within a radius via the authoritative damage pipeline, bypassing on-attack perk hooks so it can't recurse.
- **Bounce/chain** = the `lightning_chain` proc (`bounceCount` / `bounceRange` / `bounceDamageFalloff`) delivered through the beam-bounce path.
- **Per-rank ability grants** = `assignUnitPathAbilitiesLocked` (idempotent, RNG-free recompute) + the `path_ability_defs.go` loader, governed by the `per-path-ability-kits` spec.
- **Seeded progression RNG** = `s.rngPerks`; the path choice at rank-up uses `rollProgressionPathLocked` (weighted, keys sorted before the roll).

What does NOT exist: a generic spell-modifier layer, data-driven spell pools with random assignment, and any forced-displacement/knockback mechanic.

## Goals / Non-Goals

**Goals:**
- Deliver the full Arch Mage bronze tier: three castable spells assigned randomly per unit at promotion.
- Build reusable systems (modifier pipeline, pool loader/roller, forced-displacement CC) so silver/gold content is data-only later.
- Preserve every existing invariant: determinism, ID-based targeting, idempotent RNG-free recompute, single-lock discipline, load-time validation.
- Never mutate base spell definitions; resolve effective values at cast time.

**Non-Goals:**
- Silver/gold Arch Mage pool content and perks (deferred; silver/gold pools ship empty).
- A player-facing spell-picker UI (assignment is server-side RNG; the granted spell surfaces through the existing ability snapshot).
- Reworking `arcane_bolt` (it stays as-is; it is not in the bronze pool).
- A general status-effect/buff framework — the modifier collector has perk/buff/item hooks but this change only needs to prove the pipeline, not populate every source.

## Decisions

### D1 — Spell registry: extend `AbilityDef` with `Tags`, reuse `DamageType` as school

Add `Tags []string` (JSON `tags`, default empty). Use the existing `DamageType` as the modifier-targeting "school" key rather than adding a parallel `School` field — fire/shadow/lightning/arcane already live there, and a second field would be a second source of truth. Keep the def flat and typed; reject the spec's freeform `config` blob because it would discard the load-time validation that catches authoring errors at startup.

*Alternative considered:* a separate `School` taxonomy distinct from damage element. Rejected for now — no spell needs a school that differs from its element. If that need arises, add the field then.

### D2 — Modifier pipeline: typed field enum, fold-add-then-multiply, source-agnostic collector

`SpellModifier{ Target{SpellID, School, Tag string}, Field SpellModField (typed enum), Operation (add|multiply, default add), Value float64 }`. Resolution:

```
base AbilityDef (immutable)
      │
collectSpellModifiersLocked(caster, def) []SpellModifier   ← perks + buffs + items
      │   (each source implements the same "modifiers for (caster, spell)" contract)
      ▼
resolveEffectiveSpellLocked → EffectiveSpell{ ManaCost, Cooldown, CastTime, Damage,
                                              Radius, ProjectileSpeed, Duration,
                                              ChainCount, PullStrength, ... }
      │   per field: apply ALL adds, then ALL multiplies
      ▼
cast path reads EffectiveSpell, never the raw def
```

- **Typed `SpellModField` enum, not string paths.** Load-time-validatable, no reflection, matches the codebase's extensible-enum idiom (`DamageType`, `AbilityCategory`). String paths like `"config.explosionRadius"` are rejected.
- **Fold order = adds-then-multiplies, per field.** Add-within-group and multiply-within-group are each commutative, so the result is independent of collection order — no need to sort modifiers, and determinism holds regardless of how perks/buffs/items enumerate. This is *the* reason the ordering is pinned this way.
- **Operation defaults to `add`, switchable per modifier** (the requested balance knob) — a JSON `"operation": "multiply"` flips a single modifier.
- **Collector is the documented plug-in point.** Each source type exposes `spellModifiersFor(caster, def) []SpellModifier`; the collector concatenates them. This change wires the hook and proves it with a test-only or perk-backed modifier; it does not need to ship a full buff framework.
- **Matching semantics:** a modifier applies when every specified target field matches (unspecified = wildcard); an empty target is a load error.

*Alternative considered:* mutate a per-cast copy of the def. Rejected — an explicit `EffectiveSpell` value type makes "what the cast actually reads" obvious and keeps the base def provably untouched.

### D3 — Spell pools: separate catalog, roll-once-record, recompute-reads

Pools live in their own catalog (`catalog/spell-pools/*.json` or a single `spell-pools.json`), shaped `{ archetype: { rank: [ids] } }`, validated at load (every id is a registered `AbilityDef`; ranks ∈ bronze/silver/gold). This keeps pools "separate from spell definitions" per the spec and lets a new spell join a pool with a one-line edit.

The critical constraint: `assignUnitPathAbilitiesLocked` is deliberately **idempotent and RNG-free**. An RNG roll cannot live inside it. So split the concern exactly like path choice does:

```
ONE-TIME ROLL (at rank-up, RNG)              IDEMPOTENT RECOMPUTE (every promotion)
────────────────────────────────            ─────────────────────────────────────
rollPoolSpellLocked(unit, rank):            assignUnitPathAbilitiesLocked:
  pool = poolFor(archetype, rank)             …path override, rank grants…
  candidates = pool − unit.known              for each reached rank R:
  sort(candidates)                              append unit.PoolSpellsByRank[R]   ← reads
  pick = candidates[ rngPerks-weighted ]          (no RNG, replayable)
  unit.PoolSpellsByRank[rank] = pick   ←record
```

- **New persistent field** `PoolSpellsByRank map[Rank]string` on `Unit` (nil-safe; absent = no pick). This is the recorded pick, analogous to `unit.ProgressionPath`. It survives ticks (per project convention it stores an **id string**, not a pointer).
- **Roll site:** immediately after `assignUnitPathOnRankUpLocked` records the path, and before `assignUnitPathAbilitiesLocked` recomputes — one roll per crossed rank. Reuse/generalize `rollProgressionPathLocked` (sort candidates, draw from `rngPerks`) so uniform-vs-weighted and determinism come for free.
- **No-dupes:** candidates = pool minus `unit.Abilities` (and minus prior recorded picks), so silver later never re-picks a known spell. Exhausted pool → record nothing.
- **`archetype` key:** the pool is keyed by the path/archetype id (`arch_mage`). Map to it from the unit's `ProgressionPath`.

### D4 — Bronze spells: two glue, one new mechanic

- **`fireball`** — an `AbilityDef` with a projectile and a `radius`; on resolve, launch the ability projectile (existing path) and on impact call `applySplashDamageLocked` with the effective damage/radius. The splash helper already routes through the authoritative pipeline (mitigation, death, threat). Glue only.
- **`chain_lightning`** — on resolve, fire the existing bounce mechanic (a `lightning_chain`-style proc / beam bounce) seeded from the target. `chainCount` maps to `bounceCount`. Deterministic bounce target selection (sorted candidate scan). Glue only.
- **`arcane_orb`** — on resolve, apply the forced-displacement effect (D5) to hostiles within `radius` of the orb center, pulling toward center for `duration` at `pullStrength`. New mechanic.

All three declare `DamageType` (school) and `Tags` so D2 modifiers target them.

### D5 — Forced-displacement CC subsystem

A new tick-updated effect list (mirroring how other timed effects like burn/slow are tracked): each active pull holds the center point, `pullStrength`, remaining `duration`, and the set of affected **unit IDs**. Each tick, for each active pull: re-resolve each affected unit by ID, validate (alive, hostile-to-caster), and apply a per-tick delta toward center:

```
dir   = normalize(center − unit.pos)
step  = pullStrength * dt          (deterministic; dt is the fixed tick delta)
unit.pos += dir * step   (not overshooting past center)
```

- **Displacement wins the tick over normal path advancement** for a displaced unit: gate the unit's normal move step while a pull is active on it, and on expiry let the AI re-plan from the current position (no stale pre-pull path snapping it back). This is the fiddly integration point with `state_movement.go`.
- **Enemies only** relative to the pull's caster (owner recorded on the effect; ally/caster skipped).
- **`pullStrength` is a `SpellModField`** so D2 perks can scale it.
- **Collision behavior — DECISION: clip-through, clamp only the final rest position.** During the pull, allow units to overlap and pass over obstacles (simplest, fully deterministic, avoids mid-pull pathfinding on every tick). When the effect expires, if a unit rests inside an obstacle, the existing stuck/repath recovery handles reintegration. This trades visual purity for determinism and simplicity; revisit if units routinely end up wedged.

### D6 — Client

The granted spell reaches the client through the existing `unit.Abilities` → `AbilitySnapshot` path — no protocol change. New projectiles/effects render via existing projectile/effect catalog conventions; the pull is visible purely as server-driven position changes (client renders where the server says units are). No client-side gameplay logic.

## Risks / Trade-offs

- **Forced-displacement vs. pathing fights** → Gate normal movement for displaced units and clear stale paths on expiry; cover with a test that a pulled-then-released unit resumes cleanly (extends the existing stuck/repath recovery tests).
- **Clip-through looks wrong through walls** → Accepted trade-off for determinism/simplicity (D5); mitigated by short pull durations and existing repath recovery. Flagged as revisitable, not blocking.
- **Modifier fold order ambiguity** → Eliminated by construction: adds-then-multiplies per field is order-independent (D2). A test asserts identical results across shuffled modifier collection order.
- **RNG breaking recompute idempotency** → The roll is isolated at rank-up and records to `PoolSpellsByRank`; the recompute only reads it. A test asserts repeated recompute draws zero RNG and yields identical abilities.
- **Splash/bounce reuse drift** → By calling `applySplashDamageLocked` and the existing bounce path (not new damage loops), fireball/chain_lightning inherit mitigation/death/threat/determinism for free; a hand-rolled loop would risk a parallel death path (explicitly disallowed).
- **Per-unit heterogeneous rolls surprise players** → Intended (confirmed): each Arch Mage may differ. No mitigation needed; documented so it isn't "fixed" later as a bug.

## Open Questions

- **Pool weighting:** bronze pool is a plain array (uniform pick). The generalized roller supports weights nearly for free — ship uniform, add optional per-id weights only if balance later needs it. (Non-blocking.)
- **`arcane_orb` delivery:** does the pull originate from the caster's target point instantly (hitscan center) or from a traveling projectile that plants the center on impact? Leaning instant-center for the first pass; a projectile-planted center is a later polish. (Resolve during task implementation.)
- **Pull cast targeting:** `arcane_orb` as a point/area cast vs. unit-targeted (center = target's position). Leaning unit-targeted to reuse the existing single-target cast path; revisit if a ground-target cast surface is warranted.
