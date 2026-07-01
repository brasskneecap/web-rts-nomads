import { GameLoop } from './GameLoop'
import { GameState } from './GameState'
import { CanvasRenderer } from '../rendering/CanvasRenderer'
import { InputManager } from '../input/InputManager'
import { Camera } from '../rendering/Camera'
import { NetworkClient } from '../network/NetworkClient'
import { playSfx } from '../../composables/useSfx'
import type {
  BattleTrackerSnapshot,
  CommanderAbilitySnapshot,
  ConnectionState,
  LootDropSnapshot,
  MapId,
  PlayerUpgradeSnapshot,
  VaultItemSnapshot,
  WaveSnapshot,
  WaveUpgradeOfferSnapshot,
} from '../network/protocol'
import type { CraftCatalogEntry, DebugSpawnConfig, NetStats, PlayerSummary, SelectionSummary, ShopCatalogEntry, Unit, Notification, ZoneInspectionInfo } from './GameState'
import { BUILDING_DEF_MAP, initBuildingDefs } from '../maps/buildingDefs'
import { initObstacleDefs } from '../maps/obstacleDefs'
import { UNIT_DEF_MAP, initPathBounds, initPathsByUnitType, initUnitDefs } from '../maps/unitDefs'
import { initActionIcons } from '../maps/actionIconDefs'
import { initPerkDefs } from '../maps/perkDefs'
import { initItemDefs } from '../maps/itemDefs'
import { initRecipeDefs } from '../maps/recipeDefs'
import {
  fetchBuildingDefs,
  fetchObstacleDefs,
  fetchUnitDefs,
  fetchActionIcons,
  fetchPerkDefs,
  fetchItemDefs,
  fetchRecipeDefs,
} from '../maps/catalog'

export type GameUiSnapshot = {
  player: PlayerSummary
  selectedUnits: Unit[]
  selection: SelectionSummary
  notifications: Notification[]
  wave: WaveSnapshot
  // Battle tracker (debug). Null when the active map does not opt in via
  // debug.battleTracker. HUD consumers render the panel only when non-null.
  battleTracker: BattleTrackerSnapshot | null
  // Individual debug opt-ins surfaced to the HUD so each debug panel can
  // show/hide itself from config without touching this file. False on any
  // non-debug map.
  debugBattleTracker: boolean
  debugSpawn: boolean
  // True iff the client is currently armed to spawn a unit on the next
  // world click (via DebugSpawnPanel's "Place on Map").
  debugSpawnTargetingActive: boolean
  mapName: string
  mapId: string
  // True when the local player has lost all their townhalls.
  isDefeated: boolean
  // True when all victory objectives have been completed.
  isVictory: boolean
  objectives: import('../network/protocol').ObjectiveSnapshot[]
  /** Zone-capture requirement cards (zones my team occupies but doesn't own).
   *  Empty when none qualify. Drives ZoneCapturePanel. */
  zoneCaptureCards: import('../zones/zoneCaptureCards').ZoneCaptureCard[]
  /** Zone inspection view-model for the currently-selected zone. Null when
   *  no zone is selected. Drives ZoneInspectionPanel. */
  zoneInspection: ZoneInspectionInfo | null
  /** Full per-player snapshot array from the most recent tick. Drives the
   *  end-of-match recap's per-player metrics columns (§15). Empty until
   *  the first snapshot arrives. AI players (enemy/neutral) are filtered
   *  out server-side before being sent. */
  players: import('../network/protocol').PlayerSnapshot[]
  /** Frozen roster captured from the first game-over / victory snapshot.
   *  Null until the match ends. The recap reads this instead of the live
   *  `players` array so host disconnects can't drop rows mid-recap. */
  frozenEndPlayers: import('../network/protocol').PlayerSnapshot[] | null
  /** This viewer's earned dominion points for the match, frozen at the
   *  same moment as frozenEndPlayers. 0 until/unless the server reports it. */
  matchDominionPointsEarned: number
  // Permanent per-player upgrades. Empty array until the server sends upgrade data.
  upgrades: PlayerUpgradeSnapshot[]
  // Current town hall tier for the local player (1/2/3). 0 until first snapshot.
  townHallTier: number
  // buildingType of the currently selected building, or null when nothing (or
  // a non-building entity) is selected. Used to gate overlay panels such as
  // BlacksmithPanel.
  selectedBuildingType: string | null
  // Vault contents for the local player.
  vault: VaultItemSnapshot[]
  vaultSelectedInstanceId: number | null
  // All local-player units (not just selected ones). Needed by VaultPanel to
  // show all units that can receive equipped items.
  allPlayerUnits: Unit[]
  // Wave upgrade offer. Null when no offer is active.
  waveUpgrade: WaveUpgradeOfferSnapshot | null
  // Player-level "commander" abilities for the bottom action bar.
  commanderAbilities: CommanderAbilitySnapshot[]
  // ID of the commander ability whose cast point is currently being picked
  // (a slot was clicked, awaiting world click). Null when no commander
  // targeting is active.
  commanderTargetingAbilityId: string | null
  // Shop catalog for the MatchMenu Shop tab. One entry per item with a
  // RequiredBuilding declared; available=true when the gating building is
  // built and owned by the local player.
  shopCatalog: ShopCatalogEntry[]
  /** Remaining merchant-reroll budget for the local player. Drives the
   *  per-shop refresh button (neutral-shop only) in the Shop menu. */
  shopRerollsRemaining: number
  // Craft catalog for the MatchMenu Craft tab. One entry per known recipe.
  craftCatalog: CraftCatalogEntry[]
  // True when the local player owns at least one Artificer's Table, gating
  // whether the Craft tab's craft actions are usable.
  hasArtificer: boolean
  // Server-side pause flag. When true the client renders a paused overlay
  // and the wave-upgrade modal freezes its visible timer.
  paused: boolean
  // Player ID that initiated the pause. Empty string when not paused.
  pausedBy: string
  // Wall-clock (Date.now()) at which the client first observed paused=true.
  // 0 when not paused. Lets the wave-upgrade modal compute a frozen
  // remaining-time at the pause moment rather than draining.
  pausedSinceMs: number
  // The ground-loot chest the cursor is currently hovering over, or null.
  // Used by the LootDropTooltip to render the chest contents near the cursor.
  hoveredLootDrop: LootDropSnapshot | null
  // Canvas-relative screen position of the cursor. Used to position the
  // loot-drop tooltip near the pointer in DOM space.
  cursorScreenX: number
  cursorScreenY: number
  cursorClientX: number
  cursorClientY: number
  // Network diagnostics for the debug HUD (F3 toggle). Always populated;
  // the HUD component decides whether to render based on its own visible
  // ref. Cheap to read every RAF — getNetStats does only window scans
  // bounded at NET_STATS_WINDOW (40 samples).
  netStats: NetStats
}

