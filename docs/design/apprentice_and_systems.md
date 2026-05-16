# Apprentice & Spellcaster Systems

Reference for the systems introduced to support the **Apprentice**, the
faction's first spellcaster. These systems are deliberately generic — the
Apprentice and its **Heal** spell are simply the first consumers. Use this
doc when adding future spellcaster units or abilities.

> **Conventions used throughout** (per `.claude/rules/AI_RULES.md`)
> - The Go server is authoritative; the client renders server state.
> - `*Locked` suffix ⇒ the caller already holds `s.mu`.
> - Targets are referenced **by ID** and re-resolved + revalidated every
>   tick; never persist a `*Unit` across ticks.
> - Simulation is deterministic under a seed: no wall-clock, no
>   `math/rand` outside the seeded streams, no map iteration that drives
>   outcomes.
> - There is **no client test runner**; client code is validated by
>   `vue-tsc` typecheck only. Every system below is tested server-side in Go.

---

## 1. Projectile asset system

**Files:** `server/internal/game/projectile_defs.go`,
`server/internal/game/projectile.go`,
`server/internal/game/catalog/projectiles/<id>/<id>.json`;
client `client/src/game-portal/src/assets/projectiles/<id>/`,
`game/rendering/projectileSpriteSheets.ts`, `game/rendering/projectileSprites.ts`.

A **`ProjectileDef`** is an authoritative, server-owned definition loaded from
an embedded per-id catalog (same drift-panic discipline as `unit_defs.go`):

| Field | Meaning |
|---|---|
| `ID` | stable id; must match the directory name |
| `Speed` | world px/s; `<= 0` defaults to `defaultProjectileSpeed` (the codebase standard, 500) |
| `FollowEffect` | optional effect id that plays **on the projectile while it flies** (fail-safe resolved) |
| `ImpactEffect` | optional effect id played **on the target unit when the projectile lands** |

`getProjectileDef(id)` / `ListProjectileDefs()` mirror `getUnitDef`.

**Behavior (fixed, not data):**
- Hit/miss is decided by the **target's** evasion via `evasionForUnit(u)`
  (the seam — returns zero today, so projectiles always hit). The roll, when
  evasion exists, uses the dedicated `rngCombat` stream so it never perturbs
  perk/loot determinism.
- `applyProjectileDamageLocked` damages **only the resolved target** — no
  AoE / pierce / pass-through. (Pierce remains the separate Marksman path.)
- A unit fires a configured projectile by setting `Unit.ProjectileID` (from
  `UnitDef.projectile`). `fireHomingProjectileLocked` then uses the def's
  speed, sets the wire `Variant` to the projectile id, and carries
  `FollowEffect` / `ImpactEffect` / `DamageType`. Unset ⇒ the unchanged
  legacy procedural shot.

**Client rendering:** the server `variant` string keys
`PROJECTILE_DRAW_REGISTRY`. Projectiles ship 8 baked rotation PNGs +
`sprites.json`; `projectileSpriteSheets.ts` loads them and every art-backed
projectile is auto-registered with a sprite draw fn. The renderer already
rotates the canvas to the flight heading, so the single forward frame is
drawn and oriented "for free" (all 8 rotations are still loaded so a baked
8-way draw is a one-line switch). Unregistered variants fall back to a
procedural arrow.

**To add a projectile:** create
`catalog/projectiles/<id>/<id>.json`; optionally drop client art at
`assets/projectiles/<id>/` with a `sprites.json`. Reference it from a unit
via `UnitDef.projectile`.

---

## 2. Effect asset system

**Files:** `server/internal/game/effect_defs.go`,
`catalog/effects/<id>/<id>.json`,
`server/internal/game/state_effects.go` (the shared transient pipeline);
client `assets/effects/<id>/`, `game/rendering/effectSprites.ts`.

An **`EffectDef`** is the authoritative definition of a cosmetic overlay
effect: `ID`, `SpritePath` (optional/logical), `Duration` (seconds; `0` =
indefinite-while-bound — not coerced), `Anchor` (`center` | `feet` | `head`,
empty ⇒ center). Effects never affect gameplay.

