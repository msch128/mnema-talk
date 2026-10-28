// Context-menu content for the sidebar (channels, categories) and for people
// (member list, voice participants). Pure builders around the stores so the
// menus are testable per role.
import { reactive, ref, computed, nextTick } from 'vue'
import type { Category, Channel, User as UserModel, NotifyLevel } from '../types/domain'
import type { ChatStore } from '../stores/chat'
import type { ContextMenuItem, MenuAnchor } from '../components/menuTypes'

type MenuMember = Pick<UserModel, 'id'> & Partial<UserModel>
type MenuBuilder = (refresh: () => void) => ContextMenuItem[]
interface ChannelHandlers { onPermissions?: (channel: Channel) => void; onWebhooks?: (channel: Channel) => void; onEdit?: (channel: Channel) => void; onDuplicate?: (channel: Channel) => void; onDelete?: (channel: Channel) => void }
interface CategoryHandlers { onPermissions?: (category: Category) => void; onCollapseAll?: () => void; onExpandAll?: () => void; allCollapsed?: boolean; noneCollapsed?: boolean; onCreateChannel?: (category: Category) => void; onCreateCategory?: (category: Category) => void; onEdit?: (category: Category) => void; onDelete?: (category: Category) => void }
import {
  CheckCheck, Link, Pencil, Trash2, User, AtSign, Volume2, VolumeX, Eye, EyeOff, UserX, Ban, LogOut,
  CopyPlus, Radio, Plus, FolderPlus, ChevronsDownUp, ChevronsUpDown, Shield, Webhook, Clock, Gavel, SlidersHorizontal
} from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { useAuthStore } from '../stores/auth'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { canConfigureTalk, canToggleStage, isStageTalk, openTalkSettings, setStage } from '../lib/talks'
import { api } from '../lib/api'
import { channelPath } from '../lib/routePaths'
import { confirm } from '../lib/confirm'
import { menuPointFromEvent } from '../lib/contextMenuPoint'
import type { MenuTrigger } from '../lib/contextMenuPoint'
import { moderationOffer, openTimeout, openBan, liftTimeout } from '../lib/moderationActions'
import { t } from '../i18n'
import { memberRelationItems } from './dmMenuItems'
import { notifyMenuItems } from './useMuteMenu'

/** Open/close state plus the items for one ContextMenu instance. */
export function useMenuState() {
  const state = reactive<{ open: boolean; x: number; y: number; anchor: MenuAnchor | null; build: MenuBuilder | null }>({ open: false, x: 0, y: 0, anchor: null, build: null })
  const tick = ref(0)
  const items = computed(() => {
    void tick.value
    return state.open && state.build ? state.build(refresh) : []
  })
  function refresh() { tick.value++ }
  async function show(e: MenuTrigger, build: MenuBuilder) {
    e?.preventDefault?.()
    e?.stopPropagation?.()
    const p = menuPointFromEvent(e)
    state.open = false
    await nextTick()
    Object.assign(state, { x: p.x, y: p.y, anchor: p.anchor, build, open: true })
  }
  return { state, items, show, refresh }
}

export function channelLink(channel: Channel) {
  const base = typeof window !== 'undefined' ? window.location.origin : ''
  return `${base}${channelPath(channel)}`
}

async function copyText(text: string, okMessage: string) {
  const toasts = useToastStore()
  try {
    await navigator.clipboard.writeText(text)
    toasts.success(okMessage)
    return true
  } catch {
    toasts.error(t('admin.copyFailed') + ' ' + text)
    return false
  }
}

export function notifyLevelOf(chatStore: Pick<ChatStore, "notificationLevel" | "readStates">, channelId: string): NotifyLevel {
  return chatStore.notificationLevel?.(channelId)
    ?? chatStore.readStates?.[channelId]?.notify_level
    ?? 'all'
}

function basicChannelItems(channel: Channel): ContextMenuItem[] {
  const chatStore = useChatStore()
  const toasts = useToastStore()
  const rs = chatStore.readStates?.[channel.id]
  const hasUnread = (rs?.unread_count || 0) > 0 || (rs?.mention_count || 0) > 0
  return [
    {
      id: 'mark-read',
      label: t('sidebar.markRead'),
      icon: CheckCheck,
      disabled: !hasUnread,
      action: async () => {
        await chatStore.markChannelRead?.(channel.id)
        toasts.success(t('sidebar.markedRead'))
      }
    },
    {
      id: 'copy-link',
      label: t('sidebar.copyLink'),
      icon: Link,
      action: () => copyText(channelLink(channel), t('sidebar.copiedLink'))
    }
  ]
}

