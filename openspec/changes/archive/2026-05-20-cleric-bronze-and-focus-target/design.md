## Context

The Acolyte → Cleric promotion path currently has Heal (single-target, mana-gated, cooldowned) and no perks (its `bronze.json` is `[]`). Greater Heal exists in the catalog (`catalog/abilities/greater_heal/greater_heal.json`) but is dormant — the file's own comment marks it as "DEFERRED" and "perk-gated REPLACEMENT of base heal". This change activates that intent and lays the rest of Cleric's identity on top.

The server is the simulation authority: a Go tick loop under `s.mu` mutates `GameState`, the client (TypeScript / Vue 3) is a thin view that sends command intents and renders snapshots. CLAUDE.md mandates that combat-and-AI targets be stored by **ID**, validated every tick — never by long-lived pointer — and the perk runtime already follows a tight recipe documented at `server/internal/game/perks.go:34-68`. The auras module (`perks_auras.go`) and cross-unit debuff decays (`WeakenedRemaining` / `TauntRemaining` in `state.go Update()`) are the prior-art patterns this design mirrors.

The brainstorming session resolved several architectural questions before this design was written:

- **Focus Target lifecycle**: any new order (Move, Stop, AttackMove, Hold, AttackTarget) cancels focus. The user said: *"a regular move command should also cancel it since the idea of the focus target is they are also moving based on that."*
- **Battle Prayer scope**: the attack-speed buff applies to every target a Heal cast lands on (so Greater Heal + Battle Prayer = up to 3 buffs per cast).
- **Greater Heal mechanism**: perk-driven ability swap in `Unit.Abilities` (not an in-place modification of base Heal).
- **No generic buff registry**: typed fields on `UnitPerkState`, matching the existing `RelentlessRemaining` / `MomentumRemaining` / `WeakenedRemaining` pattern. A generic registry was explicitly rejected as out-of-scope.

## Goals / Non-Goals

**Goals:**

- Ship a player-controlled Focus Target command that makes a Cleric follow and prioritize-heal one chosen ally, with simple deterministic behavior — no inferred-intent buff AI.
- Ship the four Cleric Bronze perks (`greater_heal`, `sanctuary`, `battle_prayer`, `mana_conduit`) data-driven through `PerkDef.Config` so every tuning knob is in JSON.
- Reuse existing patterns: ID-not-pointer for focus target, typed cross-unit timer fields for Battle Prayer's buff, the existing aura module for Sanctuary, the existing `tickUnitPerkStateLocked` for Mana Conduit's per-tick scan.
- Preserve full determinism under seed (no map iteration driving outcomes, no wall-clock, no unseeded RNG).
- Preserve no-regression for units that don't own any of the new perks and don't have a focus target (the auto-heal path is byte-identical with `TargetCount = 1`).

**Non-Goals:**

- Cleric Silver / Gold perks (out of scope; they are separate future changes).
- Arch Mage perks or abilities.
- Automatic / AI-driven buff target selection. Focus Target is the only "who to support" decision and it is always made by the player.
- Generic buff registry (Unit.Buffs map). Explicitly rejected during brainstorming.
- Stacking aura behavior. Sanctuary takes the strongest reduction across overlapping auras; this matches existing aura conventions.
- Backwards-compatibility shims. Battle Prayer and Greater Heal are new — there is no migration path to preserve.
- Modifying combat AI scoring, projectile internals, or unrelated movement logic.
- Reworking the existing auto-heal selector. We only extend it to honor `TargetCount` and force-include focus.

## Decisions

### Decision 1 — Focus Target as a new `OrderType` (`OrderFocusFollow`)

`OrderType` (in `state.go:18-27`) gets a new value `OrderFocusFollow`. Setting focus mutates `unit.Order = OrderState{Type: OrderFocusFollow}` and `unit.FocusTargetID = targetID`. Any new order overwrites the entire `unit.Order` (existing pattern; no per-order-type cleanup needed) and a small clear-helper zeroes `FocusTargetID` to keep state coherent.

**Alternatives considered:**

- **Persistent-overlay field independent of `Order`** (focus survives across other orders, Move temporarily overrides then resumes). Rejected because the user explicitly chose order-replacement semantics. The overlay model also creates ambiguity around resume-after-AttackMove that we don't need to solve.
- **Hybrid (orthogonal field, Stop clears, Move temporarily overrides)**. Same reason — explicitly rejected.

**Why this wins**: matches the existing `OrderAttackTarget` precedent, is the simplest mental model, and is exactly what the user picked.

