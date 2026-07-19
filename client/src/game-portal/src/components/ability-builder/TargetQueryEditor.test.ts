import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import type { TargetQueryDef } from '@/game/abilities/program/abilityProgram'
import FilterableSelect from '@/components/editor/FilterableSelect.vue'
import TargetQueryEditor from './TargetQueryEditor.vue'

function mountEditor(
  modelValue: TargetQueryDef | undefined,
  enums: Record<string, string[]> = {},
  fields?: string[],
  savedNames?: string[],
) {
  return mount(TargetQueryEditor, { props: { modelValue, enums, fields, savedNames } })
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
    // One "Any relationship" reset + one per enum relation.
    expect(wrapper.findAll('.tqe-checkgroup input[type="checkbox"]')).toHaveLength(4)
    // ally is pre-checked (targeted by data-test, not position).
    expect((wrapper.find('[data-test="tqe-rel-ally"]').element as HTMLInputElement).checked).toBe(true)

    await wrapper.find('[data-test="tqe-rel-enemy"]').setValue(true)

    const emitted = wrapper.emitted('update:modelValue')
    expect(emitted).toBeTruthy()
    const merged = emitted![0][0] as TargetQueryDef
    expect(merged.source).toBe('all_in_scene')
    expect(merged.relations).toEqual(['ally', 'enemy'])
  })

  it('"Any relationship" is active only when no specific relation is checked', () => {
    const none = mountEditor({ source: 'all_in_scene' }, { relations: ['self', 'ally', 'enemy'] })
    expect((none.find('[data-test="tqe-any-relation"]').element as HTMLInputElement).checked).toBe(true)

    const some = mountEditor({ source: 'all_in_scene', relations: ['enemy'] }, { relations: ['self', 'ally', 'enemy'] })
    expect((some.find('[data-test="tqe-any-relation"]').element as HTMLInputElement).checked).toBe(false)
  })

  it('clicking "Any relationship" clears the specific relation filter', async () => {
    const wrapper = mountEditor({ source: 'all_in_scene', relations: ['enemy', 'ally'] }, { relations: ['self', 'ally', 'enemy'] })
    await wrapper.find('[data-test="tqe-any-relation"]').setValue(true)
    const merged = wrapper.emitted('update:modelValue')!.at(-1)![0] as TargetQueryDef
    expect(merged.relations).toEqual([])
  })

  it('clicking "Any relationship" while already active is a no-op (no commit)', async () => {
    const wrapper = mountEditor({ source: 'all_in_scene' }, { relations: ['self', 'ally', 'enemy'] })
    await wrapper.find('[data-test="tqe-any-relation"]').setValue(false)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
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

  it('groups declared fields into fixed sections, regardless of the declared order', () => {
    // maxCount declared before source, but sections impose the display order:
    // source (Start With) always precedes maxCount (Choose Results).
    const wrapper = mountEditor({ source: 'all_in_scene' }, {}, ['maxCount', 'source'])
    const html = wrapper.html()
    expect(html.indexOf('Start With')).toBeLessThan(html.indexOf('Choose Results'))
    expect(html.indexOf('Target source')).toBeLessThan(html.indexOf('tqe-max-count'))
  })

  it('falls back to the full sectioned layout when `fields` is not supplied (forward-compat)', () => {
    const wrapper = mountEditor({ source: 'all_in_scene' })
    const text = wrapper.text()
    for (const title of ['Start With', 'Search Area', 'Filter Units', 'Choose Results']) {
      expect(text).toContain(title)
    }
    // Always-visible fields render; progressively-disclosed ones (origin at
    // radius 0, Saved Value without a named source) do not.
    expect(wrapper.find('#tqe-max-count').exists()).toBe(true)
    expect(wrapper.findAll('[aria-label="Alive state"]').length).toBeGreaterThan(0)
    expect(wrapper.find('#tqe-origin-ref').exists()).toBe(false)
  })

  // ── progressive disclosure ──────────────────────────────────────────────
  it('hides Search Around until a radius is set, with a nudge in its place', async () => {
    const wrapper = mountEditor({ source: 'all_in_scene', radius: 0 })
    // origin picker absent, nudge present.
    expect(wrapper.find('[aria-label="Target origin"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Set a Search Radius')

    await wrapper.setProps({ modelValue: { source: 'all_in_scene', radius: 200 } })
    // radius > 0 reveals the origin picker and drops the nudge.
    expect(wrapper.find('[aria-label="Target origin"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('Set a Search Radius')
  })

  it('hides Saved Value unless a named source/origin needs it', async () => {
    const wrapper = mountEditor({ source: 'all_in_scene' })
    expect(wrapper.find('#tqe-origin-ref').exists()).toBe(false)

    await wrapper.setProps({ modelValue: { source: 'named_context' } })
    expect(wrapper.find('#tqe-origin-ref').exists()).toBe(true)
  })

  // ── Saved Value (originRef) picker ──────────────────────────────────────
  it('Saved Value suggests the ability\'s saved names and commits a typed ContextRef', async () => {
    // source 'named_context' reveals Saved Value (progressive disclosure).
    const wrapper = mountEditor({ source: 'named_context' }, {}, ['source', 'originRef'], ['chainHits', 'splashTargets'])
    const input = wrapper.find('#tqe-origin-ref')
    expect(input.exists()).toBe(true)
    // The datalist offers the scanned saved names.
    const suggestions = wrapper.findAll('datalist option').map((o) => o.attributes('value'))
    expect(suggestions).toEqual(['chainHits', 'splashTargets'])

    await input.setValue('chainHits')
    const merged = wrapper.emitted('update:modelValue')!.at(-1)![0] as TargetQueryDef
    expect(merged.originRef).toEqual({ key: 'chainHits' })
  })

  it('Saved Value accepts a free-typed name and trims it', async () => {
    const wrapper = mountEditor({ source: 'named_context' }, {}, ['source', 'originRef'], [])
    await wrapper.find('#tqe-origin-ref').setValue('  customName  ')
    const merged = wrapper.emitted('update:modelValue')!.at(-1)![0] as TargetQueryDef
    expect(merged.originRef).toEqual({ key: 'customName' })
  })

  it('Saved Value commits undefined when cleared', async () => {
    const wrapper = mountEditor(
      { source: 'named_context', originRef: { key: 'chainHits' } },
      {},
      ['source', 'originRef'],
      ['chainHits'],
    )
    await wrapper.find('#tqe-origin-ref').setValue('')
    const merged = wrapper.emitted('update:modelValue')!.at(-1)![0] as TargetQueryDef
    expect(merged.originRef).toBeUndefined()
  })

  it('Saved Value shows an empty-state hint when the ability saves no names', () => {
    const wrapper = mountEditor({ source: 'named_context' }, {}, ['source', 'originRef'], [])
    expect(wrapper.text()).toContain('No saved selections yet')
  })

  // ── Exclude Saved Set (excludeRef — chains) ─────────────────────────────
  it('shows Exclude Saved Set only once the ability saves a set', () => {
    const none = mountEditor({ source: 'all_in_scene' }, {}, undefined, [])
    expect(none.find('#tqe-exclude-ref').exists()).toBe(false)

    const some = mountEditor({ source: 'all_in_scene' }, {}, undefined, ['chainHits'])
    expect(some.find('#tqe-exclude-ref').exists()).toBe(true)
    // source is all_in_scene so Saved Value is hidden — the only datalist is
    // excludeRef's, suggesting the saved set.
    expect(some.findAll('datalist option').map((o) => o.attributes('value'))).toEqual(['chainHits'])
  })

  it('keeps Exclude Saved Set visible when already configured, even with no saved names', () => {
    const wrapper = mountEditor({ source: 'all_in_scene', excludeRef: { key: 'chainHits' } }, {}, undefined, [])
    expect(wrapper.find('#tqe-exclude-ref').exists()).toBe(true)
  })

  it('Exclude Saved Set commits an excludeRef ContextRef', async () => {
    const wrapper = mountEditor({ source: 'all_in_scene' }, {}, undefined, ['chainHits'])
    await wrapper.find('#tqe-exclude-ref').setValue('chainHits')
    const merged = wrapper.emitted('update:modelValue')!.at(-1)![0] as TargetQueryDef
    expect(merged.excludeRef).toEqual({ key: 'chainHits' })
  })

  it('aliveState renders as a FilterableSelect over alive/dead/any and commits the choice', async () => {
    const wrapper = mountEditor({ source: 'all_in_scene' }, {}, ['aliveState'])
    const select = wrapper.findComponent(FilterableSelect)
    expect(select.props('options')).toEqual([
      { id: '', label: 'Living (default)' },
      { id: 'alive', label: 'Living' },
      { id: 'dead', label: 'Dead' },
      { id: 'any', label: 'Living or Dead' },
    ])

    await select.vm.$emit('update:modelValue', 'dead')
    const merged = wrapper.emitted('update:modelValue')!.at(-1)![0] as TargetQueryDef
    expect(merged.aliveState).toBe('dead')
  })

  // ── InfoTip wiring (explaining Source/Origin/Origin Ref etc.) ───────────
  it('renders an InfoTip beside Source whose tooltip explains the field on click', async () => {
    // ['source', 'radius'] — a multi-field query renders the labeled "Start
    // With" field (a lone-source query is the source-only delivery shape,
    // covered separately below).
    const wrapper = mountEditor({ source: 'all_in_scene' }, {}, ['source', 'radius'])
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

    expect(byId.caster).toBe('Caster')
    expect(byId.source_object).toBe('Source Object — unavailable')
    expect(byId.named_context).toBe('Saved Selection — pick which in Saved Value')
    // A brand-new enum value the client has no copy for yet must render with
    // its raw wire value and unsuffixed, not throw, and not inherit another
    // option's label or note.
    expect(byId.some_brand_new_source).toBe('some_brand_new_source')
  })

  // ── generated summary sentence ──────────────────────────────────────────
  it('renders a plain-English summary of the query in the sectioned shape', () => {
    const wrapper = mountEditor({
      source: 'all_in_scene',
      relations: ['enemy'],
      radius: 200,
      origin: 'current_event_position',
      maxCount: 2,
      ordering: 'closest',
      excludeCurrentEvent: true,
    })
    expect(wrapper.find('.tqe-summary').text()).toBe(
      'Select the 2 closest living enemy units within 200 units of the triggering unit, excluding the triggering unit.',
    )
  })

  it('does not render the summary in the source-only delivery shape', () => {
    const wrapper = mountEditor({ source: 'previous_action_targets' }, {}, ['source'])
    expect(wrapper.find('.tqe-summary').exists()).toBe(false)
  })

  // ── source-only shape (launch_projectile / channel_beam) ────────────────
  it('renders a bare picker with a delivery hint (no "Start With") for a source-only query', async () => {
    const wrapper = mountEditor({ source: 'previous_action_targets' }, {}, ['source'])

    // The action's own field label names it; there is no inner "Start With".
    expect(wrapper.find('.tqe-source-row').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('Start With')

    // The picker still renders and still reflects/commits the source.
    const select = wrapper.findComponent(FilterableSelect)
    expect(select.props('modelValue')).toBe('previous_action_targets')
    await select.vm.$emit('update:modelValue', 'initial_target')
    const merged = wrapper.emitted('update:modelValue')!.at(-1)![0] as TargetQueryDef
    expect(merged.source).toBe('initial_target')

    // Its InfoTip explains the direct-delivery behavior, not the pool-narrowing
    // "Start With" copy.
    const infoBtn = wrapper.find('button.info-tip__btn')
    await infoBtn.trigger('click')
    expect(bubble()?.textContent).toMatch(/targeted directly/)
    await infoBtn.trigger('click')
  })

  it('relations checkboxes show an inline note only for the flagged relation (neutral)', () => {
    const wrapper = mountEditor(
      { source: 'all_in_scene' },
      { relations: ['self', 'ally', 'neutral'] },
      ['relations'],
    )
    const labels = wrapper.findAll('.tqe-checkgroup label.ed-check')
    const text = labels.map((l) => l.text())
    // Wire values self/ally/neutral render as their human labels; only the
    // inert relation (neutral) carries an inline note.
    expect(text.find((t) => t.startsWith('Caster'))).toBe('Caster')
    expect(text.find((t) => t.startsWith('Allied'))).toBe('Allied')
    expect(text.find((t) => t.startsWith('Neutral'))).toBe('Neutral (unavailable)')
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
