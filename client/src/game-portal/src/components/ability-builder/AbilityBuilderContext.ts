// provide/inject wiring for the composable ability editor. AbilityBuilderPanel
// owns the single useAbilityBuilder() instance and provides it; every child
// region (AbilityOverviewCard, AbilityFlow, IdentityTab, InspectorBar, …)
// injects it via useAbilityBuilderContext() instead of receiving it as a prop,
// so adding a new region never requires touching the panel's prop list.

import { inject, type InjectionKey } from 'vue'
import type { useAbilityBuilder } from './useAbilityBuilder'

export type AbilityBuilder = ReturnType<typeof useAbilityBuilder>

export const AbilityBuilderKey: InjectionKey<AbilityBuilder> = Symbol('abilityBuilder')

export function useAbilityBuilderContext(): AbilityBuilder {
  const builder = inject(AbilityBuilderKey)
  if (!builder) throw new Error('AbilityBuilder not provided')
  return builder
}
