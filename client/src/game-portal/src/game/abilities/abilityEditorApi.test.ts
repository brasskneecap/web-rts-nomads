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

import { uploadAbilityIcon, abilityIconUrl } from './abilityEditorApi'

describe('ability icon upload', () => {
  it('POSTs the raw blob to /abilities/{id}/image', async () => {
    const calls: { url: string; init?: RequestInit }[] = []
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      calls.push({ url: String(url), init })
      return { ok: true, status: 201, json: async () => ({ status: 'icon_saved' }) }
    }) as unknown as typeof fetch)
    const blob = new Blob([new Uint8Array([1, 2, 3])], { type: 'image/png' })
    await uploadAbilityIcon('pic_bolt', blob)
    expect(calls[0].url).toContain('/abilities/pic_bolt/image')
    expect(calls[0].init?.method).toBe('POST')
    vi.restoreAllMocks()
  })

  it('abilityIconUrl points at the serve route', () => {
    expect(abilityIconUrl('pic_bolt')).toContain('/catalog/abilities/pic_bolt/image')
  })
})
