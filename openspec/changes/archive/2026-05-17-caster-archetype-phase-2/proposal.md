## Why

Phase 1 landed the `caster` combat profile and the **inert** `AbilityCategory` data seam, but the autocast loop is still "first ready ability in slot order wins" and the Acolyte's only ability is `heal`. A caster cannot yet make a situational choice (heal a low ally vs. nuke an enemy), and the Cleric / Arch Mage promotion paths grant no abilities. Phase 2 activates the category seam (situational priority selection) and gives the promotion paths real kits, delivering the actual caster fantasy from `docs/design/caster_archetype.md`.

## What Changes

- Rework `tickUnitAutoCastLocked` from **first-ready-wins** to **highest-scored-ready-wins**: keep every existing gate (autocast enabled, `SupportsAutoCast`, off cooldown, enough mana, a resolved selector target), then score each ready `(ability, target)` candidate by `AbilityDef.Category` and cast the highest. Deterministic tiebreak: `unit.Abilities` slot index, then ability id. Below a `minActivationScore` floor → cast nothing (basic attack proceeds via the unchanged combat AI).
- **No-regression invariant (load-bearing):** with a single autocast-enabled ability (today's Acolyte = heal-only), highest-scored-ready collapses to exactly the current behaviour — same cast ticks for the same seed/inputs.
- New `ability_priority.go`: per-category scoring — `heal` by ally HP deficit (+ nearby damaged allies), `offensive` by target value / cluster / finishing potential, `buff_ally` and `summon` per the design's formulas. Weights live in a small Go table keyed by `AbilityCategory` (JSON-tunable later; deliberately kept out of `CombatProfile`).
- New `path_ability_defs.go`: a loader twin of `path_defs.go` reading `catalog/units/human/acolyte/paths/<path>/abilities/<rank>.json` (shape `{ "grant": ["greater_heal"] }`) into `pathAbilityGrantsByKey[(path,rank)] []string`.
- New `assignUnitPathAbilitiesLocked(unit)`, called from `addUnitXPLocked` immediately after `assignUnitPerkLocked` — idempotent, ordered append to `unit.Abilities`, **no RNG** (the only progression RNG remains the existing path *choice*).
- Add a minimal `DamageAmount int` field to `AbilityDef`, applied in `resolveAbilityCastLocked` via the existing damage pipeline (`applyUnitDamageWithSourceLocked` with `DamageSource{Kind:"ability", DamageType: def.DamageType.OrPhysical(), AttackerUnitID: caster.ID}`) — exactly symmetric to the existing `HealAmount`. Without this an "offensive" ability would cast but deal no damage; `HealAmount` had no damage counterpart. `0`/absent ⇒ no damage (additive, inert for every existing ability).
- Author the first kits: ≥1 Cleric heal-line ability (e.g. `greater_heal`) and ≥1 Arch Mage offensive ability (`DamageAmount` + `category:"offensive"`) using the already-registered `closest_enemy_in_range` selector.
- The `Category` field becomes **active** — read by the priority scorer. This supersedes its Phase-1 "inert" guarantee (see Modified Capabilities). Not **BREAKING**: the no-regression invariant means single-ability units (incl. an un-promoted Acolyte) behave exactly as before.
- Frontend: **no protocol change** — `AbilitySnapshot` already carries everything; granted abilities appear because they are appended to `unit.Abilities`. Scope is verification only: action bar renders post-promotion abilities, per-ability autocast toggle, cooldown overlay.
- **Out of scope:** new ability *kinds* beyond heal/offensive as authored content (the `buff_ally`/`summon` scorers ship per the design but are exercised only by direct unit tests until such abilities exist); new autocast selectors beyond the registered set; weight tuning passes; any `CombatProfile` change.

## Capabilities

### New Capabilities

- `ability-priority-selection`: Category-driven highest-scored-ready autocast selection in the reworked `tickUnitAutoCastLocked` — candidate gating (unchanged), per-category scoring, deterministic tiebreak (slot index → id), `minActivationScore` floor, one cast/unit/tick, and the no-regression collapse for single-ability units.
- `per-path-ability-kits`: The `path_ability_defs.go` loader, the `assignUnitPathAbilitiesLocked` promotion-time grant (idempotent, ordered, RNG-free), the catalog file layout, and the authored Cleric / Arch Mage kits.

### Modified Capabilities

- `ability-category`: The Phase-1 requirements that pinned `Category` as **inert** (the "Category is inert in Phase 1" scenario under *`AbilityDef` carries an optional `Category` field validated at load*, and the "the `Category` tag SHALL be inert" clause of *The `heal` ability is tagged `category: heal`*) change: `Category` is now consumed by the priority scorer. The enum/field/validation/heal-tag and the heal-autocast gating guarantees are otherwise unchanged.

## Impact

- **Backend (Go)**: `ability_autocast.go` (`tickUnitAutoCastLocked` rework — the core change), new `ability_priority.go`, new `path_ability_defs.go`, `progression.go` (`assignUnitPathAbilitiesLocked` call after `assignUnitPerkLocked` in `addUnitXPLocked`), `ability_defs.go` (`DamageAmount` field), `ability_cast.go` (`resolveAbilityCastLocked` applies `DamageAmount` via the damage pipeline). Pattern twins: `path_defs.go` (loader), `perks.go` `assignUnitPerkLocked` (grant seam), existing `HealAmount` (the damage primitive's mirror).
- **Catalog (JSON)**: new `catalog/units/human/acolyte/paths/{cleric,arch_mage}/abilities/{bronze,silver,gold}.json`; new ability defs for the authored kit abilities (e.g. `catalog/abilities/greater_heal/greater_heal.json`).
- **No protocol / snapshot / wire changes**: `AbilitySnapshot` is already sufficient; abilities surface via the existing `unit.Abilities` → `abilityStatesLocked` path. No client code change required (verification only).
- **Determinism / AI_RULES**: scoring reads only live state + the ordered `unit.Abilities` slice; never persists a `*Unit` (within-tick `*Unit` handed to `beginAbilityCastLocked`, which stores `unit.CastTargetID int`); grants are deterministic ordered appends with no RNG. No tick-loop concurrency or `*Locked` convention changes.
- **Not breaking**: the no-regression invariant guarantees an un-promoted, heal-only Acolyte is byte-identical pre/post for the same seed; only multi-ability (promoted) casters exercise the new selection.
