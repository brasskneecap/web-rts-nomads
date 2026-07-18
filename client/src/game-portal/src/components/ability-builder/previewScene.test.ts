import { describe, expect, it } from 'vitest'
import { PREVIEW_SCENE_ORIGIN, defaultAllyPosition, defaultEnemyPosition } from './previewScene'

describe('previewScene defaults', () => {
  // Regression: the caster used to sit at (0,0) with allies at negative X,
  // which put them off the map's terrain (it spans [0,w]x[0,h]) and rendered
  // the dummies over a black void. Every default position must be positive.
  it('lays default enemy/ally positions out on the map, not around the world origin', () => {
    for (let i = 0; i < 4; i++) {
      const enemy = defaultEnemyPosition(i)
      const ally = defaultAllyPosition(i)
      expect(enemy.x).toBeGreaterThan(0)
      expect(enemy.y).toBeGreaterThan(0)
      expect(ally.x).toBeGreaterThan(0)
      expect(ally.y).toBeGreaterThan(0)
    }
  })

  it('spreads successive enemies/allies apart rather than stacking them', () => {
    const e0 = defaultEnemyPosition(0)
    const e1 = defaultEnemyPosition(1)
    expect(e1.x).not.toBe(e0.x)

    const a0 = defaultAllyPosition(0)
    const a1 = defaultAllyPosition(1)
    expect(a1.x).not.toBe(a0.x)
  })

  it('enemies sit on the positive-X side of the caster, allies on the negative-X side', () => {
    const enemy = defaultEnemyPosition(0)
    const ally = defaultAllyPosition(0)
    expect(enemy.x).toBeGreaterThan(PREVIEW_SCENE_ORIGIN.x)
    expect(ally.x).toBeLessThan(PREVIEW_SCENE_ORIGIN.x)
  })
})
