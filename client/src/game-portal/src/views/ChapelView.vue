<template>
  <UnitRosterScene :bg="bg" title="Chapel" :units="UNITS" />
</template>

<script setup lang="ts">
import UnitRosterScene, { type RosterEntry } from '@/components/meta/UnitRosterScene.vue'
import bg from '@/assets/background-images/meta-views/chapel-view.png'
import acolytePortrait from '@/assets/units/human/acolyte/portrait.png'
import clericPortrait from '@/assets/units/human/acolyte/paths/cleric/portrait.png'
import siphonerPortrait from '@/assets/units/human/acolyte/paths/siphoner/portrait.png'

const UNITS: ReadonlyArray<RosterEntry> = [
  {
    id: 'acolyte',
    label: 'Acolyte',
    portrait: acolytePortrait,
    paths: [
      { id: 'cleric', label: 'Cleric', portrait: clericPortrait },
      { id: 'siphoner', label: 'Siphoner', portrait: siphonerPortrait },
    ],
  },
]
</script>
