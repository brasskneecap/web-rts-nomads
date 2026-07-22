import { afterEach, describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import UnitTypeEditorPanel from './UnitTypeEditorPanel.vue'
import { setAutoAnswerForTest } from '@/components/ui/useConfirmDialog'

interface RecordedCall { method: string; url: string; body?: unknown }
interface Handler { (body: unknown): { status?: number; json?: unknown } }

// A single fetch mock that (a) logs every call (method/url/parsed body) so
// tests can assert ORDER, and (b) lets each test override the response for a
// specific "METHOD /url-suffix" key while every GET catalog endpoint falls
// back to an empty-but-valid default (mirrors the other panel test files'
// stubCatalogFetch idiom, extended to also capture writes).
function stubApi(handlers: Record<string, Handler> = {}, getOverrides: Record<string, unknown> = {}) {
  const calls: RecordedCall[] = []
  const defaultGetMap: Record<string, unknown> = {
    '/catalog/units': {
      units: [{ type: 'archer', name: 'Archer', faction: 'human', hp: 120, damage: 18, attackSpeed: 1.2, moveSpeed: 60, attackRange: 5 }],
    },
    '/catalog/paths': {
      paths: [{ unit: 'archer', path: 'marksman', def: { path: 'marksman', ranks: {} } }],
    },
    '/catalog/factions': { factions: [{ id: 'human', displayName: 'Human' }] },
    '/catalog/archetypes': { archetypes: [] },
    '/catalog/projectiles': { projectiles: [] },
    '/catalog/abilities': { abilities: [] },
    '/catalog/damage-types': { damageTypes: [] },
    '/catalog/buildings': { buildings: [] },
    ...getOverrides,
  }

  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const method = (init?.method ?? 'GET').toUpperCase()
    const body = init?.body ? JSON.parse(init.body as string) : undefined
    calls.push({ method, url: String(url), body })

    for (const [key, handler] of Object.entries(handlers)) {
      const [hMethod, hSuffix] = key.split(' ')
      if (method === hMethod && String(url).endsWith(hSuffix)) {
        const result = handler(body)
        const status = result.status ?? 200
        return { ok: status >= 200 && status < 300, status, json: async () => result.json ?? {} }
      }
    }

    if (method === 'GET') {
      const key = Object.keys(defaultGetMap).find((k) => String(url).endsWith(k))
      return { ok: true, status: 200, json: async () => defaultGetMap[key ?? ''] ?? {} }
    }
    const status = method === 'POST' ? 201 : 200
    return { ok: true, status, json: async () => ({ status: 'saved' }) }
  }) as unknown as typeof fetch)

  return calls
}

function findButtonByText(wrapper: ReturnType<typeof mount>, text: string) {
  const btn = wrapper.findAll('button').find((b) => b.text() === text)
  if (!btn) throw new Error(`no button with text "${text}"`)
  return btn
}

// Reach a path: select its parent unit, open the Promotion Paths tab, then click
// the path's tab in the nested strip. The path now saves/deletes via its own
// compact action bar ("Save Path" / "Delete Path"), NOT the unit header's "Save".
async function selectMarksman(wrapper: ReturnType<typeof mount>) {
  await findButtonByText(wrapper, 'Archer').trigger('click')
  await findButtonByText(wrapper, 'Promotion Paths').trigger('click')
  await flushPromises()
  await findButtonByText(wrapper, 'marksman').trigger('click')
}

async function startNewPath(wrapper: ReturnType<typeof mount>) {
  await findButtonByText(wrapper, 'Archer').trigger('click')
  await findButtonByText(wrapper, 'Promotion Paths').trigger('click')
  await flushPromises()
  await findButtonByText(wrapper, '+ New Path').trigger('click')
}

afterEach(() => vi.restoreAllMocks())

describe('UnitTypeEditorPanel — savePath (existing path)', () => {
  it('calls savePath — with no pathChances write', async () => {
    const calls = stubApi()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()
    await selectMarksman(wrapper)

    await findButtonByText(wrapper, 'Save Path').trigger('click')
    await flushPromises()

    const pathCallIdx = calls.findIndex((c) => c.method === 'POST' && c.url.endsWith('/paths'))
    expect(pathCallIdx).toBeGreaterThanOrEqual(0)

    const pathBody = calls[pathCallIdx].body as { unit: string; path: { path: string } }
    expect(pathBody.unit).toBe('archer')
    expect(pathBody.path.path).toBe('marksman')

    // Perk pools are no longer saved from here — perks are standalone defs
    // edited in the world-editor Perks screen.
    expect(calls.some((c) => c.method === 'POST' && c.url.endsWith('/perks'))).toBe(false)

    // Re-saving an EXISTING path never touches pathChances.
    expect(calls.some((c) => c.method === 'POST' && c.url.endsWith('/units'))).toBe(false)
  })

  it('surfaces a 400 validation_failed message', async () => {
    const message = 'path id "marksman" must match ^[a-z0-9_]+$'
    stubApi({
      'POST /paths': () => ({ status: 400, json: { error: 'validation_failed', message } }),
    })
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()
    await selectMarksman(wrapper)

    await findButtonByText(wrapper, 'Save Path').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain(message)
  })
})

