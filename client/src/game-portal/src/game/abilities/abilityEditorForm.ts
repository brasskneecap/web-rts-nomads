import type { AbilityProgram } from './program/abilityProgram'

// AuthoredAbilityDef is the full authored shape (superset of the runtime
// AbilityDef). Modeled fields are typed; unmodeled / future keys ride along via
// the index signature and are preserved verbatim through the form's remainder.
export interface AuthoredAbilityDef {
  id: string
  displayName?: string
  // schemaVersion / program: the composable trigger/action model. Modeled
  // so they survive formFromDef/saveRequestFromForm rather than being
  // dumped into remainder; program's own unknown sub-keys are preserved by
  // parseProgram/serializeProgram (see ./program/abilityProgram.ts), not by
  // this form's remainder mechanism.
  schemaVersion?: number
  program?: AbilityProgram
  // compiledProgram: READ-ONLY, server-computed composable view of a LEGACY
  // ability (compiled from its mechanic fields). Sent by GET /catalog/abilities
  // for display only — null for authored abilities whose own `program` already
  // carries the flow. Display contract: render `program ?? compiledProgram`.
  // NEVER persisted — stripped in saveRequestFromForm.
  compiledProgram?: AbilityProgram
  // runnable: READ-ONLY, server-computed flag indicating whether the
  // ability's flow (program or compiledProgram) is currently executable by
  // the action registry. Sent by GET /catalog/abilities. NEVER persisted —
  // stripped in saveRequestFromForm.
  runnable?: boolean
  // custom: READ-ONLY, server-computed flag — true when the author CREATED
  // this ability (it does not ship in the embedded catalog), false when it's
  // a shipped ability the author has an override for. Drives whether the
  // editor's remove button reads "Delete" (permanent) or "Reset" (restores
  // the shipped default) — see AbilityBuilderPanel's removeLabel. Sent by
  // GET /catalog/abilities. NEVER persisted — stripped in saveRequestFromForm,
  // same as generatedDescription/compiledProgram/runnable.
  custom?: boolean
  // description: OPTIONAL author override of the tooltip prose. Empty ⇒ the
  // generated text (see generatedDescription) is used at runtime. Persisted.
  description?: string
  // generatedDescription: READ-ONLY, server-computed prose the Go generator
  // produces from the current fields. Sent by GET /catalog/abilities as the
  // editor's default / "reset to generated" target. NEVER persisted — stripped
  // in saveRequestFromForm.
  generatedDescription?: string
  type?: 'spell' | 'passive' | ''
  // targeting
  canTargetSelf?: boolean
  canTargetAllies?: boolean
  canTargetEnemies?: boolean
  targetsPoint?: boolean
  // castRange: a world-pixel number OR the sentinel string.
  castRange?: number | 'match_attack_range'
  // cost / timing
  castTime?: number
  manaCost?: number
  cooldown?: number
  // classification
  damageType?: string
  tags?: string[]
  category?: string
  targetCount?: number
  // auto-cast trio
  supportsAutoCast?: boolean
  autoCastTargetSelector?: string
  defaultAutoCast?: boolean
  // presentation / refs (always shown)
  icon?: string
  casterAnimation?: string
  effectOnTarget?: string
  effectAtPoint?: string
  effectScale?: number
  burnEffectAtPoint?: string
  projectile?: string
  // family: basic
  healAmount?: number
  damageAmount?: number
  damagePerSecond?: number
  minorDamage?: boolean
  summonUnitType?: string
  summonCount?: number
  // family: channel-beam
  channelType?: string
  tickIntervalSeconds?: number
  manaCostPerTick?: number
  damagePerTick?: number
  healingMultiplier?: number
  allyHealRadius?: number
  // family: charge-fire
  chargeRequired?: number
  manaToChargeRatio?: number
  missileCount?: number
  damagePerMissile?: number
  targeting?: string
  allowDuplicateTargets?: boolean
  missileDelayMs?: number
  // family: meteor ground-hazard
  impactDelaySeconds?: number
  burnDurationSeconds?: number
  burnDamagePerTick?: number
  burnTickIntervalSeconds?: number
  burnRadius?: number
  // family: arch-mage spell
  radius?: number
  projectileSpeed?: number
  projectileScale?: number
  duration?: number
  chainCount?: number
  bounceRange?: number
  bounceDamageFalloff?: number
  pullStrength?: number
  slowMultiplier?: number
  slowDurationSeconds?: number
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  [key: string]: any
}