export class GameClient {
  private state: GameState
  private renderer: CanvasRenderer
  private input: InputManager
  private loop: GameLoop
  private camera: Camera
  private network: NetworkClient
  private canvas: HTMLCanvasElement
  private hasCenteredCameraOnSpawn = false

  /** Wired by useGameClient to propagate connection state into Vue refs. */
  onConnectionStateChange: ((state: ConnectionState) => void) | null = null

  /** Wired by useGameClient to propagate the current matchId into a Vue ref. */
  onMatchIdChange: ((id: string) => void) | null = null

  constructor(canvas: HTMLCanvasElement, mapId: MapId = '') {
    this.canvas = canvas
    this.state = new GameState()
    this.camera = new Camera()
    this.network = new NetworkClient(this.state)
    this.network.setPreferredMapId(mapId)

    this.network.onConnectionStateChange = (s) => {
      this.onConnectionStateChange?.(s)
    }

    this.network.onMatchIdChange = (id) => {
      this.onMatchIdChange?.(id)
    }

    this.network.onReconnectSuccess = () => {
      // Buffer already cleared inside NetworkClient.handleMessage for welcome.
      // Nothing extra needed here right now, but the hook is available.
    }

    this.renderer = new CanvasRenderer(canvas, this.state, this.camera)
    this.network.setRenderer(this.renderer)
    this.input = new InputManager(canvas, this.state, this, this.camera, this.network)

    this.loop = new GameLoop({
      update: (dt) => {
        this.state.update(dt)
        this.centerCameraOnSpawnIfNeeded()
      },
      render: () => this.renderer.render(),
    })
  }

  async start(options: { resume?: boolean } = {}) {
    const [buildingDefs, obstacleDefs, unitDefs, actionIcons, perkDefs, itemDefs, recipeDefs] = await Promise.all([
      fetchBuildingDefs(),
      fetchObstacleDefs(),
      fetchUnitDefs(),
      fetchActionIcons(),
      fetchPerkDefs(),
      fetchItemDefs().catch(() => []),
      fetchRecipeDefs().catch(() => []),
    ])
    initBuildingDefs(buildingDefs)
    initObstacleDefs(obstacleDefs)
    initUnitDefs(unitDefs.units)
    initPathBounds(unitDefs.paths)
    initPathsByUnitType(unitDefs.pathsByUnit)
    initActionIcons(actionIcons)
    initPerkDefs(perkDefs)
    initItemDefs(itemDefs)
    initRecipeDefs(recipeDefs)
    window.addEventListener('keydown', this.handleDevHotkey)
    await this.network.connect(options)
    this.loop.start()
  }

