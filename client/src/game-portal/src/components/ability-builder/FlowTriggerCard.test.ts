import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref, shallowRef } from 'vue'
import type { AbilityActionDef, AbilityProgram, AbilityTriggerDef } from '@/game/abilities/program/abilityProgram'
import type { ActionSchemaBundle } from '@/game/abilities/program/programSchema'
import type { ValidationIssue } from '@/game/abilities/program/programValidation'
import FlowTriggerCard from './FlowTriggerCard.vue'
import { AbilityBuilderKey } from './AbilityBuilderContext'
import { emptyProgram, type NodePath, type NodeRef } from './programTree'
import { useAbilityBuilder } from './useAbilityBuilder'

function makeSchema(): ActionSchemaBundle {
  return {
    actions: [{ type: 'deal_damage', fields: [], runnable: true }],
    enums: {},
  }
}

function makeBuilderStub(overrides: {
  program?: AbilityProgram
  schema?: ActionSchemaBundle | null
  selected?: NodeRef
  issues?: ValidationIssue[]
} = {}) {
  const program = shallowRef<AbilityProgram>(overrides.program ?? emptyProgram())
  const schema = shallowRef<ActionSchemaBundle | null>(overrides.schema ?? makeSchema())
  const selected = shallowRef<NodeRef>(overrides.selected ?? { kind: 'ability' })
  const issues = ref<ValidationIssue[]>(overrides.issues ?? [])

  return {
    program,
    schema,
    selected,
    issues,
    select: vi.fn(),
    removeTrigger: vi.fn(),
    addAction: vi.fn(),
    addTrigger: vi.fn(),
  }
}

function mountCard(
  trigger: AbilityTriggerDef,
  builder: ReturnType<typeof makeBuilderStub>,
  path: NodePath = [{ kind: 'trigger', id: trigger.id }],
) {
  return mount(FlowTriggerCard, {
    props: { trigger, index: 0, path },
    global: { provide: { [AbilityBuilderKey as unknown as string]: builder } },
  })
}

describe('FlowTriggerCard — Add Action dialog wiring', () => {
  it('"+ Action" opens the dialog closed by default', () => {
    const trigger: AbilityTriggerDef = { id: 't1', type: 'on_cast_complete', actions: [] }
    const builder = makeBuilderStub()
    const wrapper = mountCard(trigger, builder)

    expect(wrapper.find('[data-test="add-action-overlay"]').exists()).toBe(false)
  })

  it('clicking "+ Action" opens the dialog scoped to THIS trigger, not the current selection', async () => {
    // Selection points at an unrelated trigger — the dialog must still target
    // the trigger this card owns, passed explicitly as a prop.
    const trigger: AbilityTriggerDef = { id: 't2', type: 'on_target_hit', actions: [] }
    const builder = makeBuilderStub({
      selected: { kind: 'trigger', path: [{ kind: 'trigger', id: 'some-other-trigger' }] },
    })
    const wrapper = mountCard(trigger, builder)

    await wrapper.find('[data-test="flow-trigger-add-action"]').trigger('click')

    expect(wrapper.find('[data-test="add-action-overlay"]').exists()).toBe(true)

    const entry = wrapper.findAll('[data-test="add-action-entry"]').find((e) => e.text().includes('Deal Damage'))!
    await entry.trigger('click')

    expect(builder.addAction).toHaveBeenCalledWith([{ kind: 'trigger', id: 't2' }], 'deal_damage')
  })

  it('picking an entry closes the dialog', async () => {
    const trigger: AbilityTriggerDef = { id: 't1', type: 'on_cast_complete', actions: [] }
    const builder = makeBuilderStub()
    const wrapper = mountCard(trigger, builder)

    await wrapper.find('[data-test="flow-trigger-add-action"]').trigger('click')
    expect(wrapper.find('[data-test="add-action-overlay"]').exists()).toBe(true)

    const entry = wrapper.findAll('[data-test="add-action-entry"]').find((e) => e.text().includes('Deal Damage'))!
    await entry.trigger('click')

    expect(wrapper.find('[data-test="add-action-overlay"]').exists()).toBe(false)
  })

  it('the dialog backdrop also closes it without adding anything', async () => {
    const trigger: AbilityTriggerDef = { id: 't1', type: 'on_cast_complete', actions: [] }
    const builder = makeBuilderStub()
    const wrapper = mountCard(trigger, builder)

    await wrapper.find('[data-test="flow-trigger-add-action"]').trigger('click')
    await wrapper.find('[data-test="add-action-overlay"]').trigger('click')

    expect(wrapper.find('[data-test="add-action-overlay"]').exists()).toBe(false)
    expect(builder.addAction).not.toHaveBeenCalled()
  })
})

