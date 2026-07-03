// errorReporting — process-global JS error capture wired to the SPA log.
//
// An unhandled exception in a Vue render/update white-screens the WebView (the
// window stays open but the app is gone) and, because it tears down the match
// WebSocket, the server sees a `close 1005`. Without a global handler this is
// invisible: nothing reaches <ts>-spa.log, so a packaged-app crash leaves no
// trace. This module funnels window `error`, `unhandledrejection`, and Vue's
// `errorHandler` into desktopBridge.appendLog so the stack survives.

import type { App } from 'vue'
import { appendLog, type LogEntry } from './desktopBridge'

/** Normalizes an arbitrary thrown value into a structured error LogEntry.
 *  Pure and total — never throws, even on exotic inputs (circular objects,
 *  primitives). Safe to unit test in isolation. */
export function formatErrorEntry(source: string, err: unknown): LogEntry {
  let detail: string
  let stack: string | undefined

  if (err instanceof Error) {
    detail = err.message || err.name || 'Error'
    stack = err.stack
  } else if (typeof err === 'string') {
    detail = err
  } else {
    try {
      detail = JSON.stringify(err)
    } catch {
      detail = String(err)
    }
    if (detail === undefined) detail = String(err)
  }

  const context: Record<string, unknown> = {}
  if (stack) context.stack = stack

  return { level: 'error', message: `[${source}] ${detail}`, context }
}

let installed = false

/** Wires process-global JS error capture. Idempotent — safe to call once at
 *  startup. Pass the Vue app to also capture render/lifecycle errors that Vue
 *  swallows into its own errorHandler. Logging is best-effort and never throws
 *  back into the failing code path. */
export function installGlobalErrorReporting(app?: App): void {
  if (installed) return
  installed = true

  const report = (source: string, err: unknown): void => {
    try {
      void appendLog([formatErrorEntry(source, err)])
    } catch {
      /* never let error logging throw from inside an error handler */
    }
  }

  if (typeof window !== 'undefined') {
    window.addEventListener('error', (event) => {
      report('window.error', event.error ?? event.message)
    })
    window.addEventListener('unhandledrejection', (event) => {
      report('unhandledrejection', event.reason)
    })
  }

  if (app) {
    const prev = app.config.errorHandler
    app.config.errorHandler = (err, instance, info) => {
      report(`vue.errorHandler:${info}`, err)
      // Preserve any handler installed earlier so we don't shadow it.
      if (typeof prev === 'function') prev(err, instance, info)
    }
  }
}
