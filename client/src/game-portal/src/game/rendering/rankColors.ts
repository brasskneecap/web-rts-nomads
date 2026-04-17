export type RankTone = 'base' | 'light' | 'dark'

export function getRankToneColor(rank?: string, tone: RankTone = 'base'): string {
  switch (rank) {
    case 'bronze':
      switch (tone) {
        case 'light': return '#fbbf24'
        case 'dark': return '#92400e'
        default: return '#d97706'
      }
    case 'silver':
      switch (tone) {
        case 'light': return '#f8fafc'
        case 'dark': return '#94a3b8'
        default: return '#cbd5e1'
      }
    case 'gold':
      switch (tone) {
        case 'light': return '#fde047'
        case 'dark': return '#ca8a04'
        default: return '#facc15'
      }
    default:
      switch (tone) {
        case 'light': return '#ffffff'
        case 'dark': return '#cbd5e1'
        default: return '#f8fafc'
      }
  }
}
