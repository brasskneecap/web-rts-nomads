<template>
  <EditorShell class="ability-editor">
    <!-- ── Sidebar: abilities grouped by damage school ─────────────────────── -->
    <template #sidebar>
      <div class="ability-sidebar">
        <EditorSidebar
          title="Abilities"
          new-label="Add New Ability"
          :groups="sidebarGroups"
          :selected-id="selectedId ?? ''"
          :search="search"
          search-placeholder="Search abilities…"
          empty-text="No abilities match."
          @update:search="search = $event"
          @select="selectAbility"
          @new="newAbility"
          @duplicate="duplicateAbility"
        />
        <p v-if="loadError" class="ability-editor__error ability-sidebar__load-error">{{ loadError }}</p>
      </div>
    </template>

    <!-- ── Main: the ability form ──────────────────────────────────────────── -->
    <template #main>
      <div v-if="!editing" class="ability-editor__empty">
        <p v-if="loadError" role="alert">{{ loadError }}</p>
        <p v-else>Select an ability, or create a new one.</p>
      </div>

      <template v-else>
        <EditorHeader
          :title="form.displayName || form.id || 'New Ability'"
          :badge="form.damageType || ''"
          :breadcrumb="headerBreadcrumb"
          :file-path="filePath"
          :id="form.id"
          :id-editable="selectedId === null"
          id-input-id="ae-id"
          :saving="busy"
          :save-disabled="busy || !form.id"
          :saved-label="savedLabel"
          :error="saveError"
          :remove-label="selectedId ? 'Delete / Reset' : ''"
          @update:id="onIdInput"
          @save="save"
          @remove="removeAbility"
        />

        <GameScrollArea class="ability-editor__scroll">
          <!-- Top row: a narrow Identity beside a wide Ability Builder. -->
          <div class="ability-editor__top">
            <!-- 1. Identity -->
            <SectionCard title="Identity" :index="1">
              <EditorField label="Display Name" for-id="ae-display-name">
                <input id="ae-display-name" v-model="form.displayName" type="text" />
              </EditorField>
              <EditorField label="Type" for-id="ae-type">
                <select id="ae-type" v-model="form.type">
                  <option value="">(none)</option>
                  <option value="spell">spell</option>
                  <option value="passive">passive</option>
                </select>
              </EditorField>
              <EditorField label="Category" hint="(classification)" for-id="ae-category">
                <select id="ae-category" v-model="form.category">
                  <option value="">(none)</option>
                  <option v-for="c in abilityCategories" :key="c" :value="c">{{ c }}</option>
                </select>
              </EditorField>
              <EditorField label="Damage Type" hint="(element &amp; spell school)" for-id="ae-damage-type">
                <select id="ae-damage-type" v-model="form.damageType">
                  <option value="">(none)</option>
                  <option v-for="d in damageTypes" :key="d" :value="d">{{ d }}</option>
                </select>
              </EditorField>
              <EditorField label="Tags" hint="(comma-separated modifier keys)" for-id="ae-tags">
                <input
                  id="ae-tags"
                  :value="(form.tags ?? []).join(',')"
                  type="text"
                  @input="updateStringList('tags', ($event.target as HTMLInputElement).value)"
                />
              </EditorField>

              <div class="ability-editor__icon-field">
                <span class="ability-editor__icon-field-label">Icon</span>
                <div class="ability-editor__icon-preview-row">
                  <canvas ref="previewCanvasEl" width="64" height="64" class="ability-editor__icon-preview" />
                  <div class="ability-editor__icon-preview-actions">
                    <UiButton size="sm" variant="active" data-test="icon-gallery-open" @click="galleryOpen = true">
                      Choose from gallery
                    </UiButton>
                    <EditorField label="Upload custom icon" hint="(PNG)" for-id="ae-icon-upload">
                      <input id="ae-icon-upload" type="file" accept="image/png" @change="onIconFileChosen" />
                    </EditorField>
                    <p v-if="iconUploadError" class="ability-editor__error">{{ iconUploadError }}</p>
                  </div>
                </div>

                <div v-if="galleryOpen" class="ability-editor__icon-gallery-overlay">
                  <div class="ability-editor__icon-gallery">
                    <div class="ability-editor__icon-gallery-header">
                      <span>Choose an icon</span>
                      <UiButton size="sm" variant="secondary" @click="galleryOpen = false">Close</UiButton>
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
            </SectionCard>

            <!-- 2. Ability Builder — the art & presentation controls on the
                 left, the live preview card (like Items / Units) on the right. -->
            <SectionCard title="Ability Builder" :index="2">
              <div class="ability-editor__builder-body">
                <div class="ability-editor__preview-controls">
                  <EditorField label="Caster Animation" for-id="ae-caster-anim">
                    <input id="ae-caster-anim" v-model="form.casterAnimation" type="text" />
                  </EditorField>
                  <div class="ability-editor__pair">
                    <EditorField label="Effect On Target" for-id="ae-effect-target">
                      <select id="ae-effect-target" v-model="form.effectOnTarget">
                        <option value="">(none)</option>
                        <option v-for="e in effectIds" :key="e" :value="e">{{ e }}</option>
                      </select>
                    </EditorField>
                    <EditorField label="Effect At Point" for-id="ae-effect-point">
                      <select id="ae-effect-point" v-model="form.effectAtPoint">
                        <option value="">(none)</option>
                        <option v-for="e in effectIds" :key="e" :value="e">{{ e }}</option>
                      </select>
                    </EditorField>
                  </div>
                  <div class="ability-editor__pair">
                    <EditorField label="Burn Effect At Point" for-id="ae-burn-effect">
                      <select id="ae-burn-effect" v-model="form.burnEffectAtPoint">
                        <option value="">(none)</option>
                        <option v-for="e in effectIds" :key="e" :value="e">{{ e }}</option>
                      </select>
                    </EditorField>
                    <EditorField label="Effect Scale" for-id="ae-effect-scale">
                      <input id="ae-effect-scale" v-model.number="form.effectScale" type="number" min="0" step="0.1" />
                    </EditorField>
                  </div>
                  <EditorField label="Projectile" for-id="ae-projectile">
                    <select id="ae-projectile" v-model="form.projectile">
                      <option value="">(none)</option>
                      <option v-for="p in projectileIds" :key="p" :value="p">{{ p }}</option>
                    </select>
                  </EditorField>
                </div>

                <div class="ability-editor__builder-preview">
                  <!-- Live cast playback: adept casts this ability at a raider,
                       using the real game renderer. Projectile/effect scale is
                       ability-owned, so the viewer needs only the def. -->
                  <AbilityAnimationViewer :def="form" />
                  <AbilityPreviewCard
                    class="ability-editor__preview-card"
                    :name="form.displayName || form.id"
                    :description="effectiveDescription"
                    :lines="previewLines"
                    :type-label="typeLabel"
                    :school="form.damageType || ''"
                  />
                </div>
              </div>
            </SectionCard>
          </div>

          <div class="ability-editor__grid">
            <!-- 3. Description — generated by default, editable as an override -->
            <SectionCard title="Description" :index="3" class="ability-editor__wide">
              <template #head-action>
                <UiButton
                  size="sm"
                  variant="secondary"
                  :disabled="!isOverridden"
                  @click="resetDescriptionToGenerated"
                >Reset to generated</UiButton>
              </template>
              <EditorField
                label="Tooltip text"
                :hint="isOverridden ? '(custom override)' : '(auto-generated from the fields below)'"
                for-id="ae-description"
              >
                <textarea
                  id="ae-description"
                  v-model="descriptionDraft"
                  rows="3"
                  :placeholder="generatedDescription"
                ></textarea>
              </EditorField>
              <p class="ability-editor__desc-note">
                The tooltip is generated from how the ability is configured. Edit it to
                override that text until the generated wording is confirmed accurate;
                clearing it (or Reset) returns to the generated description.
              </p>
            </SectionCard>

            <!-- 4. Targeting -->
            <SectionCard title="Targeting" :index="4">
              <label class="ed-check" for="ae-target-self">
                <input id="ae-target-self" v-model="form.canTargetSelf" type="checkbox" /> Can target self
              </label>
              <label class="ed-check" for="ae-target-allies">
                <input id="ae-target-allies" v-model="form.canTargetAllies" type="checkbox" /> Can target allies
              </label>
              <label class="ed-check" for="ae-target-enemies">
                <input id="ae-target-enemies" v-model="form.canTargetEnemies" type="checkbox" /> Can target enemies
              </label>
              <label class="ed-check" for="ae-targets-point">
                <input id="ae-targets-point" v-model="form.targetsPoint" type="checkbox" /> Targets a ground point
              </label>
              <label class="ed-check" for="ae-match-range">
                <input id="ae-match-range" v-model="castRangeMatchesAttack" type="checkbox" /> Cast range matches attack range
              </label>
              <EditorField v-if="!castRangeMatchesAttack" label="Cast Range" hint="(world units)" for-id="ae-cast-range">
                <input
                  id="ae-cast-range"
                  type="number"
                  :value="typeof form.castRange === 'number' ? form.castRange : 0"
                  @input="form.castRange = Number(($event.target as HTMLInputElement).value) || 0"
                />
              </EditorField>
            </SectionCard>

            <!-- 5. Cost / Timing -->
            <SectionCard title="Cost / Timing" :index="5">
              <div class="ability-editor__pair">
                <EditorField label="Mana Cost" for-id="ae-mana">
                  <input id="ae-mana" v-model.number="form.manaCost" type="number" min="0" />
                </EditorField>
                <EditorField label="Cooldown (s)" for-id="ae-cooldown">
                  <input id="ae-cooldown" v-model.number="form.cooldown" type="number" min="0" />
                </EditorField>
              </div>
              <div class="ability-editor__pair">
                <EditorField label="Cast Time (s)" for-id="ae-cast-time">
                  <input id="ae-cast-time" v-model.number="form.castTime" type="number" min="0" />
                </EditorField>
                <EditorField label="Target Count" hint="(≥1)" for-id="ae-target-count">
                  <input id="ae-target-count" v-model.number="form.targetCount" type="number" min="0" />
                </EditorField>
              </div>
            </SectionCard>

            <!-- 6. Auto-cast -->
            <SectionCard title="Auto-cast" :index="6">
              <label class="ed-check" for="ae-supports-autocast">
                <input id="ae-supports-autocast" v-model="form.supportsAutoCast" type="checkbox" /> Supports auto-cast
              </label>
              <template v-if="form.supportsAutoCast">
                <EditorField label="Target Selector" for-id="ae-autocast-selector">
                  <select id="ae-autocast-selector" v-model="form.autoCastTargetSelector">
                    <option value="">(none)</option>
                    <option v-for="s in autoCastSelectors" :key="s" :value="s">{{ s }}</option>
                  </select>
                </EditorField>
                <label class="ed-check" for="ae-default-autocast">
                  <input id="ae-default-autocast" v-model="form.defaultAutoCast" type="checkbox" /> Enabled by default
                </label>
              </template>
            </SectionCard>

            <!-- 7. Effect — the primary damage / heal numbers (always shown) -->
            <SectionCard title="Damage &amp; Healing" :index="7">
              <div class="ability-editor__pair">
                <EditorField label="Damage Amount" hint="(one-shot)" for-id="ae-damage">
                  <input id="ae-damage" v-model.number="form.damageAmount" type="number" min="0" />
                </EditorField>
                <EditorField label="Heal Amount" for-id="ae-heal">
                  <input id="ae-heal" v-model.number="form.healAmount" type="number" min="0" />
                </EditorField>
              </div>
              <div class="ability-editor__pair">
                <EditorField label="Damage / second" hint="(DoT)" for-id="ae-dps">
                  <input id="ae-dps" v-model.number="form.damagePerSecond" type="number" min="0" />
                </EditorField>
                <EditorField label="" for-id="ae-minor">
                  <label class="ed-check" for="ae-minor">
                    <input id="ae-minor" v-model="form.minorDamage" type="checkbox" /> Minor damage popup
                  </label>
                </EditorField>
              </div>
            </SectionCard>

            <!-- Mechanic sections — auto-shown when the ability uses them, or
                 added on demand via "Add mechanic" below. -->

            <SectionCard v-if="isMechanicShown('area')" title="Area &amp; Burn">
              <template #head-action>
                <button type="button" class="ability-editor__mech-clear" @click="clearMechanic('area')">Remove</button>
              </template>
              <EditorField label="Radius" hint="(area of effect)" for-id="ae-radius">
                <input id="ae-radius" v-model.number="form.radius" type="number" min="0" />
              </EditorField>
              <div class="ability-editor__pair">
                <EditorField label="Impact Delay (s)" for-id="ae-impact-delay">
                  <input id="ae-impact-delay" v-model.number="form.impactDelaySeconds" type="number" min="0" step="0.1" />
                </EditorField>
                <EditorField label="Burn Radius" for-id="ae-burn-radius">
                  <input id="ae-burn-radius" v-model.number="form.burnRadius" type="number" min="0" />
                </EditorField>
              </div>
              <div class="ability-editor__pair">
                <EditorField label="Burn Duration (s)" for-id="ae-burn-duration">
                  <input id="ae-burn-duration" v-model.number="form.burnDurationSeconds" type="number" min="0" step="0.1" />
                </EditorField>
                <EditorField label="Burn Damage / tick" for-id="ae-burn-dmg">
                  <input id="ae-burn-dmg" v-model.number="form.burnDamagePerTick" type="number" min="0" />
                </EditorField>
              </div>
              <EditorField label="Burn Tick Interval (s)" for-id="ae-burn-tick">
                <input id="ae-burn-tick" v-model.number="form.burnTickIntervalSeconds" type="number" min="0" step="0.1" />
              </EditorField>
            </SectionCard>

            <SectionCard v-if="isMechanicShown('chain')" title="Chain / Bounce">
              <template #head-action>
                <button type="button" class="ability-editor__mech-clear" @click="clearMechanic('chain')">Remove</button>
              </template>
              <div class="ability-editor__pair">
                <EditorField label="Chain Count" for-id="ae-chain-count">
                  <input id="ae-chain-count" v-model.number="form.chainCount" type="number" min="0" />
                </EditorField>
                <EditorField label="Bounce Range" for-id="ae-bounce-range">
                  <input id="ae-bounce-range" v-model.number="form.bounceRange" type="number" min="0" />
                </EditorField>
              </div>
              <EditorField label="Bounce Damage Falloff" hint="(per bounce)" for-id="ae-bounce-falloff">
                <input id="ae-bounce-falloff" v-model.number="form.bounceDamageFalloff" type="number" min="0" />
              </EditorField>
            </SectionCard>

            <SectionCard v-if="isMechanicShown('cc')" title="Crowd Control">
              <template #head-action>
                <button type="button" class="ability-editor__mech-clear" @click="clearMechanic('cc')">Remove</button>
              </template>
              <div class="ability-editor__pair">
                <EditorField label="Slow Multiplier" hint="(0–1; 0.5 = −50% speed)" for-id="ae-slow-mult">
                  <input id="ae-slow-mult" v-model.number="form.slowMultiplier" type="number" min="0" max="1" step="0.05" />
                </EditorField>
                <EditorField label="Slow Duration (s)" for-id="ae-slow-dur">
                  <input id="ae-slow-dur" v-model.number="form.slowDurationSeconds" type="number" min="0" step="0.1" />
                </EditorField>
              </div>
              <EditorField label="Pull Strength" hint="(toward cast center)" for-id="ae-pull">
                <input id="ae-pull" v-model.number="form.pullStrength" type="number" min="0" />
              </EditorField>
            </SectionCard>

            <SectionCard v-if="isMechanicShown('motion')" title="Projectile Motion">
              <template #head-action>
                <button type="button" class="ability-editor__mech-clear" @click="clearMechanic('motion')">Remove</button>
              </template>
              <div class="ability-editor__pair">
                <EditorField label="Projectile Speed" for-id="ae-proj-speed">
                  <input id="ae-proj-speed" v-model.number="form.projectileSpeed" type="number" min="0" />
                </EditorField>
                <EditorField label="Projectile Scale" for-id="ae-proj-scale">
                  <input id="ae-proj-scale" v-model.number="form.projectileScale" type="number" min="0" step="0.1" />
                </EditorField>
              </div>
              <EditorField label="Duration (s)" hint="(effect lifetime)" for-id="ae-duration">
                <input id="ae-duration" v-model.number="form.duration" type="number" min="0" step="0.1" />
              </EditorField>
            </SectionCard>

            <SectionCard v-if="isMechanicShown('summon')" title="Summon">
              <template #head-action>
                <button type="button" class="ability-editor__mech-clear" @click="clearMechanic('summon')">Remove</button>
              </template>
              <EditorField label="Summon Unit Type" for-id="ae-summon-type">
                <select id="ae-summon-type" v-model="form.summonUnitType">
                  <option value="">(none)</option>
                  <option v-for="u in unitTypeIds" :key="u" :value="u">{{ u }}</option>
                </select>
              </EditorField>
              <EditorField label="Summon Count" for-id="ae-summon-count">
                <input id="ae-summon-count" v-model.number="form.summonCount" type="number" min="0" />
              </EditorField>
            </SectionCard>

            <SectionCard v-if="isMechanicShown('channel')" title="Channeled Beam">
              <template #head-action>
                <button type="button" class="ability-editor__mech-clear" @click="clearMechanic('channel')">Remove</button>
              </template>
              <EditorField label="Channel Type" hint="(e.g. beam)" for-id="ae-channel-type">
                <input id="ae-channel-type" v-model="form.channelType" type="text" />
              </EditorField>
              <div class="ability-editor__pair">
                <EditorField label="Tick Interval (s)" for-id="ae-tick-interval">
                  <input id="ae-tick-interval" v-model.number="form.tickIntervalSeconds" type="number" min="0" step="0.05" />
                </EditorField>
                <EditorField label="Mana Cost / tick" for-id="ae-mana-tick">
                  <input id="ae-mana-tick" v-model.number="form.manaCostPerTick" type="number" min="0" />
                </EditorField>
              </div>
              <div class="ability-editor__pair">
                <EditorField label="Damage / tick" for-id="ae-dmg-tick">
                  <input id="ae-dmg-tick" v-model.number="form.damagePerTick" type="number" min="0" />
                </EditorField>
                <EditorField label="Healing Multiplier" hint="(of damage drained)" for-id="ae-heal-mult">
                  <input id="ae-heal-mult" v-model.number="form.healingMultiplier" type="number" min="0" step="0.1" />
                </EditorField>
              </div>
              <EditorField label="Ally Heal Radius" for-id="ae-ally-heal-radius">
                <input id="ae-ally-heal-radius" v-model.number="form.allyHealRadius" type="number" min="0" />
              </EditorField>
            </SectionCard>

            <SectionCard v-if="isMechanicShown('charge')" title="Charge-Fire Passive">
              <template #head-action>
                <button type="button" class="ability-editor__mech-clear" @click="clearMechanic('charge')">Remove</button>
              </template>
              <div class="ability-editor__pair">
                <EditorField label="Charge Required" for-id="ae-charge-req">
                  <input id="ae-charge-req" v-model.number="form.chargeRequired" type="number" min="0" />
                </EditorField>
                <EditorField label="Mana → Charge Ratio" for-id="ae-charge-ratio">
                  <input id="ae-charge-ratio" v-model.number="form.manaToChargeRatio" type="number" min="0" step="0.1" />
                </EditorField>
              </div>
              <div class="ability-editor__pair">
                <EditorField label="Missile Count" for-id="ae-missile-count">
                  <input id="ae-missile-count" v-model.number="form.missileCount" type="number" min="0" />
                </EditorField>
                <EditorField label="Damage / Missile" for-id="ae-missile-dmg">
                  <input id="ae-missile-dmg" v-model.number="form.damagePerMissile" type="number" min="0" />
                </EditorField>
              </div>
              <div class="ability-editor__pair">
                <EditorField label="Targeting" hint="(selector)" for-id="ae-charge-targeting">
                  <input id="ae-charge-targeting" v-model="form.targeting" type="text" />
                </EditorField>
                <EditorField label="Missile Delay (ms)" for-id="ae-missile-delay">
                  <input id="ae-missile-delay" v-model.number="form.missileDelayMs" type="number" min="0" />
                </EditorField>
              </div>
              <label class="ed-check" for="ae-allow-dup">
                <input id="ae-allow-dup" v-model="form.allowDuplicateTargets" type="checkbox" /> Allow duplicate targets
              </label>
            </SectionCard>

            <!-- Add mechanic — reveals an empty section for a mechanic the
                 ability doesn't use yet. -->
            <SectionCard v-if="addableMechanics.length" title="Add mechanic">
              <div class="ability-editor__add-mechanics">
                <UiButton
                  v-for="m in addableMechanics"
                  :key="m.key"
                  size="sm"
                  variant="secondary"
                  @click="revealMechanic(m.key)"
                >+ {{ m.label }}</UiButton>
              </div>
            </SectionCard>

            <!-- Validation — a summary card at the bottom, spanning full width. -->
            <SectionCard title="Validation" class="ability-editor__wide">
              <ValidationChecklist :checks="checks" />
            </SectionCard>
          </div>
        </GameScrollArea>
      </template>
    </template>
  </EditorShell>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import {
  createBlankForm, formFromDef, saveRequestFromForm,
  type AbilityEditorForm, type AuthoredAbilityDef,
} from '@/game/abilities/abilityEditorForm'
import {
  fetchAuthoredAbilityDefs, fetchProjectileIds, fetchEffectIds, fetchAutoCastSelectors,
  fetchAbilityCategories, fetchDamageTypes, saveEditorAbility, deleteEditorAbility,
  uploadAbilityIcon, EditorValidationError,
} from '@/game/abilities/abilityEditorApi'
import { fetchAuthoredUnitDefs } from '@/game/units/unitEditorApi'
import { getAbilityIconSourceUrl, getAbilityPreviewUrl, listAbilityIconKeys } from '@/game/rendering/abilityAssets'
import { inferProjectileFrameCount } from '@/game/rendering/projectileSprites'
import EditorShell from '@/components/editor/EditorShell.vue'
import EditorSidebar from '@/components/editor/EditorSidebar.vue'
import type { SidebarGroup } from '@/components/editor/EditorSidebar.vue'
import EditorHeader from '@/components/editor/EditorHeader.vue'
import EditorField from '@/components/editor/EditorField.vue'
import SectionCard from '@/components/editor/SectionCard.vue'
import ValidationChecklist from '@/components/editor/ValidationChecklist.vue'
import type { ValidationCheck } from '@/components/editor/ValidationChecklist.vue'
import GameScrollArea from '@/components/ui/GameScrollArea.vue'
import UiButton from '@/components/ui/UiButton.vue'
import AbilityPreviewCard from '@/components/AbilityPreviewCard.vue'
import AbilityAnimationViewer from '@/components/AbilityAnimationViewer.vue'

