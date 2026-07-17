import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { AuthoredAbilityDef } from '@/game/abilities/abilityEditorForm'
import AbilityBuilderPanel from './AbilityBuilderPanel.vue'
import AbilityPreviewPanel from './AbilityPreviewPanel.vue'

// A legacy (pre-schemaVersion-2) ability — mirrors useAbilityBuilder.test.ts's
// fixture: no authored `program`, only a server-computed `compiledProgram`.
const legacyAbility: AuthoredAbilityDef = {
  id: 'heal',
  displayName: 'Heal',
  schemaVersion: 1,
  runnable: true,
  compiledProgram: {
    entry: { type: 'unit', range: 300 },
    triggers: [
      { id: 't1', type: 'on_cast_complete', actions: [{ id: 'a1', type: 'restore_health' }] },
    ],
  },
}

// A composable (schemaVersion 2) ability — Save should NOT be disabled for
// this one (validation clean, id present).
const composableAbility: AuthoredAbilityDef = {
  id: 'fireball',
  displayName: 'Fireball',
  schemaVersion: 2,
  runnable: true,
  program: {
    entry: { type: 'unit', range: 400 },
    triggers: [
      { id: 't1', type: 'on_cast_complete', actions: [{ id: 'a1', type: 'deal_damage', config: { amount: 10 } }] },
    ],
  },
}

