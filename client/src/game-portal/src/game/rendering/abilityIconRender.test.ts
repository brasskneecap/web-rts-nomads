import { describe, expect, it } from 'vitest'
import { parseAbilityIcon, formatAbilityIcon } from './abilityIconRender'

describe('parseAbilityIcon', () => {
  it('parses an effect ref with a frame', () => {
    expect(parseAbilityIcon('effect:meteor@3')).toEqual({ source: 'effect', ref: 'meteor', frame: 3 })
  })

  it('defaults the frame to 0 when omitted', () => {
    expect(parseAbilityIcon('effect:meteor')).toEqual({ source: 'effect', ref: 'meteor', frame: 0 })
    expect(parseAbilityIcon('projectile:fire_bolt')).toEqual({ source: 'projectile', ref: 'fire_bolt', frame: 0 })
  })

  it('parses a beam ref with a frame', () => {
    expect(parseAbilityIcon('beam:siphon_life@4')).toEqual({ source: 'beam', ref: 'siphon_life', frame: 4 })
  })

  it('parses a projectile ref with a frame', () => {
    expect(parseAbilityIcon('projectile:arcane_missiles@2')).toEqual({
      source: 'projectile',
      ref: 'arcane_missiles',
      frame: 2,
    })
  })

  it('treats a plain key as bundled/uploaded ability art (frame 0)', () => {
    expect(parseAbilityIcon('fireball')).toEqual({ source: 'key', ref: 'fireball', frame: 0 })
  })

  it('parses an object ref, defaulting state to idle and frame to 0', () => {
    expect(parseAbilityIcon('object:barrel')).toEqual({ source: 'object', ref: 'barrel', state: 'idle', frame: 0 })
  })

  it('parses an object ref with an animation state', () => {
    expect(parseAbilityIcon('object:barrel@exploding')).toEqual({
      source: 'object',
      ref: 'barrel',
      state: 'exploding',
      frame: 0,
    })
  })

  it('parses an object ref with a state and a chosen frame', () => {
    expect(parseAbilityIcon('object:barrel@exploding#3')).toEqual({
      source: 'object',
      ref: 'barrel',
      state: 'exploding',
      frame: 3,
    })
  })

  it('parses an object ref with a frame but no explicit state (idle)', () => {
    expect(parseAbilityIcon('object:barrel#2')).toEqual({ source: 'object', ref: 'barrel', state: 'idle', frame: 2 })
  })

  it('returns null for empty or placeholder values', () => {
    expect(parseAbilityIcon('')).toBeNull()
    expect(parseAbilityIcon(undefined)).toBeNull()
    expect(parseAbilityIcon('TODO/abilities/x.png')).toBeNull()
  })
})

describe('formatAbilityIcon', () => {
  it('round-trips with parseAbilityIcon', () => {
    for (const s of ['effect:meteor@3', 'beam:siphon_life@2', 'projectile:fire_bolt', 'projectile:frost_bolt@1', 'fireball']) {
      const p = parseAbilityIcon(s)!
      expect(formatAbilityIcon(p.source, p.ref, p.frame)).toBe(s)
    }
  })

  it('omits @0 for the common (first-frame) case', () => {
    expect(formatAbilityIcon('effect', 'meteor', 0)).toBe('effect:meteor')
    expect(formatAbilityIcon('effect', 'meteor', 4)).toBe('effect:meteor@4')
    expect(formatAbilityIcon('key', 'fireball', 0)).toBe('fireball')
  })

  it('formats object refs with optional state (@) and frame (#)', () => {
    expect(formatAbilityIcon('object', 'barrel', 0, 'idle')).toBe('object:barrel')
    expect(formatAbilityIcon('object', 'barrel', 0, 'exploding')).toBe('object:barrel@exploding')
    expect(formatAbilityIcon('object', 'barrel', 3, 'exploding')).toBe('object:barrel@exploding#3')
    expect(formatAbilityIcon('object', 'barrel', 2, 'idle')).toBe('object:barrel#2')
  })

  it('round-trips object refs', () => {
    for (const s of ['object:barrel', 'object:barrel@exploding', 'object:barrel@exploding#3', 'object:barrel#2']) {
      const p = parseAbilityIcon(s)!
      expect(p.source).toBe('object')
      expect(formatAbilityIcon(p.source, p.ref, p.frame, p.source === 'object' ? p.state : undefined)).toBe(s)
    }
  })
})
