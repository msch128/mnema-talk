<script setup lang="ts">
import { startDesktopGaming } from './lib/desktopGaming'
import { ref, watch, onMounted, onBeforeUnmount } from 'vue'
import { useAuthStore } from './stores/auth'
import { usePermissionsStore } from './stores/permissions'
import { useChatStore } from './stores/chat'
import { useVoiceStore } from './stores/voice'
import { currentRoute, navigate, popRedirectRoute, savePendingRoute } from './lib/router'
import Sidebar from './components/Sidebar.vue'
import UserBar from './components/UserBar.vue'
import ChatArea from './components/ChatArea.vue'
import NativeTrustControl from './components/NativeTrustControl.vue'
import { platform } from './lib/platform'
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
import TimeoutBanner from './components/TimeoutBanner.vue'
import ModerationDialog from './components/ModerationDialog.vue'
import MessageModerationDialog from './components/MessageModerationDialog.vue'
import UpdateBanner from './components/UpdateBanner.vue'
import PipHost from './components/PipHost.vue'
import { useResizable } from './composables/useResizable'
import { useWebRTC } from './composables/useWebRTC'
import { useAppRouting } from './composables/useAppRouting'
import { useAppBadge } from './composables/useAppBadge'
import { VOICE_CHAT_HEIGHT, STAGE_MIN_HEIGHT } from './lib/voiceChatPanel'

const authStore = useAuthStore()
const permissions = usePermissionsStore()
const chatStore = useChatStore()
const voiceStore = useVoiceStore()
let alive = true
let authRetryTimer: ReturnType<typeof setTimeout> | undefined
let cancelAuthRetry: (() => void) | undefined
onBeforeUnmount(() => {
  alive = false
  routing.dispose()
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
const showLegalModal = ref(false)
const routing = useAppRouting(() => alive)
useAppBadge()
const { showAdminModal, adminTab, voiceShowChat, voiceChannelId, applyRoute, syncCurrentStateToRoute, openAdmin, closeAdmin, onVoiceJoin } = routing

// Avoids flashing the login dialog while the session cookie is being checked.
const authChecked = ref(false)
// One voice-resume attempt per login (see the isConnected watcher below).
let resumeAttempted = false

async function initializeApp() {
  if (!alive) return
  routing.beginFirstRoute()
  await Promise.all([
    chatStore.fetchChannels(),
    chatStore.fetchMembers(),
    chatStore.fetchReadState()
  ])
  if (!alive) return
  chatStore.initWebSocket()
}

async function onAuthSuccess() {
  if (!alive) return
  resumeAttempted = false
  await initializeApp()
  if (!alive) return
  const redirect = popRedirectRoute()
  if (redirect) {
    routing.endFirstRoute()
    navigate(redirect, { replace: true })
  } else {
    try {
      await applyRoute(currentRoute.value)
    } finally {
      routing.endFirstRoute()
    }
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
        <TimeoutBanner />
        <!-- Reloading embedded assets cannot update the unsigned DEV application. -->
        <UpdateBanner v-if="!platform.isDesktop()" />
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
          <NativeTrustControl v-if="platform.isDesktop() && authStore.isAdmin && chatStore.activeChannel" :key="authStore.user?.id + ':' + chatStore.activeChannel.id" :channel-id="chatStore.activeChannel.id" @saved="chatStore.activeChannel && chatStore.fetchMessages(chatStore.activeChannel.id)" />
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
      <AdminDashboard v-if="showAdminModal && permissions.canOpenAdmin" :initial-tab="adminTab" @close="closeAdmin" />

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
    <ModerationDialog />
    <MessageModerationDialog />
  </div>
</template>
