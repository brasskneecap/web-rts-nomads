<template>
  <!-- The item editor's PREVIEW DESCRIPTION. It shares its content with the
       in-game tooltip (buildItemTooltipLines is the single source of truth for
       what an item "says") but is deliberately its own presentation — it is not
       the tooltip component, and restyling it never touches the vault / shop /
       HUD.

       Parchment (world-inner-panel), so the card reads as an item's own scroll
       rather than another form panel. Text is therefore INK — near-black and
       oxblood. Gold/white belongs on the dark panels; on tan it washes out. -->
  <UiPanel variant="worldInner" :padding="0" repeat="stretch" class="ipc">
    <div class="ipc__inner">
      <div class="ipc__name">{{ def.displayName || 'Untitled' }}</div>
      <!-- The tier word is painted in its own rarity color. -->
      <div class="ipc__tier" :style="{ color: tierColor }">{{ capitalize(def.tier) }}</div>

      <hr class="ipc__rule" />

      <ul v-if="lines.length" class="ipc__lines">
        <li v-for="(line, i) in lines" :key="i">{{ line }}</li>
      </ul>
      <p v-else class="ipc__empty">No stats yet.</p>

      <hr class="ipc__rule" />

      <!-- The same coin the shop and craft cards use, so a price reads the same
           everywhere. The number carries the meaning; the coin carries "gold". -->
      <div class="ipc__cost">
        <span class="ipc__cost-label">Cost:</span>
        <img :src="goldIconUrl" class="ipc__coin" alt="gold" />
        {{ def.costGold || 0 }}
      </div>

      <!-- Only for a craftable item: its ingredients, as the icons the player
           would recognise in the shop, plus what the craft itself costs. -->
      <div v-if="craft" class="ipc__cost ipc__craft">
        <span class="ipc__cost-label">Crafted:</span>
        <span class="ipc__ingredients">
          <img
            v-for="(ing, i) in craft.inputs"
            :key="`${ing.def.id}-${i}`"
            :src="ing.iconUrl"
            :alt="ing.def.displayName"
            class="ipc__ingredient"
            @mouseenter="onIngredientEnter($event, ing.def)"
            @mouseleave="hoveredIngredient = null"
          />
        </span>
        <img :src="goldIconUrl" class="ipc__coin" alt="gold" />
        {{ craft.costGold }}
      </div>

      <!-- The craft cost above is per-craft; this is the one-off price of
           learning the recipe at a Recipe Shop. Two different purchases, so
           they get two lines rather than one ambiguous number. -->
      <div v-if="craft?.recipeCostGold !== undefined" class="ipc__cost ipc__craft">
        <span class="ipc__cost-label">Recipe:</span>
        <img :src="goldIconUrl" class="ipc__coin" alt="gold" />
        {{ craft.recipeCostGold }}
      </div>

      <!-- The REAL in-game item tooltip, so an ingredient reads here exactly as
           it does in the shop or the vault. It teleports to <body>, so nesting
           it here costs nothing and keeps this component single-rooted. -->
      <ItemHoverTooltip :item="hoveredIngredient" :anchor="ingredientAnchor" />
    </div>
  </UiPanel>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import UiPanel from '@/components/ui/UiPanel.vue'
import ItemHoverTooltip from '@/components/ItemHoverTooltip.vue'
import type { ItemTooltipData } from '@/components/ItemHoverTooltip.vue'
import goldIconUrl from '@/assets/resources/gold.png'
import type { ItemDef } from '@/game/maps/itemDefs'
import { TIER_COLORS, buildItemTooltipBody, buildItemTooltipLines } from '@/game/items/itemRules'

/** One ingredient of a craftable item: its def (for the hover tooltip) and the
 *  icon to draw. */
export type PreviewCraftInput = { def: ItemDef; iconUrl: string }

const props = defineProps<{
  /** The item as it would be served — for an unsaved draft, built by
   *  previewDefFromForm (which resolves procs against the effect catalog). */
  def: ItemDef
  /** The recipe, when the item is craftable. Ingredients do not live on ItemDef
   *  (they belong to the paired RecipeDef), so the editor resolves them and
   *  hands them in. Absent = not craftable, and the crafting lines are not shown.
   *  `costGold` is the craft price at the Artificer; `recipeCostGold` is what a
   *  Recipe Shop charges to learn it (absent for a starter recipe, which is
   *  never bought). */
  craft?: { costGold: number; recipeCostGold?: number; inputs: PreviewCraftInput[] }
}>()

