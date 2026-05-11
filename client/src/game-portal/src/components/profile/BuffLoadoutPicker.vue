<template>
  <div class="buff-picker">
    <div class="buff-picker__section-label">Equipped Buffs</div>
    <div class="buff-picker__slots" :aria-label="`Buff slots (${maxBuffSlots} max)`">
      <button
        v-for="slotIdx in maxBuffSlots"
        :key="slotIdx"
        class="buff-slot"
        :class="{
          'buff-slot--filled': equippedSlots[slotIdx - 1] != null,
          'buff-slot--selected': selectedSlotIdx === slotIdx - 1,
        }"
        type="button"
        :aria-label="equippedSlots[slotIdx - 1] ? `Remove ${equippedSlots[slotIdx - 1]!.displayName}` : `Empty slot ${slotIdx}`"
        :disabled="isLoading"
        @click="onSlotClick(slotIdx - 1)"
      >
        <template v-if="equippedSlots[slotIdx - 1]">
          <BuffIcon :icon-key="equippedSlots[slotIdx - 1]!.iconKey" :label="equippedSlots[slotIdx - 1]!.displayName" />
          <span class="buff-slot__name">{{ equippedSlots[slotIdx - 1]!.displayName }}</span>
        </template>
        <template v-else>
          <span class="buff-slot__empty" aria-hidden="true">+</span>
          <span class="buff-slot__name buff-slot__name--empty">Empty</span>
        </template>
      </button>
    </div>

    <div v-if="unlockedBuffs.length > 0" class="buff-picker__section-label">Available Buffs</div>
    <div v-if="unlockedBuffs.length > 0" class="buff-picker__list" aria-label="Unlocked buffs">
      <button
        v-for="buff in unlockedBuffs"
        :key="buff.id"
        class="buff-item buff-item--unlocked"
        :class="{ 'buff-item--equipped': isEquipped(buff.id) }"
        type="button"
        :aria-label="`${buff.displayName}${isEquipped(buff.id) ? ' (equipped)' : ''}`"
        :disabled="isLoading"
        @click="onBuffClick(buff.id)"
      >
        <BuffIcon :icon-key="buff.iconKey" :label="buff.displayName" />
        <div class="buff-item__copy">
          <div class="buff-item__name">{{ buff.displayName }}</div>
          <div v-if="buff.description" class="buff-item__desc">{{ buff.description }}</div>
        </div>
        <span v-if="isEquipped(buff.id)" class="buff-item__badge">Equipped</span>
      </button>
    </div>

    <div v-if="lockedBuffs.length > 0" class="buff-picker__section-label">Locked Buffs</div>
    <div v-if="lockedBuffs.length > 0" class="buff-picker__list buff-picker__list--locked" aria-label="Locked buffs">
      <div
        v-for="buff in lockedBuffs"
        :key="buff.id"
        class="buff-item buff-item--locked"
        :aria-label="`${buff.displayName} — costs ${buff.unlockLegendPointCost} legend points`"
      >
        <BuffIcon :icon-key="buff.iconKey" :label="buff.displayName" locked />
        <div class="buff-item__copy">
          <div class="buff-item__name">{{ buff.displayName }}</div>
          <div v-if="buff.description" class="buff-item__desc">{{ buff.description }}</div>
        </div>
        <span class="buff-item__cost">{{ buff.unlockLegendPointCost }} LP</span>
      </div>
    </div>

    <div v-if="error" class="buff-picker__error" role="alert">{{ error }}</div>

    <div class="buff-picker__footer">
      <button
        class="buff-picker__save"
        type="button"
        :disabled="isLoading || !isDirty"
        @click="saveLoadout"
      >
        {{ isLoading ? 'Saving...' : 'Save Loadout' }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useProfile } from '@/composables/useProfile'
import BuffIcon from './BuffIcon.vue'

const {
  maxBuffSlots,
  equippedBuffs,
  unlockedBuffs,
  lockedBuffs,
  isLoading,
  error,
  updateLoadout,
  profile,
} = useProfile()

// Pending slot state — starts from the server-provided equipped list and
// diverges until "Save Loadout" is pressed.
const pendingIds = ref<(string | null)[]>([])

function syncPendingFromProfile() {
  const equipped = profile.value?.equippedBuffIds ?? []
  const slots: (string | null)[] = []
  for (let i = 0; i < maxBuffSlots.value; i++) {
    slots.push(equipped[i] ?? null)
  }
  pendingIds.value = slots
}

// Sync on first mount and when profile changes from the server.
syncPendingFromProfile()

const equippedSlots = computed(() => {
  const catalog = new Map(equippedBuffs.value.concat(unlockedBuffs.value).map((d) => [d.id, d]))
  return pendingIds.value.map((id) => (id ? (catalog.get(id) ?? null) : null))
})

const selectedSlotIdx = ref<number | null>(null)

const isDirty = computed(() => {
  const server = profile.value?.equippedBuffIds ?? []
  const local = pendingIds.value.filter((id): id is string => id !== null)
  if (local.length !== server.length) return true
  return local.some((id, i) => id !== server[i])
})

function isEquipped(buffId: string): boolean {
  return pendingIds.value.includes(buffId)
}

function onSlotClick(idx: number) {
  if (pendingIds.value[idx] !== null) {
    // Deselect/remove the buff from this slot.
    const updated = [...pendingIds.value]
    updated[idx] = null
    pendingIds.value = updated
    if (selectedSlotIdx.value === idx) selectedSlotIdx.value = null
    return
  }
  selectedSlotIdx.value = idx === selectedSlotIdx.value ? null : idx
}

