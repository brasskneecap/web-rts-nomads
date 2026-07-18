// AuthoredEffectDef is the full authored shape of an EffectDef. Modeled fields
// are typed; unmodeled/future keys ride along via the index signature and are
// preserved verbatim through the form's remainder.
export interface AuthoredEffectDef {
  id: string
  spritePath?: string
  duration?: number
  anchor?: string
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  [key: string]: any
}

const MODELED_KEYS = ['id', 'spritePath', 'duration', 'anchor'] as const

export interface EffectEditorForm extends AuthoredEffectDef {
  remainder: Record<string, unknown>
}

export function createBlankForm(): EffectEditorForm {
  return { id: '', remainder: {} }
}

export function formFromDef(def: AuthoredEffectDef): EffectEditorForm {
  const modeled: Record<string, unknown> = {}
  const remainder: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(def)) {
    if ((MODELED_KEYS as readonly string[]).includes(k)) modeled[k] = v
    else remainder[k] = v
  }
  return { ...(modeled as AuthoredEffectDef), remainder }
}

export function saveRequestFromForm(form: EffectEditorForm): AuthoredEffectDef {
  const { remainder, ...modeled } = form
  const out: Record<string, unknown> = { ...remainder }
  for (const [k, v] of Object.entries(modeled)) {
    if (v === undefined) continue
    out[k] = v
  }
  return out as AuthoredEffectDef
}
