<template>
  <div v-if="visibleBuffs.length > 0" class="buff-strip" aria-label="Active player buffs">
    <div
      v-for="buff in visibleBuffs"
      :key="buff.id"
      class="buff-strip__item"
    >
      <div class="buff-strip__icon-wrap" :title="tooltipFor(buff)">
        <BuffIcon :icon-key="buff.iconKey" :label="buff.displayName" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useProfile } from '@/composables/useProfile'
import BuffIcon from './BuffIcon.vue'
import type { PlayerBuffDef } from '@/types/profile'

const { equippedBuffs } = useProfile()

// The backend will add activeBuffs: ActiveEffectIcon[] to PlayerSnapshot.
// Until that field exists, we display the profile's equipped buffs as a
// static representation of what's active in-match.
// TODO: wire to PlayerSnapshot.activeBuffs once the backend ships it — resolve
// each ActiveEffectIcon.id against buffCatalog for display name + description.
const visibleBuffs = computed<PlayerBuffDef[]>(() => equippedBuffs.value)

function tooltipFor(buff: PlayerBuffDef): string {
  return buff.description ? `${buff.displayName}: ${buff.description}` : buff.displayName
}
</script>

<style scoped>
.buff-strip {
  display: flex;
  gap: 4px;
  align-items: center;
}

.buff-strip__item {
  position: relative;
}

.buff-strip__icon-wrap {
  cursor: default;
}
</style>
