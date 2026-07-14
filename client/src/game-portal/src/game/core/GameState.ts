import type {
  ActiveEffectIcon,
  BannerSnapshot,
  BattleTrackerSnapshot,
  BeamSnapshot,
  BuildingTile,
  CommanderAbilitySnapshot,
  EffectSnapshot,
  GameOverSnapshot,
  InventorySnapshot,
  ItemSnapshot,
  LootDropSnapshot,
  ObjectiveSnapshot,
  PlayerUpgradeSnapshot,
  UnitCostOverride,
  VaultItemSnapshot,
  VictorySnapshot,
  MapConfig,
  MatchSnapshotMessage,
  NeutralCampSnapshot,
  ObstacleMetadataPatch,
  ObstacleTile,
  PerkCooldownSnapshot,
  AbilitySnapshot,
  PlayerSnapshot,
  ProjectileSnapshot,
  ResourceType,
  ShieldPoolSnapshot,
  TrapSnapshot,
  UnitCapability,
  UnitOrder,
  UnitType,
  WaveSnapshot,
  WaveUpgradeOfferSnapshot,
  Zone,
  ZoneSnapshot,
} from '../network/protocol'
import { ENEMY_PLAYER_ID, NEUTRAL_PLAYER_ID, UNIT_ORDER_LABELS, ZONE_AURA_TYPE_STAT_MODIFIER, ZONE_TEAM_OWNER, formatUpgradeStatDelta } from '../network/protocol'
import { buildZoneCaptureCards, type ZoneCaptureCard } from '../zones/zoneCaptureCards'
import { buildZoneCellIndex, cellKey } from '../maps/zoneGeometry'
import { createEditorMapConfig, sanitizeMapConfig } from '../maps/mapConfig'
import { getShopPOIs, type ShopPOI } from '../rendering/minimapLayers'
import { BUILDABLE_BUILDING_DEFS, BUILDING_DEF_MAP, getUpgradeChain, townHallTierName } from '../maps/buildingDefs'
import { UNIT_DEF_MAP } from '../maps/unitDefs'
import { playSfx } from '../../composables/useSfx'
import { PERK_DEF_MAP } from '../maps/perkDefs'
import { ITEM_DEF_MAP } from '../maps/itemDefs'
import { RECIPE_DEF_MAP } from '../maps/recipeDefs'
import { buildItemTooltipBody } from '../items/itemRules'
import { formatPerkTooltip } from './perkTooltip'
import { hasItemAsset } from '../rendering/itemAssets'
import { getUnitBodyRect, isPointInUnitBody } from '../rendering/unitSprites'
import { isTerrainCellBlocked } from '../rendering/terrainTileset'
import { projectileRendersAsArrow } from '../rendering/projectileSprites'
import { FogOfWar } from './FogOfWar'

/**
 * Live-compounded trap stats for archer/trapper units, reflecting the full
 * Bronze+Silver stack. Sent by the server on unit snapshots; used by the
 * tooltip formatter to display concrete numbers for trap perks.
 * Mirrors the EffectiveTrapSnapshot struct on the server.
 */
export type EffectiveTrapSnapshot = {
  perkId: string
  durationSeconds?: number
  radius?: number
  triggerRadius?: number
  placeInterval?: number
  damagePerSecond?: number
  burstDamage?: number
  slowMultiplier?: number
  markMultiplier?: number
  markDuration?: number
  barbedFieldRampPerSec?: number
  barbedFieldMaxBonusDPS?: number
  exposedWeakenedMultiplier?: number
  lastingFlamesBurnDuration?: number
  aftershockDelaySeconds?: number
}

export type Unit = {
  id: number
  unitType: UnitType
  archetype?: string
  name: string
  capabilities: UnitCapability[]
  /** Airborne unit — renderer should use the walking animation when otherwise idle. Mirrors UnitSnapshot.flyer. */
  flyer?: boolean
  visible: boolean
  status?: string
  x: number
  y: number
  hp?: number
  maxHp?: number
  damage?: number
  attackSpeed?: number
  /** Remaining seconds on a PHYSICAL/generic slow (traps, concussive perks).
   *  Not visualized today. Mirrors UnitSnapshot.slowedRemaining. */
  slowedRemaining?: number
  /** Effective speed fraction while physically slowed (0.7 = 30% slower). */
  slowedMultiplier?: number
  /** Remaining seconds on a COLD (chill) slow. > 0 ⇒ the renderer paints an icy
   *  overlay on the unit. Mirrors UnitSnapshot.coldSlowedRemaining. */
  coldSlowedRemaining?: number
  /** Effective speed fraction while chilled (0.75 = 25% slower). */
  coldSlowedMultiplier?: number
  /** Remaining seconds on a burn (fire DoT) — fire_sword proc or Trapper
   *  fire_pit. > 0 ⇒ the renderer paints an animated burning overlay.
   *  Mirrors UnitSnapshot.burningRemaining. */
  burningRemaining?: number
  /** Accumulated Arcane Charge (Arch Mage arcane_missiles passive). The
   *  renderer floats one rotating purple orb per 10 charge. Mirrors
   *  UnitSnapshot.arcaneCharge. */
  arcaneCharge?: number
  /** Where the burning overlay anchors on the unit ("feet" | "center" | "head").
   *  Mirrors UnitSnapshot.burningAnchor; absent ⇒ "feet". */
  burningAnchor?: string
  /** Effective attack range in world pixels. Reflects perk-driven range
   *  multipliers (eagle_spirit, bullseye); absent for melee units. */
  attackRange?: number
  moveSpeed?: number
  armor?: number
  /** Effective crit probability against an unmarked target (0..1). 0 / absent
   *  for units with no crit sources. Hunter's Mark contribution is target-
   *  dependent and not folded into this snapshot value. */
  critChance?: number
  /** Damage multiplier on a successful crit. 0 / absent when the unit has
   *  no crit sources at all. */
  critMultiplier?: number
  /** Passive HP regeneration rate in HP per second. Absent when 0. */
  healthRegen?: number
  xp?: number
  rank?: string
  xpToNextRank?: number
  xpIntoCurrentRank?: number
  recentRankUpSeconds?: number
  path?: string
  perkIds?: string[]
  /** Advancement-granted extra perk slot counts, keyed by tier. Mirrors
   *  server `UnitSnapshot.ExtraPerkSlots`. Populated only when the unit's
   *  owner has a `unitExtraPerkSlot` advancement (e.g. Twin Bronze) for
   *  this unit type. Value is the count of EXTRA slots at that tier. */
  extraPerkSlots?: Record<string, number>
  shield?: number
  maxShield?: number
  /** Per-source shield pool breakdown — one entry per active source-specific
   *  shield pool (e.g. dark_renewal). Absent when the unit has no source-
   *  specific pools. The aggregate shield/maxShield above always reflect the
   *  totals including these pools plus any legacy single-pool shields. */
  shieldPools?: ShieldPoolSnapshot[]
  /** Current mana for spellcaster units (acolyte). Absent for non-casters. */
  mana?: number
  /** Max mana pool. Absent/0 for units with no mana. Drives the mana bar. */
  maxMana?: number
  /** Base passive mana regen in mana/second. Absent when 0. */
  manaRegen?: number
  /** Buffs currently active — each entry carries a perk id + optional stacks. */
  activeBuffs?: ActiveEffectIcon[]
  /** Debuffs currently active — each entry carries a raw icon id + optional stacks. */
  activeDebuffs?: ActiveEffectIcon[]
  /** Per-perk cooldown timers (only perks currently gated by a ticking timer). */
  perkCooldowns?: PerkCooldownSnapshot[]
  abilities?: AbilitySnapshot[]
  ownerId?: string
  color?: string
  carriedResourceType?: ResourceType
  carriedAmount?: number
  targetX?: number
  targetY?: number
  moving?: boolean
  /** Server-authoritative unit→target delta while attacking. Mirrors
   *  UnitSnapshot.actionFacingDx/Dy. Absent when the unit isn't firing. */
  actionFacingDx?: number
  actionFacingDy?: number
  workTargetId?: string
  /**
   * Live-compounded trap stats for archer/trapper units. Only set when the
   * unit is a trapper archetype that owns at least one trap bronze perk.
   * Absent for all other unit types.
   */
  effectiveTrap?: EffectiveTrapSnapshot
  /** Current order — mirrors UnitSnapshot.order. Absent on old-server snapshots; treat as 'idle'. */
  order?: string
  /** Focus Target — ID of the ally this unit (typically a Cleric) is focused
   *  on. Mirrors UnitSnapshot.focusTargetId. 0/absent means no focus. Drives
   *  the Focus Target button highlight, selection-HUD focus indicator, and
   *  the world-space focus marker drawn under the focused ally. */
  focusTargetId?: number
  /** Inventory the unit is carrying. Absent for units that don't have an
   *  inventory capability. See ItemDef / ITEM_DEF_MAP for item resolution. */
  inventory?: Inventory
  /** Inclusive frame range the client one-way loops through on the casting
   *  sprite sheet while this unit is channeling a beam ability. Mirrors
   *  UnitSnapshot.channelLoopStart / channelLoopEnd — both set when status
   *  is 'Channeling', absent otherwise. start == end → single held frame;
   *  start < end → small loop at the unit's normal frame cadence. Out-of-
   *  range values modulo into the sheet at draw time. */
  channelLoopStart?: number
  channelLoopEnd?: number
}

/** A held item — carries the item id and optional stack count. Look up
 *  display name, icon, modifiers, and effects via ITEM_DEF_MAP[item.itemId]. */
export type Item = ItemSnapshot

/** Unit inventory — `size` slots are unlocked; `slots` is positionally indexed
 *  (null = unlocked-but-empty). The UI may display additional locked slots
 *  beyond `size` to communicate progression headroom. */
export type Inventory = InventorySnapshot

export type ActionCost = {
  resourceId: string
  amount: number
  accent: string
}

export type ActionItem = {
  id: string
  label: string
  /** Explicit hotkey letter shown in the tooltip (e.g. "T"). When absent the
   *  tooltip falls back to parsing a "(X)" marker out of the label. Set for
   *  build-menu buildings so every building shows its hotkey with a clean
   *  (paren-free) label. */
  hotkey?: string
  /** Resource costs shown on the action button (e.g. train actions). */
  cost?: ActionCost[]
  /**
   * 'perk' marks a display-only perk slot in the bottom row of the action grid.
   * 'ability' marks an interactive ability button (left-click cast,
   * right-click toggles auto-cast). Absent means a regular action button.
   */
  kind?: 'perk' | 'ability'
  /** Ability auto-cast currently enabled — drives the action-cell glow. */
  autoCast?: boolean
  /** Ability supports auto-cast (right-click toggles it). */
  supportsAutoCast?: boolean
  /** True while this ability is actively channeling on the unit. Drives the
   *  pulsing green border on the action button in SelectionHud. */
  channeling?: boolean
  /** Rank tier for perk slots — drives the rank-colored border in SelectionHud. */
  perkRank?: 'bronze' | 'silver' | 'gold'
  /** Tooltip header shown on hover for perk slots. */
  tooltipTitle?: string
  /** Tooltip body shown on hover for perk slots. */
  tooltipBody?: string
  disabled?: boolean
  active?: boolean
  /** Override the icon lookup key used by ActionIcon. When set, ActionIcon uses
   *  this key for the PNG/SVG lookup instead of `id`. Useful when the action id
   *  is a compound slug (e.g. "buy-item-weapon_common_sword") but the visual
   *  should reuse an existing icon (e.g. "attack"). */
  iconId?: string
  iconDef?:
    | { kind: 'building'; type: string }
    | { kind: 'unit'; type: string }
    | { kind: 'item'; type: string }
    | { kind: 'ability'; type: string; projectile?: string; iconKey?: string }
  /** Seconds remaining on this perk's next activation. 0/undefined = ready. */
  cooldownRemaining?: number
  /** Full cooldown duration corresponding to cooldownRemaining. Drives the
   *  clock-wipe fraction (remaining / total). */
  cooldownTotal?: number
  /** Remaining purchasable stock for shop buy actions. Rendered as a
   *  bottom-right corner number on the action button (same visual as the
   *  Match Menu shop cards' badge), NOT as tooltip text. Absent for
   *  non-shop actions and sold-out slots. */
  stockCount?: number
  /** Accumulated-charge readout (e.g. "12/30") drawn OVER the icon for a
   *  charge-fire passive (Arcane Missiles). Updates live from the snapshot so
   *  the player watches it build toward the auto-fire. Absent otherwise. */
  chargeText?: string
}

export type DetailItem = {
  id: string
  label: string
  value?: string
  tooltip?: string
  tooltipTitle?: string
  tooltipBody?: string
  // SVG path (24×24 viewBox, stroke-style). When set, SelectionHud renders this
  // entry as an icon+value row instead of inline "Label: Value" text.
  icon?: string
}

// Stat icons used by unit detail rows. Stroke-style paths on a 24×24 viewBox,
// matching the existing action-icons.json conventions.
// World-units radius around a trap's center that counts as a "click hit".
// Matches the unit click radius (14) so the feel is consistent — and small
// enough that large trap zones like explosive_trap (80 radius) don't swallow
// clicks intended for ground orders or units inside the zone.
const TRAP_CENTER_HIT_RADIUS = 14

/**
 * Picks the initial snapshot-interpolation buffer depth based on the current
 * network path. The Steam Sockets joiner sees relay jitter (snapshot arrival
 * intervals drift in the 40-80ms range over a target 50ms cadence); a tight
 * 100ms buffer regularly underruns and the player perceives stutter. A
 * 200ms buffer absorbs that jitter at the cost of 100ms more input-feel
 * latency, which is acceptable for an RTS over Steam.
 *
 * LAN / loopback / Direct-connect host / single-player keep the original
 * 100ms because their snapshot arrival is consistent.
 */
function detectInitialInterpolationDelayMs(): number {
  let delay = 100
  try {
    if (typeof window === 'undefined') return delay
    if (window.sessionStorage.getItem('webrts.steam.proxyActive') === '1') {
      delay = 200
    }
  } catch {
    // sessionStorage may be unavailable in some sandboxed contexts.
  }
  console.log('[GameState] interpolationDelayMs initialised to', delay)
  return delay
}

// Matches the accent colors used by ResourceStock in the HUD resource tray.
// Add new resource types here as they are introduced on the server.
const RESOURCE_ACCENT: Record<string, string> = {
  gold: '#d4a84f',
  wood: '#7a9a52',
  food: '#c96e43',
}

const STAT_ICON_HEART = 'M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z'
// Lightning bolt — used for the mana row on spellcaster units.
const STAT_ICON_BOLT = 'M13 2L4 14L11 14L9 22L20 10L13 10Z'
const STAT_ICON_SWORD = 'M14.5 17.5 L3 6 L3 3 L6 3 L17.5 14.5 M20 12 L12 20.5 M16.5 17.5 L20.5 21.5 L21.5 20.5 L17.5 16.5'
const STAT_ICON_BOOT = 'M6 3v11 M6 13h9v5 M3 18h18 M6 7h2 M6 10h2'
const STAT_ICON_SHIELD = 'M12 2L4 5v6c0 5.5 3.5 10 8 11 4.5-1 8-5.5 8-11V5z'
// Shield-within-shield — distinct from the armor shield so a unit carrying
// both armor and an active overshield reads as two different concepts in the
// stat grid. The inner shield reads as "extra protection layer" at a glance.
const STAT_ICON_BARRIER =
  'M12 2L4 5v6c0 5.5 3.5 10 8 11 4.5-1 8-5.5 8-11V5z M12 6L8 7.5v3.5c0 3.5 2 6 4 6.5 2-.5 4-3 4-6.5V7.5z'
// Crit (target/burst) — used for the Marksman crit row.
const STAT_ICON_CRIT = 'M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20z M12 7a5 5 0 1 0 0 10 5 5 0 0 0 0-10z M12 11a1 1 0 1 0 0 2 1 1 0 0 0 0-2z M2 12h3 M19 12h3 M12 2v3 M12 19v3'

export type ProductionSummary = {
  unitType: string
  remainingSeconds: number
  totalSeconds: number
  queueLength: number
  queuedUnitTypes: string[]
  progress: number
  timeLabel: string
  // Whether the player can cancel this production (omitted/true = cancelable).
  cancelable?: boolean
  // Action id emitted by the SelectionHud cancel (X) button. Defaults to
  // 'cancel-training' for unit production; blacksmith upgrades use
  // 'cancel-upgrade' so the cancel routes to a refund instead of a queue pop.
  cancelActionId?: string
}

export type RepairSummary = {
  progress: number
  timeLabel: string
  builderCount: number
}

export type SelectionSummary =
  | {
      kind: 'none'
      title: string
      subtitle: string
      details: DetailItem[]
      actions: ActionItem[]
      production?: undefined
    }
  | {
      kind: 'unit'
      title: string
      subtitle: string
      details: DetailItem[]
      actions: ActionItem[]
      production?: undefined
      /** Promotion-path label shown under the unit name (e.g. "Vanguard"). */
      pathLabel?: string
      /** Rank tier label shown in the primary panel (e.g. "Bronze"). */
      rankLabel?: string
      /** XP progress string shown in the primary panel (e.g. "120 / 250 XP"). */
      xpLabel?: string
    }
  | {
      kind: 'building'
      title: string
      subtitle: string
      details: DetailItem[]
      actions: ActionItem[]
      production?: ProductionSummary
      construction?: RepairSummary
    }
  | {
      kind: 'group'
      title: string
      subtitle: string
      details: DetailItem[]
      actions: ActionItem[]
      production?: undefined
    }

export type InterpolationFrame = {
  tick: number
  serverNow: number
  receivedAt: number
  units: Unit[]
}

export type SelectionBox = {
  startX: number
  startY: number
  currentX: number
  currentY: number
  active: boolean
}

// Network diagnostics surfaced by the debug HUD (F3). Updated every time
// applySnapshot consumes a server snapshot. snapshotAgeMs is the wall-time
// delta between server-stamped `serverNow` and the client's Date.now() at
// receive; for a steady ~5s buffered lag it will read ~5000ms, far above
// any reasonable clock skew. receiveGapMs is the gap between consecutive
// snapshot arrivals — server broadcasts at 20 Hz so the healthy steady
// state is ~50ms; spikes/freezes appear here. bufferDepth is the
// interpolation-buffer depth at receive time.
export type NetStats = {
  snapshotAgeMs: number
  snapshotAgeAvgMs: number
  snapshotAgeMaxMs: number
  receiveGapMs: number
  receiveGapMaxMs: number
  snapshotsPerSec: number
  bufferDepth: number
  lastSnapshotBytes: number
  totalSnapshots: number
  transportLabel: 'steam-proxy' | 'direct'
}

export type MoveMarker = {
  id: number
  x: number
  y: number
  createdAt: number
  durationMs: number
}

// Client-derived damage event: emitted by applySnapshot whenever a unit's
// HP drops between consecutive snapshots. The server does not send discrete
// damage events — we diff HP ourselves and drain this queue each render in
// CanvasRenderer to spawn floating damage numbers.
// Client-derived resource deposit event: emitted by applySnapshot whenever a
// worker's carriedAmount drops to 0 (it just dumped its load at a townhall).
// Drained each render in CanvasRenderer to spawn floating resource numbers.
export type ResourceDepositEvent = {
  unitId: number
  x: number
  y: number
  resourceId: ResourceType
  amount: number
  /** 1.0 = full credit, < 1 = reduced gain (future: capacity-cap, over-cap
   *  storage). Drives the float-text color: green = full, yellow/red = lossy. */
  capacityFraction: number
  createdAt: number
}

export type DamageEvent = {
  unitId: number
  unitType: string
  x: number
  y: number
  amount: number
  // True when the victim is owned by the local player (→ red number).
  // False when the victim is an enemy or neutral unit (→ white number).
  isFriendly: boolean
  createdAt: number
  /**
   * Visual flavour of the floating number:
   *   - 'normal'   : default white / red (default).
   *   - 'combined' : yellow combined number rendered when a Marksman's
   *                  Double Shot pair lands; sum of both arrows' damage on
   *                  the same target.
   *   - 'crit'     : critical hit — renderer draws a red circle behind the
   *                  number. Set by matching server CritEventSnapshot
   *                  entries against HP-diff damage events.
   *   - 'minor'    : ancillary splash damage (Reactive Flames, Electrified
   *                  Caltrops, etc.) — renderer draws smaller in a color
   *                  picked from `minorVariant`. The portion is peeled off
   *                  the HP-diff using server MinorDamageEventSnapshot
   *                  entries so a victim hit by trap-DoT + Infusion shows
   *                  two distinct numbers.
   *   - 'heal'     : intentional healing (heal ability). Renderer draws a
   *                  light-green "+N" floating up. Sourced from server
   *                  HealEventSnapshot, not from HP-diff (HP going up is
   *                  not tracked as a damage event).
   *   - 'evade'    : an incoming hit was fully dodged or blocked (no damage
   *                  landed). Renderer draws "Dodged!"/"Blocked!" text
   *                  instead of a number. Sourced from server
   *                  EvadeEventSnapshot, spawned directly like 'heal' —
   *                  there is no HP-diff to derive it from.
   */
  kind?: 'normal' | 'combined' | 'crit' | 'minor' | 'heal' | 'manaRestore' | 'evade'
  /**
   * Sub-flavour for kind='evade': selects the popup text ("Blocked!" vs
   * "Dodged!") and color. Mirrors server EvadeEventSnapshot.kind.
   */
  evadeKind?: 'dodge' | 'block'
  /**
   * Sub-flavour for kind='minor', mirroring MinorDamageEventSnapshot.variant.
   * "fire" → orange, "electric" → purple, omitted defaults to fire/orange.
   */
  minorVariant?: string
  /**
   * Sub-flavour for kind='normal' / 'crit' — mirrors
   * DamageTypeHintSnapshot.variant. When set, the renderer colors the major
   * (floating-up) popup with the same palette used by minorVariant ("shadow"
   * → dark purple, "fire" → orange, "holy" → gold, "electric" → light
   * purple). Absent ⇒ default white/red.
   */
  damageType?: string
  /**
   * Per-hit split metadata. When a unit takes multiple simultaneous hits in
   * one snapshot (two soldier strikes, two frostbolts) the HP-diff popup is
   * split into one event per hit, using the server's hitDamageEvents. Each
   * split event carries its index and the total count so the renderer can
   * fan the numbers out horizontally; createdAt is also staggered so they
   * pop in sequence. Absent / count<=1 ⇒ ordinary single popup.
   */
  spreadIndex?: number
  spreadCount?: number
}

export type Vec2 = {
  x: number
  y: number
}

export type BuildingTargetingMode = 'set-spawn-point' | 'debug-spawn-unit'

// Loadout carried while the "debug-spawn-unit" targeting mode is active.
// Populated by BeginDebugSpawnTargeting from the DebugSpawnPanel's current
// selection; consumed by tryHandleWorldClick which sends the loadout +
// click coords as a debug_spawn_unit command.
export type DebugSpawnConfig = {
  unitType: string
  // Ownership: "mine" (default) spawns the unit on the caller's team so it
  // accepts commands and contributes to army count; "enemy" spawns as a
  // hostile NPC-owned unit for testing combat matchups.
  team?: 'mine' | 'enemy'
  path?: string
  rank?: string
  perkIds?: string[]
  customHp?: number
}
export type UnitTargetingMode = 'move' | 'gather' | 'repair' | 'attack' | 'patrol' | 'cast-ability' | 'focus-target'

export type BuildPlacement = {
  buildingType: string
  gridW: number
  gridH: number
  cursorGridX: number
  cursorGridY: number
  valid: boolean
  builderUnitIds: number[]
}

export type PlayerSummary = {
  playerId: string | null
  color: string | null
  totalUnits: number
  selectedUnits: number
  totalHp: number
  resources: ResourceStock[]
}

export type ResourceStock = {
  id: string
  label: string
  amount: number
  max?: number
  accent: string
}

export type Notification = {
  id: number
  message: string
  remaining: number
}

/** View-model for the Zone Inspection panel. Produced by
 *  getZoneInspection() and carried in GameUiSnapshot.zoneInspection.
 *  The component uses formatModifier (from statRegistry) to render each
 *  aura's bonus — no formatting is done here. */
export type ZoneInspectionInfo = {
  zoneId: string
  name: string
  /** Display label for the controlling player or team. "Unclaimed" when
   *  no owner. "Team" when the ZONE_TEAM_OWNER sentinel is set. */
  ownerLabel: string
  /** The controlling player's colour hex (from ZoneSnapshot.ownerColor or
   *  playerColors). Null when unowned / neutral. */
  ownerColor: string | null
  /** Static aura definitions from the map config. Only stat_modifier auras
   *  are shown; unknown types are silently filtered client-side. */
  auras: import('../network/protocol').ZoneAura[]
}

export type ShopCatalogEntry = {
  /** Discriminates item slots from recipe slots on the Shop tab. Absent =
   *  'item' (the overwhelming majority; left unset so item entries are
   *  unchanged). Recipe Shops emit 'recipe' entries that buy *knowledge*,
   *  dispatched via purchase_recipe rather than purchase_item. */
  entryType?: 'item' | 'recipe'
  /** For item entries this is the item id; for recipe entries it is the
   *  recipe id (used for keying + as the purchase argument). */
  itemId: string
  displayName: string
  description?: string
  iconKey: string
  /** Item category. Not meaningful for recipe entries (filled with a
   *  placeholder); the Shop tab keys recipe rendering off entryType instead. */
  kind: 'equipment' | 'consumable'
  tier: 'common' | 'uncommon' | 'rare' | 'epic' | 'legendary'
  costGold: number
  /** Remaining stock at the purchase building. 0 = sold out (entry stays
   *  visible but the buy action is disabled / greyed-out). */
  quantity: number
  /** ID of the shop building this item will be purchased from. Always
   *  set; only available items are emitted, so there is no "unavailable"
   *  state and no lock indicator. */
  purchaseBuildingId: string
  /** Building type of the shop (e.g. 'marketplace', 'neutral-shop') — drives
   *  the shop-card icon. */
  purchaseBuildingType: string
  /** Per-instance sprite style (metadata "shopStyle"), used to pick the
   *  neutral-shop merchant art on the shop card. Undefined = default sprite. */
  purchaseBuildingStyle?: string
  /** Human-readable shop name shown on the shop card — the assigned item
   *  list's name when set, else the building type label. */
  purchaseBuildingName: string
  /** Recipe entries only: true when the local player already knows this
   *  recipe. The slot stays visible but is greyed-out and non-purchasable
   *  ("Recipe already known"), mirroring the sold-out treatment. */
  recipeKnown?: boolean
}

