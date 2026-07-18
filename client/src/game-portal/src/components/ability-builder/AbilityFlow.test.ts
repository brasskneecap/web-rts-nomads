import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref, shallowRef } from 'vue'
import type { AbilityProgram } from '@/game/abilities/program/abilityProgram'
import type { ActionSchemaBundle } from '@/game/abilities/program/programSchema'
import type { ValidationIssue } from '@/game/abilities/program/programValidation'
import AbilityFlow from './AbilityFlow.vue'
import { AbilityBuilderKey } from './AbilityBuilderContext'
import type { NodeRef } from './programTree'
import { emptyProgram } from './programTree'

// A program with one trigger carrying three actions (to exercise move
// bounds: first/middle/last) and a second, empty trigger (to exercise the
// "renders a card per trigger" / empty-body path).
function makeProgram(): AbilityProgram {
  return {
    entry: { type: 'unit', range: 300 },
    triggers: [
      {
        id: 't1',
        type: 'on_cast_complete',
        actions: [
          { id: 'a1', type: 'deal_damage', config: { amount: 10, type: 'fire' } },
          { id: 'a2', type: 'apply_status', config: { status: 'slow' }, disabled: true },
          { id: 'a3', type: 'camera_shake' },
        ],
      },
      { id: 't2', type: 'on_zone_enter', actions: [] },
    ],
  }
}

function makeSchema(): ActionSchemaBundle {
  return {
    actions: [
      { type: 'deal_damage', fields: [], runnable: true },
      { type: 'apply_status', fields: [], runnable: true },
      // Not (yet) executed by the runtime — should surface the
      // "display-only" chip on its action card.
      { type: 'camera_shake', fields: [], runnable: false },
    ],
    enums: { triggerTypes: ['on_cast_complete', 'on_zone_tick'] },
  }
}

// makeBuilderStub builds a minimal object satisfying everything AbilityFlow /
// FlowTriggerCard / FlowActionCard read or call from the injected builder —
// real refs for the reads (so v-model/computed reactivity works exactly as
// it would against the real composable), spies for every op.
function makeBuilderStub(overrides: {
  program?: AbilityProgram
  schema?: ActionSchemaBundle | null
  selected?: NodeRef
  issues?: ValidationIssue[]
} = {}) {
  const program = shallowRef<AbilityProgram>(overrides.program ?? emptyProgram())
  const schema = shallowRef<ActionSchemaBundle | null>(overrides.schema ?? null)
  const selected = shallowRef<NodeRef>(overrides.selected ?? { kind: 'ability' })
  const issues = ref<ValidationIssue[]>(overrides.issues ?? [])

  return {
    program,
    schema,
    selected,
    issues,
    select: vi.fn((r: NodeRef) => { selected.value = r }),
    addTrigger: vi.fn(),
    removeTrigger: vi.fn(),
    addAction: vi.fn(),
    removeAction: vi.fn(),
    moveAction: vi.fn(),
    duplicateAction: vi.fn(),
    toggleActionDisabled: vi.fn(),
  }
}

function mountFlow(builder: ReturnType<typeof makeBuilderStub>) {
  return mount(AbilityFlow, {
    global: { provide: { [AbilityBuilderKey as unknown as string]: builder } },
  })
}

