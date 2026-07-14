<template>
  <div class="unit-editor">
    <header class="unit-editor__topbar">
      <div class="unit-editor__pills">
        <button
          type="button"
          class="unit-editor__pill"
          :class="{ 'is-active': factionFilter === '' }"
          @click="factionFilter = ''"
        >
          All <span class="unit-editor__pill-count">{{ units.length }}</span>
        </button>
        <button
          v-for="f in factions"
          :key="f.id"
          type="button"
          class="unit-editor__pill"
          :class="{ 'is-active': factionFilter === f.id }"
          @click="factionFilter = f.id"
        >
          {{ f.displayName }} <span class="unit-editor__pill-count">{{ unitCountByFaction[f.id] ?? 0 }}</span>
        </button>

        <span class="unit-editor__pill-spacer"></span>

        <button
          type="button"
          class="unit-editor__pill unit-editor__pill--add"
          :disabled="busy"
          @click="showNewFaction = !showNewFaction"
        >
          + Faction
        </button>
        <button
          v-if="factionFilter"
          type="button"
          class="unit-editor__pill unit-editor__pill--danger"
          :disabled="busy"
          @click="removeFaction(factionFilter)"
        >
          Delete Faction
        </button>
      </div>

      <div v-if="showNewFaction" class="unit-editor__new-faction">
        <input v-model="newFactionId" placeholder="id (a-z0-9_)" />
        <input v-model="newFactionName" placeholder="Display Name" />
        <button type="button" :disabled="busy || !newFactionId.trim()" @click="createFaction">Create</button>
        <button type="button" @click="showNewFaction = false">Cancel</button>
      </div>
      <p v-if="factionError" class="unit-editor__error">{{ factionError }}</p>
    </header>

    <div class="unit-editor__body">
      <aside class="unit-editor__list">
        <div class="unit-editor__new-menu">
          <button
            type="button"
            class="unit-editor__new"
            :disabled="busy"
            @click="showNewMenu = !showNewMenu"
          >
            + New ▾
          </button>
          <div v-if="showNewMenu" class="unit-editor__new-menu-popover">
            <button type="button" @click="chooseNewBaseUnit">New Base Unit</button>
            <button type="button" @click="chooseNewPath">New Path</button>
          </div>
        </div>

        <div v-if="showNewPathPicker" class="unit-editor__new-path-picker">
          <select v-model="newPathParentUnit" class="unit-editor__new-path-parent">
            <option value="" disabled>— select parent unit —</option>
            <option v-for="u in visibleUnits" :key="u.type" :value="u.type">{{ u.name || u.type }}</option>
          </select>
          <button type="button" :disabled="!newPathParentUnit" @click="confirmNewPath">Create</button>
          <button type="button" @click="showNewPathPicker = false">Cancel</button>
        </div>

        <p v-if="loadError" class="unit-editor__error">{{ loadError }}</p>
        <ul class="unit-editor__tree">
          <li v-for="u in visibleUnits" :key="u.type" class="unit-editor__tree-node">
            <div class="unit-editor__tree-row">
              <button
                v-if="(pathsByUnit[u.type]?.length ?? 0) > 0"
                type="button"
                class="unit-editor__tree-toggle"
                @click="toggleUnitExpanded(u.type)"
              >
                {{ expandedUnits.has(u.type) ? '▾' : '▸' }}
              </button>
              <span v-else class="unit-editor__tree-toggle-spacer" aria-hidden="true"></span>
              <button
                type="button"
                class="unit-editor__tree-unit-btn"
                :class="{ 'is-selected': editorMode === 'unit' && u.type === selectedType }"
                @click="selectUnit(u)"
              >
                {{ u.name || u.type }}
              </button>
            </div>
            <ul v-if="expandedUnits.has(u.type)" class="unit-editor__tree-children">
              <li v-for="entry in pathsByUnit[u.type] ?? []" :key="entry.path">
                <button
                  type="button"
                  class="unit-editor__tree-path-btn"
                  :class="{
                    'is-selected':
                      editorMode === 'path' && selectedPath === entry.path && selectedPathParent === entry.unit,
                  }"
                  @click="selectPath(entry)"
                >
                  {{ entry.path }}
                </button>
              </li>
            </ul>
          </li>
        </ul>
      </aside>

      <section v-if="editorMode === 'unit'" class="unit-editor__form">
      <!-- Preview -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('preview') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('preview')">Preview</button>
        <div v-if="openSections.has('preview')" class="unit-editor__section-body">
          <UnitSpritePreview
            ref="preview"
            :unit-key="form.type"
            :projectile="form.projectile"
            :projectile-scale="form.projectileScale"
            v-model:attack-origin="form.attackOrigin"
          />

          <div
            class="unit-editor__art-ingest"
            @dragover.prevent
            @drop.prevent="onFolderDropped"
          >
            <label v-if="form.type && form.faction" class="unit-editor__art-drop">
              <input
                type="file"
                webkitdirectory
                multiple
                :disabled="ingesting"
                @change="onFolderInputChanged(($event.target as HTMLInputElement).files)"
              />
              {{ ingesting ? 'Packing…' : 'Drop / choose a PixelLab export folder' }}
            </label>
            <p v-else class="unit-editor__hint">Set the unit's type and faction to ingest art.</p>

            <div v-if="pendingArt" class="unit-editor__art-actions">
              <button type="button" :disabled="busy" @click="saveArt">Save Art</button>
              <button type="button" :disabled="busy" @click="discardPendingArt">Discard</button>
            </div>

            <p v-for="w in ingestWarnings" :key="w" class="unit-editor__warning">{{ w }}</p>
            <p v-if="ingestError" class="unit-editor__error">{{ ingestError }}</p>
          </div>
        </div>
      </section>

      <!-- Identity -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('identity') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('identity')">Identity</button>
        <div v-if="openSections.has('identity')" class="unit-editor__section-body">
          <label>Name <input v-model="form.name" /></label>
          <label>
            ID
            <span class="unit-editor__hint">
              {{ selectedType === null ? 'auto from Name · a–z 0–9 _ · edit to override' : "an existing unit's id can't change" }}
            </span>
            <input
              :value="form.type"
              :disabled="selectedType !== null"
              placeholder="e.g. raider_brute"
              @input="onTypeIdInput(($event.target as HTMLInputElement).value)"
            />
          </label>
          <label>
            Faction
            <select v-model="form.faction">
              <option value="" disabled>— select a faction —</option>
              <option v-for="f in factions" :key="f.id" :value="f.id">{{ f.displayName }}</option>
            </select>
          </label>
          <label>
            Archetype
            <input v-model="form.archetype" list="unit-editor-archetypes" placeholder="(defaults to the unit type)" />
          </label>
          <p v-if="archetypeWarning" class="unit-editor__warning">{{ archetypeWarning }}</p>
          <label>Train Label <input v-model="form.trainLabel" /></label>
        </div>
      </section>

      <!-- Stats -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('stats') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('stats')">Stats</button>
        <div v-if="openSections.has('stats')" class="unit-editor__section-body">
          <label>HP <input type="number" v-model.number="form.hp" /></label>
          <!-- Blank and 0 mean DIFFERENT things here (blank = inherit the
               server default, 0 = never regenerates), so this cannot use
               v-model.number — it would coerce a cleared field to 0 and
               silently turn off regeneration. -->
          <label>
            HP Regen Rate <span class="unit-editor__hint">(per second; blank = server default)</span>
            <input
              type="number"
              step="0.1"
              :value="form.healthRegenRate ?? ''"
              @input="form.healthRegenRate = ($event.target as HTMLInputElement).value === '' ? undefined : Number(($event.target as HTMLInputElement).value)"
            />
          </label>
          <label>Mana <input type="number" v-model.number="form.maxMana" /></label>
          <label>Mana Regen Rate <input type="number" step="0.1" v-model.number="form.manaRegenRate" /></label>
          <label>Armor <input type="number" v-model.number="form.armor" /></label>
          <label>Damage <input type="number" v-model.number="form.damage" /></label>
          <label>Attack Range <input type="number" v-model.number="form.attackRange" /></label>
          <label>Attack Speed <input type="number" v-model.number="form.attackSpeed" /></label>
          <label>Splash Radius <input type="number" v-model.number="form.splashRadius" /></label>
          <label>Move Speed <input type="number" v-model.number="form.moveSpeed" /></label>
          <label>Vision Range <input type="number" v-model.number="form.visionRange" /></label>
        </div>
      </section>

      <!-- Cost -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('cost') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('cost')">Cost</button>
        <div v-if="openSections.has('cost')" class="unit-editor__section-body">
          <div class="unit-editor__map">
            <span class="unit-editor__map-label">Resource Cost</span>
            <div v-for="(row, idx) in resourceCostRows" :key="idx" class="unit-editor__map-row">
              <input v-model="row.key" placeholder="resource key" />
              <input type="number" v-model.number="row.value" />
              <button type="button" @click="removeResourceCostRow(idx)">Remove</button>
            </div>
            <button type="button" @click="addResourceCostRow">+ Add resource cost</button>
          </div>
          <label>Meat Cost <input type="number" v-model.number="form.meatCost" /></label>
          <label>Spawn Seconds <input type="number" v-model.number="form.spawnSeconds" /></label>
        </div>
      </section>

      <!-- Combat -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('combat') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('combat')">Combat</button>
        <div v-if="openSections.has('combat')" class="unit-editor__section-body">
          <label>
            Combat Profile
            <input v-model="form.combatProfile" list="unit-editor-archetypes" placeholder="(inferred from archetype)" />
          </label>
          <label>Attack Type <input v-model="form.attackType" /></label>
          <label>
            Damage Type
            <input v-model="form.damageType" list="unit-editor-damage-types" placeholder="(unspecified = physical)" />
            <datalist id="unit-editor-damage-types">
              <option v-for="d in damageTypes" :key="d" :value="d" />
            </datalist>
          </label>
          <label>
            Targetable Types (comma-separated)
            <input
              :value="(form.targetableTypes ?? []).join(',')"
              @input="updateStringList('targetableTypes', ($event.target as HTMLInputElement).value)"
            />
          </label>
          <label>
            Projectile
            <input v-model="form.projectile" list="unit-editor-projectiles" />
            <datalist id="unit-editor-projectiles">
              <option v-for="p in projectileIds" :key="p" :value="p" />
            </datalist>
          </label>
          <label>Projectile Scale <input type="number" v-model.number="form.projectileScale" /></label>
        </div>
      </section>

      <!-- Resources -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('resources') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('resources')">Resources</button>
        <div v-if="openSections.has('resources')" class="unit-editor__section-body">
          <label>Gold Gather Amount <input type="number" v-model.number="form.goldGatherAmount" /></label>
          <label>Wood Gather Amount <input type="number" v-model.number="form.woodGatherAmount" /></label>
        </div>
      </section>

      <!-- Abilities -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('abilities') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('abilities')">Abilities</button>
        <div v-if="openSections.has('abilities')" class="unit-editor__section-body">
          <label>
            Abilities (comma-separated)
            <input
              :value="(form.abilities ?? []).join(',')"
              @input="updateStringList('abilities', ($event.target as HTMLInputElement).value)"
            />
            <span class="unit-editor__hint">Known: {{ abilityIds.join(', ') || '—' }}</span>
          </label>
          <label>
            Capabilities (comma-separated)
            <input
              :value="(form.capabilities ?? []).join(',')"
              @input="updateStringList('capabilities', ($event.target as HTMLInputElement).value)"
            />
          </label>
          <!-- Not a stat: start/end frame indices of the casting animation to
               loop while a channelled ability is being cast. Lives with
               Abilities because that is the only thing it affects. -->
          <div class="unit-editor__channel-loop">
            <span class="unit-editor__map-label">Channel Loop <span class="unit-editor__hint">(casting-animation frames to loop; leave both blank to unset)</span></span>
            <label>
              Start
              <input
                type="number"
                :value="channelLoopStart ?? ''"
                @input="channelLoopStart = ($event.target as HTMLInputElement).value === '' ? undefined : Number(($event.target as HTMLInputElement).value)"
              />
            </label>
            <label>
              End
              <input
                type="number"
                :value="channelLoopEnd ?? ''"
                @input="channelLoopEnd = ($event.target as HTMLInputElement).value === '' ? undefined : Number(($event.target as HTMLInputElement).value)"
              />
            </label>
          </div>
        </div>
      </section>

      <!-- Gating -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('gating') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('gating')">Gating</button>
        <div v-if="openSections.has('gating')" class="unit-editor__section-body">
          <label>
            Requires Buildings (comma-separated)
            <input
              :value="(form.requiresBuildings ?? []).join(',')"
              @input="updateStringList('requiresBuildings', ($event.target as HTMLInputElement).value)"
            />
            <span class="unit-editor__hint">Known: {{ buildingIds.join(', ') || '—' }}</span>
          </label>
          <div class="unit-editor__map">
            <span class="unit-editor__map-label">Path Chances</span>
            <div v-for="(row, idx) in pathChancesRows" :key="idx" class="unit-editor__map-row">
              <input v-model="row.key" placeholder="path key" />
              <input type="number" v-model.number="row.value" />
              <button type="button" @click="removePathChanceRow(idx)">Remove</button>
            </div>
            <button type="button" @click="addPathChanceRow">+ Add path chance</button>
          </div>
        </div>
      </section>

      <!-- Rewards -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('rewards') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('rewards')">Rewards</button>
        <div v-if="openSections.has('rewards')" class="unit-editor__section-body">
          <label>Dominion Point Drop Chance <input type="number" v-model.number="form.dominionPointDropChance" /></label>
          <label>Dominion Point Amount <input type="number" v-model.number="form.dominionPointAmount" /></label>
          <label>Spawn Exp <input type="number" v-model.number="form.spawnExp" /></label>
          <label>Experience <input type="number" v-model.number="form.experience" /></label>
        </div>
      </section>

      <!-- Flags -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('flags') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('flags')">Flags</button>
        <div v-if="openSections.has('flags')" class="unit-editor__section-body">
          <label class="unit-editor__checkbox-label">
            <input type="checkbox" v-model="form.flyer" /> Flyer
          </label>
          <label class="unit-editor__checkbox-label">
            <input type="checkbox" v-model="form.nonCombat" /> Non-combat
          </label>
        </div>
      </section>

        <p v-if="saveError" class="unit-editor__error">{{ saveError }}</p>
        <div class="unit-editor__actions">
          <button type="button" :disabled="busy || !form.type || !form.faction" @click="save">Save</button>
          <button type="button" :disabled="busy || selectedType === null" @click="removeUnit">Delete</button>
        </div>
      </section>

      <!-- Path editor: mirrors the unit form's accordion pattern. -->
      <section v-else class="unit-editor__form">
        <template v-if="pathForm">
          <!-- Preview (mirrors the unit form's Preview section: same name, top of the form) -->
          <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('path-preview') }">
            <button type="button" class="unit-editor__section-summary" @click="toggleSection('path-preview')">Preview</button>
            <div v-if="openSections.has('path-preview')" class="unit-editor__section-body">
              <UnitSpritePreview
                ref="preview"
                :path-key="pathForm.path"
                :unit-key="pathForm.parentUnit"
                :projectile="pathForm.projectile"
                :projectile-scale="pathForm.projectileScale"
                v-model:attack-origin="pathForm.attackOrigin"
              />

              <div
                class="unit-editor__art-ingest"
                @dragover.prevent
                @drop.prevent="onFolderDropped"
              >
                <label v-if="canIngestArt" class="unit-editor__art-drop">
                  <input
                    type="file"
                    webkitdirectory
                    multiple
                    :disabled="ingesting"
                    @change="onFolderInputChanged(($event.target as HTMLInputElement).files)"
                  />
                  {{ ingesting ? 'Packing…' : 'Drop / choose a PixelLab export folder' }}
                </label>
                <p v-else class="unit-editor__hint">Set the path's id (parent unit is already fixed) to ingest art.</p>

                <div v-if="pendingArt" class="unit-editor__art-actions">
                  <button type="button" :disabled="busy" @click="saveArt">Save Art</button>
                  <button type="button" :disabled="busy" @click="discardPendingArt">Discard</button>
                </div>

                <p v-for="w in ingestWarnings" :key="w" class="unit-editor__warning">{{ w }}</p>
                <p v-if="ingestError" class="unit-editor__error">{{ ingestError }}</p>
              </div>
            </div>
          </section>

          <!-- Identity -->
          <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('path-identity') }">
            <button type="button" class="unit-editor__section-summary" @click="toggleSection('path-identity')">Identity</button>
            <div v-if="openSections.has('path-identity')" class="unit-editor__section-body">
              <label>
                Path ID
                <span class="unit-editor__hint">
                  {{ selectedPath === null ? 'a–z 0–9 _' : "an existing path's id can't change" }}
                </span>
                <input
                  :value="pathForm.path"
                  :disabled="selectedPath !== null"
                  placeholder="e.g. marksman"
                  @input="onPathIdInput(($event.target as HTMLInputElement).value)"
                />
              </label>
              <label>Parent Unit <input :value="pathForm.parentUnit" disabled /></label>
              <label>Description <input v-model="pathForm.description" /></label>
            </div>
          </section>

          <!-- Stats (mirrors the base unit's Stats section). A path only
               flat-overrides Vision Range; every other stat scales per-rank in
               the Ranks section below, so this is intentionally sparse. -->
          <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('path-stats') }">
            <button type="button" class="unit-editor__section-summary" @click="toggleSection('path-stats')">Stats</button>
            <div v-if="openSections.has('path-stats')" class="unit-editor__section-body">
              <label>Vision Range <input type="number" v-model.number="pathForm.visionRange" /></label>
              <p class="unit-editor__hint">Other stats (HP, damage, move speed…) scale per-rank in the Ranks section below — they aren't flat overrides here.</p>
            </div>
          </section>

          <!-- Combat (mirrors the base unit's Combat section) -->
          <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('path-combat') }">
            <button type="button" class="unit-editor__section-summary" @click="toggleSection('path-combat')">Combat</button>
            <div v-if="openSections.has('path-combat')" class="unit-editor__section-body">
              <label>
                Projectile
                <input v-model="pathForm.projectile" list="unit-editor-projectiles" placeholder="(blank = no override)" />
                <datalist id="unit-editor-projectiles">
                  <option v-for="p in projectileIds" :key="p" :value="p" />
                </datalist>
              </label>
              <label>Projectile Scale <input type="number" v-model.number="pathForm.projectileScale" /></label>
              <label>Attack Type <input v-model="pathForm.attackType" placeholder="(blank = no override)" /></label>
              <label>
                Damage Type
                <input v-model="pathForm.damageType" list="unit-editor-damage-types" placeholder="(blank = no override)" />
                <datalist id="unit-editor-damage-types">
                  <option v-for="d in damageTypes" :key="d" :value="d" />
                </datalist>
              </label>
            </div>
          </section>

          <!-- Abilities (mirrors the base unit's Abilities section) -->
          <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('path-abilities') }">
            <button type="button" class="unit-editor__section-summary" @click="toggleSection('path-abilities')">Abilities</button>
            <div v-if="openSections.has('path-abilities')" class="unit-editor__section-body">
              <label>
                Abilities (comma-separated)
                <input
                  :value="(pathForm.abilities ?? []).join(',')"
                  @input="updatePathAbilities(($event.target as HTMLInputElement).value)"
                />
                <span class="unit-editor__hint">Replaces the base unit's abilities entirely — this is not additive.</span>
              </label>
              <div class="unit-editor__channel-loop">
                <span class="unit-editor__map-label">Channel Loop <span class="unit-editor__hint">(casting-animation frames to loop; leave both blank to unset)</span></span>
                <label>
                  Start
                  <input
                    type="number"
                    :value="pathChannelLoopStart ?? ''"
                    @input="pathChannelLoopStart = ($event.target as HTMLInputElement).value === '' ? undefined : Number(($event.target as HTMLInputElement).value)"
                  />
                </label>
                <label>
                  End
                  <input
                    type="number"
                    :value="pathChannelLoopEnd ?? ''"
                    @input="pathChannelLoopEnd = ($event.target as HTMLInputElement).value === '' ? undefined : Number(($event.target as HTMLInputElement).value)"
                  />
                </label>
              </div>
              <!-- `bounds` is an opaque passthrough (no authored UI control here
                   on purpose) — it stays a plain top-level field on pathForm
                   (MODELED_PATH_KEYS includes it) so an existing path's bounds
                   round-trip untouched via saveRequestFromPathForm. -->
            </div>
          </section>

          <!-- Rank grid -->
          <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('path-ranks') }">
            <button type="button" class="unit-editor__section-summary" @click="toggleSection('path-ranks')">Ranks</button>
            <div v-if="openSections.has('path-ranks')" class="unit-editor__section-body">
              <PathRankGrid
                :base-stats="parentBaseStats"
                :ranks="pathForm.ranks || {}"
                @update:ranks="onPathRanksUpdate"
              />
            </div>
          </section>

          <!-- Perk pools -->
          <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('path-perks') }">
            <button type="button" class="unit-editor__section-summary" @click="toggleSection('path-perks')">Perk Pools</button>
            <div v-if="openSections.has('path-perks')" class="unit-editor__section-body">
              <PerkPoolEditor
                :unit="pathForm.parentUnit ?? ''"
                :path="pathForm.path ?? ''"
                :pools="pathPools"
                :catalog="perkCatalog"
                @update:pools="onPathPoolsUpdate"
              />
            </div>
          </section>

          <!-- Only meaningful for a brand-new path — an existing path's
               pathChances membership is edited on the UNIT side (Gating
               section), not re-decided every time the path is re-saved. -->
          <label v-if="selectedPath === null" class="unit-editor__checkbox-label">
            <input type="checkbox" v-model="addPathToPathChances" />
            Also add to {{ pathForm.parentUnit }}'s promotion paths (weight 1)
          </label>

          <p v-if="saveError" class="unit-editor__error">{{ saveError }}</p>
          <div class="unit-editor__actions">
            <button type="button" :disabled="busy || !pathForm.parentUnit || !pathForm.path" @click="savePath">Save Path</button>
            <button type="button" :disabled="busy || selectedPath === null" @click="removePath">Delete Path</button>
          </div>
        </template>
      </section>
    </div>
  </div>
  <!-- Shared by both Archetype (Identity) and Combat Profile (Combat): one
       combat-profile key set backs both fields. Mounted at the root so a
       collapsed Identity section can't strip the Combat picker's suggestions. -->
  <datalist id="unit-editor-archetypes">
    <option v-for="a in archetypes" :key="a" :value="a" />
  </datalist>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import {
  createBlankForm, formFromDef, saveRequestFromForm, pickTemplateStats,
  type AuthoredUnitDef, type UnitEditorForm,
} from '@/game/units/unitEditorForm'
import {
  fetchAuthoredUnitDefs, saveEditorUnit, deleteEditorUnit, EditorValidationError,
} from '@/game/units/unitEditorApi'
import { createBlankPathForm, pathFormFromDef, saveRequestFromPathForm, type PathEditorForm, type PathRankStats } from '@/game/units/pathEditorForm'
import {
  fetchPaths, fetchPerkCatalog,
  savePath as savePathApi, deletePath as deletePathApi,
  savePerks as savePerksApi,
  type EditorPathEntry, type PerkEntry,
} from '@/game/units/pathEditorApi'
import PathRankGrid from '@/components/PathRankGrid.vue'
import PerkPoolEditor from '@/components/PerkPoolEditor.vue'
import {
  fetchFactions, saveFaction, deleteFaction, fetchArchetypes,
  fetchProjectileIds, fetchAbilityIds, fetchDamageTypes, fetchBuildingIds,
  saveUnitArt, type FactionDef, type UnitArtUploadFile,
} from '@/game/units/editorCatalogApi'
import {
  ingestExportFolder, packedSheetToObjectUrls, blobToBase64,
  type DroppedFile, type IngestResult,
} from '@/game/units/spriteIngest'
import {
  buildSpriteSet, registerRuntimeSpriteSet, loadRuntimeSpriteSets, type SpriteManifest,
} from '@/game/rendering/unitSprites'
import UnitSpritePreview from '@/components/UnitSpritePreview.vue'

