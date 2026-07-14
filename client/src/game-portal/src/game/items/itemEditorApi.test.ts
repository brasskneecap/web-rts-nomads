import { afterEach, describe, expect, it, vi } from 'vitest'
import { EditorValidationError, MAX_ITEM_ICON_BYTES, deleteEditorItem, saveEditorItem, uploadItemIcon } from './itemEditorApi'

function stubFetch(status: number, body: unknown) {
  const mock = vi.fn(async () => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }))
  vi.stubGlobal('fetch', mock)
  return mock
}
afterEach(() => vi.unstubAllGlobals())

describe('itemEditorApi', () => {
  it('saveEditorItem posts to /items and resolves on 201', async () => {
    const mock = stubFetch(201, { id: 'x', status: 'saved' })
    await saveEditorItem({ item: { id: 'x' } })
    expect(mock).toHaveBeenCalledOnce()
    const [url, init] = mock.mock.calls[0] as unknown as [string, RequestInit]
    expect(url.endsWith('/items')).toBe(true)
    expect(init.method).toBe('POST')
  })
  it('saveEditorItem throws EditorValidationError with the server message on 400', async () => {
    stubFetch(400, { error: 'validation_failed', message: 'item id "X" must match ^[a-z0-9_]+$' })
    await expect(saveEditorItem({ item: { id: 'X' } }))
      .rejects.toSatisfy((e: unknown) => e instanceof EditorValidationError && (e as EditorValidationError).serverMessage.includes('must match'))
  })
  it('saveEditorItem throws a generic Error on non-validation 400s', async () => {
    stubFetch(400, { error: 'invalid_json', message: 'unexpected end of JSON input' })
    await expect(saveEditorItem({ item: { id: 'x' } }))
      .rejects.toSatisfy((e: unknown) => e instanceof Error && !(e instanceof EditorValidationError))
  })
  it('deleteEditorItem returns the status field', async () => {
    stubFetch(200, { id: 'x', status: 'reset' })
    await expect(deleteEditorItem('x')).resolves.toBe('reset')
  })
})

describe('uploadItemIcon', () => {
  // The server caps the body with http.MaxBytesReader, which closes the
  // connection mid-upload; the browser reports that as an opaque "Failed to
  // fetch". Rejecting up front is what makes the failure legible.
  it('rejects an oversized icon before it ever hits the network', async () => {
    const mock = stubFetch(201, {})
    const tooBig = new Blob([new Uint8Array(MAX_ITEM_ICON_BYTES + 1)], { type: 'image/png' })
    await expect(uploadItemIcon('x', tooBig)).rejects.toThrow(/limit is 256 KB/)
    expect(mock).not.toHaveBeenCalled()
  })

  it('uploads a file within the limit', async () => {
    const mock = stubFetch(201, { id: 'x', status: 'icon_saved' })
    await uploadItemIcon('x', new Blob([new Uint8Array(1024)], { type: 'image/png' }))
    const [url, init] = mock.mock.calls[0] as unknown as [string, RequestInit]
    expect(url.endsWith('/items/x/image')).toBe(true)
    expect(init.method).toBe('POST')
  })

  it('surfaces the server message rather than the raw JSON body', async () => {
    stubFetch(400, { error: 'icon_rejected', message: 'icon is not a valid PNG' })
    await expect(uploadItemIcon('x', new Blob([new Uint8Array(8)])))
      .rejects.toThrow('icon is not a valid PNG')
  })

  it('reports a network failure as an unreachable server, not "Failed to fetch"', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => { throw new TypeError('Failed to fetch') }))
    await expect(uploadItemIcon('x', new Blob([new Uint8Array(8)])))
      .rejects.toThrow(/Could not reach the server/)
  })
})