export interface CraftCatalogIngredient {
  itemId: string
  have: number
  need: number
}
export interface CraftCatalogEntry {
  recipeId: string
  name: string
  output: string
  costGold: number
  ingredients: CraftCatalogIngredient[]
  craftable: boolean
}

export class GameState {
  private resourceStocks: ResourceStock[] = [
    { id: 'gold', label: 'Gold', amount: 500, accent: '#d4a84f' },
    { id: 'wood', label: 'Wood', amount: 180, accent: '#7a9a52' },
    { id: 'food', label: 'Food', amount: 0, max: 0, accent: '#c96e43' },
  ]
  private playerColors = new Map<string, string>()
  // ownerId → TeamID, mirrored from PlayerSnapshot each tick. Drives the
  // alliance predicates so the client matches the server chokepoint.
  private playerTeams = new Map<string, number>()
  /** Full per-player snapshot array from the most recent tick. Retained so
   *  the end-of-match recap (§15 of campaign-objectives-and-metrics) can
   *  render comparison columns for every player in the lobby. Empty until
   *  the first snapshot arrives. */
  playerSnapshots: PlayerSnapshot[] = []
  private nextNotificationId = 0
  notifications: Notification[] = []

  units: Unit[] = []
  banners: BannerSnapshot[] = []
  traps: TrapSnapshot[] = []
  projectiles: ProjectileSnapshot[] = []
  beams: BeamSnapshot[] = []
  effects: EffectSnapshot[] = []
  // Battle tracker snapshot (debug). Null when the active map does not have
  // debug.battleTracker enabled. Consumed by BattleTrackerPanel.vue.
  battleTracker: BattleTrackerSnapshot | null = null

  // Permanent per-player upgrade state. Populated from the local player's
  // PlayerSnapshot every tick. Empty until the server sends upgrade data.
  playerUpgrades: PlayerUpgradeSnapshot[] = []
  // Unit types the local player cannot currently train (RequiresBuildings
  // unsatisfied). Populated from the local player's PlayerSnapshot every
  // tick. Empty until the server says otherwise.
  lockedUnitTypes: string[] = []
  // Current tier of the local player's town hall (1 = Town Hall, 2 = Keep,
  // 3 = Castle). 0 until the server sends the first snapshot with this data.
  townHallTier: number = 0

  // Wave upgrade offer — populated from MatchSnapshotMessage each tick. Null
  // when no offer is active (between waves or before the first wave).
  waveUpgrade: WaveUpgradeOfferSnapshot | null = null

  // Live per-tick state for each neutral camp, keyed by camp placement id.
  // Populated from MatchSnapshotMessage.neutralCamps each tick. The
  // minimap uses currentTier for dot color and aliveUnitCount to hide
  // dots for cleared / wave-hidden camps. Static placement data
  // (position, group, scaling) still comes from MapConfig.neutralSpawns.
  neutralCampSnapshotsById: Map<string, NeutralCampSnapshot> = new Map()

  // Live ground-loot chests, keyed by id. Populated from
  // MatchSnapshotMessage.lootDrops each tick. Used by the world render
  // layer (chest sprites), the minimap POI layer, hover tooltip, and the
  // right-click input dispatch.
  lootDropsById: Map<string, LootDropSnapshot> = new Map()

  // Per-tick zone ownership snapshots. Parallel to MapConfig.zones by id.
  // Absent (empty) on maps that have no zones.
  zoneSnapshotsById: Map<string, ZoneSnapshot> = new Map()

  // Server-side pause state. paused=true freezes the visible wave-upgrade
  // timer and triggers the in-match paused overlay. pausedBy is the player
  // ID that initiated the pause (empty when not paused).
  paused = false
  pausedBy = ''
  // Wall-clock at which the local client first observed paused=true. Reset
  // to 0 on resume. Used by the wave-upgrade modal to freeze its visible
  // timer at the moment the pause arrived rather than letting Date.now()
  // continue to drain it.
  pausedSinceMs = 0

  // Vault state — populated from PlayerSnapshot each tick. The vault is
  // unbounded; there is no capacity to track.
  localPlayerVault: VaultItemSnapshot[] = []

  // Remaining merchant-reroll budget. Mirrored from PlayerSnapshot each tick.
  // Drives whether the reroll action on a neutral-shop building is enabled.
  localPlayerShopRerollsRemaining = 0

  // Recipes the local player has unlocked (purchased from a Recipe Shop).
  // Mirrored from PlayerSnapshot each tick. Drives craft-* actions at the Artificer.
  localPlayerUnlockedRecipeIds: string[] = []

  // Effective per-unit training costs for the local player, keyed by unit
  // type. Populated from PlayerSnapshot.unitCostOverrides every tick — only
  // unit types whose cost the player's advancements changed appear here. The
  // build menu overlays these on the catalog cost so the displayed price
  // matches what the server charges. Empty = every unit uses catalog cost.
  localPlayerUnitCostOverrides: Map<string, UnitCostOverride> = new Map()

  // Commander abilities (player-level action bar). Populated from
  // PlayerSnapshot.commanderAbilities every tick.
  localPlayerCommanderAbilities: CommanderAbilitySnapshot[] = []
  // Active commander targeting — the ability whose cast point is being
  // picked. Null when no commander targeting is active. Separate from
  // unitTargetingMode because the cast is player-level, not unit-level,
  // and must work with no units selected.
  commanderTargetingAbilityId: string | null = null
  // Active consumable-item targeting — set when the player clicks an item in
  // the ItemsBar; the next world click uses the item as a ground AoE there.
  // Null when no item targeting is active.
  itemTargeting: { instanceId: number; itemId: string; radius: number } | null = null
  // Currently selected vault item (for click-to-equip flow). Set by
  // VaultPanel; cleared when the user deselects or closes the panel.
  vaultSelectedInstanceId: number | null = null

  fow: FogOfWar = new FogOfWar()

  snapshotBuffer: InterpolationFrame[] = []

  // -------------------------------------------------------------------------
  // Network diagnostics (debug HUD — toggle with F3)
  // -------------------------------------------------------------------------
  // Ring of recent snapshot ages (ms) for max-over-window computation.
  // 40 entries = 2s at the server's 20 Hz broadcast cadence.
  private readonly NET_STATS_WINDOW = 40
  private netAgeRing: number[] = []
  private netGapRing: number[] = []
  private netReceiveTimesRing: number[] = []
  private netLastReceivedAt = 0
  private netLastSnapshotBytes = 0
  private netTotalSnapshots = 0
  // EWMA of snapshot age (ms). Alpha=0.2 → ~10-sample effective window.
  private netAgeEwmaMs = 0
  // Interpolation buffer depth. The renderer plays back snapshots
  // delayed by this much so it can smoothly interpolate between them
  // even when arrival times jitter. Tuned per network path:
  //   - 100ms: LAN / loopback / Direct connect (consistent ~50ms gaps)
  //   - 200ms: Steam Sockets joiner (relay jitter regularly pushes
  //     individual snapshots 40-80ms late; a 100ms buffer underruns
  //     and the joiner sees stutter). Initialised below by the
  //     `?proxy=steam` detection. Increase if you still see stutter
  //     on poor relay routes; decrease to trade smoothness for less
  //     input-feel latency.
  interpolationDelayMs = detectInitialInterpolationDelayMs()
  maxBufferedSnapshots = 20

  localPlayerId: string | null = null

  // Current wave state, mirrored from the server snapshot every tick.
  waveSnapshot: WaveSnapshot = {
    enabled: false,
    currentWave: 0,
    totalWaves: 0,
    state: '',
    timer: 0,
    waveDuration: 0,
  }

  // Game over state — non-null once any player has lost all townhalls.
  gameOverSnapshot: GameOverSnapshot | null = null
  // Victory state — non-null once the designated boss unit has been killed.
  victorySnapshot: VictorySnapshot | null = null
  // Frozen end-of-match roster. Captured from the FIRST snapshot that reports
  // game-over (or victory) so connection teardown — e.g. the host leaving and
  // dropping out of the roster — can't clobber the recap's data source. Null
  // until the match ends. The recap reads this, not the live playerSnapshots.
  frozenEndPlayers: PlayerSnapshot[] | null = null
  // This viewer's own earned dominion points for the match, taken from the
  // game-over snapshot at freeze time. 0 until/unless the server reports it.
  matchDominionPointsEarned = 0
  private endRosterFrozen = false

  mapWidth = 6144
  mapHeight = 4096
  mapConfig: MapConfig = createEditorMapConfig(96, 64, {
    id: 'loading-map',
    name: 'Loading Map',
    description: '',
  })
  // Neutral shop minimap POIs, captured once from the full authored map in
  // setMapConfig. Snapshot merges must NOT touch this: per-tick building
  // lists are FOW-filtered and simply omit unscouted shops, but the minimap
  // shows shop markers regardless of scouting (same as neutral-camp dots).
  neutralShopPOIs: ShopPOI[] = []

  selectedUnitIds = new Set<number>()
  selectedUnitOrder: number[] = []
  // RTS-style control groups. Keyed 1..10 (0 maps to 10 by convention).
  // Each entry is the unit IDs assigned via Ctrl+N at assignment time;
  // recall via N replaces the current selection with those ids that are
  // still alive and locally owned. Stale ids (dead/transferred units) are
  // silently dropped on recall via setSelection's ownership filter.
  controlGroups = new Map<number, number[]>()
  selectedBuildingId: string | null = null
  selectedObstacleId: string | null = null
  selectedTrapId: string | null = null
  /** The zone id selected by a canvas left-click that hit no unit/building/trap.
   *  Cleared whenever any other selection is made or the selection is cleared. */
  selectedZoneId: string | null = null
  inspectedEnemyUnitId: number | null = null
  // Read-only inspection of an allied (other-real-player) unit. Mirrors
  // inspectedEnemyUnitId — the player can view perks/stats but cannot issue
  // any orders. Mutually exclusive with selectedUnitIds and the other
  // inspected-* / selected-* fields, same lifecycle pattern.
  inspectedAllyUnitId: number | null = null
  hoveredEnemyUnitId: number | null = null
  // Set by InputManager while the player is in a friendly-targeting cursor
  // mode (cast-ability, focus-target). The renderer draws a blue dashed ring
  // around this unit so the player can see which ally their click would
  // commit to. Null whenever no friendly-targeting mode is active OR the
  // cursor is not over a valid friendly. Same lifecycle as hoveredEnemyUnitId.
  hoveredFriendlyUnitId: number | null = null
  // Last known cursor position in world space (set by InputManager every
  // mousemove). Lets renderer code that needs to anchor a preview at the
  // cursor (commander ability AoE indicator, etc.) read it directly without
  // each call site doing its own screen→world conversion.
  cursorWorldX = 0
  cursorWorldY = 0
  // Last known cursor position in canvas-relative screen space. Set by
  // InputManager alongside cursorWorldX/Y.
  cursorScreenX = 0
  cursorScreenY = 0
  // Last known cursor position in VIEWPORT-relative coords (event.clientX/Y).
  // Used directly by DOM-overlay tooltips (LootDropTooltip) with
  // position:fixed so they don't depend on the canvas's bounding rect.
  cursorClientX = 0
  cursorClientY = 0
  hoveredInteractableBuildingId: string | null = null
  hoveredInteractableObstacleId: string | null = null
  // Non-null while the cursor hovers a ground-loot chest. The chest tooltip
  // reads this to show the chest's pre-rolled contents. Cleared on mouse-leave
  // and on every frame where the cursor is not over any chest.
  hoveredLootDropId: string | null = null
  buildingTargetingMode: BuildingTargetingMode | null = null
  unitTargetingMode: UnitTargetingMode | null = null
  // Ability whose cast target is being picked. Only meaningful while
  // unitTargetingMode === 'cast-ability'; a stale value is inert because
  // every reader gates on that mode first.
  castAbilityId: string | null = null
  // Loadout carried with the 'debug-spawn-unit' buildingTargetingMode. Null
  // whenever debug-spawn targeting is inactive. Persisted across clicks so
  // the user can drop multiple test subjects with the same configuration
  // in rapid succession until they right-click to cancel.
  debugSpawnConfig: DebugSpawnConfig | null = null
  workerBuildMenuOpen = false
  buildPlacement: BuildPlacement | null = null

  selectionBox: SelectionBox = {
    startX: 0,
    startY: 0,
    currentX: 0,
    currentY: 0,
    active: false,
  }

  moveMarkers: MoveMarker[] = []
  private nextMoveMarkerId = 1

  // Screen-space rect of the HUD minimap panel, written by SelectionHud each
  // time its layout changes. When set, the canvas-rendered minimap and the
  // minimap input handlers anchor to this rect instead of the default
  // top-right fallback. Coordinates are CSS pixels (== canvas pixels, since
  // CanvasRenderer.resize sets canvas.width = clientWidth).
  minimapPanelRect: { x: number; y: number; width: number; height: number } | null = null

  // Drained each render by CanvasRenderer to spawn floating damage numbers.
  // Populated by applySnapshot from HP deltas between snapshots.
  damageEvents: DamageEvent[] = []
  // Per-unit previous snapshot state used by the HP-diff and "unit
  // disappeared = killing blow" detectors. Storing position + ownerId +
  // unitType lets us still render a damage number for the killing blow,
  // since the dead unit is removed from s.Units server-side before the
  // snapshot reaches the client.
  private prevUnitHp = new Map<
    number,
    { hp: number; x: number; y: number; unitType: string; ownerId?: string }
  >()
  // Tracks projectiles seen in the previous snapshot so we can detect which
  // ones have just landed (present last tick, gone this tick). Used by the
  // Marksman Double Shot combined-damage emitter to know when to sum.
  private prevProjectiles = new Map<string, ProjectileSnapshot>()
  // Per-target rolling damage history (last ~500ms) for the combined yellow
  // Double Shot number. Populated alongside the regular damageEvents push;
  // pruned each tick. Keyed by unit id.
  private recentDamageByUnit = new Map<number, Array<{ amount: number; at: number }>>()
  private readonly RECENT_DAMAGE_WINDOW_MS = 500

  // Rate cap for melee attack (swing/stab) SFX. In a sustained melee a swing
  // starts almost every tick, which would machine-gun the same sound; here we
  // allow at most MELEE_SOUND_MAX_PER_WINDOW plays of a given attackType within
  // MELEE_SOUND_WINDOW_MS and drop the rest. That conveys a busy fight without
  // the loudness scaling with unit count. recentMeleeSoundPlays holds, per
  // attackType, the performance.now() timestamps of recent plays, pruned to the
  // window on each use (keys are a tiny fixed set — "swing", "stab", ... — so it
  // never grows unbounded). Tune the two constants to taste.
  private readonly MELEE_SOUND_WINDOW_MS = 300
  private readonly MELEE_SOUND_MAX_PER_WINDOW = 3
  private recentMeleeSoundPlays = new Map<string, number[]>()

  // Current camera view in world coords, pushed every render frame by the
  // renderer (which owns the camera; applySnapshot runs on the network thread
  // and has no camera of its own). Positional combat SFX — arrow shots, melee
  // swings — are suppressed when their world position falls outside this rect,
  // so fighting off-screen doesn't add to the mix. Null until the first frame
  // sets it; treated as "in view" so we never go silent before the camera is
  // known.
  private currentViewBounds:
    | { left: number; top: number; right: number; bottom: number }
    | null = null

  // Called by the renderer each frame with the camera's visible world rect.
  setViewBounds(bounds: {
    left: number
    top: number
    right: number
    bottom: number
  }): void {
    this.currentViewBounds = bounds
  }

  // True when a world point is inside the current camera view (or the view is
  // not yet known). Gates positional combat SFX in applySnapshot.
  private isWorldPointInView(x: number, y: number): boolean {
    const b = this.currentViewBounds
    if (!b) return true
    return x >= b.left && x <= b.right && y >= b.top && y <= b.bottom
  }

  // Time gap between successive floating numbers when one snapshot's HP loss is
  // split into per-hit popups (see emitDamageEvent / hitDamageEvents). Each
  // split hit's popup startedAt is pushed this many ms into the future so they
  // pop in sequence — combined with the horizontal fan-out in the renderer,
  // two simultaneous hits read as two distinct numbers instead of one sum.
  private static readonly HIT_SPLIT_STAGGER_MS = 70

  // Drained each render by CanvasRenderer to spawn floating resource numbers.
  // Populated by applySnapshot from carriedAmount deltas (drop to 0 = deposit).
  resourceDepositEvents: ResourceDepositEvent[] = []
  private prevUnitCarried = new Map<number, { resourceType: ResourceType; amount: number }>()

  setLocalPlayerId(playerId: string) {
    this.localPlayerId = playerId
  }

  clearSnapshotBuffer() {
    this.snapshotBuffer = []
    // Reset net-stats rings too — a stale gap-ms value carried across a
    // reconnect would show a misleading "5s freeze" on the HUD right
    // after the resume.
    this.netAgeRing = []
    this.netGapRing = []
    this.netReceiveTimesRing = []
    this.netLastReceivedAt = 0
    this.netAgeEwmaMs = 0
  }

  // Called by NetworkClient.onmessage with the raw wire byte length BEFORE
  // applySnapshot runs, so the size lines up with the snapshot the HUD is
  // about to read. Kept as its own setter (rather than threading bytes
  // through applySnapshot's signature) so the snapshot pipeline stays
  // ignorant of transport-level details.
  recordSnapshotBytes(bytes: number) {
    this.netLastSnapshotBytes = bytes
  }

  // Returns a snapshot of network diagnostics for the debug HUD. Cheap —
  // called once per RAF from GameClient.getUiSnapshot().
  getNetStats(): NetStats {
    // Compute snapshots-per-second over the last second by counting
    // receive timestamps that fall inside the 1s window. The ring is
    // maintained in applySnapshot below.
    const now = performance.now()
    const cutoff = now - 1000
    let perSec = 0
    for (let i = this.netReceiveTimesRing.length - 1; i >= 0; i--) {
      if (this.netReceiveTimesRing[i] >= cutoff) perSec++
      else break
    }

    let ageMax = 0
    for (const v of this.netAgeRing) if (v > ageMax) ageMax = v
    let gapMax = 0
    for (const v of this.netGapRing) if (v > gapMax) gapMax = v

    const lastAge = this.netAgeRing.length ? this.netAgeRing[this.netAgeRing.length - 1] : 0
    const lastGap = this.netGapRing.length ? this.netGapRing[this.netGapRing.length - 1] : 0

    let transportLabel: 'steam-proxy' | 'direct' = 'direct'
    try {
      if (typeof window !== 'undefined' && window.sessionStorage.getItem('webrts.steam.proxyActive') === '1') {
        transportLabel = 'steam-proxy'
      }
    } catch {
      // sessionStorage may be unavailable in sandboxed contexts.
    }

    return {
      snapshotAgeMs: lastAge,
      snapshotAgeAvgMs: this.netAgeEwmaMs,
      snapshotAgeMaxMs: ageMax,
      receiveGapMs: lastGap,
      receiveGapMaxMs: gapMax,
      snapshotsPerSec: perSec,
      bufferDepth: this.snapshotBuffer.length,
      lastSnapshotBytes: this.netLastSnapshotBytes,
      totalSnapshots: this.netTotalSnapshots,
      transportLabel,
    }
  }

  // Records one snapshot's diagnostic samples. Called from applySnapshot
  // with the values it already has on hand — no extra time reads.
  private recordNetSample(serverNow: number, receivedAt: number) {
    // Snapshot age: wall-clock delta between server stamp and client
    // receive. Date.now() is wall-clock to match server's UnixMilli();
    // includes any clock skew between machines, but a steady ~5s value
    // is far above any plausible skew so the signal is unambiguous.
    const ageMs = Date.now() - serverNow

    // Receive gap: monotonic delta between consecutive snapshot arrivals.
    // First snapshot of a session has no predecessor — record 0.
    const gapMs = this.netLastReceivedAt > 0 ? receivedAt - this.netLastReceivedAt : 0
    this.netLastReceivedAt = receivedAt

    // Push to rings, trim to window.
    this.netAgeRing.push(ageMs)
    if (this.netAgeRing.length > this.NET_STATS_WINDOW) this.netAgeRing.shift()
    this.netGapRing.push(gapMs)
    if (this.netGapRing.length > this.NET_STATS_WINDOW) this.netGapRing.shift()
    this.netReceiveTimesRing.push(receivedAt)
    if (this.netReceiveTimesRing.length > this.NET_STATS_WINDOW) this.netReceiveTimesRing.shift()

    // EWMA of age — alpha=0.2 gives ~10-sample effective window, smooth
    // enough for the live readout without lagging real shifts.
    if (this.netTotalSnapshots === 0) {
      this.netAgeEwmaMs = ageMs
    } else {
      this.netAgeEwmaMs = 0.2 * ageMs + 0.8 * this.netAgeEwmaMs
    }
    this.netTotalSnapshots++
  }

  update(dt: number) {
    const now = performance.now()
    this.moveMarkers = this.moveMarkers.filter(
      (marker) => now - marker.createdAt < marker.durationMs,
    )
    this.notifications = this.notifications
      .map((n) => ({ ...n, remaining: n.remaining - dt }))
      .filter((n) => n.remaining > 0)
  }

  addNotification(message: string) {
    this.notifications.push({ id: this.nextNotificationId++, message, remaining: 2.5 })
  }

  addMoveMarker(x: number, y: number, durationMs = 550) {
    this.moveMarkers.push({
      id: this.nextMoveMarkerId++,
      x,
      y,
      createdAt: performance.now(),
      durationMs,
    })
  }

  addMoveMarkers(points: Vec2[], durationMs = 550) {
    const createdAt = performance.now()

    for (const point of points) {
      this.moveMarkers.push({
        id: this.nextMoveMarkerId++,
        x: point.x,
        y: point.y,
        createdAt,
        durationMs,
      })
    }
  }

