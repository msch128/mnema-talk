<script setup>
import { ref, watch, onMounted, nextTick } from 'vue'
import { useAuthStore } from './stores/auth'
import { useChatStore } from './stores/chat'
import { useVoiceStore } from './stores/voice'
import { currentRoute, navigate, popRedirectRoute, savePendingRoute } from './lib/router'
import Sidebar from './components/Sidebar.vue'
import UserBar from './components/UserBar.vue'
import ChatArea from './components/ChatArea.vue'
import VoiceStage from './components/VoiceStage.vue'
import MemberList from './components/MemberList.vue'
import ThreadSidebar from './components/ThreadSidebar.vue'
import LoginModal from './components/LoginModal.vue'
import AdminDashboard from './components/AdminDashboard.vue'
import ConnectionStatsModal from './components/ConnectionStatsModal.vue'
import AudioSettingsModal from './components/AudioSettingsModal.vue'
import UserProfileModal from './components/UserProfileModal.vue'
import LegalModal from './components/LegalModal.vue'
import ResizeHandle from './components/ResizeHandle.vue'
import ToastHost from './components/ToastHost.vue'
import ConfirmDialog from './components/ConfirmDialog.vue'
import ConnectionBanner from './components/ConnectionBanner.vue'
import { useResizable } from './composables/useResizable'
import { useWebRTC } from './composables/useWebRTC'

const authStore = useAuthStore()
const chatStore = useChatStore()
const voiceStore = useVoiceStore()
const { resumeVoiceSession, resumeRemoteAudio, leaveVoiceChannel, rejoinAfterReconnect } = useWebRTC()

// Column widths. Order = shrink priority on narrow windows
// (thread first, then member list, then the left sidebar).
const panels = useResizable([
  { name: 'left', side: 'left', defaultWidth: 240, min: 200, max: 360 },
  { name: 'members', side: 'right', defaultWidth: 240, min: 200, max: 360, visible: () => chatStore.showMemberList },
  { name: 'thread', side: 'right', defaultWidth: 400, min: 320, max: 640, visible: () => !!chatStore.activeThread }
], { centerMin: 400 })
const showAdminModal = ref(false)
const showLegalModal = ref(false)
const adminTab = ref('users')
const previewVoiceChannelId = ref(null)
const voiceShowChat = ref(true)
let isSyncingFromRoute = false

// Avoids flashing the login dialog while the session cookie is being checked.
const authChecked = ref(false)
// One voice-resume attempt per login (see the isConnected watcher below).
let resumeAttempted = false

async function initializeApp() {
  await Promise.all([
    chatStore.fetchChannels(),
    chatStore.fetchMembers(),
    chatStore.fetchReadState()
  ])
  chatStore.initWebSocket()
}

async function applyRoute(route) {
  if (!authStore.isAuthenticated) return
  if (!chatStore.allChannels.length) return

  isSyncingFromRoute = true
  try {
    if (route.view === 'chat' && route.channelId) {
      showAdminModal.value = false
      previewVoiceChannelId.value = null
      voiceStore.activeView = 'chat'
      const targetChannel = chatStore.allChannels.find(c => c.id === route.channelId)
      if (targetChannel) {
        if (chatStore.activeChannel?.id !== targetChannel.id) {
          await chatStore.selectChannel(targetChannel)
        }
        if (route.messageId) {
          await chatStore.jumpToMessage(route.messageId)
        } else if (route.threadId) {
          if (chatStore.activeThread?.id !== route.threadId) {
            await chatStore.openThread(route.threadId)
          }
        } else if (chatStore.activeThread) {
          chatStore.closeThread()
        }
      }
    } else if (route.view === 'voice' && route.channelId) {
      showAdminModal.value = false
      previewVoiceChannelId.value = route.channelId
      voiceStore.activeView = 'voice'
      voiceShowChat.value = !!route.showChat
      const targetChannel = chatStore.allChannels.find(c => c.id === route.channelId)
      if (targetChannel && chatStore.activeChannel?.id !== targetChannel.id) {
        await chatStore.selectChannel(targetChannel)
      }
    } else if (route.view === 'admin') {
      adminTab.value = route.tab || 'users'
      showAdminModal.value = true
    } else if (route.view === 'root') {
      if (!chatStore.activeChannel && chatStore.allChannels.length) {
        const first = chatStore.allChannels.find(c => c.type === 'text') || chatStore.allChannels[0]
        if (first) {
          if (first.type === 'voice') {
            navigate(`/v/${first.id}`, { replace: true })
          } else {
            navigate(`/c/${first.id}`, { replace: true })
          }
        }
      }
    }
  } finally {
    nextTick(() => {
      isSyncingFromRoute = false
    })
  }
}

