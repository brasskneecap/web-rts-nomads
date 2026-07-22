// confirmDelete: the single gate every catalog-entity delete goes through.
//
// It exists because the unit editor's Delete button removed a unit type — a
// hand-authored catalog file with its whole stat block, abilities and promotion
// paths — with no prompt whatsoever. One misclick, gone. The unit, perk, list,
// projectile, effect and table editors all had the same hole; the item, campaign
// and map editors had already grown their own inline window.confirm calls, each
// worded differently.
//
// So this is deliberately ONE function rather than a convention: a shared gate
// can be wired everywhere and TESTED, whereas "remember to prompt" is the thing
// that already failed. The consistent wording is a bonus, not the point.
//
// It renders an IN-APP themed dialog (useConfirmDialog), not window.confirm.
// See that module's doc comment for the full argument — briefly: a native OS
// dialog looks like a crash over a fullscreen game, it restores the system
// cursor this project works hard to suppress, and in the Tauri/Steam build JS
// dialogs depend on platform-webview behaviour we cannot rely on.
import { ask } from '@/components/ui/useConfirmDialog'

/**
 * Ask the user to confirm deleting one catalog entity.
 *
 * @param kind  Singular noun for what is being deleted, lowercase — "unit
 *              type", "perk", "list", "projectile", "promotion path", "faction".
 * @param name  The entity's display name or id, quoted in the prompt so the
 *              user can see they are deleting what they think they are.
 * @param extra Optional extra consequence to spell out, e.g. that a unit type
 *              takes its promotion paths with it.
 * @param warning Overrides the default "This cannot be undone." Some catalog
 *              entities do NOT vanish: deleting a SHIPPED perk, projectile,
 *              list, effect or table removes the overlay copy and the built-in
 *              resurfaces, so telling the author it is irreversible would be a
 *              lie that makes a safe action look scary.
 * @returns true when the user confirmed.
 */
export function confirmDelete(
  kind: string,
  name: string,
  extra?: string,
  warning?: string,
): Promise<boolean> {
  return ask({
    title: `Delete ${kind} "${name}"?`,
    lines: [extra ?? '', warning ?? 'This cannot be undone.'],
    confirmLabel: 'Delete',
    cancelLabel: 'Cancel',
    danger: true,
  })
}
