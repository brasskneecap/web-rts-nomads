// Client-side mirror of the server's list validation, driving the editor's live
// checklist. The server is still the authority — SaveEditorList re-validates and
// its message is what surfaces on a failed save.
import type { ValidationCheck } from '@/components/editor/ValidationChecklist.vue'
import type { ItemDef } from '../maps/itemDefs'
import { analyzeCoverage } from './rollCoverage'

const ID_PATTERN = /^[a-z0-9_]+$/

// One member row of the editor form. In UNIFORM mode only `item` is used; in
// WEIGHTED mode `min`/`max` are the rolls it owns. Keeping both on one row means
// toggling the mode never loses the member selection.
export type ListMemberForm = {
  item: string
  min: number
  max: number
}

export type ListEditorForm = {
  id: string
  isNew: boolean
  name: string
  /** WEIGHTED when true (members own roll ranges), UNIFORM when false. */
  weighted: boolean
  maxRoll: number
  members: ListMemberForm[]
}

/** The member item ids, in order, dropping blanks. Form-agnostic. */
export function formMemberIds(form: ListEditorForm): string[] {
  return form.members.map((m) => m.item).filter(Boolean)
}

export type ListValidationContext = {
  /** Every list id in the catalog — a NEW list must not collide with one. */
  knownListIds: Set<string>
  /** id → def, for resolving members and spotting the non-craftable ones. */
  itemsById: Map<string, ItemDef>
}

export function validateListForm(form: ListEditorForm, ctx: ListValidationContext): ValidationCheck[] {
  const checks: ValidationCheck[] = []

  const collides = form.isNew && ctx.knownListIds.has(form.id)
  checks.push(
    !form.id
      ? { ok: false, message: 'ID is required.' }
      : !ID_PATTERN.test(form.id)
        ? { ok: false, message: 'ID must be lowercase letters, digits and underscores only.' }
        : collides
          ? { ok: false, message: `A list with the ID "${form.id}" already exists — pick another.` }
          : { ok: true, message: 'ID is valid.' },
  )

  checks.push(form.name
    ? { ok: true, message: 'Name is set.' }
    : { ok: false, message: 'Name is required.' })

  const members = formMemberIds(form)
  const unknown = members.filter((id) => !ctx.itemsById.has(id))
  const dupes = members.filter((id, i) => members.indexOf(id) !== i)
  checks.push(
    members.length === 0
      ? { ok: false, message: 'A list needs at least one item.' }
      : unknown.length > 0
        ? { ok: false, message: `Unknown item: ${unknown.join(', ')}.` }
        : dupes.length > 0
          ? { ok: false, message: `Duplicate item: ${[...new Set(dupes)].join(', ')}.` }
          : { ok: true, message: `${members.length} item${members.length === 1 ? '' : 's'}.` },
  )

  // Weighted lists must tile the die — the same coverage rule tables use. A
  // weighted list has no "nothing" outcome, so a gap here is always a bug.
  if (form.weighted && members.length > 0) {
    const cov = analyzeCoverage(
      form.maxRoll,
      form.members.filter((m) => m.item).map((m) => ({ min: m.min, max: m.max, label: m.item })),
    )
    checks.push(cov.complete
      ? { ok: true, message: 'The die is fully covered.' }
      : { ok: false, message: cov.errors[0] })
  }

  return checks
}

/**
 * The non-craftable warning. A list is UNTYPED, so its members may be perfectly
 * right for one consumer and meaningless to another — a list of potions is fine
 * as shop stock or a loot pool and useless on an Artificer.
 *
 * So this is a WARNING, never a blocking check: it says what will happen, and
 * lets the author decide whether that is what they meant. Returns '' when there
 * is nothing to warn about.
 */
export function nonCraftableWarning(form: ListEditorForm, ctx: ListValidationContext): string {
  const members = formMemberIds(form).filter((id) => ctx.itemsById.has(id))
  if (members.length === 0) return ''
  const notCraftable = members.filter((id) => ctx.itemsById.get(id)?.crafting === undefined)
  if (notCraftable.length === 0) return ''
  return `${notCraftable.length} of ${members.length} items are not craftable — a Recipe Shop or crafting building will ignore them. (Fine if this list is shop stock or a loot pool.)`
}