### Decision 2 — `FocusTargetID int` field on `Unit`, validated per tick

Mirrors the `AttackTargetID int` pattern in `state.go:217`. The ID is the single source of truth. No `*Unit` cached anywhere. Every tick the focus-target consumers (follow movement, heal target selection) call `s.getUnitByIDLocked(unit.FocusTargetID)` and apply the canonical guard:

```go
focus := s.getUnitByIDLocked(unit.FocusTargetID)
if focus == nil || !focus.Visible || focus.HP <= 0 || focus.OwnerID != unit.OwnerID && !s.sameTeamLocked(focus, unit) {
    s.clearFocusTargetLocked(unit)
}
```

Same-team-different-owner is allowed (team-based ally check), matching how `canAbilityTargetUnitLocked` already treats heal.

**Alternatives considered:**

- Storing `*Unit` directly — explicitly forbidden by CLAUDE.md target-reference rules.
- Storing on `UnitPerkState` rather than `Unit` — focus target is not a perk; it is a base Cleric capability (every Cleric can use it regardless of Bronze pick), so the field belongs on `Unit`.

### Decision 3 — Follow movement piggybacks on the existing path system, no new mover

When `unit.Order.Type == OrderFocusFollow` and `FocusTargetID` resolves to a valid focus, the movement tick in `state_movement.go` computes a follow destination at most `focusFollowDistance` from the focus target's current position and assigns a path with the existing `assignUnitPathWithSubBlocked` helper. The destination is re-evaluated each tick; if the focus has not moved much, the existing path remains in place (we only repath when the existing path's end-cell is farther than `focusFollowDistance + leashSlack` from the focus). This avoids re-pathing every tick (determinism + perf).

**Tuning:**

