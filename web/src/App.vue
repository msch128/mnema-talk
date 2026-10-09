<script setup lang="ts">
import { startDesktopGaming } from './lib/desktopGaming'
import type { Route } from './lib/router'
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useAuthStore } from './stores/auth'
import { useChatStore } from './stores/chat'
import { useVoiceStore } from './stores/voice'
import { currentRoute, navigate, popRedirectRoute, savePendingRoute, resolveRoute, canGoBackInApp } from './lib/router'
import { useToastStore } from './stores/toast'
import { t } from './i18n'
import Sidebar from './components/Sidebar.vue'
import UserBar from './components/UserBar.vue'
import ChatArea from './components/ChatArea.vue'
import NativeTrustControl from './components/NativeTrustControl.vue'
import { isDesktopRuntime } from './lib/desktopRuntime'
import VoiceStage from './components/VoiceStage.vue'
import VoiceChatPanel from './components/VoiceChatPanel.vue'
import MemberList from './components/MemberList.vue'
import ThreadSidebar from './components/ThreadSidebar.vue'
import LoginModal from './components/LoginModal.vue'
import AdminDashboard from './components/AdminDashboard.vue'
import ConnectionStatsModal from './components/ConnectionStatsModal.vue'
import AudioSettingsModal from './components/AudioSettingsModal.vue'
import ScreenShareModal from './components/ScreenShareModal.vue'
import UserProfileModal from './components/UserProfileModal.vue'
import LegalModal from './components/LegalModal.vue'
import ResizeHandle from './components/ResizeHandle.vue'
import ToastHost from './components/ToastHost.vue'
import ConfirmDialog from './components/ConfirmDialog.vue'
import ConnectionBanner from './components/ConnectionBanner.vue'
import UpdateBanner from './components/UpdateBanner.vue'
import PipHost from './components/PipHost.vue'
import { useResizable } from './composables/useResizable'
import { useWebRTC } from './composables/useWebRTC'
import {
  loadVoiceChatOpen, saveVoiceChatOpen, voiceChatOpenFor, VOICE_CHAT_HEIGHT, STAGE_MIN_HEIGHT
} from './lib/voiceChatPanel'

const authStore = useAuthStore()
const chatStore = useChatStore()
const voiceStore = useVoiceStore()
const toasts = useToastStore()
let alive = true
let authRetryTimer: ReturnType<typeof setTimeout> | undefined
let cancelAuthRetry: (() => void) | undefined
onBeforeUnmount(() => {
  alive = false
  ++routeGen
  clearTimeout(authRetryTimer)
  cancelAuthRetry?.()
  cancelAuthRetry = undefined
})
const { resumeVoiceSession, resumeRemoteAudio, leaveVoiceChannel, rejoinAfterReconnect } = useWebRTC()

// Column widths. Order = shrink priority on narrow windows
// (thread first, then member list, then the left sidebar).
const panels = useResizable([
  { name: 'left', side: 'left', defaultWidth: 240, min: 200, max: 360 },
  { name: 'members', side: 'right', defaultWidth: 240, min: 200, max: 360, visible: () => chatStore.showMemberList },
  { name: 'thread', side: 'right', defaultWidth: 400, min: 320, max: 640, visible: () => !!chatStore.activeThread }
], { centerMin: 400 })
// Height of the Talk's chat under the stage; the stage (and its 48px header)
// keeps the rest, at least STAGE_MIN_HEIGHT.
const voiceChatHeight = useResizable([VOICE_CHAT_HEIGHT], { axis: 'y', centerMin: STAGE_MIN_HEIGHT + 48 }).voiceChat
const showAdminModal = ref(false)
const showLegalModal = ref(false)
const adminTab = ref('users')
const previewVoiceChannelId = ref<string | null>(null)
// /v/:id is the Talk on its own, /v/:id/chat adds its chat under the stage.
// Open or closed is remembered per browser (closed by default).
// A deep link to /v/:id/chat counts from the start: a resumed call can sync
// the address before that route is applied.
const voiceShowChat = ref((currentRoute.value.view === 'voice' && !!currentRoute.value.showChat) || loadVoiceChatOpen())
watch(voiceShowChat, open => saveVoiceChatOpen(open))
// The voice channel of the last applied route (null after any other route):
// /v/:id within that Talk closes its chat, entering a Talk keeps it as it was.
let lastVoiceRouteChannel: string | null = null
// The channel the Talk view shows: a previewed one (not joined) or the joined one.
const voiceChannelId = computed(() => previewVoiceChannelId.value || voiceStore.currentChannelId)
let isSyncingFromRoute = false
let routesApplying = 0

