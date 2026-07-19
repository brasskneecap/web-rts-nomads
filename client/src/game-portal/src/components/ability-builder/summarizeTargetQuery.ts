// summarizeTargetQuery: a pure, human-readable sentence describing what a
// TargetQueryDef selects, shown beneath the targeting inspector so a designer
// gets constant confirmation the query means what they think it means (per the
// targeting-UX review's "context-sensitive summaries").
//
// Best-effort and NEVER throws — a TargetQueryDef is author-editable and any
// field may be missing or hold a brand-new enum value the client has no prose
// for yet; every read is guarded and unknown values degrade to their raw
// string rather than blanking the sentence. This is PRESENTATION ONLY: nothing
// reads the output back, and drift here loses readability, never correctness.
//
// Vocabulary is prose-specific on purpose (e.g. relation `ally` → "allied",
// origin `impact_position` → "the impact point") rather than reusing the
// dropdown labels from targetQueryHints.ts — a label that reads well in a
// select ("Projectile Impact Point") is not what reads well mid-sentence.

import type { TargetQueryDef } from '@/game/abilities/program/abilityProgram'

// Ordering → the adjective that describes the survivors it keeps. `unit_id`
// ("stable order") is intentionally absent: it has no designer-facing prose
// meaning, so a capped query ordered by it reads as a plain "up to N".
const ORDER_ADJ: Record<string, string> = {
  closest: 'closest',
  farthest: 'farthest',
  lowest_health: 'lowest-health',
  highest_health: 'highest-health',
  lowest_health_percentage: 'lowest-health-percent',
  random: 'random',
}

const RELATION_ADJ: Record<string, string> = {
  self: 'self',
  ally: 'allied',
  enemy: 'enemy',
  neutral: 'neutral',
}

// Origin → the point a radius is measured from, in prose. The three fallback
// origins (projectile/status/summoned) resolve to the caster server-side (see
// resolveOriginLocked, ability_exec_targeting.go), so they read as "the caster"
// — the summary tells the truth about what actually happens.
const ORIGIN_NOUN: Record<string, string> = {
  caster: 'the caster',
  initial_target: 'the initial target',
  initial_target_position: "the initial target's position",
  cast_point: 'the cast point',
  impact_position: 'the impact point',
  current_event_position: 'the triggering unit',
  zone_center: 'the zone center',
  named_context_value: 'the saved position',
  projectile_position: 'the caster',
  status_owner: 'the caster',
  summoned_unit: 'the caster',
}

// Sources that resolve to ONE specific unit — named directly, since the
// pool-narrowing modifiers (count/state/relation/radius) don't apply to a
// single referenced unit.
const SINGULAR_SOURCE_NOUN: Record<string, string> = {
  caster: 'the caster',
  initial_target: 'the initial target',
  current_event: 'the triggering unit',
  source_object: 'the source object',
}

function stateWord(aliveState: string | undefined): string {
  if (aliveState === 'dead') return 'dead'
  if (aliveState === 'any') return '' // "living or dead" adds noise; omit
  return 'living' // '' and 'alive' are the same default
}

function relationWords(relations: string[] | undefined): string {
  if (!Array.isArray(relations) || relations.length === 0) return ''
  return relations.map((r) => RELATION_ADJ[r] ?? r).join('/')
}

function originNoun(q: TargetQueryDef): string {
  // Unset origin resolves to the caster's position server-side.
  if (!q.origin) return 'the caster'
  return ORIGIN_NOUN[q.origin] ?? 'the caster'
}

// The leading quantifier ("the 2 closest", "up to 3", "a random", "all") plus
// whether the noun that follows is plural.
function quantifier(q: TargetQueryDef): { text: string; plural: boolean } {
  const max = typeof q.maxCount === 'number' && q.maxCount > 0 ? q.maxCount : 0
  const adj = q.ordering ? ORDER_ADJ[q.ordering] : undefined
  if (max > 0) {
    const plural = max !== 1
    if (adj === 'random') return { text: max === 1 ? 'a random' : `${max} random`, plural }
    if (adj) return { text: max === 1 ? `the ${adj}` : `the ${max} ${adj}`, plural }
    return { text: max === 1 ? '1' : `up to ${max}`, plural }
  }
  return { text: 'all', plural: true }
}

// The trailing ", excluding …" / ", always including …" clause.
function exclusionsClause(q: TargetQueryDef): string {
  const parts: string[] = []
  if (q.includeInitialTarget) parts.push('always including the initial target')
  if (q.excludeSource) parts.push('excluding the caster')
  if (q.excludeCurrentEvent) parts.push('excluding the triggering unit')
  if (q.excludeRef?.key) parts.push(`excluding anyone already in "${q.excludeRef.key}"`)
  return parts.length ? `, ${parts.join(', ')}` : ''
}

/** Builds a one-sentence, plain-English description of `query`. Returns e.g.
 *  "Select the 2 closest living enemy units within 200 units of the triggering
 *  unit, excluding the triggering unit." Never throws. */
export function summarizeTargetQuery(query: TargetQueryDef | undefined): string {
  const q: TargetQueryDef = query ?? { source: 'all_in_scene' }
  const source = q.source ?? 'all_in_scene'

  // Single referenced unit: just name it (+ any exclusions).
  const singular = SINGULAR_SOURCE_NOUN[source]
  if (singular) return `Select ${singular}${exclusionsClause(q)}.`

  const { text: quant, plural } = quantifier(q)
  const noun = plural ? 'units' : 'unit'
  const core = [quant, stateWord(q.aliveState), relationWords(q.relations), noun]
    .filter(Boolean)
    .join(' ')

  const radius = typeof q.radius === 'number' && q.radius > 0 ? q.radius : 0
  let where: string
  if (source === 'previous_action_targets') {
    where = radius
      ? `within ${radius} units of ${originNoun(q)}, among the previous action's targets`
      : "among the previous action's targets"
  } else if (source === 'named_context') {
    const ref = q.originRef?.key
    where = `from the saved selection${ref ? ` "${ref}"` : ''}`
  } else {
    // all_in_scene (and any unrecognized pool source)
    where = radius ? `within ${radius} units of ${originNoun(q)}` : 'in the scene'
  }

  return `Select ${core} ${where}${exclusionsClause(q)}.`
}
