<template>
  <header class="ed-head">
    <div class="ed-head__left">
      <div class="ed-head__titlerow">
        <h1 class="ed-head__title">{{ title || 'Untitled' }}</h1>
        <span v-if="badge" class="ed-head__badge" :style="{ color: badgeColor, borderColor: badgeColor }">
          {{ badge }}
        </span>
      </div>
      <div v-if="breadcrumb" class="ed-head__crumb">{{ breadcrumb }}</div>
      <div v-if="filePath" class="ed-head__path" :title="filePath">{{ filePath }}</div>
    </div>

    <div class="ed-head__right">
      <!-- The id is the one identity field that can never change after the
           first save, so it lives in the header rather than the form. -->
      <div class="ed-head__id">
        <label class="ed-head__id-label" :for="idInputId">
          ID <span v-if="!idEditable" class="ed-head__lock" aria-label="locked">🔒</span>
        </label>
        <input
          :id="idInputId"
          :value="id"
          type="text"
          :disabled="!idEditable"
          :placeholder="idEditable ? 'lowercase_with_underscores' : ''"
          @input="emit('update:id', ($event.target as HTMLInputElement).value.trim())"
        />
      </div>

      <div class="ed-head__actions">
        <UiButton size="sm" variant="active" :disabled="saveDisabled" @click="emit('save')">
          {{ saving ? 'Saving…' : 'Save' }}
        </UiButton>
        <!-- The lesser action sits beside Save. Its LABEL is the caller's, so
             an editor can say "Reset" for a shipped def and "Delete" for one
             the author created — the two are not the same act. -->
        <UiButton v-if="removeLabel" size="sm" variant="secondary" @click="emit('remove')">
          {{ removeLabel }}
        </UiButton>
        <span v-if="error" class="ed-head__status ed-head__status--bad" role="alert">{{ error }}</span>
        <span v-else-if="savedLabel" class="ed-head__status ed-head__status--ok">Saved {{ savedLabel }}</span>
      </div>
    </div>
  </header>
</template>

<script setup lang="ts">
import UiButton from '@/components/ui/UiButton.vue'

defineProps<{
  title: string
  /** Tier / category chip shown beside the title. */
  badge?: string
  badgeColor?: string
  /** "Equipment • Weapon • Tier: Common" */
  breadcrumb?: string
  /** Repo-relative path of the file this def writes to. */
  filePath?: string
  id: string
  /** False once the def has been saved — the id is immutable after that. */
  idEditable: boolean
  idInputId?: string
  saving?: boolean
  saveDisabled?: boolean
  /** Relative time since the last successful save ("just now", "2 min ago"). */
  savedLabel?: string
  error?: string
  /** Label for the destructive action beside Save ("Reset" / "Delete").
   *  Empty or absent hides the button (e.g. an unsaved draft has nothing to
   *  reset). */
  removeLabel?: string
}>()

const emit = defineEmits<{ save: []; remove: []; 'update:id': [string] }>()
</script>

<style scoped>
.ed-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex: 0 0 auto;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--ed-line);
}

.ed-head__left {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.ed-head__titlerow {
  display: flex;
  align-items: center;
  gap: 10px;
}

.ed-head__title {
  margin: 0;
  font-family: var(--font-title);
  font-size: 1.5rem;
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--ed-brass);
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.85);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.ed-head__badge {
  flex: 0 0 auto;
  padding: 2px 8px;
  font-size: 0.66rem;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  border: 1px solid currentColor;
  border-radius: 999px;
}

.ed-head__crumb {
  font-size: 0.76rem;
  color: var(--ed-text-dim);
}

.ed-head__path {
  font-family: var(--mono);
  font-size: 0.66rem;
  color: var(--ed-text-dim);
  opacity: 0.7;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 46ch;
}

.ed-head__right {
  flex: 0 0 auto;
  display: flex;
  align-items: flex-end;
  gap: 12px;
}

.ed-head__id {
  display: flex;
  flex-direction: column;
  gap: 3px;
  width: 190px;
}

.ed-head__id-label {
  font-size: 0.68rem;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--ed-text-dim);
}

.ed-head__lock {
  font-size: 0.62rem;
  opacity: 0.7;
}

.ed-head__actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.ed-head__status {
  font-size: 0.74rem;
  white-space: nowrap;
}

.ed-head__status--ok {
  color: var(--ed-ok);
}

.ed-head__status--bad {
  color: var(--ed-danger);
  max-width: 34ch;
  white-space: normal;
}
</style>
