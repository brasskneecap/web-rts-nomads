<template>
  <div class="ability-editor">
    <aside class="ability-editor__list">
      <button type="button" class="ability-editor__new" :disabled="busy" @click="newAbility">+ New Ability</button>
      <p v-if="loadError" class="ability-editor__error">{{ loadError }}</p>
      <ul>
        <li v-for="a in abilities" :key="a.id">
          <button
            type="button"
            data-test="ability-row"
            :class="{ 'is-selected': a.id === selectedId }"
            @click="selectAbility(a)"
          >
            {{ a.id }} <span v-if="a.displayName">— {{ a.displayName }}</span>
          </button>
        </li>
      </ul>
    </aside>

    <section class="ability-editor__form">
      <!-- Identity -->
      <section class="ability-editor__section">
        <h3 class="ability-editor__section-title">Identity</h3>
        <label>Id <input v-model="form.id" :disabled="selectedId !== null" /></label>
        <label>Display Name <input v-model="form.displayName" /></label>
        <label>
          Type
          <select v-model="form.type">
            <option value="">(none)</option>
            <option value="spell">spell</option>
            <option value="passive">passive</option>
          </select>
        </label>
        <label>
          Category
          <select v-model="form.category">
            <option value="">(none)</option>
            <option v-for="c in abilityCategories" :key="c" :value="c">{{ c }}</option>
          </select>
        </label>
        <label>
          Damage Type
          <select v-model="form.damageType">
            <option value="">(none)</option>
            <option v-for="d in damageTypes" :key="d" :value="d">{{ d }}</option>
          </select>
        </label>
        <label>
          Tags (comma-separated)
          <input
            :value="(form.tags ?? []).join(',')"
            @input="updateStringList('tags', ($event.target as HTMLInputElement).value)"
          />
        </label>
      </section>

      <!-- Targeting -->
      <section class="ability-editor__section">
        <h3 class="ability-editor__section-title">Targeting</h3>
        <label class="ability-editor__checkbox-label">
          <input type="checkbox" v-model="form.canTargetSelf" /> Can Target Self
        </label>
        <label class="ability-editor__checkbox-label">
          <input type="checkbox" v-model="form.canTargetAllies" /> Can Target Allies
        </label>
        <label class="ability-editor__checkbox-label">
          <input type="checkbox" v-model="form.canTargetEnemies" /> Can Target Enemies
        </label>
        <label class="ability-editor__checkbox-label">
          <input type="checkbox" v-model="form.targetsPoint" /> Targets Point
        </label>
        <label class="ability-editor__checkbox-label">
          <input type="checkbox" v-model="castRangeMatchesAttack" /> Match Attack Range
        </label>
        <label v-if="!castRangeMatchesAttack">
          Cast Range
          <input
            type="number"
            :value="typeof form.castRange === 'number' ? form.castRange : 0"
            @input="form.castRange = Number(($event.target as HTMLInputElement).value) || 0"
          />
        </label>
      </section>

      <!-- Cost / Timing -->
      <section class="ability-editor__section">
        <h3 class="ability-editor__section-title">Cost / Timing</h3>
        <label>Cast Time <input type="number" v-model.number="form.castTime" /></label>
        <label>Mana Cost <input type="number" v-model.number="form.manaCost" /></label>
        <label>Cooldown <input type="number" v-model.number="form.cooldown" /></label>
        <label>Target Count <input type="number" v-model.number="form.targetCount" /></label>
      </section>

      <!-- Auto-cast -->
      <section class="ability-editor__section">
        <h3 class="ability-editor__section-title">Auto-cast</h3>
        <label class="ability-editor__checkbox-label">
          <input type="checkbox" v-model="form.supportsAutoCast" /> Supports Auto-cast
        </label>
        <template v-if="form.supportsAutoCast">
          <label>
            Auto-cast Target Selector
            <select v-model="form.autoCastTargetSelector">
              <option value="">(none)</option>
              <option v-for="s in autoCastSelectors" :key="s" :value="s">{{ s }}</option>
            </select>
          </label>
          <label class="ability-editor__checkbox-label">
            <input type="checkbox" v-model="form.defaultAutoCast" /> Default Auto-cast
          </label>
        </template>
      </section>

      <!-- Presentation / Effects -->
      <section class="ability-editor__section">
        <h3 class="ability-editor__section-title">Presentation / Effects</h3>
        <div class="ability-editor__icon-field">
          <span class="ability-editor__icon-field-label">Icon</span>
          <div class="ability-editor__icon-preview-row">
            <canvas
              ref="previewCanvasEl"
              width="64"
              height="64"
              class="ability-editor__icon-preview"
            />
            <div class="ability-editor__icon-preview-actions">
              <button type="button" data-test="icon-gallery-open" @click="galleryOpen = true">
                Choose from gallery
              </button>
              <label>
                Upload custom icon <span class="ability-editor__hint">(PNG)</span>
                <input type="file" accept="image/png" @change="onIconFileChosen" />
              </label>
              <p v-if="iconUploadError" class="ability-editor__error">{{ iconUploadError }}</p>
            </div>
          </div>

          <div v-if="galleryOpen" class="ability-editor__icon-gallery-overlay">
            <div class="ability-editor__icon-gallery">
              <div class="ability-editor__icon-gallery-header">
                <span>Choose an icon</span>
                <button type="button" @click="galleryOpen = false">Close</button>
              </div>
              <div v-if="galleryKeys.length" class="ability-editor__icon-gallery-grid">
                <button
                  v-for="key in galleryKeys"
                  :key="key"
                  type="button"
                  class="ability-editor__icon-gallery-item"
                  data-test="icon-gallery-cell"
                  @click="pickGalleryIcon(key)"
                >
                  <canvas :ref="(el) => onGalleryCellRef(el, key)" width="40" height="40" />
                  <span>{{ key }}</span>
                </button>
              </div>
              <p v-else class="ability-editor__icon-gallery-empty">No bundled ability icons found.</p>
            </div>
          </div>
        </div>
        <label>Caster Animation <input v-model="form.casterAnimation" /></label>
        <label>
          Effect On Target
          <select v-model="form.effectOnTarget">
            <option value="">(none)</option>
            <option v-for="e in effectIds" :key="e" :value="e">{{ e }}</option>
          </select>
        </label>
        <label>
          Effect At Point
          <select v-model="form.effectAtPoint">
            <option value="">(none)</option>
            <option v-for="e in effectIds" :key="e" :value="e">{{ e }}</option>
          </select>
        </label>
        <label>
          Burn Effect At Point
          <select v-model="form.burnEffectAtPoint">
            <option value="">(none)</option>
            <option v-for="e in effectIds" :key="e" :value="e">{{ e }}</option>
          </select>
        </label>
        <label>Effect Scale <input type="number" v-model.number="form.effectScale" /></label>
        <label>
          Projectile
          <select v-model="form.projectile">
            <option value="">(none)</option>
            <option v-for="p in projectileIds" :key="p" :value="p">{{ p }}</option>
          </select>
        </label>
      </section>

      <!-- Family -->
      <section class="ability-editor__section">
        <h3 class="ability-editor__section-title">Family</h3>
        <label>
          Family
          <select v-model="selectedFamily">
            <option value="basic">basic</option>
            <option value="channel">channel</option>
            <option value="charge">charge</option>
            <option value="meteor">meteor</option>
            <option value="archmage">archmage</option>
          </select>
        </label>

        <div v-if="selectedFamily === 'basic'" class="ability-editor__family-body">
          <label>Heal Amount <input type="number" v-model.number="form.healAmount" /></label>
          <label>Damage Amount <input type="number" v-model.number="form.damageAmount" /></label>
          <label>Damage Per Second <input type="number" v-model.number="form.damagePerSecond" /></label>
          <label class="ability-editor__checkbox-label">
            <input type="checkbox" v-model="form.minorDamage" /> Minor Damage
          </label>
          <label>
            Summon Unit Type
            <select v-model="form.summonUnitType">
              <option value="">(none)</option>
              <option v-for="u in unitTypeIds" :key="u" :value="u">{{ u }}</option>
            </select>
          </label>
          <label>Summon Count <input type="number" v-model.number="form.summonCount" /></label>
        </div>

        <div v-else-if="selectedFamily === 'channel'" class="ability-editor__family-body">
          <label>Channel Type <input v-model="form.channelType" /></label>
          <label>Tick Interval Seconds <input type="number" v-model.number="form.tickIntervalSeconds" /></label>
          <label>Mana Cost Per Tick <input type="number" v-model.number="form.manaCostPerTick" /></label>
          <label>Damage Per Tick <input type="number" v-model.number="form.damagePerTick" /></label>
          <label>Healing Multiplier <input type="number" v-model.number="form.healingMultiplier" /></label>
          <label>Ally Heal Radius <input type="number" v-model.number="form.allyHealRadius" /></label>
        </div>

        <div v-else-if="selectedFamily === 'charge'" class="ability-editor__family-body">
          <label>Charge Required <input type="number" v-model.number="form.chargeRequired" /></label>
          <label>Mana To Charge Ratio <input type="number" v-model.number="form.manaToChargeRatio" /></label>
          <label>Missile Count <input type="number" v-model.number="form.missileCount" /></label>
          <label>Damage Per Missile <input type="number" v-model.number="form.damagePerMissile" /></label>
          <label>Targeting <input v-model="form.targeting" /></label>
          <label class="ability-editor__checkbox-label">
            <input type="checkbox" v-model="form.allowDuplicateTargets" /> Allow Duplicate Targets
          </label>
          <label>Missile Delay Ms <input type="number" v-model.number="form.missileDelayMs" /></label>
        </div>

        <div v-else-if="selectedFamily === 'meteor'" class="ability-editor__family-body">
          <label>Impact Delay Seconds <input type="number" v-model.number="form.impactDelaySeconds" /></label>
          <label>Burn Duration Seconds <input type="number" v-model.number="form.burnDurationSeconds" /></label>
          <label>Burn Damage Per Tick <input type="number" v-model.number="form.burnDamagePerTick" /></label>
          <label>Burn Tick Interval Seconds <input type="number" v-model.number="form.burnTickIntervalSeconds" /></label>
          <label>Burn Radius <input type="number" v-model.number="form.burnRadius" /></label>
        </div>

        <div v-else-if="selectedFamily === 'archmage'" class="ability-editor__family-body">
          <label>Radius <input type="number" v-model.number="form.radius" /></label>
          <label>Projectile Speed <input type="number" v-model.number="form.projectileSpeed" /></label>
          <label>Projectile Scale <input type="number" v-model.number="form.projectileScale" /></label>
          <label>Duration <input type="number" v-model.number="form.duration" /></label>
          <label>Chain Count <input type="number" v-model.number="form.chainCount" /></label>
          <label>Bounce Range <input type="number" v-model.number="form.bounceRange" /></label>
          <label>Bounce Damage Falloff <input type="number" v-model.number="form.bounceDamageFalloff" /></label>
          <label>Pull Strength <input type="number" v-model.number="form.pullStrength" /></label>
          <label>Slow Multiplier <input type="number" v-model.number="form.slowMultiplier" /></label>
          <label>Slow Duration Seconds <input type="number" v-model.number="form.slowDurationSeconds" /></label>
        </div>
      </section>

      <p v-if="saveError" class="ability-editor__error">{{ saveError }}</p>
      <p v-if="statusMessage" class="ability-editor__status">{{ statusMessage }}</p>
      <div class="ability-editor__actions">
        <button type="button" :disabled="busy || !form.id" @click="save">Save</button>
        <button type="button" :disabled="busy || selectedId === null" @click="removeAbility">Delete / Reset</button>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import {
  createBlankForm, formFromDef, inferFamily, saveRequestFromForm,
  type AbilityEditorForm, type AbilityFamily, type AuthoredAbilityDef,
} from '@/game/abilities/abilityEditorForm'
import {
  fetchAuthoredAbilityDefs, fetchProjectileIds, fetchEffectIds, fetchAutoCastSelectors,
  fetchAbilityCategories, fetchDamageTypes, saveEditorAbility, deleteEditorAbility,
  uploadAbilityIcon, EditorValidationError,
} from '@/game/abilities/abilityEditorApi'
import { fetchAuthoredUnitDefs } from '@/game/units/unitEditorApi'
import { getAbilityIconSourceUrl, getAbilityPreviewUrl, listAbilityIconKeys } from '@/game/rendering/abilityAssets'
import { inferProjectileFrameCount } from '@/game/rendering/projectileSprites'