function jsonResponse(body: unknown, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

// Stubs every /catalog + /abilities endpoint useAbilityBuilder touches on
// mount + selection, mirroring useAbilityBuilder.test.ts's makeFetchMock.
function stubFetch(abilities: AuthoredAbilityDef[]) {
  const fn = vi.fn(async (url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'
    if (method === 'GET' && u.endsWith('/catalog/abilities')) return jsonResponse({ abilities })
    if (method === 'GET' && u.endsWith('/catalog/action-schema')) return jsonResponse({ actions: [], enums: {} })
    if (method === 'GET' && u.endsWith('/catalog/effects')) return jsonResponse({ effects: [] })
    if (method === 'GET' && u.endsWith('/catalog/projectiles')) return jsonResponse({ projectiles: [] })
    if (method === 'GET' && u.endsWith('/catalog/damage-types')) return jsonResponse({ damageTypes: [] })
    if (method === 'GET' && u.endsWith('/catalog/ability-categories')) return jsonResponse({ abilityCategories: [] })
    if (method === 'GET' && u.endsWith('/catalog/autocast-selectors')) return jsonResponse({ autoCastSelectors: [] })
    if (method === 'GET' && u.endsWith('/catalog/units')) return jsonResponse({ units: [] })
    if (method === 'POST' && u.endsWith('/abilities/validate')) return jsonResponse({ issues: [] })
    return jsonResponse({})
  })
  vi.stubGlobal('fetch', fn)
}

afterEach(() => vi.restoreAllMocks())

describe('AbilityBuilderPanel', () => {
  it('shows the empty state before any ability is selected', async () => {
    stubFetch([legacyAbility, composableAbility])
    const wrapper = mount(AbilityBuilderPanel)
    await flushPromises()

    expect(wrapper.text()).toContain('Select an ability, or create a new one.')
    expect(wrapper.find('[data-test="ability-flow"]').exists()).toBe(false)
  })

  it('selecting a sidebar entry renders the header, lands on Identity by default, and shows the bottom inspector bar', async () => {
    stubFetch([legacyAbility, composableAbility])
    const wrapper = mount(AbilityBuilderPanel)
    await flushPromises()

    await wrapper.find('.ed-side__pick').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Heal')
    // Identity is the default tab on selection — Build Ability's flow isn't
    // rendered until the author switches to it.
    expect(wrapper.find('[data-test="identity-tab"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="ability-flow"]').exists()).toBe(false)
    // The bottom InspectorBar spans both tabs, not just Build.
    expect(wrapper.find('[data-test="inspector-bar"]').exists()).toBe(true)
  })

  it('disables Save and shows Convert for a selected legacy ability', async () => {
    stubFetch([legacyAbility])
    const wrapper = mount(AbilityBuilderPanel)
    await flushPromises()

    await wrapper.find('.ed-side__pick').trigger('click')
    await flushPromises()

    const saveButton = wrapper.findAll('button').find((b) => b.text().includes('Save'))
    expect(saveButton?.attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-test="convert-button"]').exists()).toBe(true)
  })

  it('does not show Convert for a selected composable ability', async () => {
    stubFetch([composableAbility])
    const wrapper = mount(AbilityBuilderPanel)
    await flushPromises()

    await wrapper.find('.ed-side__pick').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="convert-button"]').exists()).toBe(false)
  })

  it('locks the ID field once a newly-saved ability appears in the reloaded catalog', async () => {
    // isNewAbility is derived from builder.abilities — this test proves that
    // derivation, not a locally-tracked flag: the id starts editable (not in
    // the catalog), and locks the instant the POST-then-reload round trip
    // brings the saved id back from the "server".
    const abilities: AuthoredAbilityDef[] = [composableAbility]
    const fn = vi.fn(async (url: string, init?: RequestInit) => {
      const u = String(url)
      const method = init?.method ?? 'GET'
      if (method === 'GET' && u.endsWith('/catalog/abilities')) return jsonResponse({ abilities })
      if (method === 'GET' && u.endsWith('/catalog/action-schema')) return jsonResponse({ actions: [], enums: {} })
      if (method === 'GET' && u.endsWith('/catalog/effects')) return jsonResponse({ effects: [] })
      if (method === 'GET' && u.endsWith('/catalog/projectiles')) return jsonResponse({ projectiles: [] })
      if (method === 'GET' && u.endsWith('/catalog/damage-types')) return jsonResponse({ damageTypes: [] })
      if (method === 'GET' && u.endsWith('/catalog/ability-categories')) return jsonResponse({ abilityCategories: [] })
      if (method === 'GET' && u.endsWith('/catalog/autocast-selectors')) return jsonResponse({ autoCastSelectors: [] })
      if (method === 'GET' && u.endsWith('/catalog/units')) return jsonResponse({ units: [] })
      if (method === 'POST' && u.endsWith('/abilities/validate')) return jsonResponse({ issues: [] })
      if (method === 'POST' && u.endsWith('/abilities')) {
        // Mimic the real backend: a save persists the ability, so the NEXT
        // GET /catalog/abilities (reloadAbilities, inside builder.save())
        // must include it.
        const { ability } = JSON.parse(String(init?.body)) as { ability: AuthoredAbilityDef }
        abilities.push(ability)
        return jsonResponse({})
      }
      return jsonResponse({})
    })
    vi.stubGlobal('fetch', fn)

    const wrapper = mount(AbilityBuilderPanel)
    await flushPromises()

    const newButton = wrapper.findAll('button').find((b) => b.text() === 'Add New Ability')
    await newButton!.trigger('click')
    await flushPromises()

    const idInput = wrapper.find('#ab-id')
    expect(idInput.attributes('disabled')).toBeUndefined()
    // Delete/Reset is a "this def already exists" action — hidden for a draft.
    expect(wrapper.findAll('button').some((b) => b.text() === 'Delete / Reset')).toBe(false)

    await idInput.setValue('new_bolt')
    await flushPromises()

    const saveButton = wrapper.findAll('button').find((b) => b.text() === 'Save')
    expect(saveButton?.attributes('disabled')).toBeUndefined()
    await saveButton!.trigger('click')
    await flushPromises()

    const idInputAfterSave = wrapper.find('#ab-id')
    expect(idInputAfterSave.attributes('disabled')).toBeDefined()
    expect(wrapper.findAll('button').some((b) => b.text() === 'Delete / Reset')).toBe(true)
  })

  it('shows a validation summary with error/warning counts, and a blocked hint when there are errors', async () => {
    const fn = vi.fn(async (url: string, init?: RequestInit) => {
      const u = String(url)
      const method = init?.method ?? 'GET'
      if (method === 'GET' && u.endsWith('/catalog/abilities')) return jsonResponse({ abilities: [composableAbility] })
      if (method === 'GET' && u.endsWith('/catalog/action-schema')) return jsonResponse({ actions: [], enums: {} })
      if (method === 'GET' && u.endsWith('/catalog/effects')) return jsonResponse({ effects: [] })
      if (method === 'GET' && u.endsWith('/catalog/projectiles')) return jsonResponse({ projectiles: [] })
      if (method === 'GET' && u.endsWith('/catalog/damage-types')) return jsonResponse({ damageTypes: [] })
      if (method === 'GET' && u.endsWith('/catalog/ability-categories')) return jsonResponse({ abilityCategories: [] })
      if (method === 'GET' && u.endsWith('/catalog/autocast-selectors')) return jsonResponse({ autoCastSelectors: [] })
      if (method === 'GET' && u.endsWith('/catalog/units')) return jsonResponse({ units: [] })
      if (method === 'POST' && u.endsWith('/abilities/validate')) {
        return jsonResponse({
          issues: [
            { path: 'triggers[0].actions[0]', code: 'x', message: 'amount required', severity: 'error' },
            { path: 'identity.category', code: 'y', message: 'category missing', severity: 'warning' },
          ],
        })
      }
      return jsonResponse({})
    })
    vi.stubGlobal('fetch', fn)

    const wrapper = mount(AbilityBuilderPanel)
    await flushPromises()

    await wrapper.find('.ed-side__pick').trigger('click')
    await flushPromises()
    // Validation is debounced (300ms) inside useAbilityBuilder — selectAbility
    // itself calls revalidate() directly (no debounce), so flushPromises alone
    // is enough here without needing fake timers.

    const summary = wrapper.find('[data-test="validation-summary"]')
    expect(summary.exists()).toBe(true)
    expect(summary.text()).toContain('1 error')
    expect(summary.text()).toContain('1 warning')
    expect(summary.text()).toContain('blocked')
  })

  it('clicking Convert to Composable opens the convert dialog instead of converting immediately', async () => {
    stubFetch([legacyAbility])
    const wrapper = mount(AbilityBuilderPanel)
    await flushPromises()

    await wrapper.find('.ed-side__pick').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="convert-dialog-overlay"]').exists()).toBe(false)

    await wrapper.find('[data-test="convert-button"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="convert-dialog-overlay"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="convert-confirm"]').exists()).toBe(true)
  })

  it('the Identity/Build Ability tabs switch the main view, and only appear while editing', async () => {
    stubFetch([composableAbility])
    const wrapper = mount(AbilityBuilderPanel)
    await flushPromises()

    // Not editing yet — no tabs.
    expect(wrapper.find('[data-test="ability-builder-tab-identity"]').exists()).toBe(false)

    await wrapper.find('.ed-side__pick').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="ability-builder-tab-identity"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="identity-tab"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="ability-flow"]').exists()).toBe(false)
    // The InspectorBar (bottom) and the preview panel (rail) are independent
    // of the tab — both are visible regardless of which tab is active.
    expect(wrapper.find('[data-test="inspector-bar"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="ability-preview-panel"]').exists()).toBe(true)

    await wrapper.find('[data-test="ability-builder-tab-build"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="identity-tab"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="ability-flow"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="inspector-bar"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="ability-preview-panel"]').exists()).toBe(true)

    await wrapper.find('[data-test="ability-builder-tab-identity"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="identity-tab"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="ability-flow"]').exists()).toBe(false)
  })

  it('keeps the preview rail mounted (the SAME component instance) across a tab switch', async () => {
    // The load-bearing assertion for Task 4: playback/scrub state inside
    // AbilityPreviewPanel must survive switching tabs, which only holds if
    // the component is never unmounted+remounted — re-rendering with fresh
    // state would look identical in a shallower "does it exist" check.
    //
    // Comparing `.vm` directly isn't reliable here: Vue Test Utils re-wraps
    // the public instance in a fresh proxy on each `findComponent` call, so
    // `toBe` can fail even when the underlying instance (same `.vm.$.uid`)
    // hasn't changed. The component's root DOM element, by contrast, is only
    // ever a new node if Vue actually unmounted and remounted the
    // component — exactly the thing this test needs to rule out — so it's
    // the more reliable proxy-free signal of "never remounted".
    stubFetch([composableAbility])
    const wrapper = mount(AbilityBuilderPanel)
    await flushPromises()

    await wrapper.find('.ed-side__pick').trigger('click')
    await flushPromises()

    const initial = wrapper.findComponent(AbilityPreviewPanel)
    expect(initial.exists()).toBe(true)
    // `.$` is the internal component instance (untyped in vue-test-utils'
    // public `vm` type) — its `uid` is stable for the life of one mounted
    // instance and reassigned only on a genuine unmount+remount, which is
    // exactly what this test is proving does NOT happen.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- see comment above
    const initialUid = (initial.vm as any).$.uid
    const initialElement = initial.element

    await wrapper.find('[data-test="ability-builder-tab-build"]').trigger('click')
    await flushPromises()

    const afterBuild = wrapper.findComponent(AbilityPreviewPanel)
    expect(afterBuild.exists()).toBe(true)
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- see comment above
    expect((afterBuild.vm as any).$.uid).toBe(initialUid)
    expect(afterBuild.element).toBe(initialElement)

    await wrapper.find('[data-test="ability-builder-tab-identity"]').trigger('click')
    await flushPromises()

    const afterIdentity = wrapper.findComponent(AbilityPreviewPanel)
    expect(afterIdentity.exists()).toBe(true)
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- see comment above
    expect((afterIdentity.vm as any).$.uid).toBe(initialUid)
    expect(afterIdentity.element).toBe(initialElement)
  })

  it('switching from Build (with a trigger/action selected) to Identity resets the selection to the ability node', async () => {
    stubFetch([composableAbility])
    const wrapper = mount(AbilityBuilderPanel)
    await flushPromises()

    await wrapper.find('.ed-side__pick').trigger('click')
    await flushPromises()

    await wrapper.find('[data-test="ability-builder-tab-build"]').trigger('click')
    await flushPromises()

    await wrapper.find('.flow-action__body').trigger('click')
    await flushPromises()

    // An action is selected — the InspectorBar has fields, not the hint.
    expect(wrapper.find('[data-test="inspector-bar-empty"]').exists()).toBe(false)

    await wrapper.find('[data-test="ability-builder-tab-identity"]').trigger('click')
    await flushPromises()

    // Selection follows the tab back to the ability node, so the bar and
    // the Identity tab agree instead of the bar silently keeping stale
    // action fields nobody can see anymore.
    expect(wrapper.find('[data-test="inspector-bar-empty"]').exists()).toBe(true)
  })

  it("clicking the overview card's identity button navigates from Build back to Identity", async () => {
    stubFetch([composableAbility])
    const wrapper = mount(AbilityBuilderPanel)
    await flushPromises()

    await wrapper.find('.ed-side__pick').trigger('click')
    await flushPromises()

    await wrapper.find('[data-test="ability-builder-tab-build"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="ability-flow"]').exists()).toBe(true)

    await wrapper.find('[data-test="overview-open-settings"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="identity-tab"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="ability-flow"]').exists()).toBe(false)
  })
})
