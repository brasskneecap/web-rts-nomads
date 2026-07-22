<template>
  <!-- Mounted once, in App.vue. Renders nothing until something calls ask(). -->
  <Teleport to="body">
    <div
      v-if="open && request"
      class="confirm-backdrop"
      data-test="confirm-backdrop"
      @click.self="onCancel"
    >
      <div
        class="confirm-panel"
        :style="panelStyle"
        role="alertdialog"
        aria-modal="true"
        :aria-label="request.title"
        data-test="confirm-dialog"
      >
        <h2 class="confirm-title" data-test="confirm-title">{{ request.title }}</h2>
        <p v-for="(line, i) in request.lines" :key="i" class="confirm-line" data-test="confirm-line">
          {{ line }}
        </p>
        <div class="confirm-actions">
          <!-- Cancel is FIRST in DOM order so it takes the initial focus below:
               the safe choice should be what Enter hits on a dialog whose other
               button destroys a catalog file. -->
          <UiButton ref="cancelBtn" size="sm" variant="secondary" data-test="confirm-cancel" @click="onCancel">
            {{ request.cancelLabel }}
          </UiButton>
          <!-- The accept button carries the emphasis plate; the wording carries
               the danger. Both plates are set in CSS below, because this panel
               repaints every .ui-button inside it and would otherwise flatten
               UiButton's own variant art to one look. -->
          <UiButton
            size="sm"
            :class="['confirm-accept', { 'is-danger': request.danger }]"
            data-test="confirm-accept"
            @click="onAccept"
          >
            {{ request.confirmLabel }}
          </UiButton>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
// ConfirmDialogHost renders whatever useConfirmDialog's singleton is asking.
// See that module's doc comment for why this exists instead of window.confirm.
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import UiButton from './UiButton.vue'
import { settle, useConfirmDialogState } from './useConfirmDialog'
// Painted with the UPDATED theme art directly rather than through UiPanel:
// UiPanel's `default` variant is still the OLD ui_panel.png, so a plain
// <UiPanel> would have shipped this brand-new dialog on the dated look. Frame
// treatment (slice/width/repeat) copied from MatchSettingsModal so every modal
// in the game reads as the same object.
import mainWindowPanelUrl from '@/assets/ui/themes/updated/main-window-panel.png'
import buttonUrl from '@/assets/ui/themes/updated/button.png'
import activeButtonUrl from '@/assets/ui/themes/updated/active-button.png'

const { request, open } = useConfirmDialogState()

const panelStyle = computed(() => ({
  '--ui-window-image': `url(${mainWindowPanelUrl})`,
  '--ui-button-image': `url(${buttonUrl})`,
  '--ui-button-accept-image': `url(${activeButtonUrl})`,
}))
const cancelBtn = ref<InstanceType<typeof UiButton> | null>(null)

function onCancel() {
  settle(false)
}
function onAccept() {
  settle(true)
}

// Esc cancels. Registered on window rather than the panel because the dialog
// may open without the user having focused anything inside it yet.
function onKeydown(e: KeyboardEvent) {
  if (!open.value) return
  if (e.key === 'Escape') {
    e.preventDefault()
    settle(false)
  }
}

onMounted(() => window.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))

// Focus Cancel when the dialog opens: keyboard-reachable, and the SAFE action is
// the one under the finger. Deliberately not the confirm button — Enter on a
// freshly-opened destructive prompt should not delete anything.
watch(open, async (isOpen) => {
  if (!isOpen) return
  await nextTick()
  const el = (cancelBtn.value?.$el ?? null) as HTMLElement | null
  el?.focus?.()
})
</script>

<style scoped>
.confirm-backdrop {
  position: fixed;
  inset: 0;
  /* Above every editor panel and the menu chrome. */
  z-index: 4000;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.55);
}

.confirm-panel {
  position: relative;
  box-sizing: border-box;
  width: min(30rem, calc(100vw - 3rem));
  padding: 0.25rem 0.5rem 0.5rem;
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  /* Main-window-panel frame with `fill`, so the wood interior backs the text.
     Slice keeps the full 44px corner art, rendered at 40px for a modal this
     size — the same treatment MatchSettingsModal uses. */
  border: 40px solid transparent;
  border-image-source: var(--ui-window-image);
  border-image-slice: 44 fill;
  border-image-width: 40px;
  border-image-repeat: round;
  image-rendering: auto;
  color: #f6ecd2;
  box-shadow: 0 18px 48px rgba(0, 0, 0, 0.65);
}

/* The dialog's own buttons use the updated plates. Scoped to this panel so it
   cannot leak into whatever is behind the backdrop. */
.confirm-panel :deep(.ui-button) {
  border: 0;
  padding: 0 18px;
  min-width: 132px;
  min-height: 52px;
  background: var(--ui-button-image) center / 100% 100% no-repeat;
  image-rendering: auto;
  color: #f6ecd2;
}

/* The action the dialog is ABOUT gets the emphasis plate, so Cancel and Delete
   are never one undifferentiated pair of buttons. */
.confirm-panel :deep(.ui-button.confirm-accept) {
  background-image: var(--ui-button-accept-image);
}

/* `danger` is the difference between "Delete" and "Move"/"Reset", and it has to
   be VISIBLE or the flag is decoration: a destructive accept reads warm-red
   rather than parchment. Deliberately a text treatment, not a second plate —
   the frame art stays consistent and the eye still lands on the wording, which
   is what actually tells the user what is about to happen. */
.confirm-panel :deep(.ui-button.confirm-accept.is-danger) {
  color: #ffcbb0;
  text-shadow: 0 0 6px rgba(190, 60, 30, 0.55);
}

.confirm-title {
  margin: 0;
  font-family: var(--font-display, inherit);
  font-size: 1.15rem;
  color: #f2d090;
}

.confirm-line {
  margin: 0;
  line-height: 1.45;
  color: rgba(236, 224, 196, 0.86);
}

.confirm-actions {
  display: flex;
  justify-content: flex-end;
  gap: 0.6rem;
  margin-top: 0.4rem;
}
</style>
