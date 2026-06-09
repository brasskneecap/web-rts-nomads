<template>
  <div class="campaign-victory-overlay" role="dialog" aria-modal="true" aria-labelledby="campaign-victory-title">
    <UiPanel variant="parchment" :padding="0" class="campaign-victory__card">
      <div class="campaign-victory__inner">
        <div id="campaign-victory-title" class="campaign-victory__title">
          ★ Victory ★
        </div>
        <div class="campaign-victory__subtitle">
          All required objectives complete.
        </div>
        <div class="campaign-victory__actions">
          <button
            type="button"
            class="campaign-victory__btn campaign-victory__btn--secondary"
            @click="$emit('continue')"
          >Continue Playing</button>
          <button
            type="button"
            class="campaign-victory__btn campaign-victory__btn--primary"
            @click="$emit('exit')"
          >Exit to Victory Screen</button>
        </div>
      </div>
    </UiPanel>
  </div>
</template>

<script setup lang="ts">
import UiPanel from '@/components/ui/UiPanel.vue'

defineEmits<{
  (e: 'continue'): void
  (e: 'exit'): void
}>()
</script>

<style scoped>
.campaign-victory-overlay {
  position: fixed;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.55);
  z-index: 1000;
  color: #3a1f0a;
  font-family: 'Trebuchet MS', 'Lucida Sans Unicode', system-ui, sans-serif;
}

.campaign-victory__card {
  width: min(560px, 90vw);
  box-sizing: border-box;
}

.campaign-victory__inner {
  padding: 48px 56px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 18px;
}

.campaign-victory__title {
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: 42px;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-align: center;
  text-transform: uppercase;
  color: #8a5a2a;
  text-shadow: 0 0 18px rgba(212, 168, 71, 0.45);
}

.campaign-victory__subtitle {
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: 16px;
  letter-spacing: 0.08em;
  text-align: center;
  color: rgba(58, 31, 10, 0.75);
}

.campaign-victory__actions {
  display: flex;
  gap: 18px;
  margin-top: 18px;
  flex-wrap: wrap;
  justify-content: center;
}

.campaign-victory__btn {
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: 15px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  border-radius: 4px;
  padding: 13px 28px;
  min-width: 220px;
}

.campaign-victory__btn--primary {
  background: linear-gradient(180deg, #d8b06a 0%, #a87a36 100%);
  color: #2a1505;
  border: 1px solid rgba(58, 31, 10, 0.55);
}

.campaign-victory__btn--secondary {
  background: linear-gradient(180deg, #efe2c4 0%, #cdb88a 100%);
  color: #3a1f0a;
  border: 1px solid rgba(58, 31, 10, 0.45);
}
</style>
