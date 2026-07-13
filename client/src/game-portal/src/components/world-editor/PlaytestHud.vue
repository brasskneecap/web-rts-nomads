<template>
  <div class="playtest-hud">
    <MatchHud :ui="ui" />
    <SelectionHud
      :ui="ui"
      @action="hud.performSelectionAction"
      @select-unit="hud.selectUnitOnly"
      @deselect-unit="hud.deselectUnit"
      @minimap-rect="hud.setMinimapPanelRect"
      @use-consumable="({ unitId, slotIndex }) => hud.sendUseConsumable(unitId, slotIndex)"
      @unequip-item="({ unitId, slotIndex }) => hud.sendUnequipItem(unitId, slotIndex)"
      @equip-item="({ unitId, slotIndex, instanceId }) => hud.sendEquipItem(unitId, slotIndex, instanceId)"
    />
    <CommanderActionBar
      :abilities="ui.commanderAbilities"
      :active-ability-id="ui.commanderTargetingAbilityId"
      @cast="onCommanderCast"
    />
    <div
      v-if="ui.objectives.length || ui.zoneCaptureCards.length || ui.zoneInspection"
      class="playtest-hud__objectives"
    >
      <MatchObjectivesPanel v-if="ui.objectives.length" :objectives="ui.objectives" />
      <ZoneCapturePanel v-if="ui.zoneCaptureCards.length" :cards="ui.zoneCaptureCards" />
      <ZoneInspectionPanel v-if="ui.zoneInspection" :info="ui.zoneInspection" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import MatchHud from '@/components/MatchHud.vue'
import SelectionHud from '@/components/SelectionHud.vue'
import CommanderActionBar from '@/components/CommanderActionBar.vue'
import MatchObjectivesPanel from '@/components/match/MatchObjectivesPanel.vue'
import ZoneCapturePanel from '@/components/match/ZoneCapturePanel.vue'
import ZoneInspectionPanel from '@/components/match/ZoneInspectionPanel.vue'

type HudApi = ReturnType<typeof import('@/composables/useGameClient').useGameClient>

const props = defineProps<{ hud: HudApi }>()

// The live GameUiSnapshot from the shared composable, refreshed per frame by
// the composable's rAF loop.
const ui = computed(() => props.hud.ui.value)

// Mirrors Match.vue's onCommanderCast: clicking the ability that is already
// armed cancels targeting; otherwise begin targeting it.
function onCommanderCast(abilityId: string) {
  if (props.hud.ui.value.commanderTargetingAbilityId === abilityId) {
    props.hud.cancelCommanderAbility()
    return
  }
  props.hud.beginCommanderAbility(abilityId)
}
</script>

<style scoped>
/* Full-viewport passthrough overlay: the child HUD components position
   themselves (fixed/absolute), so this wrapper just needs to not block the
   canvas beneath. No literal cursor declarations (global rules own the cursor). */
.playtest-hud { position: absolute; inset: 0; pointer-events: none; }
.playtest-hud > * { pointer-events: auto; }
.playtest-hud__objectives { position: absolute; top: 12px; left: 12px; z-index: 20; }
</style>