  applySnapshot(message: MatchSnapshotMessage) {
    const now = performance.now()

    this.mergeSnapshotBuildings(message.buildings)
    this.applyObstacleDeltas(message.obstaclesRemoved, message.obstacleMetadata)

    const frame: InterpolationFrame = {
      tick: message.tick,
      serverNow: message.serverNow,
      receivedAt: now,
      units: message.units.map((unit) => ({
        id: unit.id,
        unitType: unit.unitType,
        archetype: unit.archetype,
        name: unit.name,
        capabilities: unit.capabilities ?? [],
        flyer: unit.flyer,
        visible: unit.visible,
        status: unit.status,
        x: unit.x,
        y: unit.y,
        hp: unit.hp,
        maxHp: unit.maxHp,
        damage: unit.damage,
        attackSpeed: unit.attackSpeed,
        slowedRemaining: unit.slowedRemaining,
        slowedMultiplier: unit.slowedMultiplier,
        coldSlowedRemaining: unit.coldSlowedRemaining,
        coldSlowedMultiplier: unit.coldSlowedMultiplier,
        burningRemaining: unit.burningRemaining,
        arcaneCharge: unit.arcaneCharge,
        burningAnchor: unit.burningAnchor,
        attackRange: unit.attackRange,
        moveSpeed: unit.moveSpeed,
        armor: unit.armor,
        critChance: unit.critChance,
        critMultiplier: unit.critMultiplier,
        healthRegen: unit.healthRegen,
        xp: unit.xp,
        rank: unit.rank,
        xpToNextRank: unit.xpToNextRank,
        xpIntoCurrentRank: unit.xpIntoCurrentRank,
        recentRankUpSeconds: unit.recentRankUpSeconds,
        path: unit.progressionPath,
        perkIds: unit.perkIds,
        extraPerkSlots: unit.extraPerkSlots,
        shield: unit.shield,
        maxShield: unit.maxShield,
        shieldPools: unit.shieldPools,
        mana: unit.mana,
        maxMana: unit.maxMana,
        manaRegen: unit.manaRegen,
        activeBuffs: unit.activeBuffs,
        activeDebuffs: unit.activeDebuffs,
        perkCooldowns: unit.perkCooldowns,
        abilities: unit.abilities,
        ownerId: unit.ownerId,
        color: unit.color,
        carriedResourceType: unit.carriedResourceType,
        carriedAmount: unit.carriedAmount,
        targetX: unit.targetX,
        targetY: unit.targetY,
        moving: unit.moving,
        actionFacingDx: unit.actionFacingDx,
        actionFacingDy: unit.actionFacingDy,
        workTargetId: unit.workTargetId,
        effectiveTrap: unit.effectiveTrap,
        order: unit.order,
        focusTargetId: unit.focusTargetId,
        inventory: unit.inventory,
        channelLoopStart: unit.channelLoopStart,
        channelLoopEnd: unit.channelLoopEnd,
      })),
    }

    this.snapshotBuffer.push(frame)

    if (this.snapshotBuffer.length > this.maxBufferedSnapshots) {
      this.snapshotBuffer.shift()
    }

    // Debug HUD diagnostics. Records age/gap/etc using values already on
    // hand in this scope so no extra time reads are needed.
    this.recordNetSample(message.serverNow, now)

    // Build per-unit crit pools for this tick so the HP-diff loop below can
    // tag matching damage events as crits. Server pushes (unitId, damage)
    // entries every time a crit lands; we group by unit so multi-hit ticks
    // can match by amount, with a fallback to "any crit on this target this
    // tick" when amounts disagree (mark amplification, etc.).
    const critPool = new Map<number, number[]>()
    let critTargetsThisTick: Set<number> | null = null
    for (const evt of message.critEvents ?? []) {
      let pool = critPool.get(evt.unitId)
      if (!pool) {
        pool = []
        critPool.set(evt.unitId, pool)
      }
      pool.push(evt.damage)
      if (!critTargetsThisTick) critTargetsThisTick = new Set<number>()
      critTargetsThisTick.add(evt.unitId)
    }

    // Build per-unit minor-damage pools. Each entry is a portion of the
    // unit's HP-delta this tick that should render as a smaller distinctly-
    // colored popup (Reactive Flames splash, Electrified Caltrops bonus,
    // etc.). variant selects the renderer color downstream. The HP-diff
    // loop below peels matching amounts off before emitting the remainder
    // as a normal popup, so a victim hit by 1 DoT + 1 Reactive shows "1"
    // white + "1" orange.
    const minorPool = new Map<number, Array<{ damage: number; variant?: string }>>()
    for (const evt of message.minorDamageEvents ?? []) {
      let pool = minorPool.get(evt.unitId)
      if (!pool) {
        pool = []
        minorPool.set(evt.unitId, pool)
      }
      pool.push({ damage: evt.damage, variant: evt.variant })
    }

    // Build per-unit damage-type hint pools. Unlike the minor pool above,
    // hints do NOT spawn additional popups — they only COLOR the existing
    // major (floating-up) popup the HP-diff loop emits. The server auto-
    // emits a hint at every typed damage call, so a unit struck by a
    // shadow-typed Siphon Life tick this frame has a {damage, "shadow"}
    // entry here; the renderer paints the regular popup dark purple
    // instead of the default white/red. Unmatched hints (mitigation
    // mismatch, etc.) silently fall through to the default color.
    const damageTypePool = new Map<number, Array<{ damage: number; variant?: string }>>()
    for (const evt of message.damageTypeHints ?? []) {
      let pool = damageTypePool.get(evt.unitId)
      if (!pool) {
        pool = []
        damageTypePool.set(evt.unitId, pool)
      }
      pool.push({ damage: evt.damage, variant: evt.variant })
    }

    // Per-hit pool: the individual landed-hit amounts the server reports so we
    // can split a single HP-diff popup into one number per hit (two 12-damage
    // soldier strikes → "12" "12" instead of "24"). Only used when the entries
    // reconcile exactly with the major (post-minor-peel) remainder — see
    // emitDamageEvent — so mixed-in minor/regen ticks safely fall back to the
    // combined number.
    const hitPool = new Map<number, number[]>()
    for (const evt of message.hitDamageEvents ?? []) {
      if (evt.damage <= 0) continue
      let pool = hitPool.get(evt.unitId)
      if (!pool) {
        pool = []
        hitPool.set(evt.unitId, pool)
      }
      pool.push(evt.damage)
    }

    // Helper: emit a damage event with crit-pool matching and rolling
    // history mirror, shared by the surviving-unit HP-diff loop and the
    // killing-blow synthesis below. Pass-through `amount`, `unit info`, etc.
    const emitDamageEvent = (
      unitId: number,
      unitType: string,
      x: number,
      y: number,
      amount: number,
      ownerId: string | undefined,
    ) => {
      const isFriendly = !!this.localPlayerId && ownerId === this.localPlayerId

      // Peel any minor (ancillary) damage out of the HP-delta first. Each
      // entry in the pool is a server-authored portion of this tick's HP
      // loss that should render as a smaller popup colored by variant.
      // Cap each peeled amount at the remaining damage so we never emit
      // more than the actual HP delta.
      const minorList = minorPool.get(unitId)
      let remainder = amount
      if (minorList && minorList.length > 0) {
        for (const entry of minorList) {
          if (remainder <= 0) break
          const take = Math.min(entry.damage, remainder)
          if (take <= 0) continue
          this.damageEvents.push({
            unitId,
            unitType,
            x,
            y,
            amount: take,
            isFriendly,
            createdAt: now,
            kind: 'minor',
            minorVariant: entry.variant,
          })
          remainder -= take
        }
        // Pool consumed for this unit — drop so a same-unit retry later in
        // the tick (e.g. killing-blow synthesis) doesn't re-peel.
        minorPool.delete(unitId)
      }
      if (remainder <= 0) return

      // pushMajorHit renders ONE major (floating-up) popup for a sub-amount of
      // this tick's post-minor-peel HP loss, running the per-popup damage-type
      // and crit matching against the server pools. `index`/`count` drive the
      // horizontal fan-out + time stagger when a single HP delta is split into
      // multiple simultaneous hits (count === 1 ⇒ ordinary single popup).
      const pushMajorHit = (hitAmount: number, index: number, count: number) => {
        // Damage-type color hint: peel a matching entry off the pool to
        // color the major popup. Two-phase matching:
        //
        //   1. EXACT amount first. In the common single-source tick the
        //      server's hint equals this hit's amount and this wins.
        //
        //   2. FALLBACK to the first hint when no exact match exists. This
        //      catches the cases where the amount doesn't match any single
        //      hint cleanly: passive HP regen landing in the same tick
        //      (prev.hp - damage + 1 regen ≠ hint.damage), two siphoner
        //      beams stacking on one enemy, future shield-decay perks, etc.
        //      The cost of the fallback: if a unit takes mixed damage
        //      types in one tick (e.g. shadow Siphon + physical sword
        //      strike), the popup colors with the typed hint even though
        //      part of the HP loss was physical. We accept that edge — the
        //      user-facing rule "my Siphoner's damage looks shadow" reads
        //      cleanly, and physical attacks emit no hint so they can't
        //      compete in practice today.
        //
        // Consumed entries are removed so a second damage event on the
        // same unit this tick can't re-use the same hint (one popup per
        // hint, not infinite re-use).
        let damageType: string | undefined
        const typeList = damageTypePool.get(unitId)
        if (typeList && typeList.length > 0) {
          const exactIdx = typeList.findIndex((t) => t.damage === hitAmount)
          if (exactIdx >= 0) {
            damageType = typeList[exactIdx].variant
            typeList.splice(exactIdx, 1)
          } else {
            damageType = typeList[0].variant
            typeList.shift()
          }
        }

        let kind: 'normal' | 'crit' = 'normal'
        const pool = critPool.get(unitId)
        if (pool && pool.length > 0) {
          const exactIdx = pool.indexOf(hitAmount)
          if (exactIdx >= 0) {
            pool.splice(exactIdx, 1)
            kind = 'crit'
          } else if (critTargetsThisTick && critTargetsThisTick.has(unitId)) {
            // Consume the closest entry to avoid re-using it on a
            // subsequent same-tick same-target damage event.
            pool.shift()
            kind = 'crit'
          }
        }
        this.damageEvents.push({
          unitId,
          unitType,
          x,
          y,
          amount: hitAmount,
          isFriendly,
          // Stagger split hits into the near future so they pop in sequence
          // rather than all at once (the renderer holds a popup until its
          // startedAt is reached). The first hit fires immediately.
          createdAt: now + index * GameState.HIT_SPLIT_STAGGER_MS,
          kind,
          damageType,
          spreadIndex: count > 1 ? index : undefined,
          spreadCount: count > 1 ? count : undefined,
        })
        // Debug: pair with the [atk-timing] anim-start logs from unitAnimation
        // to verify swing-vs-damage alignment. The filter trio mirrors the
        // one in unitAnimation so the same toggles apply on both ends:
        //   window.debugAttackTiming         — master enable
        //   window.debugAttackTimingMineOnly — skip enemy-owned victims
        //   window.debugAttackTimingUnitType — only log victims of this type
        // Note: damage events filter on the VICTIM's type/owner, not the
        // attacker (the HP-diff derivation has no attacker attribution).
        const dbg = globalThis as {
          debugAttackTiming?: boolean
          debugAttackTimingMineOnly?: boolean
          debugAttackTimingUnitType?: string
        }
        if (dbg.debugAttackTiming) {
          let allow = true
          if (dbg.debugAttackTimingMineOnly) {
            if (this.localPlayerId) {
              allow = ownerId === this.localPlayerId
            } else {
              allow = ownerId !== '__enemy__'
            }
          }
          if (allow && dbg.debugAttackTimingUnitType && unitType !== dbg.debugAttackTimingUnitType) {
            allow = false
          }
          if (allow) {
            // eslint-disable-next-line no-console
            console.log(
              `[atk-timing] damage  unit=${unitId} type=${unitType} amount=${hitAmount} kind=${kind} t=${now.toFixed(0)}ms`,
            )
          }
        }
        let history = this.recentDamageByUnit.get(unitId)
        if (!history) {
          history = []
          this.recentDamageByUnit.set(unitId, history)
        }
        history.push({ amount: hitAmount, at: now })
      }

      // Split the major remainder into per-hit popups when the server's
      // individual landed-hit amounts reconcile with it EXACTLY and there are
      // 2+ of them (two soldier strikes, two frostbolts landing the same
      // snapshot). Reconciliation is the safety valve: if a minor/DoT tick or
      // passive regen also landed this tick the sum won't match the major
      // remainder, so we fall back to the single combined popup — never a
      // wrong split. Consume the pool so killing-blow synthesis can't re-split.
      const hits = hitPool.get(unitId)
      if (hits && hits.length >= 2 && hits.reduce((a, b) => a + b, 0) === remainder) {
        hitPool.delete(unitId)
        hits.forEach((hitAmount, index) => pushMajorHit(hitAmount, index, hits.length))
      } else {
        pushMajorHit(remainder, 0, 1)
      }
    }

    // Derive damage events by diffing HP against the previous snapshot. Any
    // strict decrease becomes a floating damage number. Heals (HP up) and
    // brand-new units (no prev) are skipped.
    const currentUnitIds = new Set<number>()
    for (const unit of frame.units) {
      currentUnitIds.add(unit.id)
      const prev = this.prevUnitHp.get(unit.id)
      if (prev !== undefined && unit.hp !== undefined && unit.hp < prev.hp) {
        const amount = prev.hp - unit.hp
        emitDamageEvent(unit.id, unit.unitType, unit.x, unit.y, amount, unit.ownerId)
      }
    }

    // Killing-blow detection — the server's drainPendingDeathsLocked removes
    // dead units from s.Units BEFORE the snapshot reaches the client, so the
    // HP-diff loop above never sees the final hit. Walk the previous-tick
    // state for any unit that disappeared this tick and synthesize a damage
    // event using prev.hp as the amount (the visible HP that went away). Use
    // the prev-tick position so the floating number lands at the body's last
    // known location even though the unit no longer exists.
    //
    // Overkill override: when the server reports a lethalDamageEvent for the
    // unit, the actual damage exceeded prev.hp. Use the server value so the
    // popup reads "100" instead of "5 / 5". Falls back to prev.hp when no
    // entry exists (exact kills, or kills via paths that don't go through
    // applyUnitDamageWithSourceLocked).
    const lethalOverride = new Map<number, number>()
    for (const evt of message.lethalDamageEvents ?? []) {
      lethalOverride.set(evt.unitId, evt.damage)
    }
    for (const [unitId, prev] of this.prevUnitHp) {
      if (currentUnitIds.has(unitId)) continue
      if (prev.hp <= 0) continue
      const amount = lethalOverride.get(unitId) ?? prev.hp
      emitDamageEvent(unitId, prev.unitType, prev.x, prev.y, amount, prev.ownerId)
    }

    // Heal events — intentional healing (heal ability). HP going up is not a
    // damage event, so the server reports each heal explicitly. Resolve the
    // unit's live position from this frame and spawn a light-green "+N" that
    // floats up like a normal damage number. A unit healed and then killed in
    // the same tick is dropped (no current position to anchor to).
    if (message.healEvents && message.healEvents.length > 0) {
      const unitById = new Map(frame.units.map((u) => [u.id, u]))
      for (const evt of message.healEvents) {
        if (evt.amount <= 0) continue
        const u = unitById.get(evt.unitId)
        if (!u) continue
        this.damageEvents.push({
          unitId: u.id,
          unitType: u.unitType,
          x: u.x,
          y: u.y,
          amount: evt.amount,
          isFriendly: !!this.localPlayerId && u.ownerId === this.localPlayerId,
          createdAt: now,
          kind: 'heal',
        })
      }
    }

    // Mana restore popups — mirror the heal-event loop above. Blue "+N"
    // floats over the recipient when an intentional mana grant lands
    // (Repurposed Life, future cleric mana abilities). Passive regen is
    // not emitted by the server, so this loop naturally only fires for
    // intentional grants.
    if (message.manaRestoreEvents && message.manaRestoreEvents.length > 0) {
      const unitById = new Map(frame.units.map((u) => [u.id, u]))
      for (const evt of message.manaRestoreEvents) {
        if (evt.amount <= 0) continue
        const u = unitById.get(evt.unitId)
        if (!u) continue
        this.damageEvents.push({
          unitId: u.id,
          unitType: u.unitType,
          x: u.x,
          y: u.y,
          amount: evt.amount,
          isFriendly: !!this.localPlayerId && u.ownerId === this.localPlayerId,
          createdAt: now,
          kind: 'manaRestore',
        })
      }
    }

    // Evade popups — a hit was fully dodged/blocked, so no HP changed and
    // there is nothing to peel off an HP-diff. Spawn directly over the unit,
    // mirroring the heal/mana-restore loops above rather than the minor-pool
    // peel used for HP-diff-derived popups.
    if (message.evadeEvents && message.evadeEvents.length > 0) {
      const unitById = new Map(frame.units.map((u) => [u.id, u]))
      for (const evt of message.evadeEvents) {
        const u = unitById.get(evt.unitId)
        if (!u) continue
        this.damageEvents.push({
          unitId: u.id,
          unitType: u.unitType,
          x: u.x,
          y: u.y,
          amount: 0,
          isFriendly: !!this.localPlayerId && u.ownerId === this.localPlayerId,
          createdAt: now,
          kind: 'evade',
          evadeKind: evt.kind,
        })
      }
    }

    this.prevUnitHp.clear()
    for (const unit of frame.units) {
      if (unit.hp !== undefined) {
        this.prevUnitHp.set(unit.id, {
          hp: unit.hp,
          x: unit.x,
          y: unit.y,
          unitType: unit.unitType,
          ownerId: unit.ownerId,
        })
      }
    }
    // Prune the rolling damage history to the last RECENT_DAMAGE_WINDOW_MS
    // so it doesn't accumulate forever.
    const cutoff = now - this.RECENT_DAMAGE_WINDOW_MS
    for (const [unitId, history] of this.recentDamageByUnit) {
      const kept = history.filter((entry) => entry.at >= cutoff)
      if (kept.length === 0) {
        this.recentDamageByUnit.delete(unitId)
      } else {
        this.recentDamageByUnit.set(unitId, kept)
      }
    }

    // Marksman Double Shot — combined yellow damage number.
    // When a projectile flagged `doubleShotSecond` was in flight last tick
    // but is gone this tick, the second arrow has just landed. Sum the
    // recent damage on its target (which includes both arrows' hits within
    // the rolling window) and emit a combined yellow event at the target's
    // current position. The two underlying white numbers are still rendered
    // — combined sits on top.
    const currentProjectileIds = new Set<string>()
    for (const proj of message.projectiles ?? []) {
      currentProjectileIds.add(proj.id)
    }
    for (const [id, prev] of this.prevProjectiles) {
      if (currentProjectileIds.has(id)) continue
      if (!prev.doubleShotSecond) continue
      const targetId = prev.targetUnitId
      if (!targetId) continue
      const history = this.recentDamageByUnit.get(targetId)
      if (!history || history.length === 0) continue
      const sum = history.reduce((s, e) => s + e.amount, 0)
      const target = frame.units.find((u) => u.id === targetId)
      const x = target?.x ?? prev.targetX
      const y = target?.y ?? prev.targetY
      this.damageEvents.push({
        unitId: targetId,
        unitType: target?.unitType ?? 'archer',
        x,
        y,
        amount: sum,
        isFriendly: !!target && !!this.localPlayerId && target.ownerId === this.localPlayerId,
        createdAt: now,
        kind: 'combined',
      })
    }
    // Arrow-shot SFX. A projectile that appears this tick but was not in flight
    // last tick means a shot was just loosed; if it renders as an arrow (any
    // shooter whose projectile falls back to the default arrow draw — archers,
    // towers, ranged raiders — as opposed to a magic-bolt sprite) we play the
    // arrow-shot effect. Gating on low `progress` keeps arrows that were already
    // mid-flight when we joined a match (all "new" to us on the first snapshot)
    // from retriggering the sound. Uses prevProjectiles before it is refreshed
    // below.
    for (const proj of message.projectiles ?? []) {
      if (this.prevProjectiles.has(proj.id)) continue
      if (proj.progress > 0.15) continue
      // Suppress the shot sound when it's loosed off-screen (origin = the
      // shooter's position at fire time).
      if (!this.isWorldPointInView(proj.originX, proj.originY)) continue
      if (projectileRendersAsArrow(proj.variant)) {
        // Anything whose shot draws as the default arrow — archers, towers,
        // ranged raiders.
        playSfx('arrow_shot.mp3')
      } else {
        // A magic-bolt sprite. Play the spell-cast sound only when the shooter
        // is a caster-archetype unit (its normal attack). Other sources of
        // sprite projectiles — e.g. a melee unit's frost_sword item proc firing
        // a frost_bolt — are NOT caster attacks, so they stay silent here.
        const owner = frame.units.find((u) => u.id === proj.ownerUnitId)
        if (owner && UNIT_DEF_MAP.get(owner.unitType)?.archetype === 'caster') {
          playSfx('spell_attack.mp3')
        }
      }
    }

    // Melee attack SFX — the swing counterpart of the arrow sound above. The
    // server pushes one event per melee swing that started this tick, carrying
    // the attackType sound key it resolved from the unit def / promotion path
    // ("swing", "stab", ...). Two throttles keep a big melee from drowning in
    // identical sounds:
    //   1. Per tick, collapse identical keys to a single candidate — dozens of
    //      units swinging the same instant would just clip into noise, not read
    //      as distinct hits.
    //   2. Rolling-window rate cap — at most MELEE_SOUND_MAX_PER_WINDOW plays of
    //      a key within MELEE_SOUND_WINDOW_MS; further swings are dropped. You
    //      still hear a steady stream (the fight reads as busy) but the loudness
    //      no longer scales with unit count.
    // Unknown/absent keys resolve to no sound file and no-op in playSfx.
    if (message.meleeAttackEvents && message.meleeAttackEvents.length > 0) {
      const consideredThisTick = new Set<string>()
      const cutoff = now - this.MELEE_SOUND_WINDOW_MS
      for (const evt of message.meleeAttackEvents) {
        const key = evt.attackType
        if (!key) continue
        // Off-screen swings make no sound — checked per event (before the
        // per-tick dedup) so an in-view swing still fires even when an
        // off-view one of the same type arrived first this tick.
        if (!this.isWorldPointInView(evt.x, evt.y)) continue
        if (consideredThisTick.has(key)) continue
        consideredThisTick.add(key)
        let times = this.recentMeleeSoundPlays.get(key)
        if (!times) {
          times = []
          this.recentMeleeSoundPlays.set(key, times)
        }
        // Drop plays that have aged out of the window (timestamps are pushed in
        // increasing order, so expired ones are always at the front).
        let expired = 0
        while (expired < times.length && times[expired] < cutoff) expired++
        if (expired > 0) times.splice(0, expired)
        if (times.length >= this.MELEE_SOUND_MAX_PER_WINDOW) continue
        times.push(now)
        playSfx(`${key}.mp3`)
      }
    }

    // Refresh prevProjectiles for next tick's diff.
    this.prevProjectiles.clear()
    for (const proj of message.projectiles ?? []) {
      this.prevProjectiles.set(proj.id, proj)
    }

    // Derive resource deposit events by diffing carriedAmount per unit.
    // A worker that previously carried a non-zero amount and now carries 0
    // has just deposited at a townhall — emit at the unit's current
    // position so the float-text reads as if it spawned at the drop site.
    for (const unit of frame.units) {
      const prev = this.prevUnitCarried.get(unit.id)
      const currentAmount = unit.carriedAmount ?? 0
      if (prev && prev.amount > 0 && currentAmount === 0) {
        this.resourceDepositEvents.push({
          unitId: unit.id,
          x: unit.x,
          y: unit.y,
          resourceId: prev.resourceType,
          amount: prev.amount,
          // Always full for now. Future: townhall capacity caps or perk
          // penalties should set a fractional value to drive yellow/red text.
          capacityFraction: 1,
          createdAt: now,
        })
      }
    }
    this.prevUnitCarried.clear()
    for (const unit of frame.units) {
      if (
        unit.carriedAmount !== undefined &&
        unit.carriedAmount > 0 &&
        unit.carriedResourceType
      ) {
        this.prevUnitCarried.set(unit.id, {
          resourceType: unit.carriedResourceType,
          amount: unit.carriedAmount,
        })
      }
    }

    this.units = frame.units.map((unit) => ({ ...unit }))
    this.banners = message.banners ?? []
    this.traps = message.traps ?? []
    this.projectiles = message.projectiles ?? []
    this.beams = message.beams ?? []
    this.effects = message.effects ?? []
    if (this.selectedTrapId && !this.traps.some((t) => t.id === this.selectedTrapId)) {
      this.selectedTrapId = null
    }
    // Battle tracker (debug) — only populated when the active map has
    // debug.battleTracker=true. Null when disabled so reactive watchers can
    // key off a single "is the debug panel visible?" check.
    this.battleTracker = message.battleTracker ?? null
    this.applyPlayerSnapshots(message.players)
    if (message.wave) {
      this.waveSnapshot = message.wave
    }
    if (message.gameOver) {
      this.gameOverSnapshot = message.gameOver
    }
    if (message.victory) {
      this.victorySnapshot = message.victory
    }

    // Freeze the end-of-match roster + this viewer's earned DP exactly once,
    // from the first snapshot that reports the match is over. playerSnapshots
    // was already updated above (applyPlayerSnapshots ran earlier this call),
    // so it reflects this snapshot's full roster.
    const matchEnded = !!message.gameOver || message.victory?.achieved === true
    if (matchEnded && !this.endRosterFrozen) {
      this.endRosterFrozen = true
      // Clone each element too (not just the array): playerSnapshots stores
      // wire objects directly, so a bare spread would leave the frozen roster
      // aliasing objects a later snapshot could touch. Defensive immutability.
      this.frozenEndPlayers = this.playerSnapshots.map((p) => ({ ...p }))
      this.matchDominionPointsEarned = message.gameOver?.yourDominionPointsEarned ?? 0
    }

    if (message.fow) {
      this.fow.applySnapshot(message.fow)
    }

    this.waveUpgrade = message.waveUpgrade ?? null

    // Rebuild the camp snapshot map each tick. Empty/absent snapshot
    // field ⇒ the minimap falls back to startingTier from
    // MapConfig.neutralSpawns and shows every camp's dot.
    this.neutralCampSnapshotsById.clear()
    if (message.neutralCamps) {
      for (const camp of message.neutralCamps) {
        this.neutralCampSnapshotsById.set(camp.id, camp)
      }
    }

    // Rebuild the loot-drop map each tick. Chests are always-visible world
    // entities — no FOW gating — so we simply replace the entire map.
    this.lootDropsById.clear()
    if (message.lootDrops) {
      for (const drop of message.lootDrops) {
        this.lootDropsById.set(drop.id, drop)
      }
    }

    // Rebuild the zone snapshot map each tick. Absent field = map has no zones.
    this.zoneSnapshotsById.clear()
    if (message.zones) {
      for (const zone of message.zones) {
        this.zoneSnapshotsById.set(zone.id, zone)
      }
    }

    const nextPaused = message.paused === true
    if (nextPaused && !this.paused) {
      this.pausedSinceMs = Date.now()
    } else if (!nextPaused && this.paused) {
      this.pausedSinceMs = 0
    }
    this.paused = nextPaused
    this.pausedBy = message.pausedBy ?? ''

    const validIds = new Set(this.units.map((u) => u.id))
    const unitById = new Map(this.units.map((u) => [u.id, u]))

    // Drop from the selection any unit that no longer exists, or that has just
    // entered build mode. A worker that starts constructing is hidden inside
    // the building footprint (Status "Building"); leaving it selected lets the
    // player issue a move order that pulls it back out and wedges the build, so
    // it must not remain commandable.
    const isCommandable = (id: number): boolean => {
      const u = unitById.get(id)
      if (!u) return false
      if (u.status === 'Building' || u.status === 'Building (Paused)') return false
      return true
    }

    for (const id of Array.from(this.selectedUnitIds)) {
      if (!isCommandable(id)) {
        this.selectedUnitIds.delete(id)
      }
    }

    this.selectedUnitOrder = this.selectedUnitOrder.filter(isCommandable)

    if (this.selectedUnitOrder.length === 0) {
      this.unitTargetingMode = null
    }

    if (this.inspectedEnemyUnitId !== null && !validIds.has(this.inspectedEnemyUnitId)) {
      this.inspectedEnemyUnitId = null
    }

    if (this.inspectedAllyUnitId !== null && !validIds.has(this.inspectedAllyUnitId)) {
      this.inspectedAllyUnitId = null
    }
  }

  getInterpolatedUnits(renderTime: number): Unit[] {
    if (this.snapshotBuffer.length === 0) {
      return this.units
    }

    if (this.snapshotBuffer.length === 1) {
      return this.snapshotBuffer[0].units.map((unit) => ({ ...unit }))
    }

    const targetTime = renderTime - this.interpolationDelayMs

    let fromFrame: InterpolationFrame | null = null
    let toFrame: InterpolationFrame | null = null

    for (let i = 0; i < this.snapshotBuffer.length - 1; i++) {
      const current = this.snapshotBuffer[i]
      const next = this.snapshotBuffer[i + 1]

      if (current.receivedAt <= targetTime && next.receivedAt >= targetTime) {
        fromFrame = current
        toFrame = next
        break
      }
    }

    if (!fromFrame && !toFrame && targetTime < this.snapshotBuffer[0].receivedAt) {
      return this.snapshotBuffer[0].units.map((unit) => ({ ...unit }))
    }

    if (!fromFrame || !toFrame) {
      const latest = this.snapshotBuffer[this.snapshotBuffer.length - 1]
      return latest.units.map((unit) => ({ ...unit }))
    }

    const duration = Math.max(1, toFrame.receivedAt - fromFrame.receivedAt)
    const alphaRaw = (targetTime - fromFrame.receivedAt) / duration
    const alpha = Math.max(0, Math.min(alphaRaw, 1))

    const fromMap = new Map<number, (typeof fromFrame.units)[0]>()
    for (const u of fromFrame.units) fromMap.set(u.id, u)
    const interpolated: Unit[] = []

    for (const toUnit of toFrame.units) {
      const fromUnit = fromMap.get(toUnit.id)

      if (!fromUnit) {
        interpolated.push({ ...toUnit })
        continue
      }

      interpolated.push({
        ...toUnit,
        x: fromUnit.x + (toUnit.x - fromUnit.x) * alpha,
        y: fromUnit.y + (toUnit.y - fromUnit.y) * alpha,
      })
    }

    return interpolated
  }

  private getInteractionUnits(): Unit[] {
    return this.getInterpolatedUnits(performance.now())
  }

  private isOwnedByLocalPlayer(unit: { ownerId?: string }): boolean {
    return !!this.localPlayerId && unit.ownerId === this.localPlayerId
  }