describe('AbilityFlow', () => {
  it('renders a trigger card per root trigger and an action card per action', () => {
    const builder = makeBuilderStub({ program: makeProgram(), schema: makeSchema() })
    const wrapper = mountFlow(builder)

    const triggerCards = wrapper.findAll('[data-test="flow-trigger-card"]')
    expect(triggerCards).toHaveLength(2)

    const actionCards = wrapper.findAll('[data-test="flow-action-card"]')
    expect(actionCards).toHaveLength(3)
  })

  it('clicking an action card calls builder.select with the right action ref', async () => {
    const builder = makeBuilderStub({ program: makeProgram(), schema: makeSchema() })
    const wrapper = mountFlow(builder)

    const secondActionCard = wrapper.findAll('[data-test="flow-action-card"]')[1]
    await secondActionCard.find('.flow-action__body').trigger('click')

    expect(builder.select).toHaveBeenCalledWith({
      kind: 'action',
      path: [{ kind: 'trigger', id: 't1' }, { kind: 'action', id: 'a2' }],
    })
  })

  it('renders a disabled action dimmed with a disabled marker', () => {
    const builder = makeBuilderStub({ program: makeProgram(), schema: makeSchema() })
    const wrapper = mountFlow(builder)

    const actionCards = wrapper.findAll('[data-test="flow-action-card"]')
    // a2 (index 1) is authored with disabled: true.
    expect(actionCards[1].classes()).toContain('flow-action--disabled')
    expect(actionCards[1].text()).toContain('disabled')
    expect(actionCards[0].classes()).not.toContain('flow-action--disabled')
  })

  it('shows a display-only chip for a non-runnable action type', () => {
    const builder = makeBuilderStub({ program: makeProgram(), schema: makeSchema() })
    const wrapper = mountFlow(builder)

    const actionCards = wrapper.findAll('[data-test="flow-action-card"]')
    // a3 is camera_shake, marked runnable: false in the schema stub.
    expect(actionCards[2].find('.flow-action__chip').exists()).toBe(true)
    expect(actionCards[2].text()).toContain('display-only')
    // deal_damage / apply_status are runnable — no chip.
    expect(actionCards[0].find('.flow-action__chip').exists()).toBe(false)
    expect(actionCards[1].find('.flow-action__chip').exists()).toBe(false)
  })

  it('shows a validation badge for an action whose path has an error issue', () => {
    const builder = makeBuilderStub({
      program: makeProgram(),
      schema: makeSchema(),
      issues: [{ path: 'triggers[0].actions[0]', code: 'x', message: 'amount required', severity: 'error' }],
    })
    const wrapper = mountFlow(builder)

    const actionCards = wrapper.findAll('[data-test="flow-action-card"]')
    const badge = actionCards[0].find('.flow-action__badge')
    expect(badge.exists()).toBe(true)
    expect(badge.classes()).toContain('flow-action__badge--error')
    expect(badge.text()).toBe('1')
    // Only a1's exact path matched — a2/a3 get no badge.
    expect(actionCards[1].find('.flow-action__badge').exists()).toBe(false)
    expect(actionCards[2].find('.flow-action__badge').exists()).toBe(false)
  })

  it('disables move-up on the first action and move-down on the last', () => {
    const builder = makeBuilderStub({ program: makeProgram(), schema: makeSchema() })
    const wrapper = mountFlow(builder)

    const actionCards = wrapper.findAll('[data-test="flow-action-card"]')
    const controlsOf = (card: (typeof actionCards)[number]) => card.findAll('.flow-action__controls button')
    const [firstUp, firstDown] = controlsOf(actionCards[0])
    const [, middleDown] = controlsOf(actionCards[1])
    const [lastUp, lastDown] = controlsOf(actionCards[2])

    expect(firstUp.attributes('disabled')).toBeDefined()
    expect(firstDown.attributes('disabled')).toBeUndefined()
    expect(middleDown.attributes('disabled')).toBeUndefined()
    expect(lastUp.attributes('disabled')).toBeUndefined()
    expect(lastDown.attributes('disabled')).toBeDefined()
  })

  it('shows a friendly empty state when the program has no triggers', () => {
    const builder = makeBuilderStub({ program: emptyProgram(), schema: makeSchema() })
    const wrapper = mountFlow(builder)

    expect(wrapper.find('[data-test="ability-flow-empty"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="flow-trigger-card"]').exists()).toBe(false)
  })

  it('adding a trigger calls builder.addTrigger with the selected type', async () => {
    const builder = makeBuilderStub({ program: emptyProgram(), schema: makeSchema() })
    const wrapper = mountFlow(builder)

    await wrapper.find('select').setValue('on_zone_tick')
    await wrapper.find('[data-test="add-trigger-button"]').trigger('click')

    expect(builder.addTrigger).toHaveBeenCalledWith('on_zone_tick')
  })

  // Presentations used to render as a standalone read-only section at the
  // bottom of the Flow view (data-test="flow-presentation"). That section is
  // gone — a play_presentation action's presentation now renders inline
  // under its own FlowActionCard (see FlowActionCard.test.ts for coverage of
  // the resolution itself). This asserts the relocation, not just the new
  // location's behavior, which the FlowActionCard tests already own.
  it('no longer renders a standalone presentations section', () => {
    const program: AbilityProgram = {
      entry: { type: 'ground_point', range: 400 },
      triggers: [
        {
          id: 't1',
          type: 'on_cast_complete',
          actions: [
            {
              id: 'meteor',
              type: 'play_presentation',
              config: { asset: 'meteor', presentationId: 'p_meteor' },
            },
          ],
        },
      ],
      presentations: [
        {
          id: 'p_meteor',
          asset: 'meteor',
          position: { key: 'castPoint' },
          triggers: [
            { id: 'impact', type: 'on_animation_marker', actions: [{ id: 'a1', type: 'deal_damage' }] },
          ],
        },
      ],
    }
    const builder = makeBuilderStub({ program, schema: makeSchema() })
    const wrapper = mountFlow(builder)

    // Old standalone section is gone entirely...
    expect(wrapper.find('[data-test="flow-presentation"]').exists()).toBe(false)
    expect(wrapper.find('.ab-flow__presentations').exists()).toBe(false)
    // ...but the presentation itself still renders, now inline under its
    // referencing action card.
    expect(wrapper.find('[data-test="flow-action-presentation"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="flow-action-presentation"]').text()).toContain('p_meteor')
  })

  // The phase-7 acceptance fixture: meteor nests 3 levels —
  // cast -> meteor (play_presentation p_meteor)
  //   -> p_meteor.triggers[impact] -> sel, dmg, zone (create_zone)
  //     -> zone.config.triggers[burn] -> bsel, bdmg
  // This proves the recursion end-to-end: a depth-3 action nested under a
  // PRESENTATION's trigger's create_zone's OWN config.triggers is reachable
  // and selects with its full, correctly-ordered NodePath — not just that
  // some builder op fired.
  it('selects a depth-3 action (the crater DoT nested under a presentation trigger\'s create_zone) with its full NodePath', async () => {
    const program: AbilityProgram = {
      entry: { type: 'ground_point', range: 400 },
      triggers: [
        {
          id: 'cast',
          type: 'on_cast_complete',
          actions: [{ id: 'meteor', type: 'play_presentation', config: { presentationId: 'p_meteor' } }],
        },
      ],
      presentations: [
        {
          id: 'p_meteor',
          asset: 'meteor',
          position: { key: 'castPoint' },
          triggers: [
            {
              id: 'impact',
              type: 'on_animation_marker',
              timing: { marker: 'impact' },
              actions: [
                { id: 'sel', type: 'select_targets' },
                { id: 'dmg', type: 'deal_damage', config: { amount: 140, type: 'fire' } },
                {
                  id: 'zone',
                  type: 'create_zone',
                  config: {
                    radius: 150,
                    triggers: [
                      {
                        id: 'burn',
                        type: 'on_zone_tick',
                        timing: { tickInterval: 1000 },
                        actions: [
                          { id: 'bsel', type: 'select_targets' },
                          { id: 'bdmg', type: 'deal_damage', config: { amount: 30, type: 'fire' } },
                        ],
                      },
                    ],
                  },
                },
              ],
            },
          ],
        },
      ],
    }

    const builder = makeBuilderStub({ program, schema: makeSchema() })
    const wrapper = mountFlow(builder)

    // Distinct amounts (140 vs 30) so the impact damage and the crater DoT
    // damage are unambiguously distinguishable by rendered summary text.
    const bdmgCard = wrapper
      .findAll('[data-test="flow-action-card"]')
      .find((c) => c.text().includes('30 fire'))!
    await bdmgCard.find('.flow-action__body').trigger('click')

    expect(builder.select).toHaveBeenCalledWith({
      kind: 'action',
      path: [
        { kind: 'presentation', id: 'p_meteor' },
        { kind: 'trigger', id: 'impact' },
        { kind: 'action', id: 'zone' },
        { kind: 'trigger', id: 'burn' },
        { kind: 'action', id: 'bdmg' },
      ],
    })
  })
})
