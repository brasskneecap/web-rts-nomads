<template>
  <div class="volume-slider">
    <label :for="id" class="volume-slider__label">{{ label }}</label>
    <input
      :id="id"
      class="volume-slider__input"
      type="range"
      min="0"
      max="100"
      step="1"
      :value="Math.round(modelValue * 100)"
      @input="onInput"
    />
    <span class="volume-slider__value" aria-hidden="true">{{ Math.round(modelValue * 100) }}</span>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  id: string
  label: string
  modelValue: number
}>()

const emit = defineEmits<{
  'update:modelValue': [value: number]
}>()

function onInput(e: Event) {
  const target = e.target as HTMLInputElement
  const n = parseInt(target.value, 10)
  if (Number.isNaN(n)) return
  emit('update:modelValue', Math.max(0, Math.min(1, n / 100)))
}
</script>

<style scoped>
.volume-slider {
  display: grid;
  grid-template-columns: 140px 1fr 40px;
  align-items: center;
  gap: 12px;
}

.volume-slider__label {
  font-size: 14px;
  font-weight: 600;
  color: #f5ead2;
  letter-spacing: 0.04em;
}

.volume-slider__input {
  width: 100%;
  appearance: none;
  height: 6px;
  background: linear-gradient(180deg, rgba(36, 22, 12, 0.95), rgba(20, 12, 4, 0.95));
  border: 1px solid rgba(70, 47, 24, 0.7);
  border-radius: 999px;
  outline: none;
  cursor: pointer;
}

.volume-slider__input::-webkit-slider-thumb {
  appearance: none;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: linear-gradient(180deg, #e8b95c, #bb7f30);
  border: 1px solid rgba(46, 20, 10, 0.85);
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.6);
  cursor: pointer;
}

.volume-slider__input::-moz-range-thumb {
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: linear-gradient(180deg, #e8b95c, #bb7f30);
  border: 1px solid rgba(46, 20, 10, 0.85);
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.6);
  cursor: pointer;
}

.volume-slider__input:focus-visible {
  outline: 2px solid rgba(247, 216, 142, 0.9);
  outline-offset: 3px;
}

.volume-slider__value {
  text-align: right;
  font-size: 13px;
  font-weight: 700;
  color: #ffe9a0;
  font-variant-numeric: tabular-nums;
}
</style>
