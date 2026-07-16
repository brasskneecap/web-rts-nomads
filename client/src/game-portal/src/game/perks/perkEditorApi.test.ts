import { afterEach, describe, expect, it, vi } from 'vitest'
import { EditorValidationError, saveEditorPerk, fetchAuthoredPerkDefs } from './perkEditorApi'

afterEach(() => vi.restoreAllMocks())

function mockFetch(status: number, body: unknown) {
  vi.stubGlobal('fetch', vi.fn(async () => ({
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  })) as unknown as typeof fetch)
}

describe('perkEditorApi', () => {
  it('throws EditorValidationError on 400 validation_failed', async () => {
    mockFetch(400, { error: 'validation_failed', message: 'bad config' })
    await expect(saveEditorPerk({ id: 'x' })).rejects.toBeInstanceOf(EditorValidationError)
  })
  it('fetchAuthoredPerkDefs reads the perks array', async () => {
    mockFetch(200, { perks: [{ id: 'flame-ward' }, { id: 'ice-armor' }] })
    await expect(fetchAuthoredPerkDefs()).resolves.toEqual([{ id: 'flame-ward' }, { id: 'ice-armor' }])
  })
})
