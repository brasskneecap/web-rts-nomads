// Shared data shapes for the Vault redesign. The orchestrator (VaultPanel)
// builds these from raw game state and passes them into the presentational
// child components by props, so the cards never reach into global state.

export type VaultRank = 'bronze' | 'silver' | 'gold'

/** A single granted perk rendered on a unit card. `iconId` is the ActionIcon
 *  lookup id (the perk def's icon key); title/body feed the existing perk
 *  tooltip. */
export interface VaultPerkChip {
  id: string
  iconId: string
  title: string
  body: string
}

/** The item occupying a unit inventory slot, pre-resolved for display. */
export interface VaultSlotItem {
  instanceId: number
  itemId: string
  displayName: string
  tier?: string
  tierColor: string
  tooltipBody: string
  isConsumable: boolean
}

/** One rank-tied inventory slot. Exactly three per unit: bronze, silver, gold.
 *  `locked` means the unit has not yet reached the rank that unlocks the slot. */
export interface VaultInventorySlot {
  rank: VaultRank
  slotIndex: number
  locked: boolean
  item: VaultSlotItem | null
}

/** A storage-grid entry, pre-resolved for display. */
export interface VaultStorageItem {
  instanceId: number
  itemId: string
  displayName: string
  tier?: string
  tierColor: string
  tooltipBody: string
  stacks?: number
}

/** The currently-selected storage item, shown in the details panel. */
export interface VaultSelectedItem {
  itemId: string
  displayName: string
  tier?: string
  tierColor: string
  description?: string
  /** Stat line, e.g. "+5 Damage, +2 Armor" or "Heals 50 HP". */
  stats?: string
}

/** Everything a unit card needs to render, fully derived upstream. */
export interface VaultUnitCardData {
  id: number
  portraitUrl: string | null
  initials: string
  /** Specialization/path name, or the base unit name when unpathed. */
  specializationName: string
  rank: string
  rankChevrons: number
  rankColor: string
  /** XP into the current rank and the threshold for the next, or null. */
  xpInto: number | null
  xpToNext: number | null
  isMaxRank: boolean
  perks: VaultPerkChip[]
  inventory: VaultInventorySlot[]
  /** True when this unit can receive the currently-selected item (always true
   *  when no item is selected). */
  eligible: boolean
  /** True when the unit is eligible and has at least one empty unlocked slot. */
  hasEmptyMatchingSlot: boolean
}
