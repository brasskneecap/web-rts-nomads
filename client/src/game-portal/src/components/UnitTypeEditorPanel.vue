<template>
  <EditorShell class="unit-editor">
    <!-- ── Sidebar: units grouped by faction + faction management ────────── -->
    <template #sidebar>
      <div class="unit-sidebar">
        <div class="unit-sidebar__list">
          <EditorSidebar
            title="Units"
            new-label="Add New Unit"
            :groups="sidebarGroups"
            :selected-id="sidebarSelectedId"
            :search="search"
            search-placeholder="Search units…"
            empty-text="No units match."
            @update:search="search = $event"
            @select="selectUnitById"
            @new="newUnit"
            @duplicate="duplicateUnit"
          />
        </div>
        <p v-if="loadError" class="unit-error unit-sidebar__load-error">{{ loadError }}</p>

        <!-- Faction CRUD lives under the list: a unit's Faction dropdown can
             only offer factions that exist, so creating one is a prerequisite
             for authoring a unit in a new faction. -->
        <div class="unit-sidebar__factions">
          <button
            type="button"
            class="unit-sidebar__faction-toggle"
            :disabled="busy"
            @click="showNewFaction = !showNewFaction"
          >
            {{ showNewFaction ? '− Faction' : '+ Faction' }}
          </button>
          <div v-if="showNewFaction" class="unit-sidebar__faction-form">
            <input v-model="newFactionId" placeholder="id (a-z0-9_)" aria-label="Faction id" />
            <input v-model="newFactionName" placeholder="Display Name" aria-label="Faction name" />
            <UiButton size="sm" variant="active" :disabled="busy || !newFactionId.trim()" @click="createFaction">
              Create
            </UiButton>
          </div>
          <div v-if="factions.length" class="unit-sidebar__faction-del">
            <select v-model="factionToDelete" aria-label="Delete a faction">
              <option value="" disabled>Delete faction…</option>
              <option v-for="f in factions" :key="f.id" :value="f.id">{{ f.displayName }}</option>
            </select>
            <UiButton
              size="sm"
              variant="secondary"
              :disabled="busy || !factionToDelete"
              @click="factionToDelete && removeFaction(factionToDelete)"
            >
              Delete
            </UiButton>
          </div>
          <p v-if="factionError" class="unit-error">{{ factionError }}</p>
        </div>
      </div>
    </template>

    <!-- ── Main: the editor form (base unit OR promotion path) ───────────── -->
    <template #main>
      <!-- Default (and after a delete): nothing is selected, so entering the
           tab shows an empty screen. The form appears only on demand. -->
      <div v-if="editorMode === 'empty'" class="unit-editor__empty">
        <p v-if="loadError" role="alert">{{ loadError }}</p>
        <p v-else>Select a unit, or create a new one.</p>
      </div>

      <template v-else>
      <!-- One header for the unit; a promotion path saves via its own compact
           action bar inside the Paths tab (no separate path page anymore). -->
      <EditorHeader
        :title="form.name || form.type || 'New Unit'"
        :badge="form.faction ? factionName(form.faction) : ''"
        :breadcrumb="unitBreadcrumb"
        :file-path="unitFilePath"
        :id="form.type"
        :id-editable="selectedType === null"
        id-input-id="ue-id"
        :saving="busy"
        :save-disabled="unitSaveDisabled"
        :saved-label="savedLabel"
        :error="saveError"
        :remove-label="selectedType ? 'Delete' : ''"
        @update:id="onTypeIdInput"
        @save="save"
        @remove="removeUnit"
      />

      <!-- Unit sections are grouped into tabs. -->
      <EditorTabs
        v-model="activeUnitTab"
        :tabs="UNIT_TABS"
        id-prefix="unit-editor"
        label="Unit sections"
        class="unit-editor__tabs"
      />

      <GameScrollArea class="unit-editor__scroll">
        <!-- Non-Paths tabs render the unit's own card grid; the Paths tab swaps
             in the promotion-path manager (below). -->
        <div v-if="activeUnitTab !== 'paths'" class="unit-editor__grid">
            <!-- Identity -->
            <SectionCard v-show="activeUnitTab === sectionTab('identity')" title="Identity" :index="sectionIndex('identity')">
              <!-- Profile picture sits at the top of Identity: a framed preview
                   plus a PNG file picker that writes the unit's portrait.png. -->
              <div class="unit-editor__portrait">
                <IconPreview :src="portraitUrl" :size="96" />
                <div class="unit-editor__portrait-side">
                  <EditorField label="Profile Picture" hint="(PNG · square works best)" for-id="ue-portrait">
                    <input
                      id="ue-portrait"
                      type="file"
                      accept="image/png"
                      :disabled="!canUploadPortrait || busy"
                      @change="onPortraitChosen"
                    />
                  </EditorField>
                  <p v-if="!canUploadPortrait" class="unit-hint">Save the unit first to set its profile picture.</p>
                </div>
              </div>

              <EditorField label="Name" for-id="ue-name">
                <input id="ue-name" v-model="form.name" type="text" />
              </EditorField>
              <EditorField label="Faction" for-id="ue-faction">
                <select id="ue-faction" v-model="form.faction">
                  <option value="" disabled>— select a faction —</option>
                  <option v-for="f in factions" :key="f.id" :value="f.id">{{ f.displayName }}</option>
                </select>
              </EditorField>
              <EditorField label="Archetype" hint="(defaults to the unit type)" for-id="ue-archetype">
                <input id="ue-archetype" v-model="form.archetype" list="unit-editor-archetypes" />
              </EditorField>
              <p v-if="archetypeWarning" class="unit-warn">{{ archetypeWarning }}</p>
              <EditorField label="Train Label" for-id="ue-train-label">
                <input id="ue-train-label" v-model="form.trainLabel" type="text" />
              </EditorField>
            </SectionCard>

            <!-- Preview: card 2 — sits to the right of Identity, two cards wide. -->
            <SectionCard v-show="activeUnitTab === sectionTab('preview')" title="Preview" :index="sectionIndex('preview')" class="unit-editor__wide3">
              <UnitSpritePreview
                ref="preview"
                :unit-key="form.type"
                :projectile="form.projectile"
                :projectile-scale="form.projectileScale"
                :channel-loop="form.channelLoop"
                :channel-ability="channelAbility"
                v-model:attack-origin="form.attackOrigin"
              />
              <div class="unit-art" @dragover.prevent @drop.prevent="onFolderDropped">
                <label v-if="canIngestArt" class="unit-art__drop">
                  <input
                    type="file"
                    webkitdirectory
                    multiple
                    :disabled="ingesting"
                    @change="onFolderInputChanged(($event.target as HTMLInputElement).files)"
                  />
                  {{ ingesting ? 'Packing…' : 'Drop / choose a PixelLab export folder' }}
                </label>
                <p v-else class="unit-hint">Set the unit's type and faction to ingest art.</p>

                <div v-if="pendingArt" class="unit-art__actions">
                  <UiButton size="sm" variant="active" :disabled="busy" @click="saveArt">Save Art</UiButton>
                  <UiButton size="sm" variant="secondary" :disabled="busy" @click="discardPendingArt">Discard</UiButton>
                </div>

                <p v-for="w in ingestWarnings" :key="w" class="unit-warn">{{ w }}</p>
                <p v-if="ingestError" class="unit-error">{{ ingestError }}</p>
              </div>

              <!-- Channel Loop tunes the casting animation shown above (start/end
                   frames to loop). Blank both to unset. -->
              <EditorField label="Channel Loop" hint="(casting frames · blank = unset)">
                <div class="unit-editor__pair">
                  <input
                    type="number"
                    placeholder="start"
                    aria-label="Channel loop start"
                    :value="channelLoopStart ?? ''"
                    @input="channelLoopStart = ($event.target as HTMLInputElement).value === '' ? undefined : Number(($event.target as HTMLInputElement).value)"
                  />
                  <input
                    type="number"
                    placeholder="end"
                    aria-label="Channel loop end"
                    :value="channelLoopEnd ?? ''"
                    @input="channelLoopEnd = ($event.target as HTMLInputElement).value === '' ? undefined : Number(($event.target as HTMLInputElement).value)"
                  />
                </div>
              </EditorField>
            </SectionCard>

            <!-- Cost -->
            <SectionCard v-show="activeUnitTab === sectionTab('cost')" title="Cost" :index="sectionIndex('cost')">
              <EditorField label="Resource Cost">
                <!-- Add button intentionally omitted for now — existing costs
                     still show and can be edited/removed. -->
                <div class="ed-list">
                  <p v-if="resourceCostRows.length === 0" class="unit-hint">No resource cost.</p>
                  <div v-for="(row, idx) in resourceCostRows" :key="idx" class="unit-editor__map-row">
                    <input v-model="row.key" placeholder="resource key" :aria-label="`Resource ${idx + 1} key`" />
                    <input v-model.number="row.value" type="number" :aria-label="`Resource ${idx + 1} amount`" />
                    <button type="button" class="unit-editor__row-del" title="Remove" @click="removeResourceCostRow(idx)">✕</button>
                  </div>
                </div>
              </EditorField>
              <div class="unit-editor__pair">
                <EditorField label="Meat Cost" for-id="ue-meat">
                  <input id="ue-meat" v-model.number="form.meatCost" type="number" />
                </EditorField>
                <EditorField label="Spawn Seconds" for-id="ue-spawn">
                  <input id="ue-spawn" v-model.number="form.spawnSeconds" type="number" />
                </EditorField>
              </div>
            </SectionCard>

            <!-- Gating -->
            <SectionCard v-show="activeUnitTab === sectionTab('gating')" title="Gating" :index="sectionIndex('gating')">
              <EditorField label="Requires Buildings">
                <RepeatableList
                  :rows="(form.requiresBuildings ?? []).length"
                  add-label="Add Building"
                  empty-text="No building requirements."
                  @add="addRequiresBuilding"
                >
                  <div
                    v-for="(building, idx) in (form.requiresBuildings ?? [])"
                    :key="idx"
                    class="unit-editor__ability-row"
                  >
                    <FilterableSelect
                      :model-value="building"
                      :options="buildingOptions"
                      placeholder="Select building…"
                      :aria-label="`Required building ${idx + 1}`"
                      @update:model-value="updateRequiresBuildingAt(idx, $event)"
                    />
                    <button
                      type="button"
                      class="unit-editor__row-del"
                      title="Remove"
                      @click="removeRequiresBuildingAt(idx)"
                    >✕</button>
                  </div>
                </RepeatableList>
              </EditorField>
              <EditorField label="Path Chances" hint="(promotion-roll weights)">
                <RepeatableList
                  :rows="pathChancesRows.length"
                  add-label="Add Path Chance"
                  empty-text="No promotion paths — this unit stays base rank."
                  @add="addPathChanceRow"
                >
                  <div v-for="(row, idx) in pathChancesRows" :key="idx" class="unit-editor__map-row">
                    <FilterableSelect
                      :model-value="row.key"
                      :options="pathChanceOptions"
                      placeholder="Select path…"
                      :aria-label="`Path ${idx + 1} key`"
                      @update:model-value="row.key = $event"
                    />
                    <input v-model.number="row.value" type="number" :aria-label="`Path ${idx + 1} weight`" />
                    <button type="button" class="unit-editor__row-del" title="Remove" @click="removePathChanceRow(idx)">✕</button>
                  </div>
                </RepeatableList>
              </EditorField>
            </SectionCard>

            <!-- Rewards -->
            <SectionCard v-show="activeUnitTab === sectionTab('rewards')" title="Death Rewards" :index="sectionIndex('rewards')">
              <div class="unit-editor__pair">
                <EditorField label="DP Drop Chance" hint="(0–1)" for-id="ue-dp-chance">
                  <input id="ue-dp-chance" v-model.number="form.dominionPointDropChance" type="number" />
                </EditorField>
                <EditorField label="DP Amount" for-id="ue-dp-amount">
                  <input id="ue-dp-amount" v-model.number="form.dominionPointAmount" type="number" />
                </EditorField>
                <EditorField label="Spawn Exp" for-id="ue-spawn-exp">
                  <input id="ue-spawn-exp" v-model.number="form.spawnExp" type="number" />
                </EditorField>
                <EditorField label="Experience" for-id="ue-experience">
                  <input id="ue-experience" v-model.number="form.experience" type="number" />
                </EditorField>
              </div>
            </SectionCard>

            <!-- Combat -->
            <SectionCard v-show="activeUnitTab === sectionTab('combat')" title="Combat" :index="sectionIndex('combat')">
              <EditorField label="Combat Profile" hint="(inferred from archetype)" for-id="ue-combat-profile">
                <input id="ue-combat-profile" v-model="form.combatProfile" list="unit-editor-archetypes" />
              </EditorField>
              <EditorField label="Attack Sound Type" for-id="ue-attack-type">
                <select id="ue-attack-type" v-model="form.attackType">
                  <option value="">—</option>
                  <option v-for="t in ATTACK_TYPE_OPTIONS" :key="t.value" :value="t.value">{{ t.label }}</option>
                </select>
              </EditorField>
              <EditorField label="Damage Type" hint="(unspecified = physical)" for-id="ue-damage-type">
                <input id="ue-damage-type" v-model="form.damageType" list="unit-editor-damage-types" />
              </EditorField>
              <EditorField label="Targetable Types" hint="(comma-separated)" for-id="ue-targetable">
                <input
                  id="ue-targetable"
                  :value="(form.targetableTypes ?? []).join(',')"
                  @input="updateStringList('targetableTypes', ($event.target as HTMLInputElement).value)"
                />
              </EditorField>
              <div class="unit-editor__pair">
                <EditorField label="Projectile" for-id="ue-projectile">
                  <input id="ue-projectile" v-model="form.projectile" list="unit-editor-projectiles" />
                </EditorField>
                <EditorField label="Projectile Scale" for-id="ue-projectile-scale">
                  <input id="ue-projectile-scale" v-model.number="form.projectileScale" type="number" />
                </EditorField>
              </div>
            </SectionCard>

            <!-- Stats (spans two columns so it doesn't feel cramped; also holds
                 the gather amounts and the flags that drive capabilities). -->
            <SectionCard v-show="activeUnitTab === sectionTab('stats')" title="Stats" :index="sectionIndex('stats')" class="unit-editor__wide">
              <div class="unit-editor__stats">
                <EditorField label="HP" for-id="ue-hp">
                  <input id="ue-hp" v-model.number="form.hp" type="number" />
                </EditorField>
                <!-- Blank ≠ 0: blank inherits the server default, 0 means never
                     regenerates. Bind :value/@input so a cleared field is
                     undefined, not coerced to 0. -->
                <EditorField label="HP Regen" hint="(/s · blank = default)" for-id="ue-hp-regen">
                  <input
                    id="ue-hp-regen"
                    type="number"
                    step="0.1"
                    :value="form.healthRegenRate ?? ''"
                    @input="form.healthRegenRate = ($event.target as HTMLInputElement).value === '' ? undefined : Number(($event.target as HTMLInputElement).value)"
                  />
                </EditorField>
                <EditorField label="Mana" for-id="ue-mana">
                  <input id="ue-mana" v-model.number="form.maxMana" type="number" />
                </EditorField>
                <EditorField label="Mana Regen" for-id="ue-mana-regen">
                  <input id="ue-mana-regen" v-model.number="form.manaRegenRate" type="number" step="0.1" />
                </EditorField>
                <EditorField label="Armor" for-id="ue-armor">
                  <input id="ue-armor" v-model.number="form.armor" type="number" />
                </EditorField>
                <EditorField label="Damage" for-id="ue-damage">
                  <input id="ue-damage" v-model.number="form.damage" type="number" />
                </EditorField>
                <EditorField label="Attack Range" for-id="ue-range">
                  <input id="ue-range" v-model.number="form.attackRange" type="number" />
                </EditorField>
                <EditorField label="Attack Speed" for-id="ue-atk-speed">
                  <input id="ue-atk-speed" v-model.number="form.attackSpeed" type="number" />
                </EditorField>
                <EditorField label="Splash Radius" for-id="ue-splash">
                  <input id="ue-splash" v-model.number="form.splashRadius" type="number" />
                </EditorField>
                <EditorField label="Move Speed" for-id="ue-move">
                  <input id="ue-move" v-model.number="form.moveSpeed" type="number" />
                </EditorField>
                <EditorField label="Vision Range" for-id="ue-vision">
                  <input id="ue-vision" v-model.number="form.visionRange" type="number" />
                </EditorField>
                <!-- Gather amounts belong to the "Can gather" flag and only show
                     while it's on. -->
                <EditorField v-if="canGather" label="Gold Gather" for-id="ue-gold-gather">
                  <input id="ue-gold-gather" v-model.number="form.goldGatherAmount" type="number" />
                </EditorField>
                <EditorField v-if="canGather" label="Wood Gather" for-id="ue-wood-gather">
                  <input id="ue-wood-gather" v-model.number="form.woodGatherAmount" type="number" />
                </EditorField>
              </div>

              <!-- Flags. They set the unit's capabilities implicitly (see hint). -->
              <div class="unit-editor__flags">
                <label class="ed-check" for="ue-flyer">
                  <input id="ue-flyer" v-model="form.flyer" type="checkbox" /> Flyer
                </label>
                <label class="ed-check" for="ue-noncombat">
                  <input id="ue-noncombat" v-model="form.nonCombat" type="checkbox" /> Non-combat
                </label>
                <label class="ed-check" for="ue-cangather">
                  <input id="ue-cangather" v-model="canGather" type="checkbox" /> Can gather
                </label>
                <label class="ed-check" for="ue-builder">
                  <input id="ue-builder" v-model="builder" type="checkbox" /> Builder
                </label>
              </div>
              <p class="unit-hint">
                Capabilities are assigned automatically — move (has move speed),
                attack (unless non-combat), gather (can gather), build (builder).
              </p>
              <EditorField label="Base Stats" hint="(per-unit base for fieldless stats: crit chance, crit multiplier, lifesteal — fractions are 0–1)">
                <RepeatableList
                  :rows="baseStatRows.length"
                  add-label="Add Base Stat"
                  empty-text="No base stats — this unit uses the global defaults."
                  @add="addBaseStatRow"
                >
                  <div v-for="(row, idx) in baseStatRows" :key="idx" class="unit-editor__map-row">
                    <FilterableSelect
                      :model-value="row.key"
                      :options="baseStatOptions"
                      placeholder="Select stat…"
                      :aria-label="`Base stat ${idx + 1} key`"
                      @update:model-value="row.key = $event"
                    />
                    <input v-model.number="row.value" type="number" step="0.05" :aria-label="`Base stat ${idx + 1} value`" />
                    <button type="button" class="unit-editor__row-del" title="Remove" @click="removeBaseStatRow(idx)">✕</button>
                  </div>
                </RepeatableList>
              </EditorField>
              <EditorField
                label="Ability Stats"
                hint="(applies to EVERY ability this unit casts — pick a broad kind like Duration, or scope it to one action like Zone Duration)"
              >
                <AbilityStatsEditor
                  v-model="abilityStatsModel"
                  :defs="abilityStatDefs"
                  empty-text="No ability stats — this unit's abilities use their authored values."
                />
              </EditorField>
            </SectionCard>

            <!-- Abilities -->
            <SectionCard v-show="activeUnitTab === sectionTab('abilities')" title="Abilities" :index="sectionIndex('abilities')">
              <EditorField label="Abilities">
                <RepeatableList
                  :rows="(form.abilities ?? []).length"
                  add-label="Add Ability"
                  empty-text="No abilities."
                  @add="addAbility"
                >
                  <div
                    v-for="(ability, idx) in (form.abilities ?? [])"
                    :key="idx"
                    class="unit-editor__ability-row"
                  >
                    <FilterableSelect
                      :model-value="ability"
                      :options="abilityOptions"
                      placeholder="Select ability…"
                      :aria-label="`Ability ${idx + 1}`"
                      @update:model-value="updateAbilityAt(idx, $event)"
                    />
                    <button
                      type="button"
                      class="unit-editor__row-del"
                      title="Remove"
                      @click="removeAbilityAt(idx)"
                    >✕</button>
                  </div>
                </RepeatableList>
              </EditorField>
            </SectionCard>

            <!-- Promotion Paths: navigation into the per-path editor. -->
            <!-- Validation: pinned bottom-right, the sign-off for the form. -->
            <SectionCard title="Validation" class="unit-editor__validation">
              <ValidationChecklist :checks="checks" />
              <span class="unit-hint">The server has the final say — it validates every field on save.</span>
            </SectionCard>
        </div>

        <!-- Paths tab: a nested path selector (existing paths + New Path on the
             right), then the selected path's inline editor — a compact action
             bar (id + Save/Delete) over its own sub-tabbed sections. -->
        <div v-else class="unit-editor__paths-tab">
          <p v-if="!selectedType" class="unit-hint">Save this unit before adding promotion paths.</p>
          <template v-else>
            <div class="unit-editor__path-strip" role="tablist" aria-label="Promotion paths">
              <button
                v-for="entry in pathsByUnit[selectedType] ?? []"
                :key="entry.path"
                type="button"
                role="tab"
                class="unit-editor__path-tab"
                :class="{ 'unit-editor__path-tab--active': selectedPath === entry.path }"
                :aria-selected="selectedPath === entry.path"
                @click="selectPath(entry)"
              >{{ entry.path }}</button>
              <button
                type="button"
                class="unit-editor__path-tab unit-editor__path-tab--new"
                :class="{ 'unit-editor__path-tab--active': pathForm !== null && selectedPath === null }"
                @click="startNewPath"
              >+ New Path</button>
            </div>

            <template v-if="pathForm">
              <!-- Compact action bar: id editable only for a brand-new path. -->
              <div class="unit-editor__path-actions">
                <label class="unit-editor__path-id">
                  <span>id</span>
                  <input
                    id="pe-id"
                    :value="pathForm.path"
                    :disabled="selectedPath !== null"
                    placeholder="new_path_id"
                    aria-label="Path id"
                    @input="onPathIdInput(($event.target as HTMLInputElement).value)"
                  />
                </label>
                <UiButton size="sm" variant="active" :disabled="busy || !pathForm.parentUnit || !pathForm.path" @click="savePath">Save Path</UiButton>
                <UiButton v-if="selectedPath !== null" size="sm" variant="secondary" :disabled="busy" @click="removePath">Delete Path</UiButton>
                <span v-if="saveError" class="unit-error unit-editor__path-error">{{ saveError }}</span>
              </div>

              <EditorTabs
                v-model="activePathTab"
                :tabs="PATH_TABS"
                id-prefix="unit-path"
                label="Path sections"
                class="unit-editor__tabs"
              />

              <div class="unit-editor__grid">
                <!-- Identity -->
                <SectionCard v-show="activePathTab === pathSectionTab('identity')" title="Identity" :index="pathSectionIndex('identity')">
              <EditorField label="Parent Unit" for-id="pe-parent">
                <input id="pe-parent" :value="pathForm.parentUnit" type="text" disabled />
              </EditorField>
              <EditorField label="Description" for-id="pe-description">
                <input id="pe-description" v-model="pathForm.description" type="text" />
              </EditorField>
            </SectionCard>

            <!-- Preview -->
            <SectionCard v-show="activePathTab === pathSectionTab('preview')" title="Preview" :index="pathSectionIndex('preview')" class="unit-editor__wide">
              <UnitSpritePreview
                ref="preview"
                :path-key="pathForm.path"
                :unit-key="pathForm.parentUnit"
                :projectile="pathForm.projectile"
                :projectile-scale="pathForm.projectileScale"
                :channel-loop="pathForm.channelLoop"
                :channel-ability="channelAbility"
                v-model:attack-origin="pathForm.attackOrigin"
              />
              <div class="unit-art" @dragover.prevent @drop.prevent="onFolderDropped">
                <label v-if="canIngestArt" class="unit-art__drop">
                  <input
                    type="file"
                    webkitdirectory
                    multiple
                    :disabled="ingesting"
                    @change="onFolderInputChanged(($event.target as HTMLInputElement).files)"
                  />
                  {{ ingesting ? 'Packing…' : 'Drop / choose a PixelLab export folder' }}
                </label>
                <p v-else class="unit-hint">Set the path's id to ingest art.</p>

                <div v-if="pendingArt" class="unit-art__actions">
                  <UiButton size="sm" variant="active" :disabled="busy" @click="saveArt">Save Art</UiButton>
                  <UiButton size="sm" variant="secondary" :disabled="busy" @click="discardPendingArt">Discard</UiButton>
                </div>

                <p v-for="w in ingestWarnings" :key="w" class="unit-warn">{{ w }}</p>
                <p v-if="ingestError" class="unit-error">{{ ingestError }}</p>
              </div>

              <!-- Channel Loop tunes the casting animation shown above (start/end
                   frames to loop). Blank both to unset. -->
              <EditorField label="Channel Loop" hint="(casting frames · blank = unset)">
                <div class="unit-editor__pair">
                  <input
                    type="number"
                    placeholder="start"
                    aria-label="Channel loop start"
                    :value="pathChannelLoopStart ?? ''"
                    @input="pathChannelLoopStart = ($event.target as HTMLInputElement).value === '' ? undefined : Number(($event.target as HTMLInputElement).value)"
                  />
                  <input
                    type="number"
                    placeholder="end"
                    aria-label="Channel loop end"
                    :value="pathChannelLoopEnd ?? ''"
                    @input="pathChannelLoopEnd = ($event.target as HTMLInputElement).value === '' ? undefined : Number(($event.target as HTMLInputElement).value)"
                  />
                </div>
              </EditorField>
            </SectionCard>

            <!-- Combat: all overrides, blank = inherit the base unit. -->
            <SectionCard v-show="activePathTab === pathSectionTab('combat')" title="Combat" :index="pathSectionIndex('combat')" class="unit-editor__combat-main">
              <div class="unit-editor__pair">
                <EditorField label="Projectile" for-id="pe-projectile">
                  <input id="pe-projectile" v-model="pathForm.projectile" list="unit-editor-projectiles" placeholder="(no override)" />
                </EditorField>
                <EditorField label="Projectile Scale" for-id="pe-projectile-scale">
                  <input id="pe-projectile-scale" v-model.number="pathForm.projectileScale" type="number" />
                </EditorField>
              </div>
              <EditorField label="Attack Sound Type" for-id="pe-attack-type">
                <select id="pe-attack-type" v-model="pathForm.attackType">
                  <option value="">(no override)</option>
                  <option v-for="t in ATTACK_TYPE_OPTIONS" :key="t.value" :value="t.value">{{ t.label }}</option>
                </select>
              </EditorField>
              <EditorField label="Damage Type" for-id="pe-damage-type">
                <input id="pe-damage-type" v-model="pathForm.damageType" list="unit-editor-damage-types" placeholder="(no override)" />
              </EditorField>
            </SectionCard>

            <!-- Abilities: REPLACES the base unit's list entirely. -->
            <SectionCard v-show="activePathTab === pathSectionTab('abilities')" title="Abilities" :index="pathSectionIndex('abilities')" class="unit-editor__combat-sub">
              <EditorField label="Abilities" hint="(replaces base)">
                <RepeatableList
                  :rows="(pathForm.abilities ?? []).length"
                  add-label="Add Ability"
                  empty-text="No abilities."
                  @add="addPathAbility"
                >
                  <div
                    v-for="(ability, idx) in (pathForm.abilities ?? [])"
                    :key="idx"
                    class="unit-editor__ability-row"
                  >
                    <FilterableSelect
                      :model-value="ability"
                      :options="abilityOptions"
                      placeholder="Select ability…"
                      :aria-label="`Ability ${idx + 1}`"
                      @update:model-value="updatePathAbilityAt(idx, $event)"
                    />
                    <button
                      type="button"
                      class="unit-editor__row-del"
                      title="Remove"
                      @click="removePathAbilityAt(idx)"
                    >✕</button>
                  </div>
                </RepeatableList>
                <span class="unit-hint">Replaces the base unit's abilities entirely — not additive.</span>
              </EditorField>
            </SectionCard>

            <!-- Ranks -->
            <SectionCard v-show="activePathTab === pathSectionTab('ranks')" title="Rank Stats" :index="pathSectionIndex('ranks')" variant="worldMenu" class="unit-editor__combat-ranks">
              <!-- The rank grid is wide (a column per stat); let it scroll rather
                   than squeeze/clip when the card is narrower than the table. -->
              <div class="unit-editor__rank-scroll">
                <PathRankGrid
                  :base-stats="parentBaseStats"
                  :ranks="pathForm.ranks || {}"
                  @update:ranks="onPathRanksUpdate"
                />
              </div>
            </SectionCard>

            <!-- Rank Slots: per-rank choice between a Perk slot (explicit
                 grants of standalone perk defs, union'd server-side with a
                 path's own perk pools) and an Ability slot (one ability
                 rolled from abilityPoolsByRank). A rank is one or the other,
                 never both. -->
            <div v-show="activePathTab === pathSectionTab('perks')" class="unit-editor__rank-slot-stack">
              <SectionCard
                v-for="(rank, i) in PERK_RANK_ORDER"
                :key="rank"
                :title="rank.charAt(0).toUpperCase() + rank.slice(1)"
                :index="i + 1"
              >
                <div class="unit-editor__slot-type" :data-test="`slot-type-${rank}`">
                  <label class="unit-editor__slot-type-option">
                    <input
                      type="radio"
                      :name="`slot-type-${rank}`"
                      value="perk"
                      :checked="rankSlotType(rank) === 'perk'"
                      @change="setRankSlotType(rank, 'perk')"
                    /> Perk slot
                  </label>
                  <label class="unit-editor__slot-type-option">
                    <input
                      type="radio"
                      :name="`slot-type-${rank}`"
                      value="ability"
                      :checked="rankSlotType(rank) === 'ability'"
                      @change="setRankSlotType(rank, 'ability')"
                    /> Ability slot
                  </label>
                </div>

                <div v-if="rankSlotType(rank) === 'perk'" :key="`${rank}-perk`">
                  <ul class="unit-editor__perk-list">
                    <li v-for="perkId in perksForRank(rank)" :key="perkId" class="unit-editor__perk-row">
                      <span class="unit-editor__perk-name">{{ perkLabel(perkId) }}</span>
                      <span v-if="isPerkInert(perkId)" class="unit-hint">(inert — not wired)</span>
                      <button
                        type="button"
                        class="unit-editor__row-del"
                        :title="`Remove ${perkId}`"
                        @click="removePerkFromRank(rank, perkId)"
                      >✕</button>
                    </li>
                    <li v-if="perksForRank(rank).length === 0" class="unit-hint">No perks referenced.</li>
                  </ul>
                  <select
                    class="unit-editor__perk-add"
                    :aria-label="`Add perk to ${rank}`"
                    value=""
                    @change="onPerkAddChange(rank, $event)"
                  >
                    <option value="">Add perk…</option>
                    <option v-for="opt in availablePerkOptionsForRank(rank)" :key="opt.id" :value="opt.id">{{ opt.label }}</option>
                  </select>
                </div>

                <div v-else :key="`${rank}-ability`">
                  <ul class="unit-editor__perk-list">
                    <li v-for="abilityId in abilitiesForRank(rank)" :key="abilityId" class="unit-editor__perk-row">
                      <span class="unit-editor__perk-name">{{ abilityLabel(abilityId) }}</span>
                      <button
                        type="button"
                        class="unit-editor__row-del"
                        :title="`Remove ${abilityId}`"
                        @click="removeAbilityFromRank(rank, abilityId)"
                      >✕</button>
                    </li>
                    <li v-if="abilitiesForRank(rank).length === 0" class="unit-hint">No abilities in pool.</li>
                  </ul>
                  <select
                    class="unit-editor__perk-add"
                    :aria-label="`Add ability to ${rank}`"
                    :data-test="`ability-add-${rank}`"
                    value=""
                    @change="onAbilityAddChange(rank, $event)"
                  >
                    <option value="">Add ability…</option>
                    <option v-for="opt in availableAbilityOptionsForRank(rank)" :key="opt.id" :value="opt.id">{{ opt.label }}</option>
                  </select>
                </div>
              </SectionCard>
            </div>

            <SectionCard
              v-if="selectedPath === null"
              v-show="activePathTab === pathSectionTab('membership')"
              title="Membership"
              :index="pathSectionIndex('membership')"
            >
              <label class="ed-check" for="pe-add-chances">
                <input id="pe-add-chances" v-model="addPathToPathChances" type="checkbox" />
                Also add to {{ pathForm.parentUnit }}'s promotion paths (weight 1)
              </label>
            </SectionCard>
              </div>
            </template>
            <p v-else class="unit-hint">Select a path, or add a new one.</p>
          </template>
        </div>
      </GameScrollArea>
      </template>
    </template>
  </EditorShell>

  <!-- Shared by both Archetype (Identity) and Combat Profile (Combat): one
       combat-profile key set backs both fields. Mounted at the root so it can't
       be torn down with a collapsed section. -->
  <datalist id="unit-editor-archetypes">
    <option v-for="a in archetypes" :key="a" :value="a" />
  </datalist>
  <!-- Projectile / damage-type suggestion lists live at the root too, so the
       promotion-path Combat fields (rendered when the unit grid is swapped out
       for the Paths tab) still resolve their `list=` targets. -->
  <datalist id="unit-editor-projectiles">
    <option v-for="p in projectileIds" :key="p" :value="p" />
  </datalist>
  <datalist id="unit-editor-damage-types">
    <option v-for="d in damageTypes" :key="d" :value="d" />
  </datalist>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  createBlankForm, formFromDef, saveRequestFromForm, pickTemplateStats,
  type AuthoredUnitDef, type UnitEditorForm,
} from '@/game/units/unitEditorForm'
import {
  fetchAuthoredUnitDefs, saveEditorUnit, deleteEditorUnit, EditorValidationError,
} from '@/game/units/unitEditorApi'
import { createBlankPathForm, pathFormFromDef, saveRequestFromPathForm, type PathEditorForm, type PathRankStats } from '@/game/units/pathEditorForm'
import {
  fetchPaths,
  savePath as savePathApi, deletePath as deletePathApi,
  type EditorPathEntry,
} from '@/game/units/pathEditorApi'
import PathRankGrid from '@/components/PathRankGrid.vue'
import {
  fetchFactions, saveFaction, deleteFaction, fetchArchetypes,
  fetchProjectileIds, fetchDamageTypes, fetchBuildingIds,
  saveUnitArt, type FactionDef, type UnitArtUploadFile,
} from '@/game/units/editorCatalogApi'
import { fetchAuthoredAbilityDefs } from '@/game/abilities/abilityEditorApi'
import type { AuthoredAbilityDef } from '@/game/abilities/abilityEditorForm'
import { fetchAuthoredPerkDefs } from '@/game/perks/perkEditorApi'
import type { AuthoredPerkDef } from '@/game/perks/perkEditorForm'
import { baseAuthorableStatDefs } from '@/game/stats/statRegistry'
import { confirmDelete } from '@/components/editor/confirmDelete'
import AbilityStatsEditor, { type AbilityStatDef } from './AbilityStatsEditor.vue'
import { pickChannelAbility } from '@/game/units/channelPreview'
import {
  ingestExportFolder, packedSheetToObjectUrls, blobToBase64,
  type DroppedFile, type IngestResult,
} from '@/game/units/spriteIngest'
import {
  buildSpriteSet, registerRuntimeSpriteSet, loadRuntimeSpriteSets, getUnitPortraitUrl, type SpriteManifest,
} from '@/game/rendering/unitSprites'
import UnitSpritePreview from '@/components/UnitSpritePreview.vue'
import UiButton from '@/components/ui/UiButton.vue'
import GameScrollArea from '@/components/ui/GameScrollArea.vue'
import EditorShell from '@/components/editor/EditorShell.vue'
import EditorSidebar from '@/components/editor/EditorSidebar.vue'
import type { SidebarGroup } from '@/components/editor/EditorSidebar.vue'
import EditorHeader from '@/components/editor/EditorHeader.vue'
import EditorField from '@/components/editor/EditorField.vue'
import SectionCard from '@/components/editor/SectionCard.vue'
import EditorTabs from '@/components/editor/EditorTabs.vue'
import RepeatableList from '@/components/editor/RepeatableList.vue'
import FilterableSelect from '@/components/editor/FilterableSelect.vue'
import type { FilterableOption } from '@/components/editor/FilterableSelect.vue'
import ValidationChecklist from '@/components/editor/ValidationChecklist.vue'
import type { ValidationCheck } from '@/components/editor/ValidationChecklist.vue'
import IconPreview from '@/components/editor/IconPreview.vue'

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

