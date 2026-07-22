// useConfirmDialog: the app's in-game replacement for window.confirm.
//
// WHY NOT window.confirm. It was the first implementation and it is wrong here
// for three reasons, in increasing order of importance:
//
//   1. A native OS dialog over a fullscreen fantasy RTS reads as a crash, not a
//      prompt.
//   2. It is OS-drawn, so it brings back the system arrow cursor and steals
//      focus — and this project goes to real lengths to keep its custom cursor
//      (see CLAUDE.md's cursor rules and style.css's two global rules).
//   3. The game also ships as a Tauri/Steam desktop build. There, JS dialogs
//      are the platform webview's business: WebView2 renders them, but the
//      macOS/Linux webviews only do so when the host implements UI-delegate
//      callbacks. "Probably works on Windows, unverified elsewhere" is a bad
//      foundation for the only thing standing between a misclick and a deleted
//      catalog file. A DOM modal behaves identically everywhere.
//
// SHAPE. A module-level singleton with a promise-based `ask`, plus one
// <ConfirmDialogHost /> mounted in App.vue. Call sites stay one-liners and no
// panel has to host a dialog component of its own — which matters because the
// call sites are spread across eight delete handlers in six editors.
import { ref, shallowRef } from 'vue'

export interface ConfirmRequest {
  title: string
  /** Body paragraphs, rendered in order. Empty entries are skipped. */
  lines: string[]
  confirmLabel: string
  cancelLabel: string
  /** Paints the confirm button as destructive. */
  danger: boolean
}

const request = shallowRef<ConfirmRequest | null>(null)
const open = ref(false)
let resolveCurrent: ((ok: boolean) => void) | null = null

/** Reactive state consumed by ConfirmDialogHost. Not for direct use elsewhere. */
export function useConfirmDialogState() {
  return { request, open }
}

/**
 * Show the confirm dialog and resolve with the user's choice.
 *
 * Only ONE dialog can be open at a time. A second `ask` while one is pending
 * resolves the first as CANCELLED rather than stacking or dropping it — a
 * pending destructive confirm must never be silently inherited by a different
 * question the user then answers "yes" to.
 */
export function ask(req: Partial<ConfirmRequest> & { title: string }): Promise<boolean> {
  // Test seam. Component tests that exercise a delete handler care about what
  // the handler DOES once confirmed, not about the dialog — without this each
  // would have to mount the host and click through it, or (worse) the promise
  // would hang unresolved and the test would assert nothing. See
  // setAutoAnswerForTest.
  if (autoAnswer !== null) return Promise.resolve(autoAnswer)

  if (resolveCurrent) {
    resolveCurrent(false)
    resolveCurrent = null
  }
  request.value = {
    title: req.title,
    lines: (req.lines ?? []).filter((l) => l.length > 0),
    confirmLabel: req.confirmLabel ?? 'Delete',
    cancelLabel: req.cancelLabel ?? 'Cancel',
    danger: req.danger ?? true,
  }
  open.value = true
  return new Promise<boolean>((resolve) => {
    resolveCurrent = resolve
  })
}

/** Settle the open dialog. Called by the host on button click / Esc / backdrop. */
export function settle(ok: boolean) {
  open.value = false
  request.value = null
  const resolve = resolveCurrent
  resolveCurrent = null
  resolve?.(ok)
}

// autoAnswer short-circuits ask() in tests. null (the default, and always the
// value in production) means "really open the dialog".
let autoAnswer: boolean | null = null

/**
 * Make every subsequent ask() resolve immediately with `value`, without opening
 * the dialog. Pass null to restore real behaviour. TESTS ONLY.
 */
export function setAutoAnswerForTest(value: boolean | null) {
  autoAnswer = value
}

/** Test seam: drop any pending dialog without resolving a stale promise. */
export function resetConfirmDialogForTest() {
  settle(false)
}
