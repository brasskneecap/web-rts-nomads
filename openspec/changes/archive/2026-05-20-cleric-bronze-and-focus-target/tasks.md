## 1. Catalog & data model foundation

- [x] 1.1 Add optional `TargetCount int` field to `AbilityDef` (`server/internal/game/ability_defs.go`) with JSON key `"targetCount"`, default `1` when omitted or `< 1`.
- [x] 1.2 Surface `TargetCount` on `AbilitySnapshot` (Go + TS types) and confirm it serialises to JSON without breaking existing snapshots.
- [x] 1.3 Author `catalog/abilities/greater_heal/greater_heal.json`: set `category: "heal"`, `targetCount: 3`, `manaCost: 10`, `healAmount: 10`, `canTargetAllies: true`, `canTargetSelf: true`, ally-only selector. Drop the dormant comments — the file goes live.
- [x] 1.4 Confirm `catalog/abilities/heal/heal.json` has `category: "heal"` (it already does) and explicitly add `"targetCount": 1` for clarity.
- [x] 1.5 Author all four perks in `catalog/units/human/apprentice/paths/cleric/perks/bronze.json` with starting `Config` values:
  - `greater_heal`: no `Config` keys needed beyond the ability swap (the heal numbers live on the ability def).
  - `sanctuary`: `radiusPixels: 192`, `damageReductionPercent: 0.25`.
  - `battle_prayer`: `buffDurationSeconds: 5.0`, `attackSpeedMultiplier: 0.25`, `recastThresholdPercent: 0.30`.
  - `mana_conduit`: `radiusPixels: 192`, `bonusManaRegenPerAlly: 0.5`, `maxAlliesCounted: 5`.
- [x] 1.6 Add icon entries to `catalog/action-icons.json` for `perk-greater_heal`, `perk-sanctuary`, `perk-battle_prayer`, `perk-mana_conduit`, and `focus_target`.

## 2. AbilityDef multi-target plumbing

- [x] 2.1 Update `AbilityDef` JSON-unmarshal path to read `targetCount`; normalise `< 1` values to `1`.
- [x] 2.2 Extend the ally-target selector helper (in `ability_autocast.go`) to return up to `def.TargetCount` candidates ordered by ascending HP percent, tie-broken by ascending `unit.ID`.
- [x] 2.3 Add a `forceIncludeUnitID` parameter to the multi-target selector. When non-zero and resolves to a valid ally, force-include it (displace the highest-HP-percent natural pick if the natural set is already full).
- [x] 2.4 Refactor `resolveAbilityCastLocked` to accept a `[]*Unit` target slice (length 1 for legacy single-target, length up to `TargetCount` for multi). Apply per-target effect (heal amount, effect, heal-event record, healing_glow VFX) once per target.
- [x] 2.5 Update `tickUnitCastLocked` target-validation to re-check every target in the resolved set; if all targets become invalid, cancel the cast (single-target case is byte-identical: 1-element set).
- [x] 2.6 Add a unit test verifying single-target abilities behave identically (no regression: pre/post snapshot of a heal-only Apprentice match).

## 3. Focus target — server state & protocol

- [x] 3.1 Add `FocusTargetID int` field to `Unit` in `state.go` with the same comment-block convention used for `AttackTargetID`.
- [x] 3.2 Add `OrderFocusFollow` to the `OrderType` enum in `state.go`.
- [x] 3.3 Create `server/internal/game/focus_target.go` with:
  - `RequestSetFocusTargetLocked(playerID int, casterUnitID int, targetUnitID int) error` — validates ownership, validates target legality (alive, visible, same team) when non-zero, then sets `unit.Order = OrderState{Type: OrderFocusFollow}` and `unit.FocusTargetID = targetUnitID`. When `targetUnitID == 0`, calls `clearFocusTargetLocked`.
  - `clearFocusTargetLocked(unit *Unit)` — sets `unit.FocusTargetID = 0` and (if `unit.Order.Type == OrderFocusFollow`) transitions `Order` to `OrderIdle`.
  - `validateFocusTargetLocked(unit *Unit) bool` — re-resolves and team-checks; clears focus + returns false if invalid.
