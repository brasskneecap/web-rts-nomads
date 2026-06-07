export type AcquiredAdvancement = {
  id: string
  costPaid: number
}

export type UnitAdvancementEffect =
  | { kind: 'unitStatAdd'; stat: 'maxHp' | 'armor' | 'damage' | 'attackRange' | 'moveSpeed' | 'attackSpeed'; amount: number }
  | { kind: 'unitStatMul'; stat: 'maxHp' | 'armor' | 'damage' | 'attackRange' | 'moveSpeed' | 'attackSpeed'; percent: number }
  | { kind: 'unitSpawnExp'; amount: number }
  | { kind: 'unitExtraPerkSlot'; tier: 'bronze' | 'silver' | 'gold'; rank: number }
  | { kind: 'unitBonusArrows'; amount: number }
  | { kind: 'unitTrapEffectMul'; percent: number }
  | { kind: 'unitTrapRadiusMul'; percent: number }

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
  legendPoints: number
  lifetimeLegendPoints: number
  ownedCommanderIds: string[]
  selectedCommanderId: string
  activeUpgradeIds: string[]
  ownedUpgradeRanks: Record<string, number>
  acquiredAdvancements: AcquiredAdvancement[]
  completedCampaignLevels: string[]
  stats: ProfileStats
}

export type ProfileUpgradeEffect =
  | { type: 'extraStartingUnit'; unitType: string; countPerRank: number }
  | { type: 'damageMultiplierByType'; damageTypeClass: 'physical' | 'nonPhysical'; multiplierPerRank: number }

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
  legendPoints: {
    winBonus: number
    lossConsolation: number
    perObjective: number
    perKillBaseDropChance: number
    perKillBaseAmount: number
  }
}