  // ownerId → TeamID; absent ⇒ 0 (default shared team). Mirrors the server's
  // playerTeamLocked (unknown owner ⇒ default team).
  private teamOf(ownerId: string): number {
    return this.playerTeams.get(ownerId) ?? 0
  }

  // Alliance is now team-based (PlayerSnapshot.teamId), mirroring the server
  // chokepoint. Same owner ⇒ never hostile; the __enemy__ AI and __neutral__
  // camp mobs ⇒ hostile to every team; otherwise hostile iff different team.
  // At the default (all team 0) this is exactly the old "only __enemy__ is
  // hostile" behavior, extended to include neutral camps.
  // Use this (not raw ownerId comparisons) for any "should the cursor /
  // health bar / attack visual treat this as an enemy?" decision.
  isHostileToLocalPlayer(ownerId: string | undefined): boolean {
    if (!ownerId) return false
    if (!this.localPlayerId) return ownerId === ENEMY_PLAYER_ID || ownerId === NEUTRAL_PLAYER_ID
    return this.ownersAreHostile(ownerId, this.localPlayerId)
  }

  ownersAreHostile(a: string | null | undefined, b: string | null | undefined): boolean {
    if (!a || !b || a === b) return false
    if (a === ENEMY_PLAYER_ID || b === ENEMY_PLAYER_ID) return true
    if (a === NEUTRAL_PLAYER_ID || b === NEUTRAL_PLAYER_ID) return true
    return this.teamOf(a) !== this.teamOf(b)
  }

  // True when the unit belongs to a real allied player — same team, not me,
  // not the __enemy__ AI, not a neutral mob. Used to gate read-only
  // inspection of allies. Mirrors the server playersAreFriendly (which is
  // NOT just !hostile).
  private isAlliedOtherPlayerUnit(unit: Unit): boolean {
    const ownerId = unit.ownerId
    if (!ownerId || ownerId === ENEMY_PLAYER_ID || ownerId === NEUTRAL_PLAYER_ID) return false
    if (!this.localPlayerId || ownerId === this.localPlayerId) return false
    return this.teamOf(ownerId) === this.teamOf(this.localPlayerId)
  }

  clearSelection() {
    this.selectedUnitIds.clear()
    this.selectedUnitOrder = []
    this.selectedBuildingId = null
    this.selectedObstacleId = null
    this.selectedTrapId = null
    this.selectedZoneId = null
    this.inspectedEnemyUnitId = null
    this.inspectedAllyUnitId = null
    this.buildingTargetingMode = null
    this.unitTargetingMode = null
    this.workerBuildMenuOpen = false
    this.buildPlacement = null
  }

  selectUnit(unitId: number) {
    const unit = this.units.find((u) => u.id === unitId)
    if (!unit || !this.isOwnedByLocalPlayer(unit)) return

    this.selectedUnitIds.clear()
    this.selectedUnitIds.add(unitId)
    this.selectedUnitOrder = [unitId]
    this.selectedBuildingId = null
    this.selectedObstacleId = null
    this.selectedTrapId = null
    this.selectedZoneId = null
    this.inspectedEnemyUnitId = null
    this.inspectedAllyUnitId = null
    this.buildingTargetingMode = null
    this.unitTargetingMode = null
    this.workerBuildMenuOpen = false
    this.buildPlacement = null
  }

  setSelection(unitIds: number[]) {
    const ownedIds = unitIds.filter((id) => {
      const unit = this.units.find((u) => u.id === id)
      return !!unit && this.isOwnedByLocalPlayer(unit)
    })

    this.selectedUnitIds.clear()
    this.selectedBuildingId = null
    this.selectedObstacleId = null
    this.selectedTrapId = null
    this.selectedZoneId = null
    this.buildingTargetingMode = null
    this.unitTargetingMode = null
    this.workerBuildMenuOpen = false
    this.buildPlacement = null

    for (const id of ownedIds) {
      this.selectedUnitIds.add(id)
    }

    this.selectedUnitOrder = [...ownedIds]
  }

  /** Bind the current selection to a control group (1..10). Subsequent
   *  presses of the same digit recall this exact roster (filtered to alive +
   *  owned units at recall time). Empty selections clear the slot so a
   *  later recall is a no-op rather than recalling stale ids. */
  assignControlGroup(groupKey: number) {
    if (!Number.isInteger(groupKey) || groupKey < 1 || groupKey > 10) return
    const ids = [...this.selectedUnitOrder]
    if (ids.length === 0) {
      this.controlGroups.delete(groupKey)
      return
    }
    this.controlGroups.set(groupKey, ids)
  }

  /** Recall a previously-assigned control group, replacing the current
   *  selection. No-op when the slot is empty or holds only stale ids.
   *  Returns true when a recall actually replaced the selection, false
   *  when the slot was empty/invalid — used by the input layer to detect
   *  "double-tap on a populated group" and center the camera on it. */
  selectControlGroup(groupKey: number): boolean {
    if (!Number.isInteger(groupKey) || groupKey < 1 || groupKey > 10) return false
    const ids = this.controlGroups.get(groupKey)
    if (!ids || ids.length === 0) return false
    this.setSelection(ids)
    return true
  }

  openWorkerBuildMenu() {
    const selectedUnits = this.getSelectedUnits()
    if (!selectedUnits.some((u) => u.capabilities.includes('build'))) return
    this.workerBuildMenuOpen = true
  }

  closeWorkerBuildMenu() {
    this.workerBuildMenuOpen = false
  }

  addFormationMoveMarkers(destX: number, destY: number, durationMs = 550) {
    const points = this.getFormationDestinations(destX, destY)
    this.addMoveMarkers(points, durationMs)
  }

  addUnitToSelection(unitId: number) {
    const unit = this.units.find((u) => u.id === unitId)
    if (!unit || !this.isOwnedByLocalPlayer(unit)) return
    if (this.selectedUnitIds.has(unitId)) return

    this.selectedUnitIds.add(unitId)
    this.selectedUnitOrder.push(unitId)
  }

  removeUnitFromSelection(unitId: number) {
    this.selectedUnitIds.delete(unitId)
    this.selectedUnitOrder = this.selectedUnitOrder.filter((id) => id !== unitId)
  }

  toggleUnitSelection(unitId: number) {
    const unit = this.units.find((u) => u.id === unitId)
    if (!unit || !this.isOwnedByLocalPlayer(unit)) return

    if (this.selectedUnitIds.has(unitId)) {
      this.removeUnitFromSelection(unitId)
    } else {
      this.addUnitToSelection(unitId)
    }
  }

  beginSelectionBox(x: number, y: number) {
    this.selectionBox.startX = x
    this.selectionBox.startY = y
    this.selectionBox.currentX = x
    this.selectionBox.currentY = y
    this.selectionBox.active = true
  }

  updateSelectionBox(x: number, y: number) {
    this.selectionBox.currentX = x
    this.selectionBox.currentY = y
  }

  endSelectionBox() {
    this.selectionBox.active = false
  }

  getSelectionBounds() {
    const { startX, startY, currentX, currentY } = this.selectionBox

    return {
      left: Math.min(startX, currentX),
      right: Math.max(startX, currentX),
      top: Math.min(startY, currentY),
      bottom: Math.max(startY, currentY),
    }
  }

  // True when the selection box covers the unit's upper-feet / lower-leg
  // level — the test point sits 85% down the sprite-aware body rect, so a
  // drag only needs to reach the ankles (not the mid-torso) to pick the
  // unit up. Same body rect as single-click hit testing so drag-select and
  // click-select agree on what "hits" a unit.
  isUnitInSelectionBox(unit: {
    ownerId?: string
    visible?: boolean
    x: number
    y: number
    unitType?: string
    path?: string
  }): boolean {
    if (!this.selectionBox.active) return false
    if (!this.isOwnedByLocalPlayer(unit) || !unit.visible) return false
    const { left, right, top, bottom } = this.getSelectionBounds()
    const rect = getUnitBodyRect({
      x: unit.x,
      y: unit.y,
      unitType: unit.unitType,
      path: unit.path,
      padding: 0,
    })
    const cx = (rect.minX + rect.maxX) / 2
    const cy = rect.minY + (rect.maxY - rect.minY) * 0.85
    return cx >= left && cx <= right && cy >= top && cy <= bottom
  }

  private getUnitsInSelectionBox(): Unit[] {
    return this.getInteractionUnits()
      .filter((unit) => this.isUnitInSelectionBox(unit))
      .sort((a, b) => {
        const rowTolerance = 12
        const yDiff = a.y - b.y

        if (Math.abs(yDiff) > rowTolerance) {
          return yDiff
        }

        return a.x - b.x
      })
  }

  selectUnitsInBox() {
    const selected = this.getUnitsInSelectionBox().map((unit) => unit.id)
    this.setSelection(selected)
  }

  addUnitsInBox() {
    const unitsInBox = this.getUnitsInSelectionBox()

    for (const unit of unitsInBox) {
      this.addUnitToSelection(unit.id)
    }
  }

  // Selects every owned, visible unit matching the given (unitType, path)
  // tuple whose position is inside the supplied world-space viewport rect.
  // Used by the double-click "select all of type on screen" gesture. Path
  // is part of the match key so a double-clicked Vanguard selects only
  // other Vanguards rather than every base Soldier — same for other ranked
  // paths (Berserker, Marksman, Trapper, Cleric, Arch Mage). Units that
  // haven't ranked into a path are bucketed together as "no path".
  selectVisibleSameTypeUnits(
    unitType: string,
    path: string | undefined,
    viewBounds: { left: number; top: number; right: number; bottom: number },
  ) {
    const targetPath = path && path !== 'none' ? path : ''
    const matches = this.getInteractionUnits()
      .filter((unit) => {
        if (!this.isOwnedByLocalPlayer(unit) || !unit.visible) return false
        if (unit.unitType !== unitType) return false
        const unitPath = unit.path && unit.path !== 'none' ? unit.path : ''
        if (unitPath !== targetPath) return false
        return (
          unit.x >= viewBounds.left &&
          unit.x <= viewBounds.right &&
          unit.y >= viewBounds.top &&
          unit.y <= viewBounds.bottom
        )
      })
      .map((unit) => unit.id)

    if (matches.length === 0) return
    this.setSelection(matches)
  }

  // Hit-tests against the unit's visible body (sprite or procedural bounds),
  // not a circle at the feet anchor. `padding` grows the hit rect outward on
  // all sides; the default is tuned to feel forgiving without overlapping
  // adjacent units. When multiple units' bodies contain the point, the unit
  // with the largest y wins — this matches the renderer's Y-sort at
  // CanvasRenderer.drawUnits, so the unit drawn on top (standing "in front")
  // is the one that gets selected.
  private pickFrontmostUnit(
    x: number,
    y: number,
    padding: number | undefined,
    accept: (unit: Unit) => boolean,
  ): Unit | undefined {
    let best: Unit | undefined
    for (const unit of this.getInteractionUnits()) {
      if (!accept(unit)) continue
      if (!isPointInUnitBody(x, y, unit, padding)) continue
      if (!best || unit.y > best.y) best = unit
    }
    return best
  }

  getUnitAtPosition(x: number, y: number, padding?: number): Unit | undefined {
    return this.pickFrontmostUnit(x, y, padding, (unit) =>
      this.isOwnedByLocalPlayer(unit) && unit.visible,
    )
  }

  getEnemyUnitAtPosition(x: number, y: number, padding?: number): Unit | undefined {
    return this.pickFrontmostUnit(x, y, padding, (unit) =>
      unit.visible && this.isHostileToLocalPlayer(unit.ownerId),
    )
  }

  // Allied other-player units. Used by left-click to open a read-only
  // inspection panel — no orders can be issued against / for these units.
  getAllyUnitAtPosition(x: number, y: number, padding?: number): Unit | undefined {
    return this.pickFrontmostUnit(x, y, padding, (unit) =>
      unit.visible && this.isAlliedOtherPlayerUnit(unit),
    )
  }

  getOrderedSelectedUnitIds(): number[] {
    return this.selectedUnitOrder.filter((id) => {
      if (!this.selectedUnitIds.has(id)) return false
      const unit = this.units.find((u) => u.id === id)
      return !!unit && this.isOwnedByLocalPlayer(unit)
    })
  }

  getFormationDestinations(destX: number, destY: number): Vec2[] {
    const selectedUnits = this.getSelectedUnits()
    const count = selectedUnits.length

    if (count === 0) {
      return []
    }

    if (count === 1) {
      return [{ x: destX, y: destY }]
    }

    // Must match server unitFormationSpacing in state.go so the move-marker
    // circles preview the actual landing positions. If the server constant
    // changes, update this too.
    return buildFormationDestinations(selectedUnits, { x: destX, y: destY }, 40)
  }

  getBuildingAtPosition(x: number, y: number, padding = 0): BuildingTile | undefined {
    const { cellSize, buildings } = this.mapConfig

    return buildings.find((building) => {
      if (!building.visible) return false
      if (building.ghost) return false

      const left = building.x * cellSize - padding
      const top = building.y * cellSize - padding
      const right = left + building.width * cellSize + padding * 2
      const bottom = top + building.height * cellSize + padding * 2

      return x >= left && x <= right && y >= top && y <= bottom
    })
  }

  selectBuilding(buildingId: string) {
    this.selectedUnitIds.clear()
    this.selectedUnitOrder = []
    this.selectedBuildingId = buildingId
    this.selectedObstacleId = null
    this.selectedTrapId = null
    this.selectedZoneId = null
    this.inspectedEnemyUnitId = null
    this.inspectedAllyUnitId = null
    this.buildingTargetingMode = null
    this.unitTargetingMode = null
    this.workerBuildMenuOpen = false
    this.buildPlacement = null
  }

  selectObstacle(obstacleId: string) {
    this.selectedUnitIds.clear()
    this.selectedUnitOrder = []
    this.selectedBuildingId = null
    this.selectedObstacleId = obstacleId
    this.selectedTrapId = null
    this.selectedZoneId = null
    this.inspectedEnemyUnitId = null
    this.inspectedAllyUnitId = null
    this.buildingTargetingMode = null
    this.unitTargetingMode = null
    this.workerBuildMenuOpen = false
    this.buildPlacement = null
  }

  // Selects a trap by ID, clearing any other selection. Mirrors selectBuilding
  // / selectObstacle. No ownership filter — any visible trap can be inspected.
  selectTrap(trapId: string) {
    this.selectedUnitIds.clear()
    this.selectedUnitOrder = []
    this.selectedBuildingId = null
    this.selectedObstacleId = null
    this.selectedTrapId = trapId
    this.selectedZoneId = null
    this.inspectedEnemyUnitId = null
    this.inspectedAllyUnitId = null
    this.buildingTargetingMode = null
    this.unitTargetingMode = null
    this.workerBuildMenuOpen = false
    this.buildPlacement = null
  }

  /** Selects a zone by id. Any pre-existing unit/building/obstacle/trap selection
   *  is cleared first. Zone selection has the lowest priority — it only fires when
   *  a left-click (non-drag) lands on a zone cell and no unit/building/trap was hit. */
  selectZone(zoneId: string) {
    this.selectedUnitIds.clear()
    this.selectedUnitOrder = []
    this.selectedBuildingId = null
    this.selectedObstacleId = null
    this.selectedTrapId = null
    this.selectedZoneId = zoneId
    this.inspectedEnemyUnitId = null
    this.inspectedAllyUnitId = null
    this.buildingTargetingMode = null
    this.unitTargetingMode = null
    this.workerBuildMenuOpen = false
    this.buildPlacement = null
  }

  /** Hit-test a world-space point against zones on the current map. Returns the
   *  id of the first zone whose cell set contains the grid cell at (worldX, worldY),
   *  or null when no zone is hit. Uses the map's cellSize for the conversion.
   *
   *  Reuses the buildZoneCellIndex helper (which iterates every cell once and
   *  builds a Map) only when the map config changes — the result is memoised via
   *  a simple reference-equality guard on mapConfig.zones. */
  private _zoneCellIndexZones: import('../network/protocol').Zone[] | null = null
  private _zoneCellIndex: Map<string, string> | null = null

  getZoneIdAtWorld(worldX: number, worldY: number): string | null {
    const zones = this.mapConfig.zones
    if (!zones || zones.length === 0) return null
    if (this._zoneCellIndexZones !== zones || this._zoneCellIndex === null) {
      this._zoneCellIndexZones = zones
      this._zoneCellIndex = buildZoneCellIndex(zones)
    }
    const cellSize = this.mapConfig.cellSize
    const cellX = Math.floor(worldX / cellSize)
    const cellY = Math.floor(worldY / cellSize)
    return this._zoneCellIndex.get(cellKey(cellX, cellY)) ?? null
  }

  /** Invalidates the zone cell index when the map is reloaded so a new zone
   *  layout doesn't serve stale hit-test results. */
  private invalidateZoneCellIndex() {
    this._zoneCellIndexZones = null
    this._zoneCellIndex = null
  }

  getSelectedBuilding(): BuildingTile | null {
    if (!this.selectedBuildingId) return null
    return this.mapConfig.buildings.find((building) => building.id === this.selectedBuildingId) ?? null
  }

  getSelectedBuildingType(): string | null {
    return this.getSelectedBuilding()?.buildingType ?? null
  }

  getSelectedObstacle(): ObstacleTile | null {
    if (!this.selectedObstacleId) return null
    return this.mapConfig.obstacles.find((obstacle) => obstacle.id === this.selectedObstacleId) ?? null
  }

  getSelectedTrap(): TrapSnapshot | null {
    if (!this.selectedTrapId) return null
    return this.traps.find((trap) => trap.id === this.selectedTrapId) ?? null
  }

  // Returns the trap under (x, y). Trap selection requires clicking near the
  // CENTER of the trap (within TRAP_CENTER_HIT_RADIUS world units) rather than
  // anywhere inside the zone — this keeps trap selection deliberate and avoids
  // stealing clicks from ground/movement orders inside large trap radii. When
  // multiple centers are within range, the closest wins.
  getTrapAtPosition(x: number, y: number): TrapSnapshot | undefined {
    const hitRadiusSq = TRAP_CENTER_HIT_RADIUS * TRAP_CENTER_HIT_RADIUS
    let best: TrapSnapshot | undefined
    let bestDistSq = Infinity
    for (const trap of this.traps) {
      const dx = trap.x - x
      const dy = trap.y - y
      const distSq = dx * dx + dy * dy
      if (distSq > hitRadiusSq) continue
      if (distSq < bestDistSq) {
        best = trap
        bestDistSq = distSq
      }
    }
    return best
  }

  // Returns the obstacle covering the given world point, or undefined. Only
  // obstacles whose def marks them as selectable (capability `selectable` or
  // `resource-source`) are considered — walls return undefined here.
  getObstacleAtPosition(x: number, y: number, padding = 0): ObstacleTile | undefined {
    const { cellSize, obstacles } = this.mapConfig
    return obstacles.find((o) => {
      if (!o.id) return false
      const caps = o.capabilities ?? []
      if (!caps.includes('selectable') && !caps.includes('resource-source')) return false
      const w = (o.width ?? 1) * cellSize
      const h = (o.height ?? 1) * cellSize
      const left = o.x * cellSize - padding
      const top = o.y * cellSize - padding
      const right = left + w + padding * 2
      const bottom = top + h + padding * 2
      return x >= left && x <= right && y >= top && y <= bottom
    })
  }

  // Returns the obstacle that would be targeted by a gather order at the given
  // world point, if any. Matches obstacles with the `resource-source`
  // capability (trees); rocks/walls are excluded.
  getGatherableObstacleAtPosition(x: number, y: number, padding = 0): ObstacleTile | undefined {
    const obstacle = this.getObstacleAtPosition(x, y, padding)
    if (!obstacle) return undefined
    if (!(obstacle.capabilities ?? []).includes('resource-source')) return undefined
    return obstacle
  }

  setHoveredInteractableObstacle(obstacleId: string | null) {
    this.hoveredInteractableObstacleId = obstacleId
  }

  beginBuildingTargeting(mode: BuildingTargetingMode) {
    if (!this.getSelectedBuilding()) return
    this.unitTargetingMode = null
    this.buildingTargetingMode = mode
  }

  cancelBuildingTargeting() {
    this.buildingTargetingMode = null
    this.debugSpawnConfig = null
  }

  // Arms the 'debug-spawn-unit' building-targeting mode so the next world
  // click sends a debug_spawn_unit command with the supplied loadout. The
  // mode persists across clicks until cancelBuildingTargeting (right-click
  // or ESC) so the user can place multiple identical test subjects in a row.
  beginDebugSpawnTargeting(config: DebugSpawnConfig) {
    if (!config.unitType) return
    this.selectedUnitIds.clear()
    this.selectedUnitOrder = []
    this.selectedBuildingId = null
    this.selectedObstacleId = null
    this.selectedTrapId = null
    this.selectedZoneId = null
    this.inspectedEnemyUnitId = null
    this.inspectedAllyUnitId = null
    this.unitTargetingMode = null
    this.workerBuildMenuOpen = false
    this.buildPlacement = null
    this.buildingTargetingMode = 'debug-spawn-unit'
    this.debugSpawnConfig = { ...config }
  }

  beginUnitTargeting(mode: UnitTargetingMode) {
    if (this.getSelectedUnits().length === 0) return
    if (mode === 'gather' && !this.selectedUnitsCanGather()) return
    if (mode === 'repair' && !this.selectedUnitsCanBuild()) return

    this.selectedBuildingId = null
    this.buildingTargetingMode = null
    this.unitTargetingMode = mode
  }

  // Enter ability-cast targeting: the next friendly-unit click casts
  // abilityId. Mirrors beginUnitTargeting; selection is preserved (the
  // selected unit is the caster).
  beginAbilityTargeting(abilityId: string) {
    if (this.getSelectedUnits().length === 0) return
    this.selectedBuildingId = null
    this.buildingTargetingMode = null
    this.castAbilityId = abilityId
    this.unitTargetingMode = 'cast-ability'
  }

  // Enter focus-target targeting: the next ally click sets this unit's focus
  // target (Cleric support assignment). Mirrors beginAbilityTargeting; the
  // selected unit is the caster. A click on anything other than a valid ally
  // is handled by the input layer as a "cancel" that also clears focus.
  beginFocusTargetTargeting() {
    if (this.getSelectedUnits().length === 0) return
    this.selectedBuildingId = null
    this.buildingTargetingMode = null
    this.castAbilityId = null
    this.unitTargetingMode = 'focus-target'
  }

  cancelUnitTargeting() {
    this.unitTargetingMode = null
    this.castAbilityId = null
  }

  // Enter commander-ability targeting: the next world click casts the
  // selected commander ability at that position. Independent of unit
  // selection (the player IS the caster).
  beginCommanderTargeting(abilityId: string) {
    this.buildingTargetingMode = null
    this.unitTargetingMode = null
    this.castAbilityId = null
    this.buildPlacement = null
    this.commanderTargetingAbilityId = abilityId
  }

  cancelCommanderTargeting() {
    this.commanderTargetingAbilityId = null
  }

  isCommanderTargetingActive(abilityId?: string): boolean {
    if (!this.commanderTargetingAbilityId) return false
    return abilityId ? this.commanderTargetingAbilityId === abilityId : true
  }

  // Enter consumable-item targeting: the next world click uses the vault item
  // as a ground AoE at that position. Player-level, like commander abilities
  // — no unit selection needed. Radius is carried here so the renderer can
  // draw the AoE preview without an item-def lookup per frame.
  beginItemTargeting(instanceId: number, itemId: string, radius: number) {
    this.buildingTargetingMode = null
    this.unitTargetingMode = null
    this.castAbilityId = null
    this.buildPlacement = null
    this.commanderTargetingAbilityId = null
    this.itemTargeting = { instanceId, itemId, radius }
  }

  cancelItemTargeting() {
    this.itemTargeting = null
  }

  isItemTargetingActive(): boolean {
    return this.itemTargeting !== null
  }

  isUnitTargetingActive(mode?: UnitTargetingMode) {
    if (!this.unitTargetingMode) return false
    return mode ? this.unitTargetingMode === mode : true
  }

  isAnyTargetingActive() {
    return (
      this.isBuildingTargetingActive() ||
      this.isUnitTargetingActive() ||
      this.isCommanderTargetingActive() ||
      this.isItemTargetingActive() ||
      this.buildPlacement !== null
    )
  }

  isBuildPlacementActive(): boolean {
    return this.buildPlacement !== null
  }

  beginBuildPlacement(buildingType: string, builderUnitIds: number[]) {
    const def = BUILDING_DEF_MAP.get(buildingType)
    const gridW = def?.width ?? 2
    const gridH = def?.height ?? 2
    const { gridCols, gridRows } = this.mapConfig
    const startX = Math.max(0, Math.floor(gridCols / 2) - 1)
    const startY = Math.max(0, Math.floor(gridRows / 2) - 1)

    this.buildPlacement = {
      buildingType,
      gridW,
      gridH,
      cursorGridX: startX,
      cursorGridY: startY,
      valid: this.isBuildPlacementCellsValid(startX, startY, gridW, gridH),
      builderUnitIds,
    }
    this.workerBuildMenuOpen = false
  }

  cancelBuildPlacement() {
    this.buildPlacement = null
  }

  updateBuildPlacement(worldX: number, worldY: number) {
    if (!this.buildPlacement) return

    const { cellSize, gridCols, gridRows } = this.mapConfig
    const { gridW, gridH } = this.buildPlacement

    const rawGridX = Math.round(worldX / cellSize - gridW / 2)
    const rawGridY = Math.round(worldY / cellSize - gridH / 2)
    const gridX = Math.max(0, Math.min(rawGridX, gridCols - gridW))
    const gridY = Math.max(0, Math.min(rawGridY, gridRows - gridH))

    this.buildPlacement = {
      ...this.buildPlacement,
      cursorGridX: gridX,
      cursorGridY: gridY,
      valid: this.isBuildPlacementCellsValid(gridX, gridY, gridW, gridH),
    }
  }