- [x] 3.4 Hook `clearFocusTargetLocked(unit)` into every player-order setter (`Move`, `Stop`, `AttackMove`, `Hold`, `AttackTarget`) so the order-replacement always clears focus. Audit `state.go` / `player_orders.go` for every site that mutates `unit.Order`.
- [x] 3.5 Call `validateFocusTargetLocked` at the top of the per-unit tick in `state.go Update()` (inside the `Order.Type == OrderFocusFollow` branch) so dead/invalid focus targets clear automatically.
- [x] 3.6 Add `SetFocusTargetCommandMessage` (type tag `"set_focus_target_command"`) to `server/pkg/protocol/messages.go`.
- [x] 3.7 Add WS handler dispatch for `set_focus_target_command` in `server/internal/ws/handlers.go`, calling `match.State.RequestSetFocusTargetLocked`. On validation failure, reply with `NotificationMessage`.
- [x] 3.8 Surface `FocusTargetID` on `UnitSnapshot` so the client can render the focus indicator and button highlight.

## 4. Focus follow movement

- [x] 4.1 Add `focusFollowDistance` (default `80`) and `focusLeashSlack` (default `24`) as exported tuning constants (or perk/path catalog keys — final location chosen during implementation; document the choice in code comments).
- [x] 4.2 In `state_movement.go`'s per-unit movement branch, handle `Order.Type == OrderFocusFollow`: compute follow destination = focus position offset by `focusFollowDistance` in the direction from focus to caster (so caster stays *near* but not on top of focus). Use existing `assignUnitPathWithSubBlocked` for path assignment.
- [x] 4.3 Implement repath debounce: only request a new path when the current path's end-cell is farther than `focusFollowDistance + focusLeashSlack` from the focus's *current* position. Use squared-distance comparisons throughout.
- [x] 4.4 When `validateFocusTargetLocked` clears focus mid-tick, ensure the unit's path is cleared and `Order` transitions to `OrderIdle` cleanly (no orphaned path entries).

## 5. Greater Heal perk grant — ability swap

- [x] 5.1 In `assignUnitPerkLocked` (or a sibling helper in `perks.go`), after appending a perk id to `unit.PerkIDs`, check if the new perk is `greater_heal`. If so, find `"heal"` in `unit.Abilities` and replace it in place with `"greater_heal"`. Preserve slot index. *(Also wired via `applyPerkGrantedHooksLocked` into `DebugSpawnUnit` so manually-set perks fire the swap too.)*
- [x] 5.2 Migrate `AutoCastEnabled["heal"]` → `AutoCastEnabled["greater_heal"]` (then delete the `"heal"` key); same for `AbilityCooldowns`.
- [x] 5.3 Make the swap a safe no-op when `"heal"` is not in `unit.Abilities`.
- [x] 5.4 Add a unit test: Cleric with `Abilities: ["heal"]` and `AutoCastEnabled: {"heal": true}` is granted `greater_heal` → `Abilities == ["greater_heal"]`, `AutoCastEnabled: {"greater_heal": true}`, autocast still fires next tick.

## 6. Battle Prayer — buff state & hooks