const abilities = ref<AuthoredAbilityDef[]>([])
const form = ref<AbilityEditorForm>(createBlankForm())
const selectedId = ref<string | null>(null)
// editing gates the whole form/rail: false = the empty prompt (default and
// after a delete), matching how the Units/Items editors open on an empty screen.
const editing = ref(false)
const search = ref('')
const saveError = ref('')
const loadError = ref('')
const savedLabel = ref('')
const busy = ref(false)

const projectileIds = ref<string[]>([])
const effectIds = ref<string[]>([])
const autoCastSelectors = ref<string[]>([])
const abilityCategories = ref<string[]>([])
const damageTypes = ref<string[]>([])
const unitTypeIds = ref<string[]>([])

// ── Description: generated by default, editable as an override ───────────────
// descriptionDraft is the live textarea value. It is seeded from the def's
// override (or the server-generated text) on select and reconciled back into
// form.description on save: unchanged-from-generated saves an empty override
// (stays dynamic), edited text saves as the override.
const descriptionDraft = ref('')
const generatedDescription = computed(() => form.value.generatedDescription ?? '')
const effectiveDescription = computed(() =>
  descriptionDraft.value.trim() ? descriptionDraft.value : generatedDescription.value,
)
const isOverridden = computed(() => {
  const d = descriptionDraft.value.trim()
  return !!d && d !== generatedDescription.value.trim()
})
function resetDescriptionToGenerated() {
  descriptionDraft.value = generatedDescription.value
}
function applyDescriptionOverride() {
  form.value.description = isOverridden.value ? descriptionDraft.value : ''
}

