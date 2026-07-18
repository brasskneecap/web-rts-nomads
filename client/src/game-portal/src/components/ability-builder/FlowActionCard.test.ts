import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref, shallowRef } from 'vue'
import type { AbilityActionDef, AbilityProgram } from '@/game/abilities/program/abilityProgram'
import type { ActionSchemaBundle } from '@/game/abilities/program/programSchema'
import type { ValidationIssue } from '@/game/abilities/program/programValidation'
import FlowActionCard from './FlowActionCard.vue'
import { AbilityBuilderKey } from './AbilityBuilderContext'
import type { NodePath, NodeRef } from './programTree'
import { emptyProgram } from './programTree'

// makeBuilderStub mirrors AbilityFlow.test.ts's stub — a minimal object
// satisfying everything FlowActionCard reads/calls from the injected
// builder: real refs for reads, spies for ops.
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
    updateActionConfig: vi.fn(),
  }
}

function loopAction(): AbilityActionDef {
  return {
    id: 'a1',
    type: 'loop',
    config: {
      iterations: 3,
      vars: [{ name: 'a', start: 65, step: -5 }],
      body: [
        { id: 'dmg', type: 'deal_damage', config: { amount: 'a', type: 'lightning' } },
        { id: 'gap', type: 'wait', config: { seconds: 0.12 } },
      ],
    },
  }
}

const defaultPath: NodePath = [{ kind: 'trigger', id: 't1' }, { kind: 'action', id: 'a1' }]

function mountCard(action: AbilityActionDef, builder: ReturnType<typeof makeBuilderStub>, path: NodePath = defaultPath) {
  return mount(FlowActionCard, {
    props: { action, index: 0, count: 1, path },
    global: { provide: { [AbilityBuilderKey as unknown as string]: builder } },
  })
}

// The same meteor-style presentation shape as the server-verified indirection
// (ability_exec_presentation.go:112-126): a play_presentation action's
// config.presentationId matched against program.presentations[].id.
function meteorProgram(): AbilityProgram {
  return {
    entry: { type: 'ground_point', range: 400 },
    triggers: [],
    presentations: [
      {
        id: 'p_meteor',
        asset: 'meteor',
        position: { key: 'castPoint' },
        scale: 3,
        triggers: [
          {
            id: 'impact',
            type: 'on_animation_marker',
            timing: { marker: 'impact', delaySeconds: 0.6 },
            actions: [
              { id: 'a1', type: 'deal_damage', config: { amount: 140, type: 'fire' } },
              { id: 'a2', type: 'create_zone', config: { name: 'Burning Crater' } },
              { id: 'a3', type: 'camera_shake' },
            ],
          },
        ],
      },
    ],
  }
}