// Sidebar search over the unit list (name / type).
const search = ref('')

// Server-sourced catalogs backing the selects. Free-text fallbacks stay
// available (datalist, not select) because the SERVER is the validator — a
// stale dropdown must never block a legal value.
const factions = ref<FactionDef[]>([])
const archetypes = ref<string[]>([])
const projectileIds = ref<string[]>([])
// Full ability defs (not just ids) so the preview can auto-detect whether the
// selected unit channels and at what tick cadence. Keyed for O(1) lookup.
const abilityDefs = ref<AuthoredAbilityDef[]>([])
const abilityDefsById = computed(() => {
  const map = new Map<string, AuthoredAbilityDef>()
  for (const d of abilityDefs.value) map.set(d.id, d)
  return map
})
const damageTypes = ref<string[]>([])
const buildingIds = ref<string[]>([])
// Standalone perk catalog (world-editor Perks screen) — backs the Perk
// References picker on a path's Combat tab. Keyed for O(1) label/wired
// lookup, same idiom as abilityDefs/abilityDefsById above.
const perkDefs = ref<AuthoredPerkDef[]>([])
const perkDefsById = computed(() => {
  const map = new Map<string, AuthoredPerkDef>()
  for (const d of perkDefs.value) map.set(d.id, d)
  return map
})

