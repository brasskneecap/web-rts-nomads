import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref, shallowRef } from 'vue'
import type { AbilityEditorForm, AuthoredAbilityDef } from '@/game/abilities/abilityEditorForm'
import { createBlankForm } from '@/game/abilities/abilityEditorForm'
import type { AbilityProgram } from '@/game/abilities/program/abilityProgram'
import type { ActionSchemaBundle } from '@/game/abilities/program/programSchema'
import type { ValidationIssue } from '@/game/abilities/program/programValidation'
import { ACTION_ICON_MAP, initActionIcons } from '@/game/maps/actionIconDefs'
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

// A producing action (select_targets) whose result can be named via
// outputs.targets — the F3 "Save result as" surface.
function makeSelectProgram(outputs?: Record<string, string>): AbilityProgram {
  return {
    entry: { type: 'unit', range: 300 },
    triggers: [
      {
        id: 't1',
        type: 'on_cast_complete',
        actions: [{ id: 'a1', type: 'select_targets', target: { source: 'all_in_scene' }, ...(outputs ? { outputs } : {}) }],
      },
    ],
  }
}

function selectSchema(): ActionSchemaBundle {
  return { actions: [{ type: 'select_targets', runnable: true, fields: [] }], enums: {} }
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
  abilities?: AuthoredAbilityDef[]
  selected?: NodeRef
  issues?: ValidationIssue[]
} = {}) {
  const form = shallowRef<AbilityEditorForm>(overrides.form ?? createBlankForm())
  return {
    form,
    program: shallowRef<AbilityProgram>(overrides.program ?? emptyProgram()),
    schema: shallowRef<ActionSchemaBundle | null>(overrides.schema ?? null),
    catalogs: shallowRef<AbilityBuilderCatalogs>(overrides.catalogs ?? emptyCatalogs()),
    abilities: shallowRef<AuthoredAbilityDef[]>(overrides.abilities ?? []),
    selected: shallowRef<NodeRef>(overrides.selected ?? { kind: 'ability' }),
    issues: ref<ValidationIssue[]>(overrides.issues ?? []),
    updateForm: vi.fn(),
    updateAction: vi.fn(),
    updateActionConfig: vi.fn(),
    updateTrigger: vi.fn(),
    select: vi.fn(),
    // params: threaded down to every SchemaField this bar renders, so a
    // number/duration/percentage field can offer Parameter mode (see
    // SchemaField.vue) — defaults from the stubbed form so a test can just
    // set `form.params` rather than wiring both up separately.
    params: shallowRef<Record<string, number>>(form.value.params ?? {}),
    setParam: vi.fn(),
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


  it('shows a "Save result as" field for a producing action and commits outputs.targets', async () => {
    const builder = makeBuilderStub({
      program: makeSelectProgram(),
      schema: selectSchema(),
      selected: { kind: 'action', path: t1ActionA1Path },
    })
    const wrapper = mountInspectorBar(builder)

    expect(wrapper.text()).toContain('Save result as')
    const input = wrapper.find('input[type="text"]')
    const el = input.element as HTMLInputElement
    el.value = 'marked'
    await input.trigger('input')
    await input.trigger('change')
    expect(builder.updateAction).toHaveBeenCalledWith(t1ActionA1Path, { outputs: { targets: 'marked' } })
  })

  it('does not show "Save result as" for a non-producing action', () => {
    const builder = makeBuilderStub({
      program: makeProgram(),
      schema: makeSchema(),
      selected: { kind: 'action', path: t1ActionA1Path },
    })
    const wrapper = mountInspectorBar(builder)
    expect(wrapper.text()).not.toContain('Save result as')
  })

  it('prefills "Save result as" from outputs.targets and clears the output when emptied', async () => {
    const builder = makeBuilderStub({
      program: makeSelectProgram({ targets: 'marked' }),
      schema: selectSchema(),
      selected: { kind: 'action', path: t1ActionA1Path },
    })
    const wrapper = mountInspectorBar(builder)

    const input = wrapper.find('input[type="text"]')
    expect((input.element as HTMLInputElement).value).toBe('marked')
    const el = input.element as HTMLInputElement
    el.value = ''
    await input.trigger('input')
    await input.trigger('change')
    expect(builder.updateAction).toHaveBeenCalledWith(t1ActionA1Path, { outputs: undefined })
  })

  it('warns when a saved name is never read back (dead save)', () => {
    const builder = makeBuilderStub({
      program: makeSelectProgram({ targets: 'hit' }), // saved, but nothing reads "hit"
      schema: selectSchema(),
      selected: { kind: 'action', path: t1ActionA1Path },
    })
    const wrapper = mountInspectorBar(builder)

    const warn = wrapper.find('[data-test="ib-unread-save"]')
    expect(warn.exists()).toBe(true)
    expect(warn.text()).toContain('"hit"')
    expect(warn.text()).toContain('no effect')
  })

  it('does not warn when the saved name is read back by a later query', () => {
    const program: AbilityProgram = {
      entry: { type: 'unit', range: 300 },
      triggers: [
        {
          id: 't1',
          type: 'on_cast_complete',
          actions: [
            { id: 'a1', type: 'select_targets', target: { source: 'all_in_scene' }, outputs: { targets: 'hit' } },
            // a sibling reads "hit" back via excludeRef → not dead
            { id: 'a2', type: 'select_targets', target: { source: 'all_in_scene', excludeRef: { key: 'hit' } } },
          ],
        },
      ],
    }
    const builder = makeBuilderStub({
      program,
      schema: selectSchema(),
      selected: { kind: 'action', path: t1ActionA1Path },
    })
    const wrapper = mountInspectorBar(builder)
    expect(wrapper.find('[data-test="ib-unread-save"]').exists()).toBe(false)
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
                    type: 'on_tick',
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

  // ── target-query schema fallback removal ────────────────────────────────
  // deal_damage's real server schema (amount, type) declares NO target_query
  // field at all — this must render NO Targeting section, proving the old
  // blanket `!hasTargetQueryField` fallback (which unconditionally rendered
  // TargetQueryEditor for any action lacking a target_query field) is gone.
  it('shows no Targeting section for an action whose schema declares no target_query field', () => {
    const builder = makeBuilderStub({
      program: makeProgram(),
      schema: {
        actions: [
          {
            type: 'deal_damage',
            runnable: true,
            fields: [
              { key: 'amount', label: 'Amount', control: 'number', section: 'Properties' },
              { key: 'type', label: 'Damage Type', control: 'enum', section: 'Properties' },
            ],
          },
        ],
        enums: {},
      },
      selected: { kind: 'action', path: t1ActionA1Path },
    })
    const wrapper = mountInspectorBar(builder)

    expect(wrapper.text()).not.toContain('Targeting')
  })

  // ── showWhen conditional field visibility ───────────────────────────────
  function launchProjectileSchema(): ActionSchemaBundle {
    return {
      actions: [
        {
          type: 'launch_projectile',
          runnable: true,
          fields: [
            { key: 'travelMode', label: 'Travel Mode', control: 'enum', section: 'Properties', options: ['to_target', 'direction'] },
            {
              key: 'target',
              label: 'Target',
              control: 'target_query',
              section: 'Targeting',
              targetQueryFields: ['source', 'radius'],
              showWhen: { key: 'travelMode', op: 'ne', value: 'direction' },
            },
            {
              key: 'distance',
              label: 'Distance',
              control: 'number',
              section: 'Properties',
              showWhen: { key: 'travelMode', op: 'eq', value: 'direction' },
            },
            { key: 'chainCount', label: 'Chain Count', control: 'number', section: 'Properties' },
            {
              key: 'amount',
              label: 'Amount (chain only)',
              control: 'number',
              section: 'Properties',
              showWhen: { key: 'chainCount', op: 'gt', value: 0 },
            },
            {
              key: 'type',
              label: 'Damage Type (chain only)',
              control: 'enum',
              section: 'Properties',
              showWhen: { key: 'chainCount', op: 'gt', value: 0 },
            },
            {
              key: 'bounceRange',
              label: 'Bounce Range',
              control: 'number',
              section: 'Properties',
              showWhen: { key: 'chainCount', op: 'gt', value: 0 },
            },
            {
              key: 'bounceDamageFalloff',
              label: 'Bounce Damage Falloff',
              control: 'number',
              section: 'Properties',
              showWhen: { key: 'chainCount', op: 'gt', value: 0 },
            },
          ],
        },
      ],
      enums: {},
    }
  }

  function launchProjectileProgram(config: Record<string, unknown>): AbilityProgram {
    return {
      entry: { type: 'unit', range: 300 },
      triggers: [
        {
          id: 't1',
          type: 'on_cast_complete',
          actions: [{ id: 'a1', type: 'launch_projectile', config }],
        },
      ],
    }
  }

  it('with chainCount: 0, hides the 4 chain-only fields', () => {
    const builder = makeBuilderStub({
      program: launchProjectileProgram({ travelMode: 'to_target', chainCount: 0 }),
      schema: launchProjectileSchema(),
      selected: { kind: 'action', path: t1ActionA1Path },
    })
    const wrapper = mountInspectorBar(builder)

    expect(wrapper.text()).not.toContain('Amount (chain only)')
    expect(wrapper.text()).not.toContain('Damage Type (chain only)')
    expect(wrapper.text()).not.toContain('Bounce Range')
    expect(wrapper.text()).not.toContain('Bounce Damage Falloff')
  })

  it('with chainCount omitted entirely, chain-only fields stay hidden (missing key = zero value)', () => {
    const builder = makeBuilderStub({
      program: launchProjectileProgram({ travelMode: 'to_target' }),
      schema: launchProjectileSchema(),
      selected: { kind: 'action', path: t1ActionA1Path },
    })
    const wrapper = mountInspectorBar(builder)

    expect(wrapper.text()).not.toContain('Bounce Range')
  })

  it('with chainCount: 2, shows the 4 chain-only fields', () => {
    const builder = makeBuilderStub({
      program: launchProjectileProgram({ travelMode: 'to_target', chainCount: 2 }),
      schema: launchProjectileSchema(),
      selected: { kind: 'action', path: t1ActionA1Path },
    })
    const wrapper = mountInspectorBar(builder)

    expect(wrapper.text()).toContain('Amount (chain only)')
    expect(wrapper.text()).toContain('Damage Type (chain only)')
    expect(wrapper.text()).toContain('Bounce Range')
    expect(wrapper.text()).toContain('Bounce Damage Falloff')
  })

  it('travelMode "direction" hides Target and shows Distance', () => {
    const builder = makeBuilderStub({
      program: launchProjectileProgram({ travelMode: 'direction' }),
      schema: launchProjectileSchema(),
      selected: { kind: 'action', path: t1ActionA1Path },
    })
    const wrapper = mountInspectorBar(builder)

    expect(wrapper.text()).not.toContain('Targeting')
    expect(wrapper.text()).toContain('Distance')
  })

  // Proves a newly-exposed TargetQueryDef sub-field (excludeSource — reachable
  // for the first time via the declared targetQueryFields subset) routes
  // through the SAME builder op as every other target edit: updateAction
  // with the merged TargetQueryDef, at the selected action's path.
  it('editing a newly-exposed target-query field (excludeSource) commits via updateAction at the right path', async () => {
    const selectTargetsSchema: ActionSchemaBundle = {
      actions: [
        {
          type: 'select_targets',
          runnable: true,
          fields: [
            {
              key: 'target',
              label: 'Target Query',
              control: 'target_query',
              section: 'Targeting',
              targetQueryFields: [
                'source', 'origin', 'originRef', 'relations', 'radius',
                'ordering', 'maxCount', 'includeInitialTarget', 'excludeSource', 'aliveState',
              ],
            },
          ],
        },
      ],
      enums: {},
    }
    const program: AbilityProgram = {
      entry: { type: 'unit', range: 300 },
      triggers: [
        {
          id: 't1',
          type: 'on_cast_complete',
          actions: [{ id: 'a1', type: 'select_targets', target: { source: 'all_in_scene' } }],
        },
      ],
    }
    const builder = makeBuilderStub({
      program,
      schema: selectTargetsSchema,
      selected: { kind: 'action', path: t1ActionA1Path },
    })
    const wrapper = mountInspectorBar(builder)

    // excludeSource is the second-to-last checkbox rendered (the last is
    // aliveState, a select, so the checkboxes are includeInitialTarget then
    // excludeSource, per the declared order).
    const checkboxes = wrapper.findAll('input[type="checkbox"]')
    const excludeSourceCheckbox = checkboxes[checkboxes.length - 1]
    await excludeSourceCheckbox.setValue(true)

    expect(builder.updateAction).toHaveBeenCalledTimes(1)
    expect(builder.updateAction).toHaveBeenCalledWith(t1ActionA1Path, {
      target: { source: 'all_in_scene', excludeSource: true },
    })
  })

  it('travelMode "to_target" (the inverse) shows Target and hides Distance', () => {
    const builder = makeBuilderStub({
      program: launchProjectileProgram({ travelMode: 'to_target' }),
      schema: launchProjectileSchema(),
      selected: { kind: 'action', path: t1ActionA1Path },
    })
    const wrapper = mountInspectorBar(builder)

    expect(wrapper.text()).toContain('Targeting')
    expect(wrapper.text()).not.toContain('Distance')
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

  // ── Damage Scope (on_damage_dealt trigger) ──────────────────────────────
  function makeDamageDealtProgram(damageScope?: { categories?: string[]; abilityId?: string }): AbilityProgram {
    return {
      entry: { type: 'unit', range: 300 },
      triggers: [
        {
          id: 't1',
          type: 'on_damage_dealt',
          ...(damageScope ? { damageScope } : {}),
          actions: [{ id: 'a1', type: 'deal_damage', config: { amount: 10 } }],
        },
      ],
    }
  }

  it('renders the Damage Scope section for an on_damage_dealt trigger', () => {
    const builder = makeBuilderStub({
      program: makeDamageDealtProgram(),
      selected: { kind: 'trigger', path: t1Path },
    })
    const wrapper = mountInspectorBar(builder)

    expect(wrapper.find('[data-test="damage-scope-categories"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Damage Scope')
    expect(wrapper.text()).toContain('Basic Attack')
    expect(wrapper.text()).toContain('Specific Ability')
  })

  it('does NOT render the Damage Scope section for another trigger type', () => {
    const builder = makeBuilderStub({
      program: makeProgram(), // t1 is on_cast_complete
      selected: { kind: 'trigger', path: t1Path },
    })
    const wrapper = mountInspectorBar(builder)

    expect(wrapper.find('[data-test="damage-scope-categories"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Damage Scope')
  })

  it('uses the schema-published damageCategories enum when loaded', () => {
    const schema: ActionSchemaBundle = { actions: [], enums: { damageCategories: ['ability', 'trap'] } }
    const builder = makeBuilderStub({
      program: makeDamageDealtProgram(),
      schema,
      selected: { kind: 'trigger', path: t1Path },
    })
    const wrapper = mountInspectorBar(builder)

    const section = wrapper.find('[data-test="damage-scope-categories"]')
    expect(section.text()).toContain('Ability')
    expect(section.text()).toContain('Trap')
    expect(section.text()).not.toContain('Basic Attack')
  })

  it('selecting a category writes damageScope.categories', async () => {
    const builder = makeBuilderStub({
      program: makeDamageDealtProgram(),
      selected: { kind: 'trigger', path: t1Path },
    })
    const wrapper = mountInspectorBar(builder)

    const basicAttackCheckbox = wrapper
      .findAll('[data-test="damage-scope-categories"] input[type="checkbox"]')[0]
    await basicAttackCheckbox.setValue(true)

    expect(builder.updateTrigger).toHaveBeenCalledTimes(1)
    expect(builder.updateTrigger).toHaveBeenCalledWith(t1Path, {
      damageScope: { categories: ['basic_attack'] },
    })
  })

  it('clearing all categories omits damageScope.categories', async () => {
    const builder = makeBuilderStub({
      program: makeDamageDealtProgram({ categories: ['basic_attack'] }),
      selected: { kind: 'trigger', path: t1Path },
    })
    const wrapper = mountInspectorBar(builder)

    const basicAttackCheckbox = wrapper
      .findAll('[data-test="damage-scope-categories"] input[type="checkbox"]')[0]
    expect((basicAttackCheckbox.element as HTMLInputElement).checked).toBe(true)
    await basicAttackCheckbox.setValue(false)

    expect(builder.updateTrigger).toHaveBeenCalledWith(t1Path, { damageScope: undefined })
  })

  it('setting an abilityId writes it on blur (change), not per keystroke', async () => {
    const builder = makeBuilderStub({
      program: makeDamageDealtProgram(),
      selected: { kind: 'trigger', path: t1Path },
    })
    const wrapper = mountInspectorBar(builder)

    const abilityIdInput = wrapper.find('#ib-damage-scope-ability-id')
    const el = abilityIdInput.element as HTMLInputElement
    el.value = 'fireball'
    await abilityIdInput.trigger('input')
    expect(builder.updateTrigger).not.toHaveBeenCalled()

    await abilityIdInput.trigger('change')
    expect(builder.updateTrigger).toHaveBeenCalledTimes(1)
    expect(builder.updateTrigger).toHaveBeenCalledWith(t1Path, {
      damageScope: { abilityId: 'fireball' },
    })
  })

  it('clearing the abilityId omits it', async () => {
    const builder = makeBuilderStub({
      program: makeDamageDealtProgram({ abilityId: 'fireball' }),
      selected: { kind: 'trigger', path: t1Path },
    })
    const wrapper = mountInspectorBar(builder)

    const abilityIdInput = wrapper.find('#ib-damage-scope-ability-id')
    const el = abilityIdInput.element as HTMLInputElement
    expect(el.value).toBe('fireball')
    el.value = ''
    await abilityIdInput.trigger('input')
    await abilityIdInput.trigger('change')

    expect(builder.updateTrigger).toHaveBeenCalledWith(t1Path, { damageScope: undefined })
  })

  it('an untouched on_damage_dealt trigger never carries a damageScope key (round-trip clean)', () => {
    const program = makeDamageDealtProgram()
    expect(program.triggers[0]).not.toHaveProperty('damageScope')
  })

  it('offers authored ability ids in the "Specific Ability" datalist', () => {
    const builder = makeBuilderStub({
      program: makeDamageDealtProgram(),
      abilities: [{ id: 'fireball' }, { id: 'frost_nova' }],
      selected: { kind: 'trigger', path: t1Path },
    })
    const wrapper = mountInspectorBar(builder)

    const options = wrapper.findAll('#ib-damage-scope-ability-ids option').map((o) => o.attributes('value'))
    expect(options).toEqual(['fireball', 'frost_nova'])
  })

  it('shows a non-blocking hint when abilityId is set but Categories excludes "ability"', () => {
    const builder = makeBuilderStub({
      program: makeDamageDealtProgram({ categories: ['trap'], abilityId: 'fireball' }),
      selected: { kind: 'trigger', path: t1Path },
    })
    const wrapper = mountInspectorBar(builder)

    const warn = wrapper.find('[data-test="damage-scope-contradiction"]')
    expect(warn.exists()).toBe(true)
    expect(warn.text()).toContain('Ability')
  })

  it('shows no contradiction hint when Categories includes "ability"', () => {
    const builder = makeBuilderStub({
      program: makeDamageDealtProgram({ categories: ['ability'], abilityId: 'fireball' }),
      selected: { kind: 'trigger', path: t1Path },
    })
    const wrapper = mountInspectorBar(builder)

    expect(wrapper.find('[data-test="damage-scope-contradiction"]').exists()).toBe(false)
  })

  // ── "duration is its own action" — change_stat (Stat/Op/Value) + apply_mark
  // (icon/iconKind) ──────────────────────────────────────────────────────────
  // apply_status no longer carries statModifiers/icon/iconKind at all (see
  // applyStatusConfig's doc comment, ability_exec_actions.go) — those effects
  // are now authored as change_stat / apply_mark actions nested inside an
  // apply_status_duration container (ability_status_duration.go). Schemas
  // below mirror the server's REAL registrations for these three action
  // types exactly (ListStatIDs() for change_stat's "stat" — the full
  // registry, aura-only stats included, since the SERVER schema doesn't
  // pre-filter them; the client custom control does).
  function changeStatSchema(): ActionSchemaBundle {
    return {
      actions: [
        {
          type: 'change_stat',
          runnable: true,
          fields: [
            {
              key: 'stat', label: 'Stat', control: 'enum', section: 'Properties',
              options: ['armor', 'armorPercent', 'healingReceived', 'moveSpeed', 'projectileDamageReduction'],
            },
            { key: 'op', label: 'Operation', control: 'enum', options: ['add', 'multiply'], section: 'Properties' },
            { key: 'value', label: 'Value', control: 'number', section: 'Properties' },
            { key: 'stage', label: 'Stage', control: 'enum', options: ['intrinsic', 'base', 'final'], section: 'Advanced' },
          ],
        },
      ],
      enums: {},
    }
  }

  function changeStatProgram(config: Record<string, unknown>): AbilityProgram {
    return {
      entry: { type: 'unit', range: 300 },
      triggers: [
        {
          id: 't1',
          type: 'on_cast_complete',
          actions: [{ id: 'a1', type: 'change_stat', config }],
        },
      ],
    }
  }

  function applyMarkSchema(): ActionSchemaBundle {
    return {
      actions: [
        {
          type: 'apply_mark',
          runnable: true,
          fields: [
            { key: 'icon', label: 'Overhead Icon', control: 'enum', section: 'Properties' },
            {
              key: 'iconKind', label: 'Icon Channel', control: 'enum', options: ['buff', 'debuff'], section: 'Properties',
              showWhen: { key: 'icon', op: 'ne', value: '' },
            },
          ],
        },
      ],
      enums: { icon: ['debuff-weakened', 'debuff-slowed'] },
    }
  }

  function applyMarkProgram(config: Record<string, unknown>): AbilityProgram {
    return {
      entry: { type: 'unit', range: 300 },
      triggers: [
        {
          id: 't1',
          type: 'on_cast_complete',
          actions: [{ id: 'a1', type: 'apply_mark', config }],
        },
      ],
    }
  }

  it('renders the Stat/Operation/Value editor for a change_stat action, populated from config', () => {
    const builder = makeBuilderStub({
      program: changeStatProgram({ stat: 'armor', op: 'add', value: -50 }),
      schema: changeStatSchema(),
      selected: { kind: 'action', path: t1ActionA1Path },
    })
    const wrapper = mountInspectorBar(builder)

    const statSelect = wrapper.find('select[aria-label="Stat"]')
    expect((statSelect.element as HTMLSelectElement).value).toBe('armor')

    const valueInput = wrapper.find('input[type="number"]')
    expect((valueInput.element as HTMLInputElement).value).toBe('-50')
  })

  it('does not offer aura-only stats (armorPercent, projectileDamageReduction) in the change_stat Stat dropdown', () => {
    const builder = makeBuilderStub({
      program: changeStatProgram({ stat: 'armor', op: 'add', value: -50 }),
      schema: changeStatSchema(),
      selected: { kind: 'action', path: t1ActionA1Path },
    })
    const wrapper = mountInspectorBar(builder)

    const statSelect = wrapper.find('select[aria-label="Stat"]')
    const optionValues = statSelect.findAll('option').map((o) => o.attributes('value'))
    expect(optionValues).not.toContain('armorPercent')
    expect(optionValues).not.toContain('projectileDamageReduction')
    expect(optionValues).toContain('armor')
  })

  it('editing the Stat dropdown commits config.stat', async () => {
    const builder = makeBuilderStub({
      program: changeStatProgram({ stat: 'armor', op: 'add', value: -50 }),
      schema: changeStatSchema(),
      selected: { kind: 'action', path: t1ActionA1Path },
    })
    const wrapper = mountInspectorBar(builder)

    const statSelect = wrapper.find('select[aria-label="Stat"]')
    await statSelect.setValue('moveSpeed')

    expect(builder.updateActionConfig).toHaveBeenCalledWith(t1ActionA1Path, { stat: 'moveSpeed' })
  })

  it('editing the Value field for change_stat commits config.value on change', async () => {
    const builder = makeBuilderStub({
      program: changeStatProgram({ stat: 'armor', op: 'add', value: -50 }),
      schema: changeStatSchema(),
      selected: { kind: 'action', path: t1ActionA1Path },
    })
    const wrapper = mountInspectorBar(builder)

    const valueInput = wrapper.find('input[type="number"]')
    ;(valueInput.element as HTMLInputElement).value = '-30'
    await valueInput.trigger('input')
    await valueInput.trigger('change')

    expect(builder.updateActionConfig).toHaveBeenCalledWith(t1ActionA1Path, { value: -30 })
  })

  it('apply_status no longer shows a Change Status section, a Stat dropdown, or an Overhead Icon field', () => {
    const legacyApplyStatusSchema: ActionSchemaBundle = {
      actions: [
        {
          type: 'apply_status',
          runnable: true,
          fields: [
            { key: 'status', label: 'Status', control: 'enum', options: ['slow', 'stun', 'burn'], section: 'Properties' },
            { key: 'duration', label: 'Duration', control: 'duration', section: 'Timing' },
          ],
        },
      ],
      enums: {},
    }
    const builder = makeBuilderStub({
      program: {
        entry: { type: 'unit', range: 300 },
        triggers: [
          { id: 't1', type: 'on_cast_complete', actions: [{ id: 'a1', type: 'apply_status', config: { status: 'slow', duration: 4 } }] },
        ],
      },
      schema: legacyApplyStatusSchema,
      selected: { kind: 'action', path: t1ActionA1Path },
    })
    const wrapper = mountInspectorBar(builder)

    expect(wrapper.text()).not.toContain('Change Status')
    expect(wrapper.text()).not.toContain('Overhead Icon')
    expect(wrapper.find('select[aria-label="Stat"]').exists()).toBe(false)
  })

  // ── apply_mark's icon field: visual OverheadIconPicker ──────────────────
  // iconKind is now fully DERIVED from the chosen icon id's prefix (see
  // InspectorBar's commitApplyMarkIcon + actionIconDefs' iconKindForId) —
  // there is no more separate "Icon Channel" control for apply_mark to show
  // or hide, so the old showWhen-driven assertions are gone along with it.
  function findIconOption(wrapper: ReturnType<typeof mountInspectorBar>, id: string) {
    const el = wrapper.find(`[data-test="overhead-icon-option-${id}"]`)
    if (!el.exists()) throw new Error(`no overhead icon option for ${id}`)
    return el
  }

  it('renders the icon picker from the schema enums bundle, with the configured icon marked selected, and never shows Icon Channel', () => {
    const builder = makeBuilderStub({
      program: applyMarkProgram({ icon: 'debuff-weakened', iconKind: 'debuff' }),
      schema: applyMarkSchema(),
      selected: { kind: 'action', path: t1ActionA1Path },
    })
    const wrapper = mountInspectorBar(builder)

    expect(wrapper.text()).toContain('Overhead Icon')
    expect(wrapper.text()).not.toContain('Icon Channel')

    const options = wrapper.findAll('[data-test="overhead-icon-picker"] [role="option"]')
    // "None" + the 2 enums.icon options.
    expect(options.length).toBe(3)

    const selected = findIconOption(wrapper, 'debuff-weakened')
    expect(selected.attributes('aria-selected')).toBe('true')
    const unselected = findIconOption(wrapper, 'debuff-slowed')
    expect(unselected.attributes('aria-selected')).toBe('false')
  })

  it('selecting an icon commits both icon and the derived iconKind in one write', async () => {
    const builder = makeBuilderStub({
      program: applyMarkProgram({}),
      schema: applyMarkSchema(),
      selected: { kind: 'action', path: t1ActionA1Path },
    })
    const wrapper = mountInspectorBar(builder)

    await findIconOption(wrapper, 'debuff-weakened').trigger('click')

    expect(builder.updateActionConfig).toHaveBeenCalledWith(t1ActionA1Path, {
      icon: 'debuff-weakened',
      iconKind: 'debuff',
    })
  })

  it('clearing the icon (the "none" option) omits both icon and iconKind', async () => {
    const builder = makeBuilderStub({
      program: applyMarkProgram({ icon: 'debuff-weakened', iconKind: 'debuff' }),
      schema: applyMarkSchema(),
      selected: { kind: 'action', path: t1ActionA1Path },
    })
    const wrapper = mountInspectorBar(builder)

    await wrapper.find('[data-test="overhead-icon-option-none"]').trigger('click')

    expect(builder.updateActionConfig).toHaveBeenCalledWith(t1ActionA1Path, { icon: undefined, iconKind: undefined })
  })

  it('renders the actual icon art (an SVG path from ACTION_ICON_MAP) for a loaded icon', () => {
    if (ACTION_ICON_MAP.size === 0) {
      initActionIcons([
        { id: 'debuff-weakened', path: 'M4 4h16v16H4z' },
        { id: 'debuff-slowed', path: 'M2 2h20v20H2z' },
      ])
    }
    const builder = makeBuilderStub({
      program: applyMarkProgram({ icon: 'debuff-weakened' }),
      schema: applyMarkSchema(),
      selected: { kind: 'action', path: t1ActionA1Path },
    })
    const wrapper = mountInspectorBar(builder)

    const option = findIconOption(wrapper, 'debuff-weakened')
    const path = option.find('svg path')
    expect(path.exists()).toBe(true)
    expect(path.attributes('d')).toBe(ACTION_ICON_MAP.get('debuff-weakened'))
  })

  it('degrades gracefully to id labels when ACTION_ICON_MAP has no art for an option (no crash)', () => {
    const builder = makeBuilderStub({
      program: applyMarkProgram({}),
      schema: {
        actions: [
          {
            type: 'apply_mark',
            runnable: true,
            fields: [{ key: 'icon', label: 'Overhead Icon', control: 'enum', section: 'Properties' }],
          },
        ],
        enums: { icon: ['debuff-unregistered-icon-id'] },
      },
      selected: { kind: 'action', path: t1ActionA1Path },
    })
    const wrapper = mountInspectorBar(builder)

    const option = findIconOption(wrapper, 'debuff-unregistered-icon-id')
    expect(option.find('svg').exists()).toBe(false)
    expect(option.text()).toContain('debuff-unregistered-icon-id')
  })
})
