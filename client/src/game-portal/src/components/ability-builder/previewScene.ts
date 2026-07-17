// Where the ability-preview scene sits in world space.
//
// Lives in its own module (not in PreviewSceneControls.vue) because both that
// component and AbilityPreviewPanel.vue need it, and `<script setup>` cannot
// contain ES module exports.

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
// 5120x4096). PreviewSceneControls lays its units out around this origin and
// AbilityPreviewPanel spawns the caster on it — keep the two in step. The
// cast-range / AoE overlays derive from these same request coords, so they
// follow the offset for free.
export const PREVIEW_SCENE_ORIGIN = { x: 600, y: 500 }
