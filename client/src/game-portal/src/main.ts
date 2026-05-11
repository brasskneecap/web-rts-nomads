import { createApp } from 'vue'
import './style.css'
import App from './App.vue'
import { router } from './router'
import { useProfile } from './composables/useProfile'

const { initialize } = useProfile()
void initialize()

createApp(App).use(router).mount('#app')
