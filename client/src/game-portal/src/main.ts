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

document.addEventListener('dragstart', (event) => {
  const target = event.target as HTMLElement | null
  if (target?.closest('[data-allow-drag]')) return
  event.preventDefault()
})

createApp(App).use(router).mount('#app')
