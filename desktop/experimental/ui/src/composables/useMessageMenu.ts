// The message context menu of the channel chat: what it offers for a
// message and where it opens (pointer, ⋯ button or keyboard).
import { ref } from 'vue'
import type { Message } from '../types/domain'
import type { ContextMenuItem } from '../components/menuTypes'
import type { useMessageActions } from './useMessageActions'
import {
  MessageSquare, Pencil, Trash2, SmilePlus, Reply,
  Link as LinkIcon, Copy, Bookmark
} from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'

/**
 * @param actions  the list's useMessageActions()
 * @param reply    (msg) => void, starts a reply in the composer
 */
export function useMessageMenu({ actions, reply }: { actions: ReturnType<typeof useMessageActions>; reply: (message: Message) => void }) {
  const chatStore = useChatStore()
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

  function itemsFor(msg: Message): ContextMenuItem[] {
    const items: ContextMenuItem[] = [
      { label: t('chat.reply'), icon: Reply, shortcut: 'r', action: () => reply(msg) },
      { label: t('chat.addReaction'), icon: SmilePlus, action: () => { actions.pickerId.value = msg.id } },
      { label: t('chat.openThread'), icon: MessageSquare, action: () => chatStore.openThread(msg) }
    ]
    if (actions.isOwn(msg)) {
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
    items.push({
      label: t('chat.markUnread'),
      icon: Bookmark,
      action: async () => {
        try {
          await chatStore.markChannelUnread(chatStore.activeChannel?.id ?? msg.channel_id, msg.id)
          toasts.success(t('chat.markedUnread'))
        } catch (err) {
          toasts.error(err instanceof Error && err.message ? err.message : t('chat.markUnreadFailed'))
        }
      }
    })
    if (actions.canDelete(msg)) {
      items.push({ type: 'separator' })
      items.push({ label: t('chat.delete'), icon: Trash2, danger: true, action: () => actions.deleteMessage(msg) })
    }
    return items
  }

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
