import type { Route } from '../lib/router'
import { ref, computed, watch, nextTick } from 'vue'
import { useAuthStore } from '../stores/auth'
import { usePermissionsStore } from '../stores/permissions'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { useToastStore } from '../stores/toast'
import { currentRoute, navigate, resolveRoute, canGoBackInApp } from '../lib/router'
import { loadVoiceChatOpen, saveVoiceChatOpen, voiceChatOpenFor } from '../lib/voiceChatPanel'
import { t } from '../i18n'

/**
 * The app's address and what it shows, kept in step both ways: the route
 * decides the channel, thread, Talk or admin console on screen, and what the
 * user does on screen is written back to the address.
 */
export function useAppRouting(alive: () => boolean) {
  const authStore = useAuthStore()
  const permissions = usePermissionsStore()
  const chatStore = useChatStore()
  const voiceStore = useVoiceStore()
  const toasts = useToastStore()

  const showAdminModal = ref(false)
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
  // True from the first fetch of a login until its route is applied: loading the
  // channels selects one, which must not overwrite a deep link (the admin console).
  let loadingFirstRoute = false
  // Bumped by every applyRoute: a route that is still loading when the next one
  // arrives stops after its current await instead of acting on stale state.
  let routeGen = 0

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

  async function applyChatRoute(route: Route & { view: 'chat' }, superseded: () => boolean) {
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
  }

  async function applyVoiceRoute(route: Route & { view: 'voice' }, previousVoiceChannel: string | null, superseded: () => boolean) {
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
  }

  function applyRootRoute() {
    showAdminModal.value = false
    const own = currentStatePath()
    if (own !== '/') {
      navigate(own, { replace: true })
    } else {
      const first = chatStore.allChannels.find(c => c.type === 'text') || chatStore.allChannels[0]
      if (first) navigate(`/${first.type === 'voice' ? 'v' : 'c'}/${first.id}`, { replace: true })
    }
  }

  async function applyRoute(route: Route) {
    if (!alive() || !authStore.isAuthenticated) return
    const gen = ++routeGen
    const superseded = () => !alive() || gen !== routeGen

    // Admin console is checked before anything renders, so non-admins never see it.
    if (route.view === 'admin' && !permissions.isOwner && !permissions.loaded) {
      // A reload straight into the console: what the member may do is not known yet.
      await permissions.load()
      if (superseded()) return
    }
    if (route.view === 'admin' && !permissions.canOpenAdmin) {
      showAdminModal.value = false
      toasts.error(t('nav.noAccess'))
      navigate('/', { replace: true })
      return
    }
    if (!chatStore.allChannels.length) return

    const fix = resolveRoute(route, { isAdmin: permissions.canOpenAdmin, channels: chatStore.allChannels })
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
        await applyChatRoute(route, superseded)
      } else if (route.view === 'voice' && route.channelId) {
        await applyVoiceRoute(route, previousVoiceChannel, superseded)
      } else if (route.view === 'admin') {
        adminTab.value = route.tab || 'users'
        showAdminModal.value = true
      } else if (route.view === 'root') {
        applyRootRoute()
      }
    } finally {
      // Overlapping routes each hold the flag; the last one out clears it.
      nextTick(() => {
        if (--routesApplying === 0) isSyncingFromRoute = false
      })
    }
  }

  function syncCurrentStateToRoute() {
    if (isSyncingFromRoute || loadingFirstRoute) return
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
    if (!permissions.canOpenAdmin) return
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

  return {
    showAdminModal,
    adminTab,
    voiceShowChat,
    voiceChannelId,
    applyRoute,
    syncCurrentStateToRoute,
    currentStatePath,
    openAdmin,
    closeAdmin,
    onVoiceJoin,
    /** A login's first fetch starts: the channel list selecting one must not overwrite a deep link. */
    beginFirstRoute() { loadingFirstRoute = true },
    endFirstRoute() { loadingFirstRoute = false },
    /** Stops a route that is still loading. */
    dispose() { ++routeGen }
  }
}
