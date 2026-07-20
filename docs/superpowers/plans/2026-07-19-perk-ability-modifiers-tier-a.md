# Data-Driven Perk→Ability Scalar Modifiers (Tier A) — Siphoner Pilot

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`).

**Goal:** Introduce a generic, data-driven way for a perk to scalar-modify an ability (damage/heal/mana/range/…), replacing the bespoke per-ability modifier hooks. Pilot it on the Siphoner: migrate `soul_leech` + `beam_mastery` (which scale `siphon_life`) from hardcoded Go into `abilityModifiers` data, generalizing the existing `siphonLifeChannelModifiers` aggregation.

**Architecture:** A new `PerkDef.AbilityModifiers []AbilityModifier` field (target ability id + scalar mults). One generic aggregator `abilityScalarModifiersForCasterLocked(caster, abilityID)` composes all owned perks' modifiers for an ability (multiplicatively). The `siphon_life` channel read-points read that generic struct instead of the siphon-specific function. Behavior is provably identical — the golden test `ability_compile_golden_channel_perks_test.go` pins soul_leech/beam_mastery scaling.

**Tech Stack:** Go (`internal/game`). Gate on `go build ./... && go vet ./... && go test ./internal/game/`. Run from `server/`. **No git commit/add** — human stages.

**Behavior invariant:** `siphon_life` damage/heal/mana/range under soul_leech and/or beam_mastery is byte-identical before and after. `TestGoldenChannelPerks` (or whatever `ability_compile_golden_channel_perks_test.go` names it) is the guard.

---

## Reference facts (verified)

- `siphon_life` is a schemaVersion-2 ability: `on_cast_complete → beam{channeled, damagePerTick:6, manaCostPerTick:1, healingMultiplier:1, allyHealRadius:220, on_beam_tick → deal_damage}` ([catalog/abilities/siphon_life/siphon_life.json]).
- Current scalar aggregation: `siphonLifeChannelModifiersForCasterLocked(caster) SiphonLifeChannelModifiers{DamageMult,HealMult,ManaCostMult,RangeMult}` — [perks_siphoner.go:101](server/internal/game/perks_siphoner.go#L101). Scans `caster.PerkIDs`, switches on `soul_leech` (config keys `damageMultiplier`,`healingMultiplier`) and `beam_mastery` (`damageMultiplier`,`healingMultiplier`,`manaCostMultiplier`,`rangeMultiplier`), multiplying each present (`>0`) value.
- Read-points ([ability_channel.go]): `:440` `mods := siphonLifeChannelModifiersForCasterLocked(unit)`; `:441` mana `×mods.ManaCostMult`; `:465/467` damage `×mods.DamageMult` (folded via `ctx.effectiveDamageMultiplier` in `fireChannelBeamTickLocked`); `:492` heal `×mods.HealMult`; range via `channelRangeMultiplierForCasterLocked` ([:569](server/internal/game/ability_channel.go#L569)) → `mods.RangeMult`.
- `beam_mastery` ALSO adds a chain_siphon target (handled in `chainSiphonEffectiveConfigLocked`, NOT part of the scalar channel modifiers) — leave that untouched; only its 4 scalar mults migrate.
- Golden guard: `ability_compile_golden_channel_perks_test.go` runs soul_leech/beam_mastery/combined and asserts tick damage `= round(DamagePerTick × DamageMult)` etc. against `siphonLifeChannelModifiersForCasterLocked`.

---

## Task 1: `AbilityModifier` schema + generic aggregator (additive — nothing reads it yet)

**Files:** `perk_defs.go` (struct+field), a new `ability_modifiers.go` (aggregator), `perk_defs.go`/validation, test `ability_modifiers_test.go`.

- [ ] **Step 1: Define the data types.** In `perk_defs.go`, add:
```go
// AbilityModifier is a scalar modification a perk applies to a target ability
// (by id today; ability tags are the planned extension). A zero-valued mult
// means "unset" (identity 1.0) — perks only set the fields they change.
type AbilityModifier struct {
	Target       string  `json:"target"`                 // ability id
	DamageMult   float64 `json:"damageMult,omitempty"`
	HealMult     float64 `json:"healMult,omitempty"`
	ManaCostMult float64 `json:"manaCostMult,omitempty"`
	RangeMult    float64 `json:"rangeMult,omitempty"`
	CooldownMult float64 `json:"cooldownMult,omitempty"` // reserved (traps/others later)
	RadiusMult   float64 `json:"radiusMult,omitempty"`   // reserved
	DurationMult float64 `json:"durationMult,omitempty"` // reserved
}
```
Add `AbilityModifiers []AbilityModifier \`json:"abilityModifiers,omitempty"\`` to `PerkDef`.

