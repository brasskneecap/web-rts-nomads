## Why

The Arch Mage path exists structurally (path def, sprites, rank curve) but grants no abilities — its offensive-caster fantasy is unrealized and `arcane_bolt` sits dormant. Delivering the Arch Mage requires more than one spell: it needs a data-driven way to define spells, a random per-unit spell assignment at promotion, and a modifier layer so future perks can tune spells without forking code. Building these as reusable systems now (rather than hardcoding three spells) is what makes silver/gold Arch Mage content — and future casters — cheap to add later.

## What Changes

- **Spell registry (extend existing).** `AbilityDef` gains a `Tags []string` field. The existing `DamageType` doubles as the spell's "school" for modifier targeting (fire/shadow/lightning/arcane). The catalog loader stays flat, typed, and load-validated — no freeform `config` blob.
- **Spell modifier pipeline (new).** A generic, source-agnostic modifier system: a modifier targets a spell by `spellId` / `school` / `tag`, names a field via a **typed enum** (manaCost, cooldown, castTime, damage, radius, projectileSpeed, duration, chainCount, pullStrength, …), and applies an `add` (default) or `multiply` operation. Effective spell values are resolved **at cast time** by folding active modifiers over the immutable base def — the base def is never mutated. Fold order is fixed (all adds, then all multiplies, per field), which is order-independent and deterministic. This is the single plug-in point where future perks/buffs/items modify spells.
- **Arch Mage spell pools (new).** A data-driven pool catalog (`{ arch_mage: { bronze: [...ids], silver: [] } }`, each id validated against a registered `AbilityDef`). On promotion to a rank, each unit rolls **one** spell from that rank's pool (minus already-known spells) using the seeded progression RNG. The roll is **per-unit** — a squad of Arch Mages is heterogeneous. The roll happens once at rank-up and records its pick on a new persistent unit field; the existing idempotent, RNG-free ability recompute then reads the recorded pick. No-duplicate-known-spells is enforced by rolling from pool-minus-known.
- **Three bronze spells (new content).**
  - `fireball` — projectile + area splash, reusing `applySplashDamageLocked` and the existing ability-projectile launch path.
  - `chain_lightning` — bouncing chain, reusing the `lightning_chain` proc / beam-bounce mechanic (bounceCount / bounceRange / bounceDamageFalloff).
  - `arcane_orb` — pulls enemies toward a point via a **new forced-displacement CC subsystem** (see below).
- **Forced-displacement CC (new subsystem).** Affected enemy units (referenced by ID) receive a deterministic per-tick position delta toward a pull center over a duration. Pure math, seed-safe. `pullStrength` is a modifier-eligible field. This is the first displacement/knockback primitive in the codebase and is built to be reused.
- **Deferred (out of scope):** silver and gold Arch Mage pool content and perks. The silver pool ships empty; this change delivers the full bronze tier plus every system needed to add higher tiers as data later.

## Capabilities

### New Capabilities
- `spell-modifier-pipeline`: cast-time resolution of effective spell values from generic, source-agnostic modifiers (target by spellId/school/tag, typed field enum, add/multiply), without mutating base definitions.
- `arch-mage-spell-pools`: data-driven per-(archetype, rank) spell pools and deterministic per-unit random spell assignment recorded persistently at promotion.
- `forced-displacement`: deterministic pull/knockback control effect that moves enemy units (by ID) toward a point over a duration, interacting with the movement/pathing system.
- `arch-mage-spell-system`: the Arch Mage bronze spell content — `AbilityDef` tags plus the `fireball`, `chain_lightning`, and `arcane_orb` definitions wiring splash / chain / pull to ability resolution.

### Modified Capabilities
- `per-path-ability-kits`: `assignUnitPathAbilitiesLocked` gains an additional deterministic composition step — after path-level overrides and rank grants, it includes the unit's recorded pool-spell picks. The recompute remains idempotent and RNG-free (the RNG roll lives in the new spell-pools capability); this capability only reads the recorded result.

## Impact

- **Server (Go):** `ability_defs.go` (`Tags`), a new modifier subsystem + a new pool catalog/loader + roller, a new forced-displacement subsystem interacting with `state_movement.go`, new `AbilityDef`s and their projectile defs, and a new persistent `Unit` field for recorded pool picks. Wiring in `ability_cast.go` (read effective values), `progression.go` / `path_ability_defs.go` (roll + recompute), and reuse of `applySplashDamageLocked` and the proc/beam-bounce path.
- **Client (TS/Vue3):** surfaces the granted spell through the existing `AbilitySnapshot` path (no protocol change expected); rendering of the new projectiles/pull effect follows existing projectile/effect conventions. Client remains a pure view — no gameplay simulation added.
- **Determinism:** new RNG use routes through the existing seeded `rngPerks` stream; modifier folding and displacement math are order-independent / pure. No wall-clock, unseeded rand, or map-iteration-order-driven outcomes introduced.
- **Assets:** placeholder icons/art for the three spells and the pull effect (TODO markers, matching existing dormant-ability convention).