const abilities = ref<AuthoredAbilityDef[]>([])
const form = ref<AbilityEditorForm>(createBlankForm())
const selectedId = ref<string | null>(null)
const selectedFamily = ref<AbilityFamily>('basic')
const saveError = ref('')
const loadError = ref('')
const statusMessage = ref('')
const busy = ref(false)

const projectileIds = ref<string[]>([])
const effectIds = ref<string[]>([])
const autoCastSelectors = ref<string[]>([])
const abilityCategories = ref<string[]>([])
const damageTypes = ref<string[]>([])
const unitTypeIds = ref<string[]>([])

// Icon section: preview canvas draws the FIRST frame of a (possibly
// multi-frame) bundled or server-uploaded icon sprite — mirrors
// ActionIcon.vue's drawActionSpriteFirstFrame. The gallery overlay lists
// bundled ability-icon keys (assets/abilities/**); picking one just sets
// form.icon, which already saves via saveRequestFromForm.
const galleryOpen = ref(false)
const iconUploadError = ref('')
const previewCanvasEl = ref<HTMLCanvasElement | null>(null)
const galleryKeys = listAbilityIconKeys()
// Bumped after a successful upload so the preview re-fetches the new bytes
// even though the server URL for the (unchanged) icon key is identical.
const iconCacheBust = ref(0)