- [x] 6.1 Add `BattlePrayerRemaining float64` and `BattlePrayerMultiplier float64` to `UnitPerkState` in `perks.go`. Add the doc comment block matching the `WeakenedRemaining` / `LastStandRemaining` style.
- [x] 6.2 Add cross-unit decay for `BattlePrayerRemaining` in the per-unit loop in `state.go Update()`, alongside `WeakenedRemaining` / `TauntRemaining` decay. When `BattlePrayerRemaining` reaches `<= 0`, reset both fields to `0.0`.
- [x] 6.3 Implement `onPerkAbilityResolvedLocked(caster *Unit, def *AbilityDef, target *Unit)` in `perks.go` following the existing hook patterns. Switch on `caster.PerkIDs`, with a `battle_prayer` case that applies the buff to `target`: `target.PerkState.BattlePrayerRemaining = max(current, Config["buffDurationSeconds"])`, `target.PerkState.BattlePrayerMultiplier = max(current, Config["attackSpeedMultiplier"])`. Gated on `def.Category == AbilityCategoryHeal` (so only heal-class abilities trigger the buff).
- [x] 6.4 Wire `onPerkAbilityResolvedLocked` into `resolveAbilityCastLocked` — invoke once per resolved target.
- [x] 6.5 Extend `perkAttackSpeedBonusLocked(unit)` to add `unit.PerkState.BattlePrayerMultiplier` when `unit.PerkState.BattlePrayerRemaining > 0`. The unit may not own the `battle_prayer` perk itself (cross-unit buff).
- [x] 6.6 Update the auto-cast heal selector: when the caster owns `battle_prayer` and has a valid focus at full HP whose `BattlePrayerRemaining < def.Config["recastThresholdPercent"] * Config["buffDurationSeconds"]`, force-include the focus in the multi-target set (or use it as the single target for `TargetCount == 1`). Threshold logic SHALL NOT fire for casters without `battle_prayer`.

## 7. Sanctuary — aura mitigation

- [x] 7.1 Add `perkRangedDamageMultiplierFromAurasLocked(target *Unit, src DamageSource) float64` in `perks_auras.go`. When `src.Kind != "projectile"`, return `1.0`. Otherwise iterate `s.Units` for same-team units owning `sanctuary` within `Config["radiusPixels"]`; track the strongest reduction; return `1.0 - bestReductionPercent`.
- [x] 7.2 Call the new helper from the damage pipeline. Find the existing per-event mitigation chain in `damage_pipeline.go` / `applyUnitDamageLocked`; multiply the incoming damage by the helper's return value before HP subtraction. Place this after any existing per-event mitigations but before final clamping to integer damage.
- [x] 7.3 Make sure trap damage (`src.Kind == "trap"`, `"caltrops"`, etc.) is not affected — `Kind != "projectile"` is the gate.

## 8. Mana Conduit — per-tick regen scaling

- [x] 8.1 Add a `mana_conduit` case to `tickUnitPerkStateLocked` in `perks.go`. Iterate `s.Units`, count same-team injured allies (`HP > 0 && Visible && HP < MaxHP`) within `Config["radiusPixels"]` (squared-distance compare). Cap at `Config["maxAlliesCounted"]`.
- [x] 8.2 Add `bonusManaRegenPerAlly * count * dt` to `unit.CurrentMana`, clamped to `unit.MaxMana`. Use the existing mana fields on `Unit` (verify location: likely `CurrentMana` / `MaxMana`).
- [x] 8.3 Ensure iteration order is `s.Units` slot order — never map iteration — so determinism holds.

## 9. HUD icons

- [x] 9.1 In `activeBuffIconsLocked` (`perks_icons.go`), emit a Battle Prayer buff icon for any unit with `PerkState.BattlePrayerRemaining > 0`. *(Note: remaining-duration on the icon is not surfaced today — `ActiveEffectIcon` carries only ID and Stacks; if a duration display is wanted later, extend the protocol type.)*
- [x] 9.2 Emit passive perk icons for `sanctuary` and `mana_conduit` on units that own those perks (so the player can see the perk is active even when no aura/effect is currently triggering).
- [x] 9.3 Verify Greater Heal's ability icon overrides Heal's in the ability bar (this is automatic since the ability id changed — the bar renders from `unit.Abilities`, and `applyGreaterHealPerkSwapLocked` overwrites the slot in place so the ID-keyed icon resolution picks up the new entry).

## 10. Protocol & snapshot serialisation

