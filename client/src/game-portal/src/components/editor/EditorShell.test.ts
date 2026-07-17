import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import EditorShell from './EditorShell.vue'

describe('EditorShell', () => {
  // Guards the six consumers (AbilityEditorPanel, UnitTypeEditorPanel,
  // ItemEditorPanel, TableEditorPanel, ListEditorPanel,
  // AbilityBuilderPanel): with no #inspector slot and wideRail defaulted, the
  // shell must render exactly as it always has.
  it('renders the base grid when #inspector/wideRail are unused', () => {
    const wrapper = mount(EditorShell, {
      slots: {
        sidebar: '<div class="stub-sidebar" />',
        main: '<div class="stub-main" />',
      },
    })

    expect(wrapper.find('.ed-shell__inspector').exists()).toBe(false)
    expect(wrapper.find('.ed-shell__grid--inspector').exists()).toBe(false)
    expect(wrapper.find('.ed-shell__grid--wide-rail').exists()).toBe(false)
    expect(wrapper.find('.ed-shell__grid--no-rail').exists()).toBe(true)
    expect(wrapper.find('.ed-shell__rail').exists()).toBe(false)
    expect(wrapper.find('.stub-sidebar').exists()).toBe(true)
    expect(wrapper.find('.stub-main').exists()).toBe(true)
  })

  it('keeps the three-column grid (no --no-rail) when a rail slot is provided, as before', () => {
    const wrapper = mount(EditorShell, {
      slots: {
        sidebar: '<div />',
        main: '<div />',
        rail: '<div class="stub-rail" />',
      },
    })

    expect(wrapper.find('.ed-shell__grid--no-rail').exists()).toBe(false)
    expect(wrapper.find('.ed-shell__rail').exists()).toBe(true)
    expect(wrapper.find('.stub-rail').exists()).toBe(true)
    expect(wrapper.find('.ed-shell__inspector').exists()).toBe(false)
  })

  it('renders the inspector as its own column between main and rail when provided', () => {
    const wrapper = mount(EditorShell, {
      slots: {
        sidebar: '<div />',
        main: '<div class="stub-main" />',
        inspector: '<div class="stub-inspector">fields</div>',
        rail: '<div class="stub-rail" />',
      },
    })

    const inspector = wrapper.find('.ed-shell__inspector')
    expect(inspector.exists()).toBe(true)
    expect(inspector.text()).toContain('fields')
    expect(wrapper.find('.ed-shell__grid--inspector').exists()).toBe(true)

    // Order matters — the inspector sits between the flow and the preview.
    const cols = wrapper.findAll('.ed-shell__grid > *').map((c) => c.classes().join(' '))
    expect(cols.findIndex((c) => c.includes('ed-shell__main')))
      .toBeLessThan(cols.findIndex((c) => c.includes('ed-shell__inspector')))
    expect(cols.findIndex((c) => c.includes('ed-shell__inspector')))
      .toBeLessThan(cols.findIndex((c) => c.includes('ed-shell__rail')))
  })

  it('applies the wide-rail grid class only when wideRail is true AND a rail slot exists', () => {
    const withoutRail = mount(EditorShell, {
      props: { wideRail: true },
      slots: { sidebar: '<div />', main: '<div />' },
    })
    // No rail slot => no-rail wins regardless of wideRail; the wide-rail
    // column template must never apply when there's no rail to widen.
    expect(withoutRail.find('.ed-shell__grid--wide-rail').exists()).toBe(false)
    expect(withoutRail.find('.ed-shell__grid--no-rail').exists()).toBe(true)

    const withRail = mount(EditorShell, {
      props: { wideRail: true },
      slots: { sidebar: '<div />', main: '<div />', rail: '<div />' },
    })
    expect(withRail.find('.ed-shell__grid--wide-rail').exists()).toBe(true)
    expect(withRail.find('.ed-shell__grid--no-rail').exists()).toBe(false)
  })

  it('defaults wideRail to false', () => {
    const wrapper = mount(EditorShell, {
      slots: { sidebar: '<div />', main: '<div />', rail: '<div />' },
    })
    expect(wrapper.find('.ed-shell__grid--wide-rail').exists()).toBe(false)
  })
})
