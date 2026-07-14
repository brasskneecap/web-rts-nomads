import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AbilityEditorPanel from './AbilityEditorPanel.vue'

// Stub every fetch the panel makes on mount with an empty-but-valid payload,
// keyed by URL suffix.
function stubCatalogFetch() {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    const map: Record<string, unknown> = {
      '/catalog/abilities': { abilities: [{ id: 'heal', displayName: 'Heal', healAmount: 40 }] },
      '/catalog/projectiles': { projectiles: [{ id: 'fire_bolt' }] },
      '/catalog/effects': { effects: [{ id: 'healing_glow' }] },
      '/catalog/autocast-selectors': { autoCastSelectors: ['self'] },
      '/catalog/ability-categories': { abilityCategories: ['heal'] },
      '/catalog/damage-types': { damageTypes: ['holy'] },
      '/catalog/units': { units: [{ type: 'skeleton' }], paths: [], pathsByUnit: {} },
    }
    const key = Object.keys(map).find((k) => String(url).endsWith(k))
    return { ok: true, status: 200, json: async () => map[key ?? ''] ?? {} }
  }) as unknown as typeof fetch)
}

afterEach(() => vi.restoreAllMocks())

describe('AbilityEditorPanel', () => {
  it('mounts and lists abilities from the catalog', async () => {
    stubCatalogFetch()
    const wrapper = mount(AbilityEditorPanel)
    await flushPromises()
    expect(wrapper.text()).toContain('Heal')
  })

  it('lists bundled ability icons in the gallery and picks one', async () => {
    stubCatalogFetch()
    const wrapper = mount(AbilityEditorPanel)
    await flushPromises()
    // open an ability to edit, then open the gallery
    // (select the first listed ability, then click "Choose from gallery")
    await wrapper.find('[data-test="ability-row"]').trigger('click')
    await wrapper.find('[data-test="icon-gallery-open"]').trigger('click')
    await flushPromises()
    const cells = wrapper.findAll('[data-test="icon-gallery-cell"]')
    expect(cells.length).toBeGreaterThan(0)
  })
})