  // Dev hotkey: F9 re-fetches /catalog/units + /catalog/buildings and reseeds
  // UNIT_DEF_MAP + PATH_BOUNDS_MAP + BUILDING_DEF_MAP without a page reload.
  // Lets unit-bounds and building-def tuning (selection rings, sprite
  // overflow, etc.) iterate at air's rebuild speed instead of a full browser
  // refresh + websocket reconnect.
  private handleDevHotkey = (e: KeyboardEvent) => {
    if (e.key !== 'F9') return
    e.preventDefault()
    void Promise.all([fetchUnitDefs(), fetchBuildingDefs()])
      .then(([{ units, paths, pathsByUnit }, buildingDefs]) => {
        initUnitDefs(units)
        initPathBounds(paths)
        initPathsByUnitType(pathsByUnit)
        initBuildingDefs(buildingDefs)
        console.log('[dev] reloaded unit defs + path bounds + building defs')
      })
      .catch((err) => console.error('[dev] catalog reload failed:', err))
  }

  setActiveUpgradeIds(ids: string[] | null) {
    this.network.setActiveUpgradeIds(ids)
  }

  setOwnedUpgradeRanks(ranks: Record<string, number>) {
    this.network.setOwnedUpgradeRanks(ranks)
  }

  setAcquiredAdvancementIds(ids: string[]) {
    this.network.setAcquiredAdvancementIds(ids)
  }

  setKnownRecipeIds(ids: string[]): void {
    this.network.setKnownRecipeIds(ids)
  }

  async leaveStoredMatch() {
    await this.network.leaveStoredMatch()
  }

  retryReconnect() {
    this.network.retryReconnect()
  }

  /** Anchors the canvas-rendered minimap (and minimap input handlers) to the
   *  given viewport-space DOMRect. Pass null to fall back to the default
   *  top-right corner placement. The rect is converted into canvas-pixel
   *  space here so callers (HUD components) can pass raw DOMRects.
   *
   *  The rect is inset by MINIMAP_FRAME_INSET on each side so the minimap
   *  draws inside the visible interior of the panel rather than being clipped
   *  by the 9-slice frame border that overlays it. */
  setMinimapPanelRect(rect: DOMRect | null) {
    if (!rect || rect.width <= 0 || rect.height <= 0) {
      this.state.minimapPanelRect = null
      return
    }
    const inset = 17
    const canvasRect = this.canvas.getBoundingClientRect()
    this.state.minimapPanelRect = {
      x: rect.left - canvasRect.left + inset,
      y: rect.top - canvasRect.top + inset,
      width: Math.max(0, rect.width - inset * 2),
      height: Math.max(0, rect.height - inset * 2),
    }
  }

  get reconnectAttempt(): number {
    return this.network.currentReconnectAttempt
  }

  get maxReconnectAttempts(): number {
    return this.network.maxReconnectAttempts
  }

  stop() {
    this.loop.stop()
    this.input.destroy()
    this.renderer.destroy()
    this.network.disconnect()
    window.removeEventListener('keydown', this.handleDevHotkey)
  }

  getUiSnapshot(): GameUiSnapshot {
    return {
      player: this.state.getPlayerSummary(),
      selectedUnits: this.state.getSelectedUnits(),
      selection: this.state.getSelectionSummary(),
      notifications: [...this.state.notifications],
      wave: this.state.getWaveSnapshot(),
      battleTracker: this.state.battleTracker,
      debugBattleTracker: this.state.mapConfig.debug?.battleTracker === true,
      debugSpawn: this.state.mapConfig.debug?.debugSpawn === true,
      debugSpawnTargetingActive: this.state.isBuildingTargetingActive('debug-spawn-unit'),
      mapName: this.state.mapConfig.name,
      mapId: this.state.mapConfig.id,
      isDefeated: this.state.isLocalPlayerDefeated(),
      isVictory: this.state.isVictoryAchieved(),
      objectives: this.state.getObjectives(),
      zoneCaptureCards: this.state.getZoneCaptureCards(),
      zoneInspection: this.state.getZoneInspection(),
      players: this.state.playerSnapshots,
      frozenEndPlayers: this.state.frozenEndPlayers,
      matchDominionPointsEarned: this.state.matchDominionPointsEarned,
      upgrades: this.state.playerUpgrades,
      townHallTier: this.state.townHallTier,
      selectedBuildingType: this.state.getSelectedBuildingType(),
      vault: this.state.localPlayerVault,
      vaultSelectedInstanceId: this.state.vaultSelectedInstanceId,
      allPlayerUnits: this.state.getLocalPlayerUnits(),
      waveUpgrade: this.state.waveUpgrade,
      commanderAbilities: this.state.localPlayerCommanderAbilities,
      commanderTargetingAbilityId: this.state.commanderTargetingAbilityId,
      shopCatalog: this.state.getShopCatalogSnapshot(),
      shopRerollsRemaining: this.state.localPlayerShopRerollsRemaining,
      craftCatalog: this.state.getCraftCatalogSnapshot(),
      hasArtificer: this.state.localPlayerHasArtificer(),
      paused: this.state.paused,
      pausedBy: this.state.pausedBy,
      pausedSinceMs: this.state.pausedSinceMs,
      hoveredLootDrop: this.state.hoveredLootDropId
        ? (this.state.lootDropsById.get(this.state.hoveredLootDropId) ?? null)
        : null,
      cursorScreenX: this.state.cursorScreenX,
      cursorScreenY: this.state.cursorScreenY,
      cursorClientX: this.state.cursorClientX,
      cursorClientY: this.state.cursorClientY,
      netStats: this.state.getNetStats(),
    }
  }