const previewIconUrl = computed(() => {
  const base = getAbilityPreviewUrl(form.value.icon, form.value.id)
  if (!base) return ''
  if (iconCacheBust.value === 0) return base
  return `${base}${base.includes('?') ? '&' : '?'}v=${iconCacheBust.value}`
})

function drawIconFirstFrame(canvas: HTMLCanvasElement | null, url: string) {
  if (!canvas) return
  const ctx = canvas.getContext('2d')
  if (!ctx) return
  ctx.clearRect(0, 0, canvas.width, canvas.height)
  if (!url) return
  const img = new Image()
  img.onload = () => {
    const frames = inferProjectileFrameCount(img.naturalWidth, img.naturalHeight)
    const sw = img.naturalWidth / frames
    const sh = img.naturalHeight
    ctx.imageSmoothingEnabled = false
    ctx.clearRect(0, 0, canvas.width, canvas.height)
    ctx.drawImage(img, 0, 0, sw, sh, 0, 0, canvas.width, canvas.height)
  }
  img.src = url
}

watch(previewIconUrl, (url) => drawIconFirstFrame(previewCanvasEl.value, url))

// Callback ref for a gallery cell canvas — Vue invokes this with the mounted
// element (or null on unmount) each time the overlay's v-for re-renders.
function onGalleryCellRef(el: unknown, key: string) {
  if (!(el instanceof HTMLCanvasElement)) return
  drawIconFirstFrame(el, getAbilityIconSourceUrl(key))
}

