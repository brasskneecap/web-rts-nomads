import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ProjectileEditorPanel from './ProjectileEditorPanel.vue'

function stubFetch() {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (String(url).endsWith('/catalog/projectiles')) {
      return { ok: true, status: 200, json: async () => ({ projectiles: [{ id: 'fire_bolt', kind: 'projectile', speed: 300 }] }) }
    }
    if (String(url).endsWith('/catalog/effects')) {
      return { ok: true, status: 200, json: async () => ({ effects: [{ id: 'fizzle' }] }) }
    }
    return { ok: true, status: 200, json: async () => ({}) }
  }) as unknown as typeof fetch)
}

afterEach(() => vi.restoreAllMocks())

describe('ProjectileEditorPanel', () => {
  it('mounts and lists projectiles from the catalog', async () => {
    stubFetch()
    const wrapper = mount(ProjectileEditorPanel)
    await flushPromises()
    expect(wrapper.text()).toContain('fire_bolt')
  })
})
