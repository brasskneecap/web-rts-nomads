## Context

Verified against current code (Phase 1 is merged + archived; canonical specs exist at `openspec/specs/{ability-category,caster-combat-profile}/spec.md`):

- **Autocast loop** `tickUnitAutoCastLocked` (ability_autocast.go:74-112) is *first-ready-wins*: iterates `unit.Abilities` in slot order; the first ability that is autocast-enabled, `SupportsAutoCast`, off cooldown (`unit.AbilityCooldowns[id] <= 0`), mana-affordable (`unit.CurrentMana >= def.ManaCost`), and has a non-nil `resolveAutoCastTargetLocked` target is cast via `beginAbilityCastLocked`, then `return` (one cast/unit/tick; never while `unit.CastAbilityID != ""`).
- **Selectors** (autocast_selectors.go): name→`AutoCastSelector` registry; `resolveAutoCastTargetLocked(caster, def)` returns a within-tick `*Unit` or nil. Registered: `lowest_hp_percentage_ally_in_range` (heal), `closest_enemy_in_range` (`selectClosestEnemyInRange`, autocast_selectors.go:122-135 — a working stub usable for the Arch Mage offensive ability), `self`. Selectors iterate the `s.Units` slice with ordered tiebreaks (deterministic).
- **Grant seam** `addUnitXPLocked` (progression.go:232-260): per crossed rank it calls `assignUnitPathOnRankUpLocked` → `assignUnitPerkLocked` → `applyRankModifiersLocked` → `onUnitRankUpLocked`. `assignUnitPerkLocked` (perks.go:610) is the structural twin for the new `assignUnitPathAbilitiesLocked`.
- **Loader twin** `path_defs.go`: `//go:embed catalog/units` `embed.FS`; per-`(path,rank)` keyed maps (e.g. `"vanguard/bronze"`), rank names validated `bronze/silver/gold` with a panic on unknown. `path_ability_defs.go` mirrors this exactly.
- **Snapshot** `abilityStatesLocked` (ability_autocast.go:120-145) builds `[]protocol.AbilitySnapshot` from `unit.Abilities` (ID/DisplayName/Icon/ManaCost/SupportsAutoCast/AutoCast/CooldownRemaining/CooldownTotal). Granting an ability = appending its id to `unit.Abilities`; the snapshot then carries it with no protocol change.
- **`Category`** (ability_defs.go) exists and is validated at load but is read by no runtime code (Phase-1 inert). The canonical `ability-category` spec pins this inertness in two places — Phase 2 modifies those.

Constraints (`.claude/rules/AI_RULES.md`): Go server authoritative; deterministic under seed (no wall-clock, no unseeded `math/rand`, no outcome-driving map iteration); `*Locked` conventions; targets by ID, re-resolved + revalidated per tick; never persist a `*Unit`.

## Goals / Non-Goals

**Goals:**

- A multi-ability caster picks the situationally-best ready ability each tick via category-driven scoring, with fully deterministic tiebreaks.
- **Zero behavioural change for single-ability units** (the load-bearing no-regression invariant): one autocast ability ⇒ highest-scored-ready is identical to first-ready.
- Cleric / Arch Mage gain path-specific abilities deterministically on promotion (idempotent, ordered, RNG-free).
- No protocol/snapshot change; granted abilities surface through the existing `unit.Abilities` → `abilityStatesLocked` path.
- Author the first real kits (≥1 Cleric heal-line, ≥1 Arch Mage offensive on `closest_enemy_in_range`).

**Non-Goals:**

- New ability *kinds* as shipped content beyond the heal/offensive kit (the `buff_ally`/`summon` scorers exist per the design but are exercised only by direct unit tests until such abilities are authored).
- New autocast selectors; weight-tuning/balance passes; any `CombatProfile` change; player-cast/manual-cast changes (locked Phase-1 decision: manual cast just preempts).

## Decisions

### Decision 1: `tickUnitAutoCastLocked` becomes gather → score → pick, gates unchanged

Restructure into: (1) **gather** — iterate `unit.Abilities` in slot order, applying the *exact existing gates* (enabled, `SupportsAutoCast`, off cooldown, mana, non-nil `resolveAutoCastTargetLocked`), collecting `{slotIndex, abilityID, def, target}` candidates; (2) **score** each via `ability_priority.go`; (3) **pick** the max score; tiebreak by ascending `slotIndex`, then ability id; if `best.score < minActivationScore` cast nothing; else `beginAbilityCastLocked` + arm cooldown, exactly as today. The pre-flight guards (`unit == nil`, `HP <= 0`, no autocast map, `CastAbilityID != ""`) and "one cast/unit/tick" are unchanged.