**Key design decision (reconciliation):** there was already a rendered
transient-effect pipeline (`queueEffectLocked → effectInstance →
EffectSnapshot → effectSprites.ts`) used by perks. The effect *asset type*
(`EffectDef`) layers on top of it — `playEffectOnUnitLocked(unit, effectID)`
resolves the def and **delegates to `queueEffectLocked`** so effects actually
render via the same path perks use. There is intentionally **no parallel
`ActiveEffect` system**.

Effects are played:
- on a unit — `playEffectOnUnitLocked` (heal target, etc.);
- as a projectile **impact** — `ProjectileDef.ImpactEffect`, played on the
  unit a projectile reaches (`playProjectileImpactLocked` from
  `landProjectileLocked`);
- carried by a projectile in flight — `ProjectileDef.FollowEffect`.

**`EffectAnchor` rendering:** `Anchor` rides on `EffectSnapshot`. The client
shifts only for `feet`/`head` using the unit's bounds; empty/`center` keeps
the historical origin placement, so existing perk effects are pixel-unchanged.

**To add an effect:** create `catalog/effects/<id>/<id>.json` and client art
at `assets/effects/<id>/{sprites.json,sheet.png}` (folder name = effect id =
wire `Name`). Reference by id from an ability (`effectOnTarget`) or projectile
(`impactEffect`/`followEffect`).

**First users:** `fire_bolt` → `fizzle` (impact); Heal → `healing_glow`
(on-target).

---

## 3. Damage type system

**Files:** `server/internal/game/damage_type.go`,
`damage_pipeline.go` (`DamageSource`).

`DamageType` is an extensible string enum — `physical` (default), `fire`,
`frost`, `lightning`, `arcane`, `holy` — with a registry
(`RegisterDamageType` / `IsValidDamageType` / `DamageTypes`). It is carried on
`DamageSource` (`ResolvedDamageType()` maps the empty zero value →
`physical`), so the ~33 existing `DamageSource{}` call sites needed **no
changes**.

**Scope:** today this is **flavor/metadata only** — it does not change any
damage numbers. The damage type is determined by the attacker/ability, not
the projectile (a fire-themed shot does fire damage; the bolt sprite is just
visual). It rides on `Projectile.DamageType` (from `Unit.AttackDamageType`)
as the seam future resistance/weakness logic will read.

> **Tracked TODO:** resistances/weaknesses are deferred. When added,
> mitigation should branch on `DamageSource.ResolvedDamageType()`. It was
> deliberately **not** threaded through the shared `resolveAttackHitLocked`
> yet (no consumer; premature).

---

## 4. Mana resource system

**Files:** `server/internal/game/mana.go`; `Unit` fields in `state.go`.

Optional per-unit resource: `MaxMana`, `CurrentMana` (int, like HP),
`ManaRegenPerSecond` (float), `ManaRegenAccumulator` (sub-1/s cadence —
mirrors the HP-regen accumulator). All default `0` ⇒ a non-caster unit; the
regen tick is skipped entirely when `MaxMana == 0`.

- `tickUnitManaRegenLocked` — passive regen, capped at `MaxMana`, accumulator
  reset at full (no instant re-bank after a spend). Wired in the Update loop
  next to HP regen.
- `spendUnitManaLocked(unit, cost) bool` — the single spend entry point.
  `cost <= 0` succeeds free; a costed spend on a no-mana unit fails; mirrors
  `healUnitLocked`'s guard style.

Loaded from `UnitDef.maxMana` / `UnitDef.manaRegenRate`; `CurrentMana` starts
at `MaxMana` at spawn.

---

## 5. Ability framework

There was no prior ability system (perks are passive). This is a **new
framework** that reuses existing conventions (catalog layout, lock
discipline, ID-based targeting, the transient-effect pipeline) rather than
the perk system.

### 5.1 Definition — `AbilityDef`

**Files:** `server/internal/game/ability_defs.go`,
`catalog/abilities/<id>/<id>.json`.