  private isBuildPlacementCellsValid(gridX: number, gridY: number, gridW: number, gridH: number): boolean {
    const { gridCols, gridRows, buildings, obstacles, cellSize } = this.mapConfig

    if (gridX < 0 || gridY < 0 || gridX + gridW > gridCols || gridY + gridH > gridRows) {
      return false
    }

    // Reject cells whose Wang-tile visual is a transition (cliff/slope) tile.
    // Mirrors the server's addTerrainBlocks: those cells aren't walkable and
    // the server would refuse to pathfind through the finished building.
    for (let dy = 0; dy < gridH; dy++) {
      for (let dx = 0; dx < gridW; dx++) {
        if (isTerrainCellBlocked(this.mapConfig, gridX + dx, gridY + dy)) {
          return false
        }
      }
    }

    for (const obs of obstacles) {
      if (obs.x >= gridX && obs.x < gridX + gridW && obs.y >= gridY && obs.y < gridY + gridH) {
        return false
      }
    }

    for (const building of buildings) {
      if (!building.visible) continue

      const bRight = building.x + building.width
      const bBottom = building.y + building.height
      const pRight = gridX + gridW
      const pBottom = gridY + gridH

      if (gridX < bRight && pRight > building.x && gridY < bBottom && pBottom > building.y) {
        return false
      }
    }

    for (const unit of this.units) {
      if (!unit.visible) continue
      const ux = Math.floor(unit.x / cellSize)
      const uy = Math.floor(unit.y / cellSize)
      if (ux >= gridX && ux < gridX + gridW && uy >= gridY && uy < gridY + gridH) {
        return false
      }
    }

    return true
  }

  /** Mirrors the server zone build-gate: a footprint cell inside a zone the
   *  player's team does not control blocks the build — EXCEPT a claim zone's
   *  capture-point slot, which is buildable while unowned (that's how the team
   *  breaks in to claim it). Returns true when the placement is blocked by an
   *  uncontrolled zone, so the caller can surface "Zone not controlled". */
  buildBlockedByUnownedZone(gridX: number, gridY: number, gridW: number, gridH: number, buildingType: string): boolean {
    const zones = this.mapConfig.zones
    if (!zones?.length) return false
    for (let dy = 0; dy < gridH; dy++) {
      for (let dx = 0; dx < gridW; dx++) {
        const cx = gridX + dx
        const cy = gridY + dy
        const zone = zones.find((z) => z.cells.some(([zx, zy]) => zx === cx && zy === cy))
        if (!zone) continue
        if (this.zoneControlledByMyTeam(this.zoneSnapshotsById.get(zone.id)?.owner)) continue // my team controls it
        if (this.isBuildableClaimSlotCell(zone, cx, cy, buildingType)) continue // claim-slot exception
        return true
      }
    }
    return false
  }

  /** Whether a zone owner string means "my team controls this zone". The team
   *  sentinel or a friendly real player count; the neutral/enemy sentinels and
   *  empty never do. Mirrors ownerIsTeam in zoneCaptureCards. */
  private zoneControlledByMyTeam(owner: string | undefined): boolean {
    if (!owner) return false
    if (owner === ZONE_TEAM_OWNER) return true
    if (owner === 'neutral' || owner === ENEMY_PLAYER_ID || owner === NEUTRAL_PLAYER_ID) return false
    return this.isFriendlyOwnerForZone(owner)
  }

  /** True when (cx, cy) is a buildable claim capture-point slot for buildingType.
   *  Mirrors the server claimSlotBuildableLocked: the cell is in a claim zone's
   *  2x2 slot and (if a towerType is configured) the building is that tower. */
  private isBuildableClaimSlotCell(zone: Zone, cx: number, cy: number, buildingType: string): boolean {
    if (zone.capture?.type !== 'claim') return false
    const towerType = (zone.capture.config?.['towerType'] as string | undefined) ?? ''
    if (towerType && buildingType !== towerType) return false
    const points = (zone.claimPoints?.length ?? 0) > 0 ? zone.claimPoints! : [[zone.anchor.x, zone.anchor.y]]
    return points.some(([ax, ay]) => cx >= ax && cx <= ax + 1 && cy >= ay && cy <= ay + 1)
  }

  isBuildingTargetingActive(mode?: BuildingTargetingMode) {
    if (!this.buildingTargetingMode) return false
    return mode ? this.buildingTargetingMode === mode : true
  }

  getTargetedBuildingSpawnPoint(worldX: number, worldY: number): Vec2 | null {
    const building = this.getSelectedBuilding()
    if (!building || this.buildingTargetingMode !== 'set-spawn-point') return null
    return clampBuildingSpawnPoint(this.mapConfig, building, { x: worldX, y: worldY })
  }

  getSelectedBuildingSpawnPointTarget(worldX: number, worldY: number): Vec2 | null {
    const building = this.getSelectedBuilding()
    if (!building) return null
    return clampBuildingSpawnPoint(this.mapConfig, building, { x: worldX, y: worldY })
  }

  getBuildingSpawnPoint(building: BuildingTile): Vec2 | null {
    const x = getBuildingMetadataNumber(building, 'spawnPointX')
    const y = getBuildingMetadataNumber(building, 'spawnPointY')
    if (x === undefined || y === undefined) return null
    return { x, y }
  }

  setHoveredInteractableBuilding(buildingId: string | null) {
    this.hoveredInteractableBuildingId = buildingId
  }

  setHoveredLootDrop(lootDropId: string | null) {
    this.hoveredLootDropId = lootDropId
  }

  setHoveredEnemyUnit(unitId: number | null) {
    this.hoveredEnemyUnitId = unitId
  }

  // Mirrors setHoveredEnemyUnit for the friendly-targeting cursor modes
  // (cast-ability, focus-target). Called from InputManager each frame the
  // pointer is over a same-team unit and a friendly-targeting mode is active.
  setHoveredFriendlyUnit(unitId: number | null) {
    this.hoveredFriendlyUnitId = unitId
  }

  setCursorWorld(x: number, y: number) {
    this.cursorWorldX = x
    this.cursorWorldY = y
  }

  setCursorScreen(x: number, y: number) {
    this.cursorScreenX = x
    this.cursorScreenY = y
  }

  setCursorClient(x: number, y: number) {
    this.cursorClientX = x
    this.cursorClientY = y
  }

  inspectEnemyUnit(unitId: number) {
    const unit = this.units.find((u) => u.id === unitId)
    if (!unit || !this.isHostileToLocalPlayer(unit.ownerId)) return

    this.selectedUnitIds.clear()
    this.selectedUnitOrder = []
    this.selectedBuildingId = null
    this.selectedObstacleId = null
    this.selectedZoneId = null
    this.inspectedEnemyUnitId = unitId
    this.inspectedAllyUnitId = null
    this.buildingTargetingMode = null
    this.unitTargetingMode = null
    this.workerBuildMenuOpen = false
    this.buildPlacement = null
  }

  // Read-only inspection of an allied other-player unit. Mirrors
  // inspectEnemyUnit; the UI surfaces stats/perks but no order buttons.
  inspectAllyUnit(unitId: number) {
    const unit = this.units.find((u) => u.id === unitId)
    if (!unit || !this.isAlliedOtherPlayerUnit(unit)) return

    this.selectedUnitIds.clear()
    this.selectedUnitOrder = []
    this.selectedBuildingId = null
    this.selectedObstacleId = null
    this.selectedTrapId = null
    this.selectedZoneId = null
    this.inspectedEnemyUnitId = null
    this.inspectedAllyUnitId = unitId
    this.buildingTargetingMode = null
    this.unitTargetingMode = null
    this.workerBuildMenuOpen = false
    this.buildPlacement = null
  }

  selectedUnitsCanAttack(): boolean {
    const units = this.getSelectedUnits()
    return units.length > 0 && units.some((unit) => unit.capabilities.includes('attack'))
  }

  /**
   * Called on every match_snapshot. Replaces the buildings array in the stored
   * mapConfig with the incoming one, then invalidates any selection that refers
   * to a building that is no longer present or is no longer visible.
   * Terrain, obstacles, and map dimensions are static after welcome and are not
   * touched here.
   */
  private mergeSnapshotBuildings(buildings: BuildingTile[]) {
    this.mapConfig = { ...this.mapConfig, buildings }
    this.invalidateBuildingSelection()
  }

  /**
   * Called on every match_snapshot. Applies obstacle deltas — IDs removed
   * since the previous broadcast, and metadata patches (currently only
   * tree currentWorkers updates) — to the locally-mirrored obstacle list.
   *
   * The full obstacle geometry is sent ONCE in the WelcomeMessage's
   * MapConfig (consumed by setMapConfig). Subsequent snapshots only ship
   * the changes. Both deltas are absent on the overwhelming majority of
   * ticks (no tree was chopped, no worker entered/left), so this is a
   * near-no-op in the steady state.
   *
   * Both arguments are optional: an absent removed list means "nothing
   * disappeared this tick", and an absent metadata list means "no live
   * counter changed". Removals on IDs the client doesn't have are silent
   * no-ops (idempotent against double-delivery / out-of-order applies);
   * metadata patches for unknown IDs are also dropped.
   */
  private applyObstacleDeltas(
    removed: string[] | undefined,
    metadata: ObstacleMetadataPatch[] | undefined,
  ) {
    const hasRemoved = removed !== undefined && removed.length > 0
    const hasMetadata = metadata !== undefined && metadata.length > 0
    if (!hasRemoved && !hasMetadata) {
      return
    }

    let obstacles = this.mapConfig.obstacles

    if (hasRemoved) {
      const removedSet = new Set(removed)
      obstacles = obstacles.filter((o) => !(o.id !== undefined && removedSet.has(o.id)))
    }

    if (hasMetadata) {
      // Build an id→patch map once, then walk obstacles once. Patches for
      // unknown ids are silently ignored — same as removed-IDs above.
      const patchById = new Map<string, ObstacleMetadataPatch>()
      for (const p of metadata!) patchById.set(p.id, p)
      obstacles = obstacles.map((o) => {
        if (o.id === undefined) return o
        const p = patchById.get(o.id)
        if (!p) return o
        // Preserve every other metadata key the welcome sent; only patch
        // currentWorkers. Spread keeps maxWorkers (constant, set at welcome)
        // and any future per-obstacle annotations intact.
        return {
          ...o,
          metadata: { ...(o.metadata ?? {}), currentWorkers: p.currentWorkers },
        }
      })
    }

    this.mapConfig = { ...this.mapConfig, obstacles }
    this.invalidateObstacleSelection()
  }

  /**
   * Clears selectedBuildingId (and the dependent targeting mode) when the
   * selected building has been removed or hidden in the latest snapshot.
   * Extracted so it can be called after either a full setMapConfig or a
   * lightweight mergeSnapshotBuildings.
   */
  private invalidateBuildingSelection() {
    if (
      this.selectedBuildingId &&
      !this.mapConfig.buildings.some(
        (building) => building.id === this.selectedBuildingId && building.visible && !building.ghost,
      )
    ) {
      this.selectedBuildingId = null
      this.buildingTargetingMode = null
    }
  }

  // Mirrors invalidateBuildingSelection for obstacles: clears the selection
  // when the selected obstacle (e.g. a tree that just got depleted) no longer
  // exists on the map.
  private invalidateObstacleSelection() {
    if (
      this.selectedObstacleId &&
      !this.mapConfig.obstacles.some((obstacle) => obstacle.id === this.selectedObstacleId)
    ) {
      this.selectedObstacleId = null
    }
  }

  setMapConfig(map: MapConfig) {
    this.mapConfig = sanitizeMapConfig(map)
    this.mapWidth = this.mapConfig.width
    this.mapHeight = this.mapConfig.height
    this.neutralShopPOIs = getShopPOIs(this.mapConfig.buildings)
    this.invalidateBuildingSelection()
    this.invalidateZoneCellIndex()
  }

  getLocalPlayerUnits(): Unit[] {
    if (!this.localPlayerId) return []
    return this.units.filter((unit) => unit.ownerId === this.localPlayerId)
  }

  // Returns the Shop catalog for the local player: one entry per distinct
  // item stocked by any shop the player can currently buy from. Eligible
  // shops are:
  //   - Player-owned buildings with the "item-purchase" capability that
  //     are not under construction.
  //   - Neutral-owned shop buildings that have been discovered by the
  //     player (shopDiscovered === true) and are not guard-locked
  //     (shopLocked !== true).
  // Items are deduped across shops by itemId; the first eligible building
  // (player-owned shops scanned first) is the one used for the purchase
  // call. Unavailable items are not emitted — the shop tab no longer
  // renders lock icons.
  getShopCatalogSnapshot(): ShopCatalogEntry[] {
    const entries: ShopCatalogEntry[] = []

    if (!this.localPlayerId) {
      return entries
    }

    // Player-owned shops first so duplicates resolve to a player building.
    // capabilities can arrive as null/undefined for partial building tiles
    // (e.g. freshly placed ghosts before the server fills in the def).
    // Optional-chaining the call guards against a TypeError that would
    // otherwise propagate out of the snapshot builder and freeze the UI.
    const eligibleBuildings: BuildingTile[] = []
    for (const b of this.mapConfig.buildings) {
      if (!b.capabilities?.includes('item-purchase')) continue
      if (b.metadata?.['underConstruction'] === true) continue
      if (b.ownerId !== this.localPlayerId) continue
      eligibleBuildings.push(b)
    }
    for (const b of this.mapConfig.buildings) {
      if (!b.capabilities?.includes('item-purchase')) continue
      if (b.metadata?.['underConstruction'] === true) continue
      if (b.ownerId === this.localPlayerId) continue
      // Neutral shop access requires both discovery and an unlocked state.
      if (b.shopDiscovered !== true) continue
      if (b.shopLocked === true) continue
      eligibleBuildings.push(b)
    }

    for (const b of eligibleBuildings) {
      const inventory = b.shopInventory ?? []
      // Dedup per building, not globally: each shop card must reflect its own
      // full inventory even when items overlap another shop's stock. (A global
      // dedup attributed shared items to whichever shop came first, making the
      // Shop menu under-report a merchant's stock vs. its in-world panel.)
      const seenItems = new Set<string>()
      for (const slot of inventory) {
        if (seenItems.has(slot.itemId)) continue
        const def = ITEM_DEF_MAP.get(slot.itemId)
        if (!def) continue
        seenItems.add(slot.itemId)
        entries.push({
          itemId: def.id,
          displayName: def.displayName,
          description: def.description,
          iconKey: def.iconKey,
          kind: def.kind,
          tier: def.tier,
          costGold: def.costGold,
          quantity: slot.quantity,
          purchaseBuildingId: b.id,
          purchaseBuildingType: b.buildingType,
          purchaseBuildingStyle: b.metadata?.['shopStyle'] as string | undefined,
          // Prefer the assigned item list's name (e.g. "Wandering Merchant");
          // fall back to the building type label.
          purchaseBuildingName: b.shopDisplayName || formatBuildingName(b.buildingType),
        })
      }
    }

    // Recipe Shops sell *knowledge*, not items — a separate capability
    // ('recipe-purchase') and a separate purchase command. They surface on the
    // same Shop tab once their contents are available, gated identically to
    // neutral item shops: discovered in FOW and not guard-locked.
    const knownRecipes = new Set(this.localPlayerUnlockedRecipeIds)
    for (const b of this.mapConfig.buildings) {
      if (!b.capabilities?.includes('recipe-purchase')) continue
      if (b.metadata?.['underConstruction'] === true) continue
      // Recipe shops are always neutral-owned; require discovery + unlocked.
      if (b.ownerId === this.localPlayerId) continue
      if (b.shopDiscovered !== true) continue
      if (b.shopLocked === true) continue

      const seenRecipes = new Set<string>()
      for (const slot of b.recipeInventory ?? []) {
        if (seenRecipes.has(slot.recipeId)) continue
        const recipe = RECIPE_DEF_MAP.get(slot.recipeId)
        if (!recipe) continue
        seenRecipes.add(slot.recipeId)
        // Rarity-keyed recipe scroll icon (matching the in-world panel): prefer
        // ${rarity}_recipe, fall back to rare_recipe when the tier asset is absent.
        const rarity = recipe.rarity ?? 'common'
        const rarityIconKey = `${rarity}_recipe`
        const recipeIconKey = hasItemAsset(rarityIconKey) ? rarityIconKey : 'rare_recipe'
        const need = new Map<string, number>()
        for (const input of recipe.inputs) need.set(input, (need.get(input) ?? 0) + 1)
        const inputLines = [...need].map(([itemId, count]) => {
          const name = ITEM_DEF_MAP.get(itemId)?.displayName ?? itemId
          return count > 1 ? `${name} ×${count}` : name
        })
        entries.push({
          entryType: 'recipe',
          itemId: recipe.id,
          displayName: `Recipe: ${recipe.name}`,
          description: `Requires: ${inputLines.join(', ')}`,
          iconKey: recipeIconKey,
          kind: 'equipment', // placeholder — recipe entries render via entryType
          // recipe.rarity is a loose string from the catalog; it is always one
          // of the tier folders, so narrow it to the ShopCatalogEntry union.
          tier: rarity as ShopCatalogEntry['tier'],
          // The shelf price of a recipe is what it costs to LEARN it, not the
          // craft cost the Artificer will charge later (recipe.costGold). The
          // server charges UnlockCostGold here — keep the two in step.
          costGold: recipe.unlockCostGold ?? 0,
          quantity: slot.quantity,
          recipeKnown: knownRecipes.has(recipe.id),
          purchaseBuildingId: b.id,
          purchaseBuildingType: b.buildingType,
          purchaseBuildingStyle: b.metadata?.['shopStyle'] as string | undefined,
          purchaseBuildingName: b.shopDisplayName || formatBuildingName(b.buildingType),
        })
      }
    }

    entries.sort((a, b) => a.costGold - b.costGold || a.displayName.localeCompare(b.displayName))
    return entries
  }

  // localPlayerHasArtificer reports whether the local player owns at least one
  // fully-built (not under-construction) building with the "crafting"
  // capability — the client-side mirror of the server's craft gate. Used to
  // gate the Craft tab's buttons + empty-state hint.
  localPlayerHasArtificer(): boolean {
    if (!this.localPlayerId) return false
    for (const b of this.mapConfig.buildings) {
      if (b.ownerId !== this.localPlayerId) continue
      if (b.metadata?.['underConstruction'] === true) continue
      if (b.capabilities?.includes('crafting')) return true
    }
    return false
  }

  // getCraftCatalogSnapshot builds the Craft tab's data from the player's
  // unlocked recipes: per recipe, the ingredient have/need (have summed from the
  // Vault, need counted from recipe.inputs incl. duplicates) and whether it is
  // craftable right now. Server re-validates on craft_item — this is a UX hint.
  getCraftCatalogSnapshot(): CraftCatalogEntry[] {
    const hasArtificer = this.localPlayerHasArtificer()
    const have = new Map<string, number>()
    for (const vi of this.localPlayerVault) {
      have.set(vi.itemId, (have.get(vi.itemId) ?? 0) + (vi.stacks ?? 1))
    }
    const entries: CraftCatalogEntry[] = []
    // Deterministic order: iterate the sorted unlocked ids.
    for (const recipeId of [...this.localPlayerUnlockedRecipeIds].sort()) {
      const recipe = RECIPE_DEF_MAP.get(recipeId)
      if (!recipe) continue
      const need = new Map<string, number>()
      for (const input of recipe.inputs) need.set(input, (need.get(input) ?? 0) + 1)
      let allPresent = true
      const ingredients: CraftCatalogIngredient[] = []
      for (const input of recipe.inputs) {
        if (ingredients.some((i) => i.itemId === input)) continue // dedup display
        const needCount = need.get(input) ?? 0
        const haveCount = have.get(input) ?? 0
        if (haveCount < needCount) allPresent = false
        ingredients.push({ itemId: input, have: haveCount, need: needCount })
      }
      entries.push({
        recipeId: recipe.id,
        name: recipe.name,
        output: recipe.output,
        costGold: recipe.costGold,
        ingredients,
        craftable: hasArtificer && allPresent,
      })
    }
    entries.sort((a, b) => a.name.localeCompare(b.name) || a.recipeId.localeCompare(b.recipeId))
    return entries
  }

  getSelectedUnits(): Unit[] {
    const selectedIds = this.getOrderedSelectedUnitIds()

    return selectedIds
      .map((id) => this.units.find((unit) => unit.id === id))
      .filter((unit): unit is Unit => !!unit)
  }

  selectedUnitsCanGather(): boolean {
    const units = this.getSelectedUnits()
    return units.length > 0 && units.every((unit) => unit.capabilities.includes('gather'))
  }

  // True when at least one selected unit is currently carrying a resource —
  // gates the deposit hover/right-click on owned deposit-point buildings.
  selectedUnitsHaveCarriedResource(): boolean {
    return this.getSelectedUnits().some(
      (unit) => (unit.carriedAmount ?? 0) > 0 && !!unit.carriedResourceType,
    )
  }

  // Subset of the ordered selection that is currently carrying a resource. Used
  // by the deposit right-click flow so only carriers receive the deposit order;
  // non-carriers fall through to a normal move command.
  getSelectedCarrierUnitIds(): number[] {
    const carrying = new Set(
      this.getSelectedUnits()
        .filter((unit) => (unit.carriedAmount ?? 0) > 0 && !!unit.carriedResourceType)
        .map((unit) => unit.id),
    )
    return this.getOrderedSelectedUnitIds().filter((id) => carrying.has(id))
  }

  selectedUnitsCanBuild(): boolean {
    const units = this.getSelectedUnits()
    return units.length > 0 && units.some((unit) => unit.capabilities.includes('build'))
  }

  hasUnderConstructionBuildings(): boolean {
    return this.mapConfig.buildings.some(
      (b) => b.ownerId === this.localPlayerId && b.metadata?.['underConstruction'] === true,
    )
  }

  getSelectionSummary(): SelectionSummary {
    if (this.inspectedEnemyUnitId !== null && this.selectedUnitIds.size === 0) {
      const unit = this.units.find((u) => u.id === this.inspectedEnemyUnitId)
      if (unit) {
        return {
          kind: 'unit',
          title: `Enemy ${unit.name}`,
          subtitle: unit.status ?? 'Hostile',
          details: getUnitDetails(unit),
          actions: [],
          pathLabel: unit.path && unit.path !== 'none' ? formatUnitPath(unit.path) : undefined,
          rankLabel: formatUnitRank(unit.rank),
          xpLabel: getUnitXpLabel(unit),
        }
      }
      this.inspectedEnemyUnitId = null
    }

    if (this.inspectedAllyUnitId !== null && this.selectedUnitIds.size === 0) {
      const unit = this.units.find((u) => u.id === this.inspectedAllyUnitId)
      if (unit) {
        // Read-only: actions stays empty so no order buttons render.
        return {
          kind: 'unit',
          title: `Ally ${unit.name}`,
          subtitle: unit.status ?? 'Allied',
          details: getUnitDetails(unit),
          actions: [],
          pathLabel: unit.path && unit.path !== 'none' ? formatUnitPath(unit.path) : undefined,
          rankLabel: formatUnitRank(unit.rank),
          xpLabel: getUnitXpLabel(unit),
        }
      }
      this.inspectedAllyUnitId = null
    }

    const selectedObstacle = this.getSelectedObstacle()
    if (selectedObstacle) {
      return {
        kind: 'building',
        title: formatObstacleName(selectedObstacle.obstacle),
        subtitle: getObstacleSubtitle(selectedObstacle),
        details: getObstacleDetails(selectedObstacle),
        actions: [],
      }
    }

    const selectedTrap = this.getSelectedTrap()
    if (selectedTrap) {
      return {
        kind: 'building',
        title: formatTrapName(selectedTrap.type),
        subtitle: selectedTrap.ownerId
          ? selectedTrap.ownerId === this.localPlayerId
            ? 'Your Trap'
            : `Enemy Trap (${selectedTrap.ownerId})`
          : 'Trap',
        details: getTrapDetails(selectedTrap),
        actions: [],
      }
    }

    const selectedBuilding = this.getSelectedBuilding()
    if (selectedBuilding) {
      const buildingTier = getBuildingMetadataNumber(selectedBuilding, 'tier') ?? 1
      const title = formatBuildingName(selectedBuilding.buildingType, buildingTier)
      const activeProduction = getBuildingProductionState(selectedBuilding)
      // A blacksmith researching an upgrade shows the same production card as a
      // training building (track portrait + progress + countdown), but cannot
      // be cancelled. Only consulted when no real unit production is active.
      const upgradeProduction = activeProduction ? null : getBuildingUpgradeState(selectedBuilding)
      const isUnderConstruction = selectedBuilding.metadata?.['underConstruction'] === true
      const defaultSubtitle = selectedBuilding.ownerId
        ? `Owned by ${selectedBuilding.ownerId}`
        : selectedBuilding.occupied
          ? 'Occupied'
          : 'Neutral'
        const subtitle = isUnderConstruction
          ? 'Under Construction'
          : this.buildingTargetingMode === 'set-spawn-point'
            ? 'Click anywhere on the map to set the rally point target.'
            : activeProduction || upgradeProduction
              // While training (or researching an upgrade), the visual
              // production card conveys what's happening; suppress the textual
              // subtitle so it doesn't duplicate the cards.
              ? ''
              : defaultSubtitle

      return {
        kind: 'building',
        title,
        subtitle,
        details: getBuildingDetails(selectedBuilding),
        actions: isUnderConstruction
          ? (selectedBuilding.ownerId === this.localPlayerId
              ? getUnderConstructionActions(selectedBuilding)
              : [])
          : getBuildingActions(
              selectedBuilding,
              this.playerUpgrades,
              { vault: this.localPlayerVault },
              this.townHallTier,
              new Set(this.lockedUnitTypes),
              this.localPlayerShopRerollsRemaining,
              new Set(this.localPlayerUnlockedRecipeIds),
              this.localPlayerUnitCostOverrides,
            ),
        production: activeProduction
          ? toProductionSummary(activeProduction)
          : upgradeProduction
            ? { ...toProductionSummary(upgradeProduction), cancelActionId: 'cancel-upgrade' }
            : undefined,
        construction: isUnderConstruction
          ? getBuildingConstructionSummary(selectedBuilding)
          : undefined,
      }
    }

    const selectedUnits = this.getSelectedUnits()
    if (selectedUnits.length === 0) {
      return {
        kind: 'none',
        title: 'No Selection',
        subtitle: 'Select a unit or building to inspect details and actions.',
        details: [],
        actions: [],
      }
    }

    if (selectedUnits.length === 1) {
      const unit = selectedUnits[0]
      const buildMenuOpen = this.workerBuildMenuOpen && unit.capabilities.includes('build')
      const placementActive = this.buildPlacement !== null

      // Regular actions occupy slots 1–8 (top two rows of the 4×3 grid).
      // Perk items always land in slots 9–11 (bottom row, left side):
      // bronze, silver, gold — slot 12 is left empty for future use.
      // When the build menu is open the full 12 slots are used for building
      // choices, so we skip the perk row in that state.
      const regularActions = getUnitActions(unit, this.unitTargetingMode, buildMenuOpen, this.townHallTier)
      // Interactive ability buttons follow the standard actions, sharing the
      // top-8 slots. Empty for non-caster units / when the build menu is open.
      const abilityActions = buildMenuOpen
        ? []
        : getAbilityActionItems(unit, this.unitTargetingMode, this.castAbilityId)
      // Focus Target action — only for units that can heal an ally. Appears
      // alongside heal/greater_heal in the action bar so the support kit
      // reads as one group.
      const focusActions =
        !buildMenuOpen && unitOwnsHealAbility(unit)
          ? [buildFocusTargetActionItem([unit], this.unitTargetingMode)]
          : []
      const topActions = [...regularActions, ...abilityActions, ...focusActions]
      const emptySlot: ActionItem = { id: '', label: '', disabled: true }
      const perkActions = getPerkActionItems(unit, this.unitTargetingMode, this.castAbilityId)
      const actions = buildMenuOpen
        ? regularActions
        : [
            ...topActions,
            // Pad to 8 so perks always land starting at slot 9 (bottom-left)
            // regardless of how many action/ability slots are filled.
            ...Array<ActionItem>(Math.max(0, 8 - topActions.length)).fill(emptySlot),
            ...perkActions,
            // When perkActions has length 3 (no extra slot), pad slot 12 with an
            // empty cell. When length 4 (Twin Bronze granted), the 4th cell IS
            // slot 12.
            ...(perkActions.length < 4 ? [emptySlot] : []),
          ]

      return {
        kind: 'unit',
        title: unit.name,
        subtitle: placementActive
          ? 'Click to place the Barracks. Right-click to cancel.'
          : buildMenuOpen
            ? 'Choose a structure to build.'
            : getSelectionUnitSubtitle(
                unit.order
                  ? `Order: ${formatUnitOrder(unit.order)}`
                  : unit.status || formatUnitType(unit.unitType),
                this.unitTargetingMode,
              ),
        details: getUnitDetails(unit),
        actions,
        pathLabel: unit.path && unit.path !== 'none' ? formatUnitPath(unit.path) : undefined,
        rankLabel: formatUnitRank(unit.rank),
        xpLabel: getUnitXpLabel(unit),
      }
    }

    const groupBuildMenuOpen =
      this.workerBuildMenuOpen && selectedUnits.every((u) => u.capabilities.includes('build'))

    return {
      kind: 'group',
      title: `${selectedUnits.length} Units Selected`,
      subtitle: groupBuildMenuOpen
        ? 'Choose a structure to build.'
        : getSelectionUnitSubtitle(
            selectedUnits.every((unit) => unit.unitType === 'worker')
              ? summarizeWorkerGroupStatus(selectedUnits)
              : 'Mixed Detachment',
            this.unitTargetingMode,
          ),
      details: getGroupDetails(selectedUnits),
      actions: getGroupActions(selectedUnits, this.unitTargetingMode, groupBuildMenuOpen, this.townHallTier, this.castAbilityId),
    }
  }

