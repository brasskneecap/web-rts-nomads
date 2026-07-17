import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref, shallowRef } from 'vue'
import type { AbilityEditorForm } from '@/game/abilities/abilityEditorForm'
import { createBlankForm } from '@/game/abilities/abilityEditorForm'
import type { AbilityProgram } from '@/game/abilities/program/abilityProgram'
import AbilityOverviewCard from './AbilityOverviewCard.vue'
import { AbilityBuilderKey } from './AbilityBuilderContext'
import { emptyProgram } from './programTree'

function makeBuilderStub(overrides: {
  form?: AbilityEditorForm
  program?: AbilityProgram
  runnable?: boolean
} = {}) {
  return {
    form: shallowRef<AbilityEditorForm>(overrides.form ?? createBlankForm()),
    program: shallowRef<AbilityProgram>(overrides.program ?? emptyProgram()),
    runnable: ref(overrides.runnable ?? true),
    select: vi.fn(),
  }
}

function mountCard(builder: ReturnType<typeof makeBuilderStub>) {
  return mount(AbilityOverviewCard, {
    global: { provide: { [AbilityBuilderKey as unknown as string]: builder } },
  })
}

describe('AbilityOverviewCard', () => {
  it('renders name, id, category/type badges, and cost/timing rows', () => {
    const builder = makeBuilderStub({
      form: {
        ...createBlankForm(),
        id: 'fireball',
        displayName: 'Fireball',
        category: 'Offense',
        type: 'spell',
        manaCost: 20,
        cooldown: 4,
        castTime: 0.5,
      },
    })
    const wrapper = mountCard(builder)

    expect(wrapper.text()).toContain('Fireball')
    expect(wrapper.text()).toContain('fireball')
    expect(wrapper.text()).toContain('Offense')
    expect(wrapper.text()).toContain('Spell')
    expect(wrapper.text()).toContain('20')
    expect(wrapper.text()).toContain('4s')
    expect(wrapper.text()).toContain('0.5s')
  })

  it('only shows stat rows that are actually set', () => {
    const builder = makeBuilderStub({
      form: { ...createBlankForm(), id: 'x', manaCost: 10 },
    })
    const wrapper = mountCard(builder)

    expect(wrapper.text()).toContain('Mana')
    expect(wrapper.text()).not.toContain('Cooldown')
    expect(wrapper.text()).not.toContain('Cast Time')
  })

  it('renders the entry-targeting summary from the program entry', () => {
    const builder = makeBuilderStub({
      program: { entry: { type: 'ground_point', relations: ['enemy'], range: 400 }, triggers: [] },
    })
    const wrapper = mountCard(builder)

    expect(wrapper.text()).toContain('Ground point · enemies · range 400')
  })

  it('renders tag chips when present', () => {
    const builder = makeBuilderStub({
      form: { ...createBlankForm(), id: 'x', tags: ['fire', 'aoe'] },
    })
    const wrapper = mountCard(builder)

    expect(wrapper.text()).toContain('fire')
    expect(wrapper.text()).toContain('aoe')
  })

  it('shows the generated description, and an override hint when description is set', () => {
    const builder = makeBuilderStub({
      form: { ...createBlankForm(), id: 'x', generatedDescription: 'Deals fire damage.' },
    })
    const wrapper = mountCard(builder)
    expect(wrapper.text()).toContain('Deals fire damage.')
    expect(wrapper.text()).not.toContain('(override)')

    const overrideBuilder = makeBuilderStub({
      form: {
        ...createBlankForm(),
        id: 'x',
        generatedDescription: 'Deals fire damage.',
        description: 'Custom prose.',
      },
    })
    const overrideWrapper = mountCard(overrideBuilder)
    expect(overrideWrapper.text()).toContain('Custom prose.')
    expect(overrideWrapper.text()).toContain('(override)')
    expect(overrideWrapper.text()).not.toContain('Deals fire damage.')
  })

  it('shows the display-only banner when runnable is false, and hides it when true', () => {
    const notRunnable = makeBuilderStub({ runnable: false })
    const wrapper = mountCard(notRunnable)
    expect(wrapper.find('[data-test="overview-display-only-banner"]').exists()).toBe(true)

    const runnable = makeBuilderStub({ runnable: true })
    const runnableWrapper = mountCard(runnable)
    expect(runnableWrapper.find('[data-test="overview-display-only-banner"]').exists()).toBe(false)
  })

  it('clicking the identity row selects the ability node and emits open-identity', async () => {
    const builder = makeBuilderStub()
    const wrapper = mountCard(builder)

    await wrapper.find('[data-test="overview-open-settings"]').trigger('click')

    expect(builder.select).toHaveBeenCalledWith({ kind: 'ability' })
    // open-identity is what lets AbilityBuilderPanel navigate to the
    // Identity tab even when `selected` was ALREADY {kind:'ability'} (a
    // same-value write a watcher on `selected` would never fire for).
    expect(wrapper.emitted('open-identity')).toHaveLength(1)
  })
})
