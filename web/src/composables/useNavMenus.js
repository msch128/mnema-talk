// Context-menu content for the sidebar (channels, categories) and for people
// (member list, voice participants). Pure builders around the stores so the
// menus are testable per role.
import { reactive, ref, computed, nextTick } from 'vue'
import {
  CheckCheck, Link, Pencil, Trash2, User, AtSign, Volume2, VolumeX, Eye, EyeOff, UserX, Ban, LogOut,
  CopyPlus, Plus, FolderPlus, ChevronsDownUp, ChevronsUpDown
} from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'
import { api } from '../lib/api'
import { confirm } from '../lib/confirm'
import { menuPointFromEvent } from '../lib/contextMenuPoint'
import { t } from '../i18n'

export const NOTIFY_LEVELS = ['all', 'mentions', 'mute']

/** Open/close state plus the items for one ContextMenu instance. */
export function useMenuState() {
  const state = reactive({ open: false, x: 0, y: 0, anchor: null, build: null })
  const tick = ref(0)
  const items = computed(() => {
    void tick.value
    return state.open && state.build ? state.build(refresh) : []
  })
  function refresh() { tick.value++ }
  async function show(e, build) {
    e?.preventDefault?.()
    e?.stopPropagation?.()
    const p = menuPointFromEvent(e)
    state.open = false
    await nextTick()
    Object.assign(state, { x: p.x, y: p.y, anchor: p.anchor, build, open: true })
  }
  return { state, items, show, refresh }
}

export function channelLink(channel) {
  const base = typeof window !== 'undefined' ? window.location.origin : ''
  return `${base}/${channel.type === 'voice' ? 'v' : 'c'}/${channel.id}`
}

