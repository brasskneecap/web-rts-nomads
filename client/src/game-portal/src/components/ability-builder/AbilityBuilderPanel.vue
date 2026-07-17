<template>
  <EditorShell class="ab-panel" wide-rail>
    <!-- ── Sidebar: abilities grouped by damage school ─────────────────────── -->
    <template #sidebar>
      <div class="ab-panel__sidebar">
        <EditorSidebar
          title="Abilities"
          new-label="Add New Ability"
          :groups="sidebarGroups"
          :selected-id="builder.editing.value ? builder.form.value.id : ''"
          :search="search"
          search-placeholder="Search abilities…"
          empty-text="No abilities match."
          @update:search="search = $event"
          @select="onSelect"
          @new="onNew"
          @duplicate="onDuplicate"
        />
        <p v-if="builder.loadError.value" class="ab-panel__error ab-panel__load-error">
          {{ builder.loadError.value }}
        </p>
      </div>
    </template>

    <!-- ── Main: header + flow ──────────────────────────────────────────────── -->
    <template #main>
      <div v-if="!builder.editing.value" class="ab-panel__empty">
        <p v-if="builder.loadError.value" role="alert">{{ builder.loadError.value }}</p>
        <p v-else>Select an ability, or create a new one.</p>
      </div>

      <template v-else>
        <EditorHeader
          :title="builder.form.value.displayName || builder.form.value.id || 'New Ability'"
          :badge="builder.form.value.damageType || ''"
          :breadcrumb="headerBreadcrumb"
          :file-path="filePath"
          :id="builder.form.value.id"
          :id-editable="isNewAbility"
          id-input-id="ab-id"
          :saving="builder.busy.value"
          :save-disabled="saveDisabled"
          :saved-label="builder.savedLabel.value"
          :error="builder.saveError.value"
          :remove-label="isNewAbility ? '' : 'Delete / Reset'"
          @update:id="onIdInput"
          @save="builder.save"
          @remove="builder.remove"
        />

        <div class="ab-panel__toolbar">
          <UiButton
            v-if="builder.isLegacy.value"
            size="sm"
            variant="active"
            data-test="convert-button"
            @click="convertDialogOpen = true"
          >
            Convert to Composable
          </UiButton>
          <UiButton
            size="sm"
            variant="secondary"
            :disabled="!builder.canUndo.value"
            @click="builder.undo"
          >Undo</UiButton>
          <UiButton
            size="sm"
            variant="secondary"
            :disabled="!builder.canRedo.value"
            @click="builder.redo"
          >Redo</UiButton>
          <div
            v-if="issueSummary"
            class="ab-panel__validation"
            data-test="validation-summary"
            :class="{ 'ab-panel__validation--error': hasErrors, 'ab-panel__validation--warning': !hasErrors }"
          >
            <span>{{ issueSummary }}</span>
            <span v-if="hasErrors" class="ab-panel__validation-blocked">— Save is blocked until fixed</span>
          </div>
        </div>

        <!-- Tabs sit directly under the header/toolbar, matching the unit and
             item editors (EditorHeader -> EditorTabs -> scrolled content).
             EditorTabs is the shared strip, so the tablist's arrow-key
             semantics are inherited rather than re-derived here. -->
        <EditorTabs
          v-model="activeTab"
          :tabs="MAIN_TABS"
          id-prefix="ability-builder"
          label="Ability sections"
          class="ab-panel__tabs"
        />

        <GameScrollArea class="ab-panel__scroll">
          <!-- The overview card rides above BOTH tabs' content: it's a compact
               read-only summary that stays useful while building, and its
               "Open ability settings" button is a shortcut INTO Identity —
               which only means anything from the Build tab. -->
          <AbilityOverviewCard @open-identity="activeTab = 'identity'" />

          <IdentityTab v-if="activeTab === 'identity'" />
          <AbilityFlow v-else />
        </GameScrollArea>
      </template>
    </template>

    <!-- ── Rail: the live preview, ALWAYS mounted while an ability is open ──
         (both tabs) so Run Preview's playback/scrub state survives switching
         between Identity and Build Ability — it must NEVER be wrapped in a
         v-if keyed on activeTab. Gated on builder.editing (not on a tab)
         purely because there's nothing to preview before an ability is
         selected/created, matching the empty-state gate the rest of #main
         already uses. -->
    <template v-if="builder.editing.value" #rail>
      <AbilityPreviewPanel />
    </template>

    <!-- ── Inspector: schema-driven fields for the flow-selected trigger/
         action, in its own column between the flow and the preview. Same
         editing gate as the rail — nothing to inspect before an ability is
         open. -->
    <template v-if="builder.editing.value" #inspector>
      <InspectorBar />
    </template>
  </EditorShell>

  <!-- Fixed-position overlay: a sibling of EditorShell (Vue 3 multi-root),
       not nested inside it — EditorShell only exposes sidebar/main/rail
       named slots, and inset:0 positioning doesn't care where in the DOM
       tree it sits anyway. -->
  <ConvertDialog :open="convertDialogOpen" @close="convertDialogOpen = false" />
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, provide, ref, watch } from 'vue'
import EditorShell from '@/components/editor/EditorShell.vue'
import EditorSidebar from '@/components/editor/EditorSidebar.vue'
import type { SidebarGroup } from '@/components/editor/EditorSidebar.vue'
import EditorHeader from '@/components/editor/EditorHeader.vue'
import EditorTabs, { type EditorTab } from '@/components/editor/EditorTabs.vue'
import GameScrollArea from '@/components/ui/GameScrollArea.vue'
import UiButton from '@/components/ui/UiButton.vue'
import AbilityOverviewCard from './AbilityOverviewCard.vue'
import AbilityFlow from './AbilityFlow.vue'
import AbilityPreviewPanel from './AbilityPreviewPanel.vue'
import IdentityTab from './IdentityTab.vue'
import InspectorBar from './InspectorBar.vue'
import ConvertDialog from './ConvertDialog.vue'
import { useAbilityBuilder } from './useAbilityBuilder'
import { AbilityBuilderKey } from './AbilityBuilderContext'
import { hasBlockingError } from '@/game/abilities/program/programValidation'

