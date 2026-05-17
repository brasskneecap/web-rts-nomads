## Context

The Apprentice is authored in `catalog/units/human/apprentice/apprentice.json` with `"archetype": "archer"` and `"combatProfile": "archer"`. `resolveCombatProfile` (combat_ai_profiles.go:444) reads `CombatProfile` directly when set, so the Apprentice runs the `archer` profile verbatim.

Verified against current code:

- The `archer` profile (combat_ai_profiles.go:33-61) has **no** `RetreatDistance` / `RetreatTriggerMeleeRange` (zero-valued ⇒ no retreat). The `support` profile (combat_ai_profiles.go:317-349) sets `RetreatDistance: 120`, `RetreatTriggerMeleeRange: 90`, `Backline: true`. So an archer-profiled caster stands and dies to melee; a support-derived one kites. Design-doc conclusion #2 holds.
- `support` is tuned for a short-range AoE healer: `MaxChaseDistance: 110`, `LeashDistance: 160`, `AoERadius: 70`, and a `AoECluster: 18` target weight. The Apprentice's *current* kit is single-target and 220-range: its basic attack fires the `fire_bolt` projectile and its only ability is `heal` — neither is AoE. `effectiveLeashDistance` (combat_ai_profiles.go:437) clamps the effective leash up to `AttackRange` (160→220), but **`MaxChaseDistance` has no equivalent clamp** — a verbatim `support` clone would silently shrink the Apprentice's pursuit range from `archer`'s 180 to 110. The `AoERadius`/`AoECluster` fields are dead weight given that current single-target kit (a future AoE caster ability is an anticipated extension — see Decision 1 — that would warrant re-tuning the profile then, not a reason to keep support's AoE tuning now). So a *verbatim* clone is not balance-neutral; the `caster` profile takes two deliberate deltas (see Decision 1).
- `UnitDef.CombatProfile` is validated at catalog load: unit_defs.go:170-172 panics if the profile name is not a key in `combatProfiles`. This imposes a hard implementation ordering constraint (see Decision 4). `UnitDef.Archetype` is **not** load-validated.
- `unit.Archetype` has a second consumer beyond profile-fallback: it is the match key for archetype-scoped upgrades. `upgrade_apply.go:55-56` (`upgradeScopeArchetype` ⇒ `unit.Archetype == def.Archetype`) and `:101-102` (the buff equivalent). The live upgrades `swift_strikes_common` (`multiplier: 1.08`, +8% attackSpeed) and `swift_strikes_rare` (`multiplier: 1.14`, +14% attackSpeed) are both `scope: archetype`, `archetype: "archer"`, `maxStacks: 3`. The Apprentice currently matches them; flipping `archetype` to `caster` removes it from that pool with no replacement (see Decision 5).
- `AbilityCategory` does not exist. `AbilityDef` (ability_defs.go:92) has no `Category` field. The codebase already has a clean precedent for an extensible string enum with a registry + load-time validation: `DamageType` (damage_type.go:25-91) with `damageTypeRegistry`, `RegisterDamageType`, `IsValidDamageType`, and the panic at ability_defs.go:232. (`RegisterDamageType` has no dynamic callers today — it is a static-but-extensible convention; `AbilityCategory` mirrors it for codebase consistency and to give Phase 2 a ready validated seam.)
- `inferCombatArchetype` already maps enemy `"caster"` → `"support"` (combat_ai_profiles.go:473), but that is a *fallback* only reached when `CombatProfile` is empty. It is not a registered profile and does not give the Apprentice a profile.
- The Cleric and Arch Mage are **not** separate `UnitDef`s. `catalog/units/human/apprentice/paths/cleric/cleric.json` / `.../paths/arch_mage/arch_mage.json` (each path owns a directory; `path_defs.go` reads `<path>/<path>.json`, so there is no top-level `cleric.json`) are rank stat-*multiplier* files; they carry no `type`, `archetype`, or `combatProfile`. Promotion keeps `unit.UnitType == "apprentice"` and applies multipliers, so `resolveCombatProfile` always resolves through `apprentice.json`. The paths inherit `caster` because they *are* the Apprentice `UnitDef` with multipliers — not via any field in the path files (see Decision 6).

**Deliberate divergence from the source design doc.** `docs/design/caster_archetype.md` frames Phase 1 as "**zero behaviour regression**" and its QA bullet demands "heal autocast **byte-identical** pre/post (same seed → same cast ticks)". That framing is technically false: the heal autocast selector is position-gated (`lowest_hp_percentage_ally_in_range`, `castRange: match_attack_range`) and the `caster` profile deliberately moves the unit (retreat), so cadence legitimately changes once the Apprentice retreats. This change **knowingly supersedes** that wording with the enumerated-behavioural-deltas model and the scoped no-melee heal tripwire below; the source design doc should be corrected to match (per `.claude/rules/AI_RULES.md`: trust the code, flag the doc for correction rather than bending the implementation to a misread).

