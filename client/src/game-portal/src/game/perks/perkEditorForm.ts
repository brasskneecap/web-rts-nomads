export interface PerkEffectShape {
  name?: string
  target?: string
  sizeScale?: number
  durationSeconds?: number
  variant?: string
}

export interface AuthoredPerkDef {
  id: string
  displayName?: string
  description?: string
  tooltipTemplate?: string
  tooltipTemplateByTrap?: Record<string, string>
  tooltipTemplateByOwnedPerk?: Record<string, string>
  icon?: string
  unitType?: string
  path?: string
  rank?: string
  requiresPerk?: string
  config?: Record<string, number>
  configByRank?: Record<string, Record<string, number>>
  effect?: PerkEffectShape | null
  grantsAbilities?: string[]
  wired?: boolean
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  [key: string]: any
}

const MODELED_KEYS = [
  'id', 'displayName', 'description', 'tooltipTemplate', 'tooltipTemplateByTrap',
  'tooltipTemplateByOwnedPerk', 'icon', 'unitType', 'path', 'rank', 'requiresPerk',
  'config', 'configByRank', 'effect', 'grantsAbilities', 'wired',
] as const

export interface PerkEditorForm extends AuthoredPerkDef {
  remainder: Record<string, unknown>
}

export function createBlankForm(): PerkEditorForm {
  return { id: '', remainder: {} }
}

export function formFromDef(def: AuthoredPerkDef): PerkEditorForm {
  const modeled: Record<string, unknown> = {}
  const remainder: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(def)) {
    if ((MODELED_KEYS as readonly string[]).includes(k)) modeled[k] = v
    else remainder[k] = v
  }
  return { ...(modeled as AuthoredPerkDef), remainder }
}

export function saveRequestFromForm(form: PerkEditorForm): AuthoredPerkDef {
  const { remainder, ...modeled } = form
  const out: Record<string, unknown> = { ...remainder }
  for (const [k, v] of Object.entries(modeled)) {
    if (v === undefined) continue
    if (k === 'wired') continue // derived server-side; never sent
    out[k] = v
  }
  return out as AuthoredPerkDef
}
