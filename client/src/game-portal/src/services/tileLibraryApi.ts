// Write/read API for the Tile Library (individual cut tiles, distinct from
// whole-sheet Tilesets). Mirrors tilesetEditorApi.ts's fetch/error style.

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

export interface TileAsset {
  id: string
  width: number
  height: number
}

// Thrown on a 400 validation_failed from the server (author-fixable — the
// message names the problem, e.g. a bad id or an unreadable PNG).
export class TileSaveError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'TileSaveError'
  }
}

async function readError(res: Response): Promise<string> {
  const body = (await res.json().catch(() => null)) as { message?: string } | null
  return body?.message ?? `Server error ${res.status}`
}

// List every tile in the library. GET /catalog/tiles.
export async function listTiles(): Promise<TileAsset[]> {
  const res = await fetch(`${API_BASE}/catalog/tiles`)
  if (!res.ok) {
    throw new Error(await readError(res))
  }
  const body = (await res.json()) as { tiles?: TileAsset[] }
  return body.tiles ?? []
}

// Create or overwrite a tile. POST /tiles/{id} with the raw PNG bytes as the
// body (no multipart wrapper) — mirrors uploadTilesetImage.
export async function saveTile(id: string, blob: Blob): Promise<{ image: string }> {
  const res = await fetch(`${API_BASE}/tiles/${encodeURIComponent(id)}`, {
    method: 'POST',
    headers: { 'Content-Type': 'image/png' },
    body: blob,
  })
  if (res.status === 400) {
    throw new TileSaveError(await readError(res))
  }
  if (!res.ok) {
    throw new Error(await readError(res))
  }
  return (await res.json()) as { image: string }
}

// Delete a tile. DELETE /tiles/{id}.
export async function deleteTile(id: string): Promise<void> {
  const res = await fetch(`${API_BASE}/tiles/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
  if (res.status === 400) {
    throw new TileSaveError(await readError(res))
  }
  if (!res.ok) {
    throw new Error(await readError(res))
  }
}

// URL for the tile's PNG. GET /tiles/images/{id}.png.
export function tileImageUrl(id: string): string {
  return `${API_BASE}/tiles/images/${encodeURIComponent(id)}.png`
}
