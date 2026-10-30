import type { Ref } from 'vue'
import type { Route, TargetRoute } from '../lib/router'
import { nextTick } from 'vue'
import { useAuthStore } from '../stores/auth'
import { usePermissionsStore } from '../stores/permissions'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { useToastStore } from '../stores/toast'
import { navigate, resolveRoute, targetOf } from '../lib/router'
import { pathDms } from '../lib/dm'
import { channelPath, postPath, postShortId, isUuid, textPath, talkPath } from '../lib/routePaths'
import { fetchForumPosts } from '../lib/forumApi'
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
  const target = ctx.chatStore.allChannels.find(c => c.id === channelId) ?? ctx.chatStore.dmChannel(channelId)
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

async function openRouteThread(ctx: RoutingContext, channel: ChannelLike, threadId: string, superseded: Superseded) {
  const { chatStore } = ctx
  if (chatStore.activeThread?.id !== threadId) {
    await chatStore.openThread(threadId)
    if (superseded()) return
  }
  // A thread that failed to load only has its id.
  if (isThreadEmptyStub(chatStore.activeThread, threadId)) {
    chatStore.closeThread()
    ctx.toasts.error(t('chat.messageNotFound'))
    navigate(textPath(channel), { replace: true })
    return
  }
  // A forum post has a speaking address of its own.
  const post = chatStore.activeThread
  if (channel.type !== 'forum' || !post?.title) return
  // Rewriting the address to itself would apply this route again, which
  // reopens the post the user has just closed.
  const own = postPath(channel, post)
  if (window.location.pathname !== own) navigate(own, { replace: true })
}

/** Pages of 100 posts a /p address searches before it gives up, so a stale link cannot walk a whole large forum. */
const MAX_POST_PAGES = 50

type ChannelLike = { id: string; type: string; slug?: string | undefined }

/** The post a /p/:title-:shortId address names, looked up page by page through the forum's posts, newest first; null when none or several match. */
async function findForumPost(channelId: string, ref: string): Promise<string | null> {
  if (isUuid(ref)) return ref.toLowerCase()
  const short = postShortId(ref)
  if (!short) return null
  let before: string | undefined
  for (let page = 0; page < MAX_POST_PAGES; page++) {
    const posts = await fetchForumPosts(channelId, { sort: 'newest', limit: 100, ...(before ? { before } : {}) })
    const hits = posts.filter(p => p.id.startsWith(short))
    if (hits.length > 1) return null
    if (hits[0]) return hits[0].id
    const last = posts[posts.length - 1]
    // A cursor that does not advance would page forever.
    if (posts.length < 100 || !last || last.id === before) return null
    before = last.id
  }
  return null
}

async function openRoutePost(ctx: RoutingContext, channel: ChannelLike, ref: string, superseded: Superseded) {
  const id = await findForumPost(channel.id, ref).catch(() => null)
  if (superseded()) return
  if (!id) {
    ctx.toasts.error(t('chat.messageNotFound'))
    navigate(textPath(channel), { replace: true })
    return
  }
  await openRouteThread(ctx, channel, id, superseded)
}

async function applyChatRoute(ctx: RoutingContext, route: TargetRoute & { view: 'chat' }, superseded: Superseded) {
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
    if (found === false) navigate(textPath(targetChannel), { replace: true })
  } else if (route.threadId) {
    await openRouteThread(ctx, targetChannel, route.threadId, superseded)
  } else if (route.postRef) {
    await openRoutePost(ctx, targetChannel, route.postRef, superseded)
  } else if (chatStore.activeThread) {
    chatStore.closeThread()
  }
}

function syncVoiceViewState(
  ctx: RoutingContext,
  route: TargetRoute & { view: 'voice' },
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
    navigate(talkPath(channelOf(ctx, route.channelId), { chat: true }), { replace: true })
    return true
  }
  return false
}

async function jumpToVoiceMessage(
  ctx: RoutingContext,
  targetChannel: ChannelLike,
  messageId: string,
  superseded: Superseded
) {
  await nextTick()
  if (superseded()) return
  const found = await ctx.chatStore.jumpToMessage(messageId)
  if (superseded()) return
  if (found === false) navigate(talkPath(targetChannel, { chat: true }), { replace: true })
}

async function applyVoiceRoute(
  ctx: RoutingContext,
  route: TargetRoute & { view: 'voice' },
  previousVoiceChannel: string | null,
  superseded: Superseded
) {
  if (syncVoiceViewState(ctx, route, previousVoiceChannel)) return
  const targetChannel = await selectChannelFor(ctx, route.channelId, superseded)
  if (!targetChannel || !route.messageId) return
  await jumpToVoiceMessage(ctx, targetChannel, route.messageId, superseded)
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
  if (first) navigate(channelPath(first), { replace: true })
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

async function dispatchRoute(ctx: RoutingContext, route: TargetRoute, previousVoiceChannel: string | null, superseded: Superseded) {
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

function routeLookups(ctx: RoutingContext) {
  return { channels: ctx.chatStore.allChannels, dms: pathDms(ctx.chatStore.dms, ctx.authStore.user?.id) }
}

function channelOf(ctx: RoutingContext, id: string): ChannelLike {
  return ctx.chatStore.allChannels.find(c => c.id === id) ?? { id, type: 'voice' }
}

function handleRouteRedirect(ctx: RoutingContext, route: Route): boolean {
  const fix = resolveRoute(route, { isAdmin: ctx.permissions.canOpenAdmin, ...routeLookups(ctx) })
  if (!fix) return false
  if (fix.reason) ctx.toasts.error(t(`nav.${fix.reason}`))
  navigate(fix.redirect, { replace: true })
  return true
}

async function runSyncedDispatch(
  ctx: RoutingContext,
  route: TargetRoute,
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

/**
 * Shows what the address says: the channel, thread, Talk or admin console.
 * Returns true when the address was rewritten instead: the rewritten route is
 * applied by the router's own watcher, and until it is, the screen does not
 * yet show what the address will say.
 */
export async function applyRoute(ctx: RoutingContext, route: Route): Promise<boolean> {
  if (!ctx.alive() || !ctx.authStore.isAuthenticated) return false
  const gen = ++ctx.routeGen
  const superseded = () => !ctx.alive() || gen !== ctx.routeGen

  if (!(await admitRoute(ctx, route, superseded))) return false
  if (!ctx.chatStore.allChannels.length) return false
  if (handleRouteRedirect(ctx, route)) return true

  const target = targetOf(route, routeLookups(ctx))
  if (!target) return false
  const previousVoiceChannel = ctx.lastVoiceRouteChannel
  ctx.lastVoiceRouteChannel = target.view === 'voice' ? target.channelId : null

  await runSyncedDispatch(ctx, target, previousVoiceChannel, superseded)
  return false
}
