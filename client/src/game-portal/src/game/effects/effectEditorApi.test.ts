import { afterEach, describe, expect, it, vi } from 'vitest'
import { EditorValidationError, saveEditorEffect, fetchAuthoredEffectDefs } from './effectEditorApi'

afterEach(() => vi.restoreAllMocks())

function mockFetch(status: number, body: unknown) {
  vi.stubGlobal('fetch', vi.fn(async () => ({
    ok: status >= 200 && status < 300, status, json: async () => body,
  })) as unknown as typeof fetch)
}

describe('effectEditorApi', () => {
  it('throws EditorValidationError on 400 validation_failed', async () => {
    mockFetch(400, { error: 'validation_failed', message: 'bad duration' })
    await expect(saveEditorEffect({ id: 'x' })).rejects.toBeInstanceOf(EditorValidationError)
  })
  it('fetchAuthoredEffectDefs reads the effects array', async () => {
    mockFetch(200, { effects: [{ id: 'glow' }, { id: 'fizzle' }] })
    await expect(fetchAuthoredEffectDefs()).resolves.toEqual([{ id: 'glow' }, { id: 'fizzle' }])
  })
})
