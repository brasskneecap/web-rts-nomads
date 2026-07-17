import { afterEach, describe, expect, it } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import InfoTip from './InfoTip.vue'

// The bubble is Teleported to <body>, so assertions on its presence/content
// look at document.body directly rather than wrapper.find().
function bubble(): HTMLElement | null {
  return document.body.querySelector('.info-tip__bubble')
}

// `pinnedId` (useInfoTip.ts) is module-scoped and shared by every InfoTip
// instance on the page, so a wrapper left mounted (and reactive) from one
// test can react to a later test pinning a *different* instance open — track
// and unmount every wrapper after each test to keep tests isolated.
let wrappers: VueWrapper[] = []
function mountInfoTip(props: { text: string; ariaLabel?: string }) {
  const wrapper = mount(InfoTip, { props })
  wrappers.push(wrapper)
  return wrapper
}

describe('InfoTip', () => {
  afterEach(() => {
    wrappers.forEach((w) => w.unmount())
    wrappers = []
    document.body.innerHTML = ''
  })

  it('renders the (i) button when text is provided', () => {
    const wrapper = mountInfoTip({ text: 'Some helpful copy.' })
    expect(wrapper.find('button.info-tip__btn').exists()).toBe(true)
  })

  it('is a real <button> with an accessible label', () => {
    const wrapper = mountInfoTip({ text: 'Some helpful copy.', ariaLabel: 'About Source' })
    const btn = wrapper.get('button.info-tip__btn')
    expect(btn.element.tagName).toBe('BUTTON')
    expect(btn.attributes('aria-label')).toBe('About Source')
  })

  it('graceful fallback: renders no icon at all when text is empty', () => {
    const wrapper = mountInfoTip({ text: '' })
    expect(wrapper.find('button.info-tip__btn').exists()).toBe(false)
    expect(bubble()).toBeNull()
  })

  it('graceful fallback: whitespace-only text also renders no icon', () => {
    const wrapper = mountInfoTip({ text: '   ' })
    expect(wrapper.find('button.info-tip__btn').exists()).toBe(false)
  })

  it('click toggles the tooltip open and closed, and aria-expanded flips with it', async () => {
    const wrapper = mountInfoTip({ text: 'Which units to start from.' })
    const btn = wrapper.get('button.info-tip__btn')

    expect(btn.attributes('aria-expanded')).toBe('false')
    expect(bubble()).toBeNull()

    await btn.trigger('click')
    expect(btn.attributes('aria-expanded')).toBe('true')
    expect(bubble()).not.toBeNull()
    expect(bubble()!.textContent).toContain('Which units to start from.')

    await btn.trigger('click')
    expect(btn.attributes('aria-expanded')).toBe('false')
    expect(bubble()).toBeNull()
  })

  it('the bubble contains the exact text passed in', async () => {
    const wrapper = mountInfoTip({ text: 'Drop the caster from the results.' })
    await wrapper.get('button.info-tip__btn').trigger('click')
    expect(bubble()!.textContent).toBe('Drop the caster from the results.')
    await wrapper.get('button.info-tip__btn').trigger('click') // close before teardown
  })

  it('Escape closes an open tooltip', async () => {
    const wrapper = mountInfoTip({ text: 'Some helpful copy.' })
    const btn = wrapper.get('button.info-tip__btn')

    await btn.trigger('click')
    expect(bubble()).not.toBeNull()

    await btn.trigger('keydown', { key: 'Escape' })
    expect(bubble()).toBeNull()
    expect(btn.attributes('aria-expanded')).toBe('false')
  })

  it('clicking outside the component closes an open tooltip', async () => {
    const wrapper = mount(InfoTip, { props: { text: 'Some helpful copy.' }, attachTo: document.body })
    wrappers.push(wrapper)
    await wrapper.get('button.info-tip__btn').trigger('click')
    expect(bubble()).not.toBeNull()

    document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    await wrapper.vm.$nextTick()

    expect(bubble()).toBeNull()
  })

  it('only one tip is pinned open at a time: opening a second closes the first', async () => {
    const a = mountInfoTip({ text: 'Tip A' })
    const b = mountInfoTip({ text: 'Tip B' })

    await a.get('button.info-tip__btn').trigger('click')
    expect(a.get('button.info-tip__btn').attributes('aria-expanded')).toBe('true')

    await b.get('button.info-tip__btn').trigger('click')
    expect(b.get('button.info-tip__btn').attributes('aria-expanded')).toBe('true')
    expect(a.get('button.info-tip__btn').attributes('aria-expanded')).toBe('false')
  })
})