const builder = useAbilityBuilder()
provide(AbilityBuilderKey, builder)

// ── Convert-to-composable dialog ──────────────────────────────────────────
// The toolbar's "Convert to Composable" button opens this instead of calling
// builder.convert() directly, so the server's degradation warnings are
// always shown to the author (see ConvertDialog.vue) rather than silently
// applied.
const convertDialogOpen = ref(false)

// ── Identity / Build Ability main-tab toggle ──────────────────────────────
// Local, not part of the builder composable — it's pure UI state, not
// editing state, and switching it never touches form/program. Reset to
// 'identity' whenever a different ability is opened (onSelect/onNew below)
// so a freshly-opened ability doesn't land on a stale Build Ability view
// showing a PREVIOUS ability's flow scroll position.
//
// Note this is independent of `builder.selected` (the flow's trigger/action
// selection) — the preview rail and the InspectorBar both key off
// `builder.selected`, not this tab, which is exactly what keeps the rail
// mounted across tab switches (see the #rail template's comment).
// Typed as plain string (not the 'identity' | 'build' union) because that's
// EditorTabs' v-model contract — the ids below are the only values written.
const activeTab = ref<string>('identity')

const MAIN_TABS: EditorTab[] = [
  { id: 'identity', label: 'Identity' },
  { id: 'build', label: 'Build Ability' },
]

// Landing on Identity also selects the ability node. This is the Task 4
// stale-selection decision: without it, switching tabs after selecting a
// trigger/action in Build would leave the bottom InspectorBar still showing
// that node's fields while Identity's own fields sit on screen above it,
// which reads as two panels disagreeing about what's selected. Forcing the
// ability node keeps the tab and the bottom bar's empty-selection hint in
// agreement.
//
// Watching activeTab (rather than builder.selected) is what makes this fire
// reliably: `selected` is ALREADY {kind:'ability'} much of the time, so a
// watcher keyed on it silently no-ops — that bug ate an "Open ability
// settings" click during Task 4. activeTab genuinely transitions, so both
// the tab strip and AbilityOverviewCard's open-identity emit route through
// this one watcher.
watch(activeTab, (tab) => {
  if (tab === 'identity') builder.select({ kind: 'ability' })
})

onMounted(() => {
  void builder.load()
})

// ── Sidebar selection / search ────────────────────────────────────────────
const search = ref('')

// isNewAbility is DERIVED, not tracked locally: a brand-new (never-saved)
// ability's form.id is either empty or a not-yet-persisted id, so it won't
// appear in builder.abilities (the loaded catalog). Once save() succeeds, it
// reloads the abilities list and reselects — the saved id now IS in the
// list, so this flips to false and the ID field locks automatically, with
// no extra bookkeeping (and no risk of a locally-tracked flag drifting out
// of sync with what actually got saved).
const isNewAbility = computed(() =>
  builder.editing.value && !builder.abilities.value.some((a) => a.id === builder.form.value.id),
)

const sidebarGroups = computed<SidebarGroup[]>(() => {
  const q = search.value.trim().toLowerCase()
  const matches = builder.abilities.value.filter((a) => {
    if (!q) return true
    return a.id.toLowerCase().includes(q) || (a.displayName ?? '').toLowerCase().includes(q)
  })
  const bySchool = new Map<string, typeof matches>()
  for (const a of matches) {
    const school = a.damageType || 'Unspecified'
    const list = bySchool.get(school) ?? []
    list.push(a)
    bySchool.set(school, list)
  }
  return [...bySchool.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([school, list]) => ({
      label: school,
      entries: list
        .slice()
        .sort((a, b) => (a.displayName ?? a.id).localeCompare(b.displayName ?? b.id))
        .map((a) => ({ id: a.id, name: a.displayName ? `${a.displayName}` : a.id })),
    }))
})