- [ ] **Step 2: Define the aggregated result + aggregator.** New file `ability_modifiers.go`:
```go
// AbilityModifierSet is the composed (multiplied) scalar modifiers a caster's
// perks apply to one ability. All fields default to 1.0 (no-op).
type AbilityModifierSet struct {
	DamageMult, HealMult, ManaCostMult, RangeMult, CooldownMult, RadiusMult, DurationMult float64
}

func identityAbilityModifierSet() AbilityModifierSet {
	return AbilityModifierSet{1, 1, 1, 1, 1, 1, 1}
}

// abilityScalarModifiersForCasterLocked composes every owned perk's
// AbilityModifiers entry that targets abilityID, multiplicatively. A modifier
// field <= 0 is treated as unset (identity) — matching the old
// siphonLifeChannelModifiers "if m > 0 { mult *= m }" convention. Safe on nil.
// Caller holds s.mu (read or write).
func (s *GameState) abilityScalarModifiersForCasterLocked(caster *Unit, abilityID string) AbilityModifierSet {
	set := identityAbilityModifierSet()
	if caster == nil || abilityID == "" {
		return set
	}
	for _, perkID := range caster.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		for i := range def.AbilityModifiers {
			m := def.AbilityModifiers[i]
			if m.Target != abilityID {
				continue
			}
			if m.DamageMult > 0 { set.DamageMult *= m.DamageMult }
			if m.HealMult > 0 { set.HealMult *= m.HealMult }
			if m.ManaCostMult > 0 { set.ManaCostMult *= m.ManaCostMult }
			if m.RangeMult > 0 { set.RangeMult *= m.RangeMult }
			if m.CooldownMult > 0 { set.CooldownMult *= m.CooldownMult }
			if m.RadiusMult > 0 { set.RadiusMult *= m.RadiusMult }
			if m.DurationMult > 0 { set.DurationMult *= m.DurationMult }
		}
	}
	return set
}
```

- [ ] **Step 3: Validation.** In `validatePerkDef` (perk_defs.go), reject an `AbilityModifier` with empty `Target`. (Optionally warn on a target that doesn't resolve to a registered AbilityDef — but keep it non-fatal / skip if it complicates load ordering; the aggregator already no-ops on a non-matching target.)

- [ ] **Step 4: Unit test** `ability_modifiers_test.go`: construct a `GameState` + a caster with synthetic perks carrying `AbilityModifiers` and assert `abilityScalarModifiersForCasterLocked` composes correctly — e.g. one perk `{target:"x", damageMult:2}` and another `{target:"x", damageMult:1.5, healMult:2}` → DamageMult 3.0, HealMult 2.0, others 1.0; a modifier targeting a different ability is ignored; empty caster → identity. (Use a test perk registered via the overlay or a direct `PerkDef` if the aggregator can read it — match how other perk tests inject defs.)

- [ ] **Step 5: Verify.** `cd server && go build ./... && go vet ./... && go test ./internal/game/ -run TestAbilityModifiers -v` → PASS. Full `go test ./internal/game/` → PASS (nothing reads the field in production yet — behavior-neutral).

- [ ] **Step 6: Commit** (human).

## Task 2: Migrate soul_leech + beam_mastery to data; wire read-points; delete the bespoke aggregator

**Files:** `catalog/perks/siphoner/{soul_leech,beam_mastery}/*.json`, `ability_channel.go`, `perks_siphoner.go`, tests.

- [ ] **Step 1: Author `abilityModifiers` on the two perks.** Read each perk's current `config` values and add the equivalent `abilityModifiers`:
  - `soul_leech.json`: `"abilityModifiers": [{ "target": "siphon_life", "damageMult": <config.damageMultiplier>, "healMult": <config.healingMultiplier> }]`.
  - `beam_mastery.json`: `"abilityModifiers": [{ "target": "siphon_life", "damageMult": <..>, "healMult": <..>, "manaCostMult": <config.manaCostMultiplier>, "rangeMult": <config.rangeMultiplier> }]`.
  Read the EXACT current values from the JSON (do not guess). Keep the existing `config` keys in place if the perk's `tooltipTemplate` interpolates them (check — if the tooltip reads e.g. `{damageMultiplier+%}`, leave config; the duplication is acceptable for now). If beam_mastery has ConfigByRank affecting these, replicate per-rank via… (NOTE: check — if these mults are rank-scaled via `ConfigForRank`, the flat `abilityModifiers` can't express per-rank yet; if so, STOP and report DONE_WITH_CONCERNS so we design per-rank modifiers before cutover. Verify soul_leech/beam_mastery have NO configByRank on the migrated keys — grep.)

