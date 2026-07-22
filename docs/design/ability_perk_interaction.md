# Abilities & Perks: How They Interact

**Status:** Direction / architecture. Not yet implemented.
**Supersedes in practice:** the ad-hoc "perk patches an ability from outside" thinking behind Tier A/B (`abilityModifiers` / `abilityRiders`). Tier A/B are not deleted — they are narrowed to a specific role (see §7).

---

## 1. The headline rule

> **An ability must be fully readable and understandable in the editor, from
> its own program. The only acceptable indirection is a reference to another
> ability — which you can open and read the same way.**
>
> An ability owns its behavior graph. It branches on the PERKS a caster owns,
> naming them inline. Items and advancements never change behavior — they only
> move NUMBERS, through declared parameters.

No invisible action-at-a-distance. Reading one ability file tells you what that
ability does, including every perk variant of it. Sources never reach into an
ability's internals: they never address an action by id, never inject nodes,
never know the shape of the program.

This is the same inversion the unit-stat system already uses: nothing patches a
unit's damage calculation; sources contribute stat modifiers and a single
chokepoint folds them. Ability parameters are that idea applied to abilities.

---

## 2. Why the current model breaks

Sorting real Trapper perks by what they actually do to an ability:

| Kind | Example | Expressible today? |
|---|---|---|
| 1. Scalar tweak | `wider_nets`, `extended_setup`, `rapid_deployment` | Yes — Tier A `abilityModifiers` |
| 2. Additive behavior | `explosive_chain` (aftershock) | Yes — Tier B `abilityRiders` |
| 3. Multiplicity | `increased_deployment` (place N traps), Scatter Bomb | **No** |
| 4. Replacement | `lasting_flames` (damage → lingering status) | **No** |
| 5. Modification of a modification | `overload_protocol` altering the lasting-flames pit | **No** (was deferred as "Tier B.5") |

Riders can only **add**. They have no way to say "and stop doing the original
thing," which is precisely what `lasting_flames` does — it changes fire_pit from
*dealing damage* to *applying a burn that persists after the victim leaves*.
And `overload_protocol` must then compose on top of that result.

There is also a **source** problem, independent of the above. Ability numbers
are already modified by non-perk sources, hand-wired case by case:

- `Unit.TrapEffectBonus` / `Unit.TrapRadiusBonus` — set by the **Master
  Huntsman advancement** (`advancement_defs.go`), folded in
  `trapModifiersForUnitLocked`.
- `Unit.BonusArrows` — same advancement, folded elsewhere.

Any design that only lets *perks* modify abilities is wrong on arrival. Items
and advancements need the same reach, and future sources will too.

---

## 3. The model

### 3.1 Parameters (numbers)

An ability declares named, tunable numbers:

```jsonc
// catalog/abilities/fire_pit/fire_pit.json
"params": {
  "dps":            16,
  "radius":         55,
  "duration":       10,
  "placementCount":  1
}
```

The program references a parameter instead of hard-coding a literal. Any numeric
config field may be authored as a literal **or** as a parameter reference.

Parameters are the **decoupling layer between "what number" and "how it is
delivered."** This is what makes composition work: if every damage-delivery
branch reads `dps`, then a source that scales `dps` keeps working no matter
which branch is active. It never needs to know a branch exists.

### 3.2 Perk branches (semantics)

When a perk changes WHAT an ability does, the ability branches on it directly,
naming the perk:

```jsonc
{ "type": "conditional",
  "config": {
    "conditions": [ { "op": "has_perk", "right": "lasting_flames" } ],
    "then": [ /* apply a lingering burn */ ],
    "else": [ /* deal damage directly */ ]
  } }
```

Operators: `has_perk` / `not_perk`, with the perk id in `right`.

A real `else` (rather than a second, inverted sibling conditional) keeps the two
branches as ONE node that cannot drift out of sync.