  purchaseUpgrade(track: string, buildingId?: string): void {
    this.network.sendPurchaseUpgrade(track, buildingId)
  }

  cancelUpgrade(buildingId: string, queueIndex?: number): void {
    this.network.sendCancelUpgrade(buildingId, queueIndex)
  }

  upgradeTownHall(buildingId: string): void {
    this.network.sendUpgradeTownHall(buildingId)
  }

  sendPurchaseItem(buildingId: string, itemId: string): void {
    this.network.send({ type: 'purchase_item', buildingId, itemId })
  }

  sendRerollShop(buildingId: string): void {
    this.network.send({ type: 'reroll_shop', buildingId })
  }

  sendPurchaseRecipe(buildingId: string, recipeId: string): void {
    this.network.send({ type: 'purchase_recipe', buildingId, recipeId })
  }

  sendCraftItem(recipeId: string): void {
    this.network.send({ type: 'craft_item', recipeId })
  }

  sendEquipItem(unitId: number, slotIndex: number, instanceId: number): void {
    this.network.send({ type: 'equip_item', unitId, slotIndex, instanceId })
  }

  sendUnequipItem(unitId: number, slotIndex: number): void {
    this.network.send({ type: 'unequip_item', unitId, slotIndex })
  }

  sendUseConsumable(unitId: number, slotIndex: number): void {
    this.network.send({ type: 'use_consumable', unitId, slotIndex })
  }

  sendTransferItem(fromUnitId: number, fromSlotIdx: number, toUnitId: number, toSlotIdx: number): void {
    this.network.send({ type: 'transfer_item', fromUnitId, fromSlotIdx, toUnitId, toSlotIdx })
  }

  sendWaveUpgradeChoice(upgradeID: string, targetUnitID?: number): void {
    this.network.send({ type: 'wave_upgrade_choice', upgradeId: upgradeID, targetUnitId: targetUnitID ?? 0 })
  }

  sendWaveUpgradeReroll(): void {
    this.network.send({ type: 'wave_upgrade_reroll' })
  }

  sendSetPause(paused: boolean): void {
    this.network.send({ type: 'set_pause', paused })
  }

  setVaultSelectedInstanceId(instanceId: number | null): void {
    this.state.vaultSelectedInstanceId = instanceId
  }

  // Arms the 'debug-spawn-unit' targeting mode. Exposed for the Debug Spawn
  // panel so it can just call this rather than reaching into GameState.
  beginDebugSpawn(config: DebugSpawnConfig) {
    this.state.beginDebugSpawnTargeting(config)
    this.input.refreshCursor()
  }

  cancelDebugSpawn() {
    this.state.cancelBuildingTargeting()
    this.input.refreshCursor()
  }

  selectUnitOnly(unitId: number) {
    this.state.selectUnit(unitId)
  }

  /** Select a single unit and pan the camera to bring it into view. Used by
   *  the Vault unit cards so clicking a card both selects the unit (showing
   *  the world selection ring) and frames it.
   *
   *  The unit is always kept vertically centered. Horizontally, when the Vault
   *  window's right edge is provided (`menuRightPx`, in viewport CSS px), the
   *  unit is placed 200 screen px to the right of that edge so it stays clear
   *  of the window regardless of screen size. Without it we fall back to a
   *  small fixed left-nudge. */
  focusUnit(unitId: number, menuRightPx?: number) {
    this.state.selectUnit(unitId)
    const units = this.state.getSelectedUnits()
    if (units.length === 0) return
    const u = units[0]
    // Desired on-canvas screen X (in CSS px; canvas.width === clientWidth) at
    // which the unit should land.
    const centerScreenX = this.canvas.width / 2
    let targetScreenX: number
    if (menuRightPx != null) {
      const canvasRect = this.canvas.getBoundingClientRect()
      targetScreenX = menuRightPx - canvasRect.left + 200
    } else {
      // Fallback: nudge slightly right of center, clear of a left-anchored window.
      targetScreenX = centerScreenX + 100
    }
    // centerOn() places the given world point at the screen center, so offset
    // the world point by the screen delta (converted to world units via zoom).
    const offsetWorldX = (targetScreenX - centerScreenX) / this.camera.zoom
    this.camera.centerOn(
      u.x - offsetWorldX,
      u.y,
      this.canvas.width,
      this.canvas.height,
      this.state.mapWidth,
      this.state.mapHeight,
    )
  }

