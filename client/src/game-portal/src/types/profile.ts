export type PlayerProfile = {
  playerId: string
  version: number
  createdAtUnix: number
  updatedAtUnix: number
  legendPoints: number
  lifetimeLegendPoints: number
  ownedCommanderIds: string[]
  selectedCommanderId: string
  equippedBuffIds: string[]
  unlockedBuffIds: string[]
  stats: ProfileStats
}

export type ProfileStats = {
  matchesPlayed: number
  matchesWon: number
  matchesLost: number
  enemiesKilled: number
  objectivesDone: number
}

export type PlayerBuffDef = {
  id: string
  displayName: string
  description?: string
  iconKey: string
  unlockLegendPointCost: number
  /** "ownedUnits" (default) boosts own units; "enemyUnits" boosts enemies (self-debuff). */
  appliesTo?: 'ownedUnits' | 'enemyUnits'
  modifiers: PlayerBuffModifiers
  allowedUnitTypes?: string[]
}

export type PlayerBuffModifiers = {
  hpBonus?: number
  damageBonus?: number
  armorBonus?: number
  attackSpeedBonus?: number
  moveSpeedMultBonus?: number
  bonusDamageMult?: number
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
  buffSlots: {
    maxActive: number
  }
}