const lines = computed(() => buildItemTooltipLines(props.def))
const tierColor = computed(() => TIER_COLORS[props.def.tier])

// Ingredient hover. The tooltip floats above everything (it teleports to
// <body>), so the parchment card can't clip it.
const hoveredIngredient = ref<ItemTooltipData | null>(null)
const ingredientAnchor = ref<DOMRect | null>(null)

function onIngredientEnter(event: MouseEvent, def: ItemDef) {
  ingredientAnchor.value = (event.currentTarget as HTMLElement).getBoundingClientRect()
  hoveredIngredient.value = {
    displayName: def.displayName,
    tier: def.tier,
    tierColor: TIER_COLORS[def.tier],
    body: buildItemTooltipBody(def),
  }
}

function capitalize(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1)
}
</script>

<style scoped>
.ipc {
  min-width: 0;
  /* The parchment fill is stretched, not tiled, so it must be smoothed —
     UiPanel's `image-rendering: pixelated` would turn the scaled-up texture
     into visible blocks. */
  image-rendering: auto;
}

/* Ink on parchment. The panel art is the background, so nothing here paints
   one; every color below is chosen for contrast against tan (~#b09a72), which
   is why none of them are the editor's gold/white — those are for dark panels
   and would wash out here. */
.ipc__inner {
  padding: 14px 16px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  color: #24160a; /* near-black ink: the body text has to carry the card */
}

.ipc__name {
  font-family: var(--font-title);
  font-size: 1.25rem;
  font-weight: 800;
  letter-spacing: 0.02em;
  color: #17100a; /* near-black — 6.7:1 on the parchment fill */
  line-height: 1.2;
}

/* The tier word carries the rarity color itself (color is bound inline from
   TIER_COLORS). Those colors are authored for dark backgrounds, so on parchment
   they get a black outline to pop — without it a pale tier like `common` sinks
   into the tan. The 8-direction text-shadow is the outline: -webkit-text-stroke
   would eat into the glyph and thin the letterforms instead of ringing them. */
.ipc__tier {
  font-family: var(--font-unit);
  font-size: 0.82rem;
  font-weight: 800;
  letter-spacing: 0.08em;
  text-shadow:
    -1px -1px 0 #000,
    1px -1px 0 #000,
    -1px 1px 0 #000,
    1px 1px 0 #000,
    0 -1px 0 #000,
    0 1px 0 #000,
    -1px 0 0 #000,
    1px 0 0 #000;
}

.ipc__rule {
  width: 100%;
  height: 1px;
  margin: 4px 0;
  border: 0;
  background: rgba(36, 22, 10, 0.28);
}

.ipc__lines {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.ipc__lines li {
  font-size: 0.84rem;
  font-weight: 600;
  line-height: 1.35;
}

.ipc__empty {
  margin: 0;
  font-size: 0.82rem;
  font-style: italic;
  color: rgba(36, 22, 10, 0.62);
}

.ipc__cost {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 0.95rem;
  font-weight: 800;
  color: #2f1e04; /* dark gold-brown — 4.9:1; reads as gold without going pale */
}

.ipc__craft {
  margin-top: 4px;
  flex-wrap: wrap;
}

.ipc__cost-label {
  color: #24160a; /* the label is ink; the number is the gold-brown */
}

.ipc__coin {
  width: 18px;
  height: 18px;
  flex: 0 0 auto;
  object-fit: contain;
  image-rendering: pixelated;
}

.ipc__ingredients {
  display: flex;
  align-items: center;
  gap: 3px;
}

/* Ingredient icons sit in a thin inked frame so they read as items rather than
   free-floating art on the parchment. */
.ipc__ingredient {
  width: 26px;
  height: 26px;
  object-fit: contain;
  image-rendering: pixelated;
  border: 1px solid rgba(36, 22, 10, 0.45);
  border-radius: 4px;
  background: rgba(36, 22, 10, 0.08);
}
</style>
