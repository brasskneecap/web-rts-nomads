// Human-readable copy for TargetQueryEditor — field labels, option labels, and
// explanatory InfoTip text. All PRESENTATION: the underlying wire values
// (TargetSource / TargetOrigin / TargetOrdering / TargetRelation string consts
// on the server) are frozen and never change here — only how they're SHOWN.
//
// Nothing in TargetQueryEditor's behavior reads from this file, and nothing
// here should ever gate behavior. Every lookup degrades gracefully to a
// sensible fallback (the raw wire value for a label, '' for a hint) rather
// than throwing or showing wrong/stale copy: the server can add TargetQueryDef
// enum values at any time (see the `enums` bundle TargetQueryEditor already
// treats as forward-compatible), and this file is not guaranteed to be updated
// in lockstep. A drift here loses a friendly label or a hint, never
// correctness — unlike, say, ProgramEnums' hand-maintained triggerTypes list,
// which the same drift risk applies to but which DOES drive behavior (a
// cautionary precedent, not one this file repeats).
//
// Wording verified line-by-line against the server's actual targeting pipeline
// (ability_exec_targeting.go): source gathers, origin/radius/relations filter,
// ordering+maxCount cap. Do not add semantics here that aren't backed by that
// file.

// ── Field labels ───────────────────────────────────────────────────────────
// The human-facing name for each TargetQueryDef sub-field, keyed by the same
// field names TargetQueryEditor's template switches on ('source', 'origin',
// ...). These replace the old execution-internal names (Source/Origin/
// Relations/…) with terms a designer reads without knowing the runtime.
const FIELD_LABELS: Record<string, string> = {
  source: 'Start With',
  origin: 'Search Around',
  originRef: 'Saved Value',
  relations: 'Relationship to Caster',
  radius: 'Search Radius',
  ordering: 'Prioritize By',
  maxCount: 'Maximum Targets',
  includeInitialTarget: 'Always Include Initial Target',
  excludeSource: 'Exclude Caster',
  excludeCurrentEvent: 'Exclude Triggering Unit',
  excludeRef: 'Exclude Saved Set',
  aliveState: 'Unit State',
}

/** The human-facing label for a TargetQueryDef field. Falls back to the raw
 *  field key for a field this file has no copy for yet (a future addition) —
 *  never throws, never shows another field's label. */
export function targetQueryFieldLabel(field: string): string {
  return FIELD_LABELS[field] ?? field
}

// ── Option labels ──────────────────────────────────────────────────────────
// Player-facing labels for individual enum VALUES, keyed by field then by the
// option's wire value. An unmapped value (a brand-new enum the server started
// sending) renders with its raw wire value as the label — same graceful
// degradation as the field labels. "Triggering Unit" reads far clearer than
// "current_event": an event isn't a unit, even though the runtime resolves it
// to one.
const OPTION_LABELS: Record<string, Record<string, string>> = {
  source: {
    caster: 'Caster',
    initial_target: 'Initial Target',
    previous_action_targets: "Previous Action's Targets",
    current_event: 'Triggering Unit',
    named_context: 'Saved Selection',
    source_object: 'Source Object',
    all_in_scene: 'All Units in Scene',
  },
  origin: {
    caster: 'Caster',
    initial_target: 'Initial Target',
    initial_target_position: "Initial Target's Original Position",
    cast_point: 'Chosen Cast Point',
    impact_position: 'Projectile Impact Point',
    current_event_position: "Triggering Unit's Position",
    projectile_position: 'Projectile Position',
    zone_center: 'Zone Center',
    status_owner: 'Status Owner',
    summoned_unit: 'Summoned Unit',
    named_context_value: 'Saved Position',
    targets_center: 'Center of Targets',
  },
  ordering: {
    closest: 'Closest',
    farthest: 'Farthest',
    lowest_health: 'Lowest Current Health',
    lowest_health_percentage: 'Lowest Health Percentage',
    highest_health: 'Highest Current Health',
    random: 'Random',
    unit_id: 'Stable Unit Order',
  },
  // relations 'self' → 'Caster' (not 'Self'): "self" is ambiguous when editing
  // a nested event's query, where the caster and the triggering unit differ.
  relations: {
    self: 'Caster',
    ally: 'Allied',
    enemy: 'Enemy',
    neutral: 'Neutral',
  },
  // '' and 'alive' are the same default (HP > 0 required); 'any' skips the
  // HP check. Verified against applyTargetFiltersLocked (ability_exec_targeting.go).
  aliveState: {
    '': 'Living (default)',
    alive: 'Living',
    dead: 'Dead',
    any: 'Living or Dead',
  },
}

/** The human-facing label for a field's enum VALUE. Falls back to the raw
 *  wire value for an option this file has no copy for yet — never throws. */