const units = ref<AuthoredUnitDef[]>([])
// The preview re-resolves its sprite set from the runtime overlay on refresh().
// Called after selecting a unit and after a save — including an art save,
// which reloads the runtime overlay first (see saveArt) so refresh() picks
// up the newly-served art.
const preview = ref<InstanceType<typeof UnitSpritePreview> | null>(null)
const form = ref<UnitEditorForm>(createBlankForm())
const selectedType = ref<string | null>(null)
const saveError = ref('')
const loadError = ref('')
const busy = ref(false)

// Server-sourced catalogs backing the selects. Free-text fallbacks stay
// available (datalist, not select) because the SERVER is the validator — a
// stale dropdown must never block a legal value.
const factions = ref<FactionDef[]>([])
const archetypes = ref<string[]>([])
const projectileIds = ref<string[]>([])
const abilityIds = ref<string[]>([])
const damageTypes = ref<string[]>([])
const buildingIds = ref<string[]>([])

const factionFilter = ref<string>('')   // '' = All
const newFactionId = ref('')
const newFactionName = ref('')
const showNewFaction = ref(false)
const factionError = ref('')

// --- Path/perk authoring mode (Task 3): left-list TREE + base-unit-vs-path
// editor mode. `paths` is the full merged catalog (fetchPaths, GET
// /catalog/paths); pathsByUnit re-keys it by owning unit type for the tree's
// child rows. Base-unit editing (form/selectedType/etc. above) is completely
// unchanged — editorMode just decides which half of the right panel renders.
const paths = ref<EditorPathEntry[]>([])
const pathsByUnit = computed(() => {
  const map: Record<string, EditorPathEntry[]> = {}
  for (const entry of paths.value) {
    (map[entry.unit] ??= []).push(entry)
  }
  return map
})