/** "Talk settings" for a Talk the member manages, or a temporary Talk they lead. */
function talkSettingsItems(channel: Channel): ContextMenuItem[] {
  if (!useServerFeaturesStore().talksLobby) return []
  const canManage = usePermissionsStore().can('manage_channels')
  if (!canConfigureTalk(channel, useAuthStore().user?.id, canManage)) return []
  return [{ id: 'talk-settings', label: t('talk.settings'), icon: SlidersHorizontal, action: () => openTalkSettings(channel.id) }]
}

/** "Make Stage" / "Make normal Talk" for a permanent Talk. */
function stageToggleItems(channel: Channel): ContextMenuItem[] {
  if (!useServerFeaturesStore().talksStage || !canToggleStage(channel)) return []
  const stage = !isStageTalk(channel)
  const failed = () => useToastStore().error(t('stage.toggleFailed'))
  return [{ id: 'stage-toggle', label: t(stage ? 'stage.make' : 'stage.makeNormal'), icon: Radio, action: () => { void setStage(channel.id, stage).catch(failed) } }]
}

function channelManagementItems(channel: Channel, handlers: ChannelHandlers): ContextMenuItem[] {
  const permissions = usePermissionsStore()
  const canAdmin = permissions.can('administrator')
  const canManage = permissions.can('manage_channels')
  if (!canAdmin && !canManage) return []
  const items: ContextMenuItem[] = [{ type: 'separator' }]
  if (canAdmin) items.push(webhooksItem(channel, handlers))
  if (canManage) {
    items.push(
      { id: 'permissions', label: t('overwrites.menu'), icon: Shield, action: () => handlers.onPermissions?.(channel) },
      ...stageToggleItems(channel),
      { id: 'edit', label: t('sidebar.editChannel'), icon: Pencil, action: () => handlers.onEdit?.(channel) },
      { id: 'duplicate', label: t('sidebar.duplicateChannel'), icon: CopyPlus, action: () => handlers.onDuplicate?.(channel) },
      {
        id: 'delete',
        label: t(channel.type === 'voice' ? 'sidebar.deleteVoiceChannel' : 'sidebar.deleteChannel'),
        icon: Trash2,
        danger: true,
        action: () => handlers.onDelete?.(channel)
      }
    )
  }
  return items
}

/** handlers: { onEdit(channel), onDuplicate(channel), onDelete(channel) } (admin only). */
export function buildChannelItems(channel: Channel, handlers: ChannelHandlers = {}): ContextMenuItem[] {
  return [
    ...basicChannelItems(channel),
    { type: 'separator' },
    ...notifyMenuItems({ kind: 'channel', id: channel.id, inCategory: !!channel.category_id }),
    ...talkSettingsItems(channel),
    ...channelManagementItems(channel, handlers)
  ]
}

function webhooksItem(channel: Channel, handlers: ChannelHandlers): ContextMenuItem {
  return { id: 'webhooks', label: t('webhooks.menu'), icon: Webhook, action: () => handlers.onWebhooks?.(channel) }
}

/**
 * handlers: { onCollapseAll, onExpandAll, allCollapsed, noneCollapsed } for
 * everyone; { onCreateChannel(category), onCreateCategory(category), onEdit,
 * onDelete } for admins.
 */
export function buildCategoryItems(category: Category, handlers: CategoryHandlers = {}): ContextMenuItem[] {
  const chatStore = useChatStore()
  const permissions = usePermissionsStore()
  const toasts = useToastStore()
  // Text and voice channels alike: both have a chat.
  const channels = category.channels || []
  const hasUnread = channels.some(c => {
    const rs = chatStore.readStates?.[c.id]
    return (rs?.unread_count || 0) > 0 || (rs?.mention_count || 0) > 0
  })
  const items: ContextMenuItem[] = [{
    id: 'mark-all-read',
    label: t('sidebar.markAllRead'),
    icon: CheckCheck,
    disabled: !hasUnread,
    action: async () => {
      await chatStore.markAllRead?.(category.id)
      toasts.success(t('sidebar.allMarkedRead'))
    }
  }]
  items.push({ type: 'separator' })
  items.push(...notifyMenuItems({ kind: 'category', id: category.id }))
  items.push({ type: 'separator' })
  items.push({
    id: 'collapse-all',
    label: t('sidebar.collapseAll'),
    icon: ChevronsDownUp,
    disabled: !!handlers.allCollapsed,
    action: () => handlers.onCollapseAll?.()
  })
  items.push({
    id: 'expand-all',
    label: t('sidebar.expandAll'),
    icon: ChevronsUpDown,
    disabled: !!handlers.noneCollapsed,
    action: () => handlers.onExpandAll?.()
  })
  if (permissions.can('manage_channels')) {
    items.push({ type: 'separator' })
    items.push({ id: 'permissions', label: t('overwrites.menu'), icon: Shield, action: () => handlers.onPermissions?.(category) })
    items.push({ id: 'create-channel', label: t('channel.create'), icon: Plus, action: () => handlers.onCreateChannel?.(category) })
    items.push({ id: 'create-category', label: t('sidebar.createCategory'), icon: FolderPlus, action: () => handlers.onCreateCategory?.(category) })
    items.push({ type: 'separator' })
    items.push({ id: 'edit', label: t('sidebar.editCategory'), icon: Pencil, action: () => handlers.onEdit?.(category) })
    items.push({ id: 'delete', label: t('sidebar.deleteCategory'), icon: Trash2, danger: true, action: () => handlers.onDelete?.(category) })
  }
  return items
}