describe('UnitTypeEditorPanel — Add Path ordering (spec §7.1/§9.1)', () => {
  it('saves the path BEFORE writing the parent unit pathChances, merging rather than clobbering', async () => {
    const calls = stubApi({}, {
      '/catalog/units': {
        units: [{
          type: 'archer', name: 'Archer', faction: 'human',
          hp: 120, damage: 18, attackSpeed: 1.2, moveSpeed: 60, attackRange: 5,
          pathChances: { trapper: 2 },
        }],
      },
    })
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()

    await startNewPath(wrapper)

    const idInput = wrapper.find('#pe-id')
    await idInput.setValue('gold_arrow')

    // Checkbox defaults CHECKED per spec §7.1 — leave it as-is.
    expect(wrapper.text()).toContain("Also add to archer's promotion paths")

    await findButtonByText(wrapper, 'Save Path').trigger('click')
    await flushPromises()

    const pathCallIdx = calls.findIndex((c) => c.method === 'POST' && c.url.endsWith('/paths'))
    const unitsCallIdx = calls.findIndex((c) => c.method === 'POST' && c.url.endsWith('/units'))

    expect(pathCallIdx).toBeGreaterThanOrEqual(0)
    expect(unitsCallIdx).toBeGreaterThanOrEqual(0)

    // Path first, then (and only then) the unit's pathChances write — the
    // exact ordering the ID-safety rule requires.
    expect(unitsCallIdx).toBeGreaterThan(pathCallIdx)

    const unitBody = calls[unitsCallIdx].body as { unit: { pathChances?: Record<string, number> } }
    expect(unitBody.unit.pathChances).toEqual({ trapper: 2, gold_arrow: 1 })
  })

  it('does not touch pathChances when the checkbox is unchecked', async () => {
    const calls = stubApi()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()

    await startNewPath(wrapper)

    const idInput = wrapper.find('#pe-id')
    await idInput.setValue('gold_arrow')

    const checkboxLabel = wrapper.findAll('label').find((l) => l.text().includes('Also add to'))!
    await checkboxLabel.find('input[type="checkbox"]').setValue(false)

    await findButtonByText(wrapper, 'Save Path').trigger('click')
    await flushPromises()

    expect(calls.some((c) => c.method === 'POST' && c.url.endsWith('/units'))).toBe(false)
  })
})

describe('UnitTypeEditorPanel — removePath', () => {
  // Deleting a path now prompts first (confirmDelete → the themed dialog).
  // Auto-answer it so this test keeps exercising what the handler does AFTER
  // confirmation; without it the promise never resolves and the test would
  // silently assert nothing.
  beforeEach(() => {
    setAutoAnswerForTest(true)
  })
  afterEach(() => {
    setAutoAnswerForTest(null)
  })

  it('surfaces the server message and does NOT reload when the path is still referenced', async () => {
    const message = 'path "marksman" is still referenced by pathChances on: archer. Remove those rows first.'
    const calls = stubApi({
      'DELETE /paths/marksman': () => ({ status: 400, json: { error: 'validation_failed', message } }),
    })
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()
    await selectMarksman(wrapper)

    const getPathsCallsBefore = calls.filter((c) => c.method === 'GET' && c.url.endsWith('/catalog/paths')).length

    await findButtonByText(wrapper, 'Delete Path').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain(message)
    const getPathsCallsAfter = calls.filter((c) => c.method === 'GET' && c.url.endsWith('/catalog/paths')).length
    expect(getPathsCallsAfter).toBe(getPathsCallsBefore)
  })

  it('has no Delete Path action for a brand-new, not-yet-saved path', async () => {
    stubApi()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()

    await startNewPath(wrapper)

    // A brand-new path has nothing to delete, so the header shows no destructive
    // action at all (rather than a disabled one).
    const deleteBtn = wrapper.findAll('button').find((b) => b.text() === 'Delete Path')
    expect(deleteBtn).toBeUndefined()
  })
})
