## Why

The Acolyte (and its promotion paths Cleric / Arch Mage) is conceptually a spellcaster, but it currently runs the `archer` combat profile, which has no retreat configured. The unit stands still while melee attackers walk into it and kill it — a backline caster that fights like a frontline archer. The longer-term goal (situational ability selection, per-path kits) needs a data seam — an `AbilityCategory` on `AbilityDef` — before any scoring can be written. Phase 1 lays both foundations: introduce a real, deliberately-tuned `caster` combat profile so the Acolyte kites instead of dying, and add the (inert) ability-category data model that Phase 2 will build scoring on top of. A third, equally deliberate motivation: `archetype` is the upgrade-eligibility boundary, so giving casters their own archetype is *how* you stop role-inappropriate upgrades (an archer attack-speed buff) from bleeding onto a backline caster. Separating which units benefit from which upgrades by role is a primary reason this archetype exists — not an incidental side-effect of it.

This is **not** a zero-behaviour-regression change. It deliberately changes how the Acolyte fights, how enemies value it, and which upgrades apply to it. The value of this spec is in enumerating those deltas precisely (see *Intended Behavioural Deltas*), not in pretending they don't exist.

It therefore **knowingly supersedes** the source design doc (`docs/design/caster_archetype.md`), whose Phase 1 is titled "zero behaviour regression" and whose QA bullet demands "heal autocast byte-identical pre/post (same seed → same cast ticks)" — both technically false once the position-gated heal selector meets the deliberate retreat motion. That source-doc wording should be corrected to this enumerated-deltas framing (per `.claude/rules/AI_RULES.md`: trust the code, flag the doc). The narrower, accurate heal guarantee is stated under *Heal Autocast: the real guarantee* below.

## What Changes

