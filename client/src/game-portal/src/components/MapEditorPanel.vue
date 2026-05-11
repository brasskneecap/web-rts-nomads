<template>
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
              <label for="wave-prep">Prep Duration <span class="field-hint">(sec, 0 = default 60)</span></label>
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

          <div class="wave-config-block">
            <div class="wave-config-title">Victory Conditions <span class="field-hint">(all must be met)</span></div>
            <div
              v-for="(vc, i) in model.victoryConditions ?? []"
              :key="vc.id"
              class="victory-condition-row"
            >
              <select
                :value="vc.type"
                @change="updateVictoryCondition(i, 'type', ($event.target as HTMLSelectElement).value)"
              >
                <option value="killUnit">Kill Unit</option>
                <option value="destroyBuilding">Destroy Building</option>
                <option value="surviveWaves">Survive Waves</option>
              </select>
              <input
                :value="vc.label ?? ''"
                @input="updateVictoryCondition(i, 'label', ($event.target as HTMLInputElement).value)"
                type="text"
                placeholder="Label (shown in HUD)"
              />
              <input
                v-if="vc.type === 'killUnit'"
                :value="vc.count ?? 1"
                @input="updateVictoryCondition(i, 'count', +($event.target as HTMLInputElement).value)"
                type="number"
                min="1"
                title="Kills required"
                style="width: 56px"
              />
              <span class="vc-id-badge" :title="`Objective ID: ${vc.id}`">{{ vc.id }}</span>
              <button type="button" @click="removeVictoryCondition(i)">✕</button>
            </div>
            <button type="button" @click="addVictoryCondition()">+ Add Condition</button>
          </div>

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
              <option value="unit">Unit</option>
              <option value="erase">Erase</option>
            </select>
          </div>

          <div v-if="brushMode !== 'building' && brushMode !== 'unit'" class="control-group">
            <label for="brush-size">Brush Size</label>
            <select id="brush-size" v-model.number="brushSize" :disabled="!paintModeEnabled">
              <option :value="1">1 × 1</option>
              <option :value="3">3 × 3</option>
              <option :value="5">5 × 5</option>
              <option :value="7">7 × 7</option>
            </select>
          </div>

          <div class="control-group">
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
              <option value="goldmine">Goldmine</option>
              <option value="townhall">Townhall</option>
              <option value="spawn-point">Spawn Point</option>
              <option value="enemy-spawnpoint">Enemy Spawnpoint</option>
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

          <div v-if="brushMode === 'building' && selectedBuilding === 'enemy-spawnpoint'" class="control-group enemy-spawn-config">
            <label for="enemy-unit-type">Unit Type</label>
            <select
              id="enemy-unit-type"
              v-model="enemyUnitType"
              :disabled="!paintModeEnabled"
            >
              <option v-for="u in ENEMY_SPAWN_UNITS" :key="u.type" :value="u.type">{{ u.label }}</option>
            </select>
            <label for="enemy-wave-mode">Spawn Timing</label>
            <select id="enemy-wave-mode" v-model="enemyWaveMode" :disabled="!paintModeEnabled">
              <option value="gameStart">Game Start</option>
              <option value="always">Always (legacy)</option>
              <option value="specific">Specific Wave</option>
              <option value="repeating">Every Wave From</option>
            </select>
            <template v-if="enemyWaveMode !== 'always' && enemyWaveMode !== 'gameStart'">
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
          </div>

          <div v-if="brushMode === 'unit'" class="control-group unit-brush-config">
            <label>Faction</label>
            <select v-model="placedUnitFaction" :disabled="!paintModeEnabled">
              <option value="raider">Raider</option>
              <option value="neutral">Neutral</option>
              <option value="human">Human</option>
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

          <div v-if="brushMode === 'building' && selectedBuilding !== 'enemy-spawnpoint' && destroyBuildingObjectives.length" class="control-group">
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
        <span v-else-if="isPasteMode" class="preview-header__paste-mode">
          Paste mode — click to place {{ copiedBuilding?.buildingType }}
          <button type="button" class="preview-header__cancel-paste" @click="isPasteMode = false">Cancel (Esc)</button>
        </span>
        <span v-else>{{ paintModeEnabled ? 'Paint mode armed' : 'Navigation mode' }}</span>
        <span>{{ hoverLabel }}</span>
      </div>

      <div class="canvas-frame">
        <canvas ref="canvas" class="editor-canvas"></canvas>
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
              <label>Spawn Timing</label>
              <select :value="editWaveMode" @change="updateEditWaveMode(($event.target as HTMLSelectElement).value as 'gameStart'|'always'|'specific'|'repeating', editWaveNumber)">
                <option value="gameStart">Game Start</option>
                <option value="always">Always</option>
                <option value="specific">Specific Wave</option>
                <option value="repeating">Every Wave From</option>
              </select>
            </div>
            <div v-if="editWaveMode !== 'always' && editWaveMode !== 'gameStart'" class="edit-field">
              <label>{{ editWaveMode === 'specific' ? 'Wave Number' : 'Starting Wave' }}</label>
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

          <!-- generic building: objective -->
          <template v-else>
            <div v-if="destroyBuildingObjectives.length" class="edit-field">
              <label>Destroy Objective</label>
              <select :value="selectedEditBuilding.metadata?.['objectiveId'] ?? ''" @change="updateEditMeta('objectiveId', ($event.target as HTMLSelectElement).value || undefined)">
                <option value="">None</option>
                <option v-for="vc in destroyBuildingObjectives" :key="vc.id" :value="vc.id">{{ vc.label || vc.id }}</option>
              </select>
            </div>
            <div v-else class="edit-panel__empty">No editable fields for this building type.</div>
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
              <option value="raider">Raider</option>
              <option value="neutral">Neutral</option>
              <option value="human">Human</option>
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

          <button type="button" class="edit-delete-btn" @click="deleteSelectedPlacedUnit">Delete Unit</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { fetchBuildingDefs, fetchMapCatalog, fetchMapCatalogFile, fetchObstacleDefs, fetchUnitDefs, saveMapCatalogFile } from '@/game/maps/catalog'