// ── Unsaved-changes guard ─────────────────────────────────────────────────
// A plain window.confirm is sufficient for Task 2; a nicer in-app dialog can
// replace this later without changing the call sites below.
function confirmDiscard(): boolean {
  if (!builder.dirty.value) return true
  return window.confirm('Discard unsaved changes?')
}

function onSelect(id: string) {
  if (!confirmDiscard()) return
  builder.selectAbility(id)
  activeTab.value = 'identity'
}

function onNew() {
  if (!confirmDiscard()) return
  builder.newAbility()
  activeTab.value = 'identity'
}

// duplicateAbility: full duplicate-as-new-unsaved-draft is a later
// refinement (needs a dedicated composable op so the id/undo-stack reset
// happens atomically). For now, open the source ability for editing so the
// author has a starting point to rename and Save-As.
function onDuplicate(id: string) {
  if (!confirmDiscard()) return
  builder.selectAbility(id)
}

// ── Header ─────────────────────────────────────────────────────────────────
const typeLabel = computed(() => {
  if (builder.form.value.type === 'spell') return 'Spell'
  if (builder.form.value.type === 'passive') return 'Passive'
  return ''
})

const headerBreadcrumb = computed(() =>
  [typeLabel.value, builder.form.value.category].filter(Boolean).join(' • '),
)

const filePath = computed(() => {
  const id = builder.form.value.id
  return id ? `server/internal/game/catalog/abilities/${id}/${id}.json` : ''
})

// A LEGACY ability must be converted before it can be saved (the composable
// guards this too — see useAbilityBuilder.save — but disabling the button
// makes the requirement visible instead of surfacing only as a save error).
const saveDisabled = computed(() =>
  builder.busy.value ||
  !builder.form.value.id ||
  builder.isLegacy.value ||
  hasBlockingError(builder.issues.value),
)

const errorCount = computed(() => builder.issues.value.filter((i) => i.severity === 'error').length)
const hasErrors = computed(() => errorCount.value > 0)

const issueSummary = computed(() => {
  const issues = builder.issues.value
  if (!issues.length) return ''
  const errors = errorCount.value
  const warnings = issues.length - errors
  const parts: string[] = []
  if (errors) parts.push(`${errors} error${errors === 1 ? '' : 's'}`)
  if (warnings) parts.push(`${warnings} warning${warnings === 1 ? '' : 's'}`)
  return parts.join(' · ')
})

function onIdInput(raw: string) {
  if (!isNewAbility.value) return
  const sanitized = raw.toLowerCase().replace(/[^a-z0-9_]/g, '')
  builder.updateForm({ id: sanitized })
}

// ── Keyboard: Ctrl+Z / Ctrl+Shift+Z / Ctrl+Y for undo/redo ───────────────────
// Attached at the window level (not the panel's DOM subtree) so focus
// starting outside the shell — e.g. nothing focused yet after mount — still
// reaches it. Skipped while a text input/textarea has focus so native undo
// inside a field isn't clobbered.
function onWindowKeydown(e: KeyboardEvent) {
  if (!builder.editing.value) return
  const tag = document.activeElement?.tagName
  if (tag === 'INPUT' || tag === 'TEXTAREA') return
  if (!(e.ctrlKey || e.metaKey)) return
  const key = e.key.toLowerCase()
  if (key === 'z' && e.shiftKey) {
    e.preventDefault()
    builder.redo()
  } else if (key === 'z') {
    e.preventDefault()
    builder.undo()
  } else if (key === 'y') {
    e.preventDefault()
    builder.redo()
  }
}

onMounted(() => {
  window.addEventListener('keydown', onWindowKeydown)
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onWindowKeydown)
})
</script>

<style scoped>
.ab-panel {
  width: 100%;
  height: 100%;
}

.ab-panel__sidebar {
  display: flex;
  flex-direction: column;
  gap: 8px;
  height: 100%;
  min-height: 0;
}

.ab-panel__load-error {
  padding: 0 4px;
}

.ab-panel__empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: var(--ed-text-dim);
  font-size: 0.9rem;
}

.ab-panel__toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 0 0 auto;
}

.ab-panel__validation {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 0.76rem;
}

.ab-panel__validation--error {
  color: var(--ed-danger);
}

.ab-panel__validation--warning {
  color: #e0b258;
}

/* Matches the unit editor's tab strip: the tabs sit on a rule that reads as
   the top edge of the content below them. EditorTabs owns the tab chrome
   itself — only the strip's seam against the content is ours. */
.ab-panel__tabs {
  border-bottom: 1px solid var(--ed-line);
}

.ab-panel__validation-blocked {
  color: var(--ed-danger);
  font-weight: 600;
}

.ab-panel__scroll {
  flex: 1 1 auto;
  min-height: 0;
}

.ab-panel__error {
  color: #fca5a5;
  font-size: 0.78rem;
}
</style>