// Which unit rows are expanded in the tree — mirrors openSections' Set idiom.
const expandedUnits = reactive(new Set<string>())
function toggleUnitExpanded(type: string) {
  if (expandedUnits.has(type)) expandedUnits.delete(type)
  else expandedUnits.add(type)
}

const editorMode = ref<'unit' | 'path'>('unit')
const selectedPath = ref<string | null>(null)
const selectedPathParent = ref<string | null>(null)
const pathForm = ref<PathEditorForm | null>(null)

// Full /catalog/perks (loaded once in reload(), Task 4b-i's PerkPoolEditor
// needs it for wired lookups + "add existing" suggestions) and the currently
// selected/new path's per-rank pools, derived from it (see poolsForPath).
// Saving pools back to the server is Task 5 — here they only load + bind.
const perkCatalog = ref<PerkEntry[]>([])
const pathPools = ref<Record<string, PerkEntry[]>>({ bronze: [], silver: [], gold: [] })

function poolsForPath(parentUnit: string, pathId: string): Record<string, PerkEntry[]> {
  const pools: Record<string, PerkEntry[]> = { bronze: [], silver: [], gold: [] }
  for (const entry of perkCatalog.value) {
    if (entry.unitType !== parentUnit || entry.path !== pathId) continue
    const rank = entry.rank ?? ''
    if (!pools[rank]) pools[rank] = []
    pools[rank].push(entry)
  }
  return pools
}