import type {
  BuildingType,
  JsonObject,
  JsonValue,
  MapCatalogEntry,
  MapCatalogFile,
  MapConfig,
  ObstacleType,
  PlacedUnit,
  TerrainType,
  TileSheet,
  UnitType,
  VictoryCondition,
} from '@/game/network/protocol'
import type { UnitFaction } from '@/game/maps/unitDefs'
import { Camera } from '@/game/rendering/Camera'
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
import { getBuildingSprite } from '@/game/rendering/buildingSprites'
import { getObstacleSprite } from '@/game/rendering/obstacleSprites'
import { getUnitSpriteSet } from '@/game/rendering/unitSprites'
import { initObstacleDefs, OBSTACLE_DEF_MAP } from '@/game/maps/obstacleDefs'
import { BUILDING_DEF_MAP, initBuildingDefs } from '@/game/maps/buildingDefs'
import { initPathBounds } from '@/game/maps/unitDefs'

const model = defineModel<MapConfig>({ required: true })

const canvas = ref<HTMLCanvasElement | null>(null)
const tilePickerCanvas = ref<HTMLCanvasElement | null>(null)

const TILE_PICKER_SCALE = 2
const brushMode = ref<'terrain' | 'tile' | 'obstacle' | 'building' | 'unit' | 'erase'>('terrain')
const brushSize = ref<1 | 3 | 5 | 7>(1)
const selectedTerrain = ref<TerrainType>('grass')
const selectedObstacle = ref<ObstacleType>('rock')
const selectedBuilding = ref<BuildingType>('goldmine')
const selectedTileSheet = ref<TileSheet>('tileset')

// All unit types known to the catalog, populated by fetchUnitDefs. Drives the
// faction-filtered type pickers below — adding a unit JSON on the server (with
// a valid faction) makes it immediately available in the editor on next load.
const unitDefsByFaction = ref<Record<UnitFaction, Array<{ type: UnitType; label: string }>>>({
  raider: [],
  neutral: [],
  human: [],
})
// playerSpawnUnits remains a subset filtered by trainLabel — it's what
// barracks-style spawn-points reference for in-game training pools, not the
// editor brushing flow.
const playerSpawnUnits = ref<Array<{ type: UnitType; label: string }>>([])

