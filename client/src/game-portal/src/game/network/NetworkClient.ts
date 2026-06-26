import type {
  AttackCommandMessage,
  CastAbilityCommandMessage,
  CastCommanderAbilityCommandMessage,
  ToggleAutoCastCommandMessage,
  AttackMoveCommandMessage,
  BuildBuildingCommandMessage,
  CancelTrainingCommandMessage,
  ClientMessage,
  ConnectionState,
  DemolishBuildingCommandMessage,
  DepositCommandMessage,
  GatherCommandMessage,
  KickBuildersCommandMessage,
  LeaveMatchMessage,
  LootCollectedNotification,
  MapConfig,
  MapId,
  WelcomeMessage,
  MapContentMessage,
  GuardCommandMessage,
  MatchSnapshotMessage,
  MoveCommandMessage,
  PatrolCommandMessage,
  PickupLootCommandMessage,
  PurchaseUpgradeCommand,
  CancelUpgradeCommand,
  SetBuildingSpawnPointCommandMessage,
  SetFocusTargetCommandMessage,
  SetStanceCommandMessage,
  ServerMessage,
  TrainUnitCommandMessage,
  UpgradeTownHallCommand,
} from './protocol'
import { GameState } from '../core/GameState'
import { ITEM_DEF_MAP } from '../maps/itemDefs'
import { startSteamGame } from '@/services/desktopBridge'
import { getOrCreatePlayerId as getProfilePlayerId } from '@/services/profileApi'
import { hasMapVersion, putMapVersion } from '@/services/mapVersionCache'
import {
  getCachedMap,
  putCachedMap,
  getHashesForMap,
  decompressMapGz,
  withTimeout,
} from '@/services/mapContentCache'
import type { CanvasRenderer } from '../rendering/CanvasRenderer'

/** Derive the WebSocket base URL from the HTTP base URL env var.
 *  http -> ws, https -> wss so both schemes work in prod and dev.
 *  When the env var is empty, fall back to the page origin so the same
 *  build works behind a tunnel/proxy without a hardcoded host. */
function getWsBaseUrl(): string {
  const http = import.meta.env.VITE_API_BASE_URL || window.location.origin
  return http.replace(/^http/, 'ws')
}

/** Session-storage key for the Direct-connect proxy token. Set by the
 *  DirectConnect.vue view after a successful POST /api/direct-connect/join;
 *  consumed by every WebSocket open in this tab so all WS traffic flows
 *  through the joiner-as-proxy path to the remote host's hub. Cleared by
 *  DirectConnect on explicit disconnect or by the user closing the tab. */
const PROXY_TOKEN_STORAGE_KEY = 'webrts.directConnect.proxyToken'

/** Session-storage flag for the Steam Sockets joiner-as-proxy path (§14.3).
 *  Set by SteamMultiplayer.vue when the joiner receives the
 *  `steam_lobby_started` Tauri event from the shell. When set, every WS
 *  open in this tab includes `?proxy=steam` so the joiner's local hub
 *  pulls the parked steamTransport from SteamSessions and pairs it with
 *  the SPA conn. Cleared by leaveStoredMatch. Exported so SteamMultiplayer.vue
 *  can set it without re-declaring the storage key string. */
export const STEAM_PROXY_FLAG_KEY = 'webrts.steam.proxyActive'

/** Session-storage key the host uses to signal "after this welcome arrives,
 *  call startSteamGame with the matchId". Value is the lobbyId. Set by
 *  SteamMultiplayer.vue on the host's "Start Game" click; cleared by
 *  handleMessage('welcome') after firing startSteamGame. */
export const STEAM_PENDING_HOST_START_KEY = 'webrts.steam.pendingHostStart'

/** Session-storage key identifying the paired Steam Matchmaking lobby for
 *  the SPA's current /lobby view (host OR joiner side). Set by CreateGame
 *  (host) or FindGame (joiner) when a Steam lobby is involved. /lobby
 *  reads it to:
 *    - render the Invite Friend button (host-side, Steam-only)
 *    - poll Steam lobby metadata for the joiner's player list / status
 *  Cleared on /lobby leave or when the user lands back at /custom. */
export const STEAM_LOBBY_ID_KEY = 'webrts.steam.lobbyId'

