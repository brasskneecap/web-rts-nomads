import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  EditorValidationError,
  saveEditorAbility,
  deleteEditorAbility,
  fetchProjectileIds,
  fetchActionSchema,
  validateAbilityProgram,
  convertAbility,
  runAbilityPreview,
} from './abilityEditorApi'
import type { PreviewRequest } from './program/programPreview'

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

  // The server now reports a 3-way status ('deleted' | 'reverted' | 'reset')
  // instead of the old 2-way — deleteEditorAbility must pass all three
  // through untouched (see EditorAbilityRemoveStatus).
  it('deleteEditorAbility returns the widened 3-way status untouched', async () => {
    mockFetch(200, { id: 'fireball', status: 'reverted' })
    await expect(deleteEditorAbility('fireball')).resolves.toBe('reverted')
  })

  it('deleteEditorAbility still resolves "deleted" and "reset"', async () => {
    mockFetch(200, { id: 'fireball', status: 'deleted' })
    await expect(deleteEditorAbility('fireball')).resolves.toBe('deleted')
  })

  it('fetchProjectileIds maps defs to ids', async () => {
    mockFetch(200, { projectiles: [{ id: 'fire_bolt' }, { id: 'holy_bolt' }] })
    await expect(fetchProjectileIds()).resolves.toEqual(['fire_bolt', 'holy_bolt'])
  })

  it('fetchActionSchema loads and parses the action schema bundle', async () => {
    mockFetch(200, {
      actions: [{ type: 'deal_damage', fields: [{ key: 'amount', label: 'Amount', control: 'number' }], runnable: true }],
      enums: { relations: ['self', 'ally', 'enemy', 'neutral'] },
    })
    const bundle = await fetchActionSchema()
    expect(bundle.actions).toHaveLength(1)
    expect(bundle.actions[0].fields[0].key).toBe('amount')
    expect(bundle.enums.relations).toContain('enemy')
  })

  it('validateAbilityProgram posts the ability and returns issues', async () => {
    const calls: { url: string; init?: RequestInit }[] = []
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      calls.push({ url: String(url), init })
      return {
        ok: true,
        status: 200,
        json: async () => ({ issues: [{ path: 'identity.id', code: 'invalid_id', message: 'bad', severity: 'error' }] }),
      }
    }) as unknown as typeof fetch)
    const issues = await validateAbilityProgram({ id: 'x' })
    expect(calls[0].url).toContain('/abilities/validate')
    expect(calls[0].init?.method).toBe('POST')
    expect(JSON.parse(String(calls[0].init?.body))).toEqual({ ability: { id: 'x' } })
    expect(issues).toHaveLength(1)
    expect(issues[0].code).toBe('invalid_id')
  })

  it('validateAbilityProgram defaults to an empty array when issues is absent', async () => {
    mockFetch(200, {})
    await expect(validateAbilityProgram({ id: 'x' })).resolves.toEqual([])
  })

  it('convertAbility posts to /abilities/{id}/convert and returns the parsed body', async () => {
    const calls: { url: string; init?: RequestInit }[] = []
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      calls.push({ url: String(url), init })
      return {
        ok: true,
        status: 200,
        json: async () => ({ ability: { id: 'fireball' }, warnings: ['legacy field dropped'], runnable: true }),
      }
    }) as unknown as typeof fetch)
    const result = await convertAbility('fireball')
    expect(calls[0].url).toContain('/abilities/fireball/convert')
    expect(calls[0].init?.method).toBe('POST')
    expect(result.ability.id).toBe('fireball')
    expect(result.warnings).toEqual(['legacy field dropped'])
    expect(result.runnable).toBe(true)
  })

  it('convertAbility throws a clear error on 404', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: false,
      status: 404,
      json: async () => ({ error: 'not_found' }),
    })) as unknown as typeof fetch)
    await expect(convertAbility('missing')).rejects.toThrow(/not found|404/i)
  })

  it('runAbilityPreview posts to /abilities/preview and returns the parsed result', async () => {
    const calls: { url: string; init?: RequestInit }[] = []
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      calls.push({ url: String(url), init })
      return {
        ok: true,
        status: 200,
        json: async () => ({
          trace: [{ t: 0, type: 'cast_started' }],
          units: [{ index: 0, team: 'enemy', hpBefore: 200, hpAfter: 60 }],
          casterManaSpent: 40,
          runnable: true,
          warnings: [],
        }),
      }
    }) as unknown as typeof fetch)
    const req: PreviewRequest = {
      ability: { id: 'fireball' },
      seed: 1,
      casterX: 0,
      casterY: 0,
      units: [{ team: 'enemy', x: 120, y: 0, hp: 200, maxHp: 200 }],
      target: 0,
      castX: 120,
      castY: 0,
      casterCharge: 0,
      durationSeconds: 3,
    }
    const result = await runAbilityPreview(req)
    expect(calls[0].url).toContain('/abilities/preview')
    expect(calls[0].init?.method).toBe('POST')
    expect(JSON.parse(String(calls[0].init?.body))).toEqual(req)
    expect(result.trace).toHaveLength(1)
    expect(result.units[0].hpAfter).toBe(60)
    expect(result.runnable).toBe(true)
  })

  it('runAbilityPreview throws a clear error on failure', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: false,
      status: 400,
      json: async () => ({ error: 'preview_failed', message: 'bad ability program' }),
    })) as unknown as typeof fetch)
    const req: PreviewRequest = {
      ability: { id: 'fireball' },
      seed: 1,
      casterX: 0,
      casterY: 0,
      units: [],
      target: 0,
      castX: 0,
      castY: 0,
      casterCharge: 0,
      durationSeconds: 1,
    }
    await expect(runAbilityPreview(req)).rejects.toThrow(/bad ability program/)
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

  it('uploadAbilityIcon rejects with the server-provided reason on failure', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: false,
      status: 400,
      json: async () => ({ error: 'icon_rejected', message: 'icon is not a valid PNG' }),
    })) as unknown as typeof fetch)
    const blob = new Blob([new Uint8Array([1, 2, 3])], { type: 'image/png' })
    await expect(uploadAbilityIcon('x', blob)).rejects.toThrow('not a valid PNG')
    vi.restoreAllMocks()
  })
})