Constraints (per `.claude/rules/AI_RULES.md`): Go server authoritative; deterministic tick under seed; `*Locked` conventions preserved; targets by ID. None of this change touches the tick loop's per-tick logic — it is profile-table + enum + catalog data only.

## Goals / Non-Goals

**Goals:**

- The Apprentice (and Cleric / Arch Mage, which are the same `UnitDef` + rank multipliers) kites melee attackers instead of standing still and dying.
- Introduce a registered `caster` combat profile that is `support`-derived (positioning + retreat + scoring identity) with exactly two intentional Phase-1 deltas — `MaxChaseDistance` near the archer envelope, AoE fields zeroed — leaving `archer` and `support` untouched.
- Wire `caster` into `unitStrategicValue` and `unitTypePreference` so a caster is valued/targeted exactly as a `support` unit is — no special-casing. (`unitStrategicValue`/`unitTypePreference` read only `Name`/`Frontline`/`Backline`, so the Decision-1 deltas do not change *strategic-value or type-preference* scoring; they **do** change *target-selection* scoring via `scoreUnitTargetLocked`/`scoreBuildingTargetLocked` — that is Delta 4, intended.)
- Use `archetype` as the upgrade-eligibility boundary: a backline caster SHALL no longer inherit archer-only upgrades (Swift Strikes). This role separation is a *motivating goal* of the new archetype — the whole point of giving casters their own archetype — not a side-effect to be tolerated.
- Enumerate the gameplay deltas this introduces (kiting, enemy-scoring shift, archetype-correct upgrade separation, chase-envelope change) rather than claiming zero regression.
- Land the `AbilityCategory` enum + `AbilityDef.Category` data model (registry mirroring `DamageType`) and tag `heal` as `category:heal`, as inert groundwork for Phase 2.
- Preserve the heal-autocast *gating* (mana/cooldown/selector logic untouched) and prove the absence of profile↔cadence coupling with a no-melee scoped seeded-replay test.

**Non-Goals:**

- Ability priority scoring, `tickUnitAutoCastLocked` rework (Phase 2).
- Per-path `paths/<path>/abilities/<rank>.json` files and promotion-time grants (Phase 2).
- Any new `AbilityCategory` *reader* — nothing consumes `Category` in Phase 1.
- A caster-scoped upgrade line to replace the lost Swift Strikes eligibility (future work; the loss is accepted for Phase 1).
- Final tuning of the `caster` profile numbers — Phase 1 ships `support`-derived values with two deliberate deltas; a full balance pass is separate later work.
- Frontend/protocol changes — none; snapshot shapes are unchanged.

## Decisions

### Decision 1: `caster` is a new registered profile, `support`-derived with two intentional Phase-1 deltas

Add a `"caster"` entry to the `combatProfiles` map (combat_ai_profiles.go:3) whose value equals `"support"` **except**:

