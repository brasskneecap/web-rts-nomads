// mapContentCache — content-addressed IndexedDB cache of full map bodies.
//
// Keyed by `contentHash`; the value is the flat MapConfig the renderer needs.
// This is the body store behind content-addressed map distribution: on join the
// client tells the server which hashes it holds (getHashesForMap), and the
// server omits the map from the welcome on a hit so the client renders from here.
//
// IndexedDB (not localStorage) because maps are 100s of KB and we want async,
// non-blocking storage with ample quota. Browser-native — works identically in a
// plain browser and the Tauri WebView. All operations degrade to a safe no-op /
// null when IndexedDB is unavailable (private mode, blocked), so the caller just
// treats every join as a cache miss.
//
// Eviction: keep the MAX_ENTRIES most-recently-used maps. Entries are immutable
// under their hash, so a cached map is never stale.

import type { MapConfig } from '@/game/network/protocol'

const DB_NAME = 'nomads-map-cache'
const DB_VERSION = 1
const STORE = 'maps'
const MAX_ENTRIES = 20

interface CacheRecord {
  contentHash: string // keyPath
  mapId: string
  map: MapConfig
  lastUsed: number
}

let dbPromise: Promise<IDBDatabase | null> | null = null

function openDB(): Promise<IDBDatabase | null> {
  if (dbPromise) return dbPromise
  dbPromise = new Promise((resolve) => {
    try {
      const req = indexedDB.open(DB_NAME, DB_VERSION)
      req.onupgradeneeded = () => {
        const db = req.result
        if (!db.objectStoreNames.contains(STORE)) {
          const store = db.createObjectStore(STORE, { keyPath: 'contentHash' })
          store.createIndex('mapId', 'mapId', { unique: false })
        }
      }
      req.onsuccess = () => resolve(req.result)
      req.onerror = () => {
        console.warn('mapContentCache: IndexedDB open failed', req.error)
        resolve(null)
      }
    } catch (e) {
      console.warn('mapContentCache: IndexedDB unavailable', e)
      resolve(null)
    }
  })
  return dbPromise
}

function store(db: IDBDatabase, mode: IDBTransactionMode): IDBObjectStore {
  return db.transaction(STORE, mode).objectStore(STORE)
}

/** Returns the cached map for an exact contentHash, or null on miss / no DB.
 *  Touches the entry's LRU timestamp on a hit. */
export async function getCachedMap(contentHash: string): Promise<MapConfig | null> {
  if (!contentHash) return null
  const db = await openDB()
  if (!db) return null
  return new Promise((resolve) => {
    const os = store(db, 'readwrite')
    const req = os.get(contentHash)
    req.onsuccess = () => {
      const rec = req.result as CacheRecord | undefined
      if (!rec) {
        resolve(null)
        return
      }
      rec.lastUsed = Date.now()
      os.put(rec) // best-effort LRU touch
      resolve(rec.map)
    }
    req.onerror = () => resolve(null)
  })
}

/** Stores a map under its contentHash and evicts down to MAX_ENTRIES. No-op when
 *  IndexedDB is unavailable. */
export async function putCachedMap(
  contentHash: string,
  mapId: string,
  map: MapConfig,
): Promise<void> {
  if (!contentHash || !mapId) return
  const db = await openDB()
  if (!db) return
  await new Promise<void>((resolve) => {
    const os = store(db, 'readwrite')
    const rec: CacheRecord = { contentHash, mapId, map, lastUsed: Date.now() }
    os.put(rec)
    os.transaction.oncomplete = () => resolve()
    os.transaction.onerror = () => resolve()
    os.transaction.onabort = () => resolve()
  })
  await evictToLimit()
}

/** The contentHashes the client currently holds for a given mapId. Sent in
 *  join_match as cachedMapHashes so the server can decide hit/miss. */
export async function getHashesForMap(mapId: string): Promise<string[]> {
  if (!mapId) return []
  const db = await openDB()
  if (!db) return []
  return new Promise((resolve) => {
    const hashes: string[] = []
    const req = store(db, 'readonly').index('mapId').openKeyCursor(IDBKeyRange.only(mapId))
    req.onsuccess = () => {
      const cur = req.result
      if (cur) {
        hashes.push(String(cur.primaryKey))
        cur.continue()
      } else {
        resolve(hashes)
      }
    }
    req.onerror = () => resolve(hashes)
  })
}

async function evictToLimit(): Promise<void> {
  const db = await openDB()
  if (!db) return
  return new Promise((resolve) => {
    const os = store(db, 'readwrite')
    const records: CacheRecord[] = []
    const cursor = os.openCursor()
    cursor.onsuccess = () => {
      const cur = cursor.result
      if (cur) {
        records.push(cur.value as CacheRecord)
        cur.continue()
        return
      }
      const over = records.length - MAX_ENTRIES
      if (over > 0) {
        records.sort((a, b) => a.lastUsed - b.lastUsed)
        for (let i = 0; i < over; i++) os.delete(records[i].contentHash)
      }
      resolve()
    }
    cursor.onerror = () => resolve()
  })
}

/** Decodes a base64 gzip payload (WelcomeMessage.mapGz / MapContentMessage.mapGz)
 *  into a MapConfig using the native DecompressionStream. Throws on malformed
 *  input so callers can fall back to request_map. */
export async function decompressMapGz(mapGzB64: string): Promise<MapConfig> {
  const binary = atob(mapGzB64)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
  const stream = new Blob([bytes]).stream().pipeThrough(new DecompressionStream('gzip'))
  const text = await new Response(stream).text()
  return JSON.parse(text) as MapConfig
}