**Why the perk is named inline.** An earlier revision routed this through a
declared "capability" contract so the ability would not mention the perk. That
was removed: to understand `damage_delivery = lingering_burn` you had to open a
second, non-ability file, which is exactly the indirection §1 forbids. The
decoupling it bought only matters at a branch count the design explicitly does
not target (§10's 3–4 budget).

### 3.3 Branch structure

All variants of an ability live **in that ability**, visible in one graph:

```
On Tick
  Select Targets
  Conditional — has perk: lasting_flames
    THEN  Apply Status Duration (Burning)
            On Action Complete → Play Presentation (burning)
            On Tick            → Deal Damage $dps
    ELSE  Deal Damage $dps
```

### 3.4 What a source contributes

| Source | May contribute | May NOT |
|---|---|---|
| **Perk** | parameters, and behavior via a branch the ability names | — |
| **Item / advancement** | parameters (numbers) only | change behavior |

```jsonc
// a perk or an item — same shape, source-agnostic
"abilityParams": [
  { "target": "fire_pit", "param": "duration", "op": "multiply", "value": 1.5 }
]
```

`target` accepts an ability id or `tag:<name>` (§6.2).

**Items and advancements move values, never behavior.** If something should be
item-modifiable, express it as a tracked value the ability declares — not as a
behavior flip. Worked example: an item that raises how many times an ability
chains. `chain_lightning` had its bounce count as a literal `loop.iterations: 2`,
which no source can reach. Declaring `params: { bounces: 2 }` and referencing
`"iterations": "$bounces"` makes it modifiable by any source, and a `tag:chain`
target would raise it on every chaining ability at once.

---

## 4. Resolution: one chokepoint, all sources

This is the load-bearing requirement. Parameters resolve through a **single
source-agnostic chokepoint**, exactly mirroring `effectiveStatLocked`:

```go
effectiveAbilityParamLocked(caster *Unit, abilityID, param string) float64
```

It folds, in order:

1. the ability's declared **base** value,
2. modifiers from every **perk** the caster owns,
3. modifiers from every equipped **item**,
4. modifiers from **advancements**,
5. modifiers from active **statuses**,
6. modifiers from **zone auras**.

Using the same staged `(base + Σadd) × Πmul` fold the stat system already uses
(`applyStatStages`), so add/multiply semantics and stage ordering are identical
across the two systems and nobody has to learn a second set of rules.

**Adding a new kind of source later must not require touching abilities.** A
source type is wired into the chokepoint once; every ability benefits.

### 4.1 Determinism

Sources are iterated in a **sorted, stable order** (by source id), never map
order. Float add/multiply is order-sensitive and the simulation must stay
reproducible under a seed. Same rule the perk stat pools already follow.

### 4.2 Where the runtime reads them

Conditions already resolve against `ctx.Named` — the runtime context bag — using
the existing operators (`has`, `not`, `eq/ne/lt/lte/gt/gte`). Seeding the
caster's resolved parameters into `ctx.Named` at context
construction makes **the existing condition machinery work with no new condition
types**. `deal_damage` and `restore_health` already accept an `amountRef` naming
a context scalar, so those read parameters for free.

This is the cheapest possible insertion point and should be preferred over any
new evaluation path.

---

## 5. Conflict policy

Parameters do **not** conflict: adds sum, multiplies compose, amplifies compose
as a product. All three are order-independent, so the deterministic fold has no
ambiguity to resolve.

Behavior does not conflict either, because it is not contributed by sources at
all — the ability owns its branches, and two perks wanting different behavior is
visible as two branches in one readable graph rather than an invisible race.

This is a direct benefit of naming perks inline: the whole class of
"two sources set the same key to different values" simply does not exist.

---

## 6. Worked examples

### 6.1 Fire Pit — the full composition case

This is the acceptance test for the whole design: a replacement, a second perk
composing on top without knowing about the first, and a shared parameter both
delivery modes read.

**Ability** declares `params: { dps, radius, duration, placementCount }` and:

```
on_zone_tick
├─ if damage_delivery == lingering_burn → apply burn status (its tick deals dps)
└─ else                                 → deal_damage(dps)

on_zone_expire
└─ if has_overload_effect → flame collapse: explode + apply burn
```

**Perks:**

| Perk | Contributes |
|---|---|
| `lasting_flames` | the ability branches on `has_perk: lasting_flames` |
| `overload_protocol` | the ability branches on `has_perk: overload_protocol` (a SEPARATE branch point) |
| `amplified_effects` | param `dps × 1.35` |
| `extended_setup` | param `duration × 1.5` |
| `wider_nets` | param `radius × 1.3` |

**Why it composes:** `overload_protocol` fills a *different branch point* than
`lasting_flames`, so they never contend. `amplified_effects` scales `dps`, which
**both** delivery branches read — so it works identically whether the pit deals
direct damage or applies a burn. Nothing knows about anything else.

This is the case that killed the rider model, and it falls out here with no
perk-on-perk mechanism at all.

### 6.2 Increased Deployment — multiplicity, and multi-source

`placementCount` is an ordinary parameter. The program loops `placementCount`
times, offsetting each placement by the loop index.

| Source | Contributes |
|---|---|
| `increased_deployment` (perk) | `placementCount + 2` |
| a future item | `placementCount + 1` |
| a future advancement | `placementCount + 1` |

All three stack through the same chokepoint with **zero new code** — this is the
requirement that drove §4. Note the perk should target `tag:trap` rather than
listing four ability ids; §3.4's tag extension turns four entries into one.

Multiplicity therefore needs **no engine concept of its own**. It is a parameter
plus a loop.

### 6.3 Proving it is not trap-shaped

The primitives must be general. Same two mechanisms, unrelated abilities:

- **Multi-shot:** an attack/projectile ability declares `projectileCount`. The
  Master Huntsman advancement's `BonusArrows` becomes `projectileCount + 1`,
  and the bespoke `Unit.BonusArrows` field and its fold site are deleted.
- **Healing:** a heal declares `healAmount` / `radius`; an item contributes
  `healAmount × 1.2`; a perk makes it chain, and the heal branches on
  `has_perk` into a chaining variant.
- **Any ability** gains "a source can tune my numbers" and "a source can switch
  my behavior" the moment it declares parameters and branches — no per-ability
  engine work.

### 6.4 What this deletes

- `TrapModifiers` / `TrapSpecificModifiers` and both aggregators
  (`perks_trapper.go`) — ~40 fields of hand-wired per-trap payloads.
- The twelve-payload `switch trapType` dispatch inside
  `trapSpecificModifiersForUnitLocked`. Each trap ability owns its own
  `if has_overload_effect` branch, so the "adaptive per Bronze trap" machinery
  evaporates rather than being ported.
- `Unit.TrapEffectBonus`, `Unit.TrapRadiusBonus`, `Unit.BonusArrows` and their
  fold sites, replaced by advancement-sourced parameter modifiers.
- The legacy trap runtime's per-type behavior switches (`trap.go`), once traps
  are authored as zones.

---

## 7. Which mechanism to use

| Use | When |
|---|---|
| **Parameter** | The change is a number. Always prefer this — and it is the ONLY thing an item or advancement may contribute. |
| **Perk branch** (`has_perk` + if/else) | A perk changes what the ability DOES. The ability names the perk inline. |
| **Rider** (Tier B) | The change applies uniformly across MANY abilities, or the ability genuinely should not know about it. |

Riders are **retained but narrowed**. `shared_suffering` ships on riders and
stays. They are no longer the answer for "a perk modifies one ability" — a named
branch is, because it keeps the behavior readable in the ability itself.

**The scale-vs-structure test:** does the effect work *differently*, or just
*harder*? Differently ⇒ branch. Harder/bigger/more often ⇒ parameter.

---

## 8. Supporting feature: visible zones

`AbilityZone` (`ability_zone.go`) is already a persistent, tick-driven spatial
entity with `on_zone_tick` / `on_zone_enter` / `on_zone_exit` triggers running
through the normal executor. It is, however, explicitly *"server-only by design:
never serialized"* — it has no client representation.

Add an **opt-in visibility** option to `create_zone`: a zone may declare a
sprite/presentation and, when set, is serialized so the client renders it as a
persistent world entity.

This is deliberately generic — not a trap feature. Any ability that wants a
visible persistent area (a healing circle, a hazard, a ward) gets it from the
same option. Traps are simply the first consumer, and it is what lets the trap
*runtime* be deleted while keeping traps visible.

---

## 9. Authoring guidance

**When building an ability that you want sources to be able to modify:**

1. **Declare every number a designer might want tuned as a `param`.** A literal
   buried in an action config is a number no perk, item or advancement can ever
   reach. This is the single most important habit.
2. **Read the parameter from every branch.** If two branches deliver damage
   differently, both read `dps`. That is what makes a scaling source
   branch-agnostic.
3. **Express a perk variant as an if/else branch naming the perk.** One node,
   two sides, readable in place. Put the "what it means" wording in the branch's
   own actions and in the PERK's description — not in an indirection layer.
4. **Give each perk its own branch point.** Two perks touching different
   branches compose with no interaction; two perks on the same branch is a
   design smell you can see in one graph.
5. Reach for a rider only when the behavior is genuinely cross-ability.

**When building a source (perk / item / advancement):**

1. Prefer contributing a parameter. An item or advancement may ONLY contribute
   parameters — behavior belongs to a perk branch the ability names.
2. **Use the scale-vs-structure test.** A branch is for a *perk* (any rank)
   introducing a genuine behavioral variant — the effect works
   *differently*, not just *harder*. If the source is an item, an advancement,
   or a perk that just "makes it stronger / bigger / more often," it is a
   **parameter** — full stop. Reaching for a branch to express scaling is the
   mistake that erodes the §10 budget.
3. **Respect the budget.** If an ability is already at 3–4 branches and you want
   another, the answer is usually a second ability, not a fifth branch.
3. Ship a behavioral perk together with any parameters that tune it.
4. Target a **tag** rather than enumerating ability ids where the intent is
   "all abilities of this kind."

---

## 10. Consequences to design for

**Branching is bounded by a design budget — 3–4 mutations per ability.**

> **An ability is expected to carry at most 3–4 behavioral variants.
> Only *perks* introduce branches (at any rank — the Cleric has a Bronze perk
> that modifies an ability). Everything else — items, advancements, statuses,
> auras, and scaling perks — contributes *scale*, not *structure*.**

Be precise about what kind of claim this is: it is a **design budget, not an
architectural guarantee**. Nothing in the engine prevents a fifth or tenth
branch. The budget is a discipline the catalog is held to, and exceeding it is a
signal that the ability is doing too much and should probably be **split into
two abilities** rather than grown another branch.

Two things keep it realistic:

- A unit holds **one perk per rank**, so at most three branches can be live at
  once on any given caster regardless of how many the ability declares.
- Trap-specific perks are gated to a single ability, so they do not accumulate
  across the family: `barbed_field` only ever appears in `caltrops`,
  `lasting_flames` only in `fire_pit`.

Concretely `fire_pit` carries three branch points — `lasting_flames`,
`ascendant_infusion`, `overload_protocol` — comfortably inside budget. The other
five Trapper perks (`amplified_effects`, `extended_setup`, `wider_nets`,
`rapid_deployment`, `increased_deployment`) add **zero** branches; they are
parameter modifiers, as is every future item or advancement that "makes the
number bigger."

So the graph does not grow with the catalog — it grows only with the number of
genuine behavioral variants, which is held to a handful by intent. Editor
mitigations (collapse inactive branches, preview-as-if-caster-had-perk-X)
remain worth building for readability, but they are conveniences, not
load-bearing. If they ever become load-bearing, the budget has been breached and
the ability should be split.

**Extensibility direction.** Adding a source that changes an ability's *behavior*
means editing that ability to add a branch; riders let a perk be entirely
self-contained. This is a real trade, but the budget limits its blast radius: it
applies only to behavioral perks, which are rare and deliberate. The common case
— a new item or advancement that scales an existing number — touches **no
ability files at all**, because it flows through the parameter chokepoint (§4).
Pure-additive cross-ability effects should still be riders.

**Generated descriptions.** Tooltip prose is Go-generated and is the single
source of truth. Branches and parameters must both describe: an ability should
describe its default branch and note its variants; a source should describe as
"Fire Pit: damage is delivered as a lingering burn" or "+2 trap placements".
A mechanism that cannot be described in prose is not acceptable here — this is
part of why open-ended program patching was rejected.

---

## 11. Decisions

### D1 — Parameter references ride the existing substitution pass

A program references a parameter as a **`"$name"` string in any numeric config
field**: `"radius": "$radius"`.

Resolution reuses machinery that already exists. `ability_exec.go` decodes every
action config as `desc.Decode(ctx.resolveConfigVars(a.Config))`, and
`resolveConfigVars` (`ability_exec_loop.go`) already unmarshals the raw config,
walks it **recursively**, replaces strings naming an in-scope loop variable with
their numeric value, and re-marshals. `substituteLoopVarPlaceholders` is its
documented static-validation counterpart, already called from the validator.

Extending that walk to also resolve `$name` against the caster's resolved
parameter set gives parameter support in **both the exec and validate paths**,
for every numeric field including nested ones, with no per-action work.

- Keep the existing fast-path guard, widened to "no loop vars **and** no
  params," so an ability declaring none pays nothing.
- Use the `$` sigil rather than the bare-name convention loop variables use:
  loop vars are single letters, parameters are words, and the sigil stops a
  legitimate string value from being substituted by accident.

*Rejected:* per-field `…Ref` siblings (combinatorial — `radiusRef`,
`durationRef`, `tickIntervalRef`… on every action; it is why only `amount` ever
got one) and a union `ParamValue` type (churns every config struct and every
read site for the same result).

### D2 — Perks are named inline; capabilities were built and REMOVED

An ability branches on a perk by name (`has_perk` / `not_perk`, §3.2), with a
real `else` so a branch is one node rather than two that can drift.

**This reverses an earlier decision, and the history is worth keeping.** D2
originally specified a declared "capability" contract: the ability published
branch keys and allowed values, and a perk set one, so the ability never named
the perk. It was built — declaration, grants on perks and items, resolution,
load-time validation, a `ctxString` context kind, string comparison operators —
and then removed, because:

1. It violates §1. To know what `lasting_flames` does you had to read the perk
   file, find `damage_delivery = lingering_burn`, then find the branch in the
   ability. Two files and a join key, for one behavior.
2. The coupling it avoided only bites at a branch count §10 explicitly does not
   target. At 3–4 branches, `if has perk X` is simply more readable.
3. Its remaining justification — a non-perk source flipping behavior — was ruled
   out by the rule that items and advancements move values, never behavior.

The removal took `ctxString` and the string `eq`/`ne` comparison with it, since
capabilities were their only consumer. Per this project's standing rule (delete
unused things now, re-add when needed), leaving it as a second, unused way to do
the same thing was not acceptable.

