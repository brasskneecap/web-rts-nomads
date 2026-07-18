import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import EffectEditorPanel from './EffectEditorPanel.vue'

function stubFetch() {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (String(url).endsWith('/catalog/effects')) {
      return { ok: true, status: 200, json: async () => ({ effects: [{ id: 'healing_glow', duration: 0.6, anchor: 'center' }] }) }
    }
    return { ok: true, status: 200, json: async () => ({}) }
  }) as unknown as typeof fetch)
}

afterEach(() => vi.restoreAllMocks())

describe('EffectEditorPanel', () => {
  it('mounts and lists effects from the catalog', async () => {
    stubFetch()
    const wrapper = mount(EffectEditorPanel)
    await flushPromises()
    expect(wrapper.text()).toContain('healing_glow')
  })
})
