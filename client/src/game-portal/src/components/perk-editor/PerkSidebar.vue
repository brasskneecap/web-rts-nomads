<template>
  <UiPanel variant="warRoomInner" :padding="0" class="pk-side">
    <div class="pk-side__inner">
      <div class="pk-side__actions">
        <UiButton size="sm" variant="active" data-test="perk-new" @click="$emit('new')">+ New Perk</UiButton>
        <input v-model="search" type="search" placeholder="Search perks…" aria-label="Search perks" />
      </div>
      <GameScrollArea class="pk-side__scroll">
        <p v-if="loadError" class="pk-side__error">{{ loadError }}</p>
        <div v-for="group in groups" :key="group.unit" class="pk-side__group">
          <button type="button" class="pk-side__unit" :aria-expanded="expanded.has(group.unit)" @click="toggle(group.unit)">
            <span class="pk-side__chev">{{ expanded.has(group.unit) ? '▾' : '▸' }}</span>
            {{ unitLabel(group.unit) }}
          </button>
          <template v-if="expanded.has(group.unit)">
            <div v-for="pg in group.paths" :key="pg.path" class="pk-side__path">
              <button v-if="pg.path" type="button" class="pk-side__path-label" :aria-expanded="expanded.has(group.unit + '/' + pg.path)" @click="toggle(group.unit + '/' + pg.path)">
                <span class="pk-side__chev">{{ expanded.has(group.unit + '/' + pg.path) ? '▾' : '▸' }}</span>
                {{ pg.path }}
              </button>
              <ul v-if="!pg.path || expanded.has(group.unit + '/' + pg.path)">
                <li v-for="p in pg.perks" :key="p.id">
                  <button type="button" data-test="perk-row" :class="{ 'is-on': p.id === selectedId }" :title="p.id" @click="$emit('select', p.id)">
                    {{ p.displayName || p.id }}
                    <span v-if="!p.wired" class="pk-side__inert">inert</span>
                  </button>
                </li>
              </ul>
            </div>
          </template>
        </div>
      </GameScrollArea>
    </div>
  </UiPanel>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import UiPanel from '@/components/ui/UiPanel.vue'
import UiButton from '@/components/ui/UiButton.vue'
import GameScrollArea from '@/components/ui/GameScrollArea.vue'
import type { AuthoredPerkDef } from '@/game/perks/perkEditorForm'

const props = defineProps<{
  perks: AuthoredPerkDef[]
  pathsByUnit: Record<string, string[]>
  selectedId: string | null
  loadError: string
}>()
defineEmits<{ select: [string]; new: [] }>()

const search = ref('')
const expanded = ref(new Set<string>())
function toggle(key: string) {
  const s = new Set(expanded.value)
  s.has(key) ? s.delete(key) : s.add(key)
  expanded.value = s
}
function unitLabel(u: string): string { return u && u !== 'Generic' ? u[0].toUpperCase() + u.slice(1) : u }

const pathToUnit = computed(() => {
  const m = new Map<string, string>()
  for (const [u, ps] of Object.entries(props.pathsByUnit)) for (const p of ps) m.set(p, u)
  return m
})

interface Grp { unit: string; paths: Array<{ path: string; perks: AuthoredPerkDef[] }> }
const groups = computed<Grp[]>(() => {
  const q = search.value.trim().toLowerCase()
  const match = (p: AuthoredPerkDef) => !q || p.id.toLowerCase().includes(q) || (p.displayName ?? '').toLowerCase().includes(q)
  const byUnitPath = new Map<string, Map<string, AuthoredPerkDef[]>>()
  const generic: AuthoredPerkDef[] = []
  for (const p of props.perks) {
    if (!match(p)) continue
    const path = p.path ?? ''
    const unit = path ? pathToUnit.value.get(path) : undefined
    if (!path || !unit) { generic.push(p); continue }
    if (!byUnitPath.has(unit)) byUnitPath.set(unit, new Map())
    const paths = byUnitPath.get(unit)!
    if (!paths.has(path)) paths.set(path, [])
    paths.get(path)!.push(p)
  }
  const out: Grp[] = [...byUnitPath.entries()].sort((a, b) => a[0].localeCompare(b[0])).map(([unit, paths]) => ({
    unit,
    paths: [...paths.entries()].sort((a, b) => a[0].localeCompare(b[0])).map(([path, ps]) => ({ path, perks: [...ps].sort((x, y) => x.id.localeCompare(y.id)) })),
  }))
  if (generic.length) out.push({ unit: 'Generic', paths: [{ path: '', perks: [...generic].sort((x, y) => x.id.localeCompare(y.id)) }] })
  return out
})
</script>

<style scoped>
.pk-side { height: 100%; min-height: 0; }
.pk-side__inner { height: 100%; min-height: 0; display: flex; flex-direction: column; gap: 8px; padding: 10px; box-sizing: border-box; }
.pk-side__actions { display: flex; flex-direction: column; gap: 6px; }
.pk-side__scroll { flex: 1 1 auto; min-height: 0; }
.pk-side__error { font-size: 0.78rem; color: var(--ed-danger); }
.pk-side__group { display: flex; flex-direction: column; gap: 2px; }
.pk-side__unit { display: flex; align-items: center; gap: 6px; width: 100%; margin-top: 8px; padding: 3px 4px; background: none; border: 0; text-align: left; font-size: 0.82rem; font-weight: 700; color: var(--ed-brass); }
.pk-side__path { padding-left: 8px; }
.pk-side__path-label { display: flex; align-items: center; gap: 6px; width: 100%; padding: 2px 4px; background: none; border: 0; text-align: left; font-size: 0.76rem; color: var(--ed-text-dim); }
.pk-side__chev { flex: 0 0 auto; font-size: 0.66rem; }
.pk-side ul { list-style: none; margin: 0; padding: 0 0 0 10px; display: flex; flex-direction: column; gap: 3px; }
.pk-side li button { width: 100%; border: 1px solid transparent; border-radius: 6px; background: var(--ed-field); color: var(--ed-text); padding: 6px 8px; font-size: 0.76rem; text-align: left; }
.pk-side li button.is-on { border-color: var(--ed-line-strong); box-shadow: inset 2px 0 0 var(--ed-brass); }
.pk-side__inert { margin-left: 4px; font-size: 0.58rem; letter-spacing: 0.06em; text-transform: uppercase; color: var(--ed-danger); }
</style>
