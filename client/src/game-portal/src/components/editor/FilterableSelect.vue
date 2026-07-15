<template>
  <!-- A type-to-filter select. The input shows the chosen option's LABEL but the
       model is its ID, so callers store an id while the author reads a name.
       The menu teleports to <body> so an editor scroll container can't clip it;
       it is positioned under the input from a captured rect. -->
  <div class="fsel">
    <input
      ref="inputEl"
      type="text"
      class="fsel__input"
      role="combobox"
      aria-autocomplete="list"
      autocomplete="off"
      :aria-expanded="open"
      :aria-label="ariaLabel"
      :placeholder="placeholder"
      :value="displayValue"
      @focus="onFocus"
      @input="onInput"
      @keydown="onKeydown"
      @blur="onBlur"
    />
    <Teleport to="body">
      <ul
        v-if="open && filtered.length"
        ref="menuEl"
        class="fsel__menu"
        :style="menuStyle"
        role="listbox"
        @mousedown.prevent
      >
        <li
          v-for="(opt, i) in filtered"
          :key="opt.id"
          class="fsel__opt"
          :class="{ 'is-active': i === highlight }"
          role="option"
          :aria-selected="opt.id === modelValue"
          @mouseenter="highlight = i"
          @click="choose(opt)"
        >{{ opt.label }}</li>
      </ul>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'

export type FilterableOption = { id: string; label: string }

const props = defineProps<{
  /** The selected option id ('' when nothing is chosen). */
  modelValue: string
  options: FilterableOption[]
  placeholder?: string
  ariaLabel?: string
}>()

const emit = defineEmits<{ 'update:modelValue': [string] }>()

const inputEl = ref<HTMLInputElement | null>(null)
const menuEl = ref<HTMLUListElement | null>(null)
const open = ref(false)
// `typing` distinguishes "author is filtering" (input shows their query and the
// list narrows) from "resting" (input shows the selected label, list shows all).
const typing = ref(false)
const query = ref('')
const highlight = ref(0)
const menuStyle = ref<Record<string, string>>({})

const selectedLabel = computed(
  () => props.options.find((o) => o.id === props.modelValue)?.label ?? '',
)

const displayValue = computed(() => (typing.value ? query.value : selectedLabel.value))

const filtered = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!typing.value || !q) return props.options
  return props.options.filter(
    (o) => o.label.toLowerCase().includes(q) || o.id.toLowerCase().includes(q),
  )
})

function positionMenu() {
  const el = inputEl.value
  if (!el) return
  const r = el.getBoundingClientRect()
  menuStyle.value = { left: `${r.left}px`, top: `${r.bottom + 2}px`, width: `${r.width}px` }
}

// The menu is fixed-positioned from a captured rect, so it must follow the input
// when anything scrolls or the window resizes. Capture-phase catches scrolling
// inside editor containers, not just the window.
function onViewportChange() {
  if (open.value) positionMenu()
}

function openMenu() {
  open.value = true
  const sel = filtered.value.findIndex((o) => o.id === props.modelValue)
  highlight.value = sel >= 0 ? sel : 0
  positionMenu()
  window.addEventListener('scroll', onViewportChange, true)
  window.addEventListener('resize', onViewportChange)
}

function closeMenu() {
  open.value = false
  typing.value = false
  query.value = ''
  window.removeEventListener('scroll', onViewportChange, true)
  window.removeEventListener('resize', onViewportChange)
}

function onFocus() {
  typing.value = false
  openMenu()
  // Select the shown label so the first keystroke replaces it.
  nextTick(() => inputEl.value?.select())
}

function onInput(e: Event) {
  typing.value = true
  query.value = (e.target as HTMLInputElement).value
  highlight.value = 0
  if (!open.value) openMenu()
  else nextTick(positionMenu)
}

function choose(opt: FilterableOption) {
  emit('update:modelValue', opt.id)
  closeMenu()
  inputEl.value?.blur()
}

// Nothing is committed on blur — the model only changes via choose(), so
// abandoning a half-typed query leaves the previous selection untouched.
function onBlur() {
  closeMenu()
}

function onKeydown(e: KeyboardEvent) {
  switch (e.key) {
    case 'ArrowDown':
      e.preventDefault()
      if (!open.value) openMenu()
      else highlight.value = Math.min(highlight.value + 1, filtered.value.length - 1)
      break
    case 'ArrowUp':
      e.preventDefault()
      highlight.value = Math.max(highlight.value - 1, 0)
      break
    case 'Enter':
      if (open.value && filtered.value[highlight.value]) {
        e.preventDefault()
        choose(filtered.value[highlight.value])
      }
      break
    case 'Escape':
      if (open.value) {
        e.preventDefault()
        closeMenu()
        inputEl.value?.blur()
      }
      break
  }
}

// Keep the highlighted option visible when arrowing through a long list.
watch(highlight, () => {
  nextTick(() => {
    const active = menuEl.value?.querySelector('.is-active') as HTMLElement | null
    active?.scrollIntoView({ block: 'nearest' })
  })
})

onBeforeUnmount(() => {
  window.removeEventListener('scroll', onViewportChange, true)
  window.removeEventListener('resize', onViewportChange)
})
</script>

<style scoped>
.fsel {
  min-width: 0;
}

.fsel__input {
  width: 100%;
  box-sizing: border-box;
}

/* Teleported to <body>, so it is OUTSIDE .ed-shell — every value below carries
   a literal fallback (matching the item tooltip) rather than relying on the
   editor's CSS vars being in scope here. */
.fsel__menu {
  position: fixed;
  z-index: var(--z-tooltip, 10000);
  margin: 0;
  padding: 4px;
  list-style: none;
  max-height: 240px;
  overflow-y: auto;
  background: rgba(10, 12, 20, 0.98);
  border: 1px solid var(--ed-line, rgba(212, 168, 79, 0.4));
  border-radius: 6px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.6);
}

.fsel__opt {
  padding: 6px 8px;
  border-radius: 4px;
  font-size: 0.9rem;
  color: var(--ed-text, #e8dcc4);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.fsel__opt.is-active {
  background: rgba(184, 150, 79, 0.22);
}
</style>
