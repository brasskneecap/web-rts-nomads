import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { AuthoredAbilityDef } from '@/game/abilities/abilityEditorForm'
import AbilityBuilderPanel from './AbilityBuilderPanel.vue'
import { settle, useConfirmDialogState } from '@/components/ui/useConfirmDialog'

// The delete/reset prompts go through the app's themed dialog rather than
// window.confirm (see useConfirmDialog's doc comment), so these tests drive that
// singleton: assert what it was ASKED, then settle it.
const confirmState = useConfirmDialogState()
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
// `deleteStatus` controls what DELETE /abilities/{id} reports; `deleteCalls`
// (returned) records every id it was invoked with, so a test can assert the
// confirm-cancel path never reaches the network.
function stubFetch(abilities: AuthoredAbilityDef[], deleteStatus: 'deleted' | 'reverted' | 'reset' = 'deleted') {
  const deleteCalls: string[] = []
  const fn = vi.fn(async (url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'
    if (method === 'GET' && u.endsWith('/catalog/abilities')) return jsonResponse({ abilities })
    if (method === 'GET' && u.endsWith('/catalog/action-schema')) return jsonResponse({ actions: [], enums: {} })
    if (method === 'GET' && u.endsWith('/catalog/action-icons')) return jsonResponse({ icons: [] })
    if (method === 'GET' && u.endsWith('/catalog/effects')) return jsonResponse({ effects: [] })
    if (method === 'GET' && u.endsWith('/catalog/projectiles')) return jsonResponse({ projectiles: [] })
    if (method === 'GET' && u.endsWith('/catalog/damage-types')) return jsonResponse({ damageTypes: [] })
    if (method === 'GET' && u.endsWith('/catalog/ability-categories')) return jsonResponse({ abilityCategories: [] })
    if (method === 'GET' && u.endsWith('/catalog/autocast-selectors')) return jsonResponse({ autoCastSelectors: [] })
    if (method === 'GET' && u.endsWith('/catalog/units')) return jsonResponse({ units: [] })
    if (method === 'GET' && u.endsWith('/catalog/perks')) return jsonResponse({ perks: [] })
    if (method === 'POST' && u.endsWith('/abilities/validate')) return jsonResponse({ issues: [] })
    if (method === 'DELETE' && u.includes('/abilities/')) {
      const id = decodeURIComponent(u.split('/abilities/')[1])
      deleteCalls.push(id)
      return jsonResponse({ id, status: deleteStatus })
    }
    return jsonResponse({})
  })
  vi.stubGlobal('fetch', fn)
  return { deleteCalls }
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

  it('selecting a sidebar entry renders the header, lands on Identity by default, with no inspector column yet', async () => {
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
    // The inspector column is hidden until a trigger/action is selected — the
    // ability node lands on Identity, which has nothing for the bar to show.
    expect(wrapper.find('[data-test="inspector-bar"]').exists()).toBe(false)
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
      if (method === 'GET' && u.endsWith('/catalog/action-icons')) return jsonResponse({ icons: [] })
      if (method === 'GET' && u.endsWith('/catalog/effects')) return jsonResponse({ effects: [] })
      if (method === 'GET' && u.endsWith('/catalog/projectiles')) return jsonResponse({ projectiles: [] })
      if (method === 'GET' && u.endsWith('/catalog/damage-types')) return jsonResponse({ damageTypes: [] })
      if (method === 'GET' && u.endsWith('/catalog/ability-categories')) return jsonResponse({ abilityCategories: [] })
      if (method === 'GET' && u.endsWith('/catalog/autocast-selectors')) return jsonResponse({ autoCastSelectors: [] })
      if (method === 'GET' && u.endsWith('/catalog/units')) return jsonResponse({ units: [] })
    if (method === 'GET' && u.endsWith('/catalog/perks')) return jsonResponse({ perks: [] })
      if (method === 'POST' && u.endsWith('/abilities/validate')) return jsonResponse({ issues: [] })
      if (method === 'POST' && u.endsWith('/abilities')) {
        // Mimic the real backend: a save persists the ability, so the NEXT
        // GET /catalog/abilities (reloadAbilities, inside builder.save())
        // must include it — tagged `custom: true` because an author-created
        // id that didn't previously exist in the catalog IS, by definition,
        // a custom entry (see the DELETE contract's custom/deleted meaning).
        const { ability } = JSON.parse(String(init?.body)) as { ability: AuthoredAbilityDef }
        abilities.push({ ...ability, custom: true })
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
    expect(wrapper.findAll('button').some((b) => b.text() === 'Delete' || b.text() === 'Reset')).toBe(false)

    await idInput.setValue('new_bolt')
    await flushPromises()

    const saveButton = wrapper.findAll('button').find((b) => b.text() === 'Save')
    expect(saveButton?.attributes('disabled')).toBeUndefined()
    await saveButton!.trigger('click')
    await flushPromises()

    const idInputAfterSave = wrapper.find('#ab-id')
    expect(idInputAfterSave.attributes('disabled')).toBeDefined()
    // Author-created (custom: true) → "Delete", never the old ambiguous
    // static "Delete / Reset" label.
    expect(wrapper.findAll('button').some((b) => b.text() === 'Delete')).toBe(true)
    expect(wrapper.findAll('button').some((b) => b.text() === 'Delete / Reset')).toBe(false)
  })

  it('shows a validation summary with error/warning counts, and a blocked hint when there are errors', async () => {
    const fn = vi.fn(async (url: string, init?: RequestInit) => {
      const u = String(url)
      const method = init?.method ?? 'GET'
      if (method === 'GET' && u.endsWith('/catalog/abilities')) return jsonResponse({ abilities: [composableAbility] })
      if (method === 'GET' && u.endsWith('/catalog/action-schema')) return jsonResponse({ actions: [], enums: {} })
      if (method === 'GET' && u.endsWith('/catalog/action-icons')) return jsonResponse({ icons: [] })
      if (method === 'GET' && u.endsWith('/catalog/effects')) return jsonResponse({ effects: [] })
      if (method === 'GET' && u.endsWith('/catalog/projectiles')) return jsonResponse({ projectiles: [] })
      if (method === 'GET' && u.endsWith('/catalog/damage-types')) return jsonResponse({ damageTypes: [] })
      if (method === 'GET' && u.endsWith('/catalog/ability-categories')) return jsonResponse({ abilityCategories: [] })
      if (method === 'GET' && u.endsWith('/catalog/autocast-selectors')) return jsonResponse({ autoCastSelectors: [] })
      if (method === 'GET' && u.endsWith('/catalog/units')) return jsonResponse({ units: [] })
    if (method === 'GET' && u.endsWith('/catalog/perks')) return jsonResponse({ perks: [] })
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
    // The preview panel (rail) is always present while editing; the inspector
    // column is NOT — it only appears once a trigger/action is selected, which
    // hasn't happened yet (selection is the ability node on Identity).
    expect(wrapper.find('[data-test="inspector-bar"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="ability-preview-panel"]').exists()).toBe(true)

    await wrapper.find('[data-test="ability-builder-tab-build"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="identity-tab"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="ability-flow"]').exists()).toBe(true)
    // Switching to Build alone doesn't select a node, so the inspector stays
    // hidden until the author clicks a trigger/action in the flow.
    expect(wrapper.find('[data-test="inspector-bar"]').exists()).toBe(false)
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

    // An action is selected — the inspector column appears with fields (not the
    // hint), since a trigger/action is now selected.
    expect(wrapper.find('[data-test="inspector-bar"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="inspector-bar-empty"]').exists()).toBe(false)

    await wrapper.find('[data-test="ability-builder-tab-identity"]').trigger('click')
    await flushPromises()

    // Selection follows the tab back to the ability node — with nothing
    // trigger/action-shaped selected, the inspector column is omitted entirely
    // instead of lingering with stale action fields nobody can see anymore.
    expect(wrapper.find('[data-test="inspector-bar"]').exists()).toBe(false)
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

describe('AbilityBuilderPanel delete/reset (3-way contract)', () => {
  it('shows "Reset" (never "Delete / Reset") for a shipped ability', async () => {
    // composableAbility carries no `custom` flag — undefined reads the same
    // as false, matching ItemEditorPanel's selectedIsCustom contract.
    stubFetch([composableAbility])
    const wrapper = mount(AbilityBuilderPanel)
    await flushPromises()

    await wrapper.find('.ed-side__pick').trigger('click')
    await flushPromises()

    expect(wrapper.findAll('button').some((b) => b.text() === 'Reset')).toBe(true)
    expect(wrapper.findAll('button').some((b) => b.text() === 'Delete')).toBe(false)
  })

  it('shows "Delete" for an author-created ability', async () => {
    const customAbility: AuthoredAbilityDef = { ...composableAbility, custom: true }
    stubFetch([customAbility])
    const wrapper = mount(AbilityBuilderPanel)
    await flushPromises()

    await wrapper.find('.ed-side__pick').trigger('click')
    await flushPromises()

    expect(wrapper.findAll('button').some((b) => b.text() === 'Delete')).toBe(true)
  })

  it('cancelling the confirm does NOT call the delete API (the anti-misclick guard)', async () => {
    const customAbility: AuthoredAbilityDef = { ...composableAbility, custom: true }
    const { deleteCalls } = stubFetch([customAbility])

    const wrapper = mount(AbilityBuilderPanel)
    await flushPromises()
    await wrapper.find('.ed-side__pick').trigger('click')
    await flushPromises()

    const removeButton = wrapper.findAll('button').find((b) => b.text() === 'Delete')
    await removeButton!.trigger('click')
    await flushPromises()

    // The themed dialog is open and nothing has been sent yet.
    expect(confirmState.open.value).toBe(true)
    expect(deleteCalls).toEqual([])

    settle(false)
    await flushPromises()
    expect(deleteCalls).toEqual([])
    // The ability is still open — a cancelled confirm must not close the editor.
    expect(wrapper.find('[data-test="identity-tab"]').exists()).toBe(true)
  })

  it('confirming Delete on a custom ability names the permanent-removal consequence, then calls the API and closes the editor', async () => {
    const customAbility: AuthoredAbilityDef = { ...composableAbility, custom: true }
    const { deleteCalls } = stubFetch([customAbility], 'deleted')

    const wrapper = mount(AbilityBuilderPanel)
    await flushPromises()
    await wrapper.find('.ed-side__pick').trigger('click')
    await flushPromises()

    const removeButton = wrapper.findAll('button').find((b) => b.text() === 'Delete')
    await removeButton!.trigger('click')
    await flushPromises()

    // The prompt must name the CONSEQUENCE, not just ask "are you sure?".
    expect(confirmState.request.value?.title).toMatch(/Delete ability/)
    expect(confirmState.request.value?.lines.join(' ')).toMatch(/cannot be undone/)

    settle(true)
    await flushPromises()
    expect(deleteCalls).toEqual(['fireball'])
    expect(wrapper.find('[data-test="identity-tab"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Select an ability, or create a new one.')
  })

  it('confirming Reset on a shipped ability names the discard-unsaved-changes consequence, then reloads it back into the editor', async () => {
    const { deleteCalls } = stubFetch([composableAbility], 'reverted')

    const wrapper = mount(AbilityBuilderPanel)
    await flushPromises()
    await wrapper.find('.ed-side__pick').trigger('click')
    await flushPromises()

    const removeButton = wrapper.findAll('button').find((b) => b.text() === 'Reset')
    await removeButton!.trigger('click')
    await flushPromises()

    expect(confirmState.request.value?.title).toMatch(/Reset "/)
    expect(confirmState.request.value?.lines.join(' ')).toMatch(/unsaved editor changes.*discarded/)

    settle(true)
    await flushPromises()
    expect(deleteCalls).toEqual(['fireball'])
    // reverted/reset keeps the ability open, reselected — not closed.
    expect(wrapper.find('[data-test="identity-tab"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="status-note"]').text()).toBe('Reverted to the state before your last save.')
  })
})
