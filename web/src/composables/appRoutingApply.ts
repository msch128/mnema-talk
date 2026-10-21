import type { Ref } from 'vue'
import type { Route } from '../lib/router'
import { nextTick } from 'vue'
import { useAuthStore } from '../stores/auth'
import { usePermissionsStore } from '../stores/permissions'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { useToastStore } from '../stores/toast'
import { navigate, resolveRoute } from '../lib/router'
import { voiceChatOpenFor } from '../lib/voiceChatPanel'
import { t } from '../i18n'

/** Everything the route applier reads and writes, owned by useAppRouting. */
export interface RoutingContext {
  alive: () => boolean
  authStore: ReturnType<typeof useAuthStore>
  permissions: ReturnType<typeof usePermissionsStore>
  chatStore: ReturnType<typeof useChatStore>
  voiceStore: ReturnType<typeof useVoiceStore>
  toasts: ReturnType<typeof useToastStore>
  showAdminModal: Ref<boolean>
  adminTab: Ref<string>
  previewVoiceChannelId: Ref<string | null>
  voiceShowChat: Ref<boolean>
  currentStatePath: () => string
  /** The voice channel of the last applied route (null after any other route). */
  lastVoiceRouteChannel: string | null
  isSyncingFromRoute: boolean
  routesApplying: number
  /** True from the first fetch of a login until its route is applied. */
  loadingFirstRoute: boolean
  /** Bumped by every applyRoute so a stale, still-loading route stops. */
  routeGen: number
}

type Superseded = () => boolean

async function selectChannelFor(ctx: RoutingContext, channelId: string, superseded: Superseded) {
  const target = ctx.chatStore.allChannels.find(c => c.id === channelId)
  if (!target) return null
  if (ctx.chatStore.activeChannel?.id !== target.id) {
    await ctx.chatStore.selectChannel(target)
    if (superseded()) return null
  }
  return target
}

function isThreadEmptyStub(th: { id: string; user_id?: string; content?: string } | null | undefined, threadId: string): boolean {
  if (th?.id !== threadId) return false
  return !th.user_id && !th.content
}

async function openRouteThread(ctx: RoutingContext, channelId: string, threadId: string, superseded: Superseded) {
  const { chatStore } = ctx
  if (chatStore.activeThread?.id === threadId) return
  await chatStore.openThread(threadId)
  if (superseded()) return
  // A thread that failed to load only has its id.
  if (isThreadEmptyStub(chatStore.activeThread, threadId)) {
    chatStore.closeThread()
    ctx.toasts.error(t('chat.messageNotFound'))
    navigate(`/c/${channelId}`, { replace: true })
  }
}

async function applyChatRoute(ctx: RoutingContext, route: Route & { view: 'chat' }, superseded: Superseded) {
  const { chatStore } = ctx
  ctx.showAdminModal.value = false
  ctx.previewVoiceChannelId.value = null
  ctx.voiceStore.activeView = 'chat'
  const targetChannel = await selectChannelFor(ctx, route.channelId, superseded)
  if (!targetChannel) return
  if (route.messageId) {
    const found = await chatStore.jumpToMessage(route.messageId)
    if (superseded()) return
    // jumpToMessage already toasted; leave the dead /m/:id address.
    if (found === false) navigate(`/c/${targetChannel.id}`, { replace: true })
  } else if (route.threadId) {
    await openRouteThread(ctx, targetChannel.id, route.threadId, superseded)
  } else if (chatStore.activeThread) {
    chatStore.closeThread()
  }
}

function syncVoiceViewState(
  ctx: RoutingContext,
  route: Route & { view: 'voice' },
  previousVoiceChannel: string | null
): boolean {
  ctx.showAdminModal.value = false
  ctx.previewVoiceChannelId.value = route.channelId === ctx.voiceStore.currentChannelId ? null : route.channelId
  ctx.voiceStore.activeView = 'voice'
  ctx.voiceShowChat.value = voiceChatOpenFor({
    routeShowChat: !!route.showChat,
    sameTalk: previousVoiceChannel === route.channelId,
    remembered: ctx.voiceShowChat.value
  })
  if (ctx.voiceShowChat.value && !route.showChat) {
    navigate(`/v/${route.channelId}/chat`, { replace: true })
    return true
  }
  return false
}

