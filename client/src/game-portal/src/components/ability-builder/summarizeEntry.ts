// summarizeEntry: a pure, best-effort one-line human summary of an
// AbilityEntryDef (the program's cast-time targeting contract), for the
// Overview card. Mirrors summarizeAction.ts's "never throw, degrade
// gracefully" contract — every field is optional/defensive so a
// freshly-blank or partially-authored entry still renders something sane.

import type { AbilityEntryDef, AbilityEntryType, TargetRelation } from '@/game/abilities/program/abilityProgram'

const ENTRY_TYPE_LABELS: Record<AbilityEntryType, string> = {
  self: 'Self',
  unit: 'Unit',
  ground_point: 'Ground point',
  direction: 'Direction',
  no_target: 'No target',
  passive: 'Passive',
}

// humanizeEntryType turns an AbilityEntryType into its display label,
// falling back to the raw value for anything not in the known set (forward
// compat with a newer server).
export function humanizeEntryType(type: AbilityEntryType): string {
  return ENTRY_TYPE_LABELS[type] ?? type
}

const RELATION_LABELS: Record<TargetRelation, string> = {
  self: 'self',
  ally: 'allies',
  enemy: 'enemies',
  neutral: 'neutrals',
}

function humanizeRelations(relations?: TargetRelation[]): string {
  if (!relations || relations.length === 0) return ''
  return relations.map((r) => RELATION_LABELS[r] ?? r).join('/')
}

function humanizeRange(range: AbilityEntryDef['range']): string {
  return range === 'match_attack_range' ? 'match attack range' : `range ${range}`
}

// ENTRY_TYPES_WITHOUT_RANGE: entry types where `range` isn't a meaningful
// cast-time constraint, so the summary omits it rather than showing a
// leftover/default number that means nothing to the author.
const ENTRY_TYPES_WITHOUT_RANGE = new Set<AbilityEntryType>(['no_target', 'passive', 'self'])

// summarizeEntry renders "<Type> · <relations> · <range>", omitting any
// segment that has nothing to show (e.g. no relations authored yet, or a
// type whose range isn't meaningful). Never throws.
export function summarizeEntry(entry: AbilityEntryDef | undefined | null): string {
  if (!entry) return ''
  const parts = [humanizeEntryType(entry.type)]
  const relations = humanizeRelations(entry.relations)
  if (relations) parts.push(relations)
  if (!ENTRY_TYPES_WITHOUT_RANGE.has(entry.type)) parts.push(humanizeRange(entry.range))
  return parts.join(' · ')
}
