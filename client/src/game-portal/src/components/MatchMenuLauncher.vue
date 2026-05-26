<template>
  <div
    class="menu-launcher"
    role="toolbar"
    aria-label="Match actions"
  >
    <button
      v-for="entry in ENTRIES"
      :key="entry.tabId"
      type="button"
      class="menu-launcher__btn"
      :class="{ 'menu-launcher__btn--active': activeTab === entry.tabId }"
      :aria-pressed="activeTab === entry.tabId"
      :aria-label="`${entry.label} — ${entry.description} (hotkey ${entry.hotkey})`"
      @click="emit('open', entry.tabId)"
    >
      <span class="menu-launcher__label">{{ entry.label }}</span>
      <div class="menu-launcher__tooltip" role="tooltip">
        <div class="menu-launcher__tooltip-title">{{ entry.label }}</div>
        <div class="menu-launcher__tooltip-desc">{{ entry.description }}</div>
        <div class="menu-launcher__tooltip-hotkey">Hotkey: <kbd>{{ entry.hotkey }}</kbd></div>
      </div>
    </button>

    <CommanderActionBar
      embedded
      :abilities="abilities"
      :active-ability-id="activeAbilityId"
      @cast="(id) => emit('cast-ability', id)"
    />

    <button
      type="button"
      class="menu-launcher__btn"
      aria-label="Settings"
      @click="emit('settings')"
    >
      <span class="menu-launcher__label">Settings</span>
      <div class="menu-launcher__tooltip" role="tooltip">
        <div class="menu-launcher__tooltip-title">Settings</div>
        <div class="menu-launcher__tooltip-desc">Open the match settings menu.</div>
      </div>
    </button>
  </div>
</template>

<script setup lang="ts">
import CommanderActionBar from '@/components/CommanderActionBar.vue'
import type { CommanderAbilitySnapshot } from '@/game/network/protocol'

interface LauncherEntry {
  tabId: string
  label: string
  hotkey: string
  description: string
}

// Keep in sync with MATCH_MENU_HOTKEYS in Match.vue. Order here drives
// left-to-right button order.
const ENTRIES: LauncherEntry[] = [
  {
    tabId: 'shop',
    label: 'Shop',
    hotkey: 'S',
    description: 'Browse and purchase items from buildings you own.',
  },
  {
    tabId: 'upgrades',
    label: 'Upgrades',
    hotkey: 'U',
    description: 'View and purchase permanent upgrades for your units.',
  },
  {
    tabId: 'vault',
    label: 'Vault',
    hotkey: 'V',
    description: 'Manage stored items and equip them on your units.',
  },
]

withDefaults(defineProps<{
  /** Tab id of the currently open MatchMenu tab, or null when the menu is
   *  closed. Drives the pressed/active styling on the matching button. */
  activeTab: string | null
  /** Commander abilities to render in the middle of the action row. */
  abilities?: CommanderAbilitySnapshot[]
  /** Id of the commander ability currently in targeting mode, or null. */
  activeAbilityId?: string | null
}>(), {
  abilities: () => [],
  activeAbilityId: null,
})

const emit = defineEmits<{
  open: [tabId: string]
  'cast-ability': [abilityId: string]
  settings: []
}>()
</script>

<style scoped>
.menu-launcher {
  /* Horizontal strip sitting on top of SelectionHud's details panel,
     spanning the gap between the minimap (left) and actions (right) side
     panels. SelectionHud anatomy: selection-main = 1080px centered →
     left edge at calc(50% - 540px). Inside it: minimap (220) | details
     (600) | actions (260). So the details panel spans
     [50% - 320px, 50% + 280px] — the launcher mirrors that.
     Vertical: bottom edge sits at the top of the details panel frame
     (SelectionHud height 200px minus --details-top-spacer 32px = 168px).
     Height = 64px so the embedded ability slots fit at full size. */
  position: absolute;
  bottom: 168px;
  left: calc(50% - 320px);
  width: 600px;
  height: 64px;
  z-index: 6;
  display: flex;
  flex-direction: row;
  align-items: flex-end;
  justify-content: flex-start;
  gap: 8px;
  /* Match the inter-button gap so the Shop button stands off the minimap
     by the same distance as the buttons stand off each other. */
  padding-left: 8px;
  box-sizing: border-box;
  pointer-events: auto;
  user-select: none;
}

/* Pill button styled to match the "Exit Game" item in MatchHud's settings
   menu (.settings-item) — warm brown gradient + subtle gold border. */
.menu-launcher__btn {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  height: 36px;
  padding: 0 16px;
  border-radius: 8px;
  border: 1px solid rgba(200, 164, 106, 0.24);
  background: linear-gradient(180deg, rgba(113, 75, 39, 0.85), rgba(61, 39, 22, 0.95));
  color: #f5ead2;
  font: inherit;
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 0.04em;
  cursor: pointer;
  transition: background 0.12s ease, border-color 0.12s ease, filter 0.12s ease;
}

.menu-launcher__btn:hover {
  background: linear-gradient(180deg, rgba(145, 96, 48, 0.95), rgba(83, 53, 28, 0.98));
  border-color: rgba(220, 180, 110, 0.5);
}

.menu-launcher__btn:active {
  filter: brightness(0.92);
}

.menu-launcher__btn--active {
  background: linear-gradient(180deg, rgba(160, 110, 56, 1), rgba(96, 62, 32, 1));
  border-color: rgba(247, 216, 142, 0.7);
}

.menu-launcher__btn:focus-visible {
  outline: 2px solid rgba(247, 216, 142, 0.9);
  outline-offset: 2px;
}

.menu-launcher__label {
  color: #f5ead2;
}

/* Hover tooltip — shares the action-tooltip language used elsewhere in the
   in-match UI (SelectionHud action cells, MatchMenu shop items). */
.menu-launcher__tooltip {
  position: absolute;
  bottom: calc(100% + 6px);
  left: 50%;
  transform: translateX(-50%);
  min-width: 180px;
  max-width: 260px;
  padding: 7px 10px;
  border-radius: 8px;
  background: linear-gradient(180deg, rgba(34, 22, 10, 0.98), rgba(20, 12, 4, 0.98));
  border: 1px solid rgba(200, 164, 106, 0.45);
  box-shadow: 0 6px 18px rgba(0, 0, 0, 0.5);
  color: #f5ead2;
  text-align: left;
  opacity: 0;
  visibility: hidden;
  transition: opacity 0.12s ease-out;
  pointer-events: none;
  z-index: 10;
}

.menu-launcher__btn:hover .menu-launcher__tooltip,
.menu-launcher__btn:focus-visible .menu-launcher__tooltip {
  opacity: 1;
  visibility: visible;
}

.menu-launcher__tooltip-title {
  font-size: 13px;
  font-weight: 700;
  color: #fff2d6;
  margin-bottom: 4px;
  line-height: 1.5;
  letter-spacing: 0.02em;
}

.menu-launcher__tooltip-desc {
  font-size: 11px;
  color: #d4b87a;
  line-height: 1.5;
}

.menu-launcher__tooltip-hotkey {
  margin-top: 5px;
  padding-top: 5px;
  border-top: 1px solid rgba(200, 164, 106, 0.22);
  font-size: 11px;
  color: #d4b87a;
  line-height: 1.5;
}

.menu-launcher__tooltip-hotkey kbd {
  display: inline-block;
  padding: 1px 6px;
  margin-left: 4px;
  border-radius: 4px;
  background: rgba(20, 12, 4, 0.6);
  border: 1px solid rgba(200, 164, 106, 0.35);
  color: #ffe9a0;
  font-family: inherit;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.04em;
}
</style>