/** Resolve the WS URL fresh on every connect so a Direct-connect session
 *  starting mid-tab is picked up without a page reload (sessionStorage may
 *  have changed since the module loaded). Steam-proxy takes precedence
 *  over direct-connect token if both happen to be set, because the §14.3
 *  flow is the more-specific intent — but in normal use only one is set
 *  at a time. */
function resolveWsUrl(): string {
  let url = `${getWsBaseUrl()}/ws`
  let steamProxy = false
  let token: string | null = null
  try {
    steamProxy = sessionStorage.getItem(STEAM_PROXY_FLAG_KEY) === '1'
    token = sessionStorage.getItem(PROXY_TOKEN_STORAGE_KEY)
  } catch {
    // sessionStorage can throw in some sandboxed contexts; degrade silently.
  }
  if (steamProxy) {
    url += '?proxy=steam'
  } else if (token) {
    url += `?proxy=${encodeURIComponent(token)}`
  }
  return url
}

const MAP_ID_STORAGE_KEY = 'webrts.mapId'
const MATCH_ID_STORAGE_KEY = 'webrts.matchId'

// Backoff schedule: 1s, 2s, 4s, 8s — total ~15s across 4 retries, well within the 30s server grace.
// After 4 attempts without success the state moves to 'failed'.
const BACKOFF_DELAYS_MS = [1000, 2000, 4000, 8000]
const MAX_RECONNECT_ATTEMPTS = BACKOFF_DELAYS_MS.length

// Single source of truth for the player's identity across both the WS match
// path and the HTTP profile path. The HTTP layer (X-Player-ID header)
// requires a UUID, so we delegate to profileApi's UUID-based implementation.
// Historically NetworkClient had its own `player-XXXXXX` generator under a
// separate localStorage key, which meant match progress (DP drops, etc.)
// was credited to a different profile than the one the Profile screen read.
function getOrCreatePlayerId(): string {
  return getProfilePlayerId()
}

function getPreferredMapId(): MapId {
  return localStorage.getItem(MAP_ID_STORAGE_KEY) ?? ''
}

function getStoredMatchId(): string | null {
  return localStorage.getItem(MATCH_ID_STORAGE_KEY)
}

/** §14.3 host fan-out: when the SteamMultiplayer view set the pending
 *  start flag and we just received our matchId via `welcome`, stamp the
 *  matchId into the Steam lobby metadata so joiners can enter. Cleared
 *  once fired regardless of outcome — a retry on reconnect would
 *  double-fire and stamp a fresh matchId, which is not what we want. */
function firePendingSteamHostStartIfNeeded(matchId: string): void {
  let lobbyId: string | null = null
  try {
    lobbyId = sessionStorage.getItem(STEAM_PENDING_HOST_START_KEY)
  } catch {
    return
  }
  if (!lobbyId) return
  try {
    sessionStorage.removeItem(STEAM_PENDING_HOST_START_KEY)
  } catch {
    // Best-effort; even if removal fails we still attempt the call.
  }
  // Fire-and-forget; errors surface as console logs since the host's
  // session is already in the match either way.
  void startSteamGame(lobbyId, matchId).catch((e) => {
    console.error('startSteamGame failed:', e)
  })
}

/** Records the host's map version in the localStorage cache so future lobby
 *  previews can tell whether this client has the host's exact version. Pure
 *  localStorage — no network and no catalog write — so it cannot block, delay,
 *  or interfere with entering the match. Idempotent: a no-op once this exact
 *  (id, hash) has been seen.
 *
 *  NOTE: an earlier version also re-grouped the full welcome map and POSTed it
 *  to the local /maps catalog so a joiner could re-host an acquired map. That
 *  ran heavy synchronous work plus a (for large maps, multi-hundred-KB) catalog
 *  write on the match-entry path and was implicated in both clients freezing on
 *  entry to a large edited map in multiplayer. The catalog re-host path was
 *  removed; reintroduce it only off the hot path (e.g. a deferred idle task or
 *  an explicit "import this map" action), never inline on the welcome. */
function persistWelcomeMap(map: MapConfig): void {
  const { id, name, contentHash, version } = map
  if (!id || !contentHash) return
  if (hasMapVersion(id, contentHash)) return

  putMapVersion({
    id,
    name: name ?? id,
    contentHash,
    version: version ?? '',
    gridCols: map.gridCols,
    gridRows: map.gridRows,
    // WelcomeMessage / MapConfig does not carry spawnPointCount directly;
    // derive from placedUnits or fall back to 0 (preview null-coalesces).
    spawnPointCount: (map.placedUnits ?? []).filter(
      (u) => u.playerSlot !== 'enemy',
    ).length,
  })
}

