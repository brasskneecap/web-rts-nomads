import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref, shallowRef } from 'vue'
import type { AbilityEditorForm } from '@/game/abilities/abilityEditorForm'
import { createBlankForm } from '@/game/abilities/abilityEditorForm'
import type { AbilityProgram } from '@/game/abilities/program/abilityProgram'
import type { ActionSchemaBundle } from '@/game/abilities/program/programSchema'
import type { ValidationIssue } from '@/game/abilities/program/programValidation'
import InspectorBar from './InspectorBar.vue'
import { AbilityBuilderKey } from './AbilityBuilderContext'
import type { AbilityBuilderCatalogs } from './useAbilityBuilder'
import type { NodePath, NodeRef } from './programTree'
import { emptyProgram } from './programTree'

const t1ActionA1Path: NodePath = [{ kind: 'trigger', id: 't1' }, { kind: 'action', id: 'a1' }]
const t1Path: NodePath = [{ kind: 'trigger', id: 't1' }]

function emptyCatalogs(): AbilityBuilderCatalogs {
  return { effects: [], projectiles: [], damageTypes: [], categories: [], autoCastSelectors: [], unitTypes: [] }
}

function makeProgram(): AbilityProgram {
  return {
    entry: { type: 'unit', range: 300 },
    triggers: [
      {
        id: 't1',
        type: 'on_cast_complete',
        actions: [{ id: 'a1', type: 'deal_damage', config: { amount: 10 } }],
      },
    ],
  }
}

function makeSchema(): ActionSchemaBundle {
  return {
    actions: [
      {
        type: 'deal_damage',
        runnable: true,
        fields: [
          { key: 'amount', label: 'Amount', control: 'number', section: 'Properties' },
          { key: 'radius', label: 'Radius', control: 'number', section: 'Targeting' },
        ],
      },
    ],
    enums: {},
  }
}

// makeBuilderStub mirrors ItemInspector.test.ts's precedent: real refs for
// the reads InspectorBar consumes, spies for every op it can call.
function makeBuilderStub(overrides: {
  form?: AbilityEditorForm
  program?: AbilityProgram
  schema?: ActionSchemaBundle | null
  catalogs?: AbilityBuilderCatalogs
  selected?: NodeRef
  issues?: ValidationIssue[]
} = {}) {
  return {
    form: shallowRef<AbilityEditorForm>(overrides.form ?? createBlankForm()),
    program: shallowRef<AbilityProgram>(overrides.program ?? emptyProgram()),
    schema: shallowRef<ActionSchemaBundle | null>(overrides.schema ?? null),
    catalogs: shallowRef<AbilityBuilderCatalogs>(overrides.catalogs ?? emptyCatalogs()),
    selected: shallowRef<NodeRef>(overrides.selected ?? { kind: 'ability' }),
    issues: ref<ValidationIssue[]>(overrides.issues ?? []),
    updateForm: vi.fn(),
    updateAction: vi.fn(),
    updateActionConfig: vi.fn(),
    updateTrigger: vi.fn(),
    select: vi.fn(),
  }
}

function mountInspectorBar(builder: ReturnType<typeof makeBuilderStub>) {
  return mount(InspectorBar, {
    global: { provide: { [AbilityBuilderKey as unknown as string]: builder } },
  })
}

