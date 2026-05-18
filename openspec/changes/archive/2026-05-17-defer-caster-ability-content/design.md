## Context

Phase 2 of the caster archetype (archived, `openspec/changes/archive/2026-05-17-caster-archetype-phase-2/`) authored two abilities — `greater_heal`, `arcane_bolt` — and wired them as unconditional Cleric/Arch Mage promotion grants (`paths/<path>/abilities/silver.json`). These were placeholder content created to satisfy the spec's "author a kit" requirement and to make the new priority/grant/`DamageAmount` systems exercisable. They were never a deliberate ability design.

User intent (clarified via brainstorming this session) deliberately defers the acquisition model:

- `greater_heal` should be a **perk-gated *replacement* of base `heal`** — when a (TBD, undesigned) perk is present, `greater_heal` supersedes `heal`; the unit carries one heal tier at a time, each tier its own independently-tunable `AbilityDef`. The gating perk is explicitly the user's later work.
- `arcane_bolt`'s acquisition mechanism is also explicitly **TBD**.

So the shipped unconditional grants assert an unintended design. The Phase 2 *engine* is mechanism-agnostic and correct: `ability_priority.go`, `path_ability_defs.go` + `assignUnitPathAbilitiesLocked`, the `tickUnitAutoCastLocked` rework, `AbilityDef.DamageAmount`, and active `ability-category`. Only the speculative *content* is wrong.

Constraints (`.claude/rules/AI_RULES.md`): data + test changes only; no tick-loop, `*Locked`, ID-targeting, or determinism impact. Archived Phase 2 is immutable.

## Goals / Non-Goals

**Goals:**

- Stop the codebase from auto-granting `greater_heal`/`arcane_bolt` (remove the contradiction with intent).
- Keep the Phase 2 grant engine genuinely tested even with zero authored content (no silent coverage loss on a now-shipped engine).
- Preserve `greater_heal.json`/`arcane_bolt.json` as valid, dormant, clearly-labelled defs so future acquisition work has them ready.
- Make the `per-path-ability-kits` canonical spec tell the truth (mechanism shipped; content deferred).
- Durably record the deferred acquisition direction so it is not lost.

**Non-Goals:**

- Designing the gating perk or `greater_heal`→`heal` replacement mechanic (user's later work).
- Designing `arcane_bolt` acquisition.
- Any change to the Phase 2 engine or the `ability-priority-selection` / `ability-category` canonical specs.
- Editing the archived Phase 2 change.

## Decisions

### Decision 1: Remove grants, keep dormant defs

Delete `paths/cleric/abilities/silver.json`, `paths/arch_mage/abilities/silver.json`, and the now-empty `abilities/` dirs — `pathAbilityGrantsFor` then returns empty for every `(path,rank)` and `assignUnitPathAbilitiesLocked` is a no-op in practice. Keep `greater_heal.json`/`arcane_bolt.json` unchanged except an additive top-level `description` marking them dormant and recording intended acquisition. They must still parse, validate (`Category`, `DamageType`), and be resolvable by id so engine/dormant-def tests keep working. Rejected alternative: deleting the defs too — loses ready scaffolding and forces wider test rewrites for no benefit (user chose keep-dormant).

### Decision 2: Synthetic-fixture grant-engine tests

`assignUnitPathAbilitiesLocked` + the loader are now shipped engine with no real content. Drop the content-asserting tests (`PromotionGrant_ClericGetHealLine`, `PromotionGrant_ArchMageGetOffensive`, and the grant-file-dependent parts of `GrantedAbilityInSnapshot`/`MultiRankCatchupNoDuplicates`/`Idempotent`/`RNGFree`). Replace with tests that inject a **synthetic grant** at test time (populate `pathAbilityGrantsByKey[pathModifierKey(p,r)]` with a synthetic id backed by a synthetic `AbilityDef`, restored/cleaned up after) and assert ordering, append-iff-absent idempotency, multi-rank catch-up, and RNG-free determinism — independent of authored catalog content. Priority/tiebreak/`DamageAmount` tests are untouched (they set `unit.Abilities` directly; dormant defs still resolve). Rejected: "assert absence only" and "just delete" — both leave the shipped grant engine effectively untested.

### Decision 3: Spec delta is REMOVED + ADDED, not MODIFIED

The "Cleric and Arch Mage starter kits are authored" requirement does not merely change behaviour — its meaning *inverts* (from "kits authored" to "deliberately not authored; mechanism only"). The faithful OpenSpec representation is `REMOVED` (with Reason + Migration) plus an `ADDED` requirement covering the new truth (mechanism shipped + fixture-tested, no grants, acquisition deferred). All other `per-path-ability-kits` requirements (loader, deterministic idempotent grant, snapshot surfacing, `DamageAmount`-on-resolve) are untouched and still hold.

### Decision 4: Record the deferred direction in-repo + memory

Capture the deferred acquisition design (greater_heal = perk-gated `heal` replacement, perk TBD; arcane_bolt TBD) in this `design.md` (below) and in the `project_caster_archetype_design` memory, so a future session/contributor does not re-derive or accidentally re-grant.

### Decision 5: Engine and archived change are untouched

No edits to `ability_priority.go`, `path_ability_defs.go`, `tickUnitAutoCastLocked`, `AbilityDef.DamageAmount`, or any archived file. This is a content/test/spec correction only.

## Deferred Design Direction (recorded — not built here)

- **`greater_heal`**: future **perk-gated replacement** of `heal`. When a (to-be-designed) perk is present on the unit, `greater_heal` *replaces* `heal` (the unit carries exactly one heal tier; tiers are separate `AbilityDef`s so each is independently tunable). This implies a future "ability supersession on perk" mechanism — explicitly the user's later design, **not** specified or built here.
- **`arcane_bolt`**: an offensive caster ability; acquisition mechanism **TBD** (could be a path grant, a perk, a tech, etc. — undecided).
- Both defs remain dormant in the catalog as ready scaffolding; nothing references them at runtime until that design lands.

## Risks / Trade-offs

- **[Dormant defs look like dead code]** A future contributor may "clean up" the unreferenced `greater_heal`/`arcane_bolt` defs. → Mitigation: explicit dormant/deferred `description` in each JSON + memory note + this design doc.
- **[Grant-engine coverage via synthetic fixture could drift from real usage]** Synthetic grants might not mirror eventual real content shape. → The fixture exercises the exact code path (`pathAbilityGrantsFor` → `assignUnitPathAbilitiesLocked` → `unit.Abilities`); shape is `[]string` of ids regardless of which ids — representative by construction.
- **[Spec REMOVED could read as "feature cut"]** → The Reason/Migration text states the *mechanism* is retained and tested; only authored content is deferred, with the intended direction recorded.

## Migration Plan

No runtime/data migration. Removing grant files only shrinks `pathAbilityGrantsByKey` (fewer entries) — any in-flight unit simply never receives those grants (it never should have). Rollback = restore the two grant files. No protocol/version coordination.

## Open Questions

None blocking. The gating-perk design and `arcane_bolt` acquisition are intentionally open and owned by the user as later work; this change exists precisely to stop pre-empting them.