export class NetworkClient {
  private socket: WebSocket | null = null
  private state: GameState
  /** Injected by GameClient after both are constructed. Used to spawn
   *  world-space floating text for loot pickup events. */
  private renderer: CanvasRenderer | null = null
  private playerId = getOrCreatePlayerId()
  private matchId: string | null = getStoredMatchId()
  private mapId: MapId = getPreferredMapId()
  private activeUpgradeIds: string[] | null = null
  private ownedUpgradeRanks: Record<string, number> = {}
  private acquiredAdvancementIds: string[] = []

  // --- Content-addressed map gate -------------------------------------------
  // While the welcome map is being resolved (decompressed, loaded from cache,
  // or fetched via request_map), non-lifecycle messages — chiefly the join
  // match_snapshot that immediately follows the welcome — are queued so they
  // never apply before the map exists. Drained in arrival order once the map
  // is set. Pings bypass the gate so the heartbeat never stalls.
  private blockedOnMap = false
  private mapMessageQueue: { message: ServerMessage; isReconnect: boolean }[] = []
  /** Set while a request_map is in flight; resolved by the map_content reply. */
  private mapContentResolver: ((mapGz: string) => void) | null = null

  /** Set to false before calling close() for an intentional disconnect so the
   *  reconnect loop does not fire. */
  private shouldReconnect = true

  private reconnectAttempt = 0
  private reconnectTimerId: ReturnType<typeof setTimeout> | null = null

  /** Called whenever the connection state changes. GameClient wires this up. */
  onConnectionStateChange: ((state: ConnectionState) => void) | null = null

  /** Called when a welcome message assigns a matchId. GameClient wires this up. */
  onMatchIdChange: ((id: string) => void) | null = null

  /** Callback that lets GameClient clear the interpolation buffer before the
   *  first fresh snapshot arrives after a successful reconnect. */
  onReconnectSuccess: (() => void) | null = null

  constructor(state: GameState) {
    this.state = state
    this.state.setLocalPlayerId(this.playerId)
  }

  setActiveUpgradeIds(ids: string[] | null) {
    this.activeUpgradeIds = ids
  }

  setOwnedUpgradeRanks(ranks: Record<string, number>) {
    this.ownedUpgradeRanks = ranks
  }

  setAcquiredAdvancementIds(ids: string[]) {
    this.acquiredAdvancementIds = ids
  }

  /** Provide the renderer so loot pickup events can spawn world-space
   *  floating text. Called by GameClient after the renderer is constructed. */
  setRenderer(renderer: CanvasRenderer): void {
    this.renderer = renderer
  }

  setPreferredMapId(mapId: MapId) {
    this.mapId = mapId
    localStorage.setItem(MAP_ID_STORAGE_KEY, mapId)
  }

  // -------------------------------------------------------------------------
  // Public connect / disconnect
  // -------------------------------------------------------------------------

  connect({ resume = true }: { resume?: boolean } = {}) {
    this.shouldReconnect = true
    this.reconnectAttempt = 0
    this.notifyState('connecting')
    return this.openSocket({ resume, isReconnect: false })
  }

  disconnect() {
    this.shouldReconnect = false
    this.clearReconnectTimer()
    this.closeSocket()
  }

  // -------------------------------------------------------------------------
  // Internal socket helpers
  // -------------------------------------------------------------------------

