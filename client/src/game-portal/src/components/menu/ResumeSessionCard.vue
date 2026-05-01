<template>
  <div class="resume-card">
    <UiPanel :padding="20">
      <div class="resume-card__content">
        <div class="resume-card__title">Resume previous game?</div>
        <div class="resume-card__subtitle">{{ mapName }}</div>
        <div class="resume-card__actions">
          <UiButton size="md" @click="emit('resume')">Resume</UiButton>
          <UiButton size="md" @click="emit('dismiss')">Dismiss</UiButton>
        </div>
      </div>
    </UiPanel>
  </div>
</template>

<script setup lang="ts">
import UiPanel from '@/components/ui/UiPanel.vue'
import UiButton from '@/components/ui/UiButton.vue'

defineProps<{
  mapName: string
}>()

const emit = defineEmits<{
  resume: []
  dismiss: []
}>()
</script>

<style scoped>
.resume-card {
  position: absolute;
  top: 32px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 10;
  width: 360px;
}

.resume-card__content {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.resume-card__title {
  font-size: 16px;
  font-weight: 700;
  color: #f7d88e;
  letter-spacing: 0.05em;
}

.resume-card__subtitle {
  font-size: 13px;
  color: #cbb893;
}

.resume-card__actions {
  display: flex;
  gap: 10px;
  margin-top: 8px;
}
</style>