const factionFilter = ref<string>('')   // seeds a new unit's faction
const newFactionId = ref('')
const newFactionName = ref('')
const showNewFaction = ref(false)
const factionToDelete = ref('')
const factionError = ref('')

// --- Path/perk authoring mode: base-unit-vs-path editor mode. `paths` is the
// full merged catalog (fetchPaths, GET /catalog/paths); pathsByUnit re-keys it
// by owning unit type for the promotion-paths nav card.
const paths = ref<EditorPathEntry[]>([])
const pathsByUnit = computed(() => {
  const map: Record<string, EditorPathEntry[]> = {}
  for (const entry of paths.value) {
    (map[entry.unit] ??= []).push(entry)
  }
  return map
})

// Starts 'empty' so entering the Unit Types tab shows a blank screen rather
// than springing an unsaved new-unit form — the form appears only once a unit
// or path is chosen (or "Add New Unit" is clicked).
// Only 'empty' (nothing selected) or 'unit' (a unit is open). Promotion paths
// are no longer a separate mode — they edit inline inside the unit's Paths tab.
const editorMode = ref<'empty' | 'unit'>('empty')
const selectedPath = ref<string | null>(null)
const selectedPathParent = ref<string | null>(null)
const pathForm = ref<PathEditorForm | null>(null)

