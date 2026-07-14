<template>
  <div class="perk-pool">
    <header class="perk-pool__header">
      <h2 class="perk-pool__title">{{ unit }} — {{ path }}</h2>
      <p class="perk-pool__honesty-note">
        New perks are inert until a developer wires their behavior in code.
      </p>
    </header>

    <section v-for="rank in RANK_ORDER" :key="rank" class="perk-pool__rank">
      <h3 class="perk-pool__rank-label">{{ rank }}</h3>

      <p v-if="poolFor(rank).length === 0" class="perk-pool__empty-hint">
        Empty pool — no perks authored for this rank yet.
      </p>
      <ul v-else class="perk-pool__list">
        <li v-for="(entry, idx) in poolFor(rank)" :key="entry.id" class="perk-pool__row">
          <div class="perk-pool__row-main">
            <span class="perk-pool__id">{{ entry.id }}</span>
            <span class="perk-pool__display-name">{{ entry.displayName || '—' }}</span>
            <span v-if="isWired(entry)" class="perk-pool__badge perk-pool__badge--wired">Wired</span>
            <span v-else class="perk-pool__badge perk-pool__badge--inert">Inert — no effect in match</span>
          </div>
          <div class="perk-pool__row-actions">
            <button
              type="button"
              :data-move-up-rank="rank"
              :disabled="idx === 0"
              @click="moveUp(rank, idx)"
            >▲</button>
            <button
              type="button"
              :data-move-down-rank="rank"
              :disabled="idx === poolFor(rank).length - 1"
              @click="moveDown(rank, idx)"
            >▼</button>
            <button type="button" :data-remove-rank="rank" @click="removePerk(rank, idx)">Remove</button>
          </div>
        </li>
      </ul>

      <div class="perk-pool__add">
        <input
          v-model="newPerkIdByRank[rank]"
          :data-add-rank="rank"
          list="perk-pool-catalog-ids"
          placeholder="perk id (existing or new)"
          @input="duplicateErrorByRank[rank] = ''"
        />
        <button type="button" :data-add-btn-rank="rank" @click="addPerk(rank)">+ Add</button>
        <p v-if="duplicateErrorByRank[rank]" class="perk-pool__error">{{ duplicateErrorByRank[rank] }}</p>
      </div>
    </section>

    <datalist id="perk-pool-catalog-ids">
      <option v-for="c in catalog" :key="c.id" :value="c.id" />
    </datalist>
  </div>
</template>

<script setup lang="ts">
import { reactive } from 'vue'
import type { PerkEntry } from '@/game/units/pathEditorApi'

const props = defineProps<{
  unit: string
  path: string
  // v-model: { bronze: PerkEntry[], silver: [...], gold: [...] }. A rank
  // absent from this record is treated as an empty pool (see poolFor).
  pools: Record<string, PerkEntry[]>
  // Full /catalog/perks — the source of truth for wired status and the
  // "add existing" datalist suggestions.
  catalog: PerkEntry[]
}>()

const emit = defineEmits<{ 'update:pools': [Record<string, PerkEntry[]>] }>()

const RANK_ORDER = ['bronze', 'silver', 'gold'] as const

function poolFor(rank: string): PerkEntry[] {
  return props.pools[rank] ?? []
}

// wired/inert is the single most important honesty signal this component
// surfaces (spec §7.3): a perk with no Go handler behind its id grants
// NOTHING in a match, no matter how thoroughly it's authored here. The
// catalog (fetched fresh from GET /catalog/perks) is the source of truth;
// an entry's own `wired` copy is only a fallback for a perk added in THIS
// editing session that hasn't round-tripped through the catalog yet. An id
// that resolves to neither is inert — most commonly a brand-new,
// never-handled id, which must read as inert immediately, not "unknown".
function isWired(entry: PerkEntry): boolean {
  const catalogMatch = props.catalog.find((c) => c.id === entry.id)
  return catalogMatch?.wired ?? entry.wired ?? false
}

const newPerkIdByRank = reactive<Record<string, string>>({ bronze: '', silver: '', gold: '' })
const duplicateErrorByRank = reactive<Record<string, string>>({ bronze: '', silver: '', gold: '' })

function emitUpdate(rank: string, list: PerkEntry[]) {
  emit('update:pools', { ...props.pools, [rank]: list })
}

