// AuthoredUnitDef is the full authored shape (superset of the render-time
// UnitDef). Modeled fields are typed; unmodeled keys (attackVisual/bounds/
// shadow + any future keys) ride along via the index signature.
export interface AuthoredUnitDef {
  type: string
  faction: string
  name?: string
  archetype?: string
  hp?: number
  armor?: number
  damage?: number
  attackRange?: number
  attackSpeed?: number
  splashRadius?: number
  moveSpeed?: number
  resourceCost?: Record<string, number>
  meatCost?: number
  spawnSeconds?: number
  capabilities?: string[]
  combatProfile?: string
  attackType?: string
  damageType?: string
  targetableTypes?: string[]
  projectile?: string
  projectileScale?: number
  goldGatherAmount?: number
  woodGatherAmount?: number
  maxMana?: number
  manaRegenRate?: number
  visionRange?: number
  flyer?: boolean
  abilities?: string[]
  requiresBuildings?: string[]
  pathChances?: Record<string, number>
  dominionPointDropChance?: number
  dominionPointAmount?: number
  spawnExp?: number
  experience?: number
  nonCombat?: boolean
  trainLabel?: string
  channelLoop?: { start: number; end: number }
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  [key: string]: any
}

// The keys the form models — everything NOT in this set is preserved verbatim
// in the form's `remainder`.
const MODELED_KEYS = [
  'type','faction','name','archetype','hp','armor','damage','attackRange',
  'attackSpeed','splashRadius','moveSpeed','resourceCost','meatCost','spawnSeconds',
  'capabilities','combatProfile','attackType','damageType','targetableTypes',
  'projectile','projectileScale','goldGatherAmount','woodGatherAmount','maxMana',
  'manaRegenRate','visionRange','flyer','abilities','requiresBuildings','pathChances',
  'dominionPointDropChance','dominionPointAmount','spawnExp','experience','nonCombat',
  'trainLabel','channelLoop',
] as const

export interface UnitEditorForm extends AuthoredUnitDef {
  remainder: Record<string, unknown>
}

export function createBlankForm(): UnitEditorForm {
  return { type: '', faction: '', remainder: {} }
}

export function formFromDef(def: AuthoredUnitDef): UnitEditorForm {
  const modeled: Record<string, unknown> = {}
  const remainder: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(def)) {
    if ((MODELED_KEYS as readonly string[]).includes(k)) modeled[k] = v
    else remainder[k] = v
  }
  return { ...(modeled as AuthoredUnitDef), remainder }
}

export function saveRequestFromForm(form: UnitEditorForm): AuthoredUnitDef {
  const { remainder, ...modeled } = form
  const out: Record<string, unknown> = { ...remainder }
  for (const [k, v] of Object.entries(modeled)) {
    if (v === undefined) continue
    out[k] = v
  }
  return out as AuthoredUnitDef
}
