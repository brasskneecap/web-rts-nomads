export type AcquiredAdvancement = {
  id: string
  costPaid: number
  badgesPaid?: number
}

export type UnitAdvancementEffect =
  | { kind: 'unitStatAdd'; stat: 'maxHp' | 'armor' | 'damage' | 'attackRange' | 'moveSpeed' | 'attackSpeed' | 'goldGatherAmount' | 'woodGatherAmount' | 'goldCost'; amount: number }
  | { kind: 'unitStatMul'; stat: 'maxHp' | 'armor' | 'damage' | 'attackRange' | 'moveSpeed' | 'attackSpeed'; percent: number }
  | { kind: 'unitSpawnExp'; amount: number }
  | { kind: 'unitExtraPerkSlot'; tier: 'bronze' | 'silver' | 'gold'; rank: number }
  | { kind: 'unitBonusArrows'; amount: number }
  | { kind: 'unitTrapEffectMul'; percent: number }
  | { kind: 'unitTrapRadiusMul'; percent: number }
  | { kind: 'unitExtraStartingUnit'; amount: number }

export type UnitAdvancementNode = {
  id: string
  name: string
  description: string
  kind: 'minor' | 'major'
  cost: number
  effects: UnitAdvancementEffect[]
}

export type UnitAdvancementTrack = {
  unitType: string
  nodes: UnitAdvancementNode[]
}

export type PlayerProfile = {
  playerId: string
  version: number
  createdAtUnix: number
  updatedAtUnix: number
  dominionPoints: number
  lifetimeDominionPoints: number
  conquestBadges: number
  ownedCommanderIds: string[]
  selectedCommanderId: string
  activeUpgradeIds: string[]
  ownedUpgradeRanks: Record<string, number>
  acquiredAdvancements: AcquiredAdvancement[]
  /** ITEM IDs whose recipes this player has learned — an item is its own recipe. */
  knownCraftableIds: string[]
  completedCampaignLevels: string[]
  /** Per-level union of objective IDs the player has ever completed.
   *  Keys are `"<campaignId>/<levelId>"`. Written by the §11
   *  complete-objectives endpoint at match end. */
  completedCampaignObjectives: Record<string, string[]>
  stats: ProfileStats
}

export type ProfileUpgradeEffect =
  | { type: 'extraStartingUnit'; unitType: string; countPerRank: number }
  | { type: 'damageMultiplierByType'; damageTypeClass: 'physical' | 'nonPhysical'; multiplierPerRank: number }
  | { type: 'startingResource'; resourceType: string; amountPerRank: number }

export type ProfileUpgradeDef = {
  id: string
  name: string
  description: string
  maxRanks: number
  costPerRank: number[]
  effect: ProfileUpgradeEffect
}

export type ProfileStats = {
  matchesPlayed: number
  matchesWon: number
  matchesLost: number
  enemiesKilled: number
  objectivesDone: number
}

export type GameplayTuning = {
  version: number
  dominionPoints: {
    winBonus: number
    lossConsolation: number
    perObjective: number
    perKillBaseDropChance: number
    perKillBaseAmount: number
  }
}