**Why gates-before-score:** preserves mana/cooldown semantics verbatim and means a lone candidate is scored only against `minActivationScore`. With one candidate the pick is that candidate iff it would have been cast before — *provided* `minActivationScore` is ≤ the score any currently-castable single ability produces (see Decision 3). This is what makes the no-regression invariant hold by construction.

*Alternative considered:* score-then-gate (score all abilities, then filter). Rejected — it risks scoring an unaffordable/cooldowned ability and complicates the equivalence proof; gating first matches today's control flow.

### Decision 2: Category scoring lives in `ability_priority.go`, weights in a Go table

`scoreAutoCastCandidateLocked(unit, def, target) float64` switches on `def.Category`:

- `heal`: `w.heal * clamp01((healThresholdPct − targetHPpct)/healThresholdPct)` plus a bounded bonus for other nearby damaged allies. Generalises Phase-1's implicit "<100%" selector behaviour.
- `offensive`: target strategic value / cluster / finishing potential (reusing existing scoring helpers where available).
- `buff_ally`: high when the target lacks the buff and is in combat, ~0 otherwise.
- `summon`: from local force deficit; target is self.
- Empty/unknown `Category`: a defined conservative score (≥ `minActivationScore` for a single-candidate so the no-regression invariant also covers an *uncategorised* lone ability — important because not every existing autocast ability need be categorised).

Weights: a small unexported `map[AbilityCategory]…` table in `ability_priority.go`. **Not** on `CombatProfile` (avoids profile bloat; JSON-tunable later). Scoring reads only live state + ordered slices; no persisted pointer; deterministic.

### Decision 3: `minActivationScore` chosen so a lone categorised/uncategorised candidate never regresses

`minActivationScore` is a low constant such that any ability that is *currently castable and has a valid selector target* scores strictly above it in its normal operating range (heal of a damaged ally, offensive on a valid enemy, or the uncategorised fallback). The no-regression test (below) is the executable proof: same seed, heal-only Acolyte, identical cast-tick set pre/post. If a category's natural minimum could dip to the floor, that category's formula gets a small positive ε so a valid-target candidate always clears it.

### Decision 4: `path_ability_defs.go` is a structural twin of `path_defs.go`

