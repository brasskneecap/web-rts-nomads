<template>
  <ul class="ed-checks">
    <li v-if="checks.length === 0" class="ed-checks__row ed-checks__row--ok">
      <span class="ed-checks__mark">✓</span> Ready to save.
    </li>
    <li
      v-for="(c, i) in checks"
      :key="i"
      class="ed-checks__row"
      :class="c.ok ? 'ed-checks__row--ok' : 'ed-checks__row--bad'"
    >
      <span class="ed-checks__mark">{{ c.ok ? '✓' : '✕' }}</span>
      {{ c.message }}
    </li>
  </ul>
</template>

<script setup lang="ts">
/** One rule's outcome. `message` reads as a statement either way ("ID is
 *  valid." / "ID must be lowercase…"), so the list scans top to bottom. */
export type ValidationCheck = { ok: boolean; message: string }

defineProps<{ checks: ValidationCheck[] }>()
</script>

<style scoped>
.ed-checks {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.ed-checks__row {
  display: flex;
  align-items: baseline;
  gap: 8px;
  font-size: 0.78rem;
  line-height: 1.3;
}

.ed-checks__row--ok {
  color: var(--ed-text-dim);
}

.ed-checks__row--bad {
  color: var(--ed-danger);
}

.ed-checks__mark {
  flex: 0 0 auto;
  font-weight: 700;
}

.ed-checks__row--ok .ed-checks__mark {
  color: var(--ed-ok);
}
</style>
