<template>
  <div class="ed-list">
    <p v-if="rows === 0 && emptyText" class="ed-list__empty">{{ emptyText }}</p>

    <!-- Rows are rendered by the caller (it owns the model); this component
         owns the framing, the remove affordance and the add button, which is
         what every repeatable block in every editor has in common. -->
    <slot />

    <button type="button" class="ed-list__add" @click="emit('add')">
      + {{ addLabel }}
    </button>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  /** Current row count — drives the empty state only. */
  rows: number
  /** Label for the add button, e.g. "Add Proc". */
  addLabel: string
  /** Shown when there are no rows. Omit for no empty state. */
  emptyText?: string
}>()

const emit = defineEmits<{ add: [] }>()
</script>

<style scoped>
.ed-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
}

.ed-list__empty {
  margin: 0;
  font-size: 0.78rem;
  color: var(--ed-text-dim);
}

.ed-list__add {
  align-self: flex-start;
  padding: 5px 10px;
  font-family: var(--font-body);
  font-size: 0.76rem;
  font-weight: 600;
  color: var(--ed-brass);
  background: rgba(212, 168, 71, 0.08);
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
}

.ed-list__add:hover {
  background: rgba(212, 168, 71, 0.16);
  border-color: var(--ed-line-strong);
}
</style>