// Avoids flashing the login dialog while the session cookie is being checked.
const authChecked = ref(false)
// One voice-resume attempt per login (see the isConnected watcher below).
let resumeAttempted = false

async function initializeApp() {
  if (!alive) return
  await Promise.all([
    chatStore.fetchChannels(),
    chatStore.fetchMembers(),
    chatStore.fetchReadState()
  ])
  if (!alive) return
  chatStore.initWebSocket()
}

// Path of whatever the UI currently shows (used to leave the admin console and
// to give "/" a concrete address).
function currentStatePath() {
  if (voiceStore.activeView === 'voice') {
    const chId = voiceChannelId.value
    if (chId) return voiceShowChat.value ? `/v/${chId}/chat` : `/v/${chId}`
  }
  if (chatStore.activeChannel) {
    if (chatStore.activeThread) return `/c/${chatStore.activeChannel.id}/t/${chatStore.activeThread.id}`
    return `/c/${chatStore.activeChannel.id}`
  }
  return '/'
}

// Bumped by every applyRoute: a route that is still loading when the next one
// arrives stops after its current await instead of acting on stale state.
let routeGen = 0

async function applyRoute(route: Route) {
  if (!alive || !authStore.isAuthenticated) return
  const gen = ++routeGen
  const superseded = () => !alive || gen !== routeGen

  // Admin console is checked before anything renders, so non-admins never see it.
  if (route.view === 'admin' && !authStore.isAdmin) {
    showAdminModal.value = false
    toasts.error(t('nav.noAccess'))
    navigate('/', { replace: true })
    return
  }
  if (!chatStore.allChannels.length) return

  const fix = resolveRoute(route, { isAdmin: authStore.isAdmin, channels: chatStore.allChannels })
  if (fix) {
    if (fix.reason) toasts.error(t(`nav.${fix.reason}`))
    navigate(fix.redirect, { replace: true })
    return
  }

  const previousVoiceChannel = lastVoiceRouteChannel
  lastVoiceRouteChannel = route.view === 'voice' ? route.channelId : null

  isSyncingFromRoute = true
  routesApplying++
  try {
    if (route.view === 'chat' && route.channelId) {
      showAdminModal.value = false
      previewVoiceChannelId.value = null
      voiceStore.activeView = 'chat'
      const targetChannel = chatStore.allChannels.find(c => c.id === route.channelId)
      if (!targetChannel) return
      if (chatStore.activeChannel?.id !== targetChannel.id) {
        await chatStore.selectChannel(targetChannel)
        if (superseded()) return
      }
      if (route.messageId) {
        const found = await chatStore.jumpToMessage(route.messageId)
        if (superseded()) return
        // jumpToMessage already toasted; leave the dead /m/:id address.
        if (found === false) navigate(`/c/${targetChannel.id}`, { replace: true })
      } else if (route.threadId) {
        if (chatStore.activeThread?.id !== route.threadId) {
          await chatStore.openThread(route.threadId)
          if (superseded()) return
          // A thread that failed to load only has its id.
          const th = chatStore.activeThread
          if (th && th.id === route.threadId && !th.user_id && !th.content) {
            chatStore.closeThread()
            toasts.error(t('chat.messageNotFound'))
            navigate(`/c/${targetChannel.id}`, { replace: true })
          }
        }
      } else if (chatStore.activeThread) {
        chatStore.closeThread()
      }
    } else if (route.view === 'voice' && route.channelId) {
      showAdminModal.value = false
      // Opening the channel you're connected to is not a preview.
      previewVoiceChannelId.value = route.channelId === voiceStore.currentChannelId ? null : route.channelId
      voiceStore.activeView = 'voice'
      voiceShowChat.value = voiceChatOpenFor({
        routeShowChat: !!route.showChat,
        sameTalk: previousVoiceChannel === route.channelId,
        remembered: voiceShowChat.value
      })
      // Entering a Talk with its chat remembered open: say so in the address
      // (that route then does the rest).
      if (voiceShowChat.value && !route.showChat) {
        navigate(`/v/${route.channelId}/chat`, { replace: true })
        return
      }
      const targetChannel = chatStore.allChannels.find(c => c.id === route.channelId)
      if (!targetChannel) return
      if (chatStore.activeChannel?.id !== targetChannel.id) {
        await chatStore.selectChannel(targetChannel)
        if (superseded()) return
      }
      if (route.messageId) {
        // The chat (and its scroll handling) renders before the jump.
        await nextTick()
        if (superseded()) return
        const found = await chatStore.jumpToMessage(route.messageId)
        if (superseded()) return
        if (found === false) navigate(`/v/${targetChannel.id}/chat`, { replace: true })
      }
    } else if (route.view === 'admin') {
      adminTab.value = route.tab || 'users'
      showAdminModal.value = true
    } else if (route.view === 'root') {
      showAdminModal.value = false
      const own = currentStatePath()
      if (own !== '/') {
        navigate(own, { replace: true })
      } else {
        const first = chatStore.allChannels.find(c => c.type === 'text') || chatStore.allChannels[0]
        if (first) navigate(`/${first.type === 'voice' ? 'v' : 'c'}/${first.id}`, { replace: true })
      }
    }
  } finally {
    // Overlapping routes each hold the flag; the last one out clears it.
    nextTick(() => {
      if (--routesApplying === 0) isSyncingFromRoute = false
    })
  }
}