const selectedTileCoord = ref<{ sx: number; sy: number } | null>(null)
const selectedSpawnTownhallId = ref('')
const spawnPointFillOrder = ref(0)
const spawnPointPlayerLabel = ref('')
const enemyTargetPlayerLabel = ref('')
const enemySpawnDelay = ref(0)
const enemySpawnInterval = ref(10)
const enemySpawnCount = ref(1)
const enemySpawnOnce = ref(false)
const enemyIgnoreWaveClear = ref(false)
const enemyUnitType = ref('raider')
const enemyWaveMode = ref<'gameStart' | 'always' | 'specific' | 'repeating'>('always')
const enemyWaveNumber = ref(1)
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
const openSection = ref<'setup' | 'paint' | 'export' | null>('paint')
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
const editPanelPos = ref<{ x: number; y: number } | null>(null)
const placedUnitEditPanelPos = ref<{ x: number; y: number } | null>(null)
const copiedBuilding = ref<{ buildingType: string; metadata: JsonObject | undefined } | null>(null)
const isPasteMode = ref(false)
const movingBuilding = ref<{ id: string; buildingType: string; metadata: JsonObject | undefined; x: number; y: number } | null>(null)
let nextVictoryConditionId = 1

const camera = new Camera()
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
const townhallOptions = computed(() =>
  model.value.buildings
    .filter((building) => building.buildingType === 'townhall')
    .map((building) => ({
      id: building.id,
      label: `${building.id} (${building.x}, ${building.y})`,
    })),
)

const killUnitObjectives = computed(() =>
  (model.value.victoryConditions ?? []).filter((vc) => vc.type === 'killUnit'),
)

const destroyBuildingObjectives = computed(() =>
  (model.value.victoryConditions ?? []).filter((vc) => vc.type === 'destroyBuilding'),
)

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

// Unit types filtered to the currently selected faction. Drives the brush
// type picker; the edit panel uses its own per-unit faction lookup so it can
// switch the selected unit's type to one of a different faction.
const unitTypesForBrushFaction = computed(() =>
  unitDefsByFaction.value[placedUnitFaction.value] ?? [],
)

// All hostile-side units (raider + neutral) for the enemy-spawnpoint building's
// "Unit Type" picker. Enemy spawnpoints emit hostiles by design, so this list
// excludes the human faction — placing a barracks-style spawner that emits
// soldiers belongs in a different building, not enemy-spawnpoint.
const ENEMY_SPAWN_UNITS = computed(() => [
  ...unitDefsByFaction.value.raider,
  ...unitDefsByFaction.value.neutral,
])

function factionForUnitType(unitType: string): UnitFaction {
  for (const faction of ['raider', 'neutral', 'human'] as const) {
    if (unitDefsByFaction.value[faction].some((u) => u.type === unitType)) {
      return faction
    }
  }
  return 'raider'
}