describe('InspectorBar', () => {
  it('shows a hint, not a blank bar, when the ability node is selected', () => {
    const builder = makeBuilderStub({ selected: { kind: 'ability' } })
    const wrapper = mountInspectorBar(builder)

    expect(wrapper.find('[data-test="inspector-bar-empty"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Select a trigger or action')
    expect(wrapper.text()).not.toContain('Identity')
  })

  it('shows a hint-free schema-driven form for a selected action, grouped by section', () => {
    const builder = makeBuilderStub({
      program: makeProgram(),
      schema: makeSchema(),
      selected: { kind: 'action', path: t1ActionA1Path },
    })
    const wrapper = mountInspectorBar(builder)

    expect(wrapper.find('[data-test="inspector-bar-empty"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('deal_damage')
    expect(wrapper.text()).toContain('Properties')
    expect(wrapper.text()).toContain('Targeting')

    const amountInput = wrapper.find('input[type="number"]')
    expect((amountInput.element as HTMLInputElement).value).toBe('10')
  })

  it('editing an action field commits on blur (change), not per keystroke', async () => {
    const builder = makeBuilderStub({
      program: makeProgram(),
      schema: makeSchema(),
      selected: { kind: 'action', path: t1ActionA1Path },
    })
    const wrapper = mountInspectorBar(builder)

    const amountInput = wrapper.find('input[type="number"]')
    // NOTE: VTU's wrapper.setValue() fires BOTH 'input' and 'change' (see
    // SchemaField.test.ts) — simulate a bare keystroke with .value + 'input'.
    const el = amountInput.element as HTMLInputElement
    el.value = '4'
    await amountInput.trigger('input')
    el.value = '42'
    await amountInput.trigger('input')
    expect(builder.updateActionConfig).not.toHaveBeenCalled()

    await amountInput.trigger('change')
    expect(builder.updateActionConfig).toHaveBeenCalledTimes(1)
    expect(builder.updateActionConfig).toHaveBeenCalledWith(t1ActionA1Path, { amount: 42 })
  })

  it('shows a display-only note for a non-runnable action type', () => {
    const schema = makeSchema()
    schema.actions[0].runnable = false
    const builder = makeBuilderStub({
      program: makeProgram(),
      schema,
      selected: { kind: 'action', path: t1ActionA1Path },
    })
    const wrapper = mountInspectorBar(builder)

    expect(wrapper.text()).toContain("isn't executed by the runtime yet")
  })

  it('renders trigger fields for a selected trigger and commits timing on blur', async () => {
    const builder = makeBuilderStub({
      program: makeProgram(),
      selected: { kind: 'trigger', path: t1Path },
    })
    const wrapper = mountInspectorBar(builder)

    expect(wrapper.text()).toContain('Trigger')
    const frameInput = wrapper.find('input[type="number"]')
    ;(frameInput.element as HTMLInputElement).value = '12'
    await frameInput.trigger('input')
    expect(builder.updateTrigger).not.toHaveBeenCalled()
    await frameInput.trigger('change')

    expect(builder.updateTrigger).toHaveBeenCalledTimes(1)
    expect(builder.updateTrigger).toHaveBeenCalledWith(t1Path, { timing: { frame: 12 } })
  })

  it('shows a friendly hint when the trigger selection no longer resolves', () => {
    const builder = makeBuilderStub({
      program: emptyProgram(),
      selected: { kind: 'trigger', path: [{ kind: 'trigger', id: 'ghost' }] },
    })
    const wrapper = mountInspectorBar(builder)

    expect(wrapper.text()).toContain('no longer exists')
  })

  it('shows validation issues for the selected action', () => {
    const builder = makeBuilderStub({
      program: makeProgram(),
      schema: makeSchema(),
      selected: { kind: 'action', path: t1ActionA1Path },
      issues: [{ path: 'triggers[0].actions[0]', code: 'x', message: 'amount required', severity: 'error' }],
    })
    const wrapper = mountInspectorBar(builder)

    expect(wrapper.find('[data-test="inspector-bar-issues"]').text()).toContain('amount required')
  })

  it('does not show ability-level issues (those surface in the Identity tab instead)', () => {
    const builder = makeBuilderStub({
      program: makeProgram(),
      schema: makeSchema(),
      selected: { kind: 'action', path: t1ActionA1Path },
      issues: [{ path: 'identity.damageType', code: 'x', message: 'damage type required', severity: 'error' }],
    })
    const wrapper = mountInspectorBar(builder)

    expect(wrapper.find('[data-test="inspector-bar-issues"]').exists()).toBe(false)
  })

  // A crater DoT nested 3 levels deep inside create_zone's config.triggers —
  // the phase-7 acceptance shape (meteor's impact -> zone -> burn -> bdmg).
  // Proves selectedTrigger/selectedAction resolve via resolveNode at depth,
  // and that selectedPath derives the Go validator's exact
  // "...config.triggers[j].actions[k]" grammar via indexPathFor rather than
  // hand-deriving `triggers[i].actions[j]` (which cannot express this
  // nesting at all) — this is the contract that makes nested validation
  // badges work.
  function nestedCraterProgram(): AbilityProgram {
    return {
      entry: { type: 'ground_point', range: 600 },
      triggers: [
        {
          id: 'cast',
          type: 'on_cast_complete',
          actions: [
            {
              id: 'zone',
              type: 'create_zone',
              config: {
                triggers: [
                  {
                    id: 'burn',
                    type: 'on_zone_tick',
                    timing: { tickInterval: 1000 },
                    actions: [{ id: 'bdmg', type: 'deal_damage', config: { amount: 5 } }],
                  },
                ],
              },
            },
          ],
        },
      ],
    }
  }

  const bdmgPath: NodePath = [
    { kind: 'trigger', id: 'cast' },
    { kind: 'action', id: 'zone' },
    { kind: 'trigger', id: 'burn' },
    { kind: 'action', id: 'bdmg' },
  ]

  it('resolves + inspects a nested action 3 levels deep (config.triggers slot)', () => {
    const builder = makeBuilderStub({
      program: nestedCraterProgram(),
      schema: makeSchema(),
      selected: { kind: 'action', path: bdmgPath },
    })
    const wrapper = mountInspectorBar(builder)

    expect(wrapper.find('[data-test="inspector-bar-empty"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('deal_damage')
    expect(wrapper.text()).toContain('(id: bdmg)')
    const amountInput = wrapper.find('input[type="number"]')
    expect((amountInput.element as HTMLInputElement).value).toBe('5')
  })

  it("derives the nested config.triggers index path for issuesForPath, matching the Go validator's grammar", () => {
    const builder = makeBuilderStub({
      program: nestedCraterProgram(),
      schema: makeSchema(),
      selected: { kind: 'action', path: bdmgPath },
      issues: [
        {
          path: 'triggers[0].actions[0].config.triggers[0].actions[0]',
          code: 'x',
          message: 'crater damage too high',
          severity: 'error',
        },
      ],
    })
    const wrapper = mountInspectorBar(builder)

    expect(wrapper.find('[data-test="inspector-bar-issues"]').text()).toContain('crater damage too high')
  })
})
