<template>
  <div class="effect-editor">
    <aside class="effect-editor__list">
      <button type="button" class="effect-editor__new" :disabled="busy" @click="newEffect">+ New Effect</button>
      <p v-if="loadError" class="effect-editor__error">{{ loadError }}</p>
      <ul>
        <li v-for="e in effects" :key="e.id">
          <button
            type="button"
            data-test="effect-row"
            :class="{ 'is-selected': e.id === selectedId }"
            @click="selectEffect(e)"
          >
            {{ e.id }}
          </button>
        </li>
      </ul>
    </aside>

    <section class="effect-editor__form">
      <section class="effect-editor__section">
        <h3 class="effect-editor__section-title">Identity</h3>
        <label>Id <input v-model="form.id" :disabled="selectedId !== null" /></label>
        <label>Sprite Path <input v-model="form.spritePath" /></label>
        <label>Duration <input type="number" v-model.number="form.duration" /></label>
        <label>
          Anchor
          <select v-model="form.anchor">
            <option value="">(center — default)</option>
            <option value="center">center</option>
            <option value="feet">feet</option>
            <option value="head">head</option>
          </select>
        </label>
      </section>

      <p v-if="saveError" class="effect-editor__error">{{ saveError }}</p>
      <p v-if="statusMessage" class="effect-editor__status">{{ statusMessage }}</p>
      <div class="effect-editor__actions">
        <button type="button" :disabled="busy || !form.id" @click="save">Save</button>
        <button type="button" :disabled="busy || selectedId === null" @click="removeEffect">Delete / Reset</button>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { confirmDelete } from '@/components/editor/confirmDelete'
import { onMounted, ref } from 'vue'
import {
  createBlankForm, formFromDef, saveRequestFromForm,
  type AuthoredEffectDef, type EffectEditorForm,
} from '@/game/effects/effectEditorForm'
import {
  fetchAuthoredEffectDefs, saveEditorEffect, deleteEditorEffect, EditorValidationError,
} from '@/game/effects/effectEditorApi'

const effects = ref<AuthoredEffectDef[]>([])
const form = ref<EffectEditorForm>(createBlankForm())
const selectedId = ref<string | null>(null)
const saveError = ref('')
const loadError = ref('')
const statusMessage = ref('')
const busy = ref(false)

async function reload() {
  try {
    effects.value = await fetchAuthoredEffectDefs()
    loadError.value = ''
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : String(e)
  }
}

function selectEffect(def: AuthoredEffectDef) {
  form.value = formFromDef(def)
  selectedId.value = def.id
  saveError.value = ''
  statusMessage.value = ''
}

function newEffect() {
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
    await saveEditorEffect(saveRequestFromForm(form.value))
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

async function removeEffect() {
  if (!selectedId.value) return
  if (!(await confirmDelete('effect', selectedId.value, undefined, 'If it ships with the game it will reset to its built-in default; a custom one is removed.'))) return
  saveError.value = ''
  statusMessage.value = ''
  busy.value = true
  try {
    const status = await deleteEditorEffect(selectedId.value)
    await reload()
    newEffect()
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
})
</script>

<style scoped>
.effect-editor {
  display: flex;
  width: 100%;
  height: 100%;
  min-height: 0;
  min-width: 0;
  gap: 12px;
  padding: 16px;
  box-sizing: border-box;
}

.effect-editor__list {
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

.effect-editor__list ul {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.effect-editor__list button {
  width: 100%;
  border: 1px solid transparent;
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.6);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
  text-align: left;
}

.effect-editor__list button.is-selected {
  border-color: rgba(215, 187, 132, 0.6);
  background: rgba(30, 41, 59, 0.9);
}

.effect-editor__new {
  font-weight: 700;
}

.effect-editor__form {
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

.effect-editor__section {
  border: 1px solid rgba(148, 163, 184, 0.18);
  border-radius: 12px;
  background: rgba(8, 14, 24, 0.55);
  padding: 10px;
  display: grid;
  gap: 8px;
}

.effect-editor__section-title {
  margin: 0;
  font-size: 0.78rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: #d7bb84;
}

.effect-editor__section label {
  display: grid;
  gap: 4px;
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.75rem;
}

.effect-editor__section input,
.effect-editor__section select {
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
}

.effect-editor__actions {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: auto;
  padding-top: 8px;
}

.effect-editor__error {
  color: #fca5a5;
  font-size: 0.78rem;
}

.effect-editor__status {
  color: #86efac;
  font-size: 0.78rem;
}
</style>