- Introduce a new `caster` combat profile, **derived from** the existing `support` profile (backline positioning + retreat) with two intentional Phase-1 deltas: `MaxChaseDistance` kept near the archer envelope, and AoE-oriented fields zeroed (the Acolyte's *current* kit — basic attack via the `fire_bolt` projectile, only ability `heal` — is single-target; the design still anticipates future AoE caster abilities, at which point the profile is re-tuned, so this is a current-kit decision not a permanent invariant). The `archer` and `support` profiles are left untouched.
- Wire `caster` into the AI's strategic-value and unit-type-preference logic so casters are valued and targeted like other backline support units.
- Flip the Acolyte catalog entry's `archetype` *and* `combatProfile` to `caster`.
- Add an extensible `AbilityCategory` string enum (`heal | buff_ally | summon | offensive`, default `""`) and a `Category` field on `AbilityDef`, mirroring the existing `AbilityType` / `DamageType` const-block + registry pattern.
- Tag the existing `heal` ability with `"category":"heal"` (additive JSON line, no behaviour change — nothing reads `Category` yet).
- **Out of scope (Phase 2):** ability priority scoring, `tickUnitAutoCastLocked` rework, per-path `paths/<path>/abilities/<rank>.json` files, promotion-time ability grants.

## Intended Behavioural Deltas

These are the deliberate, accepted gameplay changes Phase 1 ships. None is a regression to be "fixed"; each is verified and tested.

1. **The Acolyte kites.** With a real retreat configured (`RetreatDistance` / `RetreatTriggerMeleeRange` inherited from `support`), an Acolyte with a melee attacker closing now retreats instead of standing still and dying.
2. **Enemy focus-fire shifts archer→support.** Flipping the profile raises the Acolyte's `unitStrategicValue` (the support `+2.5` branch) and changes enemy `unitTypePreference` toward it from archer-target rules to support-target rules. A backline caster *should* be valued/targeted like support; this is intended, but it is a real change to enemy targeting, not a no-op.
3. **Swift Strikes is correctly separated off the caster line — by design.** `unit.Archetype` is not only the profile-resolution fallback key — it is also the match key for archetype-scoped upgrades (`upgrade_apply.go`). The live `swift_strikes_*` upgrades (`scope: archetype`, `archetype: "archer"`, 3 stacks — `swift_strikes_common` +8% / `swift_strikes_rare` +14% attack speed) currently apply to the Acolyte *because it is mislabelled `archer`*. Flipping `archetype` to `caster` removes the Acolyte / Cleric / Arch Mage from that pool — and that is **the intended outcome**, a motivating reason for the archetype split (an archer attack-speed upgrade should not buff a backline caster). It is role-correct separation, not a regression to be tolerated. No caster-scoped upgrade exists yet; authoring a caster upgrade line is future *content* work (not remediation), explicitly out of scope here.
4. **The chase envelope changes.** The caster profile keeps `MaxChaseDistance` near the archer baseline (≈180) rather than inheriting support's 110. Leash already self-clamps up to the unit's `AttackRange` (220) via `effectiveLeashDistance`, but `MaxChaseDistance` has no such clamp — a verbatim support clone would have shrunk the Acolyte's pursuit range materially. Keeping it near the archer envelope is the intended Phase-1 behaviour; final tuning is a later balance pass.

## Heal Autocast: the real guarantee

The heal autocast selector (`lowest_hp_percentage_ally_in_range`, `castRange: match_attack_range`) is **position-gated**, and the `caster` profile deliberately moves the unit (retreat). Therefore heal cast *cadence* legitimately differs once the Acolyte retreats — that is correct behaviour, not a regression. The real, testable guarantee is narrower and accurate:

- **Gating logic is untouched.** Heal autocast still obeys identical mana / cooldown / selector gating; the profile change introduces no new gate and removes none.
- **No profile↔cadence coupling when position is held constant.** In a scenario with no melee threat (the Acolyte never retreats), the set of ticks heal is auto-cast on is identical pre/post change for the same seed and inputs. This scoped seeded-replay test is a tripwire for *unintended* coupling, not a byte-identical-always claim.

## Capabilities

### New Capabilities

- `caster-combat-profile`: The `caster` combat profile (support-derived: backline + retreat, with `MaxChaseDistance` near the archer envelope and AoE fields zeroed), its registration in the combat-profile registry, its strategic-value and type-preference wiring, the Acolyte/Cleric/Arch Mage using it instead of `archer`, and the resulting upgrade-eligibility change.
- `ability-category`: The `AbilityCategory` enum and `Category` field on `AbilityDef`, its default-empty semantics, the registry mirroring `DamageType`, and the `heal` ability being tagged `category:heal`. Inert in Phase 1 — pure data-model groundwork for Phase 2 priority selection.

### Modified Capabilities

<!-- None. No OpenSpec specs exist yet (openspec/specs/ is empty); all capabilities here are new. -->

## Impact

- **Backend (Go)**: `combat_ai_profiles.go` (register `caster`, support-derived with the two Phase-1 deltas), `combat_ai_scoring.go` (add `caster` to `unitStrategicValue` support branch + `unitTypePreference` cases + `support`/`mage` target checks), `ability_defs.go` (`AbilityCategory` enum + `Category` field), `ability_category.go` (new — registry mirroring `damage_type.go`).
- **Catalog (JSON)**: `catalog/units/human/acolyte/acolyte.json` (`archetype` *and* `combatProfile` → `caster`), `catalog/abilities/heal/heal.json` (additive `"category":"heal"`).
- **Upgrade economy**: the Acolyte / Cleric / Arch Mage stop matching the `archer`-scoped `swift_strikes_*` upgrades (intended, see Delta 3). No upgrade JSON is edited.
- **No protocol / snapshot changes**: `AbilitySnapshot` and all wire shapes are unchanged. The `archetype` value carried in unit snapshots changes `"archer"→"caster"`, but no client code branches on that value (verified: client only mirrors the field through), so no client change is needed; frontend work is none for Phase 1.
- **Tests**: full server suite is the no-regression gate for everything *outside* the enumerated deltas. New tests assert the profile registration and its intended structural deltas vs `support`, the Acolyte kiting behaviour, the lost Swift Strikes eligibility, and the scoped seeded heal-autocast tripwire.
