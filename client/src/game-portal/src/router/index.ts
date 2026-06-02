import { createRouter, createWebHashHistory } from 'vue-router'
import MainMenu from '@/views/MainMenu.vue'
import CustomGame from '@/views/CustomGame.vue'
import CreateGame from '@/views/CreateGame.vue'
import DirectConnect from '@/views/DirectConnect.vue'
import Lobby from '@/views/Lobby.vue'
import FindGame from '@/views/FindGame.vue'
import Match from '@/views/Match.vue'
import Editor from '@/views/Editor.vue'
import ProfileView from '@/views/ProfileView.vue'
import OptionsView from '@/views/OptionsView.vue'
import WarRoom from '@/views/WarRoom.vue'

// /steam-mp removed as of §14R-E. Steam friend MP is now integrated into
// /create-game (Steam lobby created in parallel with the local one) and
// /find-game (friends' Steam lobbies merged into the lobby list). The
// previous standalone Steam Multiplayer view is gone — the paste-lobby-ID
// fallback that view offered no longer has a purpose.
export const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', component: MainMenu },
    { path: '/custom', component: CustomGame },
    { path: '/create-game', component: CreateGame },
    { path: '/direct-connect', component: DirectConnect },
    { path: '/lobby/:id', component: Lobby },
    { path: '/find-game', component: FindGame },
    { path: '/starting', component: Match, meta: { hideMenuChrome: true } },
    { path: '/match/:matchId', component: Match, meta: { hideMenuChrome: true } },
    { path: '/editor', component: Editor },
    { path: '/profile', component: ProfileView },
    { path: '/options', component: OptionsView },
    { path: '/war-room', component: WarRoom, meta: { hideMenuChrome: true } },
    { path: '/:catchAll(.*)', redirect: '/' },
  ],
})