### D3 — Ability damage scales off a unit stat, not per-rank parameters

Rather than each ability hard-coding its damage per unit rank, **the unit
carries an ability-damage scalar that the abilities it casts scale by.**

This belongs in the existing stat system, not in the parameter system:

- Register it in `statRegistry` so it folds through `effectiveStatLocked`
  alongside every other stat, contributed by rank, **items, advancements**,
  perks, statuses and auras with no new plumbing.
- Make it **base-authorable** (`UnitDef.baseStats`), so a unit type can carry
  its own baseline the way `critChance` / `lifesteal` / `thorns` already do.
- Fold it at the existing ability-damage chokepoint
  (`effectiveAbilityDamageLocked`), so every ability benefits without per-ability
  work.

This is strictly more useful than per-rank damage tables: it gives units a
general "my abilities hit harder" axis that items and advancements can improve
later, which is the actual long-term goal.

**Known gap — non-damage rank scaling.** This covers the damage axis only.
`fire_pit` today also scales *radius* by rank (55 → 75 → 95), and a damage stat
does not express that. Options, unresolved: allow a per-rank base block on
`params` for the non-damage cases, treat rank as an ordinary modifier source, or
decide that non-damage rank scaling is a balance nicety we drop. Expect this to
matter far less once damage is a stat — flag it, do not block on it.