  private openSocket({
    resume,
    isReconnect,
  }: {
    resume: boolean
    isReconnect: boolean
  }): Promise<void> {
    return new Promise<void>((resolve, reject) => {
      const ws = new WebSocket(resolveWsUrl())
      this.socket = ws

      ws.onopen = async () => {
        const hasUpgrades = Object.keys(this.ownedUpgradeRanks).length > 0
        // Omit activeUpgradeIds when null or empty so the server falls back to
        // "all owned upgrades active" — the correct default per schema v3.
        const shouldSendActiveUpgrades =
          this.activeUpgradeIds !== null && this.activeUpgradeIds.length > 0
        // Content-addressed map distribution: tell the server which versions of
        // this map we already hold locally so it can omit the map on a hit.
        // Degrades to [] when IndexedDB is unavailable (treated as a full miss).
        const cachedMapHashes = await getHashesForMap(this.mapId)
        const joinMessage: ClientMessage = {
          type: 'join_match',
          playerId: this.playerId,
          mapId: this.mapId,
          matchId: resume ? (this.matchId ?? undefined) : undefined,
          activeUpgradeIds: shouldSendActiveUpgrades ? this.activeUpgradeIds! : undefined,
          ownedUpgradeRanks: hasUpgrades ? this.ownedUpgradeRanks : undefined,
          acquiredAdvancementIds: this.acquiredAdvancementIds,
          cachedMapHashes: cachedMapHashes.length > 0 ? cachedMapHashes : undefined,
        }
        console.log('[join_match] activeUpgradeIds:', this.activeUpgradeIds, 'ownedUpgradeRanks:', this.ownedUpgradeRanks)
        // The socket may have closed during the await (rare). Guard the send.
        if (this.socket !== ws || ws.readyState !== WebSocket.OPEN) return
        this.send(joinMessage)

        if (!isReconnect) {
          resolve()
        }
      }

      ws.onerror = (err) => {
        if (!isReconnect) {
          reject(err)
        }
        // For reconnect attempts, onerror is followed by onclose — handle there.
      }

      ws.onmessage = (event) => {
        // Capture wire byte size BEFORE parse so the debug HUD (F3) can
        // show last-snapshot bandwidth. event.data is always a string here
        // (server sends text frames); length is UTF-16 chars which is a
        // close-enough proxy for the JSON payload size in bytes for the
        // ASCII-dominated snapshot wire format. The Steam-relayed path
        // gzips upstream but the SPA always reads decompressed JSON, so
        // this measures payload-into-the-renderer regardless of route.
        const rawLength = typeof event.data === 'string' ? event.data.length : 0
        const message = JSON.parse(event.data) as ServerMessage
        if (message.type === 'match_snapshot') {
          this.state.recordSnapshotBytes(rawLength)
        }
        this.routeMessage(message, isReconnect)
      }

      ws.onclose = () => {
        // If the socket we just closed is no longer the active one (e.g. we
        // already opened a replacement), ignore the stale close event.
        if (this.socket !== ws) return

        if (this.shouldReconnect) {
          this.scheduleReconnect()
        }
      }
    })
  }

  private closeSocket() {
    if (this.socket) {
      this.socket.onclose = null // suppress reconnect handler
      this.socket.close()
      this.socket = null
    }
  }

  // -------------------------------------------------------------------------
  // Reconnect logic
  // -------------------------------------------------------------------------

  private scheduleReconnect() {
    if (this.reconnectAttempt >= MAX_RECONNECT_ATTEMPTS) {
      this.notifyState('failed')
      return
    }

    this.notifyState('reconnecting')

    const delay = BACKOFF_DELAYS_MS[this.reconnectAttempt]
    this.reconnectAttempt++

    this.reconnectTimerId = setTimeout(() => {
      this.reconnectTimerId = null
      // Guard: user may have intentionally disconnected while timer was ticking.
      if (!this.shouldReconnect) return

      void this.openSocket({ resume: true, isReconnect: true })
    }, delay)
  }

  /** Called by the UI's "Retry" button after 'failed'. */
  retryReconnect() {
    this.shouldReconnect = true
    this.reconnectAttempt = 0
    this.clearReconnectTimer()
    this.closeSocket()
    this.scheduleReconnect()
  }

  private clearReconnectTimer() {
    if (this.reconnectTimerId !== null) {
      clearTimeout(this.reconnectTimerId)
      this.reconnectTimerId = null
    }
  }

  private notifyState(state: ConnectionState) {
    this.onConnectionStateChange?.(state)
  }

  get currentReconnectAttempt(): number {
    return this.reconnectAttempt
  }

  get maxReconnectAttempts(): number {
    return MAX_RECONNECT_ATTEMPTS
  }

  // -------------------------------------------------------------------------
  // Send helpers
  // -------------------------------------------------------------------------

  send(message: ClientMessage) {
    if (!this.socket || this.socket.readyState !== WebSocket.OPEN) return
    this.socket.send(JSON.stringify(message))
  }

