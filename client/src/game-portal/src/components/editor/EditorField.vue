<template>
  <div class="ed-field" :class="{ 'ed-field--inline': inline }">
    <label v-if="label" :for="forId" class="ed-field__label">
      {{ label }}
      <span v-if="hint" class="ed-field__hint">{{ hint }}</span>
      <!-- Optional slot for a per-field InfoTip (or similar small
           label-adjacent control) — kept separate from the plain-text `hint`
           prop above rather than overloading it, since an InfoTip is a
           component instance (with its own click/hover state), not a string. -->
      <span v-if="$slots['label-extra']" class="ed-field__label-extra">
        <slot name="label-extra" />
      </span>
    </label>
    <!-- The control itself is a plain <input>/<select>/<textarea> supplied by
         the caller; editor-controls.css styles anything inside .ed-field, so
         every editor gets identical form chrome without a wrapper component
         per control type. -->
    <slot />
  </div>
</template>

<script setup lang="ts">
defineProps<{
  /** Field label. Omit for a control that labels itself (e.g. a checkbox). */
  label?: string
  /** Small parenthetical after the label ("(0–99)", "(blank = inherit)"). */
  hint?: string
  /** id of the control this label points at. */
  forId?: string
  /** Lay label and control side by side instead of stacked. */
  inline?: boolean
}>()
</script>

<style scoped>
.ed-field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

.ed-field--inline {
  flex-direction: row;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.ed-field__label {
  font-family: var(--font-body);
  font-size: 0.72rem;
  font-weight: 600;
  letter-spacing: 0.04em;
  color: var(--ed-text-dim);
  text-transform: uppercase;
}

.ed-field__hint {
  margin-left: 4px;
  font-size: 0.68rem;
  font-weight: 400;
  letter-spacing: 0;
  text-transform: none;
  color: var(--ed-text-dim);
  opacity: 0.75;
}

.ed-field__label-extra {
  display: inline-flex;
  align-items: center;
  margin-left: 5px;
  vertical-align: middle;
}
</style>