### D4 — Tag targeting lands in phase one

`target` accepts `tag:<name>` as well as an ability id. `AbilityDef.Tags` already
exists and the four trap abilities already carry `"tags": ["trap"]`, so the
resolver change is small.

Without it, `amplified_effects`, `extended_setup`, `wider_nets`,
`rapid_deployment` and `increased_deployment` each need four entries — **20 rows
that all mean "all traps"** — and adding a fifth trap means editing five perks.

*Expect this to need adjustment in real use* (tag granularity, whether a source
may target both a tag and an id, precedence between them). Build it simple,
revisit once it has real traffic.

### D5 — Parameter references are validated at load

Non-negotiable, and the reason is historical: the whole data-driven perk effort
exists because `PerkDef.config` was a freeform map where **a typo'd key silently
read 0 forever** while the perk still badged itself "wired." `$name`
substitution is stringly-typed and reintroduces exactly that failure mode.

- A program referencing an undeclared parameter → load-time error.
- A source targeting an undeclared parameter, or setting an undeclared
  parameter → load-time error.

The validator already runs the substitution counterpart
(`substituteLoopVarPlaceholders`), so it is the natural home.

**Folded-in consequence — generated descriptions must resolve parameters.**
`describeAbility` builds tooltip prose by reading literals out of action
configs. Once `radius` is `"$radius"`, it must resolve parameters against the
**base** set (no caster) or every parameterized ability's tooltip degrades to
blanks. Generated prose is the single source of truth for player-facing text in
this codebase, and the coverage tests assert every catalog ability produces
prose — so this ships **with** the parameter work, not after it.

