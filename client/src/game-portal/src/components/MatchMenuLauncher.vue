<template>
  <div
    class="menu-launcher"
    role="toolbar"
    aria-label="Match actions"
    :style="{ '--ui-icon-container-image': `url(${iconContainerUrl})` }"
  >
    <button
      v-for="entry in ENTRIES"
      :key="entry.tabId"
      type="button"
      class="menu-launcher__btn"
      :class="[
        { 'menu-launcher__btn--active': activeTab === entry.tabId },
        entry.icon ? 'menu-launcher__btn--icon' : null,
      ]"
      :aria-pressed="activeTab === entry.tabId"
      :aria-label="`${entry.label} — ${entry.description} (hotkey ${entry.hotkey})`"
      @click="emit('open', entry.tabId)"
    >
      <img
        v-if="entry.icon"
        :src="entry.icon"
        :alt="entry.label"
        class="menu-launcher__icon"
        draggable="false"
      />
      <span v-else class="menu-launcher__label">{{ entry.label }}</span>
      <div class="menu-launcher__tooltip" role="tooltip">
        <div class="menu-launcher__tooltip-title">{{ entry.label }}</div>
        <div class="menu-launcher__tooltip-desc">{{ entry.description }}</div>
        <div class="menu-launcher__tooltip-hotkey">Hotkey: <kbd>{{ entry.hotkey }}</kbd></div>
      </div>
    </button>

    <!-- Items row toggle: sits to the right of the Vault button, using the
         same icon-container slot style as Shop/Upgrades/Vault. Not a menu
         tab — it shows/hides the ItemsBar (consumables row above the
         commander abilities). Pressed styling while the bar is visible. -->
    <button
      type="button"
      class="menu-launcher__btn menu-launcher__btn--icon"
      :class="{ 'menu-launcher__btn--active': itemsBarVisible }"
      :aria-pressed="itemsBarVisible"
      aria-label="Items — Show or hide your consumable items row. (hotkey I)"
      @click="emit('toggle-items')"
    >
      <img
        :src="itemBagIconUrl"
        alt="Items"
        class="menu-launcher__icon"
        draggable="false"
      />
      <div class="menu-launcher__tooltip" role="tooltip">
        <div class="menu-launcher__tooltip-title">Items</div>
        <div class="menu-launcher__tooltip-desc">Show or hide your consumable items row. Click an item, then click on your units to use it.</div>
        <div class="menu-launcher__tooltip-hotkey">Hotkey: <kbd>I</kbd></div>
      </div>
    </button>

    <!-- Centered group: commander abilities centered on the launcher /
         details-panel midline. Floats above the left-anchored
         Shop/Upgrades/Vault row. -->
    <div class="menu-launcher__center-group">
      <CommanderActionBar
        embedded
        :abilities="abilities"
        :active-ability-id="activeAbilityId"
        @cast="(id) => emit('cast-ability', id)"
      />
    </div>

    <!-- Settings sits at the far right edge of the launcher. margin-left:
         auto pushes it past the centered group (which is absolute, so it
         doesn't consume flex space) to the launcher's right edge. -->
    <button
      type="button"
      class="menu-launcher__btn menu-launcher__btn--icon menu-launcher__btn--settings"
      aria-label="Settings"
      @click="emit('settings')"
    >
      <img
        :src="settingsIconUrl"
        alt="Settings"
        class="menu-launcher__icon"
        draggable="false"
      />
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
import shopIconUrl from '@/assets/ui/buttons/shop.png'
import upgradesIconUrl from '@/assets/ui/buttons/upgrades.png'
import vaultIconUrl from '@/assets/ui/buttons/vault.png'
import itemBagIconUrl from '@/assets/ui/buttons/item_bag.png'
import settingsIconUrl from '@/assets/ui/buttons/settings.png'
import iconContainerUrl from '@/assets/ui/themes/default/icon-container.png'

interface LauncherEntry {
  tabId: string
  label: string
  hotkey: string
  description: string
  icon?: string
}

// Keep in sync with MATCH_MENU_HOTKEYS in Match.vue. Order here drives
// left-to-right button order.
const ENTRIES: LauncherEntry[] = [
  {
    tabId: 'shop',
    label: 'Shop',
    hotkey: 'S',
    description: 'Browse and purchase items from buildings you own.',
    icon: shopIconUrl,
  },
  {
    tabId: 'upgrades',
    label: 'Upgrades',
    hotkey: 'U',
    description: 'View and purchase permanent upgrades for your units.',
    icon: upgradesIconUrl,
  },
  {
    tabId: 'vault',
    label: 'Vault',
    hotkey: 'V',
    description: 'Manage stored items and equip them on your units.',
    icon: vaultIconUrl,
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
  /** Whether the consumable ItemsBar is currently shown — drives the Items
   *  button's pressed styling. */
  itemsBarVisible?: boolean
}>(), {
  abilities: () => [],
  activeAbilityId: null,
  itemsBarVisible: true,
})

const emit = defineEmits<{
  open: [tabId: string]
  'cast-ability': [abilityId: string]
  'toggle-items': []
  settings: []
}>()
</script>

<style scoped>
.menu-launcher {
  /* Horizontal strip sitting on top of SelectionHud's details panel,
     spanning the gap between the minimap (left) and actions (right) side
     panels. SelectionHud anatomy (post-25% scale): selection-main = 1350px
     centered → left edge at calc(50% - 675px). Inside it: minimap (275) |
     details (750) | actions (325). So the details panel spans
     [50% - 400px, 50% + 350px] — the launcher mirrors that.
     Vertical: bottom edge sits at the top of the details panel frame
     (SelectionHud height 250px minus --details-top-spacer 40px = 210px).
     Height = 80px so the embedded ability slots fit at full size. */
  position: absolute;
  bottom: 210px;
  left: calc(50% - 400px);
  width: 750px;
  height: 80px;
  z-index: 6;
  display: flex;
  flex-direction: row;
  align-items: flex-end;
  justify-content: flex-start;
  gap: 8px;
  /* Match the inter-button gap so the Shop button stands off the minimap
     (left) and Settings stands off the actions panel (right) by the same
     distance the inner buttons stand off each other. */
  padding-left: 8px;
  padding-right: 8px;
  box-sizing: border-box;
  pointer-events: auto;
  user-select: none;
}

/* While any launcher button (or embedded commander ability slot) is hovered,
   raise the whole launcher so its tooltip paints above every coexisting
   panel (ItemsBar, vault, MatchMenu, MatchHud). Inline tooltips cannot
   escape this element's stacking context, so the context itself moves up.
   Only overlap-safe while hovered: the pointer is over a launcher button,
   so nothing covered by the raise was clickable under the cursor anyway. */
.menu-launcher:has(button:hover) {
  z-index: var(--z-panel-raised, 300);
}

/* Pill button styled to match the "Exit Game" item in MatchHud's settings
   menu (.settings-item) — warm brown gradient + subtle gold border. */
.menu-launcher__btn {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  height: 45px;
  padding: 0 20px;
  border-radius: 8px;
  border: 1px solid rgba(200, 164, 106, 0.24);
  background: linear-gradient(180deg, rgba(113, 75, 39, 0.85), rgba(61, 39, 22, 0.95));
  color: #f5ead2;
  font: inherit;
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 0.04em;
  cursor: pointer;
  transition: border-color 0.12s ease;
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

/* Elevate the hovered/focused button so its tooltip sits above sibling action
   bar slots that come later in the DOM. (position: relative on the base rule
   makes this z-index effective; it no longer relies on a filter-induced
   stacking context.) */
.menu-launcher__btn:hover,
.menu-launcher__btn:focus-visible {
  z-index: 2;
}

.menu-launcher__label {
  color: #f5ead2;
}

/* Center group holds the commander ability bar. Absolute-positioned at the
   launcher's horizontal center with translateX(-50%) so the bar lands on
   the launcher midline — which aligns with the SelectionHud details panel
   midline. Out of the flex flow, so Settings (with margin-left: auto) can
   still travel all the way to the launcher's right edge. */
.menu-launcher__center-group {
  position: absolute;
  bottom: 0;
  left: 50%;
  transform: translateX(-50%);
  display: flex;
  align-items: flex-end;
  pointer-events: auto;
}

/* Pin the Settings button to the right edge of the launcher row. */
.menu-launcher__btn--settings {
  margin-left: auto;
}

/* Icon-mode buttons (Shop, Upgrades) use the shared icon-container frame
   as their background instead of the warm-brown pill, with the action
   artwork centered inside at 70% — same idiom as inventory/action slots
   in SelectionHud. 56px = the original 45px slot +25%, sized up so the
   launcher actions read better at 100% zoom. */
.menu-launcher__btn--icon,
.menu-launcher__btn--icon:hover,
.menu-launcher__btn--icon.menu-launcher__btn--active {
  width: 56px;
  height: 56px;
  padding: 0;
  border: 0;
  border-radius: 0;
  background: var(--ui-icon-container-image) center / 100% 100% no-repeat;
  image-rendering: pixelated;
}

/* Hover / active highlight uses the shared inset glow (--ui-hover-glow from
   style.css) instead of `filter: brightness()`, which churned a GPU layer
   over the pixelated icon-container PNG and caused flicker — the same fix
   applied to the shop/vault/upgrade slots and the SelectionHud action cells.
   The active-hover rule composes the glow on top of the active gold ring. */
.menu-launcher__btn--icon:hover {
  box-shadow: var(--ui-hover-glow);
}

.menu-launcher__btn--icon.menu-launcher__btn--active {
  box-shadow:
    inset 0 0 0 2px rgba(255, 226, 138, 0.7),
    0 0 12px rgba(255, 200, 80, 0.45);
}

.menu-launcher__btn--icon.menu-launcher__btn--active:hover {
  box-shadow:
    inset 0 0 0 2px rgba(255, 226, 138, 0.7),
    0 0 12px rgba(255, 200, 80, 0.45),
    var(--ui-hover-glow);
}

.menu-launcher__icon {
  width: 70%;
  height: 70%;
  object-fit: contain;
  image-rendering: pixelated;
  pointer-events: none;
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