- `Name: "caster"`
- `MaxChaseDistance` set near the `archer` envelope (≈180, not `support`'s 110) — because leash self-clamps to `AttackRange` (220) but `MaxChaseDistance` does not, and a verbatim clone would materially shrink the Apprentice's pursuit range.
- `AoERadius: 0` and the `AoECluster` target weight `0` — the Apprentice's *current* kit is single-target (basic attack = the `fire_bolt` projectile; only ability = `heal`), so `support`'s AoE tuning is dead/misleading weight on it today. This is a current-kit decision, **not** a permanent design statement that casters are never AoE: the architecture intentionally accommodates future AoE caster abilities (cf. the source design's "future ability kinds"); when one lands, this profile is re-tuned rather than this delta being treated as a fixed invariant.

`archer` and `support` entries are not edited.

**Why not alias archer:** archer has no retreat — the exact bug being fixed. **Why not just point the Apprentice at `support`:** the design wants a distinct, independently-tunable profile identity for casters, and `support`'s AoE/short-chase tuning is wrong for a single-target 220-range unit. **Why a deliberate profile rather than a verbatim clone:** a verbatim clone is *not* balance-neutral (chase shrinks 180→110; dead AoE weights), so "clone = zero risk" was rejected as false; the two deltas make `caster` a coherent single-target backline caster from day one while still deferring full tuning.

*Alternative considered:* parameterised profile factory (e.g. `cloneProfile("support", overrides)`). Rejected for Phase 1 — the map is a flat literal table by convention; a factory is a larger pattern change with no Phase-1 payoff. The three deltas are written inline in the literal entry.

### Decision 2: Scoring wiring treats `caster` exactly as `support`

- `unitStrategicValue` (combat_ai_scoring.go: function ~:301, the `support`/`mage` branch ~:313): extend `profile.Name == "support" || profile.Name == "mage"` to also include `"caster"` so a caster gets the same `+2.5` backline-value bump.
- `unitTypePreference` attacker switch (combat_ai_scoring.go:382): add `"caster"` to the existing `case "enemy_archer", "support":` group so a caster picks targets with support's preferences.
- `unitTypePreference` target checks (combat_ai_scoring.go:357, :361, :365): everywhere `targetProfile.Name == "support"` gates a prioritise-the-backline bonus, add `|| targetProfile.Name == "caster"`, so archers/mages/cavalry/skirmishers prioritise killing a caster the same way they prioritise killing support.

This is the "no special-casing" choice for *value/preference* scoring. **Note:** `unitStrategicValue` and `unitTypePreference` read only `profile.Name`, `Frontline`, and `Backline` — never `MaxChaseDistance`, `AoERadius`, or `AoECluster` — so for those two functions `caster` and `support` are scoring-identical for any given unit. The Decision-1 deltas **do**, however, feed the separate target-selection scorers `scoreUnitTargetLocked` / `scoreBuildingTargetLocked` (`combat_ai_scoring.go:216,224-225` and `:265,269-271`): the `MaxChaseDistance` reach term and the `AoERadius`/`AoECluster` cluster term. That difference is intended — it *is* the chase-envelope change (proposal Delta 4) plus the deliberate removal of AoE-cluster bias from a currently single-target unit — not an unintended leak (see *Risks*).

### Decision 3: `AbilityCategory` mirrors the `DamageType` registry pattern

New file `ability_category.go` modelled 1:1 on `damage_type.go`:

- `type AbilityCategory string`; const block `AbilityCategoryHeal/BuffAlly/Summon/Offensive` = `"heal" | "buff_ally" | "summon" | "offensive"`.
- `abilityCategoryRegistry` map, `RegisterAbilityCategory` (panics on empty), `IsValidAbilityCategory`, `AbilityCategories()` (sorted).
- Empty (`""`) is the reserved "unspecified" default and is **not** a registerable/valid value, identical to `DamageType` semantics.

`AbilityDef` gets `Category AbilityCategory \`json:"category,omitempty"\`` (ability_defs.go:92 struct). Load validation mirrors ability_defs.go:232: `if def.Category != "" && !IsValidAbilityCategory(def.Category) { panic(...) }`. `heal.json` gains one additive line `"category": "heal"`.

**Why a registry not a bare enum:** consistency with `DamageType` (the established convention for extensible enums here) and it gives Phase 2 a validated seam. `RegisterDamageType` has no dynamic callers today, so this is convention-parity rather than a dynamic requirement — accepted deliberately for one consistent extensible-enum pattern across the codebase. **Why inert:** no Phase-1 code reads `Category`; this guarantees the data-model half of the change is behaviourally invisible.

### Decision 4: Implementation ordering is load-order-constrained

unit_defs.go:170-172 panics at catalog init if `apprentice.json`'s `combatProfile` is not a registered profile. Therefore the `caster` entry **must** be added to `combatProfiles` *before* `apprentice.json` is flipped. The enforced order: (1) add `caster` profile + scoring wiring, (2) add `AbilityCategory` + struct field + load validation, (3) flip `apprentice.json`, (4) add `"category":"heal"` to `heal.json`. Tasks.md sequences accordingly; tests run last as the no-regression gate for everything outside the enumerated deltas.

### Decision 5: `archetype` is flipped too — Swift Strikes role-separation is the goal, not a forfeit

Both `apprentice.json` fields (`archetype` and `combatProfile`) are flipped to `caster`. `archetype` is not load-validated, so `"caster"` is accepted; and because `"caster"` is a registered profile key, the archetype-fallback path in `resolveCombatProfile` also resolves correctly (consistency, not the primary reason — `combatProfile` is always set for the Apprentice so the fallback is not exercised).

The consequential effect is via `upgrade_apply.go`: archetype-scoped upgrades match on `unit.Archetype`. The `archer`-scoped `swift_strikes_common` (+8% attackSpeed) / `swift_strikes_rare` (+14% attackSpeed) upgrades (3 stacks each) currently apply to the Apprentice **only because it is mislabelled `archer`**, and will correctly stop applying after the flip.

**Decision: this separation is the intended outcome.** `archetype` *is* the upgrade-eligibility boundary; making casters their own archetype is precisely how you keep an archer attack-speed upgrade off a backline caster. Leaving `archetype: "archer"` while `combatProfile: "caster"` would be a role-incoherent mismatch that perpetuates the very cross-role bleed this change exists to fix. So Phase 1 flips both fields, treats the de-coupling as a *goal* (proposal Delta 3), and tests it explicitly. The absence of a replacement caster-scoped upgrade is a content gap to fill later, not a regression introduced here — a caster upgrade line is future work, out of scope.

### Decision 6: Cleric / Arch Mage inherit `caster` with no path-file edit — by being the same `UnitDef`

`catalog/units/human/apprentice/paths/cleric/cleric.json` / `.../paths/arch_mage/arch_mage.json` are rank stat-multiplier files with no `type`/`archetype`/`combatProfile` (note the per-path directory — there is no top-level `cleric.json`/`arch_mage.json`). Promotion does not swap `unit.UnitType` (it stays `"apprentice"`); it applies multipliers. `resolveCombatProfile` therefore always resolves through `apprentice.json`. The paths inherit `caster` because they *are* the Apprentice `UnitDef` plus rank multipliers — **not** via any field in the path files. (Stated precisely so an implementer does not look for a `combatProfile` field in `cleric.json` / `arch_mage.json` — there is none, and none is needed.)

## Risks / Trade-offs

The four intended gameplay deltas (kiting, enemy-scoring shift, lost Swift Strikes eligibility, chase-envelope change) are enumerated in `proposal.md` *Intended Behavioural Deltas* and are not repeated here as "risks" — they are accepted, tested outcomes. What remains are residual risks:

- **[Heal-cadence invariant could be misstated as byte-identical]** The heal autocast selector is position-gated (`lowest_hp_percentage_ally_in_range`, `castRange: match_attack_range`) and the `caster` profile deliberately moves the unit (retreat). A blanket "byte-identical heal autocast under a fixed seed" claim is therefore false the moment the Apprentice retreats — and that divergence is *correct behaviour*. → The spec asserts the accurate, narrower guarantee instead: (a) heal-autocast gating logic (mana/cooldown/selector) is untouched; (b) a *no-melee* scoped seeded-replay test proves the absence of *unintended* profile↔cadence coupling. If that scoped test ever diverges, it indicates a real unintended coupling to investigate, not a tuning artifact.
- **[Scope of the scoring-identity claim]** The `caster`==`support` scoring guarantee is *scoped to* `unitStrategicValue`/`unitTypePreference` (which do not read the Decision-1 fields). The same deltas **do** change target-selection scoring via `scoreUnitTargetLocked`/`scoreBuildingTargetLocked` (`combat_ai_scoring.go:216,224-225`/`:265,269-271`) — that is the intended chase-envelope change (Delta 4) and the deliberate single-target AoE-bias removal, not a regression. → Covered by (a) a scoring-identity test asserting `caster`==`support` strategic value & type preference for the same unit, and (b) treating the target-scorer difference as the expected manifestation of Delta 4 — explicitly **not** asserted equal between `caster` and `support`.
- **[Anchor-line drift]** Design-doc / this-doc line numbers (e.g. combat_ai_scoring.go:313) are point-in-time. → Implementer verifies the named symbol at edit time; tasks.md references symbols, not just lines.
- **[Archetype value in snapshots]** The `archetype` string carried in unit snapshots changes `"archer"→"caster"`. Verified no client code branches on that value (client mirrors the field through only). → No client change; a verification task confirms the diff touches no client files.

## Migration Plan

No data/state migration. The change is additive at the catalog and profile-table level:

- Deploy is a single binary swap; the new `caster` profile and enum are compiled in.
- Existing saved/replay state has no `caster` units until the catalog change is live; the Apprentice's `UnitType` is unchanged so no per-unit migration is needed.
- **Rollback:** reverting the `apprentice.json` two-line flip alone restores the prior `archer` profile **and** Swift Strikes eligibility (the two are coupled through `unit.Archetype` — rollback restores both together, by design), while leaving the inert `caster` profile + `AbilityCategory` enum compiled in harmlessly. Full revert removes the new file and the additive struct/scoring lines.

## Open Questions

- None blocking Phase 1. A caster-scoped upgrade line to compensate for the forfeited Swift Strikes eligibility, and the full `caster` profile balance pass (retreat distances, weights, the chosen `MaxChaseDistance`), are acknowledged future work, explicitly out of scope here.