function pickGalleryIcon(key: string) {
  form.value.icon = key
  galleryOpen.value = false
}

async function onIconFileChosen(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  iconUploadError.value = ''
  // selectedId is null only for a brand-new, unsaved ability (see newAbility /
  // save below) — the same "is this persisted yet" signal the Delete/Reset
  // button already relies on.
  if (selectedId.value === null || !form.value.id) {
    iconUploadError.value = 'Save the ability before uploading an icon.'
    input.value = ''
    return
  }
  try {
    await uploadAbilityIcon(form.value.id, file)
    form.value.icon = form.value.id // server forces the icon key to the id
    iconCacheBust.value += 1
  } catch (err) {
    iconUploadError.value = err instanceof Error ? err.message : String(err)
  } finally {
    input.value = ''
  }
}

const castRangeMatchesAttack = computed({
  get: () => form.value.castRange === 'match_attack_range',
  set: (checked: boolean) => {
    form.value.castRange = checked ? 'match_attack_range' : 0
  },
})

type StringListField = 'tags'
function updateStringList(field: StringListField, raw: string) {
  form.value[field] = raw.split(',').map((s) => s.trim()).filter(Boolean)
}

async function reload() {
  try {
    abilities.value = await fetchAuthoredAbilityDefs()
    loadError.value = ''
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : String(e)
  }
}

async function loadCatalogs() {
  try {
    const [projectiles, effects, autoCast, categories, damage, units] = await Promise.all([
      fetchProjectileIds(),
      fetchEffectIds(),
      fetchAutoCastSelectors(),
      fetchAbilityCategories(),
      fetchDamageTypes(),
      fetchAuthoredUnitDefs(),
    ])
    projectileIds.value = projectiles
    effectIds.value = effects
    autoCastSelectors.value = autoCast
    abilityCategories.value = categories
    damageTypes.value = damage
    unitTypeIds.value = units.map((u) => u.type)
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : String(e)
  }
}

function selectAbility(def: AuthoredAbilityDef) {
  form.value = formFromDef(def)
  selectedId.value = def.id
  selectedFamily.value = inferFamily(def)
  saveError.value = ''
  statusMessage.value = ''
  iconUploadError.value = ''
  galleryOpen.value = false
}

