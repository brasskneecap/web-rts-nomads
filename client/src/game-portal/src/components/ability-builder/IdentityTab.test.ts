import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref, shallowRef } from 'vue'
import type { AbilityEditorForm } from '@/game/abilities/abilityEditorForm'
import { createBlankForm } from '@/game/abilities/abilityEditorForm'
import type { AbilityProgram } from '@/game/abilities/program/abilityProgram'
import type { ActionSchemaBundle } from '@/game/abilities/program/programSchema'
import type { ValidationIssue } from '@/game/abilities/program/programValidation'
import IdentityTab from './IdentityTab.vue'
import { AbilityBuilderKey } from './AbilityBuilderContext'
import type { AbilityBuilderCatalogs } from './useAbilityBuilder'
import type { NodeRef } from './programTree'
import { emptyProgram } from './programTree'

function emptyCatalogs(): AbilityBuilderCatalogs {
  return { effects: [], projectiles: [], damageTypes: [], categories: [], autoCastSelectors: [], unitTypes: [] }
}

// makeBuilderStub mirrors ItemInspector.test.ts's precedent: real refs for
// the reads IdentityTab consumes, spies for every op it can call.
function makeBuilderStub(overrides: {
  form?: AbilityEditorForm
  program?: AbilityProgram
  schema?: ActionSchemaBundle | null
  catalogs?: AbilityBuilderCatalogs
  selected?: NodeRef
  issues?: ValidationIssue[]
} = {}) {
  const form = shallowRef<AbilityEditorForm>(overrides.form ?? createBlankForm())
  return {
    form,
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
    // params + its ops: read/called by the nested ParametersCard (see
    // ParametersCard.test.ts for its own dedicated coverage — this stub only
    // needs to exist so mounting IdentityTab doesn't throw).
    params: shallowRef<Record<string, number>>(form.value.params ?? {}),
    setParam: vi.fn(),
    removeParam: vi.fn(),
    setParamForRank: vi.fn(),
    renameParam: vi.fn(),
  }
}

function mountIdentityTab(builder: ReturnType<typeof makeBuilderStub>) {
  return mount(IdentityTab, {
    global: { provide: { [AbilityBuilderKey as unknown as string]: builder } },
  })
}

describe('IdentityTab', () => {
  it("renders the ability's identity fields regardless of flow selection", () => {
    const builder = makeBuilderStub({
      form: { ...createBlankForm(), id: 'fireball', displayName: 'Fireball', manaCost: 20 },
      // Selection points at a trigger, not the ability — IdentityTab must
      // still render: it's no longer selection-gated like the old rail.
      selected: { kind: 'trigger', path: [{ kind: 'trigger', id: 't1' }] },
    })
    const wrapper = mountIdentityTab(builder)

    expect(wrapper.text()).toContain('Identity')
    expect(wrapper.text()).toContain('Cast Setup')
    expect(wrapper.text()).toContain('Entry (read-only)')
    const displayNameInput = wrapper.find('input[type="text"]')
    expect((displayNameInput.element as HTMLInputElement).value).toBe('Fireball')
  })

  it('committing an identity field on blur calls builder.updateForm once, not per keystroke', async () => {
    const builder = makeBuilderStub({
      form: { ...createBlankForm(), id: 'fireball', displayName: 'Fireball' },
    })
    const wrapper = mountIdentityTab(builder)

    const displayNameInput = wrapper.find('input[type="text"]')
    // NOTE: VTU's wrapper.setValue() fires BOTH 'input' and 'change' (see
    // SchemaField.test.ts), so keystrokes are simulated by setting .value
    // and triggering 'input' alone, matching what a real browser does per
    // character typed — 'input' alone must NOT commit.
    const el = displayNameInput.element as HTMLInputElement
    el.value = 'M'
    await displayNameInput.trigger('input')
    el.value = 'Me'
    await displayNameInput.trigger('input')
    el.value = 'Meteor'
    await displayNameInput.trigger('input')
    expect(builder.updateForm).not.toHaveBeenCalled()

    // Commit happens on `change` (blur), exactly once, with the final value.
    await displayNameInput.trigger('change')
    expect(builder.updateForm).toHaveBeenCalledTimes(1)
    expect(builder.updateForm).toHaveBeenCalledWith({ displayName: 'Meteor' })
  })

  it('shows the read-only Entry summary sourced from the program', () => {
    const builder = makeBuilderStub({
      program: { entry: { type: 'unit', range: 450, relations: ['enemy'] }, triggers: [] },
    })
    const wrapper = mountIdentityTab(builder)

    expect(wrapper.text()).toContain('Type: unit')
    expect(wrapper.text()).toContain('Range: 450')
    expect(wrapper.text()).toContain('enemy')
  })

  it('shows ability-level validation issues but not trigger/action-scoped ones', () => {
    const builder = makeBuilderStub({
      issues: [
        { path: 'identity.damageType', code: 'x', message: 'damage type required', severity: 'error' },
        { path: 'triggers[0].actions[0]', code: 'y', message: 'amount required', severity: 'error' },
      ],
    })
    const wrapper = mountIdentityTab(builder)

    const issues = wrapper.find('[data-test="identity-issues"]')
    expect(issues.text()).toContain('damage type required')
    expect(issues.text()).not.toContain('amount required')
  })

  it('committing tags parses the comma-separated text into an array', async () => {
    const builder = makeBuilderStub({
      form: { ...createBlankForm(), tags: [] },
    })
    const wrapper = mountIdentityTab(builder)

    const tagsInput = wrapper.find('#ins-tags')
    await tagsInput.setValue('fire, aoe , burst')
    await tagsInput.trigger('change')

    expect(builder.updateForm).toHaveBeenCalledWith({ tags: ['fire', 'aoe', 'burst'] })
  })
})