  deselectUnit(unitId: number) {
    this.state.removeUnitFromSelection(unitId)
  }

  /** Bind the current selection to control group N (1..10) — Ctrl+N. */
  assignControlGroup(groupKey: number) {
    this.state.assignControlGroup(groupKey)
  }

  /** Recall control group N (1..10), replacing the current selection — N.
   *  Returns true when a recall actually happened (the slot was populated),
   *  so the input layer can branch on double-tap behavior. */
  selectControlGroup(groupKey: number): boolean {
    return this.state.selectControlGroup(groupKey)
  }

  // finishUnitTargeting pairs the state mutation with an immediate cursor
  // refresh so the targeting reticle reverts to the default arrow on the same
  // frame as the click. Without the explicit refresh, the cursor only
  // updates on the next mousemove event — producing a visible lag between
  // "I clicked to confirm" and "the cursor looks normal again." Every site
  // that ends a unit-targeting cursor mode after a successful pick should
  // go through this helper so the pattern can't accidentally be split.
  private finishUnitTargeting() {
    this.state.cancelUnitTargeting()
    this.input.refreshCursor()
  }

  performSelectionAction(actionId: string) {
    const selectedBuilding = this.state.getSelectedBuilding()

    if (actionId === 'move') {
      this.state.beginUnitTargeting('move')
      this.input.refreshCursor()
      return
    }

    if (actionId === 'gather') {
      this.state.beginUnitTargeting('gather')
      this.input.refreshCursor()
      return
    }

    if (actionId === 'repair') {
      this.state.beginUnitTargeting('repair')
      this.input.refreshCursor()
      return
    }

    if (actionId === 'attack') {
      this.state.beginUnitTargeting('attack')
      this.input.refreshCursor()
      return
    }

    // Ability standard cast (action-bar left-click): enter targeting mode;
    // the next friendly-unit click sends the cast command.
    if (actionId.startsWith('cast-ability-')) {
      this.state.beginAbilityTargeting(actionId.slice('cast-ability-'.length))
      this.input.refreshCursor()
      return
    }

    // Focus Target — left-click enters ally-only targeting cursor; the next
    // valid ally click sends SetFocusTargetCommandMessage. Clicking anywhere
    // invalid in this cursor mode is treated as a clear (see the click
    // handler below).
    if (actionId === 'focus_target') {
      this.state.beginFocusTargetTargeting()
      this.input.refreshCursor()
      return
    }

    // Focus Target right-click (action-bar emits 'autocast-toggle-focus_target')
    // — clears focus on every selected unit that supports it. Mirrors the
    // multi-unit autocast semantics above: one click, deterministic result
    // across the whole group.
    if (actionId === 'autocast-toggle-focus_target') {
      const selectedUnits = this.state.getSelectedUnits()
      for (const u of selectedUnits) {
        this.network.sendSetFocusTargetCommand(u.id, 0)
      }
      this.finishUnitTargeting()
      return
    }

    // Auto-cast toggle (action-bar right-click → emits 'autocast-toggle-' +
    // the ability's action id). No-op for non-ability cells.
    //
    // Group semantics: when multiple selected units share the ability, the
    // right-click derives a single desired state for the whole group rather
    // than flipping each unit independently. If ANY unit currently has the
    // ability's auto-cast OFF, the click enables it everywhere (toggling
    // only the off ones). If ALL units have it ON, the click disables it
    // everywhere. This makes the button behave like a group switch instead
    // of a per-unit toggle that would create split states (some on, some off).
    if (actionId.startsWith('autocast-toggle-')) {
      const rest = actionId.slice('autocast-toggle-'.length)
      if (rest.startsWith('cast-ability-')) {
        const abilityId = rest.slice('cast-ability-'.length)
        const selectedUnits = this.state.getSelectedUnits()
        // Restrict to units that actually own this ability — irrelevant
        // selections (e.g. a Soldier alongside Clerics) are skipped.
        const owners = selectedUnits.filter((u) =>
          (u.abilities ?? []).some((a) => a.id === abilityId),
        )
        if (owners.length === 0) return
        const anyOff = owners.some(
          (u) => !(u.abilities ?? []).find((a) => a.id === abilityId)?.autoCast,
        )
        // Send a toggle to every unit whose current state doesn't match the
        // desired group state. anyOff → desired ON, so flip the off ones.
        // !anyOff (all on) → desired OFF, so flip them all.
        for (const u of owners) {
          const current = (u.abilities ?? []).find((a) => a.id === abilityId)?.autoCast ?? false
          const needsFlip = anyOff ? !current : current
          if (needsFlip) {
            this.network.sendToggleAutocastCommand(u.id, abilityId)
          }
        }
      }
      return
    }

    if (actionId === 'hold') {
      const unitIds = this.state.getOrderedSelectedUnitIds()
      if (unitIds.length > 0) {
        this.network.sendStanceCommand(unitIds, 'hold')
      }
      return
    }

    if (actionId === 'patrol') {
      this.state.beginUnitTargeting('patrol')
      this.input.refreshCursor()
      return
    }

    if (actionId === 'guard') {
      // In-place stance, like Hold — no targeting cursor. Each unit guards the
      // spot it is currently standing on.
      const unitIds = this.state.getOrderedSelectedUnitIds()
      if (unitIds.length > 0) {
        this.network.sendGuardCommand(unitIds)
      }
      return
    }

    if (actionId === 'build') {
      this.state.openWorkerBuildMenu()
      return
    }

    if (actionId === 'close-build-menu') {
      this.state.closeWorkerBuildMenu()
      return
    }

    if (actionId.startsWith('build-') && BUILDING_DEF_MAP.has(actionId.slice(6))) {
      const unitIds = this.state.getOrderedSelectedUnitIds()
      this.state.closeWorkerBuildMenu()
      this.state.beginBuildPlacement(actionId.slice(6), unitIds)
      return
    }

    if (selectedBuilding && actionId.startsWith('train-') && UNIT_DEF_MAP.has(actionId.slice(6))) {
      this.network.sendTrainUnitCommand(selectedBuilding.id, actionId.slice(6))
      return
    }

    if (selectedBuilding && actionId === 'cancel-training') {
      this.network.sendCancelTrainingCommand(selectedBuilding.id)
      return
    }

    if (selectedBuilding && actionId === 'kick-builders') {
      this.network.sendKickBuildersCommand(selectedBuilding.id)
      return
    }

    if (selectedBuilding && actionId === 'demolish-building') {
      this.network.sendDemolishBuildingCommand(selectedBuilding.id)
      return
    }

    // Queue-slot cancel — emitted by SelectionHud when a queued unit is
    // left-clicked. Action id format: "cancel-queue-<index>" where index
    // is the queue position (1..7, since 0 is the leading unit handled by
    // the "X" cancel button above).
    if (selectedBuilding && actionId.startsWith('cancel-queue-')) {
      const index = Number(actionId.slice('cancel-queue-'.length))
      if (Number.isInteger(index) && index > 0) {
        // The same queue strip backs both unit training and blacksmith upgrade
        // research; a blacksmith stamps upgradeInProgress while it works, so
        // route the cancel to the matching command.
        if (selectedBuilding.metadata?.['upgradeInProgress'] === true) {
          this.network.sendCancelUpgrade(selectedBuilding.id, index)
        } else {
          this.network.sendCancelTrainingCommand(selectedBuilding.id, index)
        }
      }
      return
    }

    if (selectedBuilding && actionId === 'set-spawn-point') {
      this.state.beginBuildingTargeting('set-spawn-point')
      return
    }

    if (selectedBuilding && actionId === 'upgrade-townhall') {
      this.network.sendUpgradeTownHall(selectedBuilding.id)
      return
    }

    // Cancel the in-progress upgrade at the selected blacksmith (full refund).
    // Emitted both by the action-bar cancel button and the SelectionHud
    // production card's X for upgrade research.
    if (selectedBuilding && actionId === 'cancel-upgrade') {
      this.network.sendCancelUpgrade(selectedBuilding.id)
      return
    }

    if (selectedBuilding && actionId.startsWith('upgrade-')) {
      const track = actionId.slice('upgrade-'.length)
      if (track && track !== 'townhall') {
        // Research at THIS blacksmith (per-building model).
        this.network.sendPurchaseUpgrade(track, selectedBuilding.id)
        return
      }
    }

    if (actionId.startsWith('buy-item-')) {
      const itemId = actionId.slice('buy-item-'.length)
      if (selectedBuilding) {
        this.sendPurchaseItem(selectedBuilding.id, itemId)
      }
      return
    }

    if (actionId === 'reroll-shop') {
      if (selectedBuilding && selectedBuilding.buildingType === 'neutral-shop') {
        this.sendRerollShop(selectedBuilding.id)
      }
      return
    }

    if (actionId.startsWith('buy-recipe-')) {
      const recipeId = actionId.slice('buy-recipe-'.length)
      if (selectedBuilding) {
        this.sendPurchaseRecipe(selectedBuilding.id, recipeId)
      }
      return
    }

    if (actionId.startsWith('craft-')) {
      const recipeId = actionId.slice('craft-'.length)
      this.sendCraftItem(recipeId)
      return
    }

  }