function syncCurrentStateToRoute() {
  if (isSyncingFromRoute) return
  if (!authStore.isAuthenticated) return

  const targetPath = showAdminModal.value ? `/admin/${adminTab.value || 'users'}` : currentStatePath()
  if (targetPath !== '/' && window.location.pathname !== targetPath) {
    navigate(targetPath, { replace: false })
  }
}

watch(currentRoute, (newRoute) => {
  applyRoute(newRoute)
})

watch(() => chatStore.activeChannel, () => syncCurrentStateToRoute())
watch(() => chatStore.activeThread, () => syncCurrentStateToRoute())
watch(() => voiceStore.activeView, () => syncCurrentStateToRoute())
// Joining a call (from anywhere) makes the joined channel the one on screen.
watch(() => voiceStore.currentChannelId, id => {
  if (id) previewVoiceChannelId.value = null
  syncCurrentStateToRoute()
})
watch(previewVoiceChannelId, () => syncCurrentStateToRoute())
watch(voiceShowChat, () => syncCurrentStateToRoute())
// "Mention" (profile, member menu) in a Talk opens its chat to write it there.
watch(() => chatStore.pendingMention, name => {
  if (name && voiceStore.activeView === 'voice' && !voiceShowChat.value) voiceShowChat.value = true
})
watch(showAdminModal, () => syncCurrentStateToRoute())
watch(adminTab, () => syncCurrentStateToRoute())

function openAdmin(tab = 'users') {
  if (!authStore.isAdmin) return
  adminTab.value = tab
  showAdminModal.value = true
  navigate(`/admin/${tab}`, { replace: false })
}

function closeAdmin() {
  // Opened from inside the app: step back (popstate then closes the console).
  if (canGoBackInApp()) {
    window.history.back()
    return
  }
  // Deep-linked or reloaded: there's nothing in-app to go back to.
  showAdminModal.value = false
  navigate(currentStatePath(), { replace: true })
}