// zoneTriggerProgram builds a minimal program shaped like meteor's own
// create_zone/config.triggers nesting: root trigger t1 -> action zone
// (create_zone) -> config.triggers[0] burn. Used by every test below that
// needs `builder.program` to structurally agree with the `trigger`/`path`
// props being mounted (indexPathFor/resolveNode read builder.program, not
// the trigger prop, so a mismatched stub program would silently break the
// badge/selection assertions).
function zoneTriggerProgram(nested: AbilityTriggerDef, extra?: Partial<AbilityActionDef>): AbilityProgram {
  return {
    entry: { type: 'no_target', range: 0 },
    triggers: [
      {
        id: 't1',
        type: 'on_cast_complete',
        actions: [
          {
            id: 'zone',
            type: 'create_zone',
            config: { radius: 150, triggers: [nested] },
            ...extra,
          },
        ],
      },
    ],
  }
}

describe('FlowTriggerCard — nested triggers (recursive rendering)', () => {
  it('renders a nested trigger as a real recursive FlowTriggerCard and it is selectable', async () => {
    const burn: AbilityTriggerDef = {
      id: 'burn',
      type: 'on_zone_tick',
      timing: { tickInterval: 1000 },
      actions: [{ id: 'bdmg', type: 'deal_damage', config: { amount: 5 } }],
    }
    const program = zoneTriggerProgram(burn)
    const builder = makeBuilderStub({ program })
    const wrapper = mountCard(program.triggers[0], builder)

    // Root + one real nested card, not a dead label.
    const triggerCards = wrapper.findAll('[data-test="flow-trigger-card"]')
    expect(triggerCards).toHaveLength(2)
    expect(triggerCards[1].text()).toContain('On Zone Tick')
    // Its own action renders as a full FlowActionCard underneath it.
    expect(triggerCards[1].find('[data-test="flow-action-card"]').text()).toContain('Deal Damage')

    await triggerCards[1].find('.flow-trigger__title').trigger('click')

    expect(builder.select).toHaveBeenCalledWith({
      kind: 'trigger',
      path: [{ kind: 'trigger', id: 't1' }, { kind: 'action', id: 'zone' }, { kind: 'trigger', id: 'burn' }],
    })
  })

  it('shows BOTH nested triggers when an action has children AND config.triggers populated (union, not first-match)', () => {
    const configNested: AbilityTriggerDef = { id: 'cfgT', type: 'on_zone_tick', actions: [] }
    const childNested: AbilityTriggerDef = { id: 'childT', type: 'on_action_complete', actions: [] }
    const program = zoneTriggerProgram(configNested, { children: [childNested] })
    const builder = makeBuilderStub({ program })
    const wrapper = mountCard(program.triggers[0], builder)

    // Root + BOTH nested triggers — the old duck-typed reader would have
    // hidden `cfgT` here because `children` is non-empty (first-match bug).
    const triggerCards = wrapper.findAll('[data-test="flow-trigger-card"]')
    expect(triggerCards).toHaveLength(3)
    const nestedTexts = [triggerCards[1].text(), triggerCards[2].text()]
    expect(nestedTexts.some((t) => t.includes('On Zone Tick'))).toBe(true)
    expect(nestedTexts.some((t) => t.includes('On Action Complete'))).toBe(true)
  })

  it("renders a validation badge on a nested node, proving indexPathFor's path agrees with the Go validator's grammar", () => {
    const burn: AbilityTriggerDef = { id: 'burn', type: 'on_zone_tick', actions: [] }
    const program = zoneTriggerProgram(burn)
    const builder = makeBuilderStub({
      program,
      // Grammar per programTree.indexPathFor: root trigger (index 0) ->
      // its action (index 0) -> that action's config.triggers (index 0).
      issues: [
        {
          path: 'triggers[0].actions[0].config.triggers[0]',
          code: 'invalid_tick_interval',
          message: 'tick interval must be positive',
          severity: 'error',
        },
      ],
    })
    const wrapper = mountCard(program.triggers[0], builder)

    // .find() searches the WHOLE subtree, and a nested card's head is a
    // descendant of the root card's DOM — so scope through each card's OWN
    // `.flow-trigger__head` first (siblings of the nested cards, which live
    // in `.flow-trigger__body` instead) to avoid the root's query picking up
    // the nested card's badge too.
    const triggerCards = wrapper.findAll('[data-test="flow-trigger-card"]')
    expect(triggerCards[0].find('.flow-trigger__head').find('.flow-trigger__badge').exists()).toBe(false)
    const nestedBadge = triggerCards[1].find('.flow-trigger__head').find('.flow-trigger__badge')
    expect(nestedBadge.exists()).toBe(true)
    expect(nestedBadge.classes()).toContain('flow-trigger__badge--error')
    expect(nestedBadge.text()).toBe('1')
  })

  it('adding a nested trigger to a create_zone action targets config.triggers, never children', async () => {
    // Uses the REAL composable (not a spy stub) so the assertion checks
    // where the trigger actually landed, not just that addTrigger was
    // called — proving the slot-routing rule end-to-end through the UI.
    const builder = useAbilityBuilder()
    builder.program.value = {
      entry: { type: 'no_target', range: 0 },
      triggers: [
        {
          id: 't1',
          type: 'on_cast_complete',
          actions: [{ id: 'zone', type: 'create_zone', config: { radius: 100 } }],
        },
      ],
    }
    const wrapper = mount(FlowTriggerCard, {
      props: { trigger: builder.program.value.triggers[0], index: 0, path: [{ kind: 'trigger', id: 't1' }] },
      global: { provide: { [AbilityBuilderKey as unknown as string]: builder } },
    })

    await wrapper.find('[data-test="flow-trigger-add-nested-trigger"]').trigger('click')

    const zoneAction = builder.program.value.triggers[0].actions[0]
    const cfgTriggers = zoneAction.config?.triggers as AbilityTriggerDef[] | undefined
    expect(cfgTriggers).toHaveLength(1)
    expect(cfgTriggers?.[0].type).toBe('on_zone_tick')
    expect(zoneAction.children ?? []).toHaveLength(0)
  })

  it('offers a type picker for a nested trigger on create_zone but a single fixed type (no picker) for any other action', () => {
    const burn: AbilityTriggerDef = { id: 'burn', type: 'on_zone_tick', actions: [] }
    const zoneProgram = zoneTriggerProgram(burn)
    const zoneBuilder = makeBuilderStub({ program: zoneProgram })
    const zoneWrapper = mountCard(zoneProgram.triggers[0], zoneBuilder)
    expect(zoneWrapper.find('.flow-trigger__nested-add select').exists()).toBe(true)

    const plainProgram: AbilityProgram = {
      entry: { type: 'no_target', range: 0 },
      triggers: [{ id: 't2', type: 'on_cast_complete', actions: [{ id: 'a1', type: 'camera_shake' }] }],
    }
    const plainBuilder = makeBuilderStub({ program: plainProgram })
    const plainWrapper = mountCard(plainProgram.triggers[0], plainBuilder)
    expect(plainWrapper.find('.flow-trigger__nested-add select').exists()).toBe(false)
    expect(plainWrapper.find('[data-test="flow-trigger-add-nested-trigger"]').exists()).toBe(true)
  })
})