  /** Arm commander-ability targeting. Called by the bottom action bar when
   *  a slot is clicked. The next world click resolves to a
   *  CastCommanderAbilityCommandMessage at the click point. */
  beginCommanderAbility(abilityId: string) {
    // Block when this ability is still on cooldown — the action bar already
    // greys out the button, but this defends against any other entry path
    // (keyboard shortcut, programmatic call) racing ahead of the snapshot.
    const ability = this.state.localPlayerCommanderAbilities.find((a) => a.id === abilityId)
    if (ability && (ability.cooldownRemaining ?? 0) > 0) return
    this.state.beginCommanderTargeting(abilityId)
    this.input.refreshCursor()
  }

  cancelCommanderAbility() {
    this.state.cancelCommanderTargeting()
    this.input.refreshCursor()
  }

  tryHandleWorldClick(x: number, y: number) {
    // Commander abilities are player-level (no unit selection needed) and
    // resolve at the click point. Handle BEFORE the build/unit-targeting
    // branches so a click during commander targeting commits the cast even
    // when a building is also selected.
    if (this.state.isCommanderTargetingActive()) {
      const abilityId = this.state.commanderTargetingAbilityId
      if (abilityId) {
        this.network.sendCastCommanderAbilityCommand(abilityId, x, y)
      }
      this.state.cancelCommanderTargeting()
      this.input.refreshCursor()
      return true
    }

    if (this.state.isBuildPlacementActive()) {
      this.state.updateBuildPlacement(x, y)
      const placement = this.state.buildPlacement
      if (placement?.valid) {
        if (this.state.buildBlockedByUnownedZone(placement.cursorGridX, placement.cursorGridY, placement.gridW, placement.gridH, placement.buildingType)) {
          // The cell sits in a zone the team doesn't control (and isn't a claim
          // slot) — the server would silently reject the build, so tell the player.
          this.state.addNotification('Zone not controlled')
        } else {
          this.network.sendBuildBuildingCommand(placement.builderUnitIds, placement.buildingType, placement.cursorGridX, placement.cursorGridY)
          playSfx('building_placement.mp3')
          this.state.cancelBuildPlacement()
        }
      } else {
        this.state.addNotification('Cannot place building here')
      }
      return true
    }

    // Debug spawn: fire a debug_spawn_unit command with the pending loadout.
    // Mode stays active so the user can drop multiple copies in a row; right-
    // click cancels via the existing cancelTargeting() path.
    if (this.state.isBuildingTargetingActive('debug-spawn-unit') && this.state.debugSpawnConfig) {
      const cfg = this.state.debugSpawnConfig
      this.network.sendDebugSpawnUnitCommand({
        unitType: cfg.unitType,
        team: cfg.team,
        path: cfg.path,
        rank: cfg.rank,
        perkIds: cfg.perkIds,
        customHp: cfg.customHp,
        x,
        y,
      })
      return true
    }

    const selectedBuilding = this.state.getSelectedBuilding()
    if (!selectedBuilding || !this.state.isBuildingTargetingActive('set-spawn-point')) {
      const unitIds = this.state.getOrderedSelectedUnitIds()

      if (this.state.isUnitTargetingActive('move') && unitIds.length > 0) {
        this.state.addFormationMoveMarkers(x, y)
        this.network.sendMoveCommand(unitIds, x, y)
        this.finishUnitTargeting()
        return true
      }

      if (this.state.isUnitTargetingActive('gather') && unitIds.length > 0) {
        const clickedBuilding = this.state.getBuildingAtPosition(x, y, 16)

        if (
          clickedBuilding &&
          clickedBuilding.capabilities.includes('resource-source') &&
          this.state.selectedUnitsCanGather()
        ) {
          const cellSize = this.state.mapConfig.cellSize
          const buildingCenterX = (clickedBuilding.x + clickedBuilding.width / 2) * cellSize
          const buildingCenterY = (clickedBuilding.y + clickedBuilding.height / 2) * cellSize
          this.state.addMoveMarker(buildingCenterX, buildingCenterY, 700)
          this.network.sendGatherCommand(unitIds, clickedBuilding.id)
        } else {
          const clickedObstacle = this.state.getGatherableObstacleAtPosition(x, y, 16)
          if (clickedObstacle && clickedObstacle.id && this.state.selectedUnitsCanGather()) {
            const cellSize = this.state.mapConfig.cellSize
            const obstacleCenterX = (clickedObstacle.x + (clickedObstacle.width ?? 1) / 2) * cellSize
            const obstacleCenterY = (clickedObstacle.y + (clickedObstacle.height ?? 1) / 2) * cellSize
            this.state.addMoveMarker(obstacleCenterX, obstacleCenterY, 700)
            this.network.sendGatherCommand(unitIds, clickedObstacle.id)
          }
        }

        this.finishUnitTargeting()
        return true
      }

      if (this.state.isUnitTargetingActive('cast-ability') && unitIds.length > 0) {
        // The selected unit is the caster; the click resolves the target
        // (own/visible unit — covers self & allies). The server validates
        // ownership, targeting rules, range, and mana.
        const target = this.state.getUnitAtPosition(x, y)
        const abilityId = this.state.castAbilityId
        if (target && abilityId) {
          this.network.sendCastAbilityCommand(unitIds[0], abilityId, target.id)
        }
        this.finishUnitTargeting()
        return true
      }

      // Focus-target targeting: a click on a same-team ally sets focus for
      // every selected unit (so a group of Clerics can be aimed at one
      // protectee with one click). A click on anything else — enemy, ground,
      // a building — clears focus on the group, matching the auto-cast UX
      // where re-entering targeting and failing to pick a valid target
      // deactivates the toggle.
      if (this.state.isUnitTargetingActive('focus-target') && unitIds.length > 0) {
        const clickedUnit = this.state.getUnitAtPosition(x, y)
        const localPlayerId = this.state.localPlayerId
        const isAlly = !!clickedUnit && !!localPlayerId && clickedUnit.ownerId === localPlayerId
        const targetId = isAlly ? clickedUnit!.id : 0
        for (const uid of unitIds) {
          // Skip self-as-target: a Cleric cannot focus on itself (no support
          // value, and the server would just reject it anyway). Sending the
          // clear case (targetId === 0) is always safe.
          if (targetId !== 0 && targetId === uid) continue
          this.network.sendSetFocusTargetCommand(uid, targetId)
        }
        this.finishUnitTargeting()
        return true
      }

      if (this.state.isUnitTargetingActive('attack') && unitIds.length > 0) {
        const clickedUnit = this.state.getEnemyUnitAtPosition(x, y)

        if (clickedUnit) {
          this.network.sendAttackCommand(unitIds, clickedUnit.id)
        } else {
          this.state.addFormationMoveMarkers(x, y)
          this.network.sendAttackMoveCommand(unitIds, x, y)
        }

        this.finishUnitTargeting()
        return true
      }

      if (this.state.isUnitTargetingActive('patrol') && unitIds.length > 0) {
        this.state.addFormationMoveMarkers(x, y)
        this.network.sendPatrolCommand(unitIds, x, y)
        this.finishUnitTargeting()
        return true
      }

      if (this.state.isUnitTargetingActive('repair') && unitIds.length > 0) {
        const clickedBuilding = this.state.getBuildingAtPosition(x, y, 16)

        if (
          clickedBuilding &&
          clickedBuilding.ownerId === this.state.localPlayerId &&
          clickedBuilding.metadata?.['underConstruction'] === true
        ) {
          const cellSize = this.state.mapConfig.cellSize
          const buildingCenterX = (clickedBuilding.x + clickedBuilding.width / 2) * cellSize
          const buildingCenterY = (clickedBuilding.y + clickedBuilding.height / 2) * cellSize
          this.state.addMoveMarker(buildingCenterX, buildingCenterY, 700)
          this.network.sendRepairCommand(unitIds, clickedBuilding.id)
        }

        this.finishUnitTargeting()
        return true
      }

      return false
    }

    const spawnPoint = this.state.getTargetedBuildingSpawnPoint(x, y)
    if (!spawnPoint) return false

    this.network.sendSetBuildingSpawnPointCommand(selectedBuilding.id, spawnPoint.x, spawnPoint.y)
    this.state.addMoveMarker(spawnPoint.x, spawnPoint.y, 800)
    this.state.cancelBuildingTargeting()
    return true
  }

  cancelTargeting() {
    this.state.cancelBuildingTargeting()
    this.state.cancelUnitTargeting()
    this.state.cancelCommanderTargeting()
    this.state.cancelBuildPlacement()
    // Same rationale as finishUnitTargeting: refresh the cursor right now
    // (right-click / Escape cancel path) so the reticle reverts on the same
    // frame as the cancel action, not on the next mousemove.
    this.input.refreshCursor()
  }

  private centerCameraOnSpawnIfNeeded() {
    if (this.hasCenteredCameraOnSpawn) return

    const spawnCenter = this.state.getLocalPlayerSpawnCenter()
    if (!spawnCenter) return

    this.camera.centerOn(
      spawnCenter.x,
      spawnCenter.y,
      this.canvas.width,
      this.canvas.height,
      this.state.mapWidth,
      this.state.mapHeight,
    )

    this.hasCenteredCameraOnSpawn = true
  }
}
