import { afterEach, describe, expect, it, vi } from 'vitest'
import { EditorValidationError, deleteEditorItem, fetchItemAvailability, saveEditorItem } from './itemEditorApi'

function stubFetch(status: number, body: unknown) {
  const mock = vi.fn(async () => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }))
  vi.stubGlobal('fetch', mock)
  return mock
}
afterEach(() => vi.unstubAllGlobals())

describe('itemEditorApi', () => {
  it('saveEditorItem posts to /items and resolves on 201', async () => {
    const mock = stubFetch(201, { id: 'x', status: 'saved' })
    await saveEditorItem({ item: { id: 'x' }, recipe: null, availability: { marketplace: false, wanderingMerchant: false, lootTable: { enabled: false, weight: 0 }, recipeList: false } })
    expect(mock).toHaveBeenCalledOnce()
    const [url, init] = mock.mock.calls[0] as unknown as [string, RequestInit]
    expect(url.endsWith('/items')).toBe(true)
    expect(init.method).toBe('POST')
  })
  it('saveEditorItem throws EditorValidationError with the server message on 400', async () => {
    stubFetch(400, { error: 'validation_failed', message: 'item id "X" must match ^[a-z0-9_]+$' })
    await expect(saveEditorItem({ item: { id: 'X' }, recipe: null, availability: { marketplace: false, wanderingMerchant: false, lootTable: { enabled: false, weight: 0 }, recipeList: false } }))
      .rejects.toSatisfy((e: unknown) => e instanceof EditorValidationError && (e as EditorValidationError).serverMessage.includes('must match'))
  })
  it('deleteEditorItem returns the status field', async () => {
    stubFetch(200, { id: 'x', status: 'reset' })
    await expect(deleteEditorItem('x')).resolves.toBe('reset')
  })
  it('fetchItemAvailability unwraps the availability object', async () => {
    stubFetch(200, { marketplace: true, wanderingMerchant: false, lootTable: { enabled: true, weight: 12 }, recipeList: false })
    const av = await fetchItemAvailability('x')
    expect(av.lootTable.weight).toBe(12)
  })
})
