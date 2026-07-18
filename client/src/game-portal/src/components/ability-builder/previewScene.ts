// Where the ability-preview scene sits in world space, plus the shared
// default scene layout (starting positions/HP for a freshly-added scene
// unit) that both AbilityPreviewPanel's editable scene state and
// PreviewSceneControls' count inputs key off. Kept in one module (not
// `PreviewSceneControls.vue`) because the panel needs it too, and
// `<script setup>` cannot export runtime bindings for another SFC to import.

// PREVIEW_SCENE_ORIGIN places the whole preview scene inside the map's
// terrain. The preview replays onto a real catalog map (ability_preview.go
// runs GetMapConfigByID(DefaultMapID())), whose terrain spans
// [0,width] x [0,height] from the top-left corner — it is NOT centered on the
// world origin. The scene layout is authored RELATIVE to the caster (allies at
// negative X, enemies at positive X), so with the caster at (0,0) the allies
// land off-map entirely and the dummies render over a black void with no
// ground under them.
//
// (600, 500) clears the negative-X allies on even the smallest catalog map
// (battletest, 1536x1152) while staying well inside the default one (forest-1,
// 5120x4096). AbilityPreviewPanel spawns the caster here by default (now
// user-draggable — see AbilityPreviewPanel.vue's `casterPos`), and a fresh
// scene unit's default position is authored around this same origin. The
// cast-range / AoE overlays derive from the live request coords, so they
// follow the offset for free.
export const PREVIEW_SCENE_ORIGIN = { x: 600, y: 500 }

// Authored RELATIVE to the caster (allies at negative X, enemies at positive
// X) — see the module doc comment above for why the whole layout is then
// shifted onto PREVIEW_SCENE_ORIGIN.
export const ENEMY_START_X = PREVIEW_SCENE_ORIGIN.x + 120
export const ENEMY_STEP_X = 40
export const ALLY_START_X = PREVIEW_SCENE_ORIGIN.x - 80
export const ALLY_STEP_X = -40
export const SCENE_Y = PREVIEW_SCENE_ORIGIN.y

export const DEFAULT_ENEMY_HP = 200
export const DEFAULT_ENEMY_MAX_HP = 200
// Pre-damaged so a heal/buff ability's effect is visible without the user
// tweaking anything first.
export const DEFAULT_ALLY_HP = 40
export const DEFAULT_ALLY_MAX_HP = 100

// defaultEnemyPosition/defaultAllyPosition: where the Nth (0-indexed) enemy
// or ally spawns BEFORE the user drags it anywhere — used both for the
// initial default scene and for a newly-appended unit when the user raises
// enemyCount/allyCount in PreviewSceneControls (AbilityPreviewPanel.vue's
// reconcileSceneUnitCounts).
export function defaultEnemyPosition(index: number): { x: number; y: number } {
  return { x: ENEMY_START_X + index * ENEMY_STEP_X, y: SCENE_Y }
}

export function defaultAllyPosition(index: number): { x: number; y: number } {
  return { x: ALLY_START_X + index * ALLY_STEP_X, y: SCENE_Y }
}