  getLocalPlayerSpawnCenter(): Vec2 | null {
    const localUnits = this.getLocalPlayerUnits()
    if (localUnits.length === 0) return null

    const totals = localUnits.reduce(
      (acc, unit) => {
        acc.x += unit.x
        acc.y += unit.y
        return acc
      },
      { x: 0, y: 0 },
    )

    return {
      x: totals.x / localUnits.length,
      y: totals.y / localUnits.length,
    }
  }

  getPlayerSummary(): PlayerSummary {
    const localUnits = this.getLocalPlayerUnits()

    return {
      playerId: this.localPlayerId,
      color:
        (this.localPlayerId ? this.playerColors.get(this.localPlayerId) : null) ??
        localUnits[0]?.color ??
        null,
      totalUnits: localUnits.length,
      selectedUnits: this.selectedUnitIds.size,
      totalHp: localUnits.reduce((sum, unit) => sum + (unit.hp ?? 0), 0),
      resources: this.resourceStocks.map((resource) => ({ ...resource })),
    }
  }

  getWaveSnapshot(): WaveSnapshot {
    return this.waveSnapshot
  }

  isLocalPlayerDefeated(): boolean {
    if (!this.localPlayerId || !this.gameOverSnapshot) return false
    return this.gameOverSnapshot.lostPlayerIds.includes(this.localPlayerId)
  }

  isVictoryAchieved(): boolean {
    return this.victorySnapshot?.achieved === true
  }

  getObjectives(): ObjectiveSnapshot[] {
    return this.victorySnapshot?.objectives ?? []
  }

  /** A unit owner counts as "my team" when it's the local player or an allied
   *  player on the same team — never the enemy/neutral AI. Mirrors the server
   *  playersAreFriendly chokepoint for the zone-capture HUD. */
  private isFriendlyOwnerForZone(ownerId: string | undefined): boolean {
    if (!ownerId || ownerId === ENEMY_PLAYER_ID || ownerId === NEUTRAL_PLAYER_ID) return false
    if (!this.localPlayerId) return false
    if (ownerId === this.localPlayerId) return true
    return this.teamOf(ownerId) === this.teamOf(this.localPlayerId)
  }

  /** Zone HUD cards: capture-requirement cards for zones my team is contesting,
   *  plus an always-on card (with granted bonuses) for every zone my team owns.
   *  Drives ZoneCapturePanel. Empty when no zones qualify. */
  getZoneCaptureCards(): ZoneCaptureCard[] {
    // Called every UI tick — skip all work on maps without zones (the common
    // case). buildZoneCaptureCards accepts BuildingTile (null owner ids) directly.
    if (!this.mapConfig.zones?.length) return []
    return buildZoneCaptureCards({
      zones: this.mapConfig.zones,
      snapshotsById: this.zoneSnapshotsById,
      units: this.units,
      buildings: this.mapConfig.buildings,
      cellSize: this.mapConfig.cellSize,
      isFriendlyOwner: (o) => this.isFriendlyOwnerForZone(o),
      isHostileOwner: (o) => this.isHostileToLocalPlayer(o),
    })
  }

  getPlayerColor(playerId: string | null | undefined): string | null {
    if (!playerId) return null
    return this.playerColors.get(playerId) ?? null
  }

  /** Builds the inspection view-model for the currently-selected zone, or
   *  null when no zone is selected or the selected zone id is stale. */
  getZoneInspection(): ZoneInspectionInfo | null {
    const zoneId = this.selectedZoneId
    if (!zoneId) return null
    const zone = this.mapConfig.zones?.find((z) => z.id === zoneId)
    if (!zone) return null
    const snap = this.zoneSnapshotsById.get(zoneId)

    const owner = snap?.owner ?? ''
    let ownerLabel: string
    let ownerColor: string | null = null

    if (!owner || owner === 'neutral') {
      ownerLabel = 'Unclaimed'
    } else if (owner === ZONE_TEAM_OWNER) {
      ownerLabel = 'Team'
      // ownerColor from the snapshot is the lowest-slot player's color,
      // resolved server-side for team-owned zones.
      ownerColor = snap?.ownerColor && snap.ownerColor.length > 0 ? snap.ownerColor : null
    } else {
      // Individual player owner — use their color from the registry.
      ownerLabel = owner
      ownerColor = snap?.ownerColor && snap.ownerColor.length > 0
        ? snap.ownerColor
        : (this.playerColors.get(owner) ?? null)
    }

    // Only expose stat_modifier auras; unknown types are quietly skipped so
    // the panel stays correct if the server adds new aura types in future.
    const auras = (zone.auras ?? []).filter((a) => a.type === ZONE_AURA_TYPE_STAT_MODIFIER)

    return {
      zoneId,
      name: zone.name || zone.id,
      ownerLabel,
      ownerColor,
      auras,
    }
  }

  private applyPlayerSnapshots(players: PlayerSnapshot[]) {
    this.playerSnapshots = players
    this.playerColors = new Map(players.map((player) => [player.playerId, player.color]))
    this.playerTeams = new Map(players.map((player) => [player.playerId, player.teamId ?? 0]))

    if (!this.localPlayerId) return

    const localPlayer = players.find((player) => player.playerId === this.localPlayerId)
    if (!localPlayer) return

    this.resourceStocks = localPlayer.resources.map((resource) => ({
      id: resource.id,
      label: resource.label,
      amount: resource.amount,
      max: resource.max,
      accent: resource.accent,
    }))

    if (localPlayer.upgrades !== undefined) {
      this.playerUpgrades = localPlayer.upgrades
    }
    // Server uses `omitempty` for lockedUnitTypes, so an empty locked set
    // arrives as undefined — that means "nothing locked right now", not
    // "no data yet". The else branch must reset the field; without it the
    // train button stays greyed after the player completes a Blacksmith.
    // Don't harmonize this with the surrounding `if (... !== undefined)`
    // blocks that legitimately preserve last-known state.
    if (localPlayer.lockedUnitTypes !== undefined) {
      this.lockedUnitTypes = localPlayer.lockedUnitTypes
    } else {
      this.lockedUnitTypes = []
    }
    // Like lockedUnitTypes, the server omits this when empty, so undefined
    // means "no cost overrides", not "no data yet" — always rebuild the map so
    // a refund/reset that removes an advancement mid-match clears stale prices.
    this.localPlayerUnitCostOverrides = new Map(
      (localPlayer.unitCostOverrides ?? []).map((o) => [o.unitType, o]),
    )
    if (localPlayer.townHallTier !== undefined) {
      this.townHallTier = localPlayer.townHallTier
    }
    if (localPlayer.vault !== undefined) {
      this.localPlayerVault = localPlayer.vault ?? []
    }
    this.localPlayerShopRerollsRemaining = localPlayer.shopRerollsRemaining ?? 0
    this.localPlayerUnlockedRecipeIds = localPlayer.unlockedRecipeIds ?? []
    this.localPlayerCommanderAbilities = localPlayer.commanderAbilities ?? []
  }
}

function buildFormationDestinations(units: Unit[], anchor: Vec2, spacing: number): Vec2[] {
  if (units.length === 0) return []
  if (units.length === 1) return [anchor]

  const center = getUnitCenter(units)
  let forwardX = anchor.x - center.x
  let forwardY = anchor.y - center.y
  let forwardLength = Math.hypot(forwardX, forwardY)

  if (forwardLength < 0.001) {
    forwardX = 0
    forwardY = 1
    forwardLength = 1
  }

  forwardX /= forwardLength
  forwardY /= forwardLength

  const rightX = forwardY
  const rightY = -forwardX
  const cols = Math.ceil(Math.sqrt(units.length))
  const rows = Math.ceil(units.length / cols)
  const totalWidth = (cols - 1) * spacing
  const totalHeight = (rows - 1) * spacing
  const slots = units.map((_, index) => {
    const col = index % cols
    const row = Math.floor(index / cols)
    const rightOffset = col * spacing - totalWidth / 2
    const forwardOffset = row * spacing - totalHeight / 2

    return {
      x: anchor.x + rightX * rightOffset + forwardX * forwardOffset,
      y: anchor.y + rightY * rightOffset + forwardY * forwardOffset,
    }
  })

  const unitOrder = units
    .map((unit, index) => {
      const relativeX = unit.x - center.x
      const relativeY = unit.y - center.y

      return {
        index,
        right: relativeX * rightX + relativeY * rightY,
        forward: relativeX * forwardX + relativeY * forwardY,
      }
    })
    .sort((a, b) => {
      if (Math.abs(a.forward - b.forward) > 8) {
        return a.forward - b.forward
      }

      return a.right - b.right
    })

  const slotOrder = slots
    .map((slot, index) => {
      const relativeX = slot.x - anchor.x
      const relativeY = slot.y - anchor.y

      return {
        index,
        right: relativeX * rightX + relativeY * rightY,
        forward: relativeX * forwardX + relativeY * forwardY,
      }
    })
    .sort((a, b) => {
      if (Math.abs(a.forward - b.forward) > 8) {
        return a.forward - b.forward
      }

      return a.right - b.right
    })

  const targets = new Array<Vec2>(units.length)
  for (let i = 0; i < units.length; i++) {
    targets[unitOrder[i].index] = slots[slotOrder[i].index]
  }

  return targets
}

function getUnitCenter(units: Unit[]): Vec2 {
  const totals = units.reduce(
    (acc, unit) => {
      acc.x += unit.x
      acc.y += unit.y
      return acc
    },
    { x: 0, y: 0 },
  )

  return {
    x: totals.x / units.length,
    y: totals.y / units.length,
  }
}

function formatUnitType(unitType: UnitType): string {
  const def = UNIT_DEF_MAP.get(unitType)
  return def ? `${def.name} Unit` : unitType
}

function formatUnitOrder(order: string): string {
  return UNIT_ORDER_LABELS[order as UnitOrder] ?? order
}

function formatBuildingName(buildingType: string, tier?: number): string {
  // Tiered buildings (townhall → keep → castle) display their current tier's
  // label. A placed building keeps its base buildingType plus a numeric `tier`
  // in metadata, so resolve the tier's label from the upgrade chain.
  if (tier !== undefined && tier > 1) {
    const tierLabel = getUpgradeChain(buildingType)[tier - 1]?.label
    if (tierLabel) return tierLabel
  }
  // Prefer the display label from the building def map (covers buildable types
  // including those referenced by RequiresBuildings).
  const defLabel = BUILDING_DEF_MAP.get(buildingType)?.label
  if (defLabel) return defLabel
  // Fallback to legacy switch for well-known non-buildable types.
  switch (buildingType) {
    case 'goldmine':
      return 'Goldmine'
    case 'townhall':
      return 'Townhall'
    case 'barracks':
      return 'Barracks'
    case 'farm':
      return 'Farm'
    case 'enemy-spawnpoint':
      return 'Enemy Spawnpoint'
    case 'spawn-point':
      return 'Rally Point'
    default:
      if (!buildingType) return ''
      return buildingType.charAt(0).toUpperCase() + buildingType.slice(1)
  }
}

function formatResourceLabel(resourceType: ResourceType) {
  switch (resourceType) {
    case 'gold':
      return 'Gold'
    case 'wood':
      return 'Wood'
  }
}

function getBuildingConstructionSummary(building: BuildingTile): RepairSummary | undefined {
  const hp = getBuildingMetadataNumber(building, 'hp')
  const maxHp = getBuildingMetadataNumber(building, 'maxHp')
  if (hp === undefined || maxHp === undefined || maxHp <= 0) return undefined

  const progress = Math.max(0, Math.min(1, hp / maxHp))
  const builderCount = (building.metadata?.['builderCount'] as number | undefined) ?? 0
  const remainingHp = Math.max(0, maxHp - hp)
  const secondsPerHpPerBuilder = 15 / 500
  const remainingSeconds =
    builderCount > 0 ? remainingHp * secondsPerHpPerBuilder / builderCount : undefined

  return {
    progress,
    timeLabel: remainingSeconds !== undefined ? formatRemainingSeconds(remainingSeconds) : 'Paused',
    builderCount,
  }
}

// ─────────────────────────────────────────────────────────────────────────────
// Perk action items
//
// Returns exactly 3 ActionItems — one per rank tier (bronze / silver / gold) —
// that always occupy the bottom row of the 3×3 action grid.
//
// Each slot shows the assigned perk icon for that rank, or a generic locked
// icon if no perk has been assigned yet. Slots are display-only (kind: 'perk');
// the SelectionHud renders them with rank-tinted borders and no click handler.
//
// TO ADD A NEW RANK TIER: append its name to the `ranks` array below and add
// a matching CSS class in SelectionHud.vue.
// ─────────────────────────────────────────────────────────────────────────────
const PERK_RANKS: Array<'bronze' | 'silver' | 'gold'> = ['bronze', 'silver', 'gold']

// HEAL_ABILITY_IDS gates the Focus Target action button: it's only meaningful
// for units whose kit can support an ally with a single-target or AoE heal.
// Keep this in sync with any new heal-class ability ids added to the catalog.
const HEAL_ABILITY_IDS = new Set(['heal', 'greater_heal'])

// unitOwnsHealAbility returns true when the unit has at least one heal-class
// ability — the criterion for surfacing the Focus Target action button. We
// gate on the ability id rather than the unit type/path so a future
// non-cleric support unit picks up the feature automatically.
function unitOwnsHealAbility(unit: Unit): boolean {
  if (!unit.abilities || unit.abilities.length === 0) return false
  return unit.abilities.some((a) => HEAL_ABILITY_IDS.has(a.id))
}

// buildFocusTargetActionItem produces the Focus Target action cell. The same
// shape works for single-unit and group selections.
//
// Highlight model — `autoCast` drives the sky-blue glow used elsewhere for
// auto-cast toggles. We light it up in TWO situations so the button reads as
// "armed" both while the player is mid-selection and after they commit:
//
//   1) `activeMode === 'focus-target'` — the player has clicked the button
//      and is currently picking an ally; the glow gives immediate feedback
//      that the cursor is now armed for targeting.
//   2) Any selected unit has `focusTargetId !== 0` — the focus has been
//      committed; the glow persists so the player can see at a glance that
//      this Cleric/group is currently in support-focus mode.
//
// `active` (the subtler brightness boost) is also set during selection so the
// two visual states layer slightly — armed reads as "glow + bright icon" and
// committed reads as "glow only", which is a small but useful distinction.
function buildFocusTargetActionItem(
  units: Unit[],
  activeMode: UnitTargetingMode | null,
): ActionItem {
  const anyFocused = units.some((u) => (u.focusTargetId ?? 0) !== 0)
  const selecting = activeMode === 'focus-target'
  const label = 'Focus Target'
  return {
    id: 'focus_target',
    label,
    kind: 'ability' as const,
    // Note: the action click id uses an underscore (`focus_target`) for
    // consistency with the server's wire string, but the icon catalog
    // registers art under the hyphen form (`focus-target`) like the other
    // perk icons. iconId routes the look-up to the right entry.
    iconId: 'focus-target',
    active: selecting,
    autoCast: anyFocused || selecting,
    supportsAutoCast: true,
    tooltipTitle: label,
    tooltipBody:
      units.length > 1
        ? `Left-click then pick an ally to focus all ${units.length} units on them. Right-click to clear.`
        : 'Left-click then pick an ally to focus on. Right-click to clear.',
  }
}

// getAbilityActionItems builds interactive ability buttons from the unit's
// snapshot abilities. id `cast-ability-<id>` drives left-click (cast →
// targeting) and, via the `autocast-toggle-` prefix, right-click. `active`
// highlights the ability whose cast target is currently being picked;
// `autoCast` drives the action-cell glow.
function getAbilityActionItems(
  unit: Unit,
  activeMode: UnitTargetingMode | null,
  castAbilityId: string | null,
): ActionItem[] {
  if (!unit.abilities || unit.abilities.length === 0) return []
  return unit.abilities
    // Spell-slot spells render in their rank's perk cell (see
    // getPerkActionItems), so they never appear in the ability row. Passives
    // DO appear here — as a non-castable info cell (see buildPassiveAbilityCell).
    .filter((a) => !a.spellSlotRank)
    .map((a) => {
      if (a.passive) return buildPassiveAbilityCell(a)
      const name = a.displayName ?? a.id
      return {
        id: `cast-ability-${a.id}`,
        label: name,
        kind: 'ability' as const,
        // Draw the ability's bundled art if present, else its projectile image.
        iconDef: { kind: 'ability' as const, type: a.id, projectile: a.projectile, iconKey: a.icon },
        active: activeMode === 'cast-ability' && castAbilityId === a.id,
        autoCast: !!a.autoCast,
        supportsAutoCast: !!a.supportsAutoCast,
        channeling: !!a.channeling,
        cooldownRemaining: a.cooldownRemaining,
        cooldownTotal: a.cooldownTotal,
        tooltipTitle: name,
        tooltipBody: a.supportsAutoCast
          ? 'Left-click: cast. Right-click: toggle auto-cast.'
          : 'Left-click: cast.',
      }
    })
}

// buildPassiveAbilityCell renders a passive (never-cast) ability such as
// Arcane Missiles as a NON-clickable info cell: it shows the ability art, a
// tooltip explaining how the passive triggers, and — for a charge-fire passive
// — the accumulated charge as a "current/required" badge over the icon so the
// player can watch it build toward its auto-fire.
function buildPassiveAbilityCell(a: AbilitySnapshot): ActionItem {
  const name = a.displayName ?? a.id
  const required = a.chargeRequired ?? 0
  const current = Math.floor(a.chargeCurrent ?? 0)
  const hasCharge = required > 0
  const tooltipBody = hasCharge
    ? `Passive. Spend mana to build Arcane Charge (1 per mana). At ${required} charge it automatically fires a volley of Arcane Missiles at nearby enemies, then resets. Charge: ${current}/${required}.`
    : 'Passive ability.'
  return {
    id: `passive-${a.id}`,
    label: name,
    kind: 'ability' as const,
    iconDef: { kind: 'ability' as const, type: a.id, projectile: a.projectile, iconKey: a.icon },
    disabled: true, // passive — not castable
    chargeText: hasCharge ? `${current}/${required}` : undefined,
    tooltipTitle: `${name} (Passive)`,
    tooltipBody,
  }
}

// buildPerkSlot renders a single perk cell in the HUD action grid.
// perkId is the granted perk's id (or undefined when the rank hasn't been
// reached yet, in which case a locked placeholder is emitted).
// rank and tierLabel are both the CSS/perkRank tier string — they are the
// same in all current call sites but kept separate so future cross-rank
// remaps can pass different values without touching the render path.
function buildPerkSlot(
  unit: Unit,
  perkId: string | undefined,
  rank: 'bronze' | 'silver' | 'gold',
  tierLabel: 'bronze' | 'silver' | 'gold',
): ActionItem {
  const def = perkId ? PERK_DEF_MAP.get(perkId) : undefined
  const rankLabel = tierLabel.charAt(0).toUpperCase() + tierLabel.slice(1)
  if (def) {
    const cd = perkId ? unit.perkCooldowns?.find((c) => c.perkId === perkId) : undefined
    return {
      id: def.icon ?? 'perk-locked',
      label: def.displayName,
      kind: 'perk' as const,
      perkRank: rank,
      tooltipTitle: `${def.displayName} (${rankLabel})`,
      tooltipBody: formatPerkTooltip(def, unit),
      disabled: true,
      cooldownRemaining: cd?.remaining,
      cooldownTotal: cd?.total,
    }
  }
  // Locked / empty slot for ranks the unit hasn't reached yet.
  return {
    id: 'lock',
    label: `${rankLabel} Perk (locked)`,
    kind: 'perk' as const,
    perkRank: rank,
    tooltipTitle: `${rankLabel} Perk`,
    tooltipBody: 'Locked — earn this rank to unlock.',
    disabled: true,
  }
}

// buildSpellSlotCell renders a learned spell-slot spell (arch-mage-spell-system)
// in its rank's bottom-row cell. Unlike a perk cell it is a CASTABLE 'ability'
// cell (clickable, autocast, cooldown) — the Arch Mage learns spells in place of
// passive perks. It carries perkRank so it sits in the perk row with the
// rank-colored border.
function buildSpellSlotCell(
  ability: AbilitySnapshot,
  rank: 'bronze' | 'silver' | 'gold',
  activeMode: UnitTargetingMode | null,
  castAbilityId: string | null,
): ActionItem {
  const name = ability.displayName ?? ability.id
  return {
    id: `cast-ability-${ability.id}`,
    label: name,
    kind: 'ability' as const,
    perkRank: rank,
    iconDef: { kind: 'ability' as const, type: ability.id, projectile: ability.projectile, iconKey: ability.icon },
    active: activeMode === 'cast-ability' && castAbilityId === ability.id,
    autoCast: !!ability.autoCast,
    supportsAutoCast: !!ability.supportsAutoCast,
    channeling: !!ability.channeling,
    cooldownRemaining: ability.cooldownRemaining,
    cooldownTotal: ability.cooldownTotal,
    tooltipTitle: `${name} (${rank.charAt(0).toUpperCase() + rank.slice(1)} Spell)`,
    tooltipBody: ability.supportsAutoCast
      ? 'Left-click: cast. Right-click: toggle auto-cast.'
      : 'Left-click: cast.',
  }
}

// spellSlotByRank indexes the unit's learned spell-slot spells by the rank they
// were learned at (from AbilitySnapshot.spellSlotRank).
function spellSlotByRank(unit: Unit): Map<string, AbilitySnapshot> {
  const m = new Map<string, AbilitySnapshot>()
  for (const a of unit.abilities ?? []) {
    if (a.spellSlotRank) m.set(a.spellSlotRank, a)
  }
  return m
}

function getPerkActionItems(
  unit: Unit,
  activeMode: UnitTargetingMode | null,
  castAbilityId: string | null,
): ActionItem[] {
  // Spell-slot spells (Arch Mage) replace the perk cell at their rank with a
  // castable slot. A rank with no learned slot spell falls back to the normal
  // perk-slot rendering.
  const slots = spellSlotByRank(unit)
  // The 4th cell (slot 12, right of gold) is an EXTRA perk slot reserved by
  // an advancement on the unit's owner (e.g. Twin Bronze). It exists as a
  // locked placeholder from the moment the advancement is owned, mirroring
  // the way the silver/gold slots render locked before the unit promotes
  // into those tiers. The trigger is the server-authoritative
  // `unit.extraPerkSlots` snapshot field, NOT the length of perkIds.
  const extraBronze = unit.extraPerkSlots?.bronze ?? 0

  if (extraBronze === 0) {
    // Standard 3-cell layout: bronze, silver, gold. A rank with a learned
    // spell-slot spell shows that castable spell; otherwise the granted perk
    // for that rank, or a locked placeholder.
    //
    // The perk for a cell is found by its DEFINITION rank, not by slot index:
    // a unit whose earlier tiers grant no perk (e.g. the Arch Mage, whose
    // bronze/silver tiers are spell slots and only gold grants a perk) has that
    // perk appended at perkIds[0], not perkIds[2]. Matching by rank keeps the
    // perk in its correct cell regardless of how many earlier tiers were empty,
    // and is consistent with how spell slots are placed (spellSlotByRank).
    return PERK_RANKS.map((rank) => {
      const slot = slots.get(rank)
      if (slot) return buildSpellSlotCell(slot, rank, activeMode, castAbilityId)
      const perkId = unit.perkIds?.find((id) => PERK_DEF_MAP.get(id)?.rank === rank)
      return buildPerkSlot(unit, perkId, rank, rank)
    })
  }

  // Twin Bronze layout. Server grant order is:
  //   perkIds[0] = primary bronze (granted at bronze rank-up)
  //   perkIds[1] = secondary bronze (granted at bronze rank-up alongside primary)
  //   perkIds[2] = silver (granted at silver rank-up)
  //   perkIds[3] = gold   (granted at gold rank-up)
  // Standard tier cells consume indices 0/2/3; the 4th cell at the right of
  // gold consumes index 1. Any missing index renders a locked placeholder,
  // including pre-bronze (perkIds empty / undefined → all 4 cells locked).
  return [
    buildPerkSlot(unit, unit.perkIds?.[0], 'bronze', 'bronze'),
    buildPerkSlot(unit, unit.perkIds?.[2], 'silver', 'silver'),
    buildPerkSlot(unit, unit.perkIds?.[3], 'gold', 'gold'),
    buildPerkSlot(unit, unit.perkIds?.[1], 'bronze', 'bronze'),
  ]
}