  // -------------------------------------------------------------------------
  // Leave stored match (intentional)
  // -------------------------------------------------------------------------

  async leaveStoredMatch() {
    const matchId = this.matchId ?? getStoredMatchId()
    if (!matchId) return

    this.shouldReconnect = false
    this.clearReconnectTimer()

    const message: LeaveMatchMessage = {
      type: 'leave_match',
      playerId: this.playerId,
      matchId,
    }

    if (this.socket && this.socket.readyState === WebSocket.OPEN) {
      // Send on the live socket so the server removes THIS client from the
      // match — allowing it to see ClientCount == 0 and delete the match.
      this.socket.send(JSON.stringify(message))
      // Give the server one event-loop tick to process before we close.
      await new Promise<void>((resolve) => setTimeout(resolve, 50))
      this.closeSocket()
    } else {
      // No live socket (page reload, tab restore) — open a temp one.
      const socket = new WebSocket(resolveWsUrl())
      await new Promise<void>((resolve, reject) => {
        socket.onopen = () => {
          socket.send(JSON.stringify(message))
          resolve()
        }
        socket.onerror = (err) => { reject(err) }
      })
      socket.close()
    }

    this.matchId = null
    // Clear the Steam-proxy flag so a subsequent fresh SP/local session
    // doesn't accidentally try to ?proxy=steam against a non-existent
    // upstream. The flag is set by SteamMultiplayer.vue when the joiner
    // navigates into a Steam-hosted match; leaving any match ends that
    // intent. Direct-connect token is owned by directConnect.clearProxy.
    try {
      sessionStorage.removeItem(STEAM_PROXY_FLAG_KEY)
    } catch {
      /* sessionStorage may be sandboxed; no-op is correct */
    }
    // localStorage cleared by the caller (Match.vue's exitGame / startNewGame).
  }

  // -------------------------------------------------------------------------
  // Command senders
  // -------------------------------------------------------------------------

  sendMoveCommand(unitIds: number[], x: number, y: number) {
    const message: MoveCommandMessage = {
      type: 'move_command',
      unitIds,
      destination: { x, y },
    }

    this.send(message)
  }

  sendGatherCommand(unitIds: number[], targetId: string) {
    const message: GatherCommandMessage = {
      type: 'gather_command',
      unitIds,
      targetId,
    }

    this.send(message)
  }

  sendPickupLootCommand(unitIds: number[], lootDropId: string) {
    this.send({
      type: 'pickup_loot_command',
      unitIds,
      targetId: lootDropId,
    } satisfies PickupLootCommandMessage)
  }

  sendDepositCommand(unitIds: number[], buildingId: string) {
    const message: DepositCommandMessage = {
      type: 'deposit_command',
      unitIds,
      buildingId,
    }

    this.send(message)
  }

  sendTrainUnitCommand(buildingId: string, unitType: string) {
    const message: TrainUnitCommandMessage = {
      type: 'train_unit_command',
      unitType,
      buildingId,
    }

    this.send(message)
  }

  sendCancelTrainingCommand(buildingId: string, queueIndex = 0) {
    const message: CancelTrainingCommandMessage = {
      type: 'cancel_training_command',
      buildingId,
      ...(queueIndex > 0 ? { queueIndex } : {}),
    }

    this.send(message)
  }

  sendBuildBuildingCommand(unitIds: number[], buildingType: string, gridX: number, gridY: number) {
    const message: BuildBuildingCommandMessage = {
      type: 'build_building_command',
      buildingType,
      unitIds,
      gridX,
      gridY,
    }
    this.send(message)
  }

  sendAttackCommand(unitIds: number[], targetUnitId: number) {
    const message: AttackCommandMessage = {
      type: 'attack_command',
      unitIds,
      targetUnitId,
    }

    this.send(message)
  }

  sendCastAbilityCommand(casterUnitId: number, abilityId: string, targetUnitId: number) {
    const message: CastAbilityCommandMessage = {
      type: 'cast_ability_command',
      casterUnitId,
      abilityId,
      targetUnitId,
    }

    this.send(message)
  }

  sendCastCommanderAbilityCommand(abilityId: string, x: number, y: number) {
    const message: CastCommanderAbilityCommandMessage = {
      type: 'cast_commander_ability',
      abilityId,
      x,
      y,
    }

    this.send(message)
  }

