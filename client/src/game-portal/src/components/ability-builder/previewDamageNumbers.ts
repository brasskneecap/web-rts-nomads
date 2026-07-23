// previewDamageNumbers: pure derivation of "which floating damage/heal
// numbers should be on screen the instant a given preview frame becomes the
// displayed frame" — the logic AbilityPreviewCanvas.vue needs to make the
// ability-editor's Preview tab show damage numbers at all (see the
// composable-abilities-phase2/3 plans for why it didn't: the canvas applies
// each frame's snapshot to GameState via DIRECT ASSIGN, deliberately
// bypassing applySnapshot()'s own damageEvents synthesis).
//
// Design: TRACE-driven, not HP-diff. PreviewResult.trace already carries
// authoritative damage_applied/healing_applied events (server-emitted from
// ability_program_registry.go's deal_damage/restore_health actions), each
// stamped with a sim time `t` on the exact same PREVIEW_FRAME_DT_SECONDS grid
// the playhead uses (see AbilityPreviewPanel.vue's frameIndexForTraceEvent /
// activeEventIndices — frameIndexForTraceEvent below duplicates that exact
// bucketing so a spawned number and its highlighted trace-log row always
// agree on which frame they belong to). This is preferred over diffing HP
// between consecutive frames because (a) it's the server's own authoritative
// record rather than a client inference, and (b) an HP-diff would go
// "backward" (healing-shaped) on any rewind/scrub-back, which has no honest
// mapping to a damage number without extra suppression logic.
//
// damage_applied's payload carries {unit: <unit.ID>, amount, type: <DamageType>}
// (ability_program_registry.go:182); healing_applied carries {unit, amount}
// (:245). Neither carries the victim's screen position or unitType, so both
// are resolved against the snapshot for the SAME frame being displayed —
// exactly parity with the live path, where GameState.damageEvents' x/y also
// come from the unit's snapshot position at push time, not the trace/event
// source.

import type { AbilityExecutionTraceEvent } from '@/game/abilities/program/programPreview'
import type { UnitSnapshot } from '@/game/network/protocol'
import { PREVIEW_FRAME_DT_SECONDS } from './previewPlayback'

// EPS mirrors AbilityPreviewPanel.vue's own fudge factor for the same t -> tick
// floor (7 * 0.05 === 0.35000000000000003 in IEEE-754, etc.) — see that
// file's EPS comment for the full rationale. Kept in sync by hand; if the two
// ever diverge a trace event could highlight on one frame but spawn its
// popup on another.
const EPS = 1e-6

// frameIndexForTraceEvent buckets a trace event's sim time into the tick
// whose window [tick*DT, (tick+1)*DT) contains it — duplicated from
// AbilityPreviewPanel.vue's function-scoped helper of the same name (that
// one isn't exported) rather than imported, since introducing a shared
// module for a single one-line floor() felt like more coupling than the
// duplication risk. If this ever needs a THIRD call site, hoist both into
// one shared export.
export function frameIndexForTraceEvent(t: number): number {
  return Math.floor((t + EPS) / PREVIEW_FRAME_DT_SECONDS)
}

export type PreviewDamageNumberKind = 'normal' | 'heal'

export interface PreviewDamageNumberSpec {
  unitId: number
  unitType: string
  x: number
  y: number
  amount: number
  isFriendly: boolean
  kind: PreviewDamageNumberKind
  damageType?: string
}

// findCasterOwnerId resolves the owning player id of whichever scene unit
// sits at exactly (casterX, casterY). The preview harness's caster never
// moves — RunAbilityPreview forces MoveSpeed to 0 and never issues it a move
// order (ability_preview.go) — so its spawn position is a stable, exact-match
// key across every captured frame, with no need to thread a caster unit id
// through PreviewRequest/PreviewFrame just for this. Returns undefined when
// no unit matches (an empty frame, or the caster having been removed — not
// expected in practice but not fatal either); callers then treat every
// victim as non-friendly rather than guessing.
function findCasterOwnerId(units: UnitSnapshot[], casterX: number, casterY: number): string | undefined {
  return units.find((u) => u.x === casterX && u.y === casterY)?.ownerId
}

// damageNumbersForFrameIndex derives the floating popups that belong on
// screen the instant `frameIndex` becomes the displayed frame: every
// damage_applied/healing_applied trace event bucketed into that frame,
// resolved against `units` (the SAME frame's snapshot.units — the victim's
// position/type is read from it, not from the trace event, which carries
// neither).
//
// Degrades silently (skips the event, never throws) when:
//   - the payload is missing/malformed `unit` or `amount` (older/partial
//     server response shape)
//   - `amount` is <= 0 (nothing to show)
//   - the victim id has no matching entry in `units` — most plausibly the
//     unit died on exactly this tick and the captured snapshot already
//     dropped it; there is no sane position to float a number at, so this
//     one hit is silently not visualized rather than guessing (0, 0).
export function damageNumbersForFrameIndex(
  trace: AbilityExecutionTraceEvent[],
  frameIndex: number,
  units: UnitSnapshot[],
  casterX: number,
  casterY: number,
): PreviewDamageNumberSpec[] {
  const casterOwnerId = findCasterOwnerId(units, casterX, casterY)
  const out: PreviewDamageNumberSpec[] = []
  // combine-flagged damage on one unit this frame accumulates into a SINGLE
  // popup (deal_damage's combinePopup — a stacking DoT like caltrops' Barbed,
  // where N independent stacks each trace their own damage_applied but should
  // read as one total). Keyed by victim id; the accumulator is the very spec
  // already pushed into `out`, so summing mutates it in place at its original
  // position. Mirrors the in-game combine (DamageSource.SuppressHitSplit) so the
  // preview and a real cast show the same one number.
  const combined = new Map<number, PreviewDamageNumberSpec>()

  for (const e of trace) {
    if (e.type !== 'damage_applied' && e.type !== 'healing_applied') continue
    if (frameIndexForTraceEvent(e.t) !== frameIndex) continue

    const p = e.payload ?? {}
    const unitId = Number(p.unit)
    const amount = Number(p.amount)
    if (!Number.isFinite(unitId) || !Number.isFinite(amount) || amount <= 0) continue

    const victim = units.find((u) => u.id === unitId)
    if (!victim) continue

    const spec: PreviewDamageNumberSpec = {
      unitId,
      unitType: victim.unitType,
      x: victim.x,
      y: victim.y,
      amount,
      isFriendly: casterOwnerId !== undefined && victim.ownerId === casterOwnerId,
      kind: e.type === 'healing_applied' ? 'heal' : 'normal',
      damageType: e.type === 'damage_applied' && typeof p.type === 'string' ? p.type : undefined,
    }

    if (e.type === 'damage_applied' && p.combine === true) {
      const existing = combined.get(unitId)
      if (existing) {
        existing.amount += amount
        continue
      }
      combined.set(unitId, spec)
    }
    out.push(spec)
  }

  return out
}