function onPathRanksUpdate(next: Record<string, PathRankStats>) {
  if (pathForm.value) pathForm.value.ranks = next
}

function onPathPoolsUpdate(next: Record<string, PerkEntry[]>) {
  pathPools.value = next
}

// The rank grid resolves multiplier cells against the PARENT unit's base
// stats (hp/damage/attackSpeed/moveSpeed/attackRange/maxMana/healthRegenRate)
// — find it in the already-loaded unit list by pathForm.parentUnit. Cast
// rather than pass the AuthoredUnitDef through structurally: AuthoredUnitDef
// has non-numeric fields (type, faction, …) that don't satisfy
// PathRankGrid's Record<string, number|undefined> prop type, even though at
// runtime the grid only ever reads the numeric stat keys it needs.
const parentBaseStats = computed<Record<string, number | undefined>>(() => {
  const found = units.value.find((u) => u.type === pathForm.value?.parentUnit)
  return (found as unknown as Record<string, number | undefined> | undefined) ?? {}
})

// channelLoop bridging for the path form — same {start,end}-vs-undefined
// pattern as the unit form's channelLoopStart/channelLoopEnd above.
const pathChannelLoopStart = ref<number | undefined>(undefined)
const pathChannelLoopEnd = ref<number | undefined>(undefined)
watch([pathChannelLoopStart, pathChannelLoopEnd], ([s, e]) => {
  if (!pathForm.value) return
  pathForm.value.channelLoop = (s === undefined && e === undefined) ? undefined : { start: s ?? 0, end: e ?? 0 }
})

// abilities on a path REPLACE the base unit's list entirely (not additive) —
// same comma-separated binding idiom as updateStringList, scoped to the one
// string[] field a path form has.
function updatePathAbilities(raw: string) {
  if (!pathForm.value) return
  pathForm.value.abilities = raw.split(',').map((s) => s.trim()).filter(Boolean)
}

// The path id is the primary key once saved (owns art dir + perk pools), so
// it locks the same way a unit's `type` does — editable only while
// `selectedPath === null` (a brand-new, not-yet-saved path).
function onPathIdInput(raw: string) {
  if (!pathForm.value) return
  pathForm.value.path = raw.toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+/, '')
}

function selectPath(entry: EditorPathEntry) {
  // Same abandon-pending-art hazard as selectUnit — see its doc comment.
  const hadPending = pendingArt.value !== null
  clearPending()
  ingestError.value = ''
  if (hadPending) void flushPreviewOverlay()
  editorMode.value = 'path'
  selectedPath.value = entry.path
  selectedPathParent.value = entry.unit
  pathForm.value = pathFormFromDef(entry.def, entry.unit)
  pathChannelLoopStart.value = pathForm.value.channelLoop?.start
  pathChannelLoopEnd.value = pathForm.value.channelLoop?.end
  pathPools.value = poolsForPath(entry.unit, entry.path)
  preview.value?.refresh()
}

// "Also add to {parent}'s promotion paths" — only meaningful (and only shown)
// for a brand-new, not-yet-saved path; defaults CHECKED per spec §7.1.
const addPathToPathChances = ref(true)

// "+ New" chooser: Base Unit reuses the existing newUnit() flow untouched;
// Path shows an inline parent-unit picker (defaulting to the currently
// selected base unit, if any) before creating a blank path form.
const showNewMenu = ref(false)
const showNewPathPicker = ref(false)
const newPathParentUnit = ref('')

function chooseNewBaseUnit() {
  showNewMenu.value = false
  newUnit()
}

function chooseNewPath() {
  showNewMenu.value = false
  newPathParentUnit.value = selectedType.value ?? ''
  showNewPathPicker.value = true
}