  sendToggleAutocastCommand(unitId: number, abilityId: string) {
    const message: ToggleAutoCastCommandMessage = {
      type: 'toggle_autocast_command',
      unitId,
      abilityId,
    }

    this.send(message)
  }

  // Set or clear the Focus Target for a Cleric/support unit. targetUnitId === 0
  // clears the focus. The server validates ownership of casterUnitId and
  // team-allegiance of targetUnitId; rejection comes back via
  // NotificationMessage (handled by the existing notification toast path).
  sendSetFocusTargetCommand(casterUnitId: number, targetUnitId: number) {
    const message: SetFocusTargetCommandMessage = {
      type: 'set_focus_target_command',
      casterUnitId,
      targetUnitId,
    }

    this.send(message)
  }

  sendAttackMoveCommand(unitIds: number[], x: number, y: number) {
    const message: AttackMoveCommandMessage = {
      type: 'attack_move_command',
      unitIds,
      destination: { x, y },
    }

    this.send(message)
  }

  sendRepairCommand(unitIds: number[], buildingId: string) {
    this.send({ type: 'repair_command', unitIds, buildingId })
  }

  sendKickBuildersCommand(buildingId: string) {
    const message: KickBuildersCommandMessage = { type: 'kick_builders_command', buildingId }
    this.send(message)
  }

  sendDemolishBuildingCommand(buildingId: string) {
    const message: DemolishBuildingCommandMessage = { type: 'demolish_building_command', buildingId }
    this.send(message)
  }

  sendStanceCommand(unitIds: number[], stance: 'hold' | 'idle') {
    const message: SetStanceCommandMessage = {
      type: 'set_stance_command',
      unitIds,
      stance,
    }
    this.send(message)
  }

  sendPatrolCommand(unitIds: number[], x: number, y: number) {
    const message: PatrolCommandMessage = {
      type: 'patrol_command',
      unitIds,
      destination: { x, y },
    }
    this.send(message)
  }

  sendGuardCommand(unitIds: number[]) {
    const message: GuardCommandMessage = {
      type: 'guard_command',
      unitIds,
    }
    this.send(message)
  }

  // Dev-only: spawn a unit with a custom perk loadout at (x, y). Only
  // succeeds when the active map has debug.debugSpawn enabled; the server
  // silently ignores the command on production maps. team defaults to "mine"
  // (caller-owned) server-side if omitted.
  sendDebugSpawnUnitCommand(payload: {
    unitType: string
    team?: 'mine' | 'enemy'
    path?: string
    rank?: string
    perkIds?: string[]
    x: number
    y: number
    customHp?: number
  }) {
    this.send({
      type: 'debug_spawn_unit',
      unitType: payload.unitType,
      team: payload.team,
      path: payload.path,
      rank: payload.rank,
      perkIds: payload.perkIds,
      x: payload.x,
      y: payload.y,
      customHp: payload.customHp,
    })
  }

  sendSetBuildingSpawnPointCommand(buildingId: string, x: number, y: number) {
    const message: SetBuildingSpawnPointCommandMessage = {
      type: 'set_building_spawn_point_command',
      buildingId,
      point: { x, y },
    }

    this.send(message)
  }

  sendPurchaseUpgrade(track: string, buildingId?: string) {
    const message: PurchaseUpgradeCommand = {
      type: 'purchase_upgrade',
      track,
      ...(buildingId ? { buildingId } : {}),
    }
    this.send(message)
  }

  sendCancelUpgrade(buildingId: string, queueIndex?: number) {
    const message: CancelUpgradeCommand = {
      type: 'cancel_upgrade',
      buildingId,
      ...(queueIndex ? { queueIndex } : {}),
    }
    this.send(message)
  }

  sendUpgradeTownHall(buildingId: string) {
    const message: UpgradeTownHallCommand = {
      type: 'upgrade_townhall',
      buildingId,
    }
    this.send(message)
  }

  // -------------------------------------------------------------------------
  // Message handling
  // -------------------------------------------------------------------------

