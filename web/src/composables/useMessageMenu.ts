// The message context menu of the channel chat: what it offers for a
// message and where it opens (pointer, ⋯ button or keyboard).
import { ref } from 'vue'
import type { Message } from '../types/domain'
import type { ContextMenuItem } from '../components/menuTypes'
import type { useMessageActions } from './useMessageActions'
import {
  MessageSquare, Pencil, Trash2, SmilePlus, Reply,
  Link as LinkIcon, Copy, Bookmark, Pin, PinOff, Eraser
} from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { usePermissionsStore } from '../stores/permissions'
import { moderateMessages } from '../lib/messageModeration'
import { previewText } from '../lib/replies'
import { t } from '../i18n'

type MessageActions = ReturnType<typeof useMessageActions>

interface MenuDeps {
  actions: MessageActions
  reply: (message: Message) => void
  copy: (text: string, doneKey: string) => Promise<void>
}

function errorText(err: unknown, fallback: string) {
  return err instanceof Error && err.message ? err.message : fallback
}

function markUnreadItem(msg: Message): ContextMenuItem {
  const chatStore = useChatStore()
  const toasts = useToastStore()
  return {
    label: t('chat.markUnread'),
    icon: Bookmark,
    action: async () => {
      try {
        await chatStore.markChannelUnread(chatStore.activeChannel?.id ?? msg.channel_id, msg.id)
        toasts.success(t('chat.markedUnread'))
      } catch (err) {
        toasts.error(errorText(err, t('chat.markUnreadFailed')))
      }
    }
  }
}

/** Reply, react, thread, edit, copy and mark unread: what everyone gets. */
function basicItems(msg: Message, { actions, reply, copy }: MenuDeps): ContextMenuItem[] {
  const chatStore = useChatStore()
  const items: ContextMenuItem[] = [
    { label: t('chat.reply'), icon: Reply, shortcut: 'r', action: () => reply(msg) },
    { label: t('chat.addReaction'), icon: SmilePlus, action: () => { actions.pickerId.value = msg.id } },
    { label: t('chat.openThread'), icon: MessageSquare, action: () => chatStore.openThread(msg) }
  ]
  if (actions.isOwn(msg) && !msg.poll) {
    items.push({ label: t('chat.edit'), icon: Pencil, shortcut: 'e', action: () => actions.startEdit(msg) })
  }
  items.push({ type: 'separator' })
  items.push({
    label: t('chat.copyText'),
    icon: Copy,
    action: () => copy(msg.content || '', 'chat.copiedText')
  })
  items.push({
    label: t('chat.copyLink'),
    icon: LinkIcon,
    action: () => copy(`${window.location.origin}/c/${chatStore.activeChannel?.id}/m/${msg.id}`, 'chat.copiedLink')
  })
  items.push(markUnreadItem(msg))
  return items
}

function pinItem(msg: Message, channelId: string): ContextMenuItem {
  const chatStore = useChatStore()
  const toasts = useToastStore()
  return {
    label: t(msg.is_pinned ? 'moderation.unpinMessage' : 'moderation.pinMessage'),
    icon: msg.is_pinned ? PinOff : Pin,
    action: async () => {
      try {
        await chatStore.setPinned(channelId, msg.id, !msg.is_pinned)
      } catch (err) {
        toasts.error(errorText(err, t('moderation.failed')))
      }
    }
  }
}

/** Delete, and for a moderator deleting someone else's message, delete with a reason. */
function deleteItems(msg: Message, channelId: string, moderator: boolean, actions: MessageActions): ContextMenuItem[] {
  if (!actions.canDelete(msg)) return []
  const items: ContextMenuItem[] = [
    { type: 'separator' },
    { label: t('chat.delete'), icon: Trash2, danger: true, action: () => actions.deleteMessage(msg) }
  ]
  if (moderator && !actions.isOwn(msg)) {
    const excerpt = previewText(msg.content).slice(0, 160)
    items.push({
      label: t('moderation.deleteWithReason'), icon: Trash2, danger: true,
      action: () => { void moderateMessages({ kind: 'delete', channelId, messageId: msg.id, excerpt }) }
    })
  }
  return items
}

// Clearing a member's latest messages needs rank over them; the Owner and bots are out.
function canClearLatest(msg: Message, moderator: boolean, actions: MessageActions) {
  if (!moderator || actions.isOwn(msg) || msg.is_bot) return false
  const author = useChatStore().members.find(m => m.id === msg.user_id)
  return !!author && author.role !== 'admin' && usePermissionsStore().outranksMember(msg.user_id)
}

function messageItems(msg: Message, deps: MenuDeps): ContextMenuItem[] {
  const { actions } = deps
  const items = basicItems(msg, deps)
  const channelId = useChatStore().activeChannel?.id ?? msg.channel_id
  const moderator = usePermissionsStore().canIn(channelId, 'manage_messages')
  if (moderator) items.push({ type: 'separator' }, pinItem(msg, channelId))
  items.push(...deleteItems(msg, channelId, moderator, actions))
  if (canClearLatest(msg, moderator, actions)) {
    items.push({
      label: t('moderation.deleteLatest'), icon: Eraser, danger: true,
      action: () => { void moderateMessages({ kind: 'bulk', channelId, userId: msg.user_id, name: msg.display_name || msg.username }) }
    })
  }
  return items
}

/**
 * @param actions  the list's useMessageActions()
 * @param reply    (msg) => void, starts a reply in the composer
 */
export function useMessageMenu({ actions, reply }: { actions: MessageActions; reply: (message: Message) => void }) {
  const toasts = useToastStore()

  // Bound to <ContextMenu>; also used for other menus of the view.
  const state = ref<{ open: boolean; x: number; y: number; items: ContextMenuItem[] }>({ open: false, x: 0, y: 0, items: [] })

  function show(x: number, y: number, items: ContextMenuItem[]) {
    state.value = { open: true, x: x || 0, y: y || 0, items }
  }

  function close() {
    state.value.open = false
  }

  async function copy(text: string, doneKey: string) {
    try {
      await navigator.clipboard.writeText(text)
      toasts.success(t(doneKey))
    } catch (err) {
      console.warn('Copy failed:', err)
    }
  }

  const itemsFor = (msg: Message) => messageItems(msg, { actions, reply, copy })

  function openAt(x: number, y: number, msg: Message) {
    actions.closePicker()
    show(x, y, itemsFor(msg))
  }

  // Keyboard (no pointer position): at the message's own position.
  function openAtElement(el: Element, msg: Message) {
    const rect = el.getBoundingClientRect()
    openAt(rect.left + 80, Math.min(Math.max(rect.top, 0) + 24, window.innerHeight - 24), msg)
  }

  // Right-click opens at the pointer; the menu key sends no position.
  function onContextMenu(e: MouseEvent, msg: Message) {
    if (!e.clientX && !e.clientY && e.currentTarget instanceof Element) openAtElement(e.currentTarget, msg)
    else openAt(e.clientX, e.clientY, msg)
  }

  // The ⋯ button in the hover bar: below the button.
  function openFromButton(e: MouseEvent, msg: Message) {
    if (!(e.currentTarget instanceof Element)) return
    const rect = e.currentTarget.getBoundingClientRect()
    openAt(rect.left, rect.bottom + 4, msg)
  }

  return { state, show, close, itemsFor, onContextMenu, openFromButton, openAtElement }
}
