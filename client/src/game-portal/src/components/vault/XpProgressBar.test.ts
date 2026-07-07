// Pins the vault XP bar fill math. The server sends two fields per unit:
//   xpIntoCurrentRank = XP earned into the current rank band
//   xpToNextRank      = XP REMAINING to reach the next rank
// The full band width is therefore (into + toNext), matching the authoritative
// in-game label (GameState.ts getUnitXpLabel). Regression guard for the bug
// where the fill used into/toNext, so the bar hit 100% at the band's midpoint
// (into == toNext) — i.e. "full at 50% progress".

import { describe, it, expect, afterEach } from 'vitest'
import { createApp } from 'vue'
import XpProgressBar from './XpProgressBar.vue'

function mount(props: Record<string, unknown>) {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const app = createApp(XpProgressBar, props)
  app.mount(host)
  return { host, app }
}

afterEach(() => {
  document.body.innerHTML = ''
})

function fillWidth(host: HTMLElement): string {
  const fill = host.querySelector('.xp__fill') as HTMLElement | null
  return fill?.style.width ?? ''
}

describe('XpProgressBar fill math', () => {
  it('renders the band midpoint (into == toNext) at 50%, not 100%', () => {
    // Bronze band is 100→350 (width 250). A unit at XP=225 is exactly halfway:
    // into=125, toNext=125.
    const { host } = mount({ xpInto: 125, xpToNext: 125 })
    expect(fillWidth(host)).toBe('50%')
    // Label shows progress against the full band, not the remaining amount.
    expect(host.textContent).toContain('125 / 250')
  })

  it('renders a nearly-complete band close to 100%', () => {
    // into=240, toNext=10 → 240/250 = 96%.
    const { host } = mount({ xpInto: 240, xpToNext: 10 })
    expect(fillWidth(host)).toBe('96%')
  })

  it('renders the start of a band at 0%', () => {
    const { host } = mount({ xpInto: 0, xpToNext: 250 })
    expect(fillWidth(host)).toBe('0%')
  })

  it('shows Max Rank with no bar when isMaxRank', () => {
    const { host } = mount({ xpInto: 0, xpToNext: 0, isMaxRank: true })
    expect(host.textContent).toContain('Max Rank')
    expect(host.querySelector('.xp__fill')).toBeNull()
  })
})
