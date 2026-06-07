<template>
  <UnitRosterScene :bg="bg" title="Barracks" :units="UNITS" />
</template>

<script setup lang="ts">
import UnitRosterScene, { type RosterEntry } from '@/components/meta/UnitRosterScene.vue'
import bg from '@/assets/background-images/meta-views/barracks-view.png'
import soldierPortrait from '@/assets/units/human/soldier/portrait.png'
import archerPortrait from '@/assets/units/human/archer/portrait.png'
import berserkerPortrait from '@/assets/units/human/soldier/paths/berserker/portrait.png'
import vanguardPortrait from '@/assets/units/human/soldier/paths/vanguard/portrait.png'
import marksmanPortrait from '@/assets/units/human/archer/paths/marksman/portrait.png'
import trapperPortrait from '@/assets/units/human/archer/paths/trapper/portrait.png'

const UNITS: ReadonlyArray<RosterEntry> = [
  {
    id: 'soldier',
    label: 'Soldier',
    portrait: soldierPortrait,
    paths: [
      { id: 'berserker', label: 'Berserker', portrait: berserkerPortrait },
      { id: 'vanguard', label: 'Vanguard', portrait: vanguardPortrait },
    ],
  },
  {
    id: 'archer',
    label: 'Archer',
    portrait: archerPortrait,
    paths: [
      { id: 'marksman', label: 'Marksman', portrait: marksmanPortrait },
      { id: 'trapper', label: 'Trapper', portrait: trapperPortrait },
    ],
  },
]
</script>
