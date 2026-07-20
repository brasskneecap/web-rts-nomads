import type { AbilityActionDef } from '@/game/abilities/program/abilityProgram'

export interface PerkEffectShape {
  name?: string
  target?: string
  sizeScale?: number
  durationSeconds?: number
  variant?: string
}

// AbilityModifier mirrors the Go AbilityModifier struct: a scalar multiplier
// bundle a perk applies to a named ability (target = ability id).
export interface AbilityModifier {
  target: string
  damageMult?: number
  healMult?: number
  manaCostMult?: number
  rangeMult?: number
}

// AbilityRider mirrors the Go AbilityRider struct: extra action fragments a
// perk grafts onto a named ability's trigger. `actions` reuses the same
// AbilityActionDef the ability builder authors (@/game/abilities/program/
// abilityProgram) rather than redefining a parallel action shape.
export interface AbilityRider {
  target: string
  trigger: string
  actions: AbilityActionDef[]
}

// PerkStatModifier mirrors the Go PerkDef.statModifiers entry: a typed,
// registry-validated unit-stat bonus a perk grants. `stat` must be one of
// the ids in game/stats/statRegistry.ts (the client mirror of the Go
// statRegistry). `stage` defaults server-side to "base" when omitted.
export interface PerkStatModifier {
  stat: string
  op: 'add' | 'multiply'
  value: number
  stage?: 'intrinsic' | 'base' | 'final'
}

// PerkAura mirrors the Go PerkDef.auras entry: a continuously-emitted area
// effect the perk's owner grants to nearby units (as opposed to
// statModifiers, which only ever affect the owner itself). `statModifiers`
// reuses PerkStatModifier for the nested list, but the server's aura fold
// site only ever consumes `op: "add"` with `stage` empty/"base" — the editor
// never renders op/stage controls for aura rows (see PerkEditorPanel's Auras
// section) and always emits `op: "add"` with `stage` omitted.
export interface PerkAura {
  radius: number
  targets: 'allies' | 'enemies'
  includeSelf?: boolean
  stacking?: 'max'
  perAdditionalSource?: number
  statModifiers: PerkStatModifier[]
  // ringColor: PURELY PRESENTATIONAL override for the HUD aura ring's color
  // (see PerkAura.RingColor, server/internal/game/perk_defs.go). Must be a
  // valid CSS hex color (#rgb / #rrggbb / #rrggbbaa) when set — the server
  // rejects anything else. Omitted (not empty string) when the designer
  // hasn't overridden it, so the ring falls back to the owning player's
  // color, same as before this field existed.
  ringColor?: string
}

export interface AuthoredPerkDef {
  id: string
  displayName?: string
  description?: string
  tooltipTemplate?: string
  tooltipTemplateByTrap?: Record<string, string>
  tooltipTemplateByOwnedPerk?: Record<string, string>
  icon?: string
  path?: string
  requiresPerk?: string
  config?: Record<string, number>
  configByRank?: Record<string, Record<string, number>>
  effect?: PerkEffectShape | null
  grantsAbilities?: string[]
  abilityModifiers?: AbilityModifier[]
  abilityRiders?: AbilityRider[]
  statModifiers?: PerkStatModifier[]
  auras?: PerkAura[]
  wired?: boolean
  // generatedDescription: READ-ONLY, server-computed prose the Go generator
  // derives from this perk's typed data (statModifiers/abilityModifiers/
  // riders). Sent by GET /catalog/perks, same as `wired`. NEVER persisted —
  // stripped in saveRequestFromForm. Mirrors AuthoredAbilityDef.generatedDescription.
  generatedDescription?: string
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  [key: string]: any
}

const MODELED_KEYS = [
  'id', 'displayName', 'description', 'tooltipTemplate', 'tooltipTemplateByTrap',
  'tooltipTemplateByOwnedPerk', 'icon', 'path', 'requiresPerk',
  'config', 'configByRank', 'effect', 'grantsAbilities',
  'abilityModifiers', 'abilityRiders', 'statModifiers', 'auras', 'wired', 'generatedDescription',
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
    if (k === 'generatedDescription') continue // derived server-side; never sent
    out[k] = v
  }
  return out as AuthoredPerkDef
}
