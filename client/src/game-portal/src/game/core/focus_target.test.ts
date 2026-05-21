// Vitest harness for the client-side Focus Target UI plumbing. Covers the
// pure-TS reads from GameState — the SelectionHud / GameClient / InputManager
// interaction layers are exercised by the manual playtest checklist (section
// 17 of tasks.md) and by the server-side test suite which validates the
// snapshot contract end-to-end.
//
// Scope of THIS file:
//   - 16.1 The action-bar Focus Target button has autoCast: true when the
//          selected unit's snapshot reports focusTargetId !== 0 (drives the
//          .action-cell--autocast blue glow in SelectionHud.vue).
//   - 16.4-adjacent: getSelectionSummary returns a selection shape whose
//          selectedUnits[0].focusTargetId is the right value for the
//          SelectionHud "Focusing: …" computed to render against.
//
// Not covered by this file (and why):
//   - 16.2 Right-click on the Focus Target button → autocast-toggle-focus_target
//          → sendSetFocusTargetCommand(targetUnitId: 0). The handler lives in
//          GameClient.performSelectionAction; mocking GameClient's
//          NetworkClient + InputManager wiring for an isolated test would
//          require infrastructure we don't have (no @vue/test-utils,
//          @testing-library/vue, or @anthropic-ai/sdk-mock pattern).
//          Behaviour is verified by manual playtest 17.11 and by the
//          server-side test 13.7 (TestFocusTarget_ClearWithZeroTarget) which
//          asserts the server side of the contract.
//   - 16.3 Ally-only cursor mode rejects clicks on enemies. Same rationale —
//          covered by manual playtest 17.10 and by server tests 13.6 / 13.8
//          (enemy target rejection + order interactions).
//   - 16.4 Selection HUD rendering — the focusTargetLabel computed lives
//          inline in SelectionHud.vue and would require Vue component test
//          tooling to exercise. The data path it reads from (selectedUnits[0].
//          focusTargetId in the snapshot) IS tested here, so a regression
//          in the rendering layer would be a Vue template bug, not a state
//          bug; manual playtest 17.1 catches that.

import { describe, expect, it } from 'vitest'
import { GameState, type Unit, type ActionItem } from './GameState'

// Minimal synthetic unit factory. Defaults are tuned so the unit:
//   - passes unitOwnsHealAbility (has the 'heal' ability)
//   - is owned by the local player so it counts as selectable
//   - is visible so it can be picked / focused
//   - has the unit-type / capabilities the action-grid expects
function makeCleric(overrides: Partial<Unit> = {}): Unit {
  return {
    id: 1,
    unitType: 'apprentice',
    name: 'Cleric',
    capabilities: ['attack'] as Unit['capabilities'],
    visible: true,
    x: 0,
    y: 0,
    hp: 100,
    maxHp: 100,
    ownerId: 'p1',
    abilities: [
      {
        id: 'heal',
        displayName: 'Heal',
        manaCost: 5,
        supportsAutoCast: true,
        autoCast: false,
        cooldownRemaining: 0,
        cooldownTotal: 1,
        targetCount: 1,
      },
    ],
    ...overrides,
  }
}

// Pulls the Focus Target ActionItem out of the action grid returned by
// getSelectionSummary. Throws when the button is absent so failures are
// localised to the assertion instead of an undefined.find().
function focusTargetAction(actions: ActionItem[]): ActionItem {
  const item = actions.find((a) => a.id === 'focus_target')
  if (!item) {
    throw new Error(
      `focus_target action button missing from action grid: ${JSON.stringify(
        actions.map((a) => a.id),
      )}`,
    )
  }
  return item
}

// Construct a minimal GameState configured for a single local-player cleric.
// Centralised so individual tests stay terse. The state's public fields are
// initialised by class field declarations so no constructor args are needed.
function makeStateWithCleric(cleric: Unit): GameState {
  const state = new GameState()
  state.localPlayerId = 'p1'
  state.units = [cleric]
  state.selectedUnitIds = new Set([cleric.id])
  state.selectedUnitOrder = [cleric.id]
  return state
}

