## ADDED Requirements

### Requirement: `Unit` carries a `FocusTargetID` field stored by ID

`Unit` SHALL have a `FocusTargetID int` field. The field SHALL store the focused ally's unit ID, never a pointer, matching the project-wide ID-not-pointer convention for cross-tick target references. The zero value (`0`) SHALL mean "no focus target". Setting / clearing of the field SHALL only happen via `RequestSetFocusTargetLocked` and `clearFocusTargetLocked` so all transitions are observable from one place.

#### Scenario: Default value is zero

- **WHEN** a fresh unit is spawned
- **THEN** its `FocusTargetID` is `0` and no focus target is active

#### Scenario: Field stores ID, not pointer

- **WHEN** a focus target is set
- **THEN** the unit struct holds the target's ID; no `*Unit` is cached on the caster

### Requirement: `OrderType` has an `OrderFocusFollow` value

The `OrderType` enum in `state.go` SHALL declare a new value `OrderFocusFollow`. When a Cleric is given a focus target, its `Order` SHALL be set to `OrderState{Type: OrderFocusFollow}` and its `FocusTargetID` SHALL be set to the focused ally's ID. The order SHALL be cleared whenever any other order replaces it; the order replacement SHALL also zero `FocusTargetID` so the two never diverge.

#### Scenario: Setting focus transitions order and field together

- **WHEN** a player sets a focus target on a Cleric whose order was `OrderIdle`
- **THEN** the Cleric's `Order.Type` becomes `OrderFocusFollow` AND `FocusTargetID` is the chosen unit's ID

#### Scenario: New Move order clears focus

- **WHEN** a Cleric with `Order.Type == OrderFocusFollow` receives a Move order
- **THEN** `Order.Type` becomes `OrderMove` AND `FocusTargetID` is reset to `0`

#### Scenario: Stop order clears focus