function onPathRanksUpdate(next: Record<string, PathRankStats>) {
  if (pathForm.value) pathForm.value.ranks = next
}

// Perk References: per-rank list of standalone perk ids explicitly granted by
// this path. Fixed row order regardless of which rank keys are actually
// present on perksByRank, mirroring PathRankGrid's RANK_ORDER idiom.
const PERK_RANK_ORDER = ['bronze', 'silver', 'gold'] as const

function perksForRank(rank: string): string[] {
  return pathForm.value?.perksByRank?.[rank] ?? []
}

function perkLabel(id: string): string {
  return perkDefsById.value.get(id)?.displayName || id
}

// Mirrors PerkEditorPanel's `!p.wired` convention (undefined counts as
// inert, same as an explicit false) rather than a strict `=== false` check.
function isPerkInert(id: string): boolean {
  return !perkDefsById.value.get(id)?.wired
}

// Catalog perks eligible for THIS path's picker: association matches the path
// being edited, or the perk is generic (empty association). Already-referenced
// perks at this rank are excluded. Sorted alphabetically — mirrors abilityOptions.
function availablePerkOptionsForRank(rank: string): FilterableOption[] {
  const taken = new Set(perksForRank(rank))
  const activePath = pathForm.value?.path ?? ''
  return perkDefs.value
    .filter((d) => !taken.has(d.id))
    .filter((d) => !d.path || d.path === activePath) // generic OR associated
    .map((d) => ({ id: d.id, label: d.displayName || d.id }))
    .sort((a, b) => a.label.localeCompare(b.label))
}

function addPerkToRank(rank: string, id: string) {
  if (!pathForm.value || !id) return
  const byRank = { ...(pathForm.value.perksByRank ?? {}) }
  const current = byRank[rank] ?? []
  if (current.includes(id)) return
  byRank[rank] = [...current, id]
  pathForm.value.perksByRank = byRank
}

function removePerkFromRank(rank: string, id: string) {
  if (!pathForm.value?.perksByRank) return
  const byRank = { ...pathForm.value.perksByRank }
  byRank[rank] = (byRank[rank] ?? []).filter((p) => p !== id)
  pathForm.value.perksByRank = byRank
}

// The add <select> is not v-modeled to a persistent ref — reset it to the
// placeholder after every pick so the same option can be re-added post-remove
// without a stale selected value lingering.
function onPerkAddChange(rank: string, event: Event) {
  const select = event.target as HTMLSelectElement
  addPerkToRank(rank, select.value)
  select.value = ''
}

// Rank slot type: a rank is an Ability slot when its key is PRESENT on
// abilityPoolsByRank (even with an empty array) — key presence, not array
// length, is the discriminator, mirroring how perksByRank ranks are keyed by
// presence in the rank grid. Absent => Perk slot (the pre-existing default).
function rankSlotType(rank: string): 'perk' | 'ability' {
  return pathForm.value?.abilityPoolsByRank?.[rank] !== undefined ? 'ability' : 'perk'
}

// Stashes the list from the slot a rank is LEAVING so toggling back restores
// it (ability→perk→ability brings the abilities back, and vice versa) instead
// of wiping them. Session-local: only the ACTIVE slot is persisted on save, so
// the stashed inactive list is not restored across a full editor reload.
const rankSlotStash = ref<Record<string, { perks: string[]; abilities: string[] }>>({})

// Switching a rank's slot type: a rank is one or the other, never both, so the
// PERSISTED data only ever carries the active slot's list. But the inactive
// slot's list is stashed (above) and restored on switch-back, so experimenting
// with the other slot type never destroys what you'd populated.
function setRankSlotType(rank: string, type: 'perk' | 'ability') {
  if (!pathForm.value) return
  const stash = { ...(rankSlotStash.value[rank] ?? { perks: [], abilities: [] }) }
  if (type === 'ability') {
    // Stash the perks we're leaving; restore any previously-stashed abilities.
    const curPerks = pathForm.value.perksByRank?.[rank]
    if (curPerks !== undefined) stash.perks = [...curPerks]
    if (pathForm.value.perksByRank?.[rank] !== undefined) {
      const perks = { ...pathForm.value.perksByRank }
      delete perks[rank]
      pathForm.value.perksByRank = perks
    }
    const pools = { ...(pathForm.value.abilityPoolsByRank ?? {}) }
    pools[rank] = [...stash.abilities]
    pathForm.value.abilityPoolsByRank = pools
  } else {
    // Stash the abilities we're leaving; restore any previously-stashed perks.
    const curAbilities = pathForm.value.abilityPoolsByRank?.[rank]
    if (curAbilities !== undefined) stash.abilities = [...curAbilities]
    if (pathForm.value.abilityPoolsByRank?.[rank] !== undefined) {
      const pools = { ...pathForm.value.abilityPoolsByRank }
      delete pools[rank]
      pathForm.value.abilityPoolsByRank = pools
    }
    if (stash.perks.length) {
      const perks = { ...(pathForm.value.perksByRank ?? {}) }
      perks[rank] = [...stash.perks]
      pathForm.value.perksByRank = perks
    }
  }
  rankSlotStash.value = { ...rankSlotStash.value, [rank]: stash }
}

// Ability Pool: per-rank list of ability ids to roll a random grant from,
// mirroring the Perk References helpers above. Dedup rule (matches the
// server): exclude the path's always-granted base abilities and ids already
// in THIS rank's pool. Do NOT exclude other ranks' pools — the same ability
// may legitimately appear in multiple ranks' pools (e.g. shared bronze+silver
// pools); the runtime de-dupes the actual grant per-unit.
function abilitiesForRank(rank: string): string[] {
  return pathForm.value?.abilityPoolsByRank?.[rank] ?? []
}

