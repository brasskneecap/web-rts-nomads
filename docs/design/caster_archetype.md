# Caster Archetype & CombatProfile — Design Spec

> **Status: DESIGN ONLY — not implemented.** Recorded at the user's request.
> No code exists for this yet. Implementation deferred pending user direction.
> Produced by the `game-architect` agent; companion to
> [apprentice_and_systems.md](./apprentice_and_systems.md).

> **Conventions** (per `.claude/rules/AI_RULES.md`)
> - Go server authoritative; client renders server state.
> - `*Locked` suffix ⇒ caller already holds `s.mu`.
> - Targets referenced **by ID**, re-resolved + revalidated every tick; never
>   persist a `*Unit` across ticks.
> - Deterministic under seed: no wall-clock, no `math/rand` outside seeded
>   streams, no outcome-driving map iteration.
> - No client test runner; client validated by `vue-tsc` typecheck only.

---

## Goal

A `caster` `archetype` + `combatProfile` for the **Apprentice** and its
promotion paths **Cleric** and **Arch Mage**. Casters cast spells (mana cost,
optional cooldown), keep a basic ranged attack between casts, and choose the
situationally-best ability each tick (e.g. prioritise heal when nearby allies
are low). Future ability kinds to accommodate (not build now): summon, ally
buffs (armor/AD/AS), offensive spells, anti-air.

## Key conclusions

1. **The hard infrastructure already exists** — ability cast lifecycle,
   autocast loop, target-selector registry, mana, per-unit cooldowns, and the
   action-bar UI. This is a compose-and-extend job, not a new subsystem.
2. **`caster` is a NEW combat profile**, not a reuse of `archer`. The Apprentice
   today never kites (`archer` profile has no retreat configured → it stands and
   dies to melee). `caster` clones the `support` profile (backline + retreat),
   leaving `archer` untouched.
3. **Abilities are a PARALLEL system to perks, not "guaranteed perks."** Perks
   are randomly drawn from a pool via `s.rngPerks`; abilities are deterministic
   grants. Routing abilities through perk RNG would harm replay/seed
   determinism. They share the catalog *layout* and the rank-up *seam* only.
4. **Ability selection extends the existing autocast loop**: change
   `tickUnitAutoCastLocked` from "first ready ability wins" to "highest-scored
   ready ability wins," with category-driven scoring and deterministic
   tiebreaks (ability slot index, then id).
5. **Per-path kits** declared in new `paths/<path>/abilities/<rank>.json` files
   (mirroring the perk layout), granted on promotion. Phase 1 keeps Apprentice's
   `["heal"]` → zero migration, zero behaviour regression.

## Resolved decisions (locked by user)

- **Manual cast vs autocast:** player cast just *preempts*; autocast priority
  resumes when the unit is idle again. No suppression window, no new sticky
  state. (Out of scope for Phase 1/2 regardless.)
- **Offensive ability targeting / anti-air:** each ability **declares its own
  target-class filter** in its selector (an anti-air spell explicitly includes
  flyers), independent of the caster unit's normal `TargetableTypes`. Anti-air
  is therefore a *selector* concern, **not** an `AbilityCategory`.

## Data model

- **`UnitDef.Abilities []string`** stays the base/always-on kit. Apprentice
  keeps `["heal"]` (no `heal.json` migration for Phase 1).
- **Per-path grants** in new files, ordered id lists (AbilityDef remains the
  single source of truth for ability data):
  ```
  catalog/units/human/apprentice/paths/cleric/abilities/{bronze,silver,gold}.json
  catalog/units/human/apprentice/paths/arch_mage/abilities/{bronze,silver,gold}.json
  ```
  Shape: `{ "grant": ["greater_heal"] }`. Loaded by a new `path_ability_defs.go`
  (twin of `path_defs.go`) → `pathAbilityGrantsByKey[(path,rank)] []string`.
- **`AbilityDef.Category`** new extensible string enum
  (`heal | buff_ally | summon | offensive`, default `""`), mirroring the
  existing `AbilityType`/`DamageType` const-block pattern. `heal.json` gets one
  additive line `"category":"heal"` (no behaviour change). `manaCost`,
  `cooldown`, `castRange`, target flags already exist on `AbilityDef`.
- **Promotion wiring:** `assignUnitPathAbilitiesLocked(unit)` called from
  `addUnitXPLocked` immediately after `assignUnitPerkLocked` — idempotent,
  ordered append, no RNG (the only RNG is the existing path *choice* in
  `progression.go`).

## Ability-selection system

New file `ability_priority.go`. Insert a scoring step into
`tickUnitAutoCastLocked` between "ability is ready (off cooldown, enough mana,
autocast on)" and "begin cast":

1. Candidate list = ordered `unit.Abilities`, applying the existing gates
   (autocast enabled, `SupportsAutoCast`, off cooldown, enough mana) — unchanged,
   so mana/cooldown gating is preserved.