function confirmNewPath() {
  if (!newPathParentUnit.value) return
  // Same abandon-pending-art hazard as newUnit — see selectUnit's doc comment.
  const hadPending = pendingArt.value !== null
  clearPending()
  ingestError.value = ''
  if (hadPending) void flushPreviewOverlay()
  pathForm.value = createBlankPathForm(newPathParentUnit.value)
  editorMode.value = 'path'
  selectedPath.value = null
  selectedPathParent.value = newPathParentUnit.value
  pathChannelLoopStart.value = undefined
  pathChannelLoopEnd.value = undefined
  pathPools.value = { bronze: [], silver: [], gold: [] }
  addPathToPathChances.value = true
  showNewPathPicker.value = false
  preview.value?.refresh()
}

const visibleUnits = computed(() =>
  factionFilter.value
    ? units.value.filter((u) => u.faction === factionFilter.value)
    : units.value,
)

// Per-faction unit counts for the filter pills. Derived from the units the
// editor actually loaded, not from the faction records — so a faction with a
// record but no units correctly reads 0, which is the signal that it can be
// deleted.
const unitCountByFaction = computed(() => {
  const counts: Record<string, number> = {}
  for (const unit of units.value) {
    if (!unit.faction) continue
    counts[unit.faction] = (counts[unit.faction] ?? 0) + 1
  }
  return counts
})

// An archetype outside the combat-profile set is NOT rejected by the server —
// it silently falls back to the soldier profile. So warn, don't block.
// (combatProfile IS validated server-side and will hard-fail on save; archetype
// is not. Same list, different strictness — that's why this is a warning.)
const archetypeWarning = computed(() => {
  const value = form.value.archetype
  if (!value || archetypes.value.length === 0) return ''
  if (archetypes.value.includes(value)) return ''
  return `"${value}" is not a known combat profile — this unit will fall back to the soldier profile.`
})

async function reloadCatalogs() {
  const [f, a, p, ab, dt, b] = await Promise.all([
    fetchFactions(), fetchArchetypes(), fetchProjectileIds(),
    fetchAbilityIds(), fetchDamageTypes(), fetchBuildingIds(),
  ])
  factions.value = f
  archetypes.value = a
  projectileIds.value = p
  abilityIds.value = ab
  damageTypes.value = dt
  buildingIds.value = b
}

