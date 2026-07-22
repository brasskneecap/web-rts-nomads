<template>
  <div class="projectile-editor">
    <aside class="projectile-editor__list">
      <button type="button" class="projectile-editor__new" :disabled="busy" @click="newProjectile">+ New Projectile</button>
      <p v-if="loadError" class="projectile-editor__error">{{ loadError }}</p>
      <ul>
        <li v-for="p in projectiles" :key="p.id">
          <button
            type="button"
            data-test="projectile-row"
            :class="{ 'is-selected': p.id === selectedId }"
            @click="selectProjectile(p)"
          >
            {{ p.id }}
          </button>
        </li>
      </ul>
    </aside>

    <section class="projectile-editor__form">
      <section class="projectile-editor__section">
        <h3 class="projectile-editor__section-title">Identity</h3>
        <label>Id <input v-model="form.id" :disabled="selectedId !== null" /></label>
        <label>
          Kind
          <select v-model="form.kind">
            <option value="projectile">projectile</option>
            <option value="beam">beam</option>
          </select>
        </label>
        <label>Duration (ms) <input type="number" v-model.number="form.durationMs" /></label>
        <label>Speed <input type="number" v-model.number="form.speed" /></label>
        <label>
          Follow Effect
          <select v-model="form.followEffect">
            <option value="">(none)</option>
            <option v-for="e in effectIds" :key="e" :value="e">{{ e }}</option>
          </select>
        </label>
        <label>
          Impact Effect
          <select v-model="form.impactEffect">
            <option value="">(none)</option>
            <option v-for="e in effectIds" :key="e" :value="e">{{ e }}</option>
          </select>
        </label>
      </section>

      <p v-if="saveError" class="projectile-editor__error">{{ saveError }}</p>
      <p v-if="statusMessage" class="projectile-editor__status">{{ statusMessage }}</p>
      <div class="projectile-editor__actions">
        <button type="button" :disabled="busy || !form.id" @click="save">Save</button>
        <button type="button" :disabled="busy || selectedId === null" @click="removeProjectile">Delete / Reset</button>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { confirmDelete } from '@/components/editor/confirmDelete'
import { onMounted, ref } from 'vue'
import {
  createBlankForm, formFromDef, saveRequestFromForm,
  type AuthoredProjectileDef, type ProjectileEditorForm,
} from '@/game/projectiles/projectileEditorForm'
import {
  fetchAuthoredProjectileDefs, saveEditorProjectile, deleteEditorProjectile, EditorValidationError,
} from '@/game/projectiles/projectileEditorApi'
import { fetchEffectIds } from '@/game/abilities/abilityEditorApi'

const projectiles = ref<AuthoredProjectileDef[]>([])
const effectIds = ref<string[]>([])
const form = ref<ProjectileEditorForm>(createBlankForm())
const selectedId = ref<string | null>(null)
const saveError = ref('')
const loadError = ref('')
const statusMessage = ref('')
const busy = ref(false)

async function reload() {
  try {
    projectiles.value = await fetchAuthoredProjectileDefs()
    loadError.value = ''
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : String(e)
  }
}

async function loadCatalogs() {
  try {
    effectIds.value = await fetchEffectIds()
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : String(e)
  }
}

function selectProjectile(def: AuthoredProjectileDef) {
  form.value = formFromDef(def)
  selectedId.value = def.id
  saveError.value = ''
  statusMessage.value = ''
}

function newProjectile() {
  form.value = createBlankForm()
  selectedId.value = null
  saveError.value = ''
  statusMessage.value = ''
}

async function save() {
  saveError.value = ''
  statusMessage.value = ''
  busy.value = true
  try {
    await saveEditorProjectile(saveRequestFromForm(form.value))
    await reload()
    selectedId.value = form.value.id
    statusMessage.value = 'Saved.'
  } catch (e) {
    saveError.value = e instanceof EditorValidationError ? e.serverMessage
      : e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function removeProjectile() {
  if (!selectedId.value) return
  if (!(await confirmDelete('projectile', selectedId.value, undefined, 'If it ships with the game it will reset to its built-in default; a custom one is removed.'))) return
  saveError.value = ''
  statusMessage.value = ''
  busy.value = true
  try {
    const status = await deleteEditorProjectile(selectedId.value)
    await reload()
    newProjectile()
    statusMessage.value = status === 'deleted' ? 'Deleted.' : 'Reset to default.'
  } catch (e) {
    saveError.value = e instanceof EditorValidationError ? e.serverMessage
      : e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

onMounted(() => {
  reload()
  loadCatalogs()
})
</script>

<style scoped>
.projectile-editor {
  display: flex;
  width: 100%;
  height: 100%;
  min-height: 0;
  min-width: 0;
  gap: 12px;
  padding: 16px;
  box-sizing: border-box;
}

.projectile-editor__list {
  flex: 0 0 220px;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 8px;
  background: rgba(3, 8, 14, 0.86);
  border: 1px solid rgba(148, 163, 184, 0.22);
  border-radius: 16px;
  padding: 12px;
}

.projectile-editor__list ul {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.projectile-editor__list button {
  width: 100%;
  border: 1px solid transparent;
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.6);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
  text-align: left;
}

.projectile-editor__list button.is-selected {
  border-color: rgba(215, 187, 132, 0.6);
  background: rgba(30, 41, 59, 0.9);
}

.projectile-editor__new {
  font-weight: 700;
}

.projectile-editor__form {
  flex: 1;
  min-width: 0;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 12px;
  background: rgba(3, 8, 14, 0.86);
  border: 1px solid rgba(148, 163, 184, 0.22);
  border-radius: 16px;
  padding: 12px;
}

.projectile-editor__section {
  border: 1px solid rgba(148, 163, 184, 0.18);
  border-radius: 12px;
  background: rgba(8, 14, 24, 0.55);
  padding: 10px;
  display: grid;
  gap: 8px;
}

.projectile-editor__section-title {
  margin: 0;
  font-size: 0.78rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: #d7bb84;
}

.projectile-editor__section label {
  display: grid;
  gap: 4px;
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.75rem;
}

.projectile-editor__section input,
.projectile-editor__section select {
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
}

.projectile-editor__actions {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: auto;
  padding-top: 8px;
}

.projectile-editor__error {
  color: #fca5a5;
  font-size: 0.78rem;
}

.projectile-editor__status {
  color: #86efac;
  font-size: 0.78rem;
}
</style>