async function copyText(text, okMessage) {
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

export function notifyLevelOf(chatStore, channelId) {
  return chatStore.notificationLevel?.(channelId)
    ?? chatStore.readStates?.[channelId]?.notify_level
    ?? 'all'
}

/** handlers: { onEdit(channel), onDuplicate(channel), onDelete(channel) } (admin only). */
export function buildChannelItems(channel, handlers = {}) {
  const chatStore = useChatStore()
  const authStore = useAuthStore()
  const toasts = useToastStore()
  // Every channel has a chat (a voice channel's sits next to its Talk), so
  // every channel has a read state and a notification level.
  const rs = chatStore.readStates?.[channel.id]
  const hasUnread = (rs?.unread_count || 0) > 0 || (rs?.mention_count || 0) > 0
  const items = [{
    id: 'mark-read',
    label: t('sidebar.markRead'),
    icon: CheckCheck,
    disabled: !hasUnread,
    action: async () => {
      await chatStore.markChannelRead?.(channel.id)
      toasts.success(t('sidebar.markedRead'))
    }
  }, {
    id: 'copy-link',
    label: t('sidebar.copyLink'),
    icon: Link,
    action: () => copyText(channelLink(channel), t('sidebar.copiedLink'))
  }]

  const current = notifyLevelOf(chatStore, channel.id)
  items.push({ type: 'separator' })
  items.push({ type: 'label', label: t('notifications.title') })
  for (const level of NOTIFY_LEVELS) {
    items.push({
      id: `notify-${level}`,
      type: 'radio',
      label: t(`notifications.${level}`),
      checked: current === level,
      action: () => chatStore.setNotificationLevel?.(channel.id, level)
    })
  }

  if (authStore.isAdmin) {
    items.push({ type: 'separator' })
    items.push({
      id: 'edit',
      label: t('sidebar.editChannel'),
      icon: Pencil,
      action: () => handlers.onEdit?.(channel)
    })
    items.push({
      id: 'duplicate',
      label: t('sidebar.duplicateChannel'),
      icon: CopyPlus,
      action: () => handlers.onDuplicate?.(channel)
    })
    items.push({
      id: 'delete',
      label: t(channel.type === 'voice' ? 'sidebar.deleteVoiceChannel' : 'sidebar.deleteChannel'),
      icon: Trash2,
      danger: true,
      action: () => handlers.onDelete?.(channel)
    })
  }
  return items
}

/**
 * handlers: { onCollapseAll, onExpandAll, allCollapsed, noneCollapsed } for
 * everyone; { onCreateChannel(category), onCreateCategory(category), onEdit,
 * onDelete } for admins.
 */
export function buildCategoryItems(category, handlers = {}) {
  const chatStore = useChatStore()
  const authStore = useAuthStore()
  const toasts = useToastStore()
  // Text and voice channels alike: both have a chat.
  const channels = category.channels || []
  const hasUnread = channels.some(c => {
    const rs = chatStore.readStates?.[c.id]
    return (rs?.unread_count || 0) > 0 || (rs?.mention_count || 0) > 0
  })
  const items = [{
    id: 'mark-all-read',
    label: t('sidebar.markAllRead'),
    icon: CheckCheck,
    disabled: !hasUnread,
    action: async () => {
      await Promise.all(channels.map(c => chatStore.markChannelRead?.(c.id)))
      toasts.success(t('sidebar.allMarkedRead'))
    }
  }]
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
  if (authStore.isAdmin) {
    items.push({ type: 'separator' })
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
export function buildSidebarItems(handlers = {}) {
  const authStore = useAuthStore()
  if (!authStore.isAdmin) return []
  return [
    { id: 'create-channel', label: t('channel.create'), icon: Plus, action: () => handlers.onCreateChannel?.() },
    { id: 'create-category', label: t('sidebar.createCategory'), icon: FolderPlus, action: () => handlers.onCreateCategory?.() }
  ]
}

function displayName(m) {
  return m.display_name || m.username || ''
}

/** Mention in the composer (chat or Talk chat); copy "@name" if none is open. */
export async function mentionMember(member) {
  const chatStore = useChatStore()
  if (!member?.username) return
  if (chatStore.activeChannel) {
    chatStore.insertMention(member.username)
    return
  }
  await copyText(`@${member.username}`, t('nav.mentionCopied'))
}

async function adminCall(member, path, { title, body, confirmLabel, done }) {
  const toasts = useToastStore()
  const ok = await confirm({ title: t(title, { name: displayName(member) }), body: t(body), confirmLabel: t(confirmLabel), danger: true })
  if (!ok) return
  try {
    await api(`/api/admin/users/${member.id}/${path}`, { method: 'POST' })
    toasts.success(t(done))
  } catch (e) {
    toasts.error(e?.message || t('nav.actionFailed'))
  }
}

export function isInVoiceWithMe(userId) {
  const voiceStore = useVoiceStore()
  const cur = voiceStore.currentChannelId
  return !!(cur && voiceStore.channelUsers?.[cur]?.[userId])
}

/** ctx: { refresh } so the volume slider re-renders its value. */
export function buildMemberItems(member, ctx = {}) {
  const chatStore = useChatStore()
  const voiceStore = useVoiceStore()
  const authStore = useAuthStore()
  const isSelf = authStore.user?.id === member.id
  const items = [
    { id: 'profile', label: t('profile.viewProfile'), icon: User, action: () => chatStore.openUserProfile(member) },
    { id: 'mention', label: t('profile.mention'), icon: AtSign, disabled: !member.username, action: () => mentionMember(member) }
  ]

  // Local audio controls only make sense for someone I'm in a call with.
  if (!isSelf && isInVoiceWithMe(member.id)) {
    const muted = !!voiceStore.isUserLocalMuted?.(member.id)
    items.push({ type: 'separator' })
    items.push({
      id: 'volume',
      type: 'slider',
      label: t('voice.userVolume'),
      min: 0,
      max: 200,
      step: 5,
      value: voiceStore.getUserVolume?.(member.id) ?? 100,
      onInput: v => { voiceStore.setUserVolume?.(member.id, v); ctx.refresh?.() }
    })
    items.push({
      id: 'local-mute',
      label: t(muted ? 'voice.unmuteUser' : 'voice.muteUser'),
      icon: muted ? Volume2 : VolumeX,
      action: () => voiceStore.toggleLocalMute?.(member.id)
    })
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
  }

  if (authStore.isAdmin && !isSelf) {
    items.push({ type: 'separator' })
    const inAnyCall = Object.values(voiceStore.channelUsers || {}).some(u => u && u[member.id])
    if (inAnyCall) {
      items.push({
        id: 'kick',
        label: t('admin.kickFromVoice'),
        icon: UserX,
        danger: true,
        action: () => adminCall(member, 'kick', {
          title: 'admin.kickFromVoiceTitle', body: 'admin.kickFromVoiceBody', confirmLabel: 'admin.kickConfirm', done: 'admin.userKicked'
        })
      })
    }
    items.push({
      id: 'revoke',
      label: t('admin.revokeSessions'),
      icon: LogOut,
      danger: true,
      action: () => adminCall(member, 'sessions/revoke', {
        title: 'admin.revokeSessionsTitle', body: 'admin.revokeSessionsBody', confirmLabel: 'admin.revokeSessionsConfirm', done: 'admin.sessionsRevoked'
      })
    })
    items.push({
      id: 'disable',
      label: t('admin.disableUser'),
      icon: Ban,
      danger: true,
      action: () => adminCall(member, 'disable', {
        title: 'admin.disableUserTitle', body: 'admin.disableUserBody', confirmLabel: 'admin.disableUserConfirm', done: 'admin.userDisabledToast'
      })
    })
  }
  return items
}
