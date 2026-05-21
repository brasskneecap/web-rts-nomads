## Why

The Cleric promotion path currently has only base Heal and no Bronze perks (its `bronze.json` is `[]`). To make Cleric a complete defensive support unit, players need (a) an explicit way to dedicate a Cleric to protecting a chosen ally — without forcing complex automatic buff-target AI — and (b) the first tier of perks that differentiate Cleric play (AoE heal, ranged-damage aura, attack-speed buff on heal, conditional mana regen). These ship together because Focus Target is the linchpin that makes Battle Prayer's recast-on-low-buff logic meaningful and Greater Heal's "force-include focus" prioritization possible.

## What Changes

- **Focus Target (player command)**: a new player-issued sticky support assignment that makes a Cleric follow and prioritize-heal a single chosen ally. Behaves like an auto-cast toggle in the UI (button highlights when active; clicking re-enters targeting; right-click clears).
- **New `OrderFocusFollow` order type**: any other order (Move, Stop, AttackMove, Hold, AttackTarget) clears focus by replacing the order. Focus is stored by ID (`Unit.FocusTargetID int`) per the project's ID-not-pointer rule and validated every tick.
- **New `FocusTargetCommandMessage` protocol message**: Go + TypeScript sides.
- **AbilityDef `TargetCount` field**: optional integer (default 1) on every `AbilityDef`. Drives whether the resolver applies the ability to one target or up to N lowest-HP-percent allies in range.
- **Cleric Bronze perks (4 new perks, all authored)**:
  - `greater_heal` — at perk grant, swaps `"heal"` for `"greater_heal"` in `Unit.Abilities`. Greater Heal's `TargetCount = 3`. Selector prefers lowest-HP-percent injured allies; when a focus target is set + valid, it is force-included.
  - `sanctuary` — passive aura. Allies inside the aura take reduced damage when `DamageSource.Kind == "projectile"`. Overlapping auras do not stack — strongest reduction wins.
  - `battle_prayer` — Heal (or Greater Heal) additionally applies a time-limited attack-speed buff to every target the cast lands on. Refreshes on recast, does not stack. When a focus target is set and its buff is below the configured recast-threshold percentage of full duration, the Cleric is permitted to cast Heal on the focus target even at full HP, refreshing the buff.
  - `mana_conduit` — passive bonus mana regen scaling with the count of injured allies (HP < MaxHP) within radius, capped at a configurable `maxAlliesCounted`.
- **Battle Prayer buff state**: stored as `BattlePrayerRemaining` + `BattlePrayerMultiplier` on `UnitPerkState` of the **healed target** (cross-unit pattern, decays in `state.go Update()` alongside `WeakenedRemaining` / `TauntRemaining`). `perkAttackSpeedBonusLocked` reads these on the target being modified, not the perk owner.
- **New post-cast hook**: `onPerkAbilityResolvedLocked(caster, def, targets)` fires after every ability resolves. Battle Prayer's case applies the buff to each target the cast hit.
- **Auto-heal fallback (no focus)**: existing `lowest_hp_percentage_ally_in_range` selector behavior preserved; updated only to honor `TargetCount` for Greater Heal.
- **Client UI**: Focus Target action button next to Heal on the action bar (auto-cast-style highlight), ally-only targeting cursor mode, focus indicator in selection HUD, optional small visual marker on the focused ally, Battle Prayer surfaced in the existing active-buffs strip.

## Capabilities

### New Capabilities

- `cleric-focus-target`: Player-issued sticky support assignment for Cleric/Apprentice — order type, ID-stored target, follow movement at configurable distance, heal prioritization on focus, clearing semantics (any new order, target death/invalid, right-click button, failed-target re-pick), new protocol message, and client UI (button toggle + targeting cursor + selection-HUD indicator + ally marker).
- `cleric-bronze-perks`: The four authored Cleric Bronze perks (`greater_heal`, `sanctuary`, `battle_prayer`, `mana_conduit`), their `PerkDef.Config` tuning surface, the runtime hooks each plugs into (post-cast resolve hook, aura mitigation, per-tick scan), and the ability-swap mechanic that turns Heal into Greater Heal when the perk is granted. Includes Battle Prayer's cross-unit buff state and recast-threshold semantics that interact with focus target.
- `ability-multi-target`: Extension of `AbilityDef` with an optional `TargetCount int` field (default 1). The resolver, when invoked with TargetCount > 1, applies the ability to up to N lowest-HP-percent valid allies within cast range, with explicit prioritization for a caster-provided "force-include" target (used by focus target). Single-target abilities are byte-identical to today.

