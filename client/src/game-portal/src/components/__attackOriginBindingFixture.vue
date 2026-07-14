<script setup lang="ts">
// Test-only fixture: reproduces the EXACT panel binding — a `<script setup>` ref
// whose member is bound via v-model:attack-origin — so we can prove whether SFC
// compilation of that binding round-trips.
import { ref } from 'vue'
import UnitSpritePreview from './UnitSpritePreview.vue'
import type { UnitAttackOrigin } from '@/game/maps/unitDefs'

const form = ref<{ type: string; attackOrigin?: UnitAttackOrigin }>({ type: 'testunit' })
defineExpose({ form })
</script>

<template>
  <UnitSpritePreview :unit-key="form.type" v-model:attack-origin="form.attackOrigin" />
</template>
