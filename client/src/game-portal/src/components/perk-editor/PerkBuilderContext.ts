// PerkBuilderContext.ts — provide/inject wiring, mirrors AbilityBuilderContext.
import { inject, type InjectionKey } from 'vue'
import type { usePerkBuilder } from './usePerkBuilder'

export type PerkBuilder = ReturnType<typeof usePerkBuilder>
export const PerkBuilderKey: InjectionKey<PerkBuilder> = Symbol('perkBuilder')

export function usePerkBuilderContext(): PerkBuilder {
  const b = inject(PerkBuilderKey)
  if (!b) throw new Error('PerkBuilder not provided')
  return b
}
