<template>
  <div class="world-editor-root">
    <WorldEditorToolbar :active-id="toolbarActiveId" @select="onToolbarSelect" />
    <div class="editor-shell">
    <div class="editor-controls">
      <div class="editor-title">Map Editor</div>
      <p class="editor-copy">
        Pan and zoom like the main game view. Turn on paint mode only when you
        want clicks to edit the map.
      </p>

      <section class="editor-section" :class="{ 'editor-section--open': openSection === 'setup' }">
        <button type="button" class="editor-section__summary" @click="toggleSection('setup')">
          Map Setup
        </button>
        <div v-if="openSection === 'setup'" class="editor-section__body">
          <div class="control-group">
            <label for="editor-map-id">Map ID</label>
            <input id="editor-map-id" v-model.trim="model.id" type="text" />
          </div>

          <div class="control-group">
            <label for="editor-map-name">Map Name</label>
            <input id="editor-map-name" v-model.trim="model.name" type="text" />
          </div>

          <div class="control-group">
            <label for="editor-map-description">Description</label>
            <textarea
              id="editor-map-description"
              v-model.trim="model.description"
              class="metadata-box"
              rows="3"
            ></textarea>
          </div>

          <div class="wave-config-block">
            <div class="wave-config-title">Wave Config</div>
            <div class="control-group">
              <label for="wave-total">Total Waves <span class="field-hint">(0 = disabled)</span></label>
              <input
                id="wave-total"
                :value="model.waveConfig?.totalWaves ?? 0"
                @input="setWaveConfig('totalWaves', +($event.target as HTMLInputElement).value)"
                type="number"
                min="0"
                max="999"
              />
            </div>
            <div class="control-group">
              <label for="wave-initial-prep">Initial Prep <span class="field-hint">(sec before wave 1, 0 = use Prep Duration)</span></label>
              <input
                id="wave-initial-prep"
                :value="model.waveConfig?.initialPrepDuration ?? 0"
                @input="setWaveConfig('initialPrepDuration', +($event.target as HTMLInputElement).value)"
                type="number"
                min="0"
              />
            </div>
            <div class="control-group">
              <label for="wave-prep">Prep Duration <span class="field-hint">(sec between waves, 0 = default 60)</span></label>
              <input
                id="wave-prep"
                :value="model.waveConfig?.prepDuration ?? 0"
                @input="setWaveConfig('prepDuration', +($event.target as HTMLInputElement).value)"
                type="number"
                min="0"
              />
            </div>
            <div class="control-group">
              <label for="wave-active">Wave Duration <span class="field-hint">(sec, 0 = default 120)</span></label>
              <input
                id="wave-active"
                :value="model.waveConfig?.waveDuration ?? 0"
                @input="setWaveConfig('waveDuration', +($event.target as HTMLInputElement).value)"
                type="number"
                min="0"
              />
            </div>
            <div class="control-group control-group--checkbox">
              <label for="wave-continuous">
                <input
                  id="wave-continuous"
                  type="checkbox"
                  :checked="!!model.waveConfig?.continuousWaves"
                  @change="setWaveConfig('continuousWaves', ($event.target as HTMLInputElement).checked)"
                />
                Continuous Waves <span class="field-hint">(release next wave on timer; never wait for clear)</span>
              </label>
            </div>
            <div class="control-group control-group--checkbox">
              <label for="wave-enemies-fight-neutrals">
                <input
                  id="wave-enemies-fight-neutrals"
                  type="checkbox"
                  :checked="!!model.waveConfig?.enemiesFightNeutrals"
                  @change="setWaveConfig('enemiesFightNeutrals', ($event.target as HTMLInputElement).checked)"
                />
                Enemies Fight Neutrals <span class="field-hint">(camps wiped by enemies drop no loot)</span>
              </label>
            </div>
          </div>

          <div class="wave-config-block">
            <div class="wave-config-title">Debug <span class="field-hint">(omit for production maps)</span></div>
            <label class="debug-flag-row">
              <input
                type="checkbox"
                :checked="model.debug?.battleTracker ?? false"
                @change="setDebugFlag('battleTracker', ($event.target as HTMLInputElement).checked)"
              />
              <span>Battle Tracker <span class="field-hint">(per-player damage / kill HUD)</span></span>
            </label>
            <label class="debug-flag-row">
              <input
                type="checkbox"
                :checked="model.debug?.debugSpawn ?? false"
                @change="setDebugFlag('debugSpawn', ($event.target as HTMLInputElement).checked)"
              />
              <span>Debug Spawn <span class="field-hint">(enemy-with-perks placement tool)</span></span>
            </label>
          </div>

          <!-- Victory Conditions editor card removed in the
               campaign-objectives-and-metrics §6 migration. Per-level
               objectives now live on `CampaignLevel.objectives` in
               `catalog/campaigns/*.json` and are authored by hand for this
               change. A campaign objectives editor is a separate proposal. -->

          <div class="control-group load-map-group">
            <label for="editor-load-map">Load Existing Map</label>
            <select
              id="editor-load-map"
              v-model="selectedLoadMapId"
              :disabled="isLoadingMapCatalog || isLoadingSelectedMap || availableMaps.length === 0"
            >
              <option v-for="map in availableMaps" :key="map.id" :value="map.id">
                {{ map.name }}
              </option>
            </select>

            <div class="menu-text" v-if="mapLoadError">
              {{ mapLoadError }}
            </div>

            <button
              type="button"
              @click="loadSelectedMapIntoEditor"
              :disabled="!selectedLoadMapId || isLoadingMapCatalog || isLoadingSelectedMap"
            >
              {{ isLoadingSelectedMap ? 'Loading...' : 'Load Into Editor' }}
            </button>
          </div>

          <div class="control-group">
            <label for="editor-cols">Columns</label>
            <input id="editor-cols" v-model.number="draftCols" type="number" min="6" max="500" />
          </div>

          <div class="control-group">
            <label for="editor-rows">Rows</label>
            <input id="editor-rows" v-model.number="draftRows" type="number" min="6" max="500" />
          </div>

          <div class="preset-row">
            <button
              v-for="preset in MAP_EDITOR_PRESETS"
              :key="preset.label"
              type="button"
              @click="applyPreset(preset.cols, preset.rows)"
            >
              {{ preset.label }}
            </button>
          </div>

          <button type="button" class="apply-size" @click="applyGridSize">Apply Grid Size</button>

          <div class="summary-row">
            <span>{{ model.gridCols }} x {{ model.gridRows }}</span>
            <span>{{ model.terrain.length }} terrain</span>
            <span>{{ model.obstacles.length }} obstacles</span>
            <span>{{ model.buildings.length }} buildings</span>
          </div>

          <button
            type="button"
            class="clear-everything"
            :disabled="!hasPaintedContent"
            @click="clearEverything"
          >
            Clear Everything
          </button>
        </div>
      </section>

      <section class="editor-section" :class="{ 'editor-section--open': openSection === 'campaign' }">
        <button type="button" class="editor-section__summary" @click="toggleSection('campaign')">
          Campaign
        </button>
        <div v-if="openSection === 'campaign'" class="editor-section__body">
          <label class="debug-flag-row">
            <input
              type="checkbox"
              :checked="!!model.campaign"
              @change="toggleCampaign(($event.target as HTMLInputElement).checked)"
            />
            <span>This map is a campaign map <span class="field-hint">(hides it from Custom Game)</span></span>
          </label>

          <div v-if="campaignCatalogError" class="menu-text">{{ campaignCatalogError }}</div>

          <template v-if="model.campaign">
            <div class="control-group">
              <label for="editor-campaign-id">Campaign</label>
              <select
                id="editor-campaign-id"
                :value="model.campaign.campaignId"
                @change="setCampaignField('campaignId', ($event.target as HTMLSelectElement).value)"
                :disabled="campaignCatalogLoading"
              >
                <option v-for="c in campaignCatalog" :key="c.id" :value="c.id">
                  {{ c.displayName }}
                </option>
              </select>
            </div>

            <div class="control-group">
              <label for="editor-campaign-level-id">Level ID</label>
              <input
                id="editor-campaign-level-id"
                type="text"
                :value="model.campaign.levelId"
                @input="setCampaignField('levelId', ($event.target as HTMLInputElement).value)"
                placeholder="e.g. forest_01"
              />
            </div>

            <div class="control-group">
              <label for="editor-campaign-display-name">Display Name</label>
              <input
                id="editor-campaign-display-name"
                type="text"
                :value="model.campaign.displayName"
                @input="setCampaignField('displayName', ($event.target as HTMLInputElement).value)"
                placeholder="e.g. Forest 1"
              />
            </div>

            <div class="control-group">
              <label for="editor-campaign-prereq">Prerequisite Level</label>
              <select
                id="editor-campaign-prereq"
                :value="model.campaign.prerequisiteLevelId ?? ''"
                @change="setCampaignField(
                  'prerequisiteLevelId',
                  ($event.target as HTMLSelectElement).value || null,
                )"
              >
                <option value="">None (first level)</option>
                <option
                  v-for="lvl in levelsForCampaign(model.campaign.campaignId)"
                  :key="lvl.id"
                  :value="lvl.id"
                  :disabled="lvl.id === model.campaign.levelId"
                >
                  {{ lvl.displayName }} ({{ lvl.id }})
                </option>
              </select>
            </div>

            <div class="control-group">
              <label for="editor-campaign-sort-order">Sort Order <span class="field-hint">(level row order)</span></label>
              <input
                id="editor-campaign-sort-order"
                type="number"
                :value="model.campaign.sortOrder ?? 0"
                @input="setCampaignField('sortOrder', +($event.target as HTMLInputElement).value)"
              />
            </div>

            <div class="control-group">
              <label for="editor-campaign-description">Description</label>
              <textarea
                id="editor-campaign-description"
                :value="model.campaign.description ?? ''"
                @input="setCampaignField('description', ($event.target as HTMLTextAreaElement).value)"
                class="metadata-box"
                rows="2"
              ></textarea>
            </div>

            <div class="campaign-objectives">
              <div class="campaign-objectives__header">
                <span>Objectives</span>
                <button type="button" @click="addObjective">+ Add</button>
              </div>

              <div
                v-for="(obj, idx) in (model.campaign.objectives ?? [])"
                :key="idx"
                class="campaign-objective"
              >
                <div class="campaign-objective__row">
                  <input
                    class="campaign-objective__id"
                    :value="obj.id"
                    placeholder="objective id"
                    @input="updateObjective(idx, { id: ($event.target as HTMLInputElement).value })"
                  />
                  <select
                    :value="obj.type"
                    @change="updateObjectiveType(idx, ($event.target as HTMLSelectElement).value)"
                  >
                    <option v-for="t in KNOWN_OBJECTIVE_TYPES" :key="t" :value="t">{{ t }}</option>
                  </select>
                  <button type="button" class="campaign-objective__remove" @click="removeObjective(idx)" title="Remove objective">×</button>
                </div>

                <div class="campaign-objective__row">
                  <input
                    type="text"
                    :value="obj.description ?? ''"
                    placeholder="description (shown to player)"
                    @input="updateObjective(idx, { description: ($event.target as HTMLInputElement).value })"
                  />
                </div>

                <div class="campaign-objective__row campaign-objective__meta">
                  <select
                    :value="obj.scope ?? 'team'"
                    @change="updateObjective(idx, { scope: ($event.target as HTMLSelectElement).value as 'team' | 'player' })"
                  >
                    <option value="team">Team</option>
                    <option value="player">Per-Player</option>
                  </select>
                  <label class="campaign-objective__required">
                    <input
                      type="checkbox"
                      :checked="obj.required ?? false"
                      @change="updateObjective(idx, { required: ($event.target as HTMLInputElement).checked })"
                    />
                    <span>Required <span class="field-hint">(gates victory)</span></span>
                  </label>
                  <label class="campaign-objective__reward">
                    <span>DP Reward <span class="field-hint">(first completion)</span></span>
                    <input
                      type="number" min="0"
                      :value="obj.rewardDominionPoints ?? 0"
                      @input="updateObjective(idx, { rewardDominionPoints: Math.max(0, Math.floor(+($event.target as HTMLInputElement).value || 0)) })"
                    />
                  </label>
                  <label class="campaign-objective__reward">
                    <span>Badge Reward <span class="field-hint">(first completion)</span></span>
                    <input
                      type="number" min="0"
                      :value="obj.rewardConquestBadges ?? 0"
                      @input="updateObjective(idx, { rewardConquestBadges: Math.max(0, Math.floor(+($event.target as HTMLInputElement).value || 0)) })"
                    />
                  </label>
                </div>

                <!-- Per-type config fields. Keys must match the server handler
                     configs in objective_handlers.go. -->
                <div v-if="obj.type === 'kill_camps'" class="campaign-objective__config">
                  <label>Camp Tier <span class="field-hint">(blank = any)</span>
                    <input
                      type="number" min="1" max="3"
                      :value="(objectiveConfigValue(obj, 'campTier') as number | undefined) ?? ''"
                      @input="updateObjectiveConfigField(idx, 'campTier', ($event.target as HTMLInputElement).value ? +($event.target as HTMLInputElement).value : undefined)"
                    />
                  </label>
                  <label>Count
                    <input
                      type="number" min="1"
                      :value="(objectiveConfigValue(obj, 'count') as number | undefined) ?? 1"
                      @input="updateObjectiveConfigField(idx, 'count', +($event.target as HTMLInputElement).value)"
                    />
                  </label>
                </div>

                <div v-else-if="obj.type === 'build_buildings'" class="campaign-objective__config">
                  <label>Building Type
                    <select
                      :value="(objectiveConfigValue(obj, 'buildingType') as string | undefined) ?? 'barracks'"
                      @change="updateObjectiveConfigField(idx, 'buildingType', ($event.target as HTMLSelectElement).value)"
                    >
                      <option v-for="def in paintableBuildingDefs" :key="def.type" :value="def.type">
                        {{ def.label || def.type }}
                      </option>
                    </select>
                  </label>
                  <label>Count
                    <input
                      type="number" min="1"
                      :value="(objectiveConfigValue(obj, 'count') as number | undefined) ?? 1"
                      @input="updateObjectiveConfigField(idx, 'count', +($event.target as HTMLInputElement).value)"
                    />
                  </label>
                </div>

                <div v-else-if="obj.type === 'collect_resource'" class="campaign-objective__config">
                  <label>Resource
                    <select
                      :value="(objectiveConfigValue(obj, 'resource') as string | undefined) ?? 'gold'"
                      @change="updateObjectiveConfigField(idx, 'resource', ($event.target as HTMLSelectElement).value)"
                    >
                      <option value="gold">Gold</option>
                      <option value="wood">Wood</option>
                    </select>
                  </label>
                  <label>Amount
                    <input
                      type="number" min="1"
                      :value="(objectiveConfigValue(obj, 'amount') as number | undefined) ?? 100"
                      @input="updateObjectiveConfigField(idx, 'amount', +($event.target as HTMLInputElement).value)"
                    />
                  </label>
                </div>

                <div v-else-if="obj.type === 'kill_camps_before_wave'" class="campaign-objective__config">
                  <label>Count
                    <input
                      type="number" min="1"
                      :value="(objectiveConfigValue(obj, 'count') as number | undefined) ?? 1"
                      @input="updateObjectiveConfigField(idx, 'count', +($event.target as HTMLInputElement).value)"
                    />
                  </label>
                  <label>Before Wave
                    <input
                      type="number" min="1"
                      :value="(objectiveConfigValue(obj, 'beforeWave') as number | undefined) ?? 5"
                      @input="updateObjectiveConfigField(idx, 'beforeWave', +($event.target as HTMLInputElement).value)"
                    />
                  </label>
                </div>

                <div v-else-if="obj.type === 'rank_units'" class="campaign-objective__config">
                  <label>Rank
                    <select
                      :value="(objectiveConfigValue(obj, 'rank') as string | undefined) ?? 'bronze'"
                      @change="updateObjectiveConfigField(idx, 'rank', ($event.target as HTMLSelectElement).value)"
                    >
                      <option value="bronze">Bronze</option>
                      <option value="silver">Silver</option>
                      <option value="gold">Gold</option>
                    </select>
                  </label>
                  <label>Count
                    <input
                      type="number" min="1"
                      :value="(objectiveConfigValue(obj, 'count') as number | undefined) ?? 1"
                      @input="updateObjectiveConfigField(idx, 'count', +($event.target as HTMLInputElement).value)"
                    />
                  </label>
                </div>

                <div v-else-if="obj.type === 'survive_waves'" class="campaign-objective__config">
                  <label>Waves to Survive
                    <input
                      type="number" min="1"
                      :value="(objectiveConfigValue(obj, 'wavesToSurvive') as number | undefined) ?? 1"
                      @input="updateObjectiveConfigField(idx, 'wavesToSurvive', +($event.target as HTMLInputElement).value)"
                    />
                  </label>
                </div>
              </div>

              <div v-if="(model.campaign.objectives ?? []).length === 0" class="campaign-objectives__empty">
                No objectives yet. Required objectives gate victory; optional ones are achievements.
              </div>
            </div>
          </template>
        </div>
      </section>

      <!-- ── Zones section ───────────────────────────────────────────────────── -->
      <section class="editor-section" :class="{ 'editor-section--open': openSection === 'zones' }">
        <button type="button" class="editor-section__summary" @click="toggleSection('zones')">
          Zones
        </button>
        <div v-if="openSection === 'zones'" class="editor-section__body">

          <!-- Add Zone / placement hint -->
          <div class="zone-sidebar__add-row">
            <button
              type="button"
              :class="{ 'zone-sidebar__add--active': zoneSubMode === 'place' }"
              @click="zoneSubMode = zoneSubMode === 'place' ? 'idle' : 'place'"
            >Add Zone</button>
            <span v-if="zoneSubMode === 'place'" class="zone-sidebar__hint">
              Click the map to place the new zone
            </span>
          </div>

          <!-- Zone list -->
          <div class="zone-sidebar__list">
            <template v-if="(model.zones ?? []).length === 0">
              <div class="zone-sidebar__empty">No zones yet — click Add Zone</div>
            </template>
            <template v-else>
              <button
                v-for="zone in (model.zones ?? [])"
                :key="zone.id"
                type="button"
                class="zone-sidebar__row"
                :class="{ 'zone-sidebar__row--active': zone.id === selectedZoneId }"
                @click="selectZoneFromList(zone.id)"
              >
                <span class="zone-sidebar__row-name">{{ zone.name || zone.id }}</span>
                <span class="zone-sidebar__row-type">{{ zone.capture.type }}</span>
              </button>
            </template>
          </div>

          <!-- Per-zone config — shown when a zone is selected -->
          <template v-if="selectedZone">
            <div class="zone-brush-config">
              <div class="zone-brush-config__title">Zone: {{ selectedZone.id }}</div>

              <label for="zone-s-name">Name</label>
              <input
                id="zone-s-name"
                type="text"
                :value="selectedZone.name ?? ''"
                @input="updateSelectedZoneField('name', ($event.target as HTMLInputElement).value || undefined)"
                placeholder="e.g. North Hill"
              />

              <label for="zone-s-lock-to-start">Lock to Start</label>
              <select
                id="zone-s-lock-to-start"
                :value="selectedZone.lockedSpawnLabel ?? ''"
                @change="updateSelectedZoneField('lockedSpawnLabel', ($event.target as HTMLSelectElement).value || undefined)"
              >
                <option value="">(none)</option>
                <option v-for="lbl in availablePlayerLabels" :key="lbl" :value="lbl">{{ lbl }}</option>
              </select>
              <div v-if="selectedZone.lockedSpawnLabel" class="zone-brush-config__hint">
                Home zone — team-owned and not capturable.
              </div>

              <template v-if="!selectedZone.lockedSpawnLabel">
                <label for="zone-s-capture-type">Capture Type</label>
                <!-- Capture types are server-authoritative (ListZoneCaptureTypes()):
                     control_point, presence, clear, claim. -->
                <select
                  id="zone-s-capture-type"
                  :value="selectedZone.capture.type"
                  @change="onZoneCaptureTypeChange(($event.target as HTMLSelectElement).value)"
                >
                  <option value="control_point">Control Point</option>
                  <option value="presence">Presence</option>
                  <option value="clear">Clear</option>
                  <option value="claim">Claim</option>
                </select>

                <template v-if="selectedZone.capture.type === 'presence'">
                  <label for="zone-s-capture-seconds">Capture Duration (seconds)</label>
                  <input
                    id="zone-s-capture-seconds"
                    type="number"
                    min="1"
                    step="1"
                    :value="(selectedZone.capture.config?.['captureSeconds'] as number | undefined) ?? 10"
                    @input="updateZoneCaptureConfig('captureSeconds', +($event.target as HTMLInputElement).value)"
                  />
                </template>

                <template v-if="selectedZone.capture.type === 'claim'">
                  <label for="zone-s-defend-seconds">Defend Duration (seconds)</label>
                  <input
                    id="zone-s-defend-seconds"
                    type="number"
                    min="1"
                    step="1"
                    :value="(selectedZone.capture.config?.['defendSeconds'] as number | undefined) ?? 30"
                    @input="updateZoneCaptureConfig('defendSeconds', +($event.target as HTMLInputElement).value)"
                  />
                  <label for="zone-s-tower-type">Tower Type</label>
                  <select
                    id="zone-s-tower-type"
                    :value="(selectedZone.capture.config?.['towerType'] as string | undefined) ?? 'tower'"
                    @change="updateZoneCaptureConfig('towerType', ($event.target as HTMLSelectElement).value)"
                  >
                    <option value="tower">Tower</option>
                  </select>
                  <div class="zone-brush-config__hint">
                    Build the tower on each 2&#xD7;2 slot, then defend it for the duration to claim the zone.
                    Use Place Capture Point to add slots; click a slot on the map to select it — a popup opens with Move / Delete.
                  </div>
                </template>

                <label for="zone-s-starting-owner">Starting Owner</label>
                <select
                  id="zone-s-starting-owner"
                  :value="selectedZone.startingOwner ?? 'neutral'"
                  @change="updateSelectedZoneField('startingOwner', ($event.target as HTMLSelectElement).value)"
                >
                  <option value="neutral">Neutral</option>
                  <option value="team">Team</option>
                  <option v-for="lbl in availablePlayerLabels" :key="lbl" :value="lbl">{{ lbl }}</option>
                </select>
              </template>

              <!-- Capture prerequisites (directed links) -->
              <div class="zone-brush-config__links">
                <div class="zone-brush-config__links-header">
                  <span class="zone-brush-config__links-label">Capture requires</span>
                  <button
                    type="button"
                    class="zone-brush-config__links-toggle"
                    @click="linkPanelOpen = !linkPanelOpen"
                  >
                    <span v-if="(selectedZone.adjacent ?? []).length === 0">Any zone (ungated)</span>
                    <span v-else>{{ (selectedZone.adjacent ?? []).length }} zone{{ (selectedZone.adjacent ?? []).length !== 1 ? 's' : '' }}</span>
                    <span class="zone-brush-config__links-caret">{{ linkPanelOpen ? '▲' : '▼' }}</span>
                  </button>
                </div>

                <div v-if="linkPanelOpen" class="zone-brush-config__links-panel">
                  <div v-if="otherZones.length === 0" class="zone-brush-config__links-empty">
                    No other zones to link
                  </div>
                  <label
                    v-for="z in otherZones"
                    :key="z.id"
                    class="zone-brush-config__links-row"
                  >
                    <input
                      type="checkbox"
                      :checked="(selectedZone.adjacent ?? []).includes(z.id)"
                      @change="toggleZoneLink(z.id)"
                    />
                    <span>{{ z.name || z.id }}</span>
                  </label>
                </div>

                <label
                  class="zone-brush-config__require-all"
                  :class="{ 'zone-brush-config__require-all--disabled': (selectedZone.adjacent ?? []).length === 0 }"
                >
                  <input
                    type="checkbox"
                    :checked="selectedZone.requireAllLinks ?? false"
                    :disabled="(selectedZone.adjacent ?? []).length === 0"
                    @change="setZoneRequireAllLinks(($event.target as HTMLInputElement).checked)"
                  />
                  <span>Require ALL linked zones <span class="field-hint">(off = any one)</span></span>
                </label>

                <div class="zone-brush-config__hint">
                  <template v-if="(selectedZone.adjacent ?? []).length === 0">
                    Ungated — capturable any time.
                  </template>
                  <template v-else-if="selectedZone.requireAllLinks">
                    Capturable once ALL linked zones are owned.
                  </template>
                  <template v-else>
                    Capturable once ANY linked zone is owned.
                  </template>
                </div>
              </div>

              <!-- Bonuses (zone auras): stat modifiers granted to the owner. -->
              <div class="zone-brush-config__auras">
                <div class="zone-brush-config__auras-header">
                  <span class="zone-brush-config__auras-label">Bonuses</span>
                  <button
                    type="button"
                    class="zone-brush-config__aura-add"
                    @click="addAuraToSelectedZone"
                  >+ Add Bonus</button>
                </div>
                <div v-if="(selectedZone.auras ?? []).length === 0" class="zone-brush-config__hint">
                  No bonuses — this zone grants only vision + build rights.
                </div>
                <div
                  v-for="(aura, ai) in (selectedZone.auras ?? [])"
                  :key="ai"
                  class="zone-brush-config__aura-row"
                >
                  <select
                    :value="aura.modifier.stat"
                    @change="updateSelectedZoneAura(ai, 'stat', ($event.target as HTMLSelectElement).value)"
                  >
                    <option v-for="s in STAT_DEFS" :key="s.id" :value="s.id">{{ s.label }}</option>
                  </select>
                  <select
                    :value="aura.modifier.operation"
                    @change="updateSelectedZoneAura(ai, 'operation', ($event.target as HTMLSelectElement).value)"
                  >
                    <option v-for="op in STAT_OPERATIONS" :key="op" :value="op">{{ op }}</option>
                  </select>
                  <input
                    type="number"
                    step="any"
                    :value="aura.modifier.value"
                    @input="updateSelectedZoneAura(ai, 'value', +($event.target as HTMLInputElement).value)"
                  />
                  <button
                    type="button"
                    class="zone-brush-config__aura-remove"
                    title="Remove bonus"
                    @click="removeAuraFromSelectedZone(ai)"
                  >&#x2715;</button>
                </div>
                <div class="zone-brush-config__hint">
                  add = flat (e.g. +2). multiply = factor (e.g. 1.15 for +15%).
                </div>
              </div>

              <div class="zone-brush-config__actions">
                <button
                  type="button"
                  :class="{ 'zone-brush-config__action--active': zoneSubMode === 'draw' }"
                  @click="zoneSubMode = zoneSubMode === 'draw' ? 'idle' : 'draw'"
                >Draw Zone</button>
                <button
                  v-if="!selectedZone.lockedSpawnLabel && selectedZone.capture.type === 'presence'"
                  type="button"
                  :class="{ 'zone-brush-config__action--active': zoneSubMode === 'captureDraw' }"
                  @click="zoneSubMode = zoneSubMode === 'captureDraw' ? 'idle' : 'captureDraw'"
                >Draw Capture Zone</button>
                <button
                  v-if="!selectedZone.lockedSpawnLabel && selectedZone.capture.type === 'claim'"
                  type="button"
                  :class="{ 'zone-brush-config__action--active': zoneSubMode === 'claimPoint' }"
                  @click="zoneSubMode = zoneSubMode === 'claimPoint' ? 'idle' : 'claimPoint'"
                >Place Capture Point</button>
                <button
                  type="button"
                  :class="{ 'zone-brush-config__action--active': zoneSubMode === 'move' }"
                  @click="zoneSubMode = zoneSubMode === 'move' ? 'idle' : 'move'"
                >Move Node</button>
                <button
                  type="button"
                  class="zone-brush-config__delete"
                  @click="deleteSelectedZone"
                >Delete</button>
              </div>

              <div v-if="zoneSubMode === 'draw'" class="zone-brush-config__hint">
                Left-click adds a cell · Right-click removes · Esc exits
              </div>
              <div v-if="zoneSubMode === 'captureDraw'" class="zone-brush-config__hint">
                Click cells inside the zone to mark the capture area · Right-click removes · Esc exits
              </div>
              <div v-if="zoneSubMode === 'move'" class="zone-brush-config__hint">
                Click a cell inside the zone to move its node · Esc exits
              </div>
              <div v-if="zoneSubMode === 'claimPoint'" class="zone-brush-config__hint">
                Click an empty cell to add a 2&#xD7;2 capture point · Click a point to select it (a popup opens with Move / Delete) · Right-click also removes · Esc exits
              </div>
            </div>
          </template>

        </div>
      </section>

      <section class="editor-section" :class="{ 'editor-section--open': openSection === 'paint' }">
        <button type="button" class="editor-section__summary" @click="toggleSection('paint')">
          Paint
        </button>
        <div v-if="openSection === 'paint'" class="editor-section__body">
          <button
            type="button"
            class="paint-toggle"
            :class="{ 'paint-toggle--active': paintModeEnabled }"
            @click="paintModeEnabled = !paintModeEnabled"
          >
            {{ paintModeEnabled ? 'Painting Enabled' : 'Painting Disabled' }}
          </button>

          <div class="control-group">
            <label for="brush-mode">Brush</label>
            <select id="brush-mode" v-model="brushMode" :disabled="!paintModeEnabled">
              <option value="terrain">Terrain</option>
              <option value="tile">Tile</option>
              <option value="obstacle">Obstacle</option>
              <option value="building">Building</option>
              <option value="enemy-spawn">Enemy Spawn</option>
              <option value="neutral-spawn">Neutral Spawn</option>
              <option value="unit">Unit</option>
              <option value="erase">Erase</option>
            </select>
          </div>

          <div v-if="brushMode !== 'building' && brushMode !== 'enemy-spawn' && brushMode !== 'neutral-spawn' && brushMode !== 'unit'" class="control-group">
            <label for="brush-size">Brush Size</label>
            <select id="brush-size" v-model.number="brushSize" :disabled="!paintModeEnabled">
              <option :value="1">1 × 1</option>
              <option :value="3">3 × 3</option>
              <option :value="5">5 × 5</option>
              <option :value="7">7 × 7</option>
            </select>
          </div>

          <div v-if="brushMode === 'terrain' || brushMode === 'tile'" class="control-group">
            <label for="default-ground">Default Ground</label>
            <select id="default-ground" :value="defaultGroundName" @change="onDefaultGroundChange">
              <option value="grass">Grass</option>
              <option value="dirt">Dirt</option>
            </select>
          </div>

          <div v-if="brushMode === 'terrain'" class="control-group">
            <label for="terrain-type">Terrain Type</label>
            <select id="terrain-type" v-model="selectedTerrain" :disabled="!paintModeEnabled">
              <option value="grass">Grass</option>
              <option value="dirt">Dirt</option>
            </select>
          </div>

          <div v-if="brushMode === 'tile'" class="control-group">
            <label for="tile-sheet">Tile Sheet</label>
            <select id="tile-sheet" v-model="selectedTileSheet" :disabled="!paintModeEnabled">
              <option v-for="sheet in TILE_SHEET_NAMES" :key="sheet" :value="sheet">
                {{ sheet }}
              </option>
            </select>
            <div class="tile-picker-hint">
              {{ selectedTileCoord
                ? `Selected: (${selectedTileCoord.sx}, ${selectedTileCoord.sy}) — right-click a cell to erase`
                : 'Click a tile below to select it' }}
            </div>
            <canvas
              ref="tilePickerCanvas"
              class="tile-picker"
              @click="onTilePickerClick"
            />
          </div>

          <div v-if="brushMode === 'obstacle'" class="control-group">
            <label for="obstacle-type">Obstacle Type</label>
            <select id="obstacle-type" v-model="selectedObstacle" :disabled="!paintModeEnabled">
              <option value="rock">Rock</option>
              <option value="wall">Wall</option>
              <option value="tree">Tree</option>
            </select>
          </div>

          <div v-if="brushMode === 'building'" class="control-group">
            <label for="building-type">Building Type</label>
            <select id="building-type" v-model="selectedBuilding" :disabled="!paintModeEnabled">
              <option v-for="def in paintableBuildingDefs" :key="def.type" :value="def.type">
                {{ def.label || def.type }}
              </option>
            </select>
          </div>

          <div
            v-if="brushMode === 'building' && isPlayerOwnableBuildingType(selectedBuilding)"
            class="control-group"
          >
            <label for="building-player-label">Player Slot</label>
            <select id="building-player-label" v-model="buildingPlayerLabel" :disabled="!paintModeEnabled">
              <option value="">Unassigned</option>
              <option v-for="lbl in availablePlayerLabels" :key="lbl" :value="lbl">{{ lbl }}</option>
            </select>
          </div>

          <div v-if="brushMode === 'building' && selectedBuilding === 'spawn-point'" class="control-group spawn-point-config">
            <label for="spawn-point-townhall">Townhall</label>
            <select
              id="spawn-point-townhall"
              v-model="selectedSpawnTownhallId"
              :disabled="!paintModeEnabled || townhallOptions.length === 0"
            >
              <option value="">Nearest / Unassigned</option>
              <option v-for="townhall in townhallOptions" :key="townhall.id" :value="townhall.id">
                {{ townhall.label }}
              </option>
            </select>
            <label for="spawn-point-fill-order">Fill Order</label>
            <input
              id="spawn-point-fill-order"
              v-model.number="spawnPointFillOrder"
              type="number"
              min="0"
              :disabled="!paintModeEnabled"
            />
            <label for="spawn-point-player-label">Player Label</label>
            <input
              id="spawn-point-player-label"
              v-model="spawnPointPlayerLabel"
              type="text"
              placeholder="e.g. player1"
              :disabled="!paintModeEnabled"
            />
          </div>

          <div v-if="brushMode === 'enemy-spawn'" class="control-group enemy-spawn-config">
            <label for="enemy-unit-type">Unit Type</label>
            <select
              id="enemy-unit-type"
              v-model="enemyUnitType"
              :disabled="!paintModeEnabled"
            >
              <option v-for="u in ENEMY_SPAWN_UNITS" :key="u.type" :value="u.type">{{ u.label }}</option>
            </select>
            <label for="enemy-spawn-alliance">Spawn Alliance</label>
            <select id="enemy-spawn-alliance" v-model="enemySpawnAlliance" :disabled="!paintModeEnabled">
              <option value="enemy">Enemy</option>
              <option value="neutral">Neutral</option>
            </select>
            <span class="field-hint">Neutral-aligned units won't fight neutral camps.</span>
            <label for="enemy-wave-mode">Spawn Timing</label>
            <select id="enemy-wave-mode" v-model="enemyWaveMode" :disabled="!paintModeEnabled">
              <option value="gameStart">Game Start</option>
              <option value="always">Always (legacy)</option>
              <option value="specific">Specific Wave</option>
              <option value="repeating">Every Wave From</option>
              <option value="interval">Every Nth Wave</option>
              <option value="capture">While Zone Being Captured</option>
            </select>
            <template v-if="enemyWaveMode === 'specific' || enemyWaveMode === 'repeating'">
              <label for="enemy-wave-number">{{ enemyWaveMode === 'specific' ? 'Wave Number' : 'Starting Wave' }}</label>
              <input
                id="enemy-wave-number"
                v-model.number="enemyWaveNumber"
                type="number"
                min="1"
                max="999"
                :disabled="!paintModeEnabled"
              />
            </template>
            <template v-if="enemyWaveMode === 'interval'">
              <label for="enemy-wave-interval">Interval (every Nth wave)</label>
              <input
                id="enemy-wave-interval"
                v-model.number="enemyWaveInterval"
                type="number"
                min="1"
                max="999"
                :disabled="!paintModeEnabled"
              />
            </template>
            <template v-if="enemyWaveMode !== 'gameStart'">
              <label for="enemy-spawn-once" class="checkbox-label">
                <input
                  id="enemy-spawn-once"
                  type="checkbox"
                  v-model="enemySpawnOnce"
                  :disabled="!paintModeEnabled"
                />
                Spawn Once
              </label>
              <label for="enemy-spawn-delay">Spawn Delay (sec)</label>
              <input
                id="enemy-spawn-delay"
                v-model.number="enemySpawnDelay"
                type="number"
                min="0"
                max="3600"
                :disabled="!paintModeEnabled || enemySpawnOnce"
              />
              <label for="enemy-spawn-interval">Spawn Interval (sec)</label>
              <input
                id="enemy-spawn-interval"
                v-model.number="enemySpawnInterval"
                type="number"
                min="1"
                max="3600"
                :disabled="!paintModeEnabled || enemySpawnOnce"
              />
            </template>
            <label for="enemy-ignore-wave-clear" class="checkbox-label">
              <input
                id="enemy-ignore-wave-clear"
                type="checkbox"
                v-model="enemyIgnoreWaveClear"
                :disabled="!paintModeEnabled"
              />
              Ignore Wave Clear
            </label>
            <label for="enemy-spawn-count">Spawn Count</label>
            <input
              id="enemy-spawn-count"
              v-model.number="enemySpawnCount"
              type="number"
              min="1"
              max="20"
              :disabled="!paintModeEnabled"
            />
            <template v-if="killUnitObjectives.length">
              <label for="enemy-objective-id">Kill Objective <span class="field-hint">(optional)</span></label>
              <select id="enemy-objective-id" v-model="enemyObjectiveId" :disabled="!paintModeEnabled">
                <option value="">None</option>
                <option v-for="vc in killUnitObjectives" :key="vc.id" :value="vc.id">
                  {{ vc.label || vc.id }}
                </option>
              </select>
            </template>
            <label for="enemy-target-player">Target Player</label>
            <select id="enemy-target-player" v-model="enemyTargetPlayerLabel" :disabled="!paintModeEnabled">
              <option value="">Default (Nearest Player)</option>
              <option value="__none__">None (Stay at Spawn)</option>
              <option v-for="lbl in availablePlayerLabels" :key="lbl" :value="lbl">{{ lbl }}</option>
            </select>
            <template v-if="enemyWaveMode === 'capture'">
              <label for="enemy-trigger-zone">Capture Zone</label>
              <select id="enemy-trigger-zone" v-model="enemyTriggerCaptureZoneId" :disabled="!paintModeEnabled">
                <option value="">(select a zone)</option>
                <option v-for="z in captureTriggerZones" :key="z.id" :value="z.id">{{ z.name || z.id }}</option>
              </select>
              <span class="field-hint">Spawns only while this zone is actively being captured (presence/claim zones).</span>
            </template>
          </div>

          <div v-if="brushMode === 'neutral-spawn'" class="control-group neutral-spawn-config">
            <label for="neutral-starting-tier">Starting Tier</label>
            <input
              id="neutral-starting-tier"
              v-model.number="neutralStartingTier"
              type="number"
              min="1"
              :disabled="!paintModeEnabled"
            />
            <label for="neutral-tierup-every">Tier Up Every N Waves <span class="field-hint">(0 = off)</span></label>
            <input
              id="neutral-tierup-every"
              v-model.number="neutralTierUpEveryNWaves"
              type="number"
              min="0"
              :disabled="!paintModeEnabled"
            />
            <label for="neutral-group-id">Group</label>
            <select
              id="neutral-group-id"
              v-model="neutralGroupId"
              :disabled="!paintModeEnabled"
            >
              <option :value="NEUTRAL_SPAWN_RANDOM_GROUP_ID">Random</option>
              <option
                v-for="g in groupsForCurrentTier"
                :key="g.id"
                :value="g.id"
              >{{ g.name }}</option>
            </select>
            <label for="neutral-aggro">Aggro Range</label>
            <input id="neutral-aggro" v-model.number="neutralAggroRange" type="number" min="0" :disabled="!paintModeEnabled" />
            <label for="neutral-leash">Leash Range</label>
            <input id="neutral-leash" v-model.number="neutralLeashRange" type="number" min="0" :disabled="!paintModeEnabled" />
            <label for="neutral-hp-base">Wave 1 Health (%)</label>
            <input id="neutral-hp-base" type="number" step="10" min="0" :disabled="!paintModeEnabled"
              :value="Math.round(neutralHealthMultiplier * 100)"
              @input="neutralHealthMultiplier = (+($event.target as HTMLInputElement).value) / 100" />
            <label for="neutral-hp-perwave">Health Increase Per Wave (%)</label>
            <input id="neutral-hp-perwave" type="number" step="10" min="0" :disabled="!paintModeEnabled"
              :value="Math.round(neutralHealthMultiplierPerWave * 100)"
              @input="neutralHealthMultiplierPerWave = (+($event.target as HTMLInputElement).value) / 100" />
            <label for="neutral-dmg-base">Wave 1 Damage (%)</label>
            <input id="neutral-dmg-base" type="number" step="10" min="0" :disabled="!paintModeEnabled"
              :value="Math.round(neutralDamageMultiplier * 100)"
              @input="neutralDamageMultiplier = (+($event.target as HTMLInputElement).value) / 100" />
            <label for="neutral-dmg-perwave">Damage Increase Per Wave (%)</label>
            <input id="neutral-dmg-perwave" type="number" step="10" min="0" :disabled="!paintModeEnabled"
              :value="Math.round(neutralDamageMultiplierPerWave * 100)"
              @input="neutralDamageMultiplierPerWave = (+($event.target as HTMLInputElement).value) / 100" />
          </div>

          <div v-if="brushMode === 'unit'" class="control-group unit-brush-config">
            <label>Faction</label>
            <select v-model="placedUnitFaction" :disabled="!paintModeEnabled">
              <option v-for="f in availableFactions" :key="f" :value="f">{{ formatFactionLabel(f) }}</option>
            </select>

            <label for="placed-unit-player-slot">Player Slot</label>
            <select id="placed-unit-player-slot" v-model="placedUnitPlayerSlot" :disabled="!paintModeEnabled">
              <option v-for="lbl in placedUnitPlayerSlots" :key="lbl" :value="lbl">{{ lbl }}</option>
            </select>

            <label for="placed-unit-type">Unit Type</label>
            <select id="placed-unit-type" v-model="placedUnitType" :disabled="!paintModeEnabled">
              <option v-for="u in unitTypesForBrushFaction" :key="u.type" :value="u.type">{{ u.label }}</option>
            </select>

            <template v-if="placedUnitPlayerSlot === 'enemy'">
              <label for="placed-unit-aggro-range">Aggro Range</label>
              <input
                id="placed-unit-aggro-range"
                v-model.number="placedUnitAggroRange"
                type="number"
                min="0"
                max="1000"
                :disabled="!paintModeEnabled"
              />
              <label for="placed-unit-leash-range">Leash Range</label>
              <input
                id="placed-unit-leash-range"
                v-model.number="placedUnitLeashRange"
                type="number"
                min="0"
                max="1000"
                :disabled="!paintModeEnabled"
              />
            </template>
          </div>

          <div v-if="brushMode === 'building' && destroyBuildingObjectives.length" class="control-group">
            <label for="building-objective-id">Destroy Objective <span class="field-hint">(optional)</span></label>
            <select id="building-objective-id" v-model="buildingObjectiveId" :disabled="!paintModeEnabled">
              <option value="">None</option>
              <option v-for="vc in destroyBuildingObjectives" :key="vc.id" :value="vc.id">
                {{ vc.label || vc.id }}
              </option>
            </select>
          </div>

          <div class="hint-list">
            <div>`Wheel` zooms</div>
            <div>`Middle mouse` pans</div>
            <div>`Space + left drag` pans</div>
            <div>`Left click/drag` paints when enabled</div>
            <div>`Hold Control` temporarily erases</div>
            <div>`Erase` removes buildings too</div>
          </div>
        </div>
      </section>

      <section class="editor-section" :class="{ 'editor-section--open': openSection === 'export' }">
        <button type="button" class="editor-section__summary" @click="toggleSection('export')">
          Export
        </button>
        <div v-if="openSection === 'export'" class="editor-section__body">
          <div class="export-actions">
            <button type="button" @click="recenterCamera">Recenter</button>
            <button type="button" @click="clearMap">Clear Map</button>
            <button type="button" @click="copyExport">{{ copiedLabel }}</button>
            <button
              type="button"
              class="save-to-server"
              :class="{ 'save-to-server--saved': saveLabel === 'Saved!' }"
              @click="saveToServer"
              :disabled="saveLabel === 'Saving...'"
            >{{ saveLabel }}</button>
          </div>

          <div v-if="saveError" class="save-error">{{ saveError }}</div>

          <textarea
            class="export-box"
            :value="serializedMap"
            readonly
            spellcheck="false"
          ></textarea>
        </div>
      </section>
    </div>

    <div class="editor-preview">
      <div class="preview-header">
        <span v-if="movingBuilding" class="preview-header__paste-mode">
          Move mode — click to place {{ movingBuilding.buildingType }}
          <button type="button" class="preview-header__cancel-paste" @click="cancelMoveBuilding">Cancel (Esc)</button>
        </span>
        <span v-else-if="movingNeutralSpawn" class="preview-header__paste-mode">
          Move mode — click to place neutral spawn
          <button type="button" class="preview-header__cancel-paste" @click="cancelMoveNeutralSpawn">Cancel (Esc)</button>
        </span>
        <span v-else-if="isPasteMode" class="preview-header__paste-mode">
          Paste mode — click to place {{ copiedBuilding?.buildingType }}
          <button type="button" class="preview-header__cancel-paste" @click="isPasteMode = false">Cancel (Esc)</button>
        </span>
        <span v-else>{{ paintModeEnabled ? 'Paint mode armed' : 'Navigation mode' }}</span>
        <span>{{ hoverLabel }}</span>
      </div>

      <div class="canvas-frame">
        <canvas v-show="!playtestPlaying" ref="canvas" class="editor-canvas"></canvas>
        <canvas
          v-show="!playtestPlaying"
          ref="minimapCanvas"
          class="editor-minimap"
          aria-label="Map minimap (click or drag to jump)"
          @mousedown="onMinimapMouseDown"
          @mousemove="onMinimapMouseMove"
          @mouseup="onMinimapMouseUp"
          @mouseleave="onMinimapMouseUp"
        ></canvas>
        <canvas v-show="playtestPlaying" ref="playCanvas" class="we-play-canvas"></canvas>
        <PlaytestBar v-if="playtestPlaying" @stop="stopPlaytest" />
        <div v-if="selectedEditBuilding && editPanelPos" class="edit-panel" :style="editPanelStyle">
          <div class="edit-panel__header">
            <span class="edit-panel__title">{{ selectedEditBuilding.buildingType }}</span>
            <button type="button" class="edit-panel__copy" @click="copySelectedBuilding" title="Copy — then click canvas to paste">Copy</button>
            <button type="button" class="edit-panel__move" @click="startMoveBuilding" title="Move — then click canvas to place">Move</button>
            <button type="button" class="edit-panel__close" @click="selectedEditBuildingId = null">✕</button>
          </div>

          <!-- enemy-spawnpoint fields -->
          <template v-if="selectedEditBuilding.buildingType === 'enemy-spawnpoint'">
            <div class="edit-field">
              <label>Unit Type</label>
              <select :value="selectedEditBuilding.metadata?.['unitType'] ?? 'raider'" @change="updateEditMeta('unitType', ($event.target as HTMLSelectElement).value)">
                <option v-for="u in ENEMY_SPAWN_UNITS" :key="u.type" :value="u.type">{{ u.label }}</option>
              </select>
            </div>
            <div class="edit-field">
              <label>Spawn Alliance</label>
              <select :value="selectedEditBuilding.metadata?.['spawnAlliance'] ?? 'enemy'" @change="updateEditMeta('spawnAlliance', ($event.target as HTMLSelectElement).value === 'neutral' ? 'neutral' : undefined)">
                <option value="enemy">Enemy</option>
                <option value="neutral">Neutral</option>
              </select>
              <span class="edit-field__hint">Neutral-aligned units won't fight neutral camps.</span>
            </div>
            <div class="edit-field">
              <label>Spawn Timing</label>
              <select :value="editWaveMode" @change="updateEditWaveMode(($event.target as HTMLSelectElement).value as 'gameStart'|'always'|'specific'|'repeating'|'interval'|'capture', editWaveNumber)">
                <option value="gameStart">Game Start</option>
                <option value="always">Always</option>
                <option value="specific">Specific Wave</option>
                <option value="repeating">Every Wave From</option>
                <option value="interval">Every Nth Wave</option>
                <option value="capture">While Zone Being Captured</option>
              </select>
            </div>
            <div v-if="editWaveMode === 'specific' || editWaveMode === 'repeating' || editWaveMode === 'interval'" class="edit-field">
              <label>{{ editWaveMode === 'specific' ? 'Wave Number' : editWaveMode === 'repeating' ? 'Starting Wave' : 'Interval (every Nth wave)' }}</label>
              <input type="number" min="1" max="999" :value="editWaveNumber" @input="updateEditWaveMode(editWaveMode, +($event.target as HTMLInputElement).value)" />
            </div>
            <template v-if="editWaveMode !== 'gameStart'">
            <div class="edit-field">
              <label class="checkbox-label">
                <input
                  type="checkbox"
                  :checked="selectedEditBuilding.metadata?.['spawnOnce'] === true"
                  @change="updateEditMeta('spawnOnce', ($event.target as HTMLInputElement).checked || undefined)"
                />
                Spawn Once
              </label>
            </div>
            <div class="edit-field">
              <label>Spawn Delay (sec)</label>
              <input type="number" min="0" :value="selectedEditBuilding.metadata?.['spawnDelaySeconds'] ?? 60" @input="updateEditMeta('spawnDelaySeconds', +($event.target as HTMLInputElement).value)" :disabled="selectedEditBuilding.metadata?.['spawnOnce'] === true" />
            </div>
            <div class="edit-field">
              <label>Spawn Interval (sec)</label>
              <input type="number" min="1" :value="selectedEditBuilding.metadata?.['spawnIntervalSeconds'] ?? 10" @input="updateEditMeta('spawnIntervalSeconds', +($event.target as HTMLInputElement).value)" :disabled="selectedEditBuilding.metadata?.['spawnOnce'] === true" />
            </div>
            </template>
            <div class="edit-field">
              <label class="checkbox-label">
                <input
                  type="checkbox"
                  :checked="selectedEditBuilding.metadata?.['ignoreWaveClear'] === true"
                  @change="updateEditMeta('ignoreWaveClear', ($event.target as HTMLInputElement).checked || undefined)"
                />
                Ignore Wave Clear
              </label>
            </div>
            <div class="edit-field">
              <label>Spawn Count</label>
              <input type="number" min="1" max="20" :value="selectedEditBuilding.metadata?.['spawnCount'] ?? 1" @input="updateEditMeta('spawnCount', +($event.target as HTMLInputElement).value)" />
            </div>
            <div class="edit-field">
              <label>Wave 1 Health (%)</label>
              <input type="number" min="0" step="10" :value="Math.round(((selectedEditBuilding.metadata?.['healthMultiplier'] as number) ?? 1) * 100)" @input="updateEditMeta('healthMultiplier', (+($event.target as HTMLInputElement).value) / 100)" />
            </div>
            <div class="edit-field">
              <label>Health Increase Per Wave (%)</label>
              <input type="number" min="0" step="10" :value="Math.round(((selectedEditBuilding.metadata?.['healthMultiplierPerWave'] as number) ?? 0) * 100)" @input="updateEditMeta('healthMultiplierPerWave', (+($event.target as HTMLInputElement).value) / 100 || undefined)" />
            </div>
            <div class="edit-field">
              <label>Wave 1 Damage (%)</label>
              <input type="number" min="0" step="10" :value="Math.round(((selectedEditBuilding.metadata?.['damageMultiplier'] as number) ?? 1) * 100)" @input="updateEditMeta('damageMultiplier', (+($event.target as HTMLInputElement).value) / 100)" />
            </div>
            <div class="edit-field">
              <label>Damage Increase Per Wave (%)</label>
              <input type="number" min="0" step="10" :value="Math.round(((selectedEditBuilding.metadata?.['damageMultiplierPerWave'] as number) ?? 0) * 100)" @input="updateEditMeta('damageMultiplierPerWave', (+($event.target as HTMLInputElement).value) / 100 || undefined)" />
            </div>
            <div v-if="killUnitObjectives.length" class="edit-field">
              <label>Kill Objective</label>
              <select :value="selectedEditBuilding.metadata?.['objectiveId'] ?? ''" @change="updateEditMeta('objectiveId', ($event.target as HTMLSelectElement).value || undefined)">
                <option value="">None</option>
                <option v-for="vc in killUnitObjectives" :key="vc.id" :value="vc.id">{{ vc.label || vc.id }}</option>
              </select>
            </div>
            <div class="edit-field">
              <label>Target Player</label>
              <select :value="selectedEditBuilding.metadata?.['targetPlayerLabel'] ?? ''" @change="updateEditMeta('targetPlayerLabel', ($event.target as HTMLSelectElement).value || undefined)">
                <option value="">Default (Nearest Player)</option>
                <option value="__none__">None (Stay at Spawn)</option>
                <option v-for="lbl in availablePlayerLabels" :key="lbl" :value="lbl">{{ lbl }}</option>
              </select>
            </div>
            <div v-if="editWaveMode === 'capture'" class="edit-field">
              <label>Capture Zone</label>
              <select
                :value="selectedEditBuilding.metadata?.['triggerCaptureZoneId'] ?? ''"
                @change="updateEditMeta('triggerCaptureZoneId', ($event.target as HTMLSelectElement).value || undefined)"
              >
                <option v-for="z in captureTriggerZones" :key="z.id" :value="z.id">{{ z.name || z.id }}</option>
              </select>
              <span class="edit-field__hint">Spawns only while this zone is actively being captured (presence/claim zones).</span>
            </div>
          </template>

          <!-- spawn-point fields -->
          <template v-else-if="selectedEditBuilding.buildingType === 'spawn-point'">
            <div class="edit-field">
              <label>Townhall</label>
              <select :value="selectedEditBuilding.metadata?.['townhallId'] ?? ''" @change="updateEditMeta('townhallId', ($event.target as HTMLSelectElement).value || null)">
                <option value="">Nearest / Unassigned</option>
                <option v-for="th in townhallOptions" :key="th.id" :value="th.id">{{ th.label }}</option>
              </select>
            </div>
            <div class="edit-field">
              <label>Fill Order</label>
              <input type="number" min="0" :value="selectedEditBuilding.metadata?.['fillOrder'] ?? 0" @input="updateEditMeta('fillOrder', +($event.target as HTMLInputElement).value)" />
            </div>
            <div class="edit-field">
              <label>Player Label</label>
              <input type="text" placeholder="e.g. player1" :value="selectedEditBuilding.metadata?.['playerLabel'] ?? ''" @input="updateEditMeta('playerLabel', ($event.target as HTMLInputElement).value || undefined)" />
            </div>
          </template>

          <!-- generic building: objective + (for player-class) owner slot -->
          <template v-else>
            <div v-if="isPlayerOwnableBuildingType(selectedEditBuilding.buildingType)" class="edit-field">
              <label>Player Slot</label>
              <select
                :value="(selectedEditBuilding.metadata?.['playerLabel'] as string | undefined) ?? ''"
                @change="updateEditMeta('playerLabel', ($event.target as HTMLSelectElement).value || undefined)"
              >
                <option value="">Unassigned</option>
                <option v-for="lbl in availablePlayerLabels" :key="lbl" :value="lbl">{{ lbl }}</option>
              </select>
            </div>
            <div v-if="destroyBuildingObjectives.length" class="edit-field">
              <label>Destroy Objective</label>
              <select :value="selectedEditBuilding.metadata?.['objectiveId'] ?? ''" @change="updateEditMeta('objectiveId', ($event.target as HTMLSelectElement).value || undefined)">
                <option value="">None</option>
                <option v-for="vc in destroyBuildingObjectives" :key="vc.id" :value="vc.id">{{ vc.label || vc.id }}</option>
              </select>
            </div>
            <div v-if="selectedEditBuilding.resourceType" class="edit-field">
              <label>{{ selectedEditBuilding.resourceType === 'gold' ? 'Gold Amount' : 'Resource Amount' }}</label>
              <input
                type="number"
                min="0"
                step="100"
                :value="selectedEditBuilding.resourceAmount ?? 0"
                @input="updateEditBuildingResourceAmount(+($event.target as HTMLInputElement).value)"
              />
              <span class="edit-field__hint">Starting stock workers can mine. 0 uses the building default.</span>
            </div>
            <!-- Shop Guards: opt-in neutral squad that locks a neutral-shop /
                 recipe-shop until cleared. No guardGroupId = open shop. -->
            <template v-if="isShopGuardableBuildingType(selectedEditBuilding.buildingType)">
              <div class="edit-field">
                <label>Guard Squad</label>
                <select
                  :value="(selectedEditBuilding.metadata?.['guardGroupId'] as string | undefined) ?? ''"
                  @change="updateEditMeta('guardGroupId', ($event.target as HTMLSelectElement).value || undefined)"
                >
                  <option value="">None (open shop)</option>
                  <option v-for="g in guardGroupOptions" :key="g.id" :value="g.id">{{ g.name || g.id }}</option>
                </select>
                <span class="edit-field__hint">A neutral squad that locks the shop until cleared. Leave as None to place an unguarded shop.</span>
              </div>
              <template v-if="selectedEditBuilding.metadata?.['guardGroupId']">
                <div class="edit-field">
                  <label>Guard Tier</label>
                  <input type="number" min="1" max="10" :value="(selectedEditBuilding.metadata?.['guardStartingTier'] as number | undefined) ?? 1" @input="updateEditMeta('guardStartingTier', +($event.target as HTMLInputElement).value || undefined)" />
                  <span class="edit-field__hint">The squad must exist at this tier or the guards won't spawn.</span>
                </div>
                <div class="edit-field">
                  <label>Aggro Range</label>
                  <input type="number" min="0" step="10" :value="(selectedEditBuilding.metadata?.['guardAggroRange'] as number | undefined) ?? 0" @input="updateEditMeta('guardAggroRange', +($event.target as HTMLInputElement).value || undefined)" />
                </div>
                <div class="edit-field">
                  <label>Leash Range</label>
                  <input type="number" min="0" step="10" :value="(selectedEditBuilding.metadata?.['guardLeashRange'] as number | undefined) ?? 0" @input="updateEditMeta('guardLeashRange', +($event.target as HTMLInputElement).value || undefined)" />
                </div>
                <div class="edit-field">
                  <label>Guard Spawn</label>
                  <div class="guard-spawn-controls">
                    <button
                      type="button"
                      class="guard-spawn-btn"
                      :class="{ 'guard-spawn-btn--active': placingGuardSpawn }"
                      @click="togglePlaceGuardSpawn"
                    >{{ placingGuardSpawn ? 'Click a map cell…' : 'Place Guard Spawn' }}</button>
                    <button v-if="hasGuardSpawn" type="button" class="guard-spawn-btn" @click="clearGuardSpawn">Clear</button>
                  </div>
                  <span class="edit-field__hint">{{ guardSpawnLabel }}</span>
                </div>
              </template>
            </template>
            <!-- Neutral Shop (merchant): art style + which item list the shop stocks from. -->
            <template v-if="selectedEditBuilding.buildingType === 'neutral-shop'">
              <div class="edit-field">
                <label>Shop Style</label>
                <select
                  :value="(selectedEditBuilding.metadata?.['shopStyle'] as string | undefined) ?? ''"
                  @change="updateEditMeta('shopStyle', ($event.target as HTMLSelectElement).value || undefined)"
                >
                  <option value="">Default</option>
                  <option v-for="style in neutralShopStyleOptions" :key="style" :value="style">{{ style }}</option>
                </select>
                <span class="edit-field__hint">Sprite art from assets/buildings/neutral-shops. Default uses neutral-shops/sprite.png.</span>
              </div>
              <div class="edit-field">
                <label>Item List</label>
                <select
                  :value="(selectedEditBuilding.metadata?.['itemList'] as string | undefined) ?? ''"
                  @change="updateEditMeta('itemList', ($event.target as HTMLSelectElement).value || undefined)"
                >
                  <option value="">Default (merchant loot)</option>
                  <option v-for="list in itemListOptions" :key="list.id" :value="list.id">{{ list.name || list.id }}</option>
                </select>
                <span class="edit-field__hint">A pool the shop samples a few items from. Default rolls the merchant loot table.</span>
              </div>
              <div class="edit-field">
                <label>Reroll Every (waves)</label>
                <input
                  type="number" min="0"
                  :value="(selectedEditBuilding.metadata?.['rerollWaves'] as number | undefined) ?? ''"
                  @input="updateEditMeta('rerollWaves', ($event.target as HTMLInputElement).value === '' ? undefined : Math.max(0, Math.floor(+($event.target as HTMLInputElement).value || 0)))"
                />
                <span class="edit-field__hint">Auto-refresh cadence for this shop's stock. Blank = tuning default; 0 disables.</span>
              </div>
            </template>
            <!-- Recipe Shop: art style + which recipe list the shop stocks from. -->
            <template v-if="selectedEditBuilding.buildingType === 'recipe-shop'">
              <div class="edit-field">
                <label>Shop Style</label>
                <select
                  :value="(selectedEditBuilding.metadata?.['shopStyle'] as string | undefined) ?? ''"
                  @change="updateEditMeta('shopStyle', ($event.target as HTMLSelectElement).value || undefined)"
                >
                  <option value="">Default</option>
                  <option v-for="style in shopStyleOptions" :key="style" :value="style">{{ style }}</option>
                </select>
                <span class="edit-field__hint">Sprite art from assets/buildings/recipe-shops. Default uses the shared recipe-shop sprite.</span>
              </div>
              <div class="edit-field">
                <label>Recipe List</label>
                <select
                  :value="(selectedEditBuilding.metadata?.['recipeList'] as string | undefined) ?? ''"
                  @change="updateEditMeta('recipeList', ($event.target as HTMLSelectElement).value || undefined)"
                >
                  <option value="">All recipes</option>
                  <option v-for="list in recipeListOptions" :key="list.id" :value="list.id">{{ list.name || list.id }}</option>
                </select>
                <span class="edit-field__hint">The shop stocks a random sample from this list. All recipes = draw from the global pool.</span>
              </div>
            </template>
            <div
              v-if="!isPlayerOwnableBuildingType(selectedEditBuilding.buildingType) && !destroyBuildingObjectives.length && !selectedEditBuilding.resourceType && !isShopGuardableBuildingType(selectedEditBuilding.buildingType)"
              class="edit-panel__empty"
            >No editable fields for this building type.</div>
          </template>

          <button type="button" class="edit-delete-btn" @click="deleteSelectedBuilding">Delete Building</button>
        </div>
      </div>

      <!-- Placed unit edit panel -->
      <div v-if="selectedEditPlacedUnit" class="edit-panel placed-unit-edit-panel" :style="placedUnitEditPanelStyle">
        <div class="edit-panel__header">
          <span class="edit-panel__title">
            {{ selectedEditPlacedUnit.playerSlot === 'enemy' ? 'Enemy Unit' : 'Player Unit' }}
          </span>
          <button type="button" class="edit-panel__close" @click="selectedEditPlacedUnitId = null">&#x2715;</button>
        </div>
        <div class="edit-panel__body">

          <!-- Faction (drives the unit-type dropdown below; derived from the
               current unit's type, not stored on the placed instance). -->
          <div class="edit-field">
            <label>Faction</label>
            <select
              :value="factionForUnitType(selectedEditPlacedUnit.unitType)"
              @change="onEditPanelFactionChange(($event.target as HTMLSelectElement).value as UnitFaction)"
            >
              <option v-for="f in availableFactions" :key="f" :value="f">{{ formatFactionLabel(f) }}</option>
            </select>
          </div>

          <!-- Player slot -->
          <div class="edit-field">
            <label>Player Slot</label>
            <select
              :value="selectedEditPlacedUnit.playerSlot"
              @change="updateSelectedPlacedUnit({ playerSlot: ($event.target as HTMLSelectElement).value })"
            >
              <option v-for="slot in placedUnitPlayerSlots" :key="slot" :value="slot">{{ slot }}</option>
            </select>
          </div>

          <!-- Unit type, filtered to current faction -->
          <div class="edit-field">
            <label>Unit Type</label>
            <select
              :value="selectedEditPlacedUnit.unitType"
              @change="updateSelectedPlacedUnit({ unitType: ($event.target as HTMLSelectElement).value })"
            >
              <option
                v-for="u in unitDefsByFaction[factionForUnitType(selectedEditPlacedUnit.unitType)]"
                :key="u.type"
                :value="u.type"
              >{{ u.label }}</option>
            </select>
          </div>

          <!-- Enemy-slot only: aggro and leash range -->
          <template v-if="selectedEditPlacedUnit.playerSlot === 'enemy'">
            <div class="edit-field">
              <label>Aggro Range (px)</label>
              <input
                type="number" min="0" max="1000"
                :value="selectedEditPlacedUnit.aggroRange ?? 150"
                @input="updateSelectedPlacedUnit({ aggroRange: +($event.target as HTMLInputElement).value })"
              />
            </div>
            <div class="edit-field">
              <label>Leash Range (px)</label>
              <input
                type="number" min="0" max="1000"
                :value="selectedEditPlacedUnit.leashRange ?? 200"
                @input="updateSelectedPlacedUnit({ leashRange: +($event.target as HTMLInputElement).value })"
              />
            </div>
          </template>

          <!-- Rank / Items / Perks instance data — applied by the server at
               spawn for BOTH player and enemy placed units, so these controls
               are shown regardless of playerSlot. -->
          <div class="edit-field">
            <label>Rank</label>
            <select
              :value="selectedEditPlacedUnit.rank ?? ''"
              @change="onInstanceRankChange(($event.target as HTMLSelectElement).value)"
            >
              <option value="">None</option>
              <option v-for="r in selectedInstanceRankOptions" :key="r" :value="r">{{ r }}</option>
            </select>
          </div>

          <div class="edit-field">
            <label>Items</label>
            <div
              v-for="(itemId, idx) in selectedEditPlacedUnit.items ?? []"
              :key="`item-${idx}`"
              class="edit-loadout-row"
            >
              <select
                :value="itemId"
                @change="onInstanceItemChange(idx, ($event.target as HTMLSelectElement).value)"
              >
                <option v-for="def in selectedInstanceItemOptions" :key="def.id" :value="def.id">
                  {{ def.displayName }}
                </option>
              </select>
              <button type="button" @click="removeInstanceItem(idx)">&#x2715;</button>
            </div>
            <button
              type="button"
              class="edit-add-btn"
              :disabled="(selectedEditPlacedUnit.items ?? []).length >= selectedInstanceItemOptions.length"
              @click="addInstanceItem"
            >+ Add Item</button>
          </div>

          <div class="edit-field">
            <label>Perks</label>
            <div
              v-for="(perkId, idx) in selectedEditPlacedUnit.perks ?? []"
              :key="`perk-${idx}`"
              class="edit-loadout-row"
            >
              <select
                :value="perkId"
                @change="onInstancePerkChange(idx, ($event.target as HTMLSelectElement).value)"
              >
                <option v-for="def in selectedInstancePerkOptions" :key="def.id" :value="def.id">
                  {{ def.displayName }}
                </option>
              </select>
              <button type="button" @click="removeInstancePerk(idx)">&#x2715;</button>
            </div>
            <button
              type="button"
              class="edit-add-btn"
              :disabled="(selectedEditPlacedUnit.perks ?? []).length >= selectedInstancePerkOptions.length"
              @click="addInstancePerk"
            >+ Add Perk</button>
          </div>

          <button type="button" class="edit-delete-btn" @click="deleteSelectedPlacedUnit">Delete Unit</button>
        </div>
      </div>

      <!-- Neutral spawn edit panel -->
      <div v-if="selectedEditNeutralSpawn" class="edit-panel neutral-spawn-edit-panel" :style="neutralSpawnEditPanelStyle">
        <div class="edit-panel__header">
          <span class="edit-panel__title">Neutral Spawn</span>
          <button type="button" class="edit-panel__move" @click="startMoveNeutralSpawn" title="Move — then click canvas to place">Move</button>
          <button type="button" class="edit-panel__close" @click="selectedEditNeutralSpawnId = null">&#x2715;</button>
        </div>
        <div class="edit-panel__body">
          <div class="edit-field">
            <label>Starting Tier</label>
            <input
              type="number" min="1"
              :value="selectedEditNeutralSpawn.startingTier ?? 1"
              @input="updateSelectedNeutralSpawn({ startingTier: Math.max(1, +($event.target as HTMLInputElement).value) })"
            />
          </div>
          <div class="edit-field">
            <label>Tier Up Every N Waves (0 = off)</label>
            <input
              type="number" min="0"
              :value="selectedEditNeutralSpawn.tierUpEveryNWaves ?? 0"
              @input="updateSelectedNeutralSpawn({ tierUpEveryNWaves: Math.max(0, +($event.target as HTMLInputElement).value) })"
            />
          </div>
          <div class="edit-field">
            <label>Group</label>
            <select
              :value="selectedEditNeutralSpawn.groupId"
              @change="updateSelectedNeutralSpawn({ groupId: ($event.target as HTMLSelectElement).value })"
            >
              <option :value="NEUTRAL_SPAWN_RANDOM_GROUP_ID">Random</option>
              <option
                v-for="g in editNeutralGroupsForTier"
                :key="g.id"
                :value="g.id"
              >{{ g.name }}</option>
            </select>
          </div>
          <div class="edit-field">
            <label>Aggro Range (px)</label>
            <input
              type="number" min="0"
              :value="selectedEditNeutralSpawn.aggroRange ?? 150"
              @input="updateSelectedNeutralSpawn({ aggroRange: +($event.target as HTMLInputElement).value })"
            />
          </div>
          <div class="edit-field">
            <label>Leash Range (px)</label>
            <input
              type="number" min="0"
              :value="selectedEditNeutralSpawn.leashRange ?? 200"
              @input="updateSelectedNeutralSpawn({ leashRange: +($event.target as HTMLInputElement).value })"
            />
          </div>
          <div class="edit-field">
            <label>Wave 1 Health (%)</label>
            <input
              type="number" min="0" step="10"
              :value="Math.round((selectedEditNeutralSpawn.healthMultiplier ?? 1) * 100)"
              @input="updateSelectedNeutralSpawn({ healthMultiplier: (+($event.target as HTMLInputElement).value) / 100 })"
            />
          </div>
          <div class="edit-field">
            <label>Health Increase Per Wave (%)</label>
            <input
              type="number" min="0" step="10"
              :value="Math.round((selectedEditNeutralSpawn.healthMultiplierPerWave ?? 0) * 100)"
              @input="updateSelectedNeutralSpawn({ healthMultiplierPerWave: (+($event.target as HTMLInputElement).value) / 100 })"
            />
          </div>
          <div class="edit-field">
            <label>Wave 1 Damage (%)</label>
            <input
              type="number" min="0" step="10"
              :value="Math.round((selectedEditNeutralSpawn.damageMultiplier ?? 1) * 100)"
              @input="updateSelectedNeutralSpawn({ damageMultiplier: (+($event.target as HTMLInputElement).value) / 100 })"
            />
          </div>
          <div class="edit-field">
            <label>Damage Increase Per Wave (%)</label>
            <input
              type="number" min="0" step="10"
              :value="Math.round((selectedEditNeutralSpawn.damageMultiplierPerWave ?? 0) * 100)"
              @input="updateSelectedNeutralSpawn({ damageMultiplierPerWave: (+($event.target as HTMLInputElement).value) / 100 })"
            />
          </div>

          <button type="button" class="edit-delete-btn" @click="deleteSelectedNeutralSpawn">Delete Neutral Spawn</button>
        </div>
      </div>

      <div v-if="selectedClaimPointCell" class="edit-panel claim-point-edit-panel" :style="claimPointEditPanelStyle">
        <div class="edit-panel__header">
          <span class="edit-panel__title">Capture Point</span>
          <button type="button" class="edit-panel__move" @click="startMoveClaimPoint" title="Move — then click a cell inside the zone">Move</button>
          <button type="button" class="edit-panel__close" @click="selectedClaimPointIndex = null" title="Deselect">&#x2715;</button>
        </div>
        <div class="edit-panel__body">
          <div v-if="movingClaimPoint" class="edit-panel__empty">Click a cell inside the zone to place it · Esc cancels</div>
          <button type="button" class="edit-delete-btn" @click="deleteSelectedClaimPoint">Delete Point</button>
        </div>
      </div>
    </div>
    </div>

    <div v-if="itemsPopupOpen" class="we-modal-overlay">
      <div class="we-modal we-modal--wide">
        <div class="we-modal__header">
          <span>Item / Equipment Editor</span>
          <UiButton size="sm" @click="itemsPopupOpen = false">Close</UiButton>
        </div>
        <div class="we-modal__body">
          <ItemEditorPanel />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { fetchBuildingDefs, fetchMapCatalog, fetchMapCatalogFile, fetchNeutralGroups, fetchObstacleDefs, fetchRecipeLists, fetchItemLists, fetchUnitDefs, fetchItemDefs, fetchPerkDefs, saveMapCatalogFile, LevelConflictError, type RecipeListSummary, type ItemListSummary } from '@/game/maps/catalog'
import type { LevelConflict } from '@/game/maps/catalog'
import { isShopGuardableBuildingType, allGuardGroups } from '@/game/maps/shopGuardEditor'
import WorldEditorToolbar from '@/components/world-editor/WorldEditorToolbar.vue'
import ItemEditorPanel from '@/components/ItemEditorPanel.vue'
import UiButton from '@/components/ui/UiButton.vue'
import PlaytestBar from '@/components/world-editor/PlaytestBar.vue'
import { usePlaytest } from './usePlaytest'
import type {
  BuildingType,
  JsonObject,
  JsonValue,
  MapCampaignBlock,
  MapCampaignObjective,
  MapCatalogEntry,
  MapCatalogFile,
  MapConfig,
  NeutralGroupSummary,
  NeutralGroupTierSummary,
  NeutralSpawn,
  ObstacleType,
  PlacedUnit,
  TerrainType,
  TileSheet,
  UnitType,
  VictoryCondition,
  Zone,
  ZoneAura,
  ZoneCapture,
} from '@/game/network/protocol'
import { ZONE_AURA_SCOPE_GLOBAL, ZONE_AURA_TYPE_STAT_MODIFIER } from '@/game/network/protocol'
import { STAT_DEFS, STAT_OPERATIONS } from '@/game/stats/statRegistry'
import { buildZoneCellIndex, cellKey, fillEnclosedZoneCells, zoneBoundaryEdges } from '@/game/maps/zoneGeometry'
import type { Campaign } from '@/types/campaign'
import { fetchCampaignCatalog } from '@/services/campaignApi'
import { NEUTRAL_PLAYER_COLOR, NEUTRAL_SPAWN_RANDOM_GROUP_ID } from '@/game/network/protocol'
import type { UnitFaction, UnitDef } from '@/game/maps/unitDefs'
import type { PerkDef } from '@/game/maps/perkDefs'
import type { ItemDef } from '@/game/maps/itemDefs'
import { applyInstanceEdit, ranksForUnitType, perksForUnitType, itemsForUnitType, type InstancePatch } from './placedUnitInstance'
import { Camera } from '@/game/rendering/Camera'
import { buildTerrainSurface, drawMinimapBase, drawMinimapPOIs } from '@/game/rendering/minimapLayers'
import {
  DEFAULT_GRASS_COLOR,
  MAP_EDITOR_PRESETS,
  createEditorMapConfig,
  getBuildingColor,
  getObstacleColor,
  getTerrainColor,
  resizeMapConfig,
  setBuildingTile,
  setObstacleTile,
  setTerrainTile,
  setTilePaint,
} from '@/game/maps/mapConfig'
import {
  DEFAULT_TILE_PRESETS,
  drawAutoTiledTerrain,
  TILE_SHEET_NAMES,
  getSheetImage,
  getSheetTileSize,
  isTerrainTilesetReady,
  onSheetReady,
} from '@/game/rendering/terrainTileset'
import { getBuildingSprite, getRecipeShopStyleSprite, listRecipeShopStyles, getNeutralShopStyleSprite, listNeutralShopStyles } from '@/game/rendering/buildingSprites'
import { getObstacleSprite } from '@/game/rendering/obstacleSprites'
import { getUnitSpriteSet } from '@/game/rendering/unitSprites'
import { initObstacleDefs, OBSTACLE_DEF_MAP } from '@/game/maps/obstacleDefs'
import { BUILDING_DEF_MAP, BUILDING_DEFS, getBuildingStyleRender, initBuildingDefs, initBuildingStyleRenders } from '@/game/maps/buildingDefs'
import { getBuildingFallbackRender } from '@/game/maps/buildingFallbackRender'
import { initPathBounds, initPathsByUnitType } from '@/game/maps/unitDefs'

const model = defineModel<MapConfig>({ required: true })

const canvas = ref<HTMLCanvasElement | null>(null)
const tilePickerCanvas = ref<HTMLCanvasElement | null>(null)

// Editor minimap overlay. Lives in the top-left of .canvas-frame and
// provides a click/drag jump-to navigation plus a viewport rect tied to
// the editor camera. Static layers (terrain/obstacles/buildings/POIs)
// render through the shared minimapLayers module so the visual style
// matches the in-game minimap and the lobby preview.
const minimapCanvas = ref<HTMLCanvasElement | null>(null)
const MINIMAP_MAX_SIZE = 200
// Inset of the minimap from the top-left corner of the canvas frame, plus
// a small buffer. Used both for CSS positioning and for sizing the editor
// camera's top-left pan overscan so the user can scroll the map past the
// minimap to see the underlying top-left content. Kept slightly bigger
// than MINIMAP_MAX_SIZE + margin so there's a visible breath of black
// gutter when panned to the extreme corner.
const MINIMAP_INSET = 12
const MINIMAP_CLEAR_OVERSCAN = MINIMAP_MAX_SIZE + MINIMAP_INSET * 2
let minimapTerrainSurface: HTMLCanvasElement | null = null
let minimapStaticDirty = true
let minimapDragging = false

const TILE_PICKER_SCALE = 2
const brushMode = ref<'terrain' | 'tile' | 'obstacle' | 'building' | 'enemy-spawn' | 'neutral-spawn' | 'unit' | 'erase'>('terrain')

// ─── Zone sidebar state ────────────────────────────────────────────────────────
// selectedZoneId: the zone selected in the Zones list
// zoneSubMode:
//   'idle'        – normal navigation; no zone canvas interaction
//   'place'       – click canvas to create a new zone at that cell
//   'draw'        – left-click/drag adds cells; right-click removes cells
//   'captureDraw' – left-click/drag marks capture cells; right-click removes
//   'move'        – click a cell inside the zone to relocate its anchor node
const selectedZoneId = ref<string | null>(null)
const zoneSubMode = ref<'idle' | 'place' | 'draw' | 'captureDraw' | 'move' | 'claimPoint'>('idle')
// Controls visibility of the prerequisite-zone checkbox panel.
const linkPanelOpen = ref(false)
// Live cell→zoneId index, rebuilt on every zone mutation. Avoids O(N×M) scans.
let zoneCellIndex = buildZoneCellIndex(model.value.zones ?? [])
const brushSize = ref<1 | 3 | 5 | 7>(1)
const selectedTerrain = ref<TerrainType>('grass')
const selectedObstacle = ref<ObstacleType>('rock')
const selectedBuilding = ref<BuildingType>('goldmine')
const selectedTileSheet = ref<TileSheet>('tileset')

// All unit types known to the catalog, populated by fetchUnitDefs. Buckets are
// keyed by the unit's `faction` string and built dynamically — adding a new
// `catalog/units/<newfaction>/<unit>/...` folder on the server makes the
// faction appear in the editor on next load with zero code changes here.
const unitDefsByFaction = ref<Record<string, Array<{ type: UnitType; label: string }>>>({})

const selectedTileCoord = ref<{ sx: number; sy: number } | null>(null)
const selectedSpawnTownhallId = ref('')
const spawnPointFillOrder = ref(0)
const spawnPointPlayerLabel = ref('')
// Player slot assigned to newly-painted player-class buildings other than
// spawn-points (which carry their own playerLabel UI). Empty string ⇒
// unassigned (no metadata.playerLabel written, building stays unowned).
const buildingPlayerLabel = ref('')
const enemyTargetPlayerLabel = ref('')
const enemyTriggerCaptureZoneId = ref('')
const enemySpawnDelay = ref(0)
const enemySpawnInterval = ref(10)
const enemySpawnCount = ref(1)
const enemySpawnOnce = ref(false)
const enemyIgnoreWaveClear = ref(false)
const enemyUnitType = ref('raider')
// 'enemy' (default) spawns units under the hostile enemy faction; 'neutral'
// spawns them under the neutral-camp faction so they don't fight neutral camps.
const enemySpawnAlliance = ref<'enemy' | 'neutral'>('enemy')
const enemyWaveMode = ref<'gameStart' | 'always' | 'specific' | 'repeating' | 'interval' | 'capture'>('always')
const enemyWaveNumber = ref(1)
// Multiplier for the 'interval' spawn-timing mode. waveInterval = 3 ⇒ fires on
// waves 3, 6, 9, … Separate ref from enemyWaveNumber so switching modes mid-
// authoring doesn't clobber the other field's value.
const enemyWaveInterval = ref(3)
const neutralStartingTier = ref(1)
const neutralTierUpEveryNWaves = ref(0)
const neutralGroupId = ref<string>(NEUTRAL_SPAWN_RANDOM_GROUP_ID)
const neutralAggroRange = ref(150)
const neutralLeashRange = ref(200)
const neutralHealthMultiplier = ref(1.0)
const neutralHealthMultiplierPerWave = ref(0.0)
const neutralDamageMultiplier = ref(1.0)
const neutralDamageMultiplierPerWave = ref(0.0)
const neutralGroupTiers = ref<NeutralGroupTierSummary[] | null>(null)
// Recipe lists (from catalog/recipes/lists) for the recipe-shop Recipe List
// dropdown. Empty until fetchRecipeLists resolves.
const recipeLists = ref<RecipeListSummary[]>([])

// Item lists (from catalog/items/lists) for the neutral-shop Item List
// dropdown. Empty until fetchItemLists resolves.
const itemLists = ref<ItemListSummary[]>([])

// Catalogs backing the placed-unit instance-edit popup (Rank / Items /
// Perks). Loaded once in onMounted alongside the other catalog fetches;
// empty arrays until they resolve so the popup just shows no options yet.
const unitDefsList = ref<UnitDef[]>([])
const itemDefsList = ref<ItemDef[]>([])
const perkDefsList = ref<PerkDef[]>([])

const groupsForCurrentTier = computed<NeutralGroupSummary[]>(() => {
  const tiers = neutralGroupTiers.value
  if (!tiers || tiers.length === 0) return []
  // Pick the largest tier <= requested (mirrors server resolveNeutralTier).
  const sorted = [...tiers].sort((a, b) => a.tier - b.tier)
  let pick: NeutralGroupTierSummary | null = null
  for (const t of sorted) {
    if (t.tier <= neutralStartingTier.value) pick = t
  }
  return pick ? pick.groups : []
})

// If the user lowers the tier so the currently-selected group no longer
// exists in the resolved tier, fall back to Random so a click can't stamp
// a stale id the server won't recognize.
watch(groupsForCurrentTier, (groups) => {
  if (neutralGroupId.value === NEUTRAL_SPAWN_RANDOM_GROUP_ID) return
  if (!groups.some((g) => g.id === neutralGroupId.value)) {
    neutralGroupId.value = NEUTRAL_SPAWN_RANDOM_GROUP_ID
  }
})

const placedUnitFaction = ref<UnitFaction>('raider')
const placedUnitPlayerSlot = ref<string>('enemy')
const placedUnitType = ref('raider')
const placedUnitAggroRange = ref(150)
const placedUnitLeashRange = ref(200)
const placedUnits = ref<PlacedUnit[]>(model.value.placedUnits ?? [])
const draftCols = ref(model.value.gridCols)
const draftRows = ref(model.value.gridRows)
const copiedLabel = ref('Copy Export')
const saveLabel = ref('Save to Server')
const saveError = ref('')
const hoverLabel = ref('Hover a tile')
const paintModeEnabled = ref(false)
const openSection = ref<'setup' | 'campaign' | 'zones' | 'paint' | 'export' | null>('paint')

// Top toolbar (world-editor-toolbar plan, Task 5). Items category opens a
// popup implemented in Task 7; Play wires a real playtest flow in Task 8.
// Terrain/obstacles/buildings/units all reuse the existing Paint section's
// brush-mode state — the toolbar is a shortcut into tools that already exist,
// not a parallel state machine.
const itemsPopupOpen = ref(false)

function onToolbarSelect(id: string) {
  switch (id) {
    case 'terrain':
      openSection.value = 'paint'
      paintModeEnabled.value = true
      brushMode.value = 'terrain'
      break
    case 'obstacles':
      openSection.value = 'paint'
      paintModeEnabled.value = true
      brushMode.value = 'obstacle'
      break
    case 'buildings':
      openSection.value = 'paint'
      paintModeEnabled.value = true
      brushMode.value = 'building'
      break
    case 'units':
      openSection.value = 'paint'
      paintModeEnabled.value = true
      brushMode.value = 'unit'
      break
    case 'items':
      itemsPopupOpen.value = true
      break
    case 'play':
      startPlaytest()
      break
    default:
      // unit-types / unit-paths / perks / abilities / effects / projectiles /
      // campaigns are disabled in the toolbar (coming soon) and never emit.
      break
  }
}

// Task 8: ephemeral Play/Reset harness. start() persists the current editor
// map (including unsaved placements) under its working id (or the scratch id
// for a never-saved draft) and runs it as a live match on the play canvas.
// stop() tears the match down; the editor's own `model` is never mutated by
// playtest, so re-showing the editor canvas is the entire "reset".
const playCanvas = ref<HTMLCanvasElement | null>(null)
const { playing: playtestPlaying, start: startPlaytestMatch, stop: stopPlaytestMatch } = usePlaytest(
  () => playCanvas.value,
)

function startPlaytest() {
  void startPlaytestMatch(exportedCatalogFile.value)
}

function stopPlaytest() {
  stopPlaytestMatch()
}

// Drives the toolbar's active-button highlight from the panel's real tool
// state, so the toolbar never has its own source of truth for "what's on".
const toolbarActiveId = computed<string | undefined>(() => {
  if (itemsPopupOpen.value) return 'items'
  if (openSection.value === 'paint') {
    if (brushMode.value === 'obstacle') return 'obstacles'
    if (brushMode.value === 'building') return 'buildings'
    if (brushMode.value === 'unit') return 'units'
    if (brushMode.value === 'terrain' || brushMode.value === 'tile') return 'terrain'
  }
  return undefined
})
const isControlHeld = ref(false)
const availableMaps = ref<MapCatalogEntry[]>([])
const selectedLoadMapId = ref('')
const isLoadingMapCatalog = ref(false)
const isLoadingSelectedMap = ref(false)
const mapLoadError = ref('')
const enemyObjectiveId = ref('')
const buildingObjectiveId = ref('')
const selectedEditBuildingId = ref<string | null>(null)
const selectedEditPlacedUnitId = ref<string | null>(null)
const selectedEditNeutralSpawnId = ref<string | null>(null)
const editPanelPos = ref<{ x: number; y: number } | null>(null)
const placedUnitEditPanelPos = ref<{ x: number; y: number } | null>(null)
const neutralSpawnEditPanelPos = ref<{ x: number; y: number } | null>(null)
const claimPointEditPanelPos = ref<{ x: number; y: number } | null>(null)
const copiedBuilding = ref<{ buildingType: string; metadata: JsonObject | undefined } | null>(null)
const isPasteMode = ref(false)
const movingBuilding = ref<{ id: string; buildingType: string; metadata: JsonObject | undefined; x: number; y: number } | null>(null)
const movingNeutralSpawn = ref<{ id: string; x: number; y: number } | null>(null)
// Index of the currently selected claim capture point within the selected
// zone's claimPoints (null = none). movingClaimPoint arms a pending relocate:
// the next canvas click moves the selected point. The selection resets when the
// zone changes; movingClaimPoint resets on any sub-mode change (see watches).
const selectedClaimPointIndex = ref<number | null>(null)
const movingClaimPoint = ref(false)
// Armed by the guard-shop edit panel: the next canvas click sets the selected
// shop's guardSpawnX/Y (the cell its guards ring around). Cleared on commit,
// on toggle-off, and when the selected building changes.
const placingGuardSpawn = ref(false)

const camera = new Camera()
// Bump the top + left overscan so the user can pan the map's top-left
// corner past the minimap overlay. Right/bottom use the defaults.
camera.overscan = {
  ...camera.overscan,
  left: Math.max(camera.overscan.left, MINIMAP_CLEAR_OVERSCAN),
  top: Math.max(camera.overscan.top, MINIMAP_CLEAR_OVERSCAN),
}
let resizeObserver: ResizeObserver | null = null
let animationFrameId = 0
let isLeftMouseDown = false
let isMiddleMouseDown = false
let isSpaceHeld = false
let isSpacePanning = false
let isPainting = false
let lastMouseX = 0
let lastMouseY = 0
let lastPaintKey = ''

watch(
  model,
  (nextMap) => {
    draftCols.value = nextMap.gridCols
    draftRows.value = nextMap.gridRows
    placedUnits.value = nextMap.placedUnits ?? []
    clampCamera()
    // Rebuild the cell→zoneId index whenever the map changes.
    zoneCellIndex = buildZoneCellIndex(nextMap.zones ?? [])
  },
  { deep: true },
)

const exportedCatalogFile = computed<MapCatalogFile>(() => {
  const { id: _id, name: _name, description: _description, ...mapFields } = model.value
  return {
    id: model.value.id,
    name: model.value.name,
    description: model.value.description,
    sortOrder: 1000,
    map: {
      ...mapFields,
      placedUnits: placedUnits.value.length > 0 ? placedUnits.value : undefined,
    },
  }
})

const serializedMap = computed(() => JSON.stringify(exportedCatalogFile.value, null, 2))
const hasPaintedContent = computed(() =>
  model.value.terrain.length > 0 ||
  (model.value.tiles?.length ?? 0) > 0 ||
  model.value.obstacles.length > 0 ||
  model.value.buildings.length > 0,
)
const activeBrushMode = computed(() =>
  isControlHeld.value ? 'erase' : brushMode.value,
)
// Every building def in the catalog, sorted by label, with class=neutral/enemy
// surfaced last. Drives the paint dropdown so any catalog addition (e.g. a new
// chapel) is paintable in the editor with zero code changes. Excludes
// enemy-spawnpoint — it has its own top-level "Enemy Spawn" brush mode with a
// dedicated config UI rather than living inside the Building brush. Also excludes
// upgrade-tier defs (keep/castle): they're runtime tier states of their base
// building, not independently placeable.
const paintableBuildingDefs = computed(() => {
  const classRank = (kind: string) => (kind === 'player' ? 0 : kind === 'neutral' ? 1 : 2)
  return BUILDING_DEFS
    .filter((def) => def.type !== 'enemy-spawnpoint' && !def.upgradesFrom)
    .slice()
    .sort((a, b) => {
      const ca = classRank(a.class ?? 'player')
      const cb = classRank(b.class ?? 'player')
      if (ca !== cb) return ca - cb
      return (a.label || a.type).localeCompare(b.label || b.type)
    })
})

// Whether a building type accepts a playerLabel assignment. spawn-points have
// their own playerLabel UI; enemy-spawnpoints and neutral buildings (goldmine)
// are not player-owned.
function isPlayerOwnableBuildingType(buildingType: string): boolean {
  if (buildingType === 'spawn-point' || buildingType === 'enemy-spawnpoint') return false
  const def = BUILDING_DEF_MAP.get(buildingType)
  if (!def) return false
  return (def.class ?? 'player') === 'player'
}

const townhallOptions = computed(() =>
  model.value.buildings
    .filter((building) => building.buildingType === 'townhall')
    .map((building) => ({
      id: building.id,
      label: `${building.id} (${building.x}, ${building.y})`,
    })),
)

// Legacy objective-assignment dropdowns on the unit/building edit forms
// referenced these computeds via `.length` guards. The Victory Conditions
// authoring card was removed in §6 of campaign-objectives-and-metrics, so
// the source array is gone; returning empty here keeps the v-if guards
// truthful (the dropdowns simply no longer render). Section 9 cleanup or a
// future maintenance change will delete the unreachable template branches.
const killUnitObjectives = computed<VictoryCondition[]>(() => [])
const destroyBuildingObjectives = computed<VictoryCondition[]>(() => [])

const availablePlayerLabels = computed(() => {
  const labels = new Set<string>()
  for (const b of model.value.buildings) {
    if (b.buildingType === 'spawn-point') {
      const lbl = b.metadata?.['playerLabel'] as string | undefined
      if (lbl) labels.add(lbl)
    }
  }
  return [...labels].sort()
})

// All player slot labels available for placed-unit assignment: spawn-point
// labels (derived from buildings in the current map) plus the "enemy" slot
// for guards. Falls back to a default roster when no spawn points exist yet
// so the dropdown is never empty during initial authoring.
const placedUnitPlayerSlots = computed(() => {
  const fromSpawnPoints = availablePlayerLabels.value
  const slots = fromSpawnPoints.length > 0
    ? [...fromSpawnPoints]
    : ['player1', 'player2', 'player3', 'player4']
  slots.push('enemy')
  return slots
})

// Sorted list of faction keys present in the catalog. Drives every faction
// dropdown in the editor; new factions show up automatically as soon as their
// directory exists under server/internal/game/catalog/units/.
const availableFactions = computed(() =>
  Object.keys(unitDefsByFaction.value).sort(),
)

// Human-readable label for a faction key. Converts "wildborne" → "Wildborne"
// and "wave_enemy" → "Wave Enemy". Cheap heuristic — if you ever want curated
// display names, surface a field on the server `faction` directory (e.g. a
// faction.json) and read it here.
function formatFactionLabel(key: string): string {
  return key
    .split(/[_\s-]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ')
}

// Unit types filtered to the currently selected faction. Drives the brush
// type picker; the edit panel uses its own per-unit faction lookup so it can
// switch the selected unit's type to one of a different faction.
const unitTypesForBrushFaction = computed(() =>
  unitDefsByFaction.value[placedUnitFaction.value] ?? [],
)

// All units across every faction for the enemy-spawnpoint building's "Unit
// Type" picker. Enemy spawnpoints emit hostiles, but with dynamic factions
// there's no static "this faction is player-only vs hostile" signal — so any
// catalog unit can be wired as a wave spawn. If a faction should be excluded
// later, gate it via a per-faction or per-unit metadata field rather than a
// hardcoded list here.
const ENEMY_SPAWN_UNITS = computed(() =>
  availableFactions.value.flatMap((f) => unitDefsByFaction.value[f] ?? []),
)

function factionForUnitType(unitType: string): UnitFaction {
  for (const faction of availableFactions.value) {
    if (unitDefsByFaction.value[faction].some((u) => u.type === unitType)) {
      return faction
    }
  }
  return availableFactions.value[0] ?? ''
}

const selectedZone = computed<Zone | null>(() =>
  selectedZoneId.value
    ? (model.value.zones ?? []).find((z) => z.id === selectedZoneId.value) ?? null
    : null
)

/** All zones except the currently selected one — drives the prerequisite multiselect. */
const otherZones = computed<Zone[]>(() =>
  (model.value.zones ?? []).filter((z) => z.id !== selectedZoneId.value),
)

const selectedEditBuilding = computed(() =>
  selectedEditBuildingId.value
    ? model.value.buildings.find((b) => b.id === selectedEditBuildingId.value) ?? null
    : null
)

// Distinct neutral-group options for the shop-guard dropdown (see the Shop
// Guards section of the building edit panel). Empty until fetchNeutralGroups
// resolves; the mapper picks a squad + tier, the server resolves it at spawn.
const guardGroupOptions = computed<NeutralGroupSummary[]>(() =>
  allGuardGroups(neutralGroupTiers.value),
)

// Recipe-shop edit panel options: art styles from the client asset glob, and
// recipe lists from the server catalog (fetched on mount).
// Disarm guard-spawn placement whenever the selected building changes, so a
// stale armed click can't retarget the wrong shop.
watch(selectedEditBuildingId, () => { placingGuardSpawn.value = false })

const shopStyleOptions = computed<string[]>(() => listRecipeShopStyles())
const recipeListOptions = computed<RecipeListSummary[]>(() =>
  [...recipeLists.value].sort((a, b) => (a.name || a.id).localeCompare(b.name || b.id)),
)
const neutralShopStyleOptions = computed<string[]>(() => listNeutralShopStyles())
const itemListOptions = computed<ItemListSummary[]>(() =>
  [...itemLists.value].sort((a, b) => (a.name || a.id).localeCompare(b.name || b.id)),
)

const selectedEditPlacedUnit = computed(() =>
  selectedEditPlacedUnitId.value
    ? placedUnits.value.find((u) => u.id === selectedEditPlacedUnitId.value) ?? null
    : null
)

function updateSelectedPlacedUnit(patch: Partial<PlacedUnit>) {
  if (!selectedEditPlacedUnitId.value) return
  const next = placedUnits.value.map((u) =>
    u.id === selectedEditPlacedUnitId.value ? { ...u, ...patch } : u
  )
  placedUnits.value = next
  model.value = { ...model.value, placedUnits: next }
}

// Rank/Items/Perks options for the instance-edit popup, scoped to the
// currently-selected placed unit's type. Empty arrays until the catalogs
// have loaded (onMounted) or while nothing is selected.
const selectedInstanceRankOptions = computed(() =>
  selectedEditPlacedUnit.value
    ? ranksForUnitType(unitDefsList.value, selectedEditPlacedUnit.value.unitType)
    : []
)
const selectedInstancePerkOptions = computed<PerkDef[]>(() =>
  selectedEditPlacedUnit.value
    ? perksForUnitType(perkDefsList.value, selectedEditPlacedUnit.value.unitType)
    : []
)
const selectedInstanceItemOptions = computed<ItemDef[]>(() =>
  selectedEditPlacedUnit.value
    ? itemsForUnitType(itemDefsList.value, selectedEditPlacedUnit.value.unitType)
    : []
)

// applyPlacedUnitInstancePatch mutates rank/items/perks on the selected
// placed unit via the pure applyInstanceEdit helper, then writes the result
// back through the same placedUnits+model sync updateSelectedPlacedUnit
// uses. It replaces the whole entry (rather than updateSelectedPlacedUnit's
// shallow-merge patch) because applyInstanceEdit needs to be able to delete
// a cleared rank/items/perks key, which a `{ ...u, ...patch }` merge cannot
// express (a missing key in patch just leaves the old value in place).
function applyPlacedUnitInstancePatch(patch: Partial<InstancePatch>) {
  const current = selectedEditPlacedUnit.value
  if (!selectedEditPlacedUnitId.value || !current) return
  const full: InstancePatch = {
    rank: patch.rank ?? current.rank ?? '',
    items: patch.items ?? current.items ?? [],
    perks: patch.perks ?? current.perks ?? [],
  }
  const next = placedUnits.value.map((u) =>
    u.id === selectedEditPlacedUnitId.value ? applyInstanceEdit(u, full) : u
  )
  placedUnits.value = next
  model.value = { ...model.value, placedUnits: next }
}

function onInstanceRankChange(rank: string) {
  applyPlacedUnitInstancePatch({ rank })
}

function addInstanceItem() {
  const current = selectedEditPlacedUnit.value?.items ?? []
  const next = selectedInstanceItemOptions.value.find((def) => !current.includes(def.id))
  if (!next) return
  applyPlacedUnitInstancePatch({ items: [...current, next.id] })
}

function onInstanceItemChange(index: number, itemId: string) {
  const current = [...(selectedEditPlacedUnit.value?.items ?? [])]
  current[index] = itemId
  applyPlacedUnitInstancePatch({ items: current })
}

function removeInstanceItem(index: number) {
  const current = [...(selectedEditPlacedUnit.value?.items ?? [])]
  current.splice(index, 1)
  applyPlacedUnitInstancePatch({ items: current })
}

function addInstancePerk() {
  const current = selectedEditPlacedUnit.value?.perks ?? []
  const next = selectedInstancePerkOptions.value.find((def) => !current.includes(def.id))
  if (!next) return
  applyPlacedUnitInstancePatch({ perks: [...current, next.id] })
}

function onInstancePerkChange(index: number, perkId: string) {
  const current = [...(selectedEditPlacedUnit.value?.perks ?? [])]
  current[index] = perkId
  applyPlacedUnitInstancePatch({ perks: current })
}

function removeInstancePerk(index: number) {
  const current = [...(selectedEditPlacedUnit.value?.perks ?? [])]
  current.splice(index, 1)
  applyPlacedUnitInstancePatch({ perks: current })
}

// Edit-panel Faction dropdown handler. Faction isn't stored on the placed
// unit — it's derived from the unit's type. So changing the faction picker
// swaps the unit type to the first available type of the chosen faction.
function onEditPanelFactionChange(faction: UnitFaction) {
  const pool = unitDefsByFaction.value[faction] ?? []
  const next = pool[0]?.type
  if (next) updateSelectedPlacedUnit({ unitType: next })
}

function deleteSelectedPlacedUnit() {
  if (!selectedEditPlacedUnitId.value) return
  const next = placedUnits.value.filter((u) => u.id !== selectedEditPlacedUnitId.value)
  placedUnits.value = next
  model.value = { ...model.value, placedUnits: next }
  selectedEditPlacedUnitId.value = null
}

const selectedEditNeutralSpawn = computed<NeutralSpawn | null>(() => {
  const id = selectedEditNeutralSpawnId.value
  if (!id) return null
  return model.value.neutralSpawns?.find((s) => s.id === id) ?? null
})

function updateSelectedNeutralSpawn(patch: Partial<NeutralSpawn>) {
  const id = selectedEditNeutralSpawnId.value
  if (!id) return
  const existing = model.value.neutralSpawns ?? []
  const next = existing.map((s) => (s.id === id ? { ...s, ...patch } : s))
  model.value = { ...model.value, neutralSpawns: next }
}

function deleteSelectedNeutralSpawn() {
  const id = selectedEditNeutralSpawnId.value
  if (!id) return
  const existing = model.value.neutralSpawns ?? []
  const next = existing.filter((s) => s.id !== id)
  model.value = { ...model.value, neutralSpawns: next.length ? next : undefined }
  selectedEditNeutralSpawnId.value = null
  neutralSpawnEditPanelPos.value = null
}

// ─── Zone brush helpers ───────────────────────────────────────────────────────

/** Generate a zone id that doesn't collide with any existing zone. */
function generateZoneId(): string {
  const existing = new Set((model.value.zones ?? []).map((z) => z.id))
  let n = (model.value.zones ?? []).length + 1
  let id = `zone-${n}`
  while (existing.has(id)) {
    n++
    id = `zone-${n}`
  }
  return id
}

/** Create a new zone stamped at (anchorX, anchorY) with a 5x5 default footprint. */
function createZoneAt(anchorX: number, anchorY: number) {
  const { gridCols, gridRows } = model.value
  const half = 2 // 5x5 = center ± 2
  const cells: [number, number][] = []
  for (let dy = -half; dy <= half; dy++) {
    for (let dx = -half; dx <= half; dx++) {
      const x = anchorX + dx
      const y = anchorY + dy
      if (x >= 0 && x < gridCols && y >= 0 && y < gridRows) {
        cells.push([x, y])
      }
    }
  }
  const id = generateZoneId()
  const zone: Zone = {
    id,
    anchor: { x: anchorX, y: anchorY },
    cells,
    capture: { type: 'presence', config: { captureSeconds: 10 } },
    startingOwner: 'neutral',
    adjacent: [],
  }
  const zones = [...(model.value.zones ?? []), zone]
  model.value = { ...model.value, zones }
  selectedZoneId.value = id
  zoneSubMode.value = 'idle'
}

/** Update a top-level field on the selected zone. */
function updateSelectedZoneField<K extends keyof Zone>(key: K, value: Zone[K]) {
  const id = selectedZoneId.value
  if (!id) return
  const zones = (model.value.zones ?? []).map((z) =>
    z.id === id ? { ...z, [key]: value } : z,
  )
  model.value = { ...model.value, zones }
}

/** Change the capture type, resetting per-type config. Clears captureCells when leaving presence. */
function onZoneCaptureTypeChange(type: string) {
  const id = selectedZoneId.value
  if (!id) return
  let config: ZoneCapture['config'] | undefined
  if (type === 'presence') config = { captureSeconds: 10 }
  else if (type === 'claim') config = { defendSeconds: 30, towerType: 'tower' }
  const capture: ZoneCapture = { type, ...(config ? { config } : {}) }
  const zones = (model.value.zones ?? []).map((z) => {
    if (z.id !== id) return z
    const next: typeof z = { ...z, capture }
    if (type !== 'presence') delete next.captureCells
    if (type !== 'claim') delete next.claimPoints
    return next
  })
  model.value = { ...model.value, zones }
}

/** Update a specific field inside the selected zone's capture config. */
function updateZoneCaptureConfig(field: string, value: unknown) {
  const id = selectedZoneId.value
  if (!id) return
  const zones = (model.value.zones ?? []).map((z) => {
    if (z.id !== id) return z
    return {
      ...z,
      capture: {
        ...z.capture,
        config: { ...(z.capture.config ?? {}), [field]: value },
      },
    }
  })
  model.value = { ...model.value, zones }
}

/** Rewrite the selected zone's auras array via the standard model-update path. */
function setSelectedZoneAuras(auras: ZoneAura[]) {
  const id = selectedZoneId.value
  if (!id) return
  const zones = (model.value.zones ?? []).map((z) =>
    z.id === id ? { ...z, auras: auras.length ? auras : undefined } : z,
  )
  model.value = { ...model.value, zones }
}

/** Append a default stat-modifier aura (first registered stat, additive, 0). */
function addAuraToSelectedZone() {
  const current = selectedZone.value?.auras ?? []
  const next: ZoneAura = {
    type: ZONE_AURA_TYPE_STAT_MODIFIER,
    scope: ZONE_AURA_SCOPE_GLOBAL,
    modifier: { stat: STAT_DEFS[0].id, operation: 'add', value: 0 },
  }
  setSelectedZoneAuras([...current, next])
}

/** Patch one field of one aura's modifier (stat / operation / value). */
function updateSelectedZoneAura(index: number, field: 'stat' | 'operation' | 'value', value: string | number) {
  const current = selectedZone.value?.auras ?? []
  const next = current.map((a, i) =>
    i === index ? { ...a, modifier: { ...a.modifier, [field]: value } } : a,
  )
  setSelectedZoneAuras(next)
}

/** Remove the aura at index from the selected zone. */
function removeAuraFromSelectedZone(index: number) {
  const current = selectedZone.value?.auras ?? []
  setSelectedZoneAuras(current.filter((_, i) => i !== index))
}

/** Add (cx, cy) to the selected zone's cells (draw mode). Handles overlap reassign. */
function addCellToSelectedZone(cx: number, cy: number) {
  const id = selectedZoneId.value
  if (!id) return
  const key = cellKey(cx, cy)
  const existingOwner = zoneCellIndex.get(key)

  let zones = model.value.zones ?? []

  // Reassign from previous owner if different.
  if (existingOwner && existingOwner !== id) {
    zones = zones.map((z) =>
      z.id === existingOwner
        ? { ...z, cells: z.cells.filter(([x, y]) => !(x === cx && y === cy)) }
        : z,
    )
  }

  // Add to selected zone if not already there.
  zones = zones.map((z) => {
    if (z.id !== id) return z
    const alreadyMember = z.cells.some(([x, y]) => x === cx && y === cy)
    if (alreadyMember) return z
    return { ...z, cells: [...z.cells, [cx, cy]] }
  })

  model.value = { ...model.value, zones }
}

/**
 * Auto-fill any region the selected zone now encloses. Run after a draw stroke:
 * once the drawn cells form a closed loop, the trapped empty cells fill in and
 * internal divider lines reclassify from perimeter to interior. Purely additive
 * (only adds cells), and a no-op when nothing is enclosed, so it's safe to call
 * on every stroke. Skips the model write when nothing changed.
 */
function fillSelectedZoneEnclosed() {
  const id = selectedZoneId.value
  if (!id) return
  const zones = model.value.zones ?? []
  const zone = zones.find((z) => z.id === id)
  if (!zone) return
  const filled = fillEnclosedZoneCells(zone.cells)
  if (filled.length === zone.cells.length) return // nothing enclosed
  model.value = {
    ...model.value,
    zones: zones.map((z) => (z.id === id ? { ...z, cells: filled } : z)),
  }
}

/** Remove (cx, cy) from the selected zone's cells (right-click in draw mode). */
function removeCellFromSelectedZone(cx: number, cy: number) {
  const id = selectedZoneId.value
  if (!id) return
  const zones = (model.value.zones ?? []).map((z) =>
    z.id === id
      ? { ...z, cells: z.cells.filter(([x, y]) => !(x === cx && y === cy)) }
      : z,
  )
  model.value = { ...model.value, zones }
}

/**
 * Add (cx, cy) to the selected zone's captureCells. The cell must be a member
 * of that zone's cells — the capture sub-zone must stay inside the zone boundary.
 * Ignores clicks on cells that don't belong to the zone. Deduplicates.
 */
function addCaptureCellToSelectedZone(cx: number, cy: number) {
  const id = selectedZoneId.value
  if (!id) return
  const zones = model.value.zones ?? []
  const zone = zones.find((z) => z.id === id)
  if (!zone) return
  // Enforce sub-zone membership constraint.
  const inZone = zone.cells.some(([x, y]) => x === cx && y === cy)
  if (!inZone) return
  const existing = zone.captureCells ?? []
  if (existing.some(([x, y]) => x === cx && y === cy)) return // dedup
  model.value = {
    ...model.value,
    zones: zones.map((z) =>
      z.id === id
        ? { ...z, captureCells: [...(z.captureCells ?? []), [cx, cy] as [number, number]] }
        : z,
    ),
  }
}

/**
 * Add (cx, cy) as a claim capture-point slot top-left on the selected zone.
 * The cell must be inside the zone's cells. Deduplicates. Each point becomes a
 * 2x2 tower slot the team must build + defend.
 */
function addClaimPointToSelectedZone(cx: number, cy: number) {
  const id = selectedZoneId.value
  if (!id) return
  const zones = model.value.zones ?? []
  const zone = zones.find((z) => z.id === id)
  if (!zone) return
  const inZone = zone.cells.some(([x, y]) => x === cx && y === cy)
  if (!inZone) return
  const existing = zone.claimPoints ?? []
  if (existing.some(([x, y]) => x === cx && y === cy)) return // dedup
  model.value = {
    ...model.value,
    zones: zones.map((z) =>
      z.id === id
        ? { ...z, claimPoints: [...(z.claimPoints ?? []), [cx, cy] as [number, number]] }
        : z,
    ),
  }
}

/** Remove the claim capture-point at (cx, cy) from the selected zone. Clears the
 *  point selection (indices shift after a removal). */
function removeClaimPointFromSelectedZone(cx: number, cy: number) {
  const id = selectedZoneId.value
  if (!id) return
  const zones = (model.value.zones ?? []).map((z) =>
    z.id === id
      ? { ...z, claimPoints: (z.claimPoints ?? []).filter(([x, y]) => !(x === cx && y === cy)) }
      : z,
  )
  model.value = { ...model.value, zones }
  selectedClaimPointIndex.value = null
  movingClaimPoint.value = false
}

/** Returns the index of the selected zone's explicit claim point whose 2x2 slot
 *  covers (cx, cy), or null. The anchor fallback slot (empty claimPoints) is not
 *  an explicit point and is not selectable. */
function claimPointIndexAt(cx: number, cy: number): number | null {
  const zone = selectedZone.value
  if (!zone) return null
  const points = zone.claimPoints ?? []
  for (let i = 0; i < points.length; i++) {
    const [ax, ay] = points[i]
    if (cx >= ax && cx <= ax + 1 && cy >= ay && cy <= ay + 1) return i
  }
  return null
}

/** Arm move mode for the selected claim point; the next canvas click inside the
 *  zone relocates it (mirrors the building Move flow). */
function startMoveClaimPoint() {
  if (selectedClaimPointIndex.value === null) return
  movingClaimPoint.value = true
}

/** Relocate the selected claim point to (cx, cy). Rejected (move stays armed)
 *  when the target is outside the zone or on another point's slot top-left. */
function commitMoveClaimPoint(cx: number, cy: number) {
  const id = selectedZoneId.value
  const idx = selectedClaimPointIndex.value
  if (!id || idx === null) {
    movingClaimPoint.value = false
    return
  }
  const zone = (model.value.zones ?? []).find((z) => z.id === id)
  if (!zone) {
    movingClaimPoint.value = false
    return
  }
  const inZone = zone.cells.some(([x, y]) => x === cx && y === cy)
  const points = zone.claimPoints ?? []
  const dup = points.some(([x, y], i) => i !== idx && x === cx && y === cy)
  if (!inZone || dup) return // invalid target — keep move armed
  const nextPoints = points.map((p, i) => (i === idx ? ([cx, cy] as [number, number]) : p))
  model.value = {
    ...model.value,
    zones: (model.value.zones ?? []).map((z) =>
      z.id === id ? { ...z, claimPoints: nextPoints } : z,
    ),
  }
  movingClaimPoint.value = false
}

/** Delete the currently selected claim point. */
function deleteSelectedClaimPoint() {
  const id = selectedZoneId.value
  const idx = selectedClaimPointIndex.value
  if (!id || idx === null) return
  const zones = (model.value.zones ?? []).map((z) =>
    z.id === id ? { ...z, claimPoints: (z.claimPoints ?? []).filter((_, i) => i !== idx) } : z,
  )
  model.value = { ...model.value, zones }
  selectedClaimPointIndex.value = null
  movingClaimPoint.value = false
}

// Clear any capture-point selection when the selected zone changes or the user
// leaves the Place Capture Point sub-mode, so the highlight / Move-Delete
// buttons never dangle on a stale point.
watch(selectedZoneId, () => {
  selectedClaimPointIndex.value = null
  movingClaimPoint.value = false
})
// Cancel an in-progress point move when the zone sub-mode changes, but KEEP the
// point selection — a capture point stays selectable/actionable whether or not
// the Place Capture Point tool is active.
watch(zoneSubMode, () => {
  movingClaimPoint.value = false
})

/** Remove (cx, cy) from the selected zone's captureCells (right-click in captureDraw mode). */
function removeCaptureCellFromSelectedZone(cx: number, cy: number) {
  const id = selectedZoneId.value
  if (!id) return
  const zones = (model.value.zones ?? []).map((z) =>
    z.id === id
      ? { ...z, captureCells: (z.captureCells ?? []).filter(([x, y]) => !(x === cx && y === cy)) }
      : z,
  )
  model.value = { ...model.value, zones }
}

/** Returns the zone whose anchor node sits on (x, y), or null. */
function zoneAtAnchorCell(x: number, y: number): Zone | null {
  return (model.value.zones ?? []).find((z) => z.anchor.x === x && z.anchor.y === y) ?? null
}

/**
 * Relocate the selected zone's anchor node to (cx, cy). The node must stay
 * inside the zone (a member cell / inside the perimeter); a click outside the
 * zone's cells is rejected. Returns true when the move was applied (or was a
 * no-op on the current anchor), false when the target is outside the zone.
 */
function moveSelectedZoneAnchor(cx: number, cy: number): boolean {
  const id = selectedZoneId.value
  if (!id) return false
  const zones = model.value.zones ?? []
  const zone = zones.find((z) => z.id === id)
  if (!zone) return false
  const inside = zone.cells.some(([x, y]) => x === cx && y === cy)
  if (!inside) return false // can't move the node outside the perimeter
  if (zone.anchor.x === cx && zone.anchor.y === cy) return true // already there
  model.value = {
    ...model.value,
    zones: zones.map((z) => (z.id === id ? { ...z, anchor: { x: cx, y: cy } } : z)),
  }
  return true
}

/**
 * Add or remove targetId from the SELECTED zone's adjacent array (directed —
 * only the selected zone is mutated; the target zone is not touched).
 * No-op when targetId === selectedZoneId.
 */
function toggleZoneLink(targetId: string) {
  const id = selectedZoneId.value
  if (!id || id === targetId) return
  const zones = model.value.zones ?? []
  const nextZones = zones.map((z) => {
    if (z.id !== id) return z
    const adj = z.adjacent ?? []
    const has = adj.includes(targetId)
    return {
      ...z,
      adjacent: has ? adj.filter((a) => a !== targetId) : [...adj, targetId],
    }
  })
  model.value = { ...model.value, zones: nextZones }
}

/**
 * Set requireAllLinks on the selected zone. Writes undefined when false so the
 * field stays absent in the exported JSON (matches the ?? false read pattern).
 */
function setZoneRequireAllLinks(value: boolean) {
  const id = selectedZoneId.value
  if (!id) return
  const zones = (model.value.zones ?? []).map((z) =>
    z.id === id
      ? { ...z, requireAllLinks: value || undefined }
      : z,
  )
  model.value = { ...model.value, zones }
}

/**
 * Handle a click on a zone row in the Zones sidebar list.
 * Selecting a zone always moves to 'idle' sub-mode.
 */
function selectZoneFromList(id: string) {
  selectedZoneId.value = id
  zoneSubMode.value = 'idle'
  linkPanelOpen.value = false
}

/** Delete the selected zone and strip its id from every other zone's adjacent list. */
function deleteSelectedZone() {
  const id = selectedZoneId.value
  if (!id) return
  const zones = (model.value.zones ?? [])
    .filter((z) => z.id !== id)
    .map((z) => ({
      ...z,
      adjacent: (z.adjacent ?? []).filter((a) => a !== id),
    }))
  model.value = { ...model.value, zones: zones.length ? zones : undefined }
  selectedZoneId.value = null
  zoneSubMode.value = 'idle'
  linkPanelOpen.value = false
}

// Group dropdown options for the edit panel, computed from the SELECTED
// spawn's startingTier (independent of the brush-panel tier).
const editNeutralGroupsForTier = computed<NeutralGroupSummary[]>(() => {
  const tiers = neutralGroupTiers.value
  if (!tiers || tiers.length === 0) return []
  const requested = selectedEditNeutralSpawn.value?.startingTier ?? 1
  const sorted = [...tiers].sort((a, b) => a.tier - b.tier)
  let pick: NeutralGroupTierSummary | null = null
  for (const t of sorted) {
    if (t.tier <= requested) pick = t
  }
  return pick ? pick.groups : []
})

const editPanelStyle = computed(() => {
  if (!editPanelPos.value) return {}
  return { left: `${editPanelPos.value.x}px`, top: `${editPanelPos.value.y}px` }
})

const placedUnitEditPanelStyle = computed(() => {
  if (!placedUnitEditPanelPos.value) return {}
  return { left: `${placedUnitEditPanelPos.value.x}px`, top: `${placedUnitEditPanelPos.value.y}px` }
})

const neutralSpawnEditPanelStyle = computed(() => {
  if (!neutralSpawnEditPanelPos.value) return {}
  return { left: `${neutralSpawnEditPanelPos.value.x}px`, top: `${neutralSpawnEditPanelPos.value.y}px` }
})

const claimPointEditPanelStyle = computed(() => {
  if (!claimPointEditPanelPos.value) return {}
  return { left: `${claimPointEditPanelPos.value.x}px`, top: `${claimPointEditPanelPos.value.y}px` }
})

// World cell [x,y] of the currently selected capture point (the 2x2 slot's
// top-left), or null. Drives the floating Capture Point popup.
const selectedClaimPointCell = computed<{ x: number; y: number } | null>(() => {
  const zone = selectedZone.value
  const idx = selectedClaimPointIndex.value
  if (!zone || idx === null) return null
  const p = (zone.claimPoints ?? [])[idx]
  if (!p) return null
  return { x: p[0], y: p[1] }
})

const editWaveMode = computed<'gameStart' | 'always' | 'specific' | 'repeating' | 'interval' | 'capture'>(() => {
  const meta = selectedEditBuilding.value?.metadata
  if (!meta) return 'always'
  // Capture-trigger takes precedence: its presence selects the mutually
  // exclusive "While Zone Being Captured" mode (auto-heals legacy data that set
  // both a wave field and a trigger zone).
  if (meta['triggerCaptureZoneId']) return 'capture'
  if (meta['gameStart'] === true) return 'gameStart'
  if ('waveInterval' in meta) return 'interval'
  if ('waveNumber' in meta) return 'specific'
  if ('startingWave' in meta) return 'repeating'
  return 'always'
})

// Zones eligible for the "While Zone Being Captured" spawn mode. Only the timed
// mechanics (presence / claim) have a "being captured" window; clear and
// control_point flip instantly, so a capture-triggered spawn there would never
// fire and must not be offered.
const captureTriggerZones = computed(() =>
  (model.value.zones ?? []).filter(
    (z) => z.capture?.type === 'presence' || z.capture?.type === 'claim',
  ),
)

const editWaveNumber = computed(() => {
  const meta = selectedEditBuilding.value?.metadata
  if (!meta) return 1
  return (meta['waveNumber'] as number)
    ?? (meta['startingWave'] as number)
    ?? (meta['waveInterval'] as number)
    ?? 1
})

const defaultGroundName = computed<'grass' | 'dirt'>(() => {
  const current = model.value.defaultTile
  if (!current) return 'grass'
  for (const name of ['grass', 'dirt'] as const) {
    const preset = DEFAULT_TILE_PRESETS[name]
    if (
      current.sheet === preset.sheet &&
      current.sx === preset.sx &&
      current.sy === preset.sy
    ) {
      return name
    }
  }
  return 'grass'
})

function onDefaultGroundChange(event: Event) {
  const value = (event.target as HTMLSelectElement).value as 'grass' | 'dirt'
  model.value = { ...model.value, defaultTile: { ...DEFAULT_TILE_PRESETS[value] } }
}

const eraseCursor = `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='24' height='24' viewBox='0 0 24 24'%3E%3Cg stroke='%23f8fafc' stroke-width='2.6' stroke-linecap='round'%3E%3Cpath d='M6 6 L18 18'/%3E%3Cpath d='M18 6 L6 18'/%3E%3C/g%3E%3Cg stroke='%230f172a' stroke-width='1.2' stroke-linecap='round'%3E%3Cpath d='M6 6 L18 18'/%3E%3Cpath d='M18 6 L6 18'/%3E%3C/g%3E%3C/svg%3E") 12 12, crosshair`

function getCanvasCursor() {
  if (isSpaceHeld) return 'grab'
  if (movingBuilding.value || movingNeutralSpawn.value || movingClaimPoint.value) return 'move'
  if (isPasteMode.value) return 'copy'
  if (!paintModeEnabled.value) return 'default'
  if (isControlHeld.value) return eraseCursor
  return 'crosshair'
}

function toggleSection(section: 'setup' | 'campaign' | 'zones' | 'paint' | 'export') {
  openSection.value = openSection.value === section ? null : section
}

// ---------------------------------------------------------------------------
// Campaign authoring — added by the map-editor-authors-campaign-maps change.
// When the user ticks "this is a campaign map" the editor writes a Campaign
// block onto `model.value.campaign` that the server picks up on save. The
// block IS the campaign level: there is no separate catalog/campaigns/*.json
// level entry to keep in sync.
//
// The dropdown of campaigns is populated from /api/catalog/campaigns so the
// list always reflects whatever header files exist on the server. A new
// campaign is added by dropping a header at catalog/campaigns/<id>.json (no
// editor change required).
// ---------------------------------------------------------------------------

const campaignCatalog = ref<readonly Campaign[]>([])
const campaignCatalogError = ref('')
const campaignCatalogLoading = ref(false)

// Objective types known to the editor for the type dropdown. Mirrors the
// six handlers registered in server/internal/game/objective_handlers.go.
// When a new handler ships server-side, add the type key here AND a default
// config in `emptyObjectiveConfig` + a per-type field block in the template.
const KNOWN_OBJECTIVE_TYPES = [
  'kill_camps',
  'build_buildings',
  'collect_resource',
  'kill_camps_before_wave',
  'rank_units',
  'survive_waves',
] as const

async function loadCampaignCatalog() {
  campaignCatalogLoading.value = true
  campaignCatalogError.value = ''
  try {
    campaignCatalog.value = await fetchCampaignCatalog()
  } catch (err) {
    campaignCatalogError.value =
      err instanceof Error ? err.message : 'Failed to load campaigns.'
  } finally {
    campaignCatalogLoading.value = false
  }
}

// emptyObjectiveConfig returns a sensible starter config for the given type
// so the user sees real fields the moment they add a row, instead of a blank
// JSON blob they have to figure out. Per-type fields in the template wire
// against these keys.
function emptyObjectiveConfig(typeKey: string): Record<string, unknown> {
  switch (typeKey) {
    case 'kill_camps':
      return { count: 1 }
    case 'build_buildings':
      return { buildingType: 'barracks', count: 1 }
    case 'collect_resource':
      return { resource: 'gold', amount: 100 }
    case 'kill_camps_before_wave':
      return { count: 1, beforeWave: 5 }
    case 'rank_units':
      return { rank: 'bronze', count: 1 }
    case 'survive_waves':
      return { wavesToSurvive: 1 }
    default:
      return {}
  }
}

// toggleCampaign turns the campaign tag on/off. Off → strips the field
// entirely so the saved map JSON is clean (no { campaign: null } noise).
function toggleCampaign(enabled: boolean) {
  if (enabled) {
    model.value = {
      ...model.value,
      campaign: model.value.campaign ?? {
        campaignId: campaignCatalog.value[0]?.id ?? '',
        levelId: '',
        displayName: '',
        prerequisiteLevelId: null,
        description: '',
        sortOrder: 0,
        objectives: [],
      },
    }
  } else {
    const next = { ...model.value }
    delete next.campaign
    model.value = next
  }
}

function setCampaignField<K extends keyof MapCampaignBlock>(
  key: K,
  value: MapCampaignBlock[K],
) {
  if (!model.value.campaign) return
  model.value = {
    ...model.value,
    campaign: { ...model.value.campaign, [key]: value },
  }
}

function levelsForCampaign(campaignId: string) {
  return campaignCatalog.value.find((c) => c.id === campaignId)?.levels ?? []
}

function addObjective() {
  if (!model.value.campaign) return
  const objectives = [...(model.value.campaign.objectives ?? [])]
  const defaultType = KNOWN_OBJECTIVE_TYPES[0]
  objectives.push({
    id: `objective_${objectives.length + 1}`,
    type: defaultType,
    description: '',
    scope: 'team',
    required: false,
    rewardDominionPoints: 0,
    rewardConquestBadges: 0,
    config: emptyObjectiveConfig(defaultType),
  })
  setCampaignField('objectives', objectives)
}

function removeObjective(index: number) {
  if (!model.value.campaign) return
  const objectives = [...(model.value.campaign.objectives ?? [])]
  objectives.splice(index, 1)
  setCampaignField('objectives', objectives)
}

function updateObjective(index: number, patch: Partial<MapCampaignObjective>) {
  if (!model.value.campaign) return
  const objectives = [...(model.value.campaign.objectives ?? [])]
  if (!objectives[index]) return
  objectives[index] = { ...objectives[index], ...patch }
  setCampaignField('objectives', objectives)
}

// updateObjectiveType swaps the type AND resets the config to the new type's
// defaults — keeping stale fields from the previous type would silently fail
// server-side validation, and migrating field-by-field isn't worth the
// complexity for a low-volume authoring tool.
function updateObjectiveType(index: number, newType: string) {
  updateObjective(index, {
    type: newType,
    config: emptyObjectiveConfig(newType),
  })
}

function updateObjectiveConfigField(index: number, key: string, value: unknown) {
  if (!model.value.campaign) return
  const objectives = [...(model.value.campaign.objectives ?? [])]
  if (!objectives[index]) return
  const config: Record<string, unknown> = { ...(objectives[index].config ?? {}) }
  if (value === undefined || value === '' || value === null) {
    delete config[key]
  } else {
    config[key] = value
  }
  objectives[index] = { ...objectives[index], config }
  setCampaignField('objectives', objectives)
}

function objectiveConfigValue(obj: MapCampaignObjective, key: string): unknown {
  return (obj.config as Record<string, unknown> | undefined)?.[key]
}

// addVictoryCondition / removeVictoryCondition / updateVictoryCondition were
// removed in the campaign-objectives-and-metrics §6 migration alongside the
// Victory Conditions authoring card. `nextVictoryConditionId` is removed for
// the same reason. Per-level objectives are now hand-authored in campaign JSON.

function setWaveConfig(
  field:
    | 'totalWaves'
    | 'initialPrepDuration'
    | 'prepDuration'
    | 'waveDuration'
    | 'continuousWaves'
    | 'enemiesFightNeutrals',
  value: number | boolean,
) {
  const current = model.value.waveConfig ?? {}
  const updated = { ...current, [field]: value }
  // Drop waveConfig entirely if every field is zero/absent/false — keeps the export clean
  const hasAny =
    (updated.totalWaves ?? 0) > 0 ||
    (updated.initialPrepDuration ?? 0) > 0 ||
    (updated.prepDuration ?? 0) > 0 ||
    (updated.waveDuration ?? 0) > 0 ||
    !!updated.continuousWaves ||
    !!updated.enemiesFightNeutrals
  model.value = { ...model.value, waveConfig: hasAny ? updated : undefined }
}

// Mirrors setWaveConfig: drops the debug block entirely when every flag is
// false so production maps stay clean of a `"debug": {}` artifact in their
// exported JSON. Server-side readers already treat a missing block as all-off.
function setDebugFlag(field: 'battleTracker' | 'debugSpawn', value: boolean) {
  const current = model.value.debug ?? {}
  const updated = { ...current, [field]: value }
  const hasAny = !!updated.battleTracker || !!updated.debugSpawn
  model.value = { ...model.value, debug: hasAny ? updated : undefined }
}

function applyGridSize() {
  model.value = resizeMapConfig(model.value, draftCols.value, draftRows.value)
  recenterCamera()
}

function applyPreset(cols: number, rows: number) {
  draftCols.value = cols
  draftRows.value = rows
  applyGridSize()
}

function clearMap() {
  model.value = createEditorMapConfig(model.value.gridCols, model.value.gridRows)
  placedUnits.value = []
  selectedEditPlacedUnitId.value = null
  selectedEditNeutralSpawnId.value = null
}

// Wipes all painted content (terrain, custom tiles, obstacles, buildings)
// while preserving setup metadata (id / name / description / grid size /
// default ground / wave config). Confirms before acting because the change
// isn't undoable.
function clearEverything() {
  if (!window.confirm('Clear all terrain, tiles, obstacles, and buildings? This cannot be undone.')) return
  model.value = createEditorMapConfig(model.value.gridCols, model.value.gridRows, {
    id: model.value.id,
    name: model.value.name,
    description: model.value.description,
    cellSize: model.value.cellSize,
    defaultTile: model.value.defaultTile,
    waveConfig: model.value.waveConfig,
  })
  placedUnits.value = []
  selectedEditPlacedUnitId.value = null
  selectedEditNeutralSpawnId.value = null
  selectedSpawnTownhallId.value = ''
}


function updateEditMeta(key: string, value: JsonValue | undefined) {
  if (!selectedEditBuildingId.value) return
  model.value = {
    ...model.value,
    buildings: model.value.buildings.map((b) => {
      if (b.id !== selectedEditBuildingId.value) return b
      const meta: JsonObject = { ...(b.metadata ?? {}) }
      if (value === undefined) delete meta[key]
      else meta[key] = value
      return { ...b, metadata: meta }
    }),
  }
}

// True when the selected building has an explicit guard spawn cell set.
const hasGuardSpawn = computed(() => {
  const m = selectedEditBuilding.value?.metadata
  return typeof m?.['guardSpawnX'] === 'number' && typeof m?.['guardSpawnY'] === 'number'
})

// Human-readable description of the selected shop's guard spawn location.
const guardSpawnLabel = computed(() => {
  const m = selectedEditBuilding.value?.metadata
  if (typeof m?.['guardSpawnX'] === 'number' && typeof m?.['guardSpawnY'] === 'number') {
    return `Guards spawn at cell (${m['guardSpawnX']}, ${m['guardSpawnY']}).`
  }
  return 'Guards spawn at the shop center. Click Place to choose a cell.'
})

function togglePlaceGuardSpawn() {
  placingGuardSpawn.value = !placingGuardSpawn.value
}

// Commits a guard spawn cell to the selected shop (armed via placingGuardSpawn).
function commitGuardSpawn(cx: number, cy: number) {
  placingGuardSpawn.value = false
  const id = selectedEditBuildingId.value
  if (!id) return
  model.value = {
    ...model.value,
    buildings: model.value.buildings.map((b) => {
      if (b.id !== id) return b
      const meta: JsonObject = { ...(b.metadata ?? {}) }
      meta['guardSpawnX'] = cx
      meta['guardSpawnY'] = cy
      return { ...b, metadata: meta }
    }),
  }
}

function clearGuardSpawn() {
  placingGuardSpawn.value = false
  updateEditMeta('guardSpawnX', undefined)
  updateEditMeta('guardSpawnY', undefined)
}

// Updates the selected building's top-level resourceAmount (the starting
// resource stock for resource-source buildings like the goldmine). This is a
// first-class BuildingTile field, not metadata, so it can't go through
// updateEditMeta. Clamped to a non-negative integer; the server falls back to
// the building def's default when a map leaves this unset.
function updateEditBuildingResourceAmount(amount: number) {
  if (!selectedEditBuildingId.value) return
  const safe = Number.isFinite(amount) ? Math.max(0, Math.round(amount)) : 0
  model.value = {
    ...model.value,
    buildings: model.value.buildings.map((b) =>
      b.id === selectedEditBuildingId.value ? { ...b, resourceAmount: safe } : b,
    ),
  }
}

function updateEditWaveMode(
  mode: 'gameStart' | 'always' | 'specific' | 'repeating' | 'interval' | 'capture',
  waveNum: number,
) {
  if (!selectedEditBuildingId.value) return
  model.value = {
    ...model.value,
    buildings: model.value.buildings.map((b) => {
      if (b.id !== selectedEditBuildingId.value) return b
      const meta = { ...(b.metadata ?? {}) }
      // The timing modes are mutually exclusive — clear every mode's marker
      // (including the capture trigger) before setting the chosen one.
      delete meta['gameStart']
      delete meta['waveNumber']
      delete meta['startingWave']
      delete meta['waveInterval']
      delete meta['triggerCaptureZoneId']
      if (mode === 'gameStart') meta['gameStart'] = true
      if (mode === 'specific') meta['waveNumber'] = waveNum
      if (mode === 'repeating') meta['startingWave'] = waveNum
      if (mode === 'interval') meta['waveInterval'] = waveNum
      if (mode === 'capture') {
        // The mode is derived from triggerCaptureZoneId, so seed it with the
        // existing value or the first eligible zone so the selection sticks.
        const current = (b.metadata?.['triggerCaptureZoneId'] as string) || ''
        const zoneId = current || captureTriggerZones.value[0]?.id || ''
        if (zoneId) meta['triggerCaptureZoneId'] = zoneId
      }
      return { ...b, metadata: meta }
    }),
  }
}


function copySelectedBuilding() {
  const b = selectedEditBuilding.value
  if (!b) return
  copiedBuilding.value = {
    buildingType: b.buildingType,
    metadata: b.metadata ? JSON.parse(JSON.stringify(b.metadata)) : undefined,
  }
  isPasteMode.value = true
  selectedEditBuildingId.value = null
}

function pasteCopiedBuilding(cx: number, cy: number) {
  const copy = copiedBuilding.value
  if (!copy) return
  model.value = setBuildingTile(model.value, cx, cy, copy.buildingType, copy.metadata)
}

function startMoveBuilding() {
  const b = selectedEditBuilding.value
  if (!b) return
  movingBuilding.value = {
    id: b.id,
    buildingType: b.buildingType,
    metadata: b.metadata ? JSON.parse(JSON.stringify(b.metadata)) : undefined,
    x: b.x,
    y: b.y,
  }
  selectedEditBuildingId.value = null
}

function commitMoveBuilding(cx: number, cy: number) {
  const moving = movingBuilding.value
  if (!moving) return
  // Remove from old position then place at new position.
  let next = setBuildingTile(model.value, moving.x, moving.y, null)
  next = setBuildingTile(next, cx, cy, moving.buildingType, moving.metadata)
  model.value = next
  movingBuilding.value = null
  // Re-select the building at its new position so the edit panel reopens.
  const placed = model.value.buildings.find((b) => b.x === cx && b.y === cy && b.buildingType === moving.buildingType)
  if (placed) selectedEditBuildingId.value = placed.id
}

function cancelMoveBuilding() {
  movingBuilding.value = null
}

function startMoveNeutralSpawn() {
  const ns = selectedEditNeutralSpawn.value
  if (!ns) return
  movingNeutralSpawn.value = { id: ns.id, x: ns.x, y: ns.y }
  selectedEditNeutralSpawnId.value = null
}

function commitMoveNeutralSpawn(cx: number, cy: number) {
  const moving = movingNeutralSpawn.value
  if (!moving) return
  const newId = `neutral-spawn-${cx}-${cy}`
  const spawns = (model.value.neutralSpawns ?? []).map((ns) =>
    ns.id === moving.id ? { ...ns, id: newId, x: cx, y: cy } : ns,
  )
  model.value = { ...model.value, neutralSpawns: spawns }
  movingNeutralSpawn.value = null
  // Re-select at the new position so the edit panel reopens.
  selectedEditNeutralSpawnId.value = newId
}

function cancelMoveNeutralSpawn() {
  movingNeutralSpawn.value = null
}

function deleteSelectedBuilding() {
  if (!selectedEditBuildingId.value) return
  const b = selectedEditBuilding.value
  if (!b) return
  model.value = setBuildingTile(model.value, b.x, b.y, null)
  selectedEditBuildingId.value = null
  editPanelPos.value = null
}

async function loadAvailableMaps() {
  isLoadingMapCatalog.value = true
  mapLoadError.value = ''

  try {
    const maps = await fetchMapCatalog()
    availableMaps.value = maps
    if (!selectedLoadMapId.value) {
      selectedLoadMapId.value = maps[0]?.id ?? ''
    }
  } catch (error) {
    mapLoadError.value =
      error instanceof Error ? error.message : 'Failed to load saved maps.'
  } finally {
    isLoadingMapCatalog.value = false
  }
}

async function loadSelectedMapIntoEditor() {
  if (!selectedLoadMapId.value) return

  isLoadingSelectedMap.value = true
  mapLoadError.value = ''

  try {
    const catalogFile = await fetchMapCatalogFile(selectedLoadMapId.value)
    model.value = createEditorMapConfig(
      catalogFile.map.gridCols,
      catalogFile.map.gridRows,
      catalogFile.map,
    )
    placedUnits.value = model.value.placedUnits ?? []
    draftCols.value = model.value.gridCols
    draftRows.value = model.value.gridRows
    recenterCamera()
  } catch (error) {
    mapLoadError.value =
      error instanceof Error ? error.message : 'Failed to load selected map.'
  } finally {
    isLoadingSelectedMap.value = false
  }
}

function recenterCamera() {
  const targetCanvas = canvas.value
  if (!targetCanvas) return

  resizeCanvas()
  camera.centerOn(
    model.value.width / 2,
    model.value.height / 2,
    targetCanvas.width,
    targetCanvas.height,
    model.value.width,
    model.value.height,
  )
}

async function saveToServer() {
  if (saveLabel.value === 'Saving...') return
  saveError.value = ''
  saveLabel.value = 'Saving...'
  try {
    await saveMapCatalogFile(exportedCatalogFile.value)
    await onSaveSucceeded()
  } catch (err) {
    if (err instanceof LevelConflictError) {
      await handleLevelConflict(err.conflict)
      return
    }
    saveError.value = err instanceof Error ? err.message : 'Save failed'
    saveLabel.value = 'Save to Server'
  }
}

// Shared success tail for both a plain save and a reassign save.
async function onSaveSucceeded() {
  saveLabel.value = 'Saved!'
  await loadAvailableMaps()
  window.setTimeout(() => {
    saveLabel.value = 'Save to Server'
  }, 2000)
}

// A campaign level the author is claiming is already owned by another map.
// Carry the old owner's level definition across (so a pure geometry swap keeps
// its objectives), confirm with the author, then re-save as a reassignment —
// which clears the old map's campaign tag server-side.
async function handleLevelConflict(conflict: LevelConflict) {
  try {
    const ownerFile = await fetchMapCatalogFile(conflict.ownerMapId)
    if (ownerFile.map.campaign && model.value.campaign) {
      prefillEmptyCampaignFields(ownerFile.map.campaign)
    }
  } catch {
    // Non-fatal: without the owner we can still reassign using whatever the
    // author entered — they just don't get the field carry-over.
  }

  const confirmed = window.confirm(
    `Level "${conflict.levelId}" is currently the map "${conflict.ownerMapName}".\n\n` +
      `Move it to this map instead? "${conflict.ownerMapName}" will no longer be a ` +
      `campaign level (its other map content is untouched).`,
  )
  if (!confirmed) {
    saveLabel.value = 'Save to Server'
    return
  }

  saveLabel.value = 'Saving...'
  try {
    await saveMapCatalogFile(exportedCatalogFile.value, { reassignLevel: true })
    await onSaveSucceeded()
  } catch (err) {
    saveError.value = err instanceof Error ? err.message : 'Reassign failed'
    saveLabel.value = 'Save to Server'
  }
}

// Fill only the campaign fields the author left at their default with the
// previous owner's values — anything explicitly authored on the new map wins.
function prefillEmptyCampaignFields(ownerBlock: MapCampaignBlock) {
  const cur = model.value.campaign
  if (!cur) return
  const filled: MapCampaignBlock = { ...cur }
  if (!filled.displayName) filled.displayName = ownerBlock.displayName
  if (filled.prerequisiteLevelId == null) {
    filled.prerequisiteLevelId = ownerBlock.prerequisiteLevelId ?? null
  }
  if (!filled.sortOrder) filled.sortOrder = ownerBlock.sortOrder ?? 0
  if (!filled.description) filled.description = ownerBlock.description ?? ''
  if (!filled.objectives || filled.objectives.length === 0) {
    filled.objectives = ownerBlock.objectives ?? []
  }
  model.value = { ...model.value, campaign: filled }
}

async function copyExport() {
  try {
    await navigator.clipboard.writeText(serializedMap.value)
    copiedLabel.value = 'Copied'
    window.setTimeout(() => {
      copiedLabel.value = 'Copy Export'
    }, 1400)
  } catch {
    copiedLabel.value = 'Copy Failed'
  }
}

function resizeCanvas() {
  const targetCanvas = canvas.value
  if (!targetCanvas) return

  const width = targetCanvas.clientWidth
  const height = targetCanvas.clientHeight
  if (!width || !height) return

  targetCanvas.width = width
  targetCanvas.height = height
  clampCamera()
}

function clampCamera() {
  const targetCanvas = canvas.value
  if (!targetCanvas) return

  camera.clamp(
    targetCanvas.width,
    targetCanvas.height,
    model.value.width,
    model.value.height,
  )
}

function getPointerScreenPosition(event: MouseEvent | WheelEvent) {
  const rect = canvas.value?.getBoundingClientRect()
  if (!rect) {
    return { x: 0, y: 0 }
  }

  return {
    x: event.clientX - rect.left,
    y: event.clientY - rect.top,
  }
}

function updateHoverLabel(screenX: number, screenY: number) {
  const cell = getGridCellAtScreen(screenX, screenY)
  if (!cell) {
    hoverLabel.value = 'Outside map'
    return
  }

  const terrain = getTerrainAt(cell.x, cell.y) ?? 'empty'
  const obstacle = getObstacleAt(cell.x, cell.y) ?? 'none'
  const building = getBuildingAt(cell.x, cell.y)
  const buildingLabel = building ? building.buildingType : 'none'
  const pu = placedUnitAt(cell.x, cell.y)
  const unitLabel = pu ? ` unit: ${pu.unitType}(${pu.playerSlot})` : ''
  hoverLabel.value = `(${cell.x}, ${cell.y}) terrain: ${terrain}, obstacle: ${obstacle}, building: ${buildingLabel}${unitLabel}`
}

function getGridCellAtScreen(screenX: number, screenY: number) {
  const world = camera.screenToWorld(screenX, screenY)
  const x = Math.floor(world.x / model.value.cellSize)
  const y = Math.floor(world.y / model.value.cellSize)

  if (x < 0 || y < 0 || x >= model.value.gridCols || y >= model.value.gridRows) {
    return null
  }

  return { x, y }
}

function getBrushCells(cx: number, cy: number, size: number): Array<{ x: number; y: number }> {
  const half = Math.floor(size / 2)
  const { gridCols, gridRows } = model.value
  const cells: Array<{ x: number; y: number }> = []
  for (let dy = -half; dy <= half; dy++) {
    for (let dx = -half; dx <= half; dx++) {
      const x = cx + dx
      const y = cy + dy
      if (x < 0 || y < 0 || x >= gridCols || y >= gridRows) continue
      cells.push({ x, y })
    }
  }
  return cells
}

function paintAtScreen(screenX: number, screenY: number) {
  selectedEditBuildingId.value = null
  selectedEditPlacedUnitId.value = null
  selectedEditNeutralSpawnId.value = null
  const cell = getGridCellAtScreen(screenX, screenY)
  if (!cell) return

  const paintKey = `${cell.x}:${cell.y}:${activeBrushMode.value}:${brushSize.value}`
  if (paintKey === lastPaintKey) return
  lastPaintKey = paintKey

  // Buildings, enemy-spawn, and units ignore brush size — placement is per-cell.
  if (activeBrushMode.value === 'building') {
    paintBuildingAt(cell.x, cell.y)
    return
  }

  if (activeBrushMode.value === 'enemy-spawn') {
    paintEnemySpawnAt(cell.x, cell.y)
    return
  }

  if (activeBrushMode.value === 'neutral-spawn') {
    paintNeutralSpawnAt(cell.x, cell.y)
    return
  }

  if (activeBrushMode.value === 'unit') {
    paintUnitAt(cell.x, cell.y)
    return
  }

  const cells = getBrushCells(cell.x, cell.y, brushSize.value)

  if (activeBrushMode.value === 'erase') {
    let next = model.value
    for (const c of cells) {
      next = setTilePaint(
        setBuildingTile(
          setObstacleTile(setTerrainTile(next, c.x, c.y, null), c.x, c.y, null),
          c.x,
          c.y,
          null,
        ),
        c.x,
        c.y,
        null,
      )
      placedUnits.value = placedUnits.value.filter((u) => !(u.x === c.x && u.y === c.y))
      if (next.neutralSpawns) {
        next = {
          ...next,
          neutralSpawns: next.neutralSpawns.filter((s) => !(s.x === c.x && s.y === c.y)),
        }
      }
    }
    model.value = { ...next, placedUnits: placedUnits.value }
    return
  }

  if (activeBrushMode.value === 'terrain') {
    let next = model.value
    for (const c of cells) {
      next = setTerrainTile(next, c.x, c.y, selectedTerrain.value)
    }
    model.value = next
    return
  }

  if (activeBrushMode.value === 'tile') {
    if (!selectedTileCoord.value) return
    let next = model.value
    for (const c of cells) {
      next = setTilePaint(next, c.x, c.y, {
        sheet: selectedTileSheet.value,
        sx: selectedTileCoord.value.sx,
        sy: selectedTileCoord.value.sy,
      })
    }
    model.value = next
    return
  }

  // Obstacle brush (default fall-through).
  let next = model.value
  for (const c of cells) {
    next = setObstacleTile(next, c.x, c.y, selectedObstacle.value)
  }
  model.value = next
}

function paintBuildingAt(cx: number, cy: number) {
  let metadata: JsonObject | undefined

  if (selectedBuilding.value === 'spawn-point') {
    metadata = {
      townhallId: selectedSpawnTownhallId.value || null,
      fillOrder: Math.round(spawnPointFillOrder.value || 0),
      ...(spawnPointPlayerLabel.value ? { playerLabel: spawnPointPlayerLabel.value } : {}),
    }
  } else if (buildingObjectiveId.value) {
    metadata = { objectiveId: buildingObjectiveId.value }
  }

  if (isPlayerOwnableBuildingType(selectedBuilding.value) && buildingPlayerLabel.value) {
    metadata = { ...(metadata ?? {}), playerLabel: buildingPlayerLabel.value }
  }

  model.value = setBuildingTile(model.value, cx, cy, selectedBuilding.value, metadata)
}

// Stamps an enemy-spawnpoint tile using the dedicated Enemy Spawn brush. Lives
// in its own brush mode (not under Building) because the config UI is large
// and the building type is logical, not structural.
function paintEnemySpawnAt(cx: number, cy: number) {
  const metadata: JsonObject = {
    ...(enemyWaveMode.value === 'gameStart' ? { gameStart: true } : {}),
    ...(enemyWaveMode.value === 'specific' ? { waveNumber: enemyWaveNumber.value } : {}),
    ...(enemyWaveMode.value === 'repeating' ? { startingWave: enemyWaveNumber.value } : {}),
    ...(enemyWaveMode.value === 'interval' ? { waveInterval: enemyWaveInterval.value } : {}),
    ...(enemyWaveMode.value !== 'gameStart' ? { spawnDelaySeconds: enemySpawnDelay.value, spawnIntervalSeconds: enemySpawnInterval.value } : {}),
    ...(enemyWaveMode.value !== 'gameStart' && enemySpawnOnce.value ? { spawnOnce: true } : {}),
    ...(enemyIgnoreWaveClear.value ? { ignoreWaveClear: true } : {}),
    spawnCount: enemySpawnCount.value,
    unitType: enemyUnitType.value,
    ...(enemySpawnAlliance.value === 'neutral' ? { spawnAlliance: 'neutral' } : {}),
    ...(enemyObjectiveId.value ? { objectiveId: enemyObjectiveId.value } : {}),
    ...(enemyTargetPlayerLabel.value ? { targetPlayerLabel: enemyTargetPlayerLabel.value } : {}),
    ...(enemyWaveMode.value === 'capture' && enemyTriggerCaptureZoneId.value
      ? { triggerCaptureZoneId: enemyTriggerCaptureZoneId.value }
      : {}),
  }
  model.value = setBuildingTile(model.value, cx, cy, 'enemy-spawnpoint', metadata)
}

function paintNeutralSpawnAt(cx: number, cy: number) {
  const id = `neutral-spawn-${cx}-${cy}`
  const record: NeutralSpawn = {
    id,
    x: cx,
    y: cy,
    groupId: neutralGroupId.value,
    startingTier: neutralStartingTier.value,
    tierUpEveryNWaves: neutralTierUpEveryNWaves.value,
    aggroRange: neutralAggroRange.value,
    leashRange: neutralLeashRange.value,
    healthMultiplier: neutralHealthMultiplier.value,
    healthMultiplierPerWave: neutralHealthMultiplierPerWave.value,
    damageMultiplier: neutralDamageMultiplier.value,
    damageMultiplierPerWave: neutralDamageMultiplierPerWave.value,
  }
  const existing = model.value.neutralSpawns ?? []
  const existingIdx = existing.findIndex((s) => s.id === id)
  let next: NeutralSpawn[]
  if (existingIdx >= 0) {
    next = existing.map((s, i) => (i === existingIdx ? record : s))
  } else {
    next = [...existing, record]
  }
  model.value = { ...model.value, neutralSpawns: next }
}

function placedUnitAt(x: number, y: number): PlacedUnit | undefined {
  return placedUnits.value.find((u) => u.x === x && u.y === y)
}

function neutralSpawnAt(x: number, y: number): NeutralSpawn | undefined {
  return model.value.neutralSpawns?.find((s) => s.x === x && s.y === y)
}

function paintUnitAt(cx: number, cy: number) {
  const filtered = placedUnits.value.filter((u) => !(u.x === cx && u.y === cy))
  const slot = placedUnitPlayerSlot.value
  const id = `placed-unit-${slot}-${cx}-${cy}`
  const entry: PlacedUnit = {
    id,
    x: cx,
    y: cy,
    playerSlot: slot,
    unitType: placedUnitType.value,
  }
  if (slot === 'enemy') {
    entry.aggroRange = placedUnitAggroRange.value
    entry.leashRange = placedUnitLeashRange.value
  }
  const next = [...filtered, entry]
  placedUnits.value = next
  model.value = { ...model.value, placedUnits: next }
}

function getTerrainAt(x: number, y: number) {
  return model.value.terrain.find((tile) => tile.x === x && tile.y === y)?.terrain
}

function getObstacleAt(x: number, y: number) {
  return model.value.obstacles.find((tile) => tile.x === x && tile.y === y)?.obstacle
}

function getBuildingAt(x: number, y: number) {
  return model.value.buildings.find(
    (building) =>
      x >= building.x &&
      x < building.x + building.width &&
      y >= building.y &&
      y < building.y + building.height,
  )
}

function onMouseDown(event: MouseEvent) {
  const targetCanvas = canvas.value
  if (!targetCanvas) return

  const screen = getPointerScreenPosition(event)
  lastMouseX = screen.x
  lastMouseY = screen.y
  updateHoverLabel(screen.x, screen.y)

  if (event.button === 1) {
    event.preventDefault()
    isMiddleMouseDown = true
    return
  }

  // Right-click in Zone draw / captureDraw / claimPoint mode: remove from the active set.
  if (
    event.button === 2 &&
    (zoneSubMode.value === 'draw' || zoneSubMode.value === 'captureDraw' || zoneSubMode.value === 'claimPoint') &&
    selectedZoneId.value
  ) {
    event.preventDefault()
    const cell = getGridCellAtScreen(screen.x, screen.y)
    if (cell) {
      if (zoneSubMode.value === 'draw') {
        removeCellFromSelectedZone(cell.x, cell.y)
      } else if (zoneSubMode.value === 'captureDraw') {
        removeCaptureCellFromSelectedZone(cell.x, cell.y)
      } else {
        removeClaimPointFromSelectedZone(cell.x, cell.y)
      }
    }
    return
  }

  // Right-click in Tile brush mode: erase only painted tiles under the brush.
  if (event.button === 2 && paintModeEnabled.value && brushMode.value === 'tile') {
    event.preventDefault()
    const cell = getGridCellAtScreen(screen.x, screen.y)
    if (cell) {
      let next = model.value
      for (const c of getBrushCells(cell.x, cell.y, brushSize.value)) {
        next = setTilePaint(next, c.x, c.y, null)
      }
      model.value = next
    }
    return
  }

  if (event.button !== 0) return

  isLeftMouseDown = true

  if (isSpaceHeld) {
    isSpacePanning = true
    targetCanvas.style.cursor = 'grabbing'
    return
  }

  if (movingBuilding.value) {
    const cell = getGridCellAtScreen(screen.x, screen.y)
    if (cell) commitMoveBuilding(cell.x, cell.y)
    return
  }

  if (placingGuardSpawn.value) {
    const cell = getGridCellAtScreen(screen.x, screen.y)
    if (cell) commitGuardSpawn(cell.x, cell.y)
    return
  }

  if (movingClaimPoint.value) {
    const cell = getGridCellAtScreen(screen.x, screen.y)
    if (cell) commitMoveClaimPoint(cell.x, cell.y)
    return
  }

  if (movingNeutralSpawn.value) {
    const cell = getGridCellAtScreen(screen.x, screen.y)
    if (cell) commitMoveNeutralSpawn(cell.x, cell.y)
    return
  }

  if (isPasteMode.value && copiedBuilding.value) {
    const cell = getGridCellAtScreen(screen.x, screen.y)
    if (cell) pasteCopiedBuilding(cell.x, cell.y)
    return
  }

  // Zone place mode: left-click creates a new zone at the target cell.
  if (zoneSubMode.value === 'place' && !isSpaceHeld) {
    const cell = getGridCellAtScreen(screen.x, screen.y)
    if (cell) {
      createZoneAt(cell.x, cell.y)
      zoneSubMode.value = 'idle'
    }
    return
  }

  // Capture sub-zone draw mode: left-click/drag adds capture cells inside the zone.
  if (zoneSubMode.value === 'captureDraw' && selectedZoneId.value && !isSpaceHeld) {
    const cell = getGridCellAtScreen(screen.x, screen.y)
    if (cell) {
      addCaptureCellToSelectedZone(cell.x, cell.y)
      isPainting = true
      lastPaintKey = cellKey(cell.x, cell.y)
    }
    return
  }

  // Claim capture-point editing: left-click an existing point to SELECT it
  // (so it can be moved/deleted), or an empty cell inside the zone to ADD a new
  // point (and select it).
  if (zoneSubMode.value === 'claimPoint' && selectedZoneId.value && !isSpaceHeld) {
    const cell = getGridCellAtScreen(screen.x, screen.y)
    if (cell) {
      const hitIdx = claimPointIndexAt(cell.x, cell.y)
      if (hitIdx !== null) {
        selectedClaimPointIndex.value = hitIdx
      } else {
        const before = (selectedZone.value?.claimPoints ?? []).length
        addClaimPointToSelectedZone(cell.x, cell.y)
        const after = selectedZone.value?.claimPoints ?? []
        // Select the just-added point only if one was actually added (the add is
        // rejected for out-of-zone or duplicate cells).
        if (after.length > before) selectedClaimPointIndex.value = after.length - 1
      }
    }
    return
  }

  // Zone draw mode: left-click starts drag-add; right-click remove is handled above.
  if (zoneSubMode.value === 'draw' && selectedZoneId.value && !isSpaceHeld) {
    const cell = getGridCellAtScreen(screen.x, screen.y)
    if (cell) {
      addCellToSelectedZone(cell.x, cell.y)
      isPainting = true
      lastPaintKey = cellKey(cell.x, cell.y)
    }
    return
  }

  // Zone move mode: click a cell inside the zone to relocate its anchor node.
  // Clicks outside the perimeter are rejected, keeping move mode armed.
  if (zoneSubMode.value === 'move' && selectedZoneId.value && !isSpaceHeld) {
    const cell = getGridCellAtScreen(screen.x, screen.y)
    if (cell && moveSelectedZoneAnchor(cell.x, cell.y)) {
      zoneSubMode.value = 'idle'
    }
    return
  }

  if (!paintModeEnabled.value) {
    if (event.button === 0 && !isSpaceHeld) {
      const cell = getGridCellAtScreen(screen.x, screen.y)
      // Claim capture-point selection: when a claim zone is selected, clicking
      // one of its capture-point slots selects that point so it can be moved or
      // deleted (Move Point / Delete Point). Works without entering the Place
      // Capture Point sub-mode — you just need the zone selected.
      if (cell && selectedZone.value?.capture.type === 'claim') {
        const ptIdx = claimPointIndexAt(cell.x, cell.y)
        if (ptIdx !== null) {
          selectedClaimPointIndex.value = ptIdx
          selectedEditBuildingId.value = null
          selectedEditPlacedUnitId.value = null
          selectedEditNeutralSpawnId.value = null
          return
        }
      }
      // Clicked something other than a capture point → deselect any selected
      // point (closes its popup), so only one thing is selected at a time.
      selectedClaimPointIndex.value = null
      const hitUnit = cell ? placedUnitAt(cell.x, cell.y) : null
      const hitNeutral = !hitUnit && cell ? neutralSpawnAt(cell.x, cell.y) : null
      if (hitUnit) {
        selectedEditPlacedUnitId.value = hitUnit.id
        selectedEditNeutralSpawnId.value = null
        selectedEditBuildingId.value = null
      } else if (hitNeutral) {
        selectedEditNeutralSpawnId.value = hitNeutral.id
        selectedEditPlacedUnitId.value = null
        selectedEditBuildingId.value = null
      } else {
        const hit = cell ? getBuildingAt(cell.x, cell.y) : null
        if (hit) {
          selectedEditBuildingId.value = hit.id
          selectedEditPlacedUnitId.value = null
          selectedEditNeutralSpawnId.value = null
        } else {
          // Clicking a zone's node (its anchor cell) selects that zone and
          // opens the Zones section — a precise alternative to the list. Only
          // the anchor cell triggers this, so it won't fire from clicking
          // elsewhere in a zone's body.
          const zoneNode = cell ? zoneAtAnchorCell(cell.x, cell.y) : null
          if (zoneNode) {
            selectedZoneId.value = zoneNode.id
            zoneSubMode.value = 'idle'
            openSection.value = 'zones'
          }
          selectedEditBuildingId.value = null
          selectedEditPlacedUnitId.value = null
          selectedEditNeutralSpawnId.value = null
        }
      }
    }
    return
  }

  isPainting = true
  lastPaintKey = ''
  paintAtScreen(screen.x, screen.y)
}

function onMouseMove(event: MouseEvent) {
  const targetCanvas = canvas.value
  if (!targetCanvas) return

  const screen = getPointerScreenPosition(event)
  updateHoverLabel(screen.x, screen.y)

  if (isMiddleMouseDown || (isLeftMouseDown && isSpacePanning)) {
    const dx = screen.x - lastMouseX
    const dy = screen.y - lastMouseY

    camera.pan(-dx / camera.zoom, -dy / camera.zoom)
    clampCamera()

    lastMouseX = screen.x
    lastMouseY = screen.y
    return
  }

  // Zone draw drag: add cells while holding the mouse button, independent of paint mode.
  if (isPainting && zoneSubMode.value === 'draw' && selectedZoneId.value) {
    const cell = getGridCellAtScreen(screen.x, screen.y)
    if (cell) {
      const key = cellKey(cell.x, cell.y)
      if (key !== lastPaintKey) {
        addCellToSelectedZone(cell.x, cell.y)
        lastPaintKey = key
      }
    }
    return
  }

  // Capture sub-zone draw drag: add capture cells while holding mouse button.
  if (isPainting && zoneSubMode.value === 'captureDraw' && selectedZoneId.value) {
    const cell = getGridCellAtScreen(screen.x, screen.y)
    if (cell) {
      const key = cellKey(cell.x, cell.y)
      if (key !== lastPaintKey) {
        addCaptureCellToSelectedZone(cell.x, cell.y)
        lastPaintKey = key
      }
    }
    return
  }

  if (isPainting && paintModeEnabled.value) {
    paintAtScreen(screen.x, screen.y)
  }
}

function onMouseUp(event: MouseEvent) {
  if (event.button === 1) {
    isMiddleMouseDown = false
    return
  }

  if (event.button !== 0) return

  isLeftMouseDown = false
  isPainting = false
  lastPaintKey = ''

  // After a zone draw stroke, auto-fill any region the stroke just enclosed.
  // No-op unless the drawn cells now form a closed loop.
  if (zoneSubMode.value === 'draw' && selectedZoneId.value) {
    fillSelectedZoneEnclosed()
  }

  if (isSpacePanning) {
    isSpacePanning = false
    if (canvas.value) {
      canvas.value.style.cursor = isSpaceHeld ? 'grab' : (paintModeEnabled.value ? 'crosshair' : 'default')
    }
  }
}

function onWheel(event: WheelEvent) {
  event.preventDefault()
  const screen = getPointerScreenPosition(event)
  camera.adjustZoom(event.deltaY, screen.x, screen.y)
  clampCamera()
  updateHoverLabel(screen.x, screen.y)
}

function onMouseLeave() {
  hoverLabel.value = 'Outside map'
}

// True when the user is typing into an input/textarea/select (or any
// contentEditable element). Used to suppress the editor's canvas-level
// keybinds — without this gate, the Space `preventDefault` below swallows
// the space character before it can land in a text field, which is why
// users couldn't type spaces in the map Display Name / Description /
// objective description inputs.
function isEditingText(): boolean {
  const el = document.activeElement as HTMLElement | null
  if (!el) return false
  if (el.isContentEditable) return true
  const tag = el.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT'
}

function onKeyDown(event: KeyboardEvent) {
  if (isEditingText()) return

  if (event.key === 'Escape') {
    selectedEditBuildingId.value = null
    selectedEditPlacedUnitId.value = null
    selectedEditNeutralSpawnId.value = null
    isPasteMode.value = false
    cancelMoveBuilding()
    cancelMoveNeutralSpawn()
    movingClaimPoint.value = false
    // Exit zone place/draw/captureDraw/move sub-mode on Esc. Selection stays intact.
    if (zoneSubMode.value !== 'idle') {
      zoneSubMode.value = 'idle'
    }
  }

  if (event.key === 'Control') {
    isControlHeld.value = true
    if (canvas.value && !isSpacePanning) {
      canvas.value.style.cursor = getCanvasCursor()
    }
  }

  if (event.code !== 'Space') return
  isSpaceHeld = true
  if (canvas.value && !isSpacePanning) {
    canvas.value.style.cursor = getCanvasCursor()
  }
  event.preventDefault()
}

function onKeyUp(event: KeyboardEvent) {
  if (isEditingText()) return

  if (event.key === 'Control') {
    isControlHeld.value = false
    if (canvas.value && !isSpacePanning) {
      canvas.value.style.cursor = getCanvasCursor()
    }
  }

  if (event.code !== 'Space') return
  isSpaceHeld = false
  isSpacePanning = false
  if (canvas.value) {
    canvas.value.style.cursor = getCanvasCursor()
  }
  event.preventDefault()
}

function render() {
  const targetCanvas = canvas.value
  const ctx = targetCanvas?.getContext('2d')

  if (targetCanvas && ctx) {
    ctx.clearRect(0, 0, targetCanvas.width, targetCanvas.height)
    ctx.fillStyle = '#0a0a0a'
    ctx.fillRect(0, 0, targetCanvas.width, targetCanvas.height)

    ctx.save()
    ctx.scale(camera.zoom, camera.zoom)
    ctx.translate(-camera.x, -camera.y)

    drawMapBackground(ctx)
    drawZones(ctx)
    drawGrid(ctx)
    drawMapBounds(ctx)
    drawPlacedUnits(ctx)
    drawNeutralSpawns(ctx)
    drawSelectionHighlight(ctx)

    ctx.restore()

    updateEditPanelPos()
    drawEditorMinimap()
  }

  animationFrameId = requestAnimationFrame(render)
}

// Computes the minimap canvas size that fits MINIMAP_MAX_SIZE while
// preserving the map's aspect ratio. Used both to set the canvas pixel
// resolution and to convert between minimap coords and world coords.
function getMinimapDims(): { w: number; h: number } {
  const aspect = model.value.gridCols / model.value.gridRows
  if (aspect >= 1) {
    return { w: MINIMAP_MAX_SIZE, h: Math.max(1, Math.round(MINIMAP_MAX_SIZE / aspect)) }
  }
  return { h: MINIMAP_MAX_SIZE, w: Math.max(1, Math.round(MINIMAP_MAX_SIZE * aspect)) }
}

function drawEditorMinimap() {
  const c = minimapCanvas.value
  if (!c) return
  const { w, h } = getMinimapDims()
  if (c.width !== w || c.height !== h) {
    c.width = w
    c.height = h
    c.style.width = `${w}px`
    c.style.height = `${h}px`
    // Resolution change invalidates the (full map-resolution) terrain
    // surface only indirectly — but easier to just force a rebuild.
    minimapStaticDirty = true
  }
  const mctx = c.getContext('2d')
  if (!mctx) return

  if (minimapStaticDirty || !minimapTerrainSurface) {
    minimapTerrainSurface = buildTerrainSurface(model.value)
    minimapStaticDirty = false
  }

  const bounds = { x: 0, y: 0, width: w, height: h }
  drawMinimapBase(mctx, model.value, bounds, minimapTerrainSurface)
  // Zones before POIs so a marker on a zone border (e.g. a shop between
  // zones) stays readable — matches the in-game minimap layer order.
  drawMinimapZones(mctx, bounds)
  drawMinimapPOIs(mctx, model.value, bounds, null)

  // Viewport rect tied to the editor camera. Clipped to the minimap rect
  // so it can't bleed past the frame when the camera overscans the map
  // edges (the editor allows some pan overscan — see Camera.clamp).
  const main = canvas.value
  if (main && main.width > 0 && main.height > 0) {
    const mapW = model.value.width
    const mapH = model.value.height
    if (mapW > 0 && mapH > 0) {
      const viewW = main.width / camera.zoom
      const viewH = main.height / camera.zoom
      const vx = (camera.x / mapW) * w
      const vy = (camera.y / mapH) * h
      const vw = (viewW / mapW) * w
      const vh = (viewH / mapH) * h
      mctx.save()
      mctx.beginPath()
      mctx.rect(0, 0, w, h)
      mctx.clip()
      mctx.strokeStyle = 'rgba(125, 211, 252, 0.95)'
      mctx.lineWidth = 1.5
      mctx.strokeRect(vx, vy, vw, vh)
      mctx.restore()
    }
  }
}

// Draws each zone onto the editor minimap: a faint footprint fill plus the
// outer boundary edges, scaled from grid coords into the minimap rect. Mirrors
// the main-canvas zone style (outline, not filled boxes) so the minimap reads
// consistently. Selected zone is tinted blue.
function drawMinimapZones(mctx: CanvasRenderingContext2D, bounds: { x: number; y: number; width: number; height: number }) {
  const zones = model.value.zones
  if (!zones || zones.length === 0) return
  const cols = model.value.gridCols
  const rows = model.value.gridRows
  if (cols <= 0 || rows <= 0) return

  const cellW = bounds.width / cols
  const cellH = bounds.height / rows
  const selId = selectedZoneId.value

  for (const zone of zones) {
    const isSelected = zone.id === selId
    const edges = zoneBoundaryEdges(zone)

    mctx.save()

    mctx.fillStyle = isSelected ? 'rgba(96, 165, 250, 0.20)' : 'rgba(160, 160, 160, 0.18)'
    for (const [cx, cy] of zone.cells) {
      mctx.fillRect(bounds.x + cx * cellW, bounds.y + cy * cellH, cellW, cellH)
    }

    // Inset inward so adjacent zones don't overpaint a shared edge (small at
    // minimap scale, clamped so it can't cross a thin zone).
    const insetX = Math.min(1, cellW * 0.3)
    const insetY = Math.min(1, cellH * 0.3)
    mctx.setLineDash([])
    mctx.beginPath()
    for (const e of edges) {
      mctx.moveTo(bounds.x + e.x1 * cellW + e.nx * insetX, bounds.y + e.y1 * cellH + e.ny * insetY)
      mctx.lineTo(bounds.x + e.x2 * cellW + e.nx * insetX, bounds.y + e.y2 * cellH + e.ny * insetY)
    }
    // Light halo under a dark core so the outline reads at minimap scale too.
    mctx.lineCap = 'round'
    mctx.lineJoin = 'round'
    mctx.strokeStyle = 'rgba(255, 255, 255, 0.6)'
    mctx.lineWidth = 2.5
    mctx.stroke()
    mctx.strokeStyle = isSelected ? '#3b82f6' : 'rgba(20, 20, 20, 0.95)'
    mctx.lineWidth = 1
    mctx.stroke()

    mctx.restore()
  }
}

// Click / drag on the minimap pans the editor camera. Continuous tracking
// while the mouse is held down so the mapper can survey a large map by
// dragging.
function onMinimapMouseDown(event: MouseEvent) {
  if (event.button !== 0) return
  minimapDragging = true
  jumpEditorCameraFromMinimap(event)
}

function onMinimapMouseMove(event: MouseEvent) {
  if (!minimapDragging) return
  jumpEditorCameraFromMinimap(event)
}

function onMinimapMouseUp() {
  minimapDragging = false
}

function jumpEditorCameraFromMinimap(event: MouseEvent) {
  const c = minimapCanvas.value
  const main = canvas.value
  if (!c || !main) return
  const rect = c.getBoundingClientRect()
  if (rect.width === 0 || rect.height === 0) return
  const mx = event.clientX - rect.left
  const my = event.clientY - rect.top
  const mapW = model.value.width
  const mapH = model.value.height
  const worldX = (mx / rect.width) * mapW
  const worldY = (my / rect.height) * mapH
  camera.centerOn(worldX, worldY, main.width, main.height, mapW, mapH)
}

// Any change to the model's static content (terrain, obstacles, buildings,
// neutral spawns, grid size, default tile) invalidates the cached terrain
// surface so the minimap rebuilds on the next frame. Deep watch is fine
// here — the model is small and edits happen at human cadence.
watch(model, () => { minimapStaticDirty = true }, { deep: true })

function updateEditPanelPos() {
  const targetCanvas = canvas.value
  const b = selectedEditBuilding.value
  if (!b || !targetCanvas) {
    editPanelPos.value = null
  } else {
    const cellSize = model.value.cellSize
    const worldRight = (b.x + b.width) * cellSize
    const worldTop = b.y * cellSize
    const screenX = (worldRight - camera.x) * camera.zoom + 10
    const screenY = (worldTop - camera.y) * camera.zoom
    const clampedX = Math.min(screenX, targetCanvas.width - 250)
    const clampedY = Math.max(0, Math.min(screenY, targetCanvas.height - 100))
    editPanelPos.value = { x: clampedX, y: clampedY }
  }

  const pu = selectedEditPlacedUnit.value
  if (!pu || !targetCanvas) {
    placedUnitEditPanelPos.value = null
  } else {
    const cellSize = model.value.cellSize
    const worldRight = (pu.x + 1) * cellSize
    const worldTop = pu.y * cellSize
    const screenX = (worldRight - camera.x) * camera.zoom + 10
    const screenY = (worldTop - camera.y) * camera.zoom
    const clampedX = Math.min(screenX, targetCanvas.width - 250)
    const clampedY = Math.max(0, Math.min(screenY, targetCanvas.height - 100))
    placedUnitEditPanelPos.value = { x: clampedX, y: clampedY }
  }

  const ns = selectedEditNeutralSpawn.value
  if (!ns || !targetCanvas) {
    neutralSpawnEditPanelPos.value = null
  } else {
    const cellSize = model.value.cellSize
    const worldRight = (ns.x + 1) * cellSize
    const worldTop = ns.y * cellSize
    const screenX = (worldRight - camera.x) * camera.zoom + 10
    const screenY = (worldTop - camera.y) * camera.zoom
    const clampedX = Math.min(screenX, targetCanvas.width - 280)
    const clampedY = Math.max(0, Math.min(screenY, targetCanvas.height - 100))
    neutralSpawnEditPanelPos.value = { x: clampedX, y: clampedY }
  }

  const cp = selectedClaimPointCell.value
  if (!cp || !targetCanvas) {
    claimPointEditPanelPos.value = null
  } else {
    const cellSize = model.value.cellSize
    const worldRight = (cp.x + 2) * cellSize // right edge of the 2x2 slot
    const worldTop = cp.y * cellSize
    const screenX = (worldRight - camera.x) * camera.zoom + 10
    const screenY = (worldTop - camera.y) * camera.zoom
    const clampedX = Math.min(screenX, targetCanvas.width - 220)
    const clampedY = Math.max(0, Math.min(screenY, targetCanvas.height - 90))
    claimPointEditPanelPos.value = { x: clampedX, y: clampedY }
  }
}

function drawSelectionHighlight(ctx: CanvasRenderingContext2D) {
  const cellSize = model.value.cellSize
  const b = selectedEditBuilding.value
  if (b) {
    ctx.save()
    ctx.strokeStyle = '#60a5fa'
    ctx.lineWidth = 2 / camera.zoom
    ctx.setLineDash([])
    ctx.strokeRect(b.x * cellSize, b.y * cellSize, b.width * cellSize, b.height * cellSize)
    ctx.restore()
  }
  // Guard spawn marker: the cell the selected shop's guards ring around.
  if (b && typeof b.metadata?.['guardSpawnX'] === 'number' && typeof b.metadata?.['guardSpawnY'] === 'number') {
    const gx = (b.metadata['guardSpawnX'] as number + 0.5) * cellSize
    const gy = (b.metadata['guardSpawnY'] as number + 0.5) * cellSize
    ctx.save()
    ctx.strokeStyle = '#f59e0b'
    ctx.lineWidth = 2 / camera.zoom
    ctx.setLineDash([])
    ctx.beginPath()
    ctx.arc(gx, gy, cellSize * 0.4, 0, Math.PI * 2)
    ctx.moveTo(gx - cellSize * 0.55, gy)
    ctx.lineTo(gx + cellSize * 0.55, gy)
    ctx.moveTo(gx, gy - cellSize * 0.55)
    ctx.lineTo(gx, gy + cellSize * 0.55)
    ctx.stroke()
    ctx.restore()
  }
  const ns = selectedEditNeutralSpawn.value
  if (ns) {
    ctx.save()
    ctx.strokeStyle = '#60a5fa'
    ctx.lineWidth = 2 / camera.zoom
    ctx.setLineDash([])
    ctx.strokeRect(ns.x * cellSize, ns.y * cellSize, cellSize, cellSize)
    ctx.restore()
  }
  // Highlight the currently-selected zone (always visible, regardless of paint mode).
  const z = selectedZone.value
  if (z) {
    ctx.save()
    ctx.strokeStyle = '#60a5fa'
    ctx.lineWidth = 2 / camera.zoom
    ctx.setLineDash([])
    for (const [cx, cy] of z.cells) {
      ctx.strokeRect(cx * cellSize, cy * cellSize, cellSize, cellSize)
    }
    ctx.restore()
  }
}

function drawMapBackground(ctx: CanvasRenderingContext2D) {
  const cellSize = model.value.cellSize
  const { gridCols, gridRows } = model.value
  const tilesetReady = isTerrainTilesetReady()

  if (tilesetReady) {
    drawAutoTiledTerrain(ctx, {
      gridCols,
      gridRows,
      cellSize,
      defaultTile: model.value.defaultTile,
      terrain: model.value.terrain,
      tiles: model.value.tiles,
    })
  } else {
    ctx.fillStyle = DEFAULT_GRASS_COLOR
    ctx.fillRect(0, 0, model.value.width, model.value.height)
    for (const tile of model.value.terrain) {
      ctx.fillStyle = getTerrainColor(tile.terrain)
      ctx.fillRect(tile.x * cellSize, tile.y * cellSize, cellSize, cellSize)
    }
  }

  for (const tile of model.value.obstacles) {
    const gridW = tile.width ?? 1
    const gridH = tile.height ?? 1
    const footprintX = tile.x * cellSize
    const footprintY = tile.y * cellSize

    // Render bounds can extend beyond the footprint (e.g. tree canopies
    // reaching into the row above). Falls back to footprint when the
    // obstacle def has no render override.
    const renderDef = OBSTACLE_DEF_MAP.get(tile.obstacle)?.render
    const renderX = footprintX + (renderDef?.offsetX ?? 0) * cellSize
    const renderY = footprintY + (renderDef?.offsetY ?? 0) * cellSize
    const renderW = (renderDef?.width ?? gridW) * cellSize
    const renderH = (renderDef?.height ?? gridH) * cellSize

    const sprite = getObstacleSprite(tile.obstacle)
    if (sprite) {
      ctx.imageSmoothingEnabled = false
      ctx.drawImage(sprite, renderX, renderY, renderW, renderH)
    } else {
      const inset = cellSize * 0.14
      ctx.fillStyle = getObstacleColor(tile.obstacle)
      ctx.fillRect(
        renderX + inset,
        renderY + inset,
        renderW - inset * 2,
        renderH - inset * 2,
      )

      ctx.strokeStyle = 'rgba(15, 23, 42, 0.75)'
      ctx.lineWidth = 2 / camera.zoom
      ctx.strokeRect(
        renderX + inset,
        renderY + inset,
        renderW - inset * 2,
        renderH - inset * 2,
      )
    }
  }

  for (const building of model.value.buildings) {
    if (building.buildingType === 'enemy-spawnpoint') {
      drawEditorEnemySpawnpoint(ctx, building, cellSize)
      continue
    }

    const worldX = building.x * cellSize
    const worldY = building.y * cellSize
    const width = building.width * cellSize
    const height = building.height * cellSize
    const def = BUILDING_DEF_MAP.get(building.buildingType)
    const renderDef = getBuildingFallbackRender(building.buildingType)
    // Recipe shops and neutral-shop merchants use a per-instance "shopStyle" art
    // override when set; the matching per-style render config overrides the
    // sprite bounds so the editor preview frames the art the same way the
    // in-game renderer does.
    const shopStyle = building.metadata?.['shopStyle'] as string | undefined
    const styleRender = getBuildingStyleRender(building.buildingType, shopStyle)
    const spriteRenderDef = styleRender?.spriteRender ?? def?.spriteRender
    const styleSprite =
      building.buildingType === 'recipe-shop'
        ? getRecipeShopStyleSprite(shopStyle)
        : building.buildingType === 'neutral-shop'
          ? getNeutralShopStyleSprite(shopStyle)
          : null
    const sprite = styleSprite ?? getBuildingSprite(building.buildingType)
    // Sprite box may extend beyond the grid footprint (e.g. townhall's 3x3
    // sprite on a 3x2 footprint). Falls back to the footprint when no
    // override is set so unchanged buildings render identically.
    const spriteX = worldX + (spriteRenderDef?.offsetX ?? 0) * cellSize
    const spriteY = worldY + (spriteRenderDef?.offsetY ?? 0) * cellSize
    const spriteW = (spriteRenderDef?.width ?? building.width) * cellSize
    const spriteH = (spriteRenderDef?.height ?? building.height) * cellSize

    ctx.save()
    ctx.globalAlpha = building.visible ? 1 : 0.6

    if (sprite) {
      ctx.imageSmoothingEnabled = false
      ctx.drawImage(sprite, spriteX, spriteY, spriteW, spriteH)
    } else if (renderDef) {
      const fill = def?.color ?? getBuildingColor(building.buildingType, building.occupied)
      for (const layer of renderDef.layers) {
        ctx.fillStyle = layer.color === 'player' ? fill : layer.color
        if (!('kind' in layer) || layer.kind === 'rect') {
          ctx.fillRect(
            spriteX + layer.x * cellSize,
            spriteY + layer.y * cellSize,
            layer.w * cellSize,
            layer.h * cellSize,
          )
        } else if (layer.kind === 'tri') {
          const s = cellSize / 6
          const tlX = spriteX + layer.cx * cellSize + layer.sc * s
          const tlY = spriteY + layer.cy * cellSize + layer.sr * s
          const bslash = (layer.sc + layer.sr) % 2 === 1
          ctx.beginPath()
          if (!bslash) {
            if (layer.h === 0) { ctx.moveTo(tlX, tlY); ctx.lineTo(tlX + s, tlY); ctx.lineTo(tlX, tlY + s) }
            else { ctx.moveTo(tlX + s, tlY); ctx.lineTo(tlX + s, tlY + s); ctx.lineTo(tlX, tlY + s) }
          } else {
            if (layer.h === 0) { ctx.moveTo(tlX, tlY); ctx.lineTo(tlX + s, tlY); ctx.lineTo(tlX + s, tlY + s) }
            else { ctx.moveTo(tlX, tlY); ctx.lineTo(tlX, tlY + s); ctx.lineTo(tlX + s, tlY + s) }
          }
          ctx.closePath()
          ctx.fill()
        }
      }
    } else {
      const inset = cellSize * 0.18
      ctx.fillStyle = getBuildingColor(building.buildingType, building.occupied)
      ctx.fillRect(worldX + inset, worldY + inset, width - inset * 2, height - inset * 2)
    }

    if (!building.visible) {
      ctx.strokeStyle = 'rgba(226, 232, 240, 0.85)'
      ctx.lineWidth = 2 / camera.zoom
      ctx.setLineDash([10 / camera.zoom, 6 / camera.zoom])
      ctx.strokeRect(worldX, worldY, width, height)
    }

    ctx.restore()
  }
}

// Short label for the wave an enemy spawn point first activates on, shown on
// the tile in the editor. Mirrors editWaveMode / editWaveNumber precedence so
// the badge always matches the Spawn Timing the edit panel would show:
//   capture → 'C' (zone-triggered, no fixed wave)
//   gameStart → '0' (spawns at game start, before wave 1)
//   interval → the interval N (first fires on wave N)
//   specific → the wave number
//   repeating → the starting wave
//   else (always) → '1'
function enemySpawnWaveLabel(meta: Record<string, unknown> | null | undefined): string {
  if (!meta) return '1'
  if (meta['triggerCaptureZoneId']) return 'C'
  if (meta['gameStart'] === true) return '0'
  if ('waveInterval' in meta) return String(meta['waveInterval'] ?? 1)
  if ('waveNumber' in meta) return String(meta['waveNumber'] ?? 1)
  if ('startingWave' in meta) return String(meta['startingWave'] ?? 1)
  return '1'
}

function drawEditorEnemySpawnpoint(
  ctx: CanvasRenderingContext2D,
  building: {
    x: number
    y: number
    width: number
    height: number
    metadata?: Record<string, unknown> | null
  },
  cellSize: number,
) {
  const worldX = building.x * cellSize
  const worldY = building.y * cellSize
  const width = building.width * cellSize
  const height = building.height * cellSize

  ctx.save()
  ctx.fillStyle = 'rgba(153, 27, 27, 0.45)'
  ctx.fillRect(worldX, worldY, width, height)
  ctx.strokeStyle = '#fca5a5'
  ctx.lineWidth = 2 / camera.zoom
  ctx.setLineDash([8 / camera.zoom, 4 / camera.zoom])
  ctx.strokeRect(worldX, worldY, width, height)

  // Starting-wave badge — centered, sized to the footprint, with a dark
  // outline so it stays legible over the translucent red fill at any zoom.
  const label = enemySpawnWaveLabel(building.metadata)
  const fontSize = Math.min(width, height) * 0.62
  ctx.setLineDash([])
  ctx.font = `bold ${fontSize}px sans-serif`
  ctx.textAlign = 'center'
  ctx.textBaseline = 'middle'
  ctx.lineWidth = fontSize * 0.18
  ctx.strokeStyle = 'rgba(0, 0, 0, 0.85)'
  ctx.fillStyle = '#fee2e2'
  const cx = worldX + width / 2
  const cy = worldY + height / 2
  ctx.strokeText(label, cx, cy)
  ctx.fillText(label, cx, cy)
  ctx.restore()
}

function drawGrid(ctx: CanvasRenderingContext2D) {
  const targetCanvas = canvas.value
  if (!targetCanvas) return

  const gridSize = model.value.cellSize
  const worldWidth = targetCanvas.width / camera.zoom
  const worldHeight = targetCanvas.height / camera.zoom
  const viewStartX = camera.x
  const viewEndX = camera.x + worldWidth
  const viewStartY = camera.y
  const viewEndY = camera.y + worldHeight

  const startX = Math.max(0, Math.floor(viewStartX / gridSize) * gridSize)
  const endX = Math.min(model.value.width, viewEndX + gridSize)
  const startY = Math.max(0, Math.floor(viewStartY / gridSize) * gridSize)
  const endY = Math.min(model.value.height, viewEndY + gridSize)

  ctx.strokeStyle = '#1f1f1f'
  ctx.lineWidth = 1 / camera.zoom

  for (let x = startX; x < endX; x += gridSize) {
    ctx.beginPath()
    ctx.moveTo(x, startY)
    ctx.lineTo(x, endY)
    ctx.stroke()
  }

  for (let y = startY; y < endY; y += gridSize) {
    ctx.beginPath()
    ctx.moveTo(startX, y)
    ctx.lineTo(endX, y)
    ctx.stroke()
  }
}

function renderTilePicker() {
  const canvasEl = tilePickerCanvas.value
  if (!canvasEl) return
  const img = getSheetImage(selectedTileSheet.value)
  if (!img) {
    onSheetReady(selectedTileSheet.value, renderTilePicker)
    return
  }

  const scale = TILE_PICKER_SCALE
  canvasEl.width = img.naturalWidth * scale
  canvasEl.height = img.naturalHeight * scale

  const ctx = canvasEl.getContext('2d')
  if (!ctx) return

  ctx.imageSmoothingEnabled = false
  ctx.clearRect(0, 0, canvasEl.width, canvasEl.height)
  ctx.drawImage(img, 0, 0, canvasEl.width, canvasEl.height)

  // Highlight the selected tile, if any.
  if (selectedTileCoord.value) {
    const { sx, sy } = selectedTileCoord.value
    const tileSize = getSheetTileSize(selectedTileSheet.value)
    ctx.strokeStyle = '#facc15'
    ctx.lineWidth = 2
    ctx.strokeRect(sx * scale, sy * scale, tileSize * scale, tileSize * scale)
  }
}

function onTilePickerClick(event: MouseEvent) {
  const canvasEl = tilePickerCanvas.value
  if (!canvasEl) return
  const rect = canvasEl.getBoundingClientRect()
  // CSS size may differ from canvas pixel size; convert through the ratio.
  const cssToPx = canvasEl.width / rect.width
  const px = (event.clientX - rect.left) * cssToPx
  const py = (event.clientY - rect.top) * cssToPx
  const scale = TILE_PICKER_SCALE
  const tileSize = getSheetTileSize(selectedTileSheet.value)
  const sx = Math.floor(px / (tileSize * scale)) * tileSize
  const sy = Math.floor(py / (tileSize * scale)) * tileSize
  selectedTileCoord.value = { sx, sy }
  renderTilePicker()
}

function drawMapBounds(ctx: CanvasRenderingContext2D) {
  ctx.strokeStyle = '#444'
  ctx.lineWidth = 2 / camera.zoom
  ctx.strokeRect(0, 0, model.value.width, model.value.height)
}

function drawPlacedUnits(ctx: CanvasRenderingContext2D) {
  const cellSize = model.value.cellSize
  for (const pu of placedUnits.value) {
    const wx = pu.x * cellSize
    const wy = pu.y * cellSize
    const cx = wx + cellSize / 2
    const cy = wy + cellSize / 2
    const isSelected = pu.id === selectedEditPlacedUnitId.value

    const spriteSet = getUnitSpriteSet(pu.unitType)
    const portrait = spriteSet?.rotations.south ?? spriteSet?.rotations.north
      ?? spriteSet?.rotations.east ?? spriteSet?.rotations.west

    if (portrait) {
      if (!portrait.complete || portrait.naturalWidth === 0) {
        // Re-render when loaded
        portrait.addEventListener('load', () => render(), { once: true })
      } else {
        ctx.save()
        ctx.imageSmoothingEnabled = false

        // Tinted background
        ctx.fillStyle = pu.playerSlot === 'enemy' ? 'rgba(231,76,60,0.25)' : 'rgba(59,130,246,0.25)'
        ctx.fillRect(wx, wy, cellSize, cellSize)

        // Sprite centered in cell
        const scale = cellSize / Math.max(portrait.naturalWidth, portrait.naturalHeight)
        const w = portrait.naturalWidth * scale
        const h = portrait.naturalHeight * scale
        ctx.globalAlpha = 0.92
        ctx.drawImage(portrait, cx - w / 2, cy - h / 2, w, h)
        ctx.globalAlpha = 1

        // Border: yellow when selected, slot-color otherwise
        ctx.strokeStyle = isSelected ? '#facc15' : (pu.playerSlot === 'enemy' ? '#e74c3c' : '#3b82f6')
        ctx.lineWidth = isSelected ? 2 / camera.zoom : 1 / camera.zoom
        ctx.strokeRect(wx, wy, cellSize, cellSize)
        ctx.restore()
        continue
      }
    }

    // Fallback: circle with initial
    const r = cellSize * 0.35
    ctx.beginPath()
    ctx.arc(cx, cy, r, 0, Math.PI * 2)
    ctx.fillStyle = pu.playerSlot === 'enemy' ? '#e74c3c' : '#3b82f6'
    ctx.globalAlpha = 0.85
    ctx.fill()
    ctx.globalAlpha = 1
    ctx.strokeStyle = isSelected ? '#facc15' : '#fff'
    ctx.lineWidth = isSelected ? 2 : 1
    ctx.stroke()
    ctx.fillStyle = '#fff'
    ctx.font = `bold ${Math.max(8, cellSize * 0.3)}px sans-serif`
    ctx.textAlign = 'center'
    ctx.textBaseline = 'middle'
    ctx.fillText(pu.unitType.charAt(0).toUpperCase(), cx, cy)
  }
  ctx.textAlign = 'left'
  ctx.textBaseline = 'alphabetic'
}

function drawNeutralSpawns(ctx: CanvasRenderingContext2D) {
  const spawns = model.value.neutralSpawns
  if (!spawns || spawns.length === 0) return
  const cellSize = model.value.cellSize
  for (const ns of spawns) {
    const px = ns.x * cellSize + cellSize / 2
    const py = ns.y * cellSize + cellSize / 2
    ctx.save()
    ctx.fillStyle = NEUTRAL_PLAYER_COLOR
    ctx.beginPath()
    ctx.arc(px, py, cellSize * 0.4, 0, Math.PI * 2)
    ctx.fill()
    ctx.fillStyle = '#fff'
    ctx.font = `bold ${Math.max(8, cellSize * 0.3)}px sans-serif`
    ctx.textAlign = 'center'
    ctx.textBaseline = 'middle'
    ctx.fillText('N', px, py)
    ctx.restore()
  }
}

/**
 * Draw all zones on the editor canvas.
 * - Interior cells: lighter grey, semi-transparent fill.
 * - Perimeter cells: darker grey, semi-transparent fill.
 * - Anchor node: small labelled circle.
 * - Adjacency edges: dashed line between connected anchor nodes.
 * - Selected zone: highlighted border.
 * Zones draw above terrain so the author can see the map through them.
 * Per spec §10.1 and the perimeter-derived-not-stored invariant.
 */
function drawZones(ctx: CanvasRenderingContext2D) {
  const zones = model.value.zones
  if (!zones || zones.length === 0) return
  const cellSize = model.value.cellSize
  const selId = selectedZoneId.value

  // First pass: faint footprint fill + outline only the OUTER boundary edges
  // (the cell sides bordering a non-member) so zones read as a thin outline,
  // not a band of filled perimeter squares. The faint full-footprint fill keeps
  // cell membership visible for authoring.
  for (const zone of zones) {
    const isSelected = zone.id === selId
    const edges = zoneBoundaryEdges(zone)

    ctx.save()

    // Faint fill across the whole zone footprint (membership aid).
    ctx.fillStyle = isSelected
      ? 'rgba(96, 165, 250, 0.15)'
      : 'rgba(160, 160, 160, 0.12)'
    for (const [x, y] of zone.cells) {
      ctx.fillRect(x * cellSize, y * cellSize, cellSize, cellSize)
    }

    // Boundary outline. Built once, then stroked twice: a light halo under a
    // dark (or blue, when selected) core line so it stays legible over both the
    // faint zone fill and any terrain color. Inset INWARD along each edge's
    // normal so two adjacent zones sharing a boundary each draw their own line
    // instead of overpainting the same pixels.
    const inset = Math.min(2.5 / camera.zoom, cellSize * 0.3)
    ctx.setLineDash([])
    ctx.beginPath()
    for (const e of edges) {
      const ox = e.nx * inset
      const oy = e.ny * inset
      ctx.moveTo(e.x1 * cellSize + ox, e.y1 * cellSize + oy)
      ctx.lineTo(e.x2 * cellSize + ox, e.y2 * cellSize + oy)
    }
    ctx.lineCap = 'round'
    ctx.lineJoin = 'round'
    ctx.strokeStyle = 'rgba(255, 255, 255, 0.65)'
    ctx.lineWidth = (isSelected ? 4 : 3.5) / camera.zoom
    ctx.stroke()
    ctx.strokeStyle = isSelected ? '#1d4ed8' : 'rgba(20, 20, 20, 0.95)'
    ctx.lineWidth = (isSelected ? 2 : 1.5) / camera.zoom
    ctx.stroke()

    ctx.restore()

    // Capture sub-zone overlay: warm orange tint drawn on top of the normal fill
    // so authors see the capture area distinctly inside the zone.
    if (zone.captureCells && zone.captureCells.length > 0) {
      ctx.save()
      ctx.fillStyle = isSelected
        ? 'rgba(251, 146, 60, 0.55)'  // orange-400 at higher opacity when selected
        : 'rgba(234, 88, 12, 0.40)'   // orange-600 at base opacity
      for (const [x, y] of zone.captureCells) {
        ctx.fillRect(x * cellSize, y * cellSize, cellSize, cellSize)
      }
      // Thin inner stroke to delineate individual capture cells.
      ctx.strokeStyle = isSelected ? 'rgba(253, 186, 116, 0.85)' : 'rgba(251, 146, 60, 0.60)'
      ctx.lineWidth = (isSelected ? 1.5 : 1) / camera.zoom
      ctx.setLineDash([])
      for (const [x, y] of zone.captureCells) {
        ctx.strokeRect(x * cellSize, y * cellSize, cellSize, cellSize)
      }
      ctx.restore()
    }

    // Claim build-slot overlay: a cyan/teal 2×2 block at EACH authored capture
    // point (zone.claimPoints), or a single block at the anchor when no points
    // are authored — mirroring the server's claimPointCells fallback. Visually
    // distinct from the orange capture sub-zone and the grey zone fill. Cells
    // outside the grid are skipped defensively.
    if (zone.capture.type === 'claim') {
      const cols = model.value.gridCols
      const rows = model.value.gridRows
      const points: [number, number][] =
        zone.claimPoints && zone.claimPoints.length > 0
          ? zone.claimPoints
          : [[zone.anchor.x, zone.anchor.y]]

      ctx.save()
      ctx.setLineDash([])
      // Only explicit claimPoints entries are selectable; the anchor fallback
      // (when claimPoints is empty) has no index to select.
      const hasExplicitPoints = !!(zone.claimPoints && zone.claimPoints.length > 0)
      for (let i = 0; i < points.length; i++) {
        const [ax, ay] = points[i]
        const isSelectedPoint =
          isSelected && hasExplicitPoints && selectedClaimPointIndex.value === i
        const slotCells: [number, number][] = [
          [ax,     ay    ],
          [ax + 1, ay    ],
          [ax,     ay + 1],
          [ax + 1, ay + 1],
        ].filter(([cx, cy]) => cx >= 0 && cy >= 0 && cx < cols && cy < rows) as [number, number][]
        if (slotCells.length === 0) continue

        // Semi-transparent fill per cell — amber for the selected point, cyan
        // otherwise.
        ctx.fillStyle = isSelectedPoint
          ? 'rgba(250, 204, 21, 0.38)'   // amber-400: selected point
          : isSelected
            ? 'rgba(34, 211, 238, 0.40)' // cyan-400 at higher opacity when zone selected
            : 'rgba(6, 182, 212, 0.28)'  // cyan-500 at base opacity
        for (const [cx, cy] of slotCells) {
          ctx.fillRect(cx * cellSize, cy * cellSize, cellSize, cellSize)
        }
        // Solid outline around the full 2×2 block (not per-cell strokes).
        // Bounding rect so partial slots near edges size correctly.
        ctx.strokeStyle = isSelectedPoint
          ? 'rgba(253, 224, 71, 0.98)'   // amber-300: selected point outline
          : isSelected
            ? 'rgba(103, 232, 249, 0.95)'
            : 'rgba(34, 211, 238, 0.75)'
        ctx.lineWidth = (isSelectedPoint ? 3 : isSelected ? 2 : 1.5) / camera.zoom
        const minCx = Math.min(...slotCells.map(([cx]) => cx))
        const minCy = Math.min(...slotCells.map(([, cy]) => cy))
        const maxCx = Math.max(...slotCells.map(([cx]) => cx))
        const maxCy = Math.max(...slotCells.map(([, cy]) => cy))
        ctx.strokeRect(
          minCx * cellSize,
          minCy * cellSize,
          (maxCx - minCx + 1) * cellSize,
          (maxCy - minCy + 1) * cellSize,
        )
      }
      ctx.restore()
    }
  }

  // Second pass: directed prerequisite edges (zone → its prerequisites).
  // Each link is drawn from the zone's anchor toward the prerequisite anchor,
  // with a small arrowhead at the prerequisite end to convey direction.
  for (const zone of zones) {
    const ax = zone.anchor.x * cellSize + cellSize / 2
    const ay = zone.anchor.y * cellSize + cellSize / 2
    for (const prereqId of zone.adjacent ?? []) {
      const prereqZone = zones.find((z) => z.id === prereqId)
      if (!prereqZone) continue
      const bx = prereqZone.anchor.x * cellSize + cellSize / 2
      const by = prereqZone.anchor.y * cellSize + cellSize / 2
      const dx = bx - ax
      const dy = by - ay
      const len = Math.sqrt(dx * dx + dy * dy)
      if (len < 1) continue

      ctx.save()
      ctx.strokeStyle = 'rgba(250, 204, 21, 0.75)' // amber
      ctx.lineWidth = 2 / camera.zoom
      ctx.setLineDash([8 / camera.zoom, 4 / camera.zoom])
      ctx.beginPath()
      ctx.moveTo(ax, ay)
      ctx.lineTo(bx, by)
      ctx.stroke()

      // Arrowhead at the prerequisite end (solid, no dash).
      const nx = dx / len
      const ny = dy / len
      const arrowLen = Math.min(cellSize * 0.35, len * 0.25)
      const arrowAngle = Math.PI / 6 // 30 degrees
      ctx.setLineDash([])
      ctx.lineWidth = 1.5 / camera.zoom
      ctx.beginPath()
      ctx.moveTo(bx, by)
      ctx.lineTo(
        bx - arrowLen * (nx * Math.cos(arrowAngle) - ny * Math.sin(arrowAngle)),
        by - arrowLen * (ny * Math.cos(arrowAngle) + nx * Math.sin(arrowAngle)),
      )
      ctx.moveTo(bx, by)
      ctx.lineTo(
        bx - arrowLen * (nx * Math.cos(arrowAngle) + ny * Math.sin(arrowAngle)),
        by - arrowLen * (ny * Math.cos(arrowAngle) - nx * Math.sin(arrowAngle)),
      )
      ctx.stroke()
      ctx.restore()
    }
  }

  // Third pass: anchor nodes (drawn last so they sit above edges).
  for (const zone of zones) {
    const isSelected = zone.id === selId
    const cx = zone.anchor.x * cellSize + cellSize / 2
    const cy = zone.anchor.y * cellSize + cellSize / 2
    const r = cellSize * 0.3

    ctx.save()
    ctx.beginPath()
    ctx.arc(cx, cy, r, 0, Math.PI * 2)
    ctx.fillStyle = isSelected ? '#3b82f6' : 'rgba(100, 100, 100, 0.85)'
    ctx.fill()
    ctx.strokeStyle = isSelected ? '#e0f2fe' : '#ddd'
    ctx.lineWidth = (isSelected ? 2 : 1) / camera.zoom
    ctx.setLineDash([])
    ctx.stroke()

    // Label: first char of id or name.
    const label = (zone.name ?? zone.id).charAt(0).toUpperCase()
    ctx.fillStyle = '#fff'
    ctx.font = `bold ${Math.max(8, cellSize * 0.25)}px sans-serif`
    ctx.textAlign = 'center'
    ctx.textBaseline = 'middle'
    ctx.fillText(label, cx, cy)
    ctx.restore()
  }

  // Fourth pass: home-zone spawn-link connectors. For each zone with a
  // lockedSpawnLabel, draw a dashed gold/amber line from the zone's anchor
  // cell center to the matching spawn-point building's cell center, plus a
  // small circle marker at the spawn end. Distinct from adjacency lines
  // (which are also amber dashes) by using a tighter dash pattern and a
  // warm-gold fill on the endpoint marker.
  const buildings = model.value.buildings
  for (const zone of zones) {
    if (!zone.lockedSpawnLabel) continue
    const spawnBuilding = buildings.find(
      (b) => b.buildingType === 'spawn-point' && b.metadata?.['playerLabel'] === zone.lockedSpawnLabel,
    )
    if (!spawnBuilding) continue

    const ax = zone.anchor.x * cellSize + cellSize / 2
    const ay = zone.anchor.y * cellSize + cellSize / 2
    const sx = spawnBuilding.x * cellSize + cellSize / 2
    const sy = spawnBuilding.y * cellSize + cellSize / 2

    ctx.save()
    // Dashed gold connector line
    ctx.strokeStyle = 'rgba(250, 176, 5, 0.90)'
    ctx.lineWidth = 2.5 / camera.zoom
    ctx.setLineDash([5 / camera.zoom, 3 / camera.zoom])
    ctx.beginPath()
    ctx.moveTo(ax, ay)
    ctx.lineTo(sx, sy)
    ctx.stroke()

    // Small filled circle at the spawn-point end
    ctx.setLineDash([])
    const mr = cellSize * 0.18
    ctx.beginPath()
    ctx.arc(sx, sy, mr, 0, Math.PI * 2)
    ctx.fillStyle = 'rgba(250, 176, 5, 0.92)'
    ctx.fill()
    ctx.strokeStyle = '#fff'
    ctx.lineWidth = 1 / camera.zoom
    ctx.stroke()
    ctx.restore()
  }
}

// Re-render the tile picker whenever it becomes visible or the sheet changes.
// Wait a tick so the v-if'd canvas is mounted before we try to draw.
watch(
  [brushMode, selectedTileSheet],
  async () => {
    if (brushMode.value !== 'tile') return
    await new Promise((resolve) => requestAnimationFrame(resolve))
    renderTilePicker()
  },
  { immediate: true },
)

// Clear the selected tile coord when switching sheets — coords aren't portable
// across sheets (different tile sizes, different content).
watch(selectedTileSheet, () => {
  selectedTileCoord.value = null
})

onMounted(() => {
  const targetCanvas = canvas.value
  if (!targetCanvas) return

  // Defer initial sizing to the next animation frame so the flex layout has
  // resolved and canvas.clientWidth/clientHeight are non-zero.
  requestAnimationFrame(() => {
    recenterCamera()
  })
  targetCanvas.style.cursor = getCanvasCursor()
  void loadAvailableMaps()
  void loadCampaignCatalog()
  void fetchBuildingDefs()
    .then(({ buildings, buildingStyles }) => {
      initBuildingDefs(buildings)
      initBuildingStyleRenders(buildingStyles)
    })
    .catch(() => {})
  void fetchObstacleDefs().then(initObstacleDefs).catch(() => {})
  void fetchNeutralGroups().then((tiers) => { neutralGroupTiers.value = tiers }).catch(() => {})
  void fetchRecipeLists().then((lists) => { recipeLists.value = lists }).catch(() => {})
  void fetchItemLists().then((lists) => { itemLists.value = lists }).catch(() => {})
  void fetchItemDefs().then((items) => { itemDefsList.value = items }).catch(() => {})
  void fetchPerkDefs().then((perks) => { perkDefsList.value = perks }).catch(() => {})
  void fetchUnitDefs()
    .then(({ units, paths, pathsByUnit }) => {
      unitDefsList.value = units
      initPathBounds(paths)
      initPathsByUnitType(pathsByUnit)
      // Bucket every catalog unit by its declared faction. Buckets are created
      // on demand from `def.faction`, so a new faction directory on the server
      // produces a new dropdown entry on next editor load with zero edits here.
      const grouped: Record<string, Array<{ type: UnitType; label: string }>> = {}
      for (const def of units) {
        const bucket = grouped[def.faction] ?? (grouped[def.faction] = [])
        bucket.push({ type: def.type as UnitType, label: def.name })
      }
      unitDefsByFaction.value = grouped
      const factionKeys = Object.keys(grouped).sort()
      // If the previously-selected faction isn't in the catalog any more
      // (renamed/removed), snap to the first available one so dropdowns stay
      // coherent. Preserve the selection when it's still valid.
      if (!grouped[placedUnitFaction.value]) {
        placedUnitFaction.value = factionKeys[0] ?? ''
      }
      // Same for placedUnitType under the (possibly new) faction.
      const factionTypes = (grouped[placedUnitFaction.value] ?? []).map((u) => u.type as string)
      if (!factionTypes.includes(placedUnitType.value)) {
        placedUnitType.value = grouped[placedUnitFaction.value]?.[0]?.type ?? placedUnitType.value
      }
      // Enemy-spawnpoint pool spans every faction now; snap enemyUnitType to
      // any available unit when the previously-selected type has gone away.
      const enemyPool = factionKeys.flatMap((f) => grouped[f] ?? [])
      if (!enemyPool.some((u) => u.type === enemyUnitType.value)) {
        enemyUnitType.value = enemyPool[0]?.type ?? enemyUnitType.value
      }
    })
    .catch(() => {})

  targetCanvas.addEventListener('mousedown', onMouseDown)
  targetCanvas.addEventListener('mousemove', onMouseMove)
  targetCanvas.addEventListener('mouseleave', onMouseLeave)
  targetCanvas.addEventListener('wheel', onWheel, { passive: false })
  targetCanvas.addEventListener('contextmenu', (event) => event.preventDefault())

  window.addEventListener('mouseup', onMouseUp)
  window.addEventListener('keydown', onKeyDown)
  window.addEventListener('keyup', onKeyUp)
  window.addEventListener('resize', resizeCanvas)

  if (typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(() => {
      resizeCanvas()
    })
    resizeObserver.observe(targetCanvas)
  }

  render()
})

watch(paintModeEnabled, () => {
  if (!canvas.value || isSpacePanning) return
  canvas.value.style.cursor = getCanvasCursor()
})

watch(isPasteMode, () => {
  if (!canvas.value || isSpacePanning) return
  canvas.value.style.cursor = getCanvasCursor()
})

watch([movingBuilding, movingNeutralSpawn], () => {
  if (!canvas.value || isSpacePanning) return
  canvas.value.style.cursor = getCanvasCursor()
})

watch(
  townhallOptions,
  (options) => {
    if (selectedSpawnTownhallId.value && options.some((option) => option.id === selectedSpawnTownhallId.value)) {
      return
    }
    selectedSpawnTownhallId.value = ''
  },
  { immediate: true },
)

watch(selectedBuilding, () => {
  enemyObjectiveId.value = ''
  enemyTriggerCaptureZoneId.value = ''
  enemySpawnAlliance.value = 'enemy'
  buildingObjectiveId.value = ''
  spawnPointPlayerLabel.value = ''
  enemyTargetPlayerLabel.value = ''
})

// When the brush faction changes, reset the placed-unit type to the first
// valid type for that faction so the dropdown selection stays coherent.
watch(placedUnitFaction, (faction) => {
  const types = unitDefsByFaction.value[faction] ?? []
  placedUnitType.value = types[0]?.type ?? placedUnitType.value
})

watch(
  () => model.value.buildings,
  (buildings) => {
    if (selectedEditBuildingId.value && !buildings.find((b) => b.id === selectedEditBuildingId.value)) {
      selectedEditBuildingId.value = null
    }
  },
)

watch(placedUnits, (units) => {
  if (selectedEditPlacedUnitId.value && !units.find((u) => u.id === selectedEditPlacedUnitId.value)) {
    selectedEditPlacedUnitId.value = null
  }
})

watch(
  () => model.value.neutralSpawns,
  (spawns) => {
    const id = selectedEditNeutralSpawnId.value
    if (id && !(spawns?.some((s) => s.id === id))) {
      selectedEditNeutralSpawnId.value = null
    }
  },
)

onBeforeUnmount(() => {
  if (canvas.value) {
    canvas.value.removeEventListener('mousedown', onMouseDown)
    canvas.value.removeEventListener('mousemove', onMouseMove)
    canvas.value.removeEventListener('mouseleave', onMouseLeave)
    canvas.value.removeEventListener('wheel', onWheel)
  }

  window.removeEventListener('mouseup', onMouseUp)
  window.removeEventListener('keydown', onKeyDown)
  window.removeEventListener('keyup', onKeyUp)
  window.removeEventListener('resize', resizeCanvas)

  resizeObserver?.disconnect()
  resizeObserver = null

  cancelAnimationFrame(animationFrameId)

  // Tear down any in-progress playtest match so it doesn't keep a live
  // GameClient/network connection open after the editor unmounts.
  if (playtestPlaying.value) {
    stopPlaytestMatch()
  }
})
</script>

<style scoped>
.world-editor-root {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100%;
  min-width: 0;
  min-height: 0;
}

.editor-shell {
  display: grid;
  grid-template-columns: minmax(410px, 450px) minmax(0, 1fr);
  grid-template-rows: minmax(0, 1fr);
  gap: 12px;
  align-items: stretch;
  width: 100%;
  flex: 1 1 auto;
  min-height: 0;
}

.editor-controls,
.editor-preview {
  background: rgba(3, 8, 14, 0.86);
  border: 1px solid rgba(148, 163, 184, 0.22);
  border-radius: 16px;
  box-shadow: 0 24px 60px rgba(0, 0, 0, 0.26);
  backdrop-filter: blur(14px);
}

.editor-controls {
  min-height: 0;
  max-height: 100%;
  overflow-y: auto;
  padding: 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  scrollbar-width: thin;
  scrollbar-color: rgba(148, 163, 184, 0.35) transparent;
}

.editor-controls::-webkit-scrollbar {
  width: 8px;
}

.editor-controls::-webkit-scrollbar-thumb {
  background: rgba(148, 163, 184, 0.35);
  border-radius: 4px;
}

.editor-controls::-webkit-scrollbar-thumb:hover {
  background: rgba(148, 163, 184, 0.55);
}

.editor-controls::-webkit-scrollbar-track {
  background: transparent;
}

.editor-section {
  border: 1px solid rgba(148, 163, 184, 0.18);
  border-radius: 12px;
  background: rgba(8, 14, 24, 0.55);
  overflow: clip;
  flex: 0 0 auto;
}

.editor-section--open {
  background: rgba(8, 14, 24, 0.72);
}

.editor-section__summary {
  width: 100%;
  border: 0;
  cursor: pointer;
  padding: 10px 12px;
  font-size: 0.78rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: #f8fafc;
  text-align: left;
  background: linear-gradient(180deg, rgba(25, 35, 52, 0.92), rgba(14, 22, 36, 0.94));
}

.editor-section__summary::after {
  content: '+';
  float: right;
  color: #d7bb84;
}

.editor-section--open .editor-section__summary::after {
  content: '-';
}

.editor-section__body {
  display: grid;
  gap: 8px;
  padding: 10px;
}

.editor-title {
  font-size: 0.9rem;
  font-weight: 700;
  color: #f8fafc;
}

.editor-copy {
  margin: 0;
  color: rgba(226, 232, 240, 0.82);
  font-size: 0.75rem;
  line-height: 1.2;
}

.control-group {
  display: grid;
  gap: 4px;
}

.tile-picker-hint {
  font-size: 0.7rem;
  color: rgba(226, 232, 240, 0.72);
  padding: 2px 0;
}

.tile-picker {
  display: block;
  max-width: 100%;
  image-rendering: pixelated;
  border: 1px solid rgba(148, 163, 184, 0.24);
  border-radius: 4px;
  cursor: crosshair;
  background: #0a0a0a;
}

.control-group label,
.preview-header,
.summary-row,
.hint-list {
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.75rem;
}

.control-group input,
.control-group select,
.control-group textarea,
.apply-size,
.preset-row button,
.export-actions button,
.paint-toggle {
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
}

.paint-toggle {
  font-weight: 700;
}

.paint-toggle--active {
  background: rgba(22, 101, 52, 0.95);
  border-color: rgba(74, 222, 128, 0.45);
}

.preset-row,
.export-actions,
.summary-row {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

.summary-row span {
  padding: 4px 8px;
  border-radius: 999px;
  background: rgba(30, 41, 59, 0.72);
}

.clear-everything {
  margin-top: 4px;
  border: 1px solid rgba(248, 113, 113, 0.35);
  border-radius: 10px;
  background: rgba(127, 29, 29, 0.55);
  color: #fecaca;
  padding: 7px 9px;
  font-size: 0.78rem;
  font-weight: 700;
  cursor: pointer;
}

.clear-everything:hover:not(:disabled) {
  background: rgba(153, 27, 27, 0.75);
  border-color: rgba(252, 165, 165, 0.55);
  color: #fff1f2;
}

.clear-everything:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.hint-list {
  display: grid;
  gap: 2px;
}

.save-to-server {
  border: 1px solid rgba(56, 189, 248, 0.35);
  border-radius: 10px;
  background: rgba(8, 145, 178, 0.28);
  color: #bae6fd;
  padding: 7px 9px;
  font-size: 0.78rem;
  cursor: pointer;
}

.save-to-server:hover:not(:disabled) {
  background: rgba(8, 145, 178, 0.48);
}

.save-to-server:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}

.save-to-server--saved {
  background: rgba(22, 101, 52, 0.75);
  border-color: rgba(74, 222, 128, 0.45);
  color: #bbf7d0;
}

.save-error {
  font-size: 0.72rem;
  color: #fca5a5;
  padding: 4px 2px;
}

.export-box {
  min-height: 104px;
  resize: vertical;
  border-radius: 12px;
  border: 1px solid rgba(148, 163, 184, 0.2);
  background: rgba(2, 6, 23, 0.88);
  color: #dbeafe;
  padding: 8px;
  font-family: Consolas, 'Courier New', monospace;
  font-size: 0.72rem;
}

.metadata-box {
  min-height: 52px;
  resize: vertical;
}

.spawn-point-config,
.enemy-spawn-config,
.unit-brush-config {
  border: 1px solid rgba(239, 68, 68, 0.3);
  border-radius: 8px;
  padding: 8px;
}

.unit-brush-config {
  background: rgba(30, 58, 138, 0.22);
  border-color: rgba(96, 165, 250, 0.35);
}

.spawn-point-config {
  background: rgba(8, 145, 178, 0.18);
  border-color: rgba(56, 189, 248, 0.35);
}

.spawn-point-loadout-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 70px auto;
  gap: 6px;
  align-items: center;
}

.spawn-point-row-button {
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.72rem;
}

.enemy-spawn-config {
  background: rgba(127, 29, 29, 0.18);
}

/* Zone brush config panel */
.zone-brush-config {
  background: rgba(20, 50, 30, 0.28);
  border: 1px solid rgba(74, 222, 128, 0.30);
  border-radius: 8px;
  padding: 8px;
  display: grid;
  gap: 6px;
}

.zone-brush-config__title {
  font-size: 0.72rem;
  font-weight: 700;
  letter-spacing: 0.07em;
  text-transform: uppercase;
  color: #86efac;
}

.zone-brush-config__actions {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  margin-top: 4px;
}

.zone-brush-config__action--active {
  background: rgba(59, 130, 246, 0.30) !important;
  border-color: rgba(96, 165, 250, 0.70) !important;
  color: #bfdbfe !important;
}

.zone-brush-config__delete {
  margin-left: auto;
  color: #fca5a5;
  border-color: rgba(239, 68, 68, 0.40) !important;
}

.zone-brush-config__hint {
  font-size: 0.68rem;
  color: #94a3b8;
  font-style: italic;
}

/* Zone aura (Bonuses) authoring UI */
.zone-brush-config__auras {
  display: grid;
  gap: 6px;
  margin-top: 4px;
  padding-top: 6px;
  border-top: 1px solid rgba(148, 163, 184, 0.2);
}

.zone-brush-config__auras-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.zone-brush-config__auras-label {
  font-size: 0.72rem;
  font-weight: 600;
  color: #cbd5e1;
}

.zone-brush-config__aura-row {
  display: grid;
  grid-template-columns: 1fr auto 70px auto;
  gap: 4px;
  align-items: center;
}

.zone-brush-config__aura-remove {
  color: #fca5a5;
  border-color: rgba(239, 68, 68, 0.4) !important;
  padding: 2px 6px;
}

/* Directed capture-prerequisite link UI */
.zone-brush-config__links {
  display: grid;
  gap: 6px;
}

.zone-brush-config__links-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
}