function buildMenuActionForDef(
  def: (typeof BUILDABLE_BUILDING_DEFS)[number],
  townHallTier: number,
): ActionItem {
  const requiredTier = def.requiresTownhallTier ?? 0
  const meetsTier = requiredTier <= 0 || townHallTier >= requiredTier
  // Tier name ("Requires Keep / Castle") comes from the town-hall upgrade chain.
  const tierName = townHallTierName(requiredTier)
  return {
    id: `build-${def.type}`,
    label: def.label,
    hotkey: def.hotkey ? def.hotkey.toUpperCase() : undefined,
    iconDef: { kind: 'building', type: def.type },
    cost: Object.entries(def.resourceCost ?? {})
      .filter(([, amount]) => amount > 0)
      .map(([id, amount]) => ({ resourceId: id, amount, accent: RESOURCE_ACCENT[id] ?? '#94a3b8' })),
    disabled: !meetsTier,
    tooltipTitle: meetsTier ? undefined : def.label,
    tooltipBody: meetsTier ? undefined : `Requires ${tierName}`,
  }
}

// buildMenuActions assembles the worker build menu: one action per buildable
// building in slots 0..N-1, then the exit button in the slot IMMEDIATELY AFTER
// them. Exit is placed after the buildings (not a fixed slot) so it never
// overwrites a building — the previous hard-coded `actions[6] = exit` collided
// with the 7th buildable building once the catalog grew past six (sorted by
// type that was townhall, which became unbuildable). The action grid has 12
// slots (GRID_SIZE), so this scales well past the current count.
export function buildMenuActions(townHallTier: number): ActionItem[] {
  const actions: ActionItem[] = []
  BUILDABLE_BUILDING_DEFS.forEach((def, i) => {
    actions[i] = buildMenuActionForDef(def, townHallTier)
  })
  actions[BUILDABLE_BUILDING_DEFS.length] = { id: 'close-build-menu', label: 'E(x)it' }
  return actions
}

function getUnitActions(
  unit: Unit,
  activeMode: UnitTargetingMode | null,
  buildMenuOpen: boolean,
  townHallTier: number = 0,
): ActionItem[] {
  if (buildMenuOpen) {
    return buildMenuActions(townHallTier)
  }
  const actions = unit.capabilities.map((capability) => {
    switch (capability) {
      case 'build':
        return { id: 'build', label: '(B)uild' }
      case 'gather':
        return { id: 'gather', label: '(G)ather', active: activeMode === 'gather' }
      case 'attack':
        return { id: 'attack', label: '(A)ttack', active: activeMode === 'attack' }
      default:
        return { id: capability, label: '(M)ove', active: activeMode === 'move' }
    }
  })
  if (unit.capabilities.includes('build')) {
    actions.push({ id: 'repair', label: '(R)epair', active: activeMode === 'repair' })
  }
  if (unit.capabilities.includes('attack')) {
    actions.push({ id: 'hold', label: '(H)old' })
    actions.push({ id: 'patrol', label: '(P)atrol', active: activeMode === 'patrol' })
    // Guard is for combat units, not workers — gathering units would never
    // auto-acquire, so the server rejects them anyway (mirrors the NonCombat
    // gate in GuardUnits). In-place stance like Hold, so no `active` targeting.
    if (!unit.capabilities.includes('gather')) {
      actions.push({ id: 'guard', label: '(G)uard' })
    }
  }
  return actions
}

function getGroupActions(
  units: Unit[],
  activeMode: UnitTargetingMode | null,
  buildMenuOpen: boolean,
  townHallTier: number = 0,
  castAbilityId: string | null = null,
): ActionItem[] {
  if (buildMenuOpen) {
    return buildMenuActions(townHallTier)
  }

  const capabilities = new Set<UnitCapability>()

  for (const unit of units) {
    for (const capability of unit.capabilities) {
      capabilities.add(capability)
    }
  }

  const actions = Array.from(capabilities).map((capability) => {
    switch (capability) {
      case 'build':
        return { id: 'build', label: '(B)uild' }
      case 'gather':
        return { id: 'gather', label: '(G)ather', active: activeMode === 'gather' }
      case 'attack':
        return { id: 'attack', label: '(A)ttack', active: activeMode === 'attack' }
      default:
        return { id: capability, label: '(M)ove', active: activeMode === 'move' }
    }
  })
  if (capabilities.has('build')) {
    actions.push({ id: 'repair', label: '(R)epair', active: activeMode === 'repair' })
  }
  if (capabilities.has('attack')) {
    actions.push({ id: 'hold', label: '(H)old' })
    actions.push({ id: 'patrol', label: '(P)atrol', active: activeMode === 'patrol' })
    // Guard surfaces when the group holds at least one combat (non-gathering)
    // unit; the server applies it only to those, skipping any workers.
    const hasGuardable = units.some(
      (u) => u.capabilities.includes('attack') && !u.capabilities.includes('gather'),
    )
    if (hasGuardable) {
      actions.push({ id: 'guard', label: '(G)uard' })
    }
  }
  // Shared ability buttons — appear when every selected unit owns the same
  // ability id (typical case: a group of Clerics with Heal). Right-click on
  // the button toggles auto-cast across the whole group (see GameClient
  // autocast handler for the "any-off → enable all" semantics).
  actions.push(...getSharedAbilityActionItems(units, activeMode, castAbilityId))
  // Focus Target — group variant. Surfaces when EVERY selected unit owns a
  // heal-class ability, so a Cleric + Acolyte mixed pair still works.
  if (units.every(unitOwnsHealAbility)) {
    actions.push(buildFocusTargetActionItem(units, activeMode))
  }
  return actions
}

// getSharedAbilityActionItems returns ability buttons for the group selection.
// An ability is "shared" iff every selected unit owns an ability with the same
// id. Aggregation rules:
//   - autoCast: true iff EVERY unit has autoCast enabled (so the glow only
//     appears when the whole group is auto-casting; partial-on shows un-lit).
//   - cooldownRemaining / cooldownTotal: max across units. Reflects the
//     slowest unit's readiness, so "ready" means the whole group is ready.
//   - displayName / supportsAutoCast: taken from the first unit (these are
//     intrinsic to the ability def, so they're identical across owners).
function getSharedAbilityActionItems(
  units: Unit[],
  activeMode: UnitTargetingMode | null,
  castAbilityId: string | null,
): ActionItem[] {
  if (units.length === 0) return []
  const first = units[0]
  if (!first.abilities || first.abilities.length === 0) return []

  return first.abilities
    .filter((a) =>
      units.every((u) => (u.abilities ?? []).some((other) => other.id === a.id)),
    )
    .map((a) => {
      const name = a.displayName ?? a.id
      // Aggregate across every unit's snapshot of THIS ability id.
      let allAutoCast = true
      let supportsAutoCast = false
      let anyChanneling = false
      let maxCdRemaining = 0
      let maxCdTotal = 0
      for (const u of units) {
        const snap = (u.abilities ?? []).find((other) => other.id === a.id)
        if (!snap) continue
        if (!snap.autoCast) allAutoCast = false
        if (snap.supportsAutoCast) supportsAutoCast = true
        if (snap.channeling) anyChanneling = true
        if ((snap.cooldownRemaining ?? 0) > maxCdRemaining) {
          maxCdRemaining = snap.cooldownRemaining ?? 0
        }
        if ((snap.cooldownTotal ?? 0) > maxCdTotal) {
          maxCdTotal = snap.cooldownTotal ?? 0
        }
      }
      return {
        id: `cast-ability-${a.id}`,
        label: name,
        kind: 'ability' as const,
        // Draw the ability's bundled art if present, else its projectile image
        // — matches the single-select button so multi-select shows real art
        // instead of the generic placeholder icon.
        iconDef: { kind: 'ability' as const, type: a.id, projectile: a.projectile, iconKey: a.icon },
        active: activeMode === 'cast-ability' && castAbilityId === a.id,
        autoCast: allAutoCast,
        supportsAutoCast,
        channeling: anyChanneling,
        cooldownRemaining: maxCdRemaining > 0 ? maxCdRemaining : undefined,
        cooldownTotal: maxCdTotal > 0 ? maxCdTotal : undefined,
        tooltipTitle: name,
        tooltipBody: supportsAutoCast
          ? `Left-click: cast. Right-click: toggle auto-cast for ${units.length} units.`
          : 'Left-click: cast.',
      }
    })
}

/**
 * Returns the two action buttons shown when an owned building is under
 * construction: Kick Builders and Demolish.
 *
 * Icon choice: both buttons reuse the existing 'repair' PNG icon (the hammer)
 * as a placeholder because no dedicated construction-management icons exist yet.
 * The asset pipeline owner can drop kick-builders.png / demolish-building.png
 * into src/assets/ui/actions/ and the renderer will pick them up automatically via
 * actionIconSprites.ts — no code change needed.
 */
function getUnderConstructionActions(building: BuildingTile): ActionItem[] {
  const builderCount = (building.metadata?.['builderCount'] as number | undefined) ?? 0
  return [
    {
      id: 'kick-builders',
      label: 'Kick Builders',
      // TODO: replace with a dedicated 'kick-builders' icon asset
      iconId: 'repair',
      disabled: builderCount === 0,
      tooltipTitle: 'Kick Builders',
      tooltipBody: 'Remove all workers currently assigned to this construction site.',
    },
    {
      id: 'demolish-building',
      label: 'Demolish',
      // TODO: replace with a dedicated 'demolish-building' icon asset
      iconId: 'repair',
      tooltipTitle: 'Demolish Building',
      tooltipBody: 'Cancel construction and demolish the building foundation.',
    },
  ]
}

export function getBuildingActions(
  building: BuildingTile,
  upgrades: PlayerUpgradeSnapshot[] = [],
  vaultState: { vault: VaultItemSnapshot[]; vaultCapacity?: number } = { vault: [] },
  // Retained for call-site positional compatibility; the tier-up action now
  // reads each building's own metadata["tier"] rather than the player-wide
  // town-hall tier, so this is no longer consulted here.
  _townHallTier: number = 0,
  lockedUnitTypes: ReadonlySet<string> = new Set(),
  shopRerollsRemaining: number = 0,
  unlockedRecipeIds: ReadonlySet<string> = new Set(),
  unitCostOverrides: ReadonlyMap<string, UnitCostOverride> = new Map(),
): ActionItem[] {
  const actions: ActionItem[] = []

  if (building.capabilities?.includes('item-purchase')) {
    // Per-building shop inventory: the server populates building.shopInventory
    // at match start (fixed list, loot-table roll, or marketplace catalog
    // fallback). Locked neutral shops disable their buy actions; undiscovered
    // shops never reach this code because the selection HUD won't surface
    // them as targets. Each entry carries the remaining quantity; entries
    // at quantity 0 stay rendered but are disabled (greyed-out).
    const inventory = building.shopInventory ?? []
    const shopLocked = building.shopLocked === true
    for (const slot of inventory) {
      const itemDef = ITEM_DEF_MAP.get(slot.itemId)
      if (!itemDef) continue
      const soldOut = slot.quantity <= 0
      // The vault is unbounded, so purchases are never blocked for lack of room.
      // Remaining stock is NOT tooltip text — it rides stockCount and renders
      // as the button's bottom-right corner number.
      let tooltipBody = buildItemTooltipBody(itemDef)
      if (soldOut) {
        tooltipBody = `${tooltipBody}\n\nSold out at this shop.`
      } else if (shopLocked) {
        tooltipBody = `${tooltipBody}\n\nGuards remain — clear them to unlock this shop.`
      }
      actions.push({
        id: `buy-item-${itemDef.id}`,
        label: itemDef.displayName,
        iconDef: { kind: 'item', type: itemDef.id },
        cost: [{ resourceId: 'gold', amount: itemDef.costGold, accent: '#d4a84f' }],
        tooltipTitle: itemDef.displayName,
        tooltipBody,
        disabled: shopLocked || soldOut,
        ...(soldOut ? {} : { stockCount: slot.quantity }),
      })
    }

    // Reroll button — only on neutral-shop buildings. The button is placed
    // in the bottom-right slot of the 12-cell action grid by padding the
    // intervening slots with id:"" so the renderer skips them. Disabled
    // when the player has no rerolls remaining; tooltip explains why.
    if (building.buildingType === 'neutral-shop') {
      while (actions.length < 11) {
        actions.push({ id: '', label: '', iconDef: { kind: 'item', type: '' } })
      }
      const canReroll = !shopLocked && shopRerollsRemaining > 0
      const rerollTooltip = shopLocked
        ? 'Guards remain — clear them to unlock this shop.'
        : canReroll
          ? `Reroll this merchant's inventory.\n\nRerolls remaining: ${shopRerollsRemaining}`
          : `Out of rerolls. Purchase the Merchant Reroll dominion-point upgrade to gain more.`
      actions.push({
        id: 'reroll-shop',
        label: 'Reroll',
        iconDef: { kind: 'item', type: 'reroll' },
        tooltipTitle: 'Reroll Merchant',
        tooltipBody: rerollTooltip,
        disabled: !canReroll,
      })
    }
  }

  if (building.capabilities?.includes('recipe-purchase')) {
    // Recipe Shop: one buy action per stocked recipe. Quantity 0 = sold out
    // (kept visible but disabled), mirroring item-purchase stock handling.
    const shopLocked = building.shopLocked === true
    for (const slot of building.recipeInventory ?? []) {
      const recipe = RECIPE_DEF_MAP.get(slot.recipeId)
      if (!recipe) continue
      const soldOut = slot.quantity <= 0
      // Recipes the player already knows are greyed out with an explanatory
      // tooltip — buying again would spend gold for nothing (the server also
      // no-ops these). Takes precedence over the sold-out / locked reasons.
      const known = unlockedRecipeIds.has(recipe.id)
      // List each required ingredient on its own line under "Requires:", using
      // the item's display name (not raw id) and collapsing duplicates to a ×N
      // count — matches how the Artificer renders ingredients.
      const need = new Map<string, number>()
      for (const input of recipe.inputs) need.set(input, (need.get(input) ?? 0) + 1)
      const inputLines = [...need].map(([itemId, count]) => {
        const name = ITEM_DEF_MAP.get(itemId)?.displayName ?? itemId
        return count > 1 ? `${name} ×${count}` : name
      })
      let tooltipBody = `Unlocks crafting:\n${recipe.name}\n\nRequires:\n${inputLines.join('\n')}`
      if (known) tooltipBody = `${tooltipBody}\n\nRecipe already known.`
      else if (soldOut) tooltipBody = `${tooltipBody}\n\nAlready purchased at this shop.`
      else if (shopLocked) tooltipBody = `${tooltipBody}\n\nGuards remain — clear them to unlock this shop.`
      // Recipe Shop sells the *recipe* (not the item), so it renders a recipe
      // scroll icon keyed by rarity rather than the output item's icon. Prefer a
      // tier-specific asset (${rarity}_recipe) and fall back to rare_recipe when
      // one hasn't been added yet — this lets epic_recipe/legendary_recipe drop
      // in later with no code change. (The Artificer deliberately keeps showing
      // the output item's icon, since it crafts that item.)
      const rarity = recipe.rarity ?? 'common'
      const rarityIconKey = `${rarity}_recipe`
      const recipeIconKey = hasItemAsset(rarityIconKey) ? rarityIconKey : 'rare_recipe'
      actions.push({
        id: `buy-recipe-${recipe.id}`,
        label: `Recipe: ${recipe.name}`,
        iconDef: { kind: 'item', type: recipeIconKey },
        cost: [{ resourceId: 'gold', amount: recipe.costGold, accent: '#d4a84f' }],
        tooltipTitle: `Recipe: ${recipe.name}`,
        tooltipBody,
        disabled: known || soldOut || shopLocked,
      })
    }
  }

  if (building.capabilities?.includes('crafting')) {
    // Artificer: one craft action per unlocked recipe. Ingredient counts are
    // computed from the Vault snapshot; a recipe is disabled (not hidden) when
    // the vault lacks the required inputs so the player can see what to gather.
    // Affordability (gold) is NOT gated client-side — the server rejects an
    // unaffordable craft, matching how buy-item actions don't gate on gold.
    const vault = vaultState?.vault ?? []
    const have = new Map<string, number>()
    for (const vi of vault) have.set(vi.itemId, (have.get(vi.itemId) ?? 0) + (vi.stacks ?? 1))
    // Deterministic order: iterate the sorted unlocked ids.
    for (const recipeId of [...unlockedRecipeIds].sort()) {
      const recipe = RECIPE_DEF_MAP.get(recipeId)
      if (!recipe) continue
      const need = new Map<string, number>()
      for (const input of recipe.inputs) need.set(input, (need.get(input) ?? 0) + 1)
      let missing = false
      const parts: string[] = []
      for (const [itemId, count] of need) {
        const owned = have.get(itemId) ?? 0
        if (owned < count) missing = true
        const itemName = ITEM_DEF_MAP.get(itemId)?.displayName ?? itemId
        parts.push(`${itemName} ${owned}/${count}`)
      }
      actions.push({
        id: `craft-${recipe.id}`,
        label: recipe.name,
        iconDef: { kind: 'item', type: recipe.output },
        cost: [{ resourceId: 'gold', amount: recipe.costGold, accent: '#d4a84f' }],
        tooltipTitle: recipe.name,
        // Name + cost are rendered by the tooltip frame (title + cost rows);
        // the body just lists the ingredients, one per line. The disabled /
        // greyed-out state already signals when ingredients are missing.
        tooltipBody: `Ingredients:\n${parts.join('\n')}`,
        disabled: missing,
      })
    }
  }

  if (building.capabilities.includes('unit-spawner')) {
    let hasTrainable = false
    for (const unitType of building.spawnUnitTypes ?? []) {
      const def = UNIT_DEF_MAP.get(unitType)
      if (def) {
        // Prefer the player's effective cost (advancement discounts baked in)
        // over the static catalog cost so the displayed price matches what the
        // server actually charges. Absent override ⇒ catalog cost.
        const override = unitCostOverrides.get(unitType)
        const resourceCost = override?.resourceCost ?? def.resourceCost ?? {}
        const meatCost = override?.meatCost ?? def.meatCost
        const cost = Object.entries(resourceCost)
          .filter(([, amount]) => amount > 0)
          .map(([id, amount]) => ({ resourceId: id, amount, accent: RESOURCE_ACCENT[id] ?? '#94a3b8' }))
        // Food (meat) is tracked separately from resourceCost on the unit def,
        // so append it as its own cost row when the unit consumes any.
        if (meatCost > 0) {
          cost.push({ resourceId: 'food', amount: meatCost, accent: RESOURCE_ACCENT.food ?? '#c96e43' })
        }
        const isLocked = lockedUnitTypes.has(unitType)
        const requires = def.requiresBuildings ?? []
        const hasRequirements = requires.length > 0
        actions.push({
          id: `train-${unitType}`,
          label: def.trainLabel,
          iconDef: { kind: 'unit', type: unitType },
          cost,
          disabled: isLocked,
          tooltipTitle: isLocked ? def.trainLabel : undefined,
          tooltipBody: isLocked
            ? (hasRequirements
                ? `Requires: ${requires.map(formatBuildingName).join(', ')}`
                : 'Requirements not met')
            : undefined,
        })
        hasTrainable = true
      }
    }
    if (hasTrainable) {
      actions.push({ id: 'set-spawn-point', label: 'Set Rally Point' })
    }
  }

  {
    // Per-building queue model: a building can stack multiple upgrades, so a
    // track already in flight here does NOT block a different track — clicking
    // it queues behind the active one. A track is only blocked when it is locked
    // to a DIFFERENT building (its queueBuildingId is some other building). Which
    // tracks a building offers is driven by capability: the building shows a
    // track only when its capabilities include the track's capability (e.g. a
    // blacksmith offers 'blacksmith-upgrade' tracks; a chapel would offer its own).
    for (const upgrade of upgrades) {
      if (!building.capabilities.includes(upgrade.capability)) continue
      const homeId = upgrade.queueBuildingId
      const lockedElsewhere = !!homeId && homeId !== building.id
      const queued = upgrade.queuedCount ?? 0
      // The level the queue will reach; the next purchase stacks one above it.
      const projectedLevel = upgrade.level + queued
      const atCap = projectedLevel >= upgrade.cap
      // Blocked by an unmet building prerequisite (e.g. needs a Keep/Castle).
      const requirementLocked =
        !atCap && projectedLevel >= upgrade.purchasableCap && !!upgrade.nextRequirement
      const statParts = (upgrade.nextStats ?? []).map(formatUpgradeStatDelta)

      // canStart (server) folds in: below the purchasable cap, affordable, and a
      // building exists. The cross-building track lock is layered on here.
      const disabled = lockedElsewhere || !upgrade.canStart
      const nextLevel = projectedLevel + 1
      const levelLabel = atCap
        ? `${upgrade.displayName} (Max)`
        : queued > 0
          ? `${upgrade.displayName} Lv ${projectedLevel} (+${queued}) → ${nextLevel}`
          : `${upgrade.displayName} Lv ${upgrade.level} → ${nextLevel}`
      const tooltipBody = lockedElsewhere
        ? 'This track is queued at another building'
        : atCap
          ? 'Maximum level reached'
          : requirementLocked
            ? `Requires a ${upgrade.nextRequirement}`
            : statParts.join('  ')
      actions.push({
        id: `upgrade-${upgrade.track}`,
        label: levelLabel,
        iconDef: { kind: 'unit', type: upgrade.track },
        cost:
          atCap || requirementLocked
            ? undefined
            : [
                { resourceId: 'gold', amount: upgrade.nextCostGold, accent: RESOURCE_ACCENT.gold ?? '#d4a84f' },
                { resourceId: 'wood', amount: upgrade.nextCostWood, accent: RESOURCE_ACCENT.wood ?? '#7a9a52' },
              ],
        disabled,
        tooltipTitle: levelLabel,
        tooltipBody,
      })
    }
  }

  // Building tier-up pinned to slot 9 (bottom-left of the 4×3 action grid) by
  // padding regular actions out to length 8. Mirrors the unit-action layout
  // where perks always start at slot 9. Generic across any building whose type
  // roots an upgradesFrom chain (townhall → keep → castle, chapel → temple, …).
  const upgradeChain = getUpgradeChain(building.buildingType)
  if (upgradeChain.length > 1) {
    while (actions.length < 8) {
      actions.push({ id: '', label: '', disabled: true })
    }
    // The chain drives the label, cost, and max tier — all sourced from the
    // catalog rather than hardcoded here. Read this building's own tier
    // (default 1) so each building tracks its upgrade state independently.
    const tier = getBuildingMetadataNumber(building, 'tier') ?? 1
    const tierUpRemaining = getBuildingMetadataNumber(building, 'tierUpRemaining')
    const inProgress = tierUpRemaining !== undefined
    const atMax = tier >= upgradeChain.length

    const currentLabel = upgradeChain[tier - 1]?.label ?? building.buildingType
    let label = `${currentLabel} (Max)`
    let cost: ActionCost[] | undefined
    // Icon shows what the action produces: the target tier's sprite while an
    // upgrade is available (e.g. the Keep sprite for "Upgrade to Keep"), falling
    // back to the current tier's sprite at max.
    let iconType = upgradeChain[tier - 1]?.type ?? building.buildingType
    if (!atMax) {
      const nextDef = upgradeChain[tier] // tier is 1-based; index tier = next tier def
      label = `Upgrade to ${nextDef?.label ?? 'next tier'}`
      iconType = nextDef?.type ?? iconType
      cost = Object.entries(nextDef?.upgradeCost ?? {})
        .filter(([, amount]) => amount > 0)
        .map(([id, amount]) => ({ resourceId: id, amount, accent: RESOURCE_ACCENT[id] ?? '#94a3b8' }))
    }

    actions.push({
      id: 'upgrade-building',
      label,
      iconDef: { kind: 'building', type: iconType },
      cost,
      disabled: atMax || inProgress,
      tooltipTitle: label,
      tooltipBody: inProgress
        ? 'Upgrade in progress…'
        : atMax
          ? `${currentLabel} is at max tier.`
          : undefined,
    })
  }

  return actions
}

