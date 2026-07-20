import type { UnitAttackOrigin } from '@/game/maps/unitDefs'

// PathRankStats models the per-rank multipliers/flats authored for a
// promotion path rank (bronze/silver/gold). All fields are optional numbers;
// `undefined` (unauthored) must be distinguished from an explicit 0, so the
// round-trip below never coerces a blank field to a default value.
export interface PathRankStats {
  maxHPMultiplier?: number
  maxMPMultiplier?: number
  healthRegenMultiplier?: number
  damageMultiplier?: number
  attackSpeedMultiplier?: number
  moveSpeedMultiplier?: number
  attackRange?: number
  attackRangeMultiplier?: number
  armor?: number
  dodgeChance?: number
  blockChance?: number
  // Per-rank flat vision override (world pixels); undefined = no override.
  visionRange?: number
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  [key: string]: any
}

// AuthoredPathDef is the full authored shape (superset of the server's
// pathCatalogFile / path def). Modeled fields are typed; unmodeled keys ride
// along via the index signature.
export interface AuthoredPathDef {
  path?: string
  // CLIENT-ONLY routing field: the unit type this path belongs to. It is NOT
  // part of the persisted path file — the server addresses paths via the
  // `{unit, path}` request shape, not a field inside the def itself. It must
  // never be modeled here as a persisted key (see MODELED_PATH_KEYS), never
  // land in `remainder`, and never appear in the object saveRequestFromPathForm
  // writes to `path`.
  parentUnit?: string
  description?: string
  visionRange?: number
  projectile?: string
  damageType?: string
  attackType?: string
  projectileScale?: number
  // REPLACE-list semantics: saving overwrites the full abilities list, it does
  // not merge with whatever the server currently has.
  abilities?: string[]
  channelLoop?: { start: number; end: number }
  bounds?: unknown
  ranks?: Record<string, PathRankStats>
  // Per-facing attack-origin override for this path's art (same shape/editor
  // as the base unit's attackOrigin — UnitSpritePreview's fire-test reads it
  // via getResolvedUnitAttackOrigin, path-keyed). Added here (mirroring
  // unitEditorForm.ts's AuthoredUnitDef) so it round-trips as a MODELED field
  // rather than an opaque remainder blob — the panel's UnitSpritePreview
  // binds `v-model:attack-origin="pathForm.attackOrigin"` directly, which
  // requires undefined-vs-"has a value" to flow through cleanly on save
  // (a remainder-only field would silently ignore a clear-to-undefined edit,
  // since saveRequestFromPathForm's overlay loop skips `undefined` values).
  attackOrigin?: UnitAttackOrigin
  perksByRank?: Record<string, string[]>
  // Per-rank Ability slot: a rank whose key is present here (even with an
  // empty array) grants one ability rolled from this pool instead of a perk
  // (see perksByRank). Key presence, not array length, is what marks a rank
  // as an Ability slot — see rankSlotType in UnitTypeEditorPanel.vue.
  abilityPoolsByRank?: Record<string, string[]>
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  [key: string]: any
}

// The keys the form models — everything NOT in this set is preserved
// verbatim in the form's `remainder`. `parentUnit` is deliberately absent:
// it is client-only routing state, supplied by the caller, never read out of
// or written back into the persisted def.
const MODELED_PATH_KEYS = [
  'path', 'description', 'visionRange', 'projectile', 'damageType', 'attackType',
  'projectileScale', 'abilities', 'channelLoop', 'bounds', 'ranks', 'attackOrigin', 'perksByRank',
  'abilityPoolsByRank',
] as const

export interface PathEditorForm extends AuthoredPathDef {
  remainder: Record<string, unknown>
}

// A path's stats are multipliers, not absolutes — there is no sensible
// "default" stat block to clone the way createBlankForm does for units, so a
// blank path starts with no ranks at all.
export function createBlankPathForm(parentUnit: string): PathEditorForm {
  return { parentUnit, path: '', ranks: {}, remainder: {} }
}

export function pathFormFromDef(def: AuthoredPathDef, parentUnit: string): PathEditorForm {
  const modeled: Record<string, unknown> = {}
  const remainder: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(def)) {
    // parentUnit is supplied by the caller, not read out of the def — if a
    // stray `parentUnit` key is present on the def (it shouldn't be, it's not
    // part of the server file), it is neither modeled nor carried into
    // remainder; it is simply dropped in favor of the caller's value.
    if (k === 'parentUnit') continue
    if ((MODELED_PATH_KEYS as readonly string[]).includes(k)) modeled[k] = v
    else remainder[k] = v
  }
  return { ...(modeled as AuthoredPathDef), parentUnit, remainder }
}

export function saveRequestFromPathForm(form: PathEditorForm): { unit: string; path: AuthoredPathDef } {
  const { remainder, parentUnit, ...modeled } = form
  const out: Record<string, unknown> = { ...remainder }
  for (const [k, v] of Object.entries(modeled)) {
    if (v === undefined) continue
    out[k] = v
  }
  return { unit: parentUnit ?? '', path: out as AuthoredPathDef }
}