---

## 12. Suggested phasing

1. **Visible zones** (§8) — independent of everything else, unblocks traps, and
   is the piece most likely to surface an unknown. Fail fast here.
2. **Parameters + the resolution chokepoint** (§3.1, §4, D1) — including
   load-time validation (D5) and parameter resolution in `describeAbility`
   (D5's folded consequence) in the same phase, not after. Wire at least two
   source types (perk and advancement) so source-agnosticism is proven, not
   assumed.
3. **Ability-damage unit stat** (D3) — small, independent, and immediately
   useful on its own: register the stat, make it base-authorable, fold it at
   `effectiveAbilityDamageLocked`. Can land in parallel with (2).
4. **Perk branches** (§3.2–3.3, D2) — the `has_perk`/`not_perk` condition
   operators and a real `else` on `conditional`.
5. **Fire Pit vertical slice** (§6.1) — `fire_pit` + `lasting_flames` +
   `overload_protocol` end-to-end. This exercises every hard part at once; if it
   lands cleanly the rest of the catalog is mechanical, and if it does not, the
   flaw is found for the cost of one ability instead of ten.
6. **Editor bidirectionality** (§5.1) — forward contract lookup and the reverse
   "who targets this ability" index. Deliberately after the slice, so it is built
   against a real declared contract rather than a guessed one.
7. Migrate the remaining traps and Trapper perks; delete the legacy trap runtime
   and the bespoke aggregators (§6.4).

---

## 13. Migration tracker — Trapper traps

Live status of the migration that is proving this design. **A trap is only
"done" when every source that used to affect it still does.** The trap-specific
Gold payloads are the easiest thing to forget: they live in
`trapSpecificModifiersForUnitLocked`, and a migrated trap stops going through it
SILENTLY — nothing fails, the perk just quietly does nothing.

### Infrastructure — DONE

Visible zones (§8) · ability parameters + `paramsByRank` (§3.1, D1) ·
ability-damage unit stat (D3) · perk branches via has_perk + if/else (§3.2, D2) ·
tag targeting (D4) · load-time validation (D5) · three wired source families
(perks, items, unit-defs/advancements).

### `fire_pit` — MIGRATED, NOT COMPLETE (Gold perks still inert)

Done: authored as a visible zone; `params` (dps/radius/duration/burnDuration) +
`paramsByRank` (16/28/45, 55/75/95); an inline `has_perk` if/else branch plus a
`burning` presentation;
`lasting_flames` is a pure data perk; `extended_setup` / `wider_nets` /
`amplified_effects` reach it via `abilityParams`; `rapid_deployment` via
`abilityModifiers.cooldownMult`; Master Huntsman via `unitAbilityParamMul`.

Outstanding — **each is a silently-inert perk today**:

1. **`overload_protocol` (Flame Collapse)** — needs its own `has_perk` branch
   on zone expiry. This also completes the §6.1 **composition proof**: a second perk
   filling a DIFFERENT branch point on the same ability, composing with
   `lasting_flames` without either knowing about the other. Replacement is
   proven; composition-without-knowledge is NOT yet.
2. **`ascendant_infusion` (Reactive Flames)** — needs its own `has_perk` branch.
3. **`increased_deployment`** — needs a `placementCount` param + a placement
   loop; multi-drop lived in `plantOneTrapLocked`, which the zone bypasses.
4. **Client trap tooltip** — `DebugEffectiveTrapStats` returns false for a
   migrated trap (it looks for a `place_trap` action), so `effectiveTrap` no
   longer populates. Repoint it to read a migrated ability's zone config.

None require new primitives.

**Preview fidelity gaps** (the preview is where abilities are actually tested,
so these matter): force-branch overrides are local component state and RESET ON
REMOUNT; the preview caster is an `adept` casting at a `raider`/`soldier`, not
the real Trapper and its targets, so anything placed relative to a unit renders
against different bounds than it will in a match; and the preview direct-assigns
snapshot state with no interpolation, so a unit-anchored effect does not ride a
moving unit the way it does live. Simulation itself IS 1:1 — same GameState,
same executor.

### Editor surface — DONE (2026-07-21)

Migrating fire_pit exposed that the mechanisms above had NO editor
representation, which violated §1's headline rule outright. All four are fixed:

1. **Params were invisible.** The client modeled neither `params` nor
   `paramsByRank` — they survived saves only via `formFromDef`'s generic
   `remainder` passthrough. Opening Create Zone showed a BLANK duration, because
   the config value is the string `"$duration"` and the control is
   `<input type="number">`. Now: a **Parameters card** (Identity tab) with base
   + silver/gold columns (bronze IS the base; blank override shows the inherited
   value as placeholder) and per-param usage counts, and every numeric field has
   a Number/Parameter mode showing and editing the bound param's value inline.
   Renaming rewrites every `$ref` program-wide in one undo step; a dangling ref
   renders flagged, never blank. Card summaries resolve refs (`16 ($dps)`).
2. **Conditional branches did not render.** The flow view walked nested
   *triggers* but not nested *action lists*, so a `has_perk` branch's contents
   were invisible — fire_pit read as a bare `Conditional` leaf. THEN/ELSE now
   render recursively and collapse independently; ELSE always renders so a first
   else action can be authored.
3. **An action inside a branch lost its own nested triggers**, because that
   block lived on the parent FlowTriggerCard's action loop. Moved onto
   FlowActionCard, so an action renders its own subtree at any depth.
4. **Perk branches were unreachable in preview.** The synthetic caster owns no
   perks, so every `has_perk` evaluated false. `PreviewRequest.ConditionalOverrides`
   (keyed by the conditional's authored action id — stable, unlike a flow path)
   plus force-branch checkboxes in the Preview Scene card. Traced with a
   `forced` payload so the event log never claims a condition was evaluated when
   it was overridden.

### Presentation/VFX — one root cause, three symptoms (2026-07-21)

`burning` is drawn by TWO renderers: `drawBurningOverlay` (perk-driven, from
`UnitSnapshot.burningRemaining`) and `drawEffects` (from an `EffectSnapshot` —
how a data-authored status's bound visual arrives). The asset was authored for
the first and had never been through the second, so migrating fire_pit broke
every dimension the two handled differently: SIZE (body-height fraction vs raw
sheet px), PLACEMENT (sprite rect vs unit origin, which is at the feet), and
FRAMES (clock loop vs progress — an 8s burn over 5 frames advanced once per
1.6s, i.e. a still image). All three now go through shared helpers in
`client/src/game-portal/src/game/rendering/effectPlacement.ts`, so the two paths
are identical by construction. Remaining known difference: the legacy overlay
applies an alpha flicker the effect path does not.

Two server-side fixes fell out of the same investigation:
`play_presentation` needs `bindToStatusDuration` to anchor to the afflicted unit
(without it the at-point shape renders at the cast point), and a
`refresh`-stacking status re-runs its On Apply on EVERY application — harmless
for `change_stat`/`apply_mark` (they write to the orphaned status object) but
NOT for `play_presentation`, whose write lands on game state. It stacked ~11
flames over a 10s pit; `refreshEffectOnUnitForDurationLocked` makes it
idempotent per (unit, effect).

### ALL FOUR TRAPS MIGRATED

| Trap | Shape | Notable |
|---|---|---|
| `fire_pit` | ticking zone | `has_perk` if/else for lasting_flames + `burning` presentation; `paramsByRank` |
| `caltrops` | ticking zone | slow via `change_stat`; forced `statOpAmplify` into existence |
| `explosive_trap` | one-shot zone | forced `consume_zone` into existence |
| `marker_trap` | zone + on_enter mark | forced the `damageTaken` stat into existence |

**No ability authors `place_trap` any more**, so the legacy trap runtime is dead
code. Remaining teardown (§6.4): `place_trap` + its action, `plantTrapLocked` /
`plantOneTrapLocked` and trap.go's six per-type behavior switches, the `Trap`
entity and its snapshot producer, `TrapModifiers` / `TrapSpecificModifiers` and
both aggregators, `Unit.TrapEffectBonus` / `TrapRadiusBonus`, and the legacy
`unitTrapEffectMul` / `unitTrapRadiusMul` advancement kinds.

Each migration got cheaper as the vocabulary filled in: fire_pit needed three new
primitives, caltrops one, explosive_trap one, marker_trap one — and the test-side
difference is absorbed entirely by `mustTrapAbilityConfig`.

### Still outstanding (perks, not traps)

- **Gold branches** — `overload_protocol` and `ascendant_infusion` have a payload
  per trap; each becomes a `has_perk` branch in that trap. **Currently inert.**
- **`increased_deployment`** — needs a `placementCount` param + a placement loop.
  **Currently inert.**
- **`barbed_field`** — needs the per-victim accumulator (a STATE primitive, not a
  composition one). **Currently inert.**
- Trap-specific silvers `explosive_chain` / `exposed_weakness` still ride the
  legacy aggregator and are **inert** on their migrated traps.

### Transition rule (what makes this incremental)

A modifier perk carries BOTH during the migration: `config` (read by the legacy
aggregator for unmigrated traps) and `abilityParams` (read by the chokepoint for
migrated ones). No double-application is possible because
a trap is either legacy or migrated, never both — which is what lets traps
migrate one at a time instead of in a big-bang cutover.

### Definition of done

All four traps migrated, all ten Trapper perks on params/branches, then
delete: `trap.go`'s per-type behavior switches, `TrapModifiers` /
`TrapSpecificModifiers` and both aggregators, `Unit.TrapEffectBonus` /
`TrapRadiusBonus` / `BonusArrows` and their fold sites, and the legacy
`unitTrapEffectMul` / `unitTrapRadiusMul` advancement effect kinds (§6.4).
