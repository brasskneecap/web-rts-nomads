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
applyBodyCursor()
applyHoverCursorVar()
onCursorChange((key) => {
  if (key === 'default') applyBodyCursor()
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
