// Service layer for the Lists tab. Mirrors itemEditorApi: no transforms here,
// just fetch + error shaping.
//
// Before this existed, lists could be SAVED server-side but there was no route
// to call and no loader to read them back — an authored list did not survive a
// restart. The server side of that gap is fixed too (LoadPersistedListsIntoOverlay).
import { EditorValidationError } from './itemEditorApi'
import type { ListDef } from '../maps/listDefs'

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

export async function saveEditorList(list: ListDef): Promise<void> {
  const response = await fetch(`${API_BASE}/lists`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ list }),
  })
  if (response.status === 400) {
    const body = (await response.json().catch(() => null)) as { error?: string; message?: string } | null
    if (body?.error === 'validation_failed') {
      throw new EditorValidationError(body.message ?? 'Validation failed')
    }
    throw new Error(body?.message ?? 'Bad request (400)')
  }
  if (!response.ok) {
    const text = await response.text().catch(() => response.statusText)
    throw new Error(text || `Server error ${response.status}`)
  }
}

export async function deleteEditorList(id: string): Promise<void> {
  const response = await fetch(`${API_BASE}/lists/${encodeURIComponent(id)}`, { method: 'DELETE' })
  // A delete blocked by referential integrity (the list is still bound by a
  // table, map, or neutral group) comes back as a 400 validation_failed whose
  // message names every referrer — surface it like a save validation error.
  if (response.status === 400) {
    const body = (await response.json().catch(() => null)) as { error?: string; message?: string } | null
    if (body?.error === 'validation_failed') {
      throw new EditorValidationError(body.message ?? 'Validation failed')
    }
    throw new Error(body?.message ?? 'Bad request (400)')
  }
  if (!response.ok) {
    const text = await response.text().catch(() => response.statusText)
    throw new Error(text || `Server error ${response.status}`)
  }
}