/**
 * Right-click on the empty part of the channel list. Only admins get a menu
 * (members keep the browser's own); handlers: { onCreateChannel, onCreateCategory }.
 */
export function buildSidebarItems(handlers: { onCreateChannel?: () => void; onCreateCategory?: () => void } = {}): ContextMenuItem[] {
  if (!usePermissionsStore().can('manage_channels')) return []
  return [
    { id: 'create-channel', label: t('channel.create'), icon: Plus, action: () => handlers.onCreateChannel?.() },
    { id: 'create-category', label: t('sidebar.createCategory'), icon: FolderPlus, action: () => handlers.onCreateCategory?.() }
  ]
}

function displayName(m: MenuMember) {
  return m.display_name || m.username || ''
}

/** Mention in the composer (chat or Talk chat); copy "@name" if none is open. */
export async function mentionMember(member: MenuMember) {
  const chatStore = useChatStore()
  if (!member?.username) return
  if (chatStore.activeChannel) {
    chatStore.insertMention(member.username)
    return
  }
  await copyText(`@${member.username}`, t('nav.mentionCopied'))
}

async function memberCall(member: MenuMember, url: string, { title, body, confirmLabel, done }: { title: string; body: string; confirmLabel: string; done: string }) {
  const toasts = useToastStore()
  const ok = await confirm({ title: t(title, { name: displayName(member) }), body: t(body), confirmLabel: t(confirmLabel), danger: true })
  if (!ok) return
  try {
    await api(url, { method: 'POST' })
    toasts.success(t(done))
  } catch (e) {
    toasts.error(e instanceof Error && e.message ? e.message : t('nav.actionFailed'))
  }
}

export function isInVoiceWithMe(userId: string) {
  const voiceStore = useVoiceStore()
  const cur = voiceStore.currentChannelId
  return !!(cur && voiceStore.channelUsers?.[cur]?.[userId])
}

/** Local audio and camera controls, only for someone I'm in a call with. */
function localCallItems(member: MenuMember, ctx: { refresh?: () => void }): ContextMenuItem[] {
  const voiceStore = useVoiceStore()
  const muted = !!voiceStore.isUserLocalMuted?.(member.id)
  const items: ContextMenuItem[] = [
    { type: 'separator' },
    {
      id: 'volume',
      type: 'slider',
      label: t('voice.userVolume'),
      min: 0,
      max: 200,
      step: 5,
      value: voiceStore.getUserVolume?.(member.id) ?? 100,
      onInput: v => { voiceStore.setUserVolume?.(member.id, v); ctx.refresh?.() }
    },
    {
      id: 'local-mute',
      label: t(muted ? 'voice.unmuteUser' : 'voice.muteUser'),
      icon: muted ? Volume2 : VolumeX,
      action: () => voiceStore.toggleLocalMute?.(member.id)
    }
  ]
  // Cameras are on by default; with "all cameras off" there is nothing to pick.
  if (!voiceStore.allCamerasOff) {
    const hidden = !!voiceStore.hiddenCameras?.[member.id]
    items.push({
      id: 'camera-hide',
      label: t(hidden ? 'talk.showCamera' : 'talk.hideCamera'),
      icon: hidden ? Eye : EyeOff,
      action: () => voiceStore.toggleCameraHidden?.(member.id)
    })
  }
  return items
}