`id`, `displayName`, `type` (`AbilityType`; `spell` drives the Casting
animation), targeting flags (`canTargetSelf/Allies/Enemies`), `castRange`
(`CastRange` — number **or** the string `"match_attack_range"` / `-1`
sentinel; `Resolve(caster)` → caster's `AttackRange` for the match case),
`castTime`, `manaCost`, `cooldown`, `damageType`, `supportsAutoCast`,
`autoCastTargetSelector`, `icon`, `casterAnimation`
(`CasterAnimationOrCasting()` defaults to `"Casting"`), `effectOnTarget`,
and `healAmount` (effect magnitude).

Validation methods: `CanTargetUnit(caster, target)` (self/ally/enemy by
ownership + alive guard) and `WithinCastRange(caster, target)` (self always
in range; resolves `match_attack_range`).

### 5.2 Cast lifecycle (Part 8)

**File:** `server/internal/game/ability_cast.go`. Cast state on `Unit`
(`CastAbilityID/TargetID/TimeRemaining`, mirrors `AttackWindup*`).

- `beginAbilityCastLocked(caster, abilityID, target) (ok, reason)` —
  validates ownership of the ability → not already casting → target legal →
  in range → enough mana. Returns `(false, reason)` with a stable
  `castFail*` string. `castTime <= 0` resolves instantly; otherwise it locks
  the caster via `beginUnitCastingLocked` (animation slot + can't
  move/attack).
- `tickUnitCastLocked` (Update loop) — re-resolves the target **by ID** each
  tick; cancels cleanly (no mana, no effect, records `LastCastFailure`) if
  the target dies / leaves range; otherwise counts down and on completion
  spends mana and resolves the effect.
- **Heal resolution:** restores `healAmount`, **clamped to MaxHP — no
  overheal** (deliberately *not* via `healUnitLocked`, which converts
  overheal to a perk shield); then plays `effectOnTarget` (`healing_glow`).

> **Design decision:** casts are **uninterruptible** — damage/CC do NOT
> cancel a cast (only caster/target death or target leaving range does).
> Mana is deducted on completion, so a cancelled cast costs nothing. A clear
> seam exists (`cancelUnitCastLocked` call sites) to add interrupts later.

**Feedback:** synchronous failures return `(false, reason)`; the WS handler
sends `protocol.NotificationMessage` (same pattern as the "Not enough
resources" path). Async mid-cast cancels record `Unit.LastCastFailure`.

### 5.3 Animation

Animation slots are **client-derived from `unit.Status`**
(`unitAnimation.ts pickAnimation`); there is no server animation enum.
`"Casting"` was added (slot `casting`), distinct from `"Attacking"`, with a
precedence guard in `tickUnitCombatLocked` so the per-tick combat writer
can't clobber an active cast. `beginUnitCastingLocked` / `endUnitCastingLocked`
are the primitives. (No `Death` slot exists — units are removed on death.)

### 5.4 Auto-cast targeting selectors (Part 9)

**File:** `server/internal/game/autocast_selectors.go`. An extensible
registry (`RegisterAutoCastSelector` / `getAutoCastSelector`), resolved via
`resolveAutoCastTargetLocked` from `AbilityDef.AutoCastTargetSelector`.

- `lowest_hp_percentage_ally_in_range` — friendly **and**
  ability-targetable **and** in cast range **and** below 100% HP; min HP% via
  **integer cross-multiplication** (exact/deterministic); ties → **closest
  distance** → **lowest unit ID** (final deterministic tiebreak).
- `closest_enemy_in_range`, `self` — registered functional placeholders for
  future offensive / self-buff auto-cast (`TODO`: no ability uses them yet).

Selectors iterate the ordered `s.Units` slice with fully-ordered
tiebreakers — never map iteration — so picks are deterministic.

### 5.5 Auto-cast framework (Part 10)

**File:** `server/internal/game/ability_autocast.go`. Per-unit-instance
state (`Unit.AutoCastEnabled` / `AbilityCooldowns` maps — GC'd with the unit,
so auto-cleared on death).

- `toggleAutoCastLocked` — no-op for an ability the unit lacks / unknown /
  `SupportsAutoCast == false` ("right-click a non-auto-cast ability has no
  effect").
- `tickUnitAutoCastLocked` — iterates the **ordered `Unit.Abilities` slice**
  (deterministic); for each auto-cast-enabled ability that is off cooldown,
  affordable, and whose selector returns a target, it initiates one cast per
  unit per tick and **never while a cast is in progress** (so `castTime` is
  respected — no stacking). Cooldown is armed on initiation and decayed each
  tick.
- Player entry points (lock-acquiring, ownership-checked, mirror
  `AttackWithUnits`): `RequestAbilityCast` (left-click standard cast) and
  `ToggleAutoCast` (right-click toggle). WS commands:
  `cast_ability_command`, `toggle_autocast_command`.

**Client action bar:** `UnitSnapshot.abilities` (`AbilitySnapshot`) carries
each ability + live auto-cast/cooldown. The client builds interactive ability
buttons in the action grid (`getAbilityActionItems`); left-click enters
`cast-ability` targeting then sends the cast on the next friendly/self click;
right-click emits an `autocast-toggle-` action id (reusing the single action
dispatch channel — no new emit) → toggle command. Auto-cast-enabled cells get
the `action-cell--autocast` glow (placeholder CSS — dedicated glow asset is a
spec `TODO`).

---

## 6. The Apprentice & Heal — first consumers

**Apprentice** (`catalog/units/human/apprentice/apprentice.json`): base stats
copied verbatim from `archer` (placeholder — `metadata.todo` flags
post-playtest tuning). Key wiring:

- `combatProfile: "archer"` — **load-bearing**: `inferCombatArchetype`
  switches on `UnitType`, so without this the `"apprentice"` type would fall
  through to the melee `soldier` profile. This makes it ranged.
- Mana: `maxMana 50`, `manaRegenRate 1.0` (placeholders), `CurrentMana`
  starts full.
- Basic attack: `projectile "fire_bolt"`, `damageType "fire"` → fires the
  `fire_bolt` `ProjectileDef` (Fire-tagged, `fizzle` on impact) via the
  unchanged shared ranged path.
- `abilities: ["heal"]`.

**Heal** (`catalog/abilities/heal/heal.json`): `type spell`, `manaCost 5`,
`healAmount 5`, `castRange match_attack_range`, `castTime 1.0s`,
`cooldown 0`, `damageType holy` (metadata only — heals deal no damage;
`TODO`: revisit if heals should be untyped), self+allies (not enemies),
`casterAnimation "Casting"`, `effectOnTarget "healing_glow"`,
`supportsAutoCast true`, `autoCastTargetSelector
"lowest_hp_percentage_ally_in_range"`.

This is the reference implementation: a new spellcaster = a unit def with a
mana kit + `abilities`, plus an `AbilityDef` per spell wired to a damage
type, an on-target effect, and (optionally) an auto-cast selector.

---

## 7. Team / alliance system

Alliance is **data**: `Player.TeamID int` (`server/internal/game/state.go`).
Same `TeamID` ⇒ allies; different ⇒ hostile. **Everyone defaults to team 0**,
so with no per-match assignment the behavior is bit-for-bit the pre-team game
(all real players allied; only the PvE `__enemy__` AI hostile). PvP/FFA is
purely a data change — assign different `TeamID`s at match setup; **no call
site changes**.

### 7.1 Chokepoint predicates

All in `state_combat.go`; every hostility/friendship decision routes through
these — never raw `OwnerID` comparisons:

- `playerTeamLocked(ownerID)` — `TeamID`, or `0` for an absent player.
- `playersAreHostileLocked(a, b)` — same owner ⇒ false; `__enemy__` involved
  ⇒ true; else `team(a) != team(b)`.
- `playersAreFriendlyLocked(a, b)` — same team (self included); **never** for
  `__enemy__` (not even enemy-vs-enemy). NOT the negation of hostile.
- `unitsHostileLocked` / `unitsFriendlyLocked` — unit-level forms for
  within-tick `*Unit` working values; encode only the alliance relation
  (callers keep their own nil/HP/Visible guards).

At team 0 these collapse exactly onto the old `playersAreHostile` logic —
proven by `TestTeamPredicates_DefaultEquivalence`, and the whole pre-existing
suite stays green (the P0 equivalence proof).

### 7.2 Subsystems that respect alliance

1. **Combat auto-targeting** — acquisition gated by `playersAreHostileLocked`
   (units never auto-attack same team); ally/backline/threat scoring in
   `combat_ai_scoring.go` / `combat_ai_threat.go` use the friendly predicate.
2. **Ability / heal targeting** — `canAbilityTargetUnitLocked` classifies
   self / ally (`unitsFriendlyLocked`, team-based) / enemy; the auto-cast
   `lowest_hp_percentage_ally_in_range` / `closest_enemy_in_range` selectors
   route through the unit predicates.
3. **No friendly-fire on allies** — splash / cleave / whirlwind / explosive /
   pierce / traps already funnel through `playersAreHostileLocked`, so they
   became team-aware for free; the lone explicit edit is the attacker-dead
   pierce fallback in `projectile.go`.
4. **Shared vision / FOW** — `fowOwnerSharesVisionLocked` (`fow_recompute.go`):
   a unit/building grants a viewer vision iff its owner is a real
   FOW-having player **and** allied. Excludes `__enemy__` and neutral/unowned
   (no FOW entry). Per-player FOW + commutative union ⇒ determinism
   preserved; protocol unchanged.
5. **Team victory/defeat** — `checkPlayerLossLocked` aggregates townhalls
   **per team**: a team is eliminated only when *every* member's townhalls
   are gone, then all its players are marked lost together (so
   `GameOverSnapshot.LostPlayerIDs` / `IsGameOver` wire contracts are
   unchanged). Victory stays **global/shared** (map `VictoryConditions`);
   per-team victory *objectives* are out of scope.

Out of scope (unchanged): per-player economy/resources, production, items,
upgrades — these stay raw-`OwnerID` ownership checks (not alliance).

### 7.3 Client mirror

`PlayerSnapshot.teamId` is on the wire (`messages.go`). The client
(`GameState.ts`) keeps an `ownerId → teamId` map and re-derives
`isHostileToLocalPlayer` / `ownersAreHostile` / `isAlliedOtherPlayerUnit`
from it via `teamOf()` — a faithful mirror of the server chokepoint (server
stays authoritative; client predicates only drive cursor/health-bar/
inspection). No new client behavior at the default team.

### 7.4 Enabling PvP later

Set `Player.TeamID` per match — the natural seam is `EnsurePlayer`
(`state.go`) reading a match-config team assignment (described in the
architect spec; no lobby UI built). `TestTeam_PvP_AllSubsystemsFollowTeamData`
proves flipping that one int alone flips all five subsystems allied↔hostile.

### 7.5 Defaults & open boundaries

- Co-op defeat: a team falls only when **all** members' townhalls fall (the
  one behavior change vs. pre-team — and only for multi-human co-op).
- Perk auras / pain-share ally selection and kill-credit stay owner-scoped
  (default-safe; revisit only if PvP feel needs it).
- Mid-match team change: predicates read live `TeamID` so it would take
  effect next tick, but join/leave + FOW-history interactions are
  unvalidated — documented non-goal.

---

## 8. Tracked follow-ups (not yet implemented)

- **Damage-type resistances/weaknesses** — see §3.
- **Cast interrupts** — currently uninterruptible by design (§5.2); seam ready.
- **Auto-cast self-only "immediate" cast** — abilities that only target self
  could skip targeting mode (needs targeting flags on `AbilitySnapshot`).
- **fire_bolt sprite tuning** — `PROJECTILE_SPRITE_SCALE` /
  `PROJECTILE_SPRITE_ANGLE_OFFSET` are visual-only TODO constants (no client
  test runner); flip to the baked 8-way draw if the single rotated frame
  looks wrong.
- **Auto-cast loop interval** — runs every tick; a configurable throttle is a
  noted optimization.