function syncCurrentStateToRoute() {
  if (isSyncingFromRoute) return
  if (!authStore.isAuthenticated) return

  let targetPath = '/'
  if (showAdminModal.value) {
    targetPath = `/admin/${adminTab.value || 'users'}`
  } else if (voiceStore.activeView === 'voice') {
    const chId = previewVoiceChannelId.value || voiceStore.currentChannelId
    if (chId) {
      targetPath = voiceShowChat.value ? `/v/${chId}/chat` : `/v/${chId}`
    }
  } else if (chatStore.activeChannel) {
    if (chatStore.activeThread) {
      targetPath = `/c/${chatStore.activeChannel.id}/t/${chatStore.activeThread.id}`
    } else {
      targetPath = `/c/${chatStore.activeChannel.id}`
    }
  }

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
watch(() => voiceStore.currentChannelId, () => syncCurrentStateToRoute())
watch(previewVoiceChannelId, () => syncCurrentStateToRoute())
watch(voiceShowChat, () => syncCurrentStateToRoute())
watch(showAdminModal, () => syncCurrentStateToRoute())
watch(adminTab, () => syncCurrentStateToRoute())

function openAdmin(tab = 'users') {
  adminTab.value = tab
  showAdminModal.value = true
  navigate(`/admin/${tab}`, { replace: false })
}

function closeAdmin() {
  showAdminModal.value = false
  if (voiceStore.activeView === 'voice') {
    const chId = previewVoiceChannelId.value || voiceStore.currentChannelId
    if (chId) {
      navigate(voiceShowChat.value ? `/v/${chId}/chat` : `/v/${chId}`, { replace: false })
      return
    }
  }
  if (chatStore.activeChannel) {
    if (chatStore.activeThread) {
      navigate(`/c/${chatStore.activeChannel.id}/t/${chatStore.activeThread.id}`, { replace: false })
    } else {
      navigate(`/c/${chatStore.activeChannel.id}`, { replace: false })
    }
    return
  }
  navigate('/', { replace: false })
}

async function onAuthSuccess() {
  resumeAttempted = false
  await initializeApp()
  const redirect = popRedirectRoute()
  if (redirect) {
    navigate(redirect, { replace: true })
  } else {
    await applyRoute(currentRoute.value)
    syncCurrentStateToRoute()
  }
}

// Single place that reacts to login, logout and expired sessions.
watch(() => authStore.isAuthenticated, async (isAuthed) => {
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

onMounted(async () => {
  const isAuthed = await authStore.checkAuth()
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
      <span class="text-sm text-mnema-tertiary">{{ $t('app.checking') }}</span>
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

      <!-- Center: connection banner, then the Tafelrunde or a text channel -->
      <div class="flex-1 min-w-0 h-full flex flex-col">
        <ConnectionBanner />
        <VoiceStage
          v-if="voiceStore.activeView === 'voice'"
          :channel-id="previewVoiceChannelId || voiceStore.currentChannelId"
          :initial-show-chat="voiceShowChat"
          @update:show-chat="voiceShowChat = $event"
          @join="previewVoiceChannelId = null"
          class="min-h-0"
        />
        <ChatArea v-else class="min-h-0" />
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
      <AdminDashboard v-if="showAdminModal" :initial-tab="adminTab" @close="closeAdmin" />

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

      <!-- User profile dialog -->
      <UserProfileModal 
        v-if="chatStore.selectedUserProfile"
        :user="chatStore.selectedUserProfile"
        @close="chatStore.closeUserProfile()"
        @mention="(uname) => chatStore.insertMention(uname)"
      />

      <!-- Legal & privacy dialog -->
      <LegalModal v-if="showLegalModal" @close="showLegalModal = false" />

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
