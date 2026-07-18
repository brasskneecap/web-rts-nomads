export interface AuthoredProjectileDef {
  id: string
  kind?: string
  durationMs?: number
  speed?: number
  followEffect?: string
  impactEffect?: string
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  [key: string]: any
}

const MODELED_KEYS = ['id', 'kind', 'durationMs', 'speed', 'followEffect', 'impactEffect'] as const

export interface ProjectileEditorForm extends AuthoredProjectileDef {
  remainder: Record<string, unknown>
}

export function createBlankForm(): ProjectileEditorForm {
  return { id: '', remainder: {} }
}

export function formFromDef(def: AuthoredProjectileDef): ProjectileEditorForm {
  const modeled: Record<string, unknown> = {}
  const remainder: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(def)) {
    if ((MODELED_KEYS as readonly string[]).includes(k)) modeled[k] = v
    else remainder[k] = v
  }
  return { ...(modeled as AuthoredProjectileDef), remainder }
}

export function saveRequestFromForm(form: ProjectileEditorForm): AuthoredProjectileDef {
  const { remainder, ...modeled } = form
  const out: Record<string, unknown> = { ...remainder }
  for (const [k, v] of Object.entries(modeled)) {
    if (v === undefined) continue
    out[k] = v
  }
  return out as AuthoredProjectileDef
}
