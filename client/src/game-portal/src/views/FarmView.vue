<template>
  <UnitRosterScene :bg="bg" title="Farm" :units="UNITS" />
</template>

<script setup lang="ts">
import UnitRosterScene, { type RosterEntry } from '@/components/meta/UnitRosterScene.vue'
import bg from '@/assets/background-images/meta-views/farm-view.png'
import workerPortrait from '@/assets/units/human/worker/portrait.png'

const UNITS: ReadonlyArray<RosterEntry> = [
  {
    id: 'worker',
    label: 'Worker',
    portrait: workerPortrait,
    paths: [],
  },
]
</script>