Same `//go:embed catalog/units` FS, same `(path,rank)` key, same `bronze/silver/gold` validation-panic. Parses `{ "grant": ["<abilityID>", …] }`; unknown ability ids panic at load (mirror the `path_defs.go` strictness and Phase-1's category-validation panic). Produces `pathAbilityGrantsByKey[(path,rank)] []string`. Missing file ⇒ empty grant (not an error) — most `(path,rank)` cells are empty.

### Decision 5: `assignUnitPathAbilitiesLocked` — idempotent ordered append, RNG-free, after `assignUnitPerkLocked`

Inserted in `addUnitXPLocked`'s rank loop immediately after `assignUnitPerkLocked` (so it runs once per crossed rank, with the correct path already assigned). For the unit's `(path, rank)` it appends each granted ability id to `unit.Abilities` **iff not already present** (idempotent — safe across the multi-rank catch-up loop and re-entry). Order = catalog list order = deterministic. No RNG: the only progression RNG remains the existing path *choice* in `assignUnitPathOnRankUpLocked`. `AbilityDef.Type == "spell"` and the autocast/cooldown maps initialise lazily exactly as for base abilities — no spawn-path change.

### Decision 6: Modify, don't fork, the `ability-category` inertness requirements

Phase 2 issues a `MODIFIED` delta against the canonical `ability-category` spec: the "Category is inert in Phase 1" scenario becomes "Category drives autocast priority selection", and the heal requirement's "the `Category` tag SHALL be inert" clause is updated to "participates in priority scoring". The enum/registry/field/load-validation/heal-tag requirements and the entire heal-autocast gating-equivalence requirement are copied through unchanged (MODIFIED requires the full updated requirement body).

### Decision 7: Minimal `DamageAmount` ability primitive (mirrors `HealAmount`)

Surfaced during implementation: `resolveAbilityCastLocked` (ability_cast.go) applies only `def.HealAmount` + the visual `EffectOnTarget`; `AbilityDef` has **no damage field**, so an authored "offensive" ability would cast (animation, mana, selector, scoring all correct) but deal **zero damage** — a hollow deliverable. The autocast/grant/scoring work (Decisions 1–6) is unaffected; the gap is purely ability *resolution*.

**Chosen:** add `DamageAmount int \`json:"damageAmount,omitempty"\`` to `AbilityDef`, applied in `resolveAbilityCastLocked` exactly symmetric to `HealAmount`:

```go
if def.DamageAmount > 0 && target.HP > 0 {
    s.applyUnitDamageWithSourceLocked(target, def.DamageAmount,
        DamageSource{AttackerUnitID: caster.ID, Kind: "ability", DamageType: def.DamageType.OrPhysical()})
}
```

This routes through the existing authoritative damage pipeline (same entrypoint melee/splash use, e.g. state_combat.go:242/315), so mitigation, the death pipeline, threat, and determinism all apply for free. `0`/absent ⇒ no damage: additive and **inert for every existing ability** (only `heal` exists, `DamageAmount` unset). No new enum/registry/validation (it is a plain magnitude like `HealAmount`). An ability may carry both `HealAmount` and `DamageAmount`; they are independent resolve steps.

*Alternatives considered:* (a) ship offensive selection-only / inert-on-resolve — rejected by the user as a hollow Phase-2 deliverable; (b) effect-driven damage via `EffectOnTarget` — rejected: effects are transient visuals with no damage primitive, and it would invent a parallel damage path instead of reusing the pipeline.

**Spec home:** the offensive-damage-on-resolve requirement lives in the `per-path-ability-kits` delta (the Arch Mage kit is what necessitates it and that spec already asserts the Arch Mage offensive ability); no new capability is introduced.

## Risks / Trade-offs

- **[No-regression invariant is the whole ballgame]** If `minActivationScore` or any single-candidate score is mis-set, every existing autocast unit silently changes cadence. → Decision 3 + an executable seeded-replay equivalence test (heal-only Acolyte, identical cast-tick set pre/post) is the gate; also assert the gather phase reproduces the old gate set exactly.
- **[Uncategorised abilities]** Other abilities in the catalog may have no `Category`. → The empty-category fallback score (Decision 2) keeps them behaving as first-ready for the single-ability case; multi-ability units mixing categorised/uncategorised are covered by explicit tests.
- **[Determinism of scoring]** A score that reads map iteration or unseeded randomness would desync replays. → Scoring reads only live unit fields + ordered slices; tiebreak is total (slot index then id); covered by a seeded multi-ability replay test.
- **[Grant idempotency across multi-rank catch-up]** A single large XP gain crosses several ranks in one loop; a non-idempotent append would duplicate abilities. → Append-iff-absent (Decision 5) + a test that boosts XP past multiple thresholds at once and asserts no duplicate grants.
- **[buff_ally/summon scorers unexercised end-to-end]** Shipping formulas with no authored ability risks rot. → Direct unit tests of `scoreAutoCastCandidateLocked` for those branches; explicitly out of integration scope and documented.
- **[`DamageAmount` is a new combat surface]** A new way to deal damage could bypass mitigation/threat/death handling if hand-rolled. → It does NOT hand-roll: it calls the same `applyUnitDamageWithSourceLocked` entrypoint melee/splash already use, so mitigation, death pipeline, threat, and determinism are inherited unchanged. Covered by a resolve test asserting damage is applied, attributed to the caster, typed by `def.DamageType`, and that `0`/absent deals none.

## Migration Plan

No data/state migration. New `catalog/.../paths/<path>/abilities/<rank>.json` files and new ability defs are additive; absent files mean "no grant". An in-flight save with no granted path abilities simply has shorter `unit.Abilities`; promotion grants apply going forward. **Rollback:** revert the `tickUnitAutoCastLocked` rework (first-ready restored) and remove the new files; the inert `Category` field can stay compiled in harmlessly (back to Phase-1 state). No protocol/version coordination with clients.

## Open Questions

- None blocking. Concrete weight values and `minActivationScore`/`healThresholdPct` constants are placeholders for a later balance pass (acknowledged Phase-2 non-goal); they must satisfy Decision 3's invariant but are otherwise un-tuned.
