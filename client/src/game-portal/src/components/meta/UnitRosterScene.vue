<template>
  <MetaSceneView :bg="bg" :title="title">
    <!-- Full advancement modal: opens when a unit is selected, showing that
         unit's advancement track in the reference-style wood/brass panel. -->
    <UnitAdvancementPanel
      v-if="selectedEntry"
      :unit-type="selectedEntry.id"
      :unit-name="selectedEntry.label"
      :portrait="selectedEntry.portrait"
      @close="selectedUnit = null"
    />

    <aside
      class="roster"
      :style="{ '--ui-icon-container-image': `url(${iconContainerUrl})` }"
      :aria-label="`${title} units`"
    >
      <div v-for="u in units" :key="u.id" class="roster__unit">
        <button
          type="button"
          class="roster__slot"
          :class="{ 'roster__slot--active': selectedUnit === u.id }"
          :aria-label="u.label"
          @click="selectUnit(u.id)"
        >
          <img class="roster__portrait" :src="u.portrait" :alt="u.label" />
        </button>

        <div v-if="u.paths.length" class="roster__paths">
          <button
            v-for="p in u.paths"
            :key="p.id"
            type="button"
            class="roster__slot roster__slot--path"
            :aria-label="p.label"
          >
            <img class="roster__portrait" :src="p.portrait" :alt="p.label" />
          </button>
        </div>
      </div>
    </aside>
  </MetaSceneView>
</template>

<script lang="ts">
export interface RosterUnit {
  id: string
  label: string
  portrait: string
}

export interface RosterEntry extends RosterUnit {
  paths: ReadonlyArray<RosterUnit>
}
</script>

<script setup lang="ts">
import { computed, ref } from 'vue'
import MetaSceneView from '@/components/meta/MetaSceneView.vue'
import UnitAdvancementPanel from '@/components/meta/UnitAdvancementPanel.vue'
import iconContainerUrl from '@/assets/ui/themes/updated/icon_container.png'

const props = defineProps<{
  bg: string
  title: string
  units: ReadonlyArray<RosterEntry>
}>()

// Which unit's advancement detail is open in the middle section. `null` closes
// it. Clicking the active unit again toggles it shut.
const selectedUnit = ref<string | null>(null)

// The full roster entry (id/label/portrait) for the open unit, driving the
// advancement modal's title and portrait.
const selectedEntry = computed(() =>
  props.units.find((u) => u.id === selectedUnit.value) ?? null,
)

function selectUnit(id: string) {
  selectedUnit.value = selectedUnit.value === id ? null : id
}
</script>

<style scoped>
/*
 * Horizontal roster bar along the bottom of the cover-fit scene, centered and
 * sitting above the exit button. Fixed pixel sizing keeps it static.
 */
.roster {
  position: absolute;
  left: 50%;
  bottom: 120px;
  transform: translateX(-50%);
  display: flex;
  flex-direction: row;
  align-items: center;
  gap: 28px;
  z-index: 1;
}

/* A unit plus its paths form one horizontal group. */
.roster__unit {
  display: flex;
  flex-direction: row;
  align-items: center;
  gap: 10px;
}

/* Path portraits sit beside their parent unit, slightly smaller, with a small
   indent separating them from the parent so the hierarchy reads at a glance. */
.roster__paths {
  display: flex;
  flex-direction: row;
  align-items: center;
  gap: 10px;
  padding-left: 12px;
}

/* Each slot is the shared icon-container frame, same idiom as the HUD slots.
   Fixed pixel size so the panel stays static regardless of page size. */
.roster__slot {
  position: relative;
  width: 100px;
  aspect-ratio: 1 / 1;
  padding: 0;
  border: 0;
  background: var(--ui-icon-container-image) center / 100% 100% no-repeat;
  image-rendering: pixelated;
  transition: box-shadow 0.12s;
}

.roster__slot--path {
  width: 72px;
}

.roster__slot:hover:not(:disabled) {
  box-shadow: var(--ui-hover-glow);
}

/* Active unit: gold ring matching the HUD active-slot idiom. */
.roster__slot--active {
  box-shadow:
    inset 0 0 0 2px rgba(255, 226, 138, 0.7),
    0 0 18px rgba(255, 200, 80, 0.45);
}

/* Portrait rendered inside the frame so the icon-container's outer edge stays
   visible — matches the 78% inset used for portraits elsewhere. */
.roster__portrait {
  position: absolute;
  top: 50%;
  left: 50%;
  width: 78%;
  height: 78%;
  transform: translate(-50%, -50%);
  object-fit: cover;
  image-rendering: pixelated;
}

</style>
