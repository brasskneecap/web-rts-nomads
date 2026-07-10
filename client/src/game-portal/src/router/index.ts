import { createRouter, createWebHashHistory } from 'vue-router'
import MainMenu from '@/views/MainMenu.vue'
import Lobby from '@/views/Lobby.vue'
import Match from '@/views/Match.vue'
import MatchEnd from '@/views/MatchEnd.vue'
import Editor from '@/views/Editor.vue'
import ItemEditor from '@/views/ItemEditor.vue'
import ProfileView from '@/views/ProfileView.vue'
import OptionsView from '@/views/OptionsView.vue'
import WarRoom from '@/views/WarRoom.vue'
import KingdomView from '@/views/KingdomView.vue'
import BarracksView from '@/views/BarracksView.vue'
import ChapelView from '@/views/ChapelView.vue'
import FarmView from '@/views/FarmView.vue'
import MarketplaceView from '@/views/MarketplaceView.vue'
import BlacksmithView from '@/views/BlacksmithView.vue'

// /steam-mp removed as of §14R-E. Steam friend MP is now integrated into
// the Custom Game panel's Start Game tab (Steam lobby created in parallel
// with the local one) and its Find Game tab (friends' Steam lobbies merged
// into the lobby list). The previous standalone Steam Multiplayer view is
// gone — the paste-lobby-ID fallback that view offered no longer has a purpose.
//
// Custom Game and its sub-flows are no longer standalone routes: they live as
// tabs inside the war-room parchment panel (see WarRoom.vue / CustomGame.vue).
// The old paths redirect into /war-room with the matching `?tab=custom&sub=`
// query so existing deep-links and the leave-lobby flow still land correctly.
export const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', component: MainMenu, meta: { hideDominionPanel: true } },
    { path: '/custom', redirect: '/war-room?tab=custom' },
    { path: '/create-game', redirect: '/war-room?tab=custom&sub=start' },
    { path: '/direct-connect', redirect: '/war-room?tab=custom&sub=direct' },
    { path: '/lobby/:id', component: Lobby },
    { path: '/find-game', redirect: '/war-room?tab=custom&sub=find' },
    { path: '/starting', component: Match, meta: { hideMenuChrome: true, silenceMusic: true } },
    { path: '/match/:matchId', component: Match, meta: { hideMenuChrome: true, silenceMusic: true } },
    { path: '/match-end', component: MatchEnd, meta: { hideMenuChrome: true, silenceMusic: true } },
    { path: '/editor', component: Editor },
    { path: '/item-editor', component: ItemEditor },
    { path: '/profile', component: ProfileView },
    { path: '/options', component: OptionsView },
    { path: '/war-room', component: WarRoom, meta: { hideMenuChrome: true } },
    { path: '/kingdom', component: KingdomView, meta: { hideMenuChrome: true } },
    { path: '/kingdom/barracks', component: BarracksView, meta: { hideMenuChrome: true } },
    { path: '/kingdom/chapel', component: ChapelView, meta: { hideMenuChrome: true } },
    { path: '/kingdom/farm', component: FarmView, meta: { hideMenuChrome: true } },
    { path: '/kingdom/marketplace', component: MarketplaceView, meta: { hideMenuChrome: true } },
    { path: '/kingdom/blacksmith', component: BlacksmithView, meta: { hideMenuChrome: true } },
    { path: '/:catchAll(.*)', redirect: '/' },
  ],
})
