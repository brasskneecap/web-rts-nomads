import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { confirmDelete } from './confirmDelete'
import { settle, useConfirmDialogState } from '@/components/ui/useConfirmDialog'

const { request, open } = useConfirmDialogState()

describe('confirmDelete', () => {
  it('opens the themed dialog naming the kind and the entity', async () => {
    const pending = confirmDelete('unit type', 'adept')
    expect(open.value).toBe(true)
    expect(request.value?.title).toBe('Delete unit type "adept"?')
    expect(request.value?.lines).toContain('This cannot be undone.')
    settle(true)
    expect(await pending).toBe(true)
  })

  it('resolves false when the user cancels', async () => {
    const pending = confirmDelete('perk', 'hawk')
    settle(false)
    expect(await pending).toBe(false)
    expect(open.value).toBe(false)
  })

  it('includes an extra consequence when given', async () => {
    const pending = confirmDelete('unit type', 'adept', 'Its promotion paths are deleted with it.')
    expect(request.value?.lines).toContain('Its promotion paths are deleted with it.')
    settle(false)
    await pending
  })

  // A shipped perk/projectile/list does not vanish — the overlay copy is removed
  // and the built-in resurfaces. Telling the author it is irreversible would be
  // false, and would make a safe action look dangerous.
  it('lets the caller replace the irreversibility warning', async () => {
    const pending = confirmDelete('perk', 'hawk', undefined, 'It will reset to its built-in default.')
    expect(request.value?.lines).toContain('It will reset to its built-in default.')
    expect(request.value?.lines).not.toContain('This cannot be undone.')
    settle(false)
    await pending
  })

  // A second ask while one is pending must resolve the FIRST as cancelled — a
  // pending destructive confirm must never be inherited by a different question
  // the user then answers "yes" to.
  it('cancels a pending dialog when a second one is asked', async () => {
    const first = confirmDelete('unit type', 'adept')
    const second = confirmDelete('perk', 'hawk')
    expect(await first).toBe(false)
    expect(request.value?.title).toBe('Delete perk "hawk"?')
    settle(true)
    expect(await second).toBe(true)
  })
})

// This is the guard that actually matters. The unit editor deleted a unit type —
// a hand-authored catalog file — with no prompt at all, and the perk, list and
// projectile editors had the same hole. A test of the helper alone would not
// have caught any of that, because the bug was that the helper was never CALLED.
//
// So: scan the editor panels for catalog-entity delete calls and require each
// one's enclosing function to gate on a confirmation — confirmDelete() for a
// catalog entity, or ask() for the odd non-delete destructive action.
describe('every editor delete is confirmed', () => {
  const componentsDir = join(__dirname, '..')

  // Matches `await deleteEditorUnit(...)`, `await deleteFaction(...)`, etc. —
  // the API wrappers that remove a catalog entity. Deliberately does NOT match
  // row-level array edits (removeAbilityAt, splice) which are undoable local
  // form state, not a destructive server write.
  const DELETE_CALL = /await\s+delete[A-Za-z]*\s*\(/

  function editorPanels(): string[] {
    return readdirSync(componentsDir)
      .filter((f) => f.endsWith('.vue') && /Editor|Panel/.test(f))
      .map((f) => join(componentsDir, f))
  }

  it('gates every catalog-entity delete behind confirmDelete or window.confirm', () => {
    const offenders: string[] = []

    for (const file of editorPanels()) {
      const src = readFileSync(file, 'utf8')
      const lines = src.split('\n')

      lines.forEach((line, i) => {
        if (!DELETE_CALL.test(line)) return

        // Walk back to the enclosing function declaration and check the body
        // between it and this call for a confirmation gate.
        let start = i
        while (start > 0 && !/^\s*(async\s+)?function\s/.test(lines[start])) start--
        const body = lines.slice(start, i).join('\n')
        if (!/confirmDelete\(|ask\(/.test(body)) {
          offenders.push(`${file.split(/[\\/]/).pop()}:${i + 1} — ${line.trim()}`)
        }
      })
    }

    expect(
      offenders,
      `These delete a catalog entity with no confirmation. Gate them with confirmDelete() ` +
        `from @/components/editor/confirmDelete:\n  ${offenders.join('\n  ')}`,
    ).toEqual([])
  })
})
