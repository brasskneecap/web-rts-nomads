import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import PerkEditorPanel from './PerkEditorPanel.vue'

function stubFetch() {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (String(url).endsWith('/catalog/perks')) {
      return {
        ok: true,
        status: 200,
        json: async () => ({ perks: [{ id: 'bloodlust', displayName: 'Bloodlust', rank: 'bronze', wired: true }] }),
      }
    }
    if (String(url).endsWith('/catalog/units')) {
      return { ok: true, status: 200, json: async () => ({ units: [{ type: 'soldier' }], paths: [], pathsByUnit: {} }) }
    }
    return { ok: true, status: 200, json: async () => ({}) }
  }) as unknown as typeof fetch)
}

afterEach(() => vi.restoreAllMocks())

describe('PerkEditorPanel', () => {
  it('mounts and lists perks from the catalog', async () => {
    stubFetch()
    const wrapper = mount(PerkEditorPanel)
    await flushPromises()
    expect(wrapper.text()).toContain('Bloodlust')
  })
})
