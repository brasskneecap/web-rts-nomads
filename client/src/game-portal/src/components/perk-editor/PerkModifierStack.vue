<template>
  <div class="pm-stack">
    <p v-if="visibleModifiers.length === 0" class="pm-stack__empty">
      No modifiers yet. Add one below to describe what this perk does.
    </p>

    <PerkModifierCard
      v-for="entry in visibleModifiers"
      :key="entry.id"
      :entry="entry"
      :selected="isSelected(entry)"
      @select="builder.selectModifier({ arrayKey: entry.arrayKey, index: entry.index })"
      @duplicate="builder.duplicateModifier({ arrayKey: entry.arrayKey, index: entry.index })"
      @delete="builder.removeModifier({ arrayKey: entry.arrayKey, index: entry.index })"
    />

    <div class="pm-stack__add">
      <div class="pm-stack__add-menu">
        <button type="button" class="pm-stack__add-btn" data-test="add-modifier" @click="menuOpen = !menuOpen">
          + Add Modifier
        </button>
        <ul v-if="menuOpen" class="pm-stack__menu" role="menu">
          <li v-for="k in ADDABLE" :key="k">
            <button
              type="button"
              role="menuitem"
              :style="{ '--mk-accent': KIND_META[k].accent }"
              @click="pick(k)"
            >
              <span class="pm-stack__menu-dot" aria-hidden="true" />
              {{ KIND_META[k].label }}
              <span v-if="!KIND_META[k].editable" class="pm-stack__menu-soon">classic</span>
            </button>
          </li>
        </ul>
      </div>

      <div class="pm-stack__quick">
        <button v-for="k in QUICK" :key="k" type="button" :data-test="`quick-add-${k}`" @click="pick(k)">
          + {{ SHORT[k] }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import PerkModifierCard from './PerkModifierCard.vue'
import { usePerkBuilderContext } from './PerkBuilderContext'
import { KIND_META, KIND_ORDER, type ModifierEntry, type ModifierKind } from './perkModifierModel'

const builder = usePerkBuilderContext()
const menuOpen = ref(false)

// Config is edited in the Setup column — hide it here so the stack has one
// clear editing surface per modifier.
const HIDDEN: ModifierKind[] = ['configValue']

const visibleModifiers = computed(() => builder.modifiers.value.filter((e) => !HIDDEN.includes(e.kind)))

// Full add-menu (every visible kind, in render order); un-migrated kinds are
// marked "classic" and route the user to the classic editor when picked.
const ADDABLE = KIND_ORDER.filter((k) => !HIDDEN.includes(k))
// Quick-add shows only the kinds the inspector can fully edit this slice.
const QUICK: ModifierKind[] = ADDABLE.filter((k) => KIND_META[k].editable)
const SHORT: Record<ModifierKind, string> = {
  unitStat: 'Unit Stat', abilityStat: 'Ability Stat', abilityField: 'Ability Field',
  abilityModifier: 'Ability Mod', abilityRider: 'Rider', aura: 'Aura',
  grantAbility: 'Grant', perkModifier: 'Perk Mod', configValue: 'Config', effect: 'Effect',
}

function isSelected(e: ModifierEntry): boolean {
  const s = builder.selected.value
  return !!s && s.arrayKey === e.arrayKey && s.index === e.index
}

function pick(kind: ModifierKind) {
  menuOpen.value = false
  if (!KIND_META[kind].editable) {
    builder.saveError.value = `${KIND_META[kind].label} isn't editable in the new builder yet — use the Classic editor.`
    return
  }
  builder.addModifier(kind)
}
</script>

<style scoped>
.pm-stack { display: flex; flex-direction: column; gap: 8px; min-width: 0; }
.pm-stack__empty { margin: 4px 2px; font-size: 0.82rem; color: var(--ed-text-dim); }
.pm-stack__add { margin-top: 4px; display: flex; flex-direction: column; gap: 8px; }
.pm-stack__add-menu { position: relative; }
.pm-stack__add-btn {
  width: 100%;
  padding: 8px;
  border: 1px dashed var(--ed-line-strong);
  border-radius: var(--ed-radius);
  background: rgba(212, 168, 71, 0.06);
  color: var(--ed-brass);
  font-weight: 700;
  font-size: 0.8rem;
}
.pm-stack__menu {
  position: absolute; z-index: 5; left: 0; right: 0; top: calc(100% + 4px);
  margin: 0; padding: 4px; list-style: none;
  background: var(--ed-sticky-bg);
  border: 1px solid var(--ed-line-strong);
  border-radius: var(--ed-radius);
  box-shadow: 0 8px 20px rgba(0, 0, 0, 0.5);
}
.pm-stack__menu button {
  display: flex; align-items: center; gap: 8px; width: 100%;
  padding: 6px 8px; background: none; border: 0; border-radius: 4px;
  color: var(--ed-text); font-size: 0.8rem; text-align: left;
}
.pm-stack__menu button:hover { background: rgba(212, 168, 71, 0.1); }
.pm-stack__menu-dot { width: 8px; height: 8px; border-radius: 2px; background: var(--mk-accent); flex: 0 0 auto; }
.pm-stack__menu-soon { margin-left: auto; font-size: 0.62rem; letter-spacing: 0.08em; text-transform: uppercase; color: var(--ed-text-dim); }
.pm-stack__quick { display: flex; flex-wrap: wrap; gap: 6px; }
.pm-stack__quick button {
  padding: 4px 8px; font-size: 0.72rem;
  border: 1px solid var(--ed-line); border-radius: 4px;
  background: var(--ed-field); color: var(--ed-text-dim);
}
.pm-stack__quick button:hover { color: var(--ed-brass); border-color: var(--ed-line-strong); }
</style>