export type AbilityFamily = 'basic' | 'channel' | 'charge' | 'meteor' | 'archmage'

// The keys the form models — everything NOT in this set is preserved verbatim
// in the form's `remainder`.
const MODELED_KEYS = [
  'id','displayName','description','generatedDescription',
  'schemaVersion','program','compiledProgram','runnable','custom','byRank',
  'type','canTargetSelf','canTargetAllies','canTargetEnemies',
  'targetsPoint','castRange','castTime','manaCost','cooldown','damageType','tags',
  'category','targetCount','supportsAutoCast','autoCastTargetSelector','defaultAutoCast',
  'icon','casterAnimation','effectOnTarget','effectAtPoint','effectScale','burnEffectAtPoint',
  'projectile','healAmount','damageAmount','damagePerSecond','minorDamage','summonUnitType',
  'summonCount','channelType','tickIntervalSeconds','manaCostPerTick','damagePerTick',
  'healingMultiplier','allyHealRadius','chargeRequired','manaToChargeRatio','missileCount',
  'damagePerMissile','targeting','allowDuplicateTargets','missileDelayMs','impactDelaySeconds',
  'burnDurationSeconds','burnDamagePerTick','burnTickIntervalSeconds','burnRadius','radius',
  'projectileSpeed','projectileScale','duration','chainCount','bounceRange','bounceDamageFalloff',
  'pullStrength','slowMultiplier','slowDurationSeconds',
] as const

export interface AbilityEditorForm extends AuthoredAbilityDef {
  remainder: Record<string, unknown>
}

export function createBlankForm(): AbilityEditorForm {
  return { id: '', remainder: {} }
}

export function formFromDef(def: AuthoredAbilityDef): AbilityEditorForm {
  const modeled: Record<string, unknown> = {}
  const remainder: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(def)) {
    if ((MODELED_KEYS as readonly string[]).includes(k)) modeled[k] = v
    else remainder[k] = v
  }
  return { ...(modeled as AuthoredAbilityDef), remainder }
}

export function saveRequestFromForm(form: AbilityEditorForm): AuthoredAbilityDef {
  const { remainder, ...modeled } = form
  const out: Record<string, unknown> = { ...remainder }
  for (const [k, v] of Object.entries(modeled)) {
    if (v === undefined) continue
    out[k] = v
  }
  // generatedDescription / compiledProgram / runnable / custom are
  // server-computed display metadata, never authored — they must not be
  // persisted back (the server's AbilityDef has no such fields and would
  // ignore them anyway, but stripping keeps the saved JSON clean).
  delete out.generatedDescription
  delete out.compiledProgram
  delete out.runnable
  delete out.custom
  return out as AuthoredAbilityDef
}

// inferFamily picks the most specific mechanic family a def uses, so the panel
// opens on the right section when editing an existing ability. Checked
// most-specific first; defaults to 'basic'. Purely a UI convenience — the form
// always carries and saves every field regardless of the selected family.
export function inferFamily(def: AuthoredAbilityDef): AbilityFamily {
  if (def.channelType) return 'channel'
  if ((def.chargeRequired ?? 0) > 0) return 'charge'
  if ((def.impactDelaySeconds ?? 0) > 0 || (def.burnDurationSeconds ?? 0) > 0) return 'meteor'
  if ((def.chainCount ?? 0) > 0 || (def.radius ?? 0) > 0 || (def.pullStrength ?? 0) > 0 ||
      (def.slowMultiplier ?? 0) > 0 || (def.duration ?? 0) > 0) return 'archmage'
  return 'basic'
}