async function createFaction() {
  factionError.value = ''
  busy.value = true
  try {
    await saveFaction({ id: newFactionId.value.trim(), displayName: newFactionName.value.trim() })
    await reloadCatalogs()
    factionFilter.value = newFactionId.value.trim()
    // Only seed the faction of a NEW unit. Writing this while an existing unit
    // is loaded would silently reassign that unit's faction, with an enabled
    // Save button sitting right under it.
    if (selectedType.value === null) form.value.faction = factionFilter.value
    newFactionId.value = ''
    newFactionName.value = ''
    showNewFaction.value = false
  } catch (e) {
    factionError.value = e instanceof EditorValidationError ? e.serverMessage
      : e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

// The server refuses to delete a faction that still owns units, and its message
// names them. Surface it verbatim — it tells the author exactly what to fix.
async function removeFaction(id: string) {
  factionError.value = ''
  busy.value = true
  try {
    await deleteFaction(id)
    if (factionFilter.value === id) factionFilter.value = ''
    await reloadCatalogs()
  } catch (e) {
    factionError.value = e instanceof EditorValidationError ? e.serverMessage
      : e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

// Collapsible sections — Identity starts open so a freshly loaded panel isn't
// entirely collapsed; every other section starts closed like ItemEditorPanel's
// accordion, but as a Set so multiple sections can be open at once.
const openSections = reactive(new Set<string>(['preview', 'identity', 'path-preview', 'path-identity']))
function toggleSection(key: string) {
  if (openSections.has(key)) openSections.delete(key)
  else openSections.add(key)
}

// resourceCost / pathChances are string->number maps on the form. Rows are
// kept as local {key,value} arrays (so add/remove/rename is simple array
// editing) and mirrored back into form.<field> via watch — saveRequestFromForm
// reads form.<field> directly, so the mirrored map is what actually saves.
interface MapRow { key: string; value: number }
const resourceCostRows = ref<MapRow[]>([])
const pathChancesRows = ref<MapRow[]>([])

function rowsFromMap(map?: Record<string, number>): MapRow[] {
  return Object.entries(map ?? {}).map(([key, value]) => ({ key, value }))
}
function mapFromRows(rows: MapRow[]): Record<string, number> {
  const out: Record<string, number> = {}
  for (const row of rows) if (row.key) out[row.key] = row.value
  return out
}
function addResourceCostRow() { resourceCostRows.value.push({ key: '', value: 0 }) }
function removeResourceCostRow(idx: number) { resourceCostRows.value.splice(idx, 1) }
function addPathChanceRow() { pathChancesRows.value.push({ key: '', value: 0 }) }
function removePathChanceRow(idx: number) { pathChancesRows.value.splice(idx, 1) }

watch(resourceCostRows, (rows) => { form.value.resourceCost = mapFromRows(rows) }, { deep: true })
watch(pathChancesRows, (rows) => { form.value.pathChances = mapFromRows(rows) }, { deep: true })

// channelLoop is a nested {start,end} object that must stay entirely
// undefined when both fields are unset. Tracked as two local optional refs
// and merged back into form.channelLoop via watch.
const channelLoopStart = ref<number | undefined>(undefined)
const channelLoopEnd = ref<number | undefined>(undefined)
watch([channelLoopStart, channelLoopEnd], ([s, e]) => {
  form.value.channelLoop = (s === undefined && e === undefined) ? undefined : { start: s ?? 0, end: e ?? 0 }
})

// Generic comma-separated string[] binding, shared by abilities/capabilities/
// requiresBuildings/targetableTypes.
type StringListField = 'abilities' | 'capabilities' | 'requiresBuildings' | 'targetableTypes'
function updateStringList(field: StringListField, raw: string) {
  form.value[field] = raw.split(',').map((s) => s.trim()).filter(Boolean)
}

// --- Art ingest (Phase 3): drop a PixelLab export folder, pack it entirely
// in-browser, preview it live via the Phase 2 runtime overlay (P3-B — reuses
// that overlay rather than a second preview path), and persist on Save. ---

// Packed-but-unsaved ingest result. Feeds the Phase 2 overlay via object URLs
// so the preview plays the freshly-packed art before anything is saved.
const pendingArt = ref<IngestResult | null>(null)
let pendingRevoke: (() => void) | null = null
const ingestError = ref('')
const ingestWarnings = ref<string[]>([])

// Guards runIngest against overlapping drops: without this, starting a
// second drop while the first is still decoding/rasterizing races
// `pendingRevoke = revoke` between the two in-flight calls — whichever
// resolves last silently overwrites the other's revoke function, leaking
// every blob URL from the earlier drop for the page's lifetime (and leaving
// its set registered in the shared overlay, see flushPreviewOverlay below).
const ingesting = ref(false)

// Revokes the pending object URLs (if any) and clears local ingest state.
// This is PURELY local cleanup — it does NOT touch the shared runtime
// overlay (`runtimeSprites`, module-global in unitSprites.ts). Callers that
// abandon a registered preview set outside save/discard must additionally
// call `flushPreviewOverlay()` — see its doc comment for why.
function clearPending() {
  pendingRevoke?.()
  pendingRevoke = null
  pendingArt.value = null
  ingestWarnings.value = []
}

// The shared runtime sprite overlay (`registerRuntimeSpriteSet` /
// `getUnitSpriteSet`) is a MODULE-GLOBAL that the live game map also reads —
// it outlives this panel. If the panel registers an in-browser preview set
// (backed by object URLs) and the URLs are then revoked by `clearPending()`
// without also flushing the overlay, that unit renders blank in an actual
// match for the rest of the session (the overlay only reloads at app boot
// otherwise). `loadRuntimeSpriteSets()` is the correct fix, not a plain
// "unregister this key": it fully swaps the overlay back to whatever the
// SERVER actually has for every unit, which correctly restores prior
// server-persisted art for this unit (if any) rather than just clearing it.
async function flushPreviewOverlay() {
  await loadRuntimeSpriteSets()
  preview.value?.refresh()
}

// spritePacking.ts's pure core can't derive `key` (no directory context —
// see spritePacking.ts's SpriteManifestJSON doc comment), so we attach it
// here from the unit type, mirroring the CLI's `path.basename(unitDir)`
// rule. Used both for the live preview manifest and the uploaded sprites.json.
function manifestWithKey(manifest: IngestResult['manifest'], key: string): SpriteManifest {
  return { ...manifest, key }
}

// Art ingest (runIngest/onFolderDropped/saveArt below) is shared between unit
// and path mode — these four computeds are the ONLY mode-aware surface of
// that machinery, so unit-mode behavior is provably unchanged (each reduces
// to exactly the old `form.value.*` expression when editorMode==='unit').
// A path's art is keyed by (parent unit, path id) — saveUnitArt's optional
// `path` param routes it to the path's own writable art dir instead of the
// base unit's.
const artKey = computed(() => editorMode.value === 'path'
  ? (pathForm.value?.path || pathForm.value?.parentUnit || '')
  : form.value.type)
const artUnit = computed(() => editorMode.value === 'path'
  ? (pathForm.value?.parentUnit ?? '')
  : form.value.type)
const artFaction = computed(() => editorMode.value === 'path'
  ? (units.value.find((u) => u.type === pathForm.value?.parentUnit)?.faction ?? '')
  : form.value.faction)
const artPath = computed(() => editorMode.value === 'path' ? (pathForm.value?.path || undefined) : undefined)
const canIngestArt = computed(() => editorMode.value === 'path'
  ? !!(pathForm.value?.parentUnit && pathForm.value?.path)
  : !!(form.value.type && form.value.faction))

async function runIngest(files: DroppedFile[]) {
  // A drop that arrives while a previous one is still decoding/rasterizing
  // is ignored outright rather than raced (see `ingesting`'s doc comment).
  if (ingesting.value) return
  ingesting.value = true
  ingestError.value = ''
  // Captured BEFORE clearPending() revokes it: clearPending only revokes this
  // drop's OWN previous object URLs, it never touches the shared overlay. If
  // a preview set was actually registered (from a prior successful drop),
  // every exit below that does NOT go on to register a replacement set must
  // flush the overlay back to server truth — otherwise runtimeSprites is left
  // pointing at the just-revoked URLs (see flushPreviewOverlay's doc comment).
  const hadPending = pendingArt.value !== null
  clearPending()
  try {
    if (files.length === 0) {
      if (hadPending) void flushPreviewOverlay()
      return
    }
    const result = await ingestExportFolder(files)
    ingestWarnings.value = result.warnings

    const { urls, revoke } = packedSheetToObjectUrls(result)
    pendingRevoke = revoke
    pendingArt.value = result

    // Feed the Phase 2 overlay so the preview shows the packed art live,
    // before saving — this IS the preview-before-persist path (P3-B).
    const key = artKey.value.toLowerCase()
    const manifest = manifestWithKey(result.manifest, artKey.value)
    const set = buildSpriteSet(key, manifest, (rel) => urls[rel])
    if (set) registerRuntimeSpriteSet(set)
    preview.value?.refresh()
  } catch (e) {
    ingestError.value = e instanceof Error ? e.message : String(e)
    clearPending()
    // The previous drop's preview set (if any) is now dead (URLs revoked, no
    // replacement registered) — restore server truth so it doesn't stay
    // shadowed-and-broken in the shared overlay.
    if (hadPending) void flushPreviewOverlay()
  } finally {
    ingesting.value = false
  }
}

// Primary ingest path: <input type="file" webkitdirectory> — the reliable
// cross-browser way to pick a whole folder (prioritized per the plan; drag
// and drop of folders is finicky). webkitRelativePath is
// "<wrapperFolder>/<rest...>"; strip the wrapper segment so paths are
// relative to the export folder root, matching what ingestExportFolder expects.
function onFolderInputChanged(fileList: FileList | null) {
  if (!fileList) return
  const files: DroppedFile[] = [...fileList].map((f) => ({
    path: f.webkitRelativePath.split('/').slice(1).join('/'),
    blob: f,
  }))
  void runIngest(files)
}

// Secondary ingest path: dragging a folder onto the zone. Best-effort via the
// non-standard-but-broadly-supported `DataTransferItem.webkitGetAsEntry`
// FileSystem API. A dropped top-level DIRECTORY is treated as the export
// folder's own wrapper (its name is stripped, matching the input path's
// convention above); a top-level FILE (or a directory dropped alongside
// loose files, without an enclosing wrapper) keeps its own relative path.
function readDirectoryEntries(reader: FileSystemDirectoryReader): Promise<FileSystemEntry[]> {
  return new Promise((resolve, reject) => {
    const all: FileSystemEntry[] = []
    const readBatch = () => {
      reader.readEntries((batch) => {
        if (batch.length === 0) resolve(all)
        else {
          all.push(...batch)
          readBatch()
        }
      }, reject)
    }
    readBatch()
  })
}

async function collectEntry(entry: FileSystemEntry, prefix: string): Promise<DroppedFile[]> {
  const relPath = prefix ? `${prefix}/${entry.name}` : entry.name
  if (entry.isFile) {
    const file = await new Promise<File>((resolve, reject) => {
      (entry as FileSystemFileEntry).file(resolve, reject)
    })
    return [{ path: relPath, blob: file }]
  }
  if (entry.isDirectory) {
    const children = await readDirectoryEntries((entry as FileSystemDirectoryEntry).createReader())
    const nested = await Promise.all(children.map((child) => collectEntry(child, relPath)))
    return nested.flat()
  }
  return []
}

async function collectTopLevelEntry(entry: FileSystemEntry): Promise<DroppedFile[]> {
  if (!entry.isDirectory) return collectEntry(entry, '')
  const children = await readDirectoryEntries((entry as FileSystemDirectoryEntry).createReader())
  const nested = await Promise.all(children.map((child) => collectEntry(child, '')))
  return nested.flat()
}

async function onFolderDropped(event: DragEvent) {
  if (!canIngestArt.value) return
  if (ingesting.value) return
  const items = event.dataTransfer?.items
  if (!items || items.length === 0) return
  const topEntries: FileSystemEntry[] = []
  for (const item of items) {
    const entry = item.webkitGetAsEntry?.()
    if (entry) topEntries.push(entry)
  }
  if (topEntries.length === 0) return
  try {
    const nested = await Promise.all(topEntries.map(collectTopLevelEntry))
    await runIngest(nested.flat())
  } catch (e) {
    ingestError.value = e instanceof Error ? e.message : String(e)
    clearPending()
  }
}

async function saveArt() {
  const art = pendingArt.value
  if (!art) return
  ingestError.value = ''
  busy.value = true
  try {
    const manifest = { ...manifestWithKey(art.manifest, artKey.value), packedAt: new Date().toISOString() }
    const files: UnitArtUploadFile[] = [
      { name: 'sprites.json', contentBase64: await blobToBase64(new Blob([JSON.stringify(manifest, null, 2)])) },
    ]
    for (const sheet of art.sheets) {
      files.push({ name: sheet.name, contentBase64: await blobToBase64(sheet.blob) })
    }
    if (art.portrait) {
      files.push({ name: 'portrait.png', contentBase64: await blobToBase64(art.portrait) })
    }
    await saveUnitArt({ faction: artFaction.value, unit: artUnit.value, path: artPath.value, files })
    // Replace the in-memory object-URL preview set with the persisted server art.
    clearPending()
    await flushPreviewOverlay()
  } catch (e) {
    ingestError.value = e instanceof EditorValidationError ? e.serverMessage
      : e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function discardPendingArt() {
  clearPending()
  ingestError.value = ''
  // Reset the runtime overlay to server truth — the discarded in-memory set
  // must stop shadowing whatever (if anything) is actually persisted.
  await flushPreviewOverlay()
}

onBeforeUnmount(() => {
  // Same hazard as selectUnit/newUnit: closing the editor with an unsaved
  // drop still registered must flush the shared overlay back to server
  // truth, or the abandoned unit stays broken in a live match after the
  // panel is long gone. Fire-and-forget — the component itself is unmounting,
  // there is nothing left here to await into.
  const hadPending = pendingArt.value !== null
  clearPending()
  if (hadPending) void loadRuntimeSpriteSets()
})

async function reload() {
  try {
    const [u, p, pc] = await Promise.all([fetchAuthoredUnitDefs(), fetchPaths(), fetchPerkCatalog()])
    units.value = u
    paths.value = p
    perkCatalog.value = pc
    loadError.value = ''
    // A successful unit list refresh means the list itself is current — any
    // stale faction-delete error (e.g. naming units the author just deleted)
    // no longer applies.
    factionError.value = ''
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : String(e)
  }
}

function selectUnit(def: AuthoredUnitDef) {
  // A unit switch abandons any not-yet-saved ingest for the PREVIOUS unit —
  // its object URLs must be revoked (clearPending), AND — if a preview set
  // was actually registered into the shared overlay — that overlay must be
  // flushed back to server truth (flushPreviewOverlay), or the abandoned
  // unit renders blank in a real match. See flushPreviewOverlay's doc comment.
  const hadPending = pendingArt.value !== null
  clearPending()
  ingestError.value = ''
  if (hadPending) void flushPreviewOverlay()
  editorMode.value = 'unit'
  selectedPath.value = null
  selectedPathParent.value = null
  form.value = formFromDef(def)
  selectedType.value = def.type
  saveError.value = ''
  resourceCostRows.value = rowsFromMap(def.resourceCost)
  pathChancesRows.value = rowsFromMap(def.pathChances)
  channelLoopStart.value = def.channelLoop?.start
  channelLoopEnd.value = def.channelLoop?.end
  preview.value?.refresh()
}

// The `type` field is the unit's id (its directory name, sprite key, and what
// pathChances/requiresBuildings/spawn all reference), so it must match
// `^[a-z0-9_]+$`. Rather than make the author type both a display Name and a
// matching id, the id is a slug auto-derived from the Name — lowercase, with any
// run of non-alphanumeric characters collapsed to a single underscore.
function slugifyUnitId(raw: string): string {
  return raw.toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, '')
}

// True once the author has hand-edited the id, which stops it from tracking the
// Name (standard slug-field behaviour). Reset per new unit.
const typeIdManuallyEdited = ref(false)

function onTypeIdInput(raw: string) {
  // Slug the manual input too, so a typed "Raider Brute" becomes a valid id
  // rather than failing server validation on save. No trailing-underscore trim
  // here so typing mid-word (e.g. "raider_") isn't fought.
  form.value.type = raw.toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+/, '')
  typeIdManuallyEdited.value = true
}

// Auto-derive the id from the Name for NEW units only, until the author edits the
// id by hand. Existing units keep their id — it's the primary key, and renaming
// it would orphan the unit's art, promotion paths, and every reference to it.
watch(() => form.value.name, (name) => {
  if (selectedType.value === null && !typeIdManuallyEdited.value) {
    form.value.type = slugifyUnitId(name ?? '')
  }
})

function newUnit() {
  const hadPending = pendingArt.value !== null
  clearPending()
  ingestError.value = ''
  if (hadPending) void flushPreviewOverlay()
  editorMode.value = 'unit'
  selectedPath.value = null
  selectedPathParent.value = null
  form.value = createBlankForm(pickTemplateStats(units.value))
  form.value.faction = factionFilter.value || ''
  selectedType.value = null
  typeIdManuallyEdited.value = false
  saveError.value = ''
  resourceCostRows.value = []
  pathChancesRows.value = []
  channelLoopStart.value = undefined
  channelLoopEnd.value = undefined
}

async function save() {
  saveError.value = ''
  busy.value = true
  try {
    await saveEditorUnit(saveRequestFromForm(form.value))
    await reload()
    selectedType.value = form.value.type
    preview.value?.refresh()
  } catch (e) {
    saveError.value = e instanceof EditorValidationError ? e.serverMessage
      : e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function removeUnit() {
  if (!selectedType.value) return
  saveError.value = ''
  busy.value = true
  try {
    await deleteEditorUnit(selectedType.value)
    await reload()
    newUnit()
  } catch (e) {
    saveError.value = e instanceof EditorValidationError ? e.serverMessage
      : e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

const PATH_RANKS = ['bronze', 'silver', 'gold'] as const

// savePath persists the path file, then its three rank perk pools, then —
// for a brand-new path with the checkbox checked — adds it to the parent
// unit's pathChances. ORDER IS LOAD-BEARING (spec §7.1/§9.1):
//   1. the path file must exist before ANYTHING references it — the server's
//      SaveEditorPerkPool rejects a pool whose path doesn't exist yet, and a
//      pathChances row pointing at a nonexistent path is exactly the
//      dangling reference that boot-panics the server (see path_editor.go's
//      doc comment on ordering). So: path first, unconditionally.
//   2. perk pools next — they reference the path, which now exists.
//   3. pathChances last, and ONLY for a new path — re-parenting or touching
//      an existing path's pathChances is not this button's job.
// If step 3 fails validation, the path (and its pools) already saved
// successfully — we surface the error but do NOT attempt to roll that back.
async function savePath() {
  if (!pathForm.value) return
  saveError.value = ''
  busy.value = true
  const isNewPath = selectedPath.value === null
  try {
    const req = saveRequestFromPathForm(pathForm.value)
    const pathId = req.path.path ?? ''

    await savePathApi(req)

    for (const rank of PATH_RANKS) {
      await savePerksApi({ unit: req.unit, path: pathId, rank, perks: pathPools.value[rank] ?? [] })
    }

    if (isNewPath && addPathToPathChances.value) {
      const parentDef = units.value.find((u) => u.type === req.unit)
      if (parentDef) {
        const parentForm = formFromDef(parentDef)
        // Merge, don't clobber — a path id already present (unlikely, but not
        // impossible if pathChances was hand-authored) keeps its own weight.
        parentForm.pathChances = { ...(parentForm.pathChances ?? {}), [pathId]: parentForm.pathChances?.[pathId] ?? 1 }
        await saveEditorUnit(saveRequestFromForm(parentForm))
      }
    }

    await reload()
    const savedEntry = paths.value.find((p) => p.unit === req.unit && p.path === pathId)
    if (savedEntry) selectPath(savedEntry)
    preview.value?.refresh()
  } catch (e) {
    saveError.value = e instanceof EditorValidationError ? e.serverMessage
      : e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function removePath() {
  if (!selectedPath.value) return
  saveError.value = ''
  busy.value = true
  try {
    await deletePathApi(selectedPath.value)
    // Only reached on success — nothing changed server-side when the delete
    // was rejected (e.g. still referenced by a unit's pathChances), so a
    // thrown error skips straight to the catch below without reloading.
    await reload()
    const parentType = selectedPathParent.value
    const parentDef = parentType ? units.value.find((u) => u.type === parentType) : undefined
    if (parentDef) {
      selectUnit(parentDef)
    } else {
      editorMode.value = 'unit'
      selectedPath.value = null
      selectedPathParent.value = null
      pathForm.value = null
    }
  } catch (e) {
    saveError.value = e instanceof EditorValidationError ? e.serverMessage
      : e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

onMounted(async () => {
  await reload()
  try {
    await reloadCatalogs()
  } catch (e) {
    // A catalog fetch failure degrades the selects to free text — it must not
    // take the whole panel down, because unit editing still works without them.
    loadError.value = e instanceof Error ? e.message : String(e)
  }
})
</script>

<style scoped>
.unit-editor {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100%;
  min-height: 0;
  min-width: 0;
  gap: 12px;
  padding: 16px;
  box-sizing: border-box;
}

/* The two-column body sits under the full-width faction pill bar. */
.unit-editor__body {
  display: flex;
  flex: 1;
  min-height: 0;
  min-width: 0;
  gap: 12px;
}

.unit-editor__list {
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

.unit-editor__new {
  font-weight: 700;
  width: 100%;
}

.unit-editor__new-menu {
  position: relative;
}

.unit-editor__new-menu-popover {
  position: absolute;
  z-index: 5;
  top: 100%;
  left: 0;
  right: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-top: 4px;
  padding: 6px;
  background: rgba(8, 14, 24, 0.96);
  border: 1px solid rgba(215, 187, 132, 0.35);
  border-radius: 10px;
}

.unit-editor__new-menu-popover button {
  width: 100%;
  border: 1px solid transparent;
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.6);
  color: #f8fafc;
  padding: 6px 8px;
  font-size: 0.76rem;
  text-align: left;
}

.unit-editor__new-path-picker {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 8px;
  border: 1px solid rgba(215, 187, 132, 0.35);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.6);
}

.unit-editor__new-path-picker select {
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
}

.unit-editor__new-path-picker button {
  border: 1px solid rgba(148, 163, 184, 0.25);
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.6);
  color: #f8fafc;
  padding: 6px 9px;
  font-size: 0.76rem;
}

.unit-editor__tree {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.unit-editor__tree-node + .unit-editor__tree-node {
  margin-top: 2px;
}

.unit-editor__tree-row {
  display: flex;
  align-items: center;
  gap: 4px;
}

.unit-editor__tree-toggle {
  flex: 0 0 auto;
  width: 18px;
  border: 0;
  background: transparent;
  color: rgba(226, 232, 240, 0.7);
  padding: 0;
  font-size: 0.7rem;
}

.unit-editor__tree-toggle-spacer {
  flex: 0 0 auto;
  width: 18px;
}

.unit-editor__tree-unit-btn,
.unit-editor__tree-path-btn {
  flex: 1 1 auto;
  border: 1px solid transparent;
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.6);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
  text-align: left;
}

.unit-editor__tree-unit-btn.is-selected,
.unit-editor__tree-path-btn.is-selected {
  border-color: rgba(215, 187, 132, 0.6);
  background: rgba(30, 41, 59, 0.9);
}

.unit-editor__tree-children {
  list-style: none;
  margin: 2px 0 0;
  padding: 0 0 0 22px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.unit-editor__tree-path-btn {
  font-size: 0.74rem;
  opacity: 0.9;
}

.unit-editor__form {
  flex: 1;
  min-width: 0;
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

.unit-editor__section {
  border: 1px solid rgba(148, 163, 184, 0.18);
  border-radius: 12px;
  background: rgba(8, 14, 24, 0.55);
  overflow: clip;
  flex: 0 0 auto;
}

.unit-editor__section--open {
  background: rgba(8, 14, 24, 0.72);
}

.unit-editor__section-summary {
  width: 100%;
  border: 0;
  padding: 10px 12px;
  font-size: 0.78rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: #f8fafc;
  text-align: left;
  background: linear-gradient(180deg, rgba(25, 35, 52, 0.92), rgba(14, 22, 36, 0.94));
}

.unit-editor__section-summary::after {
  content: '+';
  float: right;
  color: #d7bb84;
}

.unit-editor__section--open .unit-editor__section-summary::after {
  content: '-';
}

.unit-editor__section-body {
  display: grid;
  gap: 8px;
  padding: 10px;
}

.unit-editor__section-body label {
  display: grid;
  gap: 4px;
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.75rem;
}

.unit-editor__section-body input,
.unit-editor__section-body select {
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
}

.unit-editor__checkbox-label {
  flex-direction: row !important;
  align-items: center;
  gap: 6px !important;
}

.unit-editor__map {
  display: grid;
  gap: 6px;
}

.unit-editor__map-label {
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.75rem;
  font-weight: 700;
}

.unit-editor__hint {
  font-weight: 400;
  opacity: 0.65;
  text-transform: none;
  letter-spacing: 0;
}

.unit-editor__map-row {
  display: flex;
  gap: 6px;
  align-items: center;
}

.unit-editor__map-row input {
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
  flex: 1;
}

.unit-editor__channel-loop {
  display: grid;
  gap: 6px;
}

.unit-editor__actions {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: auto;
  padding-top: 8px;
}

.unit-editor__error {
  color: #fca5a5;
  font-size: 0.78rem;
}

/* Faction pill bar — spans the full panel width above both columns. */
.unit-editor__topbar {
  flex: 0 0 auto;
  display: grid;
  gap: 8px;
  background: rgba(3, 8, 14, 0.86);
  border: 1px solid rgba(148, 163, 184, 0.22);
  border-radius: 16px;
  padding: 10px 12px;
}

.unit-editor__pills {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
}

/* Pushes the +Faction / Delete actions to the right edge, so the faction
   filters read as one group and the destructive action is nowhere near them. */
.unit-editor__pill-spacer {
  flex: 1 1 auto;
}

.unit-editor__pill {
  border: 1px solid rgba(148, 163, 184, 0.25);
  border-radius: 999px;
  background: rgba(15, 23, 42, 0.72);
  color: rgba(226, 232, 240, 0.86);
  padding: 5px 12px;
  font-size: 0.76rem;
  font-weight: 600;
  white-space: nowrap;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.unit-editor__pill:hover:not(:disabled) {
  border-color: rgba(215, 187, 132, 0.45);
  background: rgba(30, 41, 59, 0.9);
}

.unit-editor__pill.is-active {
  border-color: rgba(215, 187, 132, 0.75);
  background: rgba(215, 187, 132, 0.16);
  color: #f8fafc;
}

.unit-editor__pill-count {
  font-size: 0.68rem;
  font-weight: 700;
  color: rgba(226, 232, 240, 0.6);
  background: rgba(2, 6, 12, 0.55);
  border-radius: 999px;
  padding: 1px 6px;
}

.unit-editor__pill.is-active .unit-editor__pill-count {
  color: #f8fafc;
}

.unit-editor__pill--add {
  border-style: dashed;
  border-color: rgba(215, 187, 132, 0.5);
  color: #d7bb84;
}

.unit-editor__pill--danger {
  border-color: rgba(248, 113, 113, 0.45);
  color: #fca5a5;
}

.unit-editor__pill--danger:hover:not(:disabled) {
  border-color: rgba(248, 113, 113, 0.8);
  background: rgba(127, 29, 29, 0.35);
}

.unit-editor__new-faction {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  padding: 8px;
  border: 1px solid rgba(215, 187, 132, 0.35);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.6);
}

.unit-editor__new-faction input {
  flex: 1 1 140px;
  min-width: 0;
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
}

.unit-editor__warning {
  color: #fcd34d;
  font-size: 0.72rem;
  margin: 0;
}

/* Art ingest drop zone — sits under the preview, in the panel's dark aesthetic. */
.unit-editor__art-ingest {
  display: flex;
  flex-direction: column;
  gap: 8px;
  border: 1px dashed rgba(148, 163, 184, 0.3);
  border-radius: 12px;
  padding: 12px;
  background: rgba(8, 14, 24, 0.55);
}

.unit-editor__art-drop {
  display: flex;
  flex-direction: column;
  gap: 4px;
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.78rem;
  font-weight: 600;
}

.unit-editor__art-drop input[type='file'] {
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.74rem;
}

.unit-editor__art-actions {
  display: flex;
  gap: 8px;
}
</style>