2. Resolve each candidate's target via the existing
   `resolveAutoCastTargetLocked` (keeps ID-based resolution/validation where it
   already lives). Skip if no target.
3. Score `(ability, target)` by category:
   - `heal`: `w_heal * clamp01((healThresholdPct − targetHPpct)/healThresholdPct)`
     + bonus for nearby damaged allies. (Generalises the "<50%" example.)
   - `offensive`: target value / cluster / finishing potential.
   - `buff_ally`: high if target lacks buff and is in combat; ~0 otherwise.
   - `summon`: from local force deficit; target is self.
   - Weights live in a small Go table keyed by category (JSON-tunable later;
     keep out of `CombatProfile` to avoid bloat).
4. Highest score wins; deterministic tiebreak by slot index then id. Below
   `minActivationScore` → cast nothing (basic attack proceeds via the unchanged
   combat AI / `Casting` gate). One cast/unit/tick preserved.

**Determinism/ID compliance:** reads only live state + ordered slices; never
persists a `*Unit`; hands the within-tick `*Unit` to `beginAbilityCastLocked`
which stores only `unit.CastTargetID int`. **No-regression invariant:** with a
single autocast ability (Phase 1 heal-only), highest-scored-ready collapses to
exactly today's behaviour.

## Module boundaries

| File | Ownership | Change |
|---|---|---|
| `combat_ai_profiles.go` | add `"caster"` to `combatProfiles` (clone `support`) | additive |
| `combat_ai_scoring.go` | add `"caster"` to `unitStrategicValue` support branch + `unitTypePreference` cases & `support`/`mage` target checks | additive, behaviour-affecting → tested |
| `catalog/.../apprentice/apprentice.json` | `archetype`+`combatProfile` → `"caster"` | changed |
| `ability_defs.go` | `AbilityCategory` enum + `Category` field | additive |
| `catalog/abilities/heal/heal.json` | `"category":"heal"` | additive |
| `path_ability_defs.go` (new) | load `paths/<p>/abilities/<rank>.json` | new |
| `progression.go` | `assignUnitPathAbilitiesLocked` after `assignUnitPerkLocked` | additive |
| `ability_priority.go` (new) | category scoring | new |
| `ability_autocast.go` | first-match → highest-score | core change |
| `catalog/.../paths/{cleric,arch_mage}/abilities/*.json` (new) | per-path kits | new (Phase 2) |

All new fns are `*Locked`, called from the existing tick
([state.go](../../server/internal/game/state.go) autocast wiring) or rank-up.
No new lock acquisition, no new persisted pointer fields.

## Phased plan

**Phase 1 — `caster` profile, zero behaviour regression.**
- backend: add `caster` profile (clone `support`); add `caster` to strategic
  value + type-preference; flip `apprentice.json`; add `AbilityCategory` +
  `Category`; `"category":"heal"`. No priority scoring yet, no per-path files.
- frontend: none (snapshot shape unchanged).
- qa: heal autocast byte-identical pre/post (same seed → same cast ticks);
  Apprentice now kites melee instead of standing; full server suite as the
  no-regression gate.

**Phase 2 — priority selection + per-path kits.**
- backend: `path_ability_defs.go`; `assignUnitPathAbilitiesLocked`;
  `ability_priority.go`; rework `tickUnitAutoCastLocked`; author Cleric +
  Arch Mage kits (≥1 cleric heal-line, ≥1 arch_mage offensive using the
  existing `selectClosestEnemyInRange` stub).
- frontend: no protocol change (`AbilitySnapshot` already sufficient); verify
  action bar shows post-promotion abilities, per-ability autocast toggle,
  cooldown overlay.
- qa: seeded-replay determinism; priority correctness (heal vs offensive by
  ally HP); mana/cooldown gating; deterministic per-path grants on promotion.

## Remaining open items (tuning, not architecture)

- `caster` profile numbers (retreat distances, weights) — placeholder = clone
  `support`; needs a balance pass.
- Global vs per-ability cooldown — keep current per-ability-per-unit
  (`Unit.AbilityCooldowns`); revisit only if a shared global cast cooldown is
  ever desired (not recommended for Phase 1/2).

## Anchor references (verify before implementing — may drift)

`resolveCombatProfile` combat_ai_profiles.go ~L444; `combatProfiles` map
~L33-61; `unitStrategicValue` combat_ai_scoring.go ~L301; `unitTypePreference`
~L338; `AbilityDef` ability_defs.go ~L92; cast lifecycle ability_cast.go;
autocast `tickUnitAutoCastLocked` ability_autocast.go ~L74; selectors
autocast_selectors.go; perk assignment perks.go ~L610 / progression.go ~L256;
mana `spendUnitManaLocked` mana.go ~L67; spawn state_spawn.go ~L49/L81; tick
wiring state.go ~L1864-1888; `AbilitySnapshot` protocol/messages.go ~L205;
WS cast/autocast ws/handlers.go ~L371-407; action bar SelectionHud.vue ~L328.