async function jumpToVoiceMessage(
  ctx: RoutingContext,
  targetChannelId: string,
  messageId: string,
  superseded: Superseded
) {
  await nextTick()
  if (superseded()) return
  const found = await ctx.chatStore.jumpToMessage(messageId)
  if (superseded()) return
  if (found === false) navigate(`/v/${targetChannelId}/chat`, { replace: true })
}

async function applyVoiceRoute(
  ctx: RoutingContext,
  route: Route & { view: 'voice' },
  previousVoiceChannel: string | null,
  superseded: Superseded
) {
  if (syncVoiceViewState(ctx, route, previousVoiceChannel)) return
  const targetChannel = await selectChannelFor(ctx, route.channelId, superseded)
  if (!targetChannel || !route.messageId) return
  await jumpToVoiceMessage(ctx, targetChannel.id, route.messageId, superseded)
}

function applyRootRoute(ctx: RoutingContext) {
  ctx.showAdminModal.value = false
  const own = ctx.currentStatePath()
  if (own !== '/') {
    navigate(own, { replace: true })
    return
  }
  const channels = ctx.chatStore.allChannels
  const first = channels.find(c => c.type === 'text') || channels[0]
  if (first) navigate(`/${first.type === 'voice' ? 'v' : 'c'}/${first.id}`, { replace: true })
}

/** Admin console is checked before anything renders, so non-admins never see it. */
async function admitRoute(ctx: RoutingContext, route: Route, superseded: Superseded) {
  if (route.view !== 'admin') return true
  const { permissions } = ctx
  // A reload straight into the console: what the member may do is not known yet.
  if (!permissions.isOwner && !permissions.loaded) {
    await permissions.load()
    if (superseded()) return false
  }
  if (permissions.canOpenAdmin) return true
  ctx.showAdminModal.value = false
  ctx.toasts.error(t('nav.noAccess'))
  navigate('/', { replace: true })
  return false
}

async function dispatchRoute(ctx: RoutingContext, route: Route, previousVoiceChannel: string | null, superseded: Superseded) {
  if (route.view === 'chat' && route.channelId) {
    await applyChatRoute(ctx, route, superseded)
  } else if (route.view === 'voice' && route.channelId) {
    await applyVoiceRoute(ctx, route, previousVoiceChannel, superseded)
  } else if (route.view === 'admin') {
    ctx.adminTab.value = route.tab || 'users'
    ctx.showAdminModal.value = true
  } else if (route.view === 'root') {
    applyRootRoute(ctx)
  }
}

function handleRouteRedirect(ctx: RoutingContext, route: Route): boolean {
  const fix = resolveRoute(route, { isAdmin: ctx.permissions.canOpenAdmin, channels: ctx.chatStore.allChannels })
  if (!fix) return false
  if (fix.reason) ctx.toasts.error(t(`nav.${fix.reason}`))
  navigate(fix.redirect, { replace: true })
  return true
}

async function runSyncedDispatch(
  ctx: RoutingContext,
  route: Route,
  previousVoiceChannel: string | null,
  superseded: Superseded
) {
  ctx.isSyncingFromRoute = true
  ctx.routesApplying++
  try {
    await dispatchRoute(ctx, route, previousVoiceChannel, superseded)
  } finally {
    // Overlapping routes each hold the flag; the last one out clears it.
    nextTick(() => {
      if (--ctx.routesApplying === 0) ctx.isSyncingFromRoute = false
    })
  }
}

/** Shows what the address says: the channel, thread, Talk or admin console. */
export async function applyRoute(ctx: RoutingContext, route: Route) {
  if (!ctx.alive() || !ctx.authStore.isAuthenticated) return
  const gen = ++ctx.routeGen
  const superseded = () => !ctx.alive() || gen !== ctx.routeGen

  if (!(await admitRoute(ctx, route, superseded))) return
  if (!ctx.chatStore.allChannels.length) return
  if (handleRouteRedirect(ctx, route)) return

  const previousVoiceChannel = ctx.lastVoiceRouteChannel
  ctx.lastVoiceRouteChannel = route.view === 'voice' ? route.channelId : null

  await runSyncedDispatch(ctx, route, previousVoiceChannel, superseded)
}