- `focusFollowDistance` (default ~64-96 px — within Cleric's heal range so we are always in range to cast when not panicking)
- `leashSlack` (default ~24 px — hysteresis to avoid stutter when target wobbles)

Both live in catalog (likely the Cleric path JSON or a new top-level `support_tuning.json`; final location chosen during implementation).

**Alternatives considered:**

- Reuse leader-follower group pathing (`state_movement.go:156-204`). Rejected — that system is for one-shot group moves with formation slots, not a dynamic-target follow.
- New full mover module. Rejected — overkill; the existing path helpers handle this in a few lines.

### Decision 4 — Greater Heal via ability-swap in `Unit.Abilities`

When the `greater_heal` perk is granted (via the existing perk pool roll at Bronze rank-up), the grant routine finds `"heal"` in `unit.Abilities` and replaces it in-place with `"greater_heal"`. The slot index is preserved (so the ability bar position is stable). Auto-cast / cooldown state for `"heal"` is migrated to `"greater_heal"` if any was set.

Both abilities live in the catalog as distinct `AbilityDef` entries with explicit JSON config. The resolver branches on `def.TargetCount`:

- `TargetCount <= 1` → existing single-target path, unchanged.
- `TargetCount > 1` → select up to N lowest-HP-percent valid allies in range, force-include `unit.FocusTargetID` if it resolves to a valid ally, apply heal + post-cast hook to each.

**Alternatives considered:**

- **Approach B from brainstorming** (perk hook modifies base Heal in place, no swap). Rejected: makes ability behavior depend on caster perks at runtime, breaks the "JSON describes the ability" model, and the catalog file's own comment already names the swap as the intended path.
- A new top-level "ability swaps" registry. Rejected — too much infrastructure for one swap.

### Decision 5 — `AbilityDef.TargetCount int` (default 1) as an extension, not a redesign

`AbilityDef` gains an optional JSON key `targetCount` (default 1 when omitted). All current `AbilityDef` entries are unchanged in behavior since they implicitly have `TargetCount == 1`. The resolver and the auto-cast selector both honor `TargetCount`; the snapshot ships it to the client so the targeting cursor can render a multi-target indicator (a future polish — not required for v1).

This is a small surface-area change that is reusable for any future AoE-style ability (Arch Mage's mass cleanse / chain lightning / etc.).

### Decision 6 — Battle Prayer buff state lives on `UnitPerkState` of the **healed target**

`UnitPerkState` is the right home because it is already the cross-unit-buff convention: `WeakenedRemaining` (Punishing Guard's debuff on the *attacker*), `TauntRemaining` (on the *taunted* unit), `MarkStacks` (on victims). Adding `BattlePrayerRemaining float64` and `BattlePrayerMultiplier float64` is one row in the recipe.

**Decay site**: `state.go Update()` per-unit loop alongside `WeakenedRemaining` decay — NOT in `tickUnitPerkStateLocked`, because the buff lives on units that may not own the perk (the perk owner is the Cleric, the buff is on the healed ally). This matches the documented cross-unit convention in `perks.go:60-65`.

**Hook**: `perkAttackSpeedBonusLocked(unit)` reads `unit.PerkState.BattlePrayerRemaining > 0` and adds `unit.PerkState.BattlePrayerMultiplier` to the attack-speed bonus accumulator. Multiplier comes from the buff-applier's `PerkDef.Config["attackSpeedMultiplier"]` and is stored on the buff so the value travels with the buff (future-proof if the perk is ever re-tuned mid-match).

**Refresh-vs-stack**: re-applying Battle Prayer onto a unit that already has the buff resets `BattlePrayerRemaining` to the configured duration and overwrites `BattlePrayerMultiplier` (longer-wins + stronger-wins, identical to existing mark-stack refresh semantics).

**Recast threshold**: when the Cleric is deciding whether to cast Heal on a focus target who is at full HP, the auto-cast selector checks `focus.PerkState.BattlePrayerRemaining < def.Config["recastThresholdPercent"] * def.Config["buffDurationSeconds"]`. If true (AND the caster owns Battle Prayer AND the focus target is the caster's focus AND mana/cooldown permit), the cast proceeds with the focus target as the (or as a force-included) recipient.

### Decision 7 — New post-cast hook: `onPerkAbilityResolvedLocked(caster, def, targets)`

Mirrors the existing `onPerkAttackDamageAppliedLocked` / `onPerkKillLocked` pattern. Called inside `resolveAbilityCastLocked` after the heal/damage/effect lands, with the slice of resolved targets (1 entry for single-target, up to N for `TargetCount > 1`). Battle Prayer is the only initial consumer; future ally-targeted perk effects use the same seam.

**Alternatives considered:**

- Inline the buff application directly in `resolveAbilityCastLocked`. Rejected — the perk system already enforces separation between ability resolution (which always runs) and perk-conditioned post-effects (which run only when the caster owns the perk). Mixing the two violates the recipe.

### Decision 8 — Sanctuary mitigation via the existing aura module

`perks_auras.go` is the home. A new helper `perkRangedDamageMultiplierFromAurasLocked(target, src) float64` returns the multiplier (≤ 1.0). The damage pipeline calls it inside the existing per-event mitigation chain in `damage_pipeline.go`, gated on `src.Kind == "projectile"`. Building-fired projectiles are tagged the same way (they go through the projectile path), so Sanctuary covers them too — desirable for "shields the formation from arrow towers" gameplay.

**Stacking rule**: when multiple eligible auras cover the target, take the strongest (lowest multiplier), do not multiply them together. This matches the existing aura-based armor bonus precedent.

**Config**: `radiusPixels`, `damageReductionPercent` (0.0–1.0).

### Decision 9 — Mana Conduit as a per-tick scan in `tickUnitPerkStateLocked`

Adds a `mana_conduit` case to `tickUnitPerkStateLocked`. The case:

1. Iterates same-team units within `radiusPixels`.
2. Counts those with `HP > 0 && Visible && HP < MaxHP`, capped at `maxAlliesCounted`.
3. Adds `bonusManaRegenPerAlly * count * dt` to `unit.CurrentMana` (clamped to `unit.MaxMana`).

Iteration uses the deterministic `s.Units` slice order; no map iteration drives outcomes.

**Alternatives considered:**

- On-damage-taken hook (only update mana when an ally drops below full). Rejected — coupling mana regen to damage events makes the regen feel laggy; per-tick is simpler and equally cheap at Cleric-sized neighborhood counts.
- Aura-based (mana regen aura that scales with neighbors). Rejected — the aura module is about modifiers *received by* units inside the aura; Mana Conduit is a *self-effect* keyed on neighbors, semantically inverted.

### Decision 10 — New protocol message `SetFocusTargetCommandMessage`

Wire shape:

```go
type SetFocusTargetCommandMessage struct {
    Type         string `json:"type"` // "set_focus_target_command"
    CasterUnitID int    `json:"casterUnitId"`
    TargetUnitID int    `json:"targetUnitId"` // 0 = clear focus
}
```

Mirrors `CastAbilityCommandMessage`. Server handler validates match membership, then calls `match.State.RequestSetFocusTargetLocked(playerID, casterUnitID, targetUnitID)`. Validation failures (wrong owner, dead caster, invalid target) reply with `NotificationMessage` (same precedent as `cast_ability_command`).

**Why a dedicated message, not piggybacking on `cast_ability_command`**: focus target is not a cast (no mana, no cooldown, no cast time, no animation). Reusing the cast message would require special-casing on both sides; a clean separate message is cleaner.

### Decision 11 — Client action button uses the existing ability-button component with an auto-cast highlight

The Focus Target button is rendered by the same component that renders other ability buttons, with `actionId: "focus_target"`. The button binding sends `SetFocusTargetCommandMessage` (not `CastAbilityCommandMessage`). The button's "active" highlight is driven by `unit.focusTargetId != 0` from the snapshot — identical to how auto-cast toggles render today. Right-click on the button sends a clear (`targetUnitId: 0`). Click → enter ally-only targeting cursor → click ally → server set; cancel/invalid-target click → send clear.

The selection HUD adds a single line: `Focusing: <unitName> (<currentHP>/<maxHP>)` when `selected.focusTargetId != 0`. The focus target's portrait highlight on the minimap is a stretch goal (tagged optional in the spec).

### Decision 12 — Cleric Bronze perks all roll from a single pool

The existing perk pool mechanic in `perks.go:perkPoolForRankLocked` already handles this — we simply author all four perks in `catalog/units/human/acolyte/paths/cleric/perks/bronze.json` and rank-up uses the unchanged RNG roll. A Cleric reaching Bronze rank gets exactly one of the four. No exclusion / requires-perk gating between them at Bronze tier (Silver/Gold will gate via `requiresPerk` later, out of scope here).

## Risks / Trade-offs

[**Risk**] Per-tick neighbor scan in Mana Conduit could be expensive at scale → **Mitigation**: capped at `maxAlliesCounted` (default ≤ 5). Cleric count is small in any realistic match; the inner loop is `O(allies_in_radius)` not `O(units)` once we early-out by squared-distance check. Re-uses the same squared-distance technique already in `last_stand` (`perks.go:773-789`).

[**Risk**] Focus target jitters when its target's path stutters → **Mitigation**: `leashSlack` hysteresis. Cleric only repaths when the target moves more than the slack from the current path's end-cell. No per-tick repath.

[**Risk**] Greater Heal's force-include focus might overshoot range when focus is at full HP but other allies are dying → **Mitigation**: spec the selection rule explicitly. Focus is force-included **only** when (a) focus is injured, or (b) Battle Prayer's recast threshold is triggered on the focus. Otherwise focus is treated as a normal candidate — if it's at full HP it gets ranked last and naturally excluded when other allies are competing for the 3 slots.

[**Risk**] Battle Prayer buff lives on `UnitPerkState` of units that may not have any perks themselves → **Mitigation**: this is already the established cross-unit-debuff pattern. `UnitPerkState` is allocated on every Unit at spawn (it has no perk-presence gate). No structural change needed.

[**Risk**] Sanctuary's projectile filter relies on `DamageSource.Kind == "projectile"` correctness → **Mitigation**: audit the call sites. Buildings firing arrows already use the projectile path. Trap damage uses `Kind == "trap"` (not "projectile"), which is intentional — Sanctuary should not shield against traps (different damage character; tuning may revisit this in a future change).

[**Risk**] Player issues Move on a Cleric repeatedly while micromanaging → focus keeps clearing → frustrating UX → **Mitigation**: documented behavior; the auto-cast-style button highlight makes it visually obvious that focus is gone. If players ask for shift-queue or a "follow without clearing" modifier later, we add it as a separate change rather than designing for hypothetical demand now.

[**Risk**] Battle Prayer recast threshold interacts with `lastCastFailure` UX → **Mitigation**: the recast-threshold logic only changes target *selection* (it makes the focus target eligible at full HP); standard mana/cooldown failure paths are unchanged.

[**Risk**] Determinism in multi-target heal — if we pick "lowest HP%" and two allies are tied, slot order must drive selection → **Mitigation**: sort by `(hp_percent_asc, unit.ID_asc)` to break ties deterministically. Same convention as existing autocast selectors.

[**Risk**] The `focus_target` action button on the client could be mistaken for an auto-cast toggle on Heal → **Mitigation**: distinct icon and tooltip; the button highlight color is the same auto-cast color but the icon is unique (focus reticle).