.zone-brush-config__links-label {
  font-size: 0.72rem;
  font-weight: 600;
  color: #cbd5e1;
}

.zone-brush-config__links-toggle {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 3px 8px;
  font-size: 0.7rem;
  border: 1px solid rgba(148, 163, 184, 0.22);
  border-radius: 6px;
  background: rgba(15, 23, 42, 0.7);
  color: #e2e8f0;
}

.zone-brush-config__links-caret {
  font-size: 0.6rem;
  color: #94a3b8;
}

.zone-brush-config__links-panel {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 6px 8px;
  background: rgba(2, 6, 23, 0.6);
  border: 1px solid rgba(148, 163, 184, 0.15);
  border-radius: 6px;
  max-height: 160px;
  overflow-y: auto;
  scrollbar-width: thin;
  scrollbar-color: rgba(148, 163, 184, 0.25) transparent;
}

.zone-brush-config__links-empty {
  font-size: 0.68rem;
  color: #64748b;
  font-style: italic;
}

.zone-brush-config__links-row {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 0.7rem;
  color: #e2e8f0;
  user-select: none;
}

.zone-brush-config__links-row input[type='checkbox'] {
  margin: 0;
  accent-color: #60a5fa;
}

.zone-brush-config__require-all {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 0.7rem;
  color: #cbd5e1;
  user-select: none;
}