function abilityLabel(id: string): string {
  return abilityDefsById.value.get(id)?.displayName || id
}

function availableAbilityOptionsForRank(rank: string): FilterableOption[] {
  const taken = new Set<string>([...(pathForm.value?.abilities ?? []), ...abilitiesForRank(rank)])
  return abilityDefs.value
    .filter((d) => !taken.has(d.id))
    .map((d) => ({ id: d.id, label: d.displayName || d.id }))
    .sort((a, b) => a.label.localeCompare(b.label))
}

function addAbilityToRank(rank: string, id: string) {
  if (!pathForm.value || !id) return
  const pools = { ...(pathForm.value.abilityPoolsByRank ?? {}) }
  const current = pools[rank] ?? []
  if (current.includes(id)) return
  pools[rank] = [...current, id]
  pathForm.value.abilityPoolsByRank = pools
}

function removeAbilityFromRank(rank: string, id: string) {
  if (!pathForm.value?.abilityPoolsByRank) return
  const pools = { ...pathForm.value.abilityPoolsByRank }
  pools[rank] = (pools[rank] ?? []).filter((x) => x !== id)
  pathForm.value.abilityPoolsByRank = pools
}

// Same reset-after-pick idiom as onPerkAddChange.
function onAbilityAddChange(rank: string, event: Event) {
  const select = event.target as HTMLSelectElement
  addAbilityToRank(rank, select.value)
  select.value = ''
}

// The rank grid resolves multiplier cells against the PARENT unit's base
// stats — find it in the already-loaded unit list by pathForm.parentUnit.
const parentBaseStats = computed<Record<string, number | undefined>>(() => {
  const found = units.value.find((u) => u.type === pathForm.value?.parentUnit)
  return (found as unknown as Record<string, number | undefined> | undefined) ?? {}
})

// channelLoop bridging for the path form — same {start,end}-vs-undefined
// pattern as the unit form's channelLoopStart/channelLoopEnd below.
const pathChannelLoopStart = ref<number | undefined>(undefined)
const pathChannelLoopEnd = ref<number | undefined>(undefined)
watch([pathChannelLoopStart, pathChannelLoopEnd], ([s, e]) => {
  if (!pathForm.value) return
  pathForm.value.channelLoop = (s === undefined && e === undefined) ? undefined : { start: s ?? 0, end: e ?? 0 }
})

// Attack Type is the melee attack-sound key resolved server-side (swing/stab
// emit that sound on the swing). "ranged" is the projectile archetype — melee
// units alone emit the sound (state_combat gates on a melee profile), so it is
// an inert label on ranged units, whose hits render from the projectile diff.
const ATTACK_TYPE_OPTIONS = [
  { value: 'ranged', label: 'Ranged (projectile)' },
  { value: 'swing', label: 'Swing' },
  { value: 'stab', label: 'Stab' },
] as const

// Ability picker options: authored ability defs as {id, label}, label being the
// human displayName (falling back to the id). Sorted by label so the dropdown
// reads alphabetically.
const abilityOptions = computed<FilterableOption[]>(() =>
  abilityDefs.value
    .map((d) => ({ id: d.id, label: d.displayName || d.id }))
    .sort((a, b) => a.label.localeCompare(b.label)),
)

// Abilities are edited as a list of dropdown rows (RepeatableList idiom, like
// resource cost). Add appends a blank row; the blank id shows the placeholder
// until the author picks one, and empty rows are dropped on save().
function addAbility() {
  form.value.abilities = [...(form.value.abilities ?? []), '']
}
function updateAbilityAt(idx: number, id: string) {
  const next = [...(form.value.abilities ?? [])]
  next[idx] = id
  form.value.abilities = next
}
function removeAbilityAt(idx: number) {
  const next = [...(form.value.abilities ?? [])]
  next.splice(idx, 1)
  form.value.abilities = next
}

// abilities on a path REPLACE the base unit's list entirely (not additive) —
// same dropdown-row editing as the base unit, against pathForm.
function addPathAbility() {
  if (!pathForm.value) return
  pathForm.value.abilities = [...(pathForm.value.abilities ?? []), '']
}
function updatePathAbilityAt(idx: number, id: string) {
  if (!pathForm.value) return
  const next = [...(pathForm.value.abilities ?? [])]
  next[idx] = id
  pathForm.value.abilities = next
}
function removePathAbilityAt(idx: number) {
  if (!pathForm.value) return
  const next = [...(pathForm.value.abilities ?? [])]
  next.splice(idx, 1)
  pathForm.value.abilities = next
}

// Building picker options for the "Requires Buildings" gate. Buildings have no
// separate display name here, so the id is the label. Sorted alphabetically.
const buildingOptions = computed<FilterableOption[]>(() =>
  buildingIds.value
    .map((id) => ({ id, label: id }))
    .sort((a, b) => a.label.localeCompare(b.label)),
)

// requiresBuildings is edited with the same dropdown-row idiom as abilities:
// add appends a blank row, empty rows are dropped on save().
function addRequiresBuilding() {
  form.value.requiresBuildings = [...(form.value.requiresBuildings ?? []), '']
}
function updateRequiresBuildingAt(idx: number, id: string) {
  const next = [...(form.value.requiresBuildings ?? [])]
  next[idx] = id
  form.value.requiresBuildings = next
}
function removeRequiresBuildingAt(idx: number) {
  const next = [...(form.value.requiresBuildings ?? [])]
  next.splice(idx, 1)
  form.value.requiresBuildings = next
}

// The path id is the primary key once saved (owns art dir), so it
// locks the same way a unit's `type` does — editable only while a brand-new,
// not-yet-saved path (selectedPath === null).
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
  saveError.value = ''
  selectedPath.value = entry.path
  selectedPathParent.value = entry.unit
  pathForm.value = pathFormFromDef(entry.def, entry.unit)
  activePathTab.value = PATH_TABS[0].id
  pathChannelLoopStart.value = pathForm.value.channelLoop?.start
  pathChannelLoopEnd.value = pathForm.value.channelLoop?.end
  preview.value?.refresh()
}

// "Also add to {parent}'s promotion paths" — only meaningful (and only shown)
// for a brand-new, not-yet-saved path; defaults CHECKED.
const addPathToPathChances = ref(true)

// New-path parent, set by the Promotion Paths card before confirmNewPath().
const newPathParentUnit = ref('')

// From the base unit's Promotion Paths card: the parent is always the unit
// currently open (guaranteed saved — the card gates on selectedType).
function startNewPath() {
  if (!selectedType.value) return
  newPathParentUnit.value = selectedType.value
  confirmNewPath()
}

function confirmNewPath() {
  if (!newPathParentUnit.value) return
  // Same abandon-pending-art hazard as newUnit — see selectUnit's doc comment.
  const hadPending = pendingArt.value !== null
  clearPending()
  ingestError.value = ''
  if (hadPending) void flushPreviewOverlay()
  saveError.value = ''
  pathForm.value = createBlankPathForm(newPathParentUnit.value)
  selectedPath.value = null
  selectedPathParent.value = newPathParentUnit.value
  activePathTab.value = PATH_TABS[0].id
  pathChannelLoopStart.value = undefined
  pathChannelLoopEnd.value = undefined
  addPathToPathChances.value = true
  preview.value?.refresh()
}

// ── Sidebar ─────────────────────────────────────────────────────────────────

function factionName(id: string): string {
  return factions.value.find((f) => f.id === id)?.displayName ?? id
}

const filteredUnits = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return units.value
  return units.value.filter((u) =>
    u.type.toLowerCase().includes(q) || (u.name ?? '').toLowerCase().includes(q))
})

// Units grouped by faction — one group per faction record, plus a trailing
// "Unassigned" group for any unit whose faction has no record (defensive).
const sidebarGroups = computed<SidebarGroup[]>(() => {
  const groups: SidebarGroup[] = factions.value.map((f) => ({
    label: f.displayName,
    entries: filteredUnits.value
      .filter((u) => u.faction === f.id)
      .map((u) => ({
        id: u.type,
        name: u.name || u.type,
        iconUrl: getUnitPortraitUrl(undefined, u.type) ?? undefined,
      })),
  }))
  const known = new Set(factions.value.map((f) => f.id))
  const orphans = filteredUnits.value.filter((u) => !known.has(u.faction))
  if (orphans.length) {
    groups.push({
      label: 'Unassigned',
      entries: orphans.map((u) => ({ id: u.type, name: u.name || u.type })),
    })
  }
  return groups.filter((g) => g.entries.length > 0)
})

const sidebarSelectedId = computed(() => selectedType.value ?? '')

function selectUnitById(id: string) {
  const def = units.value.find((u) => u.type === id)
  if (def) selectUnit(def)
}

// Clone the selected unit as a brand-new, unsaved def — the id clears so the
// author gives the copy its own, and the Name gets a "Copy" suffix.
function duplicateUnit(id: string) {
  const def = units.value.find((u) => u.type === id)
  if (!def) return
  const hadPending = pendingArt.value !== null
  clearPending()
  ingestError.value = ''
  if (hadPending) void flushPreviewOverlay()
  editorMode.value = 'unit'
  selectedPath.value = null
  selectedPathParent.value = null
  pathForm.value = null
  clearPortraitOverride()
  const copy = formFromDef(def)
  canGather.value = (def.capabilities ?? []).includes('gather')
  builder.value = (def.capabilities ?? []).includes('build')
  copy.type = ''
  copy.name = `${def.name || def.type} Copy`
  form.value = copy
  selectedType.value = null
  typeIdManuallyEdited.value = false
  saveError.value = ''
  resourceCostRows.value = rowsFromMap(def.resourceCost)
  pathChancesRows.value = rowsFromMap(def.pathChances)
  baseStatRows.value = rowsFromMap(def.baseStats)
  channelLoopStart.value = def.channelLoop?.start
  channelLoopEnd.value = def.channelLoop?.end
  lastSavedAt.value = null
  preview.value?.refresh()
}

// ── Header ──────────────────────────────────────────────────────────────────

const unitBreadcrumb = computed(() => {
  const parts: string[] = []
  if (form.value.archetype) parts.push(form.value.archetype)
  if (form.value.faction) parts.push(factionName(form.value.faction))
  return parts.join(' • ')
})
const unitFilePath = computed(() => {
  if (!form.value.type || !form.value.faction) return ''
  return `server/internal/game/catalog/units/${form.value.faction}/${form.value.type}/${form.value.type}.json`
})
const unitSaveDisabled = computed(() => busy.value || !form.value.type || !form.value.faction)

// "Last saved" is session-only, starting at the moment Save succeeds.
const lastSavedAt = ref<number | null>(null)
const nowTs = ref(Date.now())
let clock: ReturnType<typeof setInterval> | null = null
const savedLabel = computed(() => {
  if (lastSavedAt.value === null) return ''
  const secs = Math.max(0, Math.round((nowTs.value - lastSavedAt.value) / 1000))
  if (secs < 45) return 'just now'
  const mins = Math.round(secs / 60)
  if (mins < 60) return `${mins} min ago`
  return `${Math.round(mins / 60)} hr ago`
})

