// Write API for the Tileset Editor. Reads go through fetchTilesetDefs
// (game/maps/catalog.ts). Mirrors campaignEditorApi.ts's save/delete shape.

import type { TilesetDef } from '@/game/network/protocol'

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

// Thrown on a 400 validation_failed from the server (author-fixable — the
// message names the problem, e.g. a bad id or a still-referenced tileset).
export class TilesetSaveError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'TilesetSaveError'
  }
}

async function readError(res: Response): Promise<string> {
  const body = (await res.json().catch(() => null)) as { message?: string } | null
  return body?.message ?? `Server error ${res.status}`
}

// Create or update a tileset def. POST /tilesets.
export async function saveTileset(def: TilesetDef): Promise<void> {
  const res = await fetch(`${API_BASE}/tilesets`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(def),
  })
  if (res.status === 400) {
    throw new TilesetSaveError(await readError(res))
  }
  if (!res.ok) {
    throw new Error(await readError(res))
  }
}

// Delete an author-created tileset. DELETE /tilesets/{id}. The server
// refuses (400) if a map still references it.
export async function deleteTileset(id: string): Promise<void> {
  const res = await fetch(`${API_BASE}/tilesets/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
  if (res.status === 400) {
    throw new TilesetSaveError(await readError(res))
  }
  if (!res.ok) {
    throw new Error(await readError(res))
  }
}

// Upload the terrain PNG for a tileset. POST /tilesets/{id}/image with the
// raw file bytes as the body (no multipart wrapper) — mirrors uploadItemIcon.
export async function uploadTilesetImage(id: string, file: File): Promise<{ image: string }> {
  const res = await fetch(`${API_BASE}/tilesets/${encodeURIComponent(id)}/image`, {
    method: 'POST',
    headers: { 'Content-Type': 'image/png' },
    body: file,
  })
  if (res.status === 400) {
    throw new TilesetSaveError(await readError(res))
  }
  if (!res.ok) {
    throw new Error(await readError(res))
  }
  return (await res.json()) as { image: string }
}
