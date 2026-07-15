// Service layer for the Tables tab. Mirrors listEditorApi.
import { EditorValidationError } from './itemEditorApi'
import type { TableDef } from '../maps/tableDefs'

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

export async function saveEditorTable(table: TableDef): Promise<void> {
  const response = await fetch(`${API_BASE}/tables`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ table }),
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

export async function deleteEditorTable(id: string): Promise<void> {
  const response = await fetch(`${API_BASE}/tables/${encodeURIComponent(id)}`, { method: 'DELETE' })
  if (!response.ok) {
    const text = await response.text().catch(() => response.statusText)
    throw new Error(text || `Server error ${response.status}`)
  }
}