  private handleMessage(message: ServerMessage, isReconnect: boolean) {
    switch (message.type) {
      case 'welcome':
        this.matchId = message.matchId
        this.state.setLocalPlayerId(message.playerId)
        localStorage.setItem(MAP_ID_STORAGE_KEY, message.mapId)
        localStorage.setItem(MATCH_ID_STORAGE_KEY, message.matchId)
        this.onMatchIdChange?.(message.matchId)
        console.log('connected as', message.playerId, 'in', message.matchId)
        // Content-addressed map: resolve the map (decompress the inline mapGz,
        // load it from the local cache, or fetch it via request_map) BEFORE
        // applying snapshots or marking connected. Gate other messages until the
        // map exists. The rest of the welcome flow (host fan-out, connected,
        // version cache) runs in resolveWelcomeMap once the map is ready.
        this.blockedOnMap = true
        void this.resolveWelcomeMap(message, isReconnect)
        break

      case 'match_snapshot':
        this.matchId = message.matchId
        localStorage.setItem(MATCH_ID_STORAGE_KEY, message.matchId)
        this.applySnapshot(message)
        break

      case 'ping':
        this.send({ type: 'pong' })
        break

      case 'notification':
        this.state.addNotification(message.message)
        break

      case 'loot_collected':
        this.handleLootCollected(message as LootCollectedNotification)
        break

      case 'error':
        console.error('server error:', message.message)
        break
    }
  }

  /** First stop for every inbound message. Routes map_content to the pending
   *  request resolver, lets pings through unconditionally (heartbeat must never
   *  stall), queues other messages while the welcome map is resolving, and
   *  otherwise dispatches normally. */
  private routeMessage(message: ServerMessage, isReconnect: boolean) {
    if (message.type === 'map_content') {
      this.handleMapContent(message)
      return
    }
    if (
      this.blockedOnMap &&
      message.type !== 'welcome' &&
      message.type !== 'ping'
    ) {
      this.mapMessageQueue.push({ message, isReconnect })
      return
    }
    this.handleMessage(message, isReconnect)
  }

  /** Resolve the content-addressed map for a welcome, then complete the join.
   *  - mapGz present (miss)  → decompress + cache it
   *  - mapGz absent (hit)    → load from cache; on an eviction-race miss,
   *                            fetch via request_map
   *  Once the map is set, runs the deferred welcome steps and drains any
   *  messages (snapshots) that queued while resolving. */
  private async resolveWelcomeMap(message: WelcomeMessage, isReconnect: boolean) {
    let cfg: MapConfig | null = null
    try {
      // Bound the whole resolution: no cache read, decompress, or fetch may
      // hold the match-entry gate open indefinitely. On timeout we fall through
      // to the failure path (surfaced, not a silent grey screen).
      cfg = await withTimeout(this.loadWelcomeMap(message), 15000, null)
    } catch (e) {
      console.error('failed to resolve welcome map:', e)
      cfg = null
    }

    try {
      if (!cfg) {
        // Could not obtain the map by any path within the budget — surface a
        // failure rather than a silent grey screen.
        this.notifyState('failed')
        return
      }

      this.state.setMapConfig(cfg)

      // §14.3 host-side fan-out: stamp the matchId into the Steam lobby so
      // joiners can enter. Safe to run now that the map is applied.
      firePendingSteamHostStartIfNeeded(message.matchId)

      if (isReconnect) {
        this.state.clearSnapshotBuffer()
        this.onReconnectSuccess?.()
      }

      this.reconnectAttempt = 0
      this.notifyState('connected')

      // Lobby-preview version cache (localStorage, distinct from the IndexedDB
      // body cache). Purely local; runs after connected.
      persistWelcomeMap(cfg)
    } finally {
      // ALWAYS release the gate, even if a step above threw — a stuck gate is
      // exactly the permanent grey screen we're guarding against. Drain queued
      // messages only when we actually have a map; each drained handler is
      // isolated so one bad snapshot can't strand the rest.
      this.blockedOnMap = false
      const queued = this.mapMessageQueue
      this.mapMessageQueue = []
      if (cfg) {
        for (const item of queued) {
          try {
            this.handleMessage(item.message, item.isReconnect)
          } catch (e) {
            console.error('drain handleMessage failed:', e)
          }
        }
      }
    }
  }