function onVoiceJoin(channelId: string) {
  previewVoiceChannelId.value = channelId === voiceStore.currentChannelId ? null : channelId
}

async function onAuthSuccess() {
  if (!alive) return
  resumeAttempted = false
  await initializeApp()
  if (!alive) return
  const redirect = popRedirectRoute()
  if (redirect) {
    navigate(redirect, { replace: true })
  } else {
    await applyRoute(currentRoute.value)
    if (!alive) return
    syncCurrentStateToRoute()
  }
}

// Single place that reacts to login, logout and expired sessions.
watch(() => authStore.isAuthenticated, async (isAuthed) => {
  if (!alive) return
  if (isAuthed) {
    if (authChecked.value) {
      await onAuthSuccess()
    }
  } else {
    // Logout or expired session: end the call so mic and connection stop.
    savePendingRoute()
    if (voiceStore.currentChannelId) leaveVoiceChannel()
    chatStore.closeWebSocket()
  }
})

// The server may have dropped our media connection while the socket was down.
watch(() => chatStore.reconnectCount, () => rejoinAfterReconnect())

// Rejoin the voice channel after a page reload (within the 30 s window), once
// the WebSocket is up and the channel list is known. Only once per login, so a
// later reconnect doesn't pull the user back into a call they left.
watch(() => chatStore.isConnected, connected => {
  if (!connected || resumeAttempted) return
  resumeAttempted = true
  resumeVoiceSession().catch(() => {})
})

// Session check at start-up. checkAuth() answers null when the server can't
// tell (rate limited, down, no network): that is not "signed out", so keep
// the loading screen and try again instead of showing the login form.
const authRetrying = ref(false)
const AUTH_RETRY_MAX_MS = 15000

async function checkAuthUntilKnown(delay = 1000) {
  let result = await authStore.checkAuth()
  if (!alive) return null
  while (result === null) {
    authRetrying.value = true
    await new Promise<void>(resolve => {
      cancelAuthRetry = resolve
      authRetryTimer = setTimeout(resolve, delay)
    })
    cancelAuthRetry = undefined
    authRetryTimer = undefined
    if (!alive) return null
    delay = Math.min(delay * 2, AUTH_RETRY_MAX_MS)
    result = await authStore.checkAuth()
    if (!alive) return null
  }
  authRetrying.value = false
  return result
}

onBeforeUnmount(startDesktopGaming())
onMounted(async () => {
  const isAuthed = await checkAuthUntilKnown()
  if (!alive) return
  authChecked.value = true
  if (isAuthed) {
    await onAuthSuccess()
  } else {
    savePendingRoute()
  }
})
</script>