/** A Talk the member sits in where the viewer may disconnect people (a role's grant, server-wide or through an overwrite there). */
function disconnectableTalk(memberId: string): string | null {
  const permissions = usePermissionsStore()
  const talks = Object.entries(useVoiceStore().channelUsers || {})
  return talks.find(([id, users]) => !!users?.[memberId] && permissions.canIn(id, 'disconnect_members'))?.[0] ?? null
}

function kickItems(member: MenuMember, talk: string): ContextMenuItem[] {
  return [{
    id: 'kick',
    label: t('admin.kickFromVoice'),
    icon: UserX,
    danger: true,
    action: () => memberCall(member, `/api/channels/${talk}/voice/${member.id}/disconnect`, {
      title: 'admin.kickFromVoiceTitle', body: 'admin.kickFromVoiceBody', confirmLabel: 'admin.kickConfirm', done: 'admin.userKicked'
    })
  }]
}

function sessionItems(member: MenuMember): ContextMenuItem[] {
  return [
    {
      id: 'revoke',
      label: t('admin.revokeSessions'),
      icon: LogOut,
      danger: true,
      action: () => memberCall(member, `/api/admin/users/${member.id}/sessions/revoke`, {
        title: 'admin.revokeSessionsTitle', body: 'admin.revokeSessionsBody', confirmLabel: 'admin.revokeSessionsConfirm', done: 'admin.sessionsRevoked'
      })
    },
    {
      id: 'disable',
      label: t('admin.disableUser'),
      icon: Ban,
      danger: true,
      action: () => memberCall(member, `/api/admin/users/${member.id}/disable`, {
        title: 'admin.disableUserTitle', body: 'admin.disableUserBody', confirmLabel: 'admin.disableUserConfirm', done: 'admin.userDisabledToast'
      })
    }
  ]
}

function timeoutAndBanItems(member: MenuMember, offer: ReturnType<typeof moderationOffer>): ContextMenuItem[] {
  const items: ContextMenuItem[] = []
  if (offer.timeout) {
    items.push({ id: 'timeout', label: t('moderation.timeoutAction'), icon: Clock, danger: true, action: () => { void openTimeout(member) } })
  }
  if (offer.removeTimeout) {
    items.push({ id: 'untimeout', label: t('moderation.removeTimeout'), icon: Clock, action: () => { void liftTimeout(member) } })
  }
  if (offer.ban) {
    items.push({ id: 'ban', label: t('moderation.banAction'), icon: Gavel, danger: true, action: () => { void openBan(member) } })
  }
  return items
}

function isMemberReachable(member: MenuMember, isSelf: boolean): boolean {
  if (isSelf || member.role === 'admin') return false
  return usePermissionsStore().outranksMember(member.id)
}

function hasModerationActions(kickTalk: string | null, canModerate: boolean, offer: ReturnType<typeof moderationOffer>): boolean {
  if (kickTalk || canModerate) return true
  return !!offer.timeout || !!offer.ban
}

/** Kick, timeout, ban, revoke sessions and disable, as far as the viewer may. */
function moderationItems(member: MenuMember, isSelf: boolean): ContextMenuItem[] {
  const permissions = usePermissionsStore()
  const reachable = isMemberReachable(member, isSelf)
  const kickTalk = reachable ? disconnectableTalk(member.id) : null
  const canModerate = reachable && permissions.can('manage_members')
  const offer = moderationOffer(member)
  const items: ContextMenuItem[] = []
  if (hasModerationActions(kickTalk, canModerate, offer)) {
    items.push({ type: 'separator' })
    if (kickTalk) items.push(...kickItems(member, kickTalk))
  }
  items.push(...timeoutAndBanItems(member, offer))
  if (canModerate) items.push(...sessionItems(member))
  return items
}

/** ctx: { refresh } so the volume slider re-renders its value. */
export function buildMemberItems(member: MenuMember, ctx: { refresh?: () => void } = {}): ContextMenuItem[] {
  const chatStore = useChatStore()
  const isSelf = useAuthStore().user?.id === member.id
  const items: ContextMenuItem[] = [
    { id: 'profile', label: t('profile.viewProfile'), icon: User, action: () => chatStore.openUserProfile(member) },
    { id: 'mention', label: t('profile.mention'), icon: AtSign, disabled: !member.username, action: () => mentionMember(member) }
  ]
  // Local audio controls only make sense for someone I'm in a call with.
  if (!isSelf && isInVoiceWithMe(member.id)) items.push(...localCallItems(member, ctx))
  items.push(...memberRelationItems(member), ...moderationItems(member, isSelf))
  return items
}
