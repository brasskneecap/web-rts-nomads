import { afterEach, describe, expect, it, vi } from 'vitest'
import { EditorValidationError, saveEditorProjectile, fetchAuthoredProjectileDefs } from './projectileEditorApi'

afterEach(() => vi.restoreAllMocks())

function mockFetch(status: number, body: unknown) {
  vi.stubGlobal('fetch', vi.fn(async () => ({
    ok: status >= 200 && status < 300, status, json: async () => body,
  })) as unknown as typeof fetch)
}

describe('projectileEditorApi', () => {
  it('throws EditorValidationError on 400 validation_failed', async () => {
    mockFetch(400, { error: 'validation_failed', message: 'bad kind' })
    await expect(saveEditorProjectile({ id: 'x' })).rejects.toBeInstanceOf(EditorValidationError)
  })
  it('fetchAuthoredProjectileDefs reads the projectiles array', async () => {
    mockFetch(200, { projectiles: [{ id: 'fire_bolt' }, { id: 'frost_bolt' }] })
    await expect(fetchAuthoredProjectileDefs()).resolves.toEqual([{ id: 'fire_bolt' }, { id: 'frost_bolt' }])
  })
})
