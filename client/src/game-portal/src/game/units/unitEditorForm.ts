import type { UnitAttackOrigin } from '@/game/maps/unitDefs'

// AuthoredUnitDef is the full authored shape (superset of the render-time
// UnitDef). Modeled fields are typed; unmodeled keys (attackVisual/bounds/
// shadow + any future keys) ride along via the index signature.
export interface AuthoredUnitDef {
  type: string
  faction: string
  name?: string
  archetype?: string
  hp?: number
  // Passive HP/sec regen. `undefined` (blank in the form) means "inherit the
  // server's global default"; an explicit 0 means "never regenerates". The
  // server models this as a pointer for exactly that reason, so the form must
  // preserve the undefined-vs-0 distinction rather than coercing blanks to 0.
  healthRegenRate?: number
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
  attackOrigin?: UnitAttackOrigin
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  [key: string]: any
}

// The keys the form models — everything NOT in this set is preserved verbatim
// in the form's `remainder`.
const MODELED_KEYS = [
  'type','faction','name','archetype','hp','healthRegenRate','armor','damage','attackRange',
  'attackSpeed','splashRadius','moveSpeed','resourceCost','meatCost','spawnSeconds',
  'capabilities','combatProfile','attackType','damageType','targetableTypes',
  'projectile','projectileScale','goldGatherAmount','woodGatherAmount','maxMana',
  'manaRegenRate','visionRange','flyer','abilities','requiresBuildings','pathChances',
  'dominionPointDropChance','dominionPointAmount','spawnExp','experience','nonCombat',
  'trainLabel','channelLoop','attackOrigin',
] as const

export interface UnitEditorForm extends AuthoredUnitDef {
  remainder: Record<string, unknown>
}

// The unit a brand-new unit copies its stat block from. Defaults are cloned
// from real catalog content rather than hardcoded, so they follow balance
// changes instead of rotting.
export const TEMPLATE_UNIT_TYPE = 'soldier'

// Only the stat block is inherited. Identity, cost, and gating are per-unit
// design decisions and are deliberately left blank.
const TEMPLATE_STAT_KEYS = [
  'hp', 'armor', 'damage', 'attackRange', 'attackSpeed', 'moveSpeed', 'visionRange',
] as const

// Used only when the template unit is unavailable (offline, empty catalog).
// These are "functional", not "balanced" — the template is the real source.
// They must clear the server's stat floors (hp > 0, moveSpeed > 0, and an
// attacker needs range + speed), or a blank unit cannot be saved at all.
export const FALLBACK_STATS: Readonly<Partial<AuthoredUnitDef>> = {
  hp: 100, armor: 0, damage: 10, attackRange: 32, attackSpeed: 1, moveSpeed: 60, visionRange: 300,
}

// pickTemplateStats pulls the stat block out of the template unit in a fetched
// catalog, falling back per-key so a template missing one field still yields a
// complete, saveable block.
export function pickTemplateStats(defs: AuthoredUnitDef[]): Partial<AuthoredUnitDef> {
  const template = defs.find((d) => d.type === TEMPLATE_UNIT_TYPE)
  if (!template) return { ...FALLBACK_STATS }
  const out: Partial<AuthoredUnitDef> = {}
  for (const key of TEMPLATE_STAT_KEYS) {
    // `??` not `||`: armor 0 (or any other legitimate falsy stat) must survive
    // untouched — only an actually-missing key should fall back.
    out[key] = template[key] ?? FALLBACK_STATS[key]
  }
  return out
}

export function createBlankForm(defaults: Partial<AuthoredUnitDef> = FALLBACK_STATS): UnitEditorForm {
  return { ...defaults, type: '', faction: '', remainder: {} }
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