<template>
  <div class="h-full flex overflow-hidden bg-mnema-canvas text-mnema-text">
    <!-- Session check still running: minimal loading state, no data fetching -->
    <div
      v-if="!authChecked"
      class="flex-1 flex flex-col items-center justify-center gap-3"
      role="status"
      aria-live="polite"
    >
      <div class="w-10 h-10 rounded-lg bg-mnema-band border border-mnema-mint/30 flex items-center justify-center text-mnema-mint font-semibold text-base animate-pulse">
        M
      </div>
      <span class="text-sm text-mnema-tertiary">{{ $t(authRetrying ? 'app.retrying' : 'app.checking') }}</span>
    </div>

    <!-- Unauthenticated: login / invite registration -->
    <LoginModal v-else-if="!authStore.isAuthenticated" />

    <!-- Main Mnema Talk Layout -->
    <template v-else>
      <!-- Left Column: Navigation, Channels & User Controls -->
      <div
        class="relative flex flex-col h-full flex-shrink-0 border-r border-mnema-hairline bg-mnema-raised"
        :style="{ width: `${panels.left.width}px` }"
      >
        <Sidebar @open-admin="openAdmin('users')" @open-legal="showLegalModal = true" class="flex-1 min-h-0" />
        <UserBar @open-admin="openAdmin('users')" @open-legal="showLegalModal = true" />
        <ResizeHandle :panel="panels.left" :label="$t('resize.sidebar')" />
      </div>

      <!-- Center: connection banner, then the Talk or a text channel -->
      <div class="flex-1 min-w-0 h-full flex flex-col">
        <ConnectionBanner />
        <!-- Reloading embedded assets cannot update the unsigned DEV application. -->
        <UpdateBanner v-if="!isDesktopRuntime()" />
        <template v-if="voiceStore.activeView === 'voice'">
          <VoiceStage
            :channel-id="voiceChannelId"
            v-model:show-chat="voiceShowChat"
            @join="onVoiceJoin"
            class="min-h-0"
          />
          <!-- The Talk's own text chat, under the stage -->
          <VoiceChatPanel
            v-if="voiceShowChat && voiceChannelId"
            :channel-id="voiceChannelId"
            :panel="voiceChatHeight"
            @close="voiceShowChat = false"
          />
        </template>
        <div v-else class="min-h-0 flex flex-1 flex-col">
          <NativeTrustControl v-if="isDesktopRuntime() && authStore.isAdmin && chatStore.activeChannel" :key="authStore.user?.id + ':' + chatStore.activeChannel.id" :channel-id="chatStore.activeChannel.id" @saved="chatStore.activeChannel && chatStore.fetchMessages(chatStore.activeChannel.id)" />
          <ChatArea class="min-h-0 flex-1" />
        </div>
      </div>

      <!-- Thread panel -->
      <div
        v-if="chatStore.activeThread"
        class="relative h-full flex-shrink-0"
        :style="{ width: `${panels.thread.width}px` }"
      >
        <ThreadSidebar />
        <ResizeHandle :panel="panels.thread" :label="$t('resize.thread')" />
      </div>

      <!-- Right column: member list -->
      <div
        v-if="chatStore.showMemberList"
        class="relative h-full flex-shrink-0"
        :style="{ width: `${panels.members.width}px` }"
      >
        <MemberList />
        <ResizeHandle :panel="panels.members" :label="$t('resize.members')" />
      </div>

      <!-- Admin console dialog -->
      <AdminDashboard v-if="showAdminModal && authStore.isAdmin" :initial-tab="adminTab" @close="closeAdmin" />

      <!-- Connection statistics dialog -->
      <ConnectionStatsModal 
        v-if="voiceStore.showStatsModal" 
        @close="voiceStore.showStatsModal = false" 
      />

      <!-- Audio settings dialog -->
      <AudioSettingsModal 
        v-if="voiceStore.showAudioSettings" 
        @close="voiceStore.showAudioSettings = false" 
      />

      <!-- Screen share quality dialog -->
      <ScreenShareModal
        v-if="voiceStore.showScreenShareModal"
        @close="voiceStore.closeScreenShareModal()"
      />

      <!-- User profile dialog -->
      <UserProfileModal 
        v-if="chatStore.selectedUserProfile"
        :user="chatStore.selectedUserProfile"
        @close="chatStore.closeUserProfile()"
        @mention="(uname) => chatStore.insertMention(uname)"
      />

      <!-- Legal & privacy dialog -->
      <LegalModal v-if="showLegalModal" @close="showLegalModal = false" />

      <!-- The video of the Picture-in-Picture window (outlives the Talk view) -->
      <PipHost />

      <!-- Browser blocked call audio after a reload: one click unblocks it -->
      <button
        v-if="voiceStore.audioBlocked && voiceStore.isConnected"
        type="button"
        class="fixed bottom-4 left-1/2 -translate-x-1/2 z-50 px-4 py-2 rounded-md bg-mnema-mint text-mnema-canvas text-sm font-semibold shadow-lg hover:brightness-110"
        @click="resumeRemoteAudio"
      >
        {{ $t('voice.audioBlocked') }}
      </button>
    </template>

    <ToastHost />
    <ConfirmDialog />
  </div>
</template>