describe('Focus Target — action button highlight (16.1)', () => {
  it('autoCast is false when the cleric has no active focus', () => {
    const cleric = makeCleric({ focusTargetId: 0 })
    const state = makeStateWithCleric(cleric)

    const summary = state.getSelectionSummary()
    const action = focusTargetAction(summary.actions)

    expect(action.autoCast).toBe(false)
    expect(action.active).toBe(false)
  })

  it('autoCast is true when the cleric has an active focus target', () => {
    const focusedAllyId = 42
    const cleric = makeCleric({ focusTargetId: focusedAllyId })
    const state = makeStateWithCleric(cleric)

    const summary = state.getSelectionSummary()
    const action = focusTargetAction(summary.actions)

    expect(action.autoCast).toBe(true)
  })

  it('autoCast is true while the player is mid-selection (no focus yet)', () => {
    const cleric = makeCleric({ focusTargetId: 0 })
    const state = makeStateWithCleric(cleric)
    // beginFocusTargetTargeting flips unitTargetingMode to 'focus-target' so
    // the action item's `active` and `autoCast` both light up immediately —
    // the player needs visible feedback that the button is armed before they
    // commit on a target.
    state.beginFocusTargetTargeting()

    const summary = state.getSelectionSummary()
    const action = focusTargetAction(summary.actions)

    expect(action.active).toBe(true)
    expect(action.autoCast).toBe(true)
  })

  it('falls back to no Focus Target button when the unit owns no heal-class ability', () => {
    // A unit with abilities=[] (e.g. a Soldier) must NOT surface the Focus
    // Target button. unitOwnsHealAbility gates the visibility.
    const soldier = makeCleric({
      id: 7,
      name: 'Soldier',
      unitType: 'soldier',
      abilities: [],
    })
    const state = makeStateWithCleric(soldier)

    const summary = state.getSelectionSummary()
    const present = summary.actions.find((a) => a.id === 'focus_target')

    expect(present).toBeUndefined()
  })
})

describe('Focus Target — snapshot shape feeding the Selection HUD label (16.4-adjacent)', () => {
  // The "Focusing: <unit> (<hp>/<max>)" label in SelectionHud.vue reads from
  // selectedUnits[0].focusTargetId on the UI snapshot. These tests lock down
  // that the snapshot exposes the right value — any rendering-layer bug
  // in the Vue template is then localised, not a data-flow regression.
  it('exposes focusTargetId on the selected unit when focus is set', () => {
    const cleric = makeCleric({ focusTargetId: 42 })
    const state = makeStateWithCleric(cleric)

    const selectedUnits = state.getSelectedUnits()

    expect(selectedUnits).toHaveLength(1)
    expect(selectedUnits[0].focusTargetId).toBe(42)
  })

  it('exposes focusTargetId === 0 when no focus is set', () => {
    const cleric = makeCleric({ focusTargetId: 0 })
    const state = makeStateWithCleric(cleric)

    const selectedUnits = state.getSelectedUnits()

    expect(selectedUnits[0].focusTargetId).toBe(0)
  })

  it('resolves the focused unit when both caster and focus are in the selection snapshot', () => {
    // Simulates the case where the focused ally is also among the
    // visible-units array (e.g., player selected the cleric, and the focus
    // target happens to be visible). The SelectionHud label reads
    // selectedUnits — even when the focus isn't ALSO selected, the units
    // array still contains it via the snapshot, so the label can resolve
    // name+HP for display.
    const focusedAlly: Unit = {
      ...makeCleric({
        id: 42,
        name: 'Soldier',
        unitType: 'soldier',
        abilities: [],
        hp: 60,
        maxHp: 100,
      }),
    }
    const cleric = makeCleric({ focusTargetId: 42 })
    const state = new GameState()
    state.localPlayerId = 'p1'
    state.units = [cleric, focusedAlly]
    state.selectedUnitIds = new Set([cleric.id])
    state.selectedUnitOrder = [cleric.id]

    const selectedUnits = state.getSelectedUnits()
    const focusId = selectedUnits[0].focusTargetId ?? 0
    const focus = state.units.find((u) => u.id === focusId)

    expect(focus?.name).toBe('Soldier')
    expect(focus?.hp).toBe(60)
    expect(focus?.maxHp).toBe(100)
  })
})

describe('Focus Target — group selection behaviour', () => {
  // A group of two clerics where one is focused and one isn't: the button's
  // autoCast should still be true (anyFocused). This locks in the
  // "any-focused → glow" semantics described in buildFocusTargetActionItem.
  it('lights up the group button when any selected unit has a focus', () => {
    const cleric1 = makeCleric({ id: 1, focusTargetId: 42 })
    const cleric2 = makeCleric({ id: 2, focusTargetId: 0 })
    const state = new GameState()
    state.localPlayerId = 'p1'
    state.units = [cleric1, cleric2]
    state.selectedUnitIds = new Set([1, 2])
    state.selectedUnitOrder = [1, 2]

    const summary = state.getSelectionSummary()
    const action = focusTargetAction(summary.actions)

    expect(action.autoCast).toBe(true)
  })

  it('clears the group button when no selected unit has a focus', () => {
    const cleric1 = makeCleric({ id: 1, focusTargetId: 0 })
    const cleric2 = makeCleric({ id: 2, focusTargetId: 0 })
    const state = new GameState()
    state.localPlayerId = 'p1'
    state.units = [cleric1, cleric2]
    state.selectedUnitIds = new Set([1, 2])
    state.selectedUnitOrder = [1, 2]

    const summary = state.getSelectionSummary()
    const action = focusTargetAction(summary.actions)

    expect(action.autoCast).toBe(false)
  })
})