const selectedEditBuilding = computed(() =>
  selectedEditBuildingId.value
    ? model.value.buildings.find((b) => b.id === selectedEditBuildingId.value) ?? null
    : null
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

const editPanelStyle = computed(() => {
  if (!editPanelPos.value) return {}
  return { left: `${editPanelPos.value.x}px`, top: `${editPanelPos.value.y}px` }
})

const placedUnitEditPanelStyle = computed(() => {
  if (!placedUnitEditPanelPos.value) return {}
  return { left: `${placedUnitEditPanelPos.value.x}px`, top: `${placedUnitEditPanelPos.value.y}px` }
})

const editWaveMode = computed<'gameStart' | 'always' | 'specific' | 'repeating'>(() => {
  const meta = selectedEditBuilding.value?.metadata
  if (!meta) return 'always'
  if (meta['gameStart'] === true) return 'gameStart'
  if ('waveNumber' in meta) return 'specific'
  if ('startingWave' in meta) return 'repeating'
  return 'always'
})

const editWaveNumber = computed(() => {
  const meta = selectedEditBuilding.value?.metadata
  if (!meta) return 1
  return (meta['waveNumber'] as number) ?? (meta['startingWave'] as number) ?? 1
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
  if (movingBuilding.value) return 'move'
  if (isPasteMode.value) return 'copy'
  if (!paintModeEnabled.value) return 'default'
  if (isControlHeld.value) return eraseCursor
  return 'crosshair'
}

function toggleSection(section: 'setup' | 'paint' | 'export') {
  openSection.value = openSection.value === section ? null : section
}

function addVictoryCondition() {
  const id = `obj-${nextVictoryConditionId++}`
  const conditions: VictoryCondition[] = [
    ...(model.value.victoryConditions ?? []),
    { id, type: 'killUnit', label: '', count: 1 },
  ]
  model.value = { ...model.value, victoryConditions: conditions }
}

function removeVictoryCondition(index: number) {
  const conditions = (model.value.victoryConditions ?? []).filter((_, i) => i !== index)
  model.value = { ...model.value, victoryConditions: conditions.length ? conditions : undefined }
}

function updateVictoryCondition(index: number, field: string, value: string | number) {
  const conditions = (model.value.victoryConditions ?? []).map((vc, i) =>
    i === index ? { ...vc, [field]: value } : vc,
  )
  model.value = { ...model.value, victoryConditions: conditions }
}

function setWaveConfig(field: 'totalWaves' | 'prepDuration' | 'waveDuration', value: number) {
  const current = model.value.waveConfig ?? {}
  const updated = { ...current, [field]: value }
  // Drop waveConfig entirely if all fields are zero/absent — keeps the export clean
  const hasAny = (updated.totalWaves ?? 0) > 0 || (updated.prepDuration ?? 0) > 0 || (updated.waveDuration ?? 0) > 0
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

function updateEditWaveMode(mode: 'gameStart' | 'always' | 'specific' | 'repeating', waveNum: number) {
  if (!selectedEditBuildingId.value) return
  model.value = {
    ...model.value,
    buildings: model.value.buildings.map((b) => {
      if (b.id !== selectedEditBuildingId.value) return b
      const meta = { ...(b.metadata ?? {}) }
      delete meta['gameStart']
      delete meta['waveNumber']
      delete meta['startingWave']
      if (mode === 'gameStart') meta['gameStart'] = true
      if (mode === 'specific') meta['waveNumber'] = waveNum
      if (mode === 'repeating') meta['startingWave'] = waveNum
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
    saveLabel.value = 'Saved!'
    await loadAvailableMaps()
    window.setTimeout(() => {
      saveLabel.value = 'Save to Server'
    }, 2000)
  } catch (err) {
    saveError.value = err instanceof Error ? err.message : 'Save failed'
    saveLabel.value = 'Save to Server'
  }
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
  const cell = getGridCellAtScreen(screenX, screenY)
  if (!cell) return

  const paintKey = `${cell.x}:${cell.y}:${activeBrushMode.value}:${brushSize.value}`
  if (paintKey === lastPaintKey) return
  lastPaintKey = paintKey

  // Buildings and units ignore brush size — placement is per-cell.
  if (activeBrushMode.value === 'building') {
    paintBuildingAt(cell.x, cell.y)
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
  } else if (selectedBuilding.value === 'enemy-spawnpoint') {
    metadata = {
      ...(enemyWaveMode.value === 'gameStart' ? { gameStart: true } : {}),
      ...(enemyWaveMode.value === 'specific' ? { waveNumber: enemyWaveNumber.value } : {}),
      ...(enemyWaveMode.value === 'repeating' ? { startingWave: enemyWaveNumber.value } : {}),
      ...(enemyWaveMode.value !== 'gameStart' ? { spawnDelaySeconds: enemySpawnDelay.value, spawnIntervalSeconds: enemySpawnInterval.value } : {}),
      ...(enemyWaveMode.value !== 'gameStart' && enemySpawnOnce.value ? { spawnOnce: true } : {}),
      ...(enemyIgnoreWaveClear.value ? { ignoreWaveClear: true } : {}),
      spawnCount: enemySpawnCount.value,
      unitType: enemyUnitType.value,
      ...(enemyObjectiveId.value ? { objectiveId: enemyObjectiveId.value } : {}),
      ...(enemyTargetPlayerLabel.value ? { targetPlayerLabel: enemyTargetPlayerLabel.value } : {}),
    }
  } else if (buildingObjectiveId.value) {
    metadata = { objectiveId: buildingObjectiveId.value }
  }

  model.value = setBuildingTile(model.value, cx, cy, selectedBuilding.value, metadata)
}

function placedUnitAt(x: number, y: number): PlacedUnit | undefined {
  return placedUnits.value.find((u) => u.x === x && u.y === y)
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

  if (isPasteMode.value && copiedBuilding.value) {
    const cell = getGridCellAtScreen(screen.x, screen.y)
    if (cell) pasteCopiedBuilding(cell.x, cell.y)
    return
  }

  if (!paintModeEnabled.value) {
    if (event.button === 0 && !isSpaceHeld) {
      const cell = getGridCellAtScreen(screen.x, screen.y)
      const hitUnit = cell ? placedUnitAt(cell.x, cell.y) : null
      if (hitUnit) {
        selectedEditPlacedUnitId.value = hitUnit.id
        selectedEditBuildingId.value = null
      } else {
        const hit = cell ? getBuildingAt(cell.x, cell.y) : null
        selectedEditBuildingId.value = hit?.id ?? null
        selectedEditPlacedUnitId.value = null
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

function onKeyDown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    selectedEditBuildingId.value = null
    selectedEditPlacedUnitId.value = null
    isPasteMode.value = false
    cancelMoveBuilding()
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
    drawGrid(ctx)
    drawMapBounds(ctx)
    drawPlacedUnits(ctx)
    drawSelectionHighlight(ctx)

    ctx.restore()

    updateEditPanelPos()
  }

  animationFrameId = requestAnimationFrame(render)
}

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
}

function drawSelectionHighlight(ctx: CanvasRenderingContext2D) {
  const b = selectedEditBuilding.value
  if (!b) return
  const cellSize = model.value.cellSize
  ctx.save()
  ctx.strokeStyle = '#60a5fa'
  ctx.lineWidth = 2 / camera.zoom
  ctx.setLineDash([])
  ctx.strokeRect(b.x * cellSize, b.y * cellSize, b.width * cellSize, b.height * cellSize)
  ctx.restore()
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
    const renderDef = def?.render
    const spriteRenderDef = def?.spriteRender
    const sprite = getBuildingSprite(building.buildingType)
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

function drawEditorEnemySpawnpoint(
  ctx: CanvasRenderingContext2D,
  building: { x: number; y: number; width: number; height: number },
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
  void fetchBuildingDefs().then(initBuildingDefs).catch(() => {})
  void fetchObstacleDefs().then(initObstacleDefs).catch(() => {})
  void fetchUnitDefs()
    .then(({ units, paths }) => {
      initPathBounds(paths)
      playerSpawnUnits.value = units
        .filter((def) => def.trainLabel)
        .map((def) => ({ type: def.type as UnitType, label: def.name }))
      // Bucket every catalog unit by its declared faction so the brush type
      // picker reflects the catalog automatically — adding a new unit JSON
      // (with a valid faction) makes it appear in the matching dropdown on
      // next editor load with zero hand-wiring.
      const grouped: Record<UnitFaction, Array<{ type: UnitType; label: string }>> = {
        raider: [],
        neutral: [],
        human: [],
      }
      for (const def of units) {
        const bucket = grouped[def.faction]
        if (bucket) bucket.push({ type: def.type as UnitType, label: def.name })
      }
      unitDefsByFaction.value = grouped
      // If the current type isn't in the current faction (e.g. catalog just
      // loaded), snap to the first valid one. Same for enemyUnitType.
      const factionTypes = grouped[placedUnitFaction.value].map((u) => u.type as string)
      if (!factionTypes.includes(placedUnitType.value)) {
        placedUnitType.value = grouped[placedUnitFaction.value][0]?.type ?? placedUnitType.value
      }
      const enemyPool = [...grouped.raider, ...grouped.neutral]
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

watch(movingBuilding, () => {
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
})
</script>

<style scoped>
.editor-shell {
  display: grid;
  grid-template-columns: minmax(210px, 250px) minmax(0, 1fr);
  grid-template-rows: minmax(0, 1fr);
  gap: 12px;
  align-items: stretch;
  width: 100%;
  height: 100%;
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

.victory-condition-row {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto auto auto;
  gap: 4px;
  align-items: center;
}

.victory-condition-row select,
.victory-condition-row input[type='text'],
.victory-condition-row input[type='number'] {
  min-width: 0;
}

.victory-condition-row button {
  border: 1px solid rgba(248, 113, 113, 0.35);
  border-radius: 6px;
  background: rgba(127, 29, 29, 0.45);
  color: #fca5a5;
  padding: 4px 7px;
  font-size: 0.72rem;
  cursor: pointer;
  white-space: nowrap;
}

.victory-condition-row button:hover {
  background: rgba(153, 27, 27, 0.65);
}

.vc-id-badge {
  font-size: 0.65rem;
  font-family: Consolas, 'Courier New', monospace;
  color: #d7bb84;
  background: rgba(58, 35, 18, 0.55);
  border: 1px solid rgba(215, 187, 132, 0.25);
  border-radius: 4px;
  padding: 2px 5px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 72px;
}

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
  cursor: pointer;
  font-size: 11px;
  padding: 2px 5px;
}

.edit-loadout-row button:disabled { opacity: 0.35; cursor: not-allowed; }

.edit-add-btn {
  background: rgba(59, 130, 246, 0.15);
  border: 1px solid rgba(59, 130, 246, 0.3);
  border-radius: 5px;
  color: #93c5fd;
  cursor: pointer;
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

@media (max-width: 1100px) {
  .editor-shell {
    grid-template-columns: minmax(200px, 230px) minmax(0, 1fr);
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
</style>
