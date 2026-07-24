// perkModifierModel.ts
import type { AuthoredPerkDef } from '@/game/perks/perkEditorForm'

export type ModifierKind =
  | 'unitStat' | 'abilityStat' | 'abilityField' | 'abilityModifier'
  | 'abilityRider' | 'aura' | 'grantAbility' | 'perkModifier'
  | 'configValue' | 'effect'

type SourceShape = 'list' | 'map' | 'single'

export interface KindMeta {
  kind: ModifierKind
  label: string
  accent: string
  icon: string
  arrayKey: keyof AuthoredPerkDef
  shape: SourceShape
  editable: boolean
}

export interface ModifierEntry {
  kind: ModifierKind
  meta: KindMeta
  arrayKey: keyof AuthoredPerkDef
  index: number
  id: string
  summary: string
}

export interface ModifierLabels {
  statLabel: (id: string) => string
  abilityStatLabel: (id: string) => string
  abilityLabel: (id: string) => string
}

export const KIND_META: Record<ModifierKind, KindMeta> = {
  unitStat:       { kind: 'unitStat',       label: 'Unit Stat Modifier',    accent: '#a78bfa', icon: '◈', arrayKey: 'statModifiers',   shape: 'list',   editable: true },
  abilityStat:    { kind: 'abilityStat',    label: 'Ability Stat Modifier', accent: '#f59e0b', icon: '✦', arrayKey: 'abilityStats',    shape: 'list',   editable: true },
  abilityField:   { kind: 'abilityField',   label: 'Ability Field Modifier',accent: '#38bdf8', icon: '⊹', arrayKey: 'abilityFields',   shape: 'list',   editable: true },
  abilityModifier:{ kind: 'abilityModifier',label: 'Ability Modifier',      accent: '#2dd4bf', icon: '×', arrayKey: 'abilityModifiers',shape: 'list',   editable: true },
  abilityRider:   { kind: 'abilityRider',   label: 'Ability Rider',         accent: '#f0846c', icon: '⇥', arrayKey: 'abilityRiders',   shape: 'list',   editable: true },
  aura:           { kind: 'aura',           label: 'Aura',                  accent: '#86c46b', icon: '◎', arrayKey: 'auras',           shape: 'list',   editable: true },
  grantAbility:   { kind: 'grantAbility',   label: 'Grant Ability',         accent: '#e7c88a', icon: '➕', arrayKey: 'grantsAbilities', shape: 'list',   editable: true },
  perkModifier:   { kind: 'perkModifier',   label: 'Perk Modifier',         accent: '#ec4899', icon: '⧉', arrayKey: 'perkModifiers',   shape: 'list',   editable: true },
  configValue:    { kind: 'configValue',    label: 'Config Value',          accent: '#94a3b8', icon: '#', arrayKey: 'config',          shape: 'map',    editable: false },
  effect:         { kind: 'effect',         label: 'Cosmetic Effect',       accent: '#818cf8', icon: '✧', arrayKey: 'effect',          shape: 'single', editable: true },
}

export const KIND_ORDER: ModifierKind[] = [
  'unitStat', 'abilityStat', 'abilityField', 'abilityModifier',
  'abilityRider', 'aura', 'perkModifier', 'grantAbility', 'effect', 'configValue',
]

const MINUS = '−' // U+2212

function mult(v: number): string { return `×${v}` }
function signedPct(fraction: number): string {
  const pct = Math.round(fraction * 1000) / 10
  return pct >= 0 ? `+${pct}%` : `${MINUS}${Math.abs(pct)}%`
}
function signedFlat(v: number): string {
  return v >= 0 ? `+${v}` : `${MINUS}${Math.abs(v)}`
}
function fieldPreview(op: string | undefined, value: number): string {
  if (op === 'add') return signedFlat(value)
  return mult(value)
}

function summarize(kind: ModifierKind, raw: unknown, labels: ModifierLabels): string {
  switch (kind) {
    case 'unitStat': {
      const m = raw as { stat: string; op: string; value: number }
      return `${labels.statLabel(m.stat)} ${m.op === 'multiply' ? mult(m.value) : signedFlat(m.value)}`
    }
    case 'abilityStat': {
      const m = raw as { stat: string; ability?: string; flat?: number; pct?: number }
      const parts: string[] = []
      if (typeof m.flat === 'number') parts.push(signedPct(m.flat))
      if (typeof m.pct === 'number') parts.push(signedPct(m.pct))
      const scope = m.ability ? ` (${labels.abilityLabel(m.ability)})` : ''
      return `${labels.abilityStatLabel(m.stat)} ${parts.join(' ') || '—'}${scope}`
    }
    case 'abilityField': {
      const m = raw as { target: string; action: string; field: string; op?: string; value: number }
      return `${labels.abilityLabel(m.target)} ▸ ${m.action} ▸ ${m.field} ${fieldPreview(m.op, m.value)}`
    }
    case 'abilityModifier': {
      const m = raw as { target: string }
      return labels.abilityLabel(m.target)
    }
    case 'abilityRider': {
      const m = raw as { target: string; actions?: unknown[] }
      const n = m.actions?.length ?? 0
      return `${labels.abilityLabel(m.target)} (+${n} action${n === 1 ? '' : 's'})`
    }
    case 'aura': {
      const m = raw as { radius: number; targets: string }
      return `${m.targets}, r${m.radius}`
    }
    case 'grantAbility': return labels.abilityLabel(String(raw))
    case 'perkModifier': {
      const m = raw as { target: string; ops?: unknown[] }
      const n = m.ops?.length ?? 0
      return `${m.target} (${n} op${n === 1 ? '' : 's'})`
    }
    case 'configValue': {
      const [key, value] = raw as [string, number]
      return `${key} = ${value}`
    }
    case 'effect': {
      const m = raw as { name?: string; target?: string }
      return m.name ? `${m.name}${m.target ? ` → ${m.target}` : ''}` : '(effect)'
    }
  }
}

export function buildModifierList(form: AuthoredPerkDef, labels: ModifierLabels): ModifierEntry[] {
  const out: ModifierEntry[] = []
  for (const kind of KIND_ORDER) {
    const meta = KIND_META[kind]
    const source = form[meta.arrayKey]
    if (source == null) continue
    if (meta.shape === 'list') {
      const list = source as unknown[]
      list.forEach((raw, index) => {
        out.push({ kind, meta, arrayKey: meta.arrayKey, index, id: `${kind}:${index}`, summary: summarize(kind, raw, labels) })
      })
    } else if (meta.shape === 'map') {
      Object.entries(source as Record<string, number>).forEach(([k, v], index) => {
        out.push({ kind, meta, arrayKey: meta.arrayKey, index, id: `${kind}:${k}`, summary: summarize(kind, [k, v], labels) })
      })
    } else {
      out.push({ kind, meta, arrayKey: meta.arrayKey, index: 0, id: kind, summary: summarize(kind, source, labels) })
    }
  }
  return out
}