### Modified Capabilities

<!-- None. The existing specs ability-category, ability-priority-selection, per-path-ability-kits, unit-movement remain unchanged in their requirements. -->

## Impact

**Server (Go):**
- `server/internal/game/state.go` — `Unit.FocusTargetID`, new `OrderFocusFollow` in `OrderType` enum, cross-unit decay of `BattlePrayerRemaining` in `Update()`.
- `server/internal/game/ability_defs.go` — `AbilityDef.TargetCount`, JSON load.
- `server/internal/game/ability_cast.go` — resolver branches on `TargetCount`, forwards a slice of targets to the post-cast hook.
- `server/internal/game/ability_autocast.go` — selector returns up to N targets, force-includes focus when set.
- `server/internal/game/perks.go` — `UnitPerkState.BattlePrayerRemaining`, `BattlePrayerMultiplier`, `ManaConduitAccumulator` (if dt-based), new `onPerkAbilityResolvedLocked` hook, `tickUnitPerkStateLocked` cases for `mana_conduit`, `perkAttackSpeedBonusLocked` reads battle-prayer fields, `perkPoolForRankLocked` finds the four new perks.
- `server/internal/game/perks_auras.go` — new `perkRangedDamageMultiplierFromAurasLocked(target, src)`.
- `server/internal/game/perks_icons.go` — Battle Prayer buff icon, Mana Conduit / Sanctuary passive icons.
- `server/internal/game/damage_pipeline.go` — call site for sanctuary mitigation when `src.Kind == "projectile"`.
- `server/internal/game/state_movement.go` — follow path when `Order.Type == OrderFocusFollow`.
- `server/internal/game/focus_target.go` *(new file)* — `RequestSetFocusTargetLocked`, `clearFocusTargetLocked`, per-tick validation helper.
- `server/internal/game/catalog/units/human/apprentice/paths/cleric/perks/bronze.json` — the four new perks with `Config` tuning.
- `server/internal/game/catalog/abilities/heal/heal.json` — confirm `category: "heal"` already set; no behavior change required, but add `targetCount: 1` for clarity.
- `server/internal/game/catalog/abilities/greater_heal/greater_heal.json` — `targetCount: 3`, `manaCost: 10`, `healAmount: 10`.
- `server/pkg/protocol/messages.go` — `SetFocusTargetCommandMessage`.
- `server/internal/ws/handlers.go` — dispatch for the new message.

**Client (TypeScript / Vue 3):**
- `client/src/game-portal/src/services/networkClient.ts` (or equivalent) — send `SetFocusTargetCommandMessage`.
- `client/src/game-portal/src/components/SelectionHud.vue` — focus-target indicator on selected Cleric, Battle Prayer buff in active buffs strip.
- `client/src/game-portal/src/components/ActionIcon.vue` (or the surrounding action bar component) — Focus Target action button with auto-cast-style highlight, ally-only targeting cursor mode wiring.
- New action icon entry in `catalog/action-icons.json` for `focus_target`, `perk-greater_heal`, `perk-sanctuary`, `perk-battle_prayer`, `perk-mana_conduit`.
- Game-state TypeScript types: `Unit.focusTargetId`, `OrderType` enum addition, ability snapshot's `targetCount`.

**Tests:**
- Server: focus set/clear via message, focus invalidation on target death, any-order clears focus, Greater Heal targets 3 lowest-HP injured + force-includes focus, Battle Prayer refresh-vs-stack semantics, Battle Prayer recast threshold triggers heal on full-HP focus, Sanctuary projectile-only filter, Sanctuary no-stacking (max wins), Mana Conduit per-ally regen with cap, no-focus auto-heal fallback unchanged, determinism under seed for all new perks.
- Client: Focus button highlight reflects server `focusTargetId`, right-click clears via message, ally-only cursor filter.

**No impact on:** Arch Mage perks/abilities, Cleric Silver/Gold perks, combat AI scoring, projectile system internals, existing buff/debuff stack rules, generic buff registry (we use typed fields per existing convention).
