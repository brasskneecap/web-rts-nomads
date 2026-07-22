import { describe, it, expect, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ConfirmDialogHost from './ConfirmDialogHost.vue'
import { ask, settle } from './useConfirmDialog'

// The host teleports to body, so assertions read the document rather than the
// wrapper's own subtree. Each test unmounts its own host — clearing
// document.body.innerHTML instead would destroy the node Vue's Teleport still
// holds, and every later mount would fail on insertBefore.
let host: ReturnType<typeof mount> | null = null

afterEach(() => {
  settle(false)
  host?.unmount()
  host = null
})

function mountHost() {
  host = mount(ConfirmDialogHost)
  return host
}

describe('ConfirmDialogHost', () => {
  it('renders nothing until something asks', () => {
    mountHost()
    expect(document.querySelector('[data-test="confirm-dialog"]')).toBeNull()
  })

  it('renders the title and every body line', async () => {
    const w = mountHost()
    void ask({ title: 'Delete unit type "adept"?', lines: ['Paths go too.', 'Cannot be undone.'] })
    await w.vm.$nextTick()

    expect(document.querySelector('[data-test="confirm-title"]')?.textContent).toContain('adept')
    const lines = [...document.querySelectorAll('[data-test="confirm-line"]')].map((n) => n.textContent?.trim())
    expect(lines).toEqual(['Paths go too.', 'Cannot be undone.'])
  })

  it('resolves true on confirm and false on cancel', async () => {
    const w = mountHost()

    const accepted = ask({ title: 'Delete?' })
    await w.vm.$nextTick()
    ;(document.querySelector('[data-test="confirm-accept"]') as HTMLElement).click()
    expect(await accepted).toBe(true)

    const cancelled = ask({ title: 'Delete?' })
    await w.vm.$nextTick()
    ;(document.querySelector('[data-test="confirm-cancel"]') as HTMLElement).click()
    expect(await cancelled).toBe(false)
  })

  // Esc must cancel. A destructive prompt the user cannot dismiss with the
  // key they reflexively reach for is a trap.
  it('cancels on Escape', async () => {
    const w = mountHost()
    const pending = ask({ title: 'Delete?' })
    await w.vm.$nextTick()

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(await pending).toBe(false)
  })

  // Clicking the backdrop is the other reflex for "get me out of here".
  it('cancels on a backdrop click', async () => {
    const w = mountHost()
    const pending = ask({ title: 'Delete?' })
    await w.vm.$nextTick()

    const backdrop = document.querySelector('[data-test="confirm-backdrop"]') as HTMLElement
    backdrop.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    expect(await pending).toBe(false)
  })

  it('closes after settling so it cannot linger over the editor', async () => {
    const w = mountHost()
    void ask({ title: 'Delete?' })
    await w.vm.$nextTick()
    expect(document.querySelector('[data-test="confirm-dialog"]')).not.toBeNull()

    settle(false)
    await w.vm.$nextTick()
    expect(document.querySelector('[data-test="confirm-dialog"]')).toBeNull()
  })

  // The labels are caller-supplied because "Delete"/"Clear"/"Move"/"Reset" all
  // route through this one dialog — a hardcoded "OK" would mislabel most of them.
  it('uses the caller-supplied button labels', async () => {
    const w = mountHost()
    void ask({ title: 'Move it?', confirmLabel: 'Move', cancelLabel: 'Keep' })
    await w.vm.$nextTick()

    expect(document.querySelector('[data-test="confirm-accept"]')?.textContent).toContain('Move')
    expect(document.querySelector('[data-test="confirm-cancel"]')?.textContent).toContain('Keep')
  })
})

// The dialog must not ship on the OLD theme art. It originally used a plain
// <UiPanel>, whose `default` variant is still ui_panel.png — so a brand-new
// component rendered in the dated look that everything else has moved off.
describe('ConfirmDialogHost theming', () => {
  it('paints the frame with the updated main-window panel, not the legacy plate', async () => {
    const w = mountHost()
    void ask({ title: 'Delete?' })
    await w.vm.$nextTick()

    const panel = document.querySelector('[data-test="confirm-dialog"]') as HTMLElement
    const frame = panel.style.getPropertyValue('--ui-window-image')
    expect(frame).toContain('main-window-panel')
    expect(frame).not.toContain('ui_panel')
    expect(panel.style.getPropertyValue('--ui-button-image')).toContain('button')
  })

  // `danger` distinguishes "Delete" from "Move"/"Reset". If it drove nothing it
  // would be an inert authored field — the exact smell this project rejects.
  it('marks a destructive accept button and leaves a benign one unmarked', async () => {
    const w = mountHost()

    void ask({ title: 'Delete?', danger: true })
    await w.vm.$nextTick()
    expect(document.querySelector('[data-test="confirm-accept"]')?.classList.contains('is-danger')).toBe(true)
    settle(false)
    await w.vm.$nextTick()

    void ask({ title: 'Move?', danger: false })
    await w.vm.$nextTick()
    expect(document.querySelector('[data-test="confirm-accept"]')?.classList.contains('is-danger')).toBe(false)
  })
})