// ── Sections & tabs ──────────────────────────────────────────────────────────
// The base-unit form is split into tabs to group related sections. Each tab
// owns an ordered list of section keys; the card's leading number is its
// position WITHIN its tab (so every tab reads 1., 2., 3. …). Card DOM order
// matches these lists, so no card is ever numbered out of visual order.
const UNIT_TABS: { id: string; label: string; sections: string[] }[] = [
  { id: 'identity', label: 'Identity', sections: ['identity', 'preview'] },
  { id: 'configuration', label: 'Configuration', sections: ['cost', 'gating', 'rewards'] },
  { id: 'combat', label: 'Combat', sections: ['combat', 'stats', 'abilities'] },
  { id: 'paths', label: 'Promotion Paths', sections: ['paths'] },
]
const activeUnitTab = ref<string>(UNIT_TABS[0].id)

// The tab a given section lives on — used to v-show its card.
function sectionTab(key: string): string {
  return UNIT_TABS.find((t) => t.sections.includes(key))?.id ?? ''
}
// 1-based index of a section within its own tab (drives the card's number).
function sectionIndex(key: string): number {
  const tab = UNIT_TABS.find((t) => t.sections.includes(key))
  return tab ? tab.sections.indexOf(key) + 1 : 0
}

// The promotion-path editor now lives INSIDE the unit's Promotion Paths tab
// (no separate page). It has its own inner tab strip over the same sections,
// grouped and numbered exactly like the unit tabs above.
//
// The former 'perks' tab (Perk Pools authoring) was retired — perks are now
// standalone defs edited in the world-editor Perks screen — and its only
// other content, the new-path-only Membership checkbox, was folded into the
// Identity tab so it stays reachable without a dedicated (now-empty) tab.
const PATH_TABS: { id: string; label: string; sections: string[] }[] = [
  { id: 'identity', label: 'Identity', sections: ['identity', 'preview', 'membership'] },
  { id: 'combat', label: 'Combat', sections: ['combat', 'abilities', 'ranks'] },
  { id: 'rankSlots', label: 'Rank Slots', sections: ['perks'] },
]
const activePathTab = ref<string>(PATH_TABS[0].id)
function pathSectionTab(key: string): string {
  return PATH_TABS.find((t) => t.sections.includes(key))?.id ?? ''
}
function pathSectionIndex(key: string): number {
  const tab = PATH_TABS.find((t) => t.sections.includes(key))
  return tab ? tab.sections.indexOf(key) + 1 : 0
}

// True while a promotion path is open for editing in the Paths tab — the mode
// signal that used to be `editorMode === 'path'`. Art ingest and the channel
// preview target the path (not the base unit) when this is set.
const editingPath = computed(() => activeUnitTab.value === 'paths' && pathForm.value !== null)

// Entering the Paths tab (or switching units while it's active) auto-opens the
// first existing path so the pane isn't just a bare strip — unless the author
// is already mid-edit on a path (pathForm set).
watch([activeUnitTab, selectedType], () => {
  if (activeUnitTab.value !== 'paths' || !selectedType.value || pathForm.value) return
  const list = pathsByUnit.value[selectedType.value] ?? []
  if (list.length) selectPath(list[0])
})

// ── Validation (advisory; the server is authoritative) ───────────────────────
// Invariants only — no balance numbers — mirroring validateUnitDef's floors.
const checks = computed<ValidationCheck[]>(() => {
  const f = form.value
  const list: ValidationCheck[] = [
    { ok: !!f.type, message: f.type ? 'ID is set.' : 'ID is required.' },
    { ok: !!f.faction, message: f.faction ? 'Faction is set.' : 'Faction is required.' },
    { ok: (f.hp ?? 0) > 0, message: (f.hp ?? 0) > 0 ? 'HP is above zero.' : 'HP must be above zero.' },
    { ok: (f.moveSpeed ?? 0) > 0, message: (f.moveSpeed ?? 0) > 0 ? 'Move speed is above zero.' : 'Move speed must be above zero.' },
  ]
  if ((f.damage ?? 0) > 0) {
    list.push({ ok: (f.attackRange ?? 0) > 0, message: (f.attackRange ?? 0) > 0 ? 'Attack range is set.' : 'A damage-dealing unit needs an attack range.' })
    list.push({ ok: (f.attackSpeed ?? 0) > 0, message: (f.attackSpeed ?? 0) > 0 ? 'Attack speed is set.' : 'A damage-dealing unit needs an attack speed.' })
  }
  return list
})

// An archetype outside the combat-profile set is NOT rejected by the server —
// it silently falls back to the soldier profile. So warn, don't block.
const archetypeWarning = computed(() => {
  const value = form.value.archetype
  if (!value || archetypes.value.length === 0) return ''
  if (archetypes.value.includes(value)) return ''
  return `"${value}" is not a known combat profile — this unit will fall back to the soldier profile.`
})

async function reloadCatalogs() {
  const [f, a, p, ab, dt, b, pk] = await Promise.all([
    fetchFactions(), fetchArchetypes(), fetchProjectileIds(),
    fetchAuthoredAbilityDefs(), fetchDamageTypes(), fetchBuildingIds(),
    fetchAuthoredPerkDefs(),
  ])
  factions.value = f
  archetypes.value = a
  projectileIds.value = p
  abilityDefs.value = ab
  damageTypes.value = dt
  buildingIds.value = b
  perkDefs.value = pk
}