describe('FlowActionCard', () => {
  it("renders a play_presentation action's resolved presentation asset (read-only header) and its triggers as real, editable FlowTriggerCards", () => {
    const builder = makeBuilderStub({ program: meteorProgram() })
    const action: AbilityActionDef = {
      id: 'meteor',
      type: 'play_presentation',
      config: { asset: 'meteor', position: { key: 'castPoint' }, scale: 3.0, presentationId: 'p_meteor' },
    }
    const wrapper = mountCard(action, builder)

    const block = wrapper.find('[data-test="flow-action-presentation"]')
    expect(block.exists()).toBe(true)
    // Read-only header: presentation id + asset, no editor for either.
    expect(block.text()).toContain('p_meteor')
    expect(block.text()).toContain('meteor')

    // The presentation's own trigger ("impact") is a REAL recursive
    // FlowTriggerCard, not a dead label — the humanized type comes from its
    // own header, and its three actions are full FlowActionCards nested
    // beneath it (proving the recursion, not just a text summary).
    const nestedTrigger = block.find('[data-test="flow-trigger-card"]')
    expect(nestedTrigger.exists()).toBe(true)
    expect(nestedTrigger.text()).toContain('On Animation Marker')
    expect(block.findAll('[data-test="flow-action-card"]')).toHaveLength(3)
    expect(block.text()).toContain('Deal Damage')
    expect(block.text()).toContain('Create Zone — Burning Crater')
  })

  it("a presentation trigger's action is selectable through the recursive card, calling builder.select with its presentation-rooted path", async () => {
    const builder = makeBuilderStub({ program: meteorProgram() })
    const action: AbilityActionDef = {
      id: 'meteor',
      type: 'play_presentation',
      config: { presentationId: 'p_meteor' },
    }
    const wrapper = mountCard(action, builder)

    const dealDamageCard = wrapper
      .findAll('[data-test="flow-action-card"]')
      .find((c) => c.text().includes('Deal Damage'))!
    await dealDamageCard.find('.flow-action__body').trigger('click')

    expect(builder.select).toHaveBeenCalledWith({
      kind: 'action',
      path: [
        { kind: 'presentation', id: 'p_meteor' },
        { kind: 'trigger', id: 'impact' },
        { kind: 'action', id: 'a1' },
      ],
    })
  })

  it('renders no presentation block and does not throw when presentationId resolves to nothing', () => {
    const builder = makeBuilderStub({ program: emptyProgram() })
    const action: AbilityActionDef = {
      id: 'orphan',
      type: 'play_presentation',
      config: { presentationId: 'does_not_exist' },
    }

    expect(() => mountCard(action, builder)).not.toThrow()
    const wrapper = mountCard(action, builder)
    expect(wrapper.find('[data-test="flow-action-presentation"]').exists()).toBe(false)
  })

  it('renders no presentation block when presentationId is missing entirely', () => {
    const builder = makeBuilderStub({ program: meteorProgram() })
    const action: AbilityActionDef = { id: 'no-id', type: 'play_presentation', config: { asset: 'meteor' } }
    const wrapper = mountCard(action, builder)

    expect(wrapper.find('[data-test="flow-action-presentation"]').exists()).toBe(false)
  })

  it('renders no presentation block for a non-play_presentation action', () => {
    const builder = makeBuilderStub({ program: meteorProgram() })
    const action: AbilityActionDef = { id: 'a1', type: 'deal_damage', config: { amount: 10, type: 'fire' } }
    const wrapper = mountCard(action, builder)

    expect(wrapper.find('[data-test="flow-action-presentation"]').exists()).toBe(false)
  })

  describe('loop wrapper', () => {
    it('renders the loop header (iterations + vars) and its body as real action cards', () => {
      const wrapper = mountCard(loopAction(), makeBuilderStub())
      expect(wrapper.find('[data-test="flow-action-loop"]').exists()).toBe(true)
      expect((wrapper.find('[data-test="loop-iterations"]').element as HTMLInputElement).value).toBe('3')
      expect((wrapper.find('[data-test="loop-var-a-start"]').element as HTMLInputElement).value).toBe('65')
      expect((wrapper.find('[data-test="loop-var-a-step"]').element as HTMLInputElement).value).toBe('-5')
      // The 2 body actions render as nested FlowActionCards (deal_damage, wait),
      // so 3 cards total: the loop wrapper itself + its 2 body steps.
      expect(wrapper.findAll('[data-test="flow-action-card"]')).toHaveLength(3)
      // …and the loop has its own "+ Action" affordance to add more steps.
      expect(wrapper.find('[data-test="loop-add-action"]').exists()).toBe(true)
    })

    it('editing iterations writes back via updateActionConfig', async () => {
      const builder = makeBuilderStub()
      const wrapper = mountCard(loopAction(), builder)
      const input = wrapper.find('[data-test="loop-iterations"]')
      ;(input.element as HTMLInputElement).value = '6'
      await input.trigger('change')
      expect(builder.updateActionConfig).toHaveBeenCalledWith(defaultPath, { iterations: 6 })
    })

    it('adding a variable writes the next letter', async () => {
      const builder = makeBuilderStub()
      const wrapper = mountCard(loopAction(), builder)
      await wrapper.find('[data-test="loop-add-var"]').trigger('click')
      expect(builder.updateActionConfig).toHaveBeenCalledWith(defaultPath, {
        vars: [{ name: 'a', start: 65, step: -5 }, { name: 'b', start: 0, step: 0 }],
      })
    })

    it('editing a variable step (negative) writes back', async () => {
      const builder = makeBuilderStub()
      const wrapper = mountCard(loopAction(), builder)
      const input = wrapper.find('[data-test="loop-var-a-step"]')
      ;(input.element as HTMLInputElement).value = '-8'
      await input.trigger('change')
      expect(builder.updateActionConfig).toHaveBeenCalledWith(defaultPath, {
        vars: [{ name: 'a', start: 65, step: -8 }],
      })
    })

    it('toggling "step first" writes stepFirst back', async () => {
      const builder = makeBuilderStub()
      const wrapper = mountCard(loopAction(), builder)
      const cb = wrapper.find('[data-test="loop-step-first"]')
      expect((cb.element as HTMLInputElement).checked).toBe(false)
      await cb.setValue(true)
      expect(builder.updateActionConfig).toHaveBeenCalledWith(defaultPath, { stepFirst: true })
    })

    it('switching a variable step to percent writes stepMode back', async () => {
      const builder = makeBuilderStub()
      const wrapper = mountCard(loopAction(), builder)
      const mode = wrapper.find('[data-test="loop-var-a-stepmode"]')
      expect((mode.element as HTMLSelectElement).value).toBe('number') // default flat
      await mode.setValue('percent')
      expect(builder.updateActionConfig).toHaveBeenCalledWith(defaultPath, {
        vars: [{ name: 'a', start: 65, step: -5, stepMode: 'percent' }],
      })
    })

    it('renders no loop block for a non-loop action', () => {
      const action: AbilityActionDef = { id: 'a1', type: 'deal_damage', config: { amount: 10 } }
      const wrapper = mountCard(action, makeBuilderStub())
      expect(wrapper.find('[data-test="flow-action-loop"]').exists()).toBe(false)
    })
  })
})