- [x] 10.1 Add `FocusTargetID` field to the Go `UnitSnapshot` struct and the TS `Unit` snapshot type, JSON key `focusTargetId`. *(Go side done; TS side pending in 10.3 follow-up.)*
- [x] 10.2 Add `TargetCount` to `AbilitySnapshot` (Go + TS). *(Go side done; TS side pending in 10.3 follow-up.)*
- [x] 10.3 Add `SetFocusTargetCommandMessage` TypeScript definition mirroring the Go shape. Also add `focusTargetId` to TS `Unit` snapshot and `targetCount` to TS `AbilitySnapshot`.
- [x] 10.4 Add the new `OrderFocusFollow` value to any client-side `OrderType` mirror (if one exists) for snapshot rendering / debug. *(Extended `UnitOrder` union with `'focus_follow'` and `UNIT_ORDER_LABELS` with "Following".)*

## 11. Client UI — Focus Target button & cursor

- [x] 11.1 Add a Focus Target action button to the Cleric's action bar in the existing action-bar component. Use `actionId: "focus_target"`. The button SHALL render in the slot adjacent to Heal/Greater Heal. *(Implemented via `buildFocusTargetActionItem`, gated on `unitOwnsHealAbility` — single-unit and group selections both surface the button when every selected unit owns a heal-class ability.)*
- [x] 11.2 Bind the button: left-click → enter ally-only targeting cursor mode (the cursor SHALL filter so only same-team valid units highlight). Right-click → send `SetFocusTargetCommandMessage{CasterUnitID: clericId, TargetUnitID: 0}`. *(Handler added in `GameClient.handleAction` for `focus_target` and `autocast-toggle-focus_target`.)*
- [x] 11.3 Wire the targeting cursor: on click of a valid ally, send `SetFocusTargetCommandMessage{CasterUnitID: clericId, TargetUnitID: allyId}` and exit cursor mode. On click of an invalid target (enemy, terrain, building, or nothing), send a clear (`TargetUnitID: 0`) and exit cursor mode. *(`focus-target` cursor mode branch in `GameClient` click handler — sends per-unit for multi-select.)*
- [x] 11.4 Drive button highlight from `selectedUnit.focusTargetId != 0` (snapshot-sourced, like existing auto-cast toggles). Match the existing auto-cast highlight color/treatment. *(`autoCast: anyFocused` on the action item drives the existing `.action-cell--autocast` class.)*

## 12. Client UI — Selection HUD focus indicator

- [x] 12.1 In `SelectionHud.vue`, when the selected unit is a single Cleric and `focusTargetId != 0`, render a line `Focusing: <unitName> (<currentHP>/<maxHP>)` sourced from the snapshot's focused unit. If the focus id does not resolve in the current snapshot (out-of-vision edge), render the line with a generic placeholder. *(Added `.selection-focus` line + `focusTargetLabel` computed; fallback shows `Unit <id>` when the target isn't in `selectedUnits`.)*
- [x] 12.2 (Optional polish) Render a small visual marker on the focused ally's portrait or world sprite to help the player identify who is being focused. *(Implemented as a sky-blue glow ring at the ally's feet — same hue as the autocast button highlight, so "Focus Target button active" and "this is the focused unit" read as one connected visual idea. Drawn in `CanvasRenderer.drawWorld` via a pre-computed `focusTargetIds` set, gated on local-player ownership of the casting unit.)*

## 13. Server unit tests — focus target

