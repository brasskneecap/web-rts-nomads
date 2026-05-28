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
