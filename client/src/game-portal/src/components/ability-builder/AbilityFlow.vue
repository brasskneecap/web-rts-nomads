<template>
  <div class="ab-flow" data-test="ability-flow">
    <template v-if="triggers.length">
      <FlowTriggerCard
        v-for="(t, i) in triggers"
        :key="t.id"
        :trigger="t"
        :index="i"
        :path="[{ kind: 'trigger', id: t.id }]"
      />
    </template>
    <p v-else class="ab-flow__empty" data-test="ability-flow-empty">
      No triggers yet — add one to define what this ability does.
    </p>

    <div class="ab-flow__add-trigger">
      <select v-model="newTriggerType" aria-label="New trigger type">
        <option v-for="t in triggerTypeOptions" :key="t" :value="t">{{ humanizeActionType(t) }}</option>
      </select>
      <UiButton size="sm" variant="secondary" data-test="add-trigger-button" @click="onAddTrigger">+ Trigger</UiButton>
    </div>
  </div>
</template>

<script setup lang="ts">
// The Flow view: the program's root triggers rendered as a vertical stack of
// trigger/action cards, plus an "Add trigger" affordance. Presentations
// (play_presentation's config.presentationId resolved against
// program.presentations) render inline under their referencing action card
// — see FlowActionCard.vue — rather than as a separate section here.
import { computed, ref, watch } from 'vue'
import type { TriggerType } from '@/game/abilities/program/abilityProgram'
import UiButton from '@/components/ui/UiButton.vue'
import { useAbilityBuilderContext } from './AbilityBuilderContext'
import FlowTriggerCard from './FlowTriggerCard.vue'
import { humanizeActionType } from './summarizeAction'

const builder = useAbilityBuilderContext()

const triggers = computed(() => builder.program.value.triggers)

// Curated fallback for when the schema hasn't loaded (or a server predating
// this enum) — the three most common trigger types an author reaches for.
const CURATED_TRIGGER_TYPES: TriggerType[] = ['on_cast_complete', 'on_tick', 'on_animation_marker']

const triggerTypeOptions = computed<TriggerType[]>(() => {
  const fromSchema = builder.schema.value?.enums.triggerTypes
  return fromSchema && fromSchema.length > 0 ? fromSchema : CURATED_TRIGGER_TYPES
})

const newTriggerType = ref<TriggerType>(triggerTypeOptions.value[0] ?? 'on_cast_complete')

// If the schema loads asynchronously after mount (or a subsequent selection
// changes the options), keep the picked value valid rather than silently
// submitting a stale/no-longer-offered type.
watch(triggerTypeOptions, (opts) => {
  if (!opts.includes(newTriggerType.value)) newTriggerType.value = opts[0] ?? 'on_cast_complete'
})

function onAddTrigger() {
  builder.addTrigger(newTriggerType.value)
}
</script>

<style scoped>
.ab-flow {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.ab-flow__empty {
  margin: 0;
  padding: 16px 12px;
  font-size: 0.84rem;
  color: var(--ed-text-dim);
  text-align: center;
  border: 1px dashed var(--ed-line);
  border-radius: var(--ed-radius);
}

.ab-flow__add-trigger {
  display: flex;
  align-items: center;
  gap: 8px;
  padding-top: 8px;
  border-top: 1px dashed var(--ed-line);
}

.ab-flow__add-trigger select {
  flex: 1 1 auto;
  min-width: 0;
}
</style>