async function createFaction() {
  factionError.value = ''
  busy.value = true
  try {
    await saveFaction({ id: newFactionId.value.trim(), displayName: newFactionName.value.trim() })
    await reloadCatalogs()
    factionFilter.value = newFactionId.value.trim()
    // Only seed the faction of a NEW unit. Writing this while an existing unit
    // is loaded would silently reassign that unit's faction.
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
  // A faction delete is refused server-side while it still owns units, so this
  // prompt names that instead of implying the units go with it.
  if (!(await confirmDelete('faction', id, 'Any units still assigned to it will block the delete.'))) return
  factionError.value = ''
  busy.value = true
  try {
    await deleteFaction(id)
    if (factionFilter.value === id) factionFilter.value = ''
    if (factionToDelete.value === id) factionToDelete.value = ''
    await reloadCatalogs()
  } catch (e) {
    factionError.value = e instanceof EditorValidationError ? e.serverMessage
      : e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

// resourceCost / pathChances are string->number maps on the form. Rows are
// kept as local {key,value} arrays (so add/remove/rename is simple array
// editing) and mirrored back into form.<field> via watch — saveRequestFromForm
// reads form.<field> directly, so the mirrored map is what actually saves.
interface MapRow { key: string; value: number }
const resourceCostRows = ref<MapRow[]>([])
const pathChancesRows = ref<MapRow[]>([])
// Base Stats: per-unit-type base values for fieldless registered stats
// (critChance, critMultiplier, lifesteal). Edited as rows, mirrored into
// form.baseStats via watch. Same MapRow machinery as pathChances/resourceCost.
const baseStatRows = ref<MapRow[]>([])

function rowsFromMap(map?: Record<string, number>): MapRow[] {
  return Object.entries(map ?? {}).map(([key, value]) => ({ key, value }))
}
function mapFromRows(rows: MapRow[]): Record<string, number> {
  const out: Record<string, number> = {}
  for (const row of rows) if (row.key) out[row.key] = row.value
  return out
}
function removeResourceCostRow(idx: number) { resourceCostRows.value.splice(idx, 1) }
function addPathChanceRow() { pathChancesRows.value.push({ key: '', value: 0 }) }
function removePathChanceRow(idx: number) { pathChancesRows.value.splice(idx, 1) }
function addBaseStatRow() { baseStatRows.value.push({ key: '', value: 0 }) }
function removeBaseStatRow(idx: number) { baseStatRows.value.splice(idx, 1) }

watch(resourceCostRows, (rows) => { form.value.resourceCost = mapFromRows(rows) }, { deep: true })
watch(pathChancesRows, (rows) => { form.value.pathChances = mapFromRows(rows) }, { deep: true })
watch(baseStatRows, (rows) => { form.value.baseStats = mapFromRows(rows) }, { deep: true })

// Ability Stats: the rows are SERVER-DERIVED (every action x kind pair that
// exists), so they are fetched rather than mirrored here — a local list would
// rot the moment an action gained or lost a kinded field. A failed fetch leaves
// the list empty, which degrades to "no stats offered" rather than to a wrong
// set; any already-authored value still renders (AbilityStatsEditor keeps
// unknown ids as options).
const abilityStatDefs = ref<AbilityStatDef[]>([])

async function loadAbilityStatDefs() {
  try {
    const res = await fetch('/catalog/ability-stats')
    if (!res.ok) return
    const body = (await res.json()) as { stats?: AbilityStatDef[] }
    abilityStatDefs.value = body.stats ?? []
  } catch {
    // Offline / server down: leave the list empty (see the comment above).
  }
}
void loadAbilityStatDefs()

// v-model bridge: the editor component owns row state and emits the whole map.
const abilityStatsModel = computed({
  get: () => form.value.abilityStats ?? {},
  set: (v) => {
    form.value.abilityStats = v
  },
})

// Base Stat options: only base-authorable stats (critChance, critMultiplier,
// lifesteal) — the server rejects any other key. An already-set key is kept as
// an option so an existing value never silently vanishes from the picker.
const baseStatOptions = computed<FilterableOption[]>(() => {
  const opts = new Map<string, string>()
  for (const d of baseAuthorableStatDefs()) opts.set(d.id, d.label)
  for (const row of baseStatRows.value) if (row.key && !opts.has(row.key)) opts.set(row.key, row.key)
  return [...opts].map(([id, label]) => ({ id, label }))
})

// Path Chance keys are promotion paths that belong to THIS unit — offered as a
// dropdown rather than free text. Any already-set key that isn't (or is no
// longer) a real path is kept as an option so an existing value never silently
// vanishes from the picker.
const pathChanceOptions = computed<FilterableOption[]>(() => {
  const ids = new Set<string>()
  if (selectedType.value) {
    for (const entry of pathsByUnit.value[selectedType.value] ?? []) ids.add(entry.path)
  }
  for (const row of pathChancesRows.value) if (row.key) ids.add(row.key)
  return [...ids].sort((a, b) => a.localeCompare(b)).map((id) => ({ id, label: id }))
})

// channelLoop is a nested {start,end} object that must stay entirely undefined
// when both fields are unset. Tracked as two local optional refs and merged
// back into form.channelLoop via watch.
const channelLoopStart = ref<number | undefined>(undefined)
const channelLoopEnd = ref<number | undefined>(undefined)
watch([channelLoopStart, channelLoopEnd], ([s, e]) => {
  form.value.channelLoop = (s === undefined && e === undefined) ? undefined : { start: s ?? 0, end: e ?? 0 }
})

// The channelling ability (if any) of the unit/path currently being edited —
// drives the sprite preview's Channel Loop control. Resolves against the
// ability catalog: a channelling ability is one whose def sets channelType.
const channelAbility = computed(() => {
  const abilities = editingPath.value ? pathForm.value?.abilities : form.value.abilities
  return pickChannelAbility(abilities, abilityDefsById.value)
})

// Generic comma-separated string[] binding for targetableTypes. (Abilities and
// requiresBuildings use dropdown rows instead.)
type StringListField = 'targetableTypes'
function updateStringList(field: StringListField, raw: string) {
  form.value[field] = raw.split(',').map((s) => s.trim()).filter(Boolean)
}

// Capabilities are DERIVED, not authored: the form exposes intent (flags + move
// speed) and the saved `capabilities` list is computed from them, so the two
// can never drift. The full unit capability set is exactly these four
// (protocol.ts: UnitCapability):
//   move   ← has a positive move speed
//   attack ← NOT flagged non-combat
//   gather ← "Can gather" flag (also reveals the gather-amount fields)
//   build  ← "Builder" flag
const canGather = ref(false)
const builder = ref(false)
const derivedCapabilities = computed<string[]>(() => {
  const caps: string[] = []
  if ((form.value.moveSpeed ?? 0) > 0) caps.push('move')
  if (!form.value.nonCombat) caps.push('attack')
  if (canGather.value) caps.push('gather')
  if (builder.value) caps.push('build')
  return caps
})
watch(derivedCapabilities, (caps) => { form.value.capabilities = caps }, { immediate: true })

// --- Profile picture (portrait.png) ---------------------------------------
// The unit's portrait is a `portrait.png` in its writable art dir — the same
// file the PixelLab ingest can emit, uploaded here on its own. It backs the
// sidebar row, training queue, build menu and multi-select cards.

// Object URL of a just-picked file, shown immediately so the editor always
// reflects the chosen image even for a unit that has no packed sprites (whose
// portrait the runtime overlay can't resolve without a sprites.json manifest).
const portraitOverrideUrl = ref<string | null>(null)
// Bumped after a successful upload so portraitUrl re-resolves getUnitPortraitUrl
// (a plain function) against the freshly reloaded runtime overlay.
const portraitVersion = ref(0)

function clearPortraitOverride() {
  if (portraitOverrideUrl.value) URL.revokeObjectURL(portraitOverrideUrl.value)
  portraitOverrideUrl.value = null
}

// The portrait shown in Identity: the just-picked file if any, else whatever
// bundled/runtime art resolves for this unit (empty frame when there is none).
const portraitUrl = computed(() => {
  if (portraitOverrideUrl.value) return portraitOverrideUrl.value
  void portraitVersion.value
  if (!form.value.type) return ''
  return getUnitPortraitUrl(undefined, form.value.type) ?? ''
})

// Art is keyed by (faction, unit-type) on disk, so the unit must be saved (its
// id fixed) and have a faction before a portrait can be written — mirrors the
// Item editor's "save before uploading an icon" rule.
const canUploadPortrait = computed(() => selectedType.value !== null && !!form.value.faction)

async function onPortraitChosen(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  if (!canUploadPortrait.value) {
    saveError.value = 'Save the unit before adding a profile picture.'
    input.value = ''
    return
  }
  saveError.value = ''
  // Optimistic: show the picked file right away.
  clearPortraitOverride()
  portraitOverrideUrl.value = URL.createObjectURL(file)
  busy.value = true
  try {
    const contentBase64 = await blobToBase64(file)
    await saveUnitArt({
      faction: form.value.faction,
      unit: form.value.type,
      files: [{ name: 'portrait.png', contentBase64 }],
    })
    // Refresh the shared runtime overlay so the sidebar row + in-editor sprite
    // preview pick up the new portrait too (for units that have a manifest).
    await loadRuntimeSpriteSets()
    portraitVersion.value++
    preview.value?.refresh()
  } catch (e) {
    saveError.value = e instanceof EditorValidationError ? e.serverMessage
      : e instanceof Error ? e.message : String(e)
    clearPortraitOverride()   // upload failed — drop the optimistic preview
  } finally {
    busy.value = false
    input.value = ''
  }
}

// --- Art ingest: drop a PixelLab export folder, pack it entirely in-browser,
// preview it live via the runtime overlay, and persist on Save. ---

// Packed-but-unsaved ingest result. Feeds the overlay via object URLs so the
// preview plays the freshly-packed art before anything is saved.
const pendingArt = ref<IngestResult | null>(null)
let pendingRevoke: (() => void) | null = null
const ingestError = ref('')
const ingestWarnings = ref<string[]>([])

// Guards runIngest against overlapping drops (whichever resolves last would
// silently overwrite the other's revoke function, leaking blob URLs).
const ingesting = ref(false)

// Revokes the pending object URLs (if any) and clears local ingest state. This
// is PURELY local cleanup — it does NOT touch the shared runtime overlay.
// Callers that abandon a registered preview set outside save/discard must also
// call flushPreviewOverlay().
function clearPending() {
  pendingRevoke?.()
  pendingRevoke = null
  pendingArt.value = null
  ingestWarnings.value = []
}

// The shared runtime sprite overlay is a MODULE-GLOBAL the live game map also
// reads — it outlives this panel. If the panel registers an in-browser preview
// set (backed by object URLs) and the URLs are then revoked without flushing
// the overlay, that unit renders blank in an actual match for the rest of the
// session. loadRuntimeSpriteSets() swaps the overlay back to server truth.
async function flushPreviewOverlay() {
  await loadRuntimeSpriteSets()
  preview.value?.refresh()
}

// spritePacking.ts's pure core can't derive `key` (no directory context), so we
// attach it here from the unit/path id, mirroring the CLI's basename rule.
function manifestWithKey(manifest: IngestResult['manifest'], key: string): SpriteManifest {
  return { ...manifest, key }
}

// Art ingest is shared between unit and path mode — these computeds are the
// ONLY mode-aware surface of that machinery. A path's art is keyed by (parent
// unit, path id); saveUnitArt's optional `path` param routes it to the path's
// own writable art dir instead of the base unit's.
const artKey = computed(() => editingPath.value
  ? (pathForm.value?.path || pathForm.value?.parentUnit || '')
  : form.value.type)
const artUnit = computed(() => editingPath.value
  ? (pathForm.value?.parentUnit ?? '')
  : form.value.type)
const artFaction = computed(() => editingPath.value
  ? (units.value.find((u) => u.type === pathForm.value?.parentUnit)?.faction ?? '')
  : form.value.faction)
const artPath = computed(() => editingPath.value ? (pathForm.value?.path || undefined) : undefined)
const canIngestArt = computed(() => editingPath.value
  ? !!(pathForm.value?.parentUnit && pathForm.value?.path)
  : !!(form.value.type && form.value.faction))

async function runIngest(files: DroppedFile[]) {
  if (ingesting.value) return
  ingesting.value = true
  ingestError.value = ''
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

    const key = artKey.value.toLowerCase()
    const manifest = manifestWithKey(result.manifest, artKey.value)
    const set = buildSpriteSet(key, manifest, (rel) => urls[rel])
    if (set) registerRuntimeSpriteSet(set)
    preview.value?.refresh()
  } catch (e) {
    ingestError.value = e instanceof Error ? e.message : String(e)
    clearPending()
    if (hadPending) void flushPreviewOverlay()
  } finally {
    ingesting.value = false
  }
}

// Primary ingest path: <input type="file" webkitdirectory>. webkitRelativePath
// is "<wrapperFolder>/<rest...>"; strip the wrapper so paths are relative to the
// export folder root, matching what ingestExportFolder expects.
function onFolderInputChanged(fileList: FileList | null) {
  if (!fileList) return
  const files: DroppedFile[] = [...fileList].map((f) => ({
    path: f.webkitRelativePath.split('/').slice(1).join('/'),
    blob: f,
  }))
  void runIngest(files)
}

// Secondary ingest path: dragging a folder onto the zone.
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
  await flushPreviewOverlay()
}

async function reload() {
  try {
    const [u, p] = await Promise.all([fetchAuthoredUnitDefs(), fetchPaths()])
    units.value = u
    paths.value = p
    loadError.value = ''
    factionError.value = ''
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : String(e)
  }
}

function selectUnit(def: AuthoredUnitDef) {
  // A unit switch abandons any not-yet-saved ingest for the PREVIOUS unit — its
  // object URLs must be revoked (clearPending), AND — if a preview set was
  // actually registered — the shared overlay must be flushed back to server
  // truth (flushPreviewOverlay), or the abandoned unit renders blank in a match.
  const hadPending = pendingArt.value !== null
  clearPending()
  ingestError.value = ''
  if (hadPending) void flushPreviewOverlay()
  editorMode.value = 'unit'
  selectedPath.value = null
  selectedPathParent.value = null
  pathForm.value = null
  clearPortraitOverride()
  form.value = formFromDef(def)
  canGather.value = (def.capabilities ?? []).includes('gather')
  builder.value = (def.capabilities ?? []).includes('build')
  selectedType.value = def.type
  saveError.value = ''
  lastSavedAt.value = null
  resourceCostRows.value = rowsFromMap(def.resourceCost)
  pathChancesRows.value = rowsFromMap(def.pathChances)
  baseStatRows.value = rowsFromMap(def.baseStats)
  channelLoopStart.value = def.channelLoop?.start
  channelLoopEnd.value = def.channelLoop?.end
  preview.value?.refresh()
}

// The `type` field is the unit's id (its directory name, sprite key, and what
// pathChances/requiresBuildings/spawn all reference), so it must match
// `^[a-z0-9_]+$`. The id is a slug auto-derived from the Name.
function slugifyUnitId(raw: string): string {
  return raw.toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, '')
}

const typeIdManuallyEdited = ref(false)

function onTypeIdInput(raw: string) {
  form.value.type = raw.toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+/, '')
  typeIdManuallyEdited.value = true
}

// Auto-derive the id from the Name for NEW units only, until the author edits
// the id by hand. Existing units keep their id — it's the primary key.
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
  pathForm.value = null
  clearPortraitOverride()
  form.value = createBlankForm(pickTemplateStats(units.value))
  form.value.faction = factionFilter.value || ''
  canGather.value = false
  builder.value = false
  selectedType.value = null
  typeIdManuallyEdited.value = false
  saveError.value = ''
  lastSavedAt.value = null
  resourceCostRows.value = []
  pathChancesRows.value = []
  baseStatRows.value = []
  channelLoopStart.value = undefined
  channelLoopEnd.value = undefined
  preview.value?.refresh()
}

// Return to the empty screen — used after deleting the open unit (nothing left
// to show) and when a deleted path has no parent unit to fall back to.
function clearSelection() {
  const hadPending = pendingArt.value !== null
  clearPending()
  ingestError.value = ''
  if (hadPending) void flushPreviewOverlay()
  clearPortraitOverride()
  editorMode.value = 'empty'
  selectedType.value = null
  selectedPath.value = null
  selectedPathParent.value = null
  pathForm.value = null
  pathForm.value = null
  saveError.value = ''
  lastSavedAt.value = null
}

