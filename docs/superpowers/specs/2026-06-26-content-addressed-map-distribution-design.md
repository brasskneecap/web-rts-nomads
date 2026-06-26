# Content-addressed map distribution

**Date:** 2026-06-26
**Status:** Approved design — ready for implementation planning

## Problem

The full map is sent inline in the `welcome` message on every match join. For a
dense map (forest-1 = **262 KB** flat) this is large: it sits near the Steam
path's **1 MiB IPC cap** for the proxy joiner, and re-streams the same static
data on every join. As maps grow, the welcome is the wrong place for this
payload — it shares the socket with the realtime tick stream.

## Goal

Treat the map as a **content-addressed, client-cached asset**:

1. The realtime socket carries raw map bytes only when the client genuinely
   lacks that exact version.
2. When bytes are sent, they are **gzip-compressed** (~30 KB for forest-1).
3. The mechanism is **transport-agnostic** — identical over Steam-proxied WS,
   direct-connect proxied WS, and plain browser WS. No Steam dependency.

## Non-goals / out of scope

- **Chunking.** The `request_map` / `map_content` pair (below) is the seam where
  chunking would live, but compression gives ~30× headroom under the 1 MiB cap,
  so chunking is deferred until a single compressed map approaches that limit.
- **Old browsers.** Decompression uses the native `DecompressionStream`; no
  `pako` fallback. Evergreen browsers + the Tauri WebView only.
- **Web lobby preview.** The Steam lobby-preview hint (`map_hash` metadata)
  already shipped; a browser-MP lobby preview is a separate concern. This change
  is only about the map *transfer*, which needs no lobby hash.

## Key invariant: the compare is server-side, not Steam-side

The hit/miss decision is made in the **host's `join_match` handler**. The client
declares which versions it holds *in the `join_match` message*, which reaches the
authoritative server in every topology:

- Steam desktop: SPA → local server (`?proxy=steam`) → Steam → host server
- Direct-connect: SPA → local server (`?proxy=token`) → host server
- Browser MP: SPA → host/central server over plain WS

Steam's `map_hash` lobby metadata is only a lobby-preview hint and plays no role
in the transfer.

## Data flow

```
join_match { playerId, ..., cachedMapHashes: string[] }
  host: is the match map's contentHash in cachedMapHashes?
   ├─ yes (hit)  → welcome { mapId, contentHash }                 (no map bytes)
   └─ no  (miss) → welcome { mapId, contentHash, mapGz }          (gzipped map)

client on welcome:
   ├─ mapGz present → gunzip → render → cache by contentHash
   └─ mapGz absent  → load map from cache by contentHash → render
       └─ cache entry missing (eviction race) → request_map → map_content
```

`cachedMapHashes` is the list of `contentHash`es the client holds for the
match's `mapId`, derived from its own local cache index — a list (not a single
hash) so a client that happens to hold the host's exact version always registers
a hit.

## Components

### 1. Protocol (`server/pkg/protocol` + client `protocol.ts`)
- `join_match`: add `cachedMapHashes: string[]` (omitted/empty = "I have none").
- `welcome`: **remove** the inline flat `map` object. Always carry `mapId` and
  `contentHash`; carry `mapGz` (base64 of gzipped flat-map JSON) only on a miss.
- New `request_map { mapId, contentHash }` (client→server) and
  `map_content { mapId, contentHash, mapGz }` (server→client) for the
  eviction-race fallback and the future chunking seam.

### 2. Server map transfer (`server/internal/ws` + `server/internal/game`)
- The welcome builder compares the client's `cachedMapHashes` against the match
  map's `contentHash`; gzips the map JSON and attaches `mapGz` only on a miss.
- A `request_map` handler returns `map_content` via the same gzip path.
- Gzip via Go `compress/gzip`. Compression is computed from the same flat map
  the welcome used to inline, so no behavior change to the map content itself.

### 3. Client content cache (`client/.../services/mapContentCache.ts`, new)
- **IndexedDB**, keyed by `contentHash`, value = the flat `MapConfig`.
  IndexedDB (not localStorage) because maps are 100s of KB and we want async,
  non-blocking storage with ample quota. Browser-native; works in browser and
  the Tauri WebView identically.
- Maintains a `mapId → [contentHash]` index so the client can compute
  `cachedMapHashes` for a given map at join time.
- **Eviction:** keep the N most-recently-used maps (default 20), drop oldest.
  Maps are immutable under their hash, so a cached entry is never stale.
- The existing localStorage `mapVersionCache` stays as the lobby-preview
  metadata index; this new store holds the actual map bodies.

### 4. Client welcome / join handling (`client/.../game/network/NetworkClient.ts`)
- On connect, before `join_match`, look up `cachedMapHashes` for the target
  `mapId` from the content cache index and include it.
- On `welcome`: if `mapGz` present → `DecompressionStream('gzip')` → parse →
  render → write to cache. If absent → read from cache by `contentHash` →
  render. Missing cache entry → send `request_map`.
- All decompress/parse/cache work runs **off the connection-critical path**
  (after the welcome is acknowledged), never as a synchronous multi-hundred-KB
  block on entry — the direct lesson from the earlier freeze regression.

## Backward compatibility

The existing **build-version-mismatch guard** (WS close code 4000) already blocks
cross-build play, so host and joiner are always the same build and both speak the
new `welcome`/`join_match` shape. This is an internal protocol change behind that
guard; no old-client fallback is needed.

## Error handling

- Gunzip failure, parse failure, or a cache miss after a declared hit → fall back
  to `request_map`; if that also fails, surface a clear "failed to load map"
  state rather than a silent freeze.
- IndexedDB unavailable/blocked (private mode, quota) → treat every join as a
  miss (always receive `mapGz`); functional, just no caching benefit.
- The map payload never blocks connection establishment.

## Testing

- **Server:** hit omits `mapGz`; miss includes it; `gzip`→`gunzip` round-trips to
  a byte-identical map; `request_map` returns the same content.
- **Client:** cache hit renders with no bytes received; miss decompresses +
  caches; eviction keeps ≤ N; `cachedMapHashes` correctly lists held versions for
  a mapId; gunzip-failure path issues `request_map`; IndexedDB-blocked degrades to
  always-miss.
- **Integration / transport-agnostic:** two clients on the same map → the second
  join transfers zero map bytes; a changed map (new hash) → exactly one transfer,
  then cached. Exercised over a plain WS path (browser topology), not only Steam.

## Affected areas (orientation for planning)

- `server/pkg/protocol/messages.go` — `join_match` `cachedMapHashes`; `welcome`
  drops inline `map`, gains `mapGz`; new `request_map` / `map_content`.
- `server/internal/ws/handlers.go` — welcome builder hit/miss + gzip;
  `request_map` handler.
- `server/internal/game` — expose the flat map + `contentHash` for gzip (already
  available via `GetMapConfigByID`).
- `client/.../game/network/protocol.ts` — mirror the protocol changes.
- `client/.../services/mapContentCache.ts` (new) — IndexedDB content store + index.
- `client/.../game/network/NetworkClient.ts` — send `cachedMapHashes`; welcome
  hit/miss handling; decompression; off-critical-path caching.