// Adding a perk offers two paths, indistinguishable to this function: typing
// an id that matches a catalog entry copies its full authored shape
// (displayName, wired, config, …) so the row immediately reflects reality;
// typing a brand-new id creates a bare, explicitly-unwired entry — the
// inert badge shows the instant it's added, never a false "looks fine".
function addPerk(rank: string) {
  const id = (newPerkIdByRank[rank] ?? '').trim()
  if (!id) return
  const list = poolFor(rank)
  if (list.some((p) => p.id === id)) {
    duplicateErrorByRank[rank] = `"${id}" is already in this rank's pool.`
    return
  }
  const catalogMatch = props.catalog.find((c) => c.id === id)
  const entry: PerkEntry = catalogMatch ? { ...catalogMatch } : { id, wired: false }
  emitUpdate(rank, [...list, entry])
  newPerkIdByRank[rank] = ''
  duplicateErrorByRank[rank] = ''
}

function removePerk(rank: string, idx: number) {
  const list = [...poolFor(rank)]
  list.splice(idx, 1)
  emitUpdate(rank, list)
}

function moveUp(rank: string, idx: number) {
  if (idx <= 0) return
  const list = [...poolFor(rank)]
  ;[list[idx - 1], list[idx]] = [list[idx], list[idx - 1]]
  emitUpdate(rank, list)
}

function moveDown(rank: string, idx: number) {
  const list = [...poolFor(rank)]
  if (idx >= list.length - 1) return
  ;[list[idx + 1], list[idx]] = [list[idx], list[idx + 1]]
  emitUpdate(rank, list)
}

</script>

<style scoped>
.perk-pool {
  display: flex;
  flex-direction: column;
  gap: 10px;
  color: #f8fafc;
  font-size: 0.78rem;
}

.perk-pool__header {
  display: grid;
  gap: 4px;
}

.perk-pool__title {
  margin: 0;
  font-size: 0.9rem;
  text-transform: capitalize;
}

.perk-pool__honesty-note {
  margin: 0;
  color: rgba(226, 232, 240, 0.65);
  font-size: 0.72rem;
  font-style: italic;
}

.perk-pool__rank {
  border: 1px solid rgba(148, 163, 184, 0.18);
  border-radius: 12px;
  padding: 10px;
  background: rgba(8, 14, 24, 0.55);
  display: grid;
  gap: 8px;
}

.perk-pool__rank-label {
  margin: 0;
  text-transform: capitalize;
  font-size: 0.78rem;
  font-weight: 700;
  letter-spacing: 0.04em;
}

.perk-pool__empty-hint {
  margin: 0;
  color: rgba(226, 232, 240, 0.55);
  font-size: 0.74rem;
  font-style: italic;
}

.perk-pool__list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.perk-pool__row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  border: 1px solid rgba(148, 163, 184, 0.16);
  border-radius: 8px;
  padding: 6px 8px;
  background: rgba(15, 23, 42, 0.5);
}

.perk-pool__row-main {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  flex: 1 1 auto;
}

.perk-pool__id {
  font-weight: 700;
  font-family: monospace;
  white-space: nowrap;
}

.perk-pool__display-name {
  flex: 1 1 auto;
  min-width: 0;
  color: rgba(226, 232, 240, 0.86);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.perk-pool__badge {
  flex: 0 0 auto;
  border-radius: 999px;
  padding: 2px 9px;
  font-size: 0.68rem;
  font-weight: 700;
  white-space: nowrap;
}

.perk-pool__badge--wired {
  background: rgba(34, 197, 94, 0.16);
  color: #86efac;
  border: 1px solid rgba(34, 197, 94, 0.4);
}

.perk-pool__badge--inert {
  background: rgba(248, 113, 113, 0.18);
  color: #fca5a5;
  border: 1px solid rgba(248, 113, 113, 0.55);
}

.perk-pool__row-actions {
  display: flex;
  gap: 4px;
  flex: 0 0 auto;
}

.perk-pool__row-actions button {
  border: 1px solid rgba(148, 163, 184, 0.25);
  border-radius: 6px;
  background: rgba(15, 23, 42, 0.6);
  color: #f8fafc;
  padding: 3px 7px;
  font-size: 0.7rem;
}

.perk-pool__add {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.perk-pool__add input {
  flex: 1 1 160px;
  min-width: 0;
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 6px 9px;
  font-size: 0.76rem;
}

.perk-pool__add button {
  border: 1px solid rgba(215, 187, 132, 0.5);
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.6);
  color: #d7bb84;
  padding: 6px 10px;
  font-size: 0.76rem;
  font-weight: 700;
}

.perk-pool__error {
  flex-basis: 100%;
  margin: 0;
  color: #fca5a5;
  font-size: 0.72rem;
}
</style>
