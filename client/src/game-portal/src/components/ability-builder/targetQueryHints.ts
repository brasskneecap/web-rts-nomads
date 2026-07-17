// Explanatory copy for TargetQueryEditor's fields, surfaced via InfoTip icons.
//
// This is PRESENTATION ONLY — nothing in TargetQueryEditor's behavior reads
// from this file, and nothing here should ever gate behavior. Every lookup
// degrades gracefully to "no hint" (empty string) rather than throwing or
// showing a wrong/stale tip: the server can add TargetQueryDef enum values
// at any time (see the `enums` bundle TargetQueryEditor already treats as
// forward-compatible), and this file is not guaranteed to be updated in
// lockstep. A drift here loses a hint, never correctness — unlike, say,
// ProgramEnums' hand-maintained triggerTypes list, which the same drift risk
// applies to but which DOES drive behavior (a cautionary precedent, not one
// this file repeats).
//
// Wording verified line-by-line against the server's actual targeting
// pipeline (ability_exec_targeting.go): source gathers, origin/radius/
// relations filter, ordering+maxCount cap. Do not add semantics here that
// aren't backed by that file.

/** One tip per TargetQueryDef field, keyed by the same field names
 *  TargetQueryEditor's template switches on ('source', 'origin', ...). */
const FIELD_HINTS: Record<string, string> = {
  source: 'Which units to start from, before any filtering. Relations and Radius then narrow this pool down.',
  origin: 'The point that Radius measures from. Has no effect if Radius is 0.',
  originRef: "Which saved value to use. Only applies when Source is 'named context' or Origin is 'named context value' — ignored otherwise.",
  relations: 'Keep only units with this relationship to the caster. Leave empty for no filter.',
  radius: 'Only keep units within this distance of Origin. 0 means no distance filter.',
  ordering: 'Sort whoever survived the filters. Only matters when Max Count caps the list.',
  maxCount: 'Keep only the first N after ordering. 0 means no limit.',
  includeInitialTarget: "Force the clicked unit into the results even if it's outside the Radius. It must still pass the Relations and alive checks.",
  excludeSource: 'Drop the caster from the results.',
  excludeCurrentEvent:
    "Drop the unit this trigger is about — whoever entered the zone, died, or was just hit. Use it when a query is centred on that same unit (Origin: current event position), so it doesn't pick itself: e.g. a bolt that splits from the enemy it hit to OTHER enemies nearby.",
  aliveState: "Which units count. The default keeps only living units; 'dead' targets corpses — that's how Raise Skeleton works.",
}

/** Looks up the field-level tip. Returns '' for a field with no copy (e.g. a
 *  brand-new TargetQueryDef field this file hasn't been updated for) —
 *  callers must treat '' as "render no icon", never fall back to some other
 *  text. */
export function targetQueryFieldHint(field: string): string {
  return FIELD_HINTS[field] ?? ''
}

/** Short, inline suffixes for individual enum VALUES — this is the
 *  highest-value part of the ask: several options are inert (not wired up
 *  server-side) or only meaningful in combination with another field, and
 *  the option list gives no hint of that on its own. These render appended
 *  directly onto the option's label in the dropdown/checkbox itself (see
 *  TargetQueryEditor's `withHints`), so the note is visible WITHOUT opening
 *  any tooltip — always-on discoverability beats a click for the thing
 *  that's most likely to surprise someone.
 *
 *  Keyed by field, then by the option's wire value (TargetSource /
 *  TargetOrigin / TargetOrdering / TargetRelation string). An option with no
 *  entry here renders with its plain label, unsuffixed — same graceful
 *  degradation as FIELD_HINTS. */
const OPTION_HINTS: Record<string, Record<string, string>> = {
  source: {
    source_object: 'not implemented',
    named_context: 'pick which in Origin Ref',
  },
  origin: {
    projectile_position: 'not implemented, falls back to caster',
    status_owner: 'not implemented, falls back to caster',
    summoned_unit: 'not implemented, falls back to caster',
    named_context_value: 'pick which in Origin Ref',
  },
  ordering: {
    random: 'uses the seeded RNG stream',
  },
  relations: {
    neutral: 'not implemented, never matches',
  },
}

/** Looks up an option-level suffix for `field`/`optionId`. Returns '' when
 *  there is none — the caller appends nothing in that case, same contract
 *  as targetQueryFieldHint. */
export function targetQueryOptionHint(field: string, optionId: string): string {
  return OPTION_HINTS[field]?.[optionId] ?? ''
}
