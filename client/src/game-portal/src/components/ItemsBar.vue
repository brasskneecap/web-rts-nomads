<template>
  <!-- Consumable items row. 12 fixed slots showing the player's potions
       (vault entries whose def kind is 'consumable'). Clicking a filled slot
       arms ground-AoE targeting for that item: the canvas shows the item's
       range circle at the cursor and the next world click uses it there,
       splitting the effect across the allied units hit. Clicking the armed
       slot again cancels. Visibility is toggled by the "Items" launcher
       button; geometry: 25px above the commander ability bar, left edge
       aligned with the launcher's first button (Shop). -->
  <div
    class="items-bar"
    role="toolbar"
    aria-label="Consumable items"
    :style="{ '--ui-icon-container-image': `url(${iconContainerUrl})` }"
  >
    <component
      :is="slot.entry ? 'button' : 'div'"
      v-for="slot in slots"
      :key="slot.index"
      class="items-bar__slot"
      :class="{
        'items-bar__slot--empty': !slot.entry,
        'items-bar__slot--active': slot.entry && slot.entry.instanceId === activeInstanceId,
      }"
      :type="slot.entry ? 'button' : undefined"
      :aria-label="slot.entry ? slot.entry.displayName : 'Empty item slot'"
      @click="slot.entry ? emit('use', slot.entry.instanceId, slot.entry.itemId) : undefined"
      @mouseenter="slot.entry ? onSlotEnter($event, slot.entry) : undefined"
      @mouseleave="onSlotLeave"
    >
      <template v-if="slot.entry">
        <ActionIcon
          class="items-bar__icon"
          :action="{ id: slot.entry.itemId, label: slot.entry.displayName, iconDef: { kind: 'item', type: slot.entry.itemId } }"
        />
        <span v-if="slot.entry.stacks > 1" class="items-bar__stack">{{ slot.entry.stacks }}</span>
      </template>
    </component>
  </div>

  <ItemHoverTooltip :item="hoveredTooltip" :anchor="anchorRect" />
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import ActionIcon from '@/components/ActionIcon.vue'
import ItemHoverTooltip, { type ItemTooltipData } from '@/components/ItemHoverTooltip.vue'
import type { VaultItemSnapshot } from '@/game/network/protocol'
import { ITEM_DEF_MAP } from '@/game/maps/itemDefs'
import { TIER_COLORS, buildItemTooltipBody } from '@/game/items/itemRules'
import iconContainerUrl from '@/assets/ui/themes/default/icon-container.png'

const ITEMS_BAR_SLOTS = 12

interface ItemBarEntry {
  instanceId: number
  itemId: string
  displayName: string
  tier?: string
  tierColor: string
  tooltipBody: string
  stacks: number
}

const props = defineProps<{
  /** The player's full vault; this bar shows only consumable entries. */
  vault: VaultItemSnapshot[]
  /** InstanceId of the item currently armed for AoE targeting, or null. */
  activeInstanceId: number | null
}>()

const emit = defineEmits<{
  /** A filled slot was clicked — arm (or toggle off) targeting for it. */
  use: [instanceId: number, itemId: string]
}>()

const entries = computed<ItemBarEntry[]>(() =>
  props.vault
    .filter((snap) => ITEM_DEF_MAP.get(snap.itemId)?.kind === 'consumable')
    .slice(0, ITEMS_BAR_SLOTS)
    .map((snap) => {
      const def = ITEM_DEF_MAP.get(snap.itemId)
      return {
        instanceId: snap.instanceId,
        itemId: snap.itemId,
        displayName: def?.displayName ?? snap.itemId,
        tier: def?.tier,
        tierColor: def?.tier ? TIER_COLORS[def.tier] : TIER_COLORS.common,
        tooltipBody: def ? buildItemTooltipBody(def) : '',
        stacks: snap.stacks ?? 1,
      }
    }),
)

const slots = computed(() =>
  Array.from({ length: ITEMS_BAR_SLOTS }, (_, index) => ({
    index,
    entry: entries.value[index] ?? null,
  })),
)

// ── Floating tooltip (shared ItemHoverTooltip, teleported to body) ──────────
const hovered = ref<ItemBarEntry | null>(null)
const anchorRect = ref<DOMRect | null>(null)

const hoveredTooltip = computed<ItemTooltipData | null>(() => {
  const item = hovered.value
  if (!item) return null
  return {
    displayName: item.displayName,
    tier: item.tier,
    tierColor: item.tierColor,
    body: item.tooltipBody,
    hint: 'Click, then click on your units to use',
  }
})

function onSlotEnter(e: MouseEvent, entry: ItemBarEntry) {
  anchorRect.value = (e.currentTarget as HTMLElement).getBoundingClientRect()
  hovered.value = entry
}

function onSlotLeave() {
  hovered.value = null
}
</script>

<style scoped>
.items-bar {
  /* Geometry: the MatchMenuLauncher strip sits at bottom: 210px with its
     embedded commander ability slots 70px tall and bottom-aligned, so the
     ability bar's top edge is at 280px from the viewport bottom. This row
     sits 25px above that. Left edge matches the launcher's first button
     (Shop): launcher left calc(50% - 400px) + 8px padding. */
  position: absolute;
  bottom: 305px;
  left: calc(50% - 392px);
  z-index: 6;
  display: flex;
  flex-direction: row;
  gap: 6px;
  pointer-events: auto;
  user-select: none;
}

.items-bar__slot {
  position: relative;
  /* 56px matches the launcher's icon buttons (+25% over the original 45px)
     so the item slots and the action row read as one system. */
  width: 56px;
  height: 56px;
  padding: 0;
  border: 0;
  border-radius: 0;
  background: var(--ui-icon-container-image) center / 100% 100% no-repeat;
  image-rendering: pixelated;
  box-sizing: border-box;
  flex: 0 0 auto;
}

.items-bar__slot--empty {
  opacity: 0.55;
}

.items-bar__slot:not(.items-bar__slot--empty):hover {
  box-shadow: var(--ui-hover-glow);
}

/* Armed for targeting: gold ring + glow, matching the commander ability
   active-slot language. */
.items-bar__slot--active {
  box-shadow:
    inset 0 0 0 2px rgba(255, 226, 138, 0.7),
    0 0 12px rgba(255, 200, 80, 0.45);
}

.items-bar__slot--active:hover {
  box-shadow:
    inset 0 0 0 2px rgba(255, 226, 138, 0.7),
    0 0 12px rgba(255, 200, 80, 0.45),
    var(--ui-hover-glow);
}

.items-bar__icon {
  position: absolute;
  top: 50%;
  left: 50%;
  width: 70%;
  height: 70%;
  transform: translate(-50%, -50%);
  pointer-events: none;
}

.items-bar__stack {
  position: absolute;
  bottom: 2px;
  right: 3px;
  font-size: 10px;
  font-weight: 700;
  color: #fff;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.8);
  pointer-events: none;
  line-height: 1;
}
</style>