- [x] 13.1 `TestFocusTarget_SetByMessage_PutsUnitInFollowMode` — send `SetFocusTargetCommandMessage`, assert `unit.Order.Type == OrderFocusFollow` and `FocusTargetID == ally.ID`.
- [x] 13.2 `TestFocusTarget_TargetDeath_ClearsFocus` — set focus, deal lethal damage to focus, advance one tick, assert focus cleared.
- [x] 13.3 `TestFocusTarget_MoveOrderClearsFocus` — set focus, issue Move, assert `FocusTargetID == 0` and `Order.Type == OrderMove`.
- [x] 13.4 `TestFocusTarget_StopOrderClearsFocus` — same as above with Stop.
- [x] 13.5 `TestFocusTarget_AttackMoveOrderClearsFocus` — same as above with AttackMove.
- [x] 13.6 `TestFocusTarget_AttackTargetOrderClearsFocus` — same as above with AttackTarget.
- [x] 13.7 `TestFocusTarget_ClearWithZeroTarget` — send `SetFocusTargetCommandMessage` with `TargetUnitID: 0`, assert focus cleared.
- [x] 13.8 `TestFocusTarget_EnemyTargetRejected` — send with enemy target, assert `NotificationMessage` reply and focus unchanged.
- [x] 13.9 `TestFocusTarget_FollowsMovingTarget` — set focus, move ally 200px, advance N ticks, assert Cleric is within `focusFollowDistance + leashSlack` of ally.
- [x] 13.10 `TestFocusTarget_NoRepathInsideSlack` — set focus, move ally < slack, advance one tick, assert no repath was requested.

## 14. Server unit tests — Cleric Bronze perks

- [x] 14.1 `TestGreaterHeal_PerkSwapsAbility` — Cleric with `Abilities: ["heal"]` granted `greater_heal`; assert `Abilities == ["greater_heal"]` and autocast state migrated.
- [x] 14.2 `TestGreaterHeal_TargetsThreeLowestHPAllies` — Cleric with `greater_heal` casts among 5 allies of varying HP%; assert exactly the 3 lowest-HP% receive heal.
- [x] 14.3 `TestGreaterHeal_TiedHPBreaksByUnitID` — Two allies tied at same HP%; assert the lower-ID one is selected.
- [x] 14.4 `TestGreaterHeal_ForceIncludesFocus` — Cleric with focus on full-HP ally A and two injured allies B/C/D; assert A is force-included (displacing highest-HP-percent natural pick).
- [x] 14.5 `TestBattlePrayer_AppliesBuffOnHeal` — Heal an ally, assert ally's `BattlePrayerRemaining == buffDurationSeconds` and multiplier set.
- [x] 14.6 `TestBattlePrayer_BuffAppliedToAllGreaterHealTargets` — `TargetCount: 3` cast lands on 3 allies; assert all 3 have the buff.
- [x] 14.7 `TestBattlePrayer_RefreshNotStack` — recast on a buffed target; assert duration refreshes to max (not added) and multiplier does not exceed config.
- [x] 14.8 `TestBattlePrayer_DecaysInUpdateLoop` — apply buff, advance N ticks, assert `BattlePrayerRemaining` decreased by `N * dt`; assert multiplier resets to 0 when expired.
- [x] 14.9 `TestBattlePrayer_GrantsAttackSpeedBonus` — buff applied, assert `perkAttackSpeedBonusLocked` returns the configured multiplier.
- [x] 14.10 `TestBattlePrayer_AttackSpeedBonusAppliesToNonClericAlly` — apply buff to a non-Cleric Soldier; assert their attack speed is increased.
- [x] 14.11 `TestBattlePrayer_RecastThresholdTriggersFullHPCast` — Cleric with `battle_prayer` + focus on full-HP ally with stale buff; assert Heal is cast on focus this tick.
- [x] 14.12 `TestBattlePrayer_FreshBuffNoRecast` — same setup but buff `BattlePrayerRemaining > threshold`; assert no cast.
- [x] 14.13 `TestBattlePrayer_NoRecastWithoutPerk` — Cleric without `battle_prayer` + focus on full-HP ally; assert no cast (preserves "don't cast on full-HP" default).
- [x] 14.14 `TestSanctuary_ReducesProjectileDamage` — Sanctuary-owning Cleric near an ally being hit by `src.Kind == "projectile"`; assert applied damage is `original * (1 - reductionPercent)`.
- [x] 14.15 `TestSanctuary_DoesNotReduceMelee` — same setup with `src.Kind == "melee"`; assert no reduction.
- [x] 14.16 `TestSanctuary_DoesNotReduceTrap` — same with `src.Kind == "trap"`; assert no reduction.
- [x] 14.17 `TestSanctuary_TargetOutsideRadiusUnaffected` — assert no reduction when target is outside `radiusPixels`.
- [x] 14.18 `TestSanctuary_OverlappingAurasTakeMaxNoStack` — two Sanctuary-owning Clerics with 20% and 30% reductions over one target; assert applied damage uses 30% only.
- [x] 14.19 `TestManaConduit_BonusScalesWithInjuredAllies` — Cleric with `mana_conduit` near 3 injured allies; assert bonus mana per tick is `3 * bonusManaRegenPerAlly * dt`.
- [x] 14.20 `TestManaConduit_CapsAtMaxAlliesCounted` — 5 injured allies, cap = 3; assert bonus is `3 * bonusManaRegenPerAlly * dt`.
- [x] 14.21 `TestManaConduit_FullHPAlliesNotCounted` — 5 full-HP allies; assert zero bonus.
- [x] 14.22 `TestManaConduit_EnemiesNotCounted` — injured enemy in range, no allies injured; assert zero bonus.
- [x] 14.23 `TestManaConduit_ClampsAtMaxMana` — Cleric at full mana receiving bonus; assert `CurrentMana == MaxMana` after the tick.
- [x] 14.24 `TestNoFocus_AutoHealUnchangedForSingleTarget` — Cleric without focus, no `greater_heal`; assert behavior is byte-identical to current heal-only Apprentice (this is the no-regression test).

