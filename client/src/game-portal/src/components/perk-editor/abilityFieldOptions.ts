// abilityFieldOptions.ts
// Pure helpers for the Ability Field inspector. Ported from the classic
// PerkEditorPanel so both editors offer the identical, program-derived
// Action/Field dropdowns. `defs` is the map of ability id -> { id, program }
// (from fetchAuthoredAbilityDefs()).

export type AbilityDefLite = { id: string; program?: unknown }

interface WalkedAction { id: string; type: string; config?: Record<string, unknown>; target?: Record<string, unknown> }

// walkProgramActions yields every action at any depth: nested children, a
// create_zone's own triggers, an apply_status_duration's triggers. A perk
// targets by AUTHORED ACTION ID, and those ids live at every level.
export function walkProgramActions(program: unknown): WalkedAction[] {
  const out: WalkedAction[] = []
  const seen = new Set<unknown>()
  const visitActions = (actions: unknown) => {
    if (!Array.isArray(actions)) return
    for (const raw of actions) {
      const a = raw as { id?: string; type?: string; config?: Record<string, unknown>; target?: Record<string, unknown>; children?: unknown }
      if (!a || typeof a !== 'object') continue
      if (a.id) out.push({ id: a.id, type: String(a.type ?? ''), config: a.config, target: a.target })
      visitTriggers(a.children)
      if (a.config && typeof a.config === 'object') visitTriggers((a.config as { triggers?: unknown }).triggers)
    }
  }
  const visitTriggers = (triggers: unknown) => {
    if (!Array.isArray(triggers) || seen.has(triggers)) return
    seen.add(triggers)
    for (const raw of triggers) {
      const t = raw as { actions?: unknown }
      if (t && typeof t === 'object') visitActions(t.actions)
    }
  }
  const p = program as { triggers?: unknown; namedTriggers?: Record<string, unknown> } | undefined
  visitTriggers(p?.triggers)
  for (const t of Object.values(p?.namedTriggers ?? {})) visitTriggers([t])
  return out
}

export function actionsForTarget(defs: Record<string, AbilityDefLite>, target: string): { id: string; label: string }[] {
  const def = defs[target.trim()]
  if (!def) return [] // includes "tag:" targets — no single program to show
  return walkProgramActions(def.program).map((a) => ({ id: a.id, label: a.type ? `${a.id} (${a.type})` : a.id }))
}

export function fieldsForAction(defs: Record<string, AbilityDefLite>, target: string, actionID: string): string[] {
  const def = defs[target.trim()]
  if (!def || !actionID) return []
  const action = walkProgramActions(def.program).find((a) => a.id === actionID)
  if (!action) return []
  const out = Object.entries(action.config ?? {}).filter(([, v]) => typeof v === 'number').map(([k]) => k)
  if (typeof action.target?.radius === 'number') out.push('target.radius')
  return out.sort()
}

// fieldPreview: "+35%" for a ×1.35 multiply, "+2" for an add. U+2212 minus to
// match the rest of the editor's typography.
export function fieldPreview(op: string | undefined, value: number): string {
  if (typeof value !== 'number' || Number.isNaN(value)) return ''
  if (op === 'add') return value >= 0 ? `+${value}` : `−${Math.abs(value)}`
  const pct = Math.round((value - 1) * 1000) / 10
  if (pct === 0) return 'no change'
  return pct > 0 ? `+${pct}%` : `−${Math.abs(pct)}%`
}