  /** Obtain the map for a welcome. Decompresses the inline mapGz (miss) or loads
   *  it from the local cache (hit), falling back to request_map on an eviction
   *  race. Cache WRITES are fire-and-forget: a slow/stalled IndexedDB write must
   *  never block applying the map and entering the match. Returns null if the
   *  map can't be obtained. */
  private async loadWelcomeMap(message: WelcomeMessage): Promise<MapConfig | null> {
    if (message.mapGz) {
      const cfg = await decompressMapGz(message.mapGz)
      void putCachedMap(message.contentHash, message.mapId, cfg)
      return cfg
    }
    const cached = await getCachedMap(message.contentHash)
    if (cached) return cached
    const fetched = await this.requestMap(message.mapId, message.contentHash)
    if (fetched) void putCachedMap(message.contentHash, message.mapId, fetched)
    return fetched
  }

  /** Fetch a map the server expected us to have but we couldn't load (eviction
   *  race). Sends request_map and resolves when the map_content reply arrives,
   *  or null on timeout / decompress failure. */
  private requestMap(mapId: MapId, contentHash: string): Promise<MapConfig | null> {
    return new Promise((resolve) => {
      let settled = false
      const timeoutId = setTimeout(() => {
        if (settled) return
        settled = true
        this.mapContentResolver = null
        console.error('request_map timed out for', mapId, contentHash)
        resolve(null)
      }, 10000)
      this.mapContentResolver = async (mapGz: string) => {
        if (settled) return
        settled = true
        clearTimeout(timeoutId)
        try {
          resolve(await decompressMapGz(mapGz))
        } catch (e) {
          console.error('map_content decompress failed:', e)
          resolve(null)
        }
      }
      this.send({ type: 'request_map', mapId, contentHash })
    })
  }

  /** Hand a map_content payload to the pending requestMap resolver (if any). */
  private handleMapContent(message: MapContentMessage) {
    const resolver = this.mapContentResolver
    this.mapContentResolver = null
    if (resolver) resolver(message.mapGz)
  }

  // Spawn world-space floating text above the collecting unit for each
  // resource and item gained from a loot chest pickup.
  // If the collecting unit can't be found (already dead / FOW edge case),
  // the handler skips silently — no toast, no error.
  private handleLootCollected(notif: LootCollectedNotification) {
    if (!notif.collectingUnitId) return
    const unit = this.state.units.find((u) => u.id === notif.collectingUnitId)
    if (!unit || !this.renderer) return

    const RESOURCE_COLOR = '#86efac' // soft green
    const ITEM_COLOR = '#f5b400'     // amber — matches chest
    const OVERFLOW_COLOR = '#fca5a5' // light red — vault full

    // Stack spacing between simultaneous lines (resources first, then items,
    // then overflow). Each line offsets upward from the baseline.
    const STACK_STEP_PX = 18

    type LineKind = 'resource' | 'item' | 'overflow'
    const lines: Array<{ kind: LineKind; key: string; label: string }> = []

    if (notif.resources) {
      for (const [resourceId, amount] of Object.entries(notif.resources)) {
        lines.push({ kind: 'resource', key: resourceId, label: `+${amount}` })
      }
    }
    if (notif.itemIds) {
      for (const id of notif.itemIds) {
        const def = ITEM_DEF_MAP.get(id)
        lines.push({ kind: 'item', key: id, label: def ? def.displayName : id })
      }
    }
    if (notif.overflowItemIds) {
      for (const id of notif.overflowItemIds) {
        const def = ITEM_DEF_MAP.get(id)
        lines.push({
          kind: 'overflow',
          key: id,
          label: `${def ? def.displayName : id} (vault full)`,
        })
      }
    }

    // Baseline sits a bit above the unit's world-space center; each subsequent
    // line stacks upward so they don't overlap.
    const baselineY = unit.y - 24
    lines.forEach((line, idx) => {
      const y = baselineY - idx * STACK_STEP_PX
      const color =
        line.kind === 'overflow'
          ? OVERFLOW_COLOR
          : line.kind === 'item'
            ? ITEM_COLOR
            : RESOURCE_COLOR
      this.renderer!.spawnLootPickupFloater({
        x: unit.x,
        y,
        kind: line.kind === 'overflow' ? 'item' : line.kind,
        resourceId: line.kind === 'resource' ? line.key : undefined,
        itemId: line.kind !== 'resource' ? line.key : undefined,
        label: line.label,
        color,
      })
    })
  }

  private applySnapshot(message: MatchSnapshotMessage) {
    this.state.applySnapshot(message)
  }
}
