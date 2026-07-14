<template>
  <UiPanel variant="warRoomInner" :padding="0" class="ed-side">
    <div class="ed-side__inner">
      <div class="ed-side__head">
        <span class="ed-side__title">{{ title }}</span>
      </div>

      <div class="ed-side__actions">
        <UiButton size="sm" variant="active" @click="emit('new')">{{ newLabel }}</UiButton>
        <input
          :value="search"
          type="search"
          :placeholder="searchPlaceholder"
          :aria-label="searchPlaceholder"
          @input="emit('update:search', ($event.target as HTMLInputElement).value)"
        />
      </div>

      <GameScrollArea class="ed-side__scroll">
        <div v-for="group in groups" :key="group.label" class="ed-side__group">
          <div class="ed-side__group-label" :style="{ color: group.color }">
            {{ group.label }} <span class="ed-side__count">({{ group.entries.length }})</span>
          </div>

          <div
            v-for="entry in group.entries"
            :key="entry.id"
            class="ed-side__row"
            :class="{ 'ed-side__row--on': entry.id === selectedId }"
          >
            <button type="button" class="ed-side__pick" @click="emit('select', entry.id)">
              <img v-if="entry.iconUrl" :src="entry.iconUrl" class="ed-side__icon" alt="" />
              <span class="ed-side__name" :style="{ color: entry.color }">{{ entry.name }}</span>
            </button>

            <!-- Duplicate is the only row action: Reset / Delete lives beside
                 Save in the header, where it acts on the def you have open. -->
            <span class="ed-side__row-actions">
              <button
                type="button"
                class="ed-side__act"
                title="Duplicate"
                aria-label="Duplicate"
                @click.stop="emit('duplicate', entry.id)"
              >⧉</button>
            </span>
          </div>
        </div>

        <p v-if="groups.length === 0" class="ed-side__empty">{{ emptyText }}</p>
      </GameScrollArea>
    </div>
  </UiPanel>
</template>

<script setup lang="ts">
import UiPanel from '@/components/ui/UiPanel.vue'
import UiButton from '@/components/ui/UiButton.vue'
import GameScrollArea from '@/components/ui/GameScrollArea.vue'

/** One selectable def in the list. */
export type SidebarEntry = {
  id: string
  name: string
  iconUrl?: string
  /** Accent for the name (tier color for items, faction for units, …). */
  color?: string
}

export type SidebarGroup = {
  label: string
  color?: string
  entries: SidebarEntry[]
}

withDefaults(defineProps<{
  title: string
  groups: SidebarGroup[]
  selectedId?: string
  search: string
  searchPlaceholder?: string
  emptyText?: string
  /** Label for the create button — "Add New Item", "Add New Unit", … */
  newLabel?: string
}>(), {
  searchPlaceholder: 'Search…',
  emptyText: 'Nothing matches.',
  newLabel: 'Add New',
})

const emit = defineEmits<{
  select: [string]
  new: []
  duplicate: [string]
  'update:search': [string]
}>()
</script>

<style scoped>
.ed-side {
  height: 100%;
  min-height: 0;
}

.ed-side__inner {
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px;
  box-sizing: border-box;
}

.ed-side__head {
  padding-bottom: 6px;
  border-bottom: 1px solid var(--ed-line);
}

.ed-side__title {
  font-family: var(--font-title);
  font-size: 0.86rem;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--ed-brass);
}

.ed-side__actions {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.ed-side__scroll {
  flex: 1 1 auto;
  min-height: 0;
}

.ed-side__group {
  margin-bottom: 10px;
}

.ed-side__group-label {
  font-size: 0.66rem;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  padding: 4px 2px;
}

.ed-side__count {
  opacity: 0.6;
  font-weight: 400;
}

.ed-side__row {
  display: flex;
  align-items: center;
  border-radius: var(--ed-radius);
}

.ed-side__row:hover,
.ed-side__row--on {
  background: rgba(212, 168, 71, 0.1);
}

.ed-side__row--on {
  box-shadow: inset 2px 0 0 var(--ed-brass);
}

.ed-side__pick {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 6px;
  background: none;
  border: 0;
  text-align: left;
}

.ed-side__icon {
  width: 22px;
  height: 22px;
  flex: 0 0 auto;
  object-fit: contain;
  image-rendering: pixelated;
}

.ed-side__name {
  font-size: 0.8rem;
  color: var(--ed-text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* Actions stay hidden until the row is hovered or selected, so the list reads
   as a list rather than a toolbar. */
.ed-side__row-actions {
  display: flex;
  gap: 2px;
  padding-right: 4px;
  opacity: 0;
}

.ed-side__row:hover .ed-side__row-actions,
.ed-side__row--on .ed-side__row-actions {
  opacity: 1;
}

.ed-side__act {
  padding: 2px 5px;
  font-size: 0.76rem;
  line-height: 1;
  color: var(--ed-text-dim);
  background: none;
  border: 1px solid transparent;
  border-radius: 4px;
}

.ed-side__act:hover {
  color: var(--ed-brass);
  border-color: var(--ed-line);
}

.ed-side__empty {
  margin: 8px 2px;
  font-size: 0.78rem;
  color: var(--ed-text-dim);
}
</style>
