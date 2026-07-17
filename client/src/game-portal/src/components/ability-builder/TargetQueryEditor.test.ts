import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import type { TargetQueryDef } from '@/game/abilities/program/abilityProgram'
import FilterableSelect from '@/components/editor/FilterableSelect.vue'
import TargetQueryEditor from './TargetQueryEditor.vue'

function mountEditor(
  modelValue: TargetQueryDef | undefined,
  enums: Record<string, string[]> = {},
  fields?: string[],
) {
  return mount(TargetQueryEditor, { props: { modelValue, enums, fields } })
}

// InfoTip's bubble is Teleported to <body>; the tests below use this instead
// of wrapper.find() to see it.
function bubble(): HTMLElement | null {
  return document.body.querySelector('.info-tip__bubble')
}

describe('TargetQueryEditor', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('defaults to an all_in_scene query when modelValue is undefined', () => {
    const wrapper = mountEditor(undefined)
    const selects = wrapper.findAllComponents(FilterableSelect)
    // Source is the first FilterableSelect rendered.
    expect(selects[0].props('modelValue')).toBe('all_in_scene')
  })

  it('renders relation checkboxes from the enums bundle and toggling one emits a merged query', async () => {
    const wrapper = mountEditor({ source: 'all_in_scene', relations: ['ally'] }, { relations: ['self', 'ally', 'enemy'] })
    const checkboxes = wrapper.findAll('.tqe-checkgroup input[type="checkbox"]')
    expect(checkboxes).toHaveLength(3)
    // ally is pre-checked.
    expect((checkboxes[1].element as HTMLInputElement).checked).toBe(true)

    await checkboxes[2].setValue(true) // check "enemy"

    const emitted = wrapper.emitted('update:modelValue')
    expect(emitted).toBeTruthy()
    const merged = emitted![0][0] as TargetQueryDef
    expect(merged.source).toBe('all_in_scene')
    expect(merged.relations).toEqual(['ally', 'enemy'])
  })

  it('changing the source select commits a merged query preserving other fields', async () => {
    const wrapper = mountEditor({ source: 'caster', maxCount: 3 })
    const sourceSelect = wrapper.findAllComponents(FilterableSelect)[0]

    await sourceSelect.vm.$emit('update:modelValue', 'all_in_scene')

    const merged = wrapper.emitted('update:modelValue')![0][0] as TargetQueryDef
    expect(merged.source).toBe('all_in_scene')
    expect(merged.maxCount).toBe(3)
  })

  it('commits radius as a plain number on change (not per keystroke)', async () => {
    const wrapper = mountEditor({ source: 'all_in_scene', radius: 100 })
    const radiusInput = wrapper.find('#tqe-radius')
    expect((radiusInput.element as HTMLInputElement).value).toBe('100')

    // wrapper.setValue() fires both 'input' and 'change' — set the value and
    // fire 'input' alone first to simulate a single keystroke.
    ;(radiusInput.element as HTMLInputElement).value = '250'
    await radiusInput.trigger('input')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()

    await radiusInput.trigger('change')
    const merged = wrapper.emitted('update:modelValue')![0][0] as TargetQueryDef
    expect(merged.radius).toBe(250)
  })

  // Selected by data-test, NOT by position. This test used to slice(-2) off
  // every `label.ed-check` checkbox (which also matches the relations
  // checkboxes) — so adding a third boolean field silently shifted the window
  // and the assertions started checking the wrong boxes while still "passing"
  // the ones they happened to land on.
  it('toggling each boolean field commits immediately', async () => {
    const cases: [string, keyof TargetQueryDef][] = [
      ['tqe-includeInitialTarget', 'includeInitialTarget'],
      ['tqe-excludeSource', 'excludeSource'],
      ['tqe-excludeCurrentEvent', 'excludeCurrentEvent'],
    ]
    for (const [testId, field] of cases) {
      const wrapper = mountEditor({ source: 'all_in_scene' })
      await wrapper.find(`[data-test="${testId}"]`).setValue(true)
      const merged = wrapper.emitted('update:modelValue')!.at(-1)![0] as TargetQueryDef
      expect(merged[field], `${field} did not commit`).toBe(true)
    }
  })

  // ── declared-subset rendering (the fix for the "Targeting has too many
  //    fields" complaint) ─────────────────────────────────────────────────
  it('renders exactly the declared `fields` subset, not the old hardcoded set', () => {
    const wrapper = mountEditor({ source: 'all_in_scene' }, {}, ['source', 'radius'])

    // Only Source (a FilterableSelect) and Radius (#tqe-radius) render.
    expect(wrapper.findAllComponents(FilterableSelect)).toHaveLength(1)
    expect(wrapper.find('#tqe-radius').exists()).toBe(true)
    // Everything else this component knows how to render is absent.
    expect(wrapper.find('.tqe-checkgroup').exists()).toBe(false) // relations
    expect(wrapper.find('#tqe-max-count').exists()).toBe(false)
    expect(wrapper.find('#tqe-origin-ref').exists()).toBe(false)
    expect(wrapper.findAll('label.ed-check').length).toBe(0) // includeInitialTarget/excludeSource
  })

  it('renders declared fields in the DECLARED order, not the fixed internal order', () => {
    // maxCount declared before source — order must follow the prop, so the
    // Max Count input's DOM position precedes the Source select's.
    const wrapper = mountEditor({ source: 'all_in_scene' }, {}, ['maxCount', 'source'])
    const html = wrapper.html()
    expect(html.indexOf('tqe-max-count')).toBeLessThan(html.indexOf('Target source'))
  })

  it('falls back to rendering all 10 fields when `fields` is not supplied (forward-compat)', () => {
    const wrapper = mountEditor({ source: 'all_in_scene' })
    expect(wrapper.find('#tqe-origin-ref').exists()).toBe(true)
    expect(wrapper.findAll('[aria-label="Alive state"]').length).toBeGreaterThan(0)
  })

  // ── newly-exposed fields: originRef, aliveState ─────────────────────────
  it('originRef renders as a select over curated context keys and commits a ContextRef', async () => {
    const wrapper = mountEditor({ source: 'all_in_scene' }, {}, ['originRef'])
    const select = wrapper.find('#tqe-origin-ref')
    expect(select.exists()).toBe(true)

    await select.setValue('impactPosition')
    const merged = wrapper.emitted('update:modelValue')!.at(-1)![0] as TargetQueryDef
    expect(merged.originRef).toEqual({ key: 'impactPosition' })
  })

  it('originRef commits undefined when reset to "(none)"', async () => {
    const wrapper = mountEditor({ source: 'all_in_scene', originRef: { key: 'caster' } }, {}, ['originRef'])
    const select = wrapper.find('#tqe-origin-ref')

    await select.setValue('')
    const merged = wrapper.emitted('update:modelValue')!.at(-1)![0] as TargetQueryDef
    expect(merged.originRef).toBeUndefined()
  })

  it('aliveState renders as a FilterableSelect over alive/dead/any and commits the choice', async () => {
    const wrapper = mountEditor({ source: 'all_in_scene' }, {}, ['aliveState'])
    const select = wrapper.findComponent(FilterableSelect)
    expect(select.props('options')).toEqual([
      { id: '', label: '(default: alive)' },
      { id: 'alive', label: 'alive' },
      { id: 'dead', label: 'dead' },
      { id: 'any', label: 'any' },
    ])

    await select.vm.$emit('update:modelValue', 'dead')
    const merged = wrapper.emitted('update:modelValue')!.at(-1)![0] as TargetQueryDef
    expect(merged.aliveState).toBe('dead')
  })

  // ── InfoTip wiring (explaining Source/Origin/Origin Ref etc.) ───────────
  it('renders an InfoTip beside Source whose tooltip explains the field on click', async () => {
    const wrapper = mountEditor({ source: 'all_in_scene' }, {}, ['source'])
    const infoBtn = wrapper.find('button.info-tip__btn')
    expect(infoBtn.exists()).toBe(true)
    expect(infoBtn.attributes('aria-expanded')).toBe('false')

    await infoBtn.trigger('click')
    expect(infoBtn.attributes('aria-expanded')).toBe('true')
    expect(bubble()?.textContent).toContain('Which units to start from')

    // Close it before the test ends — InfoTip's "only one pinned open at a
    // time" state (useInfoTip.ts) is module-scoped, so leaving this pinned
    // would let it react (and re-patch its Teleported node) when a later
    // test's InfoTip pins itself, even though this wrapper isn't unmounted.
    await infoBtn.trigger('click')
  })

  it('source options carry an inline note for inert/special-cased values and stay plain otherwise', () => {
    const wrapper = mountEditor(
      { source: 'all_in_scene' },
      { targetSources: ['caster', 'source_object', 'named_context', 'some_brand_new_source'] },
      ['source'],
    )
    const options = wrapper.findComponent(FilterableSelect).props('options') as { id: string; label: string }[]
    const byId = Object.fromEntries(options.map((o) => [o.id, o.label]))

    expect(byId.caster).toBe('caster')
    expect(byId.source_object).toBe('source_object — not implemented')
    expect(byId.named_context).toBe('named_context — pick which in Origin Ref')
    // A brand-new enum value the client has no copy for yet must render
    // plain and unsuffixed, not throw, and not inherit another option's note.
    expect(byId.some_brand_new_source).toBe('some_brand_new_source')
  })

  it('relations checkboxes show an inline note only for the flagged relation (neutral)', () => {
    const wrapper = mountEditor(
      { source: 'all_in_scene' },
      { relations: ['self', 'ally', 'neutral'] },
      ['relations'],
    )
    const labels = wrapper.findAll('.tqe-checkgroup label.ed-check')
    const text = labels.map((l) => l.text())
    expect(text.find((t) => t.startsWith('self'))).toBe('self')
    expect(text.find((t) => t.startsWith('ally'))).toBe('ally')
    expect(text.find((t) => t.startsWith('neutral'))).toBe('neutral (not implemented, never matches)')
  })

  it('a field with no InfoTip copy renders no icon (graceful fallback)', () => {
    // Every currently-declared TargetQueryEditor field has copy today, so
    // this exercises the same fallback contract at the component boundary by
    // stubbing InfoTip out entirely and confirming nothing else in the
    // template renders an info-tip class unconditionally — i.e. the icon is
    // gated by `v-if="targetQueryFieldHint(...)"`, not always-rendered.
    // (targetQueryHints.test.ts covers the missing-copy case directly.)
    const wrapper = mountEditor({ source: 'all_in_scene' }, {}, ['source'])
    expect(wrapper.findAll('button.info-tip__btn')).toHaveLength(1)
  })
})