export function targetQueryOptionLabel(field: string, optionId: string): string {
  return OPTION_LABELS[field]?.[optionId] ?? optionId
}

/** aliveState isn't published as a ProgramEnums bundle (the server reads it
 *  from applyTargetFiltersLocked's switch directly), so its option LIST lives
 *  here — shared by TargetQueryEditor's control and SchemaField's generic enum
 *  path so filter_targets and select_targets render the identical dropdown, in
 *  this order. */
export const ALIVE_STATE_OPTIONS: { id: string; label: string }[] = ['', 'alive', 'dead', 'any'].map(
  (id) => ({ id, label: targetQueryOptionLabel('aliveState', id) }),
)

// ── Field-level InfoTip copy ─────────────────────────────────────────────────
/** One tip per TargetQueryDef field, keyed by the same field names
 *  TargetQueryEditor's template switches on ('source', 'origin', ...).
 *  Vocabulary matches the field/option labels above. */
const FIELD_HINTS: Record<string, string> = {
  source:
    'Which units to start from, before any filtering. Relationship to Caster and Search Radius then narrow this pool down.',
  origin: 'The point that Search Radius measures from. Has no effect if Search Radius is 0.',
  originRef:
    "Which saved value to use. Only applies when Start With is 'Saved Selection' or Search Around is 'Saved Position' — ignored otherwise.",
  relations:
    "Keep only units with the checked relationship to the caster. 'Any relationship' applies no filter — every unit passes.",
  radius: 'Only keep units within this distance of Search Around. 0 means no distance filter.',
  ordering: 'Sort whoever survived the filters. Only matters when Maximum Targets caps the list.',
  maxCount: 'Keep only the first N after ordering. 0 means unlimited.',
  includeInitialTarget:
    "Force the clicked unit into the results even if it's outside the Search Radius. It must still pass the Relationship to Caster and Unit State checks.",
  excludeSource: 'Drop the caster from the results.',
  excludeCurrentEvent:
    "Drop the unit this trigger is about — whoever entered the zone, died, or was just hit. Use it when a query is centred on that same unit (Search Around: Triggering Unit's Position), so it doesn't pick itself: e.g. a bolt that splits from the enemy it hit to OTHER enemies nearby.",
  excludeRef:
    'Drop every unit already in a saved selection — the "already hit" set for a chain, accumulated with a Save Targets action (Merge). This is how a bounce never lands on the same unit twice.',
  aliveState:
    "Which units count. The default keeps only living units; 'Dead' targets corpses — that's how Raise Skeleton works.",
}

/** Looks up the field-level tip. Returns '' for a field with no copy (e.g. a
 *  brand-new TargetQueryDef field this file hasn't been updated for) —
 *  callers must treat '' as "render no icon", never fall back to some other
 *  text. */
export function targetQueryFieldHint(field: string): string {
  return FIELD_HINTS[field] ?? ''
}

// ── Option-level inline notes ────────────────────────────────────────────────
/** Short, inline suffixes for individual enum VALUES — this is the
 *  highest-value part of the ask: several options are inert (not wired up
 *  server-side) or only meaningful in combination with another field, and
 *  the option list gives no hint of that on its own. These render appended
 *  directly onto the option's label in the dropdown/checkbox itself (see
 *  TargetQueryEditor's `toOptions`), so the note is visible WITHOUT opening
 *  any tooltip — always-on discoverability beats a click for the thing
 *  that's most likely to surprise someone.
 *
 *  'unavailable' marks options the server decodes but does NOT yet enforce
 *  (they silently fall back to the caster, or never match) — see
 *  resolveOriginLocked / applyTargetFiltersLocked's own TODO notes. Until
 *  those are wired (item E: hard validation), the label warns the author.
 *
 *  Keyed by field, then by the option's wire value. An option with no entry
 *  here renders with its plain label, unsuffixed — same graceful degradation
 *  as FIELD_HINTS. */
const OPTION_HINTS: Record<string, Record<string, string>> = {
  source: {
    source_object: 'unavailable',
    named_context: 'pick which in Saved Value',
  },
  origin: {
    projectile_position: 'unavailable',
    status_owner: 'unavailable',
    summoned_unit: 'unavailable',
    named_context_value: 'pick which in Saved Value',
  },
  ordering: {
    random: 'uses the seeded RNG stream',
  },
  relations: {
    neutral: 'unavailable',
  },
}

/** Looks up an option-level suffix for `field`/`optionId`. Returns '' when
 *  there is none — the caller appends nothing in that case, same contract
 *  as targetQueryFieldHint. */
export function targetQueryOptionHint(field: string, optionId: string): string {
  return OPTION_HINTS[field]?.[optionId] ?? ''
}
