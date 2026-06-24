# Versioned maps with host-authoritative P2P sync

**Date:** 2026-06-24
**Status:** Approved design — ready for implementation planning

## Problem

Maps are embedded in the Go server binary via `go:embed catalog/maps/*.json`.
Changing a map today means recompiling the Go sidecar and redistributing the
desktop app to every player. We want to **edit a map and have it take effect
for the host and all joiners without rebuilding/redistributing the app**, with
a version number used as the safety mechanism so mismatched copies are detected
and reconciled at join time.

## Core insight (why this is small)

In Steam multiplayer the joiner is a **pure proxy**: their local Go server does
not run the simulation; it forwards bytes to the **host's** server, which is
authoritative. When a match starts, the host sends the joiner the full
`MapConfig` in the `WelcomeMessage`, and the joiner renders it. **So during a
live match the joiner already plays on the host's exact map**, even if the
joiner's locally-embedded copy of that map is stale.

That means the feature reduces to three gaps:

1. A **version** to detect a host/joiner mismatch at join time.
2. A sane **lobby preview** when the joiner's copy is stale or missing.
3. **Persisting** the host's copy so the joiner accumulates the updated map.

Distribution model (decided): **peer-to-peer from host.** No central
distribution, no Steam Workshop, no CDN. Whoever hosts defines the map; joiners
receive it over the existing Steam connection and cache it. Maps spread by
playing together.

**Conflict policy (fixed): the host always wins.** This matches the existing
host-authoritative architecture. Joiners never push maps to the host, and there
is no "which version is newer" resolution logic.

## Existing mechanisms this builds on

- **Runtime overlay.** `SaveMapCatalogEntry` writes a map to `resolveMapsDir()`
  (`MAP_CATALOG_DIR` env var, or the default catalog dir) and hot-registers it
  in the `runtimeMaps` overlay without a restart. `GetMapConfigByID` /
  `GetMapCatalogEntryByID` prefer the runtime overlay over the embedded catalog.
  This is the patchable location that lets a host serve an edited map without a
  recompile.
- **Editor save path.** `POST /maps` → `SaveMapCatalogEntryWithOptions` →
  `writeMapEntryToDisk` (canonical JSON via `RenderCatalogEntryJSON`) +
  `runtimeMaps` registration. The joiner's persistence step reuses this path.
- **In-match transfer.** `WelcomeMessage` already carries the full `MapConfig`
  from host to joiner. No new transfer channel is needed for correctness.
- **Lobby metadata.** The host already stamps `map_id` (plus `host_steam_id`,
  `host_persona`, `status`, `local_lobby_id`) into Steam lobby metadata at
  `create_lobby` (`desktop/src-tauri/src/ipc.rs`); the joiner reads it back at
  `join_lobby`. We extend this with the map hash.

## Design

### 1. Map version model (server)

Two identity fields travel with every map:

- **`contentHash`** (canonical, auto-computed): `SHA-256` over the map's
  canonical *authored* JSON — the same stable form `writeMapEntryToDisk`
  produces via `RenderCatalogEntryJSON` — with the hash field itself excluded
  from the hashed bytes. Recomputed on every authoring save so it cannot go
  stale. This is the **match key** checked at join.
- **`version`** (optional, human-readable): a free-text string in the map file
  (e.g. `"v3"`), purely for display in UI/logs. **Never used for matching.**

Both fields are surfaced on:

- `MapCatalogSummary` (the `GET /maps` list),
- `MapConfig` (the in-match `WelcomeMessage`),
- `MapCatalogEntry` (the editor `GET /maps/{id}`).

Hash computation rules:

- **Authoring saves** (editor `POST /maps`, file edits picked up from disk):
  recompute `contentHash` from the new content. Never trust an incoming hash on
  an authoring save.
- **Received-map caching** (joiner persisting a host's map): store the host's
  `contentHash`/`version` verbatim — it is a faithful copy of the authoritative
  source.

### 2. Host runs edited maps without a rebuild (existing mechanism)

No new mechanism. An edited map enters the runtime overlay (`POST /maps` or a
file in `MAP_CATALOG_DIR`), `GetMapConfigByID` prefers it over the embedded
copy, and its `contentHash` recomputes automatically. The host serves the new
version immediately. This is what satisfies "edit → host runs it, no recompile."

### 3. Carry the hash in Steam lobby metadata

Add **`map_hash`** (and optionally `map_version`) to the lobby metadata the host
already stamps. The SPA knows the hash from its catalog fetch and passes it
through `openLobby` → `create_lobby` → `set_lobby_data`. The joiner's
`join_lobby` reads `map_id` + `map_hash` back out alongside the existing fields.
Hashes are small; there is no metadata size concern.

### 4. Lobby preview reconciliation (joiner)

On join, the joiner compares the host's `map_hash` against its local copy of
`map_id`:

- **Match** → render the local preview (fast path, unchanged).
- **Differs or missing** → show the map name plus a **"host's custom map —
  loads at start"** placeholder instead of a wrong or empty preview.

No new transfer machinery runs during the lobby stage.

### 5. In-match transfer (existing) + persistence (new)

In-match correctness needs no change — `WelcomeMessage` already delivers the
host's full map. New behavior on the joiner, in two parts (see
`persistWelcomeMap` in `NetworkClient.ts`):