const typeLabel = computed(() => {
  if (form.value.type === 'spell') return 'Spell'
  if (form.value.type === 'passive') return 'Passive'
  return ''
})

const headerBreadcrumb = computed(() =>
  [typeLabel.value, form.value.category].filter(Boolean).join(' • '),
)

const filePath = computed(() =>
  form.value.id ? `server/internal/game/catalog/abilities/${form.value.id}/${form.value.id}.json` : '',
)

// ── Sidebar grouping (by damage school) ─────────────────────────────────────
const sidebarGroups = computed<SidebarGroup[]>(() => {
  const q = search.value.trim().toLowerCase()
  const matches = abilities.value.filter((a) => {
    if (!q) return true
    return a.id.toLowerCase().includes(q) || (a.displayName ?? '').toLowerCase().includes(q)
  })
  const bySchool = new Map<string, AuthoredAbilityDef[]>()
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

// ── Stat readouts for the preview card (plain field values, not prose) ───────
const previewLines = computed(() => {
  const f = form.value
  const lines: string[] = []
  if ((f.manaCost ?? 0) > 0) lines.push(`Mana: ${f.manaCost}`)
  if ((f.cooldown ?? 0) > 0) lines.push(`Cooldown: ${f.cooldown}s`)
  if ((f.castTime ?? 0) > 0) lines.push(`Cast time: ${f.castTime}s`)
  if (f.castRange === 'match_attack_range') lines.push('Range: matches attack range')
  else if (typeof f.castRange === 'number' && f.castRange > 0) lines.push(`Range: ${f.castRange}`)
  if ((f.targetCount ?? 0) > 1) lines.push(`Targets: ${f.targetCount}`)
  return lines
})

// ── Validation ──────────────────────────────────────────────────────────────
const checks = computed<ValidationCheck[]>(() => {
  const f = form.value
  const hasEffect =
    (f.damageAmount ?? 0) > 0 || (f.healAmount ?? 0) > 0 || (f.damagePerSecond ?? 0) > 0 ||
    !!f.summonUnitType || !!f.channelType || (f.chargeRequired ?? 0) > 0
  return [
    { ok: !!f.id, message: 'Has an id' },
    { ok: !!f.displayName, message: 'Has a display name' },
    { ok: hasEffect, message: 'Has an effect (damage, heal, summon, or channel)' },
  ]
})

// ── Mechanic sections: auto-shown by relevance, revealable on demand ─────────
type MechanicKey = 'area' | 'chain' | 'cc' | 'motion' | 'summon' | 'channel' | 'charge'
const MECHANICS: { key: MechanicKey; label: string; fields: (keyof AuthoredAbilityDef)[] }[] = [
  { key: 'area', label: 'Area & Burn', fields: ['radius', 'impactDelaySeconds', 'burnDurationSeconds', 'burnDamagePerTick', 'burnTickIntervalSeconds', 'burnRadius'] },
  { key: 'chain', label: 'Chain / Bounce', fields: ['chainCount', 'bounceRange', 'bounceDamageFalloff'] },
  { key: 'cc', label: 'Crowd Control', fields: ['slowMultiplier', 'slowDurationSeconds', 'pullStrength'] },
  { key: 'motion', label: 'Projectile Motion', fields: ['projectileSpeed', 'projectileScale', 'duration'] },
  { key: 'summon', label: 'Summon', fields: ['summonUnitType', 'summonCount'] },
  { key: 'channel', label: 'Channeled Beam', fields: ['channelType', 'tickIntervalSeconds', 'manaCostPerTick', 'damagePerTick', 'healingMultiplier', 'allyHealRadius'] },
  { key: 'charge', label: 'Charge-Fire Passive', fields: ['chargeRequired', 'manaToChargeRatio', 'missileCount', 'damagePerMissile', 'targeting', 'allowDuplicateTargets', 'missileDelayMs'] },
]
// Mechanics the author explicitly revealed for this ability even though they
// carry no data yet. Cleared on every select/new so a def opens showing only
// the mechanics it actually uses.
const revealed = ref<Set<MechanicKey>>(new Set())

function mechanicHasData(key: MechanicKey): boolean {
  const mech = MECHANICS.find((m) => m.key === key)
  if (!mech) return false
  return mech.fields.some((fld) => {
    const v = form.value[fld]
    if (typeof v === 'number') return v > 0
    if (typeof v === 'boolean') return v
    return !!v
  })
}
function isMechanicShown(key: MechanicKey): boolean {
  return mechanicHasData(key) || revealed.value.has(key)
}
const addableMechanics = computed(() => MECHANICS.filter((m) => !isMechanicShown(m.key)))
function revealMechanic(key: MechanicKey) {
  revealed.value = new Set(revealed.value).add(key)
}
function clearMechanic(key: MechanicKey) {
  const mech = MECHANICS.find((m) => m.key === key)
  if (mech) for (const fld of mech.fields) form.value[fld] = undefined
  const next = new Set(revealed.value)
  next.delete(key)
  revealed.value = next
}

// ── Icon preview + gallery (unchanged behavior; canvas draws the first frame
// of a possibly-multi-frame sprite sheet, so it can't use a plain <img>). ─────
const galleryOpen = ref(false)
const iconUploadError = ref('')
const previewCanvasEl = ref<HTMLCanvasElement | null>(null)
const galleryKeys = listAbilityIconKeys()
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

// flush: 'post' so the canvas is mounted (the form is gated behind `editing`)
// before we draw into it on the first selection.
watch(previewIconUrl, (url) => drawIconFirstFrame(previewCanvasEl.value, url), { flush: 'post' })

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
  if (selectedId.value === null || !form.value.id) {
    iconUploadError.value = 'Save the ability before uploading an icon.'
    input.value = ''
    return
  }
  try {
    await uploadAbilityIcon(form.value.id, file)
    form.value.icon = form.value.id
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

// ── Data + selection lifecycle ──────────────────────────────────────────────
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

function openForm(def: AuthoredAbilityDef | null, asExisting: boolean) {
  form.value = def ? formFromDef(def) : createBlankForm()
  selectedId.value = asExisting && def ? def.id : null
  editing.value = true
  descriptionDraft.value = form.value.description?.trim()
    ? (form.value.description as string)
    : (form.value.generatedDescription ?? '')
  revealed.value = new Set()
  saveError.value = ''
  savedLabel.value = ''
  iconUploadError.value = ''
  galleryOpen.value = false
}

function selectAbility(id: string) {
  const def = abilities.value.find((a) => a.id === id)
  if (def) openForm(def, true)
}

function newAbility() {
  openForm(null, false)
}

function duplicateAbility(id: string) {
  const def = abilities.value.find((a) => a.id === id)
  if (!def) return
  openForm(def, false)
  form.value.id = ''
  if (form.value.displayName) form.value.displayName = `${form.value.displayName} Copy`
  // A duplicate is a brand-new def — drop the source's generated text so its
  // description starts from the copy's own fields once saved.
  form.value.generatedDescription = ''
}

function onIdInput(raw: string) {
  form.value.id = raw.toLowerCase().replace(/[^a-z0-9_]/g, '')
}

async function save() {
  applyDescriptionOverride()
  saveError.value = ''
  savedLabel.value = ''
  busy.value = true
  try {
    await saveEditorAbility(saveRequestFromForm(form.value))
    const savedId = form.value.id
    await reload()
    // Reselect so generatedDescription / override reconcile from the server.
    const saved = abilities.value.find((a) => a.id === savedId)
    if (saved) openForm(saved, true)
    else selectedId.value = savedId
    savedLabel.value = 'just now'
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
  busy.value = true
  try {
    await deleteEditorAbility(selectedId.value)
    await reload()
    editing.value = false
    selectedId.value = null
    savedLabel.value = ''
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
.ability-editor {
  width: 100%;
  height: 100%;
}

.ability-sidebar {
  display: flex;
  flex-direction: column;
  gap: 8px;
  height: 100%;
  min-height: 0;
}

.ability-sidebar__load-error {
  padding: 0 4px;
}

.ability-editor__empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: var(--ed-text-dim);
  font-size: 0.9rem;
}

.ability-editor__scroll {
  flex: 1 1 auto;
  min-height: 0;
}

.ability-editor__grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  align-content: start;
}

/* A section that should span both grid columns (description). */
.ability-editor__wide {
  grid-column: 1 / -1;
}

.ability-editor__pair {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}

/* ── Top row: a narrow Identity beside a wide Ability Builder ────────────── */
.ability-editor__top {
  display: grid;
  grid-template-columns: minmax(0, 320px) minmax(0, 1fr);
  gap: 12px;
  margin-bottom: 12px;
  align-items: start;
}

@media (max-width: 720px) {
  .ability-editor__top {
    grid-template-columns: minmax(0, 1fr);
  }
}

/* ── Ability Builder card: controls on the left; the live cast viewer and the
   text preview card on the right (the right side gets the room). ─────────── */
.ability-editor__builder-body {
  display: grid;
  grid-template-columns: minmax(0, 340px) minmax(0, 1fr);
  gap: 14px;
  align-items: start;
}

@media (max-width: 900px) {
  .ability-editor__builder-body {
    grid-template-columns: minmax(0, 1fr);
  }
}

.ability-editor__builder-preview {
  display: grid;
  gap: 12px;
  min-width: 0;
}

.ability-editor__preview-card {
  min-width: 0;
}

.ability-editor__preview-controls {
  display: grid;
  gap: 8px;
}

.ability-editor__desc-note {
  margin: 4px 0 0;
  font-size: 0.72rem;
  line-height: 1.4;
  color: var(--ed-text-dim);
}

.ability-editor__add-mechanics {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.ability-editor__mech-clear {
  padding: 2px 8px;
  font-size: 0.72rem;
  color: var(--ed-text-dim);
  background: none;
  border: 1px solid var(--ed-line);
  border-radius: 4px;
}

.ability-editor__mech-clear:hover {
  color: var(--ed-brass);
  border-color: var(--ed-brass);
}

.ability-editor__error {
  color: #fca5a5;
  font-size: 0.78rem;
}

/* ── Icon field (canvas preview + gallery overlay) ───────────────────────── */
.ability-editor__icon-field {
  display: grid;
  gap: 4px;
}

.ability-editor__icon-field-label {
  color: var(--ed-text-dim);
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
  border: 1px solid var(--ed-line);
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.4);
}

.ability-editor__icon-preview-actions {
  display: flex;
  flex-direction: column;
  gap: 8px;
  flex: 1;
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
  border: 1px solid var(--ed-line);
  border-radius: 12px;
  padding: 12px;
}

.ability-editor__icon-gallery-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
  color: var(--ed-text);
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
  border: 1px solid var(--ed-line);
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.4);
  padding: 6px;
}

.ability-editor__icon-gallery-item canvas {
  width: 40px;
  height: 40px;
  image-rendering: pixelated;
}

.ability-editor__icon-gallery-item span {
  font-size: 0.62rem;
  color: var(--ed-text-dim);
  text-align: center;
  word-break: break-all;
}

.ability-editor__icon-gallery-empty {
  color: var(--ed-text-dim);
  font-size: 0.8rem;
  text-align: center;
  padding: 24px 0;
}
</style>
