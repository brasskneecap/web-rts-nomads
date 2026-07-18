<template>
  <!-- Root is an inline span so it drops beside a label without affecting
       layout — the bubble itself is teleported out (see below), so nothing
       here ever grows/shrinks on open. `@click.stop` on the button matters
       when an InfoTip sits inside a self-labeling `<label class="ed-check">`
       (e.g. TargetQueryEditor's includeInitialTarget checkbox) — without it,
       a click that also toggled the checkbox would be indistinguishable
       from one that only meant to open the tip. -->
  <span v-if="hasText" ref="rootEl" class="info-tip">
    <button
      type="button"
      class="info-tip__btn"
      :aria-expanded="open"
      :aria-label="ariaLabel"
      :aria-describedby="open ? tooltipId : undefined"
      @click.stop="onClick"
      @mouseenter="hovering = true"
      @mouseleave="hovering = false"
      @keydown.escape="close"
    >
      <span aria-hidden="true">i</span>
    </button>
    <!-- Teleported to <body> and `position: fixed` from a captured rect, the
         same trick FilterableSelect's menu uses — an inspector column is
         `overflow-y: auto`, and an absolutely-positioned child would be
         clipped the moment the trigger nears the panel edge. Fixed-from-body
         sidesteps that entirely. -->
    <Teleport to="body">
      <div
        v-if="open"
        ref="bubbleEl"
        :id="tooltipId"
        role="tooltip"
        class="info-tip__bubble"
        :style="bubbleStyle"
      >{{ text }}</div>
    </Teleport>
  </span>
</template>


<script setup lang="ts">
// A small (i) icon that reveals an explanatory tooltip. Click TOGGLES it open
// (the user asked for click, not hover) — hover ALSO reveals it as a
// convenience for a quick glance, without disturbing the click-pinned state:
// pinning via click keeps the bubble open after the mouse leaves, and only a
// second click, Escape, or a click outside closes it. Hovering a second,
// already-pinned tip has no effect (it's already open).
//
// Only one tip is pinned open at a time — see useInfoTip.ts, whose
// `pinnedId` is shared (module-scoped) across every InfoTip instance, so
// pinning a new one silently un-pins whichever was open before.
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useInfoTipId, useInfoTipPinning } from '@/composables/useInfoTip'

const props = withDefaults(
  defineProps<{
    /** The tooltip body. Empty/whitespace-only renders NOTHING (not even the
     *  icon) — this is the graceful-fallback contract a caller relies on: a
     *  field or enum value with no copy (e.g. targetQueryHints.ts hasn't
     *  been updated for a brand-new one yet) degrades to no icon rather than
     *  an icon that opens onto blank space. Callers may still additionally
     *  `v-if` around this component (TargetQueryEditor does, for its
     *  `#label-extra` slot) — that's belt-and-suspenders, not required. */
    text: string
    /** Accessible name for the trigger button. */
    ariaLabel?: string
  }>(),
  { ariaLabel: 'More info' },
)

const hasText = computed(() => !!props.text.trim())
const tooltipId = useInfoTipId()
const { isPinned, toggle, unpin } = useInfoTipPinning(tooltipId)
const hovering = ref(false)
const pinned = computed(isPinned)
const open = computed(() => hasText.value && (pinned.value || hovering.value))

const rootEl = ref<HTMLElement | null>(null)
// The bubble is Teleported to <body>, so it is NOT a DOM descendant of
// rootEl — onDocumentClick below must check both, or clicking inside the
// bubble itself (e.g. to select its text) would register as "outside" and
// close it immediately.
const bubbleEl = ref<HTMLElement | null>(null)
const bubbleStyle = ref<Record<string, string>>({})

function positionBubble() {
  const el = rootEl.value?.querySelector('.info-tip__btn')
  if (!el) return
  const r = el.getBoundingClientRect()
  // Anchor under-left of the icon; the bubble's own CSS clamps max-width so it
  // won't run off the right edge of the viewport for icons near the panel edge.
  bubbleStyle.value = { left: `${r.left}px`, top: `${r.bottom + 6}px` }
}

function onViewportChange() {
  if (open.value) positionBubble()
}

function onDocumentClick(e: MouseEvent) {
  const target = e.target as Node
  if (rootEl.value?.contains(target) || bubbleEl.value?.contains(target)) return
  close()
}

function attachOpenListeners() {
  window.addEventListener('scroll', onViewportChange, true)
  window.addEventListener('resize', onViewportChange)
  document.addEventListener('mousedown', onDocumentClick, true)
}

function detachOpenListeners() {
  window.removeEventListener('scroll', onViewportChange, true)
  window.removeEventListener('resize', onViewportChange)
  document.removeEventListener('mousedown', onDocumentClick, true)
}

watch(open, (isOpen) => {
  if (isOpen) {
    positionBubble()
    attachOpenListeners()
  } else {
    detachOpenListeners()
  }
})

function onClick() {
  toggle()
}

function close() {
  unpin()
  hovering.value = false
}

onBeforeUnmount(() => {
  unpin()
  detachOpenListeners()
})
</script>

<style scoped>
.info-tip {
  display: inline-flex;
  /* Deliberately no `cursor:` — the global rules in style.css already paint
     the game cursor on interactive elements (see CLAUDE.md). */
}

.info-tip__btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 15px;
  height: 15px;
  padding: 0;
  border-radius: 50%;
  border: 1px solid var(--ed-line-strong);
  background: rgba(212, 168, 71, 0.1);
  color: var(--ed-brass-dim);
  font-family: var(--font-body);
  font-size: 0.65rem;
  font-style: italic;
  font-weight: 700;
  line-height: 1;
  /* This sits inside EditorField's UPPERCASE label in most placements — a
     bare lowercase "i" reads as the info glyph; without this it would render
     as a capital "I", inherited from the label. */
  text-transform: none;
}

.info-tip__btn:hover,
.info-tip__btn:focus-visible {
  border-color: var(--ed-brass);
  color: var(--ed-brass);
}

.info-tip__btn:focus-visible {
  outline: 2px solid var(--ed-brass);
  outline-offset: 1px;
}
</style>

<style>
/* Teleported to <body>, so (like FilterableSelect's menu) it is OUTSIDE
   .ed-shell — every value carries a literal fallback rather than relying on
   the editor's CSS vars being in scope out there. */
.info-tip__bubble {
  position: fixed;
  z-index: var(--z-tooltip, 10000);
  max-width: 280px;
  padding: 8px 10px;
  font-family: var(--font-body, sans-serif);
  font-size: 0.76rem;
  line-height: 1.4;
  color: var(--ed-text, #e9dbb8);
  background: rgba(10, 12, 20, 0.98);
  border: 1px solid var(--ed-line, rgba(212, 168, 79, 0.4));
  border-radius: 6px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.6);
  white-space: normal;
}
</style>