.zone-brush-config__require-all input[type='checkbox'] {
  margin: 0;
  accent-color: #60a5fa;
}

.zone-brush-config__require-all--disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

/* Zone sidebar section */
.zone-sidebar__add-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.zone-sidebar__add--active {
  background: rgba(59, 130, 246, 0.30) !important;
  border-color: rgba(96, 165, 250, 0.70) !important;
  color: #bfdbfe !important;
}

.zone-sidebar__hint {
  font-size: 0.68rem;
  color: #94a3b8;
  font-style: italic;
}

.zone-sidebar__list {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.zone-sidebar__empty {
  font-size: 0.72rem;
  color: #64748b;
  font-style: italic;
  padding: 6px 4px;
}

.zone-sidebar__row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  width: 100%;
  padding: 5px 8px;
  border: 1px solid rgba(148, 163, 184, 0.18);
  border-radius: 6px;
  background: rgba(15, 23, 42, 0.55);
  color: #e2e8f0;
  font-size: 0.75rem;
  text-align: left;
}

.zone-sidebar__row:hover {
  background: rgba(30, 41, 59, 0.75);
  border-color: rgba(148, 163, 184, 0.35);
}

.zone-sidebar__row--active {
  background: rgba(29, 78, 216, 0.28) !important;
  border-color: rgba(96, 165, 250, 0.55) !important;
  color: #bfdbfe !important;
}

