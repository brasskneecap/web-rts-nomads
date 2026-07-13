import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  EditorValidationError,
  saveEditorAbility,
  fetchProjectileIds,
} from './abilityEditorApi'

afterEach(() => vi.restoreAllMocks())

function mockFetch(status: number, body: unknown) {
  vi.stubGlobal('fetch', vi.fn(async () => ({
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  })) as unknown as typeof fetch)
}

describe('abilityEditorApi', () => {
  it('saveEditorAbility throws EditorValidationError on 400 validation_failed', async () => {
    mockFetch(400, { error: 'validation_failed', message: 'bad category' })
    await expect(saveEditorAbility({ id: 'x' })).rejects.toBeInstanceOf(EditorValidationError)
  })

  it('fetchProjectileIds maps defs to ids', async () => {
    mockFetch(200, { projectiles: [{ id: 'fire_bolt' }, { id: 'holy_bolt' }] })
    await expect(fetchProjectileIds()).resolves.toEqual(['fire_bolt', 'holy_bolt'])
  })
})