function getBuildingDetails(building: BuildingTile): DetailItem[] {
  const hp = getBuildingMetadataNumber(building, 'hp')
  const maxHp = getBuildingMetadataNumber(building, 'maxHp')
  const isUnderConstruction = building.metadata?.['underConstruction'] === true
  if (isUnderConstruction) {
    const details: DetailItem[] = []
    if (hp !== undefined && maxHp !== undefined && maxHp > 0) {
      const pct = Math.round((hp / maxHp) * 100)
      const builderCount = getBuildingMetadataNumber(building, 'builderCount') ?? 0
      const remainingHp = Math.max(0, maxHp - hp)
      const secondsPerHpPerBuilder = 15 / 500
      const remainingSeconds =
        builderCount > 0 ? remainingHp * secondsPerHpPerBuilder / builderCount : undefined

      details.push({
        id: 'construction-health',
        label: 'Durability',
        value: formatDurability(hp, maxHp),
      })
      details.push({ id: 'construction-progress', label: 'Progress', value: `${pct}%` })
      details.push({
        id: 'construction-time',
        label: 'Build Time',
        value: remainingSeconds !== undefined ? formatRemainingSeconds(remainingSeconds) : 'Paused',
      })
      details.push({
        id: 'construction-builders',
        label: 'Builders',
        value: `${builderCount} / 3`,
      })
    }
    return details
  }

  const activeProduction = getBuildingProductionState(building)
  if (activeProduction) {
    const nextQueuedUnit = activeProduction.queuedUnitTypes[1]
    const hiddenQueueCount = Math.max(activeProduction.queueLength - 2, 0)
    const productionDetails: DetailItem[] = [
      {
        id: 'current-training-unit',
        label: 'Training',
        value: formatSpawnUnitType(activeProduction.unitType),
      },
      {
        id: 'next-queued-unit',
        label: 'Next In Queue',
        value: nextQueuedUnit
          ? `${formatSpawnUnitType(nextQueuedUnit)}${hiddenQueueCount > 0 ? ` (${hiddenQueueCount})` : ''}`
          : 'None',
      },
    ]
    appendTierUpDetails(building, productionDetails)
    return productionDetails
  }

  const details: DetailItem[] = []

  if (hp !== undefined) {
    details.push({
      id: 'durability',
      label: 'Durability',
      value: formatDurability(hp, maxHp ?? hp),
    })
  }

  const workerLabel = getBuildingResourceLabel(building)
  const workerAmount = getBuildingResourceAmount(building)
  if (workerLabel && workerAmount !== undefined) {
    details.push({
      id: 'workers-inside',
      label: workerLabel,
      value: String(workerAmount),
    })
  }

  const stockLabel = getBuildingStockLabel(building)
  const stockAmount = getBuildingStockAmount(building)
  if (stockLabel && stockAmount !== undefined) {
    details.push({
      id: 'resource-stock',
      label: stockLabel,
      value: String(stockAmount),
    })
  }

  if (building.capabilities.includes('deposit-point')) {
    details.push({ id: 'deposit-point', label: 'Deposit Point' })
  }
  if (building.capabilities.includes('occupiable')) {
    details.push({ id: 'occupiable', label: 'Occupiable' })
  }
  const buildingDef = BUILDING_DEF_MAP.get(building.buildingType)
  if ((buildingDef?.damage ?? 0) > 0) {
    details.push({
      id: 'damage',
      label: 'Damage',
      value: String(buildingDef?.damage ?? 0),
    })
  }
  if ((buildingDef?.attackRange ?? 0) > 0) {
    details.push({
      id: 'attack-range',
      label: 'Range',
      value: String(Math.round(buildingDef?.attackRange ?? 0)),
    })
  }
  if ((buildingDef?.attackSpeed ?? 0) > 0) {
    const speedTooltipLines: string[] = []
    if ((buildingDef?.damage ?? 0) > 0) speedTooltipLines.push(`Damage: ${buildingDef!.damage}`)
    speedTooltipLines.push(`Attack speed: ${buildingDef!.attackSpeed!.toFixed(2)}/s`)
    details.push({
      id: 'attack-speed',
      label: 'Attack Speed',
      value: attackSpeedLabel(buildingDef!.attackSpeed!),
      tooltipTitle: attackSpeedLabel(buildingDef!.attackSpeed!),
      tooltipBody: speedTooltipLines.join('\n'),
    })
  }
  if (building.capabilities.includes('unit-spawner') && building.spawnUnitTypes?.length) {
    details.push({
      id: 'trains-units',
      label: 'Trains',
      value: building.spawnUnitTypes.map(formatSpawnUnitType).join(', '),
    })
  }

  const spawnPointLabel = getBuildingSpawnPointLabel(building)
  if (spawnPointLabel) {
      details.push({
        id: 'spawn-point',
        label: 'Rally Point',
        value: spawnPointLabel,
      })
  }

  appendTierUpDetails(building, details)

  return details
}

function appendTierUpDetails(building: BuildingTile, details: DetailItem[]): void {
  const tierUpRemaining = getBuildingMetadataNumber(building, 'tierUpRemaining')
  const tierUpTotal = getBuildingMetadataNumber(building, 'tierUpTotal')
  const tierTargetLevel = getBuildingMetadataNumber(building, 'tierTargetLevel')
  if (tierUpRemaining === undefined || tierUpTotal === undefined || tierUpTotal <= 0) return
  // Name the target tier from THIS building's own upgrade chain (townhall →
  // keep → castle, chapel → temple, …) so a chapel reads "Temple", not a
  // townhall-chain name.
  const chain = getUpgradeChain(building.buildingType)
  const targetName =
    tierTargetLevel !== undefined
      ? (chain[Math.round(tierTargetLevel) - 1]?.label ?? 'next tier')
      : 'next tier'
  const progress = Math.max(0, Math.min(1, 1 - tierUpRemaining / tierUpTotal))
  details.push({ id: 'tierup-remaining', label: 'Upgrading to', value: targetName })
  details.push({ id: 'tierup-progress', label: 'Progress', value: String(progress) })
}

// Slow < 1/s, Normal 1–1.25/s, Fast 1.25–1.75/s, Very Fast > 1.75/s
function attackSpeedLabel(attacksPerSecond: number): string {
  if (attacksPerSecond < 1) return 'Slow'
  if (attacksPerSecond < 1.25) return 'Normal'
  if (attacksPerSecond <= 1.75) return 'Fast'
  return 'Very Fast'
}

// Render HP regen as "1 HP / 5s" when the rate is sub-1 HP/s, otherwise as
// "X HP / s" with one decimal. Matches the "1 every N" phrasing players expect
// for trickle regen.
function formatHealthRegen(hpPerSecond: number): string {
  if (hpPerSecond <= 0) return '0 HP / s'
  if (hpPerSecond < 1) {
    const interval = Math.round(10 / hpPerSecond) / 10
    return `1 HP / ${interval}s`
  }
  return `${hpPerSecond.toFixed(1)} HP / s`
}

// Mirror of formatHealthRegen for mana regen — same "1 every N" phrasing
// for sub-1 mana/s trickle rates (e.g. the acolyte's 0.2/s default), decimal
// form for faster rates.
function formatManaRegen(manaPerSecond: number): string {
  if (manaPerSecond <= 0) return '0 / s'
  if (manaPerSecond < 1) {
    const interval = Math.round(10 / manaPerSecond) / 10
    return `1 / ${interval}s`
  }
  return `${manaPerSecond.toFixed(1)} / s`
}

// Mirrors server/internal/game/progression.go armorDamageReduction — keep in sync.
// reduction = armor / (armor + K) where K = 100.
const ARMOR_MITIGATION_K = 100

function armorDamageReductionFraction(armor: number): number {
  if (armor <= 0) return 0
  return armor / (armor + ARMOR_MITIGATION_K)
}

// Display labels for source-specific shield pools (server -> human-readable).
// Add an entry when you wire up a new shield perk on the server. Unknown
// source types fall back to a title-cased version of the raw id so a missing
// entry degrades gracefully instead of dumping an internal snake_case key.
const SHIELD_SOURCE_LABELS: Record<string, string> = {
  dark_renewal: 'Dark Renewal',
}

function shieldSourceLabel(sourceType: string): string {
  const known = SHIELD_SOURCE_LABELS[sourceType]
  if (known) return known
  return sourceType
    .split(/[_-]/)
    .filter(Boolean)
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(' ')
}

function getUnitDetails(unit: Unit): DetailItem[] {
  const healthRegen = unit.healthRegen ?? 0
  const durabilityTooltipBody = healthRegen > 0
    ? `Regenerates ${formatHealthRegen(healthRegen)}`
    : 'No passive regeneration'

  const details: DetailItem[] = [
    {
      id: 'durability',
      label: 'Durability',
      value: `${unit.hp ?? 0} / ${unit.maxHp ?? unit.hp ?? 0}`,
      icon: STAT_ICON_HEART,
      tooltipTitle: `HP ${unit.hp ?? 0} / ${unit.maxHp ?? unit.hp ?? 0}`,
      tooltipBody: durabilityTooltipBody,
    },
  ]

  // Overshield — placed directly under Durability so the player reads the
  // protective layer alongside HP. Only surfaced when an active shield source
  // exists; units with no source (the common case) skip this row entirely so
  // the panel stays compact.
  //
  // Aggregate values come from unit.shield / unit.maxShield. The tooltip body
  // breaks the totals out by source via unit.shieldPools when the server has
  // shipped a per-source breakdown — that's the case for source-specific
  // perks like dark_renewal. Legacy single-pool shields (blood_engine) are
  // represented only in the aggregate and surface as a "(other sources)"
  // remainder line when present so the totals always reconcile.
  const overshield = unit.shield ?? 0
  const maxOvershield = unit.maxShield ?? 0
  if (maxOvershield > 0 || overshield > 0) {
    const pools = unit.shieldPools ?? []
    const tooltipLines: string[] = [
      `Shield: ${overshield} / ${maxOvershield}`,
      'Temporary HP pool. Drains before HP on every incoming hit.',
    ]
    if (pools.length > 0) {
      tooltipLines.push('') // blank line before the per-source breakdown
      for (const pool of pools) {
        tooltipLines.push(`${shieldSourceLabel(pool.sourceType)}: ${pool.current} / ${pool.max}`)
      }
      // Reconcile pools vs aggregate so any legacy single-pool shield
      // (blood_engine) the server hasn't decomposed surfaces as a labelled
      // remainder instead of silently disappearing from the breakdown.
      const poolsCurrent = pools.reduce((sum, p) => sum + p.current, 0)
      const poolsMax = pools.reduce((sum, p) => sum + p.max, 0)
      const otherCurrent = Math.max(0, overshield - poolsCurrent)
      const otherMax = Math.max(0, maxOvershield - poolsMax)
      if (otherCurrent > 0 || otherMax > 0) {
        tooltipLines.push(`Other sources: ${otherCurrent} / ${otherMax}`)
      }
    }
    details.push({
      id: 'overshield',
      label: 'Shield',
      value: `${overshield} / ${maxOvershield}`,
      icon: STAT_ICON_BARRIER,
      tooltipTitle: `Shield: ${overshield} / ${maxOvershield}`,
      tooltipBody: tooltipLines.join('\n'),
    })
  }

  // Mana — only surfaced for spellcaster units (server omits maxMana for
  // non-casters). Mirrors the durability row's current/max format and
  // appends the passive regen rate inline when present so the player can
  // read both pool size and refill cadence in one glance.
  if ((unit.maxMana ?? 0) > 0) {
    const mana = unit.mana ?? 0
    const maxMana = unit.maxMana ?? 0
    const manaRegen = unit.manaRegen ?? 0
    const regenSuffix = manaRegen > 0 ? ` (+${formatManaRegen(manaRegen)})` : ''
    const tooltipLines = ['Spent to cast abilities.']
    if (manaRegen > 0) {
      tooltipLines.push(`Regenerates ${formatManaRegen(manaRegen)}`)
    }
    details.push({
      id: 'mana',
      label: 'Mana',
      value: `${mana} / ${maxMana}${regenSuffix}`,
      icon: STAT_ICON_BOLT,
      tooltipTitle: `Mana ${mana} / ${maxMana}`,
      tooltipBody: tooltipLines.join('\n'),
    })
  }

  // Damage, attack speed, and attack range share one row — the sword icon
  // covers all three. Range is a substat under attack rather than its own row
  // so the stats panel stays compact.
  const hasDamage = (unit.damage ?? 0) > 0
  const hasAttackSpeed = (unit.attackSpeed ?? 0) > 0
  const hasAttackRange = unit.attackRange !== undefined && unit.attackRange > 0
  if (hasDamage || hasAttackSpeed) {
    const parts: string[] = []
    if (hasDamage) parts.push(String(unit.damage))
    if (hasAttackSpeed) parts.push(attackSpeedLabel(unit.attackSpeed!))
    if (hasAttackRange) parts.push(`${Math.round(unit.attackRange!)} rng`)

    // Build hover tooltip with base vs bonus breakdown.
    const unitDef = UNIT_DEF_MAP.get(unit.unitType)
    const tooltipLines: string[] = []
    if (hasDamage) {
      const baseDmg = unitDef?.damage ?? unit.damage!
      const bonusDmg = (unit.damage ?? 0) - baseDmg
      tooltipLines.push(`Base damage: ${baseDmg}`)
      if (bonusDmg > 0) tooltipLines.push(`Bonus damage: +${bonusDmg}`)
    }
    if (hasAttackSpeed) {
      const baseSpeed = unitDef?.attackSpeed ?? unit.attackSpeed!
      const bonusSpeed = Math.round(((unit.attackSpeed ?? 0) - baseSpeed) * 100) / 100
      tooltipLines.push(`Attack speed: ${unit.attackSpeed!.toFixed(2)}/s`)
      if (bonusSpeed > 0) tooltipLines.push(`Bonus speed: +${bonusSpeed.toFixed(2)}/s`)
    }
    if (hasAttackRange) {
      const baseRange = unitDef?.attackRange ?? unit.attackRange!
      const bonusRange = unit.attackRange! - baseRange
      tooltipLines.push(`Attack range: ${Math.round(unit.attackRange!)}`)
      if (Math.abs(bonusRange) >= 1) {
        tooltipLines.push(`Bonus range: ${bonusRange > 0 ? '+' : ''}${Math.round(bonusRange)}`)
      }
    }

    details.push({
      id: 'attack',
      label: 'Damage / Attack Speed / Range',
      value: parts.join(' · '),
      icon: STAT_ICON_SWORD,
      tooltipTitle: parts.join(' · '),
      tooltipBody: tooltipLines.join('\n'),
    })
  }

  // Crit row — combined chance + multiplier so the player sees both values
  // in one place. Only rendered when the unit owns a crit source (server
  // omits the fields otherwise).
  if ((unit.critChance ?? 0) > 0 || (unit.critMultiplier ?? 0) > 0) {
    const chancePct = Math.round((unit.critChance ?? 0) * 100)
    const mult = unit.critMultiplier ?? 0
    const tooltipLines: string[] = [
      `Crit chance: ${chancePct}%`,
      `Crit damage: ${mult.toFixed(1)}×`,
      'Hunter’s Mark on the target adds extra chance per stack.',
    ]
    details.push({
      id: 'crit',
      label: 'Crit',
      value: `${chancePct}% / ${mult.toFixed(1)}×`,
      icon: STAT_ICON_CRIT,
      tooltipTitle: `Critical Hit: ${chancePct}% chance, ${mult.toFixed(1)}× damage`,
      tooltipBody: tooltipLines.join('\n'),
    })
  }

  if (unit.moveSpeed !== undefined && unit.moveSpeed > 0) {
    const unitDef = UNIT_DEF_MAP.get(unit.unitType)
    const baseSpeed = unitDef?.moveSpeed ?? unit.moveSpeed
    const bonusSpeed = unit.moveSpeed - baseSpeed
    const tooltipLines: string[] = [`Base move speed: ${Math.round(baseSpeed)}`]
    if (bonusSpeed > 0) {
      tooltipLines.push(`Bonus move speed: +${Math.round(bonusSpeed)}`)
    }
    details.push({
      id: 'move-speed',
      label: 'Move Speed',
      value: String(Math.round(unit.moveSpeed)),
      icon: STAT_ICON_BOOT,
      tooltipTitle: `Move Speed: ${Math.round(unit.moveSpeed)}`,
      tooltipBody: tooltipLines.join('\n'),
    })
  }

  {
    const armor = unit.armor ?? 0
    const reductionPct = Math.round(armorDamageReductionFraction(armor) * 100)
    details.push({
      id: 'armor',
      label: 'Armor',
      value: String(armor),
      icon: STAT_ICON_SHIELD,
      tooltipTitle: `Armor: ${armor}`,
      tooltipBody: `${reductionPct}% damage reduction`,
    })
  }

  // Non-stat details without icons — rendered inline below the stat grid.
  // (Shield used to live here as a non-stat row; it's now a first-class stat
  // entry inserted under Durability above, with a per-source tooltip.)

  if (unit.carriedResourceType && unit.carriedAmount !== undefined) {
    details.push({
      id: 'carried-resource',
      label: `${formatResourceLabel(unit.carriedResourceType)} Carried`,
      value: String(unit.carriedAmount),
    })
  }

  return details
}

function getUnitXpLabel(unit: Unit): string {
  const xp = unit.xp ?? 0
  if (unit.xpToNextRank && unit.xpToNextRank > 0) {
    const intoCurrent = unit.xpIntoCurrentRank ?? 0
    const rankTotal = intoCurrent + unit.xpToNextRank
    return `${intoCurrent} / ${rankTotal} XP`
  }
  return `${xp} XP (max)`
}

function getGroupDetails(units: Unit[]): DetailItem[] {
  const details: DetailItem[] = []

  const carryingGold = units.reduce(
    (sum, unit) => sum + (unit.carriedResourceType === 'gold' ? unit.carriedAmount ?? 0 : 0),
    0,
  )
  const carryingWood = units.reduce(
    (sum, unit) => sum + (unit.carriedResourceType === 'wood' ? unit.carriedAmount ?? 0 : 0),
    0,
  )

  if (carryingGold > 0) {
    details.push({ id: 'group-gold', label: 'Gold Carried', value: String(carryingGold) })
  }
  if (carryingWood > 0) {
    details.push({ id: 'group-wood', label: 'Wood Carried', value: String(carryingWood) })
  }

  return details
}

export function formatUnitPath(path?: string) {
  switch (path) {
    case 'vanguard':
      return 'Vanguard'
    case 'berserker':
      return 'Berserker'
    case 'trapper':
      return 'Trapper'
    case 'marksman':
      return 'Marksman'
    case 'cleric':
      return 'Cleric'
    case 'arch_mage':
      return 'Arch Mage'
    default:
      return ''
  }
}

function formatUnitRank(rank?: string) {
  switch (rank) {
    case 'bronze':
      return 'Bronze'
    case 'silver':
      return 'Silver'
    case 'gold':
      return 'Gold'
    default:
      return 'Base'
  }
}

function summarizeWorkerGroupStatus(units: Unit[]) {
  const gathering = units.filter(
    (unit) => unit.status === 'Mining Gold' || unit.status === 'Chopping Wood',
  ).length
  const returning = units.filter(
    (unit) => unit.status === 'Returning Gold' || unit.status === 'Returning Wood',
  ).length
  const heading = units.filter(
    (unit) => unit.status === 'Heading To Mine' || unit.status === 'Heading To Tree',
  ).length

  if (gathering > 0) return `${gathering} Gathering, ${returning} Returning`
  if (heading > 0) return `${heading} Heading Out`
  if (returning > 0) return `${returning} Returning Resources`
  return 'Worker Crew'
}

function getBuildingResourceLabel(building: BuildingTile) {
  const currentWorkers = getBuildingMetadataNumber(building, 'currentWorkers')
  const maxWorkers = getBuildingMetadataNumber(building, 'maxWorkers')
  if (currentWorkers !== undefined && maxWorkers !== undefined) {
    return `Workers Inside / ${maxWorkers} Max`
  }

  return building.resourceType ? formatResourceLabel(building.resourceType) : undefined
}

function getBuildingResourceAmount(building: BuildingTile) {
  const currentWorkers = getBuildingMetadataNumber(building, 'currentWorkers')
  if (currentWorkers !== undefined) {
    return currentWorkers
  }

  return building.resourceAmount
}

function getBuildingMetadataNumber(building: BuildingTile, key: string) {
  const value = building.metadata?.[key]
  return typeof value === 'number' ? value : undefined
}

function getBuildingMetadataString(building: BuildingTile, key: string) {
  const value = building.metadata?.[key]
  return typeof value === 'string' ? value : undefined
}

function getBuildingProductionState(building: BuildingTile) {
  const unitType = getBuildingMetadataString(building, 'producingUnitType')
  const remainingSeconds = getBuildingMetadataNumber(building, 'productionRemainingSeconds')
  const totalSeconds = getBuildingMetadataNumber(building, 'productionTotalSeconds')
  const queueLength = getBuildingMetadataNumber(building, 'productionQueueLength')
  const queuedUnitTypesRaw = getBuildingMetadataString(building, 'queuedUnitTypes')

  if (!unitType || remainingSeconds === undefined || totalSeconds === undefined) {
    return null
  }

  return {
    unitType,
    remainingSeconds,
    totalSeconds,
    queueLength: Math.max(1, Math.round(queueLength ?? 1)),
    queuedUnitTypes: queuedUnitTypesRaw
      ? queuedUnitTypesRaw.split(',').map((item) => item.trim()).filter(Boolean)
      : [unitType],
  }
}

// Reads the blacksmith upgrade-in-progress metadata (stamped server-side while
// an upgrade is researching) into the same shape as getBuildingProductionState
// so the SelectionHud renders an upgrade exactly like a unit being trained.
// The track string equals a unit type (e.g. "soldier"/"archer") so the unit
// portrait resolves. Returns null when no upgrade is in flight.
function getBuildingUpgradeState(building: BuildingTile) {
  if (building.metadata?.['upgradeInProgress'] !== true) return null
  const track = getBuildingMetadataString(building, 'upgradeTrack')
  const remainingSeconds = getBuildingMetadataNumber(building, 'upgradeRemainingSeconds')
  const totalSeconds = getBuildingMetadataNumber(building, 'upgradeTotalSeconds')
  if (!track || remainingSeconds === undefined || totalSeconds === undefined) {
    return null
  }
  const queueLength = getBuildingMetadataNumber(building, 'upgradeQueueLength')
  const queuedTracksRaw = getBuildingMetadataString(building, 'queuedUpgradeTracks')
  return {
    unitType: track,
    remainingSeconds,
    totalSeconds,
    queueLength: Math.max(1, Math.round(queueLength ?? 1)),
    // Tracks equal unit-type strings ("soldier"/"archer"), so the production
    // queue strip resolves their portraits the same way it does for training.
    queuedUnitTypes: queuedTracksRaw
      ? queuedTracksRaw.split(',').map((item) => item.trim()).filter(Boolean)
      : [track],
  }
}

function toProductionSummary(production: NonNullable<ReturnType<typeof getBuildingProductionState>>): ProductionSummary {
  const progress = production.totalSeconds > 0
    ? 1 - Math.max(0, Math.min(production.remainingSeconds / production.totalSeconds, 1))
    : 1

  return {
    ...production,
    progress,
    timeLabel: formatRemainingSeconds(production.remainingSeconds),
  }
}

function getBuildingSpawnPointLabel(building: BuildingTile) {
  const x = getBuildingMetadataNumber(building, 'spawnPointX')
  const y = getBuildingMetadataNumber(building, 'spawnPointY')
  if (x === undefined || y === undefined) return undefined
  return `${Math.round(x)}, ${Math.round(y)}`
}

function getBuildingStockLabel(building: BuildingTile): string | undefined {
  if (!building.resourceType) return undefined
  return formatResourceLabel(building.resourceType as ResourceType) + ' Remaining'
}

function getBuildingStockAmount(building: BuildingTile): number | undefined {
  if (!building.resourceType) return undefined
  return building.resourceAmount ?? 0
}

function formatSpawnUnitType(unitType: string): string {
  return UNIT_DEF_MAP.get(unitType)?.name ?? unitType
}

function formatRemainingSeconds(seconds: number) {
  if (seconds >= 10) {
    return `${seconds.toFixed(0)}s`
  }

  return `${seconds.toFixed(1)}s`
}

function formatDurability(current: number, max: number) {
  return `${Math.round(current)} / ${Math.round(max)}`
}

function formatObstacleName(obstacleType: ObstacleTile['obstacle']): string {
  switch (obstacleType) {
    case 'tree':
      return 'Tree'
    case 'rock':
      return 'Rock'
    case 'wall':
      return 'Wall'
    default:
      return obstacleType
  }
}

function getObstacleSubtitle(obstacle: ObstacleTile): string {
  if (obstacle.obstacle === 'tree') return 'Harvestable Resource'
  if (obstacle.obstacle === 'rock') return 'Destructible Obstacle'
  return 'Neutral'
}

function formatTrapName(trapType: TrapSnapshot['type']): string {
  switch (trapType) {
    case 'caltrops':
      return 'Caltrops'
    case 'fire_pit':
      return 'Fire Pit'
    case 'explosive_trap':
      return 'Explosive Trap'
    case 'marker_trap':
      return 'Marker Trap'
    default:
      return trapType
  }
}

function getTrapDetails(trap: TrapSnapshot): DetailItem[] {
  const details: DetailItem[] = []
  // Remaining duration — rounded to one decimal for readability. Updates
  // live because getSelectionSummary is recomputed on each tick while the
  // trap's snapshot is present.
  details.push({
    id: 'trap-remaining',
    label: 'Remaining',
    value: `${Math.max(0, trap.remainingSeconds).toFixed(1)}s`,
  })
  details.push({
    id: 'trap-radius',
    label: 'Radius',
    value: `${Math.round(trap.radius)}`,
  })
  return details
}

function getObstacleDetails(obstacle: ObstacleTile): DetailItem[] {
  const details: DetailItem[] = []

  if (obstacle.maxHp !== undefined && obstacle.maxHp > 0) {
    const hp = obstacle.hp ?? obstacle.maxHp
    details.push({
      id: 'durability',
      label: 'Durability',
      value: formatDurability(hp, obstacle.maxHp),
    })
  }

  if (obstacle.resourceType && obstacle.resourceAmount !== undefined) {
    details.push({
      id: 'resource-stock',
      label: `${formatResourceLabel(obstacle.resourceType)} Remaining`,
      value: String(obstacle.resourceAmount),
    })
  }

  const currentWorkers = typeof obstacle.metadata?.['currentWorkers'] === 'number'
    ? (obstacle.metadata['currentWorkers'] as number)
    : undefined
  const maxWorkers = typeof obstacle.metadata?.['maxWorkers'] === 'number'
    ? (obstacle.metadata['maxWorkers'] as number)
    : undefined
  if (currentWorkers !== undefined && maxWorkers !== undefined) {
    details.push({
      id: 'workers-inside',
      label: `Chopping / ${maxWorkers} Max`,
      value: String(currentWorkers),
    })
  }

  return details
}

function getSelectionUnitSubtitle(baseSubtitle: string, unitTargetingMode: UnitTargetingMode | null) {
  switch (unitTargetingMode) {
    case 'move':
      return 'Move order ready. Left-click a destination.'
    case 'gather':
      return 'Gather order ready. Left-click a goldmine or tree.'
    case 'repair':
      return 'Repair/build ready. Left-click a building under construction.'
    case 'patrol':
      return 'Patrol order ready. Left-click a destination.'
    default:
      return baseSubtitle
  }
}

function clampBuildingSpawnPoint(map: MapConfig, _building: BuildingTile, point: Vec2): Vec2 {
  return {
    x: clamp(point.x, 10, map.width - 10),
    y: clamp(point.y, 10, map.height - 10),
  }
}

function clamp(value: number, min: number, max: number) {
  return Math.max(min, Math.min(max, value))
}