- [ ] **Step 2: Wire the read-points to the generic aggregator.** In `ability_channel.go`, replace `mods := s.siphonLifeChannelModifiersForCasterLocked(unit)` (:440) with `mods := s.abilityScalarModifiersForCasterLocked(unit, def.ID)`. The field names match (`DamageMult`/`HealMult`/`ManaCostMult`/`RangeMult`), so `:441/:465/:467/:492` are unchanged. In `channelRangeMultiplierForCasterLocked` (:569), replace the siphon-specific call with `s.abilityScalarModifiersForCasterLocked(caster, def.ID).RangeMult` (drop the `def.ID == "siphon_life"` special-case — it now works for any ability; a plain ability with no modifiers returns 1.0).

- [ ] **Step 3: Delete the bespoke aggregator.** Remove `siphonLifeChannelModifiersForCasterLocked`, `siphonLifeModifiersForCasterLocked` (the legacy 2-return shim), `SiphonLifeChannelModifiers`, `defaultSiphonLifeChannelModifiers` from `perks_siphoner.go` (and its doc block). Follow the compiler for any other caller (grep `siphonLife.*Modifiers`); repoint each to `abilityScalarModifiersForCasterLocked`.

- [ ] **Step 4: Update tests referencing the deleted symbols.** `ability_compile_golden_channel_perks_test.go` computes `wantFirstTickDamage` via `sLegacy.siphonLifeChannelModifiersForCasterLocked(casterL)` — repoint it to `s.abilityScalarModifiersForCasterLocked(casterL, "siphon_life")` (same struct field `.DamageMult`). The test's ASSERTION (executor tick damage == round(DamagePerTick × DamageMult)) is unchanged and is the behavior proof. Fix any `ability_exec_perk_parity_test.go` references similarly.

- [ ] **Step 5: Verify (behavior preserved).** `cd server && go build ./... && go vet ./...` → clean. `go test ./internal/game/ -run 'GoldenChannel|Siphon|AbilityModifiers' -v` → **PASS** — the golden test passing proves soul_leech/beam_mastery scaling is byte-identical through the data-driven path. Full `go test ./internal/game/` → PASS. Grep `siphonLifeChannelModifiers` → gone.

- [ ] **Step 6: Commit** (human).

---

## Self-Review

- The golden channel-perks test passes throughout → soul_leech/beam_mastery scaling is provably unchanged.
- `abilityScalarModifiersForCasterLocked` is generic (any ability id, any perk's `abilityModifiers`) and multiplicative, matching the old `>0 → *=` convention.
- Only the SCALAR part of beam_mastery migrates; its chain_siphon-target tweak stays in `chainSiphonEffectiveConfigLocked`.
- Rank-scaling caveat checked in 2.1 — if a migrated mult is rank-scaled, we stop and design per-rank modifiers first.
- Bespoke siphon modifier symbols fully deleted; no other caller left (grep clean).

## Not in this plan (next steps, flagged)
- **Tier B — composable riders:** `chain_siphon` / `shared_suffering` / `withering_beam` / `dark_renewal` as program fragments grafted onto `siphon_life`'s `on_beam_tick`. Separate initiative.
- **Ability tags** for family-targeting (`target: "tag:trap"`).
- **Broader read-points:** cooldown/radius/duration modifiers wired into non-channel abilities (the reserved fields) — additive when a consumer needs them.
- **Trapper migration** resumes once Tier A + Tier B exist.
