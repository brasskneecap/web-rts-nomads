# UI Element Sprites

Drop PNGs here to use as HUD/UI artwork (panels, frames, banners, etc.).

- Filename stem = lookup key (case-insensitive). Example: `action-panel.png`
  → `getUiSpriteUrl('action-panel')`.
- Static single-frame images — no pack step required, Vite picks them up at
  build time.
- For CSS backgrounds, prefer importing the URL directly in the component:
  `import actionPanelUrl from '@/assets/ui/action-panel.png'`, then bind it
  via `:style="{ backgroundImage: \`url(${actionPanelUrl})\` }"`.
- For dynamic lookup by key, use the helpers in
  `@/game/rendering/uiSprites` (`getUiSpriteUrl` / `getUiSpriteImage`).

## Convention

Use kebab-case filenames matching the component or HUD region they belong to:
`action-panel.png`, `selection-frame.png`, `resource-bar.png`.