function onBuffClick(buffId: string) {
  const updated = [...pendingIds.value]

  // If already equipped, remove it.
  const existingIdx = updated.indexOf(buffId)
  if (existingIdx !== -1) {
    updated[existingIdx] = null
    pendingIds.value = updated
    return
  }

  // Place into selected slot, or the first empty slot, or do nothing if full.
  const targetIdx = selectedSlotIdx.value !== null && updated[selectedSlotIdx.value] === null
    ? selectedSlotIdx.value
    : updated.indexOf(null)

  if (targetIdx === -1) return

  updated[targetIdx] = buffId
  pendingIds.value = updated
  selectedSlotIdx.value = null
}

async function saveLoadout() {
  const ids = pendingIds.value.filter((id): id is string => id !== null)
  await updateLoadout(ids)
  syncPendingFromProfile()
}
</script>

<style scoped>
.buff-picker {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.buff-picker__section-label {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: #d7bb84;
  margin-top: 6px;
}

.buff-picker__slots {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.buff-slot {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  width: 72px;
  min-height: 80px;
  padding: 8px 4px 6px;
  border-radius: 8px;
  border: 1px solid rgba(200, 164, 106, 0.24);
  background: linear-gradient(180deg, rgba(30, 20, 10, 0.85), rgba(15, 10, 5, 0.92));
  color: #f5ead2;
  cursor: pointer;
  transition: filter 0.1s, border-color 0.1s;
}

.buff-slot--filled {
  border-color: rgba(200, 164, 106, 0.5);
}

.buff-slot--selected {
  border-color: rgba(247, 216, 142, 0.75);
  filter: brightness(1.15);
}

.buff-slot:hover:not(:disabled) {
  filter: brightness(1.1);
  border-color: rgba(220, 180, 100, 0.55);
}

.buff-slot:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.buff-slot:focus-visible {
  outline: 2px solid rgba(247, 216, 142, 0.9);
  outline-offset: 2px;
}

.buff-slot__name {
  font-size: 10px;
  font-weight: 600;
  text-align: center;
  line-height: 1.2;
  color: #d7bb84;
  word-break: break-word;
}

.buff-slot__name--empty {
  color: rgba(200, 164, 106, 0.4);
}

.buff-slot__empty {
  font-size: 22px;
  color: rgba(200, 164, 106, 0.3);
  line-height: 1;
  margin-bottom: 2px;
}

.buff-picker__list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: 260px;
  overflow-y: auto;
  scrollbar-width: thin;
  scrollbar-color: rgba(200, 164, 106, 0.3) transparent;
}

.buff-picker__list--locked {
  opacity: 0.55;
}

.buff-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border-radius: 8px;
  border: 1px solid rgba(200, 164, 106, 0.18);
  background: linear-gradient(180deg, rgba(25, 18, 8, 0.85), rgba(12, 9, 4, 0.9));
  text-align: left;
  color: #f5ead2;
}

.buff-item--unlocked {
  cursor: pointer;
  transition: filter 0.1s, border-color 0.1s;
}

.buff-item--unlocked:hover:not(:disabled) {
  filter: brightness(1.12);
  border-color: rgba(220, 180, 100, 0.45);
}

.buff-item--unlocked:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.buff-item--unlocked:focus-visible {
  outline: 2px solid rgba(247, 216, 142, 0.9);
  outline-offset: 2px;
}

.buff-item--equipped {
  border-color: rgba(247, 216, 142, 0.5);
  background: linear-gradient(180deg, rgba(45, 32, 12, 0.9), rgba(25, 17, 6, 0.95));
}

.buff-item--locked {
  cursor: default;
}

.buff-item__copy {
  flex: 1 1 0;
  min-width: 0;
}

.buff-item__name {
  font-size: 13px;
  font-weight: 700;
  color: #f5ead2;
}

.buff-item__desc {
  margin-top: 2px;
  font-size: 11px;
  color: #a09070;
  line-height: 1.4;
}

.buff-item__badge {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: #f7d88e;
  white-space: nowrap;
}

.buff-item__cost {
  font-size: 11px;
  font-weight: 700;
  color: #a09070;
  white-space: nowrap;
}

.buff-picker__error {
  font-size: 12px;
  color: #f07070;
  padding: 6px 0;
}

.buff-picker__footer {
  display: flex;
  justify-content: flex-end;
  padding-top: 4px;
}

.buff-picker__save {
  padding: 10px 24px;
  border-radius: 10px;
  border: 1px solid rgba(200, 164, 106, 0.35);
  background: linear-gradient(180deg, rgba(113, 75, 39, 0.9), rgba(61, 39, 22, 0.95));
  color: #f5ead2;
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 0.05em;
  cursor: pointer;
  transition: background 0.1s, border-color 0.1s, opacity 0.1s;
}

.buff-picker__save:hover:not(:disabled) {
  background: linear-gradient(180deg, rgba(145, 96, 48, 1), rgba(83, 53, 28, 1));
  border-color: rgba(220, 180, 110, 0.55);
}

.buff-picker__save:disabled {
  opacity: 0.38;
  cursor: not-allowed;
}

.buff-picker__save:focus-visible {
  outline: 2px solid rgba(247, 216, 142, 0.9);
  outline-offset: 2px;
}
</style>
