// TypeScript mirror of the Go preview API served by POST /abilities/preview
// (game.PreviewRequest / game.PreviewResult). Drives the Phase 6a editor's
// live preview panel: run an authored ability against a synthetic scene and
// render the server's authoritative execution trace + resulting HP deltas.
//
// IMPORTANT: AbilityExecutionTraceEvent's time field is JSON-tagged `t`, not
// `time` — match it exactly. Trace events arrive in execution order; never
// sort them, the panel's timeline relies on that ordering.

import type { AuthoredAbilityDef } from '../abilityEditorForm'
import type { MatchSnapshotMessage } from '../../network/protocol'

// AbilityExecutionTraceEvent mirrors Go's AbilityExecutionTraceEvent.
export interface AbilityExecutionTraceEvent {
  t: number
  type: string
  path?: string
  payload?: Record<string, unknown>
}

// PreviewSceneUnit mirrors Go's PreviewSceneUnit. The `| (string & {})`
// escape hatch keeps `team` open so an unrecognized value from a newer
// server doesn't fail to type-check.
export interface PreviewSceneUnit {
  team: 'ally' | 'enemy' | (string & {})
  x: number
  y: number
  hp: number
  maxHp: number
}

// PreviewUnitResult mirrors Go's PreviewUnitResult.
export interface PreviewUnitResult {
  index: number
  team: string
  hpBefore: number
  hpAfter: number
}

// PreviewRequest mirrors Go's game.PreviewRequest (POST /abilities/preview body).
export interface PreviewRequest {
  ability: AuthoredAbilityDef
  seed: number
  casterX: number
  casterY: number
  units: PreviewSceneUnit[]
  target: number
  castX: number
  castY: number
  durationSeconds: number
}

// PreviewFrame mirrors Go's game.PreviewFrame: one per-tick snapshot of the
// synthetic preview scene. `snapshot` is the SAME wire shape the live client
// applies every tick (MatchSnapshotMessage) — reused as-is, no parallel type.
export interface PreviewFrame {
  tick: number
  t: number
  snapshot: MatchSnapshotMessage
}

// PreviewResult mirrors Go's game.PreviewResult (POST /abilities/preview response).
export interface PreviewResult {
  trace: AbilityExecutionTraceEvent[]
  units: PreviewUnitResult[]
  casterManaSpent: number
  runnable: boolean
  warnings: string[]
  error?: string
  frames: PreviewFrame[]
}

// parsePreviewResult defensively shapes the raw JSON body into a
// PreviewResult: missing `trace`/`units`/`warnings` become empty arrays,
// missing/falsy `runnable` becomes false, numeric fields are coerced. Trace
// events are mapped in place — order is preserved, never sorted.
export function parsePreviewResult(raw: unknown): PreviewResult {
  const src = (raw ?? {}) as {
    trace?: unknown
    units?: unknown
    casterManaSpent?: unknown
    runnable?: unknown
    warnings?: unknown
    error?: unknown
    frames?: unknown
  }

  const rawTrace = Array.isArray(src.trace) ? src.trace : []
  const trace: AbilityExecutionTraceEvent[] = rawTrace.map((entry) => {
    const e = (entry ?? {}) as { t?: unknown; type?: unknown; path?: unknown; payload?: unknown }
    return {
      t: Number(e.t) || 0,
      type: String(e.type ?? ''),
      path: e.path as string | undefined,
      payload: e.payload as Record<string, unknown> | undefined,
    }
  })

  const units = (Array.isArray(src.units) ? src.units : []) as PreviewUnitResult[]
  const warnings = (Array.isArray(src.warnings) ? src.warnings : []) as string[]

  // frames is absent on older/6a-shape responses — default to [] rather than
  // failing, so the panel degrades to trace-only preview.
  const rawFrames = Array.isArray(src.frames) ? src.frames : []
  const frames: PreviewFrame[] = rawFrames
    .filter((entry): entry is Record<string, unknown> => typeof entry === 'object' && entry !== null)
    .map((entry) => {
      const f = entry as { tick?: unknown; t?: unknown; snapshot?: unknown }
      return {
        tick: Number(f.tick) || 0,
        t: Number(f.t) || 0,
        // Minimal validation only: the renderer already tolerates partial
        // snapshots (most fields are optional on MatchSnapshotMessage).
        snapshot: (typeof f.snapshot === 'object' && f.snapshot !== null ? f.snapshot : {}) as MatchSnapshotMessage,
      }
    })

  return {
    trace,
    units,
    casterManaSpent: Number(src.casterManaSpent) || 0,
    runnable: !!src.runnable,
    warnings,
    error: src.error as string | undefined,
    frames,
  }
}

// defaultPreviewRequest returns a sensible default scene for the preview
// panel to start from: one full-health enemy in range (for damage/debuff
// abilities) and one pre-damaged ally (for heal/buff abilities), so either
// kind of effect shows something visible without the user configuring a
// scene first. The panel is expected to let the user tweak units, seed, and
// cast position from here.
export function defaultPreviewRequest(ability: AuthoredAbilityDef): PreviewRequest {
  return {
    ability,
    seed: 1,
    casterX: 0,
    casterY: 0,
    units: [
      { team: 'enemy', x: 120, y: 0, hp: 200, maxHp: 200 },
      { team: 'ally', x: -80, y: 0, hp: 40, maxHp: 100 },
    ],
    target: 0,
    castX: 120,
    castY: 0,
    durationSeconds: 3,
  }
}
