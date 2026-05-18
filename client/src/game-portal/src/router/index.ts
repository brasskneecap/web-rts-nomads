import { createRouter, createWebHashHistory } from 'vue-router'
import MainMenu from '@/views/MainMenu.vue'
import CustomGame from '@/views/CustomGame.vue'
import CreateGame from '@/views/CreateGame.vue'
import DirectConnect from '@/views/DirectConnect.vue'
import SteamMultiplayer from '@/views/SteamMultiplayer.vue'
import Lobby from '@/views/Lobby.vue'
import FindGame from '@/views/FindGame.vue'
import Match from '@/views/Match.vue'
import Editor from '@/views/Editor.vue'
import ProfileView from '@/views/ProfileView.vue'

export const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', component: MainMenu },
    { path: '/custom', component: CustomGame },
    { path: '/create-game', component: CreateGame },
    { path: '/direct-connect', component: DirectConnect },
    { path: '/steam-mp', component: SteamMultiplayer },
    { path: '/lobby/:id', component: Lobby },
    { path: '/find-game', component: FindGame },
    { path: '/starting', component: Match, meta: { hideMenuChrome: true } },
    { path: '/match/:matchId', component: Match, meta: { hideMenuChrome: true } },
    { path: '/editor', component: Editor },
    { path: '/profile', component: ProfileView },
    { path: '/:catchAll(.*)', redirect: '/' },
  ],
})