async function save() {
  saveError.value = ''
  // Drop blank ability / building rows the author added but never picked.
  form.value.abilities = (form.value.abilities ?? []).filter(Boolean)
  form.value.requiresBuildings = (form.value.requiresBuildings ?? []).filter(Boolean)
  busy.value = true
  try {
    await saveEditorUnit(saveRequestFromForm(form.value))
    await reload()
    selectedType.value = form.value.type
    lastSavedAt.value = Date.now()
    nowTs.value = lastSavedAt.value
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
  // A unit type is a hand-authored catalog file — stat block, abilities and its
  // promotion paths. Deleting it unprompted was how an adept got lost.
  if (!(await confirmDelete('unit type', selectedType.value, 'Its promotion paths are deleted with it.'))) return
  saveError.value = ''
  busy.value = true
  try {
    await deleteEditorUnit(selectedType.value)
    await reload()
    clearSelection()
  } catch (e) {
    saveError.value = e instanceof EditorValidationError ? e.serverMessage
      : e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

// savePath persists the path file, then — for a brand-new path with the
// checkbox checked — adds it to the parent unit's pathChances. ORDER IS
// LOAD-BEARING: (1) path file first (nothing may reference a path that
// doesn't exist), (2) pathChances last and only for a new path.
async function savePath() {
  if (!pathForm.value) return
  saveError.value = ''
  // Drop blank ability rows the author added but never picked.
  pathForm.value.abilities = (pathForm.value.abilities ?? []).filter(Boolean)
  busy.value = true
  const isNewPath = selectedPath.value === null
  try {
    const req = saveRequestFromPathForm(pathForm.value)
    const pathId = req.path.path ?? ''

    await savePathApi(req)

    if (isNewPath && addPathToPathChances.value) {
      const parentDef = units.value.find((u) => u.type === req.unit)
      if (parentDef) {
        const parentForm = formFromDef(parentDef)
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
  if (!(await confirmDelete('promotion path', selectedPath.value))) return
  saveError.value = ''
  busy.value = true
  try {
    await deletePathApi(selectedPath.value)
    await reload()
    const parentType = selectedPathParent.value
    const parentDef = parentType ? units.value.find((u) => u.type === parentType) : undefined
    if (parentDef) {
      selectUnit(parentDef)
    } else {
      clearSelection()
    }
  } catch (e) {
    saveError.value = e instanceof EditorValidationError ? e.serverMessage
      : e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

onMounted(async () => {
  clock = setInterval(() => { nowTs.value = Date.now() }, 15_000)
  await reload()
  try {
    await reloadCatalogs()
  } catch (e) {
    // A catalog fetch failure degrades the selects to free text — it must not
    // take the whole panel down, because unit editing still works without them.
    loadError.value = e instanceof Error ? e.message : String(e)
  }
})

onBeforeUnmount(() => {
  if (clock !== null) clearInterval(clock)
  clearPortraitOverride()
  // Same hazard as selectUnit/newUnit: closing the editor with an unsaved drop
  // still registered must flush the shared overlay back to server truth.
  const hadPending = pendingArt.value !== null
  clearPending()
  if (hadPending) void loadRuntimeSpriteSets()
})
</script>

<style scoped>
.unit-editor {
  font-family: var(--font-body);
  color: var(--ed-text);
}

/* ── Sidebar: unit list on top, faction CRUD pinned below ── */
.unit-sidebar {
  display: flex;
  flex-direction: column;
  gap: 8px;
  height: 100%;
  min-height: 0;
}

.unit-sidebar__list {
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
}

.unit-sidebar__load-error {
  flex: 0 0 auto;
}

.unit-sidebar__factions {
  flex: 0 0 auto;
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding-top: 8px;
  border-top: 1px solid var(--ed-line);
}

.unit-sidebar__faction-toggle {
  align-self: flex-start;
  padding: 4px 10px;
  font-family: var(--font-body);
  font-size: 0.72rem;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--ed-brass);
  background: rgba(212, 168, 71, 0.08);
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
}

.unit-sidebar__faction-toggle:hover:not(:disabled) {
  background: rgba(212, 168, 71, 0.16);
  border-color: var(--ed-line-strong);
}

.unit-sidebar__faction-form,
.unit-sidebar__faction-del {
  display: flex;
  gap: 6px;
  align-items: center;
}

.unit-sidebar__faction-form input,
.unit-sidebar__faction-del select {
  min-width: 0;
  flex: 1 1 auto;
}

/* Empty state — shown on tab entry and after a delete, until something is
   selected. */
.unit-editor__empty {
  flex: 1 1 auto;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--ed-text-dim);
}

/* Tab strip above the base-unit form; the hairline connects it to the grid. */
.unit-editor__tabs {
  border-bottom: 1px solid var(--ed-line);
  margin-bottom: var(--ed-gap);
}

/* ── Form grid: cards flow into as many columns as fit ── */
.unit-editor__scroll {
  flex: 1 1 auto;
  min-height: 0;
}

.unit-editor__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: var(--ed-gap);
  align-content: start;
  padding-right: 4px;
}

/* Preview and the rank grid are wide — they take two card columns when
   there's room (Preview then sits to the right of Identity). */
.unit-editor__wide {
  grid-column: span 2;
}

/* The base-unit Preview is wider still — three card columns — so the sprite
   stage dominates its (Identity) tab. Steps down as the viewport narrows. */
.unit-editor__wide3 {
  grid-column: span 3;
}

/* Combat path sub-tab layout: Combat and Abilities stack in a narrow left
   column while the wide Rank Stats table fills the rest and spans both rows.
       [ 1. Combat    ] [ Rank Stats            ]
       [ 2. Abilities ] [  (spans both rows)    ]  */
.unit-editor__combat-main {
  grid-column: 1;
  grid-row: 1;
}
.unit-editor__combat-sub {
  grid-column: 1;
  grid-row: 2;
}
.unit-editor__combat-ranks {
  grid-column: 2 / -1;
  grid-row: 1 / 3;
  min-width: 0;
}

/* Too narrow for two columns — stack all three full-width instead. */
@media (max-width: 1100px) {
  .unit-editor__combat-main,
  .unit-editor__combat-sub,
  .unit-editor__combat-ranks {
    grid-column: 1 / -1;
    grid-row: auto;
  }
}

/* Rank Slots: one card per rank (Bronze/Silver/Gold), stacked vertically and
   spanning the full editor grid width. */
.unit-editor__rank-slot-stack {
  grid-column: 1 / -1;
  display: flex;
  flex-direction: column;
  gap: var(--ed-gap);
}

.unit-editor__perk-list {
  list-style: none;
  margin: 0 0 8px;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.unit-editor__perk-row {
  display: flex;
  align-items: center;
  gap: 6px;
}

.unit-editor__perk-name {
  flex: 1 1 auto;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.unit-editor__perk-add {
  width: 100%;
}

.unit-editor__slot-type {
  display: flex;
  gap: 12px;
  margin: 0 0 8px;
  font-size: 0.78rem;
  color: var(--ed-text-dim);
}

.unit-editor__slot-type-option {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

@media (max-width: 1500px) {
  .unit-editor__wide {
    grid-column: span 1;
  }
  .unit-editor__wide3 {
    grid-column: span 2;
  }
}

@media (max-width: 1100px) {
  .unit-editor__wide3 {
    grid-column: span 1;
  }
}

.unit-editor__pair {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}

/* Profile picture at the top of Identity: framed preview + file picker. */
.unit-editor__portrait {
  display: flex;
  align-items: center;
  gap: 12px;
  padding-bottom: 10px;
  margin-bottom: 2px;
  border-bottom: 1px solid var(--ed-line);
}

.unit-editor__portrait-side {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

/* In the widened Stats card, stats flow into as many columns as fit (3–4 across
   on a roomy screen) rather than a cramped two. Wider min than a stacked field
   needs, because each stat now lays its label and input side by side. */
.unit-editor__stats {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 8px 14px;
}

/* Stat fields put the label and the (compact, fixed-width) number input on one
   row — label left, value right — to consolidate the tall stacked list. */
.unit-editor__stats :deep(.ed-field) {
  flex-direction: row;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.unit-editor__stats :deep(.ed-field input) {
  width: 76px;
  flex: 0 0 auto;
}

/* Flag checkboxes sit in a wrapping row under the stats. */
.unit-editor__flags {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 18px;
  padding-top: 2px;
}

/* Validation: last card AND pinned to the final column, so it lands bottom
   right of the form no matter how many columns fit. */
.unit-editor__validation {
  grid-column: -2 / -1;
}

/* ── key→value map rows (resource cost, path chances) ── */
.unit-editor__map-row {
  display: grid;
  grid-template-columns: 1fr 90px auto;
  gap: 6px;
  align-items: center;
}

/* Ability picker rows: dropdown fills the width, the ✕ hugs the right. */
.unit-editor__ability-row {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 6px;
  align-items: center;
}

.unit-editor__row-del {
  padding: 4px 8px;
  font-size: 0.76rem;
  color: var(--ed-text-dim);
  background: none;
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
}

.unit-editor__row-del:hover {
  color: var(--ed-danger);
  border-color: rgba(240, 132, 108, 0.4);
}

/* ── Promotion-paths nav chips ── */
/* ── Promotion Paths tab: nested path selector + inline path editor ── */
.unit-editor__paths-tab {
  display: flex;
  flex-direction: column;
  gap: var(--ed-gap);
  min-width: 0;
}

/* Path selector strip — same tab look as the unit/section tabs, with New Path
   pushed to the far right. */
.unit-editor__path-strip {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  border-bottom: 1px solid var(--ed-line);
}

.unit-editor__path-tab {
  padding: 7px 16px;
  font-family: var(--font-display);
  font-size: 0.9rem;
  letter-spacing: 0.04em;
  color: var(--ed-text-dim);
  background: var(--ed-tab-bg, rgba(20, 16, 12, 0.6));
  border: 1px solid var(--ed-line);
  border-bottom: none;
  border-radius: 4px 4px 0 0;
}

.unit-editor__path-tab:hover {
  color: var(--ed-text);
}

.unit-editor__path-tab--active {
  color: var(--ed-text);
  background: var(--ed-tab-bg-active, rgba(58, 50, 41, 0.9));
  border-color: var(--ed-brass);
}

/* New Path sits directly after the existing path tabs, reading as one more tab
   (an add affordance). */
.unit-editor__path-tab--new {
  color: var(--ed-brass);
}

/* Compact action bar under the path selector: id (new paths only) + Save/Delete,
   pushed to the right. */
.unit-editor__path-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
  flex-wrap: wrap;
}

.unit-editor__path-id {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 0.72rem;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--ed-text-dim);
}

.unit-editor__path-actions .unit-editor__path-id input {
  width: 160px;
}

.unit-editor__path-error {
  margin-left: 4px;
}

/* The rank grid can be wider than its card — scroll it horizontally so every
   stat column stays reachable instead of being clipped. */
.unit-editor__rank-scroll {
  overflow-x: auto;
  max-width: 100%;
}

/* ── Rail art ingest ── */
.unit-art {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.unit-art__drop {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 10px;
  font-size: 0.74rem;
  color: var(--ed-text-dim);
  text-align: center;
  background: rgba(8, 6, 4, 0.4);
  border: 1px dashed var(--ed-line);
  border-radius: var(--ed-radius);
}

.unit-art__actions {
  display: flex;
  gap: 8px;
}

/* ── Shared inline text ── */
.unit-hint {
  font-size: 0.68rem;
  color: var(--ed-text-dim);
  opacity: 0.8;
}

.unit-warn {
  margin: 0;
  font-size: 0.72rem;
  color: var(--ed-brass);
}

.unit-error {
  margin: 0;
  font-size: 0.74rem;
  color: var(--ed-danger);
}
</style>
