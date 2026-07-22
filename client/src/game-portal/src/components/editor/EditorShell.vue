<template>
  <!-- worldMenu = the black/brass outer frame used by the meta screens
       (advancements, custom game), so an editor reads as part of the same
       world rather than a bolted-on tool. -->
  <UiPanel variant="worldMenu" :padding="0" :class="['ed-shell', theme ? `ed-theme-${theme}` : '']">
    <div
      class="ed-shell__grid"
      :class="{
        'ed-shell__grid--no-rail': !$slots.rail,
        'ed-shell__grid--wide-rail': wideRail && !!$slots.rail,
        'ed-shell__grid--inspector': !!$slots.inspector,
      }"
    >
      <aside class="ed-shell__sidebar">
        <slot name="sidebar" />
      </aside>

      <main class="ed-shell__main">
        <slot name="main" />
      </main>

      <!-- Optional column between main and rail — for a panel whose rail is a
           live preview and which still needs somewhere to edit the selection.
           Absent ⇒ no column and no DOM. -->
      <aside v-if="$slots.inspector" class="ed-shell__inspector">
        <slot name="inspector" />
      </aside>

      <aside v-if="$slots.rail" class="ed-shell__rail">
        <slot name="rail" />
      </aside>
    </div>
  </UiPanel>
</template>

<script setup lang="ts">
import UiPanel from '@/components/ui/UiPanel.vue'
// Form chrome for every control rendered inside .ed-shell. Imported here (not
// in each field component) so a single import covers the whole editor.
import './editor-controls.css'
// Editor skins, scoped under .ed-theme-<name>. `forge` is the DEFAULT (see the
// theme prop below); the older wood look is reachable with theme="".
import './editor-theme.css'

withDefaults(defineProps<{
  /** Rail gets ~2/3 of the content width instead of a fixed narrow column,
   *  and main narrows to match — for panels whose rail is a live preview
   *  rather than a companion (the ability builder). Default false keeps the
   *  five existing EditorShell consumers byte-identical. */
  wideRail?: boolean
  /** Visual skin, applied as `.ed-theme-<theme>` on the shell (see
   *  editor-theme.css).
   *
   *  Defaults to `forge` — the warmer, brass-accented look. It began as an
   *  opt-in while it was proven out on the item / unit / ability editors, but
   *  the older wood look it replaced is dated, and leaving it as the default
   *  meant a NEW editor silently shipped looking like the old ones (which is
   *  exactly what happened to the list and table editors).
   *
   *  Pass `theme=""` for no skin at all — the original wood look. */
  theme?: string
}>(), {
  wideRail: false,
  theme: 'forge',
})
</script>

<style scoped>
.ed-shell {
  width: 100%;
  height: 100%;
  min-height: 0;
  min-width: 0;
}

/* Sidebar | main | rail. The rail is optional — an editor that doesn't need a
   preview collapses to two columns with no layout change of its own. */
.ed-shell__grid {
  display: grid;
  grid-template-columns: minmax(220px, 260px) minmax(0, 1fr) minmax(280px, 340px);
  gap: var(--ed-gap);
  padding: var(--ed-gap);
  width: 100%;
  height: 100%;
  min-height: 0;
  box-sizing: border-box;
}

.ed-shell__grid--no-rail {
  grid-template-columns: minmax(220px, 260px) minmax(0, 1fr);
}

/* Rail becomes the wide column — main narrows to ~1/3 of the content width
   for panels where the rail is a live preview, not a companion. */
.ed-shell__grid--wide-rail {
  grid-template-columns: minmax(220px, 260px) minmax(0, 1fr) minmax(420px, 1.1fr);
}

/* With an inspector column, main gives up the width rather than the preview:
   the flow is a list of short cards and reads fine narrow, while both the
   inspector's fields and the renderer need their space. Listed after the two
   rules above so it wins for the wide-rail + inspector pairing (equal
   specificity — source order decides). */
.ed-shell__grid--inspector {
  grid-template-columns: minmax(200px, 240px) minmax(0, 0.85fr) minmax(260px, 340px) minmax(380px, 1fr);
}

.ed-shell__sidebar,
.ed-shell__inspector,
.ed-shell__rail {
  min-height: 0;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.ed-shell__main {
  min-height: 0;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: var(--ed-gap);
  overflow: hidden;
}

/* Below ~1200px the rail is the first thing to go — the form is the work
   surface, the preview is a companion. Applies in every rail width. An
   inspector column survives the drop (it's an editing surface, not a
   companion), so that variant keeps three columns. */
@media (max-width: 1200px) {
  .ed-shell__grid,
  .ed-shell__grid--no-rail,
  .ed-shell__grid--wide-rail {
    grid-template-columns: minmax(200px, 240px) minmax(0, 1fr);
  }

  .ed-shell__grid--inspector {
    grid-template-columns: minmax(200px, 240px) minmax(0, 1fr) minmax(260px, 320px);
  }

  .ed-shell__rail {
    display: none;
  }
}
</style>
