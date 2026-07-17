<template>
  <div v-if="open" class="conv-overlay" data-test="convert-dialog-overlay" @click.self="onClose">
    <div class="conv-panel" role="dialog" aria-modal="true" aria-label="Convert to Composable">
      <header class="conv-panel__head">
        <span>Convert to Composable</span>
        <UiButton
          size="sm"
          variant="secondary"
          data-test="convert-close"
          :disabled="builder.busy.value"
          @click="onClose"
        >Close</UiButton>
      </header>

      <div class="conv-panel__body">
        <template v-if="phase === 'confirm'">
          <p>
            This asks the server to compile this legacy ability's mechanic fields into an editable composable
            flow (triggers and actions). Some legacy mechanics don't have a direct composable equivalent yet —
            anything the server had to drop or approximate will be listed as a warning after conversion.
          </p>
          <p class="conv-panel__note">
            This creates an unsaved composable version. Review the flow and Save to keep it; pick another
            ability to discard.
          </p>
          <p v-if="attempted && builder.saveError.value" class="conv-panel__error" role="alert">
            {{ builder.saveError.value }}
          </p>

          <div class="conv-panel__actions">
            <UiButton size="sm" variant="secondary" @click="onClose">Cancel</UiButton>
            <UiButton
              size="sm"
              variant="active"
              :disabled="builder.busy.value"
              data-test="convert-confirm"
              @click="onConfirm"
            >
              {{ builder.busy.value ? 'Converting…' : 'Convert' }}
            </UiButton>
          </div>
        </template>

        <template v-else>
          <p
            v-if="!builder.runnable.value"
            class="conv-panel__degraded"
            data-test="convert-degraded"
            role="alert"
          >
            This ability uses mechanics the composable runtime doesn't execute yet — converting it makes it
            display-only (inert in-game) until a later phase.
          </p>
          <p v-else class="conv-panel__ok">Converted successfully — the new flow is runnable.</p>

          <div v-if="builder.warnings.value.length" class="conv-panel__warnings" data-test="convert-warnings">
            <span class="conv-panel__warnings-label">Degradation notices</span>
            <ul>
              <li v-for="(w, i) in builder.warnings.value" :key="i">{{ w }}</li>
            </ul>
          </div>
          <p v-else class="conv-panel__hint">No degradation notices — the conversion was lossless.</p>

          <p class="conv-panel__note">
            This creates an unsaved composable version. Review the flow and Save to keep it; pick another
            ability to discard.
          </p>

          <div class="conv-panel__actions">
            <UiButton size="sm" variant="active" data-test="convert-done" @click="onClose">Done</UiButton>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
// ConvertDialog: shown when the user clicks "Convert to Composable" on a
// legacy ability. Confirm -> builder.convert() -> the server's degradation
// warnings + runnable status are shown before the user can close it, so
// they're never swallowed. Mirrors AbilityEditorPanel's inline icon-gallery
// overlay pattern (no shared Modal component exists yet in ui/).
import { ref, watch } from 'vue'
import UiButton from '@/components/ui/UiButton.vue'
import { useAbilityBuilderContext } from './AbilityBuilderContext'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()

const builder = useAbilityBuilderContext()

type Phase = 'confirm' | 'result'
const phase = ref<Phase>('confirm')

// attempted tracks whether THIS dialog session has actually called
// builder.convert() at least once. builder.saveError is a shared, global
// error slot — a prior save/convert attempt (e.g. from a earlier dialog
// session, or a failed Save on the main panel) can leave a stale message
// sitting in it. Gating the confirm-phase error display behind `attempted`
// stops that leftover error from flashing on a fresh open, while still
// showing it the moment this session's own convert() attempt fails.
const attempted = ref(false)

// Reset back to the confirm step every time the dialog is (re)opened, so a
// prior conversion's result doesn't linger if the author reopens it for a
// different (or the same, re-converted) ability.
watch(
  () => props.open,
  (isOpen) => {
    if (isOpen) {
      phase.value = 'confirm'
      attempted.value = false
    }
  },
)

async function onConfirm() {
  attempted.value = true
  await builder.convert()
  // convert() only flips isLegacy to false on success; on failure it leaves
  // isLegacy true and sets builder.saveError, which the confirm-phase
  // template surfaces — so staying on 'confirm' here is what lets the
  // author see the error and retry instead of being dropped onto a blank
  // "result" screen.
  if (builder.isLegacy.value) return
  phase.value = 'result'
}

function onClose() {
  // Converting is in flight — closing now would abandon the dialog mid
  // request with no way to see the result or a failure. The Close button is
  // also :disabled while busy (belt-and-suspenders for keyboard/synthetic
  // clicks that bypass the disabled attribute), and this guard covers the
  // backdrop @click.self path, which has no disabled state of its own.
  if (builder.busy.value) return
  emit('close')
}
</script>

<style scoped>
.conv-overlay {
  position: fixed;
  inset: 0;
  background: rgba(3, 8, 14, 0.72);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 60;
}

.conv-panel {
  width: min(480px, 90vw);
  max-height: 80vh;
  overflow-y: auto;
  background: rgba(8, 14, 24, 0.96);
  border: 1px solid var(--ed-line-strong);
  border-radius: 12px;
  padding: 14px 16px 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.conv-panel__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--ed-line);
  font-family: var(--font-title);
  font-size: 0.9rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--ed-brass);
}

.conv-panel__body {
  display: flex;
  flex-direction: column;
  gap: 10px;
  font-size: 0.84rem;
  color: var(--ed-text);
  line-height: 1.5;
}

.conv-panel__body p {
  margin: 0;
}

.conv-panel__note {
  color: var(--ed-text-dim);
  font-style: italic;
}

.conv-panel__error {
  color: var(--ed-danger);
}

.conv-panel__degraded {
  padding: 8px 10px;
  color: #e0b258;
  background: rgba(224, 178, 88, 0.1);
  border: 1px solid rgba(224, 178, 88, 0.3);
  border-radius: var(--ed-radius);
}

.conv-panel__ok {
  color: var(--ed-ok);
}

.conv-panel__warnings {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 8px 10px;
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
  background: rgba(15, 23, 42, 0.3);
}

.conv-panel__warnings-label {
  font-family: var(--font-title);
  font-size: 0.68rem;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--ed-brass-dim);
}

.conv-panel__warnings ul {
  margin: 0;
  padding-left: 18px;
  color: #e0b258;
}

.conv-panel__hint {
  color: var(--ed-text-dim);
}

.conv-panel__actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding-top: 4px;
}
</style>