.zone-sidebar__row-name {
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.zone-sidebar__row-type {
  font-size: 0.65rem;
  color: #86efac;
  white-space: nowrap;
  opacity: 0.85;
}

.wave-config-block {
  display: grid;
  gap: 8px;
  padding: 8px;
  border-radius: 8px;
  border: 1px solid rgba(215, 187, 132, 0.25);
  background: rgba(58, 35, 18, 0.35);
}

.wave-config-title {
  font-size: 0.72rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: #d7bb84;
}

.field-hint {
  font-weight: 400;
  opacity: 0.65;
  text-transform: none;
  letter-spacing: 0;
}

.debug-flag-row {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  user-select: none;
}

.debug-flag-row input[type='checkbox'] {
  margin: 0;
}

/* Campaign authoring (map-editor-authors-campaign-maps) */
.campaign-objectives {
  margin-top: 12px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.campaign-objectives__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: #d7bb84;
}

.campaign-objectives__header button {
  font-size: 11px;
  padding: 4px 10px;
}

.campaign-objectives__empty {
  font-size: 12px;
  opacity: 0.6;
  padding: 8px;
  border: 1px dashed rgba(255, 255, 255, 0.15);
  border-radius: 4px;
}

.campaign-objective {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 8px;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 4px;
}

.campaign-objective__row {
  display: flex;
  gap: 6px;
  align-items: center;
}

.campaign-objective__row > input,
.campaign-objective__row > select {
  flex: 1 1 auto;
  min-width: 0;
}

.campaign-objective__id {
  max-width: 140px;
}

.campaign-objective__remove {
  flex: 0 0 auto;
  width: 28px;
  height: 28px;
  padding: 0;
  font-size: 16px;
  line-height: 1;
}

.campaign-objective__meta {
  flex-wrap: wrap;
  justify-content: flex-start;
  row-gap: 8px;
}

/* Keep the scope dropdown from growing to fill the row — otherwise it pushes
   the reward fields (especially the last one, Badge Reward) off the right
   edge and out of view. */
.campaign-objective__meta > select {
  flex: 0 0 auto;
}

/* Compact, fixed-width number inputs so both reward fields fit on the row
   (and wrap cleanly to a second line on narrow panels). */
.campaign-objective__reward input {
  flex: 0 0 auto;
  width: 64px;
}

.campaign-objective__required {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  user-select: none;
}

.campaign-objective__reward {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  user-select: none;
}

.campaign-objective__config {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 6px;
  padding: 6px 0 2px 0;
  border-top: 1px dashed rgba(255, 255, 255, 0.08);
}

.campaign-objective__config label {
  display: flex;
  flex-direction: column;
  gap: 2px;
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: #c8b894;
}

.campaign-objective__config input,
.campaign-objective__config select {
  width: 100%;
  box-sizing: border-box;
}

/* .victory-condition-row + .vc-id-badge rules removed alongside the Victory
   Conditions authoring card (campaign-objectives-and-metrics §6.5). */

.editor-preview {
  min-height: 0;
  min-width: 0;
  padding: 12px;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.preview-header {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
  font-size: 0.78rem;
}

.canvas-frame {
  flex: 1 1 auto;
  min-height: 0;
  overflow: hidden;
  border-radius: 14px;
  border: 1px solid rgba(68, 68, 68, 0.9);
  background: #0a0a0a;
  position: relative;
}

.edit-panel {
  position: absolute;
  z-index: 20;
  min-width: 210px;
  max-width: 250px;
  max-height: 75vh;
  overflow-y: auto;
  background: rgba(3, 8, 18, 0.95);
  border: 1px solid rgba(96, 165, 250, 0.45);
  border-radius: 10px;
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  backdrop-filter: blur(10px);
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.55);
  font-size: 12px;
  color: #e2e8f0;
  scrollbar-width: thin;
  scrollbar-color: rgba(148,163,184,0.3) transparent;
}

.edit-panel__header {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 2px;
}

.edit-panel__title {
  font-size: 11px;
  font-weight: 700;
  color: #93c5fd;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  flex: 1;
}

.edit-panel__copy {
  background: rgba(96, 165, 250, 0.15);
  border: 1px solid rgba(96, 165, 250, 0.35);
  border-radius: 4px;
  color: #93c5fd;
  cursor: pointer;
  font-size: 10px;
  font-weight: 600;
  padding: 1px 6px;
  line-height: 1.4;
}

.edit-panel__copy:hover {
  background: rgba(96, 165, 250, 0.28);
  color: #bfdbfe;
}

.edit-panel__move {
  background: rgba(52, 211, 153, 0.15);
  border: 1px solid rgba(52, 211, 153, 0.35);
  border-radius: 4px;
  color: #6ee7b7;
  cursor: pointer;
  font-size: 10px;
  font-weight: 600;
  padding: 1px 6px;
  line-height: 1.4;
}

.edit-panel__move:hover {
  background: rgba(52, 211, 153, 0.28);
  color: #a7f3d0;
}

.edit-panel__close {
  background: none;
  border: none;
  color: #94a3b8;
  cursor: pointer;
  font-size: 13px;
  line-height: 1;
  padding: 0 2px;
}

.edit-panel__close:hover { color: #f1f5f9; }

.preview-header__paste-mode {
  display: flex;
  align-items: center;
  gap: 8px;
  color: #fbbf24;
  font-weight: 600;
}

.preview-header__cancel-paste {
  background: none;
  border: 1px solid rgba(251, 191, 36, 0.4);
  border-radius: 4px;
  color: #fbbf24;
  cursor: pointer;
  font-size: 10px;
  padding: 1px 6px;
}

.preview-header__cancel-paste:hover {
  background: rgba(251, 191, 36, 0.12);
}

.edit-field {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.edit-field label {
  font-size: 10px;
  color: #94a3b8;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.edit-field__hint {
  font-size: 10px;
  color: #64748b;
  font-style: italic;
  margin-top: 2px;
}

.guard-spawn-controls {
  display: flex;
  gap: 6px;
}

.guard-spawn-btn {
  flex: 1;
  background: rgba(15, 23, 42, 0.85);
  border: 1px solid rgba(148, 163, 184, 0.25);
  border-radius: 5px;
  color: #e2e8f0;
  font-size: 11px;
  padding: 4px 6px;
}

.guard-spawn-btn--active {
  border-color: #f59e0b;
  color: #f59e0b;
}

.edit-field input,
.edit-field select {
  background: rgba(15, 23, 42, 0.85);
  border: 1px solid rgba(148, 163, 184, 0.25);
  border-radius: 5px;
  color: #e2e8f0;
  font-size: 12px;
  padding: 3px 6px;
  width: 100%;
  box-sizing: border-box;
}

.edit-field input:focus,
.edit-field select:focus {
  outline: none;
  border-color: rgba(96, 165, 250, 0.6);
}

.edit-field input:disabled,
.edit-field select:disabled {
  opacity: 0.35;
  cursor: not-allowed;
}

.checkbox-label {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: #cbd5e1;
  cursor: pointer;
  user-select: none;
}

.checkbox-label input[type='checkbox'] {
  width: auto;
  margin: 0;
  accent-color: #60a5fa;
  cursor: pointer;
}

.edit-loadout-row {
  display: flex;
  gap: 4px;
  align-items: center;
  margin-top: 3px;
}

.edit-loadout-row select { flex: 1; }

.edit-loadout-row button {
  background: rgba(239, 68, 68, 0.18);
  border: 1px solid rgba(239, 68, 68, 0.3);
  border-radius: 4px;
  color: #fca5a5;
  font-size: 11px;
  padding: 2px 5px;
}

.edit-loadout-row button:disabled { opacity: 0.35; }

.edit-add-btn {
  background: rgba(59, 130, 246, 0.15);
  border: 1px solid rgba(59, 130, 246, 0.3);
  border-radius: 5px;
  color: #93c5fd;
  font-size: 11px;
  padding: 4px 8px;
  margin-top: 2px;
  text-align: left;
}

.edit-add-btn:hover { background: rgba(59, 130, 246, 0.25); }

.edit-delete-btn {
  margin-top: 4px;
  background: rgba(239, 68, 68, 0.15);
  border: 1px solid rgba(239, 68, 68, 0.3);
  border-radius: 6px;
  color: #fca5a5;
  cursor: pointer;
  font-size: 11px;
  padding: 5px 8px;
  text-align: center;
}

.edit-delete-btn:hover { background: rgba(239, 68, 68, 0.28); }

.edit-panel__empty {
  color: #64748b;
  font-size: 11px;
  font-style: italic;
}

.editor-canvas {
  width: 100%;
  height: 100%;
  display: block;
  background: #0a0a0a;
}

.we-play-canvas {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  display: block;
  background: #0a0a0a;
  z-index: 25;
}

.editor-minimap {
  position: absolute;
  top: 12px;
  left: 12px;
  z-index: 15;
  border: 1px solid rgba(166, 191, 255, 0.45);
  border-radius: 6px;
  background: #000;
  cursor: crosshair;
  /* Width/height set in script based on map aspect ratio. */
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.6);
}

@media (max-width: 1100px) {
  .editor-shell {
    grid-template-columns: minmax(400px, 430px) minmax(0, 1fr);
  }

  .canvas-frame {
    height: 60vh;
  }
}

@media (max-width: 820px) {
  .editor-shell {
    grid-template-columns: 1fr;
  }

  .editor-preview {
    min-height: 0;
  }
}

.we-modal-overlay {
  position: fixed;
  inset: 0;
  z-index: 200;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(6, 8, 14, 0.72);
}

.we-modal {
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: #12141c;
  border: 1px solid rgba(166, 191, 255, 0.35);
  border-radius: 8px;
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.55);
}

.we-modal--wide {
  width: min(1100px, 94vw);
  height: min(880px, 88vh);
}

.we-modal__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 16px;
  border-bottom: 1px solid rgba(166, 191, 255, 0.25);
  font-weight: 600;
  letter-spacing: 0.02em;
  flex: 0 0 auto;
}

.we-modal__body {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
}
</style>