function newAbility() {
  form.value = createBlankForm()
  selectedId.value = null
  selectedFamily.value = 'basic'
  saveError.value = ''
  statusMessage.value = ''
  iconUploadError.value = ''
  galleryOpen.value = false
}

async function save() {
  saveError.value = ''
  statusMessage.value = ''
  busy.value = true
  try {
    await saveEditorAbility(saveRequestFromForm(form.value))
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

async function removeAbility() {
  if (!selectedId.value) return
  saveError.value = ''
  statusMessage.value = ''
  busy.value = true
  try {
    const status = await deleteEditorAbility(selectedId.value)
    await reload()
    newAbility()
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
  drawIconFirstFrame(previewCanvasEl.value, previewIconUrl.value)
})
</script>

<style scoped>
.ability-editor {
  display: flex;
  width: 100%;
  height: 100%;
  min-height: 0;
  min-width: 0;
  gap: 12px;
  padding: 16px;
  box-sizing: border-box;
}

.ability-editor__list {
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

.ability-editor__list ul {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.ability-editor__list button {
  width: 100%;
  border: 1px solid transparent;
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.6);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
  text-align: left;
}

.ability-editor__list button.is-selected {
  border-color: rgba(215, 187, 132, 0.6);
  background: rgba(30, 41, 59, 0.9);
}

.ability-editor__new {
  font-weight: 700;
}

.ability-editor__form {
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

.ability-editor__section {
  border: 1px solid rgba(148, 163, 184, 0.18);
  border-radius: 12px;
  background: rgba(8, 14, 24, 0.55);
  padding: 10px;
  display: grid;
  gap: 8px;
}

.ability-editor__section-title {
  margin: 0;
  font-size: 0.78rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: #d7bb84;
}

.ability-editor__section label {
  display: grid;
  gap: 4px;
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.75rem;
}

.ability-editor__section input,
.ability-editor__section select {
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
}

.ability-editor__checkbox-label {
  flex-direction: row !important;
  align-items: center;
  gap: 6px !important;
}

.ability-editor__family-body {
  display: grid;
  gap: 8px;
}

/* Icon section: preview canvas + gallery overlay, mirrors ItemEditorPanel's
   icon-preview-row / icon-gallery-* idiom (scoped styles aren't shared across
   SFCs — duplication accepted). */
.ability-editor__icon-field {
  display: grid;
  gap: 4px;
}

.ability-editor__icon-field-label {
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.75rem;
}

.ability-editor__icon-preview-row {
  display: flex;
  gap: 12px;
  align-items: flex-start;
}

.ability-editor__icon-preview {
  width: 64px;
  height: 64px;
  image-rendering: pixelated;
  border: 1px solid rgba(148, 163, 184, 0.24);
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.92);
}

.ability-editor__icon-preview-actions {
  display: flex;
  flex-direction: column;
  gap: 8px;
  flex: 1;
}

.ability-editor__hint {
  font-weight: 400;
  opacity: 0.65;
}

.ability-editor__icon-gallery-overlay {
  position: fixed;
  inset: 0;
  background: rgba(3, 8, 14, 0.72);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 50;
}

.ability-editor__icon-gallery {
  width: min(640px, 90vw);
  max-height: 80vh;
  overflow-y: auto;
  background: rgba(8, 14, 24, 0.96);
  border: 1px solid rgba(148, 163, 184, 0.24);
  border-radius: 12px;
  padding: 12px;
}

.ability-editor__icon-gallery-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
  color: #f8fafc;
  font-weight: 700;
}

.ability-editor__icon-gallery-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(72px, 1fr));
  gap: 8px;
}

.ability-editor__icon-gallery-item {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.72);
  padding: 6px;
}

.ability-editor__icon-gallery-item canvas {
  width: 40px;
  height: 40px;
  image-rendering: pixelated;
}

.ability-editor__icon-gallery-item span {
  font-size: 0.62rem;
  color: rgba(226, 232, 240, 0.82);
  text-align: center;
  word-break: break-all;
}

.ability-editor__icon-gallery-empty {
  color: rgba(148, 163, 184, 0.9);
  font-size: 0.8rem;
  text-align: center;
  padding: 24px 0;
}

.ability-editor__actions {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: auto;
  padding-top: 8px;
}

.ability-editor__error {
  color: #fca5a5;
  font-size: 0.78rem;
}

.ability-editor__status {
  color: #86efac;
  font-size: 0.78rem;
}
</style>
