import { afterEach, describe, expect, it, vi } from 'vitest'
import { EditorValidationError, deleteEditorItem, saveEditorItem } from './itemEditorApi'

function stubFetch(status: number, body: unknown) {
  const mock = vi.fn(async () => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }))
  vi.stubGlobal('fetch', mock)
  return mock
}
afterEach(() => vi.unstubAllGlobals())

describe('itemEditorApi', () => {
  it('saveEditorItem posts to /items and resolves on 201', async () => {
    const mock = stubFetch(201, { id: 'x', status: 'saved' })
    await saveEditorItem({ item: { id: 'x' }, inputs: [] })
    expect(mock).toHaveBeenCalledOnce()
    const [url, init] = mock.mock.calls[0] as unknown as [string, RequestInit]
    expect(url.endsWith('/items')).toBe(true)
    expect(init.method).toBe('POST')
  })
  it('saveEditorItem throws EditorValidationError with the server message on 400', async () => {
    stubFetch(400, { error: 'validation_failed', message: 'item id "X" must match ^[a-z0-9_]+$' })
    await expect(saveEditorItem({ item: { id: 'X' }, inputs: [] }))
      .rejects.toSatisfy((e: unknown) => e instanceof EditorValidationError && (e as EditorValidationError).serverMessage.includes('must match'))
  })
  it('saveEditorItem throws a generic Error on non-validation 400s', async () => {
    stubFetch(400, { error: 'invalid_json', message: 'unexpected end of JSON input' })
    await expect(saveEditorItem({ item: { id: 'x' }, inputs: [] }))
      .rejects.toSatisfy((e: unknown) => e instanceof Error && !(e instanceof EditorValidationError))
  })
  it('deleteEditorItem returns the status field', async () => {
    stubFetch(200, { id: 'x', status: 'reset' })
    await expect(deleteEditorItem('x')).resolves.toBe('reset')
  })
})
