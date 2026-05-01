import { createRouter, createWebHashHistory } from 'vue-router'
import MainMenu from '@/views/MainMenu.vue'
import CustomGame from '@/views/CustomGame.vue'
import CreateGame from '@/views/CreateGame.vue'
import Lobby from '@/views/Lobby.vue'
import FindGame from '@/views/FindGame.vue'
import Match from '@/views/Match.vue'
import Editor from '@/views/Editor.vue'

export const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', component: MainMenu },
    { path: '/custom', component: CustomGame },
    { path: '/create-game', component: CreateGame },
    { path: '/lobby/:id', component: Lobby },
    { path: '/find-game', component: FindGame },
    { path: '/starting', component: Match },
    { path: '/match/:matchId', component: Match },
    { path: '/editor', component: Editor },
    { path: '/:catchAll(.*)', redirect: '/' },
  ],
})