- **(a) localStorage version cache — the authority for preview matching.**
  Keyed by `${mapId}:${contentHash}` (`mapVersionCache.ts`), it records "the
  joiner has seen this exact version." The lobby/find-game reconciliation in §4
  checks *this cache*, not the server catalog. Because the key is the host's
  exact hash, preview matching **converges** after the first time the joiner
  plays a given version — independent of any server-side hash recomputation.
- **(b) Best-effort catalog persist for re-hosting.** The SPA POSTs the host's
  map to the local catalog (reusing `saveMapCatalogFile`, which writes the
  grouped authored form). It **acquires** a map id new to the joiner and
  **updates** an id the joiner already has when the host's `contentHash`
  differs (the host's newer version) — so a joiner who hosted an older version
  before still picks up the newer one. It skips the POST only when the local
  copy already matches the host's hash. This relies on `contentHash` being
  recomputed on every editor save, so "same id, different hash" reliably means
  "different version." Because the host is authoritative in a shared match,
  adopting its version is the correct convergence. Fire-and-forget; errors are
  swallowed and can never block match entry.

  Trade-off: if the joiner is the map's author and has local saved edits to that
  id that differ from the host's, this overwrites them. Accepted deliberately —
  the feature's purpose is for joiners to converge on the host's version, and
  the author is normally the host, not the joiner.

**Resolved nuance (was an open question in the original design):** because
`WelcomeMessage` carries the *hydrated* form, a re-saved copy's recomputed hash
may not equal the host's. That is why preview matching keys off the localStorage
cache (a), not the catalog. The catalog persist (b) is a convenience for
re-hosting brand-new maps, not the correctness path. host-wins in-match and the
cache (a) fully cover the same-id-but-older case without touching the catalog.

### 6. Edge cases & backward compatibility

- **Joiner lacks the map entirely (brand-new map):** lobby shows the placeholder
  → `WelcomeMessage` delivers it → persisted afterward. Fully covered.
- **Old maps with no `version` field:** `version` defaults to `""`; the hash is
  still computed from content.
- **Old lobbies with no `map_hash` metadata:** treated as "unknown" → placeholder
  fallback. No crash.
- **Non-Steam / local play:** unaffected; the lobby-metadata path is Steam-only.

## Testing

- **Server hashing:** determinism (same content → same hash; whitespace-
  insensitive; any field change → new hash; hash field excluded from its own
  input); hash/version exposed on `MapCatalogSummary`, `MapConfig`, and
  `MapCatalogEntry`.
- **Lobby metadata:** `map_hash` round-trips (host stamps at `create_lobby`,
  joiner reads at `join_lobby`).
- **Reconciliation:** equal hash → local preview; differing/missing → placeholder.
- **Persistence:** welcome with a new hash → cached once, keyed by id+hash;
  existing hash → no-op; cached copy does not appear in the authored map list.
- **Integration:** host edits a map via the runtime overlay and hosts; a joiner
  with a stale or absent copy joins → the in-match map equals the host's, and
  afterward the joiner's catalog contains the host's version.

## Out of scope (YAGNI)

- Central distribution: Steam Workshop, CDN, self-hosted update endpoint.
- Lobby-time pre-fetch of the host's map bytes for an accurate preview.
- Any "which version is newer" ordering logic (host always wins).
- Map propagation beyond directly playing together.

## Affected areas (orientation for planning)

- `server/internal/game/maps.go` — hash computation, overlay save/load, summary.
- `server/pkg/protocol/messages.go` — `contentHash`/`version` on `MapConfig`,
  `MapCatalogSummary`, `MapCatalogEntry`, `WelcomeMessage`.
- `desktop/src-tauri/src/ipc.rs` — stamp/read `map_hash` in lobby metadata.
- `client/.../services/desktopBridge.ts` — carry `map_hash` through
  `openLobby` / `JoinLobbyResult`.
- Client lobby UI — preview reconciliation (placeholder path).
- Client welcome handling + `catalog`/save composables — persist received map.
