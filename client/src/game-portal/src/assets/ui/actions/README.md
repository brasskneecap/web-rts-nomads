# Action Icon Sprites

Drop a PNG named `<action-id>.png` here to override the default SVG icon for
a HUD action button.

- Filename = action `id` (case-insensitive). Example: `attack-move.png`,
  `set-spawn-point.png`, `stop.png`, `hold.png`.
- Single static frame. Square is preferred; the loader scales the longer axis
  to fit the 64px icon canvas.
- No pack step required — Vite picks them up at build time via
  `actionIconSprites.ts` and they take precedence over the SVG path defined
  in `actionIconDefs.ts`.
- For building/unit train actions, the iconDef on the action already drives a
  building/unit sprite render — those don't need a file here.