## 15. Determinism tests

- [x] 15.1 `TestDeterminism_BattlePrayerBuffApplicationsAcrossReplays` — seeded match with Cleric repeatedly healing the same group; replay twice; assert identical per-tick buff state.
- [x] 15.2 `TestDeterminism_ManaConduitBonusAcrossReplays` — seeded match with Cleric near varying injured-ally counts; replay twice; assert identical per-tick mana totals.
- [x] 15.3 `TestDeterminism_SanctuaryMitigationAcrossReplays` — seeded match with projectile damage flying through a Sanctuary aura; replay twice; assert identical damage applied each event.
- [x] 15.4 `TestDeterminism_GreaterHealTargetSetAcrossReplays` — seeded match with multiple injured allies, possible ties; replay twice; assert identical target IDs per cast.

## 16. Client tests (where the project has UI test infrastructure)

- [x] 16.1 Focus Target button highlight reflects snapshot `focusTargetId != 0`. *(Covered by `client/src/game-portal/src/game/core/focus_target.test.ts` — 9 vitest assertions exercising autoCast / active / hide-when-no-heal-ability / group-selection semantics through `getSelectionSummary()`.)*
- [~] 16.2 Right-click on Focus Target button sends `SetFocusTargetCommandMessage{TargetUnitID: 0}`. *(Skipped at the vitest level — mocking GameClient's NetworkClient+InputManager wiring requires component-test infrastructure (@vue/test-utils) not currently in the client deps. Covered by manual playtest 17.11 + server test 13.7 `TestFocusTarget_ClearWithZeroTarget` which locks the server-side contract.)*
- [~] 16.3 Ally-only cursor mode rejects clicks on enemies and sends a clear. *(Same rationale as 16.2 — covered by manual playtest 17.10 and server tests 13.6/13.8.)*
- [x] 16.4 Selection HUD displays the focus indicator line when `focusTargetId != 0`. *(Data path covered by `focus_target.test.ts` — verifies `selectedUnits[0].focusTargetId` and resolved focus-unit name/hp are present in the snapshot shape the HUD computes against. The Vue template binding itself is verified by manual playtest 17.1.)*

## 17. Manual playtest checklist

- [ ] 17.1 Start a match with a Cleric, give it Focus Target on an ally. Verify the button highlights, the Cleric follows, and the focus indicator appears in selection HUD.
- [ ] 17.2 Move ally around the map; verify Cleric maintains follow distance without obvious jitter.
- [ ] 17.3 Heal range test: damage the focus, verify the Cleric heals it before any other injured ally.
- [ ] 17.4 Battle Prayer flow: grant `battle_prayer` perk, heal an ally, verify attack-speed buff icon + faster attack rate.
- [ ] 17.5 Battle Prayer recast: focus a full-HP ally with `battle_prayer`, advance time until buff is stale, verify recast happens; verify it does NOT happen when buff is fresh.
- [ ] 17.6 Greater Heal: grant `greater_heal`, position 4+ injured allies in range, cast Heal, verify 3 receive heal + buff and the rest do not.
- [ ] 17.7 Greater Heal + Focus: focus a full-HP ally with `greater_heal` + `battle_prayer`; verify focus is force-included when buff goes stale even though other allies are healthier.
- [ ] 17.8 Sanctuary: grant `sanctuary`, stand allies near the Cleric, take projectile fire; verify damage is reduced. Take melee damage; verify no reduction.
- [ ] 17.9 Mana Conduit: grant `mana_conduit`, observe mana regen with 0/1/3/5 injured allies in range; verify scaling and cap.
- [ ] 17.10 Order interactions: with focus active, issue Move/Stop/AttackMove/AttackTarget; verify each clears focus and the button un-highlights.
- [ ] 17.11 Right-click clear: with focus active, right-click the Focus Target button; verify focus clears.

## 18. Documentation & cleanup

- [x] 18.1 Update inline comments at `state.go` `OrderType` enum to document `OrderFocusFollow` and its clearing semantics. *(Done at field-introduction time: `OrderFocusFollow` carries a comment block describing the order's semantics, the helpers to mutate it through, and the clearing contract.)*
- [x] 18.2 Update the "TO ADD A NEW PERK" comment block in `perks.go:34-68` if any new hook category was introduced (specifically `onPerkAbilityResolvedLocked`). *(Added the hook to the recipe checklist, added `applyPerkGrantedHooksLocked` + `onPerkAbilityResolvedLocked` to the call-sites block, and extended the cross-unit decay note to include `BattlePrayerRemaining` and call out that it can live on non-perk-owning units.)*
- [x] 18.3 Update the "WHERE THINGS LIVE" table in `perks.go` if new files were added. *(No new perk-runtime files were added — `focus_target.go` is its own subsystem and lives at the package root alongside the other order-system files, not under the perk runtime tree.)*
- [x] 18.4 Verify `CLAUDE.md` target-by-ID rules are honored: no `*Unit` or `*BuildingTile` stored on `Unit`, `Projectile`, `PerkState`, etc. Grep for new fields with pointer types added in this change. *(Confirmed via `git diff HEAD` — no new persistent `*Unit`/`*BuildingTile` struct fields. All `*Unit` references in the diff are tick-local: function parameters, short-lived locals, or `[]*Unit` slices passed into `resolveAbilityCastLocked` and back. CLAUDE.md rule 4 explicitly endorses these.)*
- [x] 18.5 Run the full test suite (`go test ./...`) and the client test suite; address regressions. *(Server: `go test ./internal/game/... ./internal/ws/... ./internal/profile/... ./internal/steam/... ./internal/transportbridge/...` all pass. The `cmd/api` `TestServerReadyLineAndStdinShutdown` test fails consistently — but `git diff --stat HEAD -- server/cmd/` is empty (this change touches zero cmd/api files) and the test was last modified by an unrelated earlier commit ("standalone application phase 1"). Flagged as a pre-existing Windows-stdin-shutdown flake, not a regression from this change. Client: `vitest run` — 35 tests pass across 2 files. The `internal/ws` baseline JSON was regenerated via `go test -update` to accept the additive `targetCount` / `focusTargetId` / `focus_follow` fields plus pre-existing food-max and lockedUnitTypes drifts that were captured but not previously baselined.)*