- **WHEN** a Cleric with an active focus target receives a Stop order
- **THEN** `Order.Type` becomes `OrderIdle` (or the project's stop order) AND `FocusTargetID` is reset to `0`

#### Scenario: AttackMove order clears focus

- **WHEN** a Cleric with an active focus target receives an AttackMove order
- **THEN** `Order.Type` becomes `OrderAttackMove` AND `FocusTargetID` is reset to `0`

#### Scenario: AttackTarget order clears focus

- **WHEN** a Cleric with an active focus target receives an AttackTarget order
- **THEN** `Order.Type` becomes `OrderAttackTarget`, `AttackTargetID` is set to the named target, AND `FocusTargetID` is reset to `0`

### Requirement: Focus target validation runs every tick

Every tick a Cleric with `Order.Type == OrderFocusFollow` SHALL re-resolve `FocusTargetID` via `getUnitByIDLocked`. If the resolved unit is `nil`, `HP <= 0`, `Visible == false`, or no longer the caster's ally (team check), the focus target SHALL be cleared (`Order` transitions to `OrderIdle`, `FocusTargetID = 0`) and the Cleric SHALL fall back to default behavior (auto-heal, idle).

#### Scenario: Focus target death clears focus

- **WHEN** the focused ally's HP drops to `0` and they are removed
- **THEN** on the next tick the Cleric's `FocusTargetID` is `0` and `Order.Type` is `OrderIdle`

#### Scenario: Focus target becomes invisible (fog of war) clears focus

- **WHEN** the focused ally becomes invisible to the caster's vision
- **THEN** on the next tick the Cleric's `FocusTargetID` is `0` and the Cleric stops following

#### Scenario: Focus target switches teams clears focus

- **WHEN** the focused ally is no longer on the caster's team (hypothetical conversion)
- **THEN** the Cleric's focus is cleared on the next tick

### Requirement: `SetFocusTargetCommandMessage` is the wire protocol for setting and clearing focus

The protocol SHALL define a new message type `set_focus_target_command` with the shape:

```go
type SetFocusTargetCommandMessage struct {
    Type         string `json:"type"`
    CasterUnitID int    `json:"casterUnitId"`
    TargetUnitID int    `json:"targetUnitId"`
}
```

`TargetUnitID == 0` SHALL be interpreted as "clear focus". The WS handler SHALL validate match membership and ownership of `CasterUnitID`, then call `match.State.RequestSetFocusTargetLocked(playerID, casterUnitID, targetUnitID)`. Validation failures SHALL be reported back via the existing `NotificationMessage` mechanism.

#### Scenario: Setting focus succeeds for valid ally

- **WHEN** a player sends `SetFocusTargetCommandMessage{CasterUnitID: cleric, TargetUnitID: ally}` for a cleric they own and an allied target
- **THEN** the server sets the cleric's focus to that ally and the next snapshot reflects `focusTargetId: <ally>`

#### Scenario: Clearing focus succeeds with TargetUnitID 0

- **WHEN** a player sends `SetFocusTargetCommandMessage` with `TargetUnitID: 0`
- **THEN** the cleric's `FocusTargetID` becomes `0` and the order transitions to `OrderIdle`

#### Scenario: Setting focus on enemy is rejected

- **WHEN** a player sends `SetFocusTargetCommandMessage` with an enemy `TargetUnitID`
- **THEN** the server replies with a `NotificationMessage` indicating an invalid target and the cleric's focus is unchanged

#### Scenario: Setting focus on a unit the player does not own as caster is rejected

- **WHEN** `CasterUnitID` is not owned by the sending player
- **THEN** the server rejects the message via `NotificationMessage` and no state changes

### Requirement: Focus-followed Cleric paths toward the focus target

When `unit.Order.Type == OrderFocusFollow` and the focus target resolves to a valid ally, the movement tick SHALL maintain the Cleric within `focusFollowDistance` (a configurable per-Cleric value, default 64-96 pixels) of the focus target. Repathing SHALL be debounced: a new path SHALL be requested only when the current path's end is farther than `focusFollowDistance + leashSlack` from the focus's current position. Default `leashSlack` SHALL be ~24 pixels (hysteresis to avoid stutter). Pathing SHALL use the existing `assignUnitPathWithSubBlocked` helper — no new mover SHALL be introduced.

#### Scenario: Cleric maintains follow distance behind a moving target

- **WHEN** a focused ally moves 200 pixels in a single direction over several ticks
- **THEN** the Cleric's path is updated to keep within `focusFollowDistance + leashSlack` of the ally; no new path is requested while the existing path's end-cell is still inside the slack window

#### Scenario: Cleric remains stationary when already inside follow distance

- **WHEN** the focused ally has not moved more than `leashSlack` since the last repath
- **THEN** the Cleric does not request a new path this tick

### Requirement: Focus target reserves the Cleric's mana for itself

When a Cleric has `FocusTargetID != 0`, the Cleric's auto-cast Heal selector SHALL bypass the standard `lowest_hp_percentage_ally_in_range` selector entirely and reserve mana for the focus target. The selector SHALL return the focus target only when:

1. The focus target is injured (`HP < MaxHP`), OR
2. The focus target is at full HP AND the caster owns `battle_prayer` AND the focus target's `BattlePrayerRemaining < recastThresholdPercent * buffDurationSeconds` (buff-refresh case).

In any other case (focus invalid / out of range / full HP without a battle_prayer refresh trigger), the selector SHALL return `nil` — the Cleric saves its mana for the focus rather than auto-healing other injured allies in range. This is by design: Focus Target is a player-issued resource-management decision, and falling through to other allies would undermine it.

For multi-target casts (e.g. Greater Heal, `TargetCount > 1`), once the primary target is the focus (either because injured or because of the buff-refresh case), the rest of the slots may still be filled by the natural multi-target selector — but the cast itself is gated on the focus being a justified primary target.

When no focus target is set (`FocusTargetID == 0`), behavior SHALL be the existing `lowest_hp_percentage_ally_in_range` auto-cast selector with the only change being honoring `TargetCount` for Greater Heal.

#### Scenario: Injured focus is chosen even when another ally is more wounded

- **WHEN** a Cleric with focus on ally A (50% HP) is also near ally B (30% HP), and `TargetCount == 1`
- **THEN** the Heal cast targets A, not B (focus prioritization reserves mana for the focus; B is ignored)

#### Scenario: Focus at full HP, other ally injured — no cast

- **WHEN** a Cleric without `battle_prayer` has focus on ally A (100% HP) and is near ally B (30% HP)
- **THEN** no cast is initiated this tick — the Cleric saves its mana for A rather than spending it on B

#### Scenario: Full-HP focus with stale Battle Prayer triggers a recast

- **WHEN** a Cleric with `battle_prayer` perk has focus on ally A (100% HP) and A's `BattlePrayerRemaining` is below `recastThresholdPercent * buffDurationSeconds`
- **THEN** the Cleric casts Heal on A to refresh the buff

#### Scenario: Full-HP focus with fresh Battle Prayer does not recast

- **WHEN** a Cleric with `battle_prayer` perk has focus on a full-HP ally whose buff is above the recast threshold
- **THEN** no cast is initiated this tick

#### Scenario: Focus out of range — no cast (Cleric is en route)

- **WHEN** a Cleric has a valid injured focus that is currently outside cast range
- **THEN** no cast is initiated this tick (no fall-through to nearby allies); the Cleric is following toward the focus and will reconsider once in range

#### Scenario: No focus target uses standard auto-heal selector

- **WHEN** a Cleric without focus is near several injured allies
- **THEN** the standard `lowest_hp_percentage_ally_in_range` selector picks targets (honoring `TargetCount` if Greater Heal is owned)

### Requirement: Selecting an invalid target while in Focus-Targeting cursor mode clears focus

When the client has the player in Focus Target cursor mode (the player has clicked the Focus Target button to enter targeting), a click on an invalid target (enemy unit, terrain, building, or nothing) SHALL send a `SetFocusTargetCommandMessage` with `TargetUnitID: 0`. The server SHALL apply this as a focus clear. This matches the auto-cast-style UX where re-clicking the toggle and then failing to land a valid target deactivates the toggle.

#### Scenario: Clicking ground in focus-target mode clears focus

- **WHEN** the player enters Focus Target mode on a Cleric with an active focus and clicks empty ground
- **THEN** the client sends `SetFocusTargetCommandMessage{TargetUnitID: 0}` and the server clears focus

#### Scenario: Clicking an enemy in focus-target mode clears focus

- **WHEN** the player enters Focus Target mode and clicks an enemy unit
- **THEN** the client sends a clear (the targeting was invalid) and focus is cleared on the server

### Requirement: Right-click on the Focus Target button clears focus

The client Focus Target button SHALL respond to right-click by sending `SetFocusTargetCommandMessage{TargetUnitID: 0}` for the currently selected Cleric. This mirrors the auto-cast right-click-to-disable convention.

#### Scenario: Right-click on active Focus Target button clears focus

- **WHEN** the player right-clicks the Focus Target button while it is highlighted (focus active)
- **THEN** the client sends a clear and the button visually deactivates after the next snapshot

### Requirement: Selection HUD displays the current focus target

When a Cleric is selected and `selected.focusTargetId != 0` resolves to a known unit in the snapshot, the selection HUD SHALL display a single line `Focusing: <unitName> (<currentHP>/<maxHP>)` (exact label string is allowed to evolve; the line MUST identify the target and its HP). When no focus is set, no such line is displayed.

#### Scenario: Focused Cleric shows focus indicator

- **WHEN** a Cleric with an active focus target is the sole selection
- **THEN** the selection HUD includes the focus indicator line with the target's name and HP

#### Scenario: Cleric without focus shows no focus indicator

- **WHEN** a Cleric without focus is selected
- **THEN** no focus indicator line is rendered

### Requirement: Focus Target action button is rendered next to Heal

The Cleric's action bar SHALL include a Focus Target action button positioned adjacent to the Heal/Greater Heal ability button. The button SHALL use the same component as ability buttons but SHALL bind to the `SetFocusTargetCommandMessage` flow (not `CastAbilityCommandMessage`). The button's "active" highlight SHALL be driven by the snapshot's `focusTargetId != 0` for the currently selected Cleric — identical visual treatment to existing auto-cast toggles.

#### Scenario: Cleric without focus shows un-highlighted Focus Target button

- **WHEN** a Cleric with no active focus is selected
- **THEN** the Focus Target button is present, enabled, and not highlighted

#### Scenario: Cleric with focus shows highlighted Focus Target button

- **WHEN** a Cleric with `focusTargetId != 0` is selected
- **THEN** the Focus Target button is rendered with the active/highlight state
