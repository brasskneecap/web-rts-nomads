import { createApp } from 'vue'
import './style.css'
import App from './App.vue'
import { router } from './router'
import { useProfile } from './composables/useProfile'
import { resolveCursor, onCursorChange } from './game/rendering/cursors'

const { initialize } = useProfile()
void initialize()

const applyBodyCursor = () => {
  // Set on <html> rather than <body> so transient body-level cursor writes
  // (e.g. InputManager's edge-pan chevron) can clear back to this default
  // via inheritance instead of falling through to the OS arrow.
  document.documentElement.style.cursor = resolveCursor('default', 'default')
}
const applyHoverCursorVar = () => {
  document.documentElement.style.setProperty(
    '--cursor-hover',
    resolveCursor('hover', 'pointer'),
  )
}
const applyDefaultCursorVar = () => {
  // Exposes the game default cursor as a CSS variable so style.css rules
  // can re-assert it on disabled buttons (where the browser's user-agent
  // stylesheet otherwise overrides the inherited cursor with the OS arrow).
  document.documentElement.style.setProperty(
    '--cursor-default',
    resolveCursor('default', 'default'),
  )
}
applyBodyCursor()
applyHoverCursorVar()
applyDefaultCursorVar()
onCursorChange((key) => {
  if (key === 'default') {
    applyBodyCursor()
    applyDefaultCursorVar()
  }
  if (key === 'hover') applyHoverCursorVar()
})

document.addEventListener('contextmenu', (event) => {
  const target = event.target as HTMLElement | null
  if (target?.closest('[data-allow-context]')) return
  event.preventDefault()
})

// Suppress the browser's DEFAULT drag behaviour (ghost-dragging UI images,
// text, and links — which makes the app "function as if a html element").
// Intentional drag-and-drop is the opposite case and must keep working:
//   - Any element explicitly marked `draggable="true"` is, by definition, a
//     deliberate drag source (vault item equip, and any future DnD). Allow it.
//   - `[data-allow-drag]` remains as a container-level escape hatch for the
//     rare case of wanting a *default*-draggable element (e.g. a bare <img>)
//     to drag without setting draggable on it directly.
// New drag-and-drop features need no change here as long as their drag source
// carries `draggable="true"` — which it must, to drag at all.
document.addEventListener('dragstart', (event) => {
  const target = event.target as HTMLElement | null
  if (target?.closest('[draggable="true"], [data-allow-drag]')) return
  event.preventDefault()
})

createApp(App).use(router).mount('#app')
